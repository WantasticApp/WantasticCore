package account

import (
	"time"
)

// Account represents a tenant account in the system.
type Account struct {
	ID         string    `json:"id"`
	Name       string    `json:"name"`
	Networks   []string  `json:"networks"`    // Multiple /27 blocks (e.g., ["10.0.0.0/27", "10.0.0.32/27"])
	ServerIPs  []string  `json:"server_ips"`  // Server IP for each block (e.g., ["10.0.0.1", "10.0.0.33"])
	BlockCount int       `json:"block_count"` // Number of /27 blocks allocated
	PrivateKey string    `json:"private_key"` // WireGuard private key (base64) - persisted to survive restarts
	MaxPeers   int       `json:"max_peers"`   // Maximum number of peers (devices) allowed for this account
	CreatedAt  time.Time `json:"created_at"`
	UpdatedAt  time.Time `json:"updated_at"`
}

// Store defines the interface for account storage operations.
type Store interface {
	CreateAccount(account *Account) error
	GetAccount(id string) (*Account, error)
	UpdateAccount(account *Account) error
	DeleteAccount(id string) error
	ListAccounts() ([]*Account, error)
}
