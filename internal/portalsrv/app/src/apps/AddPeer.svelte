<script lang="ts">
  import { draggable } from "@neodrag/svelte";
  import { scale } from "svelte/transition";
  import Titlebar from "$components/shared/Titlebar.svelte";
  import {
    peerStore,
    onboardingPeer,
    type EnrollmentToken,
  } from "../store/peer";
  import { websshStore } from "../store/webssh";
  import { winboxStore } from "../store/winbox";
  import { snapshotStore, type DeviceSnapshot } from "$store/snapshot";
  import {
    openedApps,
    activeThing,
    appZIndexes,
    bringToFront,
  } from "$store/store";
  import { translateError$ } from "$store/i18n";
  import { toasts } from "$store/toast";

  import { onMount } from "svelte";
  import { Button, TextBox, ToggleSwitch } from "fluent-svelte";

  // Form state
  let name = "";
  let isLoading = false;
  let error = "";
  let claimPublicKey = "";
  let claimServerBase = "";
  let claimKeyEntryMode = false;
  let createSshAccount = false;
  let createWinboxAccount = false;

  // SSH credentials
  let sshUsername = "root";
  let sshPassword = "";
  let sshPort = 22;

  // Winbox credentials
  let winboxUsername = "admin";
  let winboxPassword = "";
  let winboxPort = 8291;

  let creatingAccounts = false;
  let accountCreationError = "";

  // Provisioning
  let provisionAfterCreate = false;
  let selectedSnapshotId = "";
  $: wuspSnapshots = $snapshotStore.snapshots.filter((s: DeviceSnapshot) => s.protocol === "wusp");
  $: isClaimMode = claimKeyEntryMode || claimPublicKey.trim().length > 0;
  $: claimServerHost =
    claimServerBase.trim() ||
    (typeof window !== "undefined" ? window.location.origin : "https://console.wantastic.app");

  function getClaimPublicKeyFromURL(): string {
    const params = new URLSearchParams(window.location.search);
    const hash = window.location.hash.slice(1);
    const hashQuery = hash.includes("?") ? hash.slice(hash.indexOf("?") + 1) : "";
    const hashParams = new URLSearchParams(hashQuery);
    return (
      hashParams.get("claim_public_key") ||
      hashParams.get("public_key") ||
      hashParams.get("pk") ||
      params.get("claim_public_key") ||
      params.get("public_key") ||
      params.get("pk") ||
      ""
    ).trim();
  }

  function getScannedClaimServerFromURL(): string {
    const params = new URLSearchParams(window.location.search);
    const hash = window.location.hash.slice(1);
    const hashQuery = hash.includes("?") ? hash.slice(hash.indexOf("?") + 1) : "";
    const hashParams = new URLSearchParams(hashQuery);
    return (
      hashParams.get("wantastic_server") ||
      hashParams.get("server") ||
      hashParams.get("domain") ||
      params.get("wantastic_server") ||
      params.get("server") ||
      params.get("domain") ||
      ""
    ).trim();
  }

  function normalizeClaimServer(value: string): string {
    const raw = value.trim();
    if (!raw) return "";
    try {
      const withScheme = /^https?:\/\//i.test(raw) ? raw : `https://${raw}`;
      const url = new URL(withScheme);
      return url.origin;
    } catch {
      return "";
    }
  }

  function getClaimServerFromURL(): string {
    const scanned = normalizeClaimServer(getScannedClaimServerFromURL());
    const saved = normalizeClaimServer(localStorage.getItem("wantastic_claim_server") || "");
    if (saved && saved !== scanned) return saved;
    return scanned || saved || (typeof window !== "undefined" ? window.location.origin : "https://console.wantastic.app");
  }

  function currentOrigin(): string {
    return typeof window !== "undefined" ? window.location.origin : "";
  }

  function redirectToClaimServerIfNeeded(): boolean {
    if (!isClaimMode) return false;
    const normalized = normalizeClaimServer(claimServerHost);
    if (!normalized) {
      error = "Enter a valid Wantastic server domain or leave it as the default";
      return true;
    }
    localStorage.setItem("wantastic_claim_server", normalized);
    if (currentOrigin() && normalized !== currentOrigin()) {
      const key = encodeURIComponent(claimPublicKey.trim());
      window.location.href = `${normalized}/#desktop?claim_public_key=${key}&wantastic_server=${encodeURIComponent(normalized)}`;
      return true;
    }
    claimServerBase = normalized;
    return false;
  }

  onMount(async () => {
    claimPublicKey = getClaimPublicKeyFromURL();
    claimKeyEntryMode = claimPublicKey.trim().length > 0;
    claimServerBase = normalizeClaimServer(getClaimServerFromURL());
    if (claimPublicKey && !name) {
      name = "Wantastic Device";
    }
    // Load WUSP snapshots so user can optionally provision the new device
    snapshotStore.list("wusp").catch(() => {});
  });

  // Z-index for window stacking
  $: zIndex = $appZIndexes["AddPeer"] || 100;

  function handleFocus() {
    $activeThing = "AddPeer";
    bringToFront("AddPeer");
  }

  async function handleSubmit() {
    if (redirectToClaimServerIfNeeded()) {
      return;
    }
    if (!name.trim()) {
      error = "Device name is required";
      return;
    }
    if (isClaimMode && !claimPublicKey.trim()) {
      error = "Device claim QR is missing the public key";
      return;
    }

    // Validate auto-create credentials if enabled
    if (createSshAccount) {
      if (!sshUsername.trim()) {
        error = "SSH username is required";
        return;
      }
      if (!sshPassword.trim()) {
        error = "SSH password is required";
        return;
      }
    }

    if (createWinboxAccount) {
      if (!winboxUsername.trim()) {
        error = "Winbox username is required";
        return;
      }
      if (!winboxPassword.trim()) {
        error = "Winbox password is required";
        return;
      }
    }

    isLoading = true;
    error = "";

    try {
      // Create the peer first
      const peer = await peerStore.addPeer(name, {
        publicKey: isClaimMode ? claimPublicKey.trim() : "",
      });

      // If auto-create is enabled, create SSH and Winbox sessions
      if (createSshAccount || createWinboxAccount) {
        creatingAccounts = true;
        accountCreationError = "";

        try {
          const peerIp = peer.assigned_ip || peer.ip_address;
          if (peerIp) {
            // Create SSH session
            if (createSshAccount) {
              try {
                await websshStore.createSession(
                  peer.id,
                  peerIp,
                  sshUsername,
                  sshPassword,
                  sshPort,
                );
                toasts.success(`SSH session created for ${sshUsername}`);
              } catch (sshErr: any) {
                const sshErrorMsg =
                  sshErr.message || "Failed to create SSH session";
                accountCreationError += `SSH: ${sshErrorMsg}\n`;
                toasts.error(`SSH creation failed: ${sshErrorMsg}`);
              }
            }

            // Create Winbox session
            if (createWinboxAccount) {
              try {
                await winboxStore.createProxy(peer.id, {
                  name: `${name} - Winbox`,
                  routerIP: peerIp,
                  routerPort: winboxPort,
                  username: winboxUsername,
                  password: winboxPassword,
                });
                toasts.success(`Winbox session created for ${winboxUsername}`);
              } catch (winboxErr: any) {
                const winboxErrorMsg =
                  winboxErr.message || "Failed to create Winbox session";
                accountCreationError += `Winbox: ${winboxErrorMsg}`;
                toasts.error(`Winbox creation failed: ${winboxErrorMsg}`);
              }
            }
          }

          // Refresh sessions lists
          try {
            await websshStore.listActiveSessions();
            await winboxStore.listActiveSessions();
          } catch (e) {
            console.error("Failed to refresh sessions:", e);
          }
        } finally {
          creatingAccounts = false;
        }

        // Show warning if there were partial failures
        if (accountCreationError) {
          error = `Device created, but some sessions failed:\n${accountCreationError}`;
          isLoading = false;
          return;
        }
      }

      // Optional provisioning — server identifies peers by WireGuard public_key
      if (provisionAfterCreate && selectedSnapshotId && peer.public_key) {
        try {
          const ok = await snapshotStore.provision(peer.public_key, selectedSnapshotId);
          if (ok) toasts.success("Device provisioned from snapshot");
          else toasts.error($snapshotStore.error || "Provisioning failed (device added)");
        } catch (provErr: any) {
          toasts.error("Provisioning failed: " + (provErr.message || "unknown error"));
        }
      }

      // Close this window after successful creation
      $openedApps = $openedApps.filter((app) => app !== "AddPeer");

      // Open Onboarding Guide
      if (!isClaimMode && !$openedApps.includes("OnboardingGuide")) {
        $openedApps = [...$openedApps, "OnboardingGuide"];
        $activeThing = "OnboardingGuide";
        $appZIndexes["OnboardingGuide"] = zIndex + 1;
      } else if (isClaimMode) {
        if (!$openedApps.includes("Peers")) {
          $openedApps = [...$openedApps, "Peers"];
        }
        $activeThing = "Peers";
        bringToFront("Peers");
        toasts.success("Wantastic device claimed");
      }
    } catch (err: any) {
      error = err.message || "Failed to add Device";
      isLoading = false;
    }
  }

  function handleCancel() {
    $openedApps = $openedApps.filter((app) => app !== "AddPeer");
  }
</script>

<div
  class="add-peer activeShadow"
  style:z-index={zIndex}
  on:mousedown={handleFocus}
  on:touchstart={handleFocus}
  use:draggable={{
    handle: ".title-bar",
    bounds: "body",
  }}
  transition:scale={{ duration: 200 }}
>
  <Titlebar
    appName="AddPeer"
    canReduce={false}
    canMaximize={false}
    canClose={true}
    on:close={handleCancel}
  >
    <img src="img/icon/Peers.svg" alt="Peers" height="16" width="16" />
    <span class="appName pl-2">{isClaimMode ? "Claim Wantastic Device" : "Add New Device"}</span>
  </Titlebar>
  <div class="mainApp">
    <form class="form-content" on:submit|preventDefault={handleSubmit}>
      {#if error}
        <div class="error-message">
          <span class="error-icon"></span>
          <span>{$translateError$(error)}</span>
        </div>
      {/if}

      <div class="form-group">
        <label for="name">Device Name</label>
        <!-- svelte-ignore a11y-autofocus -->
        <TextBox
          id="name"
          type="text"
          bind:value={name}
          placeholder={isClaimMode ? "Wantastic Device" : "My Device"}
          required
          autofocus
        />
        <span class="help-text">{isClaimMode ? "Name this device before adding it to your team" : "Enter a friendly name for this device"}</span>
      </div>

      {#if !claimKeyEntryMode && !claimPublicKey.trim()}
        <div class="add-methods">
          <button type="button" class="method-option active">
            <strong>1. Standard</strong>
            <span>Create a new WireGuard device</span>
          </button>
          <button type="button" class="method-option" on:click={() => (createSshAccount = !createSshAccount)}>
            <strong>2. SSH</strong>
            <span>Add SSH access details</span>
          </button>
          <button type="button" class="method-option" on:click={() => (createWinboxAccount = !createWinboxAccount)}>
            <strong>3. Winbox</strong>
            <span>Add RouterOS access details</span>
          </button>
          <button type="button" class="method-option" on:click={() => (claimKeyEntryMode = true)}>
            <strong>4. Claim key</strong>
            <span>Claim a pre-generated device</span>
          </button>
        </div>
      {/if}

      {#if isClaimMode}
        <div class="form-group">
          <label for="claim-public-key-input">Claim public key</label>
          <TextBox
            id="claim-public-key-input"
            type="text"
            bind:value={claimPublicKey}
            placeholder="Paste the device public key"
            required={isClaimMode}
          />
          <span class="help-text">Use the public key printed by <code>wantasticd genkey</code> or embedded in the device QR.</span>
        </div>

        <div class="form-group claim-server-group">
          <label for="claim-server">Wantastic server</label>
          <TextBox
            id="claim-server"
            type="text"
            bind:value={claimServerBase}
            placeholder="https://console.wantastic.app"
          />
          <span class="help-text">Optional. Change this before claiming if the QR should be claimed on another Wantastic domain.</span>
        </div>

        {#if claimPublicKey.trim()}
        <div class="claim-key">
          <label for="claim-key">Device public key</label>
          <code id="claim-key">{claimPublicKey}</code>
        </div>
        {/if}
      {/if}

      {#if !isClaimMode}
      <div class="form-group toggle-group">
        <div class="toggle-label-row">
          <label for="create-ssh">Create SSH account</label>
          <ToggleSwitch id="create-ssh" bind:checked={createSshAccount} />
        </div>
        <div class="toggle-label-row">
          <label for="create-winbox">Create Winbox account</label>
          <ToggleSwitch id="create-winbox" bind:checked={createWinboxAccount} />
        </div>
        <span class="help-text"
          >Create sessions immediately after device is added</span
        >
      </div>
      {/if}

      {#if !isClaimMode && (createSshAccount || createWinboxAccount)}
        <div class="credentials-section">
          {#if createSshAccount}
            <div class="section-title">SSH Credentials</div>

            <div class="form-row">
              <div class="form-group">
                <label for="ssh-username">SSH Username</label>
                <TextBox
                  id="ssh-username"
                  type="text"
                  bind:value={sshUsername}
                  placeholder="root"
                  required={createSshAccount}
                />
              </div>

              <div class="form-group">
                <label for="ssh-port">SSH Port</label>
                <TextBox
                  id="ssh-port"
                  type="number"
                  bind:value={sshPort}
                  placeholder="22"
                  min="1"
                  max="65535"
                />
              </div>
            </div>

            <div class="form-group">
              <label for="ssh-password">SSH Password</label>
              <TextBox
                id="ssh-password"
                type="password"
                bind:value={sshPassword}
                placeholder="Enter password"
                required={createSshAccount}
              />
            </div>
          {/if}

          {#if createWinboxAccount}
            {#if createSshAccount}
              <div class="section-divider">Winbox Credentials</div>
            {:else}
              <div class="section-title">Winbox Credentials</div>
            {/if}

            <div class="form-row">
              <div class="form-group">
                <label for="winbox-username">Winbox Username</label>
                <TextBox
                  id="winbox-username"
                  type="text"
                  bind:value={winboxUsername}
                  placeholder="admin"
                  required={createWinboxAccount}
                />
              </div>

              <div class="form-group">
                <label for="winbox-port">Winbox Port</label>
                <TextBox
                  id="winbox-port"
                  type="number"
                  bind:value={winboxPort}
                  placeholder="8291"
                  min="1"
                  max="65535"
                />
              </div>
            </div>

            <div class="form-group">
              <label for="winbox-password">Winbox Password</label>
              <TextBox
                id="winbox-password"
                type="password"
                bind:value={winboxPassword}
                placeholder="Enter password"
                required={createWinboxAccount}
              />
            </div>
          {/if}
        </div>
      {/if}

      {#if wuspSnapshots.length > 0}
        <div class="form-group toggle-group">
          <div class="toggle-label-row">
            <label for="provision-toggle">Provision from snapshot (WUSP)</label>
            <ToggleSwitch id="provision-toggle" bind:checked={provisionAfterCreate} />
          </div>
          {#if provisionAfterCreate}
            <select bind:value={selectedSnapshotId} style="margin-top:6px; width:100%; padding:7px 10px; background:#0f1117; border:1px solid #334155; border-radius:5px; color:#e2e8f0; font-size:13px;">
              <option value="">— select snapshot —</option>
              {#each wuspSnapshots as snap}
                <option value={snap.id}>{snap.name} · {snap.manufacturer} {snap.product_class} · OS {snap.software_version}</option>
              {/each}
            </select>
            <span class="help-text">Apply snapshot configuration to device after creation</span>
          {/if}
        </div>
      {/if}

      <div class="form-footer">
        <Button
          type="button"
          variant="standard"
          class="max-h-1/2"
          on:click={handleCancel}
        >
          Cancel
        </Button>
        <Button
          type="submit"
          variant="accent"
          class="max-h-[70px]! h-full! py-0!"
          disabled={isLoading || creatingAccounts}
        >
          {#if isLoading || creatingAccounts}
            {creatingAccounts ? "Creating accounts..." : "Adding..."}
          {:else}
            {isClaimMode && normalizeClaimServer(claimServerHost) !== currentOrigin() ? "Continue on Server" : isClaimMode ? "Claim Device" : "Add Device"}
          {/if}
        </Button>
      </div>
    </form>
  </div>
</div>

<style lang="scss">
  .add-peer {
    background: var(--mica);
    position: absolute;
    top: 50%;
    left: 50%;
    transform: translate(-50%, -50%);
    border-radius: 8px;
    overflow: hidden;
    width: min(500px, 95vw);
    height: auto;
    max-height: 85vh;
  }

  .mainApp {
    padding: 0;
    position: relative;
    min-height: 200px;
    overflow-y: auto;
  }

  .form-content {
    @apply flex flex-col gap-1 p-6 justify-start bg-[rgb(var(--bg2))] max-h-[60vh] overflow-y-auto;
  }

  .error-message {
    display: flex;
    align-items: center;
    gap: 12px;
    padding: 12px 16px;
    background: rgba(239, 68, 68, 0.1);
    border: 1px solid #ef4444;
    border-radius: 6px;
    color: #ef4444;
  }

  .error-icon {
    font-size: 18px;
  }

  .form-group {
    display: flex;
    flex-direction: column;
    gap: 8px;
  }

  .form-row {
    display: flex;
    gap: 12px;

    .form-group {
      flex: 1;
    }
  }

  .add-methods {
    display: grid;
    grid-template-columns: repeat(2, minmax(0, 1fr));
    gap: 8px;
  }

  .method-option {
    display: flex;
    flex-direction: column;
    align-items: flex-start;
    gap: 4px;
    min-height: 74px;
    padding: 10px 12px;
    border: 1px solid rgba(148, 163, 184, 0.24);
    border-radius: 6px;
    background: rgba(15, 23, 42, 0.42);
    color: rgb(var(--clr));
    text-align: left;
    cursor: pointer;

    strong {
      font-size: 13px;
      font-weight: 700;
    }

    span {
      color: rgb(var(--clr) / 62%);
      font-size: 12px;
      line-height: 1.3;
    }

    &:hover,
    &.active {
      border-color: rgba(59, 130, 246, 0.65);
      background: rgba(37, 99, 235, 0.14);
    }
  }

  .toggle-group {
    gap: 4px;

    .toggle-label-row {
      display: flex;
      align-items: center;
      justify-content: space-between;
      gap: 12px;

      label {
        margin: 0;
      }
    }
  }

  .form-group label {
    font-size: 13px;
    font-weight: 600;
    color: rgb(var(--clr) / 90%);
  }

  .form-group input {
    padding: 10px 12px;
    background: rgb(var(--bg2));
    border: 1px solid rgb(var(--clr) / 20%);
    border-radius: 6px;
    color: rgb(var(--clr));
    font-size: 14px;
    transition: all 0.2s;
  }

  .form-group input:focus {
    outline: none;
    border-color: rgb(var(--clrPrm));
    box-shadow: 0 0 0 3px rgb(var(--clrPrm) / 10%);
  }

  .help-text {
    font-size: 12px;
    color: rgb(var(--clr) / 50%);
  }

  .claim-key {
    display: flex;
    flex-direction: column;
    gap: 8px;
    padding: 12px;
    background: rgb(var(--bg3));
    border: 1px solid rgb(var(--clr) / 12%);
    border-radius: 6px;

    label {
      font-size: 13px;
      font-weight: 600;
      color: rgb(var(--clr) / 90%);
    }

    code {
      display: block;
      overflow-wrap: anywhere;
      padding: 8px 10px;
      border-radius: 5px;
      background: rgb(var(--bg2));
      color: rgb(var(--clr) / 80%);
      font-size: 12px;
      line-height: 1.45;
    }
  }

  .claim-server-group :global(input) {
    font-family: ui-monospace, SFMono-Regular, Menlo, Monaco, Consolas,
      "Liberation Mono", "Courier New", monospace;
    font-size: 12px;
  }

  .credentials-section {
    display: flex;
    flex-direction: column;
    gap: 12px;
    padding: 16px;
    background: rgb(var(--bg3));
    border: 1px solid rgb(var(--clr) / 10%);
    border-radius: 6px;
    margin: 8px 0;

    .section-title {
      font-size: 13px;
      font-weight: 700;
      color: rgb(var(--clr) / 90%);
      text-transform: uppercase;
      margin-bottom: 4px;
    }

    .section-divider {
      font-size: 13px;
      font-weight: 700;
      color: rgb(var(--clr) / 90%);
      text-transform: uppercase;
      margin-top: 8px;
      margin-bottom: 4px;
    }
  }

  .form-footer {
    @apply flex justify-end gap-3 mt-4 max-h-10;
  }

  @media (prefers-color-scheme: dark) {
    .add-peer {
      background: hsla(0, 0%, 10%, 0.95);
      backdrop-filter: blur(20px);
    }
  }

  /* Mobile responsive styles */
  @media (max-width: 768px) {
    .add-peer {
      position: fixed !important;
      width: 100vw !important;
      height: calc(100vh - 48px) !important;
      left: 0 !important;
      top: 0 !important;
      right: 0 !important;
      bottom: 48px !important;
      transform: none;
      border-radius: 0;
      max-width: none;
    }

    .mainApp {
      padding: 0;
    }

    .form-content {
      padding: 20px 16px;
    }

    input {
      font-size: 16px; /* Prevent zoom on iOS */
      padding: 14px 12px;
    }

    .form-footer {
      flex-direction: column;
      gap: 10px;
    }

    .form-row {
      flex-direction: column;
      gap: 12px;
    }

    .add-methods {
      grid-template-columns: 1fr;
    }

    .toggle-group .toggle-label-row {
      flex-direction: column;
      align-items: flex-start;
    }
  }

  @media (max-width: 480px) {
    .form-content {
      padding: 16px 12px;
    }

    .form-group label {
      font-size: 13px;
    }

    input {
      padding: 12px 10px;
    }

    h2 {
      font-size: 18px;
    }

    .credentials-section {
      padding: 12px;
      gap: 10px;
    }
  }
</style>
