<script lang="ts">
  import {
    activeThing,
    date,
    openedApps,
    minimizedApps,
    bringToFront,
  } from "$store/store";
  import { authStore } from "$store/auth";
  import { peerStore } from "$store/peer";
  import { accountStore } from "$store/account";
  import { topologyStore } from "$store/topology";
  import {
    isMobile,
    showTaskSwitcher,
    deferredPrompt,
    windowWidth,
    isStandalone,
  } from "$store/ui";
  import { _ } from "$store/i18n";
  import { IconButton, Button } from "fluent-svelte";

  const toggleActiveThing = (e: string) =>
    ($activeThing = $activeThing === e ? "" : e);

  // Define subapps that shouldn't appear in taskbar
  // Note: WebBrowser IS shown in taskbar since users need to restore minimized browser windows
  const subApps = [
    "SSHTerminal",
    "AddPeer",
    "PeerConfig",
    "Migration",
    "MigrationForm",
    "SessionActivityViewer",
    "SSHActivityViewer",
    "WinboxActivityViewer",
    "NewSSHSession",
    "NewWinboxSession",
    "CreateGroupLink",
    "WinboxAccounts",
    "OnboardingGuide",
    "PeerNotes",
    "WuspDashboard"
  ];

  // Display names for apps (when different from internal name)
  const appDisplayNames: Record<string, string> = {
    WinboxAccounts: $_("taskbar.winboxAccounts"),
    SSHActivityViewer: $_("taskbar.sshActivity"),
    WinboxActivityViewer: $_("taskbar.winboxActivity"),
    SessionActivityViewer: $_("taskbar.sessionActivity"),
    NewSSHSession: $_("taskbar.newSsh"),
    NewWinboxSession: $_("taskbar.newWinbox"),
    CreateGroupLink: $_("taskbar.createGroup"),
    PeerConfig: $_("taskbar.peerConfig"),
    AddPeer: $_("taskbar.addPeer"),
    WebBrowser: $_("taskbar.browser"),
    WuspDashboard: $_("taskbar.wuspDashboard"),
    RouterOSDashboard: "RouterOS",
  };

  // Icons for sub-apps that may not have SVG icons
  const appFallbackIcons: Record<string, string> = {
    SSHActivityViewer: "WebSSH",
    WinboxActivityViewer: "Winbox",
    SessionActivityViewer: "WebSSH",
    NewSSHSession: "WebSSH",
    NewWinboxSession: "Winbox",
    CreateGroupLink: "Topology",
    PeerConfig: "Peers",
    AddPeer: "Peers",
    Migration: "Peers",
    WebBrowser: "Peers",
    OnboardingGuide: "Peers",
    PeerNotes: "Peers",
    WuspDashboard: "Peers",
  };

  function getDisplayName(app: string): string {
    // Handle SSHTerminal-xxx format
    if (app.startsWith("SSHTerminal-")) {
      return $_("taskbar.terminal");
    }
    return appDisplayNames[app] || app;
  }

  function getIconPath(app: string): string {
    // Handle SSHTerminal-xxx format
    if (app.startsWith("SSHTerminal-")) {
      return "img/icon/WebSSH.svg";
    }
    const iconName = appFallbackIcons[app] || app;
    return `img/icon/${iconName}.svg`;
  }

  // Prefetch store data on taskbar icon hover (for apps not yet opened)
  let preloadedTaskbarApps = new Set<string>();
  const preloadTaskbarApp = (appName: string) => {
    if (preloadedTaskbarApps.has(appName) || $openedApps.includes(appName)) return;
    preloadedTaskbarApps.add(appName);
    try {
      if (appName === "Peers") peerStore.listPeers().catch(() => {});
      else if (appName === "Account") accountStore.getAccount().catch(() => {});
      else if (appName === "Topology") topologyStore.loadTopology().catch(() => {});
    } catch (e) {}
  };

  // Smarter app toggle: minimize if active, restore/open if not
  // Smarter app toggle: minimize if active, restore/open if not
  const toggleOpenApp = (app: string) => {
    if ($openedApps.includes(app)) {
      // App is already open
      if ($activeThing === app) {
        // App is active - minimize it
        $minimizedApps = [...$minimizedApps, app];
        $activeThing = "";
      } else {
        // App is minimized or background - restore it
        $minimizedApps = $minimizedApps.filter((a) => a !== app);
        $activeThing = app;
        bringToFront(app);
      }
    } else {
      // App is not open - open and activate it
      $minimizedApps = $minimizedApps.filter((a) => a !== app);
      $activeThing = app;
      $openedApps = [...$openedApps, app];
      bringToFront(app);
    }
  };

  function toggleTaskSwitcher() {
    $showTaskSwitcher = !$showTaskSwitcher;
  }

  const taskApps = [];

  // Filter opened apps to exclude subapps for taskbar display
  $: mainAppsInTaskbar = $openedApps.filter(
    (app) =>
      !subApps.includes(app) &&
      !app.startsWith("SSHTerminal-") &&
      !app.startsWith("WebBrowser-"),
  );

  // Mobile: Go home (close active app view and task switcher)
  function goHome() {
    $activeThing = "";
    $showTaskSwitcher = false;
  }

  // Context menu state
  let contextMenuVisible = false;
  let contextMenuApp = "";
  let contextMenuX = 0;
  let contextMenuY = 0;

  function showContextMenu(event: MouseEvent, app: string) {
    event.preventDefault();
    contextMenuApp = app;
    contextMenuX = event.clientX;
    contextMenuY = event.clientY;
    contextMenuVisible = true;
  }

  function hideContextMenu() {
    contextMenuVisible = false;
    contextMenuApp = "";
  }

  function closeApp(app: string) {
    $openedApps = $openedApps.filter((a) => a !== app);
    if ($activeThing === app) {
      $activeThing = "";
    }
    hideContextMenu();
  }

  // Close context menu when clicking elsewhere
  function handleWindowClick(event: MouseEvent) {
    if (contextMenuVisible) {
      hideContextMenu();
    }
  }

  // Mobile: Check if any app is currently active/visible
  $: hasActiveApp = $activeThing && $openedApps.includes($activeThing);

  // PWA Install Logic
  let showInstallInstructions = false;

  async function handleInstallClick() {
    if ($deferredPrompt) {
      // Show the install prompt
      $deferredPrompt.prompt();
      // Wait for the user to respond to the prompt
      const { outcome } = await $deferredPrompt.userChoice;
      console.log(`User response to the install prompt: ${outcome}`);
      // We've used the prompt, and can't use it again, throw it away
      deferredPrompt.set(null);
    } else {
      // Show manual instructions
      showInstallInstructions = true;
    }
  }

  function closeInstallInstructions() {
    showInstallInstructions = false;
  }
</script>

<div class="taskbar">
  {#if $isMobile}
    <!-- Mobile Navigation Bar -->
    <div class="mobile-nav">
      <!-- Home Button — always rendered; dimmed when already on home screen -->
      <button
        class="mobile-nav-btn home-btn"
        class:home-active={hasActiveApp}
        on:click={goHome}
        title={$_("taskbar.home")}
        aria-label={$_("taskbar.home")}
      >
        <svg
          xmlns="http://www.w3.org/2000/svg"
          width="22"
          height="22"
          viewBox="0 0 24 24"
          fill={hasActiveApp ? "currentColor" : "none"}
          stroke="currentColor"
          stroke-width="2"
          stroke-linecap="round"
          stroke-linejoin="round"
        >
          <path d="M3 9l9-7 9 7v11a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2z" />
          <polyline points="9 22 9 12 15 12 15 22" />
        </svg>
      </button>

      <!-- Active App Indicator (center pill) -->
      <div class="active-app-pill" class:visible={hasActiveApp}>
        {#if hasActiveApp}
          <img
            src={getIconPath($activeThing)}
            alt={getDisplayName($activeThing)}
            height="18"
            width="18"
          />
          <span>{getDisplayName($activeThing)}</span>
        {/if}
      </div>

      <!-- Task Switcher Button — always rendered -->
      <button
        class="mobile-nav-btn task-switcher-btn"
        on:click={toggleTaskSwitcher}
        title={$_("taskbar.openApps")}
        aria-label={$_("taskbar.openApps")}
      >
        <svg
          xmlns="http://www.w3.org/2000/svg"
          width="22"
          height="22"
          viewBox="0 0 24 24"
          fill="none"
          stroke="currentColor"
          stroke-width="2"
          stroke-linecap="round"
          stroke-linejoin="round"
        >
          <rect x="3" y="3" width="7" height="7" rx="1" />
          <rect x="14" y="3" width="7" height="7" rx="1" />
          <rect x="14" y="14" width="7" height="7" rx="1" />
          <rect x="3" y="14" width="7" height="7" rx="1" />
        </svg>
        {#if $openedApps.length > 0}
          <span class="badge">{$openedApps.length}</span>
        {/if}
      </button>
    </div>
  {:else}
    <!-- Desktop Navigation -->
    <div class="center">
      <IconButton
        class="taskIcon hvrBgLight"
        on:click={() => toggleActiveThing("Start")}
        on:keypress={() => toggleActiveThing("Start")}
      >
        <svg
          xmlns="http://www.w3.org/2000/svg"
          width="16"
          height="16"
          viewBox="0 0 24 24"
          fill="none"
          stroke="currentColor"
          stroke-width="2"
        >
          <rect x="3" y="3" width="7" height="7" rx="1" />
          <rect x="14" y="3" width="7" height="7" rx="1" />
          <rect x="14" y="14" width="7" height="7" rx="1" />
          <rect x="3" y="14" width="7" height="7" rx="1" />
        </svg>
      </IconButton>

      {#each taskApps as app}
        <div
          class="taskIcon hvrBgLight"
          class:openedApp={$openedApps.includes(app)}
          class:bgLight={app === $activeThing}
          class:activeApp={app === $activeThing}
          on:click={() => toggleOpenApp(app)}
          on:keypress={() => toggleOpenApp(app)}
          on:mouseenter={() => preloadTaskbarApp(app)}
        >
          <img src="img/icon/{app}.svg" alt={app} height="24" width="24" />
        </div>
      {/each}

      {#each mainAppsInTaskbar as app}
        {#if !taskApps.includes(app)}
          <div
            class="taskIcon hvrBgLight"
            class:openedApp={$openedApps.includes(app)}
            class:bgLight={app === $activeThing}
            class:activeApp={app === $activeThing}
            on:click={() => toggleOpenApp(app)}
            on:keypress={() => toggleOpenApp(app)}
            on:mouseenter={() => preloadTaskbarApp(app)}
            on:contextmenu={(e) => showContextMenu(e, app)}
            title={app === $activeThing && !$minimizedApps.includes(app)
              ? $_("taskbar.minimizeApp", {
                  values: { app: getDisplayName(app) },
                })
              : $_("taskbar.restoreApp", {
                  values: { app: getDisplayName(app) },
                })}
          >
            <img
              src="img/icon/{app}.svg"
              alt={getDisplayName(app)}
              height="24"
              width="24"
            />
          </div>
        {/if}
      {/each}
    </div>
  {/if}
</div>
{#if !$isMobile}
  <div class="right">
    {#if !$isStandalone}
      <div
        class="tier-badge install-btn hvrBgLight"
        on:click={handleInstallClick}
        on:keypress={handleInstallClick}
        title="Install App"
      >
        <svg
          xmlns="http://www.w3.org/2000/svg"
          width="16"
          height="16"
          viewBox="0 0 24 24"
          fill="none"
          stroke="currentColor"
          stroke-width="2"
          stroke-linecap="round"
          stroke-linejoin="round"
        >
          <path d="M21 15v4a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2v-4"></path>
          <polyline points="7 10 12 15 17 10"></polyline>
          <line x1="12" y1="15" x2="12" y2="3"></line>
        </svg>
        <span>Install</span>
      </div>
    {/if}

    <div
      class="date hvrBgLight"
      class:bgLight={$activeThing === "Calendar"}
      on:click={() => toggleActiveThing("Calendar")}
      on:keypress={() => toggleActiveThing("Calendar")}
    >
      <p>
        {$date.toLocaleTimeString([], { hour: "2-digit", minute: "2-digit" })}
      </p>
      <p>{$date.toLocaleDateString().replaceAll("/", "-")}</p>
    </div>
  </div>
{/if}

<!-- Context Menu -->
{#if contextMenuVisible}
  <!-- svelte-ignore a11y-click-events-have-key-events -->
  <!-- svelte-ignore a11y-no-static-element-interactions -->
  <div class="context-menu-overlay" on:click={hideContextMenu}>
    <!-- svelte-ignore a11y-click-events-have-key-events -->
    <!-- svelte-ignore a11y-no-static-element-interactions -->
    <div
      class="context-menu"
      style="left: {contextMenuX}px; bottom: 56px;"
      on:click|stopPropagation
    >
      <div class="context-menu-header">
        <img
          src="img/icon/{contextMenuApp}.svg"
          alt={contextMenuApp}
          height="16"
          width="16"
        />
        <span>{contextMenuApp}</span>
      </div>
      <div class="context-menu-divider" />
      <button
        class="context-menu-item"
        on:click={() => {
          toggleOpenApp(contextMenuApp);
          hideContextMenu();
        }}
      >
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
        </svg>
        {$activeThing === contextMenuApp &&
        !$minimizedApps.includes(contextMenuApp)
          ? $_("taskbar.minimize")
          : $_("taskbar.back")}
      </button>
      <button
        class="context-menu-item close-item"
        on:click={() => closeApp(contextMenuApp)}
      >
        <svg
          xmlns="http://www.w3.org/2000/svg"
          width="16"
          height="16"
          viewBox="0 0 24 24"
          fill="none"
          stroke="currentColor"
          stroke-width="2"
        >
          <line x1="18" y1="6" x2="6" y2="18" />
          <line x1="6" y1="6" x2="18" y2="18" />
        </svg>
        {$_("common.close")}
      </button>
    </div>
  </div>
{/if}

<!-- Install Instructions Modal -->
{#if showInstallInstructions}
  <!-- svelte-ignore a11y-click-events-have-key-events -->
  <!-- svelte-ignore a11y-no-static-element-interactions -->
  <div class="context-menu-overlay" on:click={closeInstallInstructions}>
    <div class="install-modal" on:click|stopPropagation>
      <h3>Install App</h3>
      <p>To install this app on your device:</p>
      <ul>
        <li>
          <strong>Chrome/Edge:</strong> Click the install icon in the address bar.
        </li>
        <li><strong>Safari (macOS):</strong> File > Add to Dock.</li>
        <li><strong>Safari (iOS):</strong> Share > Add to Home Screen.</li>
      </ul>
      <div class="modal-actions">
        <Button variant="accent" on:click={closeInstallInstructions}
          >Got it</Button
        >
      </div>
    </div>
  </div>
{/if}

<style>
  .taskbar {
    background: rgb(var(--bg2) / 90%);
    backdrop-filter: blur(40px) saturate(180%);
    -webkit-backdrop-filter: blur(40px) saturate(180%);
    position: absolute;
    bottom: 0;
    height: 48px;
    width: 100%;
    padding: 0 12px;
    z-index: 9000 !important;
    display: flex;
    justify-content: center;
    box-shadow:
      inset 0 1px rgb(255 255 255 / 8%),
      0 -1px 0 rgb(0 0 0 / 5%);
    border-top: 1px solid rgb(255 255 255 / 10%);
  }
  @media (prefers-color-scheme: dark) {
    .taskbar {
      background: rgb(var(--bg2) / 95%);
      box-shadow:
        inset 0 1px rgb(255 255 255 / 5%),
        0 -1px 0 rgb(0 0 0 / 10%);
      border-top: 1px solid rgb(255 255 255 / 5%);
    }
  }

  .center {
    width: auto;
    height: 100%;
    display: flex;
    align-items: center;
    justify-content: center;
    gap: 4px;
  }

  .taskIcon {
    display: flex;
    align-items: center;
    justify-content: center;
    height: 40px;
    width: 40px;
    border: 1px solid #808080;
    cursor: pointer;
    border-radius: 4px;
    position: relative;
  }
  .taskIcon img {
    transition: all 150ms;
  }
  .taskIcon:active img {
    transform: scale(75%);
  }

  .taskIcon::before {
    content: "";
    position: absolute;
    bottom: 0;
    height: 3px;
    width: 0;
    border-radius: 3px;
    transition: all 200ms;
  }
  .taskIcon.openedApp::before {
    width: 6px;
    background: gray;
  }
  .taskIcon.activeApp::before {
    width: 1rem;
    background: rgb(var(--clrPrm));
  }

  .widgetBtn {
    position: absolute;
    left: 10px;
  }

  .right {
    right: 12px;
    position: absolute;
    height: 100%;
    display: flex;
    align-items: center;
  }
  .actionCenterBtn,
  .date {
    display: flex;
    padding: 0 6px;
    border-radius: 4px;
    height: 40px;
  }
  .actionCenterBtn {
    align-items: center;
    gap: 4px;
  }
  .date {
    flex-direction: column;
    align-items: flex-end;
    justify-content: center;
    font-size: 12px;
  }

  .tier-badge {
    padding: 2px 8px;
    border-radius: 3px;
    font-size: 11px;
    font-weight: bold;
    margin-right: 10px;
    display: flex;
    align-items: center;
  }

  .shared-account-badge {
    display: flex;
    align-items: center;
    gap: 6px;
    padding: 4px 10px;
    border-radius: 4px;
    font-size: 12px;
    font-weight: 500;
    margin-right: 10px;
    background: linear-gradient(135deg, #10b981, #059669);
    color: white;
    border: none;
    cursor: pointer;
    transition: all 0.2s;
  }

  .shared-account-badge:hover {
    background: linear-gradient(135deg, #059669, #047857);
    box-shadow: 0 2px 8px rgba(16, 185, 129, 0.4);
  }

  /* ── Mobile nav bar ─────────────────────────────────── */
  .mobile-nav {
    width: 100%;
    display: grid;
    grid-template-columns: 48px 1fr 48px; /* stable 3-column layout */
    align-items: center;
    padding: 0 8px;
  }

  .mobile-nav-btn {
    position: relative;
    width: 44px;
    height: 44px;
    border-radius: 12px;
    background: transparent;
    border: none;
    cursor: pointer;
    display: flex;
    align-items: center;
    justify-content: center;
    transition:
      transform 0.15s,
      background 0.15s,
      opacity 0.2s;
    color: white;
    -webkit-tap-highlight-color: transparent;
    justify-self: center;
  }

  .mobile-nav-btn:active {
    transform: scale(0.88);
    background: rgba(255, 255, 255, 0.12);
  }

  /* Home button: dimmed when on home screen, bright when app is active */
  .home-btn {
    opacity: 0.35;
    color: white;
  }
  .home-btn.home-active {
    opacity: 1;
    color: rgb(var(--clrPrm, 99 102 241));
  }
  .home-btn.home-active:active {
    transform: scale(0.85);
  }

  /* Center pill showing active app */
  .active-app-pill {
    display: flex;
    align-items: center;
    justify-content: center;
    gap: 7px;
    font-size: 13px;
    font-weight: 600;
    color: white;
    opacity: 0;
    transform: translateY(4px);
    transition:
      opacity 0.2s,
      transform 0.2s;
    overflow: hidden;
    white-space: nowrap;
    text-overflow: ellipsis;
    padding: 0 4px;
  }
  .active-app-pill.visible {
    opacity: 1;
    transform: translateY(0);
  }
  .active-app-pill img {
    width: 18px;
    height: 18px;
    object-fit: contain;
    flex-shrink: 0;
  }

  /* Task switcher */
  .task-switcher-btn {
    color: white;
    opacity: 0.75;
  }
  .task-switcher-btn:active {
    opacity: 1;
  }
  .task-switcher-btn .badge {
    position: absolute;
    top: 5px;
    right: 5px;
    background: #ef4444;
    color: white;
    font-size: 10px;
    font-weight: 700;
    min-width: 16px;
    height: 16px;
    border-radius: 8px;
    display: flex;
    align-items: center;
    justify-content: center;
    padding: 0 3px;
    line-height: 1;
  }

  /* Context Menu Styles */
  .context-menu-overlay {
    position: fixed;
    inset: 0;
    z-index: 99999;
  }

  .context-menu {
    position: fixed;
    min-width: 200px;
    background: rgb(var(--bg1));
    border: 1px solid rgb(var(--clr) / 15%);
    border-radius: 8px;
    box-shadow: 0 8px 32px rgba(0, 0, 0, 0.25);
    backdrop-filter: blur(20px);
    padding: 6px;
    animation: contextMenuIn 0.15s ease-out;
  }

  @keyframes contextMenuIn {
    from {
      opacity: 0;
      transform: translateY(8px) scale(0.95);
    }
    to {
      opacity: 1;
      transform: translateY(0) scale(1);
    }
  }

  .context-menu-header {
    display: flex;
    align-items: center;
    gap: 8px;
    padding: 8px 10px;
    font-size: 13px;
    font-weight: 600;
    color: rgb(var(--clr));
  }

  .context-menu-header img {
    width: 16px;
    height: 16px;
    object-fit: contain;
  }

  .context-menu-divider {
    height: 1px;
    background: rgb(var(--clr) / 10%);
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
    color: rgb(var(--clr));
    cursor: pointer;
    transition: background 0.15s;
    text-align: left;
  }

  .context-menu-item:hover {
    background: rgb(var(--clr) / 8%);
  }

  .context-menu-item svg {
    width: 16px;
    height: 16px;
    opacity: 0.7;
  }

  .context-menu-item.close-item:hover {
    background: rgba(239, 68, 68, 0.15);
    color: #ef4444;
  }

  .context-menu-item.close-item:hover svg {
    stroke: #ef4444;
  }

  .install-btn {
    display: flex;
    align-items: center;
    gap: 6px;
    padding: 0 10px;
    margin-right: 4px;
    cursor: pointer;
    font-weight: 600;
    font-size: 13px;
    color: rgb(var(--clr));
    background: transparent;
    border: 1px solid rgb(var(--clr) / 15%);
  }

  .install-btn span {
    display: none;
  }

  @media (min-width: 600px) {
    .install-btn span {
      display: block;
    }
  }

  .install-modal {
    background: rgb(var(--bg1));
    padding: 24px;
    border-radius: 8px;
    width: 320px;
    position: fixed;
    top: 50%;
    left: 50%;
    transform: translate(-50%, -50%);
    box-shadow: 0 20px 50px rgba(0, 0, 0, 0.3);
    border: 1px solid rgb(var(--clr) / 10%);
    z-index: 10000;
  }

  .install-modal h3 {
    margin: 0 0 16px 0;
    font-size: 18px;
  }

  .install-modal ul {
    padding-left: 20px;
    margin-bottom: 24px;
    font-size: 14px;
    opacity: 0.9;
  }

  .install-modal li {
    margin-bottom: 8px;
  }

  .modal-actions {
    display: flex;
    justify-content: flex-end;
  }
</style>
