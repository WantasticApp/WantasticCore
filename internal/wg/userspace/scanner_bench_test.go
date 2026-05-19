package userspace

import (
	"context"
	"fmt"
	"net"
	"testing"
	"time"
)

// Wrapper for standard net dialer to satisfy NetStack interface
type HostNetStack struct {
	d net.Dialer
}

func (h *HostNetStack) DialContext(ctx context.Context, network, address string) (net.Conn, error) {
	return h.d.DialContext(ctx, network, address)
}

// M2 helper for mock (stripped down from protocol.go)
func writeMockM2(conn net.Conn, msg []byte) {
	// Length (assuming < 255 for mock)
	conn.Write([]byte{byte(len(msg))})
	conn.Write(msg)
}

func startMockListener(port int, banner string) (func(), error) {
	l, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", port))
	if err != nil {
		return nil, err
	}

	go func() {
		for {
			conn, err := l.Accept()
			if err != nil {
				return // listener closed
			}
			go func(c net.Conn) {
				defer c.Close()

				// Special handling for Winbox mock
				if port == 8291 {
					// Expect Client Hello (M2)
					buf := make([]byte, 1024)
					c.SetReadDeadline(time.Now().Add(1 * time.Second))
					_, err := c.Read(buf)
					if err == nil {
						// Send Server Hello (Handler 0x06 + data)
						// Just send enough to pass ecsrp.ReadM2Message
						// Length: 2 (Handler + 1 byte data)
						// Payload: 0x06, 0x00
						writeMockM2(c, []byte{0x06, 0x00})
					}
					return
				}

				if banner != "" {
					c.SetWriteDeadline(time.Now().Add(1 * time.Second))
					c.Write([]byte(banner))
				}
			}(conn)
		}
	}()

	return func() { l.Close() }, nil
}

func TestPortScanner_Accuracy(t *testing.T) {
	// Setup mock listeners
	// Port 21 (FTP) - mimic non-Winbox
	// Port 8291 (Winbox) - mimic Winbox (binary protocol, but scanner might check port or banner)
	// Port 8080 (HTTP)

	// We use high ports to avoid permission issues, mapping them conceptually
	ports := []int{2121, 8291, 8080}

	// Closers
	var closers []func()
	defer func() {
		for _, c := range closers {
			c()
		}
	}()

	// 1. Mock FTP (on 2121 to avoid sudo)
	c1, err := startMockListener(2121, "220 FTP Server Ready\r\n")
	if err != nil {
		t.Fatalf("failed to start mock FTP: %v", err)
	}
	closers = append(closers, c1)

	// 2. Mock Winbox (8291)
	// Real Winbox doesn't send banner on connect, but accepts data.
	c2, err := startMockListener(8291, "")
	if err != nil {
		t.Fatalf("failed to start mock Winbox: %v", err)
	}
	closers = append(closers, c2)

	// 3. Mock HTTP
	c3, err := startMockListener(8080, "HTTP/1.1 200 OK\r\nServer: Mock\r\n\r\n")
	if err != nil {
		t.Fatalf("failed to start mock HTTP: %v", err)
	}
	closers = append(closers, c3)

	// Create Scanner using Host NetStack
	hostNet := &HostNetStack{}
	scanner := NewPortScannerWithNet(hostNet, 10, 500*time.Millisecond) // 10 workers, 500ms timeout

	// Scan
	// Note: We scan 127.0.0.1. The user mentioned 192.168.1.102 but we can't mock that easily without aliases.
	// We stick to localhost for unit test reliability.
	result := scanner.ScanPorts("127.0.0.1", ports)

	// Verify
	if len(result.Ports) != 3 {
		t.Errorf("expected 3 open ports, got %d", len(result.Ports))
	}

	foundMap := make(map[int]*PortResult)
	for _, p := range result.Ports {
		foundMap[p.Port] = p
		t.Logf("Found Port %d: Service=%s Banner=%q", p.Port, p.Service, p.Banner)
	}

	if _, ok := foundMap[2121]; !ok {
		t.Error("Port 2121 (FTP) not found")
	}
	if _, ok := foundMap[8291]; !ok {
		t.Error("Port 8291 (Winbox) not found")
	}
}

func BenchmarkPortScanner_Scan(b *testing.B) {
	// Setup a few listeners to give the scanner something to do
	mockPorts := []int{8081, 8082, 8083}
	var closers []func()
	defer func() {
		for _, c := range closers {
			c()
		}
	}()

	for _, p := range mockPorts {
		c, err := startMockListener(p, "BENCHMARK\r\n")
		if err != nil {
			b.Fatalf("failed to listen: %v", err)
		}
		closers = append(closers, c)
	}

	// Define scan target ports (mix of open and closed)
	// 3 open, 7 closed = 10 ports total per op
	scanPorts := []int{8081, 8082, 8083, 9001, 9002, 9003, 9004, 9005, 9006, 9007}

	hostNet := &HostNetStack{}
	scanner := NewPortScannerWithNet(hostNet, 50, 200*time.Millisecond)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		scanner.ScanPorts("127.0.0.1", scanPorts)
	}
}

func TestFullScan_Performance(t *testing.T) {
	// Scan ports 20-10000
	var ports []int
	for p := 20; p <= 65535; p++ {
		ports = append(ports, p)
	}

	t.Logf("Starting full scan of %d ports on [IP_ADDRESS] (Rate limit ~1000pps)...", len(ports))

	hostNet := &HostNetStack{}
	// Use 500 workers for concurrency
	scanner := NewPortScannerWithNet(hostNet, 50, 200*time.Millisecond)

	start := time.Now()
	result := scanner.ScanPorts("192.168.1.102", ports)
	duration := time.Since(start)
	openCount := 0
	for _, p := range result.Ports {
		if p.State == "open" {
			openCount++
			t.Logf(" - %d/%s: %s (%s)", p.Port, p.Protocol, p.Service, p.Banner)
		}
	}
	t.Logf("Scan completed in %v", duration)
	t.Logf("Scan rate: %.2f ports/sec", float64(len(ports))/duration.Seconds())
	t.Logf("Found %d open ports (scanned %d total)", openCount, len(result.Ports))
}
