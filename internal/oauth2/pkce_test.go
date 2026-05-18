// Package oauth2 provides PKCE (RFC 7636) tests
package oauth2

import (
	"strings"
	"testing"
	"time"
)

// TestGeneratePKCE verifies PKCE code generation
func TestGeneratePKCE(t *testing.T) {
	verifier, challenge, method := GeneratePKCE()
	
	// Verify verifier length (43-128 chars per RFC 7636)
	if len(verifier) < 43 {
		t.Errorf("verifier too short: %d chars", len(verifier))
	}
	if len(verifier) > 128 {
		t.Errorf("verifier too long: %d chars", len(verifier))
	}
	
	// Verify challenge is S256 hash of verifier (43 chars for SHA256 base64url)
	if len(challenge) != 43 {
		t.Errorf("challenge should be 43 chars for S256, got %d", len(challenge))
	}
	
	// Verify method is S256
	if method != "S256" {
		t.Errorf("method should be S256, got %s", method)
	}
	
	// Verify challenge is deterministic (same verifier always produces same challenge)
	challenge2, _ := GenerateCodeChallenge(verifier)
	if challenge != challenge2 {
		t.Error("challenge generation is not deterministic")
	}
}

// TestValidateCodeChallenge verifies PKCE verification
func TestValidateCodeChallenge(t *testing.T) {
	// Generate a valid pair
	verifier, challenge, _ := GeneratePKCE()
	
	// Valid verification
	if !ValidateCodeChallenge(verifier, challenge) {
		t.Error("Valid code challenge should validate")
	}
	
	// Invalid verifier
	if ValidateCodeChallenge("wrong_verifier", challenge) {
		t.Error("Invalid verifier should not validate")
	}
	
	// Invalid challenge
	if ValidateCodeChallenge(verifier, "wrong_challenge") {
		t.Error("Invalid challenge should not validate")
	}
	
	// Empty strings
	if ValidateCodeChallenge("", challenge) {
		t.Error("Empty verifier should not validate")
	}
	if ValidateCodeChallenge(verifier, "") {
		t.Error("Empty challenge should not validate")
	}
}

// TestPKCECodeChallengeFormat verifies the S256 hash format
func TestPKCECodeChallengeFormat(t *testing.T) {
	verifier := "test_verifier_12345"
	challenge, method := GenerateCodeChallenge(verifier)
	
	// Should be base64url encoded (no padding, no +/ chars)
	if strings.Contains(challenge, "+") || strings.Contains(challenge, "/") {
		t.Error("challenge should be base64url (no + or /)")
	}
	if strings.Contains(challenge, "=") {
		t.Error("challenge should have no padding (no =)")
	}
	
	// Should be S256 method
	if method != CodeChallengeMethodS256 {
		t.Errorf("method should be S256, got %s", method)
	}
}

// TestServer_StartAuthorizationFlow verifies authorization flow initiation
func TestServer_StartAuthorizationFlow(t *testing.T) {
	server, err := NewServer(DevConfig(), nil)
	if err != nil {
		t.Fatalf("Failed to create server: %v", err)
	}
	
	// Valid request with 43-char code_challenge (BASE64URL of SHA256)
	// Using registered client "wantastic-agent-client" and state >= 16 chars
	authReq, err := server.StartAuthorizationFlow(
		"wantastic-agent-client",
		"http://localhost:58250/callback",
		"state_12345678901234", // 20 chars, meets minimum 16
		"org:create_api_key user:profile",
		"dBjftJeZ4CVP-mB92K27uhbUJU1p1r_wW1gFWFOEjXk", // 43 chars (example S256 challenge)
		"S256",
		"device_123",
	)
	if err != nil {
		t.Fatalf("StartAuthorizationFlow failed: %v", err)
	}
	
	// Verify response
	if authReq.AuthorizationCode == "" {
		t.Error("AuthorizationCode should not be empty")
	}
	if authReq.ClientID != "wantastic-agent-client" {
		t.Errorf("ClientID mismatch: got %s", authReq.ClientID)
	}
	if authReq.State != "state_12345678901234" {
		t.Errorf("State mismatch: got %s", authReq.State)
	}
	if !strings.HasPrefix(authReq.AuthorizationCode, "auth_") {
		t.Error("AuthorizationCode should start with auth_ prefix")
	}
}

// TestServer_StartAuthorizationFlow_Validation verifies input validation
func TestServer_StartAuthorizationFlow_Validation(t *testing.T) {
	server, err := NewServer(DevConfig(), nil)
	if err != nil {
		t.Fatalf("Failed to create server: %v", err)
	}
	
	tests := []struct {
		name        string
		clientID    string
		redirectURI string
		challenge   string
		method      string
		wantErr     bool
	}{
		{
			name:        "missing client_id",
			clientID:    "",
			redirectURI: "http://localhost/callback",
			challenge:   "code_challenge_1234567890123456789012345678901234567890123",
			method:      "S256",
			wantErr:     true,
		},
		{
			name:        "missing redirect_uri",
			clientID:    "test-client",
			redirectURI: "",
			challenge:   "code_challenge_1234567890123456789012345678901234567890123",
			method:      "S256",
			wantErr:     true,
		},
		{
			name:        "missing code_challenge",
			clientID:    "test-client",
			redirectURI: "http://localhost/callback",
			challenge:   "",
			method:      "S256",
			wantErr:     true,
		},
		{
			name:        "unsupported method",
			clientID:    "test-client",
			redirectURI: "http://localhost/callback",
			challenge:   "code_challenge_1234567890123456789012345678901234567890123",
			method:      "plain",
			wantErr:     true,
		},
		{
			name:        "invalid challenge length",
			clientID:    "test-client",
			redirectURI: "http://localhost/callback",
			challenge:   "too_short",
			method:      "S256",
			wantErr:     true,
		},
	}
	
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := server.StartAuthorizationFlow(
				tt.clientID,
				tt.redirectURI,
				"state",
				"scope",
				tt.challenge,
				tt.method,
				"device",
			)
			if (err != nil) != tt.wantErr {
				t.Errorf("StartAuthorizationFlow() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

// TestServer_ExchangeAuthorizationCode verifies the full PKCE flow
func TestServer_ExchangeAuthorizationCode(t *testing.T) {
	server, err := NewServer(DevConfig(), nil)
	if err != nil {
		t.Fatalf("Failed to create server: %v", err)
	}
	
	// Generate PKCE pair
	verifier, challenge, _ := GeneratePKCE()
	
	// Start authorization flow (use registered client)
	authReq, err := server.StartAuthorizationFlow(
		"wantastic-agent-client",
		"http://localhost:58250/callback",
		"state_12345678901234",
		"org:create_api_key",
		challenge,
		"S256",
		"device_123",
	)
	if err != nil {
		t.Fatalf("StartAuthorizationFlow failed: %v", err)
	}
	
	// Try to exchange before authorization (should fail)
	_, err = server.ExchangeAuthorizationCode(
		authReq.AuthorizationCode,
		verifier,
		"http://localhost:58250/callback",
		"wantastic-agent-client",
	)
	if err == nil {
		t.Error("Exchange should fail before user authorization")
	}
	
	// Authorize the request
	err = server.AuthorizeAuthorizationCode(
		authReq.AuthorizationCode,
		"user_123",
		"test@example.com",
		"Test User",
		"tenant_456",
		"free",
	)
	if err != nil {
		t.Fatalf("AuthorizeAuthorizationCode failed: %v", err)
	}
	
	// Now exchange should succeed
	tokenResp, err := server.ExchangeAuthorizationCode(
		authReq.AuthorizationCode,
		verifier,
		"http://localhost:58250/callback",
		"wantastic-agent-client",
	)
	if err != nil {
		t.Fatalf("Exchange failed: %v", err)
	}
	
	// Verify token
	if tokenResp.AccessToken == "" {
		t.Error("AccessToken should not be empty")
	}
	if tokenResp.TokenType != "Bearer" {
		t.Errorf("TokenType should be Bearer, got %s", tokenResp.TokenType)
	}
	if tokenResp.ExpiresIn <= 0 {
		t.Error("ExpiresIn should be positive")
	}
	
	// Validate the token
	claims, err := server.ValidateAccessToken(tokenResp.AccessToken)
	if err != nil {
		t.Fatalf("ValidateAccessToken failed: %v", err)
	}
	if claims.UserID != "user_123" {
		t.Errorf("UserID mismatch: got %s", claims.UserID)
	}
	if claims.Email != "test@example.com" {
		t.Errorf("Email mismatch: got %s", claims.Email)
	}
	if claims.TenantID != "tenant_456" {
		t.Errorf("TenantID mismatch: got %s", claims.TenantID)
	}
}

// TestServer_ExchangeAuthorizationCode_InvalidVerifier verifies PKCE verification
func TestServer_ExchangeAuthorizationCode_InvalidVerifier(t *testing.T) {
	server, err := NewServer(DevConfig(), nil)
	if err != nil {
		t.Fatalf("Failed to create server: %v", err)
	}
	
	// Generate PKCE pair
	_, challenge, _ := GeneratePKCE()
	
	// Start authorization flow (use registered client)
	authReq, err := server.StartAuthorizationFlow(
		"wantastic-agent-client",
		"http://localhost:58250/callback",
		"state_12345678901234",
		"org:create_api_key",
		challenge,
		"S256",
		"device",
	)
	if err != nil {
		t.Fatalf("StartAuthorizationFlow failed: %v", err)
	}
	
	// Authorize
	server.AuthorizeAuthorizationCode(authReq.AuthorizationCode, "user", "email", "name", "tenant", "tier")
	
	// Try to exchange with wrong verifier
	_, err = server.ExchangeAuthorizationCode(
		authReq.AuthorizationCode,
		"wrong_verifier",
		"http://localhost:58250/callback",
		"wantastic-agent-client",
	)
	if err == nil {
		t.Error("Exchange should fail with invalid verifier")
	}
}

// TestServer_ExchangeAuthorizationCode_Expired verifies expiration handling
func TestServer_ExchangeAuthorizationCode_Expired(t *testing.T) {
	config := DevConfig()
	config.DeviceCodeLifetime = 100 * time.Millisecond // Short for testing
	
	server, err := NewServer(config, nil)
	if err != nil {
		t.Fatalf("Failed to create server: %v", err)
	}
	
	// Generate PKCE pair
	verifier, challenge, _ := GeneratePKCE()
	
	// Start authorization flow (use registered client)
	authReq, err := server.StartAuthorizationFlow(
		"wantastic-agent-client",
		"http://localhost:58250/callback",
		"state_12345678901234",
		"org:create_api_key",
		challenge,
		"S256",
		"device",
	)
	if err != nil {
		t.Fatalf("StartAuthorizationFlow failed: %v", err)
	}
	
	// Wait for expiration
	time.Sleep(200 * time.Millisecond)
	
	// Try to exchange expired code
	_, err = server.ExchangeAuthorizationCode(
		authReq.AuthorizationCode,
		verifier,
		"http://localhost:58250/callback",
		"wantastic-agent-client",
	)
	if err == nil {
		t.Error("Exchange should fail with expired code")
	}
}

// TestServer_BuildAuthorizationURL verifies URL construction
func TestServer_BuildAuthorizationURL(t *testing.T) {
	server, err := NewServer(DevConfig(), nil)
	if err != nil {
		t.Fatalf("Failed to create server: %v", err)
	}
	
	url := server.BuildAuthorizationURL(
		"test-client",
		"http://localhost/callback",
		"state_123",
		"org:create_api_key user:profile",
		"challenge_123",
		"S256",
		"device_123",
	)
	
	// Verify URL components
	if !strings.Contains(url, "/oauth/authorize") {
		t.Error("URL should contain /oauth/authorize")
	}
	if !strings.Contains(url, "response_type=code") {
		t.Error("URL should contain response_type=code")
	}
	if !strings.Contains(url, "client_id=test-client") {
		t.Error("URL should contain client_id")
	}
	if !strings.Contains(url, "code_challenge=challenge_123") {
		t.Error("URL should contain code_challenge")
	}
	if !strings.Contains(url, "code_challenge_method=S256") {
		t.Error("URL should contain code_challenge_method=S256")
	}
	if !strings.Contains(url, "device_id=device_123") {
		t.Error("URL should contain device_id")
	}
}

// TestServer_DenyAuthorizationCode verifies denial flow
func TestServer_DenyAuthorizationCode(t *testing.T) {
	server, err := NewServer(DevConfig(), nil)
	if err != nil {
		t.Fatalf("Failed to create server: %v", err)
	}
	
	// Generate PKCE pair
	_, challenge, _ := GeneratePKCE()
	
	// Start authorization flow (use registered client)
	authReq, err := server.StartAuthorizationFlow(
		"wantastic-agent-client",
		"http://localhost:58250/callback",
		"state_12345678901234",
		"org:create_api_key",
		challenge,
		"S256",
		"device",
	)
	if err != nil {
		t.Fatalf("StartAuthorizationFlow failed: %v", err)
	}
	
	// Deny the request
	err = server.DenyAuthorizationCode(authReq.AuthorizationCode)
	if err != nil {
		t.Fatalf("DenyAuthorizationCode failed: %v", err)
	}
	
	// Try to get the denied request
	_, err = server.GetAuthorizationRequest(authReq.AuthorizationCode)
	if err == nil {
		t.Error("Should not be able to get denied request")
	}
}
