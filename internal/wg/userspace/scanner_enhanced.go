package userspace

import (
	"context"
	"fmt"
	"net"
	"sync"
	"sync/atomic"
	"time"
)

// NetStack interface matches *netstack.Net from wireguard-go
type NetStack interface {
	DialContext(ctx context.Context, network, address string) (net.Conn, error)
}

// ScannerConfig holds internal configuration for PortScanner
type ScannerConfig struct {
	Host           string
	Ports          []int
	Workers        int
	Timeout        time.Duration
	ServiceDB      NmapServiceDBMap
	ProtocolDB     NmapProtocolDBMap
	ServiceProbeDB *NmapServiceProbeDB

	// Custom Dialer for userspace networking
	Dialer NetStack

	// Context for cancellation
	Ctx    context.Context
	Cancel context.CancelFunc

	// Callback
	OnProgress func(progress int, currentPort int, found bool)
}

// PortScanner performs async port scanning using worker pools
// Replaces the old implementation in probing.go
type PortScanner struct {
	config     ScannerConfig
	paused     atomic.Bool
	OnProgress func(progress int, currentPort int, found bool)
}

// NewPortScanner creates a scanner using TenantDevice network stack
// Moved from probing.go
func (td *TenantDevice) NewPortScanner(workers int, timeout time.Duration, onProgress func(progress int, currentPort int, found bool)) *PortScanner {
	ctx, cancel := context.WithCancel(context.Background())
	return &PortScanner{
		config: ScannerConfig{
			Workers:        workers,
			Timeout:        timeout,
			Dialer:         td.Net,
			Ctx:            ctx,
			Cancel:         cancel,
			ServiceDB:      GetGlobalNmapServiceDB(),
			ProtocolDB:     GetGlobalNmapProtocolDB(),
			ServiceProbeDB: GetGlobalNmapProbes(),
		},
		OnProgress: onProgress,
	}
}

// DiscoverSSHPort quickly scans ports 22-8000 to find an SSH service.
// Uses fast TCP connect + 4-byte banner read ("SSH-" prefix) with high concurrency.
// Returns the first SSH port found (prioritizing port 22), or 0 if none found.
func (td *TenantDevice) DiscoverSSHPort(ctx context.Context, host string) int {
	timeout := 2 * time.Second
	workers := 20

	// Build port list: check port 22 first, then common alternates, then the rest
	priorityPorts := []int{22, 222, 2222, 2200, 8022, 830, 322, 422, 522, 622, 722, 822, 922}
	seen := make(map[int]bool, 8000)
	ports := make([]int, 0, 8000)
	for _, p := range priorityPorts {
		ports = append(ports, p)
		seen[p] = true
	}
	for p := 23; p <= 8000; p++ {
		if !seen[p] {
			ports = append(ports, p)
		}
	}

	type sshResult struct {
		port int
	}
	found := make(chan sshResult, 1)
	scanCtx, scanCancel := context.WithCancel(ctx)
	defer scanCancel()

	sem := make(chan struct{}, workers)
	var wg sync.WaitGroup

	for _, port := range ports {
		select {
		case <-scanCtx.Done():
			goto wait
		default:
		}

		sem <- struct{}{}
		wg.Add(1)
		go func(p int) {
			defer wg.Done()
			defer func() { <-sem }()

			select {
			case <-scanCtx.Done():
				return
			default:
			}

			connCtx, cancel := context.WithTimeout(scanCtx, timeout)
			defer cancel()

			conn, err := td.Net.DialContext(connCtx, "tcp", fmt.Sprintf("%s:%d", host, p))
			if err != nil {
				return
			}
			defer conn.Close()

			// Read first 4 bytes — SSH servers send "SSH-" immediately
			conn.SetReadDeadline(time.Now().Add(timeout))
			buf := make([]byte, 4)
			n, err := conn.Read(buf)
			if err != nil || n < 4 {
				return
			}

			if string(buf[:4]) == "SSH-" {
				select {
				case found <- sshResult{port: p}:
					scanCancel() // Stop other goroutines
				default:
				}
			}
		}(port)
	}

wait:
	// Wait for all goroutines in a separate goroutine so we can check found
	go func() {
		wg.Wait()
		close(found)
	}()

	// Return first SSH port found (or 0)
	if r, ok := <-found; ok {
		return r.port
	}
	return 0
}

// NewPortScannerWithNet creates a scanner with any NetStack implementation
// Moved from probing.go
func NewPortScannerWithNet(netStack NetStack, workers int, timeout time.Duration) *PortScanner {
	ctx, cancel := context.WithCancel(context.Background())
	return &PortScanner{
		config: ScannerConfig{
			Workers:        workers,
			Timeout:        timeout,
			Dialer:         netStack,
			Ctx:            ctx,
			Cancel:         cancel,
			ServiceDB:      GetGlobalNmapServiceDB(),
			ProtocolDB:     GetGlobalNmapProtocolDB(),
			ServiceProbeDB: GetGlobalNmapProbes(),
		},
	}
}

// Stop stops the scan consistently and returns collected results so far
func (ps *PortScanner) Stop() {
	ps.Cancel()
}

// Cancel stops the scan immediately
func (ps *PortScanner) Cancel() {
	if ps.config.Cancel != nil {
		ps.config.Cancel()
	}
}

// SetPaused pauses or checks pause state of the scan
func (ps *PortScanner) SetPaused(paused bool) {
	ps.paused.Store(paused)
}

// IsCancelled reports whether the scanner context has been cancelled.
func (ps *PortScanner) IsCancelled() bool {
	return ps.config.Ctx != nil && ps.config.Ctx.Err() != nil
}

// ScanPorts scans TCP (and potentially UDP if implemented) ports asynchronously
func (ps *PortScanner) ScanPorts(host string, ports []int) *ScanResult {
	result := &ScanResult{
		Host:      host,
		StartTime: time.Now(),
		Ports:     make([]*PortResult, 0, len(ports)),
	}

	// Channel to receive results
	resultsChan := make(chan *PortResult, ps.config.Workers)

	// Updates config for this run
	ps.config.Host = host
	ps.config.Ports = ports

	var wg sync.WaitGroup
	semaphore := make(chan struct{}, ps.config.Workers)

	currentOp := 0
	var progressMu sync.Mutex

	// Collector
	var collectorWg sync.WaitGroup
	collectorWg.Add(1)
	go func() {
		defer collectorWg.Done()
		for res := range resultsChan {
			result.Ports = append(result.Ports, res)
		}
	}()

	// Scanner loop
	for _, port := range ports {
		// Check context
		select {
		case <-ps.config.Ctx.Done():
			goto done
		default:
		}

		// Check paused
		for ps.paused.Load() {
			select {
			case <-ps.config.Ctx.Done():
				goto done
			case <-time.After(500 * time.Millisecond):
			}
		}

		// Rate limiting/Semaphore
		semaphore <- struct{}{}
		wg.Add(1)

		go func(p int) {
			defer wg.Done()
			defer func() { <-semaphore }()

			if ps.OnProgress != nil {
				progressMu.Lock()
				currentOp++
				// Pass raw count (currentOp) instead of percentage
				// server.go will calculate percentage based on total it knows
				ps.OnProgress(currentOp, p, false)
				progressMu.Unlock()
			}

			// Perform Scan
			// 1. TCP Connect
			pr := ps.scanPortTCP(ps.config.Ctx, host, p)
			if pr != nil {
				resultsChan <- pr
				if ps.OnProgress != nil {
					// For found ports, we also want to report.
					// We just pass the current count we have.
					progressMu.Lock()
					cnt := currentOp
					progressMu.Unlock()
					ps.OnProgress(cnt, p, true)
				}
			}
		}(port)
	}

done:
	wg.Wait()
	close(resultsChan)
	collectorWg.Wait()

	result.EndTime = time.Now()

	// Analyze Fingerprint
	AnalyzeFingerprint(result)

	return result
}

func (ps *PortScanner) scanPortTCP(ctx context.Context, host string, port int) *PortResult {
	address := fmt.Sprintf("%s:%d", host, port)
	connCtx, cancel := context.WithTimeout(ctx, ps.config.Timeout)
	defer cancel()

	start := time.Now()
	var conn net.Conn
	var err error

	if ps.config.Dialer != nil {
		conn, err = ps.config.Dialer.DialContext(connCtx, "tcp", address)
	} else {
		d := net.Dialer{}
		conn, err = d.DialContext(connCtx, "tcp", address)
	}

	rtt := time.Since(start)

	if err != nil {
		// Filtered or closed. We typically don't return them in basic view unless requested.
		// probing.go implementation returned nothing for filtered/closed.
		return nil
	}
	defer conn.Close()

	// Port is OPEN
	res := &PortResult{
		Port:     port,
		Protocol: "tcp",
		State:    "open",
		RTT:      rtt,
	}

	// Banner Gabbing & Service Detection
	conn.SetDeadline(time.Now().Add(ps.config.Timeout))

	// Check for Winbox specifically (port 8291)
	if port == 8291 {
		// We removed ecsrp.Probe dependency as requested.
		// We will rely on generic banner detection or Nmap probes.
		// If needed, we can send the Winbox magic bytes manually here.
		// Magic: M2 + ...
		conn.SetDeadline(time.Now().Add(ps.config.Timeout))
	}

	// Send generic probe / Read banner
	// Try reading first (many services send banner)
	buf := make([]byte, 1024)
	n, err := conn.Read(buf)

	var banner []byte
	if err == nil && n > 0 {
		banner = buf[:n]
		res.Banner = string(banner)
	} else {
		// If read failed/timeout, send a trigger?
		// Nmap logic: "NULL" probe first (read), then active probes.
		// For simplicity/parity with old scanner: check TLS
		if isTLS, _ := checkTLS(conn, ps.config.Timeout/2); isTLS {
			res.Service = "tcp/https"
			return res
		}
		// Try writing something? "\r\n\r\n"
		conn.Write([]byte("\r\n\r\n"))
		n, err = conn.Read(buf)
		if err == nil && n > 0 {
			banner = buf[:n]
			res.Banner = string(banner)
		}
	}

	// Service Detection using Nmap DB
	nmapInfo := ps.detectService(port, "tcp", banner)
	res.Service = nmapInfo.Service
	if nmapInfo.Product != "" {
		res.Service += " " + nmapInfo.String()
	}
	res.NmapInfo = nmapInfo
	res.IsWebPage = isWebPageService(res.Service, res.Banner)

	return res
}

// detectService uses Nmap DBs to identify service
func (ps *PortScanner) detectService(port int, protocol string, banner []byte) *NmapServiceInfo {
	// 0. Nmap Service Probes (Regex matching)
	if ps.config.ServiceProbeDB != nil && len(banner) > 0 {
		for _, probe := range ps.config.ServiceProbeDB.Probes {
			if probe.Name == "NULL" {
				for _, match := range probe.Matches {
					if match.Pattern.Match(banner) {
						submatches := match.Pattern.FindStringSubmatch(string(banner))
						info := match.ApplyVersionSubstitutions(submatches)
						// Trim trailing/leading spaces from service if needed,
						// though NmapServiceInfo separates them.
						return info
					}
				}
			}
		}
	}

	// 1. Nmap Service DB (Port based)
	if ps.config.ServiceDB != nil {
		key := fmt.Sprintf("%d/%s", port, protocol)
		if svc, ok := ps.config.ServiceDB[key]; ok {
			return &NmapServiceInfo{Service: svc}
		}
	}

	return &NmapServiceInfo{Service: "unknown"}
}

// ScanAllPortsWithUDP scans all 65535 TCP ports and common UDP ports
func (ps *PortScanner) ScanAllPortsWithUDP(host string) *ScanResult {
	// 1. TCP Scan (All ports)
	tcpPorts := make([]int, 0, 65535)
	for i := 1; i <= 65535; i++ {
		tcpPorts = append(tcpPorts, i)
	}

	// Calculate total operations for accurate progress in ScanAll
	// Total = 65535 TCP + 21 UDP = 65556
	// We need to coordinate the progress callback if we want 0-100% over the WHOLE scan.
	// However, ScanPorts and ScanUDPPorts both call OnProgress independently starting from 0.
	// We need to wrap the OnProgress to offset the count.

	originalOnProgress := ps.OnProgress
	var progressMu sync.Mutex
	var totalProgress int

	// Wrapper for TCP part (0 to 65535)
	if originalOnProgress != nil {
		ps.OnProgress = func(count int, port int, found bool) {
			progressMu.Lock()
			// Update total progress tracking (max seen so far)
			if count > totalProgress {
				totalProgress = count
			}
			progressMu.Unlock()
			originalOnProgress(count, port, found)
		}
	}

	// We need to be careful with progress reporting if we chain scans.
	// ScanPorts calls OnProgress.
	// Optionally we could disable OnProgress for the sub-scans and aggregate manually,
	// but for now letting them report is fine, though the "Total" might jump.
	// Given the user wants "ScanAll", the TCP part is dominant (65k vs ~20 UDP).

	result := ps.ScanPorts(host, tcpPorts)

	// restore mostly, but we need to offset for UDP
	// TCP finished, so totalProgress should be ~65535.
	tcpCount := 65535 // expected

	// Wrapper for UDP part (65536 to ...)
	if originalOnProgress != nil {
		ps.OnProgress = func(count int, port int, found bool) {
			// Offset by TCP count
			originalOnProgress(tcpCount+count, port, found)
		}
	}

	// 2. UDP Scan (Common ports)
	udpPorts := []int{
		53, 67, 68, 69, 123, 137, 138, 161, 162, 443, 500, 514, 520,
		631, 1194, 1701, 1900, 4500, 51820, 8291, 5678,
	}

	udpResult := ps.ScanUDPPorts(host, udpPorts)

	// Restore original callback
	ps.OnProgress = originalOnProgress

	// 3. Merge UDP results into TCP result
	result.Ports = append(result.Ports, udpResult.Ports...)

	// 4. Re-analyze Fingerprint with combined data
	AnalyzeFingerprint(result)

	return result
}

// ScanUDPPorts scans specified UDP ports
func (ps *PortScanner) ScanUDPPorts(host string, ports []int) *ScanResult {
	result := &ScanResult{
		Host:      host,
		StartTime: time.Now(),
		Ports:     make([]*PortResult, 0, len(ports)),
	}

	resultsChan := make(chan *PortResult, ps.config.Workers)
	var wg sync.WaitGroup

	semaphore := make(chan struct{}, ps.config.Workers)

	currentOp := 0
	var progressMu sync.Mutex

	// Collector
	var collectorWg sync.WaitGroup
	collectorWg.Add(1)
	go func() {
		defer collectorWg.Done()
		for res := range resultsChan {
			result.Ports = append(result.Ports, res)
		}
	}()

	for _, port := range ports {
		select {
		case <-ps.config.Ctx.Done():
			goto done
		default:
		}

		for ps.paused.Load() {
			time.Sleep(100 * time.Millisecond)
		}

		semaphore <- struct{}{}
		wg.Add(1)

		go func(p int) {
			defer wg.Done()
			defer func() { <-semaphore }()

			// Report progress
			if ps.OnProgress != nil {
				progressMu.Lock()
				currentOp++
				cnt := currentOp
				progressMu.Unlock()
				ps.OnProgress(cnt, p, false)
			}

			if pr := ps.scanPortUDP(ps.config.Ctx, host, p); pr != nil {
				resultsChan <- pr
				// Report found
				if ps.OnProgress != nil {
					progressMu.Lock()
					cnt := currentOp
					progressMu.Unlock()
					ps.OnProgress(cnt, p, true)
				}
			}
		}(port)
	}

done:
	wg.Wait()
	close(resultsChan)
	collectorWg.Wait()

	result.EndTime = time.Now()
	return result
}

// ScanCustomPorts scans specified ports using specified protocols
func (ps *PortScanner) ScanCustomPorts(host string, ports []int, scanTCP, scanUDP bool) *ScanResult {
	// If neither protocol specified, default to TCP
	if !scanTCP && !scanUDP {
		scanTCP = true
	}

	// 1. TCP Scan
	var result *ScanResult
	if scanTCP {
		result = ps.ScanPorts(host, ports)
	} else {
		result = &ScanResult{
			Host:      host,
			StartTime: time.Now(),
			Ports:     make([]*PortResult, 0),
		}
	}

	// 2. UDP Scan
	if scanUDP {
		// We need to coordinate progress if doing both
		// If TCP ran, result has some data.
		// Similar to ScanAllPortsWithUDP logic.

		originalOnProgress := ps.OnProgress
		tcpCount := 0
		if scanTCP {
			tcpCount = len(ports)
		}

		if originalOnProgress != nil {
			ps.OnProgress = func(count int, port int, found bool) {
				originalOnProgress(tcpCount+count, port, found)
			}
		}

		udpResult := ps.ScanUDPPorts(host, ports)

		ps.OnProgress = originalOnProgress

		// Merge
		result.Ports = append(result.Ports, udpResult.Ports...)
		result.EndTime = time.Now() // Update end time
	}

	if !scanTCP && !scanUDP {
		result.EndTime = time.Now()
	} else {
		AnalyzeFingerprint(result)
	}

	return result
}

func (ps *PortScanner) scanPortUDP(ctx context.Context, host string, port int) *PortResult {
	address := fmt.Sprintf("%s:%d", host, port)
	connCtx, cancel := context.WithTimeout(ctx, ps.config.Timeout)
	defer cancel()

	start := time.Now()
	var conn net.Conn
	var err error

	if ps.config.Dialer != nil {
		conn, err = ps.config.Dialer.DialContext(connCtx, "udp", address)
	} else {
		d := net.Dialer{}
		conn, err = d.DialContext(connCtx, "udp", address)
	}

	if err != nil {
		return nil
	}
	defer conn.Close()

	// UDP is connectionless. Dial success doesn't mean port is open.
	// We must send data and wait for response.

	probe := getUDPProbe(port)
	if len(probe) == 0 {
		probe = []byte{0x00}
	}

	conn.SetWriteDeadline(time.Now().Add(ps.config.Timeout))
	if _, err := conn.Write(probe); err != nil {
		return nil
	}

	buf := make([]byte, 2048)
	conn.SetReadDeadline(time.Now().Add(ps.config.Timeout))
	n, err := conn.Read(buf)

	rtt := time.Since(start)

	if err != nil {
		// Timeout usually means Open|Filtered in UDP, but we treat as closed/filtered
		// to avoid noise, unless we have specific logic.
		// For now, return nil (not open)
		return nil
	}

	if n > 0 {
		res := &PortResult{
			Port:     port,
			Protocol: "udp",
			State:    "open",
			RTT:      rtt,
			Banner:   string(buf[:n]),
			Service:  getUDPServiceByPort(port),
		}

		// Refine service detection
		if detected := detectUDPServiceFromResponse(port, buf[:n]); detected != "" {
			res.Service = detected
		}

		return res
	}

	return nil
}
