package server

import (
	"errors"
	"reflect"
	"testing"
	"time"

	"WantasticCore/internal/account"
)

func TestPeerAllowedIPsFallsBackToAssignedIP(t *testing.T) {
	peer := &PeerMetadata{AssignedIP: "10.6.81.34"}

	got := peerAllowedIPs(peer)
	want := []string{"10.6.81.34/32"}

	if !reflect.DeepEqual(got, want) {
		t.Fatalf("expected %v, got %v", want, got)
	}
}

func TestPeerAllowedIPsPrefersStoredAllowedIPs(t *testing.T) {
	peer := &PeerMetadata{
		AssignedIP: "10.6.81.34",
		AllowedIPs: []string{"10.6.81.34/32", "10.6.81.64/27"},
	}

	got := peerAllowedIPs(peer)
	if !reflect.DeepEqual(got, peer.AllowedIPs) {
		t.Fatalf("expected stored allowed IPs %v, got %v", peer.AllowedIPs, got)
	}
}

func TestShouldIgnoreRuntimePeerRemoval(t *testing.T) {
	if !shouldIgnoreRuntimePeerRemoval(errors.New("tenant abc not found")) {
		t.Fatal("expected missing tenant removal error to be ignored")
	}
	if !shouldIgnoreRuntimePeerRemoval(errors.New("device is closed")) {
		t.Fatal("expected closed device removal error to be ignored")
	}
	if shouldIgnoreRuntimePeerRemoval(errors.New("permission denied")) {
		t.Fatal("did not expect unrelated removal error to be ignored")
	}
}

func TestTenantDevicePeerLimitUsesMaxPeers(t *testing.T) {
	acc := &account.Account{
		MaxPeers:   25,
		BlockCount: 1,
		CreatedAt:  time.Date(2026, time.April, 1, 0, 0, 0, 0, time.UTC),
	}

	if got := tenantDevicePeerLimit(acc); got != 25 {
		t.Fatalf("expected override limit 25, got %d", got)
	}
}

func TestTenantDevicePeerLimitFallsBackToBlockCapacity(t *testing.T) {
	acc := &account.Account{
		BlockCount: 1,
		CreatedAt:  time.Date(2026, time.February, 15, 0, 0, 0, 0, time.UTC),
	}

	if got := tenantDevicePeerLimit(acc); got != 29 {
		t.Fatalf("expected block-derived limit 29, got %d", got)
	}
}
