package adminbot

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"
)

// Memory tuning constants.
const (
	// memorySummarizeAt — compress when the sliding window reaches this many turns.
	memorySummarizeAt = 16
	// memoryKeepAfterSum — retain this many recent turns after summarization.
	memoryKeepAfterSum = 6
	// defaultMemoryTTL — evict a chatter's memory after this much inactivity.
	defaultMemoryTTL = 2 * time.Hour
	// memoryCleanupInterval — how often the cleanup goroutine runs.
	memoryCleanupInterval = 20 * time.Minute
	// memoryMaxSenders — hard cap on tracked senders (evicts oldest when hit).
	memoryMaxSenders = 500
	// memoryHardResetAt — roll over very long chats to keep Claude context bounded.
	memoryHardResetAt = 64
	// memoryResetKeepTurns — keep the freshest context when a hard reset rolls over.
	memoryResetKeepTurns = 4
	// memoryMaxSummaryChars — guardrail for summary growth if the model over-answers.
	memoryMaxSummaryChars = 1800
	// memoryMaxContextChars — approximate max total context retained per chatter.
	memoryMaxContextChars = 12000
	// memoryMaxTurnChars — trim unusually large turns before storing them.
	memoryMaxTurnChars = 4000
)

// ConversationTurn is one half of a conversational exchange (user or assistant).
type ConversationTurn struct {
	Role    string // "user" | "assistant"
	Content string
	At      time.Time
}

// senderMemory holds the compressed context plus recent turns for one sender.
type senderMemory struct {
	// Summary is the compressed representation of all turns prior to the
	// sliding window.  It is rebuilt by Claude on every compression cycle.
	Summary     string
	Turns       []ConversationTurn // recent sliding window
	TotalTurns  int                // lifetime turn counter
	LastTouched time.Time
}

type MemoryConfig struct {
	TTL time.Duration
}

// MemoryStore manages per-sender conversation memories in process.
// It uses an Anchored Iterative Summarization strategy:
//   - Keep the last N turns verbatim (high recall for recent context).
//   - When the window overflows, ask Claude to compress the oldest turns into
//     a concise bullet summary, which is then injected as leading context on
//     every subsequent call.
//
// This technique reduces token usage by ~70 % per compression cycle while
// preserving key facts, IDs, and decisions across long sessions.
type MemoryStore struct {
	mu      sync.Mutex
	entries map[string]*senderMemory
	ttl     time.Duration
}

// NewMemoryStore creates an empty MemoryStore.
func NewMemoryStore(cfg MemoryConfig) *MemoryStore {
	ttl := cfg.TTL
	if ttl <= 0 {
		ttl = defaultMemoryTTL
	}
	return &MemoryStore{
		entries: make(map[string]*senderMemory),
		ttl:     ttl,
	}
}

// StartCleanup launches a background goroutine that evicts stale entries.
// It exits when ctx is cancelled.
func (m *MemoryStore) StartCleanup(ctx context.Context) {
	go func() {
		ticker := time.NewTicker(memoryCleanupInterval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				m.evictStale()
			}
		}
	}()
}

// evictStale removes all entries that have not been touched within memoryTTL.
func (m *MemoryStore) evictStale() {
	m.mu.Lock()
	defer m.mu.Unlock()
	now := time.Now()
	for k, v := range m.entries {
		if now.Sub(v.LastTouched) > m.ttl {
			delete(m.entries, k)
		}
	}
}

// evictOldestLocked removes the entry with the earliest LastTouched timestamp.
// Must be called with m.mu held.
func (m *MemoryStore) evictOldestLocked() {
	var oldestKey string
	var oldestTime time.Time
	for k, v := range m.entries {
		if oldestKey == "" || v.LastTouched.Before(oldestTime) {
			oldestKey = k
			oldestTime = v.LastTouched
		}
	}
	if oldestKey != "" {
		delete(m.entries, oldestKey)
	}
}

// touch retrieves (or creates) the senderMemory for senderID and updates
// its LastTouched timestamp.
func (m *MemoryStore) touch(senderID string) *senderMemory {
	m.mu.Lock()
	defer m.mu.Unlock()

	sm, ok := m.entries[senderID]
	if !ok {
		if len(m.entries) >= memoryMaxSenders {
			m.evictOldestLocked()
		}
		sm = &senderMemory{LastTouched: time.Now()}
		m.entries[senderID] = sm
	}
	sm.LastTouched = time.Now()
	return sm
}

// AddExchange appends a user turn and assistant turn atomically.
func (m *MemoryStore) AddExchange(senderID, question, answer string) {
	sm := m.touch(senderID)
	m.mu.Lock()
	defer m.mu.Unlock()
	now := time.Now()
	sm.Turns = append(sm.Turns,
		ConversationTurn{Role: "user", Content: trimTurnContent(question), At: now},
		ConversationTurn{Role: "assistant", Content: trimTurnContent(answer), At: now},
	)
	sm.TotalTurns += 2
	if totalContextChars(sm.Summary, sm.Turns) > memoryMaxContextChars*2 {
		m.rollMemoryLocked(sm)
	}
}

// NeedsSummarization returns true when the sliding window has grown large
// enough that a compression cycle should be triggered.
func (m *MemoryStore) NeedsSummarization(senderID string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	sm, ok := m.entries[senderID]
	if !ok {
		return false
	}
	return len(sm.Turns) >= memorySummarizeAt || totalContextChars(sm.Summary, sm.Turns) > memoryMaxContextChars
}

// Summarize compresses the oldest turns in the sliding window into a concise
// bullet-point summary using Claude, then trims those turns from the window.
// It is a best-effort operation: if Claude fails the window is left unchanged.
func (m *MemoryStore) Summarize(ctx context.Context, senderID string, claude *ClaudeClient) error {
	m.mu.Lock()
	sm, ok := m.entries[senderID]
	if !ok || len(sm.Turns) <= memoryKeepAfterSum {
		m.mu.Unlock()
		return nil
	}
	// Determine which turns to compress.
	cutoff := len(sm.Turns) - memoryKeepAfterSum
	toCompress := make([]ConversationTurn, cutoff)
	copy(toCompress, sm.Turns[:cutoff])
	existingSummary := sm.Summary
	m.mu.Unlock()

	// Build the prompt for Claude.
	var sb strings.Builder
	if existingSummary != "" {
		sb.WriteString("Existing summary of earlier context:\n")
		sb.WriteString(existingSummary)
		sb.WriteString("\n\n")
	}
	sb.WriteString("Conversation excerpt to compress:\n")
	for _, t := range toCompress {
		sb.WriteString(fmt.Sprintf("[%s]: %s\n", t.Role, t.Content))
	}

	const compressionSystem = "You are a conversation compressor. " +
		"Produce a very concise bullet-point summary (maximum 5 bullets, ~150 tokens total) " +
		"of the key facts, questions, and answers in the conversation. " +
		"Preserve specific names, IDs, numbers, and technical details verbatim. " +
		"Merge this excerpt with the existing summary if one is provided. " +
		"Output ONLY the bullet list — no preamble, no commentary."

	newSummary, err := claude.Ask(ctx, compressionSystem, sb.String())
	if err != nil {
		return fmt.Errorf("memory summarize: %w", err)
	}
	newSummary = trimSummary(newSummary)

	// Apply the result atomically.
	m.mu.Lock()
	defer m.mu.Unlock()
	sm, ok = m.entries[senderID]
	if !ok {
		return nil // evicted during compression; nothing to do
	}
	sm.Summary = newSummary
	if len(sm.Turns) > memoryKeepAfterSum {
		sm.Turns = sm.Turns[len(sm.Turns)-memoryKeepAfterSum:]
	}
	if sm.TotalTurns >= memoryHardResetAt || totalContextChars(sm.Summary, sm.Turns) > memoryMaxContextChars {
		m.rollMemoryLocked(sm)
	}
	return nil
}

// Snapshot returns a read-only copy of the current summary and turn history
// for the given sender.  Safe to call without holding the lock.
func (m *MemoryStore) Snapshot(senderID string) (summary string, turns []ConversationTurn) {
	m.mu.Lock()
	defer m.mu.Unlock()
	sm, ok := m.entries[senderID]
	if !ok {
		return "", nil
	}
	turnsCopy := make([]ConversationTurn, len(sm.Turns))
	copy(turnsCopy, sm.Turns)
	return sm.Summary, turnsCopy
}

// Reset clears the memory for a sender (e.g., on user request).
func (m *MemoryStore) Reset(senderID string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.entries, senderID)
}

func trimTurnContent(text string) string {
	trimmed := strings.TrimSpace(text)
	if len(trimmed) <= memoryMaxTurnChars {
		return trimmed
	}
	return strings.TrimSpace(trimmed[:memoryMaxTurnChars])
}

func trimSummary(summary string) string {
	trimmed := strings.TrimSpace(summary)
	if len(trimmed) <= memoryMaxSummaryChars {
		return trimmed
	}
	trimmed = trimmed[:memoryMaxSummaryChars]
	if cut := strings.LastIndex(trimmed, "\n"); cut >= memoryMaxSummaryChars/2 {
		trimmed = trimmed[:cut]
	}
	return strings.TrimSpace(trimmed)
}

func totalContextChars(summary string, turns []ConversationTurn) int {
	total := len(summary)
	for _, turn := range turns {
		total += len(turn.Content)
	}
	return total
}

func (m *MemoryStore) rollMemoryLocked(sm *senderMemory) {
	if sm == nil {
		return
	}
	if len(sm.Turns) > memoryResetKeepTurns {
		sm.Turns = sm.Turns[len(sm.Turns)-memoryResetKeepTurns:]
	}
	sm.Summary = ""
	sm.TotalTurns = len(sm.Turns)
	sm.LastTouched = time.Now()
}
