package ecsrp

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha1"
	"crypto/sha256"
	"errors"
)

// GenStreamKeys derives AES and HMAC keys from the shared secret
// This implements the key derivation from MarginResearch encryption.py
func GenStreamKeys(isServer bool, sharedSecret []byte) (sendAES, recvAES, sendHMAC, recvHMAC []byte) {
	// Magic strings from MikroTik RouterOS
	magic2 := []byte("On the client side, this is the send key; on the server side, it is the receive key.")
	magic3 := []byte("On the client side, this is the receive key; on the server side, it is the send key.")

	var txEnc, rxEnc []byte

	if isServer {
		// Server: send = magic3, receive = magic2
		txEnc = append(sharedSecret, make([]byte, 40)...)
		txEnc = append(txEnc, magic3...)
		txEnc = append(txEnc, make([]byte, 40)...)
		for i := len(sharedSecret) + 40 + len(magic3); i < len(txEnc); i++ {
			txEnc[i] = 0xf2
		}

		rxEnc = append(sharedSecret, make([]byte, 40)...)
		rxEnc = append(rxEnc, magic2...)
		rxEnc = append(rxEnc, make([]byte, 40)...)
		for i := len(sharedSecret) + 40 + len(magic2); i < len(rxEnc); i++ {
			rxEnc[i] = 0xf2
		}
	} else {
		// Client: send = magic2, receive = magic3
		txEnc = append(sharedSecret, make([]byte, 40)...)
		txEnc = append(txEnc, magic2...)
		txEnc = append(txEnc, make([]byte, 40)...)
		for i := len(sharedSecret) + 40 + len(magic2); i < len(txEnc); i++ {
			txEnc[i] = 0xf2
		}

		rxEnc = append(sharedSecret, make([]byte, 40)...)
		rxEnc = append(rxEnc, magic3...)
		rxEnc = append(rxEnc, make([]byte, 40)...)
		for i := len(sharedSecret) + 40 + len(magic3); i < len(rxEnc); i++ {
			rxEnc[i] = 0xf2
		}
	}

	// Derive seeds using SHA-1
	rxSeed := sha1.Sum(rxEnc)
	txSeed := sha1.Sum(txEnc)

	// Derive keys using HKDF
	sendKey := hkdf(txSeed[:16])
	recvKey := hkdf(rxSeed[:16])

	// Split into AES (16 bytes) and HMAC (20 bytes) keys
	sendAES = sendKey[:16]
	sendHMAC = sendKey[16:36]
	recvAES = recvKey[:16]
	recvHMAC = recvKey[16:36]

	return sendAES, recvAES, sendHMAC, recvHMAC
}

// hkdf implements the HKDF-like function from MarginResearch
// This is NOT standard HKDF - it's MikroTik's custom variant
func hkdf(message []byte) []byte {
	// Initial HMAC with zero key
	mac := hmac.New(sha1.New, make([]byte, 0x40))
	mac.Write(message)
	h1 := mac.Sum(nil)

	result := make([]byte, 0)
	h2 := []byte{}

	// Generate two iterations
	for i := 0; i < 2; i++ {
		mac = hmac.New(sha1.New, h1)
		mac.Write(h2)
		mac.Write([]byte{byte(i + 1)})
		h2 = mac.Sum(nil)
		result = append(result, h2...)
	}

	// Return first 0x24 (36) bytes
	return result[:0x24]
}

// GetSHA256Digest computes SHA-256 hash
func GetSHA256Digest(input []byte) []byte {
	hash := sha256.Sum256(input)
	return hash[:]
}

// EncryptMessage encrypts a message using AES-CBC with HMAC-SHA1 (Mac-then-Encrypt)
// This implements the modified PKCS-7 padding used by MikroTik
func EncryptMessage(msg, aesKey, hmacKey []byte) ([]byte, error) {
	if len(msg) < 2 || msg[0] != 'M' || msg[1] != '2' {
		return nil, errors.New("message must begin with 'M2'")
	}

	// Compute HMAC
	mac := hmac.New(sha1.New, hmacKey)
	mac.Write(msg)
	hmacSum := mac.Sum(nil)

	// Generate random IV
	iv := make([]byte, 16)
	if _, err := rand.Read(iv); err != nil {
		return nil, err
	}

	// Create AES cipher
	block, err := aes.NewCipher(aesKey)
	if err != nil {
		return nil, err
	}

	// Modified PKCS-7 padding: instead of padding with n, pad with n-1
	msgWithMAC := append(msg, hmacSum...)
	padByte := 0xf - (len(msgWithMAC) % 0x10)

	// Add padding and the pad byte count
	paddedMsg := make([]byte, len(msgWithMAC))
	copy(paddedMsg, msgWithMAC)
	for i := 0; i < padByte; i++ {
		paddedMsg = append(paddedMsg, byte(padByte))
	}
	paddedMsg = append(paddedMsg, byte(padByte))

	// Encrypt
	mode := cipher.NewCBCEncrypter(block, iv)
	ciphertext := make([]byte, len(paddedMsg))
	mode.CryptBlocks(ciphertext, paddedMsg)

	// Format: length(2) + iv(16) + ciphertext
	msgLen := len(ciphertext)
	result := make([]byte, 2)
	result[0] = byte(msgLen >> 8)
	result[1] = byte(msgLen & 0xff)
	result = append(result, iv...)
	result = append(result, ciphertext...)

	return result, nil
}

// DecryptMessage decrypts a message using AES-CBC and verifies HMAC-SHA1
func DecryptMessage(data, aesKey, hmacKey []byte) ([]byte, error) {
	if len(data) < 2 {
		return nil, errors.New("message too short")
	}

	// Skip length prefix and extract IV + ciphertext
	data = data[2:]
	if len(data) < 16 {
		return nil, errors.New("message too short for IV")
	}

	iv := data[:16]
	ciphertext := data[16:]

	// Create AES cipher
	block, err := aes.NewCipher(aesKey)
	if err != nil {
		return nil, err
	}

	// Decrypt
	mode := cipher.NewCBCDecrypter(block, iv)
	plaintext := make([]byte, len(ciphertext))
	mode.CryptBlocks(plaintext, ciphertext)

	// Remove modified PKCS-7 padding
	if len(plaintext) == 0 {
		return nil, errors.New("plaintext is empty")
	}

	padByte := plaintext[len(plaintext)-1]
	if padByte != 0 {
		// Remove padding
		paddingLen := int(padByte) + 1
		if paddingLen > len(plaintext) {
			return nil, errors.New("invalid padding")
		}
		plaintext = plaintext[:len(plaintext)-paddingLen]
	} else {
		// No padding, just remove the trailing byte
		plaintext = plaintext[:len(plaintext)-1]
	}

	// Extract HMAC (last 20 bytes)
	if len(plaintext) < 20 {
		return nil, errors.New("message too short for HMAC")
	}

	hmacSum := plaintext[len(plaintext)-20:]
	msg := plaintext[:len(plaintext)-20]

	// Verify HMAC
	mac := hmac.New(sha1.New, hmacKey)
	mac.Write(msg)
	expectedMAC := mac.Sum(nil)

	if !hmac.Equal(hmacSum, expectedMAC) {
		return nil, errors.New("HMAC verification failed")
	}

	return msg, nil
}

// FragmentMessage splits a message into 0xff byte chunks with proper headers
// This implements the Winbox message fragmentation protocol
// Input format: length(2) + iv(16) + ciphertext
// Output format: chunkLen(1) + handler(1) + [length(2) + iv(16) + ciphertext...]
func FragmentMessage(encrypted []byte) []byte {
	if len(encrypted) < 2 {
		return encrypted
	}

	// The encrypted message includes: length(2) + iv(16) + ciphertext
	// We need to fragment the ENTIRE message including the length prefix
	remaining := encrypted
	result := []byte{}
	isFirst := true

	for len(remaining) > 0 {
		var chunkSize int
		var handler byte

		if isFirst {
			// First chunk: use 0x06 handler
			handler = 0x06
			if len(remaining) >= 0xff {
				chunkSize = 0xff
			} else {
				chunkSize = len(remaining)
			}
			isFirst = false
		} else {
			// Continuation chunks: use 0xff handler
			handler = 0xff
			if len(remaining) >= 0xff {
				chunkSize = 0xff
			} else {
				chunkSize = len(remaining)
			}
		}

		// Add chunk header: length + handler
		result = append(result, byte(chunkSize), handler)
		// Add chunk data
		result = append(result, remaining[:chunkSize]...)
		remaining = remaining[chunkSize:]
	}

	return result
}

// ErrNeedMoreData indicates that a fragmented message is incomplete
// and more data needs to be read from the connection
var ErrNeedMoreData = errors.New("incomplete fragmented message, need more data")

// ReassembleMessage reassembles fragmented Winbox messages
// Input format: chunkLen(1) + handler(1) + data... [+ chunkLen(1) + 0xff + data...]
// Output format: length(2) + iv(16) + ciphertext (ready for DecryptMessage)
//
// IMPORTANT: This only reassembles ONE message. If the input contains multiple
// concatenated messages (common when client sends rapidly), only the first is returned.
// The caller should call this repeatedly with remaining data if needed.
//
// Returns ErrNeedMoreData if the message is fragmented and incomplete.
func ReassembleMessage(data []byte) ([]byte, int, error) {
	if len(data) < 2 {
		return nil, 0, ErrNeedMoreData
	}

	// First chunk must have handler 0x06
	if data[1] != 0x06 {
		return nil, 0, errors.New("unknown handler (expected 0x06)")
	}

	assembled := []byte{}
	pos := 0

	for pos+2 <= len(data) {
		chunkLen := int(data[pos])
		handler := data[pos+1]
		pos += 2 // Skip length and handler bytes

		// Validate handler (0x06 for first, 0xff for continuation)
		if len(assembled) == 0 && handler != 0x06 {
			return nil, 0, errors.New("first chunk must have handler 0x06")
		}
		if len(assembled) > 0 && handler != 0xff {
			return nil, 0, errors.New("continuation chunk must have handler 0xff")
		}

		// Check if we have enough data for this chunk
		if pos+chunkLen > len(data) {
			// Not enough data yet - need to wait for more from the connection
			// This can happen for:
			// 1. A fragmented message where we're waiting for continuation chunks
			// 2. The last chunk of a fragmented message (which is < 0xff bytes)
			return nil, 0, ErrNeedMoreData
		}
		assembled = append(assembled, data[pos:pos+chunkLen]...)
		pos += chunkLen

		// If this chunk was less than 0xff, the message is complete
		// (not fragmented, or this was the last fragment)
		if chunkLen < 0xff {
			break
		}

		// chunk was 0xff, check if there's more data for continuation
		if pos+2 > len(data) {
			// We need at least 2 more bytes for the next chunk header
			return nil, 0, ErrNeedMoreData
		}
	}

	if len(assembled) < 2 {
		return nil, 0, errors.New("assembled message too short")
	}

	return assembled, pos, nil
}
