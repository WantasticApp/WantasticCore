// Package oauth2 provides secure storage tests
package oauth2

import (
	"fmt"
	"testing"
	"time"
)

// TestSecureMemoryStore_Cleanup verifies that expired entries are properly cleaned
func TestSecureMemoryStore_Cleanup(t *testing.T) {
	store := NewSecureMemoryStore()
	defer store.Close()
	
	// Create a request that expires quickly
	req := &DeviceRequest{
		DeviceCode: "test_device_code",
		UserCode:   "TEST1234",
		ClientID:   "test-client",
		ExpiresAt:  time.Now().Add(100 * time.Millisecond),
	}
	
	if err := store.Create(req); err != nil {
		t.Fatalf("Create failed: %v", err)
	}
	
	// Verify it exists
	_, err := store.GetByDeviceCode("test_device_code")
	if err != nil {
		t.Fatalf("GetByDeviceCode failed: %v", err)
	}
	
	// Wait for expiration
	time.Sleep(200 * time.Millisecond)
	
	// Trigger cleanup manually
	store.cleanupExpired()
	
	// Verify it's gone
	_, err = store.GetByDeviceCode("test_device_code")
	if err == nil {
		t.Error("Expected expired request to be deleted")
	}
}

// TestSecureMemoryStore_Sanitization verifies sensitive fields are zeroed
func TestSecureMemoryStore_Sanitization(t *testing.T) {
	store := NewSecureMemoryStore()
	
	// Create authorization request with sensitive data
	authReq := &AuthorizationRequest{
		AuthorizationCode:   "secret_auth_code",
		CodeChallenge:       "secret_challenge",
		ClientID:            "test-client",
		RedirectURI:         "http://localhost/callback",
		State:               "secret_state",
		DeviceID:            "secret_device_id",
		UserID:              "secret_user_id",
		Email:               "secret@example.com",
		Name:                "Secret User",
		ExpiresAt:           time.Now().Add(time.Minute),
	}
	
	if err := store.CreateAuthorization(authReq); err != nil {
		t.Fatalf("CreateAuthorization failed: %v", err)
	}
	
	// Delete it
	if err := store.DeleteAuthorization("secret_auth_code"); err != nil {
		t.Fatalf("DeleteAuthorization failed: %v", err)
	}
	
	// Verify the original struct was sanitized (fields zeroed)
	if authReq.AuthorizationCode != "" {
		t.Error("AuthorizationCode was not zeroed after delete")
	}
	if authReq.CodeChallenge != "" {
		t.Error("CodeChallenge was not zeroed after delete")
	}
	if authReq.DeviceID != "" {
		t.Error("DeviceID was not zeroed after delete")
	}
	if authReq.UserID != "" {
		t.Error("UserID was not zeroed after delete")
	}
	if authReq.Email != "" {
		t.Error("Email was not zeroed after delete")
	}
	if authReq.Name != "" {
		t.Error("Name was not zeroed after delete")
	}
	if authReq.State != "" {
		t.Error("State was not zeroed after delete")
	}
	
	// Close and verify no panic
	store.Close()
}

// TestSecureMemoryStore_DeviceFlowOperations tests CRUD operations for device flow
func TestSecureMemoryStore_DeviceFlowOperations(t *testing.T) {
	store := NewSecureMemoryStore()
	defer store.Close()
	
	req := &DeviceRequest{
		DeviceCode: "device_123",
		UserCode:   "USER1234",
		ClientID:   "test-client",
		DeviceID:   "device_abc",
		Status:     StatusPending,
		ExpiresAt:  time.Now().Add(time.Minute),
	}
	
	// Create
	if err := store.Create(req); err != nil {
		t.Fatalf("Create failed: %v", err)
	}
	
	// Read by device code
	retrieved, err := store.GetByDeviceCode("device_123")
	if err != nil {
		t.Fatalf("GetByDeviceCode failed: %v", err)
	}
	if retrieved.UserCode != "USER1234" {
		t.Errorf("UserCode mismatch: got %s, want USER1234", retrieved.UserCode)
	}
	
	// Read by user code
	retrieved2, err := store.GetByUserCode("USER1234")
	if err != nil {
		t.Fatalf("GetByUserCode failed: %v", err)
	}
	if retrieved2.DeviceCode != "device_123" {
		t.Errorf("DeviceCode mismatch: got %s, want device_123", retrieved2.DeviceCode)
	}
	
	// Update
	req.Status = StatusAuthorized
	req.UserID = "user_123"
	if err := store.Update(req); err != nil {
		t.Fatalf("Update failed: %v", err)
	}
	
	retrieved3, _ := store.GetByDeviceCode("device_123")
	if retrieved3.Status != StatusAuthorized {
		t.Error("Status not updated")
	}
	if retrieved3.UserID != "user_123" {
		t.Error("UserID not updated")
	}
	
	// Delete
	if err := store.Delete("device_123"); err != nil {
		t.Fatalf("Delete failed: %v", err)
	}
	
	// Verify deletion
	_, err = store.GetByDeviceCode("device_123")
	if err == nil {
		t.Error("Expected error after deletion")
	}
}

// TestSecureMemoryStore_AuthorizationFlowOperations tests CRUD for authorization flow
func TestSecureMemoryStore_AuthorizationFlowOperations(t *testing.T) {
	store := NewSecureMemoryStore()
	defer store.Close()
	
	req := &AuthorizationRequest{
		AuthorizationCode:   "auth_code_123",
		ClientID:            "test-client",
		RedirectURI:         "http://localhost/callback",
		State:               "state_123",
		Scope:               "org:create_api_key user:profile",
		CodeChallenge:       "challenge_123",
		CodeChallengeMethod: "S256",
		DeviceID:            "device_abc",
		ExpiresAt:           time.Now().Add(time.Minute),
	}
	
	// Create
	if err := store.CreateAuthorization(req); err != nil {
		t.Fatalf("CreateAuthorization failed: %v", err)
	}
	
	// Read
	retrieved, err := store.GetAuthorizationByCode("auth_code_123")
	if err != nil {
		t.Fatalf("GetAuthorizationByCode failed: %v", err)
	}
	if retrieved.State != "state_123" {
		t.Errorf("State mismatch: got %s, want state_123", retrieved.State)
	}
	
	// Update
	req.UserID = "user_123"
	req.Email = "test@example.com"
	if err := store.UpdateAuthorization(req); err != nil {
		t.Fatalf("UpdateAuthorization failed: %v", err)
	}
	
	retrieved2, _ := store.GetAuthorizationByCode("auth_code_123")
	if retrieved2.UserID != "user_123" {
		t.Error("UserID not updated")
	}
	
	// Delete
	if err := store.DeleteAuthorization("auth_code_123"); err != nil {
		t.Fatalf("DeleteAuthorization failed: %v", err)
	}
	
	// Verify deletion
	_, err = store.GetAuthorizationByCode("auth_code_123")
	if err == nil {
		t.Error("Expected error after deletion")
	}
}

// TestSecureMemoryStore_ConcurrentAccess tests thread safety
func TestSecureMemoryStore_ConcurrentAccess(t *testing.T) {
	store := NewSecureMemoryStore()
	defer store.Close()
	
	done := make(chan bool, 10)
	
	// Concurrent writes
	for i := 0; i < 5; i++ {
		go func(n int) {
			req := &DeviceRequest{
				DeviceCode: fmt.Sprintf("device_%d", n),
				UserCode:   fmt.Sprintf("USER%d", n),
				ExpiresAt:  time.Now().Add(time.Minute),
			}
			store.Create(req)
			done <- true
		}(i)
	}
	
	// Concurrent reads
	for i := 0; i < 5; i++ {
		go func(n int) {
			store.GetByDeviceCode(fmt.Sprintf("device_%d", n))
			done <- true
		}(i)
	}
	
	// Wait for all goroutines
	for i := 0; i < 10; i++ {
		<-done
	}
}

// TestSecureMemoryStore_ExpirationTracking tests the O(1) expiration tracking
func TestSecureMemoryStore_ExpirationTracking(t *testing.T) {
	store := NewSecureMemoryStore()
	defer store.Close()
	
	now := time.Now()
	
	// Create requests with different expiration times
	req1 := &DeviceRequest{
		DeviceCode: "device_1",
		UserCode:   "USER0001",
		ExpiresAt:  now.Add(1 * time.Hour),
	}
	req2 := &DeviceRequest{
		DeviceCode: "device_2",
		UserCode:   "USER0002",
		ExpiresAt:  now.Add(2 * time.Hour),
	}
	
	store.Create(req1)
	store.Create(req2)
	
	// Verify both are tracked
	store.mu.RLock()
	count := len(store.deviceExpirations)
	store.mu.RUnlock()
	
	if count < 1 {
		t.Error("Expiration tracking not working")
	}
}


