package services

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	pb "WantasticCore/internal/types"
)

// handleWUSPService routes WUSPService WebSocket calls to the correct hub
// gRPC connection (the hub where the peer is currently connected).
//
// Every request must carry a peer_id so we can route to the right hub.
// This mirrors the WebProxyService routing pattern.
func (p *TenantProxy) handleWUSPService(ctx context.Context, msg *Message, session *TenantSession) *Response {
	if session.TenantID == "" {
		return errorResponse(msg.ID, fmt.Errorf("authentication required"))
	}

	// Default timeout for WUSP operations. SyncDeviceState/GetSupportedDM get a
	// longer timeout since they can involve several WireGuard control fragments
	// and a device-side data-model walk. The dashboard keeps cached state visible
	// while these live calls run, so a longer deadline avoids false timeout errors
	// on cellular links.
	timeout := 15 * time.Second
	switch msg.Method {
	case "SyncDeviceState", "GetSupportedDM":
		timeout = 90 * time.Second
	case "SendOperate":
		timeout = 45 * time.Second
	case "GetSupportedProtocol", "SendGet", "SendSet", "SendAdd", "SendDelete":
		timeout = 30 * time.Second
	}
	var cancel context.CancelFunc
	ctx, cancel = context.WithTimeout(ctx, timeout)
	defer cancel()

	switch msg.Method {

	case "GetDeviceState":
		var req pb.GetWUSPDeviceStateRequest
		if err := json.Unmarshal(msg.Request, &req); err != nil {
			return errorResponse(msg.ID, err)
		}
		req.AccountId = session.TenantID
		resp, err := p.services.WUSP.GetDeviceState(ctx, &req)
		if err != nil {
			return errorResponse(msg.ID, err)
		}
		return protoResponse(msg.ID, resp)

	case "ListDeviceStates":
		var req pb.ListWUSPDeviceStatesRequest
		if err := json.Unmarshal(msg.Request, &req); err != nil {
			return errorResponse(msg.ID, err)
		}
		req.AccountId = session.TenantID
		// List is tenant-scoped — direct in-process service call.
		resp, err := p.services.WUSP.ListDeviceStates(ctx, &req)
		if err != nil {
			return errorResponse(msg.ID, err)
		}
		return protoResponse(msg.ID, resp)

	case "SyncDeviceState":
		var req pb.SyncWUSPDeviceStateRequest
		if err := json.Unmarshal(msg.Request, &req); err != nil {
			return errorResponse(msg.ID, err)
		}
		req.AccountId = session.TenantID
		resp, err := p.services.WUSP.SyncDeviceState(ctx, &req)
		if err != nil {
			return errorResponse(msg.ID, err)
		}
		return protoResponse(msg.ID, resp)

	case "SendGet":
		var req pb.WUSPGetRequest
		if err := json.Unmarshal(msg.Request, &req); err != nil {
			return errorResponse(msg.ID, err)
		}
		req.AccountId = session.TenantID
		resp, err := p.services.WUSP.SendGet(ctx, &req)
		if err != nil {
			return errorResponse(msg.ID, err)
		}
		return protoResponse(msg.ID, resp)

	case "SendSet":
		var req pb.WUSPSetRequest
		if err := json.Unmarshal(msg.Request, &req); err != nil {
			return errorResponse(msg.ID, err)
		}
		req.AccountId = session.TenantID
		resp, err := p.services.WUSP.SendSet(ctx, &req)
		if err != nil {
			return errorResponse(msg.ID, err)
		}
		return protoResponse(msg.ID, resp)

	case "SendOperate":
		var req pb.WUSPOperateRequest
		if err := json.Unmarshal(msg.Request, &req); err != nil {
			return errorResponse(msg.ID, err)
		}
		req.AccountId = session.TenantID
		resp, err := p.services.WUSP.SendOperate(ctx, &req)
		if err != nil {
			return errorResponse(msg.ID, err)
		}
		return protoResponse(msg.ID, resp)

	case "SendAdd":
		var req pb.WUSPAddRequest
		if err := json.Unmarshal(msg.Request, &req); err != nil {
			return errorResponse(msg.ID, err)
		}
		req.AccountId = session.TenantID
		resp, err := p.services.WUSP.SendAdd(ctx, &req)
		if err != nil {
			return errorResponse(msg.ID, err)
		}
		return protoResponse(msg.ID, resp)

	case "SendDelete":
		var req pb.WUSPDeleteRequest
		if err := json.Unmarshal(msg.Request, &req); err != nil {
			return errorResponse(msg.ID, err)
		}
		req.AccountId = session.TenantID
		resp, err := p.services.WUSP.SendDelete(ctx, &req)
		if err != nil {
			return errorResponse(msg.ID, err)
		}
		return protoResponse(msg.ID, resp)

	// --- Device Snapshot ---

	case "CreateSnapshot":
		var req pb.CreateDeviceSnapshotRequest
		if err := json.Unmarshal(msg.Request, &req); err != nil {
			return errorResponse(msg.ID, err)
		}
		req.AccountId = session.TenantID
		resp, err := p.services.WUSP.CreateSnapshot(ctx, &req)
		if err != nil {
			return errorResponse(msg.ID, err)
		}
		return protoResponse(msg.ID, resp)

	case "ListSnapshots":
		var req pb.ListDeviceSnapshotsRequest
		if err := json.Unmarshal(msg.Request, &req); err != nil {
			return errorResponse(msg.ID, err)
		}
		req.AccountId = session.TenantID
		resp, err := p.services.WUSP.ListSnapshots(ctx, &req)
		if err != nil {
			return errorResponse(msg.ID, err)
		}
		return protoResponse(msg.ID, resp)

	case "GetSnapshot":
		var req pb.GetDeviceSnapshotRequest
		if err := json.Unmarshal(msg.Request, &req); err != nil {
			return errorResponse(msg.ID, err)
		}
		req.AccountId = session.TenantID
		resp, err := p.services.WUSP.GetSnapshot(ctx, &req)
		if err != nil {
			return errorResponse(msg.ID, err)
		}
		return protoResponse(msg.ID, resp)

	case "UpdateSnapshot":
		var req pb.UpdateDeviceSnapshotRequest
		if err := json.Unmarshal(msg.Request, &req); err != nil {
			return errorResponse(msg.ID, err)
		}
		req.AccountId = session.TenantID
		// If refreshing from a peer, route to its hub; otherwise use default.
		resp, err := p.services.WUSP.UpdateSnapshot(ctx, &req)
		if err != nil {
			return errorResponse(msg.ID, err)
		}
		return protoResponse(msg.ID, resp)

	case "DeleteSnapshot":
		var req pb.DeleteDeviceSnapshotRequest
		if err := json.Unmarshal(msg.Request, &req); err != nil {
			return errorResponse(msg.ID, err)
		}
		req.AccountId = session.TenantID
		resp, err := p.services.WUSP.DeleteSnapshot(ctx, &req)
		if err != nil {
			return errorResponse(msg.ID, err)
		}
		return protoResponse(msg.ID, resp)

	case "ProvisionDevice":
		var req pb.ProvisionDeviceRequest
		if err := json.Unmarshal(msg.Request, &req); err != nil {
			return errorResponse(msg.ID, err)
		}
		req.AccountId = session.TenantID
		resp, err := p.services.WUSP.ProvisionDevice(ctx, &req)
		if err != nil {
			return errorResponse(msg.ID, err)
		}
		return protoResponse(msg.ID, resp)

	case "GenerateUploadToken":
		var req pb.GenerateUploadTokenRequest
		if err := json.Unmarshal(msg.Request, &req); err != nil {
			return errorResponse(msg.ID, err)
		}
		req.AccountId = session.TenantID
		resp, err := p.services.WUSP.GenerateUploadToken(ctx, &req)
		if err != nil {
			return errorResponse(msg.ID, err)
		}
		return protoResponse(msg.ID, resp)

	case "GenerateBackupToken":
		var req pb.GenerateBackupTokenRequest
		if err := json.Unmarshal(msg.Request, &req); err != nil {
			return errorResponse(msg.ID, err)
		}
		req.AccountId = session.TenantID
		resp, err := p.services.WUSP.GenerateBackupToken(ctx, &req)
		if err != nil {
			return errorResponse(msg.ID, err)
		}
		return protoResponse(msg.ID, resp)

	default:
		return errorResponse(msg.ID, fmt.Errorf("unknown WUSPService method: %s", msg.Method))
	}
}

// wuspConn / inProcessClientConn helpers were removed when every caller
// migrated to direct p.services.X dispatch. See git history for the old
// gRPC-style versions.
