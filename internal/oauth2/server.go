package oauth2

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/rsa"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
)

// Server implements RFC 8628 OAuth 2.0 Device Authorization Grant
type Server struct {
	config        *Config
	store         Store
	signingKey    interface{} // *ecdsa.PrivateKey or *rsa.PrivateKey
	signingMethod jwt.SigningMethod
	pollMu        sync.Mutex // protects the StatusAuthorized → StatusConsumed transition
}

// NewServer creates a new OAuth2 device authorization server
func NewServer(config *Config, store Store) (*Server, error) {
	if config == nil {
		config = DefaultConfig()
	}

	// Prefer the explicitly-passed store, then config.Store, then in-memory fallback.
	if store == nil {
		if config.Store != nil {
			store = config.Store
		} else {
			store = NewMemoryStore()
		}
	}

	s := &Server{config: config, store: store}

	// Use HS256 with the shared secret when provided — all instances sharing the
	// same secret can sign and verify each other's tokens.
	if len(config.SigningSecret) > 0 {
		s.signingKey = config.SigningSecret
		s.signingMethod = jwt.SigningMethodHS256
		return s, nil
	}

	// Fall back to a per-instance ECDSA key (single-instance deployments only).
	privateKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return nil, fmt.Errorf("failed to generate signing key: %w", err)
	}
	s.signingKey = privateKey
	s.signingMethod = jwt.SigningMethodES256
	return s, nil
}

// WithRSASigning configures the server to use RSA signing instead of ECDSA
func (s *Server) WithRSASigning(bits int) (*Server, error) {
	privateKey, err := rsa.GenerateKey(rand.Reader, bits)
	if err != nil {
		return nil, fmt.Errorf("failed to generate RSA key: %w", err)
	}
	s.signingKey = privateKey
	s.signingMethod = jwt.SigningMethodRS256
	return s, nil
}

// StartDeviceFlow initiates a device authorization request per RFC 8628 Section 3.1
func (s *Server) StartDeviceFlow(clientID, deviceID string) (*DeviceAuthorizationResponse, error) {
	if clientID == "" {
		clientID = "wantastic-device-client"
	}
	
	// Generate codes
	deviceCode, err := generateDeviceCode(s.config.DeviceCodeLength)
	if err != nil {
		return nil, fmt.Errorf("failed to generate device code: %w", err)
	}
	
	userCode, err := generateUserCode(s.config.UserCodeLength)
	if err != nil {
		return nil, fmt.Errorf("failed to generate user code: %w", err)
	}
	
	now := time.Now()
	req := &DeviceRequest{
		DeviceCode: deviceCode,
		UserCode:   userCode,
		Status:     StatusPending,
		ClientID:   clientID,
		DeviceID:   deviceID,
		CreatedAt:  now,
		ExpiresAt:  now.Add(s.config.DeviceCodeLifetime),
	}
	
	if err := s.store.Create(req); err != nil {
		return nil, fmt.Errorf("failed to store request: %w", err)
	}
	
	// Build verification URIs
	verificationURI := fmt.Sprintf("%s/activate", s.config.Issuer)
	verificationURIComplete := fmt.Sprintf("%s?user_code=%s", verificationURI, userCode)
	
	return &DeviceAuthorizationResponse{
		DeviceCode:              deviceCode,
		UserCode:                userCode,
		VerificationURI:         verificationURI,
		VerificationURIComplete: verificationURIComplete,
		ExpiresIn:               int(s.config.DeviceCodeLifetime.Seconds()),
		Interval:                int(s.config.MinPollInterval.Seconds()),
	}, nil
}

// PollDeviceToken polls for an access token per RFC 8628 Section 3.4
// Returns (token, error) where error can be ErrAuthorizationPending, ErrSlowDown, etc.
func (s *Server) PollDeviceToken(deviceCode string) (*TokenResponse, error) {
	if deviceCode == "" {
		return nil, errors.New("device code is required")
	}
	
	req, err := s.store.GetByDeviceCode(deviceCode)
	if err != nil {
		return nil, errors.New(ErrExpiredToken)
	}
	
	// Check expiration
	if req.IsExpired() {
		req.Status = StatusExpired
		s.store.Update(req)
		return nil, errors.New(ErrExpiredToken)
	}
	
	// Rate limiting check
	if !req.CanPoll(s.config.MinPollInterval) {
		return nil, errors.New(ErrSlowDown)
	}
	
	// Update last poll time
	req.LastPolledAt = time.Now()
	s.store.Update(req)
	
	// Check authorization status
	switch req.Status {
	case StatusPending:
		return nil, errors.New(ErrAuthorizationPending)

	case StatusAuthorized:
		// Atomic consume: hold the lock to prevent duplicate token issuance
		// from concurrent polls that both observed StatusAuthorized.
		s.pollMu.Lock()
		defer s.pollMu.Unlock()

		// Re-fetch under lock to check that another goroutine hasn't consumed it yet
		req, err = s.store.GetByDeviceCode(deviceCode)
		if err != nil {
			return nil, errors.New(ErrExpiredToken)
		}
		if req.Status == StatusConsumed {
			return nil, errors.New(ErrExpiredToken)
		}
		if req.Status != StatusAuthorized {
			return nil, errors.New(ErrAuthorizationPending)
		}

		// Generate access token
		token, err := s.generateAccessToken(req)
		if err != nil {
			return nil, fmt.Errorf("failed to generate access token: %w", err)
		}

		// Mark as consumed atomically before returning the token
		req.Status = StatusConsumed
		req.AccessToken = token.AccessToken
		s.store.Update(req)

		return token, nil

	case StatusDenied:
		return nil, errors.New(ErrAccessDenied)

	case StatusExpired:
		return nil, errors.New(ErrExpiredToken)

	case StatusConsumed:
		return nil, errors.New(ErrExpiredToken)

	default:
		return nil, errors.New(ErrAuthorizationPending)
	}
}

// AuthorizeDevice authorizes a device request (called by the user via web UI)
func (s *Server) AuthorizeDevice(userCode, userID, email, name, tenantID, tier string) error {
	req, err := s.store.GetByUserCode(userCode)
	if err != nil {
		return fmt.Errorf("user code not found: %w", err)
	}
	
	if req.Status != StatusPending {
		return fmt.Errorf("request is not pending (status: %s)", req.Status)
	}
	
	if req.IsExpired() {
		req.Status = StatusExpired
		s.store.Update(req)
		return errors.New("request has expired")
	}
	
	req.UserID = userID
	req.Email = email
	req.Name = name
	req.TenantID = tenantID
	req.Tier = tier
	req.Status = StatusAuthorized
	
	return s.store.Update(req)
}

// DenyDevice denies a device request (called by the user via web UI)
func (s *Server) DenyDevice(userCode string) error {
	req, err := s.store.GetByUserCode(userCode)
	if err != nil {
		return fmt.Errorf("user code not found: %w", err)
	}
	
	if req.Status != StatusPending {
		return fmt.Errorf("request is not pending (status: %s)", req.Status)
	}
	
	req.Status = StatusDenied
	return s.store.Update(req)
}

// GetPendingRequest retrieves a pending request by user code (for web UI)
func (s *Server) GetPendingRequest(userCode string) (*DeviceRequest, error) {
	req, err := s.store.GetByUserCode(userCode)
	if err != nil {
		return nil, err
	}
	
	if req.Status != StatusPending || req.IsExpired() {
		return nil, errors.New("request is no longer pending")
	}
	
	return req, nil
}

// ValidateAccessToken validates an access token and returns its claims
func (s *Server) ValidateAccessToken(tokenString string) (*AccessTokenClaims, error) {
	token, err := jwt.ParseWithClaims(tokenString, &AccessTokenClaims{}, func(token *jwt.Token) (interface{}, error) {
		if token.Method != s.signingMethod {
			return nil, fmt.Errorf("unexpected signing method: %v", token.Header["alg"])
		}
		// Return the verification key
		switch key := s.signingKey.(type) {
		case *ecdsa.PrivateKey:
			return &key.PublicKey, nil
		case *rsa.PrivateKey:
			return &key.PublicKey, nil
		case []byte:
			return key, nil
		default:
			return nil, errors.New("unsupported signing key type")
		}
	}, jwt.WithIssuer(s.config.Issuer))
	
	if err != nil {
		return nil, fmt.Errorf("token validation failed: %w", err)
	}
	
	if !token.Valid {
		return nil, errors.New("token is invalid")
	}
	
	claims, ok := token.Claims.(*AccessTokenClaims)
	if !ok {
		return nil, errors.New("invalid token claims")
	}
	
	return claims, nil
}

// AccessTokenClaims represents the JWT claims for access tokens
type AccessTokenClaims struct {
	jwt.RegisteredClaims
	
	DeviceID string `json:"device_id"`
	UserID   string `json:"user_id"`
	Email    string `json:"email"`
	Name     string `json:"name"`
	TenantID string `json:"tenant_id"`
	Tier     string `json:"tier"`
}

func (s *Server) generateAccessToken(req *DeviceRequest) (*TokenResponse, error) {
	now := time.Now()
	expiresAt := now.Add(s.config.AccessTokenLifetime)
	
	claims := AccessTokenClaims{
		RegisteredClaims: jwt.RegisteredClaims{
			ID:        uuid.New().String(),
			Issuer:    s.config.Issuer,
			Subject:   req.UserID,
			Audience:  jwt.ClaimStrings{"wantastic-api"},
			IssuedAt:  jwt.NewNumericDate(now),
			NotBefore: jwt.NewNumericDate(now),
			ExpiresAt: jwt.NewNumericDate(expiresAt),
		},
		DeviceID: req.DeviceID,
		UserID:   req.UserID,
		Email:    req.Email,
		Name:     req.Name,
		TenantID: req.TenantID,
		Tier:     req.Tier,
	}
	
	token := jwt.NewWithClaims(s.signingMethod, claims)
	tokenString, err := token.SignedString(s.signingKey)
	if err != nil {
		return nil, err
	}
	
	return &TokenResponse{
		AccessToken: tokenString,
		TokenType:   "Bearer",
		ExpiresIn:   int(s.config.AccessTokenLifetime.Seconds()),
	}, nil
}
