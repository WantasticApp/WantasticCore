package userspace

import (
	"net"
	"net/netip"
	"testing"
	"unsafe"

	"WantasticCore/internal/wg/userspace/wireguard-go/tun"
	"WantasticCore/internal/wg/userspace/wireguard-go/tun/netstack"

	"gvisor.dev/gvisor/pkg/buffer"
	"gvisor.dev/gvisor/pkg/tcpip"
	"gvisor.dev/gvisor/pkg/tcpip/link/channel"
	"gvisor.dev/gvisor/pkg/tcpip/network/ipv4"
	"gvisor.dev/gvisor/pkg/tcpip/stack"
)

func TestBuildTenantSubnetRouteIsOnLink(t *testing.T) {
	_, subnetNet, err := net.ParseCIDR("10.6.81.32/27")
	if err != nil {
		t.Fatalf("parse cidr: %v", err)
	}

	route, err := buildTenantSubnetRoute(subnetNet)
	if err != nil {
		t.Fatalf("build route: %v", err)
	}

	if route.NIC != 1 {
		t.Fatalf("expected NIC 1, got %d", route.NIC)
	}
	if route.Gateway.BitLen() != 0 {
		t.Fatalf("expected on-link route with empty gateway, got %q", route.Gateway)
	}
}

func TestServerAddrsFromSubnetsUsesFirstUsableAddressPerBlock(t *testing.T) {
	got, err := serverAddrsFromSubnets([]string{"10.6.81.32/27", "10.6.81.64/27"})
	if err != nil {
		t.Fatalf("serverAddrsFromSubnets() error = %v", err)
	}

	want := []netip.Addr{
		netip.MustParseAddr("10.6.81.33"),
		netip.MustParseAddr("10.6.81.65"),
	}
	if len(got) != len(want) {
		t.Fatalf("serverAddrsFromSubnets() len = %d, want %d", len(got), len(want))
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("serverAddrsFromSubnets()[%d] = %s, want %s", i, got[i], want[i])
		}
	}
}

func TestEnableIPForwardingEnablesForwardingWithoutPromiscuousMode(t *testing.T) {
	tunDev, tnet, err := netstack.CreateNetTUN(
		[]netip.Addr{netip.MustParseAddr("10.6.81.33")},
		nil,
		1420,
	)
	if err != nil {
		t.Fatalf("CreateNetTUN() error = %v", err)
	}
	defer tunDev.Close()

	type netTunAccess struct {
		ep             *channel.Endpoint
		stack          *stack.Stack
		events         chan tun.Event
		notifyHandle   *channel.NotificationHandle
		incomingPacket chan *buffer.View
		mtu            int
		dnsServers     []netip.Addr
		hasV4, hasV6   bool
	}

	netAccess := (*netTunAccess)(unsafe.Pointer(tnet))
	if netAccess.stack == nil {
		t.Fatal("expected netstack stack to be initialized")
	}

	if err := enableIPForwarding((*netstack.Net)(tnet), []string{"10.6.81.32/27"}, netip.MustParseAddr("10.6.81.33")); err != nil {
		t.Fatalf("enableIPForwarding() error = %v", err)
	}

	nicInfo, ok := netAccess.stack.NICInfo()[tcpip.NICID(1)]
	if !ok {
		t.Fatal("expected NIC 1 to exist")
	}
	if !nicInfo.Forwarding[ipv4.ProtocolNumber] {
		t.Fatal("expected IPv4 forwarding to be enabled")
	}
}

func TestApplyACLRulesInstallsFiltersOnInputAndForward(t *testing.T) {
	tunDev, tnet, err := netstack.CreateNetTUN(
		[]netip.Addr{netip.MustParseAddr("10.6.81.33")},
		nil,
		1420,
	)
	if err != nil {
		t.Fatalf("CreateNetTUN() error = %v", err)
	}
	defer tunDev.Close()

	td := &TenantDevice{
		TenantID: "tenant-test",
		Net:      (*netstack.Net)(tnet),
		DeviceIP: netip.MustParseAddr("10.6.81.33"),
		Subnets:  []string{"10.6.81.32/27"},
	}

	if err := td.ApplyACLRules([]ACLRule{{
		RuleID:   "rule-1",
		Protocol: "tcp",
		SourceIP: "10.6.81.34/32",
		DestIP:   "10.6.81.35/32",
		Action:   "allow",
	}}); err != nil {
		t.Fatalf("ApplyACLRules() error = %v", err)
	}

	type netTunAccess struct {
		ep             *channel.Endpoint
		stack          *stack.Stack
		events         chan tun.Event
		notifyHandle   *channel.NotificationHandle
		incomingPacket chan *buffer.View
		mtu            int
		dnsServers     []netip.Addr
		hasV4, hasV6   bool
	}
	netAccess := (*netTunAccess)(unsafe.Pointer(tnet))
	filterTable := netAccess.stack.IPTables().GetTable(stack.FilterID, false)

	var inputFound bool
	var forwardFound bool
	for _, rule := range filterTable.Rules {
		if _, ok := rule.Target.(*aclRuleFilter); !ok {
			continue
		}
		if !inputFound {
			inputFound = true
			continue
		}
		forwardFound = true
		break
	}

	if !inputFound || !forwardFound {
		t.Fatalf("expected aclRuleFilter to be installed on both INPUT and FORWARD chains, got input=%v forward=%v", inputFound, forwardFound)
	}
}
