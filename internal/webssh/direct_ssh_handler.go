package webssh

import (
	"bytes"
	"context"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"

	"WantasticCore/internal/cache"
	"WantasticCore/internal/crypto"
	"WantasticCore/internal/store"
	"WantasticCore/internal/wg/userspace"

	"github.com/gorilla/websocket"
	"github.com/rs/zerolog/log"
)

// DirectSSHHandler provides zero-copy WebSocket SSH connections through WireGuard netstack.
// Uses SSH multiplexing to allow multiple terminal windows over one TCP connection.
type DirectSSHHandler struct {
	manager        *userspace.UserspaceManager
	pool           *SSHConnectionPool // Connection pool with multiplexing
	upgrader       websocket.Upgrader
	allowedOrigins []string
	sessions       map[string]*DirectSSHSession
	sessionIndex   map[string]string
	mu             sync.RWMutex

	// SESSION CACHE: LRU for session metadata and status
	// Caches session information for fast lookup without holding sessions in memory indefinitely
	sessionCache *cache.Cache

	// Activity logging callback - logs SSH activities to peer metadata
	logActivityFunc func(tenantID, peerID string, activity SSHActivityData) error

	// Storage for session persistence
	peerStore store.PeerRepository
}

// SSHActivityData represents SSH activity data for logging.
type SSHActivityData struct {
	SessionID  string
	UserAgent  string
	ClientIP   string
	Timestamp  time.Time
	EndTime    time.Time
	Commands   []SSHCommandEntry
	Username   string
	BytesSent  uint64
	BytesRecv  uint64
	DurationMs int64
}
type SSHCommandEntry struct {
	Command   string
	Timestamp time.Time
}

// SessionStatus represents the connection state of a WebSSH session
type SessionStatus string

const (
	SessionStatusActive       SessionStatus = "active"       // WebSocket connected, SSH active
	SessionStatusDisconnected SessionStatus = "disconnected" // WebSocket closed, session saved for reconnection
	SessionStatusError        SessionStatus = "error"        // Connection failed
)

// DirectSSHSession represents a direct WebSocket-to-SSH connection with multiplexing.
type DirectSSHSession struct {
	ID                   string
	Name                 string // Human-readable session name for persistence
	TenantID             string
	PeerID               string // Peer ID for tracking and metadata updates (optional)
	PeerIP               string
	Port                 int
	Username             string
	Password             string // Stored securely, not logged
	PrivateKey           string // Stored securely, not logged
	PrivateKeyPassphrase string // Stored securely, not logged
	HostKey              []byte
	HostKeyFingerprint   string
	HostKeyAlgorithm     string
	CompatibilityMode    SSHCompatibilityMode
	TerminalRows         int
	TerminalCols         int
	StartedAt            time.Time
	LastActive           time.Time
	BytesSent            uint64
	BytesRecv            uint64
	ctx                  context.Context
	cancel               context.CancelFunc
	ReceivedCommands     []SSHCommandEntry
	// Client info for activity logging
	ClientIP  string
	UserAgent string
	// Session status for persistence
	Status          SessionStatus
	LastError       string     // Last error message if Status is error
	DisconnectedAt  time.Time  // When the session was last disconnected
	History         []byte     // Local history buffer (last 64 KB of terminal output)
	historyMu       sync.Mutex // per-session lock; never held alongside h.mu
	StreamStartedAt time.Time
}

func normalizeSSHPeerIP(peerIP string) string {
	return strings.TrimSuffix(strings.TrimSpace(peerIP), "/32")
}

func sameSSHSessionIdentity(left, right *DirectSSHSession) bool {
	if left == nil || right == nil {
		return false
	}
	if left.TenantID != right.TenantID || left.Port != right.Port || left.Username != right.Username {
		return false
	}
	if left.PeerID != "" && right.PeerID != "" {
		return left.PeerID == right.PeerID
	}
	return normalizeSSHPeerIP(left.PeerIP) == normalizeSSHPeerIP(right.PeerIP)
}

func sshSessionIdentityKey(tenantID, peerID, peerIP string, port int, username string) string {
	stablePeer := strings.TrimSpace(peerID)
	if stablePeer == "" {
		stablePeer = normalizeSSHPeerIP(peerIP)
	}
	return fmt.Sprintf("%s|%s|%d|%s", tenantID, stablePeer, port, username)
}

func sshSessionIdentityKeyForSession(session *DirectSSHSession) string {
	if session == nil {
		return ""
	}
	return sshSessionIdentityKey(session.TenantID, session.PeerID, session.PeerIP, session.Port, session.Username)
}

func sessionHasReusableAuth(session *DirectSSHSession) bool {
	if session == nil {
		return false
	}
	return hasExplicitSSHAuthMaterial(session.Password, session.PrivateKey, session.PrivateKeyPassphrase)
}

func shouldReuseExistingSession(session *DirectSSHSession, requireStoredAuth bool) bool {
	if session == nil {
		return false
	}
	if requireStoredAuth && !sessionHasReusableAuth(session) {
		return false
	}
	return true
}

func preferSessionForReuse(current, candidate *DirectSSHSession) bool {
	if candidate == nil {
		return false
	}
	if current == nil {
		return true
	}

	currentHasAuth := sessionHasReusableAuth(current)
	candidateHasAuth := sessionHasReusableAuth(candidate)
	if currentHasAuth != candidateHasAuth {
		return candidateHasAuth
	}
	if current.Status != candidate.Status {
		if candidate.Status == SessionStatusActive {
			return true
		}
		if current.Status == SessionStatusActive {
			return false
		}
	}
	if candidate.LastActive.After(current.LastActive) {
		return true
	}
	if candidate.StreamStartedAt.After(current.StreamStartedAt) {
		return true
	}
	return candidate.StartedAt.After(current.StartedAt)
}

func hasExplicitSSHAuthMaterial(password, privateKey, privateKeyPassphrase string) bool {
	return strings.TrimSpace(password) != "" ||
		strings.TrimSpace(privateKey) != "" ||
		strings.TrimSpace(privateKeyPassphrase) != ""
}

func describeSSHAuthProfile(password, privateKey, privateKeyPassphrase string, hasInteractivePrompt bool) string {
	modes := make([]string, 0, 3)
	if strings.TrimSpace(privateKey) != "" {
		if strings.TrimSpace(privateKeyPassphrase) != "" {
			modes = append(modes, "publickey+passphrase")
		} else {
			modes = append(modes, "publickey")
		}
	}
	if strings.TrimSpace(password) != "" {
		modes = append(modes, "password")
	}
	if hasInteractivePrompt {
		modes = append(modes, "interactive")
	}
	if len(modes) == 0 {
		return "none"
	}
	return strings.Join(modes, ",")
}

// NewDirectSSHHandler creates an efficient direct SSH handler with multiplexing support.
func NewDirectSSHHandler(manager *userspace.UserspaceManager, peerStore store.PeerRepository, allowedOrigins []string) *DirectSSHHandler {
	h := &DirectSSHHandler{
		manager:        manager,
		peerStore:      peerStore,
		// 5-minute idle TTL: long enough that a tab switch, brief WS reconnect,
		// or page reload reuses the same mux instead of paying for a fresh SSH
		// handshake. The previous 30 s window meant any gap longer than that
		// (very common with the new fail-fast WS reconnect path that closes
		// in-flight streams immediately) forced the user to redial — surfacing
		// as the "Connection closed" symptom right after the banner appears.
		pool:           NewSSHConnectionPool(manager, 5*time.Minute, 10000),
		allowedOrigins: allowedOrigins,
		sessions:       make(map[string]*DirectSSHSession),
		sessionIndex:   make(map[string]string),
		sessionCache:   cache.NewCacheForType(cache.TypeSession), // LRU for session metadata
	}

	h.upgrader = websocket.Upgrader{
		ReadBufferSize:  16384, // Larger buffers for SSH
		WriteBufferSize: 16384,
		CheckOrigin:     h.checkOrigin,
	}

	log.Debug().Msg(" Initialized direct SSH handler with multiplexing (zero TCP proxy overhead)")
	return h
}

// SetPeerUpdateFunc sets the peer update callback for the connection pool
func (h *DirectSSHHandler) SetPeerUpdateFunc(fn func(accountID, peerID string, updateFn func(any) error) error) {
	h.pool.SetPeerUpdateFunc(fn)
}

// SetActivityLogFunc sets the callback function for logging SSH activities to peer metadata.
func (h *DirectSSHHandler) SetActivityLogFunc(fn func(tenantID, peerID string, activity SSHActivityData) error) {
	h.logActivityFunc = fn
}

// LogActivityStart logs the start of an SSH session for a peer.
// Called when the SSH gRPC stream is established.
func (h *DirectSSHHandler) LogActivityStart(sessionID string, clientIP, userAgent string) {
	h.mu.RLock()
	session, exists := h.sessions[sessionID]
	h.mu.RUnlock()

	if !exists {
		log.Warn().Str("session_id", sessionID).Msg("Cannot log SSH activity start: session not found")
		return
	}

	if session.PeerID == "" {
		log.Debug().Str("session_id", sessionID).Msg("Skipping SSH activity log: no peer_id")
		return
	}

	if h.logActivityFunc == nil {
		log.Debug().Str("session_id", sessionID).Msg("SSH activity logging not configured")
		return
	}

	resolvedUserAgent := strings.TrimSpace(session.UserAgent)
	if resolvedUserAgent == "" {
		resolvedUserAgent = strings.TrimSpace(userAgent)
	}

	resolvedClientIP := strings.TrimSpace(clientIP)
	if resolvedClientIP == "" || resolvedClientIP == "unknown" {
		resolvedClientIP = strings.TrimSpace(session.ClientIP)
		if resolvedClientIP == "" {
			resolvedClientIP = "unknown"
		}
	}

	startedAt := time.Now()
	h.mu.Lock()
	session.ClientIP = resolvedClientIP
	session.UserAgent = resolvedUserAgent
	session.StreamStartedAt = startedAt
	h.mu.Unlock()

	activity := SSHActivityData{
		SessionID: sessionID,
		UserAgent: resolvedUserAgent,
		ClientIP:  resolvedClientIP,
		Timestamp: startedAt,
		Username:  session.Username,
	}

	if err := h.logActivityFunc(session.TenantID, session.PeerID, activity); err != nil {
		log.Warn().Err(err).
			Str("tenant_id", session.TenantID).
			Str("peer_id", session.PeerID).
			Str("session_id", sessionID).
			Msg(" Failed to log SSH activity start")
	} else {
		log.Debug().
			Str("tenant_id", session.TenantID).
			Str("peer_id", session.PeerID).
			Str("session_id", sessionID).
			Str("username", session.Username).
			Msg("📝 Logged SSH activity start")
	}
}

// LogActivityEnd logs the end of an SSH session for a peer.
// Called when the SSH gRPC stream is closed.
func (h *DirectSSHHandler) LogActivityEnd(sessionID string, bytesSent, bytesRecv uint64) {
	h.mu.RLock()
	session, exists := h.sessions[sessionID]
	h.mu.RUnlock()

	if !exists {
		log.Warn().Str("session_id", sessionID).Msg("Cannot log SSH activity end: session not found")
		return
	}

	if session.PeerID == "" {
		return
	}

	if h.logActivityFunc == nil {
		return
	}

	endTime := time.Now()
	activity := buildSSHActivityEnd(session, endTime, bytesSent, bytesRecv)

	defer func() {
		h.mu.Lock()
		if current, ok := h.sessions[sessionID]; ok {
			current.StreamStartedAt = time.Time{}
		}
		h.mu.Unlock()
	}()

	if err := h.logActivityFunc(session.TenantID, session.PeerID, activity); err != nil {
		log.Warn().Err(err).
			Str("tenant_id", session.TenantID).
			Str("peer_id", session.PeerID).
			Str("session_id", sessionID).
			Msg(" Failed to log SSH activity end")
	} else {
		log.Debug().
			Str("tenant_id", session.TenantID).
			Str("peer_id", session.PeerID).
			Str("session_id", sessionID).
			Int64("duration_ms", activity.DurationMs).
			Msg("📝 Logged SSH activity end")
	}
}

func buildSSHActivityEnd(session *DirectSSHSession, endTime time.Time, bytesSent, bytesRecv uint64) SSHActivityData {
	startedAt := session.StreamStartedAt
	if startedAt.IsZero() {
		startedAt = session.StartedAt
	}
	durationMs := endTime.Sub(startedAt).Milliseconds()

	commands := make([]SSHCommandEntry, len(session.ReceivedCommands))
	copy(commands, session.ReceivedCommands)

	return SSHActivityData{
		SessionID:  session.ID,
		UserAgent:  session.UserAgent,
		ClientIP:   session.ClientIP,
		Timestamp:  startedAt,
		EndTime:    endTime,
		Commands:   commands,
		Username:   session.Username,
		BytesSent:  bytesSent,
		BytesRecv:  bytesRecv,
		DurationMs: durationMs,
	}
}

// PersistedSession represents a WebSSH session configuration from the database.
// Used for loading saved sessions on startup.
type PersistedSession struct {
	ID                   string
	Name                 string
	TenantID             string
	PeerID               string
	PeerIP               string
	Port                 int
	Username             string
	Password             string
	PrivateKey           string
	PrivateKeyPassphrase string
	HostKey              []byte
	HostKeyFingerprint   string
	HostKeyAlgorithm     string
	CompatibilityMode    SSHCompatibilityMode
	TerminalRows         int
	TerminalCols         int
}

// LoadPersistedSessions loads saved WebSSH sessions into memory for reconnection.
// Called on startup to restore sessions from database.
func (h *DirectSSHHandler) LoadPersistedSessions(sessions []PersistedSession) {
	h.mu.Lock()
	defer h.mu.Unlock()

	loaded := 0
	for _, ps := range sessions {
		// Skip if session already exists (shouldn't happen on startup, but be safe)
		if _, exists := h.sessions[ps.ID]; exists {
			continue
		}

		session := &DirectSSHSession{
			ID:                   ps.ID,
			Name:                 ps.Name,
			TenantID:             ps.TenantID,
			PeerID:               ps.PeerID,
			PeerIP:               ps.PeerIP,
			Port:                 ps.Port,
			Username:             ps.Username,
			Password:             ps.Password,
			PrivateKey:           ps.PrivateKey,
			PrivateKeyPassphrase: ps.PrivateKeyPassphrase,
			HostKey:              append([]byte(nil), ps.HostKey...),
			HostKeyFingerprint:   ps.HostKeyFingerprint,
			HostKeyAlgorithm:     ps.HostKeyAlgorithm,
			CompatibilityMode:    ps.CompatibilityMode,
			TerminalRows:         ps.TerminalRows,
			TerminalCols:         ps.TerminalCols,
			StartedAt:            time.Now(), // Reset start time
			LastActive:           time.Now(),
			Status:               SessionStatusDisconnected, // Loaded as disconnected, needs reconnection
		}
		session.ctx, session.cancel = context.WithCancel(context.Background())

		h.registerSessionLocked(session)
		loaded++
	}

	if loaded > 0 {
		log.Debug().
			Int("loaded", loaded).
			Msg(" Loaded persisted WebSSH sessions from database")
	}
}

func (h *DirectSSHHandler) registerSessionLocked(session *DirectSSHSession) {
	if session == nil {
		return
	}
	if h.sessions == nil {
		h.sessions = make(map[string]*DirectSSHSession)
	}
	if h.sessionIndex == nil {
		h.sessionIndex = make(map[string]string)
	}
	h.sessions[session.ID] = session
	h.refreshSessionIndexLocked(session, "")
}

func (h *DirectSSHHandler) refreshSessionIndexLocked(session *DirectSSHSession, previousKey string) {
	if session == nil {
		return
	}
	if h.sessionIndex == nil {
		h.sessionIndex = make(map[string]string)
	}

	currentKey := sshSessionIdentityKeyForSession(session)
	if previousKey != "" && previousKey != currentKey && h.sessionIndex[previousKey] == session.ID {
		delete(h.sessionIndex, previousKey)
		h.rebuildSessionIndexLocked(previousKey)
	}
	if currentKey == "" {
		return
	}

	currentID := h.sessionIndex[currentKey]
	current := h.sessions[currentID]
	if currentID == "" || current == nil || currentID == session.ID || preferSessionForReuse(current, session) {
		h.sessionIndex[currentKey] = session.ID
	}
}

func (h *DirectSSHHandler) rebuildSessionIndexLocked(identityKey string) {
	if identityKey == "" {
		return
	}

	var best *DirectSSHSession
	for _, candidate := range h.sessions {
		if sshSessionIdentityKeyForSession(candidate) != identityKey {
			continue
		}
		if preferSessionForReuse(best, candidate) {
			best = candidate
		}
	}

	if best == nil {
		delete(h.sessionIndex, identityKey)
		return
	}
	h.sessionIndex[identityKey] = best.ID
}

func (h *DirectSSHHandler) unregisterSessionLocked(session *DirectSSHSession) {
	if session == nil {
		return
	}

	delete(h.sessions, session.ID)
	identityKey := sshSessionIdentityKeyForSession(session)
	if h.sessionIndex[identityKey] == session.ID {
		delete(h.sessionIndex, identityKey)
		h.rebuildSessionIndexLocked(identityKey)
	}

	h.sessionCache.Delete(cache.SessionCacheKey("webssh", session.ID))
}

func (h *DirectSSHHandler) findReusableInMemorySessionLocked(tenantID, peerID, peerIP string, sshPort int, username string, requireStoredAuth bool) *DirectSSHSession {
	identityKey := sshSessionIdentityKey(tenantID, peerID, peerIP, sshPort, username)
	if sessionID := h.sessionIndex[identityKey]; sessionID != "" {
		if session := h.sessions[sessionID]; shouldReuseExistingSession(session, requireStoredAuth) {
			return session
		}
	}

	var best *DirectSSHSession
	for _, candidate := range h.sessions {
		if candidate.TenantID != tenantID || candidate.Port != sshPort || candidate.Username != username {
			continue
		}
		if peerID != "" && candidate.PeerID != "" {
			if candidate.PeerID != peerID {
				continue
			}
		} else if normalizeSSHPeerIP(candidate.PeerIP) != normalizeSSHPeerIP(peerIP) {
			continue
		}
		if !shouldReuseExistingSession(candidate, requireStoredAuth) {
			continue
		}
		if preferSessionForReuse(best, candidate) {
			best = candidate
		}
	}

	if best != nil {
		h.sessionIndex[identityKey] = best.ID
	}
	return best
}

// checkOrigin validates WebSocket connection origin
func (h *DirectSSHHandler) checkOrigin(r *http.Request) bool {
	origin := r.Header.Get("Origin")

	if len(h.allowedOrigins) == 0 {
		return true // Development mode
	}

	for _, allowed := range h.allowedOrigins {
		if origin == allowed {
			return true
		}
	}

	log.Warn().Str("origin", origin).Msg("Origin rejected")
	return false
}

func (h *DirectSSHHandler) saveSessionRecord(session *DirectSSHSession) error {
	if h.peerStore == nil {
		return nil
	}

	device, err := h.manager.GetDevice(session.TenantID)
	if err != nil {
		return fmt.Errorf("tenant not found: %w", err)
	}

	cipher, err := crypto.NewCredentialCipher(device.PrivateKey[:])
	if err != nil {
		return fmt.Errorf("failed to initialize credential cipher for WebSSH session: %w", err)
	}

	encUsername, err := cipher.Encrypt([]byte(session.Username))
	if err != nil {
		return fmt.Errorf("failed to encrypt WebSSH username: %w", err)
	}

	encPassword, err := cipher.Encrypt([]byte(session.Password))
	if err != nil {
		return fmt.Errorf("failed to encrypt WebSSH password: %w", err)
	}

	encPrivateKey, err := cipher.Encrypt([]byte(session.PrivateKey))
	if err != nil {
		return fmt.Errorf("failed to encrypt WebSSH private key: %w", err)
	}

	encPrivateKeyPassphrase, err := cipher.Encrypt([]byte(session.PrivateKeyPassphrase))
	if err != nil {
		return fmt.Errorf("failed to encrypt WebSSH private key passphrase: %w", err)
	}

	dbSession := &store.WebSSHSessionData{
		ID:                            session.ID,
		PeerID:                        session.PeerID,
		PeerIP:                        session.PeerIP,
		AccountID:                     session.TenantID,
		Name:                          session.Name,
		Port:                          session.Port,
		EncryptedUsername:             encUsername,
		EncryptedPassword:             encPassword,
		EncryptedPrivateKey:           encPrivateKey,
		EncryptedPrivateKeyPassphrase: encPrivateKeyPassphrase,
		TerminalRows:                  session.TerminalRows,
		TerminalCols:                  session.TerminalCols,
		UserAgent:                     session.UserAgent,
		LastConnected:                 session.LastActive,
		CreatedAt:                     session.StartedAt,
		UpdatedAt:                     session.LastActive,
		Enabled:                       true,
		History:                       session.History,
		HostKey:                       append([]byte(nil), session.HostKey...),
		HostKeyFingerprint:            session.HostKeyFingerprint,
		HostKeyAlgorithm:              session.HostKeyAlgorithm,
		CompatibilityMode:             string(session.CompatibilityMode),
	}

	return h.peerStore.SaveWebSSHSession(session.TenantID, session.PeerID, dbSession)
}

func (h *DirectSSHHandler) refreshExistingSession(session *DirectSSHSession, peerID, peerIP string, sshPort int, username, password, privateKey, privateKeyPassphrase, userAgent string, rows, cols int) (string, error) {
	h.mu.Lock()
	previousKey := sshSessionIdentityKeyForSession(session)
	targetChanged := session.PeerIP != peerIP || session.Port != sshPort || session.Username != username
	hasIncomingAuth := hasExplicitSSHAuthMaterial(password, privateKey, privateKeyPassphrase)
	if peerID != "" {
		session.PeerID = peerID
	}
	session.PeerIP = peerIP
	session.Port = sshPort
	session.Username = username
	if hasIncomingAuth {
		session.Password = password
		session.PrivateKey = privateKey
		session.PrivateKeyPassphrase = privateKeyPassphrase
	}
	session.Name = fmt.Sprintf("%s@%s:%d", username, peerIP, sshPort)
	session.TerminalRows = rows
	session.TerminalCols = cols
	if trimmedUserAgent := strings.TrimSpace(userAgent); trimmedUserAgent != "" {
		session.UserAgent = trimmedUserAgent
	}
	session.LastActive = time.Now()
	if targetChanged {
		session.HostKey = nil
		session.HostKeyFingerprint = ""
		session.HostKeyAlgorithm = ""
		session.CompatibilityMode = SSHCompatibilityUnknown
	}
	h.refreshSessionIndexLocked(session, previousKey)
	h.mu.Unlock()

	if err := h.saveSessionRecord(session); err != nil {
		return "", err
	}

	log.Debug().
		Str("session_id", session.ID).
		Str("peer_ip", peerIP).
		Str("username", username).
		Int("ssh_port", sshPort).
		Bool("password_provided", password != "").
		Bool("private_key_provided", privateKey != "").
		Bool("private_key_passphrase_provided", privateKeyPassphrase != "").
		Bool("stored_password_present", session.Password != "").
		Msg("♻️ Reusing persisted WebSSH session")

	return session.ID, nil
}

func (h *DirectSSHHandler) resolveAuthoritativeSession(session *DirectSSHSession) *DirectSSHSession {
	if session == nil || hasExplicitSSHAuthMaterial(session.Password, session.PrivateKey, session.PrivateKeyPassphrase) || h.peerStore == nil {
		return session
	}

	var (
		candidates []*store.WebSSHSessionData
		err        error
	)
	if session.PeerID != "" {
		candidates, err = h.peerStore.ListWebSSHSessions(session.TenantID, session.PeerID)
	} else {
		candidates, err = h.peerStore.ListAllWebSSHSessions(session.TenantID)
	}
	if err != nil {
		log.Warn().
			Err(err).
			Str("session_id", session.ID).
			Msg("Failed to list WebSSH sessions while resolving authoritative auth source")
		return session
	}

	best := session
	bestHasAuth := false
	for _, candidate := range candidates {
		if candidate == nil || candidate.ID == session.ID {
			continue
		}

		hydrated, getErr := h.GetSession(candidate.ID)
		if getErr != nil {
			continue
		}
		if !sameSSHSessionIdentity(session, hydrated) {
			continue
		}

		candidateHasAuth := hasExplicitSSHAuthMaterial(hydrated.Password, hydrated.PrivateKey, hydrated.PrivateKeyPassphrase)
		if !candidateHasAuth {
			continue
		}
		if !bestHasAuth || hydrated.LastActive.After(best.LastActive) || hydrated.StartedAt.After(best.StartedAt) {
			best = hydrated
			bestHasAuth = true
		}
	}

	if best.ID != session.ID {
		log.Debug().
			Str("requested_session_id", session.ID).
			Str("resolved_session_id", best.ID).
			Str("peer_ip", session.PeerIP).
			Str("username", session.Username).
			Msg("Resolved WebSSH stream to fresher same-target session with stored auth material")
	}

	return best
}

// CreateSession generates a session ID for direct SSH access with credentials.
// peerID is optional - if provided, peer metadata (like LastConnected) will be updated after successful connections.
func (h *DirectSSHHandler) CreateSession(tenantID, peerID, peerIP string, sshPort int, username, password, privateKey, privateKeyPassphrase, userAgent string, rows, cols int) (string, error) {
	if username == "" {
		return "", fmt.Errorf("SSH username is required")
	}
	if privateKeyPassphrase != "" && strings.TrimSpace(privateKey) == "" {
		return "", fmt.Errorf("SSH private key is required when a private key passphrase is provided")
	}
	if rows <= 0 {
		rows = 24 // Default terminal rows
	}
	if cols <= 0 {
		cols = 80 // Default terminal cols
	}
	if sshPort <= 0 {
		sshPort = 22 // Default SSH port
	}
	// Validate that the tenant device exists before creating a resumable session handle.
	if _, err := h.manager.GetDevice(tenantID); err != nil {
		return "", fmt.Errorf("tenant not found: %w", err)
	}

	// Pre-flight check removed to allow non-blocking session creation and better error handling during actual connection.
	log.Debug().
		Str("tenant_id", tenantID).
		Str("peer_ip", peerIP).
		Int("ssh_port", sshPort).
		Str("username", username).
		Bool("password_provided", password != "").
		Bool("private_key_provided", privateKey != "").
		Bool("private_key_passphrase_provided", privateKeyPassphrase != "").
		Msg(" Creating SSH session (pre-flight check skipped for performance)")

	hasExplicitAuth := hasExplicitSSHAuthMaterial(password, privateKey, privateKeyPassphrase)
	allowSessionReuse := !hasExplicitAuth
	requireStoredAuthForReuse := !hasExplicitAuth
	if !allowSessionReuse {
		log.Debug().
			Str("tenant_id", tenantID).
			Str("peer_ip", peerIP).
			Int("ssh_port", sshPort).
			Str("username", username).
			Msg("Bypassing WebSSH session reuse because explicit auth material was provided")
	}

	if allowSessionReuse {
		h.mu.Lock()
		existingSession := h.findReusableInMemorySessionLocked(tenantID, peerID, peerIP, sshPort, username, requireStoredAuthForReuse)
		h.mu.Unlock()
		if existingSession != nil {
			return h.refreshExistingSession(existingSession, peerID, peerIP, sshPort, username, password, privateKey, privateKeyPassphrase, userAgent, rows, cols)
		}
	}

	if allowSessionReuse && h.peerStore != nil && peerID != "" {
		if savedSessions, err := h.peerStore.ListWebSSHSessions(tenantID, peerID); err == nil {
			var bestPersisted *DirectSSHSession
			for _, saved := range savedSessions {
				if saved.PeerIP != peerIP || saved.Port != sshPort {
					continue
				}

				existingSession, getErr := h.GetSession(saved.ID)
				if getErr != nil {
					log.Warn().Err(getErr).Str("session_id", saved.ID).Msg("Failed to hydrate persisted WebSSH session for reuse")
					continue
				}
				if existingSession.Username != username {
					continue
				}
				if !shouldReuseExistingSession(existingSession, requireStoredAuthForReuse) {
					continue
				}
				if preferSessionForReuse(bestPersisted, existingSession) {
					bestPersisted = existingSession
				}
			}

			if bestPersisted != nil {
				return h.refreshExistingSession(bestPersisted, peerID, peerIP, sshPort, username, password, privateKey, privateKeyPassphrase, userAgent, rows, cols)
			}
		} else {
			log.Warn().Err(err).Str("peer_id", peerID).Msg("Failed to list persisted WebSSH sessions for reuse")
		}
	}

	// Generate session ID
	sessionID := fmt.Sprintf("%s_%s_%d_%d", tenantID[:8], peerIP, sshPort, time.Now().UnixNano())

	session := &DirectSSHSession{
		ID:                   sessionID,
		Name:                 fmt.Sprintf("%s@%s:%d", username, peerIP, sshPort),
		TenantID:             tenantID,
		PeerID:               peerID,
		PeerIP:               peerIP,
		Port:                 sshPort,
		Username:             username,
		Password:             password,
		PrivateKey:           privateKey,
		PrivateKeyPassphrase: privateKeyPassphrase,
		UserAgent:            strings.TrimSpace(userAgent),
		CompatibilityMode:    SSHCompatibilityUnknown,
		TerminalRows:         rows,
		TerminalCols:         cols,
		StartedAt:            time.Now(),
		LastActive:           time.Now(),
		Status:               SessionStatusDisconnected,
	}
	session.ctx, session.cancel = context.WithCancel(context.Background())

	if err := h.saveSessionRecord(session); err != nil {
		log.Warn().Err(err).Str("session_id", sessionID).Msg(" Failed to persist WebSSH session to database")
		return "", err
	}

	log.Debug().
		Str("session_id", sessionID).
		Str("tenant_id", tenantID).
		Str("peer_ip", peerIP).
		Int("ssh_port", sshPort).
		Str("username", username).
		Bool("password_provided", password != "").
		Bool("private_key_provided", privateKey != "").
		Bool("private_key_passphrase_provided", privateKeyPassphrase != "").
		Msg(" Created WebSSH session record")

	// CRITICAL: Add session to in-memory map so LogActivityStart/End can find it.
	// Previously this was missing — sessions were only saved to DB, causing
	// activity logging to fail with "session not found" and EndTime was never set.
	h.mu.Lock()
	h.registerSessionLocked(session)
	h.mu.Unlock()

	return sessionID, nil
}

// GetSession retrieves session info
func (h *DirectSSHHandler) GetSession(sessionID string) (*DirectSSHSession, error) {
	// Check cache first for fast session lookups
	cacheKey := cache.SessionCacheKey("webssh", sessionID)

	if cachedVal, found := h.sessionCache.Get(cacheKey); found {
		if session, ok := cachedVal.(*DirectSSHSession); ok {
			// Cache hit - return cached session
			return session, nil
		}
	}

	h.mu.RLock()
	// No defer, manual handling to prevent deadlock on upgrade

	session, exists := h.sessions[sessionID]
	if !exists {
		// Try to load from database (persistence)
		if h.peerStore != nil {
			dbSession, err := h.peerStore.GetWebSSHSession(sessionID)
			if err == nil && dbSession != nil {
				username := string(dbSession.EncryptedUsername)
				password := string(dbSession.EncryptedPassword)
				privateKey := string(dbSession.EncryptedPrivateKey)
				privateKeyPassphrase := string(dbSession.EncryptedPrivateKeyPassphrase)

				if device, err := h.manager.GetDevice(dbSession.AccountID); err == nil {
					if cipher, err := crypto.NewCredentialCipher(device.PrivateKey[:]); err == nil {
						if decUser, decPass, err := cipher.DecryptCredentials(dbSession.EncryptedUsername, dbSession.EncryptedPassword); err == nil {
							username = decUser
							password = decPass
						} else {
							log.Debug().Err(err).Str("session_id", sessionID).Msg("Failed to decrypt DB credentials, falling back to plaintext (likely legacy record)")
						}
						if len(dbSession.EncryptedPrivateKey) > 0 {
							if decPrivateKey, err := cipher.DecryptString(dbSession.EncryptedPrivateKey); err == nil {
								privateKey = decPrivateKey
							} else {
								log.Debug().Err(err).Str("session_id", sessionID).Msg("Failed to decrypt WebSSH private key, falling back to plaintext (likely legacy record)")
							}
						}
						if len(dbSession.EncryptedPrivateKeyPassphrase) > 0 {
							if decPrivateKeyPassphrase, err := cipher.DecryptString(dbSession.EncryptedPrivateKeyPassphrase); err == nil {
								privateKeyPassphrase = decPrivateKeyPassphrase
							} else {
								log.Debug().Err(err).Str("session_id", sessionID).Msg("Failed to decrypt WebSSH private key passphrase, falling back to plaintext (likely legacy record)")
							}
						}
					}
				}

				// Reconstruct session from DB
				session = &DirectSSHSession{
					ID:                   dbSession.ID,
					Name:                 dbSession.Name,
					TenantID:             dbSession.AccountID,
					PeerID:               dbSession.PeerID,
					PeerIP:               dbSession.PeerIP,
					Port:                 dbSession.Port,
					Username:             username,
					Password:             password,
					PrivateKey:           privateKey,
					PrivateKeyPassphrase: privateKeyPassphrase,
					HostKey:              append([]byte(nil), dbSession.HostKey...),
					HostKeyFingerprint:   dbSession.HostKeyFingerprint,
					HostKeyAlgorithm:     dbSession.HostKeyAlgorithm,
					CompatibilityMode:    SSHCompatibilityMode(dbSession.CompatibilityMode),
					TerminalRows:         dbSession.TerminalRows,
					TerminalCols:         dbSession.TerminalCols,
					UserAgent:            dbSession.UserAgent,
					StartedAt:            dbSession.CreatedAt,
					LastActive:           dbSession.UpdatedAt,
					Status:               SessionStatusDisconnected,
					History:              dbSession.History,
				}
				session.ctx, session.cancel = context.WithCancel(context.Background())

				// Cache it in memory map for active use - Release RLock first!
				h.mu.RUnlock()
				h.mu.Lock()
				h.registerSessionLocked(session)
				h.mu.Unlock()
				// Re-acquire RLock for defer? No, defer assumes RUnlock.
				// Better to return directly or handle defer.
				// Since we RUnlock/Lock/Unlock, the original defer RUnlock will panic if we don't re-acquire or if we return.
				// Solution: Remove defer, handle manually.

				log.Debug().Str("session_id", sessionID).Msg(" Restored WebSSH session from database")

				// Update cache
				h.sessionCache.SetWithTTL(cacheKey, session, time.Minute*3)
				return session, nil
			}
		}

		h.mu.RUnlock() // Manual unlock for the not found case
		return nil, fmt.Errorf("session not found")
	}

	// Cache the session for fast subsequent lookups (3-minute TTL for active sessions)
	h.sessionCache.SetWithTTL(cacheKey, session, time.Minute*3)
	h.mu.RUnlock() // Manual unlock for the found case

	return session, nil
}

// DisconnectSession terminates a session and removes it from the database
func (h *DirectSSHHandler) DisconnectSession(sessionID string) error {
	h.mu.Lock()

	session, exists := h.sessions[sessionID]
	if !exists {
		h.mu.Unlock()
		// Session not in memory — try to delete from DB directly
		if h.peerStore != nil {
			// Try to load it to get the peerID/accountID for deletion
			dbSession, err := h.peerStore.GetWebSSHSession(sessionID)
			if err == nil && dbSession != nil {
				if delErr := h.peerStore.DeleteWebSSHSession(dbSession.AccountID, dbSession.PeerID, sessionID); delErr != nil {
					log.Warn().Err(delErr).Str("session_id", sessionID).Msg("Failed to delete session from DB")
				} else {
					log.Debug().Str("session_id", sessionID).Msg("🗑️ Deleted orphan session from DB")
				}
				return nil
			}
		}
		return fmt.Errorf("session not found")
	}

	var endActivity *SSHActivityData
	if h.logActivityFunc != nil && session.PeerID != "" {
		activity := buildSSHActivityEnd(session, time.Now(), session.BytesSent, session.BytesRecv)
		endActivity = &activity
	}
	tenantID := session.TenantID
	peerID := session.PeerID

	session.cancel()
	h.unregisterSessionLocked(session)
	h.mu.Unlock()

	if endActivity != nil {
		if err := h.logActivityFunc(tenantID, peerID, *endActivity); err != nil {
			log.Warn().Err(err).Str("session_id", sessionID).Msg(" Failed to log SSH activity end on disconnect")
		}
	}

	// Delete session from DB (source of truth cleanup)
	if h.peerStore != nil {
		if err := h.peerStore.DeleteWebSSHSession(tenantID, peerID, sessionID); err != nil {
			log.Warn().Err(err).Str("session_id", sessionID).Msg("Failed to delete session from DB on disconnect")
		} else {
			log.Debug().Str("session_id", sessionID).Msg("🗑️ Deleted session from DB")
		}
	}

	log.Debug().Str("session_id", sessionID).Msg(" Disconnected and deleted SSH session")
	return nil
}

// ListSessions returns all active sessions
// ListSessions returns all active sessions for a tenant.
// Merges local in-memory sessions with those found in the database for cross-core visibility.
func (h *DirectSSHHandler) ListSessions(tenantID string) ([]*DirectSSHSession, error) {
	// PURE DB LISTING - No internal memory merge.
	// This matches "SSH Accounts" behavior and is fast/stateless.
	if h.peerStore == nil {
		return nil, nil // No store, no sessions
	}

	dbSessions, err := h.peerStore.ListAllWebSSHSessions(tenantID)
	if err != nil {
		log.Warn().Err(err).Str("tenant_id", tenantID).Msg(" Failed to list WebSSH sessions from database")
		return nil, err
	}

	var sessions []*DirectSSHSession
	// Lock for read access to active sessions map
	h.mu.RLock()
	defer h.mu.RUnlock()

	for _, ds := range dbSessions {
		status := SessionStatusDisconnected
		var startedAt, lastActive time.Time = ds.CreatedAt, ds.UpdatedAt

		// Check if active in memory
		if activeSess, ok := h.sessions[ds.ID]; ok {
			status = activeSess.Status
			startedAt = activeSess.StartedAt
			lastActive = activeSess.LastActive
		}

		// Extract username from ds.Name ("root@10.0.0.1:22") since EncryptedUsername is an AES byte blob
		// Casting raw AES bytes to string directly causes gRPC protobuf marshalling to crash with "invalid UTF-8".
		username := ""
		if ds.Name != "" {
			parts := strings.Split(ds.Name, "@")
			if len(parts) > 1 {
				username = parts[0]
			}
		}

		session := &DirectSSHSession{
			ID:                 ds.ID,
			Name:               ds.Name,
			TenantID:           ds.AccountID,
			PeerID:             ds.PeerID,
			PeerIP:             ds.PeerIP,
			Port:               ds.Port,
			Username:           username,
			HostKey:            append([]byte(nil), ds.HostKey...),
			HostKeyFingerprint: ds.HostKeyFingerprint,
			HostKeyAlgorithm:   ds.HostKeyAlgorithm,
			CompatibilityMode:  SSHCompatibilityMode(ds.CompatibilityMode),
			TerminalRows:       ds.TerminalRows,
			TerminalCols:       ds.TerminalCols,
			StartedAt:          startedAt,
			LastActive:         lastActive,
			Status:             status,
		}
		sessions = append(sessions, session)
	}

	return sessions, nil
}

// Close cleans up all sessions and connection pool
func (h *DirectSSHHandler) Close() {
	h.mu.Lock()
	defer h.mu.Unlock()

	for _, session := range h.sessions {
		session.cancel()
	}
	h.sessions = make(map[string]*DirectSSHSession)

	// Close connection pool
	if h.pool != nil {
		h.pool.Close()
	}

	log.Debug().Msg(" Direct SSH handler with multiplexing closed")
}

// GetActiveSessionCount returns the number of active SSH sessions.
func (h *DirectSSHHandler) GetActiveSessionCount() int {
	h.mu.RLock()
	defer h.mu.RUnlock()
	count := 0
	for _, session := range h.sessions {
		if session.Status == SessionStatusActive {
			count++
		}
	}
	return count
}

// SSHStream wraps an SSH session for reading and writing via io.ReadWriter interface.
type SSHStream struct {
	sessionID string
	muxSess   *MuxSession
	mux       *SSHMultiplexer // retained so AbortRead can close the session without a pool lookup
	session   *DirectSSHSession
	handler   *DirectSSHHandler
}

// AbortRead forcefully closes the underlying MuxSession to unblock any goroutine
// currently blocked in Read.  When the MuxSession is closed, the io.Pipe
// write-end closes, causing the blocked combinedReader.Read to return io.EOF.
//
// This is used by the gRPC streaming handler to guarantee that the stdout
// goroutine exits promptly when the client disconnects — without it, an idle
// SSH session leaves goroutines permanently blocked until the SSH server or
// WireGuard connection is torn down.
func (s *SSHStream) AbortRead() {
	if s.mux != nil {
		s.mux.CloseSession(s.sessionID)
	}
}

// Read reads data from the SSH session (stdout).
//
// The history buffer is protected by a per-session mutex (historyMu) rather
// than the global handler mutex (h.mu).  This eliminates the bottleneck that
// previously forced every concurrent terminal read — across ALL sessions — to
// serialise through a single write-lock, causing visible typing latency under
// load.
func (s *SSHStream) Read(p []byte) (n int, err error) {
	n, err = s.muxSess.stdout.Read(p)
	if n > 0 {
		s.muxSess.BytesRecv += uint64(n)
		if s.mux != nil {
			s.mux.touchActivity()
		}
		// Cap history at 64 KB — enough scrollback context without excessive
		// per-session memory.  Only this session's dedicated mutex is acquired.
		const maxHistory = 64 * 1024
		s.session.historyMu.Lock()
		if need := len(s.session.History) + n; need > maxHistory {
			trim := need - maxHistory
			if trim < len(s.session.History) {
				s.session.History = s.session.History[trim:]
			} else {
				s.session.History = nil
			}
		}
		s.session.History = append(s.session.History, p[:n]...)
		s.session.historyMu.Unlock()
	}
	return n, err
}

// Write writes data to the SSH session (stdin)
func (s *SSHStream) Write(p []byte) (n int, err error) {
	n, err = s.muxSess.stdin.Write(p)
	if n > 0 {
		s.muxSess.BytesSent += uint64(n)
		if s.mux != nil {
			s.mux.touchActivity()
		}
	}
	return n, err
}

// GetSSHStream returns an io.ReadWriter for the SSH session, suitable for gRPC streaming.
// This connects the gRPC stream directly to the SSH multiplexer session.
func (h *DirectSSHHandler) GetSSHStream(ctx context.Context, sessionID string, authHandler InteractiveAuthHandler) (*SSHStream, error) {
	// Use GetSession to support "Lazy Loading" from DB
	session, err := h.GetSession(sessionID)
	if err != nil {
		return nil, fmt.Errorf("session not found: %s", sessionID)
	}
	authSource := h.resolveAuthoritativeSession(session)
	resolvedFromAuthSource := authSource != nil && authSource.ID != session.ID
	if resolvedFromAuthSource {
		h.mu.Lock()
		session.Password = authSource.Password
		session.PrivateKey = authSource.PrivateKey
		session.PrivateKeyPassphrase = authSource.PrivateKeyPassphrase
		session.HostKey = append(session.HostKey[:0], authSource.HostKey...)
		session.HostKeyFingerprint = authSource.HostKeyFingerprint
		session.HostKeyAlgorithm = authSource.HostKeyAlgorithm
		session.CompatibilityMode = authSource.CompatibilityMode
		h.mu.Unlock()
	}

	if ctx == nil {
		ctx = context.Background()
	}

	h.mu.RLock()
	username := session.Username
	password := session.Password
	privateKey := session.PrivateKey
	privateKeyPassphrase := session.PrivateKeyPassphrase
	peerIP := session.PeerIP
	sshPort := session.Port
	h.mu.RUnlock()

	hasStoredAuth := hasExplicitSSHAuthMaterial(password, privateKey, privateKeyPassphrase)
	hasInteractivePrompt := authHandler != nil
	if !hasStoredAuth && !hasInteractivePrompt {
		return nil, fmt.Errorf("ssh session has no stored credentials and no interactive auth channel")
	}

	// Get or create multiplexer for this session
	creds := &SSHCredentials{
		Username:             username,
		Password:             password,
		PrivateKey:           privateKey,
		PrivateKeyPassphrase: privateKeyPassphrase,
		AuthHandler:          authHandler,
	}

	log.Debug().
		Str("session_id", sessionID).
		Str("peer_ip", peerIP).
		Int("ssh_port", sshPort).
		Str("username", username).
		Bool("stored_password_present", password != "").
		Bool("stored_private_key_present", privateKey != "").
		Bool("stored_private_key_passphrase_present", privateKeyPassphrase != "").
		Bool("interactive_prompt_available", hasInteractivePrompt).
		Str("auth_profile", describeSSHAuthProfile(password, privateKey, privateKeyPassphrase, hasInteractivePrompt)).
		Msg("Attempting WebSSH stream authentication")

	pooled, err := h.pool.GetMultiplexer(ctx, session, creds)
	if err != nil {
		return nil, fmt.Errorf("failed to get SSH multiplexer: %w", err)
	}

	needsPersist := resolvedFromAuthSource
	h.mu.Lock()
	session.LastActive = time.Now()
	session.Status = SessionStatusActive
	h.refreshSessionIndexLocked(session, "")
	if creds.Password != "" && creds.Password != session.Password {
		session.Password = creds.Password
		needsPersist = true
	}
	if session.CompatibilityMode != pooled.Mux.compatibilityMode {
		session.CompatibilityMode = pooled.Mux.compatibilityMode
		needsPersist = true
	}
	if session.HostKeyFingerprint != pooled.Mux.hostKeyFingerprint {
		session.HostKeyFingerprint = pooled.Mux.hostKeyFingerprint
		needsPersist = true
	}
	if session.HostKeyAlgorithm != pooled.Mux.hostKeyAlgorithm {
		session.HostKeyAlgorithm = pooled.Mux.hostKeyAlgorithm
		needsPersist = true
	}
	if !bytes.Equal(session.HostKey, pooled.Mux.hostKey) {
		session.HostKey = append([]byte(nil), pooled.Mux.hostKey...)
		needsPersist = true
	}
	h.mu.Unlock()
	if needsPersist {
		if err := h.saveSessionRecord(session); err != nil {
			log.Warn().Err(err).Str("session_id", sessionID).Msg("Failed to persist refreshed WebSSH credentials")
		}
		needsPersist = false
	}

	// Get the MuxSession for this sessionID.
	muxSess := pooled.Mux.GetSession(sessionID)

	// Guard against the race where CloseSession ran concurrently between
	// GetSession's map lookup and this point (the session pointer is stale but
	// IsClosed() is now true).  Treat it as if the session doesn't exist so we
	// create a fresh one below.
	if muxSess != nil && muxSess.IsClosed() {
		log.Debug().Str("session_id", sessionID).
			Msg("SSH mux session was closed during lookup race; creating fresh session")
		muxSess = nil
	}

	if muxSess == nil {
		// New shell starting: discard old terminal history.  Raw escape codes
		// from a previous shell process are meaningless on a fresh shell and
		// cause the terminal to appear blocked or show stale content to the
		// user.  History is only useful if the *same* process is resumed, which
		// is not possible with the current design (CloseSession always kills the
		// MuxSession when the gRPC stream ends).
		h.mu.Lock()
		session.History = nil
		h.mu.Unlock()

		// Create a new SSH session in the multiplexer
		muxSess, err = pooled.Mux.NewSession(sessionID, session.TerminalRows, session.TerminalCols)
		if err != nil {
			log.Warn().Err(err).Msg("Failed to create SSH session on pooled connection, invalidating pool and retrying")

			// Release and remove the invalid multiplexer
			h.pool.ReleaseMultiplexer(session, sessionID)
			h.pool.RemoveMultiplexer(session)

			// Retry with a fresh connection
			pooled, err = h.pool.GetMultiplexer(session.ctx, session, creds)
			if err != nil {
				return nil, fmt.Errorf("failed to get new SSH multiplexer after retry: %w", err)
			}

			h.mu.Lock()
			if session.CompatibilityMode != pooled.Mux.compatibilityMode {
				session.CompatibilityMode = pooled.Mux.compatibilityMode
				needsPersist = true
			}
			if session.HostKeyFingerprint != pooled.Mux.hostKeyFingerprint {
				session.HostKeyFingerprint = pooled.Mux.hostKeyFingerprint
				needsPersist = true
			}
			if session.HostKeyAlgorithm != pooled.Mux.hostKeyAlgorithm {
				session.HostKeyAlgorithm = pooled.Mux.hostKeyAlgorithm
				needsPersist = true
			}
			if !bytes.Equal(session.HostKey, pooled.Mux.hostKey) {
				session.HostKey = append([]byte(nil), pooled.Mux.hostKey...)
				needsPersist = true
			}
			h.mu.Unlock()
			if needsPersist {
				if err := h.saveSessionRecord(session); err != nil {
					log.Warn().Err(err).Str("session_id", sessionID).Msg("Failed to persist refreshed WebSSH trust state after retry")
				}
				needsPersist = false
			}

			// Try creating session again
			muxSess, err = pooled.Mux.NewSession(sessionID, session.TerminalRows, session.TerminalCols)
			if err != nil {
				h.pool.ReleaseMultiplexer(session, sessionID)
				h.pool.RemoveMultiplexer(session)
				return nil, fmt.Errorf("failed to create SSH session (after retry): %w", err)
			}
		}
	}

	log.Debug().
		Str("session_id", sessionID).
		Str("peer_ip", session.PeerIP).
		Int("ssh_port", session.Port).
		Str("username", session.Username).
		Bool("stored_password_present", session.Password != "").
		Bool("auth_password_present", creds.Password != "").
		Bool("stored_private_key_present", session.PrivateKey != "").
		Str("compatibility_mode", string(session.CompatibilityMode)).
		Str("host_key_fingerprint", session.HostKeyFingerprint).
		Msg(" SSH stream ready for gRPC")

	return &SSHStream{
		sessionID: sessionID,
		muxSess:   muxSess,
		mux:       pooled.Mux,
		session:   session,
		handler:   h,
	}, nil
}

// ResizeTerminal resizes the terminal for a session
func (h *DirectSSHHandler) ResizeTerminal(sessionID string, rows, cols int) error {
	// Use GetSession to support lazy loading (though resizing usually implies active connection)
	session, err := h.GetSession(sessionID)
	if err != nil {
		return fmt.Errorf("session not found: %s", sessionID)
	}

	pooled := h.pool.FindMultiplexer(session)
	if pooled != nil {
		if err := pooled.Mux.ResizeSession(sessionID, rows, cols); err != nil {
			return fmt.Errorf("failed to resize terminal: %w", err)
		}
	}

	// Update session state
	h.mu.Lock()
	session.TerminalRows = rows
	session.TerminalCols = cols
	session.LastActive = time.Now()
	h.mu.Unlock()

	log.Debug().
		Str("session_id", sessionID).
		Int("rows", rows).
		Int("cols", cols).
		Msg("📐 Terminal resized")

	return nil
}

// ReleaseSSHStream releases the SSH multiplexer session back to the pool.
// Called when a gRPC stream ends to properly clean up the SSH session.
func (h *DirectSSHHandler) ReleaseSSHStream(sessionID string) {
	session, err := h.GetSession(sessionID)
	if err != nil {
		return // Session not found, already cleaned up
	}

	h.mu.Lock()
	session.LastActive = time.Now()
	session.Status = SessionStatusDisconnected
	session.StreamStartedAt = time.Time{}
	h.refreshSessionIndexLocked(session, "")
	h.mu.Unlock()
	if err := h.saveSessionRecord(session); err != nil {
		log.Warn().Err(err).Str("session_id", sessionID).Msg("Failed to persist WebSSH session on release")
	}

	h.pool.ReleaseMultiplexer(session, sessionID)

	log.Debug().
		Str("session_id", sessionID).
		Msg(" Released SSH multiplexer session back to pool")
}
