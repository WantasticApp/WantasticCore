// Package userspace provides resource allocation utilities for multi-tenant WireGuard.
package userspace

import (
	"fmt"
	"sync"
)

// TUNAllocator manages TUN device name allocation.
type TUNAllocator struct {
	used map[string]bool
	mu   sync.Mutex
}

// NewTUNAllocator creates a new TUN device allocator.
func NewTUNAllocator() *TUNAllocator {
	return &TUNAllocator{
		used: make(map[string]bool),
	}
}

// Allocate reserves a TUN device name for a tenant.
func (t *TUNAllocator) Allocate(tenantID string) (string, error) {
	t.mu.Lock()
	defer t.mu.Unlock()

	// Use first 8 chars of tenant ID for TUN name
	tunName := fmt.Sprintf("tun_%s", tenantID[:min(8, len(tenantID))])

	if t.used[tunName] {
		return "", fmt.Errorf("TUN device %s already in use", tunName)
	}

	t.used[tunName] = true
	return tunName, nil
}

// Release frees a TUN device name.
func (t *TUNAllocator) Release(tunName string) {
	t.mu.Lock()
	defer t.mu.Unlock()
	delete(t.used, tunName)
}

// IsAllocated checks if a TUN device name is in use.
func (t *TUNAllocator) IsAllocated(tunName string) bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.used[tunName]
}
