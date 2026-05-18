// Package services provides WebSocket-to-gRPC proxy for web clients.
// This file contains the tenant-specific proxy that doesn't require admin auth.
package services

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"os"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"

	pb "WantasticCore/internal/types"
	core "WantasticCore/internal/core"
	"WantasticCore/internal/admin"
	"WantasticCore/internal/copilot"
	"WantasticCore/internal/errs"
	"WantasticCore/internal/portalsrv/pkg/session"
	"WantasticCore/internal/auth"
	"WantasticCore/internal/crypto"
	"WantasticCore/internal/tenant"

	"github.com/gorilla/websocket"
	"github.com/redis/go-redis/v9"
	"github.com/rs/zerolog/log"
	qrcode "github.com/skip2/go-qrcode"
)

// Helper functions
func successResponse(id string, data any) *Response {
	jsonData, _ := json.Marshal(data)
	return &Response{
		ID:       id,
		Type:     "response",
		Response: jsonData,
	}
}

func errorResponse(id string, err error) *Response {
	clientError := sanitizeClientError(err)
	if err != nil && clientError != strings.TrimSpace(err.Error()) {
		log.Warn().
			Str("request_id", id).
			Str("client_error", clientError).
			Err(err).
			Msg("Sanitized websocket error response")
	}
	return &Response{
		ID:    id,
		Type:  "error",
		Error: clientError,
	}
}

func sanitizeClientError(err error) string {
	if err == nil {
		return "An unexpected error occurred. Please try again."
	}

	switch errs.CodeOf(err) {
	case errs.InvalidArgument:
		return sanitizeClientErrorMessage(err.Error(), "Invalid request. Please check your input and try again.")
	case errs.Unauthenticated:
		return "Your session has expired. Please sign in again."
	case errs.PermissionDenied:
		return sanitizeClientErrorMessage(err.Error(), "Access denied.")
	case errs.NotFound:
		return sanitizeClientErrorMessage(err.Error(), "The requested resource was not found.")
	case errs.AlreadyExists:
		return "This resource already exists."
	case errs.FailedPrecondition:
		// Covers quota/limit errors (was codes.ResourceExhausted) plus
		// the generic FailedPrecondition cases.
		return sanitizeClientErrorMessage(err.Error(), "Request cannot be completed in the current state.")
	case errs.Unavailable:
		return "Service is temporarily unavailable. Please try again later."
	case errs.Internal, errs.Unknown:
		return sanitizeClientErrorMessage(err.Error(), "An unexpected error occurred. Please try again.")
	}

	return sanitizeClientErrorMessage(err.Error(), "An unexpected error occurred. Please try again.")
}

func sanitizeClientErrorMessage(message, fallback string) string {
	trimmed := strings.TrimSpace(message)
	if trimmed == "" {
		return fallback
	}

	lower := strings.ToLower(trimmed)

	switch {
	case strings.Contains(lower, "session expired"):
		return "Your session has expired. Please sign in again."
	case strings.Contains(lower, "does not include manage_peers"),
		strings.Contains(lower, "does not include manage_acl"),
		strings.Contains(lower, "share not found or not accepted"):
		return trimmed
	case strings.Contains(lower, "authentication required"),
		strings.Contains(lower, "unauthenticated"):
		return "Authentication required."
	case strings.Contains(lower, "permission denied"),
		strings.Contains(lower, "access denied"),
		strings.Contains(lower, "unauthorized"):
		return "Access denied."
	case strings.Contains(lower, "invalid request"),
		strings.Contains(lower, "invalid input"):
		return "Invalid request. Please check your input and try again."
	case strings.Contains(lower, "totp code required"):
		return "A verification code is required."
	case strings.Contains(lower, "totp secret required"):
		return "A verification secret is required."
	case strings.Contains(lower, "peer limit reached"):
		return "You have reached the maximum number of devices for your plan."
	case strings.Contains(lower, "rate limit"),
		strings.Contains(lower, "too many requests"),
		strings.Contains(lower, "too many login attempts"),
		strings.Contains(lower, "too many registration attempts"),
		strings.Contains(lower, "too many password reset attempts"):
		return "Too many requests. Please wait a moment and try again."
	case strings.Contains(lower, "failed to start ssh stream"),
		strings.Contains(lower, "failed to initialize ssh stream"):
		return "Failed to start SSH stream. Please try again."
	case strings.Contains(lower, "failed to create ssh session"):
		return "Failed to create SSH session. Please try again."
	case strings.Contains(lower, "failed to get ssh session"),
		strings.Contains(lower, "failed to list ssh sessions"),
		strings.Contains(lower, "failed to disconnect ssh session"):
		return "SSH session is temporarily unavailable. Please try again."
	case strings.Contains(lower, "failed to create web proxy session"),
		strings.Contains(lower, "failed to get web proxy session"),
		strings.Contains(lower, "failed to list web proxy sessions"),
		strings.Contains(lower, "failed to verify web proxy session"),
		strings.Contains(lower, "failed to close web proxy session"):
		return "Web access is temporarily unavailable. Please try again."
	case strings.Contains(lower, "all failover attempts failed"),
		strings.Contains(lower, "service unavailable"),
		strings.Contains(lower, "unavailable"),
		strings.Contains(lower, "transport: error while dialing"),
		strings.Contains(lower, "connection refused"),
		strings.Contains(lower, "no core hubs available"),
		strings.Contains(lower, "connection error"):
		return "Service is temporarily unavailable. Please try again later."
	case strings.Contains(lower, "timeout"),
		strings.Contains(lower, "deadline exceeded"):
		return "The request timed out. Please try again."
	case strings.Contains(lower, "not found"):
		return "The requested resource was not found."
	}

	return fallback
}

// parseTierString converts a tier string (e.g., "free", "standard", "premium") to a proto AccountTier enum.
// This is needed because JSON unmarshals tier as a string, but gRPC expects the enum value.
func parseTierString(tierStr string) pb.AccountTier {
	switch tierStr {
	case "free", "TIER_FREE", "0":
		return pb.AccountTier_TIER_FREE
	case "standard", "TIER_STANDARD", "1":
		return pb.AccountTier_TIER_STANDARD
	case "premium", "TIER_PREMIUM", "2":
		return pb.AccountTier_TIER_PREMIUM
	default:
		return pb.AccountTier_TIER_FREE // Default to free
	}
}

// sendError sends an error message to the client (with optional encryption).
func (p *TenantProxy) sendError(session *TenantSession, msgID string, errMsg string) {
	response := &Response{
		ID:    msgID,
		Type:  "error",
		Error: sanitizeClientErrorMessage(errMsg, "An unexpected error occurred. Please try again."),
	}
	p.sendResponse(session, response)
}

// sendResponse sends a response to the client, encrypting if E2EE is enabled.
func (p *TenantProxy) sendResponse(session *TenantSession, response *Response) {
	session.mu.Lock()
	defer session.mu.Unlock()

	session.Conn.SetWriteDeadline(time.Now().Add(30 * time.Second))

	if session.EncryptionEnabled && session.SessionCipher != nil {
		// Encrypt the response
		jsonData, err := json.Marshal(response)
		if err != nil {
			log.Error().Err(err).Str("session_id", session.ID).Msg("Failed to marshal response for encryption")
			return
		}

		ciphertext, err := session.SessionCipher.EncryptJSON(string(jsonData))
		if err != nil {
			log.Error().Err(err).Str("session_id", session.ID).Msg("Failed to encrypt response")
			return
		}

		encryptedMsg := map[string]any{
			"type":       "encrypted",
			"ciphertext": ciphertext,
		}

		if err := session.Conn.WriteJSON(encryptedMsg); err != nil {
			log.Error().Err(err).Str("session_id", session.ID).Msg("Failed to write encrypted response")
		}
		return
	}

	// Unencrypted response (backward compatibility)
	if err := session.Conn.WriteJSON(response); err != nil {
		log.Error().Err(err).Str("session_id", session.ID).Msg("Failed to write response")
	}
}

// Message represents a WebSocket message wrapping gRPC calls.
type Message struct {
	ID      string          `json:"id"`      // Unique request ID for correlation
	Service string          `json:"service"` // gRPC service name (e.g., "AccountService")
	Method  string          `json:"method"`  // Method name (e.g., "ListAccounts")
	Request json.RawMessage `json:"request"` // Request payload as JSON
	Type    string          `json:"type"`    // "request", "response", "error", "stream"
}

// Response represents a WebSocket response message.
type Response struct {
	ID       string          `json:"id"`                 // Correlates with request ID
	Type     string          `json:"type"`               // "response", "error", "stream_data", "stream_end"
	Response json.RawMessage `json:"response,omitempty"` // Response payload as JSON
	Error    string          `json:"error,omitempty"`    // Error message if type=error
}

// Session is a legacy admin-style WebSocket session value referenced by the
// MessageValidator. The fields that touched gRPC have been removed; what
// remains is the minimum the validator needs.
type Session struct {
	ID         string
	Conn       *websocket.Conn
	AuthToken  string
	AccountID  string
	CreatedAt  time.Time
	LastActive time.Time
	mu         sync.Mutex
}

// minInt returns the smaller of two integers.
func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// RateLimitEntry tracks rate limiting per key.
type RateLimitEntry struct {
	Count     int
	WindowEnd time.Time
}

// RateLimiter provides simple rate limiting per key.
type RateLimiter struct {
	entries map[string]*RateLimitEntry
	mu      sync.RWMutex
	limit   int           // Max requests per window
	window  time.Duration // Time window
}

// NewRateLimiter creates a new rate limiter.
func NewRateLimiter(limit int, window time.Duration) *RateLimiter {
	rl := &RateLimiter{
		entries: make(map[string]*RateLimitEntry),
		limit:   limit,
		window:  window,
	}
	// Start cleanup goroutine
	go rl.cleanup()
	return rl
}

// Allow checks if a request is allowed for the given key.
// Returns (allowed, remaining, retryAfterSeconds).
func (rl *RateLimiter) Allow(key string) (bool, int, int) {
	rl.mu.Lock()
	defer rl.mu.Unlock()

	now := time.Now()
	entry, exists := rl.entries[key]

	if !exists || now.After(entry.WindowEnd) {
		// New window
		rl.entries[key] = &RateLimitEntry{
			Count:     1,
			WindowEnd: now.Add(rl.window),
		}
		return true, rl.limit - 1, 0
	}

	if entry.Count >= rl.limit {
		retryAfter := int(entry.WindowEnd.Sub(now).Seconds())
		if retryAfter < 1 {
			retryAfter = 1
		}
		return false, 0, retryAfter
	}

	entry.Count++
	return true, rl.limit - entry.Count, 0
}

// cleanup removes expired entries periodically.
func (rl *RateLimiter) cleanup() {
	ticker := time.NewTicker(5 * time.Minute)
	defer ticker.Stop()

	for range ticker.C {
		rl.mu.Lock()
		now := time.Now()
		for key, entry := range rl.entries {
			if now.After(entry.WindowEnd) {
				delete(rl.entries, key)
			}
		}
		rl.mu.Unlock()
	}
}

// TenantSession represents an active tenant WebSocket session.
type TenantSession struct {
	ID           string
	Conn         *websocket.Conn
	TenantID     string // Associated tenant ID after login (original/own account)
	SessionToken string // gRPC session token
	Email        string
	UserAgent    string // Client user agent for session tracking
	IPAddress    string // Client IP address for session tracking
	Origin       string // HTTP Origin header captured at WS upgrade (used for invite URL generation)
	CreatedAt    time.Time
	LastActive   time.Time
	mu           sync.Mutex
	// End-to-end encryption (E2EE) state
	ServerKeyPair     *crypto.SessionKeyPair // Server's ephemeral X25519 keypair
	ClientPublicKey   string                 // Client's public key (base64)
	SessionCipher     *crypto.SessionCipher  // AES-256-GCM cipher after key exchange
	EncryptionEnabled bool                   // Whether E2EE is active for this session

	// SSH stream state - active SSH streams multiplexed over this WebSocket
	sshStreams   map[string]*SSHStreamHandler
	sshStreamsMu sync.RWMutex

	// RouterOS dashboard streams - one long-lived session per dashboard window.
	routerOSStreams   map[string]*RouterOSStreamHandler
	routerOSStreamsMu sync.RWMutex

	// WebProxy stream state - active WebProxy streams multiplexed over this WebSocket
	// webProxyBridges holds one bridge per active webproxy session.
	// All proxied HTTP/WebSocket traffic for a session multiplexes
	// through its bridge; lifetime is bound to the gRPC StreamHTTP RPC.
	webProxyBridges   map[string]*WebProxyBridge
	webProxyBridgesMu sync.RWMutex

	// AccountID is the resolved UUID for this tenant (may differ from TenantID slug).
	// Set during Redis subscription setup; used for tenant isolation checks on global channels.
	AccountID string

	// Subscribed peers for real-time stats (optimization)
	SubscribedPeers map[string]bool

	// WUSPSubscribedPeers tracks peers for which this session wants live WUSP
	// Notify events. Keyed by WireGuard public key (base64).
	WUSPSubscribedPeers map[string]bool

	// Active WebSocket stream lifetimes keyed by request ID.
	streamCancels   map[string]context.CancelFunc
	streamCancelsMu sync.Mutex

	// Active share context. This is a focused-share route hint for downstream RPCs,
	// not a real account switch in the portal.
	ActiveShareID        string
	ActiveShareTenantID  string                   // Owner tenant ID for focused shared routing
	ActiveShareOwnerName string                   // Display name of share owner (for UI)
	ActiveShareTagFilter []string                 // Peer tag whitelist (empty = all)
	ActiveSharePerms     *tenant.SharePermissions // Cached permissions; nil = own account
}

func (s *TenantSession) registerStream(requestID string, cancel context.CancelFunc) {
	if requestID == "" || cancel == nil {
		return
	}

	s.streamCancelsMu.Lock()
	defer s.streamCancelsMu.Unlock()

	if s.streamCancels == nil {
		s.streamCancels = make(map[string]context.CancelFunc)
	}
	if existing := s.streamCancels[requestID]; existing != nil {
		existing()
	}
	s.streamCancels[requestID] = cancel
}

func (s *TenantSession) unregisterStream(requestID string) {
	if requestID == "" {
		return
	}

	s.streamCancelsMu.Lock()
	defer s.streamCancelsMu.Unlock()

	delete(s.streamCancels, requestID)
}

func (s *TenantSession) cancelAllStreams() {
	s.streamCancelsMu.Lock()
	cancels := make([]context.CancelFunc, 0, len(s.streamCancels))
	for id, cancel := range s.streamCancels {
		if cancel != nil {
			cancels = append(cancels, cancel)
		}
		delete(s.streamCancels, id)
	}
	s.streamCancelsMu.Unlock()

	for _, cancel := range cancels {
		cancel()
	}
}

// GetEffectiveTenantID is a legacy helper kept for the few flows that still
// operate on owner-scoped tenant IDs rather than caller-tenant route hints.
func (s *TenantSession) GetEffectiveTenantID() string {
	if s.ActiveShareTenantID != "" && s.ActiveSharePerms != nil {
		return s.ActiveShareTenantID
	}
	return s.TenantID
}

// IsViewingSharedAccount reports whether the session is currently scoped to a shared account.
func (s *TenantSession) IsViewingSharedAccount() bool {
	return s.ActiveSharePerms != nil
}

// checkSharePerm returns a PermissionDenied error when the session is viewing a shared
// account and the cached share permissions do not grant perm.
// When operating on the user's own account (no active share) it always returns nil.
// Accepts the legacy 9-key names and maps them to the 2-field model.
func (s *TenantSession) checkSharePerm(perm string) error {
	if s.ActiveSharePerms == nil {
		return nil // Own account — full access
	}
	p := s.ActiveSharePerms
	var allowed bool
	switch perm {
	// Read-category operations
	case "view_peers", "view_topology", "view_acl", "view_activity":
		allowed = p.DevicesRead || p.DevicesWrite
	// Write-category operations
	case "manage_peers", "manage_winbox", "manage_webssh", "manage_acl":
		allowed = p.DevicesWrite
	default:
		allowed = false
	}
	if !allowed {
		return errs.PermissionDeniedf("your team access does not include %s", perm)
	}
	return nil
}

// filterPeersByTagFilter removes peers whose tags have no intersection with the session's
// tag filter whitelist.  When the filter is empty every peer is allowed through.
func (s *TenantSession) filterPeersByTagFilter(peers []*pb.Peer) []*pb.Peer {
	if len(s.ActiveShareTagFilter) == 0 {
		return peers
	}
	allowed := make(map[string]bool, len(s.ActiveShareTagFilter))
	for _, t := range s.ActiveShareTagFilter {
		allowed[t] = true
	}
	out := peers[:0]
	for _, p := range peers {
		for _, tag := range p.Tags {
			if allowed[tag] {
				out = append(out, p)
				break
			}
		}
	}
	return out
}

func (s *TenantSession) sharedViewerCanWrite() bool {
	return s.ActiveSharePerms != nil && s.ActiveSharePerms.CanWrite()
}

// applyRoutingCallContext returns a copy of cc with tenant/share routing fields
// populated from this session. Used to construct the auth.CallContext that
// in-process service handlers read.
func (s *TenantSession) applyRoutingCallContext(cc *auth.CallContext) *auth.CallContext {
	if cc == nil {
		cc = &auth.CallContext{}
	}
	if s.TenantID != "" {
		cc.TenantID = s.TenantID
	}
	if s.ActiveShareID != "" && s.ActiveSharePerms != nil {
		cc.FocusedShareID = s.ActiveShareID
		if s.ActiveShareTenantID != "" {
			cc.FocusedOwnerTenantID = s.ActiveShareTenantID
		}
	}
	return cc
}

func (s *TenantSession) enrichPeerForFocusedShare(peer *pb.Peer) {
	if !s.IsViewingSharedAccount() || peer == nil {
		return
	}
	peer.IsShared = true
	peer.OwnerName = s.ActiveShareOwnerName
	peer.ViewerCanWrite = s.sharedViewerCanWrite()
}

func (s *TenantSession) enrichPeerListForFocusedShare(peers []*pb.Peer) {
	if !s.IsViewingSharedAccount() {
		return
	}
	for _, peer := range peers {
		s.enrichPeerForFocusedShare(peer)
	}
}

func (s *TenantSession) needsPeerFocusedShareFallback(peers []*pb.Peer) bool {
	if !s.IsViewingSharedAccount() {
		return false
	}
	for _, peer := range peers {
		if peer == nil {
			continue
		}
		if !peer.IsShared || peer.OwnerName == "" || peer.ViewerCanWrite != s.sharedViewerCanWrite() {
			return true
		}
	}
	return false
}

func (s *TenantSession) enrichWinboxForFocusedShare(sess *pb.WinboxSession) {
	if !s.IsViewingSharedAccount() || sess == nil {
		return
	}
	sess.IsShared = true
	sess.OwnerName = s.ActiveShareOwnerName
	sess.ViewerCanWrite = s.sharedViewerCanWrite()
}

func (s *TenantSession) enrichWinboxListForFocusedShare(sessions []*pb.WinboxSession) {
	if !s.IsViewingSharedAccount() {
		return
	}
	for _, wb := range sessions {
		s.enrichWinboxForFocusedShare(wb)
	}
}

func (s *TenantSession) needsWinboxFocusedShareFallback(sess *pb.WinboxSession) bool {
	if !s.IsViewingSharedAccount() || sess == nil {
		return false
	}
	return !sess.IsShared || sess.OwnerName == "" || sess.ViewerCanWrite != s.sharedViewerCanWrite()
}

func (s *TenantSession) needsWinboxListFocusedShareFallback(sessions []*pb.WinboxSession) bool {
	if !s.IsViewingSharedAccount() {
		return false
	}
	for _, sess := range sessions {
		if s.needsWinboxFocusedShareFallback(sess) {
			return true
		}
	}
	return false
}

func (s *TenantSession) enrichWebSSHForFocusedShare(sess *pb.WebSSHSession) {
	if !s.IsViewingSharedAccount() || sess == nil {
		return
	}
	sess.IsShared = true
	sess.OwnerName = s.ActiveShareOwnerName
	sess.ViewerCanWrite = s.sharedViewerCanWrite()
}

func (s *TenantSession) enrichWebSSHListForFocusedShare(sessions []*pb.WebSSHSession) {
	if !s.IsViewingSharedAccount() {
		return
	}
	for _, ssh := range sessions {
		s.enrichWebSSHForFocusedShare(ssh)
	}
}

func (s *TenantSession) needsWebSSHFocusedShareFallback(sess *pb.WebSSHSession) bool {
	if !s.IsViewingSharedAccount() || sess == nil {
		return false
	}
	return !sess.IsShared || sess.OwnerName == "" || sess.ViewerCanWrite != s.sharedViewerCanWrite()
}

func (s *TenantSession) needsWebSSHListFocusedShareFallback(sessions []*pb.WebSSHSession) bool {
	if !s.IsViewingSharedAccount() {
		return false
	}
	for _, sess := range sessions {
		if s.needsWebSSHFocusedShareFallback(sess) {
			return true
		}
	}
	return false
}

// SSHStreamHandler manages a single SSH stream over WebSocket
type SSHStreamHandler struct {
	sessionID string
	stream    *LocalBidiStreamClient[pb.SSHStreamMessage, pb.SSHStreamMessage]
	cancel    context.CancelFunc
	active    bool
	mu        sync.Mutex                // Protects stream field access
	inputCh   chan *pb.SSHStreamMessage // decouples WebSocket read loop from in-process stream.Send
}

func (h *SSHStreamHandler) canAcceptInput() bool {
	h.mu.Lock()
	defer h.mu.Unlock()

	return h.active && h.stream != nil && h.inputCh != nil
}

const (
	sshProxyOutputBatchDelay    = 2 * time.Millisecond
	sshProxyBatchMaxBytes       = 32 * 1024
	sshProxyInteractiveMaxBytes = 1024 // chunks <= this size are flushed immediately

	// Binary SSH frame constants (must match frontend websocket.ts).
	sshBinaryFrameVersion = 1
	sshBinaryFrameOutput  = 2
)

// TenantSessionStore interface for tenant session validation.
type TenantSessionStore interface {
	ValidateSession(sessionID string) (any, error)
	DeleteSession(sessionID string) error
	ListAPIKeys(tenantID string) ([]*session.APIKey, error)
	CreateAPIKey(tenantID, name, grpcSessionToken string, expiresAt time.Time) (*session.APIKey, error)
	RevokeAPIKey(tenantID, keyID string) error
}

// TenantProxy handles WebSocket-to-gRPC proxying for tenant portal.
// Unlike the admin Proxy, this doesn't require admin auth and is simpler.
type TenantProxy struct {
	grpcAddr     string
	upgrader     websocket.Upgrader
	sessions     map[string]*TenantSession
	sessionsMu   sync.RWMutex
	sessionStore TenantSessionStore // For cookie-based session validation
	validator    *MessageValidator
	redisClient  *redis.Client // Redis client for real-time event subscriptions

	// Rate limiters for abuse prevention
	passwordResetLimiter *RateLimiter // 3 per hour per email/IP
	loginLimiter         *RateLimiter // 10 per minute per IP
	registrationLimiter  *RateLimiter // 5 per hour per IP

	// In-process service router. router is retained for peer-bound helpers
	// (port-scan routing, wuspConn) that historically returned a
	// *grpc.ClientConn but now always resolve to the in-process services.
	router   *InProcessRouter
	services *core.Services
	admin    *admin.Service
	copilot  *copilot.Service

	// wuspSubMu protects wuspSubRefs and wuspSubCancelers below.
	wuspSubMu sync.Mutex
	// wuspSubRefs counts how many sessions are subscribed to live WUSP feed
	// for each peer. The first session to subscribe (0 → 1) triggers a
	// gRPC EnsureWUSPSubscription call; the last to leave (1 → 0) schedules
	// a debounced CancelWUSPSubscription so a quick tab-flip doesn't tear
	// down and rebuild the agent-side Subscribe needlessly.
	wuspSubRefs map[string]int
	// wuspSubCancelers holds the pending unsubscribe timers per peer so a
	// re-subscribe within the debounce window can short-circuit them.
	wuspSubCancelers map[string]*time.Timer

	// Stats
	instanceID string
	ctx        context.Context
	cancel     context.CancelFunc
}

// wuspUnsubscribeDebounce is how long we wait after the last session leaves
// a peer's live feed before we tell the agent to cancel the subscription.
// Long enough to absorb tab switching and dashboard re-mounts, short enough
// that an idle peer's agent isn't sending events into the void forever.
const wuspUnsubscribeDebounce = 30 * time.Second

// addWUSPSubscriber increments the refcount for peerID and, on the 0 → 1
// edge, asks the core to set up the canonical Subscribe with the agent.
// Any pending unsubscribe-debounce timer is cancelled.
//
// accountID is needed by the gRPC EnsureWUSPSubscription, but failures here
// are non-fatal: live push is a best-effort feature on top of the existing
// request/response WS proxy.
func (p *TenantProxy) addWUSPSubscriber(ctx context.Context, accountID, peerID string) {
	p.wuspSubMu.Lock()
	if t, ok := p.wuspSubCancelers[peerID]; ok {
		t.Stop()
		delete(p.wuspSubCancelers, peerID)
	}
	p.wuspSubRefs[peerID]++
	first := p.wuspSubRefs[peerID] == 1
	p.wuspSubMu.Unlock()

	if !first {
		return
	}
	go func() {
		callCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
		defer cancel()
		resp, err := p.services.WUSP.EnsureWUSPSubscription(callCtx, &pb.EnsureWUSPSubscriptionRequest{
			PeerId:    peerID,
			AccountId: accountID,
		})
		if err != nil {
			log.Warn().Err(err).Str("peer_id", peerID).Msg("wusp: EnsureWUSPSubscription failed (live push disabled until next subscribe)")
			return
		}
		if resp != nil && !resp.Success {
			log.Warn().Str("peer_id", peerID).Str("error", resp.Error).Msg("wusp: EnsureWUSPSubscription rejected")
		}
	}()
}

// removeWUSPSubscriber decrements the refcount for peerID and, on the 1 → 0
// edge, schedules a debounced CancelWUSPSubscription. If a new subscriber
// arrives within the debounce window, addWUSPSubscriber will cancel the
// pending timer so the agent-side subscription stays put.
func (p *TenantProxy) removeWUSPSubscriber(accountID, peerID string) {
	p.wuspSubMu.Lock()
	if p.wuspSubRefs[peerID] <= 0 {
		p.wuspSubMu.Unlock()
		return
	}
	p.wuspSubRefs[peerID]--
	last := p.wuspSubRefs[peerID] == 0
	if last {
		delete(p.wuspSubRefs, peerID)
	}
	if last {
		// Schedule debounced cancel. Hold a reference in wuspSubCancelers so
		// addWUSPSubscriber can cancel it if a re-subscribe arrives soon.
		t := time.AfterFunc(wuspUnsubscribeDebounce, func() {
			p.wuspSubMu.Lock()
			// Re-check: did a new subscriber arrive while we slept?
			if p.wuspSubRefs[peerID] > 0 {
				p.wuspSubMu.Unlock()
				return
			}
			delete(p.wuspSubCancelers, peerID)
			p.wuspSubMu.Unlock()

			callCtx, cancel := context.WithTimeout(p.ctx, 5*time.Second)
			defer cancel()
			if _, err := p.services.WUSP.CancelWUSPSubscription(callCtx, &pb.CancelWUSPSubscriptionRequest{
				PeerId:    peerID,
				AccountId: accountID,
			}); err != nil {
				log.Debug().Err(err).Str("peer_id", peerID).Msg("wusp: CancelWUSPSubscription failed (will be GC'd when agent restarts)")
			}
		})
		p.wuspSubCancelers[peerID] = t
	}
	p.wuspSubMu.Unlock()
}

// NewTenantProxy creates a new tenant WebSocket-to-gRPC proxy.
func NewTenantProxy(services *core.Services, router *InProcessRouter, sessionStore TenantSessionStore, redisClient *redis.Client) (*TenantProxy, error) {
	return NewTenantProxyWithTLS(services, router, sessionStore, redisClient)
}

// NewTenantProxyStateless creates a stateless tenant WebSocket-to-gRPC proxy.
func NewTenantProxyStateless(services *core.Services, router *InProcessRouter, redisClient *redis.Client) (*TenantProxy, error) {
	return NewTenantProxyWithTLS(services, router, nil, redisClient)
}

// NewTenantProxyWithTLS creates a new tenant WebSocket-to-gRPC proxy backed
// by the in-process *core.Services bundle. router is retained for peer-bound
// helpers and is always a no-op InProcessRouter in single-process operation.
// SetAdminService wires the in-process super-admin handler. Optional —
// when nil, AdminService calls return "admin service not configured".
func (p *TenantProxy) SetAdminService(svc *admin.Service) {
	p.admin = svc
}

// SetCopilotService wires the in-process Copilot session manager. Optional —
// when nil, CopilotService calls return "copilot not enabled".
func (p *TenantProxy) SetCopilotService(svc *copilot.Service) {
	p.copilot = svc
}

func NewTenantProxyWithTLS(services *core.Services, router *InProcessRouter, sessionStore TenantSessionStore, redisClient *redis.Client) (*TenantProxy, error) {
	if services == nil {
		return nil, fmt.Errorf("services bundle is required")
	}
	if router == nil {
		router = NewInProcessRouter(services)
	}

	// Use tenant-specific validation config
	validatorConfig := DefaultValidationConfig()
	// Add tenant services to allowed services
	validatorConfig.AllowedServices = append(validatorConfig.AllowedServices,
		"TenantRegistrationService",
		"TenantPortalService",
		"TenantBillingService",
		"TenantDataService",
		"RouterOSService",
		"AdminService",
	)
	validatorConfig.ServiceMethods["AdminService"] = []string{
		"ListTenants",
		"GetTenant",
		"CreateTenant",
		"DeleteTenant",
		"SetTenantMaxPeers",
		"SetTenantPassword",
		"SetTenantAdmin",
		"SetTenantStatus",
	}
	validatorConfig.AllowedServices = append(validatorConfig.AllowedServices, "CopilotService")
	validatorConfig.ServiceMethods["CopilotService"] = []string{
		"OpenSession",
		"SendMessage",
		"GetSession",
		"ListSessions",
		"CloseSession",
	}
	// Add tenant service methods
	validatorConfig.ServiceMethods["TenantRegistrationService"] = []string{
		"GetPaymentStatus",
		"GetAvailablePlans",
		"GetAllowedPhoneRegions",
		"StartRegistration",
		"VerifyPhone",
		"ResendPhoneVerification",
		"CompleteRegistration",
		"CreateCheckoutSession",
		"GetRegistrationStatus",
	}
	validatorConfig.ServiceMethods["TenantPortalService"] = []string{
		// Authentication & Profile
		"TenantLogin",
		"Send2FACode",
		"GetTenantDashboard",
		"GetTenantAccount",
		"UpdateTenantProfile",
		"GetTwoFASettings",
		"SetTwoFAMethod",
		// Password recovery (public - no auth required)
		"RequestPasswordReset",
		"VerifyResetCode",
		"ResetPassword",
		// Peer management (tenant-scoped)
		"ListTenantPeers",
		"GetTenantPeer",
		"AddTenantPeer",
		"RemoveTenantPeer",
		"UpdateTenantPeer",
		"GetTenantPeerConfig",
		"GetTenantPeerStats",
		"PingTenantPeer",
		"GetTenantTopology",
		"BatchUpdatePeers",
		// Winbox Management (tenant-scoped)
		"ClearTenantWinboxCredentials",
		"CreateTenantWinboxSession",
		"UpdateTenantWinboxSession",
		"DeleteTenantWinboxSession",
		"ListTenantWinboxSessions",
		"GetTenantWinboxSession",
		// ACL Management (tenant-scoped)
		"CreateTenantPeerGroup",
		"DeleteTenantPeerGroup",
		"ListTenantPeerGroups",
		"AddTenantPeerToGroup",
		"RemoveTenantPeerFromGroup",
		"CreateTenantGroupLink",
		"DeleteTenantGroupLink",
		"AssignExitNode",
		"ListTenantGroupLinks",
		"CompileTenantGroups",
		"GetTenantCompilationStats",
		"AddTenantACLRule",
		"RemoveTenantACLRule",
		"GetTenantACLRules",
		"CheckTenantAccess",
		// WebSSH Management (tenant-scoped)
		"CreateTenantWebSSHSession",
		"GetTenantWebSSHSession",
		"ListTenantWebSSHSessions",
		"DisconnectTenantWebSSHSession",
		// Session Management
		"ListTenantSessions",
		"DeleteTenantSession",
		// Enrollment Tokens
		"ListEnrollmentTokens",
		"CreateEnrollmentToken",
		"DeleteEnrollmentToken",
		"ConfirmDevice",
		// MCP
		"GetMCPConfig",
		"ListAPIKeys",
		"CreateAPIKey",
		"RevokeAPIKey",
	}
	validatorConfig.ServiceMethods["TenantBillingService"] = []string{
		"GetSubscriptionStatus",
		"ChangeTier",
		"GetBillingPortal",
		"CancelSubscription",
		"GetBillingHistory",
		"CreateSetupIntent",
	}
	validatorConfig.ServiceMethods["TenantDataService"] = []string{
		"RequestBackup",
		"ListBackups",
		"GetBackupDownloadURL",
		"RestoreFromBackup",
		"RestoreBackup",
		"DeleteBackup",
		"GetRestoreStatus",
	}
	validatorConfig.ServiceMethods["RouterOSService"] = []string{
		"GetOverview",
		"ListResource",
		"AddResource",
		"UpdateResource",
		"DeleteResource",
	}

	// Add WebProxyService methods
	validatorConfig.AllowedServices = append(validatorConfig.AllowedServices, "WebProxyService")
	validatorConfig.ServiceMethods["WebProxyService"] = []string{
		"CreateWebProxySession",
		"GetWebProxySession",
		"ListWebProxySessions",
		"CloseWebProxySession",
		// Note: StreamHTTP is a streaming method and handled separately.
		// All proxied HTTP requests and WebSockets multiplex over StreamHTTP
		// via internal/webproxy/wpmux.
	}

	// Add PeerService methods (for Port Scan Control)
	validatorConfig.AllowedServices = append(validatorConfig.AllowedServices, "PeerService")
	validatorConfig.ServiceMethods["PeerService"] = []string{
		"StartPortScan",
		"StopPortScan",
		"PausePortScan",
		"ResumePortScan",
		"StreamPortScanStatus",
	}

	p := &TenantProxy{
		sessionStore:     sessionStore,
		validator:        NewMessageValidator(validatorConfig),
		redisClient:      redisClient,
		router:           router,
		services:         services,
		wuspSubRefs:      make(map[string]int),
		wuspSubCancelers: make(map[string]*time.Timer),
		upgrader: websocket.Upgrader{
			CheckOrigin: func(r *http.Request) bool {
				origin := r.Header.Get("Origin")
				host := strings.ToLower(r.Host)

				// Reject missing origin for non-local hosts to reduce CSWSH risk.
				// Browsers send Origin on WS requests; allow empty only for local/dev.
				if origin == "" {
					if strings.HasPrefix(host, "localhost") || strings.HasPrefix(host, "127.0.0.1") {
						return true
					}
					log.Warn().Str("host", host).Msg("Tenant WebSocket origin missing")
					return false
				}

				u, err := url.Parse(origin)
				if err != nil || u.Scheme == "" || u.Host == "" {
					log.Warn().Str("origin", origin).Str("host", host).Msg("Tenant WebSocket origin parse failed")
					return false
				}

				originHost := strings.ToLower(u.Host)
				allowedOrigins := []string{
					"http://" + originHost,
					"https://" + originHost,
					"http://" + host,
					"https://" + host,
					"http://localhost:8001",
					"http://127.0.0.1:8001",
					"http://localhost:8080",
					"http://127.0.0.1:8080",
					"http://localhost:5173",
					"http://127.0.0.1:5173",
				}
				if slices.Contains(allowedOrigins, origin) {
					return true
				}
				log.Warn().Str("origin", origin).Str("host", host).Msg("Tenant WebSocket origin check failed")
				return false
			},
			ReadBufferSize:  4096,
			WriteBufferSize: 4096,
		},
		sessions: make(map[string]*TenantSession),
		// Initialize rate limiters
		passwordResetLimiter: NewRateLimiter(3, time.Hour),    // 3 password resets per hour
		loginLimiter:         NewRateLimiter(10, time.Minute), // 10 login attempts per minute
		registrationLimiter:  NewRateLimiter(5, time.Hour),    // 5 registrations per hour
	}

	// Initialize Instance ID
	hostname, _ := os.Hostname()
	if hostname == "" {
		hostname = "portal-" + uuid.NewString()[:8]
	} else {
		hostname = "portal-" + hostname
	}
	p.instanceID = hostname

	// Initialize context
	p.ctx, p.cancel = context.WithCancel(context.Background())

	// Start stats pulse
	go p.statsPulseWorker()

	return p, nil
}

// tenantPortalSvc returns the in-process TenantPortalService implementation.
// The peerKey is accepted for legacy call-site compatibility but is unused —
// all callers resolve to the single in-process service registry.
func (p *TenantProxy) tenantPortalSvc(peerKey string) core.TenantPortalService {
	_ = peerKey
	return p.services.TenantPortal
}

// GetRouter returns the internal in-process router.
func (p *TenantProxy) GetRouter() *InProcessRouter {
	return p.router
}

// HandleWebSocket handles tenant WebSocket connections.
func (p *TenantProxy) HandleWebSocket(w http.ResponseWriter, r *http.Request) {
	// Check for existing tenant session cookie
	var tenantID string
	var sessionToken string

	cookie, err := r.Cookie("tenant_session")
	if err == nil && cookie.Value != "" {
		log.Debug().Bool("has_tenant_cookie", true).Msg("Tenant session cookie received")

		// STATELESS MODE: Cookie contains the gRPC session token directly
		// Validate via local session store
		if p.sessionStore == nil {
			// No session store configured - skip validation
			log.Debug().Msg("❌ No session store configured")
			sessionToken = ""
		} else {
			// Validate via local session store
			if sess, err := p.sessionStore.ValidateSession(cookie.Value); err == nil && sess != nil {
				log.Debug().Msgf("Session type: %T", sess)
				// Extract tenant info from session if available
				if ts, ok := sess.(interface{ GetTenantID() string }); ok {
					tenantID = ts.GetTenantID()
					log.Debug().Str("tenant_id", tenantID).Msg("Got tenant ID from session")
				} else {
					log.Debug().Msg("Session does not implement GetTenantID")
				}
				// Get the original gRPC session token (not the cookie value)
				if ts, ok := sess.(interface{ GetSessionToken() string }); ok {
					sessionToken = ts.GetSessionToken()
					if sessionToken == "" {
						// PostgreSQL-backed tenant sessions historically stored the
						// session identifier as the auth token. Fall back to it so the
						// shared-access middleware still receives a valid bearer token.
						if tenantSess, ok := sess.(*tenant.TenantSession); ok {
							sessionToken = tenantSess.SessionID
						}
					}
					log.Debug().Bool("has_token", sessionToken != "").Msg("Got session token from session")
				} else {
					log.Debug().Msg("Session does not implement GetSessionToken")
				}
				log.Debug().Str("tenant_id", tenantID).Bool("has_token", sessionToken != "").Msg("Tenant WebSocket authenticated from cookie")
			} else {
				log.Debug().Err(err).Msg("Session validation failed or returned nil")
			}
		}
	} else {
		log.Debug().Err(err).Msg("No tenant_session cookie found")
	}

	conn, err := p.upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Error().Err(err).Msg("Failed to upgrade tenant WebSocket")
		return
	}
	defer conn.Close()

	conn.SetReadLimit(1 << 20) // 1MB
	conn.SetReadDeadline(time.Now().Add(60 * time.Second))
	conn.SetPongHandler(func(string) error {
		conn.SetReadDeadline(time.Now().Add(60 * time.Second))
		return nil
	})

	sessionID := fmt.Sprintf("tenant-ws-%d", time.Now().UnixNano())

	// Capture user agent from HTTP request
	userAgent := r.UserAgent()
	if userAgent == "" {
		// Try case-insensitive lookup
		if ua := r.Header.Get("User-Agent"); ua != "" {
			userAgent = ua
		} else if ua := r.Header.Get("user-agent"); ua != "" {
			userAgent = ua
		} else {
			userAgent = "Unknown Client"
		}
	}

	// Capture IP address (handling proxies and ports)
	ipAddress := ""
	if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
		// X-Forwarded-For can be a comma-separated list, take the first one
		if idx := strings.Index(xff, ","); idx != -1 {
			ipAddress = strings.TrimSpace(xff[:idx])
		} else {
			ipAddress = strings.TrimSpace(xff)
		}
	}
	if ipAddress == "" {
		ipAddress = r.Header.Get("X-Real-IP")
	}
	if ipAddress == "" {
		// Fallback to RemoteAddr (usually contains port, e.g. "127.0.0.1:1234")
		host, _, err := net.SplitHostPort(r.RemoteAddr)
		if err == nil {
			ipAddress = host
		} else {
			ipAddress = r.RemoteAddr
		}
	}

	// Log connection metadata without dumping headers/cookies.
	log.Debug().
		Str("session_id", sessionID).
		Str("user_agent", userAgent).
		Str("ip_address", ipAddress).
		Str("remote_addr", r.RemoteAddr).
		Msg("Captured client info from WebSocket upgrade request")

	// Generate ephemeral X25519 keypair for this session
	serverKeyPair, err := crypto.GenerateSessionKeyPair()
	if err != nil {
		log.Error().Err(err).Msg("Failed to generate session keypair")
		conn.Close()
		return
	}

	// Capture origin for invite URL generation (e.g. "https://console.wantastic.app")
	wsOrigin := r.Header.Get("Origin")
	if wsOrigin == "" {
		// Fallback: reconstruct from Host header
		scheme := "https"
		if strings.HasPrefix(r.Host, "localhost") || strings.HasPrefix(r.Host, "127.") {
			scheme = "http"
		}
		wsOrigin = scheme + "://" + r.Host
	}

	session := &TenantSession{
		ID:              sessionID,
		Conn:            conn,
		TenantID:        tenantID,
		SessionToken:    sessionToken,
		UserAgent:       userAgent,
		IPAddress:       ipAddress,
		Origin:          wsOrigin,
		CreatedAt:       time.Now(),
		LastActive:      time.Now(),
		ServerKeyPair:   serverKeyPair,
		sshStreams:      make(map[string]*SSHStreamHandler),
		routerOSStreams: make(map[string]*RouterOSStreamHandler),
		webProxyBridges: make(map[string]*WebProxyBridge),
		SubscribedPeers: make(map[string]bool),
		streamCancels:   make(map[string]context.CancelFunc),
	}

	p.sessionsMu.Lock()
	p.sessions[sessionID] = session
	p.sessionsMu.Unlock()

	defer func() {
		p.cleanupAllSSHStreams(session)
		p.cleanupAllRouterOSStreams(session)
		p.closeAllWebProxyBridges(session)
		session.cancelAllStreams()
		// Release any WUSP live-feed subscriptions this session held so the
		// proxy refcount doesn't leak. The 30 s debounce inside removeWUSPSubscriber
		// absorbs the page-reload case (browser refresh re-opens WS within ~1 s).
		session.mu.Lock()
		var subscribedPeers []string
		for peer := range session.WUSPSubscribedPeers {
			subscribedPeers = append(subscribedPeers, peer)
		}
		session.WUSPSubscribedPeers = nil
		tenantID := session.TenantID
		session.mu.Unlock()
		for _, peer := range subscribedPeers {
			p.removeWUSPSubscriber(tenantID, peer)
		}
		p.sessionsMu.Lock()
		delete(p.sessions, sessionID)
		p.sessionsMu.Unlock()
	}()

	log.Debug().Str("session_id", sessionID).Str("tenant_id", tenantID).Msg(" Tenant WebSocket session started")

	// Start Redis subscription if tenant ID is present
	if tenantID != "" && p.redisClient != nil {
		go p.subscribeToRedisEvents(session)
	}

	// Send key exchange initiation to client
	keyExchangeMsg := map[string]any{
		"type":              "key_exchange",
		"server_public_key": serverKeyPair.PublicKeyBase64(),
		"session_id":        sessionID,
	}
	session.mu.Lock()
	conn.SetWriteDeadline(time.Now().Add(10 * time.Second))
	if err := conn.WriteJSON(keyExchangeMsg); err != nil {
		log.Error().Err(err).Str("session_id", sessionID).Msg("Failed to send key exchange")
		session.mu.Unlock()
		return
	}
	session.mu.Unlock()
	log.Debug().Str("session_id", sessionID).Msg(" Sent key exchange initiation")

	go p.keepAlive(conn, session)

	for {
		conn.SetReadDeadline(time.Now().Add(60 * time.Second))

		// Read raw message (may be JSON or binary SSH frame)
		wsMsgType, rawMsg, err := conn.ReadMessage()
		if err != nil {
			if websocket.IsUnexpectedCloseError(err, websocket.CloseGoingAway, websocket.CloseAbnormalClosure) {
				log.Error().Err(err).Str("session_id", sessionID).Msg("❌ Tenant WebSocket read error")
			} else {
				log.Debug().Err(err).Str("session_id", sessionID).Msg("Tenant WebSocket closed normally")
			}
			break
		}

		session.LastActive = time.Now()

		// Binary frames share an envelope:
		//   [version=1:1][frameType:1][sessionIdLen:2 big-endian][sessionId][payload]
		//
		// frameType 0x01 = SSH input (browser → backend SSH stream)
		// frameType 0x10 = WebProxy frame (browser → backend StreamHTTP, proto-encoded)
		if wsMsgType == websocket.BinaryMessage {
			if len(rawMsg) >= 4 && rawMsg[0] == sshBinaryFrameVersion {
				switch rawMsg[1] {
				case 1: // SSH input
					sessionIDLen := int(rawMsg[2])<<8 | int(rawMsg[3])
					if 4+sessionIDLen <= len(rawMsg) {
						binarySSHSessionID := string(rawMsg[4 : 4+sessionIDLen])
						inputPayload := rawMsg[4+sessionIDLen:]
						session.sshStreamsMu.RLock()
						binaryHandler, binaryExists := session.sshStreams[binarySSHSessionID]
						session.sshStreamsMu.RUnlock()
						if binaryExists && binaryHandler.canAcceptInput() {
							data := append([]byte(nil), inputPayload...)
							select {
							case binaryHandler.inputCh <- &pb.SSHStreamMessage{
								SessionId: binarySSHSessionID,
								Payload: &pb.SSHStreamMessage_Input{
									Input: &pb.SSHInput{InputType: &pb.SSHInput_Data{Data: data}},
								},
							}:
							default:
								log.Warn().Str("ssh_session", binarySSHSessionID).Msg("SSH input channel full, dropping keystroke")
							}
						}
					}
				case webproxyBinaryFrameClient: // WebProxy frame from browser
					p.handleWebProxyBinaryFrame(session, rawMsg)
				}
			}
			continue
		}

		// Parse to detect JSON message type
		var baseMsg struct {
			Type            string          `json:"type"`
			ClientPublicKey string          `json:"client_public_key"`
			Ciphertext      string          `json:"ciphertext"`
			SessionID       string          `json:"session_id"`
			Payload         json.RawMessage `json:"payload"`
		}
		if err := json.Unmarshal(rawMsg, &baseMsg); err != nil {
			log.Error().Err(err).Str("session_id", sessionID).Msg("Failed to parse message")
			continue
		}

		// Handle SSH stream messages (require authentication)
		if baseMsg.Type == "ssh_stream_start" && baseMsg.SessionID != "" {
			if session.TenantID == "" {
				p.sendError(session, baseMsg.Type, "authentication required")
				continue
			}
			go p.handleSSHStreamStart(session, baseMsg.SessionID)
			continue
		}
		if baseMsg.Type == "ssh_stream" && baseMsg.SessionID != "" {
			if session.TenantID == "" {
				continue
			}
			p.handleSSHStreamData(session, baseMsg.SessionID, baseMsg.Payload)
			continue
		}
		if baseMsg.Type == "ssh_stream_close" && baseMsg.SessionID != "" {
			if session.TenantID == "" {
				continue
			}
			p.handleSSHStreamClose(session, baseMsg.SessionID)
			continue
		}

		if baseMsg.Type == "routeros_stream_start" && baseMsg.SessionID != "" {
			if session.TenantID == "" {
				p.sendError(session, baseMsg.Type, "authentication required")
				continue
			}
			go p.handleRouterOSStreamStart(session, baseMsg.SessionID, baseMsg.Payload)
			continue
		}
		if baseMsg.Type == "routeros_stream" && baseMsg.SessionID != "" {
			if session.TenantID == "" {
				continue
			}
			p.handleRouterOSStreamData(session, baseMsg.SessionID, baseMsg.Payload)
			continue
		}
		if baseMsg.Type == "routeros_stream_close" && baseMsg.SessionID != "" {
			if session.TenantID == "" {
				continue
			}
			p.handleRouterOSStreamClose(session, baseMsg.SessionID)
			continue
		}

		// WebProxy streaming traffic uses the binary frame path
		// (webproxyBinaryFrameClient, frameType 0x10) handled above —
		// no JSON entry point.

		// Handle heartbeat
		if baseMsg.Type == "ping" {
			session.mu.Lock()
			conn.SetWriteDeadline(time.Now().Add(10 * time.Second))
			conn.WriteJSON(map[string]string{"type": "pong"})
			session.mu.Unlock()
			continue
		}

		// Handle peer subscription (require authentication + ownership verification)
		if baseMsg.Type == "subscribe_peer" {
			if session.TenantID == "" {
				p.sendError(session, baseMsg.Type, "authentication required")
				continue
			}
			var payload struct {
				PeerID string `json:"peer_id"`
			}
			if err := json.Unmarshal(baseMsg.Payload, &payload); err == nil && payload.PeerID != "" {
				// Verify the peer belongs to this tenant
				if _, _, err := p.resolvePeerRoute(context.Background(), session, payload.PeerID); err != nil {
					log.Warn().Err(err).Str("session_id", sessionID).Str("peer_id", payload.PeerID).Msg("subscribe_peer: peer ownership check failed")
					p.sendError(session, baseMsg.Type, "peer not found or access denied")
					continue
				}
				session.mu.Lock()
				if session.SubscribedPeers == nil {
					session.SubscribedPeers = make(map[string]bool)
				}
				session.SubscribedPeers[payload.PeerID] = true
				session.mu.Unlock()
				log.Debug().Str("session_id", sessionID).Str("peer_id", payload.PeerID).Msg("Subscribed to peer updates")
			}
			continue
		}

		if baseMsg.Type == "unsubscribe_peer" {
			if session.TenantID == "" {
				continue
			}
			var payload struct {
				PeerID string `json:"peer_id"`
			}
			if err := json.Unmarshal(baseMsg.Payload, &payload); err == nil && payload.PeerID != "" {
				session.mu.Lock()
				if session.SubscribedPeers != nil {
					delete(session.SubscribedPeers, payload.PeerID)
				}
				session.mu.Unlock()
				log.Debug().Str("session_id", sessionID).Str("peer_id", payload.PeerID).Msg("Unsubscribed from peer updates")
			}
			continue
		}

		// Handle WUSP live-feed subscription (require authentication + ownership verification)
		if baseMsg.Type == "subscribe_wusp" {
			if session.TenantID == "" {
				p.sendError(session, baseMsg.Type, "authentication required")
				continue
			}
			var payload struct {
				PeerID string `json:"peer_id"` // WireGuard public key (base64)
			}
			if err := json.Unmarshal(baseMsg.Payload, &payload); err == nil && payload.PeerID != "" {
				// Verify the peer belongs to this tenant
				if _, _, err := p.resolvePeerRoute(context.Background(), session, payload.PeerID); err != nil {
					log.Warn().Err(err).Str("session_id", sessionID).Str("peer_id", payload.PeerID).Msg("subscribe_wusp: peer ownership check failed")
					p.sendError(session, baseMsg.Type, "peer not found or access denied")
					continue
				}
				session.mu.Lock()
				if session.WUSPSubscribedPeers == nil {
					session.WUSPSubscribedPeers = make(map[string]bool)
				}
				alreadySubscribed := session.WUSPSubscribedPeers[payload.PeerID]
				session.WUSPSubscribedPeers[payload.PeerID] = true
				session.mu.Unlock()
				// Only refcount on the per-session edge (not every duplicate
				// subscribe message) so the proxy-level Ensure RPC fires
				// at most once per session per peer.
				if !alreadySubscribed {
					p.addWUSPSubscriber(context.Background(), session.TenantID, payload.PeerID)
				}
				log.Debug().Str("session_id", sessionID).Str("peer_id", payload.PeerID).Msg("wusp: subscribed to live feed")
			}
			continue
		}

		if baseMsg.Type == "unsubscribe_wusp" {
			if session.TenantID == "" {
				continue
			}
			var payload struct {
				PeerID string `json:"peer_id"`
			}
			if err := json.Unmarshal(baseMsg.Payload, &payload); err == nil && payload.PeerID != "" {
				session.mu.Lock()
				wasSubscribed := false
				if session.WUSPSubscribedPeers != nil {
					wasSubscribed = session.WUSPSubscribedPeers[payload.PeerID]
					delete(session.WUSPSubscribedPeers, payload.PeerID)
				}
				session.mu.Unlock()
				if wasSubscribed {
					p.removeWUSPSubscriber(session.TenantID, payload.PeerID)
				}
				log.Debug().Str("session_id", sessionID).Str("peer_id", payload.PeerID).Msg("wusp: unsubscribed from live feed")
			}
			continue
		}

		// Handle key exchange response from client
		if baseMsg.Type == "key_exchange" && baseMsg.ClientPublicKey != "" {
			session.ClientPublicKey = baseMsg.ClientPublicKey
			cipher, err := crypto.NewSessionCipherFromKeyExchange(
				&session.ServerKeyPair.PrivateKey,
				baseMsg.ClientPublicKey,
				sessionID,
			)
			if err != nil {
				log.Error().Err(err).Str("session_id", sessionID).Msg("Failed to complete key exchange")
				p.sendError(session, "key_exchange", "Key exchange failed")
				continue
			}
			session.SessionCipher = cipher
			session.EncryptionEnabled = true
			log.Debug().Str("session_id", sessionID).Msg(" E2EE key exchange completed")

			// Send confirmation
			session.mu.Lock()
			conn.SetWriteDeadline(time.Now().Add(10 * time.Second))
			conn.WriteJSON(map[string]any{
				"type":    "key_exchange_complete",
				"success": true,
			})
			session.mu.Unlock()
			continue
		}

		// Handle encrypted messages
		if baseMsg.Type == "encrypted" && baseMsg.Ciphertext != "" {
			if !session.EncryptionEnabled || session.SessionCipher == nil {
				log.Warn().Str("session_id", sessionID).Msg("Received encrypted message but E2EE not enabled")
				p.sendError(session, "", "Encryption not enabled")
				continue
			}

			decrypted, err := session.SessionCipher.DecryptJSON(baseMsg.Ciphertext)
			if err != nil {
				log.Error().Err(err).Str("session_id", sessionID).Msg("Failed to decrypt message")
				p.sendError(session, "", "Decryption failed")
				continue
			}

			var msg Message
			if err := json.Unmarshal([]byte(decrypted), &msg); err != nil {
				log.Error().Err(err).Str("session_id", sessionID).Msg("Failed to parse decrypted message")
				continue
			}

			log.Debug().
				Str("session_id", sessionID).
				Str("service", msg.Service).
				Str("method", msg.Method).
				Msg(" Decrypted message received")

			go p.handleMessage(session, &msg)
			continue
		}

		// Handle unencrypted messages (backward compatibility)
		var msg Message
		if err := json.Unmarshal(rawMsg, &msg); err != nil {
			log.Error().Err(err).Str("session_id", sessionID).Msg("Failed to parse message")
			continue
		}

		log.Debug().
			Str("session_id", sessionID).
			Str("service", msg.Service).
			Str("method", msg.Method).
			Str("msg_id", msg.ID).
			Bool("encrypted", false).
			Msg(" Tenant WS message received")

		go p.handleMessage(session, &msg)
	}

	log.Debug().Str("session_id", sessionID).Msg(" Tenant WebSocket session ended")
}

// handleMessage processes a tenant WebSocket message.
func (p *TenantProxy) handleMessage(session *TenantSession, msg *Message) {
	// Give gRPC calls up to 60 seconds (Twilio/email APIs can be slow)
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	// Build the auth.CallContext that travels alongside ctx through every
	// in-process service handler. Replaces the gRPC metadata pairs the
	// proxy used pre–Stage 2.
	cc := session.applyRoutingCallContext(&auth.CallContext{
		OriginIP:        session.IPAddress,
		OriginUserAgent: session.UserAgent,
		SessionToken:    session.SessionToken,
	})

	// Debug log what we're sending
	if msg.Method == "TenantLogin" || msg.Method == "ListTenantPeers" || msg.Method == "PingTenantPeer" || msg.Method == "GetTenantPeerConfig" {
		tokenPreview := "(none)"
		if session.SessionToken != "" {
			if len(session.SessionToken) > 8 {
				tokenPreview = session.SessionToken[:8] + "..."
			} else {
				tokenPreview = session.SessionToken
			}
		}
		log.Info().
			Str("session_id", session.ID).
			Str("tenant_id", session.TenantID).
			Str("method", msg.Method).
			Bool("has_session_token", session.SessionToken != "").
			Str("token_preview", tokenPreview).
			Str("effective_tenant_id", session.GetEffectiveTenantID()).
			Msg("[proxy] handleMessage: call context snapshot")
	}

	ctx = auth.WithCallContext(ctx, cc)

	var response *Response

	switch msg.Service {
	case "TenantRegistrationService":
		response = p.handleTenantRegistration(ctx, msg)
	case "TenantPortalService":
		response = p.handleTenantPortal(ctx, msg, session)
	case "TenantBillingService":
		response = p.handleTenantBilling(ctx, msg, session)
	case "TenantPeerService":
		response = p.handleTenantPeerService(ctx, msg, session)
	case "TenantNetworkService":
		response = p.handleTenantNetworkService(ctx, msg, session)
	case "TenantWebSSHService":
		response = p.handleTenantWebSSHService(ctx, msg, session)
	case "TenantDataService":
		response = p.handleTenantDataService(ctx, msg, session)
	case "WebProxyService":
		response = p.handleWebProxyService(ctx, msg, session)
	case "PeerService":
		response = p.handlePeerService(ctx, msg, session)
	case "WUSPService":
		response = p.handleWUSPService(ctx, msg, session)
	case "RouterOSService":
		response = p.handleRouterOSService(ctx, msg, session)
	case "AdminService":
		response = p.handleAdminService(ctx, msg, session)
	case "CopilotService":
		response = p.handleCopilotService(ctx, msg, session)
	default:
		response = &Response{
			ID:    msg.ID,
			Type:  "error",
			Error: fmt.Sprintf("unknown service for tenant: %s", msg.Service),
		}
	}

	// Nil response means the handler is streaming asynchronously (e.g. StreamPing).
	if response == nil {
		return
	}
	p.sendResponse(session, response)

	log.Debug().
		Str("session_id", session.ID).
		Str("msg_id", msg.ID).
		Str("type", response.Type).
		Bool("encrypted", session.EncryptionEnabled).
		Msg(" WebSocket response sent")
}

// handleTenantRegistration handles TenantRegistrationService calls.
func (p *TenantProxy) handleTenantRegistration(ctx context.Context, msg *Message) *Response {
	// All registration traffic lands in-process.
	client := p.services.TenantRegistration

	switch msg.Method {
	case "GetPaymentStatus":
		resp, err := client.GetPaymentStatus(ctx, &pb.GetPaymentStatusRequest{})
		if err != nil {
			// Graceful fallback - payment service unavailable
			log.Debug().Err(err).Msg("Payment status unavailable, using fallback")
			return successResponse(msg.ID, map[string]any{
				"stripe_ready":         false,
				"paid_plans_available": false,
				"message":              "",
			})
		}
		result := map[string]any{
			"stripe_ready":         resp.StripeReady,
			"paid_plans_available": resp.PaidPlansAvailable,
			"message":              resp.Message,
		}
		if resp.StripePublishableKey != "" {
			result["stripe_publishable_key"] = resp.StripePublishableKey
		}
		return successResponse(msg.ID, result)

	case "GetAllowedPhoneRegions":
		resp, err := client.GetAllowedPhoneRegions(ctx, &pb.GetAllowedPhoneRegionsRequest{})
		if err != nil {
			log.Warn().Err(err).Msg("Failed to get allowed phone regions from gRPC - using fallback")
			// Return default regions as fallback (same as tenant_service.go defaultCountries)
			return successResponse(msg.ID, map[string]any{
				"regions": []map[string]string{
					{"country_code": "US", "dial_code": "+1", "country_name": "United States", "flag_emoji": "🇺🇸"},
					{"country_code": "CA", "dial_code": "+1", "country_name": "Canada", "flag_emoji": "🇨🇦"},
					{"country_code": "GB", "dial_code": "+44", "country_name": "United Kingdom", "flag_emoji": "🇬🇧"},
					{"country_code": "DE", "dial_code": "+49", "country_name": "Germany", "flag_emoji": "🇩🇪"},
					{"country_code": "FR", "dial_code": "+33", "country_name": "France", "flag_emoji": "🇫🇷"},
					{"country_code": "AU", "dial_code": "+61", "country_name": "Australia", "flag_emoji": "🇦🇺"},
					{"country_code": "IN", "dial_code": "+91", "country_name": "India", "flag_emoji": "🇮🇳"},
					{"country_code": "JP", "dial_code": "+81", "country_name": "Japan", "flag_emoji": "🇯🇵"},
					{"country_code": "SG", "dial_code": "+65", "country_name": "Singapore", "flag_emoji": "🇸🇬"},
				},
				"all_regions_allowed": true,
			})
		}
		regions := make([]map[string]string, len(resp.Regions))
		for i, r := range resp.Regions {
			regions[i] = map[string]string{
				"country_code": r.CountryCode,
				"dial_code":    r.DialCode,
				"country_name": r.CountryName,
				"flag_emoji":   r.FlagEmoji,
			}
		}
		return successResponse(msg.ID, map[string]any{
			"regions":             regions,
			"all_regions_allowed": resp.AllRegionsAllowed,
		})

	case "StartRegistration":
		// Parse JSON manually to handle tier string conversion
		var rawReq map[string]any
		if err := json.Unmarshal(msg.Request, &rawReq); err != nil {
			return errorResponse(msg.ID, fmt.Errorf("invalid request: %s", err.Error()))
		}
		req := &pb.StartRegistrationRequest{}
		if email, ok := rawReq["email"].(string); ok {
			req.Email = email
		}
		if fullName, ok := rawReq["full_name"].(string); ok {
			req.FullName = fullName
		}
		if phone, ok := rawReq["phone"].(string); ok {
			req.Phone = phone
		}
		if password, ok := rawReq["password"].(string); ok {
			req.Password = password
		}
		// Handle tier as int or string
		if tierInt, ok := rawReq["tier"].(float64); ok {
			req.Tier = pb.AccountTier(int32(tierInt))
		} else if tierStr, ok := rawReq["tier"].(string); ok {
			req.Tier = parseTierString(tierStr)
		}

		// Rate limit registrations by email to prevent spam
		rateLimitKey := "registration:" + req.Email
		allowed, _, retryAfter := p.registrationLimiter.Allow(rateLimitKey)
		if !allowed {
			log.Warn().
				Str("email", req.Email).
				Int("retry_after_seconds", retryAfter).
				Msg("🚫 Registration rate limit exceeded")
			rateLimitData, _ := json.Marshal(map[string]any{
				"rate_limited": true,
				"retry_after":  retryAfter,
				"error_code":   "RATE_LIMIT_EXCEEDED",
			})
			return &Response{
				ID:       msg.ID,
				Type:     "error",
				Response: rateLimitData,
				Error:    fmt.Sprintf("Too many registration attempts. Please try again in %d minutes.", (retryAfter+59)/60),
			}
		}

		// In-process: there is exactly one hub, so the legacy "pick a hub and
		// cache the mapping for sticky routing" dance is now a no-op.
		resp, err := client.StartRegistration(ctx, req)
		if err != nil {
			log.Error().Err(err).Str("email", req.Email).Msg("StartRegistration gRPC error")
			return errorResponse(msg.ID, err)
		}

		log.Debug().
			Str("registration_id", resp.RegistrationId).
			Bool("phone_verification_sent", resp.PhoneVerificationSent).
			Msg(" Sending StartRegistration response to WebSocket")
		return successResponse(msg.ID, map[string]any{
			"success":         true,
			"registration_id": resp.RegistrationId,
			"message":         resp.Message,
		})

	case "VerifyPhone":
		var req pb.VerifyPhoneRequest
		if err := json.Unmarshal(msg.Request, &req); err != nil {
			return errorResponse(msg.ID, fmt.Errorf("invalid request: %s", err.Error()))
		}
		resp, err := client.VerifyPhone(ctx, &req)
		if err != nil {
			return errorResponse(msg.ID, err)
		}
		result := map[string]any{
			"success":           resp.Verified,
			"message":           resp.Message,
			"ready_for_payment": resp.ReadyForPayment,
		}
		// Include auto-created tenant info for free tier
		if resp.AccountCreated {
			result["account_created"] = true
			result["tenant_id"] = resp.TenantId
			result["session_token"] = resp.SessionToken
			result["email"] = resp.Email
			result["full_name"] = resp.FullName
			result["tier"] = resp.Tier
		}
		return successResponse(msg.ID, result)

	case "ResendPhoneVerification":
		var req pb.ResendPhoneVerificationRequest
		if err := json.Unmarshal(msg.Request, &req); err != nil {
			return errorResponse(msg.ID, fmt.Errorf("invalid request: %s", err.Error()))
		}
		resp, err := client.ResendPhoneVerification(ctx, &req)
		if err != nil {
			return errorResponse(msg.ID, err)
		}
		return successResponse(msg.ID, map[string]any{
			"success":              resp.Sent,
			"message":              resp.Message,
			"retry_after_seconds":  resp.RetryAfterSeconds,
			"resends_remaining":    resp.ResendsRemaining,
			"verification_channel": resp.VerificationChannel,
		})

	case "CompleteRegistration":
		var req pb.CompleteRegistrationRequest
		if err := json.Unmarshal(msg.Request, &req); err != nil {
			return errorResponse(msg.ID, fmt.Errorf("invalid request: %s", err.Error()))
		}
		resp, err := client.CompleteRegistration(ctx, &req)
		if err != nil {
			return errorResponse(msg.ID, err)
		}
		result := map[string]any{
			"success":   resp.TenantId != "",
			"tenant_id": resp.TenantId,
			"message":   resp.Message,
		}
		if resp.RequiresCheckout {
			result["requires_checkout"] = true
			result["checkout_url"] = resp.CheckoutUrl
		}
		if resp.TotpProvisioningUrl != "" {
			result["totp_url"] = resp.TotpProvisioningUrl
			result["totp_secret"] = resp.TotpSecret
		}
		return successResponse(msg.ID, result)

	case "CreateSetupIntent":
		var req struct {
			RegistrationId     string   `json:"registration_id"`
			PaymentMethodTypes []string `json:"payment_method_types"`
		}

		if err := json.Unmarshal(msg.Request, &req); err != nil {
			return errorResponse(msg.ID, fmt.Errorf("invalid request: %s", err.Error()))
		}

		grpcReq := &pb.CreateSetupIntentRequest{
			RegistrationId:     req.RegistrationId,
			PaymentMethodTypes: req.PaymentMethodTypes,
		}

		resp, err := client.CreateSetupIntent(ctx, grpcReq)
		if err != nil {
			return errorResponse(msg.ID, err)
		}

		return successResponse(msg.ID, map[string]any{
			"client_secret":   resp.ClientSecret,
			"publishable_key": resp.PublishableKey,
			"setup_intent_id": resp.SetupIntentId,
		})

	case "CreateCheckoutSession":
		// Parse JSON manually to handle tier (can be int or string)
		var rawReq map[string]any
		if err := json.Unmarshal(msg.Request, &rawReq); err != nil {
			return errorResponse(msg.ID, fmt.Errorf("invalid request: %s", err.Error()))
		}
		req := &pb.CreateCheckoutSessionRequest{}
		if regId, ok := rawReq["registration_id"].(string); ok {
			req.RegistrationId = regId
		}
		// Handle tier as int or string
		if tierInt, ok := rawReq["tier"].(float64); ok {
			req.Tier = pb.AccountTier(int32(tierInt))
		} else if tierStr, ok := rawReq["tier"].(string); ok {
			req.Tier = parseTierString(tierStr)
		}
		if successUrl, ok := rawReq["success_url"].(string); ok {
			req.SuccessUrl = successUrl
		}
		if cancelUrl, ok := rawReq["cancel_url"].(string); ok {
			req.CancelUrl = cancelUrl
		}
		resp, err := client.CreateCheckoutSession(ctx, req)
		if err != nil {
			return errorResponse(msg.ID, err)
		}
		return successResponse(msg.ID, map[string]any{
			"success":      true,
			"checkout_url": resp.CheckoutUrl,
			"session_id":   resp.SessionId,
		})

	case "GetRegistrationStatus":
		var req pb.GetRegistrationStatusRequest
		if err := json.Unmarshal(msg.Request, &req); err != nil {
			return errorResponse(msg.ID, fmt.Errorf("invalid request: %s", err.Error()))
		}
		resp, err := client.GetRegistrationStatus(ctx, &req)
		if err != nil {
			return errorResponse(msg.ID, err)
		}
		return successResponse(msg.ID, map[string]any{
			"status":           resp.Status,
			"phone_verified":   resp.PhoneVerified,
			"payment_complete": resp.PaymentComplete,
			"tier":             resp.Tier.String(),
			"email":            resp.Email,
		})

	case "GetAvailablePlans":
		resp, err := client.GetAvailablePlans(ctx, &pb.GetAvailablePlansRequest{})
		if err != nil {
			return errorResponse(msg.ID, err)
		}
		// Convert plans to JSON-friendly format
		plans := make([]map[string]any, 0, len(resp.Plans))
		for _, plan := range resp.Plans {
			plans = append(plans, map[string]any{
				"tier":        plan.Tier.String(),
				"name":        plan.Name,
				"price_cents": plan.PriceCents,
				"currency":    plan.Currency,
				"block_count": plan.BlockCount,
				"max_peers":   plan.MaxPeers,
				"features":    plan.Features,
				"trial_days":  plan.TrialDays,
				"is_popular":  plan.IsPopular,
			})
		}
		result := map[string]any{
			"plans":        plans,
			"stripe_ready": resp.StripeReady,
		}
		if resp.StripePublishableKey != "" {
			result["stripe_publishable_key"] = resp.StripePublishableKey
		}
		return successResponse(msg.ID, result)

	default:
		return errorResponse(msg.ID, fmt.Errorf("unknown method: %s", msg.Method))
	}
}

// handleTenantPortal handles TenantPortalService calls.
func (p *TenantProxy) handleTenantPortal(ctx context.Context, msg *Message, session *TenantSession) *Response {
	client := p.services.TenantPortal
	callerTenantID := session.TenantID

	switch msg.Method {
	// MCP Configuration & Management
	case "GetMCPConfig":
		// Return MCP configuration via WebSocket (Encrypted)
		// This replaces the HTTP handler at /api/mcp/config

		// If session is not authenticated, return error
		if session.TenantID == "" || session.SessionToken == "" {
			return errorResponse(msg.ID, fmt.Errorf("unauthorized"))
		}

		// We use relative path "/sse" for serverUrl, client should prepend origin.
		// NOTE: The user requested a specific JSON structure.
		// We can return the full JSON here.

		// The client (Account.svelte) expects just the config object or specific fields?
		// The HTTP endpoint returned the JSON directly.
		// We will return it in the "response" field.

		config := map[string]any{
			"mcpServers": map[string]any{
				"wantastic": map[string]any{
					"serverUrl": "/sse",
					"headers": map[string]string{
						"Authorization": "Bearer <API_KEY_REQUIRED>",
					},
					"disabled":      false,
					"disabledTools": []string{},
				},
			},
		}

		return successResponse(msg.ID, config)

	case "ListAPIKeys":
		if session.TenantID == "" {
			return errorResponse(msg.ID, fmt.Errorf("unauthorized"))
		}
		keys, err := p.sessionStore.ListAPIKeys(session.TenantID)
		if err != nil {
			return errorResponse(msg.ID, err)
		}
		// Mask tokens for security, but frontend uses prefix
		displayKeys := make([]map[string]any, len(keys))
		for i, k := range keys {
			displayKeys[i] = map[string]any{
				"id":         k.ID,
				"name":       k.Name,
				"created_at": k.CreatedAt,
				"last_used":  k.LastUsedAt,
				"expires_at": k.ExpiresAt,
				"prefix":     k.Token[:6] + "...",
				// We do NOT return the full token here for security, unless user explicitly requested "always one should be there" implies we should?
				// But we fundamentally can't if we don't have it (if it was hashed? but here it seems stored as JSON).
				// RedisSessionStore stores the whole struct.
				// So we COULD return it.
				// User said: "api key always one should be there".
				// I will return the full token for now to satisfy the user's "fix this json" request if they use an existing key.
				"token": k.Token,
			}
		}
		return successResponse(msg.ID, map[string]any{"keys": displayKeys})

	case "CreateAPIKey":
		if session.TenantID == "" {
			return errorResponse(msg.ID, fmt.Errorf("unauthorized"))
		}
		var req struct {
			Name string `json:"name"`
		}
		if err := json.Unmarshal(msg.Request, &req); err != nil {
			return errorResponse(msg.ID, fmt.Errorf("invalid request"))
		}
		// Create key valid for 30 days
		expiresAt := time.Now().Add(30 * 24 * time.Hour)
		key, err := p.sessionStore.CreateAPIKey(session.TenantID, req.Name, session.SessionToken, expiresAt)
		if err != nil {
			return errorResponse(msg.ID, err)
		}
		return successResponse(msg.ID, key)

	case "RevokeAPIKey":
		if session.TenantID == "" {
			return errorResponse(msg.ID, fmt.Errorf("unauthorized"))
		}
		var req struct {
			ID string `json:"id"`
		}
		if err := json.Unmarshal(msg.Request, &req); err != nil {
			return errorResponse(msg.ID, fmt.Errorf("invalid request"))
		}
		if err := p.sessionStore.RevokeAPIKey(session.TenantID, req.ID); err != nil {
			return errorResponse(msg.ID, err)
		}
		return successResponse(msg.ID, map[string]any{"success": true})

	case "TenantLogin":
		var req pb.TenantLoginRequest
		if err := json.Unmarshal(msg.Request, &req); err != nil {
			return errorResponse(msg.ID, fmt.Errorf("invalid request: %s", err.Error()))
		}

		// Rate limit login attempts by email to prevent brute force
		rateLimitKey := "login:" + req.Email
		allowed, _, retryAfter := p.loginLimiter.Allow(rateLimitKey)
		if !allowed {
			log.Warn().
				Str("email", req.Email).
				Int("retry_after_seconds", retryAfter).
				Msg("🚫 Login rate limit exceeded")
			rateLimitData, _ := json.Marshal(map[string]any{
				"rate_limited": true,
				"retry_after":  retryAfter,
				"error_code":   "RATE_LIMIT_EXCEEDED",
			})
			return &Response{
				ID:       msg.ID,
				Type:     "error",
				Response: rateLimitData,
				Error:    fmt.Sprintf("Too many login attempts. Please wait %d seconds.", retryAfter),
			}
		}

		resp, err := client.TenantLogin(ctx, &req)
		if err != nil {
			return errorResponse(msg.ID, err)
		}
		// Store session info on successful login
		if resp.Success && resp.SessionToken != "" {
			session.SessionToken = resp.SessionToken
			session.TenantID = resp.TenantId
			session.Email = req.Email
		}
		return protoResponse(msg.ID, resp)

	case "Send2FACode":
		var req pb.Send2FACodeRequest
		if err := json.Unmarshal(msg.Request, &req); err != nil {
			return errorResponse(msg.ID, fmt.Errorf("invalid request: %s", err.Error()))
		}
		resp, err := client.Send2FACode(ctx, &req)
		if err != nil {
			return errorResponse(msg.ID, err)
		}
		return protoResponse(msg.ID, resp)

	case "GetTenantDashboard":
		if session.TenantID == "" {
			return errorResponse(msg.ID, fmt.Errorf("authentication required"))
		}
		var req pb.GetTenantDashboardRequest
		if err := json.Unmarshal(msg.Request, &req); err != nil {
			return errorResponse(msg.ID, fmt.Errorf("invalid request: %s", err.Error()))
		}
		req.TenantId = callerTenantID
		resp, err := client.GetTenantDashboard(ctx, &req)
		if err != nil {
			return errorResponse(msg.ID, err)
		}
		return protoResponse(msg.ID, resp)

	case "GetTenantWinboxSession":
		if session.TenantID == "" {
			return errorResponse(msg.ID, fmt.Errorf("authentication required"))
		}
		var req pb.GetTenantWinboxSessionRequest
		if err := json.Unmarshal(msg.Request, &req); err != nil {
			return errorResponse(msg.ID, fmt.Errorf("invalid request: %s", err.Error()))
		}

		resp, err := client.GetTenantWinboxSession(ctx, &req)
		if err != nil {
			return errorResponse(msg.ID, err)
		}
		return protoResponse(msg.ID, resp)

	case "CreateTenantWinboxSession":
		if session.TenantID == "" {
			return errorResponse(msg.ID, fmt.Errorf("authentication required"))
		}
		var req pb.CreateTenantWinboxSessionRequest
		if err := json.Unmarshal(msg.Request, &req); err != nil {
			return errorResponse(msg.ID, fmt.Errorf("invalid request: %s", err.Error()))
		}

		resp, err := client.CreateTenantWinboxSession(ctx, &req)
		if err != nil {
			return errorResponse(msg.ID, err)
		}
		return protoResponse(msg.ID, resp)

	case "GetTenantAccount":
		if session.TenantID == "" {
			return errorResponse(msg.ID, fmt.Errorf("authentication required"))
		}
		var req pb.GetTenantAccountRequest
		if err := json.Unmarshal(msg.Request, &req); err != nil {
			return errorResponse(msg.ID, fmt.Errorf("invalid request: %s", err.Error()))
		}
		req.TenantId = session.TenantID
		resp, err := client.GetTenantAccount(ctx, &req)
		if err != nil {
			return errorResponse(msg.ID, err)
		}
		return protoResponse(msg.ID, resp)

	case "UpdateTenantProfile":
		if session.TenantID == "" {
			return errorResponse(msg.ID, fmt.Errorf("authentication required"))
		}
		var req pb.UpdateTenantProfileRequest
		if err := json.Unmarshal(msg.Request, &req); err != nil {
			return errorResponse(msg.ID, fmt.Errorf("invalid request: %s", err.Error()))
		}
		req.TenantId = session.TenantID
		resp, err := client.UpdateTenantProfile(ctx, &req)
		if err != nil {
			return errorResponse(msg.ID, err)
		}
		return protoResponse(msg.ID, resp)

	case "ChangePassword":
		if session.TenantID == "" {
			return errorResponse(msg.ID, fmt.Errorf("authentication required"))
		}
		var req pb.ChangePasswordRequest
		if err := json.Unmarshal(msg.Request, &req); err != nil {
			return errorResponse(msg.ID, fmt.Errorf("invalid request: %s", err.Error()))
		}
		req.TenantId = session.TenantID

		resp, err := client.ChangePassword(ctx, &req)
		if err != nil {
			return errorResponse(msg.ID, err)
		}
		return protoResponse(msg.ID, resp)

	case "HandleSecurityAlert":
		// No auth required (token based)
		var req pb.HandleSecurityAlertRequest
		if err := json.Unmarshal(msg.Request, &req); err != nil {
			return errorResponse(msg.ID, fmt.Errorf("invalid request: %s", err.Error()))
		}

		resp, err := client.HandleSecurityAlert(ctx, &req)
		if err != nil {
			return errorResponse(msg.ID, err)
		}
		return protoResponse(msg.ID, resp)

	case "GetTwoFASettings":
		if session.TenantID == "" {
			return errorResponse(msg.ID, fmt.Errorf("authentication required"))
		}
		var req pb.GetTwoFASettingsRequest
		if err := json.Unmarshal(msg.Request, &req); err != nil {
			return errorResponse(msg.ID, fmt.Errorf("invalid request: %s", err.Error()))
		}
		req.TenantId = session.TenantID
		resp, err := client.GetTwoFASettings(ctx, &req)
		if err != nil {
			return errorResponse(msg.ID, err)
		}
		return protoResponse(msg.ID, resp)

	case "SetTwoFAMethod":
		if session.TenantID == "" {
			return errorResponse(msg.ID, fmt.Errorf("authentication required"))
		}
		var req pb.SetTwoFAMethodRequest
		if err := json.Unmarshal(msg.Request, &req); err != nil {
			return errorResponse(msg.ID, fmt.Errorf("invalid request: %s", err.Error()))
		}
		req.TenantId = session.TenantID
		resp, err := client.SetTwoFAMethod(ctx, &req)
		if err != nil {
			return errorResponse(msg.ID, err)
		}
		return protoResponse(msg.ID, resp)

	case "SetupTOTP":
		// Generate TOTP secret and provisioning URL for QR code
		if session.TenantID == "" {
			return errorResponse(msg.ID, fmt.Errorf("authentication required"))
		}
		// Generate a new TOTP secret
		totpSecret := auth.GenerateTOTPSecret()
		provisioningURL := auth.GenerateTOTPURL(totpSecret, session.Email, "Wantastic")

		// Generate QR code as data URI
		qrCodeDataURI, err := auth.GenerateTOTPQRCode(totpSecret, session.Email, "Wantastic")
		if err != nil {
			log.Error().Err(err).Msg("Failed to generate TOTP QR code")
			return errorResponse(msg.ID, fmt.Errorf("failed to generate QR code"))
		}

		return successResponse(msg.ID, map[string]any{
			"success":               true,
			"totp_secret":           totpSecret,
			"totp_provisioning_url": provisioningURL,
			"qr_code":               qrCodeDataURI,
		})

	case "VerifyTOTP":
		// Verify TOTP code and enable 2FA for the account
		if session.TenantID == "" {
			return errorResponse(msg.ID, fmt.Errorf("authentication required"))
		}

		var reqData struct {
			Code       string `json:"code"`
			TotpSecret string `json:"totp_secret"`
		}
		if err := json.Unmarshal(msg.Request, &reqData); err != nil {
			return errorResponse(msg.ID, fmt.Errorf("invalid request: %s", err.Error()))
		}

		if reqData.Code == "" {
			return errorResponse(msg.ID, fmt.Errorf("TOTP code required"))
		}
		if reqData.TotpSecret == "" {
			return errorResponse(msg.ID, fmt.Errorf("TOTP secret required"))
		}

		// Use SetTwoFAMethod to enable TOTP (it verifies the code internally)
		req := &pb.SetTwoFAMethodRequest{
			TenantId:   session.TenantID,
			Method:     "totp",
			TotpSecret: reqData.TotpSecret,
			TotpCode:   reqData.Code,
		}
		resp, err := client.SetTwoFAMethod(ctx, req)
		if err != nil {
			return errorResponse(msg.ID, err)
		}
		if !resp.Success {
			return errorResponse(msg.ID, fmt.Errorf("%s", resp.Message))
		}

		return successResponse(msg.ID, map[string]any{
			"success": true,
			"message": "TOTP enabled successfully",
		})

	case "DisableTOTP":
		// Disable TOTP for the account (requires current TOTP code)
		if session.TenantID == "" {
			return errorResponse(msg.ID, fmt.Errorf("authentication required"))
		}

		var reqData struct {
			Code string `json:"code"`
		}
		if err := json.Unmarshal(msg.Request, &reqData); err != nil {
			return errorResponse(msg.ID, fmt.Errorf("invalid request: %s", err.Error()))
		}

		if reqData.Code == "" {
			return errorResponse(msg.ID, fmt.Errorf("TOTP code required to disable"))
		}

		// First verify the current TOTP code before disabling
		// Get tenant's current TOTP secret to verify
		accountResp, err := client.GetTenantAccount(ctx, &pb.GetTenantAccountRequest{TenantId: session.TenantID})
		if err != nil {
			return errorResponse(msg.ID, err)
		}

		// If TOTP is enabled, we need to verify the code
		// The code verification happens via the tenant's stored secret
		// Use SetTwoFAMethod with "none" to disable
		req := &pb.SetTwoFAMethodRequest{
			TenantId: session.TenantID,
			Method:   "none",
			TotpCode: reqData.Code, // Pass code for verification if needed
		}
		_ = accountResp // Used for verification context
		resp, err := client.SetTwoFAMethod(ctx, req)
		if err != nil {
			return errorResponse(msg.ID, err)
		}
		if !resp.Success {
			return errorResponse(msg.ID, fmt.Errorf("%s", resp.Message))
		}

		return successResponse(msg.ID, map[string]any{
			"success": true,
			"message": "TOTP disabled successfully",
		})

	// Password Recovery (public - no auth required)
	case "RequestPasswordReset":
		var req pb.RequestPasswordResetRequest
		if err := json.Unmarshal(msg.Request, &req); err != nil {
			return errorResponse(msg.ID, fmt.Errorf("invalid request: %s", err.Error()))
		}

		// Rate limit by email to prevent abuse
		rateLimitKey := "password_reset:" + req.Email
		allowed, _, retryAfter := p.passwordResetLimiter.Allow(rateLimitKey)
		if !allowed {
			log.Warn().
				Str("email", req.Email).
				Int("retry_after_seconds", retryAfter).
				Msg("🚫 Password reset rate limit exceeded at WebSocket proxy")
			// Return error with rate limit info
			rateLimitData, _ := json.Marshal(map[string]any{
				"rate_limited": true,
				"retry_after":  retryAfter,
				"error_code":   "RATE_LIMIT_EXCEEDED",
			})
			return &Response{
				ID:       msg.ID,
				Type:     "error",
				Response: rateLimitData,
				Error: fmt.Sprintf("Too many password reset attempts. Please try again in %d minutes.",
					(retryAfter+59)/60), // Round up to minutes
			}
		}

		resp, err := client.RequestPasswordReset(ctx, &req)
		if err != nil {
			return errorResponse(msg.ID, err)
		}
		return protoResponse(msg.ID, resp)

	case "VerifyResetCode":
		var req pb.VerifyResetCodeRequest
		if err := json.Unmarshal(msg.Request, &req); err != nil {
			return errorResponse(msg.ID, fmt.Errorf("invalid request: %s", err.Error()))
		}
		resp, err := client.VerifyResetCode(ctx, &req)
		if err != nil {
			return errorResponse(msg.ID, err)
		}
		return protoResponse(msg.ID, resp)

	case "ResetPassword":
		var req pb.ResetPasswordRequest
		if err := json.Unmarshal(msg.Request, &req); err != nil {
			return errorResponse(msg.ID, fmt.Errorf("invalid request: %s", err.Error()))
		}
		resp, err := client.ResetPassword(ctx, &req)
		if err != nil {
			return errorResponse(msg.ID, err)
		}
		return protoResponse(msg.ID, resp)
	case "GetTenantACLRules":
		if session.TenantID == "" {
			return errorResponse(msg.ID, fmt.Errorf("authentication required"))
		}
		if err := session.checkSharePerm("view_acl"); err != nil {
			return errorResponse(msg.ID, err)
		}
		var req pb.GetTenantACLRulesRequest
		if err := json.Unmarshal(msg.Request, &req); err != nil {
			return errorResponse(msg.ID, fmt.Errorf("invalid request: %s", err.Error()))
		}
		req.TenantId = session.TenantID
		resp, err := client.GetTenantACLRules(ctx, &req)
		if err != nil {
			return errorResponse(msg.ID, err)
		}
		return protoResponse(msg.ID, resp)
	case "AddTenantACLRule":
		if session.TenantID == "" {
			return errorResponse(msg.ID, fmt.Errorf("authentication required"))
		}
		if err := session.checkSharePerm("manage_acl"); err != nil {
			return errorResponse(msg.ID, err)
		}
		var req pb.AddTenantACLRuleRequest
		if err := json.Unmarshal(msg.Request, &req); err != nil {
			return errorResponse(msg.ID, fmt.Errorf("invalid request: %s", err.Error()))
		}
		req.TenantId = session.TenantID
		resp, err := client.AddTenantACLRule(ctx, &req)
		if err != nil {
			return errorResponse(msg.ID, err)
		}
		return protoResponse(msg.ID, resp)
	case "RemoveTenantACLRule":
		if session.TenantID == "" {
			return errorResponse(msg.ID, fmt.Errorf("authentication required"))
		}
		if err := session.checkSharePerm("manage_acl"); err != nil {
			return errorResponse(msg.ID, err)
		}
		var req pb.RemoveTenantACLRuleRequest
		if err := json.Unmarshal(msg.Request, &req); err != nil {
			return errorResponse(msg.ID, fmt.Errorf("invalid request: %s", err.Error()))
		}
		req.TenantId = session.TenantID
		resp, err := client.RemoveTenantACLRule(ctx, &req)
		if err != nil {
			return errorResponse(msg.ID, err)
		}
		return protoResponse(msg.ID, resp)
	case "CreateTenantPeerGroup":
		if session.TenantID == "" {
			return errorResponse(msg.ID, fmt.Errorf("authentication required"))
		}
		if err := session.checkSharePerm("manage_acl"); err != nil {
			return errorResponse(msg.ID, err)
		}
		var req pb.CreateTenantPeerGroupRequest
		if err := json.Unmarshal(msg.Request, &req); err != nil {
			return errorResponse(msg.ID, fmt.Errorf("invalid request: %s", err.Error()))
		}
		req.TenantId = callerTenantID
		resp, err := client.CreateTenantPeerGroup(ctx, &req)
		if err != nil {
			return errorResponse(msg.ID, err)
		}
		return protoResponse(msg.ID, resp)
	case "DeleteTenantPeerGroup":
		if session.TenantID == "" {
			return errorResponse(msg.ID, fmt.Errorf("authentication required"))
		}
		if err := session.checkSharePerm("manage_acl"); err != nil {
			return errorResponse(msg.ID, err)
		}
		var req pb.DeleteTenantPeerGroupRequest
		if err := json.Unmarshal(msg.Request, &req); err != nil {
			return errorResponse(msg.ID, fmt.Errorf("invalid request: %s", err.Error()))
		}
		req.TenantId = callerTenantID
		resp, err := client.DeleteTenantPeerGroup(ctx, &req)
		if err != nil {
			return errorResponse(msg.ID, err)
		}
		return protoResponse(msg.ID, resp)
	case "ListTenantPeerGroups":
		if session.TenantID == "" {
			return errorResponse(msg.ID, fmt.Errorf("authentication required"))
		}
		if err := session.checkSharePerm("view_acl"); err != nil {
			return errorResponse(msg.ID, err)
		}
		var req pb.ListTenantPeerGroupsRequest
		if err := json.Unmarshal(msg.Request, &req); err != nil {
			return errorResponse(msg.ID, fmt.Errorf("invalid request: %s", err.Error()))
		}
		req.TenantId = callerTenantID
		resp, err := client.ListTenantPeerGroups(ctx, &req)
		if err != nil {
			return errorResponse(msg.ID, err)
		}
		return protoResponse(msg.ID, resp)
	case "AddTenantPeerToGroup":
		// Tenant-scoped global ACL rule addition
		if session.TenantID == "" {
			return errorResponse(msg.ID, fmt.Errorf("authentication required"))
		}
		if err := session.checkSharePerm("manage_acl"); err != nil {
			return errorResponse(msg.ID, err)
		}
		var req pb.AddTenantPeerToGroupRequest
		if err := json.Unmarshal(msg.Request, &req); err != nil {
			return errorResponse(msg.ID, fmt.Errorf("invalid request: %s", err.Error()))
		}
		req.TenantId = callerTenantID
		resp, err := client.AddTenantPeerToGroup(ctx, &req)
		if err != nil {
			return errorResponse(msg.ID, err)
		}
		return protoResponse(msg.ID, resp)
	case "RemoveTenantPeerFromGroup":
		// Tenant-scoped global ACL rule removal
		if session.TenantID == "" {
			return errorResponse(msg.ID, fmt.Errorf("authentication required"))
		}
		if err := session.checkSharePerm("manage_acl"); err != nil {
			return errorResponse(msg.ID, err)
		}
		var req pb.RemoveTenantPeerFromGroupRequest
		if err := json.Unmarshal(msg.Request, &req); err != nil {
			return errorResponse(msg.ID, fmt.Errorf("invalid request: %s", err.Error()))
		}
		req.TenantId = callerTenantID
		resp, err := client.RemoveTenantPeerFromGroup(ctx, &req)
		if err != nil {
			return errorResponse(msg.ID, err)
		}
		return protoResponse(msg.ID, resp)
	case "CreateTenantGroupLink":
		// Tenant-scoped group creation
		if session.TenantID == "" {
			return errorResponse(msg.ID, fmt.Errorf("authentication required"))
		}
		if err := session.checkSharePerm("manage_acl"); err != nil {
			return errorResponse(msg.ID, err)
		}
		var req pb.CreateTenantGroupLinkRequest
		if err := json.Unmarshal(msg.Request, &req); err != nil {
			return errorResponse(msg.ID, fmt.Errorf("invalid request: %s", err.Error()))
		}
		req.TenantId = callerTenantID
		resp, err := client.CreateTenantGroupLink(ctx, &req)
		if err != nil {
			return errorResponse(msg.ID, err)
		}
		return protoResponse(msg.ID, resp)

	case "ListTenantGroupLinks":
		// Tenant-scoped group link revocation
		if session.TenantID == "" {
			return errorResponse(msg.ID, fmt.Errorf("authentication required"))
		}
		if err := session.checkSharePerm("view_acl"); err != nil {
			return errorResponse(msg.ID, err)
		}
		var req pb.ListTenantGroupLinksRequest
		if err := json.Unmarshal(msg.Request, &req); err != nil {
			return errorResponse(msg.ID, fmt.Errorf("invalid request: %s", err.Error()))
		}
		req.TenantId = callerTenantID
		resp, err := client.ListTenantGroupLinks(ctx, &req)
		if err != nil {
			return errorResponse(msg.ID, err)
		}
		return protoResponse(msg.ID, resp)
	case "CompileTenantGroups":
		if session.TenantID == "" {
			return errorResponse(msg.ID, fmt.Errorf("authentication required"))
		}
		if err := session.checkSharePerm("manage_acl"); err != nil {
			return errorResponse(msg.ID, err)
		}
		var req pb.CompileTenantGroupsRequest
		if err := json.Unmarshal(msg.Request, &req); err != nil {
			return errorResponse(msg.ID, fmt.Errorf("invalid request: %s", err.Error()))
		}
		req.TenantId = callerTenantID
		resp, err := client.CompileTenantGroups(ctx, &req)
		if err != nil {
			return errorResponse(msg.ID, err)
		}
		return protoResponse(msg.ID, resp)
	case "GetTenantCompilationStats":
		if session.TenantID == "" {
			return errorResponse(msg.ID, fmt.Errorf("authentication required"))
		}
		if err := session.checkSharePerm("view_acl"); err != nil {
			return errorResponse(msg.ID, err)
		}
		var req pb.GetTenantCompilationStatsRequest
		if err := json.Unmarshal(msg.Request, &req); err != nil {
			return errorResponse(msg.ID, fmt.Errorf("invalid request: %s", err.Error()))
		}
		req.TenantId = callerTenantID
		resp, err := client.GetTenantCompilationStats(ctx, &req)
		if err != nil {
			return errorResponse(msg.ID, err)
		}
		return protoResponse(msg.ID, resp)
	case "DeleteTenantGroupLink":
		// Tenant-scoped group link deletion
		if session.TenantID == "" {
			return errorResponse(msg.ID, fmt.Errorf("authentication required"))
		}
		if err := session.checkSharePerm("manage_acl"); err != nil {
			return errorResponse(msg.ID, err)
		}
		var req pb.DeleteTenantGroupLinkRequest
		if err := json.Unmarshal(msg.Request, &req); err != nil {
			return errorResponse(msg.ID, fmt.Errorf("invalid request: %s", err.Error()))
		}
		req.TenantId = callerTenantID
		resp, err := client.DeleteTenantGroupLink(ctx, &req)
		if err != nil {
			return errorResponse(msg.ID, err)
		}
		return protoResponse(msg.ID, resp)
	case "AssignExitNode":
		if session.TenantID == "" {
			return errorResponse(msg.ID, fmt.Errorf("authentication required"))
		}
		if err := session.checkSharePerm("manage_peers"); err != nil {
			return errorResponse(msg.ID, err)
		}
		var req pb.AssignExitNodeRequest
		if err := json.Unmarshal(msg.Request, &req); err != nil {
			return errorResponse(msg.ID, fmt.Errorf("invalid request: %s", err.Error()))
		}
		req.AccountId = callerTenantID

		// Legacy hub-pool peer routing collapses to the in-process service.
		resp, err := client.AssignExitNode(ctx, &req)
		if err != nil {
			return errorResponse(msg.ID, err)
		}
		return protoResponse(msg.ID, resp)
	case "CheckTenantAccess":
		if session.TenantID == "" {
			return errorResponse(msg.ID, fmt.Errorf("authentication required"))
		}
		if err := session.checkSharePerm("view_acl"); err != nil {
			return errorResponse(msg.ID, err)
		}
		var req pb.CheckTenantAccessRequest
		if err := json.Unmarshal(msg.Request, &req); err != nil {
			return errorResponse(msg.ID, fmt.Errorf("invalid request: %s", err.Error()))
		}
		req.TenantId = callerTenantID
		resp, err := client.CheckTenantAccess(ctx, &req)
		if err != nil {
			return errorResponse(msg.ID, err)
		}
		return protoResponse(msg.ID, resp)

	case "ListTenantSessions":
		// List all active sessions for the tenant
		if session.TenantID == "" {
			return errorResponse(msg.ID, fmt.Errorf("authentication required"))
		}
		var req pb.ListTenantSessionsRequest
		if err := json.Unmarshal(msg.Request, &req); err != nil {
			return errorResponse(msg.ID, fmt.Errorf("invalid request: %s", err.Error()))
		}
		req.TenantId = session.TenantID
		resp, err := client.ListTenantSessions(ctx, &req)
		if err != nil {
			return errorResponse(msg.ID, err)
		}
		return protoResponse(msg.ID, resp)

	case "DeleteTenantSession":
		// Delete a specific session
		if session.TenantID == "" {
			return errorResponse(msg.ID, fmt.Errorf("authentication required"))
		}
		var req pb.DeleteTenantSessionRequest
		if err := json.Unmarshal(msg.Request, &req); err != nil {
			return errorResponse(msg.ID, fmt.Errorf("invalid request: %s", err.Error()))
		}
		req.TenantId = session.TenantID
		resp, err := client.DeleteTenantSession(ctx, &req)
		if err != nil {
			return errorResponse(msg.ID, err)
		}
		return protoResponse(msg.ID, resp)

	case "ListEnrollmentTokens":
		if session.TenantID == "" {
			return errorResponse(msg.ID, fmt.Errorf("authentication required"))
		}
		var req pb.ListEnrollmentTokensRequest
		if err := json.Unmarshal(msg.Request, &req); err != nil {
			return errorResponse(msg.ID, fmt.Errorf("invalid request: %s", err.Error()))
		}
		req.TenantId = session.TenantID
		resp, err := client.ListEnrollmentTokens(ctx, &req)
		if err != nil {
			return errorResponse(msg.ID, err)
		}
		return protoResponse(msg.ID, resp)

	case "CreateEnrollmentToken":
		if session.TenantID == "" {
			return errorResponse(msg.ID, fmt.Errorf("authentication required"))
		}
		var req pb.CreateEnrollmentTokenRequest
		if err := json.Unmarshal(msg.Request, &req); err != nil {
			return errorResponse(msg.ID, fmt.Errorf("invalid request: %s", err.Error()))
		}
		req.TenantId = session.TenantID
		resp, err := client.CreateEnrollmentToken(ctx, &req)
		if err != nil {
			return errorResponse(msg.ID, err)
		}
		return protoResponse(msg.ID, resp)

	case "DeleteEnrollmentToken":
		if session.TenantID == "" {
			return errorResponse(msg.ID, fmt.Errorf("authentication required"))
		}
		var req pb.DeleteEnrollmentTokenRequest
		if err := json.Unmarshal(msg.Request, &req); err != nil {
			return errorResponse(msg.ID, fmt.Errorf("invalid request: %s", err.Error()))
		}
		req.TenantId = session.TenantID
		resp, err := client.DeleteEnrollmentToken(ctx, &req)
		if err != nil {
			return errorResponse(msg.ID, err)
		}
		return protoResponse(msg.ID, resp)

	case "ConfirmDevice":
		if session.TenantID == "" {
			return errorResponse(msg.ID, fmt.Errorf("authentication required"))
		}
		var req pb.ConfirmDeviceRequest
		if err := json.Unmarshal(msg.Request, &req); err != nil {
			return errorResponse(msg.ID, fmt.Errorf("invalid request: %s", err.Error()))
		}
		req.TenantId = session.TenantID
		resp, err := client.ConfirmDevice(ctx, &req)
		if err != nil {
			return errorResponse(msg.ID, err)
		}
		return protoResponse(msg.ID, resp)

	default:
		return errorResponse(msg.ID, fmt.Errorf("unknown method: %s", msg.Method))
	}
}

// handleTenantBilling handles TenantBillingService calls.
func (p *TenantProxy) handleTenantBilling(ctx context.Context, msg *Message, session *TenantSession) *Response {
	// Require authentication for all billing operations
	if session.TenantID == "" {
		return errorResponse(msg.ID, fmt.Errorf("authentication required"))
	}

	client := p.services.TenantBilling

	switch msg.Method {
	case "GetSubscriptionStatus":
		var req pb.GetSubscriptionStatusRequest
		if err := json.Unmarshal(msg.Request, &req); err != nil {
			return errorResponse(msg.ID, fmt.Errorf("invalid request: %s", err.Error()))
		}
		req.TenantId = session.TenantID
		resp, err := client.GetSubscriptionStatus(ctx, &req)
		if err != nil {
			// Graceful fallback for free tier or unavailable billing
			log.Debug().Err(err).Msg("GetSubscriptionStatus unavailable")
			return successResponse(msg.ID, map[string]any{
				"current_tier":        "FREE",
				"subscription_status": "none",
				"is_free_tier":        true,
			})
		}
		return protoResponse(msg.ID, resp)

	case "GetBillingPortal":
		var req pb.GetBillingPortalRequest
		if err := json.Unmarshal(msg.Request, &req); err != nil {
			return errorResponse(msg.ID, fmt.Errorf("invalid request: %s", err.Error()))
		}
		req.TenantId = session.TenantID
		resp, err := client.GetBillingPortal(ctx, &req)
		if err != nil {
			return errorResponse(msg.ID, err)
		}
		return protoResponse(msg.ID, resp)

	case "GetBillingHistory":
		var req pb.GetBillingHistoryRequest
		if err := json.Unmarshal(msg.Request, &req); err != nil {
			return errorResponse(msg.ID, fmt.Errorf("invalid request: %s", err.Error()))
		}
		req.TenantId = session.TenantID
		resp, err := client.GetBillingHistory(ctx, &req)
		if err != nil {
			return errorResponse(msg.ID, err)
		}
		return protoResponse(msg.ID, resp)

	case "CreateSetupIntent":
		// No request body parsing needed as it just uses the session's TenantID
		req := &pb.CreateBillingSetupIntentRequest{
			TenantId: session.TenantID,
		}
		resp, err := client.CreateSetupIntent(ctx, req)
		if err != nil {
			return errorResponse(msg.ID, err)
		}
		return protoResponse(msg.ID, resp)

	case "ChangeTier":
		// Parse JSON manually to handle tier string conversion
		var rawReq map[string]any
		if err := json.Unmarshal(msg.Request, &rawReq); err != nil {
			return errorResponse(msg.ID, fmt.Errorf("invalid request: %s", err.Error()))
		}
		req := &pb.ChangeTierRequest{}
		req.TenantId = session.TenantID
		// Handle new_tier as int or string
		if tierInt, ok := rawReq["new_tier"].(float64); ok {
			req.NewTier = pb.AccountTier(int32(tierInt))
		} else if tierStr, ok := rawReq["new_tier"].(string); ok {
			req.NewTier = parseTierString(tierStr)
		}
		if returnUrl, ok := rawReq["return_url"].(string); ok {
			req.ReturnUrl = returnUrl
		}
		resp, err := client.ChangeTier(ctx, req)
		if err != nil {
			return errorResponse(msg.ID, err)
		}
		return protoResponse(msg.ID, resp)

	case "CancelSubscription":
		var req pb.CancelSubscriptionRequest
		if err := json.Unmarshal(msg.Request, &req); err != nil {
			return errorResponse(msg.ID, fmt.Errorf("invalid request: %s", err.Error()))
		}
		req.TenantId = session.TenantID
		resp, err := client.CancelSubscription(ctx, &req)
		if err != nil {
			return errorResponse(msg.ID, err)
		}
		return protoResponse(msg.ID, resp)

	case "ContactSales":
		var req pb.ContactSalesRequest
		if err := json.Unmarshal(msg.Request, &req); err != nil {
			return errorResponse(msg.ID, fmt.Errorf("invalid request: %s", err.Error()))
		}
		req.TenantId = session.TenantID
		resp, err := client.ContactSales(ctx, &req)
		if err != nil {
			return errorResponse(msg.ID, err)
		}
		return protoResponse(msg.ID, resp)

	default:
		return errorResponse(msg.ID, fmt.Errorf("unknown billing method: %s", msg.Method))
	}
}

// handleTenantPeerService handles peer management for tenants.
// Uses TenantPortalService methods which are tenant-scoped (not admin PeerService).
func (p *TenantProxy) handleTenantPeerService(ctx context.Context, msg *Message, session *TenantSession) *Response {
	// Require authentication for all peer operations
	if session.TenantID == "" {
		return errorResponse(msg.ID, fmt.Errorf("authentication required"))
	}

	callerTenantID := session.TenantID

	// Use TenantPortalService for peer operations (tenant-scoped, not admin-only)
	portalClient := p.services.TenantPortal

	switch msg.Method {
	case "ListTenantPeers":
		if err := session.checkSharePerm("view_peers"); err != nil {
			return errorResponse(msg.ID, err)
		}
		tokenPreview := ""
		if session.SessionToken != "" {
			if len(session.SessionToken) > 8 {
				tokenPreview = session.SessionToken[:8] + "..."
			} else {
				tokenPreview = session.SessionToken
			}
		}
		log.Debug().
			Str("session_id", session.ID).
			Str("tenant_id", callerTenantID).
			Str("active_share_tenant_id", session.ActiveShareTenantID).
			Bool("has_session_token", session.SessionToken != "").
			Str("token_preview", tokenPreview).
			Msg("[proxy] ListTenantPeers → sending to gRPC backend")
		resp, err := portalClient.ListTenantPeers(ctx, &pb.ListTenantPeersRequest{
			TenantId: callerTenantID,
		})
		if err != nil {
			log.Debug().Err(err).Str("caller_tenant_id", callerTenantID).Msg("[proxy] ListTenantPeers gRPC error")
			return errorResponse(msg.ID, err)
		}
		log.Debug().
			Str("caller_tenant_id", callerTenantID).
			Int("peer_count", len(resp.Peers)).
			Msg("[proxy] ListTenantPeers response received")
		if session.IsViewingSharedAccount() {
			// Compatibility fallback while older cores still return owner-shaped payloads.
			resp.Peers = session.filterPeersByTagFilter(resp.Peers)
			if session.needsPeerFocusedShareFallback(resp.Peers) {
				session.enrichPeerListForFocusedShare(resp.Peers)
			}
		}
		return protoResponse(msg.ID, resp)

	case "AddTenantPeer":
		if err := session.checkSharePerm("manage_peers"); err != nil {
			return errorResponse(msg.ID, err)
		}
		var reqData struct {
			Name           string `json:"name"`
			PublicKey      string `json:"public_key"`
			ConnectionType int32  `json:"connection_type"` // 0 = wireguard, 1 = ipsec
		}
		if err := json.Unmarshal(msg.Request, &reqData); err != nil {
			return errorResponse(msg.ID, fmt.Errorf("invalid request: %s", err.Error()))
		}
		resp, err := portalClient.AddTenantPeer(ctx, &pb.AddTenantPeerRequest{
			TenantId:  callerTenantID,
			Name:      reqData.Name,
			PublicKey: reqData.PublicKey,
		})
		if err != nil {
			return errorResponse(msg.ID, err)
		}
		return protoResponse(msg.ID, resp)

	case "RemoveTenantPeer":
		if err := session.checkSharePerm("manage_peers"); err != nil {
			return errorResponse(msg.ID, err)
		}
		var reqData struct {
			PeerId string `json:"peer_id"`
		}
		if err := json.Unmarshal(msg.Request, &reqData); err != nil {
			return errorResponse(msg.ID, fmt.Errorf("invalid request: %s", err.Error()))
		}
		resp, err := portalClient.RemoveTenantPeer(ctx, &pb.RemoveTenantPeerRequest{
			TenantId: callerTenantID,
			PeerId:   reqData.PeerId,
		})
		if err != nil {
			return errorResponse(msg.ID, err)
		}
		return successResponse(msg.ID, map[string]any{
			"success": resp.Success,
			"message": resp.Message,
		})

	case "UpdateTenantPeer":
		if err := session.checkSharePerm("manage_peers"); err != nil {
			return errorResponse(msg.ID, err)
		}
		var reqData struct {
			PeerId string   `json:"peer_id"`
			Name   string   `json:"name"`
			Tags   []string `json:"tags"`
		}
		if err := json.Unmarshal(msg.Request, &reqData); err != nil {
			return errorResponse(msg.ID, fmt.Errorf("invalid request: %s", err.Error()))
		}

		resp, err := portalClient.UpdateTenantPeer(ctx, &pb.UpdateTenantPeerRequest{
			TenantId: callerTenantID,
			PeerId:   reqData.PeerId,
			Name:     reqData.Name,
			Tags:     reqData.Tags,
		})
		if err != nil {
			return errorResponse(msg.ID, err)
		}
		return successResponse(msg.ID, map[string]any{
			"success": resp.Success,
			"message": resp.Message,
			"peer": map[string]any{
				"id":          resp.Peer.Id,
				"name":        resp.Peer.Name,
				"public_key":  resp.Peer.PublicKey,
				"assigned_ip": resp.Peer.AssignedIp,
				"is_online":   resp.Peer.IsOnline,
				"tags":        resp.Peer.Tags,
			},
		})

	case "UpdateTenantPeerNotes":
		if err := session.checkSharePerm("manage_peers"); err != nil {
			return errorResponse(msg.ID, err)
		}
		var reqData struct {
			PeerId string `json:"peer_id"`
			Notes  string `json:"notes"`
		}
		if err := json.Unmarshal(msg.Request, &reqData); err != nil {
			return errorResponse(msg.ID, fmt.Errorf("invalid request: %s", err.Error()))
		}

		peerResp, err := portalClient.GetTenantPeer(ctx, &pb.GetTenantPeerRequest{
			TenantId: callerTenantID,
			PeerId:   reqData.PeerId,
		})
		if err != nil {
			return errorResponse(msg.ID, fmt.Errorf("failed to resolve peer owner: %w", err))
		}
		if peerResp.Peer == nil || peerResp.Peer.AccountId == "" {
			return errorResponse(msg.ID, fmt.Errorf("peer owner account not found"))
		}

		peerClient := p.services.Peer
		resp, err := peerClient.UpdatePeerNotes(ctx, &pb.UpdatePeerNotesRequest{
			AccountId: peerResp.Peer.AccountId,
			PeerId:    reqData.PeerId,
			Notes:     reqData.Notes,
		})
		if err != nil {
			return errorResponse(msg.ID, err)
		}
		return successResponse(msg.ID, map[string]any{
			"success": true,
			"message": "Notes updated",
			"peer": map[string]any{
				"id":    resp.Peer.Id,
				"notes": resp.Peer.Notes,
			},
		})

	case "BatchUpdateTenantPeers":
		if err := session.checkSharePerm("manage_peers"); err != nil {
			return errorResponse(msg.ID, err)
		}
		var reqData struct {
			PeerIds         []string `json:"peer_ids"`
			Operation       int32    `json:"operation"`
			SequencePattern string   `json:"sequence_pattern"`
			SequenceStart   int32    `json:"sequence_start"`
			Tags            []string `json:"tags"`
		}
		if err := json.Unmarshal(msg.Request, &reqData); err != nil {
			return errorResponse(msg.ID, fmt.Errorf("invalid request: %s", err.Error()))
		}

		resp, err := portalClient.BatchUpdateTenantPeers(ctx, &pb.BatchUpdatePeersRequest{
			TenantId:        callerTenantID,
			PeerIds:         reqData.PeerIds,
			Operation:       pb.BatchUpdatePeersRequest_Operation(reqData.Operation),
			SequencePattern: reqData.SequencePattern,
			SequenceStart:   reqData.SequenceStart,
			Tags:            reqData.Tags,
		})
		if err != nil {
			return errorResponse(msg.ID, err)
		}
		return successResponse(msg.ID, map[string]any{
			"success":       resp.Success,
			"message":       resp.Message,
			"updated_count": resp.UpdatedCount,
		})

	case "GetTenantPeerConfig":
		// Use tenant-specific GetTenantPeerConfig which handles TenantID -> OverlayAccountID
		var reqData struct {
			PeerId   string `json:"peer_id"`
			Endpoint string `json:"endpoint"`
		}
		if err := json.Unmarshal(msg.Request, &reqData); err != nil {
			return errorResponse(msg.ID, fmt.Errorf("invalid request: %s", err.Error()))
		}

		peerPortalClient := p.tenantPortalSvc(reqData.PeerId)
		resp, err := peerPortalClient.GetTenantPeerConfig(ctx, &pb.GetTenantPeerConfigRequest{
			TenantId: callerTenantID,
			PeerId:   reqData.PeerId,
			Endpoint: reqData.Endpoint,
		})
		if err != nil {
			return errorResponse(msg.ID, err)
		}

		// Build response with IPsec config if present
		result := map[string]any{
			"wg_config": resp.WgConfig,
			"qr_code":   resp.QrCode,
		}
		return successResponse(msg.ID, result)

	case "GetTenantPeer":
		var req pb.GetTenantPeerRequest
		if err := json.Unmarshal(msg.Request, &req); err != nil {
			return errorResponse(msg.ID, fmt.Errorf("invalid request: %s", err.Error()))
		}
		// Use effective tenant ID (shared account if viewing one)
		req.TenantId = callerTenantID

		peerPortalClient := p.tenantPortalSvc(req.PeerId)
		resp, err := peerPortalClient.GetTenantPeer(ctx, &req)
		if err != nil {
			return errorResponse(msg.ID, err)
		}
		if resp.Peer == nil {
			return errorResponse(msg.ID, fmt.Errorf("peer not found"))
		}
		if session.IsViewingSharedAccount() && session.needsPeerFocusedShareFallback([]*pb.Peer{resp.Peer}) {
			session.enrichPeerForFocusedShare(resp.Peer)
		}
		return protoResponse(msg.ID, resp)

	case "GetTenantPeerStats":
		var req pb.GetTenantPeerStatsRequest
		if err := json.Unmarshal(msg.Request, &req); err != nil {
			return errorResponse(msg.ID, fmt.Errorf("invalid request: %s", err.Error()))
		}
		// Use effective tenant ID (shared account if viewing one)
		req.TenantId = callerTenantID

		peerPortalClient := p.tenantPortalSvc(req.PeerId)
		resp, err := peerPortalClient.GetTenantPeerStats(ctx, &req)
		if err != nil {
			return errorResponse(msg.ID, err)
		}
		return protoResponse(msg.ID, resp)

	case "PingTenantPeer":
		var req pb.PingTenantPeerRequest
		if err := json.Unmarshal(msg.Request, &req); err != nil {
			return errorResponse(msg.ID, fmt.Errorf("invalid request: %s", err.Error()))
		}
		req.TenantId = callerTenantID

		// Use a separate context for the streaming goroutine — the parent ctx
		// gets cancelled when handleMessage returns, which would kill the stream.
		// Copy the CallContext from the parent ctx so auth identity propagates.
		streamCtx, streamCancel := context.WithTimeout(context.Background(), 30*time.Second)
		if cc := auth.CallContextFrom(ctx); cc != nil {
			cp := *cc
			streamCtx = auth.WithCallContext(streamCtx, &cp)
		}

		// In-process server-stream — no proto marshalling between the
		// service handler and this goroutine. Each ping event travels as
		// a *pb.PingEvent pointer through a Go channel.
		peerPortalSvc := p.tenantPortalSvc(req.PeerId)
		if peerPortalSvc == nil {
			streamCancel()
			return errorResponse(msg.ID, fmt.Errorf("TenantPortal service not configured"))
		}
		stream := NewLocalServerStream[pb.PingEvent](streamCtx, 64)
		reqPtr := &req // capture pointer; the goroutine never mutates the value
		go func() {
			if err := peerPortalSvc.StreamPingTenantPeer(reqPtr, stream); err != nil && streamCtx.Err() == nil {
				log.Warn().Err(err).Msg("StreamPingTenantPeer handler exited")
			}
			stream.Close()
		}()

		go func() {
			defer streamCancel()
			p.sendResponse(session, &Response{ID: msg.ID, Type: "stream_started"})
			for {
				event, err := stream.Recv()
				if err != nil {
					break
				}
				jsonData := marshalProtoResponse(event)
				p.sendResponse(session, &Response{ID: msg.ID, Type: "stream_data", Response: jsonData})
			}
			p.sendResponse(session, &Response{ID: msg.ID, Type: "stream_end"})
		}()
		return nil

	case "SetPeerNotification":
		var req pb.SetPeerNotificationRequest
		if err := json.Unmarshal(msg.Request, &req); err != nil {
			return errorResponse(msg.ID, fmt.Errorf("invalid request: %s", err.Error()))
		}
		// Use effective tenant ID (shared account if viewing one)
		req.TenantId = callerTenantID
		resp, err := portalClient.SetPeerNotification(ctx, &req)
		if err != nil {
			return errorResponse(msg.ID, err)
		}
		return protoResponse(msg.ID, resp)

	// ====== WINBOX SESSION MANAGEMENT ======
	case "CreateTenantWinboxSession":
		if err := session.checkSharePerm("manage_winbox"); err != nil {
			return errorResponse(msg.ID, err)
		}
		var req pb.CreateTenantWinboxSessionRequest
		if err := json.Unmarshal(msg.Request, &req); err != nil {
			return errorResponse(msg.ID, fmt.Errorf("invalid request: %s", err.Error()))
		}
		// Use effective tenant ID (shared account if viewing one)
		req.TenantId = callerTenantID

		resp, err := portalClient.CreateTenantWinboxSession(ctx, &req)
		if err != nil {
			return errorResponse(msg.ID, err)
		}
		if session.needsWinboxFocusedShareFallback(resp.Session) {
			session.enrichWinboxForFocusedShare(resp.Session)
		}
		return protoResponse(msg.ID, resp)

	case "ListTenantWinboxSessions":
		if err := session.checkSharePerm("view_peers"); err != nil {
			return errorResponse(msg.ID, err)
		}
		var req pb.ListTenantWinboxSessionsRequest
		if err := json.Unmarshal(msg.Request, &req); err != nil {
			return errorResponse(msg.ID, fmt.Errorf("invalid request: %s", err.Error()))
		}
		// Use effective tenant ID (shared account if viewing one)
		req.TenantId = callerTenantID

		resp, err := portalClient.ListTenantWinboxSessions(ctx, &req)
		if err != nil {
			return errorResponse(msg.ID, err)
		}
		if session.needsWinboxListFocusedShareFallback(resp.Sessions) {
			session.enrichWinboxListForFocusedShare(resp.Sessions)
		}
		return protoResponse(msg.ID, resp)

	case "GetTenantWinboxSession":
		if err := session.checkSharePerm("view_peers"); err != nil {
			return errorResponse(msg.ID, err)
		}
		var req pb.GetTenantWinboxSessionRequest
		if err := json.Unmarshal(msg.Request, &req); err != nil {
			return errorResponse(msg.ID, fmt.Errorf("invalid request: %s", err.Error()))
		}
		// Use effective tenant ID (shared account if viewing one)
		req.TenantId = callerTenantID

		resp, err := portalClient.GetTenantWinboxSession(ctx, &req)
		if err != nil {
			return errorResponse(msg.ID, err)
		}
		if resp.Session == nil {
			return errorResponse(msg.ID, fmt.Errorf("session not found"))
		}
		if session.needsWinboxFocusedShareFallback(resp.Session) {
			session.enrichWinboxForFocusedShare(resp.Session)
		}
		return protoResponse(msg.ID, resp)

	case "UpdateTenantWinboxSession":
		if err := session.checkSharePerm("manage_winbox"); err != nil {
			return errorResponse(msg.ID, err)
		}
		var req pb.UpdateTenantWinboxSessionRequest
		if err := json.Unmarshal(msg.Request, &req); err != nil {
			return errorResponse(msg.ID, fmt.Errorf("invalid request: %s", err.Error()))
		}
		// Use effective tenant ID (shared account if viewing one)
		req.TenantId = callerTenantID

		resp, err := portalClient.UpdateTenantWinboxSession(ctx, &req)
		if err != nil {
			return errorResponse(msg.ID, err)
		}
		if session.needsWinboxFocusedShareFallback(resp.Session) {
			session.enrichWinboxForFocusedShare(resp.Session)
		}
		return protoResponse(msg.ID, resp)

	case "DeleteTenantWinboxSession":
		if err := session.checkSharePerm("manage_winbox"); err != nil {
			return errorResponse(msg.ID, err)
		}
		var req pb.DeleteTenantWinboxSessionRequest
		if err := json.Unmarshal(msg.Request, &req); err != nil {
			return errorResponse(msg.ID, fmt.Errorf("invalid request: %s", err.Error()))
		}
		// Use effective tenant ID (shared account if viewing one)
		req.TenantId = callerTenantID

		resp, err := portalClient.DeleteTenantWinboxSession(ctx, &req)
		if err != nil {
			return errorResponse(msg.ID, err)
		}
		return protoResponse(msg.ID, resp)

	case "ClearTenantWinboxCredentials":
		if err := session.checkSharePerm("manage_winbox"); err != nil {
			return errorResponse(msg.ID, err)
		}
		var req pb.ClearTenantWinboxCredentialsRequest
		if err := json.Unmarshal(msg.Request, &req); err != nil {
			return errorResponse(msg.ID, fmt.Errorf("invalid request: %s", err.Error()))
		}
		// Use effective tenant ID (shared account if viewing one)
		req.TenantId = callerTenantID

		resp, err := portalClient.ClearTenantWinboxCredentials(ctx, &req)
		if err != nil {
			return errorResponse(msg.ID, err)
		}
		return protoResponse(msg.ID, resp)

	default:
		return errorResponse(msg.ID, fmt.Errorf("unknown peer method: %s", msg.Method))
	}
}

// handleTenantNetworkService handles NetworkService calls for tenants.
func (p *TenantProxy) handleTenantNetworkService(ctx context.Context, msg *Message, session *TenantSession) *Response {
	// Require authentication
	if session.TenantID == "" {
		return errorResponse(msg.ID, fmt.Errorf("authentication required"))
	}

	switch msg.Method {
	case "GetNetworkStats":
		if err := session.checkSharePerm("view_activity"); err != nil {
			return errorResponse(msg.ID, err)
		}
		// TODO(network): re-introduce when NetworkService is implemented.
		// The legacy NetworkServiceClient has no in-process server backing
		// it after the Phase 5b refactor; surface a clear error rather than
		// invoking a stub that would return Unimplemented anyway.
		return errorResponse(msg.ID, fmt.Errorf("network stats are not available in this build"))

	case "GetTenantTopology":
		if err := session.checkSharePerm("view_topology"); err != nil {
			return errorResponse(msg.ID, err)
		}
		// Use TenantPortalService for topology (tenant-allowed service)
		client := p.services.TenantPortal
		req := &pb.GetTenantTopologyRequest{
			TenantId: session.TenantID,
		}

		resp, err := client.GetTenantTopology(ctx, req)
		if err != nil {
			return errorResponse(msg.ID, err)
		}
		return protoResponse(msg.ID, resp)

	default:
		return errorResponse(msg.ID, fmt.Errorf("unknown network method: %s", msg.Method))
	}
}

// handleTenantWebSSHService handles WebSSHService calls for tenant portal.
func (p *TenantProxy) handleTenantWebSSHService(ctx context.Context, msg *Message, session *TenantSession) *Response {
	client := p.services.TenantPortal

	// Use effective tenant ID - this is the shared account if viewing one, otherwise own account
	callerTenantID := session.TenantID

	switch msg.Method {
	case "CreateTenantWebSSHSession":
		if err := session.checkSharePerm("manage_webssh"); err != nil {
			return errorResponse(msg.ID, err)
		}
		var req pb.CreateTenantWebSSHSessionRequest
		if err := json.Unmarshal(msg.Request, &req); err != nil {
			return errorResponse(msg.ID, fmt.Errorf("invalid request: %s", err.Error()))
		}
		// Use effective tenant ID (shared account if viewing one)
		req.TenantId = callerTenantID

		// Set defaults
		if req.SshPort == 0 {
			req.SshPort = 22
		}
		if req.TerminalRows == 0 {
			req.TerminalRows = 24
		}
		if req.TerminalCols == 0 {
			req.TerminalCols = 80
		}

		resp, err := client.CreateTenantWebSSHSession(ctx, &req)
		if err != nil {
			return errorResponse(msg.ID, fmt.Errorf("failed to create SSH session: %w", err))
		}
		return protoResponse(msg.ID, resp)

	case "GetTenantWebSSHSession":
		if err := session.checkSharePerm("view_peers"); err != nil {
			return errorResponse(msg.ID, err)
		}
		var req pb.GetTenantWebSSHSessionRequest
		if err := json.Unmarshal(msg.Request, &req); err != nil {
			return errorResponse(msg.ID, fmt.Errorf("invalid request: %s", err.Error()))
		}

		// Use effective tenant ID (shared account if viewing one)
		req.TenantId = callerTenantID

		resp, err := client.GetTenantWebSSHSession(ctx, &req)
		if err != nil {
			return errorResponse(msg.ID, fmt.Errorf("failed to get SSH session: %w", err))
		}

		if resp.Session == nil {
			return errorResponse(msg.ID, fmt.Errorf("session not found"))
		}
		if session.needsWebSSHFocusedShareFallback(resp.Session) {
			session.enrichWebSSHForFocusedShare(resp.Session)
		}
		return protoResponse(msg.ID, resp)

	case "ListTenantWebSSHSessions":
		if err := session.checkSharePerm("view_peers"); err != nil {
			return errorResponse(msg.ID, err)
		}
		// Use effective tenant ID (shared account if viewing one)
		resp, err := client.ListTenantWebSSHSessions(ctx, &pb.ListTenantWebSSHSessionsRequest{
			TenantId: callerTenantID,
		})
		if err != nil {
			return errorResponse(msg.ID, fmt.Errorf("failed to list SSH sessions: %w", err))
		}
		if session.needsWebSSHListFocusedShareFallback(resp.Sessions) {
			session.enrichWebSSHListForFocusedShare(resp.Sessions)
		}
		return protoResponse(msg.ID, resp)

	case "DisconnectTenantWebSSHSession":
		if err := session.checkSharePerm("manage_webssh"); err != nil {
			return errorResponse(msg.ID, err)
		}
		var req pb.DisconnectTenantWebSSHSessionRequest
		if err := json.Unmarshal(msg.Request, &req); err != nil {
			return errorResponse(msg.ID, fmt.Errorf("invalid request: %s", err.Error()))
		}

		// Use effective tenant ID (shared account if viewing one)
		req.TenantId = callerTenantID

		resp, err := client.DisconnectTenantWebSSHSession(ctx, &req)
		if err != nil {
			return errorResponse(msg.ID, fmt.Errorf("failed to disconnect SSH session: %w", err))
		}
		return protoResponse(msg.ID, resp)

	default:
		return errorResponse(msg.ID, fmt.Errorf("unknown WebSSH method: %s", msg.Method))
	}
}

// handleTenantDataService handles TenantDataService calls for backup/restore.
func (p *TenantProxy) handleTenantDataService(ctx context.Context, msg *Message, session *TenantSession) *Response {
	// Require authentication for all data operations
	if session.TenantID == "" {
		return errorResponse(msg.ID, fmt.Errorf("authentication required"))
	}

	client := p.services.TenantData

	switch msg.Method {
	case "RequestBackup":
		resp, err := client.RequestBackup(ctx, &pb.RequestBackupRequest{
			TenantId: session.TenantID,
		})
		if err != nil {
			return errorResponse(msg.ID, fmt.Errorf("failed to request backup: %w", err))
		}
		return protoResponse(msg.ID, resp)

	case "ListBackups":
		resp, err := client.ListBackups(ctx, &pb.ListBackupsRequest{
			TenantId: session.TenantID,
		})
		if err != nil {
			return errorResponse(msg.ID, fmt.Errorf("failed to list backups: %w", err))
		}
		return protoResponse(msg.ID, resp)

	case "GetBackupDownloadURL":
		var req pb.GetBackupDownloadURLRequest
		if err := json.Unmarshal(msg.Request, &req); err != nil {
			return errorResponse(msg.ID, fmt.Errorf("invalid request: %s", err.Error()))
		}
		req.TenantId = session.TenantID
		resp, err := client.GetBackupDownloadURL(ctx, &req)
		if err != nil {
			return errorResponse(msg.ID, fmt.Errorf("failed to get backup download URL: %w", err))
		}
		return protoResponse(msg.ID, resp)

	case "RestoreFromBackup":
		var req pb.RestoreFromBackupRequest
		if err := json.Unmarshal(msg.Request, &req); err != nil {
			return errorResponse(msg.ID, fmt.Errorf("invalid request: %s", err.Error()))
		}
		req.TenantId = session.TenantID
		resp, err := client.RestoreFromBackup(ctx, &req)
		if err != nil {
			return errorResponse(msg.ID, fmt.Errorf("failed to restore backup: %w", err))
		}
		return protoResponse(msg.ID, resp)

	case "RestoreBackup":
		var req pb.RestoreBackupRequest
		if err := json.Unmarshal(msg.Request, &req); err != nil {
			return errorResponse(msg.ID, fmt.Errorf("invalid request: %s", err.Error()))
		}
		req.TenantId = session.TenantID
		resp, err := client.RestoreBackup(ctx, &req)
		if err != nil {
			return errorResponse(msg.ID, fmt.Errorf("failed to restore backup: %w", err))
		}
		return protoResponse(msg.ID, resp)

	case "GetRestoreStatus":
		var req pb.GetRestoreStatusRequest
		if err := json.Unmarshal(msg.Request, &req); err != nil {
			return errorResponse(msg.ID, fmt.Errorf("invalid request: %s", err.Error()))
		}
		req.TenantId = session.TenantID
		resp, err := client.GetRestoreStatus(ctx, &req)
		if err != nil {
			return errorResponse(msg.ID, fmt.Errorf("failed to get restore status: %w", err))
		}
		return protoResponse(msg.ID, resp)
	case "DeleteBackup":
		var req pb.DeleteBackupRequest
		if err := json.Unmarshal(msg.Request, &req); err != nil {
			return errorResponse(msg.ID, fmt.Errorf("invalid request: %s", err.Error()))
		}
		req.TenantId = session.TenantID
		resp, err := client.DeleteBackup(ctx, &req)
		if err != nil {
			return errorResponse(msg.ID, fmt.Errorf("failed to delete backup: %w", err))
		}
		return protoResponse(msg.ID, resp)
	default:
		return errorResponse(msg.ID, fmt.Errorf("unknown data service method: %s", msg.Method))
	}
}

// handleWebProxyService handles WebProxyService calls for tenant portal.
func (p *TenantProxy) handleWebProxyService(ctx context.Context, msg *Message, session *TenantSession) *Response {
	// Require authentication for all web proxy operations
	if session.TenantID == "" {
		return errorResponse(msg.ID, fmt.Errorf("authentication required"))
	}

	client := p.services.WebProxy

	switch msg.Method {
	case "CreateWebProxySession":
		var req pb.CreateWebProxySessionRequest
		if err := json.Unmarshal(msg.Request, &req); err != nil {
			return errorResponse(msg.ID, fmt.Errorf("invalid request: %s", err.Error()))
		}
		// Force tenant's ID for security
		req.TenantId = session.TenantID

		resp, err := client.CreateWebProxySession(ctx, &req)
		if err != nil {
			return errorResponse(msg.ID, fmt.Errorf("failed to create web proxy session: %w", err))
		}
		return protoResponse(msg.ID, resp)

	case "GetWebProxySession":
		var req pb.GetWebProxySessionRequest
		if err := json.Unmarshal(msg.Request, &req); err != nil {
			return errorResponse(msg.ID, fmt.Errorf("invalid request: %s", err.Error()))
		}

		resp, err := client.GetWebProxySession(ctx, &req)
		if err != nil {
			return errorResponse(msg.ID, fmt.Errorf("failed to get web proxy session: %w", err))
		}
		// Verify the session belongs to this tenant
		if resp.Session != nil && resp.Session.TenantId != session.TenantID {
			return errorResponse(msg.ID, fmt.Errorf("session not found"))
		}
		return protoResponse(msg.ID, resp)

	case "ListWebProxySessions":
		resp, err := client.ListWebProxySessions(ctx, &pb.ListWebProxySessionsRequest{
			TenantId: session.TenantID,
		})
		if err != nil {
			return errorResponse(msg.ID, fmt.Errorf("failed to list web proxy sessions: %w", err))
		}
		return protoResponse(msg.ID, resp)

	case "CloseWebProxySession":
		var req pb.CloseWebProxySessionRequest
		if err := json.Unmarshal(msg.Request, &req); err != nil {
			return errorResponse(msg.ID, fmt.Errorf("invalid request: %s", err.Error()))
		}

		// Verify the session belongs to this tenant before closing
		getResp, err := client.GetWebProxySession(ctx, &pb.GetWebProxySessionRequest{
			SessionId: req.SessionId,
		})
		if err != nil {
			return errorResponse(msg.ID, fmt.Errorf("failed to verify web proxy session: %w", err))
		}
		if getResp.Session == nil || getResp.Session.TenantId != session.TenantID {
			return errorResponse(msg.ID, fmt.Errorf("session not found"))
		}

		resp, err := client.CloseWebProxySession(ctx, &req)
		if err != nil {
			return errorResponse(msg.ID, fmt.Errorf("failed to close web proxy session: %w", err))
		}
		return protoResponse(msg.ID, resp)

	default:
		return errorResponse(msg.ID, fmt.Errorf("unknown WebProxy method: %s", msg.Method))
	}
}

// keepAlive sends periodic pings to keep tenant WebSocket alive.
func (p *TenantProxy) keepAlive(conn *websocket.Conn, session *TenantSession) {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	for range ticker.C {
		session.mu.Lock()
		conn.SetWriteDeadline(time.Now().Add(10 * time.Second))
		if err := conn.WriteMessage(websocket.PingMessage, nil); err != nil {
			session.mu.Unlock()
			log.Debug().Str("session_id", session.ID).Msg("Tenant WebSocket ping failed")
			conn.Close()
			return
		}
		session.mu.Unlock()
	}
}

// Close closes the tenant proxy and all connections.
func (p *TenantProxy) Close() error {
	p.sessionsMu.Lock()
	defer p.sessionsMu.Unlock()

	for _, session := range p.sessions {
		session.Conn.Close()
	}

	if p.router != nil {
		p.router.Close()
	}
	return nil
}

// protoResponse wraps a proto message directly as a response without custom mapping.
// Uses json.Marshal with Go struct reflection so manually added proto fields
// (is_shared, owner_name, viewer_can_write) with their json tags are included.
// proxyGenerateQRCode generates a base64-encoded PNG QR code for the given content.
func proxyGenerateQRCode(content string) string {
	png, err := qrcode.Encode(content, qrcode.Medium, 256)
	if err != nil {
		return ""
	}
	return base64.StdEncoding.EncodeToString(png)
}

// shareInfoToMap converts an AccessShareInfo proto struct to a plain map so that
// fields added directly to the Go struct (e.g. IsLinkShare) are included in the
// JSON response even though they are absent from the compiled proto rawDesc.
func shareInfoToMap(sh *pb.AccessShareInfo) map[string]any {
	if sh == nil {
		return nil
	}
	m := map[string]any{
		"share_id":        sh.ShareId,
		"owner_tenant_id": sh.OwnerTenantId,
		"owner_email":     sh.OwnerEmail,
		"owner_name":      sh.OwnerName,
		"shared_email":    sh.SharedEmail,
		"sharee_name":     sh.ShareeName,
		"status":          sh.Status,
		"invite_token":    sh.InviteToken,
		"is_link_share":   sh.IsLinkShare,
		"resend_count":    sh.ResendCount,
		"tag_filter":      sh.TagFilter,
	}
	if sh.Permissions != nil {
		m["permissions"] = map[string]any{
			"devices_read":  sh.Permissions.ViewPeers,
			"devices_write": sh.Permissions.ManagePeers,
		}
	}
	if sh.CreatedAt != nil {
		m["created_at"] = sh.CreatedAt.AsTime().Format("2006-01-02T15:04:05Z")
	}
	if sh.AcceptedAt != nil {
		m["accepted_at"] = sh.AcceptedAt.AsTime().Format("2006-01-02T15:04:05Z")
	}
	if sh.ExpiresAt != nil {
		m["expires_at"] = sh.ExpiresAt.AsTime().Format("2006-01-02T15:04:05Z")
	}
	if sh.LastResendAt != nil {
		m["last_resend_at"] = sh.LastResendAt.AsTime().Format("2006-01-02T15:04:05Z")
	}
	return m
}

func protoResponse(id string, protoMsg any) *Response {
	jsonData := marshalProtoResponse(protoMsg)
	return &Response{
		ID:       id,
		Type:     "response",
		Response: jsonData,
	}
}

// marshalProtoResponse serializes a response DTO to JSON. Native Go types
// in internal/types carry json tags; bool fields drop omitempty so explicit
// false values (e.g. viewer_can_write) surface in the output. No proto
// runtime / reflection involved.
func marshalProtoResponse(protoMsg any) json.RawMessage {
	jsonData, _ := json.Marshal(protoMsg)
	return jsonData
}

// ===== SSH Stream Handlers for gRPC streaming over WebSocket =====

// SSHStreamPayload represents the payload for SSH stream messages
type SSHStreamPayload struct {
	Input  *SSHInputPayload  `json:"input,omitempty"`
	Resize *SSHResizePayload `json:"resize,omitempty"`
	Ping   *SSHPingPayload   `json:"ping,omitempty"`
}

type SSHInputPayload struct {
	Data string `json:"data"` // base64 encoded
}

type SSHResizePayload struct {
	Rows int `json:"rows"`
	Cols int `json:"cols"`
}

type SSHPingPayload struct {
	Timestamp int64 `json:"timestamp"`
}

type sshStreamWSMessage struct {
	Type      string             `json:"type"`
	SessionID string             `json:"session_id"`
	Payload   sshStreamWSPayload `json:"payload"`
}

type sshStreamWSPayload struct {
	Output *sshStreamWSOutput `json:"output,omitempty"`
	Ping   *sshStreamWSPing   `json:"ping,omitempty"`
	Close  bool               `json:"close,omitempty"`
}

type sshStreamWSOutput struct {
	Data  string `json:"data,omitempty"`
	Error string `json:"error,omitempty"`
}

type sshStreamWSPing struct {
	Timestamp int64 `json:"timestamp"`
}

// handleSSHStreamStart initiates a bidirectional SSH stream for a session
func (p *TenantProxy) handleSSHStreamStart(wsSession *TenantSession, sshSessionID string) {
	log.Debug().
		Str("ws_session", wsSession.ID).
		Str("ssh_session", sshSessionID).
		Msg(" Starting SSH stream")

	var (
		ctx     context.Context
		cancel  context.CancelFunc
		handler *SSHStreamHandler
	)

	for {
		wsSession.sshStreamsMu.Lock()
		if wsSession.sshStreams == nil {
			wsSession.sshStreams = make(map[string]*SSHStreamHandler)
		}
		if existing, exists := wsSession.sshStreams[sshSessionID]; exists {
			wsSession.sshStreamsMu.Unlock()
			log.Warn().
				Str("ssh_session", sshSessionID).
				Msg("Replacing existing active SSH stream")
			p.cleanupSSHStreamHandler(wsSession, sshSessionID, existing)
			continue
		}

		ctx, cancel = context.WithCancel(context.Background())
		handler = &SSHStreamHandler{
			sessionID: sshSessionID,
			cancel:    cancel,
			active:    true,
			inputCh:   make(chan *pb.SSHStreamMessage, 512),
		}
		wsSession.sshStreams[sshSessionID] = handler
		wsSession.sshStreamsMu.Unlock()
		break
	}

	// Build CallContext for downstream service handler.
	ctx = auth.WithCallContext(ctx, wsSession.applyRoutingCallContext(&auth.CallContext{
		SessionToken:    wsSession.SessionToken,
		OriginIP:        wsSession.IPAddress,
		OriginUserAgent: wsSession.UserAgent,
	}))

	// Open an in-process bidi stream against the WebSSHService directly.
	// No proto marshalling between this goroutine and the SSH handler —
	// SSHStreamMessage pointers are passed through Go channels (see
	// local_stream.go). For SSH this matters: every keystroke and every
	// output chunk used to roundtrip through proto Marshal/Unmarshal twice.
	if p.services == nil || p.services.WebSSH == nil {
		log.Error().Str("ssh_session", sshSessionID).Msg("WebSSH service not configured")
		p.sendSSHStreamError(wsSession, sshSessionID, "WebSSH service not configured")
		p.cleanupSSHStreamHandler(wsSession, sshSessionID, handler)
		return
	}
	local := NewLocalBidiStream[pb.SSHStreamMessage, pb.SSHStreamMessage](ctx, 512)
	stream := local.Client()
	go func() {
		if err := p.services.WebSSH.StreamSSH(local.Server()); err != nil && ctx.Err() == nil {
			log.Warn().Err(err).Str("ssh_session", sshSessionID).Msg("WebSSHService.StreamSSH exited")
		}
		local.Close()
	}()

	// Store the stream in the handler for use by handleSSHStreamData
	wsSession.sshStreamsMu.Lock()
	if current, exists := wsSession.sshStreams[sshSessionID]; !exists || current != handler {
		wsSession.sshStreamsMu.Unlock()
		_ = stream.CloseSend()
		cancel()
		return
	}
	handler.mu.Lock()
	if !handler.active {
		handler.mu.Unlock()
		wsSession.sshStreamsMu.Unlock()
		_ = stream.CloseSend()
		cancel()
		return
	}
	handler.stream = stream
	handler.mu.Unlock()
	wsSession.sshStreamsMu.Unlock()

	// Send the session ID immediately so the core can attach the stream without
	// generating a ping response loop through the browser.
	if err := stream.Send(&pb.SSHStreamMessage{
		SessionId: sshSessionID,
	}); err != nil {
		log.Error().Err(err).Str("ssh_session", sshSessionID).Msg("Failed to send initial SSH stream message")
		p.sendSSHStreamError(wsSession, sshSessionID, "Failed to initialize SSH stream")
		p.cleanupSSHStreamHandler(wsSession, sshSessionID, handler)
		return
	}

	// Let the browser mark the stream ready immediately after the backend stream
	// is successfully initialized. Some SSH servers do not emit output or a
	// keepalive right away, and waiting for that leaves the terminal stuck in a
	// "connecting" state with input disabled.
	p.sendSSHStreamPong(wsSession, sshSessionID)

	// Sender goroutine: reads SSH input from the channel and forwards to gRPC.
	// Running this in a dedicated goroutine decouples the WebSocket read loop from
	// gRPC stream.Send(), preventing a slow gRPC connection from stalling the
	// entire WebSocket message dispatcher.
	go func() {
		defer cancel()
		for {
			select {
			case msg, ok := <-handler.inputCh:
				if !ok {
					return
				}
				if err := stream.Send(msg); err != nil {
					if ctx.Err() == nil {
						log.Error().Err(err).Str("ssh_session", sshSessionID).Msg("SSH input send failed")
					}
					return
				}
			case <-ctx.Done():
				return
			}
		}
	}()

	// Start goroutine to read from gRPC stream and send to WebSocket
	go func() {
		defer p.cleanupSSHStreamHandler(wsSession, sshSessionID, handler)
		var (
			outputBuf   []byte
			outputTimer *time.Timer
			outputMu    sync.Mutex
		)
		flushOutput := func() {
			outputMu.Lock()
			if outputTimer != nil {
				outputTimer.Stop()
				outputTimer = nil
			}
			if len(outputBuf) == 0 {
				outputMu.Unlock()
				return
			}
			data := append([]byte(nil), outputBuf...)
			outputBuf = nil
			outputMu.Unlock()
			p.sendSSHStreamOutput(wsSession, sshSessionID, data, "")
		}
		queueOutput := func(chunk []byte) {
			if len(chunk) == 0 {
				return
			}
			outputMu.Lock()
			outputBuf = append(outputBuf, chunk...)
			bufLen := len(outputBuf)
			// Flush immediately when:
			//   • buffer hit the max batch size (large burst / paste), or
			//   • chunk is interactive-sized (≤1 KB) — avoids adding a 2 ms
			//     timer delay to every single-keypress echo from the SSH server.
			// Only hold the timer for mid-size chunks arriving in rapid succession.
			if bufLen >= sshProxyBatchMaxBytes || len(chunk) <= sshProxyInteractiveMaxBytes {
				data := append([]byte(nil), outputBuf...)
				outputBuf = nil
				if outputTimer != nil {
					outputTimer.Stop()
					outputTimer = nil
				}
				outputMu.Unlock()
				p.sendSSHStreamOutput(wsSession, sshSessionID, data, "")
				return
			}
			if outputTimer == nil {
				outputTimer = time.AfterFunc(sshProxyOutputBatchDelay, flushOutput)
			}
			outputMu.Unlock()
		}
		defer flushOutput()

		for {
			msg, err := stream.Recv()
			if err != nil {
				if ctx.Err() != nil {
					// Context cancelled - normal shutdown
					return
				}
				log.Debug().Err(err).Str("ssh_session", sshSessionID).Msg("SSH stream ended")
				p.sendSSHStreamClose(wsSession, sshSessionID)
				return
			}

			// Forward output to WebSocket
			if output := msg.GetOutput(); output != nil {
				queueOutput(output.Data)
			}
			if errMsg := msg.GetError(); errMsg != nil {
				flushOutput()
				p.sendSSHStreamOutput(wsSession, sshSessionID, nil, errMsg.Message)
			}
			if ping := msg.GetPing(); ping != nil {
				flushOutput()
				// Server ping - send pong
				p.sendSSHStreamPong(wsSession, sshSessionID)
			}
		}
	}()

	log.Debug().Str("ssh_session", sshSessionID).Msg(" SSH stream started")
}

// handleSSHStreamData processes incoming SSH stream data from WebSocket
func (p *TenantProxy) handleSSHStreamData(wsSession *TenantSession, sshSessionID string, payload json.RawMessage) {
	wsSession.sshStreamsMu.RLock()
	handler, exists := wsSession.sshStreams[sshSessionID]
	wsSession.sshStreamsMu.RUnlock()

	if !exists || !handler.canAcceptInput() {
		log.Warn().Str("ssh_session", sshSessionID).Msg("SSH stream not active, ignoring data")
		return
	}

	var sshPayload SSHStreamPayload
	if err := json.Unmarshal(payload, &sshPayload); err != nil {
		log.Error().Err(err).Str("ssh_session", sshSessionID).Msg("Failed to parse SSH payload")
		return
	}

	if sshPayload.Input != nil {
		// Decode base64 input data
		inputData, err := base64Decode(sshPayload.Input.Data)
		if err != nil {
			log.Error().Err(err).Str("ssh_session", sshSessionID).Msg("Failed to decode SSH input")
			return
		}
		if len(inputData) > 0 {
			select {
			case handler.inputCh <- &pb.SSHStreamMessage{
				SessionId: sshSessionID,
				Payload: &pb.SSHStreamMessage_Input{
					Input: &pb.SSHInput{InputType: &pb.SSHInput_Data{Data: inputData}},
				},
			}:
			default:
				log.Warn().Str("ssh_session", sshSessionID).Msg("SSH input channel full, dropping input")
			}
		}
	}

	if sshPayload.Resize != nil {
		select {
		case handler.inputCh <- &pb.SSHStreamMessage{
			SessionId: sshSessionID,
			Payload: &pb.SSHStreamMessage_Input{
				Input: &pb.SSHInput{
					InputType: &pb.SSHInput_Resize{
						Resize: &pb.SSHResize{
							Rows: int32(sshPayload.Resize.Rows),
							Cols: int32(sshPayload.Resize.Cols),
						},
					},
				},
			},
		}:
		default:
			log.Warn().Str("ssh_session", sshSessionID).Msg("SSH input channel full, dropping resize")
		}
	}

	if sshPayload.Ping != nil {
		select {
		case handler.inputCh <- &pb.SSHStreamMessage{
			SessionId: sshSessionID,
			Payload: &pb.SSHStreamMessage_Ping{
				Ping: &pb.SSHPing{Timestamp: sshPayload.Ping.Timestamp},
			},
		}:
		default:
		}
		p.sendSSHStreamPong(wsSession, sshSessionID)
	}
}

// handleSSHStreamClose closes an SSH stream
func (p *TenantProxy) handleSSHStreamClose(wsSession *TenantSession, sshSessionID string) {
	log.Debug().
		Str("ws_session", wsSession.ID).
		Str("ssh_session", sshSessionID).
		Msg(" Closing SSH stream")

	p.cleanupSSHStream(wsSession, sshSessionID)
}

// cleanupSSHStream removes an SSH stream from the session
func (p *TenantProxy) cleanupSSHStream(wsSession *TenantSession, sshSessionID string) {
	p.cleanupSSHStreamHandler(wsSession, sshSessionID, nil)
}

func (p *TenantProxy) cleanupAllSSHStreams(wsSession *TenantSession) {
	wsSession.sshStreamsMu.RLock()
	sessionIDs := make([]string, 0, len(wsSession.sshStreams))
	for sshSessionID := range wsSession.sshStreams {
		sessionIDs = append(sessionIDs, sshSessionID)
	}
	wsSession.sshStreamsMu.RUnlock()

	for _, sshSessionID := range sessionIDs {
		p.cleanupSSHStream(wsSession, sshSessionID)
	}
}

func (p *TenantProxy) cleanupSSHStreamHandler(wsSession *TenantSession, sshSessionID string, expected *SSHStreamHandler) {
	var handler *SSHStreamHandler

	wsSession.sshStreamsMu.Lock()
	if current, exists := wsSession.sshStreams[sshSessionID]; exists {
		if expected != nil && current != expected {
			wsSession.sshStreamsMu.Unlock()
			return
		}
		delete(wsSession.sshStreams, sshSessionID)
		handler = current
	}
	wsSession.sshStreamsMu.Unlock()

	if handler == nil {
		return
	}

	handler.mu.Lock()
	handler.active = false
	stream := handler.stream
	handler.stream = nil
	cancel := handler.cancel
	handler.cancel = nil
	handler.mu.Unlock()

	if stream != nil {
		_ = stream.CloseSend()
	}
	if cancel != nil {
		cancel()
	}

	log.Debug().Str("ssh_session", sshSessionID).Msg(" SSH stream cleaned up")
}

// buildSSHOutputFrame encodes raw SSH output bytes as a binary WebSocket frame:
//
//	[version:1][frameType=OUTPUT:1][sessionIdLen:2 big-endian][sessionId][payload]
//
// This avoids JSON marshalling and base64 encoding/decoding on both ends,
// cutting per-character round-trip latency significantly.
func buildSSHOutputFrame(sessionID string, data []byte) []byte {
	idBytes := []byte(sessionID)
	frame := make([]byte, 4+len(idBytes)+len(data))
	frame[0] = sshBinaryFrameVersion
	frame[1] = sshBinaryFrameOutput
	frame[2] = byte(len(idBytes) >> 8)
	frame[3] = byte(len(idBytes))
	copy(frame[4:], idBytes)
	copy(frame[4+len(idBytes):], data)
	return frame
}

// sendSSHStreamOutput sends SSH output data to WebSocket.
// Data is sent as a binary frame (no JSON, no base64) for minimum latency.
// Errors that carry no data fall back to JSON so the browser can display them.
func (p *TenantProxy) sendSSHStreamOutput(wsSession *TenantSession, sshSessionID string, data []byte, errorMsg string) {
	if errorMsg != "" {
		p.writeSSHStreamMessage(wsSession, sshStreamWSMessage{
			Type:      "ssh_stream",
			SessionID: sshSessionID,
			Payload: sshStreamWSPayload{
				Output: &sshStreamWSOutput{
					Error: errorMsg,
				},
			},
		}, sshSessionID)
		return
	}
	frame := buildSSHOutputFrame(sshSessionID, data)
	wsSession.mu.Lock()
	wsSession.Conn.SetWriteDeadline(time.Now().Add(10 * time.Second))
	if err := wsSession.Conn.WriteMessage(websocket.BinaryMessage, frame); err != nil {
		log.Error().Err(err).Str("ssh_session", sshSessionID).Msg("Failed to send SSH output binary frame")
		go p.cleanupSSHStream(wsSession, sshSessionID)
	}
	wsSession.mu.Unlock()
}

// sendSSHStreamError sends an SSH stream error to WebSocket
func (p *TenantProxy) sendSSHStreamError(wsSession *TenantSession, sshSessionID string, errorMsg string) {
	p.writeSSHStreamMessage(wsSession, sshStreamWSMessage{
		Type:      "ssh_stream",
		SessionID: sshSessionID,
		Payload: sshStreamWSPayload{
			Output: &sshStreamWSOutput{
				Error: sanitizeClientErrorMessage(errorMsg, "An unexpected error occurred. Please try again."),
			},
		},
	}, sshSessionID)
}

// sendSSHStreamClose notifies WebSocket that SSH stream is closed
func (p *TenantProxy) sendSSHStreamClose(wsSession *TenantSession, sshSessionID string) {
	p.writeSSHStreamMessage(wsSession, sshStreamWSMessage{
		Type:      "ssh_stream",
		SessionID: sshSessionID,
		Payload: sshStreamWSPayload{
			Close: true,
		},
	}, sshSessionID)
}

// sendSSHStreamPong sends a pong response for keepalive
func (p *TenantProxy) sendSSHStreamPong(wsSession *TenantSession, sshSessionID string) {
	p.writeSSHStreamMessage(wsSession, sshStreamWSMessage{
		Type:      "ssh_stream",
		SessionID: sshSessionID,
		Payload: sshStreamWSPayload{
			Ping: &sshStreamWSPing{
				Timestamp: time.Now().UnixMilli(),
			},
		},
	}, sshSessionID)
}

func (p *TenantProxy) writeSSHStreamMessage(wsSession *TenantSession, msg sshStreamWSMessage, sshSessionID string) {
	raw, err := json.Marshal(msg)
	if err != nil {
		log.Error().Err(err).Str("ssh_session", sshSessionID).Msg("Failed to marshal SSH stream message")
		return
	}

	wsSession.mu.Lock()
	defer wsSession.mu.Unlock()

	wsSession.Conn.SetWriteDeadline(time.Now().Add(10 * time.Second))
	if err := wsSession.Conn.WriteMessage(websocket.TextMessage, raw); err != nil {
		log.Error().Err(err).Str("ssh_session", sshSessionID).Msg("Failed to send SSH stream message")
		go p.cleanupSSHStream(wsSession, sshSessionID)
	}
}

// base64Encode encodes bytes to base64 string
func base64Encode(data []byte) string {
	return base64.StdEncoding.EncodeToString(data)
}

// base64Decode decodes base64 string to bytes
func base64Decode(s string) ([]byte, error) {
	return base64.StdEncoding.DecodeString(s)
}
