package copilot

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	pb "WantasticCore/internal/types"
	"WantasticCore/internal/admin"
	"WantasticCore/internal/auth"
)

// Tool is one Copilot-callable action. Schema is JSON Schema for the input,
// embedded as a raw JSON string so the LLM can validate calls itself.
type Tool struct {
	Name        string
	Description string
	InputSchema string
	AdminOnly   bool
	Run         func(ctx context.Context, svc *Service, input json.RawMessage) string
}

// allTools returns the full catalog. Filtered per-session by role; see
// dispatcher.Dispatch and Service.toolsFor.
func allTools() map[string]Tool {
	tools := map[string]Tool{}
	for _, t := range tenantTools() {
		tools[t.Name] = t
	}
	for _, t := range adminTools() {
		tools[t.Name] = t
	}
	return tools
}

// toolsFor returns the JSON-schema specs the LLM should see for this role.
func (s *Service) toolsFor(r Role) []ToolSpec {
	var picked []Tool
	for _, t := range tenantTools() {
		picked = append(picked, t)
	}
	if r == RoleAdmin {
		for _, t := range adminTools() {
			picked = append(picked, t)
		}
	}
	out := make([]ToolSpec, 0, len(picked))
	for _, t := range picked {
		out = append(out, ToolSpec{
			Name:        t.Name,
			Description: t.Description,
			InputSchema: json.RawMessage(t.InputSchema),
		})
	}
	return out
}

func systemPromptFor(r Role) string {
	common := `You are WantasticCopilot, an assistant embedded in the Wantastic admin portal.
You help the user manage their overlay network: list devices, ping them, view telemetry, and (for admins) provision tenants.
Be concise. When you need data, call a tool — do not invent answers. If a tool returns "error: ...", relay the error to the user instead of retrying blindly.`

	if r == RoleAdmin {
		return common + "\n\nYou are speaking with a SUPER-ADMIN. You may use admin-scoped tools to manage other tenants. Always confirm destructive actions (delete, disable) before executing them."
	}
	return common + "\n\nYou are speaking with a TENANT. You can only see and modify the caller's own devices."
}

// ─────────────────────────────────────────────────────────────────────────
// Tenant-scoped tools (available to everyone)
// ─────────────────────────────────────────────────────────────────────────

func tenantTools() []Tool {
	return []Tool{
		{
			Name:        "list_my_devices",
			Description: "List the caller's WireGuard peers (devices).",
			InputSchema: `{"type":"object","properties":{},"additionalProperties":false}`,
			Run: func(ctx context.Context, svc *Service, input json.RawMessage) string {
				tenantID := tenantIDFromCtx(ctx)
				if tenantID == "" {
					return "error: missing tenant id on call"
				}
				svcCtx := withCallerMetadata(ctx, tenantID)
				resp, err := svc.services.TenantPortal.ListTenantPeers(svcCtx, &pb.ListTenantPeersRequest{TenantId: tenantID})
				if err != nil {
					return "error: " + err.Error()
				}
				if len(resp.GetPeers()) == 0 {
					return "no devices."
				}
				var b strings.Builder
				fmt.Fprintf(&b, "%d device(s):\n", len(resp.GetPeers()))
				for _, p := range resp.GetPeers() {
					fmt.Fprintf(&b, "- %s (%s) ip=%s status=%s\n", p.GetName(), p.GetId(), p.GetAssignedIp(), peerStatus(p))
				}
				return b.String()
			},
		},
		{
			Name:        "ping_my_device",
			Description: "Ping one of the caller's devices by ID or name.",
			InputSchema: `{"type":"object","properties":{"target":{"type":"string","description":"peer id or name"},"count":{"type":"integer","minimum":1,"maximum":20}},"required":["target"],"additionalProperties":false}`,
			Run: func(ctx context.Context, svc *Service, input json.RawMessage) string {
				var args struct {
					Target string `json:"target"`
					Count  int32  `json:"count"`
				}
				if err := json.Unmarshal(input, &args); err != nil {
					return "error: bad input: " + err.Error()
				}
				if args.Target == "" {
					return "error: target is required"
				}
				if args.Count == 0 {
					args.Count = 4
				}
				tenantID := tenantIDFromCtx(ctx)
				svcCtx := withCallerMetadata(ctx, tenantID)
				list, err := svc.services.TenantPortal.ListTenantPeers(svcCtx, &pb.ListTenantPeersRequest{TenantId: tenantID})
				if err != nil {
					return "error: list peers: " + err.Error()
				}
				peerID := ""
				for _, p := range list.GetPeers() {
					if p.GetId() == args.Target || strings.EqualFold(p.GetName(), args.Target) {
						peerID = p.GetId()
						break
					}
				}
				if peerID == "" {
					return "not-found: no peer matched " + args.Target
				}
				resp, err := svc.services.TenantPortal.PingTenantPeer(svcCtx, &pb.PingTenantPeerRequest{
					TenantId: tenantID,
					PeerId:   peerID,
					Count:    args.Count,
				})
				if err != nil {
					return "error: " + err.Error()
				}
				return fmt.Sprintf("ping %s: avg=%.1fms loss=%d/%d", args.Target, float64(resp.GetAvgRttMs()), resp.GetPacketsSent()-resp.GetPacketsReceived(), resp.GetPacketsSent())
			},
		},
		{
			Name:        "get_my_account",
			Description: "Return the caller's account summary (network range, peer cap, status).",
			InputSchema: `{"type":"object","properties":{},"additionalProperties":false}`,
			Run: func(ctx context.Context, svc *Service, input json.RawMessage) string {
				tenantID := tenantIDFromCtx(ctx)
				svcCtx := withCallerMetadata(ctx, tenantID)
				resp, err := svc.services.TenantPortal.GetTenantAccount(svcCtx, &pb.GetTenantAccountRequest{TenantId: tenantID})
				if err != nil {
					return "error: " + err.Error()
				}
				acc := resp.GetAccount()
				accID := ""
				var networks []string
				if acc != nil {
					accID = acc.GetId()
					networks = acc.GetNetworks()
				}
				return fmt.Sprintf("account %s — networks=%v peers=%d/%d", accID, networks, resp.GetPeerCount(), resp.GetMaxPeers())
			},
		},
	}
}

// ─────────────────────────────────────────────────────────────────────────
// Admin-scoped tools (require RoleAdmin)
// ─────────────────────────────────────────────────────────────────────────

func adminTools() []Tool {
	return []Tool{
		{
			Name:        "list_tenants",
			Description: "List every tenant in the system (admin only).",
			InputSchema: `{"type":"object","properties":{},"additionalProperties":false}`,
			AdminOnly:   true,
			Run: func(ctx context.Context, svc *Service, input json.RawMessage) string {
				if svc.adminSvc == nil {
					return "error: admin service not configured"
				}
				list, err := svc.adminSvc.ListTenants()
				if err != nil {
					return "error: " + err.Error()
				}
				if len(list) == 0 {
					return "no tenants."
				}
				var b strings.Builder
				fmt.Fprintf(&b, "%d tenants:\n", len(list))
				for _, t := range list {
					fmt.Fprintf(&b, "- %s <%s> admin=%t peers=%d/%d status=%s\n", t.FullName, t.Email, t.IsAdmin, t.PeerCount, t.MaxPeers, t.Status)
				}
				return b.String()
			},
		},
		{
			Name:        "create_tenant",
			Description: "Provision a new tenant with an initial password and device cap.",
			InputSchema: `{"type":"object","properties":{"email":{"type":"string"},"full_name":{"type":"string"},"password":{"type":"string"},"max_peers":{"type":"integer","minimum":1},"is_admin":{"type":"boolean"}},"required":["email","full_name","password","max_peers"],"additionalProperties":false}`,
			AdminOnly:   true,
			Run: func(ctx context.Context, svc *Service, input json.RawMessage) string {
				if svc.adminSvc == nil {
					return "error: admin service not configured"
				}
				var args struct {
					Email    string `json:"email"`
					FullName string `json:"full_name"`
					Password string `json:"password"`
					MaxPeers int    `json:"max_peers"`
					IsAdmin  bool   `json:"is_admin"`
				}
				if err := json.Unmarshal(input, &args); err != nil {
					return "error: bad input: " + err.Error()
				}
				t, err := svc.adminSvc.CreateTenant(admin.CreateTenantInput{
					Email:    args.Email,
					FullName: args.FullName,
					Password: args.Password,
					MaxPeers: args.MaxPeers,
					IsAdmin:  args.IsAdmin,
				})
				if err != nil {
					return "error: " + err.Error()
				}
				return fmt.Sprintf("created tenant %s (%s)", t.Email, t.ID)
			},
		},
		{
			Name:        "set_tenant_max_peers",
			Description: "Change a tenant's max device cap.",
			InputSchema: `{"type":"object","properties":{"tenant_id":{"type":"string"},"max_peers":{"type":"integer","minimum":1}},"required":["tenant_id","max_peers"],"additionalProperties":false}`,
			AdminOnly:   true,
			Run: func(ctx context.Context, svc *Service, input json.RawMessage) string {
				if svc.adminSvc == nil {
					return "error: admin service not configured"
				}
				var args struct {
					TenantID string `json:"tenant_id"`
					MaxPeers int    `json:"max_peers"`
				}
				if err := json.Unmarshal(input, &args); err != nil {
					return "error: bad input: " + err.Error()
				}
				if err := svc.adminSvc.SetTenantMaxPeers(args.TenantID, args.MaxPeers); err != nil {
					return "error: " + err.Error()
				}
				return fmt.Sprintf("ok: set max_peers=%d for tenant %s", args.MaxPeers, args.TenantID)
			},
		},
		{
			Name:        "delete_tenant",
			Description: "Permanently delete a tenant, releasing their overlay account and peers.",
			InputSchema: `{"type":"object","properties":{"tenant_id":{"type":"string"}},"required":["tenant_id"],"additionalProperties":false}`,
			AdminOnly:   true,
			Run: func(ctx context.Context, svc *Service, input json.RawMessage) string {
				if svc.adminSvc == nil {
					return "error: admin service not configured"
				}
				var args struct {
					TenantID string `json:"tenant_id"`
				}
				if err := json.Unmarshal(input, &args); err != nil {
					return "error: bad input: " + err.Error()
				}
				if err := svc.adminSvc.DeleteTenant(args.TenantID); err != nil {
					return "error: " + err.Error()
				}
				return fmt.Sprintf("ok: deleted tenant %s", args.TenantID)
			},
		},
	}
}

// ─────────────────────────────────────────────────────────────────────────
// Context helpers
// ─────────────────────────────────────────────────────────────────────────

// ctxKey is unexported so callers from this package must use the helpers.
type ctxKey string

const ctxKeyTenantID ctxKey = "copilot.tenant_id"

// WithTenantID stamps the caller tenant onto the context so tools can read
// it without juggling parameters through every level.
func WithTenantID(ctx context.Context, tenantID string) context.Context {
	return context.WithValue(ctx, ctxKeyTenantID, tenantID)
}

func tenantIDFromCtx(ctx context.Context) string {
	v, _ := ctx.Value(ctxKeyTenantID).(string)
	return v
}

// withCallerMetadata attaches an auth.CallContext carrying the caller's
// tenant ID. Service impls read it via auth.CallerTenantID(ctx).
// Mirrors what the services proxy does for regular WS-to-handler dispatch.
func withCallerMetadata(ctx context.Context, tenantID string) context.Context {
	return auth.WithCallContext(ctx, &auth.CallContext{TenantID: tenantID})
}

func peerStatus(p *pb.Peer) string {
	if p.GetIsOnline() {
		return "online"
	}
	return "offline"
}
