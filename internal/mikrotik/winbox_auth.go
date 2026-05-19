package mikrotik

import (
	"context"
	"fmt"
	"net"
	"time"

	"WantasticCore/internal/mikrotik/ecsrp"

	"github.com/rs/zerolog/log"
)

// AuthMethod represents the authentication method used
type AuthMethod int

const (
	AuthMethodUnknown AuthMethod = iota
	AuthMethodECSRP5             // RouterOS 7+ (6.43+)
	AuthMethodLegacy             // RouterOS 6 (pre-6.43)
)

func (a AuthMethod) String() string {
	switch a {
	case AuthMethodECSRP5:
		return "ECSRP-5"
	case AuthMethodLegacy:
		return "Legacy"
	default:
		return "Unknown"
	}
}

// WinboxAuthenticator handles authentication to MikroTik devices
// with automatic version detection and fallback support
type WinboxAuthenticator struct {
	host     string
	port     int
	username string
	password string

	// Detected auth method after first successful connection
	detectedMethod AuthMethod

	// Connection pool (for future enhancement)
	ecsrpClient *ecsrp.Client

	// External connection (for tenant netstack)
	externalConn net.Conn
}

// NewWinboxAuthenticator creates a new authenticator for testing only
func NewWinboxAuthenticator(host string, port int, username, password string) *WinboxAuthenticator {
	return &WinboxAuthenticator{
		host:           host,
		port:           port,
		username:       username,
		password:       password,
		detectedMethod: AuthMethodUnknown,
	}
}

// NewWinboxAuthenticatorWithConn creates an authenticator using an existing connection
// Use this when connecting through tenant netstack (Wantastic.app)
func NewWinboxAuthenticatorWithConn(conn net.Conn, username, password string) *WinboxAuthenticator {
	return &WinboxAuthenticator{
		username:       username,
		password:       password,
		detectedMethod: AuthMethodUnknown,
		externalConn:   conn,
	}
}

// Authenticate attempts to authenticate using detected or auto-detected method
func (w *WinboxAuthenticator) Authenticate(ctx context.Context) (net.Conn, AuthMethod, error) {
	// If method is already detected, use it directly
	if w.detectedMethod != AuthMethodUnknown {
		return w.authenticateWithMethod(ctx, w.detectedMethod)
	}

	// Try ECSRP-5 first (modern RouterOS 7+)
	log.Debug().
		Str("host", w.host).
		Str("method", "ECSRP-5").
		Msg("Attempting ECSRP-5 authentication (RouterOS 7+)")

	conn, err := w.authenticateECSRP5(ctx)
	if err == nil {
		w.detectedMethod = AuthMethodECSRP5
		log.Debug().
			Str("host", w.host).
			Str("method", "ECSRP-5").
			Msg(" Detected RouterOS 7+ with ECSRP-5 support")
		return conn, AuthMethodECSRP5, nil
	}

	log.Debug().
		Err(err).
		Str("host", w.host).
		Msg("ECSRP-5 failed, trying legacy authentication")

	// Fallback to legacy authentication (RouterOS 6)
	// NOTE: If using external conn, we can't retry with a new connection
	if w.externalConn != nil {
		return nil, AuthMethodUnknown, fmt.Errorf("ECSRP-5 failed and cannot retry legacy with same connection: %w", err)
	}

	conn, err = w.authenticateLegacy(ctx)
	if err == nil {
		w.detectedMethod = AuthMethodLegacy
		log.Debug().
			Str("host", w.host).
			Str("method", "Legacy").
			Msg(" Detected RouterOS 6 with legacy authentication")
		return conn, AuthMethodLegacy, nil
	}

	return nil, AuthMethodUnknown, fmt.Errorf("all authentication methods failed: %w", err)
}

// authenticateWithMethod authenticates using a specific method
func (w *WinboxAuthenticator) authenticateWithMethod(ctx context.Context, method AuthMethod) (net.Conn, AuthMethod, error) {
	switch method {
	case AuthMethodECSRP5:
		conn, err := w.authenticateECSRP5(ctx)
		return conn, AuthMethodECSRP5, err
	case AuthMethodLegacy:
		conn, err := w.authenticateLegacy(ctx)
		return conn, AuthMethodLegacy, err
	default:
		return nil, AuthMethodUnknown, fmt.Errorf("unknown authentication method: %v", method)
	}
}

// authenticateECSRP5 performs ECSRP-5 authentication (RouterOS 7+)
func (w *WinboxAuthenticator) authenticateECSRP5(ctx context.Context) (net.Conn, error) {
	var client *ecsrp.Client

	// Use external connection if provided (tenant netstack)
	if w.externalConn != nil {
		client = ecsrp.NewClientWithConn(w.externalConn)
	} else {
		client = ecsrp.NewClient(w.host, w.port)
	}

	// Set context deadline if available
	deadline, hasDeadline := ctx.Deadline()
	if hasDeadline {
		timeout := time.Until(deadline)
		if timeout > 0 {
			// Set a reasonable minimum timeout
			if timeout < 5*time.Second {
				timeout = 5 * time.Second
			}
		}
	}

	// Authenticate
	err := client.Connect(w.username, w.password)
	if err != nil {
		return nil, fmt.Errorf("ECSRP-5 authentication failed: %w", err)
	}

	// Store client for keep-alive management
	w.ecsrpClient = client

	// Return the underlying connection for proxying
	return client.GetConnection(), nil
}

// authenticateLegacy performs legacy authentication (RouterOS 6)
func (w *WinboxAuthenticator) authenticateLegacy(ctx context.Context) (net.Conn, error) {
	var conn net.Conn
	var err error

	// Use external connection if provided (tenant netstack)
	if w.externalConn != nil {
		conn = w.externalConn
	} else {
		// Get deadline from context
		deadline, hasDeadline := ctx.Deadline()
		timeout := 10 * time.Second
		if hasDeadline {
			timeout = time.Until(deadline)
			if timeout < 0 {
				return nil, fmt.Errorf("context deadline exceeded")
			}
		}

		// Connect to Winbox port
		addr := net.JoinHostPort(w.host, fmt.Sprintf("%d", w.port))
		conn, err = net.DialTimeout("tcp", addr, timeout)
		if err != nil {
			return nil, fmt.Errorf("connection failed: %w", err)
		}
	}

	// Perform legacy authentication handshake
	// RouterOS 6 uses plaintext or MD5-based authentication
	err = w.performLegacyHandshake(conn)
	if err != nil {
		// Only close if we created the connection
		if w.externalConn == nil {
			conn.Close()
		}
		return nil, fmt.Errorf("legacy authentication failed: %w", err)
	}

	return conn, nil
}

// performLegacyHandshake performs RouterOS 6 legacy authentication
func (w *WinboxAuthenticator) performLegacyHandshake(conn net.Conn) error {
	// Set read/write timeout
	conn.SetDeadline(time.Now().Add(10 * time.Second))
	defer conn.SetDeadline(time.Time{})

	// Legacy Winbox protocol (RouterOS 6.x):
	// 1. Send username and password in M2 protocol format
	// 2. Receive authentication response
	// 3. If successful, connection is authenticated

	// Build legacy login packet
	// Format: M2 + type + username + password fields
	loginPacket := buildLegacyLoginPacket(w.username, w.password)

	// Send login packet
	_, err := conn.Write(loginPacket)
	if err != nil {
		return fmt.Errorf("failed to send login packet: %w", err)
	}

	// Read response
	response := make([]byte, 1024)
	n, err := conn.Read(response)
	if err != nil {
		return fmt.Errorf("failed to read response: %w", err)
	}

	// Check response for success/failure
	if !isLegacyAuthSuccess(response[:n]) {
		return fmt.Errorf("authentication rejected by server")
	}

	log.Debug().
		Str("host", w.host).
		Msg("Legacy authentication successful")

	return nil
}

// buildLegacyLoginPacket constructs a RouterOS 6 login packet
func buildLegacyLoginPacket(username, password string) []byte {
	// M2 protocol format (RouterOS 6):
	// Header: 'M' '2'
	// Type field: 0x01 (login)
	// Username field: type 0x01 + length + username
	// Password field: type 0x09 + length + password

	packet := make([]byte, 0, 256)

	// M2 header
	packet = append(packet, 'M', '2')

	// Message type: 0x01 (login request)
	packet = append(packet, 0x01, 0x00, 0xff, 0x01)

	// Username field
	packet = appendM2String(packet, 0x01, username)

	// Password field
	packet = appendM2String(packet, 0x09, password)

	// Finalize packet
	packet = append(packet, 0x00)

	return packet
}

// appendM2String appends a string field to M2 packet
func appendM2String(packet []byte, fieldType byte, value string) []byte {
	valueBytes := []byte(value)
	length := len(valueBytes)

	// Field type
	packet = append(packet, fieldType, 0x00)

	// Length encoding (0xff for multi-byte)
	if length < 0x80 {
		packet = append(packet, byte(length))
	} else {
		packet = append(packet, 0xff, byte(length&0xff), byte(length>>8))
	}

	// Value
	packet = append(packet, valueBytes...)

	return packet
}

// isLegacyAuthSuccess checks if the response indicates successful authentication
func isLegacyAuthSuccess(response []byte) bool {
	if len(response) < 4 {
		return false
	}

	// Check M2 header
	if response[0] != 'M' || response[1] != '2' {
		return false
	}

	// Check for error indicator (0x21 = error, 0x01 = success/data)
	// RouterOS 6 sends 0x01 for successful login response
	if len(response) >= 4 && response[2] == 0x21 {
		return false // Error response
	}

	// If we get here, assume success (response is valid M2 packet)
	return true
}

// Close closes the authenticator and any open connections
func (w *WinboxAuthenticator) Close() error {
	if w.ecsrpClient != nil {
		return w.ecsrpClient.Close()
	}
	return nil
}

// GetDetectedMethod returns the detected authentication method
func (w *WinboxAuthenticator) GetDetectedMethod() AuthMethod {
	return w.detectedMethod
}

// SupportsKeepAlive returns true if the current method supports keep-alive
func (w *WinboxAuthenticator) SupportsKeepAlive() bool {
	return w.detectedMethod == AuthMethodECSRP5
}
