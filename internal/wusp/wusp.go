package wusp

const (
	WUSPModelVersion = "1.0.0"
	WUSPSource       = "Wantastic WUSP over WireGuard"
	WUSPSourceURL    = "https://github.com/wantastic/wantasticd"
)

// WUSPObjects describes the custom Wantastic transport surface that rides
// inside the WireGuard control plane.
var WUSPObjects = []Object{
	{
		Path:         "Device.WUSP.",
		SinceVersion: "1.0",
		Description:  "Wantastic WireGuard USP transport configuration and capabilities.",
	},
	{
		Path:          "Device.WUSP.Request.{i}.",
		MultiInstance: true,
		SinceVersion:  "1.0",
		Description:   "Wantastic in-tunnel operation request table.",
	},
	{
		Path:          "Device.WUSP.Subscription.{i}.",
		MultiInstance: true,
		SinceVersion:  "1.0",
		Description:   "Wantastic in-tunnel notification subscription table.",
	},
	{
		Path:          "Device.WUSP.Certificate.{i}.",
		MultiInstance: true,
		SinceVersion:  "1.0",
		Description:   "Certificates used by the Wantastic in-tunnel controller trust model.",
	},
	{
		Path:          "Device.WUSP.ControllerTrust.Role.{i}.",
		MultiInstance: true,
		SinceVersion:  "1.0",
		Description:   "Named trust roles used by the Wantastic in-tunnel controller model.",
	},
}

// AllWUSPParams exposes the custom WUSP transport capabilities alongside the
// imported BBF Device model.
var AllWUSPParams = []Param{
	{
		Path:         "Device.WUSP.Enable",
		Type:         TypeBoolean,
		Access:       ReadWrite,
		SinceVersion: "1.0",
		Description:  "Enables or disables the Wantastic in-tunnel USP transport.",
	},
	{
		Path:         "Device.WUSP.Status",
		Type:         TypeString,
		Access:       ReadOnly,
		SinceVersion: "1.0",
		Description:  "Current operational status of the Wantastic USP transport.",
		Limits:       Limits{Enums: []string{"Dormant", "Active", "Error"}},
	},
	{
		Path:         "Device.WUSP.ProtocolVersion",
		Type:         TypeString,
		Access:       ReadOnly,
		SinceVersion: "1.0",
		Description:  "Version string for the Wantastic USP control and transfer framing.",
		Limits:       Limits{MaxLength: 32},
	},
	{
		Path:         "Device.WUSP.ControllerEndpointID",
		Type:         TypeString,
		Access:       ReadWrite,
		SinceVersion: "1.0",
		Description:  "Controller endpoint identifier this agent expects for WUSP control messages.",
		Limits:       Limits{MaxLength: 256},
	},
	{
		Path:         "Device.WUSP.ControllerPublicKey",
		Type:         TypeString,
		Access:       ReadWrite,
		SinceVersion: "1.0",
		Description:  "WireGuard public key authorized to act as the WUSP controller.",
		Limits:       Limits{MaxLength: 128},
	},
	{
		Path:         "Device.WUSP.MaxControlPayload",
		Type:         TypeUnsignedInt,
		Access:       ReadOnly,
		SinceVersion: "1.0",
		Description:  "Maximum recommended unfragmented WUSP control payload size in bytes.",
		Limits:       Limits{Min: iptr(256), Max: iptr(65535)},
	},
	{
		Path:         "Device.WUSP.ControlCompression",
		Type:         TypeList,
		Access:       ReadOnly,
		SinceVersion: "1.0",
		Description:  "Compression methods supported for WUSP control payloads.",
	},
	{
		Path:         "Device.WUSP.TransferCompression",
		Type:         TypeList,
		Access:       ReadOnly,
		SinceVersion: "1.0",
		Description:  "Compression methods supported for WUSP streamed uploads and downloads.",
	},
	{
		Path:         "Device.WUSP.RecommendedChunkSize",
		Type:         TypeUnsignedInt,
		Access:       ReadOnly,
		SinceVersion: "1.0",
		Description:  "Recommended stream chunk size in bytes for in-tunnel uploads and downloads.",
		Limits:       Limits{Min: iptr(256), Max: iptr(65535)},
	},
	{
		Path:         "Device.WUSP.TransferWindowSize",
		Type:         TypeUnsignedInt,
		Access:       ReadOnly,
		SinceVersion: "1.0",
		Description:  "Recommended maximum number of in-flight WUSP transfer chunks.",
		Limits:       Limits{Min: iptr(1), Max: iptr(1024)},
	},
	{
		Path:         "Device.WUSP.TunnelOnly",
		Type:         TypeBoolean,
		Access:       ReadOnly,
		SinceVersion: "1.0",
		Description:  "Always true for Wantastic WUSP because the transport is fully encapsulated inside the WireGuard tunnel.",
	},
	{
		Path:         "Device.WUSP.ReliableControl",
		Type:         TypeBoolean,
		Access:       ReadOnly,
		SinceVersion: "1.0",
		Description:  "Indicates that WUSP control messages use explicit reliability above the packet layer.",
	},
	{
		Path:         "Device.WUSP.RequestNumberOfEntries",
		Type:         TypeUnsignedInt,
		Access:       ReadOnly,
		SinceVersion: "1.0",
		Description:  "Number of entries in Device.WUSP.Request.{i}.",
	},
	{
		Path:         "Device.WUSP.SubscriptionNumberOfEntries",
		Type:         TypeUnsignedInt,
		Access:       ReadOnly,
		SinceVersion: "1.0",
		Description:  "Number of entries in Device.WUSP.Subscription.{i}.",
	},
	{
		Path:         "Device.WUSP.CertificateNumberOfEntries",
		Type:         TypeUnsignedInt,
		Access:       ReadOnly,
		SinceVersion: "1.0",
		Description:  "Number of entries in Device.WUSP.Certificate.{i}.",
	},
	{
		Path:         "Device.WUSP.ControllerTrustRoleNumberOfEntries",
		Type:         TypeUnsignedInt,
		Access:       ReadOnly,
		SinceVersion: "1.0",
		Description:  "Number of entries in Device.WUSP.ControllerTrust.Role.{i}.",
	},
	{
		Path:         "Device.WUSP.Request.{i}.Alias",
		Type:         TypeAlias,
		Access:       ReadWrite,
		SinceVersion: "1.0",
		Description:  "Non-functional unique key for a Wantastic request row.",
		Limits:       Limits{MinLength: 1, MaxLength: 64},
	},
	{
		Path:         "Device.WUSP.Request.{i}.Command",
		Type:         TypeString,
		Access:       ReadWrite,
		SinceVersion: "1.0",
		Description:  "Command or operation name issued over Wantastic WUSP.",
		Limits:       Limits{MaxLength: 256},
	},
	{
		Path:         "Device.WUSP.Request.{i}.Status",
		Type:         TypeString,
		Access:       ReadOnly,
		SinceVersion: "1.0",
		Description:  "Completion status of a Wantastic WUSP request row.",
		Limits:       Limits{Enums: []string{"Requested", "Running", "Success", "Error"}},
	},
	{
		Path:         "Device.WUSP.Subscription.{i}.Alias",
		Type:         TypeAlias,
		Access:       ReadWrite,
		SinceVersion: "1.0",
		Description:  "Non-functional unique key for a Wantastic subscription row.",
		Limits:       Limits{MinLength: 1, MaxLength: 64},
	},
	{
		Path:         "Device.WUSP.Subscription.{i}.ID",
		Type:         TypeString,
		Access:       ReadWrite,
		SinceVersion: "1.0",
		Description:  "Controller-visible identifier for a Wantastic notification subscription.",
		Limits:       Limits{MaxLength: 256},
	},
	{
		Path:         "Device.WUSP.Certificate.{i}.Alias",
		Type:         TypeAlias,
		Access:       ReadWrite,
		SinceVersion: "1.0",
		Description:  "Non-functional unique key for a Wantastic certificate row.",
		Limits:       Limits{MinLength: 1, MaxLength: 64},
	},
	{
		Path:         "Device.WUSP.ControllerTrust.Role.{i}.Alias",
		Type:         TypeAlias,
		Access:       ReadWrite,
		SinceVersion: "1.0",
		Description:  "Non-functional unique key for a Wantastic controller trust role.",
		Limits:       Limits{MinLength: 1, MaxLength: 64},
	},
}
