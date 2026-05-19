package postgres

import (
	"time"
)

func (r *peerRepository) UpdatePeerStatus(accountID, peerID string, lastHandshake time.Time, endpoint string, isOnline bool) error {
	now := time.Now().UTC()
	peer := &Peer{
		ID:                peerID,
		AccountID:         accountID,
		LastHandshakeTime: lastHandshake,
		Endpoint:          endpoint,
		IsOnline:          isOnline,
		LastSeenAt:        now,
		LastOnlineAt:      now,
	}

	q := r.db.Model(peer).
		Column("last_handshake_time", "endpoint", "is_online", "last_seen_at").
		Where("id = ?", peerID).
		Where("account_id = ?", accountID)

	if isOnline {
		q = q.Column("last_online_at")
	}

	_, err := q.Update()
	return err
}
