package middleware

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
)

// SecurityMiddleware handles XSS protection, CSRF validation, and request size limits.
type SecurityMiddleware struct {
	csrfTokens  map[string]time.Time // token -> expiry
	mu          sync.RWMutex
	maxBodySize int64 // Maximum request body size
}

// NewSecurityMiddleware creates a new security middleware.
func NewSecurityMiddleware() *SecurityMiddleware {
	sm := &SecurityMiddleware{
		csrfTokens:  make(map[string]time.Time),
		maxBodySize: 1 << 20, // 1MB default
	}

	// Start cleanup goroutine for expired CSRF tokens
	go sm.cleanupExpiredTokens()

	return sm
}

// Middleware wraps an HTTP handler function with security checks.
func (sm *SecurityMiddleware) Middleware(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		sm.Handler(http.HandlerFunc(next)).ServeHTTP(w, r)
	}
}

// Handler wraps an HTTP handler with security checks.
func (sm *SecurityMiddleware) Handler(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// 1. Set security headers (XSS, clickjacking, content-type sniffing protection)
		sm.setSecurityHeaders(w)

		// 2. Request size limit
		r.Body = http.MaxBytesReader(w, r.Body, sm.maxBodySize)

		// 3. CSRF validation for state-changing methods
		if r.Method != "GET" && r.Method != "HEAD" && r.Method != "OPTIONS" {
			// Skip CSRF for WebSocket upgrade
			if r.Header.Get("Upgrade") != "websocket" {
				if !sm.validateCSRF(r) {
					http.Error(w, "Invalid or missing CSRF token", http.StatusForbidden)
					return
				}
			}
		}

		// 4. Input sanitization headers
		w.Header().Set("X-Content-Type-Options", "nosniff")

		next.ServeHTTP(w, r)
	})
}

// setSecurityHeaders sets HTTP security headers.
func (sm *SecurityMiddleware) setSecurityHeaders(w http.ResponseWriter) {
	// XSS Protection
	w.Header().Set("X-XSS-Protection", "1; mode=block")

	// Clickjacking protection
	w.Header().Set("X-Frame-Options", "DENY")

	// Content-Type sniffing protection
	w.Header().Set("X-Content-Type-Options", "nosniff")

	// Content Security Policy (strict for admin panel)
	//
	// NOTE: htmx (and htmx-ext-ws) uses Function()/eval internally for some features
	// (notably `hx-vals='js:...'`). Without 'unsafe-eval', WS form submits can appear
	// to do "nothing" because the client-side send is blocked.
	csp := strings.Join([]string{
		"default-src 'self'",
		"script-src 'self' 'unsafe-inline' 'unsafe-eval' https://unpkg.com https://cdn.jsdelivr.net https://cdnjs.cloudflare.com https://chart.googleapis.com static.cloudflareinsights.com",
		"style-src 'self' 'unsafe-inline' https://unpkg.com https://cdn.jsdelivr.net https://cdnjs.cloudflare.com",
		"img-src 'self' data: https:",
		"font-src 'self' https://fonts.gstatic.com https://unpkg.com",
		"connect-src 'self' ws: wss:",
		"frame-src 'self'",
		"frame-ancestors 'none'",
		"base-uri 'self'",
		"form-action 'self'",
	}, "; ")
	w.Header().Set("Content-Security-Policy", csp)

	// Referrer Policy
	w.Header().Set("Referrer-Policy", "strict-origin-when-cross-origin")

	// Permissions Policy (restrict features)
	w.Header().Set("Permissions-Policy", "geolocation=(), microphone=(), camera=()")
}

// GenerateCSRFToken generates a new CSRF token and stores it.
func (sm *SecurityMiddleware) GenerateCSRFToken() string {
	token := generateRandomToken(32)

	sm.mu.Lock()
	defer sm.mu.Unlock()

	// Token expires in 1 hour
	sm.csrfTokens[token] = time.Now().Add(1 * time.Hour)

	return token
}

// validateCSRF validates a CSRF token from the request.
func (sm *SecurityMiddleware) validateCSRF(r *http.Request) bool {
	// Skip CSRF for API endpoints using JSON (they should use other auth methods)
	// These endpoints typically use fetch() with JSON body, not form submissions
	contentType := r.Header.Get("Content-Type")
	if strings.Contains(contentType, "application/json") {
		// For JSON APIs, validate Origin/Referer instead
		origin := r.Header.Get("Origin")
		referer := r.Header.Get("Referer")
		host := strings.ToLower(r.Host)

		// Allow same-origin requests only (exact host match, no Origin:null).
		if origin != "" && isSameHostURL(origin, host) {
			return true
		}
		if referer != "" && isSameHostURL(referer, host) {
			return true
		}
		// If no origin/referer but JSON content type from localhost, allow (dev mode)
		if strings.HasPrefix(host, "localhost") || strings.HasPrefix(host, "127.0.0.1") {
			return true
		}
	}

	// Try to get token from header first
	token := r.Header.Get("X-CSRF-Token")

	// If not in header, try form value
	if token == "" {
		token = r.FormValue("csrf_token")
	}

	// If not in form, try cookie
	if token == "" {
		cookie, err := r.Cookie("csrf_token")
		if err == nil {
			token = cookie.Value
		}
	}

	if token == "" {
		return false
	}

	sm.mu.RLock()
	expiry, exists := sm.csrfTokens[token]
	sm.mu.RUnlock()

	if !exists {
		return false
	}

	// Check if token expired
	if time.Now().After(expiry) {
		sm.mu.Lock()
		delete(sm.csrfTokens, token)
		sm.mu.Unlock()
		return false
	}

	return true
}

func isSameHostURL(raw, expectedHost string) bool {
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" {
		return false
	}
	h := strings.ToLower(u.Host)
	return h == strings.ToLower(expectedHost)
}

// cleanupExpiredTokens removes expired CSRF tokens periodically.
func (sm *SecurityMiddleware) cleanupExpiredTokens() {
	ticker := time.NewTicker(5 * time.Minute)
	defer ticker.Stop()

	for range ticker.C {
		now := time.Now()

		sm.mu.Lock()
		for token, expiry := range sm.csrfTokens {
			if now.After(expiry) {
				delete(sm.csrfTokens, token)
			}
		}
		sm.mu.Unlock()
	}
}

// SetMaxBodySize sets the maximum request body size.
func (sm *SecurityMiddleware) SetMaxBodySize(size int64) {
	sm.maxBodySize = size
}

// generateRandomToken generates a random hex token.
func generateRandomToken(length int) string {
	bytes := make([]byte, length)
	if _, err := rand.Read(bytes); err != nil {
		return ""
	}
	return hex.EncodeToString(bytes)
}

// SanitizeInput sanitizes user input to prevent XSS.
// This is a basic implementation - for production, use a library like bluemonday.
func SanitizeInput(input string) string {
	// Remove script tags
	input = strings.ReplaceAll(input, "<script", "&lt;script")
	input = strings.ReplaceAll(input, "</script>", "&lt;/script&gt;")

	// Remove event handlers
	dangerousPatterns := []string{
		"onclick=", "onerror=", "onload=", "onmouseover=",
		"javascript:", "vbscript:", "data:text/html",
	}

	for _, pattern := range dangerousPatterns {
		input = strings.ReplaceAll(strings.ToLower(input), pattern, "")
	}

	return input
}

// ValidateContentType ensures the request has a valid content type.
func ValidateContentType(allowedTypes []string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			contentType := r.Header.Get("Content-Type")

			// Allow empty content type for GET/DELETE
			if contentType == "" && (r.Method == "GET" || r.Method == "DELETE") {
				next.ServeHTTP(w, r)
				return
			}

			// Check if content type is allowed
			for _, allowed := range allowedTypes {
				if strings.HasPrefix(contentType, allowed) {
					next.ServeHTTP(w, r)
					return
				}
			}

			http.Error(w, fmt.Sprintf("Invalid content type: %s", contentType), http.StatusUnsupportedMediaType)
		})
	}
}
