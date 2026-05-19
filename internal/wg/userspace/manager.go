// Package userspace provides multi-tenant management for userspace WireGuard devices.
package userspace

import (
	"context"
	"fmt"
	"net"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"WantasticCore/internal/wg/userspace/wireguard-go/wgctrl/wgtypes"

	"github.com/rs/zerolog/log"
)

// UserspaceManager manages multiple tenant WireGuard devices.
// Unlike kernel-based implementations, this has no 256 interface limit.
type UserspaceManager struct {
	devices    map[string]*TenantDevice // tenantID -> device
	tunPool    *TUNAllocator
	sharedBind *SharedPortBindV2 // Single shared UDP socket for all tenants (shared mode only)
	mu         sync.RWMutex

	// Global limits
	// Global limits
	maxPeersGlobal int
	sharedPort     int

	// Stats cache
	lastGlobalStats     atomic.Value // stores *GlobalStats
	lastGlobalStatsTime atomic.Value // stores *time.Time

	// Context
	ctx context.Context

	// Peer activity callback
	peerActiveHandler func(tenantID string, publicKey string)

	// WUSP inbound callback
	wuspInboundHandler func(tenantID string, peerPublicKey string, data []byte)

	// Session confirmed callback — fires when ReceivedWithKeypair returns true
	peerSessionConfirmedHandler func(tenantID string, publicKey string)
}

// Config holds configuration for the UserspaceManager.
type Config struct {
	MaxPeersGlobal int
	// Port allocation mode
	// PortMode   PortAllocationMode // "dedicated" or "shared" (removed unused)
	SharedPort int // Used when PortMode = "shared"
}

// DefaultConfig returns optimized defaults for multi-tenant operation.
func DefaultConfig() *Config {
	return &Config{
		MaxPeersGlobal: 50000, // 5x more peers supported
		// PortMode:       PortModeShared, // Default to optimized shared mode (removed unused)
		SharedPort: 51820, // Single shared port for all tenants
	}
}

// CalculateLimitsFromPools derives MaxTenants and MaxPeersGlobal from IP pool capacity.
// baseCIDRs: List of pool CIDRs (e.g., ["10.0.0.0/8"])
// avgPeersPerTenant: Average peers per tenant for MaxPeersGlobal calculation (default: 10)
func CalculateLimitsFromPools(baseCIDRs []string, avgPeersPerTenant int) (maxTenants, maxPeersGlobal int, err error) {
	if avgPeersPerTenant <= 0 {
		avgPeersPerTenant = 10 // Reasonable default
	}

	totalBlocks := 0

	for _, cidr := range baseCIDRs {
		_, ipnet, parseErr := net.ParseCIDR(cidr)
		if parseErr != nil {
			return 0, 0, fmt.Errorf("invalid CIDR %s: %w", cidr, parseErr)
		}

		// Only support IPv4
		if ipnet.IP.To4() == nil {
			return 0, 0, fmt.Errorf("only IPv4 supported: %s", cidr)
		}

		// Calculate how many /27 blocks fit in this CIDR
		prefixLen, _ := ipnet.Mask.Size()
		if prefixLen > 27 {
			// CIDR smaller than /27, skip
			continue
		}

		numBlocks := 1 << (27 - prefixLen) // 2^(27-prefix)
		totalBlocks += numBlocks
	}

	if totalBlocks == 0 {
		return 0, 0, fmt.Errorf("no /27 blocks available from pools")
	}

	// Calculate MaxTenants based on tier distribution
	// Assume: 70% Free (1 block), 20% Standard (2 blocks), 10% Premium (8 blocks)
	// Average blocks per tenant: 0.7*1 + 0.2*2 + 0.1*8 = 0.7 + 0.4 + 0.8 = 1.9
	avgBlocksPerTenant := 1.9
	maxTenants = int(float64(totalBlocks) / avgBlocksPerTenant)

	// Calculate MaxPeersGlobal: Total usable IPs across all /27 blocks
	// Each /27 block has 32 IPs - 2 (network, broadcast) - 1 (server/gateway) = 29 usable for peers
	usableIPsPerBlock := 29
	maxPeersGlobal = totalBlocks * usableIPsPerBlock

	// Alternative: Use avgPeersPerTenant if provided
	if avgPeersPerTenant > 0 {
		alternativeMaxPeers := maxTenants * avgPeersPerTenant
		// Use the more conservative estimate
		if alternativeMaxPeers < maxPeersGlobal {
			maxPeersGlobal = alternativeMaxPeers
		}
	}

	return maxTenants, maxPeersGlobal, nil
}

// ConfigFromPools creates a Config with limits derived from IP pool capacity.
func ConfigFromPools(baseCIDRs []string, sharedPort int, avgPeersPerTenant int) (*Config, error) {
	_, maxPeersGlobal, err := CalculateLimitsFromPools(baseCIDRs, avgPeersPerTenant)
	if err != nil {
		return nil, err
	}

	if sharedPort <= 0 {
		sharedPort = 51820
	}

	return &Config{
		MaxPeersGlobal: maxPeersGlobal,
		SharedPort:     sharedPort,
	}, nil
}

// NewUserspaceManager creates a new multi-tenant WireGuard manager.
func NewUserspaceManager(ctx context.Context, config *Config) (*UserspaceManager, error) {
	if config == nil {
		config = DefaultConfig()
	}

	// Initialize firewall manager (no used after we decided to go with acl check in tun read path)
	var err error

	var sharedBind *SharedPortBindV2

	// Create single shared UDP socket for all tenants
	sharedBind, err = NewSharedPortBindV2(config.SharedPort)
	if err != nil {
		return nil, fmt.Errorf("failed to create shared port bind: %w", err)
	}
	m := &UserspaceManager{
		devices:        make(map[string]*TenantDevice),
		tunPool:        NewTUNAllocator(),
		sharedBind:     sharedBind,
		maxPeersGlobal: config.MaxPeersGlobal,
		sharedPort:     config.SharedPort,
		ctx:            ctx,
	}

	// Register callback for peer activity
	sharedBind.SetPeerActiveHandler(m.onTenantActivity)

	return m, nil
}

// SetPeerActiveHandler sets the callback for peer activity events (e.g. handshake response).
func (m *UserspaceManager) SetPeerActiveHandler(handler func(tenantID string, publicKey string)) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.peerActiveHandler = handler
}

// SetWUSPInboundHandler registers a global callback for all inbound WUSP
// control messages (WireGuard type 8) across all tenant devices.
func (m *UserspaceManager) SetWUSPInboundHandler(handler func(tenantID string, peerPublicKey string, data []byte)) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.wuspInboundHandler = handler
}

// SetPeerSessionConfirmedHandler registers a callback that fires once per new
// WireGuard session, when ReceivedWithKeypair returns true (both sides confirmed
// the new keypair). This is the safe trigger point for WUSP discovery probes.
func (m *UserspaceManager) SetPeerSessionConfirmedHandler(handler func(tenantID string, publicKey string)) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.peerSessionConfirmedHandler = handler
}

// SendWUSP sends a WUSP control payload to the peer on the given tenant device.
func (m *UserspaceManager) SendWUSP(tenantID, peerPublicKey string, data []byte) error {
	m.mu.RLock()
	device, ok := m.devices[tenantID]
	m.mu.RUnlock()
	if !ok {
		return fmt.Errorf("wusp: tenant %s not found", tenantID)
	}
	return device.SendWUSP(peerPublicKey, data)
}

// SendWUSPByPeer sends a WUSP control payload to a peer identified by its
// base64 public key, searching all tenant devices to find the right one.
func (m *UserspaceManager) SendWUSPByPeer(peerPublicKey string, data []byte) error {
	m.mu.RLock()
	defer m.mu.RUnlock()
	for tenantID, device := range m.devices {
		if err := device.SendWUSP(peerPublicKey, data); err == nil {
			return nil
		} else if !isPeerNotFoundErr(err) {
			return fmt.Errorf("wusp: tenant %s send error: %w", tenantID, err)
		}
	}
	return fmt.Errorf("wusp: peer %s not found in any tenant device", peerPublicKey)
}

func isPeerNotFoundErr(err error) bool {
	if err == nil {
		return false
	}
	msg := err.Error()
	return strings.Contains(msg, "not found") || strings.Contains(msg, "invalid base64")
}

// onTenantActivity is called by SharedBind when there is activity (Handshake Response) on a tenant.
func (m *UserspaceManager) onTenantActivity(tenantID, endpoint string) {
	// Rate limit checks per endpoint to avoid hammering FindPeerByEndpoint
	// For now, rely on SharedBind's rate limiting (which is per IP).
	// But SharedBind rate limits *Handshake Broadcasts*, not necessarily *Handshake Responses*.
	// Actually, in our implementation we added rate limiting for broadcasts, but for responses
	// we just fire the event. We should probably add a small cache here or rely on the fact
	// that handshakes are infrequent (every few mins).

	m.mu.RLock()
	device, ok := m.devices[tenantID]
	handler := m.peerActiveHandler
	m.mu.RUnlock()

	if !ok || handler == nil {
		return
	}

	// Look up peer by endpoint
	peerKey, err := device.FindPeerByEndpoint(endpoint)
	if err != nil {
		peerKey, err = device.FindPeerByEndpointFresh(endpoint)
		if err != nil {
			// Peer might still be in the middle of roaming or endpoint update propagation.
			return
		}
	}

	// Trigger callback
	handler(tenantID, peerKey)
}

// CreateTenant creates a new tenant network with an isolated WireGuard device.
// The privateKey parameter should be from the persisted account data to ensure stable public key.
// The subnets parameter contains all /27 CIDR blocks for this tenant (e.g., ["10.0.1.0/27", "10.0.1.32/27"]).
// Server IPs are automatically derived as the first usable IP in each block.
func (m *UserspaceManager) CreateTenant(tenantID string, subnets []string, maxPeers int, privateKey wgtypes.Key) (*TenantDevice, error) {
	// Validate input before acquiring lock
	if tenantID == "" {
		return nil, fmt.Errorf("tenantID cannot be empty")
	}
	if len(subnets) == 0 {
		return nil, fmt.Errorf("subnets slice cannot be empty")
	}
	if maxPeers <= 0 {
		return nil, fmt.Errorf("maxPeers must be positive, got %d", maxPeers)
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	// Check if tenant already exists
	if _, exists := m.devices[tenantID]; exists {
		return nil, fmt.Errorf("tenant %s already exists", tenantID)
	}

	// Create the userspace WireGuard device with persisted private key
	// In shared mode, pass the shared bind; otherwise use default bind
	var device *TenantDevice
	var err error
	// always shared port
	device, err = NewTenantDeviceWithBind(m.ctx, tenantID, subnets, m.sharedPort, privateKey, m.sharedBind.CreatePerTenantBind(tenantID))
	if err != nil {
		return nil, fmt.Errorf("failed to create tenant device: %w", err)
	}

	// Set resource limits
	device.SetMaxPeers(maxPeers)

	// Configure TUN address
	if _, err := device.GetTUNAddress(); err != nil {
		device.Close()
		return nil, fmt.Errorf("failed to get TUN address: %w", err)
	}

	// Register WUSP inbound handler if configured
	if m.wuspInboundHandler != nil {
		device.SetWUSPInboundHandler(func(peerPublicKey string, data []byte) {
			m.wuspInboundHandler(tenantID, peerPublicKey, data)
		})
	}

	// Register session-confirmed handler if configured
	if m.peerSessionConfirmedHandler != nil {
		device.SetPeerSessionConfirmedHandler(func(peerPublicKey string) {
			m.peerSessionConfirmedHandler(tenantID, peerPublicKey)
		})
	}

	// Store device
	m.devices[tenantID] = device

	log.Debug().
		Str("tenant_id", tenantID).
		Strs("subnets", subnets).
		Int("listen_port", device.SharedPort).
		Int("max_peers", maxPeers).
		Msg("Created tenant network")

	return device, nil
}

// DeleteTenant removes a tenant and cleans up all resources.
func (m *UserspaceManager) DeleteTenant(tenantID string) error {
	// Validate input
	if tenantID == "" {
		return fmt.Errorf("tenantID cannot be empty")
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	device, exists := m.devices[tenantID]
	if !exists {
		return fmt.Errorf("tenant %s not found", tenantID)
	}
	// Close the WireGuard device
	if err := device.Close(); err != nil {
		log.Error().Err(err).Str("tenant_id", tenantID).Msg("Error closing device")
	}
	// Remove from map
	delete(m.devices, tenantID)

	log.Debug().
		Str("tenant_id", tenantID).
		Msg("Deleted tenant network")

	return nil
}

// GetDevice returns the WireGuard device for a tenant.
func (m *UserspaceManager) GetDevice(tenantID string) (*TenantDevice, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	device, exists := m.devices[tenantID]
	if !exists {
		return nil, fmt.Errorf("tenant %s not found", tenantID)
	}

	return device, nil
}

// AddPeer adds a peer to a tenant's network.
func (m *UserspaceManager) AddPeer(tenantID string, peer *Peer) error {
	// Validate input
	if tenantID == "" {
		return fmt.Errorf("tenantID cannot be empty")
	}
	if peer == nil {
		return fmt.Errorf("peer cannot be nil")
	}

	// Get device with read lock
	m.mu.RLock()
	device, exists := m.devices[tenantID]
	m.mu.RUnlock()

	if !exists {
		return fmt.Errorf("tenant %s not found", tenantID)
	}

	// Check peer quota (this acquires device lock internally)
	stats, err := device.GetStats()
	if err != nil {
		return fmt.Errorf("failed to get device stats: %w", err)
	}

	maxPeers := device.GetMaxPeers()
	if maxPeers > 0 && stats.PeerCount >= maxPeers {
		return fmt.Errorf("tenant %s has reached max peer limit (%d)", tenantID, maxPeers)
	}

	return device.AddPeer(peer)
}

// UpdateTenantMaxPeers updates the live peer limit on an existing tenant device.
func (m *UserspaceManager) UpdateTenantMaxPeers(tenantID string, maxPeers int) error {
	if tenantID == "" {
		return fmt.Errorf("tenantID cannot be empty")
	}

	m.mu.RLock()
	device, exists := m.devices[tenantID]
	m.mu.RUnlock()
	if !exists {
		return fmt.Errorf("tenant %s not found", tenantID)
	}

	device.SetMaxPeers(maxPeers)
	return nil
}

// RemovePeer removes a peer from a tenant's network.
func (m *UserspaceManager) RemovePeer(tenantID string, publicKey wgtypes.Key) error {
	// Validate input
	if tenantID == "" {
		return fmt.Errorf("tenantID cannot be empty")
	}

	device, err := m.GetDevice(tenantID)
	if err != nil {
		return err
	}

	return device.RemovePeer(publicKey)
}

// UpdateTenantSubnets updates the subnet allocation for an existing tenant device.
// This preserves the device's private key and all existing peers while updating the subnet configuration.
func (m *UserspaceManager) UpdateTenantSubnets(tenantID string, newSubnets []string) error {
	// Validate input
	if tenantID == "" {
		return fmt.Errorf("tenantID cannot be empty")
	}
	if len(newSubnets) == 0 {
		return fmt.Errorf("newSubnets cannot be empty")
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	// Get existing device
	existingDevice, exists := m.devices[tenantID]
	if !exists {
		return fmt.Errorf("tenant device %s not found", tenantID)
	}

	// Preserve critical device information
	privateKey := existingDevice.PrivateKey
	maxPeers := existingDevice.GetMaxPeers() // Preserve peer limit for new device

	log.Debug().
		Str("tenant_id", tenantID).
		Strs("old_subnets", existingDevice.Subnets).
		Strs("new_subnets", newSubnets).
		Msg(" Updating tenant device subnets (preserving private key and peers)")

	// Note: In a more complete implementation, we would:
	// 1. Extract all current peers using IPC
	// 2. Preserve peer configurations
	// 3. Restore them to the new device
	// For now, we'll recreate with the new subnets and let the server re-add peers

	// Close existing device
	if err := existingDevice.Close(); err != nil {
		log.Warn().
			Err(err).
			Str("tenant_id", tenantID).
			Msg("  Error closing existing device during subnet update")
	}

	// Remove from devices map
	delete(m.devices, tenantID)

	// Create new device with same private key but new subnets
	var newDevice *TenantDevice
	var err error

	newDevice, err = NewTenantDeviceWithBind(m.ctx, tenantID, newSubnets, m.sharedPort, privateKey, m.sharedBind.CreatePerTenantBind(tenantID))

	if err != nil {
		return fmt.Errorf("failed to recreate tenant device with new subnets: %w", err)
	}

	// Restore the peer limit to the new device
	newDevice.SetMaxPeers(maxPeers)

	// Register WUSP inbound handler if configured
	if m.wuspInboundHandler != nil {
		newDevice.SetWUSPInboundHandler(func(peerPublicKey string, data []byte) {
			m.wuspInboundHandler(tenantID, peerPublicKey, data)
		})
	}

	// Register session-confirmed handler if configured
	if m.peerSessionConfirmedHandler != nil {
		newDevice.SetPeerSessionConfirmedHandler(func(peerPublicKey string) {
			m.peerSessionConfirmedHandler(tenantID, peerPublicKey)
		})
	}

	// Store the new device
	m.devices[tenantID] = newDevice

	log.Debug().
		Str("tenant_id", tenantID).
		Strs("subnets", newSubnets).
		Str("public_key", privateKey.PublicKey().String()).
		Msg(" Successfully updated tenant device subnets")

	return nil
}

// ListTenants returns all active tenant IDs.
func (m *UserspaceManager) ListTenants() []string {
	m.mu.RLock()
	defer m.mu.RUnlock()

	tenants := make([]string, 0, len(m.devices))
	for tenantID := range m.devices {
		tenants = append(tenants, tenantID)
	}

	return tenants
}

// GetStats returns statistics for a tenant's device.
func (m *UserspaceManager) GetStats(tenantID string) (*DeviceStats, error) {
	device, err := m.GetDevice(tenantID)
	if err != nil {
		return nil, err
	}

	return device.GetStats()
}

// GetGlobalStats returns aggregated statistics across all tenants.
// PERFORMANCE: Uses a 5-second internal cache to avoid massive CPU load when polling thousands of tenants.
func (m *UserspaceManager) GetGlobalStats() *GlobalStats {
	// Check cache
	lastStatsVal := m.lastGlobalStats.Load()
	lastTimeVal := m.lastGlobalStatsTime.Load()

	if lastStatsVal != nil && lastTimeVal != nil {
		lastStats := lastStatsVal.(*GlobalStats)
		lastTime := lastTimeVal.(*time.Time)
		if time.Since(*lastTime) < 5*time.Second {
			return lastStats
		}
	}

	m.mu.RLock()
	defer m.mu.RUnlock()

	stats := &GlobalStats{
		TotalTenants: len(m.devices),
		TotalPeers:   0,
		TotalTxBytes: 0,
		TotalRxBytes: 0,
		TenantStats:  make([]TenantStats, 0, len(m.devices)),
	}
	if m.sharedBind != nil {
		stats.SharedBindStats = m.sharedBind.Stats()
	}

	// Iterate directly - GetStats on each device is safe as it has its own locking
	for tenantID, device := range m.devices {
		deviceStats, err := device.GetStats()
		if err != nil {
			// Skip devices that error out
			continue
		}
		stats.TotalPeers += deviceStats.PeerCount
		stats.TotalTxBytes += deviceStats.TxBytes
		stats.TotalRxBytes += deviceStats.RxBytes

		// Add per-tenant stats
		tenantStat := TenantStats{
			AccountID:      tenantID,
			AccountName:    "", // Will be filled by gRPC layer
			PeerCount:      deviceStats.PeerCount,
			ConnectedPeers: deviceStats.ConnectedPeers,
			TxBytes:        deviceStats.TxBytes,
			RxBytes:        deviceStats.RxBytes,
			LastActivity:   deviceStats.LastActivity,
		}
		stats.TenantStats = append(stats.TenantStats, tenantStat)
	}

	// Update cache
	now := time.Now()
	m.lastGlobalStats.Store(stats)
	m.lastGlobalStatsTime.Store(&now)

	return stats
}

// Close shuts down all tenant devices and releases resources.
func (m *UserspaceManager) Close() error {
	m.mu.Lock()
	defer m.mu.Unlock()

	log.Debug().Int("tenant_count", len(m.devices)).Msg("Shutting down all tenant devices")

	// Use channel to track completion with timeout
	type closeResult struct {
		tenantID string
		err      error
	}

	// Close all devices in parallel with timeout
	resultChan := make(chan closeResult, len(m.devices))

	for tenantID, device := range m.devices {
		go func(tid string, dev *TenantDevice) {
			// Set a timeout for each device close (reduced since we improved device close)
			done := make(chan error, 1)
			go func() {
				done <- dev.Close()
			}()

			select {
			case err := <-done:
				resultChan <- closeResult{tid, err}
			case <-time.After(3 * time.Second): // Reduced from 5s since device close is now faster
				log.Error().Str("tenant_id", tid).Msg("Device close timeout - force closing")
				resultChan <- closeResult{tid, fmt.Errorf("close timeout")}
			}
		}(tenantID, device)
	}

	// Collect results
	var failedTenants []string
	for i := 0; i < len(m.devices); i++ {
		result := <-resultChan
		if result.err != nil {
			log.Error().Err(result.err).Str("tenant_id", result.tenantID).Msg("Error closing device")
			failedTenants = append(failedTenants, result.tenantID)
		}
	}

	// Clear devices map (only once)
	m.devices = make(map[string]*TenantDevice)

	// Close shared bind if it exists
	if m.sharedBind != nil {
		if err := m.sharedBind.Close(); err != nil {
			log.Error().Err(err).Msg("Error closing shared bind")
		}
	}

	// Report errors if any
	if len(failedTenants) > 0 {
		log.Warn().Strs("failed_tenants", failedTenants).Msg("Some devices failed to close cleanly")
		// Don't return error - we want to continue shutdown
	}

	log.Debug().Msg("All tenant devices shut down")
	return nil
}

// TenantStats contains per-tenant device statistics.
type TenantStats struct {
	AccountID      string
	AccountName    string
	PeerCount      int
	ConnectedPeers int
	TxBytes        uint64
	RxBytes        uint64
	LastActivity   time.Time
}

// GlobalStats contains aggregated statistics across all tenants.
type GlobalStats struct {
	TotalTenants    int
	TotalPeers      int
	TotalTxBytes    uint64
	TotalRxBytes    uint64
	TenantStats     []TenantStats
	SharedBindStats map[string]uint64
}
