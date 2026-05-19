package wusp

import (
	"encoding/binary"
	"fmt"
	"math/bits"
	"strings"
	"sync"
)

const uspTransferStreamVersion = 1
const uspTransferStreamMagic = 0x53

type USPTransferStreamPhase uint8

const (
	USPTransferStreamOpen USPTransferStreamPhase = iota + 1
	USPTransferStreamChunk
	USPTransferStreamAck
	USPTransferStreamComplete
	USPTransferStreamAbort
)

type USPTransferStreamFrame struct {
	SessionID   uint64
	RequestID   uint64
	Method      USPAgentMethod
	Phase       USPTransferStreamPhase
	Sequence    uint32
	AckSequence uint32
	Offset      uint64
	TotalSize   uint64
	Path        string
	Filename    string
	ContentType string
	Metadata    map[string]string
	Data        []byte
	Final       bool
}

var uspStreamLZ4Pool sync.Pool

func init() {
	uspStreamLZ4Pool.New = func() any { return new(lz4State) }
}

func EncodeUSPTransferStreamFrame(frame USPTransferStreamFrame) ([]byte, error) {
	if err := validateUSPTransferStreamFrame(frame); err != nil {
		return nil, err
	}

	flags := uint8(0)
	if frame.Final {
		flags |= 1 << 0
	}
	if stringsTrimmed(frame.Path) != "" {
		flags |= 1 << 1
	}
	if stringsTrimmed(frame.Filename) != "" {
		flags |= 1 << 2
	}
	if stringsTrimmed(frame.ContentType) != "" {
		flags |= 1 << 3
	}
	if len(frame.Metadata) > 0 {
		flags |= 1 << 4
	}

	payload := frame.Data
	if compressed, ok := compressUSPTransferChunk(payload); ok {
		flags |= 1 << 5
		payload = compressed
	}
	if len(payload) > 0 {
		flags |= 1 << 6
	}

	buf := newUSPTransportBuffer(64 + len(payload) + estimateMetadataSize(frame.Metadata))
	buf.buf = append(buf.buf, uspTransferStreamMagic, uspTransferStreamVersion, byte(frame.Phase), byte(frame.Method), flags)
	var tmp [8]byte
	binary.LittleEndian.PutUint64(tmp[:], frame.SessionID)
	buf.buf = append(buf.buf, tmp[:]...)
	binary.LittleEndian.PutUint64(tmp[:], frame.RequestID)
	buf.buf = append(buf.buf, tmp[:]...)
	var tmp32 [4]byte
	binary.LittleEndian.PutUint32(tmp32[:], frame.Sequence)
	buf.buf = append(buf.buf, tmp32[:]...)
	binary.LittleEndian.PutUint32(tmp32[:], frame.AckSequence)
	buf.buf = append(buf.buf, tmp32[:]...)
	binary.LittleEndian.PutUint64(tmp[:], frame.Offset)
	buf.buf = append(buf.buf, tmp[:]...)
	binary.LittleEndian.PutUint64(tmp[:], frame.TotalSize)
	buf.buf = append(buf.buf, tmp[:]...)
	if flags&(1<<1) != 0 {
		buf.writeString(frame.Path)
	}
	if flags&(1<<2) != 0 {
		buf.writeString(frame.Filename)
	}
	if flags&(1<<3) != 0 {
		buf.writeString(frame.ContentType)
	}
	if flags&(1<<4) != 0 {
		buf.writeMetadata(frame.Metadata)
	}
	if flags&(1<<6) != 0 {
		if flags&(1<<5) != 0 {
			buf.writeUvarint(uint64(len(frame.Data)))
		}
		buf.writeBytes(payload)
	}
	return buf.bytes(), nil
}

func DecodeUSPTransferStreamFrame(data []byte) (USPTransferStreamFrame, error) {
	if len(data) < 37 {
		return USPTransferStreamFrame{}, fmt.Errorf("%w: transfer stream payload too short", ErrUSPTransportMalformed)
	}
	if data[0] != uspTransferStreamMagic {
		return USPTransferStreamFrame{}, fmt.Errorf("%w: transfer stream magic %d", ErrUSPTransportUnsupported, data[0])
	}
	if data[1] != uspTransferStreamVersion {
		return USPTransferStreamFrame{}, fmt.Errorf("%w: transfer stream version %d", ErrUSPTransportUnsupported, data[1])
	}

	frame := USPTransferStreamFrame{
		Phase:  USPTransferStreamPhase(data[2]),
		Method: USPAgentMethod(data[3]),
		Final:  data[4]&(1<<0) != 0,
	}
	flags := data[4]
	reader := newUSPTransportReader(data[5:])
	frame.SessionID = binary.LittleEndian.Uint64(reader.data[:8])
	reader.pos += 8
	frame.RequestID = binary.LittleEndian.Uint64(reader.data[reader.pos : reader.pos+8])
	reader.pos += 8
	frame.Sequence = binary.LittleEndian.Uint32(reader.data[reader.pos : reader.pos+4])
	reader.pos += 4
	frame.AckSequence = binary.LittleEndian.Uint32(reader.data[reader.pos : reader.pos+4])
	reader.pos += 4
	frame.Offset = binary.LittleEndian.Uint64(reader.data[reader.pos : reader.pos+8])
	reader.pos += 8
	frame.TotalSize = binary.LittleEndian.Uint64(reader.data[reader.pos : reader.pos+8])
	reader.pos += 8
	var err error
	if flags&(1<<1) != 0 {
		frame.Path, err = reader.readString()
		if err != nil {
			return USPTransferStreamFrame{}, err
		}
	}
	if flags&(1<<2) != 0 {
		frame.Filename, err = reader.readString()
		if err != nil {
			return USPTransferStreamFrame{}, err
		}
	}
	if flags&(1<<3) != 0 {
		frame.ContentType, err = reader.readString()
		if err != nil {
			return USPTransferStreamFrame{}, err
		}
	}
	if flags&(1<<4) != 0 {
		frame.Metadata, err = reader.readMetadata()
		if err != nil {
			return USPTransferStreamFrame{}, err
		}
	}
	if flags&(1<<6) != 0 {
		rawSize := 0
		if flags&(1<<5) != 0 {
			value, err := reader.readUvarint()
			if err != nil {
				return USPTransferStreamFrame{}, err
			}
			rawSize = int(value)
		}
		frame.Data, err = reader.readBytes()
		if err != nil {
			return USPTransferStreamFrame{}, err
		}
		if flags&(1<<5) != 0 {
			frame.Data, err = decompressUSPTransferChunk(frame.Data, rawSize)
			if err != nil {
				return USPTransferStreamFrame{}, err
			}
		}
	}
	if !reader.done() && !reader.remainingAllZero() {
		return USPTransferStreamFrame{}, fmt.Errorf("%w: trailing transfer stream data", ErrUSPTransportMalformed)
	}
	return frame, validateUSPTransferStreamFrame(frame)
}

func validateUSPTransferStreamFrame(frame USPTransferStreamFrame) error {
	switch frame.Method {
	case USPAgentMethodUpload, USPAgentMethodDownload:
	default:
		return fmt.Errorf("%w: stream method %d", ErrUSPTransportUnsupported, frame.Method)
	}
	switch frame.Phase {
	case USPTransferStreamOpen, USPTransferStreamChunk, USPTransferStreamAck, USPTransferStreamComplete, USPTransferStreamAbort:
	default:
		return fmt.Errorf("%w: stream phase %d", ErrUSPTransportUnsupported, frame.Phase)
	}
	if frame.Phase == USPTransferStreamChunk && len(frame.Data) == 0 {
		return &ValidationError{Reason: "stream chunk frame missing payload"}
	}
	return nil
}

func compressUSPTransferChunk(data []byte) ([]byte, bool) {
	if len(data) < 256 || !likelyCompressibleUSPTransferChunk(data) {
		return nil, false
	}
	state := uspStreamLZ4Pool.Get().(*lz4State)
	defer uspStreamLZ4Pool.Put(state)
	compressed := state.compress(data)
	if len(compressed) == 0 || len(compressed) >= len(data) {
		return nil, false
	}
	return compressed, true
}

func decompressUSPTransferChunk(data []byte, rawSize int) ([]byte, error) {
	state := uspStreamLZ4Pool.Get().(*lz4State)
	defer uspStreamLZ4Pool.Put(state)
	return state.decompress(data, rawSize)
}

func stringsTrimmed(value string) string {
	return strings.TrimSpace(value)
}

func likelyCompressibleUSPTransferChunk(data []byte) bool {
	sampleSize := len(data)
	if sampleSize > 256 {
		sampleSize = 256
	}
	if sampleSize < 64 {
		return false
	}

	var seen [4]uint64
	repeats := 0
	last := data[0]
	for i := 0; i < sampleSize; i++ {
		b := data[i]
		seen[b>>6] |= 1 << (b & 63)
		if i > 0 && b == last {
			repeats++
		}
		last = b
	}
	if repeats >= sampleSize/16 {
		return true
	}

	unique := 0
	for _, bucket := range seen {
		unique += bits.OnesCount64(bucket)
	}
	return unique <= sampleSize/2
}
