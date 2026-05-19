// Package crypto provides session-based encryption for WebSocket messages.
// This file implements X25519 ECDH key exchange and AES-256-GCM message encryption.
package crypto

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/binary"
	"errors"
	"io"
	"sync/atomic"

	"golang.org/x/crypto/curve25519"
	"golang.org/x/crypto/hkdf"
)

const (
	// X25519KeySize is the size of X25519 keys (32 bytes)
	X25519KeySize = 32
	// SessionNonceSize is the size of the AES-GCM nonce (12 bytes)
	SessionNonceSize = 12
	// SessionKeySize is the size of the AES-256 key (32 bytes)
	SessionKeySize = 32
	// SessionTagSize is the size of the AES-GCM authentication tag (16 bytes)
	SessionTagSize = 16
)

var (
	// ErrKeyExchangeFailed indicates ECDH key exchange failed
	ErrKeyExchangeFailed = errors.New("key exchange failed")
	// ErrEncryptionNotEnabled indicates encryption is not yet enabled for this session
	ErrEncryptionNotEnabled = errors.New("encryption not enabled")
	// ErrInvalidPublicKey indicates the public key is invalid
	ErrInvalidPublicKey = errors.New("invalid public key")
	// ErrMessageDecryptionFailed indicates message decryption failed
	ErrMessageDecryptionFailed = errors.New("message decryption failed")
)

// SessionKeyPair holds an X25519 key pair for ECDH key exchange.
type SessionKeyPair struct {
	PrivateKey [X25519KeySize]byte
	PublicKey  [X25519KeySize]byte
}

// GenerateSessionKeyPair generates a new X25519 key pair for session encryption.
func GenerateSessionKeyPair() (*SessionKeyPair, error) {
	kp := &SessionKeyPair{}

	// Generate random private key
	if _, err := rand.Read(kp.PrivateKey[:]); err != nil {
		return nil, err
	}

	// Clamp private key per X25519 spec
	kp.PrivateKey[0] &= 248
	kp.PrivateKey[31] &= 127
	kp.PrivateKey[31] |= 64

	// Derive public key
	curve25519.ScalarBaseMult(&kp.PublicKey, &kp.PrivateKey)

	return kp, nil
}

// PublicKeyBase64 returns the public key as a base64 string for transmission.
func (kp *SessionKeyPair) PublicKeyBase64() string {
	return base64.StdEncoding.EncodeToString(kp.PublicKey[:])
}

// SessionCipher provides AES-256-GCM encryption for WebSocket messages.
// It uses an atomic counter for nonce generation to prevent replay attacks.
type SessionCipher struct {
	encryptionKey [SessionKeySize]byte
	sendCounter   uint64 // atomic counter for outgoing messages
	recvCounter   uint64 // atomic counter for incoming messages (for validation)
	sessionID     string // bound to session for additional security
	enabled       bool
}

// ComputeSharedSecret performs X25519 ECDH to derive a shared secret.
func ComputeSharedSecret(privateKey *[X25519KeySize]byte, peerPublicKey *[X25519KeySize]byte) ([X25519KeySize]byte, error) {
	var sharedSecret [X25519KeySize]byte

	// Validate peer public key is not zero
	var zeroKey [X25519KeySize]byte
	if *peerPublicKey == zeroKey {
		return sharedSecret, ErrInvalidPublicKey
	}

	curve25519.ScalarMult(&sharedSecret, privateKey, peerPublicKey)

	// Check for low-order point (all zeros result)
	if sharedSecret == zeroKey {
		return sharedSecret, ErrKeyExchangeFailed
	}

	return sharedSecret, nil
}

// DeriveSessionKey derives an AES-256 key from the shared secret using HKDF.
// The sessionID is mixed in as "info" to bind the key to this specific session.
func DeriveSessionKey(sharedSecret *[X25519KeySize]byte, sessionID string) ([SessionKeySize]byte, error) {
	var key [SessionKeySize]byte

	// HKDF-SHA256 to derive session key
	// Salt includes a domain separator
	salt := []byte("wantastic-ws-session-v1")
	info := []byte("ws-session:" + sessionID)

	hkdfReader := hkdf.New(sha256.New, sharedSecret[:], salt, info)
	if _, err := io.ReadFull(hkdfReader, key[:]); err != nil {
		return key, err
	}

	return key, nil
}

// NewSessionCipher creates a new session cipher from a shared secret.
func NewSessionCipher(sharedSecret *[X25519KeySize]byte, sessionID string) (*SessionCipher, error) {
	key, err := DeriveSessionKey(sharedSecret, sessionID)
	if err != nil {
		return nil, err
	}

	return &SessionCipher{
		encryptionKey: key,
		sendCounter:   0,
		recvCounter:   0,
		sessionID:     sessionID,
		enabled:       true,
	}, nil
}

// NewSessionCipherFromKeyExchange creates a cipher from a key exchange.
// serverPrivateKey: server's X25519 private key
// clientPublicKeyBase64: client's public key as base64 string
// sessionID: unique session identifier
func NewSessionCipherFromKeyExchange(serverPrivateKey *[X25519KeySize]byte, clientPublicKeyBase64 string, sessionID string) (*SessionCipher, error) {
	// Decode client public key
	clientPubBytes, err := base64.StdEncoding.DecodeString(clientPublicKeyBase64)
	if err != nil {
		return nil, ErrInvalidPublicKey
	}
	if len(clientPubBytes) != X25519KeySize {
		return nil, ErrInvalidPublicKey
	}

	var clientPublicKey [X25519KeySize]byte
	copy(clientPublicKey[:], clientPubBytes)

	// Compute shared secret
	sharedSecret, err := ComputeSharedSecret(serverPrivateKey, &clientPublicKey)
	if err != nil {
		return nil, err
	}

	return NewSessionCipher(&sharedSecret, sessionID)
}

// IsEnabled returns whether encryption is enabled.
func (c *SessionCipher) IsEnabled() bool {
	return c != nil && c.enabled
}

// Encrypt encrypts a message using AES-256-GCM.
// Returns base64-encoded ciphertext with counter-based nonce.
// Format: counter(8 bytes) + nonce(12 bytes) + ciphertext + tag(16 bytes)
func (c *SessionCipher) Encrypt(plaintext []byte) (string, error) {
	if !c.enabled {
		return "", ErrEncryptionNotEnabled
	}

	block, err := aes.NewCipher(c.encryptionKey[:])
	if err != nil {
		return "", err
	}

	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", err
	}

	// Get next counter value (atomic)
	counter := atomic.AddUint64(&c.sendCounter, 1)

	// Build nonce from counter + random bytes
	// First 8 bytes: counter (prevents replay)
	// Last 4 bytes: random (adds entropy)
	nonce := make([]byte, SessionNonceSize)
	binary.BigEndian.PutUint64(nonce[:8], counter)
	if _, err := rand.Read(nonce[8:]); err != nil {
		return "", err
	}

	// Allocate result: counter(8) + nonce(12) + ciphertext + tag(16)
	result := make([]byte, 8+SessionNonceSize, 8+SessionNonceSize+len(plaintext)+SessionTagSize)

	// Prepend counter for verification
	binary.BigEndian.PutUint64(result[:8], counter)
	copy(result[8:8+SessionNonceSize], nonce)

	// Encrypt and append
	result = gcm.Seal(result, nonce, plaintext, nil)

	return base64.StdEncoding.EncodeToString(result), nil
}

// Decrypt decrypts a message encrypted by Encrypt.
// Input: base64-encoded ciphertext
func (c *SessionCipher) Decrypt(ciphertextBase64 string) ([]byte, error) {
	if !c.enabled {
		return nil, ErrEncryptionNotEnabled
	}

	ciphertext, err := base64.StdEncoding.DecodeString(ciphertextBase64)
	if err != nil {
		return nil, err
	}

	// Minimum size: counter(8) + nonce(12) + tag(16) = 36 bytes
	minSize := 8 + SessionNonceSize + SessionTagSize
	if len(ciphertext) < minSize {
		return nil, ErrInvalidCiphertext
	}

	block, err := aes.NewCipher(c.encryptionKey[:])
	if err != nil {
		return nil, err
	}

	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}

	// Extract counter and nonce
	counter := binary.BigEndian.Uint64(ciphertext[:8])
	nonce := ciphertext[8 : 8+SessionNonceSize]
	encryptedData := ciphertext[8+SessionNonceSize:]

	// Verify counter is greater than last received (replay protection)
	// Note: We use >= for the first message (counter starts at 0)
	lastRecv := atomic.LoadUint64(&c.recvCounter)
	if counter <= lastRecv && lastRecv > 0 {
		return nil, ErrMessageDecryptionFailed // Possible replay attack
	}

	// Decrypt
	plaintext, err := gcm.Open(nil, nonce, encryptedData, nil)
	if err != nil {
		return nil, ErrMessageDecryptionFailed
	}

	// Update receive counter
	atomic.StoreUint64(&c.recvCounter, counter)

	return plaintext, nil
}

// EncryptJSON is a convenience wrapper for encrypting JSON strings.
func (c *SessionCipher) EncryptJSON(jsonStr string) (string, error) {
	return c.Encrypt([]byte(jsonStr))
}

// DecryptJSON is a convenience wrapper for decrypting to JSON string.
func (c *SessionCipher) DecryptJSON(ciphertextBase64 string) (string, error) {
	plaintext, err := c.Decrypt(ciphertextBase64)
	if err != nil {
		return "", err
	}
	return string(plaintext), nil
}

// ParsePublicKeyBase64 parses a base64-encoded public key.
func ParsePublicKeyBase64(publicKeyBase64 string) (*[X25519KeySize]byte, error) {
	pubBytes, err := base64.StdEncoding.DecodeString(publicKeyBase64)
	if err != nil {
		return nil, ErrInvalidPublicKey
	}
	if len(pubBytes) != X25519KeySize {
		return nil, ErrInvalidPublicKey
	}

	var publicKey [X25519KeySize]byte
	copy(publicKey[:], pubBytes)
	return &publicKey, nil
}
