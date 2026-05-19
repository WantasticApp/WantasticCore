package ecsrp

import (
	"crypto/rand"
	"crypto/sha256"
	"fmt"
	"math/big"
	"net"
)

// Server represents an ECSRP-5 server that computes valid shared secrets
// Used for MITM authentication in the Winbox multiplexer
// By default password = username, but can be set explicitly for separate tokens
type Server struct {
	conn  net.Conn
	curve *WCurve

	// Server ephemeral keys
	sB        []byte // Server ephemeral private key (32 bytes)
	xWB       []byte // Server public key x-coordinate (32 bytes)
	xWBParity bool   // Server public key y-parity

	// Client public key (received)
	xWA          []byte // Client public key x-coordinate (32 bytes)
	xWAParity    bool   // Client public key y-parity
	username     string // Client's username (extracted from handshake)
	authUsername string // Username for ECSRP computation (if different from handshake username, e.g. RoMON mode)
	password     string // Password for ECSRP computation (defaults to username if not set)

	// Shared secret computation
	j        []byte // Hash of concatenated public keys (32 bytes)
	z        []byte // Shared secret x-coordinate (32 bytes)
	secret   []byte // Final shared secret SHA256(z) (32 bytes)
	i        []byte // Password validator private key (32 bytes)
	xGamma   []byte // x-coordinate of gamma = i*G (32 bytes)
	clientCC []byte // Client confirmation code (received, for server confirmation)

	// Salt (we generate random)
	salt []byte

	// Encryption state (for post-auth communication)
	sendAESKey  []byte
	sendHMACKey []byte
	recvAESKey  []byte
	recvHMACKey []byte

	// Receive buffer for handling multiple messages in one TCP read
	recvBuffer []byte
}

// NewServer creates a new ECSRP-5 server for accepting client connections
func NewServer(conn net.Conn) *Server {
	return &Server{
		conn:  conn,
		curve: NewWCurve(),
	}
}

// AcceptAuth performs server-side ECSRP-5 handshake, accepting ANY credentials
// Returns the username the client provided
func (s *Server) AcceptAuth() (string, error) {
	// Step 1: Receive client's public key + username
	if err := s.receiveClientHello(); err != nil {
		return "", fmt.Errorf("receive client hello: %w", err)
	}

	// Step 2: Generate our ephemeral keys and send response
	if err := s.sendServerHello(); err != nil {
		return "", fmt.Errorf("send server hello: %w", err)
	}

	// Step 3: Receive and accept client confirmation (we accept anything)
	if err := s.receiveClientConfirmation(); err != nil {
		return "", fmt.Errorf("receive client confirmation: %w", err)
	}

	// Step 4: Send server confirmation
	if err := s.sendServerConfirmation(); err != nil {
		return "", fmt.Errorf("send server confirmation: %w", err)
	}

	return s.username, nil
}

// AcceptAuthPhase1 performs the first phase of ECSRP-5 handshake (exchange keys)
// Returns the username. Call ConfirmAuth() to complete or RejectAuth() to reject.
// Note: This uses password = username. For separate password, use AcceptAuthWithPassword.
func (s *Server) AcceptAuthPhase1() (string, error) {
	// Step 1: Receive client's public key + username
	if err := s.receiveClientHello(); err != nil {
		return "", fmt.Errorf("receive client hello: %w", err)
	}

	// Step 2: Generate our ephemeral keys and send response
	if err := s.sendServerHello(); err != nil {
		return "", fmt.Errorf("send server hello: %w", err)
	}

	// Step 3: Receive client confirmation (we accept anything)
	if err := s.receiveClientConfirmation(); err != nil {
		return "", fmt.Errorf("receive client confirmation: %w", err)
	}

	// Don't send server confirmation yet - caller will call ConfirmAuth() or RejectAuth()
	return s.username, nil
}

// ReceiveClientHello receives the client hello and returns the username.
// This is the first step of a split handshake that allows password lookup.
// Call SetPassword() then ContinueAuthWithPassword() to complete.
func (s *Server) ReceiveClientHello() (string, error) {
	if err := s.receiveClientHello(); err != nil {
		return "", fmt.Errorf("receive client hello: %w", err)
	}
	return s.username, nil
}

// SetPassword sets the password to use for ECSRP computation.
// Must be called after ReceiveClientHello and before ContinueAuthWithPassword.
func (s *Server) SetPassword(password string) {
	s.password = password
}

// SetAuthUsername sets the username to use for ECSRP password validator computation.
// This is needed for RoMON mode where the client sends "username+r" in the handshake
// but the password hash uses just "username" without the +r suffix.
// Must be called after ReceiveClientHello and before ContinueAuthWithPassword.
func (s *Server) SetAuthUsername(username string) {
	s.authUsername = username
}

// ContinueAuthWithPassword continues the handshake after password is set.
// Returns the username. Call ConfirmAuth() to complete or RejectAuth() to reject.
func (s *Server) ContinueAuthWithPassword() (string, error) {
	// Step 2: Generate our ephemeral keys and send response (uses s.password)
	if err := s.sendServerHello(); err != nil {
		return "", fmt.Errorf("send server hello: %w", err)
	}

	// Step 3: Receive client confirmation
	if err := s.receiveClientConfirmation(); err != nil {
		return "", fmt.Errorf("receive client confirmation: %w", err)
	}

	// Don't send server confirmation yet - caller will call ConfirmAuth() or RejectAuth()
	return s.username, nil
}

// ConfirmAuth sends a valid server confirmation to complete the handshake
func (s *Server) ConfirmAuth() error {
	return s.sendServerConfirmation()
}

// receiveClientHello reads the client's initial handshake packet
// Format: length(1) + handler(1:0x06) + username + \x00 + pubkey(32) + parity(1)
func (s *Server) receiveClientHello() error {
	// Read packet
	buf := make([]byte, 256)
	n, err := s.conn.Read(buf)
	if err != nil {
		return fmt.Errorf("read client hello: %w", err)
	}

	if n < 36 { // Minimum: 1 + 1 + 1 + 1 + 32 + 1 = 37
		return fmt.Errorf("client hello too short: %d bytes", n)
	}

	packet := buf[:n]
	// 	fmt.Printf("[ECSRP Server] Received client hello (%d bytes): %x\n", n, packet)

	// Verify handler
	if packet[1] != 0x06 {
		return fmt.Errorf("invalid handler: 0x%02x (expected 0x06)", packet[1])
	}

	// Extract username (null-terminated after handler)
	nullPos := -1
	for i := 2; i < len(packet); i++ {
		if packet[i] == 0x00 {
			nullPos = i
			break
		}
	}
	if nullPos == -1 {
		return fmt.Errorf("no null terminator found for username")
	}

	s.username = string(packet[2:nullPos])

	// Extract public key (32 bytes after null)
	pubkeyStart := nullPos + 1
	if pubkeyStart+33 > len(packet) {
		return fmt.Errorf("packet too short for public key")
	}

	s.xWA = make([]byte, 32)
	copy(s.xWA, packet[pubkeyStart:pubkeyStart+32])
	s.xWAParity = packet[pubkeyStart+32] == 0x01

	// 	fmt.Printf("[ECSRP Server] Received client hello: username=%s, pubkey=%x\n", s.username, s.xWA[:8])

	return nil
}

// sendServerHello generates server keys and sends response to client
// Implements ECPEPKGP-SRP-B: password-entangled public key generation
// Format: length(1) + handler(1:0x06) + pubkey(32) + parity(1) + salt(16)
func (s *Server) sendServerHello() error {
	// Generate random salt (16 bytes)
	s.salt = make([]byte, 16)
	if _, err := rand.Read(s.salt); err != nil {
		return fmt.Errorf("generate salt: %w", err)
	}

	// Generate ephemeral private key (32 bytes)
	s.sB = make([]byte, 32)
	if _, err := rand.Read(s.sB); err != nil {
		return fmt.Errorf("generate private key: %w", err)
	}

	// Compute base public key: s_b * G
	sBInt := new(big.Int).SetBytes(s.sB)
	basePoint := s.curve.MultiplyByG(sBInt)

	// For password-entangled public key, we need:
	// 1. Compute password validator: i = SHA256(salt || SHA256(username:password))
	//    Use explicit password if set, otherwise default to username (for backward compatibility)
	//    Use authUsername if set (for RoMON mode where username has +r suffix but password uses plain username)
	password := s.password
	if password == "" {
		password = s.username // Backward compatibility: password = username
	}
	authUser := s.authUsername
	if authUser == "" {
		authUser = s.username // Default: use handshake username
	}
	s.i = s.curve.GenPasswordValidatorPriv(authUser, password, s.salt)
	// 	fmt.Printf("[ECSRP Server] Password validator i: %x\n", s.i)

	// 2. Compute gamma = i * G (password validator public key)
	iInt := new(big.Int).SetBytes(s.i)
	gamma := s.curve.MultiplyByG(iInt)
	xGamma, _, err := s.curve.ToMontgomery(gamma)
	if err != nil {
		return fmt.Errorf("compute gamma: %w", err)
	}
	s.xGamma = xGamma
	// 	fmt.Printf("[ECSRP Server] x_gamma (password verifier x): %x\n", xGamma)

	// 3. Compute e = redp1(x_gamma, false) - pseudo-random point with positive parity
	//    The client will compute v = redp1(x_gamma, true) with inverted parity
	//    So client's (W_b + v) = (W_b + e') where e' = -e, giving W_b - e
	//    For this to work, server's W_b must be (base + e)
	e := s.curve.Redp1(xGamma, false)
	if e == nil {
		return fmt.Errorf("failed to compute e point")
	}
	// 	fmt.Printf("[ECSRP Server] Computed e point for entanglement\n")

	// 4. Compute password-entangled public key: W_b = s_b*G + e
	entangledPoint := s.curve.Add(basePoint, e)
	if entangledPoint == nil {
		return fmt.Errorf("failed to add e to base point")
	}

	// Convert to Montgomery x-coordinate
	s.xWB, s.xWBParity, err = s.curve.ToMontgomery(entangledPoint)
	if err != nil {
		return fmt.Errorf("convert entangled point: %w", err)
	}
	// 	fmt.Printf("[ECSRP Server] Entangled public key W_b: %x (parity=%v)\n", s.xWB, s.xWBParity)

	// Compute j = SHA256(xWA || xWB)
	jHash := sha256.New()
	jHash.Write(s.xWA)
	jHash.Write(s.xWB)
	s.j = jHash.Sum(nil)

	// Build response packet
	// Client expects: length + handler(0x06) + pubkey(32) + parity(1) + salt(16)
	response := make([]byte, 0, 51)
	response = append(response, 0x00)     // length placeholder
	response = append(response, 0x06)     // handler
	response = append(response, s.xWB...) // public key (32 bytes)
	if s.xWBParity {
		response = append(response, 0x01)
	} else {
		response = append(response, 0x00)
	}
	response = append(response, s.salt...) // salt (16 bytes)
	response[0] = byte(len(response) - 2)  // length excludes length+handler bytes

	if _, err := s.conn.Write(response); err != nil {
		return fmt.Errorf("write server hello: %w", err)
	}

	// 	fmt.Printf("[ECSRP Server] Sent server hello (%d bytes): %x\n", len(response), response)
	// 	fmt.Printf("[ECSRP Server]   pubkey=%x, parity=%v, salt=%x\n", s.xWB, s.xWBParity, s.salt)

	return nil
}

// receiveClientConfirmation receives client's confirmation and computes shared secret
// Implements ECPESVDP-SRP-B: z = Ts * (Wc + h*v)
// where Ts = s_b, Wc = W_a, h = j, v = gamma = i*G
func (s *Server) receiveClientConfirmation() error {
	// Read confirmation packet
	buf := make([]byte, 64)
	n, err := s.conn.Read(buf)
	if err != nil {
		return fmt.Errorf("read client confirmation: %w", err)
	}

	if n < 34 { // 1 + 1 + 32
		return fmt.Errorf("client confirmation too short: %d bytes", n)
	}

	// Extract client's confirmation code
	s.clientCC = make([]byte, 32)
	copy(s.clientCC, buf[2:34])
	// 	fmt.Printf("[ECSRP Server] Received client confirmation (%d bytes): %x\n", n, s.clientCC[:8])

	// Note: Password validator i and gamma were already computed in sendServerHello
	// 	fmt.Printf("[ECSRP Server] Using pre-computed i: %x\n", s.i)
	// 	fmt.Printf("[ECSRP Server] Using pre-computed x_gamma: %x\n", s.xGamma)
	// 	fmt.Printf("[ECSRP Server] j: %x\n", s.j)

	// Step 1: Compute gamma = i * G (already have xGamma, need the full point)
	iInt := new(big.Int).SetBytes(s.i)
	gamma := s.curve.MultiplyByG(iInt)
	// 	fmt.Printf("[ECSRP Server] Recomputed gamma point\n")

	// Step 2: Compute j * gamma (scalar multiply gamma by j)
	jInt := new(big.Int).SetBytes(s.j)
	jGamma := s.curve.Multiply(gamma, jInt)
	if jGamma == nil {
		return fmt.Errorf("failed to compute j * gamma")
	}
	// 	fmt.Printf("[ECSRP Server] Computed j * gamma\n")

	// Step 3: Lift client public key: W_a = lift_x(x_w_a, x_w_a_parity)
	// 	fmt.Printf("[ECSRP Server] Client public key x_w_a: %x (parity=%v)\n", s.xWA, s.xWAParity)
	xWAInt := new(big.Int).SetBytes(s.xWA)
	wA := s.curve.LiftX(xWAInt, s.xWAParity)
	if wA == nil {
		return fmt.Errorf("failed to lift client public key")
	}

	// Step 4: Compute W_a + j*gamma
	wAPlusJGamma := s.curve.Add(wA, jGamma)
	if wAPlusJGamma == nil {
		return fmt.Errorf("failed to compute W_a + j*gamma")
	}
	// 	fmt.Printf("[ECSRP Server] Computed W_a + j*gamma\n")

	// Step 5: Compute z = s_b * (W_a + j*gamma)
	// This is the server-side ECPESVDP-SRP-B formula
	sBInt := new(big.Int).SetBytes(s.sB)
	ptZ := s.curve.Multiply(wAPlusJGamma, sBInt)
	if ptZ == nil {
		return fmt.Errorf("failed to compute Z point")
	}

	// Extract x-coordinate
	s.z, _, err = s.curve.ToMontgomery(ptZ)
	if err != nil {
		return fmt.Errorf("failed to extract z: %w", err)
	}
	// 	fmt.Printf("[ECSRP Server] Computed z: %x\n", s.z)

	// Compute shared secret: secret = SHA256(z)
	zHash := sha256.Sum256(s.z)
	s.secret = zHash[:]
	// 	fmt.Printf("[ECSRP Server] Final secret (SHA256 of z): %x\n", s.secret)

	// Verify client confirmation: Cc = SHA256(j || z)
	expectedCC := GetSHA256Digest(append(s.j, s.z...))
	// 	fmt.Printf("[ECSRP Server] Expected Cc: %x\n", expectedCC)
	// 	fmt.Printf("[ECSRP Server] Received Cc: %x\n", s.clientCC)

	// Check if confirmation matches
	if string(expectedCC) != string(s.clientCC) {
		// 		fmt.Printf("[ECSRP Server] WARNING: Client confirmation does not match! Client may have used different password.\n")
	} else {
		// 		fmt.Printf("[ECSRP Server] SUCCESS: Client confirmation matches!\n")
	}

	// Derive encryption keys
	s.deriveEncryptionKeys()

	return nil
}

// sendServerConfirmation sends our confirmation to the client
func (s *Server) sendServerConfirmation() error {
	// Compute server confirmation: Sc = SHA256(j || Cc || z)
	// This matches what the client expects to verify
	scData := append(s.j, s.clientCC...)
	scData = append(scData, s.z...)
	serverCC := GetSHA256Digest(scData)

	// 	fmt.Printf("[ECSRP Server] Server confirmation Sc = SHA256(j || Cc || z)\n")
	// 	fmt.Printf("[ECSRP Server]   j:  %x\n", s.j)
	// 	fmt.Printf("[ECSRP Server]   Cc: %x\n", s.clientCC)
	// 	fmt.Printf("[ECSRP Server]   z:  %x\n", s.z)
	// 	fmt.Printf("[ECSRP Server]   Sc: %x\n", serverCC)

	// Build confirmation packet
	// Length excludes length+handler bytes = 32 (just the confirmation code)
	response := make([]byte, 0, 34)
	response = append(response, byte(32)) // length (excludes length+handler)
	response = append(response, 0x06)     // handler
	response = append(response, serverCC...)

	if _, err := s.conn.Write(response); err != nil {
		return fmt.Errorf("write server confirmation: %w", err)
	}

	// 	fmt.Printf("[ECSRP Server] Sent server confirmation\n")

	return nil
}

// deriveEncryptionKeys derives AES and HMAC keys from the shared secret
func (s *Server) deriveEncryptionKeys() {
	// Use the same GenStreamKeys as client, but with isServer=true
	// This uses the RouterOS magic strings for proper key derivation
	s.sendAESKey, s.recvAESKey, s.sendHMACKey, s.recvHMACKey = GenStreamKeys(true, s.secret)
}

// GetConn returns the underlying connection for proxying
func (s *Server) GetConn() net.Conn {
	return s.conn
}

// Receive receives and decrypts a message from the client
// Handles multiple concatenated messages by buffering
// Also handles fragmented messages that span multiple TCP reads
func (s *Server) Receive() ([]byte, error) {
	for {
		// Check if we have buffered data from previous read
		if len(s.recvBuffer) == 0 {
			// Read more data from connection
			buf := make([]byte, 4096)
			// 			fmt.Printf("[ECSRP Server] Waiting for client message...\n")
			n, err := s.conn.Read(buf)
			if err != nil {
				// 				fmt.Printf("[ECSRP Server] Read error: %v\n", err)
				return nil, fmt.Errorf("receive failed: %w", err)
			}
			s.recvBuffer = buf[:n]
			// 			fmt.Printf("[ECSRP Server] Received %d bytes from client: %x\n", n, s.recvBuffer[:min(32, len(s.recvBuffer))])
		} else {
			// 			fmt.Printf("[ECSRP Server] Processing buffered data: %d bytes\n", len(s.recvBuffer))
		}

		// Reassemble fragmented message (only first message if multiple concatenated)
		assembled, consumed, err := ReassembleMessage(s.recvBuffer)
		if err == ErrNeedMoreData {
			// Fragmented message is incomplete, read more data
			// 			fmt.Printf("[ECSRP Server] Need more data, reading from connection...\n")
			buf := make([]byte, 4096)
			n, readErr := s.conn.Read(buf)
			if readErr != nil {
				// 				fmt.Printf("[ECSRP Server] Read error while waiting for more data: %v\n", readErr)
				s.recvBuffer = nil
				return nil, fmt.Errorf("receive failed: %w", readErr)
			}
			// Append new data to existing buffer
			s.recvBuffer = append(s.recvBuffer, buf[:n]...)
			// 			fmt.Printf("[ECSRP Server] Buffer now has %d bytes\n", len(s.recvBuffer))
			continue // Try reassembly again
		}
		if err != nil {
			// 			fmt.Printf("[ECSRP Server] Reassemble error: %v\n", err)
			s.recvBuffer = nil // Clear buffer on error
			return nil, fmt.Errorf("reassemble failed: %w", err)
		}
		// 		fmt.Printf("[ECSRP Server] Assembled message: %d bytes (consumed %d of %d)\n", len(assembled), consumed, len(s.recvBuffer))

		// Update buffer with remaining data
		if consumed < len(s.recvBuffer) {
			s.recvBuffer = s.recvBuffer[consumed:]
			// 			fmt.Printf("[ECSRP Server] Buffered %d bytes for next message\n", len(s.recvBuffer))
		} else {
			s.recvBuffer = nil
		}

		// Decrypt message using client->server keys
		// 		fmt.Printf("[ECSRP Server] Decrypting with recvAES=%x..., recvHMAC=%x...\n",
		// s.recvAESKey[:8], s.recvHMACKey[:8])
		decrypted, err := DecryptMessage(assembled, s.recvAESKey, s.recvHMACKey)
		if err != nil {
			// 			fmt.Printf("[ECSRP Server] Decryption error: %v\n", err)
			return nil, fmt.Errorf("decryption failed: %w", err)
		}
		// 		fmt.Printf("[ECSRP Server] Decrypted message: %d bytes\n", len(decrypted))

		return decrypted, nil
	}
}

// Send encrypts and sends a message to the client
func (s *Server) Send(msg []byte) error {
	// 	fmt.Printf("[ECSRP Server] Sending %d bytes to client\n", len(msg))

	// Encrypt message using server->client keys
	encrypted, err := EncryptMessage(msg, s.sendAESKey, s.sendHMACKey)
	if err != nil {
		// 		fmt.Printf("[ECSRP Server] Encryption error: %v\n", err)
		return fmt.Errorf("encryption failed: %w", err)
	}
	// 	fmt.Printf("[ECSRP Server] Encrypted to %d bytes\n", len(encrypted))

	// Fragment message
	fragmented := FragmentMessage(encrypted)
	// 	fmt.Printf("[ECSRP Server] Fragmented to %d bytes\n", len(fragmented))

	// Send
	if _, err := s.conn.Write(fragmented); err != nil {
		// 		fmt.Printf("[ECSRP Server] Write error: %v\n", err)
		return fmt.Errorf("send failed: %w", err)
	}
	// 	fmt.Printf("[ECSRP Server] Sent successfully\n")

	return nil
}

// GetEncryptionKeys returns the encryption keys for external use
func (s *Server) GetEncryptionKeys() (sendAES, sendHMAC, recvAES, recvHMAC []byte) {
	return s.sendAESKey, s.sendHMACKey, s.recvAESKey, s.recvHMACKey
}

// RejectAuth sends an invalid confirmation to make client show "wrong password"
// This should be called after AcceptAuth() when we want to reject the session
// (e.g., access token not found, IP not allowed, etc.)
func (s *Server) RejectAuth() error {
	// Send an invalid confirmation code (all zeros)
	// The client will verify this against expected value and fail
	invalidCC := make([]byte, 32)

	response := make([]byte, 0, 34)
	response = append(response, byte(32)) // length
	response = append(response, 0x06)     // handler
	response = append(response, invalidCC...)

	if _, err := s.conn.Write(response); err != nil {
		return fmt.Errorf("write reject confirmation: %w", err)
	}

	return nil
}
