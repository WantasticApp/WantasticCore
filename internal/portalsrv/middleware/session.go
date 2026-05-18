package middleware

import (
	"net/http"

	"WantasticCore/internal/portalsrv/pkg/session"

	"github.com/rs/zerolog/log"
)

// SessionMiddleware validates tenant sessions using local session store.
// This makes the tenant portal stateless at the database level, but manages
// sessions in memory (or Redis for production).
type SessionMiddleware struct {
	sessionStore *session.InMemorySessionStore
	cookieName   string
	secureCookie bool
}

// NewSessionMiddleware creates a new session middleware.
func NewSessionMiddleware(sessionStore *session.InMemorySessionStore, cookieName string) *SessionMiddleware {
	return &SessionMiddleware{
		sessionStore: sessionStore,
		cookieName:   cookieName,
		secureCookie: false, // Set to true for HTTPS
	}
}

// SetSecureCookie configures whether to use secure cookies.
func (m *SessionMiddleware) SetSecureCookie(secure bool) {
	m.secureCookie = secure
}

// RequireAuth wraps an HTTP handler and requires valid session.
// Redirects to /login if session is invalid.
func (m *SessionMiddleware) RequireAuth(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		session, err := m.ValidateRequest(r)
		if err != nil || session == nil {
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

// ValidateRequest validates the session from the request cookie.
func (m *SessionMiddleware) ValidateRequest(r *http.Request) (*session.TenantSession, error) {
	cookie, err := r.Cookie(m.cookieName)
	if err != nil {
		return nil, err
	}

	return m.sessionStore.GetSession(cookie.Value)
}

// GetSessionToken retrieves the session token from the request cookie.
func (m *SessionMiddleware) GetSessionToken(r *http.Request) string {
	cookie, err := r.Cookie(m.cookieName)
	if err != nil {
		return ""
	}
	return cookie.Value
}

// GetSession retrieves the full session from the request cookie.
func (m *SessionMiddleware) GetSession(r *http.Request) (*session.TenantSession, error) {
	token := m.GetSessionToken(r)
	if token == "" {
		return nil, http.ErrNoCookie
	}
	return m.sessionStore.GetSession(token)
}

// SetSessionCookie sets the session token as an HTTP-only cookie.
func (m *SessionMiddleware) SetSessionCookie(w http.ResponseWriter, token string, rememberMe bool) {
	maxAge := 8 * 60 * 60 // 8 hours default
	if rememberMe {
		maxAge = 30 * 24 * 60 * 60 // 30 days
	}

	// Use Lax SameSite for development (allows cross-origin from localhost:5173 to localhost:8001)
	// In production with same domain, use Strict
	sameSite := http.SameSiteLaxMode
	if m.secureCookie {
		sameSite = http.SameSiteStrictMode
	}

	cookie := &http.Cookie{
		Name:     m.cookieName,
		Value:    token,
		Path:     "/",
		MaxAge:   maxAge,
		HttpOnly: true,           // Prevent JavaScript access
		Secure:   m.secureCookie, // HTTPS only in production
		SameSite: sameSite,       // Lax for development, Strict for production
	}

	http.SetCookie(w, cookie)
}

// ClearSessionCookie clears the session cookie (logout).
func (m *SessionMiddleware) ClearSessionCookie(w http.ResponseWriter) {
	// Use same SameSite mode as SetSessionCookie
	sameSite := http.SameSiteLaxMode
	if m.secureCookie {
		sameSite = http.SameSiteStrictMode
	}

	cookie := &http.Cookie{
		Name:     m.cookieName,
		Value:    "",
		Path:     "/",
		MaxAge:   -1,
		HttpOnly: true,
		Secure:   m.secureCookie,
		SameSite: sameSite,
	}

	http.SetCookie(w, cookie)
}
