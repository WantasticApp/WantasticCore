package userspace

// Package userspace provides userspace WireGuard implementation for multi-tenant Wantastic.apps.
// Uses netstack for complete userspace networking - no kernel routing or iptables required!
// This eliminates the 256 interface limit and enables true per-tenant isolation.

import (
	"context"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"net"
	"net/netip"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"
	"unsafe"

	"WantasticCore/internal/wg/userspace/wireguard-go/conn"
	"WantasticCore/internal/wg/userspace/wireguard-go/device"
	"WantasticCore/internal/wg/userspace/wireguard-go/tun"
	"WantasticCore/internal/wg/userspace/wireguard-go/tun/netstack"
	"WantasticCore/internal/wg/userspace/wireguard-go/wgctrl/wgtypes"

	"gvisor.dev/gvisor/pkg/buffer"
	"gvisor.dev/gvisor/pkg/tcpip"
	"gvisor.dev/gvisor/pkg/tcpip/link/channel"
	"gvisor.dev/gvisor/pkg/tcpip/network/ipv4"
	"gvisor.dev/gvisor/pkg/tcpip/network/ipv6"
	"gvisor.dev/gvisor/pkg/tcpip/stack"

	"github.com/rs/zerolog/log"
)

// TenantDevice represents an isolated userspace WireGuard device for a single tenant.
// Uses netstack for complete TCP/IP stack with userspace WireGuard (no kernel dependencies).
// LOCK-FREE DESIGN: Uses atomic operations and immutable state for thread safety.
type TenantDevice struct {
	TenantID string
	Device   *device.Device // Read-only after initialization
	Net      *netstack.Net  // Read-only after initialization
	UDPBind  conn.Bind      // Read-only after initialization
	Logger   *device.Logger // Read-only after initialization
	DeviceIP netip.Addr     // Read-only after initialization

	// Configuration (immutable after creation)
	Subnets    []string
	ListenPort int // Actual port this device is bound to
	SharedPort int // The shared port (only set in shared mode)
	PrivateKey wgtypes.Key
	PublicKey  wgtypes.Key

	// Resource limits (immutable after creation)
	maxPeers atomic.Int64

	// IPC result caching for performance (atomic access)
	lastIPCResult   atomic.Value  // stores string
	lastIPCSnapshot atomic.Value  // stores *ipcSnapshot
	lastIPCTime     atomic.Value  // stores time.Time
	ipcCacheTTL     time.Duration // Default 1 second

	// Control (immutable after creation)
	ctx    context.Context
	cancel context.CancelFunc

	// State management (atomic)
	closed atomic.Bool
}

// GetEndpointPort returns the port that should be used in peer configs.
// In shared mode, this is the shared port; in dedicated mode, it's the device's listen port.
func (td *TenantDevice) GetEndpointPort() int {
	return td.SharedPort
}

// GetMaxPeers returns the current peer limit enforced by this tenant device.
func (td *TenantDevice) GetMaxPeers() int {
	return int(td.maxPeers.Load())
}

// SetMaxPeers updates the peer limit enforced by this tenant device.
func (td *TenantDevice) SetMaxPeers(maxPeers int) {
	if maxPeers < 0 {
		maxPeers = 0
	}
	td.maxPeers.Store(int64(maxPeers))
}

// khasna ndiro had lablan a si AI
func (td *TenantDevice) SetPeerAnnounceHandler(handler func(*device.NoisePublicKey)) {
	td.Device.SetPeerAnnounceHandler(handler)
}

// NewTenantDeviceWithBind creates a new userspace WireGuard device with a custom bind.
// This is used in shared port mode with the SharedPortBind.
func NewTenantDeviceWithBind(ctx context.Context, tenantID string, subnets []string, port int, privateKey wgtypes.Key, udpBind conn.Bind) (*TenantDevice, error) {
	serverAddrs, err := serverAddrsFromSubnets(subnets)
	if err != nil {
		return nil, err
	}
	serverAddr := serverAddrs[0]

	// Create netstack TUN device with one local router IP per allocated subnet
	// block. This is essential for multi-block tenants and keeps the primary
	// router address on the first usable IP rather than the raw network address.
	tunDev, tnet, err := netstack.CreateNetTUN(
		serverAddrs,
		[]netip.Addr{}, // DNS
		device.DefaultMTU,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create netstack TUN: %w", err)
	}

	// Configure netstack with subnet route and IP forwarding
	netTun := (*netstack.Net)(tnet)
	if err := enableIPForwarding(netTun, subnets, serverAddr); err != nil {
		tunDev.Close()
		return nil, fmt.Errorf("failed to configure netstack: %w", err)
	}

	// Setup custom logger to filter noise (passive server mode)
	logger := &device.Logger{
		Verbosef: func(format string, args ...any) {
			log.Debug().Msgf("[tenant:%s] %s", tenantID, fmt.Sprintf(format, args...))
		},
		Errorf: func(format string, args ...any) {
			// Skip "no known endpoint" errors for offline peers to reduce log noise
			msg := fmt.Sprintf(format, args...)
			if strings.Contains(strings.ToLower(msg), "no known endpoint") ||
				strings.Contains(strings.ToLower(msg), "no endpoint") {
				return
			}
			log.Error().Msgf("[tenant:%s] %s", tenantID, msg)
		},
	}

	// Create WireGuard device
	wgDevice := device.NewDevice(tunDev, udpBind, logger)
	wgDevice.EnableCoordinator() // Explicitly enable P2P coordination

	// Create device context
	deviceCtx, cancel := context.WithCancel(ctx)

	td := &TenantDevice{
		TenantID:    tenantID,
		Device:      wgDevice,
		Net:         tnet,
		UDPBind:     udpBind,
		Logger:      logger,
		DeviceIP:    serverAddr,
		Subnets:     subnets,
		ListenPort:  port,
		SharedPort:  port,
		PrivateKey:  privateKey,
		PublicKey:   privateKey.PublicKey(),
		ipcCacheTTL: 10 * time.Second, // 10-second cache for IPC results
		ctx:         deviceCtx,
		cancel:      cancel,
	}

	// Configure the device
	if err := td.configure(); err != nil {
		wgDevice.Close()
		tunDev.Close()
		return nil, fmt.Errorf("failed to configure device: %w", err)
	}

	// Start the device
	wgDevice.Up()

	log.Debug().
		Str("tenant_id", tenantID).
		Strs("subnets", subnets).
		Str("server_ip", serverAddr.String()).
		Int("listen_port", port).
		Str("public_key", td.PublicKey.String()).
		Msg(" Created tenant device with SharedPortBind (single socket)")

	return td, nil
}

// configure applies initial configuration to the WireGuard device.
func (td *TenantDevice) configure() error {
	// Encode private key as hex for IPC (wireguard-go expects hex, not base64)
	privateKeyHex := hex.EncodeToString(td.PrivateKey[:])

	// Create IPC configuration
	var cfg string
	cfg = fmt.Sprintf("private_key=%s\n", privateKeyHex)

	// Apply configuration via IPC
	if err := td.Device.IpcSet(cfg); err != nil {
		return fmt.Errorf("failed to configure device: %w", err)
	}

	return nil
}

func serverAddrsFromSubnets(subnets []string) ([]netip.Addr, error) {
	if len(subnets) == 0 {
		return nil, fmt.Errorf("at least one tenant subnet is required")
	}

	addrs := make([]netip.Addr, 0, len(subnets))
	for _, subnet := range subnets {
		_, subnetNet, err := net.ParseCIDR(subnet)
		if err != nil {
			return nil, fmt.Errorf("invalid subnet %s: %w", subnet, err)
		}

		serverAddr, err := firstUsableAddr(subnetNet)
		if err != nil {
			return nil, fmt.Errorf("failed to derive server IP for %s: %w", subnet, err)
		}
		addrs = append(addrs, serverAddr)
	}

	return addrs, nil
}

func firstUsableAddr(subnetNet *net.IPNet) (netip.Addr, error) {
	if subnetNet == nil {
		return netip.Addr{}, fmt.Errorf("subnet is nil")
	}

	ip := make(net.IP, len(subnetNet.IP))
	copy(ip, subnetNet.IP)
	if ipv4 := ip.To4(); ipv4 != nil {
		ip = ipv4
	}

	for i := len(ip) - 1; i >= 0; i-- {
		ip[i]++
		if ip[i] != 0 {
			break
		}
	}

	addr, ok := netip.AddrFromSlice(ip)
	if !ok {
		return netip.Addr{}, fmt.Errorf("invalid IP %v", ip)
	}
	return addr, nil
}

// configureInterface is NO LONGER NEEDED with netstack!
// netstack.CreateNetTUN handles IP configuration internally in the userspace network stack.
// The IP address is managed entirely in userspace - no kernel netlink operations required.
// This is the key advantage: no iptables, no routing tables, no kernel dependencies!
//
// SetPeerSessionConfirmedHandler registers a callback that fires once per new
// WireGuard session when both sides have confirmed the new keypair
// (ReceivedWithKeypair returned true). The handler receives the peer's base64
// public key. This is the correct point to trigger WUSP probes because
// keypairs.Current() is now the freshly-confirmed session key on both sides.
func (td *TenantDevice) SetPeerSessionConfirmedHandler(handler func(peerPublicKey string)) {
	td.Device.SetPeerSessionConfirmedHandler(func(peer *device.Peer) {
		pk := peer.PublicKey()
		key := base64.StdEncoding.EncodeToString(pk[:])
		handler(key)
	})
}

// SetWUSPInboundHandler registers a callback for incoming WUSP control messages
// (WireGuard message type 8). The handler receives the peer's base64 public key
// and the raw WUSP fragment payload.
func (td *TenantDevice) SetWUSPInboundHandler(handler func(peerPublicKey string, data []byte)) {
	td.Device.SetWUSPHandler(func(peer *device.Peer, data []byte) {
		pk := peer.PublicKey()
		key := base64.StdEncoding.EncodeToString(pk[:])
		handler(key, data)
	})
}

// SendWUSP sends a raw WUSP fragment to the peer identified by its base64 public key.
// The caller is responsible for fragmenting payloads larger than WUSPMaxDatagramPayload.
func (td *TenantDevice) SendWUSP(peerPublicKey string, data []byte) error {
	rawKey, err := base64.StdEncoding.DecodeString(peerPublicKey)
	if err != nil {
		return fmt.Errorf("SendWUSP: invalid base64 public key: %w", err)
	}
	if len(rawKey) != 32 {
		return fmt.Errorf("SendWUSP: public key must be 32 bytes, got %d", len(rawKey))
	}
	var noiseKey device.NoisePublicKey
	copy(noiseKey[:], rawKey)
	peer := td.Device.LookupPeer(noiseKey)
	if peer == nil {
		return fmt.Errorf("SendWUSP: peer %s not found", peerPublicKey)
	}
	if err := peer.SendWUSP(data); err != nil {
		return fmt.Errorf("SendWUSP: enqueue failed for peer %s: %w", peerPublicKey, err)
	}
	return nil
}

// RestorePeer loads an existing peer from the database back into the WireGuard
// device on startup. It intentionally skips the MaxPeers cap because the peer
// already exists in persistent storage — we are rehydrating state, not creating
// something new. Limit enforcement only applies to new peer creation (AddPeer).
func (td *TenantDevice) RestorePeer(peer *Peer) error {
	if td.closed.Load() {
		return fmt.Errorf("device is closed")
	}
	return td.applyPeerIPC(peer)
}

// AddPeer adds a new peer to this tenant's WireGuard device.
// LOCK-FREE: Uses atomic operations for state checking.
func (td *TenantDevice) AddPeer(peer *Peer) error {
	// Check if device is closed
	if td.closed.Load() {
		return fmt.Errorf("device is closed")
	}

	// Defense-in-depth: Check peer limit at device level
	maxPeers := td.GetMaxPeers()
	if maxPeers > 0 {
		currentCount, err := td.getCurrentPeerCount()
		if err != nil {
			log.Warn().Err(err).Str("tenant_id", td.TenantID).Msg("Failed to get current peer count for validation")
			// Continue anyway - manager-level validation is primary defense
		} else if currentCount >= maxPeers {
			return fmt.Errorf("tenant %s peer limit exceeded: %d/%d (device-level check)",
				td.TenantID, currentCount, maxPeers)
		}
	}

	return td.applyPeerIPC(peer)
}

// applyPeerIPC writes a peer configuration to the WireGuard device via IPC.
// This is the shared implementation used by both AddPeer and RestorePeer.
func (td *TenantDevice) applyPeerIPC(peer *Peer) error {
	// Encode public key as hex for IPC
	publicKeyHex := hex.EncodeToString(peer.PublicKey[:])

	// Build IPC configuration for peer
	var cfg strings.Builder
	fmt.Fprintf(&cfg, "public_key=%s\n", publicKeyHex)
	// Re-application paths provide the full desired route set for the peer, so
	// we must replace any older AllowedIPs instead of accumulating stale routes.
	fmt.Fprintf(&cfg, "replace_allowed_ips=true\n")

	// Add allowed IPs (each on separate line)
	for _, allowedIP := range peer.AllowedIPs {
		fmt.Fprintf(&cfg, "allowed_ip=%s\n", allowedIP)
	}

	// Add endpoint if provided
	if peer.Endpoint != "" {
		fmt.Fprintf(&cfg, "endpoint=%s\n", peer.Endpoint)
	}

	// Add persistent keepalive
	// Always set it (even to 0) to ensure we override any previous value in the device
	fmt.Fprintf(&cfg, "persistent_keepalive_interval=%d\n", peer.PersistentKeepalive)

	// Apply peer configuration
	if err := td.Device.IpcSet(cfg.String()); err != nil {
		return fmt.Errorf("failed to add peer: %w", err)
	}

	// Invalidate IPC cache since device state changed
	td.invalidateIPCCache()

	log.Debug().
		Str("tenant_id", td.TenantID).
		Str("peer_pubkey", peer.PublicKey.String()).
		Strs("allowed_ips", peer.AllowedIPs).
		Msg("Added peer to tenant device")

	return nil
}

// RemovePeer removes a peer from the device.
// LOCK-FREE: Uses atomic operations for state checking.
func (td *TenantDevice) RemovePeer(publicKey wgtypes.Key) error {
	// Check if device is closed
	if td.closed.Load() {
		return fmt.Errorf("device is closed")
	}

	// Encode public key as hex for IPC (same as AddPeer)
	publicKeyHex := hex.EncodeToString(publicKey[:])
	cfg := fmt.Sprintf("public_key=%s\nremove=true\n", publicKeyHex)

	if err := td.Device.IpcSet(cfg); err != nil {
		return fmt.Errorf("failed to remove peer: %w", err)
	}

	// Invalidate IPC cache since device state changed
	td.invalidateIPCCache()

	log.Debug().
		Str("tenant_id", td.TenantID).
		Str("peer_pubkey", publicKey.String()).
		Msg("Removed peer from tenant device")

	return nil
}

// GetStats returns current device statistics.
func (td *TenantDevice) GetStats() (*DeviceStats, error) {
	snapshot, err := td.getIPCSnapshotCached()
	if err != nil {
		return nil, fmt.Errorf("failed to get device status: %w", err)
	}

	// Read device info (lock-free)
	stats := &DeviceStats{
		TenantID:       td.TenantID,
		ListenPort:     td.ListenPort,
		EndpointPort:   td.GetEndpointPort(), // Use the correct port for peer configs
		PublicKey:      td.PublicKey.String(),
		PeerCount:      snapshot.peerCount,
		ConnectedPeers: snapshot.connectedPeers,
		TxBytes:        snapshot.totalTxBytes,
		RxBytes:        snapshot.totalRxBytes,
		LastActivity:   snapshot.lastActivity,
	}

	return stats, nil
}

// getIPCSnapshotCached returns a parsed snapshot of IPC output if fresh, otherwise fetches and parses new state.
func (td *TenantDevice) getIPCSnapshotCached() (*ipcSnapshot, error) {
	lastTime := td.lastIPCTime.Load()
	lastSnapshot := td.lastIPCSnapshot.Load()

	if lastTime != nil && lastSnapshot != nil {
		if time.Since(lastTime.(time.Time)) < td.ipcCacheTTL {
			return lastSnapshot.(*ipcSnapshot), nil
		}
	}

	ipcResult, err := td.Device.IpcGet()
	if err != nil {
		return nil, err
	}

	now := time.Now()
	snapshot := parseIPCSnapshot(ipcResult, now)
	td.lastIPCResult.Store(ipcResult)
	td.lastIPCSnapshot.Store(snapshot)
	td.lastIPCTime.Store(now)

	return snapshot, nil
}

func (td *TenantDevice) getIPCSnapshotFresh() (*ipcSnapshot, error) {
	ipcResult, err := td.Device.IpcGet()
	if err != nil {
		return nil, err
	}

	now := time.Now()
	snapshot := parseIPCSnapshot(ipcResult, now)
	td.lastIPCResult.Store(ipcResult)
	td.lastIPCSnapshot.Store(snapshot)
	td.lastIPCTime.Store(now)

	return snapshot, nil
}

// getIPCResultCached returns cached IPC result if fresh, otherwise fetches new one.
// This method is lock-free and thread-safe using atomic operations.
func (td *TenantDevice) getIPCResultCached() (string, error) {
	snapshot, err := td.getIPCSnapshotCached()
	if err != nil {
		return "", err
	}
	return snapshot.raw, nil
}

// invalidateIPCCache clears the IPC result cache.
// Called when device configuration changes (add/remove peer).
func (td *TenantDevice) invalidateIPCCache() {
	td.lastIPCResult.Store("")
	td.lastIPCTime.Store(time.Time{})
}

// getCurrentPeerCount returns the current number of peers without caching.
// Used for defense-in-depth validation. Caller must hold write lock.
func (td *TenantDevice) getCurrentPeerCount() (int, error) {
	// Force fresh IPC fetch (bypass cache)
	ipcResult, err := td.Device.IpcGet()
	if err != nil {
		return 0, fmt.Errorf("failed to get device status: %w", err)
	}

	// Count peers by counting "public_key=" lines in IPC output (streaming parser)
	peerCount := 0
	start := 0
	for i := 0; i <= len(ipcResult); i++ {
		if i == len(ipcResult) || ipcResult[i] == '\n' {
			line := ipcResult[start:i]
			start = i + 1
			if strings.HasPrefix(line, "public_key=") {
				peerCount++
			}
		}
	}

	return peerCount, nil
}

// PeerStatus contains runtime status for a specific peer.
type PeerStatus struct {
	PublicKey                   string
	LastHandshakeTime           time.Time
	LastAuthenticatedPacketTime time.Time
	RxBytes                     int64
	TxBytes                     int64
	IsOnline                    bool   // True if authenticated traffic was seen recently
	HasEndpoint                 bool   // True if peer has a discovered endpoint (connected at least once)
	Endpoint                    string // The peer's public IP and port (e.g., "1.2.3.4:5678")
	AssignedIP                  string // The internal IP assigned to the peer
	AllowedIPs                  []string
}

// GetPeerStatus retrieves runtime status for a specific peer from the WireGuard device.
// This checks the actual device state via IPC to determine if the peer is online.
// OPTIMIZED: Uses a cached immutable IPC snapshot shared across status readers.
func (td *TenantDevice) GetPeerStatus(publicKey string) (*PeerStatus, error) {
	if _, err := base64.StdEncoding.DecodeString(publicKey); err != nil {
		return nil, fmt.Errorf("invalid base64 public key: %w", err)
	}

	snapshot, err := td.getIPCSnapshotCached()
	if err != nil {
		return nil, fmt.Errorf("failed to get device status: %w", err)
	}

	status, ok := snapshot.peers[publicKey]
	if !ok {
		return nil, fmt.Errorf("peer %s not found in device", publicKey)
	}

	return copyPeerStatus(status), nil
}

// GetPeerStatusFresh retrieves runtime status for a specific peer using a
// fresh IPC read. Use this on latency-sensitive presence paths where a stale
// cache can incorrectly flip a peer back to offline immediately after a
// successful handshake.
func (td *TenantDevice) GetPeerStatusFresh(publicKey string) (*PeerStatus, error) {
	if _, err := base64.StdEncoding.DecodeString(publicKey); err != nil {
		return nil, fmt.Errorf("invalid base64 public key: %w", err)
	}

	snapshot, err := td.getIPCSnapshotFresh()
	if err != nil {
		return nil, fmt.Errorf("failed to get fresh device status: %w", err)
	}

	status, ok := snapshot.peers[publicKey]
	if !ok {
		return nil, fmt.Errorf("peer %s not found in device", publicKey)
	}

	return copyPeerStatus(status), nil
}

// ListPeerPublicKeys returns the public keys currently configured on the device
// from a fresh IPC snapshot. Higher-level sync logic uses this to prune stale
// peers that were deleted from persistent storage.
func (td *TenantDevice) ListPeerPublicKeys() ([]string, error) {
	snapshot, err := td.getIPCSnapshotFresh()
	if err != nil {
		return nil, fmt.Errorf("failed to get fresh device status: %w", err)
	}

	keys := make([]string, 0, len(snapshot.peers))
	for publicKey := range snapshot.peers {
		keys = append(keys, publicKey)
	}
	sort.Strings(keys)
	return keys, nil
}

// FindPeerByEndpoint finds a peer's public key by its current endpoint address.
// This allows SharedBind to map an incoming packet source to a specific peer.
func (td *TenantDevice) FindPeerByEndpoint(endpoint string) (string, error) {
	snapshot, err := td.getIPCSnapshotCached()
	if err != nil {
		return "", fmt.Errorf("failed to get device status: %w", err)
	}

	if publicKey, ok := snapshot.endpointToPeer[endpoint]; ok {
		return publicKey, nil
	}

	return "", fmt.Errorf("peer with endpoint %s not found", endpoint)
}

func (td *TenantDevice) FindPeerByEndpointFresh(endpoint string) (string, error) {
	snapshot, err := td.getIPCSnapshotFresh()
	if err != nil {
		return "", fmt.Errorf("failed to get fresh device status: %w", err)
	}

	if publicKey, ok := snapshot.endpointToPeer[endpoint]; ok {
		return publicKey, nil
	}

	return "", fmt.Errorf("peer with endpoint %s not found", endpoint)
}

// ConfigureTUNAddress configures the IP address on the TUN device.
// This requires netlink access but can be done without NET_ADMIN if the TUN
// device was pre-configured by a privileged helper.
func (td *TenantDevice) ConfigureTUNAddress() error {
	// Parse subnet to get the server IP (first usable IP)
	_, ipNet, err := net.ParseCIDR(td.Subnets[0])
	if err != nil {
		return fmt.Errorf("invalid subnet %s: %w", td.Subnets[0], err)
	}

	// The TUN device should be configured with the server IP
	// This is typically done using netlink or ip command
	// For now, we assume it's pre-configured or done by a helper

	// tunName, _ := td.TUN.Name()
	log.Debug().
		Str("tenant_id", td.TenantID).
		Strs("subnets", td.Subnets).
		// Str("tun_name", tunName).
		Msg("TUN device configured")

	_ = ipNet // Use this to configure the device
	return nil
}

// Close gracefully shuts down the WireGuard device and releases resources.
// LOCK-FREE: Uses atomic operations for state management.
func (td *TenantDevice) Close() error {
	log.Debug().Str("tenant_id", td.TenantID).Msg("Starting lock-free device shutdown")

	// Check if already closed (atomic)
	if !td.closed.CompareAndSwap(false, true) {
		log.Debug().Str("tenant_id", td.TenantID).Msg("Device already closed")
		return nil
	}

	log.Debug().
		Str("tenant_id", td.TenantID).
		Msg("Shutting down userspace WireGuard device")

	// Cancel context first to signal all goroutines to stop
	if td.cancel != nil {
		log.Debug().Str("tenant_id", td.TenantID).Msg("Cancelling device context")
		td.cancel()
	}

	// Close UDP bind FIRST to stop network I/O immediately
	if perTenantBind, ok := td.UDPBind.(*perTenantBindV2); ok {
		if err := perTenantBind.Close(); err != nil {
			log.Warn().Err(err).Str("tenant_id", td.TenantID).Msg("Failed to close per-tenant bind")
		}
	}

	// Close WireGuard device with improved timeout handling
	if td.Device != nil {
		log.Debug().Str("tenant_id", td.TenantID).Msg("Starting WireGuard device close")

		// Use a timeout for WireGuard device close
		done := make(chan struct{})
		go func() {
			defer func() {
				if r := recover(); r != nil {
					log.Error().Str("tenant_id", td.TenantID).Interface("panic", r).Msg("Panic in device close")
				}
				log.Debug().Str("tenant_id", td.TenantID).Msg("Device close goroutine finished")
				close(done)
			}()

			log.Debug().Str("tenant_id", td.TenantID).Msg("Calling td.Device.Close()")
			td.Device.Close()
			log.Debug().Str("tenant_id", td.TenantID).Msg("td.Device.Close() returned")
		}()

		select {
		case <-done:
			log.Debug().Str("tenant_id", td.TenantID).Msg("WireGuard device closed successfully")
		case <-time.After(1 * time.Second): // Reduced timeout to 1s for faster detection
			log.Error().Str("tenant_id", td.TenantID).Msg("WireGuard device close timeout - forcing completion")
			// Don't wait any longer - continue with cleanup
		}
	}

	log.Debug().Str("tenant_id", td.TenantID).Msg("Device shutdown complete")
	return nil
}

// Peer represents a WireGuard peer configuration.
type Peer struct {
	PublicKey           wgtypes.Key
	AllowedIPs          []string
	Endpoint            string
	PersistentKeepalive int
}

// DeviceStats contains statistics for a tenant's WireGuard device.
type DeviceStats struct {
	TenantID       string
	ListenPort     int // Actual UDP port the device is bound to
	EndpointPort   int // Port to use in peer configs (shared port in shared mode)
	PublicKey      string
	PeerCount      int
	ConnectedPeers int // Peers with handshake within 3 minutes
	TxBytes        uint64
	RxBytes        uint64
	LastActivity   time.Time // Most recent peer handshake
}

// enableIPForwarding enables IP forwarding and subnet routing in netstack.
// This uses unsafe pointer casting to access the private stack field in netstack.Net.
// CRITICAL: Adds route for entire tenant subnet so ICMP/TCP/UDP to peers flows through TUN→WireGuard.
func enableIPForwarding(tnet *netstack.Net, subnets []string, serverIP netip.Addr) error {
	// Silence unused import warnings (types used in struct field declarations)
	_ = (*channel.Endpoint)(nil)
	_ = (*buffer.View)(nil)
	_ = tun.EventUp
	type netTunAccess struct {
		ep             *channel.Endpoint
		stack          *stack.Stack
		events         chan tun.Event
		notifyHandle   *channel.NotificationHandle
		incomingPacket chan *buffer.View // Using buffer v2 from gvisor.dev/gvisor/pkg/buffer
		mtu            int
		dnsServers     []netip.Addr
		hasV4, hasV6   bool
	}

	// Cast Net to our access struct to reach the stack field
	netAccess := (*netTunAccess)(unsafe.Pointer(tnet))
	if netAccess.stack == nil {
		return fmt.Errorf("netstack stack is nil")
	}

	// Configure ICMP to allow local processing while forwarding is enabled
	// This ensures ICMP works for both local pings and peer-to-peer pings
	netAccess.stack.SetICMPLimit(1000) // 1000 ICMP messages per second
	// Peer-to-peer packets arrive on the WireGuard-backed NIC with a destination
	// of another tenant peer, not the router's own .33/.65/... address. Without
	// promiscuous mode, netstack treats those packets as "not for this NIC" and
	// may drop them before the forwarding path ever runs. That produces the exact
	// failure mode where the tenant router can reach every peer, but peers cannot
	// reach each other through the router.
	// if tcpipErr := netAccess.stack.SetPromiscuousMode(1, true); tcpipErr != nil {
	// 	return fmt.Errorf("failed to enable promiscuous mode on WireGuard NIC: %v", tcpipErr)
	// }
	// Forwarded peer-to-peer packets egress the same TUN NIC with the original
	// peer source IP, not the server IP. Spoofing must therefore be enabled on
	// the NIC or netstack rejects legitimate forwarded traffic as non-local.
	netAccess.stack.SetSpoofing(1, true)
	netAccess.stack.SetICMPBurst(100) // Burst of 100 ICMP messages
	// log.Debug().Msg(" Configured netstack peer forwarding flags (promiscuous + spoofing + ICMP limits)")
	if netAccess.hasV4 {
		if tcpipErr := netAccess.stack.SetForwardingDefaultAndAllNICs(ipv4.ProtocolNumber, true); tcpipErr != nil {
			return fmt.Errorf("failed to enable IPv4 forwarding: %v", tcpipErr)
		}
		log.Debug().Msg(" Enabled IPv4 forwarding (peer-to-peer mesh mode)")
	}

	if netAccess.hasV6 {
		if tcpipErr := netAccess.stack.SetForwardingDefaultAndAllNICs(ipv6.ProtocolNumber, true); tcpipErr != nil {
			return fmt.Errorf("failed to enable IPv6 forwarding: %v", tcpipErr)
		}
		log.Debug().Msg(" Enabled IPv6 forwarding (peer-to-peer mesh mode)")
	}

	// CRITICAL FIX: Add explicit subnet routes so peer-to-peer traffic (ICMP/TCP/UDP) flows through WireGuard
	// The default 0.0.0.0/0 route from CreateNetTUN is not sufficient - we need subnet-specific routes
	// This ensures packets to peers (10.255.255.x) go through TUN→WireGuard, not dropped as unreachable
	for _, subnet := range subnets {
		_, subnetNet, err := net.ParseCIDR(subnet)
		if err != nil {
			log.Warn().Err(err).Str("subnet", subnet).Msg("Failed to parse subnet for routing")
			continue
		}

		// Convert to netip format for netstack
		subnetPrefix, err := netip.ParsePrefix(subnet)
		if err != nil {
			log.Warn().Err(err).Str("subnet", subnet).Msg("Failed to parse subnet prefix")
			continue
		}

		route, err := buildTenantSubnetRoute(subnetNet)
		if err != nil {
			log.Warn().Err(err).Str("subnet", subnet).Msg("Failed to build tenant subnet route")
			continue
		}

		// Add an on-link route for this subnet on the WireGuard TUN NIC.
		// Using the server's own IP as a gateway here is incorrect and can cause
		// peer-to-peer traffic to bounce back into local delivery or be marked
		// unreachable instead of being forwarded back through WireGuard.
		netAccess.stack.AddRoute(route)

		log.Debug().
			Str("subnet", subnet).
			Str("destination", subnetPrefix.Addr().String()).
			Int("prefix_bits", subnetPrefix.Bits()).
			Msg(" Added explicit subnet route to WireGuard TUN (NIC 1)")
	}

	log.Debug().
		Strs("subnets", subnets).
		Str("server_ip", serverIP.String()).
		Msg(" Netstack configured with explicit subnet routes + IP forwarding")

	return nil
}

func buildTenantSubnetRoute(subnetNet *net.IPNet) (tcpip.Route, error) {
	if subnetNet == nil {
		return tcpip.Route{}, fmt.Errorf("subnet is nil")
	}

	ipBytes := subnetNet.IP
	if ip4 := subnetNet.IP.To4(); ip4 != nil {
		ipBytes = ip4
	} else if ip16 := subnetNet.IP.To16(); ip16 != nil {
		ipBytes = ip16
	}

	tcpipSubnet, err := tcpip.NewSubnet(
		tcpip.AddrFromSlice(ipBytes),
		tcpip.MaskFromBytes(subnetNet.Mask),
	)
	if err != nil {
		return tcpip.Route{}, err
	}

	return tcpip.Route{
		Destination: tcpipSubnet,
		NIC:         1, // NIC 1 is the WireGuard-backed TUN device
	}, nil
}

// ACLRule represents a single ACL firewall rule for packet filtering.
type ACLRule struct {
	RuleID      string
	Protocol    string // "tcp", "udp", "icmp", "all"
	SourceIP    string // CIDR notation or "any"
	DestIP      string // CIDR notation or "any"
	DestPort    int    // 0 means any port
	Action      string // "allow" or "deny"
	Description string
}

// compiledACLRule is a pre-parsed ACL rule optimized for fast packet matching.
// PERFORMANCE: Pre-parsing CIDRs eliminates repeated parsing on every packet.
type compiledACLRule struct {
	ruleID      string
	protocol    string
	srcCIDR     *net.IPNet // nil if "any"
	dstCIDR     *net.IPNet // nil if "any"
	dstPort     int
	action      string
	description string
}

// compileACLRules converts ACLRule to compiledACLRule for fast packet matching.
// PERFORMANCE: Pre-parse all CIDRs once instead of parsing on every packet.
func compileACLRules(rules []ACLRule) ([]compiledACLRule, error) {
	compiled := make([]compiledACLRule, 0, len(rules))

	for _, rule := range rules {
		c := compiledACLRule{
			ruleID:      rule.RuleID,
			protocol:    rule.Protocol,
			dstPort:     rule.DestPort,
			action:      rule.Action,
			description: rule.Description,
		}

		// Pre-parse source CIDR
		if rule.SourceIP != "any" {
			_, cidr, err := net.ParseCIDR(rule.SourceIP)
			if err != nil {
				// Try single IP
				ip := net.ParseIP(rule.SourceIP)
				if ip == nil {
					return nil, fmt.Errorf("invalid source IP: %s", rule.SourceIP)
				}
				// Convert single IP to /32 CIDR
				if ip.To4() != nil {
					cidr = &net.IPNet{IP: ip.To4(), Mask: net.CIDRMask(32, 32)}
				} else {
					cidr = &net.IPNet{IP: ip, Mask: net.CIDRMask(128, 128)}
				}
			}
			c.srcCIDR = cidr
		}

		// Pre-parse destination CIDR
		if rule.DestIP != "any" {
			_, cidr, err := net.ParseCIDR(rule.DestIP)
			if err != nil {
				// Try single IP
				ip := net.ParseIP(rule.DestIP)
				if ip == nil {
					return nil, fmt.Errorf("invalid dest IP: %s", rule.DestIP)
				}
				// Convert single IP to /32 CIDR
				if ip.To4() != nil {
					cidr = &net.IPNet{IP: ip.To4(), Mask: net.CIDRMask(32, 32)}
				} else {
					cidr = &net.IPNet{IP: ip, Mask: net.CIDRMask(128, 128)}
				}
			}
			c.dstCIDR = cidr
		}

		compiled = append(compiled, c)
	}

	return compiled, nil
}

func stripACLFilterRules(table stack.Table) (stack.Table, bool) {
	if len(table.Rules) == 0 {
		return table, false
	}

	oldRules := table.Rules
	indexMap := make([]int, len(oldRules))
	cleanRules := make([]stack.Rule, 0, len(oldRules))
	removed := false

	for oldIdx, rule := range oldRules {
		if _, ok := rule.Target.(*aclRuleFilter); ok {
			indexMap[oldIdx] = -1
			removed = true
			continue
		}
		indexMap[oldIdx] = len(cleanRules)
		cleanRules = append(cleanRules, rule)
	}

	if !removed {
		return table, false
	}

	remapIndex := func(oldIdx int) int {
		if oldIdx < 0 {
			return 0
		}
		if oldIdx >= len(indexMap) {
			if len(cleanRules) == 0 {
				return 0
			}
			return len(cleanRules) - 1
		}
		if newIdx := indexMap[oldIdx]; newIdx >= 0 {
			return newIdx
		}
		for i := oldIdx + 1; i < len(indexMap); i++ {
			if indexMap[i] >= 0 {
				return indexMap[i]
			}
		}
		if len(cleanRules) == 0 {
			return 0
		}
		return len(cleanRules) - 1
	}

	table.Rules = cleanRules
	for i := range table.BuiltinChains {
		table.BuiltinChains[i] = remapIndex(table.BuiltinChains[i])
	}
	for i := range table.Underflows {
		table.Underflows[i] = remapIndex(table.Underflows[i])
	}

	return table, true
}

func insertACLFilterRule(table *stack.Table, chain stack.Hook, rule stack.Rule) {
	insertAt := table.BuiltinChains[chain]
	newRules := make([]stack.Rule, 0, len(table.Rules)+1)
	newRules = append(newRules, table.Rules[:insertAt]...)
	newRules = append(newRules, rule)
	newRules = append(newRules, table.Rules[insertAt:]...)
	table.Rules = newRules

	for i := range table.BuiltinChains {
		if table.BuiltinChains[i] >= insertAt {
			table.BuiltinChains[i]++
		}
	}
	for i := range table.Underflows {
		if table.Underflows[i] >= insertAt {
			table.Underflows[i]++
		}
	}
}

// ApplyACLRules applies ACL firewall rules to the netstack IPTables.
// This validates rules to prevent blocking critical access and applies them to packet filter.
// Rules are checked for:
// - Not blocking peer→server communication (device IP)
// - IPs within tenant subnet range
// - No redundant/conflicting rules
// LOCK-FREE: Since rules application is atomic within netstack
func (td *TenantDevice) ApplyACLRules(rules []ACLRule) error {
	// Check if device is closed
	if td.closed.Load() {
		return fmt.Errorf("device is closed")
	}

	// Validate rules before applying
	if err := td.validateACLRules(rules); err != nil {
		return fmt.Errorf("ACL rule validation failed: %w", err)
	}

	// Access netstack for IPTables configuration
	type netTunAccess struct {
		ep             *channel.Endpoint
		stack          *stack.Stack
		events         chan tun.Event
		notifyHandle   *channel.NotificationHandle
		incomingPacket chan *buffer.View
		mtu            int
		dnsServers     []netip.Addr
		hasV4, hasV6   bool
	}

	netAccess := (*netTunAccess)(unsafe.Pointer(td.Net))
	if netAccess.stack == nil {
		return fmt.Errorf("netstack stack is nil")
	}

	st := netAccess.stack
	ipt := st.IPTables()

	// Compile ACL rules for fast packet matching
	compiledRules, err := compileACLRules(rules)
	if err != nil {
		return fmt.Errorf("failed to compile ACL rules: %w", err)
	}

	// Create ACL target with compiled rules
	aclTarget := &aclRuleFilter{
		tenantID:      td.TenantID,
		compiledRules: compiledRules,
		serverIP:      td.DeviceIP.String(),
		subnetStr:     strings.Join(td.Subnets, ","),
	}

	// Get current filter table
	filterTable := ipt.GetTable(stack.FilterID, false) // IPv4

	// Remove existing ACL rules from all builtin chains before reinstalling.
	filterTable, aclRuleRemoved := stripACLFilterRules(filterTable)

	// If no ACL rules to apply, just remove old filter and exit
	if len(rules) == 0 {
		if aclRuleRemoved {
			ipt.ForceReplaceTable(stack.FilterID, filterTable, false)
			log.Debug().
				Str("tenant_id", td.TenantID).
				Msg(" ACL packet filters removed from INPUT/FORWARD - all traffic allowed")
		}
		return nil
	}

	// Create new ACL rule with the provided rules
	aclRule := stack.Rule{
		Filter: stack.IPHeaderFilter{}, // Match all packets
		Target: aclTarget,
	}

	// Apply the ACL target to both INPUT (device-local traffic) and FORWARD
	// (peer-to-peer traffic transiting the tenant router). This is the tenant-
	// device equivalent of Tailscale-style policy enforcement on routed traffic.
	type chainInsert struct {
		hook stack.Hook
		pos  int
	}
	inserts := []chainInsert{
		{hook: stack.Input, pos: filterTable.BuiltinChains[stack.Input]},
		{hook: stack.Forward, pos: filterTable.BuiltinChains[stack.Forward]},
	}
	if inserts[0].pos > inserts[1].pos {
		inserts[0], inserts[1] = inserts[1], inserts[0]
	}
	for _, insert := range inserts {
		insertACLFilterRule(&filterTable, insert.hook, aclRule)
	}

	// Install updated filter table
	ipt.ForceReplaceTable(stack.FilterID, filterTable, false)

	log.Debug().
		Str("tenant_id", td.TenantID).
		Int("rule_count", len(rules)).
		Msg(" ACL packet filter applied to IPTables INPUT and FORWARD chains")

	return nil
}

// validateACLRules validates ACL rules to prevent blocking critical access.
func (td *TenantDevice) validateACLRules(rules []ACLRule) error {
	// Parse tenant subnet
	var subnetNets []*net.IPNet
	for _, subnet := range td.Subnets {
		_, subnetNet, err := net.ParseCIDR(subnet)
		if err != nil {
			return fmt.Errorf("invalid tenant subnet: %w", err)
		}
		subnetNets = append(subnetNets, subnetNet)
	}
	serverIP := td.DeviceIP.String()
	ruleIDs := make(map[string]bool)

	for i, rule := range rules {
		// Check for duplicate rule IDs
		if rule.RuleID == "" {
			return fmt.Errorf("rule %d: missing rule ID", i)
		}
		if ruleIDs[rule.RuleID] {
			return fmt.Errorf("duplicate rule ID: %s", rule.RuleID)
		}
		ruleIDs[rule.RuleID] = true

		// Validate action
		if rule.Action != "allow" && rule.Action != "deny" {
			return fmt.Errorf("rule %s: invalid action '%s' (must be 'allow' or 'deny')", rule.RuleID, rule.Action)
		}

		// Validate protocol
		validProtos := map[string]bool{"tcp": true, "udp": true, "icmp": true, "all": true}
		if !validProtos[rule.Protocol] {
			return fmt.Errorf("rule %s: invalid protocol '%s'", rule.RuleID, rule.Protocol)
		}

		// STRICT CHECK: Reject deny rules that would block server IP
		// This prevents users from accidentally blocking critical infrastructure
		if rule.Action == "deny" {
			if rule.DestIP == serverIP {
				return fmt.Errorf("rule %s: cannot deny access to server IP %s (would break WireGuard keepalive and connectivity)", rule.RuleID, serverIP)
			}
			if rule.DestIP == "any" {
				return fmt.Errorf("rule %s: cannot use 'any' as destination in deny rules (would block server IP %s). Use explicit peer IPs instead", rule.RuleID, serverIP)
			}
		}

		// Validate IPs are within subnet (if not "any")
		if rule.DestIP != "any" && rule.DestIP != "" {
			destIP, destNet, err := net.ParseCIDR(rule.DestIP)
			if err != nil {
				// Try parsing as single IP
				destIP = net.ParseIP(rule.DestIP)
				if destIP == nil {
					return fmt.Errorf("rule %s: invalid destination IP '%s'", rule.RuleID, rule.DestIP)
				}
			} else {
				destIP = destNet.IP
			}

			// Check if destination IP is within tenant subnet
			for _, subnetNet := range subnetNets {
				if subnetNet.Contains(destIP) {
					goto DestIPValid
				}
			}
			return fmt.Errorf("rule %s: destination IP %s is outside tenant subnets %v", rule.RuleID, rule.DestIP, td.Subnets)
		}
		if rule.SourceIP != "any" && rule.SourceIP != "" {
			srcIP, srcNet, err := net.ParseCIDR(rule.SourceIP)
			if err != nil {
				srcIP = net.ParseIP(rule.SourceIP)
				if srcIP == nil {
					return fmt.Errorf("rule %s: invalid source IP '%s'", rule.RuleID, rule.SourceIP)
				}
			} else {
				srcIP = srcNet.IP
			}

			// Check if source IP is within tenant subnet
			for _, subnetNet := range subnetNets {
				if subnetNet.Contains(srcIP) {
					goto SourceIPValid
				}
			}
			return fmt.Errorf("rule %s: source IP %s is outside tenant subnets %v", rule.RuleID, rule.SourceIP, td.Subnets)
		}

		// Validate port range
		if rule.DestPort < 0 || rule.DestPort > 65535 {
			return fmt.Errorf("rule %s: invalid port %d (must be 0-65535)", rule.RuleID, rule.DestPort)
		}

	DestIPValid:
	SourceIPValid:
		// Rule is valid
	}

	return nil
}

// aclRuleFilter implements stack.Target for rule-based packet filtering.
type aclRuleFilter struct {
	tenantID      string
	compiledRules []compiledACLRule // Pre-compiled rules for fast matching
	serverIP      string
	subnetStr     string
	flowCache     sync.Map // O(1) session cache for established flows
}

// flowKey uniquely identifies a packet flow for caching
type flowKey struct {
	protocol int
	srcIP    string
	dstIP    string
	dstPort  int
}

// Action checks packet against ACL rules and returns verdict.
// OPTIMIZED: Uses O(1) flow cache and pre-compiled rules with binary IP matching.
func (f *aclRuleFilter) Action(pkt *stack.PacketBuffer, hook stack.Hook, r *stack.Route, addressEP stack.AddressableEndpoint) (stack.RuleVerdict, int) {
	// Fast path: no rules = accept immediately
	if len(f.compiledRules) == 0 {
		return stack.RuleAccept, 0
	}

	// Use gvisor's Network() interface
	netHeader := pkt.Network()
	if netHeader == nil {
		return stack.RuleAccept, 0
	}

	// PERFORMANCE: Get binary IP addresses
	srcAddr := netHeader.SourceAddress()
	dstAddr := netHeader.DestinationAddress()

	transportProto := pkt.TransportProtocolNumber
	if transportProto == 0 {
		transportProto = netHeader.TransportProtocol()
	}

	var dstPort int
	if transportProto == 6 || transportProto == 17 {
		transHeader := pkt.TransportHeader()
		if transHeader.View().Size() > 0 {
			data := transHeader.View().AsSlice()
			if len(data) >= 4 {
				dstPort = int(data[2])<<8 | int(data[3])
			}
		}
	}

	// O(1) FLOW CACHE: Check if we've already seen this flow
	// This avoids linear rule scanning for every packet in established connections
	cacheKey := flowKey{
		protocol: int(transportProto),
		srcIP:    string(srcAddr.AsSlice()),
		dstIP:    string(dstAddr.AsSlice()),
		dstPort:  dstPort,
	}

	if val, ok := f.flowCache.Load(cacheKey); ok {
		return val.(stack.RuleVerdict), 0
	}

	// --- SLOW PATH: Linear Rule Scan ---
	// Only reached for the first packet of a new flow

	// Convert to net.IP for CIDR matching
	srcIP := net.IP(srcAddr.AsSlice())
	dstIP := net.IP(dstAddr.AsSlice())

	var protoStr string
	switch transportProto {
	case 1:
		protoStr = "icmp"
	case 6:
		protoStr = "tcp"
	case 17:
		protoStr = "udp"
	case 58:
		protoStr = "icmpv6"
	default:
		protoStr = fmt.Sprintf("proto-%d", transportProto)
	}

	verdict := stack.RuleAccept

	// Check rules in order (first match wins)
	for i := range f.compiledRules {
		rule := &f.compiledRules[i]

		// Match protocol
		if rule.protocol != "all" && rule.protocol != protoStr {
			continue
		}

		// Match source IP using pre-compiled CIDR
		if rule.srcCIDR != nil && !rule.srcCIDR.Contains(srcIP) {
			continue
		}

		// Match destination IP using pre-compiled CIDR
		if rule.dstCIDR != nil && !rule.dstCIDR.Contains(dstIP) {
			continue
		}

		// Match port (0 means any port)
		if rule.dstPort != 0 && rule.dstPort != dstPort {
			continue
		}

		// Rule matched - determine verdict and break
		if rule.action == "deny" {
			// ONLY log drops (not accepts) to avoid I/O on hot path
			log.Warn().
				Str("tenant_id", f.tenantID).
				Str("rule_id", rule.ruleID).
				Str("protocol", protoStr).
				Str("src_ip", srcIP.String()).
				Str("dst_ip", dstIP.String()).
				Int("dst_port", dstPort).
				Msg("🚫 ACL BLOCKED")
			verdict = stack.RuleDrop
		} else {
			verdict = stack.RuleAccept
		}
		break
	}

	// Cache the decision for this flow
	f.flowCache.Store(cacheKey, verdict)

	return verdict, 0
}

// HasEndpoint checks if a peer with the given IP has an active endpoint.
// Used to avoid proactive handshakes to peers that haven't connected yet.
func (td *TenantDevice) HasEndpoint(peerIP string) bool {
	ipcGet, err := td.getIPCResultCached()
	if err != nil {
		return false
	}

	// Remove /32 suffix if present
	peerIP = strings.TrimSpace(strings.TrimSuffix(peerIP, "/32"))

	var inPeerBlock bool
	var currentPeerHasEndpoint bool
	var currentPeerMatchesIP bool

	// Process line by line
	start := 0
	for i := 0; i <= len(ipcGet); i++ {
		if i == len(ipcGet) || ipcGet[i] == '\n' {
			line := strings.TrimSpace(ipcGet[start:i])
			start = i + 1

			if strings.HasPrefix(line, "public_key=") {
				// We just finished a peer block
				if inPeerBlock && currentPeerMatchesIP && currentPeerHasEndpoint {
					return true
				}
				// Start new peer block
				inPeerBlock = true
				currentPeerHasEndpoint = false
				currentPeerMatchesIP = false
			} else if inPeerBlock {
				if strings.HasPrefix(line, "allowed_ip=") {
					allowedIP := strings.TrimSpace(strings.TrimSuffix(line[11:], "/32"))
					if allowedIP == peerIP {
						currentPeerMatchesIP = true
					}
				} else if strings.HasPrefix(line, "endpoint=") {
					// Endpoint line exists and contains more than just "endpoint="
					if len(line) > 9 {
						// Double check it's not just "endpoint=\n" or "0.0.0.0:0"
						val := strings.TrimSpace(line[9:])
						if val != "" && val != "0.0.0.0:0" && val != "[::]:0" {
							currentPeerHasEndpoint = true
						}
					}
				}
			}
		}
	}

	// Check last peer block
	return inPeerBlock && currentPeerMatchesIP && currentPeerHasEndpoint
}

func (td *TenantDevice) GetTUNAddress() (*netip.Addr, error) {
	addr := td.DeviceIP
	if !addr.IsValid() {
		return nil, fmt.Errorf("tenant device IP is invalid")
	}
	return &addr, nil
}
