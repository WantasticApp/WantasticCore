package core

import (
	"testing"
	"time"

	pb "WantasticCore/internal/types"
	"WantasticCore/internal/server"
)

func TestRouterOSProbeTargetsPrefersSavedAPIEndpoint(t *testing.T) {
	t.Parallel()

	targets := routerOSProbeTargets(0, false, &server.WinboxSession{
		RouterOSAPIPort: 8729,
		RouterOSAPITLS:  true,
	})

	if len(targets) != 2 {
		t.Fatalf("expected 2 unique probe targets, got %d", len(targets))
	}
	if targets[0].Port != 8729 || !targets[0].UseTLS {
		t.Fatalf("expected saved API endpoint first, got %+v", targets[0])
	}
	if targets[1].Port != 8728 || targets[1].UseTLS {
		t.Fatalf("expected tcp fallback second, got %+v", targets[1])
	}
}

func TestBestRouterOSSessionPrefersVerifiedEnabledSession(t *testing.T) {
	t.Parallel()

	sessions := []server.WinboxSession{
		{ID: "plain", Enabled: true},
		{ID: "verified", Enabled: true, RouterOSAPIVerified: true, RouterOSAPIPort: 8728},
		{ID: "disabled-verified", Enabled: false, RouterOSAPIVerified: true, RouterOSAPIPort: 8729},
	}

	best := bestRouterOSSession(sessions)
	if best == nil {
		t.Fatal("expected a session to be selected")
	}
	if best.ID != "verified" {
		t.Fatalf("expected verified session to win, got %q", best.ID)
	}
}

func TestRouterOSCapabilityFromSessionIncludesVerificationState(t *testing.T) {
	t.Parallel()

	validatedAt := time.Now().UTC().Truncate(time.Second)
	capability := routerOSCapabilityFromSession(true, &server.WinboxSession{
		ID:                       "sess-1",
		RouterOSAPIVerified:      true,
		RouterOSAPIPort:          8729,
		RouterOSAPITLS:           true,
		RouterOSAPIError:         "",
		RouterOSAPILastValidated: validatedAt,
	})

	if !capability.Candidate || !capability.ApiReady {
		t.Fatalf("expected candidate and api_ready to be true, got %+v", capability)
	}
	if capability.ApiPort != 8729 || !capability.ApiTls {
		t.Fatalf("unexpected api target: %+v", capability)
	}
	if capability.SessionId != "sess-1" {
		t.Fatalf("expected session id to be propagated, got %q", capability.SessionId)
	}
	if capability.LastValidated == nil || capability.LastValidated.AsTime() != validatedAt {
		t.Fatalf("expected last validated timestamp %v, got %+v", validatedAt, capability.LastValidated)
	}
}

func TestRouterOSPeerFlagsUseVerifiedSessionAndFingerprint(t *testing.T) {
	t.Parallel()

	candidate, ready, port, useTLS := routerOSPeerFlags(
		&server.PeerMetadata{
			WinboxSessions: []server.WinboxSession{
				{ID: "a", Enabled: true, RouterOSAPIVerified: true, RouterOSAPIPort: 8729, RouterOSAPITLS: true},
			},
		},
		&pb.OSFingerprint{Vendor: "MikroTik"},
		[]*pb.OpenPort{{Port: 8291}},
	)

	if !candidate {
		t.Fatal("expected peer to be marked as RouterOS candidate")
	}
	if !ready {
		t.Fatal("expected verified session to mark API ready")
	}
	if port != 8729 || !useTLS {
		t.Fatalf("unexpected API endpoint returned: port=%d tls=%t", port, useTLS)
	}
}

func TestRouterOSPeerFlagsUsePeerLevelVerificationFirst(t *testing.T) {
	t.Parallel()

	candidate, ready, port, useTLS := routerOSPeerFlags(
		&server.PeerMetadata{
			HasWinbox:           true,
			RouterOSAPIVerified: true,
			RouterOSAPIPort:     8728,
			RouterOSAPITLS:      false,
		},
		nil,
		nil,
	)

	if !candidate || !ready {
		t.Fatalf("expected peer-level RouterOS verification to win, got candidate=%t ready=%t", candidate, ready)
	}
	if port != 8728 || useTLS {
		t.Fatalf("unexpected peer-level api endpoint returned: port=%d tls=%t", port, useTLS)
	}
}

// Fingerprint says "not MikroTik" → suppress the dashboard even when
// Winbox port 8291 is open. This is the false-positive case that drove
// the rework: home NATs and unrelated services often expose 8291.
func TestRouterOSPeerFlagsHidesWhenFingerprintSaysOther(t *testing.T) {
	t.Parallel()

	for _, fp := range []*pb.OSFingerprint{
		{Vendor: "Microsoft", OsFamily: "windows"},
		{Vendor: "Apple", OsFamily: "macos"},
		{Vendor: "Linux", OsFamily: "linux"},
		{Vendor: "Cisco"},
	} {
		candidate, ready, _, _ := routerOSPeerFlags(
			&server.PeerMetadata{HasWinbox: true, ScannedWinboxPort: 8291},
			fp,
			[]*pb.OpenPort{{Port: 8291}},
		)
		if candidate {
			t.Fatalf("vendor=%q os=%q: candidate must be false, got true", fp.Vendor, fp.OsFamily)
		}
		if ready {
			t.Fatalf("vendor=%q os=%q: ready must remain false without API verification", fp.Vendor, fp.OsFamily)
		}
	}
}

// Verified API trumps a contradicting fingerprint: if we've actually
// talked the RouterOS API the device IS a RouterOS box, full stop.
func TestRouterOSPeerFlagsApiVerificationOverridesFingerprint(t *testing.T) {
	t.Parallel()

	candidate, ready, port, _ := routerOSPeerFlags(
		&server.PeerMetadata{
			RouterOSAPIVerified: true,
			RouterOSAPIPort:     8728,
		},
		&pb.OSFingerprint{Vendor: "Cisco", OsFamily: "ios"},
		nil,
	)

	if !candidate || !ready || port != 8728 {
		t.Fatalf("API verification must win over contradicting fingerprint, got candidate=%t ready=%t port=%d", candidate, ready, port)
	}
}

// No fingerprint yet + Winbox port detected → keep the affordance on.
// The port-scan-then-fingerprint pipeline has a brief window where
// the operator should still see the dashboard button.
func TestRouterOSPeerFlagsPendingFingerprintKeepsCandidate(t *testing.T) {
	t.Parallel()

	candidate, ready, _, _ := routerOSPeerFlags(
		&server.PeerMetadata{HasWinbox: true, ScannedWinboxPort: 8291},
		nil,
		[]*pb.OpenPort{{Port: 8291}},
	)

	if !candidate {
		t.Fatal("expected candidate=true when fingerprint hasn't landed yet but Winbox port is open")
	}
	if ready {
		t.Fatal("expected ready=false without API verification")
	}
}
