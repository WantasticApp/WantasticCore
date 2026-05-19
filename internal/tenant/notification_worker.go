// Package tenant provides peer offline notification services.
package tenant

import (
	"fmt"
	"time"
)

// OfflineReason describes why a peer is considered offline
type OfflineReason string

const (
	// OfflineReasonGoneOffline - peer was online before but stopped responding
	OfflineReasonGoneOffline OfflineReason = "gone_offline"
	// OfflineReasonMisconfigured - peer has failed handshakes (misconfigured)
	OfflineReasonMisconfigured OfflineReason = "misconfigured"
)

// NotificationConfig holds configuration for the notification worker.
type NotificationConfig struct {
	CheckInterval       time.Duration // How often to check for offline peers
	OfflineThreshold    time.Duration // How long a peer must be offline before notification (5 min)
	CooldownPeriod      time.Duration // Minimum time between notifications for same peer (1 hour)
	FailedHandshakeMax  int           // Number of failed handshakes to consider as misconfigured
	DryRun              bool          // If true, don't actually send notifications (for testing)
	MaxNotificationsDay int           // Maximum notifications per tenant per day (0 = unlimited)
}

// DefaultNotificationConfig returns the default notification configuration.
func DefaultNotificationConfig() NotificationConfig {
	return NotificationConfig{
		CheckInterval:       1 * time.Minute, // Check every minute
		OfflineThreshold:    5 * time.Minute, // 5 minutes offline before notification
		CooldownPeriod:      1 * time.Hour,   // 1 hour between notifications for same peer
		FailedHandshakeMax:  3,               // 3 failed handshakes = misconfigured
		DryRun:              false,
		MaxNotificationsDay: 1, // Max 1 notification per tenant per day
	}
}

// OfflinePeerInfo contains information about an offline peer for notification
type OfflinePeerInfo struct {
	PeerID           string
	PeerName         string
	AssignedIP       string
	TenantID         string
	TenantEmail      string
	TenantName       string
	OfflineSince     time.Time
	OfflineDuration  time.Duration
	Reason           OfflineReason
	FailedHandshakes int
	LastOnlineAt     time.Time
}

// formatDuration formats a duration in a human-readable way.
func formatDuration(d time.Duration) string {
	if d < time.Minute {
		return fmt.Sprintf("%d seconds", int(d.Seconds()))
	}
	if d < time.Hour {
		return fmt.Sprintf("%d minutes", int(d.Minutes()))
	}
	if d < 24*time.Hour {
		hours := int(d.Hours())
		minutes := int(d.Minutes()) % 60
		if minutes > 0 {
			return fmt.Sprintf("%dh %dm", hours, minutes)
		}
		return fmt.Sprintf("%d hours", hours)
	}
	days := int(d.Hours() / 24)
	hours := int(d.Hours()) % 24
	if hours > 0 {
		return fmt.Sprintf("%dd %dh", days, hours)
	}
	return fmt.Sprintf("%d days", days)
}
