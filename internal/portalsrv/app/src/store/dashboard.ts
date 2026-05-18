import { writable } from "svelte/store";
import { wsStore } from "./websocket";

// ============================================================================
// Type Definitions - Matches GetTenantDashboardResponse from proto
// ============================================================================

export interface DashboardStats {
  // Account overview
  tenant_id: string;
  name: string;
  status: string;

  // Usage stats
  peer_count: number;
  max_peers: number;
  block_count: number;
  rx_bytes: number;
  tx_bytes: number;
  online_peers: number;

  // Network details
  total_ips_available: number;
  ips_used: number;
  network_blocks: string[];

  // System metrics
  goroutine_count: number;
  cpu_usage_percent: number;
  memory_bytes: number;
}

export interface DashboardState {
  stats: DashboardStats | null;
  isLoading: boolean;
  error: string | null;
  lastUpdated: string | null;
}

// ============================================================================
// Store Creation
// ============================================================================

const initialState: DashboardState = {
  stats: null,
  isLoading: false,
  error: null,
  lastUpdated: null,
};

function createDashboardStore() {
  const { subscribe, set, update } = writable<DashboardState>(initialState);

  async function getDashboard(tenantId?: string) {
    update((s) => ({ ...s, isLoading: true, error: null }));
    try {
      const response = await wsStore.callGRPC<DashboardStats>(
        "TenantPortalService",
        "GetTenantDashboard",
        {
          tenant_id: tenantId || "",
        }
      );

      update((s) => ({
        ...s,
        stats: response,
        isLoading: false,
        lastUpdated: new Date().toLocaleTimeString(),
      }));
      return { success: true, stats: response };
    } catch (err: any) {
      const errorMsg = err.message || "Failed to get dashboard stats";
      update((s) => ({ ...s, error: errorMsg, isLoading: false }));
      return { success: false, error: errorMsg };
    }
  }

  function reset() {
    set(initialState);
  }

  return {
    subscribe,
    getDashboard,
    reset,
  };
}

export const dashboardStore = createDashboardStore();
