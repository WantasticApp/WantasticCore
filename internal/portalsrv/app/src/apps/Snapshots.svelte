<script lang="ts">
  import { draggable } from "@neodrag/svelte";
  import { scale } from "svelte/transition";
  import { onMount } from "svelte";
  import Titlebar from "$components/shared/Titlebar.svelte";
  import { snapshotStore, type DeviceSnapshot } from "$store/snapshot";
  import { peerStore } from "$store/peer";
  import { openedApps, activeThing, appZIndexes, bringToFront } from "$store/store";
  import { toasts } from "$store/toast";

  let isMaximized = false;
  let isMinimized = false;

  $: zIndex = $appZIndexes["Snapshots"] || 100;
  $: if ($activeThing === "Snapshots" && isMinimized) isMinimized = false;
  $: if ($activeThing === "Snapshots") bringToFront("Snapshots");

  function handleFocus() {
    $activeThing = "Snapshots";
    bringToFront("Snapshots");
  }

  // Create snapshot dialog
  let showCreate = false;
  let createPeerId = "";
  let createName = "";
  let createProtocol = "wusp";

  // Provision dialog
  let showProvision = false;
  let provisionPeerId = "";
  let provisionSnapId = "";

  // Rename dialog
  let showRename = false;
  let renameId = "";
  let renameName = "";

  $: peers = $peerStore.peers ?? [];
  $: snapshots = $snapshotStore.snapshots;
  $: isLoading = $snapshotStore.isLoading;
  $: isSaving = $snapshotStore.isSaving;
  $: isProvisioning = $snapshotStore.isProvisioning;
  $: storeError = $snapshotStore.error;

  onMount(async () => {
    await Promise.all([snapshotStore.list("wusp"), peerStore.listPeers()]);
  });

  function handleClose() {
    $openedApps = $openedApps.filter((a) => a !== "Snapshots");
  }

  function formatDate(unix: number): string {
    if (!unix) return "—";
    return new Date(unix * 1000).toLocaleString();
  }

  async function handleCreate() {
    if (!createPeerId || !createName.trim()) {
      toasts.error("Select a peer and enter a name");
      return;
    }
    const snap = await snapshotStore.create(createPeerId, createName.trim(), createProtocol);
    if (snap) {
      toasts.success(`Snapshot "${snap.name}" created`);
      showCreate = false;
      createName = "";
      createPeerId = "";
    } else {
      toasts.error($snapshotStore.error || "Failed to create snapshot");
    }
  }

  async function handleDelete(snap: DeviceSnapshot) {
    if (!confirm(`Delete snapshot "${snap.name}"?`)) return;
    const ok = await snapshotStore.delete(snap.id);
    if (ok) toasts.success("Snapshot deleted");
    else toasts.error($snapshotStore.error || "Failed to delete snapshot");
  }

  function openRename(snap: DeviceSnapshot) {
    renameId = snap.id;
    renameName = snap.name;
    showRename = true;
  }

  async function handleRename() {
    if (!renameName.trim()) return;
    const ok = await snapshotStore.update(renameId, renameName.trim());
    if (ok) {
      toasts.success("Snapshot renamed");
      showRename = false;
    } else {
      toasts.error($snapshotStore.error || "Rename failed");
    }
  }

  function openProvision(snap: DeviceSnapshot) {
    provisionSnapId = snap.id;
    provisionPeerId = "";
    showProvision = true;
  }

  async function handleProvision() {
    if (!provisionPeerId) {
      toasts.error("Select a target peer");
      return;
    }
    const ok = await snapshotStore.provision(provisionPeerId, provisionSnapId);
    if (ok) {
      toasts.success("Device provisioned successfully");
      showProvision = false;
    } else {
      toasts.error($snapshotStore.error || "Provisioning failed");
    }
  }
</script>

<div
  class="snapshots-window"
  style="z-index: {zIndex};"
  class:maximized={isMaximized}
  class:minimized={isMinimized}
  use:draggable={{ handle: ".titlebar", disabled: isMaximized }}
  transition:scale={{ duration: 150, start: 0.95 }}
  on:mousedown={handleFocus}
  role="dialog"
  aria-label="Device Snapshots"
>
  <Titlebar
    appName="Snapshots"
    title="Device Snapshots"
    customClose={true}
    on:close={handleClose}
    on:maximize={() => (isMaximized = !isMaximized)}
    on:reduce={() => { isMinimized = true; $activeThing = ""; }}
  >
    <svg width="18" height="18" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2">
      <path d="M21 16V8a2 2 0 0 0-1-1.73l-7-4a2 2 0 0 0-2 0l-7 4A2 2 0 0 0 3 8v8a2 2 0 0 0 1 1.73l7 4a2 2 0 0 0 2 0l7-4A2 2 0 0 0 21 16z"/>
      <polyline points="3.27 6.96 12 12.01 20.73 6.96"/><line x1="12" y1="22.08" x2="12" y2="12"/>
    </svg>
    <span class="appName pl-2">Device Snapshots</span>
  </Titlebar>

  <div class="content">
    <!-- Header row -->
    <div class="toolbar">
      <span class="heading">Saved Snapshots</span>
      <button class="btn-primary" on:click={() => (showCreate = true)}>
        + New Snapshot
      </button>
    </div>

    {#if storeError}
      <div class="error-bar">{storeError}</div>
    {/if}

    {#if isLoading}
      <div class="loading">Loading…</div>
    {:else if snapshots.length === 0}
      <div class="empty">
        <p>No snapshots yet.</p>
        <p class="hint">Create a snapshot from a connected device to save its configuration.</p>
      </div>
    {:else}
      <div class="snap-list">
        {#each snapshots as snap (snap.id)}
          <div class="snap-card">
            <div class="snap-header">
              <span class="snap-name">{snap.name}</span>
              <span class="proto-badge">{snap.protocol}</span>
            </div>
            <div class="snap-meta">
              {#if snap.manufacturer}
                <span class="meta-item">
                  <svg width="12" height="12" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2">
                    <rect x="2" y="3" width="20" height="14" rx="2"/>
                  </svg>
                  {snap.manufacturer} {snap.product_class}
                </span>
              {/if}
              {#if snap.software_version}
                <span class="meta-item">
                  <svg width="12" height="12" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2">
                    <polyline points="16 18 22 12 16 6"/><polyline points="8 6 2 12 8 18"/>
                  </svg>
                  OS {snap.software_version}
                </span>
              {/if}
              {#if snap.serial_number}
                <span class="meta-item">S/N {snap.serial_number}</span>
              {/if}
              <span class="meta-item date">
                <svg width="12" height="12" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2">
                  <rect x="3" y="4" width="18" height="18" rx="2"/><line x1="16" y1="2" x2="16" y2="6"/><line x1="8" y1="2" x2="8" y2="6"/><line x1="3" y1="10" x2="21" y2="10"/>
                </svg>
                {formatDate(snap.created_at)}
              </span>
            </div>
            <div class="snap-actions">
              <button class="btn-sm" on:click={() => openRename(snap)}>Rename</button>
              <button class="btn-sm btn-accent" on:click={() => openProvision(snap)}>Provision</button>
              <button class="btn-sm btn-danger" on:click={() => handleDelete(snap)}>Delete</button>
            </div>
          </div>
        {/each}
      </div>
    {/if}
  </div>

  <!-- Create Snapshot Dialog -->
  {#if showCreate}
    <div class="dialog-overlay" on:click|self={() => (showCreate = false)} role="dialog">
      <div class="dialog">
        <h3>Create Snapshot</h3>
        <div class="form-group">
          <label>Peer (source device)</label>
          <select bind:value={createPeerId}>
            <option value="">— select peer —</option>
            {#each peers as p}
              <option value={p.id}>{p.name || p.id}</option>
            {/each}
          </select>
        </div>
        <div class="form-group">
          <label>Snapshot Name</label>
          <input type="text" bind:value={createName} placeholder="e.g. Office Router v1.2" />
        </div>
        <div class="form-group">
          <label>Protocol</label>
          <select bind:value={createProtocol}>
            <option value="wusp">WUSP</option>
          </select>
        </div>
        <div class="dialog-actions">
          <button class="btn-sm" on:click={() => (showCreate = false)}>Cancel</button>
          <button class="btn-sm btn-accent" on:click={handleCreate} disabled={isSaving}>
            {isSaving ? "Saving…" : "Create"}
          </button>
        </div>
      </div>
    </div>
  {/if}

  <!-- Rename Dialog -->
  {#if showRename}
    <div class="dialog-overlay" on:click|self={() => (showRename = false)} role="dialog">
      <div class="dialog">
        <h3>Rename Snapshot</h3>
        <div class="form-group">
          <label>Name</label>
          <input type="text" bind:value={renameName} />
        </div>
        <div class="dialog-actions">
          <button class="btn-sm" on:click={() => (showRename = false)}>Cancel</button>
          <button class="btn-sm btn-accent" on:click={handleRename} disabled={isSaving}>
            {isSaving ? "Saving…" : "Save"}
          </button>
        </div>
      </div>
    </div>
  {/if}

  <!-- Provision Dialog -->
  {#if showProvision}
    <div class="dialog-overlay" on:click|self={() => (showProvision = false)} role="dialog">
      <div class="dialog">
        <h3>Provision Device</h3>
        <p class="dialog-hint">Apply this snapshot's configuration to a live peer.</p>
        <div class="form-group">
          <label>Target Peer</label>
          <select bind:value={provisionPeerId}>
            <option value="">— select peer —</option>
            {#each peers as p}
              <option value={p.id}>{p.name || p.id}</option>
            {/each}
          </select>
        </div>
        <div class="dialog-actions">
          <button class="btn-sm" on:click={() => (showProvision = false)}>Cancel</button>
          <button class="btn-sm btn-accent" on:click={handleProvision} disabled={isProvisioning}>
            {isProvisioning ? "Provisioning…" : "Apply"}
          </button>
        </div>
      </div>
    </div>
  {/if}
</div>

<style>
  .snapshots-window {
    position: fixed;
    top: 80px;
    left: 50%;
    transform: translateX(-50%);
    width: 680px;
    max-height: 70vh;
    background: #1e2130;
    border: 1px solid #334155;
    border-radius: 8px;
    display: flex;
    flex-direction: column;
    box-shadow: 0 20px 60px rgba(0, 0, 0, 0.5);
    overflow: hidden;
  }
  .snapshots-window.maximized {
    top: 0; left: 0; transform: none;
    width: 100vw; height: 100vh; max-height: 100vh;
    border-radius: 0;
  }
  .snapshots-window.minimized { display: none; }

  .content {
    flex: 1;
    overflow-y: auto;
    padding: 16px;
    display: flex;
    flex-direction: column;
    gap: 12px;
  }

  .toolbar {
    display: flex;
    justify-content: space-between;
    align-items: center;
  }
  .heading {
    font-size: 14px;
    font-weight: 600;
    color: #e2e8f0;
  }

  .error-bar {
    background: rgba(239, 68, 68, 0.15);
    border: 1px solid rgba(239, 68, 68, 0.4);
    color: #fca5a5;
    border-radius: 6px;
    padding: 8px 12px;
    font-size: 13px;
  }

  .loading, .empty {
    text-align: center;
    color: #64748b;
    padding: 32px 0;
  }
  .empty .hint { font-size: 12px; margin-top: 6px; }

  .snap-list { display: flex; flex-direction: column; gap: 8px; }

  .snap-card {
    background: #252a3d;
    border: 1px solid #334155;
    border-radius: 6px;
    padding: 12px 14px;
    display: flex;
    flex-direction: column;
    gap: 6px;
  }

  .snap-header {
    display: flex;
    align-items: center;
    gap: 10px;
  }
  .snap-name {
    font-size: 14px;
    font-weight: 600;
    color: #e2e8f0;
    flex: 1;
  }
  .proto-badge {
    background: rgba(99, 102, 241, 0.2);
    color: #a5b4fc;
    border: 1px solid rgba(99, 102, 241, 0.4);
    border-radius: 4px;
    padding: 2px 8px;
    font-size: 11px;
    font-weight: 600;
    text-transform: uppercase;
  }

  .snap-meta {
    display: flex;
    flex-wrap: wrap;
    gap: 10px;
  }
  .meta-item {
    font-size: 12px;
    color: #94a3b8;
    display: flex;
    align-items: center;
    gap: 4px;
  }
  .meta-item.date { color: #64748b; }

  .snap-actions {
    display: flex;
    gap: 8px;
    margin-top: 4px;
  }

  /* Buttons */
  .btn-primary {
    background: rgba(99, 102, 241, 0.2);
    border: 1px solid rgba(99, 102, 241, 0.5);
    color: #a5b4fc;
    border-radius: 5px;
    padding: 6px 12px;
    font-size: 13px;
    cursor: pointer;
    transition: background 0.15s;
  }
  .btn-primary:hover { background: rgba(99, 102, 241, 0.35); }

  .btn-sm {
    background: rgba(255,255,255,0.05);
    border: 1px solid #334155;
    color: #94a3b8;
    border-radius: 4px;
    padding: 4px 10px;
    font-size: 12px;
    cursor: pointer;
    transition: background 0.15s;
  }
  .btn-sm:hover { background: rgba(255,255,255,0.1); color: #e2e8f0; }
  .btn-sm.btn-accent { background: rgba(99,102,241,0.2); border-color: rgba(99,102,241,0.4); color: #a5b4fc; }
  .btn-sm.btn-accent:hover { background: rgba(99,102,241,0.35); }
  .btn-sm.btn-danger { background: rgba(239,68,68,0.1); border-color: rgba(239,68,68,0.3); color: #fca5a5; }
  .btn-sm.btn-danger:hover { background: rgba(239,68,68,0.2); }
  .btn-sm:disabled { opacity: 0.5; cursor: not-allowed; }

  /* Dialog */
  .dialog-overlay {
    position: absolute;
    inset: 0;
    background: rgba(0,0,0,0.6);
    display: flex;
    align-items: center;
    justify-content: center;
    z-index: 10;
  }
  .dialog {
    background: #1e2130;
    border: 1px solid #334155;
    border-radius: 8px;
    padding: 20px;
    width: 340px;
    display: flex;
    flex-direction: column;
    gap: 14px;
  }
  .dialog h3 { font-size: 15px; font-weight: 600; color: #e2e8f0; margin: 0; }
  .dialog-hint { font-size: 12px; color: #64748b; margin: 0; }

  .form-group { display: flex; flex-direction: column; gap: 6px; }
  .form-group label { font-size: 12px; color: #94a3b8; }
  .form-group input, .form-group select {
    background: #0f1117;
    border: 1px solid #334155;
    border-radius: 5px;
    color: #e2e8f0;
    padding: 7px 10px;
    font-size: 13px;
    outline: none;
  }
  .form-group input:focus, .form-group select:focus { border-color: #6366f1; }

  .dialog-actions { display: flex; gap: 8px; justify-content: flex-end; }

  /* Responsive */
  @media (max-width: 768px) {
    .snapshots-window {
      width: 100vw;
      max-height: 100vh;
      top: 0;
      left: 0;
      transform: none;
      border-radius: 0;
    }
    .toolbar { flex-direction: column; gap: 8px; align-items: stretch; }
    .snap-actions { flex-wrap: wrap; }
    .content { padding: 10px; }
  }
</style>
