package postgres

import (
	"fmt"
	"time"

	"WantasticCore/internal/store"

	"github.com/go-pg/pg/v10"
)

// tenantRepository implements store.TenantRepository using PostgreSQL.
type tenantRepository struct {
	db *pg.DB
}

// NewTenantRepository creates a new PostgreSQL tenant repository.
func NewTenantRepository(db *pg.DB) store.TenantRepository {
	return &tenantRepository{db: db}
}

func (r *tenantRepository) Create(t *store.TenantData) error {
	model := &Tenant{
		ID:                      t.ID,
		Email:                   t.Email,
		FullName:                t.FullName,
		PasswordHash:            t.PasswordHash,
		TOTPSecret:              t.TOTPSecret,
		TOTPEnabled:             t.TOTPEnabled,
		LastLogin:               t.LastLogin,
		TwoFAMethod:             t.TwoFAMethod,
		TwoFAWhatsApp:           t.TwoFAWhatsApp,
		TwoFAPendingCode:        t.TwoFAPendingCode,
		TwoFACodeExpiry:         t.TwoFACodeExpiry,
		TwoFACodeAttempts:       t.TwoFACodeAttempts,
		OverlayAccountID:        t.OverlayAccountID,
		Networks:                t.Networks,
		Status:                  t.Status,
		IsAdmin:                 t.IsAdmin,
		PreferredLanguage:       t.PreferredLanguage,
		InactivityWarningSentAt: t.InactivityWarningSentAt,
		Auth0Sub:                t.Auth0Sub,
		CreatedAt:               t.CreatedAt,
		UpdatedAt:               t.UpdatedAt,
	}

	if model.CreatedAt.IsZero() {
		model.CreatedAt = time.Now()
	}
	if model.UpdatedAt.IsZero() {
		model.UpdatedAt = time.Now()
	}

	_, err := r.db.Model(model).Insert()
	if err != nil {
		return fmt.Errorf("failed to create tenant: %w", err)
	}
	return nil
}

func (r *tenantRepository) Get(id string) (*store.TenantData, error) {
	model := &Tenant{ID: id}
	err := r.db.Model(model).WherePK().Select()
	if err != nil {
		if err == pg.ErrNoRows {
			return nil, fmt.Errorf("tenant not found: %s", id)
		}
		return nil, fmt.Errorf("failed to get tenant: %w", err)
	}
	return r.toData(model), nil
}

func (r *tenantRepository) GetByEmail(email string) (*store.TenantData, error) {
	model := &Tenant{}
	err := r.db.Model(model).Where("LOWER(email) = LOWER(?)", email).Select()
	if err != nil {
		if err == pg.ErrNoRows {
			return nil, fmt.Errorf("tenant not found with email: %s", email)
		}
		return nil, fmt.Errorf("failed to get tenant: %w", err)
	}
	return r.toData(model), nil
}

func (r *tenantRepository) GetByOverlayAccount(overlayAccountID string) (*store.TenantData, error) {
	model := &Tenant{}
	err := r.db.Model(model).Where("overlay_account_id = ?", overlayAccountID).Select()
	if err != nil {
		if err == pg.ErrNoRows {
			return nil, fmt.Errorf("tenant not found with overlay account: %s", overlayAccountID)
		}
		return nil, fmt.Errorf("failed to get tenant: %w", err)
	}
	return r.toData(model), nil
}

func (r *tenantRepository) GetByAuth0Sub(auth0Sub string) (*store.TenantData, error) {
	model := &Tenant{}
	err := r.db.Model(model).Where("auth0_sub = ?", auth0Sub).Select()
	if err != nil {
		if err == pg.ErrNoRows {
			return nil, fmt.Errorf("tenant not found with auth0_sub: %s", auth0Sub)
		}
		return nil, fmt.Errorf("failed to get tenant by auth0_sub: %w", err)
	}
	return r.toData(model), nil
}

func (r *tenantRepository) Update(t *store.TenantData) error {
	model := &Tenant{
		ID:                      t.ID,
		Email:                   t.Email,
		FullName:                t.FullName,
		PasswordHash:            t.PasswordHash,
		TOTPSecret:              t.TOTPSecret,
		TOTPEnabled:             t.TOTPEnabled,
		LastLogin:               t.LastLogin,
		TwoFAMethod:             t.TwoFAMethod,
		TwoFAWhatsApp:           t.TwoFAWhatsApp,
		TwoFAPendingCode:        t.TwoFAPendingCode,
		TwoFACodeExpiry:         t.TwoFACodeExpiry,
		TwoFACodeAttempts:       t.TwoFACodeAttempts,
		OverlayAccountID:        t.OverlayAccountID,
		Networks:                t.Networks,
		Status:                  t.Status,
		IsAdmin:                 t.IsAdmin,
		PreferredLanguage:       t.PreferredLanguage,
		InactivityWarningSentAt: t.InactivityWarningSentAt,
		Auth0Sub:                t.Auth0Sub,
		CreatedAt:               t.CreatedAt,
		UpdatedAt:               time.Now(),
	}

	result, err := r.db.Model(model).WherePK().Update()
	if err != nil {
		return fmt.Errorf("failed to update tenant: %w", err)
	}
	if result.RowsAffected() == 0 {
		return fmt.Errorf("tenant not found: %s", t.ID)
	}
	return nil
}

func (r *tenantRepository) Delete(id string) error {
	model := &Tenant{ID: id}
	result, err := r.db.Model(model).WherePK().Delete()
	if err != nil {
		return fmt.Errorf("failed to delete tenant: %w", err)
	}
	if result.RowsAffected() == 0 {
		return fmt.Errorf("tenant not found: %s", id)
	}
	return nil
}

func (r *tenantRepository) List() ([]*store.TenantData, error) {
	var models []Tenant
	err := r.db.Model(&models).Order("created_at DESC").Select()
	if err != nil {
		return nil, fmt.Errorf("failed to list tenants: %w", err)
	}

	result := make([]*store.TenantData, len(models))
	for i := range models {
		result[i] = r.toData(&models[i])
	}
	return result, nil
}

// 2FA & status helpers

func (r *tenantRepository) SetTwoFAMethod(tenantID, method string, totpSecret string) error {
	_, err := r.db.Model((*Tenant)(nil)).
		Set("twofa_method = ?", method).
		Set("totp_secret = ?", totpSecret).
		Set("totp_enabled = ?", method == "totp"). // Simplified logic
		Where("id = ?", tenantID).
		Update()
	return err
}

func (r *tenantRepository) GetActiveTwoFAMethod(tenantID string) (string, error) {
	var method string
	err := r.db.Model((*Tenant)(nil)).
		Column("twofa_method").
		Where("id = ?", tenantID).
		Select(&method)
	return method, err
}

func (r *tenantRepository) IsTwoFAEnabled(tenantID string) (bool, error) {
	var enabled bool
	// Check if any method is enabled.
	// Simplification: just check if method is not empty/none
	err := r.db.Model((*Tenant)(nil)).
		ColumnExpr("twofa_method != '' AND twofa_method != 'none'").
		Where("id = ?", tenantID).
		Select(&enabled)
	return enabled, err
}

func (r *tenantRepository) SetPending2FACode(tenantID, code string, expiresIn time.Duration) error {
	_, err := r.db.Model((*Tenant)(nil)).
		Set("twofa_pending_code = ?", code).
		Set("twofa_code_expiry = ?", time.Now().Add(expiresIn)).
		Set("twofa_code_attempts = 0").
		Where("id = ?", tenantID).
		Update()
	return err
}

func (r *tenantRepository) Verify2FACode(tenantID, code string) (bool, bool, bool, error) {
	// Returns valid, expired, maxAttempts, err
	var t Tenant
	err := r.db.Model(&t).Where("id = ?", tenantID).Select()
	if err != nil {
		return false, false, false, err
	}

	if t.TwoFACodeAttempts >= 3 {
		return false, false, true, nil
	}

	if time.Now().After(t.TwoFACodeExpiry) {
		return false, true, false, nil
	}

	if t.TwoFAPendingCode != code {
		// Increment attempts
		r.db.Model(&t).Set("twofa_code_attempts = twofa_code_attempts + 1").WherePK().Update()
		return false, false, false, nil
	}

	// Success
	r.Clear2FACode(tenantID)
	return true, false, false, nil
}

func (r *tenantRepository) Clear2FACode(tenantID string) error {
	_, err := r.db.Model((*Tenant)(nil)).
		Set("twofa_pending_code = NULL").
		Set("twofa_code_expiry = NULL").
		Set("twofa_code_attempts = 0").
		Where("id = ?", tenantID).
		Update()
	return err
}

func (r *tenantRepository) SetOverlayAccountID(tenantID, overlayAccountID string, networks []string) error {
	_, err := r.db.Model((*Tenant)(nil)).
		Set("overlay_account_id = ?", overlayAccountID).
		Set("networks = ?", pg.Array(networks)).
		Where("id = ?", tenantID).
		Update()
	return err
}

func (r *tenantRepository) UpdateLastLogin(tenantID string) error {
	_, err := r.db.Model((*Tenant)(nil)).
		Set("last_login = ?", time.Now()).
		Where("id = ?", tenantID).
		Update()
	return err
}

func (r *tenantRepository) SetStatus(tenantID, status string) error {
	_, err := r.db.Model((*Tenant)(nil)).
		Set("status = ?", status).
		Where("id = ?", tenantID).
		Update()
	return err
}

func (r *tenantRepository) UpdatePassword(tenantID, passwordHash string) error {
	_, err := r.db.Model((*Tenant)(nil)).
		Set("password_hash = ?", passwordHash).
		Where("id = ?", tenantID).
		Update()
	return err
}

func (r *tenantRepository) toData(m *Tenant) *store.TenantData {
	return &store.TenantData{
		ID:                      m.ID,
		Email:                   m.Email,
		FullName:                m.FullName,
		PasswordHash:            m.PasswordHash,
		TOTPSecret:              m.TOTPSecret,
		TOTPEnabled:             m.TOTPEnabled,
		LastLogin:               m.LastLogin,
		TwoFAMethod:             m.TwoFAMethod,
		TwoFAWhatsApp:           m.TwoFAWhatsApp,
		TwoFAPendingCode:        m.TwoFAPendingCode,
		TwoFACodeExpiry:         m.TwoFACodeExpiry,
		TwoFACodeAttempts:       m.TwoFACodeAttempts,
		OverlayAccountID:        m.OverlayAccountID,
		Networks:                m.Networks,
		Status:                  m.Status,
		IsAdmin:                 m.IsAdmin,
		PreferredLanguage:       m.PreferredLanguage,
		InactivityWarningSentAt: m.InactivityWarningSentAt,
		Auth0Sub:                m.Auth0Sub,
		CreatedAt:               m.CreatedAt,
		UpdatedAt:               m.UpdatedAt,
	}
}
