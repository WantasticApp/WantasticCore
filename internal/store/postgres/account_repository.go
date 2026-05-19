package postgres

import (
	"fmt"
	"time"

	"WantasticCore/internal/store"

	"github.com/go-pg/pg/v10"
)

// accountRepository implements store.AccountRepository using PostgreSQL.
type accountRepository struct {
	db *pg.DB
}

// NewAccountRepository creates a new PostgreSQL account repository.
func NewAccountRepository(db *pg.DB) store.AccountRepository {
	return &accountRepository{db: db}
}

func (r *accountRepository) Create(acc *store.AccountData) error {
	model := toAccountModel(acc)

	if model.CreatedAt.IsZero() {
		model.CreatedAt = time.Now()
	}
	if model.UpdatedAt.IsZero() {
		model.UpdatedAt = time.Now()
	}

	_, err := r.db.Model(model).Insert()
	if err != nil {
		return fmt.Errorf("failed to create account: %w", err)
	}
	return nil
}

func (r *accountRepository) Get(id string) (*store.AccountData, error) {
	model := &Account{ID: id}
	err := r.db.Model(model).WherePK().Select()
	if err != nil {
		if err == pg.ErrNoRows {
			return nil, fmt.Errorf("account not found: %s", id)
		}
		return nil, fmt.Errorf("failed to get account: %w", err)
	}
	return r.toData(model), nil
}

func (r *accountRepository) Update(acc *store.AccountData) error {
	model := toAccountModel(acc)
	model.UpdatedAt = time.Now()

	result, err := r.db.Model(model).WherePK().Update()
	if err != nil {
		return fmt.Errorf("failed to update account: %w", err)
	}
	if result.RowsAffected() == 0 {
		return fmt.Errorf("account not found: %s", acc.ID)
	}
	return nil
}

func toAccountModel(acc *store.AccountData) *Account {
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

func (r *accountRepository) Delete(id string) error {
	model := &Account{ID: id}
	result, err := r.db.Model(model).WherePK().Delete()
	if err != nil {
		return fmt.Errorf("failed to delete account: %w", err)
	}
	if result.RowsAffected() == 0 {
		return fmt.Errorf("account not found: %s", id)
	}
	return nil
}

func (r *accountRepository) List() ([]*store.AccountData, error) {
	var models []Account
	err := r.db.Model(&models).Order("created_at ASC").Select()
	if err != nil {
		return nil, fmt.Errorf("failed to list accounts: %w", err)
	}

	result := make([]*store.AccountData, len(models))
	for i := range models {
		result[i] = r.toData(&models[i])
	}
	return result, nil
}

func (r *accountRepository) toData(m *Account) *store.AccountData {
	return &store.AccountData{
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
