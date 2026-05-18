package adapter

import (
	"WantasticCore/internal/account"
	"WantasticCore/internal/store"
)

// AccountStore adapts store.AccountRepository to account.Store interface.
type AccountStore struct {
	repo store.AccountRepository
}

// NewAccountStore creates a new account store adapter.
func NewAccountStore(repo store.AccountRepository) account.Store {
	return &AccountStore{repo: repo}
}

func toStoreAccount(a *account.Account) *store.AccountData {
	return &store.AccountData{
		ID:         a.ID,
		Name:       a.Name,
		Networks:   a.Networks,
		ServerIPs:  a.ServerIPs,
		BlockCount: a.BlockCount,
		PrivateKey: a.PrivateKey,
		MaxPeers:   a.MaxPeers,
		CreatedAt:  a.CreatedAt,
		UpdatedAt:  a.UpdatedAt,
	}
}

func toDomainAccount(d *store.AccountData) *account.Account {
	if d == nil {
		return nil
	}
	return &account.Account{
		ID:         d.ID,
		Name:       d.Name,
		Networks:   d.Networks,
		ServerIPs:  d.ServerIPs,
		BlockCount: d.BlockCount,
		PrivateKey: d.PrivateKey,
		MaxPeers:   d.MaxPeers,
		CreatedAt:  d.CreatedAt,
		UpdatedAt:  d.UpdatedAt,
	}
}

func (s *AccountStore) CreateAccount(acc *account.Account) error {
	return s.repo.Create(toStoreAccount(acc))
}

func (s *AccountStore) GetAccount(id string) (*account.Account, error) {
	d, err := s.repo.Get(id)
	if err != nil {
		return nil, err
	}
	return toDomainAccount(d), nil
}

func (s *AccountStore) UpdateAccount(acc *account.Account) error {
	return s.repo.Update(toStoreAccount(acc))
}

func (s *AccountStore) DeleteAccount(id string) error {
	return s.repo.Delete(id)
}

func (s *AccountStore) ListAccounts() ([]*account.Account, error) {
	list, err := s.repo.List()
	if err != nil {
		return nil, err
	}
	result := make([]*account.Account, len(list))
	for i, d := range list {
		result[i] = toDomainAccount(d)
	}
	return result, nil
}
