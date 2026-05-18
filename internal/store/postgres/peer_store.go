package postgres

import (
	"WantasticCore/internal/models"
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/go-pg/pg/v10"
)

// PeerStore is the PostgreSQL implementation of the peer store.
type PeerStore struct {
	db *pg.DB
}

// NewPeerStore creates a new PostgreSQL-backed peer store.
func NewPeerStore(db *pg.DB) *PeerStore {
	return &PeerStore{db: db}
}

// SavePeer stores peer metadata.
func (s *PeerStore) SavePeer(peer *models.PeerMetadata) error {
	pgPeer := fromModelPeerMetadata(peer)
	_, err := s.db.Model(pgPeer).OnConflict("(id) DO UPDATE").Insert()
	return err
}

// GetPeer retrieves peer metadata by ID.
func (s *PeerStore) GetPeer(accountID, peerID string) (*models.PeerMetadata, error) {
	pgPeer := new(Peer)
	err := s.db.Model(pgPeer).
		Where("id = ? AND account_id = ?", peerID, accountID).
		Relation("WinboxSessions").
		Relation("WebSSHSessions").
		Relation("SSHActivities").
		Relation("WinboxActivities").
		Select()
	if err != nil {
		return nil, err
	}
	return toModelPeerMetadata(pgPeer), nil
}

// ListPeers lists all peers for an account.
func (s *PeerStore) ListPeers(accountID string) ([]*models.PeerMetadata, error) {
	var pgPeers []*Peer
	err := s.db.Model(&pgPeers).
		Where("account_id = ?", accountID).
		Relation("SSHActivities", func(q *pg.Query) (*pg.Query, error) {
			return q.Order("timestamp DESC").Limit(50), nil
		}).
		Relation("WinboxActivities", func(q *pg.Query) (*pg.Query, error) {
			return q.Order("timestamp DESC").Limit(50), nil
		}).
		Select()
	if err != nil {
		return nil, err
	}

	var modelPeers []*models.PeerMetadata
	for _, p := range pgPeers {
		modelPeers = append(modelPeers, toModelPeerMetadata(p))
	}
	return modelPeers, nil
}

// DeletePeer removes peer metadata.
func (s *PeerStore) DeletePeer(accountID, peerID string) error {
	_, err := s.db.Model(&Peer{}).Where("id = ? AND account_id = ?", peerID, accountID).Delete()
	return err
}

// DeleteAccountPeers removes all peers for a given account.
func (s *PeerStore) DeleteAccountPeers(accountID string) error {
	_, err := s.db.Model(&Peer{}).Where("account_id = ?", accountID).Delete()
	return err
}

// ClearWinboxSessions removes all winbox sessions for a given peer.
func (s *PeerStore) ClearWinboxSessions(accountID, peerID string) error {
	// Ensure the peer belongs to the account before deleting sessions.
	exists, err := s.db.Model((*Peer)(nil)).Where("id = ? AND account_id = ?", peerID, accountID).Exists()
	if err != nil {
		return err
	}
	if !exists {
		return fmt.Errorf("peer not found")
	}
	_, err = s.db.Model(&WinboxSession{}).Where("peer_id = ?", peerID).Delete()
	return err
}

// GetWinboxSessionByAccessToken performs O(1) lookup of a Winbox session by access token.
func (s *PeerStore) GetWinboxSessionByAccessToken(accessToken string) (*models.WinboxSessionLookupResult, error) {
	var res struct {
		WinboxSession
		AccountID string `pg:"account_id"`
	}

	// Join with Peers table to get AccountID and PeerID
	err := s.db.Model(&res.WinboxSession).
		Column("ws.*").
		ColumnExpr("p.account_id").
		TableExpr("winbox_sessions AS ws").
		Join("JOIN peers p ON p.id = ws.peer_id").
		Where("ws.access_token = ?", accessToken).
		Select()

	if err != nil {
		return nil, fmt.Errorf("access token not found: %w", err)
	}

	// Fetch full peer metadata
	peer, err := s.GetPeer(res.AccountID, res.PeerID)
	if err != nil {
		return nil, fmt.Errorf("failed to get peer for session: %w", err)
	}

	// Convert session
	session := &models.WinboxSession{
		ID:                res.ID,
		Name:              res.Name,
		RouterIP:          res.RouterIP,
		AccessToken:       res.AccessToken,
		PasswordToken:     res.PasswordToken,
		EncryptedUsername: res.EncryptedUsername,
		EncryptedPassword: res.EncryptedPassword,
		AuthMethod:        res.AuthMethod,
		AllowedClientIPs:  res.AllowedClientIPs,
		CredentialsValid:  res.CredentialsValid,
		LastValidated:     res.LastValidated,
		ValidationError:   res.ValidationError,
		LastConnected:     res.LastConnected,
		CreatedAt:         res.CreatedAt,
		UpdatedAt:         res.UpdatedAt,
		Enabled:           res.Enabled,
	}

	return &models.WinboxSessionLookupResult{
		AccountID: res.AccountID,
		PeerID:    res.PeerID,
		Session:   session,
		Peer:      peer,
	}, nil
}

// LogSSHActivity logs an SSH session activity for a peer.
func (s *PeerStore) LogSSHActivity(accountID, peerID string, activity models.SSHActivity) error {
	// Verify peer exists and belongs to account
	exists, err := s.db.Model((*Peer)(nil)).Where("id = ? AND account_id = ?", peerID, accountID).Exists()
	if err != nil {
		return err
	}
	if !exists {
		return fmt.Errorf("peer not found")
	}

	cmdsJSON, _ := json.Marshal(activity.Commands)
	pgActivity := &SSHActivity{
		PeerID:     peerID,
		SessionID:  activity.SessionID,
		UserAgent:  activity.UserAgent,
		ClientIP:   activity.ClientIP,
		Timestamp:  activity.Timestamp,
		EndTime:    activity.EndTime,
		Username:   activity.Username,
		Commands:   json.RawMessage(cmdsJSON),
		BytesSent:  activity.BytesSent,
		BytesRecv:  activity.BytesRecv,
		DurationMs: activity.DurationMs,
	}

	_, err = s.db.Model(pgActivity).Insert()
	return err
}

// UpdateSSHActivityForPeer updates an existing SSH activity.
func (s *PeerStore) UpdateSSHActivityForPeer(accountID, peerID, sessionID string, updateFn func(*models.SSHActivity)) error {
	return s.db.RunInTransaction(context.Background(), func(tx *pg.Tx) error {
		// Verify peer ownership
		exists, err := tx.Model((*Peer)(nil)).Where("id = ? AND account_id = ?", peerID, accountID).Exists()
		if err != nil {
			return err
		}
		if !exists {
			return fmt.Errorf("peer not found")
		}

		// Get activity
		pgActivity := new(SSHActivity)
		err = tx.Model(pgActivity).
			Where("peer_id = ? AND session_id = ?", peerID, sessionID).
			Select()
		if err != nil {
			return fmt.Errorf("SSH activity not found: %w", err)
		}

		// Convert commands from any to []SSHSessionCommand
		var commands []models.SSHSessionCommand
		if pgActivity.Commands != nil {
			data, err := json.Marshal(pgActivity.Commands)
			if err == nil {
				_ = json.Unmarshal(data, &commands)
			}
		}

		// Convert to model struct
		srvActivity := models.SSHActivity{
			SessionID:  pgActivity.SessionID,
			UserAgent:  pgActivity.UserAgent,
			ClientIP:   pgActivity.ClientIP,
			Timestamp:  pgActivity.Timestamp,
			EndTime:    pgActivity.EndTime,
			Username:   pgActivity.Username,
			Commands:   commands,
			BytesSent:  pgActivity.BytesSent,
			BytesRecv:  pgActivity.BytesRecv,
			DurationMs: pgActivity.DurationMs,
		}

		// Apply update
		updateFn(&srvActivity)

		// Update fields
		pgActivity.EndTime = srvActivity.EndTime
		pgActivity.BytesSent = srvActivity.BytesSent
		pgActivity.BytesRecv = srvActivity.BytesRecv
		pgActivity.DurationMs = srvActivity.DurationMs
		cmdsJSON, _ := json.Marshal(srvActivity.Commands)
		pgActivity.Commands = json.RawMessage(cmdsJSON)

		_, err = tx.Model(pgActivity).WherePK().Update()
		return err
	})
}

// LogWinboxActivity logs a Winbox login activity for a peer.
func (s *PeerStore) LogWinboxActivity(accountID, peerID string, activity models.WinboxActivity) error {
	// Verify peer exists
	exists, err := s.db.Model((*Peer)(nil)).Where("id = ? AND account_id = ?", peerID, accountID).Exists()
	if err != nil {
		return err
	}
	if !exists {
		return fmt.Errorf("peer not found")
	}

	pgActivity := &WinboxActivity{
		PeerID:      peerID,
		SessionName: activity.SessionName,
		Username:    activity.Username,
		ClientIP:    activity.ClientIP,
		Timestamp:   activity.Timestamp,
		EndTime:     activity.EndTime,
		DurationMs:  activity.DurationMs,
		RomonMode:   activity.RomonMode,
	}

	_, err = s.db.Model(pgActivity).Insert()
	return err
}

// UpdateWinboxActivityForPeer updates an existing Winbox activity.
func (s *PeerStore) UpdateWinboxActivityForPeer(accountID, peerID, sessionName string, timestamp time.Time, updateFn func(*models.WinboxActivity)) error {
	return s.db.RunInTransaction(context.Background(), func(tx *pg.Tx) error {
		// Verify peer
		exists, err := tx.Model((*Peer)(nil)).Where("id = ? AND account_id = ?", peerID, accountID).Exists()
		if err != nil {
			return err
		}
		if !exists {
			return fmt.Errorf("peer not found")
		}

		// Find activity with tolerance for timestamp
		pgActivity := new(WinboxActivity)

		// Use a 1-second window for timestamp matching
		minTime := timestamp.Add(-1 * time.Second)
		maxTime := timestamp.Add(1 * time.Second)

		err = tx.Model(pgActivity).
			Where("peer_id = ? AND session_name = ?", peerID, sessionName).
			Where("timestamp >= ? AND timestamp <= ?", minTime, maxTime).
			Select()

		if err != nil {
			return fmt.Errorf("Winbox activity not found: %w", err)
		}

		// Convert to model struct
		srvActivity := models.WinboxActivity{
			SessionName: pgActivity.SessionName,
			Username:    pgActivity.Username,
			ClientIP:    pgActivity.ClientIP,
			Timestamp:   pgActivity.Timestamp,
			EndTime:     pgActivity.EndTime,
			DurationMs:  pgActivity.DurationMs,
			RomonMode:   pgActivity.RomonMode,
		}

		// Apply update
		updateFn(&srvActivity)

		// Update fields
		pgActivity.EndTime = srvActivity.EndTime
		pgActivity.DurationMs = srvActivity.DurationMs

		_, err = tx.Model(pgActivity).WherePK().Update()
		return err
	})
}

func toModelPeerMetadata(p *Peer) *models.PeerMetadata {
	if p == nil {
		return nil
	}

	// Convert WinboxSessions
	winboxSessions := make([]models.WinboxSession, len(p.WinboxSessions))
	for i, ws := range p.WinboxSessions {
		winboxSessions[i] = models.WinboxSession{
			ID:                ws.ID,
			Name:              ws.Name,
			RouterIP:          ws.RouterIP,
			AccessToken:       ws.AccessToken,
			PasswordToken:     ws.PasswordToken,
			EncryptedUsername: ws.EncryptedUsername,
			EncryptedPassword: ws.EncryptedPassword,
			AuthMethod:        ws.AuthMethod,
			AllowedClientIPs:  ws.AllowedClientIPs,
			CredentialsValid:  ws.CredentialsValid,
			LastValidated:     ws.LastValidated,
			ValidationError:   ws.ValidationError,
			LastConnected:     ws.LastConnected,
			CreatedAt:         ws.CreatedAt,
			UpdatedAt:         ws.UpdatedAt,
			Enabled:           ws.Enabled,
		}
	}

	// Convert WebSSHSessions
	webSSHSessions := make([]models.WebSSHSession, len(p.WebSSHSessions))
	for i, wss := range p.WebSSHSessions {
		webSSHSessions[i] = models.WebSSHSession{
			ID:                wss.ID,
			Name:              wss.Name,
			Port:              wss.Port,
			EncryptedUsername: wss.EncryptedUsername,
			EncryptedPassword: wss.EncryptedPassword,
			TerminalRows:      wss.TerminalRows,
			TerminalCols:      wss.TerminalCols,
			LastConnected:     wss.LastConnected,
			CreatedAt:         wss.CreatedAt,
			UpdatedAt:         wss.UpdatedAt,
			Enabled:           wss.Enabled,
		}
	}

	// Convert SSHActivities
	sshActivities := make([]models.SSHActivity, len(p.SSHActivities))
	for i, sa := range p.SSHActivities {
		// Convert Commands from any
		var commands []models.SSHSessionCommand
		if sa.Commands != nil {
			if data, err := json.Marshal(sa.Commands); err == nil {
				_ = json.Unmarshal(data, &commands)
			}
		}

		sshActivities[i] = models.SSHActivity{
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

	// Convert WinboxActivities
	winboxActivities := make([]models.WinboxActivity, len(p.WinboxActivities))
	for i, wa := range p.WinboxActivities {
		winboxActivities[i] = models.WinboxActivity{
			SessionName: wa.SessionName,
			Username:    wa.Username,
			ClientIP:    wa.ClientIP,
			Timestamp:   wa.Timestamp,
			EndTime:     wa.EndTime,
			DurationMs:  wa.DurationMs,
			RomonMode:   wa.RomonMode,
		}
	}

	// Convert CachedPortScanJSON
	var cachedScanJSON []byte
	if p.CachedPortScanJSON != nil {
		cachedScanJSON, _ = json.Marshal(p.CachedPortScanJSON)
	}

	return &models.PeerMetadata{
		ID:                       p.ID,
		AccountID:                p.AccountID,
		Name:                     p.Name,
		AssignedIP:               p.AssignedIP,
		AllowedIPs:               p.AllowedIPs,
		Tags:                     p.Tags,
		PrivateKey:               p.PrivateKey,
		CreatedAt:                p.CreatedAt,
		UpdatedAt:                p.UpdatedAt,
		IsOnline:                 p.IsOnline,
		LastHandshakeTime:        p.LastHandshakeTime,
		LastSeenAt:               p.LastSeenAt,
		RxBytes:                  p.RxBytes,
		TxBytes:                  p.TxBytes,
		WebSSHConsumerActive:     p.WebSSHConsumerActive,
		WebSSHConsumerPort:       p.WebSSHConsumerPort,
		WebSSHLinkActive:         p.WebSSHLinkActive,
		WebSSHLinkExpiry:         p.WebSSHLinkExpiry,
		HasWinbox:                p.HasWinbox,
		WinboxSessions:           winboxSessions,
		WebSSHSessions:           webSSHSessions,
		SSHActivities:            sshActivities,
		WinboxActivities:         winboxActivities,
		LastPortScan:             p.LastPortScan,
		CachedPortScanJSON:       cachedScanJSON,
		ScannedSSHPort:           p.ScannedSSHPort,
		ScannedWinboxPort:        p.ScannedWinboxPort,
		LastPortScanTime:         p.LastPortScanTime,
		NotificationEnabled:      p.NotificationEnabled,
		FirstSeenOnline:          p.FirstSeenOnline,
		LastOnlineAt:             p.LastOnlineAt,
		FailedHandshakes:         p.FailedHandshakes,
		LastNotificationSentAt:   p.LastNotificationSentAt,
		OfflineNotificationState: p.OfflineNotificationState,
	}
}

func fromModelPeerMetadata(sp *models.PeerMetadata) *Peer {
	if sp == nil {
		return nil
	}

	// Convert WinboxSessions
	winboxSessions := make([]*WinboxSession, len(sp.WinboxSessions))
	for i, ws := range sp.WinboxSessions {
		winboxSessions[i] = &WinboxSession{
			ID:                ws.ID,
			PeerID:            sp.ID,
			Name:              ws.Name,
			RouterIP:          ws.RouterIP,
			AccessToken:       ws.AccessToken,
			PasswordToken:     ws.PasswordToken,
			EncryptedUsername: ws.EncryptedUsername,
			EncryptedPassword: ws.EncryptedPassword,
			AuthMethod:        ws.AuthMethod,
			AllowedClientIPs:  ws.AllowedClientIPs,
			CredentialsValid:  ws.CredentialsValid,
			LastValidated:     ws.LastValidated,
			ValidationError:   ws.ValidationError,
			LastConnected:     ws.LastConnected,
			CreatedAt:         ws.CreatedAt,
			UpdatedAt:         ws.UpdatedAt,
			Enabled:           ws.Enabled,
		}
	}

	// Convert WebSSHSessions
	webSSHSessions := make([]*WebSSHSession, len(sp.WebSSHSessions))
	for i, wss := range sp.WebSSHSessions {
		webSSHSessions[i] = &WebSSHSession{
			ID:                wss.ID,
			PeerID:            sp.ID,
			Name:              wss.Name,
			Port:              wss.Port,
			EncryptedUsername: wss.EncryptedUsername,
			EncryptedPassword: wss.EncryptedPassword,
			TerminalRows:      wss.TerminalRows,
			TerminalCols:      wss.TerminalCols,
			LastConnected:     wss.LastConnected,
			CreatedAt:         wss.CreatedAt,
			UpdatedAt:         wss.UpdatedAt,
			Enabled:           wss.Enabled,
		}
	}

	// Convert SSHActivities
	sshActivities := make([]*SSHActivity, len(sp.SSHActivities))
	for i, sa := range sp.SSHActivities {
		cmdsJSON, _ := json.Marshal(sa.Commands)
		sshActivities[i] = &SSHActivity{
			PeerID:     sp.ID,
			SessionID:  sa.SessionID,
			UserAgent:  sa.UserAgent,
			ClientIP:   sa.ClientIP,
			Timestamp:  sa.Timestamp,
			EndTime:    sa.EndTime,
			Username:   sa.Username,
			Commands:   json.RawMessage(cmdsJSON),
			BytesSent:  sa.BytesSent,
			BytesRecv:  sa.BytesRecv,
			DurationMs: sa.DurationMs,
		}
	}

	// Convert WinboxActivities
	winboxActivities := make([]*WinboxActivity, len(sp.WinboxActivities))
	for i, wa := range sp.WinboxActivities {
		winboxActivities[i] = &WinboxActivity{
			PeerID:      sp.ID,
			SessionName: wa.SessionName,
			Username:    wa.Username,
			ClientIP:    wa.ClientIP,
			Timestamp:   wa.Timestamp,
			EndTime:     wa.EndTime,
			DurationMs:  wa.DurationMs,
			RomonMode:   wa.RomonMode,
		}
	}

	// Convert CachedPortScanJSON
	var cachedScanJSON json.RawMessage
	if len(sp.CachedPortScanJSON) > 0 {
		cachedScanJSON = json.RawMessage(sp.CachedPortScanJSON)
	}

	return &Peer{
		ID:                       sp.ID,
		AccountID:                sp.AccountID,
		Name:                     sp.Name,
		AssignedIP:               sp.AssignedIP,
		AllowedIPs:               sp.AllowedIPs,
		Tags:                     sp.Tags,
		PrivateKey:               sp.PrivateKey,
		CreatedAt:                sp.CreatedAt,
		UpdatedAt:                sp.UpdatedAt,
		IsOnline:                 sp.IsOnline,
		LastHandshakeTime:        sp.LastHandshakeTime,
		LastSeenAt:               sp.LastSeenAt,
		RxBytes:                  sp.RxBytes,
		TxBytes:                  sp.TxBytes,
		WebSSHConsumerActive:     sp.WebSSHConsumerActive,
		WebSSHConsumerPort:       sp.WebSSHConsumerPort,
		WebSSHLinkActive:         sp.WebSSHLinkActive,
		WebSSHLinkExpiry:         sp.WebSSHLinkExpiry,
		HasWinbox:                sp.HasWinbox,
		WinboxSessions:           winboxSessions,
		WebSSHSessions:           webSSHSessions,
		SSHActivities:            sshActivities,
		WinboxActivities:         winboxActivities,
		LastPortScan:             sp.LastPortScan,
		CachedPortScanJSON:       cachedScanJSON,
		ScannedSSHPort:           sp.ScannedSSHPort,
		ScannedWinboxPort:        sp.ScannedWinboxPort,
		LastPortScanTime:         sp.LastPortScanTime,
		NotificationEnabled:      sp.NotificationEnabled,
		FirstSeenOnline:          sp.FirstSeenOnline,
		LastOnlineAt:             sp.LastOnlineAt,
		FailedHandshakes:         sp.FailedHandshakes,
		LastNotificationSentAt:   sp.LastNotificationSentAt,
		OfflineNotificationState: sp.OfflineNotificationState,
	}
}
