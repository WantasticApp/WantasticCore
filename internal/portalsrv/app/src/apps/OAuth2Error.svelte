<script lang="ts">
  import { onMount } from "svelte";
  import { _, setLanguage } from "../store/i18n";
  import NetworkBackground from "$components/NetworkBackground.svelte";

  export let errorCode = "";
  export let errorDescription = "";

  let title = "";
  let message = "";
  let i18nReady = false;

  function buildMessages() {
    switch (errorCode) {
      case "access_denied":
        title = $_("oauth2Error.accessDenied");
        message = errorDescription || $_("oauth2Error.accessDeniedDesc");
        break;
      case "invalid_request":
        title = $_("oauth2Error.invalidRequest");
        message = errorDescription || $_("oauth2Error.unknownError");
        break;
      case "server_error":
        title = $_("oauth2Error.serverError");
        message = errorDescription || $_("oauth2Error.unknownError");
        break;
      case "expired_token":
        title = $_("oauth2Error.expiredToken");
        message = errorDescription || $_("oauth2Error.unknownError");
        break;
      case "unsupported_grant_type":
        title = $_("oauth2Error.unsupportedGrantType");
        message = errorDescription || $_("oauth2Error.unknownError");
        break;
      default:
        title = $_("oauth2Error.title");
        message = errorDescription || $_("oauth2Error.unknownError");
    }
  }

  $: if (i18nReady) buildMessages();

  onMount(async () => {
    // Read error from URL if not passed as props (standalone error page)
    if (!errorCode) {
      const params = new URLSearchParams(window.location.search);
      errorCode = params.get("error") ?? "";
      errorDescription = params.get("error_description") ?? "";
    }

    const browserLang = (navigator.languages?.[0] ?? navigator.language).split("-")[0].toLowerCase();
    await setLanguage(browserLang);
    i18nReady = true;
    buildMessages();
  });
</script>

<NetworkBackground />

<div class="page">
  <div class="card glass">

    <div class="logo">{$_("oauth2.title")}</div>

    <div class="error-banner">
      <div class="warning-icon">
        <svg width="22" height="22" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.5">
          <path d="M10.29 3.86L1.82 18a2 2 0 0 0 1.71 3h16.94a2 2 0 0 0 1.71-3L13.71 3.86a2 2 0 0 0-3.42 0z" />
          <line x1="12" y1="9" x2="12" y2="13" />
          <line x1="12" y1="17" x2="12.01" y2="17" />
        </svg>
      </div>
      <div>
        <div class="error-title">{i18nReady ? title : $_("oauth2Error.title")}</div>
        {#if message}
          <div class="error-msg">{message}</div>
        {/if}
      </div>
    </div>

    {#if errorCode}
      <div class="error-code-row">
        <span class="error-code-badge">{errorCode}</span>
      </div>
    {/if}

    <div class="actions">
      <a href="/#desktop" class="btn-primary">
        {$_("oauth2Error.backToPortal")}
      </a>
      <a href="/#login" class="btn-secondary">
        {$_("oauth2Error.tryAgain")}
      </a>
    </div>

  </div>
</div>

<style>
  .page {
    min-height: 100dvh;
    display: flex;
    align-items: center;
    justify-content: center;
    padding: 1rem;
  }

  .card {
    width: 100%;
    max-width: 380px;
    background: rgba(var(--bg3), 0.85);
    backdrop-filter: blur(24px);
    border: 1px solid rgba(var(--clr), 0.08);
    border-radius: 16px;
    padding: 2rem;
    display: flex;
    flex-direction: column;
    gap: 1.25rem;
  }

  .logo {
    font-size: 1.25rem;
    font-weight: 700;
    color: rgb(var(--clrPrm));
  }

  .error-banner {
    display: flex;
    align-items: flex-start;
    gap: 0.875rem;
    background: rgba(239, 68, 68, 0.07);
    border: 1px solid rgba(239, 68, 68, 0.2);
    border-radius: 12px;
    padding: 1rem 1.125rem;
  }

  .warning-icon {
    flex-shrink: 0;
    color: #f87171;
    margin-top: 1px;
  }

  .error-title {
    font-weight: 700;
    font-size: 0.95rem;
    color: #fca5a5;
    margin-bottom: 0.3rem;
  }

  .error-msg {
    font-size: 0.83rem;
    color: rgba(var(--clr), 0.65);
    line-height: 1.45;
  }

  .error-code-row {
    display: flex;
  }

  .error-code-badge {
    font-family: monospace;
    font-size: 0.72rem;
    background: rgba(var(--clr), 0.06);
    border: 1px solid rgba(var(--clr), 0.1);
    border-radius: 4px;
    padding: 0.2rem 0.5rem;
    color: rgba(var(--clr), 0.45);
  }

  .actions {
    display: flex;
    flex-direction: column;
    gap: 0.625rem;
  }

  .btn-primary {
    display: block;
    text-align: center;
    padding: 0.72rem;
    background: rgb(var(--clrPrm));
    color: #fff;
    border: none;
    border-radius: 8px;
    font-size: 0.875rem;
    font-weight: 600;
    cursor: pointer;
    text-decoration: none;
    transition: opacity 0.15s;
  }

  .btn-primary:hover { opacity: 0.88; }

  .btn-secondary {
    display: block;
    text-align: center;
    padding: 0.65rem;
    background: transparent;
    border: 1px solid rgba(var(--clr), 0.12);
    border-radius: 8px;
    color: rgba(var(--clr), 0.6);
    font-size: 0.875rem;
    font-weight: 500;
    cursor: pointer;
    text-decoration: none;
    transition: all 0.15s;
  }

  .btn-secondary:hover {
    background: rgba(var(--clr), 0.05);
    color: rgb(var(--clr));
  }
</style>
