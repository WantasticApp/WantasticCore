package core

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"strconv"
	"strings"
	"time"

	pb "WantasticCore/internal/types"
	"WantasticCore/internal/store"
	"WantasticCore/internal/wusp"
	"WantasticCore/internal/wuspcontroller"
)

// WUSPService implements WUSPServiceHandler.
// It exposes WUSP controller operations to the portal WebSocket proxy.
type WUSPService struct {
	UnimplementedWUSPServiceHandler
	ctrl  *wuspcontroller.WUSPController
	redis RedisClient // for backup token storage
}

// RedisClient is the minimal Redis interface needed for backup tokens.
type RedisClient interface {
	Set(ctx context.Context, key string, value interface{}, expiration time.Duration) error
	Get(ctx context.Context, key string) (string, error)
	Del(ctx context.Context, keys ...string) error
}

// NewWUSPService creates a WUSPService backed by the given controller.
func NewWUSPService(ctrl *wuspcontroller.WUSPController) *WUSPService {
	return &WUSPService{ctrl: ctrl}
}

// SetRedis injects the Redis client for backup token management.
func (s *WUSPService) SetRedis(r RedisClient) {
	s.redis = r
}

// GetDeviceState returns the last persisted device model snapshot for a peer.
func (s *WUSPService) GetDeviceState(ctx context.Context, req *pb.GetWUSPDeviceStateRequest) (*pb.GetWUSPDeviceStateResponse, error) {
	state, err := s.ctrl.StateRepo().GetByPeer(req.PeerId)
	if err != nil {
		return nil, fmt.Errorf("wusp: GetDeviceState: %w", err)
	}
	return &pb.GetWUSPDeviceStateResponse{
		State: deviceStateToProto(state),
	}, nil
}

// ListDeviceStates lists all device state snapshots for a tenant.
func (s *WUSPService) ListDeviceStates(ctx context.Context, req *pb.ListWUSPDeviceStatesRequest) (*pb.ListWUSPDeviceStatesResponse, error) {
	states, err := s.ctrl.StateRepo().GetByAccount(req.AccountId)
	if err != nil {
		return nil, fmt.Errorf("wusp: ListDeviceStates: %w", err)
	}
	out := make([]*pb.WUSPDeviceState, 0, len(states))
	for _, s := range states {
		out = append(out, deviceStateToProto(s))
	}
	return &pb.ListWUSPDeviceStatesResponse{States: out}, nil
}

// SyncDeviceState triggers a live Get round-trip to the peer and persists the result.
func (s *WUSPService) SyncDeviceState(ctx context.Context, req *pb.SyncWUSPDeviceStateRequest) (*pb.SyncWUSPDeviceStateResponse, error) {
	if err := s.ctrl.SyncDeviceState(ctx, req.PeerId, req.AccountId); err != nil {
		return &pb.SyncWUSPDeviceStateResponse{Success: false, Error: err.Error()}, nil
	}
	// Return the freshly persisted state.
	state, err := s.ctrl.StateRepo().GetByPeer(req.PeerId)
	if err != nil {
		return &pb.SyncWUSPDeviceStateResponse{Success: true}, nil
	}
	return &pb.SyncWUSPDeviceStateResponse{
		Success: true,
		State:   deviceStateToProto(state),
	}, nil
}

// SendGet sends a USP Get to the peer and returns the parameter values.
func (s *WUSPService) SendGet(ctx context.Context, req *pb.WUSPGetRequest) (*pb.WUSPGetResponse, error) {
	resp, err := s.ctrl.Get(ctx, req.PeerId, req.Paths...)
	if err != nil {
		return &pb.WUSPGetResponse{Success: false, Error: err.Error()}, nil
	}
	if resp.Error != "" {
		return &pb.WUSPGetResponse{Success: false, Error: resp.Error}, nil
	}
	return &pb.WUSPGetResponse{
		Success: true,
		Params:  messageToParams(resp.Message),
	}, nil
}

// SendSet sends a USP Set to the peer.
func (s *WUSPService) SendSet(ctx context.Context, req *pb.WUSPSetRequest) (*pb.WUSPSetResponse, error) {
	msg := s.paramsToMessage(req.Params)
	// SetValidated drops read-only fields (e.g. UpTime, counters) before encoding,
	// so the agent never receives a Set request that mixes writable and read-only paths.
	resp, err := s.ctrl.SetValidated(ctx, req.PeerId, msg)
	if err != nil {
		return &pb.WUSPSetResponse{Success: false, Error: err.Error()}, nil
	}
	if resp.Error != "" {
		return &pb.WUSPSetResponse{Success: false, Error: resp.Error}, nil
	}
	return &pb.WUSPSetResponse{Success: true}, nil
}

// SendOperate sends a USP Operate command to the peer.
func (s *WUSPService) SendOperate(ctx context.Context, req *pb.WUSPOperateRequest) (*pb.WUSPOperateResponse, error) {
	input := s.paramsToMessage(req.InputParams)
	resp, err := s.ctrl.Operate(ctx, req.PeerId, req.CommandPath, input, nil)
	if err != nil {
		return &pb.WUSPOperateResponse{Success: false, Error: err.Error()}, nil
	}
	if resp.Error != "" {
		return &pb.WUSPOperateResponse{Success: false, Error: resp.Error}, nil
	}
	return &pb.WUSPOperateResponse{
		Success:      true,
		OutputParams: messageToParams(resp.Message),
	}, nil
}

// SendAdd creates a new object instance on the peer device.
func (s *WUSPService) SendAdd(ctx context.Context, req *pb.WUSPAddRequest) (*pb.WUSPAddResponse, error) {
	msg := s.paramsToMessage(req.Params)
	resp, err := s.ctrl.Add(ctx, req.PeerId, req.ObjectPath)
	if err != nil {
		return &pb.WUSPAddResponse{Success: false, Error: err.Error()}, nil
	}
	if resp.Error != "" {
		return &pb.WUSPAddResponse{Success: false, Error: resp.Error}, nil
	}
	// If initial params were provided, set them on the new instance
	if msg != nil && resp.ObjectPath != "" {
		setResp, err := s.ctrl.Set(ctx, req.PeerId, msg)
		if err != nil || setResp.Error != "" {
			// Instance created but initial values failed — still return success with warning
			errMsg := "instance created but initial values failed"
			if err != nil {
				errMsg += ": " + err.Error()
			} else if setResp.Error != "" {
				errMsg += ": " + setResp.Error
			}
			return &pb.WUSPAddResponse{
				Success:       true,
				InstancePath:  resp.ObjectPath,
				CreatedPaths:  resp.Paths,
				Error:         errMsg,
			}, nil
		}
	}
	return &pb.WUSPAddResponse{
		Success:      true,
		InstancePath: resp.ObjectPath,
		CreatedPaths: resp.Paths,
	}, nil
}

// SendDelete removes object instances or parameters from the peer device.
func (s *WUSPService) SendDelete(ctx context.Context, req *pb.WUSPDeleteRequest) (*pb.WUSPDeleteResponse, error) {
	resp, err := s.ctrl.Delete(ctx, req.PeerId, req.Paths...)
	if err != nil {
		return &pb.WUSPDeleteResponse{Success: false, Error: err.Error()}, nil
	}
	if resp.Error != "" {
		return &pb.WUSPDeleteResponse{Success: false, Error: resp.Error}, nil
	}
	return &pb.WUSPDeleteResponse{Success: true}, nil
}

// GetSupportedProtocol returns the live peer capability descriptor.
func (s *WUSPService) GetSupportedProtocol(ctx context.Context, req *pb.WUSPSupportedProtocolRequest) (*pb.WUSPSupportedProtocolResponse, error) {
	resp, err := s.ctrl.GetSupportedProtocol(ctx, req.PeerId)
	if err != nil {
		return &pb.WUSPSupportedProtocolResponse{Success: false, Error: err.Error()}, nil
	}
	if resp.Error != "" {
		return &pb.WUSPSupportedProtocolResponse{Success: false, Error: resp.Error}, nil
	}
	return &pb.WUSPSupportedProtocolResponse{
		Success:  true,
		Protocol: protocolInfoToProto(resp.Protocol),
	}, nil
}

// GetSupportedDM remains served by the bundled schema and controller sync path.

// =============================================================================
// Device Snapshot Methods
// =============================================================================

// CreateSnapshot takes the current live device state of a peer and saves it as
// a named snapshot in the tenant's account.
func (s *WUSPService) CreateSnapshot(ctx context.Context, req *pb.CreateDeviceSnapshotRequest) (*pb.CreateDeviceSnapshotResponse, error) {
	// Resolve the live device state for this peer.
	state, err := s.ctrl.StateRepo().GetByPeer(req.PeerId)
	if err != nil || state == nil {
		return &pb.CreateDeviceSnapshotResponse{Success: false, Error: "peer device state not found; run SyncDeviceState first"}, nil
	}

	protocol := req.Protocol
	if protocol == "" {
		protocol = "wusp"
	}

	snap := &store.DeviceSnapshotData{
		AccountID:       req.AccountId,
		Name:            req.Name,
		Protocol:        protocol,
		Manufacturer:    state.Manufacturer,
		ProductClass:    state.ProductClass,
		SerialNumber:    state.SerialNumber,
		SoftwareVersion: state.SoftwareVersion,
		HardwareVersion: state.HardwareVersion,
		DeviceSnapshot:  state.DeviceSnapshot,
	}

	if err := store.DB().DeviceSnapshots().Create(snap); err != nil {
		return &pb.CreateDeviceSnapshotResponse{Success: false, Error: err.Error()}, nil
	}
	return &pb.CreateDeviceSnapshotResponse{
		Success:  true,
		Snapshot: deviceSnapshotToProto(snap),
	}, nil
}

func protocolInfoToProto(info *wusp.USPProtocolInfo) *pb.WUSPProtocolInfo {
	if info == nil {
		return nil
	}
	methods := make([]uint32, 0, len(info.Methods))
	for _, method := range info.Methods {
		if value := methodStringToProto(method); value != 0 {
			methods = append(methods, value)
		}
	}
	return &pb.WUSPProtocolInfo{
		Name:                 info.Name,
		Version:              uint32(info.Version),
		Methods:              methods,
		Compression:          append([]string(nil), info.Compression...),
		ControlTransport:     info.ControlTransport,
		TransferTransport:    info.TransferTransport,
		MaxControlPayload:    uint32(info.MaxControlPayload),
		RecommendedChunkSize: uint32(info.RecommendedChunkSize),
		TunnelOnly:           info.TunnelOnly,
		ReliableTransfer:     info.ReliableTransfer,
	}
}

func methodStringToProto(name string) uint32 {
	switch name {
	case "Get":
		return uint32(wusp.USPAgentMethodGet)
	case "Set":
		return uint32(wusp.USPAgentMethodSet)
	case "Add":
		return uint32(wusp.USPAgentMethodAdd)
	case "Delete":
		return uint32(wusp.USPAgentMethodDelete)
	case "GetInstances":
		return uint32(wusp.USPAgentMethodGetInstances)
	case "Operate":
		return uint32(wusp.USPAgentMethodOperate)
	case "Notify":
		return uint32(wusp.USPAgentMethodNotify)
	case "GetSupportedDM":
		return uint32(wusp.USPAgentMethodGetSupportedDM)
	case "GetSupportedProtocol":
		return uint32(wusp.USPAgentMethodGetSupportedProtocol)
	case "Upload":
		return uint32(wusp.USPAgentMethodUpload)
	case "Download":
		return uint32(wusp.USPAgentMethodDownload)
	default:
		return 0
	}
}

// ListSnapshots returns all snapshots for the account, optionally filtered by protocol.
func (s *WUSPService) ListSnapshots(ctx context.Context, req *pb.ListDeviceSnapshotsRequest) (*pb.ListDeviceSnapshotsResponse, error) {
	var (
		snaps []*store.DeviceSnapshotData
		err   error
	)
	if req.Protocol != "" {
		snaps, err = store.DB().DeviceSnapshots().ListByProtocol(req.AccountId, req.Protocol)
	} else {
		snaps, err = store.DB().DeviceSnapshots().List(req.AccountId)
	}
	if err != nil {
		return nil, fmt.Errorf("wusp: ListSnapshots: %w", err)
	}
	out := make([]*pb.DeviceSnapshot, 0, len(snaps))
	for _, s := range snaps {
		out = append(out, deviceSnapshotToProto(s))
	}
	return &pb.ListDeviceSnapshotsResponse{Snapshots: out}, nil
}

// GetSnapshot returns a single snapshot by ID.
func (s *WUSPService) GetSnapshot(ctx context.Context, req *pb.GetDeviceSnapshotRequest) (*pb.GetDeviceSnapshotResponse, error) {
	snap, err := store.DB().DeviceSnapshots().Get(req.Id, req.AccountId)
	if err != nil {
		return nil, fmt.Errorf("wusp: GetSnapshot: %w", err)
	}
	return &pb.GetDeviceSnapshotResponse{Snapshot: deviceSnapshotToProto(snap)}, nil
}

// UpdateSnapshot updates the name of an existing snapshot or refreshes its
// data from a live peer (when PeerId is set in the request).
func (s *WUSPService) UpdateSnapshot(ctx context.Context, req *pb.UpdateDeviceSnapshotRequest) (*pb.UpdateDeviceSnapshotResponse, error) {
	snap, err := store.DB().DeviceSnapshots().Get(req.Id, req.AccountId)
	if err != nil {
		return &pb.UpdateDeviceSnapshotResponse{Success: false, Error: err.Error()}, nil
	}

	if req.Name != "" {
		snap.Name = req.Name
	}

	if req.PeerId != "" {
		state, err := s.ctrl.StateRepo().GetByPeer(req.PeerId)
		if err != nil || state == nil {
			return &pb.UpdateDeviceSnapshotResponse{Success: false, Error: "peer device state not found; run SyncDeviceState first"}, nil
		}
		snap.Manufacturer = state.Manufacturer
		snap.ProductClass = state.ProductClass
		snap.SerialNumber = state.SerialNumber
		snap.SoftwareVersion = state.SoftwareVersion
		snap.HardwareVersion = state.HardwareVersion
		snap.DeviceSnapshot = state.DeviceSnapshot
	}

	snap.UpdatedAt = time.Now()
	if err := store.DB().DeviceSnapshots().Update(snap); err != nil {
		return &pb.UpdateDeviceSnapshotResponse{Success: false, Error: err.Error()}, nil
	}
	return &pb.UpdateDeviceSnapshotResponse{
		Success:  true,
		Snapshot: deviceSnapshotToProto(snap),
	}, nil
}

// DeleteSnapshot removes a snapshot.
func (s *WUSPService) DeleteSnapshot(ctx context.Context, req *pb.DeleteDeviceSnapshotRequest) (*pb.DeleteDeviceSnapshotResponse, error) {
	if err := store.DB().DeviceSnapshots().Delete(req.Id, req.AccountId); err != nil {
		return &pb.DeleteDeviceSnapshotResponse{Success: false, Error: err.Error()}, nil
	}
	return &pb.DeleteDeviceSnapshotResponse{Success: true}, nil
}

// ProvisionDevice applies a saved snapshot to a live peer.
func (s *WUSPService) ProvisionDevice(ctx context.Context, req *pb.ProvisionDeviceRequest) (*pb.ProvisionDeviceResponse, error) {
	snap, err := store.DB().DeviceSnapshots().Get(req.SnapshotId, req.AccountId)
	if err != nil {
		return &pb.ProvisionDeviceResponse{Success: false, Error: err.Error()}, nil
	}
	if len(snap.DeviceSnapshot) == 0 {
		return &pb.ProvisionDeviceResponse{Success: false, Error: "snapshot is empty"}, nil
	}
	resp, err := s.ctrl.ProvisionDevice(ctx, req.PeerId, snap.DeviceSnapshot)
	if err != nil {
		return &pb.ProvisionDeviceResponse{Success: false, Error: err.Error()}, nil
	}
	if resp.Error != "" {
		return &pb.ProvisionDeviceResponse{Success: false, Error: resp.Error}, nil
	}
	return &pb.ProvisionDeviceResponse{Success: true}, nil
}

func (s *WUSPService) GetSnapshotBackup(ctx context.Context, req *pb.GetSnapshotBackupRequest) (*pb.GetSnapshotBackupResponse, error) {
	snap, err := store.DB().DeviceSnapshots().Get(req.SnapshotId, req.AccountId)
	if err != nil {
		return nil, fmt.Errorf("snapshot not found: %w", err)
	}
	return &pb.GetSnapshotBackupResponse{
		BackupFile: []byte(snap.BackupFile),
		BackupName: snap.BackupName,
		BackupSize: int32(snap.BackupSize),
	}, nil
}

func (s *WUSPService) UploadSnapshotBackup(ctx context.Context, req *pb.UploadSnapshotBackupRequest) (*pb.UploadSnapshotBackupResponse, error) {
	if req.UploadToken == "" {
		return &pb.UploadSnapshotBackupResponse{Success: false, Error: "missing upload token"}, nil
	}

	// First try: Redis-backed peer backup token (from GenerateBackupToken).
	// This token is intentionally reusable for unattended RouterOS backups.
	if s.redis != nil {
		key := backupTokenKey(req.UploadToken)
		if val, err := s.redis.Get(ctx, key); err == nil && val != "" {
			// Token is valid — parse account_id:peer_id
			parts := strings.SplitN(val, ":", 2)
			if len(parts) == 2 {
				accountID, peerID := parts[0], parts[1]

				// Find or create a snapshot for this peer
				snap := s.findOrCreateBackupSnapshot(accountID, peerID, req.BackupName)
				if snap != nil {
					snap.BackupFile = string(req.BackupFile)
					snap.BackupName = req.BackupName
					snap.BackupSize = len(req.BackupFile)
					if err := store.DB().DeviceSnapshots().Update(snap); err == nil {
						return &pb.UploadSnapshotBackupResponse{Success: true}, nil
					}
				}
			}
		}
	}

	// Second try: snapshot upload_token (from GenerateUploadToken)
	snap, err := store.DB().DeviceSnapshots().GetByUploadToken(req.UploadToken)
	if err != nil {
		return &pb.UploadSnapshotBackupResponse{Success: false, Error: "invalid token"}, nil
	}

	// Store backup and rotate token
	snap.BackupFile = string(req.BackupFile)
	snap.BackupName = req.BackupName
	snap.BackupSize = len(req.BackupFile)
	snap.UploadToken = generateToken()

	if err := store.DB().DeviceSnapshots().Update(snap); err != nil {
		return &pb.UploadSnapshotBackupResponse{Success: false, Error: err.Error()}, nil
	}

	return &pb.UploadSnapshotBackupResponse{
		Success:  true,
		NewToken: snap.UploadToken,
	}, nil
}

func (s *WUSPService) GenerateUploadToken(ctx context.Context, req *pb.GenerateUploadTokenRequest) (*pb.GenerateUploadTokenResponse, error) {
	snap, err := store.DB().DeviceSnapshots().Get(req.SnapshotId, req.AccountId)
	if err != nil {
		return &pb.GenerateUploadTokenResponse{Success: false, Error: "snapshot not found"}, nil
	}

	token := generateToken()
	snap.UploadToken = token
	if err := store.DB().DeviceSnapshots().Update(snap); err != nil {
		return &pb.GenerateUploadTokenResponse{Success: false, Error: err.Error()}, nil
	}

	return &pb.GenerateUploadTokenResponse{
		Success:     true,
		UploadToken: token,
		UploadUrl:   fmt.Sprintf("https://console.wantastic.app/hooks/backup?token=%s", token),
	}, nil
}

// findOrCreateBackupSnapshot finds the latest "mikrotik" protocol snapshot for the
// peer's account, or creates one if none exists.
func (s *WUSPService) findOrCreateBackupSnapshot(accountID, peerID, backupName string) *store.DeviceSnapshotData {
	repo := store.DB().DeviceSnapshots()

	// Try to find an existing mikrotik backup snapshot for this account
	existing, err := repo.ListByProtocol(accountID, "mikrotik")
	if err == nil && len(existing) > 0 {
		return existing[0] // reuse the most recent one
	}

	// Create a new snapshot
	snap := &store.DeviceSnapshotData{
		AccountID: accountID,
		Name:      backupName,
		Protocol:  "mikrotik",
	}
	if err := repo.Create(snap); err != nil {
		return nil
	}
	return snap
}

func generateToken() string {
	b := make([]byte, 32)
	rand.Read(b)
	return hex.EncodeToString(b)
}

func backupTokenKey(token string) string {
	return "backup_token:" + token
}

func peerBackupTokenKey(accountID, peerID string) string {
	return fmt.Sprintf("backup_peer_token:%s:%s", accountID, peerID)
}

func (s *WUSPService) GenerateBackupToken(ctx context.Context, req *pb.GenerateBackupTokenRequest) (*pb.GenerateBackupTokenResponse, error) {
	if req.PeerId == "" || req.AccountId == "" {
		return &pb.GenerateBackupTokenResponse{Success: false, Error: "peer_id and account_id required"}, nil
	}
	if s.redis == nil {
		return &pb.GenerateBackupTokenResponse{Success: false, Error: "backup token store unavailable"}, nil
	}

	token := generateToken()

	// Keep exactly one reusable backup token per peer. Regenerating rotates the
	// prior token out so redeploying the script revokes the older credential.
	peerKey := peerBackupTokenKey(req.AccountId, req.PeerId)
	if prevToken, err := s.redis.Get(ctx, peerKey); err == nil && prevToken != "" {
		_ = s.redis.Del(ctx, backupTokenKey(prevToken))
	}
	value := fmt.Sprintf("%s:%s", req.AccountId, req.PeerId)
	if err := s.redis.Set(ctx, backupTokenKey(token), value, 0); err != nil {
		return &pb.GenerateBackupTokenResponse{Success: false, Error: "failed to store backup token"}, nil
	}
	if err := s.redis.Set(ctx, peerKey, token, 0); err != nil {
		_ = s.redis.Del(ctx, backupTokenKey(token))
		return &pb.GenerateBackupTokenResponse{Success: false, Error: "failed to store peer backup token"}, nil
	}

	return &pb.GenerateBackupTokenResponse{
		Success:     true,
		UploadToken: token,
		UploadUrl:   fmt.Sprintf("https://console.wantastic.app/hooks/backup?token=%s", token),
	}, nil
}

// EnsureWUSPSubscription registers (or refreshes) the controller's canonical
// dashboard subscription on the agent so it pushes ValueChange / OperationComplete
// / ObjectCreation / ObjectDeletion events back. Idempotent.
//
// Failures are non-fatal to the dashboard: we still surface success=false so
// the caller can log, but the dashboard's request/response path keeps working
// — only live push is affected.
func (s *WUSPService) EnsureWUSPSubscription(ctx context.Context, req *pb.EnsureWUSPSubscriptionRequest) (*pb.EnsureWUSPSubscriptionResponse, error) {
	if req.PeerId == "" {
		return &pb.EnsureWUSPSubscriptionResponse{Success: false, Error: "peer_id required"}, nil
	}
	if err := s.ctrl.EnsureDashboardSubscription(req.PeerId); err != nil {
		return &pb.EnsureWUSPSubscriptionResponse{Success: false, Error: err.Error()}, nil
	}
	return &pb.EnsureWUSPSubscriptionResponse{Success: true}, nil
}

// CancelWUSPSubscription removes the canonical dashboard subscription. Called
// after the last dashboard session for a peer goes away (debounced caller-side).
// Errors are non-fatal: if the agent is offline the subscription disappears
// when the agent restarts or its session table ages out.
func (s *WUSPService) CancelWUSPSubscription(ctx context.Context, req *pb.CancelWUSPSubscriptionRequest) (*pb.CancelWUSPSubscriptionResponse, error) {
	if req.PeerId == "" {
		return &pb.CancelWUSPSubscriptionResponse{Success: false, Error: "peer_id required"}, nil
	}
	if err := s.ctrl.CancelDashboardSubscription(req.PeerId); err != nil {
		return &pb.CancelWUSPSubscriptionResponse{Success: false, Error: err.Error()}, nil
	}
	return &pb.CancelWUSPSubscriptionResponse{Success: true}, nil
}

// deviceSnapshotToProto converts a store snapshot to its protobuf representation.
func deviceSnapshotToProto(s *store.DeviceSnapshotData) *pb.DeviceSnapshot {
	if s == nil {
		return nil
	}
	// HasBackup is derived: lets the listing UI show a download badge without
	// shipping the (potentially large) backup body in every list call.
	hasBackup := len(s.BackupFile) > 0 || s.BackupSize > 0
	return &pb.DeviceSnapshot{
		Id:              s.ID,
		AccountId:       s.AccountID,
		Name:            s.Name,
		Protocol:        s.Protocol,
		Manufacturer:    s.Manufacturer,
		ProductClass:    s.ProductClass,
		SerialNumber:    s.SerialNumber,
		SoftwareVersion: s.SoftwareVersion,
		HardwareVersion: s.HardwareVersion,
		DeviceSnapshot:  s.DeviceSnapshot,
		CreatedAt:       s.CreatedAt.Unix(),
		UpdatedAt:       s.UpdatedAt.Unix(),
		BackupName:      s.BackupName,
		BackupSize:      int32(s.BackupSize),
		HasBackup:       hasBackup,
	}
}

// =============================================================================
// Helpers
// =============================================================================

func deviceStateToProto(s *store.WUSPDeviceStateData) *pb.WUSPDeviceState {
	if s == nil {
		return nil
	}
	// DeviceSnapshot is stored as JSON []byte in the DB. The proto field is
	// `bytes` which protojson encodes as base64. To make the frontend's life
	// easier, we pass it through — the frontend decodes base64 → JSON string.
	return &pb.WUSPDeviceState{
		Id:              s.ID,
		PeerId:          s.PeerID,
		AccountId:       s.AccountID,
		LastSyncAt:      s.LastSyncAt.Unix(),
		SyncError:       s.SyncError,
		DeviceSnapshot:  s.DeviceSnapshot,
		DeviceId:        s.DeviceID,
		Manufacturer:    s.Manufacturer,
		ProductClass:    s.ProductClass,
		SerialNumber:    s.SerialNumber,
		SoftwareVersion: s.SoftwareVersion,
		HardwareVersion: s.HardwareVersion,
		WuspEnable:      s.WUSPEnable,
		WuspStatus:      s.WUSPStatus,
		WuspVersion:     s.WUSPVersion,
		CreatedAt:       s.CreatedAt.Unix(),
		UpdatedAt:       s.UpdatedAt.Unix(),
	}
}

func messageToParams(msg *wusp.Message) []*pb.WUSPParam {
	if msg == nil {
		return nil
	}
	out := make([]*pb.WUSPParam, 0, len(msg.Fields))
	for _, f := range msg.Fields {
		out = append(out, &pb.WUSPParam{Path: f.Path, Value: wusp.ValueToString(f.Val)})
	}
	return out
}

func (s *WUSPService) paramsToMessage(params []*pb.WUSPParam) *wusp.Message {
	if len(params) == 0 {
		return nil
	}
	msg := wusp.NewMessage()
	for _, p := range params {
		msg.Set(p.Path, s.parseValueForPath(p.Path, p.Value))
	}
	return msg
}

// parseValueForPath converts the wire string value into a wusp.Value, using the
// bundled TR-181 schema to pick the right TypeTag (unsignedInt vs string vs
// bool, etc.). Falls back to the heuristic ParseStringValue when the path is
// not in the schema.
func (s *WUSPService) parseValueForPath(path, str string) wusp.Value {
	if s.ctrl != nil {
		if param, ok := s.ctrl.ParamInfo(path); ok {
			switch param.Type {
			case wusp.TypeBoolean:
				switch strings.ToLower(strings.TrimSpace(str)) {
				case "true", "1", "yes":
					return wusp.Bool(true)
				default:
					return wusp.Bool(false)
				}
			case wusp.TypeUnsignedInt, wusp.TypeUnsignedLong, wusp.TypeStatsCounter:
				if n, err := strconv.ParseUint(strings.TrimSpace(str), 10, 64); err == nil {
					return wusp.Uint(n)
				}
				return wusp.Uint(0)
			case wusp.TypeInt, wusp.TypeLong:
				if n, err := strconv.ParseInt(strings.TrimSpace(str), 10, 64); err == nil {
					return wusp.Int(n)
				}
				return wusp.Int(0)
			case wusp.TypeString:
				return wusp.String(str)
			}
		}
	}
	return wusp.ParseStringValue(str)
}
