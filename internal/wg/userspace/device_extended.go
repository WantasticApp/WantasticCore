package userspace

import "fmt"

// GetAllPeersStatus retrieves status for ALL peers in a single pass.
// OPTIMIZED for background monitoring: parses IPC output once for all peers.
func (td *TenantDevice) GetAllPeersStatus() (map[string]*PeerStatus, error) {
	snapshot, err := td.getIPCSnapshotCached()
	if err != nil {
		return nil, fmt.Errorf("failed to get device status: %w", err)
	}

	return copyPeerStatusMap(snapshot.peers), nil
}

// GetAllPeersStatusFresh retrieves status for all peers using a fresh IPC read.
// This is intended for fast presence/routing decisions where stale cached state
// can cause online peers to flap offline in the control plane.
func (td *TenantDevice) GetAllPeersStatusFresh() (map[string]*PeerStatus, error) {
	snapshot, err := td.getIPCSnapshotFresh()
	if err != nil {
		return nil, fmt.Errorf("failed to get fresh device status: %w", err)
	}

	return copyPeerStatusMap(snapshot.peers), nil
}
