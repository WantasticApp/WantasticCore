// Package webproxy provides HTTP/HTTPS proxying for remote peer web interfaces.
// It allows tenants to browse web interfaces on peers through the WireGuard
// overlay network. The transport multiplexer lives in webproxy/wpmux.
package webproxy

import (
	"context"
	"crypto/tls"
	"fmt"
	"net"
	"net/http"
	"sync"
	"sync/atomic"
	"time"

	"WantasticCore/internal/cache"
	"WantasticCore/internal/wg/userspace"

	"github.com/google/uuid"
	"github.com/rs/zerolog/log"
	"golang.org/x/net/http2"
)

const (
	// DefaultIdleTimeout is the session idle timeout before cleanup.
	// 2 hours covers users who leave browser tabs open without traffic.
	DefaultIdleTimeout = 2 * time.Hour

	// DefaultMaxSessions is the maximum number of concurrent sessions.
	DefaultMaxSessions = 100

	// CleanupInterval is how often to run the cleanup routine.
	CleanupInterval = 5 * time.Minute

	// ChunkSize is used as the websocket read/write buffer size and for
	// HTTP body streaming chunks.
	ChunkSize = 64 * 1024
)

// SessionStatus represents the state of a proxy session.
type SessionStatus string

const (
	SessionStatusActive       SessionStatus = "active"
	SessionStatusIdle         SessionStatus = "idle"
	SessionStatusDisconnected SessionStatus = "disconnected"
	SessionStatusError        SessionStatus = "error"
)

// Session represents an active web proxy session.
//
// A Session is metadata + a single *http.Transport plumbed through the
// tenant's WireGuard userspace device. The transport supports both
// HTTP/1.1 and HTTP/2 (TLS ALPN), so gRPC, gRPC-Web, SOAP, and ordinary
// HTTP all flow without special-casing. WebSockets reuse the same
// transport's DialContext and TLS config.
//
// All actual request execution lives in the gRPC StreamHTTP handler
// (internal/grpc/webproxy_service.go), not on Session — Session is just
// the lifecycle/policy carrier (auth, idle timeout, ownership, IP).
type Session struct {
	ID            string
	TenantID      string
	PeerID        string
	PeerIP        string
	Port          int
	UseHTTPS      bool
	SkipTLSVerify bool
	BaseURL       string

	transport *http.Transport

	// Counters maintained by callers (the gRPC handler increments
	// these as it streams body chunks). Plain uint64 + atomic ops are
	// sufficient — no other field on Session is mutated after creation
	// except LastActive/Status which already have their own mutex.
	RequestsCount uint64
	BytesSent     uint64
	BytesReceived uint64

	CreatedAt  time.Time
	LastActive time.Time
	Status     SessionStatus

	ctx    context.Context
	cancel context.CancelFunc
	mu     sync.RWMutex

	handler *Handler
}

// Transport returns the *http.Transport bound to this session's WireGuard
// device. Callers should not mutate the returned transport — it is shared
// across all in-flight requests for the session.
func (s *Session) Transport() *http.Transport { return s.transport }

// Touch marks the session as recently active. Called from the gRPC
// handler whenever a request frame arrives or a response chunk is sent.
func (s *Session) Touch() {
	s.mu.Lock()
	s.LastActive = time.Now()
	if s.Status == SessionStatusIdle {
		s.Status = SessionStatusActive
	}
	s.mu.Unlock()
}

// MarkIdle transitions an active session to idle. Called when a request
// completes successfully.
func (s *Session) MarkIdle() {
	s.mu.Lock()
	s.LastActive = time.Now()
	s.Status = SessionStatusIdle
	s.mu.Unlock()
}

// Context returns the session's context, cancelled on Close/Shutdown.
func (s *Session) Context() context.Context { return s.ctx }

// Handler manages web proxy sessions with periodic idle cleanup.
type Handler struct {
	manager  *userspace.UserspaceManager
	sessions map[string]*Session
	mu       sync.RWMutex

	idleTimeout time.Duration
	maxSessions int

	sessionCache *cache.Cache

	cleanupTicker *time.Ticker
	stopCh        chan struct{}
	wg            sync.WaitGroup
	closed        bool

	totalSessions   uint64
	activeSessions  int32
	cleanedSessions uint64
}

// NewHandler creates a new web proxy handler with automatic idle cleanup.
func NewHandler(manager *userspace.UserspaceManager) *Handler {
	h := &Handler{
		manager:       manager,
		sessions:      make(map[string]*Session),
		idleTimeout:   DefaultIdleTimeout,
		maxSessions:   DefaultMaxSessions,
		sessionCache:  cache.NewCacheForType(cache.TypeSession),
		cleanupTicker: time.NewTicker(CleanupInterval),
		stopCh:        make(chan struct{}),
	}
	h.wg.Add(1)
	go h.cleanupLoop()

	log.Debug().
		Dur("idle_timeout", h.idleTimeout).
		Int("max_sessions", h.maxSessions).
		Msg("Initialized WebProxy handler")
	return h
}

func (h *Handler) cleanupLoop() {
	defer h.wg.Done()
	for {
		select {
		case <-h.stopCh:
			return
		case <-h.cleanupTicker.C:
			h.cleanupIdleSessions()
		}
	}
}

func (h *Handler) cleanupIdleSessions() {
	h.mu.Lock()
	defer h.mu.Unlock()

	now := time.Now()
	var toRemove []string

	for id, session := range h.sessions {
		session.mu.RLock()
		idleTime := now.Sub(session.LastActive)
		st := session.Status
		session.mu.RUnlock()

		shouldCleanup := false
		reason := ""
		switch {
		case idleTime > h.idleTimeout:
			shouldCleanup = true
			reason = fmt.Sprintf("idle for %v", idleTime.Round(time.Second))
		case st == SessionStatusError && idleTime > time.Minute:
			shouldCleanup = true
			reason = "error state timeout"
		case session.ctx.Err() != nil:
			shouldCleanup = true
			reason = "context cancelled"
		}

		if shouldCleanup {
			toRemove = append(toRemove, id)
			log.Debug().
				Str("session_id", id).
				Str("tenant_id", session.TenantID).
				Str("peer_ip", session.PeerIP).
				Str("reason", reason).
				Dur("idle_time", idleTime).
				Msg("Cleaning up WebProxy session")
		}
	}

	for _, id := range toRemove {
		if session, exists := h.sessions[id]; exists {
			h.closeSessionLocked(session)
			delete(h.sessions, id)
			atomic.AddUint64(&h.cleanedSessions, 1)
			atomic.AddInt32(&h.activeSessions, -1)
		}
	}

	if len(toRemove) > 0 {
		log.Debug().
			Int("cleaned", len(toRemove)).
			Int("remaining", len(h.sessions)).
			Msg("WebProxy cleanup completed")
	}
}

func (h *Handler) closeSessionLocked(session *Session) {
	session.mu.Lock()
	defer session.mu.Unlock()
	if session.cancel != nil {
		session.cancel()
	}
	if session.transport != nil {
		session.transport.CloseIdleConnections()
	}
	session.Status = SessionStatusDisconnected

	log.Debug().
		Str("session_id", session.ID).
		Uint64("requests", atomic.LoadUint64(&session.RequestsCount)).
		Uint64("bytes_sent", atomic.LoadUint64(&session.BytesSent)).
		Uint64("bytes_received", atomic.LoadUint64(&session.BytesReceived)).
		Msg("WebProxy session closed")
}

// CreateSession builds a new session for a peer. The transport is
// configured for HTTP/2 over TLS via ALPN; non-TLS targets fall back
// to HTTP/1.1 automatically. tenantID is the original tenant ID;
// overlayAccountID is the resolved WireGuard device key.
func (h *Handler) CreateSession(tenantID, overlayAccountID, peerID, peerIP string, port int, useHTTPS, skipTLSVerify bool) (*Session, error) {
	if tenantID == "" {
		return nil, fmt.Errorf("tenant_id is required")
	}
	if overlayAccountID == "" {
		return nil, fmt.Errorf("overlay_account_id is required")
	}
	if peerIP == "" {
		return nil, fmt.Errorf("peer_ip is required")
	}
	if port <= 0 || port > 65535 {
		return nil, fmt.Errorf("invalid port: %d", port)
	}

	if atomic.LoadInt32(&h.activeSessions) >= int32(h.maxSessions) {
		return nil, fmt.Errorf("max sessions limit reached (%d)", h.maxSessions)
	}

	device, err := h.manager.GetDevice(overlayAccountID)
	if err != nil {
		return nil, fmt.Errorf("tenant not found: %w", err)
	}

	scheme := "http"
	if useHTTPS {
		scheme = "https"
	}
	baseURL := fmt.Sprintf("%s://%s:%d", scheme, peerIP, port)

	// Best-effort connectivity probe + auto-HTTPS detection. If we can
	// open a plain TCP conn AND it accepts a TLS handshake within 500ms,
	// flip the session to HTTPS even if the caller asked for plain HTTP.
	probeCtx, probeCancel := context.WithTimeout(context.Background(), 5*time.Second)
	probeConn, err := device.Net.DialContext(probeCtx, "tcp", fmt.Sprintf("%s:%d", peerIP, port))
	probeCancel()
	if err != nil {
		log.Warn().
			Str("tenant_id", tenantID).
			Str("peer_ip", peerIP).
			Int("port", port).
			Err(err).
			Msg("WebProxy connectivity probe failed (continuing)")
	} else {
		if !useHTTPS {
			probeConn.SetDeadline(time.Now().Add(500 * time.Millisecond))
			tlsConn := tls.Client(probeConn, &tls.Config{InsecureSkipVerify: true, ServerName: peerIP})
			if err := tlsConn.Handshake(); err == nil {
				useHTTPS = true
				baseURL = fmt.Sprintf("https://%s:%d", peerIP, port)
				log.Debug().Str("peer_ip", peerIP).Int("port", port).Msg("Auto-detected HTTPS")
			}
		}
		probeConn.Close()
	}

	transport := &http.Transport{
		DialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
			return device.Net.DialContext(ctx, network, addr)
		},
		MaxIdleConns:          32,
		MaxIdleConnsPerHost:   16,
		IdleConnTimeout:       90 * time.Second,
		TLSHandshakeTimeout:   10 * time.Second,
		ExpectContinueTimeout: 1 * time.Second,
		// No ResponseHeaderTimeout — slow upstreams (long-poll, SSE,
		// gRPC streaming) must not be killed at the transport layer.
		// Cancellation is owned by the per-request context.
		DisableCompression: false,
		ForceAttemptHTTP2:  true,
	}
	if useHTTPS {
		transport.TLSClientConfig = &tls.Config{
			InsecureSkipVerify: skipTLSVerify,
			MinVersion:         tls.VersionTLS12,
			NextProtos:         []string{"h2", "http/1.1"},
		}
	}
	// Wire up the explicit HTTP/2 transport so server pushes / sticky
	// streams work; ALPN-negotiated h2 still works through the upgrade.
	if err := http2.ConfigureTransport(transport); err != nil {
		log.Warn().Err(err).Msg("http2.ConfigureTransport failed; falling back to HTTP/1.1")
	}

	sessionID := uuid.New().String()
	sessionCtx, sessionCancel := context.WithCancel(context.Background())

	session := &Session{
		ID:            sessionID,
		TenantID:      tenantID,
		PeerID:        peerID,
		PeerIP:        peerIP,
		Port:          port,
		UseHTTPS:      useHTTPS,
		SkipTLSVerify: skipTLSVerify,
		BaseURL:       baseURL,
		transport:     transport,
		CreatedAt:     time.Now(),
		LastActive:    time.Now(),
		Status:        SessionStatusActive,
		ctx:           sessionCtx,
		cancel:        sessionCancel,
		handler:       h,
	}

	h.mu.Lock()
	for id, existing := range h.sessions {
		if existing.TenantID == tenantID && existing.PeerIP == peerIP && existing.Port == port {
			log.Debug().
				Str("old_session_id", id).
				Str("peer_ip", peerIP).
				Int("port", port).
				Msg("Closing existing WebProxy session for peer")
			h.closeSessionLocked(existing)
			delete(h.sessions, id)
			atomic.AddInt32(&h.activeSessions, -1)
		}
	}
	h.sessions[sessionID] = session
	h.mu.Unlock()

	atomic.AddUint64(&h.totalSessions, 1)
	atomic.AddInt32(&h.activeSessions, 1)

	log.Debug().
		Str("session_id", sessionID).
		Str("tenant_id", tenantID).
		Str("peer_id", peerID).
		Str("peer_ip", peerIP).
		Int("port", port).
		Bool("https", useHTTPS).
		Str("base_url", baseURL).
		Msg("Created WebProxy session")

	return session, nil
}

// GetSession returns a session by ID.
func (h *Handler) GetSession(sessionID string) (*Session, error) {
	h.mu.RLock()
	session, exists := h.sessions[sessionID]
	h.mu.RUnlock()
	if !exists {
		return nil, fmt.Errorf("session not found: %s", sessionID)
	}
	return session, nil
}

// ListSessions returns all sessions for a tenant.
func (h *Handler) ListSessions(tenantID string) []*Session {
	h.mu.RLock()
	defer h.mu.RUnlock()
	var sessions []*Session
	for _, session := range h.sessions {
		if session.TenantID == tenantID {
			sessions = append(sessions, session)
		}
	}
	return sessions
}

// CloseSession closes a session by ID.
func (h *Handler) CloseSession(sessionID string) error {
	h.mu.Lock()
	session, exists := h.sessions[sessionID]
	if !exists {
		h.mu.Unlock()
		return fmt.Errorf("session not found: %s", sessionID)
	}
	h.closeSessionLocked(session)
	delete(h.sessions, sessionID)
	h.mu.Unlock()

	atomic.AddInt32(&h.activeSessions, -1)
	log.Debug().Str("session_id", sessionID).Msg("WebProxy session closed by request")
	return nil
}

// Shutdown gracefully shuts down the handler and all sessions.
func (h *Handler) Shutdown() {
	h.mu.Lock()
	if h.closed {
		h.mu.Unlock()
		return
	}
	h.closed = true
	h.mu.Unlock()

	log.Debug().Msg("Shutting down WebProxy handler...")

	close(h.stopCh)
	h.cleanupTicker.Stop()

	h.mu.Lock()
	for id, session := range h.sessions {
		h.closeSessionLocked(session)
		delete(h.sessions, id)
	}
	h.mu.Unlock()

	h.wg.Wait()

	log.Debug().
		Uint64("total_sessions", atomic.LoadUint64(&h.totalSessions)).
		Uint64("cleaned_sessions", atomic.LoadUint64(&h.cleanedSessions)).
		Msg("WebProxy handler shutdown complete")
}

// Stats returns handler statistics.
func (h *Handler) Stats() map[string]any {
	return map[string]any{
		"total_sessions":   atomic.LoadUint64(&h.totalSessions),
		"active_sessions":  atomic.LoadInt32(&h.activeSessions),
		"cleaned_sessions": atomic.LoadUint64(&h.cleanedSessions),
		"max_sessions":     h.maxSessions,
		"idle_timeout":     h.idleTimeout.String(),
	}
}
