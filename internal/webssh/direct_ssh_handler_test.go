package webssh

import (
	"context"
	"testing"
	"time"

	"WantasticCore/internal/cache"
	"WantasticCore/internal/store"
)

type websshTestPeerStore struct {
	store.PeerRepository
	byPeerID  map[string][]*store.WebSSHSessionData
	byAccount map[string][]*store.WebSSHSessionData
}

func (s *websshTestPeerStore) ListWebSSHSessions(accountID, peerID string) ([]*store.WebSSHSessionData, error) {
	return s.byPeerID[accountID+"|"+peerID], nil
}

func (s *websshTestPeerStore) ListAllWebSSHSessions(accountID string) ([]*store.WebSSHSessionData, error) {
	return s.byAccount[accountID], nil
}

func TestHasExplicitSSHAuthMaterial(t *testing.T) {
	tests := []struct {
		name               string
		password           string
		privateKey         string
		privateKeyPassphra string
		want               bool
	}{
		{
			name: "empty auth",
			want: false,
		},
		{
			name:     "password provided",
			password: "secret",
			want:     true,
		},
		{
			name:       "private key provided",
			privateKey: "-----BEGIN OPENSSH PRIVATE KEY-----",
			want:       true,
		},
		{
			name:               "private key passphrase provided",
			privateKeyPassphra: "hunter2",
			want:               true,
		},
		{
			name:       "whitespace only is ignored",
			password:   "   ",
			privateKey: "\n\t",
			want:       false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := hasExplicitSSHAuthMaterial(tt.password, tt.privateKey, tt.privateKeyPassphra)
			if got != tt.want {
				t.Fatalf("hasExplicitSSHAuthMaterial() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestDescribeSSHAuthProfile(t *testing.T) {
	tests := []struct {
		name        string
		password    string
		privateKey  string
		passphrase  string
		interactive bool
		want        string
	}{
		{
			name: "none",
			want: "none",
		},
		{
			name:        "password and interactive",
			password:    "secret",
			interactive: true,
			want:        "password,interactive",
		},
		{
			name:       "public key",
			privateKey: "-----BEGIN OPENSSH PRIVATE KEY-----",
			want:       "publickey",
		},
		{
			name:       "encrypted public key",
			privateKey: "-----BEGIN OPENSSH PRIVATE KEY-----",
			passphrase: "hunter2",
			want:       "publickey+passphrase",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := describeSSHAuthProfile(tt.password, tt.privateKey, tt.passphrase, tt.interactive)
			if got != tt.want {
				t.Fatalf("describeSSHAuthProfile() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestResolveAuthoritativeSessionPrefersNewerStoredAuth(t *testing.T) {
	now := time.Now()
	stale := &DirectSSHSession{
		ID:         "stale",
		TenantID:   "account-1",
		PeerID:     "peer-1",
		PeerIP:     "10.0.0.2",
		Port:       22,
		Username:   "root",
		LastActive: now.Add(-5 * time.Minute),
		StartedAt:  now.Add(-10 * time.Minute),
		Status:     SessionStatusDisconnected,
	}
	fresh := &DirectSSHSession{
		ID:         "fresh",
		TenantID:   "account-1",
		PeerID:     "peer-1",
		PeerIP:     "10.0.0.2",
		Port:       22,
		Username:   "root",
		Password:   "super-secret",
		LastActive: now,
		StartedAt:  now.Add(-time.Minute),
		Status:     SessionStatusDisconnected,
	}

	handler := &DirectSSHHandler{
		sessions: map[string]*DirectSSHSession{
			stale.ID: stale,
			fresh.ID: fresh,
		},
		sessionCache: cache.NewCacheForType(cache.TypeSession),
		peerStore: &websshTestPeerStore{
			byPeerID: map[string][]*store.WebSSHSessionData{
				"account-1|peer-1": {
					{ID: stale.ID, PeerID: stale.PeerID, AccountID: stale.TenantID, PeerIP: stale.PeerIP, Port: stale.Port, Name: "root@10.0.0.2:22"},
					{ID: fresh.ID, PeerID: fresh.PeerID, AccountID: fresh.TenantID, PeerIP: fresh.PeerIP, Port: fresh.Port, Name: "root@10.0.0.2:22"},
				},
			},
		},
	}

	resolved := handler.resolveAuthoritativeSession(stale)
	if resolved == nil {
		t.Fatal("resolveAuthoritativeSession returned nil")
	}
	if resolved.ID != fresh.ID {
		t.Fatalf("resolveAuthoritativeSession() resolved %q, want %q", resolved.ID, fresh.ID)
	}
}

func TestGetActiveSessionCountCountsOnlyLiveSessions(t *testing.T) {
	handler := &DirectSSHHandler{
		sessions: map[string]*DirectSSHSession{
			"active":       {ID: "active", Status: SessionStatusActive},
			"disconnected": {ID: "disconnected", Status: SessionStatusDisconnected},
			"error":        {ID: "error", Status: SessionStatusError},
		},
	}

	if got := handler.GetActiveSessionCount(); got != 1 {
		t.Fatalf("GetActiveSessionCount() = %d, want 1", got)
	}
}

func TestShouldReuseExistingSessionRequiresStoredAuthWhenRequested(t *testing.T) {
	passwordless := &DirectSSHSession{
		ID:       "prompt-only",
		TenantID: "account-1",
		PeerID:   "peer-1",
		PeerIP:   "10.0.0.2",
		Port:     22,
		Username: "root",
	}
	withPassword := &DirectSSHSession{
		ID:       "with-password",
		TenantID: "account-1",
		PeerID:   "peer-1",
		PeerIP:   "10.0.0.2",
		Port:     22,
		Username: "root",
		Password: "secret",
	}

	if shouldReuseExistingSession(passwordless, true) {
		t.Fatal("passwordless session unexpectedly reusable when stored auth is required")
	}
	if !shouldReuseExistingSession(withPassword, true) {
		t.Fatal("stored-auth session should be reusable when stored auth is required")
	}
	if !shouldReuseExistingSession(passwordless, false) {
		t.Fatal("passwordless session should remain reusable when stored auth is not required")
	}
}

func TestFindReusableInMemorySessionLockedPrefersStoredAuthForSameIdentity(t *testing.T) {
	now := time.Now()
	passwordless := &DirectSSHSession{
		ID:         "prompt-only",
		TenantID:   "account-1",
		PeerID:     "peer-1",
		PeerIP:     "10.0.0.2",
		Port:       22,
		Username:   "root",
		LastActive: now.Add(-time.Minute),
		StartedAt:  now.Add(-2 * time.Minute),
	}
	withPassword := &DirectSSHSession{
		ID:         "with-password",
		TenantID:   "account-1",
		PeerID:     "peer-1",
		PeerIP:     "10.0.0.2",
		Port:       22,
		Username:   "root",
		Password:   "secret",
		LastActive: now,
		StartedAt:  now.Add(-30 * time.Second),
	}

	handler := &DirectSSHHandler{
		sessions:     make(map[string]*DirectSSHSession),
		sessionIndex: make(map[string]string),
		sessionCache: cache.NewCacheForType(cache.TypeSession),
	}

	handler.registerSessionLocked(passwordless)
	handler.registerSessionLocked(withPassword)

	got := handler.findReusableInMemorySessionLocked("account-1", "peer-1", "10.0.0.2", 22, "root", true)
	if got == nil {
		t.Fatal("findReusableInMemorySessionLocked returned nil")
	}
	if got.ID != withPassword.ID {
		t.Fatalf("findReusableInMemorySessionLocked() = %q, want %q", got.ID, withPassword.ID)
	}
}

func TestLogActivityStartPrefersSessionUserAgent(t *testing.T) {
	var captured SSHActivityData
	handler := &DirectSSHHandler{
		sessions: map[string]*DirectSSHSession{
			"session-1": {
				ID:        "session-1",
				TenantID:  "account-1",
				PeerID:    "peer-1",
				Username:  "root",
				UserAgent: "Mozilla/5.0",
			},
		},
		logActivityFunc: func(_, _ string, activity SSHActivityData) error {
			captured = activity
			return nil
		},
	}

	handler.LogActivityStart("session-1", "192.0.2.10", "grpc-go/1.76.0")

	if captured.UserAgent != "Mozilla/5.0" {
		t.Fatalf("LogActivityStart() logged user agent %q, want browser user agent", captured.UserAgent)
	}
	if captured.ClientIP != "192.0.2.10" {
		t.Fatalf("LogActivityStart() logged client IP %q, want %q", captured.ClientIP, "192.0.2.10")
	}
	if captured.Timestamp.IsZero() {
		t.Fatal("LogActivityStart() did not set timestamp")
	}
}

func TestDisconnectSessionLogsEndFromStreamStart(t *testing.T) {
	streamStartedAt := time.Now().Add(-2 * time.Minute).Round(0)
	sessionStartedAt := time.Now().Add(-30 * time.Minute).Round(0)

	session := &DirectSSHSession{
		ID:              "session-1",
		TenantID:        "account-1",
		PeerID:          "peer-1",
		PeerIP:          "10.0.0.2",
		Port:            22,
		Username:        "root",
		UserAgent:       "Mozilla/5.0",
		ClientIP:        "192.0.2.10",
		StartedAt:       sessionStartedAt,
		StreamStartedAt: streamStartedAt,
		BytesSent:       123,
		BytesRecv:       456,
		ReceivedCommands: []SSHCommandEntry{
			{Command: "ls", Timestamp: streamStartedAt.Add(5 * time.Second)},
		},
	}
	session.ctx, session.cancel = context.WithCancel(context.Background())

	var captured SSHActivityData
	handler := &DirectSSHHandler{
		sessions:     make(map[string]*DirectSSHSession),
		sessionIndex: make(map[string]string),
		sessionCache: cache.NewCacheForType(cache.TypeSession),
		logActivityFunc: func(_, _ string, activity SSHActivityData) error {
			captured = activity
			return nil
		},
	}
	handler.registerSessionLocked(session)

	if err := handler.DisconnectSession(session.ID); err != nil {
		t.Fatalf("DisconnectSession() error = %v", err)
	}

	if !captured.Timestamp.Equal(streamStartedAt) {
		t.Fatalf("DisconnectSession() logged timestamp %v, want stream start %v", captured.Timestamp, streamStartedAt)
	}
	if captured.UserAgent != session.UserAgent {
		t.Fatalf("DisconnectSession() logged user agent %q, want %q", captured.UserAgent, session.UserAgent)
	}
	if captured.ClientIP != session.ClientIP {
		t.Fatalf("DisconnectSession() logged client IP %q, want %q", captured.ClientIP, session.ClientIP)
	}
	if captured.BytesSent != session.BytesSent || captured.BytesRecv != session.BytesRecv {
		t.Fatalf("DisconnectSession() logged bytes sent/recv %d/%d, want %d/%d", captured.BytesSent, captured.BytesRecv, session.BytesSent, session.BytesRecv)
	}
	if captured.EndTime.IsZero() {
		t.Fatal("DisconnectSession() did not set end time")
	}
	if captured.DurationMs <= 0 {
		t.Fatalf("DisconnectSession() logged duration %d, want positive value", captured.DurationMs)
	}
	if len(captured.Commands) != 1 || captured.Commands[0].Command != "ls" {
		t.Fatalf("DisconnectSession() logged commands %+v, want captured session commands", captured.Commands)
	}
	if _, exists := handler.sessions[session.ID]; exists {
		t.Fatal("DisconnectSession() did not unregister session")
	}
}
