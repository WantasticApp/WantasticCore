package postgres

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"time"

	"WantasticCore/internal/tenant"

	"github.com/go-pg/pg/v10"
)

// TenantStore implements tenant persistence in PostgreSQL.
type TenantStore struct {
	db *pg.DB
}

// NewTenantStore creates a new tenant store.
func NewTenantStore(db *pg.DB) *TenantStore {
	return &TenantStore{db: db}
}

// CreateTenant stores a new tenant.
func (s *TenantStore) CreateTenant(t *tenant.Tenant) error {
	pgTenant := &Tenant{
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
	}

	_, err := s.db.Model(pgTenant).Insert()
	if err != nil {
		return fmt.Errorf("failed to insert tenant: %w", err)
	}
	return nil
}

// GetTenant retrieves a tenant by ID.
func (s *TenantStore) GetTenant(id string) (*tenant.Tenant, error) {
	pgTenant := &Tenant{ID: id}
	err := s.db.Model(pgTenant).WherePK().Select()
	if err != nil {
		if err == pg.ErrNoRows {
			return nil, fmt.Errorf("tenant not found: %s", id)
		}
		return nil, fmt.Errorf("failed to get tenant: %w", err)
	}
	return s.toTenant(pgTenant), nil
}

// GetTenantByEmail retrieves a tenant by email.
func (s *TenantStore) GetTenantByEmail(email string) (*tenant.Tenant, error) {
	pgTenant := &Tenant{}
	err := s.db.Model(pgTenant).Where("LOWER(email) = LOWER(?)", email).Select()
	if err != nil {
		if err == pg.ErrNoRows {
			return nil, fmt.Errorf("tenant not found with email: %s", email)
		}
		return nil, fmt.Errorf("failed to get tenant by email: %w", err)
	}
	return s.toTenant(pgTenant), nil
}

// GetTenantByOverlayAccount retrieves a tenant by overlay account ID.
func (s *TenantStore) GetTenantByOverlayAccount(accountID string) (*tenant.Tenant, error) {
	pgTenant := &Tenant{}
	err := s.db.Model(pgTenant).Where("overlay_account_id = ?", accountID).Select()
	if err != nil {
		if err == pg.ErrNoRows {
			return nil, fmt.Errorf("tenant not found with overlay account: %s", accountID)
		}
		return nil, fmt.Errorf("failed to get tenant by overlay account: %w", err)
	}
	return s.toTenant(pgTenant), nil
}

// UpdateTenant updates an existing tenant.
func (s *TenantStore) UpdateTenant(t *tenant.Tenant) error {
	pgTenant := &Tenant{
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

	_, err := s.db.Model(pgTenant).WherePK().Update()
	if err != nil {
		return fmt.Errorf("failed to update tenant: %w", err)
	}
	return nil
}

// GetTenantByAuth0Sub retrieves a tenant by their Auth0 subject identifier.
func (s *TenantStore) GetTenantByAuth0Sub(auth0Sub string) (*tenant.Tenant, error) {
	pgTenant := &Tenant{}
	err := s.db.Model(pgTenant).Where("auth0_sub = ?", auth0Sub).Select()
	if err != nil {
		if err == pg.ErrNoRows {
			return nil, fmt.Errorf("tenant not found with auth0_sub: %s", auth0Sub)
		}
		return nil, fmt.Errorf("failed to get tenant by auth0_sub: %w", err)
	}
	return s.toTenant(pgTenant), nil
}

// DeleteTenant removes a tenant.
func (s *TenantStore) DeleteTenant(id string) error {
	pgTenant := &Tenant{ID: id}
	_, err := s.db.Model(pgTenant).WherePK().Delete()
	if err != nil {
		return fmt.Errorf("failed to delete tenant: %w", err)
	}
	return nil
}

// ListTenants returns all tenants.
func (s *TenantStore) ListTenants() ([]*tenant.Tenant, error) {
	var pgTenants []Tenant
	err := s.db.Model(&pgTenants).Select()
	if err != nil {
		return nil, fmt.Errorf("failed to list tenants: %w", err)
	}

	tenants := make([]*tenant.Tenant, len(pgTenants))
	for i, pgTenant := range pgTenants {
		tenants[i] = s.toTenant(&pgTenant)
	}
	return tenants, nil
}

// SetOverlayAccountID links a tenant to an overlay account.
func (s *TenantStore) SetOverlayAccountID(tenantID, overlayAccountID string, networks []string) error {
	t, err := s.GetTenant(tenantID)
	if err != nil {
		return err
	}

	t.OverlayAccountID = overlayAccountID
	t.Networks = networks
	return s.UpdateTenant(t)
}

// UpdateLastLogin updates the last login timestamp.
func (s *TenantStore) UpdateLastLogin(tenantID string) error {
	t, err := s.GetTenant(tenantID)
	if err != nil {
		return err
	}

	t.LastLogin = time.Now()
	return s.UpdateTenant(t)
}

// SetTenantStatus updates the tenant status.
func (s *TenantStore) SetTenantStatus(tenantID, status string) error {
	t, err := s.GetTenant(tenantID)
	if err != nil {
		return err
	}

	t.Status = status
	return s.UpdateTenant(t)
}

// SetTwoFAMethod sets the 2FA method for a tenant.
// SMS 2FA was removed in Phase 3; only TOTP, email, and WhatsApp are supported.
func (s *TenantStore) SetTwoFAMethod(tenantID, method string, totpSecret string) error {
	t, err := s.GetTenant(tenantID)
	if err != nil {
		return err
	}

	t.TwoFAMethod = "none"
	t.TOTPEnabled = false
	t.TOTPSecret = ""
	t.TwoFAWhatsApp = false
	t.TwoFAPendingCode = ""
	t.TwoFACodeExpiry = time.Time{}
	t.TwoFACodeAttempts = 0

	switch method {
	case "totp":
		if totpSecret == "" {
			return fmt.Errorf("TOTP secret required for TOTP method")
		}
		t.TwoFAMethod = "totp"
		t.TOTPEnabled = true
		t.TOTPSecret = totpSecret
	case "email":
		if t.Email == "" {
			return fmt.Errorf("email address required for email 2FA")
		}
		t.TwoFAMethod = "email"
	case "whatsapp":
		t.TwoFAMethod = "whatsapp"
		t.TwoFAWhatsApp = true
	case "none", "":
		t.TwoFAMethod = "none"
	default:
		return fmt.Errorf("invalid 2FA method: %s", method)
	}

	return s.UpdateTenant(t)
}

// GetTwoFAInfo returns 2FA configuration information.
func (s *TenantStore) GetTwoFAInfo(tenantID string) (*tenant.TwoFAInfo, error) {
	t, err := s.GetTenant(tenantID)
	if err != nil {
		return nil, err
	}

	// SMS 2FA removed in Phase 3; choices are TOTP, email (via SMTP), or WhatsApp (via adminbot).
	canChangeTo := []string{"totp", "email", "whatsapp"}

	return &tenant.TwoFAInfo{
		Enabled:     t.TwoFAMethod != "none" && t.TwoFAMethod != "",
		Method:      t.TwoFAMethod,
		PhoneMasked: "",
		CanChangeTo: canChangeTo,
	}, nil
}

// GetActiveTwoFAMethod returns the currently active 2FA method.
func (s *TenantStore) GetActiveTwoFAMethod(tenantID string) (string, error) {
	t, err := s.GetTenant(tenantID)
	if err != nil {
		return "", err
	}

	if t.TwoFAMethod != "" && t.TwoFAMethod != "none" {
		return t.TwoFAMethod, nil
	}
	return "none", nil
}

// IsTwoFAEnabled checks if 2FA is enabled.
func (s *TenantStore) IsTwoFAEnabled(tenantID string) (bool, error) {
	method, err := s.GetActiveTwoFAMethod(tenantID)
	if err != nil {
		return false, err
	}
	return method != "none", nil
}

// SetPending2FACode sets a pending 2FA code.
func (s *TenantStore) SetPending2FACode(tenantID, code string, expiresIn time.Duration) error {
	t, err := s.GetTenant(tenantID)
	if err != nil {
		return err
	}

	t.TwoFAPendingCode = code
	t.TwoFACodeExpiry = time.Now().Add(expiresIn)
	t.TwoFACodeAttempts = 0
	return s.UpdateTenant(t)
}

// Verify2FACode verifies a pending 2FA code.
func (s *TenantStore) Verify2FACode(tenantID, code string) (bool, bool, bool, error) {
	t, err := s.GetTenant(tenantID)
	if err != nil {
		return false, false, false, err
	}

	if t.TwoFAPendingCode == "" {
		return false, false, false, nil
	}

	if time.Now().After(t.TwoFACodeExpiry) {
		// Expired
		return false, true, false, nil
	}

	if t.TwoFACodeAttempts >= 3 {
		// Too many attempts
		return false, false, true, nil
	}

	if t.TwoFAPendingCode != code {
		t.TwoFACodeAttempts++
		if err := s.UpdateTenant(t); err != nil {
			return false, false, false, err
		}
		return false, false, false, nil
	}

	// Success - clear code
	t.TwoFAPendingCode = ""
	t.TwoFACodeAttempts = 0
	if err := s.UpdateTenant(t); err != nil {
		return true, false, false, err
	}

	return true, false, false, nil
}

// Clear2FACode clears the pending 2FA code.
func (s *TenantStore) Clear2FACode(tenantID string) error {
	t, err := s.GetTenant(tenantID)
	if err != nil {
		return err
	}

	t.TwoFAPendingCode = ""
	t.TwoFACodeAttempts = 0
	return s.UpdateTenant(t)
}

// CreateSession creates a new tenant session.
func (s *TenantStore) CreateSession(tenantID, sessionID, ipAddress, userAgent, deviceHash string, duration time.Duration, trustedDevice bool) error {
	now := time.Now()
	// sessionID is passed in

	// Clean up old sessions from same device if deviceHash provided
	if deviceHash != "" {
		_, err := s.db.Model(&TenantSession{}).
			Where("tenant_id = ? AND device_hash = ?", tenantID, deviceHash).
			Delete()
		if err != nil {
			// Log but don't fail session creation
			_ = err
		}
	}

	session := &tenant.TenantSession{
		SessionID:     sessionID,
		TenantID:      tenantID,
		SessionToken:  sessionID,
		CreatedAt:     now,
		ExpiresAt:     now.Add(duration),
		LastActivity:  now,
		IPAddress:     ipAddress,
		UserAgent:     userAgent,
		DeviceHash:    deviceHash,
		TrustedDevice: trustedDevice,
	}

	pgSession := toPgSession(session)
	_, err := s.db.Model(pgSession).Insert()
	if err != nil {
		return fmt.Errorf("failed to insert session: %w", err)
	}

	return nil
}

// DeleteAllUserSessions removes all sessions for a tenant.
func (s *TenantStore) DeleteAllUserSessions(tenantID string) error {
	_, err := s.db.Model(&TenantSession{}).Where("tenant_id = ?", tenantID).Delete()
	if err != nil {
		return fmt.Errorf("failed to delete user sessions: %w", err)
	}
	return nil
}

// GetUserActiveSessions returns all active sessions for a tenant.
func (s *TenantStore) GetUserActiveSessions(tenantID string) ([]*tenant.TenantSession, error) {
	var pgSessions []*TenantSession
	err := s.db.Model(&pgSessions).Where("tenant_id = ?", tenantID).Select()
	if err != nil {
		return nil, fmt.Errorf("failed to get user active sessions: %w", err)
	}

	sessions := make([]*tenant.TenantSession, len(pgSessions))
	for i, pgSession := range pgSessions {
		sessions[i] = s.toTenantSession(pgSession)
	}
	return sessions, nil
}

func toPgSession(s *tenant.TenantSession) *TenantSession {
	return &TenantSession{
		SessionID:     s.SessionID,
		TenantID:      s.TenantID,
		Email:         s.Email,
		FullName:      s.FullName,
		SessionToken:  s.SessionToken,
		CreatedAt:     s.CreatedAt,
		ExpiresAt:     s.ExpiresAt,
		LastActivity:  s.LastActivity,
		IPAddress:     s.IPAddress,
		UserAgent:     s.UserAgent,
		RememberMe:    s.RememberMe,
		DeviceHash:    s.DeviceHash,
		TrustedDevice: s.TrustedDevice,
	}
}

// generateSessionID creates a secure random session ID.
func generateSessionID() string {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		panic(err)
	}
	return hex.EncodeToString(b)
}

// ValidateSession validates a session token and returns the tenant ID.
func (s *TenantStore) ValidateSession(sessionID string) (string, error) {
	pgSession := &TenantSession{SessionID: sessionID}
	err := s.db.Model(pgSession).WherePK().Select()
	if err != nil {
		if err == pg.ErrNoRows {
			return "", fmt.Errorf("session not found")
		}
		return "", fmt.Errorf("failed to get session: %w", err)
	}

	if time.Now().After(pgSession.ExpiresAt) {
		_ = s.DeleteSession(sessionID)
		return "", fmt.Errorf("session expired")
	}

	return pgSession.TenantID, nil
}

// GetSession returns the full session object for a session ID.
func (s *TenantStore) GetSession(sessionID string) (*tenant.TenantSession, error) {
	pgSession := &TenantSession{SessionID: sessionID}
	err := s.db.Model(pgSession).WherePK().Select()
	if err != nil {
		if err == pg.ErrNoRows {
			return nil, fmt.Errorf("session not found")
		}
		return nil, fmt.Errorf("failed to get session: %w", err)
	}

	if time.Now().After(pgSession.ExpiresAt) {
		_ = s.DeleteSession(sessionID)
		return nil, fmt.Errorf("session expired")
	}

	return s.toTenantSession(pgSession), nil
}

// DeleteSession removes a tenant session.
func (s *TenantStore) DeleteSession(sessionID string) error {
	pgSession := &TenantSession{SessionID: sessionID}
	_, err := s.db.Model(pgSession).WherePK().Delete()
	if err != nil {
		return fmt.Errorf("failed to delete session: %w", err)
	}
	return nil
}

// GetTenantSessions retrieves all active sessions for a tenant.
func (s *TenantStore) GetTenantSessions(tenantID string) ([]*tenant.TenantSession, error) {
	var pgSessions []TenantSession
	err := s.db.Model(&pgSessions).
		Where("tenant_id = ?", tenantID).
		Where("expires_at > ?", time.Now()).
		Order("last_activity DESC").
		Select()
	if err != nil {
		return nil, fmt.Errorf("failed to list sessions: %w", err)
	}

	sessions := make([]*tenant.TenantSession, len(pgSessions))
	for i, pgSession := range pgSessions {
		sessions[i] = s.toTenantSession(&pgSession)
	}
	return sessions, nil
}

// DeleteTenantSession deletes a specific session for a tenant (with ownership check).
func (s *TenantStore) DeleteTenantSession(tenantID, sessionID string) error {
	pgSession := &TenantSession{SessionID: sessionID}
	err := s.db.Model(pgSession).WherePK().Select()
	if err != nil {
		return err
	}

	if pgSession.TenantID != tenantID {
		return fmt.Errorf("session does not belong to this tenant")
	}

	_, err = s.db.Model(pgSession).WherePK().Delete()
	if err != nil {
		return fmt.Errorf("failed to delete session: %w", err)
	}
	return nil
}

// UpdateSessionActivity updates the last activity timestamp for a session.
func (s *TenantStore) UpdateSessionActivity(sessionID string) error {
	pgSession := &TenantSession{SessionID: sessionID}
	_, err := s.db.Model(pgSession).
		Set("last_activity = ?", time.Now()).
		WherePK().
		Update()
	if err != nil {
		return fmt.Errorf("failed to update session activity: %w", err)
	}
	return nil
}

// HasTrustedDevice checks if a tenant has a trusted device.
func (s *TenantStore) HasTrustedDevice(tenantID, deviceHash string) bool {
	count, err := s.db.Model((*TenantSession)(nil)).
		Where("tenant_id = ?", tenantID).
		Where("device_hash = ?", deviceHash).
		Where("remember_me = ?", true).
		Where("expires_at > ?", time.Now()).
		Count()

	if err != nil {
		return false
	}
	return count > 0
}

// InvalidateAllSessions invalidates all sessions for a tenant.
func (s *TenantStore) InvalidateAllSessions(tenantID string) error {
	_, err := s.db.Model((*TenantSession)(nil)).
		Where("tenant_id = ?", tenantID).
		Delete()
	if err != nil {
		return fmt.Errorf("failed to invalidate all sessions: %w", err)
	}
	return nil
}

// CleanupExpiredSessions removes all expired sessions.
func (s *TenantStore) CleanupExpiredSessions() (int, error) {
	res, err := s.db.Model((*TenantSession)(nil)).
		Where("expires_at < ?", time.Now()).
		Delete()
	if err != nil {
		return 0, fmt.Errorf("failed to cleanup expired sessions: %w", err)
	}
	return res.RowsAffected(), nil
}

// UpdatePassword updates the tenant's password.
func (s *TenantStore) UpdatePassword(tenantID, passwordHash string) error {
	t, err := s.GetTenant(tenantID)
	if err != nil {
		return err
	}

	t.PasswordHash = passwordHash
	return s.UpdateTenant(t)
}

// Helper methods

func (s *TenantStore) toTenant(pgTenant *Tenant) *tenant.Tenant {
	return &tenant.Tenant{
		ID:                      pgTenant.ID,
		Email:                   pgTenant.Email,
		FullName:                pgTenant.FullName,
		PasswordHash:            pgTenant.PasswordHash,
		TOTPSecret:              pgTenant.TOTPSecret,
		TOTPEnabled:             pgTenant.TOTPEnabled,
		LastLogin:               pgTenant.LastLogin,
		TwoFAMethod:             pgTenant.TwoFAMethod,
		TwoFAWhatsApp:           pgTenant.TwoFAWhatsApp,
		TwoFAPendingCode:        pgTenant.TwoFAPendingCode,
		TwoFACodeExpiry:         pgTenant.TwoFACodeExpiry,
		TwoFACodeAttempts:       pgTenant.TwoFACodeAttempts,
		OverlayAccountID:        pgTenant.OverlayAccountID,
		Networks:                pgTenant.Networks,
		Status:                  pgTenant.Status,
		IsAdmin:                 pgTenant.IsAdmin,
		PreferredLanguage:       pgTenant.PreferredLanguage,
		InactivityWarningSentAt: pgTenant.InactivityWarningSentAt,
		Auth0Sub:                pgTenant.Auth0Sub,
		CreatedAt:               pgTenant.CreatedAt,
		UpdatedAt:               pgTenant.UpdatedAt,
	}
}

func (s *TenantStore) toTenantSession(pgSession *TenantSession) *tenant.TenantSession {
	return &tenant.TenantSession{
		SessionID:     pgSession.SessionID,
		TenantID:      pgSession.TenantID,
		Email:         pgSession.Email,
		FullName:      pgSession.FullName,
		SessionToken:  pgSession.SessionToken,
		CreatedAt:     pgSession.CreatedAt,
		ExpiresAt:     pgSession.ExpiresAt,
		LastActivity:  pgSession.LastActivity,
		IPAddress:     pgSession.IPAddress,
		UserAgent:     pgSession.UserAgent,
		RememberMe:    pgSession.RememberMe,
		TrustedDevice: pgSession.TrustedDevice,
		DeviceHash:    pgSession.DeviceHash,
	}
}

// Enrollment Token Implementation

func (s *TenantStore) CreateEnrollmentToken(t *tenant.EnrollmentToken) error {
	pgToken := &EnrollmentToken{
		ID:         t.ID,
		TenantID:   t.TenantID,
		Name:       t.Name,
		Token:      t.Token,
		MaxUses:    t.MaxUses,
		UsageCount: t.UsageCount,
		ExpiresAt:  t.ExpiresAt,
		CreatedAt:  t.CreatedAt,
		CreatedBy:  t.CreatedBy,
	}

	_, err := s.db.Model(pgToken).Insert()
	if err != nil {
		return fmt.Errorf("failed to insert enrollment token: %w", err)
	}
	return nil
}

func (s *TenantStore) GetEnrollmentToken(tokenID string) (*tenant.EnrollmentToken, error) {
	pgToken := &EnrollmentToken{ID: tokenID}
	err := s.db.Model(pgToken).WherePK().Select()
	if err != nil {
		if err == pg.ErrNoRows {
			return nil, fmt.Errorf("enrollment token not found: %s", tokenID)
		}
		return nil, fmt.Errorf("failed to get enrollment token: %w", err)
	}
	return s.toEnrollmentToken(pgToken), nil
}

func (s *TenantStore) ListEnrollmentTokens(tenantID string) ([]*tenant.EnrollmentToken, error) {
	var pgTokens []EnrollmentToken
	err := s.db.Model(&pgTokens).Where("tenant_id = ?", tenantID).Order("created_at DESC").Select()
	if err != nil {
		return nil, fmt.Errorf("failed to list enrollment tokens: %w", err)
	}

	tokens := make([]*tenant.EnrollmentToken, len(pgTokens))
	for i, pgToken := range pgTokens {
		tokens[i] = s.toEnrollmentToken(&pgToken)
	}
	return tokens, nil
}

func (s *TenantStore) DeleteEnrollmentToken(tenantID, tokenID string) error {
	_, err := s.db.Model((*EnrollmentToken)(nil)).
		Where("id = ? AND tenant_id = ?", tokenID, tenantID).
		Delete()
	if err != nil {
		return fmt.Errorf("failed to delete enrollment token: %w", err)
	}
	return nil
}

func (s *TenantStore) ValidateEnrollmentToken(tokenString string) (string, string, error) {
	pgToken := &EnrollmentToken{}
	err := s.db.Model(pgToken).Where("token = ?", tokenString).Select()
	if err != nil {
		if err == pg.ErrNoRows {
			return "", "", fmt.Errorf("invalid enrollment token")
		}
		return "", "", fmt.Errorf("failed to validate enrollment token: %w", err)
	}

	if !pgToken.ExpiresAt.IsZero() && time.Now().After(pgToken.ExpiresAt) {
		return "", "", fmt.Errorf("enrollment token expired")
	}

	if pgToken.MaxUses > 0 && pgToken.UsageCount >= pgToken.MaxUses {
		return "", "", fmt.Errorf("enrollment token usage limit reached")
	}

	return pgToken.TenantID, pgToken.ID, nil
}

func (s *TenantStore) IncrementEnrollmentTokenUsage(tokenID string) error {
	_, err := s.db.Model((*EnrollmentToken)(nil)).
		Set("usage_count = usage_count + 1").
		Where("id = ?", tokenID).
		Update()
	if err != nil {
		return fmt.Errorf("failed to increment enrollment token usage: %w", err)
	}
	return nil
}

func (s *TenantStore) CleanupExpiredEnrollmentTokens() (int, error) {
	res, err := s.db.Model((*EnrollmentToken)(nil)).
		Where("expires_at < ?", time.Now()).
		Delete()
	if err != nil {
		return 0, fmt.Errorf("failed to cleanup expired enrollment tokens: %w", err)
	}
	return res.RowsAffected(), nil
}

func (s *TenantStore) toEnrollmentToken(pgToken *EnrollmentToken) *tenant.EnrollmentToken {
	return &tenant.EnrollmentToken{
		ID:         pgToken.ID,
		TenantID:   pgToken.TenantID,
		Name:       pgToken.Name,
		Token:      pgToken.Token,
		MaxUses:    pgToken.MaxUses,
		UsageCount: pgToken.UsageCount,
		ExpiresAt:  pgToken.ExpiresAt,
		CreatedAt:  pgToken.CreatedAt,
		CreatedBy:  pgToken.CreatedBy,
	}
}

