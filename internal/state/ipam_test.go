package state

import (
	"testing"
	"time"
)

func TestNewGlobalIPAMInitializesFreeQueuesSafely(t *testing.T) {
	baseCIDRs := []string{
		"10.0.0.0/18",
		"10.0.64.0/18",
		"10.0.128.0/18",
		"10.0.192.0/18",
		"10.1.0.0/18",
		"10.1.64.0/18",
		"10.1.128.0/18",
		"10.1.192.0/18",
	}

	ipam, err := NewGlobalIPAM(nil, baseCIDRs)
	if err != nil {
		t.Fatalf("NewGlobalIPAM() error = %v", err)
	}
	t.Cleanup(func() {
		_ = ipam.Close()
	})

	const blocksPerPool = 512
	deadline := time.Now().Add(5 * time.Second)

	for time.Now().Before(deadline) {
		if allQueuesHaveExpectedBlocks(ipam, len(baseCIDRs), blocksPerPool) {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}

	if !allQueuesHaveExpectedBlocks(ipam, len(baseCIDRs), blocksPerPool) {
		t.Fatalf("timed out waiting for pool initialization; queue lengths = %v", queueLengths(ipam, len(baseCIDRs)))
	}

	wantTotalBlocks := int64(len(baseCIDRs) * blocksPerPool)
	stats := ipam.GetStatistics()
	if stats.TotalBlocks != wantTotalBlocks {
		t.Fatalf("stats totalBlocks = %d, want %d", stats.TotalBlocks, wantTotalBlocks)
	}
	if stats.FreeBlocks != wantTotalBlocks {
		t.Fatalf("stats freeBlocks = %d, want %d", stats.FreeBlocks, wantTotalBlocks)
	}
}

func allQueuesHaveExpectedBlocks(ipam *GlobalIPAM, poolCount int, expected int) bool {
	for poolIndex := 0; poolIndex < poolCount; poolIndex++ {
		queue, ok := ipam.getFreeBlockQueue(poolIndex)
		if !ok {
			return false
		}

		queue.mu.Lock()
		got := len(queue.blocks)
		queue.mu.Unlock()

		if got != expected {
			return false
		}
	}

	return true
}

func queueLengths(ipam *GlobalIPAM, poolCount int) []int {
	lengths := make([]int, 0, poolCount)
	for poolIndex := 0; poolIndex < poolCount; poolIndex++ {
		queue, ok := ipam.getFreeBlockQueue(poolIndex)
		if !ok {
			lengths = append(lengths, -1)
			continue
		}

		queue.mu.Lock()
		got := len(queue.blocks)
		queue.mu.Unlock()

		lengths = append(lengths, got)
	}

	return lengths
}
