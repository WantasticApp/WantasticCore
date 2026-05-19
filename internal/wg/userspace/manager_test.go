package userspace

import "testing"

func TestUpdateTenantMaxPeersUpdatesLiveDeviceLimit(t *testing.T) {
	device := &TenantDevice{}
	device.SetMaxPeers(3)

	manager := &UserspaceManager{
		devices: map[string]*TenantDevice{
			"tenant-1": device,
		},
	}

	if err := manager.UpdateTenantMaxPeers("tenant-1", 25); err != nil {
		t.Fatalf("update tenant max peers: %v", err)
	}

	if got := device.GetMaxPeers(); got != 25 {
		t.Fatalf("expected device max peers 25, got %d", got)
	}
}

func TestUpdateTenantMaxPeersMissingTenant(t *testing.T) {
	manager := &UserspaceManager{devices: map[string]*TenantDevice{}}

	if err := manager.UpdateTenantMaxPeers("missing", 25); err == nil {
		t.Fatal("expected missing tenant error")
	}
}
