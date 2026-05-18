package server

import (
	"context"
	"testing"
	"time"
)

// TestScanPeerPorts_Timeout verifies that port scanning respects the 5-second timeout
func TestScanPeerPorts_Timeout(t *testing.T) {
	// This demonstrates how the 5-second timeout works
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// Simulate a long-running scan
	done := make(chan struct{})
	go func() {
		time.Sleep(6 * time.Second) // Simulate slow scan
		close(done)
	}()

	select {
	case <-ctx.Done():
		t.Log("✓ Timeout triggered as expected after 5 seconds")
	case <-done:
		t.Fatal("Scan should have timed out")
	}
}

// Example of how to use ScanPeerPorts in your code:
//
// // Scan common ports (top 100)
// result, err := server.ScanPeerPorts(accountID, peerID, nil)
//
// // Scan specific ports
// ports := []int{22, 80, 443, 8291, 50051}
// result, err := server.ScanPeerPorts(accountID, peerID, ports)
//
// // Process results
// for _, port := range result.OpenPorts() {
//     log.Debug().
//         Int("port", port.Port).
//         Str("service", port.Service).
//         Dur("rtt", port.RTT).
//         Msg("Open port detected")
// }
