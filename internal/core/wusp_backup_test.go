package core

import (
	"context"
	"testing"
	"time"

	"WantasticCore/internal/store"
	pb "WantasticCore/internal/types"
)

// mockRedis implements RedisClient for testing
type mockRedis struct {
	store map[string]string
}

func newMockRedis() *mockRedis { return &mockRedis{store: make(map[string]string)} }

func (m *mockRedis) Set(_ context.Context, key string, value interface{}, _ time.Duration) error {
	m.store[key] = value.(string)
	return nil
}
func (m *mockRedis) Get(_ context.Context, key string) (string, error) {
	v, ok := m.store[key]
	if !ok {
		return "", nil
	}
	return v, nil
}
func (m *mockRedis) Del(_ context.Context, keys ...string) error {
	for _, k := range keys {
		delete(m.store, k)
	}
	return nil
}

func TestGenerateBackupToken(t *testing.T) {
	svc := &WUSPService{}
	redis := newMockRedis()
	svc.SetRedis(redis)

	resp, err := svc.GenerateBackupToken(context.Background(), &pb.GenerateBackupTokenRequest{
		PeerId:    "test-peer-123",
		AccountId: "test-account-456",
	})
	if err != nil {
		t.Fatalf("GenerateBackupToken failed: %v", err)
	}
	if !resp.Success {
		t.Fatalf("expected success, got error: %s", resp.Error)
	}
	if resp.UploadToken == "" {
		t.Fatal("expected non-empty token")
	}
	if len(resp.UploadToken) != 64 { // 32 bytes hex
		t.Fatalf("expected 64-char hex token, got %d chars", len(resp.UploadToken))
	}
	if resp.UploadUrl == "" {
		t.Fatal("expected non-empty URL")
	}

	// Verify token is stored in Redis and linked to the peer
	key := backupTokenKey(resp.UploadToken)
	val, _ := redis.Get(context.Background(), key)
	if val != "test-account-456:test-peer-123" {
		t.Fatalf("Redis value mismatch: got %q, want %q", val, "test-account-456:test-peer-123")
	}
	peerVal, _ := redis.Get(context.Background(), peerBackupTokenKey("test-account-456", "test-peer-123"))
	if peerVal != resp.UploadToken {
		t.Fatalf("peer token mismatch: got %q, want %q", peerVal, resp.UploadToken)
	}

	t.Logf("Token: %s", resp.UploadToken)
	t.Logf("URL: %s", resp.UploadUrl)
}

func TestGenerateBackupToken_MissingFields(t *testing.T) {
	svc := &WUSPService{}
	redis := newMockRedis()
	svc.SetRedis(redis)

	resp, err := svc.GenerateBackupToken(context.Background(), &pb.GenerateBackupTokenRequest{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.Success {
		t.Fatal("expected failure for missing fields")
	}
}

func TestBackupTokenStableForScheduledBackups(t *testing.T) {
	svc := &WUSPService{}
	redis := newMockRedis()
	svc.SetRedis(redis)

	// Generate token
	genResp, _ := svc.GenerateBackupToken(context.Background(), &pb.GenerateBackupTokenRequest{
		PeerId:    "peer-1",
		AccountId: "acct-1",
	})

	token := genResp.UploadToken

	// Simulate upload lookup — token should remain valid for repeated scheduled
	// backups on the deployed MikroTik.
	key := backupTokenKey(token)
	val, _ := redis.Get(context.Background(), key)
	if val == "" {
		t.Fatal("token should exist in Redis for backup validation")
	}
	val, _ = redis.Get(context.Background(), key)
	if val == "" {
		t.Fatal("token should still exist after lookup for repeated backups")
	}
}

func TestGenerateBackupTokenRotatesPreviousPeerToken(t *testing.T) {
	svc := &WUSPService{}
	redis := newMockRedis()
	svc.SetRedis(redis)

	first, err := svc.GenerateBackupToken(context.Background(), &pb.GenerateBackupTokenRequest{
		PeerId:    "peer-rotate",
		AccountId: "acct-rotate",
	})
	if err != nil {
		t.Fatalf("first GenerateBackupToken failed: %v", err)
	}
	second, err := svc.GenerateBackupToken(context.Background(), &pb.GenerateBackupTokenRequest{
		PeerId:    "peer-rotate",
		AccountId: "acct-rotate",
	})
	if err != nil {
		t.Fatalf("second GenerateBackupToken failed: %v", err)
	}
	if first.UploadToken == second.UploadToken {
		t.Fatal("expected rotated token to differ from the previous token")
	}
	oldVal, _ := redis.Get(context.Background(), backupTokenKey(first.UploadToken))
	if oldVal != "" {
		t.Fatalf("old token should be revoked after rotation, got %q", oldVal)
	}
	currentVal, _ := redis.Get(context.Background(), peerBackupTokenKey("acct-rotate", "peer-rotate"))
	if currentVal != second.UploadToken {
		t.Fatalf("current peer token mismatch: got %q want %q", currentVal, second.UploadToken)
	}
}

func TestWUSPLiveStateCacheRoundTrip(t *testing.T) {
	svc := &WUSPService{}
	redis := newMockRedis()
	svc.SetRedis(redis)

	want := &store.WUSPDeviceStateData{
		PeerID:         "peer-cache",
		AccountID:      "acct-cache",
		LastSyncAt:     time.Unix(100, 0),
		DeviceSnapshot: []byte(`[{"path":"Device.DeviceInfo.Manufacturer","value":"Wantastic"}]`),
		Manufacturer:   "Wantastic",
		WUSPStatus:     "Active",
	}
	svc.cacheDeviceState(context.Background(), want)

	got, ok := svc.getCachedDeviceState(context.Background(), "acct-cache", "peer-cache")
	if !ok {
		t.Fatal("expected cached state")
	}
	if got.AccountID != want.AccountID || got.PeerID != want.PeerID || got.Manufacturer != want.Manufacturer {
		t.Fatalf("cached state mismatch: got %+v want %+v", got, want)
	}
	if _, ok := svc.getCachedDeviceState(context.Background(), "other-account", "peer-cache"); ok {
		t.Fatal("cache should not return a peer under a different account key")
	}
}

func TestWUSPLiveStateCacheSensitivePathFilter(t *testing.T) {
	for _, path := range []string{
		"Device.Cellular.AccessPoint.1.Username",
		"Device.WiFi.AccessPoint.1.Security.KeyPassphrase",
		"Device.WUSP.Certificate.1.Value",
		"Device.LocalAgent.Controller.1.Password",
	} {
		if isSafeWUSPCachePath(path) {
			t.Fatalf("expected sensitive path to be excluded from Redis overlay cache: %s", path)
		}
	}
	if !isSafeWUSPCachePath("Device.Cellular.Interface.1.RSRP") {
		t.Fatal("expected telemetry path to be safe for Redis overlay cache")
	}
}
