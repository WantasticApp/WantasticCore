package core

import (
	"WantasticCore/internal/errs"
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"net"
	"strings"
	"time"

	pb "WantasticCore/internal/types"
	"WantasticCore/internal/account"
	"WantasticCore/internal/auth"
	"WantasticCore/internal/server"
	"WantasticCore/internal/tenant"

	"github.com/google/uuid"
	"github.com/rs/zerolog/log"
	"golang.org/x/crypto/chacha20poly1305"
)

// WantasticServiceServer implements AuthService.
// It receives trusted identity claims from the portal layer and is
// responsible for WireGuard peer provisioning and tenant management.
type WantasticServiceServer struct {
	UnimplementedAuthService
	server         server.OverlayServer
	tenantRegistry tenant.Registry
}

// NewWantasticServiceServer creates a new WantasticServiceServer.
func NewWantasticServiceServer(
	srv server.OverlayServer,
	tenantRegistry tenant.Registry,
) *WantasticServiceServer {
	return &WantasticServiceServer{
		server:         srv,
		tenantRegistry: tenantRegistry,
	}
}

// ─────────────────────────────────────────────
// StartDeviceFlow — RFC 8628 initiation
// ─────────────────────────────────────────────

// StartDeviceFlow — handled entirely by the portal; not exposed on the core.
func (s *WantasticServiceServer) StartDeviceFlow(_ context.Context, _ *pb.StartDeviceFlowRequest) (*pb.StartDeviceFlowResponse, error) {
	return nil, errs.UnimplementedE("device flow is managed by the portal layer")
}

// ─────────────────────────────────────────────
// PollDeviceFlow — RFC 8628 token poll
// ─────────────────────────────────────────────

// PollDeviceFlow — handled entirely by the portal; not exposed on the core.
func (s *WantasticServiceServer) PollDeviceFlow(_ context.Context, _ *pb.PollDeviceFlowRequest) (*pb.PollDeviceFlowResponse, error) {
	return nil, errs.UnimplementedE("device flow is managed by the portal layer")
}

// ─────────────────────────────────────────────
// RegisterDevice — provision WireGuard peer
// ─────────────────────────────────────────────

// RegisterDevice provisions a WireGuard peer for an authenticated agent.
//
// Auth0 JWT validation is performed by the portal layer. This method receives
// trusted identity claims via gRPC metadata (only reachable by the portal over
// mTLS — never exposed on any public port):
//
//	x-wantastic-auth0-sub  — Auth0 subject (stable user ID)
//	x-wantastic-email      — verified email address
//	x-wantastic-name       — display name
//	x-wantastic-device-id  — Hashed machine ID for device tracking
//
// Flow:
//  1. Read identity from trusted gRPC metadata.
//  2. Resolve tenant: auth0_sub → email → auto-create (free tier).
//  3. Check for existing peer by device_id (duplicate detection).
//  4. Create WireGuard peer (or return existing config for duplicates).
//  5. Create a gRPC session (portal will exchange for an HTTP session cookie).
//  6. Encrypt WireGuard config with SHA256(req.Token) + req.Nonce.
//  7. Return config + internal token (unpacked by portal, not returned to agent).
func (s *WantasticServiceServer) RegisterDevice(ctx context.Context, req *pb.RegisterDeviceRequest) (*pb.RegisterDeviceResponse, error) {
	if req.Token == "" {
		return nil, errs.InvalidArgumentE("auth token is required")
	}

	// ── 1. Read trusted identity from CallContext (populated by the
	//       portal layer after Auth0/OAuth2 validation) ──────────────────
	cc := auth.CallContextFrom(ctx)
	if cc == nil {
		return nil, errs.UnauthenticatedE("missing authentication context — request must come from portal")
	}
	auth0Sub := strings.TrimSpace(cc.Auth0Sub)
	email := strings.TrimSpace(cc.Email)
	fullName := strings.TrimSpace(cc.FullName)
	deviceID := strings.TrimSpace(cc.DeviceID)
	if deviceID == "" {
		deviceID = strings.TrimSpace(req.DeviceId)
	}

	log.Debug().
		Str("email", email).
		Str("os", req.Os).
		Str("hostname", req.Hostname).
		Str("device_id", deviceID[:min(16, len(deviceID))]+"...").
		Msg("RegisterDevice: provisioning peer")

	// ── 2. Resolve tenant via trusted identity or enrollment token ─────────
	var (
		overlayAccountID  string
		tenantID          string
		tier              string
		enrollmentTokenID string
		err               error
	)
	if auth0Sub != "" && email != "" {
		overlayAccountID, tenantID, tier, err = s.resolveOrCreateTenant(auth0Sub, email, fullName)
		if err != nil {
			return nil, errs.Internalf("failed to resolve tenant: %v", err)
		}
	} else {
		tenantID, enrollmentTokenID, err = s.tenantRegistry.ValidateEnrollmentToken(req.Token)
		if err != nil {
			return nil, errs.Unauthenticatedf("invalid enrollment token: %v", err)
		}
		t, getErr := s.tenantRegistry.GetTenant(tenantID)
		if getErr != nil {
			return nil, errs.Internalf("failed to load tenant for enrollment token: %v", getErr)
		}
		overlayAccountID, err = s.ensureOverlayAccount(t)
		if err != nil {
			return nil, errs.Internalf("overlay account error: %v", err)
		}
		tier = "free"
		log.Debug().
			Str("tenant_id", tenantID).
			Str("token_id", enrollmentTokenID).
			Str("device_id", deviceID[:min(16, len(deviceID))]+"...").
			Msg("RegisterDevice: validated enrollment token")
	}

	// ── 3. Check for existing peer by device_id (duplicate detection) ─────
	if deviceID != "" {
		if existingPeer := s.findPeerByDeviceID(overlayAccountID, deviceID); existingPeer != nil {
			log.Debug().
				Str("device_id", deviceID[:min(16, len(deviceID))]+"...").
				Str("peer_key", existingPeer.WireGuardPublicKey).
				Msg("RegisterDevice: returning existing peer config for known device")
			resp, buildErr := s.buildRegisterResponse(existingPeer, req, overlayAccountID, tenantID, tier)
			if buildErr != nil {
				return nil, buildErr
			}
			if enrollmentTokenID != "" {
				if incErr := s.tenantRegistry.IncrementEnrollmentTokenUsage(enrollmentTokenID); incErr != nil {
					log.Warn().Err(incErr).Str("tenant_id", tenantID).Str("token_id", enrollmentTokenID).Msg("RegisterDevice: failed to increment enrollment token usage for existing peer")
				}
			}
			return resp, nil
		}
	}

	// ── 4. Choose peer name ────────────────────────────────────────────────
	peerName := req.Hostname
	if peerName == "" {
		peerName = fmt.Sprintf("device-%s", time.Now().Format("20060102-150405"))
	}

	// ── 5. Add WireGuard peer (atomic: limit check + IP alloc + DB) ────────
	peerInfo, err := s.server.AddPeer(overlayAccountID, peerName, "")
	if err != nil {
		log.Error().Err(err).Str("tenant_id", tenantID).Msg("RegisterDevice: AddPeer failed")
		if strings.Contains(strings.ToLower(err.Error()), "peer limit reached") {
			return nil, errs.FailedPreconditionf("%v", err)
		}
		return nil, errs.Internalf("failed to create peer: %v", err)
	}

	// Convert PeerInfo to PeerMetadata for response building
	peer := s.peerInfoToMetadata(peerInfo, overlayAccountID)

	// ── 5b. Store device_id → peer mapping for duplicate detection ────────
	if deviceID != "" {
		s.associateDeviceWithPeer(overlayAccountID, deviceID, peer.WireGuardPublicKey)
	}

	resp, err := s.buildRegisterResponse(peer, req, overlayAccountID, tenantID, tier)
	if err != nil {
		return nil, err
	}
	if enrollmentTokenID != "" {
		if incErr := s.tenantRegistry.IncrementEnrollmentTokenUsage(enrollmentTokenID); incErr != nil {
			log.Warn().Err(incErr).Str("tenant_id", tenantID).Str("token_id", enrollmentTokenID).Msg("RegisterDevice: failed to increment enrollment token usage")
		}
	}
	return resp, nil
}

// peerInfoToMetadata converts PeerInfo to PeerMetadata
func (s *WantasticServiceServer) peerInfoToMetadata(info *server.PeerInfo, accountID string) *server.PeerMetadata {
	assignedIP := ""
	if len(info.AllowedIPs) > 0 {
		assignedIP = strings.TrimSpace(strings.TrimSuffix(info.AllowedIPs[0], "/32"))
	}
	return &server.PeerMetadata{
		ID:                  info.PublicKey,
		AccountID:           accountID,
		Name:                info.Name,
		AssignedIP:          assignedIP,
		AllowedIPs:          info.AllowedIPs,
		PrivateKey:          info.PrivateKey,
		WireGuardPublicKey:  info.PublicKey,
		WireGuardPrivateKey: info.PrivateKey,
	}
}

// findPeerByDeviceID looks up an existing peer by its device ID.
// Returns nil if no peer is found for this device.
func (s *WantasticServiceServer) findPeerByDeviceID(overlayAccountID, deviceID string) *server.PeerMetadata {
	// Query all peers for this account
	peers, err := s.server.ListPeers(overlayAccountID)
	if err != nil {
		return nil
	}

	// Look for peer with matching device_id in fingerprint
	for _, peer := range peers {
		if peer.CachedPortScanJSON != nil {
			var fp map[string]interface{}
			if err := json.Unmarshal(peer.CachedPortScanJSON, &fp); err == nil {
				if devID, ok := fp["device_id"].(string); ok && devID == deviceID {
					// Found matching device
					return peer
				}
			}
		}
	}
	return nil
}

// associateDeviceWithPeer stores the device_id → peer mapping in the peer's fingerprint.
func (s *WantasticServiceServer) associateDeviceWithPeer(overlayAccountID, deviceID, peerKey string) {
	peer, err := s.server.GetPeer(overlayAccountID, peerKey)
	if err != nil {
		return
	}

	// Parse existing fingerprint or create new one
	var fp map[string]interface{}
	if peer.CachedPortScanJSON != nil {
		json.Unmarshal(peer.CachedPortScanJSON, &fp)
	}
	if fp == nil {
		fp = make(map[string]interface{})
	}

	// Store device_id for duplicate detection
	fp["device_id"] = deviceID
	fp["device_registered_at"] = time.Now().UTC().Format(time.RFC3339)

	fpBytes, _ := json.Marshal(fp)
	peer.CachedPortScanJSON = fpBytes

	// Need to get the full peer object to update
	if fullPeer, err := s.server.GetPeer(overlayAccountID, peerKey); err == nil {
		s.server.UpdatePeer(fullPeer)
	}
}

// buildRegisterResponse constructs the RegisterDeviceResponse from peer metadata.
func (s *WantasticServiceServer) buildRegisterResponse(peer *server.PeerMetadata, req *pb.RegisterDeviceRequest, overlayAccountID, tenantID, tier string) (*pb.RegisterDeviceResponse, error) {
	peerID := peer.ID
	if peer.WireGuardPublicKey != "" {
		peerID = peer.WireGuardPublicKey
	}
	if peerID == "" {
		return nil, errs.InternalE("peer public key not available for registration response")
	}

	// Always switch back to the authoritative stored peer before building the
	// welcome config so the newly created device receives the exact persisted
	// keys and routes instead of any partially populated intermediate object.
	if storedPeer, err := s.server.GetPeer(overlayAccountID, peerID); err == nil && storedPeer != nil {
		peer = storedPeer
	}

	// Persist device fingerprint asynchronously
	go func(peerKey, accID, osFamily, hostname string) {
		peer, err := s.server.GetPeer(accID, peerKey)
		if err == nil {
			var fp map[string]interface{}
			if peer.CachedPortScanJSON != nil {
				json.Unmarshal(peer.CachedPortScanJSON, &fp)
			}
			if fp == nil {
				fp = make(map[string]interface{})
			}
			fp["fingerprint"] = map[string]interface{}{
				"vendor":      "Wantastic",
				"os_family":   osFamily,
				"hostname":    hostname,
				"device_type": "Wantastic Client",
			}
			fpBytes, _ := json.Marshal(fp)
			peer.CachedPortScanJSON = fpBytes
			s.server.UpdatePeer(peer)
		}
	}(peerID, overlayAccountID, req.Os, req.Hostname)

	// Billing hook (OnDemand auto-scale)
	go s.billingHook(tenantID, "")

	// Create gRPC session — the portal will exchange this for an HTTP session cookie
	grpcSessionToken := uuid.New().String()
	if err := s.tenantRegistry.CreateSession(tenantID, grpcSessionToken, "", "Wantastic Agent", "", 30*24*time.Hour, true); err != nil {
		log.Warn().Err(err).Str("tenant_id", tenantID).Msg("RegisterDevice: failed to create gRPC session (non-fatal)")
	}

	// Get server info from the overlay account's WireGuard device
	serverPublicKey := s.server.GetServerPublicKey(overlayAccountID)
	serverEndpoint := s.server.GetServerEndpoint()
	acc, err := s.server.GetAccount(overlayAccountID)
	if err != nil {
		return nil, errs.Internalf("account not found for peer config: %v", err)
	}
	dnsServer, err := primaryDNSFromNetworks(acc.Networks)
	if err != nil {
		return nil, errs.Internalf("failed to derive tenant dns server: %v", err)
	}
	dnsServers := server.WireGuardDNSServers(dnsServer)

	if serverPublicKey == "" {
		return nil, errs.InternalE("server public key not available — WireGuard device not ready")
	}
	if serverEndpoint == "" {
		return nil, errs.InternalE("server endpoint not configured")
	}

	config, err := s.server.GetPeerConfig(overlayAccountID, peerID, serverEndpoint)
	if err != nil {
		return nil, errs.Internalf("failed to build peer config: %v", err)
	}

	// Encrypt config: key = SHA256(req.Token), nonce from req.Nonce
	hash := sha256.Sum256([]byte(req.Token))
	nonceBytes := make([]byte, 12)
	binary.LittleEndian.PutUint64(nonceBytes[:8], uint64(req.Nonce))

	aead, err := chacha20poly1305.New(hash[:])
	if err != nil {
		return nil, errs.Internalf("create cipher: %v", err)
	}
	encryptedConfig := aead.Seal(nil, nonceBytes, []byte(config), nil)

	// Pack internal token for portal (never returned to agent)
	internalToken := grpcSessionToken + "|" + tenantID + "|" + tier

	return &pb.RegisterDeviceResponse{
		Success:             true,
		Token:               internalToken,
		ServerKey:           serverPublicKey,
		Endpoint:            serverEndpoint,
		AllowedIps:          []string{peer.AssignedIP},
		PersistentKeepalive: 25,
		DnsServers:          dnsServers,
		Mtu:                 1420,
		ListenPort:          51820,
		EncryptedConfig:     encryptedConfig,
	}, nil
}

func primaryDNSFromNetworks(networks []string) (string, error) {
	if len(networks) == 0 {
		return "", fmt.Errorf("no tenant networks configured")
	}

	_, ipNet, err := net.ParseCIDR(networks[0])
	if err != nil {
		return "", err
	}

	ip := make(net.IP, len(ipNet.IP))
	copy(ip, ipNet.IP)
	if ip4 := ip.To4(); ip4 != nil {
		ip = ip4
	}

	for i := len(ip) - 1; i >= 0; i-- {
		ip[i]++
		if ip[i] != 0 {
			break
		}
	}

	return ip.String(), nil
}

func formatAgentEndpoint(endpoint string, listenPort int) string {
	if endpoint == "" {
		return ""
	}
	if _, _, err := net.SplitHostPort(endpoint); err == nil {
		return endpoint
	}
	if strings.Count(endpoint, ":") > 1 && !strings.HasPrefix(endpoint, "[") {
		return fmt.Sprintf("[%s]:%d", endpoint, listenPort)
	}
	return fmt.Sprintf("%s:%d", endpoint, listenPort)
}

// ─────────────────────────────────────────────
// Helpers
// ─────────────────────────────────────────────

// resolveOrCreateTenant finds or provisions the overlay account for an Auth0 user.
// Lookup order:
//  1. auth0_sub (most reliable — stable across email changes)
//  2. email (existing password-based account → links auth0_sub)
//  3. auto-create (new user → free tier, no password)
func (s *WantasticServiceServer) resolveOrCreateTenant(auth0Sub, email, fullName string) (overlayAccountID, tenantID, tier string, err error) {
	// ── Try by auth0_sub ───────────────────────────────────────────────────
	if t, err := s.tenantRegistry.GetTenantByAuth0Sub(auth0Sub); err == nil {
		overlayAccountID, err = s.ensureOverlayAccount(t)
		if err != nil {
			return "", "", "", fmt.Errorf("overlay account error: %w", err)
		}
		return overlayAccountID, t.ID, "free", nil
	}

	// ── Try by email (link existing account) ──────────────────────────────
	if email != "" {
		if t, err := s.tenantRegistry.GetTenantByEmail(email); err == nil {
			// Link the Auth0 sub so future look-ups are instant.
			if t.Auth0Sub == "" {
				t.Auth0Sub = auth0Sub
				if updateErr := s.tenantRegistry.UpdateTenant(t); updateErr != nil {
					log.Warn().Err(updateErr).Str("tenant_id", t.ID).Msg("RegisterDevice: failed to link auth0_sub to existing tenant")
				}
			}
			overlayAccountID, err = s.ensureOverlayAccount(t)
			if err != nil {
				return "", "", "", fmt.Errorf("overlay account error: %w", err)
			}
			return overlayAccountID, t.ID, "free", nil
		}
	}

	// ── Auto-create tenant (first time this Auth0 user registers) ─────────
	if email == "" {
		return "", "", "", fmt.Errorf("identity metadata must include an email address")
	}

	newTenantID := uuid.New().String()
	overlayAcc, err := s.server.CreateAccount(fmt.Sprintf("tenant-%s", newTenantID[:8]), account.DefaultMaxPeers)
	if err != nil {
		return "", "", "", fmt.Errorf("failed to create overlay account: %w", err)
	}

	newTenant := &tenant.Tenant{
		ID:               newTenantID,
		Email:            email,
		FullName:         fullName,
		PasswordHash:     "", // no password — Auth0 handles login
		Auth0Sub:         auth0Sub,
		OverlayAccountID: overlayAcc.ID,
		Networks:         overlayAcc.Networks,
		Status:           "active",
		CreatedAt:        time.Now().UTC(),
		UpdatedAt:        time.Now().UTC(),
	}

	if err := s.tenantRegistry.CreateTenant(newTenant); err != nil {
		// Rollback overlay account.
		_ = s.server.DeleteAccount(overlayAcc.ID)
		return "", "", "", fmt.Errorf("failed to create tenant: %w", err)
	}

	log.Debug().
		Str("tenant_id", newTenantID).
		Str("overlay_account_id", overlayAcc.ID).
		Str("email", email).
		Msg("RegisterDevice: auto-provisioned new tenant from Auth0 identity")

	return overlayAcc.ID, newTenantID, "free", nil
}

// ensureOverlayAccount checks that the tenant's overlay account exists; recreates it if missing.
func (s *WantasticServiceServer) ensureOverlayAccount(t *tenant.Tenant) (string, error) {
	if t.OverlayAccountID == "" {
		return "", fmt.Errorf("tenant %s has no overlay account", t.ID)
	}
	if _, err := s.server.GetAccount(t.OverlayAccountID); err == nil {
		return t.OverlayAccountID, nil
	}

	// Account missing (e.g. after admin DB reset) — recreate it.
	log.Warn().Str("tenant_id", t.ID).Str("old_account_id", t.OverlayAccountID).
		Msg("RegisterDevice: overlay account missing, recreating")

	newAcc, err := s.server.CreateAccount(fmt.Sprintf("tenant-%s", t.ID[:8]), account.DefaultMaxPeers)
	if err != nil {
		return "", fmt.Errorf("failed to recreate overlay account: %w", err)
	}
	t.OverlayAccountID = newAcc.ID
	t.Networks = newAcc.Networks
	if err := s.tenantRegistry.UpdateTenant(t); err != nil {
		log.Warn().Err(err).Str("tenant_id", t.ID).Msg("RegisterDevice: failed to persist new overlay account ID")
	}
	return newAcc.ID, nil
}

// billingHook expands the overlay account allocation if more peers are present
// than the current block count can serve. The Stripe-quantity integration was
// removed in Phase 2 along with the rest of the billing code.
func (s *WantasticServiceServer) billingHook(tenantID, overlayAccountID string) {
	_ = tenantID
	if overlayAccountID == "" {
		return
	}

	peers, err := s.server.ListPeers(overlayAccountID)
	if err != nil {
		log.Error().Err(err).Msg("billingHook: failed to list peers")
		return
	}
	peerCount := len(peers)

	acc, err := s.server.GetAccount(overlayAccountID)
	if err != nil {
		return
	}

	const peersPerBlock = 29
	neededBlocks := (peerCount + peersPerBlock - 1) / peersPerBlock
	if neededBlocks > acc.BlockCount {
		if _, err := s.server.AddBlockToAccount(overlayAccountID); err != nil {
			log.Error().Err(err).Msg("billingHook: failed to add block")
		}
	}
}

// getOverlayAccountID is kept as a thin helper for legacy callers.
func (s *WantasticServiceServer) getOverlayAccountID(tenantID string) (string, error) {
	t, err := s.tenantRegistry.GetTenant(tenantID)
	if err != nil {
		return "", err
	}
	if t.OverlayAccountID == "" {
		return "", fmt.Errorf("tenant has no overlay account")
	}
	return t.OverlayAccountID, nil
}

// min returns the smaller of a and b.
func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
