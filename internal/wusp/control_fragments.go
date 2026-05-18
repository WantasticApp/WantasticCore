package wusp

import (
	"encoding/binary"
	"fmt"
	"sync"
)

const (
	WUSPMaxDatagramPayload       = 1200
	USPRecommendedChunkSize      = 1120
	uspControlFragmentVersion    = 1
	uspControlFragmentMagic      = 0x54
	uspControlFragmentHeaderSize = 23
)

type USPControlFragment struct {
	MessageID  uint64
	Index      uint32
	Count      uint32
	RawSize    uint32
	Compressed bool
	Data       []byte
}

var uspControlLZ4Pool sync.Pool

func init() {
	uspControlLZ4Pool.New = func() any { return new(lz4State) }
}

func FragmentUSPControlPayload(payload []byte, messageID uint64, maxDatagramPayload int) ([][]byte, error) {
	if len(payload) == 0 {
		return nil, &ValidationError{Reason: "control payload missing bytes"}
	}
	if messageID == 0 {
		return nil, &ValidationError{Reason: "control fragment message id required"}
	}
	if maxDatagramPayload < uspControlFragmentHeaderSize+1 {
		maxDatagramPayload = WUSPMaxDatagramPayload
	}

	fragmentPayloadSize := maxDatagramPayload - uspControlFragmentHeaderSize
	wirePayload := payload
	rawSize := uint32(0)
	compressed := false
	if compressedPayload, ok := compressUSPControlPayload(payload); ok {
		wirePayload = compressedPayload
		rawSize = uint32(len(payload))
		compressed = true
	}

	count := uint32((len(wirePayload) + fragmentPayloadSize - 1) / fragmentPayloadSize)
	if count == 0 {
		count = 1
	}

	frames := make([][]byte, 0, count)
	for index := uint32(0); index < count; index++ {
		start := int(index) * fragmentPayloadSize
		end := start + fragmentPayloadSize
		if end > len(wirePayload) {
			end = len(wirePayload)
		}
		frame, err := EncodeUSPControlFragment(USPControlFragment{
			MessageID:  messageID,
			Index:      index,
			Count:      count,
			RawSize:    rawSize,
			Compressed: compressed,
			Data:       wirePayload[start:end],
		})
		if err != nil {
			return nil, err
		}
		frames = append(frames, frame)
	}
	return frames, nil
}

func EncodeUSPControlFragment(fragment USPControlFragment) ([]byte, error) {
	if err := validateUSPControlFragment(fragment); err != nil {
		return nil, err
	}

	flags := uint8(0)
	if fragment.Compressed {
		flags |= 1 << 0
	}

	buf := make([]byte, 0, uspControlFragmentHeaderSize+len(fragment.Data))
	buf = append(buf, uspControlFragmentMagic, uspControlFragmentVersion, flags)
	var tmp64 [8]byte
	var tmp32 [4]byte
	binary.LittleEndian.PutUint64(tmp64[:], fragment.MessageID)
	buf = append(buf, tmp64[:]...)
	binary.LittleEndian.PutUint32(tmp32[:], fragment.Index)
	buf = append(buf, tmp32[:]...)
	binary.LittleEndian.PutUint32(tmp32[:], fragment.Count)
	buf = append(buf, tmp32[:]...)
	binary.LittleEndian.PutUint32(tmp32[:], fragment.RawSize)
	buf = append(buf, tmp32[:]...)
	buf = append(buf, fragment.Data...)
	return buf, nil
}

func DecodeUSPControlFragment(data []byte) (USPControlFragment, bool, error) {
	if len(data) == 0 || data[0] != uspControlFragmentMagic {
		return USPControlFragment{}, false, nil
	}
	if len(data) < uspControlFragmentHeaderSize {
		return USPControlFragment{}, true, fmt.Errorf("%w: control fragment payload too short", ErrUSPTransportMalformed)
	}
	if data[1] != uspControlFragmentVersion {
		return USPControlFragment{}, true, fmt.Errorf("%w: control fragment version %d", ErrUSPTransportUnsupported, data[1])
	}

	fragment := USPControlFragment{
		Compressed: data[2]&(1<<0) != 0,
		MessageID:  binary.LittleEndian.Uint64(data[3:11]),
		Index:      binary.LittleEndian.Uint32(data[11:15]),
		Count:      binary.LittleEndian.Uint32(data[15:19]),
		RawSize:    binary.LittleEndian.Uint32(data[19:23]),
		Data:       data[23:],
	}
	if err := validateUSPControlFragment(fragment); err != nil {
		return USPControlFragment{}, true, err
	}
	return fragment, true, nil
}

func ReassembleUSPControlFragments(fragments []USPControlFragment) ([]byte, error) {
	if len(fragments) == 0 {
		return nil, &ValidationError{Reason: "missing control fragments"}
	}

	first := fragments[0]
	if err := validateUSPControlFragment(first); err != nil {
		return nil, err
	}

	if uint32(len(fragments)) != first.Count {
		return nil, fmt.Errorf("%w: control fragment count mismatch", ErrUSPTransportMalformed)
	}

	ordered := make([][]byte, first.Count)
	totalSize := 0
	for _, fragment := range fragments {
		if err := validateUSPControlFragment(fragment); err != nil {
			return nil, err
		}
		if fragment.MessageID != first.MessageID || fragment.Count != first.Count || fragment.Compressed != first.Compressed || fragment.RawSize != first.RawSize {
			return nil, fmt.Errorf("%w: mixed control fragment assembly", ErrUSPTransportMalformed)
		}
		if ordered[fragment.Index] != nil {
			return nil, fmt.Errorf("%w: duplicate control fragment index %d", ErrUSPTransportMalformed, fragment.Index)
		}
		ordered[fragment.Index] = fragment.Data
		totalSize += len(fragment.Data)
	}

	payload := make([]byte, 0, totalSize)
	for index, data := range ordered {
		if data == nil {
			return nil, fmt.Errorf("%w: missing control fragment index %d", ErrUSPTransportMalformed, index)
		}
		payload = append(payload, data...)
	}
	if !first.Compressed {
		return payload, nil
	}
	if first.RawSize == 0 {
		return nil, fmt.Errorf("%w: compressed control fragments missing raw size", ErrUSPTransportMalformed)
	}
	return decompressUSPControlPayload(payload, int(first.RawSize))
}

func validateUSPControlFragment(fragment USPControlFragment) error {
	if fragment.MessageID == 0 {
		return &ValidationError{Reason: "control fragment message id required"}
	}
	if fragment.Count == 0 {
		return &ValidationError{Reason: "control fragment count required"}
	}
	if fragment.Index >= fragment.Count {
		return &ValidationError{Reason: "control fragment index out of range"}
	}
	if len(fragment.Data) == 0 {
		return &ValidationError{Reason: "control fragment missing payload"}
	}
	if fragment.Compressed && fragment.RawSize == 0 {
		return &ValidationError{Reason: "compressed control fragment missing raw size"}
	}
	return nil
}

func compressUSPControlPayload(data []byte) ([]byte, bool) {
	if len(data) < 512 {
		return nil, false
	}
	state := uspControlLZ4Pool.Get().(*lz4State)
	defer uspControlLZ4Pool.Put(state)
	compressed := state.compress(data)
	if len(compressed) == 0 || len(compressed) >= len(data) {
		return nil, false
	}
	return compressed, true
}

func decompressUSPControlPayload(data []byte, rawSize int) ([]byte, error) {
	state := uspControlLZ4Pool.Get().(*lz4State)
	defer uspControlLZ4Pool.Put(state)
	return state.decompress(data, rawSize)
}
