<script lang="ts">
  import { draggable } from "@neodrag/svelte";
  import { scale } from "svelte/transition";
  import Titlebar from "$components/shared/Titlebar.svelte";
  import { peerStore } from "$store/peer";
  import { migrationStore, getDeviceOSLabel } from "$store/migration";
  import {
    activeThing,
    appZIndexes,
    bringToFront,
    openedApps,
  } from "$store/store";
  import { onMount } from "svelte";
  import { isMobile } from "$store/ui";

  // Window state
  let isMaximized = false;
  let isMinimized = false;

  $: zIndex = $appZIndexes["MigrationForm"] || 100;

  $: if ($activeThing === "MigrationForm" && isMinimized) {
    isMinimized = false;
  }

  $: if ($activeThing === "MigrationForm") {
    bringToFront("MigrationForm");
  }

  function handleFocus() {
    $activeThing = "MigrationForm";
    bringToFront("MigrationForm");
  }

  function handleMaximize() {
    isMaximized = !isMaximized;
  }

  function handleReduce() {
    isMinimized = true;
    $activeThing = "";
  }

  function handleClose() {
    $openedApps = $openedApps.filter((app) => app !== "MigrationForm");
    $activeThing = "";
  }

  // Step state
  let currentStep = 1;
  const totalSteps = 3;

  // Form state
  let selectedPeerIds: string[] = [];
  let targetEmail = "";
  let sshUsername = "";
  let sshPassword = "";
  let showPassword = false;
  let searchQuery = "";

  $: peers = $peerStore.peers;
  $: verificationResults = $migrationStore.verificationResults;
  $: isVerifying = $migrationStore.isVerifying;
  $: error = $migrationStore.error;

  $: filteredPeers = peers.filter((peer) => {
    if (!searchQuery) return true;
    const q = searchQuery.toLowerCase();
    return (
      peer.name?.toLowerCase().includes(q) ||
      peer.assigned_ip?.toLowerCase().includes(q)
    );
  });

  $: canProceedStep1 = selectedPeerIds.length > 0;
  $: canProceedStep2 =
    targetEmail.trim() !== "" &&
    sshUsername.trim() !== "" &&
    sshPassword.trim() !== "";
  $: allVerified =
    verificationResults.length > 0 &&
    verificationResults.every((r) => r.success);

  onMount(() => {
    peerStore.listPeers();
    migrationStore.clearVerification();
  });

  function togglePeer(peerId: string) {
    if (selectedPeerIds.includes(peerId)) {
      selectedPeerIds = selectedPeerIds.filter((id) => id !== peerId);
    } else {
      selectedPeerIds = [...selectedPeerIds, peerId];
    }
  }

  function selectAllPeers() {
    selectedPeerIds = filteredPeers.map((p) => p.id);
  }

  function clearSelection() {
    selectedPeerIds = [];
  }

  function nextStep() {
    if (currentStep < totalSteps) {
      currentStep++;
    }
  }

  function prevStep() {
    if (currentStep > 1) {
      currentStep--;
    }
  }

  // User-friendly error messages
  function getErrorMessage(err: any): string {
    const msg = err?.message || String(err);

    // Connection/timeout errors
    if (msg.includes("timeout") || msg.includes("Timeout")) {
      return "Connection timed out. The device may be offline or unreachable. Please check the device is connected and try again.";
    }
    if (msg.includes("connection refused") || msg.includes("ECONNREFUSED")) {
      return "Could not connect to the device. Please verify the device is online and SSH is enabled.";
    }
    if (msg.includes("network") || msg.includes("Network")) {
      return "Network error. Please check your internet connection and try again.";
    }

    // SSH errors
    if (
      msg.includes("authentication") ||
      msg.includes("password") ||
      msg.includes("Permission denied")
    ) {
      return "SSH authentication failed. Please check your username and password are correct.";
    }
    if (msg.includes("SSH") || msg.includes("ssh")) {
      return "SSH connection failed. Please verify SSH is enabled on the device and the credentials are correct.";
    }

    // Migration errors
    if (msg.includes("not found") || msg.includes("NotFound")) {
      return "The requested resource was not found. It may have been deleted or expired.";
    }
    if (msg.includes("already") || msg.includes("exists")) {
      return "This migration already exists or the devices are already being transferred.";
    }
    if (msg.includes("expired")) {
      return "This invitation has expired. Please create a new migration request.";
    }

    // Generic errors
    if (msg.includes("Unavailable") || msg.includes("unavailable")) {
      return "Service temporarily unavailable. Please try again in a moment.";
    }
    if (msg.includes("Internal") || msg.includes("internal")) {
      return "An unexpected error occurred. Please try again or contact support if the issue persists.";
    }

    // Fallback - return a generic message
    return "An unexpected error occurred. Please try again or check your settings.";
  }

  let formError = "";

  async function verifySSH() {
    if (!canProceedStep2) return;
    formError = "";

    try {
      await migrationStore.verifySSH(selectedPeerIds, sshUsername, sshPassword);
      if ($migrationStore.verificationResults.every((r) => r.success)) {
        nextStep();
      }
    } catch (err: any) {
      console.error("SSH verification failed:", err);
      formError = getErrorMessage(err);
    }
  }

  async function createMigration() {
    formError = "";
    try {
      await migrationStore.createMigration(
        selectedPeerIds,
        targetEmail,
        sshUsername,
        sshPassword,
      );
      handleClose();
      // Refresh migration list
      await migrationStore.listMigrations(true, true, true);
    } catch (err: any) {
      console.error("Failed to create migration:", err);
      formError = getErrorMessage(err);
    }
  }

  function resetForm() {
    currentStep = 1;
    selectedPeerIds = [];
    targetEmail = "";
    sshUsername = "";
    sshPassword = "";
    showPassword = false;
    searchQuery = "";
    formError = "";
    migrationStore.clearVerification();
  }
</script>

<div
  class="migration-form-app"
  class:maximized={isMaximized}
  class:minimized={isMinimized}
  class:mobile={$isMobile}
  style="z-index: {zIndex};"
  use:draggable={{
    handle: ".title-bar",
    disabled: isMaximized || $isMobile,
    bounds: "body",
  }}
  on:mousedown={handleFocus}
  on:touchstart={handleFocus}
  transition:scale={{ duration: 150, start: 0.95 }}
>
  <Titlebar
    appName="MigrationForm"
    on:maximize={handleMaximize}
    on:reduce={handleReduce}
  >
    <div slot="icon" class="app-icon">
      <svg
        width="18"
        height="18"
        viewBox="0 0 24 24"
        fill="none"
        stroke="currentColor"
        stroke-width="2"
      >
        <path d="M12 5v14M5 12h14" stroke-linecap="round" />
      </svg>
    </div>
    <span class="app-title">New Device Transfer</span>
    <button slot="extra" class="close-btn" on:click={handleClose} title="Close">
      <svg
        width="14"
        height="14"
        viewBox="0 0 24 24"
        fill="none"
        stroke="currentColor"
        stroke-width="2"
      >
        <path d="M18 6L6 18M6 6l12 12" stroke-linecap="round" />
      </svg>
    </button>
  </Titlebar>

  <div class="content">
    <!-- Progress Steps -->
    <div class="steps">
      <div
        class="step"
        class:active={currentStep >= 1}
        class:completed={currentStep > 1}
      >
        <div class="step-number">1</div>
        <span>Select Devices</span>
      </div>
      <div class="step-line" class:active={currentStep > 1} />
      <div
        class="step"
        class:active={currentStep >= 2}
        class:completed={currentStep > 2}
      >
        <div class="step-number">2</div>
        <span>Credentials</span>
      </div>
      <div class="step-line" class:active={currentStep > 2} />
      <div class="step" class:active={currentStep >= 3}>
        <div class="step-number">3</div>
        <span>Confirm</span>
      </div>
    </div>

    <!-- Step Content -->
    <div class="step-content">
      {#if currentStep === 1}
        <!-- Step 1: Select Devices -->
        <div class="step-body">
          <div class="step-header">
            <h3>Select Devices to Transfer</h3>
            <p>Choose which devices you want to transfer to another account.</p>
          </div>

          <div class="search-bar">
            <svg
              width="16"
              height="16"
              viewBox="0 0 24 24"
              fill="none"
              stroke="currentColor"
              stroke-width="2"
            >
              <circle cx="11" cy="11" r="8" /><path
                d="M21 21l-4.35-4.35"
                stroke-linecap="round"
              />
            </svg>
            <input
              type="text"
              placeholder="Search devices..."
              bind:value={searchQuery}
            />
          </div>

          <div class="selection-actions">
            <button class="link-btn" on:click={selectAllPeers}
              >Select All</button
            >
            <span class="sep">•</span>
            <button class="link-btn" on:click={clearSelection}>Clear</button>
            <span class="selection-count"
              >{selectedPeerIds.length} selected</span
            >
          </div>

          <div class="device-list">
            {#each filteredPeers as peer (peer.id)}
              <label
                class="device-item"
                class:selected={selectedPeerIds.includes(peer.id)}
              >
                <input
                  type="checkbox"
                  checked={selectedPeerIds.includes(peer.id)}
                  on:change={() => togglePeer(peer.id)}
                />
                <div class="device-info">
                  <span class="device-name"
                    >{peer.name || "Unnamed Device"}</span
                  >
                  <span class="device-ip">{peer.assigned_ip || "No IP"}</span>
                </div>
                <div class="device-status" class:online={peer.is_online}>
                  <span class="status-dot" />
                </div>
              </label>
            {/each}
            {#if filteredPeers.length === 0}
              <div class="no-devices">
                <p>No devices found</p>
              </div>
            {/if}
          </div>
        </div>
      {:else if currentStep === 2}
        <!-- Step 2: Credentials -->
        <div class="step-body">
          <div class="step-header">
            <h3>Enter Transfer Details</h3>
            <p>
              Provide the recipient's email and SSH credentials for the devices.
            </p>
          </div>

          <form class="form" on:submit|preventDefault={verifySSH}>
            <div class="form-group">
              <label for="targetEmail">Recipient Email</label>
              <input
                id="targetEmail"
                type="email"
                placeholder="recipient@example.com"
                bind:value={targetEmail}
                required
              />
              <span class="form-hint"
                >They'll receive an invitation to accept the transfer</span
              >
            </div>

            <div class="form-divider">
              <span>SSH Credentials</span>
            </div>

            <div class="form-group">
              <label for="sshUsername">SSH Username</label>
              <input
                id="sshUsername"
                type="text"
                placeholder="root"
                bind:value={sshUsername}
                required
              />
            </div>

            <div class="form-group">
              <label for="sshPassword">SSH Password</label>
              <div class="password-input">
                {#if showPassword}
                  <input
                    id="sshPassword"
                    type="text"
                    placeholder="Enter password"
                    bind:value={sshPassword}
                    required
                  />
                {:else}
                  <input
                    id="sshPassword"
                    type="password"
                    placeholder="Enter password"
                    bind:value={sshPassword}
                    required
                  />
                {/if}
                <button
                  type="button"
                  class="toggle-password"
                  on:click={() => (showPassword = !showPassword)}
                >
                  <svg
                    width="16"
                    height="16"
                    viewBox="0 0 24 24"
                    fill="none"
                    stroke="currentColor"
                    stroke-width="2"
                  >
                    {#if showPassword}
                      <path
                        d="M17.94 17.94A10.07 10.07 0 0112 20c-7 0-11-8-11-8a18.45 18.45 0 015.06-5.94M9.9 4.24A9.12 9.12 0 0112 4c7 0 11 8 11 8a18.5 18.5 0 01-2.16 3.19m-6.72-1.07a3 3 0 11-4.24-4.24"
                      />
                      <line x1="1" y1="1" x2="23" y2="23" />
                    {:else}
                      <path d="M1 12s4-8 11-8 11 8 11 8-4 8-11 8-11-8-11-8z" />
                      <circle cx="12" cy="12" r="3" />
                    {/if}
                  </svg>
                </button>
              </div>
              <span class="form-hint"
                >Used to update WireGuard config on devices</span
              >
            </div>

            {#if formError || error}
              <div class="error-message">
                <svg
                  width="16"
                  height="16"
                  viewBox="0 0 24 24"
                  fill="none"
                  stroke="currentColor"
                  stroke-width="2"
                >
                  <circle cx="12" cy="12" r="10" /><path
                    d="M12 8v4M12 16h.01"
                  />
                </svg>
                <span>{formError || error}</span>
              </div>
            {/if}

            {#if verificationResults.length > 0}
              <div class="verification-results">
                <h4>Verification Results</h4>
                {#each verificationResults as result}
                  <div
                    class="result-item"
                    class:success={result.success}
                    class:failed={!result.success}
                  >
                    <svg
                      width="14"
                      height="14"
                      viewBox="0 0 24 24"
                      fill="none"
                      stroke="currentColor"
                      stroke-width="2"
                    >
                      {#if result.success}
                        <path
                          d="M20 6L9 17l-5-5"
                          stroke-linecap="round"
                          stroke-linejoin="round"
                        />
                      {:else}
                        <path
                          d="M18 6L6 18M6 6l12 12"
                          stroke-linecap="round"
                          stroke-linejoin="round"
                        />
                      {/if}
                    </svg>
                    <span class="result-name"
                      >{result.peer_name || result.peer_id}</span
                    >
                    {#if result.success}
                      <span class="result-os"
                        >{getDeviceOSLabel(result.detected_os)}</span
                      >
                    {:else}
                      <span class="result-error">{result.error}</span>
                    {/if}
                  </div>
                {/each}
              </div>
            {/if}
          </form>
        </div>
      {:else if currentStep === 3}
        <!-- Step 3: Confirm -->
        <div class="step-body">
          <div class="step-header">
            <h3>Confirm Transfer</h3>
            <p>Review the details before sending the transfer invitation.</p>
          </div>

          <div class="summary">
            <div class="summary-item">
              <span class="summary-label">Recipient</span>
              <span class="summary-value">{targetEmail}</span>
            </div>
            <div class="summary-item">
              <span class="summary-label">Devices</span>
              <span class="summary-value"
                >{selectedPeerIds.length} device(s)</span
              >
            </div>
            <div class="summary-devices">
              {#each selectedPeerIds as peerId}
                {@const peer = peers.find((p) => p.id === peerId)}
                {#if peer}
                  <div class="summary-device">
                    <svg
                      width="14"
                      height="14"
                      viewBox="0 0 24 24"
                      fill="none"
                      stroke="currentColor"
                      stroke-width="2"
                    >
                      <rect x="2" y="3" width="20" height="14" rx="2" ry="2" />
                      <line x1="8" y1="21" x2="16" y2="21" /><line
                        x1="12"
                        y1="17"
                        x2="12"
                        y2="21"
                      />
                    </svg>
                    <span>{peer.name || "Unnamed"}</span>
                    <span class="device-ip">{peer.assigned_ip}</span>
                  </div>
                {/if}
              {/each}
            </div>
          </div>

          <div class="info-box">
            <svg
              width="18"
              height="18"
              viewBox="0 0 24 24"
              fill="none"
              stroke="currentColor"
              stroke-width="2"
            >
              <circle cx="12" cy="12" r="10" /><path d="M12 16v-4M12 8h.01" />
            </svg>
            <div>
              <p><strong>What happens next?</strong></p>
              <p>
                The recipient will receive an email invitation. When they
                accept, the devices will be automatically reconfigured to
                connect to their account.
              </p>
            </div>
          </div>
        </div>
      {/if}
    </div>

    <!-- Footer Actions -->
    <div class="footer">
      {#if currentStep > 1}
        <button class="btn-secondary" on:click={prevStep}>
          <svg
            width="16"
            height="16"
            viewBox="0 0 24 24"
            fill="none"
            stroke="currentColor"
            stroke-width="2"
          >
            <path
              d="M15 18l-6-6 6-6"
              stroke-linecap="round"
              stroke-linejoin="round"
            />
          </svg>
          Back
        </button>
      {:else}
        <button class="btn-secondary" on:click={handleClose}>Cancel</button>
      {/if}

      <div class="spacer" />

      {#if currentStep === 1}
        <button
          class="btn-primary"
          disabled={!canProceedStep1}
          on:click={nextStep}
        >
          Continue
          <svg
            width="16"
            height="16"
            viewBox="0 0 24 24"
            fill="none"
            stroke="currentColor"
            stroke-width="2"
          >
            <path
              d="M9 18l6-6-6-6"
              stroke-linecap="round"
              stroke-linejoin="round"
            />
          </svg>
        </button>
      {:else if currentStep === 2}
        <button
          class="btn-primary"
          disabled={!canProceedStep2 || isVerifying}
          on:click={verifySSH}
        >
          {#if isVerifying}
            <div class="btn-spinner" />
            Verifying...
          {:else if allVerified}
            Continue
            <svg
              width="16"
              height="16"
              viewBox="0 0 24 24"
              fill="none"
              stroke="currentColor"
              stroke-width="2"
            >
              <path
                d="M9 18l6-6-6-6"
                stroke-linecap="round"
                stroke-linejoin="round"
              />
            </svg>
          {:else}
            Verify SSH
          {/if}
        </button>
      {:else}
        <button class="btn-primary" on:click={createMigration}>
          <svg
            width="16"
            height="16"
            viewBox="0 0 24 24"
            fill="none"
            stroke="currentColor"
            stroke-width="2"
          >
            <path
              d="M22 2L11 13M22 2l-7 20-4-9-9-4 20-7z"
              stroke-linecap="round"
              stroke-linejoin="round"
            />
          </svg>
          Send Invitation
        </button>
      {/if}
    </div>
  </div>
</div>

<style lang="scss">
  .migration-form-app {
    position: absolute;
    top: 50%;
    left: 50%;
    transform: translate(-50%, -50%);
    width: 400px;
    height: 560px;
    min-width: 320px;
    min-height: 400px;
    background: #1a1a2e;
    border-radius: 12px;
    box-shadow: 0 8px 32px rgba(0, 0, 0, 0.4);
    display: flex;
    flex-direction: column;
    overflow: hidden;
    border: 1px solid rgba(255, 255, 255, 0.08);

    &.maximized {
      top: 0;
      left: 0;
      transform: none;
      width: 100%;
      height: 100%;
      border-radius: 0;
    }

    &.minimized {
      display: none;
    }

    &.mobile {
      top: 0;
      left: 0;
      transform: none;
      width: 100%;
      height: 100%;
      border-radius: 0;
    }
  }

  .app-icon {
    display: flex;
    align-items: center;
    justify-content: center;
    color: #14b8a6;
  }

  .app-title {
    font-size: 14px;
    font-weight: 500;
    color: #e2e8f0;
    margin-left: 8px;
  }

  .close-btn {
    display: flex;
    align-items: center;
    justify-content: center;
    width: 24px;
    height: 24px;
    background: transparent;
    border: none;
    border-radius: 4px;
    color: #64748b;
    cursor: pointer;
    margin-left: auto;
    margin-right: 8px;

    &:hover {
      background: rgba(239, 68, 68, 0.2);
      color: #ef4444;
    }
  }

  .content {
    flex: 1;
    display: flex;
    flex-direction: column;
    overflow: hidden;
  }

  .steps {
    display: flex;
    align-items: center;
    justify-content: center;
    gap: 8px;
    padding: 16px 20px;
    background: rgba(0, 0, 0, 0.2);
    border-bottom: 1px solid rgba(255, 255, 255, 0.08);
  }

  .step {
    display: flex;
    align-items: center;
    gap: 6px;
    font-size: 12px;
    color: #64748b;

    &.active {
      color: #14b8a6;

      .step-number {
        background: #14b8a6;
        color: white;
      }
    }

    &.completed {
      color: #94a3b8;

      .step-number {
        background: rgba(20, 184, 166, 0.2);
        color: #14b8a6;
      }
    }
  }

  .step-number {
    width: 22px;
    height: 22px;
    display: flex;
    align-items: center;
    justify-content: center;
    background: rgba(255, 255, 255, 0.1);
    border-radius: 50%;
    font-size: 11px;
    font-weight: 600;
  }

  .step-line {
    width: 24px;
    height: 2px;
    background: rgba(255, 255, 255, 0.1);
    border-radius: 1px;

    &.active {
      background: #14b8a6;
    }
  }

  .step-content {
    flex: 1;
    overflow-y: auto;
  }

  .step-body {
    padding: 20px;
  }

  .step-header {
    margin-bottom: 20px;

    h3 {
      margin: 0 0 6px;
      font-size: 16px;
      font-weight: 600;
      color: #e2e8f0;
    }

    p {
      margin: 0;
      font-size: 13px;
      color: #64748b;
    }
  }

  .search-bar {
    display: flex;
    align-items: center;
    gap: 10px;
    padding: 10px 14px;
    background: rgba(0, 0, 0, 0.3);
    border: 1px solid rgba(255, 255, 255, 0.08);
    border-radius: 8px;
    margin-bottom: 12px;

    svg {
      color: #64748b;
      flex-shrink: 0;
    }

    input {
      flex: 1;
      background: transparent;
      border: none;
      outline: none;
      color: #e2e8f0;
      font-size: 13px;

      &::placeholder {
        color: #64748b;
      }
    }
  }

  .selection-actions {
    display: flex;
    align-items: center;
    gap: 8px;
    margin-bottom: 12px;
    font-size: 12px;

    .link-btn {
      background: none;
      border: none;
      color: #14b8a6;
      cursor: pointer;
      padding: 0;

      &:hover {
        text-decoration: underline;
      }
    }

    .sep {
      color: #64748b;
    }

    .selection-count {
      margin-left: auto;
      color: #94a3b8;
    }
  }

  .device-list {
    display: flex;
    flex-direction: column;
    gap: 6px;
    max-height: 240px;
    overflow-y: auto;
  }

  .device-item {
    display: flex;
    align-items: center;
    gap: 12px;
    padding: 10px 12px;
    background: rgba(255, 255, 255, 0.03);
    border: 1px solid rgba(255, 255, 255, 0.08);
    border-radius: 8px;
    cursor: pointer;
    transition: all 0.15s ease;

    &:hover {
      background: rgba(255, 255, 255, 0.05);
    }

    &.selected {
      background: rgba(20, 184, 166, 0.1);
      border-color: rgba(20, 184, 166, 0.3);
    }

    input[type="checkbox"] {
      width: 16px;
      height: 16px;
      accent-color: #14b8a6;
    }
  }

  .device-info {
    flex: 1;
    display: flex;
    flex-direction: column;
    gap: 2px;
  }

  .device-name {
    font-size: 13px;
    font-weight: 500;
    color: #e2e8f0;
  }

  .device-ip {
    font-size: 11px;
    color: #64748b;
  }

  .device-status {
    .status-dot {
      width: 8px;
      height: 8px;
      border-radius: 50%;
      background: #64748b;
    }

    &.online .status-dot {
      background: #22c55e;
    }
  }

  .no-devices {
    padding: 20px;
    text-align: center;
    color: #64748b;
    font-size: 13px;
  }

  .form {
    display: flex;
    flex-direction: column;
    gap: 16px;
  }

  .form-group {
    display: flex;
    flex-direction: column;
    gap: 6px;

    label {
      font-size: 12px;
      font-weight: 500;
      color: #94a3b8;
    }

    input {
      padding: 10px 12px;
      background: rgba(0, 0, 0, 0.3);
      border: 1px solid rgba(255, 255, 255, 0.08);
      border-radius: 6px;
      color: #e2e8f0;
      font-size: 13px;
      outline: none;
      transition: border-color 0.15s ease;

      &:focus {
        border-color: #14b8a6;
      }

      &::placeholder {
        color: #64748b;
      }
    }
  }

  .form-hint {
    font-size: 11px;
    color: #64748b;
  }

  .form-divider {
    display: flex;
    align-items: center;
    gap: 12px;
    margin: 8px 0;

    &::before,
    &::after {
      content: "";
      flex: 1;
      height: 1px;
      background: rgba(255, 255, 255, 0.08);
    }

    span {
      font-size: 11px;
      font-weight: 600;
      color: #64748b;
      text-transform: uppercase;
    }
  }

  .password-input {
    position: relative;

    input {
      width: 100%;
      padding-right: 40px;
    }

    .toggle-password {
      position: absolute;
      right: 10px;
      top: 50%;
      transform: translateY(-50%);
      background: none;
      border: none;
      color: #64748b;
      cursor: pointer;
      padding: 4px;

      &:hover {
        color: #94a3b8;
      }
    }
  }

  .error-message {
    display: flex;
    align-items: center;
    gap: 8px;
    padding: 10px 12px;
    background: rgba(239, 68, 68, 0.1);
    border: 1px solid rgba(239, 68, 68, 0.3);
    border-radius: 6px;
    color: #ef4444;
    font-size: 12px;
  }

  .verification-results {
    padding: 12px;
    background: rgba(0, 0, 0, 0.2);
    border-radius: 8px;

    h4 {
      margin: 0 0 10px;
      font-size: 12px;
      font-weight: 600;
      color: #94a3b8;
    }
  }

  .result-item {
    display: flex;
    align-items: center;
    gap: 8px;
    padding: 6px 0;
    font-size: 12px;

    &.success {
      color: #22c55e;
    }

    &.failed {
      color: #ef4444;
    }

    .result-name {
      color: #e2e8f0;
    }

    .result-os {
      margin-left: auto;
      color: #64748b;
    }

    .result-error {
      margin-left: auto;
      color: #ef4444;
      font-size: 11px;
    }
  }

  .summary {
    display: flex;
    flex-direction: column;
    gap: 12px;
    padding: 16px;
    background: rgba(0, 0, 0, 0.2);
    border-radius: 8px;
    margin-bottom: 16px;
  }

  .summary-item {
    display: flex;
    align-items: center;
    justify-content: space-between;

    .summary-label {
      font-size: 12px;
      color: #64748b;
    }

    .summary-value {
      font-size: 13px;
      font-weight: 500;
      color: #e2e8f0;
    }
  }

  .summary-devices {
    display: flex;
    flex-direction: column;
    gap: 6px;
    margin-top: 8px;
    padding-top: 12px;
    border-top: 1px solid rgba(255, 255, 255, 0.08);
  }

  .summary-device {
    display: flex;
    align-items: center;
    gap: 8px;
    font-size: 12px;
    color: #94a3b8;

    svg {
      color: #64748b;
    }

    .device-ip {
      margin-left: auto;
      color: #64748b;
      font-size: 11px;
    }
  }

  .info-box {
    display: flex;
    gap: 12px;
    padding: 14px;
    background: rgba(20, 184, 166, 0.08);
    border: 1px solid rgba(20, 184, 166, 0.2);
    border-radius: 8px;

    svg {
      color: #14b8a6;
      flex-shrink: 0;
      margin-top: 2px;
    }

    p {
      margin: 0;
      font-size: 12px;
      color: #94a3b8;
      line-height: 1.5;

      strong {
        color: #e2e8f0;
      }

      & + p {
        margin-top: 6px;
      }
    }
  }

  .footer {
    display: flex;
    align-items: center;
    gap: 12px;
    padding: 16px 20px;
    border-top: 1px solid rgba(255, 255, 255, 0.08);
    background: rgba(0, 0, 0, 0.2);
  }

  .spacer {
    flex: 1;
  }

  .btn-primary {
    display: flex;
    align-items: center;
    gap: 6px;
    padding: 10px 16px;
    background: #14b8a6;
    border: none;
    border-radius: 6px;
    color: white;
    font-size: 13px;
    font-weight: 500;
    cursor: pointer;
    transition: all 0.15s ease;

    &:hover:not(:disabled) {
      background: #0d9488;
    }

    &:disabled {
      opacity: 0.5;
      cursor: not-allowed;
    }
  }

  .btn-secondary {
    display: flex;
    align-items: center;
    gap: 6px;
    padding: 10px 16px;
    background: transparent;
    border: 1px solid rgba(255, 255, 255, 0.15);
    border-radius: 6px;
    color: #94a3b8;
    font-size: 13px;
    font-weight: 500;
    cursor: pointer;
    transition: all 0.15s ease;

    &:hover {
      background: rgba(255, 255, 255, 0.05);
      color: #e2e8f0;
    }
  }

  .btn-spinner {
    width: 14px;
    height: 14px;
    border: 2px solid rgba(255, 255, 255, 0.3);
    border-top-color: white;
    border-radius: 50%;
    animation: spin 0.8s linear infinite;
  }

  @keyframes spin {
    to {
      transform: rotate(360deg);
    }
  }

  // Mobile responsive
  @media (max-width: 480px) {
    .steps {
      padding: 12px 16px;
    }

    .step span {
      display: none;
    }

    .step-body {
      padding: 16px;
    }

    .footer {
      padding: 12px 16px;
    }
  }
</style>
