package session

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"
	"github.com/rs/zerolog/log"
)

const apiKeyPrefix = "tenant_apikey:"
const tenantAPIKeyIndexPrefix = "tenant_apikey_index:"
const sessionPrefix = "tenant_session:"
const tenantIndexPrefix = "tenant_sessions:"

// CreateAPIKey creates a new persistent API key for a tenant, replacing any existing keys
func (s *RedisSessionStore) CreateAPIKey(tenantID, name, grpcSessionToken string, expiresAt time.Time) (*APIKey, error) {
	// Generate secure random token
	tokenBytes := make([]byte, 32)
	if _, err := rand.Read(tokenBytes); err != nil {
		return nil, fmt.Errorf("failed to generate api key token: %w", err)
	}
	token := base64.URLEncoding.EncodeToString(tokenBytes)

	// Generate ID
	idBytes := make([]byte, 16)
	rand.Read(idBytes)
	id := fmt.Sprintf("ak_%x", idBytes)

	apiKey := &APIKey{
		ID:               id,
		TenantID:         tenantID,
		Name:             name,
		Token:            token,
		GRPCSessionToken: grpcSessionToken,
		CreatedAt:        time.Now(),
		LastUsedAt:       time.Now(),
		ExpiresAt:        expiresAt,
	}

	data, err := json.Marshal(apiKey)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal api key: %w", err)
	}

	ctx := context.Background()

	// Get existing keys to check if any exist (enforce single key policy)
	existingTokens, err := s.redis.SMembers(ctx, tenantAPIKeyIndexPrefix+tenantID).Result()
	if err != nil {
		return nil, fmt.Errorf("failed to check existing keys: %w", err)
	}

	if len(existingTokens) > 0 {
		return nil, fmt.Errorf("api key already exists. please delete the existing one first.")
	}

	pipe := s.redis.Pipeline()

	// Calculate TTL
	ttl := time.Until(expiresAt)
	if ttl < 0 {
		return nil, fmt.Errorf("expiration time must be in the future")
	}

	// 2. Store new key
	pipe.Set(ctx, apiKeyPrefix+token, data, ttl)
	// Index by tenant
	pipe.SAdd(ctx, tenantAPIKeyIndexPrefix+tenantID, token)

	if _, err := pipe.Exec(ctx); err != nil {
		return nil, fmt.Errorf("failed to store api key in Redis: %w", err)
	}

	return apiKey, nil
}

// GetAPIKey retrieves an API key by token
func (s *RedisSessionStore) GetAPIKey(token string) (*APIKey, error) {
	ctx := context.Background()
	data, err := s.redis.Get(ctx, apiKeyPrefix+token).Bytes()
	if err != nil {
		if err == redis.Nil {
			return nil, fmt.Errorf("api key not found")
		}
		return nil, fmt.Errorf("redis error getting api key: %v", err)
	}

	var apiKey APIKey
	if err := json.Unmarshal(data, &apiKey); err != nil {
		return nil, fmt.Errorf("failed to unmarshal api key: %w", err)
	}

	// Update LastUsedAt (async to avoid blocking)
	go func() {
		apiKey.LastUsedAt = time.Now()
		updatedData, _ := json.Marshal(apiKey)
		// Keep the same TTL
		ttl := s.redis.TTL(ctx, apiKeyPrefix+token).Val()
		s.redis.Set(ctx, apiKeyPrefix+token, updatedData, ttl)
	}()

	return &apiKey, nil
}

// ListAPIKeys lists all active API keys for a tenant
func (s *RedisSessionStore) ListAPIKeys(tenantID string) ([]*APIKey, error) {
	ctx := context.Background()
	key := tenantAPIKeyIndexPrefix + tenantID

	tokens, err := s.redis.SMembers(ctx, key).Result()
	if err != nil {
		return nil, fmt.Errorf("failed to list api keys: %w", err)
	}

	if len(tokens) == 0 {
		return []*APIKey{}, nil
	}

	// MGet all keys
	var redisKeys []string
	for _, t := range tokens {
		redisKeys = append(redisKeys, apiKeyPrefix+t)
	}

	results, err := s.redis.MGet(ctx, redisKeys...).Result()
	if err != nil {
		return nil, fmt.Errorf("failed to mget api keys: %w", err)
	}

	var apiKeys []*APIKey
	pipe := s.redis.Pipeline()
	needsCleanup := false

	for i, res := range results {
		if res == nil {
			// Key expired but still in index, clean up
			pipe.SRem(ctx, key, tokens[i])
			needsCleanup = true
			continue
		}

		strData, ok := res.(string) // Redis MGET returns []interface{}, strings are headers? No, values.
		// go-redis MGET returns []interface{} where each is likely string or nil
		if !ok {
			continue
		}

		var apiKey APIKey
		if err := json.Unmarshal([]byte(strData), &apiKey); err == nil {
			apiKeys = append(apiKeys, &apiKey)
		}
	}

	if needsCleanup {
		pipe.Exec(ctx)
	}

	return apiKeys, nil
}

// RevokeAPIKey revokes an API key by ID
func (s *RedisSessionStore) RevokeAPIKey(tenantID, keyID string) error {
	// First list keys to find the one with matching ID
	keys, err := s.ListAPIKeys(tenantID)
	if err != nil {
		return err
	}

	var targetToken string
	for _, k := range keys {
		if k.ID == keyID {
			targetToken = k.Token
			break
		}
	}

	if targetToken == "" {
		return fmt.Errorf("api key not found")
	}

	ctx := context.Background()
	pipe := s.redis.Pipeline()
	pipe.Del(ctx, apiKeyPrefix+targetToken)
	pipe.SRem(ctx, tenantAPIKeyIndexPrefix+tenantID, targetToken)

	_, err = pipe.Exec(ctx)
	return err
}

// RedisSessionStore stores sessions in Redis so they survive process restarts
type RedisSessionStore struct {
	redis *redis.Client
}

// NewRedisSessionStore creates a new Redis session store
func NewRedisSessionStore(client *redis.Client) *RedisSessionStore {
	return &RedisSessionStore{
		redis: client,
	}
}

// CreateSession creates a new session for a tenant and stores it in Redis
func (s *RedisSessionStore) CreateSession(tenantID, fullName, email, tier, grpcSessionToken string, rememberMe bool) (*TenantSession, error) {
	// Generate secure random token
	tokenBytes := make([]byte, 32)
	if _, err := rand.Read(tokenBytes); err != nil {
		return nil, fmt.Errorf("failed to generate session token: %w", err)
	}
	token := base64.URLEncoding.EncodeToString(tokenBytes)

	// Determine expiration based on remember me
	duration := 8 * time.Hour // Default: 8 hours
	if rememberMe {
		duration = 30 * 24 * time.Hour // Remember me: 30 days
	}
	expiresAt := time.Now().Add(duration)

	session := &TenantSession{
		Token:            token,
		GRPCSessionToken: grpcSessionToken,
		TenantID:         tenantID,
		FullName:         fullName,
		Email:            email,
		Tier:             tier,
		CreatedAt:        time.Now(),
		ExpiresAt:        expiresAt,
		RememberMe:       rememberMe,
	}

	// Serialize and store in Redis
	data, err := json.Marshal(session)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal session: %w", err)
	}

	ctx := context.Background()
	pipe := s.redis.Pipeline()

	// Store the session data
	pipe.Set(ctx, sessionPrefix+token, data, duration)

	// Index by tenant ID for mass invalidation (e.g. password change)
	pipe.SAdd(ctx, tenantIndexPrefix+tenantID, token)
	pipe.Expire(ctx, tenantIndexPrefix+tenantID, duration) // Extend index TTL

	if _, err := pipe.Exec(ctx); err != nil {
		return nil, fmt.Errorf("failed to store session in Redis: %w", err)
	}

	return session, nil
}

// ValidateSession validates a session token and returns session data
func (s *RedisSessionStore) ValidateSession(token string) (any, error) {
	return s.GetSession(token)
}

// GetSession validates and returns the typed session object from Redis
func (s *RedisSessionStore) GetSession(token string) (*TenantSession, error) {
	ctx := context.Background()
	data, err := s.redis.Get(ctx, sessionPrefix+token).Bytes()
	if err != nil {
		if err == redis.Nil {
			return nil, fmt.Errorf("session not found")
		}
		return nil, fmt.Errorf("redis error getting session: %v", err)
	}

	var session TenantSession
	if err := json.Unmarshal(data, &session); err != nil {
		return nil, fmt.Errorf("failed to unmarshal session: %w", err)
	}

	// Check if session is expired (Redis should handle this via TTL, but we verify anyway)
	if time.Now().After(session.ExpiresAt) {
		_ = s.DeleteSession(token)
		return nil, fmt.Errorf("session expired")
	}

	return &session, nil
}

// DeleteSession removes a session (logout)
func (s *RedisSessionStore) DeleteSession(token string) error {
	ctx := context.Background()

	// Get session first to find tenant ID for index cleanup
	data, err := s.redis.Get(ctx, sessionPrefix+token).Bytes()
	if err == nil {
		var session TenantSession
		if err := json.Unmarshal(data, &session); err == nil {
			// Remove from tenant index
			s.redis.SRem(ctx, tenantIndexPrefix+session.TenantID, token)
		}
	}

	return s.redis.Del(ctx, sessionPrefix+token).Err()
}

// DeleteAllSessionsForTenant removes all sessions for a specific tenant
func (s *RedisSessionStore) DeleteAllSessionsForTenant(tenantID string) int {
	ctx := context.Background()
	key := tenantIndexPrefix + tenantID

	// Get all active session tokens for this tenant
	tokens, err := s.redis.SMembers(ctx, key).Result()
	if err != nil {
		log.Error().Err(err).Str("tenant_id", tenantID).Msg("Failed to list tenant sessions for deletion")
		return 0
	}

	if len(tokens) == 0 {
		return 0
	}

	// Delete each session and the index
	count := 0
	pipe := s.redis.Pipeline()
	for _, token := range tokens {
		pipe.Del(ctx, sessionPrefix+token)
		count++
	}
	pipe.Del(ctx, key)

	if _, err := pipe.Exec(ctx); err != nil {
		log.Error().Err(err).Str("tenant_id", tenantID).Msg("Failed to execute Redis delete pipeline")
	}

	return count
}

// GetSessionCount returns the total number of active tenant portal sessions
// Warning: SCARD overhead if used frequently, but fine for debug
func (s *RedisSessionStore) GetSessionCount() int {
	ctx := context.Background()
	iter := s.redis.Scan(ctx, 0, sessionPrefix+"*", 0).Iterator()
	count := 0
	for iter.Next(ctx) {
		count++
	}
	return count
}
