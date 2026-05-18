// Package state provides flexible IPAM with /27 block allocation for enterprise multi-network tenants
package state

import (
	"encoding/json"
	"fmt"
	"net"
	"sync"
	"sync/atomic"
	"time"

	"WantasticCore/internal/cache"
	"WantasticCore/internal/store"
)

// TenantAllocation represents a tenant's allocated IP blocks
type TenantAllocation struct {
	TenantID    string      // Tenant/Account ID
	Networks    []net.IPNet // List of allocated /27 blocks (ready for UserspaceTUN)
	AllocatedAt time.Time   // Initial allocation timestamp
	UpdatedAt   time.Time   // Last modification timestamp
	BlockCount  int         // Number of /27 blocks allocated
	ServerIPs   []net.IP    // First usable IP in each block (for gateway/server)
}

// MarshalJSON implements json.Marshaler for TenantAllocation
func (ta *TenantAllocation) MarshalJSON() ([]byte, error) {
	networks := make([]string, len(ta.Networks))
	for i, net := range ta.Networks {
		networks[i] = net.String()
	}

	serverIPs := make([]string, len(ta.ServerIPs))
	for i, ip := range ta.ServerIPs {
		serverIPs[i] = ip.String()
	}

	return json.Marshal(&struct {
		TenantID    string    `json:"tenant_id"`
		Networks    []string  `json:"networks"`
		AllocatedAt time.Time `json:"allocated_at"`
		UpdatedAt   time.Time `json:"updated_at"`
		BlockCount  int       `json:"block_count"`
		ServerIPs   []string  `json:"server_ips"`
	}{
		TenantID:    ta.TenantID,
		Networks:    networks,
		AllocatedAt: ta.AllocatedAt,
		UpdatedAt:   ta.UpdatedAt,
		BlockCount:  ta.BlockCount,
		ServerIPs:   serverIPs,
	})
}

// Block27 represents a single /27 block (32 IPs)
type Block27 struct {
	Network   net.IPNet // The /27 network
	TenantID  string    // Which tenant owns this block (empty if free)
	Allocated bool      // Is this block allocated?
	PoolIndex int       // Which global pool this came from
}

// GlobalIPAM manages IP allocation using /27 blocks from global pools
type GlobalIPAM struct {
	// Global pool tracking: all /27 blocks across all pools
	// Key: CIDR string (e.g., "10.0.1.0/27"), Value: *Block27
	blocks sync.Map

	// Tenant allocations
	// Key: tenantID, Value: *TenantAllocation
	tenants sync.Map

	// Free block queue for fast allocation
	// Each pool has its own queue
	freeBlocks   map[int]*BlockQueue // map[poolIndex]*BlockQueue
	freeBlocksMu sync.RWMutex

	// Global pool definitions
	pools []PoolDefinition

	// Cache for O(1) lookups
	cache *cache.Cache

	// Statistics (atomic for lock-free reads)
	totalBlocks     atomic.Int64
	allocatedBlocks atomic.Int64
	totalTenants    atomic.Int64
	totalExpansions atomic.Int64

	repo store.IPAMRepository
}

// BlockQueue is a thread-safe queue of free /27 blocks
type BlockQueue struct {
	blocks []string // CIDR strings of free blocks
	mu     sync.Mutex
}

func newBlockQueue() *BlockQueue {
	return &BlockQueue{
		blocks: make([]string, 0, 1024),
	}
}

func (ipam *GlobalIPAM) getFreeBlockQueue(poolIndex int) (*BlockQueue, bool) {
	ipam.freeBlocksMu.RLock()
	queue, ok := ipam.freeBlocks[poolIndex]
	ipam.freeBlocksMu.RUnlock()
	return queue, ok
}

func (ipam *GlobalIPAM) ensureFreeBlockQueue(poolIndex int) *BlockQueue {
	if queue, ok := ipam.getFreeBlockQueue(poolIndex); ok {
		return queue
	}

	ipam.freeBlocksMu.Lock()
	defer ipam.freeBlocksMu.Unlock()

	if queue, ok := ipam.freeBlocks[poolIndex]; ok {
		return queue
	}

	queue := newBlockQueue()
	ipam.freeBlocks[poolIndex] = queue

	return queue
}

// PoolDefinition defines a source pool (e.g., 10.0.0.0/8)
type PoolDefinition struct {
	Index  int       // Pool index
	CIDR   string    // Original CIDR (e.g., "10.0.0.0/8")
	IPNet  net.IPNet // Parsed network
	Prefix string    // Display prefix (e.g., "10.0.0.0/8")
}

// NewGlobalIPAM creates a new IPAM that splits pools into /27 blocks
func NewGlobalIPAM(repo store.IPAMRepository, baseCIDRs []string) (*GlobalIPAM, error) {
	ipam := &GlobalIPAM{
		repo:       repo,
		pools:      make([]PoolDefinition, 0, len(baseCIDRs)),
		freeBlocks: make(map[int]*BlockQueue),
	}

	// Initialize cache
	cacheConfig := &cache.CacheConfig{
		Algorithm:   cache.AlgorithmLRU,
		MaxSize:     20 * 1024 * 1024, // 20MB
		MaxEntries:  50000,
		DefaultTTL:  10 * time.Minute,
		CleanupFreq: 2 * time.Minute,
		ShardCount:  16,
	}
	ipam.cache = cache.NewCache(cacheConfig)

	// optimized: 1. Restore state from DB first (to avoid N+1 upserts)
	if repo != nil {
		blocks, err := repo.RestoreState()
		if err == nil {
			ipam.allocatedBlocks.Store(0)

			for _, b := range blocks {
				// Update in-memory map
				_, ipnet, _ := net.ParseCIDR(b.CIDR)
				block27 := &Block27{
					Network:   *ipnet,
					TenantID:  b.TenantID,
					Allocated: b.Allocated,
					PoolIndex: b.PoolIndex,
				}
				ipam.blocks.Store(b.CIDR, block27)

				if b.Allocated {
					ipam.allocatedBlocks.Add(1)
				} else {
					// We will add to free queue later when we iterate pools
					// ensuring strict order or just add here?
					// Actually, splitIntoBlocks27 handles the queueing.
					// But we need to make sure we don't double-add.
					// Let's add here to be safe and ensure splitIntoBlocks27 checks existence.
				}
			}
			fmt.Printf(" IPAM: Restored %d blocks from database\n", len(blocks))
		}
	}

	// 2. Initial split of ALL CIDRs (fill in gaps and populate queues)
	for poolIndex, cidr := range baseCIDRs {
		if err := ipam.addPoolAndSplit(poolIndex, cidr); err != nil {
			return nil, fmt.Errorf("failed to add pool %s: %w", cidr, err)
		}
	}

	if len(ipam.pools) == 0 {
		return nil, fmt.Errorf("no valid pools created from CIDRs: %v", baseCIDRs)
	}

	return ipam, nil
}

// addPoolAndSplit splits a large CIDR into /27 blocks
func (ipam *GlobalIPAM) addPoolAndSplit(poolIndex int, cidr string) error {
	_, ipnet, err := net.ParseCIDR(cidr)
	if err != nil {
		return fmt.Errorf("invalid CIDR: %w", err)
	}

	// Only support IPv4
	if ipnet.IP.To4() == nil {
		return fmt.Errorf("only IPv4 supported: %s", cidr)
	}

	// Store pool definition
	pool := PoolDefinition{
		Index:  poolIndex,
		CIDR:   cidr,
		IPNet:  *ipnet,
		Prefix: cidr,
	}
	ipam.pools = append(ipam.pools, pool)

	// Create free block queue for this pool
	ipam.ensureFreeBlockQueue(poolIndex)

	// Split the CIDR into /27 blocks asynchronously to not block startup
	go func() {
		start := time.Now()
		count := ipam.splitIntoBlocks27(ipnet, poolIndex)
		fmt.Printf(" IPAM: Initialized pool %d (%s) with %d blocks in %v\n", poolIndex, cidr, count, time.Since(start))
	}()

	return nil
}

// splitIntoBlocks27 divides a CIDR into /27 blocks and adds them to free queue
func (ipam *GlobalIPAM) splitIntoBlocks27(ipnet *net.IPNet, poolIndex int) int {
	count := 0
	queue := ipam.ensureFreeBlockQueue(poolIndex)

	// Calculate how many /27 blocks fit in this CIDR
	prefixLen, _ := ipnet.Mask.Size()
	if prefixLen > 27 {
		// CIDR is smaller than /27, can't split
		return 0
	}

	numBlocks := 1 << (27 - prefixLen) // 2^(27-prefix)

	// Start from the base IP
	currentIP := make(net.IP, len(ipnet.IP))
	copy(currentIP, ipnet.IP)

	// Create /27 mask
	mask27 := net.CIDRMask(27, 32)

	// Create persistent transaction if needed? No, UpsertBlock is atomic.
	// But performing 30k+ Upserts is slow.

	for i := 0; i < numBlocks; i++ {
		// Create /27 block
		block27 := net.IPNet{
			IP:   make(net.IP, len(currentIP)),
			Mask: mask27,
		}
		copy(block27.IP, currentIP)

		blockCIDR := block27.String()

		// Optimization: Check if block already exists (loaded from DB)
		existing, loaded := ipam.blocks.Load(blockCIDR)
		var block *Block27

		if loaded {
			// Block exists, use it
			block = existing.(*Block27)
			// Ensure PoolIndex matches (migration safety)
			block.PoolIndex = poolIndex
		} else {
			// New block (fresh pool or expansion)
			block = &Block27{
				Network:   block27,
				TenantID:  "",
				Allocated: false,
				PoolIndex: poolIndex,
			}
			ipam.blocks.Store(blockCIDR, block)

			// Persist to DB only if it's new (avoids N+1 updates on startup)
			if ipam.repo != nil {
				_ = ipam.repo.UpsertBlock(&store.IPAMBlockData{
					CIDR:      blockCIDR,
					TenantID:  "",
					Allocated: false,
					PoolIndex: poolIndex,
				})
			}
		}

		// Queuing Logic:
		// If unallocated, ensure it's in the free queue.
		// Since we rebuild the queue from scratch here, we add it.
		if !block.Allocated {
			queue.mu.Lock()
			queue.blocks = append(queue.blocks, blockCIDR)
			queue.mu.Unlock()
		}

		count++

		// Move to next /27 block (add 32 to IP)
		incrementIPBy(currentIP, 32)
		if !ipnet.Contains(currentIP) {
			break
		}
	}

	ipam.totalBlocks.Add(int64(count))

	return count
}

// incrementIPBy adds n to an IP address
func incrementIPBy(ip net.IP, n int) {
	for i := len(ip) - 1; i >= 0 && n > 0; i-- {
		val := int(ip[i]) + n
		ip[i] = byte(val & 0xFF)
		n = val >> 8
	}
}

// AllocateTenant allocates the requested number of /27 blocks for a new tenant.
func (ipam *GlobalIPAM) AllocateTenant(tenantID string, blockCount int) (*TenantAllocation, error) {
	// Check if tenant already exists
	if existing, ok := ipam.tenants.Load(tenantID); ok {
		return nil, fmt.Errorf("tenant %s already has allocation: %+v", tenantID, existing)
	}

	if blockCount < 1 {
		blockCount = 1
	}

	// Allocate blocks
	blocks, err := ipam.allocateBlocks(tenantID, blockCount)
	if err != nil {
		return nil, fmt.Errorf("failed to allocate %d blocks: %w", blockCount, err)
	}

	// Create allocation record
	allocation := &TenantAllocation{
		TenantID:    tenantID,
		Networks:    blocks,
		AllocatedAt: time.Now(),
		UpdatedAt:   time.Now(),
		BlockCount:  len(blocks),
		ServerIPs:   make([]net.IP, len(blocks)),
	}

	// Calculate server IP for each block (first usable IP)
	for i, network := range blocks {
		allocation.ServerIPs[i] = getFirstUsableIP(&network)
	}

	// Store allocation
	ipam.tenants.Store(tenantID, allocation)
	ipam.cache.Set(tenantID, allocation)
	ipam.totalTenants.Add(1)

	return allocation, nil
}

// allocateBlocks allocates N free /27 blocks for a tenant
func (ipam *GlobalIPAM) allocateBlocks(tenantID string, count int) ([]net.IPNet, error) {
	networks := make([]net.IPNet, 0, count)
	allocated := make([]string, 0, count)

	// Try to allocate from each pool in round-robin fashion
	ipam.freeBlocksMu.RLock()
	poolIndices := make([]int, 0, len(ipam.freeBlocks))
	for poolIdx := range ipam.freeBlocks {
		poolIndices = append(poolIndices, poolIdx)
	}
	ipam.freeBlocksMu.RUnlock()

	poolIdx := 0
	for len(networks) < count {
		if len(poolIndices) == 0 {
			// Rollback allocated blocks
			for _, cidr := range allocated {
				_ = ipam.releaseBlock(cidr)
			}
			return nil, fmt.Errorf("no free blocks available (allocated %d/%d)", len(networks), count)
		}

		currentPool := poolIndices[poolIdx%len(poolIndices)]

		// Try to get blocks from repo if available
		if ipam.repo != nil {
			needed := count - len(networks)
			cIDs, err := ipam.repo.AllocateBlocks(tenantID, currentPool, needed)
			if err == nil {
				for _, cidr := range cIDs {
					if blockVal, ok := ipam.blocks.Load(cidr); ok {
						block := blockVal.(*Block27)
						block.TenantID = tenantID
						block.Allocated = true
						networks = append(networks, block.Network)
						allocated = append(allocated, cidr)
						ipam.allocatedBlocks.Add(1)
					}
				}
				// If we got everything we needed, we're done
				if len(networks) >= count {
					break
				}
			}
			// If pool is empty or failed, move to next
			poolIndices = append(poolIndices[:poolIdx%len(poolIndices)], poolIndices[(poolIdx%len(poolIndices))+1:]...)
			continue
		}

		// Fallback to in-memory allocation (single core mode)
		blockCIDR, err := ipam.popFreeBlock(currentPool)
		if err != nil {
			// This pool is empty, remove it
			poolIndices = append(poolIndices[:poolIdx%len(poolIndices)], poolIndices[(poolIdx%len(poolIndices))+1:]...)
			continue
		}

		// Mark block as allocated
		if blockVal, ok := ipam.blocks.Load(blockCIDR); ok {
			block := blockVal.(*Block27)
			block.TenantID = tenantID
			block.Allocated = true

			networks = append(networks, block.Network)
			allocated = append(allocated, blockCIDR)
			ipam.allocatedBlocks.Add(1)
		}

		poolIdx++
	}

	return networks, nil
}

// popFreeBlock removes and returns a free block from a pool's queue
func (ipam *GlobalIPAM) popFreeBlock(poolIndex int) (string, error) {
	// If we have a repo, use it for atomic allocation across cores
	if ipam.repo != nil {
		// This is a temporary tenant ID, will be updated by caller
		// Actually, popFreeBlock is called by allocateBlocks which knows the tenantID.
		// Let's refactor allocateBlocks to use Repo.AllocateBlocks directly.
		return "", fmt.Errorf("popFreeBlock should not be used with persistent storage")
	}

	queue, ok := ipam.getFreeBlockQueue(poolIndex)

	if !ok {
		return "", fmt.Errorf("pool %d not found", poolIndex)
	}

	queue.mu.Lock()
	defer queue.mu.Unlock()

	if len(queue.blocks) == 0 {
		return "", fmt.Errorf("no free blocks in pool %d", poolIndex)
	}

	// Pop last block (LIFO for better locality)
	blockCIDR := queue.blocks[len(queue.blocks)-1]
	queue.blocks = queue.blocks[:len(queue.blocks)-1]

	return blockCIDR, nil
}

// releaseBlock returns a block to the free queue
func (ipam *GlobalIPAM) releaseBlock(blockCIDR string) error {
	blockVal, ok := ipam.blocks.Load(blockCIDR)
	if !ok {
		return fmt.Errorf("block %s not found", blockCIDR)
	}

	block := blockVal.(*Block27)
	block.TenantID = ""
	block.Allocated = false

	// Update DB if available
	if ipam.repo != nil {
		_ = ipam.repo.UpsertBlock(&store.IPAMBlockData{
			CIDR:      blockCIDR,
			TenantID:  "",
			Allocated: false,
			PoolIndex: block.PoolIndex,
		})
	}

	// Return to free queue
	queue, ok := ipam.getFreeBlockQueue(block.PoolIndex)

	if !ok {
		return fmt.Errorf("pool %d not found", block.PoolIndex)
	}

	queue.mu.Lock()
	queue.blocks = append(queue.blocks, blockCIDR)
	queue.mu.Unlock()

	ipam.allocatedBlocks.Add(-1)

	return nil
}

// ExpandTenant adds more /27 blocks to an existing tenant (for enterprise growth)
func (ipam *GlobalIPAM) ExpandTenant(tenantID string, additionalBlocks int) (*TenantAllocation, error) {
	allocVal, ok := ipam.tenants.Load(tenantID)
	if !ok {
		return nil, fmt.Errorf("tenant %s not found", tenantID)
	}

	allocation := allocVal.(*TenantAllocation)

	// Allocate additional blocks
	newBlocks, err := ipam.allocateBlocks(tenantID, additionalBlocks)
	if err != nil {
		return nil, fmt.Errorf("failed to allocate %d additional blocks: %w", additionalBlocks, err)
	}

	// Add to existing allocation
	allocation.Networks = append(allocation.Networks, newBlocks...)
	allocation.BlockCount = len(allocation.Networks)
	allocation.UpdatedAt = time.Now()

	// Calculate server IPs for new blocks
	for _, network := range newBlocks {
		allocation.ServerIPs = append(allocation.ServerIPs, getFirstUsableIP(&network))
	}

	// Update storage
	ipam.tenants.Store(tenantID, allocation)
	ipam.cache.Set(tenantID, allocation)
	ipam.totalExpansions.Add(1)

	return allocation, nil
}

// GetAllocation retrieves a tenant's allocation (O(1) with cache)
func (ipam *GlobalIPAM) GetAllocation(tenantID string) (*TenantAllocation, error) {
	// Try cache first
	if cached, ok := ipam.cache.Get(tenantID); ok {
		if allocation, ok := cached.(*TenantAllocation); ok {
			return allocation, nil
		}
	}

	// Cache miss - load from sync.Map
	allocVal, ok := ipam.tenants.Load(tenantID)
	if !ok {
		return nil, fmt.Errorf("tenant %s not found", tenantID)
	}

	allocation := allocVal.(*TenantAllocation)
	ipam.cache.Set(tenantID, allocation)

	return allocation, nil
}

// GetNetworkList returns []net.IPNet for userspace TUN
func (ipam *GlobalIPAM) GetNetworkList(tenantID string) ([]net.IPNet, error) {
	allocation, err := ipam.GetAllocation(tenantID)
	if err != nil {
		return nil, err
	}

	return allocation.Networks, nil
}

// ReleaseTenant releases all blocks allocated to a tenant
func (ipam *GlobalIPAM) ReleaseTenant(tenantID string) error {
	allocVal, ok := ipam.tenants.Load(tenantID)
	if !ok {
		return fmt.Errorf("tenant %s not found", tenantID)
	}

	allocation := allocVal.(*TenantAllocation)

	// Release from DB if available
	if ipam.repo != nil {
		_ = ipam.repo.ReleaseBlocks(tenantID)
	}

	// Release all blocks in-memory
	for _, network := range allocation.Networks {
		blockCIDR := network.String()
		_ = ipam.releaseBlock(blockCIDR)
	}

	// Remove tenant
	ipam.tenants.Delete(tenantID)
	ipam.cache.Delete(tenantID)
	ipam.totalTenants.Add(-1)

	return nil
}

// RestoreAllocation restores a tenant's allocation from database
func (ipam *GlobalIPAM) RestoreAllocation(tenantID string, networks []string, allocatedAt time.Time) error {
	// Parse network CIDRs
	parsedNetworks := make([]net.IPNet, 0, len(networks))
	serverIPs := make([]net.IP, 0, len(networks))

	for _, cidr := range networks {
		_, ipnet, err := net.ParseCIDR(cidr)
		if err != nil {
			return fmt.Errorf("invalid CIDR %s: %w", cidr, err)
		}

		// Verify block exists in our global blocks
		blockVal, ok := ipam.blocks.Load(cidr)
		if !ok {
			return fmt.Errorf("block %s not found in global pool", cidr)
		}

		// Mark as allocated
		block := blockVal.(*Block27)
		block.TenantID = tenantID
		block.Allocated = true

		// Remove from free queue
		if queue, ok := ipam.getFreeBlockQueue(block.PoolIndex); ok {
			queue.mu.Lock()
			// Remove from free list if present
			for i, freeCIDR := range queue.blocks {
				if freeCIDR == cidr {
					queue.blocks = append(queue.blocks[:i], queue.blocks[i+1:]...)
					break
				}
			}
			queue.mu.Unlock()
		}

		parsedNetworks = append(parsedNetworks, *ipnet)
		serverIPs = append(serverIPs, getFirstUsableIP(ipnet))
		ipam.allocatedBlocks.Add(1)
	}

	// Create allocation record
	allocation := &TenantAllocation{
		TenantID:    tenantID,
		Networks:    parsedNetworks,
		AllocatedAt: allocatedAt,
		UpdatedAt:   time.Now(),
		BlockCount:  len(parsedNetworks),
		ServerIPs:   serverIPs,
	}

	// Store
	ipam.tenants.Store(tenantID, allocation)
	ipam.cache.Set(tenantID, allocation)
	ipam.totalTenants.Add(1)

	return nil
}

// getFirstUsableIP returns the first usable IP in a /27 block (skip network address)
func getFirstUsableIP(network *net.IPNet) net.IP {
	ip := make(net.IP, len(network.IP))
	copy(ip, network.IP)

	// Ensure IPv4
	if ipv4 := ip.To4(); ipv4 != nil {
		ip = ipv4
	}

	// Increment by 1 (skip network address)
	for i := len(ip) - 1; i >= 0; i-- {
		ip[i]++
		if ip[i] != 0 {
			break
		}
	}

	return ip
}

// GetStatistics returns current IPAM statistics
func (ipam *GlobalIPAM) GetStatistics() *IPAMStatistics {
	stats := &IPAMStatistics{
		TotalPools:      len(ipam.pools),
		TotalBlocks:     ipam.totalBlocks.Load(),
		AllocatedBlocks: ipam.allocatedBlocks.Load(),
		FreeBlocks:      ipam.totalBlocks.Load() - ipam.allocatedBlocks.Load(),
		TotalTenants:    ipam.totalTenants.Load(),
		TotalExpansions: ipam.totalExpansions.Load(),
		PoolStats:       make([]PoolStatistics, 0, len(ipam.pools)),
	}

	// Per-pool statistics
	for _, pool := range ipam.pools {
		freeCount := 0
		if queue, ok := ipam.getFreeBlockQueue(pool.Index); ok {
			queue.mu.Lock()
			freeCount = len(queue.blocks)
			queue.mu.Unlock()
		}

		stats.PoolStats = append(stats.PoolStats, PoolStatistics{
			PoolIndex:  pool.Index,
			CIDR:       pool.CIDR,
			FreeBlocks: freeCount,
		})
	}

	return stats
}

// IPAMStatistics holds IPAM metrics
type IPAMStatistics struct {
	TotalPools      int
	TotalBlocks     int64 // Total /27 blocks
	AllocatedBlocks int64 // Allocated /27 blocks
	FreeBlocks      int64 // Free /27 blocks
	TotalTenants    int64
	TotalExpansions int64
	PoolStats       []PoolStatistics
}

// PoolStatistics holds per-pool metrics
type PoolStatistics struct {
	PoolIndex  int
	CIDR       string
	FreeBlocks int
}

// Close shuts down the IPAM manager gracefully
func (ipam *GlobalIPAM) Close() error {
	if ipam.cache != nil {
		ipam.cache.Close()
	}
	return nil
}
