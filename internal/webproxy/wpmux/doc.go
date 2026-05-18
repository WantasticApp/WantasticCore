// Package wpmux multiplexes many concurrent virtual requests over a single
// bidirectional gRPC stream for the webproxy.
//
// Architecture:
//
//	browser <--WebSocket--> portal bridge <--gRPC bidi--> backend handler <--HTTP--> device
//	                          |                              |
//	                          +------- one Mux per --------+
//	                                  browser session
//
// One Mux owns one Stream. Within that Mux, each in-flight HTTP request
// or proxied WebSocket is a Request — a virtual stream identified by the
// request_id field already present on every WebProxyStreamMessage. The Mux
// serializes all writes through a single goroutine fed by a bounded channel
// (this is the backpressure mechanism — a slow peer blocks senders rather
// than blowing memory). Reads are dispatched by request_id to per-request
// handler callbacks supplied at construction time.
//
// Request lifetime is bounded by:
//   - the parent Mux context (cancellation tears down all requests),
//   - the request's own context (cancelled on RST, error, or final response),
//   - the underlying stream returning io.EOF.
//
// The Mux deliberately does NOT define its own framing — the existing
// WebProxyStreamMessage proto already carries everything we need
// (request_id + oneof payload). What this package adds is correct,
// race-free demultiplexing, single-writer serialization, bounded queues,
// and end-to-end cancellation propagation.
package wpmux
