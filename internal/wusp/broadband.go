package wusp

import (
	"strconv"
	"strings"
)

const (
	// BroadbandTR181ModelVersion is the BBF full-model snapshot used by the
	// imported runtime Device.* schema.
	BroadbandTR181ModelVersion = "2.20.1"

	// BroadbandRootDataModelVersion is the major.minor value exposed by
	// Device.RootDataModelVersion.
	BroadbandRootDataModelVersion = "2.20"

	BroadbandTR181Source     = "BroadbandForum/cwmp-data-models tr-181-2-20-1-cwmp-full.xml"
	BroadbandTR181SourceURL  = "https://github.com/BroadbandForum/cwmp-data-models/blob/main/tr-181-2-20-1-cwmp-full.xml"
	BroadbandWireGuardSource = "BroadbandForum/device-data-model tr-181-2-wireguard.xml"
)

// BroadbandDataModel describes one source data-model slice bundled into WUSP.
// ModelVersion is the source snapshot version. FirstVersion and LatestVersion
// describe the per-object/per-parameter SinceVersion range covered by that
// slice in this package.
type BroadbandDataModel struct {
	ID            string
	Name          string
	ModelVersion  string
	FirstVersion  string
	LatestVersion string
	Source        string
	SourceURL     string
	ObjectCount   int
	ParamCount    int
}

var allImportedDeviceParams = cloneParams(runtimeDeviceParams)

// AllDeviceObjects is the full runtime schema object registry consumed by the
// WUSP supported-data-model surface.
var AllDeviceObjects = concatObjects(
	DeviceObjects,
	WireGuardObjects,
	WUSPObjects,
)

// AllDeviceParams is the full WUSP schema registry consumed by the encoder and
// tests. It stays centralized here so the package has one authoritative
// aggregation point.
var AllDeviceParams = concat(
	allImportedDeviceParams,
	AllWireGuardParams,
	AllWUSPParams,
)

// BroadbandDataModels collects the runtime model slices bundled into WUSP.
var BroadbandDataModels = []BroadbandDataModel{
	newBroadbandDataModel(
		"device",
		"TR-181 Device (CWMP Full Import)",
		BroadbandTR181ModelVersion,
		BroadbandTR181Source,
		BroadbandTR181SourceURL,
		DeviceObjects,
		allImportedDeviceParams,
	),
	newBroadbandDataModel(
		"wireguard",
		"TR-181 WireGuard",
		BroadbandTR181ModelVersion,
		BroadbandWireGuardSource,
		BroadbandTR181SourceURL,
		WireGuardObjects,
		AllWireGuardParams,
	),
	newBroadbandDataModel(
		"wusp",
		"Wantastic WUSP",
		WUSPModelVersion,
		WUSPSource,
		WUSPSourceURL,
		WUSPObjects,
		AllWUSPParams,
	),
}

var runtimeDeviceModel = NewDevice(ImportedModelSummary{
	ID:           "runtime-device",
	FileName:     "runtime-device",
	Name:         "Device",
	ModelVersion: BroadbandTR181ModelVersion,
	Source:       "Wantastic runtime device model",
	SourceURL:    BroadbandTR181SourceURL,
	ObjectCount:  len(AllDeviceObjects),
	ParamCount:   len(AllDeviceParams),
}, AllDeviceObjects, AllDeviceParams)

// RuntimeDevice returns the canonical Wantastic Device model, rooted at
// "device.", with the imported BBF schema plus the runtime WireGuard and WUSP
// extensions merged in.
func RuntimeDevice() *Device {
	return runtimeDeviceModel.Clone()
}

func runtimeDeviceFast() *Device {
	return runtimeDeviceModel
}

func LookupBroadbandDataModel(id string) (BroadbandDataModel, bool) {
	for _, model := range BroadbandDataModels {
		if model.ID == id {
			return model, true
		}
	}
	return BroadbandDataModel{}, false
}

func newBroadbandDataModel(id, name, modelVersion, source, sourceURL string, objects []Object, params []Param) BroadbandDataModel {
	first, latest := versionRange(objects, params)
	return BroadbandDataModel{
		ID:            id,
		Name:          name,
		ModelVersion:  normalizeModelVersion(modelVersion),
		FirstVersion:  first,
		LatestVersion: latest,
		Source:        source,
		SourceURL:     sourceURL,
		ObjectCount:   len(objects),
		ParamCount:    len(params),
	}
}

func versionRange(objects []Object, params []Param) (string, string) {
	var first, latest string
	for _, object := range objects {
		first, latest = extendVersionRange(first, latest, object.SinceVersion)
	}
	for _, param := range params {
		first, latest = extendVersionRange(first, latest, param.SinceVersion)
	}
	return first, latest
}

func extendVersionRange(first, latest, value string) (string, string) {
	value = normalizeModelVersion(value)
	if value == "" {
		return first, latest
	}
	if first == "" || compareModelVersion(value, first) < 0 {
		first = value
	}
	if latest == "" || compareModelVersion(value, latest) > 0 {
		latest = value
	}
	return first, latest
}

func normalizeModelVersion(value string) string {
	value = strings.TrimSpace(value)
	value = strings.TrimPrefix(value, "v")
	value = strings.TrimPrefix(value, "V")
	return value
}

func compareModelVersion(a, b string) int {
	a = normalizeModelVersion(a)
	b = normalizeModelVersion(b)
	if a == b {
		return 0
	}
	aParts := strings.Split(a, ".")
	bParts := strings.Split(b, ".")
	width := len(aParts)
	if len(bParts) > width {
		width = len(bParts)
	}
	for i := 0; i < width; i++ {
		aPart := 0
		if i < len(aParts) {
			aPart = parseModelVersionPart(aParts[i])
		}
		bPart := 0
		if i < len(bParts) {
			bPart = parseModelVersionPart(bParts[i])
		}
		if aPart < bPart {
			return -1
		}
		if aPart > bPart {
			return 1
		}
	}
	if a < b {
		return -1
	}
	return 1
}

func parseModelVersionPart(value string) int {
	n, err := strconv.Atoi(value)
	if err != nil {
		return 0
	}
	return n
}

// iptr returns a pointer to the given int64 value. Used in Limits.Min/Max.
func iptr(v int64) *int64 { return &v }

// fptr returns a pointer to the given float64 value. Used in Limits.MinF/MaxF.
func fptr(v float64) *float64 { return &v }

// concat merges multiple []Param slices into one.
func concat(slices ...[]Param) []Param {
	n := 0
	for _, s := range slices {
		n += len(s)
	}
	out := make([]Param, 0, n)
	for _, s := range slices {
		out = append(out, s...)
	}
	return out
}

func concatObjects(slices ...[]Object) []Object {
	n := 0
	for _, s := range slices {
		n += len(s)
	}
	out := make([]Object, 0, n)
	for _, s := range slices {
		out = append(out, s...)
	}
	return out
}
