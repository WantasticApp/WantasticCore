// Package account provides account management with block-based IPAM allocation.
package account

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"net"
	"strings"
	"sync"
	"time"

	"WantasticCore/internal/state"
	"WantasticCore/internal/store"

	"WantasticCore/internal/wg/userspace/wireguard-go/wgctrl/wgtypes"
)

// DefaultMaxPeers is the default peer cap when none is specified on account creation.
const DefaultMaxPeers = 30

// Manager handles account lifecycle and resource allocation using block-based IPAM.
type Manager struct {
	store Store
	ipam  *state.GlobalIPAM // Block-based IPAM (/27 blocks)
	mu    sync.RWMutex
}

// NewManager creates a new account manager with block-based IPAM.
// subnetPoolCIDRs should be in format like ["10.0.0.0/8", "172.16.0.0/12"]
func NewManager(s Store, ipamRepo store.IPAMRepository, subnetPoolCIDRs []string) *Manager {
	// Initialize GlobalIPAM for block-based allocations
	ipam, err := state.NewGlobalIPAM(ipamRepo, subnetPoolCIDRs)
	if err != nil {
		panic(fmt.Sprintf("FATAL: Failed to initialize GlobalIPAM: %v", err))
	}

	m := &Manager{
		store: s,
		ipam:  ipam,
	}

	// Restore IPAM state from existing accounts
	if err := m.restoreIPAM(); err != nil {
		fmt.Printf("WARNING: Failed to restore IPAM state: %v\n", err)
	}

	return m
}

// GetStore returns the underlying AccountStore for ACL persistence.
// Used by group managers and other components that need direct store access.
func (m *Manager) GetStore() Store {
	return m.store
}

// restoreIPAM rebuilds GlobalIPAM state from existing accounts with Networks field
func (m *Manager) restoreIPAM() error {
	accounts, err := m.store.ListAccounts()
	if err != nil {
		return fmt.Errorf("failed to list accounts: %w", err)
	}

	if len(accounts) == 0 {
		return nil // No accounts to restore
	}

	restoredCount := 0
	for _, acc := range accounts {
		// Only restore accounts with Networks field
		if len(acc.Networks) > 0 {
			// Restore allocation in GlobalIPAM
			if err := m.ipam.RestoreAllocation(acc.ID, acc.Networks, acc.CreatedAt); err != nil {
				fmt.Printf("WARNING: Failed to restore IPAM for account %s: %v\n", acc.ID, err)
				continue
			}
			restoredCount++
		}
	}

	if restoredCount > 0 {
		fmt.Printf(" Restored %d block-based allocations in GlobalIPAM\n", restoredCount)
	}
	return nil
}

// blocksForPeers returns how many /27 blocks are needed to cover maxPeers devices.
// Each /27 block carries ~29 usable peer IPs (32 - network - broadcast - server).
func blocksForPeers(maxPeers int) int {
	if maxPeers <= 0 {
		return 1
	}
	const usablePerBlock = 29
	blocks := (maxPeers + usablePerBlock - 1) / usablePerBlock
	if blocks < 1 {
		blocks = 1
	}
	return blocks
}

// CreateAccount creates a new tenant account with block-based allocation.
// maxPeers controls the device cap; the IPAM allocates enough /27 blocks to cover it.
// Pass maxPeers <= 0 to use the default cap.
func (m *Manager) CreateAccount(name string, maxPeers int) (*Account, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	// Validate IPAM is initialized
	if m.ipam == nil {
		return nil, fmt.Errorf("IPAM not initialized")
	}

	if maxPeers <= 0 {
		maxPeers = DefaultMaxPeers
	}

	// Generate unique account ID
	id, err := generateID()
	if err != nil {
		return nil, fmt.Errorf("failed to generate account ID: %w", err)
	}

	// Generate WireGuard private key for this tenant
	privateKey, err := generatePrivateKey()
	if err != nil {
		return nil, fmt.Errorf("failed to generate private key: %w", err)
	}

	// Determine how many /27 blocks to allocate
	blockCount := blocksForPeers(maxPeers)

	// Allocate blocks from GlobalIPAM
	allocation, err := m.ipam.AllocateTenant(id, blockCount)
	if err != nil {
		return nil, fmt.Errorf("failed to allocate blocks from IPAM: %w", err)
	}

	// Convert networks to strings
	networks := make([]string, len(allocation.Networks))
	for i, net := range allocation.Networks {
		networks[i] = net.String()
	}

	// Convert server IPs to strings
	serverIPs := make([]string, len(allocation.ServerIPs))
	for i, ip := range allocation.ServerIPs {
		serverIPs[i] = ip.String()
	}

	// Create account with block-based fields
	account := &Account{
		ID:         id,
		Name:       name,
		Networks:   networks,
		ServerIPs:  serverIPs,
		BlockCount: allocation.BlockCount,
		PrivateKey: privateKey,
		MaxPeers:   maxPeers,
		CreatedAt:  allocation.AllocatedAt,
		UpdatedAt:  allocation.UpdatedAt,
	}

	// Store account
	if err := m.store.CreateAccount(account); err != nil {
		// Rollback IPAM allocation
		m.ipam.ReleaseTenant(id)
		return nil, fmt.Errorf("failed to store account: %w", err)
	}

	return account, nil
}

// GetAccount retrieves an account by ID.
func (m *Manager) GetAccount(id string) (*Account, error) {
	return m.store.GetAccount(id)
}

// SetMaxPeers updates the account's max-peer cap and ensures enough /27 blocks are
// allocated to cover it. Pass limit <= 0 to fall back to the default cap.
func (m *Manager) SetMaxPeers(id string, limit int) error {
	if limit < 0 {
		return fmt.Errorf("max peers must be >= 0")
	}
	m.mu.Lock()
	defer m.mu.Unlock()

	acc, err := m.store.GetAccount(id)
	if err != nil {
		return fmt.Errorf("account not found: %w", err)
	}

	if limit <= 0 {
		limit = DefaultMaxPeers
	}

	acc.MaxPeers = limit

	// Expand allocation if more blocks are needed.
	target := blocksForPeers(limit)
	if acc.BlockCount < target && m.ipam != nil {
		needed := target - acc.BlockCount
		if allocation, err := m.ipam.ExpandTenant(id, needed); err == nil {
			existing := make(map[string]bool, len(acc.Networks))
			for _, n := range acc.Networks {
				existing[n] = true
			}
			for i, network := range allocation.Networks {
				netStr := network.String()
				if existing[netStr] {
					continue
				}
				acc.Networks = append(acc.Networks, netStr)
				if i < len(allocation.ServerIPs) {
					acc.ServerIPs = append(acc.ServerIPs, allocation.ServerIPs[i].String())
				}
			}
			acc.BlockCount = len(acc.Networks)
		}
	}

	acc.UpdatedAt = time.Now()
	if err := m.store.UpdateAccount(acc); err != nil {
		return fmt.Errorf("update account: %w", err)
	}
	return nil
}

// DeleteAccount removes an account and releases its blocks.
func (m *Manager) DeleteAccount(id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	// Get account to validate it exists
	_, err := m.store.GetAccount(id)
	if err != nil {
		return err
	}

	// Delete account from store
	if err := m.store.DeleteAccount(id); err != nil {
		return err
	}

	// Release blocks back to IPAM
	if m.ipam != nil {
		if err := m.ipam.ReleaseTenant(id); err != nil {
			fmt.Printf("WARNING: Failed to release IPAM blocks for account %s: %v\n", id, err)
		}
	}

	return nil
}

// ListAccounts returns all accounts.
func (m *Manager) ListAccounts() ([]*Account, error) {
	return m.store.ListAccounts()
}

// AddBlockToAccount adds a new /27 block to an existing account.
func (m *Manager) AddBlockToAccount(id string) (*Account, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.ipam == nil {
		return nil, fmt.Errorf("IPAM not initialized")
	}

	// Get current account
	account, err := m.store.GetAccount(id)
	if err != nil {
		return nil, err
	}

	// Allocate one more block via expansion
	allocation, err := m.ipam.ExpandTenant(id, 1)
	if err != nil {
		return nil, fmt.Errorf("failed to allocate additional block: %w", err)
	}

	// Find new blocks (ones not already in account.Networks)
	existingBlocks := make(map[string]bool)
	for _, net := range account.Networks {
		existingBlocks[net] = true
	}

	var newBlocks []string
	var newServerIPs []string
	for i, net := range allocation.Networks {
		netStr := net.String()
		if !existingBlocks[netStr] {
			newBlocks = append(newBlocks, netStr)
			if i < len(allocation.ServerIPs) {
				newServerIPs = append(newServerIPs, allocation.ServerIPs[i].String())
			}
		}
	}

	if len(newBlocks) == 0 {
		return nil, fmt.Errorf("no new blocks allocated")
	}

	// Update account with new blocks
	account.Networks = append(account.Networks, newBlocks...)
	account.ServerIPs = append(account.ServerIPs, newServerIPs...)
	account.BlockCount = len(account.Networks)
	account.UpdatedAt = time.Now()

	// Persist the change
	if err := m.store.UpdateAccount(account); err != nil {
		return nil, fmt.Errorf("failed to update account: %w", err)
	}

	fmt.Printf(" Added %d block(s) to account %s (total: %d blocks)\n", len(newBlocks), id, account.BlockCount)
	return account, nil
}

// RemoveBlockFromAccount removes a /27 block from an existing account.
// Validates that at least one block remains.
func (m *Manager) RemoveBlockFromAccount(id string, blockCIDR string) (*Account, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.ipam == nil {
		return nil, fmt.Errorf("IPAM not initialized")
	}

	// Get current account
	account, err := m.store.GetAccount(id)
	if err != nil {
		return nil, err
	}

	// Validate account has this block
	hasBlock := false
	blockIndex := -1
	for i, net := range account.Networks {
		if net == blockCIDR {
			hasBlock = true
			blockIndex = i
			break
		}
	}

	if !hasBlock {
		return nil, fmt.Errorf("account does not have block %s", blockCIDR)
	}

	// Validate at least one block remains
	if len(account.Networks) <= 1 {
		return nil, fmt.Errorf("cannot remove last block from account")
	}

	// Update account - remove block and corresponding server IP
	account.Networks = append(account.Networks[:blockIndex], account.Networks[blockIndex+1:]...)
	if blockIndex < len(account.ServerIPs) {
		account.ServerIPs = append(account.ServerIPs[:blockIndex], account.ServerIPs[blockIndex+1:]...)
	}
	account.BlockCount = len(account.Networks)
	account.UpdatedAt = time.Now()

	// Persist the change
	if err := m.store.UpdateAccount(account); err != nil {
		return nil, fmt.Errorf("failed to update account: %w", err)
	}

	fmt.Printf(" Removed block from account %s: %s (remaining: %d blocks)\n", id, blockCIDR, account.BlockCount)
	return account, nil
}

// GetIPAMStatistics returns IPAM allocation statistics.
func (m *Manager) GetIPAMStatistics() *state.IPAMStatistics {
	if m.ipam == nil {
		return &state.IPAMStatistics{}
	}
	return m.ipam.GetStatistics()
}

// ValidatePeerIP checks if an IP is valid for peer assignment in an account.
// Returns error if:
// - IP is not within any of the account's Networks blocks
// - IP is a reserved server IP
// - IP is network or broadcast address
func (m *Manager) ValidatePeerIP(accountID, ipAddress string) error {
	// Get account
	account, err := m.store.GetAccount(accountID)
	if err != nil {
		return fmt.Errorf("account not found: %w", err)
	}

	if len(account.Networks) == 0 {
		return fmt.Errorf("account has no networks allocated")
	}

	// Strip /32 suffix if present
	ipAddr := strings.TrimSuffix(ipAddress, "/32")

	// Parse IP
	parsedIP := net.ParseIP(ipAddr)
	if parsedIP == nil {
		return fmt.Errorf("invalid IP address: %s", ipAddr)
	}

	// Check if IP is within any of the account's networks
	var foundInNetwork bool
	var matchedNetwork string
	for _, networkCIDR := range account.Networks {
		_, network, err := net.ParseCIDR(networkCIDR)
		if err != nil {
			continue
		}
		if network.Contains(parsedIP) {
			foundInNetwork = true
			matchedNetwork = networkCIDR
			break
		}
	}

	if !foundInNetwork {
		return fmt.Errorf("IP %s is not within any account network blocks: %v", ipAddr, account.Networks)
	}

	// Check if IP is a reserved server IP
	for _, serverIP := range account.ServerIPs {
		if serverIP == ipAddr {
			return fmt.Errorf("IP %s is reserved as server/gateway IP in block %s", ipAddr, matchedNetwork)
		}
	}

	// Parse the matched network to check for network/broadcast addresses
	_, network, _ := net.ParseCIDR(matchedNetwork)

	// Network address check (first IP in block)
	if parsedIP.Equal(network.IP) {
		return fmt.Errorf("IP %s is the network address of %s", ipAddr, matchedNetwork)
	}

	// Broadcast address check for /27 (last IP in block)
	ones, bits := network.Mask.Size()
	if ones == 27 && bits == 32 {
		// Calculate last IP in /27 block
		ip := network.IP.To4()
		lastIP := net.IPv4(ip[0], ip[1], ip[2], ip[3]|byte(31)) // OR with 0x1f for /27
		if parsedIP.Equal(lastIP) {
			return fmt.Errorf("IP %s is the broadcast address of %s", ipAddr, matchedNetwork)
		}
	}

	return nil
}

// generatePrivateKey generates a new WireGuard private key and returns it as base64 string.
func generatePrivateKey() (string, error) {
	key, err := wgtypes.GeneratePrivateKey()
	if err != nil {
		return "", fmt.Errorf("failed to generate private key: %w", err)
	}
	return key.String(), nil
}

// generateID creates a random account ID.
func generateID() (string, error) {
	bytes := make([]byte, 16)
	if _, err := rand.Read(bytes); err != nil {
		return "", err
	}
	return hex.EncodeToString(bytes), nil
}
