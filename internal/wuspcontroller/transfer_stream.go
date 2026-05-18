package wuspcontroller

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"WantasticCore/internal/wusp"
)

const (
	uspTransferWindowSize = 8
	uspTransferAckTimeout = 2 * time.Second
)

type uspTransferSession struct {
	id               uint64
	requestID        uint64
	method           wusp.USPAgentMethod
	peerPublicKey    string
	startedAt        time.Time
	path             string
	totalSize        int64
	transferred      int64
	nextSequence     uint32
	file             *os.File
	writer           *bufio.Writer
	ackCh            chan uint32
	completeCh       chan wusp.USPTransferStreamFrame
	errCh            chan error
	mu               sync.Mutex
}

func (c *WUSPController) uploadOverStream(ctx context.Context, peerPublicKey string, transfer *wusp.USPTransferRequest, resp wusp.USPAgentResponse) (wusp.USPAgentResponse, error) {
	sessionID, ok := transferSessionID(resp.Transfer)
	if !ok {
		return resp, nil
	}
	var (
		reader     io.ReadCloser
		size       int64
		sourcePath string
	)
	if len(transfer.Payload) > 0 {
		reader = io.NopCloser(bytes.NewReader(append([]byte(nil), transfer.Payload...)))
		size = int64(len(transfer.Payload))
	} else {
		sourcePath = firstNonEmpty(strings.TrimSpace(transfer.Metadata[wusp.TransferMetadataSource]))
		if sourcePath == "" {
			return wusp.USPAgentResponse{}, fmt.Errorf("wuspcontroller: upload source: upload payload or %q metadata is required", wusp.TransferMetadataSource)
		}
		file, openErr := os.Open(sourcePath)
		if openErr != nil {
			return wusp.USPAgentResponse{}, fmt.Errorf("wuspcontroller: upload source: %w", openErr)
		}
		info, statErr := file.Stat()
		if statErr != nil {
			_ = file.Close()
			return wusp.USPAgentResponse{}, fmt.Errorf("wuspcontroller: upload source: %w", statErr)
		}
		reader = file
		size = info.Size()
	}
	defer reader.Close()
	session := &uspTransferSession{
		id:            sessionID,
		requestID:     resp.ID,
		method:        wusp.USPAgentMethodUpload,
		peerPublicKey: peerPublicKey,
		startedAt:     time.Now(),
		totalSize:     size,
		nextSequence:  1,
		ackCh:         make(chan uint32, uspTransferWindowSize*2),
		completeCh:    make(chan wusp.USPTransferStreamFrame, 1),
		errCh:         make(chan error, 1),
	}
	c.storeTransferSession(session)
	c.recordTransferSessionStart()
	defer c.deleteTransferSession(peerPublicKey, sessionID)

	if err := c.streamUploadSession(ctx, session, reader, transfer); err != nil {
		_ = c.abortTransferSession(session)
		c.recordTransferSessionAbort(session, err)
		return wusp.USPAgentResponse{}, err
	}
	c.recordTransferSessionComplete(session)

	if resp.Transfer == nil {
		resp.Transfer = &wusp.USPTransferResult{}
	}
	resp.Transfer.Path = transfer.Path
	resp.Transfer.Bytes = size
	resp.Transfer.Metadata = wusp.CloneMetadata(resp.Transfer.Metadata)
	if resp.Transfer.Metadata == nil {
		resp.Transfer.Metadata = make(map[string]string, 2)
	}
	resp.Transfer.Metadata[wusp.TransferMetadataTransport] = "wg-stream"
	if sourcePath != "" {
		resp.Transfer.Metadata[wusp.TransferMetadataSource] = sourcePath
	}
	return resp, nil
}

func (c *WUSPController) streamUploadSession(ctx context.Context, session *uspTransferSession, reader io.Reader, transfer *wusp.USPTransferRequest) error {
	if err := c.sendTransferStream(session, wusp.USPTransferStreamFrame{
		SessionID:   session.id,
		RequestID:   session.requestID,
		Method:      session.method,
		Phase:       wusp.USPTransferStreamOpen,
		Path:        transfer.Path,
		Filename:    transfer.Filename,
		ContentType: transfer.ContentType,
		TotalSize:   uint64(maxInt64(session.totalSize, 0)),
	}); err != nil {
		return err
	}

	buf := make([]byte, wusp.USPRecommendedChunkSize)
	pending := make(map[uint32]uspTransferPendingChunk, uspTransferWindowSize)
	for {
		for len(pending) < uspTransferWindowSize {
			n, readErr := reader.Read(buf)
			if n > 0 {
				payload := append([]byte(nil), buf[:n]...)
				frame := wusp.USPTransferStreamFrame{
					SessionID: session.id,
					RequestID: session.requestID,
					Method:    session.method,
					Phase:     wusp.USPTransferStreamChunk,
					Sequence:  session.nextSequence,
					Offset:    uint64(session.transferred + pendingTransferredBytes(pending)),
					TotalSize: uint64(maxInt64(session.totalSize, 0)),
					Data:      payload,
					Final:     readErr == io.EOF,
				}
				pending[frame.Sequence] = uspTransferPendingChunk{frame: frame, size: n}
				if err := c.sendTransferStream(session, frame); err != nil {
					return err
				}
				session.nextSequence++
			}
			if readErr == io.EOF {
				goto waitPending
			}
			if readErr != nil {
				return readErr
			}
		}

	waitPending:
		if len(pending) == 0 {
			break
		}
		ackSequence, err := session.waitForAnyAck(ctx)
		if err != nil {
			c.stats.transferAckTimeouts.Add(1)
			resent, ok := resendPendingChunks(c, session, pending)
			if resent > 0 {
				c.stats.transferChunkResends.Add(uint64(resent))
				c.log.Debug().
					Str("peer", session.peerPublicKey).
					Uint64("session_id", session.id).
					Str("method", session.method.String()).
					Int("chunks", resent).
					Msg("wusp: resending pending transfer chunks after ack timeout")
			}
			if !ok {
				return err
			}
			continue
		}
		session.transferred += releaseAckedPendingChunks(pending, ackSequence)
	}

	if err := c.sendTransferStream(session, wusp.USPTransferStreamFrame{
		SessionID:   session.id,
		RequestID:   session.requestID,
		Method:      session.method,
		Phase:       wusp.USPTransferStreamComplete,
		AckSequence: session.nextSequence - 1,
		Offset:      uint64(session.transferred),
		TotalSize:   uint64(maxInt64(session.totalSize, session.transferred)),
		Final:       true,
	}); err != nil {
		return err
	}

	select {
	case <-ctx.Done():
		return fmt.Errorf("wuspcontroller: upload stream session %d: %w", session.id, ctx.Err())
	case err := <-session.errCh:
		return err
	case <-session.completeCh:
		return nil
	}
}

func (c *WUSPController) downloadOverStream(ctx context.Context, peerPublicKey string, transfer *wusp.USPTransferRequest, resp wusp.USPAgentResponse) (wusp.USPAgentResponse, error) {
	sessionID, ok := transferSessionID(resp.Transfer)
	if !ok {
		return resp, nil
	}
	targetPath := firstNonEmpty(strings.TrimSpace(transfer.Metadata[wusp.TransferMetadataDestination]), localPathFromURI(transfer.Filename))
	if targetPath == "" {
		targetPath = filepath.Join(os.TempDir(), sanitizeTransferName(transfer.Path)+".download")
	}
	if err := os.MkdirAll(filepath.Dir(targetPath), 0o755); err != nil {
		return wusp.USPAgentResponse{}, fmt.Errorf("wuspcontroller: download mkdir: %w", err)
	}
	file, err := os.Create(targetPath)
	if err != nil {
		return wusp.USPAgentResponse{}, fmt.Errorf("wuspcontroller: download open: %w", err)
	}

	session := &uspTransferSession{
		id:            sessionID,
		requestID:     resp.ID,
		method:        wusp.USPAgentMethodDownload,
		peerPublicKey: peerPublicKey,
		startedAt:     time.Now(),
		path:          targetPath,
		file:          file,
		writer:        bufio.NewWriterSize(file, wusp.USPRecommendedChunkSize*uspTransferWindowSize),
		nextSequence:  1,
		ackCh:         make(chan uint32, uspTransferWindowSize*2),
		completeCh:    make(chan wusp.USPTransferStreamFrame, 1),
		errCh:         make(chan error, 1),
	}
	c.storeTransferSession(session)
	c.recordTransferSessionStart()
	defer c.deleteTransferSession(peerPublicKey, sessionID)
	defer func() {
		session.mu.Lock()
		if session.writer != nil {
			_ = session.writer.Flush()
			session.writer = nil
		}
		if session.file != nil {
			_ = session.file.Close()
			session.file = nil
		}
		session.mu.Unlock()
	}()

	select {
	case <-ctx.Done():
		_ = c.abortTransferSession(session)
		_ = os.Remove(targetPath)
		err := fmt.Errorf("wuspcontroller: download stream session %d: %w", session.id, ctx.Err())
		c.recordTransferSessionAbort(session, err)
		return wusp.USPAgentResponse{}, err
	case err := <-session.errCh:
		_ = os.Remove(targetPath)
		c.recordTransferSessionAbort(session, err)
		return wusp.USPAgentResponse{}, err
	case frame := <-session.completeCh:
		c.recordTransferSessionComplete(session)
		if resp.Transfer == nil {
			resp.Transfer = &wusp.USPTransferResult{}
		}
		resp.Transfer.Path = transfer.Path
		resp.Transfer.URI = "file://" + targetPath
		resp.Transfer.Bytes = session.transferred
		resp.Transfer.Metadata = wusp.CloneMetadata(resp.Transfer.Metadata)
		if resp.Transfer.Metadata == nil && len(frame.Metadata) > 0 {
			resp.Transfer.Metadata = make(map[string]string, len(frame.Metadata))
		}
		for key, value := range frame.Metadata {
			resp.Transfer.Metadata[key] = value
		}
		if resp.Transfer.Metadata == nil {
			resp.Transfer.Metadata = make(map[string]string, 2)
		}
		resp.Transfer.Metadata[wusp.TransferMetadataTransport] = "wg-stream"
		resp.Transfer.Metadata[wusp.TransferMetadataDestination] = targetPath
		return resp, nil
	}
}

func (c *WUSPController) handleTransferStreamFrame(peerPublicKey string, frame wusp.USPTransferStreamFrame) {
	session, ok := c.loadTransferSession(peerPublicKey, frame.SessionID)
	if !ok {
		c.stats.transferUnknownSessions.Add(1)
		c.log.Warn().
			Str("peer", peerPublicKey).
			Uint64("session_id", frame.SessionID).
			Str("method", frame.Method.String()).
			Msg("wusp: dropped stream frame for unknown session")
		return
	}
	switch session.method {
	case wusp.USPAgentMethodUpload:
		c.handleUploadStreamFrame(session, frame)
	case wusp.USPAgentMethodDownload:
		if err := c.handleDownloadStreamFrame(session, frame); err != nil {
			session.signalErr(err)
		}
	default:
		session.signalErr(fmt.Errorf("wuspcontroller: unsupported transfer session method %d", session.method))
	}
}

func (c *WUSPController) handleUploadStreamFrame(session *uspTransferSession, frame wusp.USPTransferStreamFrame) {
	switch frame.Phase {
	case wusp.USPTransferStreamAck:
		select {
		case session.ackCh <- frame.AckSequence:
		default:
		}
	case wusp.USPTransferStreamComplete:
		session.signalComplete(frame)
	case wusp.USPTransferStreamAbort:
		session.signalErr(fmt.Errorf("wuspcontroller: upload stream aborted by peer"))
	}
}

func (c *WUSPController) handleDownloadStreamFrame(session *uspTransferSession, frame wusp.USPTransferStreamFrame) error {
	switch frame.Phase {
	case wusp.USPTransferStreamOpen:
		session.totalSize = int64(frame.TotalSize)
		return nil
	case wusp.USPTransferStreamChunk:
		if frame.Sequence != session.nextSequence {
			return c.sendTransferStream(session, wusp.USPTransferStreamFrame{
				SessionID:   session.id,
				RequestID:   session.requestID,
				Method:      session.method,
				Phase:       wusp.USPTransferStreamAck,
				AckSequence: session.nextSequence - 1,
				Offset:      uint64(session.transferred),
				TotalSize:   uint64(maxInt64(session.totalSize, session.transferred)),
			})
		}
		session.mu.Lock()
		writer := session.writer
		if writer == nil {
			writer = bufio.NewWriterSize(session.file, wusp.USPRecommendedChunkSize*uspTransferWindowSize)
			session.writer = writer
		}
		written, err := writer.Write(frame.Data)
		if err == nil && frame.Final {
			err = writer.Flush()
		}
		if err != nil {
			session.mu.Unlock()
			return err
		}
		session.transferred += int64(written)
		session.nextSequence++
		offset := session.transferred
		totalSize := maxInt64(session.totalSize, session.transferred)
		session.mu.Unlock()
		return c.sendTransferStream(session, wusp.USPTransferStreamFrame{
			SessionID:   session.id,
			RequestID:   session.requestID,
			Method:      session.method,
			Phase:       wusp.USPTransferStreamAck,
			AckSequence: frame.Sequence,
			Offset:      uint64(offset),
			TotalSize:   uint64(totalSize),
			Final:       frame.Final,
		})
	case wusp.USPTransferStreamComplete:
		session.mu.Lock()
		if session.writer != nil {
			if err := session.writer.Flush(); err != nil {
				session.mu.Unlock()
				return err
			}
			session.writer = nil
		}
		if session.file != nil {
			if err := session.file.Close(); err != nil {
				session.mu.Unlock()
				return err
			}
			session.file = nil
		}
		session.mu.Unlock()
		session.signalComplete(frame)
		return nil
	case wusp.USPTransferStreamAbort:
		session.signalErr(fmt.Errorf("wuspcontroller: download stream aborted by peer"))
		return nil
	default:
		return nil
	}
}

func (c *WUSPController) sendTransferStream(session *uspTransferSession, frame wusp.USPTransferStreamFrame) error {
	if c == nil || c.opts.Send == nil {
		return fmt.Errorf("wuspcontroller: Send function not configured")
	}
	payload, err := wusp.EncodeUSPTransferStreamFrame(frame)
	if err != nil {
		return err
	}
	c.stats.transferFramesSent.Add(1)
	c.stats.transferFrameBytesSent.Add(uint64(len(payload)))
	return c.opts.Send(session.peerPublicKey, payload)
}

func (c *WUSPController) abortTransferSession(session *uspTransferSession) error {
	return c.sendTransferStream(session, wusp.USPTransferStreamFrame{
		SessionID: session.id,
		RequestID: session.requestID,
		Method:    session.method,
		Phase:     wusp.USPTransferStreamAbort,
		Final:     true,
	})
}

func (c *WUSPController) storeTransferSession(session *uspTransferSession) {
	c.streamsMu.Lock()
	defer c.streamsMu.Unlock()
	byPeer := c.streams[session.peerPublicKey]
	if byPeer == nil {
		byPeer = make(map[uint64]*uspTransferSession)
		c.streams[session.peerPublicKey] = byPeer
	}
	byPeer[session.id] = session
}

func (c *WUSPController) loadTransferSession(peerPublicKey string, sessionID uint64) (*uspTransferSession, bool) {
	c.streamsMu.Lock()
	defer c.streamsMu.Unlock()
	byPeer := c.streams[peerPublicKey]
	if byPeer == nil {
		return nil, false
	}
	session, ok := byPeer[sessionID]
	return session, ok
}

func (c *WUSPController) deleteTransferSession(peerPublicKey string, sessionID uint64) {
	c.streamsMu.Lock()
	defer c.streamsMu.Unlock()
	byPeer := c.streams[peerPublicKey]
	if byPeer == nil {
		return
	}
	delete(byPeer, sessionID)
	if len(byPeer) == 0 {
		delete(c.streams, peerPublicKey)
	}
}

func transferSessionID(result *wusp.USPTransferResult) (uint64, bool) {
	if result == nil || len(result.Metadata) == 0 {
		return 0, false
	}
	value := strings.TrimSpace(result.Metadata[wusp.TransferMetadataSessionID])
	if value == "" {
		return 0, false
	}
	id, err := strconv.ParseUint(value, 10, 64)
	if err != nil || id == 0 {
		return 0, false
	}
	return id, true
}

type uspTransferPendingChunk struct {
	frame wusp.USPTransferStreamFrame
	size  int
}

func (s *uspTransferSession) waitForAnyAck(ctx context.Context) (uint32, error) {
	waitCtx, cancel := context.WithTimeout(ctx, uspTransferAckTimeout)
	defer cancel()

	select {
	case <-waitCtx.Done():
		return 0, waitCtx.Err()
	case ackSequence := <-s.ackCh:
		return ackSequence, nil
	case err := <-s.errCh:
		return 0, err
	}
}

func (s *uspTransferSession) signalComplete(frame wusp.USPTransferStreamFrame) {
	select {
	case s.completeCh <- frame:
	default:
	}
}

func (s *uspTransferSession) signalErr(err error) {
	select {
	case s.errCh <- err:
	default:
	}
}

func resendPendingChunks(ctrl *WUSPController, session *uspTransferSession, pending map[uint32]uspTransferPendingChunk) (int, bool) {
	sequences := make([]uint32, 0, len(pending))
	for sequence := range pending {
		sequences = append(sequences, sequence)
	}
	slices.Sort(sequences)
	resent := 0
	for _, sequence := range sequences {
		if err := ctrl.sendTransferStream(session, pending[sequence].frame); err != nil {
			return resent, false
		}
		resent++
	}
	return resent, true
}

func pendingTransferredBytes(pending map[uint32]uspTransferPendingChunk) int64 {
	var total int64
	for _, chunk := range pending {
		total += int64(chunk.size)
	}
	return total
}

func releaseAckedPendingChunks(pending map[uint32]uspTransferPendingChunk, ackSequence uint32) int64 {
	var released int64
	for sequence, chunk := range pending {
		if sequence <= ackSequence {
			released += int64(chunk.size)
			delete(pending, sequence)
		}
	}
	return released
}

func maxInt64(values ...int64) int64 {
	var max int64
	for _, value := range values {
		if value > max {
			max = value
		}
	}
	return max
}

func localPathFromURI(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}
	u, err := url.Parse(raw)
	if err != nil || u == nil {
		return raw
	}
	switch strings.ToLower(u.Scheme) {
	case "":
		return raw
	case "file":
		return u.Path
	default:
		return ""
	}
}

func sanitizeTransferName(path string) string {
	replacer := strings.NewReplacer("/", "_", "\\", "_", ":", "_", ".", "_", "{", "", "}", "")
	value := replacer.Replace(strings.TrimSpace(path))
	if value == "" {
		return "transfer"
	}
	return value
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		trimmed := strings.TrimSpace(value)
		if trimmed != "" {
			return trimmed
		}
	}
	return ""
}
