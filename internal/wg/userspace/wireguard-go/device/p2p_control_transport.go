package device

import (
	"encoding/binary"
	"errors"
	"fmt"
)

const (
	maxP2PControlPayload = 1200
	p2pControlHeaderSize = 6
	p2pControlMagic      = "WPC\x01"
)

func frameP2PControlPayload(payload []byte) ([]byte, error) {
	if len(payload) > maxP2PControlPayload {
		return nil, fmt.Errorf("P2P control payload too large: %d", len(payload))
	}
	framed := make([]byte, p2pControlHeaderSize+len(payload))
	copy(framed, p2pControlMagic)
	binary.LittleEndian.PutUint16(framed[4:], uint16(len(payload)))
	copy(framed[p2pControlHeaderSize:], payload)
	return framed, nil
}

func consumeP2PControlPayload(plaintext []byte) ([]byte, error) {
	if len(plaintext) < p2pControlHeaderSize {
		return nil, errors.New("payload is missing its length header")
	}
	if string(plaintext[:4]) != p2pControlMagic {
		return nil, errors.New("unsupported P2P control envelope")
	}
	length := int(binary.LittleEndian.Uint16(plaintext[4:p2pControlHeaderSize]))
	if length > maxP2PControlPayload || length > len(plaintext)-p2pControlHeaderSize {
		return nil, fmt.Errorf("invalid payload length %d", length)
	}
	end := p2pControlHeaderSize + length
	if len(plaintext)-end >= PaddingMultiple {
		return nil, errors.New("excess P2P control padding")
	}
	for _, b := range plaintext[end:] {
		if b != 0 {
			return nil, errors.New("invalid P2P control padding")
		}
	}
	return plaintext[p2pControlHeaderSize:end], nil
}
