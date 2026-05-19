package server

import (
	"fmt"
	"sync"
	"testing"
	"time"
)

func TestPeerStatusRefreshLogic(t *testing.T) {
	guard := newPeerStatusGuard(25 * time.Second)

	peer := "AZBZ4rBGA2+Tr4dVfGLXaqbNNZZMWI7gv/Lc9mj8h0Q="
	hub := "hub-1"

	// First call: should need refresh
	if !guard.NeedsRefresh(peer, hub) {
		t.Fatal("first call should need refresh")
	}
	guard.MarkRefreshed(peer, hub)

	// Immediate second call: should NOT need refresh
	if guard.NeedsRefresh(peer, hub) {
		t.Fatal("immediate second call should not need refresh")
	}

	// Different hub: SHOULD need refresh (peer migrated)
	if !guard.NeedsRefresh(peer, "hub-2") {
		t.Fatal("different hub should need refresh")
	}

	// Different peer: SHOULD need refresh
	if !guard.NeedsRefresh("other-peer", hub) {
		t.Fatal("different peer should need refresh")
	}
}

func TestPeerStatusGuardConcurrency(t *testing.T) {
	guard := newPeerStatusGuard(25 * time.Second)

	var wg sync.WaitGroup
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			peer := fmt.Sprintf("peer-%d", id%10)
			hub := fmt.Sprintf("hub-%d", id%3)
			for j := 0; j < 100; j++ {
				if guard.NeedsRefresh(peer, hub) {
					guard.MarkRefreshed(peer, hub)
				}
			}
		}(i)
	}
	wg.Wait()
}

func TestPeerStatusGuardPrune(t *testing.T) {
	guard := newPeerStatusGuard(10 * time.Millisecond) // short for testing

	guard.MarkRefreshed("peer-1", "hub-1")
	guard.MarkRefreshed("peer-2", "hub-1")

	// Both should exist
	if guard.NeedsRefresh("peer-1", "hub-1") {
		t.Fatal("peer-1 should not need refresh yet")
	}

	// Wait for entries to expire
	time.Sleep(25 * time.Millisecond)
	guard.Prune()

	// Now both should need refresh (pruned)
	if !guard.NeedsRefresh("peer-1", "hub-1") {
		t.Fatal("peer-1 should need refresh after prune")
	}
}

func BenchmarkPeerStatusGuard(b *testing.B) {
	guard := newPeerStatusGuard(25 * time.Second)
	peer := "AZBZ4rBGA2+Tr4dVfGLXaqbNNZZMWI7gv/Lc9mj8h0Q="
	hub := "hub-1"
	guard.MarkRefreshed(peer, hub)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		guard.NeedsRefresh(peer, hub)
	}
}

func BenchmarkPeerStatusGuardManyPeers(b *testing.B) {
	guard := newPeerStatusGuard(25 * time.Second)
	for i := 0; i < 10000; i++ {
		guard.MarkRefreshed(fmt.Sprintf("peer-%d", i), "hub-1")
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		peer := fmt.Sprintf("peer-%d", i%10000)
		guard.NeedsRefresh(peer, "hub-1")
	}
}
