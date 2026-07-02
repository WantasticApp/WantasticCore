package portalsrv

import (
	"context"
	"crypto/rand"
	"embed"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io/fs"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"
	"path"
	"strconv"
	"strings"
	"time"

	core "WantasticCore/internal/core"
	"WantasticCore/internal/errs"
	"WantasticCore/internal/portalsrv/hooks"
	"WantasticCore/internal/portalsrv/middleware"
	"WantasticCore/internal/portalsrv/pkg/cipher"
	"WantasticCore/internal/portalsrv/pkg/services"
	"WantasticCore/internal/portalsrv/pkg/session"
	proto "WantasticCore/internal/types"

	"WantasticCore/internal/admin"
	"WantasticCore/internal/auth"
	"WantasticCore/internal/config"
	"WantasticCore/internal/copilot"
	"WantasticCore/internal/oauth2"
	redisstore "WantasticCore/internal/store/redis"

	"github.com/redis/go-redis/v9"
	"github.com/rs/zerolog"
	zlog "github.com/rs/zerolog/log"
)

//go:embed app/dist/*
var spaFiles embed.FS

// wantasticAuth0Domain is the public activation/custom domain exposed to agents.
// wantasticAuth0ClientID is the default Auth0 Native app Client ID.
// Real security is enforced by the cipher proof on every gRPC/HTTP call.
const (
	wantasticAuth0Domain   = "console.wantastic.app"
	wantasticAuth0ClientID = "wantastic_cipher_v_1_0_0"
	secretPath             = "/tmp/portal_session_secret.key"
)

// Args is the configuration accepted by Start. Each field corresponds to a
// command-line flag exposed by the legacy cmd/web/portal main wrapper. Empty
// string fields fall back to the corresponding WANTASTIC_* environment
// variable (and then to the historical default) at runtime.
type Args struct {
	HTTPAddr          string // was -http (default ":8001")
	GRPCAddrs         string // was -grpc (comma-sep list, unused after Phase 5b)
	CertDir           string // was -cert-dir
	MTLS              bool   // was -mtls
	Dev               bool   // was -dev
	HooksSecret       string // was -hooks-secret
	RedisAddr         string // was -redis-addr
	RedisPassword     string // was -redis-password
	RedisUser         string // was -redis-user
	ViteURL           string // was -vite-url
	AutoCertGen       bool   // was -auto-cert-gen
	Auth0PublicDomain string // was -auth0-public-domain
	Auth0OAuthDomain  string // was -auth0-oauth-domain
	Auth0ClientID     string // was -auth0-client-id
	SessionSecret     string // was -session-secret

	// Services is the in-process gRPC service bundle. After the Phase 5b
	// refactor the portal no longer dials a hub — every hub-bound RPC is
	// served by these implementations directly. Required.
	Services *core.Services

	// Admin is the in-process super-admin service. Optional; when nil, the
	// AdminService WebSocket endpoints return "admin service not configured".
	Admin *admin.Service

	// Copilot is the in-process Claude-backed assistant. Optional; when nil,
	// the CopilotService WebSocket endpoints return "copilot not enabled".
	Copilot *copilot.Service

	// OnSetCopilotAPIKey is called when an admin saves a new Anthropic API
	// key from the in-app Copilot setup form. The callback owns persistence
	// (e.g. writing to config.yaml) and swapping the live Copilot LLM. When
	// nil, the WebSocket endpoint reports "not supported in this build".
	OnSetCopilotAPIKey func(apiKey string) error
}

// envOrDefault returns provided if non-empty (after trimming), else the value
// of the named env var if set (after trimming), else fallback.
func envOrDefault(provided, envKey, fallback string) string {
	if v := strings.TrimSpace(provided); v != "" {
		return v
	}
	if v := strings.TrimSpace(os.Getenv(envKey)); v != "" {
		return v
	}
	return fallback
}

// applyDefaults fills in env-var fallbacks for any caller-empty Auth0/session
// fields and sets historical defaults for any otherwise-unspecified value.
// publicDomainExplicit is true iff a non-empty Auth0PublicDomain was provided
// either by the caller or by the WANTASTIC_AUTH0_PUBLIC_DOMAIN env var (the
// equivalent of the legacy flagWasSet("auth0-public-domain") check).
func (a *Args) applyDefaults() (publicDomainExplicit bool) {
	publicDomainExplicit = strings.TrimSpace(a.Auth0PublicDomain) != "" ||
		strings.TrimSpace(os.Getenv("WANTASTIC_AUTH0_PUBLIC_DOMAIN")) != ""

	a.Auth0PublicDomain = envOrDefault(a.Auth0PublicDomain, "WANTASTIC_AUTH0_PUBLIC_DOMAIN", wantasticAuth0Domain)
	a.Auth0OAuthDomain = envOrDefault(a.Auth0OAuthDomain, "WANTASTIC_AUTH0_OAUTH_DOMAIN", wantasticAuth0Domain)
	a.Auth0ClientID = envOrDefault(a.Auth0ClientID, "WANTASTIC_AUTH0_CLIENT_ID", wantasticAuth0ClientID)
	a.SessionSecret = envOrDefault(a.SessionSecret, "WANTASTIC_SESSION_SECRET", "")
	return publicDomainExplicit
}

type portalApp struct {
	args                Args
	auth0PublicExplicit bool // whether Auth0PublicDomain was set by caller or env var
	sessionStore        session.SessionStore
	tenantProxy         *services.TenantProxy
	hooksHandler        *hooks.Handler
	router              *services.InProcessRouter
	services            *core.Services
	redisClient         *redis.Client
	isSecure            bool
	cipherInterceptor   *cipher.Interceptor // validates Wantastic agent proofs
	oauth2Server        *oauth2.Server      // internal OAuth2 server (optional)
	oauth2Issuer        string              // public origin used in OAuth2 verification links
	oauth2Domain        string              // public host returned to agents for HTTP OAuth2
	sessionSecret       []byte              // shared by session cookies and internal OAuth2 JWTs
}

// Start runs the tenant portal HTTP server. It blocks until the context is
// canceled or the HTTP server returns an error other than ErrServerClosed.
func Start(ctx context.Context, args Args) error {
	if args.Services == nil {
		return fmt.Errorf("portalsrv: in-process Services bundle is required")
	}
	explicit := args.applyDefaults()

	// Initialize structured logging
	zerolog.TimeFieldFormat = zerolog.TimeFormatUnix
	zlog.Logger = zlog.Output(zerolog.ConsoleWriter{Out: os.Stderr})
	if args.Dev {
		zerolog.SetGlobalLevel(zerolog.DebugLevel)
	} else {
		zerolog.SetGlobalLevel(zerolog.InfoLevel)
	}
	app := &portalApp{args: args, auth0PublicExplicit: explicit, services: args.Services}
	app.isSecure = !strings.HasPrefix(args.HTTPAddr, ":") || strings.Contains(args.HTTPAddr, "443")
	app.cipherInterceptor = cipher.NewInterceptor()

	// 1. mTLS manager (unused for hub-bound traffic now, retained for cert helpers)
	_ = app.initMTLS()

	// 2. Initialize Redis
	app.initRedis()
	if app.redisClient != nil {
		defer app.redisClient.Close()
	}

	// 3. In-process service router. Used for the peer-bound dialing surface
	// the TenantProxy keeps around for symmetry; all in-process traffic goes
	// through args.Services directly now, no gRPC dispatch.
	app.router = services.NewInProcessRouter(args.Services)
	defer app.router.Close()

	// 4. Initialize Session Store
	app.initSessionStore()

	// 5. Initialize Internal OAuth2
	app.initOAuth2()

	// 6. Initialize Tenant Proxy
	app.initTenantProxy()
	defer app.tenantProxy.Close()

	// 7. Initialize HTTP Routes
	mux := app.setupRoutes()

	zlog.Debug().
		Str("http_addr", args.HTTPAddr).
		Str("core_hubs", args.GRPCAddrs).
		Msg(" Wantastic Tenant Portal starting")

	srv := &http.Server{Addr: args.HTTPAddr, Handler: mux}
	go func() {
		<-ctx.Done()
		_ = srv.Shutdown(context.Background())
	}()
	if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		return err
	}
	return nil
}

func (app *portalApp) initMTLS() *auth.MTLSManager {
	mtlsConfig := &auth.MTLSConfig{
		CertDir:      app.args.CertDir,
		AutoGenerate: app.args.AutoCertGen,
	}
	mtlsMgr, err := auth.NewMTLSManager(mtlsConfig)
	if err != nil {
		if app.args.MTLS {
			zlog.Fatal().Err(err).Msg("Failed to initialize mTLS manager")
		} else {
			zlog.Warn().Err(err).Msg("mTLS manager initialization failed (non-blocking)")
		}
	}
	return mtlsMgr
}

func (app *portalApp) initRedis() {
	zlog.Debug().Str("addr", app.args.RedisAddr).Msg(" Connecting to Redis")
	redisCfg := config.RedisConfig{
		Enabled:  true,
		Addr:     app.args.RedisAddr,
		Password: app.args.RedisPassword,
		Username: app.args.RedisUser,
		DB:       0,
	}

	// Retry up to 5 times with exponential backoff to handle transient startup failures.
	// In production, Redis may not be ready immediately when the portal starts.
	delays := []time.Duration{0, 1 * time.Second, 2 * time.Second, 4 * time.Second, 8 * time.Second}
	var lastErr error
	for attempt, delay := range delays {
		if delay > 0 {
			zlog.Warn().Err(lastErr).Int("attempt", attempt+1).Dur("retry_in", delay).Msg("Redis connection failed — retrying")
			time.Sleep(delay)
		}
		client, err := redisstore.NewClient(redisCfg)
		if err == nil {
			app.redisClient = client
			zlog.Info().Int("attempt", attempt+1).Msg("✅ Connected to Redis")
			return
		}
		lastErr = err
	}
	zlog.Warn().Err(lastErr).Msg("❌ Failed to connect to Redis after retries (sessions will be in-memory — restart will sign everyone out)")
}

// The in-process service router is initialised inline in Start.

func (app *portalApp) initSessionStore() {
	secret := app.resolveSessionSecret()
	app.sessionSecret = secret
	app.sessionStore = session.NewJWTSessionStore(secret, app.redisClient)

	if app.redisClient != nil {
		zlog.Info().Msg(" Session management: JWT (cookie) + Redis revocation")
	} else {
		zlog.Info().Msg(" Session management: JWT (cookie) — sessions persist across restarts")
	}
}

func (app *portalApp) initOAuth2() {
	cfg := oauth2.DefaultConfig()
	if app.args.Dev {
		cfg = oauth2.DevConfig()
	}

	cfg.Issuer = app.configuredOAuth2Issuer()
	if len(app.sessionSecret) == 0 {
		app.sessionSecret = app.resolveSessionSecret()
	}
	cfg.SigningSecret = app.sessionSecret

	var store oauth2.Store
	if app.redisClient != nil {
		store = oauth2.NewRedisStore(app.redisClient)
	}

	srv, err := oauth2.NewServer(cfg, store)
	if err != nil {
		zlog.Fatal().Err(err).Msg("Failed to initialize internal OAuth2 server")
	}

	app.oauth2Server = srv
	app.oauth2Issuer = cfg.Issuer
	app.oauth2Domain = issuerHost(cfg.Issuer)
	zlog.Info().
		Str("issuer", cfg.Issuer).
		Str("domain", app.oauth2Domain).
		Bool("redis_store", store != nil).
		Msg(" Internal OAuth2 device flow initialized")
}

func (app *portalApp) configuredOAuth2Issuer() string {
	publicDomain := strings.TrimSpace(app.args.Auth0PublicDomain)
	if app.args.Dev && !app.auth0PublicExplicit {
		publicDomain = "wantastic.local"
	}
	return normalizeOAuth2Issuer(publicDomain)
}

func normalizeOAuth2Issuer(publicDomain string) string {
	publicDomain = strings.TrimRight(strings.TrimSpace(publicDomain), "/")
	if publicDomain == "" {
		publicDomain = wantasticAuth0Domain
	}
	if strings.HasPrefix(publicDomain, "http://") || strings.HasPrefix(publicDomain, "https://") {
		return publicDomain
	}
	return "https://" + publicDomain
}

func issuerHost(issuer string) string {
	u, err := url.Parse(issuer)
	if err == nil && u.Host != "" {
		return u.Host
	}
	return strings.TrimPrefix(strings.TrimPrefix(strings.TrimRight(issuer, "/"), "https://"), "http://")
}

// resolveSessionSecret returns the HMAC key for session signing.
// Priority: Args.SessionSecret (already env-merged) > random (unsafe).
func (app *portalApp) resolveSessionSecret() []byte {
	if raw := strings.TrimSpace(app.args.SessionSecret); raw != "" {
		if key := decodeSecretString(raw); key != nil {
			zlog.Info().Msg(" Session secret: loaded from --session-secret / WANTASTIC_SESSION_SECRET")
			return key
		}
		zlog.Warn().Msg("--session-secret value too short or invalid; falling back to random key (sessions will not survive restarts)")
	}
	key := make([]byte, 32)
	if _, err := rand.Read(key); err != nil {
		panic(fmt.Sprintf("failed to generate session secret: %v", err))
	}
	zlog.Warn().Msg("No session secret configured: sessions will not survive portal restarts. Use --session-secret.")
	return key
}

// decodeSecretString tries base64 (std + URL, padded + raw), then hex, then raw bytes.
// Returns nil if the result is shorter than 16 bytes.
func decodeSecretString(raw string) []byte {
	for _, enc := range []*base64.Encoding{
		base64.StdEncoding, base64.RawStdEncoding,
		base64.URLEncoding, base64.RawURLEncoding,
	} {
		if b, err := enc.DecodeString(raw); err == nil && len(b) >= 16 {
			return b
		}
	}
	if b, err := hex.DecodeString(raw); err == nil && len(b) >= 16 {
		return b
	}
	if len(raw) >= 16 {
		return []byte(raw)
	}
	return nil
}

func (app *portalApp) initTenantProxy() {
	proxy, err := services.NewTenantProxy(app.services, app.router, app.sessionStore, app.redisClient)
	if err != nil {
		zlog.Fatal().Err(err).Msg("Failed to create tenant proxy")
	}
	if app.args.Admin != nil {
		proxy.SetAdminService(app.args.Admin)
	}
	if app.args.Copilot != nil {
		proxy.SetCopilotService(app.args.Copilot)
	}
	if app.args.OnSetCopilotAPIKey != nil {
		proxy.SetCopilotAPIKeyHandler(app.args.OnSetCopilotAPIKey)
	}
	app.tenantProxy = proxy
}

func (app *portalApp) setupRoutes() *http.ServeMux {
	mux := http.NewServeMux()
	securityMiddleware := middleware.NewSecurityMiddleware()
	rateLimiterSmall := middleware.NewDefaultRateLimiter(500)

	// Hooks. Always mount the route so /hooks/backup and /hooks/stripe work
	// even when WANTASTIC_HOOKS_SECRET is not configured (dev). Endpoints that
	// genuinely require the cipher (unsubscribe, invite) return 503 in that
	// mode; backup/stripe/sms validate via gRPC tokens or shared secrets.
	{
		handler, err := hooks.NewHandler(app.args.HooksSecret)
		if err != nil {
			zlog.Fatal().Err(err).Msg("Failed to create hooks handler")
		}
		handler.SetServices(hooks.HookServices{
			TenantPortal:       app.services.TenantPortal,
			TenantRegistration: app.services.TenantRegistration,
			WUSP:               app.services.WUSP,
		})
		app.hooksHandler = handler
		mux.HandleFunc("/hooks/", rateLimiterSmall.Middleware(handler.ServeHTTP))
		if app.args.HooksSecret == "" {
			zlog.Warn().Msg("Hooks handler mounted WITHOUT notification cipher — /hooks/unsubscribe will return 503. Set --hooks-secret to enable it.")
		} else {
			zlog.Debug().Msg("Hooks handler enabled (full)")
		}
	}

	// API Endpoints
	mux.HandleFunc("/api/session", rateLimiterSmall.Middleware(securityMiddleware.Middleware(app.handleSetSession)))
	mux.HandleFunc("/api/logout", rateLimiterSmall.Middleware(securityMiddleware.Middleware(app.handleLogout)))
	mux.HandleFunc("/api/device-handoff", rateLimiterSmall.Middleware(app.handleDeviceHandoff))
	mux.HandleFunc("/api/agent/credentials", rateLimiterSmall.Middleware(app.handleAgentCredentials))
	mux.HandleFunc("/api/agent/claim-config", rateLimiterSmall.Middleware(app.handleAgentClaimConfig))
	mux.HandleFunc("/api/snapshot/download", rateLimiterSmall.Middleware(app.handleSnapshotDownload))
	mux.HandleFunc("/ws", rateLimiterSmall.Middleware(app.tenantProxy.HandleWebSocket))

	// OAuth2 Endpoints
	// OAuth2 Device Authorization Grant endpoints (RFC 8628)
	mux.HandleFunc("/oauth/device/code", rateLimiterSmall.Middleware(app.handleOAuth2DeviceCode))
	mux.HandleFunc("/oauth/device/code/", rateLimiterSmall.Middleware(app.handleOAuth2DeviceCode))

	// OAuth2 Authorization Code Flow with PKCE endpoints (RFC 6749 + RFC 7636)
	mux.HandleFunc("/oauth/authorize", rateLimiterSmall.Middleware(app.handleOAuth2Authorize))
	mux.HandleFunc("/oauth/token", rateLimiterSmall.Middleware(app.handleOAuth2Token))
	mux.HandleFunc("/oauth/token/", rateLimiterSmall.Middleware(app.handleOAuth2Token))

	// Device activation page (user enters code and approves)
	mux.HandleFunc("/device-login", rateLimiterSmall.Middleware(app.handleDeviceLogin))
	mux.HandleFunc("/device-login/", rateLimiterSmall.Middleware(app.handleDeviceLogin))
	mux.HandleFunc("/activate", rateLimiterSmall.Middleware(app.handleOAuth2Activate))
	mux.HandleFunc("/api/oauth/approve", rateLimiterSmall.Middleware(app.handleOAuth2Approve))
	mux.HandleFunc("/api/oauth/deny", rateLimiterSmall.Middleware(app.handleOAuth2Deny))

	// PKCE authorization callback/consent page
	mux.HandleFunc("/oauth/consent", rateLimiterSmall.Middleware(app.handleOAuth2Consent))
	mux.HandleFunc("/api/oauth/consent-login", rateLimiterSmall.Middleware(app.handleOAuth2ConsentLogin))
	mux.HandleFunc("/api/oauth/authorize-confirm", rateLimiterSmall.Middleware(app.handleOAuth2AuthorizeConfirm))
	// JSON info endpoints for the Svelte SPA pages
	mux.HandleFunc("/api/oauth/consent-info", rateLimiterSmall.Middleware(app.handleOAuth2ConsentInfo))
	mux.HandleFunc("/api/oauth/pending-device", rateLimiterSmall.Middleware(app.handleOAuth2PendingDevice))

	zlog.Debug().Msg(" OAuth2 endpoints registered")

	// SPA
	if app.args.Dev {
		app.setupDevProxy(mux)
	} else {
		mux.Handle("/", createSPAHandler())
	}

	return mux
}

func (app *portalApp) setupDevProxy(mux *http.ServeMux) {
	viteURL, err := url.Parse(app.args.ViteURL)
	if err != nil {
		zlog.Fatal().Err(err).Msg("Failed to parse Vite URL")
	}
	viteProxy := httputil.NewSingleHostReverseProxy(viteURL)
	originalDirector := viteProxy.Director
	viteProxy.Director = func(req *http.Request) {
		originalDirector(req)
		req.Host = viteURL.Host
	}
	viteProxy.ModifyResponse = func(resp *http.Response) error {
		resp.Header.Set("Access-Control-Allow-Origin", "*")
		return nil
	}
	mux.Handle("/", viteProxy)
	zlog.Debug().Str("url", app.args.ViteURL).Msg(" Dev mode: proxying to Vite")
}

// =============================================================================
// Cookie Helpers
// =============================================================================

// cookieDomain returns the appropriate cookie domain for the current request.
// On production (*.wantastic.app) it returns ".wantastic.app" so the cookie is
// shared across sub-domains. On any other host (local dev, staging under a
// different domain, etc.) it returns an empty string so the browser defaults to
// the exact request hostname — otherwise the browser silently drops the cookie.
func cookieDomain(r *http.Request) string {
	host := r.Host
	if host == "" {
		host = r.Header.Get("X-Forwarded-Host")
	}
	hostname, _, err := net.SplitHostPort(host)
	if err != nil {
		hostname = host // no port present
	}
	if strings.HasSuffix(hostname, ".wantastic.app") || hostname == "wantastic.app" {
		return ".wantastic.app"
	}
	return ""
}

// isSecureRequest returns true when the request arrived over TLS (directly or
// via a TLS-terminating proxy). Falls back to the static app.isSecure flag that
// is derived from the listening address at startup.
func isSecureRequest(r *http.Request, appIsSecure bool) bool {
	if r.TLS != nil {
		return true
	}
	if r.Header.Get("X-Forwarded-Proto") == "https" {
		return true
	}
	return appIsSecure
}

// HTTP Handlers
// =============================================================================

func (app *portalApp) handleSetSession(w http.ResponseWriter, r *http.Request) {
	// Add CORS headers for development only
	if app.args.Dev {
		origin := r.Header.Get("Origin")
		if origin == "http://localhost:5173" || origin == "http://localhost:5174" {
			w.Header().Set("Access-Control-Allow-Origin", origin)
			w.Header().Set("Access-Control-Allow-Credentials", "true")
			w.Header().Set("Access-Control-Allow-Methods", "POST, GET, OPTIONS")
			w.Header().Set("Access-Control-Allow-Headers", "Content-Type")
		}
	}

	if r.Method == http.MethodOptions {
		w.WriteHeader(http.StatusOK)
		return
	}

	// GET /api/session — lightweight session validity check (for SPA pages that
	// need auth status before WebSocket is connected, e.g. Activate page).
	if r.Method == http.MethodGet {
		cookie, err := r.Cookie("tenant_session")
		if err != nil || cookie.Value == "" {
			http.Error(w, `{"authenticated":false}`, http.StatusUnauthorized)
			return
		}
		sess, err := app.sessionStore.GetSession(cookie.Value)
		if err != nil || sess == nil {
			http.Error(w, `{"authenticated":false}`, http.StatusUnauthorized)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"authenticated": true,
			"email":         sess.Email,
		})
		return
	}

	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	if ct := strings.ToLower(r.Header.Get("Content-Type")); !strings.Contains(ct, "application/json") {
		http.Error(w, "Content-Type must be application/json", http.StatusUnsupportedMediaType)
		return
	}

	var req struct {
		TenantID         string `json:"tenant_id"`
		FullName         string `json:"full_name"`
		Email            string `json:"email"`
		Tier             string `json:"tier"`
		GRPCSessionToken string `json:"grpc_session_token"`
		RememberMe       bool   `json:"remember_me"`
		IsFirstLogin     bool   `json:"is_first_login"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	if strings.TrimSpace(req.TenantID) == "" || strings.TrimSpace(req.Email) == "" || strings.TrimSpace(req.GRPCSessionToken) == "" {
		http.Error(w, "Missing required session fields", http.StatusBadRequest)
		return
	}

	sess, err := app.sessionStore.CreateSession(
		req.TenantID, req.FullName, req.Email, req.Tier, req.GRPCSessionToken, req.RememberMe,
	)
	if err != nil {
		zlog.Error().Err(err).Msg("Failed to create session")
		http.Error(w, "Session creation failed", http.StatusInternalServerError)
		return
	}

	zlog.Debug().Str("tenant_id", req.TenantID).Str("token", sess.Token[:8]+"...").Msg(" Session created")

	// Derive MaxAge from the JWT's actual expiry so the cookie and token expire together.
	maxAge := int(time.Until(sess.ExpiresAt).Seconds())
	if maxAge < 0 {
		maxAge = 0
	}

	secure := isSecureRequest(r, app.isSecure)
	domain := cookieDomain(r)
	http.SetCookie(w, &http.Cookie{
		Name:     "tenant_session",
		Value:    sess.Token,
		Path:     "/",
		MaxAge:   maxAge,
		HttpOnly: true,
		Domain:   domain,
		Secure:   secure,
		SameSite: http.SameSiteLaxMode,
	})
	http.SetCookie(w, &http.Cookie{
		Name:     "tenant_name",
		Value:    sess.FullName,
		Path:     "/",
		MaxAge:   maxAge,
		HttpOnly: true,
		Domain:   domain,
		Secure:   secure,
		SameSite: http.SameSiteLaxMode,
	})

	// Set firsttime cookie if this is the user's first login
	if req.IsFirstLogin {
		http.SetCookie(w, &http.Cookie{
			Name:     "firsttime",
			Value:    "1",
			Path:     "/",
			MaxAge:   3600,  // 1 hour
			HttpOnly: false, // Must be readable from JS
			Secure:   secure,
			SameSite: http.SameSiteLaxMode,
		})
	}

	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Pragma", "no-cache")
	json.NewEncoder(w).Encode(map[string]any{"success": true, "tenant_id": req.TenantID, "tier": req.Tier})
}

func (app *portalApp) handleLogout(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	cookie, err := r.Cookie("tenant_session")
	if err == nil && cookie.Value != "" {
		var grpcToken string
		if sess, err := app.sessionStore.GetSession(cookie.Value); err == nil && sess != nil {
			grpcToken = sess.GRPCSessionToken
		}

		app.sessionStore.DeleteSession(cookie.Value)

		if grpcToken != "" {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			client := app.services.TenantPortal
			_, _ = client.TenantLogout(ctx, &proto.TenantLogoutRequest{SessionToken: grpcToken})
		}
	}

	secure := isSecureRequest(r, app.isSecure)
	domain := cookieDomain(r)
	http.SetCookie(w, &http.Cookie{
		Name:     "tenant_session",
		Value:    "",
		Path:     "/",
		MaxAge:   -1,
		HttpOnly: true,
		Domain:   domain,
		Secure:   secure,
		SameSite: http.SameSiteLaxMode,
	})
	http.SetCookie(w, &http.Cookie{
		Name:     "tenant_name",
		Value:    "",
		Path:     "/",
		MaxAge:   -1,
		HttpOnly: true,
		Domain:   domain,
		Secure:   secure,
		SameSite: http.SameSiteLaxMode,
	})
	http.SetCookie(w, &http.Cookie{
		Name:     "firsttime",
		Value:    "",
		Path:     "/",
		MaxAge:   -1,
		HttpOnly: false,
		Secure:   secure,
		SameSite: http.SameSiteLaxMode,
	})

	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Pragma", "no-cache")
	json.NewEncoder(w).Encode(map[string]any{"success": true})
}

// =============================================================================
// SPA Handler
// =============================================================================

func createSPAHandler() http.Handler {
	distFS, err := fs.Sub(spaFiles, "app/dist")
	if err != nil {
		zlog.Fatal().Err(err).Msg("Failed to get app/dist subdirectory")
	}
	return &spaFileServer{fs: distFS}
}

type spaFileServer struct {
	fs fs.FS
}

func (s *spaFileServer) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	urlPath := path.Clean(r.URL.Path)
	if urlPath == "/" {
		urlPath = "/index.html"
	}
	filePath := strings.TrimPrefix(urlPath, "/")

	file, err := s.fs.Open(filePath)
	if err != nil {
		s.serveIndex(w, r)
		return
	}
	file.Close()

	stat, err := fs.Stat(s.fs, filePath)
	if err != nil || stat.IsDir() {
		s.serveIndex(w, r)
		return
	}

	contentType := getContentType(filePath)
	if contentType != "" {
		w.Header().Set("Content-Type", contentType)
	}

	switch {
	case strings.HasPrefix(filePath, "assets/") || strings.HasPrefix(filePath, "_app/immutable/"):
		// Hashed asset bundles — safe to cache forever
		w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
	case filePath == "sw.js" || filePath == "manifest.json":
		// Service worker and manifest must never be cached — browsers enforce this
		// for SW files, but Cloudflare/CDN layers can serve stale copies otherwise.
		w.Header().Set("Cache-Control", "no-cache, no-store, must-revalidate")
		w.Header().Set("Service-Worker-Allowed", "/")
	}

	content, err := fs.ReadFile(s.fs, filePath)
	if err != nil {
		s.serveIndex(w, r)
		return
	}
	w.Write(content)
}

func (s *spaFileServer) serveIndex(w http.ResponseWriter, r *http.Request) {
	content, err := fs.ReadFile(s.fs, "index.html")
	if err != nil {
		http.Error(w, "index.html not found", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-cache, no-store, must-revalidate")
	w.Write(content)
}

func getContentType(filePath string) string {
	ext := strings.ToLower(path.Ext(filePath))
	switch ext {
	case ".html":
		return "text/html; charset=utf-8"
	case ".css":
		return "text/css; charset=utf-8"
	case ".js":
		return "application/javascript; charset=utf-8"
	case ".json":
		return "application/json; charset=utf-8"
	case ".svg":
		return "image/svg+xml"
	case ".png":
		return "image/png"
	case ".jpg", ".jpeg":
		return "image/jpeg"
	case ".webp":
		return "image/webp"
	case ".woff2":
		return "font/woff2"
	case ".ico":
		return "image/x-icon"
	case ".rsc":
		return "text/plain; charset=utf-8"
	default:
		return ""
	}
}

// handleDeviceHandoff is the landing endpoint for devices completing the Auth0 flow.
// After RegisterDevice succeeds, the agent directs its embedded WebView to:
//
//	https://console.wantastic.app/api/device-handoff?t=<portal_session_token>
//
// This handler validates the token, sets the tenant_session cookie, and redirects
// to the dashboard so the user is seamlessly logged in.
func (app *portalApp) handleDeviceHandoff(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	token := r.URL.Query().Get("t")
	if token == "" {
		http.Error(w, "Missing session token", http.StatusBadRequest)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Pragma", "no-cache")
	w.Header().Set("Referrer-Policy", "no-referrer")

	sess, err := app.sessionStore.GetSession(token)
	if err != nil || sess == nil {
		zlog.Warn().Str("token_prefix", safePrefix(token)).Msg("handleDeviceHandoff: invalid or expired session token")
		http.Error(w, "Session not found or expired", http.StatusUnauthorized)
		return
	}

	maxAge := int(time.Until(sess.ExpiresAt).Seconds())
	if maxAge < 0 {
		maxAge = 0
	}

	http.SetCookie(w, &http.Cookie{
		Name:     "tenant_session",
		Value:    token,
		Path:     "/",
		MaxAge:   maxAge,
		HttpOnly: true,
		Domain:   cookieDomain(r),
		Secure:   isSecureRequest(r, app.isSecure),
		SameSite: http.SameSiteLaxMode,
	})

	zlog.Debug().
		Str("tenant_id", sess.TenantID).
		Msg("handleDeviceHandoff: session cookie set, redirecting to dashboard")

	http.Redirect(w, r, "/", http.StatusSeeOther)
}

// safePrefix returns the first 8 chars of a string (or all of it if shorter),
// safe to log without leaking a full session token.
func safePrefix(s string) string {
	if len(s) <= 8 {
		return "[short]"
	}
	return s[:8] + "..."
}

func shortLogValue(value string) string {
	if len(value) <= 12 {
		return value
	}
	return value[:12] + "..."
}

// handleAgentCredentials is the endpoint Wantastic agents call before starting
// the HTTP OAuth2 device flow. It proves the caller is a genuine Wantastic agent
// by validating the device-specific cipher signature, then returns the public
// OAuth2 domain and client ID needed to complete the flow.
//
// The agent must send three headers:
//
//	x-wantastic-ts     — current Unix timestamp (seconds)
//	x-wantastic-device — device unique ID / hardware fingerprint
//	x-wantastic-sig    — HMAC-SHA256(SharedSecret, timestamp+":"+deviceID) as hex
//
// Only clients that know the shared secret (compiled into the Wantastic agent)
// can produce a valid signature. The device ID is baked into the HMAC so the
// proof is non-transferable between devices.
func (app *portalApp) handleAgentCredentials(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		// Pre-auth: agent fetches OAuth2 credentials before starting device flow
		app.handleAgentCredentialsGet(w, r)
	case http.MethodPost:
		// Post-auth: agent registers with bearer token after OAuth2 device flow
		app.handleAgentRegister(w, r)
	default:
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
	}
}

func (app *portalApp) handleAgentCredentialsGet(w http.ResponseWriter, r *http.Request) {
	deviceID, err := app.cipherInterceptor.ValidateHTTPRequest(r)
	if err != nil {
		zlog.Warn().Err(err).
			Str("remote", r.RemoteAddr).
			Msg("handleAgentCredentials: cipher validation failed")
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	zlog.Debug().
		Str("device_id", deviceID).
		Str("remote", r.RemoteAddr).
		Msg("handleAgentCredentials: verified Wantastic agent, issuing credentials")

	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	domain := app.publicOAuth2Domain()
	clientID := app.args.Auth0ClientID
	json.NewEncoder(w).Encode(map[string]string{
		"auth0_domain":    domain,
		"auth0_client_id": clientID,
		"domain":          domain,
		"client_id":       clientID,
	})
}

func (app *portalApp) handleAgentClaimConfig(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	deviceID, err := app.cipherInterceptor.ValidateHTTPRequest(r)
	if err != nil {
		zlog.Warn().Err(err).
			Str("remote", r.RemoteAddr).
			Msg("handleAgentClaimConfig: cipher validation failed")
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}
	publicKey := strings.TrimSpace(r.URL.Query().Get("public_key"))
	if publicKey == "" {
		http.Error(w, "public_key required", http.StatusBadRequest)
		return
	}
	if app.services == nil || app.services.Auth == nil {
		http.Error(w, "Core not available", http.StatusServiceUnavailable)
		return
	}

	resp, err := app.services.Auth.GetClaimedDeviceConfig(r.Context(), &proto.GetClaimedDeviceConfigRequest{
		PublicKey: publicKey,
	})
	if err != nil {
		zlog.Error().Err(err).
			Str("device_id", deviceID).
			Str("public_key", shortLogValue(publicKey)).
			Msg("handleAgentClaimConfig: GetClaimedDeviceConfig failed")
		http.Error(w, "Claim config unavailable", http.StatusServiceUnavailable)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"claimed":              resp.Claimed,
		"public_key":           resp.PublicKey,
		"assigned_ip":          resp.AssignedIp,
		"server_key":           resp.ServerKey,
		"endpoint":             resp.Endpoint,
		"allowed_ips":          resp.AllowedIps,
		"dns_servers":          resp.DnsServers,
		"persistent_keepalive": resp.PersistentKeepalive,
		"mtu":                  resp.Mtu,
		"listen_port":          resp.ListenPort,
	})
}

func (app *portalApp) publicOAuth2Domain() string {
	if strings.TrimSpace(app.oauth2Domain) != "" {
		return app.oauth2Domain
	}
	return issuerHost(app.configuredOAuth2Issuer())
}

// handleAgentRegister handles POST /api/agent/credentials — registers a device
// using either an OAuth2 bearer token from the device flow or an enrollment
// token from `wantasticd login --token`. It creates a WireGuard peer and
// returns the encrypted config.
func (app *portalApp) handleAgentRegister(w http.ResponseWriter, r *http.Request) {
	// Extract bearer token
	authHeader := r.Header.Get("Authorization")
	if !strings.HasPrefix(authHeader, "Bearer ") {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}
	accessToken := strings.TrimPrefix(authHeader, "Bearer ")

	// Parse request body
	var reqBody struct {
		Hostname string `json:"hostname"`
		OS       string `json:"os"`
		Arch     string `json:"arch"`
		Nonce    uint64 `json:"nonce"`
	}
	if err := json.NewDecoder(r.Body).Decode(&reqBody); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	// Prefer the HMAC-authenticated device ID from the agent's headers over
	// the JWT claim (which may be "unknown-device" for older agents that didn't
	// send device_id during StartDeviceFlow).
	deviceID := r.Header.Get("x-wantastic-device")

	if app.services == nil || app.services.Auth == nil {
		http.Error(w, "Core not available", http.StatusServiceUnavailable)
		return
	}

	outCtx := r.Context()
	cc := &auth.CallContext{DeviceID: deviceID}

	// Validate as OAuth2 first. If that fails, let the core try the bearer as an
	// enrollment token for headless `wantasticd login --token` installs.
	if app.oauth2Server != nil {
		claims, err := app.oauth2Server.ValidateAccessToken(accessToken)
		if err == nil {
			displayName := claims.Name
			if displayName == "" && claims.Email != "" {
				displayName = strings.SplitN(claims.Email, "@", 2)[0]
			}
			if deviceID == "" {
				deviceID = claims.DeviceID
			}
			cc.Auth0Sub = claims.UserID
			cc.Email = claims.Email
			cc.FullName = displayName
			cc.DeviceID = deviceID
		} else {
			zlog.Debug().Err(err).Msg("handleAgentRegister: bearer token is not an OAuth2 access token, falling back to enrollment-token registration")
		}
	}
	outCtx = auth.WithCallContext(outCtx, cc)

	coreResp, err := app.services.Auth.RegisterDevice(outCtx, &proto.RegisterDeviceRequest{
		Token:    accessToken,
		DeviceId: deviceID,
		Hostname: reqBody.Hostname,
		Os:       reqBody.OS,
		Arch:     reqBody.Arch,
		Nonce:    int64(reqBody.Nonce),
	})
	if err != nil {
		zlog.Error().Err(err).Str("device_id", deviceID).Msg("handleAgentRegister: RegisterDevice failed")
		switch errs.CodeOf(err) {
		case errs.Unauthenticated:
			http.Error(w, "Invalid or expired login token", http.StatusUnauthorized)
			return
		case errs.FailedPrecondition:
			// Tier/quota: agent register returns FailedPrecondition for limit-reached.
			http.Error(w, "Peer limit reached", http.StatusForbidden)
			return
		}
		http.Error(w, "Registration failed", http.StatusInternalServerError)
		return
	}

	// Extract session token (first segment of internal token: "session|tenantID|tier")
	agentToken := coreResp.Token
	if idx := strings.Index(agentToken, "|"); idx > 0 {
		agentToken = agentToken[:idx]
	}

	// Build server URL for the agent's gRPC runtime connection
	serverURL := coreResp.Endpoint
	if serverURL != "" {
		serverURL = serverURL + ":443"
	}

	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	// Build handoff URL so the user can confirm the device appeared in the console
	handoffURL := fmt.Sprintf("https://%s/api/device-handoff?t=%s", r.Host, url.QueryEscape(agentToken))

	json.NewEncoder(w).Encode(map[string]interface{}{
		"encrypted_config": coreResp.EncryptedConfig,
		"token":            agentToken,
		"server_url":       serverURL,
		"handoff_url":      handoffURL,
	})
}

func (app *portalApp) publicOAuth2Issuer() string {
	if strings.TrimSpace(app.oauth2Issuer) != "" {
		return strings.TrimRight(strings.TrimSpace(app.oauth2Issuer), "/")
	}
	return normalizeOAuth2Issuer(app.publicOAuth2Domain())
}

// handleDeviceLogin is the browser-facing page agents open to authorize themselves.
//
// The agent constructs the URL as:
//
//	https://console.wantastic.app/device-login?code=XXXX-YYYY
//
// If a code is present the page immediately redirects the user to the portal's
// activation page where they log in and approve the pending device request.
//
// If no code is present a minimal HTML form is shown so the user can enter
// the code manually (fallback for headless / non-interactive agents).
func (app *portalApp) handleDeviceLogin(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	code := strings.TrimSpace(r.URL.Query().Get("code"))

	// If the agent supplied the code, redirect straight to the activation UI.
	if code != "" {
		target := app.publicOAuth2Issuer() + "/activate?user_code=" + url.QueryEscape(code)
		http.Redirect(w, r, target, http.StatusFound)
		return
	}

	// No code provided — show a minimal branded form so the user can enter it.
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(http.StatusOK)
	w.Write([]byte(`<!DOCTYPE html>
<html lang="en">
<head>
  <meta charset="UTF-8">
  <meta name="viewport" content="width=device-width, initial-scale=1.0">
  <title>Connect Device — Wantastic</title>
  <style>
    *, *::before, *::after { box-sizing: border-box; margin: 0; padding: 0; }
    body {
      font-family: -apple-system, BlinkMacSystemFont, "Segoe UI", Roboto, sans-serif;
      background: #0f1117;
      color: #e2e8f0;
      display: flex;
      align-items: center;
      justify-content: center;
      min-height: 100vh;
      padding: 1rem;
    }
    .card {
      background: #1a1d27;
      border: 1px solid #2d3748;
      border-radius: 12px;
      padding: 2.5rem 2rem;
      width: 100%%;
      max-width: 380px;
      text-align: center;
    }
    .logo { font-size: 1.5rem; font-weight: 700; color: #7c3aed; margin-bottom: 0.5rem; }
    h1 { font-size: 1.1rem; font-weight: 600; margin-bottom: 0.4rem; }
    p  { font-size: 0.875rem; color: #94a3b8; margin-bottom: 1.5rem; line-height: 1.5; }
    input {
      width: 100%%;
      padding: 0.75rem 1rem;
      border: 1px solid #374151;
      border-radius: 8px;
      background: #111827;
      color: #f1f5f9;
      font-size: 1rem;
      letter-spacing: 0.1em;
      text-align: center;
      text-transform: uppercase;
      margin-bottom: 1rem;
      outline: none;
    }
    input:focus { border-color: #7c3aed; }
    button {
      width: 100%%;
      padding: 0.75rem;
      background: #7c3aed;
      color: #fff;
      border: none;
      border-radius: 8px;
      font-size: 0.95rem;
      font-weight: 600;
      cursor: pointer;
    }
    button:hover { background: #6d28d9; }
    .hint { margin-top: 1rem; font-size: 0.8rem; color: #64748b; }
  </style>
</head>
<body>
  <div class="card">
    <div class="logo">Wantastic</div>
    <h1>Connect your device</h1>
    <p>Enter the code shown in the Wantastic agent on your device.</p>
    <form id="f">
      <input id="code" name="code" type="text"
             placeholder="XXXX-YYYY" autocomplete="off" autofocus
             pattern="[A-Za-z0-9]{4}-[A-Za-z0-9]{4}"
             title="8-character code in the format XXXX-YYYY" required>
      <button type="submit">Continue</button>
    </form>
    <p class="hint">You will be redirected to log in and approve the connection.</p>
  </div>
  <script>
    document.getElementById('f').addEventListener('submit', function(e) {
      e.preventDefault();
      var code = document.getElementById('code').value.trim().toUpperCase();
      if (!code) return;
      window.location.href = '/device-login?code=' + encodeURIComponent(code);
    });
  </script>
</body>
</html>`))
}

// =============================================================================
// OAuth2 HTTP Handlers (Internal OAuth2 Mode)
// =============================================================================

// handleOAuth2DeviceCode handles POST /oauth/device/code
// RFC 8628 Device Authorization Request
func (app *portalApp) handleOAuth2DeviceCode(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/oauth/device/code" && r.URL.Path != "/oauth/device/code/" {
		writeOAuth2JSONError(w, http.StatusNotFound, "not_found", "unknown OAuth2 endpoint")
		return
	}

	if r.Method != http.MethodPost {
		writeOAuth2JSONError(w, http.StatusMethodNotAllowed, "method_not_allowed", "POST is required")
		return
	}

	// Parse form data
	if err := r.ParseForm(); err != nil {
		writeOAuth2JSONError(w, http.StatusBadRequest, "invalid_request", "invalid form body")
		return
	}

	clientID := r.FormValue("client_id")
	if clientID == "" {
		clientID = "wantastic-device-client"
	}

	if app.oauth2Server == nil {
		zlog.Error().Msg("handleOAuth2DeviceCode: internal OAuth2 server is not initialized")
		writeOAuth2JSONError(w, http.StatusServiceUnavailable, "server_error", "OAuth2 device flow is not available")
		return
	}

	// In a real implementation, we would validate client_id against registered clients
	// For now, we accept any client_id

	// Generate a device ID (in production, this would come from the agent)
	deviceID := r.Header.Get("x-wantastic-device")
	if deviceID == "" {
		deviceID = r.FormValue("device_id")
	}
	if deviceID == "" {
		deviceID = "unknown-device"
	}

	// Start device flow
	resp, err := app.oauth2Server.StartDeviceFlow(clientID, deviceID)
	if err != nil {
		zlog.Error().Err(err).Msg("handleOAuth2DeviceCode: failed to start device flow")
		writeOAuth2JSONError(w, http.StatusInternalServerError, "server_error", "failed to start device flow")
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"device_code":               resp.DeviceCode,
		"user_code":                 resp.UserCode,
		"verification_uri":          resp.VerificationURI,
		"verification_uri_complete": resp.VerificationURIComplete,
		"expires_in":                resp.ExpiresIn,
		"interval":                  resp.Interval,
	})
}

// handleOAuth2Token handles POST /oauth/token
// Supports both RFC 8628 Device Authorization Grant and RFC 6749 Authorization Code Flow with PKCE
func (app *portalApp) handleOAuth2Token(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/oauth/token" && r.URL.Path != "/oauth/token/" {
		writeOAuth2JSONError(w, http.StatusNotFound, "not_found", "unknown OAuth2 endpoint")
		return
	}

	if r.Method != http.MethodPost {
		writeOAuth2JSONError(w, http.StatusMethodNotAllowed, "method_not_allowed", "POST is required")
		return
	}

	// Parse form data
	if err := r.ParseForm(); err != nil {
		writeOAuth2JSONError(w, http.StatusBadRequest, "invalid_request", "invalid form body")
		return
	}

	if app.oauth2Server == nil {
		zlog.Error().Msg("handleOAuth2Token: internal OAuth2 server is not initialized")
		writeOAuth2JSONError(w, http.StatusServiceUnavailable, "server_error", "OAuth2 device flow is not available")
		return
	}

	grantType := r.FormValue("grant_type")

	switch grantType {
	case "urn:ietf:params:oauth:grant-type:device_code":
		app.handleDeviceCodeToken(w, r)
	case "authorization_code":
		app.handleAuthorizationCodeToken(w, r)
	default:
		writeOAuth2JSONError(w, http.StatusBadRequest, "unsupported_grant_type", "unsupported grant_type")
	}
}

func writeOAuth2JSONError(w http.ResponseWriter, status int, code, description string) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	resp := oauth2.TokenErrorResponse{Error: code}
	if description != "" {
		resp.ErrorDescription = description
	}
	_ = json.NewEncoder(w).Encode(resp)
}

// handleDeviceCodeToken handles device authorization grant token requests (RFC 8628)
func (app *portalApp) handleDeviceCodeToken(w http.ResponseWriter, r *http.Request) {
	deviceCode := r.FormValue("device_code")
	if deviceCode == "" {
		writeOAuth2JSONError(w, http.StatusBadRequest, "invalid_request", "device_code is required")
		return
	}

	// Poll for result
	tokenResp, err := app.oauth2Server.PollDeviceToken(deviceCode)
	if err != nil {
		errStr := err.Error()
		// Check for specific OAuth2 errors per RFC 8628
		switch errStr {
		case oauth2.ErrAuthorizationPending, oauth2.ErrSlowDown:
			writeOAuth2JSONError(w, http.StatusBadRequest, errStr, "")
			return
		case oauth2.ErrExpiredToken, oauth2.ErrAccessDenied:
			writeOAuth2JSONError(w, http.StatusBadRequest, errStr, "")
			return
		default:
			zlog.Error().Err(err).Msg("handleDeviceCodeToken: failed to poll device flow")
			writeOAuth2JSONError(w, http.StatusInternalServerError, "server_error", "failed to poll device flow")
			return
		}
	}

	// Success — return the access token. The agent will separately call
	// POST /api/agent/credentials to register the device with proper metadata
	// (hostname, OS, arch, nonce for encrypted config). We do NOT register
	// inline here because the token poll request lacks the device metadata and
	// would create a peer that conflicts with the agent's subsequent registration.
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"access_token": tokenResp.AccessToken,
		"token_type":   tokenResp.TokenType,
		"expires_in":   tokenResp.ExpiresIn,
	})
}

// handleAuthorizationCodeToken handles authorization code exchange with PKCE (RFC 6749 + RFC 7636).
// On success it registers the device with the core and returns the full peer WireGuard config
// inline so the agent does not need a separate RegisterDevice gRPC call.
func (app *portalApp) handleAuthorizationCodeToken(w http.ResponseWriter, r *http.Request) {
	code := r.FormValue("code")
	codeVerifier := r.FormValue("code_verifier")
	redirectURI := r.FormValue("redirect_uri")
	clientID := r.FormValue("client_id")

	if code == "" {
		http.Error(w, `{"error": "invalid_request", "error_description": "code is required"}`, http.StatusBadRequest)
		return
	}
	if codeVerifier == "" {
		http.Error(w, `{"error": "invalid_request", "error_description": "code_verifier is required"}`, http.StatusBadRequest)
		return
	}

	// Exchange authorization code for tokens (with PKCE verification)
	tokenResp, err := app.oauth2Server.ExchangeAuthorizationCode(code, codeVerifier, redirectURI, clientID)
	if err != nil {
		errStr := err.Error()
		switch errStr {
		case "authorization pending":
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusBadRequest)
			json.NewEncoder(w).Encode(map[string]interface{}{
				"error":             "authorization_pending",
				"error_description": "User has not yet authorized the request",
			})
			return
		case oauth2.ErrExpiredToken:
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusBadRequest)
			json.NewEncoder(w).Encode(map[string]interface{}{
				"error":             errStr,
				"error_description": "The authorization code has expired",
			})
			return
		case "invalid code_verifier":
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusBadRequest)
			json.NewEncoder(w).Encode(map[string]interface{}{
				"error":             "invalid_grant",
				"error_description": "Invalid code verifier",
			})
			return
		default:
			zlog.Error().Err(err).Msg("handleAuthorizationCodeToken: failed to exchange code")
			http.Error(w, `{"error": "server_error"}`, http.StatusInternalServerError)
			return
		}
	}

	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")

	// ── Register device with core and return peer config inline ──────────────
	// Validate the access token to extract identity claims (device_id, email, etc.)
	claims, err := app.oauth2Server.ValidateAccessToken(tokenResp.AccessToken)
	if err != nil {
		zlog.Error().Err(err).Msg("handleAuthorizationCodeToken: failed to validate access token post-exchange")
		json.NewEncoder(w).Encode(map[string]any{
			"access_token": tokenResp.AccessToken,
			"token_type":   tokenResp.TokenType,
			"expires_in":   tokenResp.ExpiresIn,
		})
		return
	}

	displayName := claims.Name
	if displayName == "" && claims.Email != "" {
		displayName = strings.SplitN(claims.Email, "@", 2)[0]
	}

	if app.services == nil || app.services.Auth == nil {
		// Core not connected — return access token only (agent must call RegisterDevice separately)
		json.NewEncoder(w).Encode(map[string]any{
			"access_token": tokenResp.AccessToken,
			"token_type":   tokenResp.TokenType,
			"expires_in":   tokenResp.ExpiresIn,
		})
		return
	}

	// Optional device metadata from the agent (passed alongside the token exchange request)
	hostname := r.FormValue("hostname")
	osName := r.FormValue("os")
	arch := r.FormValue("arch")
	nonce, _ := strconv.ParseInt(r.FormValue("nonce"), 10, 64)

	// Inject trusted identity into CallContext — same pattern as registerDeviceInternal.
	outCtx := auth.WithCallContext(r.Context(), &auth.CallContext{
		Auth0Sub: claims.UserID,
		Email:    claims.Email,
		FullName: displayName,
		DeviceID: claims.DeviceID,
	})

	coreResp, err := app.services.Auth.RegisterDevice(outCtx, &proto.RegisterDeviceRequest{
		Token:    tokenResp.AccessToken,
		DeviceId: claims.DeviceID,
		Hostname: hostname,
		Os:       osName,
		Arch:     arch,
		Nonce:    nonce,
	})
	if err != nil {
		zlog.Error().Err(err).
			Str("device_id", claims.DeviceID).
			Str("email", claims.Email).
			Msg("handleAuthorizationCodeToken: RegisterDevice failed")
		// Return access token so agent can retry via gRPC RegisterDevice
		json.NewEncoder(w).Encode(map[string]any{
			"access_token": tokenResp.AccessToken,
			"token_type":   tokenResp.TokenType,
			"expires_in":   tokenResp.ExpiresIn,
		})
		return
	}

	// Unpack internal token (grpcSessionToken|tenantID|tier)
	parts := strings.SplitN(coreResp.Token, "|", 3)
	if len(parts) != 3 || parts[0] == "" || parts[1] == "" {
		zlog.Error().
			Str("token_shape", fmt.Sprintf("parts=%d", len(parts))).
			Msg("handleAuthorizationCodeToken: malformed internal token from core")
		json.NewEncoder(w).Encode(map[string]any{
			"access_token": tokenResp.AccessToken,
			"token_type":   tokenResp.TokenType,
			"expires_in":   tokenResp.ExpiresIn,
		})
		return
	}
	grpcSessionToken, tenantID, tier := parts[0], parts[1], parts[2]

	// Create portal HTTP session — agent navigates its WebView to /api/device-handoff?t=<Token>
	sess, err := app.sessionStore.CreateSession(tenantID, displayName, claims.Email, tier, grpcSessionToken, true)
	if err != nil {
		zlog.Error().Str("tenant_id", tenantID).Msg("handleAuthorizationCodeToken: failed to create portal session")
		json.NewEncoder(w).Encode(map[string]any{
			"access_token": tokenResp.AccessToken,
			"token_type":   tokenResp.TokenType,
			"expires_in":   tokenResp.ExpiresIn,
		})
		return
	}

	// Release the device lock — this flow is now complete
	if app.redisClient != nil && claims.DeviceID != "" {
		app.redisClient.Del(r.Context(), "oauth2:device_lock:"+claims.DeviceID)
	}

	// Return full peer config so the agent needs no separate RegisterDevice gRPC call
	json.NewEncoder(w).Encode(map[string]any{
		"access_token":         tokenResp.AccessToken,
		"token_type":           tokenResp.TokenType,
		"expires_in":           tokenResp.ExpiresIn,
		"portal_session_token": sess.Token,
		"server_key":           coreResp.ServerKey,
		"endpoint":             coreResp.Endpoint,
		"allowed_ips":          coreResp.AllowedIps,
		"dns_servers":          coreResp.DnsServers,
		"routes":               coreResp.Routes,
		"mtu":                  coreResp.Mtu,
		"persistent_keepalive": coreResp.PersistentKeepalive,
		"listen_port":          coreResp.ListenPort,
	})
}

// handleOAuth2Authorize handles GET /oauth/authorize
// RFC 6749 Authorization Endpoint with PKCE (RFC 7636)
// This is the entry point for OAuth2 Authorization Code Flow with PKCE
func (app *portalApp) handleOAuth2Authorize(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Parse query parameters
	query := r.URL.Query()

	clientID := query.Get("client_id")
	redirectURI := query.Get("redirect_uri")
	state := query.Get("state")
	scope := query.Get("scope")
	codeChallenge := query.Get("code_challenge")
	codeChallengeMethod := query.Get("code_challenge_method")
	deviceID := query.Get("device_id")

	// Validate required parameters
	if clientID == "" {
		app.redirectOAuth2Error(w, r, redirectURI, "invalid_request", "client_id is required", state)
		return
	}
	if redirectURI == "" {
		app.redirectOAuth2Error(w, r, "", "invalid_request", "redirect_uri is required", state)
		return
	}
	if codeChallenge == "" {
		app.redirectOAuth2Error(w, r, redirectURI, "invalid_request", "code_challenge is required (PKCE is mandatory)", state)
		return
	}

	// Device deduplication: prevent the same device_id from starting concurrent auth flows.
	// This ensures at most one pending authorization per device at any time.
	if deviceID != "" && app.redisClient != nil {
		lockKey := "oauth2:device_lock:" + deviceID
		ok, err := app.redisClient.SetNX(r.Context(), lockKey, "1", 10*time.Minute).Result()
		if err == nil && !ok {
			app.redirectOAuth2Error(w, r, redirectURI, "device_flow_in_progress", "A device authorization flow is already in progress for this device", state)
			return
		}
	}

	// Start authorization flow
	authReq, err := app.oauth2Server.StartAuthorizationFlow(clientID, redirectURI, state, scope, codeChallenge, codeChallengeMethod, deviceID)
	if err != nil {
		zlog.Error().Err(err).Msg("handleOAuth2Authorize: failed to start authorization flow")
		app.redirectOAuth2Error(w, r, redirectURI, "server_error", "failed to start authorization", state)
		return
	}

	// Store authorization code and state in secure cookies.
	// Path "/" so the SPA at "/#oauth2-consent" can POST to "/api/oauth/*" with these cookies.
	secure := isSecureRequest(r, app.isSecure)
	http.SetCookie(w, &http.Cookie{
		Name:     "oauth2_auth_code",
		Value:    authReq.AuthorizationCode,
		Path:     "/",
		MaxAge:   600, // 10 minutes
		HttpOnly: true,
		Secure:   secure,
		SameSite: http.SameSiteLaxMode,
	})
	http.SetCookie(w, &http.Cookie{
		Name:     "oauth2_state",
		Value:    state,
		Path:     "/",
		MaxAge:   600,
		HttpOnly: true,
		Secure:   secure,
		SameSite: http.SameSiteLaxMode,
	})

	// Redirect to Svelte SPA consent page.
	// query string is before hash so window.location.search works in the SPA.
	consentURL := "/?code=" + url.QueryEscape(authReq.AuthorizationCode) + "#oauth2-consent"
	http.Redirect(w, r, consentURL, http.StatusFound)
}

// redirectOAuth2Error redirects with OAuth2 error parameters
func (app *portalApp) redirectOAuth2Error(w http.ResponseWriter, r *http.Request, redirectURI, errCode, errDesc, state string) {
	if redirectURI == "" {
		// No redirect URI, show error page
		http.Error(w, errDesc, http.StatusBadRequest)
		return
	}

	// Validate redirect URI scheme to prevent open-redirect to javascript: or data: URIs
	parsed, err := url.Parse(redirectURI)
	if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") {
		http.Error(w, "invalid redirect_uri", http.StatusBadRequest)
		return
	}

	q := url.Values{}
	q.Set("error", errCode)
	q.Set("error_description", errDesc)
	if state != "" {
		q.Set("state", state)
	}
	http.Redirect(w, r, redirectURI+"?"+q.Encode(), http.StatusFound)
}

// handleOAuth2Consent handles GET /oauth/consent
// Shows the consent page for PKCE authorization flow
func (app *portalApp) handleOAuth2Consent(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	code := r.URL.Query().Get("code")
	if code == "" {
		// Try to get from cookie
		cookie, err := r.Cookie("oauth2_auth_code")
		if err == nil {
			code = cookie.Value
		}
	}

	if code == "" {
		app.renderOAuth2Error(w, "Invalid or missing authorization code. Please try again.")
		return
	}

	// Get the authorization request
	authReq, err := app.oauth2Server.GetAuthorizationRequest(code)
	if err != nil {
		app.renderOAuth2Error(w, "This authorization request is invalid or has expired. Please try again.")
		return
	}

	// Check if user is logged in with a valid session
	sessionCookie, err := r.Cookie("tenant_session")
	var validSession *session.TenantSession
	if err == nil && sessionCookie.Value != "" {
		validSession, _ = app.sessionStore.GetSession(sessionCookie.Value)
	}

	if validSession == nil {
		// Show login page with authorization context
		app.renderOAuth2LoginForAuth(w, authReq)
		return
	}

	// User is logged in, show consent page
	app.renderOAuth2ConsentPage(w, authReq)
}

// renderOAuth2LoginForAuth renders the login page for PKCE flow
func (app *portalApp) renderOAuth2LoginForAuth(w http.ResponseWriter, authReq *oauth2.AuthorizationRequest) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(http.StatusOK)
	w.Write([]byte(`<!DOCTYPE html>
<html lang="en">
<head>
    <meta charset="UTF-8">
    <title>Sign In — Wantastic</title>
    <meta name="viewport" content="width=device-width, initial-scale=1">
    <style>
        *, *::before, *::after { box-sizing: border-box; margin: 0; padding: 0; }
        body { font-family: -apple-system, BlinkMacSystemFont, "Segoe UI", Roboto, sans-serif; background: #0f1117; color: #e2e8f0; display: flex; align-items: center; justify-content: center; min-height: 100vh; padding: 1rem; }
        .card { background: #1a1d27; border: 1px solid #2d3748; border-radius: 12px; padding: 2rem; width: 100%; max-width: 380px; }
        .logo { font-size: 1.25rem; font-weight: 700; color: #7c3aed; margin-bottom: 1.5rem; }
        .request-banner { background: rgba(124,58,237,0.1); border: 1px solid rgba(124,58,237,0.3); border-radius: 8px; padding: 0.75rem 1rem; margin-bottom: 1.5rem; font-size: 0.85rem; color: #c4b5fd; }
        h1 { font-size: 1.05rem; font-weight: 700; margin-bottom: 1.25rem; }
        label { display: block; font-size: 0.75rem; font-weight: 600; color: #94a3b8; text-transform: uppercase; letter-spacing: 0.05em; margin-bottom: 0.35rem; }
        input { width: 100%; padding: 0.7rem 0.875rem; border: 1px solid #374151; border-radius: 8px; background: #111827; color: #f1f5f9; font-size: 0.9rem; margin-bottom: 1rem; outline: none; }
        input:focus { border-color: #7c3aed; }
        button { width: 100%; padding: 0.75rem; background: #7c3aed; color: #fff; border: none; border-radius: 8px; font-size: 0.9rem; font-weight: 600; cursor: pointer; }
        button:hover { background: #6d28d9; }
        .meta { margin-top: 1.25rem; padding-top: 1rem; border-top: 1px solid #1f2937; font-size: 0.75rem; color: #4b5563; }
        code { font-family: monospace; color: #6b7280; }
    </style>
</head>
<body>
    <div class="card">
        <div class="logo">Wantastic</div>
        <div class="request-banner">&#x1F512; <strong>Wantastic Agent</strong> is requesting access to your account</div>
        <h1>Sign in to continue</h1>
        <form method="POST" action="/api/oauth/consent-login">
            <input type="hidden" name="auth_code" value="` + authReq.AuthorizationCode + `">
            <label>Email</label>
            <input type="email" name="email" placeholder="you@example.com" required autofocus>
            <label>Password</label>
            <input type="password" name="password" placeholder="Password" required>
            <button type="submit">Sign In &amp; Continue</button>
        </form>
        <div class="meta">Client: <code>` + authReq.ClientID + `</code> &nbsp;&bull;&nbsp; Scope: <code>` + authReq.Scope + `</code></div>
    </div>
</body>
</html>`))
}

// renderOAuth2LoginError re-renders the PKCE login form with an inline error message
func (app *portalApp) renderOAuth2LoginError(w http.ResponseWriter, authCode string, errMsg string) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(http.StatusOK)
	w.Write([]byte(`<!DOCTYPE html>
<html>
<head>
    <title>Sign In - Wantastic</title>
    <meta name="viewport" content="width=device-width, initial-scale=1">
    <style>
        *, *::before, *::after { box-sizing: border-box; margin: 0; padding: 0; }
        body { font-family: -apple-system, BlinkMacSystemFont, "Segoe UI", Roboto, sans-serif; background: #0f1117; color: #e2e8f0; display: flex; align-items: center; justify-content: center; min-height: 100vh; padding: 1rem; }
        .card { background: #1a1d27; border: 1px solid #2d3748; border-radius: 12px; padding: 2rem; width: 100%; max-width: 380px; }
        h1 { font-size: 1.1rem; font-weight: 700; margin-bottom: 1.25rem; }
        .error { background: rgba(239,68,68,0.12); border: 1px solid rgba(239,68,68,0.35); border-radius: 8px; padding: 0.75rem 1rem; margin-bottom: 1rem; color: #fca5a5; font-size: 0.85rem; }
        label { display: block; font-size: 0.75rem; font-weight: 600; color: #94a3b8; text-transform: uppercase; letter-spacing: 0.05em; margin-bottom: 0.35rem; }
        input { width: 100%; padding: 0.7rem 0.875rem; border: 1px solid #374151; border-radius: 8px; background: #111827; color: #f1f5f9; font-size: 0.9rem; margin-bottom: 1rem; outline: none; }
        input:focus { border-color: #7c3aed; }
        button { width: 100%; padding: 0.75rem; background: #7c3aed; color: #fff; border: none; border-radius: 8px; font-size: 0.95rem; font-weight: 600; cursor: pointer; }
        button:hover { background: #6d28d9; }
    </style>
</head>
<body>
    <div class="card">
        <h1>Sign In to Wantastic</h1>
        <div class="error">` + errMsg + `</div>
        <form method="POST" action="/api/oauth/consent-login">
            <input type="hidden" name="auth_code" value="` + authCode + `">
            <label>Email</label>
            <input type="email" name="email" placeholder="you@example.com" required autofocus>
            <label>Password</label>
            <input type="password" name="password" placeholder="Password" required>
            <button type="submit">Sign In &amp; Authorize</button>
        </form>
    </div>
</body>
</html>`))
}

// renderOAuth2ConsentPage renders the consent page for PKCE flow
func (app *portalApp) renderOAuth2ConsentPage(w http.ResponseWriter, authReq *oauth2.AuthorizationRequest) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(http.StatusOK)
	w.Write([]byte(`<!DOCTYPE html>
<html lang="en">
<head>
    <meta charset="UTF-8">
    <title>Authorize — Wantastic</title>
    <meta name="viewport" content="width=device-width, initial-scale=1">
    <style>
        *, *::before, *::after { box-sizing: border-box; margin: 0; padding: 0; }
        body { font-family: -apple-system, BlinkMacSystemFont, "Segoe UI", Roboto, sans-serif; background: #0f1117; color: #e2e8f0; display: flex; align-items: center; justify-content: center; min-height: 100vh; padding: 1rem; }
        .card { background: #1a1d27; border: 1px solid #2d3748; border-radius: 12px; padding: 2rem; width: 100%; max-width: 380px; }
        .logo { font-size: 1.25rem; font-weight: 700; color: #7c3aed; margin-bottom: 1.5rem; }
        .app-banner { display: flex; align-items: center; gap: 0.75rem; background: rgba(124,58,237,0.1); border: 1px solid rgba(124,58,237,0.25); border-radius: 10px; padding: 0.875rem 1rem; margin-bottom: 1.25rem; }
        .app-icon { width: 36px; height: 36px; background: #7c3aed; border-radius: 8px; display: flex; align-items: center; justify-content: center; flex-shrink: 0; font-size: 1.1rem; }
        .app-name { font-weight: 700; font-size: 0.9rem; }
        .app-desc { font-size: 0.78rem; color: #94a3b8; margin-top: 1px; }
        h2 { font-size: 0.95rem; font-weight: 600; margin-bottom: 0.75rem; color: #94a3b8; }
        .scope-list { list-style: none; display: flex; flex-direction: column; gap: 0.5rem; margin-bottom: 1.5rem; }
        .scope-item { display: flex; align-items: center; gap: 0.6rem; background: rgba(255,255,255,0.03); border: 1px solid #1f2937; border-radius: 8px; padding: 0.6rem 0.875rem; font-size: 0.85rem; }
        .scope-dot { width: 6px; height: 6px; background: #4ade80; border-radius: 50%; flex-shrink: 0; }
        .buttons { display: flex; gap: 0.75rem; }
        button { flex: 1; padding: 0.7rem; border: none; border-radius: 8px; font-size: 0.875rem; font-weight: 600; cursor: pointer; }
        .primary { background: #7c3aed; color: #fff; }
        .primary:hover { background: #6d28d9; }
        .secondary { background: transparent; border: 1px solid #374151; color: #94a3b8; }
        .secondary:hover { background: #1f2937; color: #e2e8f0; }
    </style>
</head>
<body>
    <div class="card">
        <div class="logo">Wantastic</div>
        <div class="app-banner">
            <div class="app-icon">&#x1F916;</div>
            <div>
                <div class="app-name">Wantastic Agent</div>
                <div class="app-desc">` + authReq.ClientID + `</div>
            </div>
        </div>
        <h2>This app will be able to:</h2>
        <ul class="scope-list">
            <li class="scope-item"><span class="scope-dot"></span>Create API keys for device authentication</li>
            <li class="scope-item"><span class="scope-dot"></span>Access your profile information</li>
            <li class="scope-item"><span class="scope-dot"></span>Register this device to your network</li>
        </ul>
        <form method="POST" action="/api/oauth/authorize-confirm">
            <input type="hidden" name="auth_code" value="` + authReq.AuthorizationCode + `">
            <div class="buttons">
                <button type="submit" name="action" value="deny" class="secondary">Deny</button>
                <button type="submit" name="action" value="allow" class="primary">Allow Access</button>
            </div>
        </form>
    </div>
</body>
</html>`))
}

// handleOAuth2Activate handles GET /activate
// Redirects to the Svelte SPA activation page, passing user_code as a query param.
func (app *portalApp) handleOAuth2Activate(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	userCode := strings.TrimSpace(r.URL.Query().Get("user_code"))
	if userCode != "" {
		// Redirect to SPA with user_code preserved in query string before hash
		activateURL := "/?user_code=" + url.QueryEscape(userCode) + "#activate"
		http.Redirect(w, r, activateURL, http.StatusFound)
		return
	}
	// No user_code — redirect to SPA activate page for manual code entry
	http.Redirect(w, r, "/#activate", http.StatusFound)
}

// handleOAuth2Approve handles POST /api/oauth/approve
// Approves a device authorization request
func (app *portalApp) handleOAuth2Approve(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Verify user is logged in
	cookie, err := r.Cookie("tenant_session")
	if err != nil || cookie.Value == "" {
		http.Error(w, `{"error": "unauthorized"}`, http.StatusUnauthorized)
		return
	}

	sess, err := app.sessionStore.GetSession(cookie.Value)
	if err != nil || sess == nil {
		http.Error(w, `{"error": "unauthorized"}`, http.StatusUnauthorized)
		return
	}

	// Parse request
	if err := r.ParseForm(); err != nil {
		http.Error(w, `{"error": "invalid_request"}`, http.StatusBadRequest)
		return
	}

	userCode := r.FormValue("user_code")
	if userCode == "" {
		http.Error(w, `{"error": "missing_user_code"}`, http.StatusBadRequest)
		return
	}

	// Approve the request
	if err := app.oauth2Server.AuthorizeDevice(userCode, sess.TenantID, sess.Email, sess.FullName, sess.TenantID, sess.Tier); err != nil {
		zlog.Error().Err(err).Str("user_code", userCode).Msg("handleOAuth2Approve: failed to approve request")
		http.Error(w, `{"error": "approval_failed"}`, http.StatusBadRequest)
		return
	}

	// Return success
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"success": true,
		"message": "Device approved successfully",
	})
}

// handleOAuth2Deny handles POST /api/oauth/deny
// Denies a device authorization request
func (app *portalApp) handleOAuth2Deny(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Verify user is logged in
	cookie, err := r.Cookie("tenant_session")
	if err != nil || cookie.Value == "" {
		http.Error(w, `{"error": "unauthorized"}`, http.StatusUnauthorized)
		return
	}

	_, err = app.sessionStore.GetSession(cookie.Value)
	if err != nil {
		http.Error(w, `{"error": "unauthorized"}`, http.StatusUnauthorized)
		return
	}

	// Parse request
	if err := r.ParseForm(); err != nil {
		http.Error(w, `{"error": "invalid_request"}`, http.StatusBadRequest)
		return
	}

	userCode := r.FormValue("user_code")
	if userCode == "" {
		http.Error(w, `{"error": "missing_user_code"}`, http.StatusBadRequest)
		return
	}

	// Deny the request
	if err := app.oauth2Server.DenyDevice(userCode); err != nil {
		zlog.Error().Err(err).Str("user_code", userCode).Msg("handleOAuth2Deny: failed to deny request")
		http.Error(w, `{"error": "deny_failed"}`, http.StatusBadRequest)
		return
	}

	// Return success
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"success": true,
		"message": "Device denied",
	})
}

// handleOAuth2ConsentLogin handles POST /api/oauth/consent-login
// Accepts JSON body: {"auth_code", "email", "password"}
// Returns JSON: {"success": true} on success, {"success": false, "error": "..."} on failure.
// On success it sets the tenant_session cookie so the browser can call authorize-confirm.
func (app *portalApp) handleOAuth2ConsentLogin(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	w.Header().Set("Content-Type", "application/json")

	var req struct {
		AuthCode string `json:"auth_code"`
		Email    string `json:"email"`
		Password string `json:"password"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]interface{}{"success": false, "error": "invalid_request"})
		return
	}

	if req.AuthCode == "" || req.Email == "" || req.Password == "" {
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]interface{}{"success": false, "error": "missing_fields"})
		return
	}

	// Verify the authorization request exists and hasn't expired.
	// Keep the authReq so we can embed consent info in the login response below
	// (avoids a second round-trip that may hit a different instance).
	authReq, err := app.oauth2Server.GetAuthorizationRequest(req.AuthCode)
	if err != nil {
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]interface{}{"success": false, "error": "invalid_authorization_code"})
		return
	}

	// Validate credentials via gRPC
	tenantClient := app.services.TenantPortal
	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()

	loginResp, err := tenantClient.TenantLogin(ctx, &proto.TenantLoginRequest{
		Email:    req.Email,
		Password: req.Password,
	})
	if err != nil || !loginResp.GetSuccess() {
		msg := "invalid_credentials"
		if loginResp != nil && loginResp.GetMessage() != "" {
			msg = loginResp.GetMessage()
		}
		w.WriteHeader(http.StatusUnauthorized)
		json.NewEncoder(w).Encode(map[string]interface{}{"success": false, "error": msg})
		return
	}

	// Create a real portal session
	sess, err := app.sessionStore.CreateSession(
		loginResp.GetTenantId(),
		loginResp.GetFullName(),
		loginResp.GetEmail(),
		loginResp.GetTier(),
		loginResp.GetSessionToken(),
		false,
	)
	if err != nil {
		zlog.Error().Err(err).Msg("handleOAuth2ConsentLogin: failed to create session")
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(map[string]interface{}{"success": false, "error": "session_creation_failed"})
		return
	}

	// Authorize the OAuth2 request with real tenant identity (sets UserID so token exchange works)
	if err := app.oauth2Server.AuthorizeAuthorizationCode(
		req.AuthCode,
		loginResp.GetTenantId(),
		loginResp.GetEmail(),
		loginResp.GetFullName(),
		loginResp.GetTenantId(),
		loginResp.GetTier(),
	); err != nil {
		zlog.Error().Err(err).Msg("handleOAuth2ConsentLogin: failed to authorize")
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(map[string]interface{}{"success": false, "error": "authorization_failed"})
		return
	}

	// Set session cookie so the next authorize-confirm POST includes it
	consentMaxAge := int(time.Until(sess.ExpiresAt).Seconds())
	if consentMaxAge < 0 {
		consentMaxAge = 0
	}
	secure := isSecureRequest(r, app.isSecure)
	http.SetCookie(w, &http.Cookie{
		Name:     "tenant_session",
		Value:    sess.Token,
		Path:     "/",
		MaxAge:   consentMaxAge,
		HttpOnly: true,
		Secure:   secure,
		SameSite: http.SameSiteLaxMode,
	})

	// Return consent info inline — frontend uses this directly and never needs
	// a second GET /api/oauth/consent-info that could hit a different instance.
	json.NewEncoder(w).Encode(map[string]interface{}{
		"success":       true,
		"authenticated": true,
		"client_id":     authReq.ClientID,
		"scope":         authReq.Scope,
		"device_id":     authReq.DeviceID,
		"code":          authReq.AuthorizationCode,
		"expires_in":    int(authReq.ExpiresAt.Unix() - authReq.CreatedAt.Unix()),
		"user_email":    loginResp.GetEmail(),
	})
}

// handleOAuth2AuthorizeConfirm handles POST /api/oauth/authorize-confirm
// Accepts JSON body: {"auth_code", "action": "allow"|"deny"}
// Returns JSON: {"success": true, "action": "allow", "redirect_uri": "..."} so the SPA performs the redirect.
func (app *portalApp) handleOAuth2AuthorizeConfirm(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	w.Header().Set("Content-Type", "application/json")

	var req struct {
		AuthCode string `json:"auth_code"`
		Action   string `json:"action"` // "allow" | "deny"
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]interface{}{"success": false, "error": "invalid_request"})
		return
	}

	if req.AuthCode == "" {
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]interface{}{"success": false, "error": "missing_auth_code"})
		return
	}

	// CSRF Protection: Validate oauth2_state cookie matches stored state
	stateCookie, err := r.Cookie("oauth2_state")
	if err != nil {
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]interface{}{"success": false, "error": "csrf_missing"})
		return
	}

	// Get the authorization request
	authReq, err := app.oauth2Server.GetAuthorizationRequest(req.AuthCode)
	if err != nil {
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]interface{}{"success": false, "error": "invalid_authorization_code"})
		return
	}

	// Verify state (CSRF)
	if stateCookie.Value != authReq.State {
		zlog.Warn().Str("expected", authReq.State).Str("got", stateCookie.Value).Msg("CSRF state mismatch in authorize-confirm")
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]interface{}{"success": false, "error": "csrf_mismatch"})
		return
	}

	// Verify the user is logged in with a valid session
	sessionCookie, err := r.Cookie("tenant_session")
	if err != nil || sessionCookie.Value == "" {
		w.WriteHeader(http.StatusUnauthorized)
		json.NewEncoder(w).Encode(map[string]interface{}{"success": false, "error": "unauthenticated"})
		return
	}
	sess, err := app.sessionStore.GetSession(sessionCookie.Value)
	if err != nil || sess == nil {
		w.WriteHeader(http.StatusUnauthorized)
		json.NewEncoder(w).Encode(map[string]interface{}{"success": false, "error": "session_expired"})
		return
	}

	if req.Action == "deny" {
		app.oauth2Server.DenyAuthorizationCode(req.AuthCode)
		if app.redisClient != nil && authReq.DeviceID != "" {
			app.redisClient.Del(r.Context(), "oauth2:device_lock:"+authReq.DeviceID)
		}
		app.clearOAuth2Cookies(w, r)

		q := url.Values{}
		q.Set("error", "access_denied")
		q.Set("error_description", "user_denied")
		q.Set("state", authReq.State)
		json.NewEncoder(w).Encode(map[string]interface{}{
			"success":      true,
			"action":       "deny",
			"redirect_uri": authReq.RedirectURI + "?" + q.Encode(),
		})
		return
	}

	// Peer limit check: block OAuth2 device creation if user has reached plan limit
	if app.services != nil {
		tenantClient := app.services.TenantPortal
		statsResp, err := tenantClient.GetTenantAccount(r.Context(), &proto.GetTenantAccountRequest{TenantId: sess.TenantID})
		if err == nil && statsResp.MaxPeers > 0 && statsResp.PeerCount >= statsResp.MaxPeers {
			app.oauth2Server.DenyAuthorizationCode(req.AuthCode)
			if app.redisClient != nil && authReq.DeviceID != "" {
				app.redisClient.Del(r.Context(), "oauth2:device_lock:"+authReq.DeviceID)
			}
			app.clearOAuth2Cookies(w, r)
			q := url.Values{}
			q.Set("error", "access_denied")
			q.Set("error_description", "peer_limit_exceeded")
			q.Set("state", authReq.State)
			json.NewEncoder(w).Encode(map[string]interface{}{
				"success":      true,
				"action":       "deny",
				"redirect_uri": authReq.RedirectURI + "?" + q.Encode(),
			})
			return
		}
	}

	// Allow: authorize the code with the logged-in user (skip if already authorized via consent-login)
	if authReq.UserID == "" {
		if err := app.oauth2Server.AuthorizeAuthorizationCode(
			req.AuthCode,
			sess.Email, // use email as stable user identifier (no separate UserID in session)
			sess.Email,
			sess.FullName,
			sess.TenantID,
			sess.Tier,
		); err != nil {
			zlog.Error().Err(err).Msg("handleOAuth2AuthorizeConfirm: failed to authorize")
			w.WriteHeader(http.StatusInternalServerError)
			json.NewEncoder(w).Encode(map[string]interface{}{"success": false, "error": "authorization_failed"})
			return
		}
	}

	q := url.Values{}
	q.Set("code", req.AuthCode)
	q.Set("state", authReq.State)

	app.clearOAuth2Cookies(w, r)

	json.NewEncoder(w).Encode(map[string]interface{}{
		"success":      true,
		"action":       "allow",
		"redirect_uri": authReq.RedirectURI + "?" + q.Encode(),
	})
}

// clearOAuth2Cookies clears all OAuth2-related cookies
func (app *portalApp) clearOAuth2Cookies(w http.ResponseWriter, r *http.Request) {
	secure := isSecureRequest(r, app.isSecure)
	http.SetCookie(w, &http.Cookie{
		Name:     "oauth2_auth_code",
		Value:    "",
		Path:     "/",
		MaxAge:   -1,
		HttpOnly: true,
		Secure:   secure,
		SameSite: http.SameSiteLaxMode,
	})
	http.SetCookie(w, &http.Cookie{
		Name:     "oauth2_state",
		Value:    "",
		Path:     "/",
		MaxAge:   -1,
		HttpOnly: true,
		Secure:   secure,
		SameSite: http.SameSiteLaxMode,
	})
}

// handleOAuth2ConsentInfo handles GET /api/oauth/consent-info?code=AUTH_CODE
// Returns JSON describing the pending authorization request so the Svelte SPA can render the consent UI.
// Also includes the caller's session status so the SPA can decide login vs. consent screen without
// depending on the WebSocket-based authStore.
func (app *portalApp) handleOAuth2ConsentInfo(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	code := r.URL.Query().Get("code")
	if code == "" {
		http.Error(w, `{"error":"missing_code"}`, http.StatusBadRequest)
		return
	}

	authReq, err := app.oauth2Server.GetAuthorizationRequest(code)
	if err != nil {
		http.Error(w, `{"error":"invalid_or_expired"}`, http.StatusNotFound)
		return
	}

	// Check whether the caller already has a valid portal session (cookie-based).
	authenticated := false
	userEmail := ""
	if sessionCookie, err := r.Cookie("tenant_session"); err == nil && sessionCookie.Value != "" {
		if sess, err := app.sessionStore.GetSession(sessionCookie.Value); err == nil && sess != nil {
			authenticated = true
			userEmail = sess.Email
		}
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"client_id":     authReq.ClientID,
		"scope":         authReq.Scope,
		"device_id":     authReq.DeviceID,
		"code":          authReq.AuthorizationCode,
		"expires_in":    int(authReq.ExpiresAt.Unix() - authReq.CreatedAt.Unix()),
		"authenticated": authenticated,
		"user_email":    userEmail,
	})
}

// handleOAuth2PendingDevice handles GET /api/oauth/pending-device?user_code=USER_CODE
// Returns JSON about a pending device authorization request (for the Activate SPA page).
func (app *portalApp) handleOAuth2PendingDevice(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Require authenticated session — prevents enumeration of pending device codes
	cookie, err := r.Cookie("tenant_session")
	if err != nil || cookie.Value == "" {
		http.Error(w, `{"error":"unauthorized"}`, http.StatusUnauthorized)
		return
	}
	if sess, err := app.sessionStore.GetSession(cookie.Value); err != nil || sess == nil {
		http.Error(w, `{"error":"unauthorized"}`, http.StatusUnauthorized)
		return
	}

	userCode := strings.TrimSpace(r.URL.Query().Get("user_code"))
	if userCode == "" {
		http.Error(w, `{"error":"missing_user_code"}`, http.StatusBadRequest)
		return
	}

	req, err := app.oauth2Server.GetPendingRequest(userCode)
	if err != nil {
		http.Error(w, `{"error":"invalid_or_expired"}`, http.StatusNotFound)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"user_code":  req.UserCode,
		"expires_at": req.ExpiresAt.Unix(),
		"status":     req.Status,
	})
}

// handleSnapshotDownload serves a binary backup file from a device snapshot.
// Requires a valid tenant_session cookie. The snapshot must belong to the caller's account.
// GET /api/snapshot/download?id=<snapshot_id>
func (app *portalApp) handleSnapshotDownload(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Require session
	cookie, err := r.Cookie("tenant_session")
	if err != nil || cookie.Value == "" {
		http.Error(w, `{"error":"unauthorized"}`, http.StatusUnauthorized)
		return
	}
	sess, err := app.sessionStore.GetSession(cookie.Value)
	if err != nil || sess == nil {
		http.Error(w, `{"error":"unauthorized"}`, http.StatusUnauthorized)
		return
	}

	snapshotID := r.URL.Query().Get("id")
	if snapshotID == "" {
		http.Error(w, `{"error":"missing id"}`, http.StatusBadRequest)
		return
	}

	if app.services == nil || app.services.Auth == nil {
		http.Error(w, `{"error":"service unavailable"}`, http.StatusServiceUnavailable)
		return
	}

	// Call the core's gRPC to get the snapshot with backup data
	client := app.services.WUSP
	resp, err := client.GetSnapshotBackup(r.Context(), &proto.GetSnapshotBackupRequest{
		SnapshotId: snapshotID,
		AccountId:  sess.TenantID,
	})
	if err != nil || resp == nil || len(resp.BackupFile) == 0 {
		http.Error(w, `{"error":"not found or no backup attached"}`, http.StatusNotFound)
		return
	}

	filename := resp.BackupName
	if filename == "" {
		filename = "backup.rsc"
	}

	w.Header().Set("Content-Type", "application/octet-stream")
	w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="%s"`, filename))
	w.Header().Set("Content-Length", fmt.Sprintf("%d", len(resp.BackupFile)))
	w.Write(resp.BackupFile)
}

// renderOAuth2CodeEntry renders the code entry form
func (app *portalApp) renderOAuth2CodeEntry(w http.ResponseWriter) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(http.StatusOK)
	w.Write([]byte(`<!DOCTYPE html>
<html lang="en">
<head>
  <meta charset="UTF-8">
  <meta name="viewport" content="width=device-width, initial-scale=1.0">
  <title>Connect Device — Wantastic</title>
  <style>
    *, *::before, *::after { box-sizing: border-box; margin: 0; padding: 0; }
    body {
      font-family: -apple-system, BlinkMacSystemFont, "Segoe UI", Roboto, sans-serif;
      background: #0f1117;
      color: #e2e8f0;
      display: flex;
      align-items: center;
      justify-content: center;
      min-height: 100vh;
      padding: 1rem;
    }
    .card {
      background: #1a1d27;
      border: 1px solid #2d3748;
      border-radius: 12px;
      padding: 2.5rem 2rem;
      width: 100%%;
      max-width: 380px;
      text-align: center;
    }
    .logo { font-size: 1.5rem; font-weight: 700; color: #7c3aed; margin-bottom: 0.5rem; }
    h1 { font-size: 1.1rem; font-weight: 600; margin-bottom: 0.4rem; }
    p  { font-size: 0.875rem; color: #94a3b8; margin-bottom: 1.5rem; line-height: 1.5; }
    input {
      width: 100%%;
      padding: 0.75rem 1rem;
      border: 1px solid #374151;
      border-radius: 8px;
      background: #111827;
      color: #f1f5f9;
      font-size: 1rem;
      letter-spacing: 0.1em;
      text-align: center;
      text-transform: uppercase;
      margin-bottom: 1rem;
      outline: none;
    }
    input:focus { border-color: #7c3aed; }
    button {
      width: 100%%;
      padding: 0.75rem;
      background: #7c3aed;
      color: #fff;
      border: none;
      border-radius: 8px;
      font-size: 0.95rem;
      font-weight: 600;
      cursor: pointer;
    }
    button:hover { background: #6d28d9; }
    .hint { margin-top: 1rem; font-size: 0.8rem; color: #64748b; }
  </style>
</head>
<body>
  <div class="card">
    <div class="logo">Wantastic</div>
    <h1>Connect your device</h1>
    <p>Enter the code shown in the Wantastic agent on your device.</p>
    <form id="f">
      <input id="code" name="code" type="text"
             placeholder="XXXX-YYYY" autocomplete="off" autofocus
             pattern="[A-Z0-9]{4}-[A-Z0-9]{4}"
             title="8-character code in the format XXXX-YYYY" required>
      <button type="submit">Continue</button>
    </form>
    <p class="hint">The code is displayed in your Wantastic agent.</p>
  </div>
  <script>
    document.getElementById('f').addEventListener('submit', function(e) {
      e.preventDefault();
      var code = document.getElementById('code').value.trim().toUpperCase();
      if (!code) return;
      window.location.href = '/activate?user_code=' + encodeURIComponent(code);
    });
  </script>
</body>
</html>`))
}

// renderOAuth2Login renders the login form for unauthenticated users
func (app *portalApp) renderOAuth2Login(w http.ResponseWriter, req *oauth2.DeviceRequest, userCode string) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(http.StatusOK)
	w.Write([]byte(fmt.Sprintf(`<!DOCTYPE html>
<html lang="en">
<head>
  <meta charset="UTF-8">
  <meta name="viewport" content="width=device-width, initial-scale=1.0">
  <title>Sign In — Wantastic</title>
  <style>
    *, *::before, *::after { box-sizing: border-box; margin: 0; padding: 0; }
    body {
      font-family: -apple-system, BlinkMacSystemFont, "Segoe UI", Roboto, sans-serif;
      background: #0f1117;
      color: #e2e8f0;
      display: flex;
      align-items: center;
      justify-content: center;
      min-height: 100vh;
      padding: 1rem;
    }
    .card {
      background: #1a1d27;
      border: 1px solid #2d3748;
      border-radius: 12px;
      padding: 2.5rem 2rem;
      width: 100%%;
      max-width: 380px;
      text-align: center;
    }
    .logo { font-size: 1.5rem; font-weight: 700; color: #7c3aed; margin-bottom: 0.5rem; }
    h1 { font-size: 1.1rem; font-weight: 600; margin-bottom: 0.4rem; }
    .device-info {
      background: #111827;
      border: 1px solid #374151;
      border-radius: 8px;
      padding: 1rem;
      margin: 1rem 0;
      font-family: monospace;
      font-size: 1.25rem;
      color: #7c3aed;
      letter-spacing: 0.1em;
    }
    p  { font-size: 0.875rem; color: #94a3b8; margin-bottom: 1.5rem; line-height: 1.5; }
    .btn {
      display: inline-block;
      width: 100%%;
      padding: 0.75rem;
      background: #7c3aed;
      color: #fff;
      border: none;
      border-radius: 8px;
      font-size: 0.95rem;
      font-weight: 600;
      cursor: pointer;
      text-decoration: none;
      margin-top: 1rem;
    }
    .btn:hover { background: #6d28d9; }
    .btn-secondary {
      background: transparent;
      border: 1px solid #374151;
    }
    .btn-secondary:hover { background: #1f2937; }
  </style>
</head>
<body>
  <div class="card">
    <div class="logo">Wantastic</div>
    <h1>A device wants to connect</h1>
    <div class="device-info">%s</div>
    <p>To approve this device, please sign in to your Wantastic account.</p>
    <a href="/#login" onclick="sessionStorage.setItem('returnUrl','/?user_code=%s#activate')" class="btn">Sign In</a>
    <button class="btn btn-secondary" onclick="history.back()">Cancel</button>
  </div>
</body>
</html>`, userCode, url.QueryEscape(userCode))))
}

// renderOAuth2Approval renders the approval form for authenticated users
func (app *portalApp) renderOAuth2Approval(w http.ResponseWriter, req *oauth2.DeviceRequest, userCode string) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(http.StatusOK)
	w.Write([]byte(fmt.Sprintf(`<!DOCTYPE html>
<html lang="en">
<head>
  <meta charset="UTF-8">
  <meta name="viewport" content="width=device-width, initial-scale=1.0">
  <title>Approve Device — Wantastic</title>
  <style>
    *, *::before, *::after { box-sizing: border-box; margin: 0; padding: 0; }
    body {
      font-family: -apple-system, BlinkMacSystemFont, "Segoe UI", Roboto, sans-serif;
      background: #0f1117;
      color: #e2e8f0;
      display: flex;
      align-items: center;
      justify-content: center;
      min-height: 100vh;
      padding: 1rem;
    }
    .card {
      background: #1a1d27;
      border: 1px solid #2d3748;
      border-radius: 12px;
      padding: 2.5rem 2rem;
      width: 100%%;
      max-width: 380px;
      text-align: center;
    }
    .logo { font-size: 1.5rem; font-weight: 700; color: #7c3aed; margin-bottom: 0.5rem; }
    h1 { font-size: 1.1rem; font-weight: 600; margin-bottom: 0.4rem; }
    .device-info {
      background: #111827;
      border: 1px solid #374151;
      border-radius: 8px;
      padding: 1rem;
      margin: 1rem 0;
      font-family: monospace;
      font-size: 1.25rem;
      color: #7c3aed;
      letter-spacing: 0.1em;
    }
    p  { font-size: 0.875rem; color: #94a3b8; margin-bottom: 1.5rem; line-height: 1.5; }
    .warning {
      background: rgba(239, 68, 68, 0.1);
      border: 1px solid #ef4444;
      border-radius: 8px;
      padding: 0.75rem;
      margin: 1rem 0;
      font-size: 0.8rem;
      color: #fca5a5;
    }
    button {
      width: 100%%;
      padding: 0.75rem;
      border: none;
      border-radius: 8px;
      font-size: 0.95rem;
      font-weight: 600;
      cursor: pointer;
      margin-top: 0.5rem;
    }
    .btn-primary {
      background: #7c3aed;
      color: #fff;
    }
    .btn-primary:hover { background: #6d28d9; }
    .btn-secondary {
      background: transparent;
      border: 1px solid #374151;
      color: #94a3b8;
    }
    .btn-secondary:hover { background: #1f2937; }
    #result { margin-top: 1rem; font-size: 0.875rem; }
    #result.success { color: #4ade80; }
    #result.error { color: #ef4444; }
  </style>
</head>
<body>
  <div class="card">
    <div class="logo">Wantastic</div>
    <h1>Connect this device?</h1>
    <div class="device-info">%s</div>
    <p>A device is requesting access to your Wantastic network.</p>
    <div class="warning">Only approve devices you recognize.</div>
    <button class="btn-primary" onclick="approve()">Approve</button>
    <button class="btn-secondary" onclick="deny()">Deny</button>
    <div id="result"></div>
  </div>
  <script>
    function approve() {
      fetch('/api/oauth/approve', {
        method: 'POST',
        headers: {'Content-Type': 'application/x-www-form-urlencoded'},
        body: 'user_code=%s'
      })
      .then(r => r.json())
      .then(data => {
        const result = document.getElementById('result');
        if (data.success) {
          result.className = 'success';
          result.textContent = 'Device approved! You can close this window.';
        } else {
          result.className = 'error';
          result.textContent = data.error || 'Approval failed.';
        }
      })
      .catch(err => {
        document.getElementById('result').className = 'error';
        document.getElementById('result').textContent = 'Error: ' + err.message;
      });
    }
    function deny() {
      fetch('/api/oauth/deny', {
        method: 'POST',
        headers: {'Content-Type': 'application/x-www-form-urlencoded'},
        body: 'user_code=%s'
      })
      .then(r => r.json())
      .then(data => {
        const result = document.getElementById('result');
        if (data.success) {
          result.className = 'success';
          result.textContent = 'Device denied.';
        } else {
          result.className = 'error';
          result.textContent = data.error || 'Request failed.';
        }
      })
      .catch(err => {
        document.getElementById('result').className = 'error';
        document.getElementById('result').textContent = 'Error: ' + err.message;
      });
    }
  </script>
</body>
</html>`, userCode, userCode, userCode)))
}

// renderOAuth2Error renders an error page
func (app *portalApp) renderOAuth2Error(w http.ResponseWriter, message string) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(http.StatusBadRequest)
	w.Write([]byte(fmt.Sprintf(`<!DOCTYPE html>
<html lang="en">
<head>
  <meta charset="UTF-8">
  <meta name="viewport" content="width=device-width, initial-scale=1.0">
  <title>Error — Wantastic</title>
  <style>
    *, *::before, *::after { box-sizing: border-box; margin: 0; padding: 0; }
    body {
      font-family: -apple-system, BlinkMacSystemFont, "Segoe UI", Roboto, sans-serif;
      background: #0f1117;
      color: #e2e8f0;
      display: flex;
      align-items: center;
      justify-content: center;
      min-height: 100vh;
      padding: 1rem;
    }
    .card {
      background: #1a1d27;
      border: 1px solid #2d3748;
      border-radius: 12px;
      padding: 2.5rem 2rem;
      width: 100%%;
      max-width: 380px;
      text-align: center;
    }
    .logo { font-size: 1.5rem; font-weight: 700; color: #7c3aed; margin-bottom: 0.5rem; }
    h1 { font-size: 1.1rem; font-weight: 600; margin-bottom: 0.4rem; color: #ef4444; }
    p  { font-size: 0.875rem; color: #94a3b8; margin-bottom: 1.5rem; line-height: 1.5; }
    a {
      color: #7c3aed;
      text-decoration: none;
    }
    a:hover { text-decoration: underline; }
  </style>
</head>
<body>
  <div class="card">
    <div class="logo">Wantastic</div>
    <h1>Error</h1>
    <p>%s</p>
    <p><a href="/activate">Try again</a></p>
  </div>
</body>
</html>`, message)))
}
