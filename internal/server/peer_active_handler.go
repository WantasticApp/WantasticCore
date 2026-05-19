package server

import (
	"context"
	"fmt"
	"time"

	"github.com/rs/zerolog/log"
)

// handlePeerActive is invoked when a peer's WireGuard handshake response is
// sent (i.e. the peer became active for this tenant). It refreshes the Redis
// routing entries so that follow-up traffic and WUSP control plane messages
// can find the peer's hub.
func (s *Server) handlePeerActive(tenantID string, publicKey string) {
	if s.redisClient == nil {
		return
	}

	ctx := context.Background()
	hubID := s.getHubID()
	pipe := s.redisClient.Pipeline()
	pipe.Set(ctx, "hub_addr:"+hubID, s.GetHubAddress(), hubRouteTTL)

	// Peer ID → Hub ID
	pipe.Set(ctx, "online_peer:"+publicKey, hubID, peerRouteTTL)
	pipe.Set(ctx, "last_handshake:"+publicKey, fmt.Sprintf("%d", time.Now().UnixMilli()), peerRouteTTL)

	// Peer IP → Hub ID for each allowed IP
	s.mu.RLock()
	device, ok := s.tenantDevices[tenantID]
	s.mu.RUnlock()

	var allowedIPs []string
	if ok {
		if status, err := device.GetPeerStatus(publicKey); err == nil {
			allowedIPs = status.AllowedIPs
			for _, allowedIP := range allowedIPs {
				ip := allowedIP
				for i := 0; i < len(allowedIP); i++ {
					if allowedIP[i] == '/' {
						ip = allowedIP[:i]
						break
					}
				}
				if ip != "" {
					pipe.Set(ctx, "online_ip:"+ip, hubID, peerRouteTTL)
				}
			}
		}
	}

	// Cache accountID → publicKey so WUSP event handlers can bootstrap
	// new devices without hitting the database on every OnBoardRequest.
	pipe.Set(ctx, "wusp_peer_account:"+publicKey, tenantID, 24*time.Hour)

	if _, err := pipe.Exec(ctx); err != nil {
		log.Warn().Err(err).Str("peer_id", publicKey).Msg("Failed to flush peer activity route update to Redis")
	}

	go s.broadcastPeerRoaming(ctx, publicKey)
	for _, allowedIP := range allowedIPs {
		ip := allowedIP
		for i := 0; i < len(allowedIP); i++ {
			if allowedIP[i] == '/' {
				ip = allowedIP[:i]
				break
			}
		}
		if ip != "" {
			ipCopy := ip
			go s.broadcastPeerRoaming(ctx, ipCopy)
		}
	}

	go s.PublishPeerStatusEvent(tenantID, publicKey, true)
}
