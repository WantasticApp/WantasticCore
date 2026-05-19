package services

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	pb "WantasticCore/internal/types"

	"github.com/gorilla/websocket"
	"github.com/rs/zerolog/log"
)

type RouterOSStreamHandler struct {
	sessionID string
	peerID    string
	accountID string
	stream    *LocalBidiStreamClient[pb.StreamRouterOSDashboardRequest, pb.StreamRouterOSDashboardEvent]
	cancel    context.CancelFunc
	active    bool
	inputCh   chan *pb.StreamRouterOSDashboardRequest
	mu        sync.Mutex
}

func (h *RouterOSStreamHandler) canAcceptInput() bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.active && h.inputCh != nil
}

type routerOSStreamWSMessage struct {
	Type      string                  `json:"type"`
	SessionID string                  `json:"session_id"`
	Payload   routerOSStreamWSPayload `json:"payload"`
}

type routerOSStreamWSPayload struct {
	State    *pb.RouterOSDashboardState   `json:"state,omitempty"`
	Resource *pb.RouterOSResourceSnapshot `json:"resource,omitempty"`
	Notice   *pb.RouterOSMutationNotice   `json:"notice,omitempty"`
	Error    string                       `json:"error,omitempty"`
	Close    bool                         `json:"close,omitempty"`
}

type routerOSStreamRequestPayload struct {
	Open            *pb.OpenRouterOSDashboardRequest    `json:"open,omitempty"`
	LoadResource    *pb.LoadRouterOSResourceRequest     `json:"load_resource,omitempty"`
	Refresh         *pb.RefreshRouterOSDashboardRequest `json:"refresh,omitempty"`
	ConfigureAccess *pb.ConfigureRouterOSAccessRequest  `json:"configure_access,omitempty"`
	AddResource     *pb.MutateRouterOSResourceRequest   `json:"add_resource,omitempty"`
	UpdateResource  *pb.MutateRouterOSResourceRequest   `json:"update_resource,omitempty"`
	DeleteResource  *pb.DeleteRouterOSResourceRequest   `json:"delete_resource,omitempty"`
}

// handleRouterOSService routes RouterOSService websocket calls to the hub that
// currently owns the peer's WireGuard session, following the same pattern used
// by WUSP.
func (p *TenantProxy) handleRouterOSService(ctx context.Context, msg *Message, session *TenantSession) *Response {
	if session.TenantID == "" {
		return errorResponse(msg.ID, fmt.Errorf("authentication required"))
	}

	timeout := 20 * time.Second
	var cancel context.CancelFunc
	ctx, cancel = context.WithTimeout(ctx, timeout)
	defer cancel()

	switch msg.Method {
	case "GetOverview":
		if err := session.checkSharePerm("view_peers"); err != nil {
			return errorResponse(msg.ID, err)
		}
		var req pb.GetRouterOSOverviewRequest
		if err := json.Unmarshal(msg.Request, &req); err != nil {
			return errorResponse(msg.ID, err)
		}
		req.AccountId = session.GetEffectiveTenantID()
		resp, err := p.services.RouterOS.GetOverview(ctx, &req)
		if err != nil {
			return errorResponse(msg.ID, err)
		}
		return protoResponse(msg.ID, resp)

	case "ConfigureAccess":
		if err := session.checkSharePerm("manage_winbox"); err != nil {
			return errorResponse(msg.ID, err)
		}
		var req pb.ConfigureRouterOSAccessRequest
		if err := json.Unmarshal(msg.Request, &req); err != nil {
			return errorResponse(msg.ID, err)
		}
		req.AccountId = session.GetEffectiveTenantID()
		resp, err := p.services.RouterOS.ConfigureAccess(ctx, &req)
		if err != nil {
			return errorResponse(msg.ID, err)
		}
		return protoResponse(msg.ID, resp)

	case "ListResource":
		if err := session.checkSharePerm("view_peers"); err != nil {
			return errorResponse(msg.ID, err)
		}
		var req pb.ListRouterOSResourceRequest
		if err := json.Unmarshal(msg.Request, &req); err != nil {
			return errorResponse(msg.ID, err)
		}
		req.AccountId = session.GetEffectiveTenantID()
		resp, err := p.services.RouterOS.ListResource(ctx, &req)
		if err != nil {
			return errorResponse(msg.ID, err)
		}
		return protoResponse(msg.ID, resp)

	case "AddResource":
		if err := session.checkSharePerm("manage_winbox"); err != nil {
			return errorResponse(msg.ID, err)
		}
		var req pb.MutateRouterOSResourceRequest
		if err := json.Unmarshal(msg.Request, &req); err != nil {
			return errorResponse(msg.ID, err)
		}
		req.AccountId = session.GetEffectiveTenantID()
		resp, err := p.services.RouterOS.AddResource(ctx, &req)
		if err != nil {
			return errorResponse(msg.ID, err)
		}
		return protoResponse(msg.ID, resp)

	case "UpdateResource":
		if err := session.checkSharePerm("manage_winbox"); err != nil {
			return errorResponse(msg.ID, err)
		}
		var req pb.MutateRouterOSResourceRequest
		if err := json.Unmarshal(msg.Request, &req); err != nil {
			return errorResponse(msg.ID, err)
		}
		req.AccountId = session.GetEffectiveTenantID()
		resp, err := p.services.RouterOS.UpdateResource(ctx, &req)
		if err != nil {
			return errorResponse(msg.ID, err)
		}
		return protoResponse(msg.ID, resp)

	case "DeleteResource":
		if err := session.checkSharePerm("manage_winbox"); err != nil {
			return errorResponse(msg.ID, err)
		}
		var req pb.DeleteRouterOSResourceRequest
		if err := json.Unmarshal(msg.Request, &req); err != nil {
			return errorResponse(msg.ID, err)
		}
		req.AccountId = session.GetEffectiveTenantID()
		resp, err := p.services.RouterOS.DeleteResource(ctx, &req)
		if err != nil {
			return errorResponse(msg.ID, err)
		}
		return protoResponse(msg.ID, resp)

	default:
		return errorResponse(msg.ID, fmt.Errorf("unknown RouterOSService method: %s", msg.Method))
	}
}

func decodeRouterOSStreamRequest(payload json.RawMessage, peerID, accountID string) (*pb.StreamRouterOSDashboardRequest, string, error) {
	var request routerOSStreamRequestPayload
	if err := json.Unmarshal(payload, &request); err != nil {
		return nil, "", fmt.Errorf("invalid routeros dashboard payload")
	}

	switch {
	case request.Open != nil:
		if request.Open.PeerId == "" {
			request.Open.PeerId = peerID
		}
		if request.Open.AccountId == "" {
			request.Open.AccountId = accountID
		}
		return &pb.StreamRouterOSDashboardRequest{
			Payload: &pb.StreamRouterOSDashboardRequest_Open{Open: request.Open},
		}, request.Open.PeerId, nil

	case request.LoadResource != nil:
		return &pb.StreamRouterOSDashboardRequest{
			Payload: &pb.StreamRouterOSDashboardRequest_LoadResource{LoadResource: request.LoadResource},
		}, peerID, nil

	case request.Refresh != nil:
		return &pb.StreamRouterOSDashboardRequest{
			Payload: &pb.StreamRouterOSDashboardRequest_Refresh{Refresh: request.Refresh},
		}, peerID, nil

	case request.ConfigureAccess != nil:
		request.ConfigureAccess.PeerId = peerID
		request.ConfigureAccess.AccountId = accountID
		return &pb.StreamRouterOSDashboardRequest{
			Payload: &pb.StreamRouterOSDashboardRequest_ConfigureAccess{ConfigureAccess: request.ConfigureAccess},
		}, peerID, nil

	case request.AddResource != nil:
		request.AddResource.PeerId = peerID
		request.AddResource.AccountId = accountID
		return &pb.StreamRouterOSDashboardRequest{
			Payload: &pb.StreamRouterOSDashboardRequest_AddResource{AddResource: request.AddResource},
		}, peerID, nil

	case request.UpdateResource != nil:
		request.UpdateResource.PeerId = peerID
		request.UpdateResource.AccountId = accountID
		return &pb.StreamRouterOSDashboardRequest{
			Payload: &pb.StreamRouterOSDashboardRequest_UpdateResource{UpdateResource: request.UpdateResource},
		}, peerID, nil

	case request.DeleteResource != nil:
		request.DeleteResource.PeerId = peerID
		request.DeleteResource.AccountId = accountID
		return &pb.StreamRouterOSDashboardRequest{
			Payload: &pb.StreamRouterOSDashboardRequest_DeleteResource{DeleteResource: request.DeleteResource},
		}, peerID, nil
	}

	return nil, "", fmt.Errorf("routeros dashboard command is required")
}

func (p *TenantProxy) handleRouterOSStreamStart(wsSession *TenantSession, streamID string, payload json.RawMessage) {
	if err := wsSession.checkSharePerm("view_peers"); err != nil {
		p.sendRouterOSStreamError(wsSession, streamID, err.Error())
		return
	}

	req, peerID, err := decodeRouterOSStreamRequest(payload, "", "")
	if err != nil {
		p.sendRouterOSStreamError(wsSession, streamID, err.Error())
		return
	}
	if _, ok := req.Payload.(*pb.StreamRouterOSDashboardRequest_Open); !ok {
		p.sendRouterOSStreamError(wsSession, streamID, "routeros dashboard session must start with an open command")
		return
	}
	if peerID == "" {
		p.sendRouterOSStreamError(wsSession, streamID, "device not found")
		return
	}

	overlayAccountID, _, err := p.resolvePeerRoute(context.Background(), wsSession, peerID)
	if err != nil {
		p.sendRouterOSStreamError(wsSession, streamID, err.Error())
		return
	}

	req, peerID, err = decodeRouterOSStreamRequest(payload, peerID, overlayAccountID)
	if err != nil {
		p.sendRouterOSStreamError(wsSession, streamID, err.Error())
		return
	}

	var (
		ctx     context.Context
		cancel  context.CancelFunc
		handler *RouterOSStreamHandler
	)

	for {
		wsSession.routerOSStreamsMu.Lock()
		if wsSession.routerOSStreams == nil {
			wsSession.routerOSStreams = make(map[string]*RouterOSStreamHandler)
		}
		if existing, exists := wsSession.routerOSStreams[streamID]; exists {
			wsSession.routerOSStreamsMu.Unlock()
			p.cleanupRouterOSStreamHandler(wsSession, streamID, existing)
			continue
		}

		ctx, cancel = context.WithCancel(context.Background())
		handler = &RouterOSStreamHandler{
			sessionID: streamID,
			peerID:    peerID,
			accountID: overlayAccountID,
			cancel:    cancel,
			active:    true,
			inputCh:   make(chan *pb.StreamRouterOSDashboardRequest, 128),
		}
		wsSession.routerOSStreams[streamID] = handler
		wsSession.routerOSStreamsMu.Unlock()
		break
	}

	if p.services == nil || p.services.RouterOS == nil {
		p.sendRouterOSStreamError(wsSession, streamID, "RouterOS service not configured")
		p.cleanupRouterOSStreamHandler(wsSession, streamID, handler)
		return
	}
	// Zero-copy in-process bidi stream — see local_stream.go. Dashboard
	// frames (telemetry → browser) and dashboard requests (browser →
	// device) travel as *pb.* pointers through Go channels, with the
	// RouterOSService handler running in a goroutine.
	local := NewLocalBidiStream[pb.StreamRouterOSDashboardRequest, pb.StreamRouterOSDashboardEvent](ctx, 128)
	stream := local.Client()
	go func() {
		if err := p.services.RouterOS.StreamDashboard(local.Server()); err != nil && ctx.Err() == nil {
			log.Warn().Err(err).Str("stream_id", streamID).Msg("RouterOSService.StreamDashboard exited")
		}
		local.Close()
	}()
	_ = peerID // peerID retained for symmetry; routing happens inside the service now

	wsSession.routerOSStreamsMu.Lock()
	if current, exists := wsSession.routerOSStreams[streamID]; !exists || current != handler {
		wsSession.routerOSStreamsMu.Unlock()
		_ = stream.CloseSend()
		cancel()
		return
	}
	handler.mu.Lock()
	handler.stream = stream
	handler.mu.Unlock()
	wsSession.routerOSStreamsMu.Unlock()

	if err := stream.Send(req); err != nil {
		p.sendRouterOSStreamError(wsSession, streamID, err.Error())
		p.cleanupRouterOSStreamHandler(wsSession, streamID, handler)
		return
	}

	go func() {
		defer cancel()
		for {
			select {
			case msg, ok := <-handler.inputCh:
				if !ok {
					return
				}
				if err := stream.Send(msg); err != nil {
					if ctx.Err() == nil {
						p.sendRouterOSStreamError(wsSession, streamID, err.Error())
					}
					return
				}
			case <-ctx.Done():
				return
			}
		}
	}()

	go func() {
		defer p.cleanupRouterOSStreamHandler(wsSession, streamID, handler)
		for {
			event, err := stream.Recv()
			if err != nil {
				if ctx.Err() == nil {
					p.sendRouterOSStreamClose(wsSession, streamID)
				}
				return
			}
			p.sendRouterOSStreamEvent(wsSession, streamID, event)
		}
	}()
}

func (p *TenantProxy) handleRouterOSStreamData(wsSession *TenantSession, streamID string, payload json.RawMessage) {
	wsSession.routerOSStreamsMu.RLock()
	handler, exists := wsSession.routerOSStreams[streamID]
	wsSession.routerOSStreamsMu.RUnlock()

	if !exists || !handler.canAcceptInput() {
		return
	}

	req, _, err := decodeRouterOSStreamRequest(payload, handler.peerID, handler.accountID)
	if err != nil {
		p.sendRouterOSStreamError(wsSession, streamID, err.Error())
		return
	}

	switch req.Payload.(type) {
	case *pb.StreamRouterOSDashboardRequest_ConfigureAccess,
		*pb.StreamRouterOSDashboardRequest_AddResource,
		*pb.StreamRouterOSDashboardRequest_UpdateResource,
		*pb.StreamRouterOSDashboardRequest_DeleteResource:
		if err := wsSession.checkSharePerm("manage_winbox"); err != nil {
			p.sendRouterOSStreamError(wsSession, streamID, err.Error())
			return
		}
	default:
		if err := wsSession.checkSharePerm("view_peers"); err != nil {
			p.sendRouterOSStreamError(wsSession, streamID, err.Error())
			return
		}
	}

	select {
	case handler.inputCh <- req:
	default:
		p.sendRouterOSStreamError(wsSession, streamID, "routeros dashboard is busy. Please try again.")
	}
}

func (p *TenantProxy) handleRouterOSStreamClose(wsSession *TenantSession, streamID string) {
	p.cleanupRouterOSStreamHandler(wsSession, streamID, nil)
}

func (p *TenantProxy) cleanupAllRouterOSStreams(wsSession *TenantSession) {
	wsSession.routerOSStreamsMu.RLock()
	streamIDs := make([]string, 0, len(wsSession.routerOSStreams))
	for streamID := range wsSession.routerOSStreams {
		streamIDs = append(streamIDs, streamID)
	}
	wsSession.routerOSStreamsMu.RUnlock()

	for _, streamID := range streamIDs {
		p.cleanupRouterOSStreamHandler(wsSession, streamID, nil)
	}
}

func (p *TenantProxy) cleanupRouterOSStreamHandler(wsSession *TenantSession, streamID string, expected *RouterOSStreamHandler) {
	var handler *RouterOSStreamHandler

	wsSession.routerOSStreamsMu.Lock()
	if current, exists := wsSession.routerOSStreams[streamID]; exists {
		if expected != nil && current != expected {
			wsSession.routerOSStreamsMu.Unlock()
			return
		}
		delete(wsSession.routerOSStreams, streamID)
		handler = current
	}
	wsSession.routerOSStreamsMu.Unlock()

	if handler == nil {
		return
	}

	handler.mu.Lock()
	handler.active = false
	stream := handler.stream
	handler.stream = nil
	cancel := handler.cancel
	handler.cancel = nil
	inputCh := handler.inputCh
	handler.inputCh = nil
	handler.mu.Unlock()

	if inputCh != nil {
		close(inputCh)
	}
	if stream != nil {
		_ = stream.CloseSend()
	}
	if cancel != nil {
		cancel()
	}
}

func (p *TenantProxy) sendRouterOSStreamEvent(wsSession *TenantSession, streamID string, event *pb.StreamRouterOSDashboardEvent) {
	if event == nil {
		return
	}

	msg := routerOSStreamWSMessage{
		Type:      "routeros_stream",
		SessionID: streamID,
	}

	switch payload := event.Payload.(type) {
	case *pb.StreamRouterOSDashboardEvent_State:
		msg.Payload.State = payload.State
	case *pb.StreamRouterOSDashboardEvent_Resource:
		msg.Payload.Resource = payload.Resource
	case *pb.StreamRouterOSDashboardEvent_Notice:
		msg.Payload.Notice = payload.Notice
	default:
		return
	}

	p.writeRouterOSStreamMessage(wsSession, msg, streamID)
}

func (p *TenantProxy) sendRouterOSStreamError(wsSession *TenantSession, streamID string, errorMsg string) {
	p.writeRouterOSStreamMessage(wsSession, routerOSStreamWSMessage{
		Type:      "routeros_stream",
		SessionID: streamID,
		Payload: routerOSStreamWSPayload{
			Error: sanitizeClientErrorMessage(errorMsg, "RouterOS dashboard is unavailable right now. Please try again."),
		},
	}, streamID)
}

func (p *TenantProxy) sendRouterOSStreamClose(wsSession *TenantSession, streamID string) {
	p.writeRouterOSStreamMessage(wsSession, routerOSStreamWSMessage{
		Type:      "routeros_stream",
		SessionID: streamID,
		Payload: routerOSStreamWSPayload{
			Close: true,
		},
	}, streamID)
}

func (p *TenantProxy) writeRouterOSStreamMessage(wsSession *TenantSession, msg routerOSStreamWSMessage, streamID string) {
	raw, err := json.Marshal(msg)
	if err != nil {
		return
	}

	wsSession.mu.Lock()
	defer wsSession.mu.Unlock()

	wsSession.Conn.SetWriteDeadline(time.Now().Add(10 * time.Second))
	if err := wsSession.Conn.WriteMessage(websocket.TextMessage, raw); err != nil {
		go p.cleanupRouterOSStreamHandler(wsSession, streamID, nil)
	}
}
