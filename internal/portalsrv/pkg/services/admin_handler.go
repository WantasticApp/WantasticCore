package services

import (
	"context"
	"encoding/json"

	"WantasticCore/internal/admin"
)

// handleAdminService dispatches super-admin WebSocket messages
// (CreateTenant, ListTenants, DeleteTenant, SetTenantMaxPeers,
// SetTenantPassword, SetTenantAdmin, SetTenantStatus). Every method requires
// the calling session to belong to a tenant flagged IsAdmin.
func (p *TenantProxy) handleAdminService(_ context.Context, msg *Message, session *TenantSession) *Response {
	if p.admin == nil {
		return adminErr(msg.ID, "admin service not configured")
	}
	if err := p.admin.Authorize(session.TenantID); err != nil {
		return adminErr(msg.ID, err.Error())
	}

	switch msg.Method {
	case "ListTenants":
		out, err := p.admin.ListTenants()
		if err != nil {
			return adminErr(msg.ID, err.Error())
		}
		return adminOK(msg.ID, map[string]any{"tenants": out})

	case "GetTenant":
		var req struct {
			ID string `json:"id"`
		}
		if err := json.Unmarshal(msg.Request, &req); err != nil {
			return adminErr(msg.ID, "invalid request: "+err.Error())
		}
		t, err := p.admin.GetTenant(req.ID)
		if err != nil {
			return adminErr(msg.ID, err.Error())
		}
		return adminOK(msg.ID, t)

	case "CreateTenant":
		var req struct {
			Email    string `json:"email"`
			FullName string `json:"full_name"`
			Password string `json:"password"`
			MaxPeers int    `json:"max_peers"`
			IsAdmin  bool   `json:"is_admin"`
		}
		if err := json.Unmarshal(msg.Request, &req); err != nil {
			return adminErr(msg.ID, "invalid request: "+err.Error())
		}
		t, err := p.admin.CreateTenant(admin.CreateTenantInput{
			Email:    req.Email,
			FullName: req.FullName,
			Password: req.Password,
			MaxPeers: req.MaxPeers,
			IsAdmin:  req.IsAdmin,
		})
		if err != nil {
			return adminErr(msg.ID, err.Error())
		}
		return adminOK(msg.ID, t)

	case "DeleteTenant":
		var req struct {
			ID string `json:"id"`
		}
		if err := json.Unmarshal(msg.Request, &req); err != nil {
			return adminErr(msg.ID, "invalid request: "+err.Error())
		}
		if req.ID == session.TenantID {
			return adminErr(msg.ID, "refuse to delete the caller's own account")
		}
		if err := p.admin.DeleteTenant(req.ID); err != nil {
			return adminErr(msg.ID, err.Error())
		}
		return adminOK(msg.ID, map[string]any{"ok": true})

	case "SetTenantMaxPeers":
		var req struct {
			ID       string `json:"id"`
			MaxPeers int    `json:"max_peers"`
		}
		if err := json.Unmarshal(msg.Request, &req); err != nil {
			return adminErr(msg.ID, "invalid request: "+err.Error())
		}
		if err := p.admin.SetTenantMaxPeers(req.ID, req.MaxPeers); err != nil {
			return adminErr(msg.ID, err.Error())
		}
		return adminOK(msg.ID, map[string]any{"ok": true})

	case "SetTenantPassword":
		var req struct {
			ID       string `json:"id"`
			Password string `json:"password"`
		}
		if err := json.Unmarshal(msg.Request, &req); err != nil {
			return adminErr(msg.ID, "invalid request: "+err.Error())
		}
		if err := p.admin.SetTenantPassword(req.ID, req.Password); err != nil {
			return adminErr(msg.ID, err.Error())
		}
		return adminOK(msg.ID, map[string]any{"ok": true})

	case "SetTenantAdmin":
		var req struct {
			ID      string `json:"id"`
			IsAdmin bool   `json:"is_admin"`
		}
		if err := json.Unmarshal(msg.Request, &req); err != nil {
			return adminErr(msg.ID, "invalid request: "+err.Error())
		}
		if err := p.admin.SetTenantAdmin(req.ID, req.IsAdmin); err != nil {
			return adminErr(msg.ID, err.Error())
		}
		return adminOK(msg.ID, map[string]any{"ok": true})

	case "SetTenantStatus":
		var req struct {
			ID     string `json:"id"`
			Status string `json:"status"`
		}
		if err := json.Unmarshal(msg.Request, &req); err != nil {
			return adminErr(msg.ID, "invalid request: "+err.Error())
		}
		if err := p.admin.SetTenantStatus(req.ID, req.Status); err != nil {
			return adminErr(msg.ID, err.Error())
		}
		return adminOK(msg.ID, map[string]any{"ok": true})

	default:
		return adminErr(msg.ID, "unknown admin method: "+msg.Method)
	}
}

func adminOK(id string, payload any) *Response {
	raw, err := json.Marshal(payload)
	if err != nil {
		return adminErr(id, "marshal response: "+err.Error())
	}
	return &Response{ID: id, Type: "response", Response: raw}
}

func adminErr(id, msg string) *Response {
	return &Response{ID: id, Type: "error", Error: msg}
}
