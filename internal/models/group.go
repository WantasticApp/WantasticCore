package models

import "time"

// PeerGroup
type PeerGroup struct {
	ID          string
	AccountID   string
	Name        string
	Description string
	Protocols   []uint8
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

// GroupLink
type GroupLink struct {
	ID         string
	AccountID  string
	SrcGroupID string
	DstGroupID string
	Action     string // "allow" or "deny"
	Protocols  []uint8
	// PortRanges restrict destination ports for TCP/UDP. Start/End inclusive.
	PortRanges []PortRange
	CreatedAt  time.Time
	UpdatedAt  time.Time
}

// PortRange represents an inclusive TCP/UDP destination port range.
type PortRange struct {
	Start uint32 `json:"start"`
	End   uint32 `json:"end"`
}
