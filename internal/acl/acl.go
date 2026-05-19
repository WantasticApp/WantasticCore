package acl

import (
	"encoding/json"
	"fmt"
	"net"
	"sync"
	"time"

	"WantasticCore/internal/cache"
	"WantasticCore/internal/store"

	"github.com/rs/zerolog/log"
)

// ACLRule represents a firewall rule for peer-to-peer communication within a tenant
type ACLRule struct {
	ID          string    `json:"id"`
	AccountID   string    `json:"account_id"`
	Name        string    `json:"name"`
	Action      string    `json:"action"`               // "allow" or "deny"
	Protocol    string    `json:"protocol"`             // "tcp", "udp", "icmp", "all"
	SourceIPs   []string  `json:"source_ips"`           // Source peer IPs or "any"
	DestIPs     []string  `json:"dest_ips"`             // Destination peer IPs or "any"
	DestPorts   []int     `json:"dest_ports,omitempty"` // For TCP/UDP
	Priority    int       `json:"priority"`             // Lower number = higher priority
	Description string    `json:"description,omitempty"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`

	// High-level intent fields (for UI/API)
	SourcePeerIDs []string `json:"source_peer_ids,omitempty"`
	DestPeerIDs   []string `json:"dest_peer_ids,omitempty"`
	Services      []string `json:"services,omitempty"`

	// HOT PATH OPTIMIZATIONS (pre-parsed for zero-alloc packet checks)
	srcIPsParsed []net.IP     `json:"-"` // Pre-parsed SourceIPs
	dstIPsParsed []net.IP     `json:"-"` // Pre-parsed DestIPs
	portBitmap   *[8192]uint8 `json:"-"` // Port bitmap for O(1) port checks (65536 bits = 8192 bytes)
	hasAnySource bool         `json:"-"` // Fast path: "any" in SourceIPs
	hasAnyDest   bool         `json:"-"` // Fast path: "any" in DestIPs
}

// ACLCacheResult represents a cached access control decision
type ACLCacheResult struct {
	Allowed  bool   `json:"allowed"`
	RuleID   string `json:"rule_id"`
	CachedAt int64  `json:"cached_at"` // Unix timestamp for cache entry age tracking
}

// ACLManager manages access control lists for tenant networks.
// Uses PostgreSQL for persistence via store.ACLRepository.
type ACLManager struct {
	rules          map[string][]*ACLRule // accountID -> rules (cached)
	loadedAccounts map[string]bool       // accountID -> true if rules are loaded
	mu             sync.RWMutex
	repo           store.ACLRepository   // PostgreSQL repository for persistence
	tenantSubnets  map[string]*net.IPNet // accountID -> tenant subnet (for validation)

	// HOT PATH INDEX: protocol+port -> rules for O(1) lookup
	// Format: accountID -> protocol -> port -> []*ACLRule
	// Special port 0 means "any port" or protocol doesn't use ports (ICMP)
	ruleIndex map[string]map[string]map[int][]*ACLRule

	// PACKET-LEVEL CACHE: TinyLFU for ultra-high frequency access decisions
	// Caches boolean results of CheckAccess calls to avoid rule evaluation
	accessCache *cache.Cache
}

// NewACLManager creates a new ACL manager with PostgreSQL persistence.
func NewACLManager(repo store.ACLRepository) *ACLManager {
	mgr := &ACLManager{
		rules:          make(map[string][]*ACLRule),
		loadedAccounts: make(map[string]bool),
		tenantSubnets:  make(map[string]*net.IPNet),
		ruleIndex:      make(map[string]map[string]map[int][]*ACLRule),
		repo:           repo,
		accessCache:    cache.NewCacheForType(cache.TypeACL), // TinyLFU for packet filtering
	}

	return mgr
}

// NewACLManagerInMemory creates an ACL manager without persistence (for testing).
func NewACLManagerInMemory() *ACLManager {
	return &ACLManager{
		rules:          make(map[string][]*ACLRule),
		loadedAccounts: make(map[string]bool),
		tenantSubnets:  make(map[string]*net.IPNet),
		ruleIndex:      make(map[string]map[string]map[int][]*ACLRule),
		accessCache:    cache.NewCacheForType(cache.TypeACL),
	}
}

// loadRules loads all ACL rules from PostgreSQL into memory cache & marks them as loaded
func (m *ACLManager) loadRules() error {
	// Skip loading if no repository configured (e.g., in tests)
	if m.repo == nil {
		return nil
	}

	// In the previous implementation, we said "load on demand".
	// But if we want to preload everything (e.g. startup), we would iterate accounts.
	// For now, let's keep it empty as we rely on lazy loading.
	log.Debug().Msg("ACL manager initialized (rules will load on demand)")
	return nil
}

// ensureRulesLoaded ensures rules for an account are loaded from DB
func (m *ACLManager) ensureRulesLoaded(accountID string) {
	if m.repo == nil {
		return
	}

	m.mu.RLock()
	loaded := m.loadedAccounts[accountID]
	m.mu.RUnlock()

	if loaded {
		return
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	// Double check inside lock
	if m.loadedAccounts[accountID] {
		return
	}

	if err := m.loadAccountRulesLocked(accountID); err != nil {
		log.Warn().Err(err).Str("account_id", accountID).Msg("Failed to load ACL rules")
	}
	m.loadedAccounts[accountID] = true
}

// loadAccountRulesLocked loads rules for a specific account from the database (caller must hold lock)
func (m *ACLManager) loadAccountRulesLocked(accountID string) error {
	if m.repo == nil {
		return nil
	}

	rules, err := m.repo.ListByAccount(accountID)
	if err != nil {
		return fmt.Errorf("failed to load rules for account %s: %w", accountID, err)
	}

	// Convert to internal ACLRule format
	aclRules := make([]*ACLRule, len(rules))
	for i, r := range rules {
		aclRules[i] = &ACLRule{
			ID:            r.ID,
			AccountID:     r.AccountID,
			Name:          r.Name,
			Action:        r.Action,
			Protocol:      r.Protocol,
			SourceIPs:     r.SourceIPs,
			DestIPs:       r.DestIPs,
			DestPorts:     r.DestPorts,
			Priority:      r.Priority,
			Description:   r.Description,
			CreatedAt:     r.CreatedAt,
			UpdatedAt:     r.UpdatedAt,
			SourcePeerIDs: r.SourcePeerIDs,
			DestPeerIDs:   r.DestPeerIDs,
			Services:      r.Services,
		}
		// Pre-parse IPs for hot path optimization
		m.optimizeRule(aclRules[i])
	}

	m.rules[accountID] = aclRules
	m.sortRules(accountID)
	m.rebuildIndex(accountID)

	log.Debug().Str("account_id", accountID).Int("rules", len(aclRules)).Msg("Loaded ACL rules from database")
	return nil
}

// loadAccountRules is a wrapper for external calls (acquires lock)
func (m *ACLManager) loadAccountRules(accountID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.loadAccountRulesLocked(accountID)
}

// countTotalRules returns the total number of rules across all accounts
func (m *ACLManager) countTotalRules() int {
	count := 0
	for _, rules := range m.rules {
		count += len(rules)
	}
	return count
}

// AddRule adds an ACL rule for a tenant
func (m *ACLManager) AddRule(rule *ACLRule) error {
	// Validate rule BEFORE locking (validation needs read lock internally)
	if err := m.validateRule(rule); err != nil {
		return fmt.Errorf("invalid ACL rule: %w", err)
	}

	// Ensure rules are loaded first to prevent overwriting or inconsistency
	m.ensureRulesLoaded(rule.AccountID)

	m.mu.Lock()
	defer m.mu.Unlock()

	// Generate ID if not provided
	if rule.ID == "" {
		accountPrefix := rule.AccountID
		if len(accountPrefix) > 8 {
			accountPrefix = accountPrefix[:8]
		}
		rule.ID = fmt.Sprintf("acl-%s-%d", accountPrefix, len(m.rules[rule.AccountID]))
	}

	// Set timestamps
	now := time.Now()
	if rule.CreatedAt.IsZero() {
		rule.CreatedAt = now
	}
	rule.UpdatedAt = now

	// Save to database
	if err := m.saveRule(rule); err != nil {
		return fmt.Errorf("failed to save rule to database: %w", err)
	}

	// PRE-PARSE IPs for hot path optimization
	m.optimizeRule(rule)

	// Add to in-memory cache
	m.rules[rule.AccountID] = append(m.rules[rule.AccountID], rule)

	// Sort by priority
	m.sortRules(rule.AccountID)

	// REBUILD FAST-PATH INDEX
	m.rebuildIndex(rule.AccountID)

	// INVALIDATE CACHE: Clear ACL decisions for this tenant since rules changed
	m.invalidateACLCache(rule.AccountID)
	return nil
}

// saveRule persists a rule to PostgreSQL
func (m *ACLManager) saveRule(rule *ACLRule) error {
	// Skip persistence if no repository configured (e.g., in tests)
	if m.repo == nil {
		return nil
	}

	data := &store.ACLRuleData{
		ID:            rule.ID,
		AccountID:     rule.AccountID,
		Name:          rule.Name,
		Action:        rule.Action,
		Protocol:      rule.Protocol,
		SourceIPs:     rule.SourceIPs,
		DestIPs:       rule.DestIPs,
		DestPorts:     rule.DestPorts,
		Priority:      rule.Priority,
		Description:   rule.Description,
		CreatedAt:     rule.CreatedAt,
		UpdatedAt:     rule.UpdatedAt,
		SourcePeerIDs: rule.SourcePeerIDs,
		DestPeerIDs:   rule.DestPeerIDs,
		Services:      rule.Services,
	}

	return m.repo.SaveRule(data)
}

// RemoveRule removes an ACL rule
func (m *ACLManager) RemoveRule(accountID, ruleID string) error {
	m.ensureRulesLoaded(accountID)

	m.mu.Lock()
	defer m.mu.Unlock()

	// Remove from in-memory cache
	rules := m.rules[accountID]
	found := false
	for i, rule := range rules {
		if rule.ID == ruleID {
			m.rules[accountID] = append(rules[:i], rules[i+1:]...)
			found = true
			break
		}
	}

	if !found {
		return fmt.Errorf("rule not found: %s", ruleID)
	}

	// Remove from database (skip if no repository configured)
	if m.repo != nil {
		if err := m.repo.DeleteRule(ruleID); err != nil {
			return fmt.Errorf("failed to delete rule from database: %w", err)
		}
	}

	// REBUILD FAST-PATH INDEX after rule removal
	m.rebuildIndex(accountID)

	// INVALIDATE CACHE: Clear ACL decisions for this tenant since rules changed
	m.invalidateACLCache(accountID)

	log.Debug().
		Str("account_id", accountID).
		Str("rule_id", ruleID).
		Msg("Removed ACL rule")

	return nil
}

// GetRules returns all ACL rules for a tenant
func (m *ACLManager) GetRules(accountID string) []*ACLRule {
	m.ensureRulesLoaded(accountID)

	m.mu.RLock()
	defer m.mu.RUnlock()

	rules := m.rules[accountID]
	result := make([]*ACLRule, len(rules))
	copy(result, rules)
	return result
}

// CheckAccess checks if a packet is allowed by ACL rules
// HOT PATH: Called for EVERY packet through TUN device - optimized for speed with TinyLFU cache
func (m *ACLManager) CheckAccess(accountID, protocol, srcIP, dstIP string, dstPort int) (bool, string) {
	// ULTRA-FAST PATH: Check cache first using TinyLFU for optimal hit rate
	cacheKey := cache.ACLCacheKey(accountID, protocol, srcIP, dstIP, dstPort)

	if m.accessCache != nil {
		if cachedVal, found := m.accessCache.Get(cacheKey); found {
			if result, ok := cachedVal.(*ACLCacheResult); ok {
				// Cache hit - return cached decision
				return result.Allowed, result.RuleID
			}
		}
	}

	// Ensure rules are loaded for this tenant (Lazy Load)
	// Only do this on cache miss (optimization)
	m.ensureRulesLoaded(accountID)

	// Cache miss - perform full rule evaluation
	allowed, ruleID := m.checkAccessUncached(accountID, protocol, srcIP, dstIP, dstPort)

	// Cache the result with TTL for fast subsequent lookups
	if m.accessCache != nil {
		result := &ACLCacheResult{
			Allowed:  allowed,
			RuleID:   ruleID,
			CachedAt: time.Now().Unix(),
		}
		m.accessCache.SetWithTTL(cacheKey, result, time.Minute*5) // 5-minute TTL for balance of freshness/performance
	}

	return allowed, ruleID
}

// checkAccessUncached performs the actual rule evaluation without cache
func (m *ACLManager) checkAccessUncached(accountID, protocol, srcIP, dstIP string, dstPort int) (bool, string) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	// FAST PATH: No rules = allow all
	if len(m.rules[accountID]) == 0 {
		return true, "default-allow"
	}

	// FAST PATH: Use index for O(1) rule lookup by protocol+port
	accountIndex := m.ruleIndex[accountID]
	if accountIndex != nil {
		protoIndex := accountIndex[protocol]
		if protoIndex != nil {
			// Pre-parse IPs once (amortized across all rule checks)
			srcIPParsed := net.ParseIP(srcIP)
			dstIPParsed := net.ParseIP(dstIP)

			// Check rules for specific port
			if rules := protoIndex[dstPort]; len(rules) > 0 {
				for _, rule := range rules {
					if m.ruleMatchesFast(rule, srcIPParsed, dstIPParsed, dstPort) {
						if rule.Action == "allow" {
							return true, rule.ID
						}
						return false, rule.ID
					}
				}
			}

			// Check rules for "any port" (port 0)
			if rules := protoIndex[0]; len(rules) > 0 {
				for _, rule := range rules {
					if m.ruleMatchesFast(rule, srcIPParsed, dstIPParsed, dstPort) {
						if rule.Action == "allow" {
							return true, rule.ID
						}
						return false, rule.ID
					}
				}
			}
		}
	}

	// FALLBACK: Index not built yet, use slow path
	for _, rule := range m.rules[accountID] {
		if m.ruleMatches(rule, protocol, srcIP, dstIP, dstPort) {
			if rule.Action == "allow" {
				return true, rule.ID
			}
			return false, rule.ID
		}
	}

	// Default deny if rules exist but none matched
	return false, "default-deny"
}

// validateRule validates an ACL rule and enforces tenant isolation
func (m *ACLManager) validateRule(rule *ACLRule) error {
	if rule.AccountID == "" {
		return fmt.Errorf("account_id is required")
	}

	if rule.Action != "allow" && rule.Action != "deny" {
		return fmt.Errorf("action must be 'allow' or 'deny'")
	}

	if rule.Protocol != "tcp" && rule.Protocol != "udp" && rule.Protocol != "icmp" && rule.Protocol != "all" && rule.Protocol != "any" {
		return fmt.Errorf("protocol must be 'tcp', 'udp', 'icmp', 'all', or 'any'")
	}

	// Validate IP addresses and check tenant isolation
	for _, ip := range rule.SourceIPs {
		if ip != "any" {
			if net.ParseIP(ip) == nil {
				return fmt.Errorf("invalid source IP: %s", ip)
			}
			// Validate IP belongs to this tenant's subnet
			if err := m.validateIPBelongsToTenant(ip, rule.AccountID); err != nil {
				return fmt.Errorf("source IP %s: %w", ip, err)
			}
		}
	}

	for _, ip := range rule.DestIPs {
		if ip != "any" {
			if net.ParseIP(ip) == nil {
				return fmt.Errorf("invalid destination IP: %s", ip)
			}
			// Validate IP belongs to this tenant's subnet
			if err := m.validateIPBelongsToTenant(ip, rule.AccountID); err != nil {
				return fmt.Errorf("destination IP %s: %w", ip, err)
			}
		}
	}

	return nil
}

// validateIPBelongsToTenant ensures an IP address belongs to the tenant's subnet
// This prevents tenants from creating ACL rules that affect other tenants' networks
func (m *ACLManager) validateIPBelongsToTenant(ip string, accountID string) error {
	// Parse the IP
	parsedIP := net.ParseIP(ip)
	if parsedIP == nil {
		return fmt.Errorf("invalid IP address format")
	}

	// Convert to IPv4
	ipv4 := parsedIP.To4()
	if ipv4 == nil {
		return fmt.Errorf("only IPv4 addresses are supported")
	}

	// Check if we have the tenant's subnet cached
	m.mu.RLock()
	tenantSubnet, exists := m.tenantSubnets[accountID]
	m.mu.RUnlock()

	if !exists {
		// No subnet registered yet - this is OK during account creation
		// Just validate it's in the overlay range
		firstOctet := ipv4[0]
		if firstOctet != 10 && firstOctet != 100 {
			return fmt.Errorf("IP must be in Wantastic.app range (10.0.0.0/8 or 100.0.0.0/16), got %s", ip)
		}

		return nil
	}

	// CRITICAL SECURITY CHECK: Verify IP is within this tenant's specific subnet
	if !tenantSubnet.Contains(parsedIP) {
		// Get subnet details for error message
		subnetStr := tenantSubnet.String()

		// Check if it might belong to another tenant
		var possibleOwner string
		m.mu.RLock()
		for otherAccountID, otherSubnet := range m.tenantSubnets {
			if otherAccountID != accountID && otherSubnet.Contains(parsedIP) {
				possibleOwner = otherAccountID
				break
			}
		}
		m.mu.RUnlock()

		if possibleOwner != "" {
			return fmt.Errorf("IP %s belongs to another tenant (%s), not allowed in rules for tenant %s (subnet: %s)",
				ip, possibleOwner[:8]+"...", accountID[:8]+"...", subnetStr)
		}

		return fmt.Errorf("IP %s is outside tenant's subnet %s (tenant: %s)",
			ip, subnetStr, accountID[:8]+"...")
	}

	return nil
}

// RegisterTenantSubnet registers a tenant's subnet for ACL validation
// This should be called when an account is created or loaded
func (m *ACLManager) RegisterTenantSubnet(accountID string, subnet string) error {
	_, ipNet, err := net.ParseCIDR(subnet)
	if err != nil {
		return fmt.Errorf("invalid subnet CIDR: %w", err)
	}

	m.mu.Lock()
	m.tenantSubnets[accountID] = ipNet
	m.mu.Unlock()

	log.Debug().
		Str("account_id", accountID).
		Str("subnet", subnet).
		Msg(" Registered tenant subnet for ACL validation")

	return nil
}

// UnregisterTenantSubnet removes a tenant's subnet registration
func (m *ACLManager) UnregisterTenantSubnet(accountID string) {
	m.mu.Lock()
	delete(m.tenantSubnets, accountID)
	m.mu.Unlock()

	log.Debug().
		Str("account_id", accountID).
		Msg("Unregistered tenant subnet")
}

// ruleMatches checks if a packet matches an ACL rule
func (m *ACLManager) ruleMatches(rule *ACLRule, protocol, srcIP, dstIP string, dstPort int) bool {
	// Check protocol
	if rule.Protocol != "all" && rule.Protocol != protocol {
		return false
	}

	// Check source IP
	if !m.ipMatches(srcIP, rule.SourceIPs) {
		return false
	}

	// Check destination IP
	if !m.ipMatches(dstIP, rule.DestIPs) {
		return false
	}

	// Check destination port (for TCP/UDP)
	if (protocol == "tcp" || protocol == "udp") && len(rule.DestPorts) > 0 {
		if !m.portMatches(dstPort, rule.DestPorts) {
			return false
		}
	}

	return true
}

// ruleMatchesFast checks if a rule matches using pre-parsed IPs (HOT PATH - zero alloc)
func (m *ACLManager) ruleMatchesFast(rule *ACLRule, srcIP, dstIP net.IP, dstPort int) bool {
	// Check source IP (fast path with pre-parsed IPs)
	if !rule.hasAnySource && len(rule.srcIPsParsed) > 0 {
		matched := false
		for _, ruleIP := range rule.srcIPsParsed {
			if ruleIP.Equal(srcIP) {
				matched = true
				break
			}
		}
		if !matched {
			return false
		}
	}

	// Check destination IP (fast path with pre-parsed IPs)
	if !rule.hasAnyDest && len(rule.dstIPsParsed) > 0 {
		matched := false
		for _, ruleIP := range rule.dstIPsParsed {
			if ruleIP.Equal(dstIP) {
				matched = true
				break
			}
		}
		if !matched {
			return false
		}
	}

	// Check destination port (use bitmap if available)
	if len(rule.DestPorts) > 0 {
		if rule.portBitmap != nil {
			// O(1) bitmap check
			byteIdx := dstPort / 8
			bitIdx := uint(dstPort % 8)
			if (rule.portBitmap[byteIdx] & (1 << bitIdx)) == 0 {
				return false
			}
		} else {
			// Fallback to linear search for small port lists
			matched := false
			for _, rulePort := range rule.DestPorts {
				if rulePort == dstPort {
					matched = true
					break
				}
			}
			if !matched {
				return false
			}
		}
	}

	return true
}

// ipMatches checks if an IP matches the rule's IP list (LEGACY - used by slow path)
func (m *ACLManager) ipMatches(ip string, ruleIPs []string) bool {
	if len(ruleIPs) == 0 {
		return true
	}

	for _, ruleIP := range ruleIPs {
		if ruleIP == "any" || ruleIP == ip {
			return true
		}
	}

	return false
}

// portMatches checks if a port matches the rule's port list (LEGACY - used by slow path)
func (m *ACLManager) portMatches(port int, rulePorts []int) bool {
	for _, rulePort := range rulePorts {
		if rulePort == port {
			return true
		}
	}
	return false
}

// optimizeRule pre-parses IPs and builds port bitmaps for hot path performance
func (m *ACLManager) optimizeRule(rule *ACLRule) {
	// Parse source IPs
	rule.srcIPsParsed = make([]net.IP, 0, len(rule.SourceIPs))
	rule.hasAnySource = false
	for _, ipStr := range rule.SourceIPs {
		if ipStr == "any" {
			rule.hasAnySource = true
			continue
		}
		if ip := net.ParseIP(ipStr); ip != nil {
			rule.srcIPsParsed = append(rule.srcIPsParsed, ip)
		}
	}

	// Parse destination IPs
	rule.dstIPsParsed = make([]net.IP, 0, len(rule.DestIPs))
	rule.hasAnyDest = false
	for _, ipStr := range rule.DestIPs {
		if ipStr == "any" {
			rule.hasAnyDest = true
			continue
		}
		if ip := net.ParseIP(ipStr); ip != nil {
			rule.dstIPsParsed = append(rule.dstIPsParsed, ip)
		}
	}

	// Build port bitmap if many ports (threshold: 10+ ports)
	if len(rule.DestPorts) >= 10 {
		rule.portBitmap = new([8192]uint8)
		for _, port := range rule.DestPorts {
			if port >= 0 && port < 65536 {
				byteIdx := port / 8
				bitIdx := uint(port % 8)
				rule.portBitmap[byteIdx] |= 1 << bitIdx
			}
		}
	}
}

// rebuildIndex rebuilds the fast-path protocol+port index for an account
func (m *ACLManager) rebuildIndex(accountID string) {
	// Initialize top-level map if needed (defensive)
	if m.ruleIndex == nil {
		m.ruleIndex = make(map[string]map[string]map[int][]*ACLRule)
	}

	// Clear and reinitialize account index
	m.ruleIndex[accountID] = make(map[string]map[int][]*ACLRule)

	// Build index for each rule
	for _, rule := range m.rules[accountID] {
		protocol := rule.Protocol
		if protocol == "all" {
			// Index under all protocols
			for _, p := range []string{"tcp", "udp", "icmp"} {
				m.indexRule(accountID, p, rule)
			}
		} else {
			m.indexRule(accountID, protocol, rule)
		}
	}
}

// indexRule adds a rule to the fast-path index
func (m *ACLManager) indexRule(accountID, protocol string, rule *ACLRule) {
	if m.ruleIndex[accountID][protocol] == nil {
		m.ruleIndex[accountID][protocol] = make(map[int][]*ACLRule)
	}

	// Index by ports for TCP/UDP
	if protocol == "tcp" || protocol == "udp" {
		if len(rule.DestPorts) == 0 {
			// No specific ports = matches any port (port 0)
			m.ruleIndex[accountID][protocol][0] = append(m.ruleIndex[accountID][protocol][0], rule)
		} else {
			// Index each port
			for _, port := range rule.DestPorts {
				m.ruleIndex[accountID][protocol][port] = append(m.ruleIndex[accountID][protocol][port], rule)
			}
		}
	} else {
		// ICMP doesn't use ports (use port 0)
		m.ruleIndex[accountID][protocol][0] = append(m.ruleIndex[accountID][protocol][0], rule)
	}
}

// sortRules sorts rules by priority (lower number = higher priority)
func (m *ACLManager) sortRules(accountID string) {
	rules := m.rules[accountID]
	// Simple bubble sort for now
	for i := 0; i < len(rules); i++ {
		for j := i + 1; j < len(rules); j++ {
			if rules[j].Priority < rules[i].Priority {
				rules[i], rules[j] = rules[j], rules[i]
			}
		}
	}
}

// invalidateACLCache removes all ACL decision cache entries for a tenant
// Called when rules are added/removed/modified to ensure cache consistency
func (m *ACLManager) invalidateACLCache(accountID string) {
	// Skip if no cache configured (e.g., in tests)
	if m.accessCache == nil {
		return
	}

	// Get cache statistics to track invalidation impact
	stats := m.accessCache.Stats()
	entriesBeforeInvalidation := stats["total_entries"].(int)

	// Since we can't do prefix-based invalidation efficiently with our current cache,
	// we'll clear the entire cache when any tenant's rules change.
	// In a real ISP deployment, we'd use a more sophisticated cache with tenant-based sharding.
	m.accessCache.Clear()

	log.Debug().
		Str("account_id", accountID).
		Int("cache_entries_cleared", entriesBeforeInvalidation).
		Msg("Invalidated ACL cache due to rule changes")
}

// hasPrefix checks if key starts with prefix.
func hasPrefix(key, prefix []byte) bool {
	if len(key) < len(prefix) {
		return false
	}
	for i := range prefix {
		if key[i] != prefix[i] {
			return false
		}
	}
	return true
}

// MarshalJSON implements custom JSON marshaling for ACLRule
func (r *ACLRule) MarshalJSON() ([]byte, error) {
	type Alias ACLRule
	return json.Marshal(&struct {
		*Alias
	}{
		Alias: (*Alias)(r),
	})
}
