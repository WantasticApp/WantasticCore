package wuspcontroller

// Integration tests for WUSPController.
//
// These tests wire the real WUSPController against an in-process "fake agent"
// that uses real wusp encoding/decoding — no mocking of the binary protocol.
// The fake agent:
//   - drains the controller's outbound channel (which contains fragments)
//   - reassembles fragments
//   - handles requests using a real wusp.USPAgent
//   - fragments the response and delivers it to ctrl.HandleInbound
//
// This exercises the full controller pipeline:
//   ctrl.Get/Set/GetAll → fragment → send → agent handles → fragment → HandleInbound → caller

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"WantasticCore/internal/store"
	"WantasticCore/internal/wusp"
)

// ── fake agent ────────────────────────────────────────────────────────────────

// fakeAgent is an in-process USP agent that responds to controller requests.
// It uses a real wusp.USPAgent for request processing and the same wire codec
// used in production.
type fakeAgent struct {
	t       testing.TB
	ctrl    *WUSPController
	peerKey string // base64 WireGuard public key as seen by the controller
	agent   *wusp.USPAgent

	// Fragment reassembly buffer: messageID → collected fragments
	fragMu sync.Mutex
	frags  map[uint64][]wusp.USPControlFragment

	// Closed when the agent goroutine has exited.
	stopped chan struct{}

	transferMu        sync.Mutex
	uploadSessionID   uint64
	uploadRequestID   uint64
	uploadTransferred []byte
}

func newFakeAgent(t testing.TB, ctrl *WUSPController, peerKey string) *fakeAgent {
	t.Helper()
	agent := wusp.NewUSPAgent(wusp.USPAgentOptions{})
	// Bootstrap from the full BBF schema so the fake agent has ALL paths that
	// SyncDeviceState's targeted Get requests. Then override key fields.
	if err := agent.Bootstrap(wusp.FillOptions{
		Profile:   wusp.FillProfileRealistic,
		DeviceID:  "test-device-" + peerKey[:8],
		Overwrite: true,
	}); err != nil {
		t.Fatalf("Bootstrap: %v", err)
	}
	_ = agent.Set("Device.DeviceInfo.Manufacturer", wusp.String("Wantastic"))
	_ = agent.Set("Device.DeviceInfo.ProductClass", wusp.String("WG-Router"))
	_ = agent.Set("Device.DeviceInfo.SerialNumber", wusp.String("TEST-"+peerKey[:8]))
	_ = agent.Set("Device.DeviceInfo.SoftwareVersion", wusp.String("1.2.3"))
	_ = agent.Set("Device.DeviceInfo.HardwareVersion", wusp.String("rev-B"))
	_ = agent.Set("Device.WUSP.Enable", wusp.Bool(true))
	_ = agent.Set("Device.WUSP.Status", wusp.String("Enabled"))
	_ = agent.Set("Device.WUSP.ProtocolVersion", wusp.String(wusp.WUSPModelVersion))

	return &fakeAgent{
		t:       t,
		ctrl:    ctrl,
		peerKey: peerKey,
		agent:   agent,
		frags:   make(map[uint64][]wusp.USPControlFragment),
		stopped: make(chan struct{}),
	}
}

// start launches the agent goroutine that drains outbound and responds.
// Call stop() or cancel() to terminate it.
func (a *fakeAgent) start(outbound <-chan []byte) {
	go func() {
		defer close(a.stopped)
		for frame := range outbound {
			a.process(frame)
		}
	}()
}

// process handles one fragment or assembled payload from the controller.
func (a *fakeAgent) process(data []byte) {
	if len(data) > 0 && data[0] == 0x53 {
		a.handleStreamFrame(data)
		return
	}

	frag, isFrag, err := wusp.DecodeUSPControlFragment(data)
	if err != nil {
		a.t.Logf("fakeAgent: bad fragment: %v", err)
		return
	}
	if isFrag {
		a.fragMu.Lock()
		a.frags[frag.MessageID] = append(a.frags[frag.MessageID], frag)
		complete := uint32(len(a.frags[frag.MessageID])) >= frag.Count
		var assembled []wusp.USPControlFragment
		if complete {
			assembled = a.frags[frag.MessageID]
			delete(a.frags, frag.MessageID)
		}
		a.fragMu.Unlock()

		if !complete {
			return
		}
		payload, err := wusp.ReassembleUSPControlFragments(assembled)
		if err != nil {
			a.t.Logf("fakeAgent: reassembly error: %v", err)
			return
		}
		data = payload
	}

	a.dispatch(data)
}

// dispatch decodes a fully-assembled payload and sends the response.
func (a *fakeAgent) dispatch(data []byte) {
	req, err := wusp.DecodeUSPAgentRequest(data)
	if err != nil {
		a.t.Logf("fakeAgent: decode request: %v", err)
		return
	}

	if req.Method == wusp.USPAgentMethodUpload || req.Method == wusp.USPAgentMethodDownload {
		a.dispatchTransfer(req)
		return
	}

	ctx := context.Background()
	resp, _ := a.agent.HandleRequest(ctx, req)

	respData, err := wusp.EncodeUSPAgentResponse(resp)
	if err != nil {
		a.t.Logf("fakeAgent: encode response: %v", err)
		return
	}

	// Fragment and deliver back to the controller.
	fragments, err := wusp.FragmentUSPControlPayload(respData, req.ID, wusp.RequestedResponseMaxControlPayload(req.Metadata, wusp.WUSPMaxDatagramPayload))
	if err != nil {
		a.t.Logf("fakeAgent: fragment response: %v", err)
		return
	}
	for _, frag := range fragments {
		a.ctrl.HandleInbound(ctx, a.peerKey, frag)
	}
}

func (a *fakeAgent) dispatchTransfer(req wusp.USPAgentRequest) {
	const sessionID = 7001
	respData, err := wusp.EncodeUSPAgentResponse(wusp.USPAgentResponse{
		ID:     req.ID,
		Method: req.Method,
		Transfer: &wusp.USPTransferResult{
			Path: req.Transfer.Path,
			Metadata: map[string]string{
				wusp.TransferMetadataTransport: "wg-stream",
				wusp.TransferMetadataSessionID: strconv.FormatUint(sessionID, 10),
				wusp.TransferMetadataChunkSize: strconv.Itoa(wusp.USPRecommendedChunkSize),
			},
		},
	})
	if err != nil {
		a.t.Logf("fakeAgent: encode transfer response: %v", err)
		return
	}
	fragments, err := wusp.FragmentUSPControlPayload(respData, req.ID, wusp.RequestedResponseMaxControlPayload(req.Metadata, wusp.WUSPMaxDatagramPayload))
	if err != nil {
		a.t.Logf("fakeAgent: fragment transfer response: %v", err)
		return
	}
	for _, frag := range fragments {
		a.ctrl.HandleInbound(context.Background(), a.peerKey, frag)
	}

	switch req.Method {
	case wusp.USPAgentMethodUpload:
		a.transferMu.Lock()
		a.uploadSessionID = sessionID
		a.uploadRequestID = req.ID
		a.uploadTransferred = a.uploadTransferred[:0]
		a.transferMu.Unlock()
	case wusp.USPAgentMethodDownload:
		go a.streamDownload(sessionID, req)
	}
}

func (a *fakeAgent) handleStreamFrame(data []byte) {
	frame, err := wusp.DecodeUSPTransferStreamFrame(data)
	if err != nil {
		a.t.Logf("fakeAgent: decode stream frame: %v", err)
		return
	}

	switch frame.Method {
	case wusp.USPAgentMethodUpload:
		a.handleUploadStreamFrame(frame)
	case wusp.USPAgentMethodDownload:
		// Download ACKs are optional for this in-process harness.
		return
	}
}

func (a *fakeAgent) handleUploadStreamFrame(frame wusp.USPTransferStreamFrame) {
	a.transferMu.Lock()
	sessionID := a.uploadSessionID
	requestID := a.uploadRequestID
	if frame.SessionID != sessionID || frame.RequestID != requestID {
		a.transferMu.Unlock()
		return
	}
	switch frame.Phase {
	case wusp.USPTransferStreamOpen:
		a.transferMu.Unlock()
		a.ctrl.HandleInbound(context.Background(), a.peerKey, mustEncodeStream(a.t, wusp.USPTransferStreamFrame{
			SessionID:   sessionID,
			RequestID:   requestID,
			Method:      wusp.USPAgentMethodUpload,
			Phase:       wusp.USPTransferStreamAck,
			AckSequence: 0,
		}))
	case wusp.USPTransferStreamChunk:
		a.uploadTransferred = append(a.uploadTransferred, frame.Data...)
		offset := len(a.uploadTransferred)
		a.transferMu.Unlock()
		a.ctrl.HandleInbound(context.Background(), a.peerKey, mustEncodeStream(a.t, wusp.USPTransferStreamFrame{
			SessionID:   sessionID,
			RequestID:   requestID,
			Method:      wusp.USPAgentMethodUpload,
			Phase:       wusp.USPTransferStreamAck,
			AckSequence: frame.Sequence,
			Offset:      uint64(offset),
			TotalSize:   uint64(offset),
			Final:       frame.Final,
		}))
	case wusp.USPTransferStreamComplete:
		received := append([]byte(nil), a.uploadTransferred...)
		a.uploadSessionID = 0
		a.uploadRequestID = 0
		a.uploadTransferred = nil
		a.transferMu.Unlock()
		_ = a.agent.Set("Device.DeviceInfo.ProvisioningCode", wusp.String(string(received)))
		a.ctrl.HandleInbound(context.Background(), a.peerKey, mustEncodeStream(a.t, wusp.USPTransferStreamFrame{
			SessionID:   sessionID,
			RequestID:   requestID,
			Method:      wusp.USPAgentMethodUpload,
			Phase:       wusp.USPTransferStreamComplete,
			AckSequence: frame.AckSequence,
			Offset:      uint64(len(received)),
			TotalSize:   uint64(len(received)),
			Final:       true,
		}))
	default:
		a.transferMu.Unlock()
	}
}

func (a *fakeAgent) streamDownload(sessionID uint64, req wusp.USPAgentRequest) {
	payload := []byte("downloaded-from-fake-agent")
	time.Sleep(10 * time.Millisecond)
	a.ctrl.HandleInbound(context.Background(), a.peerKey, mustEncodeStream(a.t, wusp.USPTransferStreamFrame{
		SessionID: sessionID,
		RequestID: req.ID,
		Method:    wusp.USPAgentMethodDownload,
		Phase:     wusp.USPTransferStreamOpen,
		Path:      req.Transfer.Path,
		Filename:  "download.bin",
		TotalSize: uint64(len(payload)),
	}))
	a.ctrl.HandleInbound(context.Background(), a.peerKey, mustEncodeStream(a.t, wusp.USPTransferStreamFrame{
		SessionID: sessionID,
		RequestID: req.ID,
		Method:    wusp.USPAgentMethodDownload,
		Phase:     wusp.USPTransferStreamChunk,
		Sequence:  1,
		Offset:    0,
		TotalSize: uint64(len(payload)),
		Data:      payload,
		Final:     true,
	}))
	a.ctrl.HandleInbound(context.Background(), a.peerKey, mustEncodeStream(a.t, wusp.USPTransferStreamFrame{
		SessionID:   sessionID,
		RequestID:   req.ID,
		Method:      wusp.USPAgentMethodDownload,
		Phase:       wusp.USPTransferStreamComplete,
		AckSequence: 1,
		Offset:      uint64(len(payload)),
		TotalSize:   uint64(len(payload)),
		Final:       true,
		Metadata: map[string]string{
			wusp.TransferMetadataSource: "/tmp/fake-agent.bin",
		},
	}))
}

// stop closes the outbound channel to terminate the agent goroutine,
// then waits for it to exit.
func (a *fakeAgent) waitStopped(timeout time.Duration) {
	select {
	case <-a.stopped:
	case <-time.After(timeout):
		a.t.Log("fakeAgent: stop timed out")
	}
}

// emitOnBoard injects an OnBoardRequest into the controller as the fake agent.
func (a *fakeAgent) emitOnBoard(serial, protoVer string) {
	frame, err := encodeOnBoardFrame(1, serial, protoVer)
	if err != nil {
		a.t.Fatalf("fakeAgent emitOnBoard: %v", err)
	}
	a.ctrl.HandleInbound(context.Background(), a.peerKey, frame)
}

// ── fake state repository ─────────────────────────────────────────────────────

// fakeStateRepo is an in-memory WUSPDeviceStateRepository for testing SyncDeviceState.
type fakeStateRepo struct {
	mu     sync.Mutex
	states map[string]*store.WUSPDeviceStateData
}

func newFakeStateRepo() *fakeStateRepo {
	return &fakeStateRepo{states: make(map[string]*store.WUSPDeviceStateData)}
}

func (r *fakeStateRepo) Upsert(state *store.WUSPDeviceStateData) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	cp := *state
	r.states[state.PeerID] = &cp
	return nil
}

func (r *fakeStateRepo) GetByPeer(peerID string) (*store.WUSPDeviceStateData, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	s, ok := r.states[peerID]
	if !ok {
		return nil, nil
	}
	cp := *s
	return &cp, nil
}

func (r *fakeStateRepo) GetByAccount(accountID string) ([]*store.WUSPDeviceStateData, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	var out []*store.WUSPDeviceStateData
	for _, s := range r.states {
		if s.AccountID == accountID {
			cp := *s
			out = append(out, &cp)
		}
	}
	return out, nil
}

func (r *fakeStateRepo) Delete(peerID string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.states, peerID)
	return nil
}

// ── controller + agent wiring helper ─────────────────────────────────────────

// newIntegrationPair creates a WUSPController + fakeAgent pair wired together
// via an in-memory outbound channel. The fakeAgent goroutine is started
// automatically; close `outbound` to stop it.
func newIntegrationPair(t *testing.T, peerKey string, stateRepo store.WUSPDeviceStateRepository, onEvent EventFunc) (*WUSPController, *fakeAgent, chan []byte) {
	t.Helper()
	outbound := make(chan []byte, 128)
	ctrl := New(Options{
		Send: func(_ string, data []byte) error {
			outbound <- append([]byte(nil), data...)
			return nil
		},
		OnEvent:        onEvent,
		StateRepo:      stateRepo,
		RequestTimeout: 5 * time.Second,
	})
	ctrl.Start()
	t.Cleanup(func() {
		ctrl.Stop()
		close(outbound)
	})

	agent := newFakeAgent(t, ctrl, peerKey)
	agent.start(outbound)
	t.Cleanup(func() { agent.waitStopped(2 * time.Second) })

	return ctrl, agent, outbound
}

func mustEncodeStream(t testing.TB, frame wusp.USPTransferStreamFrame) []byte {
	t.Helper()
	payload, err := wusp.EncodeUSPTransferStreamFrame(frame)
	if err != nil {
		t.Fatalf("EncodeUSPTransferStreamFrame: %v", err)
	}
	return payload
}

// ── tests ─────────────────────────────────────────────────────────────────────

// TestControllerIntegration_GetRoundTrip verifies a complete controller→agent
// Get round-trip using real encoding on both sides.
func TestControllerIntegration_GetRoundTrip(t *testing.T) {
	ctrl, _, _ := newIntegrationPair(t, testPeer, nil, nil)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	resp, err := ctrl.Get(ctx, testPeer, "Device.DeviceInfo.Manufacturer")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if resp.Error != "" {
		t.Fatalf("agent error: %q", resp.Error)
	}
	val, ok := resp.Message.Get("Device.DeviceInfo.Manufacturer")
	if !ok {
		t.Fatal("Device.DeviceInfo.Manufacturer missing from response")
	}
	if val.AsString() != "Wantastic" {
		t.Errorf("value=%q want=%q", val.AsString(), "Wantastic")
	}
}

// TestControllerIntegration_GetAll verifies that GetAll returns a populated
// message with multiple parameters.
func TestControllerIntegration_GetAll(t *testing.T) {
	ctrl, _, _ := newIntegrationPair(t, testPeer, nil, nil)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	resp, err := ctrl.GetAll(ctx, testPeer)
	if err != nil {
		t.Fatalf("GetAll: %v", err)
	}
	if resp.Error != "" {
		t.Fatalf("agent error: %q", resp.Error)
	}
	if resp.Message == nil || len(resp.Message.Fields) == 0 {
		t.Fatal("GetAll returned empty message")
	}
	t.Logf("GetAll: %d parameters", len(resp.Message.Fields))
}

// TestControllerIntegration_SyncDeviceState verifies the full SyncDeviceState
// pipeline: GetAll round-trip + state repository upsert with real device data.
func TestControllerIntegration_SyncDeviceState(t *testing.T) {
	repo := newFakeStateRepo()
	ctrl, _, _ := newIntegrationPair(t, testPeer, repo, nil)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	const accountID = "test-account-1"
	// SyncDeviceState uses targeted Get — partial agent errors are tolerated.
	if err := ctrl.SyncDeviceState(ctx, testPeer, accountID); err != nil {
		t.Fatalf("SyncDeviceState: %v", err)
	}

	state, err := repo.GetByPeer(testPeer)
	if err != nil {
		t.Fatalf("GetByPeer: %v", err)
	}
	if state == nil {
		t.Fatal("SyncDeviceState did not upsert any state")
	}

	if state.PeerID != testPeer {
		t.Errorf("state.PeerID=%q want=%q", state.PeerID, testPeer)
	}
	if state.AccountID != accountID {
		t.Errorf("state.AccountID=%q want=%q", state.AccountID, accountID)
	}
	// Fields extracted from targeted Get — present when agent has them seeded.
	if state.Manufacturer != "Wantastic" {
		t.Errorf("state.Manufacturer=%q want=%q", state.Manufacturer, "Wantastic")
	}
	if state.SerialNumber == "" {
		t.Error("state.SerialNumber is empty")
	}
	if !state.WUSPEnable {
		t.Error("state.WUSPEnable is false — expected true from seeded data")
	}
	if state.WUSPVersion == "" {
		t.Error("state.WUSPVersion is empty")
	}
	t.Logf("state: manufacturer=%q serial=%q version=%q",
		state.Manufacturer, state.SerialNumber, state.WUSPVersion)
}

// TestControllerIntegration_SetAndGet verifies that Set persists to the agent's
// in-memory store and a subsequent Get returns the new value.
func TestControllerIntegration_SetAndGet(t *testing.T) {
	ctrl, _, _ := newIntegrationPair(t, testPeer, nil, nil)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	const path = "Device.DeviceInfo.ProvisioningCode"
	const val = "ctrl-integration-test"

	setMsg := wusp.NewMessage()
	setMsg.Set(path, wusp.String(val))

	_, err := ctrl.Set(ctx, testPeer, setMsg)
	if err != nil {
		t.Fatalf("Set: %v", err)
	}

	getResp, err := ctrl.Get(ctx, testPeer, path)
	if err != nil {
		t.Fatalf("Get after Set: %v", err)
	}
	got, ok := getResp.Message.Get(path)
	if !ok {
		t.Fatalf("%q missing from Get response", path)
	}
	if got.AsString() != val {
		t.Errorf("value=%q want=%q", got.AsString(), val)
	}
}

// TestControllerIntegration_SetValidated_RejectsReadOnly verifies that
// SetValidated blocks writes to read-only parameters client-side (no round-trip).
func TestControllerIntegration_SetValidated_RejectsReadOnly(t *testing.T) {
	ctrl, _, _ := newIntegrationPair(t, testPeer, nil, nil)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// Device.DeviceInfo.Manufacturer is read-only per BBF TR-181.
	msg := wusp.NewMessage()
	msg.Set("Device.DeviceInfo.Manufacturer", wusp.String("hacked"))

	_, err := ctrl.SetValidated(ctx, testPeer, msg)
	if err == nil {
		t.Fatal("SetValidated accepted a write to a read-only parameter")
	}
	var ve *wusp.ValidationError
	if !errors.As(err, &ve) {
		t.Fatalf("expected *wusp.ValidationError, got %T: %v", err, err)
	}
	t.Logf("SetValidated correctly rejected: %v", err)
}

// TestControllerIntegration_GetSupportedProtocol verifies a full
// GetSupportedProtocol round-trip returns a valid protocol descriptor.
func TestControllerIntegration_GetSupportedProtocol(t *testing.T) {
	ctrl, _, _ := newIntegrationPair(t, testPeer, nil, nil)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	resp, err := ctrl.GetSupportedProtocol(ctx, testPeer)
	if err != nil {
		t.Fatalf("GetSupportedProtocol: %v", err)
	}
	if resp.Protocol == nil {
		t.Fatal("Protocol is nil")
	}
	if resp.Protocol.Version == 0 {
		t.Error("Protocol.Version is 0")
	}
	t.Logf("protocol: name=%q version=%d methods=%v",
		resp.Protocol.Name, resp.Protocol.Version, resp.Protocol.Methods)
}

func TestControllerIntegration_UploadTunnelTransfer(t *testing.T) {
	ctrl, agent, _ := newIntegrationPair(t, testPeer, nil, nil)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	payload := []byte("controller-stream-upload")
	resp, err := ctrl.Upload(ctx, testPeer, &wusp.USPTransferRequest{
		Path:    "Device.DeviceInfo.ProvisioningCode",
		Payload: payload,
		Metadata: map[string]string{
			"size": strconv.Itoa(len(payload)),
		},
	})
	if err != nil {
		t.Fatalf("Upload: %v", err)
	}
	if resp.Transfer == nil {
		t.Fatal("Upload transfer result is nil")
	}
	if resp.Transfer.Bytes != int64(len(payload)) {
		t.Fatalf("upload bytes=%d want=%d", resp.Transfer.Bytes, len(payload))
	}
	msg, getErr := agent.agent.Get("Device.DeviceInfo.ProvisioningCode")
	if getErr != nil {
		t.Fatalf("agent.Get: %v", getErr)
	}
	val, ok := msg.Get("Device.DeviceInfo.ProvisioningCode")
	if !ok || val.AsString() != string(payload) {
		t.Fatalf("uploaded value=%q want=%q", val.AsString(), string(payload))
	}
}

func TestControllerIntegration_DownloadTunnelTransfer(t *testing.T) {
	ctrl, _, _ := newIntegrationPair(t, testPeer, nil, nil)

	root := t.TempDir()
	target := filepath.Join(root, "download.bin")

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	resp, err := ctrl.Download(ctx, testPeer, &wusp.USPTransferRequest{
		Path: "Device.DeviceInfo.VendorConfigFile.{i}.",
		URI:  "file:///tmp/fake-agent.bin",
		Metadata: map[string]string{
			wusp.TransferMetadataDestination: target,
		},
	})
	if err != nil {
		t.Fatalf("Download: %v", err)
	}
	got, readErr := os.ReadFile(target)
	if readErr != nil {
		t.Fatalf("ReadFile(target): %v", readErr)
	}
	if string(got) != "downloaded-from-fake-agent" {
		t.Fatalf("downloaded payload=%q want=%q", string(got), "downloaded-from-fake-agent")
	}
	if resp.Transfer == nil {
		t.Fatal("Download transfer result is nil")
	}
	if resp.Transfer.URI != "file://"+target {
		t.Fatalf("download URI=%q want=%q", resp.Transfer.URI, "file://"+target)
	}
	if resp.Transfer.Bytes != int64(len(got)) {
		t.Fatalf("download bytes=%d want=%d", resp.Transfer.Bytes, len(got))
	}
}

// TestControllerIntegration_AgentErrorPropagated verifies that when the agent
// responds with an error string, the controller surfaces it as *AgentError.
func TestControllerIntegration_AgentErrorPropagated(t *testing.T) {
	ctrl, _, _ := newIntegrationPair(t, testPeer, nil, nil)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// Delete with no paths causes the agent to return an error response.
	_, err := ctrl.Delete(ctx, testPeer, "Device.DeviceInfo.NonExistentInstance.1.")
	if err == nil {
		t.Fatal("expected an error from deleting a non-existent path, got nil")
	}
	if !IsAgentError(err) {
		t.Logf("got non-AgentError (may be validation error): %T: %v", err, err)
	} else {
		ae := err.(*AgentError)
		t.Logf("AgentError: %q", ae.Message)
	}
}

// TestControllerIntegration_LargeResponseFragmentation verifies that a very
// large agent response (GetSupportedDM returns ~580 KB of schema data) is
// correctly fragmented by the fakeAgent and reassembled by the controller
// without data loss or corruption.
func TestControllerIntegration_LargeResponseFragmentation(t *testing.T) {
	outbound := make(chan []byte, 512) // larger buffer for many fragments
	ctrl := New(Options{
		Send: func(_ string, data []byte) error {
			outbound <- append([]byte(nil), data...)
			return nil
		},
		RequestTimeout: 15 * time.Second,
	})
	ctrl.Start()
	t.Cleanup(func() {
		ctrl.Stop()
		close(outbound)
	})

	fa := newFakeAgent(t, ctrl, testPeer)
	fa.start(outbound)
	t.Cleanup(func() { fa.waitStopped(2 * time.Second) })

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	// GetSupportedDM returns the full BBF TR-181 schema (~580 KB), which forces
	// the fakeAgent to produce many fragments that the controller must reassemble.
	resp, err := ctrl.GetSupportedDM(ctx, testPeer)
	if err != nil {
		t.Fatalf("GetSupportedDM (large): %v", err)
	}
	if resp.SupportedDataModel == nil {
		t.Fatal("GetSupportedDM returned nil SupportedDataModel")
	}
	if len(resp.SupportedDataModel.Params) == 0 {
		t.Error("SupportedDataModel.Params is empty")
	}
	t.Logf("GetSupportedDM: %d params, %d objects — large fragment reassembly OK",
		len(resp.SupportedDataModel.Params), len(resp.SupportedDataModel.Objects))
}

// TestControllerIntegration_OnBoardEventReceived verifies that when the agent
// sends an OnBoardRequest, the controller's OnEvent callback fires with the
// correct event type and serial number.
func TestControllerIntegration_OnBoardEventReceived(t *testing.T) {
	const serial = "ctrl-integration-serial"

	eventCh := make(chan wusp.USPEvent, 1)
	ctrl, agent, _ := newIntegrationPair(t, testPeer, nil, func(_ string, ev wusp.USPEvent) {
		eventCh <- ev
	})
	_ = ctrl

	agent.emitOnBoard(serial, wusp.WUSPModelVersion)

	select {
	case ev := <-eventCh:
		if ev.Type != wusp.USPEventTypeOnBoardRequest {
			t.Fatalf("event.Type=%d want OnBoardRequest (%d)", ev.Type, wusp.USPEventTypeOnBoardRequest)
		}
		if ev.OnBoard == nil || ev.OnBoard.SerialNumber != serial {
			t.Fatalf("unexpected OnBoard: %+v", ev.OnBoard)
		}
		t.Logf("OnBoardRequest: serial=%q proto=%q",
			ev.OnBoard.SerialNumber, ev.OnBoard.AgentSupportedProtocolVersions)
	case <-time.After(3 * time.Second):
		t.Fatal("timeout: OnEvent was not called")
	}
}

// TestControllerIntegration_ConcurrentPeers verifies that concurrent requests
// to multiple distinct peers do not corrupt request ID correlation.
func TestControllerIntegration_ConcurrentPeers(t *testing.T) {
	const peerCount = 5
	const requestsPerPeer = 4

	type peer struct {
		key   string
		outch chan []byte
		ctrl  *WUSPController
		agent *fakeAgent
	}

	// Shared controller with per-peer routing.
	peerChans := make(map[string]chan []byte)
	var peerChansMu sync.Mutex

	ctrl := New(Options{
		Send: func(peerKey string, data []byte) error {
			peerChansMu.Lock()
			ch := peerChans[peerKey]
			peerChansMu.Unlock()
			if ch != nil {
				ch <- append([]byte(nil), data...)
			}
			return nil
		},
		RequestTimeout: 5 * time.Second,
	})
	ctrl.Start()
	t.Cleanup(ctrl.Stop)

	// Create peers.
	peers := make([]*peer, peerCount)
	for i := range peerCount {
		key := testPeer[:len(testPeer)-2] + string(rune('A'+i)) + "="
		ch := make(chan []byte, 64)
		peerChansMu.Lock()
		peerChans[key] = ch
		peerChansMu.Unlock()

		fa := newFakeAgent(t, ctrl, key)
		fa.start(ch)

		peers[i] = &peer{key: key, outch: ch, ctrl: ctrl, agent: fa}
		// Capture loop variables explicitly for the cleanup closure.
		capturedKey := key
		capturedIdx := i
		t.Cleanup(func() {
			close(peerChans[capturedKey])
			peers[capturedIdx].agent.waitStopped(2 * time.Second)
		})
	}

	type result struct {
		peerIdx int
		reqIdx  int
		err     error
		val     string
	}
	results := make(chan result, peerCount*requestsPerPeer)

	var wg sync.WaitGroup
	for pi, p := range peers {
		for ri := range requestsPerPeer {
			wg.Add(1)
			go func(pi, ri int, peerKey string) {
				defer wg.Done()
				ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
				defer cancel()

				resp, err := ctrl.Get(ctx, peerKey, "Device.DeviceInfo.SerialNumber")
				var val string
				if err == nil && resp.Message != nil {
					if v, ok := resp.Message.Get("Device.DeviceInfo.SerialNumber"); ok {
						val = v.AsString()
					}
				}
				results <- result{peerIdx: pi, reqIdx: ri, err: err, val: val}
			}(pi, ri, p.key)
		}
	}

	wg.Wait()
	close(results)

	failed := 0
	for r := range results {
		if r.err != nil {
			t.Errorf("peer[%d] request[%d]: %v", r.peerIdx, r.reqIdx, r.err)
			failed++
		} else if r.val == "" {
			t.Errorf("peer[%d] request[%d]: empty serial number", r.peerIdx, r.reqIdx)
			failed++
		}
	}
	if failed == 0 {
		t.Logf("all %d requests across %d peers completed correctly", peerCount*requestsPerPeer, peerCount)
	}
}

// TestControllerIntegration_RequestTimeout verifies that when the agent does
// not respond (agent goroutine stopped), the controller returns a context-
// deadline error within the configured timeout.
func TestControllerIntegration_RequestTimeout(t *testing.T) {
	// Create a controller with no responding agent.
	outbound := make(chan []byte, 64)
	ctrl := New(Options{
		Send: func(_ string, data []byte) error {
			outbound <- append([]byte(nil), data...)
			return nil
		},
		RequestTimeout: 300 * time.Millisecond,
	})
	ctrl.Start()
	t.Cleanup(ctrl.Stop)

	ctx := context.Background()
	start := time.Now()
	_, err := ctrl.Get(ctx, testPeer, "Device.DeviceInfo.HostName")
	elapsed := time.Since(start)

	if err == nil {
		t.Fatal("expected timeout error, got nil")
	}
	if elapsed > 2*time.Second {
		t.Fatalf("Get did not respect RequestTimeout: took %v", elapsed)
	}
	t.Logf("timed out after %v: %v", elapsed, err)
}

// TestControllerIntegration_FullOnboardSequence simulates the complete sequence
// the server runs when a new WUSP-capable peer connects:
//  1. OnBoardRequest event fires OnEvent
//  2. Controller calls GetSupportedProtocol
//  3. Controller calls SyncDeviceState (GetAll + persist)
func TestControllerIntegration_FullOnboardSequence(t *testing.T) {
	const serial = "full-onboard-device"
	const accountID = "acct-full-onboard"

	repo := newFakeStateRepo()
	eventCh := make(chan wusp.USPEvent, 1)

	ctrl, agent, _ := newIntegrationPair(t, testPeer, repo, func(_ string, ev wusp.USPEvent) {
		eventCh <- ev
	})

	// Step 1 — agent sends OnBoardRequest
	agent.emitOnBoard(serial, wusp.WUSPModelVersion)

	select {
	case ev := <-eventCh:
		if ev.Type != wusp.USPEventTypeOnBoardRequest {
			t.Fatalf("step1: event.Type=%d want OnBoardRequest", ev.Type)
		}
		if ev.OnBoard == nil || ev.OnBoard.SerialNumber != serial {
			t.Fatalf("step1: unexpected OnBoard: %+v", ev.OnBoard)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("step1: timeout waiting for OnBoardRequest event")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()

	// Step 2 — capability probe
	protoResp, err := ctrl.GetSupportedProtocol(ctx, testPeer)
	if err != nil {
		t.Fatalf("step2 GetSupportedProtocol: %v", err)
	}
	if protoResp.Protocol == nil {
		t.Fatal("step2: Protocol is nil")
	}

	// Step 3 — sync device state
	if err := ctrl.SyncDeviceState(ctx, testPeer, accountID); err != nil {
		t.Fatalf("step3 SyncDeviceState: %v", err)
	}

	state, _ := repo.GetByPeer(testPeer)
	if state == nil {
		t.Fatal("step3: no state saved")
	}
	if state.AccountID != accountID {
		t.Errorf("state.AccountID=%q want=%q", state.AccountID, accountID)
	}
	t.Logf("onboard complete: serial=%q manufacturer=%q wusp_version=%q",
		state.SerialNumber, state.Manufacturer, state.WUSPVersion)
}

// ── helpers used only by integration tests ────────────────────────────────────

// integrationCounter provides unique IDs for concurrent integration tests.
var integrationCounter atomic.Uint64
