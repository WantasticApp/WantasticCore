//go:build darwin

package userspace

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"net"
	"net/netip"
	"runtime"
	"sync"
	"sync/atomic"
	"syscall"
	"time"
	"unsafe"

	"WantasticCore/internal/wg/userspace/wireguard-go/conn"

	"github.com/rs/zerolog/log"
	"golang.org/x/net/ipv4"
	"golang.org/x/net/ipv6"
	"golang.org/x/sys/unix"
)

const (
	messageTypeHandshakeInitiation = 1
	messageTypeHandshakeResponse   = 2
	messageTypeCookieReply         = 3
	messageTypeTransportData       = 4
	messageTypeStats               = 5
	messageTypeP2P                 = 6
	messageTypeTUNControl          = 7
	messageTypeWUSP                = 8
	perTenantQueueSize             = 8192
	// perTenantControlQueueSize is the depth of the priority lane reserved
	// for handshake/cookie/P2P traffic — see linux file for full rationale.
	perTenantControlQueueSize = 512
	addrRouteTTL              = 2 * time.Minute
	cacheCleanupInterval      = 5 * time.Minute
)

// SharedPortBindV2 - High-performance multi-tenant shared socket
// Architecture inspired by Tailscale's magicsock + batching.Conn
//
// Key optimizations:
// - SO_REUSEPORT for kernel-level load balancing across cores
// - recvmmsg/sendmmsg for batched syscalls (Linux)
// - UDP GSO for TX coalescing (64 datagrams per syscall)
// - UDP GRO for RX coalescing
// - sync.Map for O(1) receiver_index → tenant routing
// - sync.Pool for zero-alloc hot path
// - Atomic operations for lock-free stats
// - CoDel-inspired AQM for latency control
type SharedPortBindV2 struct {
	conns     []*net.UDPConn
	xconns    []xnetBatchRW // ipv4.PacketConn wrappers
	port      int
	batchSize int

	// Round-robin counter for TX load balancing
	txIdx atomic.Uint64

	// TX/RX offload support (Linux GSO/GRO)
	txOffload atomic.Bool
	rxOffload bool

	// Fast-path routing: receiver_index (uint32) → *tenantRoute
	routingTable sync.Map

	// Fast-path handshake routing: source_addr (IP:Port) → *tenantRoute
	// PERFORMANCE: Avoids broadcasting to all tenants for established peers
	addrToTenant sync.Map

	// Handshake broadcast list: tenantID (string) → *tenantRoute
	handshakeBroadcast sync.Map

	// Handshake rate limiting: source endpoint (IP:port) → *handshakeRateLimit.
	// Using the full endpoint avoids penalizing many peers sharing one public IP.
	handshakeRateLimits sync.Map

	// Pool for receive batches
	receiveBatchPool sync.Pool

	// Pool for send batches (TX GSO coalescing)
	sendBatchPool sync.Pool

	// Control
	stopCh chan struct{}
	closed atomic.Bool

	// Stats
	packetsReceived atomic.Uint64
	packetsSent     atomic.Uint64
	packetsDropped  atomic.Uint64

	routeMissHandshakeResponse       atomic.Uint64
	routeMissCookieReply             atomic.Uint64
	routeMissTransport               atomic.Uint64
	routeMissStats                   atomic.Uint64
	receiverParseFailures            atomic.Uint64
	tenantQueueFullDrops             atomic.Uint64
	handshakeBroadcastQueueFullDrops atomic.Uint64
	staleDataQueueDrops              atomic.Uint64
	handshakeRateLimited             atomic.Uint64

	// Callbacks
	peerActiveHandler func(tenantID, peerEndpoint string)
}

// SetPeerActiveHandler sets the callback for peer activity events.
func (b *SharedPortBindV2) SetPeerActiveHandler(handler func(tenantID, peerEndpoint string)) {
	b.peerActiveHandler = handler
}

// handshakeRateLimit implements a token bucket for rate limiting handshakes per IP
type handshakeRateLimit struct {
	tokens     atomic.Int32 // Current token count
	lastRefill atomic.Int64 // Last refill timestamp (unix nano)
}

type xnetBatchRW interface {
	ReadBatch([]ipv6.Message, int) (int, error)
	WriteBatch([]ipv6.Message, int) (int, error)
}

type tenantRoute struct {
	tenantID string
	// recvCh carries data-plane traffic; controlCh is a small priority lane
	// for handshake/cookie/P2P. See linux file for rationale.
	recvCh    chan *tenantPacket
	controlCh chan *tenantPacket
	// dropCount is the running total of packets dropped on this tenant's
	// channels because they were full. lastDropLog throttles the warning
	// so high-rate drops do not flood the log — one line per tenant per
	// ~30s is enough to surface the problem without spamming.
	dropCount   atomic.Uint64
	lastDropLog atomic.Int64
}

type addrRouteCacheEntry struct {
	route    *tenantRoute
	lastSeen atomic.Int64
}

type tenantPacket struct {
	data       []byte
	size       int
	addr       netip.AddrPort
	enqueuedAt time.Time // For CoDel AQM
}

// Batching constants from Tailscale
const (
	// Control message size for GSO/GRO
	controlMessageSize = 24 // unix.CmsgSpace(2)

	// Handshake rate limiting - 50 handshakes per second per IP (increased for NAT/Offices)
	handshakeTokensBucket = 50 // Max tokens (burst)
	handshakeTokensRate   = 5  // Tokens per 100ms
	handshakeRefillPeriod = 100 * time.Millisecond

	// Drop stale data packets under queue pressure, but preserve control traffic.
	tenantDataQueueSojournLimit = 20 * time.Millisecond
)

// receiveBatch holds pre-allocated message buffers
type receiveBatch struct {
	msgs []ipv6.Message
}

// NewSharedPortBindV2 creates a high-performance shared socket
func NewSharedPortBindV2(port int) (*SharedPortBindV2, error) {
	// Use one socket per CPU core for optimal scaling via SO_REUSEPORT
	numSockets := runtime.NumCPU()
	if numSockets < 1 {
		numSockets = 1
	}

	conns := make([]*net.UDPConn, numSockets)

	// Configure SO_REUSEPORT to allow multiple sockets on same port
	lc := net.ListenConfig{
		Control: func(network, address string, c syscall.RawConn) error {
			var err error
			c.Control(func(fd uintptr) {
				err = unix.SetsockoptInt(int(fd), unix.SOL_SOCKET, unix.SO_REUSEPORT, 1)
			})
			return err
		},
	}

	// Bind all sockets to the same port
	for i := 0; i < numSockets; i++ {
		pc, err := lc.ListenPacket(context.Background(), "udp4", fmt.Sprintf(":%d", port))
		if err != nil {
			// Cleanup already opened sockets
			for j := 0; j < i; j++ {
				conns[j].Close()
			}
			return nil, fmt.Errorf("bind UDP port %d (socket %d/%d): %w", port, i+1, numSockets, err)
		}

		udpConn := pc.(*net.UDPConn)

		// Aggressive buffer sizing (7MB like Tailscale)
		const socketBufferSize = 7 << 20
		_ = udpConn.SetReadBuffer(socketBufferSize)
		_ = udpConn.SetWriteBuffer(socketBufferSize)

		conns[i] = udpConn
	}

	batchSize := conn.IdealBatchSize
	if batchSize <= 0 {
		batchSize = 128 // Tailscale default
	}

	b := &SharedPortBindV2{
		conns:     conns,
		port:      port,
		batchSize: batchSize,
		stopCh:    make(chan struct{}),
	}

	// Setup batch I/O on Linux
	b.xconns = make([]xnetBatchRW, numSockets)
	for i, pc := range conns {
		b.xconns[i] = ipv4.NewPacketConn(pc)
	}

	b.txOffload.Store(false) // Start with TX offload disabled, enable after probe
	b.rxOffload = false

	// Probe GSO/GRO support (using first socket)
	b.probeUDPOffload()

	// Initialize receive batch pool
	b.receiveBatchPool = sync.Pool{
		New: func() any {
			msgs := make([]ipv6.Message, b.batchSize)
			for i := range msgs {
				msgs[i].Buffers = make([][]byte, 1)
				msgs[i].Buffers[0] = make([]byte, 2048)
				msgs[i].OOB = make([]byte, controlMessageSize)
			}
			return &receiveBatch{msgs: msgs}
		},
	}

	// Initialize send batch pool for TX GSO coalescing
	b.sendBatchPool = sync.Pool{
		New: func() any {
			msgs := make([]ipv6.Message, b.batchSize)
			for i := range msgs {
				msgs[i].Buffers = make([][]byte, 1)
				msgs[i].OOB = make([]byte, controlMessageSize)
			}
			return msgs
		},
	}

	// Start receive workers (one per socket)
	for i := 0; i < numSockets; i++ {
		go b.receiveLoop(i)
	}
	go b.cacheJanitorLoop()

	log.Debug().
		Int("port", port).
		Int("batch_size", batchSize).
		Int("sockets", numSockets).
		Bool("gso", b.txOffload.Load()).
		Bool("gro", b.rxOffload).
		Msg("SharedPortBindV2: initialized with SO_REUSEPORT and multi-core scaling")

	return b, nil
}

// probeUDPOffload checks for GSO/GRO support
func (b *SharedPortBindV2) probeUDPOffload() {
	if len(b.conns) == 0 {
		return
	}
	// Probe using the first socket
	rc, err := b.conns[0].SyscallConn()
	if err != nil {
		return
	}

	var hasTX, hasRX bool
	_ = rc.Control(func(fd uintptr) {
		// Check GSO (TX offload) support
		_, errSyscall := syscall.GetsockoptInt(int(fd), unix.IPPROTO_UDP, 103)
		hasTX = errSyscall == nil

		// Enable GRO (RX offload)
		errSyscall = syscall.SetsockoptInt(int(fd), unix.IPPROTO_UDP, 104, 1)
		hasRX = errSyscall == nil
	})

	b.txOffload.Store(hasTX)
	b.rxOffload = hasRX

	log.Debug().Bool("gso", hasTX).Bool("gro", hasRX).Msg("UDP offload probe complete")
}

// receiveLoop is the main packet receive goroutine
func (b *SharedPortBindV2) receiveLoop(socketIdx int) {
	if len(b.xconns) > socketIdx && b.xconns[socketIdx] != nil {
		b.receiveLoopBatch(socketIdx)
	} else {
		b.receiveLoopSingle(socketIdx)
	}
}

// receiveLoopBatch uses recvmmsg for high throughput
func (b *SharedPortBindV2) receiveLoopBatch(socketIdx int) {
	// Read buffer: 64KB for GRO support (must be large enough for coalesced packets)
	// We use a separate read buffer to allow GRO to work effectively while keeping
	// the processing batch (batch.msgs) using standard MTU-sized buffers.
	const readBatchSize = 32
	readMsgs := make([]ipv6.Message, readBatchSize)
	for i := range readMsgs {
		readMsgs[i].Buffers = make([][]byte, 1)
		readMsgs[i].Buffers[0] = make([]byte, 65535) // Max IP packet size
		readMsgs[i].OOB = make([]byte, controlMessageSize)
	}

	batch := b.receiveBatchPool.Get().(*receiveBatch)
	defer b.receiveBatchPool.Put(batch)

	xconn := b.xconns[socketIdx]

	for {
		select {
		case <-b.stopCh:
			return
		default:
		}

		// Read into our large-buffer read batch
		numMsgs, err := xconn.ReadBatch(readMsgs, 0)
		if err != nil {
			if b.closed.Load() || errors.Is(err, net.ErrClosed) {
				return
			}
			if isTemporaryError(err) {
				continue
			}
			log.Err(err).Msg("batch read failed")
			time.Sleep(time.Millisecond)
			continue
		}

		if numMsgs == 0 {
			continue
		}

		// Process read messages into the output batch
		batchIdx := 0

		for i := 0; i < numMsgs; i++ {
			msg := &readMsgs[i]
			if msg.N <= 0 {
				continue
			}

			// Check for GRO
			gsoSize := 0
			if b.rxOffload {
				gsoSize, _ = b.getGSOSizeFromControl(msg.OOB[:msg.NN])
			}

			if gsoSize > 0 {
				// Split GRO packet
				data := msg.Buffers[0][:msg.N]
				for start := 0; start < len(data); start += gsoSize {
					end := start + gsoSize
					if end > len(data) {
						end = len(data)
					}

					// Copy segment to output batch
					if batchIdx >= len(batch.msgs) {
						b.dispatchBatch(batch, batchIdx)
						batchIdx = 0
					}

					outMsg := &batch.msgs[batchIdx]
					n := copy(outMsg.Buffers[0], data[start:end])
					outMsg.N = n
					outMsg.Addr = msg.Addr
					batchIdx++
				}
			} else {
				// Single packet
				if batchIdx >= len(batch.msgs) {
					b.dispatchBatch(batch, batchIdx)
					batchIdx = 0
				}

				outMsg := &batch.msgs[batchIdx]
				n := copy(outMsg.Buffers[0], msg.Buffers[0][:msg.N])
				outMsg.N = n
				outMsg.Addr = msg.Addr
				batchIdx++
			}
		}

		// Dispatch remaining
		if batchIdx > 0 {
			b.dispatchBatch(batch, batchIdx)
		}
	}
} // dispatchBatch processes a batch of messages
func (b *SharedPortBindV2) dispatchBatch(batch *receiveBatch, count int) {
	for i := 0; i < count; i++ {
		msg := &batch.msgs[i]
		if msg.N <= 4 {
			continue
		}

		addr, ok := addrPortFromNetAddr(msg.Addr)
		if !ok {
			continue
		}

		b.packetsReceived.Add(1)
		b.dispatchPacket(msg.Buffers[0][:msg.N], addr)
	}
}

// receiveLoopSingle is fallback for non-Linux
func (b *SharedPortBindV2) receiveLoopSingle(socketIdx int) {
	buf := make([]byte, 2048)
	conn := b.conns[socketIdx]

	for {
		select {
		case <-b.stopCh:
			return
		default:
		}

		n, addr, err := conn.ReadFromUDPAddrPort(buf)
		if err != nil {
			if b.closed.Load() || errors.Is(err, net.ErrClosed) {
				return
			}
			if isTemporaryError(err) {
				continue
			}
			time.Sleep(time.Millisecond)
			continue
		}

		if n > 4 {
			b.packetsReceived.Add(1)
			b.dispatchPacket(buf[:n], addr)
		}
	}
}

// getGSOSizeFromControl extracts GSO size from control message
func (b *SharedPortBindV2) getGSOSizeFromControl(control []byte) (int, error) {
	if len(control) < unix.SizeofCmsghdr {
		return 0, nil
	}
	hdr, data, _, err := unix.ParseOneSocketControlMessage(control)
	if err != nil {
		return 0, err
	}
	if hdr.Level == unix.IPPROTO_UDP && hdr.Type == 104 && len(data) >= 2 {
		return int(binary.NativeEndian.Uint16(data[:2])), nil
	}
	return 0, nil
}

// dispatchPacket routes a packet to the appropriate tenant
func (b *SharedPortBindV2) dispatchPacket(data []byte, addr netip.AddrPort) {
	// // DEBUG: Trace all received packets to confirm ingress
	// if len(data) > 0 {
	// 	// Only log interesting types (Handshake=1, Response=2, Stats=5) to reduce noise, or all if debugging
	// 	if data[0] == 1 || data[0] == 2 || data[0] == 5 {
	// 		log.Debug().
	// 			Str("src", addr.String()).
	// 			Int("len", len(data)).
	// 			Uint8("type", data[0]).
	// 			Msg("SharedBind RX")
	// 	}
	// }

	msgType := data[0]

	switch msgType {
	case messageTypeHandshakeInitiation:
		b.broadcastHandshake(data, addr)

	case messageTypeHandshakeResponse, messageTypeCookieReply, messageTypeTransportData, messageTypeStats, messageTypeTUNControl, messageTypeWUSP:
		receiverIdx, ok := receiverIndexForSharedBindPacket(msgType, data)
		if !ok {
			b.recordReceiverParseFailure()
			return
		}
		if route := b.lookupRoute(receiverIdx); route != nil {
			b.storeAddrRoute(addr.String(), route)

			b.deliverToTenant(route, data, addr)
		} else {
			b.recordRouteMiss(msgType)
			if msgType == messageTypeTransportData {
				b.packetsDropped.Add(1)
			}
		}
	case messageTypeP2P:
		// P2P/Punch packets do not have a receiverIdx.
		// If the peer has already sent transport data, we can fast-path route it.
		// If not (e.g. this is the first packet after handshake), we must broadcast it
		// so the correct tenant can process it.
		if route := b.lookupAddrRoute(addr.String()); route != nil {
			b.deliverToTenant(route, data, addr)
		} else {
			// Unknown client attempting P2P before transport data; broadcast it safely
			b.broadcastHandshake(data, addr)
		}
	}
}

// broadcastHandshake sends to a specific tenant if known, otherwise to all with rate limiting
func (b *SharedPortBindV2) broadcastHandshake(data []byte, addr netip.AddrPort) {
	// ❌ WE NO LONGER USE addrToTenant fast-path for handshakes!
	// This prevents the sticky IP/port bug where a device gets bound to the wrong tenant if moved.
	// We MUST broadcast all HandshakeInitiations to all tenants so the correct one can decrypt it.

	// Rate limit handshakes per source IP for unknown peers
	if !b.checkHandshakeRateLimit(addr) {
		b.packetsDropped.Add(1)
		b.recordHandshakeRateLimited()
		log.Warn().
			Str("source_endpoint", addr.String()).
			Msg("Handshake rate limited")
		return
	}

	// PERFORMANCE: Pre-clone data ONCE for all recipients to reduce GC pressure
	// Although each tenant needs its own buffer if it modifies it, WireGuard handshakes are read-only
	// during initial processing. To be safe, we clone for the channel.
	pktData := make([]byte, len(data))
	copy(pktData, data)

	b.handshakeBroadcast.Range(func(key, value any) bool {
		route := value.(*tenantRoute)

		pkt := &tenantPacket{data: pktData, size: len(data), addr: addr, enqueuedAt: time.Now()}

		// Handshake initiations belong on the priority lane. Fall back to
		// recvCh only when controlCh has not been wired (older callers).
		ch := route.controlCh
		if ch == nil {
			ch = route.recvCh
		}

		select {
		case ch <- pkt:
			// Delivered
		default:
			// Queue full, drop
			b.packetsDropped.Add(1)
			b.recordHandshakeBroadcastQueueFullDrop()
		}
		return true
	})
}

// checkHandshakeRateLimit implements token bucket rate limiting per IP
func (b *SharedPortBindV2) checkHandshakeRateLimit(addr netip.AddrPort) bool {
	ipKey := addr.String()
	now := time.Now().UnixNano()

	// Get or create rate limiter for this IP
	limiterInterface, _ := b.handshakeRateLimits.LoadOrStore(ipKey, &handshakeRateLimit{})
	limiter := limiterInterface.(*handshakeRateLimit)

	// Initialize if first time
	if limiter.lastRefill.Load() == 0 {
		limiter.lastRefill.Store(now)
		limiter.tokens.Store(handshakeTokensBucket - 1) // Take one token
		return true
	}

	// Calculate tokens to add based on time elapsed
	lastRefill := limiter.lastRefill.Load()
	elapsed := time.Duration(now - lastRefill)

	if elapsed >= handshakeRefillPeriod {
		// Refill tokens
		tokensToAdd := int32(elapsed / handshakeRefillPeriod * handshakeTokensRate)
		currentTokens := limiter.tokens.Load()
		newTokens := currentTokens + tokensToAdd

		// Cap at bucket size
		if newTokens > handshakeTokensBucket {
			newTokens = handshakeTokensBucket
		}

		limiter.tokens.Store(newTokens)
		limiter.lastRefill.Store(now)
	}

	// Try to consume a token
	for {
		currentTokens := limiter.tokens.Load()
		if currentTokens <= 0 {
			return false // Rate limited
		}

		if limiter.tokens.CompareAndSwap(currentTokens, currentTokens-1) {
			return true // Token consumed, allow
		}
		// CAS failed, retry
	}
}

func (b *SharedPortBindV2) storeAddrRoute(addr string, route *tenantRoute) {
	now := time.Now().UnixNano()
	if value, ok := b.addrToTenant.Load(addr); ok {
		entry := value.(*addrRouteCacheEntry)
		entry.route = route
		entry.lastSeen.Store(now)
		return
	}

	entry := &addrRouteCacheEntry{route: route}
	entry.lastSeen.Store(now)
	b.addrToTenant.Store(addr, entry)
}

func (b *SharedPortBindV2) lookupAddrRoute(addr string) *tenantRoute {
	value, ok := b.addrToTenant.Load(addr)
	if !ok {
		return nil
	}

	entry := value.(*addrRouteCacheEntry)
	lastSeen := entry.lastSeen.Load()
	if lastSeen == 0 || time.Since(time.Unix(0, lastSeen)) > addrRouteTTL {
		b.addrToTenant.Delete(addr)
		return nil
	}

	return entry.route
}

// lookupRoute finds tenant route by receiver index
func (b *SharedPortBindV2) lookupRoute(receiverIdx uint32) *tenantRoute {
	if value, ok := b.routingTable.Load(receiverIdx); ok {
		return value.(*tenantRoute)
	}
	return nil
}

// deliverToTenant sends packet to tenant's receive channel. Control traffic
// (handshake init/response, cookie reply, P2P) takes the small priority lane
// so handshakes survive when the data lane is full — see linux file for full
// rationale.
func (b *SharedPortBindV2) deliverToTenant(route *tenantRoute, data []byte, addr netip.AddrPort) {
	clone := make([]byte, len(data))
	copy(clone, data)

	pkt := &tenantPacket{data: clone, size: len(data), addr: addr, enqueuedAt: time.Now()}

	ch := route.recvCh
	if route.controlCh != nil && len(data) > 0 {
		switch data[0] {
		case messageTypeHandshakeInitiation, messageTypeHandshakeResponse,
			messageTypeCookieReply, messageTypeP2P:
			ch = route.controlCh
		}
	}

	select {
	case ch <- pkt:
		// Delivered
	default:
		b.packetsDropped.Add(1)
		b.recordTenantQueueFullDrop()
		// Per-tenant drop accounting with a 30s log throttle so a stuck
		// peer reading too slowly is visible (otherwise drops are silent
		// and the peer just looks "online but unreachable"). Control-lane
		// drops are flagged separately because they prevent reconnects.
		isControl := ch == route.controlCh && route.controlCh != nil
		accum := route.dropCount.Add(1)
		nowNs := time.Now().UnixNano()
		last := route.lastDropLog.Load()
		if (last == 0 || nowNs-last > int64(30*time.Second)) && route.lastDropLog.CompareAndSwap(last, nowNs) {
			ev := log.Warn().
				Str("tenant", route.tenantID).
				Uint64("accumulated_drops", accum).
				Bool("control_lane", isControl)
			if isControl {
				ev.Int("queue_capacity", perTenantControlQueueSize).
					Msg("tenant control lane full — handshakes/cookies dropping; peers will fail to reconnect")
			} else {
				ev.Int("queue_capacity", perTenantQueueSize).
					Msg("tenant recv queue full — packets being dropped silently")
			}
		}
	}
}

// RegisterTenantV2 adds a tenant for handshake broadcasts. controlCh may be
// nil (older callers / tests); when nil, control packets fall back to recvCh.
func (b *SharedPortBindV2) RegisterTenantV2(tenantID string, recvCh, controlCh chan *tenantPacket) {
	route := &tenantRoute{tenantID: tenantID, recvCh: recvCh, controlCh: controlCh}
	b.handshakeBroadcast.Store(tenantID, route)
	log.Debug().Str("tenant", tenantID[:8]).Msg("registered tenant for handshake broadcast")
}

// RegisterPeerIndex maps receiver_index → tenant for fast routing
func (b *SharedPortBindV2) RegisterPeerIndex(receiverIdx uint32, tenantID string) {
	if value, ok := b.handshakeBroadcast.Load(tenantID); ok {
		route := value.(*tenantRoute)
		b.routingTable.Store(receiverIdx, route)
		log.Debug().Uint32("idx", receiverIdx).Str("tenant", tenantID[:8]).Msg("mapped receiver index")
	}
}

// UnregisterTenant removes a tenant
func (b *SharedPortBindV2) UnregisterTenant(tenantID string) {
	b.handshakeBroadcast.Delete(tenantID)

	// Clean up routing entries
	b.routingTable.Range(func(key, value any) bool {
		if route := value.(*tenantRoute); route.tenantID == tenantID {
			b.routingTable.Delete(key)
		}
		return true
	})

	// Clean up fast-path handshake routing cache
	b.addrToTenant.Range(func(key, value any) bool {
		if entry, ok := value.(*addrRouteCacheEntry); ok && entry.route != nil && entry.route.tenantID == tenantID {
			b.addrToTenant.Delete(key)
		}
		return true
	})
}

// Send writes packets to the network (called by WireGuard)
func (b *SharedPortBindV2) Send(data []byte, addr netip.AddrPort) error {
	// Round-robin transmit across sockets
	idx := b.txIdx.Add(1) % uint64(len(b.conns))
	conn := b.conns[idx]

	_, err := conn.WriteToUDPAddrPort(data, addr)
	if err == nil {
		b.packetsSent.Add(1)
	}
	return err
}

// SendBatch sends multiple packets with GSO coalescing when available
// This is the high-performance TX path for bulk data transfer
func (b *SharedPortBindV2) SendBatch(bufs [][]byte, addr netip.AddrPort) error {
	if len(bufs) == 0 {
		return nil
	}

	// Round-robin transmit across sockets
	idx := b.txIdx.Add(1) % uint64(len(b.conns))
	conn := b.conns[idx]

	// Fast path: single packet or no GSO
	if len(bufs) == 1 || !b.txOffload.Load() || len(b.xconns) == 0 {
		for _, buf := range bufs {
			if _, err := conn.WriteToUDPAddrPort(buf, addr); err != nil {
				return err
			}
			b.packetsSent.Add(1)
		}
		return nil
	}

	// GSO path: coalesce packets into batched sendmsg
	// Group packets by size for GSO (all packets in a GSO batch must be same size except last)
	return b.sendWithGSO(bufs, addr, idx)
}

// sendWithGSO uses UDP GSO to coalesce multiple packets into one syscall
func (b *SharedPortBindV2) sendWithGSO(bufs [][]byte, addr netip.AddrPort, socketIdx uint64) error {
	if len(bufs) == 0 {
		return nil
	}

	conn := b.conns[socketIdx]
	xconn := b.xconns[socketIdx]

	// Find uniform segment size (use first packet's size)
	segmentSize := len(bufs[0])

	// Coalesce buffers that match the segment size
	// GSO requires all segments except the last to be the same size
	coalescedBuf := make([]byte, 0, segmentSize*len(bufs))
	for i, buf := range bufs {
		if i < len(bufs)-1 && len(buf) != segmentSize {
			// Non-uniform sizes, fall back to individual sends
			for _, pkt := range bufs {
				if _, err := conn.WriteToUDPAddrPort(pkt, addr); err != nil {
					return err
				}
				b.packetsSent.Add(1)
			}
			return nil
		}
		coalescedBuf = append(coalescedBuf, buf...)
	}

	// Build message with GSO control message
	msgs := b.sendBatchPool.Get().([]ipv6.Message)
	defer b.sendBatchPool.Put(msgs)

	msgs[0].Buffers[0] = coalescedBuf
	msgs[0].Addr = net.UDPAddrFromAddrPort(addr)
	msgs[0].OOB = b.makeGSOControlMessage(uint16(segmentSize))

	n, err := xconn.WriteBatch(msgs[:1], 0)
	if err != nil {
		// GSO failed, fall back to individual sends
		b.txOffload.Store(false) // Disable GSO for future sends
		for _, buf := range bufs {
			if _, err := conn.WriteToUDPAddrPort(buf, addr); err != nil {
				return err
			}
			b.packetsSent.Add(1)
		}
		return nil
	}

	if n > 0 {
		b.packetsSent.Add(uint64(len(bufs)))
	}
	return nil
}

// makeGSOControlMessage creates the control message for UDP_SEGMENT
func (b *SharedPortBindV2) makeGSOControlMessage(segmentSize uint16) []byte {
	oob := make([]byte, controlMessageSize)
	// Set up cmsg header for UDP_SEGMENT
	cmsg := (*unix.Cmsghdr)(unsafe.Pointer(&oob[0]))
	cmsg.Level = unix.IPPROTO_UDP
	cmsg.Type = 103
	cmsg.SetLen(unix.CmsgLen(2))
	*(*uint16)(unsafe.Pointer(&oob[unix.CmsgLen(0)])) = segmentSize
	return oob
}

// Close shuts down the socket
func (b *SharedPortBindV2) Close() error {
	if b.closed.Swap(true) {
		return nil
	}
	close(b.stopCh)

	// Clean up rate limiters (prevent memory leak)
	b.cleanupOldRateLimiters()
	b.cleanupOldAddrRoutes()

	var lastErr error
	for _, conn := range b.conns {
		if err := conn.Close(); err != nil {
			lastErr = err
		}
	}
	return lastErr
}

func (b *SharedPortBindV2) cacheJanitorLoop() {
	ticker := time.NewTicker(cacheCleanupInterval)
	defer ticker.Stop()

	for {
		select {
		case <-b.stopCh:
			return
		case <-ticker.C:
			b.cleanupOldRateLimiters()
			b.cleanupOldAddrRoutes()
		}
	}
}

// cleanupOldRateLimiters removes rate limiters that haven't been used recently
func (b *SharedPortBindV2) cleanupOldRateLimiters() {
	now := time.Now().UnixNano()
	cutoff := now - int64(5*time.Minute) // Remove if idle for 5 minutes

	b.handshakeRateLimits.Range(func(key, value any) bool {
		limiter := value.(*handshakeRateLimit)
		if limiter.lastRefill.Load() < cutoff {
			b.handshakeRateLimits.Delete(key)
		}
		return true
	})
}

func (b *SharedPortBindV2) cleanupOldAddrRoutes() {
	cutoff := time.Now().Add(-addrRouteTTL).UnixNano()

	b.addrToTenant.Range(func(key, value any) bool {
		entry := value.(*addrRouteCacheEntry)
		if entry.lastSeen.Load() < cutoff {
			b.addrToTenant.Delete(key)
		}
		return true
	})
}

// CreatePerTenantBind creates a conn.Bind wrapper for a tenant
func (b *SharedPortBindV2) CreatePerTenantBind(tenantID string) conn.Bind {
	return &perTenantBindV2{
		shared:    b,
		tenantID:  tenantID,
		recvCh:    make(chan *tenantPacket, perTenantQueueSize),
		controlCh: make(chan *tenantPacket, perTenantControlQueueSize),
	}
}

// Stats returns performance metrics
func (b *SharedPortBindV2) Stats() map[string]uint64 {
	return map[string]uint64{
		"received":                             b.packetsReceived.Load(),
		"sent":                                 b.packetsSent.Load(),
		"dropped":                              b.packetsDropped.Load(),
		"route_miss_handshake_response":        b.routeMissHandshakeResponse.Load(),
		"route_miss_cookie_reply":              b.routeMissCookieReply.Load(),
		"route_miss_transport":                 b.routeMissTransport.Load(),
		"route_miss_stats":                     b.routeMissStats.Load(),
		"receiver_parse_failures":              b.receiverParseFailures.Load(),
		"tenant_queue_full_drops":              b.tenantQueueFullDrops.Load(),
		"handshake_broadcast_queue_full_drops": b.handshakeBroadcastQueueFullDrops.Load(),
		"stale_data_queue_drops":               b.staleDataQueueDrops.Load(),
		"handshake_rate_limited":               b.handshakeRateLimited.Load(),
	}
}

// --- Per-tenant bind wrapper ---

type perTenantBindV2 struct {
	shared    *SharedPortBindV2
	tenantID  string
	recvCh    chan *tenantPacket
	controlCh chan *tenantPacket
	closeCh   chan struct{}
	closed    atomic.Bool
}

func (b *perTenantBindV2) Open(port uint16) ([]conn.ReceiveFunc, uint16, error) {
	// Reset close channel (WireGuard calls Close then Open during Up)
	b.closeCh = make(chan struct{})
	b.closed.Store(false)

	// Register with shared bind
	b.shared.RegisterTenantV2(b.tenantID, b.recvCh, b.controlCh)

	// Priority pass drains control lane first; nil controlCh is harmless
	// because a nil channel case in select never fires.
	receiveFn := func(bufs [][]byte, sizes []int, eps []conn.Endpoint) (int, error) {
		for {
			// Non-blocking priority pass for control traffic.
			select {
			case pkt := <-b.controlCh:
				if pkt == nil {
					return 0, net.ErrClosed
				}
				n := copy(bufs[0], pkt.data[:pkt.size])
				sizes[0] = n
				eps[0] = &conn.StdNetEndpoint{AddrPort: pkt.addr}
				return 1, nil
			default:
			}

			select {
			case <-b.closeCh:
				return 0, net.ErrClosed
			case pkt := <-b.controlCh:
				if pkt == nil {
					return 0, net.ErrClosed
				}
				n := copy(bufs[0], pkt.data[:pkt.size])
				sizes[0] = n
				eps[0] = &conn.StdNetEndpoint{AddrPort: pkt.addr}
				return 1, nil
			case pkt := <-b.recvCh:
				if pkt == nil {
					return 0, net.ErrClosed
				}

				// Drop only stale data packets under queue pressure; keep handshake/control traffic.
				if shouldDropQueuedTenantPacket(pkt, time.Now()) {
					b.shared.packetsDropped.Add(1)
					b.shared.recordStaleDataQueueDrop()
					continue
				}

				n := copy(bufs[0], pkt.data[:pkt.size])
				sizes[0] = n
				eps[0] = &conn.StdNetEndpoint{AddrPort: pkt.addr}
				return 1, nil
			}
		}
	}

	return []conn.ReceiveFunc{receiveFn}, uint16(b.shared.port), nil
}

func (b *perTenantBindV2) Close() error {
	if b.closed.Swap(true) {
		return nil
	}
	// closeCh may be nil if Close() is called before Open()
	if b.closeCh != nil {
		select {
		case <-b.closeCh:
		default:
			close(b.closeCh)
		}
	}
	b.shared.UnregisterTenant(b.tenantID)
	return nil
}

func (b *perTenantBindV2) Send(bufs [][]byte, endpoint conn.Endpoint) error {
	ep, ok := endpoint.(*conn.StdNetEndpoint)
	if !ok {
		return fmt.Errorf("invalid endpoint type")
	}

	// Learn sender index from handshake responses (must check before batch send)
	for _, buf := range bufs {
		if len(buf) >= 8 && buf[0] == messageTypeHandshakeResponse {
			senderIdx := binary.LittleEndian.Uint32(buf[4:8])
			b.shared.RegisterPeerIndex(senderIdx, b.tenantID)

			// Notify manager about active peer (for instant discovery/roaming)
			if b.shared.peerActiveHandler != nil {
				// Extract destination endpoint from the conn.Endpoint
				// This is where we are SENDING the response TO (the peer's public IP:Port)
				dst := ep.DstToString()
				if dst != "" {
					// Async call to avoid blocking the hot path
					go b.shared.peerActiveHandler(b.tenantID, dst)
				}
			}
		}
	}

	// Use batch send with GSO coalescing for better throughput
	return b.shared.SendBatch(bufs, ep.AddrPort)
}

func (b *perTenantBindV2) SetMark(mark uint32) error { return nil }

// BatchSize returns ideal batch size for WireGuard
// When GSO is enabled, we can handle up to 64 packets per batch
func (b *perTenantBindV2) BatchSize() int {
	if b.shared.txOffload.Load() {
		return 64 // GSO can coalesce up to 64 segments
	}
	return 1 // Without GSO, single packets only
}

func (b *perTenantBindV2) ParseEndpoint(s string) (conn.Endpoint, error) {
	addr, err := netip.ParseAddrPort(s)
	if err != nil {
		return nil, err
	}
	return &conn.StdNetEndpoint{AddrPort: addr}, nil
}

// --- Helpers ---
func isTemporaryError(err error) bool {
	if ne, ok := err.(net.Error); ok {
		return ne.Timeout()
	}
	return false
}

func receiverIndexForSharedBindPacket(msgType byte, data []byte) (uint32, bool) {
	switch msgType {
	case messageTypeHandshakeResponse:
		if len(data) < 12 {
			return 0, false
		}
		return binary.LittleEndian.Uint32(data[8:12]), true
	case messageTypeCookieReply, messageTypeTransportData, messageTypeStats, messageTypeTUNControl, messageTypeWUSP:
		if len(data) < 8 {
			return 0, false
		}
		return binary.LittleEndian.Uint32(data[4:8]), true
	default:
		return 0, false
	}
}

func shouldDropQueuedTenantPacket(pkt *tenantPacket, now time.Time) bool {
	if pkt == nil || pkt.size == 0 || len(pkt.data) == 0 {
		return true
	}
	if now.Sub(pkt.enqueuedAt) <= tenantDataQueueSojournLimit {
		return false
	}

	switch pkt.data[0] {
	case messageTypeTransportData, messageTypeStats, messageTypeTUNControl, messageTypeWUSP:
		return true
	default:
		return false
	}
}
func addrPortFromNetAddr(addr net.Addr) (netip.AddrPort, bool) {
	udp, ok := addr.(*net.UDPAddr)
	if !ok || udp == nil {
		return netip.AddrPort{}, false
	}
	var ip netip.Addr
	if ip4 := udp.IP.To4(); ip4 != nil {
		ip = netip.AddrFrom4([4]byte{ip4[0], ip4[1], ip4[2], ip4[3]})
	} else if ip16 := udp.IP.To16(); ip16 != nil {
		var raw16 [16]byte
		copy(raw16[:], ip16)
		ip = netip.AddrFrom16(raw16)
	} else {
		return netip.AddrPort{}, false
	}
	return netip.AddrPortFrom(ip, uint16(udp.Port)), true
}

func shortTenantID(id string) string {
	if len(id) <= 8 {
		return id
	}
	return id[:8]
}
