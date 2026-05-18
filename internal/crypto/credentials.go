// Package crypto provides credential encryption for sensitive data.
package crypto

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"errors"
	"io"

	"golang.org/x/crypto/hkdf"
)

const (
	// NonceSize is the size of the AES-GCM nonce (12 bytes)
	NonceSize = 12
	// KeySize is the size of the AES-256 key (32 bytes)
	KeySize = 32
	// TagSize is the size of the AES-GCM authentication tag (16 bytes)
	TagSize = 16
	// SaltSize is the size of the HKDF salt (16 bytes)
	SaltSize = 16
)

var (
	// ErrInvalidCiphertext indicates the ciphertext is too short or malformed
	ErrInvalidCiphertext = errors.New("invalid ciphertext: too short")
	// ErrDecryptionFailed indicates AES-GCM decryption failed (wrong key or tampered data)
	ErrDecryptionFailed = errors.New("decryption failed: authentication error")
)

// CredentialCipher provides AES-256-GCM encryption for credentials.
// Uses HKDF to derive encryption key from WireGuard private key.
type CredentialCipher struct {
	key []byte // 32-byte AES-256 key
}

// NewCredentialCipher creates a new cipher using the WireGuard private key.
// The private key is used with HKDF to derive a dedicated encryption key.
//
// Key derivation: HKDF-SHA256(privateKey, salt, "winbox-credentials") → 32-byte AES key
//
// This ensures:
// - Each tenant has unique encryption key (derived from their WG private key)
// - Credentials encrypted with one tenant's key can't be decrypted by another
// - Fast: single HKDF call, no external dependencies
func NewCredentialCipher(privateKey []byte) (*CredentialCipher, error) {
	if len(privateKey) != 32 {
		return nil, errors.New("private key must be 32 bytes")
	}

	// Use a fixed salt for deterministic key derivation
	// The tenant's private key provides the uniqueness
	salt := []byte("winbox-cred-salt")
	info := []byte("winbox-credentials")

	// HKDF-SHA256 to derive 32-byte AES key
	hkdfReader := hkdf.New(sha256.New, privateKey, salt, info)
	key := make([]byte, KeySize)
	if _, err := io.ReadFull(hkdfReader, key); err != nil {
		return nil, err
	}

	return &CredentialCipher{key: key}, nil
}

// Encrypt encrypts plaintext using AES-256-GCM.
// Returns: nonce(12) + ciphertext + tag(16)
//
// The format is designed for efficient single-allocation storage:
// - 12-byte random nonce (unique per encryption)
// - Variable-length ciphertext (same length as plaintext)
// - 16-byte authentication tag (GCM integrity)
func (c *CredentialCipher) Encrypt(plaintext []byte) ([]byte, error) {
	block, err := aes.NewCipher(c.key)
	if err != nil {
		return nil, err
	}

	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}

	// Generate random nonce
	nonce := make([]byte, NonceSize)
	if _, err := rand.Read(nonce); err != nil {
		return nil, err
	}

	// Allocate result buffer: nonce + ciphertext + tag
	// GCM Seal appends ciphertext+tag to dst
	result := make([]byte, NonceSize, NonceSize+len(plaintext)+TagSize)
	copy(result, nonce)

	// Encrypt and append to result
	result = gcm.Seal(result, nonce, plaintext, nil)

	return result, nil
}

// Decrypt decrypts ciphertext encrypted by Encrypt.
// Input format: nonce(12) + ciphertext + tag(16)
func (c *CredentialCipher) Decrypt(ciphertext []byte) ([]byte, error) {
	// Minimum size: nonce(12) + tag(16) = 28 bytes (empty plaintext)
	if len(ciphertext) < NonceSize+TagSize {
		return nil, ErrInvalidCiphertext
	}

	block, err := aes.NewCipher(c.key)
	if err != nil {
		return nil, err
	}

	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}

	nonce := ciphertext[:NonceSize]
	encryptedData := ciphertext[NonceSize:]

	plaintext, err := gcm.Open(nil, nonce, encryptedData, nil)
	if err != nil {
		return nil, ErrDecryptionFailed
	}

	return plaintext, nil
}

// EncryptString is a convenience wrapper for encrypting string credentials.
func (c *CredentialCipher) EncryptString(plaintext string) ([]byte, error) {
	return c.Encrypt([]byte(plaintext))
}

// DecryptString is a convenience wrapper for decrypting to string.
func (c *CredentialCipher) DecryptString(ciphertext []byte) (string, error) {
	plaintext, err := c.Decrypt(ciphertext)
	if err != nil {
		return "", err
	}
	return string(plaintext), nil
}

// EncryptCredentials encrypts both username and password in a single call.
// Returns (encryptedUsername, encryptedPassword, error)
func (c *CredentialCipher) EncryptCredentials(username, password string) ([]byte, []byte, error) {
	encUser, err := c.EncryptString(username)
	if err != nil {
		return nil, nil, err
	}

	encPass, err := c.EncryptString(password)
	if err != nil {
		return nil, nil, err
	}

	return encUser, encPass, nil
}

// DecryptCredentials decrypts both username and password in a single call.
// Returns (username, password, error)
func (c *CredentialCipher) DecryptCredentials(encUsername, encPassword []byte) (string, string, error) {
	username, err := c.DecryptString(encUsername)
	if err != nil {
		return "", "", err
	}

	password, err := c.DecryptString(encPassword)
	if err != nil {
		return "", "", err
	}

	return username, password, nil
}
