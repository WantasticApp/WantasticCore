<script lang="ts">
  import { winboxStore, winboxApi } from "../store/winbox";
  import { ApiError } from "../lib/errors";
  import { translateError$ } from "../store/i18n";
  import { onMount, onDestroy } from "svelte";
  import type { Peer } from "../store/peer";

  export let peer: Peer;
  export let onClose: () => void;

  let isConnecting = false;
  let isConnected = false;
  let error = "";
  let iframeContainer: HTMLDivElement;
  let proxyUrl = "";

  // Winbox connection settings
  let routerIp = peer.ip_address;
  let routerPort = 8291;
  let username = "admin";
  let password = "";
  let name = "";

  async function connect() {
    isConnecting = true;
    error = "";

    try {
      // Create Winbox proxy session via API
      const response = await winboxStore.createProxy(peer.id, {
        routerIP: routerIp,
        routerPort,
        name,
        username,
        password,
      });

      proxyUrl = response.proxyUrl;
      isConnected = true;
      isConnecting = false;
    } catch (err) {
      isConnecting = false;
    }
  }

  function disconnect() {
    if (proxyUrl) {
      winboxApi.closeProxy(proxyUrl).catch(console.error);
    }
    isConnected = false;
    proxyUrl = "";
  }

  function handleIframeLoad() {
    // console.log(' Winbox proxy loaded');
  }

  function handleIframeError() {
    error = "Failed to load Winbox interface";
  }

  onDestroy(() => {
    disconnect();
  });
</script>

<div
  class="winbox-modal-overlay"
  on:click={onClose}
  on:keydown={(e) => e.key === "Escape" && onClose()}
  role="presentation"
>
  <div
    class="winbox-modal-content"
    on:click|stopPropagation
    on:keydown={() => {}}
    role="dialog"
    aria-label="Winbox proxy"
  >
    <div class="winbox-header">
      <div class="winbox-title">
        <h2>MikroTik Winbox</h2>
        <p class="winbox-peer">{peer.name}</p>
      </div>
      <button on:click={onClose} class="winbox-close">✕</button>
    </div>

    {#if error}
      <div class="winbox-error">
        <span>{$translateError$(error)}</span>
        <button on:click={() => (error = "")}>✕</button>
      </div>
    {/if}

    {#if !isConnected}
      <div class="winbox-form">
        <div class="form-group">
          <label for="router-ip">Router IP Address</label>
          <input
            id="router-ip"
            type="text"
            bind:value={routerIp}
            placeholder="192.168.1.1"
            class="form-input"
          />
        </div>

        <div class="form-row">
          <div class="form-group">
            <label for="router-port">Port</label>
            <input
              id="router-port"
              type="number"
              bind:value={routerPort}
              placeholder="8291"
              class="form-input"
            />
          </div>
          <div class="form-group">
            <label for="winbox-name">Name</label>
            <input
              id="winbox-name"
              type="text"
              bind:value={name}
              placeholder="My Winbox"
              class="form-input"
            />
          </div>
        </div>

        <div class="form-group">
          <label for="winbox-username">Username</label>
          <input
            id="winbox-username"
            type="text"
            bind:value={username}
            placeholder="admin"
            class="form-input"
          />
        </div>

        <div class="form-group">
          <label for="winbox-password">Password</label>
          <input
            id="winbox-password"
            type="password"
            bind:value={password}
            placeholder="••••••••"
            class="form-input"
          />
        </div>

        <div class="form-actions">
          <button
            on:click={connect}
            disabled={isConnecting}
            class="form-button"
          >
            {isConnecting ? "Connecting..." : "Connect"}
          </button>
          <button on:click={onClose} class="form-button secondary"
            >Cancel</button
          >
        </div>
      </div>
    {:else}
      <div class="winbox-container" bind:this={iframeContainer}>
        <iframe
          src={proxyUrl}
          title="Winbox proxy"
          on:load={handleIframeLoad}
          on:error={handleIframeError}
          class="winbox-iframe"
        />
      </div>

      <div class="winbox-footer">
        <button on:click={disconnect} class="winbox-button disconnect"
          >Disconnect</button
        >
      </div>
    {/if}
  </div>
</div>

<style>
  .winbox-modal-overlay {
    position: fixed;
    top: 0;
    left: 0;
    right: 0;
    bottom: 0;
    background: rgba(0, 0, 0, 0.9);
    display: flex;
    align-items: center;
    justify-content: center;
    z-index: 1002;
  }

  .winbox-modal-content {
    background: #1e1e1e;
    border: 1px solid #404040;
    border-radius: 8px;
    width: 90%;
    max-width: 1200px;
    height: 80vh;
    display: flex;
    flex-direction: column;
    box-shadow: 0 10px 40px rgba(0, 0, 0, 0.8);
  }

  .winbox-header {
    padding: 16px 20px;
    border-bottom: 1px solid #404040;
    display: flex;
    justify-content: space-between;
    align-items: center;
    gap: 16px;
  }

  .winbox-title h2 {
    margin: 0;
    font-size: 16px;
    color: #e0e0e0;
  }

  .winbox-peer {
    margin: 4px 0 0 0;
    font-size: 12px;
    color: #a0a0a0;
  }

  .winbox-close {
    background: none;
    border: none;
    color: #a0a0a0;
    font-size: 18px;
    cursor: pointer;
    padding: 0;
  }

  .winbox-close:hover {
    color: #e0e0e0;
  }

  .winbox-error {
    background: #3f2020;
    border-bottom: 1px solid #6b4040;
    padding: 12px 20px;
    display: flex;
    justify-content: space-between;
    align-items: center;
    color: #ffb3b3;
    font-size: 12px;
  }

  .winbox-error button {
    background: none;
    border: none;
    color: #ffb3b3;
    cursor: pointer;
    font-size: 16px;
  }

  .winbox-form {
    padding: 24px;
    flex: 1;
    overflow-y: auto;
  }

  .form-group {
    margin-bottom: 16px;
    display: flex;
    flex-direction: column;
    gap: 6px;
  }

  .form-group label {
    font-size: 12px;
    font-weight: 500;
    color: #a0a0a0;
  }

  .form-row {
    display: grid;
    grid-template-columns: 1fr 1fr;
    gap: 16px;
  }

  .form-input {
    padding: 8px 12px;
    background: #2d2d2d;
    border: 1px solid #404040;
    border-radius: 4px;
    color: #e0e0e0;
    font-size: 12px;
  }

  .form-input:focus {
    outline: none;
    border-color: #0d7bff;
    box-shadow: 0 0 0 2px rgba(13, 123, 255, 0.1);
  }

  .form-actions {
    display: flex;
    gap: 8px;
    margin-top: 24px;
  }

  .form-button {
    padding: 8px 16px;
    background: linear-gradient(180deg, #0d7bff 0%, #0a5bcc 100%);
    border: none;
    border-radius: 4px;
    color: #fff;
    font-size: 12px;
    cursor: pointer;
    transition: all 0.2s;
  }

  .form-button:hover:not(:disabled) {
    background: linear-gradient(180deg, #1e87ff 0%, #1570e0 100%);
    box-shadow: 0 4px 12px rgba(13, 123, 255, 0.3);
  }

  .form-button:disabled {
    opacity: 0.5;
    cursor: not-allowed;
  }

  .form-button.secondary {
    background: #2d2d2d;
    border: 1px solid #404040;
    color: #a0a0a0;
  }

  .form-button.secondary:hover {
    background: #3d3d3d;
    border-color: #505050;
    color: #e0e0e0;
  }

  .winbox-container {
    flex: 1;
    overflow: hidden;
    background: #0a0e27;
  }

  .winbox-iframe {
    width: 100%;
    height: 100%;
    border: none;
    background: #fff;
  }

  .winbox-footer {
    padding: 12px 20px;
    border-top: 1px solid #404040;
    background: #1e1e1e;
    display: flex;
    justify-content: flex-end;
  }

  .winbox-button {
    padding: 6px 12px;
    background: #d32f2f;
    border: none;
    border-radius: 4px;
    color: #fff;
    font-size: 12px;
    cursor: pointer;
    transition: all 0.2s;
  }

  .winbox-button:hover {
    background: #f32c1e;
  }

  .winbox-button.disconnect {
    background: #d32f2f;
  }

  @media (max-width: 768px) {
    .winbox-modal-content {
      width: 95%;
      height: 90vh;
    }

    .form-row {
      grid-template-columns: 1fr;
    }

    .form-actions {
      flex-direction: column;
    }
  }
</style>
