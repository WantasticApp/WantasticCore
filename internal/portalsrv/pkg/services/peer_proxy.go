package services

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	pb "WantasticCore/internal/types"
	core "WantasticCore/internal/core"

	"github.com/rs/zerolog/log"
)

func (p *TenantProxy) resolvePeerRoute(ctx context.Context, session *TenantSession, peerID string) (string, *pb.GetTenantPeerResponse, error) {
	if peerID == "" {
		return "", nil, fmt.Errorf("peer_id is required for routing")
	}

	peerResp, err := p.services.TenantPortal.GetTenantPeer(ctx, &pb.GetTenantPeerRequest{
		TenantId: session.TenantID,
		PeerId:   peerID,
	})
	if err != nil {
		return "", nil, fmt.Errorf("failed to resolve peer owner: %w", err)
	}
	if peerResp.Peer == nil || peerResp.Peer.AccountId == "" {
		return "", nil, fmt.Errorf("peer owner account not found")
	}

	return peerResp.Peer.AccountId, peerResp, nil
}

// handlePeerService handles PeerService calls for port scanning control
func (p *TenantProxy) handlePeerService(ctx context.Context, msg *Message, session *TenantSession) *Response {
	// Require authentication
	if session.TenantID == "" {
		return errorResponse(msg.ID, fmt.Errorf("authentication required"))
	}

	// All peer-bound calls land on the single in-process PeerService.
	// peerID is kept in the signature for future multi-hub routing, but
	// today it's just a presence check.
	getPeerSvc := func(peerID string) (core.PeerService, error) {
		if peerID == "" {
			return nil, fmt.Errorf("peer_id is required for routing")
		}
		if p.services == nil || p.services.Peer == nil {
			return nil, fmt.Errorf("PeerService not configured")
		}
		return p.services.Peer, nil
	}

	switch msg.Method {
	case "StartPortScan":
		if err := session.checkSharePerm("manage_peers"); err != nil {
			return errorResponse(msg.ID, err)
		}
		var req pb.StartPortScanRequest
		if err := json.Unmarshal(msg.Request, &req); err != nil {
			return errorResponse(msg.ID, fmt.Errorf("invalid request: %s", err.Error()))
		}

		overlayAccountID, _, err := p.resolvePeerRoute(ctx, session, req.PeerId)
		if err != nil {
			return errorResponse(msg.ID, err)
		}
		req.AccountId = overlayAccountID

		// Route to the node managing this peer
		client, err := getPeerSvc(req.PeerId)
		if err != nil {
			return errorResponse(msg.ID, err)
		}

		resp, err := client.StartPortScan(ctx, &req)
		if err != nil {
			return errorResponse(msg.ID, fmt.Errorf("failed to start port scan: %w", err))
		}
		return protoResponse(msg.ID, resp)

	case "StopPortScan":
		if err := session.checkSharePerm("manage_peers"); err != nil {
			return errorResponse(msg.ID, err)
		}
		var req pb.StopPortScanRequest
		if err := json.Unmarshal(msg.Request, &req); err != nil {
			return errorResponse(msg.ID, fmt.Errorf("invalid request: %s", err.Error()))
		}

		overlayAccountID, _, err := p.resolvePeerRoute(ctx, session, req.PeerId)
		if err != nil {
			return errorResponse(msg.ID, err)
		}
		req.AccountId = overlayAccountID

		// Route to the node managing this peer
		client, err := getPeerSvc(req.PeerId)
		if err != nil {
			return errorResponse(msg.ID, err)
		}

		resp, err := client.StopPortScan(ctx, &req)
		if err != nil {
			return errorResponse(msg.ID, fmt.Errorf("failed to stop port scan: %w", err))
		}
		return protoResponse(msg.ID, resp)

	case "PausePortScan":
		if err := session.checkSharePerm("manage_peers"); err != nil {
			return errorResponse(msg.ID, err)
		}
		var req pb.PausePortScanRequest
		if err := json.Unmarshal(msg.Request, &req); err != nil {
			return errorResponse(msg.ID, fmt.Errorf("invalid request: %s", err.Error()))
		}

		overlayAccountID, _, err := p.resolvePeerRoute(ctx, session, req.PeerId)
		if err != nil {
			return errorResponse(msg.ID, err)
		}
		req.AccountId = overlayAccountID

		// Route to the node managing this peer
		client, err := getPeerSvc(req.PeerId)
		if err != nil {
			return errorResponse(msg.ID, err)
		}

		resp, err := client.PausePortScan(ctx, &req)
		if err != nil {
			return errorResponse(msg.ID, fmt.Errorf("failed to pause port scan: %w", err))
		}
		return protoResponse(msg.ID, resp)

	case "ResumePortScan":
		if err := session.checkSharePerm("manage_peers"); err != nil {
			return errorResponse(msg.ID, err)
		}
		var req pb.ResumePortScanRequest
		if err := json.Unmarshal(msg.Request, &req); err != nil {
			return errorResponse(msg.ID, fmt.Errorf("invalid request: %s", err.Error()))
		}

		overlayAccountID, _, err := p.resolvePeerRoute(ctx, session, req.PeerId)
		if err != nil {
			return errorResponse(msg.ID, err)
		}
		req.AccountId = overlayAccountID

		// Route to the node managing this peer
		client, err := getPeerSvc(req.PeerId)
		if err != nil {
			return errorResponse(msg.ID, err)
		}

		resp, err := client.ResumePortScan(ctx, &req)
		if err != nil {
			return errorResponse(msg.ID, fmt.Errorf("failed to resume port scan: %w", err))
		}
		return protoResponse(msg.ID, resp)

	case "StreamPortScanStatus":
		if err := session.checkSharePerm("view_peers"); err != nil {
			return errorResponse(msg.ID, err)
		}
		var req pb.StreamPortScanStatusRequest
		if err := json.Unmarshal(msg.Request, &req); err != nil {
			return errorResponse(msg.ID, fmt.Errorf("invalid request: %s", err.Error()))
		}

		// Resolve peer ownership and get overlay account ID
		overlayAccountID := ""
		if req.PeerId != "" {
			accID, _, err := p.resolvePeerRoute(ctx, session, req.PeerId)
			if err != nil {
				return errorResponse(msg.ID, err)
			}
			overlayAccountID = accID
		}
		req.AccountId = overlayAccountID

		// Route to the core that owns this peer (strict routing)
		client, err := getPeerSvc(req.PeerId)
		if err != nil {
			return errorResponse(msg.ID, err)
		}

		// Open gRPC server-stream — same pattern as StreamPing.
		// Use a detached context so the stream outlives the request handler.
		streamCtx, streamCancel := context.WithTimeout(context.Background(), 35*time.Minute)
		session.registerStream(msg.ID, streamCancel)

		// In-process server-stream — see local_stream.go. Each update
		// travels as *pb.PortScanStatusUpdate through a Go channel; no
		// proto Marshal/Unmarshal between the handler and this goroutine.
		stream := NewLocalServerStream[pb.PortScanStatusUpdate](streamCtx, 32)
		reqPtr := &req
		go func() {
			if err := client.StreamPortScanStatus(reqPtr, stream); err != nil && streamCtx.Err() == nil {
				log.Warn().Err(err).Msg("StreamPortScanStatus handler exited")
			}
			stream.Close()
		}()

		go func() {
			defer streamCancel()
			defer session.unregisterStream(msg.ID)

			p.sendResponse(session, &Response{ID: msg.ID, Type: "stream_started"})

			for {
				update, err := stream.Recv()
				if err != nil {
					break
				}
				jsonData := marshalProtoResponse(update)
				p.sendResponse(session, &Response{ID: msg.ID, Type: "stream_data", Response: jsonData})

				// End stream on terminal states
				if update.Status == "completed" || update.Status == "failed" {
					break
				}
			}
			p.sendResponse(session, &Response{ID: msg.ID, Type: "stream_end"})
		}()

		return nil // streaming — no immediate response

	default:
		return errorResponse(msg.ID, fmt.Errorf("unknown PeerService method: %s", msg.Method))
	}
}

// streamPortScanStatus removed — now uses direct gRPC server-streaming
// (same pattern as StreamPing). No more Redis Pub/Sub in the proxy layer.
