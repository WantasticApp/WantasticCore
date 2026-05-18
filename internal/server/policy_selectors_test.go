package server

import "testing"

func TestResolvePolicyPeerIDsSupportsExplicitGroupsAndTags(t *testing.T) {
	srv := &Server{
		peerGroups:      map[string]*PeerGroup{},
		accountGroups:   map[string]map[string]*PeerGroup{},
		peerGroupsIndex: map[string]map[string]bool{},
	}

	srv.peerGroups["core"] = &PeerGroup{
		ID:        "core",
		AccountID: "acct-1",
		Name:      "Core Routers",
	}
	srv.peerGroupsIndex["peer-a"] = map[string]bool{"core": true}

	peers := []*PeerMetadata{
		{ID: "peer-a", AccountID: "acct-1", Tags: []string{"ignored"}},
		{ID: "peer-b", AccountID: "acct-1", Tags: []string{"core"}},
		{ID: "peer-c", AccountID: "acct-1", Tags: []string{"branch-office"}, ClientType: "wantasticd"},
	}

	got := srv.resolvePolicyPeerIDs("acct-1", "core", peers)
	if len(got) != 2 || got[0] != "peer-a" || got[1] != "peer-b" {
		t.Fatalf("resolvePolicyPeerIDs(group) = %v, want [peer-a peer-b]", got)
	}

	got = srv.resolvePolicyPeerIDs("acct-1", "tag:branch-office", peers)
	if len(got) != 1 || got[0] != "peer-c" {
		t.Fatalf("resolvePolicyPeerIDs(tag) = %v, want [peer-c]", got)
	}

	got = srv.resolvePolicyPeerIDs("acct-1", "client:wantasticd", peers)
	if len(got) != 1 || got[0] != "peer-c" {
		t.Fatalf("resolvePolicyPeerIDs(client) = %v, want [peer-c]", got)
	}
}

func TestTopologyPeerLabelsIncludeGroupsTagsAndClientType(t *testing.T) {
	srv := &Server{
		peerGroups:      map[string]*PeerGroup{},
		accountGroups:   map[string]map[string]*PeerGroup{},
		peerGroupsIndex: map[string]map[string]bool{},
	}
	srv.peerGroups["ops"] = &PeerGroup{
		ID:        "ops",
		AccountID: "acct-1",
		Name:      "Operations",
	}
	srv.peerGroupsIndex["peer-z"] = map[string]bool{"ops": true}

	labels := srv.TopologyPeerLabels("acct-1", &PeerMetadata{
		ID:           "peer-z",
		AccountID:    "acct-1",
		Tags:         []string{"edge", "MikroTik"},
		ClientType:   "wantasticd",
		IsWantasticd: true,
	})

	expected := map[string]bool{
		"group:ops":         true,
		"group:operations":  true,
		"tag:edge":          true,
		"tag:mikrotik":      true,
		"client:wantasticd": true,
	}
	for _, label := range labels {
		delete(expected, label)
	}
	if len(expected) != 0 {
		t.Fatalf("TopologyPeerLabels missing labels: %v (got %v)", expected, labels)
	}
}
