import { writable, derived } from "svelte/store";
import { wsStore } from "./websocket";
import { peerStore } from "./peer";
import { websshStore } from "./webssh";
import { winboxAccountStore } from "./winboxAccounts";

// ── Types ─────────────────────────────────────────────────────────────────────

export interface SharePermissions {
  devices_read: boolean;   // view devices, topology, ACL, activity, ping, metrics
  devices_write: boolean;  // manage devices, Winbox, WebSSH, ACL
}

export interface TeamShare {
  share_id: string;
  owner_tenant_id: string;
  owner_email: string;
  owner_name: string;
  shared_email: string;
  sharee_name: string;
  permissions: SharePermissions | null;
  tag_filter: string[];        // empty = all devices
  status: "pending" | "accepted" | "revoked" | "expired";
  invite_token?: string;       // only returned to owner
  is_link_share?: boolean;     // true = anyone with URL can accept
  resend_count: number;
  created_at?: string;
  accepted_at?: string;
  expires_at?: string;
  last_resend_at?: string;
}

export interface AccessibleAccount {
  owner_tenant_id: string;
  owner_email: string;
  owner_name: string;
  share_id: string;
  sharee_name: string;
  permissions: SharePermissions | null;
  tag_filter: string[];
  accepted_at?: string;
}

export interface InviteDetails {
  valid: boolean;
  owner_email: string;
  owner_name: string;
  share_id: string;
  permissions: SharePermissions | null;
  tag_filter: string[];
  status: string;
  expires_at?: string;
  is_link_share?: boolean;
}

export interface TeamState {
  /** Outgoing invites — current user is owner */
  teammates: TeamShare[];
  teammate_limit: number;
  teammate_used: number;
  /** Incoming invites — current user is invitee */
  pending_invites: TeamShare[];
  /** Accepted memberships — accounts the current user was invited to */
  memberships: AccessibleAccount[];
  isLoading: boolean;
  error: string | null;
}

const initialState: TeamState = {
  teammates: [],
  teammate_limit: 0,
  teammate_used: 0,
  pending_invites: [],
  memberships: [],
  isLoading: false,
  error: null,
};

// ── Helpers ───────────────────────────────────────────────────────────────────
//
// ── SHARING FLOW OVERVIEW ─────────────────────────────────────────────────────
// Backend (shared_access.go):
//   1. Middleware builds CallerContext from session token (own scope + accepted shares).
//   2. ListTenantPeers / ListTenantWinboxSessions / ListTenantWebSSHSessions:
//      - CallerContext resolves own + shared scope, and resource-level flags come back
//        on each peer/session (is_shared, owner_name, viewer_can_write).
//   3. SharePermissions stored as {devices_read, devices_write} in DB; proto still uses
//      9 legacy fields for wire compat — they are expanded/collapsed at the service layer.
// Frontend (this store):
//   - parsePermissions() collapses any proto 9-field response to the 2-field model.
//   - toShare() / toAccessibleAccount() use parsePermissions() when normalising responses.
//   - Peer-level is_shared / owner_name / viewer_can_write come from peer.ts, not here.
// ─────────────────────────────────────────────────────────────────────────────

/** Convert 9-field proto permissions (camelCase or snake_case) to the 2-field model. */
function parsePermissions(p: any): SharePermissions | null {
  if (!p) return null;
  // New 2-field format (direct)
  if ("devices_read" in p || "devices_write" in p) {
    return { devices_read: !!p.devices_read, devices_write: !!p.devices_write };
  }
  // Legacy 9-field proto response (camelCase from JSON-decoded proto)
  const read  = !!(p.viewPeers || p.view_peers || p.viewTopology || p.view_topology ||
                   p.viewAcl   || p.view_acl   || p.viewActivity || p.view_activity);
  const write = !!(p.managePeers || p.manage_peers || p.manageWinbox || p.manage_winbox ||
                   p.manageWebssh || p.manage_webssh || p.manageAcl || p.manage_acl );
  return { devices_read: read, devices_write: write };
}

function toShare(raw: any): TeamShare {
  return {
    share_id:        raw.share_id       ?? raw.shareId       ?? "",
    owner_tenant_id: raw.owner_tenant_id ?? raw.ownerTenantId ?? "",
    owner_email:     raw.owner_email     ?? raw.ownerEmail    ?? "",
    owner_name:      raw.owner_name      ?? raw.ownerName     ?? "",
    shared_email:    raw.shared_email    ?? raw.sharedEmail   ?? "",
    sharee_name:     raw.sharee_name     ?? raw.shareeName    ?? "",
    permissions:     parsePermissions(raw.permissions),
    tag_filter:      raw.tag_filter      ?? [],
    status:          raw.status          ?? "pending",
    invite_token:    raw.invite_token    ?? raw.inviteToken   ?? undefined,
    is_link_share:   raw.is_link_share   ?? raw.isLinkShare   ?? false,
    resend_count:    raw.resend_count    ?? 0,
    created_at:      raw.created_at      ?? undefined,
    accepted_at:     raw.accepted_at     ?? undefined,
    expires_at:      raw.expires_at      ?? undefined,
    last_resend_at:  raw.last_resend_at  ?? undefined,
  };
}

function toAccessibleAccount(raw: any): AccessibleAccount {
  return {
    owner_tenant_id: raw.owner_tenant_id ?? "",
    owner_email:     raw.owner_email     ?? "",
    owner_name:      raw.owner_name      ?? "",
    share_id:        raw.share_id        ?? "",
    sharee_name:     raw.sharee_name     ?? "",
    permissions:     parsePermissions(raw.permissions),
    tag_filter:      raw.tag_filter      ?? [],
    accepted_at:     parseProtoDate(raw.accepted_at),
  };
}

/** Convert a proto Timestamp ({seconds, nanos}) or ISO string to an ISO string, or return undefined. */
function parseProtoDate(v: any): string | undefined {
  if (!v) return undefined;
  if (typeof v === "string") return v;
  if (typeof v === "number") return new Date(v).toISOString();
  if (typeof v === "object" && v.seconds != null) {
    return new Date(Number(v.seconds) * 1000).toISOString();
  }
  return undefined;
}

// ── Store factory ─────────────────────────────────────────────────────────────

export function createTeamStore() {
  const { subscribe, set, update } = writable<TeamState>(initialState);

  async function refreshSharedResources() {
    await Promise.allSettled([
      peerStore.listPeers(undefined, true),
      websshStore.listActiveSessions(true),
      winboxAccountStore.listAccounts(),
    ]);
  }

  // List outgoing invites (current user as owner)
  async function listTeammates() {
    update((s) => ({ ...s, isLoading: true, error: null }));
    try {
      const resp = await wsStore.callGRPC<{
        shares: any[];
        teammate_limit: number;
        teammate_used: number;
      }>("TenantPortalService", "ListAccessShares", {});

      const teammates = (resp.shares ?? []).map(toShare);
      update((s) => ({
        ...s,
        teammates,
        teammate_limit: resp.teammate_limit ?? 0,
        teammate_used:  resp.teammate_used  ?? 0,
        isLoading: false,
      }));
      return { success: true, teammates };
    } catch (err: any) {
      const error = err.message ?? "Failed to load teammates";
      update((s) => ({ ...s, error, isLoading: false }));
      return { success: false, error };
    }
  }

  // Invite a teammate by email
  async function inviteTeammate(params: {
    shared_email: string;
    sharee_name: string;
    permissions: SharePermissions;
    tag_filter?: string[];
  }) {
    try {
      const resp = await wsStore.callGRPC<{
        success: boolean;
        message: string;
        share: any;
      }>("TenantPortalService", "CreateAccessShare", {
        shared_email: params.shared_email,
        sharee_name:  params.sharee_name,
        permissions:  params.permissions,
        tag_filter:   params.tag_filter ?? [],
      });

      if (resp.success && resp.share) {
        const share = toShare(resp.share);
        update((s) => ({
          ...s,
          teammates:     [share, ...s.teammates],
          teammate_used: s.teammate_used + 1,
        }));
      }
      return { success: resp.success, message: resp.message };
    } catch (err: any) {
      return { success: false, message: err.message ?? "Failed to send invite" };
    }
  }

  // Create a link share (no email required — anyone with the URL can accept)
  async function createLinkShare(params: {
    permissions: SharePermissions;
    tag_filter?: string[];
    sharee_name?: string;
  }) {
    try {
      const resp = await wsStore.callGRPC<{
        success: boolean;
        message: string;
        share: any;
        invite_url: string;
        qr_code: string;
      }>("TenantPortalService", "CreateAccessShare", {
        is_link_share: true,
        sharee_name:   params.sharee_name ?? "",
        permissions:   params.permissions,
        tag_filter:    params.tag_filter ?? [],
      });

      if (resp.success && resp.share) {
        const share = toShare(resp.share);
        update((s) => ({
          ...s,
          teammates:     [share, ...s.teammates],
          teammate_used: s.teammate_used + 1,
        }));
      }
      return {
        success:    resp.success,
        message:    resp.message,
        invite_url: resp.invite_url ?? "",
        qr_code:    resp.qr_code   ?? "",
      };
    } catch (err: any) {
      return { success: false, message: err.message ?? "Failed to create link share", invite_url: "", qr_code: "" };
    }
  }

  // Revoke a teammate's access
  async function revokeTeammate(shareId: string) {
    try {
      const resp = await wsStore.callGRPC<{ success: boolean; message: string }>(
        "TenantPortalService", "DeleteAccessShare", { share_id: shareId }
      );
      if (resp.success) {
        update((s) => ({
          ...s,
          teammates: s.teammates.filter((t) => t.share_id !== shareId),
          teammate_used: Math.max(0, s.teammate_used - 1),
        }));
      }
      return { success: resp.success, message: resp.message };
    } catch (err: any) {
      return { success: false, message: err.message ?? "Failed to revoke access" };
    }
  }

  // Resend invite email (rate limited)
  async function resendInvite(shareId: string) {
    try {
      const resp = await wsStore.callGRPC<{
        success: boolean;
        message: string;
        retry_after_seconds?: number;
      }>("TenantPortalService", "ResendAccessShareInvite", { share_id: shareId });

      if (resp.success) {
        update((s) => ({
          ...s,
          teammates: s.teammates.map((t) =>
            t.share_id === shareId
              ? { ...t, resend_count: t.resend_count + 1, last_resend_at: new Date().toISOString() }
              : t
          ),
        }));
      }
      return {
        success: resp.success,
        message: resp.message,
        retryAfter: resp.retry_after_seconds ?? 0,
      };
    } catch (err: any) {
      return { success: false, message: err.message ?? "Failed to resend invite", retryAfter: 0 };
    }
  }

  // Load pending invites (current user as invitee)
  async function loadPendingInvites() {
    update((s) => ({ ...s, isLoading: true, error: null }));
    try {
      const resp = await wsStore.callGRPC<{ pending_shares: any[] }>(
        "TenantPortalService", "GetPendingShares", {}
      );
      const pending_invites = (resp.pending_shares ?? []).map(toShare);
      update((s) => ({ ...s, pending_invites, isLoading: false }));
      return { success: true, pending_invites };
    } catch (err: any) {
      const error = err.message ?? "Failed to load invites";
      update((s) => ({ ...s, error, isLoading: false }));
      return { success: false, error };
    }
  }

  // Accept an invite by token or share_id
  async function acceptInvite(params: { invite_token?: string; share_id?: string }) {
    try {
      const resp = await wsStore.callGRPC<{
        success: boolean;
        message: string;
        share: any;
      }>("TenantPortalService", "AcceptAccessShare", params);

      if (resp.success) {
        update((s) => ({
          ...s,
          pending_invites: s.pending_invites.filter(
            (i) => i.share_id !== params.share_id && i.invite_token !== params.invite_token
          ),
        }));
        // Refresh memberships so the new account shows up, then force-refresh
        // resource lists so shared devices appear immediately without a page reload.
        await loadMemberships();
        await refreshSharedResources();
      }
      return { success: resp.success, message: resp.message };
    } catch (err: any) {
      return { success: false, message: err.message ?? "Failed to accept invite" };
    }
  }

  // Reject/decline an invite
  async function rejectInvite(inviteToken: string) {
    try {
      const resp = await wsStore.callGRPC<{ success: boolean; message: string }>(
        "TenantPortalService", "RejectAccessShare", { invite_token: inviteToken }
      );
      if (resp.success) {
        update((s) => ({
          ...s,
          pending_invites: s.pending_invites.filter((i) => i.invite_token !== inviteToken),
        }));
      }
      return { success: resp.success, message: resp.message };
    } catch (err: any) {
      return { success: false, message: err.message ?? "Failed to reject invite" };
    }
  }

  // Load accounts the current user was invited to (memberships)
  async function loadMemberships() {
    try {
      const resp = await wsStore.callGRPC<{ accounts: any[] }>(
        "TenantPortalService", "ListAccessibleAccounts", {}
      );
      const memberships = (resp.accounts ?? []).map(toAccessibleAccount);
      update((s) => ({ ...s, memberships }));
      return { success: true, memberships };
    } catch (err: any) {
      return { success: false, error: err.message ?? "Failed to load memberships" };
    }
  }

  // Generate a QR code (base64 PNG) for a given invite URL
  async function generateQRCode(inviteUrl: string): Promise<string> {
    try {
      const resp = await wsStore.callGRPC<{ qr_code: string }>(
        "TenantPortalService", "GenerateShareQR", { invite_url: inviteUrl }
      );
      return resp.qr_code ?? "";
    } catch {
      return "";
    }
  }

  // Get invite details by token (public — works before login)
  async function getInviteDetails(token: string): Promise<InviteDetails | null> {
    try {
      const resp = await wsStore.callGRPC<any>(
        "TenantPortalService", "GetAccessShareByToken", { invite_token: token }
      );
      return {
        valid:         resp.valid         ?? false,
        owner_email:   resp.owner_email   ?? "",
        owner_name:    resp.owner_name    ?? "",
        share_id:      resp.share_id      ?? "",
        permissions:   resp.permissions   ?? null,
        tag_filter:    resp.tag_filter    ?? [],
        status:        resp.status        ?? "expired",
        expires_at:    resp.expires_at    ?? undefined,
        is_link_share: resp.is_link_share ?? resp.isLinkShare ?? false,
      };
    } catch {
      return null;
    }
  }

  // Load everything at once
  async function loadAll() {
    await Promise.all([listTeammates(), loadPendingInvites(), loadMemberships()]);
  }

  function reset() {
    set(initialState);
  }

  return {
    subscribe,
    listTeammates,
    inviteTeammate,
    createLinkShare,
    generateQRCode,
    revokeTeammate,
    resendInvite,
    loadPendingInvites,
    acceptInvite,
    rejectInvite,
    loadMemberships,
    getInviteDetails,
    loadAll,
    reset,
  };
}

export const teamStore = createTeamStore();

// Derived helpers
export const pendingInviteCount = derived(
  teamStore,
  ($s) => $s.pending_invites.length
);

export const hasTeamAccess = derived(
  teamStore,
  ($s) => $s.memberships.length > 0
);

export const canInviteMore = derived(
  teamStore,
  ($s) => $s.teammate_limit === 0 || $s.teammate_used < $s.teammate_limit
);
