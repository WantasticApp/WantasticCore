// Package store provides the unified storage layer interfaces and singleton manager.
// All storage operations go through this package to ensure:
// 1. No circular imports (this package has no internal dependencies)
// 2. Singleton database connection management
// 3. Optional Redis caching
// 4. Proper database migrations
package store

import "time"

// =============================================================================
// Account Repository
// =============================================================================

// AccountRepository defines account storage operations.
type AccountRepository interface {
	Create(acc *AccountData) error
	Get(id string) (*AccountData, error)
	Update(acc *AccountData) error
	Delete(id string) error
	List() ([]*AccountData, error)
}

// =============================================================================
// Tenant Repository
// =============================================================================

// TenantRepository defines tenant storage operations.
type TenantRepository interface {
	Create(t *TenantData) error
	Get(id string) (*TenantData, error)
	GetByEmail(email string) (*TenantData, error)
	GetByOverlayAccount(overlayAccountID string) (*TenantData, error)
	// GetByAuth0Sub returns the tenant with the given Auth0 subject identifier.
	GetByAuth0Sub(auth0Sub string) (*TenantData, error)
	Update(t *TenantData) error
	Delete(id string) error
	List() ([]*TenantData, error)

	// 2FA
	SetTwoFAMethod(tenantID, method string, totpSecret string) error
	GetActiveTwoFAMethod(tenantID string) (string, error)
	IsTwoFAEnabled(tenantID string) (bool, error)
	SetPending2FACode(tenantID, code string, expiresIn time.Duration) error
	Verify2FACode(tenantID, code string) (valid, expired, maxAttempts bool, err error)
	Clear2FACode(tenantID string) error

	// Status management
	SetOverlayAccountID(tenantID, overlayAccountID string, networks []string) error
	UpdateLastLogin(tenantID string) error
	SetStatus(tenantID, status string) error
	UpdatePassword(tenantID, passwordHash string) error
}

// =============================================================================
// Session Repository
// =============================================================================

// SessionRepository defines session storage operations.
type SessionRepository interface {
	Create(session *SessionData) error
	Get(sessionID string) (*SessionData, error)
	Validate(sessionID string) (tenantID string, err error)
	Delete(sessionID string) error
	DeleteByTenant(tenantID string) error
	ListByTenant(tenantID string) ([]*SessionData, error)
	UpdateActivity(sessionID string) error
	HasTrustedDevice(tenantID, deviceHash string) bool
	CleanupExpired() (count int, err error)

	// Deleted shared session support methods
}

// =============================================================================
// Peer Repository
// =============================================================================

// PeerRepository defines peer storage operations.
type PeerRepository interface {
	Save(peer *PeerData) error
	Get(accountID, peerID string) (*PeerData, error)
	FindByPeerID(peerID string) (*PeerData, error)
	List(accountID string) ([]*PeerData, error)
	// Count returns the number of peers for an account directly from the DB,
	// bypassing any cache layer. Used for authoritative limit enforcement.
	Count(accountID string) (int, error)
	Delete(accountID, peerID string) error
	DeleteByAccount(accountID string) error

	// Winbox sessions
	SaveWinboxSession(accountID, peerID string, session *WinboxSessionData) error
	GetWinboxSession(sessionID string) (*WinboxSessionData, error)
	ListWinboxSessions(accountID, peerID string) ([]*WinboxSessionData, error)
	ListAllWinboxSessions(accountID string) ([]*WinboxSessionData, error)
	DeleteWinboxSession(accountID, peerID, sessionID string) error
	ClearWinboxSessions(accountID, peerID string) error
	LookupWinboxByToken(accessToken string) (*WinboxLookupResult, error)

	// WebSSH sessions
	SaveWebSSHSession(accountID, peerID string, session *WebSSHSessionData) error
	GetWebSSHSession(sessionID string) (*WebSSHSessionData, error)
	ListWebSSHSessions(accountID, peerID string) ([]*WebSSHSessionData, error)
	ListAllWebSSHSessions(accountID string) ([]*WebSSHSessionData, error)
	DeleteWebSSHSession(accountID, peerID, sessionID string) error

	// Activity logging
	LogSSHActivity(accountID, peerID string, activity *SSHActivityData) error
	UpdateSSHActivity(accountID, peerID, sessionID string, update func(*SSHActivityData)) error
	LogWinboxActivity(accountID, peerID string, activity *WinboxActivityData) error
	UpdateWinboxActivity(accountID, peerID string, sessionName string, timestamp time.Time, update func(*WinboxActivityData)) error
	ListSSHActivities(accountID, peerID string, limit int) ([]*SSHActivityData, error)
	ListWinboxActivities(accountID, peerID string, limit int) ([]*WinboxActivityData, error)

	// Handshake tracking
	RecordHandshake(accountID, peerID string, timestamp time.Time, endpoint string) error
	GetHandshakeHistory(accountID, peerID string, since time.Time) ([]*PeerHandshakeData, error)
	UpdatePeerStatus(accountID, peerID string, lastHandshake time.Time, endpoint string, isOnline bool) error
	UpdatePeerScanResults(accountID, peerID string, lastScan time.Time, scanJSON []byte, openPorts []OpenPortData, fingerprint *OSFingerprintData) error
	UpdatePeerNotes(accountID, peerID, notes string) error
	// UpdatePeerAgentInfo persists agent model and version discovered from stats messages.
	// agentModel should be a stable software family identifier (e.g. "wantasticd").
	// agentVersion is the agent's build version string.
	UpdatePeerAgentInfo(accountID, peerID, agentModel, agentVersion string) error
}

// =============================================================================
// Group Repository (for ACL)
// =============================================================================

// GroupRepository defines peer group storage operations.
type GroupRepository interface {
	SaveGroup(group *GroupData) error
	GetGroup(groupID string) (*GroupData, error)
	ListByAccount(accountID string) ([]*GroupData, error)
	ListAll() ([]*GroupData, error)
	DeleteGroup(groupID string) error

	// Peer-Group membership
	AddPeerToGroup(groupID, peerID string) error
	RemovePeerFromGroup(groupID, peerID string) error
	GetPeerGroups(peerID string) ([]string, error)
	GetGroupPeers(groupID string) ([]string, error)

	// Group links
	SaveLink(link *GroupLinkData) error
	GetLink(linkID string) (*GroupLinkData, error)
	ListLinksByAccount(accountID string) ([]*GroupLinkData, error)
	ListAllLinks() ([]*GroupLinkData, error)
	DeleteLink(linkID string) error
}

// =============================================================================
// ACL Repository
// =============================================================================

// ACLRepository defines ACL rule storage operations.
type ACLRepository interface {
	SaveRule(rule *ACLRuleData) error
	GetRule(ruleID string) (*ACLRuleData, error)
	ListByAccount(accountID string) ([]*ACLRuleData, error)
	DeleteRule(ruleID string) error
}

// =============================================================================
// IPAM Repository
// =============================================================================

// IPAMRepository defines IP block allocation storage operations.
type IPAMRepository interface {
	UpsertBlock(block *IPAMBlockData) error
	ListBlocks() ([]*IPAMBlockData, error)
	GetBlock(cidr string) (*IPAMBlockData, error)
	AllocateBlocks(tenantID string, poolIndex int, count int) ([]string, error)
	ReleaseBlocks(tenantID string) error
	RestoreState() ([]*IPAMBlockData, error)
}

// =============================================================================
// WUSP Device State Repository
// =============================================================================

// WUSPDeviceStateRepository persists WUSP device model snapshots per peer.
// It is consumed directly by the WUSP controller and its gRPC/server callers.
type WUSPDeviceStateRepository interface {
	Upsert(state *WUSPDeviceStateData) error
	GetByPeer(peerID string) (*WUSPDeviceStateData, error)
	GetByAccount(accountID string) ([]*WUSPDeviceStateData, error)
	Delete(peerID string) error
}

// =============================================================================
// Device Snapshot Repository
// =============================================================================

// DeviceSnapshotRepository stores named, tenant-scoped device configuration
// snapshots. Snapshots are protocol-tagged (e.g. "wusp") so the same table
// can serve future device management protocols.
type DeviceSnapshotRepository interface {
	Create(snap *DeviceSnapshotData) error
	Get(id, accountID string) (*DeviceSnapshotData, error)
	GetByUploadToken(token string) (*DeviceSnapshotData, error)
	List(accountID string) ([]*DeviceSnapshotData, error)
	ListByProtocol(accountID, protocol string) ([]*DeviceSnapshotData, error)
	Update(snap *DeviceSnapshotData) error
	Delete(id, accountID string) error
}
