<script lang="ts">
  import { draggable } from "@neodrag/svelte";
  import { scale } from "svelte/transition";
  import Titlebar from "$components/shared/Titlebar.svelte";
  import { topologyStore } from "$store/topology";
  import { wsStore } from "$store/websocket";
  import {
    openedApps,
    activeThing,
    appZIndexes,
    bringToFront,
  } from "$store/store";
  import { translateError$ } from "$store/i18n";
  import { onMount } from "svelte";

  // Z-index for window stacking
  $: zIndex = $appZIndexes["CreateGroupLink"] || 100;

  function handleFocus() {
    $activeThing = "CreateGroupLink";
    bringToFront("CreateGroupLink");
  }

  // Get selected peers from store
  $: selectedPeers = $topologyStore.selectedPeersForGroupLink;

  // Form state
  let groupName = "";
  let isLoading = false;
  let error = "";

  let selectedServices = {
    icmp: true,
    allTcp: true,
    allUdp: true,
    ssh: true,
    dns: true,
    winbox: true,
  };

  async function handleSubmit() {
    if (!groupName.trim()) {
      error = "Please enter a group name";
      return;
    }

    if (selectedPeers.length < 2) {
      error = "At least 2 peers are required";
      return;
    }

    isLoading = true;
    error = "";

    try {
      // Collect selected services
      const protocols: number[] = [];
      const ports: string[] = [];

      if (selectedServices.icmp) protocols.push(1);
      if (selectedServices.allTcp) protocols.push(6);
      if (selectedServices.allUdp) protocols.push(17);

      if (selectedServices.ssh) {
        protocols.push(6);
        ports.push("22");
      }
      if (selectedServices.dns) {
        protocols.push(17);
        ports.push("53");
      }
      if (selectedServices.winbox) {
        protocols.push(6);
        ports.push("8291");
      }

      // If no services selected, allow all
      if (protocols.length === 0) {
        protocols.push(6, 17, 1);
      }

      // Add peers to group
      for (const peer of selectedPeers) {
        await wsStore.callGRPC("TenantPortalService", "AddTenantPeerToGroup", {
          peer_id: peer.id,
          group_id: groupName,
        });
      }

      // Create bidirectional link
      await wsStore.callGRPC("TenantPortalService", "CreateTenantGroupLink", {
        source_group_id: groupName,
        target_group_id: groupName,
        allowed_protocols: protocols,
        allowed_ports: ports,
      });

      // Close and clean up
      handleClose();

      // Refresh topology
      await topologyStore.refresh();
    } catch (err: any) {
      error = err.message || "Failed to create group link";
      isLoading = false;
    }
  }

  function handleClose() {
    topologyStore.clearSelectedPeersForGroupLink();
    $openedApps = $openedApps.filter((app) => app !== "CreateGroupLink");
    if ($activeThing === "CreateGroupLink") {
      $activeThing = "";
    }
  }
</script>

<div
  class="create-group-link activeShadow"
  style:z-index={zIndex}
  on:mousedown={handleFocus}
  on:touchstart={handleFocus}
  use:draggable={{
    handle: ".title-bar",
    bounds: "body",
  }}
  transition:scale={{ duration: 200 }}
>
  <Titlebar appName="CreateGroupLink" canReduce={false} canMaximize={false}>
    <svg
      xmlns="http://www.w3.org/2000/svg"
      width="16"
      height="16"
      viewBox="0 0 24 24"
      fill="none"
      stroke="currentColor"
      stroke-width="2"
    >
      <path d="M10 13a5 5 0 0 0 7.54.54l3-3a5 5 0 0 0-7.07-7.07l-1.72 1.71" />
      <path d="M14 11a5 5 0 0 0-7.54-.54l-3 3a5 5 0 0 0 7.07 7.07l1.71-1.71" />
    </svg>
    <span class="appName pl-2">Create Group Link</span>
  </Titlebar>
  <div class="mainApp">
    <form class="form-content" on:submit|preventDefault={handleSubmit}>
      {#if error}
        <div class="error-message">
          <span class="error-icon"></span>
          <span>{$translateError$(error)}</span>
        </div>
      {/if}

      <!-- Selected Peers Card -->
      <div class="card">
        <div class="card-header">
          <h3 class="card-title">
            <svg
              width="16"
              height="16"
              viewBox="0 0 24 24"
              fill="none"
              stroke="currentColor"
              stroke-width="2"
            >
              <path d="M17 21v-2a4 4 0 0 0-4-4H5a4 4 0 0 0-4 4v2" />
              <circle cx="9" cy="7" r="4" />
              <path d="M23 21v-2a4 4 0 0 0-3-3.87" />
              <path d="M16 3.13a4 4 0 0 1 0 7.75" />
            </svg>
            Selected Peers ({selectedPeers.length})
          </h3>
        </div>
        <div class="card-body">
          <div class="peer-list">
            {#each selectedPeers as peer}
              <div class="peer-item">• {peer.label} ({peer.ip})</div>
            {/each}
          </div>
          <p class="info-text">
            These peers will form a mesh group with bidirectional connectivity
          </p>
        </div>
      </div>

      <!-- Group Name -->
      <div class="form-group">
        <label for="group-name">Group Name</label>
        <!-- svelte-ignore a11y-autofocus -->
        <input
          id="group-name"
          type="text"
          bind:value={groupName}
          placeholder="e.g., office-servers"
          required
          autofocus
        />
        <span class="help-text">Enter a unique name for this peer group</span>
      </div>

      <!-- Services Selection Card -->
      <div class="card">
        <div class="card-header">
          <h3 class="card-title">
            <svg
              width="16"
              height="16"
              viewBox="0 0 24 24"
              fill="none"
              stroke="currentColor"
              stroke-width="2"
            >
              <rect x="3" y="11" width="18" height="11" rx="2" />
              <path d="M7 11V7a5 5 0 0 1 10 0v4" />
            </svg>
            Allowed Services
          </h3>
        </div>
        <div class="card-body">
          <p class="section-hint">
            Select services allowed between peers. If none selected, all traffic
            is allowed.
          </p>

          <div class="services-grid">
            <label class="service-card">
              <input type="checkbox" bind:checked={selectedServices.icmp} />
              <svg
                class="service-icon"
                width="20"
                height="20"
                viewBox="0 0 24 24"
                fill="none"
                stroke="currentColor"
                stroke-width="1.5"
              >
                <circle cx="12" cy="12" r="10" />
                <path d="M12 6v6l4 2" />
              </svg>
              <span class="service-name">ICMP</span>
            </label>

            <label class="service-card">
              <input type="checkbox" bind:checked={selectedServices.allTcp} />
              <svg
                class="service-icon"
                width="20"
                height="20"
                viewBox="0 0 24 24"
                fill="none"
                stroke="currentColor"
                stroke-width="1.5"
              >
                <circle cx="12" cy="12" r="10" />
                <path d="M2 12h20" />
                <path
                  d="M12 2a15.3 15.3 0 0 1 4 10 15.3 15.3 0 0 1-4 10 15.3 15.3 0 0 1-4-10 15.3 15.3 0 0 1 4-10z"
                />
              </svg>
              <span class="service-name">All TCP</span>
            </label>

            <label class="service-card">
              <input type="checkbox" bind:checked={selectedServices.allUdp} />
              <svg
                class="service-icon"
                width="20"
                height="20"
                viewBox="0 0 24 24"
                fill="none"
                stroke="currentColor"
                stroke-width="1.5"
              >
                <path d="M22 12h-4l-3 9L9 3l-3 9H2" />
              </svg>
              <span class="service-name">All UDP</span>
            </label>

            <label class="service-card">
              <input type="checkbox" bind:checked={selectedServices.ssh} />
              <svg
                class="service-icon"
                width="20"
                height="20"
                viewBox="0 0 24 24"
                fill="none"
                stroke="currentColor"
                stroke-width="1.5"
              >
                <rect x="3" y="11" width="18" height="11" rx="2" ry="2" />
                <path d="M7 11V7a5 5 0 0 1 10 0v4" />
              </svg>
              <span class="service-name">SSH</span>
            </label>

            <label class="service-card">
              <input type="checkbox" bind:checked={selectedServices.dns} />
              <svg
                class="service-icon"
                width="20"
                height="20"
                viewBox="0 0 24 24"
                fill="none"
                stroke="currentColor"
                stroke-width="1.5"
              >
                <circle cx="12" cy="12" r="10" />
                <path d="M12 6v12" />
                <path d="M6 12h12" />
              </svg>
              <span class="service-name">DNS</span>
            </label>

            <label class="service-card">
              <input type="checkbox" bind:checked={selectedServices.winbox} />
              <svg
                class="service-icon"
                width="20"
                height="20"
                viewBox="0 0 24 24"
                fill="none"
                stroke="currentColor"
                stroke-width="1.5"
              >
                <rect x="2" y="3" width="20" height="14" rx="2" ry="2" />
                <path d="M8 21h8" />
                <path d="M12 17v4" />
                <path d="M7 8h2" />
                <path d="M7 11h4" />
              </svg>
              <span class="service-name">Winbox</span>
            </label>
          </div>
        </div>
      </div>
    </form>

    <div class="form-footer">
      <button type="button" class="btn-secondary" on:click={handleClose}>
        Cancel
      </button>
      <button
        type="submit"
        class="btn-primary"
        disabled={isLoading || selectedPeers.length < 2}
        on:click={handleSubmit}
      >
        {#if isLoading}
          Creating...
        {:else}
          Create Link
        {/if}
      </button>
    </div>
  </div>
</div>

<style lang="scss">
  .create-group-link {
    background: var(--mica);
    position: absolute;
    top: 10%;
    left: 25%;
    border-radius: 8px;
    overflow: hidden;
    width: 560px;
    max-width: 90vw;
    max-height: 85vh;
    display: flex;
    flex-direction: column;
    box-shadow: 0 24px 48px rgba(0, 0, 0, 0.3);
  }

  .mainApp {
    display: flex;
    flex-direction: column;
    padding: 0;
    overflow-y: auto;
    flex: 1;
  }

  .form-content {
    padding: 16px;
    display: flex;
    flex-direction: column;
    gap: 16px;
  }

  .error-message {
    display: flex;
    align-items: center;
    gap: 10px;
    padding: 10px 14px;
    background: rgba(239, 68, 68, 0.12);
    border: 1px solid rgba(239, 68, 68, 0.25);
    border-radius: 6px;
    color: #ef4444;
    font-size: 12px;
  }

  .error-icon {
    font-size: 16px;
    flex-shrink: 0;
  }

  /* Card styling matching Account.svelte */
  .card {
    background: rgb(var(--bg2) / 60%);
    border: 1px solid rgb(var(--clr) / 8%);
    border-radius: 8px;
    overflow: hidden;
  }

  .card-header {
    padding: 12px 14px;
    border-bottom: 1px solid rgb(var(--clr) / 8%);
    background: rgb(var(--bg3) / 30%);
  }

  .card-title {
    margin: 0;
    font-size: 13px;
    font-weight: 600;
    color: rgb(var(--clr));
    display: flex;
    align-items: center;
    gap: 8px;
  }

  .card-title svg {
    color: rgb(var(--clrPrm));
    flex-shrink: 0;
  }

  .card-body {
    padding: 14px;
  }

  .peer-list {
    max-height: 100px;
    overflow-y: auto;
    margin-bottom: 10px;
    padding: 8px 10px;
    background: rgb(var(--bg1));
    border: 1px solid rgb(var(--clr) / 10%);
    border-radius: 4px;
  }

  .peer-item {
    font-size: 11px;
    padding: 3px 0;
    font-family: "Cascadia Code", "Consolas", monospace;
    color: rgb(var(--clr) / 90%);
  }

  .info-text {
    font-size: 11px;
    color: rgb(var(--clr) / 60%);
    margin: 0;
    font-style: italic;
  }

  .form-group {
    margin-bottom: 0;
  }

  .form-group label {
    display: block;
    font-size: 12px;
    font-weight: 500;
    color: rgb(var(--clr) / 70%);
    margin-bottom: 6px;
  }

  .form-group input[type="text"] {
    width: 100%;
    padding: 10px 12px;
    background: rgb(var(--bg1));
    border: 1px solid rgb(var(--clr) / 15%);
    border-radius: 6px;
    color: rgb(var(--clr));
    font-size: 14px;
    transition: all 0.2s;
  }

  .form-group input[type="text"]:focus {
    outline: none;
    border-color: rgb(var(--clrPrm));
    box-shadow: 0 0 0 3px rgb(var(--clrPrm) / 10%);
  }

  .form-group input[type="text"]::placeholder {
    color: rgb(var(--clr) / 40%);
  }

  .help-text {
    display: block;
    font-size: 11px;
    color: rgb(var(--clr) / 50%);
    margin-top: 4px;
  }

  .section-label {
    font-size: 12px;
    font-weight: 500;
    color: rgb(var(--clr) / 70%);
    margin-bottom: 6px;
  }

  .section-hint {
    font-size: 11px;
    color: rgb(var(--clr) / 50%);
    margin: 0 0 10px 0;
  }

  .services-grid {
    display: grid;
    grid-template-columns: repeat(auto-fill, minmax(100px, 1fr));
    gap: 8px;
  }

  .service-card {
    background: rgb(var(--bg1));
    border: 1px solid rgb(var(--clr) / 12%);
    border-radius: 6px;
    padding: 12px 8px;
    cursor: pointer;
    transition: all 0.2s;
    display: flex;
    flex-direction: column;
    align-items: center;
    gap: 8px;
    text-align: center;

    &:hover {
      border-color: rgb(var(--clrPrm) / 40%);
      background: rgb(var(--clr) / 3%);

      .service-icon {
        color: rgb(var(--clrPrm) / 80%);
      }
    }

    input[type="checkbox"] {
      display: none;
    }

    &:has(input:checked) {
      border-color: rgb(var(--clrPrm));
      background: rgb(var(--clrPrm) / 12%);

      .service-icon {
        color: rgb(var(--clrPrm));
        transform: scale(1.1);
      }

      .service-name {
        color: rgb(var(--clrPrm));
        font-weight: 600;
      }
    }
  }

  .service-icon {
    color: rgb(var(--clr) / 60%);
    transition: all 0.2s;
    flex-shrink: 0;
  }

  .service-name {
    font-size: 11px;
    font-weight: 500;
    color: rgb(var(--clr) / 80%);
    transition: all 0.2s;
  }

  .form-footer {
    display: flex;
    justify-content: flex-end;
    gap: 10px;
    padding: 12px 16px;
    background: rgb(var(--bg2));
    border-top: 1px solid rgb(var(--clr) / 8%);
  }

  .btn-secondary {
    padding: 8px 16px;
    background: rgb(var(--clr) / 10%);
    border: 1px solid rgb(var(--clr) / 20%);
    border-radius: 6px;
    color: rgb(var(--clr));
    font-size: 13px;
    font-weight: 500;
    cursor: pointer;
    transition: all 0.2s;

    &:hover {
      background: rgb(var(--clr) / 15%);
    }
  }

  .btn-primary {
    padding: 8px 16px;
    background: rgb(var(--clrPrm));
    border: none;
    border-radius: 6px;
    color: white;
    font-size: 13px;
    font-weight: 600;
    cursor: pointer;
    transition: all 0.2s;

    &:hover:not(:disabled) {
      background: rgb(var(--clrPrm) / 85%);
      transform: translateY(-1px);
    }

    &:disabled {
      opacity: 0.6;
      cursor: not-allowed;
    }
  }

  /* Scrollbar */
  .mainApp::-webkit-scrollbar,
  .peer-list::-webkit-scrollbar {
    width: 6px;
  }

  .mainApp::-webkit-scrollbar-track,
  .peer-list::-webkit-scrollbar-track {
    background: transparent;
  }

  .mainApp::-webkit-scrollbar-thumb,
  .peer-list::-webkit-scrollbar-thumb {
    background: rgb(var(--clr) / 20%);
    border-radius: 3px;

    &:hover {
      background: rgb(var(--clr) / 30%);
    }
  }

  /* Responsive */
  @media (max-width: 768px) {
    .create-group-link {
      top: 0;
      left: 0;
      width: 100vw;
      max-width: 100vw;
      height: calc(100vh - 48px);
      max-height: calc(100vh - 48px);
      border-radius: 0;
    }

    .form-content {
      padding: 12px;
      gap: 12px;
    }

    .services-grid {
      grid-template-columns: repeat(3, 1fr);
      gap: 6px;
    }

    .service-card {
      padding: 8px 6px;
    }

    .service-icon {
      font-size: 18px;
    }

    .service-name {
      font-size: 10px;
    }

    input {
      font-size: 16px; /* Prevent zoom on iOS */
    }
  }

  @media (max-width: 480px) {
    .services-grid {
      grid-template-columns: repeat(2, 1fr);
    }

    .form-footer {
      flex-direction: column;
      gap: 8px;
    }

    .form-footer button {
      width: 100%;
    }
  }
</style>
