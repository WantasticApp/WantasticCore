// Package cache provides cache integration for various WantasticCore components
package cache

import (
	"fmt"
	"time"

	"github.com/rs/zerolog/log"
)

// CacheManager manages multiple cache instances for different use cases
type CacheManager struct {
	// Individual cache instances
	aclCache     *Cache
	peerCache    *Cache
	sessionCache *Cache
	configCache  *Cache
	metricsCache *Cache
}

// NewCacheManager creates a new cache manager with optimized caches for each use case
func NewCacheManager() *CacheManager {
	return &CacheManager{
		aclCache:     NewCacheForType(TypeACL),
		peerCache:    NewCacheForType(TypePeer),
		sessionCache: NewCacheForType(TypeSession),
		configCache:  NewCacheForType(TypeConfig),
		metricsCache: NewCacheForType(TypeMetrics),
	}
}

// ACLCacheKey builds a cache key for ACL decisions
func ACLCacheKey(tenantID, protocol, srcIP, dstIP string, dstPort int) string {
	return fmt.Sprintf("acl:%s:%s:%s:%s:%d", tenantID, protocol, srcIP, dstIP, dstPort)
}

// CheckACLCache retrieves a cached ACL decision
func (cm *CacheManager) CheckACLCache(tenantID, protocol, srcIP, dstIP string, dstPort int) (allowed bool, ruleID string, found bool) {
	key := ACLCacheKey(tenantID, protocol, srcIP, dstIP, dstPort)

	if value, exists := cm.aclCache.Get(key); exists {
		if decision, ok := value.(ACLDecision); ok {
			log.Debug().
				Str("key", key).
				Bool("allowed", decision.Allowed).
				Str("rule_id", decision.RuleID).
				Msg("ACL cache hit")
			return decision.Allowed, decision.RuleID, true
		}
	}

	return false, "", false
}

// CacheACLDecision stores an ACL decision in cache
func (cm *CacheManager) CacheACLDecision(tenantID, protocol, srcIP, dstIP string, dstPort int, allowed bool, ruleID string) {
	key := ACLCacheKey(tenantID, protocol, srcIP, dstIP, dstPort)
	decision := ACLDecision{
		Allowed:   allowed,
		RuleID:    ruleID,
		CheckedAt: time.Now(),
	}

	// Cache ACL decisions for 10 minutes (rules change infrequently)
	cm.aclCache.SetWithTTL(key, decision, 10*time.Minute)

	log.Debug().
		Str("key", key).
		Bool("allowed", allowed).
		Str("rule_id", ruleID).
		Msg("ACL decision cached")
}

// PeerCacheKey builds a cache key for peer information
func PeerCacheKey(accountID, peerID string) string {
	return fmt.Sprintf("peer:%s:%s", accountID, peerID)
}

// GetPeerCache retrieves cached peer information
func (cm *CacheManager) GetPeerCache(accountID, peerID string) (peer *CachedPeer, found bool) {
	key := PeerCacheKey(accountID, peerID)

	if value, exists := cm.peerCache.Get(key); exists {
		if cachedPeer, ok := value.(*CachedPeer); ok {
			return cachedPeer, true
		}
	}

	return nil, false
}

// CachePeer stores peer information in cache
func (cm *CacheManager) CachePeer(accountID, peerID string, peer *CachedPeer) {
	key := PeerCacheKey(accountID, peerID)
	peer.CachedAt = time.Now()

	// Cache peer data for 5 minutes (peer status changes frequently)
	cm.peerCache.SetWithTTL(key, peer, 5*time.Minute)
}

// SessionCacheKey builds a cache key for session information
func SessionCacheKey(sessionType, sessionID string) string {
	return fmt.Sprintf("session:%s:%s", sessionType, sessionID)
}

// GetSessionCache retrieves cached session information
func (cm *CacheManager) GetSessionCache(sessionType, sessionID string) (session any, found bool) {
	key := SessionCacheKey(sessionType, sessionID)
	return cm.sessionCache.Get(key)
}

// CacheSession stores session information in cache
func (cm *CacheManager) CacheSession(sessionType, sessionID string, session any, ttl time.Duration) {
	key := SessionCacheKey(sessionType, sessionID)
	cm.sessionCache.SetWithTTL(key, session, ttl)
}

// InvalidateSession removes a session from cache
func (cm *CacheManager) InvalidateSession(sessionType, sessionID string) {
	key := SessionCacheKey(sessionType, sessionID)
	cm.sessionCache.Delete(key)
}

// GetStats returns comprehensive cache statistics
func (cm *CacheManager) GetStats() map[string]any {
	return map[string]any{
		"acl_cache":     cm.aclCache.Stats(),
		"peer_cache":    cm.peerCache.Stats(),
		"session_cache": cm.sessionCache.Stats(),
		"config_cache":  cm.configCache.Stats(),
		"metrics_cache": cm.metricsCache.Stats(),
	}
}

// Close shuts down all cache instances
func (cm *CacheManager) Close() {
	cm.aclCache.Close()
	cm.peerCache.Close()
	cm.sessionCache.Close()
	cm.configCache.Close()
	cm.metricsCache.Close()
}

// Data structures for cached information

// ACLDecision represents a cached ACL decision
type ACLDecision struct {
	Allowed   bool      `json:"allowed"`
	RuleID    string    `json:"rule_id"`
	CheckedAt time.Time `json:"checked_at"`
}

// CachedPeer represents cached peer information
type CachedPeer struct {
	ID            string    `json:"id"`
	AccountID     string    `json:"account_id"`
	Name          string    `json:"name"`
	PublicKey     string    `json:"public_key"`
	AssignedIP    string    `json:"assigned_ip"`
	IsOnline      bool      `json:"is_online"`
	LastHandshake time.Time `json:"last_handshake"`
	RxBytes       int64     `json:"rx_bytes"`
	TxBytes       int64     `json:"tx_bytes"`

	// Winbox information
	HasWinbox              bool      `json:"has_winbox"`
	RouterIP               string    `json:"router_ip"`
	VirtualWinboxUsername  string    `json:"virtual_winbox_username"`
	WinboxCredentialsValid bool      `json:"winbox_credentials_valid"`
	WinboxLastProbed       time.Time `json:"winbox_last_probed"`
	WinboxCredentialError  string    `json:"winbox_credential_error"`

	// Cache metadata
	CachedAt time.Time `json:"cached_at"`
}

// CachedSession represents cached session information
type CachedSession struct {
	SessionID   string         `json:"session_id"`
	UserID      string         `json:"user_id"`
	TenantID    string         `json:"tenant_id"`
	CreatedAt   time.Time      `json:"created_at"`
	ExpiresAt   time.Time      `json:"expires_at"`
	IsActive    bool           `json:"is_active"`
	SessionData map[string]any `json:"session_data"`

	// Cache metadata
	CachedAt time.Time `json:"cached_at"`
}

// Global cache manager instance
var GlobalCacheManager *CacheManager

// InitializeGlobalCache initializes the global cache manager
func InitializeGlobalCache() {
	GlobalCacheManager = NewCacheManager()
	log.Debug().Msg(" Global cache manager initialized with optimized algorithms")
}
