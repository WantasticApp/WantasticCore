package userspace

import (
	"sync"
	"time"

	"github.com/rs/zerolog/log"
)

// PortResult represents a single port scan result
type PortResult struct {
	Port      int
	Protocol  string // "tcp" or "udp"
	State     string // "open", "closed", "filtered", "open|filtered" (UDP)
	Service   string
	Banner    string
	RTT       time.Duration
	TTL       int              `json:"ttl,omitempty"` // IP TTL from response (for OS fingerprinting)
	IsWebPage bool             `json:"is_webpage"`    // True if this port serves HTML web content
	NmapInfo  *NmapServiceInfo `json:"nmap_info,omitempty"`
}

// OSFingerprint contains OS detection results derived from banner analysis
type OSFingerprint struct {
	OSFamily      string `json:"os_family"`      // linux, windows, routeros, ios, bsd, etc.
	OSVersion     string `json:"os_version"`     // e.g., "7.15", "10", "22.04"
	TTL           int    `json:"ttl,omitempty"`  // Initial TTL detected (64=Unix, 128=Windows, 255=Router)
	TTLGuess      string `json:"ttl_guess"`      // OS guess from TTL value
	Vendor        string `json:"vendor"`         // MikroTik, Cisco, Microsoft, Linux, etc.
	DeviceType    string `json:"device_type"`    // router, server, workstation, switch, ap
	Model         string `json:"model"`          // Device model if detected
	Hostname      string `json:"hostname"`       // Hostname if detected from banners or DNS
	MACAddress    string `json:"mac_address"`    // MAC address if available (for OUI vendor lookup)
	MACVendor     string `json:"mac_vendor"`     // Vendor derived from MAC address OUI
	Confidence    int    `json:"confidence"`     // 0-100 confidence score
	DetectionInfo string `json:"detection_info"` // How the OS was detected
}

// ScanResult contains all scan results for a host
type ScanResult struct {
	Host        string
	Hostname    string // Hostname if known (from DNS or other sources)
	MACAddress  string // MAC address if available (for vendor identification)
	StartTime   time.Time
	EndTime     time.Time
	Ports       []*PortResult
	Fingerprint *OSFingerprint `json:"fingerprint,omitempty"` // OS fingerprint from banner analysis
	mu          sync.RWMutex
}

// Stats returns scan statistics
func (sr *ScanResult) Stats() (open, closed, filtered int, duration time.Duration) {
	sr.mu.RLock()
	defer sr.mu.RUnlock()

	for _, p := range sr.Ports {
		switch p.State {
		case "open":
			open++
		case "closed":
			closed++
		case "filtered", "open|filtered":
			filtered++
		}
	}

	duration = sr.EndTime.Sub(sr.StartTime)
	return
}

// OpenPorts returns only definitively open ports (received a response).
// Does NOT include "filtered" or "open|filtered" states to avoid false positives.
func (sr *ScanResult) OpenPorts() []*PortResult {
	sr.mu.RLock()
	defer sr.mu.RUnlock()

	var open []*PortResult
	for _, p := range sr.Ports {
		if p.State == "open" {
			open = append(open, p)
		}
	}
	return open
}

// FilteredPorts returns ports that may be filtered (no response received).
// For UDP, this includes ports where we couldn't determine if open or closed.
func (sr *ScanResult) FilteredPorts() []*PortResult {
	sr.mu.RLock()
	defer sr.mu.RUnlock()

	var filtered []*PortResult
	for _, p := range sr.Ports {
		if p.State == "filtered" || p.State == "open|filtered" {
			filtered = append(filtered, p)
		}
	}
	return filtered
}

// Log logs the scan results
func (sr *ScanResult) Log() {
	open, closed, filtered, duration := sr.Stats()

	log.Debug().
		Str("host", sr.Host).
		Int("total", len(sr.Ports)).
		Int("open", open).
		Int("closed", closed).
		Int("filtered", filtered).
		Dur("duration", duration).
		Float64("rate", float64(len(sr.Ports))/duration.Seconds()).
		Msg("Port scan completed")

	// Log open ports
	for _, p := range sr.OpenPorts() {
		log.Debug().
			Str("host", sr.Host).
			Int("port", p.Port).
			Str("protocol", p.Protocol).
			Str("state", p.State).
			Str("service", p.Service).
			Dur("rtt", p.RTT).
			Msg("Open port found")
	}
}
