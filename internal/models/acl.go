package models

import (
	"net"
	"time"
)

// ACLRule represents a firewall rule for peer-to-peer communication within a tenant
type ACLRule struct {
	ID          string    `json:"id"`
	AccountID   string    `json:"account_id"`
	Name        string    `json:"name"`
	Action      string    `json:"action"`               // "allow" or "deny"
	Protocol    string    `json:"protocol"`             // "tcp", "udp", "icmp", "all"
	SourceIPs   []string  `json:"source_ips"`           // Source peer IPs or "any"
	DestIPs     []string  `json:"dest_ips"`             // Destination peer IPs or "any"
	DestPorts   []int     `json:"dest_ports,omitempty"` // For TCP/UDP
	Priority    int       `json:"priority"`             // Lower number = higher priority
	Description string    `json:"description,omitempty"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`

	// High-level intent fields (for UI/API)
	SourcePeerIDs []string `json:"source_peer_ids,omitempty"`
	DestPeerIDs   []string `json:"dest_peer_ids,omitempty"`
	Services      []string `json:"services,omitempty"`

	// HOT PATH OPTIMIZATIONS (pre-parsed for zero-alloc packet checks)
	SrcIPsParsed []net.IP     `json:"-"` // Pre-parsed SourceIPs
	DstIPsParsed []net.IP     `json:"-"` // Pre-parsed DestIPs
	PortBitmap   *[8192]uint8 `json:"-"` // Port bitmap for O(1) port checks (65536 bits = 8192 bytes)
	HasAnySource bool         `json:"-"` // Fast path: "any" in SourceIPs
	HasAnyDest   bool         `json:"-"` // Fast path: "any" in DestIPs
}

// ACLCacheResult represents a cached access control decision
type ACLCacheResult struct {
	Allowed  bool   `json:"allowed"`
	RuleID   string `json:"rule_id"`
	CachedAt int64  `json:"cached_at"` // Unix timestamp for cache entry age tracking
}
