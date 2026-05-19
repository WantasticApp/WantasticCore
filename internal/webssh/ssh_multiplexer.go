package webssh

import (
	"context"
	"fmt"
	"io"
	"net"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/rs/zerolog/log"
	"golang.org/x/crypto/ssh"
)

// sshKeepaliveInterval controls how often the keepalive goroutine probes the
// underlying SSH transport.  15 s is aggressive enough to detect a dead
// WireGuard peer within one probe interval while still leaving ample headroom
// for any NAT/firewall timeout (typically ≥ 30 s).
const sshKeepaliveInterval = 15 * time.Second

// sshKeepaliveMaxFailures is the number of consecutive keepalive failures
// before the multiplexer is declared dead.  We fail after 2 misses (30 s
// total) rather than waiting for 3 (90 s) so the pool is recycled faster.
const sshKeepaliveMaxFailures = 2

// sshKeepaliveActiveSessionMaxFailures is a slightly larger failure budget
// while a live terminal channel is attached. Interactive SSH sessions can
// survive short jitter bursts or transient stream stalls that should not tear
// down an otherwise working shell. We still detect a truly dead transport, but
// we give active sessions two extra probe windows before declaring the mux dead.
const sshKeepaliveActiveSessionMaxFailures = 4

// sshMaxSessionsPerMux caps the number of concurrent SSH sessions that may
// share a single TCP/SSH multiplexer.  Beyond this limit callers must open a
// new multiplexer.  Most SSH servers default to 10–100 channels; 64 is a safe
// conservative cap.
const sshMaxSessionsPerMux = 64

// noDelayer is an optional interface exposed by some net.Conn implementations
// (including the gVisor-based WireGuard virtual TCP stack) that allows callers
// to disable Nagle's algorithm for lower interactive latency.
type noDelayer interface{ SetNoDelay(bool) error }

// keepAliver is implemented by *net.TCPConn (and any wrapper that forwards the
// methods). It lets us turn on kernel-level TCP keepalive on the underlying
// transport. SSH-level keepalive runs at the SSH session layer; OS keepalive
// runs at the TCP layer and is what catches stuck NAT entries / dead routes
// the SSH ping can't see.
type keepAliver interface {
	SetKeepAlive(bool) error
	SetKeepAlivePeriod(time.Duration) error
}

// SSHMultiplexer manages multiple SSH sessions over a single TCP connection.
// This dramatically reduces overhead for multiple terminal windows to the same host.
type SSHMultiplexer struct {
	tenantID           string
	peerID             string // Peer ID for tracking and metadata updates
	target             string
	conn               net.Conn
	sshClient          *ssh.Client
	sessions           map[string]*MuxSession
	mu                 sync.RWMutex
	ctx                context.Context
	cancel             context.CancelFunc
	lastActive         time.Time
	alive              bool // Set to false when keepalive fails (dead connection)
	compatibilityMode  SSHCompatibilityMode
	hostKey            []byte
	hostKeyFingerprint string
	hostKeyAlgorithm   string

	// Peer update callback (optional) - updates peer metadata after successful connections
	updatePeerSession func(accountID, peerID string, updateFn func(any) error) error
}

// MuxSession represents a single multiplexed SSH session (one terminal).
type MuxSession struct {
	ID        string
	Session   *ssh.Session
	stdin     io.WriteCloser
	stdout    io.Reader
	stderr    io.Reader
	Created   time.Time
	BytesSent uint64
	BytesRecv uint64
	// closed is set atomically by CloseSession before the session is removed
	// from the multiplexer's map.  Callers that obtained a *MuxSession pointer
	// via GetSession should call IsClosed() to guard against the race between
	// "load from map" and "concurrent CloseSession".
	closed atomic.Bool
}

// IsClosed reports whether this session has been closed by CloseSession.
// Safe to call from any goroutine without holding any mutex.
func (s *MuxSession) IsClosed() bool { return s.closed.Load() }

// NewSSHMultiplexer creates a multiplexed SSH connection.
func NewSSHMultiplexer(ctx context.Context, tenantID, peerID, target string, conn net.Conn, sshConfig *ssh.ClientConfig, compatibilityMode SSHCompatibilityMode, hostKeyPolicy *sshHostKeyPolicy) (*SSHMultiplexer, error) {
	// Best-effort: disable Nagle's algorithm so small SSH packets (keystrokes)
	// are sent immediately rather than being coalesced by the TCP stack.  The
	// WireGuard virtual TCP implementation honours this flag.
	if nd, ok := conn.(noDelayer); ok {
		_ = nd.SetNoDelay(true)
	}
	// Best-effort: enable kernel TCP keepalive at 30 s. This is independent
	// of the 15 s SSH-level keepalive and catches conditions the SSH ping
	// can't (stuck NAT/conntrack entries, kernel-side half-open sockets,
	// MikroTik routers that quietly drop the conn without sending FIN).
	// gVisor virtual TCP doesn't implement this — type assertion is fine.
	if ka, ok := conn.(keepAliver); ok {
		_ = ka.SetKeepAlive(true)
		_ = ka.SetKeepAlivePeriod(30 * time.Second)
	}

	// Create SSH client over the TCP connection
	sshConn, chans, reqs, err := ssh.NewClientConn(conn, target, sshConfig)
	if err != nil {
		conn.Close()
		return nil, fmt.Errorf("SSH handshake failed: %w", err)
	}

	sshClient := ssh.NewClient(sshConn, chans, reqs)

	hostKey, hostKeyFingerprint, hostKeyAlgorithm := hostKeyPolicy.acceptedMetadata()
	muxCtx, cancel := context.WithCancel(ctx)
	mux := &SSHMultiplexer{
		tenantID:           tenantID,
		peerID:             peerID,
		target:             target,
		conn:               conn,
		sshClient:          sshClient,
		sessions:           make(map[string]*MuxSession),
		ctx:                muxCtx,
		cancel:             cancel,
		lastActive:         time.Now(),
		alive:              true,
		compatibilityMode:  compatibilityMode,
		hostKey:            hostKey,
		hostKeyFingerprint: hostKeyFingerprint,
		hostKeyAlgorithm:   hostKeyAlgorithm,
	}

	// Start SSH keepalive goroutine to prevent router idle timeouts
	// and detect dead connections early for pool cleanup.
	go mux.keepaliveLoop()

	log.Debug().
		Str("target", target).
		Str("tenant_id", tenantID).
		Str("peer_id", peerID).
		Str("compatibility_mode", string(compatibilityMode)).
		Str("host_key_fingerprint", hostKeyFingerprint).
		Msg("🔌 Created SSH multiplexer with keepalive")

	return mux, nil
}

// SetPeerUpdateFunc sets the peer update callback for updating session metadata.
func (m *SSHMultiplexer) SetPeerUpdateFunc(fn func(accountID, peerID string, updateFn func(any) error) error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.updatePeerSession = fn
}

// NewSession creates a new multiplexed session (terminal window).
func (m *SSHMultiplexer) NewSession(sessionID string, rows, cols int) (*MuxSession, error) {
	m.mu.Lock()

	// Fail-fast: don't try to open a new SSH channel on a known-dead transport.
	// The caller's retry logic will spin up a fresh multiplexer instead.
	if !m.alive {
		m.mu.Unlock()
		return nil, fmt.Errorf("SSH multiplexer transport is dead; retry with a new connection")
	}

	// Enforce per-mux session cap so we don't overwhelm the remote sshd.
	if len(m.sessions) >= sshMaxSessionsPerMux {
		m.mu.Unlock()
		return nil, fmt.Errorf("SSH multiplexer session limit reached (%d)", sshMaxSessionsPerMux)
	}

	// Create SSH session
	session, err := m.sshClient.NewSession()
	if err != nil {
		m.mu.Unlock()
		return nil, fmt.Errorf("failed to create SSH session: %w", err)
	}

	// Request PTY
	if err := session.RequestPty("xterm-256color", rows, cols, ssh.TerminalModes{
		ssh.ECHO:          1,
		ssh.TTY_OP_ISPEED: 38400,
		ssh.TTY_OP_OSPEED: 38400,
	}); err != nil {
		session.Close()
		m.mu.Unlock()
		return nil, fmt.Errorf("PTY request failed: %w", err)
	}

	// Get I/O pipes
	stdin, err := session.StdinPipe()
	if err != nil {
		session.Close()
		m.mu.Unlock()
		return nil, fmt.Errorf("stdin pipe failed: %w", err)
	}

	stdout, err := session.StdoutPipe()
	if err != nil {
		session.Close()
		m.mu.Unlock()
		return nil, fmt.Errorf("stdout pipe failed: %w", err)
	}

	stderr, err := session.StderrPipe()
	if err != nil {
		session.Close()
		m.mu.Unlock()
		return nil, fmt.Errorf("stderr pipe failed: %w", err)
	}

	// Start shell
	if err := session.Shell(); err != nil {
		session.Close()
		m.mu.Unlock()
		return nil, fmt.Errorf("failed to start shell: %w", err)
	}

	combinedReader, combinedWriter := io.Pipe()
	var outputWG sync.WaitGroup
	outputWG.Add(2)

	copyOutput := func(name string, src io.Reader) {
		defer outputWG.Done()
		if _, err := io.Copy(combinedWriter, src); err != nil && err != io.ErrClosedPipe {
			log.Debug().
				Str("session_id", sessionID).
				Str("stream", name).
				Err(err).
				Msg("SSH output stream closed")
		}
	}

	go copyOutput("stdout", stdout)
	go copyOutput("stderr", stderr)
	go func() {
		outputWG.Wait()
		_ = combinedWriter.Close()
	}()

	muxSession := &MuxSession{
		ID:      sessionID,
		Session: session,
		stdin:   stdin,
		stdout:  combinedReader,
		stderr:  stderr,
		Created: time.Now(),
	}

	m.sessions[sessionID] = muxSession
	m.lastActive = time.Now()

	// Capture first-session flag and peer identifiers while holding the lock,
	// then release before calling the update callback to avoid deadlock.
	isFirstSession := len(m.sessions) == 1
	tenantID := m.tenantID
	peerID := m.peerID
	m.mu.Unlock()

	// Update peer session metadata after successful connection (first session only).
	if isFirstSession && m.updatePeerSession != nil && peerID != "" {
		if err := m.updatePeerSession(tenantID, peerID, func(data any) error {
			now := time.Now()
			if peer, ok := data.(interface{ SetWebSSHLastConnected(time.Time) }); ok {
				peer.SetWebSSHLastConnected(now)
			}
			return nil
		}); err != nil {
			log.Warn().Err(err).
				Str("tenant_id", tenantID).
				Str("peer_id", peerID).
				Msg("⚠️  Failed to update peer WebSSH LastConnected timestamp")
		} else {
			log.Debug().
				Str("tenant_id", tenantID).
				Str("peer_id", peerID).
				Msg("✅ Updated peer WebSSH LastConnected timestamp")
		}
	}

	log.Debug().
		Str("session_id", sessionID).
		Str("target", m.target).
		Int("total_sessions", len(m.sessions)).
		Msg("🖥️  Created multiplexed SSH session")

	return muxSession, nil
}

// CloseSession closes a specific multiplexed session.
func (m *SSHMultiplexer) CloseSession(sessionID string) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if session, exists := m.sessions[sessionID]; exists {
		// Mark closed atomically BEFORE deleting from the map so that any
		// concurrent caller that already holds the *MuxSession pointer (via
		// GetSession returning before this lock was acquired) can detect the
		// stale reference via IsClosed() and discard it.
		session.closed.Store(true)
		session.Session.Close()
		delete(m.sessions, sessionID)

		log.Debug().
			Str("session_id", sessionID).
			Int("remaining_sessions", len(m.sessions)).
			Msg("Closed multiplexed SSH session")
	}

	// IMPORTANT: Do NOT close the multiplexer here.  The pool's cleanup loop
	// is the sole owner of the multiplexer lifetime; keeping the underlying
	// SSH connection alive enables reuse by the next terminal window.
}

// ResizeSession resizes a terminal session.
func (m *SSHMultiplexer) ResizeSession(sessionID string, rows, cols int) error {
	m.mu.RLock()
	session, exists := m.sessions[sessionID]
	m.mu.RUnlock()

	if !exists {
		return fmt.Errorf("session not found")
	}

	return session.Session.WindowChange(rows, cols)
}

// Write sends data to a specific session's stdin.
func (m *SSHMultiplexer) Write(sessionID string, data []byte) (int, error) {
	m.mu.RLock()
	session, exists := m.sessions[sessionID]
	m.mu.RUnlock()

	if !exists {
		return 0, fmt.Errorf("session not found")
	}

	n, err := session.stdin.Write(data)
	if err == nil {
		session.BytesSent += uint64(n)
		m.lastActive = time.Now()
	}
	return n, err
}

// Read reads data from a specific session's stdout.
func (m *SSHMultiplexer) Read(sessionID string, buf []byte) (int, error) {
	m.mu.RLock()
	session, exists := m.sessions[sessionID]
	m.mu.RUnlock()

	if !exists {
		return 0, fmt.Errorf("session not found")
	}

	n, err := session.stdout.Read(buf)
	if err == nil {
		session.BytesRecv += uint64(n)
		m.lastActive = time.Now()
	}
	return n, err
}

// Close closes the multiplexer and all sessions.
func (m *SSHMultiplexer) Close() error {
	m.cancel()

	m.mu.Lock()
	m.alive = false
	defer m.mu.Unlock()

	// Mark and close all sessions.
	for _, session := range m.sessions {
		session.closed.Store(true)
		session.Session.Close()
	}
	m.sessions = make(map[string]*MuxSession)

	if m.sshClient != nil {
		m.sshClient.Close()
	}
	if m.conn != nil {
		m.conn.Close()
	}

	log.Debug().Str("target", m.target).Msg("Closed SSH multiplexer")
	return nil
}

// SessionCount returns the number of active sessions.
func (m *SSHMultiplexer) SessionCount() int {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return len(m.sessions)
}

// LastActive returns the last activity time.
func (m *SSHMultiplexer) LastActive() time.Time {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.lastActive
}

// touchActivity records successful interactive SSH traffic on the transport.
// This is used by the stream bridge so a live shell counts as fresh activity
// even before the next periodic keepalive probe lands.
func (m *SSHMultiplexer) touchActivity() {
	m.mu.Lock()
	m.lastActive = time.Now()
	m.mu.Unlock()
}

// GetSession returns a specific multiplexed session by ID.
// The caller MUST check IsClosed() on the returned session to guard against
// the race where CloseSession runs concurrently after the map lookup but
// before the caller uses the session.
func (m *SSHMultiplexer) GetSession(sessionID string) *MuxSession {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.sessions[sessionID]
}

// IsAlive returns whether the multiplexer's SSH connection is healthy.
// The keepalive goroutine sets this to false when it detects a dead connection.
func (m *SSHMultiplexer) IsAlive() bool {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.alive
}

// keepaliveLoop sends SSH keepalive requests periodically to:
//  1. Prevent routers/firewalls from closing idle SSH connections.
//  2. Detect dead connections early so the pool can recycle them fast.
//
// The interval (15 s) and max failures (2) are intentionally aggressive:
// dead connections are marked within 30 s instead of the previous 90 s.
func (m *SSHMultiplexer) keepaliveLoop() {
	ticker := time.NewTicker(sshKeepaliveInterval)
	defer ticker.Stop()

	failures := 0

	for {
		select {
		case <-m.ctx.Done():
			return
		case <-ticker.C:
			_, _, err := m.sshClient.SendRequest("keepalive@openssh.com", true, nil)
			if err != nil {
				// On a fatal transport error (reset, broken pipe, EOF) there is
				// no point waiting for further probes — mark dead immediately.
				if isSSHTransportDead(err) {
					log.Debug().
						Str("target", m.target).
						Err(err).
						Msg("💀 SSH transport fatally dead; marking multiplexer for cleanup")
					m.mu.Lock()
					m.alive = false
					m.mu.Unlock()
					return
				}

				failures++
				maxFailures := sshKeepaliveFailureBudget(m.SessionCount())
				log.Debug().
					Str("target", m.target).
					Int("failures", failures).
					Int("max", maxFailures).
					Err(err).
					Msg("💔 SSH keepalive failed")

				if failures >= maxFailures {
					log.Warn().
						Str("target", m.target).
						Msg("💀 SSH connection dead after keepalive failures; marking for cleanup")
					m.mu.Lock()
					m.alive = false
					m.mu.Unlock()
					return
				}
			} else {
				failures = 0 // reset consecutive counter on success
				// Bump lastActive on every successful keepalive: the pool's
				// reaper uses this signal to decide whether a mux is "fresh".
				// Without this, an interactive session where the user is
				// reading (no Read/Write traffic) but the keepalive is happily
				// pinging every 15 s appears stale to the pool and risks
				// being reaped if RefCount drops to zero.
				m.mu.Lock()
				m.lastActive = time.Now()
				m.mu.Unlock()
			}
		}
	}
}

func sshKeepaliveFailureBudget(sessionCount int) int {
	if sessionCount > 0 {
		return sshKeepaliveActiveSessionMaxFailures
	}
	return sshKeepaliveMaxFailures
}

// isSSHTransportDead returns true for errors that indicate the underlying TCP
// connection is permanently gone.  These errors do not recover on retry; the
// multiplexer must be closed and a new one dialled.
func isSSHTransportDead(err error) bool {
	if err == nil {
		return false
	}
	if err == io.EOF {
		return true
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "connection reset") ||
		strings.Contains(msg, "broken pipe") ||
		strings.Contains(msg, "use of closed network connection") ||
		strings.Contains(msg, "connection refused")
}
