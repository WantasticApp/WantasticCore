package server

import (
	"sort"
	"strings"
)

type policySelector struct {
	kind  string
	value string
}

func parsePolicySelector(raw string) policySelector {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return policySelector{kind: "group"}
	}

	lower := strings.ToLower(raw)
	switch {
	case strings.HasPrefix(lower, "tag:"):
		return policySelector{kind: "tag", value: normalizePolicyToken(raw[4:])}
	case strings.HasPrefix(lower, "peer:"):
		return policySelector{kind: "peer", value: strings.TrimSpace(raw[5:])}
	case strings.HasPrefix(lower, "client:"):
		return policySelector{kind: "client", value: normalizePolicyToken(raw[7:])}
	case strings.HasPrefix(lower, "group:"):
		return policySelector{kind: "group", value: strings.TrimSpace(raw[6:])}
	default:
		return policySelector{kind: "group", value: raw}
	}
}

func normalizePolicyToken(value string) string {
	value = strings.TrimSpace(strings.ToLower(value))
	if value == "" {
		return ""
	}

	replacer := strings.NewReplacer(" ", "-", "_", "-", "/", "-", ":", "-")
	value = replacer.Replace(value)
	for strings.Contains(value, "--") {
		value = strings.ReplaceAll(value, "--", "-")
	}
	return strings.Trim(value, "-")
}

func peerTagMatches(peer *PeerMetadata, aliases map[string]struct{}) bool {
	for _, tag := range peer.Tags {
		if _, ok := aliases[normalizePolicyToken(tag)]; ok {
			return true
		}
	}
	return false
}

func groupTagAliases(groupID string, group *PeerGroup) map[string]struct{} {
	aliases := map[string]struct{}{}
	if normalized := normalizePolicyToken(groupID); normalized != "" {
		aliases[normalized] = struct{}{}
	}
	if group != nil {
		if normalized := normalizePolicyToken(group.Name); normalized != "" {
			aliases[normalized] = struct{}{}
		}
	}
	return aliases
}

func (s *Server) resolvePolicyPeerIDs(accountID string, rawSelector string, peers []*PeerMetadata) []string {
	selector := parsePolicySelector(rawSelector)

	s.groupMu.RLock()
	group := s.peerGroups[selector.value]
	explicitMembers := make(map[string]bool)
	if selector.kind == "group" {
		for _, peerID := range s.GetGroupPeers(selector.value) {
			explicitMembers[peerID] = true
		}
	}
	s.groupMu.RUnlock()

	matched := make(map[string]struct{})
	switch selector.kind {
	case "peer":
		for _, peer := range peers {
			if peer.ID == selector.value {
				matched[peer.ID] = struct{}{}
				break
			}
		}

	case "tag":
		aliases := map[string]struct{}{selector.value: {}}
		for _, peer := range peers {
			if peerTagMatches(peer, aliases) {
				matched[peer.ID] = struct{}{}
			}
		}

	case "client":
		for _, peer := range peers {
			clientType := normalizePolicyToken(peer.ClientType)
			if selector.value == "wantasticd" {
				if clientType == "wantasticd" || peer.IsWantasticd {
					matched[peer.ID] = struct{}{}
				}
				continue
			}
			if clientType == selector.value {
				matched[peer.ID] = struct{}{}
			}
		}

	default:
		aliases := groupTagAliases(selector.value, group)
		for _, peer := range peers {
			if explicitMembers[peer.ID] || peerTagMatches(peer, aliases) {
				matched[peer.ID] = struct{}{}
			}
		}
	}

	peerIDs := make([]string, 0, len(matched))
	for peerID := range matched {
		peerIDs = append(peerIDs, peerID)
	}
	sort.Strings(peerIDs)
	return peerIDs
}

func (s *Server) TopologyPeerLabels(accountID string, peer *PeerMetadata) []string {
	labels := make(map[string]struct{})
	for _, group := range s.GetPeerGroups(accountID, peer.ID) {
		labels["group:"+group.ID] = struct{}{}
		if normalized := normalizePolicyToken(group.Name); normalized != "" {
			labels["group:"+normalized] = struct{}{}
		}
	}
	for _, tag := range peer.Tags {
		normalized := normalizePolicyToken(tag)
		if normalized == "" {
			continue
		}
		labels["tag:"+normalized] = struct{}{}
	}
	if peer.IsWantasticd || normalizePolicyToken(peer.ClientType) == "wantasticd" {
		labels["client:wantasticd"] = struct{}{}
	}
	if clientType := normalizePolicyToken(peer.ClientType); clientType != "" && clientType != "wantasticd" {
		labels["client:"+clientType] = struct{}{}
	}

	result := make([]string, 0, len(labels))
	for label := range labels {
		result = append(result, label)
	}
	sort.Strings(result)
	return result
}
