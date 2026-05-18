package tenant

import "time"

// Registry defines the interface for tenant management.
// It covers both data access (CRUD) and business logic (2FA, Verification, Sharing).
type Registry interface {
	// Tenant CRUD
	CreateTenant(tenant *Tenant) error
	GetTenant(tenantID string) (*Tenant, error)
	GetTenantByEmail(email string) (*Tenant, error)
	GetTenantByOverlayAccount(overlayAccountID string) (*Tenant, error)
	// GetTenantByAuth0Sub finds a tenant by their Auth0 subject identifier ("sub" claim).
	// Returns an error wrapping "not found" when no match exists.
	GetTenantByAuth0Sub(auth0Sub string) (*Tenant, error)
	UpdateTenant(tenant *Tenant) error
	DeleteTenant(tenantID string) error
	ListTenants() ([]*Tenant, error)

	// Tenant Logic/State
	SetOverlayAccountID(tenantID, overlayAccountID string, networks []string) error
	UpdateLastLogin(tenantID string) error
	SetTenantStatus(tenantID, status string) error

	// Two-Factor Authentication
	SetTwoFAMethod(tenantID, method string, totpSecret string) error
	GetActiveTwoFAMethod(tenantID string) (string, error)
	IsTwoFAEnabled(tenantID string) (bool, error)
	SetPending2FACode(tenantID, code string, expiresIn time.Duration) error
	Verify2FACode(tenantID, code string) (bool, bool, bool, error)
	Clear2FACode(tenantID string) error
	GetTwoFAInfo(tenantID string) (*TwoFAInfo, error)

	// Session Management
	CreateSession(tenantID, sessionID, ipAddress, userAgent, deviceHash string, duration time.Duration, trustedDevice bool) error

	ValidateSession(sessionID string) (string, error)
	GetSession(sessionID string) (*TenantSession, error)
	DeleteSession(sessionID string) error
	GetTenantSessions(tenantID string) ([]*TenantSession, error)
	DeleteTenantSession(tenantID, sessionID string) error
	UpdateSessionActivity(sessionID string) error
	HasTrustedDevice(tenantID, deviceHash string) bool
	InvalidateAllSessions(tenantID string) error
	CleanupExpiredSessions() (int, error)

	// Tenant Management
	UpdatePassword(tenantID, passwordHash string) error

	// Enrollment Tokens
	CreateEnrollmentToken(token *EnrollmentToken) error
	GetEnrollmentToken(tokenID string) (*EnrollmentToken, error)
	ListEnrollmentTokens(tenantID string) ([]*EnrollmentToken, error)
	DeleteEnrollmentToken(tenantID, tokenID string) error
	ValidateEnrollmentToken(tokenString string) (tenantID string, tokenID string, err error)
	IncrementEnrollmentTokenUsage(tokenID string) error
	CleanupExpiredEnrollmentTokens() (int, error)

}
