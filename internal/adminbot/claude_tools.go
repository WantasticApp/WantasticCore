package adminbot

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"

	pb "WantasticCore/internal/types"
	"WantasticCore/internal/auth"

	"github.com/anthropics/anthropic-sdk-go"
)

// claudeTools defines the on-demand context tools the model can call when the
// pre-baked telemetry context isn't enough. The catalog is intentionally
// small: full tenant list + single-tenant detail. PII (email, phone) is gated
// behind explicit `include_email` / `include_phone` flags so the assistant
// has to ask for it deliberately — and so prompt-injection from a chat
// message can't quietly exfiltrate addresses.
//
// Adding a new tool: append to this slice, then add a case in the dispatch
// switch in runClaudeToolLoop. Tool inputs come back as a JSON object —
// unmarshal into a typed struct so the executor stays self-documenting.
func claudeTools() []anthropic.ToolUnionParam {
	return []anthropic.ToolUnionParam{
		{
			OfTool: &anthropic.ToolParam{
				Name:        "list_tenants",
				Description: anthropic.String("List tenants in the current account with timestamps (created_at, updated_at, last_login, last_activity). Use when the pre-baked context's top-5 isn't enough — e.g. \"who is the oldest tenant?\", \"how many tenants signed up last month?\". Email is excluded by default; set include_email=true ONLY if the user explicitly asked for emails."),
				InputSchema: anthropic.ToolInputSchemaParam{
					Properties: map[string]any{
						"sort": map[string]any{
							"type":        "string",
							"enum":        []string{"created_asc", "created_desc", "last_activity_desc", "peers_desc", "name_asc"},
							"description": "Sort order. Use created_asc to find the oldest tenant.",
						},
						"limit": map[string]any{
							"type":        "integer",
							"minimum":     1,
							"maximum":     200,
							"description": "Max rows to return (default 50).",
						},
						"include_email": map[string]any{
							"type":        "boolean",
							"description": "Include email addresses (PII). Only when explicitly requested by the user.",
						},
						"include_phone": map[string]any{
							"type":        "boolean",
							"description": "Include phone numbers (PII). Only when explicitly requested by the user.",
						},
						"status": map[string]any{
							"type":        "string",
							"description": "Filter by status (e.g. active, suspended). Optional.",
						},
						"tier": map[string]any{
							"type":        "string",
							"description": "Filter by tier (e.g. free, pro). Optional.",
						},
					},
				},
			},
		},
		{
			OfTool: &anthropic.ToolParam{
				Name:        "get_tenant",
				Description: anthropic.String("Look up a single tenant by ID, email, full name, or phone. Returns full timestamps + peer fleet age window + peer_limit_override if set. Same PII gating rules as list_tenants."),
				InputSchema: anthropic.ToolInputSchemaParam{
					Properties: map[string]any{
						"selector": map[string]any{
							"type":        "string",
							"description": "Tenant ID (UUID), email, full name, or phone number.",
						},
						"include_email": map[string]any{"type": "boolean"},
						"include_phone": map[string]any{"type": "boolean"},
					},
					Required: []string{"selector"},
				},
			},
		},
		{
			OfTool: &anthropic.ToolParam{
				Name:        "set_peer_limit",
				Description: anthropic.String("Override the maximum number of devices (peers) for a tenant without touching their billing plan. Use when the user asks to raise/lower a specific tenant's device cap. Pass limit=0 to clear the override and revert to plan defaults. Always call get_tenant first to confirm the right tenant."),
				InputSchema: anthropic.ToolInputSchemaParam{
					Properties: map[string]any{
						"selector": map[string]any{
							"type":        "string",
							"description": "Tenant ID (UUID), email, full name, or phone number.",
						},
						"limit": map[string]any{
							"type":        "integer",
							"minimum":     0,
							"description": "New device cap. 0 = clear override and revert to plan default.",
						},
					},
					Required: []string{"selector", "limit"},
				},
			},
		},
		{
			OfTool: &anthropic.ToolParam{
				Name:        "create_peer",
				Description: anthropic.String("Create a new WireGuard peer (device) for a tenant. Returns the peer ID, assigned IP, and WireGuard config. Use when the user asks to add a device for a specific tenant. If the user does not provide a device name, omit `name` and the bot will auto-generate one."),
				InputSchema: anthropic.ToolInputSchemaParam{
					Properties: map[string]any{
						"selector": map[string]any{
							"type":        "string",
							"description": "Tenant ID (UUID), email, full name, or phone number.",
						},
						"name": map[string]any{
							"type":        "string",
							"description": "Optional display name for the new peer (e.g. 'Office Router', 'Lab-01'). Omit to auto-generate a safe default.",
						},
					},
					Required: []string{"selector"},
				},
			},
		},
		{
			OfTool: &anthropic.ToolParam{
				Name:        "ping_peer",
				Description: anthropic.String("Ping a specific peer (device) on a tenant to check connectivity. Identify the peer by name or peer ID. Returns RTT stats and packet loss."),
				InputSchema: anthropic.ToolInputSchemaParam{
					Properties: map[string]any{
						"selector": map[string]any{
							"type":        "string",
							"description": "Tenant ID (UUID), email, full name, or phone number.",
						},
						"peer": map[string]any{
							"type":        "string",
							"description": "Peer name or peer ID (UUID).",
						},
						"count": map[string]any{
							"type":        "integer",
							"minimum":     1,
							"maximum":     20,
							"description": "Number of ping packets (default 5).",
						},
					},
					Required: []string{"selector", "peer"},
				},
			},
		},
	}
}

// DispatchClaudeTool runs a single tool call. Implements ToolDispatcher so
// *Bot can be passed directly into AskWithMemory. Any error is wrapped into
// the returned string under an `error:` key so the assistant can see what
// went wrong instead of the conversation getting torn down by a
// transport-level failure on Anthropic's side.
func (b *Bot) DispatchClaudeTool(ctx context.Context, name string, rawInput json.RawMessage) string {
	switch name {
	case "list_tenants":
		var args struct {
			Sort         string `json:"sort"`
			Limit        int    `json:"limit"`
			IncludeEmail bool   `json:"include_email"`
			IncludePhone bool   `json:"include_phone"`
			Status       string `json:"status"`
			Tier         string `json:"tier"`
		}
		if err := json.Unmarshal(rawInput, &args); err != nil {
			return fmt.Sprintf("error: invalid arguments: %v", err)
		}
		return b.toolListTenants(ctx, args.Sort, args.Limit, args.IncludeEmail, args.IncludePhone, args.Status, args.Tier)

	case "get_tenant":
		var args struct {
			Selector     string `json:"selector"`
			IncludeEmail bool   `json:"include_email"`
			IncludePhone bool   `json:"include_phone"`
		}
		if err := json.Unmarshal(rawInput, &args); err != nil {
			return fmt.Sprintf("error: invalid arguments: %v", err)
		}
		return b.toolGetTenant(ctx, args.Selector, args.IncludeEmail, args.IncludePhone)

	case "set_peer_limit":
		var args struct {
			Selector string `json:"selector"`
			Limit    int    `json:"limit"`
		}
		if err := json.Unmarshal(rawInput, &args); err != nil {
			return fmt.Sprintf("error: invalid arguments: %v", err)
		}
		return b.toolSetPeerLimit(ctx, args.Selector, args.Limit)

	case "create_peer":
		var args struct {
			Selector string `json:"selector"`
			Name     string `json:"name"`
		}
		if err := json.Unmarshal(rawInput, &args); err != nil {
			return fmt.Sprintf("error: invalid arguments: %v", err)
		}
		return b.toolCreatePeer(ctx, args.Selector, args.Name)

	case "ping_peer":
		var args struct {
			Selector string `json:"selector"`
			Peer     string `json:"peer"`
			Count    int32  `json:"count"`
		}
		if err := json.Unmarshal(rawInput, &args); err != nil {
			return fmt.Sprintf("error: invalid arguments: %v", err)
		}
		if args.Count <= 0 {
			args.Count = 5
		}
		return b.toolPingPeer(ctx, args.Selector, args.Peer, args.Count)

	default:
		return fmt.Sprintf("error: unknown tool %q", name)
	}
}

func (b *Bot) toolListTenants(ctx context.Context, sortKey string, limit int, includeEmail, includePhone bool, statusFilter, tierFilter string) string {
	cards, err := b.telemetry.LoadTenantCards(ctx, "month", AnalyticsFilters{})
	if err != nil {
		return fmt.Sprintf("error: load tenants: %v", err)
	}

	// Apply optional filters.
	if statusFilter = strings.TrimSpace(statusFilter); statusFilter != "" {
		filtered := cards[:0]
		for _, c := range cards {
			if strings.EqualFold(c.Status, statusFilter) {
				filtered = append(filtered, c)
			}
		}
		cards = filtered
	}
	// Tier filter removed — billing/tier concept is gone (Phase 2).
	_ = tierFilter

	// Apply sort.
	switch sortKey {
	case "created_asc":
		sort.Slice(cards, func(i, j int) bool { return cards[i].CreatedAt.Before(cards[j].CreatedAt) })
	case "created_desc", "":
		sort.Slice(cards, func(i, j int) bool { return cards[i].CreatedAt.After(cards[j].CreatedAt) })
	case "last_activity_desc":
		sort.Slice(cards, func(i, j int) bool { return cards[i].LastActivityAt.After(cards[j].LastActivityAt) })
	case "peers_desc":
		sort.Slice(cards, func(i, j int) bool { return cards[i].PeerCount > cards[j].PeerCount })
	case "name_asc":
		sort.Slice(cards, func(i, j int) bool {
			return strings.ToLower(cards[i].FullName) < strings.ToLower(cards[j].FullName)
		})
	}

	if limit <= 0 {
		limit = 50
	}
	if limit > len(cards) {
		limit = len(cards)
	}
	cards = cards[:limit]

	now := time.Now()
	var lines []string
	lines = append(lines, fmt.Sprintf("total=%d sort=%s", limit, sortKey))
	for _, c := range cards {
		lines = append(lines, formatTenantToolLine(*c, now, includeEmail, includePhone))
	}
	return strings.Join(lines, "\n")
}

func (b *Bot) toolGetTenant(ctx context.Context, selector string, includeEmail, includePhone bool) string {
	if strings.TrimSpace(selector) == "" {
		return "error: selector is required"
	}
	card, err := b.telemetry.ResolveTenant(ctx, selector)
	if err != nil {
		return fmt.Sprintf("error: resolve tenant %q: %v", selector, err)
	}
	if card == nil {
		return fmt.Sprintf("not-found: no tenant matched %q", selector)
	}
	now := time.Now()
	out := []string{
		fmt.Sprintf("id=%s", card.ID),
		formatTenantToolLine(*card, now, includeEmail, includePhone),
	}
	if len(card.Networks) > 0 {
		out = append(out, fmt.Sprintf("networks=%s", strings.Join(card.Networks, ",")))
	}
	return strings.Join(out, "\n")
}

func (b *Bot) toolSetPeerLimit(ctx context.Context, selector string, limit int) string {
	if strings.TrimSpace(selector) == "" {
		return "error: selector is required"
	}
	if limit < 0 {
		return "error: limit must be >= 0 (use 0 to clear override)"
	}
	card, err := b.telemetry.ResolveTenant(ctx, selector)
	if err != nil {
		return fmt.Sprintf("error: resolve tenant %q: %v", selector, err)
	}
	if card == nil {
		return fmt.Sprintf("not-found: no tenant matched %q", selector)
	}
	if card.OverlayAccountID == "" {
		return fmt.Sprintf("error: tenant %q has no associated account", selector)
	}

	// Route the write through gRPC so the bot never touches the DB directly.
	// UpdateAccountQuotas.max_peers_per_network is the peer-limit override knob;
	// 0 clears the override and reverts to plan default.
	grpcCtx, cancel := context.WithTimeout(ctx, b.cfg.GRPCTimeout())
	defer cancel()
	_, err = b.services.Account.UpdateAccountQuotas(grpcCtx, &pb.UpdateAccountQuotasRequest{
		AccountId:          card.OverlayAccountID,
		MaxPeersPerNetwork: int32(limit),
	})
	if err != nil {
		return fmt.Sprintf("error: set peer limit via gRPC: %v", err)
	}
	if limit == 0 {
		return fmt.Sprintf("ok: peer limit override cleared for tenant %q (%s) — reverts to plan default", card.FullName, card.ID)
	}
	return fmt.Sprintf("ok: peer limit set to %d for tenant %q (%s)", limit, card.FullName, card.ID)
}

func (b *Bot) toolCreatePeer(ctx context.Context, selector, peerName string) string {
	if strings.TrimSpace(selector) == "" {
		return "error: selector is required"
	}
	card, err := b.telemetry.ResolveTenant(ctx, selector)
	if err != nil {
		return fmt.Sprintf("error: resolve tenant %q: %v", selector, err)
	}
	if card == nil {
		return fmt.Sprintf("not-found: no tenant matched %q", selector)
	}
	resolvedPeerName, autoNamed := resolveCreatedPeerName(peerName, time.Now().UTC())

	grpcCtx, cancel := context.WithTimeout(ctx, b.cfg.GRPCTimeout())
	defer cancel()
	grpcCtx = auth.WithCallContext(grpcCtx, &auth.CallContext{TenantID: card.ID})

	resp, err := b.services.TenantPortal.AddTenantPeer(grpcCtx, &pb.AddTenantPeerRequest{
		TenantId: card.ID,
		Name:     resolvedPeerName,
	})
	if err != nil {
		return fmt.Sprintf("error: create peer: %v", err)
	}
	peer := resp.GetPeer()
	lines := []string{
		fmt.Sprintf("ok: peer created for tenant %q (%s)", card.FullName, card.ID),
		fmt.Sprintf("peer_id=%s name=%s ip=%s", peer.GetId(), peer.GetName(), peer.GetAssignedIp()),
	}
	if autoNamed {
		lines = append(lines, fmt.Sprintf("note: name was auto-generated as %q", resolvedPeerName))
	}
	if cfg := strings.TrimSpace(resp.GetConfig()); cfg != "" {
		lines = append(lines, "wg_config:\n```\n"+cfg+"\n```")
	} else {
		lines = append(lines, "note: peer was created, but the WireGuard config is not available yet. Retrieve it later from the portal.")
	}
	return strings.Join(lines, "\n")
}

func resolveCreatedPeerName(peerName string, now time.Time) (string, bool) {
	trimmed := strings.TrimSpace(peerName)
	if trimmed != "" {
		return trimmed, false
	}

	return fmt.Sprintf("device-%s-%03d", now.Format("20060102-150405"), now.Nanosecond()/1e6), true
}

func (b *Bot) toolPingPeer(ctx context.Context, selector, peerSelector string, count int32) string {
	if strings.TrimSpace(selector) == "" {
		return "error: selector is required"
	}
	if strings.TrimSpace(peerSelector) == "" {
		return "error: peer is required"
	}
	card, err := b.telemetry.ResolveTenant(ctx, selector)
	if err != nil {
		return fmt.Sprintf("error: resolve tenant %q: %v", selector, err)
	}
	if card == nil {
		return fmt.Sprintf("not-found: no tenant matched %q", selector)
	}

	// Resolve peer: list tenant's peers and match by name or ID.
	grpcCtx, cancel := context.WithTimeout(ctx, b.cfg.GRPCTimeout())
	defer cancel()
	grpcCtx = auth.WithCallContext(grpcCtx, &auth.CallContext{TenantID: card.ID})

	listResp, err := b.services.TenantPortal.ListTenantPeers(grpcCtx, &pb.ListTenantPeersRequest{TenantId: card.ID})
	if err != nil {
		return fmt.Sprintf("error: list peers: %v", err)
	}
	var peerID string
	var peerName string
	for _, p := range listResp.GetPeers() {
		if p.GetId() == peerSelector || strings.EqualFold(p.GetName(), peerSelector) {
			peerID = p.GetId()
			peerName = p.GetName()
			break
		}
	}
	if peerID == "" {
		return fmt.Sprintf("not-found: no peer matched %q on tenant %q", peerSelector, card.FullName)
	}

	pingCtx, pingCancel := context.WithTimeout(ctx, b.cfg.GRPCTimeout())
	defer pingCancel()
	pingCtx = auth.WithCallContext(pingCtx, &auth.CallContext{TenantID: card.ID})

	pingResp, err := b.services.TenantPortal.PingTenantPeer(pingCtx, &pb.PingTenantPeerRequest{
		TenantId: card.ID,
		PeerId:   peerID,
		Count:    count,
	})
	if err != nil {
		return fmt.Sprintf("error: ping peer %q: %v", peerName, err)
	}
	if !pingResp.GetSuccess() {
		return fmt.Sprintf("ping failed for peer %q (%s): %s", peerName, peerID, pingResp.GetError())
	}
	return fmt.Sprintf(
		"ping %q (%s) tenant=%q: sent=%d recv=%d loss=%.1f%% rtt_min=%.2fms avg=%.2fms max=%.2fms",
		peerName, peerID, card.FullName,
		pingResp.GetPacketsSent(), pingResp.GetPacketsReceived(), pingResp.GetPacketLossPercent(),
		pingResp.GetMinRttMs(), pingResp.GetAvgRttMs(), pingResp.GetMaxRttMs(),
	)
}

// formatTenantToolLine matches the compact key=value format the system-prompt
// telemetry context uses, so the model sees consistent shapes across the
// pre-baked context and on-demand tool calls.
func formatTenantToolLine(c TenantCard, now time.Time, includeEmail, includePhone bool) string {
	parts := []string{
		fmt.Sprintf("n=%s", emptyAs(c.FullName, "(anon)")),
		fmt.Sprintf("st=%s", emptyAs(c.Status, "?")),
		fmt.Sprintf("ctry=%s", emptyAs(c.Country, "-")),
		fmt.Sprintf("p=%d(on=%d/off=%d/wd=%d)", c.PeerCount, c.OnlinePeers, c.OfflinePeers, c.WantasticdPeers),
		fmt.Sprintf("ssh=%d wb=%d", c.SSHSessions, c.WinboxSessions),
		fmt.Sprintf("c=%s", shortDate(c.CreatedAt)),
		fmt.Sprintf("u=%s", shortDate(c.UpdatedAt)),
		fmt.Sprintf("login=%s", relAge(now, c.LastLogin)),
		fmt.Sprintf("last=%s", relAge(now, c.LastActivityAt)),
		fmt.Sprintf("pold=%s", shortDate(c.PeerOldestAt)),
		fmt.Sprintf("pnew=%s", shortDate(c.PeerNewestAt)),
		fmt.Sprintf("plast=%s", relAge(now, c.PeerLastSeenAt)),
	}
	if c.MaxPeers > 0 {
		parts = append(parts, fmt.Sprintf("max_peers=%d", c.MaxPeers))
	}
	if includeEmail && strings.TrimSpace(c.Email) != "" {
		parts = append(parts, fmt.Sprintf("email=%s", c.Email))
	}
	if includePhone && strings.TrimSpace(c.Phone) != "" {
		parts = append(parts, fmt.Sprintf("phone=%s", c.Phone))
	}
	return "- " + strings.Join(parts, " ")
}
