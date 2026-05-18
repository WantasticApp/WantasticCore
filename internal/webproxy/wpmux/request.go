package wpmux

import (
	"context"
	"sync"

	pb "WantasticCore/internal/types"
)

// Request is one virtual stream multiplexed inside a Mux. It owns its
// own context (cancelled when the request finishes for any reason) and
// signals completion via Done.
//
// Request is goroutine-safe — handlers and the read loop touch it
// concurrently, but the only mutable state (signalling done) is guarded.
type Request struct {
	ID  string
	mux *Mux

	ctx    context.Context
	cancel context.CancelFunc

	doneOnce sync.Once
	done     chan struct{}
}

// Context returns a context that is cancelled when this request closes
// for any reason: final response received, error frame, peer RST, or
// Mux shutdown. HTTP handlers should pass this context to their outbound
// requests so cancellation propagates end-to-end.
func (r *Request) Context() context.Context { return r.ctx }

// Done returns a channel that closes once the request finishes. Mirror
// of Context().Done() but cheaper for goroutines that just want to wait.
func (r *Request) Done() <-chan struct{} { return r.done }

// SessionID returns the session ID this request belongs to.
func (r *Request) SessionID() string { return r.mux.sessionID }

// SendRequest sends a Request payload tagged with this request's ID.
// Used by the client end (browser side) to initiate an HTTP request.
func (r *Request) SendRequest(req *pb.WebProxyRequest) error {
	return r.mux.Send(&pb.WebProxyStreamMessage{
		SessionId: r.mux.sessionID,
		RequestId: r.ID,
		Payload:   &pb.WebProxyStreamMessage_Request{Request: req},
	})
}

// SendResponse sends a Response payload (one chunk) tagged with this
// request's ID. Used by the server end to stream response chunks back.
//
// If resp.IsFinal is true, the request is closed locally after the frame
// is enqueued — subsequent SendResponse calls return ErrClosed via the
// context.
func (r *Request) SendResponse(resp *pb.WebProxyResponse) error {
	err := r.mux.Send(&pb.WebProxyStreamMessage{
		SessionId: r.mux.sessionID,
		RequestId: r.ID,
		Payload:   &pb.WebProxyStreamMessage_Response{Response: resp},
	})
	if err == nil && resp.IsFinal {
		r.mux.closeRequest(r.ID)
	}
	return err
}

// SendWSFrame sends a WebSocket frame tagged with this request's ID.
// Used in both directions for proxied WebSockets.
func (r *Request) SendWSFrame(frame *pb.WebProxyWebSocketFrame) error {
	return r.mux.Send(&pb.WebProxyStreamMessage{
		SessionId: r.mux.sessionID,
		RequestId: r.ID,
		Payload:   &pb.WebProxyStreamMessage_WebsocketFrame{WebsocketFrame: frame},
	})
}

// SendError sends an Error frame for this request and closes it locally.
func (r *Request) SendError(code, message string, retryable bool) error {
	return r.mux.SendError(r.ID, code, message, retryable)
}

// Close releases this request's resources without sending any frame.
// Used when local cleanup is needed but the peer has already finished
// (e.g. after seeing IsFinal in a response).
func (r *Request) Close() {
	r.mux.closeRequest(r.ID)
}

// signalDone closes the done channel exactly once. Idempotent — the Mux
// may call it from both shutdown and closeRequest paths.
func (r *Request) signalDone() {
	r.doneOnce.Do(func() { close(r.done) })
}
