package services

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/redis/go-redis/v9"
	"github.com/rs/zerolog/log"

	pb "WantasticCore/internal/types"
)

// normalizeAccountID strips dashes so 32-char hex and UUID formats compare equal.
// e.g. "ddd5cc5d-d26f-4b77-8961-06ef7b140801" → "ddd5cc5dd26f4b77896106ef7b140801"
func normalizeAccountID(id string) string {
	return strings.ReplaceAll(id, "-", "")
}

// subscribeToRedisEvents subscribes to Redis channels for real-time updates.
func (p *TenantProxy) subscribeToRedisEvents(session *TenantSession) {
	// 1. Resolve Overlay Account ID (UUID) from Tenant ID (Slug)
	// The backend publishes events to tenant:<uuid>:* but we have the slug here.
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	var accountID string
	if p.services != nil && p.services.TenantPortal != nil {
		resp, err := p.services.TenantPortal.GetTenantAccount(ctx, &pb.GetTenantAccountRequest{
			TenantId: session.TenantID,
		})

		if err == nil && resp.Account != nil {
			accountID = resp.Account.Id // Use UUID
			log.Debug().Str("tenant_id", session.TenantID).Str("account_id", accountID).Msg(" Resolved Tenant ID to Account UUID for Redis")
		} else {
			log.Warn().Err(err).Str("tenant_id", session.TenantID).Msg("Failed to resolve Tenant ID to Account UUID, falling back to slug")
			accountID = session.TenantID
		}
	} else {
		log.Warn().Msg("TenantPortalService not available for ID resolution")
		accountID = session.TenantID
	}

	// Cache the resolved account UUID on the session for tenant isolation checks
	// on global channels (e.g. peer_status).
	session.AccountID = accountID

	// Channels/Patterns to listen to
	// We listen to BOTH the UUID (for system events like scans) and the Slug (if used elsewhere)
	// or just the UUID if we are sure. To be safe, let's use the resolved ID.
	tenantPattern := fmt.Sprintf("tenant:%s:*", accountID)

	// Subscribe to tenant event channels.
	// Scan progress no longer goes through Redis — it uses gRPC server-streaming
	// via StreamPortScanStatus (same pattern as StreamPing).
	channels := []string{
		"system:updates",
		fmt.Sprintf("tenant:%s:events", session.TenantID),        // Legacy slug channel
		fmt.Sprintf("tenant:%s:events", accountID),               // UUID channel
		fmt.Sprintf("tenant:%s:peer:*:wusp", accountID),          // WUSP live Notify events (UUID)
	}

	globalChannel := "global:device_auth_requests"
	peerStatusChannel := "wantastic:events:peer_status"

	// Add global and peer status channels to the list
	channels = append(channels, globalChannel, peerStatusChannel)

	log.Debug().Strs("channels", channels).Msg(" Subscribing to Redis channels")

	// Use PSubscribe for pattern and Subscribe for literal channel
	pubsub := p.redisClient.PSubscribe(context.Background(), channels...)
	log.Debug().
		Str("session_id", session.ID).
		Str("tenant_id", session.TenantID).
		Str("account_id", accountID).
		Str("pattern", tenantPattern). // This pattern is now covered by the specific channels
		Msg(" WebSocket session subscribing to Redis channels")
	defer pubsub.Close()

	// Wait for confirmation
	_, err := pubsub.Receive(context.Background())
	if err != nil {
		log.Error().Err(err).Str("session_id", session.ID).Msg("Failed to subscribe to Redis events")
		return
	}

	ch := pubsub.Channel()

	log.Debug().
		Str("session_id", session.ID).
		Str("tenant_id", session.TenantID).
		Str("ip", session.IPAddress).
		Msg(" WebSocket session subscribed to Redis events")

	// Read messages
	for msg := range ch {
		// Handle global auth requests if IP matches
		if msg.Channel == globalChannel {
			p.handleGlobalAuthRequest(session, msg)
			continue
		}

		// Handle peer status events
		if msg.Channel == peerStatusChannel {
			p.handlePeerStatusEvent(session, msg)
			continue
		}

		// Handle tenant-specific events
		p.handleRedisMessage(session, msg)
	}
}

// handlePeerStatusEvent processes a peer status change event
func (p *TenantProxy) handlePeerStatusEvent(session *TenantSession, msg *redis.Message) {
	var payload map[string]any
	if err := json.Unmarshal([]byte(msg.Payload), &payload); err != nil {
		return
	}

	// Security Check: Only forward if the event belongs to this tenant.
	// The peer_status channel is global, so we MUST filter by account ownership.
	eventAccountID, _ := payload["account_id"].(string)

	// Compare normalized IDs — account IDs are 32-char hex but tenant/session IDs
	// may be UUIDs with dashes. Strip dashes so both formats match.
	eventNorm := normalizeAccountID(eventAccountID)
	if eventNorm != normalizeAccountID(session.TenantID) && eventNorm != normalizeAccountID(session.AccountID) {
		log.Debug().
			Str("event_account_id", eventAccountID).
			Str("session_tenant_id", session.TenantID).
			Str("session_account_id", session.AccountID).
			Msg("Peer status event dropped: account ID does not match session tenant")
		return
	}

	// Construct client event
	// Frontend expects: { type: "peer_event", payload: { type: "status_change", peerId: "...", isOnline: true ... } }
	event := map[string]any{
		"type": "peer_event",
		"payload": map[string]any{
			"type":      "status_change",
			"peerId":    payload["peer_id"],
			"isOnline":  payload["is_online"],
			"timestamp": payload["timestamp"],
		},
	}

	p.sendEventToSession(session, event)
}

// sendEventToSession sends a raw event map to the session
func (p *TenantProxy) sendEventToSession(session *TenantSession, event map[string]any) {
	session.mu.Lock()
	defer session.mu.Unlock()

	session.Conn.SetWriteDeadline(time.Now().Add(10 * time.Second))

	if session.EncryptionEnabled && session.SessionCipher != nil {
		jsonData, _ := json.Marshal(event)
		if ciphertext, err := session.SessionCipher.EncryptJSON(string(jsonData)); err == nil {
			encryptedMsg := map[string]any{
				"type":       "encrypted",
				"ciphertext": ciphertext,
			}
			session.Conn.WriteJSON(encryptedMsg)
		}
	} else {
		session.Conn.WriteJSON(event)
	}
}

// handleGlobalAuthRequest processes a global device auth request and forwards it if the IP matches.
func (p *TenantProxy) handleGlobalAuthRequest(session *TenantSession, msg *redis.Message) {
	var raw map[string]any
	if err := json.Unmarshal([]byte(msg.Payload), &raw); err != nil {
		return
	}

	// Support both nested payload and flat structure
	payload, ok := raw["payload"].(map[string]any)
	if !ok {
		payload = raw
	}

	// Filter by IP Address to ensure the popup only appears for the relevant user
	eventIP, _ := payload["client_ip"].(string)
	log.Debug().
		Str("session_id", session.ID).
		Str("session_ip", session.IPAddress).
		Str("event_ip", eventIP).
		Msg(" Comparing IP for device auth request")

	// Lenient IP match for localhost in development
	isMatch := eventIP == session.IPAddress
	if !isMatch && (eventIP == "127.0.0.1" || eventIP == "::1") && (session.IPAddress == "127.0.0.1" || session.IPAddress == "::1") {
		isMatch = true
	}

	if eventIP != "" && isMatch {
		log.Debug().
			Str("session_id", session.ID).
			Str("device_id", fmt.Sprintf("%v", payload["device_id"])).
			Str("user_code", fmt.Sprintf("%v", payload["user_code"])).
			Msg(" Matching device auth request found, forwarding to WebSocket")

		p.sendEvent(session, "device_auth_popup", payload)
	}
}

// sendEvent is a helper to send an event to the client
func (p *TenantProxy) sendEvent(session *TenantSession, eventType string, payload any) {
	event := map[string]any{
		"type": "event",
		"payload": map[string]any{
			"type": eventType,
			"data": payload,
		},
	}

	session.mu.Lock()
	defer session.mu.Unlock()

	session.Conn.SetWriteDeadline(time.Now().Add(10 * time.Second))

	if session.EncryptionEnabled && session.SessionCipher != nil {
		jsonData, _ := json.Marshal(event)
		if ciphertext, err := session.SessionCipher.EncryptJSON(string(jsonData)); err == nil {
			encryptedMsg := map[string]any{
				"type":       "encrypted",
				"ciphertext": ciphertext,
			}
			session.Conn.WriteJSON(encryptedMsg)
		}
	} else {
		session.Conn.WriteJSON(event)
	}
}

// handleRedisMessage processes a message from Redis and forwards it to the WebSocket.
func (p *TenantProxy) handleRedisMessage(session *TenantSession, msg *redis.Message) {
	// Parse the message payload
	var payload map[string]any
	if err := json.Unmarshal([]byte(msg.Payload), &payload); err != nil {
		log.Warn().Err(err).Str("payload", msg.Payload).Msg("Failed to parse Redis message")
		return
	}

	// Determine event type from the payload's "type" field.
	// Scan progress no longer comes through Redis (uses gRPC streaming).
	eventType := "event"
	if t, ok := payload["type"].(string); ok {
		eventType = t
	}

	// Filter WUSP live Notify events — only forward to sessions subscribed to the peer.
	if eventType == "wusp_notify" {
		peerID, _ := payload["peer_id"].(string)
		if peerID != "" {
			session.mu.Lock()
			subscribed := session.WUSPSubscribedPeers != nil && session.WUSPSubscribedPeers[peerID]
			session.mu.Unlock()
			if !subscribed {
				return
			}
		}
	}

	// Construct the client-facing event
	event := map[string]any{
		"type": "peer_event",
		"payload": map[string]any{
			"type":   eventType,
			"peerId": payload["peer_id"],
			"data":   payload,
		},
	}

	// Send to client
	session.mu.Lock()
	defer session.mu.Unlock()

	// Check if we can write
	session.Conn.SetWriteDeadline(time.Now().Add(10 * time.Second))

	if session.EncryptionEnabled && session.SessionCipher != nil {
		// Encrypt event
		jsonData, _ := json.Marshal(event)
		if ciphertext, err := session.SessionCipher.EncryptJSON(string(jsonData)); err == nil {
			encryptedMsg := map[string]any{
				"type":       "encrypted",
				"ciphertext": ciphertext,
			}
			session.Conn.WriteJSON(encryptedMsg)
		}
	} else {
		session.Conn.WriteJSON(event)
	}
}
