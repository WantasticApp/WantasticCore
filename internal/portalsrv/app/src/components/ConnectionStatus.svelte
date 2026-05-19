<script lang="ts">
  import { wsStore } from "../store/websocket";

  $: status = $wsStore.status;
  $: message = $wsStore.message;
</script>

<div
  class="connection-status"
  class:connected={status === "connected"}
  class:connecting={status === "connecting"}
  class:error={status === "error" || status === "disconnected"}
>
  <span class="status-dot" />
  <span class="status-text">
    {#if status === "connected"}
      Connected
    {:else if status === "connecting"}
      Connecting...
    {:else if status === "error"}
      {message || "Connection error"}
    {:else}
      Disconnected
    {/if}
  </span>
</div>

<style>
  .connection-status {
    display: inline-flex;
    align-items: center;
    gap: 6px;
    padding: 4px 10px;
    border-radius: 12px;
    font-size: 11px;
    font-weight: 500;
    background: #2d2d2d;
    border: 1px solid #404040;
    transition: all 0.3s ease;
  }

  .status-dot {
    width: 6px;
    height: 6px;
    border-radius: 50%;
    background: #666;
    animation: pulse 2s ease-in-out infinite;
  }

  .connection-status.connected {
    background: rgba(16, 185, 129, 0.1);
    border-color: rgba(16, 185, 129, 0.3);
  }

  .connection-status.connected .status-dot {
    background: #10b981;
    animation: none;
  }

  .connection-status.connected .status-text {
    color: #10b981;
  }

  .connection-status.connecting .status-dot {
    background: #f59e0b;
  }

  .connection-status.connecting .status-text {
    color: #f59e0b;
  }

  .connection-status.error {
    background: rgba(239, 68, 68, 0.1);
    border-color: rgba(239, 68, 68, 0.3);
  }

  .connection-status.error .status-dot {
    background: #ef4444;
    animation: none;
  }

  .connection-status.error .status-text {
    color: #ef4444;
  }

  .status-text {
    color: #a0a0a0;
    white-space: nowrap;
  }

  @keyframes pulse {
    0%,
    100% {
      opacity: 1;
      transform: scale(1);
    }
    50% {
      opacity: 0.5;
      transform: scale(0.8);
    }
  }
</style>
