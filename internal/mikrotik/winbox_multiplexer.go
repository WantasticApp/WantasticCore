package mikrotik

import (
	"context"
	"encoding/hex"
	"fmt"
	"io"
	"net"
	"os"
	"strings"
	"sync"
	"time"

	"WantasticCore/internal/cache"
	"WantasticCore/internal/mikrotik/ecsrp"
	"WantasticCore/internal/wg/userspace"

	"github.com/rs/zerolog/log"
)

// WinboxPort is the standard MikroTik Winbox port
const WinboxPort = 8291

// Debug mode - set via environment variable WINBOX_DEBUG=1
var winboxDebugMode = os.Getenv("WINBOX_DEBUG") == "1"

// WinboxMultiplexer manages on-demand Winbox sessions with credential rotation
// Pattern mirrors SSH multiplexer in internal/webssh/
type WinboxMultiplexer struct {
	// Listener
	listener net.Listener
	addr     string

	// UserspaceManager for tenant device lookup (follows DirectSSHHandler pattern)
	manager *userspace.UserspaceManager

	// Peer lookup function (injected dependency)
	// Looks up peer by virtual username to get AccountID, PeerID, RouterIP
	findPeerByVirtualUsername func(username string) (*SessionInfo, error)

	// Peer update function (injected dependency)
	// Updates peer metadata like LastConnected, CredentialsValid after successful connections
	updatePeerSession func(accountID, peerID string, updateFn func(interface{}) error) error

	// Winbox activity logging callback (injected dependency)
	// Logs Winbox connection activities to peer metadata for audit trail
	logWinboxActivity func(accountID, peerID string, activity WinboxActivityData) error

	// Client IP access control (Winbox GUI client restrictions)
	// If empty, all IPs are allowed. If set, only these IPs can connect.
	allowedClientIPs   map[string]bool
	allowedClientIPsMu sync.RWMutex

	// Active sessions cache (one session per access token)
	// Uses LRU cache with no TTL - sessions are manually removed when connection ends
	// When a new connection uses the same token, the old one is closed
	activeSessions *cache.Cache

	// Status
	running bool
	stopCh  chan struct{}
	// connWG tracks in-flight HandleConnection goroutines so Stop() drains them.
	connWG sync.WaitGroup
}

// WinboxActivityData represents Winbox activity data for logging.
type WinboxActivityData struct {
	SessionName string
	Username    string // Account username who connected
	ClientIP    string
	Timestamp   time.Time
	EndTime     time.Time
	DurationMs  int64
	RomonMode   bool
}

// activeSession tracks a live Winbox connection for session management
type activeSession struct {
	clientConn  net.Conn
	backendConn net.Conn
	clientAddr  string
	startTime   time.Time
	closeCh     chan struct{} // Signal to close this session
}

// SessionInfo contains metadata to route virtual username to tenant+peer
type SessionInfo struct {
	AccountID        string
	PeerID           string
	SessionName      string // Human-readable session name for activity logging
	RouterIP         string
	RouterPort       int          // Winbox port (default 8291, or scanned port from peer metadata)
	PasswordToken    string       // Virtual password for Winbox login (separate from access token)
	RealUsername     string       // Real MikroTik username for backend auth
	RealPassword     string       // Real MikroTik password for backend auth
	AuthMethod       string       // "ECSRP-5" or "Legacy"
	AllowedClientIPs []*net.IPNet // Optional: CIDR networks allowed to connect (e.g., "192.168.1.0/24", "10.0.0.5/32")
}

// NewWinboxMultiplexer creates a new Winbox multiplexer following DirectSSHHandler pattern
func NewWinboxMultiplexer(addr string, manager *userspace.UserspaceManager) *WinboxMultiplexer {
	// Create session cache - no TTL since we manage lifecycle manually
	sessionCache := cache.NewCache(&cache.CacheConfig{
		Algorithm:   cache.AlgorithmLRU,
		MaxSize:     50 * 1024 * 1024, // 50MB - sessions are small, scale for 100K devices
		MaxEntries:  100000,           // 100k concurrent sessions max
		DefaultTTL:  0,                // No auto-expiry - we manage lifecycle
		CleanupFreq: 5 * time.Minute,
		ShardCount:  32, // More shards for higher concurrency at scale
	})

	if winboxDebugMode {
		log.Debug().Msg(" Winbox debug mode ENABLED - decrypted traffic will be logged")
	}

	return &WinboxMultiplexer{
		addr:             addr,
		manager:          manager,
		stopCh:           make(chan struct{}),
		allowedClientIPs: make(map[string]bool),
		activeSessions:   sessionCache,
	}
}

// SetAllowedClientIPs sets the global whitelist of allowed Winbox GUI client IPs.
// If empty, all client IPs are allowed (open access).
// IPs should be provided without port (e.g., "192.168.1.100").
func (m *WinboxMultiplexer) SetAllowedClientIPs(ips []string) {
	m.allowedClientIPsMu.Lock()
	defer m.allowedClientIPsMu.Unlock()

	m.allowedClientIPs = make(map[string]bool)
	for _, ip := range ips {
		m.allowedClientIPs[ip] = true
	}

	if len(ips) > 0 {
		log.Debug().
			Strs("allowed_ips", ips).
			Msg("🔒 Winbox client IP whitelist configured")
	} else {
		log.Debug().Msg("🔓 Winbox client IP whitelist disabled (all IPs allowed)")
	}
}

// AddAllowedClientIP adds a single IP to the allowed client whitelist.
func (m *WinboxMultiplexer) AddAllowedClientIP(ip string) {
	m.allowedClientIPsMu.Lock()
	defer m.allowedClientIPsMu.Unlock()
	m.allowedClientIPs[ip] = true
	log.Debug().Str("ip", ip).Msg("Added IP to Winbox client whitelist")
}

// RemoveAllowedClientIP removes an IP from the allowed client whitelist.
func (m *WinboxMultiplexer) RemoveAllowedClientIP(ip string) {
	m.allowedClientIPsMu.Lock()
	defer m.allowedClientIPsMu.Unlock()
	delete(m.allowedClientIPs, ip)
	log.Debug().Str("ip", ip).Msg("Removed IP from Winbox client whitelist")
}

// isClientIPAllowed checks if the client IP is allowed to connect.
// Returns true if: whitelist is empty (open) OR IP is in whitelist.
func (m *WinboxMultiplexer) isClientIPAllowed(clientIP string) bool {
	m.allowedClientIPsMu.RLock()
	defer m.allowedClientIPsMu.RUnlock()

	// If no whitelist configured, allow all
	if len(m.allowedClientIPs) == 0 {
		return true
	}

	return m.allowedClientIPs[clientIP]
}

// extractClientIP extracts just the IP address from a net.Addr (strips port).
func extractClientIP(addr net.Addr) string {
	if tcpAddr, ok := addr.(*net.TCPAddr); ok {
		return tcpAddr.IP.String()
	}
	// Fallback: parse host:port string
	host, _, err := net.SplitHostPort(addr.String())
	if err != nil {
		return addr.String()
	}
	return host
}

// SetPeerLookupFunc sets the peer lookup function (injected from server)
func (m *WinboxMultiplexer) SetPeerLookupFunc(fn func(username string) (*SessionInfo, error)) {
	m.findPeerByVirtualUsername = fn
}

// SetPeerUpdateFunc sets the peer update function for updating session metadata
// This allows the multiplexer to update LastConnected and other fields after successful connections
func (m *WinboxMultiplexer) SetPeerUpdateFunc(fn func(accountID, peerID string, updateFn func(interface{}) error) error) {
	m.updatePeerSession = fn
}

// SetActivityLogFunc sets the callback function for logging Winbox activities to peer metadata.
func (m *WinboxMultiplexer) SetActivityLogFunc(fn func(accountID, peerID string, activity WinboxActivityData) error) {
	m.logWinboxActivity = fn
}

// Start begins listening for Winbox connections
func (m *WinboxMultiplexer) Start() error {
	listener, err := net.Listen("tcp", m.addr)
	if err != nil {
		log.Error().Err(err).Str("addr", m.addr).Msg("❌ Failed to listen on Winbox port")
		return fmt.Errorf("failed to listen on %s: %w", m.addr, err)
	}

	m.listener = listener
	m.running = true

	log.Debug().
		Str("addr", m.addr).
		Str("tcp_addr", listener.Addr().String()).
		Msg(" Winbox multiplexer started and listening")

	// NOTE: No background credential probing - validation happens only at SetWinboxCredentials time
	// This is more efficient and credentials are already validated before being stored

	// Accept connections
	for m.running {
		log.Debug().Msg(" Waiting for Winbox connections...")
		conn, err := listener.Accept()
		if err != nil {
			if !m.running {
				log.Debug().Msg("🛑 Winbox multiplexer stopped gracefully")
				break
			}
			log.Error().Err(err).Msg("❌ Failed to accept Winbox connection")
			continue
		}

		log.Debug().
			Str("client", conn.RemoteAddr().String()).
			Msg(" New Winbox connection accepted")

		m.connWG.Add(1)
		go func(c net.Conn) {
			defer m.connWG.Done()
			m.HandleConnection(c)
		}(conn)
	}

	return nil
}

// Stop stops the multiplexer
func (m *WinboxMultiplexer) Stop() error {
	m.running = false
	close(m.stopCh)

	var closeErr error
	if m.listener != nil {
		closeErr = m.listener.Close()
	}

	// Wait for all in-flight connections to finish before returning.
	m.connWG.Wait()
	return closeErr
}

// HandleConnection processes an incoming Winbox connection using Full MITM.
// The client uses access_token as BOTH username AND password.
// We terminate ECSRP on both sides and bridge encrypted messages.
func (m *WinboxMultiplexer) HandleConnection(clientConn net.Conn) {
	clientAddr := clientConn.RemoteAddr().String()
	clientIP := extractClientIP(clientConn.RemoteAddr())

	log.Debug().
		Str("client", clientAddr).
		Str("client_ip", clientIP).
		Msg("New Winbox connection - Full MITM mode")

	defer clientConn.Close()

	// Step 0: Check global IP whitelist (fast fail before ECSRP handshake)
	if !m.isClientIPAllowed(clientIP) {
		log.Warn().
			Str("client", clientAddr).
			Str("client_ip", clientIP).
			Msg("🚫 Client IP not in whitelist, rejecting connection")
		clientConn.Close()
		return
	}

	// Step 1: Act as ECSRP-5 SERVER - Receive client hello to get username (access token)
	// We use a split handshake to look up the password token before continuing
	clientECSRP := ecsrp.NewServer(clientConn)
	accessToken, err := clientECSRP.ReceiveClientHello()
	if err != nil {
		log.Error().Err(err).Str("client", clientAddr).Msg("Failed to receive client ECSRP hello")
		return
	}

	// Handle RoMON mode: Desktop Winbox appends "+r" to username for RoMON connections
	// Android Winbox doesn't use this suffix, which is why it works there
	// We need to strip the suffix for access token lookup, but preserve the mode flag
	isRomonMode := false
	cleanAccessToken := accessToken
	if strings.HasSuffix(accessToken, "+r") {
		isRomonMode = true
		cleanAccessToken = strings.TrimSuffix(accessToken, "+r")
		log.Debug().
			Str("client", clientAddr).
			Str("original_token", accessToken).
			Str("clean_token", cleanAccessToken).
			Bool("romon_mode", true).
			Msg("Received client hello - RoMON mode detected, stripped +r suffix")
	} else {
		log.Debug().
			Str("client", clientAddr).
			Str("access_token", accessToken).
			Msg("Received client hello, looking up session")
	}
	_ = isRomonMode // Reserved for future RoMON-specific handling

	// Step 2: Look up the peer by access token to get password token and real credentials (O(1) lookup)
	sessionInfo, err := m.findPeerByVirtualUsername(cleanAccessToken)
	if err != nil {
		log.Error().Err(err).
			Str("client", clientAddr).
			Str("access_token", cleanAccessToken).
			Str("original_token", accessToken).
			Bool("romon_mode", isRomonMode).
			Msg("Failed to find peer for access token")
		// Send rejection so client shows "wrong password"
		clientECSRP.RejectAuth()
		return
	}

	// Step 2.5: Set password token for ECSRP computation
	// If PasswordToken is empty (legacy sessions), fall back to access token for backward compatibility
	passwordToken := sessionInfo.PasswordToken
	if passwordToken == "" {
		passwordToken = cleanAccessToken // Backward compatibility: password = username
		log.Debug().
			Str("client", clientAddr).
			Msg("Using legacy mode: password token = access token")
	}
	clientECSRP.SetPassword(passwordToken)

	// Step 2.6: For RoMON mode, set the auth username to the CLEAN access token (without +r suffix)
	// This is because the Winbox client computes the password hash using the original username
	// (without +r suffix), but sends the username WITH +r suffix in the handshake for RoMON signaling
	if isRomonMode {
		clientECSRP.SetAuthUsername(cleanAccessToken)
		log.Debug().
			Str("client", clientAddr).
			Str("auth_username", cleanAccessToken).
			Str("handshake_username", accessToken).
			Msg(" RoMON mode: using clean access token for ECSRP password computation")
	}

	// Step 3: Check per-session IP restriction BEFORE completing ECSRP handshake
	if len(sessionInfo.AllowedClientIPs) > 0 {
		clientNetIP := net.ParseIP(clientIP)
		if clientNetIP == nil {
			log.Warn().
				Str("client", clientAddr).
				Str("client_ip", clientIP).
				Msg("🚫 Failed to parse client IP, rejecting")
			clientECSRP.RejectAuth()
			return
		}

		allowed := false
		for _, network := range sessionInfo.AllowedClientIPs {
			if network.Contains(clientNetIP) {
				allowed = true
				break
			}
		}

		if !allowed {
			log.Warn().
				Str("client", clientAddr).
				Str("client_ip", clientIP).
				Int("allowed_networks", len(sessionInfo.AllowedClientIPs)).
				Str("access_token", cleanAccessToken).
				Msg("🚫 Client IP not in any allowed CIDR network, rejecting")
			clientECSRP.RejectAuth()
			return
		}
	}

	// Step 4: Get the tenant's WireGuard device for netstack connection
	device, err := m.manager.GetDevice(sessionInfo.AccountID)
	if err != nil {
		log.Error().Err(err).
			Str("account_id", sessionInfo.AccountID).
			Msg("No WireGuard device found for tenant")
		clientECSRP.RejectAuth()
		return
	}

	// Step 5: PRE-FLIGHT — Verify backend router is reachable BEFORE completing ECSRP handshake
	// This is critical for the LB broadcast_race: the LB picks the race winner based on the
	// ECSRP handshake response (>40 bytes). If we complete the handshake before checking
	// reachability, the wrong core can win the race (both cores have session data in shared DB).
	// By connecting to the backend router FIRST, only the core with the WireGuard tunnel
	// to the router will respond as a winner. The other core will reject, and the LB will
	// correctly route to the right core.
	winboxPort := sessionInfo.RouterPort
	if winboxPort <= 0 {
		winboxPort = WinboxPort // Default to 8291 if no port specified
	}
	backendAddr := fmt.Sprintf("%s:%d", sessionInfo.RouterIP, winboxPort)
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second) // Reduced from 5s for faster race
	defer cancel()

	backendConn, err := device.Net.DialContext(ctx, "tcp", backendAddr)
	if err != nil {
		log.Warn().Err(err).
			Str("backend", backendAddr).
			Str("client", clientAddr).
			Msg("⚡ Pre-flight failed: backend router unreachable via this core's tunnel, rejecting to let correct core win")
		clientECSRP.RejectAuth()
		return
	}
	defer backendConn.Close()

	log.Debug().
		Str("client", clientAddr).
		Str("backend", backendAddr).
		Msg(" Pre-flight passed: backend router reachable, completing ECSRP handshake")

	// Step 6: NOW complete the ECSRP handshake (this is what the LB reads as the race response)
	// Only reaches here if the backend router is confirmed reachable via this core's tunnel.
	if _, err := clientECSRP.ContinueAuthWithPassword(); err != nil {
		log.Error().Err(err).Str("client", clientAddr).Msg("Failed to complete client ECSRP handshake")
		backendConn.Close()
		return
	}

	log.Debug().
		Str("client", clientAddr).
		Str("access_token", cleanAccessToken).
		Msg("Client key exchange complete, validating session")

	log.Debug().
		Str("client", clientAddr).
		Str("backend", backendAddr).
		Int("winbox_port", winboxPort).
		Bool("romon_mode", isRomonMode).
		Str("original_token", accessToken).
		Str("clean_token", cleanAccessToken).
		Msg("Connected to backend, authenticating with real credentials")

	// Step 5: Authenticate to backend with real credentials
	// NOTE: Do NOT append "+r" to backend username - the +r suffix is only for client->proxy signaling
	// RoMON mode switching happens in the M2 protocol layer after authentication, not in the username
	// The backend router uses plain username for ECSRP auth regardless of RoMON mode
	backendUsername := sessionInfo.RealUsername
	if isRomonMode {
		log.Debug().
			Str("backend_username", backendUsername).
			Str("client", clientAddr).
			Msg(" RoMON mode detected - using plain username for backend auth (RoMON signaling is post-auth)")
	} else {
		log.Debug().
			Str("backend_username", backendUsername).
			Str("client", clientAddr).
			Msg(" Normal mode: using plain username for backend")
	}

	log.Debug().
		Str("backend", backendAddr).
		Str("username", backendUsername).
		Bool("romon_mode", isRomonMode).
		Msg(" Attempting ECSRP authentication to backend...")

	backendECSRP := ecsrp.NewClientWithConn(backendConn)
	if err := backendECSRP.Connect(backendUsername, sessionInfo.RealPassword); err != nil {
		log.Error().Err(err).
			Str("backend", backendAddr).
			Str("username", backendUsername).
			Bool("romon_mode", isRomonMode).
			Str("client", clientAddr).
			Msg("❌ Failed to authenticate to backend router")
		clientECSRP.RejectAuth()
		return
	}

	log.Debug().
		Str("backend", backendAddr).
		Str("username", backendUsername).
		Bool("romon_mode", isRomonMode).
		Msg(" Backend ECSRP authentication successful")

	// Step 6: NOW confirm client auth - backend is ready!
	if err := clientECSRP.ConfirmAuth(); err != nil {
		log.Error().Err(err).Str("client", clientAddr).Msg("Failed to confirm client auth")
		return
	}

	// Step 6.1: Update peer session metadata after successful connection
	if m.updatePeerSession != nil {
		if err := m.updatePeerSession(sessionInfo.AccountID, sessionInfo.PeerID, func(data interface{}) error {
			// Update LastConnected timestamp for the Winbox session
			// The updateFn will be called on the actual peer/session object
			now := time.Now()
			if peer, ok := data.(interface{ SetWinboxLastConnected(time.Time) }); ok {
				peer.SetWinboxLastConnected(now)
			}
			return nil
		}); err != nil {
			log.Warn().Err(err).
				Str("account_id", sessionInfo.AccountID).
				Str("peer_id", sessionInfo.PeerID).
				Msg(" Failed to update peer LastConnected timestamp")
			// Don't fail the connection for this - it's just metadata
		} else {
			log.Debug().
				Str("account_id", sessionInfo.AccountID).
				Str("peer_id", sessionInfo.PeerID).
				Msg(" Updated peer Winbox LastConnected timestamp")
		}
	}

	// Step 6.5: Session management - only one connection per access token
	// If there's an existing session with this token, close it
	// EXCEPTION: RoMON mode opens multiple parallel connections that are all needed
	// In RoMON mode, use a unique session key per connection (token + client addr)
	closeCh := make(chan struct{})
	var sessionKey string
	if isRomonMode {
		// RoMON mode: allow multiple parallel connections from same token
		sessionKey = cleanAccessToken + ":" + clientAddr
		log.Debug().
			Str("access_token", cleanAccessToken).
			Str("session_key", sessionKey).
			Msg("RoMON mode: using unique session key to allow parallel connections")
	} else {
		// Normal mode: single session per token
		sessionKey = cleanAccessToken
		if existingVal, ok := m.activeSessions.Get(sessionKey); ok {
			existing := existingVal.(*activeSession)
			log.Debug().
				Str("access_token", cleanAccessToken).
				Str("old_client", existing.clientAddr).
				Str("new_client", clientAddr).
				Msg(" Closing old session - new connection with same token")
			close(existing.closeCh) // Signal old session to close
			existing.clientConn.Close()
			if existing.backendConn != nil {
				existing.backendConn.Close()
			}
		}
	}
	// Register new session
	session := &activeSession{
		clientConn:  clientConn,
		backendConn: backendConn,
		clientAddr:  clientAddr,
		startTime:   time.Now(),
		closeCh:     closeCh,
	}
	m.activeSessions.Set(sessionKey, session)

	// Cleanup session when done
	defer func() {
		// Only delete if this is still our session (not replaced by newer connection)
		if currentVal, ok := m.activeSessions.Get(sessionKey); ok {
			if currentVal.(*activeSession) == session {
				m.activeSessions.Delete(sessionKey)
			}
		}
	}()

	log.Debug().
		Str("client", clientAddr).
		Str("backend", backendAddr).
		Str("account_id", sessionInfo.AccountID).
		Str("peer_id", sessionInfo.PeerID).
		Msg(" Both sides authenticated, starting message bridge")

	// Log Winbox activity start
	connectionStartTime := time.Now()
	if m.logWinboxActivity != nil {
		activity := WinboxActivityData{
			SessionName: sessionInfo.SessionName,
			Username:    sessionInfo.RealUsername, // The MikroTik username used
			ClientIP:    clientIP,
			Timestamp:   connectionStartTime,
			RomonMode:   isRomonMode,
		}
		if err := m.logWinboxActivity(sessionInfo.AccountID, sessionInfo.PeerID, activity); err != nil {
			log.Warn().Err(err).
				Str("account_id", sessionInfo.AccountID).
				Str("peer_id", sessionInfo.PeerID).
				Msg(" Failed to log Winbox activity start")
		} else {
			log.Debug().
				Str("account_id", sessionInfo.AccountID).
				Str("peer_id", sessionInfo.PeerID).
				Str("session_name", sessionInfo.SessionName).
				Msg("📝 Logged Winbox activity start")
		}
	}

	// Step 7: Bridge encrypted messages between client and backend
	// Client messages: decrypt with client keys, re-encrypt with backend keys
	// Backend messages: decrypt with backend keys, re-encrypt with client keys
	var wg sync.WaitGroup
	wg.Add(2)

	// Client -> Backend message bridge
	go func() {
		defer wg.Done()
		msgCount := 0
		for {
			select {
			case <-closeCh:
				// Session was superseded by new connection
				log.Debug().Str("client", clientAddr).Msg("Session closed by new connection")
				return
			default:
			}

			// Receive encrypted message from client
			msg, err := clientECSRP.Receive()
			if err != nil {
				if !isClosedError(err) {
					log.Debug().Err(err).Int("msgs_bridged", msgCount).Msg("Client receive ended")
				}
				return
			}

			msgCount++

			// Debug mode: log decrypted message for reverse engineering
			if winboxDebugMode {
				logWinboxMessage("CLIENT->BACKEND", msgCount, msg, clientAddr, sessionInfo)

				log.Debug().
					Int("msg_num", msgCount).
					Int("msg_len", len(msg)).
					Str("preview", fmt.Sprintf("%x", msg[:min(16, len(msg))])).
					Msg("Client->Backend: bridging message")
			}

			// Forward to backend (re-encrypted with backend keys)
			err = backendECSRP.SendNoReply(msg)
			if err != nil {
				log.Debug().Err(err).Msg("Backend send failed")
				return
			}
			// 			fmt.Printf("[BRIDGE] Sent to backend successfully\n")
		}
	}()

	// Backend -> Client message bridge
	go func() {
		defer wg.Done()
		// 		fmt.Printf("[BRIDGE] Backend->Client goroutine started\n")
		msgCount := 0
		for {
			// Receive encrypted message from backend
			// 			fmt.Printf("[BRIDGE] Waiting for backend message %d...\n", msgCount+1)
			msg, err := backendECSRP.Receive()
			if err != nil {
				// 				fmt.Printf("[BRIDGE] Backend receive error: %v\n", err)
				if !isClosedError(err) {
					log.Debug().Err(err).Int("msgs_bridged", msgCount).Msg("Backend receive ended")
				}
				return
			}

			msgCount++

			// Debug mode: log decrypted message for reverse engineering
			if winboxDebugMode {
				logWinboxMessage("BACKEND->CLIENT", msgCount, msg, clientAddr, sessionInfo)

				log.Debug().
					Int("msg_num", msgCount).
					Int("msg_len", len(msg)).
					Str("preview", fmt.Sprintf("%x", msg[:min(16, len(msg))])).
					Msg("Backend->Client: bridging message")
			}

			// Forward to client (re-encrypted with client keys)
			// 			fmt.Printf("[BRIDGE] Sending to client...\n")
			err = clientECSRP.Send(msg)
			if err != nil {
				// 				fmt.Printf("[BRIDGE] Client send error: %v\n", err)
				log.Debug().Err(err).Msg("Client send failed")
				return
			}
			// 			fmt.Printf("[BRIDGE] Sent to client successfully\n")
		}
	}()

	wg.Wait()

	// Calculate session duration and log activity end
	endTime := time.Now()
	durationMs := endTime.Sub(connectionStartTime).Milliseconds()

	if m.logWinboxActivity != nil {
		activity := WinboxActivityData{
			SessionName: sessionInfo.SessionName,
			Username:    sessionInfo.RealUsername,
			ClientIP:    clientIP,
			Timestamp:   connectionStartTime,
			EndTime:     endTime,
			DurationMs:  durationMs,
			RomonMode:   isRomonMode,
		}
		if err := m.logWinboxActivity(sessionInfo.AccountID, sessionInfo.PeerID, activity); err != nil {
			log.Warn().Err(err).
				Str("account_id", sessionInfo.AccountID).
				Str("peer_id", sessionInfo.PeerID).
				Msg(" Failed to update Winbox activity end")
		}
	}

	log.Debug().
		Str("client", clientAddr).
		Str("backend", backendAddr).
		Int64("duration_ms", durationMs).
		Msg("Winbox session completed")
}

// NOTE: Background credential probing removed - validation happens only at SetWinboxCredentials time
// This is more efficient: credentials are validated before being stored, not in a background loop

// isClosedError returns true if the error indicates a closed connection
func isClosedError(err error) bool {
	if err == nil {
		return false
	}
	if err == io.EOF {
		return true
	}
	if netErr, ok := err.(*net.OpError); ok {
		return netErr.Err.Error() == "use of closed network connection"
	}
	return false
}

// ProxyConnection creates a bidirectional proxy between client and backend
// Core of the Winbox multiplexer - forwards all traffic transparently
func ProxyConnection(client, backend net.Conn) error {
	errChan := make(chan error, 2)

	// Client -> Backend
	go func() {
		_, err := io.Copy(backend, client)
		errChan <- err
	}()

	// Backend -> Client
	go func() {
		_, err := io.Copy(client, backend)
		errChan <- err
	}()

	// Wait for either direction to complete/error
	err := <-errChan

	// Close both connections
	client.Close()
	backend.Close()

	return err
}

// logWinboxMessage logs decrypted Winbox messages for debugging and reverse engineering
// Only called when WINBOX_DEBUG=1 environment variable is set
func logWinboxMessage(direction string, msgNum int, msg []byte, clientAddr string, session *SessionInfo) {
	// Winbox messages start with "M2" header
	hasM2Header := len(msg) >= 2 && msg[0] == 'M' && msg[1] == '2'

	// Create hex dump for debugging
	hexDump := hex.EncodeToString(msg)

	// Try to extract readable ASCII content (printable chars only)
	var asciiContent strings.Builder
	for _, b := range msg {
		if b >= 32 && b < 127 {
			asciiContent.WriteByte(b)
		} else {
			asciiContent.WriteByte('.')
		}
	}

	// Log with structured fields for easy parsing
	log.Debug().
		Str("direction", direction).
		Int("msg_num", msgNum).
		Int("msg_len", len(msg)).
		Bool("has_m2_header", hasM2Header).
		Str("client", clientAddr).
		Str("account_id", session.AccountID).
		Str("peer_id", session.PeerID).
		Str("router_ip", session.RouterIP).
		Str("hex", hexDump).
		Str("ascii", asciiContent.String()).
		Msg(" WINBOX DEBUG")

	// Also print to stdout for easier capture
	fmt.Printf("\n=== WINBOX DEBUG [%s] Msg #%d ===\n", direction, msgNum)
	fmt.Printf("Client: %s | Account: %s | Peer: %s | Router: %s\n",
		clientAddr, session.AccountID, session.PeerID, session.RouterIP)
	fmt.Printf("Length: %d bytes | M2 Header: %v\n", len(msg), hasM2Header)
	fmt.Printf("HEX:\n%s\n", formatHexDump(msg))
	fmt.Printf("ASCII: %s\n", asciiContent.String())
	fmt.Println("================================")
}

// formatHexDump creates a formatted hex dump with offsets (like xxd)
func formatHexDump(data []byte) string {
	var result strings.Builder
	for i := 0; i < len(data); i += 16 {
		// Offset
		fmt.Fprintf(&result, "%08x: ", i)

		// Hex bytes
		for j := range 16 {
			if i+j < len(data) {
				fmt.Fprintf(&result, "%02x ", data[i+j])
			} else {
				result.WriteString("   ")
			}
			if j == 7 {
				result.WriteString(" ")
			}
		}

		// ASCII representation
		result.WriteString(" |")
		for j := 0; j < 16 && i+j < len(data); j++ {
			b := data[i+j]
			if b >= 32 && b < 127 {
				result.WriteByte(b)
			} else {
				result.WriteByte('.')
			}
		}
		result.WriteString("|\n")
	}
	return result.String()
}
