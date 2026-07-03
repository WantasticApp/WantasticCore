// Package wuspcontroller implements the controller side of the WUSP
// (Wantastic USP) protocol.
//
// Wire transport: WireGuard message type 8. Inbound payloads arrive via
// TenantDevice.SetWUSPInboundHandler; outbound payloads are sent via
// TenantDevice.SendWUSP (which internally calls peer.SendWUSP).
//
// Wire codec: WantasticCore/internal/wusp — binary TR-181 encoding,
// LZ4 fragment compression, and USP control transport framing.
//
// # Receive loop (per the WUSP spec)
//
//  1. try DecodeUSPControlFragment
//  2. if fragment, reassemble; evict stale fragment buffers on every sweep
//  3. try DecodeUSPAgentResponse → match by ID → unblock caller
//  4. try DecodeUSPAgentRequest → if IsEventNotifyRequest → DecodeEventFromRequest → OnEvent
//  5. if IsSubscriptionRequest → acknowledge (controller acts as agent-side recipient)
//
// # Param validation
//
// SetValidated uses the bundled Device model to reject writes to read-only
// parameters before the request is encoded and sent, returning a typed
// *wusp.ValidationError with the offending path.
//
// # Memory management
//
// Fragment buffers have a TTL (fragmentTTL = 30 s). A background goroutine
// started by Start() sweeps expired buffers every fragmentSweepInterval.
// The per-peer fragment buffer is also capped at maxFragmentBufs to prevent
// memory exhaustion from a misbehaving or malicious peer.
package wuspcontroller

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"WantasticCore/internal/store"
	"WantasticCore/internal/wusp"

	"github.com/rs/zerolog"
)

// ── tuning constants ──────────────────────────────────────────────────────────

const (
	// fragmentTTL is the maximum age of an incomplete fragment set before it
	// is evicted to free memory.
	fragmentTTL = 30 * time.Second

	// fragmentSweepInterval is how often the background goroutine scans for
	// stale fragment buffers.
	fragmentSweepInterval = 10 * time.Second

	// maxFragmentBufs is the maximum number of concurrent incomplete messages
	// from a single peer before the oldest buffer is dropped. This bounds
	// memory per peer to maxFragmentBufs × WUSPMaxDatagramPayload bytes.
	maxFragmentBufs = 64

	// peerOnlineWindow is how long after the last inbound WUSP message a peer
	// is considered "online" and reachable. The agent re-announces every 2 min,
	// so 3 min gives one missed announce of headroom before declaring offline.
	// Outbound Set/Get/etc. fast-fail when the peer is outside this window.
	peerOnlineWindow = 3 * time.Minute

	// peerFailureBackoff is how long after a recent send error / response
	// timeout we keep fast-failing further outbound requests for the same
	// peer. Cleared instantly on the next inbound packet from that peer
	// (markPeerInbound), so a recovered peer is usable again right away.
	peerFailureBackoff = 30 * time.Second

	minControlPayloadBudget          = 512
	controlPayloadIncreaseStep       = 32
	controlPayloadIncreaseEvery      = 4
	idempotentControlRetryAttempts   = 2
)

// ErrPeerNotReachable is returned by Get/Set/Operate when the controller has
// not received any inbound WUSP message from the peer within peerOnlineWindow.
// Callers (e.g. WUSPService.SendSet) should surface this to the dashboard so
// the user sees an immediate "peer offline" instead of a 15-second hang.
var ErrPeerNotReachable = errors.New("wusp: peer not reachable (no recent inbound)")

// ── public callback types ─────────────────────────────────────────────────────

// SendFunc sends a raw WUSP type-8 payload to the peer identified by its
// WireGuard public key (base64 string).
type SendFunc func(peerPublicKey string, data []byte) error

// NotifyFunc is called when an unsolicited Notify method response arrives from
// a peer with no matching pending request. This is distinct from device-pushed
// events (which arrive as requests and are routed to EventFunc).
type NotifyFunc func(peerPublicKey string, resp wusp.USPAgentResponse)

// EventFunc is called for every decoded agent-to-controller event (Notify
// method request carrying event_type metadata). The event has already been
// fully decoded from the binary wire format.
type EventFunc func(peerPublicKey string, event wusp.USPEvent)

// ── Options ───────────────────────────────────────────────────────────────────

// Options configures a WUSPController.
type Options struct {
	// Send delivers outbound type-8 datagrams to a specific peer.
	// May be nil during in-process testing without wire transport.
	Send SendFunc

	// OnEvent is called for each decoded agent-originated event (e.g.
	// ValueChange, ObjectCreation, Boot!, OnBoardRequest). This is the
	// primary live-feed callback.
	// Safe to leave nil — events are silently dropped if unset.
	OnEvent EventFunc

	// OnNotify is called for unsolicited Notify method responses that
	// do not carry event_type metadata and have no matching pending request.
	// Kept for backward compatibility; most callers should prefer OnEvent.
	OnNotify NotifyFunc

	// StateRepo persists device model snapshots to PostgreSQL.
	StateRepo store.WUSPDeviceStateRepository

	// RequestTimeout is the per-request deadline for wire round-trips.
	// Defaults to 15 seconds if zero.
	RequestTimeout time.Duration

	Log zerolog.Logger
}

// ── internal types ────────────────────────────────────────────────────────────

// pendingRequest tracks an in-flight wire request awaiting a response.
type pendingRequest struct {
	ch        chan wusp.USPAgentResponse
	method    wusp.USPAgentMethod
	startedAt time.Time
}

// fragmentBuf holds an incomplete set of fragments for one MessageID.
type fragmentBuf struct {
	frags     []wusp.USPControlFragment
	arrivedAt time.Time // time of first fragment — used for TTL eviction
}

// peerSession tracks per-peer reachability state. Updated on every inbound
// WUSP message and on outbound send-error / response-timeout. Reads (in the
// send path) consult both LastInbound (slow positive signal) and LastFailure
// (fast negative signal) to decide whether to fast-fail Set/Get.
type peerSession struct {
	LastInbound time.Time
	// LastFailure is set when an outbound request to this peer either fails
	// at the WG layer (opts.Send returns error) or times out waiting for a
	// response. While within peerFailureBackoff, peerReachable returns false
	// so further requests fast-fail instead of hanging another 15 seconds.
	// Cleared on the next inbound packet (markPeerInbound) so a recovered
	// peer is instantly usable again.
	LastFailure time.Time

	ControlPayloadBudget int
	AdvertisedMaxPayload int
	ControlSuccesses     int
}

// WUSPControllerStats is a point-in-time snapshot of controller observability
// counters. Duration fields are cumulative/max wall-clock measurements.
type WUSPControllerStats struct {
	InboundDatagrams         uint64
	InboundBytes             uint64
	InboundResponses         uint64
	InboundRequests          uint64
	InboundControlFragments  uint64
	InboundTransferFrames    uint64
	ResponsesAfterTimeout    uint64
	FragmentReassemblies     uint64
	FragmentReassemblyErrors uint64
	FragmentEvictions        uint64

	OutboundRequests         uint64
	OutboundRequestFragments uint64
	OutboundBytes            uint64
	OutboundSendErrors       uint64
	PeerFastFails            uint64

	RoundTrips            uint64
	RoundTripRetries      uint64
	RoundTripTimeouts     uint64
	RoundTripSuccesses    uint64
	RoundTripFailures     uint64
	RoundTripLatencyTotal time.Duration
	RoundTripLatencyMax   time.Duration

	BudgetReductions    uint64
	BudgetIncreaseEvents uint64

	TransferFramesSent         uint64
	TransferFrameBytesSent     uint64
	TransferFramesReceived     uint64
	TransferFrameBytesReceived uint64
	TransferSessionsStarted    uint64
	TransferSessionsCompleted  uint64
	TransferSessionsAborted    uint64
	TransferAckTimeouts        uint64
	TransferChunkResends       uint64
	TransferUnknownSessions    uint64
	TransferPayloadBytes       uint64
	TransferDurationTotal      time.Duration
	TransferDurationMax        time.Duration
}

// WUSPPeerSessionSnapshot exposes the current per-peer control-plane state.
type WUSPPeerSessionSnapshot struct {
	PeerPublicKey        string
	Reachable            bool
	LastInbound          time.Time
	LastFailure          time.Time
	ControlPayloadBudget int
	AdvertisedMaxPayload int
	ControlSuccesses     int
}

type controllerStats struct {
	inboundDatagrams         atomic.Uint64
	inboundBytes             atomic.Uint64
	inboundResponses         atomic.Uint64
	inboundRequests          atomic.Uint64
	inboundControlFragments  atomic.Uint64
	inboundTransferFrames    atomic.Uint64
	responsesAfterTimeout    atomic.Uint64
	fragmentReassemblies     atomic.Uint64
	fragmentReassemblyErrors atomic.Uint64
	fragmentEvictions        atomic.Uint64

	outboundRequests         atomic.Uint64
	outboundRequestFragments atomic.Uint64
	outboundBytes            atomic.Uint64
	outboundSendErrors       atomic.Uint64
	peerFastFails            atomic.Uint64

	roundTrips           atomic.Uint64
	roundTripRetries     atomic.Uint64
	roundTripTimeouts    atomic.Uint64
	roundTripSuccesses   atomic.Uint64
	roundTripFailures    atomic.Uint64
	roundTripLatencyNS   atomic.Uint64
	roundTripLatencyMaxNS atomic.Uint64

	budgetReductions     atomic.Uint64
	budgetIncreaseEvents atomic.Uint64

	transferFramesSent         atomic.Uint64
	transferFrameBytesSent     atomic.Uint64
	transferFramesReceived     atomic.Uint64
	transferFrameBytesReceived atomic.Uint64
	transferSessionsStarted    atomic.Uint64
	transferSessionsCompleted  atomic.Uint64
	transferSessionsAborted    atomic.Uint64
	transferAckTimeouts        atomic.Uint64
	transferChunkResends       atomic.Uint64
	transferUnknownSessions    atomic.Uint64
	transferPayloadBytes       atomic.Uint64
	transferDurationNS         atomic.Uint64
	transferDurationMaxNS      atomic.Uint64
}

// ── WUSPController ────────────────────────────────────────────────────────────

// WUSPController is the server-side WUSP controller.
// It manages request/response correlation, fragment reassembly,
// param-level access control, and device model state persistence.
//
// Create with New(). Call Start() to enable background fragment cleanup.
// Do not copy after first use.
type WUSPController struct {
	opts  Options
	log   zerolog.Logger
	model *wusp.Device // read-only schema snapshot, used for param validation
	stats controllerStats

	// reqCounter is a monotonically increasing request-ID generator.
	// IDs are non-zero per the WUSP spec.
	reqCounter atomic.Uint64

	// pending maps request ID → waiting caller.
	pending   map[uint64]*pendingRequest
	pendingMu sync.RWMutex

	// fragments buffers incomplete control fragment sets, keyed by MessageID.
	// Each value is a per-peer map of msgID → fragmentBuf.
	fragments   map[string]map[uint64]*fragmentBuf // peer → msgID → buf
	fragmentsMu sync.Mutex

	// sessions maps peerPublicKey → liveness state. Used by the send path to
	// fast-fail outbound requests when a peer hasn't had inbound traffic
	// within peerOnlineWindow. Updated on every inbound WUSP message.
	sessions   map[string]*peerSession
	sessionsMu sync.RWMutex

	streams   map[string]map[uint64]*uspTransferSession
	streamsMu sync.Mutex

	// stopSweep signals the background sweep goroutine to stop.
	stopSweep chan struct{}
}

// New creates a WUSPController with the given options. Call Start() to begin
// background fragment cleanup.
func New(opts Options) *WUSPController {
	if opts.RequestTimeout == 0 {
		opts.RequestTimeout = 15 * time.Second
	}
	return &WUSPController{
		opts:      opts,
		log:       opts.Log.With().Str("component", "wuspcontroller").Logger(),
		model:     wusp.RuntimeDevice(),
		pending:   make(map[uint64]*pendingRequest),
		fragments: make(map[string]map[uint64]*fragmentBuf),
		sessions:  make(map[string]*peerSession),
		streams:   make(map[string]map[uint64]*uspTransferSession),
		stopSweep: make(chan struct{}),
	}
}

// markPeerInbound records that a WUSP message just arrived from peerPublicKey.
// Called on every HandleInbound entry so the send path can determine peer
// reachability without waiting for handshake-level timeouts. Also clears any
// LastFailure flag so a recovered peer is immediately usable on the next
// outbound request without waiting out peerFailureBackoff.
func (c *WUSPController) markPeerInbound(peerPublicKey string) {
	c.sessionsMu.Lock()
	defer c.sessionsMu.Unlock()
	s := c.sessions[peerPublicKey]
	if s == nil {
		s = &peerSession{}
		c.sessions[peerPublicKey] = s
	}
	s.LastInbound = time.Now()
	s.LastFailure = time.Time{} // recovery: clear any prior failure flag
}

// markPeerFailure records that an outbound request to peerPublicKey just
// failed at the WG layer or timed out waiting for a response. Subsequent
// outbound requests fast-fail until peerFailureBackoff elapses or the next
// inbound packet arrives (whichever comes first).
func (c *WUSPController) markPeerFailure(peerPublicKey string) {
	c.sessionsMu.Lock()
	defer c.sessionsMu.Unlock()
	s := c.sessions[peerPublicKey]
	if s == nil {
		s = &peerSession{}
		c.sessions[peerPublicKey] = s
	}
	s.LastFailure = time.Now()
}

// peerReachable reports whether peerPublicKey is believed to be online.
//
// Policy: optimistic on fresh start, pessimistic after observed failure or
// silence.
//   - recent failure (LastFailure within peerFailureBackoff) → false
//   - never-seen peer (no session row) → true (best-effort; controller may
//     have just restarted, or it's a freshly-discovered peer)
//   - seen-then-stale peer (LastInbound > peerOnlineWindow) → false
//   - seen-and-fresh → true
//
// This avoids 15-second hangs after a peer goes offline (the first timeout
// records LastFailure; subsequent clicks fast-fail) while not blocking the
// very first request to a freshly-discovered peer.
func (c *WUSPController) peerReachable(peerPublicKey string) bool {
	c.sessionsMu.RLock()
	defer c.sessionsMu.RUnlock()
	s := c.sessions[peerPublicKey]
	if s == nil {
		return true // never seen → optimistic
	}
	if !s.LastFailure.IsZero() && time.Since(s.LastFailure) < peerFailureBackoff {
		return false // recent failure → fast-fail next requests
	}
	if s.LastInbound.IsZero() {
		// We have a session row from a previous failure but never received
		// any inbound. Stay optimistic so the periodic re-announce path
		// can still succeed once the failure backoff elapses.
		return true
	}
	return time.Since(s.LastInbound) < peerOnlineWindow
}

// IsPeerReachable is the public form of peerReachable, used by gRPC handlers
// to decide whether to surface a fast "peer offline" error to the dashboard.
func (c *WUSPController) IsPeerReachable(peerPublicKey string) bool {
	return c.peerReachable(peerPublicKey)
}

// Start launches the background fragment-cleanup goroutine. Call Stop() when
// the server is shutting down.
func (c *WUSPController) Start() {
	go c.sweepLoop()
}

// Stop signals the background sweep goroutine to exit.
func (c *WUSPController) Stop() {
	select {
	case c.stopSweep <- struct{}{}:
	default:
	}
}

// StateRepo returns the underlying device state repository.
// Used by the gRPC service for direct persistence queries.
func (c *WUSPController) StateRepo() store.WUSPDeviceStateRepository {
	return c.opts.StateRepo
}

// StatsSnapshot returns an in-memory snapshot of controller counters and
// aggregate timings. It is safe for concurrent use.
func (c *WUSPController) StatsSnapshot() WUSPControllerStats {
	if c == nil {
		return WUSPControllerStats{}
	}
	return WUSPControllerStats{
		InboundDatagrams:         c.stats.inboundDatagrams.Load(),
		InboundBytes:             c.stats.inboundBytes.Load(),
		InboundResponses:         c.stats.inboundResponses.Load(),
		InboundRequests:          c.stats.inboundRequests.Load(),
		InboundControlFragments:  c.stats.inboundControlFragments.Load(),
		InboundTransferFrames:    c.stats.inboundTransferFrames.Load(),
		ResponsesAfterTimeout:    c.stats.responsesAfterTimeout.Load(),
		FragmentReassemblies:     c.stats.fragmentReassemblies.Load(),
		FragmentReassemblyErrors: c.stats.fragmentReassemblyErrors.Load(),
		FragmentEvictions:        c.stats.fragmentEvictions.Load(),
		OutboundRequests:         c.stats.outboundRequests.Load(),
		OutboundRequestFragments: c.stats.outboundRequestFragments.Load(),
		OutboundBytes:            c.stats.outboundBytes.Load(),
		OutboundSendErrors:       c.stats.outboundSendErrors.Load(),
		PeerFastFails:            c.stats.peerFastFails.Load(),
		RoundTrips:               c.stats.roundTrips.Load(),
		RoundTripRetries:         c.stats.roundTripRetries.Load(),
		RoundTripTimeouts:        c.stats.roundTripTimeouts.Load(),
		RoundTripSuccesses:       c.stats.roundTripSuccesses.Load(),
		RoundTripFailures:        c.stats.roundTripFailures.Load(),
		RoundTripLatencyTotal:    time.Duration(c.stats.roundTripLatencyNS.Load()),
		RoundTripLatencyMax:      time.Duration(c.stats.roundTripLatencyMaxNS.Load()),
		BudgetReductions:         c.stats.budgetReductions.Load(),
		BudgetIncreaseEvents:     c.stats.budgetIncreaseEvents.Load(),
		TransferFramesSent:         c.stats.transferFramesSent.Load(),
		TransferFrameBytesSent:     c.stats.transferFrameBytesSent.Load(),
		TransferFramesReceived:     c.stats.transferFramesReceived.Load(),
		TransferFrameBytesReceived: c.stats.transferFrameBytesReceived.Load(),
		TransferSessionsStarted:    c.stats.transferSessionsStarted.Load(),
		TransferSessionsCompleted:  c.stats.transferSessionsCompleted.Load(),
		TransferSessionsAborted:    c.stats.transferSessionsAborted.Load(),
		TransferAckTimeouts:        c.stats.transferAckTimeouts.Load(),
		TransferChunkResends:       c.stats.transferChunkResends.Load(),
		TransferUnknownSessions:    c.stats.transferUnknownSessions.Load(),
		TransferPayloadBytes:       c.stats.transferPayloadBytes.Load(),
		TransferDurationTotal:      time.Duration(c.stats.transferDurationNS.Load()),
		TransferDurationMax:        time.Duration(c.stats.transferDurationMaxNS.Load()),
	}
}

// PeerSessionSnapshot returns the current peer-specific transport state.
func (c *WUSPController) PeerSessionSnapshot(peerPublicKey string) WUSPPeerSessionSnapshot {
	out := WUSPPeerSessionSnapshot{
		PeerPublicKey: peerPublicKey,
	}
	if c == nil {
		return out
	}
	out.Reachable = c.peerReachable(peerPublicKey)
	c.sessionsMu.RLock()
	defer c.sessionsMu.RUnlock()
	s := c.sessions[peerPublicKey]
	if s == nil {
		return out
	}
	out.LastInbound = s.LastInbound
	out.LastFailure = s.LastFailure
	out.ControlPayloadBudget = s.ControlPayloadBudget
	out.AdvertisedMaxPayload = s.AdvertisedMaxPayload
	out.ControlSuccesses = s.ControlSuccesses
	return out
}

func (c *WUSPController) recordRoundTripLatency(duration time.Duration) {
	if c == nil || duration <= 0 {
		return
	}
	value := uint64(duration)
	c.stats.roundTripLatencyNS.Add(value)
	observeUint64Max(&c.stats.roundTripLatencyMaxNS, value)
}

func (c *WUSPController) recordTransferDuration(duration time.Duration) {
	if c == nil || duration <= 0 {
		return
	}
	value := uint64(duration)
	c.stats.transferDurationNS.Add(value)
	observeUint64Max(&c.stats.transferDurationMaxNS, value)
}

func (c *WUSPController) recordTransferSessionStart() {
	if c == nil {
		return
	}
	c.stats.transferSessionsStarted.Add(1)
}

func (c *WUSPController) recordTransferSessionComplete(session *uspTransferSession) {
	if c == nil || session == nil {
		return
	}
	c.stats.transferSessionsCompleted.Add(1)
	c.stats.transferPayloadBytes.Add(uint64(maxInt64(session.transferred, 0)))
	c.recordTransferDuration(time.Since(session.startedAt))
	c.log.Debug().
		Str("peer", session.peerPublicKey).
		Uint64("session_id", session.id).
		Str("method", session.method.String()).
		Int64("bytes", session.transferred).
		Dur("duration", time.Since(session.startedAt)).
		Msg("wusp: transfer session completed")
}

func (c *WUSPController) recordTransferSessionAbort(session *uspTransferSession, err error) {
	if c == nil || session == nil {
		return
	}
	c.stats.transferSessionsAborted.Add(1)
	c.recordTransferDuration(time.Since(session.startedAt))
	event := c.log.Warn().
		Str("peer", session.peerPublicKey).
		Uint64("session_id", session.id).
		Str("method", session.method.String()).
		Int64("bytes", session.transferred).
		Dur("duration", time.Since(session.startedAt))
	if err != nil {
		event = event.Err(err)
	}
	event.Msg("wusp: transfer session aborted")
}

func observeUint64Max(target *atomic.Uint64, value uint64) {
	if target == nil {
		return
	}
	for {
		current := target.Load()
		if value <= current {
			return
		}
		if target.CompareAndSwap(current, value) {
			return
		}
	}
}

// =============================================================================
// Background fragment cleanup
// =============================================================================

// sweepLoop runs every fragmentSweepInterval and evicts stale fragment buffers.
func (c *WUSPController) sweepLoop() {
	ticker := time.NewTicker(fragmentSweepInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			c.sweepStaleFragments()
		case <-c.stopSweep:
			return
		}
	}
}

// sweepStaleFragments evicts fragment buffers older than fragmentTTL.
func (c *WUSPController) sweepStaleFragments() {
	cutoff := time.Now().Add(-fragmentTTL)
	c.fragmentsMu.Lock()
	defer c.fragmentsMu.Unlock()

	for peer, byMsgID := range c.fragments {
		for msgID, buf := range byMsgID {
			if buf.arrivedAt.Before(cutoff) {
				c.stats.fragmentEvictions.Add(1)
				c.log.Debug().
					Str("peer", peer).
					Uint64("msg_id", msgID).
					Int("frags", len(buf.frags)).
					Msg("wusp: evicting stale fragment buffer")
				delete(byMsgID, msgID)
			}
		}
		if len(byMsgID) == 0 {
			delete(c.fragments, peer)
		}
	}
}

// =============================================================================
// Inbound dispatch
// =============================================================================

// HandleInbound is the transport hook for all inbound MessageType-8 payloads.
// Register via TenantDevice.SetWUSPInboundHandler(ctrl.HandleInbound).
//
// HandleInbound is safe for concurrent use.
func (c *WUSPController) HandleInbound(_ context.Context, peerPublicKey string, data []byte) {
	c.markPeerInbound(peerPublicKey)
	c.stats.inboundDatagrams.Add(1)
	c.stats.inboundBytes.Add(uint64(len(data)))
	c.log.Debug().Str("peer", peerPublicKey).Int("bytes", len(data)).Msg("wusp: inbound WUSP message")

	if len(data) > 0 && data[0] == 0x53 {
		frame, err := wusp.DecodeUSPTransferStreamFrame(data)
		if err != nil {
			c.log.Warn().Err(err).Str("peer", peerPublicKey).Int("bytes", len(data)).Msg("wusp: malformed transfer stream frame")
			return
		}
		c.stats.inboundTransferFrames.Add(1)
		c.stats.transferFramesReceived.Add(1)
		c.stats.transferFrameBytesReceived.Add(uint64(len(data)))
		c.handleTransferStreamFrame(peerPublicKey, frame)
		return
	}

	// Step 1 — fragment detection and reassembly.
	frag, isFragment, err := wusp.DecodeUSPControlFragment(data)
	if err != nil {
		c.stats.fragmentReassemblyErrors.Add(1)
		c.log.Warn().Err(err).Str("peer", peerPublicKey).Int("bytes", len(data)).Msg("wusp: malformed WUSP control fragment")
		return
	}
	if isFragment {
		c.stats.inboundControlFragments.Add(1)
		reassembled, done, err := c.handleFragment(peerPublicKey, frag)
		if err != nil {
			c.stats.fragmentReassemblyErrors.Add(1)
			c.log.Warn().Err(err).Str("peer", peerPublicKey).Msg("wusp: fragment reassembly error")
			return
		}
		if !done {
			return // waiting for more fragments
		}
		data = reassembled
	}

	// Step 2 — decode and dispatch.
	c.dispatchInbound(peerPublicKey, data)
}

// dispatchInbound routes a fully-reassembled inbound payload.
//
// Priority order per the WUSP controller receive spec:
//  1. Try USPAgentResponse → unblock waiting roundTrip caller
//  2. Try USPAgentRequest → route events or subscription acks
func (c *WUSPController) dispatchInbound(peerPublicKey string, data []byte) {
	// ── Try response (most common in controller) ──────────────────────────
	// Note: failed-response-decode is the NORMAL fallback path when the agent
	// sent a Request (e.g. OnBoardRequest, ValueChange Notify). It's not an
	// error, so we don't log it — the caller will succeed on DecodeUSPAgentRequest.
	if resp, err := wusp.DecodeUSPAgentResponse(data); err != nil {
		_ = err // Silently fall through to request decode below.
	} else {
		c.stats.inboundResponses.Add(1)
		c.pendingMu.RLock()
		p, hasPending := c.pending[resp.ID]
		c.pendingMu.RUnlock()
		event := c.log.Debug().
			Str("peer", peerPublicKey).
			Uint64("id", resp.ID).
			Str("method", resp.Method.String())
		if hasPending && !p.startedAt.IsZero() {
			event = event.Dur("latency", time.Since(p.startedAt))
		}
		event.Msg("wusp: decoded inbound response")

		if hasPending {
			select {
			case p.ch <- resp:
			default:
				// Caller already timed out — fall through for Notify handling.
				c.stats.responsesAfterTimeout.Add(1)
				event := c.log.Warn().Str("peer", peerPublicKey).Uint64("id", resp.ID)
				if !p.startedAt.IsZero() {
					event = event.Dur("age", time.Since(p.startedAt))
				}
				event.Msg("wusp: response arrived after caller timed out")
			}
			if resp.Method != wusp.USPAgentMethodNotify {
				return
			}
		}

		// Unsolicited Notify response (no pending caller or caller timed out).
		if resp.Method == wusp.USPAgentMethodNotify && c.opts.OnNotify != nil {
			go c.opts.OnNotify(peerPublicKey, resp)
		}
		return
	}

	// ── Try request (agent-to-controller event or subscription ack) ───────
	req, err := wusp.DecodeUSPAgentRequest(data)
	if err != nil {
		c.log.Warn().
			Err(err).
			Str("peer", peerPublicKey).
			Int("bytes", len(data)).
			Msg("wusp: failed to decode inbound message as response or request")
		return
	}
	c.stats.inboundRequests.Add(1)

	if wusp.IsEventNotifyRequest(req) {
		event, err := wusp.DecodeEventFromRequest(req)
		if err != nil {
			c.log.Debug().Err(err).Str("peer", peerPublicKey).Msg("wusp: failed to decode event from request")
			return
		}
		if c.opts.OnEvent != nil {
			go c.opts.OnEvent(peerPublicKey, event)
		}
		return
	}

	if wusp.IsSubscriptionRequest(req) {
		// Controller acting as subscription target — acknowledge silently.
		// Real subscription state is managed server-side; no persistent storage needed.
		c.log.Debug().Str("peer", peerPublicKey).Msg("wusp: received subscription management request")
		return
	}

	c.log.Debug().
		Str("peer", peerPublicKey).
		Str("method", req.Method.String()).
		Msg("wusp: unhandled inbound request")
}

// handleFragment buffers a fragment for peerPublicKey and returns
// (payload, true, nil) when the full message is complete.
//
// Security: each peer is limited to maxFragmentBufs concurrent incomplete
// messages. When the limit is exceeded, the oldest buffer is dropped.
func (c *WUSPController) handleFragment(peerPublicKey string, frag wusp.USPControlFragment) ([]byte, bool, error) {
	c.fragmentsMu.Lock()
	defer c.fragmentsMu.Unlock()

	byMsgID, ok := c.fragments[peerPublicKey]
	if !ok {
		byMsgID = make(map[uint64]*fragmentBuf)
		c.fragments[peerPublicKey] = byMsgID
	}

	buf, ok := byMsgID[frag.MessageID]
	if !ok {
		// Enforce per-peer buffer cap — evict the oldest entry when full.
		if len(byMsgID) >= maxFragmentBufs {
			c.evictOldestFragment(byMsgID, peerPublicKey)
		}
		buf = &fragmentBuf{arrivedAt: time.Now()}
		byMsgID[frag.MessageID] = buf
	}
	buf.frags = append(buf.frags, frag)

	if uint32(len(buf.frags)) < frag.Count {
		return nil, false, nil // still incomplete
	}

	// All fragments received — reassemble and release the buffer.
	payload, err := wusp.ReassembleUSPControlFragments(buf.frags)
	delete(byMsgID, frag.MessageID)
	if err != nil {
		return nil, false, err
	}
	c.stats.fragmentReassemblies.Add(1)
	return payload, true, nil
}

// evictOldestFragment removes the oldest fragment buffer from byMsgID to make
// room for a new message. Caller must hold fragmentsMu.
func (c *WUSPController) evictOldestFragment(byMsgID map[uint64]*fragmentBuf, peer string) {
	var oldestID uint64
	var oldestTime time.Time
	first := true
	for msgID, buf := range byMsgID {
		if first || buf.arrivedAt.Before(oldestTime) {
			oldestID = msgID
			oldestTime = buf.arrivedAt
			first = false
		}
	}
	c.log.Warn().
		Str("peer", peer).
		Uint64("msg_id", oldestID).
		Msg("wusp: fragment buffer cap reached, evicting oldest entry")
	c.stats.fragmentEvictions.Add(1)
	delete(byMsgID, oldestID)
}

// =============================================================================
// Outbound helpers
// =============================================================================

// nextID returns a unique, non-zero request ID.
func (c *WUSPController) nextID() uint64 {
	return c.reqCounter.Add(1)
}

// send encodes req and delivers it (with fragmentation if needed).
//
// Fails fast with ErrPeerNotReachable when the peer hasn't been heard from
// within peerOnlineWindow. This avoids the 15-second timeout dance when a
// peer is offline — the dashboard gets an immediate, predictable error.
func (c *WUSPController) send(peerPublicKey string, req wusp.USPAgentRequest) error {
	if c.opts.Send == nil {
		return fmt.Errorf("wuspcontroller: Send function not configured")
	}

	if !c.peerReachable(peerPublicKey) {
		c.stats.peerFastFails.Add(1)
		return ErrPeerNotReachable
	}

	req.Metadata = wusp.WithResponseMaxControlPayload(req.Metadata, c.peerControlPayloadBudget(peerPublicKey))
	encoded, err := wusp.EncodeUSPAgentRequest(req)
	if err != nil {
		return fmt.Errorf("wuspcontroller: encode: %w", err)
	}

	fragments, err := wusp.FragmentUSPControlPayload(encoded, req.ID, c.peerControlPayloadBudget(peerPublicKey))
	if err != nil {
		return fmt.Errorf("wuspcontroller: fragment: %w", err)
	}
	var outboundBytes uint64
	for _, frag := range fragments {
		outboundBytes += uint64(len(frag))
	}
	c.stats.outboundRequests.Add(1)
	c.stats.outboundRequestFragments.Add(uint64(len(fragments)))
	c.stats.outboundBytes.Add(outboundBytes)

	c.log.Debug().Str("peer", peerPublicKey).Str("method", req.Method.String()).Uint64("id", req.ID).Int("frags", len(fragments)).Msg("wusp: sending request to peer")
	for _, frag := range fragments {
		if err := c.opts.Send(peerPublicKey, frag); err != nil {
			c.stats.outboundSendErrors.Add(1)
			c.markPeerFailure(peerPublicKey)
			return fmt.Errorf("wuspcontroller: send frag: %w", err)
		}
	}
	c.log.Debug().Str("peer", peerPublicKey).Uint64("id", req.ID).Msg("wusp: request fragments delivered to WireGuard layer")
	return nil
}

// roundTripOnce sends req and blocks until a matching response arrives or the
// context deadline expires.
func (c *WUSPController) roundTripOnce(ctx context.Context, peerPublicKey string, req wusp.USPAgentRequest) (wusp.USPAgentResponse, error) {
	ch := make(chan wusp.USPAgentResponse, 1)
	startedAt := time.Now()

	c.pendingMu.Lock()
	c.pending[req.ID] = &pendingRequest{ch: ch, method: req.Method, startedAt: startedAt}
	c.pendingMu.Unlock()

	defer func() {
		c.pendingMu.Lock()
		delete(c.pending, req.ID)
		c.pendingMu.Unlock()
	}()

	if err := c.send(peerPublicKey, req); err != nil {
		return wusp.USPAgentResponse{}, err
	}

	select {
	case resp := <-ch:
		return resp, nil
	case <-ctx.Done():
		return wusp.USPAgentResponse{}, fmt.Errorf("wuspcontroller: request %d (%v): %w",
			req.ID, req.Method, ctx.Err())
	}
}

// roundTrip sends req and blocks until a matching response arrives or the
// context deadline expires. Safe read-only methods get one retry with a
// reduced control payload budget to recover from fragment loss or path-size
// issues without replaying mutating operations.
func (c *WUSPController) roundTrip(ctx context.Context, peerPublicKey string, req wusp.USPAgentRequest) (wusp.USPAgentResponse, error) {
	c.stats.roundTrips.Add(1)
	startedAt := time.Now()
	attempts := controlRetryAttempts(req.Method)
	for attempt := 0; attempt < attempts; attempt++ {
		attemptReq := req
		if attempt > 0 {
			attemptReq.ID = c.nextID()
			c.stats.roundTripRetries.Add(1)
		}
		attemptCtx, cancel := context.WithTimeout(ctx, c.opts.RequestTimeout)
		resp, err := c.roundTripOnce(attemptCtx, peerPublicKey, attemptReq)
		cancel()
		if err == nil {
			c.recordControlSuccess(peerPublicKey, resp.Protocol)
			c.stats.roundTripSuccesses.Add(1)
			c.recordRoundTripLatency(time.Since(startedAt))
			c.log.Debug().
				Str("peer", peerPublicKey).
				Str("method", req.Method.String()).
				Int("attempts", attempt+1).
				Int("budget", c.peerControlPayloadBudget(peerPublicKey)).
				Dur("duration", time.Since(startedAt)).
				Msg("wusp: round trip completed")
			if resp.Error != "" {
				return resp, &AgentError{Method: attemptReq.Method, Message: resp.Error}
			}
			return resp, nil
		}
		if errors.Is(err, context.DeadlineExceeded) {
			c.stats.roundTripTimeouts.Add(1)
			c.reducePeerControlPayload(peerPublicKey)
			if attempt+1 < attempts && ctx.Err() == nil {
				c.log.Debug().
					Str("peer", peerPublicKey).
					Str("method", req.Method.String()).
					Int("attempt", attempt+2).
					Int("budget", c.peerControlPayloadBudget(peerPublicKey)).
					Msg("wusp: retrying idempotent request with reduced payload budget")
				continue
			}
			c.markPeerFailure(peerPublicKey)
		}
		c.stats.roundTripFailures.Add(1)
		c.log.Warn().
			Err(err).
			Str("peer", peerPublicKey).
			Str("method", req.Method.String()).
			Int("attempts", attempt+1).
			Dur("duration", time.Since(startedAt)).
			Msg("wusp: round trip failed")
		return wusp.USPAgentResponse{}, err
	}
	c.markPeerFailure(peerPublicKey)
	c.stats.roundTripFailures.Add(1)
	return wusp.USPAgentResponse{}, fmt.Errorf("wuspcontroller: request %d (%v): retries exhausted", req.ID, req.Method)
}

// =============================================================================
// Param validation helpers
// =============================================================================

// IsWritable reports whether the given parameter path permits controller writes.
// Unknown paths (not in the bundled device model) are treated as writable to
// allow controller-defined extension parameters through.
func (c *WUSPController) IsWritable(path string) bool {
	if c.model == nil {
		return true
	}
	param, ok := c.model.GetParam(path)
	if !ok {
		return true // unknown path — pass through
	}
	return param.Access != wusp.ReadOnly
}

// IsReadOnly reports whether the parameter is declared read-only in the TR-181
// bundled schema. Returns false for unknown paths.
func (c *WUSPController) IsReadOnly(path string) bool {
	if c.model == nil {
		return false
	}
	param, ok := c.model.GetParam(path)
	if !ok {
		return false
	}
	return param.Access == wusp.ReadOnly
}

// ParamInfo returns the schema Param for a path, if known.
func (c *WUSPController) ParamInfo(path string) (wusp.Param, bool) {
	if c.model == nil {
		return wusp.Param{}, false
	}
	return c.model.GetParam(path)
}

// sanitizeSetMessage filters read-only and unknown-empty paths out of msg in
// place. Returns the count of fields dropped (logged as a warning by the
// caller) and an error iff the resulting message has zero writable fields.
//
// We drop read-only fields silently rather than rejecting the whole Set
// because dashboards often round-trip the entire snapshot (including RO
// counters like UpTime). Failing the whole request would block any UX that
// edits one writable field while passing the rest unchanged.
//
// Device.GetParam already resolves concrete instance paths
// (Device.WiFi.SSID.1.Enable) to the schema form (Device.WiFi.SSID.{i}.Enable)
// internally, so callers don't need to canonicalize ahead of time.
func (c *WUSPController) sanitizeSetMessage(msg *wusp.Message) (dropped int, err error) {
	if msg == nil {
		return 0, &wusp.ValidationError{Reason: "Set requires a non-nil Message"}
	}
	if len(msg.Fields) == 0 {
		return 0, &wusp.ValidationError{Reason: "Set requires at least one field"}
	}
	if c.model == nil {
		return 0, nil
	}
	kept := msg.Fields[:0]
	for _, f := range msg.Fields {
		path := strings.TrimSpace(f.Path)
		if path == "" {
			dropped++
			c.log.Warn().Msg("wusp: dropping field with empty path from Set")
			continue
		}
		if param, ok := c.model.GetParam(path); ok && param.Access == wusp.ReadOnly {
			dropped++
			c.log.Warn().Str("path", path).Str("type", string(param.Type)).Msg("wusp: dropping read-only field from Set")
			continue
		}
		kept = append(kept, f)
	}
	msg.Fields = kept
	if len(msg.Fields) == 0 {
		return dropped, &wusp.ValidationError{Reason: fmt.Sprintf("Set rejected: all %d field(s) were read-only or empty", dropped)}
	}
	return dropped, nil
}

// =============================================================================
// USP Methods — safe wrappers
// =============================================================================

// runRequest is the single round-trip entry point shared by every public USP
// method wrapper. It applies the controller's RequestTimeout to ctx, assigns a
// fresh request ID if the caller didn't, and dispatches via roundTrip — which
// already centralises the peerReachable / encode / fragment / send / mark-failure
// resilience path. Method-specific guards (path-format, non-empty message, etc.)
// stay in their wrapper functions.
func (c *WUSPController) runRequest(ctx context.Context, peerPublicKey string, req wusp.USPAgentRequest) (wusp.USPAgentResponse, error) {
	timeout := c.opts.RequestTimeout
	if attempts := controlRetryAttempts(req.Method); attempts > 1 {
		timeout *= time.Duration(attempts)
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	if req.ID == 0 {
		req.ID = c.nextID()
	}
	return c.roundTrip(ctx, peerPublicKey, req)
}

// Get retrieves parameter values for the given string paths.
// At least one path is required.
func (c *WUSPController) Get(ctx context.Context, peerPublicKey string, paths ...string) (wusp.USPAgentResponse, error) {
	if len(paths) == 0 {
		return wusp.USPAgentResponse{}, &wusp.ValidationError{Reason: "Get requires at least one path"}
	}
	return c.runRequest(ctx, peerPublicKey, wusp.USPAgentRequest{
		Method: wusp.USPAgentMethodGet,
		Paths:  paths,
	})
}

// GetAll retrieves the full device snapshot (no path filter).
func (c *WUSPController) GetAll(ctx context.Context, peerPublicKey string) (wusp.USPAgentResponse, error) {
	return c.runRequest(ctx, peerPublicKey, wusp.USPAgentRequest{
		Method: wusp.USPAgentMethodGet,
		Paths:  []string{},
	})
}

// GetCoded retrieves parameter values using stable uint64 path codes.
// At least one code is required.
func (c *WUSPController) GetCoded(ctx context.Context, peerPublicKey string, codes ...uint64) (wusp.USPAgentResponse, error) {
	if len(codes) == 0 {
		return wusp.USPAgentResponse{}, &wusp.ValidationError{Reason: "GetCoded requires at least one code"}
	}
	return c.runRequest(ctx, peerPublicKey, wusp.USPAgentRequest{
		Method:    wusp.USPAgentMethodGet,
		PathCodes: codes,
	})
}

// Set updates one or more parameters on the agent.
// msg must contain at least one field. No access-mode validation is performed;
// use SetValidated for schema-enforced writes.
func (c *WUSPController) Set(ctx context.Context, peerPublicKey string, msg *wusp.Message) (wusp.USPAgentResponse, error) {
	if msg == nil || len(msg.Fields) == 0 {
		return wusp.USPAgentResponse{}, &wusp.ValidationError{Reason: "Set requires at least one field"}
	}
	return c.runRequest(ctx, peerPublicKey, wusp.USPAgentRequest{
		Method:  wusp.USPAgentMethodSet,
		Message: msg,
	})
}

// SetValidated is like Set but FILTERS read-only parameters out of the message
// before encoding instead of rejecting the whole request. Returns
// *wusp.ValidationError only when there is nothing writable left to send.
//
// Rationale: dashboards routinely round-trip the entire snapshot (including
// counters and UpTime). Hard-failing on the first read-only field would make
// every "Save" click fail. Dropping read-only fields silently (with a WARN
// log per path) lets the writable fields through cleanly.
func (c *WUSPController) SetValidated(ctx context.Context, peerPublicKey string, msg *wusp.Message) (wusp.USPAgentResponse, error) {
	if _, err := c.sanitizeSetMessage(msg); err != nil {
		return wusp.USPAgentResponse{}, err
	}
	return c.Set(ctx, peerPublicKey, msg)
}

// Add creates a new object instance at objectPath (must end with '.').
func (c *WUSPController) Add(ctx context.Context, peerPublicKey, objectPath string) (wusp.USPAgentResponse, error) {
	if !strings.HasSuffix(strings.TrimSpace(objectPath), ".") {
		return wusp.USPAgentResponse{}, &wusp.ValidationError{
			Path:   objectPath,
			Reason: "Add: objectPath must end with '.'",
		}
	}
	return c.runRequest(ctx, peerPublicKey, wusp.USPAgentRequest{
		Method:     wusp.USPAgentMethodAdd,
		ObjectPath: objectPath,
	})
}

// Delete removes the objects or parameters at paths.
// At least one path is required.
func (c *WUSPController) Delete(ctx context.Context, peerPublicKey string, paths ...string) (wusp.USPAgentResponse, error) {
	if len(paths) == 0 {
		return wusp.USPAgentResponse{}, &wusp.ValidationError{Reason: "Delete requires at least one path"}
	}
	return c.runRequest(ctx, peerPublicKey, wusp.USPAgentRequest{
		Method: wusp.USPAgentMethodDelete,
		Paths:  paths,
	})
}

// GetInstances returns the instantiated rows for the given object table paths.
// Paths must end with '.'.
func (c *WUSPController) GetInstances(ctx context.Context, peerPublicKey string, objectPaths ...string) (wusp.USPAgentResponse, error) {
	if len(objectPaths) == 0 {
		return wusp.USPAgentResponse{}, &wusp.ValidationError{Reason: "GetInstances requires at least one object path"}
	}
	for _, p := range objectPaths {
		if !strings.HasSuffix(strings.TrimSpace(p), ".") {
			return wusp.USPAgentResponse{}, &wusp.ValidationError{
				Path:   p,
				Reason: "GetInstances: object paths must end with '.'",
			}
		}
	}
	return c.runRequest(ctx, peerPublicKey, wusp.USPAgentRequest{
		Method: wusp.USPAgentMethodGetInstances,
		Paths:  objectPaths,
	})
}

// Operate executes the TR-181 command at cmdPath. Per the BBF USP spec,
// command paths end with "()" — e.g. "Device.Reboot()", "Device.FactoryReset()",
// "Device.WiFi.NeighboringWiFiDiagnostic()". Object paths (ending with ".")
// are also accepted for backward compatibility with earlier callers.
func (c *WUSPController) Operate(ctx context.Context, peerPublicKey, cmdPath string, input *wusp.Message, meta map[string]string) (wusp.USPAgentResponse, error) {
	trimmed := strings.TrimSpace(cmdPath)
	if trimmed == "" {
		return wusp.USPAgentResponse{}, &wusp.ValidationError{
			Reason: "Operate: cmdPath required",
		}
	}
	if !strings.HasSuffix(trimmed, "()") && !strings.HasSuffix(trimmed, ".") {
		return wusp.USPAgentResponse{}, &wusp.ValidationError{
			Path:   cmdPath,
			Reason: "Operate: cmdPath must end with '()' (TR-181 command) or '.' (object path)",
		}
	}
	return c.runRequest(ctx, peerPublicKey, wusp.USPAgentRequest{
		Method:     wusp.USPAgentMethodOperate,
		ObjectPath: trimmed,
		Message:    input,
		Metadata:   meta,
	})
}

// Notify sends a controller-originated event notification to the agent at
// eventPath (must end with '.'). For wiring agent subscriptions, prefer
// Subscribe/Unsubscribe.
func (c *WUSPController) Notify(ctx context.Context, peerPublicKey, eventPath string, payload *wusp.Message, meta map[string]string) (wusp.USPAgentResponse, error) {
	if !strings.HasSuffix(strings.TrimSpace(eventPath), ".") {
		return wusp.USPAgentResponse{}, &wusp.ValidationError{
			Path:   eventPath,
			Reason: "Notify: eventPath must end with '.'",
		}
	}
	return c.runRequest(ctx, peerPublicKey, wusp.USPAgentRequest{
		Method:     wusp.USPAgentMethodNotify,
		ObjectPath: eventPath,
		Message:    payload,
		Metadata:   meta,
	})
}

// GetSupportedDM returns the agent's supported data model tree.
// pathFilters narrows the response; pass none for the full tree.
func (c *WUSPController) GetSupportedDM(ctx context.Context, peerPublicKey string, pathFilters ...string) (wusp.USPAgentResponse, error) {
	return c.runRequest(ctx, peerPublicKey, wusp.USPAgentRequest{
		Method: wusp.USPAgentMethodGetSupportedDM,
		Paths:  pathFilters,
	})
}

// GetSupportedProtocol returns the agent's WUSP transport capabilities.
// Controllers should call this first and adapt to the reported limits.
func (c *WUSPController) GetSupportedProtocol(ctx context.Context, peerPublicKey string) (wusp.USPAgentResponse, error) {
	return c.runRequest(ctx, peerPublicKey, wusp.USPAgentRequest{
		Method: wusp.USPAgentMethodGetSupportedProtocol,
	})
}

// Upload initiates a bulk upload to the agent.
// transfer.Transfer must be non-nil.
func (c *WUSPController) Upload(ctx context.Context, peerPublicKey string, transfer *wusp.USPTransferRequest) (wusp.USPAgentResponse, error) {
	if transfer == nil {
		return wusp.USPAgentResponse{}, &wusp.ValidationError{Reason: "Upload requires a Transfer block"}
	}
	resp, err := c.runRequest(ctx, peerPublicKey, wusp.USPAgentRequest{
		Method:   wusp.USPAgentMethodUpload,
		Transfer: transfer,
	})
	if err != nil {
		return wusp.USPAgentResponse{}, err
	}
	return c.uploadOverStream(ctx, peerPublicKey, transfer, resp)
}

// Download initiates a bulk download from the agent.
// transfer must be non-nil.
func (c *WUSPController) Download(ctx context.Context, peerPublicKey string, transfer *wusp.USPTransferRequest) (wusp.USPAgentResponse, error) {
	if transfer == nil {
		return wusp.USPAgentResponse{}, &wusp.ValidationError{Reason: "Download requires a Transfer block"}
	}
	resp, err := c.runRequest(ctx, peerPublicKey, wusp.USPAgentRequest{
		Method:   wusp.USPAgentMethodDownload,
		Transfer: transfer,
	})
	if err != nil {
		return wusp.USPAgentResponse{}, err
	}
	return c.downloadOverStream(ctx, peerPublicKey, transfer, resp)
}

// =============================================================================
// Subscription wire management
// =============================================================================

// Subscribe registers a subscription on the agent. The agent will push
// matching events as inbound USPAgentRequest (Notify) messages, which the
// controller decodes and routes to the OnEvent callback.
//
// subID is the controller-assigned subscription identifier (echoed in every
// matching event). types, pathFilter, and eventFilter work as documented in
// wusp.EncodeSubscribeRequest.
func (c *WUSPController) Subscribe(peerPublicKey, subID string, types []wusp.USPEventType, pathFilter, eventFilter []string) error {
	if strings.TrimSpace(subID) == "" {
		return &wusp.ValidationError{Reason: "Subscribe: subID must not be empty"}
	}
	req := wusp.EncodeSubscribeRequest(c.nextID(), subID, types, pathFilter, eventFilter)
	return c.send(peerPublicKey, req)
}

// Unsubscribe cancels a previously registered subscription on the agent.
func (c *WUSPController) Unsubscribe(peerPublicKey, subID string) error {
	if strings.TrimSpace(subID) == "" {
		return &wusp.ValidationError{Reason: "Unsubscribe: subID must not be empty"}
	}
	req := wusp.EncodeUnsubscribeRequest(c.nextID(), subID)
	return c.send(peerPublicKey, req)
}

// dashSubscriptionID is the canonical subID we use for the "dashboard live
// preview" subscription. We keep one subscription per peer regardless of how
// many dashboards (sessions) are watching it — the controller fans out the
// resulting Notify events via Redis pub/sub. Re-issuing Subscribe with the
// same subID on the agent is idempotent: the agent replaces the existing
// registration in place.
const dashSubscriptionID = "wantastic-dashboard"

// EnsureDashboardSubscription registers (or refreshes) the canonical dashboard
// subscription with the agent so it pushes ValueChange / OperationComplete
// Notify events back to the controller. Safe to call repeatedly — the agent
// treats a duplicate subID as an in-place replacement.
//
// Errors are returned but should typically be logged-and-continued by the
// caller: a failed Subscribe doesn't break the controller's request/response
// path, it just means the dashboard won't get live push until the next retry.
func (c *WUSPController) EnsureDashboardSubscription(peerPublicKey string) error {
	return c.Subscribe(
		peerPublicKey,
		dashSubscriptionID,
		[]wusp.USPEventType{
			wusp.USPEventTypeValueChange,
			wusp.USPEventTypeOperationComplete,
			wusp.USPEventTypeObjectCreation,
			wusp.USPEventTypeObjectDeletion,
		},
		nil, // path filter: all paths
		nil, // event-name filter: all
	)
}

// CancelDashboardSubscription removes the canonical dashboard subscription.
// Called when the last session for a peer goes away (debounced caller-side).
func (c *WUSPController) CancelDashboardSubscription(peerPublicKey string) error {
	return c.Unsubscribe(peerPublicKey, dashSubscriptionID)
}

// =============================================================================
// Device state sync
// =============================================================================

// syncDeviceStatePaths are the paths fetched during SyncDeviceState.
// Targeted Get keeps the response small (~1 fragment) instead of fetching
// the full Device.* tree (~250 fragments) which causes UDP fragment loss.
var syncDeviceStatePaths = []string{
	"Device.DeviceInfo.", // identity, memory, uptime, hardware
	"Device.Time.",       // NTP status, timezone
	"Device.IP.",         // IPv4/IPv6 status, interfaces
	"Device.Cellular.",   // LTE/5G modem status, signal, APN/SIM
	"Device.Firewall.",   // firewall enable/type
	"Device.WiFi.",       // radios, SSIDs, access points
	"Device.WUSP.",       // WUSP protocol config
}

// SyncDeviceState fetches key device identity fields and persists the snapshot.
// Uses a targeted Get (not GetAll) to keep the response small and reliable.
func (c *WUSPController) SyncDeviceState(ctx context.Context, peerPublicKey, accountID string) error {
	if !c.hasAdvertisedControlPayload(peerPublicKey) {
		if _, err := c.GetSupportedProtocol(ctx, peerPublicKey); err != nil {
			c.log.Debug().Err(err).Str("peer", peerPublicKey).Msg("wusp: GetSupportedProtocol probe failed before sync")
		}
	}
	resp, err := c.Get(ctx, peerPublicKey, syncDeviceStatePaths...)
	if err != nil {
		// AgentError means the agent responded but some paths were missing —
		// still use whatever fields the response contains.
		var agentErr *AgentError
		if !errors.As(err, &agentErr) {
			return fmt.Errorf("wuspcontroller: SyncDeviceState get: %w", err)
		}
		c.log.Debug().Str("peer", peerPublicKey).Str("agent_error", agentErr.Message).Msg("wusp: SyncDeviceState partial response")
	}
	c.log.Info().Str("peer", peerPublicKey).Bool("has_err", err != nil).Bool("has_msg", resp.Message != nil).Int("fields", func() int {
		if resp.Message != nil {
			return len(resp.Message.Fields)
		}
		return 0
	}()).Msg("wusp: SyncDeviceState response")

	state := &store.WUSPDeviceStateData{
		PeerID:     peerPublicKey,
		AccountID:  accountID,
		LastSyncAt: time.Now(),
		UpdatedAt:  time.Now(),
	}

	if resp.Message != nil {
		for _, f := range resp.Message.Fields {
			switch f.Path {
			case "Device.DeviceInfo.DeviceID":
				state.DeviceID = f.Val.AsString()
			case "Device.DeviceInfo.Manufacturer":
				state.Manufacturer = f.Val.AsString()
			case "Device.DeviceInfo.ProductClass":
				state.ProductClass = f.Val.AsString()
			case "Device.DeviceInfo.SerialNumber":
				state.SerialNumber = f.Val.AsString()
			case "Device.DeviceInfo.SoftwareVersion":
				state.SoftwareVersion = f.Val.AsString()
			case "Device.DeviceInfo.HardwareVersion":
				state.HardwareVersion = f.Val.AsString()
			case "Device.WUSP.Enable":
				state.WUSPEnable = f.Val.AsBool()
			case "Device.WUSP.Status":
				state.WUSPStatus = f.Val.AsString()
			case "Device.WUSP.ProtocolVersion":
				state.WUSPVersion = f.Val.AsString()
			}
		}
		if snapshot, err := marshalFieldSnapshot(resp.Message); err == nil {
			state.DeviceSnapshot = snapshot
		}
	}

	if err := c.opts.StateRepo.Upsert(state); err != nil {
		return fmt.Errorf("wuspcontroller: SyncDeviceState persist: %w", err)
	}

	c.log.Debug().
		Str("peer", peerPublicKey).
		Str("account", accountID).
		Msg("WUSP device state synced")
	return nil
}

func controlRetryAttempts(method wusp.USPAgentMethod) int {
	switch method {
	case wusp.USPAgentMethodGet, wusp.USPAgentMethodGetInstances, wusp.USPAgentMethodGetSupportedDM, wusp.USPAgentMethodGetSupportedProtocol:
		return idempotentControlRetryAttempts
	default:
		return 1
	}
}

func (c *WUSPController) hasAdvertisedControlPayload(peerPublicKey string) bool {
	c.sessionsMu.RLock()
	defer c.sessionsMu.RUnlock()
	s := c.sessions[peerPublicKey]
	return s != nil && s.AdvertisedMaxPayload > 0
}

func (c *WUSPController) peerControlPayloadBudget(peerPublicKey string) int {
	c.sessionsMu.Lock()
	defer c.sessionsMu.Unlock()
	s := c.sessions[peerPublicKey]
	if s == nil {
		s = &peerSession{}
		c.sessions[peerPublicKey] = s
	}
	cap := peerPayloadCap(s)
	if s.ControlPayloadBudget == 0 {
		s.ControlPayloadBudget = cap
	}
	if s.ControlPayloadBudget > cap {
		s.ControlPayloadBudget = cap
	}
	if s.ControlPayloadBudget < minControlPayloadBudget {
		s.ControlPayloadBudget = minControlPayloadBudget
	}
	return s.ControlPayloadBudget
}

func (c *WUSPController) recordControlSuccess(peerPublicKey string, protocol *wusp.USPProtocolInfo) {
	c.sessionsMu.Lock()
	defer c.sessionsMu.Unlock()
	s := c.sessions[peerPublicKey]
	if s == nil {
		s = &peerSession{}
		c.sessions[peerPublicKey] = s
	}
	if protocol != nil {
		s.AdvertisedMaxPayload = normalizeAdvertisedPayload(protocol.MaxControlPayload)
	}
	cap := peerPayloadCap(s)
	previousBudget := s.ControlPayloadBudget
	if s.ControlPayloadBudget == 0 {
		s.ControlPayloadBudget = cap
	}
	if s.ControlPayloadBudget > cap {
		s.ControlPayloadBudget = cap
	}
	if s.ControlPayloadBudget < cap {
		s.ControlSuccesses++
		if s.ControlSuccesses >= controlPayloadIncreaseEvery {
			s.ControlPayloadBudget += controlPayloadIncreaseStep
			if s.ControlPayloadBudget > cap {
				s.ControlPayloadBudget = cap
			}
			s.ControlSuccesses = 0
			if s.ControlPayloadBudget > previousBudget {
				c.stats.budgetIncreaseEvents.Add(1)
				c.log.Debug().
					Str("peer", peerPublicKey).
					Int("budget", s.ControlPayloadBudget).
					Int("cap", cap).
					Msg("wusp: increased peer control payload budget after stable responses")
			}
		}
	} else {
		s.ControlSuccesses = 0
	}
}

func (c *WUSPController) reducePeerControlPayload(peerPublicKey string) int {
	c.sessionsMu.Lock()
	defer c.sessionsMu.Unlock()
	s := c.sessions[peerPublicKey]
	if s == nil {
		s = &peerSession{}
		c.sessions[peerPublicKey] = s
	}
	current := s.ControlPayloadBudget
	if current == 0 {
		current = peerPayloadCap(s)
	}
	next := int(math.Floor(float64(current*3) / 4))
	next = (next / controlPayloadIncreaseStep) * controlPayloadIncreaseStep
	if next >= current {
		next = current - controlPayloadIncreaseStep
	}
	if next < minControlPayloadBudget {
		next = minControlPayloadBudget
	}
	if next < current {
		c.stats.budgetReductions.Add(1)
		c.log.Debug().
			Str("peer", peerPublicKey).
			Int("from", current).
			Int("to", next).
			Msg("wusp: reduced peer control payload budget")
	}
	s.ControlPayloadBudget = next
	s.ControlSuccesses = 0
	return next
}

func normalizeAdvertisedPayload(value uint64) int {
	if value == 0 {
		return 0
	}
	if value > wusp.WUSPMaxDatagramPayload {
		return wusp.WUSPMaxDatagramPayload
	}
	if value < minControlPayloadBudget {
		return minControlPayloadBudget
	}
	return int(value)
}

func peerPayloadCap(session *peerSession) int {
	cap := wusp.WUSPMaxDatagramPayload
	if session != nil && session.AdvertisedMaxPayload > 0 && session.AdvertisedMaxPayload < cap {
		cap = session.AdvertisedMaxPayload
	}
	if cap < minControlPayloadBudget {
		return minControlPayloadBudget
	}
	return cap
}

// ProvisionDevice applies a JSON [{path,value}] snapshot to a live peer.
// Only writable parameters are sent; read-only params are silently skipped.
// Returns the agent response or an error.
func (c *WUSPController) ProvisionDevice(ctx context.Context, peerPublicKey string, snapshotJSON []byte) (wusp.USPAgentResponse, error) {
	type kv struct {
		Path  string `json:"path"`
		Value string `json:"value"`
	}
	var pairs []kv
	if err := json.Unmarshal(snapshotJSON, &pairs); err != nil {
		return wusp.USPAgentResponse{}, fmt.Errorf("wuspcontroller: ProvisionDevice decode snapshot: %w", err)
	}

	msg := wusp.NewMessage()
	for _, p := range pairs {
		if c.IsReadOnly(p.Path) {
			continue
		}
		msg.Set(p.Path, wusp.String(p.Value))
	}

	if len(msg.Fields) == 0 {
		return wusp.USPAgentResponse{}, fmt.Errorf("wuspcontroller: ProvisionDevice: snapshot contains no writable parameters")
	}

	resp, err := c.Set(ctx, peerPublicKey, msg)
	if err != nil {
		return resp, fmt.Errorf("wuspcontroller: ProvisionDevice set: %w", err)
	}
	return resp, nil
}

// =============================================================================
// Error types
// =============================================================================

// AgentError is returned by roundTrip when the agent responds with a non-empty
// Error field. It wraps the agent message string and the originating method.
type AgentError struct {
	Method  wusp.USPAgentMethod
	Message string
}

func (e *AgentError) Error() string {
	return fmt.Sprintf("wusp agent error [%v]: %s", e.Method, e.Message)
}

// IsAgentError reports whether err is an *AgentError.
func IsAgentError(err error) bool {
	var ae *AgentError
	return errorAs(err, &ae)
}

// errorAs is a local helper to avoid importing errors package just for As.
func errorAs(err error, target any) bool {
	if err == nil {
		return false
	}
	type asInterface interface {
		As(interface{}) bool
	}
	if x, ok := err.(asInterface); ok {
		return x.As(target)
	}
	switch t := target.(type) {
	case **AgentError:
		if ae, ok := err.(*AgentError); ok {
			*t = ae
			return true
		}
	}
	return false
}

// =============================================================================
// Internal helpers
// =============================================================================

// marshalFieldSnapshot serialises a wusp.Message as a flat JSON array of
// {path, value} pairs for storage in the device_snapshot JSONB column.
// Values are rendered as human-readable strings regardless of their type tag.
func marshalFieldSnapshot(msg *wusp.Message) ([]byte, error) {
	type kv struct {
		Path   string `json:"path"`
		Value  string `json:"value"`
		Access string `json:"access,omitempty"` // "readOnly" or "readWrite"
	}
	pairs := make([]kv, 0, len(msg.Fields))
	for _, f := range msg.Fields {
		v := wusp.ValueToString(f.Val)
		v = strings.ReplaceAll(v, "\x00", "")
		if strings.HasPrefix(v, "devicex") || strings.HasPrefix(v, "alias-devicex") {
			continue
		}
		// Filter Bootstrap filler: max-uint32 counters and epoch-era dates
		if v == "4294967295" || v == "2147483647" || strings.HasPrefix(v, "1816-") || strings.HasPrefix(v, "0001-") {
			continue
		}
		access := wusp.ParamAccess(f.Path)
		pairs = append(pairs, kv{Path: f.Path, Value: v, Access: access})
	}
	return json.Marshal(pairs)
}
