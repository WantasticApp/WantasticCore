package postgres

import (
	"fmt"
	"time"

	"WantasticCore/internal/store"

	"github.com/go-pg/pg/v10"
)

// sessionRepository implements store.SessionRepository using PostgreSQL.
type sessionRepository struct {
	db *pg.DB
}

// NewSessionRepository creates a new PostgreSQL session repository.
func NewSessionRepository(db *pg.DB) store.SessionRepository {
	return &sessionRepository{db: db}
}

func (r *sessionRepository) Create(session *store.SessionData) error {
	model := &TenantSession{
		SessionID:     session.SessionID,
		TenantID:      session.TenantID,
		Email:         session.Email,
		FullName:      session.FullName,
		SessionToken:  session.SessionToken,
		CreatedAt:     session.CreatedAt,
		ExpiresAt:     session.ExpiresAt,
		LastActivity:  session.LastActivity,
		IPAddress:     session.IPAddress,
		UserAgent:     session.UserAgent,
		RememberMe:    session.RememberMe,
		DeviceHash:    session.DeviceHash,
		TrustedDevice: session.TrustedDevice,
	}

	if model.CreatedAt.IsZero() {
		model.CreatedAt = time.Now()
	}
	if model.LastActivity.IsZero() {
		model.LastActivity = time.Now()
	}

	_, err := r.db.Model(model).Insert()
	if err != nil {
		return fmt.Errorf("failed to create session: %w", err)
	}
	return nil
}

func (r *sessionRepository) Get(sessionID string) (*store.SessionData, error) {
	model := &TenantSession{SessionID: sessionID}
	err := r.db.Model(model).WherePK().Select()
	if err != nil {
		if err == pg.ErrNoRows {
			return nil, nil // Not found is not an error
		}
		return nil, fmt.Errorf("failed to get session: %w", err)
	}

	// Check expiry
	if time.Now().After(model.ExpiresAt) {
		r.Delete(sessionID)
		return nil, nil
	}

	return r.toData(model), nil
}

func (r *sessionRepository) Validate(sessionID string) (string, error) {
	session, err := r.Get(sessionID)
	if err != nil {
		return "", err
	}
	if session == nil {
		return "", nil
	}
	return session.TenantID, nil
}

func (r *sessionRepository) Delete(sessionID string) error {
	model := &TenantSession{SessionID: sessionID}
	_, err := r.db.Model(model).WherePK().Delete()
	if err != nil && err != pg.ErrNoRows {
		return fmt.Errorf("failed to delete session: %w", err)
	}
	return nil
}

func (r *sessionRepository) DeleteByTenant(tenantID string) error {
	_, err := r.db.Model((*TenantSession)(nil)).
		Where("tenant_id = ?", tenantID).
		Delete()
	if err != nil {
		return fmt.Errorf("failed to delete tenant sessions: %w", err)
	}
	return nil
}

func (r *sessionRepository) ListByTenant(tenantID string) ([]*store.SessionData, error) {
	var models []TenantSession
	err := r.db.Model(&models).
		Where("tenant_id = ?", tenantID).
		Where("expires_at > ?", time.Now()).
		Order("created_at DESC").
		Select()
	if err != nil {
		return nil, fmt.Errorf("failed to list sessions: %w", err)
	}

	result := make([]*store.SessionData, len(models))
	for i := range models {
		result[i] = r.toData(&models[i])
	}
	return result, nil
}

func (r *sessionRepository) UpdateActivity(sessionID string) error {
	_, err := r.db.Model((*TenantSession)(nil)).
		Set("last_activity = ?", time.Now()).
		Where("session_id = ?", sessionID).
		Update()
	if err != nil {
		return fmt.Errorf("failed to update session activity: %w", err)
	}
	return nil
}

func (r *sessionRepository) HasTrustedDevice(tenantID, deviceHash string) bool {
	exists, _ := r.db.Model((*TenantSession)(nil)).
		Where("tenant_id = ?", tenantID).
		Where("device_hash = ?", deviceHash).
		Where("trusted_device = TRUE").
		Where("expires_at > ?", time.Now()).
		Exists()
	return exists
}

func (r *sessionRepository) CleanupExpired() (int, error) {
	result, err := r.db.Model((*TenantSession)(nil)).
		Where("expires_at < ?", time.Now()).
		Delete()
	if err != nil {
		return 0, fmt.Errorf("failed to cleanup sessions: %w", err)
	}
	return result.RowsAffected(), nil
}

func (r *sessionRepository) toData(m *TenantSession) *store.SessionData {
	return &store.SessionData{
		SessionID:     m.SessionID,
		TenantID:      m.TenantID,
		Email:         m.Email,
		FullName:      m.FullName,
		SessionToken:  m.SessionToken,
		CreatedAt:     m.CreatedAt,
		ExpiresAt:     m.ExpiresAt,
		LastActivity:  m.LastActivity,
		IPAddress:     m.IPAddress,
		UserAgent:     m.UserAgent,
		RememberMe:    m.RememberMe,
		DeviceHash:    m.DeviceHash,
		TrustedDevice: m.TrustedDevice,
	}
}
