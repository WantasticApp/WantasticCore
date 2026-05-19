// Package services — local_stream.go: zero-copy bidirectional stream
// adapter for in-process service calls.
//
// LocalBidiStream is a pair of channels. The "client" side and the "server"
// side share the same channels; a frame written on either side appears on
// the other side WITHOUT a marshal step — we just pass the *pb.Message
// pointer through.
//
// Lifecycle:
//   ls := NewLocalBidiStream[ReqT, RespT](ctx, 64)
//   client := ls.Client()
//   server := ls.Server()
//   go svc.StreamXxx(server)   // run the handler
//   client.Send(...) / client.Recv()
//   When ctx is cancelled, both sides start failing with ctx.Err().

package services

import (
	"context"
	"errors"
	"io"
	"sync"
)

// streamChans holds the two channels backing a LocalBidiStream.
type streamChans[Req, Resp any] struct {
	ctx    context.Context
	cancel context.CancelFunc

	reqs  chan *Req
	resps chan *Resp

	closeOnce sync.Once
}

// LocalBidiStream is the constructor handle. Use .Client() and .Server() to
// obtain the two endpoints. Use .Close() (or cancel the parent context) to
// terminate the stream from outside.
type LocalBidiStream[Req, Resp any] struct {
	*streamChans[Req, Resp]
}

// NewLocalBidiStream builds a paired stream backed by buffered channels.
// bufSize tunes the channel buffer; the SSH/HTTP proxies are bursty so 64
// is a sensible default.
func NewLocalBidiStream[Req, Resp any](parent context.Context, bufSize int) *LocalBidiStream[Req, Resp] {
	if bufSize <= 0 {
		bufSize = 16
	}
	ctx, cancel := context.WithCancel(parent)
	return &LocalBidiStream[Req, Resp]{
		streamChans: &streamChans[Req, Resp]{
			ctx:    ctx,
			cancel: cancel,
			reqs:   make(chan *Req, bufSize),
			resps:  make(chan *Resp, bufSize),
		},
	}
}

// Close releases the stream. After Close, both sides return io.EOF or
// context.Canceled on Send/Recv. Safe to call multiple times.
func (l *LocalBidiStream[Req, Resp]) Close() {
	l.closeOnce.Do(func() {
		l.cancel()
		safeClose(l.reqs)
		safeClose(l.resps)
	})
}

// safeClose closes ch, swallowing the panic that would otherwise fire if
// ch was already closed by a prior CloseSend.
func safeClose[T any](ch chan *T) {
	defer func() { _ = recover() }()
	close(ch)
}

func (l *LocalBidiStream[Req, Resp]) Client() *LocalBidiStreamClient[Req, Resp] {
	return &LocalBidiStreamClient[Req, Resp]{chans: l.streamChans}
}

func (l *LocalBidiStream[Req, Resp]) Server() *LocalBidiStreamServer[Req, Resp] {
	return &LocalBidiStreamServer[Req, Resp]{chans: l.streamChans}
}

// ─────────────────────────────────────────────────────────────────────
// Client side
// ─────────────────────────────────────────────────────────────────────

type LocalBidiStreamClient[Req, Resp any] struct {
	chans *streamChans[Req, Resp]
}

// Send pushes a request. Returns ctx.Err() immediately if ctx is already
// done — even if the buffer has room — so cancellation is a hard stop.
func (c *LocalBidiStreamClient[Req, Resp]) Send(req *Req) error {
	if err := c.chans.ctx.Err(); err != nil {
		return err
	}
	select {
	case <-c.chans.ctx.Done():
		return c.chans.ctx.Err()
	case c.chans.reqs <- req:
		return nil
	}
}

func (c *LocalBidiStreamClient[Req, Resp]) Recv() (*Resp, error) {
	if err := c.chans.ctx.Err(); err != nil {
		return nil, err
	}
	select {
	case <-c.chans.ctx.Done():
		return nil, c.chans.ctx.Err()
	case msg, ok := <-c.chans.resps:
		if !ok {
			return nil, io.EOF
		}
		return msg, nil
	}
}

// CloseSend signals the server that no more requests will be sent. The
// server's next Recv returns io.EOF.
func (c *LocalBidiStreamClient[Req, Resp]) CloseSend() error {
	defer func() { _ = recover() }() // guard against double-close
	close(c.chans.reqs)
	return nil
}

func (c *LocalBidiStreamClient[Req, Resp]) Context() context.Context { return c.chans.ctx }

// ─────────────────────────────────────────────────────────────────────
// Server side
// ─────────────────────────────────────────────────────────────────────

type LocalBidiStreamServer[Req, Resp any] struct {
	chans *streamChans[Req, Resp]
}

func (s *LocalBidiStreamServer[Req, Resp]) Send(resp *Resp) error {
	if err := s.chans.ctx.Err(); err != nil {
		return err
	}
	select {
	case <-s.chans.ctx.Done():
		return s.chans.ctx.Err()
	case s.chans.resps <- resp:
		return nil
	}
}

func (s *LocalBidiStreamServer[Req, Resp]) Recv() (*Req, error) {
	if err := s.chans.ctx.Err(); err != nil {
		return nil, err
	}
	select {
	case <-s.chans.ctx.Done():
		return nil, s.chans.ctx.Err()
	case msg, ok := <-s.chans.reqs:
		if !ok {
			return nil, io.EOF
		}
		return msg, nil
	}
}

func (s *LocalBidiStreamServer[Req, Resp]) Context() context.Context { return s.chans.ctx }

var errLocalUnsupported = errors.New("local stream: not supported")

// ─────────────────────────────────────────────────────────────────────
// Server-streaming variant
// ─────────────────────────────────────────────────────────────────────

// LocalServerStream is the server-streaming sibling of LocalBidiStream:
// one Send direction (server → client), no request channel. Used for
// RPCs like StreamPingTenantPeer and StreamPortScanStatus.
type LocalServerStream[Resp any] struct {
	ctx       context.Context
	cancel    context.CancelFunc
	ch        chan *Resp
	closeOnce sync.Once
}

func NewLocalServerStream[Resp any](parent context.Context, bufSize int) *LocalServerStream[Resp] {
	if bufSize <= 0 {
		bufSize = 16
	}
	ctx, cancel := context.WithCancel(parent)
	return &LocalServerStream[Resp]{
		ctx:    ctx,
		cancel: cancel,
		ch:     make(chan *Resp, bufSize),
	}
}

func (s *LocalServerStream[Resp]) Close() {
	s.closeOnce.Do(func() {
		s.cancel()
		defer func() { _ = recover() }()
		close(s.ch)
	})
}

func (s *LocalServerStream[Resp]) Send(resp *Resp) error {
	if err := s.ctx.Err(); err != nil {
		return err
	}
	select {
	case <-s.ctx.Done():
		return s.ctx.Err()
	case s.ch <- resp:
		return nil
	}
}

func (s *LocalServerStream[Resp]) Recv() (*Resp, error) {
	if err := s.ctx.Err(); err != nil {
		return nil, err
	}
	select {
	case <-s.ctx.Done():
		return nil, s.ctx.Err()
	case msg, ok := <-s.ch:
		if !ok {
			return nil, io.EOF
		}
		return msg, nil
	}
}

func (s *LocalServerStream[Resp]) Context() context.Context { return s.ctx }

var _ = errLocalUnsupported
