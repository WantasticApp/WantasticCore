<script lang="ts">
  import { draggable } from "@neodrag/svelte";
  import { scale, fly } from "svelte/transition";
  import { onMount } from "svelte";
  import Titlebar from "$components/shared/Titlebar.svelte";
  import { peerStore, protoToDate } from "$store/peer";
  import {
    openedApps,
    activeThing,
    appZIndexes,
    bringToFront,
  } from "$store/store";

  // App identity - passed via props to differentiate SSH vs Winbox instances
  export let appId: string = "SSHActivityViewer";
  export let activityType: "SSH" | "Winbox" = "SSH";

  // Window state
  let isMaximized = false;
  let isMinimized = false;

  // Search and sort
  let searchQuery = "";
  let sortField: "timestamp" | "peer" | "username" = "timestamp";
  let sortDirection: "asc" | "desc" = "desc";

  // Peer filter (optional - passed via window global)
  let peerFilter: string | null = null;

  // Z-index for window stacking
  $: zIndex = $appZIndexes[appId] || 100;

  // Watch activeThing to restore when clicked from taskbar
  $: if ($activeThing === appId && isMinimized) {
    isMinimized = false;
  }

  // Bring to front when activated
  $: if ($activeThing === appId) {
    bringToFront(appId);
  }

  function handleFocus() {
    $activeThing = appId;
    bringToFront(appId);
  }

  function handleMaximize() {
    isMaximized = !isMaximized;
  }

  function handleReduce() {
    isMinimized = true;
    $activeThing = "";
  }

  function handleClose() {
    $openedApps = $openedApps.filter((app) => app !== appId);
    // Clean up the global filters
    if (typeof window !== "undefined") {
      const peerKey =
        activityType === "SSH"
          ? "__sshActivityPeerId"
          : "__winboxActivityPeerId";
      delete (window as any)[peerKey];
    }
  }

  // Unified activity type for display
  interface ActivityItem {
    id: string;
    peer_id: string;
    peer_name: string;
    username: string;
    client_ip: string;
    timestamp: Date;
    end_time: Date | null;
    duration_ms: number;
    bytes_sent?: number;
    bytes_recv?: number;
    user_agent?: string;
    session_name?: string;
    romon_mode?: boolean;
    is_active: boolean;
  }

  // Get all activities from peers store
  $: allActivities = (() => {
    const activities: ActivityItem[] = [];
    const peers = $peerStore.peers || [];
    let idx = 0; // Unique counter for deduplication

    for (const peer of peers) {
      if (activityType === "SSH" && peer.ssh_activities) {
        for (const act of peer.ssh_activities) {
          const ts = protoToDate(act.timestamp);
          const endTs = protoToDate(act.end_time);
          if (!ts) continue;

          activities.push({
            id: `ssh-${peer.id}-${idx++}`,
            peer_id: peer.id,
            peer_name: peer.name,
            username: act.username,
            client_ip: act.client_ip,
            timestamp: ts,
            end_time: endTs,
            duration_ms: act.duration_ms || 0,
            bytes_sent: act.bytes_sent,
            bytes_recv: act.bytes_recv,
            user_agent: act.user_agent,
            is_active: !endTs,
          });
        }
      } else if (activityType === "Winbox" && peer.winbox_activities) {
        for (const act of peer.winbox_activities) {
          const ts = protoToDate(act.timestamp);
          const endTs = protoToDate(act.end_time);
          if (!ts) continue;

          activities.push({
            id: `winbox-${peer.id}-${idx++}`,
            peer_id: peer.id,
            peer_name: peer.name,
            username: act.username,
            client_ip: act.client_ip,
            timestamp: ts,
            end_time: endTs,
            duration_ms: act.duration_ms || 0,
            session_name: act.session_name,
            romon_mode: act.romon_mode,
            is_active: !endTs,
          });
        }
      }
    }

    return activities;
  })();

  // Filter activities by peer if filter is set, then by search query
  $: filteredActivities = (() => {
    let items = peerFilter
      ? allActivities.filter((a) => a.peer_id === peerFilter)
      : allActivities;

    // Apply search filter
    if (searchQuery.trim()) {
      const q = searchQuery.toLowerCase();
      items = items.filter(
        (a) =>
          a.peer_name.toLowerCase().includes(q) ||
          a.username.toLowerCase().includes(q) ||
          a.client_ip.toLowerCase().includes(q) ||
          (a.session_name || "").toLowerCase().includes(q) ||
          (a.user_agent || "").toLowerCase().includes(q)
      );
    }

    // Apply sorting
    items = [...items].sort((a, b) => {
      let cmp = 0;
      switch (sortField) {
        case "timestamp":
          cmp = a.timestamp.getTime() - b.timestamp.getTime();
          break;
        case "peer":
          cmp = a.peer_name.localeCompare(b.peer_name);
          break;
        case "username":
          cmp = a.username.localeCompare(b.username);
          break;
      }
      return sortDirection === "asc" ? cmp : -cmp;
    });

    return items;
  })();

  function toggleSort(field: typeof sortField) {
    if (sortField === field) {
      sortDirection = sortDirection === "asc" ? "desc" : "asc";
    } else {
      sortField = field;
      sortDirection = "desc";
    }
  }

  function formatDuration(ms: number): string {
    if (!ms || ms <= 0) return "-";
    const seconds = Math.floor(ms / 1000);
    if (seconds < 60) return seconds + "s";
    if (seconds < 3600)
      return Math.floor(seconds / 60) + "m " + (seconds % 60) + "s";
    const hours = Math.floor(seconds / 3600);
    const mins = Math.floor((seconds % 3600) / 60);
    return hours + "h " + mins + "m";
  }

  function formatBytes(bytes: number | undefined): string {
    if (!bytes) return "-";
    if (bytes < 1024) return bytes + " B";
    if (bytes < 1048576) return (bytes / 1024).toFixed(1) + " KB";
    return (bytes / 1048576).toFixed(1) + " MB";
  }

  function formatDateTime(date: Date): string {
    return date.toLocaleString();
  }

  // Loading state - we use the peerStore's loading state
  $: isLoading = $peerStore.isLoading;

  async function handleRefresh() {
    await peerStore.listPeers();
  }

  onMount(() => {
    // Get the peer filter from window global based on activity type
    if (typeof window !== "undefined") {
      const peerKey =
        activityType === "SSH"
          ? "__sshActivityPeerId"
          : "__winboxActivityPeerId";
      peerFilter = (window as any)[peerKey] || null;
    }
    // Load peers if not already loaded
    if ($peerStore.peers.length === 0) {
      peerStore.listPeers();
    }
  });

  // Computed title
  $: title =
    activityType === "SSH" ? "SSH Activity Viewer" : "Winbox Activity Viewer";
  $: peerName = peerFilter
    ? $peerStore.peers.find((p) => p.id === peerFilter)?.name
    : null;
</script>

<div
  class="activity-viewer activeShadow"
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
    appName={appId}
    on:maximize={handleMaximize}
    on:reduce={handleReduce}
  >
    <svg
      xmlns="http://www.w3.org/2000/svg"
      width="16"
      height="16"
      viewBox="0 0 24 24"
      fill="none"
      stroke="currentColor"
      stroke-width="2"
    >
      <path d="M14 2H6a2 2 0 0 0-2 2v16a2 2 0 0 0 2 2h12a2 2 0 0 0 2-2V8z" />
      <polyline points="14 2 14 8 20 8" />
      <line x1="16" y1="13" x2="8" y2="13" />
      <line x1="16" y1="17" x2="8" y2="17" />
    </svg>
    <span class="appName pl-2">{title} {peerName ? `(${peerName})` : ""}</span>
  </Titlebar>

  <div class="mainApp">
    <div class="content">
      <div class="toolbar">
        <div class="toolbar-left">
          <div class="search-box">
            <svg
              width="16"
              height="16"
              viewBox="0 0 24 24"
              fill="none"
              stroke="currentColor"
              stroke-width="2"
            >
              <circle cx="11" cy="11" r="8" />
              <line x1="21" y1="21" x2="16.65" y2="16.65" />
            </svg>
            <input
              type="text"
              placeholder="Search activities..."
              bind:value={searchQuery}
            />
          </div>
        </div>

        <div class="toolbar-right">
          <div class="sort-controls">
            <button
              class="sort-btn"
              class:active={sortField === "timestamp"}
              on:click={() => toggleSort("timestamp")}
            >
              Time
              {#if sortField === "timestamp"}
                <svg
                  width="12"
                  height="12"
                  viewBox="0 0 24 24"
                  fill="none"
                  stroke="currentColor"
                  stroke-width="2"
                >
                  {#if sortDirection === "desc"}
                    <polyline points="6 9 12 15 18 9" />
                  {:else}
                    <polyline points="6 15 12 9 18 15" />
                  {/if}
                </svg>
              {/if}
            </button>
            <button
              class="sort-btn"
              class:active={sortField === "peer"}
              on:click={() => toggleSort("peer")}
            >
              Peer
              {#if sortField === "peer"}
                <svg
                  width="12"
                  height="12"
                  viewBox="0 0 24 24"
                  fill="none"
                  stroke="currentColor"
                  stroke-width="2"
                >
                  {#if sortDirection === "desc"}
                    <polyline points="6 9 12 15 18 9" />
                  {:else}
                    <polyline points="6 15 12 9 18 15" />
                  {/if}
                </svg>
              {/if}
            </button>
            <button
              class="sort-btn"
              class:active={sortField === "username"}
              on:click={() => toggleSort("username")}
            >
              User
              {#if sortField === "username"}
                <svg
                  width="12"
                  height="12"
                  viewBox="0 0 24 24"
                  fill="none"
                  stroke="currentColor"
                  stroke-width="2"
                >
                  {#if sortDirection === "desc"}
                    <polyline points="6 9 12 15 18 9" />
                  {:else}
                    <polyline points="6 15 12 9 18 15" />
                  {/if}
                </svg>
              {/if}
            </button>
          </div>

          <button
            class="toolbar-btn"
            on:click={handleRefresh}
            class:spinning={isLoading}
          >
            <svg
              width="16"
              height="16"
              viewBox="0 0 24 24"
              fill="none"
              stroke="currentColor"
              stroke-width="2"
            >
              <polyline points="23 4 23 10 17 10" />
              <path d="M20.49 15a9 9 0 1 1-2.12-9.36L23 10" />
            </svg>
          </button>
        </div>
      </div>

      <div class="header">
        <div class="header-info">
          <span class="log-count"
            >{filteredActivities.length} activit{filteredActivities.length !== 1
              ? "ies"
              : "y"}</span
          >
        </div>
        {#if peerFilter}
          <button class="clear-filter" on:click={() => (peerFilter = null)}>
            <svg
              width="14"
              height="14"
              viewBox="0 0 24 24"
              fill="none"
              stroke="currentColor"
              stroke-width="2"
            >
              <line x1="18" y1="6" x2="6" y2="18" />
              <line x1="6" y1="6" x2="18" y2="18" />
            </svg>
            Clear Filter
          </button>
        {/if}
      </div>

      <div class="logs-container">
        {#if isLoading}
          <div class="empty-state">
            <div class="loading-spinner" />
            <h3>Loading Activities...</h3>
            <p>Fetching activity data from peers</p>
          </div>
        {:else if filteredActivities.length === 0}
          <div class="empty-state">
            <svg
              width="48"
              height="48"
              viewBox="0 0 24 24"
              fill="none"
              stroke="currentColor"
              stroke-width="1.5"
            >
              <path
                d="M 14 2H6a2 2 0 0 0-2 2v16a2 2 0 0 0 2 2h12a2 2 0 0 0 2-2V8z"
              />
              <polyline points="14 2 14 8 20 8" />
            </svg>
            <h3>No Activities</h3>
            <p>
              {activityType === "SSH" ? "SSH" : "Winbox"} activity will appear here
              once sessions are established
            </p>
          </div>
        {:else}
          <div class="logs-timeline">
            {#each filteredActivities as activity (activity.id)}
              <div class="log-entry" transition:fly={{ y: 10, duration: 150 }}>
                <div
                  class="log-timeline-dot"
                  style="background: {activity.is_active
                    ? '#22c55e'
                    : '#6b7280'}"
                />
                <div class="log-content">
                  <div class="log-header">
                    <div class="header-left">
                      <span class="peer-name">{activity.peer_name}</span>
                      {#if activity.is_active}
                        <span class="status-badge active">Active</span>
                      {/if}
                      {#if activityType === "Winbox" && activity.romon_mode}
                        <span class="status-badge romon">RoMON</span>
                      {/if}
                    </div>
                    <span class="log-time"
                      >{formatDateTime(activity.timestamp)}</span
                    >
                  </div>

                  <div class="log-details">
                    <div class="detail-row">
                      <span class="detail-label">Username</span>
                      <span class="detail-value">{activity.username}</span>
                    </div>
                    <div class="detail-row">
                      <span class="detail-label">Client IP</span>
                      <span class="detail-value mono">{activity.client_ip}</span
                      >
                    </div>
                    {#if activityType === "Winbox" && activity.session_name}
                      <div class="detail-row">
                        <span class="detail-label">Session</span>
                        <span class="detail-value">{activity.session_name}</span
                        >
                      </div>
                    {/if}
                    {#if activityType === "SSH" && activity.user_agent}
                      <div class="detail-row full-width">
                        <span class="detail-label">User Agent</span>
                        <span class="detail-value small"
                          >{activity.user_agent}</span
                        >
                      </div>
                    {/if}
                  </div>

                  <div class="log-stats">
                    {#if activityType === "SSH"}
                      <div class="stat">
                        <svg
                          width="12"
                          height="12"
                          viewBox="0 0 24 24"
                          fill="none"
                          stroke="currentColor"
                          stroke-width="2"
                        >
                          <polyline points="23 6 13.5 15.5 8.5 10.5 1 18" />
                        </svg>
                        <span class="stat-label">Sent</span>
                        <span class="stat-value"
                          >{formatBytes(activity.bytes_sent)}</span
                        >
                      </div>
                      <div class="stat">
                        <svg
                          width="12"
                          height="12"
                          viewBox="0 0 24 24"
                          fill="none"
                          stroke="currentColor"
                          stroke-width="2"
                        >
                          <polyline points="23 18 13.5 8.5 8.5 13.5 1 6" />
                        </svg>
                        <span class="stat-label">Recv</span>
                        <span class="stat-value"
                          >{formatBytes(activity.bytes_recv)}</span
                        >
                      </div>
                    {/if}
                    <div class="stat">
                      <svg
                        width="12"
                        height="12"
                        viewBox="0 0 24 24"
                        fill="none"
                        stroke="currentColor"
                        stroke-width="2"
                      >
                        <circle cx="12" cy="12" r="10" />
                        <polyline points="12 6 12 12 16 14" />
                      </svg>
                      <span class="stat-label">Duration</span>
                      <span class="stat-value"
                        >{activity.is_active
                          ? "Ongoing"
                          : formatDuration(activity.duration_ms)}</span
                      >
                    </div>
                    {#if activity.end_time}
                      <div class="stat">
                        <svg
                          width="12"
                          height="12"
                          viewBox="0 0 24 24"
                          fill="none"
                          stroke="currentColor"
                          stroke-width="2"
                        >
                          <rect
                            x="3"
                            y="4"
                            width="18"
                            height="18"
                            rx="2"
                            ry="2"
                          />
                          <line x1="16" y1="2" x2="16" y2="6" />
                          <line x1="8" y1="2" x2="8" y2="6" />
                          <line x1="3" y1="10" x2="21" y2="10" />
                        </svg>
                        <span class="stat-label">Ended</span>
                        <span class="stat-value"
                          >{formatDateTime(activity.end_time)}</span
                        >
                      </div>
                    {/if}
                  </div>
                </div>
              </div>
            {/each}
          </div>
        {/if}
      </div>
    </div>
  </div>
</div>

<style lang="scss">
  .activity-viewer {
    position: absolute;
    top: 80px;
    left: 200px;
    width: 700px;
    height: 550px;
    display: flex;
    flex-direction: column;
    background: var(--mica);
    border-radius: 12px;
    box-shadow: 0 25px 50px -12px rgba(0, 0, 0, 0.5);
    overflow: hidden;
  }

  .activity-viewer.maximized {
    position: fixed;
    top: 0 !important;
    left: 0 !important;
    width: 100vw !important;
    height: calc(100vh - 48px) !important;
    border-radius: 0;
  }

  .activity-viewer.minimized {
    display: none;
  }

  @media (max-width: 768px) {
    .activity-viewer {
      top: 0;
      left: 0;
      width: 100vw;
      height: calc(100vh - 48px);
      border-radius: 0;
    }
  }

  .mainApp {
    flex: 1;
    display: flex;
    flex-direction: column;
    overflow: hidden;
  }

  .content {
    flex: 1;
    display: flex;
    flex-direction: column;
    overflow: hidden;
    padding: 16px;
    gap: 12px;
  }

  .toolbar {
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: 12px;
    padding-bottom: 8px;
    border-bottom: 1px solid rgb(var(--clr) / 8%);
  }

  .toolbar-left {
    display: flex;
    align-items: center;
    gap: 12px;
  }

  .toolbar-right {
    display: flex;
    align-items: center;
    gap: 8px;
  }

  .search-box {
    display: flex;
    align-items: center;
    gap: 8px;
    padding: 6px 12px;
    background: rgb(var(--bg2));
    border: 1px solid rgb(var(--clr) / 10%);
    border-radius: 6px;
    min-width: 200px;

    svg {
      opacity: 0.5;
      flex-shrink: 0;
    }

    input {
      flex: 1;
      background: transparent;
      border: none;
      outline: none;
      color: rgb(var(--clr));
      font-size: 13px;

      &::placeholder {
        color: rgb(var(--clr) / 40%);
      }
    }
  }

  .sort-controls {
    display: flex;
    gap: 4px;
  }

  .sort-btn {
    display: flex;
    align-items: center;
    gap: 4px;
    padding: 6px 10px;
    background: rgb(var(--bg2));
    border: 1px solid rgb(var(--clr) / 10%);
    border-radius: 6px;
    color: rgb(var(--clr) / 60%);
    font-size: 12px;
    cursor: pointer;
    transition: all 0.2s;

    &:hover {
      background: rgb(var(--bg3));
      color: rgb(var(--clr) / 80%);
    }

    &.active {
      background: rgb(var(--accent) / 10%);
      border-color: rgb(var(--accent) / 30%);
      color: rgb(var(--accent));
    }
  }

  .toolbar-btn {
    display: flex;
    align-items: center;
    justify-content: center;
    width: 32px;
    height: 32px;
    background: rgb(var(--bg2));
    border: 1px solid rgb(var(--clr) / 10%);
    border-radius: 6px;
    color: rgb(var(--clr) / 70%);
    cursor: pointer;
    transition: all 0.2s;

    &:hover {
      background: rgb(var(--bg3));
      color: rgb(var(--clr));
    }

    &.spinning svg {
      animation: spin 1s linear infinite;
    }
  }

  .header {
    display: flex;
    align-items: center;
    justify-content: space-between;
  }

  .header-info {
    display: flex;
    align-items: baseline;
    gap: 12px;
  }

  .header-info h2 {
    margin: 0;
    font-size: 18px;
    font-weight: 600;
    color: rgb(var(--clr));
  }

  .log-count {
    font-size: 13px;
    color: rgb(var(--clr) / 50%);
  }

  .clear-filter {
    display: flex;
    align-items: center;
    gap: 6px;
    padding: 6px 12px;
    background: rgba(239, 68, 68, 0.1);
    border: 1px solid rgba(239, 68, 68, 0.3);
    border-radius: 6px;
    color: #ef4444;
    font-size: 12px;
    cursor: pointer;
    transition: all 0.2s;
  }

  .clear-filter:hover {
    background: rgba(239, 68, 68, 0.2);
  }

  .logs-container {
    flex: 1;
    overflow-y: auto;
  }

  .empty-state {
    display: flex;
    flex-direction: column;
    align-items: center;
    justify-content: center;
    height: 100%;
    gap: 12px;
    color: rgb(var(--clr) / 50%);
  }

  .empty-state h3 {
    margin: 0;
    color: rgb(var(--clr) / 80%);
  }

  .empty-state p {
    margin: 0;
    font-size: 13px;
  }

  .loading-spinner {
    width: 32px;
    height: 32px;
    border: 3px solid rgb(var(--clr) / 20%);
    border-top-color: #10b981;
    border-radius: 50%;
    animation: spin 1s linear infinite;
  }

  @keyframes spin {
    to {
      transform: rotate(360deg);
    }
  }

  .logs-timeline {
    display: flex;
    flex-direction: column;
    gap: 12px;
  }

  .log-entry {
    display: flex;
    gap: 12px;
    padding: 12px;
    background: rgb(var(--bg2));
    border-radius: 8px;
    border: 1px solid rgb(var(--clr) / 8%);
    transition: all 0.2s;
  }

  .log-entry:hover {
    border-color: rgb(var(--clr) / 15%);
    background: rgb(var(--bg3));
  }

  .log-timeline-dot {
    width: 10px;
    height: 10px;
    border-radius: 50%;
    margin-top: 4px;
    flex-shrink: 0;
  }

  .log-content {
    flex: 1;
    display: flex;
    flex-direction: column;
    gap: 10px;
  }

  .log-header {
    display: flex;
    align-items: center;
    justify-content: space-between;
  }

  .header-left {
    display: flex;
    align-items: center;
    gap: 8px;
  }

  .peer-name {
    font-size: 14px;
    font-weight: 600;
    color: rgb(var(--clr));
  }

  .status-badge {
    display: inline-flex;
    padding: 2px 8px;
    border-radius: 4px;
    font-size: 10px;
    font-weight: 600;
    text-transform: uppercase;
    letter-spacing: 0.5px;
  }

  .status-badge.active {
    background: rgba(34, 197, 94, 0.15);
    color: #22c55e;
  }

  .status-badge.romon {
    background: rgba(168, 85, 247, 0.15);
    color: #a855f7;
  }

  .log-time {
    font-size: 12px;
    color: rgb(var(--clr) / 50%);
  }

  .log-details {
    display: grid;
    grid-template-columns: repeat(auto-fit, minmax(180px, 1fr));
    gap: 8px;
  }

  .detail-row {
    display: flex;
    flex-direction: column;
    gap: 2px;
  }

  .detail-row.full-width {
    grid-column: 1 / -1;
  }

  .detail-label {
    font-size: 10px;
    font-weight: 600;
    text-transform: uppercase;
    letter-spacing: 0.5px;
    color: rgb(var(--clr) / 50%);
  }

  .detail-value {
    font-size: 13px;
    color: rgb(var(--clr));
  }

  .detail-value.small {
    font-size: 11px;
    color: rgb(var(--clr) / 70%);
    word-break: break-all;
  }

  .detail-value.mono {
    font-family: "Cascadia Code", "Fira Code", monospace;
    color: #10b981;
  }

  .detail-value.command {
    font-family: "Cascadia Code", "Fira Code", monospace;
    padding: 4px 8px;
    background: rgb(var(--bg3));
    border-radius: 4px;
    color: #3b82f6;
  }

  .type-badge {
    display: inline-flex;
    padding: 2px 8px;
    border-radius: 4px;
    font-size: 11px;
    font-weight: 500;
    width: fit-content;
  }

  .type-badge.webssh {
    background: rgba(16, 185, 129, 0.1);
    color: #10b981;
  }

  .type-badge.winbox {
    background: rgba(59, 130, 246, 0.1);
    color: #3b82f6;
  }

  .log-stats {
    display: flex;
    gap: 16px;
    padding-top: 8px;
    border-top: 1px solid rgb(var(--clr) / 8%);
    flex-wrap: wrap;
  }

  .stat {
    display: flex;
    align-items: center;
    gap: 6px;
    font-size: 12px;
    color: rgb(var(--clr) / 60%);
  }

  .stat-label {
    color: rgb(var(--clr) / 40%);
  }

  .stat-value {
    font-weight: 500;
    color: rgb(var(--clr) / 80%);
  }

  /* Mobile responsive styles */
  @media (max-width: 768px) {
    .content {
      padding: 12px;
      gap: 10px;
    }

    .toolbar {
      flex-direction: column;
      align-items: stretch;
      gap: 10px;
    }

    .toolbar-left {
      width: 100%;
    }

    .toolbar-right {
      justify-content: space-between;
    }

    .search-box {
      flex: 1;
      min-width: unset;
    }

    .sort-controls {
      flex: 1;
      justify-content: flex-start;
    }

    .sort-btn {
      padding: 6px 8px;
      font-size: 11px;
    }

    .header {
      flex-direction: column;
      align-items: flex-start;
      gap: 8px;
    }

    .log-entry {
      padding: 10px;
    }

    .log-header {
      flex-direction: column;
      align-items: flex-start;
      gap: 6px;
    }

    .log-details {
      grid-template-columns: 1fr;
    }

    .log-stats {
      flex-wrap: wrap;
      gap: 10px;
    }

    .stat {
      min-width: 80px;
    }
  }

  @media (max-width: 480px) {
    .content {
      padding: 8px;
    }

    .sort-btn span {
      display: none;
    }

    .peer-name {
      font-size: 13px;
    }

    .log-time {
      font-size: 11px;
    }

    .detail-label {
      font-size: 9px;
    }

    .detail-value {
      font-size: 12px;
    }

    .stat {
      font-size: 11px;
    }
  }
</style>
