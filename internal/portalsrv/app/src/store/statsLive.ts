import { get, writable } from "svelte/store";
import { wsStore } from "./websocket";
import { tenant_id as tenantIdStore } from "./auth";
import type { DashboardStats } from "./dashboard";
import {
  buildStatsWidgetData,
  type AsyncWidgetData,
  type WidgetStatsData,
} from "./widgets";
import { t } from "./i18n";

// Polling cadence for the Statistics widget. Live enough to feel
// real-time without hammering the dashboard RPC. The TTL gates
// off-cadence callers (e.g. retries) so a slow network does not
// stack overlapping fetches.
const STATS_REFRESH_MS = 7_000;
const STATS_TTL_MS = 6_000;
const CONTEXT_WAIT_MS = 12_000;

function defaultState(): AsyncWidgetData<WidgetStatsData> {
  return { loading: false, error: null, lastUpdated: null, data: null };
}

function isContextReady(): boolean {
  const tenantId = get(tenantIdStore);
  const ws = get(wsStore);
  return Boolean(tenantId) && ws.status === "connected" && ws.encryptionReady;
}

function waitForContext(timeoutMs = CONTEXT_WAIT_MS): Promise<boolean> {
  if (isContextReady()) {
    return Promise.resolve(true);
  }

  return new Promise<boolean>((resolve) => {
    let settled = false;
    let unsubTenant = () => {};
    let unsubWs = () => {};
    let timer: ReturnType<typeof setTimeout> | null = null;

    const cleanup = () => {
      unsubTenant();
      unsubWs();
      if (timer) {
        clearTimeout(timer);
      }
    };

    const settle = (ready: boolean) => {
      if (settled) return;
      settled = true;
      cleanup();
      resolve(ready);
    };

    const probe = () => {
      if (isContextReady()) {
        settle(true);
      }
    };

    unsubTenant = tenantIdStore.subscribe(probe);
    unsubWs = wsStore.subscribe(probe);
    timer = setTimeout(() => settle(false), timeoutMs);
    probe();
  });
}

function createStatsLiveStore() {
  const { subscribe, update } = writable<AsyncWidgetData<WidgetStatsData>>(
    defaultState()
  );

  let pollTimer: ReturnType<typeof setInterval> | null = null;
  let activeMounts = 0;
  let inFlight: Promise<void> | null = null;

  // Anti-oscillation state. The backend GetTenantDashboard sums
  // peer.RxBytes/TxBytes from ListPeers, which overlays live WG device
  // stats on top of DB-stored values. If a peer just reconnected
  // (counters reset to 0) or the request lands somewhere with only
  // partial peer visibility, the response can briefly drop to a
  // dramatically smaller aggregate while a steady-state response would
  // be much larger. We tolerate up to 2 consecutive "suspect" (much-
  // smaller-than-previous) reads before accepting the new value as a
  // real traffic reset. Steady-state changes are unaffected.
  const SUSPECT_TX_RX_RATIO = 0.4; // accept if new >= 40% of previous
  const SUSPECT_MIN_PREV_BYTES = 1024; // ignore noise below 1 KiB
  const SUSPECT_MAX_HOLDS = 2;
  let suspectHolds = 0;

  function refresh(force = false): Promise<void> {
    if (inFlight) {
      return inFlight;
    }

    const state = get({ subscribe });
    if (
      !force &&
      state.lastUpdated &&
      Date.now() - state.lastUpdated < STATS_TTL_MS
    ) {
      return Promise.resolve();
    }

    inFlight = (async () => {
      update((s) => ({ ...s, loading: true, error: null }));
      try {
        const ready = await waitForContext();
        if (!ready) {
          update((s) => ({ ...s, loading: false }));
          return;
        }

        const response = await wsStore.callGRPC<DashboardStats>(
          "TenantPortalService",
          "GetTenantDashboard",
          { tenant_id: "" }
        );

        update((prev) => {
          const next = buildStatsWidgetData(response);
          if (prev.data) {
            // Step 1: zero-protection. WG byte counters / memory are
            // monotonic between resets — a sudden 0 with non-zero
            // previous is a transient/partial read.
            if (next.rxBytes === 0 && prev.data.rxBytes > 0) {
              next.rxBytes = prev.data.rxBytes;
            }
            if (next.txBytes === 0 && prev.data.txBytes > 0) {
              next.txBytes = prev.data.txBytes;
            }
            if (next.memoryBytes === 0 && prev.data.memoryBytes > 0) {
              next.memoryBytes = prev.data.memoryBytes;
            }
            if (next.peerCount === 0 && prev.data.peerCount > 0) {
              next.peerCount = prev.data.peerCount;
            }

            // Step 2: anti-oscillation. If the new response is much
            // smaller than the previous (e.g. response landed on a hub
            // with partial peer visibility, or a peer just reconnected
            // and its WG counters reset to 0), hold the previous value
            // for up to SUSPECT_MAX_HOLDS polls. After that many
            // consecutive low reads we accept the new value as a real
            // traffic reset. This stops the widget from flickering
            // between "6.97 MB" and "1.21 MB" on alternating refreshes.
            const isSuspectRx =
              prev.data.rxBytes > SUSPECT_MIN_PREV_BYTES &&
              next.rxBytes < prev.data.rxBytes * SUSPECT_TX_RX_RATIO;
            const isSuspectTx =
              prev.data.txBytes > SUSPECT_MIN_PREV_BYTES &&
              next.txBytes < prev.data.txBytes * SUSPECT_TX_RX_RATIO;

            if ((isSuspectRx || isSuspectTx) && suspectHolds < SUSPECT_MAX_HOLDS) {
              suspectHolds += 1;
              if (isSuspectRx) next.rxBytes = prev.data.rxBytes;
              if (isSuspectTx) next.txBytes = prev.data.txBytes;
            } else {
              suspectHolds = 0;
            }

            next.totalTrafficBytes = next.rxBytes + next.txBytes;
          }
          return {
            loading: false,
            error: null,
            lastUpdated: Date.now(),
            data: next,
          };
        });
      } catch (err: any) {
        update((s) => ({
          ...s,
          loading: false,
          error: err?.message || t("stats.failedToGetStats"),
        }));
      } finally {
        inFlight = null;
      }
    })();

    return inFlight;
  }

  // start/stop are reference-counted so the poll loop only runs while
  // at least one Statistics widget is mounted. If multiple instances
  // ever exist (size changes during edit, etc.) they share one poller.
  function start() {
    activeMounts += 1;
    if (activeMounts === 1) {
      refresh(true).catch(() => {});
      pollTimer = setInterval(() => {
        refresh(false).catch(() => {});
      }, STATS_REFRESH_MS);
    }
  }

  function stop() {
    activeMounts = Math.max(0, activeMounts - 1);
    if (activeMounts === 0 && pollTimer) {
      clearInterval(pollTimer);
      pollTimer = null;
    }
  }

  return { subscribe, refresh, start, stop };
}

export const statsLiveStore = createStatsLiveStore();
