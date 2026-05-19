import { writable, derived } from "svelte/store";
import { wsStore } from "./websocket";
import { toDate, type ProtoTimestamp } from "$lib/dateUtils";

// Re-export for convenience
export type { ProtoTimestamp };

// Helper to convert protobuf timestamp to Date
export function protoToDate(
  ts: ProtoTimestamp | string | undefined | null
): Date | null {
  return toDate(ts);
}

// Migration status enum matching proto
export type MigrationStatus =
  | "MIGRATION_STATUS_UNKNOWN"
  | "MIGRATION_STATUS_PENDING"
  | "MIGRATION_STATUS_SSH_VERIFIED"
  | "MIGRATION_STATUS_ACCEPTED"
  | "MIGRATION_STATUS_COMPLETED"
  | "MIGRATION_STATUS_FAILED"
  | "MIGRATION_STATUS_CANCELLED"
  | "MIGRATION_STATUS_EXPIRED";

// Device OS type enum matching proto
export type DeviceOSType =
  | "DEVICE_OS_UNKNOWN"
  | "DEVICE_OS_LINUX"
  | "DEVICE_OS_OPENWRT"
  | "DEVICE_OS_MIKROTIK";

// SSH verification result
export interface SSHVerificationResult {
  peer_id: string;
  peer_name?: string;
  peer_ip?: string;
  success: boolean;
  error?: string;
  detected_os?: DeviceOSType;
  connection_time_ms?: number;
  logs?: string[];
}

// Individual peer in a migration
export interface MigrationPeer {
  peer_id: string;
  peer_name: string;
  peer_ip: string;
  ssh_port: number;
  os_type: DeviceOSType;
  public_key?: string;
  status: MigrationStatus;
  failure_reason?: string;
  ssh_verified: boolean;
  verified_at?: ProtoTimestamp | string;
}

// Full migration record
export interface PeerMigration {
  id: string;
  source_tenant_id: string;
  source_tenant_email?: string;
  source_tenant_name?: string;
  target_email: string;
  target_tenant_id?: string;
  target_tenant_name?: string;
  peers: MigrationPeer[];
  invite_token?: string;
  status: MigrationStatus;
  failure_reason?: string;
  created_at?: ProtoTimestamp | string;
  expires_at?: ProtoTimestamp | string;
  accepted_at?: ProtoTimestamp | string;
  completed_at?: ProtoTimestamp | string;
  logs?: string[];
}

// Store state
export interface MigrationState {
  migrations: PeerMigration[];
  pendingMigrations: PeerMigration[];
  selectedMigration: PeerMigration | null;
  verificationResults: SSHVerificationResult[];
  executionLogs: string[];
  isLoading: boolean;
  isVerifying: boolean;
  isExecuting: boolean;
  error: string | null;
}

const initialState: MigrationState = {
  migrations: [],
  pendingMigrations: [],
  selectedMigration: null,
  verificationResults: [],
  executionLogs: [],
  isLoading: false,
  isVerifying: false,
  isExecuting: false,
  error: null,
};

function createMigrationStore() {
  const { subscribe, set, update } = writable<MigrationState>(initialState);

  // List migrations (as source, target, or both)
  async function listMigrations(
    asSource = true,
    asTarget = true,
    includeCompleted = false
  ) {
    update((s) => ({ ...s, isLoading: true, error: null }));
    try {
      const response = await wsStore.callGRPC<{ migrations: PeerMigration[] }>(
        "TenantPortalService",
        "ListPeerMigrations",
        {
          as_source: asSource,
          as_target: asTarget,
          include_completed: includeCompleted,
        }
      );
      const migrations = response.migrations || [];
      update((s) => ({ ...s, migrations, isLoading: false }));
      return migrations;
    } catch (err: any) {
      update((s) => ({ ...s, error: err.message, isLoading: false }));
      throw err;
    }
  }

  // Get pending migrations for the current tenant (as target)
  async function getPendingMigrations() {
    update((s) => ({ ...s, isLoading: true, error: null }));
    try {
      const response = await wsStore.callGRPC<{
        pending_migrations: PeerMigration[];
      }>("TenantPortalService", "GetPendingMigrations", {});
      const pendingMigrations = response.pending_migrations || [];
      update((s) => ({ ...s, pendingMigrations, isLoading: false }));
      return pendingMigrations;
    } catch (err: any) {
      update((s) => ({ ...s, error: err.message, isLoading: false }));
      throw err;
    }
  }

  // Get a specific migration
  async function getMigration(migrationId: string) {
    update((s) => ({ ...s, isLoading: true, error: null }));
    try {
      const response = await wsStore.callGRPC<{ migration: PeerMigration }>(
        "TenantPortalService",
        "GetPeerMigration",
        { migration_id: migrationId }
      );
      const migration = response.migration;
      update((s) => ({ ...s, selectedMigration: migration, isLoading: false }));
      return migration;
    } catch (err: any) {
      update((s) => ({ ...s, error: err.message, isLoading: false }));
      throw err;
    }
  }

  // Verify SSH connection to peers before migration
  async function verifySSH(
    peerIds: string[],
    sshUsername: string,
    sshPassword: string
  ) {
    update((s) => ({
      ...s,
      isVerifying: true,
      error: null,
      verificationResults: [],
    }));
    try {
      const response = await wsStore.callGRPC<{
        all_verified: boolean;
        results: SSHVerificationResult[];
        logs: string[];
      }>("TenantPortalService", "VerifyMigrationSSH", {
        peer_ids: peerIds,
        ssh_username: sshUsername,
        ssh_password: sshPassword,
      });
      update((s) => ({
        ...s,
        verificationResults: response.results || [],
        executionLogs: response.logs || [],
        isVerifying: false,
      }));
      return {
        allVerified: response.all_verified,
        results: response.results || [],
        logs: response.logs || [],
      };
    } catch (err: any) {
      update((s) => ({ ...s, error: err.message, isVerifying: false }));
      throw err;
    }
  }

  // Create a new peer migration
  async function createMigration(
    peerIds: string[],
    targetEmail: string,
    sshUsername: string,
    sshPassword: string
  ) {
    update((s) => ({ ...s, isLoading: true, error: null }));
    try {
      const response = await wsStore.callGRPC<{
        success: boolean;
        message: string;
        migration: PeerMigration;
        verifications: SSHVerificationResult[];
      }>("TenantPortalService", "CreatePeerMigration", {
        peer_ids: peerIds,
        target_email: targetEmail,
        ssh_username: sshUsername,
        ssh_password: sshPassword,
      });
      if (response.success && response.migration) {
        update((s) => ({
          ...s,
          migrations: [...s.migrations, response.migration],
          selectedMigration: response.migration,
          verificationResults: response.verifications || [],
          isLoading: false,
        }));
      } else {
        update((s) => ({
          ...s,
          verificationResults: response.verifications || [],
          error: response.message,
          isLoading: false,
        }));
      }
      return response;
    } catch (err: any) {
      update((s) => ({ ...s, error: err.message, isLoading: false }));
      throw err;
    }
  }

  // Cancel a migration
  async function cancelMigration(migrationId: string) {
    update((s) => ({ ...s, isLoading: true, error: null }));
    try {
      const response = await wsStore.callGRPC<{
        success: boolean;
        message: string;
      }>("TenantPortalService", "CancelPeerMigration", {
        migration_id: migrationId,
      });
      if (response.success) {
        update((s) => ({
          ...s,
          migrations: s.migrations.filter((m) => m.id !== migrationId),
          selectedMigration:
            s.selectedMigration?.id === migrationId
              ? null
              : s.selectedMigration,
          isLoading: false,
        }));
      }
      return response;
    } catch (err: any) {
      update((s) => ({ ...s, error: err.message, isLoading: false }));
      throw err;
    }
  }

  // Delete a migration from the list permanently
  async function deleteMigration(migrationId: string) {
    update((s) => ({ ...s, isLoading: true, error: null }));
    try {
      const response = await wsStore.callGRPC<{
        success: boolean;
        message: string;
      }>("TenantPortalService", "DeletePeerMigration", {
        migration_id: migrationId,
      });
      if (response.success) {
        update((s) => ({
          ...s,
          migrations: s.migrations.filter((m) => m.id !== migrationId),
          pendingMigrations: s.pendingMigrations.filter(
            (m) => m.id !== migrationId
          ),
          selectedMigration:
            s.selectedMigration?.id === migrationId
              ? null
              : s.selectedMigration,
          isLoading: false,
        }));
      }
      return response;
    } catch (err: any) {
      update((s) => ({ ...s, error: err.message, isLoading: false }));
      throw err;
    }
  }
  // Validate a migration invite token
  async function validateToken(inviteToken: string) {
    update((s) => ({ ...s, isLoading: true, error: null }));
    try {
      const response = await wsStore.callGRPC<{
        valid: boolean;
        message: string;
        migration?: PeerMigration;
        requires_registration: boolean;
      }>("TenantPortalService", "ValidateMigrationToken", {
        invite_token: inviteToken,
      });
      update((s) => ({
        ...s,
        selectedMigration: response.migration || null,
        isLoading: false,
      }));
      return response;
    } catch (err: any) {
      update((s) => ({ ...s, error: err.message, isLoading: false }));
      throw err;
    }
  }

  // Accept a peer migration
  async function acceptMigration(migrationId?: string, inviteToken?: string) {
    update((s) => ({
      ...s,
      isExecuting: true,
      error: null,
      executionLogs: [],
    }));
    try {
      const response = await wsStore.callGRPC<{
        success: boolean;
        message: string;
        migration: PeerMigration;
        execution_logs: string[];
      }>("TenantPortalService", "AcceptPeerMigration", {
        migration_id: migrationId || "",
        invite_token: inviteToken || "",
      });
      update((s) => ({
        ...s,
        selectedMigration: response.migration,
        executionLogs: response.execution_logs || [],
        pendingMigrations: s.pendingMigrations.filter(
          (m) => m.id !== response.migration?.id
        ),
        isExecuting: false,
      }));
      return response;
    } catch (err: any) {
      update((s) => ({ ...s, error: err.message, isExecuting: false }));
      throw err;
    }
  }

  // Retry a failed migration
  async function retryMigration(migrationId: string) {
    update((s) => ({ ...s, isExecuting: true, error: null }));
    try {
      const response = await wsStore.callGRPC<{
        success: boolean;
        message: string;
        migration: PeerMigration;
        execution_logs?: string[];
      }>("TenantPortalService", "RetryPeerMigration", {
        migration_id: migrationId,
      });
      update((s) => ({
        ...s,
        selectedMigration: response.migration,
        executionLogs: response.execution_logs || [],
        isExecuting: false,
      }));
      return response;
    } catch (err: any) {
      update((s) => ({ ...s, error: err.message, isExecuting: false }));
      throw err;
    }
  }

  // Clear error
  function clearError() {
    update((s) => ({ ...s, error: null }));
  }

  // Clear verification results
  function clearVerification() {
    update((s) => ({ ...s, verificationResults: [], executionLogs: [] }));
  }

  // Reset store
  function reset() {
    set(initialState);
  }

  return {
    subscribe,
    listMigrations,
    getPendingMigrations,
    getMigration,
    verifySSH,
    createMigration,
    cancelMigration,
    deleteMigration,
    validateToken,
    acceptMigration,
    retryMigration,
    clearError,
    clearVerification,
    reset,
  };
}

export const migrationStore = createMigrationStore();

// Derived stores for convenience

// Active (non-completed) migrations
export const activeMigrations = derived(migrationStore, ($store) =>
  $store.migrations.filter(
    (m) =>
      m.status !== "MIGRATION_STATUS_COMPLETED" &&
      m.status !== "MIGRATION_STATUS_CANCELLED" &&
      m.status !== "MIGRATION_STATUS_EXPIRED"
  )
);

// Pending migrations count
export const pendingCount = derived(
  migrationStore,
  ($store) => $store.pendingMigrations.length
);

// Has pending migrations
export const hasPendingMigrations = derived(
  migrationStore,
  ($store) => $store.pendingMigrations.length > 0
);

// Map numeric proto enum values to string status
// Proto enum: UNKNOWN=0, PENDING=1, ACCEPTED=2, COMPLETED=3, FAILED=4, CANCELLED=5, EXPIRED=6
const statusNumToString: Record<number, MigrationStatus> = {
  0: "MIGRATION_STATUS_UNKNOWN",
  1: "MIGRATION_STATUS_PENDING",
  2: "MIGRATION_STATUS_ACCEPTED",
  3: "MIGRATION_STATUS_COMPLETED",
  4: "MIGRATION_STATUS_FAILED",
  5: "MIGRATION_STATUS_CANCELLED",
  6: "MIGRATION_STATUS_EXPIRED",
};

// Normalize status to string (handles both numeric and string values from proto)
export function normalizeStatus(
  status: MigrationStatus | number | string | undefined
): MigrationStatus {
  if (status === undefined || status === null) {
    return "MIGRATION_STATUS_UNKNOWN";
  }
  if (typeof status === "number") {
    return statusNumToString[status] || "MIGRATION_STATUS_UNKNOWN";
  }
  // Already a string
  if (typeof status === "string" && status.startsWith("MIGRATION_STATUS_")) {
    return status as MigrationStatus;
  }
  return "MIGRATION_STATUS_UNKNOWN";
}

// Helper functions for status display
export function getMigrationStatusColor(
  status: MigrationStatus | number | string
): string {
  const normalized = normalizeStatus(status);
  switch (normalized) {
    case "MIGRATION_STATUS_PENDING":
      return "text-yellow-600 dark:text-yellow-400";
    case "MIGRATION_STATUS_SSH_VERIFIED":
      return "text-blue-600 dark:text-blue-400";
    case "MIGRATION_STATUS_ACCEPTED":
      return "text-blue-600 dark:text-blue-400";
    case "MIGRATION_STATUS_COMPLETED":
      return "text-green-600 dark:text-green-400";
    case "MIGRATION_STATUS_FAILED":
      return "text-red-600 dark:text-red-400";
    case "MIGRATION_STATUS_CANCELLED":
      return "text-gray-500 dark:text-gray-400";
    case "MIGRATION_STATUS_EXPIRED":
      return "text-gray-500 dark:text-gray-400";
    default:
      return "text-gray-500 dark:text-gray-400";
  }
}

export function getMigrationStatusLabel(
  status: MigrationStatus | number | string
): string {
  const normalized = normalizeStatus(status);
  switch (normalized) {
    case "MIGRATION_STATUS_PENDING":
      return "Pending";
    case "MIGRATION_STATUS_SSH_VERIFIED":
      return "SSH Verified";
    case "MIGRATION_STATUS_ACCEPTED":
      return "Accepted";
    case "MIGRATION_STATUS_COMPLETED":
      return "Completed";
    case "MIGRATION_STATUS_FAILED":
      return "Failed";
    case "MIGRATION_STATUS_CANCELLED":
      return "Cancelled";
    case "MIGRATION_STATUS_EXPIRED":
      return "Expired";
    default:
      return "Unknown";
  }
}

// Map numeric proto enum values for device OS
// Proto enum: UNKNOWN=0, LINUX=1, OPENWRT=2, MIKROTIK=3
const osNumToString: Record<number, DeviceOSType> = {
  0: "DEVICE_OS_UNKNOWN",
  1: "DEVICE_OS_LINUX",
  2: "DEVICE_OS_OPENWRT",
  3: "DEVICE_OS_MIKROTIK",
};

// Normalize OS type to string
export function normalizeOSType(
  osType: DeviceOSType | number | string | undefined
): DeviceOSType {
  if (osType === undefined || osType === null) {
    return "DEVICE_OS_UNKNOWN";
  }
  if (typeof osType === "number") {
    return osNumToString[osType] || "DEVICE_OS_UNKNOWN";
  }
  if (typeof osType === "string" && osType.startsWith("DEVICE_OS_")) {
    return osType as DeviceOSType;
  }
  return "DEVICE_OS_UNKNOWN";
}

export function getDeviceOSLabel(
  osType: DeviceOSType | number | string
): string {
  const normalized = normalizeOSType(osType);
  switch (normalized) {
    case "DEVICE_OS_LINUX":
      return "Linux";
    case "DEVICE_OS_OPENWRT":
      return "OpenWRT";
    case "DEVICE_OS_MIKROTIK":
      return "MikroTik";
    default:
      return "Unknown";
  }
}

export function getDeviceOSIcon(osType: DeviceOSType): string {
  switch (osType) {
    case "DEVICE_OS_LINUX":
      return "🐧";
    case "DEVICE_OS_OPENWRT":
      return "";
    case "DEVICE_OS_MIKROTIK":
      return "";
    default:
      return "❓";
  }
}
