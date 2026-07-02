// Code generated from overlay.proto. DO NOT EDIT.
// This file replaces api/proto/overlay.pb.go with pure-Go DTO types —
// no protobuf runtime, no reflection. Hand-style accessors so existing
// call sites that use Get*() keep working unchanged.
package types

import "time"

// Timestamp / Duration / Empty stand in for the corresponding
// google.protobuf well-known types. Only the fields the codebase
// reads are exposed.
type Timestamp struct {
	Seconds int64
	Nanos   int32
}
func (t *Timestamp) AsTime() time.Time {
	if t == nil { return time.Time{} }
	return time.Unix(t.Seconds, int64(t.Nanos)).UTC()
}
func TimestampNow() *Timestamp { return TimestampFromTime(time.Now()) }
func TimestampFromTime(t time.Time) *Timestamp {
	return &Timestamp{Seconds: t.Unix(), Nanos: int32(t.Nanosecond())}
}

type Duration struct {
	Seconds int64
	Nanos   int32
}
func (d *Duration) AsDuration() time.Duration {
	if d == nil { return 0 }
	return time.Duration(d.Seconds)*time.Second + time.Duration(d.Nanos)*time.Nanosecond
}

type Empty struct{}

type AccountTier int32
const (
	AccountTier_TIER_FREE AccountTier = 0
	AccountTier_TIER_STANDARD AccountTier = 1
	AccountTier_TIER_PREMIUM AccountTier = 2
	AccountTier_TIER_ENTERPRISE AccountTier = 3
)
func (x AccountTier) String() string {
	switch x {
	case AccountTier_TIER_FREE: return "TIER_FREE"
	case AccountTier_TIER_STANDARD: return "TIER_STANDARD"
	case AccountTier_TIER_PREMIUM: return "TIER_PREMIUM"
	case AccountTier_TIER_ENTERPRISE: return "TIER_ENTERPRISE"
	}
	return "UNKNOWN"
}

type UserType int32
const (
	UserType_USER_TYPE_UNKNOWN UserType = 0
	UserType_USER_TYPE_ADMIN UserType = 1
	UserType_USER_TYPE_TENANT UserType = 2
)
func (x UserType) String() string {
	switch x {
	case UserType_USER_TYPE_UNKNOWN: return "USER_TYPE_UNKNOWN"
	case UserType_USER_TYPE_ADMIN: return "USER_TYPE_ADMIN"
	case UserType_USER_TYPE_TENANT: return "USER_TYPE_TENANT"
	}
	return "UNKNOWN"
}

type NodeType int32
const (
	NodeType_NODE_TYPE_UNKNOWN NodeType = 0
	NodeType_NODE_TYPE_SERVER NodeType = 1
	NodeType_NODE_TYPE_PEER NodeType = 2
	NodeType_NODE_TYPE_ROUTER NodeType = 3
	NodeType_NODE_TYPE_GLOBAL_SERVER NodeType = 4
)
func (x NodeType) String() string {
	switch x {
	case NodeType_NODE_TYPE_UNKNOWN: return "NODE_TYPE_UNKNOWN"
	case NodeType_NODE_TYPE_SERVER: return "NODE_TYPE_SERVER"
	case NodeType_NODE_TYPE_PEER: return "NODE_TYPE_PEER"
	case NodeType_NODE_TYPE_ROUTER: return "NODE_TYPE_ROUTER"
	case NodeType_NODE_TYPE_GLOBAL_SERVER: return "NODE_TYPE_GLOBAL_SERVER"
	}
	return "UNKNOWN"
}

type NodeStatus int32
const (
	NodeStatus_NODE_STATUS_UNKNOWN NodeStatus = 0
	NodeStatus_NODE_STATUS_ONLINE NodeStatus = 1
	NodeStatus_NODE_STATUS_IDLE NodeStatus = 2
	NodeStatus_NODE_STATUS_OFFLINE NodeStatus = 3
	NodeStatus_NODE_STATUS_ERROR NodeStatus = 4
	NodeStatus_NODE_STATUS_INACTIVE NodeStatus = 5
)
func (x NodeStatus) String() string {
	switch x {
	case NodeStatus_NODE_STATUS_UNKNOWN: return "NODE_STATUS_UNKNOWN"
	case NodeStatus_NODE_STATUS_ONLINE: return "NODE_STATUS_ONLINE"
	case NodeStatus_NODE_STATUS_IDLE: return "NODE_STATUS_IDLE"
	case NodeStatus_NODE_STATUS_OFFLINE: return "NODE_STATUS_OFFLINE"
	case NodeStatus_NODE_STATUS_ERROR: return "NODE_STATUS_ERROR"
	case NodeStatus_NODE_STATUS_INACTIVE: return "NODE_STATUS_INACTIVE"
	}
	return "UNKNOWN"
}

type EdgeDirection int32
const (
	EdgeDirection_EDGE_DIRECTION_NONE EdgeDirection = 0
	EdgeDirection_EDGE_DIRECTION_FORWARD EdgeDirection = 1
	EdgeDirection_EDGE_DIRECTION_BACKWARD EdgeDirection = 2
	EdgeDirection_EDGE_DIRECTION_BOTH EdgeDirection = 3
)
func (x EdgeDirection) String() string {
	switch x {
	case EdgeDirection_EDGE_DIRECTION_NONE: return "EDGE_DIRECTION_NONE"
	case EdgeDirection_EDGE_DIRECTION_FORWARD: return "EDGE_DIRECTION_FORWARD"
	case EdgeDirection_EDGE_DIRECTION_BACKWARD: return "EDGE_DIRECTION_BACKWARD"
	case EdgeDirection_EDGE_DIRECTION_BOTH: return "EDGE_DIRECTION_BOTH"
	}
	return "UNKNOWN"
}

type EdgeType int32
const (
	EdgeType_EDGE_TYPE_UNKNOWN EdgeType = 0
	EdgeType_EDGE_TYPE_PEER_TO_SERVER EdgeType = 1
	EdgeType_EDGE_TYPE_PEER_TO_PEER EdgeType = 2
	EdgeType_EDGE_TYPE_TENANT_TO_GLOBAL EdgeType = 3
)
func (x EdgeType) String() string {
	switch x {
	case EdgeType_EDGE_TYPE_UNKNOWN: return "EDGE_TYPE_UNKNOWN"
	case EdgeType_EDGE_TYPE_PEER_TO_SERVER: return "EDGE_TYPE_PEER_TO_SERVER"
	case EdgeType_EDGE_TYPE_PEER_TO_PEER: return "EDGE_TYPE_PEER_TO_PEER"
	case EdgeType_EDGE_TYPE_TENANT_TO_GLOBAL: return "EDGE_TYPE_TENANT_TO_GLOBAL"
	}
	return "UNKNOWN"
}

type HealthCheckResponse_Status int32
const (
	HealthCheckResponse_UNKNOWN HealthCheckResponse_Status = 0
	HealthCheckResponse_HEALTHY HealthCheckResponse_Status = 1
	HealthCheckResponse_DEGRADED HealthCheckResponse_Status = 2
	HealthCheckResponse_UNHEALTHY HealthCheckResponse_Status = 3
)
func (x HealthCheckResponse_Status) String() string {
	switch x {
	case HealthCheckResponse_UNKNOWN: return "UNKNOWN"
	case HealthCheckResponse_HEALTHY: return "HEALTHY"
	case HealthCheckResponse_DEGRADED: return "DEGRADED"
	case HealthCheckResponse_UNHEALTHY: return "UNHEALTHY"
	}
	return "UNKNOWN"
}

type BatchUpdatePeersRequest_Operation int32
const (
	BatchUpdatePeersRequest_OP_UNKNOWN BatchUpdatePeersRequest_Operation = 0
	BatchUpdatePeersRequest_DELETE BatchUpdatePeersRequest_Operation = 1
	BatchUpdatePeersRequest_RENAME_SEQUENCE BatchUpdatePeersRequest_Operation = 2
	BatchUpdatePeersRequest_ADD_TAGS BatchUpdatePeersRequest_Operation = 3
	BatchUpdatePeersRequest_REMOVE_TAGS BatchUpdatePeersRequest_Operation = 4
)
func (x BatchUpdatePeersRequest_Operation) String() string {
	switch x {
	case BatchUpdatePeersRequest_OP_UNKNOWN: return "OP_UNKNOWN"
	case BatchUpdatePeersRequest_DELETE: return "DELETE"
	case BatchUpdatePeersRequest_RENAME_SEQUENCE: return "RENAME_SEQUENCE"
	case BatchUpdatePeersRequest_ADD_TAGS: return "ADD_TAGS"
	case BatchUpdatePeersRequest_REMOVE_TAGS: return "REMOVE_TAGS"
	}
	return "UNKNOWN"
}

type SessionType int32
const (
	SessionType_SESSION_TYPE_UNSPECIFIED SessionType = 0
	SessionType_SESSION_TYPE_WEBSSH SessionType = 1
	SessionType_SESSION_TYPE_WINBOX SessionType = 2
)
func (x SessionType) String() string {
	switch x {
	case SessionType_SESSION_TYPE_UNSPECIFIED: return "SESSION_TYPE_UNSPECIFIED"
	case SessionType_SESSION_TYPE_WEBSSH: return "SESSION_TYPE_WEBSSH"
	case SessionType_SESSION_TYPE_WINBOX: return "SESSION_TYPE_WINBOX"
	}
	return "UNKNOWN"
}

type ActivityEventType int32
const (
	ActivityEventType_ACTIVITY_EVENT_UNSPECIFIED ActivityEventType = 0
	ActivityEventType_ACTIVITY_EVENT_SESSION_START ActivityEventType = 1
	ActivityEventType_ACTIVITY_EVENT_SESSION_END ActivityEventType = 2
	ActivityEventType_ACTIVITY_EVENT_COMMAND ActivityEventType = 3
	ActivityEventType_ACTIVITY_EVENT_MESSAGE ActivityEventType = 4
	ActivityEventType_ACTIVITY_EVENT_AUTH_SUCCESS ActivityEventType = 5
	ActivityEventType_ACTIVITY_EVENT_AUTH_FAILURE ActivityEventType = 6
)
func (x ActivityEventType) String() string {
	switch x {
	case ActivityEventType_ACTIVITY_EVENT_UNSPECIFIED: return "ACTIVITY_EVENT_UNSPECIFIED"
	case ActivityEventType_ACTIVITY_EVENT_SESSION_START: return "ACTIVITY_EVENT_SESSION_START"
	case ActivityEventType_ACTIVITY_EVENT_SESSION_END: return "ACTIVITY_EVENT_SESSION_END"
	case ActivityEventType_ACTIVITY_EVENT_COMMAND: return "ACTIVITY_EVENT_COMMAND"
	case ActivityEventType_ACTIVITY_EVENT_MESSAGE: return "ACTIVITY_EVENT_MESSAGE"
	case ActivityEventType_ACTIVITY_EVENT_AUTH_SUCCESS: return "ACTIVITY_EVENT_AUTH_SUCCESS"
	case ActivityEventType_ACTIVITY_EVENT_AUTH_FAILURE: return "ACTIVITY_EVENT_AUTH_FAILURE"
	}
	return "UNKNOWN"
}

type RouterOSResource int32
const (
	RouterOSResource_ROUTEROS_RESOURCE_UNKNOWN RouterOSResource = 0
	RouterOSResource_ROUTEROS_RESOURCE_IP_ADDRESSES RouterOSResource = 1
	RouterOSResource_ROUTEROS_RESOURCE_ROUTES RouterOSResource = 2
	RouterOSResource_ROUTEROS_RESOURCE_FIREWALL RouterOSResource = 3
	RouterOSResource_ROUTEROS_RESOURCE_PACKAGES RouterOSResource = 4
	RouterOSResource_ROUTEROS_RESOURCE_FILES RouterOSResource = 5
	RouterOSResource_ROUTEROS_RESOURCE_WIRELESS RouterOSResource = 6
	RouterOSResource_ROUTEROS_RESOURCE_TR069_CLIENT RouterOSResource = 7
	RouterOSResource_ROUTEROS_RESOURCE_BRIDGE RouterOSResource = 8
)
func (x RouterOSResource) String() string {
	switch x {
	case RouterOSResource_ROUTEROS_RESOURCE_UNKNOWN: return "ROUTEROS_RESOURCE_UNKNOWN"
	case RouterOSResource_ROUTEROS_RESOURCE_IP_ADDRESSES: return "ROUTEROS_RESOURCE_IP_ADDRESSES"
	case RouterOSResource_ROUTEROS_RESOURCE_ROUTES: return "ROUTEROS_RESOURCE_ROUTES"
	case RouterOSResource_ROUTEROS_RESOURCE_FIREWALL: return "ROUTEROS_RESOURCE_FIREWALL"
	case RouterOSResource_ROUTEROS_RESOURCE_PACKAGES: return "ROUTEROS_RESOURCE_PACKAGES"
	case RouterOSResource_ROUTEROS_RESOURCE_FILES: return "ROUTEROS_RESOURCE_FILES"
	case RouterOSResource_ROUTEROS_RESOURCE_WIRELESS: return "ROUTEROS_RESOURCE_WIRELESS"
	case RouterOSResource_ROUTEROS_RESOURCE_TR069_CLIENT: return "ROUTEROS_RESOURCE_TR069_CLIENT"
	case RouterOSResource_ROUTEROS_RESOURCE_BRIDGE: return "ROUTEROS_RESOURCE_BRIDGE"
	}
	return "UNKNOWN"
}

type CreateAccountRequest struct {
	Name string `json:"name,omitempty"`
	Tier AccountTier `json:"tier,omitempty"`
}

func (x *CreateAccountRequest) GetName() string {
	if x == nil { return "" }
	return x.Name
}
func (x *CreateAccountRequest) GetTier() AccountTier {
	if x == nil { return AccountTier(0) }
	return x.Tier
}

type CreateAccountResponse struct {
	Account *Account `json:"account,omitempty"`
	ApiKey string `json:"api_key,omitempty"`
}

func (x *CreateAccountResponse) GetAccount() *Account {
	if x == nil { return nil }
	return x.Account
}
func (x *CreateAccountResponse) GetApiKey() string {
	if x == nil { return "" }
	return x.ApiKey
}

type GetAccountRequest struct {
	AccountId string `json:"account_id,omitempty"`
}

func (x *GetAccountRequest) GetAccountId() string {
	if x == nil { return "" }
	return x.AccountId
}

type GetAccountResponse struct {
	Account *Account `json:"account,omitempty"`
}

func (x *GetAccountResponse) GetAccount() *Account {
	if x == nil { return nil }
	return x.Account
}

type ListAccountsRequest struct {
	PageSize int32 `json:"page_size,omitempty"`
	PageToken string `json:"page_token,omitempty"`
}

func (x *ListAccountsRequest) GetPageSize() int32 {
	if x == nil { return 0 }
	return x.PageSize
}
func (x *ListAccountsRequest) GetPageToken() string {
	if x == nil { return "" }
	return x.PageToken
}

type ListAccountsResponse struct {
	Accounts []*Account `json:"accounts,omitempty"`
	NextPageToken string `json:"next_page_token,omitempty"`
	TotalCount int32 `json:"total_count,omitempty"`
}

func (x *ListAccountsResponse) GetAccounts() []*Account {
	if x == nil { return nil }
	return x.Accounts
}
func (x *ListAccountsResponse) GetNextPageToken() string {
	if x == nil { return "" }
	return x.NextPageToken
}
func (x *ListAccountsResponse) GetTotalCount() int32 {
	if x == nil { return 0 }
	return x.TotalCount
}

type DeleteAccountRequest struct {
	AccountId string `json:"account_id,omitempty"`
}

func (x *DeleteAccountRequest) GetAccountId() string {
	if x == nil { return "" }
	return x.AccountId
}

type DeleteAccountResponse struct {
	Success bool `json:"success"`
}

func (x *DeleteAccountResponse) GetSuccess() bool {
	if x == nil { return false }
	return x.Success
}

type UpdateAccountQuotasRequest struct {
	AccountId string `json:"account_id,omitempty"`
	MaxNetworks int32 `json:"max_networks,omitempty"`
	MaxPeersPerNetwork int32 `json:"max_peers_per_network,omitempty"`
}

func (x *UpdateAccountQuotasRequest) GetAccountId() string {
	if x == nil { return "" }
	return x.AccountId
}
func (x *UpdateAccountQuotasRequest) GetMaxNetworks() int32 {
	if x == nil { return 0 }
	return x.MaxNetworks
}
func (x *UpdateAccountQuotasRequest) GetMaxPeersPerNetwork() int32 {
	if x == nil { return 0 }
	return x.MaxPeersPerNetwork
}

type UpdateAccountQuotasResponse struct {
	Account *Account `json:"account,omitempty"`
}

func (x *UpdateAccountQuotasResponse) GetAccount() *Account {
	if x == nil { return nil }
	return x.Account
}

type UpdateAccountTierRequest struct {
	AccountId string `json:"account_id,omitempty"`
	Tier int32 `json:"tier,omitempty"`
}

func (x *UpdateAccountTierRequest) GetAccountId() string {
	if x == nil { return "" }
	return x.AccountId
}
func (x *UpdateAccountTierRequest) GetTier() int32 {
	if x == nil { return 0 }
	return x.Tier
}

type UpdateAccountTierResponse struct {
	Account *Account `json:"account,omitempty"`
	Message string `json:"message,omitempty"`
}

func (x *UpdateAccountTierResponse) GetAccount() *Account {
	if x == nil { return nil }
	return x.Account
}
func (x *UpdateAccountTierResponse) GetMessage() string {
	if x == nil { return "" }
	return x.Message
}

type Account struct {
	Id string `json:"id,omitempty"`
	Name string `json:"name,omitempty"`
	Networks []string `json:"networks,omitempty"`
	Tier AccountTier `json:"tier,omitempty"`
	CreatedAt *Timestamp `json:"created_at,omitempty"`
	UpdatedAt *Timestamp `json:"updated_at,omitempty"`
}

func (x *Account) GetId() string {
	if x == nil { return "" }
	return x.Id
}
func (x *Account) GetName() string {
	if x == nil { return "" }
	return x.Name
}
func (x *Account) GetNetworks() []string {
	if x == nil { return nil }
	return x.Networks
}
func (x *Account) GetTier() AccountTier {
	if x == nil { return AccountTier(0) }
	return x.Tier
}
func (x *Account) GetCreatedAt() *Timestamp {
	if x == nil { return nil }
	return x.CreatedAt
}
func (x *Account) GetUpdatedAt() *Timestamp {
	if x == nil { return nil }
	return x.UpdatedAt
}

type GetNetworkRequest struct {
	AccountId string `json:"account_id,omitempty"`
}

func (x *GetNetworkRequest) GetAccountId() string {
	if x == nil { return "" }
	return x.AccountId
}

type GetNetworkResponse struct {
	Network *Network `json:"network,omitempty"`
}

func (x *GetNetworkResponse) GetNetwork() *Network {
	if x == nil { return nil }
	return x.Network
}

type GetNetworkStatsRequest struct {
	AccountId string `json:"account_id,omitempty"`
}

func (x *GetNetworkStatsRequest) GetAccountId() string {
	if x == nil { return "" }
	return x.AccountId
}

type GetNetworkStatsResponse struct {
	Stats *NetworkStats `json:"stats,omitempty"`
}

func (x *GetNetworkStatsResponse) GetStats() *NetworkStats {
	if x == nil { return nil }
	return x.Stats
}

type GetAccountIPStatisticsRequest struct {
	AccountId string `json:"account_id,omitempty"`
}

func (x *GetAccountIPStatisticsRequest) GetAccountId() string {
	if x == nil { return "" }
	return x.AccountId
}

type GetAccountIPStatisticsResponse struct {
	TotalIps int32 `json:"total_ips,omitempty"`
	AssignedIps int32 `json:"assigned_ips,omitempty"`
	AvailableIps int32 `json:"available_ips,omitempty"`
	BlockCount int32 `json:"block_count,omitempty"`
}

func (x *GetAccountIPStatisticsResponse) GetTotalIps() int32 {
	if x == nil { return 0 }
	return x.TotalIps
}
func (x *GetAccountIPStatisticsResponse) GetAssignedIps() int32 {
	if x == nil { return 0 }
	return x.AssignedIps
}
func (x *GetAccountIPStatisticsResponse) GetAvailableIps() int32 {
	if x == nil { return 0 }
	return x.AvailableIps
}
func (x *GetAccountIPStatisticsResponse) GetBlockCount() int32 {
	if x == nil { return 0 }
	return x.BlockCount
}

type Network struct {
	AccountId string `json:"account_id,omitempty"`
	Networks []string `json:"networks,omitempty"`
	ListenPort int32 `json:"listen_port,omitempty"`
	PublicKey string `json:"public_key,omitempty"`
	Endpoint string `json:"endpoint,omitempty"`
	PeerCount int32 `json:"peer_count,omitempty"`
	CreatedAt *Timestamp `json:"created_at,omitempty"`
}

func (x *Network) GetAccountId() string {
	if x == nil { return "" }
	return x.AccountId
}
func (x *Network) GetNetworks() []string {
	if x == nil { return nil }
	return x.Networks
}
func (x *Network) GetListenPort() int32 {
	if x == nil { return 0 }
	return x.ListenPort
}
func (x *Network) GetPublicKey() string {
	if x == nil { return "" }
	return x.PublicKey
}
func (x *Network) GetEndpoint() string {
	if x == nil { return "" }
	return x.Endpoint
}
func (x *Network) GetPeerCount() int32 {
	if x == nil { return 0 }
	return x.PeerCount
}
func (x *Network) GetCreatedAt() *Timestamp {
	if x == nil { return nil }
	return x.CreatedAt
}

type NetworkStats struct {
	AccountId string `json:"account_id,omitempty"`
	TotalRxBytes int64 `json:"total_rx_bytes,omitempty"`
	TotalTxBytes int64 `json:"total_tx_bytes,omitempty"`
	ActivePeers int32 `json:"active_peers,omitempty"`
	LastHandshake *Timestamp `json:"last_handshake,omitempty"`
}

func (x *NetworkStats) GetAccountId() string {
	if x == nil { return "" }
	return x.AccountId
}
func (x *NetworkStats) GetTotalRxBytes() int64 {
	if x == nil { return 0 }
	return x.TotalRxBytes
}
func (x *NetworkStats) GetTotalTxBytes() int64 {
	if x == nil { return 0 }
	return x.TotalTxBytes
}
func (x *NetworkStats) GetActivePeers() int32 {
	if x == nil { return 0 }
	return x.ActivePeers
}
func (x *NetworkStats) GetLastHandshake() *Timestamp {
	if x == nil { return nil }
	return x.LastHandshake
}

type AddPeerRequest struct {
	AccountId string `json:"account_id,omitempty"`
	Name string `json:"name,omitempty"`
	AssignedIp string `json:"assigned_ip,omitempty"`
	AllowedIps []string `json:"allowed_ips,omitempty"`
}

func (x *AddPeerRequest) GetAccountId() string {
	if x == nil { return "" }
	return x.AccountId
}
func (x *AddPeerRequest) GetName() string {
	if x == nil { return "" }
	return x.Name
}
func (x *AddPeerRequest) GetAssignedIp() string {
	if x == nil { return "" }
	return x.AssignedIp
}
func (x *AddPeerRequest) GetAllowedIps() []string {
	if x == nil { return nil }
	return x.AllowedIps
}

type AddPeerResponse struct {
	Peer *Peer `json:"peer,omitempty"`
	WgConfig string `json:"wg_config,omitempty"`
	QrCode string `json:"qr_code,omitempty"`
}

func (x *AddPeerResponse) GetPeer() *Peer {
	if x == nil { return nil }
	return x.Peer
}
func (x *AddPeerResponse) GetWgConfig() string {
	if x == nil { return "" }
	return x.WgConfig
}
func (x *AddPeerResponse) GetQrCode() string {
	if x == nil { return "" }
	return x.QrCode
}

type GetPeerRequest struct {
	AccountId string `json:"account_id,omitempty"`
	PeerId string `json:"peer_id,omitempty"`
}

func (x *GetPeerRequest) GetAccountId() string {
	if x == nil { return "" }
	return x.AccountId
}
func (x *GetPeerRequest) GetPeerId() string {
	if x == nil { return "" }
	return x.PeerId
}

type GetPeerResponse struct {
	Peer *Peer `json:"peer,omitempty"`
}

func (x *GetPeerResponse) GetPeer() *Peer {
	if x == nil { return nil }
	return x.Peer
}

type ListPeersRequest struct {
	AccountId string `json:"account_id,omitempty"`
	PageSize int32 `json:"page_size,omitempty"`
	PageToken string `json:"page_token,omitempty"`
}

func (x *ListPeersRequest) GetAccountId() string {
	if x == nil { return "" }
	return x.AccountId
}
func (x *ListPeersRequest) GetPageSize() int32 {
	if x == nil { return 0 }
	return x.PageSize
}
func (x *ListPeersRequest) GetPageToken() string {
	if x == nil { return "" }
	return x.PageToken
}

type ListPeersResponse struct {
	Peers []*Peer `json:"peers,omitempty"`
	NextPageToken string `json:"next_page_token,omitempty"`
	TotalCount int32 `json:"total_count,omitempty"`
}

func (x *ListPeersResponse) GetPeers() []*Peer {
	if x == nil { return nil }
	return x.Peers
}
func (x *ListPeersResponse) GetNextPageToken() string {
	if x == nil { return "" }
	return x.NextPageToken
}
func (x *ListPeersResponse) GetTotalCount() int32 {
	if x == nil { return 0 }
	return x.TotalCount
}

type RemovePeerRequest struct {
	AccountId string `json:"account_id,omitempty"`
	PeerId string `json:"peer_id,omitempty"`
}

func (x *RemovePeerRequest) GetAccountId() string {
	if x == nil { return "" }
	return x.AccountId
}
func (x *RemovePeerRequest) GetPeerId() string {
	if x == nil { return "" }
	return x.PeerId
}

type RemovePeerResponse struct {
	Success bool `json:"success"`
}

func (x *RemovePeerResponse) GetSuccess() bool {
	if x == nil { return false }
	return x.Success
}

type GetPeerConfigRequest struct {
	AccountId string `json:"account_id,omitempty"`
	PeerId string `json:"peer_id,omitempty"`
	Endpoint string `json:"endpoint,omitempty"`
}

func (x *GetPeerConfigRequest) GetAccountId() string {
	if x == nil { return "" }
	return x.AccountId
}
func (x *GetPeerConfigRequest) GetPeerId() string {
	if x == nil { return "" }
	return x.PeerId
}
func (x *GetPeerConfigRequest) GetEndpoint() string {
	if x == nil { return "" }
	return x.Endpoint
}

type GetPeerConfigResponse struct {
	WgConfig string `json:"wg_config,omitempty"`
	QrCode string `json:"qr_code,omitempty"`
}

func (x *GetPeerConfigResponse) GetWgConfig() string {
	if x == nil { return "" }
	return x.WgConfig
}
func (x *GetPeerConfigResponse) GetQrCode() string {
	if x == nil { return "" }
	return x.QrCode
}

type GetPeerStatsRequest struct {
	AccountId string `json:"account_id,omitempty"`
	PeerId string `json:"peer_id,omitempty"`
}

func (x *GetPeerStatsRequest) GetAccountId() string {
	if x == nil { return "" }
	return x.AccountId
}
func (x *GetPeerStatsRequest) GetPeerId() string {
	if x == nil { return "" }
	return x.PeerId
}

type GetPeerStatsResponse struct {
	Stats *PeerStats `json:"stats,omitempty"`
}

func (x *GetPeerStatsResponse) GetStats() *PeerStats {
	if x == nil { return nil }
	return x.Stats
}

type UpdatePeerNotesRequest struct {
	AccountId string `json:"account_id,omitempty"`
	PeerId string `json:"peer_id,omitempty"`
	Notes string `json:"notes,omitempty"`
}

func (x *UpdatePeerNotesRequest) GetAccountId() string {
	if x == nil { return "" }
	return x.AccountId
}
func (x *UpdatePeerNotesRequest) GetPeerId() string {
	if x == nil { return "" }
	return x.PeerId
}
func (x *UpdatePeerNotesRequest) GetNotes() string {
	if x == nil { return "" }
	return x.Notes
}

type UpdatePeerNotesResponse struct {
	Peer *Peer `json:"peer,omitempty"`
}

func (x *UpdatePeerNotesResponse) GetPeer() *Peer {
	if x == nil { return nil }
	return x.Peer
}

type Peer struct {
	Id string `json:"id,omitempty"`
	AccountId string `json:"account_id,omitempty"`
	Name string `json:"name,omitempty"`
	PublicKey string `json:"public_key,omitempty"`
	AssignedIp string `json:"assigned_ip,omitempty"`
	AllowedIps []string `json:"allowed_ips,omitempty"`
	CreatedAt *Timestamp `json:"created_at,omitempty"`
	LastHandshake *Timestamp `json:"last_handshake,omitempty"`
	IsOnline bool `json:"is_online"`
	RxBytes int64 `json:"rx_bytes,omitempty"`
	TxBytes int64 `json:"tx_bytes,omitempty"`
	LastSeenAt *Timestamp `json:"last_seen_at,omitempty"`
	HasWinbox bool `json:"has_winbox"`
	RouterIp string `json:"router_ip,omitempty"`
	SshActivities []*PeerSSHActivity `json:"ssh_activities,omitempty"`
	WinboxActivities []*PeerWinboxActivity `json:"winbox_activities,omitempty"`
	DiscoveredPorts []*OpenPort `json:"discovered_ports,omitempty"`
	LastPortScan *Timestamp `json:"last_port_scan,omitempty"`
	ScannedSshPort int32 `json:"scanned_ssh_port,omitempty"`
	ScannedWinboxPort int32 `json:"scanned_winbox_port,omitempty"`
	Fingerprint *OSFingerprint `json:"fingerprint,omitempty"`
	NotificationEnabled bool `json:"notification_enabled"`
	FirstSeenOnline *Timestamp `json:"first_seen_online,omitempty"`
	LastOnlineAt *Timestamp `json:"last_online_at,omitempty"`
	Tags []string `json:"tags,omitempty"`
	Endpoint string `json:"endpoint,omitempty"`
	ExtendedStats map[string]any `json:"extended_stats,omitempty"`
	Notes string `json:"notes,omitempty"`
	ClientType string `json:"client_type,omitempty"`
	IsShared bool `json:"is_shared"`
	OwnerName string `json:"owner_name,omitempty"`
	ViewerCanWrite bool `json:"viewer_can_write"`
	RouterosCandidate bool `json:"routeros_candidate"`
	RouterosApiReady bool `json:"routeros_api_ready"`
	RouterosApiPort int32 `json:"routeros_api_port,omitempty"`
	RouterosApiTls bool `json:"routeros_api_tls"`
}

func (x *Peer) GetId() string {
	if x == nil { return "" }
	return x.Id
}
func (x *Peer) GetAccountId() string {
	if x == nil { return "" }
	return x.AccountId
}
func (x *Peer) GetName() string {
	if x == nil { return "" }
	return x.Name
}
func (x *Peer) GetPublicKey() string {
	if x == nil { return "" }
	return x.PublicKey
}
func (x *Peer) GetAssignedIp() string {
	if x == nil { return "" }
	return x.AssignedIp
}
func (x *Peer) GetAllowedIps() []string {
	if x == nil { return nil }
	return x.AllowedIps
}
func (x *Peer) GetCreatedAt() *Timestamp {
	if x == nil { return nil }
	return x.CreatedAt
}
func (x *Peer) GetLastHandshake() *Timestamp {
	if x == nil { return nil }
	return x.LastHandshake
}
func (x *Peer) GetIsOnline() bool {
	if x == nil { return false }
	return x.IsOnline
}
func (x *Peer) GetRxBytes() int64 {
	if x == nil { return 0 }
	return x.RxBytes
}
func (x *Peer) GetTxBytes() int64 {
	if x == nil { return 0 }
	return x.TxBytes
}
func (x *Peer) GetLastSeenAt() *Timestamp {
	if x == nil { return nil }
	return x.LastSeenAt
}
func (x *Peer) GetHasWinbox() bool {
	if x == nil { return false }
	return x.HasWinbox
}
func (x *Peer) GetRouterIp() string {
	if x == nil { return "" }
	return x.RouterIp
}
func (x *Peer) GetSshActivities() []*PeerSSHActivity {
	if x == nil { return nil }
	return x.SshActivities
}
func (x *Peer) GetWinboxActivities() []*PeerWinboxActivity {
	if x == nil { return nil }
	return x.WinboxActivities
}
func (x *Peer) GetDiscoveredPorts() []*OpenPort {
	if x == nil { return nil }
	return x.DiscoveredPorts
}
func (x *Peer) GetLastPortScan() *Timestamp {
	if x == nil { return nil }
	return x.LastPortScan
}
func (x *Peer) GetScannedSshPort() int32 {
	if x == nil { return 0 }
	return x.ScannedSshPort
}
func (x *Peer) GetScannedWinboxPort() int32 {
	if x == nil { return 0 }
	return x.ScannedWinboxPort
}
func (x *Peer) GetFingerprint() *OSFingerprint {
	if x == nil { return nil }
	return x.Fingerprint
}
func (x *Peer) GetNotificationEnabled() bool {
	if x == nil { return false }
	return x.NotificationEnabled
}
func (x *Peer) GetFirstSeenOnline() *Timestamp {
	if x == nil { return nil }
	return x.FirstSeenOnline
}
func (x *Peer) GetLastOnlineAt() *Timestamp {
	if x == nil { return nil }
	return x.LastOnlineAt
}
func (x *Peer) GetTags() []string {
	if x == nil { return nil }
	return x.Tags
}
func (x *Peer) GetEndpoint() string {
	if x == nil { return "" }
	return x.Endpoint
}
func (x *Peer) GetExtendedStats() map[string]any {
	if x == nil { return nil }
	return x.ExtendedStats
}
func (x *Peer) GetNotes() string {
	if x == nil { return "" }
	return x.Notes
}
func (x *Peer) GetClientType() string {
	if x == nil { return "" }
	return x.ClientType
}
func (x *Peer) GetIsShared() bool {
	if x == nil { return false }
	return x.IsShared
}
func (x *Peer) GetOwnerName() string {
	if x == nil { return "" }
	return x.OwnerName
}
func (x *Peer) GetViewerCanWrite() bool {
	if x == nil { return false }
	return x.ViewerCanWrite
}
func (x *Peer) GetRouterosCandidate() bool {
	if x == nil { return false }
	return x.RouterosCandidate
}
func (x *Peer) GetRouterosApiReady() bool {
	if x == nil { return false }
	return x.RouterosApiReady
}
func (x *Peer) GetRouterosApiPort() int32 {
	if x == nil { return 0 }
	return x.RouterosApiPort
}
func (x *Peer) GetRouterosApiTls() bool {
	if x == nil { return false }
	return x.RouterosApiTls
}

type PeerSSHActivity struct {
	SessionId string `json:"session_id,omitempty"`
	UserAgent string `json:"user_agent,omitempty"`
	ClientIp string `json:"client_ip,omitempty"`
	Timestamp *Timestamp `json:"timestamp,omitempty"`
	EndTime *Timestamp `json:"end_time,omitempty"`
	Username string `json:"username,omitempty"`
	Commands []string `json:"commands,omitempty"`
	BytesSent uint64 `json:"bytes_sent,omitempty"`
	BytesRecv uint64 `json:"bytes_recv,omitempty"`
	DurationMs int64 `json:"duration_ms,omitempty"`
}

func (x *PeerSSHActivity) GetSessionId() string {
	if x == nil { return "" }
	return x.SessionId
}
func (x *PeerSSHActivity) GetUserAgent() string {
	if x == nil { return "" }
	return x.UserAgent
}
func (x *PeerSSHActivity) GetClientIp() string {
	if x == nil { return "" }
	return x.ClientIp
}
func (x *PeerSSHActivity) GetTimestamp() *Timestamp {
	if x == nil { return nil }
	return x.Timestamp
}
func (x *PeerSSHActivity) GetEndTime() *Timestamp {
	if x == nil { return nil }
	return x.EndTime
}
func (x *PeerSSHActivity) GetUsername() string {
	if x == nil { return "" }
	return x.Username
}
func (x *PeerSSHActivity) GetCommands() []string {
	if x == nil { return nil }
	return x.Commands
}
func (x *PeerSSHActivity) GetBytesSent() uint64 {
	if x == nil { return 0 }
	return x.BytesSent
}
func (x *PeerSSHActivity) GetBytesRecv() uint64 {
	if x == nil { return 0 }
	return x.BytesRecv
}
func (x *PeerSSHActivity) GetDurationMs() int64 {
	if x == nil { return 0 }
	return x.DurationMs
}

type PeerWinboxActivity struct {
	SessionName string `json:"session_name,omitempty"`
	Username string `json:"username,omitempty"`
	ClientIp string `json:"client_ip,omitempty"`
	Timestamp *Timestamp `json:"timestamp,omitempty"`
	EndTime *Timestamp `json:"end_time,omitempty"`
	DurationMs int64 `json:"duration_ms,omitempty"`
	RomonMode bool `json:"romon_mode"`
}

func (x *PeerWinboxActivity) GetSessionName() string {
	if x == nil { return "" }
	return x.SessionName
}
func (x *PeerWinboxActivity) GetUsername() string {
	if x == nil { return "" }
	return x.Username
}
func (x *PeerWinboxActivity) GetClientIp() string {
	if x == nil { return "" }
	return x.ClientIp
}
func (x *PeerWinboxActivity) GetTimestamp() *Timestamp {
	if x == nil { return nil }
	return x.Timestamp
}
func (x *PeerWinboxActivity) GetEndTime() *Timestamp {
	if x == nil { return nil }
	return x.EndTime
}
func (x *PeerWinboxActivity) GetDurationMs() int64 {
	if x == nil { return 0 }
	return x.DurationMs
}
func (x *PeerWinboxActivity) GetRomonMode() bool {
	if x == nil { return false }
	return x.RomonMode
}

type OSFingerprint struct {
	OsFamily string `json:"os_family,omitempty"`
	OsVersion string `json:"os_version,omitempty"`
	Vendor string `json:"vendor,omitempty"`
	DeviceType string `json:"device_type,omitempty"`
	Model string `json:"model,omitempty"`
	Confidence int32 `json:"confidence,omitempty"`
	DetectionInfo string `json:"detection_info,omitempty"`
	Hostname string `json:"hostname,omitempty"`
}

func (x *OSFingerprint) GetOsFamily() string {
	if x == nil { return "" }
	return x.OsFamily
}
func (x *OSFingerprint) GetOsVersion() string {
	if x == nil { return "" }
	return x.OsVersion
}
func (x *OSFingerprint) GetVendor() string {
	if x == nil { return "" }
	return x.Vendor
}
func (x *OSFingerprint) GetDeviceType() string {
	if x == nil { return "" }
	return x.DeviceType
}
func (x *OSFingerprint) GetModel() string {
	if x == nil { return "" }
	return x.Model
}
func (x *OSFingerprint) GetConfidence() int32 {
	if x == nil { return 0 }
	return x.Confidence
}
func (x *OSFingerprint) GetDetectionInfo() string {
	if x == nil { return "" }
	return x.DetectionInfo
}
func (x *OSFingerprint) GetHostname() string {
	if x == nil { return "" }
	return x.Hostname
}

type OpenPort struct {
	Port int32 `json:"port,omitempty"`
	Protocol string `json:"protocol,omitempty"`
	Service string `json:"service,omitempty"`
	Banner string `json:"banner,omitempty"`
	RttMs float32 `json:"rtt_ms,omitempty"`
	IsWebpage bool `json:"is_webpage"`
}

func (x *OpenPort) GetPort() int32 {
	if x == nil { return 0 }
	return x.Port
}
func (x *OpenPort) GetProtocol() string {
	if x == nil { return "" }
	return x.Protocol
}
func (x *OpenPort) GetService() string {
	if x == nil { return "" }
	return x.Service
}
func (x *OpenPort) GetBanner() string {
	if x == nil { return "" }
	return x.Banner
}
func (x *OpenPort) GetRttMs() float32 {
	if x == nil { return 0 }
	return x.RttMs
}
func (x *OpenPort) GetIsWebpage() bool {
	if x == nil { return false }
	return x.IsWebpage
}

type Handshake struct {
	Timestamp *Timestamp `json:"timestamp,omitempty"`
	Endpoint string `json:"endpoint,omitempty"`
}

func (x *Handshake) GetTimestamp() *Timestamp {
	if x == nil { return nil }
	return x.Timestamp
}
func (x *Handshake) GetEndpoint() string {
	if x == nil { return "" }
	return x.Endpoint
}

type PeerStats struct {
	PeerId string `json:"peer_id,omitempty"`
	RxBytes int64 `json:"rx_bytes,omitempty"`
	TxBytes int64 `json:"tx_bytes,omitempty"`
	LastHandshake *Timestamp `json:"last_handshake,omitempty"`
	IsOnline bool `json:"is_online"`
	OpenPorts []*OpenPort `json:"open_ports,omitempty"`
	LastPortScan *Timestamp `json:"last_port_scan,omitempty"`
	ScannedSshPort int32 `json:"scanned_ssh_port,omitempty"`
	ScannedWinboxPort int32 `json:"scanned_winbox_port,omitempty"`
	Fingerprint *OSFingerprint `json:"fingerprint,omitempty"`
	ScanInProgress bool `json:"scan_in_progress"`
	HandshakeHistory []*Handshake `json:"handshake_history,omitempty"`
	ActiveScanId string `json:"active_scan_id,omitempty"`
	UptimeHistory []byte `json:"uptime_history,omitempty"`
}

func (x *PeerStats) GetPeerId() string {
	if x == nil { return "" }
	return x.PeerId
}
func (x *PeerStats) GetRxBytes() int64 {
	if x == nil { return 0 }
	return x.RxBytes
}
func (x *PeerStats) GetTxBytes() int64 {
	if x == nil { return 0 }
	return x.TxBytes
}
func (x *PeerStats) GetLastHandshake() *Timestamp {
	if x == nil { return nil }
	return x.LastHandshake
}
func (x *PeerStats) GetIsOnline() bool {
	if x == nil { return false }
	return x.IsOnline
}
func (x *PeerStats) GetOpenPorts() []*OpenPort {
	if x == nil { return nil }
	return x.OpenPorts
}
func (x *PeerStats) GetLastPortScan() *Timestamp {
	if x == nil { return nil }
	return x.LastPortScan
}
func (x *PeerStats) GetScannedSshPort() int32 {
	if x == nil { return 0 }
	return x.ScannedSshPort
}
func (x *PeerStats) GetScannedWinboxPort() int32 {
	if x == nil { return 0 }
	return x.ScannedWinboxPort
}
func (x *PeerStats) GetFingerprint() *OSFingerprint {
	if x == nil { return nil }
	return x.Fingerprint
}
func (x *PeerStats) GetScanInProgress() bool {
	if x == nil { return false }
	return x.ScanInProgress
}
func (x *PeerStats) GetHandshakeHistory() []*Handshake {
	if x == nil { return nil }
	return x.HandshakeHistory
}
func (x *PeerStats) GetActiveScanId() string {
	if x == nil { return "" }
	return x.ActiveScanId
}
func (x *PeerStats) GetUptimeHistory() []byte {
	if x == nil { return nil }
	return x.UptimeHistory
}

type PingPeerRequest struct {
	AccountId string `json:"account_id,omitempty"`
	PeerId string `json:"peer_id,omitempty"`
	Count int32 `json:"count,omitempty"`
	TimeoutMs int32 `json:"timeout_ms,omitempty"`
}

func (x *PingPeerRequest) GetAccountId() string {
	if x == nil { return "" }
	return x.AccountId
}
func (x *PingPeerRequest) GetPeerId() string {
	if x == nil { return "" }
	return x.PeerId
}
func (x *PingPeerRequest) GetCount() int32 {
	if x == nil { return 0 }
	return x.Count
}
func (x *PingPeerRequest) GetTimeoutMs() int32 {
	if x == nil { return 0 }
	return x.TimeoutMs
}

type PingDetail struct {
	Sequence int32 `json:"sequence,omitempty"`
	RttMs float32 `json:"rtt_ms,omitempty"`
	Success bool `json:"success"`
	Error string `json:"error,omitempty"`
	Timestamp int64 `json:"timestamp,omitempty"`
}

func (x *PingDetail) GetSequence() int32 {
	if x == nil { return 0 }
	return x.Sequence
}
func (x *PingDetail) GetRttMs() float32 {
	if x == nil { return 0 }
	return x.RttMs
}
func (x *PingDetail) GetSuccess() bool {
	if x == nil { return false }
	return x.Success
}
func (x *PingDetail) GetError() string {
	if x == nil { return "" }
	return x.Error
}
func (x *PingDetail) GetTimestamp() int64 {
	if x == nil { return 0 }
	return x.Timestamp
}

type PingPeerResponse struct {
	PeerIp string `json:"peer_ip,omitempty"`
	PacketsSent int32 `json:"packets_sent,omitempty"`
	PacketsReceived int32 `json:"packets_received,omitempty"`
	PacketLossPercent float32 `json:"packet_loss_percent,omitempty"`
	MinRttMs float32 `json:"min_rtt_ms,omitempty"`
	AvgRttMs float32 `json:"avg_rtt_ms,omitempty"`
	MaxRttMs float32 `json:"max_rtt_ms,omitempty"`
	Success bool `json:"success"`
	Error string `json:"error,omitempty"`
	Pings []*PingDetail `json:"pings,omitempty"`
}

func (x *PingPeerResponse) GetPeerIp() string {
	if x == nil { return "" }
	return x.PeerIp
}
func (x *PingPeerResponse) GetPacketsSent() int32 {
	if x == nil { return 0 }
	return x.PacketsSent
}
func (x *PingPeerResponse) GetPacketsReceived() int32 {
	if x == nil { return 0 }
	return x.PacketsReceived
}
func (x *PingPeerResponse) GetPacketLossPercent() float32 {
	if x == nil { return 0 }
	return x.PacketLossPercent
}
func (x *PingPeerResponse) GetMinRttMs() float32 {
	if x == nil { return 0 }
	return x.MinRttMs
}
func (x *PingPeerResponse) GetAvgRttMs() float32 {
	if x == nil { return 0 }
	return x.AvgRttMs
}
func (x *PingPeerResponse) GetMaxRttMs() float32 {
	if x == nil { return 0 }
	return x.MaxRttMs
}
func (x *PingPeerResponse) GetSuccess() bool {
	if x == nil { return false }
	return x.Success
}
func (x *PingPeerResponse) GetError() string {
	if x == nil { return "" }
	return x.Error
}
func (x *PingPeerResponse) GetPings() []*PingDetail {
	if x == nil { return nil }
	return x.Pings
}

type StreamPingRequest struct {
	AccountId string `json:"account_id,omitempty"`
	PeerId string `json:"peer_id,omitempty"`
	Count int32 `json:"count,omitempty"`
	TimeoutMs int32 `json:"timeout_ms,omitempty"`
}

func (x *StreamPingRequest) GetAccountId() string {
	if x == nil { return "" }
	return x.AccountId
}
func (x *StreamPingRequest) GetPeerId() string {
	if x == nil { return "" }
	return x.PeerId
}
func (x *StreamPingRequest) GetCount() int32 {
	if x == nil { return 0 }
	return x.Count
}
func (x *StreamPingRequest) GetTimeoutMs() int32 {
	if x == nil { return 0 }
	return x.TimeoutMs
}

type PingEvent struct {
	IsSummary bool `json:"is_summary"`
	Sequence int32 `json:"sequence,omitempty"`
	RttMs float32 `json:"rtt_ms,omitempty"`
	Success bool `json:"success"`
	Error string `json:"error,omitempty"`
	PeerIp string `json:"peer_ip,omitempty"`
	PacketsSent int32 `json:"packets_sent,omitempty"`
	PacketsReceived int32 `json:"packets_received,omitempty"`
	PacketLossPercent float32 `json:"packet_loss_percent,omitempty"`
	MinRttMs float32 `json:"min_rtt_ms,omitempty"`
	AvgRttMs float32 `json:"avg_rtt_ms,omitempty"`
	MaxRttMs float32 `json:"max_rtt_ms,omitempty"`
}

func (x *PingEvent) GetIsSummary() bool {
	if x == nil { return false }
	return x.IsSummary
}
func (x *PingEvent) GetSequence() int32 {
	if x == nil { return 0 }
	return x.Sequence
}
func (x *PingEvent) GetRttMs() float32 {
	if x == nil { return 0 }
	return x.RttMs
}
func (x *PingEvent) GetSuccess() bool {
	if x == nil { return false }
	return x.Success
}
func (x *PingEvent) GetError() string {
	if x == nil { return "" }
	return x.Error
}
func (x *PingEvent) GetPeerIp() string {
	if x == nil { return "" }
	return x.PeerIp
}
func (x *PingEvent) GetPacketsSent() int32 {
	if x == nil { return 0 }
	return x.PacketsSent
}
func (x *PingEvent) GetPacketsReceived() int32 {
	if x == nil { return 0 }
	return x.PacketsReceived
}
func (x *PingEvent) GetPacketLossPercent() float32 {
	if x == nil { return 0 }
	return x.PacketLossPercent
}
func (x *PingEvent) GetMinRttMs() float32 {
	if x == nil { return 0 }
	return x.MinRttMs
}
func (x *PingEvent) GetAvgRttMs() float32 {
	if x == nil { return 0 }
	return x.AvgRttMs
}
func (x *PingEvent) GetMaxRttMs() float32 {
	if x == nil { return 0 }
	return x.MaxRttMs
}

type SetWinboxCredentialsRequest struct {
	AccountId string `json:"account_id,omitempty"`
	PeerId string `json:"peer_id,omitempty"`
	RouterIp string `json:"router_ip,omitempty"`
	Username string `json:"username,omitempty"`
	Password string `json:"password,omitempty"`
}

func (x *SetWinboxCredentialsRequest) GetAccountId() string {
	if x == nil { return "" }
	return x.AccountId
}
func (x *SetWinboxCredentialsRequest) GetPeerId() string {
	if x == nil { return "" }
	return x.PeerId
}
func (x *SetWinboxCredentialsRequest) GetRouterIp() string {
	if x == nil { return "" }
	return x.RouterIp
}
func (x *SetWinboxCredentialsRequest) GetUsername() string {
	if x == nil { return "" }
	return x.Username
}
func (x *SetWinboxCredentialsRequest) GetPassword() string {
	if x == nil { return "" }
	return x.Password
}

type SetWinboxCredentialsResponse struct {
	AccessToken string `json:"access_token,omitempty"`
	Message string `json:"message,omitempty"`
	AuthMethod string `json:"auth_method,omitempty"`
}

func (x *SetWinboxCredentialsResponse) GetAccessToken() string {
	if x == nil { return "" }
	return x.AccessToken
}
func (x *SetWinboxCredentialsResponse) GetMessage() string {
	if x == nil { return "" }
	return x.Message
}
func (x *SetWinboxCredentialsResponse) GetAuthMethod() string {
	if x == nil { return "" }
	return x.AuthMethod
}

type GetWinboxStatusRequest struct {
	AccountId string `json:"account_id,omitempty"`
	PeerId string `json:"peer_id,omitempty"`
}

func (x *GetWinboxStatusRequest) GetAccountId() string {
	if x == nil { return "" }
	return x.AccountId
}
func (x *GetWinboxStatusRequest) GetPeerId() string {
	if x == nil { return "" }
	return x.PeerId
}

type GetWinboxStatusResponse struct {
	HasWinbox bool `json:"has_winbox"`
	RouterIp string `json:"router_ip,omitempty"`
	VirtualUsername string `json:"virtual_username,omitempty"`
	CredentialsValid bool `json:"credentials_valid"`
	LastProbed *Timestamp `json:"last_probed,omitempty"`
	CredentialError string `json:"credential_error,omitempty"`
	LastConnected *Timestamp `json:"last_connected,omitempty"`
}

func (x *GetWinboxStatusResponse) GetHasWinbox() bool {
	if x == nil { return false }
	return x.HasWinbox
}
func (x *GetWinboxStatusResponse) GetRouterIp() string {
	if x == nil { return "" }
	return x.RouterIp
}
func (x *GetWinboxStatusResponse) GetVirtualUsername() string {
	if x == nil { return "" }
	return x.VirtualUsername
}
func (x *GetWinboxStatusResponse) GetCredentialsValid() bool {
	if x == nil { return false }
	return x.CredentialsValid
}
func (x *GetWinboxStatusResponse) GetLastProbed() *Timestamp {
	if x == nil { return nil }
	return x.LastProbed
}
func (x *GetWinboxStatusResponse) GetCredentialError() string {
	if x == nil { return "" }
	return x.CredentialError
}
func (x *GetWinboxStatusResponse) GetLastConnected() *Timestamp {
	if x == nil { return nil }
	return x.LastConnected
}

type ClearWinboxCredentialsRequest struct {
	AccountId string `json:"account_id,omitempty"`
	PeerId string `json:"peer_id,omitempty"`
}

func (x *ClearWinboxCredentialsRequest) GetAccountId() string {
	if x == nil { return "" }
	return x.AccountId
}
func (x *ClearWinboxCredentialsRequest) GetPeerId() string {
	if x == nil { return "" }
	return x.PeerId
}

type ClearWinboxCredentialsResponse struct {
	Success bool `json:"success"`
	Message string `json:"message,omitempty"`
}

func (x *ClearWinboxCredentialsResponse) GetSuccess() bool {
	if x == nil { return false }
	return x.Success
}
func (x *ClearWinboxCredentialsResponse) GetMessage() string {
	if x == nil { return "" }
	return x.Message
}

type WinboxSession struct {
	Id string `json:"id,omitempty"`
	AccountId string `json:"account_id,omitempty"`
	PeerId string `json:"peer_id,omitempty"`
	Name string `json:"name,omitempty"`
	RouterIp string `json:"router_ip,omitempty"`
	AccessToken string `json:"access_token,omitempty"`
	EncryptedUsername []byte `json:"encrypted_username,omitempty"`
	EncryptedPassword []byte `json:"encrypted_password,omitempty"`
	AuthMethod string `json:"auth_method,omitempty"`
	AllowedClientIps []string `json:"allowed_client_ips,omitempty"`
	CredentialsValid bool `json:"credentials_valid"`
	LastValidated *Timestamp `json:"last_validated,omitempty"`
	ValidationError string `json:"validation_error,omitempty"`
	LastConnected *Timestamp `json:"last_connected,omitempty"`
	CreatedAt *Timestamp `json:"created_at,omitempty"`
	UpdatedAt *Timestamp `json:"updated_at,omitempty"`
	Enabled bool `json:"enabled"`
	PasswordToken string `json:"password_token,omitempty"`
	ActivityLogs []*SessionActivityLog `json:"activity_logs,omitempty"`
	IsShared bool `json:"is_shared"`
	OwnerName string `json:"owner_name,omitempty"`
	ViewerCanWrite bool `json:"viewer_can_write"`
	RouterosApiVerified bool `json:"routeros_api_verified"`
	RouterosApiLastValidated *Timestamp `json:"routeros_api_last_validated,omitempty"`
	RouterosApiError string `json:"routeros_api_error,omitempty"`
	RouterosApiPort int32 `json:"routeros_api_port,omitempty"`
	RouterosApiTls bool `json:"routeros_api_tls"`
}

func (x *WinboxSession) GetId() string {
	if x == nil { return "" }
	return x.Id
}
func (x *WinboxSession) GetAccountId() string {
	if x == nil { return "" }
	return x.AccountId
}
func (x *WinboxSession) GetPeerId() string {
	if x == nil { return "" }
	return x.PeerId
}
func (x *WinboxSession) GetName() string {
	if x == nil { return "" }
	return x.Name
}
func (x *WinboxSession) GetRouterIp() string {
	if x == nil { return "" }
	return x.RouterIp
}
func (x *WinboxSession) GetAccessToken() string {
	if x == nil { return "" }
	return x.AccessToken
}
func (x *WinboxSession) GetEncryptedUsername() []byte {
	if x == nil { return nil }
	return x.EncryptedUsername
}
func (x *WinboxSession) GetEncryptedPassword() []byte {
	if x == nil { return nil }
	return x.EncryptedPassword
}
func (x *WinboxSession) GetAuthMethod() string {
	if x == nil { return "" }
	return x.AuthMethod
}
func (x *WinboxSession) GetAllowedClientIps() []string {
	if x == nil { return nil }
	return x.AllowedClientIps
}
func (x *WinboxSession) GetCredentialsValid() bool {
	if x == nil { return false }
	return x.CredentialsValid
}
func (x *WinboxSession) GetLastValidated() *Timestamp {
	if x == nil { return nil }
	return x.LastValidated
}
func (x *WinboxSession) GetValidationError() string {
	if x == nil { return "" }
	return x.ValidationError
}
func (x *WinboxSession) GetLastConnected() *Timestamp {
	if x == nil { return nil }
	return x.LastConnected
}
func (x *WinboxSession) GetCreatedAt() *Timestamp {
	if x == nil { return nil }
	return x.CreatedAt
}
func (x *WinboxSession) GetUpdatedAt() *Timestamp {
	if x == nil { return nil }
	return x.UpdatedAt
}
func (x *WinboxSession) GetEnabled() bool {
	if x == nil { return false }
	return x.Enabled
}
func (x *WinboxSession) GetPasswordToken() string {
	if x == nil { return "" }
	return x.PasswordToken
}
func (x *WinboxSession) GetActivityLogs() []*SessionActivityLog {
	if x == nil { return nil }
	return x.ActivityLogs
}
func (x *WinboxSession) GetIsShared() bool {
	if x == nil { return false }
	return x.IsShared
}
func (x *WinboxSession) GetOwnerName() string {
	if x == nil { return "" }
	return x.OwnerName
}
func (x *WinboxSession) GetViewerCanWrite() bool {
	if x == nil { return false }
	return x.ViewerCanWrite
}
func (x *WinboxSession) GetRouterosApiVerified() bool {
	if x == nil { return false }
	return x.RouterosApiVerified
}
func (x *WinboxSession) GetRouterosApiLastValidated() *Timestamp {
	if x == nil { return nil }
	return x.RouterosApiLastValidated
}
func (x *WinboxSession) GetRouterosApiError() string {
	if x == nil { return "" }
	return x.RouterosApiError
}
func (x *WinboxSession) GetRouterosApiPort() int32 {
	if x == nil { return 0 }
	return x.RouterosApiPort
}
func (x *WinboxSession) GetRouterosApiTls() bool {
	if x == nil { return false }
	return x.RouterosApiTls
}

type CreateWinboxSessionRequest struct {
	AccountId string `json:"account_id,omitempty"`
	PeerId string `json:"peer_id,omitempty"`
	Name string `json:"name,omitempty"`
	RouterIp string `json:"router_ip,omitempty"`
	Username string `json:"username,omitempty"`
	Password string `json:"password,omitempty"`
	AllowedClientIps []string `json:"allowed_client_ips,omitempty"`
}

func (x *CreateWinboxSessionRequest) GetAccountId() string {
	if x == nil { return "" }
	return x.AccountId
}
func (x *CreateWinboxSessionRequest) GetPeerId() string {
	if x == nil { return "" }
	return x.PeerId
}
func (x *CreateWinboxSessionRequest) GetName() string {
	if x == nil { return "" }
	return x.Name
}
func (x *CreateWinboxSessionRequest) GetRouterIp() string {
	if x == nil { return "" }
	return x.RouterIp
}
func (x *CreateWinboxSessionRequest) GetUsername() string {
	if x == nil { return "" }
	return x.Username
}
func (x *CreateWinboxSessionRequest) GetPassword() string {
	if x == nil { return "" }
	return x.Password
}
func (x *CreateWinboxSessionRequest) GetAllowedClientIps() []string {
	if x == nil { return nil }
	return x.AllowedClientIps
}

type CreateWinboxSessionResponse struct {
	Session *WinboxSession `json:"session,omitempty"`
	AccessToken string `json:"access_token,omitempty"`
	Message string `json:"message,omitempty"`
}

func (x *CreateWinboxSessionResponse) GetSession() *WinboxSession {
	if x == nil { return nil }
	return x.Session
}
func (x *CreateWinboxSessionResponse) GetAccessToken() string {
	if x == nil { return "" }
	return x.AccessToken
}
func (x *CreateWinboxSessionResponse) GetMessage() string {
	if x == nil { return "" }
	return x.Message
}

type UpdateWinboxSessionRequest struct {
	AccountId string `json:"account_id,omitempty"`
	PeerId string `json:"peer_id,omitempty"`
	SessionId string `json:"session_id,omitempty"`
	Name string `json:"name,omitempty"`
	RouterIp string `json:"router_ip,omitempty"`
	Username string `json:"username,omitempty"`
	Password string `json:"password,omitempty"`
	AllowedClientIps []string `json:"allowed_client_ips,omitempty"`
	Enabled bool `json:"enabled"`
	RegenerateToken bool `json:"regenerate_token"`
	ClearAllowedIps bool `json:"clear_allowed_ips"`
}

func (x *UpdateWinboxSessionRequest) GetAccountId() string {
	if x == nil { return "" }
	return x.AccountId
}
func (x *UpdateWinboxSessionRequest) GetPeerId() string {
	if x == nil { return "" }
	return x.PeerId
}
func (x *UpdateWinboxSessionRequest) GetSessionId() string {
	if x == nil { return "" }
	return x.SessionId
}
func (x *UpdateWinboxSessionRequest) GetName() string {
	if x == nil { return "" }
	return x.Name
}
func (x *UpdateWinboxSessionRequest) GetRouterIp() string {
	if x == nil { return "" }
	return x.RouterIp
}
func (x *UpdateWinboxSessionRequest) GetUsername() string {
	if x == nil { return "" }
	return x.Username
}
func (x *UpdateWinboxSessionRequest) GetPassword() string {
	if x == nil { return "" }
	return x.Password
}
func (x *UpdateWinboxSessionRequest) GetAllowedClientIps() []string {
	if x == nil { return nil }
	return x.AllowedClientIps
}
func (x *UpdateWinboxSessionRequest) GetEnabled() bool {
	if x == nil { return false }
	return x.Enabled
}
func (x *UpdateWinboxSessionRequest) GetRegenerateToken() bool {
	if x == nil { return false }
	return x.RegenerateToken
}
func (x *UpdateWinboxSessionRequest) GetClearAllowedIps() bool {
	if x == nil { return false }
	return x.ClearAllowedIps
}

type UpdateWinboxSessionResponse struct {
	Session *WinboxSession `json:"session,omitempty"`
	AccessToken string `json:"access_token,omitempty"`
	Message string `json:"message,omitempty"`
}

func (x *UpdateWinboxSessionResponse) GetSession() *WinboxSession {
	if x == nil { return nil }
	return x.Session
}
func (x *UpdateWinboxSessionResponse) GetAccessToken() string {
	if x == nil { return "" }
	return x.AccessToken
}
func (x *UpdateWinboxSessionResponse) GetMessage() string {
	if x == nil { return "" }
	return x.Message
}

type DeleteWinboxSessionRequest struct {
	AccountId string `json:"account_id,omitempty"`
	PeerId string `json:"peer_id,omitempty"`
	SessionId string `json:"session_id,omitempty"`
}

func (x *DeleteWinboxSessionRequest) GetAccountId() string {
	if x == nil { return "" }
	return x.AccountId
}
func (x *DeleteWinboxSessionRequest) GetPeerId() string {
	if x == nil { return "" }
	return x.PeerId
}
func (x *DeleteWinboxSessionRequest) GetSessionId() string {
	if x == nil { return "" }
	return x.SessionId
}

type DeleteWinboxSessionResponse struct {
	Success bool `json:"success"`
	Message string `json:"message,omitempty"`
}

func (x *DeleteWinboxSessionResponse) GetSuccess() bool {
	if x == nil { return false }
	return x.Success
}
func (x *DeleteWinboxSessionResponse) GetMessage() string {
	if x == nil { return "" }
	return x.Message
}

type ListWinboxSessionsRequest struct {
	AccountId string `json:"account_id,omitempty"`
	PeerId string `json:"peer_id,omitempty"`
}

func (x *ListWinboxSessionsRequest) GetAccountId() string {
	if x == nil { return "" }
	return x.AccountId
}
func (x *ListWinboxSessionsRequest) GetPeerId() string {
	if x == nil { return "" }
	return x.PeerId
}

type ListWinboxSessionsResponse struct {
	Sessions []*WinboxSession `json:"sessions,omitempty"`
}

func (x *ListWinboxSessionsResponse) GetSessions() []*WinboxSession {
	if x == nil { return nil }
	return x.Sessions
}

type GetWinboxSessionRequest struct {
	AccountId string `json:"account_id,omitempty"`
	PeerId string `json:"peer_id,omitempty"`
	SessionId string `json:"session_id,omitempty"`
}

func (x *GetWinboxSessionRequest) GetAccountId() string {
	if x == nil { return "" }
	return x.AccountId
}
func (x *GetWinboxSessionRequest) GetPeerId() string {
	if x == nil { return "" }
	return x.PeerId
}
func (x *GetWinboxSessionRequest) GetSessionId() string {
	if x == nil { return "" }
	return x.SessionId
}

type GetWinboxSessionResponse struct {
	Session *WinboxSession `json:"session,omitempty"`
}

func (x *GetWinboxSessionResponse) GetSession() *WinboxSession {
	if x == nil { return nil }
	return x.Session
}

type StartPortScanRequest struct {
	AccountId string `json:"account_id,omitempty"`
	PeerId string `json:"peer_id,omitempty"`
	FullScan bool `json:"full_scan"`
	Ports []int32 `json:"ports,omitempty"`
	Tcp bool `json:"tcp"`
	Udp bool `json:"udp"`
}

func (x *StartPortScanRequest) GetAccountId() string {
	if x == nil { return "" }
	return x.AccountId
}
func (x *StartPortScanRequest) GetPeerId() string {
	if x == nil { return "" }
	return x.PeerId
}
func (x *StartPortScanRequest) GetFullScan() bool {
	if x == nil { return false }
	return x.FullScan
}
func (x *StartPortScanRequest) GetPorts() []int32 {
	if x == nil { return nil }
	return x.Ports
}
func (x *StartPortScanRequest) GetTcp() bool {
	if x == nil { return false }
	return x.Tcp
}
func (x *StartPortScanRequest) GetUdp() bool {
	if x == nil { return false }
	return x.Udp
}

type StartPortScanResponse struct {
	ScanId string `json:"scan_id,omitempty"`
	Status string `json:"status,omitempty"`
	Message string `json:"message,omitempty"`
}

func (x *StartPortScanResponse) GetScanId() string {
	if x == nil { return "" }
	return x.ScanId
}
func (x *StartPortScanResponse) GetStatus() string {
	if x == nil { return "" }
	return x.Status
}
func (x *StartPortScanResponse) GetMessage() string {
	if x == nil { return "" }
	return x.Message
}

type StopPortScanRequest struct {
	AccountId string `json:"account_id,omitempty"`
	PeerId string `json:"peer_id,omitempty"`
	ScanId string `json:"scan_id,omitempty"`
}

func (x *StopPortScanRequest) GetAccountId() string {
	if x == nil { return "" }
	return x.AccountId
}
func (x *StopPortScanRequest) GetPeerId() string {
	if x == nil { return "" }
	return x.PeerId
}
func (x *StopPortScanRequest) GetScanId() string {
	if x == nil { return "" }
	return x.ScanId
}

type StopPortScanResponse struct {
	Success bool `json:"success"`
	Message string `json:"message,omitempty"`
}

func (x *StopPortScanResponse) GetSuccess() bool {
	if x == nil { return false }
	return x.Success
}
func (x *StopPortScanResponse) GetMessage() string {
	if x == nil { return "" }
	return x.Message
}

type PausePortScanRequest struct {
	AccountId string `json:"account_id,omitempty"`
	PeerId string `json:"peer_id,omitempty"`
	ScanId string `json:"scan_id,omitempty"`
}

func (x *PausePortScanRequest) GetAccountId() string {
	if x == nil { return "" }
	return x.AccountId
}
func (x *PausePortScanRequest) GetPeerId() string {
	if x == nil { return "" }
	return x.PeerId
}
func (x *PausePortScanRequest) GetScanId() string {
	if x == nil { return "" }
	return x.ScanId
}

type PausePortScanResponse struct {
	Success bool `json:"success"`
	Message string `json:"message,omitempty"`
}

func (x *PausePortScanResponse) GetSuccess() bool {
	if x == nil { return false }
	return x.Success
}
func (x *PausePortScanResponse) GetMessage() string {
	if x == nil { return "" }
	return x.Message
}

type ResumePortScanRequest struct {
	AccountId string `json:"account_id,omitempty"`
	PeerId string `json:"peer_id,omitempty"`
	ScanId string `json:"scan_id,omitempty"`
}

func (x *ResumePortScanRequest) GetAccountId() string {
	if x == nil { return "" }
	return x.AccountId
}
func (x *ResumePortScanRequest) GetPeerId() string {
	if x == nil { return "" }
	return x.PeerId
}
func (x *ResumePortScanRequest) GetScanId() string {
	if x == nil { return "" }
	return x.ScanId
}

type ResumePortScanResponse struct {
	Success bool `json:"success"`
	Message string `json:"message,omitempty"`
}

func (x *ResumePortScanResponse) GetSuccess() bool {
	if x == nil { return false }
	return x.Success
}
func (x *ResumePortScanResponse) GetMessage() string {
	if x == nil { return "" }
	return x.Message
}

type StreamPortScanStatusRequest struct {
	AccountId string `json:"account_id,omitempty"`
	PeerId string `json:"peer_id,omitempty"`
	ScanId string `json:"scan_id,omitempty"`
}

func (x *StreamPortScanStatusRequest) GetAccountId() string {
	if x == nil { return "" }
	return x.AccountId
}
func (x *StreamPortScanStatusRequest) GetPeerId() string {
	if x == nil { return "" }
	return x.PeerId
}
func (x *StreamPortScanStatusRequest) GetScanId() string {
	if x == nil { return "" }
	return x.ScanId
}

type PortScanStatusUpdate struct {
	ScanId string `json:"scan_id,omitempty"`
	Status string `json:"status,omitempty"`
	ProgressPercent int32 `json:"progress_percent,omitempty"`
	CurrentPort int32 `json:"current_port,omitempty"`
	TotalPorts int32 `json:"total_ports,omitempty"`
	OpenPortsCount int32 `json:"open_ports_count,omitempty"`
	LastFoundPort *OpenPort `json:"last_found_port,omitempty"`
	Message string `json:"message,omitempty"`
	PeerId string `json:"peer_id,omitempty"`
}

func (x *PortScanStatusUpdate) GetScanId() string {
	if x == nil { return "" }
	return x.ScanId
}
func (x *PortScanStatusUpdate) GetStatus() string {
	if x == nil { return "" }
	return x.Status
}
func (x *PortScanStatusUpdate) GetProgressPercent() int32 {
	if x == nil { return 0 }
	return x.ProgressPercent
}
func (x *PortScanStatusUpdate) GetCurrentPort() int32 {
	if x == nil { return 0 }
	return x.CurrentPort
}
func (x *PortScanStatusUpdate) GetTotalPorts() int32 {
	if x == nil { return 0 }
	return x.TotalPorts
}
func (x *PortScanStatusUpdate) GetOpenPortsCount() int32 {
	if x == nil { return 0 }
	return x.OpenPortsCount
}
func (x *PortScanStatusUpdate) GetLastFoundPort() *OpenPort {
	if x == nil { return nil }
	return x.LastFoundPort
}
func (x *PortScanStatusUpdate) GetMessage() string {
	if x == nil { return "" }
	return x.Message
}
func (x *PortScanStatusUpdate) GetPeerId() string {
	if x == nil { return "" }
	return x.PeerId
}

type CheckAdminSetupRequest struct {
}


type CheckAdminSetupResponse struct {
	NeedsSetup bool `json:"needs_setup"`
	Message string `json:"message,omitempty"`
}

func (x *CheckAdminSetupResponse) GetNeedsSetup() bool {
	if x == nil { return false }
	return x.NeedsSetup
}
func (x *CheckAdminSetupResponse) GetMessage() string {
	if x == nil { return "" }
	return x.Message
}

type CreateFirstAdminRequest struct {
	Username string `json:"username,omitempty"`
	Password string `json:"password,omitempty"`
	EnableTotp bool `json:"enable_totp"`
}

func (x *CreateFirstAdminRequest) GetUsername() string {
	if x == nil { return "" }
	return x.Username
}
func (x *CreateFirstAdminRequest) GetPassword() string {
	if x == nil { return "" }
	return x.Password
}
func (x *CreateFirstAdminRequest) GetEnableTotp() bool {
	if x == nil { return false }
	return x.EnableTotp
}

type CreateFirstAdminResponse struct {
	Success bool `json:"success"`
	Message string `json:"message,omitempty"`
	TotpSecret string `json:"totp_secret,omitempty"`
	TotpProvisioningUrl string `json:"totp_provisioning_url,omitempty"`
}

func (x *CreateFirstAdminResponse) GetSuccess() bool {
	if x == nil { return false }
	return x.Success
}
func (x *CreateFirstAdminResponse) GetMessage() string {
	if x == nil { return "" }
	return x.Message
}
func (x *CreateFirstAdminResponse) GetTotpSecret() string {
	if x == nil { return "" }
	return x.TotpSecret
}
func (x *CreateFirstAdminResponse) GetTotpProvisioningUrl() string {
	if x == nil { return "" }
	return x.TotpProvisioningUrl
}

type AdminLoginRequest struct {
	Username string `json:"username,omitempty"`
	Password string `json:"password,omitempty"`
	TotpCode string `json:"totp_code,omitempty"`
	IpAddress string `json:"ip_address,omitempty"`
}

func (x *AdminLoginRequest) GetUsername() string {
	if x == nil { return "" }
	return x.Username
}
func (x *AdminLoginRequest) GetPassword() string {
	if x == nil { return "" }
	return x.Password
}
func (x *AdminLoginRequest) GetTotpCode() string {
	if x == nil { return "" }
	return x.TotpCode
}
func (x *AdminLoginRequest) GetIpAddress() string {
	if x == nil { return "" }
	return x.IpAddress
}

type AdminLoginResponse struct {
	Success bool `json:"success"`
	SessionToken string `json:"session_token,omitempty"`
	Message string `json:"message,omitempty"`
	RequiresTotp bool `json:"requires_totp"`
	AdminId string `json:"admin_id,omitempty"`
}

func (x *AdminLoginResponse) GetSuccess() bool {
	if x == nil { return false }
	return x.Success
}
func (x *AdminLoginResponse) GetSessionToken() string {
	if x == nil { return "" }
	return x.SessionToken
}
func (x *AdminLoginResponse) GetMessage() string {
	if x == nil { return "" }
	return x.Message
}
func (x *AdminLoginResponse) GetRequiresTotp() bool {
	if x == nil { return false }
	return x.RequiresTotp
}
func (x *AdminLoginResponse) GetAdminId() string {
	if x == nil { return "" }
	return x.AdminId
}

type ValidateAdminSessionRequest struct {
	SessionToken string `json:"session_token,omitempty"`
}

func (x *ValidateAdminSessionRequest) GetSessionToken() string {
	if x == nil { return "" }
	return x.SessionToken
}

type ValidateAdminSessionResponse struct {
	Valid bool `json:"valid"`
	AdminId string `json:"admin_id,omitempty"`
	Message string `json:"message,omitempty"`
}

func (x *ValidateAdminSessionResponse) GetValid() bool {
	if x == nil { return false }
	return x.Valid
}
func (x *ValidateAdminSessionResponse) GetAdminId() string {
	if x == nil { return "" }
	return x.AdminId
}
func (x *ValidateAdminSessionResponse) GetMessage() string {
	if x == nil { return "" }
	return x.Message
}

type AdminLogoutRequest struct {
	SessionToken string `json:"session_token,omitempty"`
}

func (x *AdminLogoutRequest) GetSessionToken() string {
	if x == nil { return "" }
	return x.SessionToken
}

type AdminLogoutResponse struct {
	Success bool `json:"success"`
}

func (x *AdminLogoutResponse) GetSuccess() bool {
	if x == nil { return false }
	return x.Success
}

type GetAdminProfileRequest struct {
	SessionToken string `json:"session_token,omitempty"`
}

func (x *GetAdminProfileRequest) GetSessionToken() string {
	if x == nil { return "" }
	return x.SessionToken
}

type GetAdminProfileResponse struct {
	Username string `json:"username,omitempty"`
	TotpEnabled bool `json:"totp_enabled"`
	CreatedAt *Timestamp `json:"created_at,omitempty"`
	LastLogin *Timestamp `json:"last_login,omitempty"`
}

func (x *GetAdminProfileResponse) GetUsername() string {
	if x == nil { return "" }
	return x.Username
}
func (x *GetAdminProfileResponse) GetTotpEnabled() bool {
	if x == nil { return false }
	return x.TotpEnabled
}
func (x *GetAdminProfileResponse) GetCreatedAt() *Timestamp {
	if x == nil { return nil }
	return x.CreatedAt
}
func (x *GetAdminProfileResponse) GetLastLogin() *Timestamp {
	if x == nil { return nil }
	return x.LastLogin
}

type UpdateAdminSettingsRequest struct {
	SessionToken string `json:"session_token,omitempty"`
	CurrentPassword string `json:"current_password,omitempty"`
	NewPassword string `json:"new_password,omitempty"`
	EnableTotp bool `json:"enable_totp"`
	DisableTotp bool `json:"disable_totp"`
}

func (x *UpdateAdminSettingsRequest) GetSessionToken() string {
	if x == nil { return "" }
	return x.SessionToken
}
func (x *UpdateAdminSettingsRequest) GetCurrentPassword() string {
	if x == nil { return "" }
	return x.CurrentPassword
}
func (x *UpdateAdminSettingsRequest) GetNewPassword() string {
	if x == nil { return "" }
	return x.NewPassword
}
func (x *UpdateAdminSettingsRequest) GetEnableTotp() bool {
	if x == nil { return false }
	return x.EnableTotp
}
func (x *UpdateAdminSettingsRequest) GetDisableTotp() bool {
	if x == nil { return false }
	return x.DisableTotp
}

type UpdateAdminSettingsResponse struct {
	Success bool `json:"success"`
	Message string `json:"message,omitempty"`
	TotpSecret string `json:"totp_secret,omitempty"`
	TotpProvisioningUrl string `json:"totp_provisioning_url,omitempty"`
}

func (x *UpdateAdminSettingsResponse) GetSuccess() bool {
	if x == nil { return false }
	return x.Success
}
func (x *UpdateAdminSettingsResponse) GetMessage() string {
	if x == nil { return "" }
	return x.Message
}
func (x *UpdateAdminSettingsResponse) GetTotpSecret() string {
	if x == nil { return "" }
	return x.TotpSecret
}
func (x *UpdateAdminSettingsResponse) GetTotpProvisioningUrl() string {
	if x == nil { return "" }
	return x.TotpProvisioningUrl
}

type ValidateSessionRequest struct {
	SessionToken string `json:"session_token,omitempty"`
}

func (x *ValidateSessionRequest) GetSessionToken() string {
	if x == nil { return "" }
	return x.SessionToken
}

type ValidateSessionResponse struct {
	Valid bool `json:"valid"`
	UserId string `json:"user_id,omitempty"`
	UserType UserType `json:"user_type,omitempty"`
	Message string `json:"message,omitempty"`
	EmailVerified bool `json:"email_verified"`
	Tier AccountTier `json:"tier,omitempty"`
	FullName string `json:"full_name,omitempty"`
	Email string `json:"email,omitempty"`
}

func (x *ValidateSessionResponse) GetValid() bool {
	if x == nil { return false }
	return x.Valid
}
func (x *ValidateSessionResponse) GetUserId() string {
	if x == nil { return "" }
	return x.UserId
}
func (x *ValidateSessionResponse) GetUserType() UserType {
	if x == nil { return UserType(0) }
	return x.UserType
}
func (x *ValidateSessionResponse) GetMessage() string {
	if x == nil { return "" }
	return x.Message
}
func (x *ValidateSessionResponse) GetEmailVerified() bool {
	if x == nil { return false }
	return x.EmailVerified
}
func (x *ValidateSessionResponse) GetTier() AccountTier {
	if x == nil { return AccountTier(0) }
	return x.Tier
}
func (x *ValidateSessionResponse) GetFullName() string {
	if x == nil { return "" }
	return x.FullName
}
func (x *ValidateSessionResponse) GetEmail() string {
	if x == nil { return "" }
	return x.Email
}

type GetGlobalStatsRequest struct {
}


type GetGlobalStatsResponse struct {
	TotalAccounts int32 `json:"total_accounts,omitempty"`
	TotalPeers int32 `json:"total_peers,omitempty"`
	TotalRxBytes int64 `json:"total_rx_bytes,omitempty"`
	TotalTxBytes int64 `json:"total_tx_bytes,omitempty"`
	ActiveTenants int32 `json:"active_tenants,omitempty"`
	UptimeSince *Timestamp `json:"uptime_since,omitempty"`
	CpuPercent float64 `json:"cpu_percent,omitempty"`
	MemoryBytes int64 `json:"memory_bytes,omitempty"`
	MemoryTotalBytes int64 `json:"memory_total_bytes,omitempty"`
	Goroutines int32 `json:"goroutines,omitempty"`
	GoVersion string `json:"go_version,omitempty"`
	Version string `json:"version,omitempty"`
	UptimeSeconds int64 `json:"uptime_seconds,omitempty"`
	WireguardPort int32 `json:"wireguard_port,omitempty"`
	PortMode string `json:"port_mode,omitempty"`
	SubnetPools []string `json:"subnet_pools,omitempty"`
	BlocksAllocated int32 `json:"blocks_allocated,omitempty"`
	BlocksTotal int32 `json:"blocks_total,omitempty"`
	ActiveSshSessions int32 `json:"active_ssh_sessions,omitempty"`
	ActiveWinboxSessions int32 `json:"active_winbox_sessions,omitempty"`
	TenantStats []*TenantStats `json:"tenant_stats,omitempty"`
}

func (x *GetGlobalStatsResponse) GetTotalAccounts() int32 {
	if x == nil { return 0 }
	return x.TotalAccounts
}
func (x *GetGlobalStatsResponse) GetTotalPeers() int32 {
	if x == nil { return 0 }
	return x.TotalPeers
}
func (x *GetGlobalStatsResponse) GetTotalRxBytes() int64 {
	if x == nil { return 0 }
	return x.TotalRxBytes
}
func (x *GetGlobalStatsResponse) GetTotalTxBytes() int64 {
	if x == nil { return 0 }
	return x.TotalTxBytes
}
func (x *GetGlobalStatsResponse) GetActiveTenants() int32 {
	if x == nil { return 0 }
	return x.ActiveTenants
}
func (x *GetGlobalStatsResponse) GetUptimeSince() *Timestamp {
	if x == nil { return nil }
	return x.UptimeSince
}
func (x *GetGlobalStatsResponse) GetCpuPercent() float64 {
	if x == nil { return 0 }
	return x.CpuPercent
}
func (x *GetGlobalStatsResponse) GetMemoryBytes() int64 {
	if x == nil { return 0 }
	return x.MemoryBytes
}
func (x *GetGlobalStatsResponse) GetMemoryTotalBytes() int64 {
	if x == nil { return 0 }
	return x.MemoryTotalBytes
}
func (x *GetGlobalStatsResponse) GetGoroutines() int32 {
	if x == nil { return 0 }
	return x.Goroutines
}
func (x *GetGlobalStatsResponse) GetGoVersion() string {
	if x == nil { return "" }
	return x.GoVersion
}
func (x *GetGlobalStatsResponse) GetVersion() string {
	if x == nil { return "" }
	return x.Version
}
func (x *GetGlobalStatsResponse) GetUptimeSeconds() int64 {
	if x == nil { return 0 }
	return x.UptimeSeconds
}
func (x *GetGlobalStatsResponse) GetWireguardPort() int32 {
	if x == nil { return 0 }
	return x.WireguardPort
}
func (x *GetGlobalStatsResponse) GetPortMode() string {
	if x == nil { return "" }
	return x.PortMode
}
func (x *GetGlobalStatsResponse) GetSubnetPools() []string {
	if x == nil { return nil }
	return x.SubnetPools
}
func (x *GetGlobalStatsResponse) GetBlocksAllocated() int32 {
	if x == nil { return 0 }
	return x.BlocksAllocated
}
func (x *GetGlobalStatsResponse) GetBlocksTotal() int32 {
	if x == nil { return 0 }
	return x.BlocksTotal
}
func (x *GetGlobalStatsResponse) GetActiveSshSessions() int32 {
	if x == nil { return 0 }
	return x.ActiveSshSessions
}
func (x *GetGlobalStatsResponse) GetActiveWinboxSessions() int32 {
	if x == nil { return 0 }
	return x.ActiveWinboxSessions
}
func (x *GetGlobalStatsResponse) GetTenantStats() []*TenantStats {
	if x == nil { return nil }
	return x.TenantStats
}

type TenantStats struct {
	AccountId string `json:"account_id,omitempty"`
	AccountName string `json:"account_name,omitempty"`
	PeerCount int32 `json:"peer_count,omitempty"`
	ConnectedPeers int32 `json:"connected_peers,omitempty"`
	RxBytes int64 `json:"rx_bytes,omitempty"`
	TxBytes int64 `json:"tx_bytes,omitempty"`
	LastActivity *Timestamp `json:"last_activity,omitempty"`
}

func (x *TenantStats) GetAccountId() string {
	if x == nil { return "" }
	return x.AccountId
}
func (x *TenantStats) GetAccountName() string {
	if x == nil { return "" }
	return x.AccountName
}
func (x *TenantStats) GetPeerCount() int32 {
	if x == nil { return 0 }
	return x.PeerCount
}
func (x *TenantStats) GetConnectedPeers() int32 {
	if x == nil { return 0 }
	return x.ConnectedPeers
}
func (x *TenantStats) GetRxBytes() int64 {
	if x == nil { return 0 }
	return x.RxBytes
}
func (x *TenantStats) GetTxBytes() int64 {
	if x == nil { return 0 }
	return x.TxBytes
}
func (x *TenantStats) GetLastActivity() *Timestamp {
	if x == nil { return nil }
	return x.LastActivity
}

type GetTopologyRequest struct {
	AccountId string `json:"account_id,omitempty"`
	IncludeRules bool `json:"include_rules"`
	IncludeStats bool `json:"include_stats"`
}

func (x *GetTopologyRequest) GetAccountId() string {
	if x == nil { return "" }
	return x.AccountId
}
func (x *GetTopologyRequest) GetIncludeRules() bool {
	if x == nil { return false }
	return x.IncludeRules
}
func (x *GetTopologyRequest) GetIncludeStats() bool {
	if x == nil { return false }
	return x.IncludeStats
}

type GetTopologyResponse struct {
	Nodes []*TopologyNode `json:"nodes,omitempty"`
	Edges []*TopologyEdge `json:"edges,omitempty"`
	Tenants []*TopologyTenant `json:"tenants,omitempty"`
}

func (x *GetTopologyResponse) GetNodes() []*TopologyNode {
	if x == nil { return nil }
	return x.Nodes
}
func (x *GetTopologyResponse) GetEdges() []*TopologyEdge {
	if x == nil { return nil }
	return x.Edges
}
func (x *GetTopologyResponse) GetTenants() []*TopologyTenant {
	if x == nil { return nil }
	return x.Tenants
}

type TopologyNode struct {
	Id string `json:"id,omitempty"`
	Label string `json:"label,omitempty"`
	Type NodeType `json:"type,omitempty"`
	Status NodeStatus `json:"status,omitempty"`
	X float64 `json:"x,omitempty"`
	Y float64 `json:"y,omitempty"`
	AccountId string `json:"account_id,omitempty"`
	AccountName string `json:"account_name,omitempty"`
	Ip string `json:"ip,omitempty"`
	PublicKey string `json:"public_key,omitempty"`
	RxBytes int64 `json:"rx_bytes,omitempty"`
	TxBytes int64 `json:"tx_bytes,omitempty"`
	LastHandshake *Timestamp `json:"last_handshake,omitempty"`
	HasWinbox bool `json:"has_winbox"`
	HasSsh bool `json:"has_ssh"`
	Groups []string `json:"groups,omitempty"`
	Metadata map[string]string `json:"metadata,omitempty"`
	Fingerprint *OSFingerprint `json:"fingerprint,omitempty"`
}

func (x *TopologyNode) GetId() string {
	if x == nil { return "" }
	return x.Id
}
func (x *TopologyNode) GetLabel() string {
	if x == nil { return "" }
	return x.Label
}
func (x *TopologyNode) GetType() NodeType {
	if x == nil { return NodeType(0) }
	return x.Type
}
func (x *TopologyNode) GetStatus() NodeStatus {
	if x == nil { return NodeStatus(0) }
	return x.Status
}
func (x *TopologyNode) GetX() float64 {
	if x == nil { return 0 }
	return x.X
}
func (x *TopologyNode) GetY() float64 {
	if x == nil { return 0 }
	return x.Y
}
func (x *TopologyNode) GetAccountId() string {
	if x == nil { return "" }
	return x.AccountId
}
func (x *TopologyNode) GetAccountName() string {
	if x == nil { return "" }
	return x.AccountName
}
func (x *TopologyNode) GetIp() string {
	if x == nil { return "" }
	return x.Ip
}
func (x *TopologyNode) GetPublicKey() string {
	if x == nil { return "" }
	return x.PublicKey
}
func (x *TopologyNode) GetRxBytes() int64 {
	if x == nil { return 0 }
	return x.RxBytes
}
func (x *TopologyNode) GetTxBytes() int64 {
	if x == nil { return 0 }
	return x.TxBytes
}
func (x *TopologyNode) GetLastHandshake() *Timestamp {
	if x == nil { return nil }
	return x.LastHandshake
}
func (x *TopologyNode) GetHasWinbox() bool {
	if x == nil { return false }
	return x.HasWinbox
}
func (x *TopologyNode) GetHasSsh() bool {
	if x == nil { return false }
	return x.HasSsh
}
func (x *TopologyNode) GetGroups() []string {
	if x == nil { return nil }
	return x.Groups
}
func (x *TopologyNode) GetMetadata() map[string]string {
	if x == nil { return nil }
	return x.Metadata
}
func (x *TopologyNode) GetFingerprint() *OSFingerprint {
	if x == nil { return nil }
	return x.Fingerprint
}

type TopologyEdge struct {
	Id string `json:"id,omitempty"`
	Source string `json:"source,omitempty"`
	Target string `json:"target,omitempty"`
	Type EdgeType `json:"type,omitempty"`
	Direction EdgeDirection `json:"direction,omitempty"`
	TxBytes int64 `json:"tx_bytes,omitempty"`
	RxBytes int64 `json:"rx_bytes,omitempty"`
	Active bool `json:"active"`
	Rules []*EdgeRule `json:"rules,omitempty"`
	Color string `json:"color,omitempty"`
	Width int32 `json:"width,omitempty"`
	Metadata map[string]string `json:"metadata,omitempty"`
}

func (x *TopologyEdge) GetId() string {
	if x == nil { return "" }
	return x.Id
}
func (x *TopologyEdge) GetSource() string {
	if x == nil { return "" }
	return x.Source
}
func (x *TopologyEdge) GetTarget() string {
	if x == nil { return "" }
	return x.Target
}
func (x *TopologyEdge) GetType() EdgeType {
	if x == nil { return EdgeType(0) }
	return x.Type
}
func (x *TopologyEdge) GetDirection() EdgeDirection {
	if x == nil { return EdgeDirection(0) }
	return x.Direction
}
func (x *TopologyEdge) GetTxBytes() int64 {
	if x == nil { return 0 }
	return x.TxBytes
}
func (x *TopologyEdge) GetRxBytes() int64 {
	if x == nil { return 0 }
	return x.RxBytes
}
func (x *TopologyEdge) GetActive() bool {
	if x == nil { return false }
	return x.Active
}
func (x *TopologyEdge) GetRules() []*EdgeRule {
	if x == nil { return nil }
	return x.Rules
}
func (x *TopologyEdge) GetColor() string {
	if x == nil { return "" }
	return x.Color
}
func (x *TopologyEdge) GetWidth() int32 {
	if x == nil { return 0 }
	return x.Width
}
func (x *TopologyEdge) GetMetadata() map[string]string {
	if x == nil { return nil }
	return x.Metadata
}

type EdgeRule struct {
	RuleId string `json:"rule_id,omitempty"`
	Name string `json:"name,omitempty"`
	Action string `json:"action,omitempty"`
	Direction EdgeDirection `json:"direction,omitempty"`
	Services []*EdgeService `json:"services,omitempty"`
	Priority int32 `json:"priority,omitempty"`
	Enabled bool `json:"enabled"`
}

func (x *EdgeRule) GetRuleId() string {
	if x == nil { return "" }
	return x.RuleId
}
func (x *EdgeRule) GetName() string {
	if x == nil { return "" }
	return x.Name
}
func (x *EdgeRule) GetAction() string {
	if x == nil { return "" }
	return x.Action
}
func (x *EdgeRule) GetDirection() EdgeDirection {
	if x == nil { return EdgeDirection(0) }
	return x.Direction
}
func (x *EdgeRule) GetServices() []*EdgeService {
	if x == nil { return nil }
	return x.Services
}
func (x *EdgeRule) GetPriority() int32 {
	if x == nil { return 0 }
	return x.Priority
}
func (x *EdgeRule) GetEnabled() bool {
	if x == nil { return false }
	return x.Enabled
}

type EdgeService struct {
	Name string `json:"name,omitempty"`
	Protocol string `json:"protocol,omitempty"`
	Port int32 `json:"port,omitempty"`
	PortEnd int32 `json:"port_end,omitempty"`
	Enabled bool `json:"enabled"`
}

func (x *EdgeService) GetName() string {
	if x == nil { return "" }
	return x.Name
}
func (x *EdgeService) GetProtocol() string {
	if x == nil { return "" }
	return x.Protocol
}
func (x *EdgeService) GetPort() int32 {
	if x == nil { return 0 }
	return x.Port
}
func (x *EdgeService) GetPortEnd() int32 {
	if x == nil { return 0 }
	return x.PortEnd
}
func (x *EdgeService) GetEnabled() bool {
	if x == nil { return false }
	return x.Enabled
}

type TopologyTenant struct {
	AccountId string `json:"account_id,omitempty"`
	AccountName string `json:"account_name,omitempty"`
	PeerCount int32 `json:"peer_count,omitempty"`
	OnlinePeers int32 `json:"online_peers,omitempty"`
	ServerIp string `json:"server_ip,omitempty"`
	Status NodeStatus `json:"status,omitempty"`
}

func (x *TopologyTenant) GetAccountId() string {
	if x == nil { return "" }
	return x.AccountId
}
func (x *TopologyTenant) GetAccountName() string {
	if x == nil { return "" }
	return x.AccountName
}
func (x *TopologyTenant) GetPeerCount() int32 {
	if x == nil { return 0 }
	return x.PeerCount
}
func (x *TopologyTenant) GetOnlinePeers() int32 {
	if x == nil { return 0 }
	return x.OnlinePeers
}
func (x *TopologyTenant) GetServerIp() string {
	if x == nil { return "" }
	return x.ServerIp
}
func (x *TopologyTenant) GetStatus() NodeStatus {
	if x == nil { return NodeStatus(0) }
	return x.Status
}

type HealthCheckRequest struct {
}


type HealthCheckResponse struct {
	Status HealthCheckResponse_Status `json:"status,omitempty"`
	Version string `json:"version,omitempty"`
	Components map[string]string `json:"components,omitempty"`
	Timestamp *Timestamp `json:"timestamp,omitempty"`
}

func (x *HealthCheckResponse) GetStatus() HealthCheckResponse_Status {
	if x == nil { return HealthCheckResponse_Status(0) }
	return x.Status
}
func (x *HealthCheckResponse) GetVersion() string {
	if x == nil { return "" }
	return x.Version
}
func (x *HealthCheckResponse) GetComponents() map[string]string {
	if x == nil { return nil }
	return x.Components
}
func (x *HealthCheckResponse) GetTimestamp() *Timestamp {
	if x == nil { return nil }
	return x.Timestamp
}

type CreateWebSSHSessionRequest struct {
	TenantId string `json:"tenant_id,omitempty"`
	PeerIp string `json:"peer_ip,omitempty"`
	SshPort int32 `json:"ssh_port,omitempty"`
	Username string `json:"username,omitempty"`
	Password string `json:"password,omitempty"`
	TerminalRows int32 `json:"terminal_rows,omitempty"`
	TerminalCols int32 `json:"terminal_cols,omitempty"`
	PrivateKey string `json:"private_key,omitempty"`
	PrivateKeyPassphrase string `json:"private_key_passphrase,omitempty"`
	UserAgent string `json:"user_agent,omitempty"`
}

func (x *CreateWebSSHSessionRequest) GetTenantId() string {
	if x == nil { return "" }
	return x.TenantId
}
func (x *CreateWebSSHSessionRequest) GetPeerIp() string {
	if x == nil { return "" }
	return x.PeerIp
}
func (x *CreateWebSSHSessionRequest) GetSshPort() int32 {
	if x == nil { return 0 }
	return x.SshPort
}
func (x *CreateWebSSHSessionRequest) GetUsername() string {
	if x == nil { return "" }
	return x.Username
}
func (x *CreateWebSSHSessionRequest) GetPassword() string {
	if x == nil { return "" }
	return x.Password
}
func (x *CreateWebSSHSessionRequest) GetTerminalRows() int32 {
	if x == nil { return 0 }
	return x.TerminalRows
}
func (x *CreateWebSSHSessionRequest) GetTerminalCols() int32 {
	if x == nil { return 0 }
	return x.TerminalCols
}
func (x *CreateWebSSHSessionRequest) GetPrivateKey() string {
	if x == nil { return "" }
	return x.PrivateKey
}
func (x *CreateWebSSHSessionRequest) GetPrivateKeyPassphrase() string {
	if x == nil { return "" }
	return x.PrivateKeyPassphrase
}
func (x *CreateWebSSHSessionRequest) GetUserAgent() string {
	if x == nil { return "" }
	return x.UserAgent
}

type CreateWebSSHSessionResponse struct {
	SessionId string `json:"session_id,omitempty"`
	WebsocketUrl string `json:"websocket_url,omitempty"`
	Success bool `json:"success"`
	ErrorMessage string `json:"error_message,omitempty"`
}

func (x *CreateWebSSHSessionResponse) GetSessionId() string {
	if x == nil { return "" }
	return x.SessionId
}
func (x *CreateWebSSHSessionResponse) GetWebsocketUrl() string {
	if x == nil { return "" }
	return x.WebsocketUrl
}
func (x *CreateWebSSHSessionResponse) GetSuccess() bool {
	if x == nil { return false }
	return x.Success
}
func (x *CreateWebSSHSessionResponse) GetErrorMessage() string {
	if x == nil { return "" }
	return x.ErrorMessage
}

type GetWebSSHSessionRequest struct {
	SessionId string `json:"session_id,omitempty"`
}

func (x *GetWebSSHSessionRequest) GetSessionId() string {
	if x == nil { return "" }
	return x.SessionId
}

type GetWebSSHSessionResponse struct {
	Session *WebSSHSession `json:"session,omitempty"`
}

func (x *GetWebSSHSessionResponse) GetSession() *WebSSHSession {
	if x == nil { return nil }
	return x.Session
}

type ListWebSSHSessionsRequest struct {
	TenantId string `json:"tenant_id,omitempty"`
}

func (x *ListWebSSHSessionsRequest) GetTenantId() string {
	if x == nil { return "" }
	return x.TenantId
}

type ListWebSSHSessionsResponse struct {
	Sessions []*WebSSHSession `json:"sessions,omitempty"`
}

func (x *ListWebSSHSessionsResponse) GetSessions() []*WebSSHSession {
	if x == nil { return nil }
	return x.Sessions
}

type DisconnectWebSSHSessionRequest struct {
	SessionId string `json:"session_id,omitempty"`
}

func (x *DisconnectWebSSHSessionRequest) GetSessionId() string {
	if x == nil { return "" }
	return x.SessionId
}

type DisconnectWebSSHSessionResponse struct {
	Success bool `json:"success"`
}

func (x *DisconnectWebSSHSessionResponse) GetSuccess() bool {
	if x == nil { return false }
	return x.Success
}

type WebSSHSession struct {
	Id string `json:"id,omitempty"`
	TenantId string `json:"tenant_id,omitempty"`
	PeerIp string `json:"peer_ip,omitempty"`
	SshPort int32 `json:"ssh_port,omitempty"`
	Username string `json:"username,omitempty"`
	StartedAt *Timestamp `json:"started_at,omitempty"`
	LastActive *Timestamp `json:"last_active,omitempty"`
	Active bool `json:"active"`
	BytesSent uint64 `json:"bytes_sent,omitempty"`
	BytesRecv uint64 `json:"bytes_recv,omitempty"`
	TerminalRows int32 `json:"terminal_rows,omitempty"`
	TerminalCols int32 `json:"terminal_cols,omitempty"`
	WebsocketUrl string `json:"websocket_url,omitempty"`
	ActivityLogs []*SessionActivityLog `json:"activity_logs,omitempty"`
	IsShared bool `json:"is_shared"`
	OwnerName string `json:"owner_name,omitempty"`
	ViewerCanWrite bool `json:"viewer_can_write"`
	PeerId string `json:"peer_id,omitempty"`
}

func (x *WebSSHSession) GetId() string {
	if x == nil { return "" }
	return x.Id
}
func (x *WebSSHSession) GetTenantId() string {
	if x == nil { return "" }
	return x.TenantId
}
func (x *WebSSHSession) GetPeerIp() string {
	if x == nil { return "" }
	return x.PeerIp
}
func (x *WebSSHSession) GetSshPort() int32 {
	if x == nil { return 0 }
	return x.SshPort
}
func (x *WebSSHSession) GetUsername() string {
	if x == nil { return "" }
	return x.Username
}
func (x *WebSSHSession) GetStartedAt() *Timestamp {
	if x == nil { return nil }
	return x.StartedAt
}
func (x *WebSSHSession) GetLastActive() *Timestamp {
	if x == nil { return nil }
	return x.LastActive
}
func (x *WebSSHSession) GetActive() bool {
	if x == nil { return false }
	return x.Active
}
func (x *WebSSHSession) GetBytesSent() uint64 {
	if x == nil { return 0 }
	return x.BytesSent
}
func (x *WebSSHSession) GetBytesRecv() uint64 {
	if x == nil { return 0 }
	return x.BytesRecv
}
func (x *WebSSHSession) GetTerminalRows() int32 {
	if x == nil { return 0 }
	return x.TerminalRows
}
func (x *WebSSHSession) GetTerminalCols() int32 {
	if x == nil { return 0 }
	return x.TerminalCols
}
func (x *WebSSHSession) GetWebsocketUrl() string {
	if x == nil { return "" }
	return x.WebsocketUrl
}
func (x *WebSSHSession) GetActivityLogs() []*SessionActivityLog {
	if x == nil { return nil }
	return x.ActivityLogs
}
func (x *WebSSHSession) GetIsShared() bool {
	if x == nil { return false }
	return x.IsShared
}
func (x *WebSSHSession) GetOwnerName() string {
	if x == nil { return "" }
	return x.OwnerName
}
func (x *WebSSHSession) GetViewerCanWrite() bool {
	if x == nil { return false }
	return x.ViewerCanWrite
}
func (x *WebSSHSession) GetPeerId() string {
	if x == nil { return "" }
	return x.PeerId
}

type SSHStreamMessage struct {
	SessionId string `json:"session_id,omitempty"`
	Payload isSSHStreamMessage_Payload `json:"-"`
}

func (x *SSHStreamMessage) GetSessionId() string {
	if x == nil { return "" }
	return x.SessionId
}
type isSSHStreamMessage_Payload interface { isSSHStreamMessage_Payload() }
func (x *SSHStreamMessage) GetPayload() isSSHStreamMessage_Payload {
	if x == nil { return nil }
	return x.Payload
}
type SSHStreamMessage_Input struct { Input *SSHInput `json:"input,omitempty"` }
func (*SSHStreamMessage_Input) isSSHStreamMessage_Payload() {}
func (x *SSHStreamMessage) GetInput() *SSHInput {
	if v, ok := x.GetPayload().(*SSHStreamMessage_Input); ok { return v.Input }
	return nil
}
type SSHStreamMessage_Output struct { Output *SSHOutput `json:"output,omitempty"` }
func (*SSHStreamMessage_Output) isSSHStreamMessage_Payload() {}
func (x *SSHStreamMessage) GetOutput() *SSHOutput {
	if v, ok := x.GetPayload().(*SSHStreamMessage_Output); ok { return v.Output }
	return nil
}
type SSHStreamMessage_Error struct { Error *SSHError `json:"error,omitempty"` }
func (*SSHStreamMessage_Error) isSSHStreamMessage_Payload() {}
func (x *SSHStreamMessage) GetError() *SSHError {
	if v, ok := x.GetPayload().(*SSHStreamMessage_Error); ok { return v.Error }
	return nil
}
type SSHStreamMessage_Ping struct { Ping *SSHPing `json:"ping,omitempty"` }
func (*SSHStreamMessage_Ping) isSSHStreamMessage_Payload() {}
func (x *SSHStreamMessage) GetPing() *SSHPing {
	if v, ok := x.GetPayload().(*SSHStreamMessage_Ping); ok { return v.Ping }
	return nil
}

type SSHInput struct {
	InputType isSSHInput_InputType `json:"-"`
}

type isSSHInput_InputType interface { isSSHInput_InputType() }
func (x *SSHInput) GetInputType() isSSHInput_InputType {
	if x == nil { return nil }
	return x.InputType
}
type SSHInput_Data struct { Data []byte `json:"data,omitempty"` }
func (*SSHInput_Data) isSSHInput_InputType() {}
func (x *SSHInput) GetData() []byte {
	if v, ok := x.GetInputType().(*SSHInput_Data); ok { return v.Data }
	return nil
}
type SSHInput_Resize struct { Resize *SSHResize `json:"resize,omitempty"` }
func (*SSHInput_Resize) isSSHInput_InputType() {}
func (x *SSHInput) GetResize() *SSHResize {
	if v, ok := x.GetInputType().(*SSHInput_Resize); ok { return v.Resize }
	return nil
}

type SSHResize struct {
	Rows int32 `json:"rows,omitempty"`
	Cols int32 `json:"cols,omitempty"`
}

func (x *SSHResize) GetRows() int32 {
	if x == nil { return 0 }
	return x.Rows
}
func (x *SSHResize) GetCols() int32 {
	if x == nil { return 0 }
	return x.Cols
}

type SSHOutput struct {
	Data []byte `json:"data,omitempty"`
}

func (x *SSHOutput) GetData() []byte {
	if x == nil { return nil }
	return x.Data
}

type SSHError struct {
	Code string `json:"code,omitempty"`
	Message string `json:"message,omitempty"`
	Fatal bool `json:"fatal"`
}

func (x *SSHError) GetCode() string {
	if x == nil { return "" }
	return x.Code
}
func (x *SSHError) GetMessage() string {
	if x == nil { return "" }
	return x.Message
}
func (x *SSHError) GetFatal() bool {
	if x == nil { return false }
	return x.Fatal
}

type SSHPing struct {
	Timestamp int64 `json:"timestamp,omitempty"`
}

func (x *SSHPing) GetTimestamp() int64 {
	if x == nil { return 0 }
	return x.Timestamp
}

type CreateWebProxySessionRequest struct {
	TenantId string `json:"tenant_id,omitempty"`
	PeerId string `json:"peer_id,omitempty"`
	PeerIp string `json:"peer_ip,omitempty"`
	Port int32 `json:"port,omitempty"`
	UseHttps bool `json:"use_https"`
	SkipTlsVerify bool `json:"skip_tls_verify"`
}

func (x *CreateWebProxySessionRequest) GetTenantId() string {
	if x == nil { return "" }
	return x.TenantId
}
func (x *CreateWebProxySessionRequest) GetPeerId() string {
	if x == nil { return "" }
	return x.PeerId
}
func (x *CreateWebProxySessionRequest) GetPeerIp() string {
	if x == nil { return "" }
	return x.PeerIp
}
func (x *CreateWebProxySessionRequest) GetPort() int32 {
	if x == nil { return 0 }
	return x.Port
}
func (x *CreateWebProxySessionRequest) GetUseHttps() bool {
	if x == nil { return false }
	return x.UseHttps
}
func (x *CreateWebProxySessionRequest) GetSkipTlsVerify() bool {
	if x == nil { return false }
	return x.SkipTlsVerify
}

type CreateWebProxySessionResponse struct {
	SessionId string `json:"session_id,omitempty"`
	Success bool `json:"success"`
	ErrorMessage string `json:"error_message,omitempty"`
	BaseUrl string `json:"base_url,omitempty"`
}

func (x *CreateWebProxySessionResponse) GetSessionId() string {
	if x == nil { return "" }
	return x.SessionId
}
func (x *CreateWebProxySessionResponse) GetSuccess() bool {
	if x == nil { return false }
	return x.Success
}
func (x *CreateWebProxySessionResponse) GetErrorMessage() string {
	if x == nil { return "" }
	return x.ErrorMessage
}
func (x *CreateWebProxySessionResponse) GetBaseUrl() string {
	if x == nil { return "" }
	return x.BaseUrl
}

type WebProxyStreamMessage struct {
	SessionId string `json:"session_id,omitempty"`
	RequestId string `json:"request_id,omitempty"`
	Payload isWebProxyStreamMessage_Payload `json:"-"`
}

func (x *WebProxyStreamMessage) GetSessionId() string {
	if x == nil { return "" }
	return x.SessionId
}
func (x *WebProxyStreamMessage) GetRequestId() string {
	if x == nil { return "" }
	return x.RequestId
}
type isWebProxyStreamMessage_Payload interface { isWebProxyStreamMessage_Payload() }
func (x *WebProxyStreamMessage) GetPayload() isWebProxyStreamMessage_Payload {
	if x == nil { return nil }
	return x.Payload
}
type WebProxyStreamMessage_Request struct { Request *WebProxyRequest `json:"request,omitempty"` }
func (*WebProxyStreamMessage_Request) isWebProxyStreamMessage_Payload() {}
func (x *WebProxyStreamMessage) GetRequest() *WebProxyRequest {
	if v, ok := x.GetPayload().(*WebProxyStreamMessage_Request); ok { return v.Request }
	return nil
}
type WebProxyStreamMessage_Response struct { Response *WebProxyResponse `json:"response,omitempty"` }
func (*WebProxyStreamMessage_Response) isWebProxyStreamMessage_Payload() {}
func (x *WebProxyStreamMessage) GetResponse() *WebProxyResponse {
	if v, ok := x.GetPayload().(*WebProxyStreamMessage_Response); ok { return v.Response }
	return nil
}
type WebProxyStreamMessage_Error struct { Error *WebProxyError `json:"error,omitempty"` }
func (*WebProxyStreamMessage_Error) isWebProxyStreamMessage_Payload() {}
func (x *WebProxyStreamMessage) GetError() *WebProxyError {
	if v, ok := x.GetPayload().(*WebProxyStreamMessage_Error); ok { return v.Error }
	return nil
}
type WebProxyStreamMessage_Ping struct { Ping *WebProxyPing `json:"ping,omitempty"` }
func (*WebProxyStreamMessage_Ping) isWebProxyStreamMessage_Payload() {}
func (x *WebProxyStreamMessage) GetPing() *WebProxyPing {
	if v, ok := x.GetPayload().(*WebProxyStreamMessage_Ping); ok { return v.Ping }
	return nil
}
type WebProxyStreamMessage_WebsocketFrame struct { WebsocketFrame *WebProxyWebSocketFrame `json:"websocket_frame,omitempty"` }
func (*WebProxyStreamMessage_WebsocketFrame) isWebProxyStreamMessage_Payload() {}
func (x *WebProxyStreamMessage) GetWebsocketFrame() *WebProxyWebSocketFrame {
	if v, ok := x.GetPayload().(*WebProxyStreamMessage_WebsocketFrame); ok { return v.WebsocketFrame }
	return nil
}

type WebProxyWebSocketFrame struct {
	Data []byte `json:"data,omitempty"`
	Type int32 `json:"type,omitempty"`
}

func (x *WebProxyWebSocketFrame) GetData() []byte {
	if x == nil { return nil }
	return x.Data
}
func (x *WebProxyWebSocketFrame) GetType() int32 {
	if x == nil { return 0 }
	return x.Type
}

type WebProxyRequest struct {
	Method string `json:"method,omitempty"`
	Path string `json:"path,omitempty"`
	Query string `json:"query,omitempty"`
	Headers map[string]string `json:"headers,omitempty"`
	Body []byte `json:"body,omitempty"`
	ContentType string `json:"content_type,omitempty"`
}

func (x *WebProxyRequest) GetMethod() string {
	if x == nil { return "" }
	return x.Method
}
func (x *WebProxyRequest) GetPath() string {
	if x == nil { return "" }
	return x.Path
}
func (x *WebProxyRequest) GetQuery() string {
	if x == nil { return "" }
	return x.Query
}
func (x *WebProxyRequest) GetHeaders() map[string]string {
	if x == nil { return nil }
	return x.Headers
}
func (x *WebProxyRequest) GetBody() []byte {
	if x == nil { return nil }
	return x.Body
}
func (x *WebProxyRequest) GetContentType() string {
	if x == nil { return "" }
	return x.ContentType
}

type WebProxyResponse struct {
	StatusCode int32 `json:"status_code,omitempty"`
	StatusText string `json:"status_text,omitempty"`
	Headers map[string]string `json:"headers,omitempty"`
	Body []byte `json:"body,omitempty"`
	ContentType string `json:"content_type,omitempty"`
	ContentLength int64 `json:"content_length,omitempty"`
	IsFinal bool `json:"is_final"`
	ChunkIndex int32 `json:"chunk_index,omitempty"`
}

func (x *WebProxyResponse) GetStatusCode() int32 {
	if x == nil { return 0 }
	return x.StatusCode
}
func (x *WebProxyResponse) GetStatusText() string {
	if x == nil { return "" }
	return x.StatusText
}
func (x *WebProxyResponse) GetHeaders() map[string]string {
	if x == nil { return nil }
	return x.Headers
}
func (x *WebProxyResponse) GetBody() []byte {
	if x == nil { return nil }
	return x.Body
}
func (x *WebProxyResponse) GetContentType() string {
	if x == nil { return "" }
	return x.ContentType
}
func (x *WebProxyResponse) GetContentLength() int64 {
	if x == nil { return 0 }
	return x.ContentLength
}
func (x *WebProxyResponse) GetIsFinal() bool {
	if x == nil { return false }
	return x.IsFinal
}
func (x *WebProxyResponse) GetChunkIndex() int32 {
	if x == nil { return 0 }
	return x.ChunkIndex
}

type WebProxyError struct {
	Code string `json:"code,omitempty"`
	Message string `json:"message,omitempty"`
	Retryable bool `json:"retryable"`
}

func (x *WebProxyError) GetCode() string {
	if x == nil { return "" }
	return x.Code
}
func (x *WebProxyError) GetMessage() string {
	if x == nil { return "" }
	return x.Message
}
func (x *WebProxyError) GetRetryable() bool {
	if x == nil { return false }
	return x.Retryable
}

type WebProxyPing struct {
	Timestamp int64 `json:"timestamp,omitempty"`
}

func (x *WebProxyPing) GetTimestamp() int64 {
	if x == nil { return 0 }
	return x.Timestamp
}

type GetWebProxySessionRequest struct {
	SessionId string `json:"session_id,omitempty"`
}

func (x *GetWebProxySessionRequest) GetSessionId() string {
	if x == nil { return "" }
	return x.SessionId
}

type GetWebProxySessionResponse struct {
	Session *WebProxySession `json:"session,omitempty"`
}

func (x *GetWebProxySessionResponse) GetSession() *WebProxySession {
	if x == nil { return nil }
	return x.Session
}

type ListWebProxySessionsRequest struct {
	TenantId string `json:"tenant_id,omitempty"`
}

func (x *ListWebProxySessionsRequest) GetTenantId() string {
	if x == nil { return "" }
	return x.TenantId
}

type ListWebProxySessionsResponse struct {
	Sessions []*WebProxySession `json:"sessions,omitempty"`
}

func (x *ListWebProxySessionsResponse) GetSessions() []*WebProxySession {
	if x == nil { return nil }
	return x.Sessions
}

type CloseWebProxySessionRequest struct {
	SessionId string `json:"session_id,omitempty"`
}

func (x *CloseWebProxySessionRequest) GetSessionId() string {
	if x == nil { return "" }
	return x.SessionId
}

type CloseWebProxySessionResponse struct {
	Success bool `json:"success"`
	Message string `json:"message,omitempty"`
}

func (x *CloseWebProxySessionResponse) GetSuccess() bool {
	if x == nil { return false }
	return x.Success
}
func (x *CloseWebProxySessionResponse) GetMessage() string {
	if x == nil { return "" }
	return x.Message
}

type WebProxySession struct {
	Id string `json:"id,omitempty"`
	TenantId string `json:"tenant_id,omitempty"`
	PeerId string `json:"peer_id,omitempty"`
	PeerIp string `json:"peer_ip,omitempty"`
	Port int32 `json:"port,omitempty"`
	UseHttps bool `json:"use_https"`
	CreatedAt *Timestamp `json:"created_at,omitempty"`
	LastActive *Timestamp `json:"last_active,omitempty"`
	Active bool `json:"active"`
	RequestsCount uint64 `json:"requests_count,omitempty"`
	BytesSent uint64 `json:"bytes_sent,omitempty"`
	BytesReceived uint64 `json:"bytes_received,omitempty"`
	BaseUrl string `json:"base_url,omitempty"`
}

func (x *WebProxySession) GetId() string {
	if x == nil { return "" }
	return x.Id
}
func (x *WebProxySession) GetTenantId() string {
	if x == nil { return "" }
	return x.TenantId
}
func (x *WebProxySession) GetPeerId() string {
	if x == nil { return "" }
	return x.PeerId
}
func (x *WebProxySession) GetPeerIp() string {
	if x == nil { return "" }
	return x.PeerIp
}
func (x *WebProxySession) GetPort() int32 {
	if x == nil { return 0 }
	return x.Port
}
func (x *WebProxySession) GetUseHttps() bool {
	if x == nil { return false }
	return x.UseHttps
}
func (x *WebProxySession) GetCreatedAt() *Timestamp {
	if x == nil { return nil }
	return x.CreatedAt
}
func (x *WebProxySession) GetLastActive() *Timestamp {
	if x == nil { return nil }
	return x.LastActive
}
func (x *WebProxySession) GetActive() bool {
	if x == nil { return false }
	return x.Active
}
func (x *WebProxySession) GetRequestsCount() uint64 {
	if x == nil { return 0 }
	return x.RequestsCount
}
func (x *WebProxySession) GetBytesSent() uint64 {
	if x == nil { return 0 }
	return x.BytesSent
}
func (x *WebProxySession) GetBytesReceived() uint64 {
	if x == nil { return 0 }
	return x.BytesReceived
}
func (x *WebProxySession) GetBaseUrl() string {
	if x == nil { return "" }
	return x.BaseUrl
}

type AddACLRuleRequest struct {
	AccountId string `json:"account_id,omitempty"`
	Name string `json:"name,omitempty"`
	Action string `json:"action,omitempty"`
	Protocol string `json:"protocol,omitempty"`
	SourceIps []string `json:"source_ips,omitempty"`
	DestIps []string `json:"dest_ips,omitempty"`
	DestPorts []int32 `json:"dest_ports,omitempty"`
	Priority int32 `json:"priority,omitempty"`
	Description string `json:"description,omitempty"`
}

func (x *AddACLRuleRequest) GetAccountId() string {
	if x == nil { return "" }
	return x.AccountId
}
func (x *AddACLRuleRequest) GetName() string {
	if x == nil { return "" }
	return x.Name
}
func (x *AddACLRuleRequest) GetAction() string {
	if x == nil { return "" }
	return x.Action
}
func (x *AddACLRuleRequest) GetProtocol() string {
	if x == nil { return "" }
	return x.Protocol
}
func (x *AddACLRuleRequest) GetSourceIps() []string {
	if x == nil { return nil }
	return x.SourceIps
}
func (x *AddACLRuleRequest) GetDestIps() []string {
	if x == nil { return nil }
	return x.DestIps
}
func (x *AddACLRuleRequest) GetDestPorts() []int32 {
	if x == nil { return nil }
	return x.DestPorts
}
func (x *AddACLRuleRequest) GetPriority() int32 {
	if x == nil { return 0 }
	return x.Priority
}
func (x *AddACLRuleRequest) GetDescription() string {
	if x == nil { return "" }
	return x.Description
}

type AddACLRuleResponse struct {
	Rule *ACLRule `json:"rule,omitempty"`
}

func (x *AddACLRuleResponse) GetRule() *ACLRule {
	if x == nil { return nil }
	return x.Rule
}

type RemoveACLRuleRequest struct {
	AccountId string `json:"account_id,omitempty"`
	RuleId string `json:"rule_id,omitempty"`
}

func (x *RemoveACLRuleRequest) GetAccountId() string {
	if x == nil { return "" }
	return x.AccountId
}
func (x *RemoveACLRuleRequest) GetRuleId() string {
	if x == nil { return "" }
	return x.RuleId
}

type RemoveACLRuleResponse struct {
	Success bool `json:"success"`
}

func (x *RemoveACLRuleResponse) GetSuccess() bool {
	if x == nil { return false }
	return x.Success
}

type GetACLRulesRequest struct {
	AccountId string `json:"account_id,omitempty"`
}

func (x *GetACLRulesRequest) GetAccountId() string {
	if x == nil { return "" }
	return x.AccountId
}

type GetACLRulesResponse struct {
	Rules []*ACLRule `json:"rules,omitempty"`
}

func (x *GetACLRulesResponse) GetRules() []*ACLRule {
	if x == nil { return nil }
	return x.Rules
}

type CheckAccessRequest struct {
	AccountId string `json:"account_id,omitempty"`
	Protocol string `json:"protocol,omitempty"`
	SourceIp string `json:"source_ip,omitempty"`
	DestIp string `json:"dest_ip,omitempty"`
	DestPort int32 `json:"dest_port,omitempty"`
}

func (x *CheckAccessRequest) GetAccountId() string {
	if x == nil { return "" }
	return x.AccountId
}
func (x *CheckAccessRequest) GetProtocol() string {
	if x == nil { return "" }
	return x.Protocol
}
func (x *CheckAccessRequest) GetSourceIp() string {
	if x == nil { return "" }
	return x.SourceIp
}
func (x *CheckAccessRequest) GetDestIp() string {
	if x == nil { return "" }
	return x.DestIp
}
func (x *CheckAccessRequest) GetDestPort() int32 {
	if x == nil { return 0 }
	return x.DestPort
}

type CheckAccessResponse struct {
	Allowed bool `json:"allowed"`
	MatchedRuleId string `json:"matched_rule_id,omitempty"`
	Reason string `json:"reason,omitempty"`
}

func (x *CheckAccessResponse) GetAllowed() bool {
	if x == nil { return false }
	return x.Allowed
}
func (x *CheckAccessResponse) GetMatchedRuleId() string {
	if x == nil { return "" }
	return x.MatchedRuleId
}
func (x *CheckAccessResponse) GetReason() string {
	if x == nil { return "" }
	return x.Reason
}

type ACLRule struct {
	Id string `json:"id,omitempty"`
	AccountId string `json:"account_id,omitempty"`
	Name string `json:"name,omitempty"`
	Action string `json:"action,omitempty"`
	Protocol string `json:"protocol,omitempty"`
	SourceIps []string `json:"source_ips,omitempty"`
	DestIps []string `json:"dest_ips,omitempty"`
	DestPorts []int32 `json:"dest_ports,omitempty"`
	Priority int32 `json:"priority,omitempty"`
	Description string `json:"description,omitempty"`
	CreatedAt *Timestamp `json:"created_at,omitempty"`
}

func (x *ACLRule) GetId() string {
	if x == nil { return "" }
	return x.Id
}
func (x *ACLRule) GetAccountId() string {
	if x == nil { return "" }
	return x.AccountId
}
func (x *ACLRule) GetName() string {
	if x == nil { return "" }
	return x.Name
}
func (x *ACLRule) GetAction() string {
	if x == nil { return "" }
	return x.Action
}
func (x *ACLRule) GetProtocol() string {
	if x == nil { return "" }
	return x.Protocol
}
func (x *ACLRule) GetSourceIps() []string {
	if x == nil { return nil }
	return x.SourceIps
}
func (x *ACLRule) GetDestIps() []string {
	if x == nil { return nil }
	return x.DestIps
}
func (x *ACLRule) GetDestPorts() []int32 {
	if x == nil { return nil }
	return x.DestPorts
}
func (x *ACLRule) GetPriority() int32 {
	if x == nil { return 0 }
	return x.Priority
}
func (x *ACLRule) GetDescription() string {
	if x == nil { return "" }
	return x.Description
}
func (x *ACLRule) GetCreatedAt() *Timestamp {
	if x == nil { return nil }
	return x.CreatedAt
}

type CreatePeerGroupRequest struct {
	AccountId string `json:"account_id,omitempty"`
	GroupId string `json:"group_id,omitempty"`
	Name string `json:"name,omitempty"`
	DisplayName string `json:"display_name,omitempty"`
	Description string `json:"description,omitempty"`
	AllowedProtocols []uint32 `json:"allowed_protocols,omitempty"`
}

func (x *CreatePeerGroupRequest) GetAccountId() string {
	if x == nil { return "" }
	return x.AccountId
}
func (x *CreatePeerGroupRequest) GetGroupId() string {
	if x == nil { return "" }
	return x.GroupId
}
func (x *CreatePeerGroupRequest) GetName() string {
	if x == nil { return "" }
	return x.Name
}
func (x *CreatePeerGroupRequest) GetDisplayName() string {
	if x == nil { return "" }
	return x.DisplayName
}
func (x *CreatePeerGroupRequest) GetDescription() string {
	if x == nil { return "" }
	return x.Description
}
func (x *CreatePeerGroupRequest) GetAllowedProtocols() []uint32 {
	if x == nil { return nil }
	return x.AllowedProtocols
}

type CreatePeerGroupResponse struct {
	Group *PeerGroup `json:"group,omitempty"`
}

func (x *CreatePeerGroupResponse) GetGroup() *PeerGroup {
	if x == nil { return nil }
	return x.Group
}

type DeletePeerGroupRequest struct {
	AccountId string `json:"account_id,omitempty"`
	GroupId string `json:"group_id,omitempty"`
}

func (x *DeletePeerGroupRequest) GetAccountId() string {
	if x == nil { return "" }
	return x.AccountId
}
func (x *DeletePeerGroupRequest) GetGroupId() string {
	if x == nil { return "" }
	return x.GroupId
}

type DeletePeerGroupResponse struct {
	Success bool `json:"success"`
}

func (x *DeletePeerGroupResponse) GetSuccess() bool {
	if x == nil { return false }
	return x.Success
}

type ListPeerGroupsRequest struct {
	AccountId string `json:"account_id,omitempty"`
}

func (x *ListPeerGroupsRequest) GetAccountId() string {
	if x == nil { return "" }
	return x.AccountId
}

type ListPeerGroupsResponse struct {
	Groups []*PeerGroup `json:"groups,omitempty"`
}

func (x *ListPeerGroupsResponse) GetGroups() []*PeerGroup {
	if x == nil { return nil }
	return x.Groups
}

type AddPeerToGroupRequest struct {
	AccountId string `json:"account_id,omitempty"`
	GroupId string `json:"group_id,omitempty"`
	PeerId string `json:"peer_id,omitempty"`
}

func (x *AddPeerToGroupRequest) GetAccountId() string {
	if x == nil { return "" }
	return x.AccountId
}
func (x *AddPeerToGroupRequest) GetGroupId() string {
	if x == nil { return "" }
	return x.GroupId
}
func (x *AddPeerToGroupRequest) GetPeerId() string {
	if x == nil { return "" }
	return x.PeerId
}

type AddPeerToGroupResponse struct {
	Group *PeerGroup `json:"group,omitempty"`
}

func (x *AddPeerToGroupResponse) GetGroup() *PeerGroup {
	if x == nil { return nil }
	return x.Group
}

type RemovePeerFromGroupRequest struct {
	AccountId string `json:"account_id,omitempty"`
	GroupId string `json:"group_id,omitempty"`
	PeerId string `json:"peer_id,omitempty"`
}

func (x *RemovePeerFromGroupRequest) GetAccountId() string {
	if x == nil { return "" }
	return x.AccountId
}
func (x *RemovePeerFromGroupRequest) GetGroupId() string {
	if x == nil { return "" }
	return x.GroupId
}
func (x *RemovePeerFromGroupRequest) GetPeerId() string {
	if x == nil { return "" }
	return x.PeerId
}

type RemovePeerFromGroupResponse struct {
	Group *PeerGroup `json:"group,omitempty"`
}

func (x *RemovePeerFromGroupResponse) GetGroup() *PeerGroup {
	if x == nil { return nil }
	return x.Group
}

type PeerGroup struct {
	Id string `json:"id,omitempty"`
	AccountId string `json:"account_id,omitempty"`
	Name string `json:"name,omitempty"`
	DisplayName string `json:"display_name,omitempty"`
	Description string `json:"description,omitempty"`
	PeerIds []string `json:"peer_ids,omitempty"`
	AllowedProtocols []uint32 `json:"allowed_protocols,omitempty"`
	CreatedAt *Timestamp `json:"created_at,omitempty"`
	UpdatedAt *Timestamp `json:"updated_at,omitempty"`
}

func (x *PeerGroup) GetId() string {
	if x == nil { return "" }
	return x.Id
}
func (x *PeerGroup) GetAccountId() string {
	if x == nil { return "" }
	return x.AccountId
}
func (x *PeerGroup) GetName() string {
	if x == nil { return "" }
	return x.Name
}
func (x *PeerGroup) GetDisplayName() string {
	if x == nil { return "" }
	return x.DisplayName
}
func (x *PeerGroup) GetDescription() string {
	if x == nil { return "" }
	return x.Description
}
func (x *PeerGroup) GetPeerIds() []string {
	if x == nil { return nil }
	return x.PeerIds
}
func (x *PeerGroup) GetAllowedProtocols() []uint32 {
	if x == nil { return nil }
	return x.AllowedProtocols
}
func (x *PeerGroup) GetCreatedAt() *Timestamp {
	if x == nil { return nil }
	return x.CreatedAt
}
func (x *PeerGroup) GetUpdatedAt() *Timestamp {
	if x == nil { return nil }
	return x.UpdatedAt
}

type CreateGroupLinkRequest struct {
	AccountId string `json:"account_id,omitempty"`
	LinkId string `json:"link_id,omitempty"`
	SourceGroupId string `json:"source_group_id,omitempty"`
	DestGroupId string `json:"dest_group_id,omitempty"`
	Action string `json:"action,omitempty"`
	Protocols []uint32 `json:"protocols,omitempty"`
	PortRanges []*PortRange `json:"port_ranges,omitempty"`
	Priority int32 `json:"priority,omitempty"`
}

func (x *CreateGroupLinkRequest) GetAccountId() string {
	if x == nil { return "" }
	return x.AccountId
}
func (x *CreateGroupLinkRequest) GetLinkId() string {
	if x == nil { return "" }
	return x.LinkId
}
func (x *CreateGroupLinkRequest) GetSourceGroupId() string {
	if x == nil { return "" }
	return x.SourceGroupId
}
func (x *CreateGroupLinkRequest) GetDestGroupId() string {
	if x == nil { return "" }
	return x.DestGroupId
}
func (x *CreateGroupLinkRequest) GetAction() string {
	if x == nil { return "" }
	return x.Action
}
func (x *CreateGroupLinkRequest) GetProtocols() []uint32 {
	if x == nil { return nil }
	return x.Protocols
}
func (x *CreateGroupLinkRequest) GetPortRanges() []*PortRange {
	if x == nil { return nil }
	return x.PortRanges
}
func (x *CreateGroupLinkRequest) GetPriority() int32 {
	if x == nil { return 0 }
	return x.Priority
}

type CreateGroupLinkResponse struct {
	Link *GroupLink `json:"link,omitempty"`
}

func (x *CreateGroupLinkResponse) GetLink() *GroupLink {
	if x == nil { return nil }
	return x.Link
}

type DeleteGroupLinkRequest struct {
	AccountId string `json:"account_id,omitempty"`
	LinkId string `json:"link_id,omitempty"`
}

func (x *DeleteGroupLinkRequest) GetAccountId() string {
	if x == nil { return "" }
	return x.AccountId
}
func (x *DeleteGroupLinkRequest) GetLinkId() string {
	if x == nil { return "" }
	return x.LinkId
}

type DeleteGroupLinkResponse struct {
	Success bool `json:"success"`
}

func (x *DeleteGroupLinkResponse) GetSuccess() bool {
	if x == nil { return false }
	return x.Success
}

type ListGroupLinksRequest struct {
	AccountId string `json:"account_id,omitempty"`
}

func (x *ListGroupLinksRequest) GetAccountId() string {
	if x == nil { return "" }
	return x.AccountId
}

type ListGroupLinksResponse struct {
	Links []*GroupLink `json:"links,omitempty"`
}

func (x *ListGroupLinksResponse) GetLinks() []*GroupLink {
	if x == nil { return nil }
	return x.Links
}

type GroupLink struct {
	Id string `json:"id,omitempty"`
	AccountId string `json:"account_id,omitempty"`
	SourceGroupId string `json:"source_group_id,omitempty"`
	DestGroupId string `json:"dest_group_id,omitempty"`
	Action string `json:"action,omitempty"`
	Protocols []uint32 `json:"protocols,omitempty"`
	PortRanges []*PortRange `json:"port_ranges,omitempty"`
	Priority int32 `json:"priority,omitempty"`
	CreatedAt *Timestamp `json:"created_at,omitempty"`
	UpdatedAt *Timestamp `json:"updated_at,omitempty"`
}

func (x *GroupLink) GetId() string {
	if x == nil { return "" }
	return x.Id
}
func (x *GroupLink) GetAccountId() string {
	if x == nil { return "" }
	return x.AccountId
}
func (x *GroupLink) GetSourceGroupId() string {
	if x == nil { return "" }
	return x.SourceGroupId
}
func (x *GroupLink) GetDestGroupId() string {
	if x == nil { return "" }
	return x.DestGroupId
}
func (x *GroupLink) GetAction() string {
	if x == nil { return "" }
	return x.Action
}
func (x *GroupLink) GetProtocols() []uint32 {
	if x == nil { return nil }
	return x.Protocols
}
func (x *GroupLink) GetPortRanges() []*PortRange {
	if x == nil { return nil }
	return x.PortRanges
}
func (x *GroupLink) GetPriority() int32 {
	if x == nil { return 0 }
	return x.Priority
}
func (x *GroupLink) GetCreatedAt() *Timestamp {
	if x == nil { return nil }
	return x.CreatedAt
}
func (x *GroupLink) GetUpdatedAt() *Timestamp {
	if x == nil { return nil }
	return x.UpdatedAt
}

type PortRange struct {
	Start uint32 `json:"start,omitempty"`
	End uint32 `json:"end,omitempty"`
}

func (x *PortRange) GetStart() uint32 {
	if x == nil { return 0 }
	return x.Start
}
func (x *PortRange) GetEnd() uint32 {
	if x == nil { return 0 }
	return x.End
}

type CompileGroupsRequest struct {
	AccountId string `json:"account_id,omitempty"`
}

func (x *CompileGroupsRequest) GetAccountId() string {
	if x == nil { return "" }
	return x.AccountId
}

type CompileGroupsResponse struct {
	GeneratedRules []*ACLRule `json:"generated_rules,omitempty"`
	Stats *CompilationStats `json:"stats,omitempty"`
}

func (x *CompileGroupsResponse) GetGeneratedRules() []*ACLRule {
	if x == nil { return nil }
	return x.GeneratedRules
}
func (x *CompileGroupsResponse) GetStats() *CompilationStats {
	if x == nil { return nil }
	return x.Stats
}

type GetCompilationStatsRequest struct {
	AccountId string `json:"account_id,omitempty"`
}

func (x *GetCompilationStatsRequest) GetAccountId() string {
	if x == nil { return "" }
	return x.AccountId
}

type GetCompilationStatsResponse struct {
	Stats *CompilationStats `json:"stats,omitempty"`
}

func (x *GetCompilationStatsResponse) GetStats() *CompilationStats {
	if x == nil { return nil }
	return x.Stats
}

type CompilationStats struct {
	Groups int32 `json:"groups,omitempty"`
	Links int32 `json:"links,omitempty"`
	TotalPeers int32 `json:"total_peers,omitempty"`
	EstimatedRules int32 `json:"estimated_rules,omitempty"`
}

func (x *CompilationStats) GetGroups() int32 {
	if x == nil { return 0 }
	return x.Groups
}
func (x *CompilationStats) GetLinks() int32 {
	if x == nil { return 0 }
	return x.Links
}
func (x *CompilationStats) GetTotalPeers() int32 {
	if x == nil { return 0 }
	return x.TotalPeers
}
func (x *CompilationStats) GetEstimatedRules() int32 {
	if x == nil { return 0 }
	return x.EstimatedRules
}

type GetPaymentStatusRequest struct {
}


type GetPaymentStatusResponse struct {
	StripeReady bool `json:"stripe_ready"`
	PaidPlansAvailable bool `json:"paid_plans_available"`
	Message string `json:"message,omitempty"`
	StripePublishableKey string `json:"stripe_publishable_key,omitempty"`
}

func (x *GetPaymentStatusResponse) GetStripeReady() bool {
	if x == nil { return false }
	return x.StripeReady
}
func (x *GetPaymentStatusResponse) GetPaidPlansAvailable() bool {
	if x == nil { return false }
	return x.PaidPlansAvailable
}
func (x *GetPaymentStatusResponse) GetMessage() string {
	if x == nil { return "" }
	return x.Message
}
func (x *GetPaymentStatusResponse) GetStripePublishableKey() string {
	if x == nil { return "" }
	return x.StripePublishableKey
}

type GetAllowedPhoneRegionsRequest struct {
}


type PhoneRegion struct {
	CountryCode string `json:"country_code,omitempty"`
	DialCode string `json:"dial_code,omitempty"`
	CountryName string `json:"country_name,omitempty"`
	FlagEmoji string `json:"flag_emoji,omitempty"`
}

func (x *PhoneRegion) GetCountryCode() string {
	if x == nil { return "" }
	return x.CountryCode
}
func (x *PhoneRegion) GetDialCode() string {
	if x == nil { return "" }
	return x.DialCode
}
func (x *PhoneRegion) GetCountryName() string {
	if x == nil { return "" }
	return x.CountryName
}
func (x *PhoneRegion) GetFlagEmoji() string {
	if x == nil { return "" }
	return x.FlagEmoji
}

type GetAllowedPhoneRegionsResponse struct {
	Regions []*PhoneRegion `json:"regions,omitempty"`
	AllRegionsAllowed bool `json:"all_regions_allowed"`
}

func (x *GetAllowedPhoneRegionsResponse) GetRegions() []*PhoneRegion {
	if x == nil { return nil }
	return x.Regions
}
func (x *GetAllowedPhoneRegionsResponse) GetAllRegionsAllowed() bool {
	if x == nil { return false }
	return x.AllRegionsAllowed
}

type GetAvailablePlansRequest struct {
}


type PlanInfo struct {
	Tier AccountTier `json:"tier,omitempty"`
	Name string `json:"name,omitempty"`
	PriceCents int64 `json:"price_cents,omitempty"`
	Currency string `json:"currency,omitempty"`
	BlockCount int32 `json:"block_count,omitempty"`
	MaxPeers int32 `json:"max_peers,omitempty"`
	Features []string `json:"features,omitempty"`
	TrialDays int32 `json:"trial_days,omitempty"`
	IsPopular bool `json:"is_popular"`
}

func (x *PlanInfo) GetTier() AccountTier {
	if x == nil { return AccountTier(0) }
	return x.Tier
}
func (x *PlanInfo) GetName() string {
	if x == nil { return "" }
	return x.Name
}
func (x *PlanInfo) GetPriceCents() int64 {
	if x == nil { return 0 }
	return x.PriceCents
}
func (x *PlanInfo) GetCurrency() string {
	if x == nil { return "" }
	return x.Currency
}
func (x *PlanInfo) GetBlockCount() int32 {
	if x == nil { return 0 }
	return x.BlockCount
}
func (x *PlanInfo) GetMaxPeers() int32 {
	if x == nil { return 0 }
	return x.MaxPeers
}
func (x *PlanInfo) GetFeatures() []string {
	if x == nil { return nil }
	return x.Features
}
func (x *PlanInfo) GetTrialDays() int32 {
	if x == nil { return 0 }
	return x.TrialDays
}
func (x *PlanInfo) GetIsPopular() bool {
	if x == nil { return false }
	return x.IsPopular
}

type GetAvailablePlansResponse struct {
	Plans []*PlanInfo `json:"plans,omitempty"`
	StripeReady bool `json:"stripe_ready"`
	StripePublishableKey string `json:"stripe_publishable_key,omitempty"`
}

func (x *GetAvailablePlansResponse) GetPlans() []*PlanInfo {
	if x == nil { return nil }
	return x.Plans
}
func (x *GetAvailablePlansResponse) GetStripeReady() bool {
	if x == nil { return false }
	return x.StripeReady
}
func (x *GetAvailablePlansResponse) GetStripePublishableKey() string {
	if x == nil { return "" }
	return x.StripePublishableKey
}

type CreateSetupIntentRequest struct {
	RegistrationId string `json:"registration_id,omitempty"`
	CustomerId string `json:"customer_id,omitempty"`
	PaymentMethodTypes []string `json:"payment_method_types,omitempty"`
}

func (x *CreateSetupIntentRequest) GetRegistrationId() string {
	if x == nil { return "" }
	return x.RegistrationId
}
func (x *CreateSetupIntentRequest) GetCustomerId() string {
	if x == nil { return "" }
	return x.CustomerId
}
func (x *CreateSetupIntentRequest) GetPaymentMethodTypes() []string {
	if x == nil { return nil }
	return x.PaymentMethodTypes
}

type CreateSetupIntentResponse struct {
	ClientSecret string `json:"client_secret,omitempty"`
	SetupIntentId string `json:"setup_intent_id,omitempty"`
	PublishableKey string `json:"publishable_key,omitempty"`
}

func (x *CreateSetupIntentResponse) GetClientSecret() string {
	if x == nil { return "" }
	return x.ClientSecret
}
func (x *CreateSetupIntentResponse) GetSetupIntentId() string {
	if x == nil { return "" }
	return x.SetupIntentId
}
func (x *CreateSetupIntentResponse) GetPublishableKey() string {
	if x == nil { return "" }
	return x.PublishableKey
}

type CAPTCHAProblem struct {
	ProblemId string `json:"problem_id,omitempty"`
	ImageBase64 string `json:"image_base64,omitempty"`
	Difficulty string `json:"difficulty,omitempty"`
	CreatedAtUnix int64 `json:"created_at_unix,omitempty"`
	ExpiresAtUnix int64 `json:"expires_at_unix,omitempty"`
}

func (x *CAPTCHAProblem) GetProblemId() string {
	if x == nil { return "" }
	return x.ProblemId
}
func (x *CAPTCHAProblem) GetImageBase64() string {
	if x == nil { return "" }
	return x.ImageBase64
}
func (x *CAPTCHAProblem) GetDifficulty() string {
	if x == nil { return "" }
	return x.Difficulty
}
func (x *CAPTCHAProblem) GetCreatedAtUnix() int64 {
	if x == nil { return 0 }
	return x.CreatedAtUnix
}
func (x *CAPTCHAProblem) GetExpiresAtUnix() int64 {
	if x == nil { return 0 }
	return x.ExpiresAtUnix
}

type CaptchaVerifyRequest struct {
	ProblemId string `json:"problem_id,omitempty"`
	Answer string `json:"answer,omitempty"`
}

func (x *CaptchaVerifyRequest) GetProblemId() string {
	if x == nil { return "" }
	return x.ProblemId
}
func (x *CaptchaVerifyRequest) GetAnswer() string {
	if x == nil { return "" }
	return x.Answer
}

type CaptchaVerifyResponse struct {
	Verified bool `json:"verified"`
	Message string `json:"message,omitempty"`
	AttemptsRemaining int32 `json:"attempts_remaining,omitempty"`
}

func (x *CaptchaVerifyResponse) GetVerified() bool {
	if x == nil { return false }
	return x.Verified
}
func (x *CaptchaVerifyResponse) GetMessage() string {
	if x == nil { return "" }
	return x.Message
}
func (x *CaptchaVerifyResponse) GetAttemptsRemaining() int32 {
	if x == nil { return 0 }
	return x.AttemptsRemaining
}

type AuthSessionInfo struct {
	SessionId string `json:"session_id,omitempty"`
	SessionType string `json:"session_type,omitempty"`
	CreatedAt string `json:"created_at,omitempty"`
	ExpiresAtUnix int64 `json:"expires_at_unix,omitempty"`
	CaptchaSolved bool `json:"captcha_solved"`
}

func (x *AuthSessionInfo) GetSessionId() string {
	if x == nil { return "" }
	return x.SessionId
}
func (x *AuthSessionInfo) GetSessionType() string {
	if x == nil { return "" }
	return x.SessionType
}
func (x *AuthSessionInfo) GetCreatedAt() string {
	if x == nil { return "" }
	return x.CreatedAt
}
func (x *AuthSessionInfo) GetExpiresAtUnix() int64 {
	if x == nil { return 0 }
	return x.ExpiresAtUnix
}
func (x *AuthSessionInfo) GetCaptchaSolved() bool {
	if x == nil { return false }
	return x.CaptchaSolved
}

type StartRegistrationRequest struct {
	Email string `json:"email,omitempty"`
	FullName string `json:"full_name,omitempty"`
	Phone string `json:"phone,omitempty"`
	Tier AccountTier `json:"tier,omitempty"`
	Password string `json:"password,omitempty"`
	SessionId string `json:"session_id,omitempty"`
}

func (x *StartRegistrationRequest) GetEmail() string {
	if x == nil { return "" }
	return x.Email
}
func (x *StartRegistrationRequest) GetFullName() string {
	if x == nil { return "" }
	return x.FullName
}
func (x *StartRegistrationRequest) GetPhone() string {
	if x == nil { return "" }
	return x.Phone
}
func (x *StartRegistrationRequest) GetTier() AccountTier {
	if x == nil { return AccountTier(0) }
	return x.Tier
}
func (x *StartRegistrationRequest) GetPassword() string {
	if x == nil { return "" }
	return x.Password
}
func (x *StartRegistrationRequest) GetSessionId() string {
	if x == nil { return "" }
	return x.SessionId
}

type StartRegistrationResponse struct {
	RegistrationId string `json:"registration_id,omitempty"`
	SessionId string `json:"session_id,omitempty"`
	PhoneVerificationSent bool `json:"phone_verification_sent"`
	Message string `json:"message,omitempty"`
	EmailFallbackAvailable bool `json:"email_fallback_available"`
	VerificationChannel string `json:"verification_channel,omitempty"`
	Error string `json:"error,omitempty"`
	ErrorCode string `json:"error_code,omitempty"`
	RateLimited bool `json:"rate_limited"`
	RetryAfter int32 `json:"retry_after,omitempty"`
	CaptchaChallenge *CAPTCHAProblem `json:"captcha_challenge,omitempty"`
	CaptchaRequired bool `json:"captcha_required"`
}

func (x *StartRegistrationResponse) GetRegistrationId() string {
	if x == nil { return "" }
	return x.RegistrationId
}
func (x *StartRegistrationResponse) GetSessionId() string {
	if x == nil { return "" }
	return x.SessionId
}
func (x *StartRegistrationResponse) GetPhoneVerificationSent() bool {
	if x == nil { return false }
	return x.PhoneVerificationSent
}
func (x *StartRegistrationResponse) GetMessage() string {
	if x == nil { return "" }
	return x.Message
}
func (x *StartRegistrationResponse) GetEmailFallbackAvailable() bool {
	if x == nil { return false }
	return x.EmailFallbackAvailable
}
func (x *StartRegistrationResponse) GetVerificationChannel() string {
	if x == nil { return "" }
	return x.VerificationChannel
}
func (x *StartRegistrationResponse) GetError() string {
	if x == nil { return "" }
	return x.Error
}
func (x *StartRegistrationResponse) GetErrorCode() string {
	if x == nil { return "" }
	return x.ErrorCode
}
func (x *StartRegistrationResponse) GetRateLimited() bool {
	if x == nil { return false }
	return x.RateLimited
}
func (x *StartRegistrationResponse) GetRetryAfter() int32 {
	if x == nil { return 0 }
	return x.RetryAfter
}
func (x *StartRegistrationResponse) GetCaptchaChallenge() *CAPTCHAProblem {
	if x == nil { return nil }
	return x.CaptchaChallenge
}
func (x *StartRegistrationResponse) GetCaptchaRequired() bool {
	if x == nil { return false }
	return x.CaptchaRequired
}

type VerifyPhoneRequest struct {
	RegistrationId string `json:"registration_id,omitempty"`
	VerificationCode string `json:"verification_code,omitempty"`
	VerificationChannel string `json:"verification_channel,omitempty"`
}

func (x *VerifyPhoneRequest) GetRegistrationId() string {
	if x == nil { return "" }
	return x.RegistrationId
}
func (x *VerifyPhoneRequest) GetVerificationCode() string {
	if x == nil { return "" }
	return x.VerificationCode
}
func (x *VerifyPhoneRequest) GetVerificationChannel() string {
	if x == nil { return "" }
	return x.VerificationChannel
}

type VerifyPhoneResponse struct {
	Verified bool `json:"verified"`
	Message string `json:"message,omitempty"`
	ReadyForPayment bool `json:"ready_for_payment"`
	AccountCreated bool `json:"account_created"`
	TenantId string `json:"tenant_id,omitempty"`
	SessionToken string `json:"session_token,omitempty"`
	Email string `json:"email,omitempty"`
	FullName string `json:"full_name,omitempty"`
	Tier string `json:"tier,omitempty"`
}

func (x *VerifyPhoneResponse) GetVerified() bool {
	if x == nil { return false }
	return x.Verified
}
func (x *VerifyPhoneResponse) GetMessage() string {
	if x == nil { return "" }
	return x.Message
}
func (x *VerifyPhoneResponse) GetReadyForPayment() bool {
	if x == nil { return false }
	return x.ReadyForPayment
}
func (x *VerifyPhoneResponse) GetAccountCreated() bool {
	if x == nil { return false }
	return x.AccountCreated
}
func (x *VerifyPhoneResponse) GetTenantId() string {
	if x == nil { return "" }
	return x.TenantId
}
func (x *VerifyPhoneResponse) GetSessionToken() string {
	if x == nil { return "" }
	return x.SessionToken
}
func (x *VerifyPhoneResponse) GetEmail() string {
	if x == nil { return "" }
	return x.Email
}
func (x *VerifyPhoneResponse) GetFullName() string {
	if x == nil { return "" }
	return x.FullName
}
func (x *VerifyPhoneResponse) GetTier() string {
	if x == nil { return "" }
	return x.Tier
}

type CompleteRegistrationRequest struct {
	RegistrationId string `json:"registration_id,omitempty"`
	Password string `json:"password,omitempty"`
	StripePaymentIntent string `json:"stripe_payment_intent,omitempty"`
	EnableTotp bool `json:"enable_totp"`
}

func (x *CompleteRegistrationRequest) GetRegistrationId() string {
	if x == nil { return "" }
	return x.RegistrationId
}
func (x *CompleteRegistrationRequest) GetPassword() string {
	if x == nil { return "" }
	return x.Password
}
func (x *CompleteRegistrationRequest) GetStripePaymentIntent() string {
	if x == nil { return "" }
	return x.StripePaymentIntent
}
func (x *CompleteRegistrationRequest) GetEnableTotp() bool {
	if x == nil { return false }
	return x.EnableTotp
}

type CompleteRegistrationResponse struct {
	Success bool `json:"success"`
	TenantId string `json:"tenant_id,omitempty"`
	Message string `json:"message,omitempty"`
	TotpProvisioningUrl string `json:"totp_provisioning_url,omitempty"`
	TotpSecret string `json:"totp_secret,omitempty"`
	RequiresCheckout bool `json:"requires_checkout"`
	CheckoutUrl string `json:"checkout_url,omitempty"`
	SessionToken string `json:"session_token,omitempty"`
	Email string `json:"email,omitempty"`
	FullName string `json:"full_name,omitempty"`
	Tier string `json:"tier,omitempty"`
}

func (x *CompleteRegistrationResponse) GetSuccess() bool {
	if x == nil { return false }
	return x.Success
}
func (x *CompleteRegistrationResponse) GetTenantId() string {
	if x == nil { return "" }
	return x.TenantId
}
func (x *CompleteRegistrationResponse) GetMessage() string {
	if x == nil { return "" }
	return x.Message
}
func (x *CompleteRegistrationResponse) GetTotpProvisioningUrl() string {
	if x == nil { return "" }
	return x.TotpProvisioningUrl
}
func (x *CompleteRegistrationResponse) GetTotpSecret() string {
	if x == nil { return "" }
	return x.TotpSecret
}
func (x *CompleteRegistrationResponse) GetRequiresCheckout() bool {
	if x == nil { return false }
	return x.RequiresCheckout
}
func (x *CompleteRegistrationResponse) GetCheckoutUrl() string {
	if x == nil { return "" }
	return x.CheckoutUrl
}
func (x *CompleteRegistrationResponse) GetSessionToken() string {
	if x == nil { return "" }
	return x.SessionToken
}
func (x *CompleteRegistrationResponse) GetEmail() string {
	if x == nil { return "" }
	return x.Email
}
func (x *CompleteRegistrationResponse) GetFullName() string {
	if x == nil { return "" }
	return x.FullName
}
func (x *CompleteRegistrationResponse) GetTier() string {
	if x == nil { return "" }
	return x.Tier
}

type CreateCheckoutSessionRequest struct {
	RegistrationId string `json:"registration_id,omitempty"`
	Tier AccountTier `json:"tier,omitempty"`
	SuccessUrl string `json:"success_url,omitempty"`
	CancelUrl string `json:"cancel_url,omitempty"`
}

func (x *CreateCheckoutSessionRequest) GetRegistrationId() string {
	if x == nil { return "" }
	return x.RegistrationId
}
func (x *CreateCheckoutSessionRequest) GetTier() AccountTier {
	if x == nil { return AccountTier(0) }
	return x.Tier
}
func (x *CreateCheckoutSessionRequest) GetSuccessUrl() string {
	if x == nil { return "" }
	return x.SuccessUrl
}
func (x *CreateCheckoutSessionRequest) GetCancelUrl() string {
	if x == nil { return "" }
	return x.CancelUrl
}

type CreateCheckoutSessionResponse struct {
	SessionId string `json:"session_id,omitempty"`
	CheckoutUrl string `json:"checkout_url,omitempty"`
	PublishableKey string `json:"publishable_key,omitempty"`
}

func (x *CreateCheckoutSessionResponse) GetSessionId() string {
	if x == nil { return "" }
	return x.SessionId
}
func (x *CreateCheckoutSessionResponse) GetCheckoutUrl() string {
	if x == nil { return "" }
	return x.CheckoutUrl
}
func (x *CreateCheckoutSessionResponse) GetPublishableKey() string {
	if x == nil { return "" }
	return x.PublishableKey
}

type GetRegistrationStatusRequest struct {
	RegistrationId string `json:"registration_id,omitempty"`
}

func (x *GetRegistrationStatusRequest) GetRegistrationId() string {
	if x == nil { return "" }
	return x.RegistrationId
}

type GetRegistrationStatusResponse struct {
	Status string `json:"status,omitempty"`
	PhoneVerified bool `json:"phone_verified"`
	PaymentComplete bool `json:"payment_complete"`
	Tier AccountTier `json:"tier,omitempty"`
	Email string `json:"email,omitempty"`
}

func (x *GetRegistrationStatusResponse) GetStatus() string {
	if x == nil { return "" }
	return x.Status
}
func (x *GetRegistrationStatusResponse) GetPhoneVerified() bool {
	if x == nil { return false }
	return x.PhoneVerified
}
func (x *GetRegistrationStatusResponse) GetPaymentComplete() bool {
	if x == nil { return false }
	return x.PaymentComplete
}
func (x *GetRegistrationStatusResponse) GetTier() AccountTier {
	if x == nil { return AccountTier(0) }
	return x.Tier
}
func (x *GetRegistrationStatusResponse) GetEmail() string {
	if x == nil { return "" }
	return x.Email
}

type ResendPhoneVerificationRequest struct {
	RegistrationId string `json:"registration_id,omitempty"`
	UseVoice bool `json:"use_voice"`
	UseEmail bool `json:"use_email"`
}

func (x *ResendPhoneVerificationRequest) GetRegistrationId() string {
	if x == nil { return "" }
	return x.RegistrationId
}
func (x *ResendPhoneVerificationRequest) GetUseVoice() bool {
	if x == nil { return false }
	return x.UseVoice
}
func (x *ResendPhoneVerificationRequest) GetUseEmail() bool {
	if x == nil { return false }
	return x.UseEmail
}

type ResendPhoneVerificationResponse struct {
	Sent bool `json:"sent"`
	Message string `json:"message,omitempty"`
	RetryAfterSeconds int32 `json:"retry_after_seconds,omitempty"`
	ResendsRemaining int32 `json:"resends_remaining,omitempty"`
	VoiceAvailable bool `json:"voice_available"`
	EmailFallbackAvailable bool `json:"email_fallback_available"`
	VerificationChannel string `json:"verification_channel,omitempty"`
}

func (x *ResendPhoneVerificationResponse) GetSent() bool {
	if x == nil { return false }
	return x.Sent
}
func (x *ResendPhoneVerificationResponse) GetMessage() string {
	if x == nil { return "" }
	return x.Message
}
func (x *ResendPhoneVerificationResponse) GetRetryAfterSeconds() int32 {
	if x == nil { return 0 }
	return x.RetryAfterSeconds
}
func (x *ResendPhoneVerificationResponse) GetResendsRemaining() int32 {
	if x == nil { return 0 }
	return x.ResendsRemaining
}
func (x *ResendPhoneVerificationResponse) GetVoiceAvailable() bool {
	if x == nil { return false }
	return x.VoiceAvailable
}
func (x *ResendPhoneVerificationResponse) GetEmailFallbackAvailable() bool {
	if x == nil { return false }
	return x.EmailFallbackAvailable
}
func (x *ResendPhoneVerificationResponse) GetVerificationChannel() string {
	if x == nil { return "" }
	return x.VerificationChannel
}

type ProcessStripeWebhookRequest struct {
	Body []byte `json:"body,omitempty"`
	Signature string `json:"signature,omitempty"`
}

func (x *ProcessStripeWebhookRequest) GetBody() []byte {
	if x == nil { return nil }
	return x.Body
}
func (x *ProcessStripeWebhookRequest) GetSignature() string {
	if x == nil { return "" }
	return x.Signature
}

type ProcessStripeWebhookResponse struct {
	Success bool `json:"success"`
	Message string `json:"message,omitempty"`
}

func (x *ProcessStripeWebhookResponse) GetSuccess() bool {
	if x == nil { return false }
	return x.Success
}
func (x *ProcessStripeWebhookResponse) GetMessage() string {
	if x == nil { return "" }
	return x.Message
}

type ProcessTwilioWebhookRequest struct {
	Url string `json:"url,omitempty"`
	Method string `json:"method,omitempty"`
	Headers map[string]string `json:"headers,omitempty"`
	FormValues map[string]string `json:"form_values,omitempty"`
}

func (x *ProcessTwilioWebhookRequest) GetUrl() string {
	if x == nil { return "" }
	return x.Url
}
func (x *ProcessTwilioWebhookRequest) GetMethod() string {
	if x == nil { return "" }
	return x.Method
}
func (x *ProcessTwilioWebhookRequest) GetHeaders() map[string]string {
	if x == nil { return nil }
	return x.Headers
}
func (x *ProcessTwilioWebhookRequest) GetFormValues() map[string]string {
	if x == nil { return nil }
	return x.FormValues
}

type ProcessTwilioWebhookResponse struct {
	Success bool `json:"success"`
	Message string `json:"message,omitempty"`
}

func (x *ProcessTwilioWebhookResponse) GetSuccess() bool {
	if x == nil { return false }
	return x.Success
}
func (x *ProcessTwilioWebhookResponse) GetMessage() string {
	if x == nil { return "" }
	return x.Message
}

type ContactSalesRequest struct {
	TenantId string `json:"tenant_id,omitempty"`
	Message string `json:"message,omitempty"`
}

func (x *ContactSalesRequest) GetTenantId() string {
	if x == nil { return "" }
	return x.TenantId
}
func (x *ContactSalesRequest) GetMessage() string {
	if x == nil { return "" }
	return x.Message
}

type ContactSalesResponse struct {
	Success bool `json:"success"`
	Message string `json:"message,omitempty"`
}

func (x *ContactSalesResponse) GetSuccess() bool {
	if x == nil { return false }
	return x.Success
}
func (x *ContactSalesResponse) GetMessage() string {
	if x == nil { return "" }
	return x.Message
}

type GetSubscriptionStatusRequest struct {
	TenantId string `json:"tenant_id,omitempty"`
}

func (x *GetSubscriptionStatusRequest) GetTenantId() string {
	if x == nil { return "" }
	return x.TenantId
}

type GetSubscriptionStatusResponse struct {
	CurrentTier AccountTier `json:"current_tier,omitempty"`
	SubscriptionStatus string `json:"subscription_status,omitempty"`
	CurrentPeriodEnd *Timestamp `json:"current_period_end,omitempty"`
	CancelAtPeriodEnd bool `json:"cancel_at_period_end"`
	MonthlyPriceCents int64 `json:"monthly_price_cents,omitempty"`
	Currency string `json:"currency,omitempty"`
	TrialEnd *Timestamp `json:"trial_end,omitempty"`
	PaymentMethods []*PaymentMethod `json:"payment_methods,omitempty"`
	CustomerEmail string `json:"customer_email,omitempty"`
	CustomerName string `json:"customer_name,omitempty"`
	CancelledAt *Timestamp `json:"cancelled_at,omitempty"`
	CancellationReason string `json:"cancellation_reason,omitempty"`
}

func (x *GetSubscriptionStatusResponse) GetCurrentTier() AccountTier {
	if x == nil { return AccountTier(0) }
	return x.CurrentTier
}
func (x *GetSubscriptionStatusResponse) GetSubscriptionStatus() string {
	if x == nil { return "" }
	return x.SubscriptionStatus
}
func (x *GetSubscriptionStatusResponse) GetCurrentPeriodEnd() *Timestamp {
	if x == nil { return nil }
	return x.CurrentPeriodEnd
}
func (x *GetSubscriptionStatusResponse) GetCancelAtPeriodEnd() bool {
	if x == nil { return false }
	return x.CancelAtPeriodEnd
}
func (x *GetSubscriptionStatusResponse) GetMonthlyPriceCents() int64 {
	if x == nil { return 0 }
	return x.MonthlyPriceCents
}
func (x *GetSubscriptionStatusResponse) GetCurrency() string {
	if x == nil { return "" }
	return x.Currency
}
func (x *GetSubscriptionStatusResponse) GetTrialEnd() *Timestamp {
	if x == nil { return nil }
	return x.TrialEnd
}
func (x *GetSubscriptionStatusResponse) GetPaymentMethods() []*PaymentMethod {
	if x == nil { return nil }
	return x.PaymentMethods
}
func (x *GetSubscriptionStatusResponse) GetCustomerEmail() string {
	if x == nil { return "" }
	return x.CustomerEmail
}
func (x *GetSubscriptionStatusResponse) GetCustomerName() string {
	if x == nil { return "" }
	return x.CustomerName
}
func (x *GetSubscriptionStatusResponse) GetCancelledAt() *Timestamp {
	if x == nil { return nil }
	return x.CancelledAt
}
func (x *GetSubscriptionStatusResponse) GetCancellationReason() string {
	if x == nil { return "" }
	return x.CancellationReason
}

type PaymentMethod struct {
	Id string `json:"id,omitempty"`
	Card *CardDetails `json:"card,omitempty"`
	IsDefault bool `json:"is_default"`
	CreatedAt int64 `json:"created_at,omitempty"`
}

func (x *PaymentMethod) GetId() string {
	if x == nil { return "" }
	return x.Id
}
func (x *PaymentMethod) GetCard() *CardDetails {
	if x == nil { return nil }
	return x.Card
}
func (x *PaymentMethod) GetIsDefault() bool {
	if x == nil { return false }
	return x.IsDefault
}
func (x *PaymentMethod) GetCreatedAt() int64 {
	if x == nil { return 0 }
	return x.CreatedAt
}

type CardDetails struct {
	Brand string `json:"brand,omitempty"`
	Last4 string `json:"last4,omitempty"`
	ExpMonth int32 `json:"exp_month,omitempty"`
	ExpYear int32 `json:"exp_year,omitempty"`
}

func (x *CardDetails) GetBrand() string {
	if x == nil { return "" }
	return x.Brand
}
func (x *CardDetails) GetLast4() string {
	if x == nil { return "" }
	return x.Last4
}
func (x *CardDetails) GetExpMonth() int32 {
	if x == nil { return 0 }
	return x.ExpMonth
}
func (x *CardDetails) GetExpYear() int32 {
	if x == nil { return 0 }
	return x.ExpYear
}

type ChangeTierRequest struct {
	TenantId string `json:"tenant_id,omitempty"`
	NewTier AccountTier `json:"new_tier,omitempty"`
	PromoCode string `json:"promo_code,omitempty"`
	ReturnUrl string `json:"return_url,omitempty"`
}

func (x *ChangeTierRequest) GetTenantId() string {
	if x == nil { return "" }
	return x.TenantId
}
func (x *ChangeTierRequest) GetNewTier() AccountTier {
	if x == nil { return AccountTier(0) }
	return x.NewTier
}
func (x *ChangeTierRequest) GetPromoCode() string {
	if x == nil { return "" }
	return x.PromoCode
}
func (x *ChangeTierRequest) GetReturnUrl() string {
	if x == nil { return "" }
	return x.ReturnUrl
}

type ChangeTierResponse struct {
	Success bool `json:"success"`
	Message string `json:"message,omitempty"`
	CheckoutUrl string `json:"checkout_url,omitempty"`
	RequiresCheckout bool `json:"requires_checkout"`
	ClientSecret string `json:"client_secret,omitempty"`
	SubscriptionId string `json:"subscription_id,omitempty"`
	PublishableKey string `json:"publishable_key,omitempty"`
}

func (x *ChangeTierResponse) GetSuccess() bool {
	if x == nil { return false }
	return x.Success
}
func (x *ChangeTierResponse) GetMessage() string {
	if x == nil { return "" }
	return x.Message
}
func (x *ChangeTierResponse) GetCheckoutUrl() string {
	if x == nil { return "" }
	return x.CheckoutUrl
}
func (x *ChangeTierResponse) GetRequiresCheckout() bool {
	if x == nil { return false }
	return x.RequiresCheckout
}
func (x *ChangeTierResponse) GetClientSecret() string {
	if x == nil { return "" }
	return x.ClientSecret
}
func (x *ChangeTierResponse) GetSubscriptionId() string {
	if x == nil { return "" }
	return x.SubscriptionId
}
func (x *ChangeTierResponse) GetPublishableKey() string {
	if x == nil { return "" }
	return x.PublishableKey
}

type GetBillingPortalRequest struct {
	TenantId string `json:"tenant_id,omitempty"`
	ReturnUrl string `json:"return_url,omitempty"`
}

func (x *GetBillingPortalRequest) GetTenantId() string {
	if x == nil { return "" }
	return x.TenantId
}
func (x *GetBillingPortalRequest) GetReturnUrl() string {
	if x == nil { return "" }
	return x.ReturnUrl
}

type GetBillingPortalResponse struct {
	PortalUrl string `json:"portal_url,omitempty"`
}

func (x *GetBillingPortalResponse) GetPortalUrl() string {
	if x == nil { return "" }
	return x.PortalUrl
}

type CancelSubscriptionRequest struct {
	TenantId string `json:"tenant_id,omitempty"`
	CancelImmediately bool `json:"cancel_immediately"`
}

func (x *CancelSubscriptionRequest) GetTenantId() string {
	if x == nil { return "" }
	return x.TenantId
}
func (x *CancelSubscriptionRequest) GetCancelImmediately() bool {
	if x == nil { return false }
	return x.CancelImmediately
}

type CancelSubscriptionResponse struct {
	Success bool `json:"success"`
	Message string `json:"message,omitempty"`
	CancelledAt *Timestamp `json:"cancelled_at,omitempty"`
}

func (x *CancelSubscriptionResponse) GetSuccess() bool {
	if x == nil { return false }
	return x.Success
}
func (x *CancelSubscriptionResponse) GetMessage() string {
	if x == nil { return "" }
	return x.Message
}
func (x *CancelSubscriptionResponse) GetCancelledAt() *Timestamp {
	if x == nil { return nil }
	return x.CancelledAt
}

type GetBillingHistoryRequest struct {
	TenantId string `json:"tenant_id,omitempty"`
	Limit int32 `json:"limit,omitempty"`
}

func (x *GetBillingHistoryRequest) GetTenantId() string {
	if x == nil { return "" }
	return x.TenantId
}
func (x *GetBillingHistoryRequest) GetLimit() int32 {
	if x == nil { return 0 }
	return x.Limit
}

type GetBillingHistoryResponse struct {
	Invoices []*Invoice `json:"invoices,omitempty"`
}

func (x *GetBillingHistoryResponse) GetInvoices() []*Invoice {
	if x == nil { return nil }
	return x.Invoices
}

type Invoice struct {
	Id string `json:"id,omitempty"`
	AmountCents int64 `json:"amount_cents,omitempty"`
	Currency string `json:"currency,omitempty"`
	Status string `json:"status,omitempty"`
	CreatedAt *Timestamp `json:"created_at,omitempty"`
	PaidAt *Timestamp `json:"paid_at,omitempty"`
	PdfUrl string `json:"pdf_url,omitempty"`
}

func (x *Invoice) GetId() string {
	if x == nil { return "" }
	return x.Id
}
func (x *Invoice) GetAmountCents() int64 {
	if x == nil { return 0 }
	return x.AmountCents
}
func (x *Invoice) GetCurrency() string {
	if x == nil { return "" }
	return x.Currency
}
func (x *Invoice) GetStatus() string {
	if x == nil { return "" }
	return x.Status
}
func (x *Invoice) GetCreatedAt() *Timestamp {
	if x == nil { return nil }
	return x.CreatedAt
}
func (x *Invoice) GetPaidAt() *Timestamp {
	if x == nil { return nil }
	return x.PaidAt
}
func (x *Invoice) GetPdfUrl() string {
	if x == nil { return "" }
	return x.PdfUrl
}

type CreateBillingSetupIntentRequest struct {
	TenantId string `json:"tenant_id,omitempty"`
}

func (x *CreateBillingSetupIntentRequest) GetTenantId() string {
	if x == nil { return "" }
	return x.TenantId
}

type CreateBillingSetupIntentResponse struct {
	ClientSecret string `json:"client_secret,omitempty"`
	SetupIntentId string `json:"setup_intent_id,omitempty"`
	PublishableKey string `json:"publishable_key,omitempty"`
}

func (x *CreateBillingSetupIntentResponse) GetClientSecret() string {
	if x == nil { return "" }
	return x.ClientSecret
}
func (x *CreateBillingSetupIntentResponse) GetSetupIntentId() string {
	if x == nil { return "" }
	return x.SetupIntentId
}
func (x *CreateBillingSetupIntentResponse) GetPublishableKey() string {
	if x == nil { return "" }
	return x.PublishableKey
}

type RequestBackupRequest struct {
	TenantId string `json:"tenant_id,omitempty"`
}

func (x *RequestBackupRequest) GetTenantId() string {
	if x == nil { return "" }
	return x.TenantId
}

type RequestBackupResponse struct {
	BackupId string `json:"backup_id,omitempty"`
	Status string `json:"status,omitempty"`
	Message string `json:"message,omitempty"`
}

func (x *RequestBackupResponse) GetBackupId() string {
	if x == nil { return "" }
	return x.BackupId
}
func (x *RequestBackupResponse) GetStatus() string {
	if x == nil { return "" }
	return x.Status
}
func (x *RequestBackupResponse) GetMessage() string {
	if x == nil { return "" }
	return x.Message
}

type ListBackupsRequest struct {
	TenantId string `json:"tenant_id,omitempty"`
}

func (x *ListBackupsRequest) GetTenantId() string {
	if x == nil { return "" }
	return x.TenantId
}

type ListBackupsResponse struct {
	Backups []*BackupInfo `json:"backups,omitempty"`
	MaxBackups int32 `json:"max_backups,omitempty"`
}

func (x *ListBackupsResponse) GetBackups() []*BackupInfo {
	if x == nil { return nil }
	return x.Backups
}
func (x *ListBackupsResponse) GetMaxBackups() int32 {
	if x == nil { return 0 }
	return x.MaxBackups
}

type BackupInfo struct {
	BackupId string `json:"backup_id,omitempty"`
	Status string `json:"status,omitempty"`
	SizeBytes int64 `json:"size_bytes,omitempty"`
	CreatedAt *Timestamp `json:"created_at,omitempty"`
	ExpiresAt *Timestamp `json:"expires_at,omitempty"`
	Message string `json:"message,omitempty"`
}

func (x *BackupInfo) GetBackupId() string {
	if x == nil { return "" }
	return x.BackupId
}
func (x *BackupInfo) GetStatus() string {
	if x == nil { return "" }
	return x.Status
}
func (x *BackupInfo) GetSizeBytes() int64 {
	if x == nil { return 0 }
	return x.SizeBytes
}
func (x *BackupInfo) GetCreatedAt() *Timestamp {
	if x == nil { return nil }
	return x.CreatedAt
}
func (x *BackupInfo) GetExpiresAt() *Timestamp {
	if x == nil { return nil }
	return x.ExpiresAt
}
func (x *BackupInfo) GetMessage() string {
	if x == nil { return "" }
	return x.Message
}

type GetBackupDownloadURLRequest struct {
	TenantId string `json:"tenant_id,omitempty"`
	BackupId string `json:"backup_id,omitempty"`
}

func (x *GetBackupDownloadURLRequest) GetTenantId() string {
	if x == nil { return "" }
	return x.TenantId
}
func (x *GetBackupDownloadURLRequest) GetBackupId() string {
	if x == nil { return "" }
	return x.BackupId
}

type GetBackupDownloadURLResponse struct {
	DownloadUrl string `json:"download_url,omitempty"`
	SizeBytes int64 `json:"size_bytes,omitempty"`
	ExpiresAt *Timestamp `json:"expires_at,omitempty"`
}

func (x *GetBackupDownloadURLResponse) GetDownloadUrl() string {
	if x == nil { return "" }
	return x.DownloadUrl
}
func (x *GetBackupDownloadURLResponse) GetSizeBytes() int64 {
	if x == nil { return 0 }
	return x.SizeBytes
}
func (x *GetBackupDownloadURLResponse) GetExpiresAt() *Timestamp {
	if x == nil { return nil }
	return x.ExpiresAt
}

type DeleteBackupRequest struct {
	TenantId string `json:"tenant_id,omitempty"`
	BackupId string `json:"backup_id,omitempty"`
}

func (x *DeleteBackupRequest) GetTenantId() string {
	if x == nil { return "" }
	return x.TenantId
}
func (x *DeleteBackupRequest) GetBackupId() string {
	if x == nil { return "" }
	return x.BackupId
}

type DeleteBackupResponse struct {
	Success bool `json:"success"`
	Message string `json:"message,omitempty"`
}

func (x *DeleteBackupResponse) GetSuccess() bool {
	if x == nil { return false }
	return x.Success
}
func (x *DeleteBackupResponse) GetMessage() string {
	if x == nil { return "" }
	return x.Message
}

type RestoreBackupRequest struct {
	TenantId string `json:"tenant_id,omitempty"`
	BackupId string `json:"backup_id,omitempty"`
}

func (x *RestoreBackupRequest) GetTenantId() string {
	if x == nil { return "" }
	return x.TenantId
}
func (x *RestoreBackupRequest) GetBackupId() string {
	if x == nil { return "" }
	return x.BackupId
}

type RestoreBackupResponse struct {
	RestoreId string `json:"restore_id,omitempty"`
	Status string `json:"status,omitempty"`
	Message string `json:"message,omitempty"`
}

func (x *RestoreBackupResponse) GetRestoreId() string {
	if x == nil { return "" }
	return x.RestoreId
}
func (x *RestoreBackupResponse) GetStatus() string {
	if x == nil { return "" }
	return x.Status
}
func (x *RestoreBackupResponse) GetMessage() string {
	if x == nil { return "" }
	return x.Message
}

type RestoreFromBackupRequest struct {
	TenantId string `json:"tenant_id,omitempty"`
	UploadToken string `json:"upload_token,omitempty"`
}

func (x *RestoreFromBackupRequest) GetTenantId() string {
	if x == nil { return "" }
	return x.TenantId
}
func (x *RestoreFromBackupRequest) GetUploadToken() string {
	if x == nil { return "" }
	return x.UploadToken
}

type RestoreFromBackupResponse struct {
	RestoreId string `json:"restore_id,omitempty"`
	Status string `json:"status,omitempty"`
	Message string `json:"message,omitempty"`
}

func (x *RestoreFromBackupResponse) GetRestoreId() string {
	if x == nil { return "" }
	return x.RestoreId
}
func (x *RestoreFromBackupResponse) GetStatus() string {
	if x == nil { return "" }
	return x.Status
}
func (x *RestoreFromBackupResponse) GetMessage() string {
	if x == nil { return "" }
	return x.Message
}

type GetRestoreStatusRequest struct {
	TenantId string `json:"tenant_id,omitempty"`
	RestoreId string `json:"restore_id,omitempty"`
}

func (x *GetRestoreStatusRequest) GetTenantId() string {
	if x == nil { return "" }
	return x.TenantId
}
func (x *GetRestoreStatusRequest) GetRestoreId() string {
	if x == nil { return "" }
	return x.RestoreId
}

type GetRestoreStatusResponse struct {
	Status string `json:"status,omitempty"`
	ProgressPercent int32 `json:"progress_percent,omitempty"`
	Message string `json:"message,omitempty"`
	StartedAt *Timestamp `json:"started_at,omitempty"`
	CompletedAt *Timestamp `json:"completed_at,omitempty"`
}

func (x *GetRestoreStatusResponse) GetStatus() string {
	if x == nil { return "" }
	return x.Status
}
func (x *GetRestoreStatusResponse) GetProgressPercent() int32 {
	if x == nil { return 0 }
	return x.ProgressPercent
}
func (x *GetRestoreStatusResponse) GetMessage() string {
	if x == nil { return "" }
	return x.Message
}
func (x *GetRestoreStatusResponse) GetStartedAt() *Timestamp {
	if x == nil { return nil }
	return x.StartedAt
}
func (x *GetRestoreStatusResponse) GetCompletedAt() *Timestamp {
	if x == nil { return nil }
	return x.CompletedAt
}

type BatchUpdatePeersRequest struct {
	TenantId string `json:"tenant_id,omitempty"`
	PeerIds []string `json:"peer_ids,omitempty"`
	Operation BatchUpdatePeersRequest_Operation `json:"operation,omitempty"`
	SequencePattern string `json:"sequence_pattern,omitempty"`
	SequenceStart int32 `json:"sequence_start,omitempty"`
	Tags []string `json:"tags,omitempty"`
}

func (x *BatchUpdatePeersRequest) GetTenantId() string {
	if x == nil { return "" }
	return x.TenantId
}
func (x *BatchUpdatePeersRequest) GetPeerIds() []string {
	if x == nil { return nil }
	return x.PeerIds
}
func (x *BatchUpdatePeersRequest) GetOperation() BatchUpdatePeersRequest_Operation {
	if x == nil { return BatchUpdatePeersRequest_Operation(0) }
	return x.Operation
}
func (x *BatchUpdatePeersRequest) GetSequencePattern() string {
	if x == nil { return "" }
	return x.SequencePattern
}
func (x *BatchUpdatePeersRequest) GetSequenceStart() int32 {
	if x == nil { return 0 }
	return x.SequenceStart
}
func (x *BatchUpdatePeersRequest) GetTags() []string {
	if x == nil { return nil }
	return x.Tags
}

type BatchUpdatePeersResponse struct {
	Success bool `json:"success"`
	Message string `json:"message,omitempty"`
	UpdatedCount int32 `json:"updated_count,omitempty"`
}

func (x *BatchUpdatePeersResponse) GetSuccess() bool {
	if x == nil { return false }
	return x.Success
}
func (x *BatchUpdatePeersResponse) GetMessage() string {
	if x == nil { return "" }
	return x.Message
}
func (x *BatchUpdatePeersResponse) GetUpdatedCount() int32 {
	if x == nil { return 0 }
	return x.UpdatedCount
}

type TenantLoginRequest struct {
	Email string `json:"email,omitempty"`
	Password string `json:"password,omitempty"`
	TotpCode string `json:"totp_code,omitempty"`
	RememberMe bool `json:"remember_me"`
	Fingerprint string `json:"fingerprint,omitempty"`
	SessionId string `json:"session_id,omitempty"`
	CaptchaAnswer string `json:"captcha_answer,omitempty"`
}

func (x *TenantLoginRequest) GetEmail() string {
	if x == nil { return "" }
	return x.Email
}
func (x *TenantLoginRequest) GetPassword() string {
	if x == nil { return "" }
	return x.Password
}
func (x *TenantLoginRequest) GetTotpCode() string {
	if x == nil { return "" }
	return x.TotpCode
}
func (x *TenantLoginRequest) GetRememberMe() bool {
	if x == nil { return false }
	return x.RememberMe
}
func (x *TenantLoginRequest) GetFingerprint() string {
	if x == nil { return "" }
	return x.Fingerprint
}
func (x *TenantLoginRequest) GetSessionId() string {
	if x == nil { return "" }
	return x.SessionId
}
func (x *TenantLoginRequest) GetCaptchaAnswer() string {
	if x == nil { return "" }
	return x.CaptchaAnswer
}

type TenantLoginResponse struct {
	Success bool `json:"success"`
	SessionToken string `json:"session_token,omitempty"`
	TenantId string `json:"tenant_id,omitempty"`
	Message string `json:"message,omitempty"`
	RequiresTotp bool `json:"requires_totp"`
	TwoFaMethod string `json:"two_fa_method,omitempty"`
	TwoFaPhoneLast4 string `json:"two_fa_phone_last4,omitempty"`
	FullName string `json:"full_name,omitempty"`
	Tier string `json:"tier,omitempty"`
	SessionId string `json:"session_id,omitempty"`
	Email string `json:"email,omitempty"`
	IsFirstLogin bool `json:"is_first_login"`
	CaptchaChallenge *CAPTCHAProblem `json:"captcha_challenge,omitempty"`
	CaptchaRequired bool `json:"captcha_required"`
	ErrorCode string `json:"error_code,omitempty"`
}

func (x *TenantLoginResponse) GetSuccess() bool {
	if x == nil { return false }
	return x.Success
}
func (x *TenantLoginResponse) GetSessionToken() string {
	if x == nil { return "" }
	return x.SessionToken
}
func (x *TenantLoginResponse) GetTenantId() string {
	if x == nil { return "" }
	return x.TenantId
}
func (x *TenantLoginResponse) GetMessage() string {
	if x == nil { return "" }
	return x.Message
}
func (x *TenantLoginResponse) GetRequiresTotp() bool {
	if x == nil { return false }
	return x.RequiresTotp
}
func (x *TenantLoginResponse) GetTwoFaMethod() string {
	if x == nil { return "" }
	return x.TwoFaMethod
}
func (x *TenantLoginResponse) GetTwoFaPhoneLast4() string {
	if x == nil { return "" }
	return x.TwoFaPhoneLast4
}
func (x *TenantLoginResponse) GetFullName() string {
	if x == nil { return "" }
	return x.FullName
}
func (x *TenantLoginResponse) GetTier() string {
	if x == nil { return "" }
	return x.Tier
}
func (x *TenantLoginResponse) GetSessionId() string {
	if x == nil { return "" }
	return x.SessionId
}
func (x *TenantLoginResponse) GetEmail() string {
	if x == nil { return "" }
	return x.Email
}
func (x *TenantLoginResponse) GetIsFirstLogin() bool {
	if x == nil { return false }
	return x.IsFirstLogin
}
func (x *TenantLoginResponse) GetCaptchaChallenge() *CAPTCHAProblem {
	if x == nil { return nil }
	return x.CaptchaChallenge
}
func (x *TenantLoginResponse) GetCaptchaRequired() bool {
	if x == nil { return false }
	return x.CaptchaRequired
}
func (x *TenantLoginResponse) GetErrorCode() string {
	if x == nil { return "" }
	return x.ErrorCode
}

type TenantLogoutRequest struct {
	SessionToken string `json:"session_token,omitempty"`
}

func (x *TenantLogoutRequest) GetSessionToken() string {
	if x == nil { return "" }
	return x.SessionToken
}

type TenantLogoutResponse struct {
	Success bool `json:"success"`
	Message string `json:"message,omitempty"`
}

func (x *TenantLogoutResponse) GetSuccess() bool {
	if x == nil { return false }
	return x.Success
}
func (x *TenantLogoutResponse) GetMessage() string {
	if x == nil { return "" }
	return x.Message
}

type GetTenantDashboardRequest struct {
	TenantId string `json:"tenant_id,omitempty"`
}

func (x *GetTenantDashboardRequest) GetTenantId() string {
	if x == nil { return "" }
	return x.TenantId
}

type GetTenantDashboardResponse struct {
	TenantId string `json:"tenant_id,omitempty"`
	Name string `json:"name,omitempty"`
	Tier AccountTier `json:"tier,omitempty"`
	Status string `json:"status,omitempty"`
	PeerCount int32 `json:"peer_count,omitempty"`
	MaxPeers int32 `json:"max_peers,omitempty"`
	BlockCount int32 `json:"block_count,omitempty"`
	RxBytes int64 `json:"rx_bytes,omitempty"`
	TxBytes int64 `json:"tx_bytes,omitempty"`
	OnlinePeers int32 `json:"online_peers,omitempty"`
	SubscriptionStatus string `json:"subscription_status,omitempty"`
	NextBillingDate *Timestamp `json:"next_billing_date,omitempty"`
	IsFreeTier bool `json:"is_free_tier"`
	TotalIpsAvailable int32 `json:"total_ips_available,omitempty"`
	IpsUsed int32 `json:"ips_used,omitempty"`
	NetworkBlocks []string `json:"network_blocks,omitempty"`
	GoroutineCount int32 `json:"goroutine_count,omitempty"`
	CpuUsagePercent float64 `json:"cpu_usage_percent,omitempty"`
	MemoryBytes int64 `json:"memory_bytes,omitempty"`
}

func (x *GetTenantDashboardResponse) GetTenantId() string {
	if x == nil { return "" }
	return x.TenantId
}
func (x *GetTenantDashboardResponse) GetName() string {
	if x == nil { return "" }
	return x.Name
}
func (x *GetTenantDashboardResponse) GetTier() AccountTier {
	if x == nil { return AccountTier(0) }
	return x.Tier
}
func (x *GetTenantDashboardResponse) GetStatus() string {
	if x == nil { return "" }
	return x.Status
}
func (x *GetTenantDashboardResponse) GetPeerCount() int32 {
	if x == nil { return 0 }
	return x.PeerCount
}
func (x *GetTenantDashboardResponse) GetMaxPeers() int32 {
	if x == nil { return 0 }
	return x.MaxPeers
}
func (x *GetTenantDashboardResponse) GetBlockCount() int32 {
	if x == nil { return 0 }
	return x.BlockCount
}
func (x *GetTenantDashboardResponse) GetRxBytes() int64 {
	if x == nil { return 0 }
	return x.RxBytes
}
func (x *GetTenantDashboardResponse) GetTxBytes() int64 {
	if x == nil { return 0 }
	return x.TxBytes
}
func (x *GetTenantDashboardResponse) GetOnlinePeers() int32 {
	if x == nil { return 0 }
	return x.OnlinePeers
}
func (x *GetTenantDashboardResponse) GetSubscriptionStatus() string {
	if x == nil { return "" }
	return x.SubscriptionStatus
}
func (x *GetTenantDashboardResponse) GetNextBillingDate() *Timestamp {
	if x == nil { return nil }
	return x.NextBillingDate
}
func (x *GetTenantDashboardResponse) GetIsFreeTier() bool {
	if x == nil { return false }
	return x.IsFreeTier
}
func (x *GetTenantDashboardResponse) GetTotalIpsAvailable() int32 {
	if x == nil { return 0 }
	return x.TotalIpsAvailable
}
func (x *GetTenantDashboardResponse) GetIpsUsed() int32 {
	if x == nil { return 0 }
	return x.IpsUsed
}
func (x *GetTenantDashboardResponse) GetNetworkBlocks() []string {
	if x == nil { return nil }
	return x.NetworkBlocks
}
func (x *GetTenantDashboardResponse) GetGoroutineCount() int32 {
	if x == nil { return 0 }
	return x.GoroutineCount
}
func (x *GetTenantDashboardResponse) GetCpuUsagePercent() float64 {
	if x == nil { return 0 }
	return x.CpuUsagePercent
}
func (x *GetTenantDashboardResponse) GetMemoryBytes() int64 {
	if x == nil { return 0 }
	return x.MemoryBytes
}

type UpdateTenantProfileRequest struct {
	TenantId string `json:"tenant_id,omitempty"`
	FullName string `json:"full_name,omitempty"`
	Email string `json:"email,omitempty"`
	Phone string `json:"phone,omitempty"`
	EnableTotp bool `json:"enable_totp"`
	CurrentPassword string `json:"current_password,omitempty"`
	NewPassword string `json:"new_password,omitempty"`
	PreferredLanguage string `json:"preferred_language,omitempty"`
}

func (x *UpdateTenantProfileRequest) GetTenantId() string {
	if x == nil { return "" }
	return x.TenantId
}
func (x *UpdateTenantProfileRequest) GetFullName() string {
	if x == nil { return "" }
	return x.FullName
}
func (x *UpdateTenantProfileRequest) GetEmail() string {
	if x == nil { return "" }
	return x.Email
}
func (x *UpdateTenantProfileRequest) GetPhone() string {
	if x == nil { return "" }
	return x.Phone
}
func (x *UpdateTenantProfileRequest) GetEnableTotp() bool {
	if x == nil { return false }
	return x.EnableTotp
}
func (x *UpdateTenantProfileRequest) GetCurrentPassword() string {
	if x == nil { return "" }
	return x.CurrentPassword
}
func (x *UpdateTenantProfileRequest) GetNewPassword() string {
	if x == nil { return "" }
	return x.NewPassword
}
func (x *UpdateTenantProfileRequest) GetPreferredLanguage() string {
	if x == nil { return "" }
	return x.PreferredLanguage
}

type UpdateTenantProfileResponse struct {
	Success bool `json:"success"`
	Message string `json:"message,omitempty"`
	TotpProvisioningUrl string `json:"totp_provisioning_url,omitempty"`
}

func (x *UpdateTenantProfileResponse) GetSuccess() bool {
	if x == nil { return false }
	return x.Success
}
func (x *UpdateTenantProfileResponse) GetMessage() string {
	if x == nil { return "" }
	return x.Message
}
func (x *UpdateTenantProfileResponse) GetTotpProvisioningUrl() string {
	if x == nil { return "" }
	return x.TotpProvisioningUrl
}

type DeleteTenantAccountRequest struct {
	TenantId string `json:"tenant_id,omitempty"`
	Password string `json:"password,omitempty"`
	Reason string `json:"reason,omitempty"`
}

func (x *DeleteTenantAccountRequest) GetTenantId() string {
	if x == nil { return "" }
	return x.TenantId
}
func (x *DeleteTenantAccountRequest) GetPassword() string {
	if x == nil { return "" }
	return x.Password
}
func (x *DeleteTenantAccountRequest) GetReason() string {
	if x == nil { return "" }
	return x.Reason
}

type DeleteTenantAccountResponse struct {
	Success bool `json:"success"`
	Message string `json:"message,omitempty"`
}

func (x *DeleteTenantAccountResponse) GetSuccess() bool {
	if x == nil { return false }
	return x.Success
}
func (x *DeleteTenantAccountResponse) GetMessage() string {
	if x == nil { return "" }
	return x.Message
}

type GetTenantAccountRequest struct {
	TenantId string `json:"tenant_id,omitempty"`
}

func (x *GetTenantAccountRequest) GetTenantId() string {
	if x == nil { return "" }
	return x.TenantId
}

type GetTenantAccountResponse struct {
	Account *Account `json:"account,omitempty"`
	FullName string `json:"full_name,omitempty"`
	Email string `json:"email,omitempty"`
	Phone string `json:"phone,omitempty"`
	TotpEnabled bool `json:"totp_enabled"`
	PhoneVerified bool `json:"phone_verified"`
	CreatedAt *Timestamp `json:"created_at,omitempty"`
	LastLogin *Timestamp `json:"last_login,omitempty"`
	TwoFaMethod string `json:"two_fa_method,omitempty"`
	PreferredLanguage string `json:"preferred_language,omitempty"`
	TenantId string `json:"tenant_id,omitempty"`
	PeerCount int32 `json:"peer_count,omitempty"`
	MaxPeers int32 `json:"max_peers,omitempty"`
}

func (x *GetTenantAccountResponse) GetAccount() *Account {
	if x == nil { return nil }
	return x.Account
}
func (x *GetTenantAccountResponse) GetFullName() string {
	if x == nil { return "" }
	return x.FullName
}
func (x *GetTenantAccountResponse) GetEmail() string {
	if x == nil { return "" }
	return x.Email
}
func (x *GetTenantAccountResponse) GetPhone() string {
	if x == nil { return "" }
	return x.Phone
}
func (x *GetTenantAccountResponse) GetTotpEnabled() bool {
	if x == nil { return false }
	return x.TotpEnabled
}
func (x *GetTenantAccountResponse) GetPhoneVerified() bool {
	if x == nil { return false }
	return x.PhoneVerified
}
func (x *GetTenantAccountResponse) GetCreatedAt() *Timestamp {
	if x == nil { return nil }
	return x.CreatedAt
}
func (x *GetTenantAccountResponse) GetLastLogin() *Timestamp {
	if x == nil { return nil }
	return x.LastLogin
}
func (x *GetTenantAccountResponse) GetTwoFaMethod() string {
	if x == nil { return "" }
	return x.TwoFaMethod
}
func (x *GetTenantAccountResponse) GetPreferredLanguage() string {
	if x == nil { return "" }
	return x.PreferredLanguage
}
func (x *GetTenantAccountResponse) GetTenantId() string {
	if x == nil { return "" }
	return x.TenantId
}
func (x *GetTenantAccountResponse) GetPeerCount() int32 {
	if x == nil { return 0 }
	return x.PeerCount
}
func (x *GetTenantAccountResponse) GetMaxPeers() int32 {
	if x == nil { return 0 }
	return x.MaxPeers
}

type GetTwoFASettingsRequest struct {
	TenantId string `json:"tenant_id,omitempty"`
}

func (x *GetTwoFASettingsRequest) GetTenantId() string {
	if x == nil { return "" }
	return x.TenantId
}

type GetTwoFASettingsResponse struct {
	Enabled bool `json:"enabled"`
	CurrentMethod string `json:"current_method,omitempty"`
	PhoneMasked string `json:"phone_masked,omitempty"`
	AvailableMethods []string `json:"available_methods,omitempty"`
}

func (x *GetTwoFASettingsResponse) GetEnabled() bool {
	if x == nil { return false }
	return x.Enabled
}
func (x *GetTwoFASettingsResponse) GetCurrentMethod() string {
	if x == nil { return "" }
	return x.CurrentMethod
}
func (x *GetTwoFASettingsResponse) GetPhoneMasked() string {
	if x == nil { return "" }
	return x.PhoneMasked
}
func (x *GetTwoFASettingsResponse) GetAvailableMethods() []string {
	if x == nil { return nil }
	return x.AvailableMethods
}

type SetTwoFAMethodRequest struct {
	TenantId string `json:"tenant_id,omitempty"`
	Method string `json:"method,omitempty"`
	TotpSecret string `json:"totp_secret,omitempty"`
	TotpCode string `json:"totp_code,omitempty"`
}

func (x *SetTwoFAMethodRequest) GetTenantId() string {
	if x == nil { return "" }
	return x.TenantId
}
func (x *SetTwoFAMethodRequest) GetMethod() string {
	if x == nil { return "" }
	return x.Method
}
func (x *SetTwoFAMethodRequest) GetTotpSecret() string {
	if x == nil { return "" }
	return x.TotpSecret
}
func (x *SetTwoFAMethodRequest) GetTotpCode() string {
	if x == nil { return "" }
	return x.TotpCode
}

type SetTwoFAMethodResponse struct {
	Success bool `json:"success"`
	Message string `json:"message,omitempty"`
}

func (x *SetTwoFAMethodResponse) GetSuccess() bool {
	if x == nil { return false }
	return x.Success
}
func (x *SetTwoFAMethodResponse) GetMessage() string {
	if x == nil { return "" }
	return x.Message
}

type Send2FACodeRequest struct {
	TenantId string `json:"tenant_id,omitempty"`
	Channel string `json:"channel,omitempty"`
}

func (x *Send2FACodeRequest) GetTenantId() string {
	if x == nil { return "" }
	return x.TenantId
}
func (x *Send2FACodeRequest) GetChannel() string {
	if x == nil { return "" }
	return x.Channel
}

type Send2FACodeResponse struct {
	Success bool `json:"success"`
	Message string `json:"message,omitempty"`
	ExpiresInSeconds int32 `json:"expires_in_seconds,omitempty"`
}

func (x *Send2FACodeResponse) GetSuccess() bool {
	if x == nil { return false }
	return x.Success
}
func (x *Send2FACodeResponse) GetMessage() string {
	if x == nil { return "" }
	return x.Message
}
func (x *Send2FACodeResponse) GetExpiresInSeconds() int32 {
	if x == nil { return 0 }
	return x.ExpiresInSeconds
}

type ChangePasswordRequest struct {
	TenantId string `json:"tenant_id,omitempty"`
	CurrentPassword string `json:"current_password,omitempty"`
	NewPassword string `json:"new_password,omitempty"`
}

func (x *ChangePasswordRequest) GetTenantId() string {
	if x == nil { return "" }
	return x.TenantId
}
func (x *ChangePasswordRequest) GetCurrentPassword() string {
	if x == nil { return "" }
	return x.CurrentPassword
}
func (x *ChangePasswordRequest) GetNewPassword() string {
	if x == nil { return "" }
	return x.NewPassword
}

type ChangePasswordResponse struct {
	Success bool `json:"success"`
	Message string `json:"message,omitempty"`
}

func (x *ChangePasswordResponse) GetSuccess() bool {
	if x == nil { return false }
	return x.Success
}
func (x *ChangePasswordResponse) GetMessage() string {
	if x == nil { return "" }
	return x.Message
}

type HandleSecurityAlertRequest struct {
	Token string `json:"token,omitempty"`
}

func (x *HandleSecurityAlertRequest) GetToken() string {
	if x == nil { return "" }
	return x.Token
}

type HandleSecurityAlertResponse struct {
	Success bool `json:"success"`
	Message string `json:"message,omitempty"`
	RedirectUrl string `json:"redirect_url,omitempty"`
}

func (x *HandleSecurityAlertResponse) GetSuccess() bool {
	if x == nil { return false }
	return x.Success
}
func (x *HandleSecurityAlertResponse) GetMessage() string {
	if x == nil { return "" }
	return x.Message
}
func (x *HandleSecurityAlertResponse) GetRedirectUrl() string {
	if x == nil { return "" }
	return x.RedirectUrl
}

type RequestPasswordResetRequest struct {
	Email string `json:"email,omitempty"`
}

func (x *RequestPasswordResetRequest) GetEmail() string {
	if x == nil { return "" }
	return x.Email
}

type RequestPasswordResetResponse struct {
	Success bool `json:"success"`
	Message string `json:"message,omitempty"`
	ResetToken string `json:"reset_token,omitempty"`
	PhoneMasked string `json:"phone_masked,omitempty"`
	CodeExpiresSeconds int32 `json:"code_expires_seconds,omitempty"`
	RateLimited bool `json:"rate_limited"`
	RetryAfter int32 `json:"retry_after,omitempty"`
}

func (x *RequestPasswordResetResponse) GetSuccess() bool {
	if x == nil { return false }
	return x.Success
}
func (x *RequestPasswordResetResponse) GetMessage() string {
	if x == nil { return "" }
	return x.Message
}
func (x *RequestPasswordResetResponse) GetResetToken() string {
	if x == nil { return "" }
	return x.ResetToken
}
func (x *RequestPasswordResetResponse) GetPhoneMasked() string {
	if x == nil { return "" }
	return x.PhoneMasked
}
func (x *RequestPasswordResetResponse) GetCodeExpiresSeconds() int32 {
	if x == nil { return 0 }
	return x.CodeExpiresSeconds
}
func (x *RequestPasswordResetResponse) GetRateLimited() bool {
	if x == nil { return false }
	return x.RateLimited
}
func (x *RequestPasswordResetResponse) GetRetryAfter() int32 {
	if x == nil { return 0 }
	return x.RetryAfter
}

type VerifyResetCodeRequest struct {
	ResetToken string `json:"reset_token,omitempty"`
	VerificationCode string `json:"verification_code,omitempty"`
}

func (x *VerifyResetCodeRequest) GetResetToken() string {
	if x == nil { return "" }
	return x.ResetToken
}
func (x *VerifyResetCodeRequest) GetVerificationCode() string {
	if x == nil { return "" }
	return x.VerificationCode
}

type VerifyResetCodeResponse struct {
	Success bool `json:"success"`
	Message string `json:"message,omitempty"`
	VerifiedToken string `json:"verified_token,omitempty"`
}

func (x *VerifyResetCodeResponse) GetSuccess() bool {
	if x == nil { return false }
	return x.Success
}
func (x *VerifyResetCodeResponse) GetMessage() string {
	if x == nil { return "" }
	return x.Message
}
func (x *VerifyResetCodeResponse) GetVerifiedToken() string {
	if x == nil { return "" }
	return x.VerifiedToken
}

type ResetPasswordRequest struct {
	VerifiedToken string `json:"verified_token,omitempty"`
	NewPassword string `json:"new_password,omitempty"`
}

func (x *ResetPasswordRequest) GetVerifiedToken() string {
	if x == nil { return "" }
	return x.VerifiedToken
}
func (x *ResetPasswordRequest) GetNewPassword() string {
	if x == nil { return "" }
	return x.NewPassword
}

type ResetPasswordResponse struct {
	Success bool `json:"success"`
	Message string `json:"message,omitempty"`
	SessionsInvalidated bool `json:"sessions_invalidated"`
}

func (x *ResetPasswordResponse) GetSuccess() bool {
	if x == nil { return false }
	return x.Success
}
func (x *ResetPasswordResponse) GetMessage() string {
	if x == nil { return "" }
	return x.Message
}
func (x *ResetPasswordResponse) GetSessionsInvalidated() bool {
	if x == nil { return false }
	return x.SessionsInvalidated
}

type ListTenantPeersRequest struct {
	TenantId string `json:"tenant_id,omitempty"`
}

func (x *ListTenantPeersRequest) GetTenantId() string {
	if x == nil { return "" }
	return x.TenantId
}

type ListTenantPeersResponse struct {
	Peers []*Peer `json:"peers,omitempty"`
}

func (x *ListTenantPeersResponse) GetPeers() []*Peer {
	if x == nil { return nil }
	return x.Peers
}

type AddTenantPeerRequest struct {
	TenantId string `json:"tenant_id,omitempty"`
	Name string `json:"name,omitempty"`
	PublicKey string `json:"public_key,omitempty"`
}

func (x *AddTenantPeerRequest) GetTenantId() string {
	if x == nil { return "" }
	return x.TenantId
}
func (x *AddTenantPeerRequest) GetName() string {
	if x == nil { return "" }
	return x.Name
}
func (x *AddTenantPeerRequest) GetPublicKey() string {
	if x == nil { return "" }
	return x.PublicKey
}

type AddTenantPeerResponse struct {
	Success bool `json:"success"`
	Message string `json:"message,omitempty"`
	Peer *Peer `json:"peer,omitempty"`
	PrivateKey string `json:"private_key,omitempty"`
	Config string `json:"config,omitempty"`
}

func (x *AddTenantPeerResponse) GetSuccess() bool {
	if x == nil { return false }
	return x.Success
}
func (x *AddTenantPeerResponse) GetMessage() string {
	if x == nil { return "" }
	return x.Message
}
func (x *AddTenantPeerResponse) GetPeer() *Peer {
	if x == nil { return nil }
	return x.Peer
}
func (x *AddTenantPeerResponse) GetPrivateKey() string {
	if x == nil { return "" }
	return x.PrivateKey
}
func (x *AddTenantPeerResponse) GetConfig() string {
	if x == nil { return "" }
	return x.Config
}

type RemoveTenantPeerRequest struct {
	TenantId string `json:"tenant_id,omitempty"`
	PeerId string `json:"peer_id,omitempty"`
}

func (x *RemoveTenantPeerRequest) GetTenantId() string {
	if x == nil { return "" }
	return x.TenantId
}
func (x *RemoveTenantPeerRequest) GetPeerId() string {
	if x == nil { return "" }
	return x.PeerId
}

type RemoveTenantPeerResponse struct {
	Success bool `json:"success"`
	Message string `json:"message,omitempty"`
}

func (x *RemoveTenantPeerResponse) GetSuccess() bool {
	if x == nil { return false }
	return x.Success
}
func (x *RemoveTenantPeerResponse) GetMessage() string {
	if x == nil { return "" }
	return x.Message
}

type UpdateTenantPeerRequest struct {
	TenantId string `json:"tenant_id,omitempty"`
	PeerId string `json:"peer_id,omitempty"`
	Name string `json:"name,omitempty"`
	Tags []string `json:"tags,omitempty"`
}

func (x *UpdateTenantPeerRequest) GetTenantId() string {
	if x == nil { return "" }
	return x.TenantId
}
func (x *UpdateTenantPeerRequest) GetPeerId() string {
	if x == nil { return "" }
	return x.PeerId
}
func (x *UpdateTenantPeerRequest) GetName() string {
	if x == nil { return "" }
	return x.Name
}
func (x *UpdateTenantPeerRequest) GetTags() []string {
	if x == nil { return nil }
	return x.Tags
}

type UpdateTenantPeerResponse struct {
	Success bool `json:"success"`
	Message string `json:"message,omitempty"`
	Peer *Peer `json:"peer,omitempty"`
}

func (x *UpdateTenantPeerResponse) GetSuccess() bool {
	if x == nil { return false }
	return x.Success
}
func (x *UpdateTenantPeerResponse) GetMessage() string {
	if x == nil { return "" }
	return x.Message
}
func (x *UpdateTenantPeerResponse) GetPeer() *Peer {
	if x == nil { return nil }
	return x.Peer
}

type SetPeerNotificationRequest struct {
	TenantId string `json:"tenant_id,omitempty"`
	PeerId string `json:"peer_id,omitempty"`
	Enabled bool `json:"enabled"`
}

func (x *SetPeerNotificationRequest) GetTenantId() string {
	if x == nil { return "" }
	return x.TenantId
}
func (x *SetPeerNotificationRequest) GetPeerId() string {
	if x == nil { return "" }
	return x.PeerId
}
func (x *SetPeerNotificationRequest) GetEnabled() bool {
	if x == nil { return false }
	return x.Enabled
}

type SetPeerNotificationResponse struct {
	Success bool `json:"success"`
	Message string `json:"message,omitempty"`
	NotificationEnabled bool `json:"notification_enabled"`
}

func (x *SetPeerNotificationResponse) GetSuccess() bool {
	if x == nil { return false }
	return x.Success
}
func (x *SetPeerNotificationResponse) GetMessage() string {
	if x == nil { return "" }
	return x.Message
}
func (x *SetPeerNotificationResponse) GetNotificationEnabled() bool {
	if x == nil { return false }
	return x.NotificationEnabled
}

type DisableAllPeerNotificationsRequest struct {
	TenantId string `json:"tenant_id,omitempty"`
}

func (x *DisableAllPeerNotificationsRequest) GetTenantId() string {
	if x == nil { return "" }
	return x.TenantId
}

type DisableAllPeerNotificationsResponse struct {
	Success bool `json:"success"`
	Message string `json:"message,omitempty"`
	DisabledCount int32 `json:"disabled_count,omitempty"`
}

func (x *DisableAllPeerNotificationsResponse) GetSuccess() bool {
	if x == nil { return false }
	return x.Success
}
func (x *DisableAllPeerNotificationsResponse) GetMessage() string {
	if x == nil { return "" }
	return x.Message
}
func (x *DisableAllPeerNotificationsResponse) GetDisabledCount() int32 {
	if x == nil { return 0 }
	return x.DisabledCount
}

type GetTenantTopologyRequest struct {
	TenantId string `json:"tenant_id,omitempty"`
}

func (x *GetTenantTopologyRequest) GetTenantId() string {
	if x == nil { return "" }
	return x.TenantId
}

type GetTenantTopologyResponse struct {
	Nodes []*TopologyNode `json:"nodes,omitempty"`
	Edges []*TopologyEdge `json:"edges,omitempty"`
}

func (x *GetTenantTopologyResponse) GetNodes() []*TopologyNode {
	if x == nil { return nil }
	return x.Nodes
}
func (x *GetTenantTopologyResponse) GetEdges() []*TopologyEdge {
	if x == nil { return nil }
	return x.Edges
}

type AssignExitNodeRequest struct {
	AccountId string `json:"account_id,omitempty"`
	EntryNodeId string `json:"entry_node_id,omitempty"`
	ExitNodeId string `json:"exit_node_id,omitempty"`
}

func (x *AssignExitNodeRequest) GetAccountId() string {
	if x == nil { return "" }
	return x.AccountId
}
func (x *AssignExitNodeRequest) GetEntryNodeId() string {
	if x == nil { return "" }
	return x.EntryNodeId
}
func (x *AssignExitNodeRequest) GetExitNodeId() string {
	if x == nil { return "" }
	return x.ExitNodeId
}

type AssignExitNodeResponse struct {
	Success bool `json:"success"`
	Message string `json:"message,omitempty"`
}

func (x *AssignExitNodeResponse) GetSuccess() bool {
	if x == nil { return false }
	return x.Success
}
func (x *AssignExitNodeResponse) GetMessage() string {
	if x == nil { return "" }
	return x.Message
}

type GetTenantPeerConfigRequest struct {
	TenantId string `json:"tenant_id,omitempty"`
	PeerId string `json:"peer_id,omitempty"`
	Endpoint string `json:"endpoint,omitempty"`
}

func (x *GetTenantPeerConfigRequest) GetTenantId() string {
	if x == nil { return "" }
	return x.TenantId
}
func (x *GetTenantPeerConfigRequest) GetPeerId() string {
	if x == nil { return "" }
	return x.PeerId
}
func (x *GetTenantPeerConfigRequest) GetEndpoint() string {
	if x == nil { return "" }
	return x.Endpoint
}

type GetTenantPeerConfigResponse struct {
	WgConfig string `json:"wg_config,omitempty"`
	QrCode string `json:"qr_code,omitempty"`
	SetupToken string `json:"setup_token,omitempty"`
}

func (x *GetTenantPeerConfigResponse) GetWgConfig() string {
	if x == nil { return "" }
	return x.WgConfig
}
func (x *GetTenantPeerConfigResponse) GetQrCode() string {
	if x == nil { return "" }
	return x.QrCode
}
func (x *GetTenantPeerConfigResponse) GetSetupToken() string {
	if x == nil { return "" }
	return x.SetupToken
}

type EnrollmentToken struct {
	Id string `json:"id,omitempty"`
	TenantId string `json:"tenant_id,omitempty"`
	Name string `json:"name,omitempty"`
	Token string `json:"token,omitempty"`
	MaxUses int32 `json:"max_uses,omitempty"`
	UsageCount int32 `json:"usage_count,omitempty"`
	ExpiresAt *Timestamp `json:"expires_at,omitempty"`
	CreatedAt *Timestamp `json:"created_at,omitempty"`
	CreatedBy string `json:"created_by,omitempty"`
}

func (x *EnrollmentToken) GetId() string {
	if x == nil { return "" }
	return x.Id
}
func (x *EnrollmentToken) GetTenantId() string {
	if x == nil { return "" }
	return x.TenantId
}
func (x *EnrollmentToken) GetName() string {
	if x == nil { return "" }
	return x.Name
}
func (x *EnrollmentToken) GetToken() string {
	if x == nil { return "" }
	return x.Token
}
func (x *EnrollmentToken) GetMaxUses() int32 {
	if x == nil { return 0 }
	return x.MaxUses
}
func (x *EnrollmentToken) GetUsageCount() int32 {
	if x == nil { return 0 }
	return x.UsageCount
}
func (x *EnrollmentToken) GetExpiresAt() *Timestamp {
	if x == nil { return nil }
	return x.ExpiresAt
}
func (x *EnrollmentToken) GetCreatedAt() *Timestamp {
	if x == nil { return nil }
	return x.CreatedAt
}
func (x *EnrollmentToken) GetCreatedBy() string {
	if x == nil { return "" }
	return x.CreatedBy
}

type ListEnrollmentTokensRequest struct {
	TenantId string `json:"tenant_id,omitempty"`
}

func (x *ListEnrollmentTokensRequest) GetTenantId() string {
	if x == nil { return "" }
	return x.TenantId
}

type ListEnrollmentTokensResponse struct {
	Tokens []*EnrollmentToken `json:"tokens,omitempty"`
}

func (x *ListEnrollmentTokensResponse) GetTokens() []*EnrollmentToken {
	if x == nil { return nil }
	return x.Tokens
}

type CreateEnrollmentTokenRequest struct {
	TenantId string `json:"tenant_id,omitempty"`
	Name string `json:"name,omitempty"`
	ExpiresInDays int32 `json:"expires_in_days,omitempty"`
	MaxUses int32 `json:"max_uses,omitempty"`
}

func (x *CreateEnrollmentTokenRequest) GetTenantId() string {
	if x == nil { return "" }
	return x.TenantId
}
func (x *CreateEnrollmentTokenRequest) GetName() string {
	if x == nil { return "" }
	return x.Name
}
func (x *CreateEnrollmentTokenRequest) GetExpiresInDays() int32 {
	if x == nil { return 0 }
	return x.ExpiresInDays
}
func (x *CreateEnrollmentTokenRequest) GetMaxUses() int32 {
	if x == nil { return 0 }
	return x.MaxUses
}

type CreateEnrollmentTokenResponse struct {
	Token *EnrollmentToken `json:"token,omitempty"`
}

func (x *CreateEnrollmentTokenResponse) GetToken() *EnrollmentToken {
	if x == nil { return nil }
	return x.Token
}

type DeleteEnrollmentTokenRequest struct {
	TenantId string `json:"tenant_id,omitempty"`
	TokenId string `json:"token_id,omitempty"`
}

func (x *DeleteEnrollmentTokenRequest) GetTenantId() string {
	if x == nil { return "" }
	return x.TenantId
}
func (x *DeleteEnrollmentTokenRequest) GetTokenId() string {
	if x == nil { return "" }
	return x.TokenId
}

type DeleteEnrollmentTokenResponse struct {
	Success bool `json:"success"`
	Message string `json:"message,omitempty"`
}

func (x *DeleteEnrollmentTokenResponse) GetSuccess() bool {
	if x == nil { return false }
	return x.Success
}
func (x *DeleteEnrollmentTokenResponse) GetMessage() string {
	if x == nil { return "" }
	return x.Message
}

type ConfirmDeviceRequest struct {
	UserCode string `json:"user_code,omitempty"`
	TenantId string `json:"tenant_id,omitempty"`
}

func (x *ConfirmDeviceRequest) GetUserCode() string {
	if x == nil { return "" }
	return x.UserCode
}
func (x *ConfirmDeviceRequest) GetTenantId() string {
	if x == nil { return "" }
	return x.TenantId
}

type ConfirmDeviceResponse struct {
	Success bool `json:"success"`
	Message string `json:"message,omitempty"`
	DeviceName string `json:"device_name,omitempty"`
}

func (x *ConfirmDeviceResponse) GetSuccess() bool {
	if x == nil { return false }
	return x.Success
}
func (x *ConfirmDeviceResponse) GetMessage() string {
	if x == nil { return "" }
	return x.Message
}
func (x *ConfirmDeviceResponse) GetDeviceName() string {
	if x == nil { return "" }
	return x.DeviceName
}

type GetTenantPeerRequest struct {
	TenantId string `json:"tenant_id,omitempty"`
	PeerId string `json:"peer_id,omitempty"`
}

func (x *GetTenantPeerRequest) GetTenantId() string {
	if x == nil { return "" }
	return x.TenantId
}
func (x *GetTenantPeerRequest) GetPeerId() string {
	if x == nil { return "" }
	return x.PeerId
}

type GetTenantPeerResponse struct {
	Peer *Peer `json:"peer,omitempty"`
}

func (x *GetTenantPeerResponse) GetPeer() *Peer {
	if x == nil { return nil }
	return x.Peer
}

type GetTenantPeerStatsRequest struct {
	TenantId string `json:"tenant_id,omitempty"`
	PeerId string `json:"peer_id,omitempty"`
}

func (x *GetTenantPeerStatsRequest) GetTenantId() string {
	if x == nil { return "" }
	return x.TenantId
}
func (x *GetTenantPeerStatsRequest) GetPeerId() string {
	if x == nil { return "" }
	return x.PeerId
}

type GetTenantPeerStatsResponse struct {
	Stats *PeerStats `json:"stats,omitempty"`
}

func (x *GetTenantPeerStatsResponse) GetStats() *PeerStats {
	if x == nil { return nil }
	return x.Stats
}

type PingTenantPeerRequest struct {
	TenantId string `json:"tenant_id,omitempty"`
	PeerId string `json:"peer_id,omitempty"`
	Count int32 `json:"count,omitempty"`
	TimeoutMs int32 `json:"timeout_ms,omitempty"`
}

func (x *PingTenantPeerRequest) GetTenantId() string {
	if x == nil { return "" }
	return x.TenantId
}
func (x *PingTenantPeerRequest) GetPeerId() string {
	if x == nil { return "" }
	return x.PeerId
}
func (x *PingTenantPeerRequest) GetCount() int32 {
	if x == nil { return 0 }
	return x.Count
}
func (x *PingTenantPeerRequest) GetTimeoutMs() int32 {
	if x == nil { return 0 }
	return x.TimeoutMs
}

type PingTenantPeerResponse struct {
	PeerIp string `json:"peer_ip,omitempty"`
	PacketsSent int32 `json:"packets_sent,omitempty"`
	PacketsReceived int32 `json:"packets_received,omitempty"`
	PacketLossPercent float32 `json:"packet_loss_percent,omitempty"`
	MinRttMs float32 `json:"min_rtt_ms,omitempty"`
	AvgRttMs float32 `json:"avg_rtt_ms,omitempty"`
	MaxRttMs float32 `json:"max_rtt_ms,omitempty"`
	Success bool `json:"success"`
	Error string `json:"error,omitempty"`
	Pings []*PingDetail `json:"pings,omitempty"`
}

func (x *PingTenantPeerResponse) GetPeerIp() string {
	if x == nil { return "" }
	return x.PeerIp
}
func (x *PingTenantPeerResponse) GetPacketsSent() int32 {
	if x == nil { return 0 }
	return x.PacketsSent
}
func (x *PingTenantPeerResponse) GetPacketsReceived() int32 {
	if x == nil { return 0 }
	return x.PacketsReceived
}
func (x *PingTenantPeerResponse) GetPacketLossPercent() float32 {
	if x == nil { return 0 }
	return x.PacketLossPercent
}
func (x *PingTenantPeerResponse) GetMinRttMs() float32 {
	if x == nil { return 0 }
	return x.MinRttMs
}
func (x *PingTenantPeerResponse) GetAvgRttMs() float32 {
	if x == nil { return 0 }
	return x.AvgRttMs
}
func (x *PingTenantPeerResponse) GetMaxRttMs() float32 {
	if x == nil { return 0 }
	return x.MaxRttMs
}
func (x *PingTenantPeerResponse) GetSuccess() bool {
	if x == nil { return false }
	return x.Success
}
func (x *PingTenantPeerResponse) GetError() string {
	if x == nil { return "" }
	return x.Error
}
func (x *PingTenantPeerResponse) GetPings() []*PingDetail {
	if x == nil { return nil }
	return x.Pings
}

type ClearTenantWinboxCredentialsRequest struct {
	TenantId string `json:"tenant_id,omitempty"`
	PeerId string `json:"peer_id,omitempty"`
}

func (x *ClearTenantWinboxCredentialsRequest) GetTenantId() string {
	if x == nil { return "" }
	return x.TenantId
}
func (x *ClearTenantWinboxCredentialsRequest) GetPeerId() string {
	if x == nil { return "" }
	return x.PeerId
}

type ClearTenantWinboxCredentialsResponse struct {
	Success bool `json:"success"`
	Message string `json:"message,omitempty"`
}

func (x *ClearTenantWinboxCredentialsResponse) GetSuccess() bool {
	if x == nil { return false }
	return x.Success
}
func (x *ClearTenantWinboxCredentialsResponse) GetMessage() string {
	if x == nil { return "" }
	return x.Message
}

type CreateTenantWinboxSessionRequest struct {
	TenantId string `json:"tenant_id,omitempty"`
	PeerId string `json:"peer_id,omitempty"`
	Name string `json:"name,omitempty"`
	RouterIp string `json:"router_ip,omitempty"`
	Username string `json:"username,omitempty"`
	Password string `json:"password,omitempty"`
	AllowedClientIps []string `json:"allowed_client_ips,omitempty"`
	Port int32 `json:"port,omitempty"`
}

func (x *CreateTenantWinboxSessionRequest) GetTenantId() string {
	if x == nil { return "" }
	return x.TenantId
}
func (x *CreateTenantWinboxSessionRequest) GetPeerId() string {
	if x == nil { return "" }
	return x.PeerId
}
func (x *CreateTenantWinboxSessionRequest) GetName() string {
	if x == nil { return "" }
	return x.Name
}
func (x *CreateTenantWinboxSessionRequest) GetRouterIp() string {
	if x == nil { return "" }
	return x.RouterIp
}
func (x *CreateTenantWinboxSessionRequest) GetUsername() string {
	if x == nil { return "" }
	return x.Username
}
func (x *CreateTenantWinboxSessionRequest) GetPassword() string {
	if x == nil { return "" }
	return x.Password
}
func (x *CreateTenantWinboxSessionRequest) GetAllowedClientIps() []string {
	if x == nil { return nil }
	return x.AllowedClientIps
}
func (x *CreateTenantWinboxSessionRequest) GetPort() int32 {
	if x == nil { return 0 }
	return x.Port
}

type CreateTenantWinboxSessionResponse struct {
	Session *WinboxSession `json:"session,omitempty"`
	AccessToken string `json:"access_token,omitempty"`
	Message string `json:"message,omitempty"`
	PasswordToken string `json:"password_token,omitempty"`
}

func (x *CreateTenantWinboxSessionResponse) GetSession() *WinboxSession {
	if x == nil { return nil }
	return x.Session
}
func (x *CreateTenantWinboxSessionResponse) GetAccessToken() string {
	if x == nil { return "" }
	return x.AccessToken
}
func (x *CreateTenantWinboxSessionResponse) GetMessage() string {
	if x == nil { return "" }
	return x.Message
}
func (x *CreateTenantWinboxSessionResponse) GetPasswordToken() string {
	if x == nil { return "" }
	return x.PasswordToken
}

type UpdateTenantWinboxSessionRequest struct {
	TenantId string `json:"tenant_id,omitempty"`
	PeerId string `json:"peer_id,omitempty"`
	SessionId string `json:"session_id,omitempty"`
	Name string `json:"name,omitempty"`
	RouterIp string `json:"router_ip,omitempty"`
	Username string `json:"username,omitempty"`
	Password string `json:"password,omitempty"`
	AllowedClientIps []string `json:"allowed_client_ips,omitempty"`
	Enabled bool `json:"enabled"`
	RegenerateToken bool `json:"regenerate_token"`
	ClearAllowedIps bool `json:"clear_allowed_ips"`
	RegeneratePasswordToken bool `json:"regenerate_password_token"`
}

func (x *UpdateTenantWinboxSessionRequest) GetTenantId() string {
	if x == nil { return "" }
	return x.TenantId
}
func (x *UpdateTenantWinboxSessionRequest) GetPeerId() string {
	if x == nil { return "" }
	return x.PeerId
}
func (x *UpdateTenantWinboxSessionRequest) GetSessionId() string {
	if x == nil { return "" }
	return x.SessionId
}
func (x *UpdateTenantWinboxSessionRequest) GetName() string {
	if x == nil { return "" }
	return x.Name
}
func (x *UpdateTenantWinboxSessionRequest) GetRouterIp() string {
	if x == nil { return "" }
	return x.RouterIp
}
func (x *UpdateTenantWinboxSessionRequest) GetUsername() string {
	if x == nil { return "" }
	return x.Username
}
func (x *UpdateTenantWinboxSessionRequest) GetPassword() string {
	if x == nil { return "" }
	return x.Password
}
func (x *UpdateTenantWinboxSessionRequest) GetAllowedClientIps() []string {
	if x == nil { return nil }
	return x.AllowedClientIps
}
func (x *UpdateTenantWinboxSessionRequest) GetEnabled() bool {
	if x == nil { return false }
	return x.Enabled
}
func (x *UpdateTenantWinboxSessionRequest) GetRegenerateToken() bool {
	if x == nil { return false }
	return x.RegenerateToken
}
func (x *UpdateTenantWinboxSessionRequest) GetClearAllowedIps() bool {
	if x == nil { return false }
	return x.ClearAllowedIps
}
func (x *UpdateTenantWinboxSessionRequest) GetRegeneratePasswordToken() bool {
	if x == nil { return false }
	return x.RegeneratePasswordToken
}

type UpdateTenantWinboxSessionResponse struct {
	Session *WinboxSession `json:"session,omitempty"`
	AccessToken string `json:"access_token,omitempty"`
	Message string `json:"message,omitempty"`
	PasswordToken string `json:"password_token,omitempty"`
}

func (x *UpdateTenantWinboxSessionResponse) GetSession() *WinboxSession {
	if x == nil { return nil }
	return x.Session
}
func (x *UpdateTenantWinboxSessionResponse) GetAccessToken() string {
	if x == nil { return "" }
	return x.AccessToken
}
func (x *UpdateTenantWinboxSessionResponse) GetMessage() string {
	if x == nil { return "" }
	return x.Message
}
func (x *UpdateTenantWinboxSessionResponse) GetPasswordToken() string {
	if x == nil { return "" }
	return x.PasswordToken
}

type DeleteTenantWinboxSessionRequest struct {
	TenantId string `json:"tenant_id,omitempty"`
	PeerId string `json:"peer_id,omitempty"`
	SessionId string `json:"session_id,omitempty"`
}

func (x *DeleteTenantWinboxSessionRequest) GetTenantId() string {
	if x == nil { return "" }
	return x.TenantId
}
func (x *DeleteTenantWinboxSessionRequest) GetPeerId() string {
	if x == nil { return "" }
	return x.PeerId
}
func (x *DeleteTenantWinboxSessionRequest) GetSessionId() string {
	if x == nil { return "" }
	return x.SessionId
}

type DeleteTenantWinboxSessionResponse struct {
	Success bool `json:"success"`
	Message string `json:"message,omitempty"`
}

func (x *DeleteTenantWinboxSessionResponse) GetSuccess() bool {
	if x == nil { return false }
	return x.Success
}
func (x *DeleteTenantWinboxSessionResponse) GetMessage() string {
	if x == nil { return "" }
	return x.Message
}

// DuplicateTenantWinboxSessionRequest copies an existing Winbox session into
// a new row under a new name. The router target, port, allowed-client list
// and — importantly — the encrypted credential blobs are carried over byte
// for byte, so the operation needs only the new name from the user.
// Cleartext credentials never leave the server.
type DuplicateTenantWinboxSessionRequest struct {
	TenantId  string `json:"tenant_id,omitempty"`
	SessionId string `json:"session_id,omitempty"`
	NewName   string `json:"new_name,omitempty"`
}

func (x *DuplicateTenantWinboxSessionRequest) GetTenantId() string {
	if x == nil { return "" }
	return x.TenantId
}
func (x *DuplicateTenantWinboxSessionRequest) GetSessionId() string {
	if x == nil { return "" }
	return x.SessionId
}
func (x *DuplicateTenantWinboxSessionRequest) GetNewName() string {
	if x == nil { return "" }
	return x.NewName
}

type DuplicateTenantWinboxSessionResponse struct {
	Session *WinboxSession `json:"session,omitempty"`
}

func (x *DuplicateTenantWinboxSessionResponse) GetSession() *WinboxSession {
	if x == nil { return nil }
	return x.Session
}

type ListTenantWinboxSessionsRequest struct {
	TenantId string `json:"tenant_id,omitempty"`
	PeerId string `json:"peer_id,omitempty"`
}

func (x *ListTenantWinboxSessionsRequest) GetTenantId() string {
	if x == nil { return "" }
	return x.TenantId
}
func (x *ListTenantWinboxSessionsRequest) GetPeerId() string {
	if x == nil { return "" }
	return x.PeerId
}

type ListTenantWinboxSessionsResponse struct {
	Sessions []*WinboxSession `json:"sessions,omitempty"`
}

func (x *ListTenantWinboxSessionsResponse) GetSessions() []*WinboxSession {
	if x == nil { return nil }
	return x.Sessions
}

type GetTenantWinboxSessionRequest struct {
	TenantId string `json:"tenant_id,omitempty"`
	PeerId string `json:"peer_id,omitempty"`
	SessionId string `json:"session_id,omitempty"`
}

func (x *GetTenantWinboxSessionRequest) GetTenantId() string {
	if x == nil { return "" }
	return x.TenantId
}
func (x *GetTenantWinboxSessionRequest) GetPeerId() string {
	if x == nil { return "" }
	return x.PeerId
}
func (x *GetTenantWinboxSessionRequest) GetSessionId() string {
	if x == nil { return "" }
	return x.SessionId
}

type GetTenantWinboxSessionResponse struct {
	Session *WinboxSession `json:"session,omitempty"`
}

func (x *GetTenantWinboxSessionResponse) GetSession() *WinboxSession {
	if x == nil { return nil }
	return x.Session
}

type CreateTenantPeerGroupRequest struct {
	TenantId string `json:"tenant_id,omitempty"`
	GroupId string `json:"group_id,omitempty"`
	Name string `json:"name,omitempty"`
	Description string `json:"description,omitempty"`
	AllowedProtocols []uint32 `json:"allowed_protocols,omitempty"`
}

func (x *CreateTenantPeerGroupRequest) GetTenantId() string {
	if x == nil { return "" }
	return x.TenantId
}
func (x *CreateTenantPeerGroupRequest) GetGroupId() string {
	if x == nil { return "" }
	return x.GroupId
}
func (x *CreateTenantPeerGroupRequest) GetName() string {
	if x == nil { return "" }
	return x.Name
}
func (x *CreateTenantPeerGroupRequest) GetDescription() string {
	if x == nil { return "" }
	return x.Description
}
func (x *CreateTenantPeerGroupRequest) GetAllowedProtocols() []uint32 {
	if x == nil { return nil }
	return x.AllowedProtocols
}

type CreateTenantPeerGroupResponse struct {
	Group *PeerGroup `json:"group,omitempty"`
}

func (x *CreateTenantPeerGroupResponse) GetGroup() *PeerGroup {
	if x == nil { return nil }
	return x.Group
}

type DeleteTenantPeerGroupRequest struct {
	TenantId string `json:"tenant_id,omitempty"`
	GroupId string `json:"group_id,omitempty"`
}

func (x *DeleteTenantPeerGroupRequest) GetTenantId() string {
	if x == nil { return "" }
	return x.TenantId
}
func (x *DeleteTenantPeerGroupRequest) GetGroupId() string {
	if x == nil { return "" }
	return x.GroupId
}

type DeleteTenantPeerGroupResponse struct {
	Success bool `json:"success"`
}

func (x *DeleteTenantPeerGroupResponse) GetSuccess() bool {
	if x == nil { return false }
	return x.Success
}

type ListTenantPeerGroupsRequest struct {
	TenantId string `json:"tenant_id,omitempty"`
}

func (x *ListTenantPeerGroupsRequest) GetTenantId() string {
	if x == nil { return "" }
	return x.TenantId
}

type ListTenantPeerGroupsResponse struct {
	Groups []*PeerGroup `json:"groups,omitempty"`
}

func (x *ListTenantPeerGroupsResponse) GetGroups() []*PeerGroup {
	if x == nil { return nil }
	return x.Groups
}

type AddTenantPeerToGroupRequest struct {
	TenantId string `json:"tenant_id,omitempty"`
	GroupId string `json:"group_id,omitempty"`
	PeerId string `json:"peer_id,omitempty"`
}

func (x *AddTenantPeerToGroupRequest) GetTenantId() string {
	if x == nil { return "" }
	return x.TenantId
}
func (x *AddTenantPeerToGroupRequest) GetGroupId() string {
	if x == nil { return "" }
	return x.GroupId
}
func (x *AddTenantPeerToGroupRequest) GetPeerId() string {
	if x == nil { return "" }
	return x.PeerId
}

type AddTenantPeerToGroupResponse struct {
	Success bool `json:"success"`
}

func (x *AddTenantPeerToGroupResponse) GetSuccess() bool {
	if x == nil { return false }
	return x.Success
}

type RemoveTenantPeerFromGroupRequest struct {
	TenantId string `json:"tenant_id,omitempty"`
	GroupId string `json:"group_id,omitempty"`
	PeerId string `json:"peer_id,omitempty"`
}

func (x *RemoveTenantPeerFromGroupRequest) GetTenantId() string {
	if x == nil { return "" }
	return x.TenantId
}
func (x *RemoveTenantPeerFromGroupRequest) GetGroupId() string {
	if x == nil { return "" }
	return x.GroupId
}
func (x *RemoveTenantPeerFromGroupRequest) GetPeerId() string {
	if x == nil { return "" }
	return x.PeerId
}

type RemoveTenantPeerFromGroupResponse struct {
	Success bool `json:"success"`
}

func (x *RemoveTenantPeerFromGroupResponse) GetSuccess() bool {
	if x == nil { return false }
	return x.Success
}

type CreateTenantGroupLinkRequest struct {
	TenantId string `json:"tenant_id,omitempty"`
	SourceGroupId string `json:"source_group_id,omitempty"`
	TargetGroupId string `json:"target_group_id,omitempty"`
	AllowedProtocols []uint32 `json:"allowed_protocols,omitempty"`
	AllowedPorts []string `json:"allowed_ports,omitempty"`
}

func (x *CreateTenantGroupLinkRequest) GetTenantId() string {
	if x == nil { return "" }
	return x.TenantId
}
func (x *CreateTenantGroupLinkRequest) GetSourceGroupId() string {
	if x == nil { return "" }
	return x.SourceGroupId
}
func (x *CreateTenantGroupLinkRequest) GetTargetGroupId() string {
	if x == nil { return "" }
	return x.TargetGroupId
}
func (x *CreateTenantGroupLinkRequest) GetAllowedProtocols() []uint32 {
	if x == nil { return nil }
	return x.AllowedProtocols
}
func (x *CreateTenantGroupLinkRequest) GetAllowedPorts() []string {
	if x == nil { return nil }
	return x.AllowedPorts
}

type CreateTenantGroupLinkResponse struct {
	Link *GroupLink `json:"link,omitempty"`
}

func (x *CreateTenantGroupLinkResponse) GetLink() *GroupLink {
	if x == nil { return nil }
	return x.Link
}

type DeleteTenantGroupLinkRequest struct {
	TenantId string `json:"tenant_id,omitempty"`
	SourceGroupId string `json:"source_group_id,omitempty"`
	TargetGroupId string `json:"target_group_id,omitempty"`
	LinkId string `json:"link_id,omitempty"`
}

func (x *DeleteTenantGroupLinkRequest) GetTenantId() string {
	if x == nil { return "" }
	return x.TenantId
}
func (x *DeleteTenantGroupLinkRequest) GetSourceGroupId() string {
	if x == nil { return "" }
	return x.SourceGroupId
}
func (x *DeleteTenantGroupLinkRequest) GetTargetGroupId() string {
	if x == nil { return "" }
	return x.TargetGroupId
}
func (x *DeleteTenantGroupLinkRequest) GetLinkId() string {
	if x == nil { return "" }
	return x.LinkId
}

type DeleteTenantGroupLinkResponse struct {
	Success bool `json:"success"`
}

func (x *DeleteTenantGroupLinkResponse) GetSuccess() bool {
	if x == nil { return false }
	return x.Success
}

type ListTenantGroupLinksRequest struct {
	TenantId string `json:"tenant_id,omitempty"`
}

func (x *ListTenantGroupLinksRequest) GetTenantId() string {
	if x == nil { return "" }
	return x.TenantId
}

type ListTenantGroupLinksResponse struct {
	Links []*GroupLink `json:"links,omitempty"`
}

func (x *ListTenantGroupLinksResponse) GetLinks() []*GroupLink {
	if x == nil { return nil }
	return x.Links
}

type CompileTenantGroupsRequest struct {
	TenantId string `json:"tenant_id,omitempty"`
}

func (x *CompileTenantGroupsRequest) GetTenantId() string {
	if x == nil { return "" }
	return x.TenantId
}

type CompileTenantGroupsResponse struct {
	Success bool `json:"success"`
	RulesGenerated int32 `json:"rules_generated,omitempty"`
	CompilationTimeMs int32 `json:"compilation_time_ms,omitempty"`
}

func (x *CompileTenantGroupsResponse) GetSuccess() bool {
	if x == nil { return false }
	return x.Success
}
func (x *CompileTenantGroupsResponse) GetRulesGenerated() int32 {
	if x == nil { return 0 }
	return x.RulesGenerated
}
func (x *CompileTenantGroupsResponse) GetCompilationTimeMs() int32 {
	if x == nil { return 0 }
	return x.CompilationTimeMs
}

type GetTenantCompilationStatsRequest struct {
	TenantId string `json:"tenant_id,omitempty"`
}

func (x *GetTenantCompilationStatsRequest) GetTenantId() string {
	if x == nil { return "" }
	return x.TenantId
}

type GetTenantCompilationStatsResponse struct {
	LastCompilation *Timestamp `json:"last_compilation,omitempty"`
	TotalRules int32 `json:"total_rules,omitempty"`
	TotalGroups int32 `json:"total_groups,omitempty"`
	TotalLinks int32 `json:"total_links,omitempty"`
}

func (x *GetTenantCompilationStatsResponse) GetLastCompilation() *Timestamp {
	if x == nil { return nil }
	return x.LastCompilation
}
func (x *GetTenantCompilationStatsResponse) GetTotalRules() int32 {
	if x == nil { return 0 }
	return x.TotalRules
}
func (x *GetTenantCompilationStatsResponse) GetTotalGroups() int32 {
	if x == nil { return 0 }
	return x.TotalGroups
}
func (x *GetTenantCompilationStatsResponse) GetTotalLinks() int32 {
	if x == nil { return 0 }
	return x.TotalLinks
}

type AddTenantACLRuleRequest struct {
	TenantId string `json:"tenant_id,omitempty"`
	SourceIp string `json:"source_ip,omitempty"`
	DestIp string `json:"dest_ip,omitempty"`
	Protocol uint32 `json:"protocol,omitempty"`
	DestPort string `json:"dest_port,omitempty"`
	Action string `json:"action,omitempty"`
	Priority int32 `json:"priority,omitempty"`
}

func (x *AddTenantACLRuleRequest) GetTenantId() string {
	if x == nil { return "" }
	return x.TenantId
}
func (x *AddTenantACLRuleRequest) GetSourceIp() string {
	if x == nil { return "" }
	return x.SourceIp
}
func (x *AddTenantACLRuleRequest) GetDestIp() string {
	if x == nil { return "" }
	return x.DestIp
}
func (x *AddTenantACLRuleRequest) GetProtocol() uint32 {
	if x == nil { return 0 }
	return x.Protocol
}
func (x *AddTenantACLRuleRequest) GetDestPort() string {
	if x == nil { return "" }
	return x.DestPort
}
func (x *AddTenantACLRuleRequest) GetAction() string {
	if x == nil { return "" }
	return x.Action
}
func (x *AddTenantACLRuleRequest) GetPriority() int32 {
	if x == nil { return 0 }
	return x.Priority
}

type AddTenantACLRuleResponse struct {
	Rule *ACLRule `json:"rule,omitempty"`
}

func (x *AddTenantACLRuleResponse) GetRule() *ACLRule {
	if x == nil { return nil }
	return x.Rule
}

type RemoveTenantACLRuleRequest struct {
	TenantId string `json:"tenant_id,omitempty"`
	RuleId string `json:"rule_id,omitempty"`
}

func (x *RemoveTenantACLRuleRequest) GetTenantId() string {
	if x == nil { return "" }
	return x.TenantId
}
func (x *RemoveTenantACLRuleRequest) GetRuleId() string {
	if x == nil { return "" }
	return x.RuleId
}

type RemoveTenantACLRuleResponse struct {
	Success bool `json:"success"`
}

func (x *RemoveTenantACLRuleResponse) GetSuccess() bool {
	if x == nil { return false }
	return x.Success
}

type GetTenantACLRulesRequest struct {
	TenantId string `json:"tenant_id,omitempty"`
}

func (x *GetTenantACLRulesRequest) GetTenantId() string {
	if x == nil { return "" }
	return x.TenantId
}

type GetTenantACLRulesResponse struct {
	Rules []*ACLRule `json:"rules,omitempty"`
}

func (x *GetTenantACLRulesResponse) GetRules() []*ACLRule {
	if x == nil { return nil }
	return x.Rules
}

type CheckTenantAccessRequest struct {
	TenantId string `json:"tenant_id,omitempty"`
	SourcePeerId string `json:"source_peer_id,omitempty"`
	DestPeerId string `json:"dest_peer_id,omitempty"`
	Protocol uint32 `json:"protocol,omitempty"`
	DestPort string `json:"dest_port,omitempty"`
}

func (x *CheckTenantAccessRequest) GetTenantId() string {
	if x == nil { return "" }
	return x.TenantId
}
func (x *CheckTenantAccessRequest) GetSourcePeerId() string {
	if x == nil { return "" }
	return x.SourcePeerId
}
func (x *CheckTenantAccessRequest) GetDestPeerId() string {
	if x == nil { return "" }
	return x.DestPeerId
}
func (x *CheckTenantAccessRequest) GetProtocol() uint32 {
	if x == nil { return 0 }
	return x.Protocol
}
func (x *CheckTenantAccessRequest) GetDestPort() string {
	if x == nil { return "" }
	return x.DestPort
}

type CheckTenantAccessResponse struct {
	Allowed bool `json:"allowed"`
	Reason string `json:"reason,omitempty"`
	MatchedRuleId string `json:"matched_rule_id,omitempty"`
}

func (x *CheckTenantAccessResponse) GetAllowed() bool {
	if x == nil { return false }
	return x.Allowed
}
func (x *CheckTenantAccessResponse) GetReason() string {
	if x == nil { return "" }
	return x.Reason
}
func (x *CheckTenantAccessResponse) GetMatchedRuleId() string {
	if x == nil { return "" }
	return x.MatchedRuleId
}

type CreateTenantWebSSHSessionRequest struct {
	TenantId string `json:"tenant_id,omitempty"`
	PeerIp string `json:"peer_ip,omitempty"`
	SshPort int32 `json:"ssh_port,omitempty"`
	Username string `json:"username,omitempty"`
	Password string `json:"password,omitempty"`
	TerminalRows int32 `json:"terminal_rows,omitempty"`
	TerminalCols int32 `json:"terminal_cols,omitempty"`
	PeerId string `json:"peer_id,omitempty"`
	PrivateKey string `json:"private_key,omitempty"`
	PrivateKeyPassphrase string `json:"private_key_passphrase,omitempty"`
	UserAgent string `json:"user_agent,omitempty"`
}

func (x *CreateTenantWebSSHSessionRequest) GetTenantId() string {
	if x == nil { return "" }
	return x.TenantId
}
func (x *CreateTenantWebSSHSessionRequest) GetPeerIp() string {
	if x == nil { return "" }
	return x.PeerIp
}
func (x *CreateTenantWebSSHSessionRequest) GetSshPort() int32 {
	if x == nil { return 0 }
	return x.SshPort
}
func (x *CreateTenantWebSSHSessionRequest) GetUsername() string {
	if x == nil { return "" }
	return x.Username
}
func (x *CreateTenantWebSSHSessionRequest) GetPassword() string {
	if x == nil { return "" }
	return x.Password
}
func (x *CreateTenantWebSSHSessionRequest) GetTerminalRows() int32 {
	if x == nil { return 0 }
	return x.TerminalRows
}
func (x *CreateTenantWebSSHSessionRequest) GetTerminalCols() int32 {
	if x == nil { return 0 }
	return x.TerminalCols
}
func (x *CreateTenantWebSSHSessionRequest) GetPeerId() string {
	if x == nil { return "" }
	return x.PeerId
}
func (x *CreateTenantWebSSHSessionRequest) GetPrivateKey() string {
	if x == nil { return "" }
	return x.PrivateKey
}
func (x *CreateTenantWebSSHSessionRequest) GetPrivateKeyPassphrase() string {
	if x == nil { return "" }
	return x.PrivateKeyPassphrase
}
func (x *CreateTenantWebSSHSessionRequest) GetUserAgent() string {
	if x == nil { return "" }
	return x.UserAgent
}

type CreateTenantWebSSHSessionResponse struct {
	SessionId string `json:"session_id,omitempty"`
	WebsocketUrl string `json:"websocket_url,omitempty"`
	Success bool `json:"success"`
	ErrorMessage string `json:"error_message,omitempty"`
}

func (x *CreateTenantWebSSHSessionResponse) GetSessionId() string {
	if x == nil { return "" }
	return x.SessionId
}
func (x *CreateTenantWebSSHSessionResponse) GetWebsocketUrl() string {
	if x == nil { return "" }
	return x.WebsocketUrl
}
func (x *CreateTenantWebSSHSessionResponse) GetSuccess() bool {
	if x == nil { return false }
	return x.Success
}
func (x *CreateTenantWebSSHSessionResponse) GetErrorMessage() string {
	if x == nil { return "" }
	return x.ErrorMessage
}

type GetTenantWebSSHSessionRequest struct {
	TenantId string `json:"tenant_id,omitempty"`
	SessionId string `json:"session_id,omitempty"`
}

func (x *GetTenantWebSSHSessionRequest) GetTenantId() string {
	if x == nil { return "" }
	return x.TenantId
}
func (x *GetTenantWebSSHSessionRequest) GetSessionId() string {
	if x == nil { return "" }
	return x.SessionId
}

type GetTenantWebSSHSessionResponse struct {
	Session *WebSSHSession `json:"session,omitempty"`
}

func (x *GetTenantWebSSHSessionResponse) GetSession() *WebSSHSession {
	if x == nil { return nil }
	return x.Session
}

type ListTenantWebSSHSessionsRequest struct {
	TenantId string `json:"tenant_id,omitempty"`
}

func (x *ListTenantWebSSHSessionsRequest) GetTenantId() string {
	if x == nil { return "" }
	return x.TenantId
}

type ListTenantWebSSHSessionsResponse struct {
	Sessions []*WebSSHSession `json:"sessions,omitempty"`
}

func (x *ListTenantWebSSHSessionsResponse) GetSessions() []*WebSSHSession {
	if x == nil { return nil }
	return x.Sessions
}

type DisconnectTenantWebSSHSessionRequest struct {
	TenantId string `json:"tenant_id,omitempty"`
	SessionId string `json:"session_id,omitempty"`
}

func (x *DisconnectTenantWebSSHSessionRequest) GetTenantId() string {
	if x == nil { return "" }
	return x.TenantId
}
func (x *DisconnectTenantWebSSHSessionRequest) GetSessionId() string {
	if x == nil { return "" }
	return x.SessionId
}

type DisconnectTenantWebSSHSessionResponse struct {
	Success bool `json:"success"`
}

func (x *DisconnectTenantWebSSHSessionResponse) GetSuccess() bool {
	if x == nil { return false }
	return x.Success
}

type GetEndpointsConfigRequest struct {
}


type GetEndpointsConfigResponse struct {
	WinboxServer string `json:"winbox_server,omitempty"`
	WireguardServer string `json:"wireguard_server,omitempty"`
	WireguardPort int32 `json:"wireguard_port,omitempty"`
}

func (x *GetEndpointsConfigResponse) GetWinboxServer() string {
	if x == nil { return "" }
	return x.WinboxServer
}
func (x *GetEndpointsConfigResponse) GetWireguardServer() string {
	if x == nil { return "" }
	return x.WireguardServer
}
func (x *GetEndpointsConfigResponse) GetWireguardPort() int32 {
	if x == nil { return 0 }
	return x.WireguardPort
}

type SessionActivityLog struct {
	Id string `json:"id,omitempty"`
	AccountId string `json:"account_id,omitempty"`
	PeerId string `json:"peer_id,omitempty"`
	SessionId string `json:"session_id,omitempty"`
	SessionType SessionType `json:"session_type,omitempty"`
	EventType ActivityEventType `json:"event_type,omitempty"`
	Timestamp *Timestamp `json:"timestamp,omitempty"`
	ClientIp string `json:"client_ip,omitempty"`
	UserAgent string `json:"user_agent,omitempty"`
	TargetIp string `json:"target_ip,omitempty"`
	TargetPort int32 `json:"target_port,omitempty"`
	SshUsername string `json:"ssh_username,omitempty"`
	WinboxAccessToken string `json:"winbox_access_token,omitempty"`
	Command string `json:"command,omitempty"`
	RawMessage []byte `json:"raw_message,omitempty"`
	MessageDirection string `json:"message_direction,omitempty"`
	MessageNumber int32 `json:"message_number,omitempty"`
	MessageLength int32 `json:"message_length,omitempty"`
	BytesSent uint64 `json:"bytes_sent,omitempty"`
	BytesReceived uint64 `json:"bytes_received,omitempty"`
	DurationMs int64 `json:"duration_ms,omitempty"`
}

func (x *SessionActivityLog) GetId() string {
	if x == nil { return "" }
	return x.Id
}
func (x *SessionActivityLog) GetAccountId() string {
	if x == nil { return "" }
	return x.AccountId
}
func (x *SessionActivityLog) GetPeerId() string {
	if x == nil { return "" }
	return x.PeerId
}
func (x *SessionActivityLog) GetSessionId() string {
	if x == nil { return "" }
	return x.SessionId
}
func (x *SessionActivityLog) GetSessionType() SessionType {
	if x == nil { return SessionType(0) }
	return x.SessionType
}
func (x *SessionActivityLog) GetEventType() ActivityEventType {
	if x == nil { return ActivityEventType(0) }
	return x.EventType
}
func (x *SessionActivityLog) GetTimestamp() *Timestamp {
	if x == nil { return nil }
	return x.Timestamp
}
func (x *SessionActivityLog) GetClientIp() string {
	if x == nil { return "" }
	return x.ClientIp
}
func (x *SessionActivityLog) GetUserAgent() string {
	if x == nil { return "" }
	return x.UserAgent
}
func (x *SessionActivityLog) GetTargetIp() string {
	if x == nil { return "" }
	return x.TargetIp
}
func (x *SessionActivityLog) GetTargetPort() int32 {
	if x == nil { return 0 }
	return x.TargetPort
}
func (x *SessionActivityLog) GetSshUsername() string {
	if x == nil { return "" }
	return x.SshUsername
}
func (x *SessionActivityLog) GetWinboxAccessToken() string {
	if x == nil { return "" }
	return x.WinboxAccessToken
}
func (x *SessionActivityLog) GetCommand() string {
	if x == nil { return "" }
	return x.Command
}
func (x *SessionActivityLog) GetRawMessage() []byte {
	if x == nil { return nil }
	return x.RawMessage
}
func (x *SessionActivityLog) GetMessageDirection() string {
	if x == nil { return "" }
	return x.MessageDirection
}
func (x *SessionActivityLog) GetMessageNumber() int32 {
	if x == nil { return 0 }
	return x.MessageNumber
}
func (x *SessionActivityLog) GetMessageLength() int32 {
	if x == nil { return 0 }
	return x.MessageLength
}
func (x *SessionActivityLog) GetBytesSent() uint64 {
	if x == nil { return 0 }
	return x.BytesSent
}
func (x *SessionActivityLog) GetBytesReceived() uint64 {
	if x == nil { return 0 }
	return x.BytesReceived
}
func (x *SessionActivityLog) GetDurationMs() int64 {
	if x == nil { return 0 }
	return x.DurationMs
}

type TenantSessionInfo struct {
	SessionId string `json:"session_id,omitempty"`
	IpAddress string `json:"ip_address,omitempty"`
	Browser string `json:"browser,omitempty"`
	BrowserVersion string `json:"browser_version,omitempty"`
	Os string `json:"os,omitempty"`
	DeviceType string `json:"device_type,omitempty"`
	CreatedAt *Timestamp `json:"created_at,omitempty"`
	LastActivity *Timestamp `json:"last_activity,omitempty"`
	ExpiresAt *Timestamp `json:"expires_at,omitempty"`
	IsCurrent bool `json:"is_current"`
}

func (x *TenantSessionInfo) GetSessionId() string {
	if x == nil { return "" }
	return x.SessionId
}
func (x *TenantSessionInfo) GetIpAddress() string {
	if x == nil { return "" }
	return x.IpAddress
}
func (x *TenantSessionInfo) GetBrowser() string {
	if x == nil { return "" }
	return x.Browser
}
func (x *TenantSessionInfo) GetBrowserVersion() string {
	if x == nil { return "" }
	return x.BrowserVersion
}
func (x *TenantSessionInfo) GetOs() string {
	if x == nil { return "" }
	return x.Os
}
func (x *TenantSessionInfo) GetDeviceType() string {
	if x == nil { return "" }
	return x.DeviceType
}
func (x *TenantSessionInfo) GetCreatedAt() *Timestamp {
	if x == nil { return nil }
	return x.CreatedAt
}
func (x *TenantSessionInfo) GetLastActivity() *Timestamp {
	if x == nil { return nil }
	return x.LastActivity
}
func (x *TenantSessionInfo) GetExpiresAt() *Timestamp {
	if x == nil { return nil }
	return x.ExpiresAt
}
func (x *TenantSessionInfo) GetIsCurrent() bool {
	if x == nil { return false }
	return x.IsCurrent
}

type ListTenantSessionsRequest struct {
	TenantId string `json:"tenant_id,omitempty"`
}

func (x *ListTenantSessionsRequest) GetTenantId() string {
	if x == nil { return "" }
	return x.TenantId
}

type ListTenantSessionsResponse struct {
	Sessions []*TenantSessionInfo `json:"sessions,omitempty"`
	CurrentSessionId string `json:"current_session_id,omitempty"`
}

func (x *ListTenantSessionsResponse) GetSessions() []*TenantSessionInfo {
	if x == nil { return nil }
	return x.Sessions
}
func (x *ListTenantSessionsResponse) GetCurrentSessionId() string {
	if x == nil { return "" }
	return x.CurrentSessionId
}

type DeleteTenantSessionRequest struct {
	TenantId string `json:"tenant_id,omitempty"`
	SessionId string `json:"session_id,omitempty"`
}

func (x *DeleteTenantSessionRequest) GetTenantId() string {
	if x == nil { return "" }
	return x.TenantId
}
func (x *DeleteTenantSessionRequest) GetSessionId() string {
	if x == nil { return "" }
	return x.SessionId
}

type DeleteTenantSessionResponse struct {
	Success bool `json:"success"`
	Message string `json:"message,omitempty"`
}

func (x *DeleteTenantSessionResponse) GetSuccess() bool {
	if x == nil { return false }
	return x.Success
}
func (x *DeleteTenantSessionResponse) GetMessage() string {
	if x == nil { return "" }
	return x.Message
}

type RegisterDeviceRequest struct {
	Token string `json:"token,omitempty"`
	Nonce int64 `json:"nonce,omitempty"`
	Os string `json:"os,omitempty"`
	Arch string `json:"arch,omitempty"`
	Hostname string `json:"hostname,omitempty"`
	DeviceId string `json:"device_id,omitempty"`
}

func (x *RegisterDeviceRequest) GetToken() string {
	if x == nil { return "" }
	return x.Token
}
func (x *RegisterDeviceRequest) GetNonce() int64 {
	if x == nil { return 0 }
	return x.Nonce
}
func (x *RegisterDeviceRequest) GetOs() string {
	if x == nil { return "" }
	return x.Os
}
func (x *RegisterDeviceRequest) GetArch() string {
	if x == nil { return "" }
	return x.Arch
}
func (x *RegisterDeviceRequest) GetHostname() string {
	if x == nil { return "" }
	return x.Hostname
}
func (x *RegisterDeviceRequest) GetDeviceId() string {
	if x == nil { return "" }
	return x.DeviceId
}

type RegisterDeviceResponse struct {
	Success bool `json:"success"`
	Token string `json:"token,omitempty"`
	ServerKey string `json:"server_key,omitempty"`
	Endpoint string `json:"endpoint,omitempty"`
	AllowedIps []string `json:"allowed_ips,omitempty"`
	PersistentKeepalive int32 `json:"persistent_keepalive,omitempty"`
	DnsServers []string `json:"dns_servers,omitempty"`
	ForwardingRules []string `json:"forwarding_rules,omitempty"`
	Routes []string `json:"routes,omitempty"`
	Mtu int32 `json:"mtu,omitempty"`
	ListenPort int32 `json:"listen_port,omitempty"`
	EncryptedConfig []byte `json:"encrypted_config,omitempty"`
}

func (x *RegisterDeviceResponse) GetSuccess() bool {
	if x == nil { return false }
	return x.Success
}
func (x *RegisterDeviceResponse) GetToken() string {
	if x == nil { return "" }
	return x.Token
}
func (x *RegisterDeviceResponse) GetServerKey() string {
	if x == nil { return "" }
	return x.ServerKey
}
func (x *RegisterDeviceResponse) GetEndpoint() string {
	if x == nil { return "" }
	return x.Endpoint
}
func (x *RegisterDeviceResponse) GetAllowedIps() []string {
	if x == nil { return nil }
	return x.AllowedIps
}
func (x *RegisterDeviceResponse) GetPersistentKeepalive() int32 {
	if x == nil { return 0 }
	return x.PersistentKeepalive
}
func (x *RegisterDeviceResponse) GetDnsServers() []string {
	if x == nil { return nil }
	return x.DnsServers
}
func (x *RegisterDeviceResponse) GetForwardingRules() []string {
	if x == nil { return nil }
	return x.ForwardingRules
}
func (x *RegisterDeviceResponse) GetRoutes() []string {
	if x == nil { return nil }
	return x.Routes
}
func (x *RegisterDeviceResponse) GetMtu() int32 {
	if x == nil { return 0 }
	return x.Mtu
}
func (x *RegisterDeviceResponse) GetListenPort() int32 {
	if x == nil { return 0 }
	return x.ListenPort
}
func (x *RegisterDeviceResponse) GetEncryptedConfig() []byte {
	if x == nil { return nil }
	return x.EncryptedConfig
}

type GetClaimedDeviceConfigRequest struct {
	PublicKey string `json:"public_key,omitempty"`
}

func (x *GetClaimedDeviceConfigRequest) GetPublicKey() string {
	if x == nil { return "" }
	return x.PublicKey
}

type GetClaimedDeviceConfigResponse struct {
	Claimed bool `json:"claimed"`
	PublicKey string `json:"public_key,omitempty"`
	AssignedIp string `json:"assigned_ip,omitempty"`
	ServerKey string `json:"server_key,omitempty"`
	Endpoint string `json:"endpoint,omitempty"`
	AllowedIps []string `json:"allowed_ips,omitempty"`
	DnsServers []string `json:"dns_servers,omitempty"`
	PersistentKeepalive int32 `json:"persistent_keepalive,omitempty"`
	Mtu int32 `json:"mtu,omitempty"`
	ListenPort int32 `json:"listen_port,omitempty"`
}

func (x *GetClaimedDeviceConfigResponse) GetClaimed() bool {
	if x == nil { return false }
	return x.Claimed
}
func (x *GetClaimedDeviceConfigResponse) GetPublicKey() string {
	if x == nil { return "" }
	return x.PublicKey
}
func (x *GetClaimedDeviceConfigResponse) GetAssignedIp() string {
	if x == nil { return "" }
	return x.AssignedIp
}
func (x *GetClaimedDeviceConfigResponse) GetServerKey() string {
	if x == nil { return "" }
	return x.ServerKey
}
func (x *GetClaimedDeviceConfigResponse) GetEndpoint() string {
	if x == nil { return "" }
	return x.Endpoint
}
func (x *GetClaimedDeviceConfigResponse) GetAllowedIps() []string {
	if x == nil { return nil }
	return x.AllowedIps
}
func (x *GetClaimedDeviceConfigResponse) GetDnsServers() []string {
	if x == nil { return nil }
	return x.DnsServers
}
func (x *GetClaimedDeviceConfigResponse) GetPersistentKeepalive() int32 {
	if x == nil { return 0 }
	return x.PersistentKeepalive
}
func (x *GetClaimedDeviceConfigResponse) GetMtu() int32 {
	if x == nil { return 0 }
	return x.Mtu
}
func (x *GetClaimedDeviceConfigResponse) GetListenPort() int32 {
	if x == nil { return 0 }
	return x.ListenPort
}

type DeviceRefreshTokenRequest struct {
	Token string `json:"token,omitempty"`
}

func (x *DeviceRefreshTokenRequest) GetToken() string {
	if x == nil { return "" }
	return x.Token
}

type DeviceRefreshTokenResponse struct {
	Success bool `json:"success"`
	Token string `json:"token,omitempty"`
}

func (x *DeviceRefreshTokenResponse) GetSuccess() bool {
	if x == nil { return false }
	return x.Success
}
func (x *DeviceRefreshTokenResponse) GetToken() string {
	if x == nil { return "" }
	return x.Token
}

type GetDeviceConfigurationRequest struct {
	Token string `json:"token,omitempty"`
}

func (x *GetDeviceConfigurationRequest) GetToken() string {
	if x == nil { return "" }
	return x.Token
}

type GetDeviceConfigurationResponse struct {
	DeviceConfig *DeviceNetworkConfig `json:"device_config,omitempty"`
	ServerConfig *DeviceServerConfig `json:"server_config,omitempty"`
	NetworkConfig *DeviceNetworkRoutes `json:"network_config,omitempty"`
	ExitNodeConfig *DeviceExitNodeConfig `json:"exit_node_config,omitempty"`
	UpdateAvailable bool `json:"update_available"`
	UpdateVersion string `json:"update_version,omitempty"`
	UpdateUrl string `json:"update_url,omitempty"`
}

func (x *GetDeviceConfigurationResponse) GetDeviceConfig() *DeviceNetworkConfig {
	if x == nil { return nil }
	return x.DeviceConfig
}
func (x *GetDeviceConfigurationResponse) GetServerConfig() *DeviceServerConfig {
	if x == nil { return nil }
	return x.ServerConfig
}
func (x *GetDeviceConfigurationResponse) GetNetworkConfig() *DeviceNetworkRoutes {
	if x == nil { return nil }
	return x.NetworkConfig
}
func (x *GetDeviceConfigurationResponse) GetExitNodeConfig() *DeviceExitNodeConfig {
	if x == nil { return nil }
	return x.ExitNodeConfig
}
func (x *GetDeviceConfigurationResponse) GetUpdateAvailable() bool {
	if x == nil { return false }
	return x.UpdateAvailable
}
func (x *GetDeviceConfigurationResponse) GetUpdateVersion() string {
	if x == nil { return "" }
	return x.UpdateVersion
}
func (x *GetDeviceConfigurationResponse) GetUpdateUrl() string {
	if x == nil { return "" }
	return x.UpdateUrl
}

type DeviceNetworkConfig struct {
	Addresses []string `json:"addresses,omitempty"`
	ListenPort int32 `json:"listen_port,omitempty"`
	Mtu int32 `json:"mtu,omitempty"`
	Dns []string `json:"dns,omitempty"`
}

func (x *DeviceNetworkConfig) GetAddresses() []string {
	if x == nil { return nil }
	return x.Addresses
}
func (x *DeviceNetworkConfig) GetListenPort() int32 {
	if x == nil { return 0 }
	return x.ListenPort
}
func (x *DeviceNetworkConfig) GetMtu() int32 {
	if x == nil { return 0 }
	return x.Mtu
}
func (x *DeviceNetworkConfig) GetDns() []string {
	if x == nil { return nil }
	return x.Dns
}

type DeviceServerConfig struct {
	Endpoint string `json:"endpoint,omitempty"`
	Port int32 `json:"port,omitempty"`
	PublicKey string `json:"public_key,omitempty"`
	AllowedIps []string `json:"allowed_ips,omitempty"`
	PersistentKeepalive int32 `json:"persistent_keepalive,omitempty"`
}

func (x *DeviceServerConfig) GetEndpoint() string {
	if x == nil { return "" }
	return x.Endpoint
}
func (x *DeviceServerConfig) GetPort() int32 {
	if x == nil { return 0 }
	return x.Port
}
func (x *DeviceServerConfig) GetPublicKey() string {
	if x == nil { return "" }
	return x.PublicKey
}
func (x *DeviceServerConfig) GetAllowedIps() []string {
	if x == nil { return nil }
	return x.AllowedIps
}
func (x *DeviceServerConfig) GetPersistentKeepalive() int32 {
	if x == nil { return 0 }
	return x.PersistentKeepalive
}

type DeviceNetworkRoutes struct {
	Routes []string `json:"routes,omitempty"`
	ForwardingRules []string `json:"forwarding_rules,omitempty"`
	FirewallRules []string `json:"firewall_rules,omitempty"`
}

func (x *DeviceNetworkRoutes) GetRoutes() []string {
	if x == nil { return nil }
	return x.Routes
}
func (x *DeviceNetworkRoutes) GetForwardingRules() []string {
	if x == nil { return nil }
	return x.ForwardingRules
}
func (x *DeviceNetworkRoutes) GetFirewallRules() []string {
	if x == nil { return nil }
	return x.FirewallRules
}

type DeviceExitNodeConfig struct {
	Enabled bool `json:"enabled"`
	ExitRoutes []string `json:"exit_routes,omitempty"`
	ExitDns []string `json:"exit_dns,omitempty"`
	AllowLan bool `json:"allow_lan"`
}

func (x *DeviceExitNodeConfig) GetEnabled() bool {
	if x == nil { return false }
	return x.Enabled
}
func (x *DeviceExitNodeConfig) GetExitRoutes() []string {
	if x == nil { return nil }
	return x.ExitRoutes
}
func (x *DeviceExitNodeConfig) GetExitDns() []string {
	if x == nil { return nil }
	return x.ExitDns
}
func (x *DeviceExitNodeConfig) GetAllowLan() bool {
	if x == nil { return false }
	return x.AllowLan
}

type StartDeviceFlowRequest struct {
	DeviceId string `json:"device_id,omitempty"`
}

func (x *StartDeviceFlowRequest) GetDeviceId() string {
	if x == nil { return "" }
	return x.DeviceId
}

type StartDeviceFlowResponse struct {
	DeviceCode string `json:"device_code,omitempty"`
	UserCode string `json:"user_code,omitempty"`
	VerificationUri string `json:"verification_uri,omitempty"`
	ExpiresIn int32 `json:"expires_in,omitempty"`
	Interval int32 `json:"interval,omitempty"`
}

func (x *StartDeviceFlowResponse) GetDeviceCode() string {
	if x == nil { return "" }
	return x.DeviceCode
}
func (x *StartDeviceFlowResponse) GetUserCode() string {
	if x == nil { return "" }
	return x.UserCode
}
func (x *StartDeviceFlowResponse) GetVerificationUri() string {
	if x == nil { return "" }
	return x.VerificationUri
}
func (x *StartDeviceFlowResponse) GetExpiresIn() int32 {
	if x == nil { return 0 }
	return x.ExpiresIn
}
func (x *StartDeviceFlowResponse) GetInterval() int32 {
	if x == nil { return 0 }
	return x.Interval
}

type PollDeviceFlowRequest struct {
	DeviceCode string `json:"device_code,omitempty"`
}

func (x *PollDeviceFlowRequest) GetDeviceCode() string {
	if x == nil { return "" }
	return x.DeviceCode
}

type PollDeviceFlowResponse struct {
	Success bool `json:"success"`
	Token string `json:"token,omitempty"`
}

func (x *PollDeviceFlowResponse) GetSuccess() bool {
	if x == nil { return false }
	return x.Success
}
func (x *PollDeviceFlowResponse) GetToken() string {
	if x == nil { return "" }
	return x.Token
}

type SharePermissions struct {
	ViewPeers bool `json:"view_peers"`
	ManagePeers bool `json:"manage_peers"`
	ViewTopology bool `json:"view_topology"`
	ManageWinbox bool `json:"manage_winbox"`
	ManageWebssh bool `json:"manage_webssh"`
	ViewAcl bool `json:"view_acl"`
	ManageAcl bool `json:"manage_acl"`
	ViewActivity bool `json:"view_activity"`
}

func (x *SharePermissions) GetViewPeers() bool {
	if x == nil { return false }
	return x.ViewPeers
}
func (x *SharePermissions) GetManagePeers() bool {
	if x == nil { return false }
	return x.ManagePeers
}
func (x *SharePermissions) GetViewTopology() bool {
	if x == nil { return false }
	return x.ViewTopology
}
func (x *SharePermissions) GetManageWinbox() bool {
	if x == nil { return false }
	return x.ManageWinbox
}
func (x *SharePermissions) GetManageWebssh() bool {
	if x == nil { return false }
	return x.ManageWebssh
}
func (x *SharePermissions) GetViewAcl() bool {
	if x == nil { return false }
	return x.ViewAcl
}
func (x *SharePermissions) GetManageAcl() bool {
	if x == nil { return false }
	return x.ManageAcl
}
func (x *SharePermissions) GetViewActivity() bool {
	if x == nil { return false }
	return x.ViewActivity
}

type AccessShareInfo struct {
	ShareId string `json:"share_id,omitempty"`
	OwnerTenantId string `json:"owner_tenant_id,omitempty"`
	OwnerEmail string `json:"owner_email,omitempty"`
	OwnerName string `json:"owner_name,omitempty"`
	SharedEmail string `json:"shared_email,omitempty"`
	ShareeName string `json:"sharee_name,omitempty"`
	Permissions *SharePermissions `json:"permissions,omitempty"`
	TagFilter []string `json:"tag_filter,omitempty"`
	Status string `json:"status,omitempty"`
	InviteToken string `json:"invite_token,omitempty"`
	ResendCount int32 `json:"resend_count,omitempty"`
	CreatedAt *Timestamp `json:"created_at,omitempty"`
	AcceptedAt *Timestamp `json:"accepted_at,omitempty"`
	ExpiresAt *Timestamp `json:"expires_at,omitempty"`
	LastResendAt *Timestamp `json:"last_resend_at,omitempty"`
	IsLinkShare bool `json:"is_link_share"`
}

func (x *AccessShareInfo) GetShareId() string {
	if x == nil { return "" }
	return x.ShareId
}
func (x *AccessShareInfo) GetOwnerTenantId() string {
	if x == nil { return "" }
	return x.OwnerTenantId
}
func (x *AccessShareInfo) GetOwnerEmail() string {
	if x == nil { return "" }
	return x.OwnerEmail
}
func (x *AccessShareInfo) GetOwnerName() string {
	if x == nil { return "" }
	return x.OwnerName
}
func (x *AccessShareInfo) GetSharedEmail() string {
	if x == nil { return "" }
	return x.SharedEmail
}
func (x *AccessShareInfo) GetShareeName() string {
	if x == nil { return "" }
	return x.ShareeName
}
func (x *AccessShareInfo) GetPermissions() *SharePermissions {
	if x == nil { return nil }
	return x.Permissions
}
func (x *AccessShareInfo) GetTagFilter() []string {
	if x == nil { return nil }
	return x.TagFilter
}
func (x *AccessShareInfo) GetStatus() string {
	if x == nil { return "" }
	return x.Status
}
func (x *AccessShareInfo) GetInviteToken() string {
	if x == nil { return "" }
	return x.InviteToken
}
func (x *AccessShareInfo) GetResendCount() int32 {
	if x == nil { return 0 }
	return x.ResendCount
}
func (x *AccessShareInfo) GetCreatedAt() *Timestamp {
	if x == nil { return nil }
	return x.CreatedAt
}
func (x *AccessShareInfo) GetAcceptedAt() *Timestamp {
	if x == nil { return nil }
	return x.AcceptedAt
}
func (x *AccessShareInfo) GetExpiresAt() *Timestamp {
	if x == nil { return nil }
	return x.ExpiresAt
}
func (x *AccessShareInfo) GetLastResendAt() *Timestamp {
	if x == nil { return nil }
	return x.LastResendAt
}
func (x *AccessShareInfo) GetIsLinkShare() bool {
	if x == nil { return false }
	return x.IsLinkShare
}

type AccessibleAccountInfo struct {
	OwnerTenantId string `json:"owner_tenant_id,omitempty"`
	OwnerEmail string `json:"owner_email,omitempty"`
	OwnerName string `json:"owner_name,omitempty"`
	ShareId string `json:"share_id,omitempty"`
	ShareeName string `json:"sharee_name,omitempty"`
	Permissions *SharePermissions `json:"permissions,omitempty"`
	TagFilter []string `json:"tag_filter,omitempty"`
	AcceptedAt *Timestamp `json:"accepted_at,omitempty"`
}

func (x *AccessibleAccountInfo) GetOwnerTenantId() string {
	if x == nil { return "" }
	return x.OwnerTenantId
}
func (x *AccessibleAccountInfo) GetOwnerEmail() string {
	if x == nil { return "" }
	return x.OwnerEmail
}
func (x *AccessibleAccountInfo) GetOwnerName() string {
	if x == nil { return "" }
	return x.OwnerName
}
func (x *AccessibleAccountInfo) GetShareId() string {
	if x == nil { return "" }
	return x.ShareId
}
func (x *AccessibleAccountInfo) GetShareeName() string {
	if x == nil { return "" }
	return x.ShareeName
}
func (x *AccessibleAccountInfo) GetPermissions() *SharePermissions {
	if x == nil { return nil }
	return x.Permissions
}
func (x *AccessibleAccountInfo) GetTagFilter() []string {
	if x == nil { return nil }
	return x.TagFilter
}
func (x *AccessibleAccountInfo) GetAcceptedAt() *Timestamp {
	if x == nil { return nil }
	return x.AcceptedAt
}

type ListAccessSharesRequest struct {
	TenantId string `json:"tenant_id,omitempty"`
}

func (x *ListAccessSharesRequest) GetTenantId() string {
	if x == nil { return "" }
	return x.TenantId
}

type ListAccessSharesResponse struct {
	Shares []*AccessShareInfo `json:"shares,omitempty"`
	TeammateLimit int32 `json:"teammate_limit,omitempty"`
	TeammateUsed int32 `json:"teammate_used,omitempty"`
}

func (x *ListAccessSharesResponse) GetShares() []*AccessShareInfo {
	if x == nil { return nil }
	return x.Shares
}
func (x *ListAccessSharesResponse) GetTeammateLimit() int32 {
	if x == nil { return 0 }
	return x.TeammateLimit
}
func (x *ListAccessSharesResponse) GetTeammateUsed() int32 {
	if x == nil { return 0 }
	return x.TeammateUsed
}

type CreateAccessShareRequest struct {
	TenantId string `json:"tenant_id,omitempty"`
	SharedEmail string `json:"shared_email,omitempty"`
	ShareeName string `json:"sharee_name,omitempty"`
	Permissions *SharePermissions `json:"permissions,omitempty"`
	TagFilter []string `json:"tag_filter,omitempty"`
	IsLinkShare bool `json:"is_link_share"`
}

func (x *CreateAccessShareRequest) GetTenantId() string {
	if x == nil { return "" }
	return x.TenantId
}
func (x *CreateAccessShareRequest) GetSharedEmail() string {
	if x == nil { return "" }
	return x.SharedEmail
}
func (x *CreateAccessShareRequest) GetShareeName() string {
	if x == nil { return "" }
	return x.ShareeName
}
func (x *CreateAccessShareRequest) GetPermissions() *SharePermissions {
	if x == nil { return nil }
	return x.Permissions
}
func (x *CreateAccessShareRequest) GetTagFilter() []string {
	if x == nil { return nil }
	return x.TagFilter
}
func (x *CreateAccessShareRequest) GetIsLinkShare() bool {
	if x == nil { return false }
	return x.IsLinkShare
}

type CreateAccessShareResponse struct {
	Success bool `json:"success"`
	Message string `json:"message,omitempty"`
	Share *AccessShareInfo `json:"share,omitempty"`
	InviteUrl string `json:"invite_url,omitempty"`
	QrCode string `json:"qr_code,omitempty"`
}

func (x *CreateAccessShareResponse) GetSuccess() bool {
	if x == nil { return false }
	return x.Success
}
func (x *CreateAccessShareResponse) GetMessage() string {
	if x == nil { return "" }
	return x.Message
}
func (x *CreateAccessShareResponse) GetShare() *AccessShareInfo {
	if x == nil { return nil }
	return x.Share
}
func (x *CreateAccessShareResponse) GetInviteUrl() string {
	if x == nil { return "" }
	return x.InviteUrl
}
func (x *CreateAccessShareResponse) GetQrCode() string {
	if x == nil { return "" }
	return x.QrCode
}

type DeleteAccessShareRequest struct {
	TenantId string `json:"tenant_id,omitempty"`
	ShareId string `json:"share_id,omitempty"`
}

func (x *DeleteAccessShareRequest) GetTenantId() string {
	if x == nil { return "" }
	return x.TenantId
}
func (x *DeleteAccessShareRequest) GetShareId() string {
	if x == nil { return "" }
	return x.ShareId
}

type DeleteAccessShareResponse struct {
	Success bool `json:"success"`
	Message string `json:"message,omitempty"`
}

func (x *DeleteAccessShareResponse) GetSuccess() bool {
	if x == nil { return false }
	return x.Success
}
func (x *DeleteAccessShareResponse) GetMessage() string {
	if x == nil { return "" }
	return x.Message
}

type ResendAccessShareInviteRequest struct {
	TenantId string `json:"tenant_id,omitempty"`
	ShareId string `json:"share_id,omitempty"`
}

func (x *ResendAccessShareInviteRequest) GetTenantId() string {
	if x == nil { return "" }
	return x.TenantId
}
func (x *ResendAccessShareInviteRequest) GetShareId() string {
	if x == nil { return "" }
	return x.ShareId
}

type ResendAccessShareInviteResponse struct {
	Success bool `json:"success"`
	Message string `json:"message,omitempty"`
	RetryAfterSeconds int32 `json:"retry_after_seconds,omitempty"`
}

func (x *ResendAccessShareInviteResponse) GetSuccess() bool {
	if x == nil { return false }
	return x.Success
}
func (x *ResendAccessShareInviteResponse) GetMessage() string {
	if x == nil { return "" }
	return x.Message
}
func (x *ResendAccessShareInviteResponse) GetRetryAfterSeconds() int32 {
	if x == nil { return 0 }
	return x.RetryAfterSeconds
}

type GetPendingSharesRequest struct {
	TenantId string `json:"tenant_id,omitempty"`
}

func (x *GetPendingSharesRequest) GetTenantId() string {
	if x == nil { return "" }
	return x.TenantId
}

type GetPendingSharesResponse struct {
	PendingShares []*AccessShareInfo `json:"pending_shares,omitempty"`
}

func (x *GetPendingSharesResponse) GetPendingShares() []*AccessShareInfo {
	if x == nil { return nil }
	return x.PendingShares
}

type AcceptAccessShareRequest struct {
	TenantId string `json:"tenant_id,omitempty"`
	ShareId string `json:"share_id,omitempty"`
	InviteToken string `json:"invite_token,omitempty"`
}

func (x *AcceptAccessShareRequest) GetTenantId() string {
	if x == nil { return "" }
	return x.TenantId
}
func (x *AcceptAccessShareRequest) GetShareId() string {
	if x == nil { return "" }
	return x.ShareId
}
func (x *AcceptAccessShareRequest) GetInviteToken() string {
	if x == nil { return "" }
	return x.InviteToken
}

type AcceptAccessShareResponse struct {
	Success bool `json:"success"`
	Message string `json:"message,omitempty"`
	Share *AccessShareInfo `json:"share,omitempty"`
}

func (x *AcceptAccessShareResponse) GetSuccess() bool {
	if x == nil { return false }
	return x.Success
}
func (x *AcceptAccessShareResponse) GetMessage() string {
	if x == nil { return "" }
	return x.Message
}
func (x *AcceptAccessShareResponse) GetShare() *AccessShareInfo {
	if x == nil { return nil }
	return x.Share
}

type RejectAccessShareRequest struct {
	TenantId string `json:"tenant_id,omitempty"`
	InviteToken string `json:"invite_token,omitempty"`
}

func (x *RejectAccessShareRequest) GetTenantId() string {
	if x == nil { return "" }
	return x.TenantId
}
func (x *RejectAccessShareRequest) GetInviteToken() string {
	if x == nil { return "" }
	return x.InviteToken
}

type RejectAccessShareResponse struct {
	Success bool `json:"success"`
	Message string `json:"message,omitempty"`
}

func (x *RejectAccessShareResponse) GetSuccess() bool {
	if x == nil { return false }
	return x.Success
}
func (x *RejectAccessShareResponse) GetMessage() string {
	if x == nil { return "" }
	return x.Message
}

type ListAccessibleAccountsRequest struct {
	TenantId string `json:"tenant_id,omitempty"`
}

func (x *ListAccessibleAccountsRequest) GetTenantId() string {
	if x == nil { return "" }
	return x.TenantId
}

type ListAccessibleAccountsResponse struct {
	Accounts []*AccessibleAccountInfo `json:"accounts,omitempty"`
}

func (x *ListAccessibleAccountsResponse) GetAccounts() []*AccessibleAccountInfo {
	if x == nil { return nil }
	return x.Accounts
}

type GetAccessShareByTokenRequest struct {
	InviteToken string `json:"invite_token,omitempty"`
}

func (x *GetAccessShareByTokenRequest) GetInviteToken() string {
	if x == nil { return "" }
	return x.InviteToken
}

type GetAccessShareByTokenResponse struct {
	Valid bool `json:"valid"`
	OwnerEmail string `json:"owner_email,omitempty"`
	OwnerName string `json:"owner_name,omitempty"`
	ShareId string `json:"share_id,omitempty"`
	Permissions *SharePermissions `json:"permissions,omitempty"`
	TagFilter []string `json:"tag_filter,omitempty"`
	Status string `json:"status,omitempty"`
	ExpiresAt *Timestamp `json:"expires_at,omitempty"`
	IsLinkShare bool `json:"is_link_share"`
}

func (x *GetAccessShareByTokenResponse) GetValid() bool {
	if x == nil { return false }
	return x.Valid
}
func (x *GetAccessShareByTokenResponse) GetOwnerEmail() string {
	if x == nil { return "" }
	return x.OwnerEmail
}
func (x *GetAccessShareByTokenResponse) GetOwnerName() string {
	if x == nil { return "" }
	return x.OwnerName
}
func (x *GetAccessShareByTokenResponse) GetShareId() string {
	if x == nil { return "" }
	return x.ShareId
}
func (x *GetAccessShareByTokenResponse) GetPermissions() *SharePermissions {
	if x == nil { return nil }
	return x.Permissions
}
func (x *GetAccessShareByTokenResponse) GetTagFilter() []string {
	if x == nil { return nil }
	return x.TagFilter
}
func (x *GetAccessShareByTokenResponse) GetStatus() string {
	if x == nil { return "" }
	return x.Status
}
func (x *GetAccessShareByTokenResponse) GetExpiresAt() *Timestamp {
	if x == nil { return nil }
	return x.ExpiresAt
}
func (x *GetAccessShareByTokenResponse) GetIsLinkShare() bool {
	if x == nil { return false }
	return x.IsLinkShare
}

type EnsureWUSPSubscriptionRequest struct {
	PeerId string `json:"peer_id,omitempty"`
	AccountId string `json:"account_id,omitempty"`
}

func (x *EnsureWUSPSubscriptionRequest) GetPeerId() string {
	if x == nil { return "" }
	return x.PeerId
}
func (x *EnsureWUSPSubscriptionRequest) GetAccountId() string {
	if x == nil { return "" }
	return x.AccountId
}

type EnsureWUSPSubscriptionResponse struct {
	Success bool `json:"success"`
	Error string `json:"error,omitempty"`
}

func (x *EnsureWUSPSubscriptionResponse) GetSuccess() bool {
	if x == nil { return false }
	return x.Success
}
func (x *EnsureWUSPSubscriptionResponse) GetError() string {
	if x == nil { return "" }
	return x.Error
}

type CancelWUSPSubscriptionRequest struct {
	PeerId string `json:"peer_id,omitempty"`
	AccountId string `json:"account_id,omitempty"`
}

func (x *CancelWUSPSubscriptionRequest) GetPeerId() string {
	if x == nil { return "" }
	return x.PeerId
}
func (x *CancelWUSPSubscriptionRequest) GetAccountId() string {
	if x == nil { return "" }
	return x.AccountId
}

type CancelWUSPSubscriptionResponse struct {
	Success bool `json:"success"`
	Error string `json:"error,omitempty"`
}

func (x *CancelWUSPSubscriptionResponse) GetSuccess() bool {
	if x == nil { return false }
	return x.Success
}
func (x *CancelWUSPSubscriptionResponse) GetError() string {
	if x == nil { return "" }
	return x.Error
}

type RouterOSCapability struct {
	Candidate bool `json:"candidate"`
	ApiReady bool `json:"api_ready"`
	ApiPort int32 `json:"api_port,omitempty"`
	ApiTls bool `json:"api_tls"`
	LastValidated *Timestamp `json:"last_validated,omitempty"`
	LastError string `json:"last_error,omitempty"`
	SessionId string `json:"session_id,omitempty"`
	HasSavedWinbox bool `json:"has_saved_winbox"`
	HasSavedAccess bool `json:"has_saved_access"`
	CredentialSource string `json:"credential_source,omitempty"`
	PreferredUsername string `json:"preferred_username,omitempty"`
}

func (x *RouterOSCapability) GetCandidate() bool {
	if x == nil { return false }
	return x.Candidate
}
func (x *RouterOSCapability) GetApiReady() bool {
	if x == nil { return false }
	return x.ApiReady
}
func (x *RouterOSCapability) GetApiPort() int32 {
	if x == nil { return 0 }
	return x.ApiPort
}
func (x *RouterOSCapability) GetApiTls() bool {
	if x == nil { return false }
	return x.ApiTls
}
func (x *RouterOSCapability) GetLastValidated() *Timestamp {
	if x == nil { return nil }
	return x.LastValidated
}
func (x *RouterOSCapability) GetLastError() string {
	if x == nil { return "" }
	return x.LastError
}
func (x *RouterOSCapability) GetSessionId() string {
	if x == nil { return "" }
	return x.SessionId
}
func (x *RouterOSCapability) GetHasSavedWinbox() bool {
	if x == nil { return false }
	return x.HasSavedWinbox
}
func (x *RouterOSCapability) GetHasSavedAccess() bool {
	if x == nil { return false }
	return x.HasSavedAccess
}
func (x *RouterOSCapability) GetCredentialSource() string {
	if x == nil { return "" }
	return x.CredentialSource
}
func (x *RouterOSCapability) GetPreferredUsername() string {
	if x == nil { return "" }
	return x.PreferredUsername
}

type RouterOSIdentity struct {
	Identity string `json:"identity,omitempty"`
	Version string `json:"version,omitempty"`
	BoardName string `json:"board_name,omitempty"`
	Model string `json:"model,omitempty"`
	Platform string `json:"platform,omitempty"`
	Architecture string `json:"architecture,omitempty"`
	Cpu string `json:"cpu,omitempty"`
}

func (x *RouterOSIdentity) GetIdentity() string {
	if x == nil { return "" }
	return x.Identity
}
func (x *RouterOSIdentity) GetVersion() string {
	if x == nil { return "" }
	return x.Version
}
func (x *RouterOSIdentity) GetBoardName() string {
	if x == nil { return "" }
	return x.BoardName
}
func (x *RouterOSIdentity) GetModel() string {
	if x == nil { return "" }
	return x.Model
}
func (x *RouterOSIdentity) GetPlatform() string {
	if x == nil { return "" }
	return x.Platform
}
func (x *RouterOSIdentity) GetArchitecture() string {
	if x == nil { return "" }
	return x.Architecture
}
func (x *RouterOSIdentity) GetCpu() string {
	if x == nil { return "" }
	return x.Cpu
}

type RouterOSRecord struct {
	Id string `json:"id,omitempty"`
	Fields map[string]string `json:"fields,omitempty"`
}

func (x *RouterOSRecord) GetId() string {
	if x == nil { return "" }
	return x.Id
}
func (x *RouterOSRecord) GetFields() map[string]string {
	if x == nil { return nil }
	return x.Fields
}

type GetRouterOSOverviewRequest struct {
	PeerId string `json:"peer_id,omitempty"`
	AccountId string `json:"account_id,omitempty"`
}

func (x *GetRouterOSOverviewRequest) GetPeerId() string {
	if x == nil { return "" }
	return x.PeerId
}
func (x *GetRouterOSOverviewRequest) GetAccountId() string {
	if x == nil { return "" }
	return x.AccountId
}

type ConfigureRouterOSAccessRequest struct {
	PeerId string `json:"peer_id,omitempty"`
	AccountId string `json:"account_id,omitempty"`
	Username string `json:"username,omitempty"`
	Password string `json:"password,omitempty"`
	Port int32 `json:"port,omitempty"`
	UseTls bool `json:"use_tls"`
	UseSavedWinbox bool `json:"use_saved_winbox"`
}

func (x *ConfigureRouterOSAccessRequest) GetPeerId() string {
	if x == nil { return "" }
	return x.PeerId
}
func (x *ConfigureRouterOSAccessRequest) GetAccountId() string {
	if x == nil { return "" }
	return x.AccountId
}
func (x *ConfigureRouterOSAccessRequest) GetUsername() string {
	if x == nil { return "" }
	return x.Username
}
func (x *ConfigureRouterOSAccessRequest) GetPassword() string {
	if x == nil { return "" }
	return x.Password
}
func (x *ConfigureRouterOSAccessRequest) GetPort() int32 {
	if x == nil { return 0 }
	return x.Port
}
func (x *ConfigureRouterOSAccessRequest) GetUseTls() bool {
	if x == nil { return false }
	return x.UseTls
}
func (x *ConfigureRouterOSAccessRequest) GetUseSavedWinbox() bool {
	if x == nil { return false }
	return x.UseSavedWinbox
}

type GetRouterOSOverviewResponse struct {
	Success bool `json:"success"`
	Error string `json:"error,omitempty"`
	Capability *RouterOSCapability `json:"capability,omitempty"`
	Identity *RouterOSIdentity `json:"identity,omitempty"`
	SystemResource map[string]string `json:"system_resource,omitempty"`
	Routerboard map[string]string `json:"routerboard,omitempty"`
}

func (x *GetRouterOSOverviewResponse) GetSuccess() bool {
	if x == nil { return false }
	return x.Success
}
func (x *GetRouterOSOverviewResponse) GetError() string {
	if x == nil { return "" }
	return x.Error
}
func (x *GetRouterOSOverviewResponse) GetCapability() *RouterOSCapability {
	if x == nil { return nil }
	return x.Capability
}
func (x *GetRouterOSOverviewResponse) GetIdentity() *RouterOSIdentity {
	if x == nil { return nil }
	return x.Identity
}
func (x *GetRouterOSOverviewResponse) GetSystemResource() map[string]string {
	if x == nil { return nil }
	return x.SystemResource
}
func (x *GetRouterOSOverviewResponse) GetRouterboard() map[string]string {
	if x == nil { return nil }
	return x.Routerboard
}

type ConfigureRouterOSAccessResponse struct {
	Success bool `json:"success"`
	Error string `json:"error,omitempty"`
	Capability *RouterOSCapability `json:"capability,omitempty"`
}

func (x *ConfigureRouterOSAccessResponse) GetSuccess() bool {
	if x == nil { return false }
	return x.Success
}
func (x *ConfigureRouterOSAccessResponse) GetError() string {
	if x == nil { return "" }
	return x.Error
}
func (x *ConfigureRouterOSAccessResponse) GetCapability() *RouterOSCapability {
	if x == nil { return nil }
	return x.Capability
}

type ListRouterOSResourceRequest struct {
	PeerId string `json:"peer_id,omitempty"`
	AccountId string `json:"account_id,omitempty"`
	Resource RouterOSResource `json:"resource,omitempty"`
}

func (x *ListRouterOSResourceRequest) GetPeerId() string {
	if x == nil { return "" }
	return x.PeerId
}
func (x *ListRouterOSResourceRequest) GetAccountId() string {
	if x == nil { return "" }
	return x.AccountId
}
func (x *ListRouterOSResourceRequest) GetResource() RouterOSResource {
	if x == nil { return RouterOSResource(0) }
	return x.Resource
}

type ListRouterOSResourceResponse struct {
	Success bool `json:"success"`
	Error string `json:"error,omitempty"`
	Capability *RouterOSCapability `json:"capability,omitempty"`
	Records []*RouterOSRecord `json:"records,omitempty"`
}

func (x *ListRouterOSResourceResponse) GetSuccess() bool {
	if x == nil { return false }
	return x.Success
}
func (x *ListRouterOSResourceResponse) GetError() string {
	if x == nil { return "" }
	return x.Error
}
func (x *ListRouterOSResourceResponse) GetCapability() *RouterOSCapability {
	if x == nil { return nil }
	return x.Capability
}
func (x *ListRouterOSResourceResponse) GetRecords() []*RouterOSRecord {
	if x == nil { return nil }
	return x.Records
}

type MutateRouterOSResourceRequest struct {
	PeerId string `json:"peer_id,omitempty"`
	AccountId string `json:"account_id,omitempty"`
	Resource RouterOSResource `json:"resource,omitempty"`
	Id string `json:"id,omitempty"`
	Fields map[string]string `json:"fields,omitempty"`
}

func (x *MutateRouterOSResourceRequest) GetPeerId() string {
	if x == nil { return "" }
	return x.PeerId
}
func (x *MutateRouterOSResourceRequest) GetAccountId() string {
	if x == nil { return "" }
	return x.AccountId
}
func (x *MutateRouterOSResourceRequest) GetResource() RouterOSResource {
	if x == nil { return RouterOSResource(0) }
	return x.Resource
}
func (x *MutateRouterOSResourceRequest) GetId() string {
	if x == nil { return "" }
	return x.Id
}
func (x *MutateRouterOSResourceRequest) GetFields() map[string]string {
	if x == nil { return nil }
	return x.Fields
}

type DeleteRouterOSResourceRequest struct {
	PeerId string `json:"peer_id,omitempty"`
	AccountId string `json:"account_id,omitempty"`
	Resource RouterOSResource `json:"resource,omitempty"`
	Id string `json:"id,omitempty"`
}

func (x *DeleteRouterOSResourceRequest) GetPeerId() string {
	if x == nil { return "" }
	return x.PeerId
}
func (x *DeleteRouterOSResourceRequest) GetAccountId() string {
	if x == nil { return "" }
	return x.AccountId
}
func (x *DeleteRouterOSResourceRequest) GetResource() RouterOSResource {
	if x == nil { return RouterOSResource(0) }
	return x.Resource
}
func (x *DeleteRouterOSResourceRequest) GetId() string {
	if x == nil { return "" }
	return x.Id
}

type MutateRouterOSResourceResponse struct {
	Success bool `json:"success"`
	Error string `json:"error,omitempty"`
	Capability *RouterOSCapability `json:"capability,omitempty"`
}

func (x *MutateRouterOSResourceResponse) GetSuccess() bool {
	if x == nil { return false }
	return x.Success
}
func (x *MutateRouterOSResourceResponse) GetError() string {
	if x == nil { return "" }
	return x.Error
}
func (x *MutateRouterOSResourceResponse) GetCapability() *RouterOSCapability {
	if x == nil { return nil }
	return x.Capability
}

type OpenRouterOSDashboardRequest struct {
	PeerId string `json:"peer_id,omitempty"`
	AccountId string `json:"account_id,omitempty"`
	Resource RouterOSResource `json:"resource,omitempty"`
}

func (x *OpenRouterOSDashboardRequest) GetPeerId() string {
	if x == nil { return "" }
	return x.PeerId
}
func (x *OpenRouterOSDashboardRequest) GetAccountId() string {
	if x == nil { return "" }
	return x.AccountId
}
func (x *OpenRouterOSDashboardRequest) GetResource() RouterOSResource {
	if x == nil { return RouterOSResource(0) }
	return x.Resource
}

type LoadRouterOSResourceRequest struct {
	Resource RouterOSResource `json:"resource,omitempty"`
	ForceReload bool `json:"force_reload"`
}

func (x *LoadRouterOSResourceRequest) GetResource() RouterOSResource {
	if x == nil { return RouterOSResource(0) }
	return x.Resource
}
func (x *LoadRouterOSResourceRequest) GetForceReload() bool {
	if x == nil { return false }
	return x.ForceReload
}

type RefreshRouterOSDashboardRequest struct {
	Overview bool `json:"overview"`
	Resources []RouterOSResource `json:"resources,omitempty"`
}

func (x *RefreshRouterOSDashboardRequest) GetOverview() bool {
	if x == nil { return false }
	return x.Overview
}
func (x *RefreshRouterOSDashboardRequest) GetResources() []RouterOSResource {
	if x == nil { return nil }
	return x.Resources
}

type RouterOSDashboardState struct {
	PeerId string `json:"peer_id,omitempty"`
	AccountId string `json:"account_id,omitempty"`
	Success bool `json:"success"`
	Error string `json:"error,omitempty"`
	Connected bool `json:"connected"`
	AccessRequired bool `json:"access_required"`
	Capability *RouterOSCapability `json:"capability,omitempty"`
	Identity *RouterOSIdentity `json:"identity,omitempty"`
	SystemResource map[string]string `json:"system_resource,omitempty"`
	Routerboard map[string]string `json:"routerboard,omitempty"`
}

func (x *RouterOSDashboardState) GetPeerId() string {
	if x == nil { return "" }
	return x.PeerId
}
func (x *RouterOSDashboardState) GetAccountId() string {
	if x == nil { return "" }
	return x.AccountId
}
func (x *RouterOSDashboardState) GetSuccess() bool {
	if x == nil { return false }
	return x.Success
}
func (x *RouterOSDashboardState) GetError() string {
	if x == nil { return "" }
	return x.Error
}
func (x *RouterOSDashboardState) GetConnected() bool {
	if x == nil { return false }
	return x.Connected
}
func (x *RouterOSDashboardState) GetAccessRequired() bool {
	if x == nil { return false }
	return x.AccessRequired
}
func (x *RouterOSDashboardState) GetCapability() *RouterOSCapability {
	if x == nil { return nil }
	return x.Capability
}
func (x *RouterOSDashboardState) GetIdentity() *RouterOSIdentity {
	if x == nil { return nil }
	return x.Identity
}
func (x *RouterOSDashboardState) GetSystemResource() map[string]string {
	if x == nil { return nil }
	return x.SystemResource
}
func (x *RouterOSDashboardState) GetRouterboard() map[string]string {
	if x == nil { return nil }
	return x.Routerboard
}

type RouterOSResourceSnapshot struct {
	Resource RouterOSResource `json:"resource,omitempty"`
	Success bool `json:"success"`
	Error string `json:"error,omitempty"`
	Records []*RouterOSRecord `json:"records,omitempty"`
}

func (x *RouterOSResourceSnapshot) GetResource() RouterOSResource {
	if x == nil { return RouterOSResource(0) }
	return x.Resource
}
func (x *RouterOSResourceSnapshot) GetSuccess() bool {
	if x == nil { return false }
	return x.Success
}
func (x *RouterOSResourceSnapshot) GetError() string {
	if x == nil { return "" }
	return x.Error
}
func (x *RouterOSResourceSnapshot) GetRecords() []*RouterOSRecord {
	if x == nil { return nil }
	return x.Records
}

type RouterOSMutationNotice struct {
	Action string `json:"action,omitempty"`
	Resource RouterOSResource `json:"resource,omitempty"`
	Success bool `json:"success"`
	Error string `json:"error,omitempty"`
	Id string `json:"id,omitempty"`
}

func (x *RouterOSMutationNotice) GetAction() string {
	if x == nil { return "" }
	return x.Action
}
func (x *RouterOSMutationNotice) GetResource() RouterOSResource {
	if x == nil { return RouterOSResource(0) }
	return x.Resource
}
func (x *RouterOSMutationNotice) GetSuccess() bool {
	if x == nil { return false }
	return x.Success
}
func (x *RouterOSMutationNotice) GetError() string {
	if x == nil { return "" }
	return x.Error
}
func (x *RouterOSMutationNotice) GetId() string {
	if x == nil { return "" }
	return x.Id
}

type StreamRouterOSDashboardRequest struct {
	Payload isStreamRouterOSDashboardRequest_Payload `json:"-"`
}

type isStreamRouterOSDashboardRequest_Payload interface { isStreamRouterOSDashboardRequest_Payload() }
func (x *StreamRouterOSDashboardRequest) GetPayload() isStreamRouterOSDashboardRequest_Payload {
	if x == nil { return nil }
	return x.Payload
}
type StreamRouterOSDashboardRequest_Open struct { Open *OpenRouterOSDashboardRequest `json:"open,omitempty"` }
func (*StreamRouterOSDashboardRequest_Open) isStreamRouterOSDashboardRequest_Payload() {}
func (x *StreamRouterOSDashboardRequest) GetOpen() *OpenRouterOSDashboardRequest {
	if v, ok := x.GetPayload().(*StreamRouterOSDashboardRequest_Open); ok { return v.Open }
	return nil
}
type StreamRouterOSDashboardRequest_LoadResource struct { LoadResource *LoadRouterOSResourceRequest `json:"load_resource,omitempty"` }
func (*StreamRouterOSDashboardRequest_LoadResource) isStreamRouterOSDashboardRequest_Payload() {}
func (x *StreamRouterOSDashboardRequest) GetLoadResource() *LoadRouterOSResourceRequest {
	if v, ok := x.GetPayload().(*StreamRouterOSDashboardRequest_LoadResource); ok { return v.LoadResource }
	return nil
}
type StreamRouterOSDashboardRequest_Refresh struct { Refresh *RefreshRouterOSDashboardRequest `json:"refresh,omitempty"` }
func (*StreamRouterOSDashboardRequest_Refresh) isStreamRouterOSDashboardRequest_Payload() {}
func (x *StreamRouterOSDashboardRequest) GetRefresh() *RefreshRouterOSDashboardRequest {
	if v, ok := x.GetPayload().(*StreamRouterOSDashboardRequest_Refresh); ok { return v.Refresh }
	return nil
}
type StreamRouterOSDashboardRequest_ConfigureAccess struct { ConfigureAccess *ConfigureRouterOSAccessRequest `json:"configure_access,omitempty"` }
func (*StreamRouterOSDashboardRequest_ConfigureAccess) isStreamRouterOSDashboardRequest_Payload() {}
func (x *StreamRouterOSDashboardRequest) GetConfigureAccess() *ConfigureRouterOSAccessRequest {
	if v, ok := x.GetPayload().(*StreamRouterOSDashboardRequest_ConfigureAccess); ok { return v.ConfigureAccess }
	return nil
}
type StreamRouterOSDashboardRequest_AddResource struct { AddResource *MutateRouterOSResourceRequest `json:"add_resource,omitempty"` }
func (*StreamRouterOSDashboardRequest_AddResource) isStreamRouterOSDashboardRequest_Payload() {}
func (x *StreamRouterOSDashboardRequest) GetAddResource() *MutateRouterOSResourceRequest {
	if v, ok := x.GetPayload().(*StreamRouterOSDashboardRequest_AddResource); ok { return v.AddResource }
	return nil
}
type StreamRouterOSDashboardRequest_UpdateResource struct { UpdateResource *MutateRouterOSResourceRequest `json:"update_resource,omitempty"` }
func (*StreamRouterOSDashboardRequest_UpdateResource) isStreamRouterOSDashboardRequest_Payload() {}
func (x *StreamRouterOSDashboardRequest) GetUpdateResource() *MutateRouterOSResourceRequest {
	if v, ok := x.GetPayload().(*StreamRouterOSDashboardRequest_UpdateResource); ok { return v.UpdateResource }
	return nil
}
type StreamRouterOSDashboardRequest_DeleteResource struct { DeleteResource *DeleteRouterOSResourceRequest `json:"delete_resource,omitempty"` }
func (*StreamRouterOSDashboardRequest_DeleteResource) isStreamRouterOSDashboardRequest_Payload() {}
func (x *StreamRouterOSDashboardRequest) GetDeleteResource() *DeleteRouterOSResourceRequest {
	if v, ok := x.GetPayload().(*StreamRouterOSDashboardRequest_DeleteResource); ok { return v.DeleteResource }
	return nil
}

type StreamRouterOSDashboardEvent struct {
	Payload isStreamRouterOSDashboardEvent_Payload `json:"-"`
}

type isStreamRouterOSDashboardEvent_Payload interface { isStreamRouterOSDashboardEvent_Payload() }
func (x *StreamRouterOSDashboardEvent) GetPayload() isStreamRouterOSDashboardEvent_Payload {
	if x == nil { return nil }
	return x.Payload
}
type StreamRouterOSDashboardEvent_State struct { State *RouterOSDashboardState `json:"state,omitempty"` }
func (*StreamRouterOSDashboardEvent_State) isStreamRouterOSDashboardEvent_Payload() {}
func (x *StreamRouterOSDashboardEvent) GetState() *RouterOSDashboardState {
	if v, ok := x.GetPayload().(*StreamRouterOSDashboardEvent_State); ok { return v.State }
	return nil
}
type StreamRouterOSDashboardEvent_Resource struct { Resource *RouterOSResourceSnapshot `json:"resource,omitempty"` }
func (*StreamRouterOSDashboardEvent_Resource) isStreamRouterOSDashboardEvent_Payload() {}
func (x *StreamRouterOSDashboardEvent) GetResource() *RouterOSResourceSnapshot {
	if v, ok := x.GetPayload().(*StreamRouterOSDashboardEvent_Resource); ok { return v.Resource }
	return nil
}
type StreamRouterOSDashboardEvent_Notice struct { Notice *RouterOSMutationNotice `json:"notice,omitempty"` }
func (*StreamRouterOSDashboardEvent_Notice) isStreamRouterOSDashboardEvent_Payload() {}
func (x *StreamRouterOSDashboardEvent) GetNotice() *RouterOSMutationNotice {
	if v, ok := x.GetPayload().(*StreamRouterOSDashboardEvent_Notice); ok { return v.Notice }
	return nil
}

type GenerateBackupTokenRequest struct {
	PeerId string `json:"peer_id,omitempty"`
	AccountId string `json:"account_id,omitempty"`
}

func (x *GenerateBackupTokenRequest) GetPeerId() string {
	if x == nil { return "" }
	return x.PeerId
}
func (x *GenerateBackupTokenRequest) GetAccountId() string {
	if x == nil { return "" }
	return x.AccountId
}

type GenerateBackupTokenResponse struct {
	Success bool `json:"success"`
	Error string `json:"error,omitempty"`
	UploadToken string `json:"upload_token,omitempty"`
	UploadUrl string `json:"upload_url,omitempty"`
}

func (x *GenerateBackupTokenResponse) GetSuccess() bool {
	if x == nil { return false }
	return x.Success
}
func (x *GenerateBackupTokenResponse) GetError() string {
	if x == nil { return "" }
	return x.Error
}
func (x *GenerateBackupTokenResponse) GetUploadToken() string {
	if x == nil { return "" }
	return x.UploadToken
}
func (x *GenerateBackupTokenResponse) GetUploadUrl() string {
	if x == nil { return "" }
	return x.UploadUrl
}

type WUSPDeviceState struct {
	Id string `json:"id,omitempty"`
	PeerId string `json:"peer_id,omitempty"`
	AccountId string `json:"account_id,omitempty"`
	LastSyncAt int64 `json:"last_sync_at,omitempty"`
	SyncError string `json:"sync_error,omitempty"`
	DeviceSnapshot []byte `json:"device_snapshot,omitempty"`
	DeviceId string `json:"device_id,omitempty"`
	Manufacturer string `json:"manufacturer,omitempty"`
	ProductClass string `json:"product_class,omitempty"`
	SerialNumber string `json:"serial_number,omitempty"`
	SoftwareVersion string `json:"software_version,omitempty"`
	HardwareVersion string `json:"hardware_version,omitempty"`
	WuspEnable bool `json:"wusp_enable"`
	WuspStatus string `json:"wusp_status,omitempty"`
	WuspVersion string `json:"wusp_version,omitempty"`
	CreatedAt int64 `json:"created_at,omitempty"`
	UpdatedAt int64 `json:"updated_at,omitempty"`
}

func (x *WUSPDeviceState) GetId() string {
	if x == nil { return "" }
	return x.Id
}
func (x *WUSPDeviceState) GetPeerId() string {
	if x == nil { return "" }
	return x.PeerId
}
func (x *WUSPDeviceState) GetAccountId() string {
	if x == nil { return "" }
	return x.AccountId
}
func (x *WUSPDeviceState) GetLastSyncAt() int64 {
	if x == nil { return 0 }
	return x.LastSyncAt
}
func (x *WUSPDeviceState) GetSyncError() string {
	if x == nil { return "" }
	return x.SyncError
}
func (x *WUSPDeviceState) GetDeviceSnapshot() []byte {
	if x == nil { return nil }
	return x.DeviceSnapshot
}
func (x *WUSPDeviceState) GetDeviceId() string {
	if x == nil { return "" }
	return x.DeviceId
}
func (x *WUSPDeviceState) GetManufacturer() string {
	if x == nil { return "" }
	return x.Manufacturer
}
func (x *WUSPDeviceState) GetProductClass() string {
	if x == nil { return "" }
	return x.ProductClass
}
func (x *WUSPDeviceState) GetSerialNumber() string {
	if x == nil { return "" }
	return x.SerialNumber
}
func (x *WUSPDeviceState) GetSoftwareVersion() string {
	if x == nil { return "" }
	return x.SoftwareVersion
}
func (x *WUSPDeviceState) GetHardwareVersion() string {
	if x == nil { return "" }
	return x.HardwareVersion
}
func (x *WUSPDeviceState) GetWuspEnable() bool {
	if x == nil { return false }
	return x.WuspEnable
}
func (x *WUSPDeviceState) GetWuspStatus() string {
	if x == nil { return "" }
	return x.WuspStatus
}
func (x *WUSPDeviceState) GetWuspVersion() string {
	if x == nil { return "" }
	return x.WuspVersion
}
func (x *WUSPDeviceState) GetCreatedAt() int64 {
	if x == nil { return 0 }
	return x.CreatedAt
}
func (x *WUSPDeviceState) GetUpdatedAt() int64 {
	if x == nil { return 0 }
	return x.UpdatedAt
}

type GetWUSPDeviceStateRequest struct {
	PeerId string `json:"peer_id,omitempty"`
	AccountId string `json:"account_id,omitempty"`
}

func (x *GetWUSPDeviceStateRequest) GetPeerId() string {
	if x == nil { return "" }
	return x.PeerId
}
func (x *GetWUSPDeviceStateRequest) GetAccountId() string {
	if x == nil { return "" }
	return x.AccountId
}

type GetWUSPDeviceStateResponse struct {
	State *WUSPDeviceState `json:"state,omitempty"`
}

func (x *GetWUSPDeviceStateResponse) GetState() *WUSPDeviceState {
	if x == nil { return nil }
	return x.State
}

type ListWUSPDeviceStatesRequest struct {
	AccountId string `json:"account_id,omitempty"`
}

func (x *ListWUSPDeviceStatesRequest) GetAccountId() string {
	if x == nil { return "" }
	return x.AccountId
}

type ListWUSPDeviceStatesResponse struct {
	States []*WUSPDeviceState `json:"states,omitempty"`
}

func (x *ListWUSPDeviceStatesResponse) GetStates() []*WUSPDeviceState {
	if x == nil { return nil }
	return x.States
}

type SyncWUSPDeviceStateRequest struct {
	PeerId string `json:"peer_id,omitempty"`
	AccountId string `json:"account_id,omitempty"`
}

func (x *SyncWUSPDeviceStateRequest) GetPeerId() string {
	if x == nil { return "" }
	return x.PeerId
}
func (x *SyncWUSPDeviceStateRequest) GetAccountId() string {
	if x == nil { return "" }
	return x.AccountId
}

type SyncWUSPDeviceStateResponse struct {
	Success bool `json:"success"`
	Error string `json:"error,omitempty"`
	State *WUSPDeviceState `json:"state,omitempty"`
}

func (x *SyncWUSPDeviceStateResponse) GetSuccess() bool {
	if x == nil { return false }
	return x.Success
}
func (x *SyncWUSPDeviceStateResponse) GetError() string {
	if x == nil { return "" }
	return x.Error
}
func (x *SyncWUSPDeviceStateResponse) GetState() *WUSPDeviceState {
	if x == nil { return nil }
	return x.State
}

type WUSPParam struct {
	Path string `json:"path,omitempty"`
	Value string `json:"value,omitempty"`
}

func (x *WUSPParam) GetPath() string {
	if x == nil { return "" }
	return x.Path
}
func (x *WUSPParam) GetValue() string {
	if x == nil { return "" }
	return x.Value
}

type WUSPGetRequest struct {
	PeerId string `json:"peer_id,omitempty"`
	AccountId string `json:"account_id,omitempty"`
	Paths []string `json:"paths,omitempty"`
}

func (x *WUSPGetRequest) GetPeerId() string {
	if x == nil { return "" }
	return x.PeerId
}
func (x *WUSPGetRequest) GetAccountId() string {
	if x == nil { return "" }
	return x.AccountId
}
func (x *WUSPGetRequest) GetPaths() []string {
	if x == nil { return nil }
	return x.Paths
}

type WUSPGetResponse struct {
	Success bool `json:"success"`
	Error string `json:"error,omitempty"`
	Params []*WUSPParam `json:"params,omitempty"`
}

func (x *WUSPGetResponse) GetSuccess() bool {
	if x == nil { return false }
	return x.Success
}
func (x *WUSPGetResponse) GetError() string {
	if x == nil { return "" }
	return x.Error
}
func (x *WUSPGetResponse) GetParams() []*WUSPParam {
	if x == nil { return nil }
	return x.Params
}

type WUSPSetRequest struct {
	PeerId string `json:"peer_id,omitempty"`
	AccountId string `json:"account_id,omitempty"`
	Params []*WUSPParam `json:"params,omitempty"`
}

func (x *WUSPSetRequest) GetPeerId() string {
	if x == nil { return "" }
	return x.PeerId
}
func (x *WUSPSetRequest) GetAccountId() string {
	if x == nil { return "" }
	return x.AccountId
}
func (x *WUSPSetRequest) GetParams() []*WUSPParam {
	if x == nil { return nil }
	return x.Params
}

type WUSPSetResponse struct {
	Success bool `json:"success"`
	Error string `json:"error,omitempty"`
}

func (x *WUSPSetResponse) GetSuccess() bool {
	if x == nil { return false }
	return x.Success
}
func (x *WUSPSetResponse) GetError() string {
	if x == nil { return "" }
	return x.Error
}

type WUSPOperateRequest struct {
	PeerId string `json:"peer_id,omitempty"`
	AccountId string `json:"account_id,omitempty"`
	CommandPath string `json:"command_path,omitempty"`
	InputParams []*WUSPParam `json:"input_params,omitempty"`
}

func (x *WUSPOperateRequest) GetPeerId() string {
	if x == nil { return "" }
	return x.PeerId
}
func (x *WUSPOperateRequest) GetAccountId() string {
	if x == nil { return "" }
	return x.AccountId
}
func (x *WUSPOperateRequest) GetCommandPath() string {
	if x == nil { return "" }
	return x.CommandPath
}
func (x *WUSPOperateRequest) GetInputParams() []*WUSPParam {
	if x == nil { return nil }
	return x.InputParams
}

type WUSPOperateResponse struct {
	Success bool `json:"success"`
	Error string `json:"error,omitempty"`
	OutputParams []*WUSPParam `json:"output_params,omitempty"`
}

func (x *WUSPOperateResponse) GetSuccess() bool {
	if x == nil { return false }
	return x.Success
}
func (x *WUSPOperateResponse) GetError() string {
	if x == nil { return "" }
	return x.Error
}
func (x *WUSPOperateResponse) GetOutputParams() []*WUSPParam {
	if x == nil { return nil }
	return x.OutputParams
}

type WUSPAddRequest struct {
	PeerId string `json:"peer_id,omitempty"`
	AccountId string `json:"account_id,omitempty"`
	ObjectPath string `json:"object_path,omitempty"`
	Params []*WUSPParam `json:"params,omitempty"`
}

func (x *WUSPAddRequest) GetPeerId() string {
	if x == nil { return "" }
	return x.PeerId
}
func (x *WUSPAddRequest) GetAccountId() string {
	if x == nil { return "" }
	return x.AccountId
}
func (x *WUSPAddRequest) GetObjectPath() string {
	if x == nil { return "" }
	return x.ObjectPath
}
func (x *WUSPAddRequest) GetParams() []*WUSPParam {
	if x == nil { return nil }
	return x.Params
}

type WUSPAddResponse struct {
	Success bool `json:"success"`
	Error string `json:"error,omitempty"`
	InstancePath string `json:"instance_path,omitempty"`
	CreatedPaths []string `json:"created_paths,omitempty"`
}

func (x *WUSPAddResponse) GetSuccess() bool {
	if x == nil { return false }
	return x.Success
}
func (x *WUSPAddResponse) GetError() string {
	if x == nil { return "" }
	return x.Error
}
func (x *WUSPAddResponse) GetInstancePath() string {
	if x == nil { return "" }
	return x.InstancePath
}
func (x *WUSPAddResponse) GetCreatedPaths() []string {
	if x == nil { return nil }
	return x.CreatedPaths
}

type WUSPDeleteRequest struct {
	PeerId string `json:"peer_id,omitempty"`
	AccountId string `json:"account_id,omitempty"`
	Paths []string `json:"paths,omitempty"`
}

func (x *WUSPDeleteRequest) GetPeerId() string {
	if x == nil { return "" }
	return x.PeerId
}
func (x *WUSPDeleteRequest) GetAccountId() string {
	if x == nil { return "" }
	return x.AccountId
}
func (x *WUSPDeleteRequest) GetPaths() []string {
	if x == nil { return nil }
	return x.Paths
}

type WUSPDeleteResponse struct {
	Success bool `json:"success"`
	Error string `json:"error,omitempty"`
}

func (x *WUSPDeleteResponse) GetSuccess() bool {
	if x == nil { return false }
	return x.Success
}
func (x *WUSPDeleteResponse) GetError() string {
	if x == nil { return "" }
	return x.Error
}

type WUSPSupportedProtocolRequest struct {
	PeerId string `json:"peer_id,omitempty"`
	AccountId string `json:"account_id,omitempty"`
}

func (x *WUSPSupportedProtocolRequest) GetPeerId() string {
	if x == nil { return "" }
	return x.PeerId
}
func (x *WUSPSupportedProtocolRequest) GetAccountId() string {
	if x == nil { return "" }
	return x.AccountId
}

type WUSPProtocolInfo struct {
	Name string `json:"name,omitempty"`
	Version uint32 `json:"version,omitempty"`
	Methods []uint32 `json:"methods,omitempty"`
	Compression []string `json:"compression,omitempty"`
	ControlTransport string `json:"control_transport,omitempty"`
	TransferTransport string `json:"transfer_transport,omitempty"`
	MaxControlPayload uint32 `json:"max_control_payload,omitempty"`
	RecommendedChunkSize uint32 `json:"recommended_chunk_size,omitempty"`
	TunnelOnly bool `json:"tunnel_only"`
	ReliableTransfer bool `json:"reliable_transfer"`
}

func (x *WUSPProtocolInfo) GetName() string {
	if x == nil { return "" }
	return x.Name
}
func (x *WUSPProtocolInfo) GetVersion() uint32 {
	if x == nil { return 0 }
	return x.Version
}
func (x *WUSPProtocolInfo) GetMethods() []uint32 {
	if x == nil { return nil }
	return x.Methods
}
func (x *WUSPProtocolInfo) GetCompression() []string {
	if x == nil { return nil }
	return x.Compression
}
func (x *WUSPProtocolInfo) GetControlTransport() string {
	if x == nil { return "" }
	return x.ControlTransport
}
func (x *WUSPProtocolInfo) GetTransferTransport() string {
	if x == nil { return "" }
	return x.TransferTransport
}
func (x *WUSPProtocolInfo) GetMaxControlPayload() uint32 {
	if x == nil { return 0 }
	return x.MaxControlPayload
}
func (x *WUSPProtocolInfo) GetRecommendedChunkSize() uint32 {
	if x == nil { return 0 }
	return x.RecommendedChunkSize
}
func (x *WUSPProtocolInfo) GetTunnelOnly() bool {
	if x == nil { return false }
	return x.TunnelOnly
}
func (x *WUSPProtocolInfo) GetReliableTransfer() bool {
	if x == nil { return false }
	return x.ReliableTransfer
}

type WUSPSupportedProtocolResponse struct {
	Success bool `json:"success"`
	Error string `json:"error,omitempty"`
	Protocol *WUSPProtocolInfo `json:"protocol,omitempty"`
}

func (x *WUSPSupportedProtocolResponse) GetSuccess() bool {
	if x == nil { return false }
	return x.Success
}
func (x *WUSPSupportedProtocolResponse) GetError() string {
	if x == nil { return "" }
	return x.Error
}
func (x *WUSPSupportedProtocolResponse) GetProtocol() *WUSPProtocolInfo {
	if x == nil { return nil }
	return x.Protocol
}

type WUSPSupportedDMRequest struct {
	PeerId string `json:"peer_id,omitempty"`
	AccountId string `json:"account_id,omitempty"`
	PathFilters []string `json:"path_filters,omitempty"`
}

func (x *WUSPSupportedDMRequest) GetPeerId() string {
	if x == nil { return "" }
	return x.PeerId
}
func (x *WUSPSupportedDMRequest) GetAccountId() string {
	if x == nil { return "" }
	return x.AccountId
}
func (x *WUSPSupportedDMRequest) GetPathFilters() []string {
	if x == nil { return nil }
	return x.PathFilters
}

type WUSPSupportedDMEntry struct {
	Path string `json:"path,omitempty"`
	Access string `json:"access,omitempty"`
	Type string `json:"type,omitempty"`
	IsObject bool `json:"is_object"`
}

func (x *WUSPSupportedDMEntry) GetPath() string {
	if x == nil { return "" }
	return x.Path
}
func (x *WUSPSupportedDMEntry) GetAccess() string {
	if x == nil { return "" }
	return x.Access
}
func (x *WUSPSupportedDMEntry) GetType() string {
	if x == nil { return "" }
	return x.Type
}
func (x *WUSPSupportedDMEntry) GetIsObject() bool {
	if x == nil { return false }
	return x.IsObject
}

type WUSPSupportedDMResponse struct {
	Success bool `json:"success"`
	Error string `json:"error,omitempty"`
	Entries []*WUSPSupportedDMEntry `json:"entries,omitempty"`
}

func (x *WUSPSupportedDMResponse) GetSuccess() bool {
	if x == nil { return false }
	return x.Success
}
func (x *WUSPSupportedDMResponse) GetError() string {
	if x == nil { return "" }
	return x.Error
}
func (x *WUSPSupportedDMResponse) GetEntries() []*WUSPSupportedDMEntry {
	if x == nil { return nil }
	return x.Entries
}

type DeviceSnapshot struct {
	Id string `json:"id,omitempty"`
	AccountId string `json:"account_id,omitempty"`
	Name string `json:"name,omitempty"`
	Protocol string `json:"protocol,omitempty"`
	Manufacturer string `json:"manufacturer,omitempty"`
	ProductClass string `json:"product_class,omitempty"`
	SerialNumber string `json:"serial_number,omitempty"`
	SoftwareVersion string `json:"software_version,omitempty"`
	HardwareVersion string `json:"hardware_version,omitempty"`
	DeviceSnapshot []byte `json:"device_snapshot,omitempty"`
	CreatedAt int64 `json:"created_at,omitempty"`
	UpdatedAt int64 `json:"updated_at,omitempty"`
	BackupName string `json:"backup_name,omitempty"`
	BackupSize int32 `json:"backup_size,omitempty"`
	HasBackup bool `json:"has_backup"`
}

func (x *DeviceSnapshot) GetId() string {
	if x == nil { return "" }
	return x.Id
}
func (x *DeviceSnapshot) GetAccountId() string {
	if x == nil { return "" }
	return x.AccountId
}
func (x *DeviceSnapshot) GetName() string {
	if x == nil { return "" }
	return x.Name
}
func (x *DeviceSnapshot) GetProtocol() string {
	if x == nil { return "" }
	return x.Protocol
}
func (x *DeviceSnapshot) GetManufacturer() string {
	if x == nil { return "" }
	return x.Manufacturer
}
func (x *DeviceSnapshot) GetProductClass() string {
	if x == nil { return "" }
	return x.ProductClass
}
func (x *DeviceSnapshot) GetSerialNumber() string {
	if x == nil { return "" }
	return x.SerialNumber
}
func (x *DeviceSnapshot) GetSoftwareVersion() string {
	if x == nil { return "" }
	return x.SoftwareVersion
}
func (x *DeviceSnapshot) GetHardwareVersion() string {
	if x == nil { return "" }
	return x.HardwareVersion
}
func (x *DeviceSnapshot) GetDeviceSnapshot() []byte {
	if x == nil { return nil }
	return x.DeviceSnapshot
}
func (x *DeviceSnapshot) GetCreatedAt() int64 {
	if x == nil { return 0 }
	return x.CreatedAt
}
func (x *DeviceSnapshot) GetUpdatedAt() int64 {
	if x == nil { return 0 }
	return x.UpdatedAt
}
func (x *DeviceSnapshot) GetBackupName() string {
	if x == nil { return "" }
	return x.BackupName
}
func (x *DeviceSnapshot) GetBackupSize() int32 {
	if x == nil { return 0 }
	return x.BackupSize
}
func (x *DeviceSnapshot) GetHasBackup() bool {
	if x == nil { return false }
	return x.HasBackup
}

type CreateDeviceSnapshotRequest struct {
	AccountId string `json:"account_id,omitempty"`
	PeerId string `json:"peer_id,omitempty"`
	Name string `json:"name,omitempty"`
	Protocol string `json:"protocol,omitempty"`
}

func (x *CreateDeviceSnapshotRequest) GetAccountId() string {
	if x == nil { return "" }
	return x.AccountId
}
func (x *CreateDeviceSnapshotRequest) GetPeerId() string {
	if x == nil { return "" }
	return x.PeerId
}
func (x *CreateDeviceSnapshotRequest) GetName() string {
	if x == nil { return "" }
	return x.Name
}
func (x *CreateDeviceSnapshotRequest) GetProtocol() string {
	if x == nil { return "" }
	return x.Protocol
}

type CreateDeviceSnapshotResponse struct {
	Success bool `json:"success"`
	Error string `json:"error,omitempty"`
	Snapshot *DeviceSnapshot `json:"snapshot,omitempty"`
}

func (x *CreateDeviceSnapshotResponse) GetSuccess() bool {
	if x == nil { return false }
	return x.Success
}
func (x *CreateDeviceSnapshotResponse) GetError() string {
	if x == nil { return "" }
	return x.Error
}
func (x *CreateDeviceSnapshotResponse) GetSnapshot() *DeviceSnapshot {
	if x == nil { return nil }
	return x.Snapshot
}

type ListDeviceSnapshotsRequest struct {
	AccountId string `json:"account_id,omitempty"`
	Protocol string `json:"protocol,omitempty"`
}

func (x *ListDeviceSnapshotsRequest) GetAccountId() string {
	if x == nil { return "" }
	return x.AccountId
}
func (x *ListDeviceSnapshotsRequest) GetProtocol() string {
	if x == nil { return "" }
	return x.Protocol
}

type ListDeviceSnapshotsResponse struct {
	Snapshots []*DeviceSnapshot `json:"snapshots,omitempty"`
}

func (x *ListDeviceSnapshotsResponse) GetSnapshots() []*DeviceSnapshot {
	if x == nil { return nil }
	return x.Snapshots
}

type GetDeviceSnapshotRequest struct {
	Id string `json:"id,omitempty"`
	AccountId string `json:"account_id,omitempty"`
}

func (x *GetDeviceSnapshotRequest) GetId() string {
	if x == nil { return "" }
	return x.Id
}
func (x *GetDeviceSnapshotRequest) GetAccountId() string {
	if x == nil { return "" }
	return x.AccountId
}

type GetDeviceSnapshotResponse struct {
	Snapshot *DeviceSnapshot `json:"snapshot,omitempty"`
}

func (x *GetDeviceSnapshotResponse) GetSnapshot() *DeviceSnapshot {
	if x == nil { return nil }
	return x.Snapshot
}

type UpdateDeviceSnapshotRequest struct {
	AccountId string `json:"account_id,omitempty"`
	Id string `json:"id,omitempty"`
	Name string `json:"name,omitempty"`
	PeerId string `json:"peer_id,omitempty"`
}

func (x *UpdateDeviceSnapshotRequest) GetAccountId() string {
	if x == nil { return "" }
	return x.AccountId
}
func (x *UpdateDeviceSnapshotRequest) GetId() string {
	if x == nil { return "" }
	return x.Id
}
func (x *UpdateDeviceSnapshotRequest) GetName() string {
	if x == nil { return "" }
	return x.Name
}
func (x *UpdateDeviceSnapshotRequest) GetPeerId() string {
	if x == nil { return "" }
	return x.PeerId
}

type UpdateDeviceSnapshotResponse struct {
	Success bool `json:"success"`
	Error string `json:"error,omitempty"`
	Snapshot *DeviceSnapshot `json:"snapshot,omitempty"`
}

func (x *UpdateDeviceSnapshotResponse) GetSuccess() bool {
	if x == nil { return false }
	return x.Success
}
func (x *UpdateDeviceSnapshotResponse) GetError() string {
	if x == nil { return "" }
	return x.Error
}
func (x *UpdateDeviceSnapshotResponse) GetSnapshot() *DeviceSnapshot {
	if x == nil { return nil }
	return x.Snapshot
}

type DeleteDeviceSnapshotRequest struct {
	Id string `json:"id,omitempty"`
	AccountId string `json:"account_id,omitempty"`
}

func (x *DeleteDeviceSnapshotRequest) GetId() string {
	if x == nil { return "" }
	return x.Id
}
func (x *DeleteDeviceSnapshotRequest) GetAccountId() string {
	if x == nil { return "" }
	return x.AccountId
}

type DeleteDeviceSnapshotResponse struct {
	Success bool `json:"success"`
	Error string `json:"error,omitempty"`
}

func (x *DeleteDeviceSnapshotResponse) GetSuccess() bool {
	if x == nil { return false }
	return x.Success
}
func (x *DeleteDeviceSnapshotResponse) GetError() string {
	if x == nil { return "" }
	return x.Error
}

type ProvisionDeviceRequest struct {
	AccountId string `json:"account_id,omitempty"`
	PeerId string `json:"peer_id,omitempty"`
	SnapshotId string `json:"snapshot_id,omitempty"`
}

func (x *ProvisionDeviceRequest) GetAccountId() string {
	if x == nil { return "" }
	return x.AccountId
}
func (x *ProvisionDeviceRequest) GetPeerId() string {
	if x == nil { return "" }
	return x.PeerId
}
func (x *ProvisionDeviceRequest) GetSnapshotId() string {
	if x == nil { return "" }
	return x.SnapshotId
}

type ProvisionDeviceResponse struct {
	Success bool `json:"success"`
	Error string `json:"error,omitempty"`
}

func (x *ProvisionDeviceResponse) GetSuccess() bool {
	if x == nil { return false }
	return x.Success
}
func (x *ProvisionDeviceResponse) GetError() string {
	if x == nil { return "" }
	return x.Error
}

type GetSnapshotBackupRequest struct {
	SnapshotId string `json:"snapshot_id,omitempty"`
	AccountId string `json:"account_id,omitempty"`
}

func (x *GetSnapshotBackupRequest) GetSnapshotId() string {
	if x == nil { return "" }
	return x.SnapshotId
}
func (x *GetSnapshotBackupRequest) GetAccountId() string {
	if x == nil { return "" }
	return x.AccountId
}

type GetSnapshotBackupResponse struct {
	BackupFile []byte `json:"backup_file,omitempty"`
	BackupName string `json:"backup_name,omitempty"`
	BackupSize int32 `json:"backup_size,omitempty"`
}

func (x *GetSnapshotBackupResponse) GetBackupFile() []byte {
	if x == nil { return nil }
	return x.BackupFile
}
func (x *GetSnapshotBackupResponse) GetBackupName() string {
	if x == nil { return "" }
	return x.BackupName
}
func (x *GetSnapshotBackupResponse) GetBackupSize() int32 {
	if x == nil { return 0 }
	return x.BackupSize
}

type UploadSnapshotBackupRequest struct {
	UploadToken string `json:"upload_token,omitempty"`
	BackupFile []byte `json:"backup_file,omitempty"`
	BackupName string `json:"backup_name,omitempty"`
}

func (x *UploadSnapshotBackupRequest) GetUploadToken() string {
	if x == nil { return "" }
	return x.UploadToken
}
func (x *UploadSnapshotBackupRequest) GetBackupFile() []byte {
	if x == nil { return nil }
	return x.BackupFile
}
func (x *UploadSnapshotBackupRequest) GetBackupName() string {
	if x == nil { return "" }
	return x.BackupName
}

type UploadSnapshotBackupResponse struct {
	Success bool `json:"success"`
	Error string `json:"error,omitempty"`
	NewToken string `json:"new_token,omitempty"`
}

func (x *UploadSnapshotBackupResponse) GetSuccess() bool {
	if x == nil { return false }
	return x.Success
}
func (x *UploadSnapshotBackupResponse) GetError() string {
	if x == nil { return "" }
	return x.Error
}
func (x *UploadSnapshotBackupResponse) GetNewToken() string {
	if x == nil { return "" }
	return x.NewToken
}

type GenerateUploadTokenRequest struct {
	SnapshotId string `json:"snapshot_id,omitempty"`
	AccountId string `json:"account_id,omitempty"`
}

func (x *GenerateUploadTokenRequest) GetSnapshotId() string {
	if x == nil { return "" }
	return x.SnapshotId
}
func (x *GenerateUploadTokenRequest) GetAccountId() string {
	if x == nil { return "" }
	return x.AccountId
}

type GenerateUploadTokenResponse struct {
	Success bool `json:"success"`
	Error string `json:"error,omitempty"`
	UploadToken string `json:"upload_token,omitempty"`
	UploadUrl string `json:"upload_url,omitempty"`
}

func (x *GenerateUploadTokenResponse) GetSuccess() bool {
	if x == nil { return false }
	return x.Success
}
func (x *GenerateUploadTokenResponse) GetError() string {
	if x == nil { return "" }
	return x.Error
}
func (x *GenerateUploadTokenResponse) GetUploadToken() string {
	if x == nil { return "" }
	return x.UploadToken
}
func (x *GenerateUploadTokenResponse) GetUploadUrl() string {
	if x == nil { return "" }
	return x.UploadUrl
}
