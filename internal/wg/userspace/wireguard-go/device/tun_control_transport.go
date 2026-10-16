package device

import (
	"encoding/binary"
	"errors"
	"fmt"
)

const (
	maxTUNControlPayload = 1200
	tunControlHeaderSize = 6
	tunControlMagic      = "WTC\x01"
)

func frameTUNControlPayload(payload []byte) ([]byte, error) {
	if len(payload) > maxTUNControlPayload {
		return nil, fmt.Errorf("TUN control payload too large: %d", len(payload))
	}
	framed := make([]byte, tunControlHeaderSize+len(payload))
	copy(framed, tunControlMagic)
	binary.LittleEndian.PutUint16(framed[4:], uint16(len(payload)))
	copy(framed[tunControlHeaderSize:], payload)
	return framed, nil
}

func consumeTUNControlPayload(plaintext []byte) ([]byte, error) {
	if len(plaintext) < tunControlHeaderSize {
		return nil, errors.New("payload is missing its length header")
	}
	// The outer transport type is not authenticated by WireGuard. Check a
	// domain marker inside the ciphertext before interpreting routing actions.
	if string(plaintext[:4]) != tunControlMagic {
		return nil, errors.New("unsupported TUN control envelope")
	}
	length := int(binary.LittleEndian.Uint16(plaintext[4:tunControlHeaderSize]))
	if length > maxTUNControlPayload || length > len(plaintext)-tunControlHeaderSize {
		return nil, fmt.Errorf("invalid payload length %d", length)
	}
	end := tunControlHeaderSize + length
	if len(plaintext)-end >= PaddingMultiple {
		return nil, errors.New("excess TUN control padding")
	}
	for _, b := range plaintext[end:] {
		if b != 0 {
			return nil, errors.New("invalid TUN control padding")
		}
	}
	return plaintext[tunControlHeaderSize:end], nil
}
