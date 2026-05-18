import { writable, derived, get } from "svelte/store";

// Supported languages
export const SUPPORTED_LANGUAGES = {
  en: { name: "English", nativeName: "English", dir: "ltr" },
  ar: { name: "Arabic", nativeName: "العربية", dir: "rtl" },
  he: { name: "Hebrew", nativeName: "עברית", dir: "rtl" },
  fr: { name: "French", nativeName: "Français", dir: "ltr" },
  es: { name: "Spanish", nativeName: "Español", dir: "ltr" },
  de: { name: "German", nativeName: "Deutsch", dir: "ltr" },
  zh: { name: "Chinese", nativeName: "中文", dir: "ltr" },
  ja: { name: "Japanese", nativeName: "日本語", dir: "ltr" },
  ru: { name: "Russian", nativeName: "Русский", dir: "ltr" },
  pt: { name: "Portuguese", nativeName: "Português", dir: "ltr" },
} as const;

export type LanguageCode = keyof typeof SUPPORTED_LANGUAGES | string;

// Translations type
export type TranslationKey = string;
export type InterpolationParams = Record<string, string | number>;
export type TranslationParams =
  | InterpolationParams
  | { values: InterpolationParams };
export type Translations = Record<string, string | Record<string, string>>;

/**
 * Detect browser's preferred language that we support
 * Falls back to 'en' if no supported language is found
 */
function detectBrowserLanguage(): LanguageCode {
  if (typeof navigator === "undefined") {
    return "en";
  }

  // Get browser languages (ordered by preference)
  const browserLanguages = navigator.languages || [navigator.language];

  for (const browserLang of browserLanguages) {
    // Extract the primary language code (e.g., "en-US" -> "en")
    const primaryLang = browserLang.split("-")[0].toLowerCase();

    // Check if this language is supported
    if (primaryLang in SUPPORTED_LANGUAGES) {
      return primaryLang as LanguageCode;
    }
  }

  // Default to English
  return "en";
}

/**
 * Determine initial language with priority:
 * 1. User's stored preference in localStorage
 * 2. Browser's preferred language (if supported)
 * 3. Default to 'en'
 */
function getInitialLanguage(): LanguageCode {
  // Check localStorage first
  if (typeof localStorage !== "undefined") {
    const storedLang = localStorage.getItem("language");
    if (storedLang && storedLang in SUPPORTED_LANGUAGES) {
      return storedLang as LanguageCode;
    }
  }

  // Fall back to browser language detection
  return detectBrowserLanguage();
}

const initialLanguage: LanguageCode = getInitialLanguage();

export const currentLanguage = writable<LanguageCode>(initialLanguage);

// Store for loaded translations
const translations = writable<Record<LanguageCode, Translations>>({
  en: {},
  ar: {},
  he: {},
  fr: {},
  es: {},
  de: {},
  zh: {},
  ja: {},
  ru: {},
  pt: {},
});

// Loading state
export const isLoadingTranslations = writable(false);

// Direction derived from current language
export const textDirection = derived(currentLanguage, ($lang) => {
  return SUPPORTED_LANGUAGES[$lang]?.dir || "ltr";
});

// Is RTL language
export const isRTL = derived(textDirection, ($dir) => $dir === "rtl");

// Load translations for a language
async function loadTranslations(lang: LanguageCode): Promise<Translations> {
  try {
    const module = await import(`../i18n/${lang}.json`);
    return (module.default || module) as any;
  } catch (error) {
    console.warn(`Failed to load translations for ${lang}:`, error);
    // Fallback to English if not already trying English
    if (lang !== "en") {
      try {
        const enModule = await import("../i18n/en.json");
        return (enModule.default || enModule) as any;
      } catch {
        return {};
      }
    }
    return {};
  }
}

// Set language and load translations
export async function setLanguage(lang: LanguageCode): Promise<void> {
  if (!(lang in SUPPORTED_LANGUAGES)) {
    console.warn(`Language ${lang} is not supported`);
    return;
  }

  isLoadingTranslations.set(true);

  try {
    const langTranslations = await loadTranslations(lang);

    translations.update((trans) => ({
      ...trans,
      [lang]: langTranslations,
    }));

    currentLanguage.set(lang);

    // Persist to localStorage
    if (typeof localStorage !== "undefined") {
      localStorage.setItem("language", lang);
    }

    // Update document direction
    if (typeof document !== "undefined") {
      const dir = SUPPORTED_LANGUAGES[lang].dir;
      document.documentElement.dir = dir;
      document.documentElement.lang = lang;
      document.body.classList.toggle("rtl", dir === "rtl");
      document.body.classList.toggle("ltr", dir === "ltr");
    }
  } catch (error) {
    console.error(`Failed to set language to ${lang}:`, error);
  } finally {
    isLoadingTranslations.set(false);
  }
}

// Get nested value from object using dot notation
function getNestedValue(obj: any, path: string): string | undefined {
  const keys = path.split(".");
  let result = obj;

  for (const key of keys) {
    if (result && typeof result === "object" && key in result) {
      result = result[key];
    } else {
      return undefined;
    }
  }

  return typeof result === "string" ? result : undefined;
}

// Helper to extract interpolation params
function extractParams(
  params?: TranslationParams
): InterpolationParams | undefined {
  if (!params) return undefined;
  if ("values" in params && typeof params.values === "object") {
    return params.values;
  }
  return params as InterpolationParams;
}

// Translation function
export function t(key: string, params?: TranslationParams): string {
  const lang = get(currentLanguage);
  const allTranslations = get(translations);
  const langTranslations = allTranslations[lang] || {};

  // Try to get translation for current language
  let text = getNestedValue(langTranslations, key);

  // Fallback to English if not found
  if (text === undefined && lang !== "en") {
    const enTranslations = allTranslations.en || {};
    text = getNestedValue(enTranslations, key);
  }

  // Return key if no translation found
  if (text === undefined) {
    return key;
  }

  // Replace parameters
  const interpolation = extractParams(params);
  if (interpolation) {
    Object.entries(interpolation).forEach(([paramKey, value]) => {
      text = text!.replace(new RegExp(`{${paramKey}}`, "g"), String(value));
    });
  }

  return text;
}

// Reactive translation store for use in components
export const _ = derived(
  [currentLanguage, translations],
  ([$lang, $translations]) => {
    return (key: string, params?: TranslationParams): string => {
      const langTranslations = $translations[$lang] || {};

      // Try to get translation for current language
      let text = getNestedValue(langTranslations, key);

      // Fallback to English if not found
      if (text === undefined && $lang !== "en") {
        const enTranslations = $translations.en || {};
        text = getNestedValue(enTranslations, key);
      }

      // Return key if no translation found
      if (text === undefined) {
        return key;
      }

      // Replace parameters
      const interpolation = extractParams(params);
      if (interpolation) {
        Object.entries(interpolation).forEach(([paramKey, value]) => {
          text = text!.replace(new RegExp(`{${paramKey}}`, "g"), String(value));
        });
      }

      return text;
    };
  }
);

// Initialize translations on load
export async function initializeI18n(): Promise<void> {
  const lang = get(currentLanguage);
  await setLanguage(lang);
}

/**
 * Sync language with user's account preference from backend.
 * Call this after user logs in to apply their saved preference.
 *
 * Priority:
 * 1. User's account preference (if set)
 * 2. Keep current language (localStorage or browser detected)
 *
 * @param accountLanguage - The preferred_language from the user's account (may be empty)
 */
export async function syncLanguageWithAccount(
  accountLanguage: string | undefined
): Promise<void> {
  // If user has a saved preference in their account, use it
  if (accountLanguage && accountLanguage in SUPPORTED_LANGUAGES) {
    const lang = accountLanguage as LanguageCode;
    const current = get(currentLanguage);

    // Only update if different
    if (lang !== current) {
      await setLanguage(lang);
    }
  }
  // Otherwise keep current language (already set from localStorage or browser detection)
}

/**
 * Change language and optionally sync to backend account.
 * Use this when user explicitly changes language in settings.
 *
 * @param lang - The language code to switch to
 * @param syncToBackend - Whether to save to user's account (default: true)
 */
export async function changeLanguage(
  lang: LanguageCode,
  syncToBackend: boolean = true
): Promise<{ success: boolean; error?: string }> {
  if (!(lang in SUPPORTED_LANGUAGES)) {
    return { success: false, error: `Language ${lang} is not supported` };
  }

  // Set language locally
  await setLanguage(lang);

  // Sync to backend if requested
  if (syncToBackend) {
    try {
      // Dynamic import to avoid circular dependency
      const { accountStore } = await import("./account");
      const result = await accountStore.updatePreferredLanguage(lang);
      if (!result.success) {
        console.warn("Failed to sync language to backend:", result.error);
        // Language is still set locally, so we consider this a partial success
      }
    } catch (err) {
      console.warn("Failed to sync language to backend:", err);
      // Language is still set locally, so we consider this a partial success
    }
  }

  return { success: true };
}

/**
 * Initialize i18n with user's account preferences.
 * Call this after successful authentication to apply the user's saved language preference.
 *
 * Priority:
 * 1. User's account preference (if set in backend)
 * 2. Browser language (if supported)
 * 3. Default to English
 *
 * If user has no preference saved, browser language will be saved to their account.
 */
export async function initializeI18nWithAccount(): Promise<void> {
  try {
    // Dynamic import to avoid circular dependency
    const { accountStore } = await import("./account");
    const result = await accountStore.getAccount();

    if (result.success && result.account) {
      const accountLang = result.account.preferredLanguage;

      if (accountLang && accountLang in SUPPORTED_LANGUAGES) {
        // User has a saved preference, use it
        await setLanguage(accountLang as LanguageCode);
      } else {
        // No preference saved, check browser language
        const browserLang = detectBrowserLanguage();
        if (browserLang) {
          // Use browser language and save to account
          await setLanguage(browserLang);
          // Save browser preference to account (fire and forget)
          accountStore.updatePreferredLanguage(browserLang).catch((err) => {
            console.warn(
              "Failed to save browser language preference to account:",
              err
            );
          });
        }
        // If no browser language detected, current language (default) is already set
      }
    }
  } catch (err) {
    console.warn("Failed to initialize i18n with account:", err);
    // Keep current language setting on error
  }
}

// Export language info helpers
export function getLanguageInfo(lang: LanguageCode) {
  return SUPPORTED_LANGUAGES[lang];
}

export function getLanguageDirection(lang: LanguageCode): "ltr" | "rtl" {
  return SUPPORTED_LANGUAGES[lang]?.dir || "ltr";
}

export function isRTLLanguage(lang: LanguageCode): boolean {
  return getLanguageDirection(lang) === "rtl";
}

// Error message patterns to translation key mappings
const ERROR_PATTERNS: Array<{
  pattern: RegExp;
  key: string;
  extractParams?: (match: RegExpMatchArray) => Record<string, string>;
}> = [
  // Verification/Auth errors
  {
    pattern: /unable to send verification code/i,
    key: "errors.verificationSendFailed",
  },
  {
    pattern: /verification code.*invalid|invalid.*verification code/i,
    key: "errors.verificationCodeInvalid",
  },
  {
    pattern: /verification code.*expired|expired.*verification code/i,
    key: "errors.verificationCodeExpired",
  },
  {
    pattern: /too many verification attempts/i,
    key: "errors.tooManyVerificationAttempts",
  },
  {
    pattern: /invalid.*reset.*token|reset.*token.*invalid/i,
    key: "errors.resetTokenInvalid",
  },
  {
    pattern: /reset.*token.*expired|expired.*reset.*token/i,
    key: "errors.resetTokenExpired",
  },
  {
    pattern: /phone.*verification.*required/i,
    key: "errors.phoneVerificationRequired",
  },
  {
    pattern: /password.*must.*be.*at.*least/i,
    key: "registration.passwordMinLength",
  },

  {
    pattern: /invalid credentials|invalid.*password|password.*invalid/i,
    key: "errors.invalidCredentials",
  },
  { pattern: /account.*locked|locked.*account/i, key: "errors.accountLocked" },
  {
    pattern: /account.*disabled|disabled.*account/i,
    key: "errors.accountDisabled",
  },
  {
    pattern: /session.*expired|expired.*session/i,
    key: "errors.sessionExpired",
  },
  { pattern: /unauthorized|not authorized/i, key: "errors.accessDenied" },
  { pattern: /login.*failed|failed.*login/i, key: "auth.loginFailed" },
  {
    pattern: /verification.*failed|failed.*verification/i,
    key: "auth.verificationFailed",
  },

  // Registration/Account errors
  {
    pattern: /email.*already.*registered|already.*registered.*email/i,
    key: "errors.emailAlreadyRegistered",
  },
  {
    pattern: /username.*already.*taken|already.*taken.*username/i,
    key: "errors.usernameAlreadyTaken",
  },
  {
    pattern: /phone.*already.*registered|already.*registered.*phone/i,
    key: "errors.phoneAlreadyRegistered",
  },
  {
    pattern: /invalid.*email.*address|email.*address.*invalid/i,
    key: "errors.invalidEmailAddress",
  },
  {
    pattern: /invalid.*phone.*number|phone.*number.*invalid/i,
    key: "errors.invalidPhoneNumber",
  },
  { pattern: /phone.*number.*too.*short/i, key: "validation.phoneTooShort" },
  { pattern: /phone.*number.*too.*long/i, key: "validation.phoneTooLong" },

  // Connection/Network errors
  {
    pattern: /unavailable|service.*unavailable/i,
    key: "errors.serviceUnavailable",
  },
  {
    pattern: /connection.*refused|refused.*connection/i,
    key: "errors.connectionRefused",
  },
  {
    pattern: /connection.*timeout|timeout.*connection/i,
    key: "errors.connectionTimeout",
  },
  { pattern: /network.*error|error.*network/i, key: "errors.networkError" },
  { pattern: /failed to fetch|fetch.*failed/i, key: "errors.networkError" },

  // Peer/Device errors
  { pattern: /peer.*not.*found|not.*found.*peer/i, key: "errors.peerNotFound" },
  {
    pattern: /peer.*limit.*reached|limit.*reached.*peer/i,
    key: "errors.peerLimitReached",
  },
  {
    pattern: /device.*name.*required|required.*device.*name/i,
    key: "errors.deviceNameRequired",
  },
  { pattern: /failed to add peer/i, key: "errors.failedToAddPeer" },
  { pattern: /failed to remove peer/i, key: "errors.failedToRemovePeer" },

  // SSH/Winbox errors
  {
    pattern: /ssh.*connection.*failed|failed.*ssh.*connection/i,
    key: "errors.sshConnectionFailed",
  },
  {
    pattern: /ssh.*authentication.*failed|failed.*ssh.*authentication/i,
    key: "errors.sshAuthFailed",
  },
  {
    pattern: /winbox.*connection.*failed|failed.*winbox.*connection/i,
    key: "errors.winboxConnectionFailed",
  },
  {
    pattern: /failed to create.*session/i,
    key: "errors.failedToCreateSession",
  },
  { pattern: /failed to load.*config/i, key: "errors.failedToLoadConfig" },

  // Payment/Billing errors
  { pattern: /payment.*failed|failed.*payment/i, key: "errors.paymentFailed" },
  {
    pattern: /subscription.*required|required.*subscription/i,
    key: "errors.subscriptionRequired",
  },
  { pattern: /billing.*error/i, key: "errors.billingError" },

  // Rate limiting
  {
    pattern: /rate.*limit|too many requests/i,
    key: "errors.rateLimitExceeded",
  },

  // Server errors
  {
    pattern: /internal.*server.*error|server.*error.*internal/i,
    key: "errors.serverError",
  },
  { pattern: /something went wrong/i, key: "errors.somethingWentWrong" },
];

/**
 * Parses an error message (potentially from gRPC/backend) and returns a translated message.
 * Handles gRPC error format: "code = ErrorCode desc = Error description"
 *
 * @param error - The error string, Error object, or any value
 * @returns The translated error message
 */
export function translateError(error: unknown): string {
  if (!error) {
    return t("errors.somethingWentWrong");
  }

  // Convert to string if needed
  let errorStr =
    typeof error === "string"
      ? error
      : (error as Error)?.message || String(error);

  // Remove gRPC prefixes like "code = Unavailable desc = "
  const grpcMatch = errorStr.match(/code\s*=\s*\w+\s*desc\s*=\s*(.*)/i);
  if (grpcMatch) {
    errorStr = grpcMatch[1].trim();
  }

  // Also handle "rpc error: code = X desc = Y" format
  const rpcMatch = errorStr.match(
    /rpc error:\s*code\s*=\s*\w+\s*desc\s*=\s*(.*)/i
  );
  if (rpcMatch) {
    errorStr = rpcMatch[1].trim();
  }

  // Fast path: ALL_CAPS_UNDERSCORE error codes map directly to errorCodes.*
  if (/^[A-Z][A-Z0-9_]{2,}$/.test(errorStr)) {
    const codeKey = `errorCodes.${errorStr}`;
    const translated = t(codeKey);
    if (translated !== codeKey) return translated;
  }

  // Try to match against known error patterns
  for (const { pattern, key, extractParams } of ERROR_PATTERNS) {
    const match = errorStr.match(pattern);
    if (match) {
      const params = extractParams ? extractParams(match) : undefined;
      const translated = t(key, params);
      // If translation exists (key !== translated), return it
      if (translated !== key) {
        return translated;
      }
    }
  }

  // Fallback: return a generic error message
  return t("errors.somethingWentWrong");
}

/**
 * Reactive version of translateError for use in Svelte components
 * Use like: $translateError$(error)
 */
export const translateError$ = derived(
  [currentLanguage, translations],
  ([$lang, $translations]) => {
    return (error: unknown): string => {
      if (!error) {
        return t("errors.somethingWentWrong");
      }

      let errorStr =
        typeof error === "string"
          ? error
          : (error as Error)?.message || String(error);

      // Remove gRPC prefixes
      const grpcMatch = errorStr.match(/code\s*=\s*\w+\s*desc\s*=\s*(.*)/i);
      if (grpcMatch) {
        errorStr = grpcMatch[1].trim();
      }

      const rpcMatch = errorStr.match(
        /rpc error:\s*code\s*=\s*\w+\s*desc\s*=\s*(.*)/i
      );
      if (rpcMatch) {
        errorStr = rpcMatch[1].trim();
      }

      // Fast path: ALL_CAPS_UNDERSCORE error codes map directly to errorCodes.*
      if (/^[A-Z][A-Z0-9_]{2,}$/.test(errorStr)) {
        const codeKey = `errorCodes.${errorStr}`;
        const langTranslations = $translations[$lang] || {};
        let codeText = getNestedValue(langTranslations, codeKey);
        if (codeText === undefined && $lang !== "en") {
          const enTranslations = $translations.en || {};
          codeText = getNestedValue(enTranslations, codeKey);
        }
        if (codeText !== undefined) return codeText;
      }

      // Try to match against known error patterns
      for (const { pattern, key, extractParams } of ERROR_PATTERNS) {
        const match = errorStr.match(pattern);
        if (match) {
          const params = extractParams ? extractParams(match) : undefined;
          const langTranslations = $translations[$lang] || {};
          let text = getNestedValue(langTranslations, key);

          if (text === undefined && $lang !== "en") {
            const enTranslations = $translations.en || {};
            text = getNestedValue(enTranslations, key);
          }

          if (text !== undefined) {
            if (params) {
              Object.entries(params).forEach(([paramKey, value]) => {
                text = text!.replace(
                  new RegExp(`{${paramKey}}`, "g"),
                  String(value)
                );
              });
            }
            return text;
          }
        }
      }

      // Fallback to generic error
      const langTranslations = $translations[$lang] || {};
      let fallback = getNestedValue(
        langTranslations,
        "errors.somethingWentWrong"
      );
      if (fallback === undefined && $lang !== "en") {
        const enTranslations = $translations.en || {};
        fallback = getNestedValue(enTranslations, "errors.somethingWentWrong");
      }
      const regexCode = /code\s*=\s*(\w+)/i.exec(String(error));
      if (regexCode) {
        console.error(`Error code: ${regexCode[1]}`);
        return fallback || t("errors."+regexCode[1].toLowerCase()) || t("errors.somethingWentWrong");
      }
      return fallback || t("errors.somethingWentWrong");
    };
  }
);
