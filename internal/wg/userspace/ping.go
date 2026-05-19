package userspace

import (
	"context"
	"crypto/rand"
	"encoding/binary"
	"fmt"
	"net"
	"net/netip"
	"time"

	"github.com/rs/zerolog/log"
)

// PingDetail contains the result of a single ICMP ping
type PingDetail struct {
	Sequence  int       `json:"sequence"`  // Ping sequence number (1, 2, 3, ...)
	RTTMs     float64   `json:"rtt_ms"`    // Round-trip time in milliseconds
	Success   bool      `json:"success"`   // Whether this ping succeeded
	Error     string    `json:"error"`     // Error message if ping failed
	Timestamp time.Time `json:"timestamp"` // When this ping was sent
}

// PingResult contains the results of a ping operation
type PingResult struct {
	PeerIP            string
	PacketsSent       int
	PacketsReceived   int
	PacketLossPercent float64
	MinRTTMs          float64
	AvgRTTMs          float64
	MaxRTTMs          float64
	Success           bool
	Error             string
	Method            string       // "udp", "tcp", or "hybrid"
	Pings             []PingDetail // Detailed results for each individual ping
}

// pingProbe represents a single connectivity probe result
type pingProbe struct {
	success bool
	rtt     time.Duration
	method  string
	port    int
}

// Port selection strategy for ISP infrastructure optimization
// Prioritize ports commonly open in ISP/enterprise environments
var (
	// ISP-optimized TCP port priority: SSH(22), HTTP(80), HTTPS(443), HTTP-Alt(8080), SMTP(25), DNS(53)
	// These ports are most likely to be open in ISP customer networks
	ispTcpPriorityPorts = []int{22, 80, 443, 8080, 25, 53, 3389, 21, 23, 993, 995}

	// ISP-optimized UDP port priority: DNS(53), NTP(123), SNMP(161), DHCP(67), Syslog(514)
	// These are essential infrastructure services always available
	ispUdpPriorityPorts = []int{53, 123, 161, 67, 514, 162, 69, 5353, 1812, 1813}
)

// PingPeer performs optimized connectivity checks to a peer using hybrid UDP/TCP probing
// Production-optimized: minimal resource usage, intelligent protocol selection, fast fail-fast logic
func (td *TenantDevice) PingPeer(peerIP string, count int, timeoutMs int) (*PingResult, error) {
	if count <= 0 {
		count = 10 // Default to 10 pings for better statistical accuracy
	}
	if timeoutMs <= 0 {
		timeoutMs = 1000
	}

	// Validate IP address
	dstIP := net.ParseIP(peerIP)
	if dstIP == nil {
		return nil, fmt.Errorf("invalid IP address: %s", peerIP)
	}
	if dstIP.To4() == nil {
		return nil, fmt.Errorf("only IPv4 is supported: %s", peerIP)
	}

	log.Debug().
		Str("tenant_id", td.TenantID).
		Str("peer_ip", peerIP).
		Int("count", count).
		Int("timeout_ms", timeoutMs).
		Msg("Starting optimized connectivity check")

	// Use ICMP ping (now properly routed through netstack→TUN→WireGuard)
	result, err := td.ICMPPing(peerIP, count, timeoutMs)
	if err != nil {
		return nil, err
	}

	log.Debug().
		Str("tenant_id", td.TenantID).
		Str("peer_ip", peerIP).
		Str("method", result.Method).
		Int("packets_sent", result.PacketsSent).
		Int("packets_received", result.PacketsReceived).
		Float64("packet_loss", result.PacketLossPercent).
		Float64("avg_rtt_ms", result.AvgRTTMs).
		Bool("success", result.Success).
		Msg("Connectivity check completed")

	return result, nil
}

// hybridPingProbe uses intelligent UDP + TCP probing for optimal connectivity detection
// Strategy: Smart port selection from comprehensive utils.TcpPorts and utils.UdpPorts
// Alternates between UDP (fast) and TCP (reliable) using prioritized port indices
func (td *TenantDevice) hybridPingProbe(peerIP string, count int, timeoutMs int) *PingResult {
	result := &PingResult{
		PeerIP:      peerIP,
		PacketsSent: count,
		Method:      "hybrid",
	}

	timeout := time.Duration(timeoutMs) * time.Millisecond
	var totalRTT time.Duration
	var minRTT, maxRTT time.Duration
	packetsReceived := 0

	// Strategy: Alternate UDP and TCP probes for best coverage
	// UDP: Fast, stateless (good for quick checks)
	// TCP: Reliable, get RST even if port closed (confirms host reachable)
	// Uses comprehensive port lists from utils package with intelligent prioritization
	for i := range count {
		var probe pingProbe
		var port int

		// Alternate: UDP on even, TCP on odd iterations using direct ISP-optimized ports
		if i%2 == 0 {
			// UDP probe using ISP-optimized port list
			udpIdx := (i / 2) % len(ispUdpPriorityPorts)
			port = ispUdpPriorityPorts[udpIdx]
			probe = td.udpProbe(peerIP, port, timeout)
		} else {
			// TCP probe using ISP-optimized port list
			tcpIdx := (i / 2) % len(ispTcpPriorityPorts)
			port = ispTcpPriorityPorts[tcpIdx]
			probe = td.tcpProbe(peerIP, port, timeout)
		}

		if probe.success {
			packetsReceived++
			totalRTT += probe.rtt

			if minRTT == 0 || probe.rtt < minRTT {
				minRTT = probe.rtt
			}
			if probe.rtt > maxRTT {
				maxRTT = probe.rtt
			}

			log.Debug().
				Str("tenant_id", td.TenantID).
				Str("peer_ip", peerIP).
				Int("seq", i+1).
				Int("port", probe.port).
				Str("method", probe.method).
				Float64("rtt_ms", float64(probe.rtt.Microseconds())/1000.0).
				Msg("Probe successful")
		} else {
			log.Debug().
				Str("tenant_id", td.TenantID).
				Str("peer_ip", peerIP).
				Int("seq", i+1).
				Int("port", port).
				Str("method", probe.method).
				Msg("Probe failed")
		}

		// Small delay between probes to avoid overwhelming the peer
		if i < count-1 {
			time.Sleep(50 * time.Millisecond)
		}
	}

	// Calculate statistics
	result.PacketsReceived = packetsReceived
	result.PacketLossPercent = float64(count-packetsReceived) / float64(count) * 100

	if packetsReceived > 0 {
		avgRTT := totalRTT / time.Duration(packetsReceived)
		result.AvgRTTMs = float64(avgRTT.Microseconds()) / 1000.0
		result.MinRTTMs = float64(minRTT.Microseconds()) / 1000.0
		result.MaxRTTMs = float64(maxRTT.Microseconds()) / 1000.0
		result.Success = true
	} else {
		result.Success = false
		result.Error = "no replies received - peer unreachable"
	}

	return result
}

// udpProbe performs a fast UDP connectivity check
// UDP is stateless and fast - good for quick reachability tests
func (td *TenantDevice) udpProbe(ip string, port int, timeout time.Duration) pingProbe {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	startTime := time.Now()

	// Dial UDP - this doesn't actually send anything yet
	conn, err := td.Net.DialContext(ctx, "udp", fmt.Sprintf("%s:%d", ip, port))
	if err != nil {
		return pingProbe{success: false, method: "udp", port: port}
	}
	defer conn.Close()

	// Send a minimal UDP packet (just 1 byte)
	_, err = conn.Write([]byte{0x00})
	if err != nil {
		return pingProbe{success: false, method: "udp", port: port}
	}

	// Set read deadline for quick response
	conn.SetReadDeadline(time.Now().Add(timeout / 2))

	// Try to read response (might get ICMP port unreachable, which is good - means host is up)
	buf := make([]byte, 1)
	_, _ = conn.Read(buf) // Ignore return values - we're measuring timing

	rtt := time.Since(startTime)

	// For UDP, even errors can indicate the host is reachable
	// If we get a quick response (< 50% timeout), consider it successful
	if rtt < timeout/2 {
		return pingProbe{success: true, rtt: rtt, method: "udp", port: port}
	}

	return pingProbe{success: false, method: "udp", port: port}
}

// tcpProbe performs TCP SYN-based connectivity check
// TCP is reliable - we get RST even if port is closed (confirms host reachable)
func (td *TenantDevice) tcpProbe(ip string, port int, timeout time.Duration) pingProbe {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	startTime := time.Now()
	conn, err := td.Net.DialContext(ctx, "tcp", fmt.Sprintf("%s:%d", ip, port))
	rtt := time.Since(startTime)

	if err != nil {
		// Check if we got a quick RST (port closed but host reachable)
		// RST packets arrive very quickly (< 50% of timeout)
		if rtt < timeout/2 {
			// Quick failure = RST received = host is reachable
			return pingProbe{success: true, rtt: rtt, method: "tcp-rst", port: port}
		}
		// Slow failure = timeout = host unreachable
		return pingProbe{success: false, method: "tcp", port: port}
	}

	// Connection succeeded
	conn.Close()
	return pingProbe{success: true, rtt: rtt, method: "tcp-open", port: port}
}

// // FastPing performs a single quick connectivity check (used for health checks)
// // Returns true if peer is reachable, false otherwise
// // Uses the most universal ports from utils package: DNS (UDP 53) and HTTP (TCP 80)
// func (td *TenantDevice) FastPing(peerIP string) bool {
// 	// Try UDP DNS first (port 53 - universal, fast)
// 	if len(utils.UdpPorts) > 0 {
// 		probe := td.udpProbe(peerIP, utils.UdpPorts[0], 500*time.Millisecond) // DNS
// 		if probe.success {
// 			return true
// 		}
// 	}

// 	// Fallback to TCP HTTP (port 80 - very common)
// 	if len(utils.TcpPorts) >= 7 {
// 		probe := td.tcpProbe(peerIP, utils.TcpPorts[6], 500*time.Millisecond) // HTTP
// 		return probe.success
// 	}

//		return false
//	}
func (td *TenantDevice) ICMPPing(peerIP string, count int, timeoutMs int) (*PingResult, error) {
	// Raw ICMP implementation bypassing DialPing - construct ICMP packets manually
	// and send via raw IP socket to avoid netstack routing issues
	if count <= 0 {
		count = 10 // Default to 10 pings for better statistical accuracy and charting
	}
	if timeoutMs <= 0 {
		timeoutMs = 1000 // 1s default — tunneled ICMP through WireGuard needs margin for rekey events
	}

	// Validate IP address
	dstIP, err := netip.ParseAddr(peerIP)
	if err != nil || !dstIP.IsValid() {
		return nil, fmt.Errorf("invalid IP address: %s", peerIP)
	}
	if !dstIP.Is4() {
		return nil, fmt.Errorf("only IPv4 is supported: %s", peerIP)
	}

	log.Debug().
		Str("tenant_id", td.TenantID).
		Str("peer_ip", peerIP).
		Int("count", count).
		Int("timeout_ms", timeoutMs).
		Msg("Starting raw ICMP connectivity check")

	// IMPORTANT: Check if peer has an endpoint before pinging.
	// This avoids proactive WireGuard handshakes that fail with "no known endpoint" for offline peers.
	if !td.HasEndpoint(peerIP) {
		log.Debug().
			Str("tenant_id", td.TenantID).
			Str("peer_ip", peerIP).
			Msg(" Skipping ICMP ping: peer has no active endpoint (is offline)")
		return &PingResult{
			PeerIP:      peerIP,
			PacketsSent: count,
			Method:      "icmp",
			Success:     false,
			Error:       "peer has no active endpoint",
		}, nil
	}

	result := &PingResult{
		PeerIP:      peerIP,
		PacketsSent: count,
		Method:      "icmp",
		Pings:       make([]PingDetail, 0, count), // Pre-allocate slice for ping details
	}

	timeout := time.Duration(timeoutMs) * time.Millisecond
	var totalRTT time.Duration
	var minRTT, maxRTT time.Duration
	packetsReceived := 0

	// Generate a unique ICMP identifier for this ping session
	icmpID := uint16(time.Now().UnixNano() & 0xFFFF)

	// Pre-allocate buffer for replies (reuse across pings)
	replyBuf := make([]byte, 1500)

	// OPTIMIZATION: Create single connection for all pings instead of one per ping
	// This reduces connection overhead and improves latency accuracy
	conn, err := td.Net.DialContext(context.Background(), "ping", peerIP)
	if err != nil {
		return nil, fmt.Errorf("failed to create ping connection: %w", err)
	}
	defer conn.Close()

	// Perform ICMP pings using the single connection
	for i := 0; i < count; i++ {
		pingDetail := PingDetail{
			Sequence:  i + 1,
			Timestamp: time.Now(),
		}

		// Set deadline for this specific ping
		conn.SetDeadline(time.Now().Add(timeout))

		// Construct ICMP Echo Request packet
		// ICMP Header: Type(1) + Code(1) + Checksum(2) + ID(2) + Seq(2) = 8 bytes
		// Payload: Timestamp(8) + Data(48) = 56 bytes
		icmpPacket := makeRawICMPPacket(icmpID, uint16(i+1), 56)

		// Write the ICMP packet and immediately start timing
		// CRITICAL: time.Now() must be AFTER Write to measure actual network RTT
		_, err = conn.Write(icmpPacket)
		if err != nil {
			pingDetail.Success = false
			pingDetail.Error = fmt.Sprintf("send failed: %v", err)
			result.Pings = append(result.Pings, pingDetail)

			log.Debug().
				Err(err).
				Str("tenant_id", td.TenantID).
				Str("peer_ip", peerIP).
				Int("seq", i+1).
				Msg("Failed to send ICMP packet")
			continue
		}

		// Start timing AFTER packet is sent
		startTime := time.Now()

		// Read the ICMP reply
		n, err := conn.Read(replyBuf)
		rtt := time.Since(startTime)

		if err != nil {
			pingDetail.Success = false
			pingDetail.Error = fmt.Sprintf("timeout or read error: %v", err)
			result.Pings = append(result.Pings, pingDetail)

			log.Debug().
				Err(err).
				Str("tenant_id", td.TenantID).
				Str("peer_ip", peerIP).
				Int("seq", i+1).
				Dur("rtt", rtt).
				Msg("ICMP timeout or read error")
		} else if n > 0 {
			// Verify this is an ICMP Echo Reply
			if n >= 8 && replyBuf[0] == 0 { // Type 0 = Echo Reply
				packetsReceived++
				totalRTT += rtt

				if minRTT == 0 || rtt < minRTT {
					minRTT = rtt
				}
				if rtt > maxRTT {
					maxRTT = rtt
				}

				// Record successful ping
				pingDetail.Success = true
				pingDetail.RTTMs = float64(rtt.Microseconds()) / 1000.0
				result.Pings = append(result.Pings, pingDetail)

				log.Debug().
					Str("tenant_id", td.TenantID).
					Str("peer_ip", peerIP).
					Int("seq", i+1).
					Int("bytes", n).
					Float64("rtt_ms", pingDetail.RTTMs).
					Uint16("icmp_id", icmpID).
					Msg("ICMP Echo Reply received")
			} else {
				pingDetail.Success = false
				pingDetail.Error = "invalid ICMP reply"
				result.Pings = append(result.Pings, pingDetail)
			}
		}

		// Minimal delay between pings — just enough to avoid ICMP rate limiting
		if i < count-1 {
			time.Sleep(10 * time.Millisecond)
		}
	}

	// Calculate statistics
	result.PacketsReceived = packetsReceived
	result.PacketLossPercent = float64(count-packetsReceived) / float64(count) * 100

	if packetsReceived > 0 {
		avgRTT := totalRTT / time.Duration(packetsReceived)
		result.AvgRTTMs = float64(avgRTT.Microseconds()) / 1000.0
		result.MinRTTMs = float64(minRTT.Microseconds()) / 1000.0
		result.MaxRTTMs = float64(maxRTT.Microseconds()) / 1000.0
		result.Success = true
	} else {
		result.Success = false
		result.Error = "no ICMP replies received - peer unreachable or ICMP blocked"
	}

	log.Debug().
		Str("tenant_id", td.TenantID).
		Str("peer_ip", peerIP).
		Int("packets_sent", result.PacketsSent).
		Int("packets_received", result.PacketsReceived).
		Float64("packet_loss", result.PacketLossPercent).
		Float64("avg_rtt_ms", result.AvgRTTMs).
		Bool("success", result.Success).
		Msg("Raw ICMP connectivity check completed")

	return result, nil
}

// PingCallback receives each ping result as it arrives. Return an error to stop early.
type PingCallback func(detail PingDetail) error

// StreamICMPPing sends ICMP pings and calls onPing for each result in real-time.
// This enables streaming results to the portal without waiting for all pings.
func (td *TenantDevice) StreamICMPPing(peerIP string, count, timeoutMs int, onPing PingCallback) (*PingResult, error) {
	if count <= 0 {
		count = 10
	}
	if timeoutMs <= 0 {
		timeoutMs = 1000
	}

	dstIP, err := netip.ParseAddr(peerIP)
	if err != nil || !dstIP.IsValid() || !dstIP.Is4() {
		return nil, fmt.Errorf("invalid IPv4 address: %s", peerIP)
	}

	if !td.HasEndpoint(peerIP) {
		return &PingResult{
			PeerIP: peerIP, PacketsSent: count, Method: "icmp",
			Success: false, Error: "peer has no active endpoint",
		}, nil
	}

	conn, err := td.Net.DialContext(context.Background(), "ping", peerIP)
	if err != nil {
		return nil, fmt.Errorf("failed to create ping connection: %w", err)
	}
	defer conn.Close()

	result := &PingResult{
		PeerIP: peerIP, PacketsSent: count, Method: "icmp",
		Pings: make([]PingDetail, 0, count),
	}

	timeout := time.Duration(timeoutMs) * time.Millisecond
	var totalRTT, minRTT, maxRTT time.Duration
	packetsReceived := 0
	icmpID := uint16(time.Now().UnixNano() & 0xFFFF)
	replyBuf := make([]byte, 1500)

	for i := 0; i < count; i++ {
		detail := PingDetail{Sequence: i + 1, Timestamp: time.Now()}
		conn.SetDeadline(time.Now().Add(timeout))

		icmpPacket := makeRawICMPPacket(icmpID, uint16(i+1), 56)
		if _, err := conn.Write(icmpPacket); err != nil {
			detail.Success = false
			detail.Error = fmt.Sprintf("send failed: %v", err)
		} else {
			startTime := time.Now()
			n, err := conn.Read(replyBuf)
			rtt := time.Since(startTime)

			if err != nil {
				detail.Success = false
				detail.Error = "timeout"
			} else if n >= 8 && replyBuf[0] == 0 {
				packetsReceived++
				totalRTT += rtt
				if minRTT == 0 || rtt < minRTT { minRTT = rtt }
				if rtt > maxRTT { maxRTT = rtt }
				detail.Success = true
				detail.RTTMs = float64(rtt.Microseconds()) / 1000.0
			} else {
				detail.Success = false
				detail.Error = "invalid ICMP reply"
			}
		}

		result.Pings = append(result.Pings, detail)

		// Stream this result to caller
		if onPing != nil {
			if err := onPing(detail); err != nil {
				break // caller cancelled (e.g. gRPC stream closed)
			}
		}

		if i < count-1 {
			time.Sleep(10 * time.Millisecond)
		}
	}

	result.PacketsReceived = packetsReceived
	result.PacketLossPercent = float64(count-packetsReceived) / float64(count) * 100
	if packetsReceived > 0 {
		result.AvgRTTMs = float64((totalRTT / time.Duration(packetsReceived)).Microseconds()) / 1000.0
		result.MinRTTMs = float64(minRTT.Microseconds()) / 1000.0
		result.MaxRTTMs = float64(maxRTT.Microseconds()) / 1000.0
		result.Success = true
	} else {
		result.Error = "no ICMP replies received"
	}
	return result, nil
}

// makeRawICMPPacket constructs a complete ICMP Echo Request packet with checksum
// Returns the full ICMP packet ready to send via raw IP socket
func makeRawICMPPacket(identifier, sequence uint16, dataSize int) []byte {
	// ICMP Echo Request packet structure:
	// [0]     Type = 8 (Echo Request)
	// [1]     Code = 0
	// [2-3]   Checksum (calculated)
	// [4-5]   Identifier (uint16)
	// [6-7]   Sequence number (uint16)
	// [8+]    Data (timestamp + pattern)

	const icmpHeaderSize = 8
	totalSize := icmpHeaderSize + dataSize
	packet := make([]byte, totalSize)

	// ICMP Header
	packet[0] = 8 // Type: Echo Request
	packet[1] = 0 // Code: 0
	// packet[2-3] = Checksum (calculated later)

	// Identifier (2 bytes, big-endian)
	binary.BigEndian.PutUint16(packet[4:6], identifier)

	// Sequence number (2 bytes, big-endian)
	binary.BigEndian.PutUint16(packet[6:8], sequence)

	// Data payload: timestamp + pattern
	if dataSize >= 8 {
		// Timestamp (8 bytes) for RTT verification
		timestamp := time.Now().UnixNano()
		binary.BigEndian.PutUint64(packet[8:16], uint64(timestamp))

		// Fill remaining data with pattern
		for i := 16; i < len(packet); i++ {
			packet[i] = byte((i - 16) & 0xFF)
		}

		// Add some entropy
		if len(packet) > 24 {
			rand.Read(packet[16:24])
		}
	}

	// Calculate checksum over the entire ICMP packet
	checksum := calculateICMPChecksum(packet)
	binary.BigEndian.PutUint16(packet[2:4], checksum)

	return packet
}

// calculateICMPChecksum computes the Internet Checksum (RFC 1071) for ICMP packets
func calculateICMPChecksum(data []byte) uint16 {
	sum := uint32(0)

	// Sum all 16-bit words
	for i := 0; i < len(data)-1; i += 2 {
		sum += uint32(binary.BigEndian.Uint16(data[i : i+2]))
	}

	// Add the last byte if odd length
	if len(data)%2 == 1 {
		sum += uint32(data[len(data)-1]) << 8
	}

	// Add carry bits and fold to 16 bits
	for sum > 0xFFFF {
		sum = (sum & 0xFFFF) + (sum >> 16)
	}

	// One's complement
	return ^uint16(sum)
}

// makeICMPEchoPayload constructs a proper ICMP Echo Request payload
// Format matches standard ping utility:
// - Identifier (16 bits): Unique ID for this ping session
// - Sequence Number (16 bits): Increments with each ping
// - Data: Timestamp + random/pattern data
func makeICMPEchoPayload(identifier, sequence uint16, dataSize int) []byte {
	// ICMP Echo Request payload structure:
	// [0-1]   Identifier (uint16)
	// [2-3]   Sequence number (uint16)
	// [4-11]  Timestamp (int64) - for RTT verification
	// [12+]   Data pattern (remaining bytes)

	const headerSize = 12 // ID + Seq + Timestamp
	totalSize := headerSize + dataSize
	if totalSize < headerSize {
		totalSize = headerSize
	}

	payload := make([]byte, totalSize)

	// Write identifier (2 bytes, big-endian)
	binary.BigEndian.PutUint16(payload[0:2], identifier)

	// Write sequence number (2 bytes, big-endian)
	binary.BigEndian.PutUint16(payload[2:4], sequence)

	// Write timestamp (8 bytes, big-endian) - used for RTT verification
	timestamp := time.Now().UnixNano()
	binary.BigEndian.PutUint64(payload[4:12], uint64(timestamp))

	// Fill remaining data with pattern (like standard ping)
	// Use incrementing byte pattern: 0x00, 0x01, 0x02, ..., 0xFF, 0x00, ...
	for i := headerSize; i < len(payload); i++ {
		payload[i] = byte((i - headerSize) & 0xFF)
	}

	// Add some entropy to make packets unique
	if len(payload) > headerSize+8 {
		rand.Read(payload[headerSize : headerSize+8])
	}

	return payload
}
