package core

// ServerBackend is an interface covering the subset of server.Server methods
// used by TenantPortalServiceServer. Defining it as an interface allows test
// doubles to be injected without starting a full server.
//
// *server.Server satisfies this interface — no production behaviour changes.

import (
	"time"

	"WantasticCore/internal/account"
	"WantasticCore/internal/mikrotik"
	"WantasticCore/internal/server"
	"WantasticCore/internal/store"
	webssh "WantasticCore/internal/webssh"
	"WantasticCore/internal/wg/userspace"

	"github.com/redis/go-redis/v9"
)

type ServerBackend interface {
	// Account management
	CreateAccount(name string, maxPeers int) (*account.Account, error)
	DeleteAccount(accountID string) error
	GetAccount(accountID string) (*account.Account, error)
	SetAccountMaxPeers(accountID string, maxPeers int) (*account.Account, error)
	SetPeerLimitOverride(accountID string, limit int) error
	GetAccountPeerStats(accountID string) (current, max int, err error)

	// Peer management
	AddPeer(accountID, peerName string, assignedIP string) (*server.PeerInfo, error)
	AddPeerWithKey(accountID, peerName, assignedIP, publicKey string) (*server.PeerInfo, error)
	RemovePeer(accountID string, publicKey string) error
	UpdatePeer(peer *server.PeerMetadata) error
	GetPeer(accountID, peerID string) (*server.PeerMetadata, error)
	FindPeer(peerID string) (*server.PeerMetadata, error)
	ListPeers(accountID string) ([]*server.PeerMetadata, error)
	UpdatePeerStatus(accountID, peerID string) (bool, error)
	GetPeerConfig(accountID, peerID, endpoint string) (string, error)
	PingPeer(accountID, peerID string, count, timeoutMs int) (*userspace.PingResult, error)
	ResolvePeerDevice(accountID, peerID string) (*userspace.TenantDevice, string, error)
	GetHandshakeHistory(accountID, peerID string, since time.Time) ([]store.PeerHandshakeData, error)
	GetPeerScanResult(peerID string) (*userspace.ScanResult, error)
	GetPeerStore() *server.PeerStore
	GetActiveScanID(accountID, peerID string) string
	IsScanInProgress(accountID, peerID string) bool
	GetTenantDevice(accountID string) (*userspace.TenantDevice, error)
	GetServerPublicKey(accountID string) string

	// ACL
	GetACLRules(accountID string) []*server.ACLRule
	AddACLRule(rule *server.ACLRule) error
	RemoveACLRule(accountID, ruleID string) error
	CompileGroups(accountID string) ([]*server.ACLRule, error)
	GetCompilationStats(accountID string) map[string]any

	// Peer groups
	CreatePeerGroup(accountID, groupID, name, description string, protocols []uint8) (*server.PeerGroup, error)
	DeletePeerGroup(accountID, groupID string) error
	ListPeerGroups(accountID string) []*server.PeerGroup
	TopologyPeerLabels(accountID string, peer *server.PeerMetadata) []string
	AddPeerToGroup(accountID, peerID, groupID string) error
	RemovePeerFromGroup(accountID, peerID, groupID string) error
	CreateGroupLink(accountID, linkID, srcGroupID, dstGroupID, action string, protocols []uint8, portRanges []server.PortRange) (*server.GroupLink, error)
	DeleteGroupLink(accountID, linkID string) error
	ListGroupLinks(accountID string) []*server.GroupLink

	// WebSSH
	CreateWebSSHSession(tenantID, peerID, peerIP string, sshPort int, username, password, privateKey, privateKeyPassphrase, userAgent string, rows, cols int) (string, error)
	GetWebSSHSession(sessionID string) (*webssh.DirectSSHSession, error)
	ListWebSSHSessions(tenantID string) ([]*webssh.DirectSSHSession, error)
	DisconnectWebSSHSession(sessionID string) error
	RegisterWebSSHSession(tenantID, sessionID string) error
	UnregisterWebSSHSession(tenantID, sessionID string)
	CountWebSSHSessions(tenantID string) (int, error)

	// Winbox / infra
	GetWinboxManager() *mikrotik.WinboxManager
	GetRedisClient() *redis.Client
	GetServerEndpoint() string
}
