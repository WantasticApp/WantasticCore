<script lang="ts">
  import AppWindow from "$components/AppWindow.svelte";
  import { peerStore } from "$store/peer";
  import { websshStore } from "$store/webssh";
  import { openedApps } from "$store/store";
  import { translateError$, _ } from "$store/i18n";
  import { onMount } from "svelte";
  import { Button, TextBox } from "fluent-svelte";

  // Form state
  let selectedPeerId = peerStore.getSelectedPeer()?.id || "";
  let selectedPeerIp = "";
  let username = "root";
  let password = "";
  let privateKey = "";
  let privateKeyPassphrase = "";
  let sshPort = 22;
  let currentsshPort = 22;
  // Guard: track which peer the port was last auto-assigned for, so user edits
  // are not overwritten when the peers list refreshes (store polling re-runs the
  // reactive block even though the selected peer didn't change).
  let _portAssignedForPeer = "";
  function _autoAssignPort(peerId: string, peer: any) {
    if (peerId === _portAssignedForPeer) return; // peer unchanged — keep user's edit
    _portAssignedForPeer = peerId;
    currentsshPort = peer.scanned_ssh_port && peer.scanned_ssh_port > 0
      ? peer.scanned_ssh_port
      : sshPort;
  }
  let terminalCols = 80;
  let terminalRows = 24;
  let isCreating = false;
  let error = "";
  let portError = "";
  let isLoadingPeers = true;
  let peerSearch = "";

  // Strip any non-digit characters from the port input, then clamp to 1-65535.
  // fluent-svelte TextBox does not enforce type="number" natively, so we do it
  // manually on every keystroke.
  function handlePortInput(e: Event) {
    const raw = (e.target as HTMLInputElement).value;
    // Keep only digits
    const digits = raw.replace(/\D/g, "");
    if (digits === "") {
      currentsshPort = 22;
      portError = "";
      (e.target as HTMLInputElement).value = "";
      return;
    }
    const num = parseInt(digits, 10);
    if (num < 1) {
      currentsshPort = 1;
      portError = "Port must be ≥ 1";
      (e.target as HTMLInputElement).value = "1";
    } else if (num > 65535) {
      currentsshPort = 65535;
      portError = "Port must be ≤ 65535";
      (e.target as HTMLInputElement).value = "65535";
    } else {
      currentsshPort = num;
      portError = "";
      // Rewrite value to strip any leading zeroes / non-digit characters
      if ((e.target as HTMLInputElement).value !== String(num)) {
        (e.target as HTMLInputElement).value = String(num);
      }
    }
  }

  // Get all peers from store
  $: allPeers = $peerStore.peers;
  $: peers = allPeers;

  // Helper to check if peer has SSH detected
  function hasSshDetected(peer: any): boolean {
    if (peer.scanned_ssh_port && peer.scanned_ssh_port > 0) {
      return true;
    }
    if (peer.discovered_ports && peer.discovered_ports.length > 0) {
      return peer.discovered_ports.some(
        (port: any) =>
          port.service?.toLowerCase().includes("ssh") || port.port === 22,
      );
    }
    return false;
  }

  // Format peers for the inline searchable device picker.
  $: peerItems = peers
    .map((p) => {
      const sshDetected = hasSshDetected(p);
      const name = p.name || p.id;
      const ip = p.assigned_ip || p.ip_address || "No IP";
      const portInfo =
        p.scanned_ssh_port && p.scanned_ssh_port !== 22
          ? ` - port ${p.scanned_ssh_port}`
          : "";
      return {
        id: p.id,
        name,
        ip,
        portInfo,
        value: p.id,
        _sshDetected: sshDetected,
        _searchText: `${name} ${ip} ${p.id} ${portInfo}`.toLowerCase(),
      };
    })
    .sort((a, b) => {
      // SSH-detected peers first
      if (a._sshDetected && !b._sshDetected) return -1;
      if (!a._sshDetected && b._sshDetected) return 1;
      return a.name.localeCompare(b.name);
    });

  $: normalizedPeerSearch = peerSearch.trim().toLowerCase();
  $: filteredPeerItems = normalizedPeerSearch
    ? peerItems.filter((item) => item._searchText.includes(normalizedPeerSearch))
    : peerItems;
  $: selectedPeerItem = peerItems.find((item) => item.value === selectedPeerId);

  function selectPeer(peerId: string) {
    selectedPeerId = peerId;
    error = "";
  }

  onMount(async () => {
    isLoadingPeers = true;
    try {
      if (peers.length === 0) {
        await peerStore.listPeers();
      }
      
    } catch (err: any) {
      console.error("Failed to load peers:", err);
    } finally {
      isLoadingPeers = false;
    }
  });

  // Handle peer selection
  $: if (selectedPeerId) {
    const peer = peers.find((p) => p.id === selectedPeerId);
    if (peer) {
      selectedPeerIp = peer.assigned_ip || peer.ip_address || "";
      // Only auto-assign the port when the peer actually changes, not on every
      // peers-list refresh.  User edits to the port field are preserved.
      _autoAssignPort(selectedPeerId, peer);
    } else {
      // If there's a peer with SSH detected, pre-select it
      const peer = peerStore.getSelectedPeer();
      if (peer) {
        selectedPeerId = peer.id;
      } else {
        const firstSshPeer = peers.find((p) => hasSshDetected(p));
        if (firstSshPeer) {
          selectedPeerId = firstSshPeer.id;
        }
      }
    }
  }

  // User-friendly error messages
  function getErrorMessage(err: any): string {
    const msg = err?.message || String(err);
    if (msg.includes("timeout") || msg.includes("DeadlineExceeded"))
      return "The connection timed out. The device may be offline, slow, or SSH services might be blocked.";
    if (msg.includes("connection refused") || msg.includes("ECONNREFUSED"))
      return "Connection refused. Please verify that SSH is enabled on the device and the port is correct.";
    if (msg.includes("network") || msg.includes("Unreachable"))
      return "Network error. The device is unreachable. Please check if the peer is online.";
    if (msg.includes("authentication") || msg.includes("Permission denied"))
      return "SSH authentication failed. Please check your username, password, or SSH keys.";
    if (msg.includes("no host key") || msg.includes("host key mismatch"))
      return "SSH host key error. The device fingerprint has changed or is unknown.";
    if (msg.includes("not found") || msg.includes("NotFound"))
      return "The requested device or session was not found.";
    return (
      msg || "An unexpected error occurred while creating the SSH session."
    );
  }

  async function handleCreateSession() {
    if (!selectedPeerId || !selectedPeerIp) {
      error = "Please select a peer";
      return;
    }

    if (!username) {
      error = "Username is required";
      return;
    }

    if (privateKeyPassphrase && !privateKey.trim()) {
      error = "Private key is required when a private key passphrase is set";
      return;
    }

    isCreating = true;
    error = "";

    try {
      const session = await websshStore.createSession(
        selectedPeerId,
        selectedPeerIp,
        username,
        password || undefined,
        currentsshPort,
        terminalCols,
        terminalRows,
        privateKey || undefined,
        privateKeyPassphrase || undefined,
      );
      websshStore.openTerminal(session);
      void websshStore.listActiveSessions(true).catch((err) => {
        console.warn("Failed to refresh WebSSH sessions after create:", err);
      });
      handleClose();
    } catch (err: any) {
      console.error("Failed to create SSH session:", err);
      error = getErrorMessage(err);
    } finally {
      isCreating = false;
    }
  }

  function handleClose() {
    $openedApps = $openedApps.filter((app) => app !== "NewSSHSession");
  }
</script>

<AppWindow
  appName="NewSSHSession"
  title="New SSH Session"
  canReduce={false}
  canMaximize={false}
  width="500px"
  height="min(82vh, 920px)"
  top="8%"
  left="32%"
  minHeight="360px"
>
  <div slot="header_icon" style="display: flex; align-items: center;">
    <svg
      xmlns="http://www.w3.org/2000/svg"
      width="16"
      height="16"
      viewBox="0 0 100 100"
    >
      <defs>
        <linearGradient id="sshGradNew" x1="0%" y1="0%" x2="100%" y2="100%">
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
        stroke="url(#sshGradNew)"
        stroke-width="3"
      />
      <rect
        x="10"
        y="20"
        width="80"
        height="10"
        rx="4"
        fill="url(#sshGradNew)"
      />
      <circle cx="18" cy="25" r="2" fill="#ef4444" />
      <circle cx="25" cy="25" r="2" fill="#fbbf24" />
      <circle cx="32" cy="25" r="2" fill="#10b981" />
      <text x="20" y="48" fill="#10b981" font-family="monospace" font-size="10"
        >$</text
      >
    </svg>
    <span class="appName pl-2">New SSH Session</span>
  </div>

  <div class="mainApp">
    <form class="content" on:submit|preventDefault={handleCreateSession}>
      {#if error}
        <div class="error-message">
          {$translateError$(error)}
        </div>
      {/if}

      <div class="form-group relative">
        <label for="peer">Peer Device *</label>
        {#if isLoadingPeers}
          <div class="loading-indicator">
            <span class="spinner" />
            Loading peers...
          </div>
        {:else if peers.length === 0}
          <div class="no-peers-message">
            <span></span> No peers found. Add a peer first.
          </div>
        {:else}
          <div class="peer-picker">
            <div class="peer-search-shell">
              <svg
                class="peer-search-icon"
                width="15"
                height="15"
                viewBox="0 0 24 24"
                fill="none"
                stroke="currentColor"
                stroke-width="2"
                aria-hidden="true"
              >
                <circle cx="11" cy="11" r="8" />
                <path d="m21 21-4.3-4.3" />
              </svg>
              <input
                id="peer"
                class="peer-search-input"
                type="search"
                bind:value={peerSearch}
                placeholder="Search device name, IP, or ID"
                autocomplete="off"
              />
            </div>

            {#if selectedPeerItem}
              <div class="selected-peer-summary" title={`${selectedPeerItem.name} (${selectedPeerItem.ip})`}>
                <span class:online-dot={selectedPeerItem._sshDetected} class="status-dot" />
                <span class="selected-peer-text">
                  <strong>{selectedPeerItem.name}</strong>
                  <small>{selectedPeerItem.ip}{selectedPeerItem.portInfo}</small>
                </span>
              </div>
            {/if}

            <div class="peer-option-list" role="listbox" aria-label="Peer devices">
              {#if filteredPeerItems.length === 0}
                <div class="peer-empty">No devices match “{peerSearch}”.</div>
              {:else}
                {#each filteredPeerItems as item (item.value)}
                  <button
                    type="button"
                    class:selected={selectedPeerId === item.value}
                    class="peer-option"
                    role="option"
                    aria-selected={selectedPeerId === item.value}
                    title={`${item.name} (${item.ip})${item.portInfo}`}
                    on:click={() => selectPeer(item.value)}
                  >
                    <span class:online-dot={item._sshDetected} class="status-dot" />
                    <span class="peer-option-main">
                      <span class="peer-name">{item.name}</span>
                      <span class="peer-meta">{item.ip}{item.portInfo}</span>
                    </span>
                  </button>
                {/each}
              {/if}
            </div>
          </div>
        {/if}
        <span class="help-text">Green = SSH detected, gray = not detected but may still work</span>
      </div>

      {#if selectedPeerIp}
        <div class="form-group">
          <!-- svelte-ignore a11y-label-has-associated-control -->
          <label>Peer IP</label>
          <div class="peer-ip-display">{selectedPeerIp}</div>
        </div>
      {/if}

      <div class="form-row">
        <div class="form-group">
          <label for="username">Username *</label>
          <TextBox
            id="username"
            bind:value={username}
            placeholder="root"
            required
          />
        </div>

        <div class="form-group">
          <label for="port">SSH Port</label>
          <TextBox
            id="port"
            bind:value={currentsshPort}
            placeholder="22"
            inputmode="numeric"
            pattern="[0-9]*"
            on:input={handlePortInput}
          />
          {#if portError}
            <span class="port-error">{portError}</span>
          {/if}
        </div>
      </div>

      <div class="form-group">
        <label for="password">Password (optional)</label>
        <TextBox
          id="password"
          type="password"
          bind:value={password}
          placeholder="Leave empty to see banner and input password"
        />
      </div>

      <div class="form-group">
        <label for="private-key">Private Key (optional)</label>
        <textarea
          id="private-key"
          bind:value={privateKey}
          placeholder="Paste OpenSSH or PEM private key for public-key authentication"
          rows="6"
          class="private-key-input"
          spellcheck="false"
        />
      </div>

      <div class="form-group">
        <label for="private-key-passphrase">Key Passphrase (optional)</label>
        <TextBox
          id="private-key-passphrase"
          type="password"
          bind:value={privateKeyPassphrase}
          placeholder="Only needed for encrypted private keys"
        />
      </div>

      <div class="form-row">
        <div class="form-group">
          <label for="cols">Columns</label>
          <TextBox
            id="cols"
            type="number"
            bind:value={terminalCols}
            min="40"
            max="200"
          />
        </div>

        <div class="form-group">
          <label for="rows">Rows</label>
          <TextBox
            id="rows"
            type="number"
            bind:value={terminalRows}
            min="10"
            max="120"
          />
        </div>
      </div>

      <div class="actions">
        <Button variant="standard" class="px-5" on:click={handleClose}>
          Cancel
        </Button>
        <Button
          variant="accent"
          type="submit"
          disabled={isCreating || !selectedPeerId || !username || !!portError}
        >
          {isCreating ? "Connecting..." : "Connect"}
        </Button>
      </div>
    </form>
  </div>
</AppWindow>

<style lang="scss">
  .mainApp {
    display: flex;
    flex-direction: column;
    flex: 1;
    min-height: 0;
    min-width: 0;
  }

  .private-key-input {
    width: 100%;
    min-width: 0;
    box-sizing: border-box;
    min-height: 140px;
    padding: 0.75rem;
    border-radius: 0.5rem;
    border: 1px solid rgba(255, 255, 255, 0.12);
    background: rgba(15, 23, 42, 0.5);
    color: inherit;
    font-family: "SFMono-Regular", "SF Mono", Menlo, Monaco, Consolas, "Liberation Mono", monospace;
    font-size: 0.875rem;
    resize: vertical;
  }

  .content {
    padding: 24px 24px 0px 24px;
    display: flex;
    flex-direction: column;
    gap: 6px;
    flex: 1;
    min-height: 0;
    min-width: 0;
    overflow-y: auto;
    overflow-x: hidden;
    overscroll-behavior: contain;
  }

  .error-message {
    padding: 12px;
    background: rgba(239, 68, 68, 0.1);
    border: 1px solid rgba(239, 68, 68, 0.3);
    border-radius: 6px;
    color: #ef4444;
    font-size: 14px;
  }

  .form-group {
    display: flex;
    flex-direction: column;
    gap: 8px;
    flex: 1;
    min-width: 0;
  }

  .form-row {
    display: flex;
    gap: 10px;
    padding-bottom:24px !important;
    min-width: 0;
  }

  label {
    font-size: 14px;
    font-weight: 600;
    color: rgb(var(--clr) / 90%);
  }

  .help-text {
    font-size: 12px;
    color: rgb(var(--clr) / 60%);
    font-style: italic;
  }

  .peer-picker {
    display: grid;
    gap: 8px;
    min-width: 0;
  }

  .peer-search-shell {
    position: relative;
    min-width: 0;
  }

  .peer-search-icon {
    position: absolute;
    left: 12px;
    top: 50%;
    transform: translateY(-50%);
    color: rgb(var(--clr) / 48%);
    pointer-events: none;
  }

  .peer-search-input {
    width: 100%;
    min-width: 0;
    height: 38px;
    box-sizing: border-box;
    border: 1px solid rgb(var(--clr) / 14%);
    border-radius: 6px;
    background: rgb(var(--bg2));
    color: rgb(var(--clr));
    padding: 0 12px 0 36px;
    font-size: 14px;
    outline: none;
    transition:
      border-color 0.15s ease,
      background 0.15s ease,
      box-shadow 0.15s ease;
  }

  .peer-search-input:focus {
    border-color: rgb(var(--clrPrm) / 70%);
    background: rgb(var(--bg1));
    box-shadow: inset 0 -1px 0 rgb(var(--clrPrm));
  }

  .peer-search-input::placeholder {
    color: rgb(var(--clr) / 42%);
  }

  .selected-peer-summary {
    display: grid;
    grid-template-columns: 10px minmax(0, 1fr);
    align-items: center;
    gap: 10px;
    min-width: 0;
    padding: 9px 10px;
    border: 1px solid rgb(var(--clrPrm) / 24%);
    border-radius: 6px;
    background: rgb(var(--clrPrm) / 8%);
  }

  .selected-peer-text,
  .peer-option-main {
    min-width: 0;
    display: grid;
    gap: 2px;
  }

  .selected-peer-text strong,
  .peer-name,
  .selected-peer-text small,
  .peer-meta {
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }

  .selected-peer-text strong,
  .peer-name {
    color: rgb(var(--clr));
    font-size: 13px;
    font-weight: 650;
  }

  .selected-peer-text small,
  .peer-meta {
    color: rgb(var(--clr) / 58%);
    font-size: 12px;
  }

  .peer-option-list {
    display: grid;
    gap: 4px;
    max-height: 212px;
    min-height: 44px;
    overflow-y: auto;
    overscroll-behavior: contain;
    padding: 4px;
    border: 1px solid rgb(var(--clr) / 10%);
    border-radius: 6px;
    background: rgb(var(--bg2) / 76%);
    scrollbar-width: thin;
  }

  .peer-option {
    display: grid;
    grid-template-columns: 10px minmax(0, 1fr);
    align-items: center;
    gap: 10px;
    width: 100%;
    min-width: 0;
    min-height: 46px;
    padding: 7px 9px;
    border: 1px solid transparent;
    border-radius: 5px;
    background: transparent;
    color: inherit;
    text-align: left;
    cursor: pointer;
    box-sizing: border-box;
  }

  .peer-option:hover {
    background: rgb(var(--clr) / 6%);
  }

  .peer-option.selected {
    border-color: rgb(var(--clrPrm) / 32%);
    background: rgb(var(--clrPrm) / 12%);
  }

  .status-dot {
    width: 9px;
    height: 9px;
    border-radius: 999px;
    background: rgb(var(--clr) / 32%);
    box-shadow: 0 0 0 2px rgb(var(--clr) / 8%);
  }

  .status-dot.online-dot {
    background: #10b981;
    box-shadow: 0 0 0 2px rgba(16, 185, 129, 0.18);
  }

  .peer-empty {
    padding: 12px;
    color: rgb(var(--clr) / 56%);
    font-size: 13px;
    text-align: center;
  }

  .loading-indicator {
    display: flex;
    align-items: center;
    gap: 8px;
    padding: 12px;
    background: rgb(var(--bg2));
    border: 1px solid rgb(var(--clr) / 10%);
    border-radius: 6px;
    font-size: 14px;
    color: rgb(var(--clr) / 70%);
  }

  .spinner {
    width: 16px;
    height: 16px;
    border: 2px solid rgb(var(--clr) / 20%);
    border-top-color: rgb(var(--clrPrm));
    border-radius: 50%;
    animation: spin 1s linear infinite;
  }

  @keyframes spin {
    to {
      transform: rotate(360deg);
    }
  }

  .no-peers-message {
    padding: 12px;
    background: rgba(251, 191, 36, 0.1);
    border: 1px solid rgba(251, 191, 36, 0.3);
    border-radius: 6px;
    color: #fbbf24;
    font-size: 14px;
    line-height: 1.5;
  }

  .peer-ip-display {
    padding: 10px 12px;
    background: rgb(var(--bg2));
    border: 1px solid rgb(var(--clr) / 10%);
    border-radius: 6px;
    font-size: 14px;
    color: rgb(var(--clrPrm));
    font-family: monospace;
  }

  .port-error {
    font-size: 12px;
    color: #ef4444;
    margin-top: -4px;
  }

  .actions {
    display: flex;
    justify-content: flex-end;
    gap: 12px;
    padding-top: 8px;
    padding-bottom: 10px;
    border-top: 1px solid rgb(var(--clr) / 10%);
    position: sticky;
    bottom: 0;
    background: var(--mica);
  }

  /* Mobile responsive styles */
  @media (max-width: 768px) {
    .content {
      padding: 20px 16px;
    }

    .form-row {
      flex-direction: column;
      gap: 12px;
    }

    .actions {
      flex-direction: column;
      gap: 10px;
    }
  }

  @media (max-width: 480px) {
    .content {
      padding: 16px 12px;
    }

    .form-group label {
      font-size: 13px;
    }
  }
</style>
