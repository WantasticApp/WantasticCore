import { writable, get } from "svelte/store";
import { wsStore } from "./websocket";
import { openedApps } from "./store";

export interface WebProxySession {
  id: string;
  tenant_id: string;
  peer_id: string;
  peer_ip: string;
  port: number;
  use_https: boolean;
  base_url: string;
  created_at: number;
  last_active: number;
  active: boolean;
  requests_count: number;
  bytes_sent: number;
  bytes_received: number;
}

export interface WebProxyRequest {
  request_id: string;
  method: string;
  path: string;
  query: string;
  headers: Record<string, string>;
  body?: Uint8Array;
  content_type?: string;
}

export interface WebProxyResponse {
  request_id: string;
  status_code: number;
  status_text: string;
  headers: Record<string, string>;
  body: Uint8Array;
  content_type: string;
  content_length: number;
  is_final: boolean;
  chunk_index: number;
}

export interface WebProxyError {
  code: string;
  message: string;
  retryable: boolean;
}

export interface BrowserTab {
  id: string;
  session_id: string;
  url: string;
  title: string;
  loading: boolean;
  error: string | null;
  history: string[];
  historyIndex: number;
}

export interface WebProxyState {
  sessions: WebProxySession[];
  openBrowsers: BrowserTab[];
  isMinimized: boolean;
  isLoading: boolean;
  error: string | null;
}

const initialState: WebProxyState = {
  sessions: [],
  openBrowsers: [],
  isMinimized: false,
  isLoading: false,
  error: null,
};

// Cookie jar: Map of sessionId -> Map of cookieName -> cookieValue
// This stores cookies per session to maintain authentication state
const sessionCookies: Map<string, Map<string, string>> = new Map();

// Parse a single Set-Cookie header value and store the cookie
function parseSingleCookie(
  cookies: Map<string, string>,
  cookieStr: string,
  sessionId: string
): void {
  cookieStr = cookieStr.trim();
  if (!cookieStr) return;

  // Get the name=value part (before first semicolon)
  const semicolonIndex = cookieStr.indexOf(";");
  const nameValue =
    semicolonIndex >= 0 ? cookieStr.substring(0, semicolonIndex) : cookieStr;
  const equalsIndex = nameValue.indexOf("=");

  if (equalsIndex > 0) {
    const name = nameValue.substring(0, equalsIndex).trim();
    const value = nameValue.substring(equalsIndex + 1).trim();

    // Check for expiry/deletion
    if (
      cookieStr.toLowerCase().includes("max-age=0") ||
      cookieStr.toLowerCase().includes("expires=thu, 01 jan 1970")
    ) {
      cookies.delete(name);
      //console.log(`[CookieJar] Deleted cookie ${name} for session ${sessionId}`);
    } else {
      cookies.set(name, value);
      //console.log(`[CookieJar] Set cookie ${name}=${value.substring(0, 20)}${value.length > 20 ? '...' : ''} for session ${sessionId}`);
    }
  }
}

// Parse Set-Cookie header and store cookies for the session
// The header may contain multiple cookies separated by newline (from multiple Set-Cookie headers)
export function handleSetCookie(
  sessionId: string,
  setCookieHeader: string | undefined
): void {
  if (!setCookieHeader) return;

  //console.log(`[CookieJar] Processing Set-Cookie for session ${sessionId}:`, setCookieHeader.substring(0, 100));

  // Get or create cookie jar for this session
  let cookies = sessionCookies.get(sessionId);
  if (!cookies) {
    cookies = new Map();
    sessionCookies.set(sessionId, cookies);
  }

  // Multiple Set-Cookie headers are joined with newline by the Go backend
  const cookieStrings = setCookieHeader.split("\n");

  for (const cookieStr of cookieStrings) {
    parseSingleCookie(cookies, cookieStr, sessionId);
  }
}

// Get cookies for a session as a Cookie header value
export function getCookieHeader(sessionId: string): string | undefined {
  const cookies = sessionCookies.get(sessionId);
  if (!cookies || cookies.size === 0) return undefined;

  const parts: string[] = [];
  for (const [name, value] of cookies) {
    parts.push(`${name}=${value}`);
  }

  const header = parts.join("; ");
  //console.log(`[CookieJar] Sending cookies for session ${sessionId}: ${header.substring(0, 50)}...`);
  return header;
}

// Clear cookies for a session (when session is closed)
export function clearSessionCookies(sessionId: string): void {
  sessionCookies.delete(sessionId);
  //console.log(`[CookieJar] Cleared cookies for session ${sessionId}`);
}

function createWebProxyStore() {
  const { subscribe, set, update } = writable<WebProxyState>(initialState);

  async function createSession(
    peerId: string,
    peerIp: string,
    port: number,
    useHttps: boolean = false,
    skipTlsVerify: boolean = true
  ): Promise<WebProxySession> {
    // Check for existing active session for the same peer/port
    const state = get({ subscribe });
    const existingSession = state.sessions.find(
      (s) => s.peer_ip === peerIp && s.port === port && s.active
    );

    if (existingSession) {
      //console.log(`♻️ Reusing existing WebProxy session for ${peerIp}:${port}`);
      // Update last_active
      update((s) => ({
        ...s,
        sessions: s.sessions.map((session) =>
          session.id === existingSession.id
            ? { ...session, last_active: Date.now() }
            : session
        ),
      }));
      return existingSession;
    }

    update((s) => ({ ...s, isLoading: true, error: null }));

    try {
      const response = await wsStore.callGRPC<{
        session_id: string;
        success: boolean;
        error_message?: string;
        base_url: string;
      }>("WebProxyService", "CreateWebProxySession", {
        tenant_id: "",
        peer_id: peerId,
        peer_ip: peerIp,
        port: port,
        use_https: useHttps,
        skip_tls_verify: skipTlsVerify,
      });

      if (!response.success) {
        throw new Error(response.error_message || "Failed to create session");
      }

      const session: WebProxySession = {
        id: response.session_id,
        tenant_id: "",
        peer_id: peerId,
        peer_ip: peerIp,
        port: port,
        use_https: useHttps,
        base_url: response.base_url,
        created_at: Date.now(),
        last_active: Date.now(),
        active: true,
        requests_count: 0,
        bytes_sent: 0,
        bytes_received: 0,
      };

      update((s) => ({
        ...s,
        sessions: [...s.sessions, session],
        isLoading: false,
      }));

      //console.log(` WebProxy session created: ${response.base_url}`);
      return session;
    } catch (error) {
      const message = error instanceof Error ? error.message : "Unknown error";
      update((s) => ({ ...s, isLoading: false, error: message }));
      throw error;
    }
  }

  async function closeSession(sessionId: string): Promise<void> {
    try {
      await wsStore.callGRPC("WebProxyService", "CloseWebProxySession", {
        session_id: sessionId,
      });

      // Clear cookies for this session
      clearSessionCookies(sessionId);

      update((s) => ({
        ...s,
        sessions: s.sessions.filter((session) => session.id !== sessionId),
        openBrowsers: s.openBrowsers.filter(
          (tab) => tab.session_id !== sessionId
        ),
      }));

      //console.log(` WebProxy session closed: ${sessionId}`);
    } catch (error) {
      console.error("Failed to close session:", error);
      throw error;
    }
  }

  async function listSessions(): Promise<WebProxySession[]> {
    update((s) => ({ ...s, isLoading: true }));

    try {
      const response = await wsStore.callGRPC<{ sessions: WebProxySession[] }>(
        "WebProxyService",
        "ListWebProxySessions",
        { tenant_id: "" }
      );

      const sessions = response.sessions || [];
      update((s) => ({
        ...s,
        sessions,
        isLoading: false,
      }));

      return sessions;
    } catch (error) {
      update((s) => ({ ...s, isLoading: false }));
      throw error;
    }
  }

  // proxyRequest was the unary HTTP-over-gRPC fallback. It is gone — all
  // proxied traffic now flows through WebProxyMux (see webproxy-mux.ts),
  // which streams response chunks instead of buffering whole bodies.

  // Recreate an expired session using the same peer info
  async function recreateSession(
    oldSessionId: string
  ): Promise<WebProxySession | null> {
    const state = get({ subscribe });
    const oldSession = state.sessions.find((s) => s.id === oldSessionId);

    if (!oldSession) {
      console.warn(
        "[WebProxy] Cannot recreate session - no session info found"
      );
      return null;
    }

    try {
      // Create a new session with the same parameters
      const newSession = await createSession(
        oldSession.peer_id,
        oldSession.peer_ip,
        oldSession.port,
        oldSession.use_https
      );

      // Update all browser tabs that were using the old session
      update((s) => ({
        ...s,
        sessions: s.sessions.filter((sess) => sess.id !== oldSessionId),
        openBrowsers: s.openBrowsers.map((tab) =>
          tab.session_id === oldSessionId
            ? { ...tab, session_id: newSession.id }
            : tab
        ),
      }));

      //console.log(` Session recreated: ${oldSessionId} -> ${newSession.id}`);
      return newSession;
    } catch (error) {
      console.error("[WebProxy] Failed to recreate session:", error);
      return null;
    }
  }

  // Open a browser tab for a session
  function openBrowser(
    session: WebProxySession,
    initialUrl: string = "/"
  ): BrowserTab {
    // Check if there's already a browser tab for this session
    const state = get({ subscribe });
    const existingTab = state.openBrowsers.find(
      (t) => t.session_id === session.id
    );

    if (existingTab) {
      //console.log(`♻️ Reusing existing browser tab for session ${session.id}`);
      // Just ensure the WebBrowser app is in opened apps
      const windowId = "WebBrowser";
      openedApps.update((apps) => {
        if (!apps.includes(windowId)) {
          return [...apps, windowId];
        }
        return apps;
      });
      // Restore from minimized state
      update((s) => ({ ...s, isMinimized: false }));
      return existingTab;
    }

    const tabId = `browser-${session.id}-${Date.now()}`;

    const tab: BrowserTab = {
      id: tabId,
      session_id: session.id,
      url: initialUrl,
      title: `${session.peer_ip}:${session.port}`,
      loading: false,
      error: null,
      history: [initialUrl],
      historyIndex: 0,
    };

    update((s) => ({
      ...s,
      openBrowsers: [...s.openBrowsers, tab],
      isMinimized: false, // Ensure not minimized when opening new browser
    }));

    // Add WebBrowser to opened apps (single app, not per-tab)
    const windowId = "WebBrowser";
    openedApps.update((apps) => {
      if (!apps.includes(windowId)) {
        return [...apps, windowId];
      }
      return apps;
    });

    return tab;
  }

  function closeBrowser(tabId: string) {
    update((s) => {
      const newOpenBrowsers = s.openBrowsers.filter((tab) => tab.id !== tabId);

      // If no more browsers open, remove from opened apps
      if (newOpenBrowsers.length === 0) {
        openedApps.update((apps) => apps.filter((app) => app !== "WebBrowser"));
      }

      return {
        ...s,
        openBrowsers: newOpenBrowsers,
      };
    });
  }

  function updateBrowserTab(tabId: string, updates: Partial<BrowserTab>) {
    update((s) => ({
      ...s,
      openBrowsers: s.openBrowsers.map((tab) =>
        tab.id === tabId ? { ...tab, ...updates } : tab
      ),
    }));
  }

  // Navigate to a URL in a browser tab
  async function navigate(tabId: string, url: string): Promise<void> {
    const state = get({ subscribe });
    const tab = state.openBrowsers.find((t) => t.id === tabId);
    if (!tab) {
      throw new Error("Tab not found");
    }

    const session = state.sessions.find((s) => s.id === tab.session_id);
    if (!session) {
      throw new Error("Session not found");
    }

    // Update tab state and history. The actual fetch is driven by
    // WebBrowser.svelte through WebProxyMux — this store only tracks
    // tab/history bookkeeping.
    const newHistory = [...tab.history.slice(0, tab.historyIndex + 1), url];
    updateBrowserTab(tabId, {
      loading: true,
      error: null,
      url,
      history: newHistory,
      historyIndex: newHistory.length - 1,
    });
  }

  // Navigate back in history
  function goBack(tabId: string) {
    const state = get({ subscribe });
    const tab = state.openBrowsers.find((t) => t.id === tabId);
    if (!tab || tab.historyIndex <= 0) return;

    const newIndex = tab.historyIndex - 1;
    const url = tab.history[newIndex];
    updateBrowserTab(tabId, { historyIndex: newIndex, url });
    navigate(tabId, url);
  }

  // Navigate forward in history
  function goForward(tabId: string) {
    const state = get({ subscribe });
    const tab = state.openBrowsers.find((t) => t.id === tabId);
    if (!tab || tab.historyIndex >= tab.history.length - 1) return;

    const newIndex = tab.historyIndex + 1;
    const url = tab.history[newIndex];
    updateBrowserTab(tabId, { historyIndex: newIndex, url });
    navigate(tabId, url);
  }

  // Refresh current page
  function refresh(tabId: string) {
    const state = get({ subscribe });
    const tab = state.openBrowsers.find((t) => t.id === tabId);
    if (!tab) return;

    navigate(tabId, tab.url);
  }

  function minimize() {
    update((s) => ({ ...s, isMinimized: true }));
  }

  function restore() {
    update((s) => ({ ...s, isMinimized: false }));
  }

  return {
    subscribe,
    createSession,
    closeSession,
    listSessions,
    openBrowser,
    closeBrowser,
    updateBrowserTab,
    navigate,
    goBack,
    goForward,
    refresh,
    minimize,
    restore,
    reset: () => set(initialState),
  };
}

export const webProxyStore = createWebProxyStore();
