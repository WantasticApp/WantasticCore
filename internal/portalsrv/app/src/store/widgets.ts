import { get, writable } from "svelte/store";
import { wsStore } from "./websocket";
import { tenant_id as tenantIdStore } from "./auth";
import type { DashboardStats } from "./dashboard";
import {
  type Peer,
  type PeerStats,
  type SSHActivity,
  type WinboxActivity,
} from "./peer";
import { t } from "./i18n";
import { DAY_MS, toUTCMillis } from "$lib/dateUtils";

export type WidgetId = "networkUptime" | "recentActivity" | "networkStats";
export type WidgetSize = "small" | "medium" | "large";
export type UptimeStatus = "operational" | "attention" | "degraded" | "unknown";

export interface WidgetDefinition {
  id: WidgetId;
  titleKey: string;
  descriptionKey: string;
  sizes: WidgetSize[];
  accent: string;
}

export interface WidgetLayout {
  id: WidgetId;
  enabled: boolean;
  order: number;
  size: WidgetSize;
}

export interface WidgetUptimeBar {
  key: string;
  label: string;
  score: number | null;
  status: UptimeStatus;
}

export interface WidgetUptimeData {
  windowDays: number;
  overallPercent: number;
  currentStatus: UptimeStatus;
  bars: WidgetUptimeBar[];
  trackedPeers: number;
  peersWithHistory: number;
  onlineNow: number;
}

export interface WidgetActivityItem {
  id: string;
  type: "login" | "ssh" | "winbox";
  title: string;
  subtitle: string;
  timestampMs: number;
  state: "active" | "ended";
  peerName?: string;
}

export interface WidgetActivityData {
  items: WidgetActivityItem[];
  totalSSH: number;
  totalWinbox: number;
  totalLogins: number;
  activeItems: number;
}

export interface WidgetStatsData {
  peerCount: number;
  onlinePeers: number;
  maxPeers: number;
  peerUsagePercent: number;
  rxBytes: number;
  txBytes: number;
  totalTrafficBytes: number;
  totalIPs: number;
  usedIPs: number;
  freeIPs: number;
  ipUsagePercent: number;
  networkBlocks: string[];
  cpuUsagePercent: number;
  memoryBytes: number;
  goroutineCount: number;
  trafficBars: Array<{ label: string; value: number; tone: "rx" | "tx" | "cpu" | "memory" }>;
}

export interface AsyncWidgetData<T> {
  loading: boolean;
  error: string | null;
  lastUpdated: number | null;
  data: T | null;
}

export interface WidgetState {
  widgets: WidgetLayout[];
  editMode: boolean;
  uptime: AsyncWidgetData<WidgetUptimeData>;
  activities: AsyncWidgetData<WidgetActivityData>;
}

interface TenantSessionInfo {
  session_id: string;
  ip_address: string;
  browser: string;
  browser_version: string;
  os: string;
  device_type: string;
  created_at?: { seconds: number; nanos?: number };
  last_activity?: { seconds: number; nanos?: number };
  expires_at?: { seconds: number; nanos?: number };
  is_current: boolean;
}

interface PeerHistorySnapshot {
  peer: Peer;
  uptimeHistory: PeerStats["uptime_history"] | null | undefined;
}

type LooseProtoTimestamp = { seconds?: number; nanos?: number };
type PeerWidgetSource = Peer[] | Promise<Peer[]> | (() => Promise<Peer[]>);

const STORAGE_KEY = "wantastic.widgetLayout.v1";
export const WIDGET_LIVE_REFRESH_MS = 15 * 1000;
// Uptime widget runs a live preview at 5s — the small slice that
// actually changes that fast is `onlineNow` + the current day's bar.
// The shared `refreshUptime` call is forced (bypasses UPTIME_TTL_MS)
// from this loop only.
export const WIDGET_UPTIME_LIVE_REFRESH_MS = 5 * 1000;
// Uptime history requires one stats RPC per peer, so the *gate* stays
// at 5min for non-forced callers; only the dedicated live loop above
// bypasses it.
const UPTIME_TTL_MS = 5 * 60 * 1000;
const ACTIVITY_TTL_MS = WIDGET_LIVE_REFRESH_MS;
const UPTIME_WINDOW_DAYS = 30;
const UPTIME_FETCH_CONCURRENCY = 6;
const HISTORY_SAMPLE_INTERVAL_MS = 5 * 60 * 1000;
const WIDGET_CONTEXT_WAIT_TIMEOUT_MS = 12 * 1000;
const EXPECTED_HANDSHAKES_PER_DAY = Math.max(
  1,
  Math.floor(DAY_MS / HISTORY_SAMPLE_INTERVAL_MS)
);

export const WIDGET_DEFINITIONS: WidgetDefinition[] = [
  {
    id: "networkUptime",
    titleKey: "widgets.networkUptime",
    descriptionKey: "widgets.networkUptimeDescription",
    sizes: ["small", "medium", "large"],
    accent: "rgba(128, 214, 255, 0.85)",
  },
  {
    id: "recentActivity",
    titleKey: "widgets.recentActivity",
    descriptionKey: "widgets.recentActivityDescription",
    sizes: ["small", "medium", "large"],
    accent: "rgba(255, 211, 132, 0.85)",
  },
  {
    id: "networkStats",
    titleKey: "stats.statistics",
    descriptionKey: "stats.realTimeMetrics",
    sizes: ["medium", "large"],
    accent: "rgba(178, 126, 255, 0.88)",
  },
];

const WIDGET_BY_ID = new Map(
  WIDGET_DEFINITIONS.map((widget) => [widget.id, widget])
);

function defaultLayouts(): WidgetLayout[] {
  return [
    { id: "networkUptime", enabled: true, order: 0, size: "small" },
    { id: "networkStats", enabled: true, order: 1, size: "medium" },
    { id: "recentActivity", enabled: true, order: 2, size: "small" },
  ];
}

const LEGACY_DEFAULT_LAYOUTS: WidgetLayout[] = [
  { id: "networkUptime", enabled: true, order: 0, size: "small" },
  { id: "recentActivity", enabled: true, order: 1, size: "small" },
  { id: "networkStats", enabled: true, order: 2, size: "large" },
];

function defaultAsyncState<T>(): AsyncWidgetData<T> {
  return {
    loading: false,
    error: null,
    lastUpdated: null,
    data: null,
  };
}

function sanitizeSize(widgetId: WidgetId, size: WidgetSize): WidgetSize {
  const allowed = WIDGET_BY_ID.get(widgetId)?.sizes || ["medium"];
  return allowed.includes(size) ? size : allowed[0];
}

function normalizeLayouts(
  layouts: Partial<WidgetLayout>[] | null | undefined
): WidgetLayout[] {
  const fallback = defaultLayouts();
  const byId = new Map<WidgetId, WidgetLayout>();

  (layouts || []).forEach((layout, index) => {
    const id = layout.id as WidgetId | undefined;
    if (!id || !WIDGET_BY_ID.has(id)) {
      return;
    }
    byId.set(id, {
      id,
      enabled: layout.enabled ?? true,
      order: Number.isFinite(layout.order) ? Number(layout.order) : index,
      size: sanitizeSize(
        id,
        (layout.size as WidgetSize) || fallback[index]?.size || "medium"
      ),
    });
  });

  fallback.forEach((layout, index) => {
    if (!byId.has(layout.id)) {
      byId.set(layout.id, { ...layout, order: index });
    }
  });

  const normalized = [...byId.values()]
    .sort((a, b) => a.order - b.order)
    .map((layout, index) => ({ ...layout, order: index }));

  if (matchesLayoutSignature(normalized, LEGACY_DEFAULT_LAYOUTS)) {
    return defaultLayouts();
  }

  return normalized;
}

function matchesLayoutSignature(
  left: WidgetLayout[],
  right: WidgetLayout[]
): boolean {
  if (left.length !== right.length) {
    return false;
  }

  return left.every((layout, index) => {
    const candidate = right[index];
    return (
      candidate &&
      candidate.id === layout.id &&
      candidate.enabled === layout.enabled &&
      candidate.size === layout.size
    );
  });
}

function loadLayouts(): WidgetLayout[] {
  if (typeof window === "undefined") {
    return defaultLayouts();
  }

  try {
    const raw = window.localStorage.getItem(STORAGE_KEY);
    if (!raw) {
      return defaultLayouts();
    }
    return normalizeLayouts(JSON.parse(raw));
  } catch (error) {
    console.warn("Failed to load widget layout settings", error);
    return defaultLayouts();
  }
}

function persistLayouts(layouts: WidgetLayout[]) {
  if (typeof window === "undefined") {
    return;
  }
  window.localStorage.setItem(STORAGE_KEY, JSON.stringify(layouts));
}

function sortLayouts(layouts: WidgetLayout[]): WidgetLayout[] {
  return [...layouts]
    .sort((a, b) => a.order - b.order)
    .map((layout, index) => ({ ...layout, order: index }));
}

function shouldRefreshWidget<T>(
  state: AsyncWidgetData<T>,
  ttlMs: number,
  force: boolean
): boolean {
  if (force) {
    return true;
  }
  if (state.loading) {
    return false;
  }
  if (!state.lastUpdated) {
    return true;
  }
  return Date.now() - state.lastUpdated >= ttlMs;
}

function clampPercent(value: number): number {
  return Math.max(0, Math.min(100, Math.round(value)));
}

function toNumeric(value: unknown): number {
  if (typeof value === "number") {
    return Number.isFinite(value) ? value : 0;
  }
  if (typeof value === "bigint") {
    return Number(value);
  }
  if (typeof value === "string") {
    const trimmed = value.trim();
    if (!trimmed) {
      return 0;
    }
    const parsed = Number(trimmed);
    return Number.isFinite(parsed) ? parsed : 0;
  }
  if (value == null) {
    return 0;
  }
  const parsed = Number(value);
  return Number.isFinite(parsed) ? parsed : 0;
}

export function buildStatsWidgetData(stats: DashboardStats): WidgetStatsData {
  const peerCount = toNumeric(stats.peer_count);
  const onlinePeers = toNumeric(stats.online_peers);
  const maxPeers = toNumeric(stats.max_peers);
  const rxBytes = toNumeric(stats.rx_bytes);
  const txBytes = toNumeric(stats.tx_bytes);
  const totalTrafficBytes = rxBytes + txBytes;
  const totalIPs = toNumeric(stats.total_ips_available);
  const usedIPs = toNumeric(stats.ips_used);
  const freeIPs = Math.max(0, totalIPs - usedIPs);
  const peerUsagePercent = maxPeers > 0 ? clampPercent((peerCount / maxPeers) * 100) : 0;
  const ipUsagePercent = totalIPs > 0 ? clampPercent((usedIPs / totalIPs) * 100) : 0;
  const cpuUsagePercent = clampPercent(toNumeric(stats.cpu_usage_percent));
  const memoryBytes = toNumeric(stats.memory_bytes);
  const memoryMegabytes = memoryBytes > 0 ? memoryBytes / (1024 * 1024) : 0;

  return {
    peerCount,
    onlinePeers,
    maxPeers,
    peerUsagePercent,
    rxBytes,
    txBytes,
    totalTrafficBytes,
    totalIPs,
    usedIPs,
    freeIPs,
    ipUsagePercent,
    networkBlocks: stats.network_blocks || [],
    cpuUsagePercent,
    memoryBytes,
    goroutineCount: toNumeric(stats.goroutine_count),
    trafficBars: [
      { label: "RX", value: rxBytes, tone: "rx" },
      { label: "TX", value: txBytes, tone: "tx" },
      { label: "CPU", value: cpuUsagePercent, tone: "cpu" },
      { label: "MEM", value: memoryMegabytes, tone: "memory" },
    ],
  };
}

function sequentializeLayouts(layouts: WidgetLayout[]): WidgetLayout[] {
  return layouts.map((layout, index) => ({ ...layout, order: index }));
}

function moveWithinLayouts(
  layouts: WidgetLayout[],
  widgetId: WidgetId,
  direction: -1 | 1
): WidgetLayout[] | null {
  const index = layouts.findIndex((widget) => widget.id === widgetId);
  if (index === -1) {
    return null;
  }

  const targetIndex = index + direction;
  if (targetIndex < 0 || targetIndex >= layouts.length) {
    return null;
  }

  const reordered = [...layouts];
  const [moved] = reordered.splice(index, 1);
  reordered.splice(targetIndex, 0, moved);
  return reordered;
}

function moveToTargetLayout(
  layouts: WidgetLayout[],
  widgetId: WidgetId,
  targetId: WidgetId
): WidgetLayout[] | null {
  const sourceIndex = layouts.findIndex((widget) => widget.id === widgetId);
  const targetIndex = layouts.findIndex((widget) => widget.id === targetId);
  if (sourceIndex === -1 || targetIndex === -1 || sourceIndex === targetIndex) {
    return null;
  }

  const reordered = [...layouts];
  const [moved] = reordered.splice(sourceIndex, 1);
  reordered.splice(targetIndex, 0, moved);
  return reordered;
}

function mergeVisibleAndHiddenLayouts(
  visibleLayouts: WidgetLayout[],
  hiddenLayouts: WidgetLayout[]
): WidgetLayout[] {
  return sequentializeLayouts([...visibleLayouts, ...hiddenLayouts]);
}

function decodeHistoryBytes(
  bytes: Uint8Array | string | null | undefined
): number[] {
  if (!bytes || bytes.length === 0) {
    return [];
  }

  let buffer: Uint8Array;
  if (typeof bytes === "string") {
    try {
      const binary = window.atob(bytes);
      buffer = new Uint8Array(binary.length);
      for (let i = 0; i < binary.length; i += 1) {
        buffer[i] = binary.charCodeAt(i);
      }
    } catch (error) {
      console.warn("Failed to decode uptime history", error);
      return [];
    }
  } else {
    buffer = bytes;
  }

  const count = Math.floor(buffer.length / 4);
  const view = new DataView(
    buffer.buffer,
    buffer.byteOffset,
    buffer.byteLength
  );
  const handshakes: number[] = [];
  for (let i = 0; i < count; i += 1) {
    handshakes.push(view.getUint32(i * 4, false) * 1000);
  }
  return handshakes;
}

function startOfTodayUTC(): number {
  const date = new Date();
  date.setUTCHours(0, 0, 0, 0);
  return date.getTime();
}

function clampScore(score: number): number {
  if (!Number.isFinite(score)) {
    return 0;
  }
  return Math.min(Math.max(score, 0), 1);
}

function scoreToStatus(score: number | null): UptimeStatus {
  if (score == null) {
    return "unknown";
  }
  if (score >= 0.975) {
    return "operational";
  }
  if (score >= 0.85) {
    return "attention";
  }
  return "degraded";
}

function aggregateUptimeData(entries: PeerHistorySnapshot[]): WidgetUptimeData {
  const endBoundary = startOfTodayUTC() + DAY_MS;
  const startBoundary = endBoundary - UPTIME_WINDOW_DAYS * DAY_MS;
  const bars: WidgetUptimeBar[] = [];
  let overallCoverage = 0;
  let overallExpectedPeers = 0;

  const preparedEntries = entries.map((entry) => {
    const handshakes = decodeHistoryBytes(entry.uptimeHistory);
    const onboardedAt =
      (handshakes.length > 0 ? Math.min(...handshakes) : null) ??
      toUTCMillis(entry.peer.created_at);

    return {
      ...entry,
      handshakes,
      onboardedAt,
    };
  });

  for (let dayIndex = 0; dayIndex < UPTIME_WINDOW_DAYS; dayIndex += 1) {
    const slotStart = startBoundary + dayIndex * DAY_MS;
    const slotEnd = slotStart + DAY_MS;

    let slotCoverage = 0;
    let slotPeers = 0;

    preparedEntries.forEach(({ handshakes, onboardedAt }) => {
      if (onboardedAt == null || slotEnd <= onboardedAt) {
        return;
      }

      let slotCount = 0;
      for (const handshake of handshakes) {
        if (handshake >= slotStart && handshake < slotEnd) {
          slotCount += 1;
        }
      }

      slotPeers += 1;
      slotCoverage += clampScore(slotCount / EXPECTED_HANDSHAKES_PER_DAY);
    });

    const normalizedScore = slotPeers > 0 ? slotCoverage / slotPeers : null;
    const dayLabel = new Date(slotStart).toLocaleDateString(undefined, {
      month: "short",
      day: "numeric",
    });

    if (normalizedScore != null) {
      overallCoverage += normalizedScore * slotPeers;
      overallExpectedPeers += slotPeers;
    }

    bars.push({
      key: String(slotStart),
      label: dayLabel,
      score:
        normalizedScore == null
          ? null
          : Math.round(normalizedScore * 1000) / 1000,
      status: scoreToStatus(normalizedScore),
    });
  }

  const peersWithHistory = preparedEntries.filter(
    (entry) => entry.handshakes.length > 0
  ).length;
  const trackedPeers = preparedEntries.length;

  // "Online now" needs three sources of truth, not just the is_online
  // flag. On first login the WS subscription hasn't replayed yet and the
  // DB snapshot can carry stale is_online=false; relying on it alone made
  // the widget show "0 online" while devices were demonstrably up. A peer
  // counts as online if ANY of these is true:
  //   1. the live WS-driven is_online flag,
  //   2. the most recent decoded handshake is within ONLINE_THRESHOLD_MS,
  //   3. peer.last_handshake (set by the server stats path) is within the
  //      same window.
  // The threshold matches WireGuard's 2-minute handshake interval plus a
  // small grace for clock skew and one missed handshake.
  //
  // Important: we do NOT use toTimestampMillis() here. That helper has a
  // `|| Date.now()` fallback for every missing/zero/parse-failure path,
  // which silently treats a "Never connected" peer's zero timestamp as
  // right-now and counts it as online. parseLastHandshakeMs returns 0
  // for absent/zero values so the threshold check correctly fails.
  const ONLINE_THRESHOLD_MS = 3 * 60 * 1000;
  const now = Date.now();
  const parseLastHandshakeMs = (
    v: LooseProtoTimestamp | string | number | undefined | null
  ): number => {
    if (v == null) return 0;
    if (typeof v === "object" && "seconds" in v) {
      const s = (v as LooseProtoTimestamp).seconds;
      if (typeof s !== "number" || s <= 0) return 0;
      return s * 1000 + ((v as LooseProtoTimestamp).nanos || 0) / 1e6;
    }
    if (typeof v === "number") {
      if (v <= 0) return 0;
      // Heuristic: protobuf int64 seconds vs JS millis.
      return v > 1e12 ? v : v * 1000;
    }
    if (typeof v === "string") {
      if (!v || v === "0001-01-01T00:00:00Z") return 0;
      const t = Date.parse(v);
      return isNaN(t) ? 0 : t;
    }
    return 0;
  };

  const onlineNow = preparedEntries.filter((entry) => {
    if (entry.peer.is_online) return true;
    if (entry.handshakes.length > 0) {
      const latest = entry.handshakes[entry.handshakes.length - 1];
      if (now - latest < ONLINE_THRESHOLD_MS) return true;
    }
    const lh = parseLastHandshakeMs(
      entry.peer.last_handshake as LooseProtoTimestamp | string | undefined
    );
    if (lh > 0 && now - lh < ONLINE_THRESHOLD_MS) return true;
    return false;
  }).length;
  const overallPercent =
    overallExpectedPeers > 0
      ? Math.round((overallCoverage / overallExpectedPeers) * 10000) / 100
      : 0;

  return {
    windowDays: UPTIME_WINDOW_DAYS,
    overallPercent,
    currentStatus: bars[bars.length - 1]?.status || "unknown",
    bars,
    trackedPeers,
    peersWithHistory,
    onlineNow,
  };
}

function toTimestampMillis(
  timestamp: LooseProtoTimestamp | string | number | undefined | null
): number {
  if (timestamp && typeof timestamp === "object" && "seconds" in timestamp) {
    if (typeof timestamp.seconds === "number") {
      return (
        toUTCMillis({
          seconds: timestamp.seconds,
          nanos: timestamp.nanos || 0,
        }) || Date.now()
      );
    }
    return Date.now();
  }
  if (typeof timestamp === "string" || typeof timestamp === "number") {
    return toUTCMillis(timestamp) || Date.now();
  }
  return Date.now();
}

function activityStateFromEndTime(
  endTime: LooseProtoTimestamp | string | undefined | null
): "active" | "ended" {
  if (endTime && typeof endTime === "object" && "seconds" in endTime) {
    return typeof endTime.seconds === "number" && endTime.seconds > 0
      ? "ended"
      : "active";
  }
  return typeof endTime === "string" && toUTCMillis(endTime)
    ? "ended"
    : "active";
}

function buildSSHActivities(
  peer: Peer,
  activities: SSHActivity[]
): WidgetActivityItem[] {
  return activities.map((activity, index) => {
    const timestampMs = toTimestampMillis(
      activity.end_time || activity.timestamp
    );
    const labelTarget = peer.name || peer.assigned_ip || peer.id;
    const title = activity.username
      ? `${activity.username}@${labelTarget}`
      : `SSH · ${labelTarget}`;

    return {
      id: `ssh:${peer.id}:${activity.session_id || index}:${timestampMs}`,
      type: "ssh",
      title,
      subtitle: activity.client_ip || labelTarget,
      timestampMs,
      state: activityStateFromEndTime(activity.end_time),
      peerName: peer.name || peer.assigned_ip,
    };
  });
}

function buildWinboxActivities(
  peer: Peer,
  activities: WinboxActivity[]
): WidgetActivityItem[] {
  return activities.map((activity, index) => {
    const timestampMs = toTimestampMillis(
      activity.end_time || activity.timestamp
    );
    const labelTarget = peer.name || peer.assigned_ip || peer.id;
    const title = activity.session_name
      ? `${activity.session_name} · ${labelTarget}`
      : `Winbox · ${labelTarget}`;

    return {
      id: `winbox:${peer.id}:${activity.session_name || index}:${timestampMs}`,
      type: "winbox",
      title,
      subtitle: activity.username || activity.client_ip || labelTarget,
      timestampMs,
      state: activityStateFromEndTime(activity.end_time),
      peerName: peer.name || peer.assigned_ip,
    };
  });
}

function buildLoginActivities(
  sessions: TenantSessionInfo[]
): WidgetActivityItem[] {
  return sessions.map((session) => {
    const timestampMs = toTimestampMillis(session.created_at);
    const browserName = session.browser || t("widgets.portalApp");
    const deviceLabel = session.os
      ? `${browserName} · ${session.os}`
      : browserName;

    return {
      id: `login:${session.session_id}`,
      type: "login",
      title: deviceLabel,
      subtitle:
        session.ip_address || session.device_type || t("widgets.portalAccess"),
      timestampMs,
      state: session.is_current ? "active" : "ended",
    };
  });
}

async function mapWithConcurrency<T, R>(
  items: T[],
  limit: number,
  mapper: (item: T, index: number) => Promise<R>
): Promise<R[]> {
  if (items.length === 0) {
    return [];
  }

  const results: R[] = new Array(items.length);
  let cursor = 0;

  async function worker() {
    while (cursor < items.length) {
      const current = cursor;
      cursor += 1;
      results[current] = await mapper(items[current], current);
    }
  }

  const workerCount = Math.max(1, Math.min(limit, items.length));
  await Promise.all(Array.from({ length: workerCount }, () => worker()));
  return results;
}

function createWidgetStore() {
  const { subscribe, update } = writable<WidgetState>({
    widgets: loadLayouts(),
    editMode: false,
    uptime: defaultAsyncState<WidgetUptimeData>(),
    activities: defaultAsyncState<WidgetActivityData>(),
  });

  let widgetContextWaitPromise: Promise<boolean> | null = null;

  function isWidgetContextReady(): boolean {
    const tenantId = get(tenantIdStore);
    const wsState = get(wsStore);
    return Boolean(tenantId) && wsState.status === "connected" && wsState.encryptionReady;
  }

  function finishPendingWidgetLoad<K extends keyof Pick<WidgetState, "uptime" | "activities">>(
    key: K
  ) {
    update((state) => ({
      ...state,
      [key]: {
        ...state[key],
        loading: false,
      },
    }));
  }

  function waitForWidgetContextReady(
    timeoutMs = WIDGET_CONTEXT_WAIT_TIMEOUT_MS
  ): Promise<boolean> {
    if (isWidgetContextReady()) {
      return Promise.resolve(true);
    }

    if (widgetContextWaitPromise) {
      return widgetContextWaitPromise;
    }

    widgetContextWaitPromise = new Promise<boolean>((resolve) => {
      let settled = false;
      let unsubscribeTenant = () => {};
      let unsubscribeWs = () => {};
      let timeoutId: ReturnType<typeof setTimeout> | null = null;

      const cleanup = () => {
        unsubscribeTenant();
        unsubscribeWs();
        if (timeoutId) {
          clearTimeout(timeoutId);
        }
        widgetContextWaitPromise = null;
      };

      const settle = (ready: boolean) => {
        if (settled) {
          return;
        }
        settled = true;
        cleanup();
        resolve(ready);
      };

      const maybeResolve = () => {
        if (isWidgetContextReady()) {
          settle(true);
        }
      };

      unsubscribeTenant = tenantIdStore.subscribe(() => {
        maybeResolve();
      });
      unsubscribeWs = wsStore.subscribe(() => {
        maybeResolve();
      });
      timeoutId = setTimeout(() => settle(false), timeoutMs);
      maybeResolve();
    });

    return widgetContextWaitPromise;
  }

  function normalizeWidgetPeers(rawPeers: Peer[]): Peer[] {
    const wsState = get(wsStore);
    return rawPeers.map((peer) => {
      const normalized: Peer = {
        ...peer,
        ip_address: peer.assigned_ip,
        transfer_rx: peer.rx_bytes,
        transfer_tx: peer.tx_bytes,
      };

      const wsStatus = wsState.peerStatuses.get(normalized.id);
      if (wsStatus?.isOnline) {
        return {
          ...normalized,
          is_online: true,
          last_seen_at: wsStatus.lastSeen || normalized.last_seen_at,
          last_handshake: normalized.last_handshake || wsStatus.lastSeen,
        };
      }

      return normalized;
    });
  }

  async function listPeerWidgets(_force = false): Promise<Peer[]> {
    const tenantId = get(tenantIdStore);
    if (!tenantId) {
      return [];
    }

    const response = await wsStore.callGRPC<{ peers?: Peer[] }>(
      "TenantPeerService",
      "ListTenantPeers",
      {
        tenant_id: tenantId,
      }
    );
    const all = normalizeWidgetPeers(response.peers || []);
    // Fleet-health widgets reflect *your* network. Peers shared in from
    // another tenant are managed by that tenant — counting them here
    // inflates trackedPeers and skews uptime/online math.
    return all.filter((p) => !p.is_shared);
  }

  async function resolvePeerWidgets(
    force: boolean,
    peersSource?: PeerWidgetSource
  ): Promise<Peer[]> {
    if (Array.isArray(peersSource)) {
      return peersSource;
    }
    if (typeof peersSource === "function") {
      return peersSource();
    }
    if (peersSource) {
      return peersSource;
    }
    return listPeerWidgets(force);
  }

  async function refreshUptime(
    force = false,
    peersSource?: PeerWidgetSource
  ) {
    let shouldLoad = true;
    update((state) => {
      // In-flight guard applies regardless of `force`. `force` only
      // bypasses the TTL — it should never let two refreshes pile up
      // (the live 5s loop would otherwise stack on a slow network).
      if (state.uptime.loading) {
        shouldLoad = false;
        return state;
      }
      if (
        !force &&
        state.uptime.lastUpdated &&
        Date.now() - state.uptime.lastUpdated < UPTIME_TTL_MS
      ) {
        shouldLoad = false;
        return state;
      }
      return {
        ...state,
        uptime: {
          ...state.uptime,
          loading: true,
          error: null,
        },
      };
    });

    if (!shouldLoad) {
      return;
    }

    try {
      const isReady = await waitForWidgetContextReady();
      if (!isReady) {
        finishPendingWidgetLoad("uptime");
        return;
      }

      const peers = await resolvePeerWidgets(force, peersSource);
      const histories = await mapWithConcurrency(
        peers,
        UPTIME_FETCH_CONCURRENCY,
        async (peer) => {
          if (peer.uptime_history && peer.uptime_history.length > 0) {
            return {
              peer,
              uptimeHistory: peer.uptime_history,
            } satisfies PeerHistorySnapshot;
          }
          try {
            const response = await wsStore.callGRPC<{ stats?: PeerStats }>(
              "TenantPeerService",
              "GetTenantPeerStats",
              {
                tenant_id: "",
                peer_id: peer.id,
              }
            );
            return {
              peer,
              uptimeHistory: response.stats?.uptime_history,
            } satisfies PeerHistorySnapshot;
          } catch (error) {
            console.warn(
              "Failed to load uptime history for peer",
              peer.id,
              error
            );
            return {
              peer,
              uptimeHistory: null,
            } satisfies PeerHistorySnapshot;
          }
        }
      );

      const data = aggregateUptimeData(histories);
      update((state) => ({
        ...state,
        uptime: {
          loading: false,
          error: null,
          lastUpdated: Date.now(),
          data,
        },
      }));
    } catch (error: any) {
      update((state) => ({
        ...state,
        uptime: {
          ...state.uptime,
          loading: false,
          error: error?.message || t("widgets.loadUptimeError"),
        },
      }));
    }
  }

  async function refreshActivities(
    force = false,
    peersSource?: PeerWidgetSource
  ) {
    let shouldLoad = true;
    update((state) => {
      if (!force && state.activities.loading) {
        shouldLoad = false;
        return state;
      }
      if (
        !force &&
        state.activities.lastUpdated &&
        Date.now() - state.activities.lastUpdated < ACTIVITY_TTL_MS
      ) {
        shouldLoad = false;
        return state;
      }
      return {
        ...state,
        activities: {
          ...state.activities,
          loading: true,
          error: null,
        },
      };
    });

    if (!shouldLoad) {
      return;
    }

    try {
      const isReady = await waitForWidgetContextReady();
      if (!isReady) {
        finishPendingWidgetLoad("activities");
        return;
      }

      const [peers, sessionsResponse] = await Promise.all([
        resolvePeerWidgets(force, peersSource),
        wsStore.callGRPC<{ sessions?: TenantSessionInfo[] }>(
          "TenantPortalService",
          "ListTenantSessions",
          {
            tenant_id: "",
          }
        ),
      ]);

      const activityItems: WidgetActivityItem[] = [];
      let totalSSH = 0;
      let totalWinbox = 0;

      peers.forEach((peer) => {
        const sshActivities = peer.ssh_activities || [];
        const winboxActivities = peer.winbox_activities || [];
        totalSSH += sshActivities.length;
        totalWinbox += winboxActivities.length;
        activityItems.push(...buildSSHActivities(peer, sshActivities));
        activityItems.push(...buildWinboxActivities(peer, winboxActivities));
      });

      const loginActivities = buildLoginActivities(
        sessionsResponse.sessions || []
      );
      activityItems.push(...loginActivities);

      const items = activityItems
        .sort((left, right) => right.timestampMs - left.timestampMs)
        .slice(0, 18);

      update((state) => ({
        ...state,
        activities: {
          loading: false,
          error: null,
          lastUpdated: Date.now(),
          data: {
            items,
            totalSSH,
            totalWinbox,
            totalLogins: loginActivities.length,
            activeItems: items.filter((item) => item.state === "active").length,
          },
        },
      }));
    } catch (error: any) {
      update((state) => ({
        ...state,
        activities: {
          ...state.activities,
          loading: false,
          error: error?.message || t("widgets.loadActivityError"),
        },
      }));
    }
  }

  // Network Statistics now lives in its own dedicated live store
  // (see store/statsLive.ts) so its polling cadence and state are
  // isolated from uptime/activity refresh paths.

  async function refreshAll(force = false) {
    const state = get({ subscribe });
    const enabledWidgetIds = new Set(
      state.widgets.filter((widget) => widget.enabled).map((widget) => widget.id)
    );
    const shouldLoadUptime =
      enabledWidgetIds.has("networkUptime") &&
      shouldRefreshWidget(state.uptime, UPTIME_TTL_MS, force);
    const shouldLoadActivities =
      enabledWidgetIds.has("recentActivity") &&
      shouldRefreshWidget(state.activities, ACTIVITY_TTL_MS, force);

    if (!shouldLoadUptime && !shouldLoadActivities) {
      return;
    }

    let sharedPeersPromise: Promise<Peer[]> | null = null;
    const sharedPeersSource =
      shouldLoadUptime || shouldLoadActivities
        ? () => {
            if (!sharedPeersPromise) {
              sharedPeersPromise = listPeerWidgets(force);
            }
            return sharedPeersPromise;
          }
        : undefined;

    await Promise.all([
      shouldLoadUptime
        ? refreshUptime(force, sharedPeersSource)
        : Promise.resolve(),
      shouldLoadActivities
        ? refreshActivities(force, sharedPeersSource)
        : Promise.resolve(),
    ]);
  }

  function setEditMode(editMode: boolean) {
    update((state) => ({ ...state, editMode }));
  }

  function toggleEnabled(widgetId: WidgetId) {
    update((state) => {
      const widgets = sortLayouts(state.widgets);
      const target = widgets.find((widget) => widget.id === widgetId);
      if (!target) {
        return state;
      }

      const enabled = widgets.filter(
        (widget) => widget.id !== widgetId && widget.enabled
      );
      const hidden = widgets.filter(
        (widget) => widget.id !== widgetId && !widget.enabled
      );
      const toggled = { ...target, enabled: !target.enabled };
      const normalized = target.enabled
        ? sequentializeLayouts([...enabled, ...hidden, toggled])
        : sequentializeLayouts([...enabled, toggled, ...hidden]);
      persistLayouts(normalized);
      return { ...state, widgets: normalized };
    });
  }

  function setWidgetSize(widgetId: WidgetId, size: WidgetSize) {
    update((state) => {
      const widgets = sequentializeLayouts(
        state.widgets.map((widget) =>
          widget.id === widgetId
            ? { ...widget, size: sanitizeSize(widgetId, size) }
            : widget
        )
      );
      persistLayouts(widgets);
      return { ...state, widgets };
    });
  }

  function moveWidget(widgetId: WidgetId, direction: -1 | 1) {
    update((state) => {
      const widgets = sortLayouts(state.widgets);
      const index = widgets.findIndex((widget) => widget.id === widgetId);
      if (index === -1) {
        return state;
      }
      const targetIndex = index + direction;
      if (targetIndex < 0 || targetIndex >= widgets.length) {
        return state;
      }
      const reordered = [...widgets];
      const [moved] = reordered.splice(index, 1);
      reordered.splice(targetIndex, 0, moved);
      const normalized = sequentializeLayouts(reordered);
      persistLayouts(normalized);
      return { ...state, widgets: normalized };
    });
  }

  function reorderWidget(widgetId: WidgetId, targetId: WidgetId) {
    if (widgetId === targetId) {
      return;
    }
    update((state) => {
      const widgets = sortLayouts(state.widgets);
      const reordered = moveToTargetLayout(widgets, widgetId, targetId);
      if (!reordered) {
        return state;
      }
      const normalized = sequentializeLayouts(reordered);
      persistLayouts(normalized);
      return { ...state, widgets: normalized };
    });
  }

  function moveEnabledWidget(widgetId: WidgetId, direction: -1 | 1) {
    update((state) => {
      const widgets = sortLayouts(state.widgets);
      const enabled = widgets.filter((widget) => widget.enabled);
      const hidden = widgets.filter((widget) => !widget.enabled);
      const reorderedEnabled = moveWithinLayouts(enabled, widgetId, direction);
      if (!reorderedEnabled) {
        return state;
      }
      const normalized = mergeVisibleAndHiddenLayouts(reorderedEnabled, hidden);
      persistLayouts(normalized);
      return { ...state, widgets: normalized };
    });
  }

  function moveHiddenWidget(widgetId: WidgetId, direction: -1 | 1) {
    update((state) => {
      const widgets = sortLayouts(state.widgets);
      const enabled = widgets.filter((widget) => widget.enabled);
      const hidden = widgets.filter((widget) => !widget.enabled);
      const reorderedHidden = moveWithinLayouts(hidden, widgetId, direction);
      if (!reorderedHidden) {
        return state;
      }
      const normalized = mergeVisibleAndHiddenLayouts(enabled, reorderedHidden);
      persistLayouts(normalized);
      return { ...state, widgets: normalized };
    });
  }

  function reorderEnabledWidget(widgetId: WidgetId, targetId: WidgetId) {
    if (widgetId === targetId) {
      return;
    }

    update((state) => {
      const widgets = sortLayouts(state.widgets);
      const enabled = widgets.filter((widget) => widget.enabled);
      const hidden = widgets.filter((widget) => !widget.enabled);
      const reorderedEnabled = moveToTargetLayout(enabled, widgetId, targetId);
      if (!reorderedEnabled) {
        return state;
      }
      const normalized = mergeVisibleAndHiddenLayouts(reorderedEnabled, hidden);
      persistLayouts(normalized);
      return { ...state, widgets: normalized };
    });
  }

  function resetLayout() {
    const widgets = defaultLayouts();
    persistLayouts(widgets);
    update((state) => ({
      ...state,
      widgets,
      editMode: false,
    }));
  }

  return {
    subscribe,
    refreshAll,
    refreshActivities,
    refreshUptime,
    setEditMode,
    toggleEnabled,
    setWidgetSize,
    moveWidget,
    reorderWidget,
    moveEnabledWidget,
    moveHiddenWidget,
    reorderEnabledWidget,
    resetLayout,
  };
}

export const widgetStore = createWidgetStore();
