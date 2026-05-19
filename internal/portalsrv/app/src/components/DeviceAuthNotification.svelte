<script lang="ts">
  import { wsStore } from "../store/websocket";
  import { _ } from "../store/i18n";
  import { fade, fly } from "svelte/transition";

  $: requests = $wsStore.deviceAuthRequests;

  function authorize(request: any) {
    // Navigate to the Svelte activation page within the SPA.
    // Put user_code in query string (before hash) so the Activate component can read it.
    const url = new URL(window.location.href);
    url.search = `?user_code=${encodeURIComponent(request.user_code)}`;
    url.hash = "#activate";
    window.location.href = url.toString();
    wsStore.clearDeviceAuthRequest(request.user_code);
  }

  function dismiss(request: any) {
    wsStore.clearDeviceAuthRequest(request.user_code);
  }
</script>

<div class="notifications-container">
  {#each requests as request (request.user_code)}
    <div
      class="notification glass popup"
      in:fly={{ y: 20, duration: 400 }}
      out:fade={{ duration: 200 }}
    >
      <div class="icon-pulse">
        <svg
          width="20"
          height="20"
          viewBox="0 0 24 24"
          fill="none"
          stroke="currentColor"
          stroke-width="2.5"
        >
          <path d="M18 8A6 6 0 0 0 6 8c0 7-3 9-3 9h18s-3-2-3-9" />
          <path d="M13.73 21a2 2 0 0 1-3.46 0" />
        </svg>
      </div>

      <div class="content">
        <h4>{$_("auth.deviceAuthorization")}</h4>
        <p>
          {$_("auth.deviceRequestingLink", {
            values: { deviceId: request.device_id.substring(0, 8) },
          })}
        </p>

        <div class="actions">
          <button class="btn-authorize" on:click={() => authorize(request)}>
            {$_("auth.authorizeNow")}
          </button>
          <button class="btn-dismiss" on:click={() => dismiss(request)}>
            {$_("auth.dismiss")}
          </button>
        </div>
      </div>

      <button class="close-btn" on:click={() => dismiss(request)}>
        <svg
          width="14"
          height="14"
          viewBox="0 0 24 24"
          fill="none"
          stroke="currentColor"
          stroke-width="2"
        >
          <path d="M18 6L6 18M6 6l12 12" />
        </svg>
      </button>
    </div>
  {/each}
</div>

<style>
  .notifications-container {
    position: fixed;
    bottom: 64px; /* Above taskbar */
    right: 20px;
    z-index: 10000;
    display: flex;
    flex-direction: column;
    gap: 12px;
    pointer-events: none;
  }

  .notification {
    pointer-events: auto;
    width: 340px;
    background: rgba(var(--bg3), 0.8);
    backdrop-filter: blur(20px);
    border: 1px solid rgba(var(--primary), 0.2);
    border-radius: 16px;
    padding: 20px;
    box-shadow: 0 12px 32px rgba(0, 0, 0, 0.3);
    display: flex;
    gap: 16px;
    position: relative;
    overflow: hidden;
  }

  .notification::before {
    content: "";
    position: absolute;
    top: 0;
    left: 0;
    width: 4px;
    height: 100%;
    background: var(--primary);
  }

  .icon-pulse {
    flex-shrink: 0;
    width: 36px;
    height: 36px;
    background: rgba(var(--primary), 0.1);
    color: var(--primary);
    border-radius: 10px;
    display: flex;
    align-items: center;
    justify-content: center;
    animation: pulse 2s infinite;
  }

  .content h4 {
    margin: 0 0 4px;
    font-size: 14px;
    font-weight: 700;
  }

  .content p {
    margin: 0 0 16px;
    font-size: 12px;
    color: rgba(var(--clr), 0.6);
    line-height: 1.4;
  }

  .actions {
    display: flex;
    gap: 8px;
  }

  button {
    cursor: pointer;
    border: none;
    font-weight: 600;
    font-size: 11px;
    padding: 6px 12px;
    border-radius: 6px;
    transition: all 0.2s;
  }

  .btn-authorize {
    background: var(--primary);
    color: white;
  }

  .btn-authorize:hover {
    background: rgba(var(--primary), 0.8);
    transform: translateY(-1px);
  }

  .btn-dismiss {
    background: rgba(var(--clr), 0.05);
    color: rgba(var(--clr), 0.6);
    border: 1px solid rgba(var(--clr), 0.1);
  }

  .btn-dismiss:hover {
    background: rgba(var(--clr), 0.1);
    color: rgba(var(--clr), 1);
  }

  .close-btn {
    position: absolute;
    top: 12px;
    right: 12px;
    background: transparent;
    color: rgba(var(--clr), 0.3);
    padding: 4px;
  }

  .close-btn:hover {
    color: rgba(var(--clr), 1);
  }

  @keyframes pulse {
    0% {
      box-shadow: 0 0 0 0 rgba(var(--primary), 0.4);
    }
    70% {
      box-shadow: 0 0 0 10px rgba(var(--primary), 0);
    }
    100% {
      box-shadow: 0 0 0 0 rgba(var(--primary), 0);
    }
  }

  .popup {
    animation: slideIn 0.3s ease-out;
  }

  @keyframes slideIn {
    from {
      transform: translateX(100%);
      opacity: 0;
    }
    to {
      transform: translateX(0);
      opacity: 1;
    }
  }
</style>
