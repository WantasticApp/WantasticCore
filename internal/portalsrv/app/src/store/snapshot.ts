import { writable } from "svelte/store";
import { wsStore } from "./websocket";

// ============================================================================
// Types
// ============================================================================

export interface DeviceSnapshot {
  id: string;
  account_id: string;
  name: string;
  protocol: string; // "wusp", "mikrotik", etc.
  manufacturer: string;
  product_class: string;
  serial_number: string;
  software_version: string;
  hardware_version: string;
  device_snapshot: Uint8Array | null; // WUSP TR-181 JSON
  created_at: number; // Unix seconds
  updated_at: number;
  // MikroTik /export RouterOS script — set when has_backup is true.
  // Listings carry only the size; the body is fetched via /api/snapshot/download.
  backup_name?: string;
  backup_size?: number;
  has_backup?: boolean;
}

export interface SnapshotState {
  snapshots: DeviceSnapshot[];
  isLoading: boolean;
  isSaving: boolean;
  isProvisioning: boolean;
  error: string | null;
}

// ============================================================================
// Store
// ============================================================================

function createSnapshotStore() {
  const initialState: SnapshotState = {
    snapshots: [],
    isLoading: false,
    isSaving: false,
    isProvisioning: false,
    error: null,
  };

  const { subscribe, set, update } = writable<SnapshotState>(initialState);

  return {
    subscribe,

    async list(protocol?: string) {
      update((s) => ({ ...s, isLoading: true, error: null }));
      try {
        const resp = await wsStore.callGRPC<{ snapshots: DeviceSnapshot[] }>(
          "WUSPService",
          "ListSnapshots",
          { protocol: protocol || "" },
        );
        update((s) => ({ ...s, isLoading: false, snapshots: resp.snapshots ?? [] }));
      } catch (e: any) {
        update((s) => ({ ...s, isLoading: false, error: e.message || "Failed to list snapshots" }));
      }
    },

    async create(peerId: string, name: string, protocol = "wusp"): Promise<DeviceSnapshot | null> {
      update((s) => ({ ...s, isSaving: true, error: null }));
      try {
        const resp = await wsStore.callGRPC<{ success: boolean; error?: string; snapshot: DeviceSnapshot }>(
          "WUSPService",
          "CreateSnapshot",
          { peer_id: peerId, name, protocol },
        );
        if (!resp.success) throw new Error(resp.error || "Failed to create snapshot");
        update((s) => ({
          ...s,
          isSaving: false,
          snapshots: [resp.snapshot, ...s.snapshots],
        }));
        return resp.snapshot;
      } catch (e: any) {
        update((s) => ({ ...s, isSaving: false, error: e.message }));
        return null;
      }
    },

    async update(id: string, name: string, peerId?: string): Promise<boolean> {
      update((s) => ({ ...s, isSaving: true, error: null }));
      try {
        const resp = await wsStore.callGRPC<{ success: boolean; error?: string; snapshot: DeviceSnapshot }>(
          "WUSPService",
          "UpdateSnapshot",
          { id, name, peer_id: peerId || "" },
        );
        if (!resp.success) throw new Error(resp.error || "Failed to update snapshot");
        update((s) => ({
          ...s,
          isSaving: false,
          snapshots: s.snapshots.map((snap) =>
            snap.id === id ? resp.snapshot : snap,
          ),
        }));
        return true;
      } catch (e: any) {
        update((s) => ({ ...s, isSaving: false, error: e.message }));
        return false;
      }
    },

    async delete(id: string): Promise<boolean> {
      update((s) => ({ ...s, error: null }));
      try {
        const resp = await wsStore.callGRPC<{ success: boolean; error?: string }>(
          "WUSPService",
          "DeleteSnapshot",
          { id },
        );
        if (!resp.success) throw new Error(resp.error || "Failed to delete snapshot");
        update((s) => ({
          ...s,
          snapshots: s.snapshots.filter((snap) => snap.id !== id),
        }));
        return true;
      } catch (e: any) {
        update((s) => ({ ...s, error: e.message }));
        return false;
      }
    },

    async provision(peerId: string, snapshotId: string): Promise<boolean> {
      update((s) => ({ ...s, isProvisioning: true, error: null }));
      try {
        const resp = await wsStore.callGRPC<{ success: boolean; error?: string }>(
          "WUSPService",
          "ProvisionDevice",
          { peer_id: peerId, snapshot_id: snapshotId },
        );
        if (!resp.success) throw new Error(resp.error || "Provisioning failed");
        update((s) => ({ ...s, isProvisioning: false }));
        return true;
      } catch (e: any) {
        update((s) => ({ ...s, isProvisioning: false, error: e.message }));
        return false;
      }
    },

    /** Generate an upload token for a snapshot (enables MikroTik backup upload). */
    async generateUploadToken(snapshotId: string): Promise<{ token: string; url: string } | null> {
      try {
        const resp = await wsStore.callGRPC<{
          success: boolean;
          error?: string;
          upload_token: string;
          upload_url: string;
        }>("WUSPService", "GenerateUploadToken", { snapshot_id: snapshotId });
        if (!resp.success) throw new Error(resp.error || "Failed to generate token");
        return { token: resp.upload_token, url: resp.upload_url };
      } catch (e: any) {
        update((s) => ({ ...s, error: e.message }));
        return null;
      }
    },

    /** Generate a MikroTik RouterOS script for auto-backup upload. */
    generateMikroTikScript(uploadUrl: string, snapshotName: string): string {
      return [
        `# Wantastic Auto-Backup Script`,
        `# Snapshot: ${snapshotName}`,
        `# Generated: ${new Date().toISOString()}`,
        `# Install: paste into System > Scripts, then add a Scheduler`,
        ``,
        `:local exportFile "wantastic-export"`,
        `:local uploadUrl "${uploadUrl}"`,
        ``,
        `# Export configuration (plain text, no encryption)`,
        `/export file=$exportFile`,
        `:delay 2s`,
        ``,
        `# Upload to Wantastic portal`,
        `:do {`,
        `  /tool fetch url=$uploadUrl mode=https \\`,
        `    upload=yes src-path=("$exportFile.rsc") \\`,
        `    dst-path=""`,
        `  :log info "Wantastic: backup uploaded successfully"`,
        `} on-error={`,
        `  :log warning "Wantastic: backup upload failed"`,
        `}`,
        ``,
        `# Cleanup`,
        `/file remove [find name~"$exportFile"]`,
      ].join("\n");
    },

    clearError() {
      update((s) => ({ ...s, error: null }));
    },
  };
}

export const snapshotStore = createSnapshotStore();
