import { writable, derived, get } from "svelte/store";
import { wsStore } from "./websocket";

// ============================================================================
// Type Definitions
// ============================================================================

export interface AccountInfo {
  id: string;
  name: string;
  email: string;
  fullName: string;
  phone?: string;
  company?: string;
  emailVerified: boolean;
  totpEnabled: boolean;
  createdAt: string;
  lastLogin?: string;
  preferredLanguage?: string; // ISO 639-1 language code (e.g., "en", "ar", "he")
  twoFAMethod?: "none" | "totp" | "email" | "sms" | "whatsapp";
}

export interface TOTPSetup {
  secret: string;
  provisioningUrl: string;
  qrCode: string;
}

export interface SessionInfo {
  sessionId: string;
  ipAddress: string;
  browser: string;
  browserVersion: string;
  os: string;
  deviceType: string;
  createdAt: string;
  lastActivity: string;
  expiresAt: string;
  isCurrent: boolean;
}

export interface AccountState {
  account: AccountInfo | null;
  totpSetup: TOTPSetup | null;
  sessions: SessionInfo[];
  isLoading: boolean;
  error: string | null;
}

// ============================================================================
// Store Creation
// ============================================================================

const initialState: AccountState = {
  account: null,
  totpSetup: null,
  sessions: [],
  isLoading: false,
  error: null,
};

export function createAccountStore() {
  const { subscribe, set, update } = writable<AccountState>(initialState);

  async function getAccount(tenantId?: string) {
    update((s) => ({ ...s, isLoading: true, error: null }));
    try {
      const response = await wsStore.callGRPC<{
        tenant_id?: string;
        tenant_name?: string;
        email?: string;
        full_name?: string;
        phone?: string;
        company?: string;
        email_verified?: boolean;
        totp_enabled?: boolean;
        created_at?: string;
        last_login?: string;
        preferred_language?: string;
        two_fa_method?: string;
        account?: any;
      }>("TenantPortalService", "GetTenantAccount", {
        tenant_id: tenantId || "",
      });

      const account: AccountInfo = {
        id: response.account?.id || response.tenant_id || "",
        name: response.account?.name || response.tenant_name || "",
        email: response.account?.email || response.email || "",
        fullName: response.account?.full_name || response.full_name || "",
        phone: response.account?.phone || response.phone,
        company: response.account?.company || response.company,
        emailVerified:
          response.account?.email_verified || response.email_verified || false,
        totpEnabled:
          response.account?.totp_enabled || response.totp_enabled || false,
        createdAt:
          response.account?.created_at ||
          response.created_at ||
          new Date().toISOString(),
        lastLogin: response.account?.last_login || response.last_login,
        preferredLanguage: response.preferred_language,
        twoFAMethod: (response.account?.two_fa_method ||
          response.two_fa_method ||
          "none") as any,
      };

      update((s) => ({ ...s, account, isLoading: false }));
      return { success: true, account };
    } catch (err: any) {
      const errorMsg = err.message || "Failed to get account";
      update((s) => ({ ...s, error: errorMsg, isLoading: false }));
      return { success: false, error: errorMsg };
    }
  }

  async function updateAccount(updates: Partial<AccountInfo>) {
    update((s) => ({ ...s, isLoading: true, error: null }));
    try {
      const response = await wsStore.callGRPC<{
        success?: boolean;
        message?: string;
      }>("TenantPortalService", "UpdateTenantProfile", {
        tenant_id: "",
        full_name: updates.fullName,
        phone: updates.phone,
        company: updates.company,
        preferred_language: updates.preferredLanguage,
      });

      // Update local state with the changes
      update((s) => ({
        ...s,
        account: s.account ? { ...s.account, ...updates } : null,
        isLoading: false,
      }));

      return { success: true };
    } catch (err: any) {
      const errorMsg = err.message || "Failed to update account";
      update((s) => ({ ...s, error: errorMsg, isLoading: false }));
      return { success: false, error: errorMsg };
    }
  }

  // Update just the preferred language (for i18n integration)
  async function updatePreferredLanguage(language: string) {
    try {
      await wsStore.callGRPC<{
        success?: boolean;
        message?: string;
      }>("TenantPortalService", "UpdateTenantProfile", {
        tenant_id: "",
        preferred_language: language,
      });

      // Update local state
      update((s) => ({
        ...s,
        account: s.account
          ? { ...s.account, preferredLanguage: language }
          : null,
      }));

      return { success: true };
    } catch (err: any) {
      console.error("Failed to update preferred language:", err);
      return { success: false, error: err.message };
    }
  }

  async function changePassword(currentPassword: string, newPassword: string) {
    update((s) => ({ ...s, isLoading: true, error: null }));
    try {
      await wsStore.callGRPC<{ success: boolean }>(
        "TenantPortalService",
        "ChangePassword",
        {
          tenant_id: "",
          current_password: currentPassword,
          new_password: newPassword,
        }
      );

      update((s) => ({ ...s, isLoading: false }));
      return { success: true };
    } catch (err: any) {
      const errorMsg = err.message || "Failed to change password";
      update((s) => ({ ...s, error: errorMsg, isLoading: false }));
      return { success: false, error: errorMsg };
    }
  }

  async function setupTOTP(): Promise<{
    success: boolean;
    setup?: TOTPSetup;
    error?: string;
  }> {
    try {
      const response = await wsStore.callGRPC<{
        secret?: string;
        totp_secret?: string;
        provisioning_url?: string;
        totp_provisioning_url?: string;
        qr_code?: string;
        success?: boolean;
      }>("TenantPortalService", "SetupTOTP", {
        tenant_id: "",
      });

      const setup: TOTPSetup = {
        secret: response.totp_secret || response.secret || "",
        provisioningUrl:
          response.totp_provisioning_url || response.provisioning_url || "",
        qrCode: response.qr_code || "",
      };

      update((s) => ({ ...s, totpSetup: setup }));
      return { success: true, setup };
    } catch (err: any) {
      return { success: false, error: err.message || "Failed to setup TOTP" };
    }
  }

  async function verifyTOTP(code: string, totpSecret?: string) {
    // Get the current state before starting
    let currentTotpSetup: TOTPSetup | null = null;
    const unsubscribe = subscribe((s) => {
      currentTotpSetup = s.totpSetup;
    });
    unsubscribe();

    update((s) => ({ ...s, isLoading: true, error: null }));
    try {
      // Get the secret from the current setup state if not provided
      const secret = totpSecret || currentTotpSetup?.secret;

      if (!secret) {
        throw new Error(
          "TOTP setup not found. Please restart the setup process."
        );
      }

      const response = await wsStore.callGRPC<{
        success: boolean;
        message?: string;
      }>("TenantPortalService", "SetTwoFAMethod", {
        method: "totp",
        totp_code: code,
        totp_secret: secret,
      });

      if (!response.success) {
        throw new Error(response.message || "Failed to verify TOTP");
      }

      update((s) => ({
        ...s,
        totpSetup: null,
        account: s.account
          ? { ...s.account, totpEnabled: true, twoFAMethod: "totp" }
          : null,
        isLoading: false,
      }));
      return { success: true };
    } catch (err: any) {
      const errorMsg = err.message || "Invalid TOTP code";
      update((s) => ({ ...s, error: errorMsg, isLoading: false }));
      return { success: false, error: errorMsg };
    }
  }

  async function disableTOTP(code: string) {
    return setup2FA("none");
  }

  async function setup2FA(method: "sms" | "email" | "none") {
    update((s) => ({ ...s, isLoading: true, error: null }));
    try {
      const response = await wsStore.callGRPC<{
        success: boolean;
        message?: string;
      }>("TenantPortalService", "SetTwoFAMethod", { method });

      if (!response.success) {
        throw new Error(response.message || "Failed to setup 2FA");
      }

      // Update local state based on method
      let updates: Partial<AccountInfo> = {};
      if (method === "none") {
        updates = { totpEnabled: false, twoFAMethod: "none" };
      } else {
        updates = { twoFAMethod: method };
      }

      update((s) => ({
        ...s,
        isLoading: false,
        account: s.account ? { ...s.account, ...updates } : null,
      }));

      return { success: true, result: response };
    } catch (err: any) {
      const errorMsg = err.message || "Failed to setup 2FA";
      update((s) => ({ ...s, error: errorMsg, isLoading: false }));
      return { success: false, error: errorMsg };
    }
  }

  async function getSessions() {
    update((s) => ({ ...s, isLoading: true, error: null }));
    try {
      const response = await wsStore.callGRPC<{
        sessions: Array<{
          session_id: string;
          ip_address: string;
          browser: string;
          browser_version: string;
          os: string;
          device_type: string;
          created_at: { seconds: number };
          last_activity: { seconds: number };
          expires_at: { seconds: number };
          is_current: boolean;
        }>;
        current_session_id: string;
      }>("TenantPortalService", "ListTenantSessions", {});

      const sessions: SessionInfo[] = (response.sessions || []).map((s) => ({
        sessionId: s.session_id,
        ipAddress: s.ip_address,
        browser: s.browser,
        browserVersion: s.browser_version,
        os: s.os,
        deviceType: s.device_type,
        createdAt: s.created_at?.seconds
          ? new Date(s.created_at.seconds * 1000).toISOString()
          : "",
        lastActivity: s.last_activity?.seconds
          ? new Date(s.last_activity.seconds * 1000).toISOString()
          : "",
        expiresAt: s.expires_at?.seconds
          ? new Date(s.expires_at.seconds * 1000).toISOString()
          : "",
        isCurrent: s.is_current,
      }));

      update((s) => ({ ...s, sessions, isLoading: false }));
      return { success: true, sessions };
    } catch (err: any) {
      const errorMsg = err.message || "Failed to load sessions";
      update((s) => ({ ...s, error: errorMsg, isLoading: false }));
      return { success: false, error: errorMsg };
    }
  }

  async function deleteSession(sessionId: string) {
    update((s) => ({ ...s, isLoading: true, error: null }));
    try {
      const response = await wsStore.callGRPC<{
        success: boolean;
        message: string;
      }>("TenantPortalService", "DeleteTenantSession", {
        session_id: sessionId,
      });

      if (response.success) {
        // Remove from local state
        update((s) => ({
          ...s,
          sessions: s.sessions.filter((sess) => sess.sessionId !== sessionId),
          isLoading: false,
        }));
        return { success: true, message: response.message };
      } else {
        update((s) => ({ ...s, error: response.message, isLoading: false }));
        return { success: false, error: response.message };
      }
    } catch (err: any) {
      const errorMsg = err.message || "Failed to delete session";
      update((s) => ({ ...s, error: errorMsg, isLoading: false }));
      return { success: false, error: errorMsg };
    }
  }

  // ============================================================================
  // Access Sharing Methods
  // ============================================================================

  // Convert snake_case proto response to camelCase for frontend
  function convertShareToCamelCase(share: any) {
    return {
      id: share.id,
      ownerTenantId: share.owner_tenant_id,
      ownerEmail: share.owner_email,
      ownerName: share.owner_name,
      sharedEmail: share.shared_email,
      shareeName: share.sharee_name,
      shareeTenantId: share.sharee_tenant_id,
      permissions: share.permissions
        ? {
            viewPeers: share.permissions.view_peers,
            managePeers: share.permissions.manage_peers,
            viewTopology: share.permissions.view_topology,
            manageWinbox: share.permissions.manage_winbox,
            manageWebssh: share.permissions.manage_webssh,
            viewAcl: share.permissions.view_acl,
            manageAcl: share.permissions.manage_acl,
            viewActivity: share.permissions.view_activity,
          }
        : null,
      status: share.status,
      createdAt: share.created_at,
      acceptedAt: share.accepted_at,
      expiresAt: share.expires_at,
      inviteToken: share.invite_token,
    };
  }

  async function listShares() {
    try {
      const response = await wsStore.callGRPC<{
        shares: any[];
      }>("TenantPortalService", "ListAccessShares", {});
      const shares = (response.shares || []).map(convertShareToCamelCase);
      return { success: true, shares };
    } catch (err: any) {
      return {
        success: false,
        error: err.message || "Failed to list shares",
        shares: [],
      };
    }
  }

  async function createShare(params: {
    shared_email: string;
    sharee_name: string;
    permissions: {
      owner_name: boolean;
      manage_peers: boolean;
      view_topology: boolean;
      view_peers: boolean;
      manage_winbox: boolean;
      manage_webssh: boolean;
      view_acl: boolean;
      manage_acl: boolean;
      view_activity: boolean;
    };
  }) {
    try {
      const response = await wsStore.callGRPC<{
        success: boolean;
        message: string;
        share: any;
      }>("TenantPortalService", "CreateAccessShare", {
        shared_email: params.shared_email,
        sharee_name: params.sharee_name,
        permissions: {
          view_peers: params.permissions.view_peers,
          manage_peers: params.permissions.manage_peers,
          view_topology: params.permissions.view_topology,
          manage_winbox: params.permissions.manage_winbox,
          manage_webssh: params.permissions.manage_webssh,
          view_acl: params.permissions.view_acl,
          manage_acl: params.permissions.manage_acl,
          view_activity: params.permissions.view_activity,
        },
      });
      return {
        success: response.success,
        message: response.message,
        share: response.share ? convertShareToCamelCase(response.share) : null,
      };
    } catch (err: any) {
      return {
        success: false,
        message: err.message || "Failed to create share",
      };
    }
  }

  async function deleteShare(shareId: string) {
    try {
      const response = await wsStore.callGRPC<{
        success: boolean;
        message: string;
      }>("TenantPortalService", "DeleteAccessShare", {
        share_id: shareId,
      });
      return { success: response.success, message: response.message };
    } catch (err: any) {
      return {
        success: false,
        message: err.message || "Failed to delete share",
      };
    }
  }

  async function getPendingShares() {
    try {
      const response = await wsStore.callGRPC<{
        pending_shares: any[];
      }>("TenantPortalService", "GetPendingShares", {});
      const pendingShares = (response.pending_shares || []).map(
        convertShareToCamelCase
      );
      return { success: true, pendingShares };
    } catch (err: any) {
      return {
        success: false,
        error: err.message || "Failed to get pending shares",
        pendingShares: [],
      };
    }
  }

  async function acceptShare(shareId: string, inviteToken: string) {
    try {
      const response = await wsStore.callGRPC<{
        success: boolean;
        message: string;
        share: any;
      }>("TenantPortalService", "AcceptAccessShare", {
        share_id: shareId,
        invite_token: inviteToken,
      });
      return {
        success: response.success,
        message: response.message,
        share: response.share ? convertShareToCamelCase(response.share) : null,
      };
    } catch (err: any) {
      return {
        success: false,
        message: err.message || "Failed to accept share",
      };
    }
  }

  // Convert accessible account from snake_case to camelCase
  function convertAccountToCamelCase(account: any) {
    return {
      tenantId: account.tenant_id,
      ownerEmail: account.owner_email,
      ownerName: account.owner_name,
      shareId: account.share_id,
      shareeName: account.sharee_name,
      permissions: account.permissions
        ? {
            viewPeers: account.permissions.view_peers,
            managePeers: account.permissions.manage_peers,
            viewTopology: account.permissions.view_topology,
            manageWinbox: account.permissions.manage_winbox,
            manageWebssh: account.permissions.manage_webssh,
            viewAcl: account.permissions.view_acl,
            manageAcl: account.permissions.manage_acl,
            viewActivity: account.permissions.view_activity,
          }
        : null,
      acceptedAt: account.accepted_at,
    };
  }

  async function getAccessibleAccounts() {
    try {
      const response = await wsStore.callGRPC<{
        accounts: any[];
      }>("TenantPortalService", "ListAccessibleAccounts", {});
      const accounts = (response.accounts || []).map(convertAccountToCamelCase);
      return { success: true, accounts };
    } catch (err: any) {
      return {
        success: false,
        error: err.message || "Failed to get accessible accounts",
        accounts: [],
      };
    }
  }

  async function getMCPConfig() {
    try {
      const response = await wsStore.callGRPC<{
        mcp_servers: any;
      }>("TenantPortalService", "GetMCPConfig", {});
      return { success: true, config: response };
    } catch (err: any) {
      return {
        success: false,
        error: err.message || "Failed to get MCP config",
      };
    }
  }

  async function listAPIKeys() {
    try {
      const response = await wsStore.callGRPC<{
        keys: any[];
      }>("TenantPortalService", "ListAPIKeys", {}); // Service name ignored by proxy for these custom handlers
      return { success: true, keys: response.keys || [] };
    } catch (err: any) {
      return {
        success: false,
        error: err.message || "Failed to list API keys",
      };
    }
  }

  async function createAPIKey(name: string) {
    try {
      const response = await wsStore.callGRPC<any>(
        "TenantPortalService",
        "CreateAPIKey",
        { name }
      );
      return { success: true, key: response };
    } catch (err: any) {
      return {
        success: false,
        error: err.message || "Failed to create API key",
      };
    }
  }

  async function revokeAPIKey(id: string) {
    try {
      await wsStore.callGRPC<{ success: boolean }>(
        "TenantPortalService",
        "RevokeAPIKey",
        { id }
      );
      return { success: true };
    } catch (err: any) {
      return {
        success: false,
        error: err.message || "Failed to revoke API key",
      };
    }
  }

  async function deleteAccount(password: string) {
    update((s) => ({ ...s, isLoading: true, error: null }));
    try {
      await wsStore.callGRPC<{ success: boolean }>(
        "TenantPortalService",
        "DeleteAccount",
        { password }
      );

      set(initialState);
      return { success: true };
    } catch (err: any) {
      const errorMsg = err.message || "Failed to delete account";
      update((s) => ({ ...s, error: errorMsg, isLoading: false }));
      return { success: false, error: errorMsg };
    }
  }

  const accountName = derived({ subscribe }, (s) => s.account?.name || "");
  const accountEmail = derived({ subscribe }, (s) => s.account?.email || "");
  const isTOTPEnabled = derived(
    { subscribe },
    (s) => s.account?.totpEnabled || false
  );
  const preferredLanguage = derived(
    { subscribe },
    (s) => s.account?.preferredLanguage || ""
  );

  return {
    subscribe,
    getAccount,
    updateAccount,
    updatePreferredLanguage,
    changePassword,
    setupTOTP,
    verifyTOTP,
    disableTOTP,
    setup2FA,
    getSessions,
    deleteSession,
    deleteAccount,
    // Access sharing
    listShares,
    createShare,
    deleteShare,
    getPendingShares,
    acceptShare,
    getAccessibleAccounts,
    getMCPConfig,
    listAPIKeys,
    createAPIKey,
    revokeAPIKey,
    // Derived stores
    accountName,
    accountEmail,
    isTOTPEnabled,
    preferredLanguage,
  };
}

export const accountStore = createAccountStore();
