import { writable } from "svelte/store";
import { wsStore } from "./websocket";

export type RouterOSResourceKey =
  | "addresses"
  | "routes"
  | "firewall"
  | "packages"
  | "files"
  | "wireless"
  | "tr069"
  | "bridge";

export interface RouterOSCapability {
  candidate?: boolean;
  api_ready?: boolean;
  api_port?: number;
  api_tls?: boolean;
  last_validated?: { seconds?: number; nanos?: number } | string;
  last_error?: string;
  session_id?: string;
  has_saved_winbox?: boolean;
  has_saved_access?: boolean;
  credential_source?: string;
  preferred_username?: string;
}

export interface RouterOSIdentity {
  identity?: string;
  version?: string;
  board_name?: string;
  model?: string;
  platform?: string;
  architecture?: string;
  cpu?: string;
}

export interface RouterOSRecord {
  id: string;
  fields: Record<string, string>;
}

export interface RouterOSOverview {
  capability?: RouterOSCapability;
  identity?: RouterOSIdentity;
  system_resource?: Record<string, string>;
  routerboard?: Record<string, string>;
}

type StreamStatus = "idle" | "connecting" | "ready" | "closed" | "error";

interface RouterOSResourceSnapshot {
  resource?: number;
  success?: boolean;
  error?: string;
  records?: RouterOSRecord[];
}

interface RouterOSNotice {
  action?: string;
  resource?: number;
  success?: boolean;
  error?: string;
  id?: string;
}

interface RouterOSStreamState {
  success?: boolean;
  error?: string;
  connected?: boolean;
  access_required?: boolean;
  capability?: RouterOSCapability;
  identity?: RouterOSIdentity;
  system_resource?: Record<string, string>;
  routerboard?: Record<string, string>;
}

interface RouterOSState {
  activePeerId: string;
  sessionId: string | null;
  streamStatus: StreamStatus;
  overview: RouterOSOverview | null;
  records: Partial<Record<RouterOSResourceKey, RouterOSRecord[]>>;
  counts: Partial<Record<RouterOSResourceKey, number | null>>;
  resourceErrors: Partial<Record<RouterOSResourceKey, string | null>>;
  sectionLoading: Partial<Record<RouterOSResourceKey, boolean>>;
  currentResource: RouterOSResourceKey | null;
  isLoading: boolean;
  isSavingAccess: boolean;
  error: string | null;
  notice: string | null;
}

const RESOURCE_ENUM: Record<RouterOSResourceKey, number> = {
  addresses: 1,
  routes: 2,
  firewall: 3,
  packages: 4,
  files: 5,
  wireless: 6,
  tr069: 7,
  bridge: 8,
};

const RESOURCE_KEY_BY_ENUM: Partial<Record<number, RouterOSResourceKey>> = Object.fromEntries(
  Object.entries(RESOURCE_ENUM).map(([key, value]) => [value, key as RouterOSResourceKey]),
) as Partial<Record<number, RouterOSResourceKey>>;

const RESOURCE_KEYS = Object.keys(RESOURCE_ENUM) as RouterOSResourceKey[];

const buildEmptyCounts = (): Partial<Record<RouterOSResourceKey, number | null>> =>
  Object.fromEntries(RESOURCE_KEYS.map((key) => [key, null])) as Partial<
    Record<RouterOSResourceKey, number | null>
  >;

const initialState: RouterOSState = {
  activePeerId: "",
  sessionId: null,
  streamStatus: "idle",
  overview: null,
  records: {},
  counts: buildEmptyCounts(),
  resourceErrors: {},
  sectionLoading: {},
  currentResource: null,
  isLoading: false,
  isSavingAccess: false,
  error: null,
  notice: null,
};

function createRouterOSStore() {
  const { subscribe, set, update } = writable<RouterOSState>(initialState);
  let stateSnapshot: RouterOSState = initialState;
  subscribe((value) => {
    stateSnapshot = value;
  });

  let desiredPeerId = "";
  let desiredResource: RouterOSResourceKey | null = null;
  let currentSessionId: string | null = null;
  let lastConnectionGeneration = -1;
  let queuedCommand: Record<string, unknown> | null = null;
  let reconnectTimer: ReturnType<typeof setTimeout> | null = null;
  let prefetchTimer: ReturnType<typeof setTimeout> | null = null;
  let liveRefreshTimer: ReturnType<typeof setInterval> | null = null;
  let warmedSessionId: string | null = null;
  let liveRefreshTick = 0;
  let prefetchQueue: RouterOSResourceKey[] = [];
  let lastActivityAt = 0;
  let reconnectAttempts = 0;

  const STREAM_GRACE_MS = 30_000;

  const recentlyHealthy = () =>
    lastActivityAt > 0 && Date.now() - lastActivityAt < STREAM_GRACE_MS;

  const normalizeResourceKey = (resource?: number): RouterOSResourceKey | null =>
    resource ? RESOURCE_KEY_BY_ENUM[resource] || null : null;

  const sameData = (left: unknown, right: unknown) =>
    JSON.stringify(left ?? null) === JSON.stringify(right ?? null);

  const buildSessionId = (peerId: string) =>
    `routeros-${peerId}-${Date.now()}-${Math.random().toString(36).slice(2, 8)}`;

  const pendingResourceKeys = () =>
    RESOURCE_KEYS.filter(
      (key) =>
        stateSnapshot.counts[key] === null && !stateSnapshot.sectionLoading[key],
    );

  const clearReconnectTimer = () => {
    if (reconnectTimer) {
      clearTimeout(reconnectTimer);
      reconnectTimer = null;
    }
  };

  const clearPrefetchTimer = () => {
    if (prefetchTimer) {
      clearTimeout(prefetchTimer);
      prefetchTimer = null;
    }
    prefetchQueue = [];
  };

  const clearLiveRefreshTimer = () => {
    if (liveRefreshTimer) {
      clearInterval(liveRefreshTimer);
      liveRefreshTimer = null;
    }
    liveRefreshTick = 0;
  };

  const resetState = (peerId = "") => {
    clearReconnectTimer();
    clearPrefetchTimer();
    clearLiveRefreshTimer();
    warmedSessionId = null;
    lastActivityAt = 0;
    reconnectAttempts = 0;
    set({
      ...initialState,
      activePeerId: peerId,
      streamStatus: peerId ? "connecting" : "idle",
      isLoading: !!peerId,
      counts: buildEmptyCounts(),
    });
  };

  const sendLoadRequest = (
    resource: RouterOSResourceKey,
    forceReload = false,
    options: { makeCurrent?: boolean; markLoading?: boolean } = {},
  ) => {
    const { makeCurrent = false, markLoading = true } = options;
    if (makeCurrent) {
      desiredResource = resource;
    }

    update((s) => ({
      ...s,
      currentResource: makeCurrent ? resource : s.currentResource,
      error: null,
      sectionLoading: markLoading
        ? {
            ...s.sectionLoading,
            [resource]: true,
          }
        : s.sectionLoading,
    }));

    const payload = {
      load_resource: {
        resource: RESOURCE_ENUM[resource],
        force_reload: forceReload,
      },
    };

    if (!currentSessionId) {
      return sendOrQueue(payload);
    }

    return wsStore.sendRouterOSCommand(currentSessionId, payload);
  };

  const warmResourceCache = () => {
    if (!desiredPeerId || !currentSessionId || warmedSessionId === currentSessionId) {
      return;
    }

    warmedSessionId = currentSessionId;
    clearPrefetchTimer();
    prefetchQueue = RESOURCE_KEYS.filter((key) => key !== desiredResource);

    const runNextPrefetch = () => {
      if (!currentSessionId || currentSessionId !== warmedSessionId) {
        return;
      }
      const key = prefetchQueue.shift();
      if (!key) {
        prefetchTimer = null;
        return;
      }
      sendLoadRequest(key, false, { makeCurrent: false, markLoading: false });
      if (prefetchQueue.length > 0) {
        prefetchTimer = setTimeout(runNextPrefetch, 180);
      } else {
        prefetchTimer = null;
      }
    };

    prefetchTimer = setTimeout(runNextPrefetch, 120);
  };

  const sendBackgroundRefresh = (includeOverview = false) => {
    const resources = desiredResource
      ? [desiredResource]
      : pendingResourceKeys().slice(0, 4);

    if (!includeOverview && resources.length === 0) {
      return false;
    }

    return sendOrQueue({
      refresh: {
        overview: includeOverview,
        resources: resources.map((item) => RESOURCE_ENUM[item]),
      },
    });
  };

  const startLiveRefresh = () => {
    if (liveRefreshTimer || !desiredPeerId) {
      return;
    }

    liveRefreshTick = 0;
    liveRefreshTimer = setInterval(() => {
      if (!desiredPeerId || !wsStore.isConnected()) {
        return;
      }
      liveRefreshTick += 1;
      const includeOverview = desiredResource
        ? liveRefreshTick % 5 === 0
        : liveRefreshTick % 3 === 0;
      sendBackgroundRefresh(includeOverview);
    }, 22000);
  };

  const openLiveStream = () => {
    if (!desiredPeerId || !wsStore.isConnected()) {
      return false;
    }

    clearReconnectTimer();
    clearPrefetchTimer();
    clearLiveRefreshTimer();
    warmedSessionId = null;

    const sessionId = buildSessionId(desiredPeerId);
    currentSessionId = sessionId;
    const keepStableVisualState = recentlyHealthy();

    update((s) => ({
      ...s,
      activePeerId: desiredPeerId,
      sessionId,
      streamStatus: keepStableVisualState ? "ready" : "connecting",
      isLoading: keepStableVisualState ? s.isLoading : true,
      error: null,
    }));

    const opened = wsStore.openRouterOSStream(
      sessionId,
      desiredPeerId,
      {
        onReady: () => {
          if (currentSessionId !== sessionId) return;
          lastActivityAt = Date.now();
          reconnectAttempts = 0;
          update((s) => ({
            ...s,
            streamStatus: "ready",
          }));
          startLiveRefresh();
          if (queuedCommand) {
            const payload = queuedCommand;
            queuedCommand = null;
            wsStore.sendRouterOSCommand(sessionId, payload);
          }
        },
        onState: (state: RouterOSStreamState) => {
          if (currentSessionId !== sessionId) return;
          lastActivityAt = Date.now();
          reconnectAttempts = 0;
          update((s) => {
            const nextOverview = {
              capability: state.capability || s.overview?.capability,
              identity: state.identity || s.overview?.identity,
              system_resource:
                state.system_resource || s.overview?.system_resource || {},
              routerboard: state.routerboard || s.overview?.routerboard || {},
            };
            const nextError =
              state.success === false ? state.error || s.error : null;
            const overviewChanged = !sameData(s.overview, nextOverview);

            if (
              !overviewChanged &&
              s.streamStatus === "ready" &&
              s.isLoading === false &&
              nextError === s.error
            ) {
              return s;
            }

            return {
              ...s,
              streamStatus: "ready",
              isLoading: false,
              error: nextError,
              overview: nextOverview,
            };
          });
          if (state.capability?.api_ready) {
            warmResourceCache();
          }
        },
        onResource: (resource: RouterOSResourceSnapshot) => {
          if (currentSessionId !== sessionId) return;
          lastActivityAt = Date.now();
          reconnectAttempts = 0;
          const key = normalizeResourceKey(resource.resource);
          if (!key) return;
          update((s) => ({
            ...s,
            records: {
              ...s.records,
              [key]:
                resource.success === false && !resource.records
                  ? s.records[key] || []
                  : resource.records || [],
            },
            counts: {
              ...s.counts,
              [key]:
                resource.success === false && !resource.records
                  ? s.counts[key] ?? null
                  : resource.records?.length ?? 0,
            },
            resourceErrors: {
              ...s.resourceErrors,
              [key]: resource.success === false ? resource.error || `Failed to load ${key}` : null,
            },
            sectionLoading: {
              ...s.sectionLoading,
              [key]: false,
            },
            error: resource.success === false ? resource.error || s.error : s.error,
          }));
        },
        onNotice: (notice: RouterOSNotice) => {
          if (currentSessionId !== sessionId) return;
          lastActivityAt = Date.now();
          reconnectAttempts = 0;
          update((s) => ({
            ...s,
            isSavingAccess: notice.action === "configure_access" ? false : s.isSavingAccess,
            notice:
              notice.success === false
                ? null
                : notice.action
                  ? `${notice.action.replace(/_/g, " ")} completed`
                  : s.notice,
            error: notice.success === false ? notice.error || s.error : s.error,
          }));
        },
        onError: (error: string) => {
          if (currentSessionId !== sessionId) return;
          const softFailure = recentlyHealthy();
          update((s) => ({
            ...s,
            streamStatus: softFailure ? s.streamStatus : "error",
            isLoading: softFailure ? s.isLoading : false,
            isSavingAccess: false,
            error: softFailure ? s.error : error,
          }));
        },
        onClose: () => {
          if (currentSessionId !== sessionId) return;
          currentSessionId = null;
          clearLiveRefreshTimer();
          reconnectAttempts += 1;
          const preserveReady = desiredPeerId && wsStore.isConnected() && recentlyHealthy();
          update((s) => ({
            ...s,
            sessionId: null,
            streamStatus: desiredPeerId ? (preserveReady ? "ready" : "connecting") : "idle",
            isLoading: preserveReady ? s.isLoading : false,
            isSavingAccess: false,
          }));
          if (desiredPeerId && wsStore.isConnected()) {
            clearReconnectTimer();
            reconnectTimer = setTimeout(() => {
              if (!currentSessionId && desiredPeerId && wsStore.isConnected()) {
                openLiveStream();
              }
            }, Math.min(1800, 400 + reconnectAttempts * 250));
          }
        },
      },
      {
        resource: desiredResource ? RESOURCE_ENUM[desiredResource] : 0,
      },
    );

    if (!opened) {
      update((s) => ({
        ...s,
        streamStatus: "error",
        isLoading: false,
        error: "RouterOS dashboard transport is not connected.",
      }));
    }

    return opened;
  };

  wsStore.subscribe(($ws) => {
    if ($ws.status !== "connected") {
      return;
    }
    if ($ws.connectionGeneration === lastConnectionGeneration) {
      return;
    }
    lastConnectionGeneration = $ws.connectionGeneration;

    if (!desiredPeerId) {
      return;
    }

    openLiveStream();
  });

  const sendOrQueue = (payload: Record<string, unknown>) => {
    if (!currentSessionId) {
      queuedCommand = payload;
      return openLiveStream();
    }
    return wsStore.sendRouterOSCommand(currentSessionId, payload);
  };

  return {
    subscribe,

    open(peerId: string, resource?: RouterOSResourceKey | null) {
      desiredPeerId = peerId;
      desiredResource = resource || null;
      if (currentSessionId) {
        wsStore.closeRouterOSStream(currentSessionId);
        currentSessionId = null;
      }
      resetState(peerId);
      openLiveStream();
    },

    close() {
      desiredPeerId = "";
      desiredResource = null;
      queuedCommand = null;
      if (currentSessionId) {
        wsStore.closeRouterOSStream(currentSessionId);
        currentSessionId = null;
      }
      clearReconnectTimer();
      clearPrefetchTimer();
      clearLiveRefreshTimer();
      warmedSessionId = null;
      set(initialState);
    },

    refresh(resources: RouterOSResourceKey[] = []) {
      update((s) => ({
        ...s,
        isLoading: true,
        error: null,
      }));
      return sendOrQueue({
        refresh: {
          overview: true,
          resources: resources.map((item) => RESOURCE_ENUM[item]),
        },
      });
    },

    loadResource(resource: RouterOSResourceKey, forceReload = false) {
      return sendLoadRequest(resource, forceReload, { makeCurrent: true, markLoading: true });
    },

    configureAccess(payload: {
      username?: string;
      password?: string;
      port?: number;
      use_tls?: boolean;
      use_saved_winbox?: boolean;
    }) {
      update((s) => ({
        ...s,
        isSavingAccess: true,
        error: null,
        notice: null,
      }));
      return sendOrQueue({
        configure_access: {
          username: payload.username || "",
          password: payload.password || "",
          port: payload.port || 0,
          use_tls: !!payload.use_tls,
          use_saved_winbox: !!payload.use_saved_winbox,
        },
      });
    },

    addResource(resource: RouterOSResourceKey, fields: Record<string, string>) {
      return sendOrQueue({
        add_resource: {
          resource: RESOURCE_ENUM[resource],
          fields,
        },
      });
    },

    updateResource(resource: RouterOSResourceKey, id: string, fields: Record<string, string>) {
      return sendOrQueue({
        update_resource: {
          resource: RESOURCE_ENUM[resource],
          id,
          fields,
        },
      });
    },

    deleteResource(resource: RouterOSResourceKey, id: string) {
      return sendOrQueue({
        delete_resource: {
          resource: RESOURCE_ENUM[resource],
          id,
        },
      });
    },

    clearError() {
      update((s) => ({ ...s, error: null }));
    },

    clearNotice() {
      update((s) => ({ ...s, notice: null }));
    },

    reset() {
      desiredPeerId = "";
      desiredResource = null;
      queuedCommand = null;
      if (currentSessionId) {
        wsStore.closeRouterOSStream(currentSessionId);
        currentSessionId = null;
      }
      clearReconnectTimer();
      clearPrefetchTimer();
      clearLiveRefreshTimer();
      warmedSessionId = null;
      set(initialState);
    },

    reconnect() {
      if (!desiredPeerId) {
        return false;
      }
      if (currentSessionId) {
        wsStore.closeRouterOSStream(currentSessionId);
        currentSessionId = null;
      }
      queuedCommand = null;
      clearReconnectTimer();
      clearPrefetchTimer();
      clearLiveRefreshTimer();
      warmedSessionId = null;
      return openLiveStream();
    },
  };
}

export const routerOSStore = createRouterOSStore();

export const routerOSResourceEnum = RESOURCE_ENUM;

export const routerOSResourceKeyByEnum = RESOURCE_KEY_BY_ENUM;
