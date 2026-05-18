package store

import "time"

// =============================================================================
// Account Data
// =============================================================================

// AccountData represents an account (WireGuard network allocation) in storage.
type AccountData struct {
	ID         string
	Name       string
	Networks   []string // Multiple /27 CIDR blocks
	ServerIPs  []string // Server IP for each block
	BlockCount int
	PrivateKey string
	MaxPeers   int // Max peers allowed for this account (replaces tier-based limit)
	CreatedAt  time.Time
	UpdatedAt  time.Time
}

// =============================================================================
// Tenant Data
// =============================================================================

// TenantData represents a user/tenant in storage.
type TenantData struct {
	ID       string
	Email    string
	FullName string

	// Authentication
	PasswordHash string
	TOTPSecret   string
	TOTPEnabled  bool
	LastLogin    time.Time

	// 2FA (SMS 2FA removed Phase 3; TOTP + WhatsApp only)
	TwoFAMethod       string
	TwoFAWhatsApp     bool
	TwoFAPendingCode  string
	TwoFACodeExpiry   time.Time
	TwoFACodeAttempts int

	// Links to Account
	OverlayAccountID string
	Networks         []string

	// Status
	Status string

	// Role — true for super-admins who can manage tenants
	IsAdmin bool

	// Preferences
	PreferredLanguage string

	// Inactivity
	InactivityWarningSentAt *time.Time

	// Auth0 subject identifier (populated for Device Authorization Grant accounts).
	Auth0Sub string

	CreatedAt time.Time
	UpdatedAt time.Time
}

// TwoFAInfo contains 2FA configuration for a tenant.
type TwoFAInfo struct {
	Method          string
	TOTPEnabled     bool
	SMSEnabled      bool
	WhatsAppEnabled bool
	EmailEnabled    bool
}

// =============================================================================
// Session Data
// =============================================================================

// SessionData represents a tenant session in storage.
type SessionData struct {
	SessionID     string
	TenantID      string
	Email         string
	FullName      string
	SessionToken  string
	CreatedAt     time.Time
	ExpiresAt     time.Time
	LastActivity  time.Time
	IPAddress     string
	UserAgent     string
	RememberMe    bool
	DeviceHash    string
	TrustedDevice bool
}

// =============================================================================
// Peer Data
// =============================================================================

// PeerData represents a WireGuard peer in storage.
type PeerData struct {
	ID         string // Public key
	AccountID  string
	Name       string
	AssignedIP string
	AllowedIPs []string
	Tags       []string
	PrivateKey string
	CreatedAt  time.Time
	UpdatedAt  time.Time

	// Online status
	IsOnline          bool
	LastHandshakeTime time.Time
	LastSeenAt        time.Time
	Endpoint          string // Public IP and port
	UptimeHistory     []byte // Monthly uptime bits (720 bits = 90 bytes)
	RxBytes           int64
	TxBytes           int64

	// WebSSH state
	WebSSHConsumerActive bool
	WebSSHConsumerPort   int
	WebSSHLinkActive     bool
	WebSSHLinkExpiry     time.Time

	// Winbox
	HasWinbox bool

	// RouterOS API access
	EncryptedRouterOSUsername []byte
	EncryptedRouterOSPassword []byte
	RouterOSCredentialSource  string
	RouterOSAPIVerified       bool
	RouterOSAPILastValidated  time.Time
	RouterOSAPIError          string
	RouterOSAPIPort           int
	RouterOSAPITLS            bool

	// Port scanning
	LastPortScan             time.Time
	CachedPortScanJSON       []byte
	ScannedSSHPort           int
	ScannedWinboxPort        int
	LastPortScanTime         time.Time
	NotificationEnabled      bool
	FirstSeenOnline          time.Time
	LastOnlineAt             time.Time
	FailedHandshakes         int
	LastNotificationSentAt   time.Time
	OfflineNotificationState string
	Notes                    string
	ClientType               string // 'native' or 'wantasticd'
	IsWantasticd             bool
	AgentModel               string // agent software family, e.g. "wantasticd" — populated from stats
	AgentVersion             string // agent build version, e.g. "v1.2.3" — populated from stats

	// Activities (populated on List if requested)
	SSHActivities    []*SSHActivityData
	WinboxActivities []*WinboxActivityData
}

// PeerHandshakeData represents a single successful handshake event.
type PeerHandshakeData struct {
	ID        int64
	PeerID    string
	AccountID string
	Timestamp time.Time
	Endpoint  string
}

// OpenPortData represents a discovered open port.
type OpenPortData struct {
	Port      int
	Protocol  string
	Service   string
	Banner    string
	RTTMs     int64
	IsWebPage bool
}

// OSFingerprintData represents detected OS information.
type OSFingerprintData struct {
	OSFamily      string
	OSVersion     string
	Vendor        string
	DeviceType    string
	Model         string
	Hostname      string
	MACAddress    string
	MACVendor     string
	Confidence    int
	DetectionInfo string
}

// =============================================================================
// Winbox Session Data
// =============================================================================

// WinboxSessionData represents a Winbox session for a peer.
type WinboxSessionData struct {
	ID                       string
	PeerID                   string
	AccountID                string
	Name                     string
	RouterIP                 string
	Port                     int `pg:"default:8291"`
	AccessToken              string
	PasswordToken            string
	EncryptedUsername        []byte
	EncryptedPassword        []byte
	AuthMethod               string
	AllowedClientIPs         []string
	CredentialsValid         bool
	LastValidated            time.Time
	ValidationError          string
	RouterOSAPIVerified      bool
	RouterOSAPILastValidated time.Time
	RouterOSAPIError         string
	RouterOSAPIPort          int
	RouterOSAPITLS           bool
	LastConnected            time.Time
	CreatedAt                time.Time
	UpdatedAt                time.Time
	Enabled                  bool
}

// WinboxLookupResult contains the result of a Winbox session lookup.
type WinboxLookupResult struct {
	AccountID string
	PeerID    string
	SessionID string
	Session   *WinboxSessionData
	Peer      *PeerData
}

// =============================================================================
// WebSSH Session Data
// =============================================================================

// WebSSHSessionData represents a WebSSH session for a peer.
type WebSSHSessionData struct {
	ID                            string
	PeerID                        string
	PeerIP                        string
	AccountID                     string
	Name                          string
	Port                          int
	EncryptedUsername             []byte
	EncryptedPassword             []byte
	EncryptedPrivateKey           []byte
	EncryptedPrivateKeyPassphrase []byte
	TerminalRows                  int
	TerminalCols                  int
	UserAgent                     string
	LastConnected                 time.Time
	CreatedAt                     time.Time
	UpdatedAt                     time.Time
	Enabled                       bool
	History                       []byte // Session output history
	HostKey                       []byte
	HostKeyFingerprint            string
	HostKeyAlgorithm              string
	CompatibilityMode             string
}

// =============================================================================
// Activity Data
// =============================================================================

// SSHActivityData represents SSH session activity log.
type SSHActivityData struct {
	ID           string           `json:"id"`
	PeerID       string           `json:"peer_id"`
	AccountID    string           `json:"account_id"`
	SessionID    string           `json:"session_id"`
	UserAgent    string           `json:"user_agent"`
	ClientIP     string           `json:"client_ip"`
	Timestamp    time.Time        `json:"timestamp"`
	EndTime      time.Time        `json:"end_time"`
	Username     string           `json:"username"`
	Commands     []SSHCommandData `json:"commands,omitempty"`
	CommandsJSON []byte           `json:"-"`
	BytesSent    uint64           `json:"bytes_sent"`
	BytesRecv    uint64           `json:"bytes_recv"`
	DurationMs   int64            `json:"duration_ms"`
}

// SSHCommandData represents a command in an SSH session.
type SSHCommandData struct {
	Command   string
	Timestamp time.Time
}

// WinboxActivityData represents Winbox session activity log.
type WinboxActivityData struct {
	ID          string
	PeerID      string
	AccountID   string
	SessionName string
	Username    string
	ClientIP    string
	Timestamp   time.Time
	EndTime     time.Time
	DurationMs  int64
	RomonMode   bool
}

// =============================================================================
// Group Data (ACL)
// =============================================================================

// GroupData represents a peer group for ACL.
type GroupData struct {
	ID          string
	AccountID   string
	Name        string
	Description string
	Protocols   []uint8
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

// GroupLinkData represents a link between peer groups.
type GroupLinkData struct {
	ID         string
	AccountID  string
	SrcGroupID string
	DstGroupID string
	Action     string // "allow" or "deny"
	Protocols  []uint8
	PortRanges []PortRange
	CreatedAt  time.Time
	UpdatedAt  time.Time
}

// PortRange represents an inclusive TCP/UDP port range.
type PortRange struct {
	Start uint16
	End   uint16
}

// =============================================================================
// ACL Rule Data
// =============================================================================

// ACLRuleData represents a firewall rule.
type ACLRuleData struct {
	ID          string
	AccountID   string
	Name        string
	Action      string // "allow" or "deny"
	Protocol    string // "tcp", "udp", "icmp", "all"
	SourceIPs   []string
	DestIPs     []string
	DestPorts   []int
	Priority    int
	Description string
	CreatedAt   time.Time
	UpdatedAt   time.Time

	// High-level fields
	SourcePeerIDs []string
	DestPeerIDs   []string
	Services      []string
}

// =============================================================================
// IPAM Data
// =============================================================================

// IPAMBlockData represents a /27 IP block in the global IPAM pool.
type IPAMBlockData struct {
	CIDR      string
	TenantID  string
	Allocated bool
	PoolIndex int
	UpdatedAt time.Time
}

// =============================================================================
// WUSP Device State Data
// =============================================================================

// WUSPDeviceStateData is the persisted WUSP device model snapshot for a peer.
// One row per peer; linked to peers.id via PeerID.
//
// DeviceSnapshot stores the full Device.* parameter tree as a flat JSON array
// of {path, value} pairs. The indexed string/bool fields are extracted from
// the snapshot for efficient SQL queries without JSONB scanning.
type WUSPDeviceStateData struct {
	ID        string // UUID; assigned on first upsert
	PeerID    string // WireGuard public key — FK to peers.id
	AccountID string // Owning account for tenant-scoped queries

	LastSyncAt time.Time
	SyncError  string // Non-empty when the last sync attempt failed

	// Full device model as [{path,value}] JSON array.
	DeviceSnapshot []byte

	// Indexed fields extracted from the snapshot for fast queries.
	DeviceID        string // Device.DeviceInfo.DeviceID
	Manufacturer    string // Device.DeviceInfo.Manufacturer
	ProductClass    string // Device.DeviceInfo.ProductClass
	SerialNumber    string // Device.DeviceInfo.SerialNumber
	SoftwareVersion string // Device.DeviceInfo.SoftwareVersion
	HardwareVersion string // Device.DeviceInfo.HardwareVersion
	WUSPEnable      bool   // Device.WUSP.Enable
	WUSPStatus      string // Device.WUSP.Status
	WUSPVersion     string // Device.WUSP.ProtocolVersion

	CreatedAt time.Time
	UpdatedAt time.Time
}

// DeviceSnapshotData is a named, tenant-scoped device configuration snapshot.
// Unlike WUSPDeviceStateData (which is live state tied to a peer), a
// DeviceSnapshot is portable — it can be applied to any peer during
// provisioning and is not deleted when a peer is removed.
//
// Protocol records the management protocol that produced the snapshot
// (e.g. "wusp") so the same table can serve future protocols.
type DeviceSnapshotData struct {
	ID        string // UUID
	AccountID string // Owning tenant

	// User-visible identity
	Name     string // Human-readable label given by the user
	Protocol string // Management protocol, e.g. "wusp"

	// Indexed device identity fields (extracted from snapshot for fast queries)
	Manufacturer    string // Device.DeviceInfo.Manufacturer
	ProductClass    string // Device.DeviceInfo.ProductClass (model)
	SerialNumber    string // Device.DeviceInfo.SerialNumber
	SoftwareVersion string // Device.DeviceInfo.SoftwareVersion
	HardwareVersion string // Device.DeviceInfo.HardwareVersion

	// Full device model as [{path,value}] JSON array — same format as
	// WUSPDeviceStateData.DeviceSnapshot.
	DeviceSnapshot []byte

	// MikroTik /export RouterOS script — stored as TEXT (UTF-8 .rsc content).
	// WUSP snapshots use DeviceSnapshot above; protocol field differentiates.
	BackupFile string
	BackupName string // Original filename (e.g. "export-2026-04-23.rsc")
	BackupSize int    // File size in bytes (for display without loading the blob)

	// Upload token for secure external uploads (MikroTik script hook).
	// Rotated after each successful upload. Empty if not configured.
	UploadToken string

	CreatedAt time.Time
	UpdatedAt time.Time
}
