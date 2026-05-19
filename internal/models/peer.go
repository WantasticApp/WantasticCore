package models

import "time"

// WinboxSession represents a single Winbox access session for a peer.
// Multiple sessions can exist per peer with different access tokens and allowed IPs.
type WinboxSession struct {
	ID                string    `json:"id"`                 // Unique session ID (UUID)
	Name              string    `json:"name"`               // Human-readable session name
	RouterIP          string    `json:"router_ip"`          // Backend MikroTik device IP
	AccessToken       string    `json:"access_token"`       // Virtual username for Winbox login
	PasswordToken     string    `json:"password_token"`     // Virtual password for Winbox login (separate from access token)
	EncryptedUsername []byte    `json:"encrypted_username"` // Real MikroTik username (AES-256-GCM)
	EncryptedPassword []byte    `json:"encrypted_password"` // Real MikroTik password (AES-256-GCM)
	AuthMethod        string    `json:"auth_method"`        // Detected: "ECSRP-5" or "Legacy"
	AllowedClientIPs  []string  `json:"allowed_client_ips"` // CIDR networks allowed (empty = allow all)
	CredentialsValid  bool      `json:"credentials_valid"`  // Last validation succeeded
	LastValidated     time.Time `json:"last_validated"`     // Last credential validation
	ValidationError   string    `json:"validation_error"`   // Error if credentials invalid
	LastConnected     time.Time `json:"last_connected"`     // Last successful connection
	CreatedAt         time.Time `json:"created_at"`
	UpdatedAt         time.Time `json:"updated_at"`
	Enabled           bool      `json:"enabled"` // Session enabled/disabled
}

// WebSSHSession represents a saved WebSSH session configuration for a peer.
// Similar to WinboxSession, these are persisted in LMDB and can be reconnected.
type WebSSHSession struct {
	ID                string    `json:"id"`                 // Unique session ID (UUID)
	Name              string    `json:"name"`               // Human-readable session name
	Port              int       `json:"port"`               // SSH port (default 22)
	EncryptedUsername []byte    `json:"encrypted_username"` // SSH username (AES-256-GCM encrypted)
	EncryptedPassword []byte    `json:"encrypted_password"` // SSH password (AES-256-GCM encrypted)
	TerminalRows      int       `json:"terminal_rows"`      // Terminal rows (default 24)
	TerminalCols      int       `json:"terminal_cols"`      // Terminal cols (default 80)
	LastConnected     time.Time `json:"last_connected"`     // Last successful connection
	CreatedAt         time.Time `json:"created_at"`
	UpdatedAt         time.Time `json:"updated_at"`
	Enabled           bool      `json:"enabled"` // Session enabled/disabled
}

// SSHSessionCommand represents a command executed during an SSH session.
type SSHSessionCommand struct {
	Command   string    `json:"command"`   // Command text
	Timestamp time.Time `json:"timestamp"` // When executed (UTC)
}

// SSHActivity represents a single SSH session connection for audit logging.
// Last 10 sessions are kept per peer.
type SSHActivity struct {
	SessionID  string              `json:"session_id"`  // WebSSH session ID
	UserAgent  string              `json:"user_agent"`  // Browser/client user agent
	ClientIP   string              `json:"client_ip"`   // Client's public IP address
	Timestamp  time.Time           `json:"timestamp"`   // Connection start time (UTC)
	EndTime    time.Time           `json:"end_time"`    // Connection end time (UTC), zero if still active
	Username   string              `json:"username"`    // SSH username used
	Commands   []SSHSessionCommand `json:"commands"`    // Last 10 commands executed in this session
	BytesSent  uint64              `json:"bytes_sent"`  // Total bytes sent
	BytesRecv  uint64              `json:"bytes_recv"`  // Total bytes received
	DurationMs int64               `json:"duration_ms"` // Session duration in milliseconds
}

// WinboxActivity represents a single Winbox login for audit logging.
// Last 50 logins are kept per peer.
type WinboxActivity struct {
	SessionName string    `json:"session_name"` // Winbox session name used
	Username    string    `json:"username"`     // Account username who connected
	ClientIP    string    `json:"client_ip"`    // Client's public IP address
	Timestamp   time.Time `json:"timestamp"`    // Connection time (UTC)
	EndTime     time.Time `json:"end_time"`     // Disconnection time (UTC), zero if still active
	DurationMs  int64     `json:"duration_ms"`  // Session duration in milliseconds
	RomonMode   bool      `json:"romon_mode"`   // True if connected via RoMON
}

// PeerMetadata stores additional information about a peer.
type PeerMetadata struct {
	ID         string    `json:"id"`          // Peer ID (public key)
	AccountID  string    `json:"account_id"`  // Parent account
	Name       string    `json:"name"`        // Human-readable name
	AssignedIP string    `json:"assigned_ip"` // IP address
	AllowedIPs []string  `json:"allowed_ips"` // Allowed IP ranges
	Tags       []string  `json:"tags"`        // Peer tags
	PrivateKey string    `json:"private_key"` // For regenerating config
	CreatedAt  time.Time `json:"created_at"`
	UpdatedAt  time.Time `json:"updated_at"`

	// Online status (checked from tenant device)
	IsOnline          bool      `json:"is_online"`           // True if peer has recent handshake
	LastHandshakeTime time.Time `json:"last_handshake_time"` // Last successful WireGuard handshake
	LastSeenAt        time.Time `json:"last_seen_at"`        // Last time peer was seen online
	Endpoint          string    `json:"endpoint"`            // Peer's public IP and port
	RxBytes           int64     `json:"rx_bytes"`            // Bytes received from peer
	TxBytes           int64     `json:"tx_bytes"`            // Bytes sent to peer

	// Monthly uptime history (30 days of 1-hour intervals = 720 bits)
	UptimeHistory []byte `json:"uptime_history,omitempty"`

	// WebSSH consumer state (userspace mode only)
	WebSSHConsumerActive bool      `json:"webssh_consumer_active"` // True if SSH consumer is running
	WebSSHConsumerPort   int       `json:"webssh_consumer_port"`   // Local port for SSH consumer
	WebSSHLinkActive     bool      `json:"webssh_link_active"`     // True if valid WebSSH link exists
	WebSSHLinkExpiry     time.Time `json:"webssh_link_expiry"`     // When the WebSSH link expires

	// Winbox: port detection flag
	HasWinbox bool `json:"has_winbox"` // Winbox port 8291 detected on this peer

	// Winbox sessions (multiple sessions per peer)
	WinboxSessions []WinboxSession `json:"winbox_sessions"` // All Winbox sessions for this peer

	// WebSSH sessions (multiple sessions per peer) - persisted for reconnection
	WebSSHSessions []WebSSHSession `json:"webssh_sessions"` // All WebSSH sessions for this peer

	// Activity logging - persistent audit trail
	SSHActivities    []SSHActivity    `json:"ssh_activities,omitempty"`    // Last 10 SSH sessions
	WinboxActivities []WinboxActivity `json:"winbox_activities,omitempty"` // Last 50 Winbox logins

	// Port scan cache (scanned once per day)
	LastPortScan       time.Time `json:"last_port_scan"`                  // When ports were last scanned
	CachedPortScanJSON []byte    `json:"cached_port_scan_json,omitempty"` // JSON of userspace.ScanResult
	ScannedSSHPort     int       `json:"scanned_ssh_port,omitempty"`      // Detected SSH port from last scan (0 = not found)
	ScannedWinboxPort  int       `json:"scanned_winbox_port,omitempty"`   // Detected Winbox port from last scan (0 = not found)
	LastPortScanTime   time.Time `json:"last_port_scan_time"`             // Timestamp of last port scan (for daily scheduling)
	ScanProgress       int       `json:"scan_progress,omitempty"`         // Current port scan progress (0-100)

	// Offline notification settings
	NotificationEnabled      bool      `json:"notification_enabled"`                 // True if offline notifications are enabled for this peer
	FirstSeenOnline          time.Time `json:"first_seen_online,omitempty"`          // First time this peer was ever seen online (used to skip never-connected peers)
	LastOnlineAt             time.Time `json:"last_online_at,omitempty"`             // Last time peer transitioned from offline to online
	FailedHandshakes         int       `json:"failed_handshakes"`                    // Count of consecutive failed handshake attempts
	LastNotificationSentAt   time.Time `json:"last_notification_sent_at,omitempty"`  // Last time an offline notification was sent
	OfflineNotificationState string    `json:"offline_notification_state,omitempty"` // "none", "pending", "sent" - tracks notification state

	// DEPRECATED: Legacy single-session fields (kept for migration, will be removed)
	RouterIP                    string    `json:"router_ip,omitempty"`
	VirtualWinboxUsername       string    `json:"virtual_winbox_username,omitempty"`
	WinboxAuthMethod            string    `json:"winbox_auth_method,omitempty"`
	EncryptedRealWinboxUsername []byte    `json:"encrypted_real_winbox_username,omitempty"`
	EncryptedRealWinboxPassword []byte    `json:"encrypted_real_winbox_password,omitempty"`
	WinboxCredentialsValid      bool      `json:"winbox_credentials_valid,omitempty"`
	WinboxLastProbed            time.Time `json:"winbox_last_probed,omitempty"`
	WinboxCredentialError       string    `json:"winbox_credential_error,omitempty"`
	WinboxLastConnected         time.Time `json:"winbox_last_connected,omitempty"`
	WinboxAllowedClientIPs      []string  `json:"winbox_allowed_client_ips,omitempty"`
}

// SessionLocation stores the location of a Winbox session for O(1) lookup.
type SessionLocation struct {
	AccountID string `json:"account_id"`
	PeerID    string `json:"peer_id"`
	SessionID string `json:"session_id"`
}

// WinboxSessionLookupResult contains the result of an O(1) Winbox session lookup.
type WinboxSessionLookupResult struct {
	AccountID string
	PeerID    string
	Session   *WinboxSession // The Winbox session
	Peer      *PeerMetadata  // Full peer metadata
}

const (
	MaxSSHActivities    = 10 // Keep last 10 SSH sessions per peer
	MaxWinboxActivities = 50 // Keep last 50 Winbox logins per peer
	MaxCommandsPerSSH   = 10 // Keep last 10 commands per SSH session
)

// AddSSHActivity adds a new SSH activity to the peer, keeping only the last MaxSSHActivities.
// Thread-safe: caller must handle locking if needed.
func (p *PeerMetadata) AddSSHActivity(activity SSHActivity) {
	// Prepend new activity (most recent first)
	p.SSHActivities = append([]SSHActivity{activity}, p.SSHActivities...)

	// Trim to max size
	if len(p.SSHActivities) > MaxSSHActivities {
		p.SSHActivities = p.SSHActivities[:MaxSSHActivities]
	}
}

// UpdateSSHActivity updates an existing SSH activity by session ID.
// Returns true if found and updated, false otherwise.
func (p *PeerMetadata) UpdateSSHActivity(sessionID string, updateFn func(*SSHActivity)) bool {
	for i := range p.SSHActivities {
		if p.SSHActivities[i].SessionID == sessionID {
			updateFn(&p.SSHActivities[i])
			return true
		}
	}
	return false
}

// AddCommandToSSHActivity adds a command to an SSH activity, keeping only the last MaxCommandsPerSSH.
func (p *PeerMetadata) AddCommandToSSHActivity(sessionID string, cmd SSHSessionCommand) bool {
	for i := range p.SSHActivities {
		if p.SSHActivities[i].SessionID == sessionID {
			// Append command
			p.SSHActivities[i].Commands = append(p.SSHActivities[i].Commands, cmd)

			// Trim to max size
			if len(p.SSHActivities[i].Commands) > MaxCommandsPerSSH {
				// Keep only the last MaxCommandsPerSSH commands
				p.SSHActivities[i].Commands = p.SSHActivities[i].Commands[len(p.SSHActivities[i].Commands)-MaxCommandsPerSSH:]
			}
			return true
		}
	}
	return false
}

// AddWinboxActivity adds a new Winbox activity to the peer, keeping only the last MaxWinboxActivities.
// Thread-safe: caller must handle locking if needed.
func (p *PeerMetadata) AddWinboxActivity(activity WinboxActivity) {
	// Prepend new activity (most recent first)
	p.WinboxActivities = append([]WinboxActivity{activity}, p.WinboxActivities...)

	// Trim to max size
	if len(p.WinboxActivities) > MaxWinboxActivities {
		p.WinboxActivities = p.WinboxActivities[:MaxWinboxActivities]
	}
}

// UpdateWinboxActivity updates an existing Winbox activity by matching session name and timestamp.
// Returns true if found and updated, false otherwise.
// Uses a 1-second tolerance for timestamp matching to handle serialization differences.
func (p *PeerMetadata) UpdateWinboxActivity(sessionName string, timestamp time.Time, updateFn func(*WinboxActivity)) bool {
	// Use UTC for comparison to avoid timezone issues
	timestampUTC := timestamp.UTC()

	for i := range p.WinboxActivities {
		if p.WinboxActivities[i].SessionName == sessionName {
			// Use 1-second tolerance for timestamp matching (handles JSON serialization differences)
			storedTimestampUTC := p.WinboxActivities[i].Timestamp.UTC()
			timeDiff := timestampUTC.Sub(storedTimestampUTC)
			if timeDiff < 0 {
				timeDiff = -timeDiff
			}
			if timeDiff <= time.Second {
				updateFn(&p.WinboxActivities[i])
				return true
			}
		}
	}
	return false
}

// GetSSHActivityBySessionID finds an SSH activity by session ID.
func (p *PeerMetadata) GetSSHActivityBySessionID(sessionID string) *SSHActivity {
	for i := range p.SSHActivities {
		if p.SSHActivities[i].SessionID == sessionID {
			return &p.SSHActivities[i]
		}
	}
	return nil
}

// ==================== WebSSH Session CRUD Operations ====================

// AddWebSSHSession adds a new WebSSH session to the peer's saved sessions.
func (p *PeerMetadata) AddWebSSHSession(session WebSSHSession) {
	p.WebSSHSessions = append(p.WebSSHSessions, session)
	p.UpdatedAt = time.Now()
}

// GetWebSSHSession returns a WebSSH session by ID, or nil if not found.
func (p *PeerMetadata) GetWebSSHSession(sessionID string) *WebSSHSession {
	for i := range p.WebSSHSessions {
		if p.WebSSHSessions[i].ID == sessionID {
			return &p.WebSSHSessions[i]
		}
	}
	return nil
}

// UpdateWebSSHSession updates a WebSSH session by ID using the provided update function.
// Returns true if found and updated, false otherwise.
func (p *PeerMetadata) UpdateWebSSHSession(sessionID string, updateFn func(*WebSSHSession)) bool {
	for i := range p.WebSSHSessions {
		if p.WebSSHSessions[i].ID == sessionID {
			updateFn(&p.WebSSHSessions[i])
			p.WebSSHSessions[i].UpdatedAt = time.Now()
			p.UpdatedAt = time.Now()
			return true
		}
	}
	return false
}

// DeleteWebSSHSession removes a WebSSH session by ID.
// Returns true if found and deleted, false otherwise.
func (p *PeerMetadata) DeleteWebSSHSession(sessionID string) bool {
	for i := range p.WebSSHSessions {
		if p.WebSSHSessions[i].ID == sessionID {
			p.WebSSHSessions = append(p.WebSSHSessions[:i], p.WebSSHSessions[i+1:]...)
			p.UpdatedAt = time.Now()
			return true
		}
	}
	return false
}

// GetEnabledWebSSHSessions returns all enabled WebSSH sessions for reconnection.
func (p *PeerMetadata) GetEnabledWebSSHSessions() []WebSSHSession {
	var enabled []WebSSHSession
	for _, session := range p.WebSSHSessions {
		if session.Enabled {
			enabled = append(enabled, session)
		}
	}
	return enabled
}
