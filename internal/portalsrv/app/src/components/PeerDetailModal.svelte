<script lang="ts">
  import { peerStore, type Peer } from "../store/peer";
  import { ApiError } from "../lib/errors";
  import { translateError$ } from "../store/i18n";
  import { toasts } from "../store/toast";
  import { activeThing, openedApps, bringToFront } from "../store/store";
  import { IconButton } from "fluent-svelte";
  import { FileText } from "$components/icons";

  export let peer: Peer;
  export let onClose: () => void;

  let isLoading = false;
  let error = "";
  let config = "";
  let showQRCode = false;
  let tagsInput = (peer.tags || []).join(", ");
  let isSavingTags = false;

  async function loadConfig() {
    isLoading = true;
    error = "";
    try {
      const cfg = await peerStore.getPeerConfig(peer.id);
      config = cfg.config || "";
    } catch (err: any) {
      error = err.message || "Failed to load peer config";
    } finally {
      isLoading = false;
    }
  }

  async function downloadConfig() {
    if (!config) await loadConfig();
    const element = document.createElement("a");
    element.setAttribute(
      "href",
      "data:text/plain;charset=utf-8," + encodeURIComponent(config),
    );
    element.setAttribute("download", `${peer.name}.conf`);
    element.style.display = "none";
    document.body.appendChild(element);
    element.click();
    document.body.removeChild(element);
  }

  async function loadQRCode() {
    isLoading = true;
    error = "";
    try {
      showQRCode = true;
    } catch (err) {
      error = err instanceof ApiError ? err.message : "Failed to load QR code";
    } finally {
      isLoading = false;
    }
  }

  async function saveTags() {
    isSavingTags = true;
    error = "";
    try {
      // Parse comma-separated tags, trim whitespace, filter empty values
      const tags = tagsInput
        .split(",")
        .map((t) => t.trim())
        .filter((t) => t.length > 0);

      await peerStore.updatePeer(peer.id, peer.name, tags);

      // Update the local peer object with new tags
      peer.tags = tags;

      toasts.success("Tags saved successfully");
    } catch (err: any) {
      error = err.message || "Failed to save tags";
    } finally {
      isSavingTags = false;
    }
  }

  function viewNotes() {
    peerStore.setSelectedPeer(peer);
    if (!$openedApps.includes("PeerNotes")) {
      $openedApps = [...$openedApps, "PeerNotes"];
    }
    $activeThing = "PeerNotes";
    bringToFront("PeerNotes");
    onClose();
  }
</script>

<div
  class="modal-overlay"
  on:click={onClose}
  on:keydown={(e) => e.key === "Escape" && onClose()}
  role="presentation"
>
  <!-- svelte-ignore a11y-click-events-have-key-events -->
  <div
    class="modal-content"
    on:click|stopPropagation
    role="dialog"
    aria-label="Peer details"
  >
    <div class="modal-header">
      <h2>{peer.name}</h2>
      <button class="close-button" on:click={onClose}>✕</button>
    </div>

    {#if error}
      <div class="error-banner">
        <span>{$translateError$(error)}</span>
        <button on:click={() => (error = "")}>✕</button>
      </div>
    {/if}

    <div class="modal-body">
      <div class="peer-details">
        <div class="detail-row">
          <span class="label">Peer ID:</span>
          <span class="value">{peer.id}</span>
        </div>
        <div class="detail-row">
          <span class="label">IP Address:</span>
          <span class="value">{peer.ip_address}</span>
        </div>
        <div class="detail-row">
          <span class="label">Created:</span>
          <span class="value"
            >{new Date(peer.created_at).toLocaleDateString()}</span
          >
        </div>
        <div class="detail-row">
          <span class="label">Transfer RX:</span>
          <span class="value"
            >{(peer.transfer_rx / 1024 / 1024).toFixed(2)} MB</span
          >
        </div>
        <div class="detail-row">
          <span class="label">Transfer TX:</span>
          <span class="value"
            >{(peer.transfer_tx / 1024 / 1024).toFixed(2)} MB</span
          >
        </div>
        {#if peer.last_handshake}
          <div class="detail-row">
            <span class="label">Last Handshake:</span>
            <span class="value"
              >{new Date(peer.last_handshake).toLocaleString()}</span
            >
          </div>
        {/if}
        <div class="detail-row input-row">
          <span class="label">Tags:</span>
          <div class="tags-input-container">
            <input
              type="text"
              bind:value={tagsInput}
              placeholder="e.g. server, london, production"
              class="tags-input"
            />
            <button
              class="save-btn"
              disabled={isSavingTags}
              on:click={saveTags}
            >
              {isSavingTags ? "Saving..." : "Save"}
            </button>
            <div title="View Notes">
              <IconButton on:click={viewNotes}>
                <FileText size={16} />
              </IconButton>
            </div>
          </div>
        </div>
      </div>

      {#if showQRCode}
        <div class="qr-section">
          <h3>QR Code</h3>
          <p>Scan this QR code with WireGuard app to connect:</p>
          <!-- QR code will be loaded here -->
        </div>
      {/if}

      {#if config}
        <div class="config-section">
          <h3>Configuration File</h3>
          <pre class="config-display">{config}</pre>
        </div>
      {/if}
    </div>

    <div class="modal-footer">
      {#if isLoading}
        <button class="action-button" disabled>Loading...</button>
      {:else if !config && !showQRCode}
        <button class="action-button secondary" on:click={loadConfig}
          >View Config</button
        >
        <button class="action-button secondary" on:click={loadQRCode}
          >View QR Code</button
        >
      {:else}
        {#if config}
          <button class="action-button" on:click={downloadConfig}
            >Download Config</button
          >
        {/if}
        <button class="action-button secondary" on:click={onClose}>Close</button
        >
      {/if}
    </div>
  </div>
</div>

<style>
  .modal-overlay {
    position: fixed;
    top: 0;
    left: 0;
    right: 0;
    bottom: 0;
    background: rgba(0, 0, 0, 0.8);
    display: flex;
    align-items: center;
    justify-content: center;
    z-index: 1000;
  }

  .modal-content {
    background: #1e1e1e;
    border: 1px solid #404040;
    border-radius: 8px;
    max-width: 600px;
    width: 90%;
    max-height: 80vh;
    overflow-y: auto;
    box-shadow: 0 10px 40px rgba(0, 0, 0, 0.5);
  }

  .modal-header {
    display: flex;
    justify-content: space-between;
    align-items: center;
    padding: 20px;
    border-bottom: 1px solid #404040;
  }

  .modal-header h2 {
    margin: 0;
    font-size: 20px;
  }

  .close-button {
    background: none;
    border: none;
    color: #a0a0a0;
    font-size: 20px;
    cursor: pointer;
    padding: 0;
  }

  .close-button:hover {
    color: #e0e0e0;
  }

  .error-banner {
    background: #3f2020;
    border-bottom: 1px solid #6b4040;
    padding: 12px 20px;
    display: flex;
    justify-content: space-between;
    align-items: center;
    color: #ffb3b3;
    font-size: 13px;
  }

  .error-banner button {
    background: none;
    border: none;
    color: #ffb3b3;
    cursor: pointer;
    font-size: 16px;
  }

  .modal-body {
    padding: 20px;
  }

  .peer-details {
    margin-bottom: 20px;
  }

  .detail-row {
    display: flex;
    justify-content: space-between;
    padding: 8px 0;
    border-bottom: 1px solid #2d2d2d;
  }

  .label {
    color: #a0a0a0;
    font-size: 13px;
    font-weight: 500;
  }

  .value {
    color: #e0e0e0;
    font-size: 13px;
    font-family: monospace;
    word-break: break-all;
  }

  .qr-section,
  .config-section {
    margin: 20px 0;
    padding: 16px;
    background: #2d2d2d;
    border-radius: 4px;
  }

  .qr-section h3,
  .config-section h3 {
    margin: 0 0 8px 0;
    font-size: 13px;
    color: #e0e0e0;
  }

  .qr-section p {
    margin: 0 0 12px 0;
    color: #a0a0a0;
    font-size: 12px;
  }

  .config-display {
    background: #1e1e1e;
    border: 1px solid #404040;
    padding: 12px;
    border-radius: 4px;
    overflow-x: auto;
    font-size: 11px;
    color: #0d7bff;
    margin: 0;
  }

  .input-row {
    align-items: center;
  }

  .tags-input-container {
    display: flex;
    gap: 8px;
    flex: 1;
    justify-content: flex-end;
  }

  .tags-input {
    background: #2d2d2d;
    border: 1px solid #404040;
    color: #e0e0e0;
    padding: 4px 8px;
    border-radius: 4px;
    font-size: 13px;
    width: 200px;
  }

  .tags-input:focus {
    outline: none;
    border-color: #0d7bff;
  }

  .save-btn {
    background: #2d2d2d;
    border: 1px solid #404040;
    color: #a0a0a0;
    padding: 4px 8px;
    border-radius: 4px;
    font-size: 12px;
    cursor: pointer;
  }

  .save-btn:hover:not(:disabled) {
    background: #3d3d3d;
    color: #fff;
  }

  .modal-footer {
    padding: 20px;
    border-top: 1px solid #404040;
    display: flex;
    gap: 12px;
    justify-content: flex-end;
  }

  .action-button {
    padding: 8px 16px;
    background: linear-gradient(180deg, #0d7bff 0%, #0a5bcc 100%);
    border: none;
    border-radius: 4px;
    color: #fff;
    font-size: 13px;
    cursor: pointer;
    transition: all 0.2s;
  }

  .action-button:hover:not(:disabled) {
    background: linear-gradient(180deg, #1e87ff 0%, #1570e0 100%);
    box-shadow: 0 4px 12px rgba(13, 123, 255, 0.3);
  }

  .action-button:disabled {
    opacity: 0.5;
    cursor: not-allowed;
  }

  .action-button.secondary {
    background: #2d2d2d;
    border: 1px solid #404040;
    color: #a0a0a0;
  }

  .action-button.secondary:hover {
    background: #3d3d3d;
    border-color: #505050;
    color: #e0e0e0;
  }

</style>
