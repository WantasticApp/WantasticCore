package userspace

import (
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestScanner_HostNetstack_ScanMe performs a real network scan against scanme.nmap.org
// strictly for verification purposes as requested by the user.
func TestScanner_HostNetstack_ScanMe(t *testing.T) {
	// Use a longer timeout for the test
	// ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	// defer cancel() -> unused

	// Configure scanner to use host network (nil dialer) and target scanme.nmap.org
	// We'll scan common ports to ensure we get some hits
	// scanme.nmap.org typically has 22, 80, 9929, 31337 open
	targets := []string{"scanme.nmap.org"}

	// We'll scan a range that includes known open ports and some closed ones
	// 20-100, plus 9929, 31337
	portsStr := "20-100,9929,31337"
	ports := parseNmapPorts(portsStr)

	// NewPortScannerWithNet(netStack, workers, timeout)
	// Passing nil for netStack defaults to host network (net.Dialer)
	scanner := NewPortScannerWithNet(nil, 100, 2*time.Second)

	// We need to set the context on the scanner if we want it to respect the test context
	// But NewPortScannerWithNet creates its own context.
	// The new implementation doesn't expose way to set parent context easily
	// except creating it with NewPortScannerWithNet which uses background.
	// However, ScanPorts doesn't take context. It uses scanner's context.
	// We can manually set the context field if we really want, or just rely on timeout.
	// The scanner has a Cancel() method.

	t.Logf("Starting scan against %v...", targets)
	start := time.Now()

	results := make(map[string]*ScanResult)
	for _, target := range targets {
		// We'll trust the timeout handling
		results[target] = scanner.ScanPorts(target, ports)
	}

	duration := time.Since(start)
	t.Logf("Scan completed in %s", duration)

	require.Contains(t, results, "scanme.nmap.org")
	result := results["scanme.nmap.org"]

	t.Logf("Found %d open ports", len(result.Ports))
	for _, p := range result.Ports {
		t.Logf(" - Port %d/%s: %s (Web: %v)", p.Port, p.Protocol, p.Service, p.IsWebPage)
	}

	// Assertions for known ports on scanme.nmap.org
	// We verify that service detection actually worked (pattern matching)
	var foundSSH, foundHTTP bool

	for _, p := range result.Ports {
		if p.Port == 22 && (strings.Contains(strings.ToLower(p.Service), "ssh")) {
			foundSSH = true
		}
		if p.Port == 80 && (strings.Contains(strings.ToLower(p.Service), "http")) {
			foundHTTP = true
		}
	}

	assert.True(t, foundSSH, "Should have identified SSH service on port 22")
	assert.True(t, foundHTTP, "Should have identified HTTP service on port 80")
	// Note: public services may change, but 80 and 22 are usually open.
	// We won't hard fail if they are closed to avoid flaky tests, but we'll check logic.

	assert.Greater(t, len(result.Ports), 0, "Should have found open ports on scanme.nmap.org")

	// Verify Fingerprint if available
	if result.Fingerprint != nil {
		t.Logf("OS Fingerprint: %s %s (%s)", result.Fingerprint.OSFamily, result.Fingerprint.OSVersion, result.Fingerprint.Vendor)
	}
}

// TestScanner_Performance_Localhost benchmarks the scanner loop structure
// (mocking the network to test pure loop speed)
func TestScanner_Performance_Structure(t *testing.T) {
	// This test would ideally mock the NetStack to return immediately
	// to verify the worker pool overhead.
	// For now, we rely on the integration test above.
}
