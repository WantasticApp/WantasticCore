package cache

import (
	"hash/fnv"
	"math"
)

// newBloomFilter creates a new Bloom filter for TinyLFU
func newBloomFilter(capacity int, hashCount int) *bloomFilter {
	// Calculate optimal bit array size
	size := uint64(-1 * float64(capacity) * math.Log(0.01) / (math.Log(2) * math.Log(2)))
	if size == 0 {
		size = 1
	}

	// Round up to next power of 2
	size = 1 << uint(math.Ceil(math.Log2(float64(size))))

	return &bloomFilter{
		bits:   make([]uint64, size/64+1),
		size:   size,
		hashes: hashCount,
	}
}

// hash generates multiple hash values for a key
func (bf *bloomFilter) hash(key string) []uint64 {
	h1 := fnv.New64a()
	h1.Write([]byte(key))
	hash1 := h1.Sum64()

	h2 := fnv.New64()
	h2.Write([]byte(key))
	hash2 := h2.Sum64()

	hashes := make([]uint64, bf.hashes)
	for i := 0; i < bf.hashes; i++ {
		hashes[i] = (hash1 + uint64(i)*hash2) % bf.size
	}

	return hashes
}

// add adds a key to the Bloom filter (thread-safe)
func (bf *bloomFilter) add(key string) {
	bf.mu.Lock()
	defer bf.mu.Unlock()
	hashes := bf.hash(key)
	for _, h := range hashes {
		wordIndex := h / 64
		bitIndex := h % 64
		bf.bits[wordIndex] |= 1 << bitIndex
	}
}

// contains checks if a key might be in the Bloom filter (thread-safe)
func (bf *bloomFilter) contains(key string) bool {
	bf.mu.Lock()
	defer bf.mu.Unlock()
	hashes := bf.hash(key)
	for _, h := range hashes {
		wordIndex := h / 64
		bitIndex := h % 64
		if bf.bits[wordIndex]&(1<<bitIndex) == 0 {
			return false
		}
	}
	return true
}

// reset clears all bits in the Bloom filter (thread-safe)
func (bf *bloomFilter) reset() {
	bf.mu.Lock()
	defer bf.mu.Unlock()
	for i := range bf.bits {
		bf.bits[i] = 0
	}
}

// CacheShard methods for different algorithms

// remove removes an entry from all algorithm-specific structures
func (s *CacheShard) remove(entry *Entry) {
	if entry == nil {
		return
	}

	s.currentSize -= entry.Size
	s.evictions++

	// Remove from LRU list
	if entry.lruNext != nil {
		entry.lruNext.lruPrev = entry.lruPrev
	} else {
		s.lruTail = entry.lruPrev
	}

	if entry.lruPrev != nil {
		entry.lruPrev.lruNext = entry.lruNext
	} else {
		s.lruHead = entry.lruPrev
	}

	entry.lruNext = nil
	entry.lruPrev = nil
}

// insertLRU inserts an entry using LRU algorithm
func (s *CacheShard) insertLRU(entry *Entry) {
	// Add to head of LRU list
	entry.lruNext = s.lruHead
	entry.lruPrev = nil

	if s.lruHead != nil {
		s.lruHead.lruPrev = entry
	} else {
		s.lruTail = entry
	}

	s.lruHead = entry
}

// insertARC inserts an entry using ARC algorithm
func (s *CacheShard) insertARC(entry *Entry) {
	// Simplified ARC implementation - insert into T1 (recent pages)
	entry.arcType = 0  // T1
	s.insertLRU(entry) // Use LRU for T1
}

// insertTinyLFU inserts an entry using TinyLFU algorithm
func (s *CacheShard) insertTinyLFU(entry *Entry) {
	// Add to frequency filter
	s.lfuFilter.add(entry.Key)

	// Add to LRU window first (admission window)
	s.insertLRU(entry)
}

// evictIfNecessary removes entries if cache is over capacity
func (c *Cache) evictIfNecessary(shard *CacheShard) {
	// Check size limit
	if c.config.MaxSize > 0 && shard.currentSize > c.config.MaxSize/int64(len(c.shards)) {
		c.evictOldest(shard)
	}

	// Check entry count limit
	if c.config.MaxEntries > 0 && len(shard.entries) >= c.config.MaxEntries/len(c.shards) {
		c.evictOldest(shard)
	}
}

// evictOldest removes the least recently used entry
func (c *Cache) evictOldest(shard *CacheShard) {
	if shard.lruTail == nil {
		return
	}

	victim := shard.lruTail
	delete(shard.entries, victim.Key)
	shard.remove(victim)
}
