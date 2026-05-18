package middleware

import (
	"net"
	"net/http"
	"sync"
	"time"

	"golang.org/x/time/rate"
)

// RateLimiter implements per-IP rate limiting.
type RateLimiter struct {
	limiters map[string]*rate.Limiter
	mu       sync.RWMutex
	rate     rate.Limit // Requests per second
	burst    int        // Burst size
}

// NewRateLimiter creates a new rate limiter.
// rate: requests per second per IP
// burst: maximum burst size
func NewRateLimiter(r rate.Limit, b int) *RateLimiter {
	rl := &RateLimiter{
		limiters: make(map[string]*rate.Limiter),
		rate:     r,
		burst:    b,
	}

	// Start cleanup goroutine
	go rl.cleanupStale()

	return rl
}

// NewDefaultRateLimiter creates a rate limiter with sensible defaults.
// 100 requests per minute per IP, burst of 20.
func NewDefaultRateLimiter(times int) *RateLimiter {
	return NewRateLimiter(rate.Every(time.Minute), times) // 1000 req/min
}

// Middleware wraps an HTTP handler function with rate limiting.
func (rl *RateLimiter) Middleware(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		rl.Handler(http.HandlerFunc(next)).ServeHTTP(w, r)
	}
}

// Handler wraps an HTTP handler with rate limiting.
func (rl *RateLimiter) Handler(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Get client IP
		ip := getClientIP(r)

		// Get or create limiter for this IP
		limiter := rl.getLimiter(ip)

		// Check rate limit
		if !limiter.Allow() {
			http.Error(w, "Rate limit exceeded. Please try again later.", http.StatusTooManyRequests)
			return
		}

		next.ServeHTTP(w, r)
	})
}

// getLimiter gets or creates a rate limiter for an IP address.
func (rl *RateLimiter) getLimiter(ip string) *rate.Limiter {
	rl.mu.RLock()
	limiter, exists := rl.limiters[ip]
	rl.mu.RUnlock()

	if exists {
		return limiter
	}

	// Create new limiter
	rl.mu.Lock()
	defer rl.mu.Unlock()

	// Double-check after acquiring write lock
	limiter, exists = rl.limiters[ip]
	if exists {
		return limiter
	}

	limiter = rate.NewLimiter(rl.rate, rl.burst)
	rl.limiters[ip] = limiter

	return limiter
}

// cleanupStale removes stale limiters that haven't been used recently.
func (rl *RateLimiter) cleanupStale() {
	ticker := time.NewTicker(10 * time.Minute)
	defer ticker.Stop()

	for range ticker.C {
		rl.mu.Lock()

		// Remove limiters that have full tokens (not used recently)
		for ip, limiter := range rl.limiters {
			// If limiter has full burst, it hasn't been used
			if limiter.Tokens() >= float64(rl.burst) {
				delete(rl.limiters, ip)
			}
		}

		rl.mu.Unlock()
	}
}

// getClientIP extracts the real client IP from the request.
// Handles X-Forwarded-For, X-Real-IP headers and proxies.
func getClientIP(r *http.Request) string {
	// Try X-Forwarded-For header first (for proxies)
	xff := r.Header.Get("X-Forwarded-For")
	if xff != "" {
		// Take first IP in list
		ips := splitIPs(xff)
		if len(ips) > 0 {
			return ips[0]
		}
	}

	// Try X-Real-IP header
	xri := r.Header.Get("X-Real-IP")
	if xri != "" {
		return xri
	}

	// Fall back to RemoteAddr
	ip, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}

	return ip
}

// splitIPs splits a comma-separated list of IPs and returns the first valid one.
func splitIPs(s string) []string {
	var ips []string
	for _, part := range splitAndTrim(s, ",") {
		if net.ParseIP(part) != nil {
			ips = append(ips, part)
		}
	}
	return ips
}

// splitAndTrim splits a string by separator and trims whitespace.
func splitAndTrim(s, sep string) []string {
	parts := []string{}
	for _, part := range splitString(s, sep) {
		trimmed := trimSpace(part)
		if trimmed != "" {
			parts = append(parts, trimmed)
		}
	}
	return parts
}

// splitString splits a string by separator.
func splitString(s, sep string) []string {
	result := []string{}
	current := ""

	for _, char := range s {
		if string(char) == sep {
			result = append(result, current)
			current = ""
		} else {
			current += string(char)
		}
	}

	if current != "" {
		result = append(result, current)
	}

	return result
}

// trimSpace removes leading and trailing whitespace.
func trimSpace(s string) string {
	start := 0
	end := len(s)

	// Trim leading
	for start < end && isSpace(s[start]) {
		start++
	}

	// Trim trailing
	for end > start && isSpace(s[end-1]) {
		end--
	}

	return s[start:end]
}

// isSpace checks if a byte is whitespace.
func isSpace(b byte) bool {
	return b == ' ' || b == '\t' || b == '\n' || b == '\r'
}

// ResetLimiter removes the rate limiter for a specific IP.
// Useful for allowlisting or manual resets.
func (rl *RateLimiter) ResetLimiter(ip string) {
	rl.mu.Lock()
	defer rl.mu.Unlock()

	delete(rl.limiters, ip)
}

// GetLimiterStats returns statistics about the rate limiter.
func (rl *RateLimiter) GetLimiterStats() map[string]any {
	rl.mu.RLock()
	defer rl.mu.RUnlock()

	return map[string]any{
		"total_ips":       len(rl.limiters),
		"rate_per_second": float64(rl.rate),
		"burst_size":      rl.burst,
	}
}
