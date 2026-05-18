package postgres

import (
	"encoding/json"
	"fmt"
	"time"

	"WantasticCore/internal/store"

	"github.com/go-pg/pg/v10"
)

type peerRepository struct {
	db *pg.DB
}

func NewPeerRepository(db *pg.DB) store.PeerRepository {
	return &peerRepository{db: db}
}

// Peer Metadata

func (r *peerRepository) Save(peer *store.PeerData) error {
	// Handle raw JSON - directly pass through as json.RawMessage
	var cachedScan json.RawMessage
	if len(peer.CachedPortScanJSON) > 0 {
		cachedScan = json.RawMessage(peer.CachedPortScanJSON)
	}

	model := &Peer{
		ID:                        peer.ID,
		AccountID:                 peer.AccountID,
		Name:                      peer.Name,
		AssignedIP:                peer.AssignedIP,
		AllowedIPs:                peer.AllowedIPs,
		PrivateKey:                peer.PrivateKey,
		CreatedAt:                 peer.CreatedAt,
		UpdatedAt:                 peer.UpdatedAt,
		IsOnline:                  peer.IsOnline,
		LastHandshakeTime:         peer.LastHandshakeTime,
		LastSeenAt:                peer.LastSeenAt,
		Endpoint:                  peer.Endpoint,
		UptimeHistory:             peer.UptimeHistory,
		RxBytes:                   peer.RxBytes,
		TxBytes:                   peer.TxBytes,
		WebSSHConsumerActive:      peer.WebSSHConsumerActive,
		WebSSHConsumerPort:        peer.WebSSHConsumerPort,
		WebSSHLinkActive:          peer.WebSSHLinkActive,
		WebSSHLinkExpiry:          peer.WebSSHLinkExpiry,
		HasWinbox:                 peer.HasWinbox,
		EncryptedRouterOSUsername: peer.EncryptedRouterOSUsername,
		EncryptedRouterOSPassword: peer.EncryptedRouterOSPassword,
		RouterOSCredentialSource:  peer.RouterOSCredentialSource,
		RouterOSAPIVerified:       peer.RouterOSAPIVerified,
		RouterOSAPILastValidated:  peer.RouterOSAPILastValidated,
		RouterOSAPIError:          peer.RouterOSAPIError,
		RouterOSAPIPort:           peer.RouterOSAPIPort,
		RouterOSAPITLS:            peer.RouterOSAPITLS,
		LastPortScan:              peer.LastPortScan,
		CachedPortScanJSON:        cachedScan,
		ScannedSSHPort:            peer.ScannedSSHPort,
		ScannedWinboxPort:         peer.ScannedWinboxPort,
		LastPortScanTime:          peer.LastPortScanTime,
		NotificationEnabled:       peer.NotificationEnabled,
		FirstSeenOnline:           peer.FirstSeenOnline,
		LastOnlineAt:              peer.LastOnlineAt,
		FailedHandshakes:          peer.FailedHandshakes,
		LastNotificationSentAt:    peer.LastNotificationSentAt,
		OfflineNotificationState:  peer.OfflineNotificationState,
		Tags:                      peer.Tags,
		ClientType:                peer.ClientType,
		IsWantasticd:              peer.IsWantasticd,
	}

	if model.CreatedAt.IsZero() {
		model.CreatedAt = time.Now().UTC()
	}
	if model.UpdatedAt.IsZero() {
		model.UpdatedAt = time.Now().UTC()
	}

	// Upsert
	_, err := r.db.Model(model).
		OnConflict("(id) DO UPDATE").
		Set(`
			account_id                  = EXCLUDED.account_id,
			name                        = EXCLUDED.name,
			assigned_ip                 = EXCLUDED.assigned_ip,
			allowed_ips                 = EXCLUDED.allowed_ips,
			private_key                 = EXCLUDED.private_key,
			updated_at                  = EXCLUDED.updated_at,
			is_online                   = EXCLUDED.is_online,
			last_handshake_time         = EXCLUDED.last_handshake_time,
			last_seen_at                = EXCLUDED.last_seen_at,
			endpoint                    = EXCLUDED.endpoint,
			uptime_history              = EXCLUDED.uptime_history,
			rx_bytes                    = EXCLUDED.rx_bytes,
			tx_bytes                    = EXCLUDED.tx_bytes,
			webssh_consumer_active      = EXCLUDED.webssh_consumer_active,
			webssh_consumer_port        = EXCLUDED.webssh_consumer_port,
			webssh_link_active          = EXCLUDED.webssh_link_active,
			webssh_link_expiry          = EXCLUDED.webssh_link_expiry,
			has_winbox                  = EXCLUDED.has_winbox,
			encrypted_routeros_username = EXCLUDED.encrypted_routeros_username,
			encrypted_routeros_password = EXCLUDED.encrypted_routeros_password,
			routeros_credential_source  = EXCLUDED.routeros_credential_source,
			routeros_api_verified       = EXCLUDED.routeros_api_verified,
			routeros_api_last_validated = EXCLUDED.routeros_api_last_validated,
			routeros_api_error          = EXCLUDED.routeros_api_error,
			routeros_api_port           = EXCLUDED.routeros_api_port,
			routeros_api_tls            = EXCLUDED.routeros_api_tls,
			last_port_scan              = EXCLUDED.last_port_scan,
			cached_port_scan_json       = EXCLUDED.cached_port_scan_json,
			scanned_ssh_port            = EXCLUDED.scanned_ssh_port,
			scanned_winbox_port         = EXCLUDED.scanned_winbox_port,
			last_port_scan_time         = EXCLUDED.last_port_scan_time,
			notification_enabled        = EXCLUDED.notification_enabled,
			first_seen_online           = EXCLUDED.first_seen_online,
			last_online_at              = EXCLUDED.last_online_at,
			failed_handshakes           = EXCLUDED.failed_handshakes,
			last_notification_sent_at   = EXCLUDED.last_notification_sent_at,
			offline_notification_state  = EXCLUDED.offline_notification_state,
			tags                        = EXCLUDED.tags,
			notes                       = EXCLUDED.notes,
			client_type                 = EXCLUDED.client_type,
			is_wantasticd               = EXCLUDED.is_wantasticd,
			agent_model                 = EXCLUDED.agent_model,
			agent_version               = EXCLUDED.agent_version
		`).
		Insert()
	if err != nil {
		return fmt.Errorf("failed to save peer: %w", err)
	}
	return nil
}

func (r *peerRepository) UpdatePeerScanResults(accountID, peerID string, lastScan time.Time, scanJSON []byte, openPorts []store.OpenPortData, fingerprint *store.OSFingerprintData) error {
	// Find SSH and Winbox ports for convenience columns
	var sshPort, winboxPort int
	for _, p := range openPorts {
		if p.Port == 22 || p.Service == "ssh" {
			sshPort = p.Port
		}
		if p.Port == 8291 || p.Service == "winbox" {
			winboxPort = p.Port
		}
	}

	// Update the specific fields
	_, err := r.db.Model((*Peer)(nil)).
		Set("last_port_scan = ?", lastScan).
		Set("cached_port_scan_json = ?", json.RawMessage(scanJSON)).
		Set("scanned_ssh_port = ?", sshPort).
		Set("scanned_winbox_port = ?", winboxPort).
		Set("last_port_scan_time = ?", lastScan).
		Where("id = ?", peerID).
		Where("account_id = ?", accountID).
		Update()

	if err != nil {
		return fmt.Errorf("failed to update peer scan results: %w", err)
	}
	return nil
}

func (r *peerRepository) RecordHandshake(accountID, peerID string, timestamp time.Time, endpoint string) error {
	// Verify peer exists to avoid FK violation (and log spam)
	// This handles cases where a peer might be deleted but still reporting stats in memory,
	// or if there is a key format mismatch (though that should be fixed in device_extended.go).
	exists, err := r.db.Model((*Peer)(nil)).Where("id = ?", peerID).Exists()
	if err != nil {
		return fmt.Errorf("failed to check peer existence: %w", err)
	}
	if !exists {
		// return nil to skip silently, or error?
		// Skipping silently avoids log spam for "ghost" peers.
		return nil
	}

	// 1. Record in history table
	history := &PeerHandshake{
		PeerID:    peerID,
		AccountID: accountID,
		Timestamp: timestamp.UTC(),
		Endpoint:  endpoint,
	}
	if _, err := r.db.Model(history).Insert(); err != nil {
		return fmt.Errorf("failed to insert handshake history: %w", err)
	}

	// 2. Update uptime bitmask (optional, could be done periodically)
	// For now, we rely on the history table for the chart.
	return nil
}

func (r *peerRepository) GetHandshakeHistory(accountID, peerID string, since time.Time) ([]*store.PeerHandshakeData, error) {
	var models []PeerHandshake
	err := r.db.Model(&models).
		Where("peer_id = ?", peerID).
		Where("account_id = ?", accountID).
		Where("timestamp >= ?", since.UTC()).
		Order("timestamp ASC").
		Select()
	if err != nil {
		return nil, err
	}

	result := make([]*store.PeerHandshakeData, len(models))
	for i, m := range models {
		result[i] = &store.PeerHandshakeData{
			ID:        m.ID,
			PeerID:    m.PeerID,
			AccountID: m.AccountID,
			Timestamp: m.Timestamp,
			Endpoint:  m.Endpoint,
		}
	}
	return result, nil
}

func (r *peerRepository) Get(accountID, peerID string) (*store.PeerData, error) {
	model := &Peer{ID: peerID}
	err := r.db.Model(model).
		Where("account_id = ?", accountID). // Security check
		WherePK().Select()
	if err != nil {
		if err == pg.ErrNoRows {
			return nil, fmt.Errorf("peer not found: %s", peerID)
		}
		return nil, err
	}
	return r.toData(model), nil
}

func (r *peerRepository) FindByPeerID(peerID string) (*store.PeerData, error) {
	model := &Peer{ID: peerID}
	err := r.db.Model(model).WherePK().Select()
	if err != nil {
		if err == pg.ErrNoRows {
			return nil, fmt.Errorf("peer not found: %s", peerID)
		}
		return nil, err
	}
	return r.toData(model), nil
}

func (r *peerRepository) List(accountID string) ([]*store.PeerData, error) {
	var models []Peer
	err := r.db.Model(&models).
		Where("account_id = ?", accountID).
		Relation("SSHActivities", func(q *pg.Query) (*pg.Query, error) {
			return q.Order("timestamp DESC").Limit(50), nil
		}).
		Relation("WinboxActivities", func(q *pg.Query) (*pg.Query, error) {
			return q.Order("timestamp DESC").Limit(50), nil
		}).
		Order("name ASC").
		Select()
	if err != nil {
		return nil, err
	}
	result := make([]*store.PeerData, len(models))
	for i := range models {
		result[i] = r.toData(&models[i])
	}
	return result, nil
}

func (r *peerRepository) Count(accountID string) (int, error) {
	count, err := r.db.Model((*Peer)(nil)).
		Where("account_id = ?", accountID).
		Count()
	if err != nil {
		return 0, fmt.Errorf("failed to count peers: %w", err)
	}
	return count, nil
}

func (r *peerRepository) Delete(accountID, peerID string) error {
	result, err := r.db.Model((*Peer)(nil)).
		Where("id = ?", peerID).
		Where("account_id = ?", accountID).
		Delete()
	if err != nil {
		return err
	}
	if result.RowsAffected() == 0 {
		return fmt.Errorf("peer not found")
	}
	return nil
}

// UpdatePeerNotes updates the markdown notes for a specific peer
func (r *peerRepository) UpdatePeerNotes(accountID, peerID, notes string) error {
	_, err := r.db.Exec(`
		UPDATE peers
		SET notes = ?, updated_at = NOW()
		WHERE account_id = ? AND id = ?
	`, notes, accountID, peerID)

	if err != nil {
		return fmt.Errorf("failed to update peer notes: %w", err)
	}

	return nil
}

func (r *peerRepository) UpdatePeerAgentInfo(accountID, peerID, agentModel, agentVersion string) error {
	// When the agent identifies as wantasticd, also set client_type and is_wantasticd
	// so the portal UI can show WUSP-specific actions without waiting for stats.
	isWantasticd := agentModel == "wantasticd"
	clientType := "native"
	if isWantasticd {
		clientType = "wantasticd"
	}
	_, err := r.db.Exec(`
		UPDATE peers
		SET agent_model = ?, agent_version = ?, client_type = ?, is_wantasticd = ?, updated_at = NOW()
		WHERE account_id = ? AND id = ?
	`, agentModel, agentVersion, clientType, isWantasticd, accountID, peerID)

	if err != nil {
		return fmt.Errorf("failed to update peer agent info: %w", err)
	}

	return nil
}

func (r *peerRepository) CountPeers(accountID string) (int, error) {
	var count int
	_, err := r.db.QueryOne(pg.Scan(&count), "SELECT count(*) FROM peers WHERE account_id = ?", accountID)
	if err != nil {
		return 0, fmt.Errorf("failed to count peers: %w", err)
	}
	return count, nil
}

func (r *peerRepository) DeleteByAccount(accountID string) error {
	_, err := r.db.Model((*Peer)(nil)).
		Where("account_id = ?", accountID).
		Delete()
	return err
}

// Winbox Sessions

func (r *peerRepository) SaveWinboxSession(accountID, peerID string, session *store.WinboxSessionData) error {
	model := &WinboxSession{
		ID:                       session.ID,
		PeerID:                   peerID,
		AccountID:                accountID,
		Name:                     session.Name,
		RouterIP:                 session.RouterIP,
		Port:                     session.Port,
		AccessToken:              session.AccessToken,
		PasswordToken:            session.PasswordToken,
		EncryptedUsername:        session.EncryptedUsername,
		EncryptedPassword:        session.EncryptedPassword,
		AuthMethod:               session.AuthMethod,
		AllowedClientIPs:         session.AllowedClientIPs,
		CredentialsValid:         session.CredentialsValid,
		LastValidated:            session.LastValidated,
		ValidationError:          session.ValidationError,
		RouterOSAPIVerified:      session.RouterOSAPIVerified,
		RouterOSAPILastValidated: session.RouterOSAPILastValidated,
		RouterOSAPIError:         session.RouterOSAPIError,
		RouterOSAPIPort:          session.RouterOSAPIPort,
		RouterOSAPITLS:           session.RouterOSAPITLS,
		LastConnected:            session.LastConnected,
		CreatedAt:                session.CreatedAt,
		UpdatedAt:                session.UpdatedAt,
		Enabled:                  session.Enabled,
	}
	if model.CreatedAt.IsZero() {
		model.CreatedAt = time.Now()
	}
	if model.UpdatedAt.IsZero() {
		model.UpdatedAt = time.Now()
	}

	_, err := r.db.Model(model).
		OnConflict("(id) DO UPDATE").
		Set(`
			peer_id                      = EXCLUDED.peer_id,
			account_id                   = EXCLUDED.account_id,
			name                         = EXCLUDED.name,
			router_ip                    = EXCLUDED.router_ip,
			port                         = EXCLUDED.port,
			access_token                 = EXCLUDED.access_token,
			password_token               = EXCLUDED.password_token,
			encrypted_username           = EXCLUDED.encrypted_username,
			encrypted_password           = EXCLUDED.encrypted_password,
			auth_method                  = EXCLUDED.auth_method,
			allowed_client_ips           = EXCLUDED.allowed_client_ips,
			credentials_valid            = EXCLUDED.credentials_valid,
			last_validated               = EXCLUDED.last_validated,
			validation_error             = EXCLUDED.validation_error,
			routeros_api_verified        = EXCLUDED.routeros_api_verified,
			routeros_api_last_validated  = EXCLUDED.routeros_api_last_validated,
			routeros_api_error           = EXCLUDED.routeros_api_error,
			routeros_api_port            = EXCLUDED.routeros_api_port,
			routeros_api_tls             = EXCLUDED.routeros_api_tls,
			last_connected               = EXCLUDED.last_connected,
			updated_at                   = EXCLUDED.updated_at,
			enabled                      = EXCLUDED.enabled
		`).
		Insert()
	return err
}

func (r *peerRepository) GetWinboxSession(sessionID string) (*store.WinboxSessionData, error) {
	model := &WinboxSession{ID: sessionID}
	err := r.db.Model(model).WherePK().Select()
	if err != nil {
		if err == pg.ErrNoRows {
			return nil, fmt.Errorf("session not found")
		}
		return nil, err
	}
	return r.toWinboxData(model), nil
}

func (r *peerRepository) ListWinboxSessions(accountID, peerID string) ([]*store.WinboxSessionData, error) {
	var models []WinboxSession
	err := r.db.Model(&models).
		Where("account_id = ?", accountID).
		Where("peer_id = ?", peerID).
		Order("created_at DESC").
		Select()
	if err != nil {
		return nil, err
	}
	result := make([]*store.WinboxSessionData, len(models))
	for i := range models {
		result[i] = r.toWinboxData(&models[i])
	}
	return result, nil
}

func (r *peerRepository) ListAllWinboxSessions(accountID string) ([]*store.WinboxSessionData, error) {
	var models []WinboxSession
	err := r.db.Model(&models).
		Where("account_id = ?", accountID).
		Order("created_at DESC").
		Select()
	if err != nil {
		return nil, err
	}
	result := make([]*store.WinboxSessionData, len(models))
	for i := range models {
		result[i] = r.toWinboxData(&models[i])
	}
	return result, nil
}

func (r *peerRepository) DeleteWinboxSession(accountID, peerID, sessionID string) error {
	_, err := r.db.Model((*WinboxSession)(nil)).
		Where("id = ?", sessionID).
		Where("account_id = ?", accountID). // Security check
		Delete()
	return err
}

func (r *peerRepository) ClearWinboxSessions(accountID, peerID string) error {
	_, err := r.db.Model((*WinboxSession)(nil)).
		Where("peer_id = ?", peerID).
		Where("account_id = ?", accountID).
		Delete()
	return err
}

func (r *peerRepository) LookupWinboxByToken(accessToken string) (*store.WinboxLookupResult, error) {
	var sessions []WinboxSession
	err := r.db.Model(&sessions).Where("access_token = ?", accessToken).Order("created_at DESC").Select()
	if err != nil {
		return nil, err
	}
	if len(sessions) == 0 {
		return nil, fmt.Errorf("invalid token")
	}

	// Iterate through all found sessions to find one with a valid peer
	// This handles cases where "orphan" sessions exist (pointing to deleted peers)
	for _, session := range sessions {
		peer := &Peer{ID: session.PeerID}
		// Try to find the peer
		if err := r.db.Model(peer).WherePK().Select(); err == nil {
			// Found valid peer!
			return &store.WinboxLookupResult{
				AccountID: session.AccountID,
				PeerID:    session.PeerID,
				SessionID: session.ID,
				Session:   r.toWinboxData(&session),
				Peer:      r.toData(peer),
			}, nil
		}
		// If peer not found, matched session is an orphan; continue to next match
	}

	return nil, fmt.Errorf("peer not found for session")
}

// WebSSH Sessions

func (r *peerRepository) SaveWebSSHSession(accountID, peerID string, session *store.WebSSHSessionData) error {
	model := &WebSSHSession{
		ID:                            session.ID,
		PeerID:                        peerID,
		PeerIP:                        session.PeerIP,
		AccountID:                     accountID,
		Name:                          session.Name,
		Port:                          session.Port,
		EncryptedUsername:             session.EncryptedUsername,
		EncryptedPassword:             session.EncryptedPassword,
		EncryptedPrivateKey:           session.EncryptedPrivateKey,
		EncryptedPrivateKeyPassphrase: session.EncryptedPrivateKeyPassphrase,
		TerminalRows:                  session.TerminalRows,
		TerminalCols:                  session.TerminalCols,
		UserAgent:                     session.UserAgent,
		LastConnected:                 session.LastConnected,
		CreatedAt:                     session.CreatedAt,
		UpdatedAt:                     session.UpdatedAt,
		Enabled:                       session.Enabled,
		History:                       session.History,
		HostKey:                       session.HostKey,
		HostKeyFingerprint:            session.HostKeyFingerprint,
		HostKeyAlgorithm:              session.HostKeyAlgorithm,
		CompatibilityMode:             session.CompatibilityMode,
	}
	if model.CreatedAt.IsZero() {
		model.CreatedAt = time.Now()
	}
	_, err := r.db.Model(model).OnConflict("(id) DO UPDATE").Insert()
	return err
}

func (r *peerRepository) GetWebSSHSession(sessionID string) (*store.WebSSHSessionData, error) {
	model := &WebSSHSession{ID: sessionID}
	err := r.db.Model(model).WherePK().Select()
	if err != nil {
		if err == pg.ErrNoRows {
			return nil, fmt.Errorf("session not found")
		}
		return nil, err
	}
	return r.toWebSSHData(model), nil
}

func (r *peerRepository) ListWebSSHSessions(accountID, peerID string) ([]*store.WebSSHSessionData, error) {
	var models []WebSSHSession
	err := r.db.Model(&models).
		Where("account_id = ?", accountID).
		Where("peer_id = ?", peerID).
		Order("created_at DESC").
		Select()
	if err != nil {
		return nil, err
	}
	result := make([]*store.WebSSHSessionData, len(models))
	for i := range models {
		result[i] = r.toWebSSHData(&models[i])
	}
	return result, nil
}

func (r *peerRepository) ListAllWebSSHSessions(accountID string) ([]*store.WebSSHSessionData, error) {
	var models []WebSSHSession
	err := r.db.Model(&models).
		Where("account_id = ?", accountID).
		Order("created_at DESC").
		Select()
	if err != nil {
		return nil, err
	}
	result := make([]*store.WebSSHSessionData, len(models))
	for i := range models {
		result[i] = r.toWebSSHData(&models[i])
	}
	return result, nil
}

func (r *peerRepository) DeleteWebSSHSession(accountID, peerID, sessionID string) error {
	// Close any open activities for this session first
	// This ensures we don't have "Ongoing" ghost activities
	_, _ = r.db.Model((*SSHActivity)(nil)).
		Set("end_time = ?", time.Now()).
		Set("duration_ms = EXTRACT(EPOCH FROM (now() - timestamp)) * 1000").
		Where("session_id = ?", sessionID).
		Where("end_time = '0001-01-01 00:00:00+00' OR end_time IS NULL").
		Update()

	_, err := r.db.Model((*WebSSHSession)(nil)).
		Where("id = ?", sessionID).
		Where("account_id = ?", accountID).
		Delete()
	return err
}

// Activity Logging

func (r *peerRepository) LogSSHActivity(accountID, peerID string, activity *store.SSHActivityData) error {
	commandsJSON := activity.CommandsJSON
	if len(commandsJSON) == 0 && len(activity.Commands) > 0 {
		commandsJSON, _ = json.Marshal(activity.Commands)
	}

	model := &SSHActivity{
		ID:         activity.ID,
		PeerID:     peerID,
		AccountID:  accountID,
		SessionID:  activity.SessionID,
		UserAgent:  activity.UserAgent,
		ClientIP:   activity.ClientIP,
		Timestamp:  activity.Timestamp,
		EndTime:    activity.EndTime,
		Username:   activity.Username,
		Commands:   json.RawMessage(commandsJSON),
		BytesSent:  activity.BytesSent,
		BytesRecv:  activity.BytesRecv,
		DurationMs: activity.DurationMs,
	}

	updateResult, err := r.db.Model((*SSHActivity)(nil)).
		Set("peer_id = ?", model.PeerID).
		Set("user_agent = ?", model.UserAgent).
		Set("client_ip = ?", model.ClientIP).
		Set("timestamp = ?", model.Timestamp).
		Set("end_time = ?", model.EndTime).
		Set("username = ?", model.Username).
		Set("commands = ?", model.Commands).
		Set("bytes_sent = ?", model.BytesSent).
		Set("bytes_recv = ?", model.BytesRecv).
		Set("duration_ms = ?", model.DurationMs).
		Where("account_id = ?", accountID).
		Where("session_id = ?", activity.SessionID).
		Update()
	if err != nil {
		return err
	}
	if updateResult.RowsAffected() > 0 {
		return nil
	}

	_, err = r.db.Model(model).Insert()
	return err
}

func (r *peerRepository) UpdateSSHActivity(accountID, peerID, sessionID string, update func(*store.SSHActivityData)) error {
	var model SSHActivity
	err := r.db.Model(&model).
		Where("session_id = ?", sessionID).
		Where("account_id = ?", accountID).
		Order("timestamp DESC").
		Limit(1).
		Select()
	if err != nil {
		return err
	}

	data := r.toSSHActivityData(&model)
	update(data)

	// Update model
	model.EndTime = data.EndTime
	if len(data.CommandsJSON) > 0 {
		model.Commands = json.RawMessage(data.CommandsJSON)
	} else {
		cmdsJSON, _ := json.Marshal(data.Commands)
		model.Commands = json.RawMessage(cmdsJSON)
	}
	model.BytesSent = data.BytesSent
	model.BytesRecv = data.BytesRecv
	model.DurationMs = data.DurationMs

	_, err = r.db.Model(&model).WherePK().Update()
	return err
}

func (r *peerRepository) LogWinboxActivity(accountID, peerID string, activity *store.WinboxActivityData) error {
	model := &WinboxActivity{
		ID:          activity.ID,
		PeerID:      peerID,
		AccountID:   accountID,
		SessionName: activity.SessionName,
		Username:    activity.Username,
		ClientIP:    activity.ClientIP,
		Timestamp:   activity.Timestamp,
		EndTime:     activity.EndTime,
		DurationMs:  activity.DurationMs,
		RomonMode:   activity.RomonMode,
	}
	_, err := r.db.Model(model).Insert()
	return err
}

func (r *peerRepository) UpdateWinboxActivity(accountID, peerID, sessionName string, timestamp time.Time, update func(*store.WinboxActivityData)) error {
	// Warning: Identifying unique activity by name+timestamp is tricky. ID is better.
	// But signature uses name+timestamp.
	var model WinboxActivity
	err := r.db.Model(&model).
		Where("session_name = ?", sessionName).
		Where("account_id = ?", accountID).
		Where("timestamp = ?", timestamp).
		Limit(1).
		Select()
	if err != nil {
		return err
	}

	data := r.toWinboxActivityData(&model)
	update(data)

	model.EndTime = data.EndTime
	model.DurationMs = data.DurationMs

	_, err = r.db.Model(&model).WherePK().Update()
	return err
}

func (r *peerRepository) ListSSHActivities(accountID, peerID string, limit int) ([]*store.SSHActivityData, error) {
	var models []SSHActivity
	query := r.db.Model(&models).
		Where("account_id = ?", accountID).
		Where("peer_id = ?", peerID).
		Order("timestamp DESC")

	if limit > 0 {
		query = query.Limit(limit)
	}

	err := query.Select()
	if err != nil {
		return nil, fmt.Errorf("failed to list ssh activities: %w", err)
	}

	results := make([]*store.SSHActivityData, len(models))
	for i := range models {
		results[i] = r.toSSHActivityData(&models[i])
	}
	return results, nil
}

func (r *peerRepository) ListWinboxActivities(accountID, peerID string, limit int) ([]*store.WinboxActivityData, error) {
	var models []WinboxActivity
	query := r.db.Model(&models).
		Where("account_id = ?", accountID).
		Where("peer_id = ?", peerID).
		Order("timestamp DESC")

	if limit > 0 {
		query = query.Limit(limit)
	}

	err := query.Select()
	if err != nil {
		return nil, fmt.Errorf("failed to list winbox activities: %w", err)
	}

	results := make([]*store.WinboxActivityData, len(models))
	for i := range models {
		results[i] = r.toWinboxActivityData(&models[i])
	}
	return results, nil
}

// Helpers

func (r *peerRepository) toData(m *Peer) *store.PeerData {
	peerData := &store.PeerData{
		ID:                        m.ID,
		AccountID:                 m.AccountID,
		Name:                      m.Name,
		AssignedIP:                m.AssignedIP,
		AllowedIPs:                m.AllowedIPs,
		PrivateKey:                m.PrivateKey,
		CreatedAt:                 m.CreatedAt,
		UpdatedAt:                 m.UpdatedAt,
		IsOnline:                  m.IsOnline,
		LastHandshakeTime:         m.LastHandshakeTime,
		LastSeenAt:                m.LastSeenAt,
		RxBytes:                   m.RxBytes,
		TxBytes:                   m.TxBytes,
		WebSSHConsumerActive:      m.WebSSHConsumerActive,
		WebSSHConsumerPort:        m.WebSSHConsumerPort,
		WebSSHLinkActive:          m.WebSSHLinkActive,
		WebSSHLinkExpiry:          m.WebSSHLinkExpiry,
		HasWinbox:                 m.HasWinbox,
		EncryptedRouterOSUsername: m.EncryptedRouterOSUsername,
		EncryptedRouterOSPassword: m.EncryptedRouterOSPassword,
		RouterOSCredentialSource:  m.RouterOSCredentialSource,
		RouterOSAPIVerified:       m.RouterOSAPIVerified,
		RouterOSAPILastValidated:  m.RouterOSAPILastValidated,
		RouterOSAPIError:          m.RouterOSAPIError,
		RouterOSAPIPort:           m.RouterOSAPIPort,
		RouterOSAPITLS:            m.RouterOSAPITLS,
		LastPortScan:              m.LastPortScan,
		CachedPortScanJSON:        []byte(m.CachedPortScanJSON),
		ScannedSSHPort:            m.ScannedSSHPort,
		ScannedWinboxPort:         m.ScannedWinboxPort,
		LastPortScanTime:          m.LastPortScanTime,
		NotificationEnabled:       m.NotificationEnabled,
		FirstSeenOnline:           m.FirstSeenOnline,
		LastOnlineAt:              m.LastOnlineAt,
		FailedHandshakes:          m.FailedHandshakes,
		LastNotificationSentAt:    m.LastNotificationSentAt,
		OfflineNotificationState:  m.OfflineNotificationState,
		Tags:                      m.Tags,
		ClientType:                m.ClientType,
		IsWantasticd:              m.IsWantasticd,
	}

	// Convert Activities
	if len(m.SSHActivities) > 0 {
		peerData.SSHActivities = make([]*store.SSHActivityData, len(m.SSHActivities))
		for i, a := range m.SSHActivities {
			peerData.SSHActivities[i] = r.toSSHActivityData(a)
		}
	}

	if len(m.WinboxActivities) > 0 {
		peerData.WinboxActivities = make([]*store.WinboxActivityData, len(m.WinboxActivities))
		for i, a := range m.WinboxActivities {
			peerData.WinboxActivities[i] = r.toWinboxActivityData(a)
		}
	}

	return peerData
}

func (r *peerRepository) toWinboxData(m *WinboxSession) *store.WinboxSessionData {
	return &store.WinboxSessionData{
		ID:                       m.ID,
		PeerID:                   m.PeerID,
		AccountID:                m.AccountID,
		Name:                     m.Name,
		RouterIP:                 m.RouterIP,
		Port:                     m.Port,
		AccessToken:              m.AccessToken,
		PasswordToken:            m.PasswordToken,
		EncryptedUsername:        m.EncryptedUsername,
		EncryptedPassword:        m.EncryptedPassword,
		AuthMethod:               m.AuthMethod,
		AllowedClientIPs:         m.AllowedClientIPs,
		CredentialsValid:         m.CredentialsValid,
		LastValidated:            m.LastValidated,
		ValidationError:          m.ValidationError,
		RouterOSAPIVerified:      m.RouterOSAPIVerified,
		RouterOSAPILastValidated: m.RouterOSAPILastValidated,
		RouterOSAPIError:         m.RouterOSAPIError,
		RouterOSAPIPort:          m.RouterOSAPIPort,
		RouterOSAPITLS:           m.RouterOSAPITLS,
		LastConnected:            m.LastConnected,
		CreatedAt:                m.CreatedAt,
		UpdatedAt:                m.UpdatedAt,
		Enabled:                  m.Enabled,
	}
}

func (r *peerRepository) toWebSSHData(m *WebSSHSession) *store.WebSSHSessionData {
	return &store.WebSSHSessionData{
		ID:                            m.ID,
		PeerID:                        m.PeerID,
		PeerIP:                        m.PeerIP,
		AccountID:                     m.AccountID,
		Name:                          m.Name,
		Port:                          m.Port,
		EncryptedUsername:             m.EncryptedUsername,
		EncryptedPassword:             m.EncryptedPassword,
		EncryptedPrivateKey:           m.EncryptedPrivateKey,
		EncryptedPrivateKeyPassphrase: m.EncryptedPrivateKeyPassphrase,
		TerminalRows:                  m.TerminalRows,
		TerminalCols:                  m.TerminalCols,
		UserAgent:                     m.UserAgent,
		LastConnected:                 m.LastConnected,
		CreatedAt:                     m.CreatedAt,
		UpdatedAt:                     m.UpdatedAt,
		Enabled:                       m.Enabled,
		History:                       m.History,
		HostKey:                       m.HostKey,
		HostKeyFingerprint:            m.HostKeyFingerprint,
		HostKeyAlgorithm:              m.HostKeyAlgorithm,
		CompatibilityMode:             m.CompatibilityMode,
	}
}

func (r *peerRepository) toSSHActivityData(m *SSHActivity) *store.SSHActivityData {
	var commands []store.SSHCommandData
	if len(m.Commands) > 0 {
		_ = json.Unmarshal(m.Commands, &commands)
	}
	return &store.SSHActivityData{
		ID:           m.ID,
		PeerID:       m.PeerID,
		AccountID:    m.AccountID,
		SessionID:    m.SessionID,
		UserAgent:    m.UserAgent,
		ClientIP:     m.ClientIP,
		Timestamp:    m.Timestamp,
		EndTime:      m.EndTime,
		Username:     m.Username,
		Commands:     commands,
		CommandsJSON: []byte(m.Commands),
		BytesSent:    m.BytesSent,
		BytesRecv:    m.BytesRecv,
		DurationMs:   m.DurationMs,
	}
}

func (r *peerRepository) toWinboxActivityData(m *WinboxActivity) *store.WinboxActivityData {
	return &store.WinboxActivityData{
		ID:          m.ID,
		PeerID:      m.PeerID,
		AccountID:   m.AccountID,
		SessionName: m.SessionName,
		Username:    m.Username,
		ClientIP:    m.ClientIP,
		Timestamp:   m.Timestamp,
		EndTime:     m.EndTime,
		DurationMs:  m.DurationMs,
		RomonMode:   m.RomonMode,
	}
}
