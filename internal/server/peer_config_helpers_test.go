package server

import "testing"

func TestFormatPeerEndpointAddsPortWhenMissing(t *testing.T) {
	got := formatPeerEndpoint("wg.wantastic.app", 51820)
	if got != "wg.wantastic.app:51820" {
		t.Fatalf("unexpected endpoint: %q", got)
	}
}

func TestFormatPeerEndpointPreservesExistingPort(t *testing.T) {
	got := formatPeerEndpoint("wg.wantastic.app:51820", 51820)
	if got != "wg.wantastic.app:51820" {
		t.Fatalf("unexpected endpoint: %q", got)
	}
}

func TestFormatPeerEndpointWrapsBareIPv6(t *testing.T) {
	got := formatPeerEndpoint("2001:db8::1", 51820)
	if got != "[2001:db8::1]:51820" {
		t.Fatalf("unexpected endpoint: %q", got)
	}
}
