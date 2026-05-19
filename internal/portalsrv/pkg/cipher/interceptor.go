package cipher

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"net/http"
	"regexp"
	"strconv"
	"time"
)

const (
	HeaderTimestamp = "x-wantastic-ts"
	HeaderSignature = "x-wantastic-sig"
	HeaderDevice    = "x-wantastic-device" // Unique device ID included in HTTP signature
	WindowSeconds   = 30                   // Allow +/- 30 seconds drift
	SharedSecret    = "wantastic_cipher_v_1_0_0"
)

// Interceptor validates Wantastic agent request signatures.
// Pre–Stage 2 it also exposed gRPC unary/stream interceptors; with the
// in-process dispatch chain those have been removed and only the HTTP
// signature validator remains in use (called from the portal's agent
// register / token-exchange endpoints).
type Interceptor struct {
	secret []byte
}

func NewInterceptor() *Interceptor {
	return &Interceptor{
		secret: []byte(SharedSecret),
	}
}

// ValidateHTTPRequest validates the Wantastic agent signature from HTTP request headers.
//
// Unlike the gRPC interceptor (which signs only the timestamp), the HTTP variant
// includes the device ID in the HMAC so the proof is unique to each device:
//
//	signature = HMAC-SHA256(SharedSecret, timestamp + ":" + deviceID)
//
// Required headers:
//
//	x-wantastic-ts     — Unix seconds (must be within ±WindowSeconds of server time)
//	x-wantastic-sig    — hex-encoded HMAC described above
//	x-wantastic-device — client-generated device fingerprint / unique ID
//
// Returns the deviceID on success so callers can log or store it.
// On failure, returns a generic error to avoid leaking validation details.
func (i *Interceptor) ValidateHTTPRequest(r *http.Request) (deviceID string, err error) {
	tsStr := r.Header.Get(HeaderTimestamp)
	if tsStr == "" {
		return "", errors.New("unauthorized")
	}
	ts, err := strconv.ParseInt(tsStr, 10, 64)
	if err != nil {
		return "", errors.New("unauthorized")
	}
	now := time.Now().Unix()
	if ts < now-WindowSeconds || ts > now+WindowSeconds {
		return "", errors.New("unauthorized")
	}

	gotSig := r.Header.Get(HeaderSignature)
	if gotSig == "" {
		return "", errors.New("unauthorized")
	}

	deviceID = r.Header.Get(HeaderDevice)
	if deviceID == "" {
		return "", errors.New("unauthorized")
	}

	// Signature covers ts + device ID so each device's token is unique.
	mac := hmac.New(sha256.New, i.secret)
	mac.Write([]byte(tsStr + ":" + deviceID))
	expected := hex.EncodeToString(mac.Sum(nil))

	if !hmac.Equal([]byte(gotSig), []byte(expected)) {
		return "", errors.New("unauthorized")
	}
	return deviceID, nil
}

// Machine ID Validation for Device Authentication
//
// Agents must hash their machine ID before sending it to the server.
// This protects the actual hardware identifiers while still allowing
// the server to track and deduplicate devices.
//
// Hashing Algorithm:
//   1. Collect a stable hardware identifier:
//      - macOS: IOPlatformSerialNumber from IOKit
//      - Linux: /etc/machine-id
//      - Windows: MachineGuid from registry HKLM\SOFTWARE\Microsoft\Cryptography
//   2. Normalize: trim whitespace, lowercase
//   3. Hash: SHA-256(normalized_id + ":" + SharedSecret)
//   4. Encode: hex.EncodeToString(hash)
//
// Example (Go):
//   rawID := getMachineID() // platform-specific
//   mac := hmac.New(sha256.New, []byte(cipher.SharedSecret))
//   mac.Write([]byte(strings.ToLower(strings.TrimSpace(rawID))))
//   hashedID := hex.EncodeToString(mac.Sum(nil))

const (
	// MachineIDMinLength is the minimum valid length for a hashed machine ID
	MachineIDMinLength = 32
	// MachineIDMaxLength is the maximum allowed length for a machine ID
	MachineIDMaxLength = 128
)

var (
	// hexPattern matches valid hexadecimal strings
	hexPattern = regexp.MustCompile("^[a-f0-9]+$")
)

// ValidateMachineID validates a hashed machine ID from an agent.
// Returns error if the ID is malformed, too short, or suspicious.
// Returns generic errors to avoid leaking validation logic to attackers.
func ValidateMachineID(deviceID string) error {
	if deviceID == "" {
		return errors.New("invalid device ID")
	}
	
	// Check length
	if len(deviceID) < MachineIDMinLength || len(deviceID) > MachineIDMaxLength {
		return errors.New("invalid device ID")
	}
	
	// Must be valid hex (SHA-256 produces hex-encoded output)
	if !hexPattern.MatchString(deviceID) {
		return errors.New("invalid device ID")
	}
	
	// Additional entropy check: should not be all same character
	charSet := make(map[byte]struct{})
	for i := 0; i < len(deviceID); i++ {
		charSet[deviceID[i]] = struct{}{}
	}
	if len(charSet) < 8 {
		return errors.New("invalid device ID")
	}
	
	return nil
}

// HashMachineID hashes a raw machine ID using the shared secret.
// This is the reference implementation agents should use.
func HashMachineID(rawMachineID string) string {
	mac := hmac.New(sha256.New, []byte(SharedSecret))
	mac.Write([]byte(rawMachineID))
	return hex.EncodeToString(mac.Sum(nil))
}
