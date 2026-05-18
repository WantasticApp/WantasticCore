package server

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"WantasticCore/internal/store"

	"github.com/redis/go-redis/v9"
	"github.com/rs/zerolog/log"
)

// WinboxSession represents a single Winbox access session for a peer.
type WinboxSession struct {
	ID            string `json:"id"`
	PeerID        string `json:"peer_id,omitempty"` // Added for standalone listing context
	Name          string `json:"name"`
	RouterIP      string `json:"router_ip"`
	Port          int    `json:"port"`
	AccessToken   string `json:"access_token"`
	PasswordToken string `json:"password_token"`

	// ... (rest is same, but replace_file_content needs block) ...
	// Actually better to use separate Replace for struct and separate for mapper to keep context small.
	// But mapper is further down.

	EncryptedUsername        []byte    `json:"encrypted_username"`
	EncryptedPassword        []byte    `json:"encrypted_password"`
	AuthMethod               string    `json:"auth_method"`
	AllowedClientIPs         []string  `json:"allowed_client_ips"`
	CredentialsValid         bool      `json:"credentials_valid"`
	LastValidated            time.Time `json:"last_validated"`
	ValidationError          string    `json:"validation_error"`
	RouterOSAPIVerified      bool      `json:"routeros_api_verified"`
	RouterOSAPILastValidated time.Time `json:"routeros_api_last_validated"`
	RouterOSAPIError         string    `json:"routeros_api_error"`
	RouterOSAPIPort          int       `json:"routeros_api_port"`
	RouterOSAPITLS           bool      `json:"routeros_api_tls"`
	LastConnected            time.Time `json:"last_connected"`
	CreatedAt                time.Time `json:"created_at"`
	UpdatedAt                time.Time `json:"updated_at"`
	Enabled                  bool      `json:"enabled"`
}

// WebSSHSession represents a saved WebSSH session configuration for a peer.
type WebSSHSession struct {
	ID                 string    `json:"id"`
	Name               string    `json:"name"`
	Port               int       `json:"port"`
	EncryptedUsername  []byte    `json:"encrypted_username"`
	EncryptedPassword  []byte    `json:"encrypted_password"`
	TerminalRows       int       `json:"terminal_rows"`
	TerminalCols       int       `json:"terminal_cols"`
	LastConnected      time.Time `json:"last_connected"`
	CreatedAt          time.Time `json:"created_at"`
	UpdatedAt          time.Time `json:"updated_at"`
	Enabled            bool      `json:"enabled"`
	History            []byte    `json:"history,omitempty"`
	HostKeyFingerprint string    `json:"host_key_fingerprint,omitempty"`
	HostKeyAlgorithm   string    `json:"host_key_algorithm,omitempty"`
	CompatibilityMode  string    `json:"compatibility_mode,omitempty"`
}

// SSHSessionCommand represents a command executed during an SSH session.
type SSHSessionCommand struct {
	Command   string    `json:"command"`
	Timestamp time.Time `json:"timestamp"`
}

// SSHActivity represents a single SSH session connection for audit logging.
type SSHActivity struct {
	SessionID  string              `json:"session_id"`
	UserAgent  string              `json:"user_agent"`
	ClientIP   string              `json:"client_ip"`
	Timestamp  time.Time           `json:"timestamp"`
	EndTime    time.Time           `json:"end_time"`
	Username   string              `json:"username"`
	Commands   []SSHSessionCommand `json:"commands"`
	BytesSent  uint64              `json:"bytes_sent"`
	BytesRecv  uint64              `json:"bytes_recv"`
	DurationMs int64               `json:"duration_ms"`
}

// WinboxActivity represents a single Winbox login for audit logging.
type WinboxActivity struct {
	SessionName string    `json:"session_name"`
	Username    string    `json:"username"`
	ClientIP    string    `json:"client_ip"`
	Timestamp   time.Time `json:"timestamp"`
	EndTime     time.Time `json:"end_time"`
	DurationMs  int64     `json:"duration_ms"`
	RomonMode   bool      `json:"romon_mode"`
}

// PeerMetadata stores additional information about a peer.
type PeerMetadata struct {
	ID                        string           `json:"id"`
	AccountID                 string           `json:"account_id"`
	Name                      string           `json:"name"`
	AssignedIP                string           `json:"assigned_ip"`
	AllowedIPs                []string         `json:"allowed_ips"`
	Tags                      []string         `json:"tags"`
	PrivateKey                string           `json:"private_key"`
	CreatedAt                 time.Time        `json:"created_at"`
	UpdatedAt                 time.Time        `json:"updated_at"`
	ConnectionType            string           `json:"connection_type,omitempty"`
	ProtocolLocked            bool             `json:"protocol_locked,omitempty"`
	WireGuardPublicKey        string           `json:"wireguard_public_key,omitempty"`
	WireGuardPrivateKey       string           `json:"wireguard_private_key,omitempty"`
	IPsecIdentity             string           `json:"ipsec_identity,omitempty"`
	IPsecPSK                  string           `json:"ipsec_psk,omitempty"`
	IPsecPeerID               string           `json:"ipsec_peer_id,omitempty"`
	IsOnline                  bool             `json:"is_online"`
	LastHandshakeTime         time.Time        `json:"last_handshake_time"`
	LastSeenAt                time.Time        `json:"last_seen_at"`
	Endpoint                  string           `json:"endpoint"`
	UptimeHistory             []byte           `json:"uptime_history,omitempty"`
	RxBytes                   int64            `json:"rx_bytes"`
	TxBytes                   int64            `json:"tx_bytes"`
	WebSSHConsumerActive      bool             `json:"webssh_consumer_active"`
	WebSSHConsumerPort        int              `json:"webssh_consumer_port"`
	WebSSHLinkActive          bool             `json:"webssh_link_active"`
	WebSSHLinkExpiry          time.Time        `json:"webssh_link_expiry"`
	HasWinbox                 bool             `json:"has_winbox"`
	EncryptedRouterOSUsername []byte           `json:"encrypted_routeros_username,omitempty"`
	EncryptedRouterOSPassword []byte           `json:"encrypted_routeros_password,omitempty"`
	RouterOSCredentialSource  string           `json:"routeros_credential_source,omitempty"`
	RouterOSAPIVerified       bool             `json:"routeros_api_verified"`
	RouterOSAPILastValidated  time.Time        `json:"routeros_api_last_validated"`
	RouterOSAPIError          string           `json:"routeros_api_error,omitempty"`
	RouterOSAPIPort           int              `json:"routeros_api_port,omitempty"`
	RouterOSAPITLS            bool             `json:"routeros_api_tls,omitempty"`
	WinboxSessions            []WinboxSession  `json:"winbox_sessions"`
	WebSSHSessions            []WebSSHSession  `json:"webssh_sessions"`
	SSHActivities             []SSHActivity    `json:"ssh_activities,omitempty"`
	WinboxActivities          []WinboxActivity `json:"winbox_activities,omitempty"`

	// Port Scan related fields (restored)
	LastPortScan             time.Time       `json:"last_port_scan,omitempty"`
	CachedPortScanJSON       []byte          `json:"cached_port_scan_json,omitempty"`
	ScannedSSHPort           int             `json:"scanned_ssh_port,omitempty"`
	ScannedWinboxPort        int             `json:"scanned_winbox_port,omitempty"`
	ScannedWebPort           int             `json:"scanned_web_port,omitempty"`
	LastPortScanTime         time.Time       `json:"last_port_scan_time,omitempty"`
	NotificationEnabled      bool            `json:"notification_enabled,omitempty"`
	FirstSeenOnline          time.Time       `json:"first_seen_online,omitempty"`
	LastOnlineAt             time.Time       `json:"last_online_at,omitempty"`
	FailedHandshakes         int             `json:"failed_handshakes,omitempty"`
	LastNotificationSentAt   time.Time       `json:"last_notification_sent_at,omitempty"`
	OfflineNotificationState string          `json:"offline_notification_state,omitempty"`
	ExtendedStats            json.RawMessage `json:"extended_stats,omitempty"`
	Notes                    string          `json:"notes,omitempty"`
	ClientType               string          `json:"client_type,omitempty"` // 'native' or 'wantasticd'
	IsWantasticd             bool            `json:"is_wantasticd,omitempty"`
	AgentModel               string          `json:"agent_model,omitempty"`
	AgentVersion             string          `json:"agent_version,omitempty"`

	// Transients that were in original struct
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

// PeerStore manages peer metadata persistence using the unified repository and Redis cache.
type PeerStore struct {
	repo  store.PeerRepository
	redis *redis.Client
}

// NewPeerStore creates a new peer metadata store.
func NewPeerStore(repo store.PeerRepository, redisClient *redis.Client) *PeerStore {
	return &PeerStore{
		repo:  repo,
		redis: redisClient,
	}
}

// Converters

func toStorePeer(p *PeerMetadata) *store.PeerData {
	return &store.PeerData{
		ID:                        p.ID, // PublicKey
		AccountID:                 p.AccountID,
		Name:                      p.Name,
		AssignedIP:                p.AssignedIP,
		AllowedIPs:                p.AllowedIPs,
		Tags:                      p.Tags,
		PrivateKey:                p.PrivateKey,
		CreatedAt:                 p.CreatedAt,
		UpdatedAt:                 p.UpdatedAt,
		IsOnline:                  p.IsOnline,
		LastHandshakeTime:         p.LastHandshakeTime,
		LastSeenAt:                p.LastSeenAt,
		Endpoint:                  p.Endpoint,
		UptimeHistory:             p.UptimeHistory,
		RxBytes:                   p.RxBytes,
		TxBytes:                   p.TxBytes,
		WebSSHConsumerActive:      p.WebSSHConsumerActive,
		WebSSHConsumerPort:        p.WebSSHConsumerPort,
		WebSSHLinkActive:          p.WebSSHLinkActive,
		WebSSHLinkExpiry:          p.WebSSHLinkExpiry,
		HasWinbox:                 p.HasWinbox,
		EncryptedRouterOSUsername: p.EncryptedRouterOSUsername,
		EncryptedRouterOSPassword: p.EncryptedRouterOSPassword,
		RouterOSCredentialSource:  p.RouterOSCredentialSource,
		RouterOSAPIVerified:       p.RouterOSAPIVerified,
		RouterOSAPILastValidated:  p.RouterOSAPILastValidated,
		RouterOSAPIError:          p.RouterOSAPIError,
		RouterOSAPIPort:           p.RouterOSAPIPort,
		RouterOSAPITLS:            p.RouterOSAPITLS,
		LastPortScan:              p.LastPortScan,
		CachedPortScanJSON:        p.CachedPortScanJSON,
		ScannedSSHPort:            p.ScannedSSHPort,
		ScannedWinboxPort:         p.ScannedWinboxPort,
		LastPortScanTime:          p.LastPortScanTime,
		NotificationEnabled:       p.NotificationEnabled,
		FirstSeenOnline:           p.FirstSeenOnline,
		LastOnlineAt:              p.LastOnlineAt,
		FailedHandshakes:          p.FailedHandshakes,
		LastNotificationSentAt:    p.LastNotificationSentAt,
		OfflineNotificationState:  p.OfflineNotificationState,
		Notes:                     p.Notes,
		ClientType:                p.ClientType,
		IsWantasticd:              p.IsWantasticd,
		AgentModel:                p.AgentModel,
		AgentVersion:              p.AgentVersion,
	}
}

func toDomainPeer(d *store.PeerData) *PeerMetadata {
	if d == nil {
		return nil
	}
	pm := &PeerMetadata{
		ID:                        d.ID,
		AccountID:                 d.AccountID,
		Name:                      d.Name,
		AssignedIP:                d.AssignedIP,
		AllowedIPs:                d.AllowedIPs,
		Tags:                      d.Tags,
		PrivateKey:                d.PrivateKey,
		CreatedAt:                 d.CreatedAt,
		UpdatedAt:                 d.UpdatedAt,
		IsOnline:                  d.IsOnline,
		LastHandshakeTime:         d.LastHandshakeTime,
		LastSeenAt:                d.LastSeenAt,
		Endpoint:                  d.Endpoint,
		UptimeHistory:             d.UptimeHistory,
		RxBytes:                   d.RxBytes,
		TxBytes:                   d.TxBytes,
		WebSSHConsumerActive:      d.WebSSHConsumerActive,
		WebSSHConsumerPort:        d.WebSSHConsumerPort,
		WebSSHLinkActive:          d.WebSSHLinkActive,
		WebSSHLinkExpiry:          d.WebSSHLinkExpiry,
		HasWinbox:                 d.HasWinbox,
		EncryptedRouterOSUsername: d.EncryptedRouterOSUsername,
		EncryptedRouterOSPassword: d.EncryptedRouterOSPassword,
		RouterOSCredentialSource:  d.RouterOSCredentialSource,
		RouterOSAPIVerified:       d.RouterOSAPIVerified,
		RouterOSAPILastValidated:  d.RouterOSAPILastValidated,
		RouterOSAPIError:          d.RouterOSAPIError,
		RouterOSAPIPort:           d.RouterOSAPIPort,
		RouterOSAPITLS:            d.RouterOSAPITLS,
		LastPortScan:              d.LastPortScan,
		CachedPortScanJSON:        d.CachedPortScanJSON,
		ScannedSSHPort:            d.ScannedSSHPort,
		ScannedWinboxPort:         d.ScannedWinboxPort,
		LastPortScanTime:          d.LastPortScanTime,
		NotificationEnabled:       d.NotificationEnabled,
		FirstSeenOnline:           d.FirstSeenOnline,
		LastOnlineAt:              d.LastOnlineAt,
		FailedHandshakes:          d.FailedHandshakes,
		LastNotificationSentAt:    d.LastNotificationSentAt,
		OfflineNotificationState:  d.OfflineNotificationState,
		Notes:                     d.Notes,
		ClientType:                d.ClientType,
		IsWantasticd:              d.IsWantasticd,
		AgentModel:                d.AgentModel,
		AgentVersion:              d.AgentVersion,

		// Slices need to be populated separately if needed
		WinboxSessions:   make([]WinboxSession, 0),
		WebSSHSessions:   make([]WebSSHSession, 0),
		SSHActivities:    make([]SSHActivity, len(d.SSHActivities)),
		WinboxActivities: make([]WinboxActivity, len(d.WinboxActivities)),
	}

	for i, sa := range d.SSHActivities {
		commands := make([]SSHSessionCommand, len(sa.Commands))
		for j, cmd := range sa.Commands {
			commands[j] = SSHSessionCommand{
				Command:   cmd.Command,
				Timestamp: cmd.Timestamp,
			}
		}
		pm.SSHActivities[i] = SSHActivity{
			SessionID:  sa.SessionID,
			UserAgent:  sa.UserAgent,
			ClientIP:   sa.ClientIP,
			Timestamp:  sa.Timestamp,
			EndTime:    sa.EndTime,
			Username:   sa.Username,
			Commands:   commands,
			BytesSent:  sa.BytesSent,
			BytesRecv:  sa.BytesRecv,
			DurationMs: sa.DurationMs,
		}
	}

	for i, wa := range d.WinboxActivities {
		pm.WinboxActivities[i] = WinboxActivity{
			SessionName: wa.SessionName,
			Username:    wa.Username,
			ClientIP:    wa.ClientIP,
			Timestamp:   wa.Timestamp,
			EndTime:     wa.EndTime,
			DurationMs:  wa.DurationMs,
			RomonMode:   wa.RomonMode,
		}
	}

	return pm
}

func toStoreWinboxSession(s WinboxSession, accountID, peerID string) *store.WinboxSessionData {
	return &store.WinboxSessionData{
		ID:                       s.ID,
		PeerID:                   peerID,
		AccountID:                accountID,
		Name:                     s.Name,
		RouterIP:                 s.RouterIP,
		Port:                     s.Port,
		AccessToken:              s.AccessToken,
		PasswordToken:            s.PasswordToken,
		EncryptedUsername:        s.EncryptedUsername,
		EncryptedPassword:        s.EncryptedPassword,
		AuthMethod:               s.AuthMethod,
		AllowedClientIPs:         s.AllowedClientIPs,
		CredentialsValid:         s.CredentialsValid,
		LastValidated:            s.LastValidated,
		ValidationError:          s.ValidationError,
		RouterOSAPIVerified:      s.RouterOSAPIVerified,
		RouterOSAPILastValidated: s.RouterOSAPILastValidated,
		RouterOSAPIError:         s.RouterOSAPIError,
		RouterOSAPIPort:          s.RouterOSAPIPort,
		RouterOSAPITLS:           s.RouterOSAPITLS,
		LastConnected:            s.LastConnected,
		CreatedAt:                s.CreatedAt,
		UpdatedAt:                s.UpdatedAt,
		Enabled:                  s.Enabled,
	}
}

func toDomainWinboxSession(d *store.WinboxSessionData) WinboxSession {
	return WinboxSession{
		ID:                       d.ID,
		PeerID:                   d.PeerID,
		Name:                     d.Name,
		RouterIP:                 d.RouterIP,
		Port:                     d.Port,
		AccessToken:              d.AccessToken,
		PasswordToken:            d.PasswordToken,
		EncryptedUsername:        d.EncryptedUsername,
		EncryptedPassword:        d.EncryptedPassword,
		AuthMethod:               d.AuthMethod,
		AllowedClientIPs:         d.AllowedClientIPs,
		CredentialsValid:         d.CredentialsValid,
		LastValidated:            d.LastValidated,
		ValidationError:          d.ValidationError,
		RouterOSAPIVerified:      d.RouterOSAPIVerified,
		RouterOSAPILastValidated: d.RouterOSAPILastValidated,
		RouterOSAPIError:         d.RouterOSAPIError,
		RouterOSAPIPort:          d.RouterOSAPIPort,
		RouterOSAPITLS:           d.RouterOSAPITLS,
		LastConnected:            d.LastConnected,
		CreatedAt:                d.CreatedAt,
		UpdatedAt:                d.UpdatedAt,
		Enabled:                  d.Enabled,
	}
}

// ... Similar converters for other types ...

// GetAccessTokenIndexSize returns index size (stub for Postgres refactor).
func (s *PeerStore) GetAccessTokenIndexSize() int {
	return 0 // TODO: Query DB count if needed for metrics
}

// WinboxSessionLookupResult contains the result of a Winbox session lookup.
type WinboxSessionLookupResult struct {
	AccountID string
	PeerID    string
	Session   *WinboxSession
	Peer      *PeerMetadata
}

// GetWinboxSessionByAccessToken performs lookup by access token.
func (s *PeerStore) GetWinboxSessionByAccessToken(accessToken string) (*WinboxSessionLookupResult, error) {
	res, err := s.repo.LookupWinboxByToken(accessToken)
	if err != nil {
		return nil, err
	}

	wSession := toDomainWinboxSession(res.Session)
	peer := toDomainPeer(res.Peer)

	return &WinboxSessionLookupResult{
		AccountID: res.AccountID,
		PeerID:    res.PeerID,
		Session:   &wSession,
		Peer:      peer,
	}, nil
}

// SavePeer stores peer metadata.
func (s *PeerStore) SavePeer(peer *PeerMetadata) error {
	p := toStorePeer(peer)
	if err := s.repo.Save(p); err != nil {
		return err
	}

	// Update cache
	if s.redis != nil {
		ctx := context.Background()
		key := fmt.Sprintf("peer:%s", peer.ID)
		if data, err := json.Marshal(peer); err == nil {
			s.redis.Set(ctx, key, data, 1*time.Hour)
		}
	}
	return nil
}

// UpdatePeerLastSeenCache updates the peer's last seen time in Redis (fast path).
func (s *PeerStore) UpdatePeerLastSeenCache(peerID string, lastSeen time.Time) error {
	if s.redis == nil {
		return nil
	}
	key := fmt.Sprintf("peer:last_handshake:%s", peerID)
	return s.redis.Set(context.Background(), key, lastSeen.Format(time.RFC3339), 24*time.Hour).Err()
}

// UpdatePeerStatsCache updates only the ExtendedStats in Redis to avoid DB writes for high-frequency stats.
func (s *PeerStore) UpdatePeerStatsCache(accountID, peerID string, stats json.RawMessage) error {
	if s.redis == nil {
		return nil
	}
	ctx := context.Background()
	key := fmt.Sprintf("peer:%s", peerID)

	// Optimistic locking / patches is hard with simple JSON blob.
	// We'll fetch, update, set.
	// Note: race condition possible but acceptable for stats.
	val, err := s.redis.Get(ctx, key).Bytes()
	if err != nil {
		// If not in cache, fallback to load from DB?
		// Or just ignore updating stats if peer not active in cache?
		// Usually peer should be in cache if active.
		// Let's try to load from DB to prime cache.
		if _, err := s.GetPeer(accountID, peerID); err != nil {
			return err
		}
		// Retry get from cache
		val, err = s.redis.Get(ctx, key).Bytes()
		if err != nil {
			return err
		}
	}

	var peer PeerMetadata
	if err := json.Unmarshal(val, &peer); err != nil {
		return err
	}

	// Update stats
	peer.ExtendedStats = stats

	// Save back to Redis
	data, err := json.Marshal(peer)
	if err != nil {
		return err
	}
	return s.redis.Set(ctx, key, data, 24*time.Hour).Err()
}

// GetPeer retrieves peer metadata by account and peer ID.
func (s *PeerStore) GetPeer(accountID, peerID string) (*PeerMetadata, error) {
	// Try Redis first
	if s.redis != nil {
		ctx := context.Background()
		key := fmt.Sprintf("peer:%s", peerID)
		if val, err := s.redis.Get(ctx, key).Bytes(); err == nil {
			var peer PeerMetadata
			if err := json.Unmarshal(val, &peer); err == nil {
				// Verify account ID matches to be safe (though key is unique by peerID usually)
				if peer.AccountID == accountID {
					// Check for fast-path LastHandshakeTime update
					handshakeKey := fmt.Sprintf("peer:last_handshake:%s", peerID)
					if handshakeVal, err := s.redis.Get(ctx, handshakeKey).Result(); err == nil {
						if t, err := time.Parse(time.RFC3339, handshakeVal); err == nil {
							if t.After(peer.LastHandshakeTime) {
								peer.LastHandshakeTime = t
							}
						}
					}
					// If no live extended_stats, fall back to the most recent history slot.
					// History is a Redis list capped at 4 entries with 7-day TTL so the
					// portal can display last-known metrics even when the device is offline.
					if len(peer.ExtendedStats) == 0 {
						histKey := fmt.Sprintf("peer:stats_history:%s", peerID)
						if hist, err := s.redis.LIndex(ctx, histKey, 0).Bytes(); err == nil && len(hist) > 0 {
							peer.ExtendedStats = json.RawMessage(hist)
						}
					}
					return &peer, nil
				}
			}
		}
	}

	d, err := s.repo.Get(accountID, peerID)
	if err != nil {
		return nil, err
	}
	peer := toDomainPeer(d)

	// Populate sessions/activities
	// Winbox
	wbList, err := s.repo.ListWinboxSessions(accountID, peerID)
	if err == nil {
		for _, w := range wbList {
			peer.WinboxSessions = append(peer.WinboxSessions, toDomainWinboxSession(w))
		}
	}

	// WebSSH
	wssList, err := s.repo.ListWebSSHSessions(accountID, peerID)
	if err == nil {
		for _, w := range wssList {
			peer.WebSSHSessions = append(peer.WebSSHSessions, WebSSHSession{
				ID:                 w.ID,
				Name:               w.Name,
				Port:               w.Port,
				EncryptedUsername:  w.EncryptedUsername,
				EncryptedPassword:  w.EncryptedPassword,
				TerminalRows:       w.TerminalRows,
				TerminalCols:       w.TerminalCols,
				LastConnected:      w.LastConnected,
				CreatedAt:          w.CreatedAt,
				UpdatedAt:          w.UpdatedAt,
				Enabled:            w.Enabled,
				History:            w.History,
				HostKeyFingerprint: w.HostKeyFingerprint,
				HostKeyAlgorithm:   w.HostKeyAlgorithm,
				CompatibilityMode:  w.CompatibilityMode,
			})
		}
	}

	// SSH Activities
	sshActivities, err := s.repo.ListSSHActivities(accountID, peerID, 10)
	if err == nil {
		for _, a := range sshActivities {
			commands := make([]SSHSessionCommand, 0, len(a.Commands))
			for _, cmd := range a.Commands {
				commands = append(commands, SSHSessionCommand{
					Command:   cmd.Command,
					Timestamp: cmd.Timestamp,
				})
			}
			peer.SSHActivities = append(peer.SSHActivities, SSHActivity{
				SessionID:  a.SessionID,
				UserAgent:  a.UserAgent,
				ClientIP:   a.ClientIP,
				Timestamp:  a.Timestamp,
				EndTime:    a.EndTime,
				Username:   a.Username,
				Commands:   commands,
				BytesSent:  a.BytesSent,
				BytesRecv:  a.BytesRecv,
				DurationMs: a.DurationMs,
			})
		}
	}

	// Winbox Activities
	winboxActivities, err := s.repo.ListWinboxActivities(accountID, peerID, 50)
	if err == nil {
		for _, a := range winboxActivities {
			peer.WinboxActivities = append(peer.WinboxActivities, WinboxActivity{
				SessionName: a.SessionName,
				Username:    a.Username,
				ClientIP:    a.ClientIP,
				Timestamp:   a.Timestamp,
				EndTime:     a.EndTime,
				DurationMs:  a.DurationMs,
				RomonMode:   a.RomonMode,
			})
		}
	}

	// Cache full object in Redis
	if s.redis != nil {
		ctx := context.Background()
		key := fmt.Sprintf("peer:%s", peerID)
		if data, err := json.Marshal(peer); err == nil {
			s.redis.Set(ctx, key, data, 1*time.Hour)
		}
	}

	return peer, nil
}

// FindPeer retrieves peer metadata by peer ID without requiring the caller to
// know the owning account ahead of time. This is used by shared-access flows to
// resolve the owner account from the DB/cache first, then enforce access.
func (s *PeerStore) FindPeer(peerID string) (*PeerMetadata, error) {
	if peerID == "" {
		return nil, fmt.Errorf("peer_id required")
	}

	if s.redis != nil {
		ctx := context.Background()
		key := fmt.Sprintf("peer:%s", peerID)
		if val, err := s.redis.Get(ctx, key).Bytes(); err == nil {
			var peer PeerMetadata
			if err := json.Unmarshal(val, &peer); err == nil {
				handshakeKey := fmt.Sprintf("peer:last_handshake:%s", peerID)
				if handshakeVal, err := s.redis.Get(ctx, handshakeKey).Result(); err == nil {
					if t, err := time.Parse(time.RFC3339, handshakeVal); err == nil && t.After(peer.LastHandshakeTime) {
						peer.LastHandshakeTime = t
					}
				}
				return &peer, nil
			}
		}
	}

	d, err := s.repo.FindByPeerID(peerID)
	if err != nil {
		return nil, err
	}

	peer := toDomainPeer(d)

	wbList, err := s.repo.ListWinboxSessions(peer.AccountID, peerID)
	if err == nil {
		for _, w := range wbList {
			peer.WinboxSessions = append(peer.WinboxSessions, toDomainWinboxSession(w))
		}
	}

	if s.redis != nil {
		ctx := context.Background()
		key := fmt.Sprintf("peer:%s", peerID)
		if data, err := json.Marshal(peer); err == nil {
			s.redis.Set(ctx, key, data, 1*time.Hour)
		}
	}

	return peer, nil
}

// ListPeers lists all peers for an account.
// CountPeers returns the authoritative peer count from the DB, bypassing any
// Redis cache. Use this for limit enforcement — never use ListPeers for counts.
func (s *PeerStore) CountPeers(accountID string) (int, error) {
	return s.repo.Count(accountID)
}

func (s *PeerStore) ListPeers(accountID string) ([]*PeerMetadata, error) {
	list, err := s.repo.List(accountID)
	if err != nil {
		return nil, err
	}
	result := make([]*PeerMetadata, len(list))
	for i, d := range list {
		// Optimization: Do NOT load sessions for ListPeers unless necessary.
		// If callers need sessions, they should call GetPeer.
		// However, for backward compatibility, if frontend expects sessions in list...
		// But in RestoreTenants, only ID/IPs are needed.
		// Leaving sessions empty for now to improve perf.
		result[i] = toDomainPeer(d)
	}
	return result, nil
}

// DeletePeer removes peer metadata.
func (s *PeerStore) DeletePeer(accountID, peerID string) error {
	if err := s.repo.Delete(accountID, peerID); err != nil {
		return err
	}

	if s.redis != nil {
		ctx := context.Background()
		s.redis.Del(
			ctx,
			fmt.Sprintf("peer:%s", peerID),
			fmt.Sprintf("peer:last_handshake:%s", peerID),
			fmt.Sprintf("peer:stats_history:%s", peerID),
			fmt.Sprintf("history:%s", peerID),
		)
	}

	return nil
}

func (s *PeerStore) ClearSelectedPeerConfig() {
	// Not implemented here (it's likely a frontend store concept or managed elsewhere)
}

func (s *PeerStore) RecordHandshake(accountID, peerID string, timestamp time.Time, endpoint string) error {
	return s.repo.RecordHandshake(accountID, peerID, timestamp, endpoint)
}

func (s *PeerStore) GetHandshakeHistory(accountID, peerID string, since time.Time) ([]store.PeerHandshakeData, error) {
	// Try Redis cache for history (short TTL to avoid hammering DB on polls)
	// We key by peerID. Since 'since' is typically 30d, caching the result is safe for short duration.
	if s.redis != nil {
		ctx := context.Background()
		key := fmt.Sprintf("history:%s", peerID)
		if val, err := s.redis.Get(ctx, key).Bytes(); err == nil {
			var result []store.PeerHandshakeData
			if err := json.Unmarshal(val, &result); err == nil {
				return result, nil
			}
		}
	}

	data, err := s.repo.GetHandshakeHistory(accountID, peerID, since)
	if err != nil {
		return nil, err
	}
	result := make([]store.PeerHandshakeData, len(data))
	for i, d := range data {
		result[i] = *d
	}

	// Cache result
	if s.redis != nil && len(result) > 0 {
		ctx := context.Background()
		key := fmt.Sprintf("history:%s", peerID)
		if data, err := json.Marshal(result); err == nil {
			s.redis.Set(ctx, key, data, 30*time.Second) // 30s TTL
		}
	}

	return result, nil
}

// DeleteAccountPeers removes all peers for an account.
func (s *PeerStore) DeleteAccountPeers(accountID string) error {
	return s.repo.DeleteByAccount(accountID)
}

func (s *PeerStore) UpdatePeerStatus(accountID, peerID string, lastHandshake time.Time, endpoint string, isOnline bool) error {
	if err := s.repo.UpdatePeerStatus(accountID, peerID, lastHandshake, endpoint, isOnline); err != nil {
		return err
	}
	// Invalidate cache for this peer so next fetch gets updated status
	if s.redis != nil {
		ctx := context.Background()
		key := fmt.Sprintf("peer:%s", peerID)
		s.redis.Del(ctx, key)
	}
	return nil
}

// ClearWinboxSessions removes all WinboxSessions for a peer.
func (s *PeerStore) ClearWinboxSessions(accountID, peerID string) error {
	return s.repo.ClearWinboxSessions(accountID, peerID)
}

// ListAllWinboxSessions loads all Winbox sessions for the account directly from DB.
func (s *PeerStore) ListAllWinboxSessions(accountID string) ([]WinboxSession, error) {
	list, err := s.repo.ListAllWinboxSessions(accountID)
	if err != nil {
		return nil, err
	}
	result := make([]WinboxSession, len(list))
	for i, d := range list {
		result[i] = toDomainWinboxSession(d)
	}
	return result, nil
}

// GetWinboxSession gets a single session from DB.
func (s *PeerStore) GetWinboxSession(sessionID string) (*WinboxSession, error) {
	d, err := s.repo.GetWinboxSession(sessionID)
	if err != nil {
		return nil, err
	}
	res := toDomainWinboxSession(d)
	return &res, nil
}

// ListWinboxSessions gets all sessions for a specific peer from DB.
func (s *PeerStore) ListWinboxSessions(accountID, peerID string) ([]WinboxSession, error) {
	list, err := s.repo.ListWinboxSessions(accountID, peerID)
	if err != nil {
		return nil, err
	}
	result := make([]WinboxSession, len(list))
	for i, d := range list {
		result[i] = toDomainWinboxSession(d)
	}
	return result, nil
}

// SaveWinboxSession saves a single Winbox session to the database.
func (s *PeerStore) SaveWinboxSession(accountID, peerID string, session *WinboxSession) error {
	data := toStoreWinboxSession(*session, accountID, peerID)
	if err := s.repo.SaveWinboxSession(accountID, peerID, data); err != nil {
		return err
	}
	// Invalidate peer cache to ensure subsequent fetches see the new session
	if s.redis != nil {
		ctx := context.Background()
		key := fmt.Sprintf("peer:%s", peerID)
		s.redis.Del(ctx, key)
	}
	return nil
}

// DeleteWinboxSession deletes a Winbox session from the database.
func (s *PeerStore) DeleteWinboxSession(accountID, peerID, sessionID string) error {
	if err := s.repo.DeleteWinboxSession(accountID, peerID, sessionID); err != nil {
		return err
	}
	// Invalidate peer cache
	if s.redis != nil {
		ctx := context.Background()
		key := fmt.Sprintf("peer:%s", peerID)
		s.redis.Del(ctx, key)
	}
	return nil
}

// LogSSHActivity logs an SSH session activity.
func (s *PeerStore) LogSSHActivity(accountID, peerID string, activity SSHActivity) error {
	// Convert commands
	var storeCmds []store.SSHCommandData
	for _, c := range activity.Commands {
		storeCmds = append(storeCmds, store.SSHCommandData{
			Command:   c.Command,
			Timestamp: c.Timestamp,
		})
	}

	sa := &store.SSHActivityData{
		SessionID:  activity.SessionID,
		UserAgent:  activity.UserAgent,
		ClientIP:   activity.ClientIP,
		Timestamp:  activity.Timestamp,
		EndTime:    activity.EndTime,
		Username:   activity.Username,
		Commands:   storeCmds,
		BytesSent:  activity.BytesSent,
		BytesRecv:  activity.BytesRecv,
		DurationMs: activity.DurationMs,
	}
	return s.repo.LogSSHActivity(accountID, peerID, sa)
}

func (s *PeerStore) UpdateSSHActivityForPeer(accountID, peerID, sessionID string, updateFn func(*SSHActivity)) error {
	return s.repo.UpdateSSHActivity(accountID, peerID, sessionID, func(d *store.SSHActivityData) {
		// Map store data to server data
		localCmds := make([]SSHSessionCommand, len(d.Commands))
		for i, c := range d.Commands {
			localCmds[i] = SSHSessionCommand{Command: c.Command, Timestamp: c.Timestamp}
		}

		local := &SSHActivity{
			SessionID:  d.SessionID,
			UserAgent:  d.UserAgent,
			ClientIP:   d.ClientIP,
			Timestamp:  d.Timestamp,
			EndTime:    d.EndTime,
			Username:   d.Username,
			Commands:   localCmds,
			BytesSent:  d.BytesSent,
			BytesRecv:  d.BytesRecv,
			DurationMs: d.DurationMs,
		}

		updateFn(local)

		// Map server data back to store data for update
		d.EndTime = local.EndTime
		d.BytesSent = local.BytesSent
		d.BytesRecv = local.BytesRecv
		d.DurationMs = local.DurationMs
	})
}

// LogWinboxActivity logs a Winbox login activity.
func (s *PeerStore) LogWinboxActivity(accountID, peerID string, activity WinboxActivity) error {
	wa := &store.WinboxActivityData{
		SessionName: activity.SessionName,
		Username:    activity.Username,
		ClientIP:    activity.ClientIP,
		Timestamp:   activity.Timestamp,
		EndTime:     activity.EndTime,
		DurationMs:  activity.DurationMs,
		RomonMode:   activity.RomonMode,
	}
	return s.repo.LogWinboxActivity(accountID, peerID, wa)
}

func (s *PeerStore) UpdateWinboxActivityForPeer(accountID, peerID, sessionName string, timestamp time.Time, update func(*WinboxActivity)) error {
	return s.repo.UpdateWinboxActivity(accountID, peerID, sessionName, timestamp, func(d *store.WinboxActivityData) {
		local := &WinboxActivity{
			SessionName: d.SessionName,
			Username:    d.Username,
			ClientIP:    d.ClientIP,
			Timestamp:   d.Timestamp,
			EndTime:     d.EndTime,
			DurationMs:  d.DurationMs,
			RomonMode:   d.RomonMode,
		}
		update(local)
		d.EndTime = local.EndTime
		d.DurationMs = local.DurationMs
	})
}

// ListSSHActivities lists recent SSH activities for a peer.
func (s *PeerStore) ListSSHActivities(accountID, peerID string, limit int) []SSHActivity {
	data, err := s.repo.ListSSHActivities(accountID, peerID, limit)
	if err != nil {
		log.Warn().Err(err).Str("peer_id", peerID).Msg("Failed to list SSH activities")
		return nil
	}

	results := make([]SSHActivity, len(data))
	for i, d := range data {
		commands := make([]SSHSessionCommand, 0, len(d.Commands))
		for _, cmd := range d.Commands {
			commands = append(commands, SSHSessionCommand{
				Command:   cmd.Command,
				Timestamp: cmd.Timestamp,
			})
		}
		results[i] = SSHActivity{
			SessionID:  d.SessionID,
			UserAgent:  d.UserAgent,
			ClientIP:   d.ClientIP,
			Timestamp:  d.Timestamp,
			EndTime:    d.EndTime,
			Username:   d.Username,
			Commands:   commands,
			BytesSent:  d.BytesSent,
			BytesRecv:  d.BytesRecv,
			DurationMs: d.DurationMs,
		}
	}
	return results
}

// ListWinboxActivities lists recent Winbox activities for a peer.
func (s *PeerStore) ListWinboxActivities(accountID, peerID string, limit int) []WinboxActivity {
	data, err := s.repo.ListWinboxActivities(accountID, peerID, limit)
	if err != nil {
		log.Warn().Err(err).Str("peer_id", peerID).Msg("Failed to list Winbox activities")
		return nil
	}

	results := make([]WinboxActivity, len(data))
	for i, d := range data {
		results[i] = WinboxActivity{
			SessionName: d.SessionName,
			Username:    d.Username,
			ClientIP:    d.ClientIP,
			Timestamp:   d.Timestamp,
			EndTime:     d.EndTime,
			DurationMs:  d.DurationMs,
			RomonMode:   d.RomonMode,
		}
	}
	return results
}

// UpdatePeerScanResults updates the port scan results for a peer.
func (s *PeerStore) UpdatePeerScanResults(accountID, peerID string, lastScan time.Time, scanJSON []byte, openPorts []store.OpenPortData, fingerprint *store.OSFingerprintData) error {
	// Update DB
	if err := s.repo.UpdatePeerScanResults(accountID, peerID, lastScan, scanJSON, openPorts, fingerprint); err != nil {
		return err
	}

	// Invalidate or update cache
	if s.redis != nil {
		ctx := context.Background()
		key := fmt.Sprintf("peer:%s", peerID)

		// For now, simpler to just invalidate to force reload next time
		// Or we could fetch-modify-set, but invalidation is safer for consistency
		s.redis.Del(ctx, key)
	}
	return nil
}

// UpdatePeerNotes updates the markdown notes for a peer.
func (s *PeerStore) UpdatePeerNotes(accountID, peerID, notes string) error {
	if err := s.repo.UpdatePeerNotes(accountID, peerID, notes); err != nil {
		return fmt.Errorf("repository update peer notes error: %w", err)
	}

	// Invalidate or update cache
	if s.redis != nil {
		ctx := context.Background()
		key := fmt.Sprintf("peer:%s", peerID)
		s.redis.Del(ctx, key)
	}
	return nil
}
