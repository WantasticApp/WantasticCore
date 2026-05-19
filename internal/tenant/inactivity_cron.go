// Package tenant provides inactivity tracking and cleanup for free accounts.
package tenant

import (
	"context"
	"fmt"
	"sync"
	"time"

	"WantasticCore/internal/email"

	"github.com/redis/go-redis/v9"
	"github.com/rs/zerolog/log"
)

// InactivityConfig holds configuration for inactivity cron job.
type InactivityConfig struct {
	CheckInterval time.Duration // How often to check for inactive accounts
	WarningAfter  time.Duration // Send warning after this period of inactivity (30 days)
	DeleteAfter   time.Duration // Send a follow-up reminder after this period of inactivity (45 days)
	DryRun        bool          // If true, don't actually delete accounts (for testing)
}

// DefaultInactivityConfig returns the default inactivity configuration.
func DefaultInactivityConfig() InactivityConfig {
	return InactivityConfig{
		CheckInterval: 24 * time.Hour,      // Check once daily
		WarningAfter:  30 * 24 * time.Hour, // 30 days
		DeleteAfter:   45 * 24 * time.Hour, // 45 days
		DryRun:        false,
	}
}

// EmailSender interface for sending email notifications.
type EmailSender interface {
	SendEmail(toEmail, subject, htmlContent, textContent string) error
	SendInactivityWarning(email, fullName string, daysInactive int) error
	SendAccountDeleted(email, fullName string) error
	SendPeerOfflineNotification(email, fullName string, peers []email.EmailPeerInfo, unsubscribeURL string) error
	IsConfigured() bool
}

// AccountCleaner interface for cleaning up overlay accounts when tenant is deleted.
type AccountCleaner interface {
	DeleteAccount(accountID string) error
}

// InactivityCron manages scheduled inactivity checks for free accounts.
type InactivityCron struct {
	registry       Registry
	emailSender    EmailSender
	accountCleaner AccountCleaner
	config         InactivityConfig
	redis          *redis.Client
	stopChan       chan struct{}
	wg             sync.WaitGroup
	running        bool
	mu             sync.RWMutex
}

// NewInactivityCron creates a new inactivity cron service.
func NewInactivityCron(
	registry Registry,
	emailSender EmailSender,
	accountCleaner AccountCleaner,
	config InactivityConfig,
	redis *redis.Client,
) *InactivityCron {
	return &InactivityCron{
		registry:       registry,
		emailSender:    emailSender,
		accountCleaner: accountCleaner,
		config:         config,
		redis:          redis,
		stopChan:       make(chan struct{}),
	}
}

// Start begins the inactivity check cron job.
func (c *InactivityCron) Start() {
	c.mu.Lock()
	if c.running {
		c.mu.Unlock()
		return
	}
	c.running = true
	c.mu.Unlock()

	c.wg.Add(1)
	go c.run()

	log.Debug().
		Dur("check_interval", c.config.CheckInterval).
		Dur("warning_after", c.config.WarningAfter).
		Dur("delete_after", c.config.DeleteAfter).
		Bool("dry_run", c.config.DryRun).
		Msg(" Inactivity cron started")
}

// Stop stops the inactivity check cron job.
func (c *InactivityCron) Stop() {
	c.mu.Lock()
	if !c.running {
		c.mu.Unlock()
		return
	}
	c.running = false
	c.mu.Unlock()

	close(c.stopChan)
	c.wg.Wait()

	log.Debug().Msg("🛑 Inactivity cron stopped")
}

// run is the main cron loop with distributed locking.
func (c *InactivityCron) run() {
	defer c.wg.Done()

	// Initial check
	c.tryCheckInactiveAccounts()

	ticker := time.NewTicker(c.config.CheckInterval)
	defer ticker.Stop()

	for {
		select {
		case <-c.stopChan:
			return
		case <-ticker.C:
			c.tryCheckInactiveAccounts()
		}
	}
}

// tryCheckInactiveAccounts attempts to acquire a Redis lock before running the check.
func (c *InactivityCron) tryCheckInactiveAccounts() {
	if c.redis == nil {
		// Fallback for single node without Redis
		c.checkInactiveAccounts()
		return
	}

	ctx := context.Background()
	lockKey := "wantastic:lock:inactivity_check"
	// Try to acquire lock for 1 hour (less than c.config.CheckInterval likely)
	// If check interval is 24h, 1h is safe.
	lockDuration := 1 * time.Hour
	if c.config.CheckInterval < lockDuration {
		lockDuration = c.config.CheckInterval / 2
	}

	ok, err := c.redis.SetNX(ctx, lockKey, "locked", lockDuration).Result()
	if err != nil {
		log.Error().Err(err).Msg("Failed to acquire Redis lock for inactivity check")
		return
	}

	if !ok {
		log.Debug().Msg(" Inactivity check skipped (lock held by another core)")
		return
	}

	c.checkInactiveAccounts()
}

// checkInactiveAccounts checks all free accounts for inactivity.
func (c *InactivityCron) checkInactiveAccounts() {
	ctx := context.Background()
	now := time.Now()

	log.Debug().Msg(" Checking for inactive free accounts...")

	// Cleanup expired enrollment tokens while we are at it
	if count, err := c.registry.CleanupExpiredEnrollmentTokens(); err == nil && count > 0 {
		log.Debug().Int("count", count).Msg("🧹 Cleaned up expired enrollment tokens")
	}

	tenants, err := c.registry.ListTenants()
	if err != nil {
		log.Error().Err(err).Msg("Failed to list tenants for inactivity check")
		return
	}

	var warned int
	var reminded int

	for _, tenant := range tenants {
		// Skip already deleted/suspended accounts
		if tenant.Status == "deleted" || tenant.Status == "suspended" {
			continue
		}

		// Determine last activity time
		// Use LastLogin if available, otherwise use CreatedAt
		lastActivity := tenant.LastLogin
		if lastActivity.IsZero() {
			lastActivity = tenant.CreatedAt
		}

		inactiveDuration := now.Sub(lastActivity)

		// After the initial warning window, keep sending gentle follow-up reminders
		// instead of deleting the account.
		if inactiveDuration >= c.config.DeleteAfter && tenant.InactivityWarningSentAt != nil {
			daysSinceWarning := now.Sub(*tenant.InactivityWarningSentAt)
			if daysSinceWarning >= 15*24*time.Hour {
				if err := c.sendInactivityWarning(ctx, tenant); err != nil {
					log.Error().
						Err(err).
						Str("tenant_id", tenant.ID).
						Str("email", tenant.Email).
						Msg("Failed to send inactivity follow-up reminder")
				} else {
					reminded++
				}
				continue
			}
		}

		// Check if warning should be sent (30+ days inactive, warning not yet sent)
		if inactiveDuration >= c.config.WarningAfter && tenant.InactivityWarningSentAt == nil {
			if err := c.sendInactivityWarning(ctx, tenant); err != nil {
				log.Error().
					Err(err).
					Str("tenant_id", tenant.ID).
					Str("email", tenant.Email).
					Msg("Failed to send inactivity warning")
			} else {
				warned++
			}
		}
	}

	if warned > 0 || reminded > 0 {
		log.Debug().
			Int("warnings_sent", warned).
			Int("follow_up_reminders", reminded).
			Msg(" Inactivity check completed")
	} else {
		log.Debug().Msg(" No inactive accounts found")
	}
}

// sendInactivityWarning sends an inactivity warning email to a tenant.
func (c *InactivityCron) sendInactivityWarning(ctx context.Context, tenant *Tenant) error {
	if c.emailSender == nil || !c.emailSender.IsConfigured() {
		log.Warn().Str("email", tenant.Email).Msg("Email sender not configured, skipping inactivity warning")
		return nil
	}

	daysInactive := int(time.Since(tenant.LastLogin).Hours() / 24)
	if tenant.LastLogin.IsZero() {
		daysInactive = int(time.Since(tenant.CreatedAt).Hours() / 24)
	}

	if err := c.emailSender.SendInactivityWarning(tenant.Email, tenant.FullName, daysInactive); err != nil {
		return fmt.Errorf("failed to send email: %w", err)
	}

	// Update tenant to record when the most recent reminder was sent.
	now := time.Now()
	tenant.InactivityWarningSentAt = &now
	if err := c.registry.UpdateTenant(tenant); err != nil {
		return fmt.Errorf("failed to update tenant: %w", err)
	}

	log.Debug().
		Str("tenant_id", tenant.ID).
		Str("email", tenant.Email).
		Int("days_inactive", daysInactive).
		Msg(" Inactivity warning sent")

	return nil
}

// deleteInactiveAccount is kept for compatibility with older rollout paths.
// Free accounts are no longer deleted for inactivity.
func (c *InactivityCron) deleteInactiveAccount(ctx context.Context, tenant *Tenant) error {
	_ = ctx
	log.Debug().
		Str("tenant_id", tenant.ID).
		Str("email", tenant.Email).
		Msg("Inactive account retention is enabled; skipping deletion")
	return nil
}

// RunOnce performs a single inactivity check (for testing or manual trigger).
func (c *InactivityCron) RunOnce() {
	c.checkInactiveAccounts()
}
