<script lang="ts">
  import { onDestroy, onMount } from "svelte";
  import { wsStore } from "../store/websocket";
  import { translateError$, _, initializeI18n, translateError } from "../store/i18n";
  import {
    Button,
    TextBox,
    ProgressRing,
    InfoBar,
    TextBlock,
  } from "fluent-svelte";

  type ResetStep = "email" | "verify" | "password" | "success";

  let currentStep: ResetStep = "email";
  let email = "";
  let verificationCode = "";
  let newPassword = "";
  let confirmPassword = "";
  let isLoading = false;
  let error = "";
  let successMessage = "";
  let passwordStrength = 0;
  let countdown = 300; // 5 minutes
  let countdownInterval: ReturnType<typeof setInterval> | null = null;
  let canResend = false;
  let phoneMasked = "";
  let resetToken = "";
  let verifiedToken = "";

  // Calculate password strength reactively
  $: {
    let strength = 0;
    if (newPassword.length >= 8) strength++;
    if (newPassword.length >= 12) strength++;
    if (/[A-Z]/.test(newPassword) && /[a-z]/.test(newPassword)) strength++;
    if (/\d/.test(newPassword)) strength++;
    if (/[^A-Za-z0-9]/.test(newPassword)) strength++;
    passwordStrength = strength;
  }
  onMount(async () => {
    await initializeI18n();
  });
  // Format countdown as MM:SS
  function formatCountdown(seconds: number): string {
    const mins = Math.floor(seconds / 60);
    const secs = seconds % 60;
    return `${mins}:${secs.toString().padStart(2, "0")}`;
  }

  function getStrengthLabel(): string {
    if (passwordStrength <= 1) return $_("auth.passwordStrength.weak");
    if (passwordStrength <= 2) return $_("auth.passwordStrength.fair");
    if (passwordStrength <= 4) return $_("auth.passwordStrength.good");
    return $_("auth.passwordStrength.strong");
  }

  function getStrengthClass(): string {
    if (passwordStrength <= 2) return "bg-red-500";
    if (passwordStrength <= 4) return "bg-yellow-500";
    return "bg-green-500";
  }

  // Start countdown timer
  function startCountdown(seconds: number) {
    clearCountdown();
    countdown = seconds;
    canResend = false;

    countdownInterval = setInterval(() => {
      countdown--;
      if (countdown <= 0) {
        clearCountdown();
      }
    }, 1000);

    // Allow resend after 30 seconds
    setTimeout(() => {
      canResend = true;
    }, 30000);
  }

  function clearCountdown() {
    if (countdownInterval) {
      clearInterval(countdownInterval);
      countdownInterval = null;
    }
  }

  function maskEmail(emailStr: string): string {
    const parts = emailStr.split("@");
    if (parts.length < 2) return emailStr;
    const name = parts[0];
    const domain = parts[1];
    const maskedName =
      name.length > 3 ? name.slice(0, 3) + "***" : name + "***";
    return `${maskedName}@${domain}`;
  }

  // Handle sending reset email
  async function handleSendReset(e?: Event) {
    e?.preventDefault();
    error = "";
    successMessage = "";
    isLoading = true;

    try {
      const trimmedEmail = email.trim();
      if (!trimmedEmail) {
        error = $_("auth.enterEmailError");
        isLoading = false;
        return;
      }

      const response = await wsStore.callGRPC<{
        success?: boolean;
        message?: string;
        reset_token?: string;
        phone_masked?: string;
        code_expires_seconds?: number;
      }>("TenantPortalService", "RequestPasswordReset", {
        email: trimmedEmail,
      });

      // Setup state for next step regardless of specific success details to prevent enumeration
      // If token is missing (invalid email), we still show the verify screen (fake flow)
      resetToken = response.reset_token || "";
      phoneMasked = response.phone_masked || maskEmail(trimmedEmail);
      startCountdown(response.code_expires_seconds || 300);

      // Always transition to verify step
      currentStep = "verify";

      // Optional: if there's a specific message we want to persist (like "Email sent"), we could
      // but usually the verify screen itself says "Code sent to ...".
      // We clear successMessage here as the UI changes completely.
    } catch (err: any) {
      error = translateError(err?.message || String(err)) || $_('auth.sendResetFailed');
    } finally {
      isLoading = false;
    }
  }

  // Handle verification code
  async function handleVerifyCode(e?: Event) {
    e?.preventDefault();
    error = "";
    isLoading = true;

    try {
      const code = verificationCode.trim();
      if (code.length !== 6) {
        error = $_("auth.enterCodeError");
        isLoading = false;
        return;
      }

      const response = await wsStore.callGRPC<{
        success?: boolean;
        message?: string;
        verified_token?: string;
      }>("TenantPortalService", "VerifyResetCode", {
        reset_token: resetToken,
        verification_code: code,
      });

      if (response.success && response.verified_token) {
        verifiedToken = response.verified_token;
        clearCountdown();
        currentStep = "password";
      } else {
        error = translateError(response.message) || $_("auth.invalidCode");
        verificationCode = "";
      }
    } catch (err: any) {
      error = translateError(err) || $_("auth.verificationFailed");
      verificationCode = "";
    } finally {
      isLoading = false;
    }
  }

  // Handle reset password
  async function handleResetPassword(e?: Event) {
    e?.preventDefault();
    error = "";
    isLoading = true;

    try {
      if (newPassword !== confirmPassword) {
        error = $_("registration.passwordsDoNotMatch");
        isLoading = false;
        return;
      }

      if (newPassword.length < 8) {
        error = $_("registration.passwordMinLength");
        isLoading = false;
        return;
      }

      if (passwordStrength <= 2) {
        error = $_("auth.passwordTooWeak");
        isLoading = false;
        return;
      }

      const response = await wsStore.callGRPC<{
        success?: boolean;
        message?: string;
      }>("TenantPortalService", "ResetPassword", {
        verified_token: verifiedToken,
        new_password: newPassword,
      });

      if (response.success) {
        currentStep = "success";
      } else {
        error = translateError(response.message) || $_("auth.resetFailed");
      }
    } catch (err: any) {
      error = translateError(err?.message || String(err)) || 'Password reset failed. Please try again.';
    } finally {
      isLoading = false;
    }
  }

  function goBack() {
    error = "";
    successMessage = "";
    if (currentStep === "verify") {
      currentStep = "email";
      clearCountdown();
    } else if (currentStep === "password") {
      currentStep = "verify";
    }
  }

  async function resendCode() {
    error = "";
    successMessage = "";
    isLoading = true;

    try {
      const response = await wsStore.callGRPC<{
        success?: boolean;
        message?: string;
        reset_token?: string;
        code_expires_seconds?: number;
      }>("TenantPortalService", "RequestPasswordReset", {
        email: email.trim(),
      });

      if (response.reset_token) {
        resetToken = response.reset_token;
      }
      startCountdown(response.code_expires_seconds || 300);
      successMessage = $_("auth.newCodeSent");
      verificationCode = "";
    } catch (err: any) {
      error = err.message || $_("auth.resendFailed");
    } finally {
      isLoading = false;
    }
  }

  onDestroy(() => {
    clearCountdown();
  });
</script>

<div
  class="min-h-[100dvh] flex flex-col items-center justify-center p-6 text-slate-200"
>
  <div class="w-full max-w-[380px] animate-fade-in">
    <div class="text-center mb-10">
      <img
        src="img/icon/logo.svg"
        alt="Wantastic"
        class="mb-4 mx-auto"
        height="48"
        width="48"
      />
      <p class="m-0 text-slate-400 text-[0.95rem]">{$_("auth.recovery")}</p>
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
        title={error}
        closable
        on:close={() => (error = "")}
      />
    {/if}

    <!-- Step 1: Email -->
    {#if currentStep === "email"}
      <form on:submit={handleSendReset}>
        <TextBlock variant="body" class="mb-4 text-center text-slate-400">
          {$_("auth.enterEmail")}
        </TextBlock>

        <div class="mb-5">
          <TextBox
            bind:value={email}
            placeholder="your@email.com"
            type="email"
            disabled={isLoading}
          />
        </div>

        <Button
          variant="accent"
          disabled={isLoading}
          class="w-full"
          on:click={handleSendReset}
        >
          {#if isLoading}
            <ProgressRing size={16} />
            {$_("auth.sendingCode")}
          {:else}
            {$_("auth.sendVerificationCode")}
          {/if}
        </Button>

        <div class="mt-6 text-center">
          <a
            href="#login"
            class="text-slate-400 no-underline hover:text-slate-200 hover:underline transition-colors text-sm"
          >
            ← {$_("auth.backToLogin")}
          </a>
        </div>
      </form>
    {/if}

    <!-- Step 2: Verify Code -->
    {#if currentStep === "verify"}
      <form on:submit={handleVerifyCode}>
        <div
          class="bg-slate-800/50 border border-slate-700/50 rounded-lg p-4 mb-6 flex items-center gap-4 text-slate-200 text-sm"
        >
          <img
            src="img/icon/ui/email.svg"
            alt="Email"
            class="w-6 h-6 opacity-80"
          />
          <p>
            {$_("auth.sentCodeTo", {
              values: { email: phoneMasked },
            })}
          </p>
        </div>

        <div class="mb-5">
          <TextBox
            bind:value={verificationCode}
            placeholder={$_("auth.enterCode")}
            disabled={isLoading}
          />
        </div>

        <div class="mb-4 text-center text-sm text-slate-400">
          {#if countdown > 0}
            {$_("auth.codeExpiresIn", {
              values: { time: formatCountdown(countdown) },
            })}
          {:else}
            {$_("auth.codeExpired")}
          {/if}
        </div>

        <Button
          variant="accent"
          disabled={isLoading || countdown <= 0}
          class="w-full mb-3"
          on:click={handleVerifyCode}
        >
          {#if isLoading}
            <ProgressRing size={16} />
            {$_("auth.verifying")}
          {:else}
            {$_("auth.verifyCode")}
          {/if}
        </Button>

        <Button
          disabled={isLoading || !canResend}
          class="w-full"
          on:click={resendCode}
        >
          {#if isLoading}
            <ProgressRing size={16} />
            {$_("auth.sending")}
          {:else}
            {$_("auth.resendCode")}
          {/if}
        </Button>

        <div class="mt-6 text-center">
          <button
            type="button"
            class="bg-transparent border-none text-slate-400 cursor-pointer text-sm hover:text-slate-200 hover:underline"
            on:click={goBack}
            disabled={isLoading}
          >
            ← {$_("auth.backToEmail")}
          </button>
        </div>
      </form>
    {/if}

    <!-- Step 3: New Password -->
    {#if currentStep === "password"}
      <form on:submit={handleResetPassword}>
        <div
          class="bg-slate-800/50 border border-slate-700/50 rounded-lg p-4 mb-6 flex items-center gap-4 text-slate-200 text-sm"
        >
          <img
            src="img/icon/ui/shield-lock.svg"
            alt="Password"
            class="w-6 h-6 opacity-80"
          />
          <p>{$_("auth.emailVerified")}</p>
        </div>

        <div class="mb-5">
          <TextBox
            type="password"
            bind:value={newPassword}
            placeholder={$_("auth.newPassword")}
            disabled={isLoading}
          />
          <div
            class="h-1 w-full bg-slate-700 mt-2 rounded-full overflow-hidden"
          >
            <div
              class="h-full transition-all duration-300 {getStrengthClass()}"
              style="width: {(passwordStrength / 5) * 100}%"
            />
          </div>
          <p class="text-xs text-slate-500 mt-1">
            {getStrengthLabel()} - Minimum 8 characters
          </p>
        </div>

        <div class="mb-5">
          <TextBox
            type="password"
            bind:value={confirmPassword}
            placeholder={$_("auth.confirmNewPassword")}
            disabled={isLoading}
          />
        </div>

        <Button
          variant="accent"
          disabled={isLoading ||
            newPassword !== confirmPassword ||
            newPassword.length < 8}
          class="w-full"
          on:click={handleResetPassword}
        >
          {#if isLoading}
            <ProgressRing size={16} />
            {$_("auth.resetting")}
          {:else}
            {$_("auth.resetPassword")}
          {/if}
        </Button>

        <div class="mt-6 text-center">
          <button
            type="button"
            class="bg-transparent border-none text-slate-400 cursor-pointer text-sm hover:text-slate-200 hover:underline"
            on:click={goBack}
            disabled={isLoading}
          >
            ← {$_("auth.back")}
          </button>
        </div>
      </form>
    {/if}

    <!-- Step 4: Success -->
    {#if currentStep === "success"}
      <div class="text-center animate-fade-in">
        <div class="mb-6 flex justify-center">
          <div class="bg-green-500/20 p-4 rounded-full">
            <img
              src="img/icon/ui/check-circle.svg"
              alt="Success"
              class="w-12 h-12 text-green-500"
            />
          </div>
        </div>
        <h2 class="text-xl font-semibold mb-2">{$_("auth.resetComplete")}</h2>
        <p class="text-slate-400 mb-6">{$_("auth.passwordChangedSuccess")}</p>
        <p class="text-xs text-slate-500 mb-8">
          {$_("auth.sessionsLoggedOut")}
        </p>

        <a href="#login" class="no-underline block w-full">
          <Button variant="accent" class="w-full">
            {$_("auth.goToLogin")}
          </Button>
        </a>
      </div>
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

<style>
  :global(.animate-fade-in) {
    animation: fadeIn 0.6s ease-out;
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
</style>
