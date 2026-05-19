package server

import (
	"context"
	"fmt"
	"runtime/pprof"
	"strings"
	"time"

	"github.com/rs/zerolog/log"
)

// StartUptimeMonitor starts background tasks to record peer handshake history and announce presence.
// It maintains two loops:
// 1. History Monitor (5 min): Records handshakes to DB (slow, expensive).
// 2. Presence Monitor (15 sec): Announces online peers to Redis for routing (fast).
func (s *Server) StartUptimeMonitor(ctx context.Context) {
	// History recording interval (5 minutes)
	historyTicker := time.NewTicker(90 * time.Second)
	// Presence announcement interval (15 seconds - for "sticky" routing)
	presenceTicker := time.NewTicker(5 * time.Second)

	// Cache to avoid duplicate DB writes for the same handshake event
	// map[peerPublicKey]lastRecordedTime
	lastRecorded := make(map[string]time.Time)

	// Tracks which peers were online last cycle so we can detect departures.
	// map[peerPublicKey]accountID
	prevOnlinePeers := make(map[string]string)

	log.Debug().Msg(" Starting background Uptime Monitor (Handshake Recorder + Presence Announcer)")

	s.wg.Add(1)
	go func() {
		defer s.wg.Done()
		defer historyTicker.Stop()
		defer presenceTicker.Stop()

		// Run immediately on start (inline — avoids two extra untracked goroutines)
		s.recordHandshakes(lastRecorded)
		s.announcePeersPresence(prevOnlinePeers)

		for {
			select {
			case <-ctx.Done():
				log.Debug().Msg("🛑 Uptime Monitor stopped")
				return
			case <-historyTicker.C:
				pprof.Do(ctx, pprof.Labels("goroutine", "uptime-record-handshakes"), func(ctx context.Context) {
					s.recordHandshakes(lastRecorded)
				})
			case <-presenceTicker.C:
				pprof.Do(ctx, pprof.Labels("goroutine", "uptime-announce-presence"), func(ctx context.Context) {
					s.announcePeersPresence(prevOnlinePeers)
				})
			}
		}
	}()
}

// recordHandshakes checks for new handshakes and saves them to the database.
func (s *Server) recordHandshakes(lastRecorded map[string]time.Time) {
	start := time.Now()
	recordedCount := 0
	errorCount := 0
	activeDeviceCount := 0

	// activePubKeys tracks every peer public key seen this cycle.
	// After the loop we prune lastRecorded to remove deleted peers and prevent unbounded growth.
	activePubKeys := make(map[string]struct{}, len(lastRecorded))

	s.mu.RLock()
	// Iterate over all tenant devices
	for accountID, device := range s.tenantDevices {
		// Process each device
		peers, err := device.GetAllPeersStatus()
		if err != nil {
			log.Warn().Err(err).Str("account_id", accountID).Msg("Failed to get peer status for uptime monitoring")
			continue
		}

		for pubKey, status := range peers {
			activePubKeys[pubKey] = struct{}{}

			// Skip if never handshaked or handshake is very old (e.g. > 1 week)
			if status.LastHandshakeTime.IsZero() {
				continue
			}

			// Check if we already recorded this specific handshake
			lastRec, exists := lastRecorded[pubKey]

			// We want to record a history point if the device IS ONLINE now, or if it had a NEW handshake.
			// If it's online, we record the CURRENT TIME as a heartbeat to ensure the uptime chart stays green.
			// If it's offline but had a new handshake we hadn't seen yet, we also record that handshake time.
			var recordTime time.Time

			if status.IsOnline {
				// Record presence heartbeat (time.Now) to keep UI uptime chart smooth
				recordTime = time.Now()
			} else {
				// Only record if it's a completely NEW offline handshake we missed
				if exists && !status.LastHandshakeTime.After(lastRec) {
					continue // Already recorded
				}
				recordTime = status.LastHandshakeTime
			}

			// Capture the Public IP (Endpoint)
			endpoint := status.Endpoint

			// Record to DB
			if err := s.peerStore.RecordHandshake(accountID, pubKey, recordTime, endpoint); err != nil {
				log.Error().Err(err).Str("peer_id", pubKey).Msg("Failed to record handshake history")
				errorCount++
			} else {
				recordedCount++
				lastRecorded[pubKey] = status.LastHandshakeTime

				// Update Peer status for UI/API consistency
				err := s.peerStore.UpdatePeerStatus(accountID, pubKey, status.LastHandshakeTime, endpoint, status.IsOnline)
				if err != nil {
					log.Warn().Err(err).Str("peer_id", pubKey).Msg("Failed to update peer status metadata")
				}
			}
		}
	}
	s.mu.RUnlock()

	// Prune lastRecorded for peers that no longer exist, preventing unbounded growth.
	for pubKey := range lastRecorded {
		if _, alive := activePubKeys[pubKey]; !alive {
			delete(lastRecorded, pubKey)
		}
	}

	if recordedCount > 0 {
		log.Debug().
			Int("recorded_handshakes", recordedCount).
			Int("errors", errorCount).
			Int("active_devices", activeDeviceCount).
			Str("duration", time.Since(start).String()).
			Msg("📝 Uptime Monitor: Recorded new handshakes")
	}
}

// GetHubAddress determines the reachable address of this hub for inter-hub communication.
func (s *Server) GetHubAddress() string {
	hubAddr := s.config.AdvertiseAddr
	if hubAddr == "" {
		// Prefer Hostname (HubID) for internal container-to-container communication
		if s.hubID != "" && !strings.HasPrefix(s.hubID, "wantastic-hub-") {
			// e.g. "wantastic-server-1"
			hubAddr = s.hubID
			// Append GRPC port for consistency if needed by other consumers, though Winbox strips it
			if strings.HasPrefix(s.config.GRPCAddr, ":") {
				hubAddr = hubAddr + s.config.GRPCAddr
			} else {
				hubAddr = hubAddr + ":50051"
			}
		} else {
			// Fallback to ServerEndpoint (Public URL) - risky for internal routing if loopback fails
			hubAddr = s.config.ServerEndpoint
			// Ensure port is included if missing
			if !strings.Contains(hubAddr, ":") {
				// Append GRPC Addr (e.g., ":50051")
				if strings.HasPrefix(s.config.GRPCAddr, ":") {
					hubAddr = hubAddr + s.config.GRPCAddr
				} else {
					// Fallback: assume default port if GRPCAddr is weird
					hubAddr = hubAddr + ":50051"
				}
			}
		}

		if strings.HasPrefix(hubAddr, "localhost") {
			hubAddr = "local-hub"
		}
	}
	return hubAddr
}

// getHubID returns the unique ID of this hub
func (s *Server) getHubID() string {
	if s.hubID != "" {
		return s.hubID
	}
	// Fallback if hubID not set (should not happen in prod)
	return "unknown-hub"
}

// announcePeersPresence checks online peers and updates their location in Redis.
// This allows the Tenant Proxy to route requests (WebSSH/Winbox) to the correct server.
// It sets:
// 1. hub_addr:<hub_id> -> <hub_grpc_address>
// 2. online_peer:<peer_id> -> <hub_id>
// 3. online_ip:<peer_ip> -> <hub_id>
//
// prevOnlinePeers tracks which peers were online last cycle (pubKey -> accountID).
// It is updated in-place so the caller can pass the same map across calls.
func (s *Server) announcePeersPresence(prevOnlinePeers map[string]string) {
	if s.redisClient == nil {
		return
	}

	type peerRouteAnnouncement struct {
		peerID        string
		ips           []string
		lastHandshake time.Time
	}

	start := time.Now()
	announcedCount := 0

	// Pre-calculate Hub Address and ID once
	hubAddr := s.GetHubAddress()
	hubID := s.getHubID()

	ctx := context.Background()

	// currentOnlinePeers tracks every peer seen online this cycle.
	currentOnlinePeers := make(map[string]string, len(prevOnlinePeers))
	announcements := make([]peerRouteAnnouncement, 0, len(prevOnlinePeers))

	s.mu.RLock()
		for accountID, device := range s.tenantDevices {
			// Presence is the routing source of truth. Use a fresh IPC snapshot so
			// a just-completed handshake is not overwritten by a stale cached
			// "offline" read on the next 5s announcer tick.
			peers, err := device.GetAllPeersStatusFresh()
			if err != nil {
				continue
			}

		for pubKey, status := range peers {
			if status.IsOnline {
				currentOnlinePeers[pubKey] = accountID
				ips := make([]string, 0, len(status.AllowedIPs))
				for _, allowedIP := range status.AllowedIPs {
					ip := strings.Split(allowedIP, "/")[0]
					if ip != "" {
						ips = append(ips, ip)
					}
				}
				announcements = append(announcements, peerRouteAnnouncement{peerID: pubKey, ips: ips, lastHandshake: status.LastHandshakeTime})

				announcedCount++

				// Also update last seen cache to keep dashboard fresh (15s interval)
				if !status.LastHandshakeTime.IsZero() {
					s.peerStore.UpdatePeerLastSeenCache(pubKey, status.LastHandshakeTime)
				}
			}
		}
	}
	s.mu.RUnlock()

	pipe := s.redisClient.Pipeline()
	queuedWrites := 0
	flush := func() {
		if queuedWrites == 0 {
			return
		}
		if _, err := pipe.Exec(ctx); err != nil {
			log.Warn().Err(err).Int("queued_writes", queuedWrites).Msg("Failed to flush peer presence batch to Redis")
		}
		pipe = s.redisClient.Pipeline()
		queuedWrites = 0
	}
	queueSet := func(key, value string, ttl time.Duration) {
		pipe.Set(ctx, key, value, ttl)
		queuedWrites++
		if queuedWrites >= redisRouteBatchSize {
			flush()
		}
	}

	// Announce Hub Address first so peer -> hub mappings always resolve to a live address.
	queueSet("hub_addr:"+hubID, hubAddr, hubRouteTTL)
	for _, announcement := range announcements {
		queueSet("online_peer:"+announcement.peerID, hubID, peerRouteTTL)
		if !announcement.lastHandshake.IsZero() {
			queueSet("last_handshake:"+announcement.peerID,
				fmt.Sprintf("%d", announcement.lastHandshake.UnixMilli()),
				peerRouteTTL)
		}
		for _, ip := range announcement.ips {
			queueSet("online_ip:"+ip, hubID, peerRouteTTL)
		}
	}
	flush()

	// Detect peers that were online last cycle but are not online now → publish offline event.
	for pubKey, accountID := range prevOnlinePeers {
		if _, stillOnline := currentOnlinePeers[pubKey]; !stillOnline {
			go s.PublishPeerStatusEvent(accountID, pubKey, false)
		}
	}

	// Replace previous set with current set for next cycle.
	for k := range prevOnlinePeers {
		delete(prevOnlinePeers, k)
	}
	for k, v := range currentOnlinePeers {
		prevOnlinePeers[k] = v
	}

	if announcedCount > 0 {
		// Log occasionally to avoid spam, or on debug
		if time.Since(start) > 100*time.Millisecond {
			log.Debug().
				Int("announced_peers", announcedCount).
				Str("hub_id", hubID).
				Str("hub_addr", hubAddr).
				Str("duration", time.Since(start).String()).
				Msg(" Peering Presence Announced")
		}
	}
}
