<script lang="ts">
  import { createEventDispatcher, onDestroy, onMount } from "svelte";
  import WidgetShell from "./WidgetShell.svelte";
  import { _ } from "$store/i18n";
  import { statsLiveStore } from "$store/statsLive";
  import type { WidgetLayout, WidgetSize } from "$store/widgets";

  export let widget: WidgetLayout;
  export let editMode = false;
  export let accent = "rgba(178, 126, 255, 0.88)";
  export let sizes: WidgetSize[] = ["medium", "large"];
  export let canMoveUp = false;
  export let canMoveDown = false;

  const dispatch = createEventDispatcher<{
    sizechange: WidgetSize;
    moveup: void;
    movedown: void;
  }>();

  type MetricParts = { value: string; unit: string };

  const countFormatter = new Intl.NumberFormat("en-US");

  $: payload = $statsLiveStore;
  $: data = payload.data;

  $: totalTraffic = formatBytesParts(data?.totalTrafficBytes || 0);
  $: rxTraffic = formatBytesParts(data?.rxBytes || 0);
  $: txTraffic = formatBytesParts(data?.txBytes || 0);
  $: memoryFootprint = formatBytesParts(data?.memoryBytes || 0);

  $: trafficTotal = Math.max(1, (data?.rxBytes || 0) + (data?.txBytes || 0));
  $: downloadShare = data ? Math.round((data.rxBytes / trafficTotal) * 100) : 0;
  $: uploadShare = data ? Math.round((data.txBytes / trafficTotal) * 100) : 0;
  $: trafficState =
    !data || data.totalTrafficBytes <= 0
      ? "stats.trafficIdle"
      : downloadShare >= 70
        ? "stats.trafficDownloadHeavy"
        : uploadShare >= 45
          ? "stats.trafficUploadActive"
          : "stats.trafficBalanced";

  function formatBytesParts(bytes: number): MetricParts {
    if (!bytes || bytes <= 0) {
      return { value: "0", unit: "B" };
    }
    const units = ["B", "KB", "MB", "GB", "TB"];
    let size = bytes;
    let unitIndex = 0;
    while (size >= 1024 && unitIndex < units.length - 1) {
      size /= 1024;
      unitIndex += 1;
    }
    return {
      value:
        size >= 100 || unitIndex === 0
          ? size.toFixed(0)
          : size >= 10
            ? size.toFixed(1)
            : size.toFixed(2),
      unit: units[unitIndex],
    };
  }

  function formatCount(value: number): string {
    return countFormatter.format(Math.max(0, Math.round(value || 0)));
  }

  function percentLabel(value: number): string {
    return `${Math.max(0, Math.min(100, Math.round(value || 0)))}%`;
  }

  onMount(() => {
    statsLiveStore.start();
  });

  onDestroy(() => {
    statsLiveStore.stop();
  });
</script>

<WidgetShell
  title={$_("stats.statistics")}
  subtitle={$_("stats.realTimeMetrics")}
  size={widget.size}
  {editMode}
  {accent}
  {sizes}
  loading={payload.loading}
  error={payload.error}
  lastUpdated={payload.lastUpdated}
  {canMoveUp}
  {canMoveDown}
  on:refresh={() => statsLiveStore.refresh(true)}
  on:sizechange={(event) => dispatch("sizechange", event.detail)}
  on:moveup={() => dispatch("moveup")}
  on:movedown={() => dispatch("movedown")}
>
  {#if data}
    <div class="network-stats-body">
      <section class="hero" aria-label={$_("stats.combinedTraffic")}>
        <div class="hero-head">
          <div class="hero-head-left">
            <span class="kicker">{$_("stats.combinedTraffic")}</span>
            <div class="pills-row">
              <span class="state-pill">{$_(trafficState)}</span>
            </div>
          </div>
          <div class="hero-metric">
            <strong>{totalTraffic.value}</strong>
            <small>{totalTraffic.unit}</small>
          </div>
        </div>

        <div class="dual-meter" aria-hidden="true">
          <span
            class="bar-fill rx"
            style={`flex:${data.totalTrafficBytes > 0 ? Math.max(downloadShare, 1) : 0}`}
          ></span>
          <span
            class="bar-fill tx"
            style={`flex:${data.totalTrafficBytes > 0 ? Math.max(uploadShare, 1) : 0}`}
          ></span>
        </div>

        <div class="split-row">
          <div class="split rx">
            <span class="dot"></span>
            <span class="lbl">{$_("stats.totalDownloaded")}</span>
            <strong>{rxTraffic.value} {rxTraffic.unit}</strong>
            <em>{percentLabel(downloadShare)}</em>
          </div>
          <div class="split tx">
            <span class="dot"></span>
            <span class="lbl">{$_("stats.totalUploaded")}</span>
            <strong>{txTraffic.value} {txTraffic.unit}</strong>
            <em>{percentLabel(uploadShare)}</em>
          </div>
        </div>
      </section>

      <section class="meters">
        <article class="meter-card">
          <div class="meter-head">
            <span class="kicker">{$_("stats.peerUsage")}</span>
            <span class="value">
              <strong>{formatCount(data.peerCount)}</strong>
              <em>/ {formatCount(data.maxPeers)}</em>
            </span>
          </div>
          <div class="meter">
            <span
              class="meter-fill fleet"
              style={`width:${data.peerUsagePercent}%`}
            ></span>
          </div>
          <div class="meter-foot">
            <span class="kicker">{$_("stats.memory")}</span>
            <span class="value-soft">
              <strong
                >{memoryFootprint.value}<small>{memoryFootprint.unit}</small
                ></strong
              >
            </span>
          </div>
        </article>
      </section>
    </div>
  {/if}
</WidgetShell>

<style>
  /* Single-column stacked layout. Predictable, hard to break, and fits
     inside the WidgetSlot height clamp regardless of widget width.
     Sub-grids inside (.split-row, .meters) collapse from 2 columns
     to 1 column when the container is too narrow. */
  /* Defensive layout. .stats keeps its intrinsic content height
     (no `min-height: 0`, no `height: 100%`) so even if the shell's
     flex column collapses on a malformed mobile viewport, the
     content still pushes the box to its natural size below the
     header — preventing visual overlap. */
  .network-stats-body {
    display: flex;
    flex-direction: column;
    gap: 10px;
    width: 100%;
    min-width: 0;
    overflow: hidden;
    position: relative;
    z-index: 0;
    container: stats-widget / inline-size;
    --radius-sm: 4px;
    --radius-pill: 6px;
  }

  /* ── Pills row (state) sits inside the hero card so it cannot be
        visually confused with the WidgetShell header on narrow
        viewports. ───────────────────────────────────────────── */
  .pills-row {
    display: flex;
    flex-wrap: wrap;
    gap: 6px;
    margin-top: 6px;
  }

  .state-pill {
    display: inline-flex;
    align-items: center;
    height: 22px;
    padding: 0 9px;
    border: 1px solid var(--widget-panel-border);
    border-radius: var(--radius-pill);
    background: color-mix(in srgb, var(--widget-panel-bg) 88%, transparent);
    color: var(--widget-ink-soft);
    font-size: 10px;
    font-weight: 800;
    letter-spacing: 0.08em;
    text-transform: uppercase;
    white-space: nowrap;
  }

  /* ── Hero card: combined traffic + dual meter + split ── */
  .hero {
    display: flex;
    flex-direction: column;
    gap: 8px;
    padding: 12px;
    border: 1px solid var(--widget-panel-border);
    border-radius: var(--radius-sm);
    background: color-mix(in srgb, var(--widget-panel-bg) 95%, transparent);
    flex-shrink: 0;
    min-width: 0;
  }

  .hero-head {
    display: flex;
    align-items: flex-start;
    justify-content: space-between;
    gap: 12px;
    min-width: 0;
  }

  .hero-head-left {
    display: flex;
    flex-direction: column;
    gap: 2px;
    min-width: 0;
  }

  .kicker {
    color: var(--widget-ink-muted);
    font-size: 10px;
    font-weight: 700;
    letter-spacing: 0.14em;
    text-transform: uppercase;
    font-style: normal;
    line-height: 1;
  }

  .hero-metric {
    display: inline-flex;
    align-items: baseline;
    gap: 6px;
    flex-shrink: 0;
  }

  .hero-metric strong {
    color: var(--widget-ink-strong);
    font-size: clamp(24px, 3.4cqi, 32px);
    line-height: 0.94;
    font-weight: 780;
    letter-spacing: -0.045em;
  }

  .hero-metric small {
    color: var(--widget-ink-soft);
    font-size: 11px;
    font-weight: 800;
    letter-spacing: 0.12em;
    text-transform: uppercase;
  }

  /* Two-segment proportional bar showing rx/tx share */
  .dual-meter {
    display: flex;
    height: 6px;
    overflow: hidden;
    border: 1px solid var(--widget-panel-border);
    border-radius: var(--radius-sm);
    background: rgba(8, 14, 22, 0.84);
    flex-shrink: 0;
  }

  .bar-fill {
    display: block;
    height: 100%;
    min-width: 0;
    border-radius: var(--radius-sm);
  }

  .bar-fill.rx {
    background: linear-gradient(
      90deg,
      rgba(84, 191, 255, 0.96),
      rgba(59, 128, 255, 0.88)
    );
  }

  .bar-fill.tx {
    background: linear-gradient(
      90deg,
      rgba(188, 127, 255, 0.96),
      rgba(118, 93, 255, 0.88)
    );
  }

  /* Two columns of rx/tx legend (collapses to 1 below 360px) */
  .split-row {
    display: grid;
    grid-template-columns: repeat(2, minmax(0, 1fr));
    gap: 8px 12px;
    min-width: 0;
  }

  .split {
    display: flex;
    align-items: center;
    gap: 6px;
    min-width: 0;
    overflow: hidden;
  }

  .split .dot {
    flex: 0 0 auto;
    width: 8px;
    height: 8px;
    border-radius: 50%;
  }

  .split.rx .dot {
    background: rgba(84, 191, 255, 0.96);
  }

  .split.tx .dot {
    background: rgba(188, 127, 255, 0.96);
  }

  .split .lbl {
    flex: 0 1 auto;
    color: var(--widget-ink-muted);
    font-size: 9px;
    font-weight: 700;
    letter-spacing: 0.08em;
    text-transform: uppercase;
    white-space: nowrap;
    overflow: hidden;
    text-overflow: ellipsis;
  }

  .split strong {
    flex: 0 0 auto;
    margin-left: auto;
    color: var(--widget-ink-strong);
    font-size: 12px;
    line-height: 1;
    font-weight: 760;
    letter-spacing: -0.02em;
    white-space: nowrap;
  }

  .split em {
    flex: 0 0 auto;
    color: var(--widget-ink-soft);
    font-style: normal;
    font-size: 10px;
    font-weight: 800;
    white-space: nowrap;
  }

  /* ── Capacity meter (device usage) ────────────────────── */
  .meters {
    display: flex;
    flex-direction: column;
    gap: 8px;
    flex-shrink: 0;
    min-width: 0;
  }

  .meter-card {
    display: flex;
    flex-direction: column;
    gap: 8px;
    padding: 10px;
    border: 1px solid var(--widget-panel-border);
    border-radius: var(--radius-sm);
    background: color-mix(in srgb, var(--widget-panel-bg) 92%, transparent);
    min-width: 0;
  }

  .meter-head {
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: 6px;
    min-width: 0;
  }

  .meter-head .value {
    display: inline-flex;
    align-items: baseline;
    gap: 4px;
    white-space: nowrap;
  }

  .meter-head .value strong {
    color: var(--widget-ink-strong);
    font-size: 14px;
    font-weight: 760;
    line-height: 1;
  }

  .meter-head .value em {
    color: var(--widget-ink-soft);
    font-style: normal;
    font-size: 11px;
  }

  .meter {
    height: 6px;
    overflow: hidden;
    border: 1px solid var(--widget-panel-border);
    border-radius: var(--radius-sm);
    background: rgba(8, 14, 22, 0.84);
  }

  .meter-fill {
    display: block;
    height: 100%;
    min-width: 4px;
    border-radius: var(--radius-sm);
  }

  .meter-fill.fleet {
    background: linear-gradient(
      90deg,
      rgba(102, 189, 255, 0.96),
      rgba(54, 120, 255, 0.9)
    );
  }

  .meter-fill.pool {
    background: linear-gradient(
      90deg,
      rgba(95, 241, 184, 0.96),
      rgba(63, 152, 255, 0.9)
    );
  }

  /* ── Compact secondary stat under the meter (memory) ──── */
  .meter-foot {
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: 8px;
    min-width: 0;
  }

  .meter-foot .value-soft {
    display: inline-flex;
    align-items: baseline;
    gap: 2px;
    color: var(--widget-ink);
  }

  .meter-foot .value-soft strong {
    color: var(--widget-ink);
    font-size: 12px;
    font-weight: 700;
    line-height: 1;
  }

  .meter-foot .value-soft small {
    color: var(--widget-ink-soft);
    margin-left: 3px;
    font-size: 9px;
    font-weight: 800;
    letter-spacing: 0.08em;
    text-transform: uppercase;
  }

  /* ── Narrow container: collapse split row to single col ─ */
  @container stats-widget (max-width: 360px) {
    .split-row {
      grid-template-columns: 1fr;
    }
  }
</style>
