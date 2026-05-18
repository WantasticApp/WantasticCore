<script lang="ts">
  import { draggable } from "@neodrag/svelte";
  import { scale } from "svelte/transition";
  import { onMount } from "svelte";
  import Titlebar from "$components/shared/Titlebar.svelte";
  import {
    winboxAccountStore,
    type WinboxAccount,
  } from "$store/winboxAccounts";
  import { peerStore } from "$store/peer";
  import {
    openedApps,
    activeThing,
    minimizedApps,
    appZIndexes,
    bringToFront,
  } from "$store/store";
  import { translateError$, _ } from "$store/i18n";
  import { toasts } from "$store/toast"; // Window state
  import Icon from "$components/Icon.svelte";
  import {
    createPeerSharedLookup,
    resolveSharedMeta,
  } from "$lib/sharedAccess";
  let isMaximized = false;
  let isMinimized = false;

  // Z-index for window stacking
  $: zIndex = $appZIndexes["Winbox"] || 100;

  // Search and filter
  let searchQuery = "";

  // Expanded rows state
  let expandedRows: Record<string, boolean> = {};

  // Editable allowed IPs per account
  let editableAllowedIps: Record<string, string> = {};
  let savingAllowedIps: Record<string, boolean> = {};

  // Copy feedback
  let copiedId: string | null = null;

  // Bring to front when activated
  $: if ($activeThing === "Winbox") {
    bringToFront("Winbox");
  }

  function handleFocus() {
    $activeThing = "Winbox";
    bringToFront("Winbox");
  }

  // Reactive data from stores
  $: accounts = $winboxAccountStore.accounts;
  $: peers = $peerStore.peers;
  $: peerSharedLookup = createPeerSharedLookup(peers);
  $: isLoading = $winboxAccountStore.isLoading;
  $: error = $winboxAccountStore.error;

  // Filtered accounts based on search
  $: filteredAccounts = accounts.filter((account) => {
    if (!searchQuery) return true;
    const query = searchQuery.toLowerCase();
    return (
      account.name?.toLowerCase().includes(query) ||
      account.router_ip?.toLowerCase().includes(query) ||
      account.peer_id?.toLowerCase().includes(query)
    );
  });

  // Grouping logic
  let groupByDevice = localStorage.getItem("winbox_groupByDevice") === "true";
  $: {
    localStorage.setItem("winbox_groupByDevice", String(groupByDevice));
  }

  $: accountGroups = groupByDevice
    ? groupAccounts(filteredAccounts)
    : { [$_("winbox.allAccounts")]: filteredAccounts };
  $: sortedGroupNames = Object.keys(accountGroups).sort();

  function groupAccounts(list: typeof accounts) {
    const groups: Record<string, typeof accounts> = {};
    list.forEach((acc) => {
      const peerName = getPeerName(acc.peer_id);
      if (!groups[peerName]) groups[peerName] = [];
      groups[peerName].push(acc);
    });
    return groups;
  }

  // Server address for connection guide
  const serverAddr = "winbox.wantastic.app";

  onMount(async () => {
    await Promise.all([
      winboxAccountStore.listAccounts(),
      peerStore.listPeers(),
    ]);
  });

  // Watch minimizedApps to sync local state
  // $: isMinimized = $minimizedApps.includes("Winbox");

  // Watch activeThing to restore when clicked from taskbar
  $: if ($activeThing === "Winbox" && isMinimized) {
    $minimizedApps = $minimizedApps.filter((a) => a !== "Winbox");
  }

  function handleMaximize() {
    isMaximized = !isMaximized;
  }

  function handleReduce() {
    if (!$minimizedApps.includes("Winbox")) {
      $minimizedApps = [...$minimizedApps, "Winbox"];
    }
    $activeThing = "";
  }

  // Open Winbox activity viewer
  function openActivityViewer() {
    if (!$openedApps.includes("WinboxActivityViewer")) {
      $openedApps = [...$openedApps, "WinboxActivityViewer"];
    }
    $activeThing = "WinboxActivityViewer";
    bringToFront("WinboxActivityViewer");
  }

  let lastToggleTime = 0;
  function toggleRow(accountId: string, account: WinboxAccount) {
    const now = Date.now();
    if (now - lastToggleTime < 300) return;
    lastToggleTime = now;

    // Close other rows first (accordion behavior)
    Object.keys(expandedRows).forEach((id) => {
      if (id !== accountId) expandedRows[id] = false;
    });
    expandedRows[accountId] = !expandedRows[accountId];

    // Initialize editable allowed IPs when expanding
    if (expandedRows[accountId]) {
      editableAllowedIps[accountId] =
        account.allowed_client_ips?.join(", ") || "";
    }
  }

  function getPeerName(peerId: string): string {
    const peer = peers.find((p) => p.id === peerId);
    return peer?.name || peerId?.slice(0, 12) + "...";
  }

  function getAccountSharedMeta(account: WinboxAccount) {
    return resolveSharedMeta(account, peerSharedLookup);
  }

  async function copyToken(token: string, accountId: string) {
    try {
      await navigator.clipboard.writeText(token);
      copiedId = accountId;
      setTimeout(() => (copiedId = null), 2000);
      toasts.success("Copied to clipboard");
    } catch (err) {
      console.error("Failed to copy:", err);
      toasts.error("Failed to copy");
    }
  }

  async function copyEndpoint() {
    try {
      await navigator.clipboard.writeText(`${serverAddr}`);
      toasts.success("Endpoint copied");
    } catch (err) {
      console.error("Failed to copy endpoint:", err);
      toasts.error("Failed to copy");
    }
  }

  async function copyRouterIP(ip: string) {
    try {
      await navigator.clipboard.writeText(ip);
      toasts.success("Router IP copied");
    } catch (err) {
      console.error("Failed to copy router IP:", err);
      toasts.error("Failed to copy");
    }
  }

  async function handleToggleAccount(account: WinboxAccount) {
    try {
      await winboxAccountStore.updateAccount(account.id, {
        enabled: !account.enabled,
      });
    } catch (err: any) {
      console.error("Failed to toggle account:", err);
    }
  }

  async function handleDeleteAccount(account: WinboxAccount) {
    if (getAccountSharedMeta(account).isShared) {
      toasts.error("Shared Winbox sessions cannot be deleted");
      return;
    }
    if (
      !confirm(
        `Delete Winbox account "${account.name}"? This cannot be undone.`
      )
    )
      return;
    try {
      await winboxAccountStore.deleteAccount(account.id);
    } catch (err: any) {
      console.error("Failed to delete account:", err);
    }
  }
  function handleResize() {
    isMaximized = false;
    isMinimized = false;
  }
  async function handleSaveAllowedIps(accountId: string) {
    const ipsString = editableAllowedIps[accountId] || "";
    const ipsArray = ipsString
      .split(",")
      .map((ip) => ip.trim())
      .filter((ip) => ip.length > 0);

    savingAllowedIps[accountId] = true;
    try {
      await winboxAccountStore.updateAccount(accountId, {
        allowed_client_ips: ipsArray,
      });
    } catch (err: any) {
      console.error("Failed to save allowed IPs:", err);
    } finally {
      savingAllowedIps[accountId] = false;
    }
  }

  function handleCreateAccount() {
    if (!$openedApps.includes("NewWinboxSession")) {
      $openedApps = [...$openedApps, "NewWinboxSession"];
    }
    $activeThing = "NewWinboxSession";
    bringToFront("NewWinboxSession");
  }

  async function handleRefresh() {
    await winboxAccountStore.listAccounts();
  }

  function truncateToken(token: string, length = 20): string {
    if (!token) return "";
    if (token.length <= length * 2) return token;
    return token.slice(0, length) + "..." + token.slice(-length);
  }
</script>

<div
  class="winbox-accounts activeShadow"
  class:maximized={isMaximized}
  class:minimized={isMinimized}
  class:resizable={!isMaximized}
  style:z-index={zIndex}
  on:resize={handleResize}
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
    appName="Winbox"
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
        <linearGradient id="winboxGrad" x1="0%" y1="0%" x2="100%" y2="100%">
          <stop offset="0%" style="stop-color:#3b82f6;stop-opacity:1" />
          <stop offset="100%" style="stop-color:#1d4ed8;stop-opacity:1" />
        </linearGradient>
      </defs>
      <rect
        x="15"
        y="15"
        width="70"
        height="70"
        rx="8"
        fill="#1f2937"
        stroke="url(#winboxGrad)"
        stroke-width="3"
      />
      <rect
        x="15"
        y="15"
        width="70"
        height="15"
        rx="8"
        fill="url(#winboxGrad)"
      />
      <circle cx="23" cy="22.5" r="3" fill="#ef4444" />
      <circle cx="33" cy="22.5" r="3" fill="#fbbf24" />
      <circle cx="43" cy="22.5" r="3" fill="#10b981" />
      <circle
        cx="50"
        cy="50"
        r="10"
        fill="none"
        stroke="url(#winboxGrad)"
        stroke-width="3"
      />
      <rect
        x="58"
        y="47"
        width="18"
        height="6"
        rx="2"
        fill="url(#winboxGrad)"
      />
      <rect x="66" y="53" width="4" height="8" rx="1" fill="url(#winboxGrad)" />
      <rect x="72" y="53" width="4" height="6" rx="1" fill="url(#winboxGrad)" />
    </svg>
    <span class="appName pl-2">{$_("winbox.winboxAccounts")}</span>
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
            placeholder="Search by name, peer, router IP..."
            class="search-input"
          />
        </div>
        <div class="toolbar-actions">
          <button
            class="icon-btn"
            on:click={openActivityViewer}
            title={$_("winbox.viewActivities")}
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
            on:click={handleCreateAccount}
            title={$_("winbox.createAccount")}
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
            title={$_("winbox.groupByDevice")}
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

      {#if error}
        <div class="error-message">
          <strong>{$_("common.error")}:</strong>
          {$translateError$(error)}
        </div>
      {/if}

      {#if isLoading}
        <div class="loading-state">
          <div class="spinner" />
          <p>{$_("winbox.loadingAccounts")}</p>
        </div>
      {:else if filteredAccounts.length === 0 && searchQuery}
        <div class="empty-state">
          <div class="empty-icon"><Icon name="search" /></div>
          <p>
            {$_("winbox.noMatchingAccounts", {
              values: { query: searchQuery },
            })}
          </p>
        </div>
      {:else if accounts.length === 0}
        <div class="empty-state">
          <div class="empty-icon"><Icon name="winbox" /></div>
          <h3>{$_("winbox.noAccounts")}</h3>
          <p>{$_("winbox.noAccountsMessage")}</p>
          <button
            class="primary-btn"
            on:click={(e) => {
              e.stopPropagation();
              handleCreateAccount();
            }}
          >
            {$_("winbox.createAccount")}
          </button>
        </div>
      {:else}
        <div class="accounts-count">
          {filteredAccounts.length} account{filteredAccounts.length !== 1
            ? "s"
            : ""}
        </div>

        <table class="accounts-table">
          <thead>
            <tr>
              <th class="col-expand" />
              <th class="col-name">{$_("common.name")}</th>
              <th class="col-peer">{$_("webssh.peer")}</th>
              <th class="col-router">{$_("winbox.routerIP")}</th>
              <th class="col-auth">Auth</th>
              <th class="col-status">{$_("common.status")}</th>
              <th class="col-actions">{$_("common.actions")}</th>
            </tr>
          </thead>
          <tbody>
            {#each sortedGroupNames as groupName}
              {#if groupByDevice}
                <tr class="group-header">
                  <td colspan="8">
                    <div class="group-title-tag">
                      <span class="tag-icon">#</span>
                      <span class="tag-name">{groupName}</span>
                      <span class="tag-count"
                        >{accountGroups[groupName].length}</span
                      >
                    </div>
                  </td>
                </tr>
              {/if}
              {#each accountGroups[groupName] as account (account.id)}
                {@const sharedMeta = getAccountSharedMeta(account)}
                <tr
                  class="account-row hover:bg-gray-200 cursor-pointer"
                  class:expanded={expandedRows[account.id]}
                  on:click={(e) => {
                    e.stopPropagation();
                    toggleRow(account.id, account);
                  }}
                >
                  <td class="col-expand">
                    <button
                      class="expand-btn"
                      class:expanded={expandedRows[account.id]}
                      on:click={(e) => {
                        e.stopPropagation();
                        toggleRow(account.id, account);
                      }}
                      title={expandedRows[account.id] ? "Collapse" : "Expand"}
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
                      <span class="status-dot" class:active={account.enabled} />
                      <span class="account-name"
                        >{account.name || "Unnamed"}</span
                      >
                      {#if sharedMeta.isShared}
                        <span class="shared-badge" title="Shared by {sharedMeta.ownerName || 'another account'}">
                          shared{sharedMeta.ownerName ? ` · ${sharedMeta.ownerName}` : ""}
                        </span>
                      {/if}
                    </div>
                  </td>
                  <td class="col-peer">
                    <span class="peer-name" title={account.peer_id}
                      >{getPeerName(account.peer_id)}</span
                    >
                  </td>
                  <td class="col-router">
                    {#if account.router_ip}
                      <code
                        class="router-ip copyable"
                        on:click={(e) => {
                          e.stopPropagation();
                          copyRouterIP(account.router_ip);
                        }}
                        on:keypress={(e) =>
                          e.key === "Enter" &&
                          (e.stopPropagation(),
                          copyRouterIP(account.router_ip))}
                        tabindex="0"
                        role="button"
                        title="Click to copy">{account.router_ip}</code
                      >
                    {:else}
                      <code class="router-ip">N/A</code>
                    {/if}
                  </td><td class="col-auth">
                    <div class="auth-buttons">
                      <button
                        class="icon-btn"
                        class:copied={copiedId === account.id + "-access"}
                        on:click={(e) => {
                          e.stopPropagation();
                          copyToken(
                            account.access_token || "",
                            account.id + "-access"
                          );
                        }}
                        title="Copy Username"
                      >
                        {#if copiedId === account.id + "-access"}
                          <Icon name="check" size={24} />
                        {:else}
                          <Icon name="clipboard-user" size={24} />
                        {/if}
                      </button>
                      <button
                        class="icon-btn"
                        class:copied={copiedId === account.id + "-password"}
                        on:click={(e) => {
                          e.stopPropagation();
                          copyToken(
                            account.password_token || "",
                            account.id + "-password"
                          );
                        }}
                        title="Copy Password"
                      >
                        {#if copiedId === account.id + "-password"}
                          <Icon name="check" size={24} />
                        {:else}
                          <Icon name="clipboard-key" size={24} />
                        {/if}
                      </button>
                    </div>
                  </td>
                  <td class="col-status">
                    <span class="status-badge" class:active={account.enabled}>
                      {account.enabled
                        ? $_("common.active")
                        : $_("common.disabled")}
                    </span>
                  </td>
                  <td class="col-actions">
                    <div class="action-buttons">
                      <button
                        class="action-btn"
                        on:click={(e) => {
                          e.stopPropagation();
                          handleToggleAccount(account);
                        }}
                        title={account.enabled
                          ? $_("common.disable")
                          : $_("common.enable")}
                      >
                        {#if account.enabled}
                          <svg
                            width="16"
                            height="16"
                            viewBox="0 0 24 24"
                            fill="none"
                            xmlns="http://www.w3.org/2000/svg"
                          >
                            <rect
                              x="6"
                              y="4"
                              width="4"
                              height="16"
                              rx="1"
                              fill="currentColor"
                            />
                            <rect
                              x="14"
                              y="4"
                              width="4"
                              height="16"
                              rx="1"
                              fill="currentColor"
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
                            <path d="M8 5V19L19 12L8 5Z" fill="currentColor" />
                          </svg>
                        {/if}
                      </button>
                      {#if !sharedMeta.isShared}
                        <button
                          class="action-btn danger"
                          on:click={(e) => {
                            e.stopPropagation();
                            handleDeleteAccount(account);
                          }}
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
                      {/if}
                    </div>
                  </td>
                </tr>

                <!-- Expandable Detail Row -->
                {#if expandedRows[account.id]}
                  <tr class="expanded-content">
                    <td colspan="8">
                      <div class="expanded-inner">
                        <div class="detail-grid">
                          <!-- Token Section -->
                          <div class="token-section">
                            <div class="section-label">Username</div>
                            <div class="token-row">
                              <input
                                type="text"
                                readonly
                                value={truncateToken(
                                  account.access_token || ""
                                )}
                                class="token-input"
                              />
                              <button
                                class="copy-btn cursor-pointer"
                                class:copied={copiedId ===
                                  account.id + "-access"}
                                on:click={() =>
                                  copyToken(
                                    account.access_token || "",
                                    account.id + "-access"
                                  )}
                              >
                                {#if copiedId === account.id + "-access"}
                                  <Icon name="check" size={20} />
                                {:else}
                                  <Icon name="clipboard-user" size={20} />
                                {/if}
                              </button>
                            </div>

                            <div
                              class="section-label"
                              style="margin-top: 12px;"
                            >
                              Password
                            </div>
                            <div class="token-row">
                              <input
                                type="text"
                                readonly
                                value={truncateToken(
                                  account.password_token || ""
                                )}
                                class="token-input"
                              />
                              <button
                                class="copy-btn"
                                class:copied={copiedId ===
                                  account.id + "-password"}
                                on:click={() =>
                                  copyToken(
                                    account.password_token || "",
                                    account.id + "-password"
                                  )}
                              >
                                {#if copiedId === account.id + "-password"}
                                  <Icon name="check" size={20} />
                                {:else}
                                  <Icon name="clipboard-key" size={20} />
                                {/if}
                              </button>
                            </div>

                            <!-- Editable Allowed IPs -->
                            <div class="allowed-ips-section">
                              <label
                                class="allowed-ips-label"
                                for="allowed-ips-{account.id}"
                                >Allowed Client IPs:</label
                              >
                              <div class="allowed-ips-row">
                                <input
                                  type="text"
                                  id="allowed-ips-{account.id}"
                                  bind:value={editableAllowedIps[account.id]}
                                  placeholder="e.g. 192.168.1.0/24, 10.0.0.0/8 (empty = all)"
                                  class="allowed-ips-input"
                                />
                                <button
                                  class="save-btn"
                                  class:saving={savingAllowedIps[account.id]}
                                  on:click={() =>
                                    handleSaveAllowedIps(account.id)}
                                  disabled={savingAllowedIps[account.id]}
                                >
                                  {savingAllowedIps[account.id]
                                    ? "Saving..."
                                    : "Save"}
                                </button>
                              </div>
                            </div>
                          </div>

                          <!-- Connection Guide -->
                          <div class="guide-section">
                            <div class="section-label">📖 Connection Guide</div>
                            <ol class="guide-steps">
                              <li>Open <strong>MikroTik Winbox</strong></li>
                              <li>
                                Connect to: <code
                                  class="copyable"
                                  on:click={copyEndpoint}
                                  on:keypress={(e) =>
                                    e.key === "Enter" && copyEndpoint()}
                                  tabindex="0"
                                  role="button"
                                  title="Click to copy">{serverAddr}</code
                                >
                              </li>
                              <li>
                                Login: <strong>Access Token</strong> (copy from above)
                              </li>
                              <li>
                                Password: <strong>Password Token</strong> (copy from
                                above)
                              </li>
                            </ol>
                          </div>
                        </div>
                      </div>
                    </td>
                  </tr>
                {/if}
              {/each}
            {/each}
          </tbody>
        </table>

        <!-- Mobile Winbox List -->
        <div class="mobile-winbox-list">
          {#each sortedGroupNames as groupName}
            {#if groupByDevice}
              <div class="group-header-mobile">
                <span class="tag-icon">#</span>
                <span class="tag-name">{groupName}</span>
                <span class="tag-count">{accountGroups[groupName].length}</span>
              </div>
            {/if}
            {#each accountGroups[groupName] as account (account.id)}
              {@const sharedMeta = getAccountSharedMeta(account)}
              <!-- svelte-ignore a11y-click-events-have-key-events -->
              <div
                class="winbox-mobile-card"
                class:expanded={expandedRows[account.id]}
              >
                <!-- svelte-ignore a11y-click-events-have-key-events -->
                <div
                  class="card-main"
                  on:click={(e) => {
                    e.stopPropagation();
                    toggleRow(account.id, account);
                  }}
                >
                  <div class="card-info">
                    <div
                      class="status-dot-mobile"
                      class:active={account.enabled}
                    />
                    <div class="name-box">
                      <span class="account-name"
                        >{account.name || "Unnamed"}</span
                      >
                      <span class="peer-name"
                        >{getPeerName(account.peer_id)}</span
                      >
                      {#if sharedMeta.isShared}
                        <span class="shared-badge">
                          shared{sharedMeta.ownerName
                            ? ` · ${sharedMeta.ownerName}`
                            : ""}
                        </span>
                      {/if}
                    </div>
                  </div>
                  <div class="card-meta">
                    <code class="ip-badge">{account.router_ip || "N/A"}</code>
                    <div
                      class="expand-icon"
                      class:expanded={expandedRows[account.id]}
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

                {#if expandedRows[account.id]}
                  <div
                    class="card-actions"
                    transition:scale={{ duration: 150, start: 0.95 }}
                  >
                    <button
                      class="action-btn"
                      on:click={(e) => {
                        e.stopPropagation();
                        handleToggleAccount(account);
                      }}
                    >
                      {#if account.enabled}
                        <svg
                          width="20"
                          height="20"
                          viewBox="0 0 24 24"
                          fill="none"
                          xmlns="http://www.w3.org/2000/svg"
                        >
                          <rect
                            x="6"
                            y="4"
                            width="4"
                            height="16"
                            rx="1"
                            fill="currentColor"
                          />
                          <rect
                            x="14"
                            y="4"
                            width="4"
                            height="16"
                            rx="1"
                            fill="currentColor"
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
                          <path d="M8 5V19L19 12L8 5Z" fill="currentColor" />
                        </svg>
                      {/if}
                    </button>
                    <button
                      class="action-btn"
                      class:copied={copiedId === account.id + "-access"}
                      on:click={(e) => {
                        e.stopPropagation();
                        copyToken(
                          account.access_token || "",
                          account.id + "-access"
                        );
                      }}
                    >
                      {#if copiedId === account.id + "-access"}
                        <Icon name="check" size={20} />
                      {:else}
                        <Icon name="clipboard-user" size={20} />
                      {/if}
                    </button>
                    <button
                      class="action-btn"
                      class:copied={copiedId === account.id + "-password"}
                      on:click={(e) => {
                        e.stopPropagation();
                        copyToken(
                          account.password_token || "",
                          account.id + "-password"
                        );
                      }}
                    >
                      {#if copiedId === account.id + "-password"}
                        <Icon name="check" size={20} />
                      {:else}
                        <Icon name="clipboard-key" size={20} />
                      {/if}
                    </button>
                    {#if !sharedMeta.isShared}
                      <button
                        class="action-btn danger"
                        style="color: #ef4444;"
                        on:click={(e) => {
                          e.stopPropagation();
                          handleDeleteAccount(account);
                        }}
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
                          <path
                            d="M3 6h18M19 6v14a2 2 0 01-2 2H7a2 2 0 01-2-2V6m3 0V4a2 2 0 012-2h4a2 2 0 012 2v2m-6 5v6m4-6v6"
                          />
                        </svg>
                      </button>
                    {/if}
                  </div>

                  <div
                    class="card-expanded-content"
                    on:click|stopPropagation={() => {}}
                  >
                    <div class="expanded-inner p-4">
                      <!-- Allowed IPs Section (Reused logic) -->
                      <div class="allowed-ips-section">
                        <label
                          for={"allowed-ips-" + account.id}
                          class="allowed-ips-label"
                        >
                          {$_("winbox.allowedIPs")}
                        </label>
                        <div class="allowed-ips-row">
                          <input
                            id={"allowed-ips-" + account.id}
                            type="text"
                            bind:value={editableAllowedIps[account.id]}
                            placeholder="0.0.0.0/0, ::/0"
                            class="allowed-ips-input"
                          />
                          <button
                            class="save-btn"
                            class:saving={savingAllowedIps[account.id]}
                            disabled={savingAllowedIps[account.id]}
                            on:click={() => handleSaveAllowedIps(account.id)}
                          >
                            {#if savingAllowedIps[account.id]}
                              {$_("common.saving")}...
                            {:else}
                              {$_("common.save")}
                            {/if}
                          </button>
                        </div>
                      </div>

                      <!-- Connection Guide (Reused logic) -->
                      <div class="guide-section mt-4">
                        <div class="section-label">
                          {$_("winbox.connectionGuide")}
                        </div>
                        <p class="guide-steps">
                          1. {$_("winbox.openWinbox")}
                          <br />
                          2. {$_("winbox.connectTo")}
                          <code
                            class="copyable"
                            on:click={copyEndpoint}
                            on:keypress={(e) =>
                              e.key === "Enter" && copyEndpoint()}
                            tabindex="0"
                            role="button"
                            title="Click to copy">{serverAddr}</code
                          >
                          <br />
                          3. {$_("winbox.useLogin")}
                          <strong
                            >your_tokens ({truncateToken(
                              account.access_token || "",
                              8
                            )})</strong
                          >
                        </p>
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
  .winbox-accounts {
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

  .winbox-accounts.maximized {
    position: fixed;
    top: 0 !important;
    left: 0 !important;
    width: 100vw !important;
    height: calc(100vh - 48px) !important;
    border-radius: 0;
  }

  .winbox-accounts.minimized {
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
    padding: 0px 16px;
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
    margin-top: 12px;
    align-items: center;
    justify-content: space-between;
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
    transition: border-color 0.2s, box-shadow 0.2s;
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
    padding: 6px;
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

  .icon-btn.active {
    background: rgb(var(--clrPrm) / 20%);
    border-color: rgb(var(--clrPrm));
    color: rgb(var(--clrPrm));
  }

  /* States */
  .error-message {
    padding: 12px 16px;
    background: rgba(239, 68, 68, 0.1);
    border: 1px solid rgba(239, 68, 68, 0.3);
    border-radius: 8px;
    color: #fca5a5;
  }

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

  @keyframes spin {
    to {
      transform: rotate(360deg);
    }
  }

  /* Accounts count */
  .accounts-count {
    font-size: 12px;
    color: rgb(var(--clr) / 50%);
  }

  /* Table */
  .accounts-table {
    width: 100%;
    border-collapse: collapse;
    font-size: 14px;
  }

  .accounts-table thead {
    position: sticky;
    top: 0;
    z-index: 1;
  }

  .accounts-table th {
    text-align: left;
    padding: 12px 8px;
    background: rgb(var(--bg3));
    color: rgb(var(--clr) / 70%);
    font-weight: 500;
    font-size: 12px;
    text-transform: uppercase;
    letter-spacing: 0.5px;
    border-bottom: 1px solid rgb(var(--clr) / 10%);
  }

  .accounts-table td {
    padding: 12px 8px;
    border-bottom: 1px solid rgb(var(--clr) / 10%);
    color: rgb(var(--clr));
  }

  .account-row {
    transition: background 0.2s;
  }

  .account-row:hover {
    background: rgb(var(--clr) / 3%);
  }

  .account-row.expanded {
    background: rgb(var(--clr) / 5%);
  }

  /* Columns */
  .col-expand {
    width: 40px;
  }
  .col-name {
    width: 25%;
  }
  .col-peer {
    width: 20%;
  }
  .col-router {
    width: 15%;
  }
  .col-auth {
    width: 100px;
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

  .account-name {
    font-weight: 500;
  }

  .peer-name {
    color: rgb(var(--clr) / 70%);
    font-size: 13px;
  }

  .router-ip {
    font-family: "Cascadia Code", "Fira Code", monospace;
    background: rgb(var(--bg3));
    padding: 4px 8px;
    border-radius: 4px;
    font-size: 12px;
  }

  .router-ip.copyable {
    cursor: pointer;
    transition: all 0.2s ease;
  }

  .router-ip.copyable:hover {
    background: rgb(var(--clrPrm) / 20%);
    color: rgb(var(--clrPrm));
  }

  /* Status badge */
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

  .status-badge {
    display: inline-block;
    padding: 4px 10px;
    border-radius: 12px;
    font-size: 12px;
    font-weight: 500;
    background: rgb(var(--bg3));
    color: rgb(var(--clr) / 70%);
  }

  .status-badge.active {
    background: rgba(34, 197, 94, 0.1);
    color: #22c55e;
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

  .action-btn.danger {
    color: #ef4444;
    border-color: rgba(239, 68, 68, 0.2);
  }

  .action-btn.danger:hover {
    background: rgba(239, 68, 68, 0.1);
    border-color: rgba(239, 68, 68, 0.4);
  }

  /* Auth buttons */
  .auth-buttons {
    display: flex;
    gap: 4px;
  }

  .icon-btn.xs {
    width: 24px;
    height: 24px;
    padding: 4px;
  }

  .icon-btn.copied {
    color: #22c55e;
    border-color: #22c55e;
    background: rgba(34, 197, 94, 0.1);
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

  /* Token section */
  .token-section {
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

  .token-row {
    display: flex;
    gap: 8px;
    align-items: center;
  }

  .token-input {
    flex: 1;
    font-family: "Cascadia Code", "Fira Code", monospace;
    background: rgb(var(--bg3));
    color: #10b981;
    border: none;
    padding: 10px 12px;
    border-radius: 6px;
    font-size: 13px;
  }

  .copy-btn {
    padding: 4px 8px;
    background: rgb(var(--bg2));
    border: none;
    border-radius: 6px;
    color: rgb(var(--clr));
    font-size: 13px;
    font-weight: 600;
    cursor: pointer;
    transition: all 0.2s;
    white-space: nowrap;
    svg {
      fill: rgb(var(--clr));
    }
  }

  .copy-btn:hover {
    background: rgb(var(--clr) / 25%);
    // transform: translateY(-1px);
  }

  .copy-btn.copied {
    background: #22c55e;
  }

  /* Allowed IPs section */
  .allowed-ips-section {
    margin-top: 16px;
  }

  .allowed-ips-label {
    display: block;
    color: rgb(var(--clr) / 50%);
    font-size: 12px;
    margin-bottom: 8px;
  }

  .allowed-ips-row {
    display: flex;
    gap: 8px;
    align-items: center;
  }

  .allowed-ips-input {
    flex: 1;
    font-family: "Cascadia Code", "Fira Code", monospace;
    background: rgb(var(--bg3));
    color: rgb(var(--clr));
    border: 1px solid rgb(var(--clr) / 10%);
    padding: 10px 12px;
    border-radius: 6px;
    font-size: 12px;
    transition: border-color 0.2s, box-shadow 0.2s;
  }

  .allowed-ips-input:focus {
    outline: none;
    border-color: rgb(var(--clrPrm));
    box-shadow: 0 0 0 3px rgb(var(--clrPrm) / 20%);
  }

  .allowed-ips-input::placeholder {
    color: rgb(var(--clr) / 50%);
  }

  .save-btn {
    padding: 10px 16px;
    background: #10b981;
    border: none;
    border-radius: 3px;
    color: white;
    font-size: 13px;
    font-weight: 600;
    cursor: pointer;
    transition: all 0.2s;
    white-space: nowrap;
  }
  .save-btn:disabled {
    opacity: 0.6;
    cursor: not-allowed;
  }

  .save-btn.saving {
    background: rgb(var(--clr) / 40%);
  }

  /* Guide section */
  .guide-section {
    background: rgb(var(--clrPrm) / 10%);
    border-radius: 8px;
    padding: 16px;
    border: 1px solid rgb(var(--clrPrm) / 20%);
  }

  .guide-section .section-label {
    color: rgb(var(--clrPrm));
  }

  .guide-steps {
    margin: 0;
    padding-left: 20px;
    color: rgb(var(--clr) / 90%);
    font-size: 13px;
    line-height: 1.8;
  }

  .guide-steps code {
    background: rgb(var(--bg2));
    padding: 2px 8px;
    border-radius: 4px;
    font-family: "Cascadia Code", "Fira Code", monospace;
    font-size: 12px;
  }

  .guide-steps code.copyable {
    cursor: pointer;
    transition: all 0.2s ease;
  }

  .guide-steps code.copyable:hover {
    background: rgb(var(--clrPrm) / 20%);
    color: rgb(var(--clrPrm));
  }

  .guide-steps strong {
    color: rgb(var(--clrPrm));
  }

  /* Responsive Mobile View */
  @media (max-width: 768px) {
    .accounts-table {
      display: none;
    }
    .mobile-winbox-list {
      display: flex;
      flex-direction: column;
      gap: 12px;
      padding-bottom: 24px;
    }
  }

  @media (min-width: 769px) {
    .mobile-winbox-list {
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

  .winbox-mobile-card {
    background: rgb(var(--bg3) / 50%);
    backdrop-filter: blur(20px);
    -webkit-backdrop-filter: blur(20px);
    border: 1px solid rgb(var(--clr) / 8%);
    border-radius: 16px;
    overflow: hidden;
    transition: all 0.2s cubic-bezier(0.4, 0, 0.2, 1), max-height 0.3s ease;
    box-shadow: 0 4px 12px rgb(0 0 0 / 5%);
    display: flex;
    flex-direction: column;
  }

  .winbox-mobile-card.expanded {
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
    box-shadow: 0 0 0 2px rgb(var(--bg1)), 0 0 8px rgba(34, 197, 94, 0.4);
  }

  .name-box {
    display: flex;
    flex-direction: column;
    gap: 2px;
  }

  .name-box .account-name {
    font-weight: 600;
    font-size: 15px;
    color: rgb(var(--clr));
  }

  .name-box .peer-name {
    font-size: 12px;
    color: rgb(var(--clr) / 50%);
  }

  .card-meta {
    display: flex;
    align-items: center;
    gap: 12px;
  }

  .ip-badge {
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

  .card-actions .action-btn.danger {
    background: rgba(239, 68, 68, 0.05);
    border-color: rgba(239, 68, 68, 0.2);
  }

  .card-expanded-content {
    border-top: 1px solid rgb(var(--clr) / 8%);
    background: rgb(var(--bg3) / 30%);
    animation: slideDown 0.2s ease-out;
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
    .winbox-accounts {
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

    .table-wrapper {
      overflow-x: auto;
    }

    .data-table {
      min-width: 600px;
    }

    .detail-grid {
      grid-template-columns: 1fr;
    }

    .expanded-content {
      padding: 12px;
    }

    .connection-guide {
      padding: 12px;
    }

    .guide-steps {
      gap: 8px;
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

    .data-table th,
    .data-table td {
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

  .group-header td {
    background: rgb(var(--bg3));
    padding: 8px 12px;
    font-weight: 600;
    color: rgb(var(--clr));
    border-bottom: 1px solid rgb(var(--clr) / 10%);
    border-top: 1px solid rgb(var(--clr) / 10%);
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
    background-color: #3b82f6; /* Blue 500 */
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
    background-color: #3b82f6; /* Blue 500 */
    color: white;
    padding: 4px 10px;
    font-weight: 700;
    display: flex;
    align-items: center;
    justify-content: center;
  }
</style>
