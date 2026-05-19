package services

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/rs/zerolog/log"
)

// ConnectionStats represents user presence statistics
type ConnectionStats struct {
	InstanceID    string         `json:"instance_id"`
	TotalSessions int            `json:"total_sessions"`
	TenantCounts  map[string]int `json:"tenant_counts"`
	Timestamp     time.Time      `json:"timestamp"`
	// Additional detailed stats could go here if needed
}

// statsPulseWorker periodically publishes connection statistics to Redis.
func (p *TenantProxy) statsPulseWorker() {
	if p.redisClient == nil {
		return
	}

	ticker := time.NewTicker(10 * time.Second)
	defer ticker.Stop()

	log.Debug().Str("instance_id", p.instanceID).Msg("💓 Stats pulse worker started")

	for {
		select {
		case <-p.ctx.Done():
			return
		case <-ticker.C:
			p.publishStats()
		}
	}
}

// publishStats collects and publishes current connection stats.
func (p *TenantProxy) publishStats() {
	p.sessionsMu.RLock()
	totalSessions := len(p.sessions)
	tenantCounts := make(map[string]int)

	for _, session := range p.sessions {
		if session.TenantID != "" {
			tenantCounts[session.TenantID]++
		} else {
			tenantCounts["anonymous"]++
		}
	}
	p.sessionsMu.RUnlock()

	stats := ConnectionStats{
		InstanceID:    p.instanceID,
		TotalSessions: totalSessions,
		TenantCounts:  tenantCounts,
		Timestamp:     time.Now().UTC(),
	}

	data, err := json.Marshal(stats)
	if err != nil {
		log.Error().Err(err).Msg("Failed to marshal connection stats")
		return
	}

	// Publish to Redis
	// Key: portal:stats:{instanceID}
	// Expiration: 30 seconds (if portal dies, stats disappear)
	key := fmt.Sprintf("portal:stats:%s", p.instanceID)
	if err := p.redisClient.Set(context.Background(), key, data, 30*time.Second).Err(); err != nil {
		log.Warn().Err(err).Msg("Failed to update stats in Redis")
	}

	// Also publish to pub/sub for real-time dashboards
	if err := p.redisClient.Publish(context.Background(), "portal:events:stats", data).Err(); err != nil {
		log.Warn().Err(err).Msg("Failed to publish stats event")
	}
}
