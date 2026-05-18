<script lang="ts">
  import { onMount } from "svelte";
  import { _, setLanguage } from "../store/i18n";
  import NetworkBackground from "$components/NetworkBackground.svelte";

  // ── State ─────────────────────────────────────────────────────────────────
  type Stage = "loading" | "login" | "consent" | "success" | "error";
  let stage: Stage = "loading";

  let authCode = "";
  let consentInfo: {
    client_id: string;
    scope: string;
    device_id: string;
    code: string;
    expires_in: number;
    authenticated: boolean;
    user_email: string;
  } | null = null;

  // Login form
  let email = "";
  let password = "";
  let loginLoading = false;
  let loginError = "";

  // Consent
  let consentLoading = false;
  let errorCode = "";
  let errorDescription = "";

  // ── Helpers ───────────────────────────────────────────────────────────────
  function getCodeFromURL(): string {
    const params = new URLSearchParams(window.location.search);
    return params.get("code") ?? "";
  }

  async function loadConsentInfo(code: string) {
    try {
      const res = await fetch(`/api/oauth/consent-info?code=${encodeURIComponent(code)}`);
      if (!res.ok) { stage = "error"; errorCode = "invalid_request"; return; }
      consentInfo = await res.json();
      stage = consentInfo!.authenticated ? "consent" : "login";
    } catch {
      stage = "error"; errorCode = "server_error";
    }
  }

  /** Human-readable label + icon for a scope string. */
  function scopeInfo(scope: string): { label: string; icon: string }[] {
    const map: Record<string, { label: string; icon: string }> = {
      "org:create_api_key": { label: $_("oauth2.scopeCreateApiKey"),  icon: "key"    },
      "user:profile":       { label: $_("oauth2.scopeUserProfile"),   icon: "person" },
      "device:register":    { label: $_("oauth2.scopeDeviceRegister"), icon: "chip"  },
    };
    return scope.split(" ").filter(Boolean).map(s => map[s] ?? { label: s, icon: "dot" });
  }

  function errorMessage(code: string): string {
    switch (code) {
      case "access_denied":              return $_("oauth2Error.accessDenied");
      case "invalid_request":            return $_("oauth2Error.invalidRequest");
      case "server_error":               return $_("oauth2Error.serverError");
      case "expired_token":              return $_("oauth2Error.expiredToken");
      case "invalid_authorization_code": return $_("oauth2.invalidCode");
      case "authorization_failed":       return $_("oauth2.authorizationFailed");
      case "session_expired":
      case "unauthenticated":            return $_("oauth2.sessionExpired");
      case "csrf_missing":
      case "csrf_mismatch":              return $_("oauth2.csrfError");
      default:                           return $_("oauth2Error.unknownError");
    }
  }

  /** Derive a short display name from a client_id like "wantastic_cipher_v_1_0_0". */
  function clientDisplayName(id: string): string {
    return id.replace(/_/g, " ").replace(/\s+v\s+[\d\s_]+$/i, "").trim() || id;
  }

  /** First letter of client_id, uppercased, for the avatar. */
  function clientInitial(id: string): string {
    return (id[0] ?? "A").toUpperCase();
  }

  // ── Login ─────────────────────────────────────────────────────────────────
  async function handleLogin() {
    if (!email || !password) return;
    loginLoading = true; loginError = "";
    try {
      const res = await fetch("/api/oauth/consent-login", {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ auth_code: authCode, email, password }),
      });
      const data = await res.json();
      if (data.success && data.authenticated) {
        // Server returns consent info inline — no second round-trip needed.
        consentInfo = {
          client_id:     data.client_id,
          scope:         data.scope,
          device_id:     data.device_id,
          code:          data.code,
          expires_in:    data.expires_in,
          authenticated: true,
          user_email:    data.user_email,
        };
        stage = "consent";
      } else if (data.success) {
        // Older server build — fall back to re-fetching consent info.
        await loadConsentInfo(authCode);
      } else {
        loginError = data.error === "invalid_credentials"
          ? $_("oauth2.loginFailed")
          : errorMessage(data.error ?? "server_error");
      }
    } catch { loginError = $_("oauth2Error.serverError"); }
    finally { loginLoading = false; }
  }

  // ── Consent ───────────────────────────────────────────────────────────────
  async function handleConsent(action: "allow" | "deny") {
    consentLoading = true;
    try {
      const res = await fetch("/api/oauth/authorize-confirm", {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ auth_code: authCode, action }),
      });
      const data = await res.json();
      if (data.success && data.redirect_uri) {
        window.location.href = data.redirect_uri;
        stage = "success";
      } else {
        stage = "error"; errorCode = data.error ?? "server_error";
      }
    } catch { stage = "error"; errorCode = "server_error"; }
    finally { consentLoading = false; }
  }

  // ── Init ──────────────────────────────────────────────────────────────────
  onMount(async () => {
    const browserLang = (navigator.languages?.[0] ?? navigator.language).split("-")[0].toLowerCase();
    await setLanguage(browserLang);
    authCode = getCodeFromURL();
    if (!authCode) { stage = "error"; errorCode = "invalid_request"; return; }
    await loadConsentInfo(authCode);
  });
</script>

<NetworkBackground />

<div class="page">
  <div class="card">

    <!-- ── Header ─────────────────────────────────────────────────────────── -->
    <div class="brand">
      <svg class="brand-icon" viewBox="0 0 24 24" fill="none">
        <path d="M13 2L3 14h9l-1 8 10-12h-9l1-8z" fill="currentColor"/>
      </svg>
      <span>Wantastic</span>
    </div>

    <!-- ── Loading ────────────────────────────────────────────────────────── -->
    {#if stage === "loading"}
      <div class="center-block">
        <div class="spinner" />
        <span class="hint">{$_("common.loading")}</span>
      </div>

    <!-- ── Login ──────────────────────────────────────────────────────────── -->
    {:else if stage === "login"}
      <div class="identity">
        <div class="avatar app-avatar">{clientInitial(consentInfo?.client_id ?? "A")}</div>
        <div class="arrow-flow">
          <span class="flow-dot" /><span class="flow-dot" /><span class="flow-dot" />
        </div>
        <div class="avatar wantastic-avatar">W</div>
      </div>

      <div class="section-title">Sign in to authorize</div>
      <p class="section-sub">
        <strong class="client-name">{clientDisplayName(consentInfo?.client_id ?? "Wantastic Agent")}</strong>
        {$_("oauth2.appRequestingAccess")}
      </p>

      <div class="divider" />

      {#if loginError}
        <div class="alert alert-error">{loginError}</div>
      {/if}

      <form on:submit|preventDefault={handleLogin} class="login-form">
        <input type="hidden" name="auth_code" value={authCode} />

        <div class="field">
          <label class="field-label" for="oc-email">{$_("auth.email")}</label>
          <input
            id="oc-email"
            type="email"
            bind:value={email}
            placeholder="you@example.com"
            required
            autofocus
            class="field-input"
            disabled={loginLoading}
          />
        </div>

        <div class="field">
          <label class="field-label" for="oc-password">{$_("auth.password")}</label>
          <input
            id="oc-password"
            type="password"
            bind:value={password}
            placeholder="••••••••"
            required
            class="field-input"
            disabled={loginLoading}
          />
        </div>

        <button type="submit" class="btn-primary" disabled={loginLoading}>
          {#if loginLoading}
            <span class="btn-spinner" />
          {/if}
          {loginLoading ? $_("oauth2.signingIn") : $_("oauth2.signInAndContinue")}
        </button>
      </form>

      <p class="footer-note">
        {$_("oauth2.clientLabel")}:
        <code>{authCode.substring(0, 8)}…</code>
      </p>

    <!-- ── Consent ────────────────────────────────────────────────────────── -->
    {:else if stage === "consent" && consentInfo}
      <div class="identity">
        <div class="avatar app-avatar">{clientInitial(consentInfo.client_id)}</div>
        <div class="arrow-flow">
          <span class="flow-dot" /><span class="flow-dot" /><span class="flow-dot" />
        </div>
        <div class="avatar wantastic-avatar">W</div>
      </div>

      <div class="section-title">
        <strong class="client-name">{clientDisplayName(consentInfo.client_id)}</strong>
        {$_("oauth2.appRequestingAccess")}
      </div>

      <div class="divider" />

      <p class="perms-heading">{$_("oauth2.appWillBeAbleTo")}</p>

      <ul class="scope-list">
        {#each scopeInfo(consentInfo.scope) as { label, icon }}
          <li class="scope-row">
            <span class="scope-check">
              <!-- checkmark svg -->
              <svg viewBox="0 0 16 16" fill="none">
                <path d="M3 8l3.5 3.5L13 5" stroke="currentColor" stroke-width="1.8" stroke-linecap="round" stroke-linejoin="round"/>
              </svg>
            </span>
            <span class="scope-label">{label}</span>
          </li>
        {/each}
      </ul>

      <div class="divider" />

      {#if consentInfo.user_email}
        <div class="user-row">
          <div class="user-dot" />
          <span class="user-email">{$_("common.signedInAs") || "Signed in as"} <strong>{consentInfo.user_email}</strong></span>
        </div>
      {/if}

      <div class="action-row">
        <button
          class="btn-ghost"
          disabled={consentLoading}
          on:click={() => handleConsent("deny")}
        >
          {consentLoading ? $_("oauth2.denying") : $_("oauth2.deny")}
        </button>
        <button
          class="btn-primary"
          disabled={consentLoading}
          on:click={() => handleConsent("allow")}
        >
          {#if consentLoading}
            <span class="btn-spinner" />
          {/if}
          {consentLoading ? $_("oauth2.allowing") : $_("oauth2.allowAccess")}
        </button>
      </div>

      <p class="footer-note security-note">
        <!-- shield icon -->
        <svg viewBox="0 0 16 16" fill="none" class="shield-icon">
          <path d="M8 2L3 4.5v4c0 3 2.5 4.9 5 5.5 2.5-.6 5-2.5 5-5.5v-4L8 2z" stroke="currentColor" stroke-width="1.3" stroke-linejoin="round"/>
        </svg>
        Only grant access to apps you trust. You can revoke this at any time.
      </p>

    <!-- ── Success ────────────────────────────────────────────────────────── -->
    {:else if stage === "success"}
      <div class="center-block success-block">
        <div class="success-circle">
          <svg viewBox="0 0 24 24" fill="none">
            <path d="M5 12l5 5L20 7" stroke="currentColor" stroke-width="2.5" stroke-linecap="round" stroke-linejoin="round"/>
          </svg>
        </div>
        <span class="success-text">Access granted — redirecting…</span>
      </div>

    <!-- ── Error ──────────────────────────────────────────────────────────── -->
    {:else if stage === "error"}
      <div class="center-block error-block">
        <div class="error-circle">
          <svg viewBox="0 0 24 24" fill="none">
            <path d="M12 8v4m0 4h.01M10.29 3.86L1.82 18a2 2 0 001.71 3h16.94a2 2 0 001.71-3L13.71 3.86a2 2 0 00-3.42 0z"
              stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"/>
          </svg>
        </div>
        <span class="error-heading">{$_("oauth2Error.title")}</span>
        <span class="error-body">{errorMessage(errorCode)}</span>
        <a href="/#desktop" class="btn-primary error-back">{$_("oauth2.backToPortal")}</a>
      </div>
    {/if}

  </div>
</div>

<style>
  /* ── Layout ─────────────────────────────────────────────────────────────── */
  .page {
    min-height: 100dvh;
    display: flex;
    align-items: center;
    justify-content: center;
    padding: 1.25rem;
  }

  .card {
    position: relative;
    width: 100%;
    max-width: 400px;
    background: rgba(18, 20, 30, 0.82);
    backdrop-filter: blur(32px) saturate(1.4);
    border: 1px solid rgba(255,255,255, 0.07);
    border-radius: 20px;
    padding: 1.75rem;
    display: flex;
    flex-direction: column;
    gap: 1rem;
    box-shadow: 0 24px 64px rgba(0,0,0,0.5), 0 0 0 1px rgba(255,255,255,0.04) inset;
  }

  /* ── Brand header ───────────────────────────────────────────────────────── */
  .brand {
    display: flex;
    align-items: center;
    gap: 0.45rem;
    font-size: 0.95rem;
    font-weight: 700;
    color: rgb(var(--clrPrm));
    letter-spacing: -0.01em;
    margin-bottom: 0.25rem;
  }

  .brand-icon {
    width: 18px;
    height: 18px;
    color: rgb(var(--clrPrm));
    flex-shrink: 0;
  }

  /* ── Identity / App ↔ Wantastic visual ──────────────────────────────────── */
  .identity {
    display: flex;
    align-items: center;
    justify-content: center;
    gap: 1rem;
    padding: 0.5rem 0;
  }

  .avatar {
    width: 52px;
    height: 52px;
    border-radius: 14px;
    display: flex;
    align-items: center;
    justify-content: center;
    font-size: 1.3rem;
    font-weight: 800;
    letter-spacing: -0.02em;
    flex-shrink: 0;
  }

  .app-avatar {
    background: linear-gradient(135deg, #6366f1, #818cf8);
    color: #fff;
    box-shadow: 0 4px 16px rgba(99, 102, 241, 0.35);
  }

  .wantastic-avatar {
    background: linear-gradient(135deg, rgb(var(--clrPrm)), color-mix(in srgb, rgb(var(--clrPrm)) 60%, #fff));
    color: #fff;
    box-shadow: 0 4px 16px rgba(var(--clrPrm), 0.35);
  }

  .arrow-flow {
    display: flex;
    align-items: center;
    gap: 5px;
  }

  .flow-dot {
    width: 5px;
    height: 5px;
    border-radius: 50%;
    background: rgba(255,255,255, 0.18);
  }

  .flow-dot:nth-child(2) { opacity: 0.55; }
  .flow-dot:nth-child(3) { opacity: 0.25; }

  /* ── Section text ───────────────────────────────────────────────────────── */
  .section-title {
    font-size: 0.95rem;
    font-weight: 500;
    color: rgba(255,255,255, 0.85);
    text-align: center;
    line-height: 1.45;
    margin: 0 0 -0.25rem;
  }

  .section-sub {
    font-size: 0.875rem;
    color: rgba(255,255,255, 0.55);
    text-align: center;
    margin: 0;
    line-height: 1.5;
  }

  .client-name {
    color: rgba(255,255,255, 0.92);
    font-weight: 600;
  }

  /* ── Divider ────────────────────────────────────────────────────────────── */
  .divider {
    height: 1px;
    background: rgba(255,255,255, 0.07);
    margin: 0.125rem 0;
  }

  /* ── Permissions ────────────────────────────────────────────────────────── */
  .perms-heading {
    font-size: 0.72rem;
    font-weight: 700;
    text-transform: uppercase;
    letter-spacing: 0.07em;
    color: rgba(255,255,255, 0.35);
    margin: 0;
  }

  .scope-list {
    list-style: none;
    margin: 0;
    padding: 0;
    display: flex;
    flex-direction: column;
    gap: 0.4rem;
  }

  .scope-row {
    display: flex;
    align-items: center;
    gap: 0.65rem;
    padding: 0.55rem 0.75rem;
    background: rgba(255,255,255, 0.04);
    border: 1px solid rgba(255,255,255, 0.06);
    border-radius: 10px;
    font-size: 0.875rem;
    color: rgba(255,255,255, 0.8);
  }

  .scope-check {
    width: 18px;
    height: 18px;
    background: rgba(74, 222, 128, 0.12);
    border: 1px solid rgba(74, 222, 128, 0.3);
    border-radius: 50%;
    display: flex;
    align-items: center;
    justify-content: center;
    flex-shrink: 0;
    color: #4ade80;
  }

  .scope-check svg {
    width: 10px;
    height: 10px;
  }

  .scope-label {
    line-height: 1.35;
  }

  /* ── User row ───────────────────────────────────────────────────────────── */
  .user-row {
    display: flex;
    align-items: center;
    gap: 0.5rem;
    font-size: 0.8rem;
    color: rgba(255,255,255, 0.45);
  }

  .user-dot {
    width: 7px;
    height: 7px;
    border-radius: 50%;
    background: #4ade80;
    flex-shrink: 0;
  }

  .user-email strong {
    color: rgba(255,255,255, 0.7);
    font-weight: 500;
  }

  /* ── Actions ────────────────────────────────────────────────────────────── */
  .action-row {
    display: flex;
    gap: 0.625rem;
    align-items: center;
  }

  .btn-primary {
    flex: 1;
    display: flex;
    align-items: center;
    justify-content: center;
    gap: 0.5rem;
    padding: 0.7rem 1rem;
    background: rgb(var(--clrPrm));
    color: #fff;
    border: none;
    border-radius: 10px;
    font-size: 0.875rem;
    font-weight: 600;
    cursor: pointer;
    transition: opacity 0.15s, transform 0.1s;
    text-decoration: none;
    box-sizing: border-box;
    letter-spacing: -0.01em;
  }

  .btn-primary:hover:not(:disabled) {
    opacity: 0.88;
    transform: translateY(-1px);
  }

  .btn-primary:active:not(:disabled) {
    transform: translateY(0);
  }

  .btn-primary:disabled {
    opacity: 0.45;
    cursor: not-allowed;
  }

  .btn-ghost {
    padding: 0.7rem 1rem;
    background: rgba(255,255,255, 0.06);
    border: 1px solid rgba(255,255,255, 0.1);
    border-radius: 10px;
    color: rgba(255,255,255, 0.6);
    font-size: 0.875rem;
    font-weight: 500;
    cursor: pointer;
    transition: background 0.15s, color 0.15s;
    white-space: nowrap;
  }

  .btn-ghost:hover:not(:disabled) {
    background: rgba(255,255,255, 0.1);
    color: rgba(255,255,255, 0.85);
  }

  .btn-ghost:disabled {
    opacity: 0.45;
    cursor: not-allowed;
  }

  .btn-spinner {
    width: 14px;
    height: 14px;
    border: 2px solid rgba(255,255,255, 0.3);
    border-top-color: #fff;
    border-radius: 50%;
    animation: spin 0.7s linear infinite;
    flex-shrink: 0;
  }

  /* ── Login form ─────────────────────────────────────────────────────────── */
  .login-form {
    display: flex;
    flex-direction: column;
    gap: 0;
  }

  .field {
    display: flex;
    flex-direction: column;
    gap: 0.3rem;
    margin-bottom: 0.75rem;
  }

  .field-label {
    font-size: 0.7rem;
    font-weight: 600;
    color: rgba(255,255,255, 0.4);
    text-transform: uppercase;
    letter-spacing: 0.06em;
  }

  .field-input {
    width: 100%;
    padding: 0.65rem 0.875rem;
    border: 1px solid rgba(255,255,255, 0.1);
    border-radius: 9px;
    background: rgba(255,255,255, 0.05);
    color: rgba(255,255,255, 0.9);
    font-size: 0.875rem;
    outline: none;
    box-sizing: border-box;
    transition: border-color 0.15s, background 0.15s;
  }

  .field-input::placeholder {
    color: rgba(255,255,255, 0.2);
  }

  .field-input:focus {
    border-color: rgba(var(--clrPrm), 0.7);
    background: rgba(255,255,255, 0.07);
  }

  .field-input:disabled {
    opacity: 0.45;
  }

  /* ── Alerts ─────────────────────────────────────────────────────────────── */
  .alert {
    padding: 0.65rem 0.875rem;
    border-radius: 9px;
    font-size: 0.825rem;
    line-height: 1.4;
  }

  .alert-error {
    background: rgba(239, 68, 68, 0.1);
    border: 1px solid rgba(239, 68, 68, 0.25);
    color: #fca5a5;
  }

  /* ── Footer note ────────────────────────────────────────────────────────── */
  .footer-note {
    font-size: 0.7rem;
    color: rgba(255,255,255, 0.25);
    margin: 0;
    line-height: 1.5;
  }

  .footer-note code {
    font-family: monospace;
    color: rgba(255,255,255, 0.35);
  }

  .security-note {
    display: flex;
    align-items: center;
    gap: 0.4rem;
  }

  .shield-icon {
    width: 12px;
    height: 12px;
    flex-shrink: 0;
    color: rgba(255,255,255, 0.3);
  }

  /* ── Center blocks (loading / success / error) ──────────────────────────── */
  .center-block {
    display: flex;
    flex-direction: column;
    align-items: center;
    gap: 0.75rem;
    padding: 1.5rem 0 0.5rem;
  }

  .hint {
    font-size: 0.83rem;
    color: rgba(255,255,255, 0.4);
  }

  .spinner {
    width: 28px;
    height: 28px;
    border: 2px solid rgba(255,255,255, 0.1);
    border-top-color: rgb(var(--clrPrm));
    border-radius: 50%;
    animation: spin 0.75s linear infinite;
  }

  /* Success */
  .success-block { padding-bottom: 1rem; }

  .success-circle {
    width: 52px;
    height: 52px;
    background: rgba(74, 222, 128, 0.12);
    border: 1.5px solid rgba(74, 222, 128, 0.35);
    border-radius: 50%;
    display: flex;
    align-items: center;
    justify-content: center;
    color: #4ade80;
  }

  .success-circle svg {
    width: 26px;
    height: 26px;
  }

  .success-text {
    font-size: 0.875rem;
    color: rgba(255,255,255, 0.65);
  }

  /* Error */
  .error-block { gap: 0.6rem; padding-bottom: 0.5rem; }

  .error-circle {
    width: 52px;
    height: 52px;
    background: rgba(239, 68, 68, 0.1);
    border: 1.5px solid rgba(239, 68, 68, 0.3);
    border-radius: 50%;
    display: flex;
    align-items: center;
    justify-content: center;
    color: #f87171;
  }

  .error-circle svg {
    width: 26px;
    height: 26px;
  }

  .error-heading {
    font-size: 0.925rem;
    font-weight: 600;
    color: rgba(255,255,255, 0.85);
  }

  .error-body {
    font-size: 0.825rem;
    color: rgba(255,255,255, 0.45);
    text-align: center;
    max-width: 260px;
  }

  .error-back {
    margin-top: 0.5rem;
    text-decoration: none;
  }

  /* ── Animations ─────────────────────────────────────────────────────────── */
  @keyframes spin {
    to { transform: rotate(360deg); }
  }
</style>
