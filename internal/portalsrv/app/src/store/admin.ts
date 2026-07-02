import { writable, derived } from "svelte/store";
import { wsStore } from "./websocket";

// Mirrors internal/admin/service.go TenantSummary
export interface AdminTenant {
  id: string;
  email: string;
  full_name: string;
  status: string;
  is_admin: boolean;
  account_id: string;
  max_peers: number;
  peer_count: number;
  last_login: string;
  created_at: string;
}

export type AdminAccount = AdminTenant;

export interface AdminStats {
  total_accounts?: number;
  active_accounts?: number;
  total_peers?: number;
  online_peers?: number;
}

export interface AdminState {
  tenants: AdminTenant[];
  isLoading: boolean;
  error: string | null;
}

const initialState: AdminState = {
  tenants: [],
  isLoading: false,
  error: null,
};

function createAdminStore() {
  const { subscribe, update } = writable<AdminState>(initialState);

  async function listTenants(): Promise<AdminTenant[]> {
    update((s) => ({ ...s, isLoading: true, error: null }));
    try {
      const response = await wsStore.callGRPC<{ tenants: AdminTenant[] }>(
        "AdminService",
        "ListTenants",
        {},
      );
      const tenants = response.tenants || [];
      update((s) => ({ ...s, tenants, isLoading: false }));
      return tenants;
    } catch (err: any) {
      update((s) => ({ ...s, error: err.message, isLoading: false }));
      throw err;
    }
  }

  async function createTenant(input: {
    email: string;
    full_name: string;
    password: string;
    max_peers: number;
    is_admin?: boolean;
  }): Promise<AdminTenant> {
    update((s) => ({ ...s, isLoading: true, error: null }));
    try {
      const created = await wsStore.callGRPC<AdminTenant>(
        "AdminService",
        "CreateTenant",
        input,
      );
      update((s) => ({
        ...s,
        tenants: [...s.tenants, summarizeFromTenant(created)],
        isLoading: false,
      }));
      return created;
    } catch (err: any) {
      update((s) => ({ ...s, error: err.message, isLoading: false }));
      throw err;
    }
  }

  async function deleteTenant(id: string): Promise<void> {
    update((s) => ({ ...s, isLoading: true, error: null }));
    try {
      await wsStore.callGRPC<{ ok: boolean }>("AdminService", "DeleteTenant", { id });
      update((s) => ({
        ...s,
        tenants: s.tenants.filter((t) => t.id !== id),
        isLoading: false,
      }));
    } catch (err: any) {
      update((s) => ({ ...s, error: err.message, isLoading: false }));
      throw err;
    }
  }

  async function setMaxPeers(id: string, max_peers: number): Promise<void> {
    try {
      await wsStore.callGRPC<{ ok: boolean }>("AdminService", "SetTenantMaxPeers", {
        id,
        max_peers,
      });
      update((s) => ({
        ...s,
        tenants: s.tenants.map((t) => (t.id === id ? { ...t, max_peers } : t)),
      }));
    } catch (err: any) {
      update((s) => ({ ...s, error: err.message }));
      throw err;
    }
  }

  async function setPassword(id: string, password: string): Promise<void> {
    await wsStore.callGRPC<{ ok: boolean }>("AdminService", "SetTenantPassword", {
      id,
      password,
    });
  }

  async function setAdmin(id: string, is_admin: boolean): Promise<void> {
    try {
      await wsStore.callGRPC<{ ok: boolean }>("AdminService", "SetTenantAdmin", {
        id,
        is_admin,
      });
      update((s) => ({
        ...s,
        tenants: s.tenants.map((t) => (t.id === id ? { ...t, is_admin } : t)),
      }));
    } catch (err: any) {
      update((s) => ({ ...s, error: err.message }));
      throw err;
    }
  }

  async function setStatus(id: string, status: string): Promise<void> {
    try {
      await wsStore.callGRPC<{ ok: boolean }>("AdminService", "SetTenantStatus", {
        id,
        status,
      });
      update((s) => ({
        ...s,
        tenants: s.tenants.map((t) => (t.id === id ? { ...t, status } : t)),
      }));
    } catch (err: any) {
      update((s) => ({ ...s, error: err.message }));
      throw err;
    }
  }

  // The CreateTenant response is the full Tenant struct (not a Summary), so
  // shape it into the same row format the table renders.
  function summarizeFromTenant(t: any): AdminTenant {
    return {
      id: t.id ?? "",
      email: t.email ?? "",
      full_name: t.full_name ?? "",
      status: t.status ?? "active",
      is_admin: !!t.is_admin,
      account_id: t.overlay_account_id ?? "",
      max_peers: t.max_peers ?? 0,
      peer_count: 0,
      last_login: t.last_login ?? "",
      created_at: t.created_at ?? "",
    };
  }

  const tenants = derived({ subscribe }, (s) => s.tenants);
  const isLoading = derived({ subscribe }, (s) => s.isLoading);
  const error = derived({ subscribe }, (s) => s.error);

  return {
    subscribe,
    listTenants,
    createTenant,
    deleteTenant,
    setMaxPeers,
    setPassword,
    setAdmin,
    setStatus,
    tenants,
    isLoading,
    error,
  };
}

export const adminStore = createAdminStore();
