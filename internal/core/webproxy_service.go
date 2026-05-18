package core

import (
	"WantasticCore/internal/errs"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"

	pb "WantasticCore/internal/types"
	"WantasticCore/internal/server"
	"WantasticCore/internal/tenant"
	"WantasticCore/internal/webproxy"
	"WantasticCore/internal/webproxy/wpmux"

	"github.com/gorilla/websocket"
	"github.com/rs/zerolog/log"
)

// WebProxyServiceServer implements the gRPC WebProxyService.
//
// All HTTP and WebSocket traffic flows through the single bidirectional
// StreamHTTP RPC, demultiplexed by request_id via the wpmux package.
// One Mux per StreamHTTP invocation; one Request per logical HTTP request
// or proxied WebSocket. There is no longer a unary fallback — the legacy
// ProxyHTTPRequest RPC and the resource-inliner / URL-rewriter were
// removed in the same change that introduced wpmux.
type WebProxyServiceServer struct {
	srv            *server.Server
	handler        *webproxy.Handler
	tenantRegistry tenant.Registry
}

// NewWebProxyServiceServer creates a new WebProxyService gRPC server.
func NewWebProxyServiceServer(srv *server.Server, handler *webproxy.Handler, tenantRegistry tenant.Registry) *WebProxyServiceServer {
	return &WebProxyServiceServer{
		srv:            srv,
		handler:        handler,
		tenantRegistry: tenantRegistry,
	}
}

// getOverlayAccountID resolves a tenant ID to their overlay account ID.
func (s *WebProxyServiceServer) getOverlayAccountID(tenantID string) (string, error) {
	if s.tenantRegistry == nil {
		return tenantID, nil
	}
	t, err := s.tenantRegistry.GetTenant(tenantID)
	if err != nil {
		return "", fmt.Errorf("tenant not found: %w", err)
	}
	if t.OverlayAccountID == "" {
		return "", fmt.Errorf("tenant has no overlay account")
	}
	return t.OverlayAccountID, nil
}

// CreateWebProxySession creates a new web proxy session for a peer.
func (s *WebProxyServiceServer) CreateWebProxySession(ctx context.Context, req *pb.CreateWebProxySessionRequest) (*pb.CreateWebProxySessionResponse, error) {
	if req.TenantId == "" {
		return nil, errs.InvalidArgumentE("tenant_id is required")
	}
	if req.PeerIp == "" {
		return nil, errs.InvalidArgumentE("peer_ip is required")
	}

	overlayAccountID, err := s.getOverlayAccountID(req.TenantId)
	if err != nil {
		log.Error().Err(err).Str("tenant_id", req.TenantId).Msg("Failed to resolve overlay account ID")
		return nil, errs.NotFoundf("account not found: %v", err)
	}

	port := req.Port
	if port <= 0 {
		if req.UseHttps {
			port = 443
		} else {
			port = 80
		}
	}
	if port > 65535 {
		return nil, errs.InvalidArgumentE("port must be between 1 and 65535")
	}

	peerID := req.PeerId
	if peerID == "" {
		peers, err := s.srv.ListPeers(overlayAccountID)
		if err != nil {
			return nil, errs.Internalf("failed to list peers: %v", err)
		}
		for _, peer := range peers {
			assignedIP := strings.TrimSuffix(peer.AssignedIP, "/32")
			if assignedIP == req.PeerIp {
				peerID = peer.ID
				break
			}
		}
	}

	session, err := s.handler.CreateSession(
		req.TenantId,
		overlayAccountID,
		peerID,
		req.PeerIp,
		int(port),
		req.UseHttps,
		req.SkipTlsVerify,
	)
	if err != nil {
		log.Error().
			Err(err).
			Str("tenant_id", req.TenantId).
			Str("overlay_account_id", overlayAccountID).
			Str("peer_ip", req.PeerIp).
			Int32("port", port).
			Msg("Failed to create WebProxy session")
		return &pb.CreateWebProxySessionResponse{
			Success:      false,
			ErrorMessage: err.Error(),
		}, nil
	}

	log.Debug().
		Str("session_id", session.ID).
		Str("tenant_id", req.TenantId).
		Str("peer_ip", req.PeerIp).
		Int32("port", port).
		Bool("https", req.UseHttps).
		Msg("Created WebProxy session via gRPC")

	if err := s.srv.RegisterWebProxySession(session.ID); err != nil {
		log.Warn().Err(err).Str("session_id", session.ID).Msg("Failed to register WebProxy session in Redis")
	}

	return &pb.CreateWebProxySessionResponse{
		SessionId: session.ID,
		Success:   true,
		BaseUrl:   session.BaseURL,
	}, nil
}

// StreamHTTP is the single entry point for proxied HTTP and WebSocket
// traffic. The first inbound message must carry a session_id so we can
// look up the session and bind the Mux to it.
func (s *WebProxyServiceServer) StreamHTTP(stream BidiStream[*pb.WebProxyStreamMessage, *pb.WebProxyStreamMessage]) error {
	first, err := stream.Recv()
	if err != nil {
		if errors.Is(err, io.EOF) {
			return nil
		}
		return errs.Internalf("failed to receive first message: %v", err)
	}
	if first.SessionId == "" {
		return errs.InvalidArgumentE("session_id is required in first message")
	}
	session, err := s.handler.GetSession(first.SessionId)
	if err != nil {
		return errs.NotFoundf("session not found: %s", first.SessionId)
	}

	ss := newStreamSession(session)
	defer ss.shutdown()

	// replayStream wraps the raw gRPC stream so the first message
	// (already consumed above) is replayed once before normal Recv resumes.
	wrapped := &replayStream{inner: stream, first: first}

	mux := wpmux.New(stream.Context(), wpmux.Config{SessionID: session.ID}, wrapped, wpmux.Handlers{
		OnRequest: ss.handleRequest,
		OnWSFrame: ss.handleWSFrame,
		// Server end never receives Response/Error from the peer in this
		// direction — those flow server→client. Leave nil.
	})

	log.Debug().
		Str("session_id", session.ID).
		Str("tenant_id", session.TenantID).
		Str("peer_ip", session.PeerIP).
		Msg("WebProxy mux stream started")

	return mux.Run()
}

// replayStream lets us peek the first inbound message (to identify the
// session) and then hand the rest of the stream to the Mux untouched.
type replayStream struct {
	inner    BidiStream[*pb.WebProxyStreamMessage, *pb.WebProxyStreamMessage]
	first    *pb.WebProxyStreamMessage
	consumed bool
}

func (r *replayStream) Send(msg *pb.WebProxyStreamMessage) error {
	return r.inner.Send(msg)
}

func (r *replayStream) Recv() (*pb.WebProxyStreamMessage, error) {
	if !r.consumed {
		r.consumed = true
		return r.first, nil
	}
	return r.inner.Recv()
}

func (r *replayStream) Context() context.Context {
	return r.inner.Context()
}

// streamSession holds per-StreamHTTP state: the upstream session, an
// http.Client bound to the session's transport, and the per-request
// WebSocket connections to the backend.
type streamSession struct {
	session  *webproxy.Session
	wsConns  sync.Map // string (request_id) -> *websocket.Conn
	closed   bool
	closedMu sync.Mutex
}

func newStreamSession(session *webproxy.Session) *streamSession {
	return &streamSession{session: session}
}

func (ss *streamSession) shutdown() {
	ss.closedMu.Lock()
	ss.closed = true
	ss.closedMu.Unlock()
	ss.wsConns.Range(func(k, v any) bool {
		if conn, ok := v.(*websocket.Conn); ok {
			conn.Close()
		}
		ss.wsConns.Delete(k)
		return true
	})
}

// handleRequest runs on a fresh goroutine per inbound Request frame.
// On return, the Mux closes the request automatically (final response,
// error, or RST). For WebSocket upgrades we keep the goroutine alive
// until the client cancels the request context.
func (ss *streamSession) handleRequest(req *wpmux.Request, msg *pb.WebProxyRequest) {
	if isWebSocketUpgrade(msg.Headers) {
		ss.handleWebSocketUpgrade(req, msg)
		return
	}
	ss.handleHTTPRequest(req, msg)
}

// handleHTTPRequest performs the upstream HTTP call and streams the
// response back chunk-by-chunk. The first chunk carries headers; later
// chunks carry only body bytes; the last chunk has IsFinal=true.
func (ss *streamSession) handleHTTPRequest(req *wpmux.Request, msg *pb.WebProxyRequest) {
	method := msg.Method
	if method == "" {
		method = "GET"
	}

	// CORS preflight: respond directly with the allowlist, don't bother the
	// backend. Browsers send these before non-simple XHR/fetch requests and
	// most backend admin UIs return 405 / weird CORS, breaking the actual
	// call. Handling it here removes one round-trip and one failure mode.
	if method == http.MethodOptions {
		origin := msg.Headers["Origin"]
		corsHeaders := flattenResponseHeadersWithOrigin(http.Header{}, origin)
		corsHeaders["Access-Control-Max-Age"] = "600"
		corsHeaders["Content-Length"] = "0"
		_ = req.SendResponse(&pb.WebProxyResponse{
			StatusCode: 204,
			StatusText: "No Content",
			Headers:    corsHeaders,
			IsFinal:    true,
		})
		return
	}

	urlStr := ss.session.BaseURL + msg.Path
	if msg.Query != "" {
		urlStr += "?" + msg.Query
	}

	var bodyReader io.Reader
	if len(msg.Body) > 0 {
		bodyReader = bytes.NewReader(msg.Body)
	}

	httpReq, err := http.NewRequestWithContext(req.Context(), method, urlStr, bodyReader)
	if err != nil {
		_ = req.SendError("BAD_REQUEST", err.Error(), false)
		return
	}
	for k, v := range msg.Headers {
		httpReq.Header.Set(k, v)
	}
	if httpReq.Header.Get("User-Agent") == "" {
		httpReq.Header.Set("User-Agent", "WantasticProxy/1.0")
	}

	client := &http.Client{
		Transport: ss.session.Transport(),
		// No client-level timeout — request lifetime is owned by the
		// per-request context, which propagates cancellation from the
		// browser tab/close all the way to the upstream connection.
		CheckRedirect: func(*http.Request, []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}

	resp, err := client.Do(httpReq)
	if err != nil {
		_ = req.SendError(classifyHTTPError(err), err.Error(), httpRetryable(err))
		return
	}
	defer resp.Body.Close()

	// Echo Origin so the response carries credentialed CORS — required for
	// XHR/fetch from the proxied SPA back to its own paths.
	headers := flattenResponseHeadersWithOrigin(resp.Header, msg.Headers["Origin"])
	contentType := resp.Header.Get("Content-Type")

	const chunkSize = 64 * 1024
	buf := make([]byte, chunkSize)
	chunkIdx := int32(0)
	sentHeaders := false

	for {
		n, rerr := resp.Body.Read(buf)
		atEOF := errors.Is(rerr, io.EOF)
		if rerr != nil && !atEOF {
			// I/O error mid-body. If we never sent headers, this becomes
			// a single error chunk; otherwise the client sees an error
			// frame on the open response stream.
			_ = req.SendError("RESPONSE_READ", rerr.Error(), false)
			return
		}

		// Emit a chunk if we have body bytes OR this is the final
		// (zero-byte) frame to close the stream cleanly.
		if n > 0 || atEOF {
			out := &pb.WebProxyResponse{
				Body:       append([]byte(nil), buf[:n]...),
				ChunkIndex: chunkIdx,
				IsFinal:    atEOF,
			}
			if !sentHeaders {
				out.StatusCode = int32(resp.StatusCode)
				out.StatusText = resp.Status
				out.Headers = headers
				out.ContentType = contentType
				out.ContentLength = resp.ContentLength
				sentHeaders = true
			}
			if err := req.SendResponse(out); err != nil {
				return
			}
			chunkIdx++
		}
		if atEOF {
			return
		}
	}
}

// handleWebSocketUpgrade dials the upstream WebSocket via the session's
// transport, forwards the 101 response back to the client, and starts a
// pump goroutine that copies backend frames onto the mux. It blocks
// until the request's context is cancelled (browser tab closed, RST,
// or stream shutdown), then closes the upstream conn.
func (ss *streamSession) handleWebSocketUpgrade(req *wpmux.Request, msg *pb.WebProxyRequest) {
	scheme := "ws"
	if ss.session.UseHTTPS {
		scheme = "wss"
	}
	urlStr := fmt.Sprintf("%s://%s:%d%s", scheme, ss.session.PeerIP, ss.session.Port, msg.Path)
	if msg.Query != "" {
		urlStr += "?" + msg.Query
	}

	transport := ss.session.Transport()
	dialer := &websocket.Dialer{
		NetDialContext:   transport.DialContext,
		TLSClientConfig:  transport.TLSClientConfig,
		HandshakeTimeout: 10 * time.Second,
		ReadBufferSize:   webproxy.ChunkSize,
		WriteBufferSize:  webproxy.ChunkSize,
	}
	reqHeaders := http.Header{}
	for k, v := range msg.Headers {
		reqHeaders.Set(k, v)
	}

	conn, resp, err := dialer.DialContext(req.Context(), urlStr, reqHeaders)
	if err != nil {
		_ = req.SendError("WEBSOCKET_ERROR", err.Error(), false)
		return
	}

	ss.wsConns.Store(req.ID, conn)

	respHeaders := map[string]string{}
	if resp != nil {
		respHeaders = flattenResponseHeaders(resp.Header)
	}
	if err := req.SendResponse(&pb.WebProxyResponse{
		StatusCode: 101,
		StatusText: "Switching Protocols",
		Headers:    respHeaders,
		IsFinal:    true,
	}); err != nil {
		conn.Close()
		ss.wsConns.Delete(req.ID)
		return
	}

	// Backend → mux pump runs on its own goroutine so this handler can
	// keep ownership of the request lifetime. When the request context
	// is cancelled, conn.Close() unblocks the pump's ReadMessage.
	pumpDone := make(chan struct{})
	go func() {
		defer close(pumpDone)
		ss.pumpBackendWebSocket(req, conn)
	}()

	<-req.Context().Done()
	conn.Close()
	<-pumpDone
	ss.wsConns.Delete(req.ID)
}

func (ss *streamSession) pumpBackendWebSocket(req *wpmux.Request, conn *websocket.Conn) {
	for {
		msgType, data, err := conn.ReadMessage()
		if err != nil {
			return
		}
		if err := req.SendWSFrame(&pb.WebProxyWebSocketFrame{
			Data: data,
			Type: int32(msgType),
		}); err != nil {
			return
		}
	}
}

// handleWSFrame writes a client-originated frame onto the matching
// backend WebSocket. Frames for unknown request_ids are dropped quietly
// (the backend conn already closed). Write errors close the conn and
// surface as a stream-level error to the client.
func (ss *streamSession) handleWSFrame(req *wpmux.Request, frame *pb.WebProxyWebSocketFrame) {
	v, ok := ss.wsConns.Load(req.ID)
	if !ok {
		return
	}
	conn := v.(*websocket.Conn)
	if err := conn.WriteMessage(int(frame.Type), frame.Data); err != nil {
		conn.Close()
		ss.wsConns.Delete(req.ID)
		_ = req.SendError("WEBSOCKET_WRITE_ERROR", err.Error(), false)
	}
}

// flattenResponseHeaders converts net/http multi-value headers into the
// proto's map<string,string>. Multi-valued keys (e.g. Set-Cookie) get
// their values joined with "\n" — the browser side splits on "\n" to
// recover the original list. Comma is unsafe because cookie expiry
// dates contain commas.
//
// Dynamic-webapp adjustments applied to every response:
//
//   - Cross-origin: many SPAs proxied through here are served on
//     `<portal>/webproxy/<session>/…` but call `fetch("/api/foo")` against
//     their own backend. We inject permissive CORS so the browser doesn't
//     reject those calls. Credentials=true lets cookies flow through, with
//     Access-Control-Allow-Origin echoed from the request's Origin header
//     (wildcards are incompatible with credentials).
//
//   - Set-Cookie attributes: the backend writes `Domain=10.0.0.5` (its
//     internal LAN address) which the browser silently drops on a
//     `portal.example.com` page. We strip Domain= so cookies bind to the
//     portal host instead. Same for `Secure` flags on plain-HTTP backends —
//     they'd be valid since the portal terminates TLS upstream.
//
//   - Content-Security-Policy: `frame-ancestors`, `frame-src`, and
//     `default-src 'none'` block the proxy iframe. The minimal-safe fix is
//     to strip those directives — leaves the rest of the CSP intact for the
//     embedded app, but lets the portal frame it.
func flattenResponseHeaders(h http.Header) map[string]string {
	return flattenResponseHeadersWithOrigin(h, "")
}

func flattenResponseHeadersWithOrigin(h http.Header, requestOrigin string) map[string]string {
	out := make(map[string]string, len(h)+4)

	for k, v := range h {
		if len(v) == 0 {
			continue
		}
		switch http.CanonicalHeaderKey(k) {
		case "Set-Cookie":
			rewritten := make([]string, len(v))
			for i, c := range v {
				rewritten[i] = rewriteSetCookie(c)
			}
			out[k] = strings.Join(rewritten, "\n")
		case "Content-Security-Policy", "Content-Security-Policy-Report-Only":
			out[k] = relaxCSPForFraming(strings.Join(v, ","))
		case "X-Frame-Options":
			// Sole purpose is to deny framing. Drop entirely.
		case "Access-Control-Allow-Origin",
			"Access-Control-Allow-Credentials",
			"Access-Control-Allow-Headers",
			"Access-Control-Allow-Methods",
			"Access-Control-Expose-Headers",
			"Access-Control-Max-Age":
			// We'll re-inject our own values below. Drop the backend's.
		default:
			if len(v) == 1 {
				out[k] = v[0]
			} else {
				out[k] = strings.Join(v, "\n")
			}
		}
	}

	// Inject CORS allowing the caller's origin. Echoing the Origin header
	// (rather than "*") lets us also send Allow-Credentials=true, which
	// most authenticated SPAs need.
	if requestOrigin != "" {
		out["Access-Control-Allow-Origin"] = requestOrigin
		out["Access-Control-Allow-Credentials"] = "true"
		out["Access-Control-Allow-Headers"] = "Authorization, Content-Type, X-Requested-With, X-Csrf-Token"
		out["Access-Control-Allow-Methods"] = "GET, POST, PUT, PATCH, DELETE, OPTIONS"
		out["Access-Control-Expose-Headers"] = "Content-Length, Content-Range, ETag, Last-Modified, Location"
	}

	return out
}

// rewriteSetCookie strips backend-bound attributes from a Set-Cookie value
// so the cookie actually attaches on the portal origin.
//
//   - `Domain=…`  — backend writes its LAN IP/hostname; that domain is not
//     the portal's, so the browser drops the cookie. Strip it; the cookie
//     will default-bind to the portal host.
//   - `Secure`    — kept; the portal is always HTTPS in production.
//   - `SameSite=Strict` — relaxed to `SameSite=None` since the portal frames
//     the app from a different host, and Strict prevents the cookie from
//     being sent on the iframe's initial request.
func rewriteSetCookie(value string) string {
	parts := strings.Split(value, ";")
	out := parts[:0]
	for _, p := range parts {
		trimmed := strings.TrimSpace(p)
		lower := strings.ToLower(trimmed)
		switch {
		case strings.HasPrefix(lower, "domain="):
			// drop — let the cookie default-scope to the portal host
		case strings.HasPrefix(lower, "samesite="):
			out = append(out, " SameSite=None")
		default:
			out = append(out, p)
		}
	}
	// Ensure SameSite=None is present (needed for cookies on a framed
	// cross-origin app). Browsers require Secure with SameSite=None — the
	// portal already terminates TLS, so add Secure too if absent.
	joined := strings.Join(out, ";")
	if !strings.Contains(strings.ToLower(joined), "samesite=") {
		joined += "; SameSite=None"
	}
	if !strings.Contains(strings.ToLower(joined), "secure") {
		joined += "; Secure"
	}
	return joined
}

// relaxCSPForFraming removes directives that would block the portal from
// framing this app: frame-ancestors (controls who can frame us) and
// X-Frame-Options-equivalent expressions. Other directives are preserved
// so the embedded app keeps its own CSP-driven script/style safety.
func relaxCSPForFraming(csp string) string {
	if csp == "" {
		return ""
	}
	out := make([]string, 0)
	for _, dir := range strings.Split(csp, ";") {
		trimmed := strings.TrimSpace(dir)
		if trimmed == "" {
			continue
		}
		lower := strings.ToLower(trimmed)
		// Drop directives that gate framing.
		if strings.HasPrefix(lower, "frame-ancestors") ||
			strings.HasPrefix(lower, "frame-src") {
			continue
		}
		out = append(out, trimmed)
	}
	return strings.Join(out, "; ")
}

func isWebSocketUpgrade(headers map[string]string) bool {
	connection := ""
	upgrade := ""
	for k, v := range headers {
		if strings.EqualFold(k, "Connection") {
			connection = v
		} else if strings.EqualFold(k, "Upgrade") {
			upgrade = v
		}
	}
	return strings.Contains(strings.ToLower(connection), "upgrade") &&
		strings.EqualFold(upgrade, "websocket")
}

func classifyHTTPError(err error) string {
	msg := strings.ToLower(err.Error())
	switch {
	case errors.Is(err, context.Canceled):
		return "CANCELED"
	case errors.Is(err, context.DeadlineExceeded), strings.Contains(msg, "timeout"):
		return "TIMEOUT"
	case strings.Contains(msg, "connection refused"):
		return "CONNECTION_REFUSED"
	case strings.Contains(msg, "tls"), strings.Contains(msg, "certificate"):
		return "TLS_ERROR"
	default:
		return "REQUEST_FAILED"
	}
}

func httpRetryable(err error) bool {
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "timeout") ||
		strings.Contains(msg, "connection refused") ||
		strings.Contains(msg, "connection reset")
}

// GetWebProxySession returns information about a session.
func (s *WebProxyServiceServer) GetWebProxySession(ctx context.Context, req *pb.GetWebProxySessionRequest) (*pb.GetWebProxySessionResponse, error) {
	if req.SessionId == "" {
		return nil, errs.InvalidArgumentE("session_id is required")
	}
	session, err := s.handler.GetSession(req.SessionId)
	if err != nil {
		return nil, errs.NotFoundf("session not found: %s", req.SessionId)
	}
	return &pb.GetWebProxySessionResponse{Session: s.sessionToProto(session)}, nil
}

// ListWebProxySessions lists active sessions for a tenant.
func (s *WebProxyServiceServer) ListWebProxySessions(ctx context.Context, req *pb.ListWebProxySessionsRequest) (*pb.ListWebProxySessionsResponse, error) {
	if req.TenantId == "" {
		return nil, errs.InvalidArgumentE("tenant_id is required")
	}
	sessions := s.handler.ListSessions(req.TenantId)
	protoSessions := make([]*pb.WebProxySession, 0, len(sessions))
	for _, session := range sessions {
		protoSessions = append(protoSessions, s.sessionToProto(session))
	}
	return &pb.ListWebProxySessionsResponse{Sessions: protoSessions}, nil
}

// CloseWebProxySession closes a session and tears down all its streams.
func (s *WebProxyServiceServer) CloseWebProxySession(ctx context.Context, req *pb.CloseWebProxySessionRequest) (*pb.CloseWebProxySessionResponse, error) {
	if req.SessionId == "" {
		return nil, errs.InvalidArgumentE("session_id is required")
	}
	if err := s.handler.CloseSession(req.SessionId); err != nil {
		return &pb.CloseWebProxySessionResponse{
			Success: false,
			Message: err.Error(),
		}, nil
	}
	log.Debug().Str("session_id", req.SessionId).Msg("Closed WebProxy session via gRPC")
	s.srv.UnregisterWebProxySession(req.SessionId)
	return &pb.CloseWebProxySessionResponse{Success: true, Message: "Session closed successfully"}, nil
}

// sessionToProto converts a webproxy.Session to its proto representation.
func (s *WebProxyServiceServer) sessionToProto(session *webproxy.Session) *pb.WebProxySession {
	return &pb.WebProxySession{
		Id:            session.ID,
		TenantId:      session.TenantID,
		PeerId:        session.PeerID,
		PeerIp:        session.PeerIP,
		Port:          int32(session.Port),
		UseHttps:      session.UseHTTPS,
		CreatedAt:     pb.TimestampFromTime(session.CreatedAt),
		LastActive:    pb.TimestampFromTime(session.LastActive),
		Active:        session.Status == webproxy.SessionStatusActive || session.Status == webproxy.SessionStatusIdle,
		RequestsCount: session.RequestsCount,
		BytesSent:     session.BytesSent,
		BytesReceived: session.BytesReceived,
		BaseUrl:       session.BaseURL,
	}
}

// Shutdown gracefully shuts down the service.
func (s *WebProxyServiceServer) Shutdown() {
	log.Debug().Msg("Shutting down WebProxy gRPC service...")
	s.handler.Shutdown()
	log.Debug().Msg("WebProxy gRPC service shutdown complete")
}
