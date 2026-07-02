import { writable, derived, get as getStore } from "svelte/store";
import { wsStore } from "./websocket";
import { API_BASE_URL } from "../config";

export interface Session {
  tenantId: string | null;
  email: string | null;
  fullName: string | null;
  expiresAt: number | null;
}

export interface User {
  id: string;
  email: string;
  fullName: string;
  emailVerified?: boolean;
  peersLimit?: number;
  peersUsed?: number;
  phoneNumber?: string;
  countryCode?: string;
  createdAt?: string;
}

export interface CaptchaChallenge {
  problem_id: string;
  image_base64: string;
}

export interface AuthState {
  user: User | null;
  session: Session | null;
  isLoading: boolean;
  error: string | null;
  sid: string | null;
  requiresTOTP: boolean;
  tenant_id: string | null;
  twoFAMethod: string | null;
  twoFAPhoneLast4: string | null;
  loginCaptchaRequired: boolean;
  loginCaptchaChallenge: CaptchaChallenge | null;
  loginSessionId: string | null;
}

const initialState: AuthState = {
  user: null,
  session: null,
  isLoading: false,
  tenant_id: null,
  error: null,
  sid: null,
  requiresTOTP: false,
  twoFAMethod: null,
  twoFAPhoneLast4: null,
  loginCaptchaRequired: false,
  loginCaptchaChallenge: null,
  loginSessionId: null,
};

function createAuthStore() {
  const { subscribe, set, update } = writable<AuthState>(initialState);

  /**
   * Get or generate a stable fingerprint for this device/browser
   */
  function getFingerprint(): string {
    if (typeof window === "undefined") return "";
    let fp = localStorage.getItem("tenant_fingerprint");
    if (!fp) {
      if (typeof crypto !== "undefined" && crypto.randomUUID) {
        fp = crypto.randomUUID();
      } else {
        fp =
          "fp-" +
          Math.random().toString(36).substring(2, 15) +
          Math.random().toString(36).substring(2, 15);
      }
      localStorage.setItem("tenant_fingerprint", fp);
    }
    return fp;
  }

  /**
   * Login with email and password
   * Follows exact pattern from login.html template:
   * 1. WebSocket TenantLogin request
   * 2. HTTP POST to /api/session to set cookie
   * 3. Handle 2FA if required
   */
  async function login(
    email: string,
    password: string,
    totpCode?: string,
    remember?: boolean,
    captchaAnswer?: string,
    loginSessionId?: string
  ): Promise<{
    success: boolean;
    requiresTOTP?: boolean;
    twoFAMethod?: string;
    twoFAPhoneLast4?: string;
    message?: string;
    captchaRequired?: boolean;
    captchaChallenge?: CaptchaChallenge | null;
    loginSessionId?: string | null;
  }> {
    update((s) => ({ ...s, isLoading: true, error: null }));

    try {
      // Step 1: WebSocket authentication via TenantLogin
      const response = await wsStore.callGRPC<{
        success: boolean;
        message?: string;
        error_code?: string;
        requires_totp?: boolean;
        two_fa_method?: string;
        two_fa_phone_last4?: string;
        tenant_id?: string;
        email?: string;
        session_token?: string;
        session_id?: string;
        full_name?: string;
        is_first_login?: boolean;
        captcha_required?: boolean;
        captcha_challenge?: CaptchaChallenge | null;
      }>("TenantPortalService", "TenantLogin", {
        email: email,
        password: password,
        totp_code: totpCode || "",
        remember_me: remember || false,
        fingerprint: getFingerprint(),
        session_id: loginSessionId || "",
        captcha_answer: captchaAnswer || "",
      });

      // Handle 2FA requirement
      if (response.requires_totp) {
        update((s) => ({
          ...s,
          isLoading: false,
          requiresTOTP: true,
          twoFAMethod: response.two_fa_method || null,
          twoFAPhoneLast4: response.two_fa_phone_last4 || null,
          error: null,
        }));

        return {
          success: false,
          requiresTOTP: true,
          twoFAMethod: response.two_fa_method,
          twoFAPhoneLast4: response.two_fa_phone_last4,
          message: response.message || "Two-factor authentication required",
        };
      }

      // Handle CAPTCHA challenge
      if (!response.success && response.captcha_required) {
        update((s) => ({
          ...s,
          isLoading: false,
          loginCaptchaRequired: true,
          loginCaptchaChallenge: response.captcha_challenge ?? null,
          loginSessionId: response.session_id ?? null,
          error: null,
        }));
        return {
          success: false,
          captchaRequired: true,
          captchaChallenge: response.captcha_challenge ?? null,
          loginSessionId: response.session_id ?? null,
          message: response.message || response.error_code || "CAPTCHA required",
        };
      }

      // Handle login failure
      if (!response.success) {
        const errorMessage = response.message || response.error_code || "Login failed";
        update((s) => ({
          ...s,
          isLoading: false,
          error: errorMessage,
          loginCaptchaRequired: false,
          loginCaptchaChallenge: null,
        }));

        return {
          success: false,
          message: errorMessage,
        };
      }

      // Step 2: Create session cookie via HTTP endpoint
      // This sets the session cookie that the backend uses for authentication
      const sessionResponse = await fetch(`${API_BASE_URL}/api/session`, {
        method: "POST",
        credentials: "include", // Important: include cookies
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({
          tenant_id: response.tenant_id,
          email: response.email,
          full_name: response.full_name,
          grpc_session_token: response.session_token, // For logout to invalidate gRPC session
          remember_me: remember || false,
          is_first_login: response.is_first_login || false,
        }),
      });

      if (!sessionResponse.ok) {
        throw new Error("Failed to create session cookie");
      }

      // Update store with user session
      const user: User = {
        id: response.tenant_id!,
        email: response.email!,
        fullName: response.full_name!,
      };

      const session: Session = {
        tenantId: response.tenant_id!,
        email: response.email!,
        fullName: response.full_name!,
        expiresAt: Date.now() + (remember ? 30 * 24 * 3600000 : 24 * 3600000), // 30 days or 1 day
      };

      update((s) => ({
        ...s,
        user,
        session,
        isLoading: false,
        error: null,
        requiresTOTP: false,
        twoFAMethod: null,
        sid: response.session_token,
        tenant_id: response.tenant_id,
        twoFAPhoneLast4: null,
        loginCaptchaRequired: false,
        loginCaptchaChallenge: null,
        loginSessionId: null,
      }));

      return { success: true };
    } catch (err: any) {
      const errorMessage = err.message || "An error occurred during login";
      update((s) => ({
        ...s,
        error: errorMessage,
        isLoading: false,
      }));

      return {
        success: false,
        message: errorMessage,
      };
    }
  }

  /**
   * Resend 2FA code via SMS
   * Called when user didn't receive the initial code
   */
  async function resend2FACode(email: string): Promise<boolean> {
    try {
      update((s) => ({ ...s, isLoading: true, error: null }));

      const response = await wsStore.callGRPC<{
        success: boolean;
        message?: string;
      }>("TenantPortalService", "ResendTOTP", {
        email: email,
      });

      update((s) => ({ ...s, isLoading: false }));

      return response.success;
    } catch (error) {
      const message =
        error instanceof Error ? error.message : "Failed to resend code";
      update((s) => ({
        ...s,
        error: message,
        isLoading: false,
      }));
      return false;
    }
  }

  /**
   * Logout and clear session
   * Clears session cookie via backend endpoint
   */
  async function logout(): Promise<boolean> {
    update((s) => ({ ...s, isLoading: true }));

    try {
      // Call backend logout endpoint to clear HttpOnly cookie
      await fetch(`${API_BASE_URL}/api/logout`, {
        method: "POST",
        credentials: "include", // Send cookies
        headers: {
          "Content-Type": "application/json",
        },
      });

      // Disconnect WebSocket
      wsStore.disconnect();

      // Reset store
      set(initialState);

      return true;
    } catch (err: any) {
      console.error("Logout error:", err);
      // Still reset store even if API call fails
      wsStore.disconnect();
      set(initialState);
      return false;
    } finally {
      update((s) => ({ ...s, isLoading: false }));
    }
  }

  function expireSession(reason = "Session expired") {
    wsStore.disconnect();
    set({
      ...initialState,
      error: reason,
    });
  }

  /**
   * Check if user has active session from cookie
   * Called on app load to restore session
   * Note: tenant_session cookie is HttpOnly, so we cannot read it from JavaScript
   * Instead, we'll attempt to fetch user account via gRPC - if successful, session is valid
   */
  async function checkSession(): Promise<boolean> {
    try {
      // Try to get account info via gRPC - this will work if session cookie is valid
      // The WebSocket automatically sends the HttpOnly cookie to backend
      const response = await wsStore.callGRPC<{
        tenant_id?: string;
        account?: {
          id: string;
          name: string;
        };
        full_name?: string;
        email?: string;
        phone?: string;
        totp_enabled?: boolean;
      }>("TenantPortalService", "GetTenantAccount", {}, {
        suppressAuthExpiredEvent: true,
      });

      if (response.tenant_id) {
        // console.log(' Session valid, user authenticated');

        // Update store with user data
        const user: User = {
          id: response.tenant_id,
          email: response.email || "",
          fullName: response.full_name || response.account?.name || "",
        };

        const session: Session = {
          tenantId: response.tenant_id,
          email: response.email || null,
          fullName: response.full_name || null,
          expiresAt: Date.now() + 24 * 3600000, // Estimate
        };

        update((s) => ({
          ...s,
          session,
          user,
          tenant_id: response.tenant_id,
        }));

        return true;
      }

      // console.log('❌ No valid session');
      return false;
    } catch (error) {
      // console.log('❌ Session check failed:', error);
      return false;
    }
  }

  /**
   * Set loading state manually
   */
  function setLoading(isLoading: boolean) {
    update((s) => ({ ...s, isLoading }));
  }

  /**
   * Load persisted session on app start
   * Checks for auth_session cookie
   */
  function loadPersistedSession() {
    return checkSession();
  }

  // Derived stores for convenience
  const isAuthenticated = derived(
    { subscribe },
    (s) => !!s.session && !!s.user
  );
  const currentUser = derived({ subscribe }, (s) => s.user);
  const tenant_id = derived({ subscribe }, (s) => s.tenant_id);

  return {
    subscribe,
    login,

    logout,
    expireSession,
    resend2FACode,
    checkSession,
    setLoading,
    loadPersistedSession,
    isAuthenticated,
    currentUser,
    tenant_id,
  };
}

export const authStore = createAuthStore();

// Auto-load session on import
authStore.loadPersistedSession();

// Export derived stores for convenience
export const isAuthenticated = authStore.isAuthenticated;
export const currentUser = authStore.currentUser;
export const tenant_id = authStore.tenant_id;
export const auth = authStore; // Alias for compatibility
