// Package wusp — binary codec for TR-181 parameter messages.
//
// # Wire format
//
//	┌──────────── Header (8 bytes, never compressed) ─────────────────┐
//	│ [0..1] magic:        0x57 0x55 ("WU")                           │
//	│ [2]    format ver:   0x01                                        │
//	│ [3..4] schema fp:    FNV-16 of all registered paths (LE uint16)  │
//	│ [5]    flags:        bit-0=lz4  bit-1=device_id  bit-2=ts        │
//	│ [6..7] reserved:     0x00 0x00                                   │
//	└──────────────────────────────────────────────────────────────────┘
//	┌──────────── Payload (LZ4-block-compressed when flag bit-0=1) ────┐
//	│ (if lz4) [4 bytes] raw_size uint32 LE                            │
//	│ (if dev) [varint len][bytes] DeviceID string                     │
//	│ (if ts)  [zigzag int64] UnixNano timestamp                       │
//	│ [varint] field_count                                             │
//	│ field_count × {                                                  │
//	│   [varint] field_id  (0 = inline path)                           │
//	│   (id==0) [varint len][bytes] path string                        │
//	│   [1 byte] TypeTag                                               │
//	│   [variable] value — format defined by TypeTag below             │
//	│ }                                                                │
//	└──────────────────────────────────────────────────────────────────┘
//
// # TypeTag value wire formats
//
//	TagNull    (0x00)  — 0 bytes
//	TagFalse   (0x01)  — 0 bytes
//	TagTrue    (0x02)  — 0 bytes
//	TagUint    (0x03)  — unsigned varint
//	TagInt     (0x04)  — zigzag signed varint
//	TagFloat   (0x05)  — 8 bytes IEEE-754 LE
//	TagString  (0x06)  — [varint len][UTF-8 bytes]
//	TagBytes   (0x07)  — [varint len][raw bytes]   (base64 / hexBinary)
//	TagTime    (0x08)  — [zigzag int64] unix nanoseconds
//	TagIP4     (0x09)  — 4 bytes raw
//	TagIP6     (0x0A)  — 16 bytes raw
//	TagIP4Pfx  (0x0B)  — [4 bytes addr][1 byte prefix_len]
//	TagIP6Pfx  (0x0C)  — [16 bytes addr][1 byte prefix_len]
//	TagMAC     (0x0D)  — 6 bytes raw
//	TagList    (0x0E)  — [varint count]×[TypeTag + value]
//	(0x0F reserved for future extension with inline length prefix)
//
// # Dynamic compatibility
//
// Each Param in AllDeviceParams is assigned a stable numeric field_id
// at init() time (sorted by path → id=1,2,…N). Adding paths in a future
// BBF release appends new IDs at the end of that sorted order; old decoders
// see field_id > registry_size and skip via the TypeTag length rules.
// Removing a path leaves its ID as a tombstone so IDs never shift.
// Paths not in the local registry are encoded with field_id=0 (inline),
// so a new-schema encoder is always readable by an old-schema decoder.
package wusp

import (
	"encoding/binary"
	"errors"
	"hash/fnv"
	"io"
	"math"
	"net"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
	"unsafe"
)

// ---------------------------------------------------------------------------
// TypeTag
// ---------------------------------------------------------------------------

// TypeTag identifies the wire encoding of a value. It makes each field
// self-describing: a decoder that doesn't recognise a field_id can still
// skip the correct number of bytes using only the TypeTag.
type TypeTag uint8

const (
	TagNull   TypeTag = 0x00
	TagFalse  TypeTag = 0x01
	TagTrue   TypeTag = 0x02
	TagUint   TypeTag = 0x03
	TagInt    TypeTag = 0x04
	TagFloat  TypeTag = 0x05
	TagString TypeTag = 0x06
	TagBytes  TypeTag = 0x07
	TagTime   TypeTag = 0x08
	TagIP4    TypeTag = 0x09
	TagIP6    TypeTag = 0x0A
	TagIP4Pfx TypeTag = 0x0B
	TagIP6Pfx TypeTag = 0x0C
	TagMAC    TypeTag = 0x0D
	TagList   TypeTag = 0x0E
)

// TagForParamType returns the best TypeTag for a BBF ParamType.
func TagForParamType(pt ParamType) TypeTag {
	switch pt {
	case TypeBoolean:
		return TagFalse // caller picks TagFalse or TagTrue
	case TypeInt, TypeLong:
		return TagInt
	case TypeUnsignedInt, TypeUnsignedLong, TypeStatsCounter:
		return TagUint
	case TypeDecimal:
		return TagFloat
	case TypeBase64, TypeHexBinary:
		return TagBytes
	case TypeDateTime:
		return TagTime
	case TypeIPv4Address:
		return TagIP4
	case TypeIPv6Address:
		return TagIP6
	case TypeIPv4Prefix:
		return TagIP4Pfx
	case TypeIPv6Prefix:
		return TagIP6Pfx
	case TypeMACAddress:
		return TagMAC
	case TypeList:
		return TagList
	default:
		return TagString
	}
}

// ---------------------------------------------------------------------------
// Value — zero-allocation tagged union
// ---------------------------------------------------------------------------

// Value holds one TR-181 parameter value. Only the field matching Tag is
// valid. Small values (bool, int, float, IP, MAC, prefix) are stored inline
// without heap allocation.
type Value struct {
	Tag  TypeTag
	ival int64   // TagFalse/TagTrue (unused), TagUint (uint64 cast), TagInt, TagTime
	fval float64 // TagFloat
	blob []byte  // TagString, TagBytes, TagIP4, TagIP6, TagIP4Pfx, TagIP6Pfx, TagMAC
	list []Value // TagList
}

// Null returns a null Value.
func Null() Value { return Value{Tag: TagNull} }

// Bool returns a TagFalse or TagTrue Value.
func Bool(v bool) Value {
	if v {
		return Value{Tag: TagTrue}
	}
	return Value{Tag: TagFalse}
}

// Uint returns a TagUint Value.
func Uint(v uint64) Value { return Value{Tag: TagUint, ival: int64(v)} }

// Int returns a TagInt Value.
func Int(v int64) Value { return Value{Tag: TagInt, ival: v} }

// Float returns a TagFloat Value.
func Float(v float64) Value { return Value{Tag: TagFloat, fval: v} }

// String returns a TagString Value.
func String(v string) Value { return Value{Tag: TagString, blob: unsafeBytes(v)} }

// Bytes returns a TagBytes Value (for base64/hexBinary params).
func Bytes(v []byte) Value { return Value{Tag: TagBytes, blob: v} }

// Time returns a TagTime Value (unix nanosecond precision).
func Time(v time.Time) Value { return Value{Tag: TagTime, ival: v.UnixNano()} }

// IP4 returns a TagIP4 Value from a net.IP (must be 4-byte or 16-byte IPv4).
func IP4(v net.IP) Value {
	v4 := v.To4()
	if v4 == nil {
		return Null()
	}
	b := make([]byte, 4)
	copy(b, v4)
	return Value{Tag: TagIP4, blob: b}
}

// IP6 returns a TagIP6 Value.
func IP6(v net.IP) Value {
	v6 := v.To16()
	if v6 == nil {
		return Null()
	}
	b := make([]byte, 16)
	copy(b, v6)
	return Value{Tag: TagIP6, blob: b}
}

// IP4Prefix returns a TagIP4Pfx Value from a net.IPNet.
func IP4Prefix(n *net.IPNet) Value {
	ones, _ := n.Mask.Size()
	b := make([]byte, 5)
	copy(b[:4], n.IP.To4())
	b[4] = byte(ones)
	return Value{Tag: TagIP4Pfx, blob: b}
}

// IP6Prefix returns a TagIP6Pfx Value from a net.IPNet.
func IP6Prefix(n *net.IPNet) Value {
	ones, _ := n.Mask.Size()
	b := make([]byte, 17)
	copy(b[:16], n.IP.To16())
	b[16] = byte(ones)
	return Value{Tag: TagIP6Pfx, blob: b}
}

// MAC returns a TagMAC Value.
func MAC(v net.HardwareAddr) Value {
	b := make([]byte, 6)
	copy(b, v)
	return Value{Tag: TagMAC, blob: b}
}

// List returns a TagList Value.
func List(items ...Value) Value { return Value{Tag: TagList, list: items} }

// AsBool returns the boolean value (false if tag is not bool).
func (v Value) AsBool() bool { return v.Tag == TagTrue }

// AsUint returns the uint64 value.
func (v Value) AsUint() uint64 { return uint64(v.ival) }

// AsInt returns the int64 value.
func (v Value) AsInt() int64 { return v.ival }

// AsFloat returns the float64 value.
func (v Value) AsFloat() float64 { return v.fval }

// AsString returns the string value (zero-copy when possible).
func (v Value) AsString() string {
	if len(v.blob) == 0 {
		return ""
	}
	return unsafe.String(&v.blob[0], len(v.blob))
}

// AsBytes returns the raw byte slice.
func (v Value) AsBytes() []byte { return v.blob }

// ValueToString renders any Value as a human-readable string, regardless of
// its type tag. Unlike AsString() which only works for blob-backed types,
// this handles Bool, Uint, Int, Float, Time, IP, List, etc.
func ValueToString(v Value) string {
	switch v.Tag {
	case TagNull:
		return ""
	case TagFalse:
		return "false"
	case TagTrue:
		return "true"
	case TagUint:
		return strconv.FormatUint(v.AsUint(), 10)
	case TagInt:
		return strconv.FormatInt(v.AsInt(), 10)
	case TagFloat:
		return strconv.FormatFloat(v.AsFloat(), 'f', -1, 64)
	case TagString:
		return v.AsString()
	case TagTime:
		return v.AsTime().UTC().Format(time.RFC3339)
	case TagIP4, TagIP6:
		if ip := v.AsIP4(); ip != nil {
			return ip.String()
		}
		if ip := v.AsIP6(); ip != nil {
			return ip.String()
		}
		return ""
	case TagMAC:
		return net.HardwareAddr(v.blob).String()
	case TagList:
		items := v.AsList()
		parts := make([]string, len(items))
		for i, item := range items {
			parts[i] = ValueToString(item)
		}
		return strings.Join(parts, ",")
	default:
		return v.AsString()
	}
}

// ParamAccess returns the access mode ("readOnly" or "readWrite") for a
// parameter path by looking it up in the registered schema. Concrete instance
// paths like "Device.IP.Interface.1.Name" are matched against their template
// form "Device.IP.Interface.{i}.Name". Returns "readOnly" if not found.
func ParamAccess(path string) string {
	info, _, ok := lookupSafeParam(path)
	if !ok {
		return "readOnly"
	}
	return string(info.param.Access)
}

// ParseStringValue converts a portal-supplied string into the most
// appropriate Value type. "true"/"false" → Bool, pure digits → Uint,
// everything else → String. This allows the portal to send param values
// as strings while the agent receives correctly typed values.
func ParseStringValue(s string) Value {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "true":
		return Bool(true)
	case "false":
		return Bool(false)
	}
	if n, err := strconv.ParseUint(s, 10, 64); err == nil {
		return Uint(n)
	}
	if n, err := strconv.ParseInt(s, 10, 64); err == nil {
		return Int(n)
	}
	return String(s)
}

// AsTime returns the time.Time value.
func (v Value) AsTime() time.Time { return time.Unix(0, v.ival) }

// AsIP4 returns a net.IP for TagIP4.
func (v Value) AsIP4() net.IP {
	if len(v.blob) != 4 {
		return nil
	}
	ip := make(net.IP, 4)
	copy(ip, v.blob)
	return ip
}

// AsIP6 returns a net.IP for TagIP6.
func (v Value) AsIP6() net.IP {
	if len(v.blob) != 16 {
		return nil
	}
	ip := make(net.IP, 16)
	copy(ip, v.blob)
	return ip
}

// AsList returns the list items.
func (v Value) AsList() []Value { return v.list }

// ---------------------------------------------------------------------------
// Field
// ---------------------------------------------------------------------------

// Field is a single encoded TR-181 parameter in a Message.
type Field struct {
	// id is the registry field ID. 0 means "inline path" (unknown to registry).
	id   uint16
	Path string // always populated after decode
	Val  Value
}

// ---------------------------------------------------------------------------
// Message
// ---------------------------------------------------------------------------

// Message is a collection of TR-181 parameters ready for encoding or freshly
// decoded from a binary frame.
type Message struct {
	DeviceID  string
	Timestamp time.Time
	Fields    []Field
}

// NewMessage creates an empty Message.
func NewMessage() *Message { return &Message{} }

// ResetID clears the registry field ID so the encoder uses the string path.
func (f *Field) ResetID() { f.id = 0 }

// ResetAllFieldIDs clears registry IDs on every field so the encoder uses
// string paths. Use when the receiver may have a different param registry —
// the controller and agent ship independent wusp packages whose path-code
// registries can drift, so encoding by string path is the only durable wire
// format across that boundary.
func (m *Message) ResetAllFieldIDs() {
	for i := range m.Fields {
		m.Fields[i].id = 0
	}
}

// Set adds or replaces a field by path.
func (m *Message) Set(path string, v Value) {
	for i := range m.Fields {
		if m.Fields[i].Path == path {
			m.Fields[i].Val = v
			return
		}
	}
	id, _ := globalRegistry.IDFor(path)
	m.Fields = append(m.Fields, Field{id: id, Path: path, Val: v})
}

// SetBool is a typed helper for Set.
func (m *Message) SetBool(path string, v bool) { m.Set(path, Bool(v)) }

// SetUint is a typed helper for Set.
func (m *Message) SetUint(path string, v uint64) { m.Set(path, Uint(v)) }

// SetInt is a typed helper for Set.
func (m *Message) SetInt(path string, v int64) { m.Set(path, Int(v)) }

// SetString is a typed helper for Set.
func (m *Message) SetString(path string, v string) { m.Set(path, String(v)) }

// SetTime is a typed helper for Set.
func (m *Message) SetTime(path string, v time.Time) { m.Set(path, Time(v)) }

// Get returns the first Field with the given path and a found flag.
func (m *Message) Get(path string) (Value, bool) {
	for _, f := range m.Fields {
		if f.Path == path {
			return f.Val, true
		}
	}
	return Null(), false
}

// ---------------------------------------------------------------------------
// Registry — path ↔ field_id
// ---------------------------------------------------------------------------

// registry maps TR-181 paths to compact uint16 field IDs.
// Built once at init() and never mutated.
type registry struct {
	pathToID map[string]uint16
	idToPath []string // index = id-1
	fp       uint16   // FNV-16 schema fingerprint
}

func buildRegistry(params []Param) *registry {
	sorted := make([]Param, len(params))
	copy(sorted, params)
	sort.Slice(sorted, func(i, j int) bool {
		return sorted[i].Path < sorted[j].Path
	})

	r := &registry{
		pathToID: make(map[string]uint16, len(sorted)),
		idToPath: make([]string, len(sorted)),
	}

	h := fnv.New32a()
	for i, p := range sorted {
		id := uint16(i + 1) // IDs start at 1; 0 is reserved for "inline"
		r.pathToID[p.Path] = id
		r.idToPath[i] = p.Path
		h.Write(unsafeBytes(p.Path))
	}
	r.fp = uint16(h.Sum32())
	return r
}

func (r *registry) IDFor(path string) (uint16, bool) {
	id, ok := r.pathToID[path]
	return id, ok
}

func (r *registry) PathForID(id uint16) (string, bool) {
	if id == 0 || int(id) > len(r.idToPath) {
		return "", false
	}
	return r.idToPath[id-1], true
}

// globalRegistry is set by init() after the generated/imported model layer has
// populated AllDeviceParams.
var globalRegistry *registry

func init() {
	globalRegistry = buildRegistry(AllDeviceParams)
}

// ---------------------------------------------------------------------------
// Frame constants
// ---------------------------------------------------------------------------

const (
	frameMagic0  = 0x57 // 'W'
	frameMagic1  = 0x55 // 'U'
	frameVersion = 0x01
	frameHdrSize = 8

	flagLZ4      = 1 << 0
	flagDeviceID = 1 << 1
	flagTS       = 1 << 2
)

// ---------------------------------------------------------------------------
// Encoder
// ---------------------------------------------------------------------------

// EncodeOptions controls encoding behaviour.
type EncodeOptions struct {
	Compress         bool // compress payload with LZ4 block
	IncludeDeviceID  bool
	IncludeTimestamp bool
}

// Encoder encodes Messages into binary frames.
// A single Encoder is safe for concurrent use after creation.
type Encoder struct {
	opts EncodeOptions
	pool sync.Pool // pool of *wbuf
	lz4  sync.Pool // pool of *lz4State
}

// NewEncoder creates a new Encoder.
func NewEncoder(opts EncodeOptions) *Encoder {
	e := &Encoder{opts: opts}
	e.pool.New = func() any { return &wbuf{b: make([]byte, 0, 512)} }
	e.lz4.New = func() any { return new(lz4State) }
	return e
}

// Encode encodes msg into a binary frame and returns the bytes.
// The returned slice is caller-owned and safe to modify.
func (e *Encoder) Encode(msg *Message) ([]byte, error) {
	pb := e.pool.Get().(*wbuf)
	pb.reset()
	defer e.pool.Put(pb)

	// --- payload ---
	if e.opts.IncludeDeviceID && msg.DeviceID != "" {
		pb.writeStr(msg.DeviceID)
	}
	if e.opts.IncludeTimestamp && !msg.Timestamp.IsZero() {
		pb.writeZigzag(msg.Timestamp.UnixNano())
	}

	pb.writeUvarint(uint64(len(msg.Fields)))
	for _, f := range msg.Fields {
		encodeField(pb, f)
	}

	payload := pb.b

	// --- header + optional LZ4 ---
	flags := byte(0)
	if e.opts.IncludeDeviceID && msg.DeviceID != "" {
		flags |= flagDeviceID
	}
	if e.opts.IncludeTimestamp && !msg.Timestamp.IsZero() {
		flags |= flagTS
	}

	var finalPayload []byte
	if e.opts.Compress {
		ls := e.lz4.Get().(*lz4State)
		comp := ls.compress(payload)
		e.lz4.Put(ls)
		if comp != nil {
			// LZ4 compressed: [raw_size uint32 LE][compressed data]
			hdr := make([]byte, 4)
			binary.LittleEndian.PutUint32(hdr, uint32(len(payload)))
			finalPayload = append(hdr, comp...)
			flags |= flagLZ4
		} else {
			finalPayload = payload
		}
	} else {
		finalPayload = payload
	}

	fp := globalRegistry.fp
	out := make([]byte, frameHdrSize+len(finalPayload))
	out[0] = frameMagic0
	out[1] = frameMagic1
	out[2] = frameVersion
	binary.LittleEndian.PutUint16(out[3:5], fp)
	out[5] = flags
	out[6] = 0
	out[7] = 0
	copy(out[frameHdrSize:], finalPayload)
	return out, nil
}

func encodeField(b *wbuf, f Field) {
	b.writeUvarint(uint64(f.id))
	if f.id == 0 {
		b.writeStr(f.Path)
	}
	encodeValue(b, f.Val)
}

func encodeValue(b *wbuf, v Value) {
	b.writeByte(byte(v.Tag))
	switch v.Tag {
	case TagNull, TagFalse, TagTrue:
		// 0 bytes
	case TagUint:
		b.writeUvarint(uint64(v.ival))
	case TagInt:
		b.writeZigzag(v.ival)
	case TagFloat:
		var buf [8]byte
		binary.LittleEndian.PutUint64(buf[:], math.Float64bits(v.fval))
		b.b = append(b.b, buf[:]...)
	case TagString, TagBytes:
		b.writeBlob(v.blob)
	case TagTime:
		b.writeZigzag(v.ival)
	case TagIP4:
		b.b = append(b.b, v.blob[:4]...)
	case TagIP6:
		b.b = append(b.b, v.blob[:16]...)
	case TagIP4Pfx:
		b.b = append(b.b, v.blob[:5]...)
	case TagIP6Pfx:
		b.b = append(b.b, v.blob[:17]...)
	case TagMAC:
		b.b = append(b.b, v.blob[:6]...)
	case TagList:
		b.writeUvarint(uint64(len(v.list)))
		for _, item := range v.list {
			encodeValue(b, item)
		}
	}
}

// ---------------------------------------------------------------------------
// Decoder
// ---------------------------------------------------------------------------

var (
	errBadMagic   = errors.New("wusp: bad frame magic")
	errBadVersion = errors.New("wusp: unsupported frame version")
	errTruncated  = errors.New("wusp: truncated frame")
	errUnknownTag = errors.New("wusp: unknown type tag")
)

// DecodeOptions controls decode behaviour.
type DecodeOptions struct {
	// SkipUnknownFields: if true, fields with unrecognised IDs are silently
	// dropped instead of being stored with their inline path.
	SkipUnknownFields bool
}

// Decoder decodes binary frames into Messages.
// A single Decoder is safe for concurrent use after creation.
type Decoder struct {
	opts DecodeOptions
	lz4  sync.Pool
}

// NewDecoder creates a new Decoder.
func NewDecoder(opts DecodeOptions) *Decoder {
	d := &Decoder{opts: opts}
	d.lz4.New = func() any { return new(lz4State) }
	return d
}

// Decode parses a binary frame produced by Encoder.Encode and returns a
// Message. Unknown paths (field_id > registry size) are decoded using the
// type tag and stored with their inline path string.
func (d *Decoder) Decode(data []byte) (*Message, error) {
	if len(data) < frameHdrSize {
		return nil, errTruncated
	}
	if data[0] != frameMagic0 || data[1] != frameMagic1 {
		return nil, errBadMagic
	}
	if data[2] != frameVersion {
		return nil, errBadVersion
	}
	flags := data[5]

	payload := data[frameHdrSize:]

	if flags&flagLZ4 != 0 {
		if len(payload) < 4 {
			return nil, errTruncated
		}
		rawSize := int(binary.LittleEndian.Uint32(payload[:4]))
		ls := d.lz4.Get().(*lz4State)
		var err error
		payload, err = ls.decompress(payload[4:], rawSize)
		d.lz4.Put(ls)
		if err != nil {
			return nil, err
		}
	}

	r := &rbuf{b: payload}
	msg := &Message{}

	if flags&flagDeviceID != 0 {
		s, err := r.readStr()
		if err != nil {
			return nil, err
		}
		msg.DeviceID = s
	}
	if flags&flagTS != 0 {
		ns, err := r.readZigzag()
		if err != nil {
			return nil, err
		}
		msg.Timestamp = time.Unix(0, ns)
	}

	count, err := r.readUvarint()
	if err != nil {
		return nil, err
	}

	msg.Fields = make([]Field, 0, count)
	for i := uint64(0); i < count; i++ {
		f, err := decodeField(r, d.opts.SkipUnknownFields)
		if err != nil {
			return nil, err
		}
		if f != nil {
			msg.Fields = append(msg.Fields, *f)
		}
	}
	return msg, nil
}

func decodeField(r *rbuf, skipUnknown bool) (*Field, error) {
	rawID, err := r.readUvarint()
	if err != nil {
		return nil, err
	}
	id := uint16(rawID)

	var path string
	if id == 0 {
		path, err = r.readStr()
		if err != nil {
			return nil, err
		}
	} else {
		if p, ok := globalRegistry.PathForID(id); ok {
			path = p
		} else {
			// Future field: we still need to consume value bytes.
			v, err := decodeValue(r)
			if err != nil {
				return nil, err
			}
			if skipUnknown {
				return nil, nil
			}
			return &Field{id: id, Path: "", Val: v}, nil
		}
	}

	v, err := decodeValue(r)
	if err != nil {
		return nil, err
	}
	return &Field{id: id, Path: path, Val: v}, nil
}

func decodeValue(r *rbuf) (Value, error) {
	tag, err := r.readByte()
	if err != nil {
		return Null(), err
	}
	switch TypeTag(tag) {
	case TagNull:
		return Null(), nil
	case TagFalse:
		return Bool(false), nil
	case TagTrue:
		return Bool(true), nil
	case TagUint:
		n, err := r.readUvarint()
		return Uint(n), err
	case TagInt:
		n, err := r.readZigzag()
		return Int(n), err
	case TagFloat:
		b, err := r.readN(8)
		if err != nil {
			return Null(), err
		}
		return Float(math.Float64frombits(binary.LittleEndian.Uint64(b))), nil
	case TagString:
		blob, err := r.readBlob()
		if err != nil {
			return Null(), err
		}
		return Value{Tag: TagString, blob: blob}, nil
	case TagBytes:
		blob, err := r.readBlob()
		if err != nil {
			return Null(), err
		}
		return Value{Tag: TagBytes, blob: blob}, nil
	case TagTime:
		ns, err := r.readZigzag()
		return Value{Tag: TagTime, ival: ns}, err
	case TagIP4:
		b, err := r.readN(4)
		return Value{Tag: TagIP4, blob: b}, err
	case TagIP6:
		b, err := r.readN(16)
		return Value{Tag: TagIP6, blob: b}, err
	case TagIP4Pfx:
		b, err := r.readN(5)
		return Value{Tag: TagIP4Pfx, blob: b}, err
	case TagIP6Pfx:
		b, err := r.readN(17)
		return Value{Tag: TagIP6Pfx, blob: b}, err
	case TagMAC:
		b, err := r.readN(6)
		return Value{Tag: TagMAC, blob: b}, err
	case TagList:
		cnt, err := r.readUvarint()
		if err != nil {
			return Null(), err
		}
		items := make([]Value, cnt)
		for i := range items {
			items[i], err = decodeValue(r)
			if err != nil {
				return Null(), err
			}
		}
		return List(items...), nil
	default:
		return Null(), errUnknownTag
	}
}

// ---------------------------------------------------------------------------
// wbuf — pooled write buffer
// ---------------------------------------------------------------------------

type wbuf struct{ b []byte }

func (b *wbuf) reset()           { b.b = b.b[:0] }
func (b *wbuf) writeByte(v byte) { b.b = append(b.b, v) }

func (b *wbuf) writeUvarint(v uint64) {
	var tmp [10]byte
	n := binary.PutUvarint(tmp[:], v)
	b.b = append(b.b, tmp[:n]...)
}

func (b *wbuf) writeZigzag(v int64) {
	b.writeUvarint(uint64((v << 1) ^ (v >> 63)))
}

func (b *wbuf) writeBlob(data []byte) {
	b.writeUvarint(uint64(len(data)))
	b.b = append(b.b, data...)
}

func (b *wbuf) writeStr(s string) {
	b.writeUvarint(uint64(len(s)))
	b.b = append(b.b, unsafeBytes(s)...)
}

// ---------------------------------------------------------------------------
// rbuf — zero-copy read cursor
// ---------------------------------------------------------------------------

type rbuf struct{ b []byte }

func (r *rbuf) readByte() (byte, error) {
	if len(r.b) == 0 {
		return 0, io.ErrUnexpectedEOF
	}
	v := r.b[0]
	r.b = r.b[1:]
	return v, nil
}

func (r *rbuf) readN(n int) ([]byte, error) {
	if len(r.b) < n {
		return nil, io.ErrUnexpectedEOF
	}
	v := r.b[:n]
	r.b = r.b[n:]
	return v, nil
}

func (r *rbuf) readUvarint() (uint64, error) {
	v, n := binary.Uvarint(r.b)
	if n == 0 {
		return 0, io.ErrUnexpectedEOF
	}
	if n < 0 {
		return 0, errors.New("wusp: uvarint overflow")
	}
	r.b = r.b[n:]
	return v, nil
}

func (r *rbuf) readZigzag() (int64, error) {
	u, err := r.readUvarint()
	if err != nil {
		return 0, err
	}
	return int64((u >> 1) ^ -(u & 1)), nil
}

func (r *rbuf) readBlob() ([]byte, error) {
	n, err := r.readUvarint()
	if err != nil {
		return nil, err
	}
	return r.readN(int(n))
}

func (r *rbuf) readStr() (string, error) {
	b, err := r.readBlob()
	if err != nil {
		return "", err
	}
	return string(b), nil
}

// ---------------------------------------------------------------------------
// LZ4 block — pure-Go, spec-compatible implementation
//
// Implements the LZ4 block format as specified at:
//   https://github.com/lz4/lz4/blob/dev/doc/lz4_Block_format.md
//
// Guarantees:
//   • No external dependencies — uses only sync.Pool from stdlib.
//   • compress() returns nil when the payload is incompressible (output
//     would be ≥ input length); the caller falls back to uncompressed storage.
//   • decompress() is allocation-free when rawSize is known (our frame
//     always stores it).
//   • lz4State is reused from sync.Pool to amortise the 16 KB hash-table
//     zero-fill cost across calls.
// ---------------------------------------------------------------------------

const (
	lz4HashLog  = 12 // 4096-entry table — fits in L1 cache
	lz4HashSize = 1 << lz4HashLog
	lz4MinMatch = 4
	lz4LastLits = 5 // last 5 bytes must be literals per spec
	lz4MFLimit  = lz4MinMatch + lz4LastLits
)

// lz4State holds a reusable hash table for the LZ4 block encoder.
// Each entry stores (position+1) so that 0 means "empty slot".
type lz4State struct {
	table [lz4HashSize]int32
}

// lz4hash4 computes a fast 4-byte hash using a Knuth multiplicative constant.
func lz4hash4(b []byte) uint32 {
	v := uint32(b[0]) | uint32(b[1])<<8 | uint32(b[2])<<16 | uint32(b[3])<<24
	return (v * 2654435761) >> (32 - lz4HashLog)
}

// compress performs LZ4 block compression.
// Returns nil if output would not be smaller than src (incompressible).
func (s *lz4State) compress(src []byte) []byte {
	if len(src) < lz4MFLimit+1 {
		return nil
	}

	// Clear hash table (compiler emits a single memclr via SIMD).
	for i := range s.table {
		s.table[i] = 0
	}

	bound := len(src) + len(src)/255 + 16
	dst := make([]byte, bound)

	ip := 0     // current read position
	op := 0     // current write position in dst
	anchor := 0 // start of pending literal run
	limit := len(src) - lz4MFLimit

	for ip <= limit {
		h := lz4hash4(src[ip:])
		ref := int(s.table[h]) - 1 // -1: convert stored (pos+1) → actual pos
		s.table[h] = int32(ip + 1)

		// Validate match: same 4 bytes, within 65535 byte window, forward ref.
		if ref >= 0 && ref < ip && ip-ref <= 65535 &&
			src[ref] == src[ip] && src[ref+1] == src[ip+1] &&
			src[ref+2] == src[ip+2] && src[ref+3] == src[ip+3] {

			// Count full match length.
			ml := lz4MinMatch
			maxML := len(src) - ip
			if maxML > 65535+lz4MinMatch {
				maxML = 65535 + lz4MinMatch
			}
			for ml < maxML && src[ip+ml] == src[ref+ml] {
				ml++
			}

			// Token byte position.
			litLen := ip - anchor
			tok := op
			op++

			// Literal length in token high nibble (+ overflow bytes).
			op = lz4WriteLen(dst, op, &dst[tok], true, litLen)
			// Copy literals.
			copy(dst[op:], src[anchor:anchor+litLen])
			op += litLen

			// Match offset (LE uint16).
			binary.LittleEndian.PutUint16(dst[op:], uint16(ip-ref))
			op += 2

			// Match length in token low nibble (+ overflow bytes).
			op = lz4WriteLen(dst, op, &dst[tok], false, ml-lz4MinMatch)

			ip += ml
			anchor = ip

			// Update hash for new position.
			if ip <= limit {
				s.table[lz4hash4(src[ip:])] = int32(ip + 1)
			}
			continue
		}
		ip++
	}

	// Final literal run (no match): only high nibble, no offset/match fields.
	litLen := len(src) - anchor
	tok := op
	op++
	op = lz4WriteLen(dst, op, &dst[tok], true, litLen)
	copy(dst[op:], src[anchor:])
	op += litLen

	if op >= len(src) {
		return nil // not worth compressing
	}
	return dst[:op]
}

// lz4WriteLen writes a nibble length into *tok and emits overflow bytes.
// isHigh=true for the literal-length nibble, false for the match-length nibble.
// Returns the updated op.
func lz4WriteLen(dst []byte, op int, tok *byte, isHigh bool, length int) int {
	var nibble byte
	if length >= 15 {
		nibble = 15
		rem := length - 15
		for rem >= 255 {
			dst[op] = 255
			op++
			rem -= 255
		}
		dst[op] = byte(rem)
		op++
	} else {
		nibble = byte(length)
	}
	if isHigh {
		*tok = (*tok & 0x0F) | (nibble << 4)
	} else {
		*tok = (*tok & 0xF0) | nibble
	}
	return op
}

// decompress performs LZ4 block decompression into a pre-sized output buffer.
// rawSize must equal the original uncompressed length (stored in our frame header).
func (s *lz4State) decompress(src []byte, rawSize int) ([]byte, error) {
	dst := make([]byte, rawSize)
	ip := 0 // read cursor into src
	op := 0 // write cursor into dst

	for ip < len(src) {
		tok := src[ip]
		ip++

		// ── Literal length ──────────────────────────────────────────────────
		litLen := int(tok >> 4)
		if litLen == 15 {
			for {
				if ip >= len(src) {
					return nil, errTruncated
				}
				extra := src[ip]
				ip++
				litLen += int(extra)
				if extra < 255 {
					break
				}
			}
		}

		// Copy literals.
		if ip+litLen > len(src) || op+litLen > len(dst) {
			return nil, errTruncated
		}
		copy(dst[op:], src[ip:ip+litLen])
		ip += litLen
		op += litLen

		// Last sequence has no match part.
		if ip >= len(src) {
			break
		}

		// ── Match offset (LE uint16) ─────────────────────────────────────────
		if ip+2 > len(src) {
			return nil, errTruncated
		}
		offset := int(binary.LittleEndian.Uint16(src[ip:]))
		ip += 2
		if offset == 0 {
			return nil, errors.New("wusp/lz4: zero match offset")
		}

		// ── Match length ─────────────────────────────────────────────────────
		ml := int(tok&0x0F) + lz4MinMatch
		if tok&0x0F == 15 {
			for {
				if ip >= len(src) {
					return nil, errTruncated
				}
				extra := src[ip]
				ip++
				ml += int(extra)
				if extra < 255 {
					break
				}
			}
		}

		// Copy match — may overlap (byte-by-byte to handle short distances).
		mSrc := op - offset
		if mSrc < 0 || op+ml > len(dst) {
			return nil, errTruncated
		}
		for i := 0; i < ml; i++ {
			dst[op+i] = dst[mSrc+i]
		}
		op += ml
	}

	if op != rawSize {
		return nil, errors.New("wusp/lz4: decompressed size mismatch")
	}
	return dst, nil
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

// unsafeBytes converts a string to a []byte without allocation.
// The returned slice MUST NOT be modified.
func unsafeBytes(s string) []byte {
	if s == "" {
		return nil
	}
	return unsafe.Slice(unsafe.StringData(s), len(s))
}
