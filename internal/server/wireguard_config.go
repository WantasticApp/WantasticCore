package server

import (
	"fmt"
	"strings"
)

const defaultWireGuardFallbackDNS = "1.1.1.1"

// WireGuardConfigOptions describes a generated client configuration.
type WireGuardConfigOptions struct {
	PrivateKey          string
	Address             string
	ServerPublicKey     string
	Endpoint            string
	AllowedIPs          []string
	DNSServers          []string
	PersistentKeepalive int
	MTU                 int
	ListenPort          int
}

// WireGuardDNSServers returns the preferred resolver order for generated client configs.
// The tenant-local DNS server comes first so overlay names resolve there, and it can
// recurse to the public fallback when the name is not inside the tenant zone.
func WireGuardDNSServers(primary string) []string {
	seen := make(map[string]struct{}, 2)
	servers := make([]string, 0, 2)
	for _, candidate := range []string{primary, defaultWireGuardFallbackDNS} {
		candidate = strings.TrimSpace(candidate)
		if candidate == "" {
			continue
		}
		if _, ok := seen[candidate]; ok {
			continue
		}
		seen[candidate] = struct{}{}
		servers = append(servers, candidate)
	}
	return servers
}

// WireGuardAllowedIPs normalizes the client route list so every config path
// emits the same tenant subnet routing table.
func WireGuardAllowedIPs(routes []string) ([]string, error) {
	seen := make(map[string]struct{}, len(routes))
	normalized := make([]string, 0, len(routes))
	for _, route := range routes {
		route = strings.TrimSpace(route)
		if route == "" {
			continue
		}
		if _, ok := seen[route]; ok {
			continue
		}
		seen[route] = struct{}{}
		normalized = append(normalized, route)
	}
	if len(normalized) == 0 {
		return nil, fmt.Errorf("no tenant networks configured")
	}
	return normalized, nil
}

// BuildWireGuardConfig renders a minimal wg-quick compatible client config.
func BuildWireGuardConfig(opts WireGuardConfigOptions) string {
	var cfg strings.Builder
	cfg.WriteString("[Interface]\n")
	cfg.WriteString(fmt.Sprintf("PrivateKey = %s\n", opts.PrivateKey))
	cfg.WriteString(fmt.Sprintf("Address = %s\n", opts.Address))
	if opts.MTU > 0 {
		cfg.WriteString(fmt.Sprintf("MTU = %d\n", opts.MTU))
	}
	if len(opts.DNSServers) > 0 {
		cfg.WriteString(fmt.Sprintf("DNS = %s\n", strings.Join(opts.DNSServers, ", ")))
	}
	if opts.ListenPort > 0 {
		cfg.WriteString(fmt.Sprintf("ListenPort = %d\n", opts.ListenPort))
	}
	cfg.WriteString("\n[Peer]\n")
	cfg.WriteString(fmt.Sprintf("PublicKey = %s\n", opts.ServerPublicKey))
	cfg.WriteString(fmt.Sprintf("Endpoint = %s\n", opts.Endpoint))
	cfg.WriteString(fmt.Sprintf("AllowedIPs = %s\n", strings.Join(opts.AllowedIPs, ", ")))
	if opts.PersistentKeepalive > 0 {
		cfg.WriteString(fmt.Sprintf("PersistentKeepalive = %d\n", opts.PersistentKeepalive))
	}
	return cfg.String()
}
