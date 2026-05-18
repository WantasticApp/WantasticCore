import { writable, derived, get } from "svelte/store";
import { wsStore } from "./websocket";

export interface WinboxAccount {
  id: string;
  peer_id: string;
  name: string;
  router_ip: string;
  router_port: number;
  access_token: string;
  password_token: string;
  enabled: boolean;
  allowed_client_ips?: string[];
  created_at?: number;
  updated_at?: number;
  is_shared?: boolean;
  owner_name?: string;
  viewer_can_write?: boolean;
}

export interface WinboxAccountState {
  accounts: WinboxAccount[];
  isLoading: boolean;
  error: string | null;
}

const initialState: WinboxAccountState = {
  accounts: [],
  isLoading: false,
  error: null,
};

function createWinboxAccountStore() {
  const { subscribe, set, update } = writable<WinboxAccountState>(initialState);

  async function listAccounts(): Promise<WinboxAccount[]> {
    update((s) => ({ ...s, isLoading: true, error: null }));
    try {
      const response = await wsStore.callGRPC<{ sessions: WinboxAccount[] }>(
        "TenantPeerService",
        "ListTenantWinboxSessions",
        { tenant_id: "" }
      );
      const accounts = (response.sessions || []).map((account: any) => {
        const createdAt =
          typeof account.created_at === "object" &&
          typeof account.created_at?.seconds === "number"
            ? account.created_at.seconds * 1000
            : account.created_at;
        const updatedAt =
          typeof account.updated_at === "object" &&
          typeof account.updated_at?.seconds === "number"
            ? account.updated_at.seconds * 1000
            : account.updated_at;

        return {
          ...account,
          created_at: createdAt,
          updated_at: updatedAt,
          is_shared: account.is_shared ?? account.isShared ?? false,
          owner_name: account.owner_name || account.ownerName || "",
          viewer_can_write:
            account.viewer_can_write ?? account.viewerCanWrite ?? false,
        };
      });
      update((s) => ({ ...s, accounts, isLoading: false }));
      return accounts;
    } catch (err: any) {
      update((s) => ({ ...s, error: err.message, isLoading: false }));
      throw err;
    }
  }

  async function createAccount(data: {
    peer_id: string;
    name: string;
    router_ip: string;
    router_port?: number;
    mikrotik_username: string;
    mikrotik_password: string;
    allowed_client_ips?: string[];
  }): Promise<WinboxAccount> {
    update((s) => ({ ...s, isLoading: true, error: null }));
    try {
      const response = await wsStore.callGRPC<{
        session: {
          id: string;
          access_token: string;
          password_token: string;
        };
        access_token: string;
        password_token: string;
      }>("TenantPeerService", "CreateTenantWinboxSession", {
        tenant_id: "",
        peer_id: data.peer_id,
        name: data.name,
        router_ip: data.router_ip,
        port: data.router_port || 8291,
        username: data.mikrotik_username,
        password: data.mikrotik_password,
        allowed_client_ips: data.allowed_client_ips || [],
      });

      const newAccount: WinboxAccount = {
        id: response.session?.id || "",
        peer_id: data.peer_id,
        name: data.name,
        router_ip: data.router_ip,
        router_port: data.router_port || 8291,
        access_token:
          response.access_token || response.session?.access_token || "",
        password_token:
          response.password_token || response.session?.password_token || "",
        enabled: true,
        allowed_client_ips: data.allowed_client_ips,
        created_at: Date.now(),
      };

      update((s) => ({
        ...s,
        accounts: [...s.accounts, newAccount],
        isLoading: false,
      }));

      return newAccount;
    } catch (err: any) {
      update((s) => ({ ...s, error: err.message, isLoading: false }));
      throw err;
    }
  }

  async function updateAccount(
    accountId: string,
    data: { enabled?: boolean; name?: string; allowed_client_ips?: string[] }
  ): Promise<void> {
    update((s) => ({ ...s, isLoading: true, error: null }));
    try {
      const currentState = get({ subscribe });
      const account = currentState.accounts.find((a) => a.id === accountId);
      const peerId = account?.peer_id || "";

      await wsStore.callGRPC("TenantPeerService", "UpdateTenantWinboxSession", {
        tenant_id: "",
        peer_id: peerId,
        session_id: accountId,
        ...data,
      });

      update((s) => ({
        ...s,
        accounts: s.accounts.map((a) =>
          a.id === accountId ? { ...a, ...data, updated_at: Date.now() } : a
        ),
        isLoading: false,
      }));
    } catch (err: any) {
      update((s) => ({ ...s, error: err.message, isLoading: false }));
      throw err;
    }
  }

  async function deleteAccount(accountId: string): Promise<void> {
    update((s) => ({ ...s, isLoading: true, error: null }));
    try {
      const currentState = get({ subscribe });
      const account = currentState.accounts.find((a) => a.id === accountId);
      const peerId = account?.peer_id || "";

      if (account?.is_shared) {
        throw new Error("Delete is not allowed on shared Winbox sessions");
      }

      await wsStore.callGRPC("TenantPeerService", "DeleteTenantWinboxSession", {
        tenant_id: "",
        peer_id: peerId,
        session_id: accountId,
      });

      update((s) => ({
        ...s,
        accounts: s.accounts.filter((a) => a.id !== accountId),
        isLoading: false,
      }));
    } catch (err: any) {
      update((s) => ({ ...s, error: err.message, isLoading: false }));
      throw err;
    }
  }

  function clearError(): void {
    update((s) => ({ ...s, error: null }));
  }

  // Derived stores
  const accounts = derived({ subscribe }, (s) => s.accounts);
  const isLoading = derived({ subscribe }, (s) => s.isLoading);
  const error = derived({ subscribe }, (s) => s.error);
  const enabledAccounts = derived({ subscribe }, (s) =>
    s.accounts.filter((a) => a.enabled)
  );

  return {
    subscribe,
    listAccounts,
    createAccount,
    updateAccount,
    deleteAccount,
    clearError,
    accounts,
    isLoading,
    error,
    enabledAccounts,
  };
}

export const winboxAccountStore = createWinboxAccountStore();
