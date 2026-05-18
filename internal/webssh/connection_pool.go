package webssh

import (
	"WantasticCore/internal/wg/userspace"
	"context"
	"errors"
	"fmt"
	"net"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/rs/zerolog/log"
)

// SSHConnectionPool manages reusable SSH multiplexers to reduce handshake
// overhead and allow multiple terminal windows over one TCP connection.
//
// # Concurrency design
//
// The hot path (GetMultiplexer for an already-pooled target) is lock-free: it
// uses sync.Map.Load which performs an atomic read with no mutex acquisition.
// Only the slow path (dial + insert) and cleanup need locks, and those are
// scoped to individual PooledMultiplexer entries rather than the whole map.
// This scales to 1 000+ concurrent SSH sessions without a global bottleneck.
type SSHConnectionPool struct {
	manager *userspace.UserspaceManager

	// multiplexers holds *PooledMultiplexer values keyed by poolKey strings.
	// sync.Map is chosen over map+RWMutex because:
	//   • Load (hot path) is fully lock-free for stable key sets.
	//   • Range for cleanup does not block concurrent Load/Store.
	//   • CompareAndDelete allows safe concurrent removal.
	multiplexers sync.Map

	// muxCount is an approximate count of live entries.  It may briefly drift
	// under heavy concurrent insert/delete, but is sufficient for the global
	// connection limit check.
	muxCount atomic.Int64

	maxIdleTime    time.Duration
	maxConnections int
	cleanupTicker  *time.Ticker
	stopCh         chan struct{}

	// Peer update callback (optional) — injected from server to update peer metadata.
	updatePeerSession func(accountID, peerID string, updateFn func(any) error) error
}

var ErrSSHTunnelUnavailable = errors.New("ssh peer tunnel unavailable")

// PooledMultiplexer represents a reusable SSH multiplexer with multiple sessions.
type PooledMultiplexer struct {
	Mux      *SSHMultiplexer
	TenantID string
	Target   string
	LastUsed time.Time
	RefCount int // Number of active WebSocket sessions using this multiplexer
	mu       sync.Mutex
}

// NewSSHConnectionPool creates a connection pool with automatic cleanup.
func NewSSHConnectionPool(manager *userspace.UserspaceManager, maxIdle time.Duration, maxConns int) *SSHConnectionPool {
	pool := &SSHConnectionPool{
		manager:        manager,
		maxIdleTime:    maxIdle,
		maxConnections: maxConns,
		cleanupTicker:  time.NewTicker(30 * time.Second),
		stopCh:         make(chan struct{}),
	}

	go pool.cleanupLoop()

	log.Debug().
		Dur("max_idle", maxIdle).
		Int("max_connections", maxConns).
		Msg("🏊 Initialized SSH multiplexer pool")

	return pool
}

// SSHCredentials contains SSH authentication credentials.
type SSHCredentials struct {
	Username             string
	Password             string
	PrivateKey           string
	PrivateKeyPassphrase string
	AuthHandler          InteractiveAuthHandler
}

const (
	sshEndpointWarmupTimeout = 6 * time.Second
	sshEndpointPollInterval  = 200 * time.Millisecond
)

// InteractiveAuthHandler allows interactive SSH prompting via the gRPC stream.
type InteractiveAuthHandler interface {
	Prompt(question string, echo bool) (string, error)
	Banner(message string) error
}

func poolKey(tenantID, peerIP string, port int, username string, mode SSHCompatibilityMode, hostKeyFingerprint string) string {
	return fmt.Sprintf("%s:%s:%d:%s:%s:%s",
		tenantID, peerIP, port, username, string(mode), hostKeyFingerprint)
}

func waitForPeerEndpoint(ctx context.Context, peerIP string, hasEndpoint func() bool) error {
	if hasEndpoint() {
		return nil
	}

	waitCtx := ctx
	cancel := func() {}
	if deadline, ok := ctx.Deadline(); !ok || time.Until(deadline) > sshEndpointWarmupTimeout {
		waitCtx, cancel = context.WithTimeout(ctx, sshEndpointWarmupTimeout)
	}
	defer cancel()

	ticker := time.NewTicker(sshEndpointPollInterval)
	defer ticker.Stop()

	for {
		select {
		case <-waitCtx.Done():
			if hasEndpoint() {
				return nil
			}
			return fmt.Errorf(
				"%w: peer %s has no known WireGuard endpoint yet; wait for the device to reconnect",
				ErrSSHTunnelUnavailable, peerIP,
			)
		case <-ticker.C:
			if hasEndpoint() {
				return nil
			}
		}
	}
}

func isNoKnownEndpointError(err error) bool {
	return err != nil && strings.Contains(err.Error(), "no known endpoint for peer")
}

// SetPeerUpdateFunc sets the peer update callback.
func (p *SSHConnectionPool) SetPeerUpdateFunc(fn func(accountID, peerID string, updateFn func(any) error) error) {
	p.updatePeerSession = fn
}

// GetMultiplexer gets or creates a pooled SSH multiplexer.
//
// Fast path (existing live multiplexer): lock-free sync.Map.Load + per-entry
// mutex for RefCount increment — O(1), no global lock.
//
// Slow path (new connection): dials and handshakes without any global lock,
// then uses sync.Map.LoadOrStore for a race-free insert.
func (p *SSHConnectionPool) GetMultiplexer(connectCtx context.Context, session *DirectSSHSession, creds *SSHCredentials) (*PooledMultiplexer, error) {
	if connectCtx == nil {
		connectCtx = context.Background()
	}

	requestedMode := session.CompatibilityMode
	key := poolKey(session.TenantID, session.PeerIP, session.Port, creds.Username, requestedMode, session.HostKeyFingerprint)

	// ── Fast path: lockless load ──────────────────────────────────────────────
	if v, ok := p.multiplexers.Load(key); ok {
		pooled := v.(*PooledMultiplexer)
		pooled.mu.Lock()
		if pooled.Mux.IsAlive() {
			pooled.RefCount++
			pooled.LastUsed = time.Now()
			pooled.mu.Unlock()

			log.Debug().
				Str("target", key).
				Int("ref_count", pooled.RefCount).
				Int("sessions", pooled.Mux.SessionCount()).
				Msg("♻️  Reusing pooled SSH multiplexer")
			return pooled, nil
		}
		// Dead entry: close and evict.  Another goroutine may race to do the
		// same; both paths are safe — the worst case is a redundant Close().
		pooled.Mux.Close()
		pooled.mu.Unlock()
		if p.multiplexers.CompareAndDelete(key, v) {
			p.muxCount.Add(-1)
		}
	}

	// ── Global limit check (approximate) ─────────────────────────────────────
	if p.maxConnections > 0 && p.muxCount.Load() >= int64(p.maxConnections) {
		return nil, fmt.Errorf("SSH multiplexer pool at capacity (%d)", p.maxConnections)
	}

	// ── Slow path: create new multiplexer (no global lock held) ──────────────
	device, err := p.manager.GetDevice(session.TenantID)
	if err != nil {
		return nil, fmt.Errorf("tenant not found: %w", err)
	}

	target := fmt.Sprintf("%s:%d", session.PeerIP, session.Port)
	handshakeCtx, cancel := context.WithTimeout(connectCtx, sshConnectTimeout)
	defer cancel()

	if !device.HasEndpoint(session.PeerIP) {
		log.Debug().Str("target", target).Msg("Waiting briefly for WireGuard endpoint before SSH dial")
		if err := waitForPeerEndpoint(handshakeCtx, session.PeerIP, func() bool {
			return device.HasEndpoint(session.PeerIP)
		}); err != nil {
			return nil, err
		}
	}

	muxLifetimeCtx := session.ctx
	if muxLifetimeCtx == nil {
		muxLifetimeCtx = context.Background()
	}

	attemptModes := []SSHCompatibilityMode{normalizeSSHCompatibilityMode(requestedMode)}
	if requestedMode == SSHCompatibilityUnknown {
		attemptModes = []SSHCompatibilityMode{SSHCompatibilityModern, SSHCompatibilityLegacy}
	}

	var (
		mux              *SSHMultiplexer
		selectedMode     SSHCompatibilityMode
		lastErr          error
		legacyRetried    bool
		hostKeyMismatch  bool // true when we already cleared the stale key and retried
	)

	// dialOnce dials a single TCP connection to target and returns it along with
	// any error.  All dial errors that are not "no known endpoint" are surfaced
	// directly to the caller.
	dialOnce := func() (net.Conn, error) {
		conn, dialErr := device.Net.DialContext(handshakeCtx, "tcp", target)
		if dialErr != nil {
			if isNoKnownEndpointError(dialErr) {
				return nil, fmt.Errorf(
					"%w: peer %s has no known WireGuard endpoint yet; wait for the device to reconnect",
					ErrSSHTunnelUnavailable, session.PeerIP,
				)
			}
			return nil, fmt.Errorf("failed to connect to %s: %w", target, dialErr)
		}
		if deadline, ok := handshakeCtx.Deadline(); ok {
			if err := conn.SetDeadline(deadline); err != nil {
				log.Debug().Str("target", target).Err(err).Msg("Failed to apply SSH handshake deadline")
			}
		}
		return conn, nil
	}

	for attemptIndex, attemptMode := range attemptModes {
		// Build host key policy from what the session has stored.
		// On a host-key-mismatch retry the session fields are already cleared,
		// so this becomes TOFU (Trust On First Use).
		hostKeyPolicy := newSSHHostKeyPolicy(session.HostKey, session.HostKeyFingerprint, session.HostKeyAlgorithm)
		sshConfig, configErr := buildSSHClientConfig(creds, target, attemptMode, hostKeyPolicy)
		if configErr != nil {
			return nil, fmt.Errorf("failed to build SSH client config: %w", configErr)
		}

		conn, dialErr := dialOnce()
		if dialErr != nil {
			return nil, dialErr
		}

		mux, lastErr = NewSSHMultiplexer(muxLifetimeCtx, session.TenantID, session.PeerID, target, conn, sshConfig, attemptMode, hostKeyPolicy)
		_ = conn.SetDeadline(time.Time{})
		if lastErr == nil {
			selectedMode = attemptMode
			break
		}

		conn.Close()

		// ── Host key mismatch: server key changed (reinstall / rotation) ──────
		// Clear the stale stored key and retry once with TOFU so we accept the
		// new key and persist it.  This covers every SSH server type and auth
		// method because the fix is purely at the transport/host-key layer.
		if !hostKeyMismatch && isSSHHostKeyMismatch(lastErr) {
			hostKeyMismatch = true
			oldFP := session.HostKeyFingerprint
			session.HostKey = nil
			session.HostKeyFingerprint = ""
			session.HostKeyAlgorithm = ""
			log.Warn().
				Str("target", target).
				Str("old_fingerprint", oldFP).
				Str("reason", clampSSHText(lastErr.Error(), sshLegacyRetryLogReasonMax)).
				Msg("⚠️  SSH host key changed; clearing stored key and retrying with TOFU")

			// Re-run the same mode attempt (do not advance attemptIndex).
			conn2, dialErr2 := dialOnce()
			if dialErr2 != nil {
				return nil, dialErr2
			}
			hostKeyPolicy2 := newSSHHostKeyPolicy(nil, "", "")
			sshConfig2, configErr2 := buildSSHClientConfig(creds, target, attemptMode, hostKeyPolicy2)
			if configErr2 != nil {
				return nil, fmt.Errorf("failed to build SSH client config after key reset: %w", configErr2)
			}
			mux, lastErr = NewSSHMultiplexer(muxLifetimeCtx, session.TenantID, session.PeerID, target, conn2, sshConfig2, attemptMode, hostKeyPolicy2)
			_ = conn2.SetDeadline(time.Time{})
			if lastErr == nil {
				selectedMode = attemptMode
				break
			}
			conn2.Close()
			// TOFU retry also failed — fall through to the normal error path.
		}

		// ── Legacy algorithm retry ─────────────────────────────────────────────
		if attemptIndex == 0 && len(attemptModes) > 1 && shouldRetryWithLegacy(lastErr) {
			legacyRetried = true
			log.Warn().
				Str("target", target).
				Str("username", creds.Username).
				Str("retry_mode", string(SSHCompatibilityLegacy)).
				Str("reason", clampSSHText(lastErr.Error(), sshLegacyRetryLogReasonMax)).
				Msg("Retrying SSH handshake with legacy compatibility")
			continue
		}
		return nil, fmt.Errorf("failed to create multiplexer: %w", lastErr)
	}
	if mux == nil {
		return nil, fmt.Errorf("failed to create multiplexer: %w", lastErr)
	}

	if p.updatePeerSession != nil {
		mux.SetPeerUpdateFunc(p.updatePeerSession)
	}

	// ── Atomic insert (double-check) ─────────────────────────────────────────
	// Use the actual fingerprint negotiated during handshake (may differ from
	// the requested one when the expected fingerprint was empty).
	actualKey := poolKey(session.TenantID, session.PeerIP, session.Port, creds.Username, selectedMode, mux.hostKeyFingerprint)

	newPooled := &PooledMultiplexer{
		Mux:      mux,
		TenantID: session.TenantID,
		Target:   actualKey,
		LastUsed: time.Now(),
		RefCount: 1,
	}

	if actual, existed := p.multiplexers.LoadOrStore(actualKey, newPooled); existed {
		// Another goroutine beat us to it while we were dialling.
		existing := actual.(*PooledMultiplexer)
		existing.mu.Lock()
		if existing.Mux.IsAlive() {
			existing.RefCount++
			existing.mu.Unlock()
			mux.Close() // discard our redundant connection
			log.Debug().
				Str("target", actualKey).
				Int("ref_count", existing.RefCount).
				Msg("♻️  Reusing multiplexer created by concurrent goroutine")
			return existing, nil
		}
		// The concurrent entry is already dead — overwrite with ours.
		existing.mu.Unlock()
		p.multiplexers.Store(actualKey, newPooled)
		existing.Mux.Close()
		// muxCount is unchanged (replacing dead with live, net zero)
	} else {
		p.muxCount.Add(1)
	}

	log.Debug().
		Str("target", target).
		Str("username", creds.Username).
		Str("compatibility_mode", string(selectedMode)).
		Str("host_key_fingerprint", mux.hostKeyFingerprint).
		Bool("legacy_retry", legacyRetried).
		Int64("pool_size", p.muxCount.Load()).
		Msg("🔌 Created new SSH multiplexer")

	return newPooled, nil
}

// FindMultiplexer returns a live multiplexer for a target without changing its
// refcount.  Returns nil if no live entry exists for the session.
func (p *SSHConnectionPool) FindMultiplexer(session *DirectSSHSession) *PooledMultiplexer {
	key := poolKey(session.TenantID, session.PeerIP, session.Port, session.Username, session.CompatibilityMode, session.HostKeyFingerprint)

	v, ok := p.multiplexers.Load(key)
	if !ok {
		return nil
	}

	pooled := v.(*PooledMultiplexer)
	pooled.mu.Lock()
	defer pooled.mu.Unlock()

	if !pooled.Mux.IsAlive() {
		go p.RemoveMultiplexer(session)
		return nil
	}

	pooled.LastUsed = time.Now()
	return pooled
}

// ReleaseMultiplexer releases a multiplexer back to the pool.
// Decrements ref count and closes the associated MuxSession.
func (p *SSHConnectionPool) ReleaseMultiplexer(session *DirectSSHSession, sessionID string) {
	key := poolKey(session.TenantID, session.PeerIP, session.Port, session.Username, session.CompatibilityMode, session.HostKeyFingerprint)

	v, ok := p.multiplexers.Load(key)
	if !ok {
		return
	}

	pooled := v.(*PooledMultiplexer)
	pooled.mu.Lock()
	pooled.RefCount--
	if pooled.RefCount < 0 {
		pooled.RefCount = 0 // guard against double-release bugs
	}
	pooled.LastUsed = time.Now()
	pooled.Mux.CloseSession(sessionID)

	log.Debug().
		Str("target", key).
		Str("session_id", sessionID).
		Int("ref_count", pooled.RefCount).
		Int("remaining_sessions", pooled.Mux.SessionCount()).
		Msg("Released multiplexer session")

	pooled.mu.Unlock()
}

// cleanupLoop periodically removes idle connections.
func (p *SSHConnectionPool) cleanupLoop() {
	for {
		select {
		case <-p.cleanupTicker.C:
			p.cleanup()
		case <-p.stopCh:
			return
		}
	}
}

// cleanup removes idle and dead entries without holding any global lock.
//
// Strategy:
//  1. Range over sync.Map — no lock, consistent snapshot per entry.
//  2. Decide removal under the per-entry mutex.
//  3. Use CompareAndDelete to atomically evict — safe if another goroutine
//     concurrently stored a fresh entry for the same key.
//  4. Close the underlying multiplexers AFTER eviction, outside any lock.
func (p *SSHConnectionPool) cleanup() {
	now := time.Now()

	type toClose struct {
		pooled *PooledMultiplexer
		key    any
		idleFor time.Duration
	}
	var victims []toClose

	p.multiplexers.Range(func(k, v any) bool {
		pooled := v.(*PooledMultiplexer)
		pooled.mu.Lock()
		isDead := !pooled.Mux.IsAlive()
		// Idle = time since the most recent of:
		//   - pool-level reuse (LastUsed: bumped on Get/Find/Release)
		//   - transport-level activity (Mux.LastActive: bumped on Read/Write +
		//     keepalive success — see ssh_multiplexer.go)
		// Without the LastActive arm a mux can have a session that's been
		// receiving data the whole 30 s but still get reaped because the pool
		// only sees the LastUsed timestamp from the original Get.
		freshest := pooled.LastUsed
		if active := pooled.Mux.LastActive(); active.After(freshest) {
			freshest = active
		}
		idle := now.Sub(freshest)
		isIdleEmpty := pooled.RefCount == 0 && pooled.Mux.SessionCount() == 0 && idle > p.maxIdleTime
		pooled.mu.Unlock()

		if isDead || isIdleEmpty {
			// Atomically remove only if the map still holds this exact pointer.
			// A concurrent GetMultiplexer may have already replaced it.
			if p.multiplexers.CompareAndDelete(k, v) {
				p.muxCount.Add(-1)
				victims = append(victims, toClose{pooled: pooled, key: k, idleFor: idle})
			}
		}
		return true
	})

	for _, vic := range victims {
		vic.pooled.Mux.Close()
		log.Debug().
			Any("target", vic.key).
			Dur("idle_for", vic.idleFor).
			Msg("🧹 Cleaned up idle/dead SSH multiplexer")
	}

	if len(victims) > 0 {
		log.Debug().
			Int("removed", len(victims)).
			Int64("remaining", p.muxCount.Load()).
			Msg("SSH multiplexer pool cleanup complete")
	}
}

// RemoveMultiplexer forcefully removes a multiplexer from the pool (e.g. after
// a connection error during session creation).
func (p *SSHConnectionPool) RemoveMultiplexer(session *DirectSSHSession) {
	key := poolKey(session.TenantID, session.PeerIP, session.Port, session.Username, session.CompatibilityMode, session.HostKeyFingerprint)

	if v, ok := p.multiplexers.LoadAndDelete(key); ok {
		pooled := v.(*PooledMultiplexer)
		pooled.Mux.Close()
		p.muxCount.Add(-1)
		log.Debug().Str("target", key).Msg("🚫 Removed invalid/dead SSH multiplexer from pool")
	}
}

// Close closes all pooled multiplexers and stops cleanup.
func (p *SSHConnectionPool) Close() {
	close(p.stopCh)
	p.cleanupTicker.Stop()

	p.multiplexers.Range(func(k, v any) bool {
		pooled := v.(*PooledMultiplexer)
		pooled.Mux.Close()
		p.multiplexers.Delete(k)
		return true
	})

	log.Debug().Msg("Closed SSH multiplexer pool")
}

// Stats returns pool statistics.  Uses an O(n) Range; call infrequently.
func (p *SSHConnectionPool) Stats() map[string]any {
	var totalMuxers, totalSessions int64
	p.multiplexers.Range(func(_, v any) bool {
		totalMuxers++
		pooled := v.(*PooledMultiplexer)
		pooled.mu.Lock()
		totalSessions += int64(pooled.Mux.SessionCount())
		pooled.mu.Unlock()
		return true
	})
	return map[string]any{
		"total_multiplexers": totalMuxers,
		"total_ssh_sessions": totalSessions,
	}
}
