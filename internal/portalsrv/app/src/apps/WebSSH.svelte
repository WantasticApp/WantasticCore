<script lang="ts">
  import { draggable } from "@neodrag/svelte";
  import { scale } from "svelte/transition";
  import { onMount } from "svelte";
  import { websshStore, type WebSSHSession } from "$store/webssh";
  import { peerStore } from "$store/peer";
  import {
    openedApps,
    activeThing,
    minimizedApps,
    appZIndexes,
    bringToFront,
  } from "$store/store";

  import Titlebar from "$components/shared/Titlebar.svelte";
  import Icon from "$components/Icon.svelte";
  import { translateError$, _ } from "$store/i18n";
  import { formatLocalDate } from "$lib/dateUtils";
  import {
    createPeerSharedLookup,
    resolveSharedMeta,
  } from "$lib/sharedAccess";

  // Window state
  let isMaximized = false;
  let isMinimized = false;

  // Z-index for window stacking
  $: zIndex = $appZIndexes["WebSSH"] || 100;

  // Bring to front when activated
  $: if ($activeThing === "WebSSH") {
    bringToFront("WebSSH");
  }

  function handleFocus() {
    $activeThing = "WebSSH";
    bringToFront("WebSSH");
  }

  // UI state
  let searchQuery = "";
  let isRefreshing = false;

  // Sessions state
  let sessions: WebSSHSession[] = [];
  let expandedSessionId: string | null = null;

  // Subscribe to stores
  $: sessions = $websshStore.sessions;
  $: peers = $peerStore.peers;
  $: peerSharedLookup = createPeerSharedLookup(peers);

  // Filtered sessions based on search
  $: filteredSessions = sessions.filter((session) => {
    if (!searchQuery) return true;
    const query = searchQuery.toLowerCase();
    const peer = peers.find((p) => p.id === session.peer_id);
    return (
      session.hostname?.toLowerCase().includes(query) ||
      session.username?.toLowerCase().includes(query) ||
      peer?.name?.toLowerCase().includes(query) ||
      session.status?.toLowerCase().includes(query)
    );
  });

  // Grouping logic
  let groupByDevice = localStorage.getItem("webssh_groupByDevice") === "true";
  $: {
    localStorage.setItem("webssh_groupByDevice", String(groupByDevice));
  }

  // Group sessions by peer
  function groupSessions(list: WebSSHSession[]) {
    const groups: Record<string, WebSSHSession[]> = {};
    list.forEach((sess) => {
      const peerName = getPeerName(sess.peer_ip);
      if (!groups[peerName]) groups[peerName] = [];
      groups[peerName].push(sess);
    });
    return groups;
  }

  $: sessionGroups = groupByDevice
    ? groupSessions(filteredSessions)
    : { [$_("webssh.allSessions")]: filteredSessions };
  $: sortedGroupNames = Object.keys(sessionGroups).sort();

  // Watch minimizedApps to sync local state
  // $: isMinimized = $minimizedApps.includes("WebSSH");

  // Watch activeThing to restore when clicked from taskbar
  $: if ($activeThing === "WebSSH" && isMinimized) {
    $minimizedApps = $minimizedApps.filter((a) => a !== "WebSSH");
  }

  function handleMaximize() {
    isMaximized = !isMaximized;
  }

  function handleReduce() {
    if (!$minimizedApps.includes("WebSSH")) {
      $minimizedApps = [...$minimizedApps, "WebSSH"];
    }
    $activeThing = ""; // Clear active so taskbar can restore it
  }

  function handleCreateSession() {
    // Open NewSSHSession as a separate app window
    if (!$openedApps.includes("NewSSHSession")) {
      $openedApps = [...$openedApps, "NewSSHSession"];
    }
    $activeThing = "NewSSHSession";
    bringToFront("NewSSHSession");
  }

  async function handleRefresh() {
    isRefreshing = true;
    try {
      await websshStore.listActiveSessions();
    } catch (e) {
      console.error("Failed to refresh sessions:", e);
    }
    setTimeout(() => {
      isRefreshing = false;
    }, 500);
  }

  let lastToggleTime = 0;
  function toggleExpandSession(sessionId: string) {
    const now = Date.now();
    if (now - lastToggleTime < 300) return;
    lastToggleTime = now;

    expandedSessionId = expandedSessionId === sessionId ? null : sessionId;
  }

  // Open SSHTerminal as a separate window via store
  function openTerminal(session: WebSSHSession) {
    const resolvedSession = websshStore.openTerminal(session);
    // Focus the terminal window
    const terminalWindowId = `SSHTerminal-${resolvedSession.id}`;
    $activeThing = terminalWindowId;
    bringToFront(terminalWindowId);
  }

  // Open SSH activity viewer for specific session
  function openSessionActivity(session: WebSSHSession) {
    // Store the session ID and peer ID for the activity viewer to filter
    if (typeof window !== "undefined") {
      (window as any).__sshActivityFilter = session.id;
      (window as any).__sshActivityPeerId = session.peer_id;
    }
    if (!$openedApps.includes("SSHActivityViewer")) {
      $openedApps = [...$openedApps, "SSHActivityViewer"];
    }
    $activeThing = "SSHActivityViewer";
    bringToFront("SSHActivityViewer");
  }

  // Open SSH activity viewer (all activities)
  function openActivityViewer() {
    if (!$openedApps.includes("SSHActivityViewer")) {
      $openedApps = [...$openedApps, "SSHActivityViewer"];
    }
    $activeThing = "SSHActivityViewer";
    bringToFront("SSHActivityViewer");
  }

  // Disconnect SSH session (keeps it in the list)
  let disconnectingSessionId: string | null = null;
  async function handleDisconnectSession(session: WebSSHSession) {
    if (disconnectingSessionId) return;
    disconnectingSessionId = session.id;
    try {
      await websshStore.disconnectSession(session.id);
      await websshStore.listActiveSessions();
    } catch (err) {
      console.error("Failed to disconnect session:", err);
    } finally {
      disconnectingSessionId = null;
    }
  }

  // Delete SSH session (removes it from the list permanently)
  let deletingSessionId: string | null = null;
  async function handleDeleteSession(session: WebSSHSession) {
    if (deletingSessionId) return;
    deletingSessionId = session.id;
    try {
      await websshStore.deleteSession(session.id);
    } catch (err) {
      console.error("Failed to delete session:", err);
    } finally {
      deletingSessionId = null;
    }
  }

  function getPeerName(peerIp: string): string {
    const cleanSessIp = peerIp?.replace("/32", "");
    const peer = peers.find(
      (p) => (p.assigned_ip || "").replace("/32", "") === cleanSessIp,
    );
    return peer?.name || $_("webssh.unknownPeer");
  }

  function getSessionSharedMeta(session: WebSSHSession) {
    return resolveSharedMeta(session, peerSharedLookup);
  }

  function formatDuration(ms: number): string {
    const seconds = Math.floor(ms / 1000);
    if (seconds < 60) return seconds + "s";
    if (seconds < 3600)
      return Math.floor(seconds / 60) + "m " + (seconds % 60) + "s";
    const hours = Math.floor(seconds / 3600);
    const mins = Math.floor((seconds % 3600) / 60);
    return hours + "h " + mins + "m";
  }

  function formatBytes(bytes: number): string {
    if (bytes < 1024) return bytes + " B";
    if (bytes < 1048576) return (bytes / 1024).toFixed(1) + " KB";
    return (bytes / 1048576).toFixed(1) + " MB";
  }

  function formatDate(dateStr: string): string {
    const d = new Date(dateStr);
    return d.toLocaleString();
  }
  function getStatusColor(status: string): string {
    switch (status) {
      case "connected":
        return "var(--clrGrn)";
      case "connecting":
        return "var(--clrYlw)";
      case "disconnected":
        return "var(--clrRed)";
      default:
        return "var(--fg2)";
    }
  }

  onMount(async () => {
    try {
      console.log("WebSSH Component Mount, fetching sessions...");
      await Promise.all([
        websshStore.listActiveSessions(),
        peerStore.listPeers(),
      ]);
      console.log("WebSSH Mounted. Sessions count:", sessions.length);
    } catch (err: any) {
      console.error("Failed to load sessions:", err);
    }
  });
</script>

<div
  class="webssh activeShadow"
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
    appName="WebSSH"
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
        <linearGradient id="sshGrad" x1="0%" y1="0%" x2="100%" y2="100%">
          <stop offset="0%" style="stop-color:#10b981;stop-opacity:1" />
          <stop offset="100%" style="stop-color:#059669;stop-opacity:1" />
        </linearGradient>
      </defs>
      <rect
        x="15"
        y="15"
        width="70"
        height="70"
        rx="8"
        fill="#1f2937"
        stroke="url(#sshGrad)"
        stroke-width="3"
      />
      <rect x="15" y="15" width="70" height="15" rx="8" fill="url(#sshGrad)" />
      <circle cx="23" cy="22.5" r="3" fill="#ef4444" />
      <circle cx="33" cy="22.5" r="3" fill="#fbbf24" />
      <circle cx="43" cy="22.5" r="3" fill="#10b981" />
      <text
        x="50"
        y="58"
        font-family="monospace"
        font-size="24"
        fill="url(#sshGrad)"
        text-anchor="middle">$_</text
      >
    </svg>
    <span class="appName pl-2">{$_("webssh.webSSHSessions")}</span>
  </Titlebar>

  <div class="mainApp">
    <div class="content">
      <!-- Toolbar with Search and Actions -->
      <div class="toolbar">
        <div class="search-container">
          <svg
            class="search-icon"
            width="16"
            height="16"
            viewBox="0 0 24 24"
            fill="none"
            xmlns="http://www.w3.org/2000/svg"
          >
            <circle
              cx="11"
              cy="11"
              r="7"
              stroke="currentColor"
              stroke-width="2"
            />
            <path
              d="M16 16L21 21"
              stroke="currentColor"
              stroke-width="2"
              stroke-linecap="round"
            />
          </svg>
          <input
            type="text"
            bind:value={searchQuery}
            placeholder="Search by host, user, peer..."
            class="search-input"
          />
        </div>
        <div class="toolbar-actions">
          <button
            class="icon-btn"
            on:click={openActivityViewer}
            title={$_("webssh.viewActivities")}
          >
            <svg
              width="20"
              height="20"
              viewBox="0 0 24 24"
              fill="none"
              xmlns="http://www.w3.org/2000/svg"
            >
              <path
                d="M14 2H6a2 2 0 0 0-2 2v16a2 2 0 0 0 2 2h12a2 2 0 0 0 2-2V8z"
                stroke="currentColor"
                stroke-width="2"
              />
              <polyline
                points="14 2 14 8 20 8"
                stroke="currentColor"
                stroke-width="2"
              />
              <line
                x1="16"
                y1="13"
                x2="8"
                y2="13"
                stroke="currentColor"
                stroke-width="2"
              />
              <line
                x1="16"
                y1="17"
                x2="8"
                y2="17"
                stroke="currentColor"
                stroke-width="2"
              />
            </svg>
          </button>
          <button
            class="icon-btn"
            on:click={handleCreateSession}
            title={$_("webssh.createSession")}
          >
            <svg
              width="20"
              height="20"
              viewBox="0 0 24 24"
              fill="none"
              xmlns="http://www.w3.org/2000/svg"
            >
              <path
                d="M12 5V19M5 12H19"
                stroke="currentColor"
                stroke-width="2"
                stroke-linecap="round"
              />
            </svg>
          </button>
          <button
            class="icon-btn"
            class:active={groupByDevice}
            on:click={() => (groupByDevice = !groupByDevice)}
            title={$_("webssh.groupByDevice")}
          >
            <svg
              width="20"
              height="20"
              viewBox="0 0 24 24"
              fill="none"
              xmlns="http://www.w3.org/2000/svg"
            >
              <path
                d="M3 7H21M3 12H21M3 17H21"
                stroke="currentColor"
                stroke-width="2"
                stroke-linecap="round"
              />
            </svg>
          </button>
          <button
            class="icon-btn"
            class:spinning={isRefreshing}
            on:click={handleRefresh}
            title={$_("common.refresh")}
          >
            <svg
              width="20"
              height="20"
              viewBox="0 0 24 24"
              fill="none"
              xmlns="http://www.w3.org/2000/svg"
            >
              <path
                d="M21 12C21 16.9706 16.9706 21 12 21C7.02944 21 3 16.9706 3 12C3 7.02944 7.02944 3 12 3C14.8273 3 17.35 4.33027 19 6.375"
                stroke="currentColor"
                stroke-width="2"
                stroke-linecap="round"
              />
              <path
                d="M15 3H21V9"
                stroke="currentColor"
                stroke-width="2"
                stroke-linecap="round"
                stroke-linejoin="round"
              />
            </svg>
          </button>
        </div>
      </div>

      {#if $websshStore.isLoading}
        <div class="loading-state">
          <div class="spinner" />
          <p>{$_("webssh.loadingSessions")}</p>
        </div>
      {:else if filteredSessions.length === 0 && searchQuery}
        <div class="empty-state">
          <div class="empty-icon"><Icon name="search" /></div>
          <p>
            {$_("webssh.noMatchingSessions", {
              values: { query: searchQuery },
            })}
          </p>
        </div>
      {:else if sessions.length === 0}
        <div class="empty-state">
          <div class="empty-icon"><Icon name="terminal" /></div>
          <h3>{$_("webssh.noSessions")}</h3>
          <p>{$_("webssh.noSessionsMessage")}</p>
          <button class="primary-btn" on:click={handleCreateSession}>
            {$_("webssh.createSession")}
          </button>
        </div>
      {:else}
        <div class="sessions-count">
          {filteredSessions.length} session{filteredSessions.length !== 1
            ? "s"
            : ""}
        </div>

        <table class="sessions-table">
          <thead>
            <tr>
              <th class="col-expand" />
              <th class="col-name">{$_("webssh.host")}</th>
              <th class="col-user">{$_("webssh.username")}</th>
              <th class="col-peer">{$_("webssh.peer")}</th>
              <th class="col-port">{$_("webssh.port")}</th>
              <th class="col-status">{$_("common.status")}</th>
              <th class="col-actions">{$_("common.actions")}</th>
            </tr>
          </thead>
          <tbody>
            {#each sortedGroupNames as groupName}
              {#if groupByDevice}
                <tr class="group-header">
                  <td colspan="7">
                    <div class="group-title-tag">
                      <span class="tag-icon">#</span>
                      <span class="tag-name">{groupName}</span>
                      <span class="tag-count"
                        >{sessionGroups[groupName].length}</span
                      >
                    </div>
                  </td>
                </tr>
              {/if}
              {#each sessionGroups[groupName] as session (session.id)}
                {@const sharedMeta = getSessionSharedMeta(session)}
                <tr
                  class="session-row hover:bg-gray-200 cursor-pointer"
                  on:click={() => toggleExpandSession(session.id)}
                  class:expanded={expandedSessionId === session.id}
                >
                  <td class="col-expand">
                    <button
                      class="expand-btn"
                      class:expanded={expandedSessionId === session.id}
                      on:click={() => toggleExpandSession(session.id)}
                      title={expandedSessionId === session.id
                        ? "Collapse"
                        : "Expand"}
                    >
                      <svg
                        width="12"
                        height="12"
                        viewBox="0 0 24 24"
                        fill="none"
                        xmlns="http://www.w3.org/2000/svg"
                      >
                        <path
                          d="M6 9L12 15L18 9"
                          stroke="currentColor"
                          stroke-width="2"
                          stroke-linecap="round"
                          stroke-linejoin="round"
                        />
                      </svg>
                    </button>
                  </td>
                  <td class="col-name">
                    <div class="name-cell">
                      <span
                        class="status-dot"
                        class:active={session.status === "connected"}
                        class:connecting={session.status === "connecting"}
                      />
                      <span class="session-name"
                        >{session.peer_ip ||
                          session.hostname ||
                          $_("webssh.unknown")}</span
                      >
                    </div>
                  </td>
                  <td class="col-user">
                    <span class="username">{session.username}</span>
                  </td>
                  <td class="col-peer">
                    <span class="peer-name" title={session.peer_id}
                      >{getPeerName(session.peer_ip)}</span
                    >
                    {#if sharedMeta.isShared}
                      <span
                        class="shared-badge"
                        title="Shared by {sharedMeta.ownerName ||
                          'another account'}"
                        >shared{sharedMeta.ownerName
                          ? ` · ${sharedMeta.ownerName}`
                          : ""}</span
                      >
                    {/if}
                  </td>
                  <td class="col-port">
                    <code class="port-code">{session.ssh_port || 22}</code>
                  </td>
                  <td class="col-status">
                    <span
                      class="status-badge"
                      class:active={session.status === "connected"}
                      class:connecting={session.status === "connecting"}
                      class:disconnected={session.status === "disconnected"}
                    >
                      {session.status || $_("webssh.statusIdle")}
                    </span>
                  </td>
                  <td class="col-actions">
                    <div class="action-buttons">
                      <button
                        class="action-btn success"
                        on:click|stopPropagation={() => openTerminal(session)}
                        title={$_("webssh.openTerminal")}
                      >
                        <svg
                          width="16"
                          height="16"
                          viewBox="0 0 24 24"
                          fill="none"
                          xmlns="http://www.w3.org/2000/svg"
                        >
                          <polyline
                            points="4 17 10 11 4 5"
                            stroke="currentColor"
                            stroke-width="2"
                            stroke-linecap="round"
                            stroke-linejoin="round"
                          />
                          <line
                            x1="12"
                            y1="19"
                            x2="20"
                            y2="19"
                            stroke="currentColor"
                            stroke-width="2"
                            stroke-linecap="round"
                          />
                        </svg>
                      </button>
                      <button
                        class="action-btn"
                        on:click|stopPropagation={() =>
                          openSessionActivity(session)}
                        title={$_("webssh.viewActivity")}
                      >
                        <svg
                          width="16"
                          height="16"
                          viewBox="0 0 24 24"
                          fill="none"
                          xmlns="http://www.w3.org/2000/svg"
                        >
                          <path
                            d="M14 2H6a2 2 0 0 0-2 2v16a2 2 0 0 0 2 2h12a2 2 0 0 0 2-2V8z"
                            stroke="currentColor"
                            stroke-width="2"
                          />
                          <polyline
                            points="14 2 14 8 20 8"
                            stroke="currentColor"
                            stroke-width="2"
                          />
                          <line
                            x1="16"
                            y1="13"
                            x2="8"
                            y2="13"
                            stroke="currentColor"
                            stroke-width="2"
                          />
                          <line
                            x1="16"
                            y1="17"
                            x2="8"
                            y2="17"
                            stroke="currentColor"
                            stroke-width="2"
                          />
                        </svg>
                      </button>
                      <button
                        class="action-btn danger"
                        on:click|stopPropagation={() =>
                          handleDisconnectSession(session)}
                        title={$_("webssh.disconnect")}
                        disabled={disconnectingSessionId === session.id}
                      >
                        {#if disconnectingSessionId === session.id}
                          <svg
                            class="spinning"
                            width="16"
                            height="16"
                            viewBox="0 0 24 24"
                            fill="none"
                            xmlns="http://www.w3.org/2000/svg"
                          >
                            <circle
                              cx="12"
                              cy="12"
                              r="10"
                              stroke="currentColor"
                              stroke-width="2"
                              stroke-dasharray="31.4 31.4"
                              stroke-linecap="round"
                            />
                          </svg>
                        {:else}
                          <svg
                            width="16"
                            height="16"
                            viewBox="0 0 24 24"
                            fill="none"
                            xmlns="http://www.w3.org/2000/svg"
                          >
                            <path
                              d="M18 6L6 18M6 6l12 12"
                              stroke="currentColor"
                              stroke-width="2"
                              stroke-linecap="round"
                              stroke-linejoin="round"
                            />
                          </svg>
                        {/if}
                      </button>
                      <button
                        class="action-btn delete"
                        on:click|stopPropagation={() =>
                          handleDeleteSession(session)}
                        title="Delete session"
                        disabled={deletingSessionId === session.id}
                      >
                        {#if deletingSessionId === session.id}
                          <svg
                            class="spinning"
                            width="16"
                            height="16"
                            viewBox="0 0 24 24"
                            fill="none"
                            xmlns="http://www.w3.org/2000/svg"
                          >
                            <circle
                              cx="12"
                              cy="12"
                              r="10"
                              stroke="currentColor"
                              stroke-width="2"
                              stroke-dasharray="31.4 31.4"
                              stroke-linecap="round"
                            />
                          </svg>
                        {:else}
                          <svg
                            width="16"
                            height="16"
                            viewBox="0 0 24 24"
                            fill="none"
                            xmlns="http://www.w3.org/2000/svg"
                          >
                            <path
                              d="M3 6h18M8 6V4h8v2M19 6l-1 14H6L5 6"
                              stroke="currentColor"
                              stroke-width="2"
                              stroke-linecap="round"
                              stroke-linejoin="round"
                            />
                          </svg>
                        {/if}
                      </button>
                    </div>
                  </td>
                </tr>

                <!-- Expandable Detail Row -->
                {#if expandedSessionId === session.id}
                  <tr class="expanded-content">
                    <td colspan="7">
                      <div class="expanded-inner">
                        <div class="detail-grid">
                          <!-- Session Details Section -->
                          <div class="details-section">
                            <div class="section-label">
                              {$_("webssh.sessionDetails")}
                            </div>
                            <div class="detail-row">
                              <span class="detail-label"
                                >{$_("webssh.sessionId")}</span
                              >
                              <code class="detail-value mono">{session.id}</code
                              >
                            </div>
                            <div class="detail-row">
                              <span class="detail-label"
                                >{$_("webssh.peerId")}</span
                              >
                              <code class="detail-value mono"
                                >{session.peer_id}</code
                              >
                            </div>
                            <div class="detail-row">
                              <span class="detail-label"
                                >{$_("webssh.started")}</span
                              >
                              <span class="detail-value"
                                >{new Date(
                                  session.started_at,
                                ).toLocaleString()}</span
                              >
                            </div>
                          </div>

                          <!-- Stats Section -->
                          <div class="stats-section">
                            <div class="section-label">
                              {$_("webssh.statistics")}
                            </div>
                            <div class="detail-row">
                              <span class="detail-label"
                                >{$_("webssh.dataSent")}</span
                              >
                              <span class="detail-value"
                                >{formatBytes(session.bytes_sent || 0)}</span
                              >
                            </div>
                            <div class="detail-row">
                              <span class="detail-label"
                                >{$_("webssh.dataReceived")}</span
                              >
                              <span class="detail-value"
                                >{formatBytes(session.bytes_recv || 0)}</span
                              >
                            </div>
                          </div>
                        </div>

                        <div class="expanded-actions">
                          <button
                            class="expanded-btn success"
                            on:click|stopPropagation={() =>
                              openTerminal(session)}
                          >
                            <svg
                              width="16"
                              height="16"
                              viewBox="0 0 24 24"
                              fill="none"
                              stroke="currentColor"
                              stroke-width="2"
                            >
                              <polyline points="4 17 10 11 4 5" />
                              <line x1="12" y1="19" x2="20" y2="19" />
                            </svg>
                            {$_("webssh.openTerminal")}
                          </button>
                          <button
                            class="expanded-btn"
                            on:click|stopPropagation={() =>
                              openSessionActivity(session)}
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
                                d="M14 2H6a2 2 0 0 0-2 2v16a2 2 0 0 0 2 2h12a2 2 0 0 0 2-2V8z"
                              />
                              <polyline points="14 2 14 8 20 8" />
                            </svg>
                            {$_("webssh.viewActivity")}
                          </button>
                        </div>
                      </div>
                    </td>
                  </tr>
                {/if}
              {/each}
            {/each}
          </tbody>
        </table>

        <!-- Mobile WebSSH List -->
        <div class="mobile-webssh-list">
          {#each sortedGroupNames as groupName}
            {#if groupByDevice}
              <div class="group-header-mobile">
                <span class="tag-icon">#</span>
                <span class="tag-name">{groupName}</span>
                <span class="tag-count">{sessionGroups[groupName].length}</span>
              </div>
            {/if}
            {#each sessionGroups[groupName] as session (session.id)}
              {@const sharedMeta = getSessionSharedMeta(session)}
              <!-- svelte-ignore a11y-click-events-have-key-events -->
              <div
                class="webssh-mobile-card"
                class:expanded={expandedSessionId === session.id}
              >
                <!-- svelte-ignore a11y-click-events-have-key-events -->
                <div
                  class="card-main"
                  on:click={(e) => {
                    e.stopPropagation();
                    toggleExpandSession(session.id);
                  }}
                >
                  <div class="card-info">
                    <div
                      class="status-dot-mobile"
                      class:active={session.status === "connected"}
                      class:connecting={session.status === "connecting"}
                    />
                    <div class="name-box">
                      <span class="session-name"
                        >{session.peer_ip ||
                          session.hostname ||
                          $_("webssh.unknown")}</span
                      >
                      <div class="sub-info">
                        <span class="username">{session.username}</span>
                        <span class="separator">•</span>
                        <span class="peer-name"
                          >{getPeerName(session.peer_ip)}</span
                        >
                        {#if sharedMeta.isShared}
                          <span class="shared-inline">
                            shared{sharedMeta.ownerName
                              ? ` · ${sharedMeta.ownerName}`
                              : ""}
                          </span>
                        {/if}
                      </div>
                    </div>
                  </div>
                  <div class="card-meta">
                    <code class="port-badge">{session.ssh_port || 22}</code>
                    <div
                      class="expand-icon"
                      class:expanded={expandedSessionId === session.id}
                    >
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
                        <path d="m6 9 6 6 6-6" />
                      </svg>
                    </div>
                  </div>
                </div>

                {#if expandedSessionId === session.id}
                  <div
                    class="card-actions"
                    transition:scale={{ duration: 150, start: 0.95 }}
                  >
                    <button
                      class="action-btn success"
                      on:click|stopPropagation={() => openTerminal(session)}
                    >
                      <svg
                        width="20"
                        height="20"
                        viewBox="0 0 24 24"
                        fill="none"
                        xmlns="http://www.w3.org/2000/svg"
                      >
                        <polyline
                          points="4 17 10 11 4 5"
                          stroke="currentColor"
                          stroke-width="2"
                          stroke-linecap="round"
                          stroke-linejoin="round"
                        />
                        <line
                          x1="12"
                          y1="19"
                          x2="20"
                          y2="19"
                          stroke="currentColor"
                          stroke-width="2"
                          stroke-linecap="round"
                        />
                      </svg>
                    </button>
                    <button
                      class="action-btn"
                      on:click|stopPropagation={() =>
                        openSessionActivity(session)}
                    >
                      <svg
                        width="20"
                        height="20"
                        viewBox="0 0 24 24"
                        fill="none"
                        xmlns="http://www.w3.org/2000/svg"
                      >
                        <path
                          d="M14 2H6a2 2 0 0 0-2 2v16a2 2 0 0 0 2 2h12a2 2 0 0 0 2-2V8z"
                          stroke="currentColor"
                          stroke-width="2"
                        />
                        <polyline
                          points="14 2 14 8 20 8"
                          stroke="currentColor"
                          stroke-width="2"
                        />
                        <line
                          x1="16"
                          y1="13"
                          x2="8"
                          y2="13"
                          stroke="currentColor"
                          stroke-width="2"
                        />
                        <line
                          x1="16"
                          y1="17"
                          x2="8"
                          y2="17"
                          stroke="currentColor"
                          stroke-width="2"
                        />
                      </svg>
                    </button>
                    <button
                      class="action-btn danger"
                      title={$_("webssh.disconnect")}
                      on:click|stopPropagation={() =>
                        handleDisconnectSession(session)}
                      disabled={disconnectingSessionId === session.id}
                    >
                      {#if disconnectingSessionId === session.id}
                        <svg
                          class="spinning"
                          width="20"
                          height="20"
                          viewBox="0 0 24 24"
                          fill="none"
                          xmlns="http://www.w3.org/2000/svg"
                        >
                          <circle
                            cx="12"
                            cy="12"
                            r="10"
                            stroke="currentColor"
                            stroke-width="2"
                            stroke-dasharray="31.4 31.4"
                            stroke-linecap="round"
                          />
                        </svg>
                      {:else}
                        <svg
                          width="20"
                          height="20"
                          viewBox="0 0 24 24"
                          fill="none"
                          xmlns="http://www.w3.org/2000/svg"
                        >
                          <path
                            d="M18 6L6 18M6 6l12 12"
                            stroke="currentColor"
                            stroke-width="2"
                            stroke-linecap="round"
                            stroke-linejoin="round"
                          />
                        </svg>
                      {/if}
                    </button>
                    <button
                      class="action-btn delete"
                      title="Delete session"
                      on:click|stopPropagation={() =>
                        handleDeleteSession(session)}
                      disabled={deletingSessionId === session.id}
                    >
                      {#if deletingSessionId === session.id}
                        <svg
                          class="spinning"
                          width="20"
                          height="20"
                          viewBox="0 0 24 24"
                          fill="none"
                          xmlns="http://www.w3.org/2000/svg"
                        >
                          <circle
                            cx="12"
                            cy="12"
                            r="10"
                            stroke="currentColor"
                            stroke-width="2"
                            stroke-dasharray="31.4 31.4"
                            stroke-linecap="round"
                          />
                        </svg>
                      {:else}
                        <svg
                          width="20"
                          height="20"
                          viewBox="0 0 24 24"
                          fill="none"
                          xmlns="http://www.w3.org/2000/svg"
                        >
                          <path
                            d="M3 6h18M8 6V4h8v2M19 6l-1 14H6L5 6"
                            stroke="currentColor"
                            stroke-width="2"
                            stroke-linecap="round"
                            stroke-linejoin="round"
                          />
                        </svg>
                      {/if}
                    </button>
                  </div>

                  <div
                    class="card-expanded-content"
                    on:click|stopPropagation={() => {}}
                  >
                    <div class="expanded-inner p-4">
                      <!-- Session Details -->
                      <div class="details-section">
                        <div class="section-label">
                          {$_("webssh.sessionDetails")}
                        </div>
                        <div class="detail-row">
                          <span class="detail-label"
                            >{$_("webssh.sessionId")}</span
                          >
                          <code class="detail-value mono"
                            >{session.id.slice(0, 8)}...</code
                          >
                        </div>
                        <div class="detail-row">
                          <span class="detail-label">{$_("webssh.peerId")}</span
                          >
                          <code class="detail-value mono"
                            >{session.peer_id.slice(0, 8)}...</code
                          >
                        </div>
                        <div class="detail-row">
                          <span class="detail-label"
                            >{$_("webssh.started")}</span
                          >
                          <span class="detail-value"
                            >{formatLocalDate(session.started_at)}</span
                          >
                        </div>
                      </div>

                      <!-- Stats -->
                      <div class="stats-section mt-4">
                        <div class="section-label">
                          {$_("webssh.statistics")}
                        </div>
                        <div class="detail-grid-mobile">
                          <div class="stat-box">
                            <span class="stat-label"
                              >{$_("webssh.dataSent")}</span
                            >
                            <span class="stat-value"
                              >{formatBytes(session.bytes_sent || 0)}</span
                            >
                          </div>
                          <div class="stat-box">
                            <span class="stat-label"
                              >{$_("webssh.dataReceived")}</span
                            >
                            <span class="stat-value"
                              >{formatBytes(session.bytes_recv || 0)}</span
                            >
                          </div>
                        </div>
                      </div>
                    </div>
                  </div>
                {/if}
              </div>
            {/each}
          {/each}
        </div>
      {/if}
    </div>
  </div>
</div>

<style lang="scss">
  .webssh {
    position: absolute;
    top: 100px;
    left: 150px;
    width: 900px;
    height: 600px;
    display: flex;
    flex-direction: column;
    background: var(--mica);
    border-radius: 12px;
    box-shadow: 0 25px 50px -12px rgba(0, 0, 0, 0.5);
    overflow: hidden;
    z-index: 10;
  }

  .webssh.maximized {
    position: fixed;
    top: 0 !important;
    left: 0 !important;
    width: 100vw !important;
    height: calc(100vh - 48px) !important;
    border-radius: 0;
  }

  .webssh.minimized {
    display: none;
  }

  .mainApp {
    flex: 1;
    display: flex;
    flex-direction: row;
    overflow: hidden;
  }

  .content {
    flex: 1;
    padding: 0px 16px 2px 16px;
    overflow-y: auto;
    display: flex;
    flex-direction: column;
    gap: 16px;
    min-width: 0;
  }

  /* Toolbar */
  .toolbar {
    display: flex;
    gap: 12px;
    margin-top: 16px;
    justify-content: space-between;
    align-items: center;
  }

  .search-container {
    flex: 1;
    position: relative;
    max-width: 400px;
  }

  .search-icon {
    position: absolute;
    left: 12px;
    top: 50%;
    transform: translateY(-50%);
    color: rgb(var(--clr) / 50%);
  }

  .search-input {
    width: 100%;
    padding: 10px 12px 10px 40px;
    background: rgb(var(--bg2));
    border: 1px solid rgb(var(--clr) / 10%);
    border-radius: 8px;
    color: rgb(var(--clr));
    font-size: 14px;
    transition:
      border-color 0.2s,
      box-shadow 0.2s;
  }

  .search-input:focus {
    outline: none;
    border-color: rgb(var(--clrPrm));
    box-shadow: 0 0 0 3px rgb(var(--clrPrm) / 20%);
  }

  .search-input::placeholder {
    color: rgb(var(--clr) / 50%);
  }

  .toolbar-actions {
    display: flex;
    gap: 8px;
  }

  .icon-btn {
    padding: 10px;
    background: rgb(var(--bg2));
    border: 1px solid rgb(var(--clr) / 10%);
    border-radius: 8px;
    color: rgb(var(--clr));
    cursor: pointer;
    transition: all 0.2s;
    display: flex;
    align-items: center;
    justify-content: center;
  }

  .icon-btn:hover {
    background: rgb(var(--bg3));
    border-color: rgb(var(--clrPrm));
  }

  .icon-btn.spinning svg {
    animation: spin 1s linear infinite;
  }

  @keyframes spin {
    to {
      transform: rotate(360deg);
    }
  }

  /* States */
  .loading-state,
  .empty-state {
    flex: 1;
    display: flex;
    flex-direction: column;
    align-items: center;
    justify-content: center;
    gap: 12px;
    color: rgb(var(--clr) / 70%);
    padding: 40px;
  }

  .empty-icon {
    font-size: 48px;
  }

  .empty-state h3 {
    margin: 0;
    color: rgb(var(--clr));
  }

  .empty-state p {
    margin: 0;
    color: rgb(var(--clr) / 60%);
  }

  .primary-btn {
    padding: 10px 20px;
    background: rgb(var(--clrPrm));
    border: none;
    border-radius: 6px;
    color: white;
    font-size: 13px;
    font-weight: 600;
    cursor: pointer;
    transition: all 0.2s;
  }

  .primary-btn:hover {
    background: rgb(var(--clrPrm) / 85%);
    transform: translateY(-1px);
  }

  .spinner {
    width: 32px;
    height: 32px;
    border: 3px solid rgb(var(--clr) / 20%);
    border-top-color: rgb(var(--clrPrm));
    border-radius: 50%;
    animation: spin 1s linear infinite;
  }

  /* Sessions count */
  .sessions-count {
    font-size: 12px;
    color: rgb(var(--clr) / 50%);
  }

  /* Table */
  .sessions-table {
    width: 100%;
    border-collapse: collapse;
    font-size: 14px;
  }

  .sessions-table thead {
    position: sticky;
    top: 0;
    border-radius: 7px !important;
    z-index: 1;
    overflow: clip;
  }

  .sessions-table th {
    text-align: left;
    padding: 12px 8px;
    background: rgb(var(--bg3));
    color: rgb(var(--clr) / 70%);
    font-weight: 500;
    font-size: 12px;
    text-transform: uppercase;
    letter-spacing: 0.5px;
    // border-bottom: 1px solid rgb(var(--clr) / 10%);
  }

  .sessions-table td {
    padding: 12px 8px;
    border-bottom: 1px solid rgb(var(--clr) / 10%);
    color: rgb(var(--clr));
  }

  .session-row {
    transition: background 0.2s;
  }

  .session-row:hover {
    background: rgb(var(--clr) / 3%);
  }

  .session-row.expanded {
    background: rgb(var(--clr) / 5%);
  }

  /* Group header styles */
  .group-header {
    background: rgb(var(--bg2));
  }

  .group-header td {
    padding: 8px 12px;
    border-bottom: 1px solid rgb(var(--clr) / 15%);
  }

  /* Group Tags Redesign */
  .group-title-tag {
    display: inline-flex;
    align-items: stretch;
    font-family: ui-monospace, SFMono-Regular, Menlo, Monaco, Consolas,
      "Liberation Mono", "Courier New", monospace;
    font-size: 13px;
    line-height: normal;
    border-radius: 4px;
    overflow: hidden;
    box-shadow: 0 1px 2px rgba(0, 0, 0, 0.1);
  }

  .tag-icon {
    background-color: #10b981; /* Green 500 */
    color: white;
    padding: 4px 10px;
    font-weight: 700;
    display: flex;
    align-items: center;
    justify-content: center;
  }

  .tag-name {
    background-color: #e5e7eb; /* Gray 200 */
    color: #111827; /* Gray 900 */
    padding: 4px 12px;
    display: flex;
    align-items: center;
    font-weight: 600;
    white-space: nowrap;
  }

  @media (prefers-color-scheme: dark) {
    .tag-name {
      background-color: #374151; /* Gray 700 */
      color: #f3f4f6; /* Gray 100 */
    }
  }

  .tag-count {
    background-color: #10b981; /* Green 500 */
    color: white;
    padding: 4px 10px;
    font-weight: 700;
    display: flex;
    align-items: center;
    justify-content: center;
  }

  .icon-btn.active {
    background: rgb(var(--clrPrm) / 20%);
    border-color: rgb(var(--clrPrm));
    color: rgb(var(--clrPrm));
  }

  /* Columns */
  .col-expand {
    width: 40px;
  }
  .col-name {
    width: 25%;
  }
  .col-user {
    width: 15%;
  }
  .col-peer {
    width: 20%;
  }
  .col-port {
    width: 10%;
  }
  .col-status {
    width: 15%;
  }
  .col-actions {
    width: auto;
  }

  /* Expand button */
  .expand-btn {
    width: 24px;
    height: 24px;
    padding: 0;
    background: transparent;
    border: none;
    color: rgb(var(--clr) / 60%);
    cursor: pointer;
    display: flex;
    align-items: center;
    justify-content: center;
    border-radius: 4px;
    transition: all 0.2s;
  }

  .expand-btn:hover {
    background: rgb(var(--clr) / 10%);
    color: rgb(var(--clr));
  }

  .expand-btn.expanded svg {
    transform: rotate(180deg);
  }

  .expand-btn svg {
    transition: transform 0.2s;
  }

  /* Name cell */
  .name-cell {
    display: flex;
    align-items: center;
    gap: 8px;
  }

  .status-dot {
    width: 8px;
    height: 8px;
    border-radius: 50%;
    background: #6b7280;
    flex-shrink: 0;
  }

  .status-dot.active {
    background: #22c55e;
    box-shadow: 0 0 8px rgba(34, 197, 94, 0.5);
  }

  .status-dot.connecting {
    background: #f59e0b;
    box-shadow: 0 0 8px rgba(245, 158, 11, 0.5);
  }

  .session-name {
    font-weight: 500;
    font-family: "Cascadia Code", "Fira Code", monospace;
  }

  .username {
    color: rgb(var(--clr) / 80%);
  }

  .peer-name {
    color: rgb(var(--clr) / 70%);
    font-size: 13px;
  }

  .port-code {
    font-family: "Cascadia Code", "Fira Code", monospace;
    background: rgb(var(--bg3));
    padding: 4px 8px;
    border-radius: 4px;
    font-size: 12px;
  }

  /* Shared badge */
  .shared-badge {
    display: inline-block;
    padding: 2px 7px;
    border-radius: 10px;
    font-size: 10px;
    font-weight: 600;
    background: rgba(139, 92, 246, 0.12);
    color: #a78bfa;
    border: 1px solid rgba(139, 92, 246, 0.25);
    margin-left: 6px;
    vertical-align: middle;
    white-space: nowrap;
  }

  .shared-inline {
    color: #a78bfa;
    font-size: 11px;
    font-weight: 600;
    white-space: nowrap;
  }

  /* Status badge */
  .status-badge {
    display: inline-block;
    padding: 4px 10px;
    border-radius: 12px;
    font-size: 12px;
    font-weight: 500;
    background: rgb(var(--bg3));
    color: rgb(var(--clr) / 70%);
    text-transform: capitalize;
  }

  .status-badge.active {
    background: rgba(34, 197, 94, 0.1);
    color: #22c55e;
  }

  .status-badge.connecting {
    background: rgba(245, 158, 11, 0.1);
    color: #f59e0b;
  }

  .status-badge.disconnected {
    background: rgba(239, 68, 68, 0.1);
    color: #ef4444;
  }

  /* Action buttons */
  .action-buttons {
    display: flex;
    gap: 6px;
  }

  .action-btn {
    width: 32px;
    height: 32px;
    padding: 0;
    background: transparent;
    border: 1px solid rgb(var(--clr) / 10%);
    border-radius: 4px;
    color: rgb(var(--clr) / 70%);
    cursor: pointer;
    display: flex;
    align-items: center;
    justify-content: center;
    transition: all 0.2s;
  }

  .action-btn:hover {
    background: rgb(var(--clr) / 10%);
    border-color: rgb(var(--clr) / 20%);
    color: rgb(var(--clr));
  }

  .action-btn.success {
    color: #22c55e;
    border-color: rgba(34, 197, 94, 0.2);
  }

  .action-btn.success:hover {
    background: rgba(34, 197, 94, 0.1);
    border-color: rgba(34, 197, 94, 0.4);
  }

  .action-btn.danger {
    color: #ef4444;
    border-color: rgba(239, 68, 68, 0.2);
  }

  .action-btn.danger:hover {
    background: rgba(239, 68, 68, 0.1);
    border-color: rgba(239, 68, 68, 0.4);
  }

  .action-btn.danger:disabled {
    opacity: 0.5;
    cursor: not-allowed;
  }

  .action-btn.delete {
    color: #f97316;
    border-color: rgba(249, 115, 22, 0.2);
  }

  .action-btn.delete:hover {
    background: rgba(249, 115, 22, 0.1);
    border-color: rgba(249, 115, 22, 0.4);
  }

  .action-btn.delete:disabled {
    opacity: 0.5;
    cursor: not-allowed;
  }

  .action-btn .spinning {
    animation: spin 1s linear infinite;
  }

  /* Expanded content */
  .expanded-content {
    background: rgb(var(--bg3));
  }

  .expanded-content td {
    padding: 0;
    border-bottom: 1px solid rgb(var(--clr) / 10%);
  }

  .expanded-inner {
    padding: 20px;
  }

  .detail-grid {
    display: grid;
    grid-template-columns: 1fr 1fr;
    gap: 20px;
  }

  /* Details section */
  .details-section,
  .stats-section {
    background: rgb(var(--bg2));
    border-radius: 8px;
    padding: 16px;
    border: 1px solid rgb(var(--clr) / 10%);
  }

  .section-label {
    color: rgb(var(--clr) / 70%);
    font-size: 11px;
    text-transform: uppercase;
    letter-spacing: 1px;
    margin-bottom: 12px;
  }

  .detail-row {
    display: flex;
    justify-content: space-between;
    align-items: center;
    padding: 8px 0;
    border-bottom: 1px solid rgb(var(--clr) / 5%);
  }

  .detail-row:last-child {
    border-bottom: none;
  }

  .detail-label {
    font-size: 12px;
    color: rgb(var(--clr) / 50%);
  }

  .detail-value {
    font-size: 13px;
    color: rgb(var(--clr));
  }

  .detail-value.mono {
    font-family: "Cascadia Code", "Fira Code", monospace;
    font-size: 11px;
    color: #10b981;
    background: rgb(var(--bg3));
    padding: 4px 8px;
    border-radius: 4px;
  }

  /* Expanded actions */
  .expanded-actions {
    display: flex;
    gap: 10px;
    margin-top: 16px;
    padding-top: 16px;
    border-top: 1px solid rgb(var(--clr) / 10%);
  }

  .expanded-btn {
    display: flex;
    align-items: center;
    gap: 6px;
    padding: 10px 16px;
    border: 1px solid rgb(var(--clr) / 10%);
    background: rgb(var(--bg2));
    border-radius: 6px;
    cursor: pointer;
    color: rgb(var(--clr) / 80%);
    font-size: 13px;
    font-weight: 500;
    transition: all 0.2s;
  }

  .expanded-btn:hover {
    background: rgb(var(--clr) / 10%);
    border-color: rgb(var(--clr) / 20%);
  }

  .expanded-btn.success {
    background: rgba(34, 197, 94, 0.1);
    color: #22c55e;
    border-color: rgba(34, 197, 94, 0.3);
  }

  .expanded-btn.success:hover {
    background: rgba(34, 197, 94, 0.2);
  }

  /* Responsive Mobile View */
  @media (max-width: 768px) {
    .sessions-table {
      display: none;
    }
    .mobile-webssh-list {
      display: flex;
      flex-direction: column;
      gap: 12px;
      padding-bottom: 24px;
    }
  }

  @media (min-width: 769px) {
    .mobile-webssh-list {
      display: none;
    }
  }

  .group-header-mobile {
    display: flex;
    align-items: center;
    gap: 8px;
    padding: 8px 4px;
    font-size: 13px;
    font-weight: 600;
    color: rgb(var(--clr) / 70%);
  }

  .webssh-mobile-card {
    background: rgb(var(--bg3) / 50%);
    backdrop-filter: blur(20px);
    -webkit-backdrop-filter: blur(20px);
    border: 1px solid rgb(var(--clr) / 8%);
    border-radius: 16px;
    overflow: hidden;
    transition:
      all 0.2s cubic-bezier(0.4, 0, 0.2, 1),
      max-height 0.3s ease;
    box-shadow: 0 4px 12px rgb(0 0 0 / 5%);
    display: flex;
    flex-direction: column;
  }

  .webssh-mobile-card.expanded {
    background: rgb(var(--bg2));
    border-color: rgb(var(--clr) / 15%);
    box-shadow: 0 8px 24px rgb(0 0 0 / 10%);
  }

  .card-main {
    padding: 16px;
    display: flex;
    justify-content: space-between;
    align-items: center;
  }

  .card-info {
    display: flex;
    align-items: center;
    gap: 12px;
  }

  .status-dot-mobile {
    width: 8px;
    height: 8px;
    border-radius: 50%;
    background: #6b7280;
    box-shadow: 0 0 0 2px rgb(var(--bg1));
  }

  .status-dot-mobile.active {
    background: #22c55e;
    box-shadow:
      0 0 0 2px rgb(var(--bg1)),
      0 0 8px rgba(34, 197, 94, 0.4);
  }

  .status-dot-mobile.connecting {
    background: #f59e0b;
    box-shadow:
      0 0 0 2px rgb(var(--bg1)),
      0 0 8px rgba(245, 158, 11, 0.4);
  }

  .name-box {
    display: flex;
    flex-direction: column;
    gap: 2px;
  }

  .name-box .session-name {
    font-weight: 600;
    font-size: 15px;
    color: rgb(var(--clr));
  }

  .sub-info {
    display: flex;
    align-items: center;
    gap: 6px;
    font-size: 12px;
    color: rgb(var(--clr) / 50%);
  }

  .separator {
    font-size: 8px;
    opacity: 0.5;
  }

  .card-meta {
    display: flex;
    align-items: center;
    gap: 12px;
  }

  .port-badge {
    font-family: "Cascadia Code", monospace;
    font-size: 11px;
    padding: 4px 8px;
    background: rgb(var(--bg3));
    border: 1px solid rgb(var(--clr) / 10%);
    border-radius: 6px;
    color: rgb(var(--clr) / 70%);
  }

  .expand-icon {
    color: rgb(var(--clr) / 40%);
    transition: transform 0.2s;
  }

  .expand-icon.expanded {
    transform: rotate(180deg);
    color: rgb(var(--clr));
  }

  .card-actions {
    display: flex;
    padding: 0 16px 16px;
    gap: 8px;
    overflow-x: auto;
  }

  .card-actions .action-btn {
    flex: 1;
    height: 40px;
    border-radius: 8px;
    background: rgb(var(--bg3));
    border: 1px solid rgb(var(--clr) / 10%);
  }

  .card-actions .action-btn.success {
    color: #22c55e;
    border-color: rgba(34, 197, 94, 0.2);
    background: rgba(34, 197, 94, 0.05);
  }

  .card-actions .action-btn.danger {
    background: rgba(239, 68, 68, 0.05);
    border-color: rgba(239, 68, 68, 0.2);
  }

  .card-expanded-content {
    border-top: 1px solid rgb(var(--clr) / 8%);
    background: rgb(var(--bg3) / 30%);
    animation: slideDown 0.2s ease-out;
  }

  .detail-grid-mobile {
    display: grid;
    grid-template-columns: 1fr 1fr;
    gap: 8px;
  }

  .stat-box {
    background: rgb(var(--bg3));
    padding: 8px;
    border-radius: 6px;
    border: 1px solid rgb(var(--clr) / 5%);
    display: flex;
    flex-direction: column;
    gap: 2px;
  }

  .stat-label {
    font-size: 10px;
    color: rgb(var(--clr) / 50%);
    text-transform: uppercase;
  }

  .stat-value {
    font-size: 13px;
    font-family: "Cascadia Code", monospace;
    color: rgb(var(--clr));
  }

  @keyframes slideDown {
    from {
      opacity: 0;
      transform: translateY(-10px);
    }
    to {
      opacity: 1;
      transform: translateY(0);
    }
  }

  /* Responsive */
  @media (max-width: 768px) {
    .webssh {
      top: 0;
      left: 0;
      width: 100vw;
      height: calc(100vh - 48px);
      border-radius: 0;
    }

    .mainApp {
      flex-direction: column;
    }

    .content {
      padding: 12px;
      gap: 12px;
    }

    .toolbar {
      flex-direction: column;
      gap: 10px;
    }

    .search-container {
      max-width: 100%;
    }

    .toolbar-actions {
      width: 100%;
      justify-content: flex-end;
    }

    .sessions-table {
      min-width: 600px;
    }

    .detail-grid {
      grid-template-columns: 1fr;
    }

    .expanded-inner {
      padding: 12px;
    }

    .expanded-actions {
      flex-wrap: wrap;
    }

    .expanded-btn {
      flex: 1;
      justify-content: center;
      min-width: 100px;
    }
  }

  @media (max-width: 480px) {
    .content {
      padding: 8px;
    }

    .icon-btn {
      width: 36px;
      height: 36px;
    }

    .sessions-table th,
    .sessions-table td {
      padding: 8px 6px;
      font-size: 12px;
    }

    .status-badge {
      padding: 3px 8px;
      font-size: 10px;
    }

    .action-btn {
      width: 28px;
      height: 28px;
    }
  }
</style>
