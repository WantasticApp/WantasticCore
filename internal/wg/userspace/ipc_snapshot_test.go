package userspace

import (
	"encoding/base64"
	"encoding/hex"
	"testing"
	"time"
)

func TestParseIPCSnapshotBuildsEndpointIndexAndStats(t *testing.T) {
	peerOneHex := hex.EncodeToString([]byte("0123456789abcdef0123456789abcdef"))
	peerTwoHex := hex.EncodeToString([]byte("fedcba9876543210fedcba9876543210"))

	now := time.Unix(1_700_000_000, 0)
	ipc := "" +
		"public_key=" + peerOneHex + "\n" +
		"last_handshake_time_sec=1699999000\n" +
		"last_handshake_time_nsec=0\n" +
		"last_authenticated_packet_time_sec=1699999990\n" +
		"last_authenticated_packet_time_nsec=0\n" +
		"rx_bytes=11\n" +
		"tx_bytes=22\n" +
		"endpoint=198.51.100.5:51820\n" +
		"allowed_ip=10.0.0.2/32\n" +
		"public_key=" + peerTwoHex + "\n" +
		"last_handshake_time_sec=1699999000\n" +
		"last_handshake_time_nsec=0\n" +
		"last_authenticated_packet_time_sec=1699999000\n" +
		"last_authenticated_packet_time_nsec=0\n" +
		"rx_bytes=33\n" +
		"tx_bytes=44\n" +
		"endpoint=\n" +
		"allowed_ip=10.0.0.3/32\n"

	snapshot := parseIPCSnapshot(ipc, now)

	peerOneKey := base64.StdEncoding.EncodeToString([]byte("0123456789abcdef0123456789abcdef"))
	peerTwoKey := base64.StdEncoding.EncodeToString([]byte("fedcba9876543210fedcba9876543210"))

	if snapshot.peerCount != 2 {
		t.Fatalf("expected 2 peers, got %d", snapshot.peerCount)
	}
	if snapshot.connectedPeers != 1 {
		t.Fatalf("expected 1 connected peer, got %d", snapshot.connectedPeers)
	}
	if got := snapshot.endpointToPeer["198.51.100.5:51820"]; got != peerOneKey {
		t.Fatalf("unexpected endpoint lookup result: %q", got)
	}
	if snapshot.totalRxBytes != 44 {
		t.Fatalf("unexpected total rx bytes: %d", snapshot.totalRxBytes)
	}
	if snapshot.totalTxBytes != 66 {
		t.Fatalf("unexpected total tx bytes: %d", snapshot.totalTxBytes)
	}

	peerOne := snapshot.peers[peerOneKey]
	if peerOne == nil {
		t.Fatalf("expected peer one in snapshot")
	}
	if !peerOne.IsOnline {
		t.Fatalf("expected peer one to be online")
	}
	if !peerOne.LastAuthenticatedPacketTime.Equal(time.Unix(1_699_999_990, 0)) {
		t.Fatalf("unexpected last authenticated packet time: %v", peerOne.LastAuthenticatedPacketTime)
	}
	if peerOne.AssignedIP != "10.0.0.2" {
		t.Fatalf("unexpected assigned IP: %q", peerOne.AssignedIP)
	}

	peerTwo := snapshot.peers[peerTwoKey]
	if peerTwo == nil {
		t.Fatalf("expected peer two in snapshot")
	}
	if peerTwo.IsOnline {
		t.Fatalf("expected peer two to be offline")
	}
}

func TestParseIPCSnapshotTreatsRecentHandshakeAsOnlineWithoutAuthenticatedTraffic(t *testing.T) {
	peerHex := hex.EncodeToString([]byte("aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"))
	now := time.Unix(1_700_000_000, 0)
	ipc := "" +
		"public_key=" + peerHex + "\n" +
		"last_handshake_time_sec=1699999990\n" +
		"last_handshake_time_nsec=0\n" +
		"last_authenticated_packet_time_sec=0\n" +
		"last_authenticated_packet_time_nsec=0\n" +
		"rx_bytes=0\n" +
		"tx_bytes=0\n" +
		"endpoint=198.51.100.9:51820\n" +
		"allowed_ip=10.0.0.9/32\n"

	snapshot := parseIPCSnapshot(ipc, now)
	peerKey := base64.StdEncoding.EncodeToString([]byte("aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"))
	status := snapshot.peers[peerKey]
	if status == nil {
		t.Fatalf("expected peer in snapshot")
	}
	if !status.IsOnline {
		t.Fatalf("expected recent handshake with endpoint to count as online")
	}
	if !status.LastAuthenticatedPacketTime.Equal(time.Unix(1_699_999_990, 0)) {
		t.Fatalf("expected handshake time to backfill recent activity, got %v", status.LastAuthenticatedPacketTime)
	}
}

func TestCopyPeerStatusMapProducesIndependentCopies(t *testing.T) {
	source := map[string]*PeerStatus{
		"peer": {
			PublicKey:  "peer",
			AllowedIPs: []string{"10.0.0.2/32"},
		},
	}

	cloned := copyPeerStatusMap(source)
	cloned["peer"].AllowedIPs[0] = "10.0.0.9/32"

	if source["peer"].AllowedIPs[0] != "10.0.0.2/32" {
		t.Fatalf("expected source AllowedIPs to remain unchanged, got %q", source["peer"].AllowedIPs[0])
	}
}
