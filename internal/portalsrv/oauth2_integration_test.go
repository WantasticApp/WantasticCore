package portalsrv

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"strings"
	"testing"
	"time"

	"WantasticCore/internal/portalsrv/pkg/cipher"
	"WantasticCore/internal/portalsrv/pkg/session"
	"WantasticCore/internal/oauth2"
)

// TestInternalOAuth2DeviceFlow tests the complete internal OAuth2 device flow
func TestInternalOAuth2DeviceFlow(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	// Start test server
	portal := setupTestPortal(t)
	defer portal.Shutdown()

	ctx := context.Background()

	// Step 1: Start device flow
	deviceID := generateTestDeviceID()
	flowResp, err := portal.StartDeviceFlow(ctx, deviceID)
	if err != nil {
		t.Fatalf("StartDeviceFlow failed: %v", err)
	}

	// Verify response
	if flowResp.DeviceCode == "" {
		t.Error("DeviceCode is empty")
	}
	if flowResp.UserCode == "" {
		t.Error("UserCode is empty")
	}
	if !strings.Contains(flowResp.VerificationURI, "/activate") {
		t.Errorf("VerificationURI missing /activate: %s", flowResp.VerificationURI)
	}
	if flowResp.ExpiresIn <= 0 {
		t.Errorf("ExpiresIn should be positive: %d", flowResp.ExpiresIn)
	}
	if flowResp.Interval <= 0 {
		t.Errorf("Interval should be positive: %d", flowResp.Interval)
	}

	// Step 2: Poll for token (should be pending)
	pollResp, err := portal.PollDeviceToken(ctx, flowResp.DeviceCode)
	if err == nil {
		t.Error("Expected error for pending authorization")
	}
	if err != nil && err.Error() != oauth2.ErrAuthorizationPending {
		t.Errorf("Expected authorization_pending, got: %v", err)
	}
	if pollResp != nil {
		t.Error("Expected nil response for pending")
	}

	t.Logf("Device flow started: user_code=%s", flowResp.UserCode)
}

// TestInternalOAuth2Authorization tests user authorization flow
func TestInternalOAuth2Authorization(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	portal := setupTestPortal(t)
	defer portal.Shutdown()

	ctx := context.Background()

	// Start device flow
	deviceID := generateTestDeviceID()
	flowResp, err := portal.StartDeviceFlow(ctx, deviceID)
	if err != nil {
		t.Fatalf("StartDeviceFlow failed: %v", err)
	}

	// Get pending request
	req, err := portal.GetPendingRequest(flowResp.UserCode)
	if err != nil {
		t.Fatalf("GetPendingRequest failed: %v", err)
	}
	if req.UserCode != flowResp.UserCode {
		t.Errorf("User code mismatch: %s vs %s", req.UserCode, flowResp.UserCode)
	}

	// Authorize the device
	err = portal.AuthorizeDevice(flowResp.UserCode, "user-123", "test@example.com", "Test User", "tenant-456", "free")
	if err != nil {
		t.Fatalf("AuthorizeDevice failed: %v", err)
	}

	// Poll should now succeed
	tokenResp, err := portal.PollDeviceToken(ctx, flowResp.DeviceCode)
	if err != nil {
		t.Fatalf("PollDeviceToken after auth failed: %v", err)
	}
	if tokenResp.AccessToken == "" {
		t.Error("AccessToken is empty")
	}
	if tokenResp.TokenType != "Bearer" {
		t.Errorf("Expected TokenType Bearer, got: %s", tokenResp.TokenType)
	}
	if tokenResp.ExpiresIn <= 0 {
		t.Errorf("Expected positive ExpiresIn, got: %d", tokenResp.ExpiresIn)
	}

	// Validate the token
	claims, err := portal.ValidateAccessToken(tokenResp.AccessToken)
	if err != nil {
		t.Fatalf("ValidateAccessToken failed: %v", err)
	}
	if claims.UserID != "user-123" {
		t.Errorf("UserID mismatch: %s vs user-123", claims.UserID)
	}
	if claims.Email != "test@example.com" {
		t.Errorf("Email mismatch: %s vs test@example.com", claims.Email)
	}

	t.Logf("Authorization successful, token valid for %d seconds", tokenResp.ExpiresIn)
}

// TestInternalOAuth2Deny tests device denial flow
func TestInternalOAuth2Deny(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	portal := setupTestPortal(t)
	defer portal.Shutdown()

	ctx := context.Background()

	// Start device flow
	deviceID := generateTestDeviceID()
	flowResp, err := portal.StartDeviceFlow(ctx, deviceID)
	if err != nil {
		t.Fatalf("StartDeviceFlow failed: %v", err)
	}

	// Deny the device
	err = portal.DenyDevice(flowResp.UserCode)
	if err != nil {
		t.Fatalf("DenyDevice failed: %v", err)
	}

	// Poll should return access_denied
	_, err = portal.PollDeviceToken(ctx, flowResp.DeviceCode)
	if err == nil {
		t.Fatal("Expected error for denied device")
	}
	if err.Error() != oauth2.ErrAccessDenied {
		t.Errorf("Expected access_denied, got: %v", err)
	}
}

// TestInternalOAuth2Expiration tests device code expiration
func TestInternalOAuth2Expiration(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	// Use dev config with very short expiration for testing
	config := oauth2.DevConfig()
	config.DeviceCodeLifetime = 1 * time.Second

	portal := setupTestPortalWithConfig(t, config)
	defer portal.Shutdown()

	ctx := context.Background()

	// Start device flow
	deviceID := generateTestDeviceID()
	flowResp, err := portal.StartDeviceFlow(ctx, deviceID)
	if err != nil {
		t.Fatalf("StartDeviceFlow failed: %v", err)
	}

	// Wait for expiration
	time.Sleep(2 * time.Second)

	// Poll should return expired_token
	_, err = portal.PollDeviceToken(ctx, flowResp.DeviceCode)
	if err == nil {
		t.Fatal("Expected error for expired code")
	}
	if err.Error() != oauth2.ErrExpiredToken {
		t.Errorf("Expected expired_token, got: %v", err)
	}
}

// TestInternalOAuth2HTTPEndpoints tests the HTTP OAuth2 endpoints
func TestInternalOAuth2HTTPEndpoints(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	portal := setupTestPortal(t)
	defer portal.Shutdown()

	baseURL := portal.HTTPAddr()

	// Test device authorization endpoint
	formData := url.Values{}
	formData.Set("client_id", "test-client")

	resp, err := http.PostForm(baseURL+"/oauth/device/code", formData)
	if err != nil {
		t.Fatalf("Failed to call /oauth/device/code: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("Expected 200, got: %d", resp.StatusCode)
	}

	var deviceAuthResp oauth2.DeviceAuthorizationResponse
	if err := json.NewDecoder(resp.Body).Decode(&deviceAuthResp); err != nil {
		t.Fatalf("Failed to decode response: %v", err)
	}

	if deviceAuthResp.DeviceCode == "" {
		t.Error("DeviceCode is empty")
	}
	if deviceAuthResp.UserCode == "" {
		t.Error("UserCode is empty")
	}

	t.Logf("HTTP device auth: user_code=%s", deviceAuthResp.UserCode)
}

// TestInternalOAuth2MachineIDValidation tests device ID validation
func TestInternalOAuth2MachineIDValidation(t *testing.T) {
	testCases := []struct {
		name     string
		deviceID string
		wantErr  bool
	}{
		{
			name:     "valid hashed ID",
			deviceID: generateTestDeviceID(),
			wantErr:  false,
		},
		{
			name:     "empty ID",
			deviceID: "",
			wantErr:  true,
		},
		{
			name:     "too short",
			deviceID: "abc123",
			wantErr:  true,
		},
		{
			name:     "not hex",
			deviceID: "ghijklmnopqrstuvwxyz",
			wantErr:  true,
		},
		{
			name:     "low entropy (all same chars)",
			deviceID: strings.Repeat("a", 64),
			wantErr:  true,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			err := cipher.ValidateMachineID(tc.deviceID)
			if tc.wantErr && err == nil {
				t.Error("Expected error, got nil")
			}
			if !tc.wantErr && err != nil {
				t.Errorf("Unexpected error: %v", err)
			}
		})
	}
}

// TestInternalOAuth2DevProdModes tests dev vs prod configuration
func TestInternalOAuth2DevProdModes(t *testing.T) {
	testCases := []struct {
		issuer string
		isDev  bool
	}{
		{"https://wantastic.local", true},
		{"http://wantastic.local", true},
		{"http://localhost", true},
		{"http://127.0.0.1", true},
		{"https://console.wantastic.app", false},
		{"https://prod.example.com", false},
	}

	for _, tc := range testCases {
		t.Run(tc.issuer, func(t *testing.T) {
			got := oauth2.IsDevMode(tc.issuer)
			if got != tc.isDev {
				t.Errorf("IsDevMode(%q) = %v, want %v", tc.issuer, got, tc.isDev)
			}
		})
	}
}

// TestInternalOAuth2PKCEFlow tests the full PKCE authorization code flow
func TestInternalOAuth2PKCEFlow(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	portal := setupTestPortal(t)
	defer portal.Shutdown()

	// Step 1: Generate PKCE pair (client side)
	verifier, challenge, method := oauth2.GeneratePKCE()
	if method != "S256" {
		t.Errorf("Expected S256 method, got %s", method)
	}

	// Step 2: Start authorization flow (state must be >= 16 chars for CSRF protection)
	authReq, err := portal.oauth2Server.StartAuthorizationFlow(
		"wantastic-agent-client",
		"http://localhost:58250/callback",
		"state_abc123456789",
		"org:create_api_key user:profile",
		challenge,
		method,
		generateTestDeviceID(),
	)
	if err != nil {
		t.Fatalf("StartAuthorizationFlow failed: %v", err)
	}

	// Verify authorization code was generated
	if authReq.AuthorizationCode == "" {
		t.Error("AuthorizationCode is empty")
	}
	if authReq.State != "state_abc123456789" {
		t.Errorf("State mismatch: %s vs state_abc123456789", authReq.State)
	}

	// Step 3: Try to exchange before authorization (should fail)
	_, err = portal.oauth2Server.ExchangeAuthorizationCode(
		authReq.AuthorizationCode,
		verifier,
		"http://localhost:58250/callback",
		"wantastic-agent-client",
	)
	if err == nil {
		t.Error("Expected error when exchanging unauthorized code")
	}

	// Step 4: Authorize the request (simulates user login and consent)
	err = portal.oauth2Server.AuthorizeAuthorizationCode(
		authReq.AuthorizationCode,
		"user_123",
		"admin@example.com",
		"Admin User",
		"tenant_456",
		"enterprise",
	)
	if err != nil {
		t.Fatalf("AuthorizeAuthorizationCode failed: %v", err)
	}

	// Step 5: Exchange code for token
	tokenResp, err := portal.oauth2Server.ExchangeAuthorizationCode(
		authReq.AuthorizationCode,
		verifier,
		"http://localhost:58250/callback",
		"wantastic-agent-client",
	)
	if err != nil {
		t.Fatalf("ExchangeAuthorizationCode failed: %v", err)
	}

	// Verify token response
	if tokenResp.AccessToken == "" {
		t.Error("AccessToken is empty")
	}
	if tokenResp.TokenType != "Bearer" {
		t.Errorf("Expected Bearer token type, got %s", tokenResp.TokenType)
	}
	if tokenResp.ExpiresIn <= 0 {
		t.Errorf("Expected positive ExpiresIn, got %d", tokenResp.ExpiresIn)
	}

	// Step 6: Validate the access token
	claims, err := portal.oauth2Server.ValidateAccessToken(tokenResp.AccessToken)
	if err != nil {
		t.Fatalf("ValidateAccessToken failed: %v", err)
	}
	if claims.UserID != "user_123" {
		t.Errorf("UserID mismatch: %s vs user_123", claims.UserID)
	}
	if claims.Email != "admin@example.com" {
		t.Errorf("Email mismatch: %s vs admin@example.com", claims.Email)
	}
	if claims.TenantID != "tenant_456" {
		t.Errorf("TenantID mismatch: %s vs tenant_456", claims.TenantID)
	}
	if claims.Tier != "enterprise" {
		t.Errorf("Tier mismatch: %s vs enterprise", claims.Tier)
	}

	t.Logf("PKCE flow completed successfully, token expires in %d seconds", tokenResp.ExpiresIn)
}

// TestInternalOAuth2PKCE_InvalidVerifier tests PKCE verification
func TestInternalOAuth2PKCE_InvalidVerifier(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	portal := setupTestPortal(t)
	defer portal.Shutdown()

	// Generate PKCE pair
	_, challenge, method := oauth2.GeneratePKCE()

	// Start authorization flow (state >= 16 chars, valid scope)
	authReq, err := portal.oauth2Server.StartAuthorizationFlow(
		"wantastic-agent-client",
		"http://localhost:58250/callback",
		"state_12345678901234",
		"org:create_api_key",
		challenge,
		method,
		generateTestDeviceID(),
	)
	if err != nil {
		t.Fatalf("StartAuthorizationFlow failed: %v", err)
	}

	// Authorize
	portal.oauth2Server.AuthorizeAuthorizationCode(authReq.AuthorizationCode, "user", "email", "name", "tenant", "tier")

	// Try to exchange with wrong verifier
	_, err = portal.oauth2Server.ExchangeAuthorizationCode(
		authReq.AuthorizationCode,
		"wrong_verifier",
		"http://localhost:58250/callback",
		"wantastic-agent-client",
	)
	if err == nil {
		t.Error("Expected error with invalid verifier")
	}
}

// TestInternalOAuth2PKCE_HTTPEndpoints tests PKCE HTTP endpoints
func TestInternalOAuth2PKCE_HTTPEndpoints(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	portal := setupTestPortal(t)
	defer portal.Shutdown()

	baseURL := portal.HTTPAddr()

	// Generate PKCE challenge
	_, challenge, _ := oauth2.GeneratePKCE()

	// Test authorization endpoint (use registered client and valid state >= 16 chars)
	authURL := fmt.Sprintf("%s/oauth/authorize?client_id=wantastic-agent-client&redirect_uri=http://localhost:58250/callback&state=xyz1234567890123&scope=org:create_api_key&code_challenge=%s&code_challenge_method=S256&device_id=%s",
		baseURL, challenge, generateTestDeviceID())

	resp, err := http.Get(authURL)
	if err != nil {
		t.Fatalf("Failed to call /oauth/authorize: %v", err)
	}
	defer resp.Body.Close()

	// Should redirect to consent page
	if resp.StatusCode != http.StatusFound && resp.StatusCode != http.StatusOK {
		t.Errorf("Expected 302 or 200, got: %d", resp.StatusCode)
	}

	// Check if we got redirected to consent page
	if resp.StatusCode == http.StatusFound {
		location := resp.Header.Get("Location")
		if !strings.Contains(location, "/oauth/consent") {
			t.Errorf("Expected redirect to /oauth/consent, got: %s", location)
		}
	}

	t.Logf("PKCE authorization endpoint responded correctly")
}

// Helper types and functions

type testPortal struct {
	oauth2Server *oauth2.Server
	httpServer   *http.Server
	baseURL      string
}

func (p *testPortal) StartDeviceFlow(ctx context.Context, deviceID string) (*oauth2.DeviceAuthorizationResponse, error) {
	return p.oauth2Server.StartDeviceFlow("test-client", deviceID)
}

func (p *testPortal) PollDeviceToken(ctx context.Context, deviceCode string) (*oauth2.TokenResponse, error) {
	return p.oauth2Server.PollDeviceToken(deviceCode)
}

func (p *testPortal) GetPendingRequest(userCode string) (*oauth2.DeviceRequest, error) {
	return p.oauth2Server.GetPendingRequest(userCode)
}

func (p *testPortal) AuthorizeDevice(userCode, userID, email, name, tenantID, tier string) error {
	return p.oauth2Server.AuthorizeDevice(userCode, userID, email, name, tenantID, tier)
}

func (p *testPortal) DenyDevice(userCode string) error {
	return p.oauth2Server.DenyDevice(userCode)
}

func (p *testPortal) ValidateAccessToken(token string) (*oauth2.AccessTokenClaims, error) {
	return p.oauth2Server.ValidateAccessToken(token)
}

func (p *testPortal) HTTPAddr() string {
	return p.baseURL
}

func (p *testPortal) Shutdown() {
	if p.httpServer != nil {
		p.httpServer.Close()
	}
}

func setupTestPortal(t *testing.T) *testPortal {
	return setupTestPortalWithConfig(t, oauth2.DevConfig())
}

func setupTestPortalWithConfig(t *testing.T, config *oauth2.Config) *testPortal {
	server, err := oauth2.NewServer(config, nil)
	if err != nil {
		t.Fatalf("Failed to create OAuth2 server: %v", err)
	}

	// Set up HTTP handlers for OAuth2 endpoints
	mux := http.NewServeMux()

	// Device authorization endpoint (RFC 8628)
	mux.HandleFunc("/oauth/device/code", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}

		clientID := r.FormValue("client_id")
		deviceID := r.FormValue("device_id")

		resp, err := server.StartDeviceFlow(clientID, deviceID)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	})

	// Authorization endpoint with PKCE (RFC 6749 + RFC 7636)
	mux.HandleFunc("/oauth/authorize", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}

		query := r.URL.Query()
		clientID := query.Get("client_id")
		redirectURI := query.Get("redirect_uri")
		state := query.Get("state")
		scope := query.Get("scope")
		codeChallenge := query.Get("code_challenge")
		codeChallengeMethod := query.Get("code_challenge_method")
		deviceID := query.Get("device_id")

		if clientID == "" || redirectURI == "" || codeChallenge == "" {
			http.Error(w, "Missing required parameters", http.StatusBadRequest)
			return
		}

		authReq, err := server.StartAuthorizationFlow(clientID, redirectURI, state, scope, codeChallenge, codeChallengeMethod, deviceID)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}

		// Redirect to consent page
		http.Redirect(w, r, "/oauth/consent?code="+authReq.AuthorizationCode, http.StatusFound)
	})

	// Consent page (simplified for testing - auto-approves)
	mux.HandleFunc("/oauth/consent", func(w http.ResponseWriter, r *http.Request) {
		code := r.URL.Query().Get("code")
		if code == "" {
			http.Error(w, "Missing authorization code", http.StatusBadRequest)
			return
		}
		// Auto-approve for testing
		server.AuthorizeAuthorizationCode(code, "test_user", "test@example.com", "Test User", "test_tenant", "free")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("Authorized"))
	})

	// Token endpoint
	mux.HandleFunc("/oauth/token", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}

		grantType := r.FormValue("grant_type")
		if grantType != "urn:ietf:params:oauth:grant-type:device_code" {
			http.Error(w, "Unsupported grant type", http.StatusBadRequest)
			return
		}

		deviceCode := r.FormValue("device_code")

		resp, err := server.PollDeviceToken(deviceCode)
		if err != nil {
			// Return proper OAuth2 error
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusBadRequest)
			json.NewEncoder(w).Encode(map[string]string{
				"error": err.Error(),
			})
			return
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	})

	httpServer := &http.Server{
		Addr:    "127.0.0.1:0", // Use random available port
		Handler: mux,
	}

	// Start server in background
	listener, err := net.Listen("tcp", httpServer.Addr)
	if err != nil {
		t.Fatalf("Failed to create listener: %v", err)
	}

	go func() {
		if err := httpServer.Serve(listener); err != nil && err != http.ErrServerClosed {
			t.Logf("HTTP server error: %v", err)
		}
	}()

	baseURL := fmt.Sprintf("http://%s", listener.Addr().String())

	return &testPortal{
		oauth2Server: server,
		httpServer:   httpServer,
		baseURL:      baseURL,
	}
}

func generateTestDeviceID() string {
	// Generate a valid hashed device ID
	rawID := fmt.Sprintf("test-machine-%d", time.Now().UnixNano())
	mac := hmac.New(sha256.New, []byte(cipher.SharedSecret))
	mac.Write([]byte(rawID))
	return hex.EncodeToString(mac.Sum(nil))
}

// ─── Full HTTP E2E Integration Tests ────────────────────────────────────────
// These tests replicate the exact path taken by wantasticd agents:
//   1. Agent opens browser at GET /oauth/authorize (with PKCE params)
//   2. Server redirects to /?code=AUTH_CODE#oauth2-consent
//   3. Browser SPA calls GET /api/oauth/consent-info → {authenticated, ...}
//   4. If not authenticated → POST /api/oauth/consent-login (or user already has session)
//   5. Browser SPA calls POST /api/oauth/authorize-confirm {action:"allow"}
//   6. Server returns {redirect_uri: "http://localhost:PORT/callback?code=...&state=..."}
//   7. SPA navigates to redirect_uri → agent's callback server receives the code
//   8. Agent POSTs /oauth/token to exchange code → access token

// setupRealPortalApp creates a minimal portalApp with the real oauth2 handlers,
// backed by an in-memory session store (no gRPC needed).
func setupRealPortalApp(t *testing.T) (*portalApp, *http.Server, string) {
	t.Helper()

	oauth2Cfg := oauth2.DevConfig()
	srv, err := oauth2.NewServer(oauth2Cfg, nil)
	if err != nil {
		t.Fatalf("NewServer: %v", err)
	}

	app := &portalApp{
		oauth2Server: srv,
		oauth2Issuer: "https://wantastic.local",
		oauth2Domain: "wantastic.local",
		sessionStore: session.NewInMemorySessionStore(),
		isSecure:     false,
	}

	mux := http.NewServeMux()
	// Register only the OAuth2 endpoints needed for the flow
	mux.HandleFunc("/oauth/authorize", app.handleOAuth2Authorize)
	mux.HandleFunc("/oauth/token", app.handleOAuth2Token)
	mux.HandleFunc("/oauth/token/", app.handleOAuth2Token)
	mux.HandleFunc("/oauth/device/code", app.handleOAuth2DeviceCode)
	mux.HandleFunc("/oauth/device/code/", app.handleOAuth2DeviceCode)
	mux.HandleFunc("/api/oauth/consent-info", app.handleOAuth2ConsentInfo)
	mux.HandleFunc("/api/oauth/consent-login", app.handleOAuth2ConsentLogin)
	mux.HandleFunc("/api/oauth/authorize-confirm", app.handleOAuth2AuthorizeConfirm)
	mux.HandleFunc("/api/oauth/pending-device", app.handleOAuth2PendingDevice)
	mux.HandleFunc("/api/oauth/approve", app.handleOAuth2Approve)
	mux.HandleFunc("/api/oauth/deny", app.handleOAuth2Deny)
	mux.HandleFunc("/device-login", app.handleDeviceLogin)
	mux.HandleFunc("/device-login/", app.handleDeviceLogin)
	mux.HandleFunc("/activate", app.handleOAuth2Activate)

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("net.Listen: %v", err)
	}
	httpSrv := &http.Server{Handler: mux}
	go httpSrv.Serve(ln)
	t.Cleanup(func() { httpSrv.Close() })

	baseURL := "http://" + ln.Addr().String()
	return app, httpSrv, baseURL
}

func TestRealPortalHTTPDeviceCodeResponseIsJSONAndStoresIDs(t *testing.T) {
	app, _, portalBaseURL := setupRealPortalApp(t)

	form := url.Values{}
	form.Set("client_id", "wantastic_cipher_v_1_0_0")
	form.Set("device_id", "macos-device-123")

	resp, err := http.PostForm(portalBaseURL+"/oauth/device/code", form)
	if err != nil {
		t.Fatalf("POST /oauth/device/code: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status=%d, want=%d", resp.StatusCode, http.StatusOK)
	}
	if ct := resp.Header.Get("Content-Type"); !strings.Contains(ct, "application/json") {
		t.Fatalf("Content-Type=%q, want application/json", ct)
	}

	var deviceResp oauth2.DeviceAuthorizationResponse
	if err := json.NewDecoder(resp.Body).Decode(&deviceResp); err != nil {
		t.Fatalf("decode device response: %v", err)
	}
	if deviceResp.DeviceCode == "" || deviceResp.UserCode == "" {
		t.Fatalf("missing device/user code in response: %+v", deviceResp)
	}
	if !strings.Contains(deviceResp.VerificationURI, "/activate") {
		t.Fatalf("verification_uri=%q, want /activate", deviceResp.VerificationURI)
	}
	if !strings.Contains(deviceResp.VerificationURIComplete, "user_code="+url.QueryEscape(deviceResp.UserCode)) {
		t.Fatalf("verification_uri_complete=%q, want encoded user_code", deviceResp.VerificationURIComplete)
	}

	pending, err := app.oauth2Server.GetPendingRequest(deviceResp.UserCode)
	if err != nil {
		t.Fatalf("GetPendingRequest: %v", err)
	}
	if pending.ClientID != "wantastic_cipher_v_1_0_0" {
		t.Fatalf("ClientID=%q, want wantastic_cipher_v_1_0_0", pending.ClientID)
	}
	if pending.DeviceID != "macos-device-123" {
		t.Fatalf("DeviceID=%q, want macos-device-123", pending.DeviceID)
	}
}

func TestRealPortalHTTPDeviceCodeTrailingSlashStillReturnsJSON(t *testing.T) {
	_, _, portalBaseURL := setupRealPortalApp(t)

	form := url.Values{}
	form.Set("client_id", "wantastic_cipher_v_1_0_0")
	form.Set("device_id", "macos-device-123")

	resp, err := http.PostForm(portalBaseURL+"/oauth/device/code/", form)
	if err != nil {
		t.Fatalf("POST /oauth/device/code/: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status=%d, want=%d", resp.StatusCode, http.StatusOK)
	}
	if ct := resp.Header.Get("Content-Type"); !strings.Contains(ct, "application/json") {
		t.Fatalf("Content-Type=%q, want application/json", ct)
	}

	var deviceResp oauth2.DeviceAuthorizationResponse
	if err := json.NewDecoder(resp.Body).Decode(&deviceResp); err != nil {
		t.Fatalf("decode device response: %v", err)
	}
	if deviceResp.DeviceCode == "" {
		t.Fatal("DeviceCode is empty")
	}
}

func TestRealPortalDeviceLoginRouteRedirectsToActivation(t *testing.T) {
	_, _, portalBaseURL := setupRealPortalApp(t)

	client := &http.Client{
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}

	resp, err := client.Get(portalBaseURL + "/device-login?code=ABCD-1234")
	if err != nil {
		t.Fatalf("GET /device-login: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusFound {
		t.Fatalf("status=%d, want=%d", resp.StatusCode, http.StatusFound)
	}
	wantLocation := "https://wantastic.local/activate?user_code=ABCD-1234"
	if got := resp.Header.Get("Location"); got != wantLocation {
		t.Fatalf("Location=%q, want %q", got, wantLocation)
	}
}

// TestFullPKCEHTTPFlow_AuthenticatedUser simulates the exact agent PKCE flow
// where the user already has a valid portal session (most common case in dev).
func TestFullPKCEHTTPFlow_AuthenticatedUser(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	app, _, portalBaseURL := setupRealPortalApp(t)

	// ── Step 0: Pre-seed a portal session (simulates the user being logged in) ─
	sess, err := app.sessionStore.CreateSession(
		"tenant-test-123", "Test User", "test@example.com", "enterprise", "grpc-token-xxx", false,
	)
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}

	// ── Step 1: Start a local callback server (simulates the agent's listener) ─
	callbackLn, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("callback listen: %v", err)
	}
	callbackPort := callbackLn.Addr().(*net.TCPAddr).Port
	callbackURI := fmt.Sprintf("http://127.0.0.1:%d/callback", callbackPort)

	callbackResult := make(chan url.Values, 1)
	callbackSrv := &http.Server{
		Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			callbackResult <- r.URL.Query()
			w.WriteHeader(http.StatusOK)
			w.Write([]byte("OK"))
		}),
	}
	go callbackSrv.Serve(callbackLn)
	defer callbackSrv.Close()

	// ── Step 2: Generate PKCE pair ────────────────────────────────────────────
	verifier, challenge, _ := oauth2.GeneratePKCE()
	state := "test_state_1234567890ab"
	deviceID := generateTestDeviceID()

	// ── Step 3: Build & call GET /oauth/authorize (no redirect follow) ────────
	jar, _ := cookiejar.New(nil)
	client := &http.Client{
		Jar: jar,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			return http.ErrUseLastResponse // capture redirect without following
		},
	}

	authURL := fmt.Sprintf(
		"%s/oauth/authorize?client_id=wantastic_cipher_v_1_0_0&redirect_uri=%s&state=%s&scope=%s&code_challenge=%s&code_challenge_method=S256&device_id=%s",
		portalBaseURL,
		url.QueryEscape(callbackURI),
		url.QueryEscape(state),
		url.QueryEscape("org:create_api_key user:profile"),
		url.QueryEscape(challenge),
		url.QueryEscape(deviceID),
	)

	resp, err := client.Get(authURL)
	if err != nil {
		t.Fatalf("GET /oauth/authorize: %v", err)
	}
	resp.Body.Close()

	if resp.StatusCode != http.StatusFound {
		t.Fatalf("expected 302 from /oauth/authorize, got %d", resp.StatusCode)
	}
	location := resp.Header.Get("Location")
	if !strings.Contains(location, "oauth2-consent") {
		t.Fatalf("expected redirect to oauth2-consent SPA page, got: %s", location)
	}
	t.Logf("Step 3 ✓: /oauth/authorize redirected to: %s", location)

	// Extract auth code from the redirect location (/?code=AUTH_CODE#oauth2-consent)
	parsed, _ := url.Parse(location)
	authCode := parsed.Query().Get("code")
	if authCode == "" {
		t.Fatalf("no code in redirect location: %s", location)
	}
	t.Logf("Step 3 ✓: auth code = %s…", authCode[:12])

	// ── Step 4: Add the portal session cookie (simulates logged-in browser) ────
	portalURL, _ := url.Parse(portalBaseURL)
	jar.SetCookies(portalURL, []*http.Cookie{{
		Name:  "tenant_session",
		Value: sess.Token,
	}})

	// ── Step 5: GET /api/oauth/consent-info (should return authenticated:true) ─
	consentInfoURL := fmt.Sprintf("%s/api/oauth/consent-info?code=%s", portalBaseURL, url.QueryEscape(authCode))
	resp, err = client.Get(consentInfoURL)
	if err != nil {
		t.Fatalf("GET /api/oauth/consent-info: %v", err)
	}
	var consentInfo struct {
		ClientID      string `json:"client_id"`
		Scope         string `json:"scope"`
		Authenticated bool   `json:"authenticated"`
		UserEmail     string `json:"user_email"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&consentInfo); err != nil {
		t.Fatalf("decode consent-info: %v", err)
	}
	resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200 from consent-info, got %d", resp.StatusCode)
	}
	if !consentInfo.Authenticated {
		t.Fatalf("expected authenticated=true (session cookie was set), got false")
	}
	if consentInfo.ClientID != "wantastic_cipher_v_1_0_0" {
		t.Errorf("expected client_id=wantastic_cipher_v_1_0_0, got %s", consentInfo.ClientID)
	}
	t.Logf("Step 5 ✓: consent-info: client=%s scope=%s authenticated=%v user=%s",
		consentInfo.ClientID, consentInfo.Scope, consentInfo.Authenticated, consentInfo.UserEmail)

	// ── Step 6: POST /api/oauth/authorize-confirm {action:"allow"} ─────────────
	// The SPA also sends the oauth2_state cookie (set by /oauth/authorize).
	confirmBody := `{"auth_code":"` + authCode + `","action":"allow"}`
	req, _ := http.NewRequest(http.MethodPost, portalBaseURL+"/api/oauth/authorize-confirm",
		strings.NewReader(confirmBody))
	req.Header.Set("Content-Type", "application/json")

	resp, err = client.Do(req)
	if err != nil {
		t.Fatalf("POST /api/oauth/authorize-confirm: %v", err)
	}
	var confirmResp struct {
		Success     bool   `json:"success"`
		Action      string `json:"action"`
		RedirectURI string `json:"redirect_uri"`
		Error       string `json:"error"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&confirmResp); err != nil {
		t.Fatalf("decode authorize-confirm: %v", err)
	}
	resp.Body.Close()

	if !confirmResp.Success {
		t.Fatalf("authorize-confirm failed: error=%q", confirmResp.Error)
	}
	if !strings.HasPrefix(confirmResp.RedirectURI, callbackURI) {
		t.Fatalf("redirect_uri %q does not start with callback %q", confirmResp.RedirectURI, callbackURI)
	}
	t.Logf("Step 6 ✓: authorize-confirm returned redirect_uri: %s", confirmResp.RedirectURI)

	// ── Step 7: Agent follows the redirect_uri → callback server receives code ─
	agentClient := &http.Client{Timeout: 3 * time.Second}
	_, err = agentClient.Get(confirmResp.RedirectURI)
	if err != nil {
		t.Fatalf("agent GET callback redirect_uri: %v", err)
	}

	var callbackParams url.Values
	select {
	case callbackParams = <-callbackResult:
	case <-time.After(3 * time.Second):
		t.Fatal("timeout waiting for callback server to receive redirect")
	}

	if callbackParams.Get("code") == "" {
		t.Fatalf("callback did not receive code param; got: %v", callbackParams)
	}
	if callbackParams.Get("state") != state {
		t.Errorf("callback state mismatch: got %q, want %q", callbackParams.Get("state"), state)
	}
	returnedCode := callbackParams.Get("code")
	t.Logf("Step 7 ✓: callback received code=%s… state=%s", returnedCode[:12], callbackParams.Get("state"))

	// ── Step 8: Exchange code for access token (POST /oauth/token) ─────────────
	tokenForm := url.Values{}
	tokenForm.Set("grant_type", "authorization_code")
	tokenForm.Set("code", returnedCode)
	tokenForm.Set("redirect_uri", callbackURI)
	tokenForm.Set("client_id", "wantastic_cipher_v_1_0_0")
	tokenForm.Set("code_verifier", verifier)

	resp, err = agentClient.PostForm(portalBaseURL+"/oauth/token", tokenForm)
	if err != nil {
		t.Fatalf("POST /oauth/token: %v", err)
	}
	var tokenResp struct {
		AccessToken string `json:"access_token"`
		TokenType   string `json:"token_type"`
		ExpiresIn   int    `json:"expires_in"`
		Error       string `json:"error"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&tokenResp); err != nil {
		t.Fatalf("decode token response: %v", err)
	}
	resp.Body.Close()

	if tokenResp.Error != "" {
		t.Fatalf("token exchange error: %s", tokenResp.Error)
	}
	if tokenResp.AccessToken == "" {
		t.Fatal("access_token is empty")
	}
	if tokenResp.TokenType != "Bearer" {
		t.Errorf("expected TokenType=Bearer, got %s", tokenResp.TokenType)
	}
	t.Logf("Step 8 ✓: token exchange succeeded, token expires in %d seconds", tokenResp.ExpiresIn)

	// ── Step 9: Validate the access token ────────────────────────────────────
	claims, err := app.oauth2Server.ValidateAccessToken(tokenResp.AccessToken)
	if err != nil {
		t.Fatalf("ValidateAccessToken: %v", err)
	}
	if claims.Email != "test@example.com" {
		t.Errorf("claims.Email = %q, want test@example.com", claims.Email)
	}
	if claims.TenantID != "tenant-test-123" {
		t.Errorf("claims.TenantID = %q, want tenant-test-123", claims.TenantID)
	}
	t.Logf("Step 9 ✓: token valid — email=%s tenant=%s tier=%s", claims.Email, claims.TenantID, claims.Tier)
	t.Log("✅ Full PKCE flow E2E test passed!")
}

// TestFullPKCEHTTPFlow_UnauthenticatedUser tests that unauthenticated users see
// authenticated:false from consent-info (they must login first).
func TestFullPKCEHTTPFlow_UnauthenticatedUser(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	_, _, portalBaseURL := setupRealPortalApp(t)

	verifier, challenge, _ := oauth2.GeneratePKCE()
	_ = verifier
	state := "state_unauth_test_xyz789"
	deviceID := generateTestDeviceID()

	jar, _ := cookiejar.New(nil)
	client := &http.Client{
		Jar: jar,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}

	authURL := fmt.Sprintf(
		"%s/oauth/authorize?client_id=wantastic_cipher_v_1_0_0&redirect_uri=%s&state=%s&scope=user:profile&code_challenge=%s&code_challenge_method=S256&device_id=%s",
		portalBaseURL,
		url.QueryEscape("http://127.0.0.1:58250/callback"),
		url.QueryEscape(state),
		url.QueryEscape(challenge),
		url.QueryEscape(deviceID),
	)

	resp, err := client.Get(authURL)
	if err != nil {
		t.Fatalf("GET /oauth/authorize: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusFound {
		t.Fatalf("expected 302, got %d", resp.StatusCode)
	}
	location := resp.Header.Get("Location")
	parsed, _ := url.Parse(location)
	authCode := parsed.Query().Get("code")
	if authCode == "" {
		t.Fatalf("no code in location: %s", location)
	}

	// No session cookie — consent-info should return authenticated:false
	consentInfoURL := fmt.Sprintf("%s/api/oauth/consent-info?code=%s", portalBaseURL, url.QueryEscape(authCode))
	resp, err = client.Get(consentInfoURL)
	if err != nil {
		t.Fatalf("GET /api/oauth/consent-info: %v", err)
	}
	var ci struct {
		Authenticated bool   `json:"authenticated"`
		ClientID      string `json:"client_id"`
	}
	json.NewDecoder(resp.Body).Decode(&ci)
	resp.Body.Close()

	if ci.Authenticated {
		t.Error("expected authenticated=false for unauthenticated user, got true")
	}
	if ci.ClientID != "wantastic_cipher_v_1_0_0" {
		t.Errorf("expected client_id=wantastic_cipher_v_1_0_0, got %s", ci.ClientID)
	}
	t.Logf("✅ Unauthenticated consent-info: authenticated=%v client=%s", ci.Authenticated, ci.ClientID)
}

// TestFullDeviceFlow_ApproveViaSPA tests the device authorization flow
// (approve/deny via the Activate SPA page).
func TestFullDeviceFlow_ApproveViaSPA(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	app, _, portalBaseURL := setupRealPortalApp(t)

	// Seed a session
	sess, err := app.sessionStore.CreateSession(
		"tenant-dev-456", "Dev User", "dev@example.com", "free", "grpc-token-yyy", false,
	)
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}

	jar, _ := cookiejar.New(nil)
	client := &http.Client{Jar: jar}

	// Step 1: POST /oauth/device/code (agent starts device flow)
	form := url.Values{}
	form.Set("client_id", "wantastic_cipher_v_1_0_0")
	form.Set("device_id", generateTestDeviceID())

	resp, err := client.PostForm(portalBaseURL+"/oauth/device/code", form)
	if err != nil {
		t.Fatalf("POST /oauth/device/code: %v", err)
	}
	var deviceResp oauth2.DeviceAuthorizationResponse
	if err := json.NewDecoder(resp.Body).Decode(&deviceResp); err != nil {
		t.Fatalf("decode device response: %v", err)
	}
	resp.Body.Close()
	if deviceResp.UserCode == "" {
		t.Fatal("UserCode is empty")
	}
	t.Logf("Step 1 ✓: device flow started, user_code=%s", deviceResp.UserCode)

	// Step 2: GET /api/oauth/pending-device?user_code=... (SPA fetches device info)
	pendingURL := fmt.Sprintf("%s/api/oauth/pending-device?user_code=%s",
		portalBaseURL, url.QueryEscape(deviceResp.UserCode))
	resp, err = client.Get(pendingURL)
	if err != nil {
		t.Fatalf("GET /api/oauth/pending-device: %v", err)
	}
	var pending struct {
		DeviceID  string `json:"device_id"`
		UserCode  string `json:"user_code"`
		ExpiresAt int64  `json:"expires_at"`
	}
	json.NewDecoder(resp.Body).Decode(&pending)
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200 from pending-device, got %d", resp.StatusCode)
	}
	if pending.UserCode != deviceResp.UserCode {
		t.Errorf("user_code mismatch: %s vs %s", pending.UserCode, deviceResp.UserCode)
	}
	t.Logf("Step 2 ✓: pending-device: device_id=%s…", pending.DeviceID[:8])

	// Step 3: Add session cookie (user is logged in) and approve via /api/oauth/approve
	portalURL, _ := url.Parse(portalBaseURL)
	jar.SetCookies(portalURL, []*http.Cookie{{
		Name:  "tenant_session",
		Value: sess.Token,
	}})

	approveForm := url.Values{}
	approveForm.Set("user_code", deviceResp.UserCode)
	resp, err = client.PostForm(portalBaseURL+"/api/oauth/approve", approveForm)
	if err != nil {
		t.Fatalf("POST /api/oauth/approve: %v", err)
	}
	var approveResp struct {
		Success bool   `json:"success"`
		Error   string `json:"error"`
	}
	json.NewDecoder(resp.Body).Decode(&approveResp)
	resp.Body.Close()

	if !approveResp.Success {
		t.Fatalf("approve failed: %s", approveResp.Error)
	}
	t.Logf("Step 3 ✓: device approved successfully")

	// Step 4: Agent polls /oauth/token — should receive token
	ctx := context.Background()
	tokenResp, err := app.oauth2Server.PollDeviceToken(deviceResp.DeviceCode)
	if err != nil {
		t.Fatalf("PollDeviceToken: %v", err)
	}
	if tokenResp.AccessToken == "" {
		t.Fatal("access token is empty after approval")
	}
	t.Logf("Step 4 ✓: token received, expires in %d seconds", tokenResp.ExpiresIn)

	// Step 5: Validate token claims
	claims, err := app.oauth2Server.ValidateAccessToken(tokenResp.AccessToken)
	if err != nil {
		t.Fatalf("ValidateAccessToken: %v", err)
	}
	if claims.Email != "dev@example.com" {
		t.Errorf("claims.Email = %q, want dev@example.com", claims.Email)
	}
	t.Logf("✅ Device flow E2E test passed! email=%s tenant=%s", claims.Email, claims.TenantID)
	_ = ctx
}
