package server

import (
	"context"
	"errors"
	"net"
	"net/netip"
	"strings"
	"testing"

	"golang.org/x/net/dns/dnsmessage"
)

func TestBuildTenantDNSSnapshotAddsRouterAndPeerRecords(t *testing.T) {
	snap := buildTenantDNSSnapshot("Tenant Alpha", netip.MustParseAddr("10.6.81.33"), []tenantDNSPeerRecord{
		{Name: "Desktop Client", AssignedIP: "10.6.81.36"},
	})

	if got := snap.forward["router.tenant-alpha.wantastic.internal"]; len(got) != 1 || got[0].String() != "10.6.81.33" {
		t.Fatalf("unexpected router record: %#v", got)
	}
	if got := snap.forward["desktop-client"]; len(got) != 1 || got[0].String() != "10.6.81.36" {
		t.Fatalf("unexpected peer short record: %#v", got)
	}
	if got := snap.forward["desktop-client.tenant-alpha.wantastic.internal"]; len(got) != 1 || got[0].String() != "10.6.81.36" {
		t.Fatalf("unexpected peer fqdn record: %#v", got)
	}
	if got := snap.reverse["36.81.6.10.in-addr.arpa"]; got != "desktop-client.tenant-alpha.wantastic.internal" {
		t.Fatalf("unexpected ptr record: %q", got)
	}
}

func TestTenantDNSSnapshotAnswersAAndPTR(t *testing.T) {
	snap := buildTenantDNSSnapshot("tenant-1", netip.MustParseAddr("10.6.81.33"), []tenantDNSPeerRecord{
		{Name: "peer 1", AssignedIP: "10.6.81.36"},
	})

	aName := mustDNSName("peer-1.tenant-1.wantastic.internal")
	rcode, answers := snap.answer(dnsmessage.Question{Name: aName, Type: dnsmessage.TypeA, Class: dnsmessage.ClassINET})
	if rcode != dnsmessage.RCodeSuccess || len(answers) != 1 || answers[0].a.String() != "10.6.81.36" {
		t.Fatalf("unexpected A answer: rcode=%v answers=%#v", rcode, answers)
	}

	ptrName := mustDNSName("36.81.6.10.in-addr.arpa")
	rcode, answers = snap.answer(dnsmessage.Question{Name: ptrName, Type: dnsmessage.TypePTR, Class: dnsmessage.ClassINET})
	if rcode != dnsmessage.RCodeSuccess || len(answers) != 1 || answers[0].ptr != "peer-1.tenant-1.wantastic.internal" {
		t.Fatalf("unexpected PTR answer: rcode=%v answers=%#v", rcode, answers)
	}
}

func TestBuildWireGuardConfigIncludesDNS(t *testing.T) {
	cfg := BuildWireGuardConfig(WireGuardConfigOptions{
		PrivateKey:          "priv",
		Address:             "10.6.81.36/32",
		ServerPublicKey:     "pub",
		Endpoint:            "wg.wantastic.app:51820",
		AllowedIPs:          []string{"10.6.81.32/27"},
		DNSServers:          WireGuardDNSServers("10.6.81.33"),
		PersistentKeepalive: 25,
		MTU:                 1420,
	})

	if !strings.Contains(cfg, "DNS = 10.6.81.33, 1.1.1.1\n") {
		t.Fatalf("expected DNS line in config, got:\n%s", cfg)
	}
}

func TestWireGuardDNSServersAddsPublicFallback(t *testing.T) {
	got := WireGuardDNSServers("10.6.81.33")
	want := []string{"10.6.81.33", "1.1.1.1"}
	if len(got) != len(want) {
		t.Fatalf("WireGuardDNSServers() len = %d, want %d", len(got), len(want))
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("WireGuardDNSServers()[%d] = %q, want %q", i, got[i], want[i])
		}
	}
}

func TestNewTenantDNSUpstreamResolverUsesCloudflare(t *testing.T) {
	resolver, ok := newTenantDNSUpstreamResolver().(*net.Resolver)
	if !ok {
		t.Fatal("expected *net.Resolver upstream implementation")
	}
	if !resolver.PreferGo {
		t.Fatal("expected PreferGo resolver for explicit upstream dialing")
	}
	if resolver.Dial == nil {
		t.Fatal("expected custom Dial function for Cloudflare upstream")
	}
}

func TestWireGuardAllowedIPsUsesTenantRoutes(t *testing.T) {
	got, err := WireGuardAllowedIPs([]string{"10.6.81.32/27", " 10.6.81.32/27 ", "", "10.6.81.64/27"})
	if err != nil {
		t.Fatalf("WireGuardAllowedIPs() error = %v", err)
	}
	want := []string{"10.6.81.32/27", "10.6.81.64/27"}
	if len(got) != len(want) {
		t.Fatalf("WireGuardAllowedIPs() len = %d, want %d", len(got), len(want))
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("WireGuardAllowedIPs()[%d] = %q, want %q", i, got[i], want[i])
		}
	}
}

func TestBuildTenantDNSSnapshotDedupesDuplicateLabels(t *testing.T) {
	snap := buildTenantDNSSnapshot("tenant-1", netip.MustParseAddr("10.6.81.33"), []tenantDNSPeerRecord{
		{Name: "peer-10-6-81-36", AssignedIP: "10.6.81.36"},
	})

	records := snap.forward["peer-10-6-81-36"]
	if len(records) != 1 {
		t.Fatalf("expected one deduped record, got %#v", records)
	}
}

func TestTenantDNSServerResolveQuestionFallsBackUpstream(t *testing.T) {
	snap := buildTenantDNSSnapshot("tenant-1", netip.MustParseAddr("10.6.81.33"), nil)
	srv := &tenantDNSServer{
		resolver: stubTenantDNSResolver{
			lookupNetIP: func(_ context.Context, network, host string) ([]netip.Addr, error) {
				if network != "ip4" {
					t.Fatalf("expected ip4 network, got %q", network)
				}
				if host != "example.com" {
					t.Fatalf("expected example.com lookup, got %q", host)
				}
				return []netip.Addr{netip.MustParseAddr("93.184.216.34")}, nil
			},
		},
	}

	rcode, authoritative, recursionAvailable, answers := srv.resolveQuestion(snap, dnsmessage.Question{
		Name:  mustDNSName("example.com"),
		Type:  dnsmessage.TypeA,
		Class: dnsmessage.ClassINET,
	})
	if rcode != dnsmessage.RCodeSuccess {
		t.Fatalf("expected success rcode, got %v", rcode)
	}
	if authoritative {
		t.Fatal("expected upstream answer to be non-authoritative")
	}
	if !recursionAvailable {
		t.Fatal("expected recursionAvailable=true when upstream resolver exists")
	}
	if len(answers) != 1 || answers[0].a.String() != "93.184.216.34" {
		t.Fatalf("unexpected upstream answers: %#v", answers)
	}
}

func TestTenantDNSServerResolveQuestionKeepsTenantZoneAuthoritative(t *testing.T) {
	snap := buildTenantDNSSnapshot("tenant-1", netip.MustParseAddr("10.6.81.33"), nil)
	resolverCalled := false
	srv := &tenantDNSServer{
		resolver: stubTenantDNSResolver{
			lookupNetIP: func(_ context.Context, _, _ string) ([]netip.Addr, error) {
				resolverCalled = true
				return nil, errors.New("should not be called for tenant zone")
			},
		},
	}

	rcode, authoritative, _, answers := srv.resolveQuestion(snap, dnsmessage.Question{
		Name:  mustDNSName("missing.tenant-1.wantastic.internal"),
		Type:  dnsmessage.TypeA,
		Class: dnsmessage.ClassINET,
	})
	if resolverCalled {
		t.Fatal("expected tenant-zone miss to stay local and skip upstream lookup")
	}
	if rcode != dnsmessage.RCodeNameError {
		t.Fatalf("expected NXDOMAIN for tenant-zone miss, got %v", rcode)
	}
	if !authoritative {
		t.Fatal("expected tenant-zone miss to remain authoritative")
	}
	if len(answers) != 0 {
		t.Fatalf("expected no answers, got %#v", answers)
	}
}

func TestTenantDNSPeerIndexLifecycle(t *testing.T) {
	s := &Server{
		tenantDNSPeers: make(map[string]map[string]tenantDNSPeerRecord),
	}

	peerA := &PeerMetadata{AccountID: "acc-1", ID: "peer-a", Name: "alpha", AssignedIP: "10.6.81.36"}
	peerB := &PeerMetadata{AccountID: "acc-1", ID: "peer-b", Name: "beta", AssignedIP: "10.6.81.37"}

	s.upsertTenantDNSPeer(peerA)
	s.upsertTenantDNSPeer(peerB)

	records := s.listTenantDNSPeers("acc-1")
	if len(records) != 2 {
		t.Fatalf("expected two indexed peers, got %#v", records)
	}

	s.removeTenantDNSPeer("acc-1", "peer-a")
	records = s.listTenantDNSPeers("acc-1")
	if len(records) != 1 || records[0].PeerID != "peer-b" {
		t.Fatalf("expected remaining peer-b, got %#v", records)
	}

	s.resetTenantDNSPeers("acc-1", []*PeerMetadata{peerA})
	records = s.listTenantDNSPeers("acc-1")
	if len(records) != 1 || records[0].PeerID != "peer-a" {
		t.Fatalf("expected reset to peer-a, got %#v", records)
	}

	s.clearTenantDNSPeers("acc-1")
	if records := s.listTenantDNSPeers("acc-1"); len(records) != 0 {
		t.Fatalf("expected cleared index, got %#v", records)
	}
}

type stubTenantDNSResolver struct {
	lookupNetIP func(ctx context.Context, network, host string) ([]netip.Addr, error)
	lookupAddr  func(ctx context.Context, addr string) ([]string, error)
}

func (s stubTenantDNSResolver) LookupNetIP(ctx context.Context, network, host string) ([]netip.Addr, error) {
	if s.lookupNetIP == nil {
		return nil, errors.New("unexpected LookupNetIP call")
	}
	return s.lookupNetIP(ctx, network, host)
}

func (s stubTenantDNSResolver) LookupAddr(ctx context.Context, addr string) ([]string, error) {
	if s.lookupAddr == nil {
		return nil, errors.New("unexpected LookupAddr call")
	}
	return s.lookupAddr(ctx, addr)
}
