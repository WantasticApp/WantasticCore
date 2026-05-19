// Package crypto provides notification hook encryption for unsubscribe links.
// This file implements secure token generation for email unsubscribe links.
package crypto

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"time"

	"golang.org/x/crypto/hkdf"
)

const (
	// HookNonceSize is the size of the AES-GCM nonce (12 bytes)
	HookNonceSize = 12
	// HookKeySize is the size of the AES-256 key (32 bytes)
	HookKeySize = 32
	// HookTagSize is the size of the AES-GCM authentication tag (16 bytes)
	HookTagSize = 16
	// DefaultHookTokenExpiry is the default token expiry duration (7 days)
	DefaultHookTokenExpiry = 7 * 24 * time.Hour
)

var (
	// ErrInvalidHookToken indicates the token is malformed or tampered
	ErrInvalidHookToken = errors.New("invalid hook token")
	// ErrHookTokenExpired indicates the token has expired
	ErrHookTokenExpired = errors.New("hook token expired")
	// ErrHookDecryptionFailed indicates decryption failed (wrong key or tampered)
	ErrHookDecryptionFailed = errors.New("hook token decryption failed")
)

// NotificationHookPayload contains the data encrypted in an unsubscribe link.
// This is a tenant-level token that disables all peer notifications for the tenant.
type NotificationHookPayload struct {
	TenantID   string    `json:"t"` // Tenant ID
	TenantName string    `json:"n"` // Tenant name for display
	ExpiresAt  time.Time `json:"e"` // Token expiration
}

// NotificationHookCipher provides encryption for notification hook tokens.
// Uses AES-256-GCM with a key derived from a server secret.
type NotificationHookCipher struct {
	key    []byte        // 32-byte AES-256 key
	expiry time.Duration // Token expiry duration
}

// NewNotificationHookCipher creates a new cipher for notification hook tokens.
// The serverSecret should be a high-entropy secret stored in config.
// A new secret will invalidate all existing unsubscribe tokens.
//
// Key derivation: HKDF-SHA256(serverSecret, salt, "notification-hook-v1") → 32-byte AES key
func NewNotificationHookCipher(serverSecret []byte) (*NotificationHookCipher, error) {
	if len(serverSecret) < 16 {
		return nil, errors.New("server secret must be at least 16 bytes")
	}

	// Use a fixed salt for deterministic key derivation
	salt := []byte("wantastic-hook-salt")
	info := []byte("notification-hook-v1")

	// HKDF-SHA256 to derive 32-byte AES key
	hkdfReader := hkdf.New(sha256.New, serverSecret, salt, info)
	key := make([]byte, HookKeySize)
	if _, err := io.ReadFull(hkdfReader, key); err != nil {
		return nil, err
	}

	return &NotificationHookCipher{
		key:    key,
		expiry: DefaultHookTokenExpiry,
	}, nil
}

// SetExpiry configures the token expiry duration.
func (c *NotificationHookCipher) SetExpiry(d time.Duration) {
	c.expiry = d
}

// GenerateToken creates an encrypted, URL-safe token for unsubscribing all peer notifications for a tenant.
// The token contains the tenant ID and expiration timestamp.
// Returns a base64url-encoded string suitable for use in URLs.
func (c *NotificationHookCipher) GenerateToken(tenantID, tenantName string) (string, error) {
	payload := NotificationHookPayload{
		TenantID:   tenantID,
		TenantName: tenantName,
		ExpiresAt:  time.Now().Add(c.expiry),
	}

	// Marshal payload to JSON
	plaintext, err := json.Marshal(payload)
	if err != nil {
		return "", err
	}

	// Encrypt with AES-256-GCM
	ciphertext, err := c.encrypt(plaintext)
	if err != nil {
		return "", err
	}

	// Encode as URL-safe base64
	return base64.RawURLEncoding.EncodeToString(ciphertext), nil
}

// ValidateToken decrypts and validates a hook token.
// Returns the payload if valid, or an error if invalid/expired.
func (c *NotificationHookCipher) ValidateToken(token string) (*NotificationHookPayload, error) {
	// Decode from URL-safe base64
	ciphertext, err := base64.RawURLEncoding.DecodeString(token)
	if err != nil {
		return nil, ErrInvalidHookToken
	}

	// Decrypt
	plaintext, err := c.decrypt(ciphertext)
	if err != nil {
		return nil, ErrHookDecryptionFailed
	}

	// Unmarshal payload
	var payload NotificationHookPayload
	if err := json.Unmarshal(plaintext, &payload); err != nil {
		return nil, ErrInvalidHookToken
	}

	// Check expiration
	if time.Now().After(payload.ExpiresAt) {
		return nil, ErrHookTokenExpired
	}

	return &payload, nil
}

// encrypt encrypts plaintext using AES-256-GCM.
// Returns: nonce(12) + ciphertext + tag(16)
func (c *NotificationHookCipher) encrypt(plaintext []byte) ([]byte, error) {
	block, err := aes.NewCipher(c.key)
	if err != nil {
		return nil, err
	}

	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}

	// Generate random nonce
	nonce := make([]byte, HookNonceSize)
	if _, err := rand.Read(nonce); err != nil {
		return nil, err
	}

	// Allocate result buffer: nonce + ciphertext + tag
	result := make([]byte, HookNonceSize, HookNonceSize+len(plaintext)+HookTagSize)
	copy(result, nonce)

	// Encrypt and append to result
	result = gcm.Seal(result, nonce, plaintext, nil)

	return result, nil
}

// decrypt decrypts ciphertext encrypted by encrypt.
// Input format: nonce(12) + ciphertext + tag(16)
func (c *NotificationHookCipher) decrypt(ciphertext []byte) ([]byte, error) {
	// Minimum size: nonce(12) + tag(16) = 28 bytes (empty plaintext)
	if len(ciphertext) < HookNonceSize+HookTagSize {
		return nil, ErrInvalidHookToken
	}

	block, err := aes.NewCipher(c.key)
	if err != nil {
		return nil, err
	}

	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}

	nonce := ciphertext[:HookNonceSize]
	encryptedData := ciphertext[HookNonceSize:]

	plaintext, err := gcm.Open(nil, nonce, encryptedData, nil)
	if err != nil {
		return nil, ErrHookDecryptionFailed
	}

	return plaintext, nil
}
