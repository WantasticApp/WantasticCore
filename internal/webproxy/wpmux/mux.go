package wpmux

import (
	"context"
	"errors"
	"fmt"
	"io"
	"sync"
	"sync/atomic"
	"time"

	pb "WantasticCore/internal/types"

	"github.com/rs/zerolog/log"
)

// Stream is the underlying bidirectional transport. Both
// pb.WebProxyService_StreamHTTPServer and pb.WebProxyService_StreamHTTPClient
// satisfy this interface, so the same Mux runs on either end of the wire.
type Stream interface {
	Send(*pb.WebProxyStreamMessage) error
	Recv() (*pb.WebProxyStreamMessage, error)
	Context() context.Context
}

// Handlers is the set of callbacks the Mux invokes for incoming frames.
// A nil field means "ignore that kind of frame" — useful for asymmetric
// ends (clients don't expect to receive Request frames; servers don't
// expect to receive Response frames). Handlers run on the read loop
// goroutine for synchronous payloads (Response, WSFrame, Error) and on
// a fresh goroutine for OnRequest, since Request handlers do blocking
// HTTP I/O.
type Handlers struct {
	OnRequest  func(req *Request, msg *pb.WebProxyRequest)
	OnResponse func(req *Request, msg *pb.WebProxyResponse)
	OnWSFrame  func(req *Request, msg *pb.WebProxyWebSocketFrame)
	OnError    func(reqID string, msg *pb.WebProxyError)
	OnClose    func(reqID string)
}

// Mux multiplexes many virtual requests over a single Stream.
// Send is goroutine-safe. The Mux is single-use: call Run once, and once
// it returns the Mux is dead.
type Mux struct {
	sessionID string
	stream    Stream
	handlers  Handlers

	// Bounded send queue. When full, callers block — this is the
	// backpressure path. A slow peer cannot drive memory through the
	// roof; it just makes our handlers wait.
	sendCh chan *pb.WebProxyStreamMessage

	// Cap on concurrent in-flight request handler goroutines. A burst of
	// open requests (e.g. an SPA loading 60 chunks at once) gets serialized
	// past this limit instead of spawning unbounded goroutines.
	requestSlots chan struct{}

	// How long a single Send may block on a full sendCh before we treat
	// the peer as dead and RST the request. Prevents one stalled tab from
	// monopolizing the mux indefinitely.
	sendTimeout time.Duration

	// Active virtual requests keyed by request_id.
	reqs sync.Map // string -> *Request

	ctx    context.Context
	cancel context.CancelFunc

	closed   atomic.Bool
	closeErr atomic.Pointer[error]

	wg sync.WaitGroup
}

// DefaultSendBufferSize is the default bounded queue depth for outbound
// frames. 64 is enough to batch a burst of small frames (header + body
// chunks for one or two responses) without serializing the writers, but
// small enough that a stuck peer surfaces backpressure quickly.
const DefaultSendBufferSize = 64

// DefaultMaxConcurrentRequests caps the number of in-flight handler
// goroutines per mux. Modern SPAs routinely open 30–60 parallel requests
// on first paint (chunks + images + XHRs); 256 leaves room for that while
// preventing a runaway tab from spawning thousands.
const DefaultMaxConcurrentRequests = 256

// DefaultSendTimeout bounds how long a single frame may block on the
// outbound queue before we treat the peer as dead and tear down the
// owning request. Long-poll / SSE handlers should never sit on a single
// Send for this long — if they do, the receiver is gone.
const DefaultSendTimeout = 30 * time.Second

// Config tunes a Mux at creation time.
type Config struct {
	SessionID             string
	SendBufferSize        int           // 0 → DefaultSendBufferSize
	MaxConcurrentRequests int           // 0 → DefaultMaxConcurrentRequests
	SendTimeout           time.Duration // 0 → DefaultSendTimeout
}

// New creates a Mux. The supplied ctx scopes the Mux's lifetime —
// cancelling it (or calling Close) tears down all in-flight requests
// and stops the read/write loops.
func New(ctx context.Context, cfg Config, stream Stream, handlers Handlers) *Mux {
	if cfg.SendBufferSize <= 0 {
		cfg.SendBufferSize = DefaultSendBufferSize
	}
	if cfg.MaxConcurrentRequests <= 0 {
		cfg.MaxConcurrentRequests = DefaultMaxConcurrentRequests
	}
	if cfg.SendTimeout <= 0 {
		cfg.SendTimeout = DefaultSendTimeout
	}
	cctx, cancel := context.WithCancel(ctx)
	return &Mux{
		sessionID:    cfg.SessionID,
		stream:       stream,
		handlers:     handlers,
		sendCh:       make(chan *pb.WebProxyStreamMessage, cfg.SendBufferSize),
		requestSlots: make(chan struct{}, cfg.MaxConcurrentRequests),
		sendTimeout:  cfg.SendTimeout,
		ctx:          cctx,
		cancel:       cancel,
	}
}

// Run starts the read+write loops and blocks until either the stream
// errors, the context is cancelled, or Close is called. Returns the
// cause (nil for clean shutdown).
//
// Caller must invoke Run exactly once per Mux.
func (m *Mux) Run() error {
	m.wg.Add(1)
	go m.writeLoop()

	err := m.readLoop()
	m.shutdown(err)
	m.wg.Wait()

	if stored := m.closeErr.Load(); stored != nil {
		return *stored
	}
	return err
}

// Close requests a graceful shutdown. Safe to call multiple times.
// Returns when the read/write loops have exited.
func (m *Mux) Close() {
	m.shutdown(nil)
	m.wg.Wait()
}

// Send enqueues msg for the writer goroutine. Blocks if the queue is
// full (backpressure) up to sendTimeout, then returns errSendTimeout —
// callers treat that as a fatal signal for the owning request so a
// single stalled receiver can't monopolize the mux indefinitely.
//
// Safe to call from any goroutine.
func (m *Mux) Send(msg *pb.WebProxyStreamMessage) error {
	if m.closed.Load() {
		return errMuxClosed
	}
	if msg.SessionId == "" {
		msg.SessionId = m.sessionID
	}

	// Fast path: queue has room.
	select {
	case <-m.ctx.Done():
		return m.ctx.Err()
	case m.sendCh <- msg:
		return nil
	default:
	}

	// Slow path: queue full. Wait up to sendTimeout.
	timer := time.NewTimer(m.sendTimeout)
	defer timer.Stop()
	select {
	case <-m.ctx.Done():
		return m.ctx.Err()
	case <-timer.C:
		return errSendTimeout
	case m.sendCh <- msg:
		return nil
	}
}

// SendError sends an Error frame for reqID and closes the request locally.
func (m *Mux) SendError(reqID, code, message string, retryable bool) error {
	err := m.Send(&pb.WebProxyStreamMessage{
		SessionId: m.sessionID,
		RequestId: reqID,
		Payload: &pb.WebProxyStreamMessage_Error{
			Error: &pb.WebProxyError{
				Code:      code,
				Message:   message,
				Retryable: retryable,
			},
		},
	})
	m.closeRequest(reqID)
	return err
}

// SendPing sends a ping/pong frame. Used by both ends as a liveness probe.
func (m *Mux) SendPing(reqID string, timestamp int64) error {
	return m.Send(&pb.WebProxyStreamMessage{
		SessionId: m.sessionID,
		RequestId: reqID,
		Payload: &pb.WebProxyStreamMessage_Ping{
			Ping: &pb.WebProxyPing{Timestamp: timestamp},
		},
	})
}

// OpenRequest creates a new client-side virtual stream. Returns the
// Request handle so the caller can later send the Request payload + read
// responses through Handlers.OnResponse.
//
// Used by the client end of the Mux (browser → portal bridge) where
// the local side is initiating requests.
func (m *Mux) OpenRequest(id string) *Request {
	if id == "" {
		// Caller responsibility — callers always have an id.
		// Returning a request with id "" would corrupt the demux table.
		panic("wpmux: OpenRequest requires non-empty id")
	}
	return m.openRequest(id)
}

// SessionID returns the session ID associated with this Mux.
func (m *Mux) SessionID() string { return m.sessionID }

// ── internal ─────────────────────────────────────────────────────────────

var errMuxClosed = errors.New("wpmux: mux closed")

// ErrSendTimeout is returned by Send when the mux's outbound queue stayed
// full for longer than the configured SendTimeout. Callers should treat
// this as terminal for the affected request: the receiving end is either
// gone or so slow that holding the slot starves every other request.
var ErrSendTimeout = errors.New("wpmux: send timeout (peer too slow)")

var errSendTimeout = ErrSendTimeout

func (m *Mux) writeLoop() {
	defer m.wg.Done()
	for {
		select {
		case <-m.ctx.Done():
			return
		case msg, ok := <-m.sendCh:
			if !ok {
				return
			}
			if err := m.stream.Send(msg); err != nil {
				m.shutdown(fmt.Errorf("stream send: %w", err))
				return
			}
		}
	}
}

func (m *Mux) readLoop() error {
	for {
		if err := m.ctx.Err(); err != nil {
			return err
		}
		msg, err := m.stream.Recv()
		if err != nil {
			if errors.Is(err, io.EOF) {
				return nil
			}
			return fmt.Errorf("stream recv: %w", err)
		}
		m.dispatch(msg)
	}
}

// dispatch routes one inbound frame to the right Request and handler.
// Per-frame work is light — heavy lifting (HTTP request execution) runs
// on its own goroutine spawned in OnRequest.
func (m *Mux) dispatch(msg *pb.WebProxyStreamMessage) {
	reqID := msg.RequestId

	switch payload := msg.Payload.(type) {
	case *pb.WebProxyStreamMessage_Request:
		req := m.openRequest(reqID)
		if m.handlers.OnRequest != nil {
			// Bounded concurrency: try to acquire a slot. If saturated, fall
			// back to a blocking acquire — that backpressures the read loop,
			// which in turn stalls the upstream stream until the receiver
			// drains. Better than spawning unbounded goroutines on a flood.
			select {
			case m.requestSlots <- struct{}{}:
			case <-m.ctx.Done():
				return
			default:
				log.Debug().Str("req_id", reqID).Msg("wpmux: request slots saturated — waiting")
				select {
				case m.requestSlots <- struct{}{}:
				case <-m.ctx.Done():
					return
				}
			}
			go func() {
				defer func() {
					m.closeRequest(reqID)
					<-m.requestSlots
				}()
				m.handlers.OnRequest(req, payload.Request)
			}()
		}

	case *pb.WebProxyStreamMessage_Response:
		req, ok := m.lookupRequest(reqID)
		if !ok {
			// Late frame for a closed request — drop quietly.
			return
		}
		if m.handlers.OnResponse != nil {
			m.handlers.OnResponse(req, payload.Response)
		}
		if payload.Response.IsFinal {
			m.closeRequest(reqID)
		}

	case *pb.WebProxyStreamMessage_WebsocketFrame:
		// WS frames may arrive for either a known request (browser-initiated
		// WS upgrade we already saw the open for) or are the first signal
		// of a passive WS receiver — open the request if missing so the
		// handler can register state.
		req := m.openRequest(reqID)
		if m.handlers.OnWSFrame != nil {
			m.handlers.OnWSFrame(req, payload.WebsocketFrame)
		}

	case *pb.WebProxyStreamMessage_Error:
		if m.handlers.OnError != nil {
			m.handlers.OnError(reqID, payload.Error)
		}
		m.closeRequest(reqID)

	case *pb.WebProxyStreamMessage_Ping:
		// Auto-respond. The peer is using pings as a liveness probe.
		if err := m.SendPing(reqID, payload.Ping.Timestamp); err != nil {
			log.Debug().Err(err).Str("req_id", reqID).Msg("wpmux: pong send failed")
		}

	default:
		log.Warn().Str("req_id", reqID).Msg("wpmux: unknown payload type")
	}
}

// openRequest atomically creates-or-fetches a Request for id. If a request
// already exists for id, it is returned (idempotent — handlers may receive
// multiple frames for one logical request).
func (m *Mux) openRequest(id string) *Request {
	if existing, ok := m.reqs.Load(id); ok {
		return existing.(*Request)
	}
	rctx, rcancel := context.WithCancel(m.ctx)
	r := &Request{
		ID:     id,
		mux:    m,
		ctx:    rctx,
		cancel: rcancel,
		done:   make(chan struct{}),
	}
	if existing, loaded := m.reqs.LoadOrStore(id, r); loaded {
		// Lost the race; discard the one we just made.
		rcancel()
		return existing.(*Request)
	}
	return r
}

func (m *Mux) lookupRequest(id string) (*Request, bool) {
	v, ok := m.reqs.Load(id)
	if !ok {
		return nil, false
	}
	return v.(*Request), true
}

// closeRequest removes a Request from the table, cancels its context,
// signals Done, and runs OnClose if configured. Safe to call multiple
// times for the same id.
func (m *Mux) closeRequest(id string) {
	v, loaded := m.reqs.LoadAndDelete(id)
	if !loaded {
		return
	}
	r := v.(*Request)
	r.cancel()
	r.signalDone()
	if m.handlers.OnClose != nil {
		m.handlers.OnClose(id)
	}
}

// shutdown is the single chokepoint for tearing down a Mux. Idempotent.
func (m *Mux) shutdown(cause error) {
	if !m.closed.CompareAndSwap(false, true) {
		return
	}
	if cause != nil && !errors.Is(cause, context.Canceled) {
		stored := cause
		m.closeErr.Store(&stored)
	}
	m.cancel()

	// Close every active request. Done before closing sendCh so
	// blocked Send callers get unstuck via ctx.Err().
	m.reqs.Range(func(k, v any) bool {
		r := v.(*Request)
		r.cancel()
		r.signalDone()
		m.reqs.Delete(k)
		return true
	})

	// Close sendCh once — writer loop drains and exits.
	close(m.sendCh)
}
