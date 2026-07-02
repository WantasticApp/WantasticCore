import { wsStore, type PeerEvent } from "./websocket";
import { tenant_id as tenantIdStore } from "./auth";
import { toDate, type ProtoTimestamp } from "$lib/dateUtils";
import { derived, get, writable } from "svelte/store";

// Re-export ProtoTimestamp for backwards compatibility
export type { ProtoTimestamp };

// Helper to convert protobuf timestamp to Date (uses UTC-aware utility)
export function protoToDate(
  ts: ProtoTimestamp | string | undefined | null
): Date | null {
  return toDate(ts);
}

// Activity types for audit logging
export interface SSHActivity {
  session_id: string;
  user_agent: string;
  client_ip: string;
  timestamp: ProtoTimestamp | string;
  end_time?: ProtoTimestamp | string;
  username: string;
  commands?: string[];
  bytes_sent?: number;
  bytes_recv?: number;
  duration_ms?: number;
}

export interface WinboxActivity {
  session_name: string;
  username: string;
  client_ip: string;
  timestamp: ProtoTimestamp | string;
  end_time?: ProtoTimestamp | string;
  duration_ms?: number;
  romon_mode?: boolean;
}

export interface Peer {
  id: string;
  account_id: string;
  name: string;
  public_key: string;
  assigned_ip: string;
  ip_address?: string; // Alias for assigned_ip for backwards compatibility
  allowed_ips: string[];
  tags?: string[];
  created_at?: string;
  last_handshake?: string;
  endpoint?: string;
  rx_bytes?: number;
  tx_bytes?: number;
  transfer_rx?: number; // Alias for rx_bytes
  transfer_tx?: number; // Alias for tx_bytes
  is_online?: boolean;
  last_seen_at?: string;
  // Activity audit trail
  ssh_activities?: SSHActivity[];
  winbox_activities?: WinboxActivity[];
  // Port discovery fields
  discovered_ports?: OpenPort[];
  last_port_scan?: string;
  scanned_ssh_port?: number;
  scanned_winbox_port?: number;
  has_winbox?: boolean;
  router_ip?: string;
  // LLDP neighbor discovery
  lldp_neighbors?: LLDPNeighbor[];
  last_lldp_discovery?: string;
  // Notification settings
  notification_enabled?: boolean;
  first_seen_online?: string;
  last_online_at?: string;
  fingerprint?: OSFingerprint;
  uptime_history?: Uint8Array | string;
  notes?: string;
  // Agent identification (set by server when WUSP OnBoardRequest arrives)
  client_type?: string;     // 'native' (standard WireGuard) or 'wantasticd' (custom agent)
  is_wantasticd?: boolean;  // True when peer runs the wantasticd agent
  routeros_candidate?: boolean;
  routeros_api_ready?: boolean;
  routeros_api_port?: number;
  routeros_api_tls?: boolean;
  // ── Shared-access metadata ────────────────────────────────────────────────
  // Set by the backend middleware (shared_access.go → listPeersForCaller).
  // Use these to drive badge display and action-button visibility in the UI.
  // Do NOT set these fields manually in the store — they come from the server.
  is_shared?: boolean; // true  = belongs to another account shared with this caller
  owner_name?: string; // display name of the resource owner (shown in badge)
  viewer_can_write?: boolean; // true  = caller has write (manage) permission on this peer
  // false = read-only (hide destructive actions like delete)
}

// OpenPort represents a discovered open port with service detection
export interface OpenPort {
  port: number;
  protocol: string;
  service: string;
  banner?: string;
  rtt_ms?: number;
  is_webpage?: boolean; // True if this port serves HTML web content
}

// LLDPNeighbor represents a neighbor discovered via LLDP/CDP/MNDP
export interface LLDPNeighbor {
  chassis_id: string;
  port_id: string;
  port_description?: string;
  system_name?: string;
  system_description?: string;
  management_address?: string;
  local_interface?: string;
  discovered_by?: string;
  ttl?: number;
  capabilities?: string[];
  first_seen?: string;
  last_seen?: string;
  mac_address?: string;
  model?: string;
  vendor?: string;
}

// OSFingerprint represents detected OS/device information from banner analysis
export interface OSFingerprint {
  os_family?: string; // OS family: linux, windows, routeros, ios, bsd, etc.
  os_version?: string; // OS version: e.g., "7.15", "10", "22.04"
  vendor?: string; // Device vendor: MikroTik, Cisco, Microsoft, Linux, etc.
  device_type?: string; // Device type: router, server, workstation, switch, ap
  model?: string; // Device model if detected
  hostname?: string; // Hostname if detected
  mac_address?: string; // MAC address if available
  mac_vendor?: string; // Vendor from MAC OUI lookup
  confidence?: number; // Confidence score 0-100
  detection_info?: string; // How the OS was detected
}

export interface Handshake {
  timestamp: string | { seconds: number; nanos: number };
  endpoint: string;
}

export interface PeerStats {
  peer_id: string;
  rx_bytes: number;
  tx_bytes: number;
  is_online: boolean;
  last_handshake: number;
  open_ports?: OpenPort[];
  last_port_scan?: { seconds: number; nanos: number };
  scanned_ssh_port?: number;
  scanned_winbox_port?: number;
  fingerprint?: OSFingerprint;
  scan_in_progress?: boolean;
  active_scan_id?: string;
  activeScanId?: string;
  uptime_history?: Uint8Array | string; // Base64 or Uint8Array
}

export interface PeerConfig {
  config: string;
  qr_code?: string;
  setup_token?: string;
}

export type PeerConfigTab = "wireguard" | "mikrotik" | "unix" | "qrcode";

export interface SelectedPeerConfig {
  peerId: string;
  config: PeerConfig;
  preferredTab?: PeerConfigTab;
}

export interface EnrollmentToken {
  id: string;
  tenant_id: string;
  name: string;
  token: string;
  max_uses?: number;
  usage_count?: number;
  expires_at?: string | ProtoTimestamp;
  created_at?: string | ProtoTimestamp;
  created_by?: string;
}

export interface PeerState {
  peers: Peer[];
  tokens: EnrollmentToken[];
  selectedPeer: Peer | null;
  selectedPeerConfig: SelectedPeerConfig | null;
  isLoading: boolean;
  error: string | null;
  lastFetched: number;
}

const initialState: PeerState = {
  peers: [],
  tokens: [],
  selectedPeer: null,
  selectedPeerConfig: null,
  isLoading: false,
  error: null,
  lastFetched: 0,
};

// Store for the peer currently being onboarded
export const onboardingPeer = writable<Peer | null>(null);

function createPeerStore() {
  const { subscribe, set, update } = writable<PeerState>(initialState);

  function subscribeToPeer(peerId: string) {
    wsStore.subscribeToPeer(peerId);
  }

  function unsubscribeFromPeer(peerId: string) {
    wsStore.unsubscribeFromPeer(peerId);
  }

  // Subscribe to WebSocket events for real-time updates
  let wsUnsubscribe: (() => void) | null = null;

  function startListeningToEvents() {
    if (wsUnsubscribe) return; // Already listening

    wsUnsubscribe = wsStore.subscribe((wsState) => {
      if (wsState.events.length === 0) return;

      // Get the latest event
      const latestEvent = wsState.events[wsState.events.length - 1];
      handlePeerEvent(latestEvent);
    });
  }

  // Auto-sync logic
  let isSyncing = false;

  // Subscribe to connection and auth state changes
  wsStore.subscribe((wsState) => {
    const tid = get(tenantIdStore);
    if (wsState.status === "connected" && tid && !isSyncing) {
      // Connection restored or established
      listPeers(tid).catch(console.error);
    }
  });

  tenantIdStore.subscribe((tid) => {
    const wsState = get(wsStore);
    if (wsState.status === "connected" && tid && !isSyncing) {
      // Auth established
      listPeers(tid).catch(console.error);
    }
  });

  function handlePeerEvent(event: PeerEvent) {
    const eventTime =
      (event.data?.timestamp as string | undefined) || event.timestamp;

    switch (event.type) {
      case "peer_connected":
        // Update peer online status
        update((s) => {
          const updatedPeers = s.peers.map((p) =>
            p.id === event.peerId || p.public_key === event.peerId
              ? {
                  ...p,
                  is_online: true,
                  last_handshake: eventTime,
                  last_seen_at: eventTime,
                }
              : p
          );
          const updatedSelectedPeer =
            s.selectedPeer?.id === event.peerId ||
            s.selectedPeer?.public_key === event.peerId
              ? {
                  ...s.selectedPeer,
                  is_online: true,
                  last_handshake: eventTime,
                  last_seen_at: eventTime,
                }
              : s.selectedPeer;
          return {
            ...s,
            peers: updatedPeers,
            selectedPeer: updatedSelectedPeer,
          };
        });
        break;

      case "peer_disconnected":
        // Update peer offline status
        update((s) => {
          const updatedPeers = s.peers.map((p) =>
            p.id === event.peerId || p.public_key === event.peerId
              ? { ...p, is_online: false, last_seen_at: eventTime }
              : p
          );
          const updatedSelectedPeer =
            s.selectedPeer?.id === event.peerId ||
            s.selectedPeer?.public_key === event.peerId
              ? {
                  ...s.selectedPeer,
                  is_online: false,
                  last_seen_at: eventTime,
                }
              : s.selectedPeer;
          return {
            ...s,
            peers: updatedPeers,
            selectedPeer: updatedSelectedPeer,
          };
        });
        break;

      case "peer_updated":
        // Peer configuration changed
        if (event.data) {
          update((s) => {
            const updatedPeers = s.peers.map((p) =>
              p.id === event.peerId ? { ...p, ...event.data } : p
            );
            const updatedSelectedPeer =
              s.selectedPeer?.id === event.peerId
                ? { ...s.selectedPeer, ...event.data }
                : s.selectedPeer;
            return {
              ...s,
              peers: updatedPeers,
              selectedPeer: updatedSelectedPeer,
            };
          });
        }
        break;

      case "status_change":
        // Real-time online/offline status change from Redis pub/sub
        {
          const isOnline = event.data?.isOnline ?? event.data?.is_online;
          if (isOnline !== undefined) {
            update((s) => {
              const updatedPeers = s.peers.map((p) =>
                p.id === event.peerId || p.public_key === event.peerId
                  ? {
                      ...p,
                      is_online: isOnline,
                      last_seen_at: eventTime,
                      last_handshake: isOnline
                        ? eventTime
                        : p.last_handshake,
                    }
                  : p
              );
              const updatedSelectedPeer =
                s.selectedPeer?.id === event.peerId ||
                s.selectedPeer?.public_key === event.peerId
                  ? {
                      ...s.selectedPeer,
                      is_online: isOnline,
                      last_seen_at: eventTime,
                      last_handshake: isOnline
                        ? eventTime
                        : s.selectedPeer.last_handshake,
                    }
                  : s.selectedPeer;
              return {
                ...s,
                peers: updatedPeers,
                selectedPeer: updatedSelectedPeer,
              };
            });
          }
        }
        break;
    }
  }

  async function listPeers(accountId?: string, forceRefresh = false) {
    const tid = accountId || get(tenantIdStore);
    if (!tid) return [];

    const currentState = get({ subscribe });
    const now = Date.now();
    const isStale = now - currentState.lastFetched > 5 * 60 * 1000; // 5 minutes cache

    // CACHE STRATEGY:
    // 1. If we have data and it's fresh (and not forcing refresh), return immediately.
    if (!forceRefresh && currentState.peers.length > 0 && !isStale) {
      if (!wsUnsubscribe) startListeningToEvents(); // Ensure WS is listening
      return currentState.peers;
    }

    // 2. If we have data but it's stale (or forcing refresh), show data but update in background.
    // 3. Only show loading if we have NO data.
    if (currentState.peers.length === 0 || forceRefresh) {
      update((s) => ({ ...s, isLoading: true, error: null }));
    } else {
      // We have stale data, keep showing it...
    }

    // Prevent concurrent fetches
    if (isSyncing) return currentState.peers;
    isSyncing = true;

    try {
      const response = await wsStore.callGRPC<{ peers: Peer[] }>(
        "TenantPeerService",
        "ListTenantPeers",
        {
          tenant_id: tid,
        }
      );
      // DEBUG: trace shared-access data delivery
      const myTenantId = tid;
      const rawPeers = response.peers || [];
      const sharedInResponse = rawPeers.filter(
        (p: any) => p.is_shared === true
      );
      console.log(
        `[peer.ts] ListTenantPeers | my tenant_id=${myTenantId} | ` +
          `total=${rawPeers.length} | with_is_shared=${sharedInResponse.length}`
      );
      if (rawPeers.length > 0) {
        console.log("[peer.ts] First peer sample:", {
          id: rawPeers[0].id,
          name: rawPeers[0].name,
          account_id: rawPeers[0].account_id,
          is_shared: rawPeers[0].is_shared,
          owner_name: rawPeers[0].owner_name,
          viewer_can_write: rawPeers[0].viewer_can_write,
        });
      }
      // Normalize response: add legacy field aliases for backwards compatibility.
      // Shared-access fields (is_shared, owner_name, viewer_can_write) are
      // preserved automatically by the spread — do not strip or override them.
      const peers = rawPeers.map((peer: Peer) => ({
        ...peer,
        ip_address: peer.assigned_ip,
        transfer_rx: peer.rx_bytes,
        transfer_tx: peer.tx_bytes,
      }));
      update((s) => {
        // Preserve live WS-driven status. The DB snapshot may lag behind
        // real-time WireGuard handshakes, so if the store (or wsStore.peerStatuses)
        // already knows a peer is online, keep that status rather than letting
        // a stale DB row flip it back to offline/never.
        const liveById = new Map<string, Pick<Peer, "is_online" | "last_seen_at" | "last_handshake">>();
        for (const p of s.peers) {
          if (p.is_online) {
            liveById.set(p.id, {
              is_online: true,
              last_seen_at: p.last_seen_at,
              last_handshake: p.last_handshake,
            });
          }
        }
        const wsState = get(wsStore);
        const mergedPeers = peers.map((p: Peer) => {
          const live = liveById.get(p.id);
          const wsStatus = wsState.peerStatuses.get(p.id);
          if (live?.is_online || wsStatus?.isOnline) {
            return {
              ...p,
              is_online: true,
              last_seen_at: live?.last_seen_at ?? wsStatus?.lastSeen ?? p.last_seen_at,
              last_handshake: live?.last_handshake ?? p.last_handshake,
            };
          }
          return p;
        });
        return { ...s, peers: mergedPeers, isLoading: false, lastFetched: Date.now() };
      });
      // Start listening to real-time events after loading peers
      startListeningToEvents();
      return peers;
    } catch (err: any) {
      update((s) => ({ ...s, error: err.message, isLoading: false }));
      throw err;
    } finally {
      isSyncing = false;
    }
  }

  async function getPeer(peerId: string) {
    try {
      const response = await wsStore.callGRPC<{ peer: Peer }>(
        "TenantPeerService",
        "GetTenantPeer",
        {
          peer_id: peerId,
          tenant_id: "",
        }
      );
      const peer = {
        ...response.peer,
        ip_address: response.peer.assigned_ip,
        transfer_rx: response.peer.rx_bytes,
        transfer_tx: response.peer.tx_bytes,
      };
      update((s) => ({ ...s, selectedPeer: peer }));
      return peer;
    } catch (err: any) {
      update((s) => ({ ...s, error: err.message }));
      throw err;
    }
  }

  async function addPeer(
    name: string,
    assignedIpOrOptions?: string | { publicKey?: string },
    allowedIps?: string[]
  ) {
    const options =
      typeof assignedIpOrOptions === "object" && assignedIpOrOptions !== null
        ? assignedIpOrOptions
        : {};
    const assignedIp =
      typeof assignedIpOrOptions === "string" ? assignedIpOrOptions : "";
    update((s) => ({ ...s, isLoading: true, error: null }));
    try {
      const response = await wsStore.callGRPC<{
        peer: Peer;
        config?: string;
      }>(
        "TenantPeerService",
        "AddTenantPeer",
        {
          name,
          tenant_id: "",
          assigned_ip: assignedIp || "",
          allowed_ips: allowedIps || [],
          public_key: options.publicKey || "",
        }
      );
      const peer = {
        ...response.peer,
        ip_address: response.peer.assigned_ip,
        transfer_rx: response.peer.rx_bytes,
        transfer_tx: response.peer.tx_bytes,
      };
      onboardingPeer.set(peer);
      update((s) => ({
        ...s,
        peers: [...s.peers, peer],
        selectedPeer: peer,
        selectedPeerConfig:
          response.config && response.config.trim()
            ? {
                peerId: peer.id,
                config: { config: response.config },
              }
            : null,
        isLoading: false,
      }));
      return peer;
    } catch (err: any) {
      update((s) => ({ ...s, error: err.message, isLoading: false }));
      throw err;
    }
  }

  async function removePeer(peerId: string) {
    update((s) => ({ ...s, isLoading: true, error: null }));
    try {
      await wsStore.callGRPC<{ success: boolean }>(
        "TenantPeerService",
        "RemoveTenantPeer",
        {
          peer_id: peerId,
          tenant_id: "",
        }
      );
      update((s) => ({
        ...s,
        peers: s.peers.filter((p) => p.id !== peerId),
        selectedPeer: s.selectedPeer?.id === peerId ? null : s.selectedPeer,
        isLoading: false,
      }));
    } catch (err: any) {
      update((s) => ({ ...s, error: err.message, isLoading: false }));
      throw err;
    }
  }

  async function updatePeer(peerId: string, name: string, tags?: string[]) {
    update((s) => ({ ...s, isLoading: true, error: null }));
    try {
      const response = await wsStore.callGRPC<{ success: boolean; peer: Peer }>(
        "TenantPeerService",
        "UpdateTenantPeer",
        {
          peer_id: peerId,
          tenant_id: "",
          name: name,
          tags: tags || [],
        }
      );

      if (response.peer) {
        const updatedPeer = {
          ...response.peer,
          ip_address: response.peer.assigned_ip,
          transfer_rx: response.peer.rx_bytes,
          transfer_tx: response.peer.tx_bytes,
        };
        update((s) => ({
          ...s,
          peers: s.peers.map((p) =>
            p.id === peerId ? { ...p, ...updatedPeer } : p
          ),
          selectedPeer:
            s.selectedPeer?.id === peerId
              ? { ...s.selectedPeer, ...updatedPeer }
              : s.selectedPeer,
          isLoading: false,
        }));
      }

      return response.peer;
    } catch (err: any) {
      update((s) => ({ ...s, error: err.message, isLoading: false }));
      throw err;
    }
  }

  async function updatePeerNotes(peerId: string, notes: string) {
    try {
      await wsStore.callGRPC<{ success: boolean; message: string; peer: any }>(
        "TenantPeerService",
        "UpdateTenantPeerNotes",
        {
          peer_id: peerId,
          notes: notes,
        }
      );

      // Only update the notes field on the matching peer — no full refresh
      update((s) => ({
        ...s,
        peers: s.peers.map((p) => (p.id === peerId ? { ...p, notes } : p)),
        selectedPeer:
          s.selectedPeer?.id === peerId
            ? { ...s.selectedPeer, notes }
            : s.selectedPeer,
      }));
    } catch (err: any) {
      console.error("Failed to save notes:", err);
      throw err;
    }
  }

  async function batchUpdatePeers(
    peerIds: string[],
    operation: number,
    params: {
      sequencePattern?: string;
      sequenceStart?: number;
      tags?: string[];
    } = {}
  ) {
    update((s) => ({ ...s, isLoading: true, error: null }));
    try {
      const response = await wsStore.callGRPC<{
        success: boolean;
        message: string;
        updated_count: number;
      }>("TenantPeerService", "BatchUpdateTenantPeers", {
        peer_ids: peerIds,
        operation: operation,
        sequence_pattern: params.sequencePattern || "",
        sequence_start: params.sequenceStart || 0,
        tags: params.tags || [],
      });

      // Refresh peers list if operation was successful
      // Or just invalidate/reload? Reload is safer for bulk ops.
      await listPeers();

      return response;
    } catch (err: any) {
      update((s) => ({ ...s, error: err.message, isLoading: false }));
      throw err;
    }
  }

  async function getPeerConfig(peerId: string): Promise<PeerConfig> {
    try {
      const response = await wsStore.callGRPC<{
        wg_config: string;
        qr_code: string;
        setup_token: string;
      }>("TenantPeerService", "GetTenantPeerConfig", {
        peer_id: peerId,
        tenant_id: "",
      });
      return {
        config: response.wg_config,
        qr_code: response.qr_code,
        setup_token: response.setup_token,
      };
    } catch (err: any) {
      update((s) => ({ ...s, error: err.message }));
      throw err;
    }
  }

  async function getPeerStats(peerId: string): Promise<PeerStats> {
    try {
      const response = await wsStore.callGRPC<{ stats: PeerStats }>(
        "TenantPeerService",
        "GetTenantPeerStats",
        {
          peer_id: peerId,
          tenant_id: "",
        }
      );
      return response.stats;
    } catch (err: any) {
      update((s) => ({ ...s, error: err.message }));
      throw err;
    }
  }

  /**
   * pingPeer sends streaming ICMP pings to a peer.
   * onPing is called for each result as it arrives (real-time).
   * Returns a promise that resolves with the final summary.
   */
  async function pingPeer(
    peerId: string,
    count?: number,
    timeoutMs?: number,
    onPing?: (event: any) => void,
  ): Promise<any> {
    return new Promise((resolve, reject) => {
      let summary: any = null;

      wsStore.streamGRPC(
        "TenantPeerService",
        "PingTenantPeer",
        {
          peer_id: peerId,
          tenant_id: "",
          count: count || 10,
          timeout_ms: timeoutMs || 1000,
        },
        {
          onData: (event: any) => {
            if (event?.is_summary) {
              summary = event;
            } else if (onPing) {
              onPing(event);
            }
          },
          onEnd: () => resolve(summary),
          onError: (err) => reject(new Error(err)),
        },
      );
    });
  }

  async function startPortScan(
    peerId: string,
    fullScan: boolean = false,
    ports: number[] = [],
    tcp: boolean = true,
    udp: boolean = false
  ): Promise<{
    scan_id?: string;
    status: string;
    scanId?: string;
    id?: string;
  }> {
    try {
      const response = await wsStore.callGRPC<{
        scan_id: string;
        status: string;
        scanId?: string;
        id?: string;
      }>("PeerService", "StartPortScan", {
        account_id: get(tenantIdStore),
        peer_id: peerId,
        full_scan: fullScan,
        ports: ports,
        tcp: tcp,
        udp: udp,
      });
      const resolvedScanId = response.scan_id || response.scanId || response.id;
      return {
        ...response,
        scan_id: resolvedScanId,
        scanId: resolvedScanId,
        id: resolvedScanId,
      };
    } catch (err: any) {
      update((s) => ({ ...s, error: err.message }));
      throw err;
    }
  }

  async function stopPortScan(peerId: string, scanId: string) {
    try {
      return await wsStore.callGRPC<any>("PeerService", "StopPortScan", {
        account_id: get(tenantIdStore),
        peer_id: peerId,
        scan_id: scanId,
      });
    } catch (err: any) {
      update((s) => ({ ...s, error: err.message }));
      throw err;
    }
  }

  async function pausePortScan(peerId: string, scanId: string) {
    try {
      return await wsStore.callGRPC<any>("PeerService", "PausePortScan", {
        account_id: get(tenantIdStore),
        peer_id: peerId,
        scan_id: scanId,
      });
    } catch (err: any) {
      update((s) => ({ ...s, error: err.message }));
      throw err;
    }
  }

  async function resumePortScan(peerId: string, scanId: string) {
    try {
      return await wsStore.callGRPC<any>("PeerService", "ResumePortScan", {
        account_id: get(tenantIdStore),
        peer_id: peerId,
        scan_id: scanId,
      });
    } catch (err: any) {
      update((s) => ({ ...s, error: err.message }));
      throw err;
    }
  }

  function streamScanStatus(
    peerId: string,
    scanId: string | undefined, // Optional
    handlers: {
      onStart?: () => void;
      onData?: (data: any) => void;
      onError?: (error: string) => void;
      onEnd?: () => void;
    }
  ) {
    return wsStore.streamGRPC<any>(
      "PeerService",
      "StreamPortScanStatus",
      {
        peer_id: peerId,
        scan_id: scanId || "",
      },
      {
        onStart: handlers.onStart,
        onData: (data) => {
          if (!data) return;

          const resolvedScanId = data.scan_id || data.scanId || data.id;
          handlers.onData?.({
            ...data,
            scan_id: resolvedScanId,
            scanId: resolvedScanId,
          });
        },
        onError: handlers.onError,
        onEnd: handlers.onEnd,
      }
    );
  }

  async function toggleNotification(peerId: string, enabled: boolean) {
    try {
      const response = await wsStore.callGRPC<{
        success: boolean;
        message: string;
        notification_enabled: boolean;
      }>("TenantPeerService", "SetPeerNotification", {
        peer_id: peerId,
        tenant_id: "",
        enabled: enabled,
      });

      if (response.success) {
        update((s) => ({
          ...s,
          peers: s.peers.map((p) =>
            p.id === peerId
              ? { ...p, notification_enabled: response.notification_enabled }
              : p
          ),
        }));
      }

      return response;
    } catch (err: any) {
      update((s) => ({ ...s, error: err.message }));
      throw err;
    }
  }

  function clearSelection() {
    update((s) => ({ ...s, selectedPeer: null }));
  }

  function setSelectedPeerConfig(
    peerId: string,
    config: PeerConfig,
    preferredTab?: PeerConfigTab,
  ) {
    update((s) => ({
      ...s,
      selectedPeerConfig: { peerId, config, preferredTab },
    }));
  }
  function setSelectedPeer(peer: Peer | null) {
    update((s) => ({ ...s, selectedPeer: peer }));
  }

  function refresh() {
    listPeers("", true);
  }
  function clearSelectedPeerConfig() {
    update((s) => ({ ...s, selectedPeerConfig: null }));
  }

  const peers = derived({ subscribe }, (s) => s.peers);
  const tokens = derived({ subscribe }, (s) => s.tokens);
  const selectedPeer = derived({ subscribe }, (s) => s.selectedPeer);
  const selectedPeerConfig = derived(
    { subscribe },
    (s) => s.selectedPeerConfig
  );
  const isLoading = derived({ subscribe }, (s) => s.isLoading);

  // Enrollment Token Actions
  async function listTokens(tenantId: string) {
    update((s) => ({ ...s, isLoading: true, error: null }));
    try {
      const response = await wsStore.callGRPC<{ tokens: EnrollmentToken[] }>(
        "TenantPortalService",
        "ListEnrollmentTokens",
        {
          tenant_id: tenantId,
        }
      );
      console.log("Token list response:", response);
      update((s) => ({
        ...s,
        tokens: response.tokens || [],
        isLoading: false,
      }));
    } catch (err: any) {
      update((s) => ({ ...s, isLoading: false, error: err.message }));
      throw err;
    }
  }

  async function createToken(
    tenantId: string,
    name: string,
    expiresInDays: number = 0,
    maxUses: number = 0
  ) {
    update((s) => ({ ...s, isLoading: true, error: null }));
    try {
      const response = await wsStore.callGRPC<{ token: EnrollmentToken }>(
        "TenantPortalService",
        "CreateEnrollmentToken",
        {
          tenant_id: tenantId,
          name,
          expires_in_days: expiresInDays,
          max_uses: maxUses,
        }
      );
      update((s) => ({
        ...s,
        tokens: [response.token, ...s.tokens],
        isLoading: false,
      }));
      return response.token;
    } catch (err: any) {
      update((s) => ({ ...s, isLoading: false, error: err.message }));
      throw err;
    }
  }

  async function deleteToken(tenantId: string, tokenId: string) {
    update((s) => ({ ...s, isLoading: true, error: null }));
    try {
      await wsStore.callGRPC("TenantPortalService", "DeleteEnrollmentToken", {
        tenant_id: tenantId,
        token_id: tokenId,
      });
      update((s) => ({
        ...s,
        tokens: s.tokens.filter((t) => t.id !== tokenId),
        isLoading: false,
      }));
    } catch (err: any) {
      update((s) => ({ ...s, isLoading: false, error: err.message }));
      throw err;
    }
  }

  return {
    subscribe,
    listPeers,
    getPeer,
    addPeer,
    removePeer,
    updatePeer,
    updatePeerNotes,
    updatePeerNotesDirectly: function (peerId: string, notes: string) {
      return this.updatePeerNotes(peerId, notes);
    },
    batchUpdatePeers,
    getPeerConfig,
    refresh,
    getPeerStats,
    pingPeer,
    startPortScan,
    stopPortScan,
    pausePortScan,
    resumePortScan,
    streamScanStatus,
    toggleNotification,
    clearSelection,
    getSelectedPeer: () => get({ subscribe }).selectedPeer,
    setSelectedPeerConfig,
    setSelectedPeer,
    clearSelectedPeerConfig,
    peers,
    selectedPeer,
    selectedPeerConfig,
    isLoading,
    tokens,
    listTokens,
    createToken,
    deleteToken,
    subscribeToPeer,
    unsubscribeFromPeer,
  };
}

export const peerStore = createPeerStore();
