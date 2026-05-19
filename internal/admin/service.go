// Package admin provides super-admin tenant management for in-process callers
// (the portal's admin HTTP routes and the setup wizard's bootstrap step).
// Replaces the previous public self-registration flow.
package admin

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"golang.org/x/crypto/bcrypt"

	"WantasticCore/internal/account"
	"WantasticCore/internal/tenant"
)

// OverlayServer is the subset of *server.Server methods the admin service
// needs. Kept as an interface so the package is testable with a fake.
type OverlayServer interface {
	CreateAccount(name string, maxPeers int) (*account.Account, error)
	DeleteAccount(accountID string) error
	GetAccount(accountID string) (*account.Account, error)
	SetAccountMaxPeers(accountID string, maxPeers int) (*account.Account, error)
	// GetAccountPeerStats returns (current, max, err) — used by the tenant
	// summary view. Existing *server.Server already provides this.
	GetAccountPeerStats(accountID string) (current, max int, err error)
}

// Service exposes tenant CRUD and limit management to admin callers.
// All methods assume the caller has already been authorized as an admin.
type Service struct {
	srv      OverlayServer
	registry tenant.Registry
}

// New creates a new admin service.
func New(srv OverlayServer, registry tenant.Registry) *Service {
	return &Service{srv: srv, registry: registry}
}

// Authorize confirms the caller is an admin. Used by callers (e.g. the
// portal's WS dispatcher) to gate admin endpoints with a single helper.
func (s *Service) Authorize(callerTenantID string) error {
	if callerTenantID == "" {
		return errors.New("unauthenticated")
	}
	t, err := s.registry.GetTenant(callerTenantID)
	if err != nil {
		return fmt.Errorf("authorize: %w", err)
	}
	if !t.IsAdmin {
		return errors.New("forbidden: admin role required")
	}
	return nil
}

// TenantSummary is a lightweight view of a tenant for listing.
type TenantSummary struct {
	ID          string    `json:"id"`
	Email       string    `json:"email"`
	FullName    string    `json:"full_name"`
	Status      string    `json:"status"`
	IsAdmin     bool      `json:"is_admin"`
	AccountID   string    `json:"account_id"`
	MaxPeers    int       `json:"max_peers"`
	PeerCount   int       `json:"peer_count"`
	LastLogin   time.Time `json:"last_login"`
	CreatedAt   time.Time `json:"created_at"`
}

// CreateTenantInput is the payload accepted by CreateTenant.
type CreateTenantInput struct {
	Email    string
	FullName string
	Password string
	MaxPeers int
	IsAdmin  bool
}

// CreateTenant provisions a new tenant with its overlay account. The tenant is
// admin-created, so it lands fully activated (email verified by design).
func (s *Service) CreateTenant(in CreateTenantInput) (*tenant.Tenant, error) {
	email := strings.TrimSpace(strings.ToLower(in.Email))
	if email == "" {
		return nil, errors.New("email is required")
	}
	if in.FullName == "" {
		return nil, errors.New("full name is required")
	}
	if in.Password == "" {
		return nil, errors.New("password is required")
	}
	if in.MaxPeers <= 0 {
		in.MaxPeers = account.DefaultMaxPeers
	}

	if existing, _ := s.registry.GetTenantByEmail(email); existing != nil {
		return nil, fmt.Errorf("tenant with email %q already exists", email)
	}

	hash, err := bcrypt.GenerateFromPassword([]byte(in.Password), bcrypt.DefaultCost)
	if err != nil {
		return nil, fmt.Errorf("hash password: %w", err)
	}

	acc, err := s.srv.CreateAccount(in.FullName, in.MaxPeers)
	if err != nil {
		return nil, fmt.Errorf("create overlay account: %w", err)
	}

	networks := append([]string(nil), acc.Networks...)

	now := time.Now().UTC()
	t := &tenant.Tenant{
		ID:                uuid.New().String(),
		Email:             email,
		FullName:          in.FullName,
		PasswordHash:      string(hash),
		Status:            "active",
		IsAdmin:           in.IsAdmin,
		OverlayAccountID:  acc.ID,
		Networks:          networks,
		PreferredLanguage: "en",
		TwoFAMethod:       tenant.TwoFANone,
		CreatedAt:         now,
		UpdatedAt:         now,
	}

	if err := s.registry.CreateTenant(t); err != nil {
		// Best-effort rollback of the overlay account so we don't leak resources.
		_ = s.srv.DeleteAccount(acc.ID)
		return nil, fmt.Errorf("persist tenant: %w", err)
	}
	return t, nil
}

// ListTenants returns a summary view of every tenant in the system.
func (s *Service) ListTenants() ([]TenantSummary, error) {
	tenants, err := s.registry.ListTenants()
	if err != nil {
		return nil, fmt.Errorf("list tenants: %w", err)
	}
	out := make([]TenantSummary, 0, len(tenants))
	for _, t := range tenants {
		out = append(out, s.summarize(t))
	}
	return out, nil
}

// GetTenant returns the full tenant view by ID.
func (s *Service) GetTenant(id string) (*tenant.Tenant, error) {
	return s.registry.GetTenant(id)
}

// DeleteTenant removes a tenant and releases its overlay account + peers.
// It refuses to delete the last remaining admin so the system stays manageable.
func (s *Service) DeleteTenant(id string) error {
	t, err := s.registry.GetTenant(id)
	if err != nil {
		return err
	}
	if t.IsAdmin {
		if remaining, err := s.countOtherAdmins(id); err != nil {
			return err
		} else if remaining == 0 {
			return errors.New("cannot delete the last admin")
		}
	}

	if t.OverlayAccountID != "" {
		if err := s.srv.DeleteAccount(t.OverlayAccountID); err != nil {
			return fmt.Errorf("delete overlay account: %w", err)
		}
	}
	_ = s.registry.InvalidateAllSessions(id)
	if err := s.registry.DeleteTenant(id); err != nil {
		return fmt.Errorf("delete tenant: %w", err)
	}
	return nil
}

// SetTenantMaxPeers updates the device cap and resizes the overlay allocation.
func (s *Service) SetTenantMaxPeers(id string, maxPeers int) error {
	if maxPeers <= 0 {
		return errors.New("max_peers must be positive")
	}
	t, err := s.registry.GetTenant(id)
	if err != nil {
		return err
	}
	if t.OverlayAccountID == "" {
		return errors.New("tenant has no overlay account")
	}
	if _, err := s.srv.SetAccountMaxPeers(t.OverlayAccountID, maxPeers); err != nil {
		return fmt.Errorf("set max peers: %w", err)
	}
	return nil
}

// SetTenantPassword overwrites the tenant's password (admin-driven reset).
func (s *Service) SetTenantPassword(id, newPassword string) error {
	if newPassword == "" {
		return errors.New("password is required")
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(newPassword), bcrypt.DefaultCost)
	if err != nil {
		return fmt.Errorf("hash password: %w", err)
	}
	if err := s.registry.UpdatePassword(id, string(hash)); err != nil {
		return err
	}
	_ = s.registry.InvalidateAllSessions(id)
	return nil
}

// SetTenantAdmin toggles the IsAdmin flag.
func (s *Service) SetTenantAdmin(id string, isAdmin bool) error {
	t, err := s.registry.GetTenant(id)
	if err != nil {
		return err
	}
	if t.IsAdmin && !isAdmin {
		if remaining, err := s.countOtherAdmins(id); err != nil {
			return err
		} else if remaining == 0 {
			return errors.New("cannot demote the last admin")
		}
	}
	t.IsAdmin = isAdmin
	t.UpdatedAt = time.Now().UTC()
	return s.registry.UpdateTenant(t)
}

// SetTenantStatus toggles active/paused.
func (s *Service) SetTenantStatus(id, status string) error {
	switch status {
	case "active", "paused", "disabled":
	default:
		return fmt.Errorf("invalid status %q", status)
	}
	return s.registry.SetTenantStatus(id, status)
}

func (s *Service) countOtherAdmins(excludeID string) (int, error) {
	all, err := s.registry.ListTenants()
	if err != nil {
		return 0, fmt.Errorf("list tenants: %w", err)
	}
	n := 0
	for _, t := range all {
		if t.IsAdmin && t.ID != excludeID {
			n++
		}
	}
	return n, nil
}

func (s *Service) summarize(t *tenant.Tenant) TenantSummary {
	sum := TenantSummary{
		ID:        t.ID,
		Email:     t.Email,
		FullName:  t.FullName,
		Status:    t.Status,
		IsAdmin:   t.IsAdmin,
		AccountID: t.OverlayAccountID,
		LastLogin: t.LastLogin,
		CreatedAt: t.CreatedAt,
	}
	if t.OverlayAccountID != "" {
		if acc, err := s.srv.GetAccount(t.OverlayAccountID); err == nil && acc != nil {
			sum.MaxPeers = acc.MaxPeers
		}
		if cur, _, err := s.srv.GetAccountPeerStats(t.OverlayAccountID); err == nil {
			sum.PeerCount = cur
		}
	}
	return sum
}
