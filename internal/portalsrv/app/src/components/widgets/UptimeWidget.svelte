<script lang="ts">
  import WidgetShell from "./WidgetShell.svelte";
  import { createEventDispatcher } from "svelte";
  import { _ } from "$store/i18n";
  import type {
    AsyncWidgetData,
    WidgetLayout,
    WidgetSize,
    WidgetUptimeData,
  } from "$store/widgets";

  export let widget: WidgetLayout;
  export let editMode = false;
  export let payload: AsyncWidgetData<WidgetUptimeData>;
  export let accent = "rgba(128, 214, 255, 0.85)";
  export let sizes: WidgetSize[] = ["small", "medium", "large"];
  export let canMoveUp = true;
  export let canMoveDown = true;

  const dispatch = createEventDispatcher<{
    refresh: void;
    sizechange: WidgetSize;
    moveup: void;
    movedown: void;
  }>();

  const DEFAULT_UPTIME_WINDOW_DAYS = 30;

  $: visibleBars = (() => {
    const bars = payload.data?.bars || [];
    const limit =
      widget.size === "small" ? 36 : widget.size === "large" ? 90 : 60;
    return bars.slice(-limit);
  })();

  function statusLabel(status: WidgetUptimeData["currentStatus"] | "unknown"): string {
    if (status === "operational") return $_("widgets.operational");
    if (status === "attention") return $_("widgets.attention");
    if (status === "degraded") return $_("widgets.degraded");
    return $_("widgets.unknown");
  }

  function tooltipLabel(bar: WidgetUptimeData["bars"][number]): string {
    if (bar.score == null) {
      return `${bar.label} · ${$_("widgets.noSignal")}`;
    }
    return `${bar.label} · ${Math.round(bar.score * 100)}%`;
  }

  function barStyle(bar: WidgetUptimeData["bars"][number]): string {
    if (bar.score == null) {
      return "";
    }

    const score = Math.max(0, Math.min(1, bar.score));
    const mix = (from: number, to: number, progress: number) =>
      from + (to - from) * progress;

    let hue = 0;
    if (score < 0.6) {
      hue = mix(0, 24, score / 0.6);
    } else if (score < 0.8) {
      hue = mix(24, 48, (score - 0.6) / 0.2);
    } else if (score < 0.95) {
      hue = mix(48, 88, (score - 0.8) / 0.15);
    } else {
      hue = mix(88, 116, (score - 0.95) / 0.05);
    }

    const glowOpacity = 0.08 + score * 0.12;

    return [
      `background: linear-gradient(180deg, hsla(${Math.round(hue)}, 92%, 74%, 0.98), hsla(${Math.round(hue)}, 78%, 58%, 0.94))`,
      `box-shadow: inset 0 1px 0 rgba(255, 255, 255, 0.24), 0 0 0 1px rgba(255, 255, 255, 0.04), 0 6px 14px hsla(${Math.round(hue)}, 80%, 54%, ${glowOpacity.toFixed(3)})`,
    ].join("; ");
  }
</script>

<WidgetShell
  title={$_("widgets.networkUptime")}
  subtitle={$_("widgets.networkUptimeCaption")}
  accent={accent}
  size={widget.size}
  {sizes}
  {editMode}
  loading={payload.loading}
  error={payload.error || ""}
  lastUpdated={payload.lastUpdated}
  {canMoveUp}
  {canMoveDown}
  on:refresh={() => dispatch("refresh")}
  on:sizechange={(event) => dispatch("sizechange", event.detail)}
  on:moveup={() => dispatch("moveup")}
  on:movedown={() => dispatch("movedown")}
>
  <div class="uptime-body">
    <div class="uptime-summary">
      <div class="summary-main">
        <span class="status-pill" class:operational={payload.data?.currentStatus === "operational"} class:attention={payload.data?.currentStatus === "attention"} class:degraded={payload.data?.currentStatus === "degraded"}>
          {statusLabel(payload.data?.currentStatus || "unknown")}
        </span>
        <strong>{payload.data ? `${payload.data.overallPercent.toFixed(2)}%` : "--"}</strong>
      </div>
      <div class="summary-meta">
        <span>{$_("widgets.trackedDevices", { values: { count: payload.data?.trackedPeers || 0 } })}</span>
        <span>{$_("widgets.onlineNow", { values: { count: payload.data?.onlineNow || 0 } })}</span>
      </div>
    </div>

    <div class="uptime-bars">
      {#if payload.loading && visibleBars.length === 0}
        {#each Array(DEFAULT_UPTIME_WINDOW_DAYS) as _, index (index)}
          <span class="uptime-bar skeleton" />
        {/each}
      {:else if visibleBars.length === 0}
        <div class="empty-state">{$_("widgets.noUptimeData")}</div>
      {:else}
        {#each visibleBars as bar (bar.key)}
          <span
            class="uptime-bar"
            class:unknown={bar.status === "unknown"}
            style={barStyle(bar)}
            title={tooltipLabel(bar)}
          />
        {/each}
      {/if}
    </div>

    <div class="axis">
      <span>{$_("widgets.daysBack", { values: { count: payload.data?.windowDays || DEFAULT_UPTIME_WINDOW_DAYS } })}</span>
      <span>{$_("widgets.today")}</span>
    </div>
  </div>
</WidgetShell>

<style>
  .uptime-body {
    display: flex;
    flex: 1 1 auto;
    flex-direction: column;
    min-height: 0;
    gap: 14px;
  }

  .uptime-summary {
    display: flex;
    align-items: flex-start;
    justify-content: space-between;
    gap: 12px;
    flex-wrap: wrap;
  }

  .summary-main {
    display: flex;
    flex-direction: column;
    gap: 8px;
  }

  .summary-main strong {
    font-size: clamp(22px, 2vw, 34px);
    line-height: 1;
    font-weight: 700;
    letter-spacing: -0.04em;
    color: var(--widget-ink-strong);
  }

  .status-pill {
    display: inline-flex;
    align-items: center;
    justify-content: center;
    width: fit-content;
    padding: 6px 11px;
    border-radius: 999px;
    font-size: clamp(10px, 0.55vw, 11px);
    font-weight: 700;
    letter-spacing: 0.06em;
    text-transform: uppercase;
    background: var(--widget-panel-bg-strong);
    color: var(--widget-ink);
    border: 1px solid var(--widget-panel-border);
    backdrop-filter: blur(10px);
    -webkit-backdrop-filter: blur(10px);
  }

  .status-pill.operational {
    background: rgba(92, 214, 139, 0.16);
    color: rgba(223, 255, 232, 0.96);
    border-color: rgba(120, 231, 163, 0.22);
  }

  .status-pill.attention {
    background: rgba(255, 194, 94, 0.18);
    color: rgba(255, 242, 211, 0.97);
    border-color: rgba(255, 210, 129, 0.22);
  }

  .status-pill.degraded {
    background: rgba(255, 118, 143, 0.18);
    color: rgba(255, 224, 232, 0.97);
    border-color: rgba(255, 153, 177, 0.22);
  }

  .summary-meta {
    display: grid;
    gap: 8px;
    justify-items: end;
    font-size: clamp(11px, 0.65vw, 12px);
    font-weight: 600;
    color: var(--widget-ink-soft);
    text-align: right;
  }

  .uptime-bars {
    display: grid;
    grid-template-columns: repeat(auto-fit, minmax(4px, 1fr));
    gap: 4px;
    align-items: end;
    min-height: 82px;
  }

  .uptime-bar {
    min-height: 52px;
    border-radius: 999px;
    background: var(--widget-panel-bg-strong);
    box-shadow:
      inset 0 1px 0 rgba(255, 255, 255, 0.04),
      0 0 0 1px rgba(255, 255, 255, 0.03);
  }

  .uptime-bar.unknown {
    background: var(--widget-panel-bg);
  }

  .uptime-bar.skeleton {
    animation: pulse 1.2s ease-in-out infinite;
  }

  .axis {
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: 12px;
    font-size: clamp(10px, 0.55vw, 11px);
    font-weight: 600;
    letter-spacing: 0.06em;
    text-transform: uppercase;
    color: var(--widget-ink-muted);
  }

  .empty-state {
    display: flex;
    align-items: center;
    justify-content: center;
    min-height: 86px;
    padding: 14px;
    border-radius: 16px;
    background: var(--widget-panel-bg);
    border: 1px solid var(--widget-panel-border);
    font-size: 12px;
    color: var(--widget-ink-soft);
    text-align: center;
    backdrop-filter: blur(12px);
    -webkit-backdrop-filter: blur(12px);
  }

  :global(.widget-shell.small) .summary-main strong {
    font-size: 24px;
  }

  :global(.widget-shell.small) .summary-meta {
    width: 100%;
    justify-items: start;
    text-align: left;
    gap: 4px;
  }

  :global(.widget-shell.small) .uptime-bars {
    min-height: 64px;
  }

  :global(.widget-shell.small) .uptime-bar {
    min-height: 42px;
  }

  @keyframes pulse {
    0%, 100% {
      opacity: 0.4;
    }

    50% {
      opacity: 1;
    }
  }

  @media (max-width: 768px) {
    .summary-main strong {
      font-size: 26px;
    }

    .uptime-bars {
      gap: 3px;
      min-height: 76px;
    }

    .uptime-bar {
      min-height: 48px;
    }
  }
</style>
