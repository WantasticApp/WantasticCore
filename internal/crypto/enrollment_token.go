// Package crypto provides token encryption for device enrollment.
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
	// EnrollmentKeySize is the size of the AES-256 key (32 bytes)
	EnrollmentKeySize = 32
	// EnrollmentNonceSize is the size of the AES-GCM nonce (12 bytes)
	EnrollmentNonceSize = 12
	// EnrollmentTagSize is the size of the AES-GCM authentication tag (16 bytes)
	EnrollmentTagSize = 16
)

var (
	// ErrInvalidEnrollmentToken indicates the token is malformed or tampered
	ErrInvalidEnrollmentToken = errors.New("invalid enrollment token")
	// ErrEnrollmentTokenExpired indicates the token has expired
	ErrEnrollmentTokenExpired = errors.New("enrollment token expired")
	// ErrEnrollmentDecryptionFailed indicates decryption failed
	ErrEnrollmentDecryptionFailed = errors.New("enrollment token decryption failed")
)

// EnrollmentTokenPayload contains the data encrypted in a device setup token.
type EnrollmentTokenPayload struct {
	ID        string    `json:"i"`           // Token ID (for database verification)
	TenantID  string    `json:"t"`           // Tenant ID
	ExpiresAt time.Time `json:"e,omitempty"` // Token expiration (optional)
}

// EnrollmentTokenCipher provides encryption for device enrollment tokens.
type EnrollmentTokenCipher struct {
	key []byte // 32-byte AES-256 key
}

// NewEnrollmentTokenCipher creates a new cipher for enrollment tokens.
// Uses HKDF to derive a dedicated key from the master server secret.
func NewEnrollmentTokenCipher(serverSecret []byte) (*EnrollmentTokenCipher, error) {
	if len(serverSecret) < 16 {
		return nil, errors.New("server secret must be at least 16 bytes")
	}

	// Use a distinct salt/info for domain separation
	salt := []byte("wantastic-enrollment-salt")
	info := []byte("device-enrollment-v1")

	// HKDF-SHA256 to derive 32-byte AES key
	hkdfReader := hkdf.New(sha256.New, serverSecret, salt, info)
	key := make([]byte, EnrollmentKeySize)
	if _, err := io.ReadFull(hkdfReader, key); err != nil {
		return nil, err
	}

	return &EnrollmentTokenCipher{
		key: key,
	}, nil
}

// GenerateToken creates an encrypted, URL-safe token for device enrollment.
// If expiry is zero, the token never expires (but can be revoked in DB).
func (c *EnrollmentTokenCipher) GenerateToken(tokenID, tenantID string, expiry time.Duration) (string, error) {
	payload := EnrollmentTokenPayload{
		ID:       tokenID,
		TenantID: tenantID,
	}
	if expiry > 0 {
		payload.ExpiresAt = time.Now().Add(expiry)
	}

	plaintext, err := json.Marshal(payload)
	if err != nil {
		return "", err
	}

	ciphertext, err := c.encrypt(plaintext)
	if err != nil {
		return "", err
	}

	return base64.RawURLEncoding.EncodeToString(ciphertext), nil
}

// ValidateToken decrypts and validates an enrollment token.
// Returns the tenant ID and token ID if valid.
func (c *EnrollmentTokenCipher) ValidateToken(token string) (string, string, error) {
	ciphertext, err := base64.RawURLEncoding.DecodeString(token)
	if err != nil {
		return "", "", ErrInvalidEnrollmentToken
	}

	plaintext, err := c.decrypt(ciphertext)
	if err != nil {
		return "", "", ErrEnrollmentDecryptionFailed
	}

	var payload EnrollmentTokenPayload
	if err := json.Unmarshal(plaintext, &payload); err != nil {
		return "", "", ErrInvalidEnrollmentToken
	}

	if !payload.ExpiresAt.IsZero() && time.Now().After(payload.ExpiresAt) {
		return "", "", ErrEnrollmentTokenExpired
	}

	if payload.TenantID == "" || payload.ID == "" {
		return "", "", ErrInvalidEnrollmentToken
	}

	return payload.TenantID, payload.ID, nil
}

func (c *EnrollmentTokenCipher) encrypt(plaintext []byte) ([]byte, error) {
	block, err := aes.NewCipher(c.key)
	if err != nil {
		return nil, err
	}

	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}

	nonce := make([]byte, EnrollmentNonceSize)
	if _, err := rand.Read(nonce); err != nil {
		return nil, err
	}

	return gcm.Seal(nonce, nonce, plaintext, nil), nil
}

func (c *EnrollmentTokenCipher) decrypt(ciphertext []byte) ([]byte, error) {
	if len(ciphertext) < EnrollmentNonceSize+EnrollmentTagSize {
		return nil, ErrInvalidEnrollmentToken
	}

	block, err := aes.NewCipher(c.key)
	if err != nil {
		return nil, err
	}

	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}

	nonce := ciphertext[:EnrollmentNonceSize]
	encryptedData := ciphertext[EnrollmentNonceSize:]

	return gcm.Open(nil, nonce, encryptedData, nil)
}
