package userspace

import (
	"encoding/binary"
	"net/netip"
	"testing"
	"time"

	"WantasticCore/internal/wg/userspace/wireguard-go/conn"
)

func TestReceiverIndexForSharedBindPacket(t *testing.T) {
	response := make([]byte, 12)
	response[0] = messageTypeHandshakeResponse
	binary.LittleEndian.PutUint32(response[4:8], 111)
	binary.LittleEndian.PutUint32(response[8:12], 222)

	if got, ok := receiverIndexForSharedBindPacket(messageTypeHandshakeResponse, response); !ok || got != 222 {
		t.Fatalf("handshake response receiver index = (%v, %v), want (222, true)", got, ok)
	}

	cookie := make([]byte, 8)
	cookie[0] = messageTypeCookieReply
	binary.LittleEndian.PutUint32(cookie[4:8], 333)
	if got, ok := receiverIndexForSharedBindPacket(messageTypeCookieReply, cookie); !ok || got != 333 {
		t.Fatalf("cookie reply receiver index = (%v, %v), want (333, true)", got, ok)
	}

	transport := make([]byte, 8)
	transport[0] = messageTypeTransportData
	binary.LittleEndian.PutUint32(transport[4:8], 444)
	if got, ok := receiverIndexForSharedBindPacket(messageTypeTransportData, transport); !ok || got != 444 {
		t.Fatalf("transport receiver index = (%v, %v), want (444, true)", got, ok)
	}
}

func TestShouldDropQueuedTenantPacket(t *testing.T) {
	now := time.Now()

	staleHandshake := &tenantPacket{
		data:       []byte{messageTypeHandshakeInitiation},
		size:       1,
		enqueuedAt: now.Add(-tenantDataQueueSojournLimit - time.Millisecond),
	}
	if shouldDropQueuedTenantPacket(staleHandshake, now) {
		t.Fatal("expected stale handshake packet to be preserved")
	}

	staleResponse := &tenantPacket{
		data:       []byte{messageTypeHandshakeResponse},
		size:       1,
		enqueuedAt: now.Add(-tenantDataQueueSojournLimit - time.Millisecond),
	}
	if shouldDropQueuedTenantPacket(staleResponse, now) {
		t.Fatal("expected stale handshake response packet to be preserved")
	}

	staleCookie := &tenantPacket{
		data:       []byte{messageTypeCookieReply},
		size:       1,
		enqueuedAt: now.Add(-tenantDataQueueSojournLimit - time.Millisecond),
	}
	if shouldDropQueuedTenantPacket(staleCookie, now) {
		t.Fatal("expected stale cookie reply packet to be preserved")
	}

	staleTransport := &tenantPacket{
		data:       []byte{messageTypeTransportData},
		size:       1,
		enqueuedAt: now.Add(-tenantDataQueueSojournLimit - time.Millisecond),
	}
	if !shouldDropQueuedTenantPacket(staleTransport, now) {
		t.Fatal("expected stale transport packet to be dropped")
	}
}

func TestDispatchPacketRecordsRouteMisses(t *testing.T) {
	bind := &SharedPortBindV2{}
	addr := netip.MustParseAddrPort("192.0.2.10:51820")

	response := make([]byte, 12)
	response[0] = messageTypeHandshakeResponse
	binary.LittleEndian.PutUint32(response[8:12], 101)
	bind.dispatchPacket(response, addr)

	cookie := make([]byte, 8)
	cookie[0] = messageTypeCookieReply
	binary.LittleEndian.PutUint32(cookie[4:8], 202)
	bind.dispatchPacket(cookie, addr)

	transport := make([]byte, 8)
	transport[0] = messageTypeTransportData
	binary.LittleEndian.PutUint32(transport[4:8], 303)
	bind.dispatchPacket(transport, addr)

	statsPacket := make([]byte, 8)
	statsPacket[0] = messageTypeStats
	binary.LittleEndian.PutUint32(statsPacket[4:8], 404)
	bind.dispatchPacket(statsPacket, addr)

	short := []byte{messageTypeHandshakeResponse}
	bind.dispatchPacket(short, addr)

	stats := bind.Stats()
	if stats["route_miss_handshake_response"] != 1 {
		t.Fatalf("route_miss_handshake_response = %d, want 1", stats["route_miss_handshake_response"])
	}
	if stats["route_miss_cookie_reply"] != 1 {
		t.Fatalf("route_miss_cookie_reply = %d, want 1", stats["route_miss_cookie_reply"])
	}
	if stats["route_miss_transport"] != 1 {
		t.Fatalf("route_miss_transport = %d, want 1", stats["route_miss_transport"])
	}
	if stats["route_miss_stats"] != 1 {
		t.Fatalf("route_miss_stats = %d, want 1", stats["route_miss_stats"])
	}
	if stats["receiver_parse_failures"] != 1 {
		t.Fatalf("receiver_parse_failures = %d, want 1", stats["receiver_parse_failures"])
	}
	if stats["dropped"] != 1 {
		t.Fatalf("dropped = %d, want 1", stats["dropped"])
	}
}

func TestSharedBindStatsRecordQueuePressure(t *testing.T) {
	addr := netip.MustParseAddrPort("192.0.2.20:51820")
	bind := &SharedPortBindV2{}
	bind.handshakeBroadcast.Store("tenant-a", &tenantRoute{
		tenantID: "tenant-a",
		recvCh:   make(chan *tenantPacket),
	})

	bind.broadcastHandshake([]byte{messageTypeHandshakeInitiation}, addr)
	bind.deliverToTenant(&tenantRoute{
		tenantID: "tenant-b",
		recvCh:   make(chan *tenantPacket),
	}, []byte{messageTypeHandshakeResponse}, addr)

	stats := bind.Stats()
	if stats["handshake_broadcast_queue_full_drops"] != 1 {
		t.Fatalf("handshake_broadcast_queue_full_drops = %d, want 1", stats["handshake_broadcast_queue_full_drops"])
	}
	if stats["tenant_queue_full_drops"] != 1 {
		t.Fatalf("tenant_queue_full_drops = %d, want 1", stats["tenant_queue_full_drops"])
	}
	if stats["dropped"] != 2 {
		t.Fatalf("dropped = %d, want 2", stats["dropped"])
	}
}

func TestReceiveFnRecordsStaleDataQueueDrops(t *testing.T) {
	shared := &SharedPortBindV2{}
	bind := &perTenantBindV2{
		shared:   shared,
		tenantID: "tenant-a",
		recvCh:   make(chan *tenantPacket, 2),
	}

	recvFns, _, err := bind.Open(0)
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	defer bind.Close()

	now := time.Now()
	bind.recvCh <- &tenantPacket{
		data:       []byte{messageTypeTransportData},
		size:       1,
		addr:       netip.MustParseAddrPort("192.0.2.30:51820"),
		enqueuedAt: now.Add(-tenantDataQueueSojournLimit - time.Millisecond),
	}
	bind.recvCh <- &tenantPacket{
		data:       []byte{messageTypeHandshakeResponse},
		size:       1,
		addr:       netip.MustParseAddrPort("192.0.2.31:51820"),
		enqueuedAt: now,
	}

	bufs := [][]byte{make([]byte, 64)}
	sizes := make([]int, 1)
	eps := make([]conn.Endpoint, 1)
	n, err := recvFns[0](bufs, sizes, eps)
	if err != nil {
		t.Fatalf("receiveFn() error = %v", err)
	}
	if n != 1 {
		t.Fatalf("receiveFn() count = %d, want 1", n)
	}

	stats := shared.Stats()
	if stats["stale_data_queue_drops"] != 1 {
		t.Fatalf("stale_data_queue_drops = %d, want 1", stats["stale_data_queue_drops"])
	}
	if stats["dropped"] != 1 {
		t.Fatalf("dropped = %d, want 1", stats["dropped"])
	}
}

func TestHandshakeRateLimitIsPerAddrPort(t *testing.T) {
	bind := &SharedPortBindV2{}
	first := netip.MustParseAddrPort("198.51.100.10:11111")
	second := netip.MustParseAddrPort("198.51.100.10:22222")

	for i := 0; i < handshakeTokensBucket; i++ {
		if !bind.checkHandshakeRateLimit(first) {
			t.Fatalf("checkHandshakeRateLimit(first) failed early at %d", i)
		}
	}
	if bind.checkHandshakeRateLimit(first) {
		t.Fatal("expected first endpoint bucket to be exhausted")
	}
	if !bind.checkHandshakeRateLimit(second) {
		t.Fatal("expected second endpoint on same public IP to retain its own budget")
	}
}

func TestLookupAddrRouteDoesNotRefreshOnRead(t *testing.T) {
	bind := &SharedPortBindV2{}
	addr := "198.51.100.20:33333"
	route := &tenantRoute{tenantID: "tenant-a"}
	entry := &addrRouteCacheEntry{route: route}
	originalSeen := time.Now().Add(-time.Second).UnixNano()
	entry.lastSeen.Store(originalSeen)
	bind.addrToTenant.Store(addr, entry)

	got := bind.lookupAddrRoute(addr)
	if got != route {
		t.Fatal("expected route lookup to return stored route")
	}
	if seen := entry.lastSeen.Load(); seen != originalSeen {
		t.Fatalf("lookupAddrRoute() refreshed lastSeen to %d, want %d", seen, originalSeen)
	}
}

func TestUnregisterTenantCleansAddrRouteCache(t *testing.T) {
	bind := &SharedPortBindV2{}
	addr := "198.51.100.21:44444"
	route := &tenantRoute{tenantID: "tenant-a"}
	entry := &addrRouteCacheEntry{route: route}
	entry.lastSeen.Store(time.Now().UnixNano())
	bind.addrToTenant.Store(addr, entry)

	bind.UnregisterTenant("tenant-a")

	if got := bind.lookupAddrRoute(addr); got != nil {
		t.Fatalf("expected addr route cache entry to be removed, got %#v", got)
	}
}
