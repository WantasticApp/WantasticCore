// Package oauth2 implements RFC 8628 OAuth 2.0 Device Authorization Grant.
// This is a drop-in replacement for external Auth0 device flow.
package oauth2

import (
	"time"
)

// DeviceRequest represents a device authorization request per RFC 8628 Section 3.1
type DeviceRequest struct {
	// DeviceCode is a long-term identifier for the session (opaque to client)
	DeviceCode string
	
	// UserCode is a short, human-readable code for user entry
	UserCode string
	
	// Status tracks the authorization state
	Status AuthorizationStatus
	
	// ClientID identifies the requesting client application
	ClientID string
	
	// DeviceID is the hashed machine identifier from the agent
	DeviceID string
	
	// User info (populated after successful authorization)
	UserID   string
	Email    string
	Name     string
	TenantID string
	Tier     string
	
	// AccessToken issued after authorization
	AccessToken string
	
	// Timestamps for lifecycle management
	CreatedAt    time.Time
	ExpiresAt    time.Time
	LastPolledAt time.Time
}

// AuthorizationStatus represents the state of a device authorization request
type AuthorizationStatus int

const (
	StatusPending AuthorizationStatus = iota
	StatusAuthorized
	StatusDenied
	StatusExpired
	StatusConsumed
)

func (s AuthorizationStatus) String() string {
	switch s {
	case StatusPending:
		return "pending"
	case StatusAuthorized:
		return "authorized"
	case StatusDenied:
		return "denied"
	case StatusExpired:
		return "expired"
	case StatusConsumed:
		return "consumed"
	default:
		return "unknown"
	}
}

// IsExpired checks if the device code has expired
func (r *DeviceRequest) IsExpired() bool {
	return time.Now().After(r.ExpiresAt)
}

// CanPoll checks if polling is allowed (rate limiting)
func (r *DeviceRequest) CanPoll(interval time.Duration) bool {
	return time.Since(r.LastPolledAt) >= interval
}

// DeviceAuthorizationResponse is returned from the device authorization endpoint
// RFC 8628 Section 3.2
type DeviceAuthorizationResponse struct {
	DeviceCode               string `json:"device_code"`
	UserCode                 string `json:"user_code"`
	VerificationURI          string `json:"verification_uri"`
	VerificationURIComplete  string `json:"verification_uri_complete,omitempty"`
	ExpiresIn                int    `json:"expires_in"`
	Interval                 int    `json:"interval,omitempty"`
}

// TokenResponse is returned from the token endpoint
// RFC 6749 Section 5.1
type TokenResponse struct {
	AccessToken string `json:"access_token"`
	TokenType   string `json:"token_type"`
	ExpiresIn   int    `json:"expires_in,omitempty"`
}

// TokenErrorResponse is returned for token endpoint errors
// RFC 6749 Section 5.2 and RFC 8628 Section 3.5
type TokenErrorResponse struct {
	Error            string `json:"error"`
	ErrorDescription string `json:"error_description,omitempty"`
}

// Common OAuth2 errors per RFC 8628 Section 3.5
const (
	ErrAuthorizationPending = "authorization_pending"
	ErrSlowDown             = "slow_down"
	ErrExpiredToken         = "expired_token"
	ErrAccessDenied         = "access_denied"
	ErrInvalidGrant         = "invalid_grant"
	ErrInvalidClient        = "invalid_client"
	ErrInvalidRequest       = "invalid_request"
)

// Config holds OAuth2 server configuration
type Config struct {
	// Issuer is the OAuth2 issuer identifier (e.g., "https://console.wantastic.app")
	Issuer string
	
	// DeviceCode settings
	DeviceCodeLength   int           // Length of device code (default: 40)
	UserCodeLength     int           // Length of user code (default: 8)
	DeviceCodeLifetime time.Duration // How long device codes are valid (default: 10 min)
	
	// Polling settings
	MinPollInterval    time.Duration // Minimum time between polls (default: 5 sec)
	
	// Access token settings
	AccessTokenLifetime time.Duration // How long access tokens are valid (default: 24 hours)
	
	// Storage backend
	Store Store

	// SigningSecret is an optional shared secret used for HMAC-SHA256 (HS256) JWT signing.
	// When set, all instances sharing the same secret can validate each other's tokens.
	// If empty, a random ECDSA key is generated per instance (single-instance only).
	SigningSecret []byte
}

// DefaultConfig returns a default configuration suitable for production
func DefaultConfig() *Config {
	return &Config{
		Issuer:              "https://console.wantastic.app",
		DeviceCodeLength:    40,
		UserCodeLength:      8,
		DeviceCodeLifetime:  10 * time.Minute,
		MinPollInterval:     5 * time.Second,
		AccessTokenLifetime: 24 * time.Hour,
	}
}

// DevConfig returns a configuration suitable for local development
func DevConfig() *Config {
	cfg := DefaultConfig()
	cfg.Issuer = "https://wantastic.local"
	cfg.DeviceCodeLifetime = 30 * time.Minute // Longer for dev convenience
	return cfg
}

// IsDevMode checks if the issuer is a development domain
func IsDevMode(issuer string) bool {
	return issuer == "https://wantastic.local" || 
		issuer == "http://wantastic.local" ||
		issuer == "http://localhost" ||
		issuer == "http://127.0.0.1"
}

// ============== PKCE (RFC 7636) Authorization Code Flow Types ==============

// AuthorizationRequest represents an OAuth2 authorization request with PKCE
// RFC 6749 Section 4.1.1 + RFC 7636
type AuthorizationRequest struct {
	// ClientID identifies the requesting client application
	ClientID string
	
	// RedirectURI where the user agent will be redirected after authorization
	RedirectURI string
	
	// State is an opaque value used to maintain state between request and callback (CSRF protection)
	State string
	
	// Scope defines the requested permissions
	Scope string
	
	// PKCE parameters (RFC 7636)
	CodeChallenge       string // BASE64URL(SHA256(code_verifier))
	CodeChallengeMethod string // "S256" (only S256 is supported)
	
	// Device identification for agent oauth
	DeviceID string
	
	// Authorization code (generated after user approves)
	AuthorizationCode string
	
	// User info (populated after successful authorization)
	UserID   string
	Email    string
	Name     string
	TenantID string
	Tier     string
	
	// Timestamps for lifecycle management
	CreatedAt time.Time
	ExpiresAt time.Time
}

// IsExpired checks if the authorization request has expired
func (r *AuthorizationRequest) IsExpired() bool {
	return time.Now().After(r.ExpiresAt)
}

// AuthorizationResponse is returned from the authorization endpoint redirect
// RFC 6749 Section 4.1.2
type AuthorizationResponse struct {
	Code  string `json:"code"`
	State string `json:"state,omitempty"`
}

// AuthorizationErrorResponse is returned for authorization errors
// RFC 6749 Section 4.1.2.1
type AuthorizationErrorResponse struct {
	Error            string `json:"error"`
	ErrorDescription string `json:"error_description,omitempty"`
	State            string `json:"state,omitempty"`
}

// TokenRequest represents a token endpoint request
// RFC 6749 Section 4.1.3 + RFC 7636
type TokenRequest struct {
	GrantType    string // "authorization_code" or "urn:ietf:params:oauth:grant-type:device_code"
	Code         string // Authorization code (for authorization_code grant)
	DeviceCode   string // Device code (for device_code grant)
	RedirectURI  string // Must match the one in authorization request
	ClientID     string
	CodeVerifier string // PKCE code verifier (for authorization_code grant)
}

// PKCE constants
const (
	// CodeChallengeMethodS256 is the only supported PKCE method
	CodeChallengeMethodS256 = "S256"
	
	// GrantTypeAuthorizationCode for standard authorization code flow
	GrantTypeAuthorizationCode = "authorization_code"
	
	// GrantTypeDeviceCode for device authorization flow
	GrantTypeDeviceCode = "urn:ietf:params:oauth:grant-type:device_code"
)

// Common OAuth2 authorization errors
const (
	ErrInvalidScope        = "invalid_scope"
	ErrUnsupportedResponseType = "unsupported_response_type"
	ErrServerError         = "server_error"
	ErrTemporarilyUnavailable = "temporarily_unavailable"
)
