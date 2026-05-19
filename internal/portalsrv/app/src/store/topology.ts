import { writable, derived } from "svelte/store";
import { wsStore } from "./websocket";
import type { OSFingerprint } from "./peer";

export interface TopologyNode {
  id: string;
  label: string;
  type:
  | "server"
  | "peer"
  | "internet"
  | "tenant"
  | "global_server"
  | "router"
  | number
  | string;
  status:
  | "online"
  | "offline"
  | "idle"
  | "error"
  | "inactive"
  | "unknown"
  | number
  | string;
  ip?: string;
  tenant_id?: string;
  public_key?: string;
  rx_bytes?: number;
  tx_bytes?: number;
  last_handshake?: number;
  groups?: string[];
  open_ports?: Array<
    | {
      port: number;
      protocol: string;
      service?: string;
    }
    | number
  >;
  fingerprint?: OSFingerprint;
}

export interface TopologyEdge {
  id: string;
  source: string;
  target: string;
  label?: string;
  bandwidth?: number;
  latency_ms?: number;
  packet_loss?: number;
  status: "active" | "inactive" | string;
}

export interface TopologyData {
  nodes: TopologyNode[];
  edges: TopologyEdge[];
}

export interface TopologyState {
  data: TopologyData;
  isLoading: boolean;
  error: string | null;
  lastUpdated: number;
  selectedPeersForGroupLink: Array<{ id: string; label: string; ip: string }>;
  selectedPeersForExitNode: Array<{ id: string; label: string; public_key: string; ip: string }>;
}

const initialState: TopologyState = {
  data: {
    nodes: [],
    edges: [],
  },
  isLoading: false,
  error: null,
  lastUpdated: 0,
  selectedPeersForGroupLink: [],
  selectedPeersForExitNode: [],
};

function createTopologyStore() {
  const { subscribe, set, update } = writable<TopologyState>(initialState);

  async function loadTopology() {
    update((s) => ({ ...s, isLoading: true, error: null }));
    try {
      const response = await wsStore.callGRPC<{ nodes: any[]; edges: any[] }>(
        "TenantNetworkService",
        "GetTenantTopology",
        {}
      );

      // console.log('Raw topology response:', response);

      function normalizeEnumString(value: string, prefix: string): string {
        const normalized = value.trim().toLowerCase();
        if (!normalized) return normalized;
        if (normalized.startsWith(prefix)) {
          return normalized.slice(prefix.length).toLowerCase();
        }
        return normalized;
      }

      // Map protobuf NodeType enum numbers to string names
      function mapNodeType(t: any): string {
        if (typeof t === "string" && t.length > 0) {
          return normalizeEnumString(t, "node_type_");
        }
        switch (Number(t)) {
          case 1: return "server";
          case 2: return "peer";
          case 3: return "router";
          case 4: return "global_server";
          default: return "peer";
        }
      }

      // Map protobuf NodeStatus enum numbers to string names
      function mapNodeStatus(s: any): string {
        if (typeof s === "string" && s.length > 0) {
          return normalizeEnumString(s, "node_status_");
        }
        switch (Number(s)) {
          case 1: return "online";
          case 2: return "offline";
          case 3: return "idle";
          case 4: return "error";
          default: return "offline";
        }
      }

      // Transform backend data to our format - backend already provides the right structure
      const nodes: TopologyNode[] = (response.nodes || []).map((node: any) => {
        // Normalize last_handshake: null for zero/invalid proto timestamps
        let lastHandshake: any = node.last_handshake || null;
        if (lastHandshake && typeof lastHandshake === 'object' && 'seconds' in lastHandshake) {
          // Go zero time sentinel: -62135596800; also reject zero/negative
          if (lastHandshake.seconds <= 0 || lastHandshake.seconds === -62135596800) {
            lastHandshake = null;
          }
        }
        return {
          id: node.id,
          label: node.label || node.id,
          type: mapNodeType(node.type),
          status: mapNodeStatus(node.status),
          ip: node.ip,
          tenant_id: node.account_id || node.tenant_id,
          public_key: node.public_key,
          rx_bytes: node.rx_bytes || 0,
          tx_bytes: node.tx_bytes || 0,
          last_handshake: lastHandshake,
          groups: node.groups || [],
          open_ports: node.open_ports || [],
          fingerprint: node.fingerprint,
        };
      });

      const edges: TopologyEdge[] = (response.edges || []).map((edge: any) => ({
        id: edge.id || `${edge.source}-${edge.target}`,
        source: edge.source,
        target: edge.target,
        label: edge.label,
        bandwidth: edge.bandwidth,
        latency_ms: edge.latency_ms,
        packet_loss: edge.packet_loss,
        status: edge.status || (edge.active === true ? "active" : edge.active === false ? "inactive" : "inactive"),
      }));

      update((s) => ({
        ...s,
        data: { nodes, edges },
        isLoading: false,
        lastUpdated: Date.now(),
      }));

      return { nodes, edges };
    } catch (err: any) {
      update((s) => ({ ...s, error: err.message, isLoading: false }));
      throw err;
    }
  }

  function handleTopologyEvent(event: any) {
    if (event.type === "topology_update" || event.type === "peer_updated") {
      // Reload topology on updates
      loadTopology();
    }
  }

  function setSelectedPeersForGroupLink(
    peers: Array<{ id: string; label: string; ip: string }>
  ) {
    update((s) => ({ ...s, selectedPeersForGroupLink: peers }));
  }

  function clearSelectedPeersForGroupLink() {
    update((s) => ({ ...s, selectedPeersForGroupLink: [] }));
  }

  function setSelectedPeersForExitNode(
    peers: Array<{ id: string; label: string; public_key: string; ip: string }>
  ) {
    update((s) => ({ ...s, selectedPeersForExitNode: peers }));
  }

  function clearSelectedPeersForExitNode() {
    update((s) => ({ ...s, selectedPeersForExitNode: [] }));
  }

  // Subscribe to WebSocket events
  // Note: Event subscription handled by WebSocket store
  // Events are received through the wsStore and handled by the message handler

  const topologyStore = {
    subscribe,
    loadTopology,
    refresh: loadTopology,
    setSelectedPeersForGroupLink,
    clearSelectedPeersForGroupLink,
    setSelectedPeersForExitNode,
    clearSelectedPeersForExitNode,
  };

  // Derived stores for convenience
  const nodes = derived(topologyStore, (s) => s.data.nodes);
  const edges = derived(topologyStore, (s) => s.data.edges);
  const isLoading = derived(topologyStore, (s) => s.isLoading);
  const selectedPeersForGroupLink = derived(
    topologyStore,
    (s) => s.selectedPeersForGroupLink
  );
  const selectedPeersForExitNode = derived(
    topologyStore,
    (s) => s.selectedPeersForExitNode
  );

  return {
    ...topologyStore,
    nodes,
    edges,
    isLoading,
    selectedPeersForGroupLink,
    selectedPeersForExitNode,
  };
}

export const topologyStore = createTopologyStore();
