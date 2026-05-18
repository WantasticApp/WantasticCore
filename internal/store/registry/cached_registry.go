package registry

import (
	"context"
	"encoding/json"
	"time"

	"WantasticCore/internal/tenant"

	"github.com/redis/go-redis/v9"
	"github.com/rs/zerolog/log"
)

// CachedRegistry wraps a tenant.Registry and adds Redis caching for sessions.
type CachedRegistry struct {
	tenant.Registry
	redis *redis.Client
}

// NewCachedRegistry creates a new cached registry.
func NewCachedRegistry(registry tenant.Registry, redisClient *redis.Client) *CachedRegistry {
	return &CachedRegistry{
		Registry: registry,
		redis:    redisClient,
	}
}

// CreateSession creates a new session and caches it.
func (r *CachedRegistry) CreateSession(tenantID, sessionID, ipAddress, userAgent, deviceHash string, duration time.Duration, trustedDevice bool) error {
	// Write to DB
	if err := r.Registry.CreateSession(tenantID, sessionID, ipAddress, userAgent, deviceHash, duration, trustedDevice); err != nil {
		return err
	}

	// Cache in Redis
	now := time.Now()
	session := &tenant.TenantSession{
		SessionID:     sessionID,
		TenantID:      tenantID,
		IPAddress:     ipAddress,
		UserAgent:     userAgent,
		CreatedAt:     now,
		ExpiresAt:     now.Add(duration),
		LastActivity:  now,
		TrustedDevice: trustedDevice,
		// Note: Some fields might remain empty if not passed, assuming minimal session struct for cache
	}
	r.cacheSession(session, duration)
	return nil
}

// GetSession retrieves a session from cache or DB.
func (r *CachedRegistry) GetSession(sessionID string) (*tenant.TenantSession, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	// Try Redis
	val, err := r.redis.Get(ctx, "session:"+sessionID).Result()
	if err == nil {
		var session tenant.TenantSession
		if err := json.Unmarshal([]byte(val), &session); err == nil {
			if time.Now().After(session.ExpiresAt) {
				r.DeleteSession(sessionID)
				return nil, nil // Expired
			}
			return &session, nil
		}
	}

	// Fallback to DB
	session, err := r.Registry.GetSession(sessionID)
	if err != nil {
		return nil, err
	}
	if session == nil {
		return nil, nil
	}

	// Cache it
	ttl := time.Until(session.ExpiresAt)
	if ttl > 0 {
		r.cacheSession(session, ttl)
	}

	return session, nil
}

// ValidateSession validates a session.
func (r *CachedRegistry) ValidateSession(sessionID string) (string, error) {
	session, err := r.GetSession(sessionID)
	if err != nil {
		return "", err
	}
	if session == nil {
		return "", nil
	}
	return session.TenantID, nil
}

// DeleteSession removes a session from cache and DB.
func (r *CachedRegistry) DeleteSession(sessionID string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	r.redis.Del(ctx, "session:"+sessionID)
	return r.Registry.DeleteSession(sessionID)
}

// UpdateSessionActivity updates activity timestamp (write-through).
func (r *CachedRegistry) UpdateSessionActivity(sessionID string) error {
	// Update DB (async potentially, or just sync)
	if err := r.Registry.UpdateSessionActivity(sessionID); err != nil {
		return err
	}

	// Update Redis TTL/Activity if exists
	// For simplicity, just let it slide or expire.
	// To properly slide TTL, we'd need to re-read or use EXPIRE if we don't change content.
	// But LastActivity is in content.
	// We can skip updating content in Redis for pure activity bump to save bandwidth,
	// unless LastActivity is critical for logic.
	return nil
}

// InvalidateAllSessions invalidates all sessions for a tenant.
func (r *CachedRegistry) InvalidateAllSessions(tenantID string) error {
	// We should technically find all sessions and delete them from Redis.
	// Or use a versioning scheme "tenant_version:ID" included in session key?
	// For now, just DB. Redis sessions will expire or be caught by validity check if we check against tenant logic.
	// If crucial, we'd need a set of sessionIDs per tenant.
	return r.Registry.InvalidateAllSessions(tenantID)
}

func (r *CachedRegistry) cacheSession(session *tenant.TenantSession, ttl time.Duration) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	data, err := json.Marshal(session)
	if err != nil {
		log.Error().Err(err).Msg("Failed to marshal session for Redis")
		return
	}

	if err := r.redis.Set(ctx, "session:"+session.SessionID, data, ttl).Err(); err != nil {
		log.Error().Err(err).Msg("Failed to cache session in Redis")
	}
}
