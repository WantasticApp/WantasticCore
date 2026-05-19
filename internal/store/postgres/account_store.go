package postgres

import (
	"fmt"

	"WantasticCore/internal/account"
	"github.com/go-pg/pg/v10"
)

// AccountStore implements account persistence in PostgreSQL.
type AccountStore struct {
	db *pg.DB
}

// NewAccountStore creates a new account store.
func NewAccountStore(db *pg.DB) *AccountStore {
	return &AccountStore{db: db}
}

func accountToModel(acc *account.Account) *Account {
	return &Account{
		ID:         acc.ID,
		Name:       acc.Name,
		Networks:   acc.Networks,
		ServerIPs:  acc.ServerIPs,
		BlockCount: acc.BlockCount,
		PrivateKey: acc.PrivateKey,
		MaxPeers:   acc.MaxPeers,
		CreatedAt:  acc.CreatedAt,
		UpdatedAt:  acc.UpdatedAt,
	}
}

func accountFromModel(m *Account) *account.Account {
	return &account.Account{
		ID:         m.ID,
		Name:       m.Name,
		Networks:   m.Networks,
		ServerIPs:  m.ServerIPs,
		BlockCount: m.BlockCount,
		PrivateKey: m.PrivateKey,
		MaxPeers:   m.MaxPeers,
		CreatedAt:  m.CreatedAt,
		UpdatedAt:  m.UpdatedAt,
	}
}

// CreateAccount stores a new account.
func (s *AccountStore) CreateAccount(acc *account.Account) error {
	pgAcc := accountToModel(acc)
	_, err := s.db.Model(pgAcc).Insert()
	if err != nil {
		return fmt.Errorf("failed to insert account: %w", err)
	}
	return nil
}

// GetAccount retrieves an account by ID.
func (s *AccountStore) GetAccount(id string) (*account.Account, error) {
	pgAcc := &Account{ID: id}
	err := s.db.Model(pgAcc).WherePK().Select()
	if err != nil {
		if err == pg.ErrNoRows {
			return nil, fmt.Errorf("account %s not found", id)
		}
		return nil, fmt.Errorf("failed to get account: %w", err)
	}
	return accountFromModel(pgAcc), nil
}

// UpdateAccount updates an existing account.
func (s *AccountStore) UpdateAccount(acc *account.Account) error {
	pgAcc := accountToModel(acc)
	_, err := s.db.Model(pgAcc).WherePK().Update()
	if err != nil {
		return fmt.Errorf("failed to update account: %w", err)
	}
	return nil
}

// DeleteAccount removes an account.
func (s *AccountStore) DeleteAccount(id string) error {
	pgAcc := &Account{ID: id}
	_, err := s.db.Model(pgAcc).WherePK().Delete()
	if err != nil {
		return fmt.Errorf("failed to delete account: %w", err)
	}
	return nil
}

// ListAccounts returns all accounts.
func (s *AccountStore) ListAccounts() ([]*account.Account, error) {
	var pgAccs []Account
	err := s.db.Model(&pgAccs).Select()
	if err != nil {
		return nil, fmt.Errorf("failed to list accounts: %w", err)
	}

	accs := make([]*account.Account, len(pgAccs))
	for i := range pgAccs {
		accs[i] = accountFromModel(&pgAccs[i])
	}
	return accs, nil
}
