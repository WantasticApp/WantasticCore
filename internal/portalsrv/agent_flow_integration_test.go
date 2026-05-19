package portalsrv

import (
	"crypto/hmac"
	"crypto/sha256"
	"crypto/tls"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	"WantasticCore/internal/portalsrv/pkg/cipher"
)

// Integration tests against a real running portal server.
//
// Enable explicitly:
//   WANTASTIC_INTEGRATION=1 go test ./cmd/web/portal -run Integration -v
//
// Optional env vars:
//   WANTASTIC_BASE_URL=https://wantastic.local
//   WANTASTIC_GRPC_ADDR=127.0.0.1:52990
//   WANTASTIC_AUTH0_OAUTH_DOMAIN=dev-xxxx.us.auth0.com
//   WANTASTIC_SKIP_TLS_VERIFY=1
//   WANTASTIC_STRICT_TLS=1

func requireIntegration(t *testing.T) string {
	t.Helper()
	if os.Getenv("WANTASTIC_INTEGRATION") != "1" {
		t.Skip("set WANTASTIC_INTEGRATION=1 to run real-server integration tests")
	}

	baseURL := os.Getenv("WANTASTIC_BASE_URL")
	if baseURL == "" {
		baseURL = "https://wantastic.local"
	}
	if !strings.HasPrefix(baseURL, "http") {
		t.Fatalf("WANTASTIC_BASE_URL must start with http/https, got %q", baseURL)
	}

	// Fast preflight connectivity check so failures are clear.
	host := strings.TrimPrefix(strings.TrimPrefix(baseURL, "https://"), "http://")
	host = strings.Split(host, "/")[0]
	if !strings.Contains(host, ":") {
		if strings.HasPrefix(baseURL, "https://") {
			host += ":443"
		} else {
			host += ":80"
		}
	}
	conn, err := net.DialTimeout("tcp", host, 2*time.Second)
	if err != nil {
		t.Fatalf("cannot reach dev server %s: %v", host, err)
	}
	_ = conn.Close()

	return strings.TrimRight(baseURL, "/")
}

func shouldSkipTLSVerify(baseURL string) bool {
	if os.Getenv("WANTASTIC_STRICT_TLS") == "1" {
		return false
	}
	if os.Getenv("WANTASTIC_SKIP_TLS_VERIFY") == "1" {
		return true
	}

	u, err := url.Parse(baseURL)
	if err != nil {
		return false
	}
	host := strings.ToLower(u.Hostname())

	// Dev defaults: local/self-signed certs are expected.
	return host == "localhost" || host == "127.0.0.1" || host == "::1" || strings.HasSuffix(host, ".local")
}

func integrationHTTPClient(baseURL string) *http.Client {
	skipTLS := shouldSkipTLSVerify(baseURL)
	tr := &http.Transport{}
	if skipTLS {
		tr.TLSClientConfig = &tls.Config{InsecureSkipVerify: true} //nolint:gosec
	}

	return &http.Client{
		Transport: tr,
		// Keep redirects disabled so we can assert 302/Location exactly.
		CheckRedirect: func(_ *http.Request, _ []*http.Request) error {
			return http.ErrUseLastResponse
		},
		Timeout: 10 * time.Second,
	}
}

func TestIntegration_DeviceLoginRedirect(t *testing.T) {
	baseURL := requireIntegration(t)
	client := integrationHTTPClient(baseURL)

	url := baseURL + "/device-login?code=ABCD-1234"
	resp, err := client.Get(url)
	if err != nil {
		t.Fatalf("GET %s failed: %v", url, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusFound {
		t.Fatalf("status=%d, want=%d", resp.StatusCode, http.StatusFound)
	}

	wantLocation := "https://console.wantastic.app/activate?user_code=ABCD-1234"
	if got := resp.Header.Get("Location"); got != wantLocation {
		t.Fatalf("Location=%q, want=%q", got, wantLocation)
	}
}

func TestIntegration_DeviceLoginFormPage(t *testing.T) {
	baseURL := requireIntegration(t)
	client := integrationHTTPClient(baseURL)

	url := baseURL + "/device-login"
	resp, err := client.Get(url)
	if err != nil {
		t.Fatalf("GET %s failed: %v", url, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status=%d, want=%d", resp.StatusCode, http.StatusOK)
	}
	if ct := resp.Header.Get("Content-Type"); !strings.Contains(ct, "text/html") {
		t.Fatalf("Content-Type=%q, want text/html", ct)
	}
}

func TestIntegration_AgentCredentialsEndpoint(t *testing.T) {
	baseURL := requireIntegration(t)
	client := integrationHTTPClient(baseURL)

	deviceID := "integration-dev-device-001"
	ts := strconv.FormatInt(time.Now().Unix(), 10)
	mac := hmac.New(sha256.New, []byte(cipher.SharedSecret))
	mac.Write([]byte(fmt.Sprintf("%s:%s", ts, deviceID)))
	sig := hex.EncodeToString(mac.Sum(nil))

	req, err := http.NewRequest(http.MethodGet, baseURL+"/api/agent/credentials", nil)
	if err != nil {
		t.Fatalf("new request failed: %v", err)
	}
	req.Header.Set(cipher.HeaderTimestamp, ts)
	req.Header.Set(cipher.HeaderDevice, deviceID)
	req.Header.Set(cipher.HeaderSignature, sig)

	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status=%d, want=%d", resp.StatusCode, http.StatusOK)
	}

	var payload map[string]string
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		t.Fatalf("decode json failed: %v", err)
	}
	if got, want := payload["auth0_domain"], wantasticAuth0Domain; got != want {
		t.Fatalf("auth0_domain=%q, want=%q", got, want)
	}
	if got, want := payload["auth0_client_id"], wantasticAuth0ClientID; got != want {
		t.Fatalf("auth0_client_id=%q, want=%q", got, want)
	}
}

// TestIntegration_WantasticGRPCStartDeviceFlow was removed after gRPC was
// ripped out of the core. The Auth device flow is now exercised via HTTP
// (see TestFullDeviceFlow_ApproveViaSPA in oauth2_integration_test.go).
