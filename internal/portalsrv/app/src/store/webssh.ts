import { writable, derived, get } from "svelte/store";
import { wsStore } from "./websocket";
import { openedApps } from "./store";

export interface WebSSHSession {
  id: string;
  peer_id: string;
  peer_ip: string;
  username: string;
  hostname: string;
  ssh_port: number;
  terminal_cols: number;
  terminal_rows: number;
  started_at: number;
  status: "connected" | "disconnected" | "connecting";
  active: boolean;
  websocket_url: string;
  bytes_sent: number;
  bytes_recv: number;
  is_shared?: boolean;
  owner_name?: string;
  viewer_can_write?: boolean;
}

export interface ConnectionLog {
  message: string;
  type: "info" | "error" | "warning" | "success";
  timestamp: Date;
}

// Session Activity Log - matches proto SessionActivityLog
export interface SessionActivityLog {
  id: string;
  account_id: string;
  peer_id: string;
  session_id: string;
  session_type: "WEBSSH" | "WINBOX";
  event_type:
  | "SESSION_START"
  | "SESSION_END"
  | "COMMAND"
  | "MESSAGE"
  | "AUTH_SUCCESS"
  | "AUTH_FAILURE";
  timestamp: string;
  client_ip: string;
  user_agent: string;
  target_ip: string;
  target_port: number;
  ssh_username: string;
  winbox_access_token?: string;
  command?: string;
  message_direction?: string;
  message_number?: number;
  message_length?: number;
  bytes_sent: number;
  bytes_received: number;
  duration_ms: number;
}

export interface WebSSHState {
  sessions: WebSSHSession[];
  activeSessions: WebSSHSession[];
  openTerminals: WebSSHSession[];
  connectionLogs: ConnectionLog[];
  activityLogs: SessionActivityLog[];
  activityLogsLoading: boolean;
  isLoading: boolean;
  error: string | null;
  lastFetched: number;
}

const initialState: WebSSHState = {
  sessions: [],
  activeSessions: [],
  openTerminals: [],
  connectionLogs: [],
  activityLogs: [],
  activityLogsLoading: false,
  isLoading: false,
  error: null,
  lastFetched: 0,
};

const OPEN_TERMINALS_STORAGE_KEY = "webssh_open_terminals_v1";

function readStoredOpenTerminalIds(): string[] {
  if (typeof localStorage === "undefined") {
    return [];
  }

  try {
    const raw = localStorage.getItem(OPEN_TERMINALS_STORAGE_KEY);
    if (!raw) {
      return [];
    }

    const parsed = JSON.parse(raw);
    return Array.isArray(parsed)
      ? parsed.filter((value): value is string => typeof value === "string")
      : [];
  } catch {
    return [];
  }
}

function writeStoredOpenTerminalIds(sessionIds: string[]) {
  if (typeof localStorage === "undefined") {
    return;
  }

  try {
    localStorage.setItem(
      OPEN_TERMINALS_STORAGE_KEY,
      JSON.stringify(Array.from(new Set(sessionIds)))
    );
  } catch {
    // Ignore browser storage failures and keep runtime state working.
  }
}

function createWebSSHStore() {
  const { subscribe, set, update } = writable<WebSSHState>(initialState);

  function getBrowserUserAgent(): string {
    if (typeof navigator === "undefined") {
      return "";
    }
    return navigator.userAgent || "";
  }

  function normalizePeerIp(peerIp: string): string {
    return (peerIp || "").trim().replace(/\/32$/, "");
  }

  function sessionIdentity(session: WebSSHSession): string {
    return [
      session.peer_id || "",
      normalizePeerIp(session.peer_ip || ""),
      session.ssh_port || 22,
      session.username || "",
    ].join("|");
  }

  function dedupeSessionsByIdentity(sessions: WebSSHSession[]): WebSSHSession[] {
    const byIdentity = new Map<string, WebSSHSession>();

    for (const session of sessions) {
      const key = sessionIdentity(session);

      const existing = byIdentity.get(key);
      if (!existing) {
        byIdentity.set(key, session);
        continue;
      }

      const shouldReplace =
        (!existing.active && session.active) ||
        session.started_at > existing.started_at;

      if (shouldReplace) {
        byIdentity.set(key, session);
      }
    }

    return Array.from(byIdentity.values());
  }

  function restoreOpenTerminals(sessions: WebSSHSession[]) {
    const storedIds = readStoredOpenTerminalIds();
    if (storedIds.length === 0) {
      return;
    }

    const sessionsById = new Map(
      sessions
        .filter((session) => session.active)
        .map((session) => [session.id, session])
    );
    const restored = storedIds
      .map((id) => sessionsById.get(id))
      .filter((session): session is WebSSHSession => Boolean(session));

    writeStoredOpenTerminalIds(restored.map((session) => session.id));

    if (restored.length === 0) {
      return;
    }

    openedApps.update((apps) => {
      const next = [...apps];
      for (const session of restored) {
        const windowId = `SSHTerminal-${session.id}`;
        if (!next.includes(windowId)) {
          next.push(windowId);
        }
      }
      return next;
    });

    update((state) => {
      const merged = [...state.openTerminals];
      for (const session of restored) {
        if (!merged.some((item) => item.id === session.id)) {
          merged.push(session);
        }
      }

      return {
        ...state,
        openTerminals: merged,
      };
    });
  }

  function addConnectionLog(
    message: string,
    type: ConnectionLog["type"] = "info"
  ) {
    const now = new Date();
    const timestamp = now.toLocaleTimeString("en-US", { hour12: false });
    const logEntry: ConnectionLog = {
      message: `[${timestamp}] ${message}`,
      type,
      timestamp: now,
    };

    update((s) => ({
      ...s,
      connectionLogs: [...s.connectionLogs.slice(-99), logEntry], // Keep last 100
    }));
  }

  function clearLogs() {
    update((s) => ({ ...s, connectionLogs: [] }));
  }

  // Pre-load persisted WebSSH sessions on application startup
  async function initSessions() {
    try {
      const sessions = await listActiveSessions(true);
      restoreOpenTerminals(sessions);
    } catch (err) {
      console.warn("Failed to load persisted WebSSH sessions on startup:", err);
    }
  }

  async function createSession(
    peerId: string,
    peerIp: string,
    username: string,
    password?: string,
    sshPort?: number,
    cols?: number,
    rows?: number,
    privateKey?: string,
    privateKeyPassphrase?: string
  ): Promise<WebSSHSession> {
    update((s) => ({ ...s, isLoading: true, error: null }));
    addConnectionLog(` Creating SSH session to ${peerIp}...`, "info");

    try {
      const response = await wsStore.callGRPC<{
        session_id: string;
        websocket_url?: string;
        peer_ip?: string;
        ssh_port?: number;
      }>("TenantWebSSHService", "CreateTenantWebSSHSession", {
        tenant_id: "",
        peer_id: peerId,
        peer_ip: peerIp,
        ssh_port: sshPort || 22,
        username: username,
        password: password || "",
        private_key: privateKey || "",
        private_key_passphrase: privateKeyPassphrase || "",
        user_agent: getBrowserUserAgent(),
        terminal_cols: cols || 80,
        terminal_rows: rows || 24,
      });

      const session: WebSSHSession = {
        id: response.session_id || "",
        peer_id: peerId,
        peer_ip: response.peer_ip || peerIp,
        username,
        hostname: peerIp,
        ssh_port: response.ssh_port || sshPort || 22,
        terminal_cols: cols || 80,
        terminal_rows: rows || 24,
        started_at: Date.now(),
        status: "disconnected",
        active: false,
        websocket_url: response.websocket_url || "",
        bytes_sent: 0,
        bytes_recv: 0,
        is_shared: false,
        owner_name: "",
        viewer_can_write: true,
      };

      update((s) => ({
        ...s,
        sessions: dedupeSessionsByIdentity([
          ...s.sessions.filter(
            (item) =>
              item.id !== session.id &&
              sessionIdentity(item) !== sessionIdentity(session)
          ),
          session,
        ]),
        activeSessions: dedupeSessionsByIdentity(
          [
            ...s.activeSessions.filter(
              (item) =>
                item.id !== session.id &&
                sessionIdentity(item) !== sessionIdentity(session)
            ),
            session,
          ].filter((item) => item.active)
        ),
        isLoading: false,
      }));

      addConnectionLog(
        ` Session created: ${username}@${peerIp}:${session.ssh_port}`,
        "success"
      );
      return session;
    } catch (err: any) {
      addConnectionLog(`❌ Failed to create session: ${err.message}`, "error");
      update((s) => ({ ...s, error: err.message, isLoading: false }));
      throw err;
    }
  }

  async function openSession(
    peerId: string,
    target: string,
    username: string,
    password?: string,
    privateKey?: string,
    privateKeyPassphrase?: string
  ): Promise<WebSSHSession> {
    return createSession(
      peerId,
      target,
      username,
      password,
      undefined,
      undefined,
      undefined,
      privateKey,
      privateKeyPassphrase
    );
  }

  async function disconnectSession(sessionId: string) {
    update((s) => ({ ...s, isLoading: true, error: null }));
    addConnectionLog(` Disconnecting session ${sessionId}...`, "info");

    try {
      await wsStore.callGRPC<{ success: boolean }>(
        "TenantWebSSHService",
        "DisconnectTenantWebSSHSession",
        {
          session_id: sessionId,
          tenant_id: "",
        }
      );

      // Keep the session in the list — only refresh status.
      // Use deleteSession to fully remove a session.
      update((s) => ({ ...s, isLoading: false }));
      addConnectionLog(` Session disconnected`, "success");
    } catch (err: any) {
      addConnectionLog(`❌ Failed to disconnect: ${err.message}`, "error");
      update((s) => ({ ...s, error: err.message, isLoading: false }));
      throw err;
    }
  }

  async function deleteSession(sessionId: string) {
    update((s) => ({ ...s, isLoading: true, error: null }));
    addConnectionLog(` Deleting session ${sessionId}...`, "info");

    try {
      await wsStore.callGRPC<{ success: boolean }>(
        "TenantWebSSHService",
        "DisconnectTenantWebSSHSession",
        {
          session_id: sessionId,
          tenant_id: "",
        }
      );
    } catch {
      // Ignore disconnect errors — still remove from local list.
    }

    update((s) => ({
      ...s,
      sessions: s.sessions.filter((sess) => sess.id !== sessionId),
      activeSessions: s.activeSessions.filter((sess) => sess.id !== sessionId),
      openTerminals: s.openTerminals.filter((sess) => sess.id !== sessionId),
      isLoading: false,
    }));
    writeStoredOpenTerminalIds(
      get({ subscribe }).openTerminals
        .filter((sess) => sess.id !== sessionId)
        .map((sess) => sess.id)
    );
    addConnectionLog(` Session deleted`, "success");
  }

  async function listActiveSessions(
    forceRefresh = false
  ): Promise<WebSSHSession[]> {
    const currentState = get({ subscribe });
    const now = Date.now();
    const isStale = now - currentState.lastFetched > 5 * 60 * 1000; // 5 minutes cache

    // CACHE STRATEGY:
    if (!forceRefresh && currentState.sessions.length > 0 && !isStale) {
      // Return cached active sessions
      return currentState.activeSessions;
    }

    // Capture initial loading state only if empty
    if (currentState.sessions.length === 0) {
      update((s) => ({ ...s, isLoading: true, error: null }));
      addConnectionLog(` Loading sessions...`, "info");
    }

    try {
      const response = await wsStore.callGRPC<{ sessions: any[] }>(
        "TenantWebSSHService",
        "ListTenantWebSSHSessions",
        {
          tenant_id: "",
        }
      );
      console.log("[websshStore] ListTenantWebSSHSessions RAW Response:", response);

      // Normalize sessions from backend - handle both old and new field names
      const sessions = dedupeSessionsByIdentity((response.sessions || []).map(
        (s: any) => {
          // Parse started_at - can be protobuf Timestamp {seconds, nanos} or number
          let startedAt: number;
          if (
            typeof s.started_at === "object" &&
            typeof s.started_at?.seconds === "number"
          ) {
            startedAt = s.started_at.seconds * 1000; // Convert seconds to milliseconds
          } else if (typeof s.started_at === "number") {
            startedAt = s.started_at;
          } else {
            startedAt = Date.now();
          }

          // Construct websocket URL if not provided
          const wsProtocol =
            typeof window !== "undefined" &&
              window.location.protocol === "https:"
              ? "wss:"
              : "ws:";
          const wsHost =
            typeof window !== "undefined" ? window.location.host : "";
          const wsUrl =
            s.websocket_url ||
            s.websocketUrl ||
            `${wsProtocol}//${wsHost}/ws/ssh/${s.id}`;

          return {
            id: s.id || s.session_id || "",
            peer_id: s.peer_id || s.peerId || "",
            peer_ip: s.peer_ip || s.target_ip || "",
            username: s.username || s.ssh_username || "",
            hostname: s.hostname || s.peer_ip || s.target_ip || "",
            ssh_port: s.ssh_port || 22,
            terminal_cols: s.terminal_cols || s.cols || 80,
            terminal_rows: s.terminal_rows || s.rows || 24,
            started_at: startedAt,
            status: s.active
              ? ("connected" as const)
              : ("disconnected" as const),
            active: s.active === true,
            websocket_url: wsUrl,
            bytes_sent: s.bytes_sent || 0,
            bytes_recv: s.bytes_recv || 0,
            is_shared: s.is_shared ?? s.isShared ?? false,
            owner_name: s.owner_name || s.ownerName || "",
            viewer_can_write:
              s.viewer_can_write ?? s.viewerCanWrite ?? false,
          };
        }
      ));

      update((s) => ({
        ...s,
        sessions,
        activeSessions: sessions.filter((sess) => sess.active),
        isLoading: false,
        lastFetched: Date.now(),
      }));
      restoreOpenTerminals(sessions);

      if (currentState.sessions.length === 0) {
        addConnectionLog(` Loaded ${sessions.length} session(s)`, "success");
      }
      return sessions;
    } catch (err: any) {
      addConnectionLog(`❌ Failed to load sessions: ${err.message}`, "error");
      update((s) => ({ ...s, error: err.message, isLoading: false }));
      throw err;
    }
  }

  function updateSessionStatus(
    sessionId: string,
    status: "connected" | "disconnected" | "connecting"
  ) {
    const active = status === "connected";
    update((s) => {
      const sessions = s.sessions.map((sess) =>
        sess.id === sessionId ? { ...sess, status, active } : sess
      );
      return {
        ...s,
        sessions,
        activeSessions: sessions.filter((sess) => sess.active),
      };
    });
    addConnectionLog(
      ` Session ${sessionId.substring(0, 8)}... status: ${status}`,
      "info"
    );
  }

  function openTerminal(session: WebSSHSession): WebSSHSession {
    let persistedSessionId = session.id;
    let resolvedSession = session;

    update((s) => {
      const matchingSessions = s.sessions.filter(
        (item) =>
          item.id === session.id ||
          sessionIdentity(item) === sessionIdentity(session)
      );
      const freshestSession =
        matchingSessions.sort((left, right) => {
          if (left.active !== right.active) {
            return left.active ? -1 : 1;
          }
          if (left.started_at !== right.started_at) {
            return right.started_at - left.started_at;
          }
          if (left.id === session.id && right.id !== session.id) {
            return -1;
          }
          if (right.id === session.id && left.id !== session.id) {
            return 1;
          }
          return 0;
        })[0] || session;

      resolvedSession = freshestSession;
      persistedSessionId = freshestSession.id;

      const nextOpenTerminals = [
        ...s.openTerminals.filter(
          (item) =>
            item.id !== freshestSession.id &&
            sessionIdentity(item) !== sessionIdentity(freshestSession)
        ),
        freshestSession,
      ];

      const windowId = `SSHTerminal-${freshestSession.id}`;
      const replacedWindowIds = s.openTerminals
        .filter(
          (item) =>
            item.id !== freshestSession.id &&
            sessionIdentity(item) === sessionIdentity(freshestSession)
        )
        .map((item) => `SSHTerminal-${item.id}`);

      // Don't add if already open
      if (
        s.openTerminals.some(
          (t) =>
            t.id === freshestSession.id ||
            sessionIdentity(t) === sessionIdentity(freshestSession)
        )
      ) {
        openedApps.update((apps) => {
          const filtered = apps.filter((app) => !replacedWindowIds.includes(app));
          if (!filtered.includes(windowId)) {
            return [...filtered, windowId];
          }
          return filtered;
        });

        return {
          ...s,
          openTerminals: nextOpenTerminals,
        };
      }
      addConnectionLog(
        `🖥️ Opening terminal for ${freshestSession.username}@${freshestSession.peer_ip}`,
        "info"
      );

      // Also add to openedApps so Titlebar close works
      openedApps.update((apps) => {
        const filtered = apps.filter((app) => !replacedWindowIds.includes(app));
        if (!filtered.includes(windowId)) {
          return [...filtered, windowId];
        }
        return filtered;
      });

      return {
        ...s,
        openTerminals: nextOpenTerminals,
      };
    });

    const currentOpenTerminals = get({ subscribe }).openTerminals.filter(
      (item) => item.id !== persistedSessionId
    );
    writeStoredOpenTerminalIds(
      currentOpenTerminals.concat(
        get({ subscribe }).openTerminals.find(
          (item) => item.id === persistedSessionId
        ) || session
      ).map((item) => item.id)
    );

    return resolvedSession;
  }

  function closeTerminal(sessionId: string) {
    update((s) => {
      const openTerminals = s.openTerminals.filter((t) => t.id !== sessionId);
      writeStoredOpenTerminalIds(openTerminals.map((item) => item.id));
      return {
        ...s,
        openTerminals,
      };
    });

    // Also remove from openedApps
    const windowId = `SSHTerminal-${sessionId}`;
    openedApps.update((apps) => apps.filter((app) => app !== windowId));

    addConnectionLog(`🖥️ Terminal window closed`, "info");
  }

  // Fetch session activities from backend via GetTenantPeer - activities are on the Peer object
  async function getSessionActivities(
    peerId: string,
    sessionId?: string
  ): Promise<SessionActivityLog[]> {
    if (!peerId) {
      addConnectionLog(
        `❌ Cannot fetch activities: peer_id is required`,
        "error"
      );
      return [];
    }

    update((s) => ({ ...s, activityLogsLoading: true }));

    try {
      // Use GetTenantPeer RPC - returns Peer with ssh_activities and winbox_activities
      const response = await wsStore.callGRPC<{ peer: any }>(
        "TenantPortalService",
        "GetTenantPeer",
        {
          tenant_id: "", // Will be filled by backend from session
          peer_id: peerId,
        }
      );

      const peer = response.peer;
      if (!peer) {
        update((s) => ({ ...s, activityLogs: [], activityLogsLoading: false }));
        return [];
      }

      // Map PeerSSHActivity from backend to our SessionActivityLog interface
      const sshActivities: SessionActivityLog[] = (
        peer.ssh_activities || []
      ).map((a: any) => {
        // Parse timestamp from protobuf format
        let timestamp: string;
        if (typeof a.timestamp === "object" && a.timestamp?.seconds) {
          timestamp = new Date(a.timestamp.seconds * 1000).toISOString();
        } else if (typeof a.timestamp === "string") {
          timestamp = a.timestamp;
        } else {
          timestamp = new Date().toISOString();
        }

        // Determine event type based on end_time presence
        const hasEndTime =
          a.end_time &&
          (typeof a.end_time === "object" ? a.end_time.seconds > 0 : true);
        const eventType: SessionActivityLog["event_type"] = hasEndTime
          ? "SESSION_END"
          : "SESSION_START";

        return {
          id: a.session_id || "",
          account_id: "",
          peer_id: peerId,
          session_id: a.session_id || "",
          session_type: "WEBSSH" as const,
          event_type: eventType,
          timestamp,
          client_ip: a.client_ip || "",
          user_agent: a.user_agent || "",
          target_ip: peer.assigned_ip || peer.ip_address || "",
          target_port: 22,
          ssh_username: a.username || "",
          winbox_access_token: undefined,
          command: (a.commands || []).join("; "),
          message_direction: undefined,
          message_number: undefined,
          message_length: undefined,
          bytes_sent: Number(a.bytes_sent) || 0,
          bytes_received: Number(a.bytes_recv) || 0,
          duration_ms: Number(a.duration_ms) || 0,
        };
      });

      // Filter by session_id if provided
      const filteredActivities = sessionId
        ? sshActivities.filter((a) => a.session_id === sessionId)
        : sshActivities;

      update((s) => ({
        ...s,
        activityLogs: filteredActivities,
        activityLogsLoading: false,
      }));

      return filteredActivities;
    } catch (err: any) {
      addConnectionLog(
        `❌ Failed to fetch activities: ${err.message}`,
        "error"
      );
      update((s) => ({ ...s, activityLogsLoading: false }));
      return [];
    }
  }

  function exportLogs(): string {
    let state: WebSSHState;
    subscribe((s) => (state = s))();
    return state!.connectionLogs.map((log) => log.message).join("\n");
  }

  const sessions = derived({ subscribe }, (s) => s.sessions);
  const activeSessions = derived({ subscribe }, (s) => s.activeSessions);
  const openTerminals = derived({ subscribe }, (s) => s.openTerminals);
  const connectionLogs = derived({ subscribe }, (s) => s.connectionLogs);
  const activityLogs = derived({ subscribe }, (s) => s.activityLogs);
  const activityLogsLoading = derived(
    { subscribe },
    (s) => s.activityLogsLoading
  );
  const isLoading = derived({ subscribe }, (s) => s.isLoading);
  const error = derived({ subscribe }, (s) => s.error);

  return {
    subscribe,
    initSessions,
    createSession,
    openSession,
    closeSession: deleteSession,
    disconnectSession,
    deleteSession,
    listActiveSessions,
    getSessionActivities,
    updateSessionStatus,
    openTerminal,
    closeTerminal,
    addConnectionLog,
    clearLogs,
    exportLogs,
    sessions,
    activeSessions,
    openTerminals,
    connectionLogs,
    activityLogs,
    activityLogsLoading,
    isLoading,
    error,
  };
}

export const websshStore = createWebSSHStore();
export const websshApi = websshStore; // Alias for backwards compatibility
