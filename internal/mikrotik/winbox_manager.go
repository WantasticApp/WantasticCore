package mikrotik

import (
	"WantasticCore/internal/wg/userspace"
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/rs/zerolog/log"
)

// WinboxManager manages Winbox multiplexers for all tenants
// Follows DirectSSHHandler pattern - uses UserspaceManager and device.Net for tenant network access
type WinboxManager struct {
	manager      *userspace.UserspaceManager
	multiplexers sync.Map // key: accountID, value: *WinboxMultiplexer

	// clearSessionsFunc deletes all WinboxSessions for a peer (injected from PeerStore)
	clearSessionsFunc func(accountID, peerID string) error
}

// NewWinboxManager creates a new Winbox manager following DirectSSHHandler pattern
func NewWinboxManager(manager *userspace.UserspaceManager) *WinboxManager {
	return &WinboxManager{
		manager: manager,
	}
}

// SetClearSessionsFunc injects the function to delete all WinboxSessions for a peer
func (m *WinboxManager) SetClearSessionsFunc(clearFunc func(accountID, peerID string) error) {
	m.clearSessionsFunc = clearFunc
}

// DetectWinboxPort scans for Winbox port 8291 on a peer using tenant's network
// Follows ping.go pattern: uses device.Net.DialContext with tenant's network stack
func (m *WinboxManager) DetectWinboxPort(accountID, peerIP string, timeout time.Duration) bool {
	// Get tenant device (same as DirectSSHHandler does)
	device, err := m.manager.GetDevice(accountID)
	if err != nil {
		log.Warn().
			Str("account_id", accountID).
			Err(err).
			Msg("Failed to get tenant device for Winbox port detection")
		return false
	}

	// Use tenant's network stack to connect (same as ping.go tcpProbe)
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	target := fmt.Sprintf("%s:8291", peerIP)
	conn, err := device.Net.DialContext(ctx, "tcp", target)
	if err != nil {
		// Connection failed - port closed or peer unreachable
		return false
	}

	// Port is open
	conn.Close()
	return true
}

// ClearWinboxCredentials removes all WinboxSessions for a peer
// This is used when the user wants to remove all Winbox access for a device
func (m *WinboxManager) ClearWinboxCredentials(accountID, peerID string) error {
	if m.clearSessionsFunc == nil {
		return fmt.Errorf("clearSessionsFunc not initialized - call SetClearSessionsFunc first")
	}

	if err := m.clearSessionsFunc(accountID, peerID); err != nil {
		return fmt.Errorf("failed to clear WinboxSessions: %w", err)
	}

	log.Debug().
		Str("account_id", accountID).
		Str("peer_id", peerID).
		Msg(" All WinboxSessions cleared for peer")

	return nil
}

// Close stops all multiplexers
func (m *WinboxManager) Close() error {
	var lastErr error
	m.multiplexers.Range(func(key, value interface{}) bool {
		if mux, ok := value.(*WinboxMultiplexer); ok {
			if err := mux.Stop(); err != nil {
				lastErr = err
				log.Warn().Err(err).Str("account_id", key.(string)).Msg("Failed to stop Winbox multiplexer")
			}
		}
		return true
	})
	return lastErr
}

// GetActiveSessionCount returns the number of active Winbox multiplexers.
func (m *WinboxManager) GetActiveSessionCount() int {
	count := 0
	m.multiplexers.Range(func(key, value interface{}) bool {
		count++
		return true
	})
	return count
}
