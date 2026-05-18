package oauth2

import (
	"context"
	"errors"
	"time"

	"github.com/redis/go-redis/v9"
)

// Store defines the interface for device authorization request storage
type Store interface {
	// Device Flow Methods (RFC 8628)
	
	// Create stores a new device authorization request
	Create(req *DeviceRequest) error
	
	// GetByDeviceCode retrieves a request by device code
	GetByDeviceCode(deviceCode string) (*DeviceRequest, error)
	
	// GetByUserCode retrieves a request by user code
	GetByUserCode(userCode string) (*DeviceRequest, error)
	
	// Update updates an existing request
	Update(req *DeviceRequest) error
	
	// Delete removes a request
	Delete(deviceCode string) error
	
	// Authorization Code Flow Methods (RFC 6749 + PKCE)
	
	// CreateAuthorization stores a new authorization request with PKCE
	CreateAuthorization(req *AuthorizationRequest) error
	
	// GetAuthorizationByCode retrieves an authorization request by code
	GetAuthorizationByCode(code string) (*AuthorizationRequest, error)
	
	// UpdateAuthorization updates an existing authorization request
	UpdateAuthorization(req *AuthorizationRequest) error
	
	// DeleteAuthorization removes an authorization request
	DeleteAuthorization(code string) error
}

// MemoryStore is an alias for SecureMemoryStore.
// It provides security-hardened in-memory storage with proper data sanitization.
type MemoryStore = SecureMemoryStore

// NewMemoryStore creates a new secure in-memory store
func NewMemoryStore() *MemoryStore {
	return NewSecureMemoryStore()
}

// RedisStore uses Redis for distributed deployments
type RedisStore struct {
	client *redis.Client
	prefix string
}

// NewRedisStore creates a new Redis-backed store
func NewRedisStore(client *redis.Client) *RedisStore {
	return &RedisStore{
		client: client,
		prefix: "oauth2:device:",
	}
}

func (s *RedisStore) deviceKey(code string) string {
	return s.prefix + "dc:" + code
}

func (s *RedisStore) userKey(code string) string {
	return s.prefix + "uc:" + code
}

func (s *RedisStore) Create(req *DeviceRequest) error {
	ctx := context.Background()
	ttl := time.Until(req.ExpiresAt)
	if ttl <= 0 {
		ttl = 10 * time.Minute
	}
	
	// Store by device code
	deviceKey := s.deviceKey(req.DeviceCode)
	if err := s.client.HSet(ctx, deviceKey, map[string]interface{}{
		"device_code": req.DeviceCode,
		"user_code":   req.UserCode,
		"status":      req.Status.String(),
		"client_id":   req.ClientID,
		"device_id":   req.DeviceID,
		"user_id":     req.UserID,
		"email":       req.Email,
		"name":        req.Name,
		"tenant_id":   req.TenantID,
		"tier":        req.Tier,
		"access_token": req.AccessToken,
		"created_at":  req.CreatedAt.Format(time.RFC3339),
		"expires_at":  req.ExpiresAt.Format(time.RFC3339),
	}).Err(); err != nil {
		return err
	}
	
	if err := s.client.Expire(ctx, deviceKey, ttl).Err(); err != nil {
		return err
	}
	
	// Store user code -> device code mapping
	userKey := s.userKey(req.UserCode)
	if err := s.client.Set(ctx, userKey, req.DeviceCode, ttl).Err(); err != nil {
		return err
	}
	
	return nil
}

func (s *RedisStore) GetByDeviceCode(deviceCode string) (*DeviceRequest, error) {
	ctx := context.Background()
	deviceKey := s.deviceKey(deviceCode)
	
	data, err := s.client.HGetAll(ctx, deviceKey).Result()
	if err != nil {
		return nil, err
	}
	if len(data) == 0 {
		return nil, errors.New("device code not found")
	}
	
	return s.hydrate(data), nil
}

func (s *RedisStore) GetByUserCode(userCode string) (*DeviceRequest, error) {
	ctx := context.Background()
	userKey := s.userKey(userCode)
	
	// Get device code from user code mapping
	deviceCode, err := s.client.Get(ctx, userKey).Result()
	if err == redis.Nil {
		return nil, errors.New("user code not found")
	}
	if err != nil {
		return nil, err
	}
	
	return s.GetByDeviceCode(deviceCode)
}

func (s *RedisStore) Update(req *DeviceRequest) error {
	ctx := context.Background()
	deviceKey := s.deviceKey(req.DeviceCode)
	
	exists, err := s.client.Exists(ctx, deviceKey).Result()
	if err != nil {
		return err
	}
	if exists == 0 {
		return errors.New("device code not found")
	}
	
	return s.client.HSet(ctx, deviceKey, map[string]interface{}{
		"status":       req.Status.String(),
		"user_id":      req.UserID,
		"email":        req.Email,
		"name":         req.Name,
		"tenant_id":    req.TenantID,
		"tier":         req.Tier,
		"access_token": req.AccessToken,
	}).Err()
}

func (s *RedisStore) Delete(deviceCode string) error {
	ctx := context.Background()
	
	// Get user code first
	deviceKey := s.deviceKey(deviceCode)
	userCode, err := s.client.HGet(ctx, deviceKey, "user_code").Result()
	if err == nil && userCode != "" {
		s.client.Del(ctx, s.userKey(userCode))
	}
	
	return s.client.Del(ctx, deviceKey).Err()
}

func (s *RedisStore) hydrate(data map[string]string) *DeviceRequest {
	req := &DeviceRequest{
		DeviceCode:  data["device_code"],
		UserCode:    data["user_code"],
		ClientID:    data["client_id"],
		DeviceID:    data["device_id"],
		UserID:      data["user_id"],
		Email:       data["email"],
		Name:        data["name"],
		TenantID:    data["tenant_id"],
		Tier:        data["tier"],
		AccessToken: data["access_token"],
	}
	
	// Parse status
	switch data["status"] {
	case "authorized":
		req.Status = StatusAuthorized
	case "denied":
		req.Status = StatusDenied
	case "expired":
		req.Status = StatusExpired
	case "consumed":
		req.Status = StatusConsumed
	default:
		req.Status = StatusPending
	}
	
	// Parse timestamps
	if t, err := time.Parse(time.RFC3339, data["created_at"]); err == nil {
		req.CreatedAt = t
	}
	if t, err := time.Parse(time.RFC3339, data["expires_at"]); err == nil {
		req.ExpiresAt = t
	}
	
	return req
}

// Authorization code flow methods for RedisStore

func (s *RedisStore) authCodeKey(code string) string {
	return s.prefix + "auth:" + code
}

func (s *RedisStore) CreateAuthorization(req *AuthorizationRequest) error {
	ctx := context.Background()
	ttl := time.Until(req.ExpiresAt)
	if ttl <= 0 {
		ttl = 10 * time.Minute
	}
	
	authKey := s.authCodeKey(req.AuthorizationCode)
	if err := s.client.HSet(ctx, authKey, map[string]interface{}{
		"authorization_code":   req.AuthorizationCode,
		"client_id":            req.ClientID,
		"redirect_uri":         req.RedirectURI,
		"state":                req.State,
		"scope":                req.Scope,
		"code_challenge":       req.CodeChallenge,
		"code_challenge_method": req.CodeChallengeMethod,
		"device_id":            req.DeviceID,
		"user_id":              req.UserID,
		"email":                req.Email,
		"name":                 req.Name,
		"tenant_id":            req.TenantID,
		"tier":                 req.Tier,
		"created_at":           req.CreatedAt.Format(time.RFC3339),
		"expires_at":           req.ExpiresAt.Format(time.RFC3339),
	}).Err(); err != nil {
		return err
	}
	
	return s.client.Expire(ctx, authKey, ttl).Err()
}

func (s *RedisStore) GetAuthorizationByCode(code string) (*AuthorizationRequest, error) {
	ctx := context.Background()
	authKey := s.authCodeKey(code)
	
	data, err := s.client.HGetAll(ctx, authKey).Result()
	if err != nil {
		return nil, err
	}
	if len(data) == 0 {
		return nil, errors.New("authorization code not found")
	}
	
	return s.hydrateAuthorization(data), nil
}

func (s *RedisStore) UpdateAuthorization(req *AuthorizationRequest) error {
	ctx := context.Background()
	authKey := s.authCodeKey(req.AuthorizationCode)
	
	exists, err := s.client.Exists(ctx, authKey).Result()
	if err != nil {
		return err
	}
	if exists == 0 {
		return errors.New("authorization code not found")
	}
	
	return s.client.HSet(ctx, authKey, map[string]interface{}{
		"user_id":   req.UserID,
		"email":     req.Email,
		"name":      req.Name,
		"tenant_id": req.TenantID,
		"tier":      req.Tier,
	}).Err()
}

func (s *RedisStore) DeleteAuthorization(code string) error {
	ctx := context.Background()
	return s.client.Del(ctx, s.authCodeKey(code)).Err()
}

func (s *RedisStore) hydrateAuthorization(data map[string]string) *AuthorizationRequest {
	req := &AuthorizationRequest{
		AuthorizationCode:   data["authorization_code"],
		ClientID:            data["client_id"],
		RedirectURI:         data["redirect_uri"],
		State:               data["state"],
		Scope:               data["scope"],
		CodeChallenge:       data["code_challenge"],
		CodeChallengeMethod: data["code_challenge_method"],
		DeviceID:            data["device_id"],
		UserID:              data["user_id"],
		Email:               data["email"],
		Name:                data["name"],
		TenantID:            data["tenant_id"],
		Tier:                data["tier"],
	}
	
	// Parse timestamps
	if t, err := time.Parse(time.RFC3339, data["created_at"]); err == nil {
		req.CreatedAt = t
	}
	if t, err := time.Parse(time.RFC3339, data["expires_at"]); err == nil {
		req.ExpiresAt = t
	}
	
	return req
}
