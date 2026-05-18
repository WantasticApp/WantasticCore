package session

import (
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"sync"
	"time"
)

// TenantSession represents a tenant portal session
type TenantSession struct {
	Token            string    `json:"token"`              // Local session token (cookie value)
	GRPCSessionToken string    `json:"grpc_session_token"` // gRPC server session token (for logout)
	TenantID         string    `json:"tenant_id"`          // Tenant ID
	FullName         string    `json:"full_name"`          // User's full name
	Email            string    `json:"email"`              // User's email
	Tier             string    `json:"tier"`               // Account tier
	CreatedAt        time.Time `json:"created_at"`         // Session creation time
	ExpiresAt        time.Time `json:"expires_at"`         // Session expiration time
	RememberMe       bool      `json:"remember_me"`        // Extended session
}

// InMemorySessionStore stores sessions in memory (suitable for single-instance deployments)
// For production with multiple instances, use Redis or a database
type InMemorySessionStore struct {
	sessions map[string]*TenantSession
	mu       sync.RWMutex
}

// NewInMemorySessionStore creates a new in-memory session store
func NewInMemorySessionStore() *InMemorySessionStore {
	store := &InMemorySessionStore{
		sessions: make(map[string]*TenantSession),
	}

	// Start cleanup goroutine to remove expired sessions
	go store.cleanupExpiredSessions()

	return store
}

// CreateSession creates a new session for a tenant
func (s *InMemorySessionStore) CreateSession(tenantID, fullName, email, tier, grpcSessionToken string, rememberMe bool) (*TenantSession, error) {
	// Generate secure random token
	tokenBytes := make([]byte, 32)
	if _, err := rand.Read(tokenBytes); err != nil {
		return nil, fmt.Errorf("failed to generate session token: %w", err)
	}
	token := base64.URLEncoding.EncodeToString(tokenBytes)

	// Determine expiration based on remember me
	expiresAt := time.Now().Add(8 * time.Hour) // Default: 8 hours
	if rememberMe {
		expiresAt = time.Now().Add(30 * 24 * time.Hour) // Remember me: 30 days
	}

	session := &TenantSession{
		Token:            token,
		GRPCSessionToken: grpcSessionToken,
		TenantID:         tenantID,
		FullName:         fullName,
		Email:            email,
		Tier:             tier,
		CreatedAt:        time.Now(),
		ExpiresAt:        expiresAt,
		RememberMe:       rememberMe,
	}

	s.mu.Lock()
	s.sessions[token] = session
	s.mu.Unlock()

	return session, nil
}

// ValidateSession validates a session token and returns session data as any
// This matches the TenantSessionStore interface in tenant_proxy.go
func (s *InMemorySessionStore) ValidateSession(token string) (any, error) {
	s.mu.RLock()
	session, exists := s.sessions[token]
	s.mu.RUnlock()

	if !exists {
		return nil, fmt.Errorf("session not found")
	}

	// Check if session is expired
	if time.Now().After(session.ExpiresAt) {
		// Remove expired session
		s.mu.Lock()
		delete(s.sessions, token)
		s.mu.Unlock()
		return nil, fmt.Errorf("session expired")
	}

	return session, nil
}

// GetSession validates and returns the typed session object
func (s *InMemorySessionStore) GetSession(token string) (*TenantSession, error) {
	sessionInterface, err := s.ValidateSession(token)
	if err != nil {
		return nil, err
	}
	if session, ok := sessionInterface.(*TenantSession); ok {
		return session, nil
	}
	return nil, fmt.Errorf("invalid session type")
} // DeleteSession removes a session (logout)
func (s *InMemorySessionStore) DeleteSession(token string) error {
	s.mu.Lock()
	delete(s.sessions, token)
	s.mu.Unlock()
	return nil
}

// DeleteAllSessionsForTenant removes all sessions for a specific tenant (e.g., password change)
func (s *InMemorySessionStore) DeleteAllSessionsForTenant(tenantID string) int {
	s.mu.Lock()
	defer s.mu.Unlock()

	count := 0
	for token, session := range s.sessions {
		if session.TenantID == tenantID {
			delete(s.sessions, token)
			count++
		}
	}

	return count
}

// GetSessionCount returns the number of active sessions
func (s *InMemorySessionStore) GetSessionCount() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return len(s.sessions)
}

// API Key stubs for InMemorySessionStore
func (s *InMemorySessionStore) CreateAPIKey(tenantID, name, grpcSessionToken string, expiresAt time.Time) (*APIKey, error) {
	return nil, fmt.Errorf("api keys not supported in memory store")
}

func (s *InMemorySessionStore) GetAPIKey(token string) (*APIKey, error) {
	return nil, fmt.Errorf("api keys not supported in memory store")
}

func (s *InMemorySessionStore) ListAPIKeys(tenantID string) ([]*APIKey, error) {
	return nil, fmt.Errorf("api keys not supported in memory store")
}

func (s *InMemorySessionStore) RevokeAPIKey(tenantID, keyID string) error {
	return fmt.Errorf("api keys not supported in memory store")
}

// cleanupExpiredSessions periodically removes expired sessions
func (s *InMemorySessionStore) cleanupExpiredSessions() {
	ticker := time.NewTicker(10 * time.Minute)
	defer ticker.Stop()

	for range ticker.C {
		s.mu.Lock()
		now := time.Now()
		for token, session := range s.sessions {
			if now.After(session.ExpiresAt) {
				delete(s.sessions, token)
			}
		}
		s.mu.Unlock()
	}
}

// SerializeSession converts session to JSON (for debugging)
func (s *TenantSession) SerializeSession() (string, error) {
	data, err := json.Marshal(s)
	if err != nil {
		return "", err
	}
	return string(data), nil
}

// GetTenantID implements interface for WebSocket proxy
func (s *TenantSession) GetTenantID() string {
	return s.TenantID
}

// GetSessionToken implements interface for WebSocket proxy
// Returns the gRPC session token for authentication with the gRPC server
func (s *TenantSession) GetSessionToken() string {
	return s.GRPCSessionToken
}

// GetLocalToken returns the local session token (cookie value)
func (s *TenantSession) GetLocalToken() string {
	return s.Token
}
