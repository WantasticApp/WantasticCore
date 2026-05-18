// Package cache provides a high-performance, multi-algorithm caching system
// optimized for various use cases across the WantasticCore project.
//
// Features:
// - ARC (Adaptive Replacement Cache) algorithm for optimal hit ratios
// - TinyLFU for frequency-based eviction with minimal memory overhead
// - TTL support with efficient expiration
// - Thread-safe operations with minimal lock contention
// - Multiple cache tiers for different use cases
// - Graceful shutdown with context cancellation
// - Memory pressure handling with automatic eviction
package cache

import (
	"context"
	"hash/fnv"
	"sync"
	"sync/atomic"
	"time"
	"unsafe"
)

// CacheAlgorithm defines the caching algorithm to use
type CacheAlgorithm string

const (
	// AlgorithmARC uses Adaptive Replacement Cache - best for mixed workloads
	// Adapts between LRU and LFU based on access patterns
	AlgorithmARC CacheAlgorithm = "arc"

	// AlgorithmTinyLFU uses TinyLFU - best for high-frequency access patterns
	// Minimal memory overhead with excellent hit ratios
	AlgorithmTinyLFU CacheAlgorithm = "tinylfu"

	// AlgorithmLRU uses standard LRU - simple and predictable
	// Good for general purpose caching with recency bias
	AlgorithmLRU CacheAlgorithm = "lru"
)

// CacheType defines different cache tiers for various use cases
type CacheType string

const (
	// TypeSession for user sessions, auth tokens, WebSSH sessions
	// Characteristics: High read/write, medium TTL, exact matches
	TypeSession CacheType = "session"

	// TypeACL for ACL rule lookups and packet filtering decisions
	// Characteristics: Very high read, low write, pattern matching
	TypeACL CacheType = "acl"

	// TypePeer for peer metadata and status information
	// Characteristics: Medium read/write, variable TTL, complex objects
	TypePeer CacheType = "peer"

	// TypeConfig for configuration data and settings
	// Characteristics: Low read/write, long TTL, large objects
	TypeConfig CacheType = "config"

	// TypeMetrics for statistics and monitoring data
	// Characteristics: High write, medium read, short TTL
	TypeMetrics CacheType = "metrics"
)

// Entry represents a cache entry with metadata
type Entry struct {
	Key         string
	Value       any
	Size        int64
	CreatedAt   time.Time
	AccessedAt  time.Time
	ExpiresAt   time.Time
	AccessCount uint64

	// Internal fields for algorithms
	arcType int // For ARC: 0=T1, 1=T2, 2=B1, 3=B2
	lruNext *Entry
	lruPrev *Entry
}

// isExpired checks if the entry has expired
func (e *Entry) isExpired() bool {
	return !e.ExpiresAt.IsZero() && time.Now().After(e.ExpiresAt)
}

// CacheConfig defines configuration for a cache instance
type CacheConfig struct {
	Algorithm   CacheAlgorithm
	MaxSize     int64         // Maximum memory usage in bytes
	MaxEntries  int           // Maximum number of entries
	DefaultTTL  time.Duration // Default TTL for entries
	CleanupFreq time.Duration // How often to run cleanup
	ShardCount  int           // Number of shards for lock distribution
}

// DefaultConfigs provides optimized configurations for different cache types
var DefaultConfigs = map[CacheType]*CacheConfig{
	TypeSession: {
		Algorithm:   AlgorithmLRU,      // Simple LRU for session data
		MaxSize:     100 * 1024 * 1024, // 100MB
		MaxEntries:  50000,             // 50k sessions
		DefaultTTL:  1 * time.Hour,     // 1 hour session timeout
		CleanupFreq: 5 * time.Minute,   // Cleanup every 5 minutes
		ShardCount:  16,                // 16 shards for good concurrency
	},
	TypeACL: {
		Algorithm:   AlgorithmTinyLFU, // TinyLFU for high-frequency ACL lookups
		MaxSize:     50 * 1024 * 1024, // 50MB
		MaxEntries:  100000,           // 100k ACL decisions
		DefaultTTL:  10 * time.Minute, // 10 minutes (rules change infrequently)
		CleanupFreq: 2 * time.Minute,  // Fast cleanup for security
		ShardCount:  32,               // High concurrency for packet filtering
	},
	TypePeer: {
		Algorithm:   AlgorithmARC,      // ARC adapts to peer access patterns
		MaxSize:     200 * 1024 * 1024, // 200MB
		MaxEntries:  25000,             // 25k peers
		DefaultTTL:  5 * time.Minute,   // 5 minutes (peer status changes)
		CleanupFreq: 1 * time.Minute,   // Frequent cleanup for real-time data
		ShardCount:  16,                // Medium concurrency
	},
	TypeConfig: {
		Algorithm:   AlgorithmLRU,     // LRU for infrequently changing config
		MaxSize:     20 * 1024 * 1024, // 20MB
		MaxEntries:  5000,             // 5k config entries
		DefaultTTL:  30 * time.Minute, // 30 minutes (config changes rarely)
		CleanupFreq: 15 * time.Minute, // Infrequent cleanup
		ShardCount:  4,                // Low concurrency
	},
	TypeMetrics: {
		Algorithm:   AlgorithmTinyLFU, // TinyLFU for high-write metrics
		MaxSize:     30 * 1024 * 1024, // 30MB
		MaxEntries:  15000,            // 15k metrics
		DefaultTTL:  1 * time.Minute,  // 1 minute for real-time metrics
		CleanupFreq: 30 * time.Second, // Fast cleanup for metrics
		ShardCount:  8,                // Medium concurrency
	},
}

// CacheShard represents a single shard of a cache for lock distribution
type CacheShard struct {
	entries map[string]*Entry
	mu      sync.RWMutex

	// Algorithm-specific structures
	lruHead *Entry // For LRU
	lruTail *Entry // For LRU

	// ARC algorithm structures
	arcT1 *Entry // Recent pages (LRU)
	arcT2 *Entry // Frequent pages (LFU)
	arcB1 *Entry // Ghost list for T1
	arcB2 *Entry // Ghost list for T2
	arcP  int    // Adaptive parameter

	// TinyLFU structures
	lfuFilter *bloomFilter // Bloom filter for frequency estimation
	lfuWindow []*Entry     // LRU window for new entries
	lfuMain   []*Entry     // Main cache area

	// Statistics - use atomic for lock-free reads
	hits        uint64
	misses      uint64
	evictions   uint64
	currentSize int64
}

// bloomFilter is a space-efficient probabilistic data structure for TinyLFU
type bloomFilter struct {
	bits   []uint64
	size   uint64
	hashes int
	mu     sync.Mutex // Protect concurrent access
}

// Cache represents a high-performance cache with configurable algorithms
type Cache struct {
	config *CacheConfig
	shards []*CacheShard
	stopCh chan struct{}
	closed atomic.Bool

	// Global statistics - atomic for lock-free reads
	totalHits   atomic.Uint64
	totalMisses atomic.Uint64

	ctx    context.Context
	cancel context.CancelFunc

	// Memory pressure callback (optional)
	onMemoryPressure func()
}

// NewCache creates a new cache with the specified configuration
func NewCache(config *CacheConfig) *Cache {
	return NewCacheWithContext(context.Background(), config)
}

// NewCacheWithContext creates a new cache with context for graceful shutdown
func NewCacheWithContext(ctx context.Context, config *CacheConfig) *Cache {
	if config.ShardCount <= 0 {
		config.ShardCount = 16
	}

	// Calculate per-shard limits
	entriesPerShard := config.MaxEntries / config.ShardCount
	if entriesPerShard < 100 {
		entriesPerShard = 100
	}

	shards := make([]*CacheShard, config.ShardCount)
	for i := range shards {
		shards[i] = &CacheShard{
			entries: make(map[string]*Entry, entriesPerShard/4), // Pre-allocate 25% capacity
		}

		// Initialize algorithm-specific structures
		switch config.Algorithm {
		case AlgorithmTinyLFU:
			// Create bloom filter once per shard, not per operation
			shards[i].lfuFilter = newBloomFilter(entriesPerShard, 3)
			shards[i].lfuWindow = make([]*Entry, 0, entriesPerShard/10)
			shards[i].lfuMain = make([]*Entry, 0, entriesPerShard)
		}
	}

	cacheCtx, cancel := context.WithCancel(ctx)

	cache := &Cache{
		config: config,
		shards: shards,
		stopCh: make(chan struct{}),
		ctx:    cacheCtx,
		cancel: cancel,
	}

	// Start cleanup goroutine with context
	go cache.cleanupLoop()

	// Start memory pressure monitor
	go cache.memoryPressureMonitor()

	return cache
}

// memoryPressureMonitor checks for memory pressure and evicts entries if needed
func (c *Cache) memoryPressureMonitor() {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-c.ctx.Done():
			return
		case <-c.stopCh:
			return
		case <-ticker.C:
			// Check cache-level memory pressure based on tracked size (not heap stats).
			// NOTE: runtime.ReadMemStats was intentionally removed — it was dead code
			// (result never used) and causes a stop-the-world GC pause every 30s.

			// If we're using more than 80% of allowed memory, evict aggressively
			totalSize := c.GetTotalSize()
			if c.config.MaxSize > 0 && totalSize > int64(float64(c.config.MaxSize)*0.8) {
				c.evictUnderPressure(int(float64(totalSize) * 0.2)) // Evict 20%
			}

			// Call user-defined callback if set
			if c.onMemoryPressure != nil {
				c.onMemoryPressure()
			}
		}
	}
}

// GetTotalSize returns the total size of all cached data across all shards
func (c *Cache) GetTotalSize() int64 {
	var total int64
	for _, shard := range c.shards {
		shard.mu.RLock()
		total += shard.currentSize
		shard.mu.RUnlock()
	}
	return total
}

// evictUnderPressure evicts entries to free up the specified number of bytes
func (c *Cache) evictUnderPressure(bytesToFree int) {
	freedBytes := 0
	for _, shard := range c.shards {
		if freedBytes >= bytesToFree {
			break
		}
		shard.mu.Lock()
		for freedBytes < bytesToFree && shard.lruTail != nil {
			victim := shard.lruTail
			freedBytes += int(victim.Size)
			delete(shard.entries, victim.Key)
			shard.remove(victim)
			// Help GC by clearing references
			victim.Value = nil
		}
		shard.mu.Unlock()
	}
}

// SetMemoryPressureCallback sets a callback for memory pressure events
func (c *Cache) SetMemoryPressureCallback(callback func()) {
	c.onMemoryPressure = callback
}

// NewCacheForType creates a cache with pre-optimized configuration for a specific type
func NewCacheForType(cacheType CacheType) *Cache {
	config, exists := DefaultConfigs[cacheType]
	if !exists {
		// Fallback to general purpose config
		config = &CacheConfig{
			Algorithm:   AlgorithmARC,
			MaxSize:     50 * 1024 * 1024,
			MaxEntries:  10000,
			DefaultTTL:  5 * time.Minute,
			CleanupFreq: 2 * time.Minute,
			ShardCount:  16,
		}
	}

	return NewCache(config)
}

// getShard returns the appropriate shard for a key using consistent hashing
func (c *Cache) getShard(key string) *CacheShard {
	h := fnv.New64a()
	h.Write([]byte(key))
	return c.shards[h.Sum64()%uint64(len(c.shards))]
}

// calculateSize estimates the memory size of a value
func calculateSize(value any) int64 {
	switch v := value.(type) {
	case string:
		return int64(len(v)) + int64(unsafe.Sizeof(v))
	case []byte:
		return int64(len(v)) + int64(unsafe.Sizeof(v))
	case int, int32, int64:
		return 8
	case float32, float64:
		return 8
	case bool:
		return 1
	default:
		// Rough estimate for complex objects
		return 256
	}
}

// Set stores a value in the cache with default TTL
func (c *Cache) Set(key string, value any) {
	c.SetWithTTL(key, value, c.config.DefaultTTL)
}

// SetWithTTL stores a value in the cache with specified TTL
func (c *Cache) SetWithTTL(key string, value any, ttl time.Duration) {
	if key == "" {
		return
	}

	shard := c.getShard(key)
	shard.mu.Lock()
	defer shard.mu.Unlock()

	now := time.Now()
	entry := &Entry{
		Key:         key,
		Value:       value,
		Size:        calculateSize(value),
		CreatedAt:   now,
		AccessedAt:  now,
		AccessCount: 1,
	}

	if ttl > 0 {
		entry.ExpiresAt = now.Add(ttl)
	}

	// Remove existing entry if present
	if existing, exists := shard.entries[key]; exists {
		shard.remove(existing)
	}

	// Check capacity and evict if necessary
	c.evictIfNecessary(shard)

	// Insert new entry
	shard.entries[key] = entry
	shard.currentSize += entry.Size

	// Algorithm-specific insertion
	switch c.config.Algorithm {
	case AlgorithmLRU:
		shard.insertLRU(entry)
	case AlgorithmARC:
		shard.insertARC(entry)
	case AlgorithmTinyLFU:
		shard.insertTinyLFU(entry)
	}
}

// Get retrieves a value from the cache
func (c *Cache) Get(key string) (any, bool) {
	if key == "" || c.closed.Load() {
		return nil, false
	}

	shard := c.getShard(key)
	shard.mu.RLock()

	entry, exists := shard.entries[key]
	if !exists {
		shard.mu.RUnlock()
		atomic.AddUint64(&shard.misses, 1)
		c.totalMisses.Add(1)
		return nil, false
	}

	// Check expiration
	if entry.isExpired() {
		shard.mu.RUnlock()
		// Upgrade to write lock and remove expired entry
		shard.mu.Lock()
		if stillExists, ok := shard.entries[key]; ok && stillExists.isExpired() {
			shard.remove(stillExists)
			delete(shard.entries, key)
			// Help GC
			stillExists.Value = nil
		}
		shard.mu.Unlock()
		atomic.AddUint64(&shard.misses, 1)
		c.totalMisses.Add(1)
		return nil, false
	}

	// Update access information (atomic for AccessCount)
	entry.AccessedAt = time.Now()
	atomic.AddUint64(&entry.AccessCount, 1)

	value := entry.Value
	shard.mu.RUnlock()

	atomic.AddUint64(&shard.hits, 1)
	c.totalHits.Add(1)
	return value, true
}

// Delete removes a key from the cache
func (c *Cache) Delete(key string) {
	if key == "" || c.closed.Load() {
		return
	}

	shard := c.getShard(key)
	shard.mu.Lock()
	defer shard.mu.Unlock()

	if entry, exists := shard.entries[key]; exists {
		shard.remove(entry)
		delete(shard.entries, key)
		// Help GC by clearing value reference
		entry.Value = nil
	}
}

// Clear removes all entries from the cache and helps GC
func (c *Cache) Clear() {
	for _, shard := range c.shards {
		shard.mu.Lock()
		// Help GC by clearing all value references
		for _, entry := range shard.entries {
			entry.Value = nil
			entry.lruNext = nil
			entry.lruPrev = nil
		}
		// Create new map to allow old one to be GC'd
		shard.entries = make(map[string]*Entry)
		shard.currentSize = 0
		shard.lruHead = nil
		shard.lruTail = nil
		// Reset bloom filter if using TinyLFU
		if shard.lfuFilter != nil {
			shard.lfuFilter.reset()
		}
		shard.mu.Unlock()
	}
}

// Stats returns cache statistics
func (c *Cache) Stats() map[string]any {
	var totalHits, totalMisses, totalEvictions uint64
	var totalSize int64
	var totalEntries int

	for _, shard := range c.shards {
		shard.mu.RLock()
		totalHits += atomic.LoadUint64(&shard.hits)
		totalMisses += atomic.LoadUint64(&shard.misses)
		totalEvictions += atomic.LoadUint64(&shard.evictions)
		totalSize += shard.currentSize
		totalEntries += len(shard.entries)
		shard.mu.RUnlock()
	}

	hitRatio := float64(0)
	if totalHits+totalMisses > 0 {
		hitRatio = float64(totalHits) / float64(totalHits+totalMisses)
	}

	return map[string]any{
		"algorithm":     string(c.config.Algorithm),
		"total_entries": totalEntries,
		"total_size":    totalSize,
		"max_size":      c.config.MaxSize,
		"max_entries":   c.config.MaxEntries,
		"hits":          totalHits,
		"misses":        totalMisses,
		"evictions":     totalEvictions,
		"hit_ratio":     hitRatio,
		"shard_count":   len(c.shards),
	}
}

// Close shuts down the cache and cleanup routines gracefully
func (c *Cache) Close() {
	if c.closed.Swap(true) {
		return // Already closed
	}

	// Signal goroutines to stop
	close(c.stopCh)
	if c.cancel != nil {
		c.cancel()
	}

	// Clear all entries to help GC
	c.Clear()
}

// cleanupLoop runs periodic cleanup of expired entries
func (c *Cache) cleanupLoop() {
	ticker := time.NewTicker(c.config.CleanupFreq)
	defer ticker.Stop()

	for {
		select {
		case <-c.ctx.Done():
			return
		case <-c.stopCh:
			return
		case <-ticker.C:
			c.cleanup()
		}
	}
}

// cleanup removes expired entries from all shards
func (c *Cache) cleanup() {
	if c.closed.Load() {
		return
	}

	now := time.Now()

	for _, shard := range c.shards {
		shard.mu.Lock()

		for key, entry := range shard.entries {
			if !entry.ExpiresAt.IsZero() && now.After(entry.ExpiresAt) {
				shard.remove(entry)
				delete(shard.entries, key)
				// Help GC
				entry.Value = nil
			}
		}

		shard.mu.Unlock()
	}
}
