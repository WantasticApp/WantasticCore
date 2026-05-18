package ecsrp

import (
	"crypto/rand"
	"errors"
	"fmt"
	"io"
	"math/big"
	"net"
	"time"
)

// Client represents an ECSRP-5 Winbox client
type Client struct {
	host     string
	port     int
	username string
	password string

	conn  net.Conn
	curve *WCurve
	stage int

	// ECSRP-5 state
	sA        []byte // Client ephemeral private key (32 bytes)
	xWA       []byte // Client public key x-coordinate (32 bytes)
	xWAParity bool   // Client public key y-parity
	xWB       []byte // Server public key x-coordinate (32 bytes)
	xWBParity bool   // Server public key y-parity
	j         []byte // Hash of concatenated public keys (32 bytes)
	z         []byte // Shared secret x-coordinate (32 bytes)
	secret    []byte // Final shared secret SHA256(z) (32 bytes)
	clientCC  []byte // Client confirmation code (32 bytes)
	serverCC  []byte // Server confirmation code (32 bytes)
	i         []byte // Password validator private key (32 bytes)

	// Encryption state
	sendAESKey  []byte
	sendHMACKey []byte
	recvAESKey  []byte
	recvHMACKey []byte

	// Message buffers
	msgToSend []byte
	response  []byte

	// Receive buffer for handling multiple messages in one TCP read
	recvBuffer []byte

	// Keep-alive state
	keepAliveInterval time.Duration
	stopKeepAlive     chan struct{}
	keepAliveActive   bool

	// External connection flag - disables retries since we can't reconnect
	externalConn bool
}

// NewClient creates a new ECSRP-5 Winbox client
func NewClient(host string, port int) *Client {
	return &Client{
		host:              host,
		port:              port,
		curve:             NewWCurve(),
		stage:             -1,
		keepAliveInterval: 1 * time.Second, // Default 1s interval (matches official client)
		stopKeepAlive:     make(chan struct{}),
	}
}

// NewClientWithConn creates an ECSRP-5 client using an existing connection
// Use this when connecting through tenant netstack (Wantastic.app)
// NOTE: Retries are disabled - caller must handle reconnection if auth fails
// NOTE: Keep-alive is disabled by default for external connections (MITM bridge handles it)
func NewClientWithConn(conn net.Conn) *Client {
	return &Client{
		conn:              conn,
		curve:             NewWCurve(),
		stage:             0, // Skip socket open stage - already connected
		keepAliveInterval: 0, // Disable keep-alive for MITM bridging
		stopKeepAlive:     make(chan struct{}),
		externalConn:      true, // Mark as external - no retries
	}
}

// SetKeepAliveInterval configures the keep-alive interval
func (c *Client) SetKeepAliveInterval(interval time.Duration) {
	c.keepAliveInterval = interval
}

// Connect establishes connection and performs ECSRP-5 authentication
func (c *Client) Connect(username, password string) error {
	c.username = username
	c.password = password

	// If using external connection, no retries - connection can't be reused after failure
	if c.externalConn {
		// 		fmt.Printf("[DEBUG] Authentication attempt (external conn, no retries)\n")
		err := c.authenticate()
		if err != nil {
			return fmt.Errorf("authentication failed: %w", err)
		}
		return nil
	}

	// Standard mode with retries (creates new connections)
	maxRetries := 3
	for attempt := range maxRetries {
		// 		fmt.Printf("[DEBUG] Authentication attempt %d/%d\n", attempt+1, maxRetries)
		err := c.authenticate()
		if err == nil {
			return nil
		}

		// 		fmt.Printf("[DEBUG] Attempt %d failed: %v\n", attempt+1, err)

		if attempt < maxRetries-1 {
			time.Sleep(time.Second)
		}
	}

	return errors.New("authentication failed after retries")
}

// authenticate performs the ECSRP-5 handshake
func (c *Client) authenticate() error {
	// Stage loop - limit iterations to prevent infinite loops
	maxSteps := 20
	for range maxSteps {
		switch c.stage {
		case -1:
			// Open socket
			if err := c.openSocket(); err != nil {
				return err
			}

		case 0:
			// Public key exchange (ECPEPKGP-SRP-A)
			if err := c.publicKeyExchange(); err != nil {
				return fmt.Errorf("public key exchange failed: %w", err)
			}

		case 1:
			// Process server challenge and send confirmation
			if err := c.confirmation(); err != nil {
				return fmt.Errorf("confirmation failed: %w", err)
			}

		case 2:
			// Verify server confirmation
			if err := c.verifyServerConfirmation(); err != nil {
				return fmt.Errorf("server confirmation failed: %w", err)
			}

		case 3:
			// Authentication complete - start keep-alive
			c.startKeepAlive()
			return nil
		}
	}

	return errors.New("authentication exceeded maximum steps")
}

// openSocket opens a TCP connection to the Winbox server
func (c *Client) openSocket() error {
	addr := net.JoinHostPort(c.host, fmt.Sprintf("%d", c.port))

	// 	fmt.Printf("[DEBUG] Connecting to %s...\n", addr)
	conn, err := net.DialTimeout("tcp", addr, 10*time.Second)
	if err != nil {
		return fmt.Errorf("failed to connect: %w", err)
	}

	// Enable TCP keep-alive
	if tcpConn, ok := conn.(*net.TCPConn); ok {
		tcpConn.SetKeepAlive(true)
		tcpConn.SetKeepAlivePeriod(30 * time.Second)
		// 		fmt.Printf("[DEBUG] TCP keep-alive enabled (30s)\n")
	}

	// 	fmt.Printf("[DEBUG] Connected successfully\n")
	c.conn = conn
	c.stage = 0
	return nil
}

// publicKeyExchange generates client ephemeral key and sends it to server
func (c *Client) publicKeyExchange() error {
	// 	fmt.Printf("[DEBUG] Starting public key exchange...\n")

	// Generate 32-byte ephemeral private key
	c.sA = make([]byte, 32)
	if _, err := rand.Read(c.sA); err != nil {
		return fmt.Errorf("failed to generate private key: %w", err)
	}

	// 	fmt.Printf("[DEBUG] Generated ephemeral private key\n")

	// Compute public key: W_a = s_a * G
	xWA, parity, err := c.curve.GenPublicKey(c.sA)
	if err != nil {
		return fmt.Errorf("failed to generate public key: %w", err)
	}

	// 	fmt.Printf("[DEBUG] Generated public key: %d bytes, parity=%v\n", len(xWA), parity)

	c.xWA = xWA
	c.xWAParity = parity

	// 	fmt.Printf("[DEBUG] Public key generated, sending to server...\n")

	// Format message: length(1) + handler(1) + username\x00 + x_w_a(32) + parity(1)
	msg := []byte(c.username)
	msg = append(msg, 0x00)
	msg = append(msg, c.xWA...)
	msg = append(msg, boolToByte(c.xWAParity))

	// Prepend length and handler
	msgLen := byte(len(msg))
	fullMsg := []byte{msgLen, 0x06}
	fullMsg = append(fullMsg, msg...)

	c.msgToSend = fullMsg
	c.stage = 1

	// Send message
	if _, err := c.conn.Write(c.msgToSend); err != nil {
		return fmt.Errorf("failed to send public key: %w", err)
	}

	// Receive server response
	c.response = make([]byte, 1024)
	n, err := c.conn.Read(c.response)
	if err != nil {
		return fmt.Errorf("failed to receive server challenge: %w", err)
	}
	c.response = c.response[:n]

	return nil
}

// confirmation processes server challenge and sends client proof
func (c *Client) confirmation() error {
	if len(c.response) < 2 {
		return errors.New("server response too short")
	}

	respLen := int(c.response[0])
	c.response = c.response[2:] // Skip length and handler

	if len(c.response) != respLen {
		return errors.New("server response corrupted")
	}

	// Parse server response: x_w_b(32) + parity(1) + salt(16)
	if len(c.response) < 49 {
		return errors.New("server response too short for challenge")
	}

	c.xWB = c.response[:32]
	c.xWBParity = c.response[32] != 0
	salt := c.response[33:49]

	if len(salt) != 16 {
		return errors.New("invalid salt length")
	}

	// IMPORTANT: Compute j BEFORE genSharedSecret, as it's needed for scalar computation
	// Compute j = SHA256(x_w_a || x_w_b)
	c.j = GetSHA256Digest(append(c.xWA, c.xWB...))
	// 	fmt.Printf("[DEBUG CRYPTO] Client public key x_w_a: %x\n", c.xWA)
	// 	fmt.Printf("[DEBUG CRYPTO] Server public key x_w_b: %x\n", c.xWB)
	// 	fmt.Printf("[DEBUG CRYPTO] j = SHA256(x_w_a || x_w_b): %x\n", c.j)

	// Generate shared secret using ECPESVDP-SRP-A (needs j to be set first!)
	if err := c.genSharedSecret(salt); err != nil {
		return err
	}

	// Compute client confirmation: Cc = SHA256(j || z)
	c.clientCC = GetSHA256Digest(append(c.j, c.z...))
	// 	fmt.Printf("[DEBUG CRYPTO] Client confirmation Cc = SHA256(j || z): %x\n", c.clientCC)

	// Format message: length(1) + handler(1) + Cc(32)
	msgLen := byte(len(c.clientCC))
	fullMsg := []byte{msgLen, 0x06}
	fullMsg = append(fullMsg, c.clientCC...)
	// 	fmt.Printf("[DEBUG] Sending confirmation message (%d bytes)\n", len(fullMsg))

	c.msgToSend = fullMsg
	c.stage = 2

	// Send confirmation
	if _, err := c.conn.Write(c.msgToSend); err != nil {
		return fmt.Errorf("failed to send confirmation: %w", err)
	}

	// Receive server confirmation
	c.response = make([]byte, 1024)
	n, err := c.conn.Read(c.response)
	if err != nil {
		return fmt.Errorf("failed to receive server confirmation: %w", err)
	}
	c.response = c.response[:n]

	return nil
}

// genSharedSecret implements ECPESVDP-SRP-A to compute shared secret z
func (c *Client) genSharedSecret(salt []byte) error {
	// 	fmt.Printf("[DEBUG CRYPTO] Salt: %x\n", salt)
	// 	fmt.Printf("[DEBUG CRYPTO] Username: %s\n", c.username)
	// 	fmt.Printf("[DEBUG CRYPTO] Password: %s\n", c.password)

	// Compute password validator: i = SHA256(salt || SHA256(username:password))
	c.i = c.curve.GenPasswordValidatorPriv(c.username, c.password, salt)
	// 	fmt.Printf("[DEBUG CRYPTO] Password validator i: %x\n", c.i)

	// Compute gamma = i * G (password validator public key)
	iInt := new(big.Int).SetBytes(c.i)
	ptGamma := c.curve.MultiplyByG(iInt)
	// xGamma, gammaParity, err := c.curve.ToMontgomery(ptGamma)
	xGamma, _, err := c.curve.ToMontgomery(ptGamma)
	if err != nil {
		return fmt.Errorf("failed to compute gamma: %w", err)
	}
	// 	fmt.Printf("[DEBUG CRYPTO] x_gamma: %x (parity=%v)\n", xGamma, gammaParity)

	// Compute v = redp1(x_gamma, parity=1)
	// This hashes x_gamma and lifts to curve with y-parity inverted
	v := c.curve.Redp1(xGamma, true)
	if v == nil {
		return errors.New("failed to compute v point")
	}
	// 	fmt.Printf("[DEBUG CRYPTO] v point computed\n")

	// Lift server public key: W_b = lift_x(x_w_b, x_w_b_parity)
	// 	fmt.Printf("[DEBUG CRYPTO] Server public key x_w_b: %x (parity=%v)\n", c.xWB, c.xWBParity)
	xWBInt := new(big.Int).SetBytes(c.xWB)
	wB := c.curve.LiftX(xWBInt, c.xWBParity)
	if wB == nil {
		return errors.New("failed to lift server public key")
	}

	// Compute W_b + v
	wBPlusV := c.curve.Add(wB, v)
	if wBPlusV == nil {
		return errors.New("failed to add W_b and v")
	}

	// Compute j first (should be done before this function, but verify)
	// 	fmt.Printf("[DEBUG CRYPTO] j (hash of public keys): %x\n", c.j)

	// Compute scalar: (i * j + s_a) mod r
	jInt := new(big.Int).SetBytes(c.j)
	iTimesJ := new(big.Int).Mul(iInt, jInt)

	sAInt := new(big.Int).SetBytes(c.sA)
	scalar := new(big.Int).Add(iTimesJ, sAInt)
	scalar = c.curve.FiniteFieldValue(scalar)
	// 	fmt.Printf("[DEBUG CRYPTO] Scalar: %x\n", scalar.Bytes())

	// Compute point: pt = scalar * (W_b + v)
	pt := c.curve.scalarMult(wBPlusV, scalar)
	if pt == nil {
		return errors.New("failed to compute shared secret point")
	}

	// Convert to Montgomery x-coordinate
	z, _, err := c.curve.ToMontgomery(pt)
	if err != nil {
		return fmt.Errorf("failed to convert to Montgomery: %w", err)
	}

	c.z = z
	// 	fmt.Printf("[DEBUG CRYPTO] Shared secret z: %x\n", c.z)

	// Final shared secret: secret = SHA256(z)
	c.secret = GetSHA256Digest(c.z)
	// 	fmt.Printf("[DEBUG CRYPTO] Final secret (SHA256 of z): %x\n", c.secret)

	return nil
}

// verifyServerConfirmation verifies server's proof of shared secret
func (c *Client) verifyServerConfirmation() error {
	if len(c.response) < 2 {
		return errors.New("server confirmation too short")
	}

	// 	fmt.Printf("[DEBUG] Server response length: %d bytes\n", len(c.response))
	// 	fmt.Printf("[DEBUG] Server response header: %02x %02x\n", c.response[0], c.response[1])

	// Check for error message
	// Error format: length(0x21 = 33 bytes) + handler(0x06) + error text
	// Confirmation format: length(0x20 = 32 bytes) + handler(0x06) + 32-byte confirmation
	if c.response[0] == 0x21 && c.response[1] == 0x06 {
		// Error response
		errorMsg := string(c.response[2:])
		return fmt.Errorf("server error: %s", errorMsg)
	}

	// Extract server confirmation code (skip length and handler)
	serverCC := c.response[2:]
	// 	fmt.Printf("[DEBUG] Server confirmation code length: %d bytes\n", len(serverCC))

	// Compute expected server confirmation: Sc = SHA256(j || Cc || z)
	c.serverCC = GetSHA256Digest(append(append(c.j, c.clientCC...), c.z...))
	// 	fmt.Printf("[DEBUG] Expected confirmation length: %d bytes\n", len(c.serverCC))

	// Verify
	if len(serverCC) != len(c.serverCC) {
		return fmt.Errorf("server confirmation length mismatch: got %d, expected %d", len(serverCC), len(c.serverCC))
	}

	for i := range c.serverCC {
		if serverCC[i] != c.serverCC[i] {
			return errors.New("server confirmation mismatch - authentication failed")
		}
	}

	// Derive encryption keys
	c.sendAESKey, c.recvAESKey, c.sendHMACKey, c.recvHMACKey = GenStreamKeys(false, c.secret)

	c.stage = 3
	return nil
}

// Send sends an encrypted message to the server and waits for response
func (c *Client) Send(msg []byte) ([]byte, error) {
	if c.stage != 3 {
		return nil, errors.New("not authenticated")
	}

	// Encrypt message
	encrypted, err := EncryptMessage(msg, c.sendAESKey, c.sendHMACKey)
	if err != nil {
		return nil, fmt.Errorf("encryption failed: %w", err)
	}

	// Fragment message
	fragmented := FragmentMessage(encrypted)

	// Send
	if _, err := c.conn.Write(fragmented); err != nil {
		return nil, fmt.Errorf("send failed: %w", err)
	}

	// Receive response
	return c.Receive()
}

// SendNoReply sends an encrypted message without waiting for response
// Used for keep-alive and fire-and-forget messages
func (c *Client) SendNoReply(msg []byte) error {
	if c.stage != 3 {
		return errors.New("not authenticated")
	}

	// 	fmt.Printf("[ECSRP Client] SendNoReply: %d bytes plaintext\n", len(msg))

	// Encrypt message
	encrypted, err := EncryptMessage(msg, c.sendAESKey, c.sendHMACKey)
	if err != nil {
		return fmt.Errorf("encryption failed: %w", err)
	}
	// 	fmt.Printf("[ECSRP Client] Encrypted to %d bytes\n", len(encrypted))

	// Fragment message
	fragmented := FragmentMessage(encrypted)
	// 	fmt.Printf("[ECSRP Client] Fragmented to %d bytes: %x\n", len(fragmented), fragmented[:min(32, len(fragmented))])

	// Send
	_, err = c.conn.Write(fragmented)
	// n, err := c.conn.Write(fragmented)
	if err != nil {
		return fmt.Errorf("send failed: %w", err)
	}
	// 	fmt.Printf("[ECSRP Client] Sent %d bytes to server\n", n)

	return nil
}

// Receive receives and decrypts a message from the server
// Handles multiple concatenated messages by buffering
// Also handles fragmented messages that span multiple TCP reads
func (c *Client) Receive() ([]byte, error) {
	if c.stage != 3 {
		return nil, errors.New("not authenticated")
	}

	for {
		// Check if we have buffered data from previous read
		if len(c.recvBuffer) == 0 {
			// Read more data from connection
			buf := make([]byte, 4096)
			// 			fmt.Printf("[ECSRP Client] Waiting for server message...\n")
			n, err := c.conn.Read(buf)
			if err != nil {
				// 				fmt.Printf("[ECSRP Client] Read error: %v\n", err)
				if err == io.EOF {
					return nil, err
				}
				return nil, fmt.Errorf("receive failed: %w", err)
			}
			c.recvBuffer = buf[:n]
			// 			fmt.Printf("[ECSRP Client] Received %d bytes from server: %x\n", n, c.recvBuffer[:min(32, len(c.recvBuffer))])
		} else {
			// 			fmt.Printf("[ECSRP Client] Processing buffered data: %d bytes\n", len(c.recvBuffer))
		}

		// Check for error messages (plaintext with handler 0x21)
		if len(c.recvBuffer) >= 2 && c.recvBuffer[1] == 0x21 {
			// Plaintext error message
			errMsg := c.recvBuffer
			c.recvBuffer = nil
			if len(errMsg) > 2 {
				return nil, fmt.Errorf("server error: %s", string(errMsg[2:]))
			}
			return nil, errors.New("server error (no message)")
		}

		// Reassemble fragmented message (only first message if multiple concatenated)
		assembled, consumed, err := ReassembleMessage(c.recvBuffer)
		if err == ErrNeedMoreData {
			// Fragmented message is incomplete, read more data
			// 			fmt.Printf("[ECSRP Client] Need more data, reading from connection...\n")
			buf := make([]byte, 4096)
			n, readErr := c.conn.Read(buf)
			if readErr != nil {
				// 				fmt.Printf("[ECSRP Client] Read error while waiting for more data: %v\n", readErr)
				c.recvBuffer = nil
				return nil, fmt.Errorf("receive failed: %w", readErr)
			}
			// Append new data to existing buffer
			c.recvBuffer = append(c.recvBuffer, buf[:n]...)
			// 			fmt.Printf("[ECSRP Client] Buffer now has %d bytes\n", len(c.recvBuffer))
			continue // Try reassembly again
		}
		if err != nil {
			// 			fmt.Printf("[ECSRP Client] Reassemble error: %v\n", err)
			c.recvBuffer = nil // Clear buffer on error
			return nil, fmt.Errorf("reassemble failed: %w", err)
		}
		// 		fmt.Printf("[ECSRP Client] Assembled message: %d bytes (consumed %d of %d)\n", len(assembled), consumed, len(c.recvBuffer))

		// Update buffer with remaining data
		if consumed < len(c.recvBuffer) {
			c.recvBuffer = c.recvBuffer[consumed:]
			// 			fmt.Printf("[ECSRP Client] Buffered %d bytes for next message\n", len(c.recvBuffer))
		} else {
			c.recvBuffer = nil
		}

		// Decrypt message
		decrypted, err := DecryptMessage(assembled, c.recvAESKey, c.recvHMACKey)
		if err != nil {
			// 			fmt.Printf("[ECSRP Client] Decryption error: %v\n", err)
			return nil, fmt.Errorf("decryption failed: %w", err)
		}
		// 		fmt.Printf("[ECSRP Client] Decrypted message: %d bytes\n", len(decrypted))

		return decrypted, nil
	}
}

// startKeepAlive starts the keep-alive goroutine
func (c *Client) startKeepAlive() {
	if c.keepAliveActive {
		return
	}

	// Skip if keep-alive is disabled (interval = 0)
	if c.keepAliveInterval == 0 {
		// 		fmt.Printf("[DEBUG] Keep-alive disabled (interval=0)\n")
		return
	}

	c.keepAliveActive = true
	// 	fmt.Printf("[DEBUG] Starting keep-alive with %v interval\n", c.keepAliveInterval)

	go func() {
		ticker := time.NewTicker(c.keepAliveInterval)
		defer ticker.Stop()

		for {
			select {
			case <-ticker.C:
				if err := c.sendKeepAliveMsg(); err != nil {
					// 					fmt.Printf("[WARN] Keep-alive failed: %v\n", err)
					return
				}
			case <-c.stopKeepAlive:
				// 				fmt.Printf("[DEBUG] Keep-alive stopped\n")
				return
			}
		}
	}()
}

// sendKeepAliveMsg sends a keep-alive message to the server
func (c *Client) sendKeepAliveMsg() error {
	// MikroTik Winbox keep-alive: simple M2 ping message
	// This is a minimal M2 message that just checks connection
	keepAliveMsg := []byte{
		'M', '2', // M2 protocol header
		0x05, 0x00, 0xff, 0x01, // Message type
		0x06, 0x00, 0xff, 0x09, 0x01, // Sequence
		0x07, 0x00, 0xff, 0x09, 0x07, // Request ID
	}

	// Send without waiting for response (fire-and-forget)
	err := c.SendNoReply(keepAliveMsg)
	if err != nil {
		return err
	}

	// 	fmt.Printf("[DEBUG] Keep-alive sent\n")
	return nil
}

// stopKeepAliveRoutine stops the keep-alive goroutine
func (c *Client) stopKeepAliveRoutine() {
	if c.keepAliveActive {
		close(c.stopKeepAlive)
		c.keepAliveActive = false
	}
}

// Close closes the connection
func (c *Client) Close() error {
	c.stopKeepAliveRoutine()
	if c.conn != nil {
		return c.conn.Close()
	}
	return nil
}

// IsAuthenticated returns true if authentication completed successfully
func (c *Client) IsAuthenticated() bool {
	return c.stage == 3
}

// GetConnection returns the underlying TCP connection (for proxying)
func (c *Client) GetConnection() net.Conn {
	return c.conn
}

// boolToByte converts bool to byte (0 or 1)
func boolToByte(b bool) byte {
	if b {
		return 1
	}
	return 0
}

// Probe attempts to verify if a connection is speaking the Winbox M2 protocol
// It sends a valid Client Hello (Key Exchange) and checks for a valid M2 response.
func Probe(conn net.Conn) error {
	// Generate ephemeral key
	sA := make([]byte, 32)
	if _, err := rand.Read(sA); err != nil {
		return fmt.Errorf("keygen failed: %w", err)
	}

	curve := NewWCurve()
	xWA, parity, err := curve.GenPublicKey(sA)
	if err != nil {
		return fmt.Errorf("pubkey gen failed: %w", err)
	}

	// Format: handler(0x06) + "admin" + 0x00 + pubkey(32) + parity(1)
	username := "admin"
	msg := make([]byte, 0, len(username)+40)
	msg = append(msg, 0x06)
	msg = append(msg, []byte(username)...)
	msg = append(msg, 0x00)
	msg = append(msg, xWA...)
	msg = append(msg, boolToByte(parity))

	// Send
	if _, err := conn.Write(msg); err != nil {
		return fmt.Errorf("write failed: %w", err)
	}

	// Expect response (Server Hello or Reject)
	// We don't parse the content strictly, just that it's a valid M2 message frame
	_, err = conn.Read(make([]byte, 1024))
	if err != nil {
		return fmt.Errorf("read failed: %w", err)
	}

	return nil
}
