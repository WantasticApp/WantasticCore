package wusp

import (
	"encoding/base64"
	"errors"
	"fmt"
	"math"
	"net"
	"regexp"
	"strings"
	"time"
)

// FillProfile controls how schema-derived sample values are generated.
type FillProfile string

const (
	FillProfileRealistic       FillProfile = "realistic"
	FillProfileMaxCompressible FillProfile = "max-compressible"
)

// FillOptions configures schema-driven value generation.
type FillOptions struct {
	Profile   FillProfile
	DeviceID  string
	Timestamp time.Time
	Overwrite bool
}

// ValidationError describes a fast-schema validation failure.
type ValidationError struct {
	Path   string
	Reason string
}

func (e *ValidationError) Error() string {
	if e == nil {
		return ""
	}
	if e.Path == "" {
		return "wusp: invalid message: " + e.Reason
	}
	return fmt.Sprintf("wusp: invalid %s: %s", e.Path, e.Reason)
}

type safeParamInfo struct {
	param               Param
	index               int
	expectedTag         TypeTag
	enumSet             map[string]struct{}
	pattern             *regexp.Regexp
	expectsBase64String bool
	listPathRef         bool
	listCIDR            bool
}

var (
	errNilMessage = errors.New("wusp: nil message")

	safeParamIndex = buildSafeParamIndex(AllDeviceParams)

	safeRawEncoder = NewEncoder(EncodeOptions{
		IncludeDeviceID:  true,
		IncludeTimestamp: true,
	})
	safeLZ4Encoder = NewEncoder(EncodeOptions{
		Compress:         true,
		IncludeDeviceID:  true,
		IncludeTimestamp: true,
	})
	safeDecoder = NewDecoder(DecodeOptions{
		SkipUnknownFields: true,
	})
)

// ValidateMessageFast validates a message against the registered BBF schema.
// It is intentionally strict for safety and rejects unknown paths, invalid tags,
// duplicate paths, and out-of-range values.
func ValidateMessageFast(msg *Message) error {
	if msg == nil {
		return errNilMessage
	}

	seen := make(map[string]struct{}, len(msg.Fields))
	for i := range msg.Fields {
		field := msg.Fields[i]
		if field.Path == "" {
			return &ValidationError{Reason: fmt.Sprintf("field[%d] has empty path", i)}
		}
		if _, ok := seen[field.Path]; ok {
			return &ValidationError{Path: field.Path, Reason: "duplicate field path"}
		}
		seen[field.Path] = struct{}{}

		if err := ValidateFieldFast(field); err != nil {
			return err
		}
	}
	return nil
}

// ValidateFieldFast validates a single field against the registered schema.
func ValidateFieldFast(field Field) error {
	info, canonicalPath, ok := lookupSafeParam(field.Path)
	if !ok {
		return &ValidationError{Path: field.Path, Reason: "unknown parameter path"}
	}

	if field.id != 0 {
		if canonicalPath != field.Path {
			return &ValidationError{Path: field.Path, Reason: "instance paths must be encoded inline (field id must be 0)"}
		}
		if expectedID, found := globalRegistry.IDFor(canonicalPath); found && field.id != expectedID {
			return &ValidationError{
				Path:   field.Path,
				Reason: fmt.Sprintf("field id %d does not match registry id %d", field.id, expectedID),
			}
		}
	}

	if field.Val.Tag == TagNull {
		return nil
	}
	if !tagMatchesParamType(info.param.Type, field.Val.Tag) {
		return &ValidationError{
			Path:   field.Path,
			Reason: fmt.Sprintf("tag %s does not match parameter type %s", tagName(field.Val.Tag), info.param.Type),
		}
	}

	return validateValueFast(field.Path, info, field.Val)
}

// FilledValueForPath returns a schema-derived value for one path using the
// requested fill profile. Concrete instance paths like ".1." are accepted.
func FilledValueForPath(path string, profile FillProfile) (Value, error) {
	info, _, ok := lookupSafeParam(path)
	if !ok {
		return Null(), &ValidationError{Path: path, Reason: "unknown parameter path"}
	}
	return safeFilledValueForParam(info.param, info.index, normalizeFillProfile(profile)), nil
}

// BuildFilledMessage builds a full schema message containing one value for
// every registered parameter in AllDeviceParams.
func BuildFilledMessage(opts FillOptions) (*Message, error) {
	profile := normalizeFillProfile(opts.Profile)
	msg := &Message{
		DeviceID:  opts.DeviceID,
		Timestamp: opts.Timestamp,
		Fields:    make([]Field, 0, len(AllDeviceParams)),
	}

	for i, param := range AllDeviceParams {
		id, ok := globalRegistry.IDFor(param.Path)
		if !ok || id == 0 {
			return nil, fmt.Errorf("wusp: parameter missing from registry: %s", param.Path)
		}
		msg.Fields = append(msg.Fields, Field{
			id:   id,
			Path: param.Path,
			Val:  safeFilledValueForParam(param, i, profile),
		})
	}

	if err := ValidateMessageFast(msg); err != nil {
		return nil, err
	}
	return msg, nil
}

// FillMessageValues fills missing schema fields in msg. When Overwrite is set,
// existing known fields are replaced with schema-derived values as well.
func FillMessageValues(msg *Message, opts FillOptions) error {
	if msg == nil {
		return errNilMessage
	}

	profile := normalizeFillProfile(opts.Profile)
	if opts.DeviceID != "" {
		msg.DeviceID = opts.DeviceID
	}
	if !opts.Timestamp.IsZero() {
		msg.Timestamp = opts.Timestamp
	}

	existing := make(map[string]int, len(msg.Fields))
	for i := range msg.Fields {
		existing[msg.Fields[i].Path] = i
	}

	for i, param := range AllDeviceParams {
		value := safeFilledValueForParam(param, i, profile)
		if idx, ok := existing[param.Path]; ok {
			if opts.Overwrite {
				id, _ := globalRegistry.IDFor(param.Path)
				msg.Fields[idx].id = id
				msg.Fields[idx].Path = param.Path
				msg.Fields[idx].Val = value
			}
			continue
		}

		id, _ := globalRegistry.IDFor(param.Path)
		msg.Fields = append(msg.Fields, Field{
			id:   id,
			Path: param.Path,
			Val:  value,
		})
	}

	return ValidateMessageFast(msg)
}

// EncodeMessage validates and encodes msg without LZ4 compression.
func EncodeMessage(msg *Message) ([]byte, error) {
	if err := ValidateMessageFast(msg); err != nil {
		return nil, err
	}
	return encodeMessageValidated(msg, false)
}

// EncodeMessageLZ4 validates and encodes msg with LZ4 compression.
func EncodeMessageLZ4(msg *Message) ([]byte, error) {
	if err := ValidateMessageFast(msg); err != nil {
		return nil, err
	}
	return encodeMessageValidated(msg, true)
}

// DecodeMessage decodes a frame and drops future unknown registry IDs.
// Inbound validation is skipped — agent responses may have minor type
// mismatches that would reject the entire response for one bad field.
func DecodeMessage(data []byte) (*Message, error) {
	return safeDecoder.Decode(data)
}

func encodeMessageValidated(msg *Message, compress bool) ([]byte, error) {
	if compress {
		return safeLZ4Encoder.Encode(msg)
	}
	return safeRawEncoder.Encode(msg)
}

func buildSafeParamIndex(params []Param) map[string]safeParamInfo {
	index := make(map[string]safeParamInfo, len(params))
	for i, param := range params {
		enumSet := make(map[string]struct{}, len(param.Limits.Enums))
		for _, enum := range param.Limits.Enums {
			enumSet[enum] = struct{}{}
		}

		var compiled *regexp.Regexp
		if param.Limits.Pattern != "" {
			compiled = regexp.MustCompile(param.Limits.Pattern)
		}

		lowerPath := strings.ToLower(param.Path)
		lowerDesc := strings.ToLower(param.Description)
		index[param.Path] = safeParamInfo{
			param:               param,
			index:               i,
			expectedTag:         TagForParamType(param.Type),
			enumSet:             enumSet,
			pattern:             compiled,
			expectsBase64String: param.Type == TypeString && strings.Contains(lowerDesc, "base64-encoded"),
			listPathRef:         param.Type == TypeList && strings.Contains(lowerDesc, "pathref"),
			listCIDR:            param.Type == TypeList && (strings.Contains(lowerPath, "allowedips") || strings.Contains(lowerPath, "prefix")),
		}
	}
	return index
}

func lookupSafeParam(path string) (safeParamInfo, string, bool) {
	if info, ok := safeParamIndex[path]; ok {
		return info, path, true
	}

	canonical := canonicalParamPath(path)
	info, ok := safeParamIndex[canonical]
	return info, canonical, ok
}

func canonicalParamPath(path string) string {
	if path == "" {
		return ""
	}
	parts := strings.Split(path, ".")
	for i := 0; i < len(parts); i++ {
		if parts[i] == "" || parts[i] == "{i}" {
			continue
		}
		if isNumericPathSegment(parts[i]) {
			parts[i] = "{i}"
		}
	}
	return strings.Join(parts, ".")
}

func isNumericPathSegment(part string) bool {
	if part == "" {
		return false
	}
	for i := 0; i < len(part); i++ {
		if part[i] < '0' || part[i] > '9' {
			return false
		}
	}
	return true
}

func tagMatchesParamType(paramType ParamType, tag TypeTag) bool {
	switch paramType {
	case TypeBoolean:
		return tag == TagFalse || tag == TagTrue
	default:
		return tag == TagForParamType(paramType)
	}
}

func tagName(tag TypeTag) string {
	switch tag {
	case TagNull:
		return "null"
	case TagFalse:
		return "false"
	case TagTrue:
		return "true"
	case TagUint:
		return "uint"
	case TagInt:
		return "int"
	case TagFloat:
		return "float"
	case TagString:
		return "string"
	case TagBytes:
		return "bytes"
	case TagTime:
		return "time"
	case TagIP4:
		return "ip4"
	case TagIP6:
		return "ip6"
	case TagIP4Pfx:
		return "ip4-prefix"
	case TagIP6Pfx:
		return "ip6-prefix"
	case TagMAC:
		return "mac"
	case TagList:
		return "list"
	default:
		return fmt.Sprintf("tag-%d", tag)
	}
}

func validateValueFast(path string, info safeParamInfo, value Value) error {
	param := info.param
	switch param.Type {
	case TypeBoolean:
		return nil
	case TypeUnsignedInt, TypeUnsignedLong, TypeStatsCounter:
		return validateUnsignedLimits(path, param.Limits, value.AsUint())
	case TypeInt, TypeLong:
		return validateSignedLimits(path, param.Limits, value.AsInt())
	case TypeDecimal:
		return validateDecimalLimits(path, param.Limits, value.AsFloat())
	case TypeDateTime:
		return nil
	case TypeBase64:
		return validateBinaryLimits(path, param.Limits, len(value.AsBytes()))
	case TypeHexBinary:
		return validateBinaryLimits(path, param.Limits, len(value.AsBytes()))
	case TypeIPv4Address:
		if len(value.AsBytes()) != 4 {
			return &ValidationError{Path: path, Reason: "invalid IPv4 value length"}
		}
		return nil
	case TypeIPv6Address:
		if len(value.AsBytes()) != 16 {
			return &ValidationError{Path: path, Reason: "invalid IPv6 value length"}
		}
		return nil
	case TypeIPv4Prefix:
		b := value.AsBytes()
		if len(b) != 5 || b[4] > 32 {
			return &ValidationError{Path: path, Reason: "invalid IPv4 prefix value"}
		}
		return nil
	case TypeIPv6Prefix:
		b := value.AsBytes()
		if len(b) != 17 || b[16] > 128 {
			return &ValidationError{Path: path, Reason: "invalid IPv6 prefix value"}
		}
		return nil
	case TypeMACAddress:
		if len(value.AsBytes()) != 6 {
			return &ValidationError{Path: path, Reason: "invalid MAC value length"}
		}
		return nil
	case TypeList:
		return validateListValueFast(path, info, value.AsList())
	default:
		return validateStringValueFast(path, info, value.AsString(), true)
	}
}

func validateUnsignedLimits(path string, limits Limits, value uint64) error {
	if limits.Min != nil && value < uint64(*limits.Min) {
		return &ValidationError{Path: path, Reason: fmt.Sprintf("value %d is below minimum %d", value, *limits.Min)}
	}
	if limits.Max != nil && value > uint64(*limits.Max) {
		return &ValidationError{Path: path, Reason: fmt.Sprintf("value %d is above maximum %d", value, *limits.Max)}
	}
	return nil
}

func validateSignedLimits(path string, limits Limits, value int64) error {
	if limits.Min != nil && value < *limits.Min {
		return &ValidationError{Path: path, Reason: fmt.Sprintf("value %d is below minimum %d", value, *limits.Min)}
	}
	if limits.Max != nil && value > *limits.Max {
		return &ValidationError{Path: path, Reason: fmt.Sprintf("value %d is above maximum %d", value, *limits.Max)}
	}
	return nil
}

func validateDecimalLimits(path string, limits Limits, value float64) error {
	if math.IsNaN(value) || math.IsInf(value, 0) {
		return &ValidationError{Path: path, Reason: "decimal value must be finite"}
	}
	if limits.MinF != nil && value < *limits.MinF {
		return &ValidationError{Path: path, Reason: fmt.Sprintf("value %f is below minimum %f", value, *limits.MinF)}
	}
	if limits.MaxF != nil && value > *limits.MaxF {
		return &ValidationError{Path: path, Reason: fmt.Sprintf("value %f is above maximum %f", value, *limits.MaxF)}
	}
	if limits.Min != nil && value < float64(*limits.Min) {
		return &ValidationError{Path: path, Reason: fmt.Sprintf("value %f is below minimum %d", value, *limits.Min)}
	}
	if limits.Max != nil && value > float64(*limits.Max) {
		return &ValidationError{Path: path, Reason: fmt.Sprintf("value %f is above maximum %d", value, *limits.Max)}
	}
	return nil
}

func validateBinaryLimits(path string, limits Limits, rawLen int) error {
	if limits.MinLength > 0 && rawLen < limits.MinLength {
		return &ValidationError{Path: path, Reason: fmt.Sprintf("raw length %d is below minimum %d", rawLen, limits.MinLength)}
	}
	if limits.MaxLength > 0 && rawLen > limits.MaxLength {
		return &ValidationError{Path: path, Reason: fmt.Sprintf("raw length %d is above maximum %d", rawLen, limits.MaxLength)}
	}
	return nil
}

func validateStringValueFast(path string, info safeParamInfo, value string, checkLength bool) error {
	if checkLength {
		if info.param.Limits.MinLength > 0 && len(value) < info.param.Limits.MinLength {
			return &ValidationError{
				Path:   path,
				Reason: fmt.Sprintf("length %d is below minimum %d", len(value), info.param.Limits.MinLength),
			}
		}
		if info.param.Limits.MaxLength > 0 && len(value) > info.param.Limits.MaxLength {
			return &ValidationError{
				Path:   path,
				Reason: fmt.Sprintf("length %d is above maximum %d", len(value), info.param.Limits.MaxLength),
			}
		}
	}

	if len(info.enumSet) > 0 {
		if _, ok := info.enumSet[value]; !ok {
			return &ValidationError{Path: path, Reason: fmt.Sprintf("value %q is not in the allowed enum set", value)}
		}
	}
	if info.pattern != nil && !info.pattern.MatchString(value) {
		return &ValidationError{Path: path, Reason: fmt.Sprintf("value %q does not match %q", value, info.param.Limits.Pattern)}
	}
	if info.expectsBase64String && value != "" {
		if _, err := base64.StdEncoding.DecodeString(value); err != nil {
			return &ValidationError{Path: path, Reason: "value is not valid base64"}
		}
	}
	return nil
}

func validateListValueFast(path string, info safeParamInfo, items []Value) error {
	if info.param.Limits.MinItems > 0 && len(items) < info.param.Limits.MinItems {
		return &ValidationError{
			Path:   path,
			Reason: fmt.Sprintf("list has %d items, below minimum %d", len(items), info.param.Limits.MinItems),
		}
	}
	if info.param.Limits.MaxItems > 0 && len(items) > info.param.Limits.MaxItems {
		return &ValidationError{
			Path:   path,
			Reason: fmt.Sprintf("list has %d items, above maximum %d", len(items), info.param.Limits.MaxItems),
		}
	}

	totalStringLen := 0
	allStrings := true
	for i, item := range items {
		if item.Tag == TagNull || item.Tag == TagList {
			return &ValidationError{Path: path, Reason: fmt.Sprintf("list item %d has unsupported tag %s", i, tagName(item.Tag))}
		}
		if item.Tag != TagString {
			allStrings = false
			if len(info.enumSet) > 0 || info.pattern != nil || info.listPathRef || info.listCIDR {
				return &ValidationError{Path: path, Reason: fmt.Sprintf("list item %d must be a string", i)}
			}
			continue
		}

		s := item.AsString()
		totalStringLen += len(s)
		if i > 0 {
			totalStringLen++
		}
		if err := validateStringValueFast(path, info, s, false); err != nil {
			return err
		}
		if info.listPathRef && s == "" {
			return &ValidationError{Path: path, Reason: fmt.Sprintf("list item %d must not be empty", i)}
		}
		if info.listCIDR && s != "" {
			if _, _, err := net.ParseCIDR(s); err != nil {
				return &ValidationError{Path: path, Reason: fmt.Sprintf("list item %d is not a valid CIDR", i)}
			}
		}
	}

	if allStrings {
		if info.param.Limits.MinLength > 0 && totalStringLen < info.param.Limits.MinLength {
			return &ValidationError{
				Path:   path,
				Reason: fmt.Sprintf("list length %d is below minimum %d", totalStringLen, info.param.Limits.MinLength),
			}
		}
		if info.param.Limits.MaxLength > 0 && totalStringLen > info.param.Limits.MaxLength {
			return &ValidationError{
				Path:   path,
				Reason: fmt.Sprintf("list length %d is above maximum %d", totalStringLen, info.param.Limits.MaxLength),
			}
		}
	}

	return nil
}

func normalizeFillProfile(profile FillProfile) FillProfile {
	switch profile {
	case "", FillProfileRealistic:
		return FillProfileRealistic
	case FillProfileMaxCompressible:
		return FillProfileMaxCompressible
	default:
		return FillProfileRealistic
	}
}

func safeFilledValueForParam(param Param, index int, profile FillProfile) Value {
	var value Value
	switch profile {
	case FillProfileMaxCompressible:
		value = safeMaxCompressibleValue(param, index)
	default:
		value = safeRealisticValue(param, index)
	}
	if err := ValidateFieldFast(Field{Path: param.Path, Val: value}); err != nil {
		return Null()
	}
	return value
}

func safeRealisticValue(param Param, index int) Value {
	switch param.Type {
	case TypeBoolean:
		return Bool(true)
	case TypeUnsignedInt:
		return Uint(uint64(safeMaxUnsignedForParam(param, 32)))
	case TypeUnsignedLong, TypeStatsCounter:
		return Uint(safeMaxUnsignedForParam(param, 64))
	case TypeInt:
		return Int(safeMaxSignedForParam(param, 32))
	case TypeLong:
		return Int(safeMaxSignedForParam(param, 64))
	case TypeDecimal:
		return Float(safeMaxDecimalForParam(param))
	case TypeDateTime:
		return Time(time.Unix(253402300799, int64(index%1_000_000_000)).UTC())
	case TypeBase64, TypeHexBinary:
		return Bytes(safeMaxBinaryValueForParam(param, index))
	case TypeIPv4Address:
		return IP4(net.IPv4(255, 255, 255, byte(1+(index%253))))
	case TypeIPv6Address:
		return IP6(net.ParseIP(fmt.Sprintf("ffff:ffff:ffff:ffff:ffff:ffff:ffff:%x", 1+(index%65534))))
	case TypeIPv4Prefix:
		_, n, _ := net.ParseCIDR(fmt.Sprintf("203.%d.%d.0/24", index%255, (index*7)%255))
		return IP4Prefix(n)
	case TypeIPv6Prefix:
		_, n, _ := net.ParseCIDR(fmt.Sprintf("2001:db8:%x:%x::/64", index%65535, (index*11)%65535))
		return IP6Prefix(n)
	case TypeMACAddress:
		return MAC(net.HardwareAddr{0x02, byte(index >> 16), byte(index >> 8), byte(index), 0xaa, 0xbb})
	case TypeList:
		return safeMaxListValueForParam(param, index)
	default:
		return String(safeMaxStringValueForParam(param, index))
	}
}

func safeMaxCompressibleValue(param Param, index int) Value {
	switch param.Type {
	case TypeBoolean:
		return Bool(true)
	case TypeUnsignedInt:
		return Uint(uint64(safeMaxUnsignedForParam(param, 32)))
	case TypeUnsignedLong, TypeStatsCounter:
		return Uint(safeMaxUnsignedForParam(param, 64))
	case TypeInt:
		return Int(safeMaxSignedForParam(param, 32))
	case TypeLong:
		return Int(safeMaxSignedForParam(param, 64))
	case TypeDecimal:
		return Float(safeMaxDecimalForParam(param))
	case TypeDateTime:
		return Time(time.Unix(253402300799, 0).UTC())
	case TypeBase64:
		return Bytes(safeRepeatedBytes(safeMaxBinaryLengthForParam(param), 0x41))
	case TypeHexBinary:
		return Bytes(safeRepeatedBytes(safeMaxBinaryLengthForParam(param), 0x42))
	case TypeIPv4Address:
		return IP4(net.IPv4(255, 255, 255, 255))
	case TypeIPv6Address:
		return IP6(net.ParseIP("ffff:ffff:ffff:ffff:ffff:ffff:ffff:ffff"))
	case TypeIPv4Prefix:
		_, n, _ := net.ParseCIDR("255.255.255.0/24")
		return IP4Prefix(n)
	case TypeIPv6Prefix:
		_, n, _ := net.ParseCIDR("ffff:ffff:ffff:ffff::/64")
		return IP6Prefix(n)
	case TypeMACAddress:
		return MAC(net.HardwareAddr{0xaa, 0xaa, 0xaa, 0xaa, 0xaa, 0xaa})
	case TypeList:
		return safeMaxCompressibleListValue(param)
	default:
		return String(safeMaxCompressibleStringValue(param, index))
	}
}

func safeMaxUnsignedForParam(param Param, bits int) uint64 {
	if param.Limits.Max != nil {
		return uint64(*param.Limits.Max)
	}
	if bits == 32 {
		return math.MaxUint32
	}
	return math.MaxUint64
}

func safeMaxSignedForParam(param Param, bits int) int64 {
	if param.Limits.Max != nil {
		return *param.Limits.Max
	}
	if bits == 32 {
		return math.MaxInt32
	}
	return math.MaxInt64
}

func safeMaxDecimalForParam(param Param) float64 {
	if param.Limits.MaxF != nil {
		return *param.Limits.MaxF
	}
	if param.Limits.Max != nil {
		return float64(*param.Limits.Max)
	}
	return 999999.999999
}

func safeMaxBinaryValueForParam(param Param, index int) []byte {
	return safePatternBytes(param.Path, index, safeMaxBinaryLengthForParam(param))
}

func safeMaxBinaryLengthForParam(param Param) int {
	target := 128
	switch {
	case param.Type == TypeHexBinary && param.Limits.MaxLength > 0:
		target = safeIntMax(1, param.Limits.MaxLength)
	case param.Type == TypeBase64 && param.Limits.MaxLength > 0:
		target = safeIntMax(1, param.Limits.MaxLength)
	case strings.Contains(strings.ToLower(param.Path), "privatekey"),
		strings.Contains(strings.ToLower(param.Path), "publickey"),
		strings.Contains(strings.ToLower(param.Path), "presharedkey"):
		target = 32
	case strings.Contains(strings.ToLower(param.Path), ".instruction"):
		target = 1024
	case strings.Contains(strings.ToLower(param.Path), ".value"):
		target = 256
	}
	return target
}

func safeMaxStringValueForParam(param Param, index int) string {
	if len(param.Limits.Enums) > 0 {
		return safeLongestString(param.Limits.Enums)
	}

	if param.Path == "Device.RootDataModelVersion" {
		return BroadbandRootDataModelVersion
	}

	switch {
	case strings.Contains(strings.ToLower(param.Description), "base64-encoded"):
		return base64.StdEncoding.EncodeToString(safeMaxBinaryValueForParam(param, index))
	case param.Type == TypeAlias:
		return safeFitString("alias-"+safeSanitizeSeed(param.Path), safeStringLengthTarget(param.Limits, 64))
	case param.Type == TypePathRef || strings.Contains(strings.ToLower(param.Description), "pathref"):
		sample := safeSamplePathRef(param.Path)
		return safeFitString(sample, safeStringLengthTarget(param.Limits, len(sample)))
	case strings.HasSuffix(param.Path, "URL"):
		return safeFitString(safeSampleURL(param.Path), safeStringLengthTarget(param.Limits, 256))
	case param.Limits.Pattern == `^[0-9A-F]{6}$` || param.Limits.Pattern == `[0-9A-F]{6}`:
		return "ABCDEF"
	case param.Limits.Pattern == `[r\-][w\-][x\-][n\-]`:
		return "rwxn"
	case strings.Contains(param.Limits.Pattern, `^[a-zA-Z0-9\-\.]*$`):
		return safeFitString("host-"+safeSanitizeSeed(param.Path), safeStringLengthTarget(param.Limits, 96))
	default:
		return safePatternAwareString(param, safeFitString(safeSanitizeSeed(param.Path), safeStringLengthTarget(param.Limits, 96)))
	}
}

func safeMaxListValueForParam(param Param, index int) Value {
	if len(param.Limits.Enums) > 0 {
		count := len(param.Limits.Enums)
		if param.Limits.MaxItems > 0 && count > param.Limits.MaxItems {
			count = param.Limits.MaxItems
		}
		items := make([]Value, 0, count)
		for _, enum := range param.Limits.Enums[:count] {
			items = append(items, String(enum))
		}
		return List(items...)
	}

	count := param.Limits.MaxItems
	if count == 0 {
		switch {
		case param.Limits.MaxLength >= 65535:
			count = 64
		case param.Limits.MaxLength >= 1024:
			count = 32
		case param.Limits.MaxLength >= 256:
			count = 12
		default:
			count = 4
		}
	}
	if count < 1 {
		count = 1
	}

	items := make([]Value, 0, count)
	for i := 0; i < count; i++ {
		items = append(items, String(safeListItemValue(param, index, i, count)))
	}
	return List(items...)
}

func safeMaxCompressibleListValue(param Param) Value {
	if len(param.Limits.Enums) > 0 {
		count := len(param.Limits.Enums)
		if param.Limits.MaxItems > 0 && count > param.Limits.MaxItems {
			count = param.Limits.MaxItems
		}
		item := safeLongestString(param.Limits.Enums)
		items := make([]Value, 0, count)
		for i := 0; i < count; i++ {
			items = append(items, String(item))
		}
		return List(items...)
	}

	count := param.Limits.MaxItems
	if count == 0 {
		switch {
		case param.Limits.MaxLength >= 65535:
			count = 64
		case param.Limits.MaxLength >= 1024:
			count = 32
		case param.Limits.MaxLength >= 256:
			count = 12
		default:
			count = 4
		}
	}
	if count < 1 {
		count = 1
	}

	item := safeCompressibleListItemValue(param, count)
	items := make([]Value, 0, count)
	for i := 0; i < count; i++ {
		items = append(items, String(item))
	}
	return List(items...)
}

func safeListItemValue(param Param, index, itemIndex, itemCount int) string {
	lowerPath := strings.ToLower(param.Path)
	lowerDesc := strings.ToLower(param.Description)

	switch {
	case strings.Contains(lowerPath, "allowedips"):
		pool := []string{"0.0.0.0/0", "::/0", "10.0.0.0/8", "192.168.0.0/16", "2001:db8::/32", "fd00::/8"}
		return pool[itemIndex%len(pool)]
	case strings.Contains(lowerPath, "prefix"):
		pool := []string{"203.0.113.0/24", "198.51.100.0/24", "2001:db8::/48", "fd00::/64"}
		return pool[itemIndex%len(pool)]
	case strings.Contains(lowerDesc, "pathref"):
		return safeSamplePathRef(param.Path)
	default:
		target := 32
		if param.Limits.MaxLength > 0 {
			target = safeIntMax(1, (param.Limits.MaxLength-safeIntMax(0, itemCount-1))/itemCount)
		}
		return safePatternAwareString(param, safeFitString(fmt.Sprintf("%s-%03d", safeSanitizeSeed(param.Path), index+itemIndex), target))
	}
}

func safeCompressibleListItemValue(param Param, itemCount int) string {
	lowerPath := strings.ToLower(param.Path)
	lowerDesc := strings.ToLower(param.Description)

	switch {
	case strings.Contains(lowerPath, "allowedips"):
		return "0.0.0.0/0"
	case strings.Contains(lowerPath, "prefix"):
		return "255.255.255.0/24"
	case strings.Contains(lowerDesc, "pathref"):
		return safeSamplePathRef(param.Path)
	default:
		target := 32
		if param.Limits.MaxLength > 0 {
			target = safeIntMax(1, (param.Limits.MaxLength-safeIntMax(0, itemCount-1))/itemCount)
		}
		return safePatternAwareString(param, strings.Repeat("A", target))
	}
}

func safeDecodedBase64Len(maxChars int) int {
	if maxChars <= 0 {
		return 0
	}
	return (maxChars / 4) * 3
}

func safeStringLengthTarget(limits Limits, fallback int) int {
	target := fallback
	if limits.MaxLength > 0 {
		target = limits.MaxLength
	}
	if limits.MinLength > 0 && target < limits.MinLength {
		target = limits.MinLength
	}
	if target < 1 {
		target = 1
	}
	return target
}

func safeSamplePathRef(path string) string {
	switch {
	case strings.Contains(path, "WireGuard"):
		return "Device.WireGuard.Peer.1."
	case strings.Contains(path, "ManagementServer"):
		return "Device.ManagementServer.ManageableDevice.1."
	case strings.Contains(path, "WUSP.ControllerTrust.Role"), strings.Contains(path, "LocalAgent.ControllerTrust.Role"):
		return "Device.WUSP.ControllerTrust.Role.1."
	case strings.Contains(path, "WUSP.Certificate"), strings.Contains(path, "LocalAgent.Certificate"):
		return "Device.WUSP.Certificate.1."
	default:
		return "Device.IP.Interface.1."
	}
}

func safeSampleURL(path string) string {
	host := strings.ToLower(strings.ReplaceAll(safeSanitizeSeed(path), ".", "-"))
	host = strings.Trim(host, "-")
	if host == "" {
		host = "wantastic"
	}
	return "https://" + host + ".example.net/" + safeSanitizeSeed(path)
}

func safeMaxCompressibleStringValue(param Param, index int) string {
	if len(param.Limits.Enums) > 0 {
		return safeLongestString(param.Limits.Enums)
	}

	if param.Path == "Device.RootDataModelVersion" {
		return BroadbandRootDataModelVersion
	}

	switch {
	case strings.Contains(strings.ToLower(param.Description), "base64-encoded"):
		return base64.StdEncoding.EncodeToString(safeRepeatedBytes(safeMaxBinaryLengthForParam(param), 0x41))
	case param.Type == TypeAlias:
		return strings.Repeat("A", safeStringLengthTarget(param.Limits, 64))
	case param.Type == TypePathRef || strings.Contains(strings.ToLower(param.Description), "pathref"):
		return safeSamplePathRef(param.Path)
	case strings.HasSuffix(param.Path, "URL"):
		return safeFitString("https://aaaaaaaa.example.net/aaaaaaaa", safeStringLengthTarget(param.Limits, 256))
	case param.Limits.Pattern == `^[0-9A-F]{6}$` || param.Limits.Pattern == `[0-9A-F]{6}`:
		return "AAAAAA"
	case param.Limits.Pattern == `[r\-][w\-][x\-][n\-]`:
		return "rwxn"
	case strings.Contains(param.Limits.Pattern, `^[a-zA-Z0-9\-\.]*$`):
		return strings.Repeat("a", safeStringLengthTarget(param.Limits, 96))
	default:
		_ = index
		return safePatternAwareString(param, strings.Repeat("A", safeStringLengthTarget(param.Limits, 96)))
	}
}

func safePatternAwareString(param Param, fallback string) string {
	pattern := strings.TrimSpace(param.Limits.Pattern)
	if pattern == "" {
		return fallback
	}
	re, err := regexp.Compile(pattern)
	if err != nil {
		return fallback
	}
	if safePatternCandidateValid(re, param.Limits, fallback) {
		return fallback
	}

	for _, candidate := range []string{
		BroadbandRootDataModelVersion,
		"2.20.1",
		"1 Firmware Upgrade Image",
		"2 Web Content",
		"3 Vendor Configuration File",
		"4 Vendor Log File",
		"X ABCDEF VendorType",
		"00000000-0000-0000-0000-000000000000",
		"ABCDEF",
		"rwxn",
		"host.example",
		"https://example.net/",
		"Device.WUSP.Certificate.1.",
		"Device.IP.Interface.1.",
		"Enabled",
		"Success",
		"1",
		"A",
	} {
		if !safeStringWithinLimits(param.Limits, candidate) {
			continue
		}
		if re.MatchString(candidate) {
			return candidate
		}
	}

	return fallback
}

func safePatternCandidateValid(re *regexp.Regexp, limits Limits, value string) bool {
	return re != nil && safeStringWithinLimits(limits, value) && re.MatchString(value)
}

func safeStringWithinLimits(limits Limits, value string) bool {
	if limits.MinLength > 0 && len(value) < limits.MinLength {
		return false
	}
	if limits.MaxLength > 0 && len(value) > limits.MaxLength {
		return false
	}
	return true
}

func safeSanitizeSeed(seed string) string {
	var b strings.Builder
	b.Grow(len(seed))
	for i := 0; i < len(seed); i++ {
		c := seed[i]
		switch {
		case c >= 'a' && c <= 'z':
			b.WriteByte(c)
		case c >= 'A' && c <= 'Z':
			b.WriteByte(c + ('a' - 'A'))
		case c >= '0' && c <= '9':
			b.WriteByte(c)
		default:
			b.WriteByte('x')
		}
	}
	return b.String()
}

func safeFitString(seed string, length int) string {
	if length <= 0 {
		return ""
	}
	base := seed
	if base == "" {
		base = "wusp"
	}
	if len(base) >= length {
		return base[:length]
	}
	var b strings.Builder
	b.Grow(length)
	for b.Len() < length {
		remaining := length - b.Len()
		if remaining >= len(base) {
			b.WriteString(base)
		} else {
			b.WriteString(base[:remaining])
		}
	}
	return b.String()
}

func safePatternBytes(seed string, index, length int) []byte {
	if length <= 0 {
		return nil
	}
	base := []byte(safeSanitizeSeed(seed))
	if len(base) == 0 {
		base = []byte("wusp")
	}
	out := make([]byte, length)
	for i := range out {
		out[i] = base[(i+index)%len(base)] ^ byte((i*31+index)%251)
	}
	return out
}

func safeRepeatedBytes(length int, value byte) []byte {
	if length <= 0 {
		return nil
	}
	out := make([]byte, length)
	for i := range out {
		out[i] = value
	}
	return out
}

func safeLongestString(values []string) string {
	longest := ""
	for _, value := range values {
		if len(value) > len(longest) {
			longest = value
		}
	}
	return longest
}

func safeIntMax(a, b int) int {
	if a > b {
		return a
	}
	return b
}
