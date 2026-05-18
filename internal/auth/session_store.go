package auth

import (
	"context"
	"encoding/json"
	"time"

	"WantasticCore/internal/store/postgres"
	"WantasticCore/internal/tenant"

	"github.com/redis/go-redis/v9"
	"github.com/rs/zerolog/log"
)

const (
	TenantSessionDuration = 24 * time.Hour
	TenantSessionMaxAge   = 30 * 24 * time.Hour
)

// TenantPgSessionStore manages tenant sessions in PostgreSQL with optional Redis caching.
type TenantPgSessionStore struct {
	store *postgres.TenantStore
	redis *redis.Client
}

// NewTenantPgSessionStore creates a new PostgreSQL-backed tenant session store.
func NewTenantPgSessionStore(store *postgres.TenantStore, redisClient *redis.Client) *TenantPgSessionStore {
	return &TenantPgSessionStore{
		store: store,
		redis: redisClient,
	}
}

// CreateSession creates a new session for a tenant.
func (s *TenantPgSessionStore) CreateSession(tenantID, email, fullName, tier, sessionToken, ipAddress, userAgent string, rememberMe bool) (*tenant.TenantSession, error) {
	duration := TenantSessionDuration
	if rememberMe {
		duration = TenantSessionMaxAge
	}

	// Create a new session using the PostgreSQL store
	// deviceHash is empty for now as it's not passed in
	err := s.store.CreateSession(tenantID, sessionToken, ipAddress, userAgent, "", duration, rememberMe)
	if err != nil {
		return nil, err
	}

	now := time.Now()
	session := &tenant.TenantSession{
		SessionID:     sessionToken,
		TenantID:      tenantID,
		Email:         email,
		FullName:      fullName,
		SessionToken:  sessionToken,
		CreatedAt:     now,
		ExpiresAt:     now.Add(duration),
		LastActivity:  now,
		IPAddress:     ipAddress,
		UserAgent:     userAgent,
		RememberMe:    rememberMe,
		TrustedDevice: rememberMe,
	}

	// Cache in Redis if enabled
	if s.redis != nil {
		s.cacheSession(session, duration)
	}

	return session, nil
}

// GetSession retrieves and validates a session.
func (s *TenantPgSessionStore) GetSession(sessionID string) (*tenant.TenantSession, error) {
	// Try Redis first
	if s.redis != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()

		val, err := s.redis.Get(ctx, "session:"+sessionID).Result()
		if err == nil {
			var session tenant.TenantSession
			if err := json.Unmarshal([]byte(val), &session); err == nil {
				// Check expiry
				if time.Now().After(session.ExpiresAt) {
					s.DeleteSession(sessionID)
					return nil, nil // Expired
				}
				// Slide expiry in Redis (optional, maybe too expensive to do on every read)
				return &session, nil
			}
		}
	}

	// Fallback to DB
	session, err := s.store.GetSession(sessionID)
	if err != nil {
		return nil, err
	}

	// Cache back to Redis
	if s.redis != nil && session != nil {
		ttl := time.Until(session.ExpiresAt)
		if ttl > 0 {
			s.cacheSession(session, ttl)
		}
	}

	return session, err
}

// DeleteSession removes a session.
func (s *TenantPgSessionStore) DeleteSession(sessionID string) error {
	if s.redis != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		s.redis.Del(ctx, "session:"+sessionID)
	}
	return s.store.DeleteSession(sessionID)
}

// DeleteAllUserSessions removes all sessions for a tenant.
func (s *TenantPgSessionStore) DeleteAllUserSessions(tenantID string) error {
	// We can't easily delete by pattern efficiently in Redis without SCAN
	// For now, relies on TTL or single deletion
	// Optionally could store a set of sessions per user
	return s.store.DeleteAllUserSessions(tenantID)
}

// GetUserActiveSessions returns all active sessions for a tenant.
func (s *TenantPgSessionStore) GetUserActiveSessions(tenantID string) ([]*tenant.TenantSession, error) {
	return s.store.GetUserActiveSessions(tenantID)
}

// ValidateSession implements the middleware.SessionStore interface.
func (s *TenantPgSessionStore) ValidateSession(sessionID string) (any, error) {
	return s.GetSession(sessionID)
}

func (s *TenantPgSessionStore) cacheSession(session *tenant.TenantSession, ttl time.Duration) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	data, err := json.Marshal(session)
	if err != nil {
		log.Error().Err(err).Msg("Failed to marshal session for Redis")
		return
	}

	if err := s.redis.Set(ctx, "session:"+session.SessionID, data, ttl).Err(); err != nil {
		log.Error().Err(err).Msg("Failed to cache session in Redis")
	}
}
