package services

import (
	"context"
	"encoding/json"
	"fmt"

	pb "WantasticCore/internal/types"
)

// handleWebProxy handles WebProxyService unary RPCs (session lifecycle:
// Create / Get / List / Close). Streaming traffic uses the binary frame
// path in webproxy_bridge.go — there is no JSON streaming entry point.
func (p *TenantProxy) handleWebProxy(ctx context.Context, msg *Message, session *TenantSession) *Response {
	// Every CreateWebProxySession request lands here in-process.
	if msg.Method == "CreateWebProxySession" {
		var grpcReq pb.CreateWebProxySessionRequest
		if err := json.Unmarshal(msg.Request, &grpcReq); err != nil {
			return errorResponse(msg.ID, err)
		}
		// Enforce tenant ID from session — never trust the client.
		grpcReq.TenantId = session.TenantID

		resp, err := p.services.WebProxy.CreateWebProxySession(ctx, &grpcReq)
		if err != nil {
			return errorResponse(msg.ID, err)
		}
		return protoResponse(msg.ID, resp)
	}

	client := p.services.WebProxy

	switch msg.Method {
	case "GetWebProxySession":
		var req pb.GetWebProxySessionRequest
		if err := json.Unmarshal(msg.Request, &req); err != nil {
			return errorResponse(msg.ID, err)
		}
		resp, err := client.GetWebProxySession(ctx, &req)
		if err != nil {
			return errorResponse(msg.ID, err)
		}
		return protoResponse(msg.ID, resp)

	case "ListWebProxySessions":
		var req pb.ListWebProxySessionsRequest
		if err := json.Unmarshal(msg.Request, &req); err != nil {
			return errorResponse(msg.ID, err)
		}
		req.TenantId = session.TenantID
		resp, err := client.ListWebProxySessions(ctx, &req)
		if err != nil {
			return errorResponse(msg.ID, err)
		}
		return protoResponse(msg.ID, resp)

	case "CloseWebProxySession":
		var req pb.CloseWebProxySessionRequest
		if err := json.Unmarshal(msg.Request, &req); err != nil {
			return errorResponse(msg.ID, err)
		}
		resp, err := client.CloseWebProxySession(ctx, &req)
		if err != nil {
			return errorResponse(msg.ID, err)
		}
		// Tear down the local bridge for this session if one exists.
		p.closeWebProxyBridge(session, req.SessionId)
		return protoResponse(msg.ID, resp)

	default:
		return errorResponse(msg.ID, fmt.Errorf("unknown WebProxy method: %s", msg.Method))
	}
}
