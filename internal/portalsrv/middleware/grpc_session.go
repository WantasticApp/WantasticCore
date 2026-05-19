package middleware

import (
	"context"
	"net/http"
	"time"

	pb "WantasticCore/internal/types"
	core "WantasticCore/internal/core"

	"github.com/rs/zerolog/log"
)

// GRPCSessionMiddleware validates sessions via the in-process AuthService.
type GRPCSessionMiddleware struct {
	authSvc      core.AuthService
	cookieName   string
	secureCookie bool
}

// SessionInfo contains validated session information from gRPC.
// Phase 3: email/phone-verification fields removed from session payload —
// admin-created tenants are verified by design.
type SessionInfo struct {
	Valid    bool
	UserType pb.UserType
	UserID   string
	TenantID string
	Tier     pb.AccountTier
	FullName string
	Email    string
}

// NewGRPCSessionMiddleware creates a new in-process auth session middleware.
func NewGRPCSessionMiddleware(authSvc core.AuthService, cookieName string) *GRPCSessionMiddleware {
	return &GRPCSessionMiddleware{
		authSvc:      authSvc,
		cookieName:   cookieName,
		secureCookie: false, // Set to true for HTTPS
	}
}

// SetSecureCookie configures whether to use secure cookies.
func (m *GRPCSessionMiddleware) SetSecureCookie(secure bool) {
	m.secureCookie = secure
}

// RequireAuth wraps an HTTP handler and requires valid gRPC session.
// Redirects to /login if session is invalid.
func (m *GRPCSessionMiddleware) RequireAuth(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		session, err := m.ValidateRequest(r)
		if err != nil || !session.Valid {
			log.Debug().Err(err).Msg("Session validation failed, redirecting to login")
			m.ClearSessionCookie(w)
			http.Redirect(w, r, "/login", http.StatusSeeOther)
			return
		}

		// Session valid - continue
		// Could add session info to request context here if needed
		next.ServeHTTP(w, r)
	}
}

// ValidateRequest validates the session from the request cookie via gRPC.
func (m *GRPCSessionMiddleware) ValidateRequest(r *http.Request) (*SessionInfo, error) {
	cookie, err := r.Cookie(m.cookieName)
	if err != nil {
		return &SessionInfo{Valid: false}, err
	}

	return m.ValidateToken(cookie.Value)
}

// ValidateToken validates a session token via gRPC AuthService.
func (m *GRPCSessionMiddleware) ValidateToken(token string) (*SessionInfo, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	resp, err := m.authSvc.ValidateSession(ctx, &pb.ValidateSessionRequest{
		SessionToken: token,
	})
	if err != nil {
		log.Debug().Err(err).Msg("gRPC ValidateSession failed")
		return &SessionInfo{Valid: false}, err
	}

	if !resp.Valid {
		log.Debug().Str("message", resp.Message).Msg("Session invalid per gRPC")
		return &SessionInfo{Valid: false}, nil
	}

	return &SessionInfo{
		Valid:    true,
		UserType: resp.UserType,
		UserID:   resp.UserId,
		TenantID: resp.UserId, // For tenants, UserID is TenantID
		Tier:     resp.Tier,
		FullName: resp.FullName,
		Email:    resp.Email,
	}, nil
}

// GetSessionToken retrieves the session token from the request cookie.
func (m *GRPCSessionMiddleware) GetSessionToken(r *http.Request) string {
	cookie, err := r.Cookie(m.cookieName)
	if err != nil {
		return ""
	}
	return cookie.Value
}

// SetSessionCookie sets the session token as an HTTP-only cookie.
func (m *GRPCSessionMiddleware) SetSessionCookie(w http.ResponseWriter, token string, rememberMe bool) {
	maxAge := 8 * 60 * 60 // 8 hours default
	if rememberMe {
		maxAge = 30 * 24 * 60 * 60 // 30 days
	}

	cookie := &http.Cookie{
		Name:     m.cookieName,
		Value:    token,
		Path:     "/",
		MaxAge:   maxAge,
		HttpOnly: true,                    // Prevent JavaScript access
		Secure:   m.secureCookie,          // HTTPS only in production
		SameSite: http.SameSiteStrictMode, // CSRF protection
	}

	http.SetCookie(w, cookie)
}

// ClearSessionCookie clears the session cookie (logout).
func (m *GRPCSessionMiddleware) ClearSessionCookie(w http.ResponseWriter) {
	cookie := &http.Cookie{
		Name:     m.cookieName,
		Value:    "",
		Path:     "/",
		MaxAge:   -1,
		HttpOnly: true,
		Secure:   m.secureCookie,
		SameSite: http.SameSiteStrictMode,
	}

	http.SetCookie(w, cookie)
}
