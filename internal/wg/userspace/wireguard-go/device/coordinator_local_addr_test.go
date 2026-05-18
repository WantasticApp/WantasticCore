package device

import (
	"net"
	"net/netip"
	"testing"
)

func TestShouldAdvertiseLocalAddrRejectsAssignedOverlayIP(t *testing.T) {
	localAddr := net.UDPAddr{
		IP:   net.ParseIP("172.25.0.11"),
		Port: 48066,
	}
	assignedIP := netip.MustParseAddr("172.25.0.11")

	if shouldAdvertiseLocalAddr(localAddr, assignedIP) {
		t.Fatalf("expected assigned overlay IP to be rejected as a punch candidate")
	}
}

func TestShouldAdvertiseLocalAddrAcceptsDistinctUsableLANIP(t *testing.T) {
	localAddr := net.UDPAddr{
		IP:   net.ParseIP("192.168.97.3"),
		Port: 36549,
	}
	assignedIP := netip.MustParseAddr("172.25.0.11")

	if !shouldAdvertiseLocalAddr(localAddr, assignedIP) {
		t.Fatalf("expected distinct usable LAN IP to be advertised")
	}
}
