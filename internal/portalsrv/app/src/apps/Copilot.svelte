<script lang="ts">
  import { onMount, tick } from "svelte";
  import { draggable } from "@neodrag/svelte";
  import { scale, fly } from "svelte/transition";
  import Titlebar from "$components/shared/Titlebar.svelte";
  import {
    appZIndexes,
    bringToFront,
    minimizedApps,
    activeThing,
    openedApps,
  } from "$store/store";
  import { isMobile } from "$store/ui";
  import {
    copilotStore,
    type CopilotSession,
    type CopilotStatus,
  } from "$store/copilot";

  let isMaximized = false;
  let isMinimized = false;

  function handleReduce() {
    isMinimized = true;
    if (!$minimizedApps.includes("Copilot")) {
      $minimizedApps = [...$minimizedApps, "Copilot"];
    }
    if ($activeThing === "Copilot") $activeThing = "";
  }
  function handleMaximize() {
    isMaximized = !isMaximized;
  }
  function handleClose() {
    $openedApps = $openedApps.filter((a) => a !== "Copilot");
    $minimizedApps = $minimizedApps.filter((a) => a !== "Copilot");
    if ($activeThing === "Copilot") $activeThing = "";
  }

  $: if ($activeThing === "Copilot" && isMinimized) {
    isMinimized = false;
    $minimizedApps = $minimizedApps.filter((a) => a !== "Copilot");
  }

  $: zIndex = $appZIndexes["Copilot"] || 100;

  let sessions: CopilotSession[] = [];
  let active: CopilotSession | null = null;
  let sending = false;
  let loading = false;
  let error: string | null = null;
  let status: CopilotStatus | null = null;
  let statusLoaded = false; // Distinguishes "not loaded yet" from "loaded as null"
  let savingKey = false;
  let draft = "";
  let apiKeyInput = "";
  let showApiKey = false;
  let sidebarOpen = true;
  let showSettings = false;
  let scrollEl: HTMLDivElement | null = null;
  let textareaEl: HTMLTextAreaElement | null = null;

  // On mobile, sidebar starts collapsed so the chat area gets the full width.
  $: if ($isMobile && sidebarOpen === undefined) sidebarOpen = false;

  const unsub = [
    copilotStore.sessions.subscribe((v) => (sessions = v)),
    copilotStore.activeSession.subscribe((v) => (active = v)),
    copilotStore.sending.subscribe((v) => (sending = v)),
    copilotStore.loading.subscribe((v) => (loading = v)),
    copilotStore.error.subscribe((v) => (error = v)),
    copilotStore.status.subscribe((v) => (status = v)),
    copilotStore.savingKey.subscribe((v) => (savingKey = v)),
  ];

  onMount(() => {
    if ($isMobile) sidebarOpen = false;
    init();
    return () => unsub.forEach((u) => u());
  });

  async function init() {
    try {
      const s = await copilotStore.getStatus();
      statusLoaded = true;
      if (s.configured) {
        const existing = await copilotStore.listSessions();
        if (existing.length === 0) {
          await copilotStore.openSession();
        } else {
          await copilotStore.loadSession(existing[0].session_id);
        }
      }
    } catch {
      statusLoaded = true;
    }
  }

  async function saveApiKey() {
    const trimmed = apiKeyInput.trim();
    if (!trimmed) return;
    try {
      await copilotStore.setApiKey(trimmed);
      apiKeyInput = "";
      showSettings = false;
      const existing = await copilotStore.listSessions();
      if (existing.length === 0) {
        await copilotStore.openSession();
      } else {
        await copilotStore.loadSession(existing[0].session_id);
      }
    } catch {
      /* surfaced via error */
    }
  }

  async function newChat() {
    if (!status?.configured) return;
    try {
      await copilotStore.openSession();
      await tick();
      if ($isMobile) sidebarOpen = false;
      textareaEl?.focus();
    } catch {
      /* handled in store */
    }
  }

  async function pickChat(s: CopilotSession) {
    if (!s.history) {
      await copilotStore.loadSession(s.session_id);
    } else {
      copilotStore.selectSession(s.session_id);
    }
    if ($isMobile) sidebarOpen = false;
  }

  async function closeChat(s: CopilotSession, ev: MouseEvent) {
    ev.stopPropagation();
    if (!confirm("Delete this chat? Its history will be lost.")) return;
    await copilotStore.closeSession(s.session_id);
  }

  async function send() {
    if (!active || !draft.trim() || sending) return;
    const text = draft.trim();
    draft = "";
    autoSize();
    try {
      await copilotStore.sendMessage(active.session_id, text);
      await tick();
      scrollEl?.scrollTo({ top: scrollEl.scrollHeight, behavior: "smooth" });
    } catch {
      /* error already in store */
    }
  }

  function onKey(e: KeyboardEvent) {
    if (e.key === "Enter" && !e.shiftKey) {
      e.preventDefault();
      send();
    }
  }

  function autoSize() {
    if (!textareaEl) return;
    textareaEl.style.height = "auto";
    const next = Math.min(textareaEl.scrollHeight, 200);
    textareaEl.style.height = next + "px";
  }

  function relTime(iso: string): string {
    if (!iso) return "";
    const t = new Date(iso).getTime();
    const diff = Date.now() - t;
    if (diff < 60_000) return "just now";
    if (diff < 3600_000) return Math.floor(diff / 60_000) + "m ago";
    if (diff < 86400_000) return Math.floor(diff / 3600_000) + "h ago";
    return Math.floor(diff / 86400_000) + "d ago";
  }

  function preview(s: CopilotSession): string {
    const last = s.history?.[s.history.length - 1]?.content ?? "";
    return last.trim().slice(0, 64) || "New conversation";
  }

  function applyPrompt(text: string) {
    draft = text;
    tick().then(() => {
      autoSize();
      textareaEl?.focus();
    });
  }

  function openSettings() {
    showSettings = true;
    apiKeyInput = "";
  }

  const QUICK_PROMPTS = [
    { icon: "list",     title: "List my devices",     text: "List my devices and their online status." },
    { icon: "signal",   title: "Ping a device",        text: "Ping the device named " },
    { icon: "activity", title: "Show recent traffic",  text: "Show recent traffic for my busiest device over the last hour." },
    { icon: "wrench",   title: "Diagnose an issue",    text: "One of my devices keeps disconnecting. Walk me through diagnosing it." },
  ];

  // Compute which top-level screen renders. Centralizing this avoids
  // the "fell through every {:else if} and now we're staring at a
  // useless 'Open a chat to start' label" failure mode.
  $: screen = !statusLoaded
    ? "loading"
    : !status?.configured
      ? "configure"
      : showSettings && status?.can_configure
        ? "settings"
        : active
          ? "chat"
          : "empty";
</script>

<!-- svelte-ignore a11y-no-static-element-interactions -->
<div
  class="copilot-window activeShadow"
  class:maximized={isMaximized || $isMobile}
  class:minimized={isMinimized}
  style:z-index={zIndex}
  on:mousedown={() => bringToFront("Copilot")}
  on:touchstart={() => bringToFront("Copilot")}
  use:draggable={{
    handle: ".title-bar",
    disabled: isMaximized || $isMobile,
    bounds: "body",
  }}
  transition:scale={{ duration: 200 }}
>
  <Titlebar
    title="Copilot"
    appName="Copilot"
    canMaximize={!$isMobile}
    canReduce={true}
    canClose={true}
    on:reduce={handleReduce}
    on:maximize={handleMaximize}
    on:close={handleClose}
  />

  <div class="copilot" class:sidebar-hidden={!sidebarOpen} class:mobile={$isMobile}>
    {#if sidebarOpen}
      <aside
        class="sidebar"
        transition:fly|local={{ x: $isMobile ? -240 : 0, duration: 180 }}
      >
        <div class="sidebar-top">
          <button
            class="icon-btn"
            on:click={() => (sidebarOpen = false)}
            aria-label="Close sidebar"
            title="Close sidebar"
          >
            <svg width="18" height="18" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2">
              <rect x="3" y="3" width="18" height="18" rx="2" />
              <line x1="9" y1="3" x2="9" y2="21" />
            </svg>
          </button>
          <button
            class="new-chat"
            on:click={newChat}
            disabled={sending || loading || !status?.configured}
            title={status?.configured ? "New chat" : "Configure Copilot first"}
          >
            <svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2">
              <path d="M12 5v14M5 12h14" />
            </svg>
            <span>New chat</span>
          </button>
        </div>

        {#if sessions.length === 0}
          <div class="sidebar-empty">
            <p>No chats yet</p>
            <span>Click <strong>New chat</strong> above to start.</span>
          </div>
        {:else}
          <ul class="session-list">
            {#each sessions as s (s.session_id)}
              <li
                class="session-item"
                class:active={active?.session_id === s.session_id}
                on:click={() => pickChat(s)}
                on:keydown={(e) => e.key === "Enter" && pickChat(s)}
                role="button"
                tabindex="0"
              >
                <div class="session-icon" aria-hidden="true">
                  {#if s.role === "admin"}
                    <svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2">
                      <path d="M12 22s8-4 8-10V5l-8-3-8 3v7c0 6 8 10 8 10z" />
                    </svg>
                  {:else}
                    <svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2">
                      <path d="M21 15a2 2 0 01-2 2H7l-4 4V5a2 2 0 012-2h14a2 2 0 012 2z" />
                    </svg>
                  {/if}
                </div>
                <div class="session-info">
                  <span class="session-title">{preview(s)}</span>
                  <span class="session-meta">{relTime(s.last_active)}</span>
                </div>
                <button class="close-x" on:click={(e) => closeChat(s, e)} aria-label="Delete chat" title="Delete chat">
                  <svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2">
                    <polyline points="3 6 5 6 21 6" />
                    <path d="M19 6l-1 14a2 2 0 01-2 2H8a2 2 0 01-2-2L5 6m3 0V4a2 2 0 012-2h4a2 2 0 012 2v2" />
                  </svg>
                </button>
              </li>
            {/each}
          </ul>
        {/if}

        <div class="sidebar-footer">
          {#if status?.is_admin}
            <button
              class="settings-btn"
              on:click={openSettings}
              title="Copilot settings"
            >
              <svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2">
                <circle cx="12" cy="12" r="3" />
                <path d="M19.4 15a1.65 1.65 0 00.33 1.82l.06.06a2 2 0 01-2.83 2.83l-.06-.06a1.65 1.65 0 00-1.82-.33 1.65 1.65 0 00-1 1.51V21a2 2 0 01-4 0v-.09a1.65 1.65 0 00-1-1.51 1.65 1.65 0 00-1.82.33l-.06.06a2 2 0 01-2.83-2.83l.06-.06a1.65 1.65 0 00.33-1.82 1.65 1.65 0 00-1.51-1H3a2 2 0 010-4h.09a1.65 1.65 0 001.51-1 1.65 1.65 0 00-.33-1.82l-.06-.06a2 2 0 012.83-2.83l.06.06a1.65 1.65 0 001.82.33H9a1.65 1.65 0 001-1.51V3a2 2 0 014 0v.09a1.65 1.65 0 001 1.51 1.65 1.65 0 001.82-.33l.06-.06a2 2 0 012.83 2.83l-.06.06a1.65 1.65 0 00-.33 1.82V9a1.65 1.65 0 001.51 1H21a2 2 0 010 4h-.09a1.65 1.65 0 00-1.51 1z" />
              </svg>
              <span>Settings</span>
            </button>
          {/if}
        </div>
      </aside>

      {#if $isMobile}
        <!-- Mobile: tap-anywhere backdrop closes the slide-over sidebar. -->
        <button
          class="mobile-scrim"
          on:click={() => (sidebarOpen = false)}
          aria-label="Close sidebar"
        ></button>
      {/if}
    {/if}

    <main class="chat">
      <!-- Chat header — always visible. Houses the sidebar toggle (when closed)
           and the Settings cog. Admins reach the API-key form from here even
           when no chat is open and the sidebar is hidden. -->
      <header class="chat-header">
        <div class="chat-header-left">
          {#if !sidebarOpen}
            <button
              class="icon-btn"
              on:click={() => (sidebarOpen = true)}
              aria-label="Open sidebar"
              title="Open sidebar"
            >
              <svg width="18" height="18" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2">
                <rect x="3" y="3" width="18" height="18" rx="2" />
                <line x1="9" y1="3" x2="9" y2="21" />
              </svg>
            </button>
          {/if}
          <span class="chat-title">
            {#if screen === "configure"}
              Welcome
            {:else if screen === "settings"}
              Settings
            {:else if active}
              {preview(active)}
            {:else}
              Copilot
            {/if}
          </span>
        </div>
        <div class="chat-header-right">
          {#if status?.is_admin && status?.can_configure}
            <button
              class="icon-btn"
              on:click={openSettings}
              aria-label="Settings"
              title="Settings"
            >
              <svg width="18" height="18" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2">
                <circle cx="12" cy="12" r="3" />
                <path d="M19.4 15a1.65 1.65 0 00.33 1.82l.06.06a2 2 0 01-2.83 2.83l-.06-.06a1.65 1.65 0 00-1.82-.33 1.65 1.65 0 00-1 1.51V21a2 2 0 01-4 0v-.09a1.65 1.65 0 00-1-1.51 1.65 1.65 0 00-1.82.33l-.06.06a2 2 0 01-2.83-2.83l.06-.06a1.65 1.65 0 00.33-1.82 1.65 1.65 0 00-1.51-1H3a2 2 0 010-4h.09a1.65 1.65 0 001.51-1 1.65 1.65 0 00-.33-1.82l-.06-.06a2 2 0 012.83-2.83l.06.06a1.65 1.65 0 001.82.33H9a1.65 1.65 0 001-1.51V3a2 2 0 014 0v.09a1.65 1.65 0 001 1.51 1.65 1.65 0 001.82-.33l.06-.06a2 2 0 012.83 2.83l-.06.06a1.65 1.65 0 00-.33 1.82V9a1.65 1.65 0 001.51 1H21a2 2 0 010 4h-.09a1.65 1.65 0 00-1.51 1z" />
              </svg>
            </button>
          {/if}
        </div>
      </header>

      {#if screen === "loading"}
        <div class="full-state">
          <div class="loading-spinner" aria-hidden="true"></div>
          <p>Connecting to Copilot…</p>
        </div>
      {:else if screen === "configure"}
        <!-- Configure Copilot screen — replaces the chat when no API key is set. -->
        <div class="configure-screen">
          <div class="configure-card">
            <div class="configure-hero">
              <div class="hero-icon" aria-hidden="true">
                <svg width="36" height="36" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.7">
                  <path d="M9 11a4 4 0 118 0v3a4 4 0 11-8 0v-3z" />
                  <path d="M5 11a4 4 0 014-4" />
                  <path d="M3 19h6" />
                </svg>
              </div>
              <h2>Welcome to Copilot</h2>
              <p class="hero-sub">
                A Claude-powered assistant that knows your network. Ask it to
                list devices, ping peers, check traffic, or — if you're an admin —
                create tenants and tweak limits.
              </p>
            </div>

            {#if status?.can_configure}
              <div class="configure-form">
                <label for="api-key">
                  Anthropic API key
                  <span class="hint">Starts with <code>sk-ant-…</code></span>
                </label>
                <div class="key-input-row">
                  {#if showApiKey}
                    <input
                      id="api-key"
                      type="text"
                      bind:value={apiKeyInput}
                      placeholder="sk-ant-..."
                      autocomplete="off"
                      spellcheck="false"
                      on:keydown={(e) => e.key === "Enter" && saveApiKey()}
                      disabled={savingKey}
                    />
                  {:else}
                    <input
                      id="api-key"
                      type="password"
                      bind:value={apiKeyInput}
                      placeholder="sk-ant-..."
                      autocomplete="off"
                      spellcheck="false"
                      on:keydown={(e) => e.key === "Enter" && saveApiKey()}
                      disabled={savingKey}
                    />
                  {/if}
                  <button
                    type="button"
                    class="icon-btn toggle-visibility"
                    on:click={() => (showApiKey = !showApiKey)}
                    aria-label={showApiKey ? "Hide key" : "Show key"}
                    title={showApiKey ? "Hide key" : "Show key"}
                  >
                    {#if showApiKey}
                      <svg width="18" height="18" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2">
                        <path d="M1 12s4-8 11-8 11 8 11 8-4 8-11 8-11-8-11-8z" /><circle cx="12" cy="12" r="3" />
                        <line x1="1" y1="1" x2="23" y2="23" />
                      </svg>
                    {:else}
                      <svg width="18" height="18" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2">
                        <path d="M1 12s4-8 11-8 11 8 11 8-4 8-11 8-11-8-11-8z" /><circle cx="12" cy="12" r="3" />
                      </svg>
                    {/if}
                  </button>
                </div>
                <p class="help-text">
                  The key is saved server-side to <code>config.yaml</code> and used
                  to call Claude directly — your messages never leave your server
                  except to Anthropic's API. Get a key at
                  <a href="https://console.anthropic.com/settings/keys" target="_blank" rel="noreferrer">
                    console.anthropic.com
                  </a>.
                </p>
                {#if error}
                  <div class="inline-error">{error}</div>
                {/if}
                <button
                  class="primary-btn"
                  on:click={saveApiKey}
                  disabled={savingKey || !apiKeyInput.trim()}
                >
                  {#if savingKey}
                    Saving…
                  {:else}
                    Save &amp; enable Copilot
                  {/if}
                </button>
              </div>
            {:else}
              <div class="non-admin-notice">
                <p>
                  Copilot isn't configured yet. Ask a super-admin to add an
                  Anthropic API key from this screen.
                </p>
              </div>
            {/if}
          </div>
        </div>
      {:else if screen === "settings"}
        <div class="configure-screen">
          <div class="configure-card">
            <div class="configure-hero">
              <h2>Update API key</h2>
              <p class="hero-sub">
                Replace the saved Anthropic key. The new value is written to
                <code>config.yaml</code> and used immediately.
              </p>
            </div>
            <div class="configure-form">
              <label for="api-key-2">Anthropic API key</label>
              <div class="key-input-row">
                {#if showApiKey}
                  <input
                    id="api-key-2"
                    type="text"
                    bind:value={apiKeyInput}
                    placeholder="sk-ant-..."
                    autocomplete="off"
                    spellcheck="false"
                    on:keydown={(e) => e.key === "Enter" && saveApiKey()}
                    disabled={savingKey}
                  />
                {:else}
                  <input
                    id="api-key-2"
                    type="password"
                    bind:value={apiKeyInput}
                    placeholder="sk-ant-..."
                    autocomplete="off"
                    spellcheck="false"
                    on:keydown={(e) => e.key === "Enter" && saveApiKey()}
                    disabled={savingKey}
                  />
                {/if}
                <button
                  type="button"
                  class="icon-btn toggle-visibility"
                  on:click={() => (showApiKey = !showApiKey)}
                  aria-label={showApiKey ? "Hide key" : "Show key"}
                  title={showApiKey ? "Hide key" : "Show key"}
                >
                  {#if showApiKey}
                    <svg width="18" height="18" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2">
                      <path d="M1 12s4-8 11-8 11 8 11 8-4 8-11 8-11-8-11-8z" /><circle cx="12" cy="12" r="3" />
                      <line x1="1" y1="1" x2="23" y2="23" />
                    </svg>
                  {:else}
                    <svg width="18" height="18" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2">
                      <path d="M1 12s4-8 11-8 11 8 11 8-4 8-11 8-11-8-11-8z" /><circle cx="12" cy="12" r="3" />
                    </svg>
                  {/if}
                </button>
              </div>
              {#if error}
                <div class="inline-error">{error}</div>
              {/if}
              <div class="action-row">
                <button class="ghost-btn" on:click={() => (showSettings = false)}>
                  Cancel
                </button>
                <button
                  class="primary-btn"
                  on:click={saveApiKey}
                  disabled={savingKey || !apiKeyInput.trim()}
                >
                  {#if savingKey}Saving…{:else}Save{/if}
                </button>
              </div>
            </div>
          </div>
        </div>
      {:else if screen === "empty"}
        <div class="full-state">
          <div class="empty-icon" aria-hidden="true">
            <svg width="48" height="48" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.4">
              <path d="M9 11a4 4 0 118 0v3a4 4 0 11-8 0v-3z" />
              <path d="M5 11a4 4 0 014-4" />
              <path d="M3 19h6" />
            </svg>
          </div>
          <p class="empty-title">Ready when you are</p>
          <p class="empty-sub">{loading ? "Loading sessions…" : "Start a new conversation."}</p>
          <button
            class="primary-btn empty-cta"
            on:click={newChat}
            disabled={!status?.configured}
          >
            <svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.5">
              <path d="M12 5v14M5 12h14" />
            </svg>
            New chat
          </button>
          {#if error && !error.startsWith("copilot_disabled:")}
            <div class="error" style="margin-top: 16px; max-width: 420px;">{error}</div>
          {/if}
        </div>
      {:else if active}
        <div class="scroller" bind:this={scrollEl}>
          {#if (active.history ?? []).length === 0}
            <div class="welcome">
              <div class="welcome-title">
                <div class="welcome-mark" aria-hidden="true">
                  <svg width="44" height="44" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.5">
                    <path d="M9 11a4 4 0 118 0v3a4 4 0 11-8 0v-3z" />
                    <path d="M5 11a4 4 0 014-4" />
                    <path d="M3 19h6" />
                  </svg>
                </div>
                <h3>How can I help today?</h3>
              </div>
              <div class="quick-prompts">
                {#each QUICK_PROMPTS as p}
                  <button class="prompt-card" on:click={() => applyPrompt(p.text)}>
                    <span class="prompt-icon" aria-hidden="true">
                      {#if p.icon === "list"}
                        <svg width="18" height="18" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2">
                          <line x1="8" y1="6" x2="21" y2="6" />
                          <line x1="8" y1="12" x2="21" y2="12" />
                          <line x1="8" y1="18" x2="21" y2="18" />
                          <line x1="3" y1="6" x2="3.01" y2="6" />
                          <line x1="3" y1="12" x2="3.01" y2="12" />
                          <line x1="3" y1="18" x2="3.01" y2="18" />
                        </svg>
                      {:else if p.icon === "signal"}
                        <svg width="18" height="18" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2">
                          <path d="M2 12.5a16 16 0 0120 0" />
                          <path d="M5 16a11 11 0 0114 0" />
                          <path d="M8.5 19.5a6 6 0 017 0" />
                          <circle cx="12" cy="22" r="1" />
                        </svg>
                      {:else if p.icon === "activity"}
                        <svg width="18" height="18" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2">
                          <polyline points="22 12 18 12 15 21 9 3 6 12 2 12" />
                        </svg>
                      {:else if p.icon === "wrench"}
                        <svg width="18" height="18" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2">
                          <path d="M14.7 6.3a4 4 0 00-5.4 5.4l-7 7 2 2 7-7a4 4 0 005.4-5.4l-2.6 2.6-2-2 2.6-2.6z" />
                        </svg>
                      {/if}
                    </span>
                    <span class="prompt-title">{p.title}</span>
                  </button>
                {/each}
              </div>
            </div>
          {/if}

          {#each active.history ?? [] as turn, i (i)}
            <div class="turn turn-{turn.role}">
              <div class="avatar">
                {#if turn.role === "user"}
                  <div class="avatar-user">U</div>
                {:else}
                  <div class="avatar-assistant" aria-label="Copilot">
                    <svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2">
                      <path d="M9 11a4 4 0 118 0v3a4 4 0 11-8 0v-3z" />
                      <path d="M5 11a4 4 0 014-4" />
                      <path d="M3 19h6" />
                    </svg>
                  </div>
                {/if}
              </div>
              <div class="bubble">
                <div class="bubble-role">{turn.role === "user" ? "You" : "Copilot"}</div>
                <pre>{turn.content}</pre>
              </div>
            </div>
          {/each}

          {#if sending}
            <div class="turn turn-assistant">
              <div class="avatar">
                <div class="avatar-assistant" aria-hidden="true">
                  <svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2">
                    <path d="M9 11a4 4 0 118 0v3a4 4 0 11-8 0v-3z" />
                    <path d="M5 11a4 4 0 014-4" />
                    <path d="M3 19h6" />
                  </svg>
                </div>
              </div>
              <div class="bubble">
                <div class="bubble-role">Copilot</div>
                <div class="typing"><span></span><span></span><span></span></div>
              </div>
            </div>
          {/if}

          {#if error && !error.startsWith("copilot_disabled:")}
            <div class="error">{error}</div>
          {/if}
        </div>

        <div class="composer-wrap">
          <div class="composer">
            <textarea
              bind:this={textareaEl}
              bind:value={draft}
              on:keydown={onKey}
              on:input={autoSize}
              placeholder="Message Copilot…"
              rows="1"
              disabled={sending}
            />
            <button
              class="send"
              on:click={send}
              disabled={sending || !draft.trim()}
              aria-label="Send"
              title="Send (Enter)"
            >
              {#if sending}
                <svg width="18" height="18" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.5" class="spin">
                  <circle cx="12" cy="12" r="9" stroke-dasharray="42 14" />
                </svg>
              {:else}
                <svg width="18" height="18" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.5">
                  <path d="M12 19V5M5 12l7-7 7 7" />
                </svg>
              {/if}
            </button>
          </div>
          <div class="composer-hint">
            Enter to send · Shift+Enter for newline
          </div>
        </div>
      {/if}
    </main>
  </div>
</div>

<style>
  .copilot-window {
    position: fixed;
    top: 60px;
    left: calc(50% - 480px);
    width: 960px;
    height: 640px;
    display: flex;
    flex-direction: column;
    background: #0f1117;
    border: 1px solid rgba(255, 255, 255, 0.08);
    border-radius: 10px;
    overflow: hidden;
    box-shadow: 0 30px 80px rgba(0, 0, 0, 0.55);
  }
  .copilot-window.maximized {
    top: 0;
    left: 0;
    width: 100vw;
    height: calc(100vh - 48px);
    border-radius: 0;
  }
  .copilot-window.minimized {
    display: none;
  }

  .copilot {
    display: grid;
    grid-template-columns: 260px 1fr;
    flex: 1;
    min-height: 0;
    color: #e6e8f0;
    font-family:
      -apple-system, BlinkMacSystemFont, "Segoe UI Variable", "Segoe UI",
      Inter, Roboto, sans-serif;
    position: relative;
  }
  .copilot.sidebar-hidden {
    grid-template-columns: 1fr;
  }
  /* Mobile: sidebar floats above the chat as a slide-over so neither
     panel feels cramped on a narrow viewport. */
  .copilot.mobile {
    grid-template-columns: 1fr;
  }
  .copilot.mobile .sidebar {
    position: absolute;
    top: 0;
    left: 0;
    bottom: 0;
    width: min(280px, 80vw);
    z-index: 20;
    box-shadow: 4px 0 24px rgba(0, 0, 0, 0.5);
  }
  .mobile-scrim {
    position: absolute;
    inset: 0;
    background: rgba(0, 0, 0, 0.45);
    border: none;
    z-index: 15;
    cursor: pointer;
  }

  /* ───────── Sidebar ───────── */
  .sidebar {
    background: #0a0c12;
    border-right: 1px solid rgba(255, 255, 255, 0.06);
    display: flex;
    flex-direction: column;
    min-height: 0;
    min-width: 0;
  }
  .sidebar-top {
    display: flex;
    align-items: center;
    gap: 8px;
    padding: 10px;
    border-bottom: 1px solid rgba(255, 255, 255, 0.04);
  }
  .icon-btn {
    width: 32px;
    height: 32px;
    flex: 0 0 32px;
    display: grid;
    place-items: center;
    background: transparent;
    border: 1px solid transparent;
    border-radius: 6px;
    color: inherit;
    cursor: pointer;
    transition: background 0.15s, border-color 0.15s;
  }
  .icon-btn:hover {
    background: rgba(255, 255, 255, 0.06);
    border-color: rgba(255, 255, 255, 0.08);
  }
  .new-chat {
    flex: 1;
    min-width: 0;
    display: flex;
    align-items: center;
    justify-content: center;
    gap: 6px;
    background: linear-gradient(135deg, rgba(124, 106, 247, 0.22), rgba(80, 160, 255, 0.22));
    border: 1px solid rgba(124, 106, 247, 0.4);
    color: #dfe1ff;
    padding: 7px 12px;
    border-radius: 6px;
    cursor: pointer;
    font-size: 13px;
    font-weight: 500;
    white-space: nowrap;
    overflow: hidden;
    transition: filter 0.15s;
  }
  .new-chat:hover:not(:disabled) {
    filter: brightness(1.15);
  }
  .new-chat:disabled {
    opacity: 0.4;
    cursor: not-allowed;
  }

  .sidebar-empty {
    padding: 24px 16px;
    text-align: center;
    color: rgba(230, 232, 240, 0.55);
    font-size: 13px;
    line-height: 1.6;
    flex: 1;
    display: flex;
    flex-direction: column;
    justify-content: center;
  }
  .sidebar-empty p {
    margin: 0 0 4px;
    font-weight: 500;
    color: rgba(230, 232, 240, 0.85);
  }
  .sidebar-empty span {
    font-size: 12px;
  }

  .session-list {
    list-style: none;
    margin: 0;
    padding: 6px 8px;
    overflow-y: auto;
    flex: 1;
  }
  .session-item {
    display: flex;
    align-items: center;
    gap: 8px;
    padding: 8px 10px;
    border-radius: 6px;
    font-size: 13px;
    cursor: pointer;
    color: rgba(230, 232, 240, 0.85);
    transition: background 0.12s;
  }
  .session-item:hover {
    background: rgba(255, 255, 255, 0.04);
  }
  .session-item.active {
    background: rgba(124, 106, 247, 0.14);
    color: #e8e3ff;
  }
  .session-icon {
    width: 18px;
    height: 18px;
    display: grid;
    place-items: center;
    opacity: 0.7;
    flex: 0 0 18px;
  }
  .session-info {
    flex: 1;
    min-width: 0;
    display: flex;
    flex-direction: column;
    gap: 1px;
  }
  .session-title {
    white-space: nowrap;
    overflow: hidden;
    text-overflow: ellipsis;
    font-size: 13px;
  }
  .session-meta {
    font-size: 11px;
    opacity: 0.55;
  }
  .close-x {
    background: none;
    border: none;
    color: inherit;
    cursor: pointer;
    opacity: 0;
    padding: 2px;
    border-radius: 4px;
    display: grid;
    place-items: center;
  }
  .session-item:hover .close-x {
    opacity: 0.55;
  }
  .close-x:hover {
    opacity: 1;
    background: rgba(255, 80, 80, 0.18);
  }

  .sidebar-footer {
    padding: 8px 10px;
    border-top: 1px solid rgba(255, 255, 255, 0.04);
    min-height: 0;
  }
  .settings-btn {
    width: 100%;
    display: flex;
    align-items: center;
    gap: 8px;
    background: transparent;
    border: 1px solid rgba(255, 255, 255, 0.08);
    color: inherit;
    padding: 6px 10px;
    border-radius: 6px;
    font-size: 12px;
    cursor: pointer;
    transition: background 0.12s, border-color 0.12s;
  }
  .settings-btn:hover {
    background: rgba(255, 255, 255, 0.05);
    border-color: rgba(255, 255, 255, 0.15);
  }

  /* ───────── Chat header ───────── */
  .chat-header {
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: 8px;
    padding: 8px 12px;
    border-bottom: 1px solid rgba(255, 255, 255, 0.06);
    background: rgba(255, 255, 255, 0.015);
    min-height: 48px;
  }
  .chat-header-left,
  .chat-header-right {
    display: flex;
    align-items: center;
    gap: 8px;
    min-width: 0;
  }
  .chat-title {
    font-size: 13px;
    font-weight: 500;
    opacity: 0.85;
    white-space: nowrap;
    overflow: hidden;
    text-overflow: ellipsis;
  }

  /* ───────── Chat ───────── */
  .chat {
    display: grid;
    grid-template-rows: auto 1fr auto;
    min-height: 0;
    background: #0f1117;
    position: relative;
  }

  .scroller {
    overflow-y: auto;
    padding: 24px 32px 8px;
    scroll-behavior: smooth;
  }

  .full-state {
    display: flex;
    flex-direction: column;
    align-items: center;
    justify-content: center;
    gap: 8px;
    padding: 32px 24px;
    text-align: center;
    color: rgba(230, 232, 240, 0.7);
    grid-row: 2 / 3;
  }
  .empty-icon {
    color: #b39dff;
    margin-bottom: 8px;
  }
  .empty-title {
    margin: 0;
    font-size: 18px;
    font-weight: 600;
    color: #e6e8f0;
  }
  .empty-sub {
    margin: 0 0 12px;
    font-size: 13px;
    opacity: 0.7;
  }
  .empty-cta {
    display: inline-flex;
    align-items: center;
    gap: 6px;
    margin-top: 4px;
  }
  .loading-spinner {
    width: 32px;
    height: 32px;
    border-radius: 50%;
    border: 3px solid rgba(124, 106, 247, 0.15);
    border-top-color: #7c6af7;
    animation: spin 0.8s linear infinite;
    margin-bottom: 8px;
  }

  /* Welcome / quick prompts */
  .welcome {
    max-width: 680px;
    margin: 32px auto 24px;
    text-align: center;
  }
  .welcome-title {
    display: flex;
    flex-direction: column;
    align-items: center;
    gap: 12px;
    margin-bottom: 28px;
  }
  .welcome-mark {
    color: #b39dff;
    display: grid;
    place-items: center;
  }
  .welcome-title h3 {
    margin: 0;
    font-size: 22px;
    font-weight: 600;
    background: linear-gradient(135deg, #c9beff, #9ec5ff);
    -webkit-background-clip: text;
    background-clip: text;
    color: transparent;
  }
  .quick-prompts {
    display: grid;
    grid-template-columns: 1fr 1fr;
    gap: 10px;
  }
  .prompt-card {
    display: flex;
    align-items: center;
    gap: 10px;
    padding: 12px 14px;
    background: rgba(255, 255, 255, 0.03);
    border: 1px solid rgba(255, 255, 255, 0.08);
    border-radius: 8px;
    color: inherit;
    cursor: pointer;
    text-align: left;
    transition: background 0.12s, border-color 0.12s, transform 0.12s;
  }
  .prompt-card:hover {
    background: rgba(255, 255, 255, 0.06);
    border-color: rgba(124, 106, 247, 0.4);
    transform: translateY(-1px);
  }
  .prompt-icon {
    display: grid;
    place-items: center;
    color: #9ec5ff;
  }
  .prompt-title {
    font-size: 13px;
  }

  /* Turn / bubble */
  .turn {
    display: grid;
    grid-template-columns: 32px 1fr;
    gap: 12px;
    padding: 12px 0;
  }
  .turn-assistant {
    background: rgba(255, 255, 255, 0.015);
    border-radius: 8px;
    padding: 12px;
  }
  .avatar {
    display: grid;
    place-items: center;
  }
  .avatar-user,
  .avatar-assistant {
    width: 28px;
    height: 28px;
    border-radius: 6px;
    display: grid;
    place-items: center;
    font-size: 13px;
    font-weight: 600;
  }
  .avatar-user {
    background: linear-gradient(135deg, #4a8aff, #8aacff);
    color: #fff;
  }
  .avatar-assistant {
    background: linear-gradient(135deg, #7c6af7, #b39dff);
    color: #fff;
  }
  .bubble {
    min-width: 0;
    font-size: 14px;
    line-height: 1.55;
  }
  .bubble-role {
    font-size: 11px;
    opacity: 0.6;
    margin-bottom: 4px;
    font-weight: 500;
    letter-spacing: 0.02em;
  }
  .bubble pre {
    margin: 0;
    font-family: inherit;
    font-size: inherit;
    white-space: pre-wrap;
    word-break: break-word;
    color: inherit;
  }

  /* Typing indicator */
  .typing {
    display: inline-flex;
    gap: 4px;
    padding: 6px 0;
  }
  .typing span {
    width: 6px;
    height: 6px;
    border-radius: 50%;
    background: #b39dff;
    animation: blink 1.4s infinite;
  }
  .typing span:nth-child(2) { animation-delay: 0.2s; }
  .typing span:nth-child(3) { animation-delay: 0.4s; }
  @keyframes blink {
    0%, 60%, 100% { opacity: 0.25; transform: translateY(0); }
    30% { opacity: 1; transform: translateY(-2px); }
  }

  /* Composer */
  .composer-wrap {
    padding: 12px 24px 16px;
    background: linear-gradient(180deg, rgba(15, 17, 23, 0), #0f1117 30%);
  }
  .composer {
    display: grid;
    grid-template-columns: 1fr auto;
    gap: 8px;
    padding: 8px 8px 8px 14px;
    background: #181b24;
    border: 1px solid rgba(255, 255, 255, 0.1);
    border-radius: 14px;
    transition: border-color 0.15s, box-shadow 0.15s;
  }
  .composer:focus-within {
    border-color: rgba(124, 106, 247, 0.5);
    box-shadow: 0 0 0 3px rgba(124, 106, 247, 0.12);
  }
  .composer textarea {
    resize: none;
    background: transparent;
    border: none;
    color: inherit;
    padding: 9px 0;
    font-size: 14px;
    font-family: inherit;
    line-height: 1.5;
    outline: none;
    max-height: 200px;
  }
  .send {
    align-self: end;
    width: 36px;
    height: 36px;
    display: grid;
    place-items: center;
    background: linear-gradient(135deg, #7c6af7, #5a83ff);
    border: none;
    color: #fff;
    border-radius: 10px;
    cursor: pointer;
    transition: filter 0.12s;
  }
  .send:hover:not(:disabled) {
    filter: brightness(1.15);
  }
  .send:disabled {
    opacity: 0.4;
    cursor: not-allowed;
  }
  .spin {
    animation: spin 0.9s linear infinite;
  }
  @keyframes spin {
    to { transform: rotate(360deg); }
  }
  .composer-hint {
    text-align: center;
    font-size: 11px;
    opacity: 0.45;
    margin-top: 8px;
  }

  .error {
    margin: 12px 0;
    padding: 10px 12px;
    border-radius: 6px;
    background: rgba(255, 80, 80, 0.12);
    border: 1px solid rgba(255, 80, 80, 0.25);
    color: #ffb8b8;
    font-size: 13px;
  }

  /* ───────── Configure screen ───────── */
  .configure-screen {
    display: grid;
    place-items: center;
    overflow-y: auto;
    padding: 32px 24px;
    grid-row: 2 / 3;
  }
  .configure-card {
    width: 100%;
    max-width: 480px;
    background: linear-gradient(180deg, rgba(124, 106, 247, 0.08), rgba(80, 160, 255, 0.04));
    border: 1px solid rgba(124, 106, 247, 0.25);
    border-radius: 14px;
    padding: 28px 28px 24px;
  }
  .configure-hero {
    text-align: center;
    margin-bottom: 24px;
  }
  .hero-icon {
    color: #b39dff;
    display: grid;
    place-items: center;
    margin: 0 auto 12px;
  }
  .configure-hero h2 {
    margin: 0 0 8px;
    font-size: 22px;
    font-weight: 600;
    background: linear-gradient(135deg, #c9beff, #9ec5ff);
    -webkit-background-clip: text;
    background-clip: text;
    color: transparent;
  }
  .hero-sub {
    margin: 0;
    font-size: 13px;
    line-height: 1.55;
    opacity: 0.75;
  }
  .configure-form {
    display: flex;
    flex-direction: column;
    gap: 8px;
  }
  .configure-form label {
    font-size: 13px;
    font-weight: 500;
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: 10px;
  }
  .configure-form .hint {
    font-size: 11px;
    opacity: 0.55;
    font-weight: 400;
  }
  .configure-form code {
    background: rgba(0, 0, 0, 0.3);
    padding: 1px 5px;
    border-radius: 3px;
    font-size: 11px;
  }
  .key-input-row {
    display: grid;
    grid-template-columns: 1fr auto;
    gap: 6px;
    background: #0a0c12;
    border: 1px solid rgba(255, 255, 255, 0.12);
    border-radius: 8px;
    padding: 2px 2px 2px 0;
    transition: border-color 0.15s, box-shadow 0.15s;
  }
  .key-input-row:focus-within {
    border-color: rgba(124, 106, 247, 0.5);
    box-shadow: 0 0 0 3px rgba(124, 106, 247, 0.12);
  }
  .key-input-row input {
    background: transparent;
    border: none;
    color: inherit;
    padding: 10px 12px;
    font-size: 13px;
    font-family: ui-monospace, SFMono-Regular, Menlo, Consolas, monospace;
    outline: none;
  }
  .toggle-visibility {
    width: 32px;
    height: 32px;
    align-self: center;
    opacity: 0.6;
  }
  .toggle-visibility:hover {
    opacity: 1;
  }
  .help-text {
    margin: 4px 0 12px;
    font-size: 12px;
    line-height: 1.55;
    opacity: 0.7;
  }
  .help-text a {
    color: #b39dff;
    text-decoration: none;
  }
  .help-text a:hover {
    text-decoration: underline;
  }
  .inline-error {
    padding: 8px 10px;
    border-radius: 6px;
    background: rgba(255, 80, 80, 0.12);
    border: 1px solid rgba(255, 80, 80, 0.25);
    color: #ffb8b8;
    font-size: 12px;
    margin-bottom: 4px;
  }
  .action-row {
    display: flex;
    gap: 8px;
    justify-content: flex-end;
  }
  .primary-btn {
    margin-top: 4px;
    padding: 10px 16px;
    background: linear-gradient(135deg, #7c6af7, #5a83ff);
    border: none;
    color: #fff;
    border-radius: 8px;
    font-size: 13px;
    font-weight: 600;
    cursor: pointer;
    transition: filter 0.12s;
  }
  .primary-btn:hover:not(:disabled) {
    filter: brightness(1.15);
  }
  .primary-btn:disabled {
    opacity: 0.5;
    cursor: not-allowed;
  }
  .ghost-btn {
    margin-top: 4px;
    padding: 10px 16px;
    background: transparent;
    border: 1px solid rgba(255, 255, 255, 0.12);
    color: inherit;
    border-radius: 8px;
    font-size: 13px;
    cursor: pointer;
    transition: background 0.12s;
  }
  .ghost-btn:hover {
    background: rgba(255, 255, 255, 0.05);
  }
  .non-admin-notice {
    padding: 14px 16px;
    background: rgba(255, 255, 255, 0.03);
    border: 1px solid rgba(255, 255, 255, 0.08);
    border-radius: 8px;
    font-size: 13px;
    opacity: 0.8;
    line-height: 1.55;
  }
  .non-admin-notice p {
    margin: 0;
  }

  /* ───────── Mobile overrides ───────── */
  @media (max-width: 720px) {
    .composer-wrap {
      padding: 8px 12px 12px;
    }
    .scroller {
      padding: 16px 14px 8px;
    }
    .quick-prompts {
      grid-template-columns: 1fr;
    }
    .welcome {
      margin: 16px auto;
    }
    .welcome-title h3 {
      font-size: 18px;
    }
    .configure-card {
      padding: 20px 18px 18px;
    }
  }
</style>
