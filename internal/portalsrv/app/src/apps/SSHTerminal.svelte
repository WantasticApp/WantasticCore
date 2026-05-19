<script lang="ts">
  import { draggable } from "@neodrag/svelte";
  import { onMount, onDestroy, tick } from "svelte";
  import AppWindow from "$components/AppWindow.svelte";
  import Titlebar from "$components/shared/Titlebar.svelte";
  import { websshStore, type WebSSHSession } from "$store/webssh";
  import { wsStore } from "$store/websocket";
  import {
    activeThing,
    appZIndexes,
    bringToFront,
    openedApps,
  } from "$store/store";
  import { Terminal } from "@xterm/xterm";
  import { FitAddon } from "@xterm/addon-fit";
  import "@xterm/xterm/css/xterm.css";

  // Props - session data passed from parent
  export let session: WebSSHSession;

  function normalizePeerIp(peerIp: string): string {
    return (peerIp || "").trim().replace(/\/32$/, "");
  }

  function matchesSessionIdentity(left: WebSSHSession, right: WebSSHSession): boolean {
    return (
      (left.peer_id || "") === (right.peer_id || "") &&
      normalizePeerIp(left.peer_ip || "") === normalizePeerIp(right.peer_ip || "") &&
      (left.ssh_port || 22) === (right.ssh_port || 22) &&
      (left.username || "") === (right.username || "")
    );
  }

  function findFreshestSession(seed: WebSSHSession): WebSSHSession {
    const matches = ($websshStore.sessions || []).filter(
      (candidate) =>
        candidate.id === seed.id || matchesSessionIdentity(candidate, seed)
    );

    return (
      matches.sort((left, right) => {
        if (left.active !== right.active) {
          return left.active ? -1 : 1;
        }
        if (left.started_at !== right.started_at) {
          return right.started_at - left.started_at;
        }
        if (left.id === seed.id && right.id !== seed.id) {
          return -1;
        }
        if (right.id === seed.id && left.id !== seed.id) {
          return 1;
        }
        return 0;
      })[0] || seed
    );
  }

  let windowId = `SSHTerminal-${session.id}`;

  function adoptSession(nextSession: WebSSHSession) {
    if (!nextSession || nextSession.id === session.id) {
      return;
    }

    const previousWindowId = windowId;
    session = nextSession;
    windowId = `SSHTerminal-${session.id}`;

    openedApps.update((apps) =>
      apps.map((app) => (app === previousWindowId ? windowId : app))
    );

    if ($activeThing === previousWindowId) {
      $activeThing = windowId;
    }
  }

  function syncWithFreshestSession() {
    adoptSession(findFreshestSession(session));
  }

  // Window state
  let isMaximized = false;

  // Terminal instance
  let terminalContainer: HTMLDivElement;
  let terminal: Terminal | null = null;
  let fitAddon: FitAddon | null = null;
  let streamConnected = false;
  let connectionStatus: "connecting" | "connected" | "disconnected" | "error" =
    "connecting";
  let errorMessage = "";

  // Disposables for terminal input/resize listeners — must be disposed before re-registering on reconnect
  let inputDisposable: { dispose(): void } | null = null;
  let resizeDisposable: { dispose(): void } | null = null;
  let cleanupStarted = false;
  let disconnectPromise: Promise<void> | null = null;

  // Resize terminal when maximize state changes
  $: if (isMaximized !== undefined && fitAddon && terminalContainer) {
    setTimeout(() => {
      safeFit();
      terminal?.focus();
    }, 250);
  }

  // Watch for window being closed via Titlebar (removed from openedApps)
  $: if (!$openedApps.includes(windowId)) {
    // Window was closed via Titlebar, do cleanup
    startCleanup();
  }

  function ensureSessionEnded(): Promise<void> {
    if (disconnectPromise) {
      return disconnectPromise;
    }

    disconnectPromise = websshStore.disconnectSession(session.id).catch((err) => {
      console.error("Failed to disconnect:", err);
    });

    return disconnectPromise;
  }

  function startCleanup() {
    if (cleanupStarted) {
      return;
    }
    cleanupStarted = true;

    // Dispose terminal input/resize listeners first
    inputDisposable?.dispose();
    inputDisposable = null;
    resizeDisposable?.dispose();
    resizeDisposable = null;
    // Clean up gRPC stream and terminal
    wsStore.closeSSHStream(session.id);
    streamConnected = false;
    if (terminal) {
      terminal.dispose();
      terminal = null;
    }
    // Remove from openTerminals list in store (UI state only).
    // Do NOT call disconnectSession here — closing the window only hides the
    // terminal; the SSH session stays alive and visible in the sessions list.
    websshStore.closeTerminal(session.id);
  }

  // Close the terminal window and end the SSH session on the server
  function closeWindow() {
    $openedApps = $openedApps.filter((app) => app !== windowId);
  }

  // Terminate the session on server and close window
  async function terminateSession() {
    await ensureSessionEnded();
    closeWindow();
  }

  onMount(async () => {
    await tick();
    syncWithFreshestSession();
    initTerminal();
  });

  onDestroy(() => {
    startCleanup();
  });

  /** Fit terminal to container, reserving one row so the cursor line
   *  is never clipped by the status bar. Uses FitAddon.proposeDimensions()
   *  to get the calculated size, then resizes with rows - 1. */
  function safeFit() {
    if (!fitAddon || !terminal) return;
    const dims = fitAddon.proposeDimensions();
    if (dims && dims.rows > 1 && dims.cols > 0) {
      terminal.resize(dims.cols, dims.rows - 1);
    }
  }

  function initTerminal() {
    if (!terminalContainer) return;

    terminal = new Terminal({
      rows: session.terminal_rows || 24,
      cols: session.terminal_cols || 80,
      cursorBlink: true,
      theme: {
        background: "#0c0c0c",
        foreground: "#cccccc",
        cursor: "#ffffff",
        cursorAccent: "#0c0c0c",
        selectionBackground: "#264f78",
        selectionForeground: "#ffffff",
        black: "#0c0c0c",
        red: "#c50f1f",
        green: "#13a10e",
        yellow: "#c19c00",
        blue: "#0037da",
        magenta: "#881798",
        cyan: "#3a96dd",
        white: "#cccccc",
        brightBlack: "#767676",
        brightRed: "#e74856",
        brightGreen: "#16c60c",
        brightYellow: "#f9f1a5",
        brightBlue: "#3b78ff",
        brightMagenta: "#b4009e",
        brightCyan: "#61d6d6",
        brightWhite: "#f2f2f2",
      },
      fontFamily:
        '"SFMono-Regular", Menlo, Monaco, Consolas, "Liberation Mono", "DejaVu Sans Mono", monospace',
      fontSize: 14,
      fontWeight: "400",
      fontWeightBold: "600",
      letterSpacing: 0,
      lineHeight: 1,
      drawBoldTextInBrightColors: false,
      allowProposedApi: true,
    });

    fitAddon = new FitAddon();
    terminal.loadAddon(fitAddon);

    terminal.open(terminalContainer);
    safeFit();
    terminal.focus();

    // Connect via gRPC stream
    connectSSHStream();

    // Handle window resize — debounce to avoid rapid reflows
    const resizeObserver = new ResizeObserver(() => {
      safeFit();
    });
    resizeObserver.observe(terminalContainer);
  }

  function connectSSHStream() {
    syncWithFreshestSession();
    connectionStatus = "connecting";
    websshStore.addConnectionLog(
      `Connecting to ${session.username}@${session.peer_ip} via gRPC stream`,
      "info",
    );

    try {
      // Open gRPC stream with handlers
      const success = wsStore.openSSHStream(session.id, {
        onReady: () => {
          streamConnected = true;
          connectionStatus = "connected";
          websshStore.updateSessionStatus(session.id, "connected");
          websshStore.addConnectionLog(
            `Connected to ${session.username}@${session.peer_ip}`,
            "success",
          );

          // Dispose any previous listeners before re-registering to prevent double-input on reconnect
          inputDisposable?.dispose();
          resizeDisposable?.dispose();

          // Handle terminal input - send via gRPC stream
          inputDisposable =
            terminal?.onData((data) => {
              if (streamConnected) {
                wsStore.sendSSHData(session.id, data);
              }
            }) ?? null;

          // Handle terminal resize - send via gRPC stream
          resizeDisposable =
            terminal?.onResize(({ cols, rows }) => {
              if (streamConnected) {
                wsStore.sendSSHResize(session.id, rows, cols);
              }
            }) ?? null;

          fitAddon?.fit();
          if (terminal) {
            wsStore.sendSSHResize(session.id, terminal.rows, terminal.cols);
            terminal.focus();
          }
        },
        onData: (data: string | Uint8Array) => {
          // Write SSH output to terminal
          terminal?.write(data);
        },
        onError: (error: string) => {
          connectionStatus = "error";
          errorMessage = error;
          websshStore.addConnectionLog(`SSH error: ${error}`, "error");
          terminal?.write(`\r\n\x1b[31mError: ${error}\x1b[0m\r\n`);
        },
        onClose: () => {
          connectionStatus = "disconnected";
          streamConnected = false;
          websshStore.updateSessionStatus(session.id, "disconnected");
          websshStore.addConnectionLog("Connection closed", "warning");
          terminal?.write("\r\n\x1b[33mConnection closed\x1b[0m\r\n");
        },
      });

      if (success) {
        websshStore.updateSessionStatus(session.id, "connecting");
      } else {
        connectionStatus = "error";
        errorMessage = "Failed to open SSH stream";
        websshStore.addConnectionLog("Failed to open SSH stream", "error");
      }
    } catch (err: any) {
      connectionStatus = "error";
      errorMessage = err.message || "Failed to connect";
      websshStore.addConnectionLog(
        `Failed to connect: ${err.message}`,
        "error",
      );
    }
  }

  function handleReconnect() {
    syncWithFreshestSession();
    // Close existing stream
    wsStore.closeSSHStream(session.id);
    streamConnected = false;
    terminal?.clear();
    connectSSHStream();
  }
</script>

<AppWindow
  appName={windowId}
  bind:isMaximized
  canReduce={false}
  width="900px"
  height="600px"
>
  <div slot="header_icon" style="display: flex; align-items: center;">
    <svg
      xmlns="http://www.w3.org/2000/svg"
      width="16"
      height="16"
      viewBox="0 0 100 100"
    >
      <defs>
        <linearGradient id="sshTermGrad" x1="0%" y1="0%" x2="100%" y2="100%">
          <stop offset="0%" style="stop-color:#10b981;stop-opacity:1" />
          <stop offset="100%" style="stop-color:#059669;stop-opacity:1" />
        </linearGradient>
      </defs>
      <rect
        x="10"
        y="20"
        width="80"
        height="60"
        rx="4"
        fill="#1f2937"
        stroke="url(#sshTermGrad)"
        stroke-width="3"
      />
      <rect
        x="10"
        y="20"
        width="80"
        height="10"
        rx="4"
        fill="url(#sshTermGrad)"
      />
      <circle cx="18" cy="25" r="2" fill="#ef4444" />
      <circle cx="25" cy="25" r="2" fill="#fbbf24" />
      <circle cx="32" cy="25" r="2" fill="#10b981" />
      <text x="20" y="48" fill="#10b981" font-family="monospace" font-size="10"
        >$</text
      >
    </svg>
    <span class="appName pl-2">{session.username}@{session.peer_ip}</span>
  </div>

  <div class="window-body">
    <!-- Status bar -->
    <div class="terminal-toolbar">
      <div class="connection-info">
        <span
          class="status-dot"
          class:connected={connectionStatus === "connected"}
          class:connecting={connectionStatus === "connecting"}
          class:error={connectionStatus === "error"}
        />
        <span class="status-text"
          >{session.username}@{session.peer_ip}:{session.ssh_port}</span
        >
      </div>
      <div class="toolbar-actions">
        {#if connectionStatus === "disconnected" || connectionStatus === "error"}
          <button class="btn-secondary" on:click={handleReconnect}>
            Reconnect
          </button>
        {/if}
        <button class="btn-secondary" on:click={terminateSession}>
          Disconnect
        </button>
      </div>
    </div>

    <!-- Terminal container -->
    <div class="terminal-wrapper" bind:this={terminalContainer} />

    <!-- Status bar -->
    <div class="status-bar">
      <span class="status-item">
        {#if connectionStatus === "connecting"}
          Connecting...
        {:else if connectionStatus === "connected"}
          Connected
        {:else if connectionStatus === "error"}
          Error: {errorMessage}
        {:else}
          Disconnected
        {/if}
      </span>
      <span class="status-item"
        >{session.terminal_cols}×{session.terminal_rows}</span
      >
    </div>
  </div>
</AppWindow>

<style lang="scss">
  .window-body {
    display: flex;
    flex-direction: column;
    flex: 1;
    background: #0c0c0c;
    overflow: hidden;
    min-height: 0; /* allow flex children to shrink */
    height: 0; /* force flex-basis to 0 so flex:1 calculates from parent, not content */
  }

  .terminal-toolbar {
    display: flex;
    justify-content: space-between;
    align-items: center;
    padding: 8px 12px;
    background: #1f1f1f;
    border-bottom: 1px solid #333;
    flex-shrink: 0;
  }

  .connection-info {
    display: flex;
    align-items: center;
    gap: 8px;
  }

  .status-dot {
    width: 8px;
    height: 8px;
    border-radius: 50%;
    background: #6b7280;

    &.connected {
      background: #10b981;
      box-shadow: 0 0 6px #10b981;
    }

    &.connecting {
      background: #f59e0b;
      animation: pulse 1.5s ease-in-out infinite;
    }

    &.error {
      background: #ef4444;
    }
  }

  @keyframes pulse {
    0%,
    100% {
      opacity: 1;
    }
    50% {
      opacity: 0.5;
    }
  }

  .status-text {
    font-family: "SFMono-Regular", Menlo, Monaco, Consolas, "Liberation Mono", monospace;
    font-size: 12px;
    color: #9ca3af;
  }

  .toolbar-actions {
    display: flex;
    gap: 8px;
  }

  .btn-secondary {
    padding: 6px 12px;
    background: #2d2d2d;
    color: #e5e5e5;
    border: 1px solid #404040;
    border-radius: 4px;
    font-size: 12px;
    font-weight: 500;
    cursor: pointer;
    transition: all 0.15s ease;

    &:hover {
      background: #3d3d3d;
      border-color: #505050;
    }

    &:active {
      background: #252525;
    }
  }

  .terminal-wrapper {
    flex: 1;
    overflow: hidden;
    min-height: 0;
    position: relative;
  }

  /* Prevent xterm from expanding beyond its flex container.
     Without min-height:0, a flex child can grow past the parent. */
  .window-body {
    min-height: 0;
  }

  .status-bar {
    display: flex;
    justify-content: space-between;
    padding: 4px 12px;
    background: #1f1f1f;
    border-top: 1px solid #333;
    font-size: 11px;
    color: #6b7280;
    flex-shrink: 0;
  }

  .status-item {
    font-family: "Segoe UI Variable", "Segoe UI", sans-serif;
  }

  /* xterm overrides */
  .terminal-wrapper :global(.xterm) {
    height: 100% !important;
    max-height: 100% !important;
    padding: 0;
    overflow: hidden;
  }

  .terminal-wrapper :global(.xterm-viewport) {
    overflow-y: auto !important;
  }

  .terminal-wrapper :global(.xterm-screen) {
    height: 100%;
    width: 100%;
  }

  .terminal-wrapper :global(.xterm),
  .terminal-wrapper :global(.xterm *) {
    font-family: "SFMono-Regular", Menlo, Monaco, Consolas, "Liberation Mono", "DejaVu Sans Mono", monospace !important;
    font-kerning: none !important;
    font-variant-ligatures: none !important;
    font-feature-settings: "liga" 0, "calt" 0, "kern" 0 !important;
  }

  .terminal-wrapper :global(.xterm-rows),
  .terminal-wrapper :global(.xterm .xterm-rows span) {
    letter-spacing: 0 !important;
  }

  /* Mobile responsive styles */
  @media (max-width: 768px) {
    .ssh-terminal {
      position: fixed !important;
      min-width: 100%;
      min-height: 100%;
      left: 0 !important;
      top: 0 !important;
      right: 0 !important;
      bottom: 48px !important;
      width: 100vw !important;
      height: calc(100vh - 48px) !important;
      border-radius: 0;
      resize: none;
    }

    .toolbar {
      padding: 6px 10px;
      flex-wrap: wrap;
      gap: 8px;
    }

    .toolbar button {
      padding: 8px 12px;
      font-size: 13px;
      min-height: 36px;
    }

    .statusbar {
      padding: 4px 10px;
      font-size: 10px;
    }

    .status-item {
      font-size: 10px;
    }
  }

  @media (max-width: 480px) {
    .toolbar {
      padding: 4px 8px;
      gap: 6px;
    }

    .toolbar button {
      padding: 6px 10px;
      font-size: 12px;
      min-height: 32px;
    }

    .toolbar button svg {
      width: 14px;
      height: 14px;
    }

    .statusbar {
      padding: 3px 8px;
      font-size: 9px;
    }
  }

  /* Touch-friendly improvements */
  @media (hover: none) and (pointer: coarse) {
    .toolbar button {
      min-height: 44px;
      min-width: 44px;
    }
  }
</style>
