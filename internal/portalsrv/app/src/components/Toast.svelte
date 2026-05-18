<script lang="ts">
  import { toasts } from "$store/toast";
  import { fly, fade } from "svelte/transition";

  const icons: Record<string, string> = {
    error: "✕",
    success: "✓",
    warning: "⚠",
    info: "ℹ",
  };
</script>

<div class="toast-container">
  {#each $toasts as toast (toast.id)}
    <div
      class="toast toast-{toast.type}"
      in:fly={{ x: 300, duration: 300 }}
      out:fade={{ duration: 200 }}
    >
      <span class="toast-icon">{icons[toast.type]}</span>
      <span class="toast-message">{toast.message}</span>
      <button class="toast-close" on:click={() => toasts.remove(toast.id)}
        >×</button
      >
    </div>
  {/each}
</div>

<style>
  .toast-container {
    position: fixed;
    top: 16px;
    right: 16px;
    z-index: 100000;
    display: flex;
    flex-direction: column;
    gap: 8px;
    max-width: 400px;
    pointer-events: none;
  }

  .toast {
    display: flex;
    align-items: center;
    gap: 12px;
    padding: 12px 16px;
    border-radius: 8px;
    background: #333;
    color: white;
    box-shadow: 0 4px 12px rgba(0, 0, 0, 0.3);
    font-size: 14px;
    pointer-events: auto;
    backdrop-filter: blur(10px);
  }

  .toast-error {
    background: linear-gradient(135deg, #dc3545 0%, #c82333 100%);
    border-left: 4px solid #a71d2a;
  }

  .toast-success {
    background: linear-gradient(135deg, #28a745 0%, #218838 100%);
    border-left: 4px solid #1e7e34;
  }

  .toast-warning {
    background: linear-gradient(135deg, #ffc107 0%, #e0a800 100%);
    color: #000;
    border-left: 4px solid #d39e00;
  }

  .toast-info {
    background: linear-gradient(135deg, #17a2b8 0%, #138496 100%);
    border-left: 4px solid #117a8b;
  }

  .toast-icon {
    font-size: 18px;
    font-weight: bold;
    flex-shrink: 0;
  }

  .toast-message {
    flex: 1;
    word-break: break-word;
  }

  .toast-close {
    background: none;
    border: none;
    color: inherit;
    font-size: 20px;
    cursor: pointer;
    opacity: 0.7;
    padding: 0;
    line-height: 1;
    flex-shrink: 0;
  }

  .toast-close:hover {
    opacity: 1;
  }
</style>
