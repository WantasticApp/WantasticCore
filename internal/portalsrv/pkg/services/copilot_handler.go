package services

import (
	"context"
	"encoding/json"

	"WantasticCore/internal/copilot"
)

// handleCopilotService dispatches Copilot WebSocket messages:
//   OpenSession           — start a new chat
//   SendMessage           — push a user turn, get the assistant reply
//   GetSession            — fetch a session's metadata + history
//   ListSessions          — list the caller's sessions
//   CloseSession          — release a session
//
// Each session is scoped to the caller's tenant ID; only super-admins get
// the admin tool catalog (CreateTenant, SetTenantMaxPeers, etc.) and the
// larger context budget.
func (p *TenantProxy) handleCopilotService(ctx context.Context, msg *Message, session *TenantSession) *Response {
	if p.copilot == nil {
		// Surfaced verbatim to the browser. Keep it actionable — the
		// in-app Copilot page parses for this prefix to render the "add
		// your Claude key" call-to-action instead of a raw error.
		return errResp(msg.ID, "copilot_disabled: Copilot is not configured. A super-admin can enable it from Admin → Settings by adding an Anthropic API key.")
	}
	if session.TenantID == "" {
		return errResp(msg.ID, "unauthenticated")
	}

	// Whether the caller is admin determines the tool catalog. Cheap lookup
	// via the existing admin service (Authorize) — falls open as tenant
	// when the admin service isn't wired or the caller isn't an admin.
	isAdmin := false
	if p.admin != nil && p.admin.Authorize(session.TenantID) == nil {
		isAdmin = true
	}

	switch msg.Method {
	case "OpenSession":
		sess := p.copilot.OpenSession(session.TenantID, isAdmin)
		return okResp(msg.ID, map[string]any{
			"session_id":  sess.ID,
			"role":        string(sess.Role),
			"created_at":  sess.CreatedAt,
			"last_active": sess.LastActive,
		})

	case "SendMessage":
		var req struct {
			SessionID string `json:"session_id"`
			Text      string `json:"text"`
		}
		if err := json.Unmarshal(msg.Request, &req); err != nil {
			return errResp(msg.ID, "invalid request: "+err.Error())
		}
		if req.SessionID == "" || req.Text == "" {
			return errResp(msg.ID, "session_id and text are required")
		}
		// Verify the session belongs to this tenant before allowing the call.
		sess := p.copilot.Get(req.SessionID)
		if sess == nil {
			return errResp(msg.ID, "session not found")
		}
		if sess.TenantID != session.TenantID {
			return errResp(msg.ID, "forbidden: session belongs to another tenant")
		}
		callCtx := copilot.WithTenantID(ctx, session.TenantID)
		reply, err := p.copilot.SendMessage(callCtx, req.SessionID, req.Text)
		if err != nil {
			return errResp(msg.ID, err.Error())
		}
		return okResp(msg.ID, map[string]any{
			"session_id": req.SessionID,
			"reply":      reply,
		})

	case "GetSession":
		var req struct {
			SessionID string `json:"session_id"`
		}
		if err := json.Unmarshal(msg.Request, &req); err != nil {
			return errResp(msg.ID, "invalid request: "+err.Error())
		}
		sess := p.copilot.Get(req.SessionID)
		if sess == nil || sess.TenantID != session.TenantID {
			return errResp(msg.ID, "session not found")
		}
		return okResp(msg.ID, map[string]any{
			"session_id":  sess.ID,
			"role":        string(sess.Role),
			"created_at":  sess.CreatedAt,
			"last_active": sess.LastActive,
			"history":     p.copilot.History(req.SessionID),
		})

	case "ListSessions":
		sessions := p.copilot.ListSessionsForTenant(session.TenantID)
		out := make([]map[string]any, 0, len(sessions))
		for _, s := range sessions {
			out = append(out, map[string]any{
				"session_id":  s.ID,
				"role":        string(s.Role),
				"created_at":  s.CreatedAt,
				"last_active": s.LastActive,
			})
		}
		return okResp(msg.ID, map[string]any{"sessions": out})

	case "CloseSession":
		var req struct {
			SessionID string `json:"session_id"`
		}
		if err := json.Unmarshal(msg.Request, &req); err != nil {
			return errResp(msg.ID, "invalid request: "+err.Error())
		}
		sess := p.copilot.Get(req.SessionID)
		if sess != nil && sess.TenantID != session.TenantID {
			return errResp(msg.ID, "forbidden: session belongs to another tenant")
		}
		ok := p.copilot.CloseSession(req.SessionID)
		return okResp(msg.ID, map[string]any{"ok": ok})

	default:
		return errResp(msg.ID, "unknown copilot method: "+msg.Method)
	}
}

func okResp(id string, payload any) *Response {
	raw, err := json.Marshal(payload)
	if err != nil {
		return errResp(id, "marshal response: "+err.Error())
	}
	return &Response{ID: id, Type: "response", Response: raw}
}

func errResp(id, msg string) *Response {
	return &Response{ID: id, Type: "error", Error: msg}
}
