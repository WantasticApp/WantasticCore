// Package server provides the main userspace WireGuard multi-tenant server.
package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
	"runtime/pprof"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"WantasticCore/internal/account"
	"WantasticCore/internal/crypto"
	"WantasticCore/internal/mikrotik"
	"WantasticCore/internal/store"
	"WantasticCore/internal/webproxy"
	"WantasticCore/internal/webssh"
	"WantasticCore/internal/wg/userspace"
	"WantasticCore/internal/wusp"
	"WantasticCore/internal/wuspcontroller"

	"WantasticCore/internal/wg/userspace/wireguard-go/wgctrl/wgtypes"

	wgdevice "WantasticCore/internal/wg/userspace/wireguard-go/device"

	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
	"github.com/rs/zerolog/log"
)

const (
	peerRouteTTL            = 25 * time.Second // 5× the 5-second presence tick — clean expiry without gaps
	hubRouteTTL             = 40 * time.Second // 8× the 5-second presence tick
	redisRouteBatchSize     = 1024
	legacyPingRequestPrefix = "peer:ping:request"
)

func targetedPingRequestChannel(hubID string) string {
	if hubID == "" {
		return legacyPingRequestPrefix
	}
	return legacyPingRequestPrefix + ":" + hubID
}

// Server is the main multi-tenant WireGuard server.
type Server struct {
	// Core managers
	accountMgr *account.Manager

	// WireGuard managers (only one is active based on useKernel flag)
	wgMgr *userspace.UserspaceManager

	peerStore *PeerStore

	// Group Repository
	groupRepo store.GroupRepository

	// Redis Client for Pub/Sub events
	redisClient *redis.Client

	// Configuration
	config *Config // Server configuration
	// CommonPorts for fast scanning (SSH, Telnet, HTTP, HTTPS, Winbox, Alt-HTTP, Alt-Winbox)
	commonPorts    []int
	serverEndpoint string // Hostname or IP for peer endpoints

	// State tracking
	tenantDevices map[string]*userspace.TenantDevice // accountID -> device
	mu            sync.RWMutex

	// Context
	ctx    context.Context
	cancel context.CancelFunc
	wg     sync.WaitGroup // Track background goroutines

	// Metrics
	metricsUpdateTicker *time.Ticker

	// DirectSSHHandler with multiplexing (credential-based SSH)
	directSSHHandler *webssh.DirectSSHHandler

	// WebProxyHandler for HTTP/HTTPS proxying to peers
	webProxyHandler *webproxy.Handler

	// WinboxManager handles Winbox multiplexers
	winboxMgr *mikrotik.WinboxManager

	// WUSPController drives the WUSP device management protocol
	wuspCtrl *wuspcontroller.WUSPController

	// HubID is the unique identifier for this server instance (used for multi-node online tracking)
	hubID string

	// peerStatusGuard prevents redundant Redis writes in peer status updates.
	// Reduces Redis pipeline executions to ~1 per peer per 12.5s (half of peerRouteTTL).
	peerOnlineGuard *peerStatusGuard

	// IN-MEMORY GROUP/LINK CACHE (persisted to LMDB, loaded at startup)
	// Data is cached in memory for fast lookups during ACL enforcement
	// All writes go through LMDB transactions for durability
	peerGroups      map[string]*PeerGroup            // groupID -> group (cache)
	groupLinks      map[string]*GroupLink            // linkID -> link (cache)
	peerGroupsIndex map[string]map[string]bool       // peerID -> groupID set (cache)
	accountGroups   map[string]map[string]*PeerGroup // accountID -> groupID -> group (cache)
	accountLinks    map[string]map[string]*GroupLink // accountID -> linkID -> link (cache)
	groupMu         sync.RWMutex                     // Protects all group/link maps

	// peerAddLocks provides per-account mutual exclusion for peer creation.
	// This makes the limit-check → IP-assign → save sequence atomic, preventing
	// concurrent requests from racing past the peer cap.
	peerAddLocks sync.Map // accountID (string) -> *sync.Mutex

	// activeScans tracks in-progress port scans by scanID.
	// Moved from package-level global to avoid state bleed between Server instances/tests.
	activeScans   map[string]*userspace.PortScanner
	activeScansMu sync.RWMutex

	tenantDNS   map[string]*tenantDNSServer
	tenantDNSMu sync.Mutex

	tenantDNSPeers   map[string]map[string]tenantDNSPeerRecord
	tenantDNSPeersMu sync.RWMutex
}

// Config holds server configuration.
type Config struct {
	MaxTenants     int
	MaxPeersTotal  int
	SharedPort     int      // Shared UDP port (shared mode)
	ServerEndpoint string   // Hostname or IP for peer endpoint configuration
	AdvertiseAddr  string   // Internal Hub address for gRPC routing between cores
	GRPCAddr       string   // gRPC listen address (e.g., ":50051")
	SubnetPools    []string // Global subnet pool CIDR (e.g., "10.0.0.0/8, 192.168.0.0/16")
}

// DefaultConfig returns default configuration.
func DefaultConfig() *Config {
	return &Config{
		MaxTenants:     -1, // Unlimited in shared mode
		MaxPeersTotal:  10000,
		SharedPort:     51820,       // Standard WireGuard port
		ServerEndpoint: "localhost", // Default for local testing
		GRPCAddr:       ":50051",
		SubnetPools:    []string{"10.0.0.0/8", "172.16.0.0/12"},
	}
}

// NewServer creates a new multi-tenant WireGuard server.
func NewServer(ctx context.Context, config *Config, accStore account.Store) (*Server, error) {
	if config == nil {
		config = DefaultConfig()
	}

	// Create account manager with subnet pool and provided store
	accountMgr := account.NewManager(accStore, store.DB().IPAM(), config.SubnetPools)

	// Create peer store (using Postgres and Redis)
	peerRepo := store.DB().Peers()
	peerStore := NewPeerStore(peerRepo, store.DB().Redis())

	serverCtx, cancel := context.WithCancel(ctx)

	s := &Server{
		accountMgr:     accountMgr,
		peerStore:      peerStore,
		config:         config,
		serverEndpoint: config.ServerEndpoint,
		tenantDevices:  make(map[string]*userspace.TenantDevice),
		commonPorts:    []int{22, 23, 80, 443, 8080, 8291, 8443}, // Fast scan ports
		ctx:            serverCtx,
		cancel:         cancel,
		redisClient:    store.DB().Redis(), // Initialize Redis client
		// Initialize in-memory group/link storage
		peerGroups:      make(map[string]*PeerGroup),
		groupLinks:      make(map[string]*GroupLink),
		peerGroupsIndex: make(map[string]map[string]bool),
		accountGroups:   make(map[string]map[string]*PeerGroup),
		accountLinks:    make(map[string]map[string]*GroupLink),
		groupRepo:       store.DB().Groups(),
		activeScans:     make(map[string]*userspace.PortScanner),
		tenantDNS:       make(map[string]*tenantDNSServer),
		tenantDNSPeers:  make(map[string]map[string]tenantDNSPeerRecord),
	}

	log.Debug().Msg("Initializing userspace WireGuard mode")

	// Calculate MaxTenants and MaxPeersGlobal from pool capacity if not explicitly set
	var userspaceConfig *userspace.Config
	if config.MaxPeersTotal <= 0 {
		// Auto-calculate limits from IP pools
		calculatedConfig, err := userspace.ConfigFromPools(
			config.SubnetPools,
			config.SharedPort,
			10, // Average peers per tenant
		)
		if err != nil {
			log.Warn().Err(err).Msg("Failed to calculate limits from pools, using defaults")
			userspaceConfig = userspace.DefaultConfig()
		} else {
			userspaceConfig = calculatedConfig
			log.Debug().
				Int("calculated_max_peers", userspaceConfig.MaxPeersGlobal).
				Msg(" Auto-calculated limits from IP pool capacity")
		}
	} else {
		// Use explicitly configured limits
		userspaceConfig = &userspace.Config{
			MaxPeersGlobal: config.MaxPeersTotal,
			SharedPort:     config.SharedPort,
		}
		log.Debug().
			Int("configured_max_peers", userspaceConfig.MaxPeersGlobal).
			Msg("⚙️  Using explicitly configured limits")
	}

	wgMgr, err := userspace.NewUserspaceManager(ctx, userspaceConfig)
	if err != nil {
		cancel()
		return nil, fmt.Errorf("failed to initialize userspace WireGuard manager: %w", err)
	}
	s.wgMgr = wgMgr

	// Register instant peer active handler (route/status updates on handshake response sent)
	wgMgr.SetPeerActiveHandler(s.handlePeerActive)
	// Register session confirmed handler (WUSP probe trigger once both sides confirm new keypair)
	wgMgr.SetPeerSessionConfirmedHandler(s.handlePeerSessionConfirmed)

	// Instantiate WUSP controller and wire inbound handler.
	// OnEvent handles structured agent-originated events (ValueChange, Boot!, etc.).
	// OnNotify handles raw Notify method responses for backward compatibility.
	s.wuspCtrl = wuspcontroller.New(wuspcontroller.Options{
		Send: func(peerPublicKey string, data []byte) error {
			return wgMgr.SendWUSPByPeer(peerPublicKey, data)
		},
		StateRepo: store.DB().WUSPDeviceStates(),
		OnEvent:   s.handleWUSPEvent,
		OnNotify:  s.handleWUSPNotify,
		Log:       log.Logger,
	})
	s.wuspCtrl.Start() // launch background fragment-cleanup goroutine
	wgMgr.SetWUSPInboundHandler(func(tenantID, peerPublicKey string, data []byte) {
		s.wuspCtrl.HandleInbound(s.ctx, peerPublicKey, data)
	})

	// Initialize DirectSSHHandler with SSH multiplexing (credential-based SSH)
	s.directSSHHandler = webssh.NewDirectSSHHandler(wgMgr, peerRepo, []string{
		"http://localhost:5173",
		"http://localhost:8001",
		"https://console.wantastic.app",
	})

	// Set peer update callback for SSH sessions
	s.directSSHHandler.SetPeerUpdateFunc(func(accountID, peerID string, updateFn func(any) error) error {
		peer, err := s.peerStore.GetPeer(accountID, peerID)
		if err != nil {
			return err
		}
		// Call the update function on the peer
		if err := updateFn(peer); err != nil {
			return err
		}
		// Save the updated peer
		peer.UpdatedAt = time.Now().UTC()
		return s.peerStore.SavePeer(peer)
	})

	// Set SSH activity logging callback
	s.directSSHHandler.SetActivityLogFunc(func(tenantID, peerID string, activity webssh.SSHActivityData) error {
		// Convert commands
		var commands []SSHSessionCommand
		for _, cmd := range activity.Commands {
			commands = append(commands, SSHSessionCommand{
				Command:   cmd.Command,
				Timestamp: cmd.Timestamp,
			})
		}

		// Convert to peer store SSHActivity
		sshActivity := SSHActivity{
			SessionID:  activity.SessionID,
			UserAgent:  activity.UserAgent,
			ClientIP:   activity.ClientIP,
			Timestamp:  activity.Timestamp,
			EndTime:    activity.EndTime,
			Username:   activity.Username,
			Commands:   commands,
			BytesSent:  activity.BytesSent,
			BytesRecv:  activity.BytesRecv,
			DurationMs: activity.DurationMs,
		}

		// If EndTime is set, this is an update (session end)
		if !activity.EndTime.IsZero() {
			err := s.peerStore.UpdateSSHActivityForPeer(tenantID, peerID, activity.SessionID, func(a *SSHActivity) {
				a.EndTime = activity.EndTime
				a.BytesSent = activity.BytesSent
				a.BytesRecv = activity.BytesRecv
				a.DurationMs = activity.DurationMs
				// Update commands with full history from session
				a.Commands = commands
			})

			// If update failed because record doesn't exist (e.g. restart/lost start event),
			// force insert a new completed record to ensure we capture the history
			if err != nil && (strings.Contains(err.Error(), "no rows in result set") || strings.Contains(err.Error(), "SSH activity not found")) {
				log.Warn().Str("session_id", activity.SessionID).Msg("SSH activity start missing, force logging completed activity")
				return s.peerStore.LogSSHActivity(tenantID, peerID, sshActivity)
			}

			return err
		}

		// Otherwise, log new activity (session start)
		return s.peerStore.LogSSHActivity(tenantID, peerID, sshActivity)
	})

	log.Debug().Msg(" Initialized DirectSSHHandler with SSH multiplexing and connection pooling")

	// Initialize WebProxyHandler for HTTP/HTTPS proxying to peers
	s.webProxyHandler = webproxy.NewHandler(wgMgr)
	log.Debug().Msg(" Initialized WebProxyHandler for HTTP/HTTPS proxying")

	// Set Hub ID for distributed coordination (Hostname)
	// MUST be done before StartUptimeMonitor which uses GetHubAddress()
	hostname, _ := os.Hostname()
	if hostname == "" {
		hostname = "wantastic-hub-" + uuid.NewString()[:8]
	}
	s.hubID = hostname
	s.peerOnlineGuard = newPeerStatusGuard(peerRouteTTL)
	log.Debug().Str("hub_id", s.hubID).Msg(" Server identity initialized")

	// Initialize WinboxManager (global multiplexer for all tenants)
	s.winboxMgr = mikrotik.NewWinboxManager(wgMgr)
	s.winboxMgr.SetClearSessionsFunc(func(accountID, peerID string) error {
		return s.peerStore.ClearWinboxSessions(accountID, peerID)
	})
	log.Debug().Msg(" Initialized WinboxManager (global multiplexer)")

	// Start background Uptime Monitor (Handshake Recorder + Presence)
	s.StartUptimeMonitor(s.ctx)

	// Start background Port Scan Monitor (15m periodic scan)
	s.StartPortScanMonitor(s.ctx)

	// Start scan control listener
	s.wg.Add(1)
	go func() {
		defer s.wg.Done()
		s.StartScanControlListener(s.ctx)
	}()

	// Start ping listener
	s.wg.Add(1)
	go func() {
		defer s.wg.Done()
		s.StartPingListener(s.ctx)
	}()

	return s, nil
}

// pulseRedis updates a single peer's online status in Redis.
func (s *Server) pulseRedis(peerID string) {
	if s.redisClient == nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	key := fmt.Sprintf("online_peer:%s", peerID)
	hubAddr := s.GetHubAddress()
	log.Debug().Str("key", key).Msg("💓 Pulsing Redis for peer")
	pipe := s.redisClient.Pipeline()
	pipe.Set(ctx, "hub_addr:"+s.hubID, hubAddr, hubRouteTTL)
	pipe.Set(ctx, key, s.hubID, peerRouteTTL)
	if _, err := pipe.Exec(ctx); err != nil {
		log.Warn().Err(err).Str("peer_id", peerID).Msg("Failed to pulse peer route in Redis")
	}

	// Broadcast roaming event for instant propagation to tenant proxies
	s.broadcastPeerRoaming(ctx, peerID)
}

// broadcastPeerRoaming publishes a roaming event so tenant proxies update their cache instantly.
func (s *Server) broadcastPeerRoaming(ctx context.Context, peerID string) {
	if s.redisClient == nil {
		return
	}
	// Use GetHubAddress() so the roaming message matches the hub_addr Redis key exactly.
	hubAddr := s.GetHubAddress()
	if hubAddr == "" {
		return
	}
	// Format: "peerKey:hubAddr" — SplitN at first ":" is safe even if hubAddr contains ":".
	s.redisClient.Publish(ctx, "peer_roaming", peerID+":"+hubAddr)
}

// isPeerOnlineViaRedis checks Redis (source of truth) to determine if a peer is online.
// Returns true if the online_peer:{peerID} key exists in Redis.
func (s *Server) isPeerOnlineViaRedis(peerID string) bool {
	if s.redisClient == nil {
		return false
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	val, err := s.redisClient.Exists(ctx, "online_peer:"+peerID).Result()
	return err == nil && val > 0
}

// enrichPeersOnlineFromRedis batch-checks Redis for online status of multiple peers
// using a pipeline for efficiency. Sets peer.IsOnline = true for each peer found in Redis.
func (s *Server) enrichPeersOnlineFromRedis(peers []*PeerMetadata) {
	if s.redisClient == nil || len(peers) == 0 {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	pipe := s.redisClient.Pipeline()
	cmds := make([]*redis.StringCmd, len(peers))
	for i, peer := range peers {
		cmds[i] = pipe.Get(ctx, "online_peer:"+peer.ID)
	}
	if _, err := pipe.Exec(ctx); err != nil && err != redis.Nil {
		log.Warn().Err(err).Int("peers", len(peers)).Msg("Redis pipeline failed for peer online enrichment — showing DB-only status")
	}

	for i, cmd := range cmds {
		if cmd.Err() == nil && cmd.Val() != "" {
			peers[i].IsOnline = true
			// If peer is online via Redis, ensure LastSeenAt is fresh
			// so the frontend shows "Just now" instead of a stale time or "Never"
			peers[i].LastSeenAt = time.Now().UTC()
		} else {
			// Peer is absent from Redis → definitively offline; clear any stale DB flag.
			peers[i].IsOnline = false
		}
	}
}

func (s *Server) RegisterWebProxySession(sessionID string) error {
	if s.redisClient == nil {
		return nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	key := fmt.Sprintf("webproxy:session:%s", sessionID)
	// Expire after 2 hours (matching DefaultIdleTimeout)
	return s.redisClient.Set(ctx, key, s.hubID, 2*time.Hour).Err()
}

// UnregisterWebProxySession removes a web proxy session location from Redis.
func (s *Server) UnregisterWebProxySession(sessionID string) {
	if s.redisClient == nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	key := fmt.Sprintf("webproxy:session:%s", sessionID)
	_ = s.redisClient.Del(ctx, key).Err()
}

// RegisterWebSSHSession registers where a saved WebSSH session is hosted so the
// portal can route future stream attaches to the correct core.
// It intentionally does not mark the session as "active"; that only happens
// once a live SSH stream is successfully established.
func (s *Server) RegisterWebSSHSession(tenantID, sessionID string) error {
	if s.redisClient == nil {
		return nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// 1. Session location (for routing)
	locationKey := fmt.Sprintf("webssh:session:%s", sessionID)
	if err := s.redisClient.Set(ctx, locationKey, s.hubID, 24*time.Hour).Err(); err != nil {
		return err
	}

	return nil
}

// ActivateWebSSHSession marks a WebSSH session as actively streaming.
func (s *Server) ActivateWebSSHSession(tenantID, sessionID string) error {
	if s.redisClient == nil {
		return nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	activeKey := fmt.Sprintf("webssh:active:%s", sessionID)
	if err := s.redisClient.Set(ctx, activeKey, s.hubID, 24*time.Hour).Err(); err != nil {
		return err
	}

	tenantKey := fmt.Sprintf("webssh:tenant_sessions:%s", tenantID)
	if err := s.redisClient.SAdd(ctx, tenantKey, sessionID).Err(); err != nil {
		return err
	}
	if err := s.redisClient.Expire(ctx, tenantKey, 48*time.Hour).Err(); err != nil {
		return err
	}

	return nil
}

// DeactivateWebSSHSession removes the active marker for a WebSSH stream but
// keeps the routing record so the saved session can be reopened later.
func (s *Server) DeactivateWebSSHSession(tenantID, sessionID string) {
	if s.redisClient == nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	activeKey := fmt.Sprintf("webssh:active:%s", sessionID)
	s.redisClient.Del(ctx, activeKey)

	tenantKey := fmt.Sprintf("webssh:tenant_sessions:%s", tenantID)
	s.redisClient.SRem(ctx, tenantKey, sessionID)
}

// UnregisterWebSSHSession removes a WebSSH session from Redis completely.
func (s *Server) UnregisterWebSSHSession(tenantID, sessionID string) {
	if s.redisClient == nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	locationKey := fmt.Sprintf("webssh:session:%s", sessionID)
	activeKey := fmt.Sprintf("webssh:active:%s", sessionID)

	s.redisClient.Del(ctx, locationKey)
	s.redisClient.Del(ctx, activeKey)

	tenantKey := fmt.Sprintf("webssh:tenant_sessions:%s", tenantID)
	s.redisClient.SRem(ctx, tenantKey, sessionID)
}

// CountWebSSHSessions returns the number of active WebSSH sessions for a tenant.
func (s *Server) CountWebSSHSessions(tenantID string) (int, error) {
	if s.redisClient == nil {
		return 0, nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	tenantKey := fmt.Sprintf("webssh:tenant_sessions:%s", tenantID)
	count, err := s.redisClient.SCard(ctx, tenantKey).Result()
	return int(count), err
}

// Event types
const (
	EventPeerStatus = "peer_status"
)

// PeerStatusEvent represents a peer status change event
type PeerStatusEvent struct {
	Event     string    `json:"event"`
	AccountID string    `json:"account_id"`
	PeerID    string    `json:"peer_id"`
	IsOnline  bool      `json:"is_online"`
	Timestamp time.Time `json:"timestamp"`
}

// PublishPeerStatusEvent publishes a peer status event to Redis Pub/Sub
func (s *Server) PublishPeerStatusEvent(accountID, peerID string, isOnline bool) {
	if s.redisClient == nil {
		return
	}

	event := PeerStatusEvent{
		Event:     EventPeerStatus,
		AccountID: accountID,
		PeerID:    peerID,
		IsOnline:  isOnline,
		Timestamp: time.Now().UTC(),
	}

	payload, err := json.Marshal(event)
	if err != nil {
		log.Warn().Err(err).Msg("Failed to marshal peer status event")
		return
	}

	// Publish to global channel
	if err := s.redisClient.Publish(context.Background(), "wantastic:events:peer_status", payload).Err(); err != nil {
		log.Warn().Err(err).Msg("Failed to publish peer status event")
	} else {
		log.Debug().
			Str("peer_id", peerID).
			Bool("is_online", isOnline).
			Msg(" Published peer status event")
	}
}

// RestoreTenantsFromDatabase recovers all WireGuard devices from persisted accounts.
// This must be called after NewServer to restore state after a restart.
func (s *Server) RestoreTenantsFromDatabase() error {
	accounts, err := s.accountMgr.ListAccounts()
	if err != nil {
		return fmt.Errorf("failed to list accounts: %w", err)
	}

	if len(accounts) == 0 {
		log.Debug().Msg("No existing accounts to restore")
		return nil
	}

	log.Debug().Int("account_count", len(accounts)).Msg("Restoring WireGuard devices from database")

	for _, acc := range accounts {
		// Parse the persisted private key from account
		privateKey, err := wgtypes.ParseKey(acc.PrivateKey)
		if err != nil {
			log.Error().
				Err(err).
				Str("account_id", acc.ID).
				Str("account_name", acc.Name).
				Msg("Failed to parse private key - account may need recreation")
			continue
		}

		var listenPort int

		// Use Networks if available (new multi-block accounts), fallback to Subnet for old accounts
		subnets := acc.Networks

		maxPeers := tenantDevicePeerLimit(acc)

		device, err := s.wgMgr.CreateTenant(acc.ID, subnets, maxPeers, privateKey)
		if err != nil {
			log.Error().
				Err(err).
				Str("account_id", acc.ID).
				Str("account_name", acc.Name).
				Msg("Failed to restore userspace WireGuard device")
			continue
		}
		// Track device (userspace only needs this map)
		s.mu.Lock()
		s.tenantDevices[acc.ID] = device
		s.mu.Unlock()
		s.ensureTenantDNS(acc.ID, device)

		// Enable ACL checking for this tenant
		// s.setupACLChecker(device)

		listenPort = device.GetEndpointPort() // Use endpoint port for restoration
		device.SetPeerAnnounceHandler(func(pubKey *wgdevice.NoisePublicKey) {
			s.pulseRedis(pubKey.String())
		})
		// Restore peers for this tenant
		peers, err := s.peerStore.ListPeers(acc.ID)
		if err != nil {
			log.Warn().
				Err(err).
				Str("account_id", acc.ID).
				Msg("Failed to list peers for tenant")
			continue
		}
		s.resetTenantDNSPeers(acc.ID, peers)
		s.invalidateTenantDNS(acc.ID)
		if err := s.reconcileTenantDevicePeers(acc.ID, device, peers); err != nil {
			log.Warn().
				Err(err).
				Str("account_id", acc.ID).
				Msg("Failed to reconcile peers during tenant restore")
		}

		log.Debug().
			Str("account_id", acc.ID).
			Str("account_name", acc.Name).
			Strs("subnets", acc.Networks).
			Int("listen_port", listenPort).
			Int("peer_count", len(peers)).
			Msg("Restored tenant device")
	}

	// Restore group links from database
	// Restore peer groups and links from database
	if err := s.loadPeerGroupsFromDB(); err != nil {
		log.Warn().Err(err).Msg("Failed to load peer groups from database")
	}
	if err := s.loadGroupLinksFromDB(); err != nil {
		log.Warn().Err(err).Msg("Failed to load group links from database")
	}

	log.Debug().Msg("Tenant restoration complete")
	return nil
}

// CreateAccount creates a new tenant account with its own WireGuard network.
// maxPeers controls the device cap; the account manager allocates enough
// /27 blocks to cover it (each block holds 29 usable peer IPs).
func (s *Server) CreateAccount(name string, maxPeers int) (*account.Account, error) {
	// Create account (this generates and stores the private key)
	acc, err := s.accountMgr.CreateAccount(name, maxPeers)
	if err != nil {
		return nil, fmt.Errorf("failed to create account: %w", err)
	}

	// Parse the private key from the newly created account
	privateKey, err := wgtypes.ParseKey(acc.PrivateKey)
	if err != nil {
		s.accountMgr.DeleteAccount(acc.ID)
		return nil, fmt.Errorf("failed to parse private key: %w", err)
	}

	var listenPort int
	// create tenent device in userspace mode
	subnets := acc.Networks

	effectiveMaxPeers := tenantDevicePeerLimit(acc)

	device, err := s.wgMgr.CreateTenant(acc.ID, subnets, effectiveMaxPeers, privateKey)
	if err != nil {
		// Rollback account creation
		s.accountMgr.DeleteAccount(acc.ID)
		return nil, fmt.Errorf("failed to create userspace WireGuard device: %w", err)
	}
	// Track device (userspace only)
	s.mu.Lock()
	s.tenantDevices[acc.ID] = device
	s.mu.Unlock()
	s.ensureTenantDNS(acc.ID, device)

	// Enable ACL checking for this tenant
	// s.setupACLChecker(device)

	listenPort = device.GetEndpointPort() // Use endpoint port for logging

	log.Debug().
		Str("account_id", acc.ID).
		Str("account_name", name).
		Strs("subnets", acc.Networks).
		Int("endpoint_port", listenPort).
		Msg("Created tenant account")

	return acc, nil
}

// DeleteAccount removes a tenant account and cleans up all resources.
func (s *Server) DeleteAccount(accountID string) error {
	// Delete all peer metadata first
	if err := s.peerStore.DeleteAccountPeers(accountID); err != nil {
		log.Warn().Err(err).Msg("Failed to delete peer metadata")
	}
	s.clearTenantDNSPeers(accountID)
	s.stopTenantDNS(accountID)

	// Delete WireGuard device (if it exists)
	if err := s.wgMgr.DeleteTenant(accountID); err != nil {
		log.Warn().Err(err).Str("account_id", accountID).Msg("Failed to delete userspace WireGuard device (may not exist)")
	}
	// Remove from tracking (userspace only)
	s.mu.Lock()
	delete(s.tenantDevices, accountID)
	s.mu.Unlock()

	// Delete account
	if err := s.accountMgr.DeleteAccount(accountID); err != nil {
		return fmt.Errorf("failed to delete account: %w", err)
	}

	log.Debug().Str("account_id", accountID).Msg("Deleted tenant account")

	return nil
}

// GetAccount retrieves account information.
func (s *Server) GetAccount(accountID string) (*account.Account, error) {
	return s.accountMgr.GetAccount(accountID)
}

// SetPeerLimitOverride is retained as a thin alias for SetAccountMaxPeers so
// existing call sites (admin tooling, gRPC service) keep compiling.
// Pass limit=0 to revert to the default cap.
func (s *Server) SetPeerLimitOverride(accountID string, limit int) error {
	_, err := s.SetAccountMaxPeers(accountID, limit)
	return err
}

// ListAccounts returns all accounts.
func (s *Server) ListAccounts() ([]*account.Account, error) {
	return s.accountMgr.ListAccounts()
}

// AddBlockToAccount adds a block to an account.
func (s *Server) AddBlockToAccount(accountID string) (*account.Account, error) {
	return s.accountMgr.AddBlockToAccount(accountID)
}

// SetAccountMaxPeers updates the account's max-peer cap and ensures the
// underlying WireGuard device sees the new limit. Replaces the old
// UpdateAccountTier API (Phase 2: billing/tier semantics removed).
func (s *Server) SetAccountMaxPeers(accountID string, maxPeers int) (*account.Account, error) {
	if err := s.accountMgr.SetMaxPeers(accountID, maxPeers); err != nil {
		return nil, err
	}

	updatedAccount, err := s.accountMgr.GetAccount(accountID)
	if err != nil {
		return nil, fmt.Errorf("reload account after max-peers update: %w", err)
	}

	if s.wgMgr != nil {
		if err := s.wgMgr.UpdateTenantSubnets(accountID, updatedAccount.Networks); err != nil {
			log.Error().
				Err(err).
				Str("account_id", accountID).
				Strs("networks", updatedAccount.Networks).
				Msg("❌ Failed to update WireGuard device subnets after max-peers change")
		}
		if err := s.wgMgr.UpdateTenantMaxPeers(accountID, tenantDevicePeerLimit(updatedAccount)); err != nil {
			log.Error().
				Err(err).
				Str("account_id", accountID).
				Int("effective_limit", tenantDevicePeerLimit(updatedAccount)).
				Msg("❌ Failed to update WireGuard device peer limit after max-peers change")
		}
		s.refreshTrackedTenantDevice(accountID)
	}

	return updatedAccount, nil
}

// Publish event for tier update is handled in UpdateAccountTier (below) or we add it here?
// The snippet above shows UpdateAccountTier implementation.
// Let's add the event publication at the end.

// ValidatePeerIP validates an IP address for peer assignment.
func (s *Server) ValidatePeerIP(accountID, ipAddress string) error {
	return s.accountMgr.ValidatePeerIP(accountID, ipAddress)
}

// GetNextAvailablePeerIP returns the next available IP address for peer assignment in an account.
func (s *Server) GetNextAvailablePeerIP(accountID string) (string, error) {
	// Get account
	account, err := s.accountMgr.GetAccount(accountID)
	if err != nil {
		return "", fmt.Errorf("account not found: %w", err)
	}

	if len(account.Networks) == 0 {
		return "", fmt.Errorf("account has no networks allocated")
	}

	// Get list of assigned peer IPs for this account
	assignedIPs := make(map[string]bool)
	peers, err := s.peerStore.ListPeers(accountID)
	if err != nil {
		log.Warn().Err(err).Str("account_id", accountID).Msg("Failed to get peer list for IP assignment")
	} else {
		for _, peer := range peers {
			if peer.AssignedIP != "" {
				// Store bare IP without /32
				bareIP := strings.TrimSuffix(peer.AssignedIP, "/32")
				assignedIPs[bareIP] = true
			}
		}
	}

	// Check each network block for available IPs
	for _, networkCIDR := range account.Networks {
		_, network, err := net.ParseCIDR(networkCIDR)
		if err != nil {
			continue
		}

		// For /27 networks, iterate through usable IPs (excluding network and broadcast)
		ones, bits := network.Mask.Size()
		if ones != 27 || bits != 32 {
			continue // Only handle /27 blocks for now
		}

		// Start from IP .1 (skip network address .0)
		ip := network.IP.To4()
		if ip == nil {
			continue
		}

		// Iterate through the 32 addresses in /27 (0-31)
		// Skip .0 (network) and .31 (broadcast), check .1 through .30
		for i := 1; i <= 30; i++ {
			candidateIP := net.IPv4(ip[0], ip[1], ip[2], ip[3]+byte(i))
			candidateStr := candidateIP.String()

			// Skip if already assigned to a peer
			if assignedIPs[candidateStr] {
				continue
			}

			// Skip if reserved as server IP
			isServerIP := false
			for _, serverIP := range account.ServerIPs {
				if serverIP == candidateStr {
					isServerIP = true
					break
				}
			}
			if isServerIP {
				continue
			}

			// Found available IP
			return candidateStr, nil
		}
	}

	return "", fmt.Errorf("no available IP addresses in account networks %v", account.Networks)
}

// IPStatistics represents IP allocation and usage statistics for an account.
type IPStatistics struct {
	TotalIPs     int `json:"total_ips"`     // Total usable IPs (BlockCount * 29)
	AssignedIPs  int `json:"assigned_ips"`  // Number of IPs assigned to peers
	AvailableIPs int `json:"available_ips"` // Remaining unassigned IPs
	BlockCount   int `json:"block_count"`   // Number of /27 blocks allocated
}

func planPeerLimit(acc *account.Account) int {
	if acc == nil {
		return 0
	}
	if acc.MaxPeers > 0 {
		return acc.MaxPeers
	}
	return acc.BlockCount * 29
}

func tenantDevicePeerLimit(acc *account.Account) int {
	if acc == nil {
		return 0
	}
	if acc.MaxPeers > 0 {
		return acc.MaxPeers
	}

	maxPeers := acc.BlockCount * 29
	if maxPeers <= 0 {
		maxPeers = planPeerLimit(acc)
	}

	return maxPeers
}

// accountPeerMu returns the per-account mutex used to serialise peer lifecycle
// mutations so add/remove/recovery paths cannot race each other for the same tenant.
// It is safe for concurrent use: sync.Map guarantees exactly one *sync.Mutex per key.
func (s *Server) accountPeerMu(accountID string) *sync.Mutex {
	v, _ := s.peerAddLocks.LoadOrStore(accountID, &sync.Mutex{})
	return v.(*sync.Mutex)
}

func peerAllowedIPs(peer *PeerMetadata) []string {
	if len(peer.AllowedIPs) > 0 {
		return append([]string(nil), peer.AllowedIPs...)
	}
	if peer.AssignedIP == "" {
		return nil
	}

	assignedIP := peer.AssignedIP
	if !strings.Contains(assignedIP, "/") {
		assignedIP += "/32"
	}
	return []string{assignedIP}
}

func buildUserspacePeer(peer *PeerMetadata) (*userspace.Peer, error) {
	pubKey, err := wgtypes.ParseKey(peer.ID)
	if err != nil {
		return nil, err
	}

	return &userspace.Peer{
		PublicKey:           pubKey,
		AllowedIPs:          peerAllowedIPs(peer),
		Endpoint:            "",
		PersistentKeepalive: 0,
	}, nil
}

func (s *Server) restorePeerOnDevice(device *userspace.TenantDevice, peer *PeerMetadata) error {
	wgPeer, err := buildUserspacePeer(peer)
	if err != nil {
		return err
	}
	return device.RestorePeer(wgPeer)
}

func (s *Server) reconcileTenantDevicePeers(accountID string, device *userspace.TenantDevice, peers []*PeerMetadata) error {
	currentKeys, err := device.ListPeerPublicKeys()
	if err != nil {
		return fmt.Errorf("failed to list runtime peers: %w", err)
	}

	desired := make(map[string]*PeerMetadata, len(peers))
	for _, peer := range peers {
		desired[peer.ID] = peer
	}

	for _, currentKey := range currentKeys {
		if _, ok := desired[currentKey]; ok {
			continue
		}

		pubKey, err := wgtypes.ParseKey(currentKey)
		if err != nil {
			log.Warn().Err(err).Str("account_id", accountID).Str("peer_id", currentKey).Msg("Skipping stale runtime peer with invalid public key")
			continue
		}
		if err := device.RemovePeer(pubKey); err != nil {
			return fmt.Errorf("failed to remove stale runtime peer %s: %w", currentKey, err)
		}
		log.Info().Str("account_id", accountID).Str("peer_id", currentKey).Msg("Removed stale runtime peer during reconciliation")
	}

	for _, peer := range peers {
		if err := s.restorePeerOnDevice(device, peer); err != nil {
			return fmt.Errorf("failed to restore peer %s: %w", peer.ID, err)
		}
	}

	return nil
}

func shouldIgnoreRuntimePeerRemoval(err error) bool {
	if err == nil {
		return false
	}

	lower := strings.ToLower(err.Error())
	return (strings.Contains(lower, "tenant") && strings.Contains(lower, "not found")) ||
		strings.Contains(lower, "device is closed")
}

func (s *Server) clearPeerRuntimeMappings(peerID string) {
	if s.redisClient == nil || peerID == "" {
		return
	}

	if err := s.redisClient.Del(
		context.Background(),
		"online_peer:"+peerID,
		"wusp_peer_account:"+peerID,
	).Err(); err != nil {
		log.Warn().Err(err).Str("peer_id", peerID).Msg("Failed to clear peer runtime mappings")
	}
}

// GetAccountIPStatistics returns IP allocation statistics for an account.
func (s *Server) GetAccountIPStatistics(accountID string) (*IPStatistics, error) {
	// Get account info
	acc, err := s.accountMgr.GetAccount(accountID)
	if err != nil {
		return nil, fmt.Errorf("account not found: %w", err)
	}

	// Calculate total usable IPs (29 per /27 block)
	totalIPs := acc.BlockCount * 29

	// Get assigned IPs from peer store
	peers, err := s.peerStore.ListPeers(accountID)
	if err != nil {
		return nil, fmt.Errorf("failed to list peers: %w", err)
	}
	assignedIPs := len(peers)

	// Calculate available IPs
	availableIPs := max(totalIPs-assignedIPs, 0)

	return &IPStatistics{
		TotalIPs:     totalIPs,
		AssignedIPs:  assignedIPs,
		AvailableIPs: availableIPs,
		BlockCount:   acc.BlockCount,
	}, nil
}

// AddPeer adds a peer to a tenant's network.
//
// The entire sequence — limit check, IP assignment, WireGuard registration, and
// DB persistence — is executed under a per-account mutex so that concurrent
// requests cannot race past the peer cap or claim the same IP address.
func (s *Server) AddPeer(accountID, peerName string, assignedIP string) (*PeerInfo, error) {
	// Generate the key pair BEFORE taking the lock: pure CPU work, no shared
	// state, and we want to hold the lock for the shortest possible time.
	privateKey, err := wgtypes.GeneratePrivateKey()
	if err != nil {
		return nil, fmt.Errorf("failed to generate private key: %w", err)
	}
	publicKey := privateKey.PublicKey()

	// --- begin atomic section for this account ---
	mu := s.accountPeerMu(accountID)
	mu.Lock()
	defer mu.Unlock()

	// 1. Fetch account and enforce the effective peer limit:
	//    admin override first, otherwise the normal plan-based default.
	acc, err := s.accountMgr.GetAccount(accountID)
	if err != nil {
		return nil, fmt.Errorf("account not found: %w", err)
	}
	// CountPeers goes directly to Postgres (bypasses Redis cache) so the count
	// is always authoritative — a stale cache can never let extra peers through.
	currentCount, err := s.peerStore.CountPeers(accountID)
	if err != nil {
		return nil, fmt.Errorf("failed to check peer count: %w", err)
	}
	maxPeers := planPeerLimit(acc)
	log.Info().
		Str("account_id", accountID).
		Int("current_peers", currentCount).
		Int("max_peers", maxPeers).
		Int("max_peers_cap", acc.MaxPeers).
		Msg("peer limit check")
	if maxPeers > 0 && currentCount >= maxPeers {
		log.Warn().
			Str("account_id", accountID).
			Int("current_peers", currentCount).
			Int("max_peers", maxPeers).
			Msg("🚫 peer limit enforced — request rejected")
		return nil, fmt.Errorf("peer limit reached (%d/%d) for this account", currentCount, maxPeers)
	}

	// 2. Assign IP address (also under lock so no two goroutines get the same IP).
	if assignedIP == "" {
		ip, err := s.GetNextAvailablePeerIP(accountID)
		if err != nil {
			return nil, fmt.Errorf("failed to assign IP address: %w", err)
		}
		assignedIP = ip
	}

	// Each peer owns only its own /32.  Do NOT add the entire tenant subnet here:
	// that would cause WireGuard to route all subnet traffic to the last-added peer.
	allowedIPs := []string{assignedIP + "/32"}

	// 3. Register the peer in the WireGuard device (side-effecting).
	wgPeer := &userspace.Peer{
		PublicKey:           publicKey,
		AllowedIPs:          allowedIPs,
		PersistentKeepalive: 0, // set dynamically after first handshake
	}
	if err := s.wgMgr.AddPeer(accountID, wgPeer); err != nil {
		return nil, fmt.Errorf("failed to add peer to WireGuard device: %w", err)
	}

	// 4. Persist metadata.  On failure, remove the peer from WireGuard so the
	//    two stores never diverge (compensating transaction).
	now := time.Now().UTC()
	peerMeta := &PeerMetadata{
		ID:                 publicKey.String(),
		AccountID:          accountID,
		Name:               peerName,
		AssignedIP:         assignedIP,
		AllowedIPs:         allowedIPs,
		PrivateKey:         privateKey.String(),
		WireGuardPublicKey: publicKey.String(),
		CreatedAt:          now,
		UpdatedAt:          now,
	}
	if err := s.peerStore.SavePeer(peerMeta); err != nil {
		// Rollback: pull the peer back out of WireGuard.
		if rbErr := s.wgMgr.RemovePeer(accountID, publicKey); rbErr != nil {
			log.Error().Err(rbErr).Str("public_key", publicKey.String()).
				Msg("CRITICAL: failed to rollback WireGuard peer after DB save failure")
		}
		return nil, fmt.Errorf("failed to persist peer metadata (rolled back WireGuard entry): %w", err)
	}
	s.upsertTenantDNSPeer(peerMeta)
	s.invalidateTenantDNS(accountID)
	// --- end atomic section ---

	log.Debug().
		Str("account_id", accountID).
		Str("peer_name", peerName).
		Str("peer_ip", assignedIP).
		Int("peers_after", currentCount+1).
		Int("max_peers", maxPeers).
		Msg("peer added")

	return &PeerInfo{
		Name:            peerName,
		PrivateKey:      privateKey.String(),
		PublicKey:       publicKey.String(),
		AllowedIPs:      allowedIPs,
		ServerPublicKey: s.GetServerPublicKey(accountID),
		ServerEndpoint:  s.serverEndpoint,
	}, nil
}

// AddPeerWithKey adds a peer with a caller-supplied public key.
//
// Identical atomicity guarantees as AddPeer: the full sequence runs under the
// per-account mutex so concurrent requests cannot race the limit or steal an IP.
func (s *Server) AddPeerWithKey(accountID, peerName, assignedIP, publicKey string) (*PeerInfo, error) {
	// Parse and validate the key BEFORE taking the lock (no shared state).
	pubKey, err := wgtypes.ParseKey(publicKey)
	if err != nil {
		return nil, fmt.Errorf("invalid public key: %w", err)
	}

	// --- begin atomic section for this account ---
	mu := s.accountPeerMu(accountID)
	mu.Lock()
	defer mu.Unlock()

	// 1. Fetch account and enforce the effective peer limit:
	//    admin override first, otherwise the normal plan-based default.
	acc, err := s.accountMgr.GetAccount(accountID)
	if err != nil {
		return nil, fmt.Errorf("account not found: %w", err)
	}
	currentCount, err := s.peerStore.CountPeers(accountID)
	if err != nil {
		return nil, fmt.Errorf("failed to check peer count: %w", err)
	}
	maxPeers := planPeerLimit(acc)
	log.Info().
		Str("account_id", accountID).
		Int("current_peers", currentCount).
		Int("max_peers", maxPeers).
		Int("max_peers_cap", acc.MaxPeers).
		Msg("peer limit check (with-key)")
	if maxPeers > 0 && currentCount >= maxPeers {
		log.Warn().
			Str("account_id", accountID).
			Int("current_peers", currentCount).
			Int("max_peers", maxPeers).
			Msg("🚫 peer limit enforced (with-key) — request rejected")
		return nil, fmt.Errorf("peer limit reached (%d/%d) for this account", currentCount, maxPeers)
	}

	// 2. Assign IP address under lock so no two goroutines get the same IP.
	if assignedIP == "" {
		ip, err := s.GetNextAvailablePeerIP(accountID)
		if err != nil {
			return nil, fmt.Errorf("failed to assign IP address: %w", err)
		}
		assignedIP = ip
	}

	allowedIPs := []string{assignedIP + "/32"}

	// 3. Register in WireGuard device.
	wgPeer := &userspace.Peer{
		PublicKey:           pubKey,
		AllowedIPs:          allowedIPs,
		PersistentKeepalive: 0,
	}
	if err := s.wgMgr.AddPeer(accountID, wgPeer); err != nil {
		return nil, fmt.Errorf("failed to add peer to WireGuard device: %w", err)
	}

	// 4. Persist metadata; rollback WireGuard on failure.
	now := time.Now().UTC()
	peerMeta := &PeerMetadata{
		ID:                 publicKey,
		AccountID:          accountID,
		Name:               peerName,
		AssignedIP:         assignedIP,
		AllowedIPs:         allowedIPs,
		WireGuardPublicKey: publicKey,
		CreatedAt:          now,
		UpdatedAt:          now,
	}
	if err := s.peerStore.SavePeer(peerMeta); err != nil {
		if rbErr := s.wgMgr.RemovePeer(accountID, pubKey); rbErr != nil {
			log.Error().Err(rbErr).Str("public_key", publicKey).
				Msg("CRITICAL: failed to rollback WireGuard peer after DB save failure")
		}
		return nil, fmt.Errorf("failed to persist peer metadata (rolled back WireGuard entry): %w", err)
	}
	s.upsertTenantDNSPeer(peerMeta)
	s.invalidateTenantDNS(accountID)
	// --- end atomic section ---

	log.Debug().
		Str("account_id", accountID).
		Str("peer_name", peerName).
		Str("peer_ip", assignedIP).
		Int("peers_after", currentCount+1).
		Int("max_peers", maxPeers).
		Msg("peer added (with caller key)")

	return &PeerInfo{
		Name:            peerName,
		PublicKey:       publicKey,
		AllowedIPs:      allowedIPs,
		ServerPublicKey: s.GetServerPublicKey(accountID),
		ServerEndpoint:  s.serverEndpoint,
	}, nil
}

// GetServerPublicKey returns the public key for a tenant's WireGuard device.
func (s *Server) GetServerPublicKey(accountID string) string {
	device, err := s.wgMgr.GetDevice(accountID)
	if err != nil {
		return ""
	}
	return device.PublicKey.String()
}

// RemovePeer removes a peer from a tenant's network.
func (s *Server) RemovePeer(accountID string, publicKey string) error {
	key, err := wgtypes.ParseKey(publicKey)
	if err != nil {
		return fmt.Errorf("invalid public key: %w", err)
	}

	mu := s.accountPeerMu(accountID)
	mu.Lock()
	defer mu.Unlock()

	peerMeta, err := s.peerStore.GetPeer(accountID, publicKey)
	if err != nil {
		return fmt.Errorf("peer not found: %w", err)
	}

	runtimeRemoved := false
	if err := s.wgMgr.RemovePeer(accountID, key); err != nil {
		if !shouldIgnoreRuntimePeerRemoval(err) {
			return fmt.Errorf("failed to remove peer from userspace device: %w", err)
		}
		log.Warn().
			Err(err).
			Str("account_id", accountID).
			Str("peer_id", publicKey).
			Msg("Runtime tenant device missing while removing peer; proceeding with durable delete")
	} else {
		runtimeRemoved = true
	}

	if err := s.peerStore.DeletePeer(accountID, publicKey); err != nil {
		if runtimeRemoved {
			device, devErr := s.wgMgr.GetDevice(accountID)
			if devErr != nil {
				log.Error().
					Err(devErr).
					Str("account_id", accountID).
					Str("peer_id", publicKey).
					Msg("CRITICAL: failed to reload device for peer rollback after DB delete failure")
			} else if rbErr := s.restorePeerOnDevice(device, peerMeta); rbErr != nil {
				log.Error().
					Err(rbErr).
					Str("account_id", accountID).
					Str("peer_id", publicKey).
					Msg("CRITICAL: failed to rollback runtime peer after DB delete failure")
			}
		}
		return fmt.Errorf("failed to delete peer metadata: %w", err)
	}

	s.removeTenantDNSPeer(accountID, publicKey)
	s.invalidateTenantDNS(accountID)
	s.clearPeerRuntimeMappings(publicKey)

	log.Debug().
		Str("account_id", accountID).
		Str("public_key", publicKey).
		Msg("Removed peer")

	return nil
}

// ListPeers returns all peers for an account.
func (s *Server) ListPeers(accountID string) ([]*PeerMetadata, error) {
	log.Debug().
		Str("account_id", accountID).
		Msg(" Server ListPeers called")

	peers, err := s.peerStore.ListPeers(accountID)

	log.Debug().
		Str("account_id", accountID).
		Int("peer_count", len(peers)).
		Err(err).
		Msg(" PeerStore ListPeers result")

	if err != nil || len(peers) == 0 {
		return peers, err
	}

	// 1. Redis is the SOURCE OF TRUTH for online status (always checked)
	s.enrichPeersOnlineFromRedis(peers)

	// 2. Local WireGuard device provides supplementary stats (rx/tx, endpoint, handshake)
	device, devErr := s.wgMgr.GetDevice(accountID)
	if devErr == nil && device != nil {
		statusMap, statusErr := device.GetAllPeersStatus()
		if statusErr == nil {
			for _, peer := range peers {
				if status, ok := statusMap[peer.ID]; ok {
					// Overlay detailed stats from local device
					if !status.LastHandshakeTime.IsZero() {
						peer.LastHandshakeTime = status.LastHandshakeTime
					}
					if status.Endpoint != "" {
						peer.Endpoint = status.Endpoint
					}
					peer.RxBytes = status.RxBytes
					peer.TxBytes = status.TxBytes
				}
			}
		}
	}

	return peers, err
}

// GetPeer retrieves a specific peer's metadata.
func (s *Server) GetPeer(accountID, peerID string) (*PeerMetadata, error) {
	return s.peerStore.GetPeer(accountID, peerID)
}

// FindPeer resolves a peer globally by peer ID, regardless of owning account.
func (s *Server) FindPeer(peerID string) (*PeerMetadata, error) {
	return s.peerStore.FindPeer(peerID)
}

// UpdatePeer updates a peer's metadata.
func (s *Server) UpdatePeer(peer *PeerMetadata) error {
	if err := s.peerStore.SavePeer(peer); err != nil {
		return fmt.Errorf("failed to save peer metadata: %w", err)
	}
	s.upsertTenantDNSPeer(peer)
	s.invalidateTenantDNS(peer.AccountID)

	log.Debug().
		Str("account_id", peer.AccountID).
		Str("peer_id", peer.ID).
		Msg("Updated peer metadata")

	return nil
}

// UpdatePeerStatus updates a peer's online status.
//
// Truth model:
//   - When this hub owns the peer's tenant device, the WireGuard device is the
//     final arbiter. Redis can briefly report a peer "online" while its data
//     plane is already broken (NAT rebind landing on a stale receiver index,
//     per-tenant queue overflow, mid-rekey collisions). In that window the
//     `online_peer:{id}` key is still alive but no recent authenticated traffic
//     reaches the device — so device.IsOnline (handshake/auth-packet within
//     peerOnlineThreshold AND endpoint present) is what we report.
//   - When this hub does NOT own the device for the account (cross-hub), we
//     fall back to Redis since the authoritative hub is the one publishing.
//
// Returns true if peer is online, and any error.
func (s *Server) UpdatePeerStatus(accountID, peerID string) (bool, error) {
	// Get peer metadata
	peer, err := s.peerStore.GetPeer(accountID, peerID)
	if err != nil {
		return false, fmt.Errorf("peer not found: %w", err)
	}

	// 1. Redis is the candidate signal — quick presence indicator.
	isOnline := s.isPeerOnlineViaRedis(peerID)

	// 2. If we own the tenant device locally, gate on device truth.
	device, _ := s.wgMgr.GetDevice(accountID)
	var status *userspace.PeerStatus
	if isOnline && device != nil {
		var statusErr error
		status, statusErr = device.GetPeerStatusFresh(peerID)
		if statusErr != nil {
			status, statusErr = device.GetPeerStatus(peerID)
		}
		if statusErr != nil || status == nil || !status.IsOnline {
			log.Debug().
				Str("account_id", accountID).
				Str("peer_id", peerID).
				Bool("device_has_peer", statusErr == nil && status != nil).
				Msg("Redis reports peer online but device disagrees — reporting offline")
			isOnline = false
			status = nil
		}
	}

	if isOnline {
		peer.IsOnline = true
		peer.LastSeenAt = time.Now().UTC()

		// 3. Enrich with detailed stats when we have the device locally.
		if device != nil && status != nil {
			// Update peer metadata with fresh local stats
			if !status.LastHandshakeTime.IsZero() && status.LastHandshakeTime.After(peer.LastHandshakeTime) {
				_ = s.peerStore.RecordHandshake(peer.ID, accountID, status.LastHandshakeTime, status.Endpoint)
			}
			peer.LastHandshakeTime = status.LastHandshakeTime
			if !status.LastAuthenticatedPacketTime.IsZero() {
				peer.LastSeenAt = status.LastAuthenticatedPacketTime.UTC()
			}
			peer.Endpoint = status.Endpoint
			peer.RxBytes = status.RxBytes
			peer.TxBytes = status.TxBytes

			// Track first time online for notification eligibility
			if peer.FirstSeenOnline.IsZero() {
				peer.FirstSeenOnline = time.Now().UTC()
				peer.LastOnlineAt = peer.FirstSeenOnline
			}

			// Auto port scan (only on local device)
			nowUTC := time.Now().UTC()
			shouldScan := peer.LastPortScanTime.IsZero() || nowUTC.Sub(peer.LastPortScanTime.UTC()) > 5*time.Minute
			if shouldScan {
				if !s.acquireScanLock(accountID, peerID, "periodic") {
					log.Debug().Str("peer_id", peerID).Msg("Scan already in progress (locked), skipping auto-scan")
				} else {
					log.Debug().
						Str("account_id", accountID).
						Str("peer_id", peerID).
						Str("peer_ip", peer.AssignedIP).
						Bool("first_scan", peer.LastPortScanTime.IsZero()).
						Msg(" Running port scan for peer")

					go func() {
						defer s.releaseScanLock(accountID, peerID)
						result, err := s.scanPeerPortsInternal(accountID, peerID, device, peer.AssignedIP, s.commonPorts)
						if err != nil {
							log.Warn().Err(err).Str("peer_id", peerID).Msg("Port scan failed during status update")
							return
						}

						sshPort := 0
						winboxPort := 0
						sshPriority := 5
						winboxPriority := 5

						for _, portResult := range result.Ports {
							if portResult.State == "open" {
								service := strings.ToLower(portResult.Service)
								port := portResult.Port

								if strings.Contains(service, "ssh") {
									prio := 3
									if port == 22 {
										prio = 1
									} else if port < 1024 {
										prio = 2
									} else if port >= 49152 {
										prio = 4
									}
									if prio < sshPriority && prio < 4 {
										sshPort = port
										sshPriority = prio
									}
								}

								if strings.Contains(service, "winbox") {
									prio := 3
									if port == 8291 {
										prio = 1
									} else if port < 1024 {
										prio = 2
									} else if port >= 49152 {
										prio = 4
									}
									if prio < winboxPriority && prio < 4 {
										winboxPort = port
										winboxPriority = prio
									}
								}
							}
						}

						peer.LastPortScanTime = time.Now().UTC()
						peer.ScannedSSHPort = sshPort
						peer.ScannedWinboxPort = winboxPort
						peer.HasWinbox = winboxPort > 0

						openPortsOnly := make([]*userspace.PortResult, 0)
						for _, pr := range result.Ports {
							pr.Service = strings.ToValidUTF8(strings.ReplaceAll(pr.Service, "\x00", ""), "")
							pr.Banner = strings.ToValidUTF8(strings.ReplaceAll(pr.Banner, "\x00", ""), "")
							if pr.State == "open" || (pr.Protocol == "udp" && pr.State == "open|filtered" && pr.Service != "" && !strings.Contains(pr.Service, "unknown")) {
								openPortsOnly = append(openPortsOnly, pr)
							}
						}
						result.Ports = openPortsOnly

						if resultJSON, err := json.Marshal(result); err == nil {
							peer.LastPortScan = time.Now().UTC()
							peer.CachedPortScanJSON = resultJSON
						}

						if err := s.peerStore.SavePeer(peer); err != nil {
							log.Warn().Err(err).Msg("Failed to save port scan results")
						}
					}()
				}
			}

			// Save updated metadata with fresh stats
			if err := s.peerStore.SavePeer(peer); err != nil {
				return true, fmt.Errorf("failed to save peer status: %w", err)
			}
		}

		// Publish online event
		s.PublishPeerStatusEvent(accountID, peerID, true)

		return true, nil
	}

	// 4. Truly offline (Redis says offline, or device contradicts Redis).
	if peer.IsOnline {
		peer.IsOnline = false
		peer.LastSeenAt = time.Now().UTC()
		if err := s.peerStore.SavePeer(peer); err != nil {
			return false, fmt.Errorf("failed to save peer status: %w", err)
		}
		s.PublishPeerStatusEvent(accountID, peerID, false)
	}

	return false, nil
}

// ResolvePeerDevice resolves a peer's tenant device and IP for direct operations.
func (s *Server) ResolvePeerDevice(accountID, peerID string) (*userspace.TenantDevice, string, error) {
	peer, err := s.peerStore.GetPeer(accountID, peerID)
	if err != nil {
		return nil, "", fmt.Errorf("peer not found: %w", err)
	}
	peerIP := strings.TrimSuffix(peer.AssignedIP, "/32")

	s.mu.RLock()
	device, ok := s.tenantDevices[accountID]
	s.mu.RUnlock()
	if !ok {
		return nil, "", fmt.Errorf("tenant device not found for account %s", accountID)
	}
	return device, peerIP, nil
}

// PingPeer pings a peer and returns statistics (userspace mode only).
func (s *Server) PingPeer(accountID, peerID string, count, timeoutMs int) (*userspace.PingResult, error) {
	// Get peer metadata
	peer, err := s.peerStore.GetPeer(accountID, peerID)
	if err != nil {
		return nil, fmt.Errorf("peer not found: %w", err)
	}

	// Extract peer IP
	peerIP := strings.TrimSuffix(peer.AssignedIP, "/32")

	// Get the tenant device
	s.mu.RLock()
	device, ok := s.tenantDevices[accountID]
	s.mu.RUnlock()

	// Check if local
	isLocal := false
	if ok {
		status, err := device.GetPeerStatus(peerID)
		if err == nil && status != nil && status.IsOnline && status.Endpoint != "" {
			isLocal = true
		}
	}

	// 1. Local execution
	if isLocal {
		result, err := device.ICMPPing(peerIP, count, timeoutMs)
		if err != nil {
			return nil, fmt.Errorf("ping failed: %w", err)
		}
		return result, nil
	}

	// 2. Distributed execution (Multi-core)
	if s.redisClient != nil {
		ctx := context.Background()
		hubID, err := s.redisClient.Get(ctx, fmt.Sprintf("online_peer:%s", peerID)).Result()
		if err != nil && err != redis.Nil {
			log.Warn().Err(err).Str("peer_id", peerID).Msg("Failed to resolve peer route from Redis for ping")
		}
		if hubID != "" {
			reqID := uuid.New().String()
			replyCh := fmt.Sprintf("peer:ping:response:%s", reqID)

			// Subscribe FIRST
			pubsub := s.redisClient.Subscribe(ctx, replyCh)
			defer pubsub.Close()
			if _, err := pubsub.Receive(ctx); err != nil {
				return nil, fmt.Errorf("distributed ping subscribe failed: %w", err)
			}

			// Publish Request
			req := PingRequest{
				ReqID:     reqID,
				AccountID: accountID,
				PeerID:    peerID,
				Count:     count,
				TimeoutMs: timeoutMs,
				ReplyTo:   replyCh,
			}
			reqData, _ := json.Marshal(req)
			if err := s.redisClient.Publish(ctx, targetedPingRequestChannel(hubID), reqData).Err(); err != nil {
				return nil, fmt.Errorf("distributed ping publish failed: %w", err)
			}

			// Wait for response with timeout
			// Total timeout = (count * timeout) + overhead
			totalTimeout := time.Duration(count*timeoutMs)*time.Millisecond + 2*time.Second
			select {
			case msg := <-pubsub.Channel():
				var resp PingResponse
				if err := json.Unmarshal([]byte(msg.Payload), &resp); err == nil {
					if resp.Error != "" {
						return nil, fmt.Errorf("remote ping error: %s", resp.Error)
					}
					return resp.Result, nil
				}
			case <-time.After(totalTimeout):
				return nil, fmt.Errorf("distributed ping timeout")
			}
		}
	}

	return nil, fmt.Errorf("peer offline or not found on any core")
}

// ScanPeerPorts scans ports on a peer's IP address with optimized performance.
// Results are cached for 5 minutes to avoid excessive scanning.
// Maximum scan time is limited to 2 minutes for full scans.
// Duplicate scans for the same peer are rejected while a scan is in progress.
func (s *Server) ScanPeerPorts(accountID, peerID string, ports []int) (*userspace.ScanResult, error) {
	// Check if a scan is already in progress for this peer (using Redis lock)
	if !s.acquireScanLock(accountID, peerID, "manual") {
		return nil, fmt.Errorf("port scan already in progress for this peer")
	}

	// Ensure we clean up the scan lock when done
	defer s.releaseScanLock(accountID, peerID)

	// Get peer metadata
	peer, err := s.peerStore.GetPeer(accountID, peerID)
	if err != nil {
		return nil, fmt.Errorf("peer not found: %w", err)
	}

	// Check cache first (valid for 5 minutes, using UTC to avoid timezone issues)
	nowUTC := time.Now().UTC()
	cacheExpiry := nowUTC.Add(-5 * time.Minute)
	if peer.LastPortScan.UTC().After(cacheExpiry) && len(peer.CachedPortScanJSON) > 0 {
		var cachedResult userspace.ScanResult
		if err := json.Unmarshal(peer.CachedPortScanJSON, &cachedResult); err == nil {
			log.Debug().
				Str("account_id", accountID).
				Str("peer_id", peerID).
				Dur("cache_age", nowUTC.Sub(peer.LastPortScan.UTC())).
				Msg("📦 Returning cached port scan result")
			return &cachedResult, nil
		}
	}

	// Extract peer IP (remove /32 suffix if present)
	peerIP := strings.TrimSuffix(peer.AssignedIP, "/32")

	// Get the tenant device for network access
	device, err := s.wgMgr.GetDevice(accountID)
	if err != nil {
		return nil, fmt.Errorf("failed to get tenant device: %w", err)
	}

	// Create scanner with high concurrency and timeout
	// 200 workers + 500ms timeout per port for slow/high-latency hosts
	scanner := userspace.NewPortScannerWithNet(device.Net, 200, 500*time.Millisecond)
	defer scanner.Stop()

	// Define Redis channel for this peer scan
	redisChannel := fmt.Sprintf("tenant:%s:peer:%s:scan_progress", accountID, peerID)

	// Publish scan started event
	if s.redisClient != nil {
		startEvent := map[string]any{
			"type":       "scan_started",
			"peer_id":    peerID,
			"port_count": len(ports),
			"timestamp":  time.Now().UnixMilli(),
		}
		if payload, err := json.Marshal(startEvent); err == nil {
			s.redisClient.Publish(context.Background(), redisChannel, payload)
		}
	}

	// Setup progress callback
	totalPorts := len(ports)
	if totalPorts == 0 {
		totalPorts = 65535 // approximate for full scan
	}

	// Track found ports for progress updates
	var foundCount int32

	scanner.OnProgress = func(count int, currentPort int, found bool) {
		if s.redisClient == nil {
			return
		}

		isFound := found
		if isFound {
			// Increment atomically if concurrency requires, but this callback is serialized by collector
			foundCount++
		}

		// Throttle updates: only publish every 1% or if a port is found
		if isFound || count%max(1, totalPorts/100) == 0 || count == totalPorts {
			progressPercent := float64(count) / float64(totalPorts) * 100
			if progressPercent > 100 {
				progressPercent = 100
			}

			event := map[string]any{
				"type":          "scan_progress",
				"peer_id":       peerID,
				"scanned_count": count,
				"total_ports":   totalPorts,
				"progress":      progressPercent,
				"current_port":  currentPort,
				"found":         isFound,
				"found_count":   foundCount,
			}

			if payload, err := json.Marshal(event); err == nil {
				s.redisClient.Publish(context.Background(), redisChannel, payload)
			}
		}
	}

	// Determine timeout based on number of ports
	// Empty ports list means full scan (65535 TCP + common UDP ports)
	scanTimeout := 30 * time.Second // Default for common ports
	if len(ports) == 0 {
		scanTimeout = 2 * time.Minute // 2 minutes for full scan (65535 TCP + UDP)
	} else if len(ports) > 1000 {
		scanTimeout = 2 * time.Minute // 2 minutes for large scans
	} else if len(ports) > 100 {
		scanTimeout = 1 * time.Minute // 1 minute for medium scans
	}

	log.Debug().
		Str("account_id", accountID).
		Str("peer_id", peerID).
		Str("peer_ip", peerIP).
		Int("port_count", len(ports)).
		Dur("timeout", scanTimeout).
		Msg(" Starting port scan")

	// Create scan result with timeout context
	ctx, cancel := context.WithTimeout(context.Background(), scanTimeout)
	defer cancel()

	// Perform the scan in goroutine to check timeout
	resultChan := make(chan *userspace.ScanResult, 1)
	errChan := make(chan error, 1)

	go func() {
		var result *userspace.ScanResult
		if len(ports) == 0 {
			// Scan ALL TCP ports (1-65535) + common UDP ports for full discovery
			result = scanner.ScanAllPortsWithUDP(peerIP)
		} else {
			// Custom port list provided (TCP only)
			result = scanner.ScanPorts(peerIP, ports)
		}
		resultChan <- result
	}()

	// Wait for scan or timeout
	var result *userspace.ScanResult
	select {
	case <-ctx.Done():
		err := fmt.Errorf("port scan timeout after %v", scanTimeout)
		// Publish failure event
		if s.redisClient != nil {
			failEvent := map[string]any{
				"type":    "scan_failed",
				"peer_id": peerID,
				"error":   err.Error(),
			}
			if payload, marshErr := json.Marshal(failEvent); marshErr == nil {
				s.redisClient.Publish(context.Background(), redisChannel, payload)
			}
		}
		return nil, err
	case err := <-errChan:
		// Publish failure event
		if s.redisClient != nil {
			failEvent := map[string]any{
				"type":    "scan_failed",
				"peer_id": peerID,
				"error":   err.Error(),
			}
			if payload, marshErr := json.Marshal(failEvent); marshErr == nil {
				s.redisClient.Publish(context.Background(), redisChannel, payload)
			}
		}
		return nil, fmt.Errorf("port scan error: %w", err)
	case result = <-resultChan:
		// Scan completed successfully
		// Publish completion event
		if s.redisClient != nil {
			completeEvent := map[string]any{
				"type":        "scan_complete",
				"peer_id":     peerID,
				"found_count": len(result.Ports),
				"timestamp":   time.Now().UnixMilli(),
			}
			if payload, err := json.Marshal(completeEvent); err == nil {
				s.redisClient.Publish(context.Background(), redisChannel, payload)
			}
		}
	}

	log.Debug().
		Str("account_id", accountID).
		Str("peer_id", peerID).
		Int("open_ports", len(result.OpenPorts())).
		Dur("duration", result.EndTime.Sub(result.StartTime)).
		Msg(" Port scan completed")

	// Filter result to only keep open ports before caching
	openPortsOnly := make([]*userspace.PortResult, 0)
	for _, pr := range result.Ports {
		if pr.State == "open" || (pr.Protocol == "udp" && pr.State == "open|filtered" && pr.Service != "" && !strings.Contains(pr.Service, "unknown")) {
			openPortsOnly = append(openPortsOnly, pr)
		}
	}
	result.Ports = openPortsOnly

	// Sanitize strings to prevent DB errors (Postgres doesn't like null bytes in JSONB)
	// Sanitize strings to prevent DB errors (Postgres doesn't like null bytes in JSONB)
	sanitizeString := func(s string) string {
		// Remove null bytes
		s = strings.ReplaceAll(s, "\x00", "")
		// Ensure valid UTF-8
		if !utf8.ValidString(s) {
			v := make([]rune, 0, len(s))
			for i, r := range s {
				if r == utf8.RuneError {
					_, size := utf8.DecodeRuneInString(s[i:])
					if size == 1 {
						continue
					}
				}
				v = append(v, r)
			}
			s = string(v)
		}
		return s
	}

	for _, p := range result.Ports {
		p.Banner = sanitizeString(p.Banner)
		p.Service = sanitizeString(p.Service)
	}
	if result.Fingerprint != nil {
		result.Fingerprint.DetectionInfo = sanitizeString(result.Fingerprint.DetectionInfo)
		result.Fingerprint.Hostname = sanitizeString(result.Fingerprint.Hostname)
		result.Fingerprint.OSFamily = sanitizeString(result.Fingerprint.OSFamily)
		result.Fingerprint.OSVersion = sanitizeString(result.Fingerprint.OSVersion)
	}

	// Cache the result for 24 hours in peer metadata (using filtered version)
	if resultJSON, err := json.Marshal(result); err == nil {
		peer.LastPortScan = time.Now().UTC()
		peer.CachedPortScanJSON = resultJSON
		if err := s.peerStore.SavePeer(peer); err != nil {
			log.Warn().Err(err).Msg("Failed to cache port scan result in peer metadata")
		}
	}

	return result, nil
}

// IsScanInProgress checks if a port scan is currently running for a peer.
func (s *Server) IsScanInProgress(accountID, peerID string) bool {
	return s.isScanRunning(accountID, peerID)
}

// GetActiveScanID returns the active scan ID for a peer if one is running.
func (s *Server) GetActiveScanID(accountID, peerID string) string {
	if s.redisClient == nil {
		return ""
	}

	if activeScanID, err := s.redisClient.Get(context.Background(), getActiveScanKey(peerID)).Result(); err == nil && activeScanID != "" {
		return activeScanID
	}

	key := s.getScanLockKey(accountID, peerID)
	val, err := s.redisClient.Get(context.Background(), key).Result()
	if err != nil || val == "" || val == "manual" || strings.HasPrefix(val, "periodic-") {
		return ""
	}
	return val
}

func (s *Server) getScanLockKey(accountID, peerID string) string {
	return fmt.Sprintf("scan:lock:%s:%s", accountID, peerID)
}

func (s *Server) acquireScanLock(accountID, peerID, scanID string) bool {
	if s.redisClient == nil {
		return false
	}
	if scanID == "" {
		scanID = "1"
	}
	key := s.getScanLockKey(accountID, peerID)
	// Set lock with 2 minute expiration (matching max scan timeout)
	success, err := s.redisClient.SetNX(context.Background(), key, scanID, 2*time.Minute).Result()
	if err != nil {
		log.Warn().Err(err).Msg("Failed to acquire scan lock in Redis")
		return false
	}
	return success
}

func (s *Server) releaseScanLock(accountID, peerID string) {
	if s.redisClient == nil {
		return
	}
	key := s.getScanLockKey(accountID, peerID)
	// Delete lock
	s.redisClient.Del(context.Background(), key)
}

func (s *Server) isScanRunning(accountID, peerID string) bool {
	if s.redisClient == nil {
		return false
	}
	key := s.getScanLockKey(accountID, peerID)
	val, _ := s.redisClient.Exists(context.Background(), key).Result()
	return val > 0
}

// scanPeerPortsInternal is an internal helper for performing port scans without cache checking.
// Used by UpdatePeerStatus for daily scheduled scans.
func (s *Server) scanPeerPortsInternal(accountID, peerID string, device *userspace.TenantDevice, peerIP string, ports []int) (*userspace.ScanResult, error) {
	// Remove /32 suffix if present
	peerIP = strings.TrimSuffix(peerIP, "/32")

	// Create scanner with high concurrency and timeout
	// 2000 workers + 200ms timeout per port for high speed
	scanner := userspace.NewPortScannerWithNet(device.Net, 2000, 200*time.Millisecond)
	defer scanner.Stop()

	// Define Redis channel for this peer scan
	redisChannel := fmt.Sprintf("tenant:%s:peer:%s:scan_progress", accountID, peerID)

	// Default to range 21-10000 if ports are empty (instead of full scan)
	// User requested to limit TCP to 21-10000 for speed
	if len(ports) == 0 {
		ports = make([]int, 0, 10000-21+1)
		for i := 21; i <= 10000; i++ {
			ports = append(ports, i)
		}
	}

	// Publish scan started event
	if s.redisClient != nil {
		startEvent := map[string]any{
			"type":       "scan_started",
			"peer_id":    peerID,
			"port_count": len(ports),
			"timestamp":  time.Now().UnixMilli(),
		}
		if payload, err := json.Marshal(startEvent); err == nil {
			s.redisClient.Publish(context.Background(), redisChannel, payload)
		}
	}

	// Setup progress callback
	totalPorts := len(ports)
	// No need for approximate fallback since we force ports range

	// Track found ports for progress updates
	var foundCount int32

	scanner.OnProgress = func(count int, currentPort int, found bool) {
		if s.redisClient == nil {
			return
		}

		isFound := found
		if isFound {
			foundCount++
		}

		// Throttle updates: only publish every 1% or if a port is found
		if isFound || count%max(1, totalPorts/100) == 0 || count >= totalPorts {
			progressPercent := float64(count) / float64(totalPorts) * 100
			if progressPercent > 100 {
				progressPercent = 100
			}

			event := map[string]any{
				"type":          "scan_progress",
				"peer_id":       peerID,
				"scanned_count": count,
				"total_ports":   totalPorts,
				"progress":      progressPercent,
				"current_port":  currentPort,
				"found":         isFound,
				"found_count":   foundCount,
			}

			if payload, err := json.Marshal(event); err == nil {
				s.redisClient.Publish(context.Background(), redisChannel, payload)
			}
		}
	}

	// Determine timeout based on number of ports
	// Force short timeout since we want "supper fast"
	scanTimeout := 1 * time.Minute

	log.Debug().
		Str("account_id", accountID).
		Str("peer_id", peerID).
		Str("peer_ip", peerIP).
		Int("port_count", len(ports)).
		Dur("timeout", scanTimeout).
		Msg(" Starting internal port scan with events")

	// Create scan result with timeout context
	ctx, cancel := context.WithTimeout(context.Background(), scanTimeout)
	defer cancel()

	// Perform the scan in goroutine to check timeout
	resultChan := make(chan *userspace.ScanResult, 1)

	go func() {
		var result *userspace.ScanResult
		// Always use ScanPorts (TCP only) - skipping UDP as requested
		// if len(ports) == 0 { ... } logic handled above by proactive filling
		result = scanner.ScanPorts(peerIP, ports)
		resultChan <- result
	}()

	// Wait for scan or timeout
	var result *userspace.ScanResult
	select {
	case <-ctx.Done():
		err := fmt.Errorf("port scan timeout after %v", scanTimeout)
		// Publish failure event
		if s.redisClient != nil {
			failEvent := map[string]any{
				"type":    "scan_failed",
				"peer_id": peerID,
				"error":   err.Error(),
			}
			if payload, marshErr := json.Marshal(failEvent); marshErr == nil {
				s.redisClient.Publish(context.Background(), redisChannel, payload)
			}
		}
		return nil, err
	case result = <-resultChan:
		// Scan completed successfully
		// Publish completion event
		if s.redisClient != nil {
			completeEvent := map[string]any{
				"type":        "scan_complete",
				"peer_id":     peerID,
				"found_count": len(result.Ports),
				"timestamp":   time.Now().UnixMilli(),
			}
			if payload, err := json.Marshal(completeEvent); err == nil {
				s.redisClient.Publish(context.Background(), redisChannel, payload)
			}
		}
	}

	log.Debug().
		Str("account_id", accountID).
		Str("peer_id", peerID).
		Int("open_ports", len(result.OpenPorts())).
		Dur("duration", result.EndTime.Sub(result.StartTime)).
		Msg(" Internal port scan completed")

	return result, nil
}

// GetPeerConfig regenerates WireGuard configuration for a peer.
func (s *Server) GetPeerConfig(accountID, peerID, endpoint string) (string, error) {
	// Get peer metadata
	peer, err := s.peerStore.GetPeer(accountID, peerID)
	if err != nil {
		return "", fmt.Errorf("peer not found: %w", err)
	}

	// Get account for subnet info
	acc, err := s.accountMgr.GetAccount(accountID)
	if err != nil {
		return "", fmt.Errorf("account not found: %w", err)
	}

	var serverPublicKey wgtypes.Key
	var listenPort int

	// Get device to retrieve server public key and port
	device, err := s.wgMgr.GetDevice(accountID)
	if err != nil {
		return "", fmt.Errorf("failed to get userspace device: %w", err)
	}
	serverPublicKey = device.PublicKey
	listenPort = device.GetEndpointPort() // Use endpoint port (shared port in shared mode)

	// Use provided endpoint or default
	if endpoint == "" {
		// Get effective endpoint (fallback to localhost if not configured)
		endpoint = s.getEffectiveEndpoint()
	}
	endpoint = formatPeerEndpoint(endpoint, listenPort)

	// Ensure peer address has /32 suffix
	peerAddress := peer.AssignedIP
	if !strings.Contains(peerAddress, "/") {
		peerAddress += "/32"
	}
	allowedIPs, err := WireGuardAllowedIPs(acc.Networks)
	if err != nil {
		return "", fmt.Errorf("failed to derive tenant routes: %w", err)
	}

	return BuildWireGuardConfig(WireGuardConfigOptions{
		PrivateKey:          peer.PrivateKey,
		Address:             peerAddress,
		ServerPublicKey:     serverPublicKey.String(),
		Endpoint:            endpoint,
		AllowedIPs:          allowedIPs,
		DNSServers:          WireGuardDNSServers(device.DeviceIP.String()),
		PersistentKeepalive: 25,
	}), nil
}

func formatPeerEndpoint(endpoint string, listenPort int) string {
	if endpoint == "" {
		return ""
	}
	if _, _, err := net.SplitHostPort(endpoint); err == nil {
		return endpoint
	}
	if strings.Count(endpoint, ":") > 1 && !strings.HasPrefix(endpoint, "[") {
		return fmt.Sprintf("[%s]:%d", endpoint, listenPort)
	}
	return fmt.Sprintf("%s:%d", endpoint, listenPort)
}

func (s *Server) ensureTenantDNS(accountID string, device *userspace.TenantDevice) {
	s.tenantDNSMu.Lock()
	defer s.tenantDNSMu.Unlock()

	if existing, ok := s.tenantDNS[accountID]; ok {
		if existing.device == device {
			return
		}
		existing.stop()
		delete(s.tenantDNS, accountID)
	}

	dnsSrv, err := newTenantDNSServer(s.ctx, accountID, device, s.listTenantDNSPeers)
	if err != nil {
		log.Warn().
			Err(err).
			Str("account_id", accountID).
			Msg("Failed to start tenant overlay DNS")
		return
	}
	s.tenantDNS[accountID] = dnsSrv
}

func (s *Server) stopTenantDNS(accountID string) {
	s.tenantDNSMu.Lock()
	defer s.tenantDNSMu.Unlock()

	if dnsSrv, ok := s.tenantDNS[accountID]; ok {
		dnsSrv.stop()
		delete(s.tenantDNS, accountID)
	}
}

func (s *Server) listTenantDNSPeers(accountID string) []tenantDNSPeerRecord {
	s.tenantDNSPeersMu.RLock()
	defer s.tenantDNSPeersMu.RUnlock()

	records := s.tenantDNSPeers[accountID]
	if len(records) == 0 {
		return nil
	}

	result := make([]tenantDNSPeerRecord, 0, len(records))
	for _, record := range records {
		result = append(result, record)
	}
	return result
}

func (s *Server) upsertTenantDNSPeer(peer *PeerMetadata) {
	if peer == nil || peer.AccountID == "" || peer.ID == "" {
		return
	}

	s.tenantDNSPeersMu.Lock()
	defer s.tenantDNSPeersMu.Unlock()

	if s.tenantDNSPeers[peer.AccountID] == nil {
		s.tenantDNSPeers[peer.AccountID] = make(map[string]tenantDNSPeerRecord)
	}
	s.tenantDNSPeers[peer.AccountID][peer.ID] = tenantDNSPeerRecord{
		PeerID:     peer.ID,
		Name:       peer.Name,
		AssignedIP: peer.AssignedIP,
	}
}

func (s *Server) removeTenantDNSPeer(accountID, peerID string) {
	if accountID == "" || peerID == "" {
		return
	}

	s.tenantDNSPeersMu.Lock()
	defer s.tenantDNSPeersMu.Unlock()

	if peers := s.tenantDNSPeers[accountID]; peers != nil {
		delete(peers, peerID)
		if len(peers) == 0 {
			delete(s.tenantDNSPeers, accountID)
		}
	}
}

func (s *Server) resetTenantDNSPeers(accountID string, peers []*PeerMetadata) {
	s.tenantDNSPeersMu.Lock()
	defer s.tenantDNSPeersMu.Unlock()

	if len(peers) == 0 {
		delete(s.tenantDNSPeers, accountID)
		return
	}

	records := make(map[string]tenantDNSPeerRecord, len(peers))
	for _, peer := range peers {
		if peer == nil || peer.ID == "" {
			continue
		}
		records[peer.ID] = tenantDNSPeerRecord{
			PeerID:     peer.ID,
			Name:       peer.Name,
			AssignedIP: peer.AssignedIP,
		}
	}

	if len(records) == 0 {
		delete(s.tenantDNSPeers, accountID)
		return
	}
	s.tenantDNSPeers[accountID] = records
}

func (s *Server) clearTenantDNSPeers(accountID string) {
	s.tenantDNSPeersMu.Lock()
	defer s.tenantDNSPeersMu.Unlock()
	delete(s.tenantDNSPeers, accountID)
}

func (s *Server) invalidateTenantDNS(accountID string) {
	s.tenantDNSMu.Lock()
	defer s.tenantDNSMu.Unlock()

	if dnsSrv, ok := s.tenantDNS[accountID]; ok {
		dnsSrv.invalidate()
	}
}

func (s *Server) stopAllTenantDNS() {
	s.tenantDNSMu.Lock()
	defer s.tenantDNSMu.Unlock()

	for accountID, dnsSrv := range s.tenantDNS {
		dnsSrv.stop()
		delete(s.tenantDNS, accountID)
	}
}

func (s *Server) refreshTrackedTenantDevice(accountID string) {
	device, err := s.wgMgr.GetDevice(accountID)
	if err != nil {
		log.Warn().Err(err).Str("account_id", accountID).Msg("Failed to refresh tracked tenant device")
		return
	}

	s.mu.Lock()
	s.tenantDevices[accountID] = device
	s.mu.Unlock()
	s.ensureTenantDNS(accountID, device)
}

// GetDeviceStats returns statistics for a tenant's device.
func (s *Server) GetDeviceStats(accountID string) (*userspace.DeviceStats, error) {
	return s.wgMgr.GetStats(accountID)
}

// GetGlobalStats returns aggregated statistics.
func (s *Server) GetGlobalStats() *userspace.GlobalStats {
	return s.wgMgr.GetGlobalStats()
}

// ServerConfig is the public config interface for gRPC layer
type ServerConfig struct {
	Network struct {
		SubnetPools []string
		SharedPort  int
	}
}

// GetConfig returns the server configuration for the gRPC layer.
func (s *Server) GetConfig() *ServerConfig {
	if s.config == nil {
		return nil
	}
	cfg := &ServerConfig{}
	cfg.Network.SubnetPools = s.config.SubnetPools
	cfg.Network.SharedPort = s.config.SharedPort
	return cfg
}

// WUSPCtrl returns the WUSP controller for use by the gRPC service layer.
func (s *Server) WUSPCtrl() *wuspcontroller.WUSPController {
	return s.wuspCtrl
}

// handleWUSPNotify is called by the WUSP controller when a device sends an
// unsolicited Notify message. It publishes the event to the Redis channel
// tenant:<accountID>:peer:<peerID>:wusp so the portal proxy can forward it
// to subscribed WebSocket clients in real time.
func (s *Server) handleWUSPNotify(peerPublicKey string, resp wusp.USPAgentResponse) {
	// Resolve the account ID via the WUSP state repo — PeerID IS the WireGuard public key.
	state, err := s.wuspCtrl.StateRepo().GetByPeer(peerPublicKey)
	if err != nil || state == nil {
		// Any unsolicited Notify from an unknown peer likely means the device just
		// booted and sent its first message. Attempt bootstrap sync.
		log.Info().Str("peer", peerPublicKey).Msg("wusp: notify from unknown peer — triggering bootstrap sync")
		go s.bootstrapWUSPPeer(peerPublicKey, nil)
		return
	}

	// Build a compact JSON representation of the notification.
	type paramKV struct {
		Path  string `json:"path"`
		Value string `json:"value"`
	}
	var params []paramKV
	if resp.Message != nil {
		for _, f := range resp.Message.Fields {
			params = append(params, paramKV{Path: f.Path, Value: f.Val.AsString()})
		}
	}

	payload, err := json.Marshal(map[string]any{
		"type":       "wusp_notify",
		"peer_id":    peerPublicKey,
		"account_id": state.AccountID,
		"event_path": resp.ObjectPath,
		"params":     params,
		"timestamp":  time.Now().Unix(),
	})
	if err != nil {
		return
	}

	channel := fmt.Sprintf("tenant:%s:peer:%s:wusp", state.AccountID, peerPublicKey)
	if err := s.redisClient.Publish(s.ctx, channel, payload).Err(); err != nil {
		log.Debug().Err(err).Str("channel", channel).Msg("wusp: failed to publish notify event")
	}
}

// handleWUSPEvent is called by the WUSP controller when the agent sends a
// decoded structured event (ValueChange, Boot!, OnBoardRequest, etc.).
// It publishes a richer JSON payload to the Redis channel
// tenant:<accountID>:peer:<peerPublicKey>:wusp so the portal proxy can forward
// it to subscribed WebSocket clients in real time.
func (s *Server) handleWUSPEvent(peerPublicKey string, event wusp.USPEvent) {
	isOnBoard := event.Type == wusp.USPEventTypeOnBoardRequest || event.EventName == "Boot!"

	log.Info().Str("peer", peerPublicKey).
		Str("event", event.EventName).
		Uint8("type", uint8(event.Type)).
		Bool("onboard", isOnBoard).
		Msg("wusp: received agent event")

	// For OnBoardRequest, bootstrap only on first sight (or after a long gap).
	// Agents typically re-announce on every reconnect — if we re-bootstrap each
	// time we burn DB writes + stale-flag SyncDeviceState calls. Skip when we
	// already have a device state row that was synced recently.
	if isOnBoard {
		existing, _ := s.wuspCtrl.StateRepo().GetByPeer(peerPublicKey)
		shouldBootstrap := existing == nil ||
			existing.LastSyncAt.IsZero() ||
			time.Since(existing.LastSyncAt) > 10*time.Minute
		if shouldBootstrap {
			go s.bootstrapWUSPPeer(peerPublicKey, event.OnBoard)
		} else {
			log.Debug().Str("peer", peerPublicKey).
				Time("last_sync", existing.LastSyncAt).
				Msg("wusp: skipping re-bootstrap — device already known and recently synced")
		}
	}

	// Resolve the account ID via the WUSP state repo.
	state, err := s.wuspCtrl.StateRepo().GetByPeer(peerPublicKey)
	if err != nil || state == nil {
		return // bootstrapWUSPPeer handles unknown peers
	}

	payload, err := json.Marshal(map[string]any{
		"type":            "wusp_notify",
		"peer_id":         peerPublicKey,
		"account_id":      state.AccountID,
		"event_type":      uint8(event.Type),
		"event_name":      event.EventName,
		"obj_path":        event.ObjPath,
		"subscription_id": event.SubscriptionID,
		"params":          event.Params,
		"param_value":     event.ParamValue,
		"timestamp":       time.Now().Unix(),
	})
	if err != nil {
		return
	}

	channel := fmt.Sprintf("tenant:%s:peer:%s:wusp", state.AccountID, peerPublicKey)
	if err := s.redisClient.Publish(s.ctx, channel, payload).Err(); err != nil {
		log.Debug().Err(err).Str("channel", channel).Msg("wusp: failed to publish event")
	}
}

// handlePeerSessionConfirmed is called when ReceivedWithKeypair returns true on
// the controller — meaning both the controller AND the agent are now using the
// same new WireGuard keypair. For known WUSP devices, re-sync state.
// Unknown peers are NOT probed — the agent announces itself via OnBoardRequest
// which triggers bootstrapWUSPPeer through handleWUSPEvent.
//
// This fires on EVERY WireGuard rekey (~2 min per peer). At scale (millions of
// devices), the rate limiter prevents DB and sync thrashing by allowing at most
// one SyncDeviceState per peer per 10 minutes. The agent's periodic OnBoardRequest
// (every 2 min) handles reconnects that fall within the rate-limit window.
func (s *Server) handlePeerSessionConfirmed(tenantID, peerPublicKey string) {
	if s.wuspCtrl == nil || s.redisClient == nil {
		return
	}
	// Rate-limit: at most one re-sync per 10 minutes per peer.
	// WireGuard rekeys every ~2 min, so without this we'd run 5× more syncs
	// than needed. The agent's OnBoardRequest handles reconnects within the window.
	syncKey := "wusp_resync:" + peerPublicKey
	if set, _ := s.redisClient.SetNX(context.Background(), syncKey, "1", 10*time.Minute).Result(); !set {
		return
	}
	// Fast check: only proceed if this peer has WUSP state.
	existing, _ := s.wuspCtrl.StateRepo().GetByPeer(peerPublicKey)
	if existing == nil {
		s.redisClient.Del(context.Background(), syncKey) // don't burn the window for non-WUSP peers
		return
	}
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		if err := s.wuspCtrl.SyncDeviceState(ctx, peerPublicKey, tenantID); err != nil {
			log.Debug().Err(err).Str("peer", peerPublicKey).Msg("wusp: re-sync on session confirmed failed")
			s.redisClient.Del(context.Background(), syncKey) // allow retry on next rekey
		}
	}()
}

// bootstrapWUSPPeer is called when a Boot! or OnBoardRequest event arrives for
// a peer that has no WUSP state in the database yet. It resolves the accountID
// (from Redis fast-path or DB fallback) and runs SyncDeviceState to create the
// initial state row.
func (s *Server) bootstrapWUSPPeer(peerPublicKey string, onboard *wusp.USPOnBoardInfo) {
	if s.wuspCtrl == nil {
		return
	}

	// Fast path: accountID cached in Redis by handlePeerActive.
	var accountID string
	if s.redisClient != nil {
		if val, err := s.redisClient.Get(context.Background(), "wusp_peer_account:"+peerPublicKey).Result(); err == nil {
			accountID = val
		}
	}

	// Slow path: find via peer repository.
	if accountID == "" {
		peer, err := store.DB().Peers().FindByPeerID(peerPublicKey)
		if err != nil || peer == nil {
			log.Warn().Str("peer", peerPublicKey).Msg("wusp: bootstrap: peer not found, cannot sync")
			return
		}
		accountID = peer.AccountID
	}

	log.Info().Str("peer", peerPublicKey).Str("account", accountID).Msg("wusp: bootstrapping new WUSP device from Boot!/OnBoardRequest")

	// Seed a minimal DB row immediately from OnBoardRequest identity fields so
	// the device is visible in the portal before GetAll completes (or if it
	// never does because the agent only pushes events).
	if onboard != nil {
		seed := &store.WUSPDeviceStateData{
			PeerID:          peerPublicKey,
			AccountID:       accountID,
			Manufacturer:    onboard.Manufacturer,
			ProductClass:    onboard.ProductClass,
			SerialNumber:    onboard.SerialNumber,
			SoftwareVersion: onboard.SoftwareVersion,
			WUSPEnable:      true, // device sent OnBoardRequest — it is WUSP-capable
			WUSPStatus:      "Active",
			WUSPVersion:     onboard.AgentSupportedProtocolVersions,
			LastSyncAt:      time.Now(),
			UpdatedAt:       time.Now(),
		}
		if err := s.wuspCtrl.StateRepo().Upsert(seed); err != nil {
			log.Warn().Err(err).Str("peer", peerPublicKey).Msg("wusp: bootstrap: seed upsert failed")
		} else {
			log.Info().Str("peer", peerPublicKey).Str("product_class", onboard.ProductClass).Str("serial", onboard.SerialNumber).Msg("wusp: seeded device state from OnBoardRequest")
		}
		// Mark peer as wantasticd so the portal UI shows WUSP-specific actions immediately
		// (without waiting for the legacy stats-based detection).
		if err := store.DB().Peers().UpdatePeerAgentInfo(accountID, peerPublicKey, "wantasticd", onboard.SoftwareVersion); err != nil {
			log.Debug().Err(err).Str("peer", peerPublicKey).Msg("wusp: bootstrap: failed to mark peer as wantasticd")
		}
	}

	// SyncDeviceState issues a Get to the peer; the Get itself is bounded by
	// controller.RequestTimeout (default 15s). We allow 2× headroom here for
	// the post-Get persistence steps (DB write, snapshot extraction).
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := s.wuspCtrl.SyncDeviceState(ctx, peerPublicKey, accountID); err != nil {
		// ErrPeerNotReachable is expected when bootstrap fires before the
		// agent's first OnBoardRequest endpoint registration completes — log
		// at DEBUG level since the next re-announce cycle will succeed.
		if errors.Is(err, wuspcontroller.ErrPeerNotReachable) {
			log.Debug().Str("peer", peerPublicKey).Msg("wusp: bootstrap deferred (peer not yet reachable)")
		} else {
			log.Warn().Err(err).Str("peer", peerPublicKey).Msg("wusp: bootstrap sync failed (device may be push-only)")
		}
	}
}

// GetSessionCounts returns the count of active SSH and Winbox sessions.
func (s *Server) GetSessionCounts() (sshCount int, winboxCount int) {
	if s.directSSHHandler != nil {
		sshCount = s.directSSHHandler.GetActiveSessionCount()
	}
	if s.winboxMgr != nil {
		winboxCount = s.winboxMgr.GetActiveSessionCount()
	}
	return sshCount, winboxCount
}

// GetWireGuardManager returns the WireGuard device manager
func (s *Server) GetWireGuardManager() *userspace.UserspaceManager {
	return s.wgMgr
}

// GetServerEndpoint returns the configured server endpoint
func (s *Server) GetServerEndpoint() string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.serverEndpoint
}

// getEffectiveEndpoint returns the configured endpoint or "localhost" for dev
// This is used internally - the gRPC layer handles WAN IP detection
func (s *Server) getEffectiveEndpoint() string {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if s.config.ServerEndpoint == "" {
		return "localhost"
	}
	return s.serverEndpoint
}

// Close shuts down the server gracefully.
func (s *Server) Close() error {
	log.Debug().Msg("Shutting down server")

	// Cancel context to signal goroutines to stop
	s.cancel()
	s.stopAllTenantDNS()

	// Wait for all background goroutines to finish
	s.wg.Wait()

	// Close WebProxy handler (closes all sessions, stops cleanup goroutine)
	if s.webProxyHandler != nil {
		s.webProxyHandler.Shutdown()
	}

	// Stop WUSP controller background sweep goroutine.
	if s.wuspCtrl != nil {
		s.wuspCtrl.Stop()
	}

	// Close WireGuard manager
	if err := s.wgMgr.Close(); err != nil {
		log.Error().Err(err).Msg("Error closing userspace WireGuard manager")
	}

	log.Debug().Msg("Server shutdown complete")
	return nil
}

// PeerInfo contains information for configuring a WireGuard peer.
type PeerInfo struct {
	Name            string
	PrivateKey      string
	PublicKey       string
	AllowedIPs      []string
	ServerPublicKey string
	ServerEndpoint  string
}

func (s *Server) GetHandshakeHistory(accountID, peerID string, since time.Time) ([]store.PeerHandshakeData, error) {
	return s.peerStore.GetHandshakeHistory(accountID, peerID, since)
}

// ============================================================================
// ACL Management
// ============================================================================
type ACLRule struct {
	ID        string
	AccountID string
	Action    string // "allow" or "deny"
	Protocol  string // e.g., "tcp", "udp", "icmp"
	SourceIP  string // CIDR notation
	DestIP    string // CIDR notation
	DestPort  int    // Destination port
	Priority  int    // Lower number = higher priority
}

// AddACLRule adds an access control rule for a tenant network.
func (s *Server) AddACLRule(rule *ACLRule) error {
	// Verify account exists

	log.Debug().
		Str("account_id", rule.AccountID).
		Str("rule_id", rule.ID).
		Str("action", rule.Action).
		Str("protocol", rule.Protocol).
		Int("priority", rule.Priority).
		Msg("Added ACL rule")

	return nil
}

// RemoveACLRule removes an access control rule.
func (s *Server) RemoveACLRule(accountID, ruleID string) error {
	log.Debug().
		Str("account_id", accountID).
		Str("rule_id", ruleID).
		Msg("Removed ACL rule")

	return nil
}

// GetACLRules returns all ACL rules for a tenant.
func (s *Server) GetACLRules(accountID string) []*ACLRule {
	return nil
}

// CheckACLAccess checks if a packet is allowed by ACL rules.
func (s *Server) CheckACLAccess(accountID, protocol, srcIP, dstIP string, dstPort int) (bool, string) {
	return true, "allowed by default"
}

// ============================================================================
// WebSSH Management (Direct SSH with Multiplexing)
// ============================================================================

// CreateWebSSHSession creates a new WebSSH session with SSH credentials.
// Uses SSH multiplexing for efficient multi-terminal support.
// If peerID is provided (not empty), peer metadata will be updated after successful connections.
func (s *Server) CreateWebSSHSession(tenantID, peerID, peerIP string, sshPort int, username, password, privateKey, privateKeyPassphrase, userAgent string, rows, cols int) (string, error) {
	log.Debug().
		Str("tenant_id", tenantID).
		Str("peer_id", peerID).
		Str("peer_ip", peerIP).
		Str("username", username).
		Int("ssh_port", sshPort).
		Bool("password_provided", password != "").
		Bool("private_key_provided", privateKey != "").
		Bool("private_key_passphrase_provided", privateKeyPassphrase != "").
		Msg("Server creating WebSSH session")

	// Auto-fallback to Winbox/Router credentials (source of truth) if SSH password is not provided
	if password == "" && privateKey == "" && peerID != "" {
		if peer, _ := s.peerStore.GetPeer(tenantID, peerID); peer != nil {
			if len(peer.WinboxSessions) > 0 {
				winbox := peer.WinboxSessions[0]
				if len(winbox.EncryptedPassword) > 0 {
					if acc, _ := s.accountMgr.GetAccount(tenantID); acc != nil {
						if privateKey, err := wgtypes.ParseKey(acc.PrivateKey); err == nil {
							if cipher, err := crypto.NewCredentialCipher(privateKey[:]); err == nil {
								if decryptedPassword, err := cipher.DecryptString(winbox.EncryptedPassword); err == nil {
									password = decryptedPassword
									log.Debug().
										Str("peer_id", peerID).
										Msg("🔐 Injected router password into WebSSH session as source of truth fallback")
								}
							}
						}
					}
				}
			}
		}
	}

	sessionID, err := s.directSSHHandler.CreateSession(tenantID, peerID, peerIP, sshPort, username, password, privateKey, privateKeyPassphrase, userAgent, rows, cols)
	if err != nil {
		return "", err
	}

	// Register the saved session location for later stream routing. Active
	// stream state is tracked separately when StreamSSH actually comes up.
	if err := s.RegisterWebSSHSession(tenantID, sessionID); err != nil {
		log.Warn().Err(err).Str("session_id", sessionID).Msg("Failed to register WebSSH session in Redis")
	}

	return sessionID, nil
}

// GetWebSSHSession returns information about an active WebSSH session.
func (s *Server) GetWebSSHSession(sessionID string) (*webssh.DirectSSHSession, error) {
	return s.directSSHHandler.GetSession(sessionID)
}

// ListWebSSHSessions lists all active WebSSH sessions for a tenant.
func (s *Server) ListWebSSHSessions(tenantID string) ([]*webssh.DirectSSHSession, error) {
	sessions, err := s.directSSHHandler.ListSessions(tenantID)
	if err != nil {
		return nil, err
	}

	// Enrich status from Redis for sessions not locally active (global view)
	// Enrich status from Redis for sessions not locally active (global view)
	if s.redisClient != nil && len(sessions) > 0 {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()

		pipe := s.redisClient.Pipeline()
		cmds := make(map[string]*redis.IntCmd)

		for _, sess := range sessions {
			if sess.Status == webssh.SessionStatusDisconnected {
				key := fmt.Sprintf("webssh:active:%s", sess.ID)
				cmds[sess.ID] = pipe.Exists(ctx, key)
			}
		}

		if len(cmds) > 0 {
			_, err := pipe.Exec(ctx)
			if err != nil && err != redis.Nil {
				log.Warn().Err(err).Msg("Failed to exec redis pipeline for session status")
			} else {
				for id, cmd := range cmds {
					if cmd.Val() > 0 {
						// Find session and update
						for _, sess := range sessions {
							if sess.ID == id {
								sess.Status = webssh.SessionStatusActive
								break
							}
						}
					}
				}
			}
		}
	}

	return sessions, nil
}

// StartWebSSHListener listens for distributed WebSSH control events
func (s *Server) StartWebSSHListener(ctx context.Context) {
	if s.redisClient == nil {
		return
	}

	pubsub := s.redisClient.Subscribe(ctx, "webssh:kill")
	defer pubsub.Close()
	ch := pubsub.Channel()

	log.Debug().Msg(" WebSSH Distributed Listener Started")

	for {
		select {
		case <-ctx.Done():
			return
		case msg := <-ch:
			var req map[string]string
			if err := json.Unmarshal([]byte(msg.Payload), &req); err != nil {
				continue
			}
			sessionID := req["session_id"]
			if sessionID != "" {
				// Kill local session if exists
				// We ignore error as it might not be local
				_ = s.directSSHHandler.DisconnectSession(sessionID)
				log.Debug().Str("session_id", sessionID).Msg("Received distributed kill signal for WebSSH session")
			}
		}
	}
}

// DisconnectWebSSHSession disconnects an active WebSSH session.
func (s *Server) DisconnectWebSSHSession(sessionID string) error {
	// 1. Try to disconnect locally/DB
	err := s.directSSHHandler.DisconnectSession(sessionID)

	// 2. Publish kill event to other hubs (distributed disconnect)
	if s.redisClient != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()

		msg := map[string]string{
			"session_id": sessionID,
		}
		data, _ := json.Marshal(msg)
		s.redisClient.Publish(ctx, "webssh:kill", data)
	}

	return err
}

// GetSSHStream returns an SSH stream for gRPC bidirectional streaming.
func (s *Server) GetSSHStream(ctx context.Context, sessionID string, authHandler webssh.InteractiveAuthHandler) (*webssh.SSHStream, error) {
	return s.directSSHHandler.GetSSHStream(ctx, sessionID, authHandler)
}

// ResizeSSHTerminal resizes the terminal for an SSH session.
func (s *Server) ResizeSSHTerminal(sessionID string, rows, cols int) error {
	return s.directSSHHandler.ResizeTerminal(sessionID, rows, cols)
}

// LogSSHActivityStart logs the start of an SSH session for activity tracking.
func (s *Server) LogSSHActivityStart(sessionID, clientIP, userAgent string) {
	s.directSSHHandler.LogActivityStart(sessionID, clientIP, userAgent)
}

// LogSSHActivityEnd logs the end of an SSH session with byte counts.
func (s *Server) LogSSHActivityEnd(sessionID string, bytesSent, bytesRecv uint64) {
	s.directSSHHandler.LogActivityEnd(sessionID, bytesSent, bytesRecv)
}

// ReleaseSSHStream releases the SSH multiplexer session back to the pool.
func (s *Server) ReleaseSSHStream(sessionID string) {
	s.directSSHHandler.ReleaseSSHStream(sessionID)
}

// GetDirectSSHHandler returns the DirectSSHHandler for HTTP WebSocket setup.
func (s *Server) GetDirectSSHHandler() *webssh.DirectSSHHandler {
	return s.directSSHHandler
}

// GetWinboxManager returns the WinboxManager for Winbox operations.
func (s *Server) GetWinboxManager() *mikrotik.WinboxManager {
	return s.winboxMgr
}

// GetWebProxyHandler returns the WebProxyHandler for HTTP/HTTPS proxying.
func (s *Server) GetWebProxyHandler() *webproxy.Handler {
	return s.webProxyHandler
}

// CreateWebProxySession creates a new web proxy session for a peer.
// tenantID is used for ownership, overlayAccountID for device access.
// If overlayAccountID is empty, tenantID is used for both.
func (s *Server) CreateWebProxySession(tenantID, overlayAccountID, peerID, peerIP string, port int, useHTTPS, skipTLSVerify bool) (*webproxy.Session, error) {
	if overlayAccountID == "" {
		overlayAccountID = tenantID
	}
	return s.webProxyHandler.CreateSession(tenantID, overlayAccountID, peerID, peerIP, port, useHTTPS, skipTLSVerify)
}

// GetWebProxySession returns information about an active web proxy session.
func (s *Server) GetWebProxySession(sessionID string) (*webproxy.Session, error) {
	return s.webProxyHandler.GetSession(sessionID)
}

// ListWebProxySessions lists all active web proxy sessions for a tenant.
func (s *Server) ListWebProxySessions(tenantID string) []*webproxy.Session {
	return s.webProxyHandler.ListSessions(tenantID)
}

// CloseWebProxySession closes an active web proxy session.
func (s *Server) CloseWebProxySession(sessionID string) error {
	return s.webProxyHandler.CloseSession(sessionID)
}

// // setupACLChecker configures ACL checking for a userspace tenant device
// func (s *Server) setupACLChecker(device *userspace.TenantDevice) {
// 	// if device == nil || device.TUN == nil {
// 	// 	return
// 	// }

// 	// Create ACL checker closure that captures the tenant ID
// 	tenantID := device.TenantID
// 	// checker := func(protocol, srcIP, dstIP string, dstPort int) (bool, string) {
// 	// 	return s.aclMgr.CheckAccess(tenantID, protocol, srcIP, dstIP, dstPort)
// 	// }

// 	// Set the checker on the TUN device
// 	// device.TUN.SetACLChecker(tenantID, checker)

// 	log.Debug().
// 		Str("tenant_id", tenantID).
// 		Msg("🛡️  ACL enforcement enabled for tenant")
// }

// GetTenantDevice returns the userspace tenant device for an account.
// Only available in userspace mode.
func (s *Server) GetTenantDevice(accountID string) (*userspace.TenantDevice, error) {
	return s.wgMgr.GetDevice(accountID)
}

// GetPeerStore returns the peer store for external access (e.g., gRPC services).
func (s *Server) GetPeerStore() *PeerStore {
	return s.peerStore
}

// GetAccountPeerStats returns the current peer count and plan limit for an account.
// max == 0 means unlimited (OnDemand tier).
func (s *Server) GetAccountPeerStats(accountID string) (current, max int, err error) {
	acc, err := s.accountMgr.GetAccount(accountID)
	if err != nil {
		return 0, 0, fmt.Errorf("account not found: %w", err)
	}
	current, err = s.peerStore.CountPeers(accountID)
	if err != nil {
		return 0, 0, fmt.Errorf("failed to count peers: %w", err)
	}
	max = planPeerLimit(acc)
	return current, max, nil
}

// GetUserspaceManager returns the userspace wireguard manager.
func (s *Server) GetUserspaceManager() *userspace.UserspaceManager {
	return s.wgMgr
}

// GetRedisClient returns the Redis client for external access (e.g., gRPC services).
func (s *Server) GetRedisClient() *redis.Client {
	return s.redisClient
}

// StartWinboxMultiplexer starts the Winbox multiplexer on the specified address
func (s *Server) StartWinboxMultiplexer(addr string) error {
	log.Debug().Str("addr", addr).Msg(" Starting Winbox multiplexer...")

	if s.winboxMgr == nil {
		log.Error().Msg("❌ Winbox manager not initialized")
		return fmt.Errorf("winbox manager not initialized")
	}

	log.Debug().Msg(" Creating Winbox multiplexer instance...")

	// Import the Winbox multiplexer package
	multiplexer := mikrotik.NewWinboxMultiplexer(addr, s.wgMgr)

	log.Debug().Msg(" Setting up O(1) access token lookup function...")

	// Set up peer lookup function for virtual username resolution - O(1) via index
	multiplexer.SetPeerLookupFunc(func(accessToken string) (*mikrotik.SessionInfo, error) {
		log.Debug().
			Str("access_token", accessToken).
			Msg(" O(1) lookup for access token")

		// O(1) lookup via access token index
		result, err := s.peerStore.GetWinboxSessionByAccessToken(accessToken)
		if err != nil {
			log.Warn().
				Err(err).
				Str("access_token", accessToken).
				Msg("❌ Access token not found")
			return nil, err
		}

		// Build SessionInfo from lookup result
		var routerIP, realUsername, realPassword, authMethod string
		var routerPort int
		var allowedCIDRs []string

		if result.Session != nil {
			// New multi-session mode - credentials are encrypted
			routerIP = result.Session.RouterIP
			authMethod = result.Session.AuthMethod
			allowedCIDRs = result.Session.AllowedClientIPs

			// Prioritize port explicitly configured in the session
			if result.Session.Port > 0 {
				routerPort = result.Session.Port
			}

			// Fallback to scan result ONLY if not configured in session (e.g. legacy)
			// AND scan result is valid (ignore FTP port 21)
			if routerPort <= 0 {
				peer, err := s.peerStore.GetPeer(result.AccountID, result.PeerID)
				if err == nil && peer.ScannedWinboxPort > 0 {
					if peer.ScannedWinboxPort == 21 {
						log.Warn().Str("peer_id", result.PeerID).Msg(" Ignoring scanned Winbox port 21 (FTP false positive)")
						routerPort = 8291
					} else {
						routerPort = peer.ScannedWinboxPort
						log.Debug().
							Str("peer_id", result.PeerID).
							Int("scanned_winbox_port", routerPort).
							Msg(" Using scanned Winbox port from peer metadata (session port unspecified)")
					}
				} else {
					routerPort = 8291
				}
			}

			// Get account to retrieve private key for decryption
			acc, err := s.GetAccount(result.AccountID)
			if err != nil {
				log.Error().
					Err(err).
					Str("account_id", result.AccountID).
					Msg("❌ Failed to get account for credential decryption")
				return nil, fmt.Errorf("account not found: %w", err)
			}

			// Parse the private key and create cipher
			privateKey, err := wgtypes.ParseKey(acc.PrivateKey)
			if err != nil {
				log.Error().
					Err(err).
					Str("account_id", result.AccountID).
					Msg("❌ Invalid account private key")
				return nil, fmt.Errorf("invalid private key: %w", err)
			}

			cipher, err := crypto.NewCredentialCipher(privateKey[:])
			if err != nil {
				log.Error().
					Err(err).
					Str("account_id", result.AccountID).
					Msg("❌ Failed to create credential cipher")
				return nil, fmt.Errorf("failed to create cipher: %w", err)
			}

			// Decrypt credentials
			usernameBytes, err := cipher.Decrypt(result.Session.EncryptedUsername)
			if err != nil {
				log.Error().
					Err(err).
					Str("session_id", result.Session.ID).
					Msg("❌ Failed to decrypt username")
				return nil, fmt.Errorf("failed to decrypt username: %w", err)
			}
			realUsername = string(usernameBytes)

			passwordBytes, err := cipher.Decrypt(result.Session.EncryptedPassword)
			if err != nil {
				log.Error().
					Err(err).
					Str("session_id", result.Session.ID).
					Msg("❌ Failed to decrypt password")
				return nil, fmt.Errorf("failed to decrypt password: %w", err)
			}
			realPassword = string(passwordBytes)

			log.Debug().
				Str("access_token", accessToken).
				Str("account_id", result.AccountID).
				Str("peer_id", result.PeerID).
				Str("session_id", result.Session.ID).
				Str("session_name", result.Session.Name).
				Str("router_ip", routerIP).
				Int("allowed_cidrs", len(allowedCIDRs)).
				Msg(" Found Winbox session")

			// CHECK: Is the session enabled?
			if !result.Session.Enabled {
				log.Warn().
					Str("access_token", accessToken).
					Str("account_id", result.AccountID).
					Str("peer_id", result.PeerID).
					Str("session_id", result.Session.ID).
					Msg("❌ Winbox session is disabled")
				return nil, fmt.Errorf("winbox session is disabled")
			}
		} else {
			// No session found - legacy single-session mode is no longer supported
			log.Warn().
				Str("access_token", accessToken).
				Str("account_id", result.AccountID).
				Str("peer_id", result.PeerID).
				Msg("❌ Access token found but no session - legacy mode no longer supported")
			return nil, fmt.Errorf("legacy single-session mode no longer supported, please create a Winbox session")
		}

		// Parse CIDR strings into net.IPNet for efficient matching
		var allowedNets []*net.IPNet
		for _, cidr := range allowedCIDRs {
			// Handle bare IPs by adding /32 suffix
			if !strings.Contains(cidr, "/") {
				cidr = cidr + "/32"
			}
			_, network, err := net.ParseCIDR(cidr)
			if err != nil {
				log.Warn().
					Err(err).
					Str("cidr", cidr).
					Msg(" Invalid CIDR in AllowedClientIPs, skipping")
				continue
			}
			allowedNets = append(allowedNets, network)
		}

		return &mikrotik.SessionInfo{
			AccountID:        result.AccountID,
			PeerID:           result.PeerID,
			SessionName:      result.Session.Name,
			RouterIP:         routerIP,
			RouterPort:       routerPort,
			PasswordToken:    result.Session.PasswordToken,
			RealUsername:     realUsername,
			RealPassword:     realPassword,
			AuthMethod:       authMethod,
			AllowedClientIPs: allowedNets,
		}, nil
	})

	// Set peer update callback for Winbox sessions
	multiplexer.SetPeerUpdateFunc(func(accountID, peerID string, updateFn func(any) error) error {
		peer, err := s.peerStore.GetPeer(accountID, peerID)
		if err != nil {
			return err
		}
		// Call the update function on the peer
		if err := updateFn(peer); err != nil {
			return err
		}
		// Save the updated peer
		peer.UpdatedAt = time.Now().UTC()
		return s.peerStore.SavePeer(peer)
	})

	// Set Winbox activity logging callback
	multiplexer.SetActivityLogFunc(func(accountID, peerID string, activity mikrotik.WinboxActivityData) error {
		// Convert to peer store WinboxActivity
		winboxActivity := WinboxActivity{
			SessionName: activity.SessionName,
			Username:    activity.Username,
			ClientIP:    activity.ClientIP,
			Timestamp:   activity.Timestamp,
			EndTime:     activity.EndTime,
			DurationMs:  activity.DurationMs,
			RomonMode:   activity.RomonMode,
		}

		// If EndTime is set, this is an update (session end)
		if !activity.EndTime.IsZero() {
			return s.peerStore.UpdateWinboxActivityForPeer(accountID, peerID, activity.SessionName, activity.Timestamp, func(a *WinboxActivity) {
				a.EndTime = activity.EndTime
				a.DurationMs = activity.DurationMs
			})
		}

		// Otherwise, log new activity (session start)
		return s.peerStore.LogWinboxActivity(accountID, peerID, winboxActivity)
	})

	// Start the multiplexer
	err := multiplexer.Start()
	if err != nil {
		log.Error().
			Err(err).
			Str("addr", addr).
			Msg("❌ Failed to start Winbox multiplexer")
		return err
	}

	// Log index size for monitoring
	log.Debug().
		Str("addr", addr).
		Int("indexed_tokens", s.peerStore.GetAccessTokenIndexSize()).
		Msg(" Winbox multiplexer started with O(1) access token lookup")
	return nil
}

// ============================================================================
// ACL GROUPS AND LINKS (PHASE 2)
// ============================================================================
type PeerGroup struct {
	ID          string
	AccountID   string
	Name        string
	Description string
	Protocols   []uint8
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

// GroupLink
type GroupLink struct {
	ID         string
	AccountID  string
	SrcGroupID string
	DstGroupID string
	Action     string // "allow" or "deny"
	Protocols  []uint8
	// PortRanges restrict destination ports for TCP/UDP. Start/End inclusive.
	PortRanges []PortRange
	CreatedAt  time.Time
	UpdatedAt  time.Time
}

// PortRange represents an inclusive TCP/UDP destination port range.
type PortRange struct {
	Start uint32 `json:"start"`
	End   uint32 `json:"end"`
}

// CreatePeerGroup creates a new peer group for the account.
// SIMPLE IN-MEMORY IMPLEMENTATION for MVP ACL enforcement
func (s *Server) CreatePeerGroup(accountID, groupID, name, description string, protocols []uint8) (*PeerGroup, error) {
	s.groupMu.Lock()
	defer s.groupMu.Unlock()

	// Check if group already exists
	if _, exists := s.peerGroups[groupID]; exists {
		return nil, fmt.Errorf("group %s already exists", groupID)
	}

	group := &PeerGroup{
		ID:          groupID,
		AccountID:   accountID,
		Name:        name,
		Description: description,
		Protocols:   protocols,
		CreatedAt:   time.Now().UTC(),
		UpdatedAt:   time.Now().UTC(),
	}

	s.peerGroups[groupID] = group

	// Index by account
	if s.accountGroups[accountID] == nil {
		s.accountGroups[accountID] = make(map[string]*PeerGroup)
	}
	s.accountGroups[accountID][groupID] = group

	log.Debug().
		Str("account_id", accountID).
		Str("group_id", groupID).
		Str("name", name).
		Msg(" Created peer group (in-memory)")

	// Persist to DB for durability
	if err := s.savePeerGroupToDB(accountID, group); err != nil {
		log.Warn().Err(err).Str("group_id", groupID).Msg("Failed to persist peer group to DB; continuing with in-memory only")
	}

	return group, nil
}

// DeletePeerGroup removes a peer group and all associated links.
func (s *Server) DeletePeerGroup(accountID, groupID string) error {
	s.groupMu.Lock()
	defer s.groupMu.Unlock()

	delete(s.peerGroups, groupID)
	if s.accountGroups[accountID] != nil {
		delete(s.accountGroups[accountID], groupID)
	}

	// Remove peer associations
	for peerID, groups := range s.peerGroupsIndex {
		delete(groups, groupID)
		if len(groups) == 0 {
			delete(s.peerGroupsIndex, peerID)
		}
	}

	log.Debug().
		Str("account_id", accountID).
		Str("group_id", groupID).
		Msg("🗑️  Deleted peer group")

	// Remove from DB as well (best-effort)
	if err := s.deletePeerGroupFromDB(accountID, groupID); err != nil {
		log.Warn().Err(err).Str("group_id", groupID).Msg("Failed to delete peer group from DB (maybe already removed)")
	}

	return nil
}

// savePeerGroupToDB persists a peer group to Postgres.
func (s *Server) savePeerGroupToDB(_ string, group *PeerGroup) error {
	data := &store.GroupData{
		ID:          group.ID,
		AccountID:   group.AccountID,
		Name:        group.Name,
		Description: group.Description,
		Protocols:   group.Protocols,
		CreatedAt:   group.CreatedAt,
		UpdatedAt:   group.UpdatedAt,
	}
	return s.groupRepo.SaveGroup(data)
}

// deletePeerGroupFromDB removes a peer group from Postgres.
func (s *Server) deletePeerGroupFromDB(_, groupID string) error {
	return s.groupRepo.DeleteGroup(groupID)
}

// loadPeerGroupsFromDB loads all peer groups from the database on startup
func (s *Server) loadPeerGroupsFromDB() error {
	s.groupMu.Lock()
	defer s.groupMu.Unlock()

	groups, err := s.groupRepo.ListAll()
	if err != nil {
		return fmt.Errorf("failed to load peer groups: %w", err)
	}

	count := 0
	memberCount := 0
	for _, g := range groups {
		pg := &PeerGroup{
			ID:          g.ID,
			AccountID:   g.AccountID,
			Name:        g.Name,
			Description: g.Description,
			Protocols:   g.Protocols,
			CreatedAt:   g.CreatedAt,
			UpdatedAt:   g.UpdatedAt,
		}

		s.peerGroups[pg.ID] = pg
		if s.accountGroups[pg.AccountID] == nil {
			s.accountGroups[pg.AccountID] = make(map[string]*PeerGroup)
		}
		s.accountGroups[pg.AccountID][pg.ID] = pg
		count++

		// Restore membership: populate peerGroupsIndex from peer_group_members table
		peerIDs, err := s.groupRepo.GetGroupPeers(g.ID)
		if err != nil {
			log.Warn().Err(err).Str("group_id", g.ID).Msg("Failed to load group members from DB")
			continue
		}
		for _, peerID := range peerIDs {
			if s.peerGroupsIndex[peerID] == nil {
				s.peerGroupsIndex[peerID] = make(map[string]bool)
			}
			s.peerGroupsIndex[peerID][g.ID] = true
			memberCount++
		}
	}

	log.Debug().Int("count", count).Int("members", memberCount).Msg(" Loaded peer groups and memberships from database")
	return nil
}

// AddPeerToGroup adds a peer to a peer group.
func (s *Server) AddPeerToGroup(accountID, peerID, groupID string) error {
	s.groupMu.Lock()
	defer s.groupMu.Unlock()

	// Check if group exists
	if _, exists := s.peerGroups[groupID]; !exists {
		// Auto-create group if it doesn't exist (using group name as ID)
		group := &PeerGroup{
			ID:        groupID,
			AccountID: accountID,
			Name:      groupID, // Use ID as name for auto-created groups
			CreatedAt: time.Now().UTC(),
			UpdatedAt: time.Now().UTC(),
		}
		// Persist group to DB first (FK constraints on group_links and peer_group_members require this)
		if err := s.savePeerGroupToDB(accountID, group); err != nil {
			return fmt.Errorf("failed to save auto-created group to DB: %w", err)
		}
		s.peerGroups[groupID] = group
		if s.accountGroups[accountID] == nil {
			s.accountGroups[accountID] = make(map[string]*PeerGroup)
		}
		s.accountGroups[accountID][groupID] = group
	}

	// Add peer to group index
	if s.peerGroupsIndex[peerID] == nil {
		s.peerGroupsIndex[peerID] = make(map[string]bool)
	}
	s.peerGroupsIndex[peerID][groupID] = true

	// Persist membership to DB (non-fatal: peer must exist in peers table)
	if err := s.groupRepo.AddPeerToGroup(groupID, peerID); err != nil {
		log.Warn().Err(err).Str("peer_id", peerID).Str("group_id", groupID).Msg("Failed to persist peer-group membership to DB")
	}

	log.Debug().
		Str("peer_id", peerID).
		Str("group_id", groupID).
		Msg("Added peer to group")

	return nil
}

// RemovePeerFromGroup removes a peer from a peer group.
func (s *Server) RemovePeerFromGroup(accountID, peerID, groupID string) error {
	s.groupMu.Lock()
	defer s.groupMu.Unlock()

	if s.peerGroupsIndex[peerID] != nil {
		delete(s.peerGroupsIndex[peerID], groupID)
		if len(s.peerGroupsIndex[peerID]) == 0 {
			delete(s.peerGroupsIndex, peerID)
		}
	}

	// Persist removal to DB
	if err := s.groupRepo.RemovePeerFromGroup(groupID, peerID); err != nil {
		log.Warn().Err(err).Str("peer_id", peerID).Str("group_id", groupID).Msg("Failed to remove peer-group membership from DB")
	}

	return nil
}

// ListPeerGroups returns all peer groups for an account.
func (s *Server) ListPeerGroups(accountID string) []*PeerGroup {
	s.groupMu.RLock()
	defer s.groupMu.RUnlock()

	groups := s.accountGroups[accountID]
	result := make([]*PeerGroup, 0, len(groups))
	for _, group := range groups {
		result = append(result, group)
	}
	return result
}

// GetPeerGroups returns groups that a peer belongs to.
func (s *Server) GetPeerGroups(accountID, peerID string) []*PeerGroup {
	s.groupMu.RLock()
	defer s.groupMu.RUnlock()

	groupIDs := s.peerGroupsIndex[peerID]
	result := make([]*PeerGroup, 0, len(groupIDs))
	for groupID := range groupIDs {
		if group, exists := s.peerGroups[groupID]; exists {
			if group.AccountID == accountID {
				result = append(result, group)
			}
		}
	}
	return result
}

// GetGroupPeers returns all peer IDs in a group.
func (s *Server) GetGroupPeers(groupID string) []string {
	s.groupMu.RLock()
	defer s.groupMu.RUnlock()

	var peerIDs []string
	for peerID, groups := range s.peerGroupsIndex {
		if groups[groupID] {
			peerIDs = append(peerIDs, peerID)
		}
	}
	return peerIDs
}

// CreateGroupLink creates a link (allow/deny rule) between two peer groups.
func (s *Server) CreateGroupLink(accountID, linkID, srcGroupID, dstGroupID, action string, protocols []uint8, portRanges []PortRange) (*GroupLink, error) {
	s.groupMu.Lock()
	defer s.groupMu.Unlock()

	link := &GroupLink{
		ID:         linkID,
		AccountID:  accountID,
		SrcGroupID: srcGroupID,
		DstGroupID: dstGroupID,
		Action:     action,
		Protocols:  protocols,
		PortRanges: portRanges,
		CreatedAt:  time.Now().UTC(),
		UpdatedAt:  time.Now().UTC(),
	}

	// Store in memory
	s.groupLinks[linkID] = link

	// Index by account
	if s.accountLinks[accountID] == nil {
		s.accountLinks[accountID] = make(map[string]*GroupLink)
	}
	s.accountLinks[accountID][linkID] = link

	// Persist to tenant DB
	if err := s.saveGroupLinkToDB(accountID, link); err != nil {
		// Rollback memory changes
		delete(s.groupLinks, linkID)
		delete(s.accountLinks[accountID], linkID)
		return nil, fmt.Errorf("failed to save group link to DB: %w", err)
	}

	log.Debug().
		Str("account_id", accountID).
		Str("link_id", linkID).
		Str("src_group", srcGroupID).
		Str("dst_group", dstGroupID).
		Str("action", action).
		Ints("protocols", func() []int {
			result := make([]int, len(protocols))
			for i, p := range protocols {
				result[i] = int(p)
			}
			return result
		}()).
		Msg(" Created group link (persisted to DB)")

	return link, nil
}

// saveGroupLinkToDB persists a group link to the database
func (s *Server) saveGroupLinkToDB(_ string, link *GroupLink) error {
	// Convert port ranges
	var portRanges []store.PortRange
	for _, pr := range link.PortRanges {
		portRanges = append(portRanges, store.PortRange{
			Start: uint16(pr.Start),
			End:   uint16(pr.End),
		})
	}

	linkData := &store.GroupLinkData{
		ID:         link.ID,
		AccountID:  link.AccountID,
		SrcGroupID: link.SrcGroupID,
		DstGroupID: link.DstGroupID,
		Action:     link.Action,
		Protocols:  link.Protocols,
		PortRanges: portRanges,
		CreatedAt:  link.CreatedAt,
		UpdatedAt:  link.UpdatedAt,
	}

	return s.groupRepo.SaveLink(linkData)
}

// DeleteGroupLink removes a link between two peer groups.
func (s *Server) DeleteGroupLink(accountID, linkID string) error {
	s.groupMu.Lock()
	defer s.groupMu.Unlock()

	// Delete from memory
	delete(s.groupLinks, linkID)
	if s.accountLinks[accountID] != nil {
		delete(s.accountLinks[accountID], linkID)
	}

	// Delete from DB
	if err := s.deleteGroupLinkFromDB(accountID, linkID); err != nil {
		log.Warn().Err(err).Str("link_id", linkID).Msg("Failed to delete group link from DB (already deleted from memory)")
	}

	log.Debug().
		Str("account_id", accountID).
		Str("link_id", linkID).
		Msg("🗑️  Deleted group link (from memory and DB)")

	return nil
}

// deleteGroupLinkFromDB removes a group link from the database
func (s *Server) deleteGroupLinkFromDB(_, linkID string) error {
	return s.groupRepo.DeleteLink(linkID)
}

// loadGroupLinksFromDB loads all group links from the database on startup
func (s *Server) loadGroupLinksFromDB() error {
	s.groupMu.Lock()
	defer s.groupMu.Unlock()

	links, err := s.groupRepo.ListAllLinks()
	if err != nil {
		return fmt.Errorf("failed to load group links: %w", err)
	}

	count := 0
	for _, l := range links {
		// Convert port ranges
		var portRanges []PortRange
		// l.PortRanges is []store.PortRange (concrete type)
		for _, pr := range l.PortRanges {
			portRanges = append(portRanges, PortRange{
				Start: uint32(pr.Start),
				End:   uint32(pr.End),
			})
		}

		link := GroupLink{
			ID:         l.ID,
			AccountID:  l.AccountID,
			SrcGroupID: l.SrcGroupID,
			DstGroupID: l.DstGroupID,
			Action:     l.Action,
			Protocols:  l.Protocols,
			PortRanges: portRanges,
			CreatedAt:  l.CreatedAt,
			UpdatedAt:  l.UpdatedAt,
		}

		s.groupLinks[link.ID] = &link
		if s.accountLinks[link.AccountID] == nil {
			s.accountLinks[link.AccountID] = make(map[string]*GroupLink)
		}
		s.accountLinks[link.AccountID][link.ID] = &link
		count++
	}

	log.Debug().Int("count", count).Msg(" Loaded group links from database")
	return nil
}

// ListGroupLinks returns all links for an account.
func (s *Server) ListGroupLinks(accountID string) []*GroupLink {
	s.groupMu.RLock()
	defer s.groupMu.RUnlock()

	links := s.accountLinks[accountID]
	result := make([]*GroupLink, 0, len(links))
	for _, link := range links {
		result = append(result, link)
	}
	return result
}

// GetOutboundLinks returns links originating from a group.
func (s *Server) GetOutboundLinks(accountID, groupID string) []*GroupLink {
	s.groupMu.RLock()
	defer s.groupMu.RUnlock()

	var out []*GroupLink
	for _, l := range s.accountLinks[accountID] {
		if l.SrcGroupID == groupID {
			out = append(out, l)
		}
	}
	return out
}

// GetInboundLinks returns links destined to a group.
func (s *Server) GetInboundLinks(accountID, groupID string) []*GroupLink {
	s.groupMu.RLock()
	defer s.groupMu.RUnlock()

	var in []*GroupLink
	for _, l := range s.accountLinks[accountID] {
		if l.DstGroupID == groupID {
			in = append(in, l)
		}
	}
	return in
}

// CompileGroups compiles peer groups and links into ACL rules and applies them
// to the tenant's gVisor netstack. Called from gRPC handlers triggered by user
// actions.
func (s *Server) CompileGroups(accountID string) ([]*ACLRule, error) {
	var result []*ACLRule
	var compileErr error
	pprof.Do(context.Background(), pprof.Labels("goroutine", "compile-groups-grpc", "account_id", accountID), func(ctx context.Context) {
		result, compileErr = s.compileGroups(accountID)
	})
	return result, compileErr
}

// compileGroups compiles group links into ACL rules and applies them to the
// tenant's gVisor netstack.
func (s *Server) compileGroups(accountID string) ([]*ACLRule, error) {
	// Get tenant device
	device, err := s.GetTenantDevice(accountID)
	if err != nil {
		return nil, fmt.Errorf("failed to get tenant device: %w", err)
	}

	// Get all group links for this account
	links := s.ListGroupLinks(accountID)
	if len(links) == 0 {
		// No links = no restrictions, clear ACL rules
		if err := device.ApplyACLRules([]userspace.ACLRule{}); err != nil {
			return nil, fmt.Errorf("failed to clear ACL rules: %w", err)
		}
		log.Debug().
			Str("account_id", accountID).
			Msg(" No group links - cleared ACL rules (all traffic allowed)")
		return []*ACLRule{}, nil
	}

	// Get all peers for this account to resolve peer IPs
	peers, err := s.peerStore.ListPeers(accountID)
	if err != nil {
		return nil, fmt.Errorf("failed to list peers: %w", err)
	}
	peerIPMap := make(map[string]string) // peerID -> IP
	for _, peer := range peers {
		peerIPMap[peer.ID] = peer.AssignedIP
	}

	// Get tenant device server IPs (one per subnet block) - NEVER BLOCK THESE
	serverIPs := make(map[string]bool)
	for _, subnet := range device.Subnets {
		_, ipNet, err := net.ParseCIDR(subnet)
		if err != nil {
			log.Warn().Err(err).Str("subnet", subnet).Msg("Failed to parse subnet")
			continue
		}
		// First usable IP is the server IP
		serverIP := ipNet.IP
		serverIP[len(serverIP)-1]++ // Increment to first usable
		serverIPs[serverIP.String()] = true
	}

	log.Debug().
		Str("account_id", accountID).
		Interface("server_ips", serverIPs).
		Msg("Tenant device server IPs - these will never be blocked")

	// Compile group links into ACL rules with actual peer IPs
	var aclRules []userspace.ACLRule
	ruleCounter := 0

	for _, link := range links {
		// Check if this is a bidirectional link (same group on both sides = mesh)
		isBidirectional := link.SrcGroupID == link.DstGroupID

		// Resolve source selector to concrete peers. This supports both the
		// legacy explicit group-membership model and Tailscale-style dynamic
		// selectors such as tag:<name> and client:wantasticd.
		sourcePeerIDs := s.resolvePolicyPeerIDs(accountID, link.SrcGroupID, peers)
		if len(sourcePeerIDs) == 0 {
			log.Debug().
				Str("account_id", accountID).
				Str("link_id", link.ID).
				Str("source_selector", link.SrcGroupID).
				Msg("No peers matched source selector, skipping link")
			continue
		}

		// Resolve destination selector to concrete peers.
		destPeerIDs := s.resolvePolicyPeerIDs(accountID, link.DstGroupID, peers)
		if len(destPeerIDs) == 0 {
			log.Debug().
				Str("account_id", accountID).
				Str("link_id", link.ID).
				Str("dest_selector", link.DstGroupID).
				Msg("No peers matched destination selector, skipping link")
			continue
		}

		if isBidirectional {
			log.Debug().
				Str("account_id", accountID).
				Str("link_id", link.ID).
				Str("group", link.SrcGroupID).
				Int("peer_count", len(sourcePeerIDs)).
				Msg(" Processing bidirectional mesh link (same group on both sides)")
		}

		// For each protocol in the link, create rules for all source/dest peer combinations
		for _, protoNum := range link.Protocols {
			var protoStr string
			switch protoNum {
			case 1:
				protoStr = "icmp"
			case 6:
				protoStr = "tcp"
			case 17:
				protoStr = "udp"
			default:
				protoStr = "all"
			}

			// Create rules for each source -> dest pair
			for _, srcPeerID := range sourcePeerIDs {
				srcIP := peerIPMap[srcPeerID]
				if srcIP == "" {
					log.Warn().
						Str("peer_id", srcPeerID).
						Msg("Source peer has no IP, skipping")
					continue
				}

				for _, dstPeerID := range destPeerIDs {
					dstIP := peerIPMap[dstPeerID]
					if dstIP == "" {
						log.Warn().
							Str("peer_id", dstPeerID).
							Msg("Destination peer has no IP, skipping")
						continue
					}

					// Skip self-connections (peer can always reach itself)
					if srcPeerID == dstPeerID {
						continue
					}

					// CRITICAL: Skip if destination is a server IP (WireGuard gateway)
					// Blocking server IPs breaks WireGuard keepalive and peer connectivity
					if serverIPs[dstIP] {
						log.Debug().
							Str("account_id", accountID).
							Str("dest_ip", dstIP).
							Str("link_id", link.ID).
							Msg("  Skipping rule - destination is server IP (never block gateway)")
						continue
					}

					// CRITICAL: Skip if source is a server IP
					// Server IP should never be restricted as source
					if serverIPs[srcIP] {
						log.Debug().
							Str("account_id", accountID).
							Str("src_ip", srcIP).
							Str("link_id", link.ID).
							Msg("  Skipping rule - source is server IP (never restrict gateway)")
						continue
					}

					// Create ACL rules considering port ranges if present
					if len(link.PortRanges) == 0 {
						// No port ranges - protocol-level rule
						rule := userspace.ACLRule{
							RuleID:      fmt.Sprintf("link-%s-p%d-%s-%s-%d", link.ID, protoNum, srcPeerID[:8], dstPeerID[:8], ruleCounter),
							Protocol:    protoStr,
							SourceIP:    srcIP,
							DestIP:      dstIP,
							DestPort:    0, // any port
							Action:      link.Action,
							Description: fmt.Sprintf("Link %s: %s->%s (%s)", link.ID, link.SrcGroupID, link.DstGroupID, protoStr),
						}
						aclRules = append(aclRules, rule)
						ruleCounter++
					} else {
						// Port ranges provided - only meaningful for TCP/UDP
						if protoStr != "tcp" && protoStr != "udp" {
							// For non-TCP/UDP protocols, fall back to protocol-level rule
							rule := userspace.ACLRule{
								RuleID:      fmt.Sprintf("link-%s-p%d-%s-%s-%d", link.ID, protoNum, srcPeerID[:8], dstPeerID[:8], ruleCounter),
								Protocol:    protoStr,
								SourceIP:    srcIP,
								DestIP:      dstIP,
								DestPort:    0,
								Action:      link.Action,
								Description: fmt.Sprintf("Link %s: %s->%s (%s) port-range %d-%d (fallback)", link.ID, link.SrcGroupID, link.DstGroupID, protoStr, 0, 0),
							}
							aclRules = append(aclRules, rule)
							ruleCounter++
						} else {
							// For each port range, expand to individual port rules within a reasonable cap
							for _, pr := range link.PortRanges {
								start := pr.Start
								end := pr.End
								if end < start {
									start, end = end, start
								}
								// Cap expansion to avoid explosion
								const maxExpand = 1024
								if end-start > maxExpand {
									// Too large - fall back to any-port protocol rule and log
									log.Warn().Str("link_id", link.ID).Uint32("start", start).Uint32("end", end).Msg("Port range too large to expand; falling back to protocol-level rule")
									rule := userspace.ACLRule{
										RuleID:      fmt.Sprintf("link-%s-p%d-%s-%s-%d", link.ID, protoNum, srcPeerID[:8], dstPeerID[:8], ruleCounter),
										Protocol:    protoStr,
										SourceIP:    srcIP,
										DestIP:      dstIP,
										DestPort:    0,
										Action:      link.Action,
										Description: fmt.Sprintf("Link %s: %s->%s (%s) port-range %d-%d (fallback)", link.ID, link.SrcGroupID, link.DstGroupID, protoStr, start, end),
									}
									aclRules = append(aclRules, rule)
									ruleCounter++
									continue
								}

								for p := start; p <= end; p++ {
									rule := userspace.ACLRule{
										RuleID:      fmt.Sprintf("link-%s-p%d-%s-%s-%d-%d", link.ID, protoNum, srcPeerID[:8], dstPeerID[:8], ruleCounter, p),
										Protocol:    protoStr,
										SourceIP:    srcIP,
										DestIP:      dstIP,
										DestPort:    int(p),
										Action:      link.Action,
										Description: fmt.Sprintf("Link %s: %s->%s (%s) port %d", link.ID, link.SrcGroupID, link.DstGroupID, protoStr, p),
									}
									aclRules = append(aclRules, rule)
									ruleCounter++
								}
							}
						}
					}
				}
			}
		}
	}

	// Apply compiled rules to tenant device gVisor netstack
	if err := device.ApplyACLRules(aclRules); err != nil {
		return nil, fmt.Errorf("failed to apply ACL rules to device: %w", err)
	}

	// Convert to server ACLRule format for return value
	serverRules := make([]*ACLRule, len(aclRules))
	for i, rule := range aclRules {
		serverRules[i] = &ACLRule{
			ID:        rule.RuleID,
			AccountID: accountID,
			Protocol:  rule.Protocol,
			SourceIP:  rule.SourceIP,
			DestIP:    rule.DestIP,
			DestPort:  rule.DestPort,
			Action:    rule.Action,
			Priority:  i, // Use order as priority
		}
	}

	log.Debug().
		Str("account_id", accountID).
		Int("link_count", len(links)).
		Int("rule_count", len(aclRules)).
		Msg(" Compiled group links to ACL rules and applied to gVisor netstack")

	return serverRules, nil
}

// GetCompilationStats returns statistics about group compilation for an account.
func (s *Server) GetCompilationStats(accountID string) map[string]any {
	s.groupMu.RLock()
	defer s.groupMu.RUnlock()

	groups := 0
	if s.accountGroups[accountID] != nil {
		groups = len(s.accountGroups[accountID])
	}
	links := 0
	if s.accountLinks[accountID] != nil {
		links = len(s.accountLinks[accountID])
	}
	peers := 0
	if ps, err := s.peerStore.ListPeers(accountID); err == nil {
		peers = len(ps)
	}

	// Rough estimate: rules = links * peers^2 * avg protocols (very rough)
	estimated := links * peers * peers

	return map[string]any{
		"groups":          groups,
		"links":           links,
		"total_peers":     peers,
		"estimated_rules": estimated,
	}
}

// =============================================================================
// Redis Port Scan Result Storage
// =============================================================================

// SavePeerScanResult saves the latest port scan result to Redis with a 24-hour TTL.
// It uses a consistent JSON format to avoid unmarshal errors.
// SavePeerScanResult saves the latest port scan result to Redis with a 24-hour TTL
// and persists key metadata to the PostgreSQL database for reliable retrieval.
func (s *Server) SavePeerScanResult(accountID, peerID string, result *userspace.ScanResult) error {
	// 1. Save to Redis for fast access (24h TTL)
	if s.redisClient != nil {
		key := fmt.Sprintf("scan:result:%s", peerID)
		if data, err := json.Marshal(result); err == nil {
			if err := s.redisClient.Set(context.Background(), key, data, 24*time.Hour).Err(); err != nil {
				log.Warn().Err(err).Str("peer_id", peerID).Msg("Failed to cache scan result in Redis")
			}
		}
	}

	// 2. Persist to PostgreSQL via PeerStore
	peer, err := s.peerStore.GetPeer(accountID, peerID)
	if err != nil {
		return fmt.Errorf("failed to get peer for saving scan result: %w", err)
	}

	// Filter open ports for efficient storage
	openPortsOnly := make([]*userspace.PortResult, 0)
	var winboxPort, sshPort, webPort int

	for _, pr := range result.Ports {
		// Clean text fields
		pr.Service = strings.ToValidUTF8(strings.ReplaceAll(pr.Service, "\x00", ""), "")
		pr.Banner = strings.ToValidUTF8(strings.ReplaceAll(pr.Banner, "\x00", ""), "")

		if pr.State == "open" || (pr.Protocol == "udp" && pr.State == "open|filtered" && pr.Service != "" && !strings.Contains(pr.Service, "unknown")) {
			openPortsOnly = append(openPortsOnly, pr)

			// Service detection
			// Explicitly exclude port 21 (FTP) to avoid false positives
			if winboxPort == 0 && pr.Port != 21 && (pr.Port == 8291 || strings.Contains(strings.ToLower(pr.Service), "winbox")) {
				winboxPort = pr.Port
			}
			if sshPort == 0 && (pr.Port == 22 || strings.Contains(strings.ToLower(pr.Service), "ssh")) {
				sshPort = pr.Port
			}
			if webPort == 0 && (pr.Port == 80 || pr.Port == 443 || strings.Contains(strings.ToLower(pr.Service), "http")) {
				webPort = pr.Port
			}
		}
	}

	// Update result with only open ports for storage
	result.Ports = openPortsOnly

	// Update Peer fields
	peer.LastPortScan = time.Now().UTC()
	if resultJSON, err := json.Marshal(result); err == nil {
		peer.CachedPortScanJSON = resultJSON
	}
	peer.ScannedWinboxPort = winboxPort
	peer.ScannedSSHPort = sshPort
	peer.ScannedWebPort = webPort
	peer.HasWinbox = winboxPort > 0

	// Save to DB
	if err := s.peerStore.SavePeer(peer); err != nil {
		return fmt.Errorf("failed to save peer scan metadata to DB: %w", err)
	}

	return nil
}

// GetPeerScanResult retrieves the cached port scan result from Redis.
// Returns nil, nil if no result is found.
func (s *Server) GetPeerScanResult(peerID string) (*userspace.ScanResult, error) {
	if s.redisClient == nil {
		return nil, nil // Return nil if redis not available
	}

	key := fmt.Sprintf("scan:result:%s", peerID)

	val, err := s.redisClient.Get(context.Background(), key).Result()
	if err == redis.Nil {
		return nil, nil // No cached result
	}
	if err != nil {
		return nil, fmt.Errorf("redis get error: %w", err)
	}

	var result userspace.ScanResult
	if err := json.Unmarshal([]byte(val), &result); err != nil {
		return nil, fmt.Errorf("failed to unmarshal scan result: %w", err)
	}

	return &result, nil
}

// =============================================================================
// PING
// =============================================================================

type PingRequest struct {
	ReqID     string `json:"req_id"`
	AccountID string `json:"account_id"`
	PeerID    string `json:"peer_id"`
	Count     int    `json:"count"`
	TimeoutMs int    `json:"timeout_ms"`
	ReplyTo   string `json:"reply_to"` // Redis channel to reply on
}

type PingResponse struct {
	ReqID  string                `json:"req_id"`
	Result *userspace.PingResult `json:"result,omitempty"`
	Error  string                `json:"error,omitempty"`
}

// StartPingListener listens for distributed ping requests
func (s *Server) StartPingListener(ctx context.Context) {
	if s.redisClient == nil {
		return
	}

	channels := []string{legacyPingRequestPrefix, targetedPingRequestChannel(s.getHubID())}
	pubsub := s.redisClient.Subscribe(ctx, channels...)
	defer pubsub.Close()
	ch := pubsub.Channel()

	log.Debug().Strs("channels", channels).Msg(" Distributed Ping Listener Started")

	for {
		select {
		case <-ctx.Done():
			return
		case msg := <-ch:
			var req PingRequest
			if err := json.Unmarshal([]byte(msg.Payload), &req); err != nil {
				log.Warn().Err(err).Msg("Failed to unmarshal ping request")
				continue
			}
			// Handle in background
			go s.handleDistributedPing(ctx, req)
		}
	}
}

func (s *Server) handleDistributedPing(ctx context.Context, req PingRequest) {
	// 1. Check if we own the peer (local check)
	s.mu.RLock()
	device, ok := s.tenantDevices[req.AccountID]
	s.mu.RUnlock()

	if !ok {
		// Log only at Trace/Debug to avoid noise on non-owning cores
		return
	}

	peerStatus, err := device.GetPeerStatus(req.PeerID)
	if err != nil {
		log.Warn().Err(err).Str("account_id", req.AccountID).Msg("Failed to get peer status for ping")
		return
	}
	if peerStatus == nil {
		log.Debug().Str("peer_id", req.PeerID).Msg("Ping request: Peer status unavailable")
		return
	}
	if !peerStatus.IsOnline {
		log.Debug().Str("peer_id", req.PeerID).Msg("Ping request: Peer is offline")
		return
	}
	if peerStatus.Endpoint == "" {
		log.Debug().Str("peer_id", req.PeerID).Msg("Ping request: Peer has no endpoint")
		return
	}

	log.Debug().Str("peer_id", req.PeerID).Msg("⚡ Handling distributed ping request")

	// 2. Perform Ping locally
	// Call device directly to avoid recursive loop in PingPeer
	peerIP := strings.TrimSuffix(peerStatus.AssignedIP, "/32")
	result, err := device.ICMPPing(peerIP, req.Count, req.TimeoutMs)

	resp := PingResponse{
		ReqID: req.ReqID,
	}
	if err != nil {
		resp.Error = err.Error()
	} else {
		resp.Result = result

		// If ping was successful, the peer is definitely online.
		// Force an update to the online status to ensure the dashboard reflects this immediately.
		if result.Success {
			s.peerStore.UpdatePeerLastSeenCache(req.PeerID, time.Now().UTC())
		}
	}

	// 3. Send Response
	data, _ := json.Marshal(resp)
	s.redisClient.Publish(ctx, req.ReplyTo, data)
}
