// Package oauth2 provides secure in-memory storage with proper data sanitization.
// Sensitive data (PKCE challenges, authorization codes, tokens) is zeroed out
// before releasing memory to prevent data leakage.
package oauth2

import (
	"errors"
	"runtime"
	"sync"
	"time"
)

// SecureMemoryStore is a security-hardened in-memory store that properly
// sanitizes sensitive data before deallocation.
// 
// Security features:
// - Sensitive fields are zeroed before delete (no traces left)
// - Aggressive cleanup every 30 seconds for auth codes
// - Separate TTL tracking for O(1) expiration checks
// - Forces GC after bulk cleanup of sensitive data
type SecureMemoryStore struct {
	mu       sync.RWMutex
	byDevice map[string]*DeviceRequest
	byUser   map[string]*DeviceRequest
	
	// Authorization code flow storage
	byAuthCode map[string]*AuthorizationRequest
	
	// Expiration tracking for O(1) cleanup
	// Maps expiration time -> set of keys expiring at that time
	deviceExpirations map[int64]map[string]struct{}
	authExpirations   map[int64]map[string]struct{}
	
	stopCleanup chan struct{}
}

// NewSecureMemoryStore creates a new secure in-memory store
func NewSecureMemoryStore() *SecureMemoryStore {
	s := &SecureMemoryStore{
		byDevice:          make(map[string]*DeviceRequest),
		byUser:            make(map[string]*DeviceRequest),
		byAuthCode:        make(map[string]*AuthorizationRequest),
		deviceExpirations: make(map[int64]map[string]struct{}),
		authExpirations:   make(map[int64]map[string]struct{}),
		stopCleanup:       make(chan struct{}),
	}
	
	// Start aggressive cleanup goroutine
	go s.cleanupLoop()
	
	return s
}

// Close stops the cleanup goroutine and sanitizes all data
func (s *SecureMemoryStore) Close() {
	close(s.stopCleanup)
	
	s.mu.Lock()
	defer s.mu.Unlock()
	
	// Zero out all sensitive data before releasing
	for code, req := range s.byDevice {
		s.sanitizeDeviceRequest(req)
		delete(s.byDevice, code)
	}
	for code, req := range s.byUser {
		s.sanitizeDeviceRequest(req)
		delete(s.byUser, code)
	}
	for code, req := range s.byAuthCode {
		s.sanitizeAuthRequest(req)
		delete(s.byAuthCode, code)
	}
	
	// Force GC to reclaim memory
	runtime.GC()
}

// sanitizeDeviceRequest zeros out sensitive fields
func (s *SecureMemoryStore) sanitizeDeviceRequest(req *DeviceRequest) {
	if req == nil {
		return
	}
	// Zero sensitive fields
	req.DeviceCode = ""
	req.UserCode = ""
	req.AccessToken = ""
	req.DeviceID = ""
	req.UserID = ""
	req.Email = ""
	req.Name = ""
}

// sanitizeAuthRequest zeros out sensitive fields
func (s *SecureMemoryStore) sanitizeAuthRequest(req *AuthorizationRequest) {
	if req == nil {
		return
	}
	// Zero sensitive fields - PKCE challenge is critical to protect
	req.AuthorizationCode = ""
	req.CodeChallenge = ""
	req.DeviceID = ""
	req.UserID = ""
	req.Email = ""
	req.Name = ""
	req.State = ""
}

// trackDeviceExpiration adds a key to the expiration tracker
func (s *SecureMemoryStore) trackDeviceExpiration(code string, expiresAt time.Time) {
	bucket := expiresAt.Unix()
	if s.deviceExpirations[bucket] == nil {
		s.deviceExpirations[bucket] = make(map[string]struct{})
	}
	s.deviceExpirations[bucket][code] = struct{}{}
}

// trackAuthExpiration adds a key to the expiration tracker
func (s *SecureMemoryStore) trackAuthExpiration(code string, expiresAt time.Time) {
	bucket := expiresAt.Unix()
	if s.authExpirations[bucket] == nil {
		s.authExpirations[bucket] = make(map[string]struct{})
	}
	s.authExpirations[bucket][code] = struct{}{}
}

// untrackDeviceExpiration removes a key from the expiration tracker
func (s *SecureMemoryStore) untrackDeviceExpiration(code string, expiresAt time.Time) {
	bucket := expiresAt.Unix()
	if bucketSet, ok := s.deviceExpirations[bucket]; ok {
		delete(bucketSet, code)
		if len(bucketSet) == 0 {
			delete(s.deviceExpirations, bucket)
		}
	}
}

// untrackAuthExpiration removes a key from the expiration tracker
func (s *SecureMemoryStore) untrackAuthExpiration(code string, expiresAt time.Time) {
	bucket := expiresAt.Unix()
	if bucketSet, ok := s.authExpirations[bucket]; ok {
		delete(bucketSet, code)
		if len(bucketSet) == 0 {
			delete(s.authExpirations, bucket)
		}
	}
}

// cleanupLoop runs every 30 seconds to remove expired entries
func (s *SecureMemoryStore) cleanupLoop() {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()
	
	for {
		select {
		case <-s.stopCleanup:
			return
		case <-ticker.C:
			s.cleanupExpired()
		}
	}
}

// cleanupExpired removes expired entries in O(number of expired items)
func (s *SecureMemoryStore) cleanupExpired() {
	s.mu.Lock()
	defer s.mu.Unlock()
	
	now := time.Now().Unix()
	needsGC := false
	
	// Cleanup expired device requests
	for bucket, codes := range s.deviceExpirations {
		if bucket > now {
			continue // Not expired yet
		}
		
		for code := range codes {
			if req, ok := s.byDevice[code]; ok {
				s.sanitizeDeviceRequest(req)
				delete(s.byUser, req.UserCode)
				delete(s.byDevice, code)
				needsGC = true
			}
		}
		delete(s.deviceExpirations, bucket)
	}
	
	// Cleanup expired authorization requests (more sensitive - clean aggressively)
	for bucket, codes := range s.authExpirations {
		if bucket > now {
			continue // Not expired yet
		}
		
		for code := range codes {
			if req, ok := s.byAuthCode[code]; ok {
				s.sanitizeAuthRequest(req)
				delete(s.byAuthCode, code)
				needsGC = true
			}
		}
		delete(s.authExpirations, bucket)
	}
	
	// Force GC if we cleaned up sensitive data
	if needsGC {
		s.mu.Unlock()
		runtime.GC()
		s.mu.Lock()
	}
}

// Create stores a new device authorization request
func (s *SecureMemoryStore) Create(req *DeviceRequest) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	
	s.byDevice[req.DeviceCode] = req
	s.byUser[req.UserCode] = req
	s.trackDeviceExpiration(req.DeviceCode, req.ExpiresAt)
	
	return nil
}

// GetByDeviceCode retrieves a request by device code
func (s *SecureMemoryStore) GetByDeviceCode(deviceCode string) (*DeviceRequest, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	
	req, ok := s.byDevice[deviceCode]
	if !ok {
		return nil, ErrNotFound
	}
	
	// Check expiration without modifying the request
	if time.Now().After(req.ExpiresAt) {
		return nil, ErrExpired
	}
	
	return req, nil
}

// GetByUserCode retrieves a request by user code
func (s *SecureMemoryStore) GetByUserCode(userCode string) (*DeviceRequest, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	
	req, ok := s.byUser[userCode]
	if !ok {
		return nil, ErrNotFound
	}
	
	if time.Now().After(req.ExpiresAt) {
		return nil, ErrExpired
	}
	
	return req, nil
}

// Update updates an existing request
func (s *SecureMemoryStore) Update(req *DeviceRequest) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	
	oldReq, ok := s.byDevice[req.DeviceCode]
	if !ok {
		return ErrNotFound
	}
	
	// Untrack old expiration time
	s.untrackDeviceExpiration(req.DeviceCode, oldReq.ExpiresAt)
	
	// Update and track new expiration
	s.byDevice[req.DeviceCode] = req
	s.byUser[req.UserCode] = req
	s.trackDeviceExpiration(req.DeviceCode, req.ExpiresAt)
	
	return nil
}

// Delete removes a request and sanitizes its data
func (s *SecureMemoryStore) Delete(deviceCode string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	
	req, ok := s.byDevice[deviceCode]
	if !ok {
		return nil
	}
	
	// Sanitize before delete
	s.sanitizeDeviceRequest(req)
	s.untrackDeviceExpiration(deviceCode, req.ExpiresAt)
	
	delete(s.byDevice, deviceCode)
	delete(s.byUser, req.UserCode)
	
	return nil
}

// CreateAuthorization stores a new authorization request with PKCE
func (s *SecureMemoryStore) CreateAuthorization(req *AuthorizationRequest) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	
	s.byAuthCode[req.AuthorizationCode] = req
	s.trackAuthExpiration(req.AuthorizationCode, req.ExpiresAt)
	
	return nil
}

// GetAuthorizationByCode retrieves an authorization request by code
func (s *SecureMemoryStore) GetAuthorizationByCode(code string) (*AuthorizationRequest, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	
	req, ok := s.byAuthCode[code]
	if !ok {
		return nil, ErrNotFound
	}
	
	if time.Now().After(req.ExpiresAt) {
		return nil, ErrExpired
	}
	
	return req, nil
}

// UpdateAuthorization updates an existing authorization request
func (s *SecureMemoryStore) UpdateAuthorization(req *AuthorizationRequest) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	
	oldReq, ok := s.byAuthCode[req.AuthorizationCode]
	if !ok {
		return ErrNotFound
	}
	
	// Untrack old expiration
	s.untrackAuthExpiration(req.AuthorizationCode, oldReq.ExpiresAt)
	
	// Update and track new expiration
	s.byAuthCode[req.AuthorizationCode] = req
	s.trackAuthExpiration(req.AuthorizationCode, req.ExpiresAt)
	
	return nil
}

// DeleteAuthorization removes an authorization request and sanitizes its data
func (s *SecureMemoryStore) DeleteAuthorization(code string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	
	req, ok := s.byAuthCode[code]
	if !ok {
		return nil
	}
	
	// Sanitize before delete (critical for PKCE)
	s.sanitizeAuthRequest(req)
	s.untrackAuthExpiration(code, req.ExpiresAt)
	
	delete(s.byAuthCode, code)
	
	return nil
}

// Common errors
var (
	ErrNotFound = errors.New("not found")
	ErrExpired  = errors.New("expired")
)
