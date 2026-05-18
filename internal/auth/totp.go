package auth

import (
	"encoding/base32"
	"encoding/base64"
	"fmt"
	"time"

	"github.com/skip2/go-qrcode"
	"github.com/xlzd/gotp"
)

// TOTPManager handles TOTP (Time-based One-Time Password) operations.
type TOTPManager struct {
	secret string
	issuer string
}

// NewTOTPManager creates a new TOTP manager with the given secret.
func NewTOTPManager(secret, issuer string) *TOTPManager {
	return &TOTPManager{
		secret: secret,
		issuer: issuer,
	}
}

// GenerateTOTPSecret generates a new random TOTP secret (base32-encoded).
// Returns a 16-character base32 string suitable for Google Authenticator.
func GenerateTOTPSecret() string {
	return gotp.RandomSecret(16)
}

// ValidateTOTP validates a 6-digit TOTP code against the secret.
// Returns true if the code is valid (within time window).
func ValidateTOTP(secret, code string) bool {
	if secret == "" || code == "" {
		return false
	}

	// Create TOTP instance
	totp := gotp.NewDefaultTOTP(secret)

	// Verify code using UTC time (TOTP must use UTC)
	return totp.Verify(code, time.Now().UTC().Unix())
}

// ValidateTOTPWithWindow validates a TOTP code with a custom time window.
// Window specifies how many 30-second intervals to check before/after current time.
func ValidateTOTPWithWindow(secret, code string, window int) bool {
	if secret == "" || code == "" {
		return false
	}

	totp := gotp.NewDefaultTOTP(secret)
	// Use UTC time consistently (TOTP standard requires UTC)
	currentTime := time.Now().UTC().Unix()

	// Check current time and window around it
	for i := -window; i <= window; i++ {
		if totp.Verify(code, currentTime+int64(i*30)) {
			return true
		}
	}

	return false
}

// GenerateTOTPURL generates a provisioning URL for QR code generation.
// Format: otpauth://totp/ISSUER:USERNAME?secret=SECRET&issuer=ISSUER
func GenerateTOTPURL(secret, username, issuer string) string {
	if issuer == "" {
		issuer = "WantasticCore"
	}

	totp := gotp.NewDefaultTOTP(secret)
	return totp.ProvisioningUri(username, issuer)
}

// GenerateTOTPQRCode generates a base64-encoded PNG QR code for the provisioning URL.
// Returns a data URI string that can be used directly in <enhanced:img src="...">.
func GenerateTOTPQRCode(secret, username, issuer string) (string, error) {
	provisioningURL := GenerateTOTPURL(secret, username, issuer)

	// Generate QR code as PNG (256x256 pixels, medium recovery level)
	qrBytes, err := qrcode.Encode(provisioningURL, qrcode.Medium, 256)
	if err != nil {
		return "", fmt.Errorf("failed to generate QR code: %w", err)
	}

	// Encode to base64 and create data URI
	base64Str := base64.StdEncoding.EncodeToString(qrBytes)
	dataURI := "data:image/png;base64," + base64Str

	return dataURI, nil
}

// GenerateTOTPCode generates the current TOTP code for the given secret.
// Useful for testing and verification.
func GenerateTOTPCode(secret string) string {
	if secret == "" {
		return ""
	}

	totp := gotp.NewDefaultTOTP(secret)
	return totp.Now()
}

// IsValidBase32 checks if a string is valid base32 encoding.
// Handles both padded and unpadded base32 strings (TOTP secrets are typically unpadded).
func IsValidBase32(s string) bool {
	if s == "" {
		return false
	}

	// Try with standard encoding (with padding)
	_, err := base32.StdEncoding.DecodeString(s)
	if err == nil {
		return true
	}

	// Try without padding (gotp generates unpadded secrets)
	_, err = base32.StdEncoding.WithPadding(base32.NoPadding).DecodeString(s)
	return err == nil
}

// FormatSecretForDisplay formats a TOTP secret for display (groups of 4 chars).
// Example: "JBSWY3DPEHPK3PXP" -> "JBSW Y3DP EHPK 3PXP"
func FormatSecretForDisplay(secret string) string {
	if len(secret) == 0 {
		return ""
	}

	var formatted string
	for i, char := range secret {
		if i > 0 && i%4 == 0 {
			formatted += " "
		}
		formatted += string(char)
	}

	return formatted
}

// TOTPConfig holds TOTP configuration for the admin panel.
type TOTPConfig struct {
	Issuer     string
	Period     int // Time step in seconds (default: 30)
	Digits     int // Number of digits (default: 6)
	WindowSize int // How many time steps to check before/after (default: 1)
}

// DefaultTOTPConfig returns the default TOTP configuration.
func DefaultTOTPConfig() *TOTPConfig {
	return &TOTPConfig{
		Issuer:     "WantasticCore",
		Period:     30,
		Digits:     6,
		WindowSize: 1,
	}
}

// Validate checks if the TOTP configuration is valid.
func (c *TOTPConfig) Validate() error {
	if c.Issuer == "" {
		return fmt.Errorf("issuer cannot be empty")
	}
	if c.Period <= 0 {
		return fmt.Errorf("period must be positive")
	}
	if c.Digits != 6 && c.Digits != 8 {
		return fmt.Errorf("digits must be 6 or 8")
	}
	if c.WindowSize < 0 {
		return fmt.Errorf("window size cannot be negative")
	}

	return nil
}
