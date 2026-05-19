// wireguard.go — TR-181 Issue 2 Amendment 20 Device.WireGuard.* data model.
//
// Added in TR-181 v2.20 (November 2025).
// Source: BroadbandForum/device-data-model tr-181-2-wireguard.xml
// https://usp-data-models.broadband-forum.org/tr-181-2-20-1-usp-full.xml
//
// Object hierarchy (corrected per spec):
//
//	Device.WireGuard.
//	Device.WireGuard.Tunnel.{i}.          ← tunnel interface (private key, port)
//	Device.WireGuard.Tunnel.{i}.Stats.    ← traffic counters
//	Device.WireGuard.Tunnel.{i}.Interface.{i}.  ← stackable Layer3 interface
//	Device.WireGuard.Peer.{i}.            ← global peer table (not nested in Tunnel)
//
// AllowedIPs is a single list parameter on Peer.{i}, not a sub-table.
// Tunnels reference peers via Tunnel.{i}.PeerReferences (list of pathRefs).
package wusp

// ---------------------------------------------------------------------------
// Device.WireGuard. object catalogue
// ---------------------------------------------------------------------------

var WireGuardObjects = []Object{
	{
		Path:         "Device.WireGuard.",
		SinceVersion: "2.20",
		Description:  "WireGuard service root object.",
	},
	{
		Path:          "Device.WireGuard.Tunnel.{i}.",
		MultiInstance: true,
		SinceVersion:  "2.20",
		Description:   "WireGuard tunnel interface table.",
	},
	{
		Path:         "Device.WireGuard.Tunnel.{i}.Stats.",
		SinceVersion: "2.20",
		Description:  "Traffic counters for a WireGuard tunnel interface.",
	},
	{
		Path:          "Device.WireGuard.Tunnel.{i}.Interface.{i}.",
		MultiInstance: true,
		SinceVersion:  "2.20",
		Description:   "Stackable layer-3 interface rows exposed for a tunnel interface.",
	},
	{
		Path:          "Device.WireGuard.Peer.{i}.",
		MultiInstance: true,
		SinceVersion:  "2.20",
		Description:   "Global WireGuard peer table.",
	},
}

// ---------------------------------------------------------------------------
// Device.WireGuard. root parameters
// ---------------------------------------------------------------------------

var WireGuardRootParams = []Param{
	{
		Path:         "Device.WireGuard.TunnelNumberOfEntries",
		Type:         TypeUnsignedInt,
		Access:       ReadOnly,
		SinceVersion: "2.20",
		Description:  "Number of entries in Device.WireGuard.Tunnel.{i}.",
		Limits:       Limits{Min: iptr(0)},
	},
	{
		Path:         "Device.WireGuard.PeerNumberOfEntries",
		Type:         TypeUnsignedInt,
		Access:       ReadOnly,
		SinceVersion: "2.20",
		Description:  "Number of entries in Device.WireGuard.Peer.{i}.",
		Limits:       Limits{Min: iptr(0)},
	},
}

// ---------------------------------------------------------------------------
// Device.WireGuard.Tunnel.{i}. parameters
// ---------------------------------------------------------------------------

var WireGuardTunnelParams = []Param{
	{
		Path:         "Device.WireGuard.Tunnel.{i}.Enable",
		Type:         TypeBoolean,
		Access:       ReadWrite,
		SinceVersion: "2.20",
		Description:  "Enables or disables this WireGuard tunnel interface.",
	},
	{
		Path:         "Device.WireGuard.Tunnel.{i}.Status",
		Type:         TypeString,
		Access:       ReadOnly,
		SinceVersion: "2.20",
		Description:  "Current operational status of the tunnel.",
		Limits:       Limits{Enums: []string{"Disabled", "Enabled", "Error"}},
	},
	{
		Path:         "Device.WireGuard.Tunnel.{i}.Alias",
		Type:         TypeAlias,
		Access:       ReadWrite,
		SinceVersion: "2.20",
		Description:  "USP non-functional unique key for this Tunnel instance.",
		Limits:       Limits{MinLength: 1, MaxLength: 64},
	},
	{
		Path:         "Device.WireGuard.Tunnel.{i}.PrivateKey",
		Type:         TypeBase64,
		Access:       WriteOnly,
		SinceVersion: "2.20",
		Description:  "Curve25519 private key for this tunnel interface, base64-encoded (secured — not readable after write).",
	},
	{
		Path:         "Device.WireGuard.Tunnel.{i}.PublicKey",
		Type:         TypeBase64,
		Access:       ReadOnly,
		SinceVersion: "2.20",
		Description:  "Curve25519 public key derived from PrivateKey; advertised to peers.",
	},
	{
		Path:         "Device.WireGuard.Tunnel.{i}.ListenPort",
		Type:         TypeUnsignedInt,
		Access:       ReadWrite,
		SinceVersion: "2.20",
		Description:  "UDP port the tunnel listens on. 0 = kernel chooses an ephemeral port.",
		Limits:       Limits{Min: iptr(0), Max: iptr(65535)},
	},
	{
		Path:         "Device.WireGuard.Tunnel.{i}.PeerReferences",
		Type:         TypeList,
		Access:       ReadWrite,
		SinceVersion: "2.20",
		Description:  "Comma-separated list of strong path references to Device.WireGuard.Peer.{i}. entries associated with this tunnel.",
	},
}

// ---------------------------------------------------------------------------
// Device.WireGuard.Tunnel.{i}.Stats. parameters
// ---------------------------------------------------------------------------

var WireGuardTunnelStatsParams = []Param{
	{
		Path:         "Device.WireGuard.Tunnel.{i}.Stats.BytesSent",
		Type:         TypeStatsCounter,
		Access:       ReadOnly,
		SinceVersion: "2.20",
		Description:  "Total bytes transmitted on this tunnel interface.",
	},
	{
		Path:         "Device.WireGuard.Tunnel.{i}.Stats.BytesReceived",
		Type:         TypeStatsCounter,
		Access:       ReadOnly,
		SinceVersion: "2.20",
		Description:  "Total bytes received on this tunnel interface.",
	},
	{
		Path:         "Device.WireGuard.Tunnel.{i}.Stats.PacketsSent",
		Type:         TypeStatsCounter,
		Access:       ReadOnly,
		SinceVersion: "2.20",
		Description:  "Total packets transmitted on this tunnel interface.",
	},
	{
		Path:         "Device.WireGuard.Tunnel.{i}.Stats.PacketsReceived",
		Type:         TypeStatsCounter,
		Access:       ReadOnly,
		SinceVersion: "2.20",
		Description:  "Total packets received on this tunnel interface.",
	},
	{
		Path:         "Device.WireGuard.Tunnel.{i}.Stats.ErrorsSent",
		Type:         TypeStatsCounter,
		Access:       ReadOnly,
		SinceVersion: "2.20",
		Description:  "Total transmit errors on this tunnel interface.",
	},
	{
		Path:         "Device.WireGuard.Tunnel.{i}.Stats.ErrorsReceived",
		Type:         TypeStatsCounter,
		Access:       ReadOnly,
		SinceVersion: "2.20",
		Description:  "Total receive errors on this tunnel interface.",
	},
}

// ---------------------------------------------------------------------------
// Device.WireGuard.Tunnel.{i}.Interface.{i}. parameters
//
// This is a stackable Layer3Interface component per TR-181. WireGuard creates
// exactly one Interface instance per Tunnel in practice (maxEntries=1 per
// spec wording). Parameters below are the standard stackable-interface set.
// ---------------------------------------------------------------------------

var WireGuardTunnelInterfaceParams = []Param{
	{
		Path:         "Device.WireGuard.Tunnel.{i}.Interface.{i}.Enable",
		Type:         TypeBoolean,
		Access:       ReadWrite,
		SinceVersion: "2.20",
		Description:  "Enables or disables this stackable interface.",
	},
	{
		Path:         "Device.WireGuard.Tunnel.{i}.Interface.{i}.Status",
		Type:         TypeString,
		Access:       ReadOnly,
		SinceVersion: "2.20",
		Description:  "Current operational status of this stackable interface.",
		Limits: Limits{
			Enums: []string{"Up", "Down", "Unknown", "Dormant", "NotPresent", "LowerLayerDown", "Error"},
		},
	},
	{
		Path:         "Device.WireGuard.Tunnel.{i}.Interface.{i}.Alias",
		Type:         TypeAlias,
		Access:       ReadWrite,
		SinceVersion: "2.20",
		Description:  "USP non-functional unique key for this Interface instance.",
		Limits:       Limits{MinLength: 1, MaxLength: 64},
	},
	{
		Path:         "Device.WireGuard.Tunnel.{i}.Interface.{i}.Name",
		Type:         TypeString,
		Access:       ReadOnly,
		SinceVersion: "2.20",
		Description:  "OS-level interface name (e.g. \"wg0\").",
		Limits:       Limits{MaxLength: 64},
	},
	{
		Path:         "Device.WireGuard.Tunnel.{i}.Interface.{i}.LastChange",
		Type:         TypeUnsignedInt,
		Access:       ReadOnly,
		SinceVersion: "2.20",
		Description:  "Seconds since the Status last changed.",
	},
	{
		Path:         "Device.WireGuard.Tunnel.{i}.Interface.{i}.LowerLayers",
		Type:         TypeList,
		Access:       ReadWrite,
		SinceVersion: "2.20",
		Description:  "Comma-separated list of path references to objects below this interface in the interface stack.",
	},
}

// ---------------------------------------------------------------------------
// Device.WireGuard.Peer.{i}. parameters
//
// Global peer table. Peers are NOT nested inside Tunnel — tunnels reference
// peers via Tunnel.{i}.PeerReferences. AllowedIPs is a flat list parameter,
// not a sub-table.
// ---------------------------------------------------------------------------

var WireGuardPeerParams = []Param{
	{
		Path:         "Device.WireGuard.Peer.{i}.Enable",
		Type:         TypeBoolean,
		Access:       ReadWrite,
		SinceVersion: "2.20",
		Description:  "Enables or disables this peer entry.",
	},
	{
		Path:         "Device.WireGuard.Peer.{i}.Alias",
		Type:         TypeAlias,
		Access:       ReadWrite,
		SinceVersion: "2.20",
		Description:  "USP non-functional unique key for this Peer instance.",
		Limits:       Limits{MinLength: 1, MaxLength: 64},
	},
	{
		Path:         "Device.WireGuard.Peer.{i}.PublicKey",
		Type:         TypeString,
		Access:       ReadWrite,
		SinceVersion: "2.20",
		Description:  "Peer's Curve25519 public key, base64-encoded. Functional unique key for this table.",
	},
	{
		Path:         "Device.WireGuard.Peer.{i}.PresharedKey",
		Type:         TypeString,
		Access:       WriteOnly,
		SinceVersion: "2.20",
		Description:  "Optional 256-bit symmetric preshared key, base64-encoded (secured). Empty string = no PSK.",
	},
	{
		Path:         "Device.WireGuard.Peer.{i}.AllowedIPs",
		Type:         TypeList,
		Access:       ReadWrite,
		SinceVersion: "2.20",
		Description:  "Comma-separated list of CIDR-notation IP prefixes routed through this peer (e.g. \"10.0.0.0/24,fd00::/64\"). Use \"0.0.0.0/0,::/0\" for a catch-all relay peer.",
	},
	{
		Path:         "Device.WireGuard.Peer.{i}.EndpointAddress",
		Type:         TypeString,
		Access:       ReadWrite,
		SinceVersion: "2.20",
		Description:  "Hostname or IP address of the peer's endpoint. Empty = roaming peer (waits for peer to initiate).",
		Limits:       Limits{MaxLength: 256},
	},
	{
		Path:         "Device.WireGuard.Peer.{i}.EndpointPort",
		Type:         TypeInt,
		Access:       ReadWrite,
		SinceVersion: "2.20",
		Description:  "UDP port of the peer's endpoint. -1 = not configured.",
		Limits:       Limits{Min: iptr(-1), Max: iptr(65535)},
	},
	{
		Path:         "Device.WireGuard.Peer.{i}.PersistentKeepalive",
		Type:         TypeUnsignedInt,
		Access:       ReadWrite,
		SinceVersion: "2.20",
		Description:  "Keepalive interval in seconds. 0 = disabled. Typical value when behind NAT: 25.",
		Limits:       Limits{Min: iptr(0), Max: iptr(65535)},
	},
	{
		Path:         "Device.WireGuard.Peer.{i}.LastHandshakeTime",
		Type:         TypeUnsignedInt,
		Access:       ReadOnly,
		SinceVersion: "2.20",
		Description:  "Seconds elapsed since the last successful handshake with this peer. 0 = never.",
	},
}

// ---------------------------------------------------------------------------
// Aggregated slice — included in AllDeviceParams via broadband.go aggregation.
// ---------------------------------------------------------------------------

// AllWireGuardParams is the union of all Device.WireGuard.* parameter
// definitions. Merged into AllDeviceParams as part of the runtime schema.
var AllWireGuardParams = concat(
	WireGuardRootParams,
	WireGuardTunnelParams,
	WireGuardTunnelStatsParams,
	WireGuardTunnelInterfaceParams,
	WireGuardPeerParams,
)
