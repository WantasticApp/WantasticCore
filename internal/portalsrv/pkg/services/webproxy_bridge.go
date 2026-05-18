// Package services — webproxy_bridge.go: portal-side relay between the
// browser's WebSocket and the backend WebProxyService.StreamHTTP RPC.
//
// One WebProxyBridge per webproxy session. The bridge does not understand
// individual requests — multiplexing happens at both endpoints (frontend
// webproxy-mux and backend internal/webproxy/wpmux). It does, however,
// translate between the wire formats: the browser uses a hybrid binary
// envelope (JSON header + raw body bytes) that avoids loading a proto
// library in the browser, while the backend speaks raw proto over gRPC.
//
// Hybrid envelope (browser ↔ portal):
//
//	[version=1:1][frameType:1][sessionIdLen:2 BE][sessionId][headerJSONLen:4 BE][headerJSON][bodyBytes]
//
// frameType is webproxyBinaryFrameClient (0x10) for browser→backend and
// webproxyBinaryFrameServer (0x11) for backend→browser. The session ID
// in the envelope is the WebProxy session ID — used to demux to the
// right bridge on the portal side.
//
// headerJSON carries a small, fixed schema mirroring the proto oneof:
//
//	{
//	  "kind":         "request|response|ws_frame|error|ping",
//	  "request_id":   "...",
//	  // request fields
//	  "method":   "GET", "path": "/", "query": "...", "headers": {...},
//	  // response fields
//	  "status_code": 200, "status_text": "OK", "headers": {...},
//	  "content_type": "text/html", "content_length": 1234,
//	  "is_final": true, "chunk_index": 0,
//	  // ws_frame fields
//	  "ws_type": 1,
//	  // error fields
//	  "code": "...", "message": "...", "retryable": false,
//	  // ping fields
//	  "timestamp": 1234567890
//	}
//
// bodyBytes carries the raw request body, response body chunk, or
// WebSocket frame payload. Empty for ping / error.

package services

import (
	"context"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"sync"
	"sync/atomic"
	"time"

	pb "WantasticCore/internal/types"
	"WantasticCore/internal/auth"

	"github.com/gorilla/websocket"
	"github.com/rs/zerolog/log"
)

const (
	// Binary WebSocket frame types for webproxy. version=1 shared with SSH.
	webproxyBinaryFrameClient byte = 0x10 // browser → portal → backend
	webproxyBinaryFrameServer byte = 0x11 // backend → portal → browser

	bridgeSendBufferSize = 64
	bridgeWriteTimeout   = 10 * time.Second
)

// hybridHeader is the JSON portion of the wire envelope. Fields are
// sparse — only those relevant to a given `kind` are populated. The
// portal serializes this into the matching proto oneof on its way to
// the backend, and back to JSON on its way out to the browser.
type hybridHeader struct {
	Kind      string            `json:"kind"`
	RequestID string            `json:"request_id"`
	// Request
	Method  string            `json:"method,omitempty"`
	Path    string            `json:"path,omitempty"`
	Query   string            `json:"query,omitempty"`
	Headers map[string]string `json:"headers,omitempty"`
	// Response
	StatusCode    int32  `json:"status_code,omitempty"`
	StatusText    string `json:"status_text,omitempty"`
	ContentType   string `json:"content_type,omitempty"`
	ContentLength int64  `json:"content_length,omitempty"`
	IsFinal       bool   `json:"is_final,omitempty"`
	ChunkIndex    int32  `json:"chunk_index,omitempty"`
	// WebSocket frame
	WSType int32 `json:"ws_type,omitempty"`
	// Error
	Code      string `json:"code,omitempty"`
	Message   string `json:"message,omitempty"`
	Retryable bool   `json:"retryable,omitempty"`
	// Ping
	Timestamp int64 `json:"timestamp,omitempty"`
}

// WebProxyBridge owns one gRPC StreamHTTP RPC and shuttles messages
// between it and the browser WebSocket. Safe for concurrent use.
type WebProxyBridge struct {
	sessionID string
	session   *TenantSession
	stream    *LocalBidiStreamClient[pb.WebProxyStreamMessage, pb.WebProxyStreamMessage]

	cancel context.CancelFunc

	sendCh chan *pb.WebProxyStreamMessage

	closed atomic.Bool
	wg     sync.WaitGroup
}

func (p *TenantProxy) openWebProxyBridge(session *TenantSession, webProxySessionID string) (*WebProxyBridge, error) {
	if p.services == nil || p.services.WebProxy == nil {
		return nil, fmt.Errorf("WebProxy service not configured")
	}

	// Build an in-process bidi stream — no proto marshalling between the
	// browser-facing goroutine and the WebProxyService implementation.
	// Frames travel as *pb.WebProxyStreamMessage pointers through channels.
	ctx, cancel := context.WithCancel(context.Background())
	ctx = auth.WithCallContext(ctx, session.applyRoutingCallContext(&auth.CallContext{
		SessionToken:    session.SessionToken,
		OriginIP:        session.IPAddress,
		OriginUserAgent: session.UserAgent,
	}))

	local := NewLocalBidiStream[pb.WebProxyStreamMessage, pb.WebProxyStreamMessage](ctx, bridgeSendBufferSize)

	// Run StreamHTTP on the service implementation. When it returns, close
	// the local stream so the bridge's readLoop sees the termination.
	go func() {
		err := p.services.WebProxy.StreamHTTP(local.Server())
		if err != nil && ctx.Err() == nil {
			log.Warn().Err(err).Str("session_id", webProxySessionID).Msg("WebProxyService.StreamHTTP exited")
		}
		local.Close()
	}()

	b := &WebProxyBridge{
		sessionID: webProxySessionID,
		session:   session,
		stream:    local.Client(),
		cancel:    cancel,
		sendCh:    make(chan *pb.WebProxyStreamMessage, bridgeSendBufferSize),
	}

	b.wg.Add(2)
	go b.writeLoop()
	go b.readLoop(p)

	log.Debug().Str("session_id", webProxySessionID).Msg("WebProxy bridge opened (in-process, zero-copy)")
	return b, nil
}

// SendFrame queues a proto frame for the backend. Blocks if queue is full.
func (b *WebProxyBridge) SendFrame(msg *pb.WebProxyStreamMessage) error {
	if b.closed.Load() {
		return errBridgeClosed
	}
	select {
	case b.sendCh <- msg:
		return nil
	case <-b.stream.Context().Done():
		return b.stream.Context().Err()
	}
}

// Close tears down the bridge. Safe to call multiple times.
func (b *WebProxyBridge) Close() {
	b.shutdown()
	b.wg.Wait()
}

var errBridgeClosed = errors.New("webproxy bridge closed")

func (b *WebProxyBridge) shutdown() {
	if !b.closed.CompareAndSwap(false, true) {
		return
	}
	b.cancel()
	close(b.sendCh)
	_ = b.stream.CloseSend()
}

func (b *WebProxyBridge) writeLoop() {
	defer b.wg.Done()
	for msg := range b.sendCh {
		if err := b.stream.Send(msg); err != nil {
			if !errors.Is(err, context.Canceled) && !errors.Is(err, io.EOF) {
				log.Warn().Err(err).Str("session_id", b.sessionID).Msg("WebProxy bridge: gRPC send failed")
			}
			b.shutdown()
			for range b.sendCh {
			}
			return
		}
	}
}

func (b *WebProxyBridge) readLoop(p *TenantProxy) {
	defer b.wg.Done()
	defer func() {
		p.removeWebProxyBridge(b.session, b.sessionID)
		b.shutdown()
	}()

	for {
		msg, err := b.stream.Recv()
		if err != nil {
			if !errors.Is(err, io.EOF) && !errors.Is(err, context.Canceled) {
				log.Debug().Err(err).Str("session_id", b.sessionID).Msg("WebProxy bridge: gRPC recv ended")
			}
			return
		}
		// Translate proto → hybrid envelope and ship to browser.
		header, body, ok := protoToHybrid(msg)
		if !ok {
			log.Debug().Str("session_id", b.sessionID).Msg("WebProxy bridge: unrecognised proto payload, dropping")
			continue
		}
		headerBytes, err := json.Marshal(header)
		if err != nil {
			log.Error().Err(err).Str("session_id", b.sessionID).Msg("WebProxy bridge: header marshal failed")
			continue
		}
		frame := buildWebProxyBinaryFrame(webproxyBinaryFrameServer, b.sessionID, headerBytes, body)
		if err := b.writeBinaryFrame(frame); err != nil {
			log.Debug().Err(err).Str("session_id", b.sessionID).Msg("WebProxy bridge: WS write failed")
			return
		}
	}
}

func (b *WebProxyBridge) writeBinaryFrame(frame []byte) error {
	b.session.mu.Lock()
	defer b.session.mu.Unlock()
	if b.session.Conn == nil {
		return errors.New("websocket conn nil")
	}
	if err := b.session.Conn.SetWriteDeadline(time.Now().Add(bridgeWriteTimeout)); err != nil {
		return err
	}
	return b.session.Conn.WriteMessage(websocket.BinaryMessage, frame)
}

// buildWebProxyBinaryFrame encodes the hybrid wire envelope:
//
//	[version=1:1][frameType:1][sessionIdLen:2 BE][sessionId][headerJSONLen:4 BE][headerJSON][bodyBytes]
func buildWebProxyBinaryFrame(frameType byte, sessionID string, headerJSON, body []byte) []byte {
	idBytes := []byte(sessionID)
	frame := make([]byte, 4+len(idBytes)+4+len(headerJSON)+len(body))
	frame[0] = sshBinaryFrameVersion
	frame[1] = frameType
	binary.BigEndian.PutUint16(frame[2:4], uint16(len(idBytes)))
	off := 4
	copy(frame[off:], idBytes)
	off += len(idBytes)
	binary.BigEndian.PutUint32(frame[off:off+4], uint32(len(headerJSON)))
	off += 4
	copy(frame[off:], headerJSON)
	off += len(headerJSON)
	copy(frame[off:], body)
	return frame
}

// parseWebProxyBinaryFrame splits an inbound browser frame. Returns
// (sessionID, headerJSON, bodyBytes, ok).
func parseWebProxyBinaryFrame(frame []byte) (string, []byte, []byte, bool) {
	if len(frame) < 4 || frame[0] != sshBinaryFrameVersion || frame[1] != webproxyBinaryFrameClient {
		return "", nil, nil, false
	}
	idLen := int(binary.BigEndian.Uint16(frame[2:4]))
	off := 4
	if off+idLen+4 > len(frame) {
		return "", nil, nil, false
	}
	sessionID := string(frame[off : off+idLen])
	off += idLen
	headerLen := int(binary.BigEndian.Uint32(frame[off : off+4]))
	off += 4
	if off+headerLen > len(frame) {
		return "", nil, nil, false
	}
	header := frame[off : off+headerLen]
	body := frame[off+headerLen:]
	return sessionID, header, body, true
}

// handleWebProxyBinaryFrame routes a browser-originated webproxy frame
// to the matching bridge, translating hybrid → proto along the way.
func (p *TenantProxy) handleWebProxyBinaryFrame(session *TenantSession, frame []byte) {
	if session.TenantID == "" {
		return
	}
	sessionID, headerJSON, body, ok := parseWebProxyBinaryFrame(frame)
	if !ok {
		log.Debug().Msg("WebProxy: malformed binary frame")
		return
	}

	var h hybridHeader
	if err := json.Unmarshal(headerJSON, &h); err != nil {
		log.Debug().Err(err).Str("session_id", sessionID).Msg("WebProxy: header decode failed")
		return
	}

	msg := hybridToProto(sessionID, &h, body)
	if msg == nil {
		log.Debug().Str("kind", h.Kind).Str("session_id", sessionID).Msg("WebProxy: unrecognised kind, dropping")
		return
	}

	bridge := p.getOrOpenWebProxyBridge(session, sessionID)
	if bridge == nil {
		return
	}
	if err := bridge.SendFrame(msg); err != nil {
		log.Debug().Err(err).Str("session_id", sessionID).Msg("WebProxy: forward to backend failed")
	}
}

// hybridToProto builds a proto WebProxyStreamMessage from a parsed
// browser frame. Returns nil if the kind is unknown.
func hybridToProto(sessionID string, h *hybridHeader, body []byte) *pb.WebProxyStreamMessage {
	out := &pb.WebProxyStreamMessage{
		SessionId: sessionID,
		RequestId: h.RequestID,
	}
	switch h.Kind {
	case "request":
		out.Payload = &pb.WebProxyStreamMessage_Request{
			Request: &pb.WebProxyRequest{
				Method:  h.Method,
				Path:    h.Path,
				Query:   h.Query,
				Headers: h.Headers,
				Body:    body,
			},
		}
	case "ws_frame":
		out.Payload = &pb.WebProxyStreamMessage_WebsocketFrame{
			WebsocketFrame: &pb.WebProxyWebSocketFrame{
				Data: body,
				Type: h.WSType,
			},
		}
	case "ping":
		out.Payload = &pb.WebProxyStreamMessage_Ping{
			Ping: &pb.WebProxyPing{Timestamp: h.Timestamp},
		}
	default:
		return nil
	}
	return out
}

// protoToHybrid splits a backend proto message into (header, body, ok).
// ok is false for unrecognised oneof variants.
func protoToHybrid(msg *pb.WebProxyStreamMessage) (*hybridHeader, []byte, bool) {
	h := &hybridHeader{RequestID: msg.RequestId}
	switch payload := msg.Payload.(type) {
	case *pb.WebProxyStreamMessage_Response:
		h.Kind = "response"
		h.StatusCode = payload.Response.StatusCode
		h.StatusText = payload.Response.StatusText
		h.Headers = payload.Response.Headers
		h.ContentType = payload.Response.ContentType
		h.ContentLength = payload.Response.ContentLength
		h.IsFinal = payload.Response.IsFinal
		h.ChunkIndex = payload.Response.ChunkIndex
		return h, payload.Response.Body, true
	case *pb.WebProxyStreamMessage_WebsocketFrame:
		h.Kind = "ws_frame"
		h.WSType = payload.WebsocketFrame.Type
		return h, payload.WebsocketFrame.Data, true
	case *pb.WebProxyStreamMessage_Error:
		h.Kind = "error"
		h.Code = payload.Error.Code
		h.Message = payload.Error.Message
		h.Retryable = payload.Error.Retryable
		return h, nil, true
	case *pb.WebProxyStreamMessage_Ping:
		h.Kind = "ping"
		h.Timestamp = payload.Ping.Timestamp
		return h, nil, true
	default:
		return nil, nil, false
	}
}

func (p *TenantProxy) getOrOpenWebProxyBridge(session *TenantSession, webProxySessionID string) *WebProxyBridge {
	session.webProxyBridgesMu.RLock()
	if b, ok := session.webProxyBridges[webProxySessionID]; ok {
		session.webProxyBridgesMu.RUnlock()
		return b
	}
	session.webProxyBridgesMu.RUnlock()

	session.webProxyBridgesMu.Lock()
	defer session.webProxyBridgesMu.Unlock()
	if b, ok := session.webProxyBridges[webProxySessionID]; ok {
		return b
	}
	bridge, err := p.openWebProxyBridge(session, webProxySessionID)
	if err != nil {
		log.Error().Err(err).Str("session_id", webProxySessionID).Msg("WebProxy: bridge open failed")
		return nil
	}
	session.webProxyBridges[webProxySessionID] = bridge
	return bridge
}

func (p *TenantProxy) removeWebProxyBridge(session *TenantSession, webProxySessionID string) {
	session.webProxyBridgesMu.Lock()
	defer session.webProxyBridgesMu.Unlock()
	delete(session.webProxyBridges, webProxySessionID)
}

func (p *TenantProxy) closeAllWebProxyBridges(session *TenantSession) {
	session.webProxyBridgesMu.Lock()
	bridges := make([]*WebProxyBridge, 0, len(session.webProxyBridges))
	for id, b := range session.webProxyBridges {
		bridges = append(bridges, b)
		delete(session.webProxyBridges, id)
	}
	session.webProxyBridgesMu.Unlock()

	for _, b := range bridges {
		b.Close()
	}
}

func (p *TenantProxy) closeWebProxyBridge(session *TenantSession, webProxySessionID string) {
	session.webProxyBridgesMu.Lock()
	bridge, ok := session.webProxyBridges[webProxySessionID]
	if ok {
		delete(session.webProxyBridges, webProxySessionID)
	}
	session.webProxyBridgesMu.Unlock()
	if ok && bridge != nil {
		bridge.Close()
	}
}
