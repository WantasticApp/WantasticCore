<script lang="ts">
  import { draggable } from "@neodrag/svelte";
  import { scale } from "svelte/transition";
  import { onMount } from "svelte";
  import { Button } from "fluent-svelte";
  import Titlebar from "$components/shared/Titlebar.svelte";
  import {
    appZIndexes,
    bringToFront,
    minimizedApps,
    activeThing,
    openedApps,
  } from "$store/store";
  import { isMobile } from "$store/ui";
  import { adminStore, type AdminTenant } from "$store/admin";
  import { toasts } from "$store/toast";

  // ─────────────────────────────────────────────────────────────────────
  // Window chrome — same shape as the other apps (Peers, Account, …) so the
  // app behaves like a first-class window in the desktop (draggable,
  // maximize, minimize, close).
  // ─────────────────────────────────────────────────────────────────────
  let isMaximized = false;
  let isMinimized = false;

  function handleReduce() {
    isMinimized = true;
    if (!$minimizedApps.includes("Admin")) {
      $minimizedApps = [...$minimizedApps, "Admin"];
    }
    if ($activeThing === "Admin") $activeThing = "";
  }
  function handleMaximize() {
    isMaximized = !isMaximized;
  }
  function handleClose() {
    $openedApps = $openedApps.filter((a) => a !== "Admin");
    $minimizedApps = $minimizedApps.filter((a) => a !== "Admin");
    if ($activeThing === "Admin") $activeThing = "";
  }

  $: if ($activeThing === "Admin" && isMinimized) {
    isMinimized = false;
    $minimizedApps = $minimizedApps.filter((a) => a !== "Admin");
  }

  $: zIndex = $appZIndexes["Admin"] || 100;

  // ─────────────────────────────────────────────────────────────────────
  // Tab state
  // ─────────────────────────────────────────────────────────────────────
  let activeTab: "tenants" | "create" = "tenants";

  // ─────────────────────────────────────────────────────────────────────
  // Data
  // ─────────────────────────────────────────────────────────────────────
  let tenants: AdminTenant[] = [];
  let loading = false;
  let searchQuery = "";

  $: filteredTenants = searchQuery.trim()
    ? tenants.filter((t) => {
        const q = searchQuery.toLowerCase();
        return (
          t.email.toLowerCase().includes(q) ||
          t.full_name.toLowerCase().includes(q) ||
          t.id.toLowerCase().includes(q)
        );
      })
    : tenants;

  $: adminCount = tenants.filter((t) => t.is_admin).length;

  // ─────────────────────────────────────────────────────────────────────
  // Create-tenant form
  // ─────────────────────────────────────────────────────────────────────
  let formEmail = "";
  let formName = "";
  let formPassword = "";
  let formMaxPeers = 30;
  let formIsAdmin = false;
  let formSubmitting = false;

  // ─────────────────────────────────────────────────────────────────────
  // Inline edit + reset modal state
  // ─────────────────────────────────────────────────────────────────────
  let editingId: string | null = null;
  let editingMaxPeers = 0;

  let resetTenantId: string | null = null;
  let resetTenantEmail = "";
  let resetPassword = "";
  let resetSubmitting = false;

  onMount(refresh);

  async function refresh() {
    loading = true;
    try {
      tenants = await adminStore.listTenants();
    } catch (e: any) {
      toasts.error(`Failed to load tenants: ${e?.message ?? e}`);
    } finally {
      loading = false;
    }
  }

  async function submitCreate() {
    if (!formEmail || !formName || !formPassword) {
      toasts.error("Email, full name, and password are required");
      return;
    }
    formSubmitting = true;
    try {
      await adminStore.createTenant({
        email: formEmail,
        full_name: formName,
        password: formPassword,
        max_peers: formMaxPeers,
        is_admin: formIsAdmin,
      });
      toasts.success(`Tenant ${formEmail} created`);
      formEmail = "";
      formName = "";
      formPassword = "";
      formMaxPeers = 30;
      formIsAdmin = false;
      await refresh();
      activeTab = "tenants";
    } catch (e: any) {
      toasts.error(`Create failed: ${e?.message ?? e}`);
    } finally {
      formSubmitting = false;
    }
  }

  async function toggleAdmin(t: AdminTenant) {
    const next = !t.is_admin;
    if (!confirm(`${next ? "Promote" : "Demote"} ${t.email}?`)) return;
    try {
      await adminStore.setAdmin(t.id, next);
      toasts.success(`${t.email} ${next ? "promoted to" : "demoted from"} admin`);
      await refresh();
    } catch (e: any) {
      toasts.error(`Failed: ${e?.message ?? e}`);
    }
  }

  async function deleteTenant(t: AdminTenant) {
    if (
      !confirm(
        `Permanently delete ${t.email}? This removes the overlay account and all peers.`,
      )
    )
      return;
    try {
      await adminStore.deleteTenant(t.id);
      toasts.success(`Tenant ${t.email} deleted`);
      await refresh();
    } catch (e: any) {
      toasts.error(`Delete failed: ${e?.message ?? e}`);
    }
  }

  function startEdit(t: AdminTenant) {
    editingId = t.id;
    editingMaxPeers = t.max_peers;
  }

  async function commitEdit() {
    if (!editingId) return;
    try {
      await adminStore.setMaxPeers(editingId, editingMaxPeers);
      toasts.success("Max peers updated");
      editingId = null;
      await refresh();
    } catch (e: any) {
      toasts.error(`Update failed: ${e?.message ?? e}`);
    }
  }

  function startReset(t: AdminTenant) {
    resetTenantId = t.id;
    resetTenantEmail = t.email;
    resetPassword = "";
  }

  async function commitReset() {
    if (!resetTenantId || !resetPassword) {
      toasts.error("Password is required");
      return;
    }
    resetSubmitting = true;
    try {
      await adminStore.setPassword(resetTenantId, resetPassword);
      toasts.success(`Password reset for ${resetTenantEmail}`);
      resetTenantId = null;
      resetPassword = "";
    } catch (e: any) {
      toasts.error(`Reset failed: ${e?.message ?? e}`);
    } finally {
      resetSubmitting = false;
    }
  }

  function statusClass(status: string): string {
    switch (status) {
      case "active":
        return "badge-accepted";
      case "suspended":
        return "badge-pending";
      case "deleted":
        return "badge-revoked";
      default:
        return "";
    }
  }
</script>

<!-- svelte-ignore a11y-no-static-element-interactions -->
<div
  class="admin-window activeShadow"
  class:maximized={isMaximized || $isMobile}
  class:minimized={isMinimized}
  style:z-index={zIndex}
  on:mousedown={() => bringToFront("Admin")}
  on:touchstart={() => bringToFront("Admin")}
  use:draggable={{
    handle: ".title-bar",
    disabled: isMaximized || $isMobile,
    bounds: "body",
  }}
  transition:scale={{ duration: 200 }}
>
  <Titlebar
    title="Admin"
    appName="Admin"
    canMaximize={!$isMobile}
    canReduce={true}
    canClose={true}
    on:reduce={handleReduce}
    on:maximize={handleMaximize}
    on:close={handleClose}
  />

  <div class="tabs">
    <button
      class="tab"
      class:active={activeTab === "tenants"}
      on:click={() => (activeTab = "tenants")}
    >
      Tenants
      <span class="tab-count">{tenants.length}</span>
    </button>
    <button
      class="tab"
      class:active={activeTab === "create"}
      on:click={() => (activeTab = "create")}
    >
      Create tenant
    </button>
  </div>

  <div class="content">
    {#if activeTab === "tenants"}
      <div class="toolbar">
        <input
          type="search"
          class="search"
          placeholder="Search by email, name, or ID…"
          bind:value={searchQuery}
        />
        <div class="toolbar-stats">
          <span class="stat-pill">{tenants.length} total</span>
          <span class="stat-pill admin">{adminCount} admin</span>
        </div>
        <Button
          variant="standard"
          on:click={refresh}
          disabled={loading}
        >
          {loading ? "Loading…" : "Refresh"}
        </Button>
      </div>

      {#if loading && tenants.length === 0}
        <div class="loading-state">
          <div class="spinner"></div>
          <p>Loading tenants…</p>
        </div>
      {:else if tenants.length === 0}
        <div class="empty-state">
          <img src="img/icon/Admin.svg" alt="" width="64" height="64" />
          <h3>No tenants yet</h3>
          <p>Create the first tenant to start onboarding users.</p>
          <Button
            variant="accent"
            on:click={() => (activeTab = "create")}
          >
            Create tenant
          </Button>
        </div>
      {:else if filteredTenants.length === 0}
        <div class="empty-state">
          <p class="muted">No tenants match "{searchQuery}".</p>
        </div>
      {:else}
        <div class="table-wrapper">
          <table class="tenants-table">
            <thead>
              <tr>
                <th>Tenant</th>
                <th>Status</th>
                <th>Role</th>
                <th>Peers</th>
                <th>Max</th>
                <th>Actions</th>
              </tr>
            </thead>
            <tbody>
              {#each filteredTenants as t (t.id)}
                <tr>
                  <td>
                    <div class="cell-name">{t.full_name}</div>
                    <div class="cell-sub">{t.email}</div>
                  </td>
                  <td>
                    <span class="badge {statusClass(t.status)}">
                      {t.status}
                    </span>
                  </td>
                  <td>
                    {#if t.is_admin}
                      <span class="badge badge-admin">SUPER-ADMIN</span>
                    {:else}
                      <span class="badge badge-tenant">tenant</span>
                    {/if}
                  </td>
                  <td>
                    <span class="peer-count">{t.peer_count}</span>
                  </td>
                  <td>
                    {#if editingId === t.id}
                      <span class="edit-row">
                        <input
                          type="number"
                          class="inline-input"
                          bind:value={editingMaxPeers}
                          min="1"
                          on:keydown={(e) =>
                            e.key === "Enter" && commitEdit()}
                        />
                        <button class="icon-btn ok" on:click={commitEdit}>✓</button>
                        <button
                          class="icon-btn cancel"
                          on:click={() => (editingId = null)}>✕</button
                        >
                      </span>
                    {:else}
                      <button class="link" on:click={() => startEdit(t)}>
                        {t.max_peers}
                      </button>
                    {/if}
                  </td>
                  <td>
                    <div class="row-actions">
                      <button
                        class="action-btn"
                        on:click={() => toggleAdmin(t)}
                        title={t.is_admin ? "Demote" : "Promote to admin"}
                      >
                        {t.is_admin ? "Demote" : "Promote"}
                      </button>
                      <button
                        class="action-btn"
                        on:click={() => startReset(t)}
                        title="Reset password"
                      >
                        Reset pw
                      </button>
                      <button
                        class="action-btn danger"
                        on:click={() => deleteTenant(t)}
                        title="Delete tenant"
                      >
                        Delete
                      </button>
                    </div>
                  </td>
                </tr>
              {/each}
            </tbody>
          </table>
        </div>
      {/if}
    {/if}

    {#if activeTab === "create"}
      <div class="section-header">
        <div class="section-title">
          <h3>Create tenant</h3>
          <p class="muted">
            New tenants get a dedicated overlay subnet and the configured peer
            cap. Toggle the super-admin flag only for trusted operators.
          </p>
        </div>
      </div>

      <form on:submit|preventDefault={submitCreate} class="create-form">
        <div class="form-row">
          <label
            >Email
            <input
              type="email"
              bind:value={formEmail}
              placeholder="user@example.com"
              required
            />
          </label>
          <label
            >Full name
            <input
              type="text"
              bind:value={formName}
              placeholder="Jane Smith"
              required
            />
          </label>
        </div>

        <div class="form-row">
          <label
            >Password
            <input type="password" bind:value={formPassword} required />
          </label>
          <label
            >Max peers
            <input type="number" bind:value={formMaxPeers} min="1" />
          </label>
        </div>

        <label class="perm-toggle">
          <input type="checkbox" bind:checked={formIsAdmin} />
          <span class="perm-label-row">
            Super-admin
            <span class="hint">
              — full management access to all tenants and devices
            </span>
          </span>
        </label>

        <div class="form-actions">
          <Button
            variant="standard"
            on:click={() => (activeTab = "tenants")}
          >
            Cancel
          </Button>
          <Button
            variant="accent"
            type="submit"
            disabled={formSubmitting}
          >
            {formSubmitting ? "Creating…" : "Create tenant"}
          </Button>
        </div>
      </form>
    {/if}
  </div>
</div>

{#if resetTenantId}
  <!-- svelte-ignore a11y-no-static-element-interactions a11y-click-events-have-key-events -->
  <div
    class="modal-backdrop"
    on:click={() => (resetTenantId = null)}
    role="presentation"
  >
    <!-- svelte-ignore a11y-no-static-element-interactions -->
    <div
      class="modal"
      on:click|stopPropagation
      role="dialog"
      aria-modal="true"
    >
      <h3>Reset password</h3>
      <p class="muted">
        New password for <strong>{resetTenantEmail}</strong>. All active
        sessions for this tenant will be invalidated.
      </p>
      <input
        type="password"
        bind:value={resetPassword}
        placeholder="New password"
        autofocus
      />
      <div class="form-actions">
        <Button
          variant="standard"
          on:click={() => (resetTenantId = null)}
        >
          Cancel
        </Button>
        <Button
          variant="accent"
          disabled={resetSubmitting || !resetPassword}
          on:click={commitReset}
        >
          {resetSubmitting ? "Resetting…" : "Reset"}
        </Button>
      </div>
    </div>
  </div>
{/if}

<style>
  /* ─── Window chrome ───────────────────────────────────────────────── */
  .admin-window {
    position: fixed;
    top: 60px;
    left: calc(50% - 480px);
    width: 960px;
    height: 640px;
    display: flex;
    flex-direction: column;
    background: rgb(var(--bg, 28 32 42));
    border: 1px solid rgba(255, 255, 255, 0.08);
    border-radius: 8px;
    overflow: hidden;
    color: rgb(var(--clr, 230 232 240));
    font-family:
      "Segoe UI Variable",
      "Segoe UI",
      Tahoma,
      sans-serif;
  }
  .admin-window.maximized {
    top: 0;
    left: 0;
    width: 100vw;
    height: calc(100vh - 48px);
    border-radius: 0;
  }
  .admin-window.minimized {
    display: none;
  }

  /* ─── Tabs ────────────────────────────────────────────────────────── */
  .tabs {
    display: flex;
    gap: 0;
    padding: 0 16px;
    border-bottom: 1px solid rgba(255, 255, 255, 0.08);
    flex-shrink: 0;
  }
  .tab {
    background: none;
    border: none;
    color: inherit;
    padding: 12px 16px;
    cursor: pointer;
    font-size: 14px;
    font-weight: 500;
    border-bottom: 2px solid transparent;
    display: inline-flex;
    align-items: center;
    gap: 8px;
    opacity: 0.7;
    transition: opacity 0.15s, border-color 0.15s;
  }
  .tab:hover {
    opacity: 1;
  }
  .tab.active {
    opacity: 1;
    border-bottom-color: rgb(var(--clrPrm, 0 103 192));
  }
  .tab-count {
    background: rgba(255, 255, 255, 0.08);
    padding: 1px 8px;
    border-radius: 999px;
    font-size: 12px;
    font-weight: 600;
  }

  /* ─── Content ─────────────────────────────────────────────────────── */
  .content {
    flex: 1;
    overflow-y: auto;
    padding: 20px 24px;
  }

  /* ─── Toolbar ─────────────────────────────────────────────────────── */
  .toolbar {
    display: flex;
    align-items: center;
    gap: 12px;
    margin-bottom: 16px;
  }
  .search {
    flex: 1;
    background: rgba(255, 255, 255, 0.06);
    border: 1px solid rgba(255, 255, 255, 0.12);
    color: inherit;
    padding: 7px 12px;
    border-radius: 6px;
    font-size: 13px;
  }
  .search:focus {
    outline: none;
    border-color: rgba(124, 106, 247, 0.6);
  }
  .toolbar-stats {
    display: flex;
    gap: 6px;
  }
  .stat-pill {
    background: rgba(255, 255, 255, 0.06);
    padding: 4px 10px;
    border-radius: 999px;
    font-size: 12px;
    font-weight: 500;
    opacity: 0.8;
  }
  .stat-pill.admin {
    background: rgba(124, 106, 247, 0.18);
    color: rgb(180, 165, 255);
    opacity: 1;
  }

  /* ─── Section header (Create tab) ─────────────────────────────────── */
  .section-header {
    margin-bottom: 18px;
  }
  .section-header h3 {
    margin: 0 0 4px;
    font-size: 16px;
    font-weight: 600;
  }
  .muted {
    margin: 4px 0;
    font-size: 13px;
    opacity: 0.7;
    line-height: 1.5;
  }

  /* ─── States ──────────────────────────────────────────────────────── */
  .loading-state,
  .empty-state {
    display: flex;
    flex-direction: column;
    align-items: center;
    justify-content: center;
    padding: 48px 16px;
    text-align: center;
    gap: 12px;
  }
  .empty-state img {
    opacity: 0.4;
  }
  .empty-state h3 {
    margin: 0;
    font-size: 18px;
    font-weight: 600;
  }
  .empty-state p {
    margin: 0;
    opacity: 0.7;
    font-size: 14px;
  }
  .spinner {
    width: 32px;
    height: 32px;
    border: 3px solid rgba(255, 255, 255, 0.1);
    border-top-color: rgb(var(--clrPrm, 0 103 192));
    border-radius: 50%;
    animation: spin 0.8s linear infinite;
  }
  @keyframes spin {
    to {
      transform: rotate(360deg);
    }
  }

  /* ─── Table ───────────────────────────────────────────────────────── */
  .table-wrapper {
    background: rgba(255, 255, 255, 0.02);
    border: 1px solid rgba(255, 255, 255, 0.06);
    border-radius: 8px;
    overflow: hidden;
  }
  .tenants-table {
    width: 100%;
    border-collapse: collapse;
    font-size: 13px;
  }
  .tenants-table thead {
    background: rgba(255, 255, 255, 0.04);
  }
  .tenants-table th {
    text-align: left;
    padding: 10px 14px;
    font-weight: 600;
    font-size: 11px;
    text-transform: uppercase;
    letter-spacing: 0.5px;
    opacity: 0.7;
    border-bottom: 1px solid rgba(255, 255, 255, 0.06);
  }
  .tenants-table td {
    padding: 12px 14px;
    border-bottom: 1px solid rgba(255, 255, 255, 0.04);
    vertical-align: middle;
  }
  .tenants-table tr:hover td {
    background: rgba(255, 255, 255, 0.02);
  }
  .tenants-table tr:last-child td {
    border-bottom: none;
  }
  .cell-name {
    font-weight: 500;
    color: rgb(var(--clr, 230 232 240));
  }
  .cell-sub {
    font-size: 12px;
    opacity: 0.6;
    margin-top: 2px;
  }
  .peer-count {
    font-variant-numeric: tabular-nums;
    font-weight: 500;
  }

  /* ─── Badges ──────────────────────────────────────────────────────── */
  .badge {
    display: inline-block;
    padding: 2px 8px;
    border-radius: 999px;
    font-size: 11px;
    font-weight: 600;
    text-transform: uppercase;
    letter-spacing: 0.4px;
  }
  .badge-accepted {
    background: rgba(34, 197, 94, 0.18);
    color: rgb(134, 239, 172);
  }
  .badge-pending {
    background: rgba(245, 158, 11, 0.18);
    color: rgb(252, 211, 77);
  }
  .badge-revoked {
    background: rgba(239, 68, 68, 0.18);
    color: rgb(252, 165, 165);
  }
  .badge-admin {
    background: rgba(124, 106, 247, 0.25);
    color: rgb(196, 181, 253);
  }
  .badge-tenant {
    background: rgba(255, 255, 255, 0.08);
    color: rgba(255, 255, 255, 0.7);
    text-transform: none;
    letter-spacing: 0;
  }

  /* ─── Inline edit ─────────────────────────────────────────────────── */
  .edit-row {
    display: inline-flex;
    gap: 4px;
    align-items: center;
  }
  .inline-input {
    width: 60px;
    background: rgba(255, 255, 255, 0.08);
    border: 1px solid rgba(255, 255, 255, 0.18);
    color: inherit;
    padding: 4px 6px;
    border-radius: 4px;
    font-size: 13px;
  }
  .icon-btn {
    width: 22px;
    height: 22px;
    border: 1px solid rgba(255, 255, 255, 0.12);
    background: rgba(255, 255, 255, 0.05);
    color: inherit;
    border-radius: 4px;
    cursor: pointer;
    display: inline-flex;
    align-items: center;
    justify-content: center;
    font-size: 12px;
  }
  .icon-btn.ok {
    border-color: rgba(34, 197, 94, 0.4);
    color: rgb(134, 239, 172);
  }
  .icon-btn.cancel {
    border-color: rgba(239, 68, 68, 0.4);
    color: rgb(252, 165, 165);
  }

  /* ─── Row actions ─────────────────────────────────────────────────── */
  .row-actions {
    display: flex;
    gap: 6px;
    flex-wrap: wrap;
  }
  .action-btn {
    background: rgba(255, 255, 255, 0.06);
    border: 1px solid rgba(255, 255, 255, 0.12);
    color: inherit;
    padding: 4px 10px;
    border-radius: 4px;
    font-size: 12px;
    cursor: pointer;
    transition: background 0.15s;
  }
  .action-btn:hover {
    background: rgba(255, 255, 255, 0.12);
  }
  .action-btn.danger {
    color: rgb(252, 165, 165);
    border-color: rgba(239, 68, 68, 0.3);
  }
  .action-btn.danger:hover {
    background: rgba(239, 68, 68, 0.18);
  }
  .link {
    background: none;
    border: none;
    color: rgb(130, 180, 255);
    cursor: pointer;
    font-size: 13px;
    padding: 0;
    text-decoration: underline;
    text-decoration-style: dotted;
    text-underline-offset: 3px;
  }

  /* ─── Create form ─────────────────────────────────────────────────── */
  .create-form {
    display: flex;
    flex-direction: column;
    gap: 14px;
    max-width: 640px;
  }
  .form-row {
    display: grid;
    grid-template-columns: 1fr 1fr;
    gap: 14px;
  }
  .create-form label {
    display: flex;
    flex-direction: column;
    font-size: 12px;
    opacity: 0.85;
    gap: 6px;
  }
  .create-form input[type="text"],
  .create-form input[type="email"],
  .create-form input[type="password"],
  .create-form input[type="number"] {
    background: rgba(255, 255, 255, 0.06);
    border: 1px solid rgba(255, 255, 255, 0.12);
    color: inherit;
    padding: 8px 10px;
    border-radius: 6px;
    font-size: 14px;
  }
  .create-form input:focus {
    outline: none;
    border-color: rgba(124, 106, 247, 0.6);
  }
  .perm-toggle {
    display: flex;
    flex-direction: row !important;
    align-items: center;
    gap: 10px;
    padding: 10px 12px;
    background: rgba(255, 255, 255, 0.03);
    border: 1px solid rgba(255, 255, 255, 0.08);
    border-radius: 6px;
    cursor: pointer;
  }
  .perm-label-row {
    font-size: 13px;
    opacity: 1;
  }
  .hint {
    opacity: 0.6;
    font-size: 12px;
  }
  .form-actions {
    display: flex;
    gap: 8px;
    justify-content: flex-end;
    margin-top: 4px;
  }

  /* ─── Modal ───────────────────────────────────────────────────────── */
  .modal-backdrop {
    position: fixed;
    inset: 0;
    background: rgba(0, 0, 0, 0.6);
    backdrop-filter: blur(4px);
    display: flex;
    align-items: center;
    justify-content: center;
    z-index: 9999;
  }
  .modal {
    background: rgb(var(--bg, 28 32 42));
    border: 1px solid rgba(255, 255, 255, 0.12);
    border-radius: 10px;
    padding: 24px;
    width: 400px;
    box-shadow: 0 20px 50px rgba(0, 0, 0, 0.5);
  }
  .modal h3 {
    margin: 0 0 6px 0;
    font-size: 16px;
    font-weight: 600;
  }
  .modal input {
    width: 100%;
    background: rgba(255, 255, 255, 0.06);
    border: 1px solid rgba(255, 255, 255, 0.18);
    color: inherit;
    padding: 8px 10px;
    border-radius: 6px;
    margin: 12px 0;
    font-size: 14px;
  }
  .modal input:focus {
    outline: none;
    border-color: rgba(124, 106, 247, 0.6);
  }

  /* ─── Mobile overrides ───────────────────────────────────────────────
     The window already maximizes on $isMobile, but the inner layout was
     authored for a desktop 960×640 frame. On narrow viewports the form
     row's `1fr 1fr` grid and the right-aligned action row both push
     content past the viewport edge ("Create te…", "and devic…" clips
     in the user's screenshot). These breakpoints reflow it cleanly. */
  @media (max-width: 720px) {
    .content {
      padding: 12px 14px;
    }
    .toolbar {
      flex-wrap: wrap;
      gap: 8px;
    }
    .toolbar .search {
      flex: 1 1 100%;
    }
    .tabs {
      padding: 0 8px;
      overflow-x: auto;
    }
    .tab {
      padding: 12px 10px;
      font-size: 13px;
      flex-shrink: 0;
    }
    .form-row {
      grid-template-columns: 1fr;
    }
    .create-form {
      max-width: 100%;
    }
    .form-actions {
      flex-wrap: wrap;
      gap: 8px;
    }
    .form-actions button {
      flex: 1 1 auto;
      min-width: 0;
    }
  }
</style>
