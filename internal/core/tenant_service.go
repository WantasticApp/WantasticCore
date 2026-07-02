package core

import (
	"WantasticCore/internal/errs"
	"archive/zip"
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"math/big"
	"net"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"WantasticCore/internal/account"
	"WantasticCore/internal/auth"
	"WantasticCore/internal/config"
	"WantasticCore/internal/crypto"
	"WantasticCore/internal/email"
	rosapi "WantasticCore/internal/routerosapi"
	"WantasticCore/internal/server"
	"WantasticCore/internal/tenant"
	proto "WantasticCore/internal/types"
	webssh "WantasticCore/internal/webssh"
	"WantasticCore/internal/wg/userspace"

	"WantasticCore/internal/wg/userspace/wireguard-go/device"
	"WantasticCore/internal/wg/userspace/wireguard-go/wgctrl/wgtypes"

	"github.com/google/uuid"
	"github.com/rs/zerolog/log"
	"github.com/skip2/go-qrcode"
	"golang.org/x/crypto/bcrypt"
)

// =============================================================================
// Helper functions for port scan data conversion
// =============================================================================

// =============================================================================
// Tenant Registration Service
// =============================================================================

// TenantRegistrationServiceServer implements TenantRegistrationService.
// Registration flow has been removed; only admin-created tenants exist now.
// All RPCs return Unimplemented so the interface stays satisfied.
type TenantRegistrationServiceServer struct {
	UnimplementedTenantRegistrationService
	server         server.OverlayServer
	tenantRegistry tenant.Registry
	smtpClient     *email.SMTPService
}

// NewTenantRegistrationServiceServer creates a new TenantRegistrationServiceServer.
func NewTenantRegistrationServiceServer(
	srv *server.Server,
	tenantRegistry tenant.Registry,
	smtpClient *email.SMTPService,
) *TenantRegistrationServiceServer {
	return &TenantRegistrationServiceServer{
		server:         srv,
		tenantRegistry: tenantRegistry,
		smtpClient:     smtpClient,
	}
}

// ProcessStripeWebhook returns Unimplemented; billing/Stripe was removed in Phase 2.
func (s *TenantRegistrationServiceServer) ProcessStripeWebhook(ctx context.Context, req *proto.ProcessStripeWebhookRequest) (*proto.ProcessStripeWebhookResponse, error) {
	return nil, errs.UnimplementedE("billing removed")
}

// ProcessTwilioWebhook returns Unimplemented; Twilio/SMS was removed in Phase 3.
func (s *TenantRegistrationServiceServer) ProcessTwilioWebhook(ctx context.Context, req *proto.ProcessTwilioWebhookRequest) (*proto.ProcessTwilioWebhookResponse, error) {
	return nil, errs.UnimplementedE("sms/twilio removed")
}

// GetPaymentStatus returns Unimplemented; billing was removed in Phase 2.
func (s *TenantRegistrationServiceServer) GetPaymentStatus(ctx context.Context, req *proto.GetPaymentStatusRequest) (*proto.GetPaymentStatusResponse, error) {
	return nil, errs.UnimplementedE("billing removed")
}

// GetAllowedPhoneRegions returns Unimplemented; registration removed in Phase 3.
func (s *TenantRegistrationServiceServer) GetAllowedPhoneRegions(ctx context.Context, req *proto.GetAllowedPhoneRegionsRequest) (*proto.GetAllowedPhoneRegionsResponse, error) {
	return nil, errs.UnimplementedE("registration removed")
}

// GetAvailablePlans returns Unimplemented; billing was removed in Phase 2.
func (s *TenantRegistrationServiceServer) GetAvailablePlans(ctx context.Context, req *proto.GetAvailablePlansRequest) (*proto.GetAvailablePlansResponse, error) {
	return nil, errs.UnimplementedE("billing removed")
}

// VerifyCaptcha returns Unimplemented; CAPTCHA was removed in Phase 3.
func (s *TenantRegistrationServiceServer) VerifyCaptcha(ctx context.Context, req *proto.CaptchaVerifyRequest) (*proto.CaptchaVerifyResponse, error) {
	return nil, errs.UnimplementedE("registration removed")
}

// StartRegistration returns Unimplemented; registration removed in Phase 3.
func (s *TenantRegistrationServiceServer) StartRegistration(ctx context.Context, req *proto.StartRegistrationRequest) (*proto.StartRegistrationResponse, error) {
	return nil, errs.UnimplementedE("registration removed")
}

// VerifyPhone returns Unimplemented; registration removed in Phase 3.
func (s *TenantRegistrationServiceServer) VerifyPhone(ctx context.Context, req *proto.VerifyPhoneRequest) (*proto.VerifyPhoneResponse, error) {
	return nil, errs.UnimplementedE("registration removed")
}

// CompleteRegistration returns Unimplemented; registration removed in Phase 3.
func (s *TenantRegistrationServiceServer) CompleteRegistration(ctx context.Context, req *proto.CompleteRegistrationRequest) (*proto.CompleteRegistrationResponse, error) {
	return nil, errs.UnimplementedE("registration removed")
}

// CreateCheckoutSession returns Unimplemented; billing was removed in Phase 2.
func (s *TenantRegistrationServiceServer) CreateCheckoutSession(ctx context.Context, req *proto.CreateCheckoutSessionRequest) (*proto.CreateCheckoutSessionResponse, error) {
	return nil, errs.UnimplementedE("billing removed")
}

// CreateSetupIntent returns Unimplemented; billing was removed in Phase 2.
func (s *TenantRegistrationServiceServer) CreateSetupIntent(ctx context.Context, req *proto.CreateSetupIntentRequest) (*proto.CreateSetupIntentResponse, error) {
	return nil, errs.UnimplementedE("billing removed")
}

// GetRegistrationStatus returns Unimplemented; registration removed in Phase 3.
func (s *TenantRegistrationServiceServer) GetRegistrationStatus(ctx context.Context, req *proto.GetRegistrationStatusRequest) (*proto.GetRegistrationStatusResponse, error) {
	return nil, errs.UnimplementedE("registration removed")
}

// ResendPhoneVerification returns Unimplemented; registration removed in Phase 3.
func (s *TenantRegistrationServiceServer) ResendPhoneVerification(ctx context.Context, req *proto.ResendPhoneVerificationRequest) (*proto.ResendPhoneVerificationResponse, error) {
	return nil, errs.UnimplementedE("registration removed")
}

// =============================================================================
// Tenant Portal Service (Login & Dashboard)
// =============================================================================

// pendingPasswordReset tracks password reset state with security controls.
// Phase 3: phone-based reset removed; verification now uses email codes only,
// but the field names "SMS*" are kept for compatibility with the existing flow.
type pendingPasswordReset struct {
	TenantID     string    // Tenant being reset (not exposed to client)
	Email        string    // Email address
	TokenHash    string    // SHA-256 hash of the token (never store plaintext)
	SMSCode      string    // 6-digit code (hashed)
	SMSCodeHash  string    // SHA-256 of the code
	SMSVerified  bool      // True if code verified
	CreatedAt    time.Time // When reset was requested
	ExpiresAt    time.Time // Token expiry (1 hour)
	SMSExpiresAt time.Time // Code expiry (5 minutes)
	AttemptCount int       // Failed verification attempts
	MaxAttempts  int       // Max allowed attempts (5)
	IPAddress    string    // Requesting IP (for rate limiting)
}

// deviceManagerAdapter adapts UserspaceManager
type deviceManagerAdapter struct {
	mgr *userspace.UserspaceManager
}

// tenantDeviceAdapter adapts TenantDevice  interface
type tenantDeviceAdapter struct {
	dev *userspace.TenantDevice
}

func (a *tenantDeviceAdapter) DialContext(ctx context.Context, network, address string) (net.Conn, error) {
	return a.dev.Net.DialContext(ctx, network, address)
}

// TenantPortalServiceServer implements TenantPortalService.
type TenantPortalServiceServer struct {
	UnimplementedTenantPortalService
	server              ServerBackend
	tenantRegistry      tenant.Registry
	smtpClient          *email.SMTPService            // Email service (password reset, notifications)
	config              *config.Config                // Application config for endpoints
	notificationManager *tenant.NotificationManager   // Per-tenant offline notification workers
	enrollmentCipher    *crypto.EnrollmentTokenCipher // Cipher for enrollment tokens

	// Password reset tracking (in-memory, should be moved to LMDB in production)
	pendingResets    map[string]*pendingPasswordReset // key: token hash prefix
	resetRateLimiter map[string][]time.Time           // key: email or IP, value: request times

	// Login failure tracking (in-memory, should be moved to LMDB in production)
	loginFailures map[string][]time.Time // key: email or IP, value: failed attempt times

	// Password reset "not found" tracking (when email doesn't exist)
	resetNotFoundFailures map[string][]time.Time // key: email, value: failed attempt times

	mu sync.RWMutex
}

// serverPeerCreator adapts server.Server
type serverPeerCreator struct {
	server *server.Server
}

// NewTenantPortalServiceServer creates a new TenantPortalServiceServer
func NewTenantPortalServiceServer(
	srv ServerBackend,
	tenantRegistry tenant.Registry,
	smtpClient *email.SMTPService,
	cfg *config.Config,
	notificationManager *tenant.NotificationManager,
	enrollmentCipher *crypto.EnrollmentTokenCipher,
) *TenantPortalServiceServer {
	return &TenantPortalServiceServer{
		server:                srv,
		tenantRegistry:        tenantRegistry,
		smtpClient:            smtpClient,
		config:                cfg,
		notificationManager:   notificationManager,
		enrollmentCipher:      enrollmentCipher,
		pendingResets:         make(map[string]*pendingPasswordReset),
		resetRateLimiter:      make(map[string][]time.Time),
		loginFailures:         make(map[string][]time.Time),
		resetNotFoundFailures: make(map[string][]time.Time),
	}
}

// ConfirmDevice is no longer used — device authorization is handled entirely by
// Auth0 via the OAuth2 Device Authorization Grant (RFC 8628).
// Agents should use the /device-login endpoint and the Auth0 activation URL.
// wireguardEndpoint returns the hostname/IP that should appear in generated
// peer configs' Endpoint = … line. Prefers the explicit
// Endpoints.WireguardServer override, falls back to Network.ServerEndpoint
// (the value the wizard always writes).
func (s *TenantPortalServiceServer) wireguardEndpoint() string {
	if s.config == nil {
		return ""
	}
	if v := strings.TrimSpace(s.config.Endpoints.WireguardServer); v != "" {
		return v
	}
	return strings.TrimSpace(s.config.Network.ServerEndpoint)
}

func (s *TenantPortalServiceServer) ConfirmDevice(_ context.Context, _ *proto.ConfirmDeviceRequest) (*proto.ConfirmDeviceResponse, error) {
	return nil, errs.UnimplementedE("device confirmation is handled by Auth0 — use the OAuth2 device flow")
}

// getOverlayAccountID resolves a tenant ID to their overlay account ID.
// This is needed because server operations (AddPeer, ListPeers, etc.) use the
// WireGuard account ID, not the tenant ID.

// It also ensures the overlay account exists in the admin DB, recreating it if necessary.
func (s *TenantPortalServiceServer) getOverlayAccountID(tenantID string) (string, error) {
	t, err := s.tenantRegistry.GetTenant(tenantID)
	if err != nil {
		return "", fmt.Errorf("tenant not found: %w", err)
	}
	if t.OverlayAccountID == "" {
		return "", fmt.Errorf("tenant has no overlay account")
	}

	// Verify the overlay account exists in admin DB, recreate if missing
	overlayAccountID, err := s.ensureOverlayAccount(t)
	if err != nil {
		return "", fmt.Errorf("failed to ensure overlay account: %w", err)
	}

	return overlayAccountID, nil
}

// resolveAccountForPeer finds the overlay account ID and peer metadata for a given peerID.
// It first tries the account mapped to tenantID. If the peer is not found there (which
// happens when a sharee requests a peer that lives in a shared account), it falls back
// to searching all CallerContext scopes so operations on shared peers work without
// the frontend needing to know which tenant owns each peer.
func (s *TenantPortalServiceServer) resolveAccountForPeer(ctx context.Context, tenantID, peerID string) (resolvedCtx context.Context, accountID string, peer *server.PeerMetadata, usedScope *auth.AccessScope, err error) {
	ctx = s.withResolvedCallerContext(ctx, tenantID)
	resolvedCtx = ctx

	// Primary lookup: the tenant ID carried in the request.
	if aid, aerr := s.getOverlayAccountID(tenantID); aerr == nil {
		if p, perr := s.server.GetPeer(aid, peerID); perr == nil && p.AccountID == aid {
			if cc := auth.CallerContextFromContext(ctx); cc != nil {
				if sc := cc.ScopeForAccount(aid); sc != nil {
					return resolvedCtx, aid, p, sc, nil
				}
			}
			return resolvedCtx, aid, p, nil, nil
		}
		log.Debug().
			Str("tenant_id", tenantID).
			Str("account_id", aid).
			Str("peer_id", peerID).
			Msg("resolveAccountForPeer: primary lookup missed peer, trying shared scopes")
	}

	// Fallback: search CallerContext scopes (handles aggregate-view cross-account ops).
	if cc := auth.CallerContextFromContext(ctx); cc == nil || !cc.ScopesHydrated {
		ctx = s.withHydratedCallerContext(ctx, tenantID)
		resolvedCtx = ctx
	}
	cc := auth.CallerContextFromContext(ctx)
	if cc == nil {
		log.Debug().
			Str("tenant_id", tenantID).
			Str("peer_id", peerID).
			Bool("has_caller_context", false).
			Msg("resolveAccountForPeer: no CallerContext for shared-scope fallback")
		return resolvedCtx, "", nil, nil, fmt.Errorf("peer not found")
	}
	for _, sc := range cc.Scopes {
		if sc.AccountID == "" {
			continue
		}
		p, perr := s.server.GetPeer(sc.AccountID, peerID)
		if perr != nil || p.AccountID != sc.AccountID {
			continue
		}
		log.Debug().Str("peer_id", peerID).Str("found_account", sc.AccountID).
			Bool("is_owner", sc.IsOwner).Msg("resolveAccountForPeer: found peer via CallerContext scope")
		return resolvedCtx, sc.AccountID, p, sc, nil
	}

	// Final fallback: resolve the peer globally from DB/cache, then verify the
	// caller actually has a scope for the discovered owner account.
	globalPeer, gerr := s.server.FindPeer(peerID)
	if gerr != nil || globalPeer == nil || globalPeer.AccountID == "" {
		if gerr != nil {
			log.Debug().Err(gerr).Str("peer_id", peerID).Msg("resolveAccountForPeer: global lookup failed")
		}
		return resolvedCtx, "", nil, nil, fmt.Errorf("peer not found in any accessible account")
	}

	if sc := cc.ScopeForAccount(globalPeer.AccountID); sc != nil {
		log.Debug().
			Str("peer_id", peerID).
			Str("found_account", globalPeer.AccountID).
			Bool("is_owner", sc.IsOwner).
			Msg("resolveAccountForPeer: found peer via global lookup")
		return resolvedCtx, globalPeer.AccountID, globalPeer, sc, nil
	}

	log.Warn().
		Str("peer_id", peerID).
		Str("found_account", globalPeer.AccountID).
		Str("caller_tenant_id", cc.TenantID).
		Msg("resolveAccountForPeer: peer exists but is outside caller scopes")
	return resolvedCtx, "", nil, nil, fmt.Errorf("peer not found in any accessible account")
}

func peerMatchesIP(peer *server.PeerMetadata, peerIP string) bool {
	if peer == nil || peerIP == "" {
		return false
	}
	assignedIP := strings.TrimSuffix(peer.AssignedIP, "/32")
	return peer.AssignedIP == peerIP || assignedIP == peerIP
}

// resolvePeerForAccess finds a peer in the caller's accessible scopes using the
// most precise key available. Prefer peerID, but fall back to peerIP for older
// clients that have not yet been regenerated with peer_id on the request.
func (s *TenantPortalServiceServer) resolvePeerForAccess(
	ctx context.Context,
	tenantID string,
	peerID string,
	peerIP string,
) (resolvedCtx context.Context, accountID string, peer *server.PeerMetadata, usedScope *auth.AccessScope, err error) {
	if peerID != "" {
		return s.resolveAccountForPeer(ctx, tenantID, peerID)
	}
	if peerIP == "" {
		return ctx, "", nil, nil, fmt.Errorf("peer_id or peer_ip required")
	}
	ctx = s.withResolvedCallerContext(ctx, tenantID)
	resolvedCtx = ctx
	if cc := auth.CallerContextFromContext(ctx); cc != nil && !cc.ScopesHydrated {
		ctx = s.withHydratedCallerContext(ctx, tenantID)
		resolvedCtx = ctx
	}

	type candidate struct {
		accountID string
		peer      *server.PeerMetadata
		scope     *auth.AccessScope
	}
	var matches []candidate
	addMatches := func(scope *auth.AccessScope) {
		if scope == nil || scope.AccountID == "" {
			return
		}
		peers, lerr := s.server.ListPeers(scope.AccountID)
		if lerr != nil {
			return
		}
		for _, p := range peers {
			if peerMatchesIP(p, peerIP) {
				matches = append(matches, candidate{
					accountID: scope.AccountID,
					peer:      p,
					scope:     scope,
				})
			}
		}
	}

	if cc := auth.CallerContextFromContext(ctx); cc != nil {
		if primary := cc.ScopeFor(tenantID); primary != nil {
			addMatches(primary)
		}
		for _, sc := range cc.Scopes {
			if sc == nil || sc.TenantID == tenantID {
				continue
			}
			addMatches(sc)
		}
	} else if aid, aerr := s.getOverlayAccountID(tenantID); aerr == nil {
		peers, lerr := s.server.ListPeers(aid)
		if lerr == nil {
			for _, p := range peers {
				if peerMatchesIP(p, peerIP) {
					matches = append(matches, candidate{
						accountID: aid,
						peer:      p,
					})
				}
			}
		}
	}

	if len(matches) == 0 {
		return resolvedCtx, "", nil, nil, fmt.Errorf("peer not found")
	}
	if len(matches) > 1 {
		return resolvedCtx, "", nil, nil, fmt.Errorf("peer_ip matched multiple accessible peers; send peer_id")
	}
	return resolvedCtx, matches[0].accountID, matches[0].peer, matches[0].scope, nil
}

// ensureOverlayAccount verifies the overlay account exists and recreates it if missing.
// This handles the case where the admin DB was reset but tenant registry still has the mapping.
func (s *TenantPortalServiceServer) ensureOverlayAccount(t *tenant.Tenant) (string, error) {
	if t.OverlayAccountID == "" {
		return "", fmt.Errorf("tenant has no overlay account ID")
	}

	// Check if the overlay account exists in admin DB
	_, err := s.server.GetAccount(t.OverlayAccountID)
	if err == nil {
		// Account exists, return the ID
		return t.OverlayAccountID, nil
	}

	// Account doesn't exist - need to recreate it
	log.Warn().
		Str("tenant_id", t.ID).
		Str("old_overlay_account_id", t.OverlayAccountID).
		Msg(" Overlay account missing from admin DB - recreating...")

	// Recreate overlay account with default peer cap (Phase 2: billing removed).
	newAccount, err := s.server.CreateAccount(fmt.Sprintf("tenant-%s", t.ID[:8]), account.DefaultMaxPeers)
	if err != nil {
		log.Error().
			Err(err).
			Str("tenant_id", t.ID).
			Msg("❌ Failed to recreate overlay account")
		return "", fmt.Errorf("failed to recreate overlay account: %w", err)
	}

	// Update tenant registry with new overlay account ID
	t.OverlayAccountID = newAccount.ID
	t.Networks = newAccount.Networks
	if err := s.tenantRegistry.UpdateTenant(t); err != nil {
		log.Error().
			Err(err).
			Str("tenant_id", t.ID).
			Str("new_overlay_account_id", newAccount.ID).
			Msg("❌ Failed to update tenant with new overlay account")
		// Don't fail - the account was created, just logging issue
	}

	log.Debug().
		Str("tenant_id", t.ID).
		Str("new_overlay_account_id", newAccount.ID).
		Strs("networks", newAccount.Networks).
		Msg(" Overlay account recreated successfully - peers will need to be re-added")

	return newAccount.ID, nil
}

// VerifyCaptcha returns Unimplemented; CAPTCHA was removed in Phase 3.
func (s *TenantPortalServiceServer) VerifyCaptcha(ctx context.Context, req *proto.CaptchaVerifyRequest) (*proto.CaptchaVerifyResponse, error) {
	return nil, errs.UnimplementedE("captcha removed")
}

// TenantLogin handles tenant login. CAPTCHA was removed in Phase 3; SMS 2FA was
// also removed. Supported 2FA methods are now: TOTP, email-code, and WhatsApp.
func (s *TenantPortalServiceServer) TenantLogin(ctx context.Context, req *proto.TenantLoginRequest) (*proto.TenantLoginResponse, error) {
	if req.Email == "" {
		return nil, errs.InvalidArgumentE(ErrEmailRequired)
	}
	if req.Password == "" {
		return nil, errs.InvalidArgumentE(ErrPasswordTooShort)
	}

	email := strings.ToLower(strings.TrimSpace(req.Email))

	// Extract IP and user agent from CallContext (populated upstream by
	// the portal's WebSocket dispatcher).
	var ipAddress, userAgent string
	if cc := auth.CallContextFrom(ctx); cc != nil {
		ipAddress = strings.TrimSpace(cc.OriginIP)
		userAgent = strings.TrimSpace(cc.OriginUserAgent)
	}

	// Rate limiting check for login failures
	s.mu.Lock()
	if !s.checkLoginRateLimit(email) {
		s.mu.Unlock()
		log.Warn().Str("email", email).Msg("🚫 Login rate limit exceeded for email")
		return &proto.TenantLoginResponse{
			Success:   false,
			Message:   ErrRateLimitExceeded,
			ErrorCode: ErrRateLimitExceeded,
		}, nil
	}
	s.mu.Unlock()

	// Create device hash from fingerprint or IP + User-Agent
	var deviceHash string
	if req.Fingerprint != "" {
		h := sha256.Sum256([]byte(req.Fingerprint))
		deviceHash = hex.EncodeToString(h[:16])
	} else if ipAddress != "" || userAgent != "" {
		h := sha256.Sum256([]byte(ipAddress + "|" + userAgent))
		deviceHash = hex.EncodeToString(h[:16])
	}

	// Get tenant from registry
	t, err := s.tenantRegistry.GetTenantByEmail(email)
	if err != nil {
		s.mu.Lock()
		s.recordLoginFailure(email)
		s.mu.Unlock()
		log.Warn().Str("email", email).Msg("Login failed: tenant not found")
		return &proto.TenantLoginResponse{
			Success:   false,
			Message:   ErrInvalidCredentials,
			ErrorCode: ErrInvalidCredentials,
		}, nil
	}

	// Block deleted / suspended accounts
	if t.Status == "deleted" {
		s.mu.Lock()
		s.recordLoginFailure(email)
		s.mu.Unlock()
		return &proto.TenantLoginResponse{
			Success:   false,
			Message:   ErrAccountDeleted,
			ErrorCode: ErrAccountDeleted,
		}, nil
	}
	if t.Status == "suspended" {
		s.mu.Lock()
		s.recordLoginFailure(email)
		s.mu.Unlock()
		return &proto.TenantLoginResponse{
			Success:   false,
			Message:   ErrAccountSuspended,
			ErrorCode: ErrAccountSuspended,
		}, nil
	}

	// Auto-fix legacy account statuses
	if t.Status == "cancelled" || t.Status == "" || t.Status == "pending" {
		log.Debug().
			Str("tenant_id", t.ID).
			Str("old_status", t.Status).
			Msg(" Auto-fixing legacy account status to 'active'")
		t.Status = "active"
		if err = s.tenantRegistry.UpdateTenant(t); err != nil {
			log.Error().Err(err).Str("tenant_id", t.ID).Msg("Failed to update tenant status")
		}
	}

	// Verify password
	if err = bcrypt.CompareHashAndPassword([]byte(t.PasswordHash), []byte(req.Password)); err != nil {
		s.mu.Lock()
		s.recordLoginFailure(email)
		s.mu.Unlock()
		log.Warn().Str("email", email).Msg("Login failed: invalid password")
		return &proto.TenantLoginResponse{
			Success:   false,
			Message:   ErrInvalidCredentials,
			ErrorCode: ErrInvalidCredentials,
		}, nil
	}

	// Trusted device skips 2FA
	isTrustedDevice := s.tenantRegistry.HasTrustedDevice(t.ID, deviceHash)
	if isTrustedDevice {
		log.Debug().
			Str("tenant_id", t.ID).
			Str("device_hash", deviceHash[:min(8, len(deviceHash))]+"...").
			Msg("🔓 Trusted device detected, skipping 2FA")
	}

	twoFAMethod, _ := s.tenantRegistry.GetActiveTwoFAMethod(t.ID)

	if !isTrustedDevice {
		switch twoFAMethod {
		case tenant.TwoFATOTP:
			if req.TotpCode == "" {
				return &proto.TenantLoginResponse{
					Success:      false,
					RequiresTotp: true,
					TwoFaMethod:  "totp",
					Message:      "Enter the code from your authenticator app",
				}, nil
			}
			if !auth.ValidateTOTP(t.TOTPSecret, req.TotpCode) {
				return &proto.TenantLoginResponse{
					Success:     false,
					TwoFaMethod: "totp",
					Message:     "Invalid authenticator code",
				}, nil
			}

		case "email":
			if req.TotpCode == "" {
				codeInt, cerr := rand.Int(rand.Reader, big.NewInt(1000000))
				if cerr != nil {
					return nil, errs.Internalf("failed to generate code: %v", cerr)
				}
				code := fmt.Sprintf("%06d", codeInt.Int64())
				expiresIn := 5 * time.Minute

				if err = s.tenantRegistry.SetPending2FACode(t.ID, code, expiresIn); err != nil {
					log.Error().Err(err).Str("tenant_id", t.ID).Msg("Failed to store 2FA code")
					return nil, errs.Internalf("failed to store 2FA code: %v", err)
				}

				if s.smtpClient != nil && s.smtpClient.IsConfigured() {
					if err = s.smtpClient.SendLoginCode(t.Email, code); err != nil {
						log.Error().Err(err).Str("tenant_id", t.ID).Str("email", t.Email).Msg("Failed to send 2FA email")
						return &proto.TenantLoginResponse{
							Success: false,
							Message: "Failed to send verification code. Please try again.",
						}, nil
					}
					log.Debug().Str("tenant_id", t.ID).Str("email", t.Email).Msg("📧 2FA code sent via email")
				} else {
					log.Warn().Msg("Email client not configured, cannot send 2FA code")
					return &proto.TenantLoginResponse{
						Success: false,
						Message: "Email service not configured",
					}, nil
				}

				return &proto.TenantLoginResponse{
					Success:      false,
					RequiresTotp: true,
					TwoFaMethod:  "email",
					Message:      "Enter the code sent to your email",
				}, nil
			}
			valid, expired, maxAttempts, vErr := s.tenantRegistry.Verify2FACode(t.ID, req.TotpCode)
			if vErr != nil {
				return nil, errs.Internalf("failed to verify 2FA code: %v", vErr)
			}
			if expired {
				return &proto.TenantLoginResponse{Success: false, TwoFaMethod: "email", Message: "Code expired. Please request a new code."}, nil
			}
			if maxAttempts {
				return &proto.TenantLoginResponse{Success: false, TwoFaMethod: "email", Message: "Too many attempts. Please request a new code."}, nil
			}
			if !valid {
				return &proto.TenantLoginResponse{Success: false, TwoFaMethod: "email", Message: "Invalid code"}, nil
			}

		case tenant.TwoFAWhatsApp:
			if req.TotpCode == "" {
				return &proto.TenantLoginResponse{
					Success:      false,
					RequiresTotp: true,
					TwoFaMethod:  "whatsapp",
					Message:      "Enter the code sent to your WhatsApp",
				}, nil
			}
			valid, expired, maxAttempts, vErr := s.tenantRegistry.Verify2FACode(t.ID, req.TotpCode)
			if vErr != nil {
				return nil, errs.Internalf("failed to verify 2FA code: %v", vErr)
			}
			if expired {
				return &proto.TenantLoginResponse{Success: false, TwoFaMethod: "whatsapp", Message: "Code expired. Please request a new code."}, nil
			}
			if maxAttempts {
				return &proto.TenantLoginResponse{Success: false, TwoFaMethod: "whatsapp", Message: "Too many attempts. Please request a new code."}, nil
			}
			if !valid {
				return &proto.TenantLoginResponse{Success: false, TwoFaMethod: "whatsapp", Message: "Invalid code"}, nil
			}
		}
	}

	// Detect first login
	isFirstLogin := t.LastLogin.IsZero()

	s.tenantRegistry.UpdateLastLogin(t.ID)

	sessionToken := uuid.New().String()
	sessionDuration := 8 * time.Hour
	trustedDevice := false
	if req.RememberMe {
		sessionDuration = 30 * 24 * time.Hour
		trustedDevice = true
	}

	if err = s.tenantRegistry.CreateSession(t.ID, sessionToken, ipAddress, userAgent, deviceHash, sessionDuration, trustedDevice); err != nil {
		log.Error().Err(err).Str("tenant_id", t.ID).Msg("Failed to create session")
		return nil, errs.Internalf("failed to create session: %v", err)
	}

	s.mu.Lock()
	s.clearLoginFailures(email)
	s.mu.Unlock()

	log.Debug().
		Str("tenant_id", t.ID).
		Str("email", req.Email).
		Str("session_token_preview", sessionToken[:min(16, len(sessionToken))]+"...").
		Bool("remember_me", req.RememberMe).
		Bool("trusted_device", trustedDevice).
		Msg(" Tenant logged in, session created")

	return &proto.TenantLoginResponse{
		Success:      true,
		SessionToken: sessionToken,
		TenantId:     t.ID,
		Email:        t.Email,
		Message:      "Login successful",
		FullName:     t.FullName,
		IsFirstLogin: isFirstLogin,
	}, nil
}

// TenantLogout invalidates a tenant session
func (s *TenantPortalServiceServer) TenantLogout(ctx context.Context, req *proto.TenantLogoutRequest) (*proto.TenantLogoutResponse, error) {
	if req.SessionToken == "" {
		return &proto.TenantLogoutResponse{
			Success: false,
			Message: "session_token required",
		}, nil
	}

	// Delete session from registry
	if err := s.tenantRegistry.DeleteSession(req.SessionToken); err != nil {
		log.Warn().Err(err).Str("session_token_preview", req.SessionToken[:min(16, len(req.SessionToken))]+"...").Msg("Failed to delete session (may already be expired)")
	} else {
		log.Debug().Str("session_token_preview", req.SessionToken[:min(16, len(req.SessionToken))]+"...").Msg("🔓 Tenant session deleted from gRPC server")
	}

	return &proto.TenantLogoutResponse{
		Success: true,
		Message: "Logged out successfully",
	}, nil
}

// GetTenantDashboard returns dashboard data
func (s *TenantPortalServiceServer) GetTenantDashboard(ctx context.Context, req *proto.GetTenantDashboardRequest) (*proto.GetTenantDashboardResponse, error) {
	if req.TenantId == "" {
		return nil, errs.InvalidArgumentE("tenant_id required")
	}

	ctx, overlayAccountID, usedScope, err := s.resolveTenantAccessAccount(ctx, req.TenantId, true)
	if err != nil {
		return nil, err
	}
	if err := requireScopeRead(usedScope, "dashboard"); err != nil {
		return nil, err
	}

	resourceTenantID := req.TenantId
	if usedScope != nil {
		resourceTenantID = usedScope.TenantID
	}

	// Get tenant from registry
	t, err := s.tenantRegistry.GetTenant(resourceTenantID)
	if err != nil {
		return nil, errs.NotFoundE("tenant not found")
	}

	// Get overlay account stats from server
	var peerCount, maxPeers, onlinePeers, blockCount int32
	var rxBytes, txBytes int64
	var totalIPs, ipsUsed int32
	var networkBlocks []string
	var goroutineCount int32
	var cpuUsage float64
	var memoryBytes int64

	if overlayAccountID != "" {
		acc, err := s.server.GetAccount(overlayAccountID)
		if err == nil && acc != nil {
			blockCount = int32(len(acc.Networks))
			networkBlocks = acc.Networks // Store the actual CIDR blocks

			// Calculate total IPs: each /27 block has 32 IPs, minus network/broadcast = 30 usable
			// But typically we use 29 to leave room for server IP.
			totalIPs = int32(len(acc.Networks) * 29)
			maxPeers = totalIPs

			// The account service is authoritative for the effective peer limit
			// because it already applies admin overrides before plan defaults.
			if currentPeers, effectiveMaxPeers, err := s.server.GetAccountPeerStats(overlayAccountID); err == nil {
				peerCount = int32(currentPeers)
				maxPeers = int32(effectiveMaxPeers)
			}

			// Get peer count and stats from server
			peers, _ := s.server.ListPeers(overlayAccountID)
			if peerCount == 0 {
				peerCount = int32(len(peers))
			}
			ipsUsed = peerCount // Each peer uses one IP

			// Get online peers and bandwidth stats from peers (already updated with WG device stats)
			for _, p := range peers {
				if p.IsOnline {
					onlinePeers++
				}
				rxBytes += p.RxBytes
				txBytes += p.TxBytes
			}

			// Get system metrics (runtime stats)
			// Note: These are global Go runtime stats, not per-device
			// For more accurate per-device tracking, we'd need to implement custom metrics
			goroutineCount = int32(runtime.NumGoroutine())

			// Get memory stats
			var m runtime.MemStats
			runtime.ReadMemStats(&m)
			memoryBytes = int64(m.Alloc) // Current allocated memory in bytes

			// CPU usage requires sampling over time
			// TODO: Implement proper per-device CPU monitoring
			cpuUsage = 0.0
		}
	}

	// Billing/tier reconciliation removed in Phase 2.
	tier := proto.AccountTier_TIER_FREE
	subscriptionStatus := "active"
	var nextBillingDate *proto.Timestamp

	if maxPeers <= 0 {
		maxPeers = 3
	}

	return &proto.GetTenantDashboardResponse{
		TenantId:           resourceTenantID,
		Name:               t.FullName,
		Tier:               tier,
		Status:             t.Status,
		PeerCount:          peerCount,
		MaxPeers:           maxPeers,
		BlockCount:         blockCount,
		RxBytes:            rxBytes,
		TxBytes:            txBytes,
		OnlinePeers:        onlinePeers,
		SubscriptionStatus: subscriptionStatus,
		NextBillingDate:    nextBillingDate,
		IsFreeTier:         true,
		TotalIpsAvailable:  totalIPs,
		IpsUsed:            ipsUsed,
		NetworkBlocks:      networkBlocks,
		GoroutineCount:     goroutineCount,
		CpuUsagePercent:    cpuUsage,
		MemoryBytes:        memoryBytes,
	}, nil
}

// GetTenantAccount returns the tenant's account information
func (s *TenantPortalServiceServer) GetTenantAccount(ctx context.Context, req *proto.GetTenantAccountRequest) (*proto.GetTenantAccountResponse, error) {
	if req.TenantId == "" {
		return nil, errs.InvalidArgumentE("tenant_id required")
	}

	ctx, overlayAccountID, usedScope, err := s.resolveTenantAccessAccount(ctx, req.TenantId, true)
	if err != nil {
		return nil, err
	}
	if err := requireScopeRead(usedScope, "account details"); err != nil {
		return nil, err
	}

	resourceTenantID := req.TenantId
	if usedScope != nil {
		resourceTenantID = usedScope.TenantID
	}

	// Get tenant from registry
	t, err := s.tenantRegistry.GetTenant(resourceTenantID)
	if err != nil {
		return nil, errs.NotFoundE("tenant not found")
	}

	// Build response. Phone / phone-verification fields removed in Phase 3 —
	// the proto fields are left empty so the proto definition can stay untouched.
	resp := &proto.GetTenantAccountResponse{
		FullName:          t.FullName,
		Email:             t.Email,
		TotpEnabled:       t.TOTPEnabled,
		TwoFaMethod:       t.TwoFAMethod,
		PreferredLanguage: t.PreferredLanguage,
		TenantId:          resourceTenantID,
	}

	// Add timestamps
	if !t.CreatedAt.IsZero() {
		resp.CreatedAt = proto.TimestampFromTime(t.CreatedAt)
	}
	if !t.LastLogin.IsZero() {
		resp.LastLogin = proto.TimestampFromTime(t.LastLogin)
	}

	// Add overlay account info with peer usage stats
	if overlayAccountID != "" {
		acc, err := s.server.GetAccount(overlayAccountID)
		if err == nil && acc != nil {
			resp.Account = &proto.Account{
				Id:   acc.ID,
				Name: acc.Name,
				Tier: proto.AccountTier_TIER_FREE,
			}
			peerCount, maxPeers, _ := s.server.GetAccountPeerStats(overlayAccountID)
			resp.PeerCount = int32(peerCount)
			resp.MaxPeers = int32(maxPeers)
		}
	}

	return resp, nil
}

// UpdateTenantProfile updates the tenant's profile information
func (s *TenantPortalServiceServer) UpdateTenantProfile(ctx context.Context, req *proto.UpdateTenantProfileRequest) (*proto.UpdateTenantProfileResponse, error) {
	if req.TenantId == "" {
		return nil, errs.InvalidArgumentE("tenant_id required")
	}

	// Get tenant from registry
	t, err := s.tenantRegistry.GetTenant(req.TenantId)
	if err != nil {
		return nil, errs.NotFoundE("tenant not found")
	}

	// Update fields if provided. Phase 3: phone is no longer stored on tenants —
	// any req.Phone value is ignored.
	updated := false
	if req.FullName != "" && req.FullName != t.FullName {
		t.FullName = req.FullName
		updated = true
	}
	if req.PreferredLanguage != "" && req.PreferredLanguage != t.PreferredLanguage {
		t.PreferredLanguage = req.PreferredLanguage
		updated = true
	}

	// Handle password change if requested
	if req.NewPassword != "" {
		if req.CurrentPassword == "" {
			return nil, errs.InvalidArgumentE("current password required to change password")
		}

		// Verify current password
		if err := bcrypt.CompareHashAndPassword([]byte(t.PasswordHash), []byte(req.CurrentPassword)); err != nil {
			return nil, errs.PermissionDeniedE("current password is incorrect")
		}

		// Hash new password
		newHash, err := bcrypt.GenerateFromPassword([]byte(req.NewPassword), bcrypt.DefaultCost)
		if err != nil {
			return nil, errs.InternalE("failed to process password")
		}
		t.PasswordHash = string(newHash)
		updated = true
	}

	// Save if updated
	if updated {
		t.UpdatedAt = time.Now().UTC()
		if err := s.tenantRegistry.UpdateTenant(t); err != nil {
			log.Error().Err(err).Str("tenant_id", req.TenantId).Msg("Failed to update tenant")
			return nil, errs.InternalE("failed to update profile")
		}
	}

	return &proto.UpdateTenantProfileResponse{
		Success: true,
		Message: "Profile updated successfully",
	}, nil
}

// DeleteTenantAccount soft-deletes a tenant account (sets status to "deleted")
func (s *TenantPortalServiceServer) DeleteTenantAccount(ctx context.Context, req *proto.DeleteTenantAccountRequest) (*proto.DeleteTenantAccountResponse, error) {
	if req.TenantId == "" {
		return nil, errs.InvalidArgumentE("tenant_id required")
	}
	if req.Password == "" {
		return nil, errs.InvalidArgumentE("password required for confirmation")
	}

	// Get tenant from registry
	t, err := s.tenantRegistry.GetTenant(req.TenantId)
	if err != nil {
		return nil, errs.NotFoundE("tenant not found")
	}

	// Verify password
	if err := bcrypt.CompareHashAndPassword([]byte(t.PasswordHash), []byte(req.Password)); err != nil {
		return &proto.DeleteTenantAccountResponse{
			Success: false,
			Message: "Incorrect password",
		}, nil
	}

	// Note: Stripe subscription should be cancelled separately via CancelSubscription endpoint
	// before deleting account. Account deletion just marks status as "deleted".

	// Delete all peers from WireGuard
	if t.OverlayAccountID != "" && s.server != nil {
		peers, err := s.server.ListPeers(t.OverlayAccountID)
		if err == nil {
			for _, peer := range peers {
				if err := s.server.RemovePeer(t.OverlayAccountID, peer.ID); err != nil {
					log.Error().Err(err).
						Str("tenant_id", req.TenantId).
						Str("peer_id", peer.ID).
						Msg("Failed to remove peer during account deletion")
				}
			}
		}
	}

	// Set account status to "deleted"
	t.Status = "deleted"
	t.UpdatedAt = time.Now().UTC()

	// Clear sensitive data
	t.PasswordHash = ""
	t.TOTPSecret = ""

	// Save updated tenant
	if err := s.tenantRegistry.UpdateTenant(t); err != nil {
		log.Error().Err(err).Str("tenant_id", req.TenantId).Msg("Failed to delete tenant account")
		return nil, errs.InternalE("failed to delete account")
	}

	// Invalidate all sessions
	if err := s.tenantRegistry.InvalidateAllSessions(req.TenantId); err != nil {
		log.Error().Err(err).Str("tenant_id", req.TenantId).Msg("Failed to invalidate sessions during account deletion")
	}

	log.Debug().
		Str("tenant_id", req.TenantId).
		Str("email", t.Email).
		Str("reason", req.Reason).
		Msg("🗑️ Tenant account deleted")

	return &proto.DeleteTenantAccountResponse{
		Success: true,
		Message: "Account deleted successfully",
	}, nil
}

// ChangePassword handles password changes and triggers a security alert
func (s *TenantPortalServiceServer) ChangePassword(ctx context.Context, req *proto.ChangePasswordRequest) (*proto.ChangePasswordResponse, error) {
	if req.TenantId == "" {
		return nil, errs.InvalidArgumentE("tenant_id required")
	}
	if req.CurrentPassword == "" {
		return nil, errs.InvalidArgumentE("current_password required")
	}
	if req.NewPassword == "" {
		return nil, errs.InvalidArgumentE("new_password required")
	}

	// Get tenant from registry
	t, err := s.tenantRegistry.GetTenant(req.TenantId)
	if err != nil {
		return nil, errs.NotFoundE("tenant not found")
	}

	// Verify old password
	if err := bcrypt.CompareHashAndPassword([]byte(t.PasswordHash), []byte(req.CurrentPassword)); err != nil {
		return &proto.ChangePasswordResponse{
			Success: false,
			Message: "Incorrect current password",
		}, nil
	}

	// Hash new password
	newHash, err := bcrypt.GenerateFromPassword([]byte(req.NewPassword), bcrypt.DefaultCost)
	if err != nil {
		return nil, errs.InternalE("failed to hash password")
	}

	// Update password
	if err := s.tenantRegistry.UpdatePassword(t.ID, string(newHash)); err != nil {
		log.Error().Err(err).Str("tenant_id", t.ID).Msg("Failed to update password")
		return nil, errs.InternalE("failed to update password")
	}

	// Generate security alert token
	// This token allows the user to say "It wasn't me" and lock the account
	// We use the email service's token generator which stores in memory (Redis is better but using existing pattern)
	// Ideally this should use Redis or persistent store
	token, err := s.smtpClient.GenerateVerificationToken(t.Email)
	if err != nil {
		log.Error().Err(err).Str("tenant_id", t.ID).Msg("Failed to generate security token")
		// Continue anyway - password is changed, just alert might fail
	} else if s.smtpClient != nil {
		// Send security alert email (fire-and-forget; bounded by a 30s watchdog log)
		actionDescription := fmt.Sprintf("Password changed for account %s", t.Email)
		go func(email, tok, desc string) {
			done := make(chan struct{})
			go func() {
				defer close(done)
				if err := s.smtpClient.SendSecurityAlert(email, tok, desc); err != nil {
					log.Error().Err(err).Str("email", email).Msg("Failed to send security alert email")
				}
			}()
			// Log a warning if SMTP takes too long (the goroutine still runs until completion).
			timer := time.NewTimer(30 * time.Second)
			defer timer.Stop()
			select {
			case <-done:
			case <-timer.C:
				log.Warn().Str("email", email).Msg("Security alert email taking >30s — SMTP may be slow or blocked")
			}
		}(t.Email, token, actionDescription)
	}

	return &proto.ChangePasswordResponse{
		Success: true,
		Message: "Password updated successfully",
	}, nil
}

// HandleSecurityAlert verifies a security token and locks down the account
func (s *TenantPortalServiceServer) HandleSecurityAlert(ctx context.Context, req *proto.HandleSecurityAlertRequest) (*proto.HandleSecurityAlertResponse, error) {
	if req.Token == "" {
		return nil, errs.InvalidArgumentE("token required")
	}

	if s.smtpClient == nil {
		return nil, errs.UnavailableE("email service unavailable")
	}

	// Validate token
	email, valid, err := s.smtpClient.ValidateVerificationTokenByToken(req.Token)
	if err != nil {
		log.Error().Err(err).Str("token", req.Token).Msg("Error validating security token")
		return nil, errs.InternalE("failed to validate token")
	}
	if !valid {
		return &proto.HandleSecurityAlertResponse{
			Success: false,
			Message: "Invalid or expired security token",
		}, nil
	}

	// Get tenant by email
	t, err := s.tenantRegistry.GetTenantByEmail(email)
	if err != nil {
		log.Warn().Str("email", email).Err(err).Msg("Security alert: tenant not found")
		return &proto.HandleSecurityAlertResponse{
			Success: false,
			Message: "Account not found associated with this token",
		}, nil
	}

	// INVALIDATE ALL SESSIONS
	if err := s.tenantRegistry.InvalidateAllSessions(t.ID); err != nil {
		log.Error().Err(err).Str("tenant_id", t.ID).Msg("Failed to invalidate sessions during security alert")
		return nil, errs.InternalE("failed to secure account")
	}

	log.Warn().
		Str("tenant_id", t.ID).
		Str("email", t.Email).
		Msg("🚨 SECURITY ALERT: Account locked down by user ('It wasn't me' flow)")

	return &proto.HandleSecurityAlertResponse{
		Success:     true,
		Message:     "Account secured. All sessions have been logged out.",
		RedirectUrl: "/reset-password", // Frontend should redirect here
	}, nil
}

// GetTwoFASettings returns the current 2FA settings for a tenant
func (s *TenantPortalServiceServer) GetTwoFASettings(ctx context.Context, req *proto.GetTwoFASettingsRequest) (*proto.GetTwoFASettingsResponse, error) {
	if req.TenantId == "" {
		return nil, errs.InvalidArgumentE("tenant_id required")
	}

	info, err := s.tenantRegistry.GetTwoFAInfo(req.TenantId)
	if err != nil {
		return nil, errs.NotFoundf("tenant not found: %v", err)
	}

	return &proto.GetTwoFASettingsResponse{
		Enabled:          info.Enabled,
		CurrentMethod:    info.Method,
		PhoneMasked:      info.PhoneMasked,
		AvailableMethods: info.CanChangeTo,
	}, nil
}

// SetTwoFAMethod sets or changes the 2FA method for a tenant
func (s *TenantPortalServiceServer) SetTwoFAMethod(ctx context.Context, req *proto.SetTwoFAMethodRequest) (*proto.SetTwoFAMethodResponse, error) {
	if req.TenantId == "" {
		return nil, errs.InvalidArgumentE("tenant_id required")
	}

	// Normalizing method
	method := strings.ToLower(req.Method)
	if method == "none" {
		method = ""
	}

	// If disabling (empty method), just clear it
	if method == "" {
		if err := s.tenantRegistry.SetTwoFAMethod(req.TenantId, "", ""); err != nil {
			return nil, errs.Internalf("failed to disable 2FA: %v", err)
		}
		log.Debug().Str("tenant_id", req.TenantId).Msg("🔓 2FA disabled successfully")
		return &proto.SetTwoFAMethodResponse{
			Success: true,
			Message: "Two-factor authentication disabled",
		}, nil
	}

	// SMS 2FA was removed in Phase 3
	if method == "sms" {
		return &proto.SetTwoFAMethodResponse{
			Success: false,
			Message: "SMS 2FA is no longer supported. Choose TOTP or WhatsApp.",
		}, nil
	}

	// If enabling/changing to TOTP, verify the code first
	if method == tenant.TwoFATOTP {
		if req.TotpSecret == "" {
			return &proto.SetTwoFAMethodResponse{
				Success: false,
				Message: "TOTP secret is required",
			}, nil
		}
		if req.TotpCode == "" {
			return &proto.SetTwoFAMethodResponse{
				Success: false,
				Message: "TOTP verification code is required",
			}, nil
		}
		// Verify the TOTP code against the secret
		if !auth.ValidateTOTP(req.TotpSecret, req.TotpCode) {
			return &proto.SetTwoFAMethodResponse{
				Success: false,
				Message: "Invalid verification code. Please try again.",
			}, nil
		}
		log.Debug().Str("tenant_id", req.TenantId).Msg(" TOTP code verified successfully")
	}

	// Validate other methods?
	// For "email", "sms", "whatsapp", we assume verification happened via "Send2FACode" and client checked it?
	// Or we should verify a code here too?
	// Current implementation relied on client.

	// Save new method
	err := s.tenantRegistry.SetTwoFAMethod(req.TenantId, method, req.TotpSecret)
	if err != nil {
		return &proto.SetTwoFAMethodResponse{
			Success: false,
			Message: err.Error(),
		}, nil
	}

	return &proto.SetTwoFAMethodResponse{
		Success: true,
		Message: fmt.Sprintf("Two-factor authentication enabled via %s", method),
	}, nil
}

// Send2FACode is a stub. SMS 2FA was removed in Phase 3; WhatsApp 2FA codes are
// delivered via the adminbot pipeline (see internal/adminbot/), not via this RPC.
func (s *TenantPortalServiceServer) Send2FACode(ctx context.Context, req *proto.Send2FACodeRequest) (*proto.Send2FACodeResponse, error) {
	return nil, errs.UnimplementedE("SMS 2FA removed; WhatsApp 2FA is delivered via the adminbot")
}

// maskEmailForDisplay masks an email for user display (e.g., "jo***@gmail.com")
func maskEmailForDisplay(email string) string {
	atIdx := strings.Index(email, "@")
	if atIdx < 0 {
		return "***@***"
	}
	local := email[:atIdx]
	domain := email[atIdx:]
	if len(local) <= 2 {
		return "***" + domain
	}
	return local[:2] + "***" + domain
}

// =============================================================================
// Password Reset (Secure Recovery Flow)
// =============================================================================

const (
	resetTokenExpiry     = 1 * time.Hour   // Token valid for 1 hour
	resetSMSCodeExpiry   = 5 * time.Minute // SMS code valid for 5 minutes
	resetRateLimitWindow = 1 * time.Hour   // Rate limit window
	resetRateLimitMax    = 3               // Max 3 requests per hour per email/IP
	resetMaxAttempts     = 5               // Max verification attempts

	// Login failure rate limiting
	loginFailureWindow     = 1 * time.Hour // Rate limit window for login failures
	loginFailureMaxPerDay  = 10            // Max 10 failed login attempts per day per email
	loginFailureMaxPerHour = 5             // Max 5 failed login attempts per hour per email

	// Password reset "not found" limiting (email doesn't exist)
	resetNotFoundWindow = 24 * time.Hour // Rate limit window for reset attempts on non-existent emails
	resetNotFoundMax    = 5              // Max 5 reset attempts per day on non-existent emails
)

// RequestPasswordReset initiates password reset flow
// Security: Rate limited, no user enumeration (always returns success)
func (s *TenantPortalServiceServer) RequestPasswordReset(ctx context.Context, req *proto.RequestPasswordResetRequest) (*proto.RequestPasswordResetResponse, error) {
	email := strings.ToLower(strings.TrimSpace(req.Email))
	if email == "" {
		return nil, errs.InvalidArgumentE("email is required")
	}

	// Rate limiting check
	s.mu.Lock()
	if !s.checkResetRateLimit(email) {
		s.mu.Unlock()
		log.Warn().Str("email", email).Msg("🚫 Password reset rate limit exceeded")
		// Still return success to prevent enumeration
		return &proto.RequestPasswordResetResponse{
			Success: true,
			Message: "If an account exists with this email, you will receive a verification code.",
		}, nil
	}
	s.recordResetAttempt(email)
	s.mu.Unlock()

	// Generic response (no user enumeration)
	genericResponse := &proto.RequestPasswordResetResponse{
		Success:            true,
		Message:            "If an account exists with this email, you will receive a verification code.",
		CodeExpiresSeconds: int32(resetSMSCodeExpiry.Seconds()),
	}

	// Look up tenant by email
	t, err := s.tenantRegistry.GetTenantByEmail(email)
	if err != nil {
		// Rate limiting: track attempts to reset passwords for non-existent emails (prevents enumeration after multiple attempts)
		s.mu.Lock()
		if !s.checkResetNotFoundRateLimit(email) {
			s.mu.Unlock()
			log.Warn().Str("email", email).Msg("🚫 Password reset rate limit exceeded for non-existent email")
			// Still return success to prevent enumeration
			return genericResponse, nil
		}
		s.recordResetNotFoundAttempt(email)
		s.mu.Unlock()

		log.Debug().Str("email", email).Msg("Password reset requested for non-existent email")
		// Return same response to prevent enumeration
		return genericResponse, nil
	}

	// Check if account is active
	if t.Status != "active" {
		log.Debug().Str("email", email).Str("status", t.Status).Msg("Password reset for inactive account")
		return genericResponse, nil
	}

	// Generate secure reset token (32 bytes = 256 bits)
	tokenBytes := make([]byte, 32)
	if _, err := rand.Read(tokenBytes); err != nil {
		return nil, errs.InternalE("failed to generate token")
	}
	resetToken := base64.URLEncoding.EncodeToString(tokenBytes)

	// Hash the token for storage (never store plaintext)
	tokenHash := sha256Hash(resetToken)

	// Generate 6-digit email verification code (phone-based codes removed in Phase 3)
	codeBytes := make([]byte, 3)
	rand.Read(codeBytes)
	smsCode := fmt.Sprintf("%06d", (int(codeBytes[0])<<16|int(codeBytes[1])<<8|int(codeBytes[2]))%1000000)
	smsCodeHash := sha256Hash(smsCode)

	// Store pending reset
	now := time.Now().UTC()
	pending := &pendingPasswordReset{
		TenantID:     t.ID,
		Email:        email,
		TokenHash:    tokenHash,
		SMSCodeHash:  smsCodeHash,
		SMSVerified:  false,
		CreatedAt:    now,
		ExpiresAt:    now.Add(resetTokenExpiry),
		SMSExpiresAt: now.Add(resetSMSCodeExpiry),
		AttemptCount: 0,
		MaxAttempts:  resetMaxAttempts,
	}

	s.mu.Lock()
	// Use first 16 chars of token hash as key (enough for uniqueness)
	s.pendingResets[tokenHash[:16]] = pending
	s.mu.Unlock()

	// Send verification code via Email (SMTP)
	if s.smtpClient != nil {
		err := s.smtpClient.SendPasswordResetCode(email, smsCode)
		if err != nil {
			log.Error().Err(err).Str("email", email).Msg("❌ Failed to send password reset email")
			// Still return success to prevent enumeration
			return genericResponse, nil
		}
		log.Debug().
			Str("email", email).
			Msg(" Password reset email sent via Brevo")
	} else {
		log.Warn().Str("email", email).Msg(" Brevo not configured - cannot send password reset email")
		// Still return success to prevent enumeration
		return genericResponse, nil
	}

	log.Debug().
		Str("email", email).
		Msg(" Password reset initiated")

	return &proto.RequestPasswordResetResponse{
		Success:            true,
		Message:            "Verification code sent to your email.",
		ResetToken:         resetToken,
		PhoneMasked:        maskEmailForDisplay(email),
		CodeExpiresSeconds: int32(resetSMSCodeExpiry.Seconds()),
	}, nil
}

// VerifyResetCode verifies SMS code and returns verified token
func (s *TenantPortalServiceServer) VerifyResetCode(ctx context.Context, req *proto.VerifyResetCodeRequest) (*proto.VerifyResetCodeResponse, error) {
	if req.ResetToken == "" {
		return nil, errs.InvalidArgumentE(ErrResetTokenInvalid)
	}
	if req.VerificationCode == "" {
		return nil, errs.InvalidArgumentE(ErrResetCodeInvalid)
	}

	// Hash the provided token
	tokenHash := sha256Hash(req.ResetToken)
	tokenKey := tokenHash[:16]

	s.mu.Lock()
	pending, exists := s.pendingResets[tokenKey]
	if !exists {
		s.mu.Unlock()
		return &proto.VerifyResetCodeResponse{
			Success: false,
			Message: ErrResetTokenInvalid,
		}, nil
	}

	// Check if token expired
	if time.Now().UTC().After(pending.ExpiresAt) {
		delete(s.pendingResets, tokenKey)
		s.mu.Unlock()
		return &proto.VerifyResetCodeResponse{
			Success: false,
			Message: ErrResetTokenExpired,
		}, nil
	}

	// Check max attempts
	if pending.AttemptCount >= pending.MaxAttempts {
		delete(s.pendingResets, tokenKey)
		s.mu.Unlock()
		log.Warn().Str("email", pending.Email).Msg("🚫 Password reset max attempts exceeded")
		return &proto.VerifyResetCodeResponse{
			Success: false,
			Message: ErrResetMaxAttempts,
		}, nil
	}

	// Verify the code (stored hash from when we sent the email)
	codeHash := sha256Hash(req.VerificationCode)
	if codeHash != pending.SMSCodeHash {
		pending.AttemptCount++
		s.mu.Unlock()
		log.Debug().
			Str("email", pending.Email).
			Int("attempts", pending.AttemptCount).
			Msg("Invalid verification code for password reset")
		return &proto.VerifyResetCodeResponse{
			Success: false,
			Message: ErrResetCodeInvalid,
		}, nil
	}

	// Check if code expired
	if time.Now().UTC().After(pending.SMSExpiresAt) {
		delete(s.pendingResets, tokenKey)
		s.mu.Unlock()
		return &proto.VerifyResetCodeResponse{
			Success: false,
			Message: ErrResetCodeExpired,
		}, nil
	}

	// Mark as verified and generate new verified token
	pending.SMSVerified = true

	// Generate new verified token
	verifiedTokenBytes := make([]byte, 32)
	rand.Read(verifiedTokenBytes)
	verifiedToken := base64.URLEncoding.EncodeToString(verifiedTokenBytes)
	verifiedTokenHash := sha256Hash(verifiedToken)

	// Update the pending reset with verified token
	pending.TokenHash = verifiedTokenHash
	delete(s.pendingResets, tokenKey)
	s.pendingResets[verifiedTokenHash[:16]] = pending
	s.mu.Unlock()

	log.Debug().
		Str("email", pending.Email).
		Msg(" Password reset SMS verified")

	return &proto.VerifyResetCodeResponse{
		Success:       true,
		Message:       "Phone verified. You can now reset your password.",
		VerifiedToken: verifiedToken,
	}, nil
}

// ResetPassword completes the password reset
// Security: Token is single-use, invalidates all sessions
func (s *TenantPortalServiceServer) ResetPassword(ctx context.Context, req *proto.ResetPasswordRequest) (*proto.ResetPasswordResponse, error) {
	if req.VerifiedToken == "" {
		return nil, errs.InvalidArgumentE("verified_token is required")
	}
	if req.NewPassword == "" {
		return nil, errs.InvalidArgumentE("new_password is required")
	}

	// Validate password strength
	if len(req.NewPassword) < 8 {
		return &proto.ResetPasswordResponse{
			Success: false,
			Message: ErrPasswordTooShort,
		}, nil
	}

	// Hash the provided token
	tokenHash := sha256Hash(req.VerifiedToken)
	tokenKey := tokenHash[:16]

	s.mu.Lock()
	pending, exists := s.pendingResets[tokenKey]
	if !exists {
		s.mu.Unlock()
		return &proto.ResetPasswordResponse{
			Success: false,
			Message: ErrResetTokenInvalid,
		}, nil
	}

	// Check if verified
	if !pending.SMSVerified {
		s.mu.Unlock()
		return &proto.ResetPasswordResponse{
			Success: false,
			Message: ErrResetPhoneRequired,
		}, nil
	}

	// Check if token expired
	if time.Now().UTC().After(pending.ExpiresAt) {
		delete(s.pendingResets, tokenKey)
		s.mu.Unlock()
		return &proto.ResetPasswordResponse{
			Success: false,
			Message: ErrResetTokenExpired,
		}, nil
	}

	// Delete pending reset (single-use token)
	tenantID := pending.TenantID
	email := pending.Email
	delete(s.pendingResets, tokenKey)
	s.mu.Unlock()

	// Hash new password
	hashedPassword, err := bcrypt.GenerateFromPassword([]byte(req.NewPassword), bcrypt.DefaultCost)
	if err != nil {
		return nil, errs.InternalE("failed to hash password")
	}

	// Update password in tenant registry
	if err := s.tenantRegistry.UpdatePassword(tenantID, string(hashedPassword)); err != nil {
		return nil, errs.Internalf("failed to update password: %v", err)
	}

	// Invalidate all sessions for this tenant
	sessionsInvalidated := false
	if err := s.tenantRegistry.InvalidateAllSessions(tenantID); err != nil {
		log.Warn().Err(err).Str("tenant_id", tenantID).Msg("Failed to invalidate sessions")
	} else {
		sessionsInvalidated = true
	}

	log.Debug().
		Str("email", email).
		Bool("sessions_invalidated", sessionsInvalidated).
		Msg(" Password reset completed")

	// Note: Email notification about password change is logged above.
	// A dedicated security notification email could be added by injecting an email service
	// into TenantPortalServiceServer, similar to twilioClient for SMS.

	return &proto.ResetPasswordResponse{
		Success:             true,
		Message:             "Password has been reset successfully. Please log in with your new password.",
		SessionsInvalidated: sessionsInvalidated,
	}, nil
}

// checkResetRateLimit checks if email has exceeded rate limit
// Must be called with s.mu held
func (s *TenantPortalServiceServer) checkResetRateLimit(email string) bool {
	now := time.Now().UTC()
	cutoff := now.Add(-resetRateLimitWindow)

	times, exists := s.resetRateLimiter[email]
	if !exists {
		return true
	}

	// Count requests in window
	validTimes := make([]time.Time, 0, len(times))
	for _, t := range times {
		if t.After(cutoff) {
			validTimes = append(validTimes, t)
		}
	}
	s.resetRateLimiter[email] = validTimes

	return len(validTimes) < resetRateLimitMax
}

// recordResetAttempt records a reset attempt for rate limiting
// Must be called with s.mu held
func (s *TenantPortalServiceServer) recordResetAttempt(email string) {
	s.resetRateLimiter[email] = append(s.resetRateLimiter[email], time.Now().UTC())
}

// checkLoginRateLimit checks if email has exceeded login failure rate limit
// Must be called with s.mu held
func (s *TenantPortalServiceServer) checkLoginRateLimit(email string) bool {
	now := time.Now().UTC()
	hourCutoff := now.Add(-loginFailureWindow) // Last hour
	dayCutoff := now.Add(-24 * time.Hour)      // Last 24 hours

	times, exists := s.loginFailures[email]
	if !exists {
		return true // No failures on record
	}

	// Count failures in the last hour and last 24 hours
	var failuresInHour, failuresInDay int
	validTimes := make([]time.Time, 0, len(times))
	for _, t := range times {
		if t.After(dayCutoff) {
			validTimes = append(validTimes, t)
			if t.After(hourCutoff) {
				failuresInHour++
			}
			failuresInDay++
		}
	}
	s.loginFailures[email] = validTimes

	// Check both hourly and daily limits
	if failuresInHour >= loginFailureMaxPerHour {
		return false
	}
	if failuresInDay >= loginFailureMaxPerDay {
		return false
	}

	return true
}

// recordLoginFailure records a login failure for rate limiting
// Must be called with s.mu held
func (s *TenantPortalServiceServer) recordLoginFailure(email string) {
	s.loginFailures[email] = append(s.loginFailures[email], time.Now().UTC())
	log.Debug().Str("email", email).Msg(" Login failure recorded for rate limiting")
}

// clearLoginFailures clears failed login attempts for a successful login
// Must be called with s.mu held
func (s *TenantPortalServiceServer) clearLoginFailures(email string) {
	delete(s.loginFailures, email)
	log.Debug().Str("email", email).Msg(" Login failure record cleared after successful login")
}

// checkResetNotFoundRateLimit checks if email has exceeded rate limit for password reset on non-existent emails
// Must be called with s.mu held
func (s *TenantPortalServiceServer) checkResetNotFoundRateLimit(email string) bool {
	now := time.Now().UTC()
	cutoff := now.Add(-resetNotFoundWindow)

	times, exists := s.resetNotFoundFailures[email]
	if !exists {
		return true // No attempts on record
	}

	// Count requests in window
	validTimes := make([]time.Time, 0, len(times))
	for _, t := range times {
		if t.After(cutoff) {
			validTimes = append(validTimes, t)
		}
	}
	s.resetNotFoundFailures[email] = validTimes

	return len(validTimes) < resetNotFoundMax
}

// recordResetNotFoundAttempt records a password reset attempt on a non-existent email
// Must be called with s.mu held
func (s *TenantPortalServiceServer) recordResetNotFoundAttempt(email string) {
	s.resetNotFoundFailures[email] = append(s.resetNotFoundFailures[email], time.Now().UTC())
	log.Debug().Str("email", email).Msg(" Password reset attempt on non-existent email (rate limiting)")
}

// sha256Hash returns hex-encoded SHA-256 hash
func sha256Hash(data string) string {
	h := sha256.Sum256([]byte(data))
	return hex.EncodeToString(h[:])
}

// =============================================================================
// Tenant Billing Service
// =============================================================================

// TenantBillingServiceServer implements TenantBillingService.
// Billing has been removed. All methods return Unimplemented.
type TenantBillingServiceServer struct {
	UnimplementedTenantBillingService
	server         *server.Server
	tenantRegistry tenant.Registry
	smtpClient     *email.SMTPService
}

// NewTenantBillingServiceServer creates a new TenantBillingServiceServer
func NewTenantBillingServiceServer(
	srv *server.Server,
	tenantRegistry tenant.Registry,
	smtpClient *email.SMTPService,
) *TenantBillingServiceServer {
	return &TenantBillingServiceServer{
		server:         srv,
		tenantRegistry: tenantRegistry,
		smtpClient:     smtpClient,
	}
}

// GetSubscriptionStatus returns Unimplemented; billing has been removed.
func (s *TenantBillingServiceServer) GetSubscriptionStatus(ctx context.Context, req *proto.GetSubscriptionStatusRequest) (*proto.GetSubscriptionStatusResponse, error) {
	return nil, errs.UnimplementedE("billing removed")
}

// ChangeTier returns Unimplemented; billing has been removed.
func (s *TenantBillingServiceServer) ChangeTier(ctx context.Context, req *proto.ChangeTierRequest) (*proto.ChangeTierResponse, error) {
	return nil, errs.UnimplementedE("billing removed")
}

// CreateSetupIntent returns Unimplemented; billing has been removed.
func (s *TenantBillingServiceServer) CreateSetupIntent(ctx context.Context, req *proto.CreateBillingSetupIntentRequest) (*proto.CreateBillingSetupIntentResponse, error) {
	return nil, errs.UnimplementedE("billing removed")
}

// GetBillingPortal returns Unimplemented; billing has been removed.
func (s *TenantBillingServiceServer) GetBillingPortal(ctx context.Context, req *proto.GetBillingPortalRequest) (*proto.GetBillingPortalResponse, error) {
	return nil, errs.UnimplementedE("billing removed")
}

// CancelSubscription returns Unimplemented; billing has been removed.
func (s *TenantBillingServiceServer) CancelSubscription(ctx context.Context, req *proto.CancelSubscriptionRequest) (*proto.CancelSubscriptionResponse, error) {
	return nil, errs.UnimplementedE("billing removed")
}

// GetBillingHistory returns Unimplemented; billing has been removed.
func (s *TenantBillingServiceServer) GetBillingHistory(ctx context.Context, req *proto.GetBillingHistoryRequest) (*proto.GetBillingHistoryResponse, error) {
	return nil, errs.UnimplementedE("billing removed")
}

// ContactSales remains functional but stubbed (no Stripe involvement).
func (s *TenantBillingServiceServer) ContactSales(ctx context.Context, req *proto.ContactSalesRequest) (*proto.ContactSalesResponse, error) {
	return nil, errs.UnimplementedE("billing removed")
}

// =============================================================================
// Tenant Data Service
// =============================================================================

// TenantDataServiceServer implements TenantDataService.
type TenantDataServiceServer struct {
	UnimplementedTenantDataService
	server         *server.Server
	tenantRegistry tenant.Registry
	// tenantStores   *tenant.TenantStoreManager // LMDB storage manager - disabled for Postgres migration

	// Backup state tracking (in production, this should be in persistent storage)
	backupJobs  map[string]*backupJob  // backup_id -> job
	restoreJobs map[string]*restoreJob // restore_id -> job
	mu          sync.RWMutex

	// wg tracks in-flight background restore goroutines so a graceful shutdown
	// can wait for them to complete before the process exits.
	wg sync.WaitGroup
}

type backupJob struct {
	BackupID    string
	TenantID    string
	Status      string // pending, processing, ready, error
	Message     string
	FilePath    string // Local path to backup file
	SizeBytes   int64
	CreatedAt   time.Time
	ExpiresAt   time.Time
	DownloadURL string
}

type restoreJob struct {
	RestoreID   string
	TenantID    string
	Status      string // pending, in_progress, completed, failed
	Progress    int32
	Message     string
	StartedAt   time.Time
	CompletedAt time.Time
}

// NewTenantDataServiceServer creates a new TenantDataServiceServer
func NewTenantDataServiceServer(
	srv *server.Server,
	tenantRegistry tenant.Registry,
	// tenantStores *tenant.TenantStoreManager,
) *TenantDataServiceServer {
	return &TenantDataServiceServer{
		server:         srv,
		tenantRegistry: tenantRegistry,
		// tenantStores:   tenantStores,
		backupJobs:  make(map[string]*backupJob),
		restoreJobs: make(map[string]*restoreJob),
	}
}

// RequestBackup initiates a database backup for the tenant
func (s *TenantDataServiceServer) RequestBackup(ctx context.Context, req *proto.RequestBackupRequest) (*proto.RequestBackupResponse, error) {
	return nil, errs.UnimplementedE("backup not available in Postgres mode")
	/*
			if req.TenantId == "" {
				return nil, errs.InvalidArgumentE("tenant_id is required")
			}
		    // ...
	*/
}

// performBackup executes the actual backup process in background
func (s *TenantDataServiceServer) performBackup() {
	// Disabled for Postgres migration

	/*
			s.mu.RLock()
			job, exists := s.backupJobs[backupID]
		    // ...
	*/
}

// ListBackups returns all backups for a tenant
func (s *TenantDataServiceServer) ListBackups(ctx context.Context, req *proto.ListBackupsRequest) (*proto.ListBackupsResponse, error) {
	if req.TenantId == "" {
		return nil, errs.InvalidArgumentE("tenant_id is required")
	}

	const maxBackups = 5

	s.mu.RLock()
	defer s.mu.RUnlock()

	var backups []*proto.BackupInfo
	for _, job := range s.backupJobs {
		if job.TenantID == req.TenantId {
			backup := &proto.BackupInfo{
				BackupId:  job.BackupID,
				Status:    job.Status,
				SizeBytes: job.SizeBytes,
				CreatedAt: proto.TimestampFromTime(job.CreatedAt),
				ExpiresAt: proto.TimestampFromTime(job.ExpiresAt),
				Message:   job.Message,
			}
			backups = append(backups, backup)
		}
	}

	// Sort by created_at descending (newest first)
	sort.Slice(backups, func(i, j int) bool {
		return backups[i].CreatedAt.AsTime().After(backups[j].CreatedAt.AsTime())
	})

	return &proto.ListBackupsResponse{
		Backups:    backups,
		MaxBackups: maxBackups,
	}, nil
}

// GetBackupDownloadURL returns a signed URL for downloading the backup
func (s *TenantDataServiceServer) GetBackupDownloadURL(ctx context.Context, req *proto.GetBackupDownloadURLRequest) (*proto.GetBackupDownloadURLResponse, error) {
	if req.TenantId == "" {
		return nil, errs.InvalidArgumentE("tenant_id is required")
	}
	if req.BackupId == "" {
		return nil, errs.InvalidArgumentE("backup_id is required")
	}

	s.mu.RLock()
	job, exists := s.backupJobs[req.BackupId]
	s.mu.RUnlock()

	if !exists {
		return nil, errs.NotFoundE("backup not found")
	}

	if job.TenantID != req.TenantId {
		return nil, errs.PermissionDeniedE("backup does not belong to this tenant")
	}

	if job.Status != "ready" {
		return nil, errs.FailedPreconditionf("backup not ready: %s", job.Status)
	}

	// Generate download URL (in production, this would be a signed S3 URL or similar)
	// For now, return a local file path reference
	downloadURL := fmt.Sprintf("/api/v1/tenant/backups/%s/download", req.BackupId)

	return &proto.GetBackupDownloadURLResponse{
		DownloadUrl: downloadURL,
		SizeBytes:   job.SizeBytes,
		ExpiresAt:   proto.TimestampFromTime(time.Now().UTC().Add(1 * time.Hour)), // Download URL expires in 1 hour (not the backup itself)
	}, nil
}

// RestoreFromBackup initiates a database restore from a backup
func (s *TenantDataServiceServer) RestoreFromBackup(ctx context.Context, req *proto.RestoreFromBackupRequest) (*proto.RestoreFromBackupResponse, error) {
	if req.TenantId == "" {
		return nil, errs.InvalidArgumentE("tenant_id is required")
	}
	if req.UploadToken == "" {
		return nil, errs.InvalidArgumentE("upload_token is required")
	}

	// Verify tenant exists
	_, err := s.tenantRegistry.GetTenant(req.TenantId)
	if err != nil {
		return nil, errs.NotFoundE("tenant not found")
	}

	// Generate restore ID
	restoreID := fmt.Sprintf("restore_%s_%d", req.TenantId[:8], time.Now().UTC().Unix())

	// Create restore job
	job := &restoreJob{
		RestoreID: restoreID,
		TenantID:  req.TenantId,
		Status:    "pending",
		Progress:  0,
		Message:   "Restore queued",
		StartedAt: time.Now().UTC(),
	}

	s.mu.Lock()
	s.restoreJobs[restoreID] = job
	s.mu.Unlock()

	// Start restore in background — bounded to 30 minutes and tracked by the WaitGroup.
	s.wg.Add(1)
	go func() {
		defer s.wg.Done()
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Minute)
		defer cancel()
		_ = ctx // context available for future use when performRestore accepts ctx
		s.performRestore(restoreID, req.UploadToken)
	}()

	log.Debug().
		Str("tenant_id", req.TenantId).
		Str("restore_id", restoreID).
		Msg(" Restore requested")

	return &proto.RestoreFromBackupResponse{
		RestoreId: restoreID,
		Status:    "pending",
		Message:   "Restore initiated",
	}, nil
}

// performRestore executes the actual restore process in background
func (s *TenantDataServiceServer) performRestore(restoreID, uploadToken string) {
	s.mu.RLock()
	job, exists := s.restoreJobs[restoreID]
	s.mu.RUnlock()

	if !exists {
		return
	}

	// Update status to in_progress
	s.mu.Lock()
	job.Status = "in_progress"
	job.Progress = 10
	job.Message = "Validating backup file"
	s.mu.Unlock()

	// Locate backup file (try both backup_id pattern and upload_token)
	var backupFile string
	possiblePaths := []string{
		filepath.Join("./backups", fmt.Sprintf("%s.zip", uploadToken)),
		filepath.Join("./backups", fmt.Sprintf("backup_%s.zip", uploadToken)),
		filepath.Join("./backups", uploadToken),
	}

	for _, path := range possiblePaths {
		if _, err := os.Stat(path); err == nil {
			backupFile = path
			break
		}
	}

	if backupFile == "" {
		s.mu.Lock()
		job.Status = "failed"
		job.Message = "Backup file not found"
		job.CompletedAt = time.Now().UTC()
		s.mu.Unlock()
		log.Error().Str("restore_id", restoreID).Str("upload_token", uploadToken).Msg("Restore failed: backup file not found")
		return
	}

	// Update progress
	s.mu.Lock()
	job.Progress = 30
	job.Message = "Opening backup archive"
	s.mu.Unlock()

	// Open ZIP file
	zipReader, err := zip.OpenReader(backupFile)
	if err != nil {
		s.mu.Lock()
		job.Status = "failed"
		job.Message = fmt.Sprintf("Failed to open backup: %v", err)
		job.CompletedAt = time.Now().UTC()
		s.mu.Unlock()
		log.Error().Err(err).Str("restore_id", restoreID).Msg("Restore failed: invalid ZIP file")
		return
	}
	defer zipReader.Close()

	// Find database file in ZIP
	var dbFile *zip.File
	for _, f := range zipReader.File {
		if f.Name == "database.tar.gz" {
			dbFile = f
			break
		}
	}

	if dbFile == nil {
		s.mu.Lock()
		job.Status = "failed"
		job.Message = "Database file not found in backup"
		job.CompletedAt = time.Now().UTC()
		s.mu.Unlock()
		log.Error().Str("restore_id", restoreID).Msg("Restore failed: database.tar.gz not found in ZIP")
		return
	}

	// Extract database data
	s.mu.Lock()
	job.Progress = 50
	job.Message = "Extracting database"
	s.mu.Unlock()

	rc, err := dbFile.Open()
	if err != nil {
		s.mu.Lock()
		job.Status = "failed"
		job.Message = fmt.Sprintf("Failed to extract database: %v", err)
		job.CompletedAt = time.Now().UTC()
		s.mu.Unlock()
		log.Error().Err(err).Str("restore_id", restoreID).Msg("Restore failed: cannot read database file")
		return
	}
	defer rc.Close()

	// Read tar.gz data into buffer
	var buf bytes.Buffer
	if _, err := io.Copy(&buf, rc); err != nil {
		s.mu.Lock()
		job.Status = "failed"
		job.Message = fmt.Sprintf("Failed to read database: %v", err)
		job.CompletedAt = time.Now().UTC()
		s.mu.Unlock()
		log.Error().Err(err).Str("restore_id", restoreID).Msg("Restore failed: cannot copy database data")
		return
	}
	rc.Close()

	// Get tenant store and close it for restore
	// s.mu.Lock()
	// job.Progress = 70
	// job.Message = "Preparing restore"
	// s.mu.Unlock()

	// store, err := s.tenantStores.GetStore(job.TenantID)
	// if err != nil {
	// 	s.mu.Lock()
	// 	job.Status = "failed"
	// 	job.Message = "Failed to access tenant database"
	// 	job.CompletedAt = time.Now().UTC()
	// 	s.mu.Unlock()
	// 	log.Error().Err(err).Str("restore_id", restoreID).Msg("Restore failed: cannot access store")
	// 	return
	// }

	// // Get the store's database path before closing
	// storePath := filepath.Join("./data/tenants", job.TenantID)

	// // Close the store to release locks
	// if err := store.Close(); err != nil {
	// 	log.Warn().Err(err).Str("restore_id", restoreID).Msg("Warning: failed to close store cleanly")
	// }

	// // Restore: Extract tar.gz over existing database files
	// s.mu.Lock()
	// job.Progress = 80
	// job.Message = "Restoring database files"
	// s.mu.Unlock()

	// if err := s.extractTarGzToPath(&buf, storePath); err != nil {
	// 	s.mu.Lock()
	// 	job.Status = "failed"
	// 	job.Message = fmt.Sprintf("Failed to restore: %v", err)
	// 	job.CompletedAt = time.Now().UTC()
	// 	s.mu.Unlock()
	// 	log.Error().Err(err).Str("restore_id", restoreID).Msg("Restore failed: cannot extract files")
	// 	return
	// }

	// // Reopen store
	// s.mu.Lock()
	// job.Progress = 95
	// job.Message = "Reopening database"
	// s.mu.Unlock()

	// if _, err := s.tenantStores.GetStore(job.TenantID); err != nil {
	// 	s.mu.Lock()
	// 	job.Status = "failed"
	// 	job.Message = fmt.Sprintf("Failed to reopen database: %v", err)
	// 	job.CompletedAt = time.Now().UTC()
	// 	s.mu.Unlock()
	// 	log.Error().Err(err).Str("restore_id", restoreID).Msg("Restore failed: cannot reopen store")
	// 	return
	// }

	// Complete
	s.mu.Lock()
	job.Status = "failed"
	job.Message = "Restore functionality is disabled for Postgres migration"
	job.CompletedAt = time.Now().UTC()
	s.mu.Unlock()

	log.Warn().Str("restore_id", restoreID).Msg("Restore functionality is disabled")
}

// extractTarGzToPath extracts a tar.gz buffer to a target directory
func (s *TenantDataServiceServer) extractTarGzToPath(buf *bytes.Buffer, targetPath string) error {
	// Ensure target directory exists
	if err := os.MkdirAll(targetPath, 0755); err != nil {
		return fmt.Errorf("create target directory: %w", err)
	}

	// This is a simplified restore - in production you'd use gzip+tar readers
	// to properly extract the tar.gz created by store.Backup()
	// For now, we write the entire tar.gz and rely on LMDB's robustness

	// Write backup data to temp file
	tempFile := filepath.Join(targetPath, "restore.tar.gz")
	if err := os.WriteFile(tempFile, buf.Bytes(), 0644); err != nil {
		return fmt.Errorf("write temp restore file: %w", err)
	}
	defer os.Remove(tempFile)

	// TODO: Implement proper tar.gz extraction here
	// For now, log that manual extraction may be needed
	log.Debug().
		Str("temp_file", tempFile).
		Str("target_path", targetPath).
		Msg("  Restore data written - manual extraction may be required for full restore")

	return nil
}

// =============================================================================
// Helper Functions
// =============================================================================
func (s *TenantDataServiceServer) GetRestoreStatus(ctx context.Context, req *proto.GetRestoreStatusRequest) (*proto.GetRestoreStatusResponse, error) {
	if req.TenantId == "" {
		return nil, errs.InvalidArgumentE("tenant_id is required")
	}
	if req.RestoreId == "" {
		return nil, errs.InvalidArgumentE("restore_id is required")
	}

	s.mu.RLock()
	job, exists := s.restoreJobs[req.RestoreId]
	s.mu.RUnlock()

	if !exists {
		return nil, errs.NotFoundE("restore job not found")
	}

	if job.TenantID != req.TenantId {
		return nil, errs.PermissionDeniedE("restore job does not belong to this tenant")
	}

	resp := &proto.GetRestoreStatusResponse{
		Status:          job.Status,
		ProgressPercent: job.Progress,
		Message:         job.Message,
		StartedAt:       proto.TimestampFromTime(job.StartedAt),
	}

	if !job.CompletedAt.IsZero() {
		resp.CompletedAt = proto.TimestampFromTime(job.CompletedAt)
	}

	return resp, nil
}

// DeleteBackup deletes a backup by ID
func (s *TenantDataServiceServer) DeleteBackup(ctx context.Context, req *proto.DeleteBackupRequest) (*proto.DeleteBackupResponse, error) {
	if req.TenantId == "" {
		return nil, errs.InvalidArgumentE("tenant_id is required")
	}
	if req.BackupId == "" {
		return nil, errs.InvalidArgumentE("backup_id is required")
	}

	log.Debug().
		Str("tenant_id", req.TenantId).
		Str("backup_id", req.BackupId).
		Msg("🗑️ Deleting backup")

	// Load backup job
	s.mu.RLock()
	job, exists := s.backupJobs[req.BackupId]
	s.mu.RUnlock()

	if !exists {
		return &proto.DeleteBackupResponse{
			Success: false,
			Message: "Backup not found",
		}, nil
	}

	if job.TenantID != req.TenantId {
		return nil, errs.PermissionDeniedE("backup does not belong to this tenant")
	}

	// Delete backup file
	if job.FilePath != "" {
		if err := os.Remove(job.FilePath); err != nil && !os.IsNotExist(err) {
			log.Error().Err(err).Str("path", job.FilePath).Msg("Failed to delete backup file")
			return nil, errs.InternalE("failed to delete backup file")
		}
	}

	// Delete from in-memory map
	s.mu.Lock()
	delete(s.backupJobs, req.BackupId)
	s.mu.Unlock()

	log.Debug().
		Str("tenant_id", req.TenantId).
		Str("backup_id", req.BackupId).
		Msg(" Backup deleted successfully")

	return &proto.DeleteBackupResponse{
		Success: true,
		Message: "Backup deleted successfully",
	}, nil
}

// RestoreBackup restores tenant data from an existing backup by ID
func (s *TenantDataServiceServer) RestoreBackup(ctx context.Context, req *proto.RestoreBackupRequest) (*proto.RestoreBackupResponse, error) {
	if req.TenantId == "" {
		return nil, errs.InvalidArgumentE("tenant_id is required")
	}
	if req.BackupId == "" {
		return nil, errs.InvalidArgumentE("backup_id is required")
	}

	log.Debug().
		Str("tenant_id", req.TenantId).
		Str("backup_id", req.BackupId).
		Msg(" Restoring from backup")

	// Verify backup exists and is ready
	s.mu.RLock()
	job, exists := s.backupJobs[req.BackupId]
	s.mu.RUnlock()

	if !exists {
		return nil, errs.NotFoundE("backup not found")
	}

	if job.TenantID != req.TenantId {
		return nil, errs.PermissionDeniedE("backup does not belong to this tenant")
	}

	if job.Status != "ready" {
		return nil, errs.FailedPreconditionE(fmt.Sprintf("backup is not ready (status: %s)", job.Status))
	}

	// Verify backup file exists
	if _, err := os.Stat(job.FilePath); os.IsNotExist(err) {
		return nil, errs.NotFoundE("backup file not found")
	}

	// Create restore job
	restoreID := uuid.New().String()
	restoreJobObj := &restoreJob{
		RestoreID: restoreID,
		TenantID:  req.TenantId,
		Status:    "pending",
		Message:   "Restore queued",
		StartedAt: time.Now().UTC(),
		Progress:  0,
	}

	s.mu.Lock()
	s.restoreJobs[restoreID] = restoreJobObj
	s.mu.Unlock()

	// Start restore in background — bounded to 30 minutes and tracked by the WaitGroup.
	s.wg.Add(1)
	go func() {
		defer s.wg.Done()
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Minute)
		defer cancel()
		_ = ctx // context available when performRestoreFromExistingBackup accepts ctx
		s.performRestoreFromExistingBackup(restoreID, req.TenantId, req.BackupId, job.FilePath)
	}()

	return &proto.RestoreBackupResponse{
		RestoreId: restoreID,
		Status:    "pending",
		Message:   "Restore initiated successfully",
	}, nil
}

// performRestoreFromExistingBackup performs the actual restore from an existing backup file
func (s *TenantDataServiceServer) performRestoreFromExistingBackup(restoreID, tenantID, backupID, backupPath string) {
	log.Debug().
		Str("restore_id", restoreID).
		Str("tenant_id", tenantID).
		Str("backup_id", backupID).
		Msg(" Starting restore from existing backup")

	// Update job status
	updateJob := func(statusVal, message string, progress int32) {
		s.mu.Lock()
		if job, exists := s.restoreJobs[restoreID]; exists {
			job.Status = statusVal
			job.Message = message
			job.Progress = progress
			if statusVal == "completed" || statusVal == "failed" {
				job.CompletedAt = time.Now().UTC()
			}
		}
		s.mu.Unlock()
	}

	updateJob("in_progress", "Reading backup file", 10)

	// Read backup file (it's a ZIP containing database.tar.gz)
	zipFile, err := zip.OpenReader(backupPath)
	if err != nil {
		log.Error().Err(err).Msg("Failed to open backup file")
		updateJob("failed", fmt.Sprintf("Failed to open backup file: %v", err), 0)
		return
	}
	defer zipFile.Close()

	updateJob("in_progress", "Extracting backup data", 30)

	// Find database.tar.gz in ZIP
	var tarGzData bytes.Buffer
	found := false
	for _, file := range zipFile.File {
		if file.Name == "database.tar.gz" {
			found = true
			rc, err := file.Open()
			if err != nil {
				log.Error().Err(err).Msg("Failed to extract database.tar.gz from ZIP")
				updateJob("failed", fmt.Sprintf("Failed to extract backup: %v", err), 0)
				return
			}
			_, err = io.Copy(&tarGzData, rc)
			rc.Close()
			if err != nil {
				log.Error().Err(err).Msg("Failed to read database.tar.gz")
				updateJob("failed", fmt.Sprintf("Failed to read backup: %v", err), 0)
				return
			}
			break
		}
	}

	if !found {
		log.Error().Msg("database.tar.gz not found in backup ZIP")
		updateJob("failed", "Invalid backup: missing database.tar.gz", 0)
		return
	}

	updateJob("failed", "Restore functionality is disabled for Postgres migration", 0)

	// // Close the store to ensure no locks during restore
	// _ = s.tenantStores.CloseStore(tenantID)

	// updateJob("in_progress", "Applying restored data", 80)

	// // Use tenantStores' built-in RestoreStore method (expects tar.gz buffer)
	// if err := s.tenantStores.RestoreStore(tenantID, &tarGzData); err != nil {
	// 	log.Error().Err(err).Msg("Failed to restore tenant data")
	// 	updateJob("failed", fmt.Sprintf("Failed to restore data: %v", err), 0)
	// 	return
	// }

	// log.Debug().
	// 	Str("restore_id", restoreID).
	// 	Str("tenant_id", tenantID).
	// 	Str("backup_id", backupID).
	// 	Msg(" Restore completed successfully")

	// updateJob("completed", "Restore completed successfully", 100)
}

// =============================================================================
// Helper Functions
// =============================================================================

func tierToStringFromProto(t proto.AccountTier) string {
	switch t {
	case proto.AccountTier_TIER_FREE:
		return "free"
	case proto.AccountTier_TIER_STANDARD:
		return "standard"
	case proto.AccountTier_TIER_PREMIUM:
		return "premium"
	default:
		return "free"
	}
}

func stringToProtoTier(s string) proto.AccountTier {
	normalized := strings.ToLower(strings.TrimSpace(s))
	switch normalized {
	case "free":
		return proto.AccountTier_TIER_FREE
	case "tier_free":
		return proto.AccountTier_TIER_FREE
	case "accounttier_tier_free":
		return proto.AccountTier_TIER_FREE
	case "standard":
		return proto.AccountTier_TIER_STANDARD
	case "tier_standard":
		return proto.AccountTier_TIER_STANDARD
	case "accounttier_tier_standard":
		return proto.AccountTier_TIER_STANDARD
	case "premium":
		return proto.AccountTier_TIER_PREMIUM
	case "tier_premium":
		return proto.AccountTier_TIER_PREMIUM
	case "accounttier_tier_premium":
		return proto.AccountTier_TIER_PREMIUM
	default:
		return proto.AccountTier_TIER_FREE
	}
}

// maxPeersForProtoTier returns the peer cap derived from a proto AccountTier.
// Replaces the old AccLevel/billing.Tier helpers (Phase 2: billing removed).
func maxPeersForProtoTier(tier proto.AccountTier) int {
	switch tier {
	case proto.AccountTier_TIER_STANDARD:
		return 29
	case proto.AccountTier_TIER_PREMIUM:
		return 232
	default:
		return 3
	}
}

// getCredentialCipher returns the encryption cipher for a tenant's credentials
func (s *TenantPortalServiceServer) getCredentialCipher(accountID string) (*crypto.CredentialCipher, error) {
	acc, err := s.server.GetAccount(accountID)
	if err != nil {
		return nil, fmt.Errorf("account not found: %w", err)
	}

	// Parse the base64 WireGuard private key
	privateKey, err := wgtypes.ParseKey(acc.PrivateKey)
	if err != nil {
		return nil, fmt.Errorf("invalid private key: %w", err)
	}

	// Create cipher with the 32-byte private key
	return crypto.NewCredentialCipher(privateKey[:])
}

// ============================================================================
// SHARED ACCESS HELPERS
// ============================================================================

// peerMatchesTags reports whether a peer carries at least one tag from the
// filter list. An empty filter matches all peers.
func peerMatchesTags(peer *server.PeerMetadata, tags []string) bool {
	if len(tags) == 0 {
		return true
	}
	for _, ft := range tags {
		for _, pt := range peer.Tags {
			if pt == ft {
				return true
			}
		}
	}
	return false
}

// checkTagAccess returns codes.NotFound if the caller holds a tag-filtered
// scope for tenantID and the peer does not satisfy the filter. It returns nil
// for owners, callers with no tag filter, and API-key / internal calls (no
// CallerContext). Returning NotFound (not PermissionDenied) prevents leaking
// the existence of peers outside a sharee's allowed tag set.
func checkTagAccess(ctx context.Context, peer *server.PeerMetadata, tenantID string) error {
	cc := auth.CallerContextFromContext(ctx)
	if cc == nil {
		return nil
	}
	scope := cc.ScopeFor(tenantID)
	if scope == nil || scope.IsOwner || len(scope.Tags) == 0 {
		return nil
	}
	if !peerMatchesTags(peer, scope.Tags) {
		return errs.NotFoundE("peer not found")
	}
	return nil
}

func resolveScopeForAccount(ctx context.Context, tenantID, accountID string) *auth.AccessScope {
	cc := auth.CallerContextFromContext(ctx)
	if cc == nil {
		return nil
	}
	if accountID != "" {
		if scope := cc.ScopeForAccount(accountID); scope != nil {
			return scope
		}
	}
	if tenantID != "" {
		return cc.ScopeFor(tenantID)
	}
	return nil
}

func callerTenantIDFromContext(ctx context.Context, reqTenantID string) string {
	if cc := auth.CallerContextFromContext(ctx); cc != nil && cc.TenantID != "" {
		return cc.TenantID
	}
	if tid := auth.CallerTenantID(ctx); tid != "" {
		return tid
	}
	return strings.TrimSpace(reqTenantID)
}

func (s *TenantPortalServiceServer) loadCallerContextForTenant(ctx context.Context, callerTenantID string, requireFull bool) *auth.CallerContext {
	if callerTenantID == "" || s.tenantRegistry == nil {
		return nil
	}

	cc, err := auth.ResolveCallerContextForTenant(
		ctx,
		s.tenantRegistry,
		s.server.GetRedisClient(),
		callerTenantID,
		requireFull,
	)
	if err != nil || cc == nil {
		if err != nil {
			log.Debug().
				Err(err).
				Str("caller_tenant_id", callerTenantID).
				Bool("require_full", requireFull).
				Msg("[svc] failed to resolve caller context via tenant metadata")
		}
		return nil
	}

	return cc
}

func (s *TenantPortalServiceServer) withResolvedCallerContext(ctx context.Context, reqTenantID string) context.Context {
	if cc := auth.CallerContextFromContext(ctx); cc != nil {
		if cc.ScopesHydrated || strings.TrimSpace(reqTenantID) == cc.TenantID {
			return ctx
		}
	}

	callerTenantID := callerTenantIDFromContext(ctx, reqTenantID)
	if callerTenantID == "" {
		return ctx
	}

	requireFull := callerTenantID != strings.TrimSpace(reqTenantID)
	cc := s.loadCallerContextForTenant(ctx, callerTenantID, requireFull)
	if cc == nil {
		return ctx
	}

	log.Debug().
		Str("caller_tenant_id", callerTenantID).
		Str("req_tenant_id", reqTenantID).
		Int("scope_count", len(cc.Scopes)).
		Msg("[svc] recovered CallerContext without middleware token")

	return auth.WithCallerContext(ctx, cc)
}

func (s *TenantPortalServiceServer) withHydratedCallerContext(ctx context.Context, reqTenantID string) context.Context {
	if cc := auth.CallerContextFromContext(ctx); cc != nil && cc.ScopesHydrated {
		return ctx
	}

	callerTenantID := callerTenantIDFromContext(ctx, reqTenantID)
	if callerTenantID == "" {
		return ctx
	}

	cc := s.loadCallerContextForTenant(ctx, callerTenantID, true)
	if cc == nil {
		return ctx
	}

	log.Debug().
		Str("caller_tenant_id", callerTenantID).
		Str("req_tenant_id", reqTenantID).
		Int("scope_count", len(cc.Scopes)).
		Msg("[svc] hydrated CallerContext")

	return auth.WithCallerContext(ctx, cc)
}

func routeScopeFromCallerContext(cc *auth.CallerContext, route *auth.RequestAccessRoute) *auth.AccessScope {
	if cc == nil || route == nil {
		return nil
	}
	if route.FocusedShareID != "" {
		if scope := cc.ScopeForShare(route.FocusedShareID); scope != nil {
			return scope
		}
	}
	if route.TargetTenantID != "" {
		if scope := cc.ScopeFor(route.TargetTenantID); scope != nil {
			return scope
		}
	}
	return nil
}

func (s *TenantPortalServiceServer) resolveTenantAccessScope(ctx context.Context, reqTenantID string, requireHydrated bool) (context.Context, *auth.AccessScope, error) {
	ctx = s.withResolvedCallerContext(ctx, reqTenantID)
	cc := auth.CallerContextFromContext(ctx)
	if cc == nil {
		return ctx, nil, nil
	}

	route := auth.RequestAccessRouteFromContext(ctx)
	needsHydrated := requireHydrated || (route != nil && route.Mode == auth.RequestAccessModeFocusedShared) || strings.TrimSpace(reqTenantID) != cc.TenantID
	if needsHydrated && !cc.ScopesHydrated {
		ctx = s.withHydratedCallerContext(ctx, reqTenantID)
		cc = auth.CallerContextFromContext(ctx)
	}
	if cc == nil {
		return ctx, nil, nil
	}

	if route != nil && route.Mode == auth.RequestAccessModeFocusedShared {
		if scope := routeScopeFromCallerContext(cc, route); scope != nil {
			return ctx, scope, nil
		}
		return ctx, nil, errs.PermissionDeniedE("access denied: shared scope not accessible")
	}

	targetTenantID := strings.TrimSpace(reqTenantID)
	if targetTenantID != "" && targetTenantID != cc.TenantID {
		if scope := cc.ScopeFor(targetTenantID); scope != nil {
			return ctx, scope, nil
		}
		return ctx, nil, errs.PermissionDeniedE("access denied: resource not accessible")
	}

	if scope := cc.ScopeFor(cc.TenantID); scope != nil {
		return ctx, scope, nil
	}

	return ctx, nil, nil
}

func (s *TenantPortalServiceServer) resolveTenantAccessAccount(ctx context.Context, reqTenantID string, requireHydrated bool) (context.Context, string, *auth.AccessScope, error) {
	ctx, scope, err := s.resolveTenantAccessScope(ctx, reqTenantID, requireHydrated)
	if err != nil {
		return ctx, "", nil, err
	}
	if scope != nil {
		if scope.AccountID == "" {
			return ctx, "", scope, errs.NotFoundE("account not found")
		}
		return ctx, scope.AccountID, scope, nil
	}

	overlayAccountID, err := s.getOverlayAccountID(reqTenantID)
	if err != nil {
		return ctx, "", nil, errs.NotFoundf("account not found: %v", err)
	}
	return ctx, overlayAccountID, nil, nil
}

func logSharedContextState(ctx context.Context, method, reqTenantID, peerID string) {
	ev := log.Debug().
		Str("method", method).
		Str("req_tenant_id", reqTenantID)
	if peerID != "" {
		ev = ev.Str("peer_id", peerID)
	}
	if route := auth.RequestAccessRouteFromContext(ctx); route != nil {
		ev = ev.
			Str("access_mode", string(route.Mode)).
			Str("route_request_tenant_id", route.RequestTenantID).
			Str("route_target_tenant_id", route.TargetTenantID).
			Str("route_focused_share_id", route.FocusedShareID)
	}

	cc := auth.CallerContextFromContext(ctx)
	if cc == nil {
		ev.Bool("has_caller_context", false).
			Msg("[svc] shared access context")
		return
	}

	sharedCount := 0
	for _, sc := range cc.Scopes {
		if !sc.IsOwner {
			sharedCount++
		}
	}

	ev.Bool("has_caller_context", true).
		Str("caller_tenant_id", cc.TenantID).
		Int("scope_count", len(cc.Scopes)).
		Int("shared_scope_count", sharedCount).
		Msg("[svc] shared access context")
}

func requireScopeRead(scope *auth.AccessScope, resource string) error {
	if scope != nil && !scope.Permissions.CanRead() {
		return errs.PermissionDeniedf("read access denied for shared %s", resource)
	}
	return nil
}

func requireScopeWrite(scope *auth.AccessScope, resource string) error {
	if scope != nil && !scope.Permissions.CanWrite() {
		return errs.PermissionDeniedf("write access denied for shared %s", resource)
	}
	return nil
}

func denySharedDelete(scope *auth.AccessScope, resource string) error {
	if scope != nil && !scope.IsOwner {
		return errs.PermissionDeniedf("delete is not allowed on shared %s", resource)
	}
	return nil
}

// enrichPeerSharedFlags sets IsShared, OwnerName, and ViewerCanWrite on a
// proto.Peer based on the caller's CallerContext scope for tenantID.
// No-op for API-key / internal callers (no CallerContext) and for owners.
func enrichPeerSharedFlags(ctx context.Context, peer *proto.Peer, tenantID string) {
	cc := auth.CallerContextFromContext(ctx)
	if cc == nil {
		return
	}
	scope := cc.ScopeFor(tenantID)
	if scope == nil {
		return
	}
	peer.ViewerCanWrite = scope.Permissions.CanWrite()
	if !scope.IsOwner {
		peer.IsShared = true
		peer.OwnerName = scope.OwnerName
	}
}

func enrichWinboxSessionSharedFlags(ctx context.Context, session *proto.WinboxSession, tenantID, accountID string) {
	if session == nil {
		return
	}
	scope := resolveScopeForAccount(ctx, tenantID, accountID)
	if scope == nil {
		return
	}
	session.ViewerCanWrite = scope.Permissions.CanWrite()
	if !scope.IsOwner {
		session.IsShared = true
		session.OwnerName = scope.OwnerName
	}
}

func enrichWebSSHSessionSharedFlags(ctx context.Context, session *proto.WebSSHSession, tenantID, accountID string) {
	if session == nil {
		return
	}
	scope := resolveScopeForAccount(ctx, tenantID, accountID)
	if scope == nil {
		return
	}
	session.ViewerCanWrite = scope.Permissions.CanWrite()
	if !scope.IsOwner {
		session.IsShared = true
		session.OwnerName = scope.OwnerName
	}
}

// ============================================================================
// TENANT PEER MANAGEMENT METHODS
// ============================================================================

// peerToProto converts a PeerMetadata to its proto representation, enriching
// with Redis-cached scan data and ExtendedStats when available.
func (s *TenantPortalServiceServer) peerToProto(peer *server.PeerMetadata) *proto.Peer {
	hasWinbox := peer.ScannedWinboxPort > 0
	discoveredPorts, fingerprint := s.getPeerScanData(peer)
	routerOSCandidate, routerOSReady, routerOSPort, routerOSTLS := routerOSPeerFlags(peer, fingerprint, discoveredPorts)

	protoPeer := &proto.Peer{
		Id:         peer.ID,
		Name:       peer.Name,
		AccountId:  peer.AccountID,
		PublicKey:  peer.ID,
		AssignedIp: peer.AssignedIP,
		IsOnline:   peer.IsOnline,
		RxBytes:    peer.RxBytes,
		TxBytes:    peer.TxBytes,
		HasWinbox:  hasWinbox,
		RouterIp:   strings.Split(peer.AssignedIP, "/")[0],
		AllowedIps: peer.AllowedIPs,

		CreatedAt:           proto.TimestampFromTime(peer.CreatedAt),
		SshActivities:       make([]*proto.PeerSSHActivity, len(peer.SSHActivities)),
		WinboxActivities:    make([]*proto.PeerWinboxActivity, len(peer.WinboxActivities)),
		DiscoveredPorts:     discoveredPorts,
		LastPortScan:        proto.TimestampFromTime(peer.LastPortScanTime),
		ScannedSshPort:      int32(peer.ScannedSSHPort),
		ScannedWinboxPort:   int32(peer.ScannedWinboxPort),
		Fingerprint:         fingerprint,
		NotificationEnabled: peer.NotificationEnabled,
		Tags:                peer.Tags,
		Endpoint:            peer.Endpoint,
		ClientType:          peer.ClientType,
		RouterosCandidate:   routerOSCandidate,
		RouterosApiReady:    routerOSReady,
		RouterosApiPort:     routerOSPort,
		RouterosApiTls:      routerOSTLS,
	}
	if !peer.FirstSeenOnline.IsZero() {
		protoPeer.FirstSeenOnline = proto.TimestampFromTime(peer.FirstSeenOnline)
	}
	if !peer.LastOnlineAt.IsZero() {
		protoPeer.LastOnlineAt = proto.TimestampFromTime(peer.LastOnlineAt)
	}
	if !peer.LastHandshakeTime.IsZero() {
		protoPeer.LastHandshake = proto.TimestampFromTime(peer.LastHandshakeTime)
	}
	if !peer.LastSeenAt.IsZero() {
		protoPeer.LastSeenAt = proto.TimestampFromTime(peer.LastSeenAt)
	} else if !peer.LastHandshakeTime.IsZero() {
		protoPeer.LastSeenAt = proto.TimestampFromTime(peer.LastHandshakeTime)
	}
	for j, sa := range peer.SSHActivities {
		protoPeer.SshActivities[j] = &proto.PeerSSHActivity{
			Timestamp:  proto.TimestampFromTime(sa.Timestamp),
			SessionId:  sa.SessionID,
			UserAgent:  sa.UserAgent,
			ClientIp:   sa.ClientIP,
			EndTime:    proto.TimestampFromTime(sa.EndTime),
			Username:   sa.Username,
			BytesSent:  sa.BytesSent,
			BytesRecv:  sa.BytesRecv,
			Commands:   make([]string, len(sa.Commands)),
			DurationMs: sa.DurationMs,
		}
		for k, cmd := range sa.Commands {
			protoPeer.SshActivities[j].Commands[k] = fmt.Sprintf("[%s] %s", cmd.Timestamp.Format(time.RFC3339), cmd.Command)
		}
	}
	for j, wa := range peer.WinboxActivities {
		protoPeer.WinboxActivities[j] = &proto.PeerWinboxActivity{
			Timestamp:   proto.TimestampFromTime(wa.Timestamp),
			SessionName: wa.SessionName,
			Username:    wa.Username,
			ClientIp:    wa.ClientIP,
			EndTime:     proto.TimestampFromTime(wa.EndTime),
			DurationMs:  wa.DurationMs,
			RomonMode:   wa.RomonMode,
		}
	}
	if len(peer.ExtendedStats) > 0 {
		var extendedStats map[string]any
		if err := json.Unmarshal(peer.ExtendedStats, &extendedStats); err == nil {
			protoPeer.ExtendedStats = extendedStats
		}
	} else if s.server != nil && s.server.GetRedisClient() != nil {
		redisCtx := context.Background()
		key := fmt.Sprintf("peer:%s", peer.ID)
		if val, err := s.server.GetRedisClient().Get(redisCtx, key).Bytes(); err == nil {
			var cached server.PeerMetadata
			if err := json.Unmarshal(val, &cached); err == nil && len(cached.ExtendedStats) > 0 {
				var extendedStats map[string]any
				if err := json.Unmarshal(cached.ExtendedStats, &extendedStats); err == nil {
					protoPeer.ExtendedStats = extendedStats
				}
			}
		}
	}
	return protoPeer
}

// listPeersForCaller aggregates peers from every accessible scope in the
// CallerContext, applying per-scope tag filters. Used by ListTenantPeers when
// the caller was authenticated via session token.
func (s *TenantPortalServiceServer) listPeersForCaller(cc *auth.CallerContext) (*proto.ListTenantPeersResponse, error) {
	var mu sync.Mutex
	var out []*proto.Peer
	var wg sync.WaitGroup

	for _, scope := range cc.Scopes {
		if !scope.Permissions.CanRead() {
			continue
		}
		wg.Add(1)
		go func(sc *auth.AccessScope) {
			defer wg.Done()
			peers, err := s.server.ListPeers(sc.AccountID)
			if err != nil {
				log.Warn().Err(err).Str("account_id", sc.AccountID).Bool("is_owner", sc.IsOwner).Msg("listPeersForCaller: failed to list peers for scope")
				return
			}
			log.Debug().Str("account_id", sc.AccountID).Bool("is_owner", sc.IsOwner).Int("count", len(peers)).Msg("listPeersForCaller: scope peers loaded")
			added := 0
			for _, p := range peers {
				if !peerMatchesTags(p, sc.Tags) {
					continue
				}
				pbPeer := s.peerToProto(p)
				pbPeer.ViewerCanWrite = sc.Permissions.CanWrite()
				if !sc.IsOwner {
					pbPeer.IsShared = true
					pbPeer.OwnerName = sc.OwnerName
				}
				mu.Lock()
				out = append(out, pbPeer)
				mu.Unlock()
				added++
			}
			log.Debug().Str("account_id", sc.AccountID).Bool("is_owner", sc.IsOwner).Int("added", added).Msg("listPeersForCaller: scope done")
		}(scope)
	}
	wg.Wait()

	return &proto.ListTenantPeersResponse{Peers: out}, nil
}

// ListTenantPeers lists all peers accessible to the caller.
// When the request carries a session token the middleware attaches a
// CallerContext.
//   - req.TenantId == caller's own tenant ID (or empty): aggregate all accessible
//     scopes so the UI can show own + shared peers with badges in one view.
//   - req.TenantId == a shared scope's tenant ID: return only that scope's peers,
//     all marked IsShared=true. This is the "switch to shared account" UX path.
//
// API-key / internal callers fall back to the original single-account path.
func (s *TenantPortalServiceServer) ListTenantPeers(ctx context.Context, req *proto.ListTenantPeersRequest) (*proto.ListTenantPeersResponse, error) {
	if req.TenantId == "" {
		return nil, errs.InvalidArgumentE("tenant_id required")
	}
	ctx = s.withHydratedCallerContext(ctx, req.TenantId)
	logSharedContextState(ctx, "ListTenantPeers", req.TenantId, "")

	// Session-authenticated callers.
	if cc := auth.CallerContextFromContext(ctx); cc != nil {
		sharedCount := 0
		for _, sc := range cc.Scopes {
			if !sc.IsOwner {
				sharedCount++
			}
		}
		log.Debug().Str("caller", cc.TenantID).Str("req_tenant", req.TenantId).
			Int("total_scopes", len(cc.Scopes)).Int("shared_scopes", sharedCount).
			Msg("[svc] ListTenantPeers: CallerContext present")
		if route := auth.RequestAccessRouteFromContext(ctx); route != nil && route.Mode == auth.RequestAccessModeFocusedShared {
			scope := routeScopeFromCallerContext(cc, route)
			if scope == nil {
				return nil, errs.PermissionDeniedE("access denied: resource not accessible")
			}
			log.Debug().
				Str("caller", cc.TenantID).
				Str("focused_share_id", route.FocusedShareID).
				Str("target_tenant", scope.TenantID).
				Msg("[svc] ListTenantPeers: focused shared view via route hint")
			return s.listPeersForCaller(&auth.CallerContext{
				TenantID:       cc.TenantID,
				Scopes:         []*auth.AccessScope{scope},
				ScopesHydrated: true,
			})
		}
		// Specific shared account requested → scope to just that account.
		if req.TenantId != cc.TenantID {
			scope := cc.ScopeFor(req.TenantId)
			if scope == nil {
				return nil, errs.PermissionDeniedE("access denied: resource not accessible")
			}
			log.Debug().Str("caller", cc.TenantID).Str("req_tenant", req.TenantId).Bool("is_owner", scope.IsOwner).Msg("[svc] ListTenantPeers: focused shared-account view")
			return s.listPeersForCaller(&auth.CallerContext{TenantID: cc.TenantID, Scopes: []*auth.AccessScope{scope}, ScopesHydrated: true})
		}
		// Own account → aggregate all accessible scopes (own + shared with badges).
		log.Debug().Str("caller", cc.TenantID).Int("shared_scopes", sharedCount).Msg("[svc] ListTenantPeers: aggregated own+shared view")
		return s.listPeersForCaller(cc)
	}

	// API-key / internal callers: original single-account path.
	log.Debug().Str("tenant_id", req.TenantId).Msg("[svc] ListTenantPeers: NO CallerContext — session token missing or invalid, using API-key/internal path")
	overlayAccountID, err := s.getOverlayAccountID(req.TenantId)
	if err != nil {
		log.Error().Err(err).Str("tenant_id", req.TenantId).Msg("Failed to resolve overlay account ID")
		return nil, errs.NotFoundf("account not found: %v", err)
	}

	peers, err := s.server.ListPeers(overlayAccountID)
	if err != nil {
		log.Error().Err(err).Str("tenant_id", req.TenantId).Str("overlay_account_id", overlayAccountID).Msg("Failed to list peers")
		return nil, errs.Internalf("failed to list peers: %v", err)
	}

	protoPeers := make([]*proto.Peer, len(peers))
	var wg sync.WaitGroup
	wg.Add(len(peers))
	for i, p := range peers {
		go func(idx int, peer *server.PeerMetadata) {
			defer wg.Done()
			protoPeers[idx] = s.peerToProto(peer)
		}(i, p)
	}
	wg.Wait()

	return &proto.ListTenantPeersResponse{Peers: protoPeers}, nil
}

// AddTenantPeer adds a new peer to the tenant's network.
//
// All limit enforcement (free-tier cap, block-capacity cap, IP assignment) is
// done atomically inside server.AddPeer under a per-account mutex.  This layer
// only validates the request, resolves the overlay account, and maps server
// errors to the appropriate gRPC status codes.
func (s *TenantPortalServiceServer) AddTenantPeer(ctx context.Context, req *proto.AddTenantPeerRequest) (*proto.AddTenantPeerResponse, error) {
	if req.TenantId == "" {
		return nil, errs.InvalidArgumentE("tenant_id required")
	}
	if req.Name == "" {
		return nil, errs.InvalidArgumentE("name required")
	}

	ctx, overlayAccountID, usedScope, err := s.resolveTenantAccessAccount(ctx, req.TenantId, true)
	if err != nil {
		return nil, err
	}
	resourceTenantID := req.TenantId
	if usedScope != nil {
		resourceTenantID = usedScope.TenantID
	}

	// Delegate to the server layer — this is the single authoritative place for:
	//   • effective peer limit enforcement (admin override first, otherwise plan default)
	//   • IP assignment (atomic, no duplicate IPs possible)
	//   • WireGuard registration + DB persistence (with rollback on failure)
	// Pass "" for IP so the server assigns the next available address under its lock.
	claimPublicKey := strings.TrimSpace(req.PublicKey)
	var peerInfo *server.PeerInfo
	var existingClaimPeer *server.PeerMetadata
	if claimPublicKey != "" {
		if existing, findErr := s.server.FindPeer(claimPublicKey); findErr == nil && existing != nil {
			if existing.AccountID != overlayAccountID {
				return nil, errs.AlreadyExistsE("this Wantastic device has already been claimed")
			}
			existingClaimPeer = existing
			peerInfo = &server.PeerInfo{
				Name:            existing.Name,
				PublicKey:       existing.WireGuardPublicKey,
				AllowedIPs:      existing.AllowedIPs,
				ServerPublicKey: s.server.GetServerPublicKey(overlayAccountID),
				ServerEndpoint:  s.server.GetServerEndpoint(),
			}
			if peerInfo.PublicKey == "" {
				peerInfo.PublicKey = existing.ID
			}
		} else {
			peerInfo, err = s.server.AddPeerWithKey(overlayAccountID, req.Name, "", claimPublicKey)
		}
	} else {
		peerInfo, err = s.server.AddPeer(overlayAccountID, req.Name, "")
	}
	if err != nil {
		log.Error().Err(err).
			Str("tenant_id", req.TenantId).
			Str("overlay_account_id", overlayAccountID).
			Str("name", req.Name).
			Msg("Failed to add peer")

		errLower := strings.ToLower(err.Error())
		if strings.Contains(errLower, "peer limit reached") {
			return nil, errs.FailedPreconditionf("peer limit reached for this account — contact support or change your plan to add more devices.")
		}
		return nil, errs.Internalf("failed to add peer: %v", err)
	}

	if claimPublicKey != "" && existingClaimPeer == nil {
		if peer, markErr := s.server.GetPeer(overlayAccountID, peerInfo.PublicKey); markErr == nil && peer != nil {
			peer.ClientType = "wantasticd"
			peer.IsWantasticd = true
			peer.PrivateKey = ""
			peer.WireGuardPrivateKey = ""
			if peer.CachedPortScanJSON == nil {
				claimMeta := map[string]interface{}{
					"fingerprint": map[string]interface{}{
						"vendor":      "Wantastic",
						"device_type": "Wantastic Agent",
					},
					"claimed_from_public_key_qr": true,
					"claimed_at":                 time.Now().UTC().Format(time.RFC3339),
				}
				if claimBytes, marshalErr := json.Marshal(claimMeta); marshalErr == nil {
					peer.CachedPortScanJSON = claimBytes
				}
			}
			if updateErr := s.server.UpdatePeer(peer); updateErr != nil {
				log.Warn().Err(updateErr).
					Str("tenant_id", req.TenantId).
					Str("peer_id", peerInfo.PublicKey).
					Msg("Failed to mark claimed peer as wantasticd")
			}
		} else {
			log.Warn().Err(markErr).
				Str("tenant_id", req.TenantId).
				Str("peer_id", peerInfo.PublicKey).
				Msg("Failed to load claimed peer for wantasticd metadata")
		}
	}

	// The server already embedded /32 in AllowedIPs; strip it for the address field.
	peerIP := strings.TrimSuffix(peerInfo.AllowedIPs[0], "/32")

	message := "Peer created successfully"
	if claimPublicKey != "" {
		message = "Wantastic device claimed successfully"
		if existingClaimPeer != nil {
			message = "Wantastic device is already claimed by this team"
		}
	}
	wgConfig := ""
	if claimPublicKey == "" {
		wgConfig, err = s.server.GetPeerConfig(overlayAccountID, peerInfo.PublicKey, s.wireguardEndpoint())
	}
	if claimPublicKey == "" && err != nil {
		log.Warn().
			Err(err).
			Str("tenant_id", req.TenantId).
			Str("overlay_account_id", overlayAccountID).
			Str("peer_id", peerInfo.PublicKey).
			Msg("Failed to build peer config after peer creation; trying direct fallback")

		wgConfig, err = s.buildFreshPeerConfig(overlayAccountID, peerIP, peerInfo, s.wireguardEndpoint())
		if err != nil {
			log.Warn().
				Err(err).
				Str("tenant_id", req.TenantId).
				Str("overlay_account_id", overlayAccountID).
				Str("peer_id", peerInfo.PublicKey).
				Msg("Peer created, but config generation fallback also failed")
			message = "Peer created successfully, but config generation is temporarily unavailable"
			wgConfig = ""
		}
	}

	protoPeer := &proto.Peer{
		Id:         peerInfo.PublicKey,
		Name:       peerInfo.Name,
		AccountId:  overlayAccountID,
		PublicKey:  peerInfo.PublicKey,
		AssignedIp: peerIP,
		IsOnline:   false,
		ClientType: func() string {
			if claimPublicKey != "" {
				return "wantasticd"
			}
			return ""
		}(),
	}
	enrichPeerSharedFlags(ctx, protoPeer, resourceTenantID)

	log.Debug().
		Str("tenant_id", req.TenantId).
		Str("peer_id", peerInfo.PublicKey).
		Str("peer_name", peerInfo.Name).
		Str("peer_ip", peerIP).
		Msg("Tenant added peer")

	return &proto.AddTenantPeerResponse{
		Success:    true,
		Message:    message,
		Peer:       protoPeer,
		PrivateKey: peerInfo.PrivateKey,
		Config:     wgConfig,
	}, nil
}

func (s *TenantPortalServiceServer) buildFreshPeerConfig(accountID, peerIP string, peerInfo *server.PeerInfo, endpointOverride string) (string, error) {
	if peerInfo == nil {
		return "", fmt.Errorf("peer info missing")
	}

	acc, err := s.server.GetAccount(accountID)
	if err != nil {
		return "", fmt.Errorf("get account for config fallback: %w", err)
	}

	device, err := s.server.GetTenantDevice(accountID)
	if err != nil {
		return "", fmt.Errorf("get tenant device for config fallback: %w", err)
	}

	address := strings.TrimSpace(peerIP)
	if address == "" && len(peerInfo.AllowedIPs) > 0 {
		address = strings.TrimSpace(peerInfo.AllowedIPs[0])
	}
	if address == "" {
		return "", fmt.Errorf("peer address missing")
	}
	if !strings.Contains(address, "/") {
		address += "/32"
	}

	allowedIPs, err := server.WireGuardAllowedIPs(acc.Networks)
	if err != nil {
		return "", fmt.Errorf("derive tenant routes for config fallback: %w", err)
	}

	endpoint := strings.TrimSpace(endpointOverride)
	if endpoint == "" {
		endpoint = strings.TrimSpace(peerInfo.ServerEndpoint)
	}
	if endpoint == "" {
		endpoint = strings.TrimSpace(s.server.GetServerEndpoint())
	}
	if endpoint == "" {
		endpoint = "localhost"
	}
	endpoint = formatWireGuardPeerEndpoint(endpoint, device.GetEndpointPort())

	return server.BuildWireGuardConfig(server.WireGuardConfigOptions{
		PrivateKey:          peerInfo.PrivateKey,
		Address:             address,
		ServerPublicKey:     peerInfo.ServerPublicKey,
		Endpoint:            endpoint,
		AllowedIPs:          allowedIPs,
		DNSServers:          server.WireGuardDNSServers(device.DeviceIP.String()),
		PersistentKeepalive: 25,
	}), nil
}

func formatWireGuardPeerEndpoint(endpoint string, listenPort int) string {
	if endpoint == "" {
		return ""
	}
	if _, _, err := net.SplitHostPort(endpoint); err == nil || listenPort <= 0 {
		return endpoint
	}
	if strings.Count(endpoint, ":") > 1 && !strings.HasPrefix(endpoint, "[") {
		return fmt.Sprintf("[%s]:%d", endpoint, listenPort)
	}
	return fmt.Sprintf("%s:%d", endpoint, listenPort)
}

// RemoveTenantPeer removes a peer from the tenant's network
func (s *TenantPortalServiceServer) RemoveTenantPeer(ctx context.Context, req *proto.RemoveTenantPeerRequest) (*proto.RemoveTenantPeerResponse, error) {
	if req.TenantId == "" {
		return nil, errs.InvalidArgumentE("tenant_id required")
	}
	if req.PeerId == "" {
		return nil, errs.InvalidArgumentE("peer_id required")
	}
	ctx = s.withResolvedCallerContext(ctx, req.TenantId)

	ctx, overlayAccountID, peer, usedScope, err := s.resolveAccountForPeer(ctx, req.TenantId, req.PeerId)
	if err != nil {
		return nil, errs.NotFoundf("peer not found: %v", err)
	}
	effectiveTenantID := req.TenantId
	if usedScope != nil {
		effectiveTenantID = usedScope.TenantID
	}
	if err := checkTagAccess(ctx, peer, effectiveTenantID); err != nil {
		return nil, err
	}
	if err := denySharedDelete(usedScope, "peers"); err != nil {
		return nil, err
	}

	// Remove peer
	if err := s.server.RemovePeer(overlayAccountID, req.PeerId); err != nil {
		log.Error().Err(err).Str("tenant_id", req.TenantId).Str("overlay_account_id", overlayAccountID).Str("peer_id", req.PeerId).Msg("Failed to remove peer")
		return nil, errs.Internalf("failed to remove peer: %v", err)
	}

	log.Debug().
		Str("tenant_id", req.TenantId).
		Str("overlay_account_id", overlayAccountID).
		Str("peer_id", req.PeerId).
		Msg(" Tenant removed peer")

	return &proto.RemoveTenantPeerResponse{
		Success: true,
		Message: "Peer removed successfully",
	}, nil
}

// UpdateTenantPeer updates a peer's settings
func (s *TenantPortalServiceServer) UpdateTenantPeer(ctx context.Context, req *proto.UpdateTenantPeerRequest) (*proto.UpdateTenantPeerResponse, error) {
	if req.TenantId == "" {
		return nil, errs.InvalidArgumentE("tenant_id required")
	}
	if req.PeerId == "" {
		return nil, errs.InvalidArgumentE("peer_id required")
	}
	ctx = s.withResolvedCallerContext(ctx, req.TenantId)

	ctx, _, peer, usedScope, err := s.resolveAccountForPeer(ctx, req.TenantId, req.PeerId)
	if err != nil {
		return nil, errs.NotFoundf("peer not found: %v", err)
	}
	effectiveTenantID := req.TenantId
	if usedScope != nil {
		effectiveTenantID = usedScope.TenantID
	}
	if err := checkTagAccess(ctx, peer, effectiveTenantID); err != nil {
		return nil, err
	}

	// Track if we need to save
	needsSave := false

	// Update peer name if provided
	if req.Name != "" {
		peer.Name = req.Name
		needsSave = true
	}

	// Update peer tags if provided
	if req.Tags != nil {
		peer.Tags = req.Tags
		needsSave = true
	}

	// Save peer if any changes were made
	if needsSave {
		peer.UpdatedAt = time.Now().UTC()
		if err := s.server.GetPeerStore().SavePeer(peer); err != nil {
			return nil, errs.Internalf("failed to update peer: %v", err)
		}
	}

	// Determine has_winbox from port scan result
	hasWinbox := peer.ScannedWinboxPort > 0

	// Parse discovered ports and fingerprint from Redis or cached DB
	discoveredPorts, fingerprint := s.getPeerScanData(peer)
	routerOSCandidate, routerOSReady, routerOSPort, routerOSTLS := routerOSPeerFlags(peer, fingerprint, discoveredPorts)

	protoPeer := &proto.Peer{
		Id:         peer.ID,
		Name:       peer.Name,
		AccountId:  peer.AccountID,
		PublicKey:  peer.ID, // ID is the public key in PeerMetadata
		AssignedIp: peer.AssignedIP,
		IsOnline:   peer.IsOnline,
		HasWinbox:  hasWinbox,
		RouterIp:   strings.Split(peer.AssignedIP, "/")[0],
		// Port discovery fields
		DiscoveredPorts:   discoveredPorts,
		LastPortScan:      proto.TimestampFromTime(peer.LastPortScanTime),
		ScannedSshPort:    int32(peer.ScannedSSHPort),
		ScannedWinboxPort: int32(peer.ScannedWinboxPort),
		// OS fingerprint
		Fingerprint: fingerprint,
		// Tags
		Tags:              peer.Tags,
		ClientType:        peer.ClientType,
		RouterosCandidate: routerOSCandidate,
		RouterosApiReady:  routerOSReady,
		RouterosApiPort:   routerOSPort,
		RouterosApiTls:    routerOSTLS,
	}
	enrichPeerSharedFlags(ctx, protoPeer, effectiveTenantID)

	return &proto.UpdateTenantPeerResponse{
		Success: true,
		Message: "Peer updated successfully",
		Peer:    protoPeer,
	}, nil
}

// UpdatePeerNotes updates the markdown notes for a peer
func (s *TenantPortalServiceServer) UpdatePeerNotes(ctx context.Context, req *proto.UpdatePeerNotesRequest) (*proto.UpdatePeerNotesResponse, error) {
	if req.AccountId == "" {
		return nil, errs.InvalidArgumentE("account_id required")
	}
	if req.PeerId == "" {
		return nil, errs.InvalidArgumentE("peer_id required")
	}

	// Resolve tenant ID to overlay account ID
	overlayAccountID, err := s.getOverlayAccountID(req.AccountId)
	if err != nil {
		log.Error().Err(err).Str("tenant_id", req.AccountId).Msg("Failed to resolve overlay account ID")
		return nil, errs.NotFoundf("account not found: %v", err)
	}

	// Verify peer belongs to tenant
	peer, err := s.server.GetPeer(overlayAccountID, req.PeerId)
	if err != nil {
		return nil, errs.NotFoundf("peer not found: %v", err)
	}
	if peer.AccountID != overlayAccountID {
		return nil, errs.PermissionDeniedE("peer does not belong to this tenant")
	}

	// Update notes in the store
	if err := s.server.GetPeerStore().UpdatePeerNotes(overlayAccountID, req.PeerId, req.Notes); err != nil {
		return nil, errs.Internalf("failed to update notes: %v", err)
	}

	log.Debug().
		Str("tenant_id", req.AccountId).
		Str("peer_id", req.PeerId).
		Msg(" Peer notes updated")

	return &proto.UpdatePeerNotesResponse{
		Peer: &proto.Peer{
			Id:    req.PeerId,
			Notes: req.Notes,
		},
	}, nil
}

// BatchUpdatePeers handles bulk operations on peers
func (s *TenantPortalServiceServer) BatchUpdatePeers(ctx context.Context, req *proto.BatchUpdatePeersRequest) (*proto.BatchUpdatePeersResponse, error) {
	if req.TenantId == "" {
		return nil, errs.InvalidArgumentE("tenant_id required")
	}
	if len(req.PeerIds) == 0 {
		return nil, errs.InvalidArgumentE("peer_ids required")
	}
	ctx, overlayAccountID, usedScope, err := s.resolveTenantAccessAccount(ctx, req.TenantId, true)
	if err != nil {
		return nil, err
	}
	resourceTenantID := req.TenantId
	if usedScope != nil {
		resourceTenantID = usedScope.TenantID
	}

	updatedCount := 0

	switch req.Operation {
	case proto.BatchUpdatePeersRequest_DELETE:
		if err := denySharedDelete(usedScope, "peers"); err != nil {
			return nil, err
		}
		// Delete peers loop (safest for WireGuard sync)
		for _, peerID := range req.PeerIds {
			// Verify peer belongs to tenant? RemovePeer already checks DB but optimized check might be better
			// For now, simple loop
			if err := s.server.RemovePeer(overlayAccountID, peerID); err != nil {
				// Log error but continue? Or stop?
				// Partial success is tricky. Let's continue and report count.
				log.Warn().Err(err).Str("peer_id", peerID).Msg("Failed to delete peer in batch")
				continue
			}
			updatedCount++
		}

	case proto.BatchUpdatePeersRequest_ADD_TAGS:
		// TODO: Implement efficient BatchAddTags in store
		// For now, loop Get -> Update -> Save
		for _, peerID := range req.PeerIds {
			peer, err := s.server.GetPeer(overlayAccountID, peerID)
			if err != nil {
				continue
			}
			if err := checkTagAccess(ctx, peer, resourceTenantID); err != nil {
				continue
			}

			// Add unique tags
			existingTags := make(map[string]bool)
			for _, t := range peer.Tags {
				existingTags[t] = true
			}
			changed := false
			for _, newTag := range req.Tags {
				if !existingTags[newTag] {
					peer.Tags = append(peer.Tags, newTag)
					existingTags[newTag] = true
					changed = true
				}
			}

			if changed {
				peer.UpdatedAt = time.Now().UTC()
				if err := s.server.GetPeerStore().SavePeer(peer); err == nil {
					updatedCount++
				}
			}
		}

	case proto.BatchUpdatePeersRequest_REMOVE_TAGS:
		// TODO: Implement efficient BatchRemoveTags in store
		for _, peerID := range req.PeerIds {
			peer, err := s.server.GetPeer(overlayAccountID, peerID)
			if err != nil {
				continue
			}
			if err := checkTagAccess(ctx, peer, resourceTenantID); err != nil {
				continue
			}

			// Remove tags
			newTags := []string{}
			toRemove := make(map[string]bool)
			for _, t := range req.Tags {
				toRemove[t] = true
			}

			changed := false
			for _, tag := range peer.Tags {
				if !toRemove[tag] {
					newTags = append(newTags, tag)
				} else {
					changed = true
				}
			}

			if changed {
				peer.Tags = newTags
				peer.UpdatedAt = time.Now().UTC()
				if err := s.server.GetPeerStore().SavePeer(peer); err == nil {
					updatedCount++
				}
			}
		}

	case proto.BatchUpdatePeersRequest_RENAME_SEQUENCE:
		// Rename with pattern
		// Need to sort peers? By created_at usually or current name?
		// The request peer_ids order is preserved? Assuming list is ordered.
		// Usually spreadsheet selection implies order.

		pattern := req.SequencePattern // e.g. "Device-###"
		start := int(req.SequenceStart)
		if strings.Count(pattern, "#") == 0 {
			pattern += "-###" // Default safety
		}

		for i, peerID := range req.PeerIds {
			peer, err := s.server.GetPeer(overlayAccountID, peerID)
			if err != nil {
				continue
			}
			if err := checkTagAccess(ctx, peer, resourceTenantID); err != nil {
				continue
			}

			// Generate name
			// Determine number of digits
			numDigits := strings.Count(pattern, "#")
			idx := start + i

			// Replace # sequence with formatted number
			numberStr := fmt.Sprintf("%0*d", numDigits, idx)
			// Simple replace of the # block
			// We find the first occurrence of #...#
			firstHash := strings.Index(pattern, "#")
			lastHash := strings.LastIndex(pattern, "#")

			// Reconstruct pattern
			prefix := pattern[:firstHash]
			suffix := pattern[lastHash+1:]
			newName := prefix + numberStr + suffix

			peer.Name = newName
			peer.UpdatedAt = time.Now().UTC()
			if err := s.server.GetPeerStore().SavePeer(peer); err == nil {
				updatedCount++
			}
		}

	default:
		return nil, errs.InvalidArgumentE("invalid operation")
	}

	return &proto.BatchUpdatePeersResponse{
		Success:      true,
		Message:      fmt.Sprintf("Processed %d peers", updatedCount),
		UpdatedCount: int32(updatedCount),
	}, nil
}

// SetPeerNotification enables or disables offline notifications for a peer
func (s *TenantPortalServiceServer) SetPeerNotification(ctx context.Context, req *proto.SetPeerNotificationRequest) (*proto.SetPeerNotificationResponse, error) {
	if req.TenantId == "" {
		return nil, errs.InvalidArgumentE("tenant_id required")
	}
	if req.PeerId == "" {
		return nil, errs.InvalidArgumentE("peer_id required")
	}
	ctx = s.withResolvedCallerContext(ctx, req.TenantId)

	ctx, overlayAccountID, peer, usedScope, err := s.resolveAccountForPeer(ctx, req.TenantId, req.PeerId)
	if err != nil {
		return nil, errs.NotFoundf("peer not found: %v", err)
	}
	effectiveTenantID := req.TenantId
	if usedScope != nil {
		effectiveTenantID = usedScope.TenantID
	}
	if err := checkTagAccess(ctx, peer, effectiveTenantID); err != nil {
		return nil, err
	}

	// Update notification setting
	peer.NotificationEnabled = req.Enabled
	peer.UpdatedAt = time.Now().UTC()

	// If enabling notifications for the first time, reset notification state
	if req.Enabled {
		peer.OfflineNotificationState = "none"
	}

	if err := s.server.GetPeerStore().SavePeer(peer); err != nil {
		return nil, errs.Internalf("failed to update peer: %v", err)
	}

	// Notify the notification manager about the change
	if s.notificationManager != nil {
		s.notificationManager.OnPeerNotificationToggle(effectiveTenantID, overlayAccountID, req.Enabled)
	}

	action := "disabled"
	if req.Enabled {
		action = "enabled"
	}

	log.Debug().
		Str("tenant_id", req.TenantId).
		Str("peer_id", req.PeerId).
		Str("peer_name", peer.Name).
		Bool("notification_enabled", req.Enabled).
		Msg(" Peer notification setting updated")

	return &proto.SetPeerNotificationResponse{
		Success:             true,
		Message:             fmt.Sprintf("Notifications %s for peer %s", action, peer.Name),
		NotificationEnabled: req.Enabled,
	}, nil
}

// DisableAllPeerNotifications disables offline notifications for all peers of a tenant.
// This is used by the unsubscribe link in notification emails.
func (s *TenantPortalServiceServer) DisableAllPeerNotifications(ctx context.Context, req *proto.DisableAllPeerNotificationsRequest) (*proto.DisableAllPeerNotificationsResponse, error) {
	if req.TenantId == "" {
		return nil, errs.InvalidArgumentE("tenant_id required")
	}

	ctx, overlayAccountID, usedScope, err := s.resolveTenantAccessAccount(ctx, req.TenantId, true)
	if err != nil {
		return nil, err
	}
	resourceTenantID := req.TenantId
	if usedScope != nil {
		resourceTenantID = usedScope.TenantID
	}

	// Get all peers for this tenant
	peers, err := s.server.GetPeerStore().ListPeers(overlayAccountID)
	if err != nil {
		log.Error().Err(err).Str("tenant_id", req.TenantId).Msg("Failed to list peers")
		return nil, errs.Internalf("failed to list peers: %v", err)
	}

	// Disable notifications for all peers that have them enabled
	disabledCount := 0
	now := time.Now().UTC()

	for _, peer := range peers {
		if err := checkTagAccess(ctx, peer, resourceTenantID); err != nil {
			continue
		}
		if peer.NotificationEnabled {
			peer.NotificationEnabled = false
			peer.UpdatedAt = now

			if err := s.server.GetPeerStore().SavePeer(peer); err != nil {
				log.Warn().Err(err).Str("peer_id", peer.ID).Msg("Failed to disable notification for peer")
				continue
			}
			disabledCount++
		}
	}

	// Notify the notification manager to stop the worker if no peers have notifications enabled
	if s.notificationManager != nil && disabledCount > 0 {
		s.notificationManager.OnPeerNotificationToggle(resourceTenantID, overlayAccountID, false)
	}

	log.Debug().
		Str("tenant_id", req.TenantId).
		Int("disabled_count", disabledCount).
		Int("total_peers", len(peers)).
		Msg("🔕 All peer notifications disabled via unsubscribe link")

	return &proto.DisableAllPeerNotificationsResponse{
		Success:       true,
		Message:       fmt.Sprintf("Disabled notifications for %d peer(s)", disabledCount),
		DisabledCount: int32(disabledCount),
	}, nil
}

// GetTenantTopology returns the network topology for a tenant
func (s *TenantPortalServiceServer) GetTenantTopology(ctx context.Context, req *proto.GetTenantTopologyRequest) (*proto.GetTenantTopologyResponse, error) {
	if req.TenantId == "" {
		return nil, errs.InvalidArgumentE("tenant_id required")
	}
	ctx, overlayAccountID, usedScope, err := s.resolveTenantAccessAccount(ctx, req.TenantId, true)
	if err != nil {
		return nil, err
	}
	resourceTenantID := req.TenantId
	if usedScope != nil {
		resourceTenantID = usedScope.TenantID
	}

	// Get account info
	acc, err := s.server.GetAccount(overlayAccountID)
	if err != nil {
		return nil, errs.NotFoundf("overlay account not found: %v", err)
	}

	// Get peers
	peers, err := s.server.GetPeerStore().ListPeers(overlayAccountID)
	if err != nil {
		return nil, errs.Internalf("failed to list peers: %v", err)
	}

	// Determine tag filter from CallerContext for sharees.
	var topoScopeTags []string
	if usedScope != nil && !usedScope.IsOwner {
		topoScopeTags = usedScope.Tags
	}

	nodes := make([]*proto.TopologyNode, 0, len(peers)+1)
	edges := make([]*proto.TopologyEdge, 0, len(peers))

	// Add server node
	serverNode := &proto.TopologyNode{
		Id:        "server",
		Label:     acc.Name + " Gateway",
		Type:      proto.NodeType_NODE_TYPE_SERVER,
		Status:    proto.NodeStatus_NODE_STATUS_ONLINE,
		Ip:        "wantastic.app", // Server's WireGuard IP
		AccountId: resourceTenantID,
	}
	nodes = append(nodes, serverNode)

	// Add peer nodes and edges
	for _, p := range peers {
		// Skip peers outside the caller's tag-filtered scope.
		if !peerMatchesTags(p, topoScopeTags) {
			continue
		}
		status := proto.NodeStatus_NODE_STATUS_OFFLINE
		if p.IsOnline {
			status = proto.NodeStatus_NODE_STATUS_ONLINE
		}
		peerNode := &proto.TopologyNode{
			Id:        p.ID,
			Label:     p.Name,
			Type:      proto.NodeType_NODE_TYPE_PEER,
			Status:    status,
			Ip:        p.AssignedIP,
			AccountId: resourceTenantID,
			PublicKey: p.ID,
			RxBytes:   p.RxBytes,
			TxBytes:   p.TxBytes,
			HasWinbox: p.HasWinbox,
			Groups:    s.server.TopologyPeerLabels(overlayAccountID, p),
			Metadata: map[string]string{
				"client_type":    p.ClientType,
				"is_wantasticd":  strconv.FormatBool(p.IsWantasticd || strings.EqualFold(p.ClientType, "wantasticd")),
				"policy_surface": "selector",
			},
		}
		if !p.LastHandshakeTime.IsZero() {
			peerNode.LastHandshake = proto.TimestampFromTime(p.LastHandshakeTime)
		}

		// Add fingerprint
		// Add fingerprint
		_, fingerprint := s.getPeerScanData(p)
		if fingerprint != nil {
			peerNode.Fingerprint = fingerprint
		}

		nodes = append(nodes, peerNode)

		// Edge from server to peer
		edge := &proto.TopologyEdge{
			Source: "server",
			Target: p.ID,
			Active: p.IsOnline,
		}
		edges = append(edges, edge)
	}

	return &proto.GetTenantTopologyResponse{
		Nodes: nodes,
		Edges: edges,
	}, nil
}

// AssignExitNode assigns an exit node for a peer (P2P mode)
func (s *TenantPortalServiceServer) AssignExitNode(ctx context.Context, req *proto.AssignExitNodeRequest) (*proto.AssignExitNodeResponse, error) {
	if req.AccountId == "" {
		return nil, errs.InvalidArgumentE("account_id required")
	}
	if req.EntryNodeId == "" {
		return nil, errs.InvalidArgumentE("entry_node_id required")
	}
	if req.ExitNodeId == "" {
		return nil, errs.InvalidArgumentE("exit_node_id required")
	}

	ctx, overlayAccountID, usedScope, err := s.resolveTenantAccessAccount(ctx, req.AccountId, true)
	if err != nil {
		return nil, err
	}
	if err := requireScopeWrite(usedScope, "exit node assignments"); err != nil {
		return nil, err
	}
	resourceTenantID := req.AccountId
	if usedScope != nil {
		resourceTenantID = usedScope.TenantID
	}

	// Verify exit peer belongs to tenant and exists
	exitPeer, err := s.server.GetPeer(overlayAccountID, req.ExitNodeId)
	if err != nil {
		return nil, errs.NotFoundf("exit peer not found: %v", err)
	}
	if err := checkTagAccess(ctx, exitPeer, resourceTenantID); err != nil {
		return nil, err
	}

	// Get the tenant device
	td, err := s.server.GetTenantDevice(overlayAccountID)
	if err != nil || td == nil {
		return nil, errs.Internalf("failed to get tenant device: %v", err)
	}
	if td.Device == nil {
		return nil, errs.Internalf("tenant wireguard device not active")
	}

	// Prevent operations where the server (tenant device) is specified as entry or exit node
	tdPubKey := td.PublicKey.String()
	if req.ExitNodeId == tdPubKey {
		return nil, errs.InvalidArgumentf("tenant device cannot be assigned as an exit node")
	}
	if req.EntryNodeId == tdPubKey {
		return nil, errs.InvalidArgumentf("tenant device cannot be assigned as an entry node")
	}

	// If entry node is empty, just tell the exit node to enable exit node mode
	if req.EntryNodeId == "" {
		exitKeyBytes, err := base64.StdEncoding.DecodeString(req.ExitNodeId)
		if err != nil {
			return nil, errs.InvalidArgumentf("invalid base64 for exit_node_id: %v", err)
		}
		var exitPk device.NoisePublicKey
		copy(exitPk[:], exitKeyBytes)

		wgExitPeer := td.Device.LookupPeer(exitPk)
		if wgExitPeer == nil {
			return nil, errs.NotFoundf("exit peer not actively connected")
		}

		payload := `{"action":"enable_exit_node"}`
		wgExitPeer.SendTUNControl([]byte(payload))

		log.Info().
			Str("tenant_id", req.AccountId).
			Str("exit_node", req.ExitNodeId).
			Msg("Dispatched Enable Exit Node TUN control")

		return &proto.AssignExitNodeResponse{
			Success: true,
			Message: "Exit node enabled",
		}, nil
	}

	// If entry node is provided, verify it
	_, err = s.server.GetPeer(overlayAccountID, req.EntryNodeId)
	if err != nil {
		return nil, errs.NotFoundf("entry peer not found: %v", err)
	}
	entryPeer, err := s.server.GetPeer(overlayAccountID, req.EntryNodeId)
	if err != nil {
		return nil, errs.NotFoundf("entry peer not found: %v", err)
	}
	if err := checkTagAccess(ctx, entryPeer, resourceTenantID); err != nil {
		return nil, err
	}

	// Get active WireGuard Peer reference for the entry node
	keyBytes, err := base64.StdEncoding.DecodeString(req.EntryNodeId)
	if err != nil {
		return nil, errs.InvalidArgumentf("invalid base64 for entry_node_id: %v", err)
	}
	var pk device.NoisePublicKey
	copy(pk[:], keyBytes)

	wgPeer := td.Device.LookupPeer(pk)
	if wgPeer == nil {
		// Peer is offline or not actively tracked in userspace device
		return nil, errs.NotFoundf("entry peer not actively connected")
	}

	// Construct JSON payload
	exitIP := ""
	if len(exitPeer.AllowedIPs) > 0 {
		exitIP = strings.Split(exitPeer.AllowedIPs[0], "/")[0]
	}

	payload := fmt.Sprintf(`{"action":"set_exit_node","exit_node_public_key":"%s","exit_node_endpoint":"%s","exit_node_ip":"%s"}`,
		req.ExitNodeId, exitPeer.Endpoint, exitIP)

	// Dispatch TUN control message to entry node
	wgPeer.SendTUNControl([]byte(payload))

	log.Info().
		Str("tenant_id", req.AccountId).
		Str("entry_node", req.EntryNodeId).
		Str("exit_node", req.ExitNodeId).
		Msg("Dispatched AssignExitNode TUN control")

	return &proto.AssignExitNodeResponse{
		Success: true,
		Message: "Exit node assignment dispatched",
	}, nil
}

// GetTenantPeerConfig gets WireGuard configuration for a tenant's peer
func (s *TenantPortalServiceServer) GetTenantPeerConfig(ctx context.Context, req *proto.GetTenantPeerConfigRequest) (*proto.GetTenantPeerConfigResponse, error) {
	if req.TenantId == "" {
		return nil, errs.InvalidArgumentE("tenant_id required")
	}
	if req.PeerId == "" {
		return nil, errs.InvalidArgumentE("peer_id required")
	}
	ctx = s.withResolvedCallerContext(ctx, req.TenantId)
	logSharedContextState(ctx, "GetTenantPeerConfig", req.TenantId, req.PeerId)

	// Resolve the peer — searches CallerContext scopes if not in req.TenantId's account.
	ctx, overlayAccountID, peer, usedScope, err := s.resolveAccountForPeer(ctx, req.TenantId, req.PeerId)
	if err != nil {
		return nil, errs.NotFoundf("peer not found: %v", err)
	}
	effectiveTenantID := req.TenantId
	if usedScope != nil {
		effectiveTenantID = usedScope.TenantID
	}
	if err := checkTagAccess(ctx, peer, effectiveTenantID); err != nil {
		return nil, err
	}

	// Use configured WireGuard endpoint if not provided
	endpoint := req.Endpoint
	if endpoint == "" {
		// Use the configured WireGuard server endpoint
		endpoint = s.config.Endpoints.WireguardServer
		if endpoint == "" {
			// Fallback to server endpoint if not explicitly configured
			endpoint = s.server.GetServerEndpoint()
		}
	}

	// Get the config
	config, err := s.server.GetPeerConfig(overlayAccountID, req.PeerId, endpoint)
	if err != nil {
		return nil, errs.Internalf("failed to get peer config: %v", err)
	}

	// Generate QR code
	qrCode, err := generateWireGuardQRCode(config)
	if err != nil {
		qrCode = "" // QR is optional
	}

	// Generate setup token for automated install (if enabled)
	// DEPRECATED: Setup tokens should be explicitly created/selected from the UI for security/traceability
	var setupToken string

	return &proto.GetTenantPeerConfigResponse{
		WgConfig:   config,
		QrCode:     qrCode,
		SetupToken: setupToken,
	}, nil
}

// generateWireGuardQRCode generates a base64-encoded QR code PNG for WireGuard config
func generateWireGuardQRCode(config string) (string, error) {
	return generateQRCodePNGBase64(config)
}

func generateQRCodePNGBase64(content string) (string, error) {
	qrBytes, err := qrcode.Encode(content, qrcode.Medium, 256)
	if err != nil {
		return "", fmt.Errorf("failed to generate QR code: %w", err)
	}
	return base64.StdEncoding.EncodeToString(qrBytes), nil
}

// =============================================================================
// ADDITIONAL PEER METHODS (tenant-scoped)
// =============================================================================

// GetTenantPeer gets details for a specific peer
func (s *TenantPortalServiceServer) GetTenantPeer(ctx context.Context, req *proto.GetTenantPeerRequest) (*proto.GetTenantPeerResponse, error) {
	if req.TenantId == "" {
		return nil, errs.InvalidArgumentE("tenant_id required")
	}
	if req.PeerId == "" {
		return nil, errs.InvalidArgumentE("peer_id required")
	}
	ctx = s.withResolvedCallerContext(ctx, req.TenantId)
	logSharedContextState(ctx, "GetTenantPeer", req.TenantId, req.PeerId)

	// Resolve the peer — searches CallerContext scopes if not in req.TenantId's account.
	ctx, overlayAccountID, peer, usedScope, err := s.resolveAccountForPeer(ctx, req.TenantId, req.PeerId)
	if err != nil {
		return nil, errs.NotFoundf("peer not found: %v", err)
	}
	effectiveTenantID := req.TenantId
	if usedScope != nil {
		effectiveTenantID = usedScope.TenantID
	}
	if err := checkTagAccess(ctx, peer, effectiveTenantID); err != nil {
		return nil, err
	}

	// Update peer status from device (best effort)
	isOnline, _ := s.server.UpdatePeerStatus(overlayAccountID, req.PeerId)

	// Re-fetch peer to pick up the fresh LastHandshakeTime, Endpoint, and IsOnline
	// that UpdatePeerStatus just saved — the pre-loaded peer object is stale at this point.
	if freshPeer, ferr := s.server.GetPeer(overlayAccountID, req.PeerId); ferr == nil {
		peer = freshPeer
	}

	// Override online status if determined online by UpdatePeerStatus (e.g. valid Redis heartbeat but stale DB)
	if isOnline {
		peer.IsOnline = true
	}

	// Convert SSH activities to proto format
	sshActivities := make([]*proto.PeerSSHActivity, 0, len(peer.SSHActivities))
	for _, activity := range peer.SSHActivities {
		commands := make([]string, 0, len(activity.Commands))
		for _, cmd := range activity.Commands {
			commands = append(commands, cmd.Command)
		}
		sshActivities = append(sshActivities, &proto.PeerSSHActivity{
			SessionId:  activity.SessionID,
			UserAgent:  activity.UserAgent,
			ClientIp:   activity.ClientIP,
			Timestamp:  proto.TimestampFromTime(activity.Timestamp),
			EndTime:    proto.TimestampFromTime(activity.EndTime),
			Username:   activity.Username,
			Commands:   commands,
			BytesSent:  activity.BytesSent,
			BytesRecv:  activity.BytesRecv,
			DurationMs: activity.DurationMs,
		})
	}

	// Convert Winbox activities to proto format
	winboxActivities := make([]*proto.PeerWinboxActivity, 0, len(peer.WinboxActivities))
	for _, activity := range peer.WinboxActivities {
		winboxActivities = append(winboxActivities, &proto.PeerWinboxActivity{
			SessionName: activity.SessionName,
			Username:    activity.Username,
			ClientIp:    activity.ClientIP,
			Timestamp:   proto.TimestampFromTime(activity.Timestamp),
			EndTime:     proto.TimestampFromTime(activity.EndTime),
			DurationMs:  activity.DurationMs,
			RomonMode:   activity.RomonMode,
		})
	}

	// Determine has_winbox from port scan result (ScannedWinboxPort > 0)
	hasWinbox := peer.ScannedWinboxPort > 0

	// Parse discovered ports and fingerprint from Redis or DB
	discoveredPorts, fingerprint := s.getPeerScanData(peer)
	routerOSCandidate, routerOSReady, routerOSPort, routerOSTLS := routerOSPeerFlags(peer, fingerprint, discoveredPorts)

	resp := &proto.GetTenantPeerResponse{
		Peer: &proto.Peer{
			Id:               peer.ID,
			AccountId:        peer.AccountID,
			Name:             peer.Name,
			PublicKey:        peer.ID,
			AssignedIp:       peer.AssignedIP,
			AllowedIps:       peer.AllowedIPs,
			CreatedAt:        proto.TimestampFromTime(peer.CreatedAt),
			LastHandshake:    proto.TimestampFromTime(peer.LastHandshakeTime),
			IsOnline:         peer.IsOnline,
			RxBytes:          peer.RxBytes,
			TxBytes:          peer.TxBytes,
			LastSeenAt:       proto.TimestampFromTime(peer.LastSeenAt),
			HasWinbox:        hasWinbox,
			RouterIp:         strings.Split(peer.AssignedIP, "/")[0],
			SshActivities:    sshActivities,
			WinboxActivities: winboxActivities,

			// Port discovery fields
			DiscoveredPorts:   discoveredPorts,
			LastPortScan:      proto.TimestampFromTime(peer.LastPortScanTime),
			ScannedSshPort:    int32(peer.ScannedSSHPort),
			ScannedWinboxPort: int32(peer.ScannedWinboxPort),
			// OS fingerprint
			Fingerprint: fingerprint,
			// Tags
			Tags:              peer.Tags,
			Endpoint:          peer.Endpoint,
			ClientType:        peer.ClientType,
			RouterosCandidate: routerOSCandidate,
			RouterosApiReady:  routerOSReady,
			RouterosApiPort:   routerOSPort,
			RouterosApiTls:    routerOSTLS,
		},
	}

	// Populate ExtendedStats if available
	if len(peer.ExtendedStats) > 0 {
		var extendedStats map[string]any
		if err := json.Unmarshal(peer.ExtendedStats, &extendedStats); err != nil {
			log.Warn().Err(err).Str("peer_id", peer.ID).Msg("Failed to unmarshal extended_stats")
		} else {
			resp.Peer.ExtendedStats = extendedStats
		}
	}

	// Shared-access: mark the peer with ownership context so the frontend can
	// show the "shared" badge and hide write-only actions for sharees.
	enrichPeerSharedFlags(ctx, resp.Peer, effectiveTenantID)

	return resp, nil
}

// GetTenantPeerStats retrieves peer statistics
func (s *TenantPortalServiceServer) GetTenantPeerStats(ctx context.Context, req *proto.GetTenantPeerStatsRequest) (*proto.GetTenantPeerStatsResponse, error) {
	if req.TenantId == "" {
		return nil, errs.InvalidArgumentE("tenant_id required")
	}
	if req.PeerId == "" {
		return nil, errs.InvalidArgumentE("peer_id required")
	}
	ctx = s.withResolvedCallerContext(ctx, req.TenantId)

	// Resolve the peer — searches CallerContext scopes if not in req.TenantId's account.
	ctx, overlayAccountID, peer, usedScope, err := s.resolveAccountForPeer(ctx, req.TenantId, req.PeerId)
	if err != nil {
		return nil, errs.NotFoundf("peer not found: %v", err)
	}
	effectiveTenantID := req.TenantId
	if usedScope != nil {
		effectiveTenantID = usedScope.TenantID
	}
	if err := checkTagAccess(ctx, peer, effectiveTenantID); err != nil {
		return nil, err
	}

	// Log current cached state before update
	log.Debug().
		Str("peer_id", req.PeerId).
		Int("cached_json_len", len(peer.CachedPortScanJSON)).
		Time("last_port_scan", peer.LastPortScanTime).
		Msg("GetTenantPeerStats: Before UpdatePeerStatus")

	// Update peer status from device
	var isOnline bool
	if isOnline, err = s.server.UpdatePeerStatus(overlayAccountID, req.PeerId); err != nil {
		return nil, errs.Internalf("failed to update peer status: %v", err)
	}

	// Get updated peer metadata
	peer, err = s.server.GetPeer(overlayAccountID, req.PeerId)
	if err != nil {
		return nil, errs.NotFoundf("peer not found: %v", err)
	}

	// Override online status if determined online (fixing stale DB read)
	if isOnline {
		peer.IsOnline = true
	}

	// Retrieve latest port scan result from Redis or DB
	openPorts, fingerprint := s.getPeerScanData(peer)

	if openPorts != nil {
		log.Debug().
			Str("peer_id", req.PeerId).
			Int("total_ports", len(openPorts)).
			Msg("GetTenantPeerStats: Got scan result from Redis")
	} else {
		log.Debug().Str("peer_id", req.PeerId).Msg("GetTenantPeerStats: No scan result in Redis")
	}

	// Check if a scan is currently in progress
	scanInProgress := s.server.IsScanInProgress(overlayAccountID, req.PeerId)
	activeScanID := s.server.GetActiveScanID(overlayAccountID, req.PeerId)

	// Get handshake history (last 30 days) for uptime chart
	history, _ := s.server.GetHandshakeHistory(overlayAccountID, req.PeerId, time.Now().Add(-30*24*time.Hour))

	// Compress history into uint32 array (seconds since epoch)
	var compressedHistory []byte
	if len(history) > 0 {
		compressedHistory = make([]byte, len(history)*4)
		for i, h := range history {
			unixSec := uint32(h.Timestamp.Unix())
			binary.BigEndian.PutUint32(compressedHistory[i*4:], unixSec)
		}
	}

	return &proto.GetTenantPeerStatsResponse{
		Stats: &proto.PeerStats{
			PeerId:            req.PeerId,
			RxBytes:           peer.RxBytes,
			TxBytes:           peer.TxBytes,
			LastHandshake:     proto.TimestampFromTime(peer.LastHandshakeTime),
			IsOnline:          peer.IsOnline,
			OpenPorts:         openPorts,
			LastPortScan:      proto.TimestampFromTime(peer.LastPortScanTime),
			ScannedSshPort:    int32(peer.ScannedSSHPort),
			ScannedWinboxPort: int32(peer.ScannedWinboxPort),
			Fingerprint:       fingerprint,
			ScanInProgress:    scanInProgress,
			HandshakeHistory:  nil, // Deprecated, replaced by UptimeHistory
			UptimeHistory:     compressedHistory,
			ActiveScanId:      activeScanID,
		},
	}, nil
}

// PingTenantPeer pings a peer and collects statistics
func (s *TenantPortalServiceServer) PingTenantPeer(ctx context.Context, req *proto.PingTenantPeerRequest) (*proto.PingTenantPeerResponse, error) {
	if req.TenantId == "" {
		return nil, errs.InvalidArgumentE("tenant_id required")
	}
	if req.PeerId == "" {
		return nil, errs.InvalidArgumentE("peer_id required")
	}
	ctx = s.withResolvedCallerContext(ctx, req.TenantId)
	logSharedContextState(ctx, "PingTenantPeer", req.TenantId, req.PeerId)

	// Resolve the peer — searches CallerContext scopes if not in req.TenantId's account.
	ctx, overlayAccountID, peer, usedScope, err := s.resolveAccountForPeer(ctx, req.TenantId, req.PeerId)
	if err != nil {
		return nil, errs.NotFoundf("peer not found: %v", err)
	}
	// Tag-access check: use the scope's tenant ID when we found the peer via a shared scope.
	effectiveTenantID := req.TenantId
	if usedScope != nil {
		effectiveTenantID = usedScope.TenantID
	}
	if err := checkTagAccess(ctx, peer, effectiveTenantID); err != nil {
		return nil, err
	}

	// Set defaults
	count := int(req.Count)
	if count <= 0 {
		count = 10 // Default to 10 pings for better charting
	}
	timeoutMs := int(req.TimeoutMs)
	if timeoutMs <= 0 {
		timeoutMs = 1000
	}

	// Ping the peer
	result, err := s.server.PingPeer(overlayAccountID, req.PeerId, count, timeoutMs)
	if err != nil {
		return &proto.PingTenantPeerResponse{
			Success: false,
			Error:   err.Error(),
		}, nil
	}

	// Convert ping details to proto format
	pingDetails := make([]*proto.PingDetail, 0, len(result.Pings))
	for _, ping := range result.Pings {
		pingDetails = append(pingDetails, &proto.PingDetail{
			Sequence:  int32(ping.Sequence),
			RttMs:     float32(ping.RTTMs),
			Success:   ping.Success,
			Error:     ping.Error,
			Timestamp: ping.Timestamp.UnixMilli(),
		})
	}

	return &proto.PingTenantPeerResponse{
		PeerIp:            result.PeerIP,
		PacketsSent:       int32(result.PacketsSent),
		PacketsReceived:   int32(result.PacketsReceived),
		PacketLossPercent: float32(result.PacketLossPercent),
		MinRttMs:          float32(result.MinRTTMs),
		AvgRttMs:          float32(result.AvgRTTMs),
		MaxRttMs:          float32(result.MaxRTTMs),
		Success:           result.Success,
		Error:             result.Error,
		Pings:             pingDetails,
	}, nil
}

// StreamPingTenantPeer streams ICMP ping results in real-time.
// Each PingEvent arrives as soon as the ICMP reply (or timeout) completes.
func (s *TenantPortalServiceServer) StreamPingTenantPeer(req *proto.PingTenantPeerRequest, stream ServerStream[*proto.PingEvent]) error {
	if req.TenantId == "" || req.PeerId == "" {
		return errs.InvalidArgumentE("tenant_id and peer_id required")
	}
	ctx := stream.Context()
	ctx = s.withResolvedCallerContext(ctx, req.TenantId)

	_, overlayAccountID, peer, usedScope, err := s.resolveAccountForPeer(ctx, req.TenantId, req.PeerId)
	if err != nil {
		return errs.NotFoundf("peer not found: %v", err)
	}
	effectiveTenantID := req.TenantId
	if usedScope != nil {
		effectiveTenantID = usedScope.TenantID
	}
	if err := checkTagAccess(ctx, peer, effectiveTenantID); err != nil {
		return err
	}

	device, peerIP, err := s.server.ResolvePeerDevice(overlayAccountID, req.PeerId)
	if err != nil {
		return errs.NotFoundf("device not found: %v", err)
	}

	count := int(req.Count)
	if count <= 0 {
		count = 10
	}
	timeoutMs := int(req.TimeoutMs)
	if timeoutMs <= 0 {
		timeoutMs = 1000
	}

	result, err := device.StreamICMPPing(peerIP, count, timeoutMs, func(detail userspace.PingDetail) error {
		return stream.Send(&proto.PingEvent{
			Sequence: int32(detail.Sequence),
			RttMs:    float32(detail.RTTMs),
			Success:  detail.Success,
			Error:    detail.Error,
		})
	})
	if err != nil {
		return errs.Internalf("ping failed: %v", err)
	}

	// Final summary event
	return stream.Send(&proto.PingEvent{
		IsSummary:         true,
		PeerIp:            result.PeerIP,
		PacketsSent:       int32(result.PacketsSent),
		PacketsReceived:   int32(result.PacketsReceived),
		PacketLossPercent: float32(result.PacketLossPercent),
		MinRttMs:          float32(result.MinRTTMs),
		AvgRttMs:          float32(result.AvgRTTMs),
		MaxRttMs:          float32(result.MaxRTTMs),
	})
}

// ========== ENROLLMENT TOKENS ==========

// ListEnrollmentTokens returns all active enrollment tokens for a tenant
func (s *TenantPortalServiceServer) ListEnrollmentTokens(ctx context.Context, req *proto.ListEnrollmentTokensRequest) (*proto.ListEnrollmentTokensResponse, error) {
	if req.TenantId == "" {
		return nil, errs.InvalidArgumentE("tenant_id required")
	}

	tokens, err := s.tenantRegistry.ListEnrollmentTokens(req.TenantId)
	if err != nil {
		log.Error().Err(err).Str("tenant_id", req.TenantId).Msg("Failed to list enrollment tokens")
		return nil, errs.Internalf("failed to list tokens: %v", err)
	}

	pbTokens := make([]*proto.EnrollmentToken, len(tokens))
	for i, t := range tokens {
		pbTokens[i] = &proto.EnrollmentToken{
			Id:         t.ID,
			TenantId:   t.TenantID,
			Name:       t.Name,
			Token:      t.Token,
			MaxUses:    int32(t.MaxUses),
			UsageCount: int32(t.UsageCount),
			ExpiresAt:  proto.TimestampFromTime(t.ExpiresAt),
			CreatedAt:  proto.TimestampFromTime(t.CreatedAt),
			CreatedBy:  t.CreatedBy,
		}
	}

	return &proto.ListEnrollmentTokensResponse{
		Tokens: pbTokens,
	}, nil
}

// CreateEnrollmentToken generates a new secure enrollment token
func (s *TenantPortalServiceServer) CreateEnrollmentToken(ctx context.Context, req *proto.CreateEnrollmentTokenRequest) (*proto.CreateEnrollmentTokenResponse, error) {
	if req.TenantId == "" {
		return nil, errs.InvalidArgumentE("tenant_id required")
	}
	if req.Name == "" {
		return nil, errs.InvalidArgumentE("token name required")
	}

	if s.enrollmentCipher == nil {
		return nil, errs.FailedPreconditionE("enrollment cipher not configured")
	}

	// Generate the secure token string
	expiry := time.Duration(0)
	if req.ExpiresInDays > 0 {
		expiry = time.Duration(req.ExpiresInDays) * 24 * time.Hour
	}

	tokenID := uuid.New().String()
	tokenStr, err := s.enrollmentCipher.GenerateToken(tokenID, req.TenantId, expiry)
	if err != nil {
		log.Error().Err(err).Str("tenant_id", req.TenantId).Msg("Failed to generate enrollment token")
		return nil, errs.Internalf("failed to generate token: %v", err)
	}

	var expiresAt time.Time
	if expiry > 0 {
		expiresAt = time.Now().Add(expiry).UTC()
	}

	t := &tenant.EnrollmentToken{
		ID:         tokenID,
		TenantID:   req.TenantId,
		Name:       req.Name,
		Token:      tokenStr,
		MaxUses:    int(req.MaxUses),
		UsageCount: 0,
		ExpiresAt:  expiresAt,
		CreatedAt:  time.Now().UTC(),
	}

	if err := s.tenantRegistry.CreateEnrollmentToken(t); err != nil {
		log.Error().Err(err).Str("tenant_id", req.TenantId).Msg("Failed to save enrollment token")
		return nil, errs.Internalf("failed to save token: %v", err)
	}

	log.Debug().
		Str("tenant_id", req.TenantId).
		Str("token_id", t.ID).
		Str("name", t.Name).
		Msg("🔑 Enrollment token created")

	return &proto.CreateEnrollmentTokenResponse{
		Token: &proto.EnrollmentToken{
			Id:         t.ID,
			TenantId:   t.TenantID,
			Name:       t.Name,
			Token:      t.Token,
			MaxUses:    int32(t.MaxUses),
			UsageCount: int32(t.UsageCount),
			ExpiresAt:  proto.TimestampFromTime(t.ExpiresAt),
			CreatedAt:  proto.TimestampFromTime(t.CreatedAt),
			CreatedBy:  t.CreatedBy,
		},
	}, nil
}

// DeleteEnrollmentToken revokes an enrollment token
func (s *TenantPortalServiceServer) DeleteEnrollmentToken(ctx context.Context, req *proto.DeleteEnrollmentTokenRequest) (*proto.DeleteEnrollmentTokenResponse, error) {
	if req.TenantId == "" || req.TokenId == "" {
		return nil, errs.InvalidArgumentE("tenant_id and token_id required")
	}

	if err := s.tenantRegistry.DeleteEnrollmentToken(req.TenantId, req.TokenId); err != nil {
		log.Error().Err(err).Str("tenant_id", req.TenantId).Str("token_id", req.TokenId).Msg("Failed to delete enrollment token")
		return nil, errs.Internalf("failed to delete token: %v", err)
	}

	log.Debug().
		Str("tenant_id", req.TenantId).
		Str("token_id", req.TokenId).
		Msg("🗑️ Enrollment token revoked")

	return &proto.DeleteEnrollmentTokenResponse{
		Success: true,
		Message: "Token deleted successfully",
	}, nil
}

// BatchUpdateTenantPeers performs efficient bulk operations on peers
func (s *TenantPortalServiceServer) BatchUpdateTenantPeers(ctx context.Context, req *proto.BatchUpdatePeersRequest) (*proto.BatchUpdatePeersResponse, error) {
	// If tenant ID is not provided in request (e.g. from authenticated session),
	// this would usually be handled by interceptors filling context or request.
	// But here for simplicity we require explicit tenant_id or handle empty if context auth is used.
	// Assuming req.TenantId is passed or we extract it.
	// For this implementation, we follow existing patterns that expect TenantId in request if not strictly session-bound.
	// NOTE: UpdateTenantPeer uses req.TenantId.

	// Resolve tenant ID to overlay account ID
	// If req.TenantId is empty, we might be relying on auth middleware context,
	// but s.getOverlayAccountID requires explicit ID string.
	// Just proceed with provided ID.
	targetTenantID := req.TenantId
	// Ideally extract from context if empty, but for now strict.
	if targetTenantID == "" {
		// Try to fallback to auth context if present (optional enhancement)
		// but let's stick to required field for consistency with other methods.
		// return nil, errs.InvalidArgumentE("tenant_id required")
		// Actually, frontend sends empty string sometimes if using cookie??
		// Let's assume frontend logic passes it.
	}

	updatedCount := 0
	sequenceIndex := 0

	// Compile regexp for sequence replacement once
	var patternRe *regexp.Regexp
	if req.Operation == 2 { // RENAME_SEQUENCE
		patternRe = regexp.MustCompile(`#+`)
	}

	for _, peerID := range req.PeerIds {
		ctx, overlayAccountID, peer, usedScope, err := s.resolveAccountForPeer(ctx, targetTenantID, peerID)
		if err != nil {
			log.Warn().Str("peer_id", peerID).Msg("Skipping batch update for missing peer")
			continue
		}
		effectiveTenantID := targetTenantID
		if usedScope != nil {
			effectiveTenantID = usedScope.TenantID
		}
		if err := checkTagAccess(ctx, peer, effectiveTenantID); err != nil {
			log.Warn().Err(err).Str("peer_id", peerID).Msg("Skipping batch update for inaccessible peer")
			continue
		}

		// Perform Operation
		switch req.Operation {
		case 1: // DELETE
			if err := denySharedDelete(usedScope, "peers"); err != nil {
				return nil, err
			}
			if err := s.server.RemovePeer(overlayAccountID, peerID); err != nil {
				log.Error().Err(err).Str("peer_id", peerID).Msg("Failed to remove peer in batch")
				continue
			}
			updatedCount++

		case 2: // RENAME_SEQUENCE
			if req.SequencePattern == "" {
				continue
			}
			// Calculate new name
			currentIndex := int(req.SequenceStart) + sequenceIndex
			newName := patternRe.ReplaceAllStringFunc(req.SequencePattern, func(match string) string {
				// Pad with zeros based on number of hashes
				format := fmt.Sprintf("%%0%dd", len(match))
				return fmt.Sprintf(format, currentIndex)
			})

			peer.Name = newName
			peer.UpdatedAt = time.Now().UTC()
			if err := s.server.UpdatePeer(peer); err != nil {
				log.Error().Err(err).Str("peer_id", peerID).Msg("Failed to rename peer in batch")
				continue
			}
			sequenceIndex++
			updatedCount++

		case 3: // ADD_TAGS
			if len(req.Tags) == 0 {
				continue
			}
			// Add unique tags
			existingTags := make(map[string]bool)
			for _, t := range peer.Tags {
				existingTags[t] = true
			}
			changed := false
			for _, t := range req.Tags {
				if !existingTags[t] {
					peer.Tags = append(peer.Tags, t)
					existingTags[t] = true
					changed = true
				}
			}
			if changed {
				peer.UpdatedAt = time.Now().UTC()
				if err := s.server.UpdatePeer(peer); err != nil {
					log.Error().Err(err).Msg("Failed to add tags")
					continue
				}
				updatedCount++
			}

		case 4: // REMOVE_TAGS
			if len(req.Tags) == 0 {
				continue
			}
			// Remove specified tags
			newTags := make([]string, 0, len(peer.Tags))
			changed := false
			removeMap := make(map[string]bool)
			for _, t := range req.Tags {
				removeMap[t] = true
			}

			for _, t := range peer.Tags {
				if removeMap[t] {
					changed = true
				} else {
					newTags = append(newTags, t)
				}
			}

			if changed {
				peer.Tags = newTags
				peer.UpdatedAt = time.Now().UTC()
				if err := s.server.UpdatePeer(peer); err != nil {
					log.Error().Err(err).Msg("Failed to remove tags")
					continue
				}
				updatedCount++
			}
		}
	}

	return &proto.BatchUpdatePeersResponse{
		Success:      true,
		Message:      fmt.Sprintf("Successfully processed %d peers", updatedCount),
		UpdatedCount: int32(updatedCount),
	}, nil
}

// getStringClaim safely extracts a string claim from JWT
func getStringClaim(claims map[string]interface{}, key string) string {
	if val, ok := claims[key].(string); ok {
		return val
	}
	return ""
}

// isValidEmail validates email format using RFC 5322 compliant regex
func isValidEmail(email string) bool {
	// RFC 5322 compliant regex for email validation
	// This is a simplified but effective version
	const emailRegex = `^[a-zA-Z0-9._%+-]+@[a-zA-Z0-9.-]+\.[a-zA-Z]{2,}$`
	matched, _ := regexp.MatchString(emailRegex, email)
	if !matched {
		return false
	}
	// Additional checks
	if len(email) > 254 {
		return false // RFC 5321 limit
	}
	if strings.Count(email, "@") != 1 {
		return false
	}
	parts := strings.Split(email, "@")
	if len(parts) != 2 {
		return false
	}
	localPart := parts[0]
	domain := parts[1]
	if len(localPart) > 64 { // RFC 5321 limit for local part
		return false
	}
	if len(localPart) == 0 {
		return false
	}
	// Local part cannot start or end with a dot
	if strings.HasPrefix(localPart, ".") || strings.HasSuffix(localPart, ".") {
		return false
	}
	// Local part cannot contain consecutive dots
	if strings.Contains(localPart, "..") {
		return false
	}
	if strings.HasPrefix(domain, ".") || strings.HasSuffix(domain, ".") {
		return false
	}
	if strings.Contains(domain, "..") {
		return false
	}
	return true
}

// generateTokenID generates a unique JWT ID for token revocation tracking
func generateTokenID() string {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		// Fallback to timestamp-based ID if crypto/rand fails
		return fmt.Sprintf("jti_%d", time.Now().UnixNano())
	}
	return hex.EncodeToString(b)
}

// =============================================================================
// WINBOX METHODS (tenant-scoped)
// =============================================================================

// DEPRECATED: SetTenantWinboxCredentials and GetTenantWinboxStatus removed
// Use WinboxSession CRUD operations (CreateTenantWinboxSession, etc.) instead

// ClearTenantWinboxCredentials removes stored Winbox credentials for a peer
func (s *TenantPortalServiceServer) ClearTenantWinboxCredentials(ctx context.Context, req *proto.ClearTenantWinboxCredentialsRequest) (*proto.ClearTenantWinboxCredentialsResponse, error) {
	if req.TenantId == "" {
		return nil, errs.InvalidArgumentE("tenant_id required")
	}
	if req.PeerId == "" {
		return nil, errs.InvalidArgumentE("peer_id required")
	}
	ctx = s.withResolvedCallerContext(ctx, req.TenantId)

	ctx, overlayAccountID, peer, usedScope, err := s.resolveAccountForPeer(ctx, req.TenantId, req.PeerId)
	if err != nil {
		return nil, errs.NotFoundf("peer not found: %v", err)
	}
	effectiveTenantID := req.TenantId
	if usedScope != nil {
		effectiveTenantID = usedScope.TenantID
	}
	if err := checkTagAccess(ctx, peer, effectiveTenantID); err != nil {
		return nil, err
	}

	// Clear Winbox credentials
	winboxMgr := s.server.GetWinboxManager()
	if err := winboxMgr.ClearWinboxCredentials(overlayAccountID, req.PeerId); err != nil {
		return nil, errs.Internalf("failed to clear Winbox credentials: %v", err)
	}

	return &proto.ClearTenantWinboxCredentialsResponse{
		Success: true,
		Message: "Winbox credentials cleared",
	}, nil
}

// CreateTenantWinboxSession creates a new Winbox session
func (s *TenantPortalServiceServer) CreateTenantWinboxSession(ctx context.Context, req *proto.CreateTenantWinboxSessionRequest) (*proto.CreateTenantWinboxSessionResponse, error) {
	// Validate required fields
	if req.TenantId == "" {
		return nil, errs.InvalidArgumentE("tenant_id is required")
	}
	if req.PeerId == "" {
		return nil, errs.InvalidArgumentE("peer_id is required")
	}
	if req.Name == "" {
		return nil, errs.InvalidArgumentE("name is required")
	}
	if req.Username == "" {
		return nil, errs.InvalidArgumentE("username is required")
	}
	if req.Password == "" {
		return nil, errs.InvalidArgumentE("password is required")
	}
	ctx = s.withResolvedCallerContext(ctx, req.TenantId)

	ctx, overlayAccountID, foundPeer, usedScope, err := s.resolveAccountForPeer(ctx, req.TenantId, req.PeerId)
	if err != nil {
		return nil, errs.NotFoundf("peer not found: %v", err)
	}
	effectiveTenantID := req.TenantId
	if usedScope != nil {
		effectiveTenantID = usedScope.TenantID
	}
	if err := checkTagAccess(ctx, foundPeer, effectiveTenantID); err != nil {
		return nil, err
	}

	// 1. Check Winbox session limits based on account tier
	acc, err := s.server.GetAccount(overlayAccountID)
	if err != nil {
		return nil, errs.Internalf("failed to get account details: %v", err)
	}

	sessions, err := s.server.GetPeerStore().ListAllWinboxSessions(overlayAccountID)
	if err == nil {
		maxSessions := acc.BlockCount * 29
		if len(sessions) >= maxSessions {
			return nil, errs.FailedPreconditionf("maximum number of Winbox sessions reached for your tier (max: %d)", maxSessions)
		}
	} else {
		log.Warn().Err(err).Str("account_id", overlayAccountID).Msg("Failed to check Winbox session count")
	}

	// Use discovered Winbox port if not specified (default 8291)
	// Prioritize discovered Winbox port from scan
	// The user requested: "select port faster from peer winbox port and fallback to received port"
	winboxPort := int32(foundPeer.ScannedWinboxPort)
	if winboxPort > 0 {
		log.Debug().
			Str("tenant_id", req.TenantId).
			Str("peer_id", req.PeerId).
			Int32("discovered_port", winboxPort).
			Msg("Using discovered Winbox port from port scan")
	} else {
		// Fallback to user-provided port if detection failed
		winboxPort = req.Port
		if winboxPort > 0 {
			log.Debug().
				Str("tenant_id", req.TenantId).
				Str("peer_id", req.PeerId).
				Int32("user_port", winboxPort).
				Msg("Using user-provided Winbox port (discovery unavailable)")
		} else {
			// Final fallback to default
			winboxPort = 8291
			log.Debug().
				Str("tenant_id", req.TenantId).
				Str("peer_id", req.PeerId).
				Int32("default_port", winboxPort).
				Msg("Using default Winbox port (no discovery or user input)")
		}
	}

	// Get encryption cipher for this tenant
	cipher, err := s.getCredentialCipher(overlayAccountID)
	if err != nil {
		return nil, errs.Internalf("failed to get encryption cipher: %v", err)
	}

	// Encrypt credentials
	encryptedUsername, err := cipher.Encrypt([]byte(req.Username))
	if err != nil {
		return nil, errs.Internalf("failed to encrypt username: %v", err)
	}
	encryptedPassword, err := cipher.Encrypt([]byte(req.Password))
	if err != nil {
		return nil, errs.Internalf("failed to encrypt password: %v", err)
	}

	// Generate access token (cryptographically secure) - used as virtual username
	accessToken, err := generateAccessToken()
	if err != nil {
		return nil, errs.Internalf("failed to generate access token: %v", err)
	}

	// Generate password token (cryptographically secure) - used as virtual password
	passwordToken, err := generateAccessToken()
	if err != nil {
		return nil, errs.Internalf("failed to generate password token: %v", err)
	}

	// Create session
	now := time.Now().UTC()
	session := server.WinboxSession{
		ID:                uuid.New().String(),
		Name:              req.Name,
		RouterIP:          strings.Split(foundPeer.AssignedIP, "/")[0],
		Port:              int(winboxPort),
		AccessToken:       accessToken,
		PasswordToken:     passwordToken,
		EncryptedUsername: encryptedUsername,
		EncryptedPassword: encryptedPassword,
		AuthMethod:        "", // Will be detected on first connection
		AllowedClientIPs:  req.AllowedClientIps,
		CredentialsValid:  false, // Not validated yet
		CreatedAt:         now,
		UpdatedAt:         now,
		Enabled:           true,
	}
	probeRouterOSSessionWithCredentials(ctx, s.server, rosapi.NewManager(), overlayAccountID, foundPeer, &session, req.Username, req.Password)
	if session.RouterOSAPIVerified {
		params := rosapi.ConnectParams{
			Address:            net.JoinHostPort(session.RouterIP, fmt.Sprintf("%d", session.RouterOSAPIPort)),
			Username:           req.Username,
			Password:           req.Password,
			UseTLS:             session.RouterOSAPITLS,
			InsecureSkipVerify: true,
		}
		if err := persistRouterOSAccessSuccess(s.server, overlayAccountID, foundPeer, &session, req.Username, req.Password, routerOSCredentialSourceWinbox, params); err != nil {
			log.Warn().Err(err).Str("peer_id", foundPeer.ID).Msg("Failed to persist RouterOS peer access after Winbox session create")
		}
	}

	// Add session to peer's sessions list
	// Add session to peer's sessions list (for in-memory cache update via SavePeer)
	foundPeer.WinboxSessions = append(foundPeer.WinboxSessions, session)
	foundPeer.HasWinbox = foundPeer.ScannedWinboxPort > 0 || len(foundPeer.WinboxSessions) > 0
	foundPeer.UpdatedAt = now

	peerStore := s.server.GetPeerStore()

	// 1. Persist session to DB explicitly (required as SavePeer does not save relations)
	if err := peerStore.SaveWinboxSession(overlayAccountID, req.PeerId, &session); err != nil {
		return nil, errs.Internalf("failed to save winbox session: %v", err)
	}

	// 2. Update peer metadata (HasWinbox flag) and update generic cache
	if err := peerStore.SavePeer(foundPeer); err != nil {
		return nil, errs.Internalf("failed to save peer: %v", err)
	}

	// Convert to proto
	pbSession := winboxSessionToProto(overlayAccountID, req.PeerId, &session)
	enrichWinboxSessionSharedFlags(ctx, pbSession, effectiveTenantID, overlayAccountID)

	return &proto.CreateTenantWinboxSessionResponse{
		Session:       pbSession,
		AccessToken:   accessToken,
		PasswordToken: passwordToken,
		Message:       fmt.Sprintf("Winbox session '%s' created. Use access token as username and password token as password in Winbox.", req.Name),
	}, nil
}

// DuplicateTenantWinboxSession clones an existing Winbox session under a new
// name. Router target, port, allowed-client list, auth method, and — most
// importantly — the encrypted credential blobs are copied byte for byte from
// the source row, so the caller doesn't need to re-enter the password (the
// server never decrypts the cleartext during the operation). Fresh access /
// password tokens are minted so the new row is a fully independent identity
// that can be edited or revoked without touching the source.
func (s *TenantPortalServiceServer) DuplicateTenantWinboxSession(ctx context.Context, req *proto.DuplicateTenantWinboxSessionRequest) (*proto.DuplicateTenantWinboxSessionResponse, error) {
	if req.TenantId == "" {
		return nil, errs.InvalidArgumentE("tenant_id is required")
	}
	if req.SessionId == "" {
		return nil, errs.InvalidArgumentE("session_id is required")
	}
	if req.NewName == "" {
		return nil, errs.InvalidArgumentE("new_name is required")
	}
	ctx = s.withResolvedCallerContext(ctx, req.TenantId)

	peerStore := s.server.GetPeerStore()
	src, err := peerStore.GetWinboxSession(req.SessionId)
	if err != nil || src == nil {
		return nil, errs.NotFoundf("source winbox session not found")
	}

	// Resolve scope + tag access via the peer the source session lives on.
	ctx, overlayAccountID, foundPeer, usedScope, err := s.resolveAccountForPeer(ctx, req.TenantId, src.PeerID)
	if err != nil {
		return nil, errs.NotFoundf("peer not found: %v", err)
	}
	effectiveTenantID := req.TenantId
	if usedScope != nil {
		effectiveTenantID = usedScope.TenantID
	}
	if err := checkTagAccess(ctx, foundPeer, effectiveTenantID); err != nil {
		return nil, err
	}

	// Enforce the same tier cap that gates Create.
	acc, err := s.server.GetAccount(overlayAccountID)
	if err != nil {
		return nil, errs.Internalf("failed to get account details: %v", err)
	}
	if existing, err := peerStore.ListAllWinboxSessions(overlayAccountID); err == nil {
		max := acc.BlockCount * 29
		if len(existing) >= max {
			return nil, errs.FailedPreconditionf("maximum number of Winbox sessions reached for your tier (max: %d)", max)
		}
		// Reject duplicate names on the same peer up front so the caller
		// gets a clear error rather than a generic uniqueness violation.
		for _, ws := range existing {
			if ws.PeerID == src.PeerID && strings.EqualFold(ws.Name, req.NewName) {
				return nil, errs.AlreadyExistsf("a winbox account named %q already exists on that peer", req.NewName)
			}
		}
	}

	// Fresh access + password tokens — the duplicate is a fully independent
	// identity, not an alias of the source.
	accessToken, err := generateAccessToken()
	if err != nil {
		return nil, errs.Internalf("failed to generate access token: %v", err)
	}
	passwordToken, err := generateAccessToken()
	if err != nil {
		return nil, errs.Internalf("failed to generate password token: %v", err)
	}

	now := time.Now().UTC()
	session := server.WinboxSession{
		ID:                       uuid.New().String(),
		Name:                     req.NewName,
		RouterIP:                 src.RouterIP,
		Port:                     src.Port,
		AccessToken:              accessToken,
		PasswordToken:            passwordToken,
		EncryptedUsername:        append([]byte(nil), src.EncryptedUsername...),
		EncryptedPassword:        append([]byte(nil), src.EncryptedPassword...),
		AuthMethod:               src.AuthMethod,
		AllowedClientIPs:         append([]string(nil), src.AllowedClientIPs...),
		CredentialsValid:         src.CredentialsValid,
		RouterOSAPIVerified:      src.RouterOSAPIVerified,
		RouterOSAPILastValidated: src.RouterOSAPILastValidated,
		RouterOSAPIPort:          src.RouterOSAPIPort,
		RouterOSAPITLS:           src.RouterOSAPITLS,
		CreatedAt:                now,
		UpdatedAt:                now,
		Enabled:                  true,
	}

	foundPeer.WinboxSessions = append(foundPeer.WinboxSessions, session)
	foundPeer.HasWinbox = foundPeer.ScannedWinboxPort > 0 || len(foundPeer.WinboxSessions) > 0
	foundPeer.UpdatedAt = now

	if err := peerStore.SaveWinboxSession(overlayAccountID, src.PeerID, &session); err != nil {
		return nil, errs.Internalf("failed to save winbox session: %v", err)
	}
	if err := peerStore.SavePeer(foundPeer); err != nil {
		return nil, errs.Internalf("failed to save peer: %v", err)
	}

	pbSession := winboxSessionToProto(overlayAccountID, src.PeerID, &session)
	enrichWinboxSessionSharedFlags(ctx, pbSession, effectiveTenantID, overlayAccountID)

	return &proto.DuplicateTenantWinboxSessionResponse{Session: pbSession}, nil
}

// UpdateTenantWinboxSession updates session parameters
func (s *TenantPortalServiceServer) UpdateTenantWinboxSession(ctx context.Context, req *proto.UpdateTenantWinboxSessionRequest) (*proto.UpdateTenantWinboxSessionResponse, error) {
	if req.TenantId == "" {
		return nil, errs.InvalidArgumentE("tenant_id is required")
	}
	if req.PeerId == "" {
		return nil, errs.InvalidArgumentE("peer_id is required")
	}
	if req.SessionId == "" {
		return nil, errs.InvalidArgumentE("session_id is required")
	}
	ctx = s.withResolvedCallerContext(ctx, req.TenantId)

	ctx, overlayAccountID, peer, usedScope, err := s.resolveAccountForPeer(ctx, req.TenantId, req.PeerId)
	if err != nil {
		return nil, errs.NotFoundf("peer not found: %v", err)
	}
	effectiveTenantID := req.TenantId
	if usedScope != nil {
		effectiveTenantID = usedScope.TenantID
	}
	if err := checkTagAccess(ctx, peer, effectiveTenantID); err != nil {
		return nil, err
	}

	// 2. Fetch session directly from DB (bypass potential stale cache in peer.WinboxSessions)
	peerStore := s.server.GetPeerStore()
	session, err := peerStore.GetWinboxSession(req.SessionId)
	if err != nil {
		return nil, errs.NotFoundf("session not found: %v", err)
	}
	if session.PeerID != req.PeerId {
		return nil, errs.NotFoundf("session does not belong to peer")
	}

	now := time.Now().UTC()
	// Update fields
	if req.Name != "" {
		session.Name = req.Name
	}
	if req.RouterIp != "" {
		session.RouterIP = req.RouterIp
	}
	// Update allowed IPs: clear_allowed_ips=true clears the list, otherwise replace if provided
	// Update allowed IPs: clear_allowed_ips=true clears the list, otherwise replace if provided
	if req.ClearAllowedIps {
		session.AllowedClientIPs = []string{} // Explicit empty slice
		log.Debug().Msg("🧹 Clearing AllowedClientIPs")
	} else if len(req.AllowedClientIps) > 0 {
		session.AllowedClientIPs = req.AllowedClientIps
	}
	// Note: If req.Enabled is false (default), this disables the session.
	// Ensure frontend sends the current state if no change is intended.
	session.Enabled = req.Enabled

	// Update credentials if provided
	if req.Username != "" || req.Password != "" {
		var cipher *crypto.CredentialCipher
		cipher, err = s.getCredentialCipher(overlayAccountID)
		if err != nil {
			return nil, errs.Internalf("failed to get encryption cipher: %v", err)
		}

		if req.Username != "" {
			encryptedUsername, err := cipher.Encrypt([]byte(req.Username))
			if err != nil {
				return nil, errs.Internalf("failed to encrypt username: %v", err)
			}
			session.EncryptedUsername = encryptedUsername
		}
		if req.Password != "" {
			encryptedPassword, err := cipher.Encrypt([]byte(req.Password))
			if err != nil {
				return nil, errs.Internalf("failed to encrypt password: %v", err)
			}
			session.EncryptedPassword = encryptedPassword
		}

		// Reset validation status when credentials change
		session.CredentialsValid = false
		session.ValidationError = ""
	}

	// Regenerate access token if requested
	var newAccessToken string
	if req.RegenerateToken {
		newAccessToken, err = generateAccessToken()
		if err != nil {
			return nil, errs.Internalf("failed to generate access token: %v", err)
		}
		session.AccessToken = newAccessToken
	}

	// Regenerate password token if requested
	var newPasswordToken string
	if req.RegeneratePasswordToken {
		newPasswordToken, err = generateAccessToken()
		if err != nil {
			return nil, errs.Internalf("failed to generate password token: %v", err)
		}
		session.PasswordToken = newPasswordToken
	}

	session.UpdatedAt = now
	if session.Enabled {
		if username, password, derr := decryptWinboxCredentials(s.server, overlayAccountID, session); derr == nil {
			probeRouterOSSessionWithCredentials(ctx, s.server, rosapi.NewManager(), overlayAccountID, peer, session, username, password)
			if session.RouterOSAPIVerified {
				params := rosapi.ConnectParams{
					Address:            net.JoinHostPort(routerOSProbeHost(peer, session), fmt.Sprintf("%d", session.RouterOSAPIPort)),
					Username:           username,
					Password:           password,
					UseTLS:             session.RouterOSAPITLS,
					InsecureSkipVerify: true,
				}
				if err := persistRouterOSAccessSuccess(s.server, overlayAccountID, peer, session, username, password, routerOSCredentialSourceWinbox, params); err != nil {
					log.Warn().Err(err).Str("peer_id", peer.ID).Msg("Failed to persist RouterOS peer access after Winbox session update")
				}
			}
		} else {
			session.RouterOSAPIVerified = false
			session.RouterOSAPILastValidated = now
			session.RouterOSAPIError = derr.Error()
		}
	}

	log.Debug().
		Str("session_id", session.ID).
		Str("name", session.Name).
		Bool("enabled", session.Enabled).
		Strs("allowed_ips", session.AllowedClientIPs).
		Msg("💾 Saving updated Winbox session")

	// 3. Update session in DB explicitly (also invalidates peer cache)
	if err := peerStore.SaveWinboxSession(overlayAccountID, req.PeerId, session); err != nil {
		log.Error().Err(err).Str("session_id", session.ID).Msg("Failed to save winbox session")
		return nil, errs.Internalf("failed to save winbox session: %v", err)
	}

	pbSession := convertWinboxSessionToProto(req.PeerId, session)
	enrichWinboxSessionSharedFlags(ctx, pbSession, effectiveTenantID, overlayAccountID)

	msg := fmt.Sprintf("Winbox session '%s' updated.", session.Name)
	if req.RegenerateToken {
		msg += " New access token generated."
	}
	if req.RegeneratePasswordToken {
		msg += " New password token generated."
	}

	return &proto.UpdateTenantWinboxSessionResponse{
		Session:       pbSession,
		AccessToken:   newAccessToken,
		PasswordToken: newPasswordToken,
		Message:       msg,
	}, nil
}

// DeleteTenantWinboxSession deletes a Winbox session
func (s *TenantPortalServiceServer) DeleteTenantWinboxSession(ctx context.Context, req *proto.DeleteTenantWinboxSessionRequest) (*proto.DeleteTenantWinboxSessionResponse, error) {
	if req.TenantId == "" {
		return nil, errs.InvalidArgumentE("tenant_id is required")
	}
	if req.PeerId == "" {
		return nil, errs.InvalidArgumentE("peer_id is required")
	}
	if req.SessionId == "" {
		return nil, errs.InvalidArgumentE("session_id is required")
	}
	ctx = s.withResolvedCallerContext(ctx, req.TenantId)

	ctx, overlayAccountID, peer, usedScope, err := s.resolveAccountForPeer(ctx, req.TenantId, req.PeerId)
	if err != nil {
		return nil, errs.NotFoundf("peer not found: %v", err)
	}
	effectiveTenantID := req.TenantId
	if usedScope != nil {
		effectiveTenantID = usedScope.TenantID
	}
	if err := checkTagAccess(ctx, peer, effectiveTenantID); err != nil {
		return nil, err
	}
	if err := denySharedDelete(usedScope, "winbox sessions"); err != nil {
		return nil, err
	}

	peerStore := s.server.GetPeerStore()

	// 2. Delete from DB explicitly (ID based, bypass cache issues)
	if err := peerStore.DeleteWinboxSession(overlayAccountID, req.PeerId, req.SessionId); err != nil {
		// If delete succeeds (nil error), it means done or not found.
		// Note: DeleteWinboxSession in repo returns nil if not found.
		// So we assume success.
		return nil, errs.Internalf("failed to delete winbox session: %v", err)

	}

	// 3. Update HasWinbox flag - check remaining sessions in DB (only if peer exists)
	if peer != nil {
		sessions, err := peerStore.ListWinboxSessions(overlayAccountID, req.PeerId)
		if err == nil {
			newHasWinbox := len(sessions) > 0 || peer.ScannedWinboxPort > 0
			if peer.HasWinbox != newHasWinbox {
				peer.HasWinbox = newHasWinbox
				peer.UpdatedAt = time.Now().UTC()
				if err := peerStore.SavePeer(peer); err != nil {
					log.Warn().Err(err).Msg("Failed to update peer HasWinbox status after session deletion")
				}
			}
		} else {
			log.Warn().Err(err).Msg("Failed to list sessions to update HasWinbox status")
		}
	}

	// Note: DeleteWinboxSession invalidates cache, so GetPeer next time hits DB.
	// SavePeer checks cache and sets it. If we SavePeer, we update cache.

	return &proto.DeleteTenantWinboxSessionResponse{
		Success: true,
		Message: "Winbox session deleted successfully",
	}, nil
}

// listWinboxSessionsForCaller aggregates Winbox sessions across all accessible
// scopes (own + shared). Called when a session-authenticated CallerContext is present.
func (s *TenantPortalServiceServer) listWinboxSessionsForCaller(cc *auth.CallerContext, peerIDFilter string) (*proto.ListTenantWinboxSessionsResponse, error) {
	var mu sync.Mutex
	var out []*proto.WinboxSession
	var wg sync.WaitGroup

	for _, sc := range cc.Scopes {
		if !sc.Permissions.CanRead() {
			continue
		}
		wg.Add(1)
		go func(scope *auth.AccessScope) {
			defer wg.Done()
			var sessions []server.WinboxSession
			var err error
			if peerIDFilter != "" {
				peer, e := s.server.GetPeer(scope.AccountID, peerIDFilter)
				if e != nil || !peerMatchesTags(peer, scope.Tags) {
					return
				}
				if peer.HasWinbox {
					sessions = peer.WinboxSessions
				}
			} else {
				sessions, err = s.server.GetPeerStore().ListAllWinboxSessions(scope.AccountID)
				if err != nil {
					return
				}
				// Apply tag filter when needed
				if len(scope.Tags) > 0 {
					peers, _ := s.server.ListPeers(scope.AccountID)
					allowed := make(map[string]struct{}, len(peers))
					for _, p := range peers {
						if peerMatchesTags(p, scope.Tags) {
							allowed[p.ID] = struct{}{}
						}
					}
					var filtered []server.WinboxSession
					for i := range sessions {
						if _, ok := allowed[sessions[i].PeerID]; ok {
							filtered = append(filtered, sessions[i])
						}
					}
					sessions = filtered
				}
			}
			for i := range sessions {
				pb := convertWinboxSessionToProto(sessions[i].PeerID, &sessions[i])
				pb.ViewerCanWrite = scope.Permissions.CanWrite()
				if !scope.IsOwner {
					pb.IsShared = true
					pb.OwnerName = scope.OwnerName
				}
				mu.Lock()
				out = append(out, pb)
				mu.Unlock()
			}
		}(sc)
	}
	wg.Wait()
	return &proto.ListTenantWinboxSessionsResponse{Sessions: out}, nil
}

// ListTenantWinboxSessions lists all Winbox sessions for a peer
func (s *TenantPortalServiceServer) ListTenantWinboxSessions(ctx context.Context, req *proto.ListTenantWinboxSessionsRequest) (*proto.ListTenantWinboxSessionsResponse, error) {
	if req.TenantId == "" {
		return nil, errs.InvalidArgumentE("tenant_id is required")
	}
	ctx = s.withHydratedCallerContext(ctx, req.TenantId)

	// Session-authenticated callers.
	if cc := auth.CallerContextFromContext(ctx); cc != nil {
		if route := auth.RequestAccessRouteFromContext(ctx); route != nil && route.Mode == auth.RequestAccessModeFocusedShared {
			scope := routeScopeFromCallerContext(cc, route)
			if scope == nil {
				return nil, errs.PermissionDeniedE("access denied: resource not accessible")
			}
			return s.listWinboxSessionsForCaller(&auth.CallerContext{TenantID: cc.TenantID, Scopes: []*auth.AccessScope{scope}, ScopesHydrated: true}, req.PeerId)
		}
		if req.TenantId != cc.TenantID {
			scope := cc.ScopeFor(req.TenantId)
			if scope == nil {
				return nil, errs.PermissionDeniedE("access denied: resource not accessible")
			}
			return s.listWinboxSessionsForCaller(&auth.CallerContext{TenantID: cc.TenantID, Scopes: []*auth.AccessScope{scope}, ScopesHydrated: true}, req.PeerId)
		}
		return s.listWinboxSessionsForCaller(cc, req.PeerId)
	}

	// API-key / internal callers: original single-account path.
	// Resolve tenant ID to overlay account ID
	overlayAccountID, err := s.getOverlayAccountID(req.TenantId)
	if err != nil {
		return nil, errs.NotFoundf("account not found: %v", err)
	}

	pbSessions := []*proto.WinboxSession{}

	// If PeerId is provided, fetch specific peer which populates sessions
	if req.PeerId != "" {
		peer, err := s.server.GetPeer(overlayAccountID, req.PeerId)
		if err != nil {
			return nil, errs.NotFoundf("peer not found: %v", err)
		}
		if peer.AccountID != overlayAccountID {
			return nil, errs.PermissionDeniedE("peer does not belong to this tenant")
		}
		if err := checkTagAccess(ctx, peer, req.TenantId); err != nil {
			return nil, err
		}

		if peer.HasWinbox {
			for i := range peer.WinboxSessions {
				pbSessions = append(pbSessions, convertWinboxSessionToProto(peer.ID, &peer.WinboxSessions[i]))
			}
		}
	} else {
		// List all sessions for the tenant efficiently from DB.
		sessions, err := s.server.GetPeerStore().ListAllWinboxSessions(overlayAccountID)
		if err != nil {
			return nil, errs.Internalf("failed to list winbox sessions: %v", err)
		}
		for i := range sessions {
			pbSessions = append(pbSessions, convertWinboxSessionToProto(sessions[i].PeerID, &sessions[i]))
		}
	}

	return &proto.ListTenantWinboxSessionsResponse{
		Sessions: pbSessions,
	}, nil
}

// Helper to convert session to proto
func convertWinboxSessionToProto(peerID string, s *server.WinboxSession) *proto.WinboxSession {
	return &proto.WinboxSession{
		Id:                       s.ID,
		PeerId:                   peerID,
		Name:                     s.Name,
		RouterIp:                 s.RouterIP,
		AllowedClientIps:         s.AllowedClientIPs,
		CredentialsValid:         s.CredentialsValid,
		ValidationError:          s.ValidationError,
		CreatedAt:                proto.TimestampFromTime(s.CreatedAt),
		UpdatedAt:                proto.TimestampFromTime(s.UpdatedAt),
		Enabled:                  s.Enabled,
		LastValidated:            proto.TimestampFromTime(s.LastValidated),
		LastConnected:            proto.TimestampFromTime(s.LastConnected),
		AccessToken:              s.AccessToken,
		PasswordToken:            s.PasswordToken,
		AuthMethod:               s.AuthMethod,
		RouterosApiVerified:      s.RouterOSAPIVerified,
		RouterosApiLastValidated: proto.TimestampFromTime(s.RouterOSAPILastValidated),
		RouterosApiError:         s.RouterOSAPIError,
		RouterosApiPort:          int32(s.RouterOSAPIPort),
		RouterosApiTls:           s.RouterOSAPITLS,
	}

}

// GetTenantWinboxSession gets details for a specific Winbox session
func (s *TenantPortalServiceServer) GetTenantWinboxSession(ctx context.Context, req *proto.GetTenantWinboxSessionRequest) (*proto.GetTenantWinboxSessionResponse, error) {
	if req.TenantId == "" {
		return nil, errs.InvalidArgumentE("tenant_id is required")
	}
	if req.PeerId == "" {
		return nil, errs.InvalidArgumentE("peer_id is required")
	}
	if req.SessionId == "" {
		return nil, errs.InvalidArgumentE("session_id is required")
	}
	ctx = s.withResolvedCallerContext(ctx, req.TenantId)

	ctx, overlayAccountID, peer, usedScope, err := s.resolveAccountForPeer(ctx, req.TenantId, req.PeerId)
	if err != nil {
		return nil, errs.NotFoundf("peer not found: %v", err)
	}
	effectiveTenantID := req.TenantId
	if usedScope != nil {
		effectiveTenantID = usedScope.TenantID
	}
	if err := checkTagAccess(ctx, peer, effectiveTenantID); err != nil {
		return nil, err
	}

	// Find session - O(n) but n is small
	for i := range peer.WinboxSessions {
		if peer.WinboxSessions[i].ID == req.SessionId {
			pbSession := winboxSessionToProto(overlayAccountID, req.PeerId, &peer.WinboxSessions[i])
			enrichWinboxSessionSharedFlags(ctx, pbSession, effectiveTenantID, overlayAccountID)
			return &proto.GetTenantWinboxSessionResponse{
				Session: pbSession,
			}, nil
		}
	}

	return nil, errs.NotFoundf("session not found: %s", req.SessionId)
}

// =============================================================================
// ACL METHODS (tenant-scoped)
// =============================================================================

// CreateTenantPeerGroup creates a new peer group for tenant
func (s *TenantPortalServiceServer) CreateTenantPeerGroup(ctx context.Context, req *proto.CreateTenantPeerGroupRequest) (*proto.CreateTenantPeerGroupResponse, error) {
	if req.TenantId == "" {
		return nil, errs.InvalidArgumentE("tenant_id required")
	}
	if req.GroupId == "" {
		return nil, errs.InvalidArgumentE("group_id required")
	}
	if req.Name == "" {
		return nil, errs.InvalidArgumentE("name required")
	}

	ctx, overlayAccountID, usedScope, err := s.resolveTenantAccessAccount(ctx, req.TenantId, true)
	if err != nil {
		return nil, err
	}
	if err := requireScopeWrite(usedScope, "peer groups"); err != nil {
		return nil, err
	}

	protocols := make([]uint8, len(req.AllowedProtocols))
	for i, p := range req.AllowedProtocols {
		protocols[i] = uint8(p)
	}

	group, err := s.server.CreatePeerGroup(overlayAccountID, req.GroupId, req.Name, req.Description, protocols)
	if err != nil {
		return nil, errs.Internalf("failed to create peer group: %v", err)
	}

	pbProtos := make([]uint32, len(group.Protocols))
	for i, p := range group.Protocols {
		pbProtos[i] = uint32(p)
	}

	pbGroup := &proto.PeerGroup{
		Id:               group.ID,
		AccountId:        group.AccountID,
		Name:             group.Name,
		DisplayName:      group.Name,
		Description:      group.Description,
		PeerIds:          []string{},
		AllowedProtocols: pbProtos,
		CreatedAt:        proto.TimestampFromTime(group.CreatedAt),
		UpdatedAt:        proto.TimestampFromTime(group.UpdatedAt),
	}

	return &proto.CreateTenantPeerGroupResponse{Group: pbGroup}, nil
}

// DeleteTenantPeerGroup deletes a peer group
func (s *TenantPortalServiceServer) DeleteTenantPeerGroup(ctx context.Context, req *proto.DeleteTenantPeerGroupRequest) (*proto.DeleteTenantPeerGroupResponse, error) {
	if req.TenantId == "" {
		return nil, errs.InvalidArgumentE("tenant_id required")
	}
	if req.GroupId == "" {
		return nil, errs.InvalidArgumentE("group_id required")
	}

	ctx, overlayAccountID, usedScope, err := s.resolveTenantAccessAccount(ctx, req.TenantId, true)
	if err != nil {
		return nil, err
	}
	if err := requireScopeWrite(usedScope, "peer groups"); err != nil {
		return nil, err
	}
	if err := denySharedDelete(usedScope, "peer groups"); err != nil {
		return nil, err
	}

	err = s.server.DeletePeerGroup(overlayAccountID, req.GroupId)
	if err != nil {
		return nil, errs.Internalf("failed to delete peer group: %v", err)
	}

	return &proto.DeleteTenantPeerGroupResponse{Success: true}, nil
}

// ListTenantPeerGroups lists all peer groups for tenant
func (s *TenantPortalServiceServer) ListTenantPeerGroups(ctx context.Context, req *proto.ListTenantPeerGroupsRequest) (*proto.ListTenantPeerGroupsResponse, error) {
	if req.TenantId == "" {
		return nil, errs.InvalidArgumentE("tenant_id required")
	}

	ctx, overlayAccountID, usedScope, err := s.resolveTenantAccessAccount(ctx, req.TenantId, true)
	if err != nil {
		return nil, err
	}
	if err := requireScopeRead(usedScope, "peer groups"); err != nil {
		return nil, err
	}

	groups := s.server.ListPeerGroups(overlayAccountID)
	pbGroups := make([]*proto.PeerGroup, 0, len(groups))

	for _, group := range groups {
		pbProtos := make([]uint32, len(group.Protocols))
		for i, p := range group.Protocols {
			pbProtos[i] = uint32(p)
		}

		pbGroup := &proto.PeerGroup{
			Id:               group.ID,
			AccountId:        group.AccountID,
			Name:             group.Name,
			DisplayName:      group.Name,
			Description:      group.Description,
			PeerIds:          []string{},
			AllowedProtocols: pbProtos,
			CreatedAt:        proto.TimestampFromTime(group.CreatedAt),
			UpdatedAt:        proto.TimestampFromTime(group.UpdatedAt),
		}
		pbGroups = append(pbGroups, pbGroup)
	}

	return &proto.ListTenantPeerGroupsResponse{Groups: pbGroups}, nil
}

// AddTenantPeerToGroup adds a peer to a group (both must belong to tenant)
func (s *TenantPortalServiceServer) AddTenantPeerToGroup(ctx context.Context, req *proto.AddTenantPeerToGroupRequest) (*proto.AddTenantPeerToGroupResponse, error) {
	if req.TenantId == "" {
		return nil, errs.InvalidArgumentE("tenant_id required")
	}
	if req.GroupId == "" {
		return nil, errs.InvalidArgumentE("group_id required")
	}
	if req.PeerId == "" {
		return nil, errs.InvalidArgumentE("peer_id required")
	}

	ctx, overlayAccountID, usedScope, err := s.resolveTenantAccessAccount(ctx, req.TenantId, true)
	if err != nil {
		return nil, err
	}
	if err := requireScopeWrite(usedScope, "peer groups"); err != nil {
		return nil, err
	}
	resourceTenantID := req.TenantId
	if usedScope != nil {
		resourceTenantID = usedScope.TenantID
	}

	// Verify peer belongs to tenant
	peer, err := s.server.GetPeer(overlayAccountID, req.PeerId)
	if err != nil {
		return nil, errs.NotFoundf("peer not found: %v", err)
	}
	if peer.AccountID != overlayAccountID {
		return nil, errs.PermissionDeniedE("peer does not belong to this tenant")
	}
	if err := checkTagAccess(ctx, peer, resourceTenantID); err != nil {
		return nil, err
	}

	err = s.server.AddPeerToGroup(overlayAccountID, req.PeerId, req.GroupId)
	if err != nil {
		return nil, errs.Internalf("failed to add peer to group: %v", err)
	}

	return &proto.AddTenantPeerToGroupResponse{Success: true}, nil
}

// RemoveTenantPeerFromGroup removes a peer from a group
func (s *TenantPortalServiceServer) RemoveTenantPeerFromGroup(ctx context.Context, req *proto.RemoveTenantPeerFromGroupRequest) (*proto.RemoveTenantPeerFromGroupResponse, error) {
	if req.TenantId == "" {
		return nil, errs.InvalidArgumentE("tenant_id required")
	}
	if req.GroupId == "" {
		return nil, errs.InvalidArgumentE("group_id required")
	}
	if req.PeerId == "" {
		return nil, errs.InvalidArgumentE("peer_id required")
	}

	ctx, overlayAccountID, usedScope, err := s.resolveTenantAccessAccount(ctx, req.TenantId, true)
	if err != nil {
		return nil, err
	}
	if err := requireScopeWrite(usedScope, "peer groups"); err != nil {
		return nil, err
	}
	resourceTenantID := req.TenantId
	if usedScope != nil {
		resourceTenantID = usedScope.TenantID
	}

	// Verify peer belongs to tenant
	peer, err := s.server.GetPeer(overlayAccountID, req.PeerId)
	if err != nil {
		return nil, errs.NotFoundf("peer not found: %v", err)
	}
	if peer.AccountID != overlayAccountID {
		return nil, errs.PermissionDeniedE("peer does not belong to this tenant")
	}
	if err := checkTagAccess(ctx, peer, resourceTenantID); err != nil {
		return nil, err
	}

	err = s.server.RemovePeerFromGroup(overlayAccountID, req.PeerId, req.GroupId)
	if err != nil {
		return nil, errs.Internalf("failed to remove peer from group: %v", err)
	}

	return &proto.RemoveTenantPeerFromGroupResponse{Success: true}, nil
}

// CreateTenantGroupLink creates a link between two groups (both must belong to tenant)
func (s *TenantPortalServiceServer) CreateTenantGroupLink(ctx context.Context, req *proto.CreateTenantGroupLinkRequest) (*proto.CreateTenantGroupLinkResponse, error) {
	if req.TenantId == "" {
		return nil, errs.InvalidArgumentE("tenant_id required")
	}
	if req.SourceGroupId == "" {
		return nil, errs.InvalidArgumentE("source_group_id required")
	}
	if req.TargetGroupId == "" {
		return nil, errs.InvalidArgumentE("target_group_id required")
	}

	ctx, overlayAccountID, usedScope, err := s.resolveTenantAccessAccount(ctx, req.TenantId, true)
	if err != nil {
		return nil, err
	}
	if err := requireScopeWrite(usedScope, "group links"); err != nil {
		return nil, err
	}

	protocols := make([]uint8, len(req.AllowedProtocols))
	for i, p := range req.AllowedProtocols {
		protocols[i] = uint8(p)
	}

	// Generate link ID
	linkIDBytes := make([]byte, 16)
	rand.Read(linkIDBytes)
	linkID := hex.EncodeToString(linkIDBytes)

	// Use "allow" as default action
	action := "allow"

	// Tenant API doesn't expose port ranges here - pass empty portRanges
	link, err := s.server.CreateGroupLink(overlayAccountID, linkID, req.SourceGroupId, req.TargetGroupId, action, protocols, nil)
	if err != nil {
		return nil, errs.Internalf("failed to create group link: %v", err)
	}

	pbProtos := make([]uint32, len(link.Protocols))
	for i, p := range link.Protocols {
		pbProtos[i] = uint32(p)
	}

	pbLink := &proto.GroupLink{
		Id:            link.ID,
		AccountId:     link.AccountID,
		SourceGroupId: link.SrcGroupID,
		DestGroupId:   link.DstGroupID,
		Action:        link.Action,
		Protocols:     pbProtos,
		PortRanges:    []*proto.PortRange{},
		Priority:      0,
		CreatedAt:     proto.TimestampFromTime(link.CreatedAt),
		UpdatedAt:     proto.TimestampFromTime(link.UpdatedAt),
	}

	return &proto.CreateTenantGroupLinkResponse{Link: pbLink}, nil
}

// DeleteTenantGroupLink deletes a group link
func (s *TenantPortalServiceServer) DeleteTenantGroupLink(ctx context.Context, req *proto.DeleteTenantGroupLinkRequest) (*proto.DeleteTenantGroupLinkResponse, error) {
	if req.TenantId == "" {
		return nil, errs.InvalidArgumentE("tenant_id required")
	}

	ctx, overlayAccountID, usedScope, err := s.resolveTenantAccessAccount(ctx, req.TenantId, true)
	if err != nil {
		return nil, err
	}
	if err := requireScopeWrite(usedScope, "group links"); err != nil {
		return nil, err
	}
	if err := denySharedDelete(usedScope, "group links"); err != nil {
		return nil, err
	}

	var linkID string

	// If link_id is provided, use it directly (preferred method)
	if req.LinkId != "" {
		linkID = req.LinkId
	} else {
		// Fallback: Find link by source/target group IDs (legacy method)
		if req.SourceGroupId == "" {
			return nil, errs.InvalidArgumentE("source_group_id or link_id required")
		}
		if req.TargetGroupId == "" {
			return nil, errs.InvalidArgumentE("target_group_id or link_id required")
		}

		// Find the link by source/target group IDs
		links := s.server.ListGroupLinks(overlayAccountID)
		for _, link := range links {
			if link.SrcGroupID == req.SourceGroupId && link.DstGroupID == req.TargetGroupId {
				linkID = link.ID
				break
			}
		}

		if linkID == "" {
			return nil, errs.NotFoundE("group link not found")
		}
	}

	err = s.server.DeleteGroupLink(overlayAccountID, linkID)
	if err != nil {
		return nil, errs.Internalf("failed to delete group link: %v", err)
	}

	return &proto.DeleteTenantGroupLinkResponse{Success: true}, nil
}

// ListTenantGroupLinks lists all group links for tenant
func (s *TenantPortalServiceServer) ListTenantGroupLinks(ctx context.Context, req *proto.ListTenantGroupLinksRequest) (*proto.ListTenantGroupLinksResponse, error) {
	if req.TenantId == "" {
		return nil, errs.InvalidArgumentE("tenant_id required")
	}

	ctx, overlayAccountID, usedScope, err := s.resolveTenantAccessAccount(ctx, req.TenantId, true)
	if err != nil {
		return nil, err
	}
	if err := requireScopeRead(usedScope, "group links"); err != nil {
		return nil, err
	}

	links := s.server.ListGroupLinks(overlayAccountID)
	pbLinks := make([]*proto.GroupLink, 0, len(links))

	for _, link := range links {
		pbProtos := make([]uint32, len(link.Protocols))
		for i, p := range link.Protocols {
			pbProtos[i] = uint32(p)
		}

		pbLink := &proto.GroupLink{
			Id:            link.ID,
			AccountId:     link.AccountID,
			SourceGroupId: link.SrcGroupID,
			DestGroupId:   link.DstGroupID,
			Action:        link.Action,
			Protocols:     pbProtos,
			PortRanges:    []*proto.PortRange{},
			Priority:      0,
			CreatedAt:     proto.TimestampFromTime(link.CreatedAt),
			UpdatedAt:     proto.TimestampFromTime(link.UpdatedAt),
		}
		pbLinks = append(pbLinks, pbLink)
	}

	return &proto.ListTenantGroupLinksResponse{Links: pbLinks}, nil
}

// CompileTenantGroups compiles ACL rules from peer groups and links
func (s *TenantPortalServiceServer) CompileTenantGroups(ctx context.Context, req *proto.CompileTenantGroupsRequest) (*proto.CompileTenantGroupsResponse, error) {
	if req.TenantId == "" {
		return nil, errs.InvalidArgumentE("tenant_id required")
	}

	ctx, overlayAccountID, usedScope, err := s.resolveTenantAccessAccount(ctx, req.TenantId, true)
	if err != nil {
		return nil, err
	}
	if err := requireScopeWrite(usedScope, "ACL compilation"); err != nil {
		return nil, err
	}

	startTime := time.Now()
	rules, err := s.server.CompileGroups(overlayAccountID)
	if err != nil {
		return nil, errs.Internalf("failed to compile groups: %v", err)
	}
	compilationTime := time.Since(startTime)

	return &proto.CompileTenantGroupsResponse{
		Success:           true,
		RulesGenerated:    int32(len(rules)),
		CompilationTimeMs: int32(compilationTime.Milliseconds()),
	}, nil
}

// GetTenantCompilationStats returns compilation statistics
func (s *TenantPortalServiceServer) GetTenantCompilationStats(ctx context.Context, req *proto.GetTenantCompilationStatsRequest) (*proto.GetTenantCompilationStatsResponse, error) {
	if req.TenantId == "" {
		return nil, errs.InvalidArgumentE("tenant_id required")
	}

	ctx, overlayAccountID, usedScope, err := s.resolveTenantAccessAccount(ctx, req.TenantId, true)
	if err != nil {
		return nil, err
	}
	if err := requireScopeRead(usedScope, "ACL compilation stats"); err != nil {
		return nil, err
	}

	stats := s.server.GetCompilationStats(overlayAccountID)

	// Extract values from map with defaults
	var lastCompilation time.Time
	if lc, ok := stats["last_compilation"].(time.Time); ok {
		lastCompilation = lc
	}

	totalRules, _ := stats["total_rules"].(int)
	totalGroups, _ := stats["total_groups"].(int)
	totalLinks, _ := stats["total_links"].(int)

	return &proto.GetTenantCompilationStatsResponse{
		LastCompilation: proto.TimestampFromTime(lastCompilation),
		TotalRules:      int32(totalRules),
		TotalGroups:     int32(totalGroups),
		TotalLinks:      int32(totalLinks),
	}, nil
}

// AddTenantACLRule adds a custom ACL rule
func (s *TenantPortalServiceServer) AddTenantACLRule(ctx context.Context, req *proto.AddTenantACLRuleRequest) (*proto.AddTenantACLRuleResponse, error) {
	if req.TenantId == "" {
		return nil, errs.InvalidArgumentE("tenant_id required")
	}

	ctx, overlayAccountID, usedScope, err := s.resolveTenantAccessAccount(ctx, req.TenantId, true)
	if err != nil {
		return nil, err
	}
	if err := requireScopeWrite(usedScope, "ACL rules"); err != nil {
		return nil, err
	}

	// Generate rule ID
	ruleIDBytes := make([]byte, 16)
	rand.Read(ruleIDBytes)
	ruleID := hex.EncodeToString(ruleIDBytes)

	// Create ACLRule struct (server.ACLRule has Protocol as string, DestPort as int)
	distport, err := strconv.ParseInt(req.DestPort, 10, 32)
	if err != nil && req.DestPort != "" {
		return nil, errs.InvalidArgumentf("invalid dest_port: %v", err)
	}
	rule := &server.ACLRule{
		ID:        ruleID,
		AccountID: overlayAccountID,
		Action:    req.Action,
		Protocol:  fmt.Sprintf("%d", req.Protocol), // Convert uint32 to string
		SourceIP:  req.SourceIp,
		DestIP:    req.DestIp,
		DestPort:  int(distport),
		Priority:  int(req.Priority),
	}

	err = s.server.AddACLRule(rule)
	if err != nil {
		return nil, errs.Internalf("failed to add ACL rule: %v", err)
	}

	// Proto ACLRule has: repeated source_ips, repeated dest_ips, repeated dest_ports (int32)
	pbRule := &proto.ACLRule{
		Id:        rule.ID,
		AccountId: rule.AccountID,
		Name:      "",
		Action:    rule.Action,
		Protocol:  rule.Protocol, // string in both
		SourceIps: []string{rule.SourceIP},
		DestIps:   []string{rule.DestIP},
		DestPorts: []int32{int32(rule.DestPort)},
		Priority:  int32(rule.Priority),
		CreatedAt: proto.TimestampFromTime(time.Now().UTC()),
	}

	return &proto.AddTenantACLRuleResponse{Rule: pbRule}, nil
}

// RemoveTenantACLRule removes an ACL rule
func (s *TenantPortalServiceServer) RemoveTenantACLRule(ctx context.Context, req *proto.RemoveTenantACLRuleRequest) (*proto.RemoveTenantACLRuleResponse, error) {
	if req.TenantId == "" {
		return nil, errs.InvalidArgumentE("tenant_id required")
	}
	if req.RuleId == "" {
		return nil, errs.InvalidArgumentE("rule_id required")
	}

	ctx, overlayAccountID, usedScope, err := s.resolveTenantAccessAccount(ctx, req.TenantId, true)
	if err != nil {
		return nil, err
	}
	if err := requireScopeWrite(usedScope, "ACL rules"); err != nil {
		return nil, err
	}
	if err := denySharedDelete(usedScope, "ACL rules"); err != nil {
		return nil, err
	}

	err = s.server.RemoveACLRule(overlayAccountID, req.RuleId)
	if err != nil {
		return nil, errs.Internalf("failed to remove ACL rule: %v", err)
	}

	return &proto.RemoveTenantACLRuleResponse{Success: true}, nil
}

// GetTenantACLRules returns all ACL rules for tenant
func (s *TenantPortalServiceServer) GetTenantACLRules(ctx context.Context, req *proto.GetTenantACLRulesRequest) (*proto.GetTenantACLRulesResponse, error) {
	if req.TenantId == "" {
		return nil, errs.InvalidArgumentE("tenant_id required")
	}
	ctx, overlayAccountID, usedScope, err := s.resolveTenantAccessAccount(ctx, req.TenantId, true)
	if err != nil {
		return nil, err
	}
	if err := requireScopeRead(usedScope, "ACL rules"); err != nil {
		return nil, err
	}

	rules := s.server.GetACLRules(overlayAccountID)
	pbRules := make([]*proto.ACLRule, 0, len(rules))

	for _, rule := range rules {
		pbRule := &proto.ACLRule{
			Id:        rule.ID,
			AccountId: rule.AccountID,
			Name:      "",
			Action:    rule.Action,
			Protocol:  rule.Protocol, // Already a string
			SourceIps: []string{rule.SourceIP},
			DestIps:   []string{rule.DestIP},
			DestPorts: []int32{int32(rule.DestPort)},
			Priority:  int32(rule.Priority),
			CreatedAt: proto.TimestampFromTime(time.Now().UTC()),
		}
		pbRules = append(pbRules, pbRule)
	}

	return &proto.GetTenantACLRulesResponse{Rules: pbRules}, nil
}

// CheckTenantAccess checks if access is allowed between two peers
func (s *TenantPortalServiceServer) CheckTenantAccess(ctx context.Context, req *proto.CheckTenantAccessRequest) (*proto.CheckTenantAccessResponse, error) {
	if req.TenantId == "" {
		return nil, errs.InvalidArgumentE("tenant_id required")
	}
	if req.SourcePeerId == "" {
		return nil, errs.InvalidArgumentE("source_peer_id required")
	}
	if req.DestPeerId == "" {
		return nil, errs.InvalidArgumentE("dest_peer_id required")
	}
	ctx, overlayAccountID, usedScope, err := s.resolveTenantAccessAccount(ctx, req.TenantId, true)
	if err != nil {
		return nil, err
	}
	if err := requireScopeRead(usedScope, "ACL access checks"); err != nil {
		return nil, err
	}
	resourceTenantID := req.TenantId
	if usedScope != nil {
		resourceTenantID = usedScope.TenantID
	}

	// Verify both peers belong to tenant
	sourcePeer, err := s.server.GetPeer(overlayAccountID, req.SourcePeerId)
	if err != nil {
		return nil, errs.NotFoundf("source peer not found: %v", err)
	}
	if sourcePeer.AccountID != overlayAccountID {
		return nil, errs.PermissionDeniedE("source peer does not belong to this tenant")
	}
	if err := checkTagAccess(ctx, sourcePeer, resourceTenantID); err != nil {
		return nil, err
	}

	destPeer, err := s.server.GetPeer(overlayAccountID, req.DestPeerId)
	if err != nil {
		return nil, errs.NotFoundf("dest peer not found: %v", err)
	}
	if destPeer.AccountID != overlayAccountID {
		return nil, errs.PermissionDeniedE("dest peer does not belong to this tenant")
	}
	if err := checkTagAccess(ctx, destPeer, resourceTenantID); err != nil {
		return nil, err
	}

	// CheckAccess not implemented in server, return allowed by default
	// TODO: Implement actual access checking
	return &proto.CheckTenantAccessResponse{
		Allowed:       true,
		Reason:        "access check not fully implemented",
		MatchedRuleId: "",
	}, nil
}

// =============================================================================
// WEBSSH METHODS (tenant-scoped)
// =============================================================================

// CreateTenantWebSSHSession creates a new WebSSH session
func (s *TenantPortalServiceServer) CreateTenantWebSSHSession(ctx context.Context, req *proto.CreateTenantWebSSHSessionRequest) (*proto.CreateTenantWebSSHSessionResponse, error) {
	if req.TenantId == "" {
		return nil, errs.InvalidArgumentE("tenant_id required")
	}
	if req.PeerIp == "" && req.PeerId == "" {
		return nil, errs.InvalidArgumentE("peer_ip or peer_id required")
	}
	if req.Username == "" {
		return nil, errs.InvalidArgumentE("username required")
	}
	if req.PrivateKeyPassphrase != "" && req.PrivateKey == "" {
		return nil, errs.InvalidArgumentE("private_key required when private_key_passphrase is set")
	}
	ctx = s.withResolvedCallerContext(ctx, req.TenantId)

	ctx, overlayAccountID, foundPeer, usedScope, err := s.resolvePeerForAccess(ctx, req.TenantId, req.PeerId, req.PeerIp)
	if err != nil {
		return nil, errs.NotFoundf("peer not found: %v", err)
	}
	resourceTenantID := req.TenantId
	if usedScope != nil {
		resourceTenantID = usedScope.TenantID
	}
	if err := checkTagAccess(ctx, foundPeer, resourceTenantID); err != nil {
		return nil, err
	}
	peerID := foundPeer.ID
	peerIP := strings.TrimSuffix(foundPeer.AssignedIP, "/32")
	if req.PeerIp != "" && peerMatchesIP(foundPeer, req.PeerIp) {
		peerIP = strings.TrimSuffix(req.PeerIp, "/32")
	}

	// 1. Check WebSSH session limits based on account tier
	acc, err := s.server.GetAccount(overlayAccountID)
	if err != nil {
		return nil, errs.Internalf("failed to get account details: %v", err)
	}

	activeCount, err := s.server.CountWebSSHSessions(overlayAccountID)
	if err == nil {
		// Limit is based on BlockCount (29 usable IPs per /27 block)
		maxSessions := acc.BlockCount * 29
		if activeCount >= maxSessions {
			return nil, errs.FailedPreconditionf("maximum number of active WebSSH sessions reached for your tier (max: %d)", maxSessions)
		}
	} else {
		log.Warn().Err(err).Str("tenant_id", req.TenantId).Msg("Failed to check WebSSH session count from Redis")
	}

	// Use discovered SSH port if not specified (default 22)
	sshPort := req.SshPort
	if sshPort <= 0 {
		if foundPeer != nil && foundPeer.ScannedSSHPort > 0 {
			sshPort = int32(foundPeer.ScannedSSHPort)
			log.Debug().
				Str("tenant_id", req.TenantId).
				Str("peer_ip", req.PeerIp).
				Str("peer_id", peerID).
				Int32("discovered_port", sshPort).
				Msg("Using discovered SSH port from port scan")
		} else {
			sshPort = 22 // Standard SSH port
			log.Debug().
				Str("tenant_id", req.TenantId).
				Str("peer_ip", req.PeerIp).
				Str("peer_id", peerID).
				Int32("default_port", sshPort).
				Msg("Using default SSH port (no discovered port available)")
		}
	}

	rows := req.TerminalRows
	if rows <= 0 {
		rows = 24
	}
	cols := req.TerminalCols
	if cols <= 0 {
		cols = 80
	}

	log.Debug().
		Str("tenant_id", req.TenantId).
		Str("peer_id", peerID).
		Str("peer_ip", peerIP).
		Str("username", req.Username).
		Int32("ssh_port", sshPort).
		Bool("password_provided", req.Password != "").
		Bool("private_key_provided", req.PrivateKey != "").
		Bool("private_key_passphrase_provided", req.PrivateKeyPassphrase != "").
		Msg("Creating tenant WebSSH session")

	// Create WebSSH session with overlay account ID (not tenant ID!)
	// The overlayAccountID is what has the WireGuard device
	sessionID, err := s.server.CreateWebSSHSession(
		overlayAccountID, // Use overlay account ID for WireGuard device lookup
		peerID,
		peerIP,
		int(sshPort),
		req.Username,
		req.Password,
		req.PrivateKey,
		req.PrivateKeyPassphrase,
		requestUserAgent(ctx, req.UserAgent),
		int(rows),
		int(cols),
	)
	if err != nil {
		return nil, errs.Internalf("failed to create session: %v", err)
	}

	return &proto.CreateTenantWebSSHSessionResponse{
		SessionId:    sessionID,
		WebsocketUrl: "", // Deprecated: now using gRPC streaming
		Success:      true,
	}, nil
}

// GetTenantWebSSHSession returns information about a WebSSH session
func (s *TenantPortalServiceServer) GetTenantWebSSHSession(ctx context.Context, req *proto.GetTenantWebSSHSessionRequest) (*proto.GetTenantWebSSHSessionResponse, error) {
	if req.TenantId == "" {
		return nil, errs.InvalidArgumentE("tenant_id required")
	}
	if req.SessionId == "" {
		return nil, errs.InvalidArgumentE("session_id required")
	}
	ctx = s.withResolvedCallerContext(ctx, req.TenantId)

	session, err := s.server.GetWebSSHSession(req.SessionId)
	if err != nil {
		return nil, errs.NotFoundf("WebSSH session not found: %v", err)
	}

	scope := resolveScopeForAccount(ctx, req.TenantId, session.TenantID)
	if scope == nil {
		if overlayAccountID, aerr := s.getOverlayAccountID(req.TenantId); aerr != nil || overlayAccountID != session.TenantID {
			return nil, errs.PermissionDeniedE("session does not belong to this tenant")
		}
	}

	resourceTenantID := req.TenantId
	if scope != nil {
		resourceTenantID = scope.TenantID
	}

	// Verify session belongs to tenant (session was created with overlay account ID)
	if scope == nil && session.TenantID == "" {
		return nil, errs.PermissionDeniedE("session does not belong to this tenant")
	}

	var peerMeta *server.PeerMetadata
	if session.PeerID != "" {
		peerMeta, _ = s.server.GetPeer(session.TenantID, session.PeerID)
	}
	if peerMeta == nil {
		peers, _ := s.server.ListPeers(session.TenantID)
		for _, p := range peers {
			if peerMatchesIP(p, session.PeerIP) {
				peerMeta = p
				break
			}
		}
	}
	if peerMeta != nil {
		if err := checkTagAccess(ctx, peerMeta, resourceTenantID); err != nil {
			return nil, err
		}
	} else {
		log.Warn().Str("session_id", req.SessionId).Str("peer_ip", session.PeerIP).Msg("WebSSH session exists but peer is no longer in tenant list (orphaned session)")
	}

	pbSession := &proto.WebSSHSession{
		Id:             session.ID,
		TenantId:       session.TenantID,
		PeerId:         session.PeerID,
		PeerIp:         session.PeerIP,
		SshPort:        int32(session.Port),
		Username:       session.Username,
		StartedAt:      proto.TimestampFromTime(session.StartedAt),
		LastActive:     proto.TimestampFromTime(session.LastActive),
		Active:         session.Status == webssh.SessionStatusActive,
		BytesSent:      session.BytesSent,
		BytesRecv:      session.BytesRecv,
		TerminalRows:   int32(session.TerminalRows),
		TerminalCols:   int32(session.TerminalCols),
		ViewerCanWrite: scope == nil || scope.Permissions.CanWrite(),
	}
	enrichWebSSHSessionSharedFlags(ctx, pbSession, resourceTenantID, session.TenantID)

	return &proto.GetTenantWebSSHSessionResponse{Session: pbSession}, nil
}

// listWebSSHSessionsForCaller aggregates WebSSH sessions across all accessible
// scopes (own + shared). Called when a session-authenticated CallerContext is present.
func (s *TenantPortalServiceServer) listWebSSHSessionsForCaller(cc *auth.CallerContext) (*proto.ListTenantWebSSHSessionsResponse, error) {
	var mu sync.Mutex
	var out []*proto.WebSSHSession
	var wg sync.WaitGroup

	for _, sc := range cc.Scopes {
		if !sc.Permissions.CanRead() {
			continue
		}
		wg.Add(1)
		go func(scope *auth.AccessScope) {
			defer wg.Done()
			sessions, err := s.server.ListWebSSHSessions(scope.AccountID)
			if err != nil {
				return
			}
			peers, _ := s.server.ListPeers(scope.AccountID)
			peerIPs := make(map[string]bool, len(peers)*2)
			peerIDs := make(map[string]bool, len(peers))
			allowedByTag := make(map[string]bool, len(peers)*2)
			for _, p := range peers {
				ip := strings.TrimSuffix(p.AssignedIP, "/32")
				peerIDs[p.ID] = true
				peerIPs[p.AssignedIP] = true
				peerIPs[ip] = true
				ok := peerMatchesTags(p, scope.Tags)
				allowedByTag[p.ID] = ok
				allowedByTag[p.AssignedIP] = ok
				allowedByTag[ip] = ok
			}
			for _, sess := range sessions {
				if sess.PeerID != "" {
					if !peerIDs[sess.PeerID] {
						continue
					}
					if len(scope.Tags) > 0 && !allowedByTag[sess.PeerID] {
						continue
					}
				} else {
					if !peerIPs[sess.PeerIP] {
						continue
					}
					if len(scope.Tags) > 0 && !allowedByTag[sess.PeerIP] {
						continue
					}
				}
				pb := &proto.WebSSHSession{
					Id:             sess.ID,
					TenantId:       sess.TenantID,
					PeerId:         sess.PeerID,
					PeerIp:         sess.PeerIP,
					SshPort:        int32(sess.Port),
					Username:       sess.Username,
					StartedAt:      proto.TimestampFromTime(sess.StartedAt),
					LastActive:     proto.TimestampFromTime(sess.LastActive),
					Active:         sess.Status == webssh.SessionStatusActive,
					BytesSent:      sess.BytesSent,
					BytesRecv:      sess.BytesRecv,
					TerminalRows:   int32(sess.TerminalRows),
					TerminalCols:   int32(sess.TerminalCols),
					ViewerCanWrite: scope.Permissions.CanWrite(),
				}
				if !scope.IsOwner {
					pb.IsShared = true
					pb.OwnerName = scope.OwnerName
				}
				mu.Lock()
				out = append(out, pb)
				mu.Unlock()
			}
		}(sc)
	}
	wg.Wait()
	return &proto.ListTenantWebSSHSessionsResponse{Sessions: out}, nil
}

// ListTenantWebSSHSessions lists all WebSSH sessions for tenant
func (s *TenantPortalServiceServer) ListTenantWebSSHSessions(ctx context.Context, req *proto.ListTenantWebSSHSessionsRequest) (*proto.ListTenantWebSSHSessionsResponse, error) {
	if req.TenantId == "" {
		return nil, errs.InvalidArgumentE("tenant_id required")
	}
	ctx = s.withHydratedCallerContext(ctx, req.TenantId)

	// Session-authenticated callers.
	if cc := auth.CallerContextFromContext(ctx); cc != nil {
		if route := auth.RequestAccessRouteFromContext(ctx); route != nil && route.Mode == auth.RequestAccessModeFocusedShared {
			scope := routeScopeFromCallerContext(cc, route)
			if scope == nil {
				return nil, errs.PermissionDeniedE("access denied: resource not accessible")
			}
			return s.listWebSSHSessionsForCaller(&auth.CallerContext{TenantID: cc.TenantID, Scopes: []*auth.AccessScope{scope}, ScopesHydrated: true})
		}
		if req.TenantId != cc.TenantID {
			scope := cc.ScopeFor(req.TenantId)
			if scope == nil {
				return nil, errs.PermissionDeniedE("access denied: resource not accessible")
			}
			return s.listWebSSHSessionsForCaller(&auth.CallerContext{TenantID: cc.TenantID, Scopes: []*auth.AccessScope{scope}, ScopesHydrated: true})
		}
		return s.listWebSSHSessionsForCaller(cc)
	}

	log.Debug().
		Str("tenant_id", req.TenantId).
		Msg(" ListTenantWebSSHSessions called")

	overlayAccountID, err := s.getOverlayAccountID(req.TenantId)
	if err != nil {
		log.Error().
			Err(err).
			Str("tenant_id", req.TenantId).
			Msg("❌ Failed to get overlay account ID")
		return nil, errs.NotFoundf("account not found: %v", err)
	}

	log.Debug().
		Str("tenant_id", req.TenantId).
		Str("overlay_account_id", overlayAccountID).
		Msg(" Overlay account ID resolved")

	// List sessions using overlay account ID (where WireGuard device exists)
	sessions, err := s.server.ListWebSSHSessions(overlayAccountID)
	if err != nil {
		log.Error().Err(err).Str("tenant_id", req.TenantId).Msg("Failed to list WebSSH sessions")
		return nil, errs.Internalf("failed to list sessions: %v", err)
	}

	log.Debug().
		Str("tenant_id", req.TenantId).
		Str("overlay_account_id", overlayAccountID).
		Int("session_count", len(sessions)).
		Msg(" WebSSH sessions retrieved from server")

	// Verify all session peer IPs belong to tenant.
	// When the caller is a tag-filtered sharee also restrict to their allowed peers.
	peers, err := s.server.ListPeers(overlayAccountID)
	if err != nil {
		log.Error().Err(err).Str("tenant_id", req.TenantId).Msg("Failed to list peers for verification")
		return nil, errs.Internalf("failed to list peers: %v", err)
	}

	// Build IP → peer map for ownership verification.
	peerIPs := make(map[string]bool)
	for _, p := range peers {
		ip := strings.TrimSuffix(p.AssignedIP, "/32")
		peerIPs[p.AssignedIP] = true
		peerIPs[ip] = true
	}

	log.Debug().
		Str("tenant_id", req.TenantId).
		Int("peer_count", len(peers)).
		Interface("peer_ips", peerIPs).
		Msg(" Peer IPs for tenant")

	pbSessions := make([]*proto.WebSSHSession, 0, len(sessions))
	for _, session := range sessions {
		log.Debug().
			Str("session_id", session.ID).
			Str("session_tenant_id", session.TenantID).
			Str("peer_ip", session.PeerIP).
			Bool("peer_ip_matches", peerIPs[session.PeerIP]).
			Msg(" Checking session")

		// Only include sessions for tenant's peers
		if !peerIPs[session.PeerIP] {
			log.Warn().
				Str("session_id", session.ID).
				Str("peer_ip", session.PeerIP).
				Msg(" Skipping session - peer IP not in tenant's peer list")
			continue
		}
		pbSessions = append(pbSessions, &proto.WebSSHSession{
			Id:           session.ID,
			TenantId:     session.TenantID,
			PeerId:       session.PeerID,
			PeerIp:       session.PeerIP,
			SshPort:      int32(session.Port),
			Username:     session.Username,
			StartedAt:    proto.TimestampFromTime(session.StartedAt),
			LastActive:   proto.TimestampFromTime(session.LastActive),
			Active:       session.Status == webssh.SessionStatusActive,
			BytesSent:    session.BytesSent,
			BytesRecv:    session.BytesRecv,
			TerminalRows: int32(session.TerminalRows),
			TerminalCols: int32(session.TerminalCols),
			WebsocketUrl: "", // Deprecated: now using gRPC streaming
		})
	}

	log.Debug().
		Str("tenant_id", req.TenantId).
		Int("filtered_session_count", len(pbSessions)).
		Msg(" Returning filtered WebSSH sessions")

	return &proto.ListTenantWebSSHSessionsResponse{
		Sessions: pbSessions,
	}, nil
}

// DisconnectTenantWebSSHSession disconnects a WebSSH session
func (s *TenantPortalServiceServer) DisconnectTenantWebSSHSession(ctx context.Context, req *proto.DisconnectTenantWebSSHSessionRequest) (*proto.DisconnectTenantWebSSHSessionResponse, error) {
	if req.TenantId == "" {
		return nil, errs.InvalidArgumentE("tenant_id required")
	}
	if req.SessionId == "" {
		return nil, errs.InvalidArgumentE("session_id required")
	}
	ctx = s.withResolvedCallerContext(ctx, req.TenantId)

	// Verify session belongs to tenant before disconnecting
	session, err := s.server.GetWebSSHSession(req.SessionId)
	if err != nil {
		return nil, errs.NotFoundf("WebSSH session not found: %v", err)
	}

	scope := resolveScopeForAccount(ctx, req.TenantId, session.TenantID)
	if scope == nil {
		overlayAccountID, aerr := s.getOverlayAccountID(req.TenantId)
		if aerr != nil || session.TenantID != overlayAccountID {
			return nil, errs.PermissionDeniedE("session does not belong to this tenant")
		}
	}
	resourceTenantID := req.TenantId
	if scope != nil {
		resourceTenantID = scope.TenantID
	}

	// Session was created with overlay account ID
	if scope == nil && session.TenantID == "" {
		return nil, errs.PermissionDeniedE("session does not belong to this tenant")
	}

	// Enforce tag filter: look up the peer by session's PeerIP and verify it's
	// within the caller's allowed tag scope (no-op for owners and unfiltered scopes).
	if scope != nil && !scope.IsOwner && len(scope.Tags) > 0 {
		if session.PeerID != "" {
			if peerMeta, perr := s.server.GetPeer(session.TenantID, session.PeerID); perr == nil {
				if err := checkTagAccess(ctx, peerMeta, resourceTenantID); err != nil {
					return nil, err
				}
			}
		} else {
			peers, _ := s.server.ListPeers(session.TenantID)
			for _, p := range peers {
				if peerMatchesIP(p, session.PeerIP) {
					if err := checkTagAccess(ctx, p, resourceTenantID); err != nil {
						return nil, err
					}
					break
				}
			}
		}
	}

	err = s.server.DisconnectWebSSHSession(req.SessionId)
	if err != nil {
		return &proto.DisconnectTenantWebSSHSessionResponse{
			Success: false,
		}, errs.Internalf("Failed to disconnect session: %v", err)
	}

	// Unregister from Redis (routing and active tracking)
	s.server.UnregisterWebSSHSession(session.TenantID, req.SessionId)

	return &proto.DisconnectTenantWebSSHSessionResponse{
		Success: true,
	}, nil
}

// =============================================================================
// Session Management
// =============================================================================

// ListTenantSessions returns all active sessions for the authenticated tenant
func (s *TenantPortalServiceServer) ListTenantSessions(ctx context.Context, req *proto.ListTenantSessionsRequest) (*proto.ListTenantSessionsResponse, error) {
	if req.TenantId == "" {
		return nil, errs.InvalidArgumentE("tenant_id required")
	}

	// Get current session ID from CallContext (to mark as "current")
	currentSessionID := auth.CallerSessionToken(ctx)

	// Get all sessions for this tenant
	sessions, err := s.tenantRegistry.GetTenantSessions(req.TenantId)
	if err != nil {
		log.Error().Err(err).Str("tenant_id", req.TenantId).Msg("Failed to list sessions")
		return nil, errs.Internalf("failed to list sessions: %v", err)
	}

	// Convert to proto format
	var protoSessions []*proto.TenantSessionInfo
	for _, sess := range sessions {
		// Debug: Log raw session data
		log.Debug().
			Str("session_id", sess.SessionID).
			Str("raw_user_agent", sess.UserAgent).
			Int("user_agent_len", len(sess.UserAgent)).
			Str("ip_address", sess.IPAddress).
			Msg(" ListTenantSessions: Raw session data")

		parsed := sess.GetParsedUserAgent()

		// Debug: Log parsed result
		log.Debug().
			Str("session_id", sess.SessionID).
			Str("parsed_browser", parsed.Browser).
			Str("parsed_os", parsed.OS).
			Msg(" ListTenantSessions: Parsed User-Agent result")

		protoSession := &proto.TenantSessionInfo{
			SessionId:      sess.SessionID,
			IpAddress:      sess.IPAddress,
			Browser:        parsed.Browser,
			BrowserVersion: parsed.BrowserVersion,
			Os:             parsed.OS,
			DeviceType:     parsed.DeviceType,
			CreatedAt:      proto.TimestampFromTime(sess.CreatedAt),
			LastActivity:   proto.TimestampFromTime(sess.LastActivity),
			ExpiresAt:      proto.TimestampFromTime(sess.ExpiresAt),
			IsCurrent:      sess.SessionID == currentSessionID,
		}
		protoSessions = append(protoSessions, protoSession)
	}

	log.Debug().
		Str("tenant_id", req.TenantId).
		Int("session_count", len(protoSessions)).
		Msg(" Listed tenant sessions")

	return &proto.ListTenantSessionsResponse{
		Sessions:         protoSessions,
		CurrentSessionId: currentSessionID,
	}, nil
}

// DeleteTenantSession removes a specific session for the authenticated tenant
func (s *TenantPortalServiceServer) DeleteTenantSession(ctx context.Context, req *proto.DeleteTenantSessionRequest) (*proto.DeleteTenantSessionResponse, error) {
	if req.TenantId == "" {
		return nil, errs.InvalidArgumentE("tenant_id required")
	}
	if req.SessionId == "" {
		return nil, errs.InvalidArgumentE("session_id required")
	}

	// Get current session ID from CallContext to prevent deleting own session.
	currentSessionID := auth.CallerSessionToken(ctx)

	// Prevent deleting current session (use logout instead)
	if req.SessionId == currentSessionID {
		return &proto.DeleteTenantSessionResponse{
			Success: false,
			Message: "Cannot delete current session. Use logout instead.",
		}, nil
	}

	// Delete the session (with ownership verification)
	if err := s.tenantRegistry.DeleteTenantSession(req.TenantId, req.SessionId); err != nil {
		log.Error().Err(err).
			Str("tenant_id", req.TenantId).
			Str("session_id", req.SessionId).
			Msg("Failed to delete session")
		return &proto.DeleteTenantSessionResponse{
			Success: false,
			Message: err.Error(),
		}, nil
	}

	log.Debug().
		Str("tenant_id", req.TenantId).
		Str("session_id", req.SessionId[:min(16, len(req.SessionId))]+"...").
		Msg("🔒 Session deleted")

	return &proto.DeleteTenantSessionResponse{
		Success: true,
		Message: "Session deleted successfully",
	}, nil
}

// =============================================================================
// Configuration Endpoints
// =============================================================================

// GetEndpointsConfig returns centralized service endpoint configuration for the frontend.
// This eliminates hardcoded endpoints throughout the application.
func (s *TenantPortalServiceServer) GetEndpointsConfig(ctx context.Context, req *proto.GetEndpointsConfigRequest) (*proto.GetEndpointsConfigResponse, error) {
	log.Debug().Msg(" GetEndpointsConfig called")

	// Return empty strings if config not available (graceful degradation)
	if s.config == nil {
		log.Warn().Msg(" Config not available in TenantPortalService")
		return &proto.GetEndpointsConfigResponse{}, nil
	}

	// Get WireGuard port from network config
	wireguardPort := s.config.Network.SharedPort
	if wireguardPort == 0 {
		wireguardPort = 51820 // Default WireGuard port
	}

	// Use configured endpoints or fall back to network.server_endpoint
	wireguardServer := s.config.Endpoints.WireguardServer
	if wireguardServer == "" {
		wireguardServer = s.config.Network.ServerEndpoint
		log.Debug().
			Str("fallback_endpoint", wireguardServer).
			Msg(" Using network.server_endpoint as WireGuard server (no endpoints.wireguard_server configured)")
	}

	response := &proto.GetEndpointsConfigResponse{
		WinboxServer:    s.config.Endpoints.WinboxServer,
		WireguardServer: wireguardServer,
		WireguardPort:   int32(wireguardPort),
	}

	log.Debug().
		Str("winbox_server", response.WinboxServer).
		Str("wireguard_server", response.WireguardServer).
		Int32("wireguard_port", response.WireguardPort).
		Msg(" Returning endpoint configuration")

	return response, nil
}

// calculateMaxPeersForAccount returns the device cap for an account, taking
// the MaxPeers field as the source of truth. Replaces the old AccLevel-based
// helper (Phase 2: billing removed).
func calculateMaxPeersForAccount(acc *account.Account) int {
	if acc == nil {
		return 0
	}
	if acc.MaxPeers > 0 {
		return acc.MaxPeers
	}
	return acc.BlockCount * 29
}

// Helper function to calculate max Winbox sessions based on account level
func calculateMaxWinboxSessionsForAccountLevel(acc *account.Account) int {
	if acc == nil {
		return 10
	}
	// 29 usable IPs per /27 block
	return acc.BlockCount * 29
}

// winboxSessionToProto converts a models.WinboxSession to proto.WinboxSession
func winboxSessionToProto(accountID, peerID string, session *server.WinboxSession) *proto.WinboxSession {
	pbSession := &proto.WinboxSession{
		Id:                  session.ID,
		AccountId:           accountID,
		PeerId:              peerID,
		Name:                session.Name,
		RouterIp:            session.RouterIP,
		AccessToken:         session.AccessToken,
		PasswordToken:       session.PasswordToken,
		EncryptedUsername:   session.EncryptedUsername,
		EncryptedPassword:   session.EncryptedPassword,
		AuthMethod:          session.AuthMethod,
		AllowedClientIps:    session.AllowedClientIPs,
		CredentialsValid:    session.CredentialsValid,
		ValidationError:     session.ValidationError,
		Enabled:             session.Enabled,
		RouterosApiVerified: session.RouterOSAPIVerified,
		RouterosApiError:    session.RouterOSAPIError,
		RouterosApiPort:     int32(session.RouterOSAPIPort),
		RouterosApiTls:      session.RouterOSAPITLS,
	}

	if !session.LastValidated.IsZero() {
		pbSession.LastValidated = proto.TimestampFromTime(session.LastValidated)
	}
	if !session.RouterOSAPILastValidated.IsZero() {
		pbSession.RouterosApiLastValidated = proto.TimestampFromTime(session.RouterOSAPILastValidated)
	}
	if !session.LastConnected.IsZero() {
		pbSession.LastConnected = proto.TimestampFromTime(session.LastConnected)
	}
	if !session.CreatedAt.IsZero() {
		pbSession.CreatedAt = proto.TimestampFromTime(session.CreatedAt)
	}
	if !session.UpdatedAt.IsZero() {
		pbSession.UpdatedAt = proto.TimestampFromTime(session.UpdatedAt)
	}

	return pbSession
}

// generateAccessToken creates a cryptographically secure access token (32 bytes = 64 hex chars)
func generateAccessToken() (string, error) {
	bytes := make([]byte, 32)
	if _, err := rand.Read(bytes); err != nil {
		return "", err
	}
	return hex.EncodeToString(bytes), nil
}

// maybeTimestamp converts a *time.Time to *proto.Timestamp, returning nil if input is nil
func maybeTimestamp(t *time.Time) *proto.Timestamp {
	if t == nil {
		return nil
	}
	return proto.TimestampFromTime(*t)
}

// getPeerScanData retrives scan results from Redis and converts to proto, falling back to DB if needed
func (s *TenantPortalServiceServer) getPeerScanData(peer *server.PeerMetadata) ([]*proto.OpenPort, *proto.OSFingerprint) {
	scanResult, err := s.server.GetPeerScanResult(peer.ID)
	if err != nil || scanResult == nil {
		if err != nil {
			log.Warn().Err(err).Str("peer_id", peer.ID).Msg("Failed to retrieve scan result from Redis, checking DB fallback")
		}
		// Fallback to the database-provided cache on the peer object
		if len(peer.CachedPortScanJSON) > 0 {
			err = json.Unmarshal(peer.CachedPortScanJSON, &scanResult)
			if err != nil {
				log.Warn().Err(err).Str("peer_id", peer.ID).Msg("Failed to unmarshal DB cached port scan result")
			}
		}
	}

	if scanResult == nil {
		return nil, nil // No result
	}

	var openPorts []*proto.OpenPort
	for _, portResult := range scanResult.Ports {
		// Filter UDP ports
		if portResult.Protocol == "udp" {
			if portResult.State != "open" {
				continue
			}
			if portResult.Service == "udp/unknown" || portResult.Service == "unknown" || portResult.Service == "" {
				continue
			}
		} else {
			if portResult.State != "open" {
				continue
			}
		}

		openPorts = append(openPorts, &proto.OpenPort{
			Port:      int32(portResult.Port),
			Protocol:  portResult.Protocol,
			Service:   portResult.Service,
			Banner:    portResult.Banner,
			RttMs:     float32(portResult.RTT.Seconds() * 1000),
			IsWebpage: portResult.IsWebPage,
		})
	}

	var fingerprint *proto.OSFingerprint
	if fp := scanResult.Fingerprint; fp != nil {
		fingerprint = &proto.OSFingerprint{
			OsFamily:      fp.OSFamily,
			OsVersion:     fp.OSVersion,
			Vendor:        fp.Vendor,
			DeviceType:    fp.DeviceType,
			Model:         fp.Model,
			Confidence:    int32(fp.Confidence),
			DetectionInfo: fp.DetectionInfo,
			Hostname:      fp.Hostname,
		}
	}

	return openPorts, fingerprint
}

// ============================================================
// TEAM / ACCESS SHARING RPC IMPLEMENTATIONS
// ============================================================

const (
	shareInviteExpiry   = 7 * 24 * time.Hour // 7 days
	shareResendCooldown = 30 * time.Minute   // rate limit: 1 resend per 30 min
)

func (s *TenantPortalServiceServer) consoleBaseURL() string {
	if s.config != nil && s.config.Hooks.BaseURL != "" {
		base := strings.TrimSuffix(s.config.Hooks.BaseURL, "/hooks")
		base = strings.TrimSuffix(base, "/hooks/")
		return base
	}
	return "https://console.wantastic.app"
}
