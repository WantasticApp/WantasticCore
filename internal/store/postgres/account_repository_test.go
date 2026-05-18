package postgres

import (
	"testing"
	"time"

	"WantasticCore/internal/store"
)

func TestAccountRepositoryMappingsPreserveMaxPeers(t *testing.T) {
	now := time.Now().UTC()
	data := &store.AccountData{
		ID:         "acc-1",
		Name:       "demo",
		Networks:   []string{"10.0.0.0/27"},
		ServerIPs:  []string{"10.0.0.1"},
		BlockCount: 1,
		PrivateKey: "priv",
		MaxPeers:   29,
		CreatedAt:  now,
		UpdatedAt:  now,
	}

	model := toAccountModel(data)
	if model.MaxPeers != 29 {
		t.Fatalf("expected model to keep max-peers cap, got %d", model.MaxPeers)
	}

	repo := &accountRepository{}
	roundTrip := repo.toData(model)
	if roundTrip.MaxPeers != 29 {
		t.Fatalf("expected repo round-trip to keep max-peers cap, got %d", roundTrip.MaxPeers)
	}
}
