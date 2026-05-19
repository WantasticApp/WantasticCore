<script lang="ts">
  import { onMount } from "svelte";
  import { Button, ProgressRing, TextBox, InfoBar } from "fluent-svelte";
  import { _, setLanguage } from "../store/i18n";
  import { authStore } from "../store/auth";
  import { wsStore } from "../store/websocket";
  import { WS_URL } from "../config";
  import NetworkBackground from "$components/NetworkBackground.svelte";

  // ── State ─────────────────────────────────────────────────────────────────
  type Stage = "loading" | "enter-code" | "confirm" | "success" | "denied" | "error";
  let stage: Stage = "loading";

  let userCode = "";
  let userCodeInput = "";
  let deviceInfo: { user_code: string; expires_at: number; status: string } | null = null;

  let isSubmitting = false;
  let errorMsg = "";
  let sessionReady = false;

  // ── Helpers ───────────────────────────────────────────────────────────────
  function getUserCodeFromURL(): string {
    const params = new URLSearchParams(window.location.search);
    return params.get("user_code") ?? "";
  }

  function formatExpiresIn(epochSec: number): string {
    const secs = Math.max(0, epochSec - Math.floor(Date.now() / 1000));
    if (secs <= 0) return $_("activate.expired");
    const m = Math.floor(secs / 60);
    const s = secs % 60;
    const timeStr = m > 0 ? `${m}m ${s}s` : `${s}s`;
    return $_("activate.expiresIn", { values: { time: timeStr } });
  }

  async function fetchDeviceInfo(code: string) {
    try {
      const res = await fetch(`/api/oauth/pending-device?user_code=${encodeURIComponent(code)}`);
      if (!res.ok) {
        if (res.status === 404) {
          errorMsg = $_("activate.invalidCode");
          stage = "error";
        } else {
          errorMsg = $_("oauth2Error.serverError");
          stage = "error";
        }
        return false;
      }
      deviceInfo = await res.json();
      return true;
    } catch {
      errorMsg = $_("oauth2Error.serverError");
      stage = "error";
      return false;
    }
  }

  async function handleCodeSubmit() {
    if (!userCodeInput.trim()) return;
    userCode = userCodeInput.trim().toUpperCase();
    isSubmitting = true;
    errorMsg = "";
    const ok = await fetchDeviceInfo(userCode);
    isSubmitting = false;
    if (ok) stage = "confirm";
  }

  async function handleApprove() {
    isSubmitting = true;
    errorMsg = "";
    try {
      const body = new URLSearchParams({ user_code: userCode });
      const res = await fetch("/api/oauth/approve", { method: "POST", body });
      if (res.status === 401) {
        // Session expired mid-flow — redirect to login with return URL
        goToLogin();
        return;
      }
      const data = await res.json();
      if (data.success) {
        stage = "success";
      } else {
        errorMsg = $_("oauth2Error.serverError");
        stage = "error";
      }
    } catch {
      errorMsg = $_("oauth2Error.serverError");
      stage = "error";
    } finally {
      isSubmitting = false;
    }
  }

  async function handleDeny() {
    isSubmitting = true;
    errorMsg = "";
    try {
      const body = new URLSearchParams({ user_code: userCode });
      const res = await fetch("/api/oauth/deny", { method: "POST", body });
      if (res.status === 401) {
        goToLogin();
        return;
      }
      if (res.ok) {
        stage = "denied";
      } else {
        errorMsg = $_("oauth2Error.serverError");
        stage = "error";
      }
    } catch {
      errorMsg = $_("oauth2Error.serverError");
      stage = "error";
    } finally {
      isSubmitting = false;
    }
  }

  function goToPortal() {
    // Strip the ?user_code= query param before navigating to desktop
    window.history.replaceState({}, "", "/#desktop");
    window.location.hash = "#desktop";
    window.location.reload(); // Ensure we reload so the new session state is picked up
  }

  function goToLogin() {
    const returnUrl = `/?user_code=${encodeURIComponent(userCode)}#activate`;
    sessionStorage.setItem("returnUrl", returnUrl);
    window.location.hash = "#login";
  }

  /** Lightweight session check via the session cookie HTTP endpoint. */
  async function checkSessionViaCookie(): Promise<boolean> {
    try {
      const res = await fetch("/api/session", { method: "GET", credentials: "same-origin" });
      return res.ok;
    } catch {
      return false;
    }
  }

  /** Wait for WS + auth store, with a short timeout. */
  async function waitForAuth(timeoutMs = 4000): Promise<boolean> {
    wsStore.connect(WS_URL);

    return new Promise<boolean>((resolve) => {
      const unsub = wsStore.subscribe((state) => {
        if (state.status === "connected" && state.encryptionReady) {
          unsub();
          authStore.checkSession().then((valid) => resolve(valid));
        } else if (state.status === "error") {
          unsub();
          resolve(false);
        }
      });
      setTimeout(() => { unsub(); resolve(false); }, timeoutMs);
    });
  }

  // ── Init ──────────────────────────────────────────────────────────────────
  onMount(async () => {
    const browserLang = (navigator.languages?.[0] ?? navigator.language).split("-")[0].toLowerCase();
    await setLanguage(browserLang);

    const codeFromURL = getUserCodeFromURL();
    if (codeFromURL) {
      userCode = codeFromURL;
      userCodeInput = codeFromURL;
    }

    // Check session via HTTP cookie first (fast, no WebSocket dependency)
    const hasCookie = await checkSessionViaCookie();

    if (hasCookie) {
      sessionReady = true;
      // Also kick off the full WS auth in background so the auth store populates
      waitForAuth().then((ok) => { if (ok) sessionReady = true; });
    } else {
      // No cookie — try the full WS path (might have a different session mechanism)
      const wsAuth = await waitForAuth(5000);
      sessionReady = wsAuth;
    }

    if (!sessionReady) {
      // Not authenticated — show login prompt
      stage = "enter-code";
      return;
    }

    // Authenticated — fetch device info if we have a code
    if (codeFromURL) {
      const ok = await fetchDeviceInfo(codeFromURL);
      if (ok) stage = "confirm";
    } else {
      stage = "enter-code";
    }
  });

  $: isAuthenticated = sessionReady || $authStore.user !== null;
</script>

<NetworkBackground />

<div class="page">
  <div class="card">

    <!-- Loading (shown before i18n is initialized, so use hardcoded text) -->
    {#if stage === "loading"}
      <div class="center-block">
        <ProgressRing size={32} />
      </div>

    <!-- Code entry / login gate -->
    {:else if stage === "enter-code"}
      <div class="icon-header">
        <svg width="40" height="40" viewBox="0 0 24 24" fill="none">
          <path d="M12 2C6.48 2 2 6.48 2 12s4.48 10 10 10 10-4.48 10-10S17.52 2 12 2zm-1 17.93c-3.95-.49-7-3.85-7-7.93 0-.62.08-1.21.21-1.79L9 15v1c0 1.1.9 2 2 2v1.93zm6.9-2.54c-.26-.81-1-1.39-1.9-1.39h-1v-3c0-.55-.45-1-1-1H8v-2h2c.55 0 1-.45 1-1V7h2c1.1 0 2-.9 2-2v-.41c2.93 1.19 5 4.06 5 7.41 0 2.08-.8 3.97-2.1 5.39z" fill="currentColor" opacity="0.7"/>
        </svg>
      </div>
      <h1 class="title">{$_("activate.heading")}</h1>

      {#if !isAuthenticated}
        <InfoBar severity="caution" title={$_("activate.loginRequired")} />
        <Button variant="accent" on:click={goToLogin}>
          {$_("activate.signInFirst")}
        </Button>
      {:else}
        <p class="desc">{$_("activate.description")}</p>
        <div class="field-group">
          <TextBox
            bind:value={userCodeInput}
            placeholder={$_("activate.enterCodePlaceholder")}
            disabled={isSubmitting}
            on:input={() => {
              userCodeInput = userCodeInput.toUpperCase().replace(/[^A-Z0-9-]/g, "");
            }}
          />
        </div>
        {#if errorMsg}
          <InfoBar severity="critical" title={errorMsg} closable on:close={() => (errorMsg = "")} />
        {/if}
        <Button variant="accent" disabled={isSubmitting || !userCodeInput.trim()} on:click={handleCodeSubmit}>
          {#if isSubmitting}
            <ProgressRing size={16} />
          {/if}
          {isSubmitting ? $_("common.loading") : $_("activate.approveDevice")}
        </Button>
      {/if}

    <!-- Confirm -->
    {:else if stage === "confirm" && deviceInfo}
      <div class="icon-header">
        <svg width="40" height="40" viewBox="0 0 24 24" fill="none">
          <path d="M12 2C6.48 2 2 6.48 2 12s4.48 10 10 10 10-4.48 10-10S17.52 2 12 2zm-1 17.93c-3.95-.49-7-3.85-7-7.93 0-.62.08-1.21.21-1.79L9 15v1c0 1.1.9 2 2 2v1.93zm6.9-2.54c-.26-.81-1-1.39-1.9-1.39h-1v-3c0-.55-.45-1-1-1H8v-2h2c.55 0 1-.45 1-1V7h2c1.1 0 2-.9 2-2v-.41c2.93 1.19 5 4.06 5 7.41 0 2.08-.8 3.97-2.1 5.39z" fill="currentColor" opacity="0.7"/>
        </svg>
      </div>

      <div class="device-card">
        <div class="device-meta">
          <span class="label">{$_("activate.deviceCode")}</span>
          <code class="device-id-code">{deviceInfo.user_code}</code>
        </div>
        <div class="expires muted">{formatExpiresIn(deviceInfo.expires_at)}</div>
      </div>

      <p class="desc">{$_("activate.description")}</p>

      {#if errorMsg}
        <InfoBar severity="critical" title={errorMsg} closable on:close={() => (errorMsg = "")} />
      {/if}

      <div class="btn-row">
        <Button disabled={isSubmitting} on:click={handleDeny}>
          {isSubmitting ? $_("activate.denying") : $_("activate.denyDevice")}
        </Button>
        <Button variant="accent" disabled={isSubmitting} on:click={handleApprove} class="flex1">
          {#if isSubmitting}
            <ProgressRing size={16} />
          {/if}
          {isSubmitting ? $_("activate.approving") : $_("activate.approveDevice")}
        </Button>
      </div>

    <!-- Success -->
    {:else if stage === "success"}
      <div class="center-block">
        <div class="result-icon success-icon">
          <svg width="28" height="28" viewBox="0 0 24 24" fill="none">
            <path d="M9 16.17L4.83 12l-1.42 1.41L9 19 21 7l-1.41-1.41L9 16.17z" fill="currentColor"/>
          </svg>
        </div>
        <h2 class="result-title success-text">{$_("activate.approved")}</h2>
        <p class="muted">{$_("activate.approvedDesc")}</p>
      </div>
      <Button variant="accent" on:click={goToPortal}>
        {$_("activate.backToPortal")}
      </Button>

    <!-- Denied -->
    {:else if stage === "denied"}
      <div class="center-block">
        <div class="result-icon denied-icon">
          <svg width="28" height="28" viewBox="0 0 24 24" fill="none">
            <path d="M19 6.41L17.59 5 12 10.59 6.41 5 5 6.41 10.59 12 5 17.59 6.41 19 12 13.41 17.59 19 19 17.59 13.41 12 19 6.41z" fill="currentColor"/>
          </svg>
        </div>
        <h2 class="result-title denied-text">{$_("activate.denied")}</h2>
        <p class="muted">{$_("activate.deniedDesc")}</p>
      </div>
      <Button variant="accent" on:click={goToPortal}>
        {$_("activate.backToPortal")}
      </Button>

    <!-- Error -->
    {:else if stage === "error"}
      <InfoBar severity="critical" title={errorMsg || $_("oauth2Error.unknownError")} />
      <Button variant="accent" on:click={goToPortal}>
        {$_("activate.backToPortal")}
      </Button>
    {/if}

  </div>
</div>

<style>
  .page {
    min-height: 100dvh;
    display: flex;
    align-items: center;
    justify-content: center;
    padding: 1rem;
    position: relative;
    z-index: 1;
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
    gap: 1rem;
  }

  .icon-header {
    display: flex;
    justify-content: center;
    color: rgb(var(--clrPrm));
    opacity: 0.8;
  }

  .title {
    font-size: 1rem;
    font-weight: 700;
    margin: 0;
    text-align: center;
  }

  .desc {
    font-size: 0.85rem;
    color: rgba(var(--clr), 0.65);
    margin: 0;
    line-height: 1.5;
    text-align: center;
  }

  .field-group {
    display: flex;
    flex-direction: column;
    gap: 0.5rem;
  }

  .field-group :global(.text-box) {
    font-family: monospace;
    font-size: 1.1rem;
    letter-spacing: 0.12em;
    text-align: center;
  }

  .device-card {
    display: flex;
    align-items: center;
    justify-content: space-between;
    background: rgba(var(--clrPrm), 0.07);
    border: 1px solid rgba(var(--clrPrm), 0.15);
    border-radius: 10px;
    padding: 0.875rem 1rem;
  }

  .device-meta {
    display: flex;
    flex-direction: column;
    gap: 2px;
  }

  .label {
    font-size: 0.72rem;
    font-weight: 600;
    color: rgba(var(--clr), 0.55);
    text-transform: uppercase;
    letter-spacing: 0.05em;
  }

  .device-id-code {
    font-family: monospace;
    font-size: 0.85rem;
    color: rgb(var(--clrPrm));
  }

  .expires {
    font-size: 0.75rem;
    text-align: right;
  }

  .btn-row {
    display: flex;
    gap: 0.75rem;
  }

  .btn-row :global(button) {
    flex: 1;
  }

  .center-block {
    display: flex;
    flex-direction: column;
    align-items: center;
    gap: 0.75rem;
    padding: 0.5rem 0;
  }

  .result-icon {
    width: 52px;
    height: 52px;
    border-radius: 50%;
    display: flex;
    align-items: center;
    justify-content: center;
  }

  .success-icon {
    background: rgba(74, 222, 128, 0.12);
    border: 2px solid rgba(74, 222, 128, 0.35);
    color: #4ade80;
  }

  .denied-icon {
    background: rgba(239, 68, 68, 0.1);
    border: 2px solid rgba(239, 68, 68, 0.3);
    color: #f87171;
  }

  .result-title {
    font-size: 1rem;
    font-weight: 700;
    margin: 0;
  }

  .success-text { color: #4ade80; }
  .denied-text { color: #f87171; }

  .muted {
    color: rgba(var(--clr), 0.55);
    font-size: 0.83rem;
    text-align: center;
  }

  .flex1 { flex: 1; }
</style>
