// Package tenant provides peer offline notification management.
package tenant

import (
	"context"
	"fmt"
	"sync"
	"time"

	"WantasticCore/internal/crypto"
	"WantasticCore/internal/email"
	"WantasticCore/internal/server"

	"github.com/redis/go-redis/v9"
	"github.com/rs/zerolog/log"
)

// NotificationManager manages per-tenant notification workers.
// It ensures only one worker runs per tenant, and workers are only started
// when at least one peer has notifications enabled.
type NotificationManager struct {
	registry    Registry
	peerStore   *server.PeerStore
	emailSender EmailSender
	config      NotificationConfig

	// Hook cipher for generating unsubscribe tokens
	hookCipher  *crypto.NotificationHookCipher
	hookBaseURL string // Base URL for hooks (e.g., "https://console.wantastic.app/hooks")

	// Per-tenant workers: tenantID -> worker
	workers map[string]*TenantNotificationWorker
	redis   *redis.Client
	mu      sync.RWMutex

	// Global stop channel
	stopChan chan struct{}
	stopped  bool
}

// TenantNotificationWorker monitors a single tenant's peers for offline notifications.
type TenantNotificationWorker struct {
	tenantID    string
	registry    Registry
	peerStore   *server.PeerStore
	emailSender EmailSender
	config      NotificationConfig

	// Hook cipher for generating unsubscribe tokens
	hookCipher  *crypto.NotificationHookCipher
	hookBaseURL string

	stopChan chan struct{}
	redis    *redis.Client
	wg       sync.WaitGroup
	running  bool
	mu       sync.RWMutex

	// Notification tracking
	dailyNotificationCount int
	lastResetDate          time.Time
}

// NewNotificationManager creates a new notification manager.
func NewNotificationManager(
	registry Registry,
	peerStore *server.PeerStore,
	emailSender EmailSender,
	config NotificationConfig,
	redis *redis.Client,
) *NotificationManager {
	return &NotificationManager{
		registry:    registry,
		peerStore:   peerStore,
		emailSender: emailSender,
		config:      config,
		redis:       redis,
		workers:     make(map[string]*TenantNotificationWorker),
		stopChan:    make(chan struct{}),
	}
}

// SetHookCipher sets the hook cipher for generating unsubscribe tokens.
// This must be called before any workers are started.
func (m *NotificationManager) SetHookCipher(cipher *crypto.NotificationHookCipher, baseURL string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.hookCipher = cipher
	m.hookBaseURL = baseURL
	log.Debug().Str("base_url", baseURL).Msg(" Notification manager: hook cipher configured")
}

// RestoreFromDatabase checks all tenants and starts workers for those with peers that have notifications enabled.
// This should be called at server startup after RestoreTenantsFromDatabase.
func (m *NotificationManager) RestoreFromDatabase() error {
	tenants, err := m.registry.ListTenants()
	if err != nil {
		return fmt.Errorf("failed to list tenants: %w", err)
	}

	restoredCount := 0
	for _, tenant := range tenants {
		// Skip inactive tenants
		if tenant.Status == "deleted" || tenant.Status == "suspended" {
			continue
		}
		if tenant.OverlayAccountID == "" {
			continue
		}

		// Check if any peer has notifications enabled
		if m.hasPeersWithNotificationsEnabled(tenant.OverlayAccountID) {
			m.startWorkerForTenant(tenant.ID, tenant.OverlayAccountID)
			restoredCount++
		}
	}

	if restoredCount > 0 {
		log.Debug().
			Int("workers_restored", restoredCount).
			Msg(" Notification workers restored from database")
	}

	return nil
}

// hasPeersWithNotificationsEnabled checks if any peer for the given account has notifications enabled.
func (m *NotificationManager) hasPeersWithNotificationsEnabled(overlayAccountID string) bool {
	peers, err := m.peerStore.ListPeers(overlayAccountID)
	if err != nil {
		return false
	}

	for _, peer := range peers {
		if peer.NotificationEnabled {
			return true
		}
	}
	return false
}

// OnPeerNotificationToggle is called when a peer's notification setting is toggled.
// It starts or stops the worker for the tenant based on whether any peers have notifications enabled.
// This method is non-blocking and handles worker state changes in a background goroutine.
func (m *NotificationManager) OnPeerNotificationToggle(tenantID, overlayAccountID string, enabled bool) {
	if m.stopped {
		return
	}

	// Run in background to avoid blocking gRPC calls (e.g. if worker stopping is slow)
	go func() {
		m.mu.Lock()
		defer m.mu.Unlock()

		if m.stopped {
			return
		}

		if enabled {
			// Start worker if not already running
			if _, exists := m.workers[tenantID]; !exists {
				m.startWorkerForTenantLocked(tenantID, overlayAccountID)
			}
		} else {
			// Check if any other peer still has notifications enabled
			// This involves a DB lookup, so it's safer in a goroutine
			if !m.hasPeersWithNotificationsEnabled(overlayAccountID) {
				m.stopWorkerForTenantLocked(tenantID)
			}
		}
	}()
}

// startWorkerForTenant starts a notification worker for a tenant (acquires lock).
func (m *NotificationManager) startWorkerForTenant(tenantID, overlayAccountID string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.startWorkerForTenantLocked(tenantID, overlayAccountID)
}

// startWorkerForTenantLocked starts a notification worker for a tenant (caller must hold lock).
func (m *NotificationManager) startWorkerForTenantLocked(tenantID, overlayAccountID string) {
	if m.stopped {
		return
	}

	// Don't start if already running
	if _, exists := m.workers[tenantID]; exists {
		return
	}

	worker := &TenantNotificationWorker{
		tenantID:      tenantID,
		registry:      m.registry,
		peerStore:     m.peerStore,
		emailSender:   m.emailSender,
		config:        m.config,
		hookCipher:    m.hookCipher,
		hookBaseURL:   m.hookBaseURL,
		stopChan:      make(chan struct{}),
		redis:         m.redis,
		lastResetDate: time.Now().Truncate(24 * time.Hour),
	}

	m.workers[tenantID] = worker
	worker.Start(overlayAccountID)

	log.Debug().
		Str("tenant_id", tenantID).
		Msg(" Started notification worker for tenant")
}

// stopWorkerForTenantLocked stops a notification worker for a tenant (caller must hold lock).
func (m *NotificationManager) stopWorkerForTenantLocked(tenantID string) {
	worker, exists := m.workers[tenantID]
	if !exists {
		return
	}

	worker.Stop()
	delete(m.workers, tenantID)

	log.Debug().
		Str("tenant_id", tenantID).
		Msg("🛑 Stopped notification worker for tenant (no peers with alerts enabled)")
}

// Stop stops all notification workers.
func (m *NotificationManager) Stop() {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.stopped {
		return
	}
	m.stopped = true
	close(m.stopChan)

	for tenantID, worker := range m.workers {
		worker.Stop()
		delete(m.workers, tenantID)
	}

	log.Debug().Msg("🛑 Notification manager stopped")
}

// GetActiveWorkerCount returns the number of active workers.
func (m *NotificationManager) GetActiveWorkerCount() int {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return len(m.workers)
}

// =============================================================================
// TenantNotificationWorker - Per-tenant worker
// =============================================================================

// Start begins the notification worker for a specific tenant.
func (w *TenantNotificationWorker) Start(overlayAccountID string) {
	w.mu.Lock()
	if w.running {
		w.mu.Unlock()
		return
	}
	w.running = true
	w.mu.Unlock()

	w.wg.Add(1)
	go w.run(overlayAccountID)
}

// Stop stops the notification worker.
func (w *TenantNotificationWorker) Stop() {
	w.mu.Lock()
	if !w.running {
		w.mu.Unlock()
		return
	}
	w.running = false
	w.mu.Unlock()

	close(w.stopChan)
	w.wg.Wait()
}

// run is the main worker loop for a single tenant.
func (w *TenantNotificationWorker) run(overlayAccountID string) {
	defer w.wg.Done()

	// Calculate delay until next 15-minute mark (00, 15, 30, 45)
	now := time.Now()
	next := now.Truncate(15 * time.Minute).Add(15 * time.Minute)
	delay := next.Sub(now)

	log.Debug().
		Str("tenant_id", w.tenantID).
		Dur("delay", delay).
		Time("next_run", next).
		Msg(" Notification worker scheduled (aligned to 15m clock)")

	// Wait for the first aligned tick
	select {
	case <-w.stopChan:
		return
	case <-time.After(delay):
		w.checkOfflinePeers(overlayAccountID)
	}

	// Run every 15 minutes aligned to the clock
	ticker := time.NewTicker(15 * time.Minute)
	defer ticker.Stop()

	for {
		select {
		case <-w.stopChan:
			return
		case <-ticker.C:
			w.tryCheckOfflinePeers(overlayAccountID)
		}
	}
}

// tryCheckOfflinePeers attempts to acquire a Redis lock for this specific tenant before running the check.
func (w *TenantNotificationWorker) tryCheckOfflinePeers(overlayAccountID string) {
	if w.redis == nil {
		// Fallback for single node
		w.checkOfflinePeers(overlayAccountID)
		return
	}

	ctx := context.Background()
	lockKey := fmt.Sprintf("wantastic:lock:notification:%s", w.tenantID)
	// Lock for 10 minutes (less than 15-minute interval)
	lockDuration := 10 * time.Minute

	ok, err := w.redis.SetNX(ctx, lockKey, "locked", lockDuration).Result()
	if err != nil {
		log.Error().Err(err).Str("tenant_id", w.tenantID).Msg("Failed to acquire Redis lock for tenant notification")
		return
	}

	if !ok {
		// Another core is already checking this tenant
		return
	}

	w.checkOfflinePeers(overlayAccountID)
}

// checkOfflinePeers checks the tenant's peers for offline notifications.
func (w *TenantNotificationWorker) checkOfflinePeers(overlayAccountID string) {
	ctx := context.Background()
	now := time.Now()

	// Reset daily notification count if new day
	today := now.Truncate(24 * time.Hour)
	if today.After(w.lastResetDate) {
		w.dailyNotificationCount = 0
		w.lastResetDate = today
		log.Debug().Str("tenant_id", w.tenantID).Msg(" Reset daily notification count")
	}

	// Check daily limit
	if w.config.MaxNotificationsDay > 0 && w.dailyNotificationCount >= w.config.MaxNotificationsDay {
		log.Debug().
			Str("tenant_id", w.tenantID).
			Int("daily_count", w.dailyNotificationCount).
			Int("max_day", w.config.MaxNotificationsDay).
			Msg(" Skipping notification check - daily limit reached")
		return
	}

	// Get tenant info
	tenant, err := w.registry.GetTenant(w.tenantID)
	if err != nil {
		log.Warn().Err(err).Str("tenant_id", w.tenantID).Msg("Failed to get tenant for notification check")
		return
	}

	// Skip inactive tenants
	if tenant.Status == "deleted" || tenant.Status == "suspended" {
		return
	}

	// Get peers
	peers, err := w.peerStore.ListPeers(overlayAccountID)
	if err != nil {
		log.Warn().Err(err).Str("tenant_id", w.tenantID).Msg("Failed to list peers for notification check")
		return
	}

	log.Debug().
		Str("tenant_id", w.tenantID).
		Int("peer_count", len(peers)).
		Msg(" Checking peers for offline notifications")

	// Collect offline peers that need notification
	var offlinePeers []OfflinePeerInfo

	for _, peer := range peers {
		// Skip if notifications not enabled for this peer
		if !peer.NotificationEnabled {
			continue
		}

		// Skip peers that have never been online
		if peer.FirstSeenOnline.IsZero() {
			log.Debug().
				Str("peer_id", peer.ID).
				Str("peer_name", peer.Name).
				Msg(" Skipping peer - never been online (FirstSeenOnline is zero)")
			continue
		}

		// Skip peers that are currently online
		if peer.IsOnline {
			// Reset notification state if peer came back online
			if peer.OfflineNotificationState == "sent" {
				peer.OfflineNotificationState = "none"
				peer.FailedHandshakes = 0
				peer.LastOnlineAt = now
				_ = w.peerStore.SavePeer(peer)
				log.Debug().
					Str("peer_id", peer.ID).
					Str("peer_name", peer.Name).
					Msg(" Peer came back online - reset notification state")
			}
			continue
		}

		// Calculate offline duration
		offlineSince := peer.LastHandshakeTime
		if offlineSince.IsZero() {
			offlineSince = peer.LastSeenAt
		}
		if offlineSince.IsZero() {
			log.Debug().
				Str("peer_id", peer.ID).
				Str("peer_name", peer.Name).
				Msg(" Skipping peer - cannot determine when it went offline")
			continue
		}

		offlineDuration := now.Sub(offlineSince)

		// Skip if not offline long enough
		if offlineDuration < w.config.OfflineThreshold {
			log.Debug().
				Str("peer_id", peer.ID).
				Str("peer_name", peer.Name).
				Dur("offline_duration", offlineDuration).
				Dur("threshold", w.config.OfflineThreshold).
				Msg(" Skipping peer - not offline long enough")
			continue
		}

		// Skip if notification already sent (cooldown)
		if !peer.LastNotificationSentAt.IsZero() {
			if now.Sub(peer.LastNotificationSentAt) < w.config.CooldownPeriod {
				log.Debug().
					Str("peer_id", peer.ID).
					Str("peer_name", peer.Name).
					Time("last_notification", peer.LastNotificationSentAt).
					Dur("cooldown", w.config.CooldownPeriod).
					Msg(" Skipping peer - in cooldown period")
				continue
			}
		}

		// Skip if already notified and peer hasn't come back online
		if peer.OfflineNotificationState == "sent" {
			log.Debug().
				Str("peer_id", peer.ID).
				Str("peer_name", peer.Name).
				Msg(" Skipping peer - notification already sent")
			continue
		}

		// Determine reason
		reason := OfflineReasonGoneOffline
		if peer.FailedHandshakes >= w.config.FailedHandshakeMax {
			reason = OfflineReasonMisconfigured
		}

		log.Debug().
			Str("peer_id", peer.ID).
			Str("peer_name", peer.Name).
			Dur("offline_duration", offlineDuration).
			Str("reason", string(reason)).
			Msg(" Peer qualifies for offline notification")

		offlinePeers = append(offlinePeers, OfflinePeerInfo{
			PeerID:           peer.ID,
			PeerName:         peer.Name,
			AssignedIP:       peer.AssignedIP,
			TenantID:         w.tenantID,
			TenantEmail:      tenant.Email,
			TenantName:       tenant.FullName,
			OfflineSince:     offlineSince,
			OfflineDuration:  offlineDuration,
			Reason:           reason,
			FailedHandshakes: peer.FailedHandshakes,
			LastOnlineAt:     peer.LastOnlineAt,
		})
	}

	// Send notification if we have offline peers
	if len(offlinePeers) > 0 {
		if err := w.sendOfflineNotification(ctx, tenant, offlinePeers); err != nil {
			log.Error().
				Err(err).
				Str("tenant_id", w.tenantID).
				Int("offline_peers", len(offlinePeers)).
				Msg("Failed to send offline notification")
		} else {
			w.dailyNotificationCount++

			// Update peer notification state
			for _, info := range offlinePeers {
				peer, err := w.peerStore.GetPeer(overlayAccountID, info.PeerID)
				if err != nil {
					continue
				}
				peer.OfflineNotificationState = "sent"
				peer.LastNotificationSentAt = now
				_ = w.peerStore.SavePeer(peer)
			}

			log.Debug().
				Str("tenant_id", w.tenantID).
				Int("offline_peers", len(offlinePeers)).
				Msg("📧 Offline peer notification sent")
		}
	}
}

// sendOfflineNotification sends an email notification about offline peers.
func (w *TenantNotificationWorker) sendOfflineNotification(ctx context.Context, tenant *Tenant, offlinePeers []OfflinePeerInfo) error {
	if w.emailSender == nil || !w.emailSender.IsConfigured() {
		log.Warn().Str("tenant_id", w.tenantID).Msg("Email sender not configured")
		return nil
	}

	if w.config.DryRun {
		log.Debug().
			Str("tenant_id", w.tenantID).
			Int("offline_peers", len(offlinePeers)).
			Msg("🧪 [DRY RUN] Would send offline notification")
		return nil
	}

	// Generate tenant-level unsubscribe link (disables all peer notifications)
	unsubscribeURL := ""
	if w.hookCipher != nil && w.hookBaseURL != "" {
		token, err := w.hookCipher.GenerateToken(tenant.ID, tenant.FullName)
		if err != nil {
			log.Warn().
				Err(err).
				Str("tenant_id", tenant.ID).
				Msg("Failed to generate unsubscribe token")
		} else {
			unsubscribeURL = fmt.Sprintf("%s/unsubscribe/%s", w.hookBaseURL, token)
		}
	}

	// Build device list for the new template
	emailPeers := make([]email.EmailPeerInfo, 0, len(offlinePeers))
	for _, info := range offlinePeers {
		reasonText := "Offline"
		statusColor := "#f59e0b" // Amber/Orange
		if info.Reason == OfflineReasonMisconfigured {
			reasonText = "Alert"
			statusColor = "#ef4444" // Red
		}

		emailPeers = append(emailPeers, email.EmailPeerInfo{
			Name:        info.PeerName,
			IP:          info.AssignedIP,
			Reason:      reasonText,
			Duration:    formatDuration(info.OfflineDuration),
			StatusColor: statusColor,
		})
	}

	return w.emailSender.SendPeerOfflineNotification(tenant.Email, tenant.FullName, emailPeers, unsubscribeURL)
}
