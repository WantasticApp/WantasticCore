package adminbot

import (
	"strings"
	"testing"
	"time"
)

func TestMemoryStoreUsesConfiguredTTL(t *testing.T) {
	store := NewMemoryStore(MemoryConfig{TTL: 5 * time.Millisecond})
	store.AddExchange("chat|sender", "hello", "world")

	time.Sleep(10 * time.Millisecond)
	store.evictStale()

	summary, turns := store.Snapshot("chat|sender")
	if summary != "" || len(turns) != 0 {
		t.Fatalf("expected stale memory to be evicted, got summary=%q turns=%d", summary, len(turns))
	}
}

func TestMemoryStoreRollingResetKeepsRecentTurns(t *testing.T) {
	store := NewMemoryStore(MemoryConfig{})
	sm := &senderMemory{
		Summary:    strings.Repeat("x", memoryMaxSummaryChars),
		TotalTurns: memoryHardResetAt,
		Turns: []ConversationTurn{
			{Role: "user", Content: "1"},
			{Role: "assistant", Content: "2"},
			{Role: "user", Content: "3"},
			{Role: "assistant", Content: "4"},
			{Role: "user", Content: "5"},
			{Role: "assistant", Content: "6"},
		},
	}

	store.rollMemoryLocked(sm)

	if sm.Summary != "" {
		t.Fatalf("expected summary to be cleared, got %q", sm.Summary)
	}
	if got := len(sm.Turns); got != memoryResetKeepTurns {
		t.Fatalf("expected %d recent turns to remain, got %d", memoryResetKeepTurns, got)
	}
	if sm.TotalTurns != memoryResetKeepTurns {
		t.Fatalf("expected total turns to reset to %d, got %d", memoryResetKeepTurns, sm.TotalTurns)
	}
	if sm.Turns[0].Content != "3" || sm.Turns[len(sm.Turns)-1].Content != "6" {
		t.Fatalf("expected to keep the freshest turns, got %#v", sm.Turns)
	}
}
