<script lang="ts">
  import { draggable } from "@neodrag/svelte";
  import { scale } from "svelte/transition";
  import Titlebar from "$components/shared/Titlebar.svelte";
  import {
    activeThing,
    appZIndexes,
    bringToFront,
    openedApps,
    minimizedApps,
  } from "$store/store";
  import { isMobile } from "$store/ui";

  export let appName: string;
  export let title: string = "";
  // Optional width/height with defaults
  export let width = "800px";
  export let height = "600px";
  export let minWidth = "320px";
  export let minHeight = "200px";
  export let top = "10%";
  export let left = "10%";
  export let canMaximize = true;
  export let canReduce = true;
  export let canClose = true;
  export let dragDisabled = false;
  export let canResize = false;
  // Window state
  export let isMaximized = false;

  // Z-index
  $: zIndex = $appZIndexes[appName] || 100;

  // Minimize state
  $: isMinimized = $minimizedApps.includes(appName);

  function handleFocus() {
    $activeThing = appName;
    bringToFront(appName);
  }

  function handleMaximize() {
    if (!canMaximize) return;
    isMaximized = !isMaximized;
  }

  function handleReduce() {
    if (!$minimizedApps.includes(appName)) {
      $minimizedApps = [...$minimizedApps, appName];
    }
    // Clear active thing so taskbar can restore it
    if ($activeThing === appName) {
      $activeThing = "";
    }
  }

  // Restore if active
  $: if ($activeThing === appName) {
    bringToFront(appName);
    if ($minimizedApps.includes(appName)) {
      $minimizedApps = $minimizedApps.filter((a) => a !== appName);
    }
  }

  // Draggable options
  $: dragOptions = {
    handle: ".title-bar",
    disabled: isMaximized || $isMobile || dragDisabled,
    bounds: "body",
  };

  // Close is handled by Titlebar bubbling or we can handle activeThing reset here,
  // but Titlebar has specific logic for closing.
</script>

<div
  class="app-window activeShadow"
  class:maximized={isMaximized || $isMobile}
  class:minimized={isMinimized}
  class:resizable={canResize && !isMaximized && !$isMobile}
  style:z-index={zIndex}
  style:width={isMaximized || $isMobile ? "100vw" : width}
  style:height={isMaximized || $isMobile ? "calc(100dvh - 48px)" : height}
  style:top={isMaximized || $isMobile ? "0" : top}
  style:left={isMaximized || $isMobile ? "0" : left}
  style:min-width={isMaximized || $isMobile ? "100%" : minWidth}
  style:min-height={isMaximized || $isMobile ? "100%" : minHeight}
  on:mousedown={handleFocus}
  on:touchstart={handleFocus}
  use:draggable={dragOptions}
  transition:scale={{ duration: 200, start: 0.95 }}
>
  <Titlebar
    {appName}
    {title}
    canMaximize={canMaximize && !$isMobile}
    {canReduce}
    {canClose}
    on:maximize={handleMaximize}
    on:reduce={handleReduce}
    on:close
    on:goBack
  >
    <slot name="header_icon" slot="default">
      <!-- Default icon behavior in Titlebar or custom -->
    </slot>
  </Titlebar>

  <div class="window-content">
    <slot />
  </div>
</div>

<style>
  .app-window {
    position: absolute;
    background: var(--mica);
    border-radius: 8px; /* Standardize border radius */
    backdrop-filter: blur(40px);
    box-shadow: 0 8px 32px rgba(0, 0, 0, 0.3);
    overflow: hidden;
    display: flex;
    flex-direction: column;
    /* transition: width 0.2s, height 0.2s, top 0.2s, left 0.2s; - Might conflict with drag */
  }

  .resizable {
    resize: both;
    overflow: hidden; /* was: auto — but this caused terminal/content to overflow the window */
  }

  .window-content {
    flex: 1;
    overflow: hidden;
    display: flex;
    flex-direction: column;
    position: relative;
    min-height: 0; /* allow flex children to shrink below content size */
  }

  .maximized {
    border-radius: 0 !important;
    top: 0 !important;
    left: 0 !important;
    /* Fixed position is handled by inline styles or component logic */
  }

  .minimized {
    display: none !important;
    /* Although Desktop.svelte handles hiding, we can also double check here */
  }

  @media (max-width: 768px) {
    .app-window {
      position: fixed !important;
      border-radius: 0;
      top: 0 !important;
      left: 0 !important;
      bottom: 48px !important; /* Taskbar height */
      height: calc(100dvh - 48px) !important;
      width: 100vw !important;
    }
  }
</style>
