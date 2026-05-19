<script lang="ts">
  import Login from "./apps/Login.svelte";
  import ResetPassword from "./apps/ResetPassword.svelte";
  import Privacy from "./apps/Privacy.svelte";
  import Terms from "./apps/Terms.svelte";
  import Desktop from "./Desktop.svelte";
  import Taskbar from "./Taskbar.svelte";
  import OAuth2Consent from "./apps/OAuth2Consent.svelte";
  import Activate from "./apps/Activate.svelte";
  import OAuth2Error from "./apps/OAuth2Error.svelte";
  import AdminUsers from "./apps/Admin/Users.svelte";
  import Copilot from "./apps/Copilot.svelte";
  import { onMount } from "svelte";
  import { authStore } from "./store/auth";
  import { wsStore } from "./store/websocket";
  import { websshStore } from "./store/webssh";
  import { WS_URL } from "./config";
  import { initializeI18nWithAccount, _ } from "./store/i18n";
  import { openedApps, activeThing, bringToFront } from "./store/store";

  // ============================================================
  // Route Registry — single source of truth for all routes
  // ============================================================
  //
  // Route types:
  //   public    — accessible without auth; authenticated users are sent to
  //               desktop (or their saved returnUrl)
  //   legal     — always accessible regardless of auth; no redirects
  //   protected — requires authentication; unauthenticated users are saved
  //               to returnUrl then redirected to login
  //   oauth2    — self-contained auth flow; always accessible; skips the
  //               WebSocket / gRPC session check on page load

  type RouteType = "public" | "legal" | "protected" | "oauth2";

  type Page =
    | "login"
    | "reset-password"
    | "privacy"
    | "terms"
    | "desktop"
    | "oauth2-consent"
    | "activate"
    | "oauth2-error"
    | "admin-users"
    | "copilot";

  interface Route {
    page: Page;
    type: RouteType;
    /** Additional hash slugs that resolve to this page. */
    aliases?: string[];
  }

  const ROUTES: Route[] = [
    // ── Public (unauthenticated-only; authenticated users → desktop) ──────
    { page: "login",           type: "public" },
    { page: "reset-password",  type: "public" },
    // ── Legal (always accessible, no redirect) ────────────────────────────
    { page: "privacy",         type: "legal"  },
    { page: "terms",           type: "legal"  },
    // ── Protected (requires auth) ─────────────────────────────────────────
    { page: "desktop",         type: "protected", aliases: ["dashboard", "settings"] },
    { page: "admin-users",     type: "protected" },
    { page: "copilot",         type: "protected" },
    // ── OAuth2 (self-managed auth, bypass WebSocket wait) ─────────────────
    { page: "oauth2-consent",  type: "oauth2" },
    { page: "activate",        type: "oauth2" },
    { page: "oauth2-error",    type: "oauth2" },
  ];

  // Build O(1) lookup maps from the registry
  const routeMap  = new Map<string, Route>(ROUTES.map(r => [r.page, r]));
  const aliasMap  = new Map<string, Page>(
    ROUTES.flatMap(r => (r.aliases ?? []).map(a => [a, r.page] as [string, Page]))
  );
  const knownPages = new Set<string>(ROUTES.map(r => r.page));

  // ============================================================
  // URL Resolution
  // ============================================================

  let oauth2ErrorCode = "";
  let oauth2ErrorDesc = "";

  /**
   * Reads window.location and returns the canonical Page for the current URL.
   * Resolution priority:
   *   1. ?error= query param       → oauth2-error
   *   2. #hash slug                → route or alias lookup
   *   3. ?code= query param        → oauth2-consent  (callback redirect)
   *   4. pathname slug             → route lookup    (server-rendered links)
   *   5. default                   → login
   */
  function resolveRoute(): Page {
    const search = new URLSearchParams(window.location.search);

    // 1. OAuth2 error carried in query string
    const errorCode = search.get("error");
    if (errorCode) {
      oauth2ErrorCode = errorCode;
      oauth2ErrorDesc = search.get("error_description") ?? "";
      return "oauth2-error";
    }

    // 2. Hash-based routing (primary)
    const hash = window.location.hash.slice(1);
    if (hash) {
      const slug = hash.split("?")[0];

      // Legacy backward compat: agents may still open #device-login?code=…
      // Hand off to the backend handler which redirects to OAuth2 consent.
      if (slug === "device-login") {
        const q = hash.includes("?") ? hash.slice(hash.indexOf("?")) : "";
        window.location.replace(`/device-login${q}`);
        return "login"; // unreachable; navigation replaces the page
      }

      const canonical = aliasMap.get(slug) ?? (knownPages.has(slug) ? (slug as Page) : null);
      if (canonical) return canonical;
    }

    // 3. OAuth2 authorization callback: ?code= without a hash
    if (search.get("code")) {
      return "oauth2-consent";
    }

    // 4. Path-based fallback (e.g. server-side links like /privacy)
    const pathSlug = window.location.pathname.slice(1).split("?")[0];
    if (pathSlug && knownPages.has(pathSlug)) {
      return pathSlug as Page;
    }

    return "login";
  }

  // ============================================================
  // Auth Guard
  // ============================================================

  /**
   * Applies auth rules for the resolved page and returns the page that should
   * actually render.  Navigation side-effects (hash changes, sessionStorage)
   * happen here.
   */
  function applyGuard(page: Page, authenticated: boolean): Page {
    const route = routeMap.get(page) ?? { page, type: "public" as RouteType };

    switch (route.type) {
      case "oauth2":
      case "legal":
        // Always pass through — no auth dependency
        return page;

      case "public":
        if (authenticated) {
          // Authenticated user landed on a public page (login, register, etc.)
          // Check sessionStorage first, then the ?callback= param in the current hash URL.
          const hashFrag = window.location.hash.slice(1);
          const hashSearch = hashFrag.includes("?") ? hashFrag.slice(hashFrag.indexOf("?")) : "";
          const callbackParam = new URLSearchParams(hashSearch).get("callback");
          const returnUrl = sessionStorage.getItem("returnUrl") || (callbackParam ? decodeURIComponent(callbackParam) : null);
          if (returnUrl) {
            sessionStorage.removeItem("returnUrl");
            // Determine the target page from the returnUrl so we can render it
            // immediately without a flash — the hashchange listener will also fire
            // but currentPage will already be correct.
            const retSlug = returnUrl.replace(/^#/, "").split("?")[0];
            const retPage = aliasMap.get(retSlug) ?? (knownPages.has(retSlug) ? (retSlug as Page) : null);
            if (returnUrl.startsWith("/") && !returnUrl.startsWith("/#")) {
              window.location.replace(returnUrl);
            } else {
              window.location.hash = returnUrl;
            }
            return retPage ?? "desktop";
          } else {
            window.location.hash = "#desktop";
          }
          return "desktop";
        }
        return page;

      case "protected":
        if (!authenticated) {
          // Save intended destination and redirect to login
          const intended = `#${page}${window.location.search}`;
          sessionStorage.setItem("returnUrl", intended);
          window.location.hash = "#login";
          return "login";
        }
        return page;
    }
  }

  // ============================================================
  // Router State
  // ============================================================

  let currentPage: Page = "login";
  let isCheckingAuth = true;
  let isInitialized = false;

  $: isAuthenticated = $authStore.user !== null;

  // Re-evaluate guard whenever auth state or initialization changes
  $: if (isInitialized) {
    currentPage = applyGuard(resolveRoute(), isAuthenticated);
  }

  // Convert known path-based URLs (e.g. /privacy, /terms) to hash-based.
  // This ensures server-rendered links and user-typed URLs work correctly.
  {
    const _slug = window.location.pathname.slice(1).split(/[?#]/)[0];
    if (_slug && knownPages.has(_slug) && _slug !== "desktop") {
      window.location.replace("/#" + _slug);
    }
  }

  // Normalize any non-root path back to / + hash (keeps the SPA on a single origin path)
  if (
    window.location.pathname !== "/" &&
    window.location.pathname !== "/index.html"
  ) {
    window.history.replaceState({}, "", "/" + window.location.hash);
  }

  // ============================================================
  // Initialization (onMount)
  // ============================================================

  /** Waits for the WS to be fully ready: socket open + E2E key exchange complete. */
  function waitForWS(timeoutMs = 6000): Promise<boolean> {
    return new Promise<boolean>((resolve) => {
      const unsub = wsStore.subscribe((state) => {
        if (state.status === "connected" && state.encryptionReady) {
          unsub(); resolve(true);
        } else if (state.status === "error") {
          unsub(); resolve(false);
        }
      });
      setTimeout(() => { unsub(); resolve(false); }, timeoutMs);
    });
  }

  /** Run the full WS + session check in the background (for non-blocking routes). */
  async function backgroundSessionCheck() {
    const ok = await waitForWS();
    if (!ok) return;
    const valid = await authStore.checkSession();
    if (valid) {
      await initializeI18nWithAccount();
      websshStore.initSessions();
    }
  }

  onMount(() => {
    const init = async () => {
      const initialPage  = resolveRoute();
      const initialRoute = routeMap.get(initialPage) ?? { type: "public" as RouteType };

      // ── OAuth2 ───────────────────────────────────────────────────────────
      // Self-contained flow; validates session via cookie. No WS needed.
      if (initialRoute.type === "oauth2") {
        currentPage    = initialPage;
        isCheckingAuth = false;
        isInitialized  = true;
        wsStore.connect(WS_URL);
        return;
      }

      // ── Legal (privacy, terms) ────────────────────────────────────────────
      // Always accessible. Show immediately; check session in background so
      // that authenticated users get the right UI.
      if (initialRoute.type === "legal") {
        currentPage    = initialPage;
        isCheckingAuth = false;
        isInitialized  = true;
        wsStore.connect(WS_URL);
        void backgroundSessionCheck();
        return;
      }

      // ── Public (login, register, reset-password) ─────────────────────────
      // Show the page immediately — no loading spinner. If the user already
      // has a valid session the auth reactive block will redirect them once
      // checkSession resolves.
      if (initialRoute.type === "public") {
        currentPage    = initialPage;
        isCheckingAuth = false;
        isInitialized  = true;
        wsStore.connect(WS_URL);
        void backgroundSessionCheck();
        return;
      }

      // ── Protected (desktop + all authenticated pages) ────────────────────
      // Must wait for WS + session check before rendering.
      wsStore.connect(WS_URL);

      const wsReady = await waitForWS();
      if (!wsReady) {
        console.error("❌ [Router] WebSocket not ready — proceeding unauthenticated");
        isCheckingAuth = false;
        isInitialized  = true;
        return;
      }

      const sessionValid = await authStore.checkSession();

      if (sessionValid) {
        await initializeI18nWithAccount();
        websshStore.initSessions();

        // Auto-open AddPeer for first-time users (cookie set by backend)
        const firsttime = document.cookie.split("; ").find(c => c.startsWith("firsttime="));
        if (firsttime) {
          $openedApps = ["AddPeer"];
          $activeThing = "AddPeer";
          bringToFront("AddPeer");
          document.cookie = "firsttime=; path=/; max-age=0";
        }
      }

      isCheckingAuth = false;
      isInitialized  = true;
    };

    init();

    // ── Hash / popstate listener ──────────────────────────────────────────
    // Runs on every client-side navigation after initialization.
    const handleRouteChange = () => {
      if (!isInitialized) return;
      currentPage = applyGuard(resolveRoute(), isAuthenticated);
    };

    window.addEventListener("hashchange", handleRouteChange);
    window.addEventListener("popstate",   handleRouteChange);

    return () => {
      window.removeEventListener("hashchange", handleRouteChange);
      window.removeEventListener("popstate",   handleRouteChange);
    };
  });
</script>

{#if isCheckingAuth}
  <div class="loading-container">
    <div class="loading-spinner" />
    <p>{$_("common.loading")}</p>
  </div>
{:else if currentPage === "login"}
  <Login />
{:else if currentPage === "reset-password"}
  <ResetPassword />
{:else if currentPage === "privacy"}
  <Privacy />
{:else if currentPage === "terms"}
  <Terms />
{:else if currentPage === "desktop"}
  <Desktop />
  <Taskbar />
{:else if currentPage === "admin-users"}
  <AdminUsers />
{:else if currentPage === "copilot"}
  <Copilot />
{:else if currentPage === "oauth2-consent"}
  <OAuth2Consent />
{:else if currentPage === "activate"}
  <Activate />
{:else if currentPage === "oauth2-error"}
  <OAuth2Error errorCode={oauth2ErrorCode} errorDescription={oauth2ErrorDesc} />
{:else}
  <Login />
{/if}

<style global>
  :global(*) {
    box-sizing: border-box;
  }

  :global(body) {
    margin: 0;
    padding: 0;
    font-family: "Segoe UI Variable", "Segoe UI", Tahoma, Geneva, Verdana,
      sans-serif;
    background: rgb(var(--bg1));
    color: rgb(var(--clr));
    overflow: hidden; /* Prevent body scroll, handle in apps */
  }

  .loading-container {
    min-height: 100dvh;
    display: flex;
    flex-direction: column;
    align-items: center;
    justify-content: center;
    background: var(--mica);
    gap: 24px;
  }

  .loading-spinner {
    width: 40px;
    height: 40px;
    border: 3px solid rgb(var(--clr) / 10%);
    border-top-color: rgb(var(--clrPrm));
    border-radius: 50%;
    animation: spin 0.8s cubic-bezier(0.4, 0, 0.2, 1) infinite;
  }

  @keyframes spin {
    to {
      transform: rotate(360deg);
    }
  }

  .loading-container p {
    color: rgb(var(--clr) / 60%);
    font-size: 14px;
    font-weight: 500;
    letter-spacing: 0.02em;
    animation: pulse 2s infinite ease-in-out;
  }

  @keyframes pulse {
    0%,
    100% {
      opacity: 0.6;
    }
    50% {
      opacity: 1;
    }
  }
</style>
