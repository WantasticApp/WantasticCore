package adapter

import (
	"testing"
	"time"

	"WantasticCore/internal/account"
)

func TestAccountStoreMappingsPreserveMaxPeers(t *testing.T) {
	now := time.Now().UTC()
	domain := &account.Account{
		ID:         "acc-1",
		Name:       "demo",
		Networks:   []string{"10.0.0.0/27"},
		ServerIPs:  []string{"10.0.0.1"},
		BlockCount: 1,
		PrivateKey: "priv",
		MaxPeers:   42,
		CreatedAt:  now,
		UpdatedAt:  now,
	}

	storeModel := toStoreAccount(domain)
	if storeModel.MaxPeers != 42 {
		t.Fatalf("expected store model to keep max-peers cap, got %d", storeModel.MaxPeers)
	}

	roundTrip := toDomainAccount(storeModel)
	if roundTrip.MaxPeers != 42 {
		t.Fatalf("expected domain round-trip to keep max-peers cap, got %d", roundTrip.MaxPeers)
	}
}
