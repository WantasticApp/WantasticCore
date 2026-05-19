package tenant

import (
	"time"
)

// Tenant represents a tenant in the system. Admin-created accounts only after Phase 3.
type Tenant struct {
	ID                string    `json:"id"`
	Email             string    `json:"email"`
	FullName          string    `json:"full_name"`
	PasswordHash      string    `json:"-"`
	TOTPSecret        string    `json:"-"`
	TOTPEnabled       bool      `json:"totp_enabled"`
	LastLogin         time.Time `json:"last_login"`
	TwoFAMethod       string    `json:"two_fa_method"`
	TwoFAWhatsApp     bool      `json:"two_fa_whatsapp"`
	TwoFAPendingCode  string    `json:"-"`
	TwoFACodeExpiry   time.Time `json:"-"`
	TwoFACodeAttempts int       `json:"-"`
	OverlayAccountID  string    `json:"overlay_account_id"`
	Networks          []string  `json:"networks"`
	Status            string    `json:"status"`
	IsAdmin           bool      `json:"is_admin"`
	PreferredLanguage string    `json:"preferred_language"`
	InactivityWarningSentAt *time.Time `json:"inactivity_warning_sent_at,omitempty"`
	// Auth0 identity — populated when using the Device Authorization Grant flow.
	Auth0Sub  string    `json:"auth0_sub,omitempty"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

// AccessShare represents a share of a tenant's resources.
type AccessShare struct {
	ID               string           `json:"id"`
	OwnerTenantID    string           `json:"owner_tenant_id"`
	OwnerEmail       string           `json:"owner_email"`
	OwnerName        string           `json:"owner_name"`
	SharedEmail      string           `json:"shared_email"`
	ShareeName       string           `json:"sharee_name"`
	Permissions      SharePermissions `json:"permissions"`
	Status           ShareStatus      `json:"status"`
	InviteToken      string           `json:"invite_token,omitempty"`
	AcceptedByTenant string           `json:"accepted_by_tenant,omitempty"`
	TagFilter        []string         `json:"tag_filter,omitempty"`
	IsLinkShare      bool             `json:"is_link_share"`
	ResendCount      int              `json:"resend_count"`
	LastResendAt     *time.Time       `json:"last_resend_at,omitempty"`
	CreatedAt        time.Time        `json:"created_at"`
	ExpiresAt        time.Time        `json:"expires_at"`
	AcceptedAt       *time.Time       `json:"accepted_at,omitempty"`
	RevokedAt        *time.Time       `json:"revoked_at,omitempty"`
	LastAccessedAt   *time.Time       `json:"last_accessed_at,omitempty"`
}

// SharePermissions defines what a share allows.
// Two simple categories replace the previous 9 granular flags:
//   - DevicesRead  — view devices, topology, ACL, activity, ping, metrics
//   - DevicesWrite — manage devices, Winbox, WebSSH, ACL(implies DevicesRead)
type SharePermissions struct {
	DevicesRead  bool `json:"devices_read"`
	DevicesWrite bool `json:"devices_write"`
}

// CanRead reports whether the permissions allow read-only access.
func (p SharePermissions) CanRead() bool { return p.DevicesRead || p.DevicesWrite }

// CanWrite reports whether the permissions allow write/management access.
func (p SharePermissions) CanWrite() bool { return p.DevicesWrite }

// ShareStatus defines the status of a share.
type ShareStatus string

const (
	ShareStatusPending  ShareStatus = "pending"
	ShareStatusAccepted ShareStatus = "accepted"
	ShareStatusExpired  ShareStatus = "expired"
	ShareStatusRevoked  ShareStatus = "revoked"
)

const (
	TwoFATOTP     = "totp"
	TwoFAWhatsApp = "whatsapp"
	TwoFANone     = "none"
)

func DefaultSharePermissions() SharePermissions {
	return SharePermissions{DevicesRead: true}
}

// TwoFAInfo contains 2FA configuration information.
type TwoFAInfo struct {
	Enabled     bool     `json:"enabled"`
	Method      string   `json:"method"`        // "none", "totp", "sms", "whatsapp"
	PhoneMasked string   `json:"phone_masked"`  // Masked phone number if using SMS/WhatsApp
	CanChangeTo []string `json:"can_change_to"` // Methods user can switch to
}

// TenantSession represents an active tenant session.
type TenantSession struct {
	SessionID     string    `json:"session_id"`
	TenantID      string    `json:"tenant_id"`
	Email         string    `json:"email"`
	FullName      string    `json:"full_name"`
	SessionToken  string    `json:"session_token"`
	CreatedAt     time.Time `json:"created_at"`
	ExpiresAt     time.Time `json:"expires_at"`
	LastActivity  time.Time `json:"last_activity"`
	IPAddress     string    `json:"ip_address"`
	UserAgent     string    `json:"user_agent"`
	RememberMe    bool      `json:"remember_me"`    // Same as TrustedDevice, kept for compatibility
	TrustedDevice bool      `json:"trusted_device"` // If true, skip 2FA on this session (remember me)
	DeviceHash    string    `json:"device_hash"`    // Hash of user-agent + IP for device identification
}

// GetTenantID returns the tenant ID for this session.
func (s *TenantSession) GetTenantID() string {
	return s.TenantID
}

// GetSessionToken returns the session token for this session.
func (s *TenantSession) GetSessionToken() string {
	return s.SessionToken
}

// GetParsedUserAgent returns parsed browser/OS info for this session
func (s *TenantSession) GetParsedUserAgent() *ParsedUserAgent {
	return ParseUserAgent(s.UserAgent)
}

// EnrollmentToken represents a secure token for automated device onboarding.
type EnrollmentToken struct {
	ID         string    `json:"id"`
	TenantID   string    `json:"tenant_id"`
	Name       string    `json:"name"`
	Token      string    `json:"token"`
	MaxUses    int       `json:"max_uses"`
	UsageCount int       `json:"usage_count"`
	ExpiresAt  time.Time `json:"expires_at"`
	CreatedAt  time.Time `json:"created_at"`
	CreatedBy  string    `json:"created_by"`
}
