package auth

import (
	"WantasticCore/internal/errs"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"strings"
	"time"

	pb "WantasticCore/internal/types"
	"WantasticCore/internal/tenant"

	"github.com/redis/go-redis/v9"
	"github.com/rs/zerolog/log"
)

// ─── Permission table ─────────────────────────────────────────────────────────
//
// methodPermission maps a gRPC method suffix to the required permission key.
// Suffix form (no leading slash) matches regardless of proto package prefix.
// Methods absent from this map are not intercepted for auth purposes.
//
// Permission keys:
//   "devices_read"  — view peers, topology, ACL, stats, ping
//   "devices_write" — manage peers, Winbox, WebSSH, ACL (implies read)
//
// List/aggregate ops (ListTenantPeers etc.) are read ops — the interceptor
// attaches CallerContext and lets the service handler aggregate across scopes.

var methodPermission = map[string]string{
	// ── PeerService ───────────────────────────────────────────────────────────
	"PeerService/PingPeer":             "devices_read",
	"PeerService/GetPeer":              "devices_read",
	"PeerService/ListPeers":            "devices_read",
	"PeerService/GetPeerConfig":        "devices_read",
	"PeerService/GetPeerStats":         "devices_read",
	"PeerService/StartPortScan":        "devices_write",
	"PeerService/StopPortScan":         "devices_write",
	"PeerService/PausePortScan":        "devices_write",
	"PeerService/ResumePortScan":       "devices_write",
	"PeerService/StreamPortScanStatus": "devices_write",
	"PeerService/SetWinboxCredentials": "devices_write",
	"PeerService/CreateWinboxSession":  "devices_write",
	"PeerService/CreateWebSSHSession":  "devices_write",

	// ── TenantPortalService ───────────────────────────────────────────────────
	"TenantPortalService/ListTenantPeers":               "devices_read",
	"TenantPortalService/GetTenantPeer":                 "devices_read",
	"TenantPortalService/GetTenantPeerConfig":           "devices_read",
	"TenantPortalService/GetTenantPeerStats":            "devices_read",
	"TenantPortalService/PingTenantPeer":                "devices_read",
	"TenantPortalService/GetTenantTopology":             "devices_read",
	"TenantPortalService/GetTenantACLRules":             "devices_read",
	"TenantPortalService/CheckTenantAccess":             "devices_read",
	"TenantPortalService/AddTenantPeer":                 "devices_write",
	"TenantPortalService/RemoveTenantPeer":              "devices_write",
	"TenantPortalService/UpdateTenantPeer":              "devices_write",
	"TenantPortalService/UpdatePeerNotes":               "devices_write",
	"TenantPortalService/BatchUpdatePeers":              "devices_write",
	"TenantPortalService/SetPeerNotification":           "devices_write",
	"TenantPortalService/DisableAllPeerNotifications":   "devices_write",
	"TenantPortalService/ListTenantWinboxSessions":      "devices_write",
	"TenantPortalService/GetTenantWinboxSession":        "devices_write",
	"TenantPortalService/CreateTenantWinboxSession":     "devices_write",
	"TenantPortalService/UpdateTenantWinboxSession":     "devices_write",
	"TenantPortalService/DeleteTenantWinboxSession":     "devices_write",
	"TenantPortalService/ClearTenantWinboxCredentials":  "devices_write",
	"TenantPortalService/ListTenantWebSSHSessions":      "devices_write",
	"TenantPortalService/GetTenantWebSSHSession":        "devices_write",
	"TenantPortalService/CreateTenantWebSSHSession":     "devices_write",
	"TenantPortalService/DisconnectTenantWebSSHSession": "devices_write",
	"TenantPortalService/AddTenantACLRule":              "devices_write",
	"TenantPortalService/RemoveTenantACLRule":           "devices_write",
}

// portalListMethods are TenantPortalService ops where the service handler
// aggregates results across all CallerContext scopes. The interceptor attaches
// CallerContext and passes through — no per-tenant-ID access check here.
var portalListMethods = map[string]struct{}{
	"TenantPortalService/ListTenantPeers":          {},
	"TenantPortalService/ListTenantWinboxSessions": {},
	"TenantPortalService/ListTenantWebSSHSessions": {},
	"TenantPortalService/GetTenantTopology":        {},
	"TenantPortalService/GetTenantACLRules":        {},
}

const callerContextCacheTTL = 30 * time.Second
const callerContextTokenMinimalCachePrefix = "shared_access:caller_ctx:token:min:"
const callerContextTokenFullCachePrefix = "shared_access:caller_ctx:token:full:"
const callerContextTenantMinimalCachePrefix = "shared_access:caller_ctx:tenant:min:"
const callerContextTenantFullCachePrefix = "shared_access:caller_ctx:tenant:full:"

const (
	MetadataCallerTenantID       = "x-caller-tenant-id"
	MetadataFocusedShareID       = "x-focused-share-id"
	MetadataFocusedOwnerTenantID = "x-focused-owner-tenant-id"
)

// ─── Types ────────────────────────────────────────────────────────────────────

// AccessScope describes one account bucket the caller can access.
// Tags == nil means all peers in that account are accessible; a non-nil
// slice restricts access to only peers that carry at least one matching tag.
type AccessScope struct {
	ShareID     string                  // accepted share ID when this is a shared scope
	TenantID    string                  // resource-owner's tenant ID
	AccountID   string                  // resolved overlay account ID
	Tags        []string                // nil = all peers; non-nil = tag-filtered
	Permissions tenant.SharePermissions // what operations are permitted
	IsOwner     bool                    // true when caller owns this account
	OwnerName   string                  // display name of the resource owner (for UI labelling)
}

// CallerContext is the resolved identity + access picture for an RPC caller.
// It is attached to the request context by the interceptor and read by
// service handlers via CallerContextFromContext.
type CallerContext struct {
	TenantID       string         // authenticated caller's own tenant ID
	Scopes         []*AccessScope // [0] = caller's own scope when they have an overlay account
	ScopesHydrated bool           // true once accepted shares have been resolved
}

type RequestAccessMode string

const (
	RequestAccessModeUnknown       RequestAccessMode = "unknown"
	RequestAccessModeOwner         RequestAccessMode = "owner"
	RequestAccessModeAggregate     RequestAccessMode = "aggregate"
	RequestAccessModeFocusedShared RequestAccessMode = "focused_shared"
)

// RequestAccessRoute is a lightweight routing hint produced by middleware so
// handlers can tell whether the request is operating on the caller's own
// resources, a focused shared-owner view, or an aggregate own+shared view.
type RequestAccessRoute struct {
	CallerTenantID  string
	RequestTenantID string
	TargetTenantID  string
	FocusedShareID  string
	Mode            RequestAccessMode
}

// ScopeFor returns the AccessScope for the given owner tenant ID, or nil.
func (cc *CallerContext) ScopeFor(tenantID string) *AccessScope {
	for _, s := range cc.Scopes {
		if s.TenantID == tenantID {
			return s
		}
	}
	return nil
}

// ScopeForAccount returns the AccessScope for the given overlay account ID, or nil.
func (cc *CallerContext) ScopeForAccount(accountID string) *AccessScope {
	for _, s := range cc.Scopes {
		if s.AccountID == accountID {
			return s
		}
	}
	return nil
}

// ScopeForShare returns the AccessScope for the given accepted share ID, or nil.
func (cc *CallerContext) ScopeForShare(shareID string) *AccessScope {
	for _, s := range cc.Scopes {
		if s.ShareID == shareID {
			return s
		}
	}
	return nil
}

// ─── Context key ──────────────────────────────────────────────────────────────

type ctxKey string

const callerCtxKey ctxKey = "__caller_context"
const requestAccessRouteKey ctxKey = "__request_access_route"

// CallerContextFromContext returns the CallerContext attached by the interceptor,
// or nil when no session-based caller was identified (API key / internal calls).
func CallerContextFromContext(ctx context.Context) *CallerContext {
	if ctx == nil {
		return nil
	}
	if v, ok := ctx.Value(callerCtxKey).(*CallerContext); ok {
		return v
	}
	return nil
}

// WithCallerContext returns a copy of ctx with cc attached.
// Intended for use in tests and middleware adapters.
func WithCallerContext(ctx context.Context, cc *CallerContext) context.Context {
	return context.WithValue(ctx, callerCtxKey, cc)
}

func RequestAccessRouteFromContext(ctx context.Context) *RequestAccessRoute {
	if ctx == nil {
		return nil
	}
	if v, ok := ctx.Value(requestAccessRouteKey).(*RequestAccessRoute); ok {
		return v
	}
	return nil
}

func WithRequestAccessRoute(ctx context.Context, route *RequestAccessRoute) context.Context {
	if route == nil {
		return ctx
	}
	return context.WithValue(ctx, requestAccessRouteKey, route)
}

// ─── buildCallerContext ───────────────────────────────────────────────────────

// BuildMinimalCallerContextForTenant resolves only the caller's own scope from
// a known tenant ID. This is the fast path used by middleware so owner
// requests do not pay for share expansion unless the request needs it.
func BuildMinimalCallerContextForTenant(reg tenant.Registry, callerTenantID string) (*CallerContext, error) {
	if reg == nil || callerTenantID == "" {
		return nil, nil
	}

	cc := &CallerContext{
		TenantID:       callerTenantID,
		ScopesHydrated: false,
	}

	// Caller's own scope — full permissions, no tag filter.
	if t, err := reg.GetTenant(callerTenantID); err == nil {
		if t.OverlayAccountID != "" {
			cc.Scopes = append(cc.Scopes, &AccessScope{
				TenantID:    callerTenantID,
				AccountID:   t.OverlayAccountID,
				Permissions: ownerPermissions(),
				IsOwner:     true,
			})
			log.Debug().Str("tenant", callerTenantID).Str("account", t.OverlayAccountID).Msg("[auth] own scope added")
		} else {
			log.Warn().Str("tenant", callerTenantID).Msg("[auth] caller has no overlay_account_id")
		}
	} else {
		log.Debug().Err(err).Str("tenant", callerTenantID).Msg("[auth] GetTenant failed for caller")
	}

	return cc, nil
}

// BuildCallerContextForTenant resolves the caller's full access picture from a
// known tenant ID. With the Team / access-share feature removed, the
// "hydrated" path is now identical to the minimal path — kept here so the
// CallerContext API stays the same for the rest of the codebase.
func BuildCallerContextForTenant(reg tenant.Registry, callerTenantID string) (*CallerContext, error) {
	cc, err := BuildMinimalCallerContextForTenant(reg, callerTenantID)
	if err != nil || cc == nil {
		return cc, err
	}
	cc.ScopesHydrated = true
	return cc, nil
}

// ownerPermissions returns full permissions for an account owner.
func ownerPermissions() tenant.SharePermissions {
	return tenant.SharePermissions{DevicesRead: true, DevicesWrite: true}
}

// ─── Internal helpers ─────────────────────────────────────────────────────────

func hashedCacheKey(prefix, value string) string {
	sum := sha256.Sum256([]byte(value))
	return prefix + hex.EncodeToString(sum[:])
}

func loadCallerContextFromCache(ctx context.Context, redisClient *redis.Client, cacheKey string) (*CallerContext, error) {
	if redisClient == nil || cacheKey == "" {
		return nil, nil
	}

	data, err := redisClient.Get(ctx, cacheKey).Bytes()
	if err != nil {
		if err == redis.Nil {
			return nil, nil
		}
		return nil, err
	}

	var cc CallerContext
	if err := json.Unmarshal(data, &cc); err != nil {
		_ = redisClient.Del(ctx, cacheKey).Err()
		return nil, err
	}
	return &cc, nil
}

func storeCallerContextInCache(ctx context.Context, redisClient *redis.Client, cacheKey string, cc *CallerContext) {
	if redisClient == nil || cacheKey == "" || cc == nil {
		return
	}

	data, err := json.Marshal(cc)
	if err != nil {
		return
	}
	if err := redisClient.Set(ctx, cacheKey, data, callerContextCacheTTL).Err(); err != nil {
		log.Debug().Err(err).Msg("[auth] failed to cache CallerContext")
	}
}

func callerContextTokenCacheKey(sessionToken string, requireFull bool) string {
	if requireFull {
		return hashedCacheKey(callerContextTokenFullCachePrefix, sessionToken)
	}
	return hashedCacheKey(callerContextTokenMinimalCachePrefix, sessionToken)
}

func callerContextTenantCacheKey(callerTenantID string, requireFull bool) string {
	if requireFull {
		return callerContextTenantFullCachePrefix + callerTenantID
	}
	return callerContextTenantMinimalCachePrefix + callerTenantID
}

// ResolveCallerContextForTenant returns either the caller's minimal own scope
// or the fully hydrated own+shared scope graph, using Redis as a short-lived
// cache for both modes.
func ResolveCallerContextForTenant(
	ctx context.Context,
	reg tenant.Registry,
	redisClient *redis.Client,
	callerTenantID string,
	requireFull bool,
) (*CallerContext, error) {
	if reg == nil || callerTenantID == "" {
		return nil, nil
	}

	cacheKey := callerContextTenantCacheKey(callerTenantID, requireFull)
	if cachedCC, err := loadCallerContextFromCache(ctx, redisClient, cacheKey); err == nil && cachedCC != nil {
		if !requireFull || cachedCC.ScopesHydrated {
			log.Debug().
				Str("caller_tenant_id", callerTenantID).
				Bool("require_full", requireFull).
				Int("scope_count", len(cachedCC.Scopes)).
				Msg("[auth] caller context cache hit")
			return cachedCC, nil
		}
		if redisClient != nil {
			_ = redisClient.Del(ctx, cacheKey).Err()
		}
	} else if err != nil {
		log.Debug().Err(err).Str("caller_tenant_id", callerTenantID).Bool("require_full", requireFull).Msg("[auth] caller context cache read failed")
	}

	var (
		cc  *CallerContext
		err error
	)
	if requireFull {
		cc, err = BuildCallerContextForTenant(reg, callerTenantID)
	} else {
		cc, err = BuildMinimalCallerContextForTenant(reg, callerTenantID)
	}
	if err != nil || cc == nil {
		return cc, err
	}
	storeCallerContextInCache(ctx, redisClient, cacheKey, cc)
	return cc, nil
}

// enforceOrigin returns PermissionDenied if the request origin is not in the
// allowedSet. When allowedSet is nil (no config), all origins are allowed.
// Origin is read from the CallContext's OriginIP-adjacent metadata channel —
// in the post-Stage-2 world, in-process callers don't carry an HTTP Origin
// header through to the dispatch chain, so this is a no-op when there is no
// configured allowlist (which is the production reality).
func enforceOrigin(ctx context.Context, allowedSet map[string]struct{}) error {
	_ = ctx
	if allowedSet == nil {
		return nil
	}
	// Stage 2: callers no longer travel through a gRPC server that surfaces
	// an Origin header into ctx. If an allowlist is configured we currently
	// have no signal to check against, so we allow the call. The portal's
	// HTTP layer is responsible for origin enforcement upstream.
	return nil
}

// extractToken pulls the session token from the CallContext attached to ctx.
// The token is populated by the portal's WebSocket dispatcher (or test set-up)
// via auth.WithCallContext before the in-process service handler is invoked.
func extractToken(ctx context.Context) string {
	return CallerSessionToken(ctx)
}

func resolveCallerContextFromSession(
	ctx context.Context,
	reg tenant.Registry,
	redisClient *redis.Client,
	sessionToken string,
	requireFull bool,
) (*CallerContext, error) {
	if reg == nil || sessionToken == "" {
		return nil, nil
	}

	tokenCacheKey := callerContextTokenCacheKey(sessionToken, requireFull)
	if cachedCC, err := loadCallerContextFromCache(ctx, redisClient, tokenCacheKey); err == nil && cachedCC != nil {
		if !requireFull || cachedCC.ScopesHydrated {
			log.Debug().
				Bool("require_full", requireFull).
				Int("scope_count", len(cachedCC.Scopes)).
				Msg("[auth] caller context session cache hit")
			return cachedCC, nil
		}
		if redisClient != nil {
			_ = redisClient.Del(ctx, tokenCacheKey).Err()
		}
	} else if err != nil {
		log.Debug().Err(err).Bool("require_full", requireFull).Msg("[auth] caller context session cache read failed")
	}

	callerTenantID, err := reg.ValidateSession(sessionToken)
	if err != nil {
		log.Debug().Err(err).Msg("[auth] ValidateSession failed")
		return nil, err
	}
	if callerTenantID == "" {
		return nil, errs.UnauthenticatedE("session not found")
	}

	cc, err := ResolveCallerContextForTenant(ctx, reg, redisClient, callerTenantID, requireFull)
	if err != nil || cc == nil {
		return cc, err
	}
	storeCallerContextInCache(ctx, redisClient, tokenCacheKey, cc)
	return cc, nil
}

// attachCallerContext extracts the session token, resolves the requested caller
// access mode, and returns a new context with it attached. Returns (ctx, nil,
// nil) when no token is present (API key / internal calls). Returns an error
// only on invalid sessions.
func attachCallerContext(ctx context.Context, reg tenant.Registry, redisClient *redis.Client, requireFull bool) (context.Context, *CallerContext, error) {
	if reg == nil {
		return ctx, nil, nil
	}
	token := extractToken(ctx)
	if token == "" {
		return ctx, nil, nil
	}

	preview := token
	if len(preview) > 8 {
		preview = preview[:8] + "..."
	}
	log.Debug().Str("token_preview", preview).Msg("[auth] session token found")

	cc, err := resolveCallerContextFromSession(ctx, reg, redisClient, token, requireFull)
	if err != nil {
		return ctx, nil, errs.UnauthenticatedE("invalid session")
	}
	if cc == nil {
		return ctx, nil, nil
	}
	return context.WithValue(ctx, callerCtxKey, cc), cc, nil
}

func hydrateCallerContext(ctx context.Context, reg tenant.Registry, redisClient *redis.Client) (context.Context, *CallerContext, error) {
	cc := CallerContextFromContext(ctx)
	if cc == nil {
		return attachCallerContext(ctx, reg, redisClient, true)
	}
	if cc.ScopesHydrated {
		return ctx, cc, nil
	}

	token := extractToken(ctx)
	if token != "" {
		fullCC, err := resolveCallerContextFromSession(ctx, reg, redisClient, token, true)
		if err != nil {
			return ctx, nil, errs.UnauthenticatedE("invalid session")
		}
		if fullCC != nil {
			return context.WithValue(ctx, callerCtxKey, fullCC), fullCC, nil
		}
	}

	if cc.TenantID == "" {
		return ctx, cc, nil
	}
	fullCC, err := ResolveCallerContextForTenant(ctx, reg, redisClient, cc.TenantID, true)
	if err != nil {
		return ctx, nil, err
	}
	if fullCC == nil {
		return ctx, cc, nil
	}
	return context.WithValue(ctx, callerCtxKey, fullCC), fullCC, nil
}

// extractTargetTenant returns the tenant ID the caller wants to operate on.
// TenantPortalService requests carry TenantId directly.
// PeerService requests carry an AccountId (overlay account ID); we resolve
// the tenant from the CallerContext scope lookup.
func extractTargetTenant(req any, cc *CallerContext) string {
	type hasTenantID interface{ GetTenantId() string }
	if r, ok := req.(hasTenantID); ok {
		if tid := r.GetTenantId(); tid != "" {
			return tid
		}
	}
	// PeerService: resolve tenant ID via overlay account ID.
	type hasAccountID interface{ GetAccountId() string }
	if r, ok := req.(hasAccountID); ok {
		if aid := r.GetAccountId(); aid != "" {
			if scope := cc.ScopeForAccount(aid); scope != nil {
				return scope.TenantID
			}
			// Account ID present but no matching scope → access denied.
			// Return a sentinel so checkAccess sees an inaccessible target.
			return "__no_scope__"
		}
	}
	return ""
}

func requestTenantIDFromRequest(req any) string {
	type hasTenantID interface{ GetTenantId() string }
	if r, ok := req.(hasTenantID); ok {
		return strings.TrimSpace(r.GetTenantId())
	}
	return ""
}

func buildRequestAccessRoute(ctx context.Context, cc *CallerContext, req any, isList bool) *RequestAccessRoute {
	if cc == nil {
		return nil
	}

	requestTenantID := requestTenantIDFromRequest(req)
	targetTenantID := extractTargetTenant(req, cc)
	if targetTenantID == "__no_scope__" {
		targetTenantID = requestTenantID
	}
	if targetTenantID == "" {
		targetTenantID = requestTenantID
	}

	focusedShareID, focusedOwnerTenantID := focusedShareHintFromContext(ctx)
	if focusedShareID != "" {
		if scope := cc.ScopeForShare(focusedShareID); scope != nil && !scope.IsOwner {
			return &RequestAccessRoute{
				CallerTenantID:  cc.TenantID,
				RequestTenantID: requestTenantID,
				TargetTenantID:  scope.TenantID,
				FocusedShareID:  focusedShareID,
				Mode:            RequestAccessModeFocusedShared,
			}
		}
	}
	if focusedOwnerTenantID != "" {
		if scope := cc.ScopeFor(focusedOwnerTenantID); scope != nil && !scope.IsOwner {
			return &RequestAccessRoute{
				CallerTenantID:  cc.TenantID,
				RequestTenantID: requestTenantID,
				TargetTenantID:  scope.TenantID,
				FocusedShareID:  scope.ShareID,
				Mode:            RequestAccessModeFocusedShared,
			}
		}
	}

	mode := RequestAccessModeUnknown
	switch {
	case isList && (requestTenantID == "" || requestTenantID == cc.TenantID):
		mode = RequestAccessModeAggregate
	case targetTenantID == "" || targetTenantID == cc.TenantID:
		mode = RequestAccessModeOwner
	default:
		if scope := cc.ScopeFor(targetTenantID); scope != nil && !scope.IsOwner {
			mode = RequestAccessModeFocusedShared
		} else if requestTenantID != "" && requestTenantID != cc.TenantID {
			mode = RequestAccessModeFocusedShared
		} else {
			mode = RequestAccessModeOwner
		}
	}

	return &RequestAccessRoute{
		CallerTenantID:  cc.TenantID,
		RequestTenantID: requestTenantID,
		TargetTenantID:  targetTenantID,
		FocusedShareID:  focusedShareID,
		Mode:            mode,
	}
}

// checkAccess verifies that cc has a scope for targetTenantID with at least
// the required permission. Returns a gRPC status error on failure.
func checkAccess(cc *CallerContext, targetTenantID, required, fullMethod string) error {
	scope := cc.ScopeFor(targetTenantID)
	if scope == nil {
		log.Warn().Str("method", fullMethod).Str("caller", cc.TenantID).
			Str("target", targetTenantID).Msg("[auth] access denied: target not in caller scopes")
		return errs.PermissionDeniedE("access denied")
	}
	if !hasPermission(&scope.Permissions, required) {
		log.Warn().Str("method", fullMethod).Str("caller", cc.TenantID).
			Str("target", targetTenantID).Str("required", required).
			Msg("[auth] insufficient permissions")
		return errs.PermissionDeniedE("insufficient permissions")
	}
	log.Debug().Str("method", fullMethod).Str("caller", cc.TenantID).
		Str("target", targetTenantID).Bool("is_owner", scope.IsOwner).
		Msg("[auth] access granted")
	return nil
}

// hasPermission checks whether p grants the named permission.
// "devices_read"  — granted by DevicesRead OR DevicesWrite (write implies read).
// "devices_write" — granted only by DevicesWrite.
func hasPermission(p *tenant.SharePermissions, key string) bool {
	if p == nil {
		return false
	}
	switch key {
	case "devices_read":
		return p.DevicesRead || p.DevicesWrite
	case "devices_write":
		return p.DevicesWrite
	default:
		return false
	}
}

// methodSuffix returns the "Service/Method" portion of a full gRPC method name.
// "/overlay.v1.TenantPortalService/ListTenantPeers" → "TenantPortalService/ListTenantPeers"
func methodSuffix(fullMethod string) string {
	if i := strings.LastIndex(fullMethod, "."); i >= 0 {
		return fullMethod[i+1:]
	}
	// No package prefix — strip leading slash.
	return strings.TrimPrefix(fullMethod, "/")
}

// ─── Utilities ────────────────────────────────────────────────────────────────

// buildAllowedSet converts a slice of origin strings into a set for O(1) lookup.
func buildAllowedSet(origins []string) map[string]struct{} {
	if len(origins) == 0 {
		return nil
	}
	s := make(map[string]struct{}, len(origins))
	for _, o := range origins {
		s[o] = struct{}{}
	}
	return s
}

// ─── Kept for PeerService streaming handlers ──────────────────────────────────
//
// PeerService streaming RPCs (port scan) receive the account ID in the first
// stream message, not in metadata. The service handler uses CallerContext
// directly via ScopeForAccount after reading the first message.
// No wrapper logic needed here — CallerContext is already on the stream context.
//
// The pb import is retained for the proto type references that remain in tests.
var _ = pb.PingPeerRequest{} // keep import alive

func focusedShareHintFromContext(ctx context.Context) (shareID, ownerTenantID string) {
	return CallerFocusedShare(ctx)
}
