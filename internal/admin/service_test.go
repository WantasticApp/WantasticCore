package admin

import (
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"WantasticCore/internal/account"
	"WantasticCore/internal/tenant"
)

// ─────────────────────────────────────────────────────────────────────────
// Test doubles
// ─────────────────────────────────────────────────────────────────────────

// fakeServer satisfies the admin.OverlayServer interface with an in-memory
// account store. Just enough to exercise the admin Service.
type fakeServer struct {
	mu       sync.Mutex
	accounts map[string]*account.Account
	created  []string
	deleted  []string
}

func newFakeServer() *fakeServer {
	return &fakeServer{accounts: map[string]*account.Account{}}
}

func (f *fakeServer) CreateAccount(name string, maxPeers int) (*account.Account, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	id := "acc-" + name
	acc := &account.Account{ID: id, Name: name, MaxPeers: maxPeers, Networks: []string{"10.0.0.0/27"}}
	f.accounts[id] = acc
	f.created = append(f.created, id)
	return acc, nil
}

func (f *fakeServer) DeleteAccount(id string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if _, ok := f.accounts[id]; !ok {
		return errors.New("not found")
	}
	delete(f.accounts, id)
	f.deleted = append(f.deleted, id)
	return nil
}

func (f *fakeServer) GetAccount(id string) (*account.Account, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	a, ok := f.accounts[id]
	if !ok {
		return nil, errors.New("not found")
	}
	return a, nil
}

func (f *fakeServer) SetAccountMaxPeers(id string, n int) (*account.Account, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	a, ok := f.accounts[id]
	if !ok {
		return nil, errors.New("not found")
	}
	a.MaxPeers = n
	return a, nil
}

func (f *fakeServer) GetAccountPeerStats(_ string) (current, max int, err error) {
	return 0, 0, nil
}

// fakeRegistry is a minimal in-memory tenant.Registry. It only implements
// the methods admin.Service actually calls; everything else panics so a
// missing call would surface loudly in tests.
type fakeRegistry struct {
	mu       sync.Mutex
	tenants  map[string]*tenant.Tenant
	byEmail  map[string]*tenant.Tenant
	cleared  []string
}

func newFakeRegistry() *fakeRegistry {
	return &fakeRegistry{
		tenants: map[string]*tenant.Tenant{},
		byEmail: map[string]*tenant.Tenant{},
	}
}

func (r *fakeRegistry) CreateTenant(t *tenant.Tenant) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, ok := r.byEmail[strings.ToLower(t.Email)]; ok {
		return errors.New("duplicate email")
	}
	r.tenants[t.ID] = t
	r.byEmail[strings.ToLower(t.Email)] = t
	return nil
}

func (r *fakeRegistry) GetTenant(id string) (*tenant.Tenant, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	t, ok := r.tenants[id]
	if !ok {
		return nil, errors.New("not found")
	}
	return t, nil
}

func (r *fakeRegistry) GetTenantByEmail(email string) (*tenant.Tenant, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	t, ok := r.byEmail[strings.ToLower(email)]
	if !ok {
		return nil, errors.New("not found")
	}
	return t, nil
}

func (r *fakeRegistry) GetTenantByOverlayAccount(string) (*tenant.Tenant, error) {
	return nil, errors.New("not implemented")
}

func (r *fakeRegistry) GetTenantByAuth0Sub(string) (*tenant.Tenant, error) {
	return nil, errors.New("not implemented")
}

func (r *fakeRegistry) UpdateTenant(t *tenant.Tenant) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.tenants[t.ID] = t
	return nil
}

func (r *fakeRegistry) DeleteTenant(id string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if t, ok := r.tenants[id]; ok {
		delete(r.byEmail, strings.ToLower(t.Email))
	}
	delete(r.tenants, id)
	return nil
}

func (r *fakeRegistry) ListTenants() ([]*tenant.Tenant, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]*tenant.Tenant, 0, len(r.tenants))
	for _, t := range r.tenants {
		out = append(out, t)
	}
	return out, nil
}

func (r *fakeRegistry) InvalidateAllSessions(id string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.cleared = append(r.cleared, id)
	return nil
}

func (r *fakeRegistry) UpdatePassword(id, hash string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if t, ok := r.tenants[id]; ok {
		t.PasswordHash = hash
	}
	return nil
}

func (r *fakeRegistry) SetTenantStatus(id, status string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if t, ok := r.tenants[id]; ok {
		t.Status = status
	}
	return nil
}

// Everything below is panic-stub so an accidental call fails loudly.
func (r *fakeRegistry) SetOverlayAccountID(string, string, []string) error { panic("unused in tests") }
func (r *fakeRegistry) UpdateLastLogin(string) error                       { panic("unused in tests") }
func (r *fakeRegistry) SetTwoFAMethod(string, string, string) error        { panic("unused in tests") }
func (r *fakeRegistry) GetActiveTwoFAMethod(string) (string, error)        { panic("unused in tests") }
func (r *fakeRegistry) IsTwoFAEnabled(string) (bool, error)                { panic("unused in tests") }
func (r *fakeRegistry) SetPending2FACode(string, string, time.Duration) error {
	panic("unused in tests")
}
func (r *fakeRegistry) Verify2FACode(string, string) (bool, bool, bool, error) {
	panic("unused in tests")
}
func (r *fakeRegistry) Clear2FACode(string) error               { panic("unused in tests") }
func (r *fakeRegistry) GetTwoFAInfo(string) (*tenant.TwoFAInfo, error) {
	panic("unused in tests")
}
func (r *fakeRegistry) CreateSession(string, string, string, string, string, time.Duration, bool) error {
	panic("unused in tests")
}
func (r *fakeRegistry) ValidateSession(string) (string, error) { panic("unused in tests") }
func (r *fakeRegistry) GetSession(string) (*tenant.TenantSession, error) {
	panic("unused in tests")
}
func (r *fakeRegistry) DeleteSession(string) error                       { panic("unused in tests") }
func (r *fakeRegistry) GetTenantSessions(string) ([]*tenant.TenantSession, error) {
	panic("unused in tests")
}
func (r *fakeRegistry) DeleteTenantSession(string, string) error { panic("unused in tests") }
func (r *fakeRegistry) UpdateSessionActivity(string) error       { panic("unused in tests") }
func (r *fakeRegistry) HasTrustedDevice(string, string) bool     { panic("unused in tests") }
func (r *fakeRegistry) CleanupExpiredSessions() (int, error)     { panic("unused in tests") }
func (r *fakeRegistry) CreateEnrollmentToken(*tenant.EnrollmentToken) error {
	panic("unused in tests")
}
func (r *fakeRegistry) GetEnrollmentToken(string) (*tenant.EnrollmentToken, error) {
	panic("unused in tests")
}
func (r *fakeRegistry) ListEnrollmentTokens(string) ([]*tenant.EnrollmentToken, error) {
	panic("unused in tests")
}
func (r *fakeRegistry) DeleteEnrollmentToken(string, string) error { panic("unused in tests") }
func (r *fakeRegistry) ValidateEnrollmentToken(string) (string, string, error) {
	panic("unused in tests")
}
func (r *fakeRegistry) IncrementEnrollmentTokenUsage(string) error { panic("unused in tests") }
func (r *fakeRegistry) CleanupExpiredEnrollmentTokens() (int, error) {
	panic("unused in tests")
}
func (r *fakeRegistry) CreateAccessShare(*tenant.AccessShare) error    { panic("unused in tests") }
func (r *fakeRegistry) GetAccessShare(string) (*tenant.AccessShare, error) {
	panic("unused in tests")
}
func (r *fakeRegistry) GetAccessShareByToken(string) (*tenant.AccessShare, error) {
	panic("unused in tests")
}
func (r *fakeRegistry) ListAccessSharesByOwner(string) ([]*tenant.AccessShare, error) {
	panic("unused in tests")
}
func (r *fakeRegistry) ListPendingSharesForEmail(string) ([]*tenant.AccessShare, error) {
	panic("unused in tests")
}
func (r *fakeRegistry) ListAcceptedSharesForTenant(string) ([]*tenant.AccessShare, error) {
	panic("unused in tests")
}
func (r *fakeRegistry) AcceptAccessShare(string, string) error { panic("unused in tests") }
func (r *fakeRegistry) RejectAccessShare(string) error         { panic("unused in tests") }
func (r *fakeRegistry) RevokeAccessShare(string, string) error { panic("unused in tests") }
func (r *fakeRegistry) UpdateShareResendTime(string) error     { panic("unused in tests") }
func (r *fakeRegistry) GetTeammateCount(string) (int, error)   { panic("unused in tests") }
func (r *fakeRegistry) InvalidateAllPasswordRecoveries(string) error { panic("unused in tests") }

// ─────────────────────────────────────────────────────────────────────────
// Tests
// ─────────────────────────────────────────────────────────────────────────

func newServiceForTest(t *testing.T) (*Service, *fakeServer, *fakeRegistry) {
	t.Helper()
	srv := newFakeServer()
	reg := newFakeRegistry()
	return New(srv, reg), srv, reg
}

func TestCreateTenant_HappyPath(t *testing.T) {
	svc, srv, reg := newServiceForTest(t)

	tnt, err := svc.CreateTenant(CreateTenantInput{
		Email:    "user@example.com",
		FullName: "Jane",
		Password: "secret",
		MaxPeers: 50,
	})
	if err != nil {
		t.Fatalf("CreateTenant: %v", err)
	}
	if tnt.IsAdmin {
		t.Error("expected non-admin")
	}
	if tnt.OverlayAccountID == "" {
		t.Error("expected an overlay account id")
	}
	if got, want := len(reg.tenants), 1; got != want {
		t.Errorf("registry tenant count = %d, want %d", got, want)
	}
	if got, want := len(srv.created), 1; got != want {
		t.Errorf("server created accounts = %d, want %d", got, want)
	}
}

func TestCreateTenant_RejectsDuplicateEmail(t *testing.T) {
	svc, _, _ := newServiceForTest(t)
	first := CreateTenantInput{Email: "dupe@example.com", FullName: "A", Password: "p", MaxPeers: 10}
	if _, err := svc.CreateTenant(first); err != nil {
		t.Fatalf("first create: %v", err)
	}
	if _, err := svc.CreateTenant(first); err == nil {
		t.Fatal("expected error on duplicate email")
	}
}

func TestDeleteTenant_RemovesAccountAndSessions(t *testing.T) {
	svc, srv, reg := newServiceForTest(t)
	tnt, _ := svc.CreateTenant(CreateTenantInput{Email: "del@example.com", FullName: "X", Password: "p", MaxPeers: 10})

	if err := svc.DeleteTenant(tnt.ID); err != nil {
		t.Fatalf("DeleteTenant: %v", err)
	}
	if len(reg.tenants) != 0 {
		t.Errorf("tenant not deleted from registry: %v", reg.tenants)
	}
	if got := len(srv.deleted); got != 1 {
		t.Errorf("expected 1 deleted account, got %d", got)
	}
	if len(reg.cleared) != 1 || reg.cleared[0] != tnt.ID {
		t.Errorf("sessions not invalidated for tenant %s: %v", tnt.ID, reg.cleared)
	}
}

func TestDeleteTenant_RefusesLastAdmin(t *testing.T) {
	svc, _, _ := newServiceForTest(t)
	a, _ := svc.CreateTenant(CreateTenantInput{Email: "a@x", FullName: "A", Password: "p", MaxPeers: 10, IsAdmin: true})

	err := svc.DeleteTenant(a.ID)
	if err == nil {
		t.Fatal("expected refusal when deleting the last admin")
	}
	if !strings.Contains(strings.ToLower(err.Error()), "last admin") {
		t.Errorf("unexpected error text: %v", err)
	}
}

func TestSetTenantAdmin_RefusesDemotingLastAdmin(t *testing.T) {
	svc, _, _ := newServiceForTest(t)
	a, _ := svc.CreateTenant(CreateTenantInput{Email: "a@x", FullName: "A", Password: "p", MaxPeers: 10, IsAdmin: true})

	if err := svc.SetTenantAdmin(a.ID, false); err == nil {
		t.Fatal("expected refusal when demoting the last admin")
	}
}

func TestSetTenantAdmin_AllowsWhenAnotherAdminExists(t *testing.T) {
	svc, _, _ := newServiceForTest(t)
	a, _ := svc.CreateTenant(CreateTenantInput{Email: "a@x", FullName: "A", Password: "p", MaxPeers: 10, IsAdmin: true})
	_, _ = svc.CreateTenant(CreateTenantInput{Email: "b@x", FullName: "B", Password: "p", MaxPeers: 10, IsAdmin: true})

	if err := svc.SetTenantAdmin(a.ID, false); err != nil {
		t.Fatalf("expected demotion to succeed: %v", err)
	}
}

func TestBootstrapAdmin_NoopWhenAdminExists(t *testing.T) {
	svc, srv, _ := newServiceForTest(t)
	_, _ = svc.CreateTenant(CreateTenantInput{Email: "first@x", FullName: "F", Password: "p", MaxPeers: 10, IsAdmin: true})

	beforeCreated := len(srv.created)
	if err := svc.BootstrapAdmin("other@x", "Other", "pw", 30); err != nil {
		t.Fatalf("BootstrapAdmin: %v", err)
	}
	if got, want := len(srv.created), beforeCreated; got != want {
		t.Errorf("BootstrapAdmin created a new account when an admin already existed: created=%d", got)
	}
}

func TestBootstrapAdmin_CreatesWhenNone(t *testing.T) {
	svc, srv, reg := newServiceForTest(t)
	if err := svc.BootstrapAdmin("admin@x", "Boot", "pw", 30); err != nil {
		t.Fatalf("BootstrapAdmin: %v", err)
	}
	if got := len(srv.created); got != 1 {
		t.Errorf("expected 1 account created, got %d", got)
	}
	if got := len(reg.tenants); got != 1 {
		t.Errorf("expected 1 tenant created, got %d", got)
	}
	for _, t2 := range reg.tenants {
		if !t2.IsAdmin {
			t.Error("bootstrap tenant should be admin")
		}
	}
}

func TestSetTenantMaxPeers(t *testing.T) {
	svc, srv, _ := newServiceForTest(t)
	tnt, _ := svc.CreateTenant(CreateTenantInput{Email: "x@x", FullName: "X", Password: "p", MaxPeers: 10})

	if err := svc.SetTenantMaxPeers(tnt.ID, 99); err != nil {
		t.Fatalf("SetTenantMaxPeers: %v", err)
	}
	if got := srv.accounts[tnt.OverlayAccountID].MaxPeers; got != 99 {
		t.Errorf("max_peers = %d, want 99", got)
	}
}

func TestAuthorize(t *testing.T) {
	svc, _, _ := newServiceForTest(t)
	admin, _ := svc.CreateTenant(CreateTenantInput{Email: "a@x", FullName: "A", Password: "p", MaxPeers: 10, IsAdmin: true})
	tnt, _ := svc.CreateTenant(CreateTenantInput{Email: "t@x", FullName: "T", Password: "p", MaxPeers: 10})

	if err := svc.Authorize(admin.ID); err != nil {
		t.Errorf("admin should be authorized: %v", err)
	}
	if err := svc.Authorize(tnt.ID); err == nil {
		t.Error("tenant should NOT be authorized")
	}
	if err := svc.Authorize(""); err == nil {
		t.Error("empty caller should be unauthenticated")
	}
}
