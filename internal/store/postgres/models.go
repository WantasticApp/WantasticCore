package postgres

import (
	"encoding/json"
	"time"
)

// Account represents a tenant account's resource allocation (from internal/account/store.go).
type Account struct {
	tableName struct{} `pg:"accounts"`

	ID         string    `pg:"id,pk"`
	Name       string    `pg:"name"`
	Networks   []string  `pg:"networks,array"`   // Multiple /27 blocks
	ServerIPs  []string  `pg:"server_ips,array"` // Server IP for each block
	BlockCount int       `pg:"block_count"`
	PrivateKey string    `pg:"private_key"`
	MaxPeers   int       `pg:"max_peers,use_zero"`
	CreatedAt  time.Time `pg:"created_at"`
	UpdatedAt  time.Time `pg:"updated_at"`
}

// Tenant represents a user/tenant account (from internal/tenant/store.go).
type Tenant struct {
	tableName struct{} `pg:"tenants"`

	// Identity
	ID       string `pg:"id,pk"`
	Email    string `pg:"email,unique"`
	FullName string `pg:"full_name"`

	// Authentication
	PasswordHash string    `pg:"password_hash"`
	TOTPSecret   string    `pg:"totp_secret"`
	TOTPEnabled  bool      `pg:"totp_enabled"`
	LastLogin    time.Time `pg:"last_login"`

	// 2FA Methods (SMS 2FA removed in Phase 3; only TOTP + WhatsApp remain)
	TwoFAMethod       string    `pg:"twofa_method"`
	TwoFAWhatsApp     bool      `pg:"twofa_whatsapp_enabled"`
	TwoFAPendingCode  string    `pg:"twofa_pending_code"`
	TwoFACodeExpiry   time.Time `pg:"twofa_code_expiry"`
	TwoFACodeAttempts int       `pg:"twofa_code_attempts"`

	// Links to Account
	OverlayAccountID string   `pg:"overlay_account_id"`
	Networks         []string `pg:"networks,array"`

	// Status
	Status string `pg:"status"`

	// Role
	IsAdmin bool `pg:"is_admin"`

	// Preferences
	PreferredLanguage string `pg:"preferred_language"`

	// Inactivity
	InactivityWarningSentAt *time.Time `pg:"inactivity_warning_sent_at"`

	// Auth0 subject identifier — empty for password-based accounts.
	// `use_zero` tells go-pg to send '' as an empty string rather than
	// NULL; the column is NOT NULL with default '', so the absence of
	// this tag was causing every login (which updates the row) to fail
	// with a not-null constraint violation.
	Auth0Sub string `pg:"auth0_sub,use_zero"`

	CreatedAt time.Time `pg:"created_at"`
	UpdatedAt time.Time `pg:"updated_at"`
}

// Peer represents a WireGuard peer (from internal/server/peer_store.go PeerMetadata).
type Peer struct {
	tableName struct{} `pg:"peers"`

	ID         string    `pg:"id,pk"` // Public Key
	AccountID  string    `pg:"account_id"`
	Name       string    `pg:"name"`
	AssignedIP string    `pg:"assigned_ip"`
	AllowedIPs []string  `pg:"allowed_ips,array,use_zero"`
	PrivateKey string    `pg:"private_key"`
	CreatedAt  time.Time `pg:"created_at"`
	UpdatedAt  time.Time `pg:"updated_at"`

	// Online status
	IsOnline          bool      `pg:"is_online"`
	LastHandshakeTime time.Time `pg:"last_handshake_time"`
	LastSeenAt        time.Time `pg:"last_seen_at"`
	Endpoint          string    `pg:"endpoint"`
	UptimeHistory     []byte    `pg:"uptime_history"`
	RxBytes           int64     `pg:"rx_bytes"`
	TxBytes           int64     `pg:"tx_bytes"`

	// WebSSH / Winbox Features
	WebSSHConsumerActive      bool      `pg:"webssh_consumer_active"`
	WebSSHConsumerPort        int       `pg:"webssh_consumer_port"`
	WebSSHLinkActive          bool      `pg:"webssh_link_active"`
	WebSSHLinkExpiry          time.Time `pg:"webssh_link_expiry"`
	HasWinbox                 bool      `pg:"has_winbox,use_zero"`
	EncryptedRouterOSUsername []byte    `pg:"encrypted_routeros_username"`
	EncryptedRouterOSPassword []byte    `pg:"encrypted_routeros_password"`
	RouterOSCredentialSource  string    `pg:"routeros_credential_source,use_zero"`
	RouterOSAPIVerified       bool      `pg:"routeros_api_verified,use_zero"`
	RouterOSAPILastValidated  time.Time `pg:"routeros_api_last_validated"`
	RouterOSAPIError          string    `pg:"routeros_api_error,use_zero"`
	RouterOSAPIPort           int       `pg:"routeros_api_port,use_zero"`
	RouterOSAPITLS            bool      `pg:"routeros_api_tls,use_zero"`

	// Relations
	WinboxSessions   []*WinboxSession  `pg:"rel:has-many"`
	WebSSHSessions   []*WebSSHSession  `pg:"rel:has-many"`
	SSHActivities    []*SSHActivity    `pg:"rel:has-many"`
	WinboxActivities []*WinboxActivity `pg:"rel:has-many"`

	// Scanning & Notifications
	LastPortScan             time.Time       `pg:"last_port_scan"`
	CachedPortScanJSON       json.RawMessage `pg:"cached_port_scan_json,type:jsonb"`
	ScannedSSHPort           int             `pg:"scanned_ssh_port"`
	ScannedWinboxPort        int             `pg:"scanned_winbox_port"`
	LastPortScanTime         time.Time       `pg:"last_port_scan_time"`
	NotificationEnabled      bool            `pg:"notification_enabled,use_zero"`
	FirstSeenOnline          time.Time       `pg:"first_seen_online"`
	LastOnlineAt             time.Time       `pg:"last_online_at"`
	FailedHandshakes         int             `pg:"failed_handshakes"`
	LastNotificationSentAt   time.Time       `pg:"last_notification_sent_at"`
	OfflineNotificationState string          `pg:"offline_notification_state"`
	ScanProgress             int             `pg:"scan_progress"`
	Tags                     []string        `pg:"tags,array,use_zero"`
	Notes                    string          `pg:"notes"`
	ClientType               string          `pg:"client_type,use_zero"` // 'native' or 'wantasticd'
	IsWantasticd             bool            `pg:"is_wantasticd,use_zero"`
	AgentModel               string          `pg:"agent_model,use_zero"`
	AgentVersion             string          `pg:"agent_version,use_zero"`
}

// WinboxSession represents a Winbox session for a peer.
type WinboxSession struct {
	tableName struct{} `pg:"winbox_sessions"`

	ID                       string    `pg:"id,pk"`
	PeerID                   string    `pg:"peer_id"` // FK to Peer
	AccountID                string    `pg:"account_id"`
	Name                     string    `pg:"name"`
	RouterIP                 string    `pg:"router_ip"`
	Port                     int       `pg:"port,use_zero"`
	AccessToken              string    `pg:"access_token"`
	PasswordToken            string    `pg:"password_token"`
	EncryptedUsername        []byte    `pg:"encrypted_username"`
	EncryptedPassword        []byte    `pg:"encrypted_password"`
	AuthMethod               string    `pg:"auth_method"`
	AllowedClientIPs         []string  `pg:"allowed_client_ips,array,use_zero"`
	CredentialsValid         bool      `pg:"credentials_valid"`
	LastValidated            time.Time `pg:"last_validated"`
	ValidationError          string    `pg:"validation_error"`
	RouterOSAPIVerified      bool      `pg:"routeros_api_verified"`
	RouterOSAPILastValidated time.Time `pg:"routeros_api_last_validated"`
	RouterOSAPIError         string    `pg:"routeros_api_error"`
	RouterOSAPIPort          int       `pg:"routeros_api_port,use_zero"`
	RouterOSAPITLS           bool      `pg:"routeros_api_tls"`
	LastConnected            time.Time `pg:"last_connected"`
	CreatedAt                time.Time `pg:"created_at"`
	UpdatedAt                time.Time `pg:"updated_at"`
	Enabled                  bool      `pg:"enabled,use_zero"`
}

// WebSSHSession represents a WebSSH session for a peer.
type WebSSHSession struct {
	tableName struct{} `pg:"webssh_sessions"`

	ID                            string    `pg:"id,pk"`
	PeerID                        string    `pg:"peer_id"` // FK to Peer
	PeerIP                        string    `pg:"peer_ip"`
	AccountID                     string    `pg:"account_id"`
	Name                          string    `pg:"name"`
	Port                          int       `pg:"port"`
	EncryptedUsername             []byte    `pg:"encrypted_username"`
	EncryptedPassword             []byte    `pg:"encrypted_password"`
	EncryptedPrivateKey           []byte    `pg:"encrypted_private_key"`
	EncryptedPrivateKeyPassphrase []byte    `pg:"encrypted_private_key_passphrase"`
	TerminalRows                  int       `pg:"terminal_rows"`
	TerminalCols                  int       `pg:"terminal_cols"`
	UserAgent                     string    `pg:"user_agent"`
	LastConnected                 time.Time `pg:"last_connected"`
	CreatedAt                     time.Time `pg:"created_at"`
	UpdatedAt                     time.Time `pg:"updated_at"`
	Enabled                       bool      `pg:"enabled,use_zero"`
	History                       []byte    `pg:"history"`
	HostKey                       []byte    `pg:"host_key"`
	HostKeyFingerprint            string    `pg:"host_key_fingerprint"`
	HostKeyAlgorithm              string    `pg:"host_key_algorithm"`
	CompatibilityMode             string    `pg:"compatibility_mode,use_zero"`
}

// SSHActivity represents SSH session activity log.
type SSHActivity struct {
	tableName struct{} `pg:"ssh_activities"`

	ID         string          `pg:"id,pk,default:gen_random_uuid()"` // Auto-generated ID
	PeerID     string          `pg:"peer_id"`
	AccountID  string          `pg:"account_id"`
	SessionID  string          `pg:"session_id"` // WebSSH session ID
	UserAgent  string          `pg:"user_agent"`
	ClientIP   string          `pg:"client_ip"`
	Timestamp  time.Time       `pg:"timestamp"`
	EndTime    time.Time       `pg:"end_time"`
	Username   string          `pg:"username"`
	Commands   json.RawMessage `pg:"commands,type:jsonb"` // []SSHSessionCommand
	BytesSent  uint64          `pg:"bytes_sent"`
	BytesRecv  uint64          `pg:"bytes_recv"`
	DurationMs int64           `pg:"duration_ms"`
}

// WinboxActivity represents Winbox session activity log.
type WinboxActivity struct {
	tableName struct{} `pg:"winbox_activities"`

	ID          string    `pg:"id,pk,default:gen_random_uuid()"` // Auto-generated ID
	PeerID      string    `pg:"peer_id"`
	AccountID   string    `pg:"account_id"`
	SessionName string    `pg:"session_name"`
	Username    string    `pg:"username"`
	ClientIP    string    `pg:"client_ip"`
	Timestamp   time.Time `pg:"timestamp"`
	EndTime     time.Time `pg:"end_time"`
	DurationMs  int64     `pg:"duration_ms"`
	RomonMode   bool      `pg:"romon_mode"`
}

// PeerGroup represents a group of peers (from internal/server/server.go).
type PeerGroup struct {
	tableName struct{} `pg:"peer_groups"`

	ID          string    `pg:"id,pk"`
	AccountID   string    `pg:"account_id"`
	Name        string    `pg:"name"`
	Description string    `pg:"description"`
	Protocols   []uint8   `pg:"protocols,array"`
	CreatedAt   time.Time `pg:"created_at"`
	UpdatedAt   time.Time `pg:"updated_at"`
}

// GroupLink represents a link between peer groups (from internal/server/server.go).
type GroupLink struct {
	tableName struct{} `pg:"group_links"`

	ID         string    `pg:"id,pk"`
	AccountID  string    `pg:"account_id"`
	SrcGroupID string    `pg:"src_group_id"`
	DstGroupID string    `pg:"dst_group_id"`
	Action     string    `pg:"action"`
	Protocols  []uint8   `pg:"protocols,array"`
	PortRanges any       `pg:"port_ranges,type:jsonb"` // []PortRange
	CreatedAt  time.Time `pg:"created_at"`
	UpdatedAt  time.Time `pg:"updated_at"`
}

// TenantSession represents an active tenant session (from internal/auth/tenant_session_store.go).
type TenantSession struct {
	tableName struct{} `pg:"tenant_sessions"`

	SessionID     string    `pg:"session_id,pk"`
	TenantID      string    `pg:"tenant_id"`
	Email         string    `pg:"email"`
	FullName      string    `pg:"full_name"`
	SessionToken  string    `pg:"session_token"`
	CreatedAt     time.Time `pg:"created_at"`
	ExpiresAt     time.Time `pg:"expires_at"`
	LastActivity  time.Time `pg:"last_activity"`
	IPAddress     string    `pg:"ip_address"`
	UserAgent     string    `pg:"user_agent"`
	RememberMe    bool      `pg:"remember_me"`
	DeviceHash    string    `pg:"device_hash"`
	TrustedDevice bool      `pg:"trusted_device"`
}

// ACLRule represents a firewall rule (from internal/models/acl.go).
type ACLRule struct {
	tableName struct{} `pg:"acl_rules"`

	ID          string    `pg:"id,pk"`
	AccountID   string    `pg:"account_id"`
	Name        string    `pg:"name"`
	Action      string    `pg:"action"`
	Protocol    string    `pg:"protocol"`
	SourceIPs   []string  `pg:"source_ips,array"`
	DestIPs     []string  `pg:"dest_ips,array"`
	DestPorts   []int     `pg:"dest_ports,array"`
	Priority    int       `pg:"priority"`
	Description string    `pg:"description"`
	CreatedAt   time.Time `pg:"created_at"`
	UpdatedAt   time.Time `pg:"updated_at"`

	// High-level intent fields
	SourcePeerIDs []string `pg:"source_peer_ids,array"`
	DestPeerIDs   []string `pg:"dest_peer_ids,array"`
	Services      []string `pg:"services,array"`
}

// PeerMigration represents a peer migration request.
type PeerMigration struct {
	tableName struct{} `pg:"peer_migrations"`

	ID                   string     `pg:"id,pk"`
	SourceTenantID       string     `pg:"source_tenant_id"`
	SourceTenantEmail    string     `pg:"source_tenant_email"`
	SourceTenantName     string     `pg:"source_tenant_name"`
	TargetEmail          string     `pg:"target_email"`
	TargetTenantID       string     `pg:"target_tenant_id"`
	TargetTenantName     string     `pg:"target_tenant_name"`
	Peers                any        `pg:"peers,type:jsonb"` // []store.MigrationPeerData
	InviteToken          string     `pg:"invite_token"`
	Status               string     `pg:"status"`
	FailureReason        string     `pg:"failure_reason"`
	EncryptedSSHUsername []byte     `pg:"encrypted_ssh_username"`
	EncryptedSSHPassword []byte     `pg:"encrypted_ssh_password"`
	CreatedAt            time.Time  `pg:"created_at"`
	ExpiresAt            time.Time  `pg:"expires_at"`
	AcceptedAt           *time.Time `pg:"accepted_at"`
	CompletedAt          *time.Time `pg:"completed_at"`
	Logs                 []string   `pg:"logs,array"`
}

// EnrollmentToken represents a secure enrollment token for devices.
type EnrollmentToken struct {
	tableName struct{} `pg:"enrollment_tokens"`

	ID         string    `pg:"id,pk"`
	TenantID   string    `pg:"tenant_id"`
	Name       string    `pg:"name"`
	Token      string    `pg:"token,unique"`
	MaxUses    int       `pg:"max_uses"`
	UsageCount int       `pg:"usage_count"`
	ExpiresAt  time.Time `pg:"expires_at"`
	CreatedAt  time.Time `pg:"created_at"`
	CreatedBy  string    `pg:"created_by"`
}

// IPAMBlock represents a single /27 block in the global IPAM pool.
type IPAMBlock struct {
	tableName struct{} `pg:"ipam_blocks"`

	CIDR      string    `pg:"cidr,pk"`
	TenantID  string    `pg:"tenant_id"` // Owner account ID (empty if free)
	Allocated bool      `pg:"allocated"`
	PoolIndex int       `pg:"pool_index,use_zero"`
	UpdatedAt time.Time `pg:"updated_at"`
}

// PeerHandshake represents a recorded handshake event in database.
type PeerHandshake struct {
	tableName struct{} `pg:"peer_handshakes"`

	ID        int64     `pg:"id,pk"`
	PeerID    string    `pg:"peer_id"`
	AccountID string    `pg:"account_id"`
	Timestamp time.Time `pg:"timestamp"`
	Endpoint  string    `pg:"endpoint"`
}

// WUSPDeviceState is the postgres model for wusp_device_states.
type WUSPDeviceState struct {
	tableName struct{} `pg:"wusp_device_states"`

	ID        string `pg:"id,pk"`
	PeerID    string `pg:"peer_id"`
	AccountID string `pg:"account_id"`

	LastSyncAt time.Time `pg:"last_sync_at"`
	SyncError  string    `pg:"sync_error"`
	// DeviceSnapshot holds the full Device.* parameter tree as a flat
	// [{path,value}] JSON array produced by the WUSP controller.
	DeviceSnapshot json.RawMessage `pg:"device_snapshot,type:jsonb"`

	DeviceID        string `pg:"device_id"`
	Manufacturer    string `pg:"manufacturer"`
	ProductClass    string `pg:"product_class"`
	SerialNumber    string `pg:"serial_number"`
	SoftwareVersion string `pg:"software_version"`
	HardwareVersion string `pg:"hardware_version"`
	WUSPEnable      bool   `pg:"wusp_enable"`
	WUSPStatus      string `pg:"wusp_status"`
	WUSPVersion     string `pg:"wusp_version"`

	CreatedAt time.Time `pg:"created_at"`
	UpdatedAt time.Time `pg:"updated_at"`
}

// DeviceSnapshot is the ORM model for the device_snapshots table.
// It stores named, tenant-scoped device configuration backups that can be
// applied to any peer during provisioning.
type DeviceSnapshot struct {
	tableName struct{} `pg:"device_snapshots"`

	ID        string `pg:"id,pk"`
	AccountID string `pg:"account_id"`

	Name     string `pg:"name"`
	Protocol string `pg:"protocol"`

	Manufacturer    string `pg:"manufacturer"`
	ProductClass    string `pg:"product_class"`
	SerialNumber    string `pg:"serial_number"`
	SoftwareVersion string `pg:"software_version"`
	HardwareVersion string `pg:"hardware_version"`

	DeviceSnapshot json.RawMessage `pg:"device_snapshot,type:jsonb"`

	BackupFile  string `pg:"backup_file"`
	BackupName  string `pg:"backup_name"`
	BackupSize  int    `pg:"backup_size"`
	UploadToken string `pg:"upload_token"`

	CreatedAt time.Time `pg:"created_at"`
	UpdatedAt time.Time `pg:"updated_at"`
}

// Models returns all model structs for schema creation.
func Models() []any {
	return []any{
		(*Account)(nil),
		(*Tenant)(nil),
		(*Peer)(nil),
		(*WinboxSession)(nil),
		(*WebSSHSession)(nil),
		(*SSHActivity)(nil),
		(*WinboxActivity)(nil),
		(*PeerGroup)(nil),
		(*GroupLink)(nil),
		(*TenantSession)(nil),
		(*ACLRule)(nil),
		(*PeerMigration)(nil),
		(*PeerHandshake)(nil),
		(*EnrollmentToken)(nil),
		(*IPAMBlock)(nil),
		(*WUSPDeviceState)(nil),
		(*DeviceSnapshot)(nil),
	}
}
