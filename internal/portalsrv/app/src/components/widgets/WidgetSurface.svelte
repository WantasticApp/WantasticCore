<script lang="ts">
  import { onDestroy, onMount, tick } from "svelte";
  import { fade, scale } from "svelte/transition";
  import { _ } from "$store/i18n";
  import {
    desktopBackground,
    getDesktopBackgroundPreset,
  } from "$store/store";
  import {
    WIDGET_DEFINITIONS,
    WIDGET_LIVE_REFRESH_MS,
    WIDGET_UPTIME_LIVE_REFRESH_MS,
    widgetStore,
    type WidgetId,
    type WidgetLayout,
  } from "$store/widgets";
  import UptimeWidget from "./UptimeWidget.svelte";
  import ActivityWidget from "./ActivityWidget.svelte";
  import StatsWidget from "./StatsWidget.svelte";

  export let mobile = false;

  let draggingId: WidgetId | null = null;
  let dragOverId: WidgetId | null = null;
  let dragPointerId: number | null = null;

  // ── Mobile: long-press context menu ───────────────────────────────
  // The mobile toolbar card was a permanent strip eating real estate
  // for three rarely-used actions. Replace it with a long-press menu
  // that surfaces at the press location — same actions, no card.
  const LONG_PRESS_MS = 500;
  const LONG_PRESS_MOVE_THRESHOLD_PX = 8;
  let pressTimer: ReturnType<typeof setTimeout> | null = null;
  let pressStartX = 0;
  let pressStartY = 0;
  let pressPointerId: number | null = null;
  let menuOpen = false;
  let menuX = 0;
  let menuY = 0;
  let menuEl: HTMLDivElement | null = null;

  // ── Mobile: horizontal paged scroll + page indicator ──────────────
  let railEl: HTMLDivElement | null = null;
  let activePage = 0;

  function updateActivePage() {
    if (!railEl) return;
    const w = railEl.clientWidth || 1;
    activePage = Math.round(railEl.scrollLeft / w);
  }

  function scrollToPage(index: number) {
    if (!railEl) return;
    railEl.scrollTo({ left: railEl.clientWidth * index, behavior: "smooth" });
  }

  $: widgetState = $widgetStore;
  $: enabledWidgets = [...widgetState.widgets]
    .filter((widget) => widget.enabled)
    .sort((left, right) => left.order - right.order);
  $: backgroundPreset = getDesktopBackgroundPreset($desktopBackground);
  $: widgetTheme = backgroundPreset.widgetTheme;
  $: widgetThemeStyle = [
    `--widget-shell-bg: ${widgetTheme.shellBg}`,
    `--widget-shell-border: ${widgetTheme.shellBorder}`,
    `--widget-shell-border-strong: ${widgetTheme.shellBorderStrong}`,
    `--widget-shell-shadow: ${widgetTheme.shellShadow}`,
    `--widget-ink-strong: ${widgetTheme.inkStrong}`,
    `--widget-ink: ${widgetTheme.ink}`,
    `--widget-ink-soft: ${widgetTheme.inkSoft}`,
    `--widget-ink-muted: ${widgetTheme.inkMuted}`,
    `--widget-heading-muted: ${widgetTheme.headingMuted}`,
    `--widget-panel-bg: ${widgetTheme.panelBg}`,
    `--widget-panel-bg-strong: ${widgetTheme.panelBgStrong}`,
    `--widget-panel-border: ${widgetTheme.panelBorder}`,
    `--widget-control-bg: ${widgetTheme.controlBg}`,
    `--widget-control-bg-hover: ${widgetTheme.controlBgHover}`,
    `--widget-control-border: ${widgetTheme.controlBorder}`,
    `--widget-toolbar-bg: ${widgetTheme.toolbarBg}`,
    `--widget-toolbar-border: ${widgetTheme.toolbarBorder}`,
    `--widget-empty-bg: ${widgetTheme.emptyBg}`,
    `--widget-empty-border: ${widgetTheme.emptyBorder}`,
    `--widget-ambient-top: ${widgetTheme.ambientTop}`,
    `--widget-ambient-bottom: ${widgetTheme.ambientBottom}`,
  ].join(";");

  function widgetMeta(layout: WidgetLayout) {
    return WIDGET_DEFINITIONS.find((definition) => definition.id === layout.id);
  }

  function canMoveUp(widgetId: WidgetId): boolean {
    return enabledWidgets.findIndex((widget) => widget.id === widgetId) > 0;
  }

  function canMoveDown(widgetId: WidgetId): boolean {
    const index = enabledWidgets.findIndex((widget) => widget.id === widgetId);
    return index > -1 && index < enabledWidgets.length - 1;
  }

  function startPointerDrag(event: PointerEvent, widgetId: WidgetId) {
    if (!widgetState.editMode) {
      return;
    }

    const target = event.target as HTMLElement | null;
    if (!target?.closest("[data-widget-drag-handle='true']")) {
      return;
    }

    if (event.pointerType === "mouse" && event.button !== 0) {
      return;
    }

    dragPointerId = event.pointerId;
    draggingId = widgetId;
    dragOverId = null;
    document.body.style.userSelect = "none";
    document.body.style.cursor = "grabbing";
    event.preventDefault();
  }

  function updateDropTarget(event: PointerEvent) {
    if (dragPointerId == null || draggingId == null || event.pointerId !== dragPointerId) {
      return;
    }

    const hovered = document
      .elementFromPoint(event.clientX, event.clientY)
      ?.closest(".widget-slot[data-widget-id]") as HTMLElement | null;
    const targetId = hovered?.dataset.widgetId as WidgetId | undefined;

    dragOverId = targetId && targetId !== draggingId ? targetId : null;
  }

  function clearPointerDrag() {
    dragPointerId = null;
    draggingId = null;
    dragOverId = null;
    document.body.style.userSelect = "";
    document.body.style.cursor = "";
  }

  function finishPointerDrag(event: PointerEvent) {
    if (dragPointerId == null || draggingId == null || event.pointerId !== dragPointerId) {
      return;
    }

    const sourceId = draggingId;
    const targetId = dragOverId;
    clearPointerDrag();

    if (targetId && targetId !== sourceId) {
      widgetStore.reorderEnabledWidget(sourceId, targetId);
    }
  }

  function cancelPointerDrag(event: PointerEvent) {
    if (dragPointerId == null || event.pointerId !== dragPointerId) {
      return;
    }

    clearPointerDrag();
  }

  function toggleEditMode() {
    widgetStore.setEditMode(!widgetState.editMode);
  }

  async function manualRefresh() {
    try {
      await widgetStore.refreshAll(true);
    } catch (error) {
      console.error(error);
    }
  }

  // ── Long-press detection ───────────────────────────────────────────
  // Capture-phase pointerdown on the surface; cancel if the finger
  // moves more than the threshold (page swipe) or lifts before the
  // 500ms hold. If a child element marks itself as interactive
  // (button, anchor, input) we yield to it rather than steal the press.
  function startLongPress(event: PointerEvent) {
    if (!mobile) return;
    if (event.pointerType === "mouse" && event.button !== 0) return;
    const t = event.target as HTMLElement | null;
    if (t?.closest("button, a, input, select, textarea, [data-widget-action]"))
      return;

    cancelLongPress();
    pressPointerId = event.pointerId;
    pressStartX = event.clientX;
    pressStartY = event.clientY;
    pressTimer = setTimeout(() => {
      openMenuAt(pressStartX, pressStartY);
      pressTimer = null;
    }, LONG_PRESS_MS);
  }

  function moveLongPress(event: PointerEvent) {
    if (pressPointerId == null || event.pointerId !== pressPointerId) return;
    const dx = event.clientX - pressStartX;
    const dy = event.clientY - pressStartY;
    if (Math.hypot(dx, dy) > LONG_PRESS_MOVE_THRESHOLD_PX) cancelLongPress();
  }

  function cancelLongPress() {
    if (pressTimer) {
      clearTimeout(pressTimer);
      pressTimer = null;
    }
    pressPointerId = null;
  }

  async function openMenuAt(x: number, y: number) {
    menuOpen = true;
    // First render with menu out of view so we can measure it, then
    // clamp into the viewport — prevents the menu poking off-screen
    // when the user long-presses near an edge.
    menuX = x;
    menuY = y;
    await tick();
    if (menuEl) {
      const rect = menuEl.getBoundingClientRect();
      const margin = 8;
      const maxX = window.innerWidth - rect.width - margin;
      const maxY = window.innerHeight - rect.height - margin;
      menuX = Math.max(margin, Math.min(x, maxX));
      menuY = Math.max(margin, Math.min(y, maxY));
    }
  }

  function closeMenu() {
    menuOpen = false;
  }

  function handleMenuRefresh() {
    closeMenu();
    void manualRefresh();
  }
  function handleMenuReset() {
    closeMenu();
    widgetStore.resetLayout();
  }
  function handleMenuCustomize() {
    closeMenu();
    toggleEditMode();
  }

  onMount(() => {
    widgetStore.refreshAll().catch(console.error);

    // Slow loop: refresh activities + any non-uptime widgets at the
    // standard cadence (TTL-gated, so it's a cheap no-op most ticks).
    const slow = window.setInterval(() => {
      widgetStore.refreshAll().catch(console.error);
    }, WIDGET_LIVE_REFRESH_MS);

    // Fast loop: live preview for the Network Uptime widget. force=true
    // bypasses UPTIME_TTL_MS; refreshUptime itself short-circuits if a
    // refresh is already in flight, so overlapping calls can't pile up.
    const fast = window.setInterval(() => {
      widgetStore.refreshUptime(true).catch(console.error);
    }, WIDGET_UPTIME_LIVE_REFRESH_MS);

    return () => {
      window.clearInterval(slow);
      window.clearInterval(fast);
    };
  });

  onDestroy(() => {
    clearPointerDrag();
    widgetStore.setEditMode(false);
  });
</script>

<svelte:window
  on:pointermove={updateDropTarget}
  on:pointerup={finishPointerDrag}
  on:pointercancel={cancelPointerDrag}
/>

<div
  class="widget-surface"
  class:mobile
  style={widgetThemeStyle}
  on:pointerdown|capture={startLongPress}
  on:pointermove|capture={moveLongPress}
  on:pointerup|capture={cancelLongPress}
  on:pointercancel|capture={cancelLongPress}
>
  {#if enabledWidgets.length === 0}
    <div class="empty-panel">
      <strong>{$_("widgets.noWidgetsEnabledTitle")}</strong>
      <p>{$_("widgets.noWidgetsEnabledMessage")}</p>
    </div>
  {:else if mobile}
    <!-- Android-launcher style: one widget per page, horizontal swipe,
         scroll-snap aligns the card. Page dots underneath. -->
    <div
      class="widget-rail"
      bind:this={railEl}
      on:scroll={updateActivePage}
    >
      {#each enabledWidgets as widget (widget.id)}
        {@const meta = widgetMeta(widget)}
        <div class="rail-page" data-widget-id={widget.id}>
          {#if widget.id === "networkUptime"}
            <UptimeWidget
              {widget}
              editMode={widgetState.editMode}
              payload={widgetState.uptime}
              accent={meta?.accent || "rgba(128, 214, 255, 0.85)"}
              sizes={meta?.sizes || ["small", "medium", "large"]}
              on:refresh={() => widgetStore.refreshUptime(true)}
              on:sizechange={(event) =>
                widgetStore.setWidgetSize(widget.id, event.detail)}
              canMoveUp={canMoveUp(widget.id)}
              canMoveDown={canMoveDown(widget.id)}
              on:moveup={() => widgetStore.moveEnabledWidget(widget.id, -1)}
              on:movedown={() => widgetStore.moveEnabledWidget(widget.id, 1)}
            />
          {:else if widget.id === "recentActivity"}
            <ActivityWidget
              {widget}
              editMode={widgetState.editMode}
              payload={widgetState.activities}
              accent={meta?.accent || "rgba(255, 211, 132, 0.85)"}
              sizes={meta?.sizes || ["small", "medium", "large"]}
              on:refresh={() => widgetStore.refreshActivities(true)}
              on:sizechange={(event) =>
                widgetStore.setWidgetSize(widget.id, event.detail)}
              canMoveUp={canMoveUp(widget.id)}
              canMoveDown={canMoveDown(widget.id)}
              on:moveup={() => widgetStore.moveEnabledWidget(widget.id, -1)}
              on:movedown={() => widgetStore.moveEnabledWidget(widget.id, 1)}
            />
          {:else if widget.id === "networkStats"}
            <StatsWidget
              {widget}
              editMode={widgetState.editMode}
              accent={meta?.accent || "rgba(178, 126, 255, 0.88)"}
              sizes={meta?.sizes || ["small", "medium", "large"]}
              on:sizechange={(event) =>
                widgetStore.setWidgetSize(widget.id, event.detail)}
              canMoveUp={canMoveUp(widget.id)}
              canMoveDown={canMoveDown(widget.id)}
              on:moveup={() => widgetStore.moveEnabledWidget(widget.id, -1)}
              on:movedown={() => widgetStore.moveEnabledWidget(widget.id, 1)}
            />
          {/if}
        </div>
      {/each}
    </div>

    {#if enabledWidgets.length > 1}
      <div class="rail-dots" role="tablist" aria-label="Widget pages">
        {#each enabledWidgets as widget, i (widget.id)}
          <button
            type="button"
            class="rail-dot"
            class:active={i === activePage}
            aria-label={`Page ${i + 1}`}
            aria-selected={i === activePage}
            data-widget-action="true"
            on:click={() => scrollToPage(i)}
          />
        {/each}
      </div>
    {/if}
  {:else}
    <div class="widget-grid" class:editing={widgetState.editMode}>
      {#each enabledWidgets as widget (widget.id)}
        {@const meta = widgetMeta(widget)}
        <div
          class="widget-slot"
          class:size-small={widget.size === "small"}
          class:size-medium={widget.size === "medium"}
          class:size-large={widget.size === "large"}
          class:editable={widgetState.editMode}
          class:dragging={draggingId === widget.id}
          class:drag-over={dragOverId === widget.id}
          data-widget-id={widget.id}
          on:pointerdown={(event) => startPointerDrag(event, widget.id)}
        >
          {#if widget.id === "networkUptime"}
            <UptimeWidget
              {widget}
              editMode={widgetState.editMode}
              payload={widgetState.uptime}
              accent={meta?.accent || "rgba(128, 214, 255, 0.85)"}
              sizes={meta?.sizes || ["small", "medium", "large"]}
              on:refresh={() => widgetStore.refreshUptime(true)}
              on:sizechange={(event) => widgetStore.setWidgetSize(widget.id, event.detail)}
              canMoveUp={canMoveUp(widget.id)}
              canMoveDown={canMoveDown(widget.id)}
              on:moveup={() => widgetStore.moveEnabledWidget(widget.id, -1)}
              on:movedown={() => widgetStore.moveEnabledWidget(widget.id, 1)}
            />
          {:else if widget.id === "recentActivity"}
            <ActivityWidget
              {widget}
              editMode={widgetState.editMode}
              payload={widgetState.activities}
              accent={meta?.accent || "rgba(255, 211, 132, 0.85)"}
              sizes={meta?.sizes || ["small", "medium", "large"]}
              on:refresh={() => widgetStore.refreshActivities(true)}
              on:sizechange={(event) => widgetStore.setWidgetSize(widget.id, event.detail)}
              canMoveUp={canMoveUp(widget.id)}
              canMoveDown={canMoveDown(widget.id)}
              on:moveup={() => widgetStore.moveEnabledWidget(widget.id, -1)}
              on:movedown={() => widgetStore.moveEnabledWidget(widget.id, 1)}
            />
          {:else if widget.id === "networkStats"}
            <StatsWidget
              {widget}
              editMode={widgetState.editMode}
              accent={meta?.accent || "rgba(178, 126, 255, 0.88)"}
              sizes={meta?.sizes || ["small", "medium", "large"]}
              on:sizechange={(event) => widgetStore.setWidgetSize(widget.id, event.detail)}
              canMoveUp={canMoveUp(widget.id)}
              canMoveDown={canMoveDown(widget.id)}
              on:moveup={() => widgetStore.moveEnabledWidget(widget.id, -1)}
              on:movedown={() => widgetStore.moveEnabledWidget(widget.id, 1)}
            />
          {/if}
        </div>
      {/each}
    </div>
  {/if}
</div>

{#if menuOpen}
  <!-- Backdrop catches taps outside the menu to dismiss it. -->
  <div
    class="press-menu-backdrop"
    transition:fade={{ duration: 120 }}
    on:click={closeMenu}
    on:contextmenu|preventDefault={closeMenu}
    on:pointerdown|stopPropagation
  />
  <div
    bind:this={menuEl}
    class="press-menu"
    style="left: {menuX}px; top: {menuY}px"
    transition:scale={{ duration: 140, start: 0.92 }}
    role="menu"
  >
    <button
      type="button"
      class="press-menu-item"
      data-widget-action="true"
      on:click={handleMenuRefresh}
    >
      {$_("common.refresh")}
    </button>
    <button
      type="button"
      class="press-menu-item"
      data-widget-action="true"
      on:click={handleMenuReset}
    >
      {$_("widgets.resetLayout")}
    </button>
    <button
      type="button"
      class="press-menu-item primary"
      data-widget-action="true"
      on:click={handleMenuCustomize}
    >
      {widgetState.editMode ? $_("widgets.done") : $_("widgets.customize")}
    </button>
  </div>
{/if}

<style>
  .widget-surface {
    position: absolute;
    inset: 18px 18px 18px 92px;
    display: flex;
    flex-direction: column;
    gap: 16px;
    pointer-events: none;
    z-index: 1;
  }

  .widget-surface.mobile {
    inset: auto 0 14px 0;
    top: clamp(280px, 54vh, 430px);
    gap: 8px;
    max-height: calc(100% - clamp(280px, 54vh, 430px) - 14px);
    /* horizontal-paged rail handles its own overflow; surface itself
       no longer needs to scroll. Lock vertical overflow so nothing
       sneaks past the rail. */
    overflow: hidden;
    padding: 0;
    pointer-events: auto;
  }

  .empty-panel,
  .widget-grid,
  .widget-rail,
  .widget-slot {
    pointer-events: auto;
  }

  /* Android-launcher style horizontal paged rail. Each child takes a
     full viewport width, scroll-snap aligns the active one. */
  .widget-rail {
    display: flex;
    flex: 1 1 auto;
    min-height: 0;
    overflow-x: auto;
    overflow-y: hidden;
    scroll-snap-type: x mandatory;
    scroll-behavior: smooth;
    scrollbar-width: none;
    -webkit-overflow-scrolling: touch;
    overscroll-behavior-x: contain;
  }
  .widget-rail::-webkit-scrollbar {
    display: none;
  }

  .rail-page {
    flex: 0 0 100%;
    min-width: 100%;
    box-sizing: border-box;
    padding: 0 12px;
    scroll-snap-align: center;
    scroll-snap-stop: always;
    display: flex;
    align-items: stretch;
  }
  .rail-page > :global(*) {
    flex: 1 1 auto;
    min-width: 0;
  }

  .rail-dots {
    display: flex;
    align-items: center;
    justify-content: center;
    gap: 6px;
    padding: 4px 0 2px;
    pointer-events: auto;
  }
  .rail-dot {
    width: 6px;
    height: 6px;
    border-radius: 999px;
    border: none;
    padding: 0;
    background: var(--widget-ink-muted);
    opacity: 0.45;
    cursor: pointer;
    transition:
      width 180ms ease,
      opacity 180ms ease,
      background 180ms ease;
  }
  .rail-dot.active {
    width: 18px;
    opacity: 1;
    background: var(--widget-ink);
  }

  /* Long-press context menu — appears at the press location. */
  .press-menu-backdrop {
    position: fixed;
    inset: 0;
    background: transparent;
    z-index: 50;
    -webkit-tap-highlight-color: transparent;
  }
  .press-menu {
    position: fixed;
    z-index: 51;
    min-width: 180px;
    padding: 6px;
    display: grid;
    gap: 2px;
    border-radius: 14px;
    background: var(--widget-toolbar-bg);
    border: 1px solid var(--widget-toolbar-border);
    box-shadow:
      inset 0 1px 0 rgba(255, 255, 255, 0.08),
      0 18px 38px var(--widget-shell-shadow);
    backdrop-filter: blur(24px) saturate(160%);
    -webkit-backdrop-filter: blur(24px) saturate(160%);
    transform-origin: top left;
  }
  .press-menu-item {
    appearance: none;
    border: none;
    background: transparent;
    color: var(--widget-ink);
    text-align: left;
    padding: 10px 12px;
    border-radius: 10px;
    font-size: 13px;
    font-weight: 600;
    cursor: pointer;
  }
  .press-menu-item:hover,
  .press-menu-item:active {
    background: var(--widget-control-bg-hover);
  }
  .press-menu-item.primary {
    color: var(--widget-ink-strong);
  }

  .widget-grid {
    display: grid;
    grid-template-columns: repeat(12, minmax(0, 1fr));
    gap: 18px;
    align-content: start;
  }

  .widget-slot {
    --widget-slot-height: clamp(280px, 34vh, 360px);
    display: flex;
    min-width: 0;
    min-height: 216px;
    height: var(--widget-slot-height);
    transition:
      transform 170ms ease,
      filter 170ms ease,
      box-shadow 170ms ease;
  }

  .widget-slot > :global(*) {
    flex: 1 1 auto;
    min-width: 0;
    min-height: 0;
  }

  .widget-slot.size-small {
    grid-column: span 4;
    --widget-slot-height: clamp(280px, 34vh, 360px);
  }

  .widget-slot.size-medium {
    grid-column: span 6;
    --widget-slot-height: clamp(320px, 40vh, 430px);
  }

  .widget-slot.size-large {
    grid-column: span 8;
    --widget-slot-height: clamp(380px, 52vh, 540px);
  }

  /* Network Statistics is a compact card — shrink its slot so the
     content sits flush against the footer instead of leaving large
     empty space below. The widget itself enforces overflow clipping
     so a temporarily larger header (edit-mode controls) cannot bleed
     onto the footer. */
  .widget-slot.size-medium[data-widget-id="networkStats"] {
    --widget-slot-height: clamp(260px, 30vh, 320px);
  }

  .widget-slot.size-large[data-widget-id="networkStats"] {
    --widget-slot-height: clamp(300px, 36vh, 360px);
  }

  .widget-slot.editable {
    cursor: default;
  }

  .widget-slot.dragging {
    transform: scale(0.985);
    filter: saturate(0.94);
    opacity: 0.78;
  }

  .widget-slot.drag-over {
    transform: translateY(-4px);
    box-shadow:
      0 0 0 2px var(--widget-shell-border-strong),
      0 12px 28px var(--widget-shell-shadow);
    border-radius: 24px;
  }

  .empty-panel {
    align-self: flex-end;
    max-width: 360px;
    padding: 18px 20px;
    border-radius: 22px;
    background: var(--widget-empty-bg);
    border: 1px solid var(--widget-empty-border);
    box-shadow:
      inset 0 1px 0 rgba(255, 255, 255, 0.08),
      0 12px 26px var(--widget-shell-shadow);
    backdrop-filter: blur(22px) saturate(145%);
    -webkit-backdrop-filter: blur(22px) saturate(145%);
    color: var(--widget-ink);
  }

  .empty-panel strong {
    display: block;
    font-size: 15px;
    line-height: 1.2;
  }

  .empty-panel p {
    margin: 8px 0 0;
    font-size: 12px;
    line-height: 1.55;
    color: var(--widget-ink-soft);
  }

  @media (max-height: 860px) and (min-width: 960px) {
    .widget-grid {
      display: flex;
      flex-wrap: wrap;
      justify-content: flex-end;
      align-content: flex-start;
    }

    .widget-slot {
      flex: 0 1 min(100%, 620px);
      height: clamp(250px, 31vh, 320px);
    }

    .widget-slot.size-small {
      flex-basis: min(100%, 620px);
      max-width: 620px;
      height: clamp(250px, 31vh, 320px);
    }

    .widget-slot.size-medium {
      flex-basis: min(100%, 940px);
      max-width: 940px;
      height: clamp(285px, 36vh, 380px);
    }

    .widget-slot.size-large {
      flex-basis: min(100%, 1240px);
      max-width: 1240px;
      height: clamp(330px, 44vh, 460px);
    }

    /* Compact override for networkStats in short-viewport mode. */
    .widget-slot.size-medium[data-widget-id="networkStats"] {
      height: clamp(240px, 26vh, 300px);
    }

    .widget-slot.size-large[data-widget-id="networkStats"] {
      height: clamp(270px, 30vh, 340px);
    }
  }

  @media (max-width: 1100px) {
    .widget-surface {
      inset: 16px 16px 16px 86px;
    }

    .widget-slot.size-small {
      grid-column: span 6;
    }

    .widget-slot.size-medium,
    .widget-slot.size-large {
      grid-column: span 12;
    }
  }

  @media (max-width: 768px) {
    .widget-surface.mobile {
      top: clamp(236px, 48vh, 360px);
      max-height: calc(100% - clamp(236px, 48vh, 360px) - 14px);
      /* On phones the surface scrolls vertically — let it stretch full
         width and stack the rail pages instead of forcing 12-col grid. */
      left: 8px;
      right: 8px;
      inset: clamp(236px, 48vh, 360px) 8px 14px 8px;
    }

    /* Stack the rail's pages so users scroll one-handed instead of
       swiping horizontally across a tiny screen. */
    .widget-rail {
      flex-direction: column;
      overflow-y: auto;
      overflow-x: hidden;
      scroll-snap-type: none;
    }
    .rail-page {
      width: 100%;
      flex: 0 0 auto;
      scroll-snap-align: none;
      padding: 0;
    }
    .rail-dots {
      display: none;
    }

    /* Each card spans the full width with the grid auto-stacking. */
    .widget-grid {
      grid-template-columns: 1fr;
      gap: 12px;
    }
    .widget-slot,
    .widget-slot.size-small,
    .widget-slot.size-wide,
    .widget-slot.size-tall {
      grid-column: 1 / -1 !important;
      grid-row: auto !important;
      min-height: 0;
    }

    .empty-panel {
      align-self: stretch;
      max-width: none;
      margin: 0 12px;
    }
  }
</style>
