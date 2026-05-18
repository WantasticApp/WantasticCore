package core

import (
	"testing"

	"WantasticCore/internal/server"
)

func TestPeerInfoToMetadataUsesPublicKeyIdentity(t *testing.T) {
	svc := &WantasticServiceServer{}
	info := &server.PeerInfo{
		Name:       "device-1",
		PrivateKey: "priv-key",
		PublicKey:  "pub-key",
		AllowedIPs: []string{"10.6.81.34/32"},
	}

	peer := svc.peerInfoToMetadata(info, "acct-1")

	if peer.ID != info.PublicKey {
		t.Fatalf("expected peer ID %q, got %q", info.PublicKey, peer.ID)
	}
	if peer.AssignedIP != "10.6.81.34" {
		t.Fatalf("expected assigned IP to be trimmed, got %q", peer.AssignedIP)
	}
	if peer.PrivateKey != info.PrivateKey {
		t.Fatalf("expected private key to be preserved")
	}
	if peer.WireGuardPublicKey != info.PublicKey {
		t.Fatalf("expected wireguard public key %q, got %q", info.PublicKey, peer.WireGuardPublicKey)
	}
	if peer.WireGuardPrivateKey != info.PrivateKey {
		t.Fatalf("expected wireguard private key to be preserved")
	}
}
