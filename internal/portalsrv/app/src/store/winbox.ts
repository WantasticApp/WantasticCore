import { writable, derived, get } from "svelte/store";
import { wsStore } from "./websocket";

export interface WinboxSession {
  id: string;
  peer_id: string;
  router_ip: string;
  router_port: number;
  username: string;
  started_at: number;
  status: "connected" | "disconnected";
  proxy_url?: string;
  is_shared?: boolean;
  owner_name?: string;
  viewer_can_write?: boolean;
}

export interface WinboxState {
  sessions: WinboxSession[];
  activeSessions: WinboxSession[];
  isLoading: boolean;
  error: string | null;
}

const initialState: WinboxState = {
  sessions: [],
  activeSessions: [],
  isLoading: false,
  error: null,
};

function createWinboxStore() {
  const { subscribe, set, update } = writable<WinboxState>(initialState);

  async function openProxy(
    peerId: string,
    routerIp: string,
    routerPort: number,
    username: string,
    password: string
  ): Promise<WinboxSession> {
    update((s) => ({ ...s, isLoading: true, error: null }));
    try {
      const response = await wsStore.callGRPC<{
        session?: {
          id?: string;
          peer_id?: string;
          router_ip?: string;
          created_at?: { seconds?: number };
          is_shared?: boolean;
          owner_name?: string;
          viewer_can_write?: boolean;
        };
      }>("TenantPeerService", "CreateTenantWinboxSession", {
        tenant_id: "",
        peer_id: peerId,
        router_ip: routerIp,
        port: routerPort,
        username,
        password,
      });
      const createdAt =
        typeof response.session?.created_at?.seconds === "number"
          ? response.session.created_at.seconds * 1000
          : Date.now();
      const session: WinboxSession = {
        id: response.session?.id || "",
        peer_id: response.session?.peer_id || peerId,
        router_ip: response.session?.router_ip || routerIp,
        router_port: routerPort,
        username,
        started_at: createdAt,
        status: "connected",
        proxy_url: "",
        is_shared: response.session?.is_shared ?? false,
        owner_name: response.session?.owner_name || "",
        viewer_can_write: response.session?.viewer_can_write ?? false,
      };
      update((s) => ({
        ...s,
        sessions: [...s.sessions, session],
        activeSessions: [...s.activeSessions, session],
        isLoading: false,
      }));
      return session;
    } catch (err: any) {
      update((s) => ({ ...s, error: err.message, isLoading: false }));
      throw err;
    }
  }

  async function closeProxy(sessionId: string) {
    update((s) => ({ ...s, isLoading: true, error: null }));
    try {
      const currentState = get({ subscribe });
      const session =
        currentState.sessions.find((item) => item.id === sessionId) ||
        currentState.activeSessions.find((item) => item.id === sessionId);
      if (!session?.peer_id) {
        throw new Error("peer_id is required to delete a Winbox session");
      }
      await wsStore.callGRPC<{ success: boolean }>(
        "TenantPeerService",
        "DeleteTenantWinboxSession",
        {
          peer_id: session.peer_id,
          session_id: sessionId,
          tenant_id: "",
        }
      );
      update((s) => ({
        ...s,
        sessions: s.sessions.filter((sess) => sess.id !== sessionId),
        activeSessions: s.activeSessions.filter(
          (sess) => sess.id !== sessionId
        ),
        isLoading: false,
      }));
    } catch (err: any) {
      update((s) => ({ ...s, error: err.message, isLoading: false }));
      throw err;
    }
  }

  async function listActiveSessions() {
    try {
      const response = await wsStore.callGRPC<{ sessions: WinboxSession[] }>(
        "TenantPeerService",
        "ListTenantWinboxSessions",
        {
          tenant_id: "",
        }
      );
      const sessions: WinboxSession[] = (response.sessions || []).map(
        (session: any) => {
          const createdAt =
            typeof session.created_at === "object" &&
            typeof session.created_at?.seconds === "number"
              ? session.created_at.seconds * 1000
              : Date.now();
          const status: WinboxSession["status"] =
            session.enabled === false ? "disconnected" : "connected";

          return {
            id: session.id || "",
            peer_id: session.peer_id || "",
            router_ip: session.router_ip || "",
            router_port: session.router_port || 8291,
            username: session.username || "",
            started_at: createdAt,
            status,
            proxy_url: session.proxy_url || "",
            is_shared: session.is_shared ?? session.isShared ?? false,
            owner_name: session.owner_name || session.ownerName || "",
            viewer_can_write:
              session.viewer_can_write ?? session.viewerCanWrite ?? false,
          };
        }
      );
      update((s) => ({ ...s, sessions, activeSessions: sessions }));
      return sessions;
    } catch (err: any) {
      update((s) => ({ ...s, error: err.message }));
      throw err;
    }
  }

  async function openSession(
    peerId: string,
    target: string,
    username: string,
    password: string
  ): Promise<WinboxSession> {
    return openProxy(peerId, target, 8291, username, password);
  }

  async function closeSession(sessionId: string) {
    return closeProxy(sessionId);
  }

  async function createProxy(
    peerId: string,
    config: {
      name?: string;
      routerIP: string;
      routerPort: number;
      username: string;
      password: string;
    }
  ): Promise<{ proxyUrl: string; session: WinboxSession }> {
    update((s) => ({ ...s, isLoading: true, error: null }));
    try {
      const response = await wsStore.callGRPC<{
        session?: {
          id?: string;
          peer_id?: string;
          router_ip?: string;
          created_at?: { seconds?: number };
          is_shared?: boolean;
          owner_name?: string;
          viewer_can_write?: boolean;
        };
      }>("TenantPeerService", "CreateTenantWinboxSession", {
        tenant_id: "",
        peer_id: peerId,
        name: config.name || "Winbox Session",
        router_ip: config.routerIP,
        port: config.routerPort,
        username: config.username,
        password: config.password,
      });
      const createdAt =
        typeof response.session?.created_at?.seconds === "number"
          ? response.session.created_at.seconds * 1000
          : Date.now();
      const session: WinboxSession = {
        id: response.session?.id || "",
        peer_id: response.session?.peer_id || peerId,
        router_ip: response.session?.router_ip || config.routerIP,
        router_port: config.routerPort,
        username: config.username,
        started_at: createdAt,
        status: "connected",
        proxy_url: "",
        is_shared: response.session?.is_shared ?? false,
        owner_name: response.session?.owner_name || "",
        viewer_can_write: response.session?.viewer_can_write ?? false,
      };
      update((s) => ({
        ...s,
        sessions: [...s.sessions, session],
        activeSessions: [...s.activeSessions, session],
        isLoading: false,
      }));
      return { proxyUrl: "", session };
    } catch (err: any) {
      update((s) => ({ ...s, error: err.message, isLoading: false }));
      throw err;
    }
  }

  const sessions = derived({ subscribe }, (s) => s.sessions);
  const activeSessions = derived({ subscribe }, (s) => s.activeSessions);
  const isLoading = derived({ subscribe }, (s) => s.isLoading);

  return {
    subscribe,
    openProxy,
    closeProxy,
    openSession,
    closeSession,
    createProxy,
    listActiveSessions,
    sessions,
    activeSessions,
    isLoading,
  };
}

export const winboxStore = createWinboxStore();
export const winboxApi = winboxStore; // Alias for backwards compatibility
