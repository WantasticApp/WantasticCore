import { writable, derived } from "svelte/store";
import { wsStore } from "./websocket";
import { formatLocalDateTime } from "$lib/dateUtils";

// ============================================================================
// Type Definitions
// ============================================================================

export interface BackupInfo {
  backup_id: string;
  status: "pending" | "processing" | "ready" | "error" | "failed";
  size_bytes: number;
  created_at: number; // Unix timestamp
  expires_at?: number;
  message?: string;
}

export interface RestoreStatus {
  restore_id: string;
  status: "pending" | "in_progress" | "completed" | "failed";
  progress_percent: number;
  message: string;
  started_at?: number;
  completed_at?: number;
}

export interface BackupState {
  backups: BackupInfo[];
  maxBackups: number;
  activeRestoreId: string | null;
  restoreStatus: RestoreStatus | null;
  isLoading: boolean;
  isCreating: boolean;
  isRestoring: boolean;
  error: string | null;
  statusMessage: string | null;
  statusType: "info" | "success" | "error" | "warning" | null;
}

// ============================================================================
// Store Creation
// ============================================================================

const initialState: BackupState = {
  backups: [],
  maxBackups: 5,
  activeRestoreId: null,
  restoreStatus: null,
  isLoading: false,
  isCreating: false,
  isRestoring: false,
  error: null,
  statusMessage: null,
  statusType: null,
};

export function createBackupStore() {
  const { subscribe, set, update } = writable<BackupState>(initialState);

  let restorePollingInterval: ReturnType<typeof setInterval> | null = null;
  let backupPollingInterval: ReturnType<typeof setInterval> | null = null;
  let statusMessageTimeout: ReturnType<typeof setTimeout> | null = null;

  function setStatus(
    message: string,
    type: "info" | "success" | "error" | "warning"
  ) {
    // Clear any pending status timeout to prevent memory leaks
    if (statusMessageTimeout) {
      clearTimeout(statusMessageTimeout);
      statusMessageTimeout = null;
    }

    update((s) => ({ ...s, statusMessage: message, statusType: type }));
    if (type === "success") {
      statusMessageTimeout = setTimeout(() => {
        update((s) => ({ ...s, statusMessage: null, statusType: null }));
        statusMessageTimeout = null;
      }, 5000);
    }
  }

  function clearStatus() {
    if (statusMessageTimeout) {
      clearTimeout(statusMessageTimeout);
      statusMessageTimeout = null;
    }
    update((s) => ({ ...s, statusMessage: null, statusType: null }));
  }

  async function listBackups(): Promise<{
    success: boolean;
    backups?: BackupInfo[];
    error?: string;
  }> {
    update((s) => ({ ...s, isLoading: true, error: null }));
    try {
      const response = await wsStore.callGRPC<{
        backups?: Array<{
          backup_id: string;
          status: string;
          size_bytes: number;
          created_at: { seconds: number; nanos?: number } | number;
          expires_at?: { seconds: number; nanos?: number } | number;
          message?: string;
        }>;
        max_backups?: number;
      }>("TenantDataService", "ListBackups", {});

      const backups: BackupInfo[] = (response.backups || []).map((b) => ({
        backup_id: b.backup_id,
        status: b.status as BackupInfo["status"],
        size_bytes: b.size_bytes || 0,
        created_at:
          typeof b.created_at === "object"
            ? b.created_at.seconds
            : b.created_at || 0,
        expires_at: b.expires_at
          ? typeof b.expires_at === "object"
            ? b.expires_at.seconds
            : b.expires_at
          : undefined,
        message: b.message,
      }));

      // Sort by created_at descending (newest first)
      backups.sort((a, b) => b.created_at - a.created_at);

      // Check if any backups are still processing
      const hasProcessing = backups.some(
        (b) => b.status === "pending" || b.status === "processing"
      );

      update((s) => ({
        ...s,
        backups,
        maxBackups: response.max_backups || 5,
        isLoading: false,
      }));

      // If we have processing backups, start polling
      if (hasProcessing && !backupPollingInterval) {
        startBackupPolling();
      } else if (!hasProcessing && backupPollingInterval) {
        stopBackupPolling();
      }

      return { success: true, backups };
    } catch (err: any) {
      const errorMsg = err.message || "Failed to load backups";
      update((s) => ({ ...s, error: errorMsg, isLoading: false }));
      return { success: false, error: errorMsg };
    }
  }

  function startBackupPolling() {
    if (backupPollingInterval) return;
    backupPollingInterval = setInterval(() => {
      listBackups();
    }, 3000);
  }

  function stopBackupPolling() {
    if (backupPollingInterval) {
      clearInterval(backupPollingInterval);
      backupPollingInterval = null;
    }
  }

  async function createBackup(): Promise<{
    success: boolean;
    backupId?: string;
    error?: string;
  }> {
    update((s) => ({ ...s, isCreating: true, error: null }));
    setStatus("Creating backup...", "info");

    try {
      const response = await wsStore.callGRPC<{
        backup_id: string;
        status: string;
        message?: string;
      }>("TenantDataService", "RequestBackup", {});

      const backupId = response.backup_id;
      const status = response.status || "pending";

      if (status === "ready") {
        setStatus("Backup ready!", "success");
        await listBackups();
      } else {
        setStatus(
          `Backup created (${backupId.substring(0, 8)}...) - Processing...`,
          "info"
        );
        // Start polling for status
        startBackupPolling();
        await listBackups();
      }

      update((s) => ({ ...s, isCreating: false }));
      return { success: true, backupId };
    } catch (err: any) {
      const errorMsg = err.message || "Failed to create backup";
      update((s) => ({ ...s, error: errorMsg, isCreating: false }));
      setStatus(`Failed to create backup: ${errorMsg}`, "error");
      return { success: false, error: errorMsg };
    }
  }

  async function deleteBackup(
    backupId: string
  ): Promise<{ success: boolean; error?: string }> {
    setStatus("Deleting backup...", "info");

    try {
      await wsStore.callGRPC<{ success: boolean; message?: string }>(
        "TenantDataService",
        "DeleteBackup",
        {
          backup_id: backupId,
        }
      );

      setStatus("Backup deleted successfully!", "success");
      await listBackups();
      return { success: true };
    } catch (err: any) {
      const errorMsg = err.message || "Failed to delete backup";
      setStatus(`Delete failed: ${errorMsg}`, "error");
      return { success: false, error: errorMsg };
    }
  }

  async function restoreBackup(
    backupId: string
  ): Promise<{ success: boolean; restoreId?: string; error?: string }> {
    update((s) => ({ ...s, isRestoring: true, error: null }));
    setStatus(" Initiating restore... Please wait.", "info");

    try {
      const response = await wsStore.callGRPC<{
        restore_id?: string;
        status: string;
        message?: string;
      }>("TenantDataService", "RestoreBackup", {
        backup_id: backupId,
      });

      if (response.restore_id) {
        update((s) => ({ ...s, activeRestoreId: response.restore_id! }));
        setStatus(
          ` Restore started (${response.restore_id.substring(0, 8)}...) - ${response.message || "Processing..."
          }`,
          "info"
        );
        startRestorePolling(response.restore_id);
        return { success: true, restoreId: response.restore_id };
      } else if (response.status === "completed") {
        setStatus(
          ` ${response.message || "Backup restored successfully!"}`,
          "success"
        );
        update((s) => ({ ...s, isRestoring: false }));
        // Reload page after successful restore
        setTimeout(() => window.location.reload(), 2000);
        return { success: true };
      } else if (response.status === "failed") {
        setStatus(`❌ ${response.message || "Restore failed"}`, "error");
        update((s) => ({ ...s, isRestoring: false }));
        return { success: false, error: response.message };
      } else {
        // Fallback success
        setStatus(" Backup restored successfully! Refreshing...", "success");
        update((s) => ({ ...s, isRestoring: false }));
        setTimeout(() => window.location.reload(), 2000);
        return { success: true };
      }
    } catch (err: any) {
      const errorMsg = err.message || "Failed to restore backup";
      update((s) => ({ ...s, error: errorMsg, isRestoring: false }));
      setStatus(`❌ Restore failed: ${errorMsg}`, "error");
      return { success: false, error: errorMsg };
    }
  }

  function startRestorePolling(restoreId: string) {
    if (restorePollingInterval) {
      clearInterval(restorePollingInterval);
    }

    let pollCount = 0;
    const maxPolls = 60; // Max 5 minutes at 5s interval

    restorePollingInterval = setInterval(async () => {
      pollCount++;

      if (pollCount > maxPolls) {
        stopRestorePolling();
        setStatus(
          " Restore is taking longer than expected. Please check back later.",
          "warning"
        );
        update((s) => ({ ...s, isRestoring: false }));
        return;
      }

      try {
        const response = await wsStore.callGRPC<{
          status: string;
          progress_percent?: number;
          message?: string;
        }>("TenantDataService", "GetRestoreStatus", {
          restore_id: restoreId,
        });

        const status = response.status || "";
        const progress = response.progress_percent || 0;
        const message = response.message || "";

        update((s) => ({
          ...s,
          restoreStatus: {
            restore_id: restoreId,
            status: status as RestoreStatus["status"],
            progress_percent: progress,
            message,
          },
        }));

        if (status === "completed") {
          stopRestorePolling();
          setStatus(` ${message} Refreshing page...`, "success");
          update((s) => ({ ...s, isRestoring: false, activeRestoreId: null }));
          setTimeout(() => window.location.reload(), 2000);
        } else if (status === "failed") {
          stopRestorePolling();
          setStatus(`❌ Restore failed: ${message}`, "error");
          update((s) => ({ ...s, isRestoring: false, activeRestoreId: null }));
        } else if (status === "in_progress") {
          setStatus(` Restoring... ${progress}% - ${message}`, "info");
        } else {
          setStatus(` Restore ${status}: ${message}`, "info");
        }
      } catch (err: any) {
        console.warn("Restore status error:", err.message);
        // Keep polling, might be temporary error
      }
    }, 3000);
  }

  function stopRestorePolling() {
    if (restorePollingInterval) {
      clearInterval(restorePollingInterval);
      restorePollingInterval = null;
    }
  }

  function cleanup() {
    stopBackupPolling();
    stopRestorePolling();
    // Clear status message timeout
    if (statusMessageTimeout) {
      clearTimeout(statusMessageTimeout);
      statusMessageTimeout = null;
    }
  }

  // Derived stores
  const hasBackups = derived({ subscribe }, (s) => s.backups.length > 0);
  const canCreateBackup = derived(
    { subscribe },
    (s) => s.backups.length < s.maxBackups && !s.isCreating
  );
  const readyBackups = derived({ subscribe }, (s) =>
    s.backups.filter((b) => b.status === "ready")
  );

  return {
    subscribe,
    listBackups,
    createBackup,
    deleteBackup,
    restoreBackup,
    setStatus,
    clearStatus,
    cleanup,
    hasBackups,
    canCreateBackup,
    readyBackups,
  };
}

export const backupStore = createBackupStore();

// ============================================================================
// Utility Functions
// ============================================================================

export function formatBytes(bytes: number): string {
  if (bytes === 0) return "0 Bytes";
  const k = 1024;
  const sizes = ["Bytes", "KB", "MB", "GB"];
  const i = Math.floor(Math.log(bytes) / Math.log(k));
  return `${Math.round((bytes / Math.pow(k, i)) * 100) / 100} ${sizes[i]}`;
}

export function formatBackupDate(timestamp: number): string {
  if (!timestamp || timestamp <= 0) return "N/A";
  // Convert seconds to milliseconds for the utility function
  return formatLocalDateTime(timestamp * 1000, "N/A");
}

export function getStatusBadgeClass(status: string): string {
  switch (status) {
    case "ready":
      return "status-ready";
    case "processing":
    case "pending":
      return "status-processing";
    case "error":
    case "failed":
      return "status-error";
    default:
      return "";
  }
}

export function getStatusLabel(status: string): string {
  switch (status) {
    case "ready":
      return "✓ READY";
    case "processing":
      return " PROCESSING";
    case "pending":
      return " PENDING";
    case "error":
    case "failed":
      return "❌ ERROR";
    default:
      return status.toUpperCase();
  }
}
