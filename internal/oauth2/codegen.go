package oauth2

import (
	"crypto/rand"
	"encoding/base32"
	"encoding/base64"
	"fmt"
	"strings"
)

// generateDeviceCode generates a cryptographically secure random device code
// Device codes should be unguessable and opaque to clients
func generateDeviceCode(length int) (string, error) {
	if length < 20 {
		length = 40 // RFC recommends at least 128 bits of entropy
	}
	
	// Calculate bytes needed for the desired string length
	// base64 encoding expands by 4/3, so we need length * 3/4 bytes
	byteLen := length * 3 / 4
	if byteLen < 16 {
		byteLen = 16
	}
	
	bytes := make([]byte, byteLen)
	if _, err := rand.Read(bytes); err != nil {
		return "", err
	}
	
	// Use URL-safe base64 encoding (no padding needed for opaque tokens)
	return base64.RawURLEncoding.EncodeToString(bytes), nil
}

// generateUserCode generates a human-readable user code
// Per RFC 8628: 8 characters, easy to type, case-insensitive
// Format: XXXX-YYYY (with hyphen for readability)
func generateUserCode(length int) (string, error) {
	if length < 4 {
		length = 8 // Default per RFC
	}
	
	// Use base32 encoding (A-Z, 2-7) - excludes confusing chars like 0, O, 1, I
	// This gives us 5 bits per character
	byteLen := (length*5 + 7) / 8 // Ceiling division
	
	bytes := make([]byte, byteLen)
	if _, err := rand.Read(bytes); err != nil {
		return "", err
	}
	
	// Encode to base32 (uppercase)
	encoded := base32.StdEncoding.EncodeToString(bytes)
	
	// Take only what we need and clean up
	code := strings.ToUpper(encoded[:length])
	
	// Remove any padding characters
	code = strings.ReplaceAll(code, "=", "")
	
	// Insert hyphen in middle for readability (e.g., ABCD-EFGH)
	mid := len(code) / 2
	if mid > 0 && len(code) >= 4 {
		return fmt.Sprintf("%s-%s", code[:mid], code[mid:]), nil
	}
	
	return code, nil
}
