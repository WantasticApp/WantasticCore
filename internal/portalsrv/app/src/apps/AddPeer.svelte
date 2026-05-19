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

  onMount(async () => {
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
    if (!name.trim()) {
      error = "Device name is required";
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
      const peer = await peerStore.addPeer(name);

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
      if (!$openedApps.includes("OnboardingGuide")) {
        $openedApps = [...$openedApps, "OnboardingGuide"];
        $activeThing = "OnboardingGuide";
        $appZIndexes["OnboardingGuide"] = zIndex + 1;
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
    <span class="appName pl-2">Add New Device</span>
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
          placeholder="My Device"
          required
          autofocus
        />
        <span class="help-text">Enter a friendly name for this device</span>
      </div>

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

      {#if createSshAccount || createWinboxAccount}
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
            Add Device
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
