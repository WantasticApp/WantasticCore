import { writable, derived } from "svelte/store";
import { wsStore } from "./websocket";

// ============================================================================
// Types
// ============================================================================

export interface WUSPDeviceState {
  id: string;
  peer_id: string;
  account_id: string;
  last_sync_at: number;
  sync_error: string;
  device_snapshot: string | null; // JSON array of {path, value}
  device_id: string;
  manufacturer: string;
  product_class: string;
  serial_number: string;
  software_version: string;
  hardware_version: string;
  wusp_enable: boolean;
  wusp_status: string;
  wusp_version: string;
}

export interface WUSPParam {
  path: string;
  value: string;
}

export interface SnapshotField {
  path: string;
  value: string;
  access?: string; // "readOnly" | "readWrite"
}

export interface WUSPState {
  // Device state from DB
  deviceState: WUSPDeviceState | null;
  // Parsed snapshot fields (from device_snapshot JSON)
  snapshot: SnapshotField[];
  // Live Get results (from SendGet)
  liveParams: WUSPParam[];
  // UI state
  isLoading: boolean;
  isSyncing: boolean;
  isSetting: boolean;
  error: string | null;
  lastSyncTime: number | null;
}

// ============================================================================
// Store
// ============================================================================

function createWuspStore() {
  const initial: WUSPState = {
    deviceState: null,
    snapshot: [],
    liveParams: [],
    isLoading: false,
    isSyncing: false,
    isSetting: false,
    error: null,
    lastSyncTime: null,
  };

  const { subscribe, set, update } = writable<WUSPState>(initial);

  return {
    subscribe,

    /** Load cached device state from DB (fast, no round-trip to device) */
    async getDeviceState(peerId: string) {
      update((s) => ({ ...s, isLoading: true, error: null }));
      try {
        const resp = await wsStore.callGRPC<{ state: WUSPDeviceState }>(
          "WUSPService",
          "GetDeviceState",
          { peer_id: peerId },
        );
        const state = resp.state;
        let snapshot: SnapshotField[] = [];
        if (state?.device_snapshot) {
          try {
            const raw = state.device_snapshot;
            let jsonStr: string;
            if (typeof raw === "string") {
              // protojson encodes bytes as base64 — decode first
              try { jsonStr = atob(raw); } catch { jsonStr = raw; }
            } else {
              jsonStr = new TextDecoder().decode(raw as any);
            }
            snapshot = JSON.parse(jsonStr);
            if (!Array.isArray(snapshot)) snapshot = [];
          } catch {
            snapshot = [];
          }
        }
        update((s) => ({
          ...s,
          isLoading: false,
          deviceState: state,
          snapshot,
          lastSyncTime: state?.last_sync_at ? state.last_sync_at * 1000 : null,
        }));
      } catch (e: any) {
        update((s) => ({
          ...s,
          isLoading: false,
          error: e.message || "Failed to load device state",
        }));
      }
    },

    /** Force live sync from device (round-trip via WireGuard) */
    async syncDevice(peerId: string, accountId: string) {
      update((s) => ({ ...s, isSyncing: true, error: null }));
      try {
        const resp = await wsStore.callGRPC<{
          success: boolean;
          error?: string;
          state: WUSPDeviceState;
        }>("WUSPService", "SyncDeviceState", {
          peer_id: peerId,
          account_id: accountId,
        });
        if (!resp.success) throw new Error(resp.error || "Sync failed");
        const state = resp.state;
        let snapshot: SnapshotField[] = [];
        if (state?.device_snapshot) {
          try {
            const raw = state.device_snapshot;
            let jsonStr: string;
            if (typeof raw === "string") {
              // protojson encodes bytes as base64 — decode first
              try { jsonStr = atob(raw); } catch { jsonStr = raw; }
            } else {
              jsonStr = new TextDecoder().decode(raw as any);
            }
            snapshot = JSON.parse(jsonStr);
            if (!Array.isArray(snapshot)) snapshot = [];
          } catch {
            snapshot = [];
          }
        }
        update((s) => ({
          ...s,
          isSyncing: false,
          deviceState: state,
          snapshot,
          lastSyncTime: Date.now(),
        }));
      } catch (e: any) {
        update((s) => ({ ...s, isSyncing: false, error: e.message }));
      }
    },

    /** Read live params from device (bypasses DB cache) */
    async sendGet(peerId: string, paths: string[]) {
      update((s) => ({ ...s, isLoading: true, error: null }));
      try {
        const resp = await wsStore.callGRPC<{
          success: boolean;
          error?: string;
          params: WUSPParam[];
        }>("WUSPService", "SendGet", {
          peer_id: peerId,
          paths,
        });
        if (!resp.success) throw new Error(resp.error || "Get failed");
        update((s) => ({
          ...s,
          isLoading: false,
          liveParams: resp.params ?? [],
        }));
        return resp.params ?? [];
      } catch (e: any) {
        update((s) => ({ ...s, isLoading: false, error: e.message }));
        return [];
      }
    },

    /** Write params to device */
    async sendSet(
      peerId: string,
      params: WUSPParam[],
    ): Promise<boolean> {
      update((s) => ({ ...s, isSetting: true, error: null }));
      try {
        const resp = await wsStore.callGRPC<{
          success: boolean;
          error?: string;
        }>("WUSPService", "SendSet", {
          peer_id: peerId,
          params,
        });
        if (!resp.success) throw new Error(resp.error || "Set failed");
        update((s) => ({ ...s, isSetting: false }));
        return true;
      } catch (e: any) {
        update((s) => ({ ...s, isSetting: false, error: e.message }));
        return false;
      }
    },

    /** Execute an Operate command on the device */
    async sendOperate(
      peerId: string,
      commandPath: string,
      inputParams?: WUSPParam[],
    ) {
      update((s) => ({ ...s, isLoading: true, error: null }));
      try {
        const resp = await wsStore.callGRPC<{
          success: boolean;
          error?: string;
          output_params?: WUSPParam[];
        }>("WUSPService", "SendOperate", {
          peer_id: peerId,
          command_path: commandPath,
          input_params: inputParams ?? [],
        });
        if (!resp.success) throw new Error(resp.error || "Operate failed");
        update((s) => ({ ...s, isLoading: false }));
        return resp.output_params ?? [];
      } catch (e: any) {
        update((s) => ({ ...s, isLoading: false, error: e.message }));
        return [];
      }
    },

    /** Create a new object instance on the device */
    async sendAdd(
      peerId: string,
      objectPath: string,
      params?: WUSPParam[],
    ): Promise<{ instancePath: string; createdPaths: string[] } | null> {
      update((s) => ({ ...s, isLoading: true, error: null }));
      try {
        const resp = await wsStore.callGRPC<{
          success: boolean;
          error?: string;
          instance_path: string;
          created_paths: string[];
        }>("WUSPService", "SendAdd", {
          peer_id: peerId,
          object_path: objectPath,
          params: params ?? [],
        });
        if (!resp.success) throw new Error(resp.error || "Add failed");
        update((s) => ({ ...s, isLoading: false }));
        return { instancePath: resp.instance_path, createdPaths: resp.created_paths ?? [] };
      } catch (e: any) {
        update((s) => ({ ...s, isLoading: false, error: e.message }));
        return null;
      }
    },

    /** Delete object instances or parameters from the device */
    async sendDelete(peerId: string, paths: string[]): Promise<boolean> {
      update((s) => ({ ...s, isLoading: true, error: null }));
      try {
        const resp = await wsStore.callGRPC<{
          success: boolean;
          error?: string;
        }>("WUSPService", "SendDelete", {
          peer_id: peerId,
          paths,
        });
        if (!resp.success) throw new Error(resp.error || "Delete failed");
        update((s) => ({ ...s, isLoading: false }));
        return true;
      } catch (e: any) {
        update((s) => ({ ...s, isLoading: false, error: e.message }));
        return false;
      }
    },

    /** Clear current state (when switching peers) */
    reset() {
      set(initial);
    },

    clearError() {
      update((s) => ({ ...s, error: null }));
    },

    /**
     * Apply a single live ValueChange Notify event from the agent.
     * Called by the WS event listener (peer.ts → wuspStore.applyNotify).
     *
     * Only mutates state when the event's peerId matches the currently-loaded
     * device — switching peers wipes via reset(), and stale events for an old
     * peer are discarded so we never paint another device's value into the
     * UI by accident.
     */
    applyNotify(peerId: string, path: string, value: string) {
      update((s) => {
        if (!s.deviceState || s.deviceState.peer_id !== peerId) return s;
        let mutated = false;
        const snapshot = s.snapshot.map((f) => {
          if (f.path === path) {
            mutated = true;
            return { ...f, value };
          }
          return f;
        });
        // If the path wasn't in our snapshot yet (e.g. dynamic instance),
        // append it so the dashboard learns about it on the fly.
        if (!mutated && path) {
          snapshot.push({ path, value });
          mutated = true;
        }
        if (!mutated) return s;
        return { ...s, snapshot, lastSyncTime: Date.now() };
      });
    },
  };
}

export const wuspStore = createWuspStore();

// Wire wusp_notify events from the WebSocket ring buffer into wuspStore.
// The WS proxy forwards every Redis-published WUSP event as
//   { type: "peer_event", payload: { type: "wusp_notify", peerId, data: { ... } } }
// — see redis_handler.handleRedisMessage on the server side. We watch the
// last event index so we never replay older events on subscriber re-init.
let _lastWuspEventCount = 0;
wsStore.subscribe((wsState) => {
  if (!wsState.events || wsState.events.length === _lastWuspEventCount) {
    _lastWuspEventCount = wsState.events?.length ?? 0;
    return;
  }
  // Walk only the new tail.
  for (let i = _lastWuspEventCount; i < wsState.events.length; i++) {
    const ev = wsState.events[i];
    if (!ev || ev.type !== "wusp_notify") continue;
    const data = (ev as any).data || {};
    const path: string = data.obj_path || data.path || "";
    const value: string = data.param_value ?? data.value ?? "";
    if (!path) continue;
    wuspStore.applyNotify(ev.peerId, path, String(value));
  }
  _lastWuspEventCount = wsState.events.length;
});

// ============================================================================
// Derived stores for convenience
// ============================================================================

/** Snapshot fields grouped by top-level object (Device.DeviceInfo., Device.WiFi., etc.) */
export const snapshotSections = derived(wuspStore, ($s) => {
  const sections: Record<string, SnapshotField[]> = {};
  for (const field of $s.snapshot) {
    // Extract section: "Device.DeviceInfo.Manufacturer" → "Device.DeviceInfo"
    const parts = field.path.split(".");
    const section = parts.length >= 3 ? parts.slice(0, 2).join(".") : parts[0];
    if (!sections[section]) sections[section] = [];
    sections[section].push(field);
  }
  return sections;
});

/** Is the device WUSP-enabled and synced? */
export const isWuspActive = derived(
  wuspStore,
  ($s) => $s.deviceState?.wusp_enable === true,
);

/** All known paths from current snapshot — used for autocomplete */
export const knownPaths = derived(wuspStore, ($s) =>
  $s.snapshot.map((f) => f.path).sort(),
);

/** Search snapshot fields by partial path or value match */
export function searchSnapshot(
  snapshot: SnapshotField[],
  query: string,
): SnapshotField[] {
  if (!query || query.length < 2) return snapshot;
  const q = query.toLowerCase();
  return snapshot.filter(
    (f) =>
      f.path.toLowerCase().includes(q) ||
      f.value.toLowerCase().includes(q),
  );
}

/** Common object-path prefixes for quick navigation */
export const WUSP_SECTIONS = [
  { label: "Device Info", path: "Device.DeviceInfo.", icon: "info" },
  { label: "Cellular", path: "Device.Cellular.", icon: "radio" },
  { label: "WiFi", path: "Device.WiFi.", icon: "wifi" },
  { label: "IP", path: "Device.IP.", icon: "globe" },
  { label: "Firewall", path: "Device.Firewall.", icon: "shield" },
  { label: "Time", path: "Device.Time.", icon: "clock" },
  { label: "WUSP", path: "Device.WUSP.", icon: "layers" },
] as const;
