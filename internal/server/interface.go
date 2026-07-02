package server

import (
	"WantasticCore/internal/account"
)

// OverlayServer defines the subset of Server methods used for tenant management.
type OverlayServer interface {
	CreateAccount(name string, maxPeers int) (*account.Account, error)
	GetAccount(accountID string) (*account.Account, error)
	SetAccountMaxPeers(accountID string, maxPeers int) (*account.Account, error)
	DeleteAccount(accountID string) error
	AddBlockToAccount(accountID string) (*account.Account, error)

	// Peer management
	AddPeer(accountID, peerName string, assignedIP string) (*PeerInfo, error)
	AddPeerWithKey(accountID, peerName, assignedIP, publicKey string) (*PeerInfo, error)
	GetPeer(accountID, peerID string) (*PeerMetadata, error)
	FindPeer(peerID string) (*PeerMetadata, error)
	UpdatePeer(peer *PeerMetadata) error
	GetNextAvailablePeerIP(accountID string) (string, error)
	ListPeers(accountID string) ([]*PeerMetadata, error)
	GetPeerConfig(accountID, peerID, endpoint string) (string, error)

	// Server info
	GetServerPublicKey(accountID string) string
	GetServerEndpoint() string
}

// Ensure Server implements OverlayServer
var _ OverlayServer = (*Server)(nil)
