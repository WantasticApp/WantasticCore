<script lang="ts">
  import AppWindow from "$components/AppWindow.svelte";
  import { peerStore } from "$store/peer";
  import { winboxStore } from "$store/winbox";
  import { winboxAccountStore } from "$store/winboxAccounts";
  import { openedApps, activeThing, bringToFront } from "$store/store";
  import { translateError$, _ } from "$store/i18n";
  import { toasts } from "$store/toast";
  import { onMount } from "svelte";
  import { Button, TextBox, ComboBox } from "fluent-svelte";

  // Form state
  let selectedPeerId = "";
  let sessionName = "";
  let routerIP = "";
  let routerPort = 8291;
  let username = "admin";
  let password = "";
  let isCreating = false;
  let error = "";
  let isLoadingPeers = true;

  // Get all peers from store
  $: allPeers = $peerStore.peers;
  $: peers = allPeers;

  // Helper to check if peer has Winbox detected
  function hasWinboxDetected(peer: any): boolean {
    if (peer.scanned_winbox_port && peer.scanned_winbox_port > 0) {
      return true;
    }
    if (peer.discovered_ports && peer.discovered_ports.length > 0) {
      return peer.discovered_ports.some(
        (port: any) =>
          port.service?.toLowerCase().includes("winbox") ||
          port.service?.toLowerCase().includes("mikrotik") ||
          port.port === 8291,
      );
    }
    if (peer.has_winbox) {
      return true;
    }
    return false;
  }

  // Format peers for ComboBox
  $: peerItems = peers.map((p) => {
    const status = hasWinboxDetected(p) ? "🟢" : "⚪";
    const name = p.name || p.assigned_ip || p.ip_address || "Unknown";
    const portInfo = p.scanned_winbox_port
      ? ` (port ${p.scanned_winbox_port})`
      : "";
    return {
      name: `${status} ${name}${portInfo}`,
      value: p.id,
    };
  });

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

  // Handle peer selection change
  $: if (selectedPeerId) {
    const peer = peers.find((p) => p.id === selectedPeerId);
    if (peer) {
      if (peer.assigned_ip && !routerIP) {
        routerIP = peer.assigned_ip;
      }
      if (peer.scanned_winbox_port && peer.scanned_winbox_port > 0) {
        routerPort = peer.scanned_winbox_port;
      }
    }
  }

  // User-friendly error messages
  function getErrorMessage(err: any): string {
    const msg = err?.message || String(err);
    if (
      msg.includes("ResourceExhausted") ||
      msg.includes("maximum number of Winbox sessions")
    )
      return "errors.winboxLimitReached";
    if (msg.includes("timeout") || msg.includes("DeadlineExceeded"))
      return "winbox.errors.timeout";
    if (msg.includes("connection refused") || msg.includes("ECONNREFUSED"))
      return "winbox.errors.connectionRefused";
    if (msg.includes("network") || msg.includes("Unreachable"))
      return "winbox.errors.network";
    if (msg.includes("authentication") || msg.includes("Login failed"))
      return "winbox.errors.authFailed";
    if (msg.includes("not found") || msg.includes("NotFound"))
      return "winbox.errors.notFound";
    return msg || "winbox.errors.default";
  }

  async function handleCreateSession() {
    if (
      !selectedPeerId ||
      !sessionName ||
      !routerIP ||
      !username ||
      !password
    ) {
      error = $_("winbox.form.fillRequired");
      return;
    }

    isCreating = true;
    error = "";

    try {
      const creationPromise = winboxStore.createProxy(selectedPeerId, {
        name: sessionName,
        routerIP,
        routerPort,
        username,
        password,
      });

      await creationPromise;
      await winboxAccountStore.listAccounts();
      handleClose();
      toasts.success($_("winbox.form.sessionCreated"));
    } catch (err: any) {
      console.error("Failed to create Winbox session:", err);
      const errorMsg = getErrorMessage(err);
      toasts.error(errorMsg);
      error = errorMsg;
    } finally {
      isCreating = false;
    }
  }

  function handleClose() {
    $openedApps = $openedApps.filter((app) => app !== "NewWinboxSession");
  }
</script>

<AppWindow
  appName="NewWinboxSession"
  title="New Winbox Session"
  canReduce={false}
  canMaximize={false}
  width="500px"
  height="auto"
  top="15%"
  left="32%"
  minHeight="auto"
>
  <div slot="header_icon" style="display: flex; align-items: center;">
    <img
      src="img/icon/Winbox.svg"
      alt="Icon of Winbox"
      height="16"
      width="16"
    />
    <span class="appName pl-2">New Winbox Session</span>
  </div>

  <div class="mainApp">
    <form class="content" on:submit|preventDefault={handleCreateSession}>
      {#if error}
        <div class="error-message">
          {$translateError$(error)}
        </div>
      {/if}

      <div class="form-group">
        <label for="peer">{$_("winbox.form.peer")} *</label>
        {#if isLoadingPeers}
          <div class="loading-indicator">
            <span class="spinner" />
            {$_("common.loading")}
          </div>
        {:else if peers.length === 0}
          <div class="no-peers-message">
            <span></span>
            {$_("peers.noPeersMessage")}
          </div>
        {:else}
          <ComboBox
            items={peerItems}
            bind:value={selectedPeerId}
            placeholder={$_("winbox.form.selectPeer")}
            class="w-full"
          />
        {/if}
        <span class="help-text">{$_("winbox.form.winboxDetected")}</span>
      </div>

      <div class="form-group">
        <label for="session-name">{$_("winbox.form.sessionName")} *</label>
        <TextBox
          id="session-name"
          bind:value={sessionName}
          placeholder={$_("winbox.form.sessionNamePlaceholder")}
          required
        />
        <span class="help-text">{$_("winbox.form.sessionNameHelp")}</span>
      </div>

      <div class="form-row">
        <div class="form-group">
          <label for="router-ip">{$_("winbox.form.routerIp")} *</label>
          <TextBox
            id="router-ip"
            bind:value={routerIP}
            placeholder="192.168.88.1"
            required
          />
        </div>

        <div class="form-group">
          <label for="port">{$_("winbox.form.port")}</label>
          <TextBox
            id="port"
            type="number"
            bind:value={routerPort}
            placeholder="8291"
            min="1"
            max="65535"
          />
        </div>
      </div>

      <div class="form-group">
        <label for="username">{$_("winbox.form.username")} *</label>
        <TextBox
          id="username"
          bind:value={username}
          placeholder="admin"
          required
        />
      </div>

      <div class="form-group">
        <label for="password">{$_("winbox.form.password")} *</label>
        <TextBox
          id="password"
          type="password"
          bind:value={password}
          placeholder={$_("winbox.form.enterPassword")}
          required
        />
      </div>

      <div class="actions">
        <Button variant="standard" on:click={handleClose}>
          {$_("winbox.form.cancel")}
        </Button>
        <Button
          variant="accent"
          type="submit"
          disabled={isCreating ||
            !selectedPeerId ||
            !sessionName ||
            !routerIP ||
            !username ||
            !password}
        >
          {isCreating
            ? $_("winbox.form.creating")
            : $_("winbox.form.createSession")}
        </Button>
      </div>
    </form>
  </div>
</AppWindow>

<style lang="scss">
  .mainApp {
    display: flex;
    flex-direction: column;
  }

  .content {
    padding: 24px;
    display: flex;
    flex-direction: column;
    gap: 16px;
    overflow-y: auto;
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
  }

  .form-row {
    display: flex;
    gap: 16px;
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

  .actions {
    display: flex;
    justify-content: flex-end;
    gap: 12px;
    padding-top: 8px;
    border-top: 1px solid rgb(var(--clr) / 10%);
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
</style>
