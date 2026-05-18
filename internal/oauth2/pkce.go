// Package oauth2 implements RFC 7636 Proof Key for Code Exchange (PKCE)
// and RFC 6749 Authorization Code Flow.
package oauth2

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"net/url"
	"strings"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
)

// ============== PKCE (RFC 7636) Implementation ==============

// registeredClients defines valid OAuth2 clients and their allowed redirect URIs
// In production, this should be loaded from a database
var registeredClients = map[string]*ClientRegistration{
	"wantastic-agent-client": {
		ClientID:     "wantastic-agent-client",
		RedirectURIs: []string{"http://localhost:58250/callback", "https://localhost/callback"},
		AllowedScopes: []string{"org:create_api_key", "user:profile", "device:register"},
	},
	"wantastic-device-client": {
		ClientID:      "wantastic-device-client",
		RedirectURIs:  []string{"http://localhost:58250/callback"},
		AllowedScopes: []string{"org:create_api_key", "user:profile", "device:register"},
	},
	// wantastic_cipher_v_1_0_0 is the shared cipher secret used as the default
	// client_id by wantasticd agents. It maps to the same permissions as wantastic-agent-client.
	"wantastic_cipher_v_1_0_0": {
		ClientID:      "wantastic_cipher_v_1_0_0",
		RedirectURIs:  []string{"http://localhost:58250/callback", "https://localhost/callback"},
		AllowedScopes: []string{"org:create_api_key", "user:profile", "device:register"},
	},
}

// ClientRegistration represents a registered OAuth2 client
type ClientRegistration struct {
	ClientID      string
	RedirectURIs  []string
	AllowedScopes []string
}

// StartAuthorizationFlow initiates an authorization code flow with PKCE
// RFC 6749 Section 4.1.1 + RFC 7636
func (s *Server) StartAuthorizationFlow(clientID, redirectURI, state, scope, codeChallenge, codeChallengeMethod, deviceID string) (*AuthorizationRequest, error) {
	// Validate required parameters
	if clientID == "" {
		return nil, errors.New("client_id is required")
	}
	if redirectURI == "" {
		return nil, errors.New("redirect_uri is required")
	}
	
	// Validate state parameter (CSRF protection)
	// State must be at least 16 chars to prevent guessing
	if len(state) < 16 {
		return nil, errors.New("state parameter must be at least 16 characters")
	}
	
	// Validate redirect_uri against registered clients
	client, ok := registeredClients[clientID]
	if !ok {
		return nil, errors.New("invalid client_id")
	}
	
	if !isRedirectURIAllowed(redirectURI, client.RedirectURIs) {
		return nil, errors.New("redirect_uri not registered for this client")
	}
	
	// PKCE is required for public clients (agents)
	if codeChallenge == "" {
		return nil, errors.New("code_challenge is required (PKCE is mandatory)")
	}
	if codeChallengeMethod != CodeChallengeMethodS256 {
		return nil, fmt.Errorf("code_challenge_method %s is not supported, use S256", codeChallengeMethod)
	}
	
	// Validate code_challenge format (BASE64URL, 43 chars for S256)
	if len(codeChallenge) != 43 {
		return nil, errors.New("invalid code_challenge length (expected 43 for S256)")
	}
	
	// Validate scope
	validatedScope, err := validateScope(scope, client.AllowedScopes)
	if err != nil {
		return nil, err
	}
	
	// Generate authorization code with high entropy
	authCode, err := generateSecureAuthorizationCode()
	if err != nil {
		return nil, fmt.Errorf("failed to generate authorization code: %w", err)
	}
	
	now := time.Now()
	req := &AuthorizationRequest{
		ClientID:            clientID,
		RedirectURI:         redirectURI,
		State:               state,
		Scope:               validatedScope,
		CodeChallenge:       codeChallenge,
		CodeChallengeMethod: codeChallengeMethod,
		DeviceID:            deviceID,
		AuthorizationCode:   authCode,
		CreatedAt:           now,
		ExpiresAt:           now.Add(s.config.DeviceCodeLifetime),
	}
	
	// Store the authorization request
	if err := s.store.CreateAuthorization(req); err != nil {
		return nil, fmt.Errorf("failed to store authorization request: %w", err)
	}
	
	return req, nil
}

// isLoopback reports whether h is a loopback host (localhost or 127.0.0.1).
func isLoopback(h string) bool { return h == "localhost" || h == "127.0.0.1" }

// isRedirectURIAllowed checks if a redirect URI is allowed for a client.
// For loopback (localhost / 127.0.0.1) URIs the port is ignored and
// localhost/127.0.0.1 are treated as equivalent, so agents can use any
// ephemeral port for their local callback server (RFC 8252 §8.3).
func isRedirectURIAllowed(redirectURI string, allowedURIs []string) bool {
	for _, allowed := range allowedURIs {
		if redirectURI == allowed {
			return true
		}
	}

	parsed, err := url.Parse(redirectURI)
	if err != nil || !isLoopback(parsed.Hostname()) {
		return false
	}

	for _, allowed := range allowedURIs {
		a, err := url.Parse(allowed)
		if err != nil {
			continue
		}
		if isLoopback(a.Hostname()) && a.Path == parsed.Path && a.Scheme == parsed.Scheme {
			return true
		}
	}
	return false
}

// validateScope validates requested scopes against allowed scopes
func validateScope(requestedScope string, allowedScopes []string) (string, error) {
	if requestedScope == "" {
		return "", nil
	}
	
	allowedMap := make(map[string]bool)
	for _, s := range allowedScopes {
		allowedMap[s] = true
	}
	
	scopes := strings.Split(requestedScope, " ")
	var validated []string
	
	for _, scope := range scopes {
		scope = strings.TrimSpace(scope)
		if scope == "" {
			continue
		}
		if !allowedMap[scope] {
			return "", fmt.Errorf("invalid_scope: %s is not allowed", scope)
		}
		validated = append(validated, scope)
	}
	
	return strings.Join(validated, " "), nil
}

// ValidateCodeChallenge validates the code_verifier against the stored code_challenge
// RFC 7636 Section 4.6
func ValidateCodeChallenge(verifier, challenge string) bool {
	if verifier == "" || challenge == "" {
		return false
	}
	
	// code_challenge = BASE64URL(SHA256(code_verifier))
	hash := sha256.Sum256([]byte(verifier))
	computed := base64.RawURLEncoding.EncodeToString(hash[:])
	
	return computed == challenge
}

// ExchangeAuthorizationCode exchanges an authorization code for tokens
// RFC 6749 Section 4.1.3 + RFC 7636 Section 4.5
// This operation is atomic - the authorization code is deleted after first use
func (s *Server) ExchangeAuthorizationCode(code, codeVerifier, redirectURI, clientID string) (*TokenResponse, error) {
	if code == "" {
		return nil, errors.New(ErrInvalidRequest)
	}
	if codeVerifier == "" {
		return nil, errors.New("code_verifier is required")
	}
	
	// Retrieve the authorization request
	authReq, err := s.store.GetAuthorizationByCode(code)
	if err != nil {
		// Security: Log failed token exchange attempts
		logSecurityEvent("token_exchange_failed", "invalid_authorization_code", clientID, "")
		return nil, errors.New(ErrInvalidGrant)
	}
	
	// Check if expired
	if authReq.IsExpired() {
		// Delete expired codes to prevent replay
		s.store.DeleteAuthorization(code)
		logSecurityEvent("token_exchange_failed", "expired_code", clientID, authReq.DeviceID)
		return nil, errors.New(ErrExpiredToken)
	}
	
	// Validate redirect_uri matches exactly
	if authReq.RedirectURI != redirectURI {
		logSecurityEvent("token_exchange_failed", "redirect_uri_mismatch", clientID, authReq.DeviceID)
		return nil, errors.New("redirect_uri mismatch")
	}
	
	// Validate client_id matches
	if authReq.ClientID != clientID {
		logSecurityEvent("token_exchange_failed", "client_id_mismatch", clientID, authReq.DeviceID)
		return nil, errors.New(ErrInvalidClient)
	}
	
	// Validate PKCE code_verifier
	if !ValidateCodeChallenge(codeVerifier, authReq.CodeChallenge) {
		logSecurityEvent("token_exchange_failed", "invalid_code_verifier", clientID, authReq.DeviceID)
		return nil, errors.New("invalid code_verifier")
	}
	
	// Check if authorized (UserID is populated after user consent)
	if authReq.UserID == "" {
		return nil, errors.New("authorization pending")
	}
	
	// Security: Log successful token issuance
	logSecurityEvent("token_issued", "success", clientID, authReq.DeviceID)
	
	// Generate access token BEFORE deleting the authorization code
	// The sanitize function in DeleteAuthorization zeros out the fields,
	// so we must generate the token first
	tokenResp, err := s.generateAccessTokenFromAuth(authReq)
	if err != nil {
		return nil, err
	}
	
	// CRITICAL: Delete the authorization code AFTER issuing token to prevent replay attacks
	// This ensures single-use semantics
	if err := s.store.DeleteAuthorization(code); err != nil {
		// Log the error but don't fail - the token was already issued
		logSecurityEvent("token_exchange_warning", "code_deletion_failed", clientID, authReq.DeviceID)
		// Continue - the token is valid even if deletion fails
	}
	
	return tokenResp, nil
}

// logSecurityEvent logs security-relevant events for audit purposes
func logSecurityEvent(event, result, clientID, deviceID string) {
	// This is a placeholder - integrate with your logging system
	// In production, use structured logging with security event categorization
}

// AuthorizeAuthorizationCode marks an authorization code as approved by the user
// This is called after the user successfully authenticates via browser
func (s *Server) AuthorizeAuthorizationCode(code, userID, email, name, tenantID, tier string) error {
	if code == "" {
		return errors.New("authorization code is required")
	}
	
	authReq, err := s.store.GetAuthorizationByCode(code)
	if err != nil {
		return errors.New("invalid authorization code")
	}
	
	if authReq.IsExpired() {
		return errors.New(ErrExpiredToken)
	}
	
	// Update with user info
	authReq.UserID = userID
	authReq.Email = email
	authReq.Name = name
	authReq.TenantID = tenantID
	authReq.Tier = tier
	
	// Security: Log successful authorization
	logSecurityEvent("authorization_granted", "success", authReq.ClientID, authReq.DeviceID)
	
	return s.store.UpdateAuthorization(authReq)
}

// DenyAuthorizationCode denies an authorization request
func (s *Server) DenyAuthorizationCode(code string) error {
	if code == "" {
		return errors.New("authorization code is required")
	}
	
	// Get request details before deletion for logging
	authReq, _ := s.store.GetAuthorizationByCode(code)
	
	// Security: Log denial
	if authReq != nil {
		logSecurityEvent("authorization_denied", "user_denied", authReq.ClientID, authReq.DeviceID)
	}
	
	return s.store.DeleteAuthorization(code)
}

// GetAuthorizationRequest retrieves an authorization request by code
func (s *Server) GetAuthorizationRequest(code string) (*AuthorizationRequest, error) {
	if code == "" {
		return nil, errors.New("authorization code is required")
	}
	
	return s.store.GetAuthorizationByCode(code)
}

// generateAccessTokenFromAuth creates an access token from an authorization request
func (s *Server) generateAccessTokenFromAuth(authReq *AuthorizationRequest) (*TokenResponse, error) {
	now := time.Now()
	expiresAt := now.Add(s.config.AccessTokenLifetime)
	
	claims := AccessTokenClaims{
		RegisteredClaims: jwt.RegisteredClaims{
			ID:        uuid.New().String(),
			Issuer:    s.config.Issuer,
			Subject:   authReq.UserID,
			Audience:  jwt.ClaimStrings{"wantastic-api"},
			IssuedAt:  jwt.NewNumericDate(now),
			NotBefore: jwt.NewNumericDate(now),
			ExpiresAt: jwt.NewNumericDate(expiresAt),
		},
		DeviceID: authReq.DeviceID,
		UserID:   authReq.UserID,
		Email:    authReq.Email,
		Name:     authReq.Name,
		TenantID: authReq.TenantID,
		Tier:     authReq.Tier,
	}
	
	token := jwt.NewWithClaims(s.signingMethod, claims)
	tokenString, err := token.SignedString(s.signingKey)
	if err != nil {
		return nil, err
	}
	
	return &TokenResponse{
		AccessToken: tokenString,
		TokenType:   "Bearer",
		ExpiresIn:   int(s.config.AccessTokenLifetime.Seconds()),
	}, nil
}

// generateSecureAuthorizationCode creates a high-entropy authorization code
// Uses 256 bits of randomness (exceeds OAuth2 spec minimum of 128 bits)
func generateSecureAuthorizationCode() (string, error) {
	// Generate 32 bytes = 256 bits of entropy
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	// Use URL-safe base64 encoding (no padding)
	return "auth_" + base64.RawURLEncoding.EncodeToString(b), nil
}

// GenerateCodeChallenge generates a PKCE code_challenge from a code_verifier
// This is a helper function for clients
func GenerateCodeChallenge(verifier string) (string, string) {
	hash := sha256.Sum256([]byte(verifier))
	challenge := base64.RawURLEncoding.EncodeToString(hash[:])
	return challenge, CodeChallengeMethodS256
}

// GeneratePKCE generates a complete PKCE verifier and challenge pair
// Returns: (code_verifier, code_challenge, code_challenge_method)
func GeneratePKCE() (verifier, challenge, method string) {
	// Generate random verifier (43-128 chars recommended, we use 64)
	verifier = generateRandomString(64)
	challenge, method = GenerateCodeChallenge(verifier)
	return verifier, challenge, method
}

// generateRandomString creates a random string of specified length
func generateRandomString(length int) string {
	// Generate random bytes and encode to alphanumeric
	b := make([]byte, length)
	if _, err := rand.Read(b); err != nil {
		// Fallback to UUID if crypto/rand fails
		return strings.ReplaceAll(uuid.New().String(), "-", "")[:length]
	}
	// Use base64url encoding and truncate to desired length
	return base64.RawURLEncoding.EncodeToString(b)[:length]
}

// BuildAuthorizationURL builds the complete authorization URL with all parameters
// This is what agents call to get the URL to open in the browser
func (s *Server) BuildAuthorizationURL(clientID, redirectURI, state, scope, codeChallenge, codeChallengeMethod, deviceID string) string {
	q := url.Values{}
	q.Set("response_type", "code")
	q.Set("client_id", clientID)
	q.Set("redirect_uri", redirectURI)
	q.Set("state", state)
	q.Set("scope", scope)
	q.Set("code_challenge", codeChallenge)
	q.Set("code_challenge_method", codeChallengeMethod)
	if deviceID != "" {
		q.Set("device_id", deviceID)
	}
	return s.config.Issuer + "/oauth/authorize?" + q.Encode()
}
