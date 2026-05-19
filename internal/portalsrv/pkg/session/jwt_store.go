package session

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/redis/go-redis/v9"
	"github.com/rs/zerolog/log"
)

// JWTSessionStore stores all session data inside a signed cookie (HMAC-SHA256 JWT).
// Sessions survive portal restarts and Redis outages.
// Redis is used only for revocation (logout / tenant lockout); when Redis is
// unavailable the revocation check is skipped so users stay logged in.
type JWTSessionStore struct {
	secret []byte
	redis  *redis.Client // nil when Redis is not configured

	// Local revocation cache avoids hitting Redis on every request.
	revokedMu sync.RWMutex
	revoked   map[string]time.Time // jti -> expiry
}

const jwtRevocationPrefix = "portal_revoked_jti:"
const jwtTenantRevocationPrefix = "portal_revoked_tenant:"

// NewJWTSessionStore creates a store backed by a signed JWT cookie.
// redisClient may be nil; in that case revocation is best-effort only
// (revoked sessions will be re-accepted after a portal restart, but will
// still expire at their natural expiry time).
func NewJWTSessionStore(secret []byte, redisClient *redis.Client) *JWTSessionStore {
	s := &JWTSessionStore{
		secret:  secret,
		redis:   redisClient,
		revoked: make(map[string]time.Time),
	}
	go s.cleanupRevocationCache()
	return s
}

// jwtClaims is the payload embedded in the cookie token.
type jwtClaims struct {
	JTI              string `json:"jti"`
	TenantID         string `json:"tid"`
	FullName         string `json:"name"`
	Email            string `json:"email"`
	Tier             string `json:"tier"`
	GRPCSessionToken string `json:"gst"`
	RememberMe       bool   `json:"rem"`
	CreatedAt        int64  `json:"iat"`
	ExpiresAt        int64  `json:"exp"`
}

// CreateSession mints a new JWT cookie token embedding all session fields.
func (s *JWTSessionStore) CreateSession(tenantID, fullName, email, tier, grpcSessionToken string, rememberMe bool) (*TenantSession, error) {
	// Random JTI for revocation
	jtiBytes := make([]byte, 16)
	if _, err := rand.Read(jtiBytes); err != nil {
		return nil, fmt.Errorf("failed to generate jti: %w", err)
	}
	jti := base64.RawURLEncoding.EncodeToString(jtiBytes)

	now := time.Now()
	var duration time.Duration
	if rememberMe {
		duration = 30 * 24 * time.Hour
	} else {
		duration = 8 * time.Hour
	}
	expiresAt := now.Add(duration)

	claims := jwtClaims{
		JTI:              jti,
		TenantID:         tenantID,
		FullName:         fullName,
		Email:            email,
		Tier:             tier,
		GRPCSessionToken: grpcSessionToken,
		RememberMe:       rememberMe,
		CreatedAt:        now.Unix(),
		ExpiresAt:        expiresAt.Unix(),
	}

	token, err := s.sign(claims)
	if err != nil {
		return nil, fmt.Errorf("failed to sign session token: %w", err)
	}

	// Opportunistically register in Redis for cross-instance revocation.
	// Non-fatal if Redis is unavailable.
	if s.redis != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		// Store a minimal marker under the tenant index so we can revoke by tenant.
		s.redis.SAdd(ctx, jwtTenantRevocationPrefix+"idx:"+tenantID, jti)
		s.redis.Expire(ctx, jwtTenantRevocationPrefix+"idx:"+tenantID, duration)
	}

	return &TenantSession{
		Token:            token,
		GRPCSessionToken: grpcSessionToken,
		TenantID:         tenantID,
		FullName:         fullName,
		Email:            email,
		Tier:             tier,
		CreatedAt:        now,
		ExpiresAt:        expiresAt,
		RememberMe:       rememberMe,
	}, nil
}

// GetSession validates the JWT cookie token and returns the session.
// Validation never fails due to a Redis outage — it falls back to JWT-only
// verification.  The only fatal errors are an invalid signature, expired
// token, or explicit revocation.
func (s *JWTSessionStore) GetSession(token string) (*TenantSession, error) {
	claims, err := s.verify(token)
	if err != nil {
		return nil, err
	}

	// Check revocation — first the local in-memory cache (no I/O), then Redis.
	if s.isRevoked(claims.JTI) {
		return nil, fmt.Errorf("session revoked")
	}

	if s.redis != nil {
		if s.checkRedisRevocation(claims.JTI, claims.TenantID, claims.CreatedAt) {
			return nil, fmt.Errorf("session revoked")
		}
	}

	expiresAt := time.Unix(claims.ExpiresAt, 0)

	return &TenantSession{
		Token:            token,
		GRPCSessionToken: claims.GRPCSessionToken,
		TenantID:         claims.TenantID,
		FullName:         claims.FullName,
		Email:            claims.Email,
		Tier:             claims.Tier,
		CreatedAt:        time.Unix(claims.CreatedAt, 0),
		ExpiresAt:        expiresAt,
		RememberMe:       claims.RememberMe,
	}, nil
}

// ValidateSession implements SessionStore.
func (s *JWTSessionStore) ValidateSession(token string) (any, error) {
	return s.GetSession(token)
}

// DeleteSession revokes the JWT by registering its JTI.
func (s *JWTSessionStore) DeleteSession(token string) error {
	claims, err := s.verify(token)
	if err != nil {
		// Token is already invalid; nothing to revoke.
		return nil
	}

	remaining := time.Until(time.Unix(claims.ExpiresAt, 0))
	if remaining <= 0 {
		return nil // already expired
	}

	// Local cache
	s.revokedMu.Lock()
	s.revoked[claims.JTI] = time.Unix(claims.ExpiresAt, 0)
	s.revokedMu.Unlock()

	// Redis (best-effort)
	if s.redis != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		s.redis.Set(ctx, jwtRevocationPrefix+claims.JTI, "1", remaining)
	}

	return nil
}

// DeleteAllSessionsForTenant revokes all outstanding sessions for a tenant by
// setting a tenant-level revocation timestamp in Redis.  Local sessions not
// yet in Redis will expire naturally.
func (s *JWTSessionStore) DeleteAllSessionsForTenant(tenantID string) int {
	if s.redis == nil {
		return 0
	}

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	// Store a "revoke_all_before" timestamp so existing JWTs issued before now
	// are rejected.
	key := jwtTenantRevocationPrefix + tenantID
	s.redis.Set(ctx, key, time.Now().Unix(), 30*24*time.Hour)

	// Also try to get individual JTI list and revoke them.
	idxKey := jwtTenantRevocationPrefix + "idx:" + tenantID
	jtis, err := s.redis.SMembers(ctx, idxKey).Result()
	if err != nil {
		return 0
	}

	pipe := s.redis.Pipeline()
	for _, jti := range jtis {
		pipe.Set(ctx, jwtRevocationPrefix+jti, "1", 30*24*time.Hour)
	}
	pipe.Del(ctx, idxKey)
	pipe.Exec(ctx)

	return len(jtis)
}

// GetSessionCount returns an approximate count (Redis-based, 0 when unavailable).
func (s *JWTSessionStore) GetSessionCount() int {
	if s.redis == nil {
		return 0
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	n, _ := s.redis.DBSize(ctx).Result()
	return int(n)
}

// ── API Keys (delegated to Redis) ──────────────────────────────────────────

func (s *JWTSessionStore) CreateAPIKey(tenantID, name, grpcSessionToken string, expiresAt time.Time) (*APIKey, error) {
	if s.redis == nil {
		return nil, fmt.Errorf("api keys require Redis")
	}
	rs := &RedisSessionStore{redis: s.redis}
	return rs.CreateAPIKey(tenantID, name, grpcSessionToken, expiresAt)
}

func (s *JWTSessionStore) GetAPIKey(token string) (*APIKey, error) {
	if s.redis == nil {
		return nil, fmt.Errorf("api keys require Redis")
	}
	rs := &RedisSessionStore{redis: s.redis}
	return rs.GetAPIKey(token)
}

func (s *JWTSessionStore) ListAPIKeys(tenantID string) ([]*APIKey, error) {
	if s.redis == nil {
		return nil, fmt.Errorf("api keys require Redis")
	}
	rs := &RedisSessionStore{redis: s.redis}
	return rs.ListAPIKeys(tenantID)
}

func (s *JWTSessionStore) RevokeAPIKey(tenantID, keyID string) error {
	if s.redis == nil {
		return fmt.Errorf("api keys require Redis")
	}
	rs := &RedisSessionStore{redis: s.redis}
	return rs.RevokeAPIKey(tenantID, keyID)
}

// ── Internal helpers ───────────────────────────────────────────────────────

func (s *JWTSessionStore) sign(claims jwtClaims) (string, error) {
	payload, err := json.Marshal(claims)
	if err != nil {
		return "", err
	}
	b64 := base64.RawURLEncoding.EncodeToString(payload)
	sig := s.computeHMAC(b64)
	return b64 + "." + sig, nil
}

func (s *JWTSessionStore) verify(token string) (*jwtClaims, error) {
	dot := strings.LastIndex(token, ".")
	if dot < 0 {
		return nil, fmt.Errorf("invalid session token")
	}
	b64, sig := token[:dot], token[dot+1:]

	if !hmac.Equal([]byte(s.computeHMAC(b64)), []byte(sig)) {
		return nil, fmt.Errorf("invalid session token")
	}

	payload, err := base64.RawURLEncoding.DecodeString(b64)
	if err != nil {
		return nil, fmt.Errorf("invalid session token")
	}

	var claims jwtClaims
	if err := json.Unmarshal(payload, &claims); err != nil {
		return nil, fmt.Errorf("invalid session token")
	}

	if time.Now().Unix() > claims.ExpiresAt {
		return nil, fmt.Errorf("session expired")
	}

	return &claims, nil
}

func (s *JWTSessionStore) computeHMAC(data string) string {
	mac := hmac.New(sha256.New, s.secret)
	mac.Write([]byte(data))
	return base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
}

func (s *JWTSessionStore) isRevoked(jti string) bool {
	s.revokedMu.RLock()
	_, ok := s.revoked[jti]
	s.revokedMu.RUnlock()
	return ok
}

// checkRedisRevocation returns true if the session is explicitly revoked.
// A Redis error is treated as "not revoked" to preserve availability.
// Checks both JTI-level revocation and tenant-level "revoke all before" timestamps.
func (s *JWTSessionStore) checkRedisRevocation(jti, tenantID string, issuedAt int64) bool {
	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()

	// JTI-level revocation
	res, err := s.redis.Exists(ctx, jwtRevocationPrefix+jti).Result()
	if err != nil {
		log.Debug().Err(err).Msg("Redis revocation check skipped (unavailable)")
		return false // treat unavailability as "not revoked"
	}
	if res > 0 {
		// Cache locally so future requests skip Redis
		s.revokedMu.Lock()
		s.revoked[jti] = time.Now().Add(time.Hour)
		s.revokedMu.Unlock()
		return true
	}

	// Tenant-level "revoke all before" timestamp — set by DeleteAllSessionsForTenant.
	// Any JWT issued before that timestamp is treated as revoked.
	if tenantID != "" {
		revokeAllKey := jwtTenantRevocationPrefix + tenantID
		revokeAllTs, err := s.redis.Get(ctx, revokeAllKey).Int64()
		if err == nil && issuedAt < revokeAllTs {
			// Cache as a JTI-level revocation so we skip Redis next time.
			s.revokedMu.Lock()
			s.revoked[jti] = time.Now().Add(time.Hour)
			s.revokedMu.Unlock()
			return true
		}
	}

	return false
}

func (s *JWTSessionStore) cleanupRevocationCache() {
	ticker := time.NewTicker(15 * time.Minute)
	defer ticker.Stop()
	for range ticker.C {
		now := time.Now()
		s.revokedMu.Lock()
		for jti, exp := range s.revoked {
			if now.After(exp) {
				delete(s.revoked, jti)
			}
		}
		s.revokedMu.Unlock()
	}
}
