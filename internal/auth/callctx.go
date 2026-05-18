package auth

import (
	"context"
	"strings"
)

// callctxKey is the unexported key under which CallContext is stored on a
// context.Context value. Using an unexported struct as the key prevents
// accidental collisions with other packages.
type callctxKey struct{}

// CallContext is the per-request identity / scope picture that travels
// alongside ctx through the in-process dispatch chain. It replaces the
// gRPC metadata pairs the codebase used pre–Stage 2 of the gRPC rip.
//
// All fields are optional; absence means "not provided by the caller".
// Service handlers read this via CallContextFrom(ctx) instead of reading
// gRPC metadata pairs.
type CallContext struct {
	// TenantID is the tenant whose perspective this call runs under.
	// Set by the portal's WebSocket dispatcher after session validation.
	TenantID string

	// SessionToken is the opaque session ID. Used by handlers that need
	// to revoke or look up the originating session.
	SessionToken string

	// APIKey is set when the caller authenticated via an API key rather
	// than a session cookie. Empty for browser-driven calls.
	APIKey string

	// LinkShare is true when the caller is accessing a tenant's resources
	// via a share-link, not as the tenant themselves. Handlers use this
	// to apply read-only or scope-limited rules.
	LinkShare bool

	// FocusedShareID / FocusedOwnerTenantID describe which accepted share
	// the caller has picked in the portal. Optional context for UIs that
	// switch between "my account" and a shared account.
	FocusedShareID       string
	FocusedOwnerTenantID string

	// OriginIP / OriginUserAgent are best-effort attribution for audit logs.
	OriginIP        string
	OriginUserAgent string

	// Auth0Sub / Email / FullName / DeviceID carry trusted identity claims
	// from the portal's Auth0/OAuth2 layer down to the in-process
	// RegisterDevice handler. Pre–Stage 2 these travelled as the
	// `x-wantastic-*` gRPC metadata pairs.
	Auth0Sub string
	Email    string
	FullName string
	DeviceID string
}

// WithCallContext returns a derived context carrying cc. The previous
// value (if any) is overwritten — callers that want to layer additions
// should fetch + merge first.
func WithCallContext(ctx context.Context, cc *CallContext) context.Context {
	if cc == nil {
		return ctx
	}
	return context.WithValue(ctx, callctxKey{}, cc)
}

// CallContextFrom returns the CallContext stored on ctx, or nil if none.
// A nil return is a valid signal for "unauthenticated call".
func CallContextFrom(ctx context.Context) *CallContext {
	if v, ok := ctx.Value(callctxKey{}).(*CallContext); ok {
		return v
	}
	return nil
}

// CallerTenantID is a convenience accessor that returns the caller's
// tenant ID, or "" when none is set. Equivalent to:
//   cc := CallContextFrom(ctx); if cc != nil { return cc.TenantID }
func CallerTenantID(ctx context.Context) string {
	if cc := CallContextFrom(ctx); cc != nil {
		return strings.TrimSpace(cc.TenantID)
	}
	return ""
}

// CallerSessionToken returns the session token from the call context,
// or "" if none was attached.
func CallerSessionToken(ctx context.Context) string {
	if cc := CallContextFrom(ctx); cc != nil {
		return cc.SessionToken
	}
	return ""
}

// CallerIsLinkShare reports whether this call is being made via a share
// link rather than as the tenant themselves.
func CallerIsLinkShare(ctx context.Context) bool {
	if cc := CallContextFrom(ctx); cc != nil {
		return cc.LinkShare
	}
	return false
}

// CallerFocusedShare returns the currently-focused accepted share for
// the caller, if any. (share_id, owner_tenant_id).
func CallerFocusedShare(ctx context.Context) (shareID, ownerTenantID string) {
	if cc := CallContextFrom(ctx); cc != nil {
		return cc.FocusedShareID, cc.FocusedOwnerTenantID
	}
	return "", ""
}
