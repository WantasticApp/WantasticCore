<script lang="ts">
  import { onMount } from "svelte";
  import {
    Button,
    TextBox,
    ToggleSwitch,
    ProgressRing,
    InfoBar,
  } from "fluent-svelte";
  import { authStore } from "../store/auth";
  import {
    translateError$,
    setLanguage,
    initializeI18nWithAccount,
    _,
  } from "../store/i18n";
  import NetworkBackground from "$components/NetworkBackground.svelte";

  let i18nReady = false;
  let identifier = ""; // Email or username
  let password = "";
  let rememberMe = false;
  let isLoading = false;
  let error = "";
  let successMessage = "";
  let showTOTP = false;
  let totpCode = "";
  let twoFAMethod: "totp" | "sms" | "whatsapp" | "email" = "totp";
  let twoFAPhoneLast4 = "";
  // CAPTCHA state
  let showCaptcha = false;
  let captchaAnswer = "";
  let captchaImageBase64 = "";
  let captchaSessionId = "";

  onMount(async () => {
    // Always pick the browser's preferred language
    const browserLangs = navigator.languages || [navigator.language];
    const primaryLang = browserLangs[0]?.split("-")[0]?.toLowerCase() || "en";
    await setLanguage(primaryLang);
    i18nReady = true;

    // Check for success messages from URL params
    const urlParams = new URLSearchParams(window.location.search);

    if (urlParams.get("reset") === "1") {
      successMessage = $_("auth.passwordResetSuccess");
    }

    // Clear URL params after reading them
    if (successMessage) {
      window.history.replaceState({}, document.title, window.location.pathname);
      // Auto-clear success message after 10 seconds
      setTimeout(() => {
        successMessage = "";
      }, 10000);
    }
  });

  async function handleLogin(e: Event) {
    e.preventDefault();
    error = "";
    isLoading = true;

    try {
      const response = await authStore.login(
        identifier,
        password,
        undefined,
        rememberMe
      );

      // Check for 2FA requirement first (before checking general failure)
      if (response.requiresTOTP) {
        showTOTP = true;
        twoFAMethod = (response.twoFAMethod || "totp") as any;
        twoFAPhoneLast4 = response.twoFAPhoneLast4 || "";
        isLoading = false;
        return;
      }

      // Check for CAPTCHA requirement
      if (response.captchaRequired) {
        showCaptcha = true;
        captchaImageBase64 = response.captchaChallenge?.image_base64 || "";
        captchaSessionId = response.loginSessionId || "";
        captchaAnswer = "";
        error = "";
        isLoading = false;
        return;
      }

      if (!response.success) {
        error = response.message || "Login failed";
        isLoading = false;
        return;
      }

      // Initialize i18n with account preferences after successful login
      await initializeI18nWithAccount();

      // Redirection is handled by the Router component when auth state changes
    } catch (err: any) {
      console.error("Login failed:", err);
      error = err.message || "Login failed. Please try again.";
    } finally {
      isLoading = false;
    }
  }

  async function handleCaptchaSubmit(e: Event) {
    e.preventDefault();
    if (!captchaAnswer.trim()) return;
    error = "";
    isLoading = true;

    try {
      const response = await authStore.login(
        identifier,
        password,
        undefined,
        rememberMe,
        captchaAnswer,
        captchaSessionId
      );

      if (response.requiresTOTP) {
        showCaptcha = false;
        showTOTP = true;
        twoFAMethod = (response.twoFAMethod || "totp") as any;
        twoFAPhoneLast4 = response.twoFAPhoneLast4 || "";
        isLoading = false;
        return;
      }

      // Wrong CAPTCHA answer → server sends a new challenge
      if (response.captchaRequired) {
        captchaImageBase64 = response.captchaChallenge?.image_base64 || captchaImageBase64;
        captchaSessionId = response.loginSessionId || captchaSessionId;
        captchaAnswer = "";
        error = response.message || "";
        isLoading = false;
        return;
      }

      if (!response.success) {
        error = response.message || "Login failed";
        showCaptcha = false;
        isLoading = false;
        return;
      }

      await initializeI18nWithAccount();
    } catch (err: any) {
      console.error("Captcha login failed:", err);
      error = err.message || "Login failed. Please try again.";
    } finally {
      isLoading = false;
    }
  }

  async function handleTOTP(e: Event) {
    e.preventDefault();
    error = "";
    isLoading = true;

    try {
      const response = await authStore.login(
        identifier,
        password,
        totpCode,
        rememberMe
      );

      if (!response.success) {
        error = response.message || "Verification failed";
        isLoading = false;
        return;
      }

      // Initialize i18n with account preferences after successful login
      await initializeI18nWithAccount();

      // Redirection is handled by the Router component when auth state changes
    } catch (err: any) {
      console.error("TOTP verification failed:", err);
      error = err.message || "Verification failed. Please try again.";
      totpCode = "";
    } finally {
      isLoading = false;
    }
  }

  function resetToForm() {
    showTOTP = false;
    showCaptcha = false;
    captchaAnswer = "";
    captchaImageBase64 = "";
    captchaSessionId = "";
    totpCode = "";
    error = "";
  }

  function getTwoFALabel(): string {
    switch (twoFAMethod) {
      case "sms":
        return $_("auth.smsCode", {
          values: { phone: twoFAPhoneLast4 },
        });
      case "email":
        return $_("auth.emailCode");
      case "whatsapp":
        return $_("auth.whatsappCode");
      case "totp":
      default:
        return $_("auth.authenticatorCode");
    }
  }

  function getTwoFAIcon(): string {
    switch (twoFAMethod) {
      case "sms":
        return "phone";
      case "email":
        return "email";
      case "whatsapp":
        return "message";
      case "totp":
      default:
        return "shield-lock";
    }
  }
</script>

{#if !i18nReady}
  <div class="min-h-[100dvh] flex items-center justify-center">
    <ProgressRing size={32} />
  </div>
{:else}
  <div
    class="min-h-[100dvh] flex flex-col items-center justify-center p-6 text-slate-200"
  >
    <NetworkBackground />
    <div class="w-full max-w-[380px] animate-fade-in">
      <div class="text-center mb-10">
        <img
          src="img/icon/logo.svg"
          alt="Wantastic"
          class="mb-4 mx-auto"
          height="48"
          width="48"
        />
        <p class="m-0 text-slate-400 text-[0.95rem]">
          {$_("auth.securityPriority")}
        </p>
      </div>

      {#if successMessage}
        <InfoBar
          severity="success"
          title={successMessage}
          closable
          on:close={() => (successMessage = "")}
        />
      {/if}

      {#if error}
        <InfoBar
          severity="critical"
          title={$translateError$(error)}
          closable
          on:close={() => (error = "")}
        />
      {/if}

      {#if !showTOTP && !showCaptcha}
        <form on:submit={handleLogin}>
          <div class="mb-5">
            <TextBox
              bind:value={identifier}
              placeholder={$_("auth.emailOrUsername")}
              disabled={isLoading}
            />
          </div>

          <div class="mb-5">
            <TextBox
              type="password"
              bind:value={password}
              placeholder={$_("auth.password")}
              disabled={isLoading}
            />
          </div>

          <div class="mb-6">
            <ToggleSwitch bind:checked={rememberMe} disabled={isLoading}>
              {$_("auth.rememberMe")}
            </ToggleSwitch>
          </div>

          <Button
            variant="accent"
            disabled={isLoading}
            class="w-full"
            on:click={handleLogin}
          >
            {#if isLoading}
              <ProgressRing size={16} />
              {$_("auth.signingIn")}
            {:else}
              {$_("auth.login")}
            {/if}
          </Button>
        </form>

        <div class="flex justify-center items-center gap-2 mt-6 text-sm">
          <a
            href="#reset-password"
            class="text-[var(--primary)] no-underline font-medium opacity-90 hover:opacity-100 hover:underline transition-opacity"
          >
            {$_("auth.forgotPassword")}
          </a>
        </div>
      {:else if showCaptcha}
        <form on:submit={handleCaptchaSubmit}>
          <div
            class="bg-slate-800/50 border border-slate-700/50 rounded-lg p-4 mb-5 flex flex-col items-center gap-3 text-slate-200 text-sm"
          >
            <p class="text-center">{$_("auth.captchaPrompt")}</p>
            {#if captchaImageBase64}
              <img
                src="data:image/svg+xml;base64,{captchaImageBase64}"
                alt="CAPTCHA"
                class="rounded border border-slate-600"
                style="image-rendering:pixelated;"
              />
            {/if}
          </div>

          <div class="mb-5">
            <TextBox
              bind:value={captchaAnswer}
              placeholder={$_("auth.captchaAnswer")}
              disabled={isLoading}
            />
          </div>

          <Button
            variant="accent"
            disabled={isLoading}
            class="w-full"
            on:click={handleCaptchaSubmit}
          >
            {#if isLoading}
              <ProgressRing size={16} />
              {$_("auth.verifying")}
            {:else}
              {$_("auth.login")}
            {/if}
          </Button>

          <button
            type="button"
            class="bg-transparent border-none text-slate-400 cursor-pointer text-sm mt-4 w-full hover:text-slate-300 hover:underline"
            on:click={resetToForm}
            disabled={isLoading}
          >
            {$_("auth.backToLogin")}
          </button>
        </form>
      {:else}
        <form on:submit={handleTOTP}>
          <div
            class="bg-slate-800/50 border border-slate-700/50 rounded-lg p-4 mb-6 flex items-center gap-4 text-slate-200 text-sm"
          >
            <img
              src="img/icon/ui/{getTwoFAIcon()}.svg"
              alt="2FA"
              class="w-6 h-6 opacity-80"
            />
            <p>{$_("auth.twoFactorEnabled")}</p>
          </div>

          <div class="mb-5">
            <TextBox
              bind:value={totpCode}
              placeholder={getTwoFALabel()}
              disabled={isLoading}
            />
          </div>

          <Button
            variant="accent"
            disabled={isLoading}
            class="w-full"
            on:click={handleTOTP}
          >
            {#if isLoading}
              <ProgressRing size={16} />
              {$_("auth.verifying")}
            {:else}
              {$_("auth.verifyCode")}
            {/if}
          </Button>

          <button
            type="button"
            class="bg-transparent border-none text-slate-400 cursor-pointer text-sm mt-4 w-full hover:text-slate-300 hover:underline"
            on:click={resetToForm}
            disabled={isLoading}
          >
            {$_("auth.backToLogin")}
          </button>
        </form>
      {/if}

      <div
        class="mt-12 text-center text-xs text-slate-500 flex justify-center gap-3"
      >
        <a
          href="#privacy"
          target="_blank"
          class="text-inherit no-underline hover:text-slate-400 transition-colors"
          >{$_("auth.privacyPolicy")}</a
        >
        <span>•</span>
        <a
          href="#terms"
          target="_blank"
          class="text-inherit no-underline hover:text-slate-400 transition-colors"
          >{$_("auth.termsOfService")}</a
        >
      </div>
    </div>
  </div>
{/if}

<style>
  :global(.animate-fade-in) {
    animation: fadeIn 0.6s ease-out;
  }
  :global(.animate-slide-in) {
    animation: slideIn 0.3s ease;
  }
  :global(.text-box-container) {
    block-size: 35px !important;
  }
  :global(.text-box-container input) {
    block-size: 35px !important;
  }
  :global(.button) {
    cursor: pointer !important;
  }
  :global(.button.style-accent) {
    block-size: 35px !important;
  }
  :global(.info-bar) {
    min-block-size: 36px !important;
    padding-inline-start: 10px !important;
    font-size: 13px !important;
    margin-bottom: 1.5rem !important;
  }
  :global(.info-bar h5) {
    font-size: 13px !important;
    line-height: 18px !important;
  }
  :global(.info-bar-icon) {
    margin-block-start: 10px !important;
  }
  :global(.info-bar-content) {
    margin-block-start: 5px !important;
    margin-block-end: 5px !important;
    margin-inline-start: 8px !important;
  }
  :global(.info-bar-close-button) {
    block-size: 30px !important;
    inline-size: 30px !important;
  }
  @keyframes fadeIn {
    from {
      opacity: 0;
      transform: translateY(10px);
    }
    to {
      opacity: 1;
      transform: translateY(0);
    }
  }
  @keyframes slideIn {
    from {
      opacity: 0;
      transform: translateY(-5px);
    }
    to {
      opacity: 1;
      transform: translateY(0);
    }
  }
</style>
