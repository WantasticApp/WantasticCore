package hooks

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	pb "WantasticCore/internal/types"
	core "WantasticCore/internal/core"
)

// mockWUSPServer is a minimal WUSPServiceHandler implementation that
// intercepts UploadSnapshotBackup calls so we can assert what the HTTP
// hook forwarded. It embeds core.UnimplementedWUSPServiceHandler so it
// satisfies the (large) handler interface without stubbing every method.
type mockWUSPServer struct {
	core.UnimplementedWUSPServiceHandler
	gotReq *pb.UploadSnapshotBackupRequest
	resp   *pb.UploadSnapshotBackupResponse
	err    error
}

func (m *mockWUSPServer) UploadSnapshotBackup(_ context.Context, req *pb.UploadSnapshotBackupRequest) (*pb.UploadSnapshotBackupResponse, error) {
	m.gotReq = &pb.UploadSnapshotBackupRequest{
		UploadToken: req.UploadToken,
		BackupName:  req.BackupName,
		BackupFile:  append([]byte(nil), req.BackupFile...),
	}
	if m.err != nil {
		return nil, m.err
	}
	if m.resp == nil {
		return &pb.UploadSnapshotBackupResponse{}, nil
	}
	return &pb.UploadSnapshotBackupResponse{
		Success:  m.resp.Success,
		NewToken: m.resp.NewToken,
	}, nil
}

// sampleRouterOSExport is a representative /export show-sensitive output.
// Keep this small but realistic so we exercise the text round-trip path.
const sampleRouterOSExport = `# 2026-04-25 14:00:00 by RouterOS 7.10
/interface wireguard
add listen-port=51820 mtu=1420 name=wg-wantastic private-key="yLA3ChuVGxTGbCuyR2EwHOmdmWfgp07quvhns0jzZmA="
/interface wireguard peers
add allowed-address=10.0.0.0/27 endpoint-address=wg.wantastic.local endpoint-port=51820 \
    interface=wg-wantastic persistent-keepalive=25s public-key="z7lx3lUh/qFRmsNGlDaVnesZNcBIwMcIWIBOtH17yyk="
/ip address
add address=10.0.0.4/32 interface=wg-wantastic
/ip firewall filter
add action=accept chain=input dst-port=51820 place-before=0 protocol=udp
add action=accept chain=forward in-interface=wg-wantastic place-before=0
add action=accept chain=forward out-interface=wg-wantastic place-before=0
/ip firewall nat
add action=masquerade chain=srcnat out-interface=wg-wantastic
`

func newTestHandler(t *testing.T, mock *mockWUSPServer) *Handler {
	t.Helper()
	h, err := NewHandler("0123456789abcdef0123456789abcdef")
	if err != nil {
		t.Fatalf("NewHandler: %v", err)
	}
	h.SetServices(HookServices{WUSP: mock})
	return h
}

// TestBackupUpload_HappyPath sends a POST with a valid token and verifies the
// in-process service call carries the correct token, filename, and full body bytes.
func TestBackupUpload_HappyPath(t *testing.T) {
	mock := &mockWUSPServer{
		resp: &pb.UploadSnapshotBackupResponse{Success: true, NewToken: "rotated-token-abc"},
	}
	h := newTestHandler(t, mock)

	body := strings.NewReader(sampleRouterOSExport)
	req := httptest.NewRequest(http.MethodPost, "/hooks/backup?token=bckup_test_token_value", body)
	req.Header.Set("Content-Type", "application/x-rsc")
	req.Header.Set("X-Backup-Filename", "core-router-2026-04-25.rsc")

	rr := httptest.NewRecorder()
	h.HandleBackupUpload(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", rr.Code, rr.Body.String())
	}

	if mock.gotReq == nil {
		t.Fatal("service call not invoked")
	}
	if mock.gotReq.UploadToken != "bckup_test_token_value" {
		t.Errorf("UploadToken = %q, want %q", mock.gotReq.UploadToken, "bckup_test_token_value")
	}
	if mock.gotReq.BackupName != "core-router-2026-04-25.rsc" {
		t.Errorf("BackupName = %q, want core-router-2026-04-25.rsc", mock.gotReq.BackupName)
	}
	if string(mock.gotReq.BackupFile) != sampleRouterOSExport {
		t.Errorf("BackupFile mismatch (len got=%d want=%d)", len(mock.gotReq.BackupFile), len(sampleRouterOSExport))
	}

	var resp struct {
		Success  bool   `json:"success"`
		NewToken string `json:"new_token"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
		t.Fatalf("response json: %v (body=%s)", err, rr.Body.String())
	}
	if !resp.Success || resp.NewToken != "rotated-token-abc" {
		t.Errorf("response = %+v, want success=true new_token=rotated-token-abc", resp)
	}
}

// TestBackupUpload_MissingToken returns 400 without making a service call.
func TestBackupUpload_MissingToken(t *testing.T) {
	mock := &mockWUSPServer{}
	h := newTestHandler(t, mock)

	body := strings.NewReader(sampleRouterOSExport)
	req := httptest.NewRequest(http.MethodPost, "/hooks/backup", body)
	req.Header.Set("Content-Type", "application/x-rsc")

	rr := httptest.NewRecorder()
	h.HandleBackupUpload(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400; body=%s", rr.Code, rr.Body.String())
	}
	if mock.gotReq != nil {
		t.Errorf("service call was made; should not have been")
	}
}

// TestBackupUpload_ReadsBodyBoundedSize ensures the handler caps the upload
// at the documented limit (8 MiB) and rejects oversize payloads with 413.
func TestBackupUpload_ReadsBodyBoundedSize(t *testing.T) {
	mock := &mockWUSPServer{}
	h := newTestHandler(t, mock)

	// 9 MiB body — over the 8 MiB cap.
	big := strings.Repeat("a", 9<<20)
	req := httptest.NewRequest(http.MethodPost, "/hooks/backup?token=t", strings.NewReader(big))
	req.Header.Set("Content-Type", "application/x-rsc")

	rr := httptest.NewRecorder()
	h.HandleBackupUpload(rr, req)

	if rr.Code != http.StatusRequestEntityTooLarge && rr.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 413 or 400; body=%s", rr.Code, rr.Body.String())
	}
	if mock.gotReq != nil {
		t.Errorf("service call was made; should have been rejected pre-call")
	}
}

// silence unused-import warnings on io if HandleBackup ever stops needing it.
var _ = io.Discard
