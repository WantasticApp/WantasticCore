package session

import "time"

// SessionStore defines the methods required for managing tenant portal sessions
type SessionStore interface {
	CreateSession(tenantID, fullName, email, tier, grpcSessionToken string, rememberMe bool) (*TenantSession, error)
	ValidateSession(token string) (any, error)
	GetSession(token string) (*TenantSession, error)
	DeleteSession(token string) error
	DeleteAllSessionsForTenant(tenantID string) int
	GetSessionCount() int

	// API Key Management for MCP
	CreateAPIKey(tenantID, name, grpcSessionToken string, expiresAt time.Time) (*APIKey, error)
	GetAPIKey(token string) (*APIKey, error)
	ListAPIKeys(tenantID string) ([]*APIKey, error)
	RevokeAPIKey(tenantID, keyID string) error
}

// APIKey represents a persistent authentication token for MCP clients
type APIKey struct {
	ID               string    `json:"id"`
	TenantID         string    `json:"tenant_id"`
	Name             string    `json:"name"`
	Token            string    `json:"token"`              // The high-entropy secret
	GRPCSessionToken string    `json:"grpc_session_token"` // The underlying session token
	CreatedAt        time.Time `json:"created_at"`
	LastUsedAt       time.Time `json:"last_used_at"`
	ExpiresAt        time.Time `json:"expires_at"`
}
