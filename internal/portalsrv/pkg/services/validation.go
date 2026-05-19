package services

import (
	"fmt"
	"strings"
)

// ValidationConfig holds validation configuration.
type ValidationConfig struct {
	MaxRequestSize   int64 // Maximum request size in bytes
	AllowedServices  []string
	ServiceMethods   map[string][]string // service -> allowed methods
	RequireAccountID bool                // Whether account_id is required in requests
}

// DefaultValidationConfig returns the default validation configuration.
func DefaultValidationConfig() *ValidationConfig {
	return &ValidationConfig{
		MaxRequestSize: 1 << 20, // 1MB
		AllowedServices: []string{
			"AuthService",
			"AccountService",
			"PeerService",
			"NetworkService",
			"ACLService",
			"WebSSHService",
		},
		ServiceMethods: map[string][]string{
			"AuthService": {
				"CheckSetup",
				"GenerateTOTPSecret",
				"Setup",
				"Login",
				"Logout",
			},
			"AccountService": {
				"CreateAccount",
				"GetAccount",
				"ListAccounts",
				"DeleteAccount",
				"UpdateAccountTier",
				"GetAccountStats",
			},
			"PeerService": {
				"AddPeer",
				"RemovePeer",
				"ListPeers",
				"GetPeer",
				"UpdatePeer",
				"GetPeerConfig",
				"PingPeer",
				"SetWinboxCredentials",
				"GetWinboxStatus",
				"ClearWinboxCredentials",
				"CreateWinboxSession",
				"UpdateWinboxSession",
				"DeleteWinboxSession",
				"ListWinboxSessions",
				"GetWinboxSession",
			},
			"NetworkService": {
				"GetNetwork",
				"GetNetworkStats",
				"GetAccountIPStatistics",
				"ListNetworks",
				"AllocateIP",
				"ReleaseIP",
			},
			"ACLService": {
				"AddACLRule",
				"RemoveACLRule",
				"GetACLRules",
				"UpdateACLRule",
				"CheckAccess",
			},
			"AdminService": {
				"GetGlobalStats",
				"HealthCheck",
				"ListConnections",
				"GetMetrics",
				"GetTopology",
			},
			"WebSSHService": {
				"CreateWebSSHSession",
				"GetWebSSHSession",
				"ListWebSSHSessions",
				"DisconnectWebSSHSession",
				"CreateWinboxSession",
				"ListWinboxSessions",
				"DeleteWinboxSession",
				"EnableWinboxSession",
				"DisableWinboxSession",
			},
		},
		RequireAccountID: false,
	}
}

// MessageValidator validates WebSocket messages.
type MessageValidator struct {
	config *ValidationConfig
}

// NewMessageValidator creates a new message validator.
func NewMessageValidator(config *ValidationConfig) *MessageValidator {
	if config == nil {
		config = DefaultValidationConfig()
	}
	return &MessageValidator{
		config: config,
	}
}

// ValidateMessage validates a WebSocket message.
// Returns error if validation fails.
func (v *MessageValidator) ValidateMessage(msg *Message, session *Session) error {
	// 1. Check message ID exists
	if msg.ID == "" {
		return fmt.Errorf("message ID is required")
	}

	// 2. Check message ID format (prevent injection)
	if !isValidMessageID(msg.ID) {
		return fmt.Errorf("invalid message ID format")
	}

	// 3. Check service is allowed
	if !v.isServiceAllowed(msg.Service) {
		return fmt.Errorf("service not allowed: %s", msg.Service)
	}

	// 4. Check method is allowed for this service
	if !v.isMethodAllowed(msg.Service, msg.Method) {
		return fmt.Errorf("method not allowed: %s.%s", msg.Service, msg.Method)
	}

	// 5. Check request size
	if len(msg.Request) > int(v.config.MaxRequestSize) {
		return fmt.Errorf("request too large: %d bytes (max: %d)", len(msg.Request), v.config.MaxRequestSize)
	}

	// 6. Validate request is valid JSON
	if len(msg.Request) > 0 && !isValidJSON(msg.Request) {
		return fmt.Errorf("invalid JSON in request")
	}

	// 7. Check authentication for non-auth services
	if msg.Service != "AuthService" {
		if session.AuthToken == "" {
			return fmt.Errorf("authentication required for %s", msg.Service)
		}
	}

	// 8. Validate account ID scope (if present in request)
	if v.config.RequireAccountID && msg.Service != "AuthService" && msg.Service != "AdminService" {
		if err := v.validateAccountAccess(msg, session); err != nil {
			return err
		}
	}

	return nil
}

// isServiceAllowed checks if a service is in the allowed list.
func (v *MessageValidator) isServiceAllowed(service string) bool {
	for _, allowed := range v.config.AllowedServices {
		if service == allowed {
			return true
		}
	}
	return false
}

// isMethodAllowed checks if a method is allowed for a given service.
func (v *MessageValidator) isMethodAllowed(service, method string) bool {
	methods, exists := v.config.ServiceMethods[service]
	if !exists {
		return false
	}

	for _, allowed := range methods {
		if method == allowed {
			return true
		}
	}
	return false
}

// validateAccountAccess validates that the session has access to the requested account.
func (v *MessageValidator) validateAccountAccess(msg *Message, session *Session) error {
	// Extract account_id from request
	// This is a simplified check - in production, parse JSON properly
	requestStr := string(msg.Request)

	// Check if account_id is present in request
	if !strings.Contains(requestStr, "account_id") {
		// Some methods don't require account_id (e.g., ListAccounts)
		return nil
	}

	// If session has AccountID set, ensure it matches
	if session.AccountID != "" {
		// In a real implementation, parse JSON and compare account_id
		// For now, we trust the session
	}

	return nil
}

// isValidMessageID checks if message ID format is valid.
// Prevents injection attacks through message ID.
func isValidMessageID(id string) bool {
	// Message ID should be alphanumeric with hyphens/underscores and some base64 chars
	// Max length 128 characters to allow for base64-encoded peer IDs
	if len(id) == 0 || len(id) > 128 {
		return false
	}

	for _, char := range id {
		if !isAlphanumeric(char) && char != '-' && char != '_' {
			return false
		}
	}

	return true
}

// isAlphanumeric checks if a rune is alphanumeric.
func isAlphanumeric(r rune) bool {
	return (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9')
}

// isValidJSON performs a basic check if data is valid JSON.
func isValidJSON(data []byte) bool {
	if len(data) == 0 {
		return true
	}

	// Check for basic JSON structure
	str := strings.TrimSpace(string(data))

	// Must start with { or [
	if len(str) == 0 {
		return true
	}

	if str[0] != '{' && str[0] != '[' {
		return false
	}

	// Must end with } or ]
	if str[len(str)-1] != '}' && str[len(str)-1] != ']' {
		return false
	}

	return true
}

// SanitizeString removes potentially dangerous characters from strings.
func SanitizeString(s string) string {
	// Remove null bytes
	s = strings.ReplaceAll(s, "\x00", "")

	// Remove control characters
	var sanitized strings.Builder
	for _, r := range s {
		if r >= 32 || r == '\n' || r == '\r' || r == '\t' {
			sanitized.WriteRune(r)
		}
	}

	return sanitized.String()
}

// ValidateAccountID checks if an account ID format is valid.
func ValidateAccountID(accountID string) error {
	if accountID == "" {
		return fmt.Errorf("account ID cannot be empty")
	}

	if len(accountID) > 64 {
		return fmt.Errorf("account ID too long (max 64 characters)")
	}

	// Account ID should be alphanumeric with hyphens/underscores
	for _, char := range accountID {
		if !isAlphanumeric(char) && char != '-' && char != '_' {
			return fmt.Errorf("invalid character in account ID: %c", char)
		}
	}

	return nil
}

// ValidatePeerID checks if a peer ID format is valid.
func ValidatePeerID(peerID string) error {
	if peerID == "" {
		return fmt.Errorf("peer ID cannot be empty")
	}

	if len(peerID) > 64 {
		return fmt.Errorf("peer ID too long (max 64 characters)")
	}

	// Peer ID should be alphanumeric with hyphens/underscores
	for _, char := range peerID {
		if !isAlphanumeric(char) && char != '-' && char != '_' {
			return fmt.Errorf("invalid character in peer ID: %c", char)
		}
	}

	return nil
}

// RateLimitConfig holds rate limiting configuration per service.
type RateLimitConfig struct {
	RequestsPerMinute int
	BurstSize         int
}

// GetServiceRateLimit returns rate limit config for a service.
func GetServiceRateLimit(service string) *RateLimitConfig {
	// Different rate limits for different services
	limits := map[string]*RateLimitConfig{
		"AuthService": {
			RequestsPerMinute: 120, // Strict limit for auth
			BurstSize:         3,
		},
		"AccountService": {
			RequestsPerMinute: 1000,
			BurstSize:         10,
		},
		"PeerService": {
			RequestsPerMinute: 1000,
			BurstSize:         20,
		},
		"WebSSHService": {
			RequestsPerMinute: 1000,
			BurstSize:         5,
		},
		"AdminService": {
			RequestsPerMinute: 1000,
			BurstSize:         30,
		},
	}

	if limit, exists := limits[service]; exists {
		return limit
	}

	// Default rate limit
	return &RateLimitConfig{
		RequestsPerMinute: 60,
		BurstSize:         10,
	}
}

// SecurityContext holds security-related information for a session.
type SecurityContext struct {
	IPAddress       string
	UserAgent       string
	SessionID       string
	AuthToken       string
	LastActivity    int64
	RequestCount    int
	FailedAuthCount int
}

// IsHighRisk determines if a session is high-risk based on behavior.
func (sc *SecurityContext) IsHighRisk() bool {
	// High request count
	if sc.RequestCount > 1000 {
		return true
	}

	// Multiple failed auth attempts
	if sc.FailedAuthCount > 5 {
		return true
	}

	return false
}
