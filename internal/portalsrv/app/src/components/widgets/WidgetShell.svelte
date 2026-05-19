<script lang="ts">
  import { createEventDispatcher } from "svelte";
  import { _ } from "$store/i18n";
  import type { WidgetSize } from "$store/widgets";

  export let title = "";
  export let subtitle = "";
  export let accent = "rgba(132, 205, 255, 0.85)";
  export let size: WidgetSize = "medium";
  export let sizes: WidgetSize[] = ["medium", "large"];
  export let editMode = false;
  export let loading = false;
  export let error = "";
  export let lastUpdated: number | null = null;
  export let canMoveUp = true;
  export let canMoveDown = true;

  const dispatch = createEventDispatcher<{
    refresh: void;
    sizechange: WidgetSize;
    moveup: void;
    movedown: void;
  }>();

  function sizeLabel(widgetSize: WidgetSize): string {
    if (widgetSize === "small") return $_("widgets.sizeSmall");
    if (widgetSize === "large") return $_("widgets.sizeLarge");
    return $_("widgets.sizeMedium");
  }
</script>

<div
  class="widget-shell"
  class:small={size === "small"}
  class:medium={size === "medium"}
  class:large={size === "large"}
  style={`--widget-accent: ${accent};`}
>
  <div class="header">
    <div class="heading">
      <span class="accent-dot" />
      <div class="heading-copy">
        <p class="subtitle">{subtitle}</p>
        <h3>{title}</h3>
      </div>
    </div>

    <div class="actions">
      <button
        class="icon-button"
        type="button"
        title={$_("widgets.refreshWidget")}
        on:click={() => dispatch("refresh")}
      >
        <svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2">
          <polyline points="23 4 23 10 17 10" />
          <polyline points="1 20 1 14 7 14" />
          <path d="M3.51 9a9 9 0 0114.13-3.36L23 10M1 14l5.36 4.36A9 9 0 0020.49 15" />
        </svg>
      </button>

      {#if editMode}
        <button
          class="icon-button drag-handle"
          type="button"
          data-widget-drag-handle="true"
          title={$_("widgets.reorder")}
          on:click|preventDefault
        >
          <svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2">
            <circle cx="8" cy="7" r="1" />
            <circle cx="8" cy="12" r="1" />
            <circle cx="8" cy="17" r="1" />
            <circle cx="16" cy="7" r="1" />
            <circle cx="16" cy="12" r="1" />
            <circle cx="16" cy="17" r="1" />
          </svg>
        </button>

        <div class="move-group" aria-label={$_("widgets.reorder")}>
          <button
            class="icon-button"
            type="button"
            title={$_("widgets.moveUp")}
            disabled={!canMoveUp}
            on:click={() => dispatch("moveup")}
          >
            <svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2">
              <polyline points="18 15 12 9 6 15" />
            </svg>
          </button>
          <button
            class="icon-button"
            type="button"
            title={$_("widgets.moveDown")}
            disabled={!canMoveDown}
            on:click={() => dispatch("movedown")}
          >
            <svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2">
              <polyline points="6 9 12 15 18 9" />
            </svg>
          </button>
        </div>

        <div class="size-group" aria-label={$_("widgets.resizeWidget")}>
          {#each sizes as option}
            <button
              class="size-chip"
              class:active={option === size}
              type="button"
              title={sizeLabel(option)}
              on:click={() => dispatch("sizechange", option)}
            >
              {option === "small" ? "S" : option === "medium" ? "M" : "L"}
            </button>
          {/each}
        </div>
      {/if}
    </div>
  </div>

  {#if error}
    <div class="widget-error">{error}</div>
  {:else}
    <div class="content" class:loading>
      <slot />
    </div>
  {/if}

  <div class="footer">
    {#if editMode}
      <span class="footer-note">{$_("widgets.dragToReorder")}</span>
    {:else if lastUpdated}
      <span class="footer-note">
        {$_("widgets.updatedAgo", {
          values: { time: new Date(lastUpdated).toLocaleTimeString([], { hour: "2-digit", minute: "2-digit" }) },
        })}
      </span>
    {:else}
      <span class="footer-note">{loading ? $_("widgets.refreshing") : $_("widgets.awaitingData")}</span>
    {/if}

    <slot name="footer" />
  </div>
</div>

<style>
  .widget-shell {
    position: relative;
    isolation: isolate;
    --widget-shell-bg: rgba(11, 17, 29, 0.44);
    --widget-shell-border: rgba(167, 184, 214, 0.18);
    --widget-shell-border-strong: rgba(218, 229, 248, 0.3);
    --widget-shell-shadow: rgba(4, 9, 18, 0.22);
    --widget-ink-strong: rgba(246, 250, 255, 0.98);
    --widget-ink: rgba(232, 239, 248, 0.94);
    --widget-ink-soft: rgba(189, 203, 223, 0.88);
    --widget-ink-muted: rgba(154, 170, 193, 0.82);
    --widget-heading-muted: rgba(198, 210, 229, 0.78);
    --widget-panel-bg: rgba(9, 16, 29, 0.15);
    --widget-panel-bg-strong: rgba(9, 16, 29, 0.22);
    --widget-panel-border: rgba(183, 200, 226, 0.12);
    --widget-control-bg: rgba(255, 255, 255, 0.04);
    --widget-control-bg-hover: rgba(255, 255, 255, 0.1);
    --widget-control-border: rgba(210, 223, 245, 0.16);
    --widget-ambient-top: rgba(116, 145, 255, 0.18);
    --widget-ambient-bottom: rgba(57, 119, 203, 0.12);
    display: flex;
    flex-direction: column;
    height: 100%;
    padding: 16px 16px 14px;
    border-radius: 24px;
    overflow: hidden;
    color: var(--widget-ink);
    background: var(--widget-shell-bg);
    border: 1px solid var(--widget-shell-border);
    box-shadow:
      inset 0 1px 0 rgba(255, 255, 255, 0.11),
      inset 0 0 0 1px rgba(255, 255, 255, 0.03),
      0 16px 32px var(--widget-shell-shadow);
    backdrop-filter: blur(30px) saturate(160%);
    -webkit-backdrop-filter: blur(30px) saturate(160%);
    font-family:
      "SF Pro Display",
      "SF Pro Text",
      -apple-system,
      BlinkMacSystemFont,
      "Segoe UI",
      sans-serif;
  }

  .widget-shell.small {
    padding: 14px 14px 12px;
  }

  .widget-shell::before,
  .widget-shell::after {
    content: "";
    position: absolute;
    inset: 0;
    pointer-events: none;
    border-radius: inherit;
  }

  .widget-shell::before {
    background:
      radial-gradient(circle at 14% 14%, var(--widget-ambient-top) 0%, transparent 34%),
      radial-gradient(circle at 84% 100%, var(--widget-ambient-bottom) 0%, transparent 38%);
    opacity: 0.84;
  }

  .widget-shell::after {
    inset: 0;
    background:
      radial-gradient(circle at 100% 0%, rgba(255, 255, 255, 0.08) 0%, transparent 22%),
      linear-gradient(135deg, rgba(255, 255, 255, 0.028) 0%, transparent 34%);
    opacity: 0.34;
  }

  .header,
  .content,
  .footer {
    position: relative;
    z-index: 1;
  }

  .header {
    display: flex;
    align-items: flex-start;
    justify-content: space-between;
    gap: 12px;
  }

  .heading {
    display: flex;
    align-items: flex-start;
    gap: 10px;
    min-width: 0;
  }

  .accent-dot {
    width: 11px;
    height: 11px;
    margin-top: 4px;
    border-radius: 999px;
    background: var(--widget-accent);
    box-shadow:
      0 0 0 5px rgba(255, 255, 255, 0.03),
      0 0 0 1px rgba(255, 255, 255, 0.16),
      0 10px 18px rgba(0, 0, 0, 0.16);
    flex: 0 0 auto;
  }

  .heading-copy {
    min-width: 0;
  }

  .subtitle {
    margin: 0 0 4px;
    font-size: clamp(11px, 0.65vw, 13px);
    font-weight: 600;
    letter-spacing: 0.08em;
    text-transform: uppercase;
    color: var(--widget-heading-muted);
  }

  h3 {
    margin: 0;
    font-size: clamp(18px, 1.5vw, 20px);
    line-height: 1.15;
    font-weight: 650;
    letter-spacing: -0.02em;
    color: var(--widget-ink-strong);
  }

  .widget-shell.small h3 {
    font-size: 18px;
  }

  .actions {
    display: flex;
    align-items: center;
    gap: 8px;
    flex-wrap: wrap;
    justify-content: flex-end;
  }

  .widget-shell.small .actions {
    gap: 6px;
  }

  .move-group,
  .size-group {
    display: inline-flex;
    align-items: center;
    gap: 6px;
    padding: 4px;
    border-radius: 999px;
    background: var(--widget-control-bg);
    border: 1px solid var(--widget-control-border);
    box-shadow:
      inset 0 1px 0 rgba(255, 255, 255, 0.05),
      0 6px 16px rgba(0, 0, 0, 0.12);
    backdrop-filter: blur(12px) saturate(135%);
    -webkit-backdrop-filter: blur(12px) saturate(135%);
  }

  .icon-button,
  .size-chip {
    display: inline-flex;
    align-items: center;
    justify-content: center;
    border: 1px solid var(--widget-control-border);
    color: var(--widget-ink);
    background: var(--widget-control-bg);
    cursor: pointer;
    transition:
      transform 150ms ease,
      background 150ms ease,
      color 150ms ease,
      border-color 150ms ease,
      box-shadow 150ms ease;
    backdrop-filter: blur(14px);
    -webkit-backdrop-filter: blur(14px);
  }

  .icon-button {
    width: 30px;
    height: 30px;
    border-radius: 999px;
  }

  .drag-handle {
    cursor: grab;
    touch-action: none;
  }

  .drag-handle:active {
    cursor: grabbing;
  }

  .icon-button:disabled,
  .size-chip:disabled {
    opacity: 0.42;
    cursor: not-allowed;
    transform: none;
  }

  .size-chip {
    width: 28px;
    height: 28px;
    border-radius: 999px;
    font-size: 11px;
    font-weight: 700;
  }

  .icon-button:hover,
  .size-chip:hover,
  .size-chip.active {
    color: var(--widget-ink-strong);
    background: var(--widget-control-bg-hover);
    border-color: var(--widget-shell-border-strong);
    box-shadow:
      inset 0 1px 0 rgba(255, 255, 255, 0.05),
      0 10px 18px rgba(0, 0, 0, 0.14);
    transform: translateY(-1px);
  }

  .icon-button:disabled:hover,
  .size-chip:disabled:hover {
    background: var(--widget-control-bg);
    border-color: var(--widget-control-border);
    box-shadow: none;
  }

  .content {
    display: flex;
    flex: 1 1 auto;
    min-height: 0;
    margin-top: 16px;
  }

  .widget-shell.small .content {
    margin-top: 12px;
  }

  .content.loading {
    opacity: 0.8;
  }

  .widget-error {
    margin-top: 14px;
    padding: 12px 13px;
    border-radius: 14px;
    background: rgba(92, 24, 36, 0.42);
    border: 1px solid rgba(255, 190, 204, 0.18);
    font-size: 12px;
    line-height: 1.45;
    color: rgba(255, 220, 228, 0.9);
    backdrop-filter: blur(12px);
    -webkit-backdrop-filter: blur(12px);
  }

  .footer {
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: 12px;
    margin-top: 12px;
  }

  .widget-shell.small .footer {
    margin-top: 10px;
  }

  .footer-note {
    font-size: clamp(10px, 0.5vw, 11px);
    font-weight: 600;
    letter-spacing: 0.05em;
    text-transform: uppercase;
    color: var(--widget-ink-soft);
  }

  .widget-shell.small .footer-note {
    font-size: 10px;
  }

  @media (max-width: 768px) {
    .widget-shell {
      padding: 14px 14px 12px;
      border-radius: 18px;
    }

    h3 {
      font-size: 17px;
    }

    .actions {
      gap: 6px;
    }
  }
</style>
