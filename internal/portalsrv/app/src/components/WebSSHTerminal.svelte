<script lang="ts">
  import { websshStore, websshApi, type WebSSHSession } from "../store/webssh";
  import { wsStore } from "../store/websocket";
  import { ApiError } from "../lib/errors";
  import { translateError$ } from "../store/i18n";
  import { onMount, onDestroy } from "svelte";
  import type { Peer } from "../store/peer";
  import { Terminal } from "@xterm/xterm";
  import { FitAddon } from "@xterm/addon-fit";
  import { WebLinksAddon } from "@xterm/addon-web-links";
  import "@xterm/xterm/css/xterm.css";

  export let peer: Peer;
  export let onClose: () => void;
  export let initialSessionId: string | null = null; // Optional: Resume existing session

  let terminalContainer: HTMLDivElement;
  let isConnecting = false;
  let isConnected = false;
  let error = "";
  let terminal: Terminal;
  let fitAddon: FitAddon;
  let sessionId: string | null = initialSessionId;
  let resizeObserver: ResizeObserver;
  let disconnectPromise: Promise<void> | null = null;

  // Terminal settings
  let rows = 24;
  let cols = 80;
  let username = "root";
  let target = peer.ip_address; // Default to peer IP
  let password = "";
  let privateKey = "";
  let privateKeyPassphrase = "";

  onMount(async () => {
    // Initialize xterm.js
    terminal = new Terminal({
      cursorBlink: true,
      allowTransparency: false,
      theme: {
        background: "#0d1117",
        foreground: "#c9d1d9",
        cursor: "#58a6ff",
        cursorAccent: "#0d1117",
        selectionBackground: "rgba(56, 139, 253, 0.4)",
        black:   "#484f58", red:     "#ff7b72", green:   "#3fb950",
        yellow:  "#d29922", blue:    "#58a6ff", magenta: "#bc8cff",
        cyan:    "#39c5cf", white:   "#b1bac4",
        brightBlack:   "#6e7681", brightRed:     "#ffa198",
        brightGreen:   "#56d364", brightYellow:  "#e3b341",
        brightBlue:    "#79c0ff", brightMagenta: "#d2a8ff",
        brightCyan:    "#56d4dd", brightWhite:   "#f0f6fc",
      },
      fontFamily:
        '"SFMono-Regular", Menlo, Monaco, Consolas, "Liberation Mono", "DejaVu Sans Mono", monospace',
      fontSize: 14,
      fontWeight: "400",
      fontWeightBold: "600",
      lineHeight: 1,
      letterSpacing: 0,
      scrollback: 5000,
      drawBoldTextInBrightColors: false,
      // Proper padding is set via xterm options, NOT CSS padding on the container
      // (CSS padding on the container breaks FitAddon size calculation)
    });

    fitAddon = new FitAddon();
    terminal.loadAddon(fitAddon);
    terminal.loadAddon(new WebLinksAddon());

    // IMPORTANT: open() MUST be called before loading renderer addons.
    // The DOM element must exist and be sized before WebGL initialises.
    terminal.open(terminalContainer);

    // Defer fit() to the next animation frame so the browser has finished
    // laying out the flex container before FitAddon measures its dimensions.
    // Calling fit() synchronously after open() can produce wrong cols/rows.
    requestAnimationFrame(() => {
      fitAddon.fit();
    });

    // Handle terminal input
    terminal.onData((data) => {
      if (sessionId && isConnected) {
        wsStore.sendSSHData(sessionId, data);
      }
    });

    // Handle terminal resize
    terminal.onResize((size) => {
      if (sessionId && isConnected) {
        rows = size.rows;
        cols = size.cols;
        wsStore.sendSSHResize(sessionId, rows, cols);
      }
    });

    // Observe container resize
    // Observe container resize with debounce
    let resizeTimeout: ReturnType<typeof setTimeout>;
    resizeObserver = new ResizeObserver(() => {
      clearTimeout(resizeTimeout);
      resizeTimeout = setTimeout(() => {
        fitAddon.fit();
      }, 50); // Debounce by 50ms
    });
    resizeObserver.observe(terminalContainer);

    // Initial focus
    terminal.focus();

    // Auto-connect if allowed
    connect();
  });

  onDestroy(() => {
    if (sessionId) {
      wsStore.closeSSHStream(sessionId);
      void endSession(sessionId);
    }
    if (resizeObserver) {
      resizeObserver.disconnect();
    }
    if (terminal) {
      terminal.dispose();
    }
  });

  async function connect() {
    if (isConnecting) return;
    if (privateKeyPassphrase && !privateKey.trim()) {
      error = "Private key is required when a key passphrase is set.";
      return;
    }
    isConnecting = true;
    error = "";
    terminal.clear();
    terminal.writeln("Connecting to SSH session...");

    try {
      // 1. Create or Reuse SSH session
      if (!sessionId) {
        // Create new session
        const session = await websshApi.createSession(
          peer.id,
          target,
          username,
          password || undefined,
          undefined,
          undefined,
          undefined,
          privateKey || undefined,
          privateKeyPassphrase || undefined,
        );
        sessionId = session.id;
      } else {
        terminal.writeln("Resuming session " + sessionId + "...");
      }

      // 2. Open SSH stream via WebSocket
      const success = wsStore.openSSHStream(sessionId, {
        onReady: () => {
          isConnected = true;
          isConnecting = false;
          fitAddon.fit();
          if (sessionId) {
            wsStore.sendSSHResize(sessionId, terminal.rows, terminal.cols);
          }
          terminal.focus();
        },
        onData: (data: string | Uint8Array) => {
          terminal.write(data);
        },
        onError: (errMsg: string) => {
          console.error("SSH Stream Error:", errMsg);
          error = errMsg;
          terminal.writeln(`\r\nError: ${errMsg}`);
          isConnecting = false;
        },
        onClose: () => {
          console.log("SSH Stream Closed");
          isConnected = false;
          isConnecting = false;
          terminal.writeln("\r\nConnection closed.");
        },
      });

      if (success) {
        terminal.writeln("Stream opened.\r\n");

        // Send initial resize to sync dimensions
        setTimeout(() => {
          fitAddon.fit();
          if (sessionId) {
            wsStore.sendSSHResize(sessionId, terminal.rows, terminal.cols);
          }
        }, 100);
      } else {
        throw new Error("Failed to open SSH stream (WebSocket disconnected?)");
      }
    } catch (err: any) {
      console.error("Connection failed:", err);
      error =
        err instanceof ApiError
          ? err.message
          : err.message || "Failed to connect";
      terminal.writeln(`\r\nConnection failed: ${error}`);
      isConnecting = false;
      sessionId = null;
    }
  }

  function endSession(activeSessionId: string): Promise<void> {
    if (disconnectPromise) {
      return disconnectPromise;
    }

    disconnectPromise = websshApi.disconnectSession(activeSessionId).catch((err) => {
      console.error("Failed to disconnect SSH session:", err);
    });

    return disconnectPromise;
  }

  function disconnect() {
    if (sessionId) {
      wsStore.closeSSHStream(sessionId);
      void endSession(sessionId);
    }
    isConnected = false;
    isConnecting = false;
    terminal.writeln("\r\nDisconnected.");
  }
</script>

<div
  class="webssh-modal-overlay"
  on:click={onClose}
  on:keydown={(e) => e.key === "Escape" && onClose()}
  role="presentation"
>
  <div
    class="webssh-modal-content"
    on:click|stopPropagation
    on:keydown={() => {}}
    role="dialog"
    aria-label="WebSSH terminal"
  >
    <div class="webssh-header">
      <div class="webssh-title">
        <div
          class="webssh-status"
          class:online={isConnected}
          title={isConnected ? "Connected" : "Disconnected"}
        ></div>
        <div>
          <h2>SSH Terminal</h2>
          <p class="webssh-peer">
            {peer.name} <span class="ip-batch">{peer.ip_address}</span>
          </p>
        </div>
      </div>
      <div class="webssh-controls">
        {#if !isConnected}
          <div class="control-group">
            <input
              type="text"
              bind:value={target}
              placeholder="Target IP"
              class="webssh-input"
            />
            <input
              type="text"
              bind:value={username}
              placeholder="User"
              class="webssh-input"
            />
            <input
              type="password"
              bind:value={password}
              placeholder="Ex: Pwd..."
              class="webssh-input"
            />
            <textarea
              bind:value={privateKey}
              placeholder="Private key (optional)"
              class="webssh-input webssh-key-input"
              rows="4"
              spellcheck="false"
            ></textarea>
            <input
              type="password"
              bind:value={privateKeyPassphrase}
              placeholder="Key passphrase..."
              class="webssh-input"
            />
            <button
              on:click={connect}
              disabled={isConnecting}
              class="webssh-button primary"
            >
              {isConnecting ? "Connecting..." : "Connect"}
            </button>
          </div>
        {:else}
          <div class="control-group">
            <button
              on:click={() => {
                terminal.selectAll();
                document.execCommand("copy");
                terminal.clearSelection();
              }}
              class="webssh-button icon-only"
              title="Copy All"
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
                ><rect width="14" height="14" x="8" y="8" rx="2" ry="2" /><path
                  d="M4 16c-1.1 0-2-.9-2-2V4c0-1.1.9-2 2-2h10c1.1 0 2 .9 2 2"
                /></svg
              >
            </button>
            <button
              on:click={() => terminal.clear()}
              class="webssh-button icon-only"
              title="Clear Terminal"
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
                ><path d="M3 6h18" /><path
                  d="M19 6v14c0 1-1 2-2 2H7c-1 0-2-1-2-2V6"
                /><path d="M8 6V4c0-1 1-2 2-2h4c1 0 2 1 2 2v2" /></svg
              >
            </button>
            <button on:click={disconnect} class="webssh-button"
              >Disconnect</button
            >
          </div>
        {/if}
        <button on:click={onClose} class="webssh-close" title="Close">✕</button>
      </div>
    </div>

    {#if error}
      <div class="webssh-error">
        <span>{$translateError$(error)}</span>
        <button on:click={() => (error = "")}>✕</button>
      </div>
    {/if}

    <div
      class="webssh-terminal-wrapper"
    >
      <!-- xterm mounts its canvas here — must be empty, no children -->
      <div
        class="webssh-terminal"
        bind:this={terminalContainer}
        role="textbox"
        aria-label="Terminal output"
        tabindex="0"
      ></div>

      <!-- Placeholder rendered OUTSIDE the terminal container -->
      {#if !isConnected}
        <div class="webssh-placeholder">
          <p>Not connected</p>
          <small>Click "Connect" to start SSH session</small>
        </div>
      {/if}
    </div>

    <div class="webssh-footer">
      <small>Terminal size: {cols}×{rows}</small>
    </div>
  </div>
</div>

<style>
  .webssh-modal-overlay {
    position: fixed;
    top: 0;
    left: 0;
    right: 0;
    bottom: 0;
    background: rgba(0, 0, 0, 0.6);
    backdrop-filter: blur(5px);
    display: flex;
    align-items: center;
    justify-content: center;
    z-index: 2000;
  }

  .webssh-modal-content {
    background: rgba(15, 23, 42, 0.95); /* Deep dark blue/black */
    border: 1px solid rgba(255, 255, 255, 0.1);
    border-radius: 12px;
    width: 90%;
    max-width: 1200px;
    height: 85vh;
    max-height: 800px;
    display: flex;
    flex-direction: column;
    box-shadow: 0 25px 50px -12px rgba(0, 0, 0, 0.5);
    backdrop-filter: blur(20px);
    overflow: hidden;
  }

  .webssh-header {
    padding: 16px 24px;
    border-bottom: 1px solid rgba(255, 255, 255, 0.05);
    display: flex;
    justify-content: space-between;
    align-items: center;
    gap: 16px;
    background: rgba(255, 255, 255, 0.02);
  }

  .webssh-title {
    display: flex;
    align-items: center;
    gap: 12px;
  }

  .webssh-status {
    width: 10px;
    height: 10px;
    border-radius: 50%;
    background-color: #ef4444;
    box-shadow: 0 0 10px rgba(239, 68, 68, 0.4);
    transition: all 0.3s ease;
  }

  .webssh-status.online {
    background-color: #10b981;
    box-shadow: 0 0 10px rgba(16, 185, 129, 0.4);
  }

  .webssh-title h2 {
    margin: 0;
    font-size: 16px;
    font-weight: 600;
    color: #f1f5f9;
    letter-spacing: 0.5px;
  }

  .webssh-peer {
    margin: 2px 0 0 0;
    font-size: 13px;
    color: #94a3b8;
    display: flex;
    align-items: center;
    gap: 8px;
  }

  .ip-batch {
    background: rgba(255, 255, 255, 0.1);
    padding: 1px 6px;
    border-radius: 4px;
    font-family: monospace;
    font-size: 11px;
    color: #cbd5e1;
  }

  .webssh-controls {
    display: flex;
    gap: 12px;
    align-items: center;
  }

  .control-group {
    display: flex;
    gap: 8px;
    align-items: center;
    background: rgba(0, 0, 0, 0.2);
    padding: 4px;
    border-radius: 6px;
    border: 1px solid rgba(255, 255, 255, 0.05);
  }

  .webssh-input {
    padding: 6px 12px;
    background: transparent;
    border: none;
    border-right: 1px solid rgba(255, 255, 255, 0.1);
    color: #e2e8f0;
    font-size: 13px;
    min-width: 80px;
    transition: all 0.2s;
  }

  .webssh-key-input {
    min-height: 6.5rem;
    resize: vertical;
    font-family:
      "SFMono-Regular", "SF Mono", Menlo, Monaco, Consolas,
      "Liberation Mono", monospace;
  }

  .control-group input:last-of-type {
    border-right: none;
  }

  .webssh-input:focus {
    outline: none;
    background: rgba(255, 255, 255, 0.05);
  }

  .webssh-button {
    padding: 6px 16px;
    background: rgba(255, 255, 255, 0.1);
    border: 1px solid rgba(255, 255, 255, 0.05);
    border-radius: 4px;
    color: #e2e8f0;
    font-size: 13px;
    font-weight: 500;
    cursor: pointer;
    transition: all 0.2s;
  }

  .webssh-button.primary {
    background: var(--primary);
    color: white;
    border: none;
  }

  .webssh-button.icon-only {
    padding: 6px;
    display: flex;
    align-items: center;
    justify-content: center;
    color: #94a3b8;
  }

  .webssh-button:hover:not(:disabled) {
    background: rgba(255, 255, 255, 0.15);
    color: white;
  }

  .webssh-button.primary:hover:not(:disabled) {
    filter: brightness(1.1);
  }

  .webssh-button:disabled {
    opacity: 0.5;
    cursor: not-allowed;
  }

  .webssh-close {
    background: none;
    border: none;
    color: #94a3b8;
    font-size: 20px;
    cursor: pointer;
    padding: 4px;
    margin-left: 8px;
    border-radius: 4px;
    transition: all 0.2s;
    width: 32px;
    height: 32px;
    display: flex;
    align-items: center;
    justify-content: center;
  }

  .webssh-close:hover {
    color: #ef4444;
    background: rgba(239, 68, 68, 0.1);
  }

  .webssh-error {
    background: rgba(239, 68, 68, 0.1);
    border-bottom: 1px solid rgba(239, 68, 68, 0.2);
    padding: 12px 24px;
    display: flex;
    justify-content: space-between;
    align-items: center;
    color: #fca5a5;
    font-size: 13px;
  }

  .webssh-error button {
    background: none;
    border: none;
    color: #fca5a5;
    cursor: pointer;
    font-size: 16px;
  }

  .webssh-terminal-wrapper {
    flex: 1;
    position: relative;
    /* Padding goes on the WRAPPER, not on the element xterm mounts into.
       CSS padding on the xterm container breaks FitAddon's size calculation,
       producing incorrect cols/rows. */
    padding: 12px 16px;
    background: #0d1117;
    overflow: hidden;
    min-height: 0; /* Required for flex shrink in column layout */
  }

  .webssh-terminal {
    /* Must be 100% of the wrapper — no padding, no margin.
       xterm appends a <canvas> here; any CSS that affects its box model
       will desync FitAddon's measurements. */
    width: 100%;
    height: 100%;
    overflow: hidden;
  }

  .webssh-terminal:focus {
    outline: none;
  }

  /* Make the xterm viewport fill the container correctly */
  :global(.webssh-terminal .xterm) {
    height: 100%;
  }

  :global(.webssh-terminal .xterm-viewport) {
    overflow-y: scroll !important;
  }

  .webssh-placeholder {
    position: absolute;
    inset: 0;
    display: flex;
    flex-direction: column;
    align-items: center;
    justify-content: center;
    color: #64748b;
    /* Sits on top of the xterm canvas when terminal is not yet active */
    background: #0d1117;
    z-index: 1;
    pointer-events: none;
  }

  .webssh-placeholder p {
    margin: 0;
    font-size: 15px;
    font-weight: 500;
    margin-bottom: 8px;
  }

  .webssh-footer {
    padding: 8px 24px;
    border-top: 1px solid rgba(255, 255, 255, 0.05);
    background: rgba(0, 0, 0, 0.2);
    color: #64748b;
    font-size: 11px;
    text-align: right;
    display: flex;
    justify-content: space-between;
    align-items: center;
  }

  @media (max-width: 768px) {
    .webssh-modal-content {
      width: 95%;
      height: 90vh;
      max-height: none;
    }

    .webssh-header {
      flex-wrap: wrap;
    }

    .webssh-controls {
      width: 100%;
      flex-wrap: wrap;
      justify-content: flex-end;
      gap: 4px;
    }

    .webssh-input {
      min-width: 80px;
    }
  }
</style>
