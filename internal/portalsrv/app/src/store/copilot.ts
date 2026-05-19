import { writable, derived } from "svelte/store";
import { wsStore } from "./websocket";

// Mirrors internal/copilot.Turn
export interface CopilotTurn {
  role: "user" | "assistant";
  content: string;
  at: string;
}

export interface CopilotSession {
  session_id: string;
  role: "tenant" | "admin";
  created_at: string;
  last_active: string;
  history?: CopilotTurn[];
}

export interface CopilotStatus {
  configured: boolean;
  is_admin: boolean;
  can_configure: boolean;
}

export interface CopilotState {
  sessions: CopilotSession[];
  activeSessionId: string | null;
  sending: boolean;
  loading: boolean;
  error: string | null;
  status: CopilotStatus | null;
  savingKey: boolean;
}

const initialState: CopilotState = {
  sessions: [],
  activeSessionId: null,
  sending: false,
  loading: false,
  error: null,
  status: null,
  savingKey: false,
};

function createCopilotStore() {
  const { subscribe, update, set } = writable<CopilotState>(initialState);

  async function getStatus(): Promise<CopilotStatus> {
    const resp = await wsStore.callGRPC<CopilotStatus>(
      "CopilotService",
      "GetStatus",
      {},
    );
    update((s) => ({ ...s, status: resp }));
    return resp;
  }

  async function setApiKey(apiKey: string): Promise<CopilotStatus> {
    update((s) => ({ ...s, savingKey: true, error: null }));
    try {
      await wsStore.callGRPC<{ configured: boolean }>(
        "CopilotService",
        "SetAPIKey",
        { api_key: apiKey },
      );
      // Re-read status so the UI flips from "configure me" to chat.
      const status = await getStatus();
      update((s) => ({ ...s, savingKey: false }));
      return status;
    } catch (e: any) {
      update((s) => ({ ...s, savingKey: false, error: e?.message ?? String(e) }));
      throw e;
    }
  }

  async function listSessions(): Promise<CopilotSession[]> {
    update((s) => ({ ...s, loading: true, error: null }));
    try {
      const resp = await wsStore.callGRPC<{ sessions: CopilotSession[] }>(
        "CopilotService",
        "ListSessions",
        {},
      );
      const sessions = resp.sessions ?? [];
      update((s) => ({ ...s, sessions, loading: false }));
      return sessions;
    } catch (e: any) {
      update((s) => ({ ...s, error: e?.message ?? String(e), loading: false }));
      throw e;
    }
  }

  async function openSession(): Promise<CopilotSession> {
    try {
      const sess = await wsStore.callGRPC<CopilotSession>(
        "CopilotService",
        "OpenSession",
        {},
      );
      update((s) => ({
        ...s,
        sessions: [{ ...sess, history: [] }, ...s.sessions],
        activeSessionId: sess.session_id,
      }));
      return sess;
    } catch (e: any) {
      update((s) => ({ ...s, error: e?.message ?? String(e) }));
      throw e;
    }
  }

  async function loadSession(sessionId: string): Promise<CopilotSession> {
    try {
      const sess = await wsStore.callGRPC<CopilotSession>(
        "CopilotService",
        "GetSession",
        { session_id: sessionId },
      );
      update((s) => ({
        ...s,
        sessions: s.sessions.map((x) => (x.session_id === sessionId ? sess : x)),
        activeSessionId: sessionId,
      }));
      return sess;
    } catch (e: any) {
      update((s) => ({ ...s, error: e?.message ?? String(e) }));
      throw e;
    }
  }

  async function sendMessage(sessionId: string, text: string): Promise<string> {
    if (!text.trim()) return "";
    const now = new Date().toISOString();

    // Optimistically append the user turn so the UI updates immediately.
    update((s) => ({
      ...s,
      sending: true,
      error: null,
      sessions: s.sessions.map((x) =>
        x.session_id === sessionId
          ? {
              ...x,
              history: [
                ...(x.history ?? []),
                { role: "user", content: text, at: now },
              ],
            }
          : x,
      ),
    }));

    try {
      const resp = await wsStore.callGRPC<{ reply: string; session_id: string }>(
        "CopilotService",
        "SendMessage",
        { session_id: sessionId, text },
      );
      update((s) => ({
        ...s,
        sending: false,
        sessions: s.sessions.map((x) =>
          x.session_id === sessionId
            ? {
                ...x,
                history: [
                  ...(x.history ?? []),
                  {
                    role: "assistant",
                    content: resp.reply,
                    at: new Date().toISOString(),
                  },
                ],
                last_active: new Date().toISOString(),
              }
            : x,
        ),
      }));
      return resp.reply;
    } catch (e: any) {
      update((s) => ({ ...s, sending: false, error: e?.message ?? String(e) }));
      throw e;
    }
  }

  async function closeSession(sessionId: string): Promise<void> {
    try {
      await wsStore.callGRPC<{ ok: boolean }>(
        "CopilotService",
        "CloseSession",
        { session_id: sessionId },
      );
      update((s) => ({
        ...s,
        sessions: s.sessions.filter((x) => x.session_id !== sessionId),
        activeSessionId:
          s.activeSessionId === sessionId ? null : s.activeSessionId,
      }));
    } catch (e: any) {
      update((s) => ({ ...s, error: e?.message ?? String(e) }));
      throw e;
    }
  }

  function selectSession(sessionId: string | null) {
    update((s) => ({ ...s, activeSessionId: sessionId }));
  }

  function reset() {
    set(initialState);
  }

  const sessions = derived({ subscribe }, (s) => s.sessions);
  const activeSession = derived({ subscribe }, (s) =>
    s.sessions.find((x) => x.session_id === s.activeSessionId) ?? null,
  );
  const sending = derived({ subscribe }, (s) => s.sending);
  const loading = derived({ subscribe }, (s) => s.loading);
  const error = derived({ subscribe }, (s) => s.error);
  const status = derived({ subscribe }, (s) => s.status);
  const savingKey = derived({ subscribe }, (s) => s.savingKey);

  return {
    subscribe,
    getStatus,
    setApiKey,
    listSessions,
    openSession,
    loadSession,
    sendMessage,
    closeSession,
    selectSession,
    reset,
    sessions,
    activeSession,
    sending,
    loading,
    error,
    status,
    savingKey,
  };
}

export const copilotStore = createCopilotStore();
