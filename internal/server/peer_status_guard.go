package server

import (
	"sync"
	"time"
)

// peerStatusGuard prevents redundant Redis writes for peer online status.
// At 1M devices sending stats every 2s, without this guard the system would
// do 500K Redis pipeline executions per second. The guard reduces this to
// ~1 write per TTL/2 window per peer (~40K writes/sec for 1M devices).
//
// Thread-safe via sharded RWMutex for minimal contention at scale.
type peerStatusGuard struct {
	shards  [64]peerStatusShard
	minAge  time.Duration
}

type peerStatusShard struct {
	mu      sync.RWMutex
	entries map[string]peerStatusEntry
}

type peerStatusEntry struct {
	hubID       string
	refreshedAt time.Time
}

func newPeerStatusGuard(minAge time.Duration) *peerStatusGuard {
	g := &peerStatusGuard{minAge: minAge}
	for i := range g.shards {
		g.shards[i].entries = make(map[string]peerStatusEntry, 256)
	}
	return g
}

func (g *peerStatusGuard) shard(key string) *peerStatusShard {
	// FNV-1a inspired hash — fast, good distribution
	h := uint32(2166136261)
	for i := 0; i < len(key); i++ {
		h ^= uint32(key[i])
		h *= 16777619
	}
	return &g.shards[h&63]
}

// NeedsRefresh returns true if the peer's Redis keys should be refreshed.
// True when: first time seen, hub changed, or half the TTL has elapsed.
func (g *peerStatusGuard) NeedsRefresh(peerKey, hubID string) bool {
	s := g.shard(peerKey)
	s.mu.RLock()
	entry, ok := s.entries[peerKey]
	s.mu.RUnlock()

	if !ok {
		return true
	}
	if entry.hubID != hubID {
		return true
	}
	return time.Since(entry.refreshedAt) > g.minAge/2
}

// MarkRefreshed records that the peer's Redis keys were just written.
func (g *peerStatusGuard) MarkRefreshed(peerKey, hubID string) {
	s := g.shard(peerKey)
	s.mu.Lock()
	s.entries[peerKey] = peerStatusEntry{
		hubID:       hubID,
		refreshedAt: time.Now(),
	}
	s.mu.Unlock()
}

// Prune removes entries for peers that went offline (no refresh in 2x minAge).
func (g *peerStatusGuard) Prune() {
	cutoff := time.Now().Add(-2 * g.minAge)
	for i := range g.shards {
		s := &g.shards[i]
		s.mu.Lock()
		for k, v := range s.entries {
			if v.refreshedAt.Before(cutoff) {
				delete(s.entries, k)
			}
		}
		s.mu.Unlock()
	}
}
