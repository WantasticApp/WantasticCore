<script lang="ts">
  import {
    activeThing,
    brightness,
    openedApps,
    minimizedApps,
    bringToFront,
    desktopBackground,
    desktopBackgroundColors,
    DEFAULT_DESKTOP_BACKGROUND,
  } from "$store/store";
  import { isMobile } from "$store/ui";
  import { websshStore } from "$store/webssh";
  import { webProxyStore } from "$store/webproxy";
  import { toasts } from "$store/toast";
  import { t, _ } from "$store/i18n";
  import { widgetStore } from "$store/widgets";

  import TaskSwitcher from "$components/TaskSwitcher.svelte";
  import Toast from "$components/Toast.svelte";
  import DeviceAuthNotification from "$components/DeviceAuthNotification.svelte";
  import WidgetSurface from "$components/widgets/WidgetSurface.svelte";
  import { onMount, tick } from "svelte";
  import { peerStore } from "$store/peer";
  import { accountStore } from "$store/account";
  import { topologyStore } from "$store/topology";

  // All possible desktop apps
  const allDskApps = [
    "Peers",
    "WebSSH",
    "Winbox",
    "Topology",
    "Account",
    "Copilot",
    "Admin",
  ];

  const dskApps = allDskApps;

  // Selected desktop app (single click selects, double click opens)
  let selectedApp: string | null = null;

  // Context menu state for desktop background
  let showBackgroundMenu = false;
  let contextMenuX = 0;
  let contextMenuY = 0;
  let contextMenuMaxHeight = 0;
  let desktopElement: HTMLDivElement | null = null;
  let contextMenuElement: HTMLDivElement | null = null;

  const CONTEXT_MENU_MARGIN = 12;
  const CONTEXT_MENU_MIN_HEIGHT = 220;

  function clamp(value: number, min: number, max: number): number {
    if (max < min) {
      return min;
    }
    return Math.min(Math.max(value, min), max);
  }

  function getDesktopBounds() {
    if (desktopElement) {
      return desktopElement.getBoundingClientRect();
    }

    return {
      left: 0,
      top: 0,
      right: window.innerWidth,
      bottom: window.innerHeight,
      width: window.innerWidth,
      height: window.innerHeight,
    };
  }

  function positionContextMenu() {
    if (!showBackgroundMenu || !contextMenuElement || typeof window === "undefined") {
      return;
    }

    const desktopBounds = getDesktopBounds();
    contextMenuMaxHeight = Math.max(
      CONTEXT_MENU_MIN_HEIGHT,
      Math.floor(desktopBounds.height - CONTEXT_MENU_MARGIN * 2)
    );

    const menuRect = contextMenuElement.getBoundingClientRect();
    const minX = desktopBounds.left + CONTEXT_MENU_MARGIN;
    const maxX =
      desktopBounds.right - menuRect.width - CONTEXT_MENU_MARGIN;
    const minY = desktopBounds.top + CONTEXT_MENU_MARGIN;
    const maxY =
      desktopBounds.bottom - menuRect.height - CONTEXT_MENU_MARGIN;

    contextMenuX = clamp(contextMenuX, minX, maxX);
    contextMenuY = clamp(contextMenuY, minY, maxY);
  }

  async function openContextMenuAt(x: number, y: number) {
    const desktopBounds = getDesktopBounds();
    contextMenuX = x;
    contextMenuY = y;
    contextMenuMaxHeight = Math.max(
      CONTEXT_MENU_MIN_HEIGHT,
      Math.floor(desktopBounds.height - CONTEXT_MENU_MARGIN * 2)
    );
    showBackgroundMenu = true;
    await tick();
    positionContextMenu();
  }

  function handleDesktopContextMenu(event: MouseEvent) {
    // Only show context menu if clicking on the desktop itself, not on apps
    const target = event.target as HTMLElement;
    if (
      target.closest(".dskApp") ||
      target.closest(".mobileApp") ||
      target.closest(".apps")
    ) {
      return;
    }
    event.preventDefault();
    openContextMenuAt(event.clientX, event.clientY).catch(console.error);
  }

  function hideContextMenu() {
    showBackgroundMenu = false;
  }

  function handleWindowResize() {
    if (showBackgroundMenu) {
      positionContextMenu();
    }
  }

  function setBackgroundColor(color: string) {
    $desktopBackground = color;
    hideContextMenu();
  }

  async function refreshWidgetsFromMenu() {
    try {
      await widgetStore.refreshAll(true);
    } catch (error) {
      console.error(error);
    }
    hideContextMenu();
  }

  function toggleWidgetEditModeFromMenu() {
    widgetStore.setEditMode(!$widgetStore.editMode);
    hideContextMenu();
  }

  function resetWidgetsFromMenu() {
    widgetStore.resetLayout();
    hideContextMenu();
  }

  onMount(async () => {
    // Load saved icon order (mobile launcher)
    loadIconOrder();

    // Set initial background from localStorage or default
    const savedBg = localStorage.getItem("desktopBackground");
    if (savedBg) {
      $desktopBackground = savedBg;
    } else {
      setGradientBackground();
    }
    // check if it's the first login and show welcome message
    let isFirstLogin = await cookieStore.get("firsttime");
    if (isFirstLogin) {
      toggleOpenApp("AddPeer");
      await cookieStore.delete("firsttime");
    }
  });
  function setGradientBackground() {
    $desktopBackground = DEFAULT_DESKTOP_BACKGROUND;
    hideContextMenu();
  }

  // Display names for apps (when different from internal name)
  const appDisplayNames: Record<string, string> = {
    Winbox: $_("desktop.winbox"),
    Peers: $_("desktop.devices"),
    WebSSH: $_("desktop.webSsh"),
    Topology: $_("desktop.topology"),
    Account: $_("desktop.account"),
    Copilot: "Copilot",
    Admin: "Admin",
  };

  function getDisplayName(app: string): string {
    return appDisplayNames[app] || app;
  }

  // Get open terminals from store
  $: openTerminals = $websshStore.openTerminals;

  // Get open browser windows from store
  $: openBrowsers = $webProxyStore.openBrowsers;

  // Track failed apps to prevent retry loops
  let failedApps = new Set<string>();

  // Error handling for app imports
  let appErrors: Record<string, string> = {};

  // Select app on single click (animates the icon)
  function selectApp(app: string) {
    selectedApp = app;
  }

  // Deselect when clicking on desktop
  function handleDesktopClick(event: MouseEvent) {
    const target = event.target as HTMLElement;
    if (!target.closest(".dskApp") && !target.closest(".mobileApp")) {
      selectedApp = null;
    }
  }

  const toggleOpenApp = (app: string) => {
    if ($openedApps.includes(app)) {
      $activeThing = "";
      $openedApps = $openedApps.filter((oa) => oa !== app);
    } else {
      $activeThing = app;
      $openedApps = [...$openedApps, app];
      bringToFront(app);
    }
  };

  // For mobile, single tap opens the app
  const openApp = (app: string) => {
    if (!$openedApps.includes(app)) {
      $openedApps = [...$openedApps, app];
    }
    $activeThing = app;
    bringToFront(app);
  };

  function handleAppError(appName: string, error: any) {
    if (error?.message) {
      console.error(`Error loading ${appName}:`, error.message);
    }
    appErrors[appName] = error?.message || "Failed to load app";
    failedApps.add(appName);

    // Remove from opened apps to prevent render loop
    $openedApps = $openedApps.filter((app) => app !== appName);

    const errorMsg = String(error?.message || error || "");
    const isChunkError =
      errorMsg.includes("failed to fetch dynamically imported module") ||
      errorMsg.includes("importing a module script failed") ||
      errorMsg.includes("Failed to dynamically import module") ||
      errorMsg.includes("error loading dynamically imported module") || // Common Vite error string
      errorMsg.includes("dynamically imported module");

    if (isChunkError) {
      toasts.info(
        $_("desktop.updatingApp", {
          default: "Updating app to latest version...",
        }),
      );
      setTimeout(() => {
        window.location.reload();
      }, 1000);
      return;
    }

    // Show toast notification with translated message
    toasts.error(
      $_("desktop.failedToLoad", {
        values: {
          app: appName,
          error: errorMsg || $_("errors.somethingWentWrong"),
        },
      }),
    );
  }

  // Preloads the app chunk and store data before click
  let preloadedApps = new Set<string>();
  const preloadApp = (appName: string) => {
    if (preloadedApps.has(appName) || failedApps.has(appName)) return;
    preloadedApps.add(appName);

    // Warm up Vite's chunk cache
    try {
      if (appName === "Peers") import("$apps/Peers.svelte");
      else if (appName === "WebSSH") import("$apps/WebSSH.svelte");
      else if (appName === "Winbox") import("$apps/Winbox.svelte");
      else if (appName === "Topology") import("$apps/Topology.svelte");
      else if (appName === "Account") import("$apps/Account.svelte");
      else if (appName === "Copilot") import("$apps/Copilot.svelte");
      else if (appName === "Admin") import("$apps/Admin/Users.svelte");
      else if (appName === "WebBrowser") import("$apps/WebBrowser.svelte");
    } catch (e) {
      // Ignore prefetch errors; click handler will catch them
    }

    // Prefetch store data so it's ready when the app opens
    try {
      if (appName === "Peers") peerStore.listPeers().catch(() => {});
      else if (appName === "Account") accountStore.getAccount().catch(() => {});
      else if (appName === "Topology") topologyStore.loadTopology().catch(() => {});
    } catch (e) {
      // Ignore store prefetch errors
    }
  };

  // ═══════════════════════════════════════════════════════════
  // MOBILE LAUNCHER — icon order, drag-reorder, animations
  // ═══════════════════════════════════════════════════════════

  const ICON_ORDER_KEY = "wantastic_iconOrder";
  let iconOrder: string[] = [...dskApps];

  function loadIconOrder() {
    try {
      const saved = localStorage.getItem(ICON_ORDER_KEY);
      if (saved) {
        const parsed: string[] = JSON.parse(saved);
        // Dedupe + restrict to known apps. Previously bad reorder logic
        // could write the same app twice to localStorage; the loader has
        // to heal that on every boot.
        const seen = new Set<string>();
        const inSaved = parsed.filter((a) => {
          if (!dskApps.includes(a) || seen.has(a)) return false;
          seen.add(a);
          return true;
        });
        const newApps = dskApps.filter((a) => !seen.has(a));
        iconOrder = [...inSaved, ...newApps];
        // Re-save the cleaned order so we don't keep healing on every boot.
        if (iconOrder.length !== parsed.length) {
          saveIconOrder();
        }
      }
    } catch (_) {
      iconOrder = [...dskApps];
    }
  }

  function saveIconOrder() {
    localStorage.setItem(ICON_ORDER_KEY, JSON.stringify(iconOrder));
  }

  // ── Edit / drag state ──────────────────────────────────
  let isEditMode = false;
  let isDragging = false;
  let draggingIdx = -1;
  let dragOverIdx = -1;
  let hasMoved = false;
  let dragStartX = 0;
  let dragStartY = 0;
  let longPressTimer: ReturnType<typeof setTimeout> | null = null;

  // Launch animation state
  let launchingApp: string | null = null;

  // Tap debounce — prevents SPA freeze from rapid clicking
  const tapCooldowns = new Map<string, number>();
  const TAP_COOLDOWN_MS = 350;

  function startLongPress(idx: number) {
    clearLongPress();
    longPressTimer = setTimeout(() => {
      isEditMode = true;
      draggingIdx = idx;
      try { navigator.vibrate(50); } catch (_) {}
      longPressTimer = null;
    }, 600);
  }

  function clearLongPress() {
    if (longPressTimer !== null) {
      clearTimeout(longPressTimer);
      longPressTimer = null;
    }
  }

  function exitEditMode() {
    isEditMode = false;
    isDragging = false;
    draggingIdx = -1;
    dragOverIdx = -1;
  }

  function reorderIcons(from: number, to: number) {
    if (from === to || to < 0) return;
    const arr = [...iconOrder];
    const [item] = arr.splice(from, 1);
    arr.splice(to, 0, item);
    iconOrder = arr;
    saveIconOrder();
    try { navigator.vibrate(30); } catch (_) {}
  }

  // Find the closest icon index to screen point (x, y), ignoring source
  function findClosestIcon(x: number, y: number): number {
    const els = document.querySelectorAll<HTMLElement>(".mobileApp");
    let closest = draggingIdx;
    let minDist = Infinity;
    els.forEach((el, i) => {
      if (i === draggingIdx) return;
      const r = el.getBoundingClientRect();
      const cx = r.left + r.width / 2;
      const cy = r.top + r.height / 2;
      const d = (x - cx) ** 2 + (y - cy) ** 2;
      if (d < minDist) { minDist = d; closest = i; }
    });
    // Only accept if within ~80 px radius
    return minDist < 6400 ? closest : draggingIdx;
  }

  // ── Pointer event handlers ─────────────────────────────
  function onIconPointerDown(event: PointerEvent, idx: number) {
    (event.currentTarget as HTMLElement).setPointerCapture(event.pointerId);
    dragStartX = event.clientX;
    dragStartY = event.clientY;
    hasMoved = false;
    if (isEditMode) {
      isDragging = true;
      draggingIdx = idx;
      dragOverIdx = idx;
    } else {
      startLongPress(idx);
    }
  }

  function onIconPointerMove(event: PointerEvent) {
    const dx = event.clientX - dragStartX;
    const dy = event.clientY - dragStartY;
    if (dx * dx + dy * dy > 64) { // 8 px threshold
      hasMoved = true;
      clearLongPress();
    }
    if (!isDragging || !isEditMode) return;
    event.preventDefault();
    dragOverIdx = findClosestIcon(event.clientX, event.clientY);
  }

  function onIconPointerUp(event: PointerEvent) {
    clearLongPress();
    if (isEditMode && isDragging) {
      reorderIcons(draggingIdx, dragOverIdx);
      isDragging = false;
      draggingIdx = -1;
      dragOverIdx = -1;
    }
  }

  function onIconPointerCancel() {
    clearLongPress();
    isDragging = false;
    draggingIdx = -1;
    dragOverIdx = -1;
  }

  function onGridClick(event: MouseEvent) {
    if (!isEditMode) return;
    const target = event.target as HTMLElement;
    if (!target.closest(".mobileApp")) exitEditMode();
  }

  // ── Tap handler with debounce ──────────────────────────
  function onIconClick(app: string) {
    // Ignore if this was a drag gesture
    if (hasMoved) return;
    // Exit edit mode on tap
    if (isEditMode) {
      exitEditMode();
      return;
    }
    // Debounce: prevent SPA freeze from rapid clicks
    const now = Date.now();
    if (now - (tapCooldowns.get(app) ?? 0) < TAP_COOLDOWN_MS) return;
    // Block new launches while one is animating
    if (launchingApp !== null) return;
    tapCooldowns.set(app, now);
    // Trigger launch animation
    launchingApp = app;
    setTimeout(() => { launchingApp = null; }, 450);
    openApp(app);
  }
</script>

<TaskSwitcher />
<Toast />
<DeviceAuthNotification />
<svelte:window on:resize={handleWindowResize} />

<!-- svelte-ignore a11y-no-static-element-interactions a11y-click-events-have-key-events -->
<div
  class="desktop"
  class:mobile={$isMobile}
  style:background={$desktopBackground}
  on:contextmenu={handleDesktopContextMenu}
  on:click={handleDesktopClick}
  bind:this={desktopElement}
>
  {#if !$isMobile}
    <div class="dskAppGrid">
      {#each dskApps as app}
        <button
          class="dskApp"
          class:selected={selectedApp === app}
          on:drag={() => selectApp(app)}
          on:click={() => openApp(app)}
          on:mouseenter={() => preloadApp(app)}
        >
          <div class="icon-wrapper" class:animate={selectedApp === app}>
            <img
              src="img/icon/{app}.svg"
              alt={getDisplayName(app)}
              height="48"
              width="48"
            />
          </div>
          {getDisplayName(app)}
        </button>
      {/each}
    </div>
  {:else}
    <!-- Mobile App Grid — Android launcher style -->
    <!-- svelte-ignore a11y-no-static-element-interactions a11y-click-events-have-key-events -->
    <div
      class="mobileAppGrid"
      class:edit-mode={isEditMode}
      on:click={onGridClick}
    >
      {#each iconOrder as app, idx (app)}
        {@const isSrc = isDragging && draggingIdx === idx}
        {@const isDst = isEditMode && dragOverIdx === idx && draggingIdx !== idx}
        <button
          class="mobileApp"
          class:wiggling={isEditMode && !isSrc}
          class:drag-src={isSrc}
          class:drag-dst={isDst}
          class:launching={launchingApp === app}
          style="--i:{idx}"
          on:pointerdown={(e) => onIconPointerDown(e, idx)}
          on:pointermove={onIconPointerMove}
          on:pointerup={onIconPointerUp}
          on:pointercancel={onIconPointerCancel}
          on:click={() => onIconClick(app)}
          on:mouseenter={() => preloadApp(app)}
        >
          <div class="app-icon-wrapper">
            <img
              src="img/icon/{app}.svg"
              alt={getDisplayName(app)}
              height="52"
              width="52"
              draggable="false"
            />
          </div>
          <span class="app-label">{getDisplayName(app)}</span>
          {#if isEditMode && !isSrc}
            <span class="drag-hint" aria-hidden="true">⠿</span>
          {/if}
        </button>
      {/each}
    </div>
  {/if}

  <WidgetSurface mobile={$isMobile} />

  <div class="apps">
    {#each $openedApps as e}
      <div
        class="app-wrapper"
        class:mobile-hidden={$isMobile && $activeThing !== e}
        class:minimized={!$isMobile && $minimizedApps.includes(e)}
      >
        {#if e === "Peers"}
          {#await import("$apps/Peers.svelte") then { default: Peers }}
            <Peers />
          {:catch error}
            {handleAppError("Peers", error)}
          {/await}
        {/if}
        {#if e === "AddPeer"}
          {#await import("$apps/AddPeer.svelte") then { default: AddPeer }}
            <AddPeer />
          {:catch error}
            {handleAppError("AddPeer", error)}
          {/await}
        {/if}
        {#if e === "PeerConfig"}
          {#await import("$apps/PeerConfig.svelte") then { default: PeerConfig }}
            <PeerConfig />
          {:catch error}
            {handleAppError("PeerConfig", error)}
          {/await}
        {/if}
        {#if e === "PeerNotes"}
          {#await import("$apps/PeerNotes.svelte") then { default: PeerNotes }}
            <PeerNotes
              peerId={$peerStore.selectedPeer?.id}
              peer={$peerStore.selectedPeer}
            />
          {:catch error}
            {handleAppError("PeerNotes", error)}
          {/await}
        {/if}
        {#if e === "MigrationForm"}
          {#await import("$apps/MigrationForm.svelte") then { default: MigrationForm }}
            <MigrationForm />
          {:catch error}
            {handleAppError("MigrationForm", error)}
          {/await}
        {/if}
        {#if e === "WebSSH"}
          {#await import("$apps/WebSSH.svelte") then { default: WebSSH }}
            <WebSSH />
          {:catch error}
            {handleAppError("WebSSH", error)}
          {/await}
        {/if}
        {#if e === "NewSSHSession"}
          {#await import("$apps/NewSSHSession.svelte") then { default: NewSSHSession }}
            <NewSSHSession />
          {:catch error}
            {handleAppError("NewSSHSession", error)}
          {/await}
        {/if}
        {#if e === "Winbox"}
          {#await import("$apps/Winbox.svelte") then { default: Winbox }}
            <Winbox />
          {:catch error}
            {handleAppError("Winbox", error)}
          {/await}
        {/if}
        {#if e === "WinboxAccounts"}
          {#await import("$apps/Winbox.svelte") then { default: WinboxAccounts }}
            <WinboxAccounts />
          {:catch error}
            {handleAppError("WinboxAccounts", error)}
          {/await}
        {/if}
        {#if e === "NewWinboxSession"}
          {#await import("$apps/NewWinboxSession.svelte") then { default: NewWinboxSession }}
            <NewWinboxSession />
          {:catch error}
            {handleAppError("NewWinboxSession", error)}
          {/await}
        {/if}
        {#if e === "SessionActivityViewer"}
          {#await import("$apps/SessionActivityViewer.svelte") then { default: SessionActivityViewer }}
            <SessionActivityViewer />
          {:catch error}
            {handleAppError("SessionActivityViewer", error)}
          {/await}
        {/if}
        {#if e === "SSHActivityViewer"}
          {#await import("$apps/SessionActivityViewer.svelte") then { default: SessionActivityViewer }}
            <SessionActivityViewer
              appId="SSHActivityViewer"
              activityType="SSH"
            />
          {:catch error}
            {handleAppError("SSHActivityViewer", error)}
          {/await}
        {/if}
        {#if e === "WinboxActivityViewer"}
          {#await import("$apps/SessionActivityViewer.svelte") then { default: SessionActivityViewer }}
            <SessionActivityViewer
              appId="WinboxActivityViewer"
              activityType="Winbox"
            />
          {:catch error}
            {handleAppError("WinboxActivityViewer", error)}
          {/await}
        {/if}
        {#if e === "Topology"}
          {#await import("$apps/Topology.svelte") then { default: Topology }}
            <Topology />
          {:catch error}
            {handleAppError("Topology", error)}
          {/await}
        {/if}
        {#if e === "CreateGroupLink"}
          {#await import("$apps/CreateGroupLink.svelte") then { default: CreateGroupLink }}
            <CreateGroupLink />
          {:catch error}
            {handleAppError("CreateGroupLink", error)}
          {/await}
        {/if}
        {#if e === "AssignExitNode"}
          {#await import("$apps/AssignExitNode.svelte") then { default: AssignExitNode }}
            <AssignExitNode />
          {:catch error}
            {handleAppError("AssignExitNode", error)}
          {/await}
        {/if}
        {#if e === "Copilot"}
          {#await import("$apps/Copilot.svelte") then { default: Copilot }}
            <Copilot />
          {:catch error}
            {handleAppError("Copilot", error)}
          {/await}
        {/if}
        {#if e === "Admin"}
          {#await import("$apps/Admin/Users.svelte") then { default: Admin }}
            <Admin />
          {:catch error}
            {handleAppError("Admin", error)}
          {/await}
        {/if}
        {#if e === "Account"}
          {#await import("$apps/Account.svelte") then { default: Account }}
            <Account />
          {:catch error}
            {handleAppError("Account", error)}
          {/await}
        {/if}
        {#if e === "OnboardingGuide"}
          {#await import("$apps/OnboardingGuide.svelte") then { default: OnboardingGuide }}
            <OnboardingGuide />
          {:catch error}
            {handleAppError("OnboardingGuide", error)}
          {/await}
        {/if}
        {#if e === "Snapshots"}
          {#await import("$apps/Snapshots.svelte") then { default: Snapshots }}
            <Snapshots />
          {:catch error}
            {handleAppError("Snapshots", error)}
          {/await}
        {/if}
        {#if e === "WuspDashboard"}
          {#await import("$apps/WuspDashboard.svelte") then { default: WuspDashboard }}
            <WuspDashboard />
          {:catch error}
            {handleAppError("WuspDashboard", error)}
          {/await}
        {/if}
        {#if e === "RouterOSDashboard"}
          {#await import("$apps/RouterOSDashboard.svelte") then { default: RouterOSDashboard }}
            <RouterOSDashboard />
          {:catch error}
            {handleAppError("RouterOSDashboard", error)}
          {/await}
        {/if}
      </div>
    {/each}

    <!-- SSH Terminal windows -->
    {#each openTerminals as termSession (termSession.id)}
      <div
        class="app-wrapper"
        class:mobile-hidden={$isMobile &&
          $activeThing !== `SSHTerminal-${termSession.id}`}
      >
        {#await import("$apps/SSHTerminal.svelte") then { default: SSHTerminal }}
          <SSHTerminal session={termSession} />
        {:catch error}
          {handleAppError(`SSHTerminal-${termSession.id}`, error)}
        {/await}
      </div>
    {/each}

    <!-- Web Browser proxy windows -->
    {#each openBrowsers as browserTab (browserTab.id)}
      <div
        class="app-wrapper"
        class:mobile-hidden={$isMobile && $activeThing !== "WebBrowser"}
      >
        {#await import("$apps/WebBrowser.svelte") then { default: WebBrowser }}
          <WebBrowser tabId={browserTab.id} />
        {:catch error}
          {handleAppError("WebBrowser", error)}
        {/await}
      </div>
    {/each}
  </div>
  {#if $activeThing === "Start"}
    {#await import("$components/Start.svelte") then { default: Start }}
      <Start />
    {/await}
  {/if}
</div>

<div
  class="brightoverlay"
  style:background="rgb(0 0 0 / {100 - $brightness}%)"
/>

<!-- Desktop Background Context Menu -->
{#if showBackgroundMenu}
  <!-- svelte-ignore a11y-click-events-have-key-events -->
  <!-- svelte-ignore a11y-no-static-element-interactions -->
  <div class="context-menu-overlay" on:click={hideContextMenu}>
    <!-- svelte-ignore a11y-click-events-have-key-events -->
    <!-- svelte-ignore a11y-no-static-element-interactions -->
    <div
      class="context-menu"
      style={`left: ${contextMenuX}px; top: ${contextMenuY}px; max-height: ${contextMenuMaxHeight}px;`}
      on:click|stopPropagation
      bind:this={contextMenuElement}
    >
      <div class="context-menu-header">
        <svg
          xmlns="http://www.w3.org/2000/svg"
          width="16"
          height="16"
          viewBox="0 0 24 24"
          fill="none"
          stroke="currentColor"
          stroke-width="2"
        >
          <rect x="3" y="3" width="18" height="18" rx="2" />
          <circle cx="12" cy="12" r="3" />
        </svg>
        <span>{$_("desktop.background")}</span>
      </div>
      <div class="context-menu-divider" />

      <div class="background-grid">
        {#each $desktopBackgroundColors as bg}
          <button
            class="background-option"
            class:active={$desktopBackground === bg.color}
            title={bg.name}
            on:click={() => setBackgroundColor(bg.color)}
          >
            <span
              class="background-preview"
              style:background={bg.preview || bg.color}
            />
            <span class="background-name">{bg.name}</span>
          </button>
        {/each}
      </div>

      <div class="context-menu-divider" />
      <div class="context-menu-header">
        <svg
          xmlns="http://www.w3.org/2000/svg"
          width="16"
          height="16"
          viewBox="0 0 24 24"
          fill="none"
          stroke="currentColor"
          stroke-width="2"
        >
          <rect x="3" y="3" width="7" height="7" rx="1.5" />
          <rect x="14" y="3" width="7" height="7" rx="1.5" />
          <rect x="3" y="14" width="7" height="7" rx="1.5" />
          <rect x="14" y="14" width="7" height="7" rx="1.5" />
        </svg>
        <span>{$_("widgets.desktopWidgets")}</span>
      </div>

      <button class="context-menu-item" on:click={toggleWidgetEditModeFromMenu}>
        {#if $widgetStore.editMode}
          {$_("widgets.done")}
        {:else}
          {$_("widgets.customize")}
        {/if}
      </button>

      <button class="context-menu-item" on:click={refreshWidgetsFromMenu}>
        {$_("common.refresh")}
      </button>

      <button class="context-menu-item" on:click={resetWidgetsFromMenu}>
        {$_("widgets.resetLayout")}
      </button>
    </div>
  </div>
{/if}

<style>
  .desktop {
    width: 100vw;
    height: calc(100vh - 48px); /* 48px is taskbars height */
    position: relative;
    overflow: hidden;
  }

  .dskAppGrid {
    position: absolute;
    inset: 0;
    display: grid;
    grid-template-columns: repeat(auto-fill, 74px);
    grid-template-rows: repeat(auto-fill, 70px);
    grid-auto-flow: column;
    padding-top: 6px;
    gap: 28px 1px;
  }
  .dskApp {
    background: unset;
    border: 1px solid transparent;
    height: min-content;
    min-height: 70px;
    display: flex;
    flex-direction: column;
    align-items: center;
    justify-content: center;
    font-size: 12px;
    text-align: center;
    border-radius: 2px;
    color: white;
    text-shadow:
      0 0 1px black,
      0 0 2px black,
      0 0 3px black,
      0 0 4px black,
      0 1px 1px black,
      0 1px 2px black;
    -webkit-user-drag: element;
  }
  .dskApp:focus,
  .dskApp:focus-visible {
    background: rgb(255 255 255 / 24%);
    outline: none;
  }
  .dskApp:hover {
    background: rgb(255 255 255 / 12%);
  }
  .dskApp:focus,
  .dskApp:focus-visible {
    border: 1px dotted;
  }
  .dskApp img {
    margin-bottom: 4px;
  }

  /* Icon wrapper for animation */
  .dskApp .icon-wrapper {
    display: flex;
    align-items: center;
    justify-content: center;
  }

  /* Animated bounce effect when selected */
  .dskApp .icon-wrapper.animate {
    animation: iconBounce 0.6s ease-in;
  }

  .dskApp.selected {
    background: rgb(255 255 255 / 24%);
    border: 1px solid rgb(255 255 255 / 40%);
  }

  @keyframes iconBounce {
    0%,
    100% {
      transform: translateY(0) scale(1);
    }
    25% {
      transform: translateY(-4px) scale(1.05);
    }
    50% {
      transform: translateY(0) scale(1);
    }
    75% {
      transform: translateY(-2px) scale(1.02);
    }
  }

  /* ── Mobile App Grid (Android launcher) ──────────────── */
  .mobileAppGrid {
    display: grid;
    grid-template-columns: repeat(3, 1fr);
    gap: 8px 4px;
    padding: 28px 12px 16px;
    max-width: 420px;
    margin: 0 auto;
    transition: background 0.25s;
  }

  .mobileAppGrid.edit-mode {
    background: rgba(0, 0, 0, 0.18);
    border-radius: 20px;
  }

  .mobileApp {
    display: flex;
    flex-direction: column;
    align-items: center;
    gap: 7px;
    padding: 10px 6px;
    background: transparent;
    border: none;
    border-radius: 14px;
    cursor: pointer;
    transition:
      transform 0.12s,
      opacity 0.12s,
      background 0.15s;
    -webkit-tap-highlight-color: transparent;
    touch-action: manipulation;
    position: relative;
    user-select: none;
  }

  /* touch-action: none only during edit mode to allow drag */
  .mobileAppGrid.edit-mode .mobileApp {
    touch-action: none;
  }

  .app-icon-wrapper {
    width: 64px;
    height: 64px;
    border-radius: 18px;
    background: rgba(255, 255, 255, 0.08);
    display: flex;
    align-items: center;
    justify-content: center;
    backdrop-filter: blur(12px);
    -webkit-backdrop-filter: blur(12px);
    border: 1px solid rgba(255, 255, 255, 0.12);
    transition: transform 0.15s cubic-bezier(0.34, 1.56, 0.64, 1);
    overflow: hidden;
  }

  .app-icon-wrapper img {
    width: 44px;
    height: 44px;
    object-fit: contain;
    pointer-events: none;
  }

  .app-label {
    font-size: 11.5px;
    font-weight: 500;
    color: white;
    text-shadow:
      0 1px 3px rgba(0, 0, 0, 0.9),
      0 0 6px rgba(0, 0, 0, 0.6);
    text-align: center;
    line-height: 1.2;
    max-width: 72px;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }

  /* ── Launch animation ─────────────────────────────────── */
  @keyframes iconLaunch {
    0%   { transform: scale(1); }
    30%  { transform: scale(0.82); }
    65%  { transform: scale(1.12); }
    100% { transform: scale(1); }
  }

  .mobileApp.launching .app-icon-wrapper {
    animation: iconLaunch 0.38s cubic-bezier(0.34, 1.56, 0.64, 1);
  }

  /* ── Wiggle (edit mode) ───────────────────────────────── */
  @keyframes wiggle {
    0%,  100% { transform: rotate(0deg)   scale(1);    }
    20%        { transform: rotate(-2.5deg) scale(1.03); }
    60%        { transform: rotate(2.5deg)  scale(1.03); }
  }

  .mobileApp.wiggling {
    animation: wiggle 0.5s ease-in-out infinite;
    animation-delay: calc(var(--i, 0) * 45ms);
  }

  /* Drag grip hint shown in wiggle mode */
  .drag-hint {
    position: absolute;
    top: 2px;
    right: 8px;
    font-size: 13px;
    opacity: 0.55;
    color: white;
    line-height: 1;
    pointer-events: none;
  }

  /* ── Drag source & drop target ───────────────────────── */
  .mobileApp.drag-src {
    opacity: 0.28;
    transform: scale(0.86);
  }

  .mobileApp.drag-dst .app-icon-wrapper {
    transform: scale(1.16);
    background: rgba(255, 255, 255, 0.18);
    border-color: rgba(255, 255, 255, 0.35);
  }

  /* App wrapper for mobile visibility control */
  .app-wrapper {
    display: contents; /* On desktop, wrapper is invisible */
  }

  .apps {
    position: relative;
    z-index: 4;
  }

  .app-wrapper.mobile-hidden {
    display: none;
  }

  .app-wrapper.minimized {
    display: none !important;
  }

  /* Mobile: Apps container takes full screen only when app is active */
  .desktop.mobile .apps {
    position: absolute;
    inset: 0;
    z-index: 100;
    pointer-events: none; /* Allow clicks through when empty */
  }

  /* When apps are open, allow pointer events on them */
  .desktop.mobile .apps > :global(*) {
    pointer-events: auto;
  }

  .brightoverlay {
    position: fixed;
    inset: 0;
    pointer-events: none;
    z-index: 99999;
  }

  /* Context Menu Styles */
  .context-menu-overlay {
    position: fixed;
    inset: 0;
    z-index: 99998;
  }

  .context-menu {
    position: fixed;
    min-width: min(320px, calc(100vw - 24px));
    width: min(640px, calc(100vw - 24px));
    background: linear-gradient(
      135deg,
      rgba(40, 40, 45, 0.9) 0%,
      rgba(30, 30, 35, 0.95) 100%
    );
    border: 1px solid rgba(255, 255, 255, 0.1);
    border-top: 1px solid rgba(255, 255, 255, 0.15);
    border-radius: 12px;
    box-shadow:
      0 16px 32px rgba(0, 0, 0, 0.5),
      0 4px 16px rgba(0, 0, 0, 0.2),
      inset 0 1px 0 rgba(255, 255, 255, 0.1);
    backdrop-filter: blur(24px);
    padding: 8px;
    animation: contextMenuIn 0.2s cubic-bezier(0.2, 0.8, 0.2, 1);
    overflow-x: hidden;
    overflow-y: auto;
    overscroll-behavior: contain;
    box-sizing: border-box;

    /* Vibrant glow effect */
    &::before {
      content: "";
      position: absolute;
      top: 0;
      left: 0;
      right: 0;
      height: 120px;
      background: linear-gradient(
        180deg,
        rgba(255, 255, 255, 0.05) 0%,
        transparent 100%
      );
      pointer-events: none;
    }
  }

  @keyframes contextMenuIn {
    from {
      opacity: 0;
      transform: scale(0.95);
    }
    to {
      opacity: 1;
      transform: scale(1);
    }
  }

  .context-menu-header {
    display: flex;
    align-items: center;
    gap: 8px;
    padding: 8px 10px;
    font-size: 13px;
    font-weight: 600;
    color: rgb(var(--clr, 255 255 255));
  }

  .context-menu-header svg {
    width: 16px;
    height: 16px;
    opacity: 0.7;
  }

  .context-menu-divider {
    height: 1px;
    background: rgb(var(--clr, 255 255 255) / 10%);
    margin: 4px 0;
  }

  .context-menu-item {
    display: flex;
    align-items: center;
    gap: 10px;
    width: 100%;
    padding: 8px 10px;
    border: none;
    background: transparent;
    border-radius: 4px;
    font-size: 13px;
    color: rgb(var(--clr, 255 255 255));
    cursor: pointer;
    transition: background 0.15s;
    text-align: left;
  }

  .context-menu-item:hover {
    background: rgb(var(--clr, 255 255 255) / 8%);
  }

  .background-grid {
    display: grid;
    grid-template-columns: repeat(2, minmax(0, 1fr));
    gap: 8px;
    padding: 8px 10px;
  }

  .background-option {
    display: grid;
    gap: 8px;
    padding: 8px;
    border-radius: 10px;
    border: 1px solid rgba(255, 255, 255, 0.08);
    background: rgba(255, 255, 255, 0.03);
    color: rgb(var(--clr, 255 255 255));
    text-align: left;
    cursor: pointer;
    transition:
      transform 0.15s,
      border-color 0.15s,
      background 0.15s;
  }

  .background-option:hover {
    transform: translateY(-1px);
    background: rgba(255, 255, 255, 0.06);
    border-color: rgba(255, 255, 255, 0.16);
  }

  .background-option.active {
    background: rgba(255, 255, 255, 0.08);
    border-color: rgba(255, 255, 255, 0.24);
    box-shadow:
      inset 0 0 0 1px rgba(255, 255, 255, 0.12),
      0 10px 22px rgba(0, 0, 0, 0.18);
  }

  .background-preview {
    display: block;
    width: 100%;
    height: 46px;
    border-radius: 10px;
    border: 1px solid rgba(255, 255, 255, 0.12);
    box-shadow:
      inset 0 1px 0 rgba(255, 255, 255, 0.14),
      0 10px 18px rgba(0, 0, 0, 0.18);
  }

  .background-name {
    font-size: 12px;
    font-weight: 600;
    line-height: 1.2;
  }
</style>
