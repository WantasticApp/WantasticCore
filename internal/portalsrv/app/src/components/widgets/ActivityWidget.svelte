<script lang="ts">
  import WidgetShell from "./WidgetShell.svelte";
  import { createEventDispatcher } from "svelte";
  import { _ } from "$store/i18n";
  import type {
    AsyncWidgetData,
    WidgetActivityData,
    WidgetLayout,
    WidgetSize,
  } from "$store/widgets";

  export let widget: WidgetLayout;
  export let editMode = false;
  export let payload: AsyncWidgetData<WidgetActivityData>;
  export let accent = "rgba(255, 211, 132, 0.85)";
  export let sizes: WidgetSize[] = ["small", "medium", "large"];
  export let canMoveUp = true;
  export let canMoveDown = true;

  const dispatch = createEventDispatcher<{
    refresh: void;
    sizechange: WidgetSize;
    moveup: void;
    movedown: void;
  }>();

  // Always cap at the 20 most-recent items. The list itself is
  // vertically scrollable, so showing fewer just to fit a fixed widget
  // height was hiding events the user actually wanted to see.
  const ACTIVITY_MAX_ITEMS = 20;
  $: visibleItems = (payload.data?.items || []).slice(0, ACTIVITY_MAX_ITEMS);

  function kindLabel(kind: "login" | "ssh" | "winbox"): string {
    if (kind === "login") return $_("widgets.login");
    if (kind === "winbox") return $_("widgets.winbox");
    return $_("widgets.ssh");
  }

  function relativeTime(timestampMs: number): string {
    const diffSeconds = Math.max(0, Math.floor((Date.now() - timestampMs) / 1000));
    if (diffSeconds < 60) return $_("time.justNow");

    const minutes = Math.floor(diffSeconds / 60);
    if (minutes < 60) {
      return $_("time.minutesAgo", { values: { count: minutes } });
    }

    const hours = Math.floor(minutes / 60);
    if (hours < 24) {
      return $_("time.hoursAgo", { values: { count: hours } });
    }

    const days = Math.floor(hours / 24);
    return $_("time.daysAgo", { values: { count: days } });
  }
</script>

<WidgetShell
  title={$_("widgets.recentActivity")}
  subtitle={$_("widgets.recentActivityCaption")}
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
  <div class="activity-body">
    <div class="activity-stats">
      <div class="mini-stat">
        <span>{$_("widgets.ssh")}</span>
        <strong>{payload.data?.totalSSH || 0}</strong>
      </div>
      <div class="mini-stat">
        <span>{$_("widgets.winbox")}</span>
        <strong>{payload.data?.totalWinbox || 0}</strong>
      </div>
      <div class="mini-stat">
        <span>{$_("widgets.logins")}</span>
        <strong>{payload.data?.totalLogins || 0}</strong>
      </div>
    </div>

    <div class="activity-list">
      {#if payload.loading && visibleItems.length === 0}
        {#each Array(widget.size === "small" ? 4 : 6) as _, index (index)}
          <div class="activity-row skeleton">
            <span class="activity-type" />
            <div class="activity-copy">
              <span class="line short" />
              <span class="line long" />
            </div>
          </div>
        {/each}
      {:else if visibleItems.length === 0}
        <div class="empty-state">{$_("widgets.noRecentActivity")}</div>
      {:else}
        {#each visibleItems as item (item.id)}
          <div class="activity-row">
            <span class="activity-type" class:login={item.type === "login"} class:ssh={item.type === "ssh"} class:winbox={item.type === "winbox"}>
              {kindLabel(item.type)}
            </span>

            <div class="activity-copy">
              <div class="row-head">
                <strong>{item.title}</strong>
                <span class="state-badge" class:active={item.state === "active"}>
                  {item.state === "active" ? $_("widgets.activeNowBadge") : $_("widgets.completed")}
                </span>
              </div>
              <div class="row-foot">
                <span>{item.subtitle}</span>
                <time>{relativeTime(item.timestampMs)}</time>
              </div>
            </div>
          </div>
        {/each}
      {/if}
    </div>
  </div>
</WidgetShell>

<style>
  .activity-body {
    display: flex;
    flex: 1 1 auto;
    flex-direction: column;
    min-height: 0;
    overflow: hidden;
    gap: 14px;
  }

  .activity-stats {
    display: grid;
    grid-template-columns: repeat(3, minmax(0, 1fr));
    gap: 10px;
  }

  .mini-stat {
    padding: 10px 11px;
    border-radius: 15px;
    background: var(--widget-panel-bg);
    border: 1px solid var(--widget-panel-border);
    box-shadow:
      inset 0 1px 0 rgba(255, 255, 255, 0.04),
      0 10px 18px rgba(0, 0, 0, 0.12);
    backdrop-filter: blur(12px) saturate(135%);
    -webkit-backdrop-filter: blur(12px) saturate(135%);
  }

  .mini-stat span {
    display: block;
    font-size: clamp(10px, 0.55vw, 11px);
    font-weight: 600;
    letter-spacing: 0.06em;
    text-transform: uppercase;
    color: var(--widget-ink-muted);
  }

  .mini-stat strong {
    display: block;
    margin-top: 6px;
    font-size: clamp(18px, 1.5vw, 20px);
    line-height: 1;
    color: var(--widget-ink-strong);
  }

  .activity-list {
    display: grid;
    gap: 9px;
    min-height: 0;
    flex: 1 1 auto;
    overflow-y: auto;
    overflow-x: hidden;
    /* Native momentum on touch + contained overscroll so swiping past
       the last row doesn't bubble out to the rail's horizontal pager. */
    -webkit-overflow-scrolling: touch;
    overscroll-behavior: contain;
    scrollbar-width: thin;
    scrollbar-color: var(--widget-panel-border) transparent;
    /* Inside padding on both sides so rows have a visual breathing
       edge and their drop shadows don't visually run into the
       widget-shell border on narrow viewports. */
    padding: 0 4px;
    /* Defensive: the rows have rounded corners + box-shadow that can
       extend past the parent on glassmorphic backdrops. Containing
       horizontal overflow keeps everything inside the shell. */
    contain: layout paint;
  }
  .activity-list::-webkit-scrollbar {
    width: 6px;
  }
  .activity-list::-webkit-scrollbar-track {
    background: transparent;
  }
  .activity-list::-webkit-scrollbar-thumb {
    background: var(--widget-panel-border);
    border-radius: 999px;
  }

  .activity-row {
    display: grid;
    grid-template-columns: auto 1fr;
    gap: 10px;
    align-items: flex-start;
    padding: 10px 11px;
    border-radius: 12px;
    /* More opaque than the default --widget-panel-bg (rgba 0.15) so
       the WidgetShell's ambient glow gradient and adjacent type-color
       tints don't bleed through and make rows look like they extend
       outside the shell. */
    background: rgba(11, 18, 32, 0.55);
    border: 1px solid var(--widget-panel-border);
    box-shadow: inset 0 1px 0 rgba(255, 255, 255, 0.04);
  }

  .activity-type {
    display: inline-flex;
    align-items: center;
    justify-content: center;
    min-width: 58px;
    padding: 6px 10px;
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

  .activity-type.login {
    background: rgba(108, 186, 255, 0.18);
    color: rgba(221, 241, 255, 0.96);
    border-color: rgba(142, 207, 255, 0.22);
  }

  .activity-type.ssh {
    background: rgba(92, 214, 139, 0.16);
    color: rgba(223, 255, 232, 0.96);
    border-color: rgba(120, 231, 163, 0.22);
  }

  .activity-type.winbox {
    background: rgba(255, 194, 94, 0.18);
    color: rgba(255, 242, 211, 0.97);
    border-color: rgba(255, 210, 129, 0.22);
  }

  .activity-copy {
    min-width: 0;
  }

  .row-head,
  .row-foot {
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: 10px;
  }

  .row-head strong {
    display: block;
    min-width: 0;
    overflow: hidden;
    font-size: clamp(13px, 1vw, 14px);
    font-weight: 650;
    line-height: 1.35;
    text-overflow: ellipsis;
    white-space: nowrap;
    color: var(--widget-ink-strong);
  }

  .row-foot {
    margin-top: 4px;
    font-size: clamp(10px, 0.6vw, 11px);
    font-weight: 600;
    color: var(--widget-ink-soft);
  }

  .row-foot span,
  .row-foot time {
    min-width: 0;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }

  .state-badge {
    display: inline-flex;
    align-items: center;
    justify-content: center;
    padding: 5px 8px;
    border-radius: 999px;
    font-size: clamp(9px, 0.55vw, 10px);
    font-weight: 700;
    letter-spacing: 0.06em;
    text-transform: uppercase;
    color: var(--widget-ink-soft);
    background: var(--widget-panel-bg-strong);
    border: 1px solid var(--widget-panel-border);
    backdrop-filter: blur(10px);
    -webkit-backdrop-filter: blur(10px);
  }

  .state-badge.active {
    background: rgba(92, 214, 139, 0.16);
    color: rgba(223, 255, 232, 0.96);
    border-color: rgba(120, 231, 163, 0.22);
  }

  .empty-state {
    display: flex;
    align-items: center;
    justify-content: center;
    min-height: 120px;
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

  .activity-row.skeleton {
    grid-template-columns: 58px 1fr;
    animation: pulse 1.2s ease-in-out infinite;
  }

  .activity-row.skeleton .activity-type,
  .activity-row.skeleton .line {
    background: var(--widget-panel-bg-strong);
    color: transparent;
  }

  .activity-row.skeleton .activity-type {
    min-height: 24px;
  }

  .line {
    display: block;
    height: 10px;
    border-radius: 999px;
  }

  .line.short {
    width: 58%;
  }

  .line.long {
    width: 82%;
    margin-top: 8px;
  }

  :global(.widget-shell.small) .activity-stats {
    gap: 8px;
  }

  :global(.widget-shell.small) .mini-stat {
    padding: 8px 9px;
  }

  :global(.widget-shell.small) .mini-stat strong {
    font-size: 15px;
  }

  :global(.widget-shell.small) .activity-row {
    padding: 9px 10px;
    gap: 8px;
  }

  :global(.widget-shell.small) .activity-type {
    min-width: 50px;
    padding: 5px 8px;
    font-size: 10px;
  }

  :global(.widget-shell.small) .row-foot {
    font-size: 10px;
  }

  @keyframes pulse {
    0%, 100% {
      opacity: 0.45;
    }

    50% {
      opacity: 1;
    }
  }

  @media (max-width: 768px) {
    .activity-stats {
      gap: 8px;
    }

    .mini-stat {
      padding: 8px 9px;
    }

    .mini-stat strong {
      font-size: 16px;
    }
  }
</style>
