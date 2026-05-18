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
