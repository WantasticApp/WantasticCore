<script lang="ts">
  import { draggable } from "@neodrag/svelte";
  import { scale } from "svelte/transition";
  import { onMount, onDestroy } from "svelte";
  import Titlebar from "$components/shared/Titlebar.svelte";
  import { accountStore } from "$store/account";
  import { dashboardStore } from "$store/dashboard";
  import { copy, copyToClipboard } from "$lib/clipboard";
  import { peerStore } from "$store/peer";
  import { authStore } from "$store/auth";
  import { snapshotStore, type DeviceSnapshot } from "$store/snapshot";
  import { activeThing, appZIndexes, bringToFront, openedApps } from "$store/store";
  import {
    widgetStore,
    WIDGET_DEFINITIONS,
    type WidgetDefinition,
    type WidgetId,
  } from "$store/widgets";
  import type { AccountInfo, TOTPSetup, SessionInfo } from "$store/account";
  import {
    changeLanguage,
    currentLanguage,
    SUPPORTED_LANGUAGES,
    type LanguageCode,
    _,
    t,
  } from "$store/i18n";
  import { TextBox, ComboBox, ToggleSwitch, Button } from "fluent-svelte";

  let activeTab = "profile";
  let isMaximized = false;
  let isMinimized = false;

  // Z-index for window stacking
  $: zIndex = $appZIndexes["Account"] || 100;

  // Watch activeThing to restore when clicked from taskbar
  $: if ($activeThing === "Account" && isMinimized) {
    isMinimized = false;
  }

  // Bring to front when activated
  $: if ($activeThing === "Account") {
    bringToFront("Account");
  }

  function handleFocus() {
    $activeThing = "Account";
    bringToFront("Account");
  }

  // Profile form
  let fullName = "";
  let email = "";
  let phone = "";

  // Password form
  let currentPassword = "";
  let newPassword = "";
  let confirmPassword = "";

  // TOTP Setup
  let totpSetup: TOTPSetup | null = null;
  let totpVerifyCode = "";
  let twoFAMethod: "none" | "totp" | "email" | "sms" = "none";
  let showTotpSetup = false;
  let totpStatusText = $_("account.loadingAccount");
  let totpStatusColor = "#6b7280";
  let phoneMasked = "";

  // Enrollment Script
  let showEnrollmentScript = false;
  let selectedTokenId = "";
  $: selectedToken =
    tokens.find((t) => t.id === selectedTokenId) ||
    (tokens.length > 0 ? tokens[0] : null);

  $: enrollmentCommand = selectedToken
    ? `curl -sSL https://get.wantastic.app/install.sh | sh -s -- ${selectedToken.token}`
    : `curl -sSL https://get.wantastic.app/install.sh | sh -s -- --token YOUR_TOKEN`;

  $: tokenItems = tokens.map((t) => ({ name: t.name, value: t.id }));

  // Auto-select first token if none selected
  $: if (!selectedTokenId && tokens.length > 0) {
    selectedTokenId = tokens[0].id;
  }

  // MCP
  let mcpConfig: any = null;
  let apiKeys: any[] = [];
  let showCreateKey = false;
  let newKeyName = "";
  let generatedKey: any = null; // To show the full token once

  // Sessions
  let sessions: SessionInfo[] = [];
  let sessionsLoading = false;

  // Secrets
  let tokens: any[] = [];
  let secretsLoading = false;
  let showCreateSecret = false;
  let newSecretName = "";
  let newSecretExpiresDays = 30; // Realistic default
  let newSecretMaxUses = 10; // Realistic default

  $: tokens = $peerStore.tokens;

  // State
  let loading = false;
  let statusMessage = "";
  let messageType: "success" | "error" | "info" = "success";
  let statusBar = "Ready";
  let statusTimeout: ReturnType<typeof setTimeout> | null = null;

  // Check if viewing a shared account
  $: isViewingShared = false;
  $: viewingAccountName = $authStore.user?.fullName;

  // Dashboard Stats for Overview
  $: dashboardState = $dashboardStore;
  $: stats = dashboardState.stats;
  $: blockCount = stats?.block_count || 0;
  $: networkBlocks = stats?.network_blocks || [];
  $: orderedWidgets = [...$widgetStore.widgets].sort(
    (left, right) => left.order - right.order,
  );

  // Destructure store values directly to reduce reactive overhead
  $: ({ account: accountData } = $accountStore);

  onMount(async () => {
    await loadAccount();
    // Load dashboard stats for overview
    if (!stats) {
      await dashboardStore.getDashboard();
    }
  });

  onDestroy(() => {
    // Clear any pending status message timeouts
    if (statusTimeout) {
      clearTimeout(statusTimeout);
      statusTimeout = null;
    }
  });

  async function loadAccount() {
    loading = true;
    statusBar = "Loading account...";
    const result = await accountStore.getAccount();
    if (result.success && result.account) {
      fullName = result.account.fullName || "";
      email = result.account.email || "";
      phone = result.account.phone || "";

      // 2FA status
      // 2FA status
      if (result.account.twoFAMethod && result.account.twoFAMethod !== "none") {
        twoFAMethod = result.account.twoFAMethod as any;
        if (twoFAMethod === "totp") {
          totpStatusText = $_("account.authenticatorApp");
          totpStatusColor = "var(--success)";
        } else if (twoFAMethod === "email") {
          totpStatusText = $_("auth.email");
          totpStatusColor = "var(--success)";
        } else if (twoFAMethod === "sms") {
          totpStatusText = $_("account.sms");
          totpStatusColor = "var(--success)";
        }
      } else if (result.account.totpEnabled) {
        totpStatusText = $_("account.authenticatorApp");
        totpStatusColor = "var(--success)";
        twoFAMethod = "totp";
      } else {
        totpStatusText = $_("common.disabled");
        totpStatusColor = "var(--error)";
        twoFAMethod = "none";
      }

      phoneMasked = phone ? "***-***-" + phone.slice(-4) : "no phone";
    }
    loading = false;
    statusBar = "Ready";
  }

  function switchTab(tabId: string) {
    activeTab = tabId;
    if (tabId === "sessions" && sessions.length === 0) {
      loadSessions();
    }
    if (tabId === "tokens") {
      loadSecrets();
    }
    if (tabId === "mcp") {
      loadMCPConfig();
      loadAPIKeys();
    }
    if (tabId === "snapshots") {
      snapshotStore.list();
      // Make sure peers are loaded so we can show tag/peer filters
      if (!$peerStore.peers || $peerStore.peers.length === 0) {
        peerStore.listPeers().catch(() => {});
      }
    }
    if (tabId === "widgets") {
      widgetStore.refreshAll().catch(() => {});
    }
  }

  function widgetDefinition(widgetId: WidgetId): WidgetDefinition | undefined {
    return WIDGET_DEFINITIONS.find((widget) => widget.id === widgetId);
  }

  function widgetTitle(widgetId: WidgetId): string {
    const definition = widgetDefinition(widgetId);
    return definition ? $_(definition.titleKey) : widgetId;
  }

  function widgetDescription(widgetId: WidgetId): string {
    const definition = widgetDefinition(widgetId);
    return definition ? $_(definition.descriptionKey) : widgetId;
  }

  $: snapshotState = $snapshotStore;

  // ── Snapshots filters ────────────────────────────────────────────────────
  // The snapshot row carries `name` (set to the peer name during backup) and
  // `created_at`. We don't currently store peer_id on the snapshot, so peer/tag
  // filtering works by matching `snap.name` against the peers list.
  let snapSearch = "";
  let snapPeerFilter = ""; // peer.id, "" = any
  let snapTagFilter = "";  // tag string, "" = any
  let snapFromDate = "";   // YYYY-MM-DD
  let snapToDate = "";     // YYYY-MM-DD

  // Derived: union of all distinct tags across peers (sorted, lowercased once)
  $: allPeerTags = (() => {
    const seen = new Set<string>();
    for (const p of $peerStore.peers || []) {
      for (const t of p.tags || []) seen.add(t);
    }
    return [...seen].sort();
  })();

  // Build name→tags map so we can match a snapshot.name back to its peer's tags
  $: peerTagsByName = (() => {
    const m: Record<string, string[]> = {};
    for (const p of $peerStore.peers || []) {
      if (p.name) m[p.name.toLowerCase()] = p.tags || [];
    }
    return m;
  })();

  $: filteredSnapshots = (() => {
    let rows = snapshotState.snapshots || [];
    const q = snapSearch.trim().toLowerCase();
    if (q) {
      rows = rows.filter((s) =>
        (s.name || "").toLowerCase().includes(q) ||
        (s.serial_number || "").toLowerCase().includes(q) ||
        (s.product_class || "").toLowerCase().includes(q),
      );
    }
    if (snapPeerFilter) {
      const peer = ($peerStore.peers || []).find((p) => p.id === snapPeerFilter);
      const target = peer?.name?.toLowerCase() || "";
      rows = rows.filter((s) => (s.name || "").toLowerCase() === target);
    }
    if (snapTagFilter) {
      rows = rows.filter((s) => {
        const tags = peerTagsByName[(s.name || "").toLowerCase()] || [];
        return tags.includes(snapTagFilter);
      });
    }
    if (snapFromDate) {
      const fromTs = Math.floor(new Date(snapFromDate).getTime() / 1000);
      rows = rows.filter((s) => (s.created_at || 0) >= fromTs);
    }
    if (snapToDate) {
      // include the whole "to" day (end of day = +86399s)
      const toTs = Math.floor(new Date(snapToDate).getTime() / 1000) + 86399;
      rows = rows.filter((s) => (s.created_at || 0) <= toTs);
    }
    return rows;
  })();

  function clearSnapshotFilters() {
    snapSearch = "";
    snapPeerFilter = "";
    snapTagFilter = "";
    snapFromDate = "";
    snapToDate = "";
  }

  async function loadSessions() {
    sessionsLoading = true;
    statusBar = $_("account.loadingSessions");
    const result = await accountStore.getSessions();
    if (result.success && result.sessions) {
      sessions = result.sessions;
    } else {
      showMessage(
        $_("account.failedToLoadSessions", { values: { error: result.error } }),
        "error",
      );
    }
    sessionsLoading = false;
    statusBar = "Ready";
  }

  async function loadAPIKeys() {
    try {
      const res = await accountStore.listAPIKeys();
      if (res.success) {
        apiKeys = res.keys || [];
      }
    } catch (err) {
      console.error($_("account.failedToListKeys"), err);
    }
  }

  async function handleCreateAPIKey() {
    if (!newKeyName) return;
    try {
      const res = await accountStore.createAPIKey(newKeyName);
      if (res.success) {
        generatedKey = res.key;
        // Construct a display-friendly key object that matches the table expectation
        // The API might return the full object, but let's ensure 'prefix' is set if missing
        const newKeyDisplay = {
          ...res.key,
          prefix: res.key.token
            ? res.key.token.substring(0, 6) + "..."
            : res.key.prefix || "......",
        };

        apiKeys = [...apiKeys, newKeyDisplay];
        newKeyName = "";
        showCreateKey = false;

        // Auto-update config view if needed
        if (mcpConfig) {
          updateMCPConfigWithToken(generatedKey.token);
        }
        showMessage($_("account.keyCreated"), "success");
      } else {
        showMessage(res.error || $_("account.failedToCreateKey"), "error");
      }
    } catch (err) {
      console.error(err);
      showMessage($_("account.errorCreatingKey"), "error");
    }
  }

  async function handleRevokeAPIKey(id: string) {
    if (!confirm($_("account.revokeKeyConfirm"))) return;
    try {
      const res = await accountStore.revokeAPIKey(id);
      if (res.success) {
        apiKeys = apiKeys.filter((k) => k.id !== id);
        showMessage($_("account.keyRevoked"), "success");
      } else {
        showMessage(res.error || $_("account.failedToRevokeKey"), "error");
      }
    } catch (err) {
      console.error(err);
      showMessage($_("account.errorRevokingKey"), "error");
    }
  }

  function updateMCPConfigWithToken(token: string) {
    if (!mcpConfig) return;
    // Create a copy
    const newConfig = JSON.parse(JSON.stringify(mcpConfig));
    newConfig.mcpServers["wantastic"].headers = {
      Authorization: "Bearer " + token,
    };
    mcpConfig = newConfig;
  }

  async function loadMCPConfig() {
    statusBar = $_("account.loadingMCPConfig");
    try {
      const result = await accountStore.getMCPConfig();

      if (!result.success || !result.config) {
        throw new Error(result.error || $_("account.failedToFetchConfig"));
      }

      mcpConfig = result.config;
      // Force correct SSE endpoint derived from current window location
      mcpConfig.mcpServers["wantastic"].serverUrl =
        window.location.protocol + "//" + window.location.host + "/sse";

      // If we have a generated key, use it
      if (generatedKey) {
        updateMCPConfigWithToken(generatedKey.token);
      }
    } catch (e: any) {
      showMessage(
        $_("account.errorLoadingMCPConfig", { values: { error: e.message } }),
        "error",
      );
    }
    statusBar = $_("common.ready");
  }

  async function handleDeleteSession(sessionId: string) {
    // if (!confirm($_("account.endSessionConfirm"))) {
    //   return;
    // }

    statusBar = $_("account.endingSession");
    const result = await accountStore.deleteSession(sessionId);
    if (result.success) {
      sessions = sessions.filter((s) => s.sessionId !== sessionId);
      showMessage($_("account.sessionEnded"), "success");
    } else {
      showMessage(
        $_("account.failedToEndSession", { values: { error: result.error } }),
        "error",
      );
    }
    statusBar = "Ready";
  }

  async function loadSecrets() {
    if (!$authStore.tenant_id) return;
    secretsLoading = true;
    statusBar = $_("secrets.loadingSecrets");
    try {
      await peerStore.listTokens($authStore.tenant_id);
    } catch (err: any) {
      showMessage(
        $_("account.failedToLoadSecrets", { values: { error: err.message } }),
        "error",
      );
    }
    secretsLoading = false;
    statusBar = "Ready";
  }

  async function handleCreateSecret() {
    if (!newSecretName.trim()) {
      showMessage($_("account.secretNameRequired"), "error");
      return;
    }
    statusBar = $_("account.creatingSecret");
    try {
      await peerStore.createToken(
        $authStore.tenant_id!,
        newSecretName,
        newSecretExpiresDays,
        newSecretMaxUses,
      );
      newSecretName = "";
      newSecretExpiresDays = 30;
      newSecretMaxUses = 10;
      showCreateSecret = false;
      showMessage($_("secrets.secretCreated"), "success");
    } catch (err: any) {
      showMessage(
        $_("account.failedToCreateSecret", { values: { error: err.message } }),
        "error",
      );
    }
    statusBar = $_("common.ready");
  }

  async function handleDeleteSecret(secretId: string) {
    if (!confirm($_("secrets.deleteConfirm"))) {
      return;
    }
    statusBar = $_("account.deletingSecret");
    try {
      await peerStore.deleteToken($authStore.tenant_id!, secretId);
      showMessage($_("secrets.secretDeleted"), "success");
    } catch (err: any) {
      showMessage(
        $_("account.failedToDeleteSecret", { values: { error: err.message } }),
        "error",
      );
    }
    statusBar = $_("common.ready");
  }

  function formatSessionDate(isoDate: string): string {
    if (!isoDate) return "Unknown";
    const date = new Date(isoDate);
    const now = new Date();
    const diffMs = now.getTime() - date.getTime();
    const diffMins = Math.floor(diffMs / 60000);
    const diffHours = Math.floor(diffMs / 3600000);
    const diffDays = Math.floor(diffMs / 86400000);

    if (diffMins < 1) return $_("time.justNow");
    if (diffMins < 60)
      return $_("time.minutesAgo", { values: { count: diffMins } });
    if (diffHours < 24)
      return $_("time.hoursAgo", { values: { count: diffHours } });
    if (diffDays < 7)
      return $_("time.daysAgo", { values: { count: diffDays } });

    return date.toLocaleDateString();
  }

  function getDeviceIcon(deviceType: string): string {
    switch (deviceType?.toLowerCase()) {
      case "mobile":
        return "";
      case "tablet":
        return "";
      default:
        return "💻";
    }
  }

  async function handleSaveProfile() {
    statusBar = $_("account.savingProfile");
    const result = await accountStore.updateAccount({
      fullName,
      phone,
    });

    if (result.success) {
      showMessage($_("account.profileSaved"), "success");
    } else {
      showMessage(
        $_("account.failed", { values: { error: result.error } }),
        "error",
      );
    }
    statusBar = $_("common.ready");
  }

  async function handlePasswordChange(e: Event) {
    e.preventDefault();

    if (!currentPassword) {
      showMessage($_("account.currentPasswordRequired"), "error");
      return;
    }

    if (newPassword.length < 8) {
      showMessage($_("account.passwordMinLength"), "error");
      return;
    }

    if (newPassword !== confirmPassword) {
      showMessage($_("account.passwordsDoNotMatch"), "error");
      return;
    }

    statusBar = $_("account.changingPassword");
    const result = await accountStore.changePassword(
      currentPassword,
      newPassword,
    );
    if (result.success) {
      showMessage($_("account.passwordChanged"), "success");
      currentPassword = "";
      newPassword = "";
      confirmPassword = "";
    } else {
      showMessage(
        $_("account.failed", { values: { error: result.error } }),
        "error",
      );
    }
    statusBar = $_("common.ready");
  }

  async function handleSetupTOTP() {
    statusBar = $_("account.generatingQR");
    const result = await accountStore.setupTOTP();
    if (result.success && result.setup) {
      totpSetup = result.setup;
    } else {
      showMessage(
        $_("account.failed", { values: { error: result.error } }),
        "error",
      );
    }
    statusBar = $_("common.ready");
  }

  async function handleSave2FA() {
    if (twoFAMethod === "totp") {
      if (!totpVerifyCode || totpVerifyCode.length !== 6) {
        showMessage($_("account.enter6DigitCode"), "error");
        return;
      }

      statusBar = $_("account.verifyingTOTP");
      const result = await accountStore.verifyTOTP(totpVerifyCode);
      if (result.success) {
        showMessage($_("account.twoFAUpdated"), "success");
        totpSetup = null;
        totpVerifyCode = "";
        showTotpSetup = false;
        totpStatusText = $_("account.authenticatorApp");
        totpStatusColor = "var(--success)";
      } else {
        showMessage(
          $_("account.failed", { values: { error: result.error } }),
          "error",
        );
      }
    } else if (twoFAMethod === "email") {
      const result = await accountStore.setup2FA("email");
      if (result.success) {
        showMessage($_("account.twoFAUpdated"), "success");
        totpStatusText = "Email";
        totpStatusColor = "var(--success)";
      } else {
        showMessage(
          $_("account.failed", { values: { error: result.error } }),
          "error",
        );
      }
    } else if (twoFAMethod === "sms") {
      const result = await accountStore.setup2FA("sms");
      if (result.success) {
        showMessage($_("account.twoFAUpdated"), "success");
        totpStatusText = "SMS";
        totpStatusColor = "var(--success)";
      } else {
        showMessage(
          $_("account.failed", { values: { error: result.error } }),
          "error",
        );
      }
    } else {
      statusBar = $_("account.disabling2FA");
      const result = await accountStore.setup2FA("none");
      if (result.success) {
        showMessage($_("account.twoFADisabled"), "success");
        totpStatusText = $_("common.disabled");
        totpStatusColor = "var(--error)";
      } else {
        showMessage(
          $_("account.failed", { values: { error: result.error } }),
          "error",
        );
      }
    }
    statusBar = $_("common.ready");
  }

  function handle2FAMethodChange(method: "none" | "totp" | "email" | "sms") {
    twoFAMethod = method;
    showTotpSetup = method === "totp";
  }

  function showMessage(msg: string, type: "success" | "error" | "info") {
    statusMessage = msg;
    messageType = type;
    // Clear previous timeout to prevent accumulation
    if (statusTimeout) {
      clearTimeout(statusTimeout);
    }
    statusTimeout = setTimeout(() => {
      statusMessage = "";
      statusTimeout = null;
    }, 5000);
  }

  function handleMaximize() {
    isMaximized = !isMaximized;
  }

  function handleReduce() {
    isMinimized = true;
    $activeThing = "";
  }

  async function handleDeleteAccount() {
    const password = prompt($_("account.deleteAccountConfirm"));

    if (!password) return;

    const reason = prompt($_("account.deleteReasonPrompt"));

    if (!confirm($_("account.deleteConfirmFinal"))) {
      return;
    }

    statusBar = $_("account.deletingAccount");
    const result = await accountStore.deleteAccount(password);
    if (result.success) {
      alert($_("account.accountDeleted"));
      window.location.href = "/login";
    } else {
      showMessage(
        $_("account.errorDeletingAccount", { values: { error: result.error } }),
        "error",
      );
    }
    statusBar = $_("common.ready");
  }

  async function handleDownloadMCPConfig() {
    await loadMCPConfig();
    if (!mcpConfig) return;

    try {
      // Ensure we have a token
      let tokenToUse = "<API_KEY_REQUIRED>";

      // Priority 1: Freshly generated key (full token visible)
      if (generatedKey && generatedKey.token) {
        tokenToUse = generatedKey.token;
      }
      // Priority 2: Use existing key if we have one locally
      // The backend now returns the full token (at least temporarily/per user request)
      if (
        tokenToUse === "<API_KEY_REQUIRED>" &&
        apiKeys &&
        apiKeys.length > 0
      ) {
        // Find a key with a token property if available
        // The store/backend mapping might put it in 'token' or we might need to check how it's mapped.
        // In store/account.ts: keys are just passed through from response.keys.
        // In tenant_proxy.go: we return map with "token": k.Token.
        // So it should be there.
        const validKey = apiKeys.find(
          (k: any) => k.token && k.token.length > 10,
        );
        if (validKey) {
          tokenToUse = validKey.token;
        }
      }

      // Update config with the best token we have
      updateMCPConfigWithToken(tokenToUse);

      // Ensure URL is correct (protocol relative or absolute)
      // The user mentioned "http://localhost:8080/stdio" in their example,
      // but that is likely an artifact of their local testing or copy-paste.
      // We should stick to the actual server URL.
      // However, we MUST sure 'args', 'command', etc exist as per their JSON example.

      const downloadConfig = JSON.parse(JSON.stringify(mcpConfig));

      // Ensure all fields from user example are present
      // Ensure all fields from user example are present
      if (downloadConfig.mcpServers && downloadConfig.mcpServers["wantastic"]) {
        const srv = downloadConfig.mcpServers["wantastic"];

        // Remove stdio fields not needed for SSE
        delete srv.args;
        delete srv.command;
        delete srv.env;

        // Disable tools by default empty
        if (srv.disabled === undefined) srv.disabled = false;
        if (!srv.disabledTools) srv.disabledTools = [];

        // Ensure Server URL matches SSE endpoint
        // Adjust protocol if needed, but assuming user wants the deduced one
        srv.serverUrl = srv.serverUrl.replace("/mcp", "/sse"); // Handled by loadMCPConfig replacer usually, but let's be safe

        // Headers are critical
        if (!srv.headers) srv.headers = {};
        srv.headers["Authorization"] = "Bearer " + tokenToUse;
      }

      const blob = new Blob([JSON.stringify(downloadConfig, null, 2)], {
        type: "application/json",
      });
      const url = window.URL.createObjectURL(blob);
      const a = document.createElement("a");
      a.href = url;
      a.download = "wantastic_mcp.json";
      document.body.appendChild(a);
      a.click();
      window.URL.revokeObjectURL(url);
      document.body.removeChild(a);

      showMessage($_("account.mcpConfigDownloaded"), "success");
    } catch (e: any) {
      showMessage(
        $_("account.errorDownloadingMCPConfig", {
          values: { error: e.message },
        }),
        "error",
      );
    }
    statusBar = $_("common.ready");
  }

</script>

<div
  class="account activeShadow"
  class:maximized={isMaximized}
  class:minimized={isMinimized}
  style:z-index={zIndex}
  on:mousedown={handleFocus}
  on:touchstart={handleFocus}
  use:draggable={{
    handle: ".title-bar",
    disabled: isMaximized,
    bounds: "body",
  }}
  transition:scale={{ duration: 200 }}
>
  <Titlebar
    appName="Account"
    on:maximize={handleMaximize}
    on:reduce={handleReduce}
  >
    <svg
      xmlns="http://www.w3.org/2000/svg"
      width="16"
      height="16"
      viewBox="0 0 100 100"
    >
      <defs>
        <linearGradient id="accountGrad" x1="0%" y1="0%" x2="100%" y2="100%">
          <stop offset="0%" style="stop-color:#64748b;stop-opacity:1" />
          <stop offset="100%" style="stop-color:#475569;stop-opacity:1" />
        </linearGradient>
      </defs>
      <circle
        cx="50"
        cy="50"
        r="35"
        fill="url(#accountGrad)"
        stroke="#334155"
        stroke-width="2"
      />
      <circle
        cx="50"
        cy="40"
        r="12"
        fill="#1f2937"
        stroke="#94a3b8"
        stroke-width="2"
      />
      <path
        d="M50 54 Q30 58 25 75 Q25 78 28 78 L72 78 Q75 78 75 75 Q70 58 50 54 Z"
        fill="#1f2937"
        stroke="#94a3b8"
        stroke-width="2"
      />
    </svg>
    <span class="appName pl-2"
      >{isViewingShared
        ? viewingAccountName + " " + $_("account.myAccount")
        : $_("account.myAccount")}</span
    >
  </Titlebar>

  <div class="mainApp">
    {#if statusMessage}
      <div
        class="message"
        class:error={messageType === "error"}
        class:info={messageType === "info"}
      >
        {statusMessage}
      </div>
    {/if}

    <div class="tabs">
      <button
        class="tab-btn"
        class:active={activeTab === "profile"}
        on:click={() => switchTab("profile")}
      >
        <svg
          width="16"
          height="16"
          viewBox="0 0 24 24"
          fill="none"
          stroke="currentColor"
          stroke-width="2"
        >
          <circle cx="12" cy="8" r="4" /><path
            d="M6 21v-2a4 4 0 014-4h4a4 4 0 014 4v2"
          />
        </svg>
        <span>{$_("account.profile")}</span>
      </button>
      <button
        class="tab-btn"
        class:active={activeTab === "security"}
        on:click={() => switchTab("security")}
      >
        <svg
          width="16"
          height="16"
          viewBox="0 0 24 24"
          fill="none"
          stroke="currentColor"
          stroke-width="2"
        >
          <rect x="3" y="11" width="18" height="11" rx="2" /><path
            d="M7 11V7a5 5 0 0110 0v4"
          />
        </svg>
        <span>{$_("account.security")}</span>
      </button>
      <button
        class="tab-btn"
        class:active={activeTab === "sessions"}
        on:click={() => switchTab("sessions")}
      >
        <svg
          width="16"
          height="16"
          viewBox="0 0 24 24"
          fill="none"
          stroke="currentColor"
          stroke-width="2"
        >
          <rect x="2" y="3" width="20" height="14" rx="2" /><line
            x1="8"
            y1="21"
            x2="16"
            y2="21"
          /><line x1="12" y1="17" x2="12" y2="21" />
        </svg>
        <span>{$_("account.sessions")}</span>
      </button>
      <button
        class="tab-btn"
        class:active={activeTab === "twofa"}
        on:click={() => switchTab("twofa")}
      >
        <svg
          width="16"
          height="16"
          viewBox="0 0 24 24"
          fill="none"
          stroke="currentColor"
          stroke-width="2"
        >
          <path d="M12 22s8-4 8-10V5l-8-3-8 3v7c0 6 8 10 8 10z" />
        </svg>
        <span>{$_("account.twoFA")}</span>
      </button>
      <button
        class="tab-btn"
        class:active={activeTab === "tokens"}
        on:click={() => switchTab("tokens")}
      >
        <svg
          width="16"
          height="16"
          viewBox="0 0 24 24"
          fill="none"
          stroke="currentColor"
          stroke-width="2"
        >
          <path
            d="M21 2l-2 2m-7.61 7.61a5.5 5.5 0 1 1-7.778 7.778 5.5 5.5 0 0 1 7.777-7.777zm0 0L15.5 7.5m0 0l3 3L22 7l-3-3.5"
          />
        </svg>
        <span>Secrets</span>
      </button>
      <button
        class="tab-btn"
        class:active={activeTab === "snapshots"}
        on:click={() => switchTab("snapshots")}
      >
        <svg
          width="16"
          height="16"
          viewBox="0 0 24 24"
          fill="none"
          stroke="currentColor"
          stroke-width="2"
        >
          <path d="M21 16V8a2 2 0 0 0-1-1.73l-7-4a2 2 0 0 0-2 0l-7 4A2 2 0 0 0 3 8v8a2 2 0 0 0 1 1.73l7 4a2 2 0 0 0 2 0l7-4A2 2 0 0 0 21 16z"/>
          <polyline points="3.27 6.96 12 12.01 20.73 6.96"/><line x1="12" y1="22.08" x2="12" y2="12"/>
        </svg>
        <span>Snapshots</span>
      </button>
      <button
        class="tab-btn"
        class:active={activeTab === "widgets"}
        on:click={() => switchTab("widgets")}
      >
        <svg
          width="16"
          height="16"
          viewBox="0 0 24 24"
          fill="none"
          stroke="currentColor"
          stroke-width="2"
        >
          <rect x="3" y="3" width="7" height="7" rx="1.5" />
          <rect x="14" y="3" width="7" height="7" rx="1.5" />
          <rect x="3" y="14" width="7" height="7" rx="1.5" />
          <rect x="14" y="14" width="7" height="7" rx="1.5" />
        </svg>
        <span>{$_("account.widgets")}</span>
      </button>
      <!-- <button
        class="tab-btn"
        class:active={activeTab === "mcp"}
        on:click={() => switchTab("mcp")}
      >
        <svg
          width="16"
          height="16"
          viewBox="0 0 24 24"
          fill="none"
          stroke="currentColor"
          stroke-width="2"
        >
          <rect x="2" y="3" width="20" height="14" rx="2" ry="2" />
          <line x1="8" y1="21" x2="16" y2="21" />
          <line x1="12" y1="17" x2="12" y2="21" />
        </svg>
        <span>{$_("apps.mcp")}</span>
      </button> -->
    </div>

    <div class="content">
      {#if loading}
        <!-- Skeleton Loading State -->
        <div class="skeleton-container">
          <div class="skeleton-card">
            <div class="skeleton-header">
              <div class="skeleton-icon" />
              <div class="skeleton-title" />
            </div>
            <div class="skeleton-body">
              <div class="skeleton-field">
                <div class="skeleton-label" />
                <div class="skeleton-input" />
              </div>
              <div class="skeleton-field">
                <div class="skeleton-label" />
                <div class="skeleton-input" />
              </div>
              <div class="skeleton-field">
                <div class="skeleton-label" />
                <div class="skeleton-input" />
              </div>
              <div class="skeleton-button" />
            </div>
          </div>
        </div>
        <!-- {:else if activeTab === "mcp"} -->
        <!-- <div class="mcp-container">
          <div class="card glass-panel">
            <div class="card-header">
              <h3 class="card-title">
                <div class="icon-wrapper">
                  <svg
                    width="20"
                    height="20"
                    viewBox="0 0 24 24"
                    fill="none"
                    stroke="currentColor"
                    stroke-width="2"
                    stroke-linecap="round"
                    stroke-linejoin="round"
                  >
                    <rect x="2" y="3" width="20" height="14" rx="2" ry="2" />
                    <line x1="8" y1="21" x2="16" y2="21" />
                    <line x1="12" y1="17" x2="12" y2="21" />
                  </svg>
                </div>
                {$_("apps.mcp")}
              </h3>
            </div>
            <div class="card-body">
              <div class="info-section">
                <p class="description">
                  {$_("account.mcpDescription")}
                </p>
                <div class="download-section">
                  <button
                    class="btn-primary"
                    on:click={handleDownloadMCPConfig}
                  >
                    <svg
                      width="16"
                      height="16"
                      viewBox="0 0 24 24"
                      fill="none"
                      stroke="currentColor"
                      stroke-width="2"
                    >
                      <path d="M21 15v4a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2v-4" />
                      <polyline points="7 10 12 15 17 10" />
                      <line x1="12" y1="15" x2="12" y2="3" />
                    </svg>
                    {$_("account.downloadMCPConfig")}
                  </button>
                  <p class="help-text">
                    {$_("account.importMCPConfigHelper")}
                  </p>
                </div>
              </div>

              <div class="divider" />

              <div class="keys-section">
                <div class="section-header">
                  <h4>{$_("account.apiKeys")}</h4>
                  {#if apiKeys.length === 0}
                    <button
                      class="btn-secondary"
                      on:click={() => (showCreateKey = true)}
                    >
                      <svg
                        width="16"
                        height="16"
                        viewBox="0 0 24 24"
                        fill="none"
                        stroke="currentColor"
                        stroke-width="2"
                      >
                        <line x1="12" y1="5" x2="12" y2="19" />
                        <line x1="5" y1="12" x2="19" y2="12" />
                      </svg>
                      {$_("account.createAPIKey")}
                    </button>
                  {/if}
                </div>

                {#if generatedKey}
                  <div class="success-box" transition:scale>
                    <div class="success-header">
                      <span class="success-icon">✓</span>
                      <h5>{$_("account.apiKeyCreatedTitle")}</h5>
                    </div>
                    <p>{$_("account.apiKeyCopyWarning")}</p>
                    <div class="key-display">
                      <code>{generatedKey.token}</code>
                      <button
                        class="copy-btn"
                        on:click={() => copyToClipboard(generatedKey.token)}
                      >
                        {$_("common.copy")}
                      </button>
                    </div>
                  </div>
                {/if}

                {#if showCreateKey && !generatedKey}
                  <div class="create-form" transition:scale>
                    <div class="input-group">
                      <input
                        type="text"
                        placeholder={$_("account.keyNamePlaceholder")}
                        bind:value={newKeyName}
                        class="input-field"
                        on:keydown={(e) =>
                          e.key === "Enter" && handleCreateAPIKey()}
                      />
                      <button
                        class="btn-primary"
                        on:click={handleCreateAPIKey}
                        disabled={!newKeyName}
                      >
                        {$_("account.generate")}
                      </button>
                      <button
                        class="btn-ghost"
                        on:click={() => (showCreateKey = false)}
                      >
                        {$_("common.cancel")}
                      </button>
                    </div>
                  </div>
                {/if}

                {#if apiKeys.length > 0}
                  <div class="table-container">
                    <table class="styled-table">
                      <thead>
                        <tr>
                          <th>{$_("common.name")}</th>
                          <th>{$_("account.prefix")}</th>
                          <th>{$_("account.created")}</th>
                          <th>{$_("common.status")}</th>
                          <th />
                        </tr>
                      </thead>
                      <tbody>
                        {#each apiKeys as key}
                          <tr>
                            <td class="font-medium">{key.name}</td>
                            <td class="font-mono text-sm opacity-70">
                              {key.prefix ||
                                (key.token
                                  ? key.token.substring(0, 6) + "..."
                                  : "......")}
                            </td>
                            <td class="text-sm opacity-70">
                              {new Date(
                                key.created_at || key.createdAt
                              ).toLocaleDateString()}
                            </td>
                            <td>
                              <span class="badge badge-success"
                                >{$_("common.active")}</span
                              >
                            </td>
                            <td class="actions">
                              <button
                                class="action-btn delete"
                                on:click={() => handleRevokeAPIKey(key.id)}
                                title={$_("account.revokeKey")}
                              >
                                <svg
                                  width="16"
                                  height="16"
                                  viewBox="0 0 24 24"
                                  fill="none"
                                  stroke="currentColor"
                                  stroke-width="2"
                                >
                                  <polyline points="3 6 5 6 21 6" />
                                  <path
                                    d="M19 6v14a2 2 0 0 1-2 2H7a2 2 0 0 1-2-2V6m3 0V4a2 2 0 0 1 2-2h4a2 2 0 0 1 2 2v2"
                                  />
                                </svg>
                              </button>
                            </td>
                          </tr>
                        {/each}
                      </tbody>
                    </table>
                  </div>
                {:else if !showCreateKey && !generatedKey}
                  <div class="empty-state">
                    <p>{$_("account.noApiKeys")}</p>
                  </div>
                {/if}
              </div>
            </div>
          </div>
        </div>

        <style>
          .mcp-container {
            padding: 1rem;
          }
          .glass-panel {
            background: rgba(30, 41, 59, 0.7);
            backdrop-filter: blur(10px);
            border: 1px solid rgba(255, 255, 255, 0.1);
            border-radius: 12px;
            overflow: hidden;
          }
          .card-header {
            padding: 1.5rem;
            border-bottom: 1px solid rgba(255, 255, 255, 0.05);
            display: flex;
            align-items: center;
            gap: 0.75rem;
          }
          .icon-wrapper {
            width: 32px;
            height: 32px;
            border-radius: 8px;
            display: flex;
            align-items: center;
            justify-content: center;
            color: white;
          }
          .card-title {
            font-size: 1.1rem;
            font-weight: 600;
            color: #f1f5f9;
            margin: 0;
            display: flex;
            align-items: center;
            gap: 0.5rem;
          }
          .card-body {
            padding: 1.5rem;
          }
          .description {
            color: #94a3b8;
            margin-bottom: 1.5rem;
            line-height: 1.5;
          }
          .download-section {
            display: flex;
            align-items: center;
            gap: 1rem;
            background: rgba(0, 0, 0, 0.2);
            padding: 1rem;
            border-radius: 8px;
          }
          .help-text {
            color: #64748b;
            font-size: 0.875rem;
            margin: 0;
          }
          .divider {
            height: 1px;
            background: rgba(255, 255, 255, 0.05);
            margin: 2rem 0;
          }
          .section-header {
            display: flex;
            justify-content: space-between;
            align-items: center;
            margin-bottom: 1rem;
          }
          .section-header h4 {
            color: #f1f5f9;
            font-weight: 600;
            margin: 0;
          }
          .success-box {
            background: rgba(16, 185, 129, 0.1);
            border: 1px solid rgba(16, 185, 129, 0.2);
            border-radius: 8px;
            padding: 1rem;
            margin-bottom: 1.5rem;
          }
          .success-header {
            display: flex;
            align-items: center;
            gap: 0.5rem;
            color: #10b981;
            margin-bottom: 0.5rem;
          }
          .success-header h5 {
            margin: 0;
            font-weight: 600;
          }
          .key-display {
            display: flex;
            gap: 0.5rem;
            margin-top: 0.5rem;
          }
          .key-display code {
            flex: 1;
            background: rgba(0, 0, 0, 0.3);
            padding: 0.5rem 0.75rem;
            border-radius: 6px;
            font-family: monospace;
            color: #e2e8f0;
            border: 1px solid rgba(255, 255, 255, 0.1);
            overflow-x: auto;
          }
          .copy-btn {
            background: rgba(255, 255, 255, 0.1);
            border: 1px solid rgba(255, 255, 255, 0.1);
            color: #e2e8f0;
            padding: 0 1rem;
            border-radius: 6px;
            cursor: pointer;
          }
          .copy-btn:hover {
            background: rgba(255, 255, 255, 0.2);
          }
          .input-group {
            display: flex;
            gap: 0.5rem;
          }
          .input-field {
            flex: 1;
            background: rgba(0, 0, 0, 0.3);
            border: 1px solid rgba(255, 255, 255, 0.1);
            color: #f1f5f9;
            padding: 0.5rem 1rem;
            border-radius: 6px;
            outline: none;
          }
          .input-field:focus {
            border-color: #3b82f6;
          }
          .styled-table {
            width: 100%;
            border-collapse: separate;
            border-spacing: 0;
          }
          .styled-table th {
            text-align: left;
            padding: 0.75rem 1rem;
            color: #94a3b8;
            font-weight: 500;
            font-size: 0.875rem;
            border-bottom: 1px solid rgba(255, 255, 255, 0.05);
          }
          .styled-table td {
            padding: 0.75rem 1rem;
            color: #e2e8f0;
            border-bottom: 1px solid rgba(255, 255, 255, 0.05);
          }
          .styled-table tr:last-child td {
            border-bottom: none;
          }
          .badge {
            padding: 0.25rem 0.75rem;
            border-radius: 9999px;
            font-size: 0.75rem;
            font-weight: 500;
          }
          .badge-success {
            background: rgba(16, 185, 129, 0.1);
            color: #34d399;
            border: 1px solid rgba(16, 185, 129, 0.2);
          }
          .btn-primary {
            background: linear-gradient(135deg, #3b82f6 0%, #2563eb 100%);
            color: white;
            border: none;
            padding: 0.5rem 1rem;
            border-radius: 6px;
            font-weight: 500;
            cursor: pointer;
            display: flex;
            align-items: center;
            gap: 0.5rem;
            transition: opacity 0.2s;
          }
          .btn-primary:hover {
            opacity: 0.9;
          }
          .btn-primary:disabled {
            opacity: 0.5;
            cursor: not-allowed;
          }
          .btn-secondary {
            background: rgba(255, 255, 255, 0.05);
            color: #e2e8f0;
            border: 1px solid rgba(255, 255, 255, 0.1);
            padding: 0.5rem 1rem;
            border-radius: 6px;
            font-weight: 500;
            cursor: pointer;
            display: flex;
            align-items: center;
            gap: 0.5rem;
          }
          .btn-secondary:hover {
            background: rgba(255, 255, 255, 0.1);
          }
          .btn-ghost {
            background: transparent;
            color: #94a3b8;
            border: none;
            padding: 0.5rem 1rem;
            cursor: pointer;
          }
          .btn-ghost:hover {
            color: #f1f5f9;
          }
          .action-btn {
            background: transparent;
            border: none;
            color: #94a3b8;
            cursor: pointer;
            padding: 4px;
            border-radius: 4px;
            display: flex;
            align-items: center;
            justify-content: center;
          }
          .action-btn:hover {
            background: rgba(255, 255, 255, 0.05);
            color: #f1f5f9;
          }
          .action-btn.delete:hover {
            color: #ef4444;
            background: rgba(239, 68, 68, 0.1);
          }
          .empty-state {
            text-align: center;
            padding: 2rem;
            color: #64748b;
            border: 1px dashed rgba(255, 255, 255, 0.1);
            border-radius: 8px;
          }
        </style> -->
      {:else if activeTab === "profile"}
        <!-- Account Overview Card -->

        <div class="card">
          <div class="card-header">
            <h3 class="card-title">
              <svg
                width="18"
                height="18"
                viewBox="0 0 24 24"
                fill="none"
                stroke="currentColor"
                stroke-width="2"
              >
                <circle cx="12" cy="8" r="4" /><path
                  d="M6 21v-2a4 4 0 014-4h4a4 4 0 014 4v2"
                />
              </svg>
              {$_("account.profileInfo")}
            </h3>
          </div>
          <div class="card-body">
            <!-- Account Overview (Integrated) -->
            <div class="overview-integrated mb-6 pb-6 border-b border-white/10">
              <div class="stat-grid-3">
                <div class="stat-item simple">
                  <div class="stat-label">Status</div>
                  <div class="stat-value status-active">
                    {stats?.status || "Active"}
                  </div>
                </div>
                <div class="stat-item simple">
                  <div class="stat-label">Allocated Subnets</div>
                  <div class="stat-value">
                    {blockCount || networkBlocks.length}
                  </div>
                </div>
              </div>
            </div>

            <div class="form-group">
              <label for="account-fullname">{$_("account.fullName")}</label>
              <input
                type="text"
                id="account-fullname"
                bind:value={fullName}
                placeholder={$_("account.enterFullName")}
              />
            </div>
            <div class="form-group">
              <label for="account-email">{$_("account.email")}</label>
              <input
                type="email"
                id="account-email"
                bind:value={email}
                class="copyable"
                on:click={() => copy.email(email)}
                disabled
              />
              <span class="hint">{$_("account.emailCannotChange")}</span>
            </div>
            <div class="form-group">
              <label for="account-phone">{$_("account.phone")}</label>
              <input
                type="tel"
                id="account-phone"
                bind:value={phone}
                placeholder={$_("account.enterPhone")}
              />
            </div>

            <div class="form-group">
              <label for="account-language">{$_("account.language")}</label>
              <div class="language-selector">
                <select
                  id="account-language"
                  value={$currentLanguage}
                  on:change={(e) => changeLanguage(e.currentTarget.value, true)}
                >
                  {#each Object.entries(SUPPORTED_LANGUAGES) as [code, lang]}
                    <option value={code}>{lang.nativeName} ({lang.name})</option
                    >
                  {/each}
                </select>
                <div class="select-arrow">
                  <svg
                    width="12"
                    height="12"
                    viewBox="0 0 24 24"
                    fill="none"
                    stroke="currentColor"
                    stroke-width="2"
                  >
                    <path d="M6 9l6 6 6-6" />
                  </svg>
                </div>
              </div>
              <span class="hint">{$_("account.languageDescription")}</span>
            </div>

            <div class="form-actions">
              <Button
                variant="accent"
                on:click={handleSaveProfile}
                disabled={loading}>{$_("account.saveChanges")}</Button
              >
            </div>
          </div>
        </div>
      {/if}

      {#if activeTab === "security"}
        <div class="card">
          <div class="card-header">
            <h3 class="card-title">
              <svg
                width="18"
                height="18"
                viewBox="0 0 24 24"
                fill="none"
                stroke="currentColor"
                stroke-width="2"
              >
                <rect x="3" y="11" width="18" height="11" rx="2" /><path
                  d="M7 11V7a5 5 0 0110 0v4"
                />
              </svg>
              {$_("account.changePassword")}
            </h3>
          </div>
          <div class="card-body">
            <form on:submit={handlePasswordChange}>
              <div class="form-group">
                <label for="account-current-password"
                  >{$_("account.currentPassword")}</label
                >
                <input
                  type="password"
                  id="account-current-password"
                  bind:value={currentPassword}
                  required
                />
              </div>
              <div class="form-group">
                <label for="account-new-password"
                  >{$_("account.newPassword")}</label
                >
                <input
                  type="password"
                  id="account-new-password"
                  bind:value={newPassword}
                  required
                  minlength="8"
                />
              </div>
              <div class="form-group">
                <label for="account-confirm-password"
                  >{$_("account.confirmNewPassword")}</label
                >
                <input
                  type="password"
                  id="account-confirm-password"
                  bind:value={confirmPassword}
                  required
                />
              </div>
              <div class="form-actions">
                <Button type="submit" variant="accent"
                  >{$_("account.changePasswordBtn")}</Button
                >
              </div>
            </form>
          </div>
        </div>
        <div class="card danger-card">
          <div class="card-header">
            <h3 class="card-title danger-title">
              <svg
                width="18"
                height="18"
                viewBox="0 0 24 24"
                fill="none"
                stroke="currentColor"
                stroke-width="2"
              >
                <path
                  d="M10.29 3.86L1.82 18a2 2 0 001.71 3h16.94a2 2 0 001.71-3L13.71 3.86a2 2 0 00-3.42 0z"
                />
                <line x1="12" y1="9" x2="12" y2="13" /><line
                  x1="12"
                  y1="17"
                  x2="12.01"
                  y2="17"
                />
              </svg>
              {$_("account.dangerZone")}
            </h3>
          </div>
          <div class="card-body">
            <p class="danger-text">
              {$_("account.deleteAccountWarning")}
            </p>
            <button class="btn btn-danger" on:click={handleDeleteAccount}
              >{$_("account.deleteAccount")}</button
            >
          </div>
        </div>
      {/if}

      {#if activeTab === "sessions"}
        <div class="card sessions-card">
          <div class="card-header">
            <h3 class="card-title">
              <svg
                width="18"
                height="18"
                viewBox="0 0 24 24"
                fill="none"
                stroke="currentColor"
                stroke-width="2"
              >
                <rect x="2" y="3" width="20" height="14" rx="2" /><line
                  x1="8"
                  y1="21"
                  x2="16"
                  y2="21"
                /><line x1="12" y1="17" x2="12" y2="21" />
              </svg>
              {$_("account.activeSessions")}
            </h3>
            <button
              class="btn btn-ghost btn-icon btn-refresh"
              on:click={loadSessions}
              disabled={sessionsLoading}
            >
              {#if sessionsLoading}
                <svg
                  class="spin"
                  width="16"
                  height="16"
                  viewBox="0 0 24 24"
                  fill="none"
                  stroke="currentColor"
                  stroke-width="2"
                >
                  <path d="M21 12a9 9 0 11-6.219-8.56" />
                </svg>
              {:else}
                <svg
                  width="16"
                  height="16"
                  viewBox="0 0 24 24"
                  fill="none"
                  stroke="currentColor"
                  stroke-width="2"
                >
                  <path d="M23 4v6h-6" /><path d="M1 20v-6h6" /><path
                    d="M3.51 9a9 9 0 0114.85-3.36L23 10M1 14l4.64 4.36A9 9 0 0020.49 15"
                  />
                </svg>
              {/if}
            </button>
          </div>
          <div class="card-body sessions-body">
            {#if sessionsLoading}
              <div class="sessions-loading">
                <svg
                  class="spin"
                  width="24"
                  height="24"
                  viewBox="0 0 24 24"
                  fill="none"
                  stroke="currentColor"
                  stroke-width="2"
                >
                  <path d="M21 12a9 9 0 11-6.219-8.56" />
                </svg>
                <span>{$_("account.loadingSessions")}</span>
              </div>
            {:else if sessions.length === 0}
              <div class="sessions-empty">
                <svg
                  width="48"
                  height="48"
                  viewBox="0 0 24 24"
                  fill="none"
                  stroke="currentColor"
                  stroke-width="1.5"
                >
                  <rect x="2" y="3" width="20" height="14" rx="2" /><line
                    x1="8"
                    y1="21"
                    x2="16"
                    y2="21"
                  /><line x1="12" y1="17" x2="12" y2="21" />
                </svg>
                <p>{$_("account.noActiveSessions")}</p>
              </div>
            {:else}
              <div class="sessions-table-container">
                <table class="sessions-table">
                  <thead>
                    <tr>
                      <th>{$_("common.name")}</th>
                      <th class="hide-mobile">{$_("topology.nodeIP")}</th>
                      <th class="hide-mobile">{$_("webssh.viewActivity")}</th>
                      <th>{$_("common.actions")}</th>
                    </tr>
                  </thead>
                  <tbody>
                    {#each sessions as session}
                      <tr class:current={session.isCurrent}>
                        <td class="device-cell">
                          <div class="device-info">
                            <span class="device-icon"
                              >{getDeviceIcon(session.deviceType)}</span
                            >
                            <div class="device-details">
                              <span class="device-browser"
                                >{session.browser ||
                                  "Unknown"}{session.browserVersion
                                  ? ` ${session.browserVersion}`
                                  : ""}</span
                              >
                              <span class="device-os"
                                >{session.os || "Unknown"}</span
                              >
                              <!-- Mobile-only info -->
                              <span class="device-ip show-mobile"
                                >{session.ipAddress}</span
                              >
                              <span class="device-activity show-mobile"
                                >{formatSessionDate(session.lastActivity)}</span
                              >
                            </div>
                          </div>
                          {#if session.isCurrent}
                            <span class="current-badge"
                              >{$_("account.currentBadge")}</span
                            >
                          {/if}
                        </td>
                        <td class="hide-mobile">
                          <!-- svelte-ignore a11y-click-events-have-key-events -->
                          <code
                            class="ip-address copyable"
                            on:click={() => copy.ip(session.ipAddress)}
                            >{session.ipAddress}</code
                          >
                        </td>
                        <td class="hide-mobile">
                          <span class="activity-time"
                            >{formatSessionDate(session.lastActivity)}</span
                          >
                        </td>
                        <td class="actions-cell">
                          {#if !session.isCurrent}
                            <button
                              class="btn-end-session"
                              on:click={() =>
                                handleDeleteSession(session.sessionId)}
                              title="End this session"
                            >
                              <svg
                                width="14"
                                height="14"
                                viewBox="0 0 24 24"
                                fill="none"
                                stroke="currentColor"
                                stroke-width="2"
                              >
                                <path
                                  d="M9 21H5a2 2 0 01-2-2V5a2 2 0 012-2h4"
                                /><polyline points="16 17 21 12 16 7" /><line
                                  x1="21"
                                  y1="12"
                                  x2="9"
                                  y2="12"
                                />
                              </svg>
                              <span class="hide-mobile"
                                >{$_("account.endBtn")}</span
                              >
                            </button>
                          {:else}
                            <span class="current-session-text"
                              >{$_("account.currentSession")}</span
                            >
                          {/if}
                        </td>
                      </tr>
                    {/each}
                  </tbody>
                </table>
              </div>

              <div class="sessions-footer">
                <p class="sessions-hint">
                  <svg
                    width="14"
                    height="14"
                    viewBox="0 0 24 24"
                    fill="none"
                    stroke="currentColor"
                    stroke-width="2"
                  >
                    <circle cx="12" cy="12" r="10" /><line
                      x1="12"
                      y1="16"
                      x2="12"
                      y2="12"
                    /><line x1="12" y1="8" x2="12.01" y2="8" />
                  </svg>
                  {$_("account.sessionEndHint")}
                </p>
              </div>
            {/if}
          </div>
        </div>
      {/if}

      {#if activeTab === "twofa"}
        <div class="card twofa-card">
          <div class="card-header">
            <h3 class="card-title">
              <svg
                width="18"
                height="18"
                viewBox="0 0 24 24"
                fill="none"
                stroke="currentColor"
                stroke-width="2"
              >
                <path d="M12 22s8-4 8-10V5l-8-3-8 3v7c0 6 8 10 8 10z" />
              </svg>
              {$_("account.twoFactorAuth")}
            </h3>
          </div>
          <div class="card-body">
            <!-- Current Status Badge -->
            <div
              class="twofa-status-badge"
              class:enabled={twoFAMethod !== "none"}
              class:disabled={twoFAMethod === "none"}
            >
              <div class="status-icon">
                {#if twoFAMethod !== "none"}
                  <svg
                    width="20"
                    height="20"
                    viewBox="0 0 24 24"
                    fill="none"
                    stroke="currentColor"
                    stroke-width="2.5"
                  >
                    <path d="M22 11.08V12a10 10 0 11-5.93-9.14" /><polyline
                      points="22 4 12 14.01 9 11.01"
                    />
                  </svg>
                {:else}
                  <svg
                    width="20"
                    height="20"
                    viewBox="0 0 24 24"
                    fill="none"
                    stroke="currentColor"
                    stroke-width="2"
                  >
                    <circle cx="12" cy="12" r="10" /><line
                      x1="15"
                      y1="9"
                      x2="9"
                      y2="15"
                    /><line x1="9" y1="9" x2="15" y2="15" />
                  </svg>
                {/if}
              </div>
              <div class="status-text">
                <span class="status-label">{$_("account.currentStatus")}</span>
                <span class="status-value">{totpStatusText}</span>
              </div>
            </div>
            <p>
              {$_("account.twoFADescription")}
            </p>

            <!-- 2FA Method Options Grid -->
            <div class="twofa-options">
              <!-- Disabled Option -->
              <label
                class="twofa-option"
                class:selected={twoFAMethod === "none"}
              >
                <input
                  type="radio"
                  name="twofa-method"
                  value="none"
                  checked={twoFAMethod === "none"}
                  on:change={() => handle2FAMethodChange("none")}
                />
                <div class="option-icon disabled-icon">
                  <svg
                    width="24"
                    height="24"
                    viewBox="0 0 24 24"
                    fill="none"
                    stroke="currentColor"
                    stroke-width="1.5"
                  >
                    <circle cx="12" cy="12" r="10" /><line
                      x1="4.93"
                      y1="4.93"
                      x2="19.07"
                      y2="19.07"
                    />
                  </svg>
                </div>
                <div class="option-content">
                  <span class="option-title">Disabled</span>
                  <span class="option-desc">No additional verification</span>
                </div>
                <div class="option-check">
                  <svg
                    width="18"
                    height="18"
                    viewBox="0 0 24 24"
                    fill="none"
                    stroke="currentColor"
                    stroke-width="3"><polyline points="20 6 9 17 4 12" /></svg
                  >
                </div>
              </label>

              <!-- Authenticator App Option -->
              <label
                class="twofa-option"
                class:selected={twoFAMethod === "totp"}
              >
                <input
                  type="radio"
                  name="twofa-method"
                  value="totp"
                  checked={twoFAMethod === "totp"}
                  on:change={() => handle2FAMethodChange("totp")}
                />
                <div class="option-icon totp-icon">
                  <svg
                    width="24"
                    height="24"
                    viewBox="0 0 24 24"
                    fill="none"
                    stroke="currentColor"
                    stroke-width="1.5"
                  >
                    <rect x="3" y="11" width="18" height="11" rx="2" /><path
                      d="M7 11V7a5 5 0 0110 0v4"
                    />
                  </svg>
                </div>
                <div class="option-content">
                  <span class="option-title">Authenticator App</span>
                  <span class="option-desc">Google Auth, Authy, or similar</span
                  >
                </div>
                <div class="option-check">
                  <svg
                    width="18"
                    height="18"
                    viewBox="0 0 24 24"
                    fill="none"
                    stroke="currentColor"
                    stroke-width="3"><polyline points="20 6 9 17 4 12" /></svg
                  >
                </div>
              </label>

              <!-- Email Option -->
              <label
                class="twofa-option"
                class:selected={twoFAMethod === "email"}
              >
                <input
                  type="radio"
                  name="twofa-method"
                  value="email"
                  checked={twoFAMethod === "email"}
                  on:change={() => handle2FAMethodChange("email")}
                />
                <div class="option-icon email-icon">
                  <svg
                    width="24"
                    height="24"
                    viewBox="0 0 24 24"
                    fill="none"
                    stroke="currentColor"
                    stroke-width="1.5"
                  >
                    <rect x="2" y="4" width="20" height="16" rx="2" /><path
                      d="M22 7l-10 7L2 7"
                    />
                  </svg>
                </div>
                <div class="option-content">
                  <span class="option-title">Email</span>
                  <span class="option-desc">Receive codes via email</span>
                </div>
                <div class="option-check">
                  <svg
                    width="18"
                    height="18"
                    viewBox="0 0 24 24"
                    fill="none"
                    stroke="currentColor"
                    stroke-width="3"><polyline points="20 6 9 17 4 12" /></svg
                  >
                </div>
              </label>

              <!-- SMS Option -->
              <label
                class="twofa-option"
                class:selected={twoFAMethod === "sms"}
              >
                <input
                  type="radio"
                  name="twofa-method"
                  value="sms"
                  checked={twoFAMethod === "sms"}
                  on:change={() => handle2FAMethodChange("sms")}
                />
                <div class="option-icon sms-icon">
                  <svg
                    width="24"
                    height="24"
                    viewBox="0 0 24 24"
                    fill="none"
                    stroke="currentColor"
                    stroke-width="1.5"
                  >
                    <rect x="5" y="2" width="14" height="20" rx="2" /><line
                      x1="12"
                      y1="18"
                      x2="12.01"
                      y2="18"
                    />
                  </svg>
                </div>
                <div class="option-content">
                  <span class="option-title">SMS</span>
                  <span class="option-desc">{phoneMasked}</span>
                </div>
                <div class="option-check">
                  <svg
                    width="18"
                    height="18"
                    viewBox="0 0 24 24"
                    fill="none"
                    stroke="currentColor"
                    stroke-width="3"><polyline points="20 6 9 17 4 12" /></svg
                  >
                </div>
              </label>
            </div>

            <!-- TOTP Setup Section -->
            {#if showTotpSetup}
              <div class="totp-setup-panel">
                <div class="setup-header">
                  <svg
                    width="18"
                    height="18"
                    viewBox="0 0 24 24"
                    fill="none"
                    stroke="currentColor"
                    stroke-width="2"
                  >
                    <circle cx="12" cy="12" r="10" /><path d="M12 16v-4" /><path
                      d="M12 8h.01"
                    />
                  </svg>
                  <span>{$_("account.setupAuthenticatorApp")}</span>
                </div>

                {#if !totpSetup}
                  <div class="setup-generate">
                    <p>{$_("account.generateQRCodeHelper")}</p>
                    <button class="btn-generate" on:click={handleSetupTOTP}>
                      <svg
                        width="16"
                        height="16"
                        viewBox="0 0 24 24"
                        fill="none"
                        stroke="currentColor"
                        stroke-width="2"
                      >
                        <rect x="3" y="3" width="7" height="7" /><rect
                          x="14"
                          y="3"
                          width="7"
                          height="7"
                        />
                        <rect x="14" y="14" width="7" height="7" /><rect
                          x="3"
                          y="14"
                          width="7"
                          height="7"
                        />
                      </svg>
                      {$_("account.generateQRCode")}
                    </button>
                  </div>
                {:else}
                  <div class="setup-qr-section">
                    <div class="qr-wrapper">
                      <img
                        src={totpSetup.qrCode}
                        alt="TOTP QR Code"
                        class="qr-image"
                      />
                    </div>
                    <div class="qr-instructions">
                      <p class="instruction-step">
                        <span class="step-num">1</span>
                        {$_("account.step1OpenApp")}
                      </p>
                      <p class="instruction-step">
                        <span class="step-num">2</span>
                        {$_("account.step2ScanQR")}
                      </p>
                      <p class="instruction-step">
                        <span class="step-num">3</span>
                        {$_("account.step3EnterCode")}
                      </p>
                    </div>
                  </div>

                  <div class="secret-key-section">
                    <span class="secret-label">{$_("account.cantScan")}</span>
                    <div class="secret-key-box">
                      <!-- svelte-ignore a11y-click-events-have-key-events -->
                      <code
                        class="secret-code copyable"
                        on:click={() => copy.key(totpSetup.secret)}
                        >{totpSetup.secret}</code
                      >
                      <button
                        class="copy-btn"
                        on:click={() => copy.key(totpSetup.secret)}
                        title="Copy to clipboard"
                      >
                        <svg
                          width="14"
                          height="14"
                          viewBox="0 0 24 24"
                          fill="none"
                          stroke="currentColor"
                          stroke-width="2"
                        >
                          <rect x="9" y="9" width="13" height="13" rx="2" />
                          <path
                            d="M5 15H4a2 2 0 01-2-2V4a2 2 0 012-2h9a2 2 0 012 2v1"
                          />
                        </svg>
                      </button>
                    </div>
                  </div>
                {/if}

                <div class="verify-code-section">
                  <label for="totp-verify-code"
                    >{$_("account.verificationCodeLabel")}</label
                  >
                  <div class="code-input-wrapper">
                    <input
                      type="text"
                      id="totp-verify-code"
                      bind:value={totpVerifyCode}
                      maxlength="6"
                      placeholder="000000"
                      class="code-input"
                      inputmode="numeric"
                      pattern="[0-9]*"
                    />
                  </div>
                </div>
              </div>
            {/if}

            <div class="form-actions twofa-actions">
              <button class="btn-primary btn-save-2fa" on:click={handleSave2FA}>
                <svg
                  width="16"
                  height="16"
                  viewBox="0 0 24 24"
                  fill="none"
                  stroke="currentColor"
                  stroke-width="2"
                >
                  <path
                    d="M19 21H5a2 2 0 01-2-2V5a2 2 0 012-2h11l5 5v11a2 2 0 01-2 2z"
                  />
                  <polyline points="17 21 17 13 7 13 7 21" /><polyline
                    points="7 3 7 8 15 8"
                  />
                </svg>
                {$_("account.save2FASettings")}
              </button>
            </div>
          </div>
        </div>
      {/if}
      {#if activeTab === "tokens"}
        <div class="card tokens-card">
          <div class="card-header tokens-header">
            <h3 class="card-title">
              <svg
                width="18"
                height="18"
                viewBox="0 0 24 24"
                fill="none"
                stroke="currentColor"
                stroke-width="2"
              >
                <path
                  d="M21 2l-2 2m-7.61 7.61a5.5 5.5 0 1 1-7.778 7.778 5.5 5.5 0 0 1 7.777-7.777zm0 0L15.5 7.5m0 0l3 3L22 7l-3-3.5"
                />
              </svg>
              {$_("secrets.title")}
            </h3>
            <button
              class="btn btn-add-token"
              on:click={() => (showCreateSecret = !showCreateSecret)}
            >
              {#if showCreateSecret}{$_("common.cancel")}{:else}{$_(
                  "secrets.newSecret",
                )}{/if}
            </button>
          </div>
          <div class="card-body">
            <!-- <p class="text-sm text-gray-400 mb-6 font-medium tracking-tight">
              {$_("secrets.intro")}
            </p> -->

            <div class="mb-6 flex flex-col gap-4">
              <div
                class="flex items-start gap-3 p-3 rounded-lg hover:bg-white/5 transition-colors border border-transparent hover:border-white/5"
              >
                <ToggleSwitch bind:checked={showEnrollmentScript} />
                <div class="flex flex-col">
                  <span class="text-sm font-medium text-gray-200"
                    >Show Enrollment Script</span
                  >
                  <span class="text-xs text-gray-500"
                    >Generate a command to auto-enroll new devices</span
                  >
                </div>
              </div>

              {#if showEnrollmentScript}
                <div
                  class="fluent-card bg-gray-900/50 p-4 rounded-lg border border-white/5"
                  transition:scale={{ duration: 200, start: 0.95, opacity: 0 }}
                >
                  <div class="mb-4">
                    <label
                      for="token-select"
                      class="block text-xs font-medium text-gray-400 mb-2 uppercase tracking-wider"
                      >Select Token</label
                    >
                    <ComboBox
                      items={tokenItems}
                      bind:value={selectedTokenId}
                      placeholder="Select a token"
                      disabled={!tokens.length}
                      class="w-full"
                    />
                    {#if !tokens.length}
                      <p class="text-xs text-red-400 mt-1">
                        No tokens available. Please create a new secret below.
                      </p>
                    {/if}
                  </div>

                  <div class="relative group mt-4">
                    <div class="relative">
                      <TextBox
                        value={enrollmentCommand}
                        readonly
                        multiline
                        rows={4}
                        class="font-mono text-xs bg-gray-950 text-blue-300 border-gray-800 w-full"
                      />
                      <div class="absolute bottom-2 right-2">
                        <Button
                          on:click={() =>
                            copy.text(enrollmentCommand, "Command")}
                          title="Copy command"
                        >
                          <svg
                            width="16"
                            height="16"
                            viewBox="0 0 24 24"
                            fill="none"
                            stroke="currentColor"
                            stroke-width="2"
                            stroke-linecap="round"
                            stroke-linejoin="round"
                            ><rect
                              x="9"
                              y="9"
                              width="13"
                              height="13"
                              rx="2"
                              ry="2"
                            /><path
                              d="M5 15H4a2 2 0 0 1-2-2V4a2 2 0 0 1 2-2h9a2 2 0 0 1 2 2v1"
                            /></svg
                          >
                        </Button>
                      </div>
                    </div>
                  </div>

                  <p
                    class="mt-3 text-[11px] text-gray-500 flex items-center gap-2"
                  >
                    <svg
                      width="12"
                      height="12"
                      viewBox="0 0 24 24"
                      fill="none"
                      stroke="currentColor"
                      stroke-width="2"
                      ><circle cx="12" cy="12" r="10" /><line
                        x1="12"
                        y1="16"
                        x2="12"
                        y2="12"
                      /><line x1="12" y1="8" x2="12.01" y2="8" /></svg
                    >
                    Run this command on your Linux device to install the agent and
                    connect automatically.
                  </p>
                </div>
              {/if}
            </div>

            {#if showCreateSecret}
              <!-- svelte-ignore a11y-click-events-have-key-events -->
              <div
                class="fluent-card mb-6"
                transition:scale={{ duration: 250, start: 0.95, opacity: 0 }}
              >
                <!-- <div class="fluent-header">
                  <span class="fluent-title">{$_("secrets.newSecret")}</span>
                  <div class="fluent-badge">Configuration</div>
                </div> -->

                <div class="grid grid-cols-1 gap-4 items-end">
                  <div class="floating-group">
                    <input
                      id="new-secret-name"
                      class="fluent-input"
                      type="text"
                      placeholder=" "
                      bind:value={newSecretName}
                    />
                    <label for="new-secret-name" class="fluent-label"
                      >{$_("secrets.secretName")}</label
                    >
                  </div>
                  <div class="floating-group">
                    <input
                      id="new-secret-expiry"
                      class="fluent-input"
                      type="number"
                      placeholder=" "
                      bind:value={newSecretExpiresDays}
                      min="0"
                    />
                    <label for="new-secret-expiry" class="fluent-label"
                      >{$_("secrets.expiryDays")}</label
                    >
                  </div>
                  <div class="floating-group">
                    <input
                      id="new-secret-max-uses"
                      class="fluent-input"
                      type="number"
                      placeholder=" "
                      bind:value={newSecretMaxUses}
                      min="0"
                    />
                    <label for="new-secret-max-uses" class="fluent-label"
                      >{$_("secrets.maxUses")}</label
                    >
                  </div>
                  <div class="">
                    <button
                      class="fluent-btn-primary w-full h-[30px]"
                      on:click={handleCreateSecret}
                      disabled={!newSecretName.trim()}
                    >
                      {#if !newSecretName.trim()}
                        <span class="btn-text">{$_("secrets.enterName")}</span>
                      {:else}
                        <svg
                          width="18"
                          height="18"
                          viewBox="0 0 24 24"
                          fill="none"
                          stroke="currentColor"
                          stroke-width="2.5"
                        >
                          <path d="M12 5v14M5 12h14" />
                        </svg>
                        <span class="btn-text"
                          >{$_("secrets.generateSecret")}</span
                        >
                      {/if}
                    </button>
                  </div>
                </div>
              </div>
            {/if}

            {#if secretsLoading && tokens.length === 0}
              <div
                class="flex flex-col items-center justify-center py-10 px-6 animate-pulse"
              >
                <svg
                  class="spin text-blue-500"
                  width="24"
                  height="24"
                  viewBox="0 0 24 24"
                  fill="none"
                  stroke="currentColor"
                  stroke-width="2"
                >
                  <path d="M21 12a9 9 0 1 1-6.219-8.56" />
                </svg>
                <div class="mt-3 text-sm text-muted font-medium">
                  {$_("secrets.loadingSecrets")}
                </div>
              </div>
            {:else if tokens.length === 0}
              <div
                class="flex flex-col items-center justify-center py-16 px-10 rounded-2xl border border-dashed border-gray-500/20 bg-gray-500/5"
              >
                <svg
                  width="48"
                  height="48"
                  viewBox="0 0 24 24"
                  fill="none"
                  stroke="currentColor"
                  stroke-width="1"
                  class="mb-4 text-muted opacity-50"
                >
                  <path
                    d="M21 2l-2 2m-7.61 7.61a5.5 5.5 0 1 1-7.778 7.778 5.5 5.5 0 0 1 7.777-7.777zm0 0L15.5 7.5m0 0l3 3L22 7l-3-3.5"
                  />
                </svg>
                <h4 class="text-lg font-bold mb-2 opacity-80">
                  {$_("secrets.noSecrets")}
                </h4>
                <p
                  class="text-sm text-muted text-center max-w-xs leading-relaxed"
                >
                  {$_("secrets.noSecretsDesc")}
                </p>
              </div>
            {:else}
              <div class="table-container max-h-[600px] overflow-y-auto">
                <table class="w-full">
                  <thead class="sticky top-0 z-10">
                    <tr>
                      <th
                        class="px-4 py-3 text-left text-xs font-bold uppercase tracking-wider text-muted hidden sm:table-cell"
                        >{$_("common.name")}</th
                      >
                      <th
                        class="px-4 py-3 text-left text-xs font-bold uppercase tracking-wider text-muted"
                        >{$_("account.secretHeader")}</th
                      >
                      <th
                        class="px-4 py-3 text-left text-xs font-bold uppercase tracking-wider text-muted"
                        >{$_("common.status")}</th
                      >
                      <th
                        class="px-4 py-3 text-right text-xs font-bold uppercase tracking-wider text-muted"
                      />
                    </tr>
                  </thead>
                  <tbody>
                    {#each tokens as token}
                      <tr>
                        <td class="px-4 py-3 hidden sm:table-cell relative">
                          <span
                            class="text-sm block font-semibold truncate break-words relative max-w-[70px]"
                            >{token.name}</span
                          >
                          <span class="absolute inset-0" />
                        </td>
                        <td class="px-4 py-3">
                          <div
                            class="sm:hidden text-xs font-bold mb-1 opacity-70"
                          >
                            {token.name}
                          </div>
                          <!-- svelte-ignore a11y-click-events-have-key-events -->
                          <code
                            class="text-xs font-mono badge badge-success truncate cursor-pointer"
                            on:click={() => copy.text(token.token, "Secret")}
                          >
                            {token.token.substring(0, 15)}...
                          </code>
                          <svg
                            width="12"
                            height="12"
                            viewBox="0 0 24 24"
                            fill="none"
                            stroke="currentColor"
                            stroke-width="2"
                            class="opacity-0 group-hover:opacity-100 transition-opacity"
                          >
                            <rect
                              x="9"
                              y="9"
                              width="13"
                              height="13"
                              rx="2"
                            /><path
                              d="M5 15H4a2 2 0 01-2-2V4a2 2 0 012-2h9a2 2 0 012 2v1"
                            />
                          </svg>
                        </td>
                        <td class="px-4 py-3">
                          {#if !token.expiresAt || new Date(token.expiresAt).getTime() === 0 || new Date(token.expiresAt) > new Date()}
                            {#if token.maxUses > 0 && (token.usageCount || 0) >= token.maxUses}
                              <span class="badge badge-error"
                                >{$_("account.fullLabel")}</span
                              >
                            {:else}
                              <div class="flex flex-col items-start gap-1">
                                <span class="badge badge-success"
                                  >{$_("common.active")}</span
                                >
                                {#if token.expiresAt && new Date(token.expiresAt).getTime() !== 0}
                                  <span
                                    class="text-[10px] text-muted whitespace-nowrap"
                                    >{$_("account.expiresLabel")}
                                    {new Date(
                                      token.expiresAt,
                                    ).toLocaleDateString()}</span
                                  >
                                {/if}
                              </div>
                            {/if}
                          {:else}
                            <span class="badge badge-error"
                              >{$_("account.expiredLabel")}</span
                            >
                          {/if}
                        </td>
                        <td class="px-4 py-3 text-right">
                          <button
                            class="p-2 text-muted cursor-pointer bg-transparent border-none text-red-500 transition-colors"
                            on:click={() => handleDeleteSecret(token.id)}
                            title={$_("common.delete")}
                          >
                            <svg
                              width="16"
                              height="16"
                              viewBox="0 0 24 24"
                              fill="none"
                              xmlns="http://www.w3.org/2000/svg"
                            >
                              <path
                                d="M3 6h18M8 6V4h8v2M19 6v14a2 2 0 01-2 2H7a2 2 0 01-2-2V6"
                                stroke="currentColor"
                                stroke-width="2"
                                stroke-linecap="round"
                                stroke-linejoin="round"
                              />
                              <path
                                d="M10 11v6M14 11v6"
                                stroke="currentColor"
                                stroke-width="2"
                                stroke-linecap="round"
                              />
                            </svg>
                          </button>
                        </td>
                      </tr>
                    {/each}
                  </tbody>
                </table>
              </div>
            {/if}
          </div>
        </div>
      {/if}

      {#if activeTab === "widgets"}
        <div class="card widgets-card">
          <div class="card-header widgets-header">
            <div>
              <h3 class="card-title">
                <svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2">
                  <rect x="3" y="3" width="7" height="7" rx="1.5" />
                  <rect x="14" y="3" width="7" height="7" rx="1.5" />
                  <rect x="3" y="14" width="7" height="7" rx="1.5" />
                  <rect x="14" y="14" width="7" height="7" rx="1.5" />
                </svg>
                {$_("widgets.desktopWidgets")}
              </h3>
              <p class="widgets-subtitle">{$_("widgets.accountPanelDescription")}</p>
            </div>
          </div>

          <div class="card-body widgets-body">
            <div class="widget-table-wrap">
              <table class="widget-table">
                <thead>
                  <tr>
                    <th>{$_("common.name")}</th>
                    <th class="widget-table-toggle-head">{$_("common.enabled")}</th>
                  </tr>
                </thead>
                <tbody>
                  {#each orderedWidgets as widget (widget.id)}
                    <tr>
                      <td>
                        <div class="widget-row-copy">
                          <strong>{widgetTitle(widget.id)}</strong>
                          <span>{widgetDescription(widget.id)}</span>
                        </div>
                      </td>
                      <td class="widget-toggle-cell">
                        <div class="widget-toggle-field">
                          <ToggleSwitch
                            checked={widget.enabled}
                            on:change={() => widgetStore.toggleEnabled(widget.id)}
                          >
                            {widget.enabled ? $_("common.enabled") : $_("common.disabled")}
                          </ToggleSwitch>
                        </div>
                      </td>
                    </tr>
                  {/each}
                </tbody>
              </table>
            </div>
          </div>
        </div>
      {/if}

      {#if activeTab === "snapshots"}
        <div class="card">
          <div class="card-header" style="display: flex; justify-content: space-between; align-items: center;">
            <h3 class="card-title">
              <svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2">
                <path d="M21 16V8a2 2 0 0 0-1-1.73l-7-4a2 2 0 0 0-2 0l-7 4A2 2 0 0 0 3 8v8a2 2 0 0 0 1 1.73l7 4a2 2 0 0 0 2 0l7-4A2 2 0 0 0 21 16z"/>
                <polyline points="3.27 6.96 12 12.01 20.73 6.96"/><line x1="12" y1="22.08" x2="12" y2="12"/>
              </svg>
              Device Snapshots & Backups
            </h3>
            <div style="font-size: 11px; color: rgb(var(--clr) / 40%);">
              WUSP snapshots + MikroTik backups
            </div>
          </div>
          <div class="card-body">
            {#if snapshotState.isLoading}
              <p style="color: rgb(var(--clr) / 50%); padding: 16px;">Loading snapshots...</p>
            {:else if snapshotState.snapshots.length === 0}
              <div style="color: rgb(var(--clr) / 50%); padding: 16px; text-align: center;">
                <p>No snapshots or backups saved yet.</p>
                <p style="font-size: 12px; margin-top: 8px;">
                  <strong>WUSP devices:</strong> Use the Snapshots tab in the WUSP Dashboard.<br/>
                  <strong>MikroTik:</strong> Enable the backup scheduler in the Device Config script.
                </p>
              </div>
            {:else}
              <!-- Filter bar — mirrors the Peers.svelte search shell -->
              <div class="snap-filterbar">
                <div class="snap-search-wrap">
                  <svg class="snap-search-icon" width="14" height="14" viewBox="0 0 24 24" fill="none">
                    <circle cx="11" cy="11" r="7" stroke="currentColor" stroke-width="2"/>
                    <path d="M16 16L21 21" stroke="currentColor" stroke-width="2" stroke-linecap="round"/>
                  </svg>
                  <input
                    class="snap-search-input"
                    type="text"
                    placeholder="Search by name, serial, or model"
                    bind:value={snapSearch}
                  />
                </div>
                <select class="snap-select" bind:value={snapPeerFilter} title="Filter by peer">
                  <option value="">All peers</option>
                  {#each $peerStore.peers || [] as p}
                    <option value={p.id}>{p.name}</option>
                  {/each}
                </select>
                <select class="snap-select" bind:value={snapTagFilter} title="Filter by peer tag">
                  <option value="">All tags</option>
                  {#each allPeerTags as t}
                    <option value={t}>{t}</option>
                  {/each}
                </select>
                <input
                  class="snap-date"
                  type="date"
                  bind:value={snapFromDate}
                  title="From date"
                />
                <span class="snap-date-sep">→</span>
                <input
                  class="snap-date"
                  type="date"
                  bind:value={snapToDate}
                  title="To date"
                />
                {#if snapSearch || snapPeerFilter || snapTagFilter || snapFromDate || snapToDate}
                  <button class="snap-clear" on:click={clearSnapshotFilters} title="Clear filters">
                    <svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2">
                      <line x1="18" y1="6" x2="6" y2="18"/><line x1="6" y1="6" x2="18" y2="18"/>
                    </svg>
                  </button>
                {/if}
                <span class="snap-count">{filteredSnapshots.length} / {snapshotState.snapshots.length}</span>
              </div>

              {#if filteredSnapshots.length === 0}
                <div style="color: rgb(var(--clr) / 50%); padding: 24px; text-align: center; font-size: 13px;">
                  No snapshots match your filters.
                </div>
              {:else}
                <div style="display: flex; flex-direction: column; gap: 8px; padding: 8px;">
                  {#each filteredSnapshots as snap (snap.id)}
                    {@const peerForSnap = ($peerStore.peers || []).find(
                      (p) => (p.name || '').toLowerCase() === (snap.name || '').toLowerCase()
                    )}
                    <div class="snapshot-row">
                      <div class="snapshot-info">
                        <div class="snapshot-name">
                          {snap.name || 'Unnamed'}
                          <span class="snapshot-protocol">{snap.protocol || 'wusp'}</span>
                          {#if peerForSnap?.tags?.length}
                            {#each peerForSnap.tags as t}
                              <span class="snapshot-tag">{t}</span>
                            {/each}
                          {/if}
                        </div>
                        <div class="snapshot-meta">
                          {snap.manufacturer || ''} {snap.product_class || ''}
                          {#if snap.software_version} — v{snap.software_version}{/if}
                          {#if snap.created_at}
                            <span style="margin-left: 8px;">
                              {new Date(snap.created_at * 1000).toLocaleString()}
                            </span>
                          {/if}
                        </div>
                      </div>
                      <div class="snapshot-actions">
                        {#if snap.backup_size && snap.backup_size > 0}
                          <a
                            href="/api/snapshot/download?id={snap.id}"
                            class="snapshot-btn download"
                            download
                          >
                            <svg width="12" height="12" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2">
                              <path d="M21 15v4a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2v-4"/><polyline points="7 10 12 15 17 10"/><line x1="12" y1="15" x2="12" y2="3"/>
                            </svg>
                            {Math.round((snap.backup_size || 0) / 1024)}KB
                          </a>
                        {/if}
                        <button
                          class="snapshot-btn delete"
                          on:click={() => snapshotStore.delete(snap.id)}
                        >Delete</button>
                      </div>
                    </div>
                  {/each}
                </div>
              {/if}
            {/if}
          </div>
        </div>
      {/if}
    </div>

    <div class="status-bar">
      <span>{statusBar}</span>
    </div>
  </div>
</div>

<style lang="scss">
  .language-selector {
    position: relative;
    width: 100%;

    select {
      appearance: none;
      width: 100%;
      cursor: pointer;
      padding: 10px 12px;
      padding-right: 36px;
      background: rgb(var(--bg2));
      border: 1px solid rgb(var(--clr) / 20%);
      border-radius: 6px;
      font-size: 13px;
      color: rgb(var(--clr));
      transition: all 0.2s;
      outline: none;

      &:focus {
        border-color: rgb(var(--clrPrm));
        background: rgb(var(--bg2));
        box-shadow: 0 0 0 2px rgb(var(--clrPrm) / 20%);
      }

      &:hover {
        border-color: rgb(var(--clrPrm) / 50%);
      }
    }

    .select-arrow {
      position: absolute;
      right: 12px;
      top: 50%;
      transform: translateY(-50%);
      pointer-events: none;
      color: rgb(var(--clr) / 60%);
      display: flex;
    }
  }

  .account {
    background: var(--mica);
    position: absolute;
    top: 8%;
    left: 15%;
    border-radius: 8px;
    overflow: hidden;
    resize: both;
    width: 720px;
    min-height: 520px;
    max-height: 85vh;
    display: flex;
    flex-direction: column;
  }

  .shared-account-banner {
    display: flex;
    align-items: center;
    gap: 8px;
    padding: 8px 16px;
    background: linear-gradient(
      90deg,
      rgba(16, 185, 129, 0.15),
      rgba(16, 185, 129, 0.05)
    );
    border-bottom: 1px solid rgba(16, 185, 129, 0.2);
    color: #10b981;
    font-size: 13px;

    svg {
      flex-shrink: 0;
    }

    span {
      flex: 1;

      strong {
        font-weight: 600;
      }
    }

    .btn-back {
      display: flex;
      align-items: center;
      gap: 4px;
      padding: 4px 10px;
      background: rgba(16, 185, 129, 0.15);
      border: 1px solid rgba(16, 185, 129, 0.3);
      border-radius: 4px;
      color: #10b981;
      font-size: 12px;
      font-weight: 500;
      cursor: pointer;
      transition: all 0.15s ease;

      &:hover:not(:disabled) {
        background: rgba(16, 185, 129, 0.25);
        border-color: rgba(16, 185, 129, 0.5);
      }

      &:disabled {
        opacity: 0.5;
        cursor: not-allowed;
      }
    }
  }

  .account.maximized {
    position: fixed !important;
    top: 0 !important;
    left: 0 !important;
    width: 100vw !important;
    height: calc(100vh - 48px) !important;
    max-height: none;
    border-radius: 0;
    resize: none;
  }

  .account.minimized {
    display: none;
  }

  .mainApp {
    display: flex;
    flex-direction: column;
    flex: 1;
    overflow: hidden;
  }

  .message {
    margin: 12px 16px 0;
    padding: 10px 14px;
    background: rgba(16, 185, 129, 0.12);
    border: 1px solid rgba(16, 185, 129, 0.25);
    border-radius: 6px;
    color: #10b981;
    font-size: 13px;
    font-weight: 500;
  }

  .message.error {
    background: rgba(239, 68, 68, 0.12);
    border-color: rgba(239, 68, 68, 0.25);
    color: #ef4444;
  }

  .message.info {
    background: rgba(59, 130, 246, 0.12);
    border-color: rgba(59, 130, 246, 0.25);
    color: #3b82f6;
  }

  .tabs {
    display: flex;
    gap: 4px;
    flex-wrap: wrap;
    align-content: flex-start;
    overflow: hidden;
    max-width: 100%;
    position: relative;
    padding: 12px 16px;
    background: rgb(var(--bg2) / 40%);
    border-bottom: 1px solid rgb(var(--clr) / 8%);
    flex-shrink: 0;
  }

  .tab-btn {
    display: flex;
    align-items: center;
    justify-content: center;
    gap: 8px;
    padding: 10px 16px;
    background: transparent;
    border: 1px solid transparent;
    border-radius: 8px;
    color: rgb(var(--clr) / 60%);
    font-size: 13px;
    font-weight: 500;
    cursor: pointer;
    transition: all 0.2s ease;
    flex-shrink: 0;
    white-space: nowrap;
  }

  .tab-btn svg {
    opacity: 0.6;
    flex-shrink: 0;
    transition: all 0.2s ease;
  }

  .tab-btn:hover {
    color: rgb(var(--clr) / 85%);
    background: rgb(var(--clr) / 6%);
    border-color: rgb(var(--clr) / 10%);
  }

  .tab-btn:hover svg {
    opacity: 0.8;
  }

  .tab-btn.active {
    color: rgb(var(--clrPrm));
    background: rgb(var(--clrPrm) / 12%);
    border-color: rgb(var(--clrPrm) / 25%);
    font-weight: 600;
  }

  .tab-btn.active svg {
    opacity: 1;
    color: rgb(var(--clrPrm));
  }

  .content {
    flex: 1;
    min-width: 0;
    overflow-y: auto;
    padding: 16px;
    display: flex;
    flex-direction: column;
    gap: 16px;
  }

  .card {
    background: rgb(var(--bg2) / 60%);
    border: 1px solid rgb(var(--clr) / 8%);
    border-radius: 8px;
    overflow-y: scroll;
    overflow-x: hidden;
    scrollbar-width: none;
    -ms-overflow-style: none;
  }

  .card-header {
    padding: 14px 16px;
    border-bottom: 1px solid rgb(var(--clr) / 8%);
    background: rgb(var(--bg3) / 30%);
  }

  .card-title {
    margin: 0;
    font-size: 14px;
    font-weight: 600;
    color: rgb(var(--clr));
    display: flex;
    align-items: center;
    gap: 8px;
  }

  .card-title svg {
    color: rgb(var(--clrPrm));
  }

  .card-body {
    padding: 16px;
  }

  .snapshot-row {
    display: flex;
    justify-content: space-between;
    align-items: center;
    padding: 10px 12px;
    background: rgb(var(--bg2) / 30%);
    border-radius: 6px;
    border: 1px solid rgb(var(--clr) / 8%);
  }
  .snapshot-info { flex: 1; min-width: 0; }
  .snapshot-name {
    color: rgb(var(--clr) / 90%);
    font-size: 13px;
    font-weight: 600;
    display: flex;
    align-items: center;
    gap: 8px;
  }
  .snapshot-protocol {
    font-size: 10px;
    font-weight: 500;
    padding: 1px 6px;
    border-radius: 3px;
    background: rgb(var(--clrPrm) / 12%);
    color: rgb(var(--clrPrm));
    text-transform: uppercase;
  }
  .snapshot-meta {
    font-size: 11px;
    color: rgb(var(--clr) / 45%);
    margin-top: 2px;
  }
  .snapshot-actions {
    display: flex;
    gap: 6px;
    flex-shrink: 0;
  }
  .snapshot-btn {
    padding: 4px 10px;
    font-size: 11px;
    font-weight: 500;
    border-radius: 4px;
    border: 1px solid rgb(var(--clr) / 15%);
    background: rgb(var(--clr) / 5%);
    color: rgb(var(--clr) / 70%);
    cursor: pointer;
    text-decoration: none;
    display: flex;
    align-items: center;
    gap: 4px;
    transition: all 0.15s;
  }
  .snapshot-btn:hover { background: rgb(var(--clr) / 10%); }
  .snapshot-btn.download {
    background: rgb(var(--clrPrm) / 10%);
    border-color: rgb(var(--clrPrm) / 25%);
    color: rgb(var(--clrPrm));
  }
  .snapshot-btn.download:hover { background: rgb(var(--clrPrm) / 18%); }
  .snapshot-btn.delete { color: rgb(239, 68, 68, 0.7); border-color: rgb(239, 68, 68, 0.2); }
  .snapshot-btn.delete:hover { background: rgb(239, 68, 68, 0.08); }

  /* Per-peer tag chip on a snapshot row (e.g. "production", "staging"). */
  .snapshot-tag {
    font-size: 10px;
    font-weight: 500;
    padding: 1px 6px;
    border-radius: 3px;
    background: rgb(var(--clr) / 8%);
    color: rgb(var(--clr) / 60%);
  }

  /* ── Snapshot filter bar ───────────────────────────────────────────────
     Single flex-wrap row that holds search + two selects + date range +
     clear + count. Inputs share the same height + radius so they read as
     one toolbar even when wrapping to multiple lines on narrow widths. */
  .snap-filterbar {
    display: flex;
    flex-wrap: wrap;
    align-items: center;
    gap: 8px;
    padding: 12px;
    border-bottom: 1px solid rgb(var(--clr) / 8%);
    background: rgb(var(--bg2) / 25%);
  }
  .snap-filterbar :global(input),
  .snap-filterbar :global(select) {
    height: 32px;
    box-sizing: border-box;
  }
  .snap-search-wrap {
    position: relative;
    flex: 1 1 220px;
    min-width: 180px;
  }
  .snap-search-icon {
    position: absolute;
    left: 10px;
    top: 50%;
    transform: translateY(-50%);
    color: rgb(var(--clr) / 45%);
    pointer-events: none;
  }
  .snap-search-input {
    width: 100%;
    padding: 0 10px 0 30px;
    border-radius: 6px;
    border: 1px solid rgb(var(--clr) / 12%);
    background: rgb(var(--bg1));
    color: rgb(var(--clr) / 90%);
    font-size: 13px;
    outline: none;
    transition: border-color 0.15s;
  }
  .snap-search-input:focus { border-color: rgb(var(--clrPrm) / 60%); }
  .snap-search-input::placeholder { color: rgb(var(--clr) / 35%); }

  .snap-select {
    flex: 0 1 160px;
    min-width: 120px;
    padding: 0 28px 0 10px;
    border-radius: 6px;
    border: 1px solid rgb(var(--clr) / 12%);
    background: rgb(var(--bg1));
    color: rgb(var(--clr) / 85%);
    font-size: 13px;
    appearance: none;
    -webkit-appearance: none;
    background-image: url("data:image/svg+xml;utf8,<svg xmlns='http://www.w3.org/2000/svg' viewBox='0 0 24 24' fill='none' stroke='%23888' stroke-width='2' stroke-linecap='round' stroke-linejoin='round'><polyline points='6 9 12 15 18 9'/></svg>");
    background-repeat: no-repeat;
    background-position: right 8px center;
    background-size: 14px;
    cursor: pointer;
    outline: none;
  }
  .snap-select:focus { border-color: rgb(var(--clrPrm) / 60%); }

  .snap-date {
    flex: 0 1 140px;
    min-width: 130px;
    padding: 0 8px;
    border-radius: 6px;
    border: 1px solid rgb(var(--clr) / 12%);
    background: rgb(var(--bg1));
    color: rgb(var(--clr) / 85%);
    font-size: 12px;
    font-family: inherit;
    outline: none;
    color-scheme: dark;
  }
  .snap-date:focus { border-color: rgb(var(--clrPrm) / 60%); }
  .snap-date-sep {
    color: rgb(var(--clr) / 45%);
    font-size: 14px;
    flex-shrink: 0;
  }

  .snap-clear {
    height: 32px;
    width: 32px;
    display: inline-flex;
    align-items: center;
    justify-content: center;
    border-radius: 6px;
    border: 1px solid rgb(var(--clr) / 12%);
    background: rgb(var(--clr) / 5%);
    color: rgb(var(--clr) / 70%);
    cursor: pointer;
    flex-shrink: 0;
    transition: all 0.15s;
  }
  .snap-clear:hover {
    background: rgb(var(--clr) / 10%);
    color: rgb(var(--clr) / 90%);
  }

  .snap-count {
    margin-left: auto;
    font-size: 11px;
    color: rgb(var(--clr) / 50%);
    font-variant-numeric: tabular-nums;
    padding: 4px 10px;
    border-radius: 4px;
    background: rgb(var(--clr) / 6%);
    flex-shrink: 0;
  }

  /* Mobile: stack the snapshot rows so action buttons don't get pushed off-
     screen, and let the filter bar items grow to full width when wrapping. */
  @media (max-width: 768px) {
    .snap-filterbar { padding: 10px; }
    .snap-search-wrap { flex-basis: 100%; }
    .snap-select { flex: 1 1 calc(50% - 4px); }
    .snap-date { flex: 1 1 calc(50% - 22px); }
    .snap-count { margin-left: 0; }

    .snapshot-row {
      flex-direction: column;
      align-items: stretch;
      gap: 10px;
    }
    .snapshot-actions {
      justify-content: flex-end;
      flex-wrap: wrap;
    }
    .snapshot-name { flex-wrap: wrap; }
    .snapshot-meta { font-size: 11px; line-height: 1.5; }
  }

  /* Skeleton Loading Styles */
  .skeleton-container {
    padding: 16px;
  }

  .skeleton-card {
    background: rgb(var(--bg2));
    border-radius: 12px;
    padding: 20px;
    border: 1px solid rgb(var(--clr) / 8%);
  }

  .skeleton-header {
    display: flex;
    align-items: center;
    gap: 12px;
    margin-bottom: 24px;
    padding-bottom: 16px;
    border-bottom: 1px solid rgb(var(--clr) / 10%);
  }

  .skeleton-icon {
    width: 24px;
    height: 24px;
    border-radius: 6px;
    background: linear-gradient(
      90deg,
      rgb(var(--clr) / 8%) 25%,
      rgb(var(--clr) / 15%) 50%,
      rgb(var(--clr) / 8%) 75%
    );
    background-size: 200% 100%;
    animation: skeleton-shimmer 1.5s infinite;
  }

  .skeleton-title {
    height: 20px;
    width: 180px;
    border-radius: 4px;
    background: linear-gradient(
      90deg,
      rgb(var(--clr) / 8%) 25%,
      rgb(var(--clr) / 15%) 50%,
      rgb(var(--clr) / 8%) 75%
    );
    background-size: 200% 100%;
    animation: skeleton-shimmer 1.5s infinite;
  }

  .skeleton-body {
    display: flex;
    flex-direction: column;
    gap: 20px;
  }

  .skeleton-field {
    display: flex;
    flex-direction: column;
    gap: 8px;
  }

  .skeleton-label {
    height: 14px;
    width: 100px;
    border-radius: 4px;
    background: linear-gradient(
      90deg,
      rgb(var(--clr) / 8%) 25%,
      rgb(var(--clr) / 15%) 50%,
      rgb(var(--clr) / 8%) 75%
    );
    background-size: 200% 100%;
    animation: skeleton-shimmer 1.5s infinite;
  }

  .skeleton-input {
    height: 42px;
    width: 100%;
    border-radius: 6px;
    background: linear-gradient(
      90deg,
      rgb(var(--clr) / 8%) 25%,
      rgb(var(--clr) / 15%) 50%,
      rgb(var(--clr) / 8%) 75%
    );
    background-size: 200% 100%;
    animation: skeleton-shimmer 1.5s infinite;
  }

  .skeleton-button {
    height: 42px;
    width: 140px;
    border-radius: 8px;
    margin-top: 8px;
    background: linear-gradient(
      90deg,
      rgb(var(--clrPrm) / 20%) 25%,
      rgb(var(--clrPrm) / 35%) 50%,
      rgb(var(--clrPrm) / 20%) 75%
    );
    background-size: 200% 100%;
    animation: skeleton-shimmer 1.5s infinite;
  }

  @keyframes skeleton-shimmer {
    0% {
      background-position: 200% 0;
    }
    100% {
      background-position: -200% 0;
    }
  }

  .form-group {
    margin-bottom: 16px;
  }

  .form-group label {
    display: block;
    font-size: 12px;
    font-weight: 500;
    color: rgb(var(--clr) / 70%);
    margin-bottom: 6px;
  }

  .form-group input {
    width: 100%;
    padding: var(--sp-3) var(--sp-4);
    background: rgb(var(--bg3));
    border: 1px solid var(--border-color);
    border-radius: var(--radius-sm);
    color: rgb(var(--clr));
    font-size: var(--text-base);
    transition: var(--trans-normal);
  }

  .form-group input:focus {
    outline: none;
    border-color: var(--primary);
    box-shadow: 0 0 0 3px rgb(var(--primary-rgb) / 10%);
  }

  .hint {
    display: block;
    font-size: 11px;
    color: rgb(var(--clr) / 50%);
    margin-top: 4px;
  }

  .form-actions {
    display: flex;
    justify-content: flex-end;
    margin-top: 20px;
  }

  .btn-primary:hover:not(:disabled) {
    background: rgb(var(--clrPrm) / 85%);
    transform: translateY(-1px);
  }

  .btn-primary:disabled {
    opacity: 0.6;
    cursor: not-allowed;
  }

  .btn-secondary {
    padding: 8px 16px;
    background: rgb(var(--clr) / 10%);
    color: rgb(var(--clr));
    border: 1px solid rgb(var(--clr) / 20%);
    border-radius: 6px;
    font-size: 13px;
    font-weight: 500;
    cursor: pointer;
    transition: all 0.2s;
  }

  .btn-secondary:hover {
    background: rgb(var(--clr) / 15%);
  }

  .btn-danger {
    padding: 10px 20px;
    background: #dc3545;
    color: white;
    border: none;
    border-radius: 6px;
    font-size: 13px;
    font-weight: 600;
    cursor: pointer;
    transition: all 0.2s;
  }

  .btn-danger:hover {
    background: #c82333;
  }

  .danger-card {
    border-color: rgba(220, 53, 69, 0.4);
    background: rgba(220, 53, 69, 0.05);
  }

  .danger-title {
    color: #dc3545 !important;
  }
  .danger-title svg {
    color: #dc3545 !important;
  }

  .danger-text {
    margin: 0 0 16px;
    color: rgb(var(--clr) / 70%);
    font-size: 13px;
  }

  // ============================================================================
  // Two-Factor Authentication Styles
  // ============================================================================
  .twofa-card {
    overflow-y: scroll;
    overflow-x: hidden;
    /* hide scrollbar */
    scrollbar-width: none;
    -ms-overflow-style: none;
  }
  .twofa-card .card-body {
    padding: 20px;
  }

  .twofa-status-badge {
    display: flex;
    align-items: center;
    gap: 14px;
    padding: 14px 18px;
    border-radius: 10px;
    margin-bottom: 16px;
    transition: all 0.2s ease;
  }

  .twofa-status-badge.enabled {
    background: linear-gradient(
      135deg,
      rgba(16, 185, 129, 0.12) 0%,
      rgba(16, 185, 129, 0.06) 100%
    );
    border: 1px solid rgba(16, 185, 129, 0.25);
  }

  .twofa-status-badge.disabled {
    background: linear-gradient(
      135deg,
      rgba(239, 68, 68, 0.1) 0%,
      rgba(239, 68, 68, 0.05) 100%
    );
    border: 1px solid rgba(239, 68, 68, 0.2);
  }

  .twofa-status-badge .status-icon {
    display: flex;
    align-items: center;
    justify-content: center;
    width: 40px;
    height: 40px;
    border-radius: 50%;
    flex-shrink: 0;
  }

  .twofa-status-badge.enabled .status-icon {
    background: rgba(16, 185, 129, 0.15);
    color: #10b981;
  }

  .twofa-status-badge.disabled .status-icon {
    background: rgba(239, 68, 68, 0.12);
    color: #ef4444;
  }

  .twofa-status-badge .status-text {
    display: flex;
    flex-direction: column;
    gap: 2px;
  }

  .twofa-status-badge .status-label {
    font-size: 11px;
    text-transform: uppercase;
    letter-spacing: 0.5px;
    color: rgb(var(--clr) / 50%);
    font-weight: 500;
  }

  .twofa-status-badge .status-value {
    font-size: 15px;
    font-weight: 600;
  }

  .twofa-status-badge.enabled .status-value {
    color: #10b981;
  }
  .twofa-status-badge.disabled .status-value {
    color: #ef4444;
  }

  .twofa-description {
    font-size: 13px;
    color: rgb(var(--clr) / 65%);
    line-height: 1.5;
    margin: 0 0 20px;
  }

  .twofa-options {
    display: grid;
    grid-template-columns: repeat(auto-fit, minmax(200px, 1fr));
    gap: 12px;
    margin-bottom: 20px;
  }

  .twofa-option {
    display: flex;
    align-items: center;
    gap: 12px;
    padding: 14px 16px;
    background: rgb(var(--bg1));
    border: 2px solid rgb(var(--clr) / 10%);
    border-radius: 10px;
    cursor: pointer;
    transition: all 0.2s ease;
    position: relative;
  }

  .twofa-option:hover {
    background: rgb(var(--clr) / 4%);
    border-color: rgb(var(--clr) / 20%);
  }

  .twofa-option.selected {
    background: rgb(var(--clrPrm) / 8%);
    border-color: rgb(var(--clrPrm) / 50%);
  }

  .twofa-option input[type="radio"] {
    position: absolute;
    opacity: 0;
    width: 0;
    height: 0;
  }

  .twofa-option .option-icon {
    display: flex;
    align-items: center;
    justify-content: center;
    width: 44px;
    height: 44px;
    border-radius: 10px;
    flex-shrink: 0;
    transition: all 0.2s ease;
  }

  .twofa-option .option-icon.disabled-icon {
    background: rgb(var(--clr) / 8%);
    color: rgb(var(--clr) / 50%);
  }

  .twofa-option .option-icon.totp-icon {
    background: rgba(99, 102, 241, 0.12);
    color: #6366f1;
  }

  .twofa-option .option-icon.email-icon {
    background: rgba(59, 130, 246, 0.12);
    color: #3b82f6;
  }

  .twofa-option .option-icon.sms-icon {
    background: rgba(16, 185, 129, 0.12);
    color: #10b981;
  }

  .twofa-option .option-content {
    display: flex;
    flex-direction: column;
    gap: 2px;
    flex: 1;
    min-width: 0;
  }

  .twofa-option .option-title {
    font-size: 12px;
    font-weight: 500;
    color: rgb(var(--clr));
  }

  .twofa-option .option-desc {
    font-size: 12px;
    color: rgb(var(--clr) / 55%);
    white-space: nowrap;
    overflow: hidden;
    text-overflow: ellipsis;
  }

  .twofa-option .option-check {
    display: flex;
    align-items: center;
    justify-content: center;
    width: 24px;
    height: 24px;
    border-radius: 50%;
    background: rgb(var(--clr) / 8%);
    color: transparent;
    flex-shrink: 0;
    transition: all 0.2s ease;
  }

  .twofa-option.selected .option-check {
    background: rgb(var(--clrPrm));
    color: white;
  }

  // TOTP Setup Panel
  .totp-setup-panel {
    background: linear-gradient(
      135deg,
      rgb(var(--bg1)) 0%,
      rgb(var(--clr) / 3%) 100%
    );
    border: 1px solid rgb(var(--clr) / 12%);
    border-radius: 12px;
    padding: 20px;
    margin-bottom: 20px;
  }

  .totp-setup-panel .setup-header {
    display: flex;
    align-items: center;
    gap: 10px;
    font-weight: 600;
    font-size: 14px;
    color: rgb(var(--clrPrm));
    margin-bottom: 16px;
    padding-bottom: 12px;
    border-bottom: 1px solid rgb(var(--clr) / 10%);
  }

  .setup-generate {
    text-align: center;
    padding: 20px;
  }

  .setup-generate p {
    font-size: 13px;
    color: rgb(var(--clr) / 70%);
    margin: 0 0 16px;
  }

  .btn-generate {
    display: inline-flex;
    align-items: center;
    gap: 8px;
    padding: 12px 24px;
    background: linear-gradient(
      135deg,
      rgb(var(--clrPrm)) 0%,
      rgb(var(--clrPrm) / 80%) 100%
    );
    color: white;
    border: none;
    border-radius: 8px;
    font-size: 14px;
    font-weight: 600;
    cursor: pointer;
    transition: all 0.2s ease;
  }

  .btn-generate:hover {
    transform: translateY(-1px);
    box-shadow: 0 4px 12px rgb(var(--clrPrm) / 30%);
  }

  .setup-qr-section {
    display: flex;
    gap: 24px;
    align-items: flex-start;
    margin-bottom: 20px;
  }

  .qr-wrapper {
    flex-shrink: 0;
  }

  .qr-wrapper .qr-image {
    width: 160px;
    height: 160px;
    border-radius: 12px;
    border: 3px solid rgb(var(--clr) / 10%);
    background: white;
    padding: 8px;
  }

  .qr-instructions {
    display: flex;
    flex-direction: column;
    gap: 12px;
    padding-top: 8px;
  }

  .instruction-step {
    display: flex;
    align-items: center;
    gap: 12px;
    font-size: 13px;
    color: rgb(var(--clr) / 80%);
    margin: 0;
  }

  .step-num {
    display: flex;
    align-items: center;
    justify-content: center;
    width: 24px;
    height: 24px;
    background: rgb(var(--clrPrm) / 15%);
    color: rgb(var(--clrPrm));
    border-radius: 50%;
    font-size: 12px;
    font-weight: 700;
    flex-shrink: 0;
  }

  .secret-key-section {
    background: rgb(var(--bg1));
    border: 1px solid rgb(var(--clr) / 10%);
    border-radius: 8px;
    padding: 14px;
    margin-bottom: 20px;
  }

  .secret-key-section .secret-label {
    display: block;
    font-size: 12px;
    color: rgb(var(--clr) / 60%);
    margin-bottom: 8px;
  }

  .secret-key-box {
    display: flex;
    align-items: center;
    gap: 10px;
  }

  .secret-key-box .secret-code {
    font-family: "SF Mono", "Consolas", "Monaco", monospace;
    font-size: 14px;
    font-weight: 600;
    color: rgb(var(--clrPrm));
    letter-spacing: 1.5px;
    background: rgb(var(--clr) / 5%);
    padding: 8px 12px;
    border-radius: 6px;
    flex: 1;
    user-select: all;
    word-break: break-all;
  }

  .secret-key-box .copy-btn {
    display: flex;
    align-items: center;
    justify-content: center;
    width: 36px;
    height: 36px;
    background: rgb(var(--clr) / 8%);
    border: 1px solid rgb(var(--clr) / 12%);
    border-radius: 8px;
    color: rgb(var(--clr) / 60%);
    cursor: pointer;
    transition: all 0.2s ease;
    flex-shrink: 0;
  }

  .secret-key-box .copy-btn:hover {
    background: rgb(var(--clrPrm) / 12%);
    border-color: rgb(var(--clrPrm) / 25%);
    color: rgb(var(--clrPrm));
  }

  .verify-code-section {
    border-top: 1px solid rgb(var(--clr) / 10%);
    padding-top: 16px;
  }

  .verify-code-section label {
    display: block;
    font-size: 13px;
    font-weight: 600;
    color: rgb(var(--clr) / 80%);
    margin-bottom: 10px;
  }

  .code-input-wrapper {
    max-width: 180px;
  }

  .code-input {
    width: 100%;
    padding: 14px 18px;
    font-size: 22px;
    font-weight: 700;
    font-family: "SF Mono", "Consolas", "Monaco", monospace;
    letter-spacing: 8px;
    text-align: center;
    background: rgb(var(--bg1));
    border: 2px solid rgb(var(--clr) / 15%);
    border-radius: 10px;
    color: rgb(var(--clr));
    transition: all 0.2s ease;
  }

  .code-input:focus {
    outline: none;
    border-color: rgb(var(--clrPrm) / 50%);
    box-shadow: 0 0 0 3px rgb(var(--clrPrm) / 15%);
  }

  .code-input::placeholder {
    color: rgb(var(--clr) / 25%);
    letter-spacing: 6px;
  }

  .twofa-actions {
    padding-top: 8px;
  }

  .btn-save-2fa {
    display: inline-flex;
    align-items: center;
    gap: 8px;
  }

  // Responsive adjustments for 2FA
  @media (max-width: 480px) {
    .twofa-options {
      grid-template-columns: 1fr;
    }

    .setup-qr-section {
      flex-direction: column;
      align-items: center;
      text-align: center;
    }

    .qr-instructions {
      align-items: center;
    }

    .twofa-status-badge {
      padding: 12px 14px;
    }

    .twofa-status-badge .status-icon {
      width: 36px;
      height: 36px;
    }
  }

  // Keep old styles for backwards compatibility (can be removed later)
  .status-panel {
    padding: 12px 14px;
    background: rgb(var(--bg1));
    border: 1px solid rgb(var(--clr) / 12%);
    border-radius: 6px;
    margin-bottom: 16px;
    font-size: 13px;
  }

  .radio-group {
    display: flex;
    flex-direction: column;
    gap: 10px;
    margin-bottom: 16px;
  }

  .radio-item {
    display: flex;
    align-items: center;
    gap: 10px;
    cursor: pointer;
    font-size: 13px;
    color: rgb(var(--clr));
  }

  .radio-item input[type="radio"] {
    width: 16px;
    height: 16px;
    accent-color: rgb(var(--clrPrm));
  }

  .radio-icon {
    color: rgb(var(--clr) / 60%);
    flex-shrink: 0;
  }

  .totp-setup {
    margin-left: 26px;
    margin-bottom: 12px;
    padding: 14px;
    background: rgb(var(--bg1));
    border: 1px solid rgb(var(--clr) / 12%);
    border-radius: 6px;
  }

  .totp-label {
    margin: 0 0 12px;
    font-weight: 600;
    font-size: 13px;
    color: rgb(var(--clr));
  }

  .totp-qr-container {
    text-align: center;
    margin-bottom: 14px;
  }

  .qr-image {
    width: 180px;
    height: 180px;
    border-radius: 8px;
    border: 1px solid rgb(var(--clr) / 15%);
  }

  .qr-hint {
    font-size: 12px;
    color: rgb(var(--clr) / 60%);
    margin: 8px 0 12px;
  }

  .secret-container {
    display: inline-flex;
    align-items: center;
    gap: 8px;
    padding: 8px 12px;
    background: rgb(var(--bg1));
    border: 1px solid rgb(var(--clr) / 15%);
    border-radius: 6px;
    margin-bottom: 8px;
  }

  .secret-label {
    font-size: 11px;
    color: rgb(var(--clr) / 60%);
  }

  .secret-code {
    font-family: "Consolas", "Monaco", monospace;
    font-size: 13px;
    font-weight: 600;
    color: rgb(var(--clrPrm));
    letter-spacing: 1px;
    user-select: all;
  }

  .copy-secret-btn {
    display: inline-flex;
    align-items: center;
    justify-content: center;
    width: 28px;
    height: 28px;
    background: rgb(var(--clr) / 8%);
    border: 1px solid rgb(var(--clr) / 15%);
    border-radius: 4px;
    color: rgb(var(--clr) / 70%);
    cursor: pointer;
    transition: all 0.2s;
  }

  .copy-secret-btn:hover {
    background: rgb(var(--clrPrm) / 15%);
    border-color: rgb(var(--clrPrm) / 30%);
    color: rgb(var(--clrPrm));
  }

  .qr-manual-hint {
    font-size: 10px;
    color: rgb(var(--clr) / 40%);
    margin: 0;
    font-style: italic;
  }

  .totp-verify {
    margin-bottom: 0;
  }

  .totp-input {
    width: 100% !important;
    padding: var(--sp-3) var(--sp-4) !important;
    background: rgb(var(--bg3)) !important;
    border: 1px solid var(--border-color) !important;
    border-radius: var(--radius-sm) !important;
    color: rgb(var(--clr)) !important;
    font-size: var(--text-2xl) !important;
    text-align: center;
    letter-spacing: var(--sp-2);
    font-family: var(--font-mono);
  }

  /* ========== Access Sharing Styles ========== */
  .sharing-container {
    display: flex;
    flex-direction: column;
    gap: 16px;
    padding: 16px;
  }

  .sharing-banner {
    display: flex;
    align-items: center;
    gap: 16px;
    padding: 20px;
    background: linear-gradient(
      135deg,
      rgb(var(--clrPrm) / 12%) 0%,
      rgb(var(--bg2)) 100%
    );
    border: 1px solid rgb(var(--clrPrm) / 20%);
    border-radius: 12px;
  }

  .banner-icon {
    width: 52px;
    height: 52px;
    background: linear-gradient(
      135deg,
      rgb(var(--clrPrm)) 0%,
      rgb(var(--clrPrm) / 75%) 100%
    );
    border-radius: 12px;
    display: flex;
    align-items: center;
    justify-content: center;
    flex-shrink: 0;
  }

  .banner-icon svg {
    color: white;
  }

  .banner-text h2 {
    margin: 0 0 4px 0;
    font-size: 18px;
    font-weight: 700;
    color: rgb(var(--clr));
  }

  .banner-text p {
    margin: 0;
    font-size: 13px;
    color: rgb(var(--clr) / 60%);
    line-height: 1.4;
  }

  .sharing-panel {
    background: rgb(var(--bg2));
    border: 1px solid rgb(var(--clr) / 10%);
    border-radius: 10px;
    overflow: hidden;
  }

  .panel-header {
    display: flex;
    align-items: center;
    gap: 10px;
    padding: 14px 18px;
    background: rgb(var(--bg1));
    border-bottom: 1px solid rgb(var(--clr) / 8%);
    font-size: 14px;
    font-weight: 600;
    color: rgb(var(--clr) / 85%);
  }

  .panel-header svg {
    color: rgb(var(--clrPrm));
    flex-shrink: 0;
  }

  .panel-header.accent {
    background: rgba(245, 158, 11, 0.08);
    border-color: rgba(245, 158, 11, 0.2);
  }

  .panel-header.accent svg {
    color: #f59e0b;
  }

  .count-badge {
    margin-left: auto;
    padding: 2px 10px;
    background: rgb(var(--clrPrm) / 15%);
    border-radius: 10px;
    font-size: 12px;
    font-weight: 600;
    color: rgb(var(--clrPrm));
  }

  .count-badge.accent {
    background: rgba(245, 158, 11, 0.15);
    color: #f59e0b;
  }

  .panel-body {
    padding: 18px;
  }

  /* Invite Form */
  .invite-fields {
    display: grid;
    grid-template-columns: 1fr 1fr;
    gap: 14px;
    margin-bottom: 18px;
  }

  .field {
    display: flex;
    flex-direction: column;
    gap: 6px;
  }

  .field label {
    font-size: 12px;
    font-weight: 600;
    color: rgb(var(--clr) / 65%);
  }

  .field input {
    height: 48px;
    padding: var(--sp-3) var(--sp-4);
    background: rgb(var(--bg3));
    border: 1px solid var(--border-color);
    border-radius: var(--radius-sm);
    color: rgb(var(--clr));
    font-size: var(--text-base);
    transition: var(--trans-normal);
  }

  .field input:focus {
    outline: none;
    border-color: var(--primary);
    box-shadow: 0 0 0 3px rgb(var(--primary-rgb) / 10%);
  }

  .field input::placeholder {
    color: rgb(var(--clr) / 35%);
  }

  /* Permissions */
  .perms-section {
    margin-bottom: 18px;
  }

  .perms-label {
    display: flex;
    align-items: center;
    gap: 8px;
    margin-bottom: 12px;
    font-size: 13px;
    font-weight: 600;
    color: rgb(var(--clr) / 70%);
  }

  .perms-label svg {
    color: rgb(var(--clrPrm));
  }

  .perms-grid {
    display: grid;
    grid-template-columns: repeat(auto-fill, minmax(160px, 1fr));
    gap: 10px;
  }

  .perm-item {
    display: flex;
    align-items: center;
    gap: 10px;
    padding: 12px 14px;
    background: rgb(var(--bg1));
    border: 1px solid rgb(var(--clr) / 10%);
    border-radius: 8px;
    cursor: pointer;
    transition: all 0.2s;
  }

  .perm-item:hover {
    border-color: rgb(var(--clrPrm) / 30%);
  }

  .perm-item.checked {
    background: rgb(var(--clrPrm) / 8%);
    border-color: rgb(var(--clrPrm) / 40%);
  }

  .perm-item input[type="checkbox"] {
    width: 18px;
    height: 18px;
    accent-color: rgb(var(--clrPrm));
    cursor: pointer;
  }

  .perm-label {
    font-size: 13px;
    font-weight: 500;
    color: rgb(var(--clr) / 80%);
  }

  .perm-item.checked .perm-label {
    color: rgb(var(--clrPrm));
  }

  .btn-invite {
    display: inline-flex;
    align-items: center;
    justify-content: center;
    gap: 8px;
    width: 100%;
    padding: 14px 24px;
    background: linear-gradient(
      135deg,
      rgb(var(--clrPrm)) 0%,
      rgb(var(--clrPrm) / 80%) 100%
    );
    border: none;
    border-radius: 8px;
    font-size: 14px;
    font-weight: 600;
    color: white;
    cursor: pointer;
    transition: all 0.2s;
  }

  .btn-invite:hover:not(:disabled) {
    transform: translateY(-1px);
    box-shadow: 0 6px 20px rgb(var(--clrPrm) / 35%);
  }

  .btn-invite:disabled {
    opacity: 0.5;
    cursor: not-allowed;
  }

  /* Shares List */
  .shares-list {
    display: flex;
    flex-direction: column;
    gap: 10px;
  }

  .share-row,
  .invite-row,
  .account-row {
    display: flex;
    align-items: center;
    gap: 14px;
    padding: 14px;
    background: rgb(var(--bg1));
    border: 1px solid rgb(var(--clr) / 8%);
    border-radius: 10px;
    transition: all 0.2s;
  }

  .share-row:hover,
  .invite-row:hover,
  .account-row:hover {
    border-color: rgb(var(--clrPrm) / 20%);
    box-shadow: 0 2px 8px rgb(var(--clr) / 5%);
  }

  .share-avatar,
  .invite-avatar,
  .account-avatar {
    width: 42px;
    height: 42px;
    border-radius: 10px;
    display: flex;
    align-items: center;
    justify-content: center;
    font-size: 16px;
    font-weight: 700;
    color: white;
    flex-shrink: 0;
  }

  .share-details,
  .invite-details,
  .account-details {
    flex: 1;
    min-width: 0;
  }

  .share-name,
  .invite-name,
  .account-name {
    display: block;
    font-size: 14px;
    font-weight: 600;
    color: rgb(var(--clr));
    margin-bottom: 2px;
  }

  .share-email,
  .invite-email,
  .account-email {
    display: block;
    font-size: 12px;
    color: rgb(var(--clr) / 55%);
    white-space: nowrap;
    overflow: hidden;
    text-overflow: ellipsis;
  }

  .share-status {
    padding: 5px 12px;
    border-radius: 6px;
    font-size: 11px;
    font-weight: 600;
    text-transform: uppercase;
    letter-spacing: 0.3px;
  }

  .share-status.pending {
    background: rgba(245, 158, 11, 0.12);
    color: #f59e0b;
  }

  .share-status.active {
    background: rgba(34, 197, 94, 0.12);
    color: #22c55e;
  }

  .btn-remove {
    display: flex;
    align-items: center;
    justify-content: center;
    width: 36px;
    height: 36px;
    background: rgb(var(--clr) / 5%);
    border: 1px solid rgb(var(--clr) / 10%);
    border-radius: 8px;
    color: rgb(var(--clr) / 45%);
    cursor: pointer;
    transition: all 0.2s;
    flex-shrink: 0;
  }

  .btn-remove:hover {
    background: rgba(239, 68, 68, 0.1);
    border-color: rgba(239, 68, 68, 0.3);
    color: #ef4444;
  }

  .btn-accept {
    display: inline-flex;
    align-items: center;
    gap: 6px;
    padding: 10px 18px;
    background: linear-gradient(135deg, #22c55e 0%, #16a34a 100%);
    border: none;
    border-radius: 8px;
    font-size: 13px;
    font-weight: 600;
    color: white;
    cursor: pointer;
    transition: all 0.2s;
  }

  .btn-accept:hover {
    transform: translateY(-1px);
    box-shadow: 0 4px 12px rgba(34, 197, 94, 0.35);
  }

  .btn-switch {
    display: inline-flex;
    align-items: center;
    gap: 6px;
    padding: 10px 18px;
    background: rgb(var(--clrPrm) / 10%);
    border: 1px solid rgb(var(--clrPrm) / 25%);
    border-radius: 8px;
    font-size: 13px;
    font-weight: 600;
    color: rgb(var(--clrPrm));
    cursor: pointer;
    transition: all 0.2s;
  }

  .btn-switch:hover {
    background: rgb(var(--clrPrm) / 18%);
    border-color: rgb(var(--clrPrm) / 40%);
  }

  /* Empty & Loading States */
  .empty-state,
  .loading-state {
    display: flex;
    flex-direction: column;
    align-items: center;
    justify-content: center;
    padding: 32px 20px;
    text-align: center;
    color: rgb(var(--clr) / 45%);
  }

  .empty-state svg {
    margin-bottom: 12px;
    opacity: 0.4;
  }

  .empty-state span,
  .loading-state span {
    font-size: 13px;
  }

  .loading-state {
    flex-direction: row;
    gap: 10px;
  }

  .status-bar {
    height: 26px;
    background: rgb(var(--bg2));
    border-top: 1px solid rgb(var(--clr) / 8%);
    display: flex;
    align-items: center;
    padding: 0 12px;
    font-size: 11px;
    color: rgb(var(--clr) / 60%);
    flex-shrink: 0;
  }

  /* Mobile responsive styles */
  @media (max-width: 768px) {
    .account {
      position: fixed !important;
      width: 100vw !important;
      max-height: none;
      left: 0 !important;
      top: 0 !important;
      right: 0 !important;
      bottom: 48px !important;
      height: calc(100vh - 48px) !important;
      border-radius: 0;
      resize: none;
    }

    .content {
      padding: 10px;
      gap: 10px;
      overflow-x: hidden;
    }

    .tabs {
      flex-wrap: nowrap;
      overflow-x: auto;
      -webkit-overflow-scrolling: touch;
      scrollbar-width: none;
      padding: 8px 10px 6px;
      gap: 6px;
    }

    .tabs::-webkit-scrollbar {
      display: none;
    }

    .tab-btn {
      white-space: nowrap;
      padding: 8px 12px;
      font-size: 12px;
      flex: 0 0 auto;
    }

    .tab-btn span {
      display: none;
    }

    .card {
      border-radius: 6px;
    }

    .card-header {
      padding: 12px;
    }

    .card-body {
      padding: 12px;
    }

    .form-group input,
    .form-group select {
      font-size: 16px; /* Prevent zoom on iOS */
    }
  }

  @media (max-width: 480px) {
    .content {
      padding: 8px;
      gap: 8px;
    }

    .tabs {
      padding: 6px 8px 4px;
      gap: 4px;
    }

    .tab-btn {
      padding: 6px 10px;
      font-size: 11px;
    }

    .card-header {
      padding: 10px 12px;
    }

    .card-body {
      padding: 10px 12px;
    }

    .form-group {
      margin-bottom: 14px;
    }

    .form-group label {
      font-size: 12px;
    }

    .form-group input {
      padding: 12px 10px;
    }

    .section-title {
      font-size: 14px;
    }

    .status-bar {
      height: 22px;
      padding: 0 10px;
      font-size: 10px;
    }
  }

  /* Touch-friendly improvements */
  @media (hover: none) and (pointer: coarse) {
    .tab-btn,
    .btn-primary,
    .btn-secondary {
      min-height: 44px;
    }
  }

  /* ============================================
     Sessions Tab Styles
     ============================================ */

  .sessions-card {
    max-width: 100%;
  }

  .sessions-card .card-header {
    display: flex;
    justify-content: space-between;
    align-items: center;
  }

  .btn-refresh {
    padding: 6px 10px;
    background: transparent;
    border: 1px solid rgb(var(--clr) / 20%);
    border-radius: 6px;
    color: rgb(var(--clr) / 70%);
    cursor: pointer;
    transition: all 0.2s;
    display: flex;
    align-items: center;
    gap: 4px;

    &:hover:not(:disabled) {
      background: rgb(var(--clr) / 5%);
      border-color: rgb(var(--clr) / 30%);
    }

    &:disabled {
      opacity: 0.5;
      cursor: not-allowed;
    }
  }

  .sessions-body {
    padding: 0 !important;
  }

  .sessions-loading,
  .sessions-empty {
    display: flex;
    flex-direction: column;
    align-items: center;
    justify-content: center;
    padding: 40px 20px;
    color: rgb(var(--clr) / 50%);
    gap: 12px;
  }

  .sessions-loading svg,
  .sessions-empty svg {
    opacity: 0.5;
  }

  .sessions-table-container {
    overflow-x: auto;
    -webkit-overflow-scrolling: touch;
  }

  .sessions-table {
    width: 100%;
    border-collapse: collapse;
    font-size: 13px;
  }

  .sessions-table thead {
    background: rgb(var(--bg3));
    position: sticky;
    top: 0;
  }

  .sessions-table th {
    padding: 12px 16px;
    text-align: left;
    font-weight: 600;
    font-size: 11px;
    text-transform: uppercase;
    letter-spacing: 0.5px;
    color: rgb(var(--clr) / 60%);
    border-bottom: 1px solid rgb(var(--clr) / 10%);
  }

  .sessions-table td {
    padding: 14px 16px;
    border-bottom: 1px solid rgb(var(--clr) / 5%);
    vertical-align: middle;
  }

  .sessions-table tr:last-child td {
    border-bottom: none;
  }

  .sessions-table tr.current {
    background: rgb(var(--clrPrm) / 5%);
  }

  .sessions-table tr:hover:not(.current) {
    background: rgb(var(--clr) / 3%);
  }

  .device-cell {
    display: flex;
    align-items: center;
    gap: 8px;
    flex-wrap: wrap;
  }

  .device-info {
    display: flex;
    align-items: center;
    gap: 10px;
  }

  .device-icon {
    font-size: 20px;
    width: 32px;
    height: 32px;
    display: flex;
    align-items: center;
    justify-content: center;
    background: rgb(var(--bg3));
    border-radius: 6px;
  }

  .device-details {
    display: flex;
    flex-direction: column;
    gap: 2px;
  }

  .device-browser {
    font-weight: 500;
    color: rgb(var(--clr));
  }

  .device-os {
    font-size: 11px;
    color: rgb(var(--clr) / 60%);
  }

  .device-ip,
  .device-activity {
    display: none;
    font-size: 10px;
    color: rgb(var(--clr) / 50%);
  }

  .current-badge {
    background: rgb(var(--clrPrm));
    color: white;
    font-size: 9px;
    font-weight: 600;
    padding: 3px 8px;
    border-radius: 10px;
    text-transform: uppercase;
    letter-spacing: 0.5px;
  }

  .ip-address {
    font-family: "Cascadia Code", "Fira Code", monospace;
    font-size: 12px;
    background: rgb(var(--bg3));
    padding: 4px 8px;
    border-radius: 4px;
  }

  .activity-time {
    color: rgb(var(--clr) / 70%);
    font-size: 12px;
  }

  .actions-cell {
    text-align: right;
  }

  .btn-end-session {
    padding: 6px 12px;
    background: transparent;
    border: 1px solid var(--error);
    border-radius: var(--radius-xs);
    color: var(--error);
    font-size: 12px;
    font-weight: 500;
    cursor: pointer;
    transition: var(--trans-normal);
    display: inline-flex;
    align-items: center;
    gap: 6px;

    &:hover {
      background: var(--error);
      color: white;
    }
  }

  .current-session-text {
    font-size: 11px;
    color: rgb(var(--clr) / 40%);
    font-style: italic;
  }

  .sessions-footer {
    padding: 12px 16px;
    background: rgb(var(--bg3) / 50%);
    border-top: 1px solid rgb(var(--clr) / 5%);
  }

  .sessions-hint {
    display: flex;
    align-items: center;
    gap: 8px;
    font-size: 11px;
    color: rgb(var(--clr) / 50%);
    margin: 0;

    svg {
      flex-shrink: 0;
      opacity: 0.6;
    }
  }

  /* Spin animation */
  @keyframes spin {
    from {
      transform: rotate(0deg);
    }
    to {
      transform: rotate(360deg);
    }
  }

  .spin {
    animation: spin 1s linear infinite;
  }

  /* Mobile responsive for sessions */
  .show-mobile {
    display: none !important;
  }

  @media (max-width: 768px) {
    .hide-mobile {
      display: none !important;
    }

    .show-mobile {
      display: block !important;
    }

    .sessions-table th:first-child,
    .sessions-table td:first-child {
      padding-left: 12px;
    }

    .sessions-table th:last-child,
    .sessions-table td:last-child {
      padding-right: 12px;
    }

    .device-cell {
      flex-direction: column;
      align-items: flex-start;
    }

    .device-info {
      width: 100%;
    }

    .device-ip,
    .device-activity {
      display: block !important;
    }

    .btn-end-session span {
      display: none;
    }

    .btn-end-session {
      padding: 8px;
    }

    .current-badge {
      margin-top: 4px;
    }
  }

  /* Sharing responsive styles */
  @media (max-width: 768px) {
    .sharing-container {
      padding: 12px;
      gap: 12px;
    }

    .sharing-banner {
      padding: 16px;
      gap: 14px;
    }

    .banner-icon {
      width: 44px;
      height: 44px;
    }

    .banner-icon svg {
      width: 20px;
      height: 20px;
    }

    .banner-text h2 {
      font-size: 16px;
    }

    .banner-text p {
      font-size: 12px;
    }

    .panel-body {
      padding: 14px;
    }

    .invite-fields {
      grid-template-columns: 1fr;
      gap: 12px;
    }

    .perms-grid {
      grid-template-columns: 1fr;
    }

    .share-row,
    .invite-row,
    .account-row {
      flex-wrap: wrap;
      gap: 12px;
    }

    .share-details,
    .invite-details,
    .account-details {
      flex: 1 1 60%;
    }

    .share-status {
      order: 4;
    }

    .btn-remove,
    .btn-accept,
    .btn-switch {
      margin-left: auto;
    }
  }

  @media (max-width: 480px) {
    .sharing-banner {
      flex-direction: column;
      text-align: center;
    }

    .field input {
      height: 46px;
      font-size: 16px;
    }

    .perm-item {
      padding: 14px;
    }

    .btn-invite {
      padding: 16px;
      font-size: 15px;
    }

    .share-avatar,
    .invite-avatar,
    .account-avatar {
      width: 38px;
      height: 38px;
      font-size: 14px;
    }

    .share-name,
    .invite-name,
    .account-name {
      font-size: 13px;
    }

    .share-email,
    .invite-email,
    .account-email {
      font-size: 11px;
    }

    .btn-accept,
    .btn-switch {
      padding: 12px 16px;
    }
  }

  @media (max-width: 480px) {
    .sessions-table {
      font-size: 12px;
    }

    .device-icon {
      width: 28px;
      height: 28px;
      font-size: 16px;
    }

    .sessions-footer {
      padding: 10px 12px;
    }
  }
  /* ============================================
     Tokens Tab Styles
     ============================================ */
  .tokens-card {
    display: flex;
    flex-direction: column;
  }

  .tokens-header {
    display: flex;
    justify-content: space-between;
    align-items: center;
  }

  .btn-add-token {
    padding: 6px 12px;
    background: rgb(var(--clrPrm) / 10%);
    border: 1px solid rgb(var(--clrPrm) / 30%);
    border-radius: 6px;
    color: rgb(var(--clrPrm));
    font-size: 12px;
    font-weight: 600;
    transition: all 0.2s;

    &:hover {
      background: rgb(var(--clrPrm) / 15%);
      border-color: rgb(var(--clrPrm) / 50%);
    }
  }

  .tokens-intro {
    font-size: 13px;
    color: rgb(var(--clr) / 60%);
    margin-bottom: 24px;
    line-height: 1.5;
  }

  .create-token-form {
    background: rgb(var(--bg3) / 50%);
    border: 1px solid rgb(var(--clr) / 10%);
    border-radius: 12px;
    padding: 24px;
    margin-bottom: 32px;
    box-shadow: 0 4px 12px rgba(0, 0, 0, 0.1);

    .form-row {
      display: flex;
      gap: 16px;
      margin-bottom: 16px;

      .form-group {
        flex: 1;
        margin-bottom: 0;
      }
    }

    .form-actions {
      display: flex;
      justify-content: flex-end;
      margin-top: 24px;
    }
  }

  .tokens-list {
    display: flex;
    flex-direction: column;
    gap: 12px;
  }

  .token-item {
    display: flex;
    align-items: center;
    justify-content: space-between;
    padding: 16px;
    background: rgb(var(--bg3) / 30%);
    border: 1px solid rgb(var(--clr) / 5%);
    border-radius: 10px;
    transition: all 0.2s ease;

    &:hover {
      background: rgb(var(--bg3) / 50%);
      border-color: rgb(var(--clr) / 10%);
      transform: translateX(2px);
    }
  }

  .token-info {
    flex: 1;
    display: flex;
    flex-direction: column;
    gap: 6px;
  }

  .token-name {
    font-weight: 600;
    font-size: 14px;
    color: rgb(var(--clr));
  }

  .token-value {
    display: inline-flex;
    align-items: center;
    gap: 8px;
    background: rgb(var(--clr) / 5%);
    padding: 6px 12px;
    border: 1px solid rgb(var(--clr) / 10%);
    border-radius: var(--radius-sm);
    color: var(--success);
    font-family: var(--font-mono);
    font-size: var(--text-sm);
    cursor: pointer;
    width: fit-content;
    transition: var(--trans-normal);

    &:hover {
      background: rgb(var(--clr) / 10%);
      border-color: var(--success);
    }
  }

  .token-meta {
    display: flex;
    align-items: center;
    gap: 8px;
    font-size: 11px;
    color: rgb(var(--clr) / 40%);

    .divider {
      opacity: 0.5;
    }

    .expiry {
      color: rgb(245 158 11 / 80%);
    }
  }

  .btn-delete-token {
    color: rgb(var(--clr) / 30%);
    transition: all 0.2s;

    &:hover {
      color: #ef4444;
      background: rgba(239, 68, 68, 0.1);
    }
  }

  /* Floating Input Styles */
  .floating-group {
    position: relative;
    width: 100%;
  }

  .floating-input {
    width: 100%;
    height: 48px;
    padding: 24px 12px 6px;
    background: rgb(var(--bg1));
    border: 1px solid rgb(var(--clr) / 10%);
    border-radius: 6px;
    font-size: 14px;
    color: rgb(var(--clr));
    outline: none;
    transition: all 0.2s;

    &::placeholder {
      color: transparent;
    }

    &:focus {
      border-color: rgb(var(--clrPrm));
      background: rgb(var(--bg2));
      box-shadow: 0 0 0 2px rgb(var(--clrPrm) / 10%);
    }
  }

  .floating-label {
    position: absolute;
    left: 12px;
    top: 50%;
    transform: translateY(-50%);
    font-size: 13px;
    color: rgb(var(--clr) / 50%);
    pointer-events: none;
    transition: all 0.2s cubic-bezier(0.4, 0, 0.2, 1);
    transform-origin: left top;
    white-space: nowrap;
    overflow: hidden;
    text-overflow: ellipsis;
    max-width: calc(100% - 24px);
    z-index: 1;
  }

  .floating-input:focus ~ .floating-label,
  .floating-input:not(:placeholder-shown) ~ .floating-label {
    top: 6px;
    transform: none;
    font-size: 10px;
    font-weight: 600;
    color: rgb(var(--clrPrm));
    letter-spacing: 0.4px;
    text-transform: uppercase;
  }

  /* Fluent Design Styles */
  .fluent-card {
    background: rgba(30, 30, 30, 0.6);
    backdrop-filter: blur(20px);
    -webkit-backdrop-filter: blur(20px);
    border: 1px solid rgba(255, 255, 255, 0.08);
    border-radius: 8px;
    padding: 24px;
    // box-shadow: 0 8px 32px rgba(0, 0, 0, 0.2);
    position: relative;
    overflow: hidden;
  }

  .fluent-header {
    display: flex;
    justify-content: space-between;
    align-items: center;
    margin-bottom: 20px;
    padding-bottom: 12px;
    border-bottom: 1px solid rgba(255, 255, 255, 0.06);
  }

  .fluent-title {
    font-size: 14px;
    font-weight: 600;
    color: rgba(255, 255, 255, 0.9);
    letter-spacing: 0.5px;
    text-transform: uppercase;
  }

  .fluent-badge {
    font-size: 10px;
    background: rgba(255, 255, 255, 0.1);
    color: rgba(255, 255, 255, 0.6);
    padding: 2px 8px;
    border-radius: 4px;
    font-weight: 500;
    text-transform: uppercase;
    letter-spacing: 0.5px;
  }

  .fluent-input {
    width: 100%;
    height: 48px;
    padding: 24px 12px 6px;
    background: rgba(0, 0, 0, 0.3);
    border: 1px solid rgba(255, 255, 255, 0.08);
    border-bottom: 1px solid rgba(255, 255, 255, 0.4);
    border-radius: 4px;
    font-size: 14px;
    color: #fff;
    outline: none;
    transition: all 0.2s cubic-bezier(0.2, 0, 0, 1);

    &::placeholder {
      color: transparent;
    }

    &:hover {
      background: rgba(0, 0, 0, 0.4);
      border-color: rgba(255, 255, 255, 0.2);
    }

    &:focus {
      background: rgba(0, 0, 0, 0.5);
      border-color: rgba(255, 255, 255, 0.1);
      border-bottom-color: var(--primary);
      border-bottom-width: 2px;
    }
  }

  .fluent-label {
    position: absolute;
    left: 12px;
    top: 50%;
    transform: translateY(-50%);
    font-size: 13px;
    color: rgba(255, 255, 255, 0.5);
    pointer-events: none;
    transition: all 0.2s cubic-bezier(0.2, 0, 0, 1);
    transform-origin: left top;
    z-index: 1;
  }

  .fluent-input:focus ~ .fluent-label,
  .fluent-input:not(:placeholder-shown) ~ .fluent-label {
    top: 6px;
    transform: none;
    font-size: 10px;
    font-weight: 600;
    color: var(--primary);
    text-transform: uppercase;
    letter-spacing: 0.5px;
  }

  .fluent-input:not(:focus):not(:placeholder-shown) ~ .fluent-label {
    color: rgba(255, 255, 255, 0.4);
  }

  .fluent-btn-primary {
    display: flex;
    align-items: center;
    justify-content: center;
    gap: 8px;
    background: var(--primary);
    color: white;
    border: none;
    border-radius: 4px;
    font-size: 14px;
    font-weight: 600;
    cursor: pointer;
    transition: all 0.2s;
    box-shadow: 0 2px 4px rgba(0, 0, 0, 0.2);
    position: relative;
    overflow: hidden;

    &::after {
      content: "";
      position: absolute;
      top: 0;
      left: 0;
      width: 100%;
      height: 100%;
      background: linear-gradient(
        to bottom,
        rgba(255, 255, 255, 0.15),
        rgba(255, 255, 255, 0)
      );
      opacity: 0;
      transition: opacity 0.2s;
    }

    &:hover {
      transform: translateY(-1px);
      box-shadow: 0 4px 12px rgba(var(--primary-rgb), 0.3);

      &::after {
        opacity: 1;
      }
    }

    &:active {
      transform: translateY(0);
      box-shadow: 0 1px 2px rgba(0, 0, 0, 0.2);
    }

    &:disabled {
      background: rgba(255, 255, 255, 0.05);
      color: rgba(255, 255, 255, 0.3);
      cursor: not-allowed;
      box-shadow: none;
      transform: none;
    }

    .btn-text {
      position: relative;
      z-index: 1;
      font-weight: 600;
      font-size: 10px;
      text-transform: uppercase;
    }
  }

  /* Stat Grids */
  .stat-grid-3 {
    display: grid;
    gap: 16px;
    grid-template-columns: repeat(3, 1fr);
  }

  /* Stat Items */
  .stat-item {
    display: flex;
    flex-direction: column;
    gap: 8px;
    padding: 14px 16px;
    background: rgb(var(--bg1));
    border: 1px solid rgb(var(--clr) / 6%);
    border-radius: 6px;
    text-align: center;
    transition: all 0.15s ease;
  }

  .stat-item:hover {
    background: rgb(var(--bg1) / 80%);
    border-color: rgb(var(--clr) / 12%);
    transform: translateY(-1px);
  }

  .stat-item.simple {
    background: transparent;
    border: none;
    padding: 0;
    text-align: left;
    align-items: flex-start;
  }

  .stat-item.simple:hover {
    transform: none;
    background: transparent;
  }

  .stat-item.simple .stat-value {
    justify-content: flex-start;
    font-size: 20px;
  }

  .stat-label {
    font-size: 11px;
    font-weight: 500;
    color: rgb(var(--clr) / 60%);
    text-transform: uppercase;
    letter-spacing: 0.5px;
  }

  .stat-value {
    display: flex;
    align-items: center;
    justify-content: center;
    gap: 8px;
    font-size: 24px;
    font-weight: 600;
    line-height: 1.2;
    color: rgb(var(--clr));
  }

  /* Status Colors */
  .status-active {
    color: #10b981 !important;
  }

  .widgets-card {
    overflow: hidden;
  }

  .widgets-header {
    display: flex;
    align-items: flex-start;
    justify-content: flex-start;
    gap: 12px;
  }

  .widgets-subtitle {
    margin: 8px 0 0;
    max-width: 620px;
    font-size: 12px;
    line-height: 1.55;
    color: rgb(var(--clr) / 62%);
  }

  .widgets-body {
    display: grid;
    gap: 16px;
  }

  .widget-table-wrap {
    overflow-x: auto;
    -webkit-overflow-scrolling: touch;
    border: 1px solid rgb(var(--clr) / 8%);
    border-radius: 14px;
    background: rgb(var(--bg1) / 72%);
  }

  .widget-table {
    width: 100%;
    border-collapse: collapse;
    min-width: 560px;
  }

  .widget-table th,
  .widget-table td {
    padding: 14px 16px;
    border-bottom: 1px solid rgb(var(--clr) / 8%);
    text-align: left;
    vertical-align: middle;
  }

  .widget-table th {
    font-size: 11px;
    font-weight: 700;
    letter-spacing: 0.08em;
    text-transform: uppercase;
    color: rgb(var(--clr) / 58%);
    background: rgb(var(--bg1) / 85%);
  }

  .widget-table-toggle-head {
    width: 1%;
    white-space: nowrap;
    text-align: right;
  }

  .widget-table tbody tr:last-child td {
    border-bottom: none;
  }

  .widget-row-copy {
    display: grid;
    gap: 4px;
    min-width: 0;
  }

  .widget-row-copy strong {
    font-size: 14px;
    line-height: 1.3;
    color: rgb(var(--clr));
  }

  .widget-row-copy span {
    font-size: 12px;
    line-height: 1.5;
    color: rgb(var(--clr) / 62%);
  }

  .widget-toggle-cell {
    text-align: right !important;
    white-space: nowrap;
  }

  .widget-toggle-field {
    display: inline-flex;
    justify-content: flex-end;
    min-width: 0;
  }

  .widget-toggle-field :global(.toggle-switch-container) {
    justify-content: flex-end;
    gap: 10px;
    color: rgb(var(--clr) / 78%);
    font-size: 12px;
    font-weight: 600;
  }

  .widget-toggle-field :global(.toggle-switch-container > span) {
    padding-inline-start: 0;
  }

  .widget-toggle-field :global(.toggle-switch) {
    flex: 0 0 auto;
  }

  @media (max-width: 920px) {
    .widgets-header {
      align-items: flex-start;
    }
  }

  @media (max-width: 768px) {
    .widgets-body {
      gap: 12px;
    }

    .widgets-subtitle {
      margin-top: 6px;
      font-size: 11px;
      line-height: 1.45;
    }

    .widget-table-wrap {
      overflow: hidden;
      border-radius: 12px;
    }

    .widget-table {
      min-width: 0;
    }

    .widget-table thead {
      display: none;
    }

    .widget-table,
    .widget-table tbody,
    .widget-table tr,
    .widget-table td {
      display: block;
      width: 100%;
    }

    .widget-table tr {
      border-bottom: 1px solid rgb(var(--clr) / 8%);
    }

    .widget-table tbody tr:last-child {
      border-bottom: none;
    }

    .widget-table td {
      padding: 12px;
      border-bottom: none;
    }

    .widget-table td + td {
      padding-top: 10px;
      border-top: 1px solid rgb(var(--clr) / 8%);
    }

    .widget-toggle-cell {
      text-align: left !important;
      white-space: normal;
    }

    .widget-toggle-field {
      display: flex;
      width: 100%;
    }

    .widget-toggle-field :global(.toggle-switch-container) {
      width: 100%;
      justify-content: space-between;
      gap: 12px;
    }

    .widget-toggle-field :global(.toggle-switch-container > span) {
      min-width: 0;
      white-space: normal;
    }

    .sessions-card .card-header {
      padding: 12px;
    }

    .sessions-table-container {
      overflow-x: hidden;
    }

    .sessions-table th:first-child,
    .sessions-table td:first-child {
      padding-left: 12px;
    }

    .sessions-table th:last-child,
    .sessions-table td:last-child {
      padding-right: 12px;
    }

    .actions-cell {
      width: 1%;
      white-space: nowrap;
    }

    .sessions-footer {
      padding: 10px 12px;
    }
  }

  @media (max-width: 480px) {
    .widgets-card {
      border-radius: 6px;
    }

    .widget-table td {
      padding: 10px 12px;
    }

    .widget-row-copy strong {
      font-size: 13px;
    }

    .widget-row-copy span {
      font-size: 11px;
      line-height: 1.45;
    }

    .sessions-card .card-header {
      padding: 10px 12px;
    }

    .sessions-table th:first-child,
    .sessions-table td:first-child,
    .sessions-table th:last-child,
    .sessions-table td:last-child {
      padding-left: 10px;
      padding-right: 10px;
    }

    .sessions-table td {
      padding-top: 12px;
      padding-bottom: 12px;
    }

    .device-info {
      gap: 8px;
    }

    .device-details {
      min-width: 0;
    }

    .device-browser,
    .device-os,
    .device-ip,
    .device-activity,
    .current-session-text {
      overflow-wrap: anywhere;
    }
  }
</style>
