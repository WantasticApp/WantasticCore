package cache

import (
	"context"
	"encoding/json"
	"time"

	"WantasticCore/internal/store"

	"github.com/redis/go-redis/v9"
	"github.com/rs/zerolog/log"
)

const sessionPrefix = "session:"

// cachedSessionRepository wraps a SessionRepository with Redis caching.
type cachedSessionRepository struct {
	store.SessionRepository
	redis *redis.Client
}

// NewCachedSessionRepository creates a new Redis-cached session repository.
func NewCachedSessionRepository(repo store.SessionRepository, redis *redis.Client) store.SessionRepository {
	return &cachedSessionRepository{
		SessionRepository: repo,
		redis:             redis,
	}
}

func (r *cachedSessionRepository) Create(session *store.SessionData) error {
	// Write to database first
	if err := r.SessionRepository.Create(session); err != nil {
		return err
	}
	// Cache it
	r.cache(session)
	return nil
}

func (r *cachedSessionRepository) Get(sessionID string) (*store.SessionData, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	// Try cache first
	data, err := r.redis.Get(ctx, sessionPrefix+sessionID).Bytes()
	if err == nil {
		var session store.SessionData
		if json.Unmarshal(data, &session) == nil {
			// Check expiry
			if time.Now().After(session.ExpiresAt) {
				r.Delete(sessionID)
				return nil, nil
			}
			return &session, nil
		}
	}

	// Fallback to database
	session, err := r.SessionRepository.Get(sessionID)
	if err != nil {
		return nil, err
	}

	// Cache for future
	if session != nil {
		r.cache(session)
	}

	return session, nil
}

func (r *cachedSessionRepository) Validate(sessionID string) (string, error) {
	session, err := r.Get(sessionID)
	if err != nil {
		return "", err
	}
	if session == nil {
		return "", nil
	}
	return session.TenantID, nil
}

func (r *cachedSessionRepository) Delete(sessionID string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	// Delete from cache
	r.redis.Del(ctx, sessionPrefix+sessionID)

	// Delete from database
	return r.SessionRepository.Delete(sessionID)
}

func (r *cachedSessionRepository) DeleteByTenant(tenantID string) error {
	// Get all sessions for tenant first
	sessions, err := r.SessionRepository.ListByTenant(tenantID)
	if err != nil {
		return err
	}

	// Delete from cache
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	for _, s := range sessions {
		r.redis.Del(ctx, sessionPrefix+s.SessionID)
	}

	// Delete from database
	return r.SessionRepository.DeleteByTenant(tenantID)
}

func (r *cachedSessionRepository) UpdateActivity(sessionID string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	// Update database
	if err := r.SessionRepository.UpdateActivity(sessionID); err != nil {
		return err
	}

	// Update cache TTL if exists (don't need to re-fetch full session)
	// Just reset the TTL to extend it
	key := sessionPrefix + sessionID
	ttl, err := r.redis.TTL(ctx, key).Result()
	if err == nil && ttl > 0 {
		// Extend cache TTL by the same amount
		r.redis.Expire(ctx, key, ttl)
	}

	return nil
}

func (r *cachedSessionRepository) cache(session *store.SessionData) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	data, err := json.Marshal(session)
	if err != nil {
		log.Error().Err(err).Msg("Failed to marshal session for cache")
		return
	}

	ttl := time.Until(session.ExpiresAt)
	if ttl <= 0 {
		return
	}

	if err := r.redis.Set(ctx, sessionPrefix+session.SessionID, data, ttl).Err(); err != nil {
		log.Error().Err(err).Msg("Failed to cache session")
	}
}
