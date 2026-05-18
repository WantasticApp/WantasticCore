<script lang="ts">
  import { onMount, tick } from "svelte";
  import { draggable } from "@neodrag/svelte";
  import { scale } from "svelte/transition";
  import Titlebar from "$components/shared/Titlebar.svelte";
  import {
    appZIndexes,
    bringToFront,
    minimizedApps,
    activeThing,
    openedApps,
  } from "$store/store";
  import { isMobile } from "$store/ui";
  import { copilotStore, type CopilotSession } from "$store/copilot";

  // Window chrome state — same pattern as the other apps (Peers, Account, …)
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
  let draft = "";
  let scrollEl: HTMLDivElement | null = null;

  const unsub = [
    copilotStore.sessions.subscribe((v) => (sessions = v)),
    copilotStore.activeSession.subscribe((v) => (active = v)),
    copilotStore.sending.subscribe((v) => (sending = v)),
    copilotStore.loading.subscribe((v) => (loading = v)),
    copilotStore.error.subscribe((v) => (error = v)),
  ];

  onMount(() => {
    init();
    return () => unsub.forEach((u) => u());
  });

  async function init() {
    try {
      const existing = await copilotStore.listSessions();
      if (existing.length === 0) {
        await copilotStore.openSession();
      } else {
        await copilotStore.loadSession(existing[0].session_id);
      }
    } catch (e) {
      // listSessions already records error on store
    }
  }

  async function newChat() {
    try {
      await copilotStore.openSession();
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
  }

  async function closeChat(s: CopilotSession, ev: MouseEvent) {
    ev.stopPropagation();
    if (!confirm("Close this chat? Its history will be lost.")) return;
    await copilotStore.closeSession(s.session_id);
  }

  async function send() {
    if (!active || !draft.trim() || sending) return;
    const text = draft.trim();
    draft = "";
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
<div class="copilot">
  <aside class="sidebar">
    <button class="new-chat" on:click={newChat} disabled={sending || loading}>+ New chat</button>
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
          <span class="session-id">
            {s.role === "admin" ? "🛡️ " : "💬 "}
            {s.session_id.slice(0, 8)}
          </span>
          <button class="close-x" on:click={(e) => closeChat(s, e)} aria-label="Close chat">×</button>
        </li>
      {/each}
    </ul>
  </aside>

  <main class="chat">
    {#if error}
      {#if error.startsWith("copilot_disabled:")}
        <div class="copilot-disabled-card">
          <h3>Copilot isn't configured yet</h3>
          <p>
            Copilot needs an Anthropic API key to answer. A super-admin can
            enable it from <strong>Admin → Settings</strong>, or by setting
            <code>adminbot.claude.api_key</code> in <code>config.yaml</code>
            and restarting.
          </p>
        </div>
      {:else}
        <div class="error">{error}</div>
      {/if}
    {/if}
    {#if !active}
      <div class="empty">
        <p>{loading ? "Loading…" : "Open a chat to start."}</p>
      </div>
    {:else}
      <div class="scroller" bind:this={scrollEl}>
        {#if (active.history ?? []).length === 0}
          <div class="hint">
            <p>
              Ask me to <em>list your devices</em>, <em>ping device X</em>, or — if you're an admin —
              <em>create a tenant</em>. I'll call the right tools and report back.
            </p>
          </div>
        {/if}
        {#each active.history ?? [] as turn, i (i)}
          <div class="turn turn-{turn.role}">
            <div class="bubble">
              <pre>{turn.content}</pre>
            </div>
          </div>
        {/each}
        {#if sending}
          <div class="turn turn-assistant">
            <div class="bubble pulse">…</div>
          </div>
        {/if}
      </div>

      <div class="composer">
        <textarea
          bind:value={draft}
          on:keydown={onKey}
          placeholder="Send a message — Enter to send, Shift+Enter for newline"
          rows="2"
          disabled={sending}
        />
        <button class="send" on:click={send} disabled={sending || !draft.trim()}>
          {sending ? "…" : "Send"}
        </button>
      </div>
    {/if}
  </main>
</div>
</div>

<style>
  /* Window chrome wrapper */
  .copilot-window {
    position: fixed;
    top: 60px;
    left: calc(50% - 480px);
    width: 960px;
    height: 640px;
    display: flex;
    flex-direction: column;
    background: rgb(20, 24, 32);
    border: 1px solid rgba(255, 255, 255, 0.08);
    border-radius: 8px;
    overflow: hidden;
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
    grid-template-columns: 240px 1fr;
    flex: 1;
    min-height: 0;
    color: rgb(var(--clr, 230 232 240));
    font-family:
      "Segoe UI Variable",
      "Segoe UI",
      Tahoma,
      Geneva,
      Verdana,
      sans-serif;
  }

  .sidebar {
    border-right: 1px solid rgba(255, 255, 255, 0.06);
    background: rgba(255, 255, 255, 0.02);
    padding: 12px 10px;
    display: flex;
    flex-direction: column;
    gap: 10px;
  }

  .sidebar header {
    display: flex;
    align-items: center;
    gap: 10px;
    padding: 0 4px;
  }

  .sidebar .title {
    font-weight: 600;
    font-size: 14px;
  }

  .back-link {
    color: inherit;
    text-decoration: none;
    opacity: 0.7;
    font-size: 13px;
  }

  .back-link:hover {
    opacity: 1;
    text-decoration: underline;
  }

  .new-chat {
    background: rgba(80, 160, 255, 0.18);
    border: 1px solid rgba(80, 160, 255, 0.4);
    color: rgb(200, 220, 255);
    padding: 6px 10px;
    border-radius: 4px;
    cursor: pointer;
    font-size: 13px;
  }

  .new-chat:disabled {
    opacity: 0.5;
    cursor: not-allowed;
  }

  .session-list {
    list-style: none;
    margin: 0;
    padding: 0;
    overflow-y: auto;
    flex: 1;
  }

  .session-item {
    display: flex;
    align-items: center;
    justify-content: space-between;
    padding: 8px 10px;
    border-radius: 4px;
    font-size: 13px;
    cursor: pointer;
    color: rgba(230, 232, 240, 0.85);
  }

  .session-item:hover {
    background: rgba(255, 255, 255, 0.04);
  }

  .session-item.active {
    background: rgba(80, 160, 255, 0.12);
    color: rgb(200, 220, 255);
  }

  .session-id {
    font-family: ui-monospace, SFMono-Regular, Menlo, Consolas, monospace;
  }

  .close-x {
    background: none;
    border: none;
    color: inherit;
    cursor: pointer;
    opacity: 0.5;
    font-size: 14px;
    padding: 0 4px;
  }

  .close-x:hover {
    opacity: 1;
  }

  .chat {
    display: grid;
    grid-template-rows: 1fr auto;
    height: 100dvh;
    overflow: hidden;
  }

  .scroller {
    overflow-y: auto;
    padding: 24px 32px;
  }

  .empty {
    display: grid;
    place-items: center;
    height: 100%;
    opacity: 0.6;
  }

  .hint {
    max-width: 540px;
    margin: 24px auto;
    padding: 12px 14px;
    border: 1px dashed rgba(255, 255, 255, 0.12);
    border-radius: 6px;
    font-size: 13px;
    opacity: 0.8;
    line-height: 1.5;
  }

  .turn {
    display: flex;
    margin: 10px 0;
  }

  .turn-user {
    justify-content: flex-end;
  }

  .turn-assistant {
    justify-content: flex-start;
  }

  .bubble {
    max-width: min(720px, 80%);
    padding: 10px 14px;
    border-radius: 10px;
    font-size: 14px;
    line-height: 1.5;
    white-space: pre-wrap;
    word-break: break-word;
  }

  .turn-user .bubble {
    background: rgba(80, 160, 255, 0.18);
    color: rgb(200, 220, 255);
  }

  .turn-assistant .bubble {
    background: rgba(255, 255, 255, 0.05);
    color: rgb(230, 232, 240);
  }

  .bubble pre {
    margin: 0;
    font-family: inherit;
    font-size: inherit;
    white-space: pre-wrap;
    word-break: break-word;
  }

  .pulse {
    animation: pulse 1.2s ease-in-out infinite;
  }

  @keyframes pulse {
    0%,
    100% {
      opacity: 0.4;
    }
    50% {
      opacity: 1;
    }
  }

  .composer {
    display: grid;
    grid-template-columns: 1fr auto;
    gap: 10px;
    padding: 12px 16px;
    border-top: 1px solid rgba(255, 255, 255, 0.06);
    background: rgba(0, 0, 0, 0.2);
  }

  .composer textarea {
    resize: none;
    background: rgba(255, 255, 255, 0.06);
    border: 1px solid rgba(255, 255, 255, 0.12);
    color: inherit;
    padding: 8px 10px;
    border-radius: 4px;
    font-size: 14px;
    font-family: inherit;
  }

  .send {
    background: rgba(80, 160, 255, 0.25);
    border: 1px solid rgba(80, 160, 255, 0.5);
    color: rgb(220, 235, 255);
    padding: 0 16px;
    border-radius: 4px;
    cursor: pointer;
    font-size: 13px;
  }

  .send:disabled {
    opacity: 0.5;
    cursor: not-allowed;
  }

  .error {
    margin: 12px 16px 0;
    padding: 8px 10px;
    border-radius: 4px;
    background: rgba(255, 80, 80, 0.12);
    color: rgb(255, 200, 200);
    font-size: 13px;
  }

  .copilot-disabled-card {
    margin: 24px 16px;
    padding: 20px 22px;
    border-radius: 10px;
    background: linear-gradient(135deg, rgba(124, 106, 247, 0.18), rgba(34, 211, 238, 0.10));
    border: 1px solid rgba(124, 106, 247, 0.35);
    color: rgb(220, 220, 240);
    font-size: 14px;
    line-height: 1.55;
  }
  .copilot-disabled-card h3 {
    margin: 0 0 8px;
    font-size: 16px;
    font-weight: 600;
    color: rgb(160, 200, 255);
  }
  .copilot-disabled-card code {
    background: rgba(0, 0, 0, 0.30);
    padding: 1px 6px;
    border-radius: 3px;
    font-size: 12px;
  }
</style>
