<script lang="ts">
  import { createEventDispatcher } from "svelte";
  import { peerStore } from "$store/peer";
  import { toasts } from "$store/toast";
  import { activeThing, openedApps, bringToFront } from "$store/store";

  export let peer: any; // Type defined in store usually, using any for now to match pattern

  const dispatch = createEventDispatcher();

  // Edit peer name state
  let editingName = false;
  let newName = "";
  let savingName = false;

  // Edit peer tags state
  let editingTags = false;
  let newTags = "";
  let savingTags = false;

  function startEditName() {
    editingName = true;
    newName = peer.name;
  }

  function cancelEditName() {
    editingName = false;
    newName = "";
  }

  // Svelte action to focus input on mount
  function focusOnMount(node: HTMLInputElement) {
    node.focus();
  }

  async function handleSaveName() {
    if (!newName.trim()) {
      cancelEditName();
      return;
    }

    savingName = true;
    try {
      await peerStore.updatePeer(peer.id, newName.trim());
      editingName = false;
      newName = "";
    } catch (err: any) {
      console.error("Failed to update peer name:", err);
      toasts.error("Failed to update name");
    } finally {
      savingName = false;
    }
  }

  function startEditTags() {
    editingTags = true;
    newTags = (peer.tags || []).join(", ");
  }

  function cancelEditTags() {
    editingTags = false;
    newTags = "";
  }

  async function handleSaveTags() {
    savingTags = true;
    try {
      const tags = newTags
        .split(",")
        .map((t) => t.trim())
        .filter((t) => t.length > 0);

      await peerStore.updatePeer(peer.id, peer.name, tags);
      editingTags = false;
      newTags = "";
    } catch (err: any) {
      console.error("Failed to update tags:", err);
      toasts.error("Failed to update tags");
    } finally {
      savingTags = false;
    }
  }

  function handleClose() {
    dispatch("close");
  }

  function viewNotes() {
    peerStore.setSelectedPeer(peer);
    if (!$openedApps.includes("PeerNotes")) {
      $openedApps = [...$openedApps, "PeerNotes"];
    }
    $activeThing = "PeerNotes";
    bringToFront("PeerNotes");
  }

  // We need to keep a copy of peer ID to reset state if peer changes (though unlikely in this context)
  let currentPeerId = peer.id;
  $: if (peer.id !== currentPeerId) {
    currentPeerId = peer.id;
    cancelEditName();
    cancelEditTags();
  }
</script>

<div class="stats-card config-card">
  <div class="stats-header">
    <h3>
      <svg
        width="16"
        height="16"
        viewBox="0 0 24 24"
        fill="none"
        stroke="currentColor"
        stroke-width="2"
        ><path
          d="M12 15V3m0 12l-4-4m4 4l4-4M2 17l.621 2.485A2 2 0 0 0 4.561 21h14.878a2 2 0 0 0 1.94-1.515L22 17"
          stroke-linecap="round"
          stroke-linejoin="round"
        /></svg
      >
      Device Settings
    </h3>
    <div class="stats-actions">
      <button class="stats-action-btn close" on:click={handleClose}>
        <svg
          width="16"
          height="16"
          viewBox="0 0 24 24"
          fill="none"
          stroke="currentColor"
          stroke-width="2.5"><path d="M18 6L6 18M6 6l12 12" /></svg
        >
      </button>
    </div>
  </div>
  <div class="details-grid">
    <div class="detail-item full-width">
      <span class="detail-label">Device Name</span>
      {#if editingName}
        <div class="edit-name-container">
          <input
            type="text"
            class="edit-name-input"
            bind:value={newName}
            on:keydown={(e) => {
              if (e.key === "Enter") handleSaveName();
              if (e.key === "Escape") cancelEditName();
            }}
            disabled={savingName}
            use:focusOnMount
          />
          <button
            class="edit-action-btn save"
            on:click={handleSaveName}
            disabled={savingName}
          >
            {#if savingName}<div class="mini-spinner" />{:else}<svg
                width="14"
                height="14"
                viewBox="0 0 24 24"
                fill="none"
                ><path
                  d="M20 6L9 17L4 12"
                  stroke="currentColor"
                  stroke-width="2"
                  stroke-linecap="round"
                  stroke-linejoin="round"
                /></svg
              >{/if}
          </button>
          <button
            class="edit-action-btn cancel"
            on:click={cancelEditName}
            disabled={savingName}
          >
            <svg width="14" height="14" viewBox="0 0 24 24" fill="none"
              ><path
                d="M18 6L6 18M6 6l12 12"
                stroke="currentColor"
                stroke-width="2"
                stroke-linecap="round"
                stroke-linejoin="round"
              /></svg
            >
          </button>
        </div>
      {:else}
        <div class="editable-value">
          <span class="detail-value">{peer.name}</span>
          <button class="edit-btn" on:click={startEditName}
            ><svg width="14" height="14" viewBox="0 0 24 24" fill="none"
              ><path
                d="M11 4H4a2 2 0 0 0-2 2v14a2 2 0 0 0 2 2h14a2 2 0 0 0 2-2v-7"
                stroke="currentColor"
                stroke-width="2"
                stroke-linecap="round"
                stroke-linejoin="round"
              /><path
                d="M18.5 2.5a2.121 2.121 0 0 1 3 3L12 15l-4 1 1-4 9.5-9.5z"
                stroke="currentColor"
                stroke-width="2"
                stroke-linecap="round"
                stroke-linejoin="round"
              /></svg
            ></button
          >
        </div>
      {/if}
    </div>
    <div class="detail-item full-width">
      <span class="detail-label">Tags</span>
      {#if editingTags}
        <div class="edit-name-container">
          <input
            type="text"
            class="edit-name-input"
            bind:value={newTags}
            placeholder="e.g. server, london, prod"
            on:keydown={(e) => {
              if (e.key === "Enter") handleSaveTags();
              if (e.key === "Escape") cancelEditTags();
            }}
            disabled={savingTags}
            use:focusOnMount
          />
          <button
            class="edit-action-btn save"
            on:click={handleSaveTags}
            disabled={savingTags}
          >
            {#if savingTags}<div class="mini-spinner" />{:else}<svg
                width="14"
                height="14"
                viewBox="0 0 24 24"
                fill="none"
                ><path
                  d="M20 6L9 17L4 12"
                  stroke="currentColor"
                  stroke-width="2"
                  stroke-linecap="round"
                  stroke-linejoin="round"
                /></svg
              >{/if}
          </button>
          <button
            class="edit-action-btn cancel"
            on:click={cancelEditTags}
            disabled={savingTags}
          >
            <svg width="14" height="14" viewBox="0 0 24 24" fill="none"
              ><path
                d="M18 6L6 18M6 6l12 12"
                stroke="currentColor"
                stroke-width="2"
                stroke-linecap="round"
                stroke-linejoin="round"
              /></svg
            >
          </button>
        </div>
      {:else}
        <div class="editable-value">
          {#if peer.tags && peer.tags.length > 0}<div class="tags-list">
              {#each peer.tags as tag}<span class="tag-pill">{tag}</span>{/each}
            </div>{:else}<span class="detail-value text-muted">No tags</span
            >{/if}
          <button class="edit-btn" on:click={startEditTags}
            ><svg width="14" height="14" viewBox="0 0 24 24" fill="none"
              ><path
                d="M11 4H4a2 2 0 0 0-2 2v14a2 2 0 0 0 2 2h14a2 2 0 0 0 2-2v-7"
                stroke="currentColor"
                stroke-width="2"
                stroke-linecap="round"
                stroke-linejoin="round"
              /><path
                d="M18.5 2.5a2.121 2.121 0 0 1 3 3L12 15l-4 1 1-4 9.5-9.5z"
                stroke="currentColor"
                stroke-width="2"
                stroke-linecap="round"
                stroke-linejoin="round"
              /></svg
            ></button
          >
          <div title="View Notes">
            <button class="edit-btn" on:click={viewNotes}>
              <svg
                width="14"
                height="14"
                viewBox="0 0 24 24"
                fill="none"
                stroke="currentColor"
                stroke-width="2"
                stroke-linecap="round"
                stroke-linejoin="round"
                ><path
                  d="M14.5 2H6a2 2 0 0 0-2 2v16a2 2 0 0 0 2 2h12a2 2 0 0 0 2-2V7.5L14.5 2z"
                /><polyline points="14 2 14 8 20 8" /><line
                  x1="16"
                  x2="8"
                  y1="13"
                  y2="13"
                /><line x1="16" x2="8" y1="17" y2="17" /><line
                  x1="10"
                  x2="8"
                  y1="9"
                  y2="9"
                /></svg
              >
            </button>
          </div>
        </div>
      {/if}
    </div>
    <div class="detail-item full-width">
      <span class="detail-label">Public Key</span>
      <code class="detail-value">{peer.public_key}</code>
    </div>
  </div>
</div>

<style lang="scss">
  /* Inheriting variables from global scope */
  .stats-card {
    background: rgb(var(--bg2));
    border: 1px solid rgb(var(--clr) / 8%);
    border-radius: 8px;
    padding: 16px;
  }

  .stats-header {
    display: flex;
    justify-content: space-between;
    align-items: center;
    margin-bottom: 16px;
    padding-bottom: 12px;
    border-bottom: 1px solid rgb(var(--clr) / 8%);

    h3 {
      font-size: 14px;
      font-weight: 600;
      color: rgb(var(--clr));
      margin: 0;
      display: flex;
      align-items: center;
      gap: 8px;

      svg {
        color: rgb(var(--clrPrm));
      }
    }
  }

  .stats-actions {
    display: flex;
    gap: 8px;
  }

  .stats-action-btn {
    display: flex;
    align-items: center;
    justify-content: center;
    width: 24px;
    height: 24px;
    background: transparent;
    border: none;
    border-radius: 4px;
    color: rgb(var(--clr) / 50%);
    cursor: pointer;
    transition: all 0.2s;

    &:hover {
      background: rgb(var(--clr) / 8%);
      color: rgb(var(--clr));
    }

    &.close:hover {
      color: rgb(var(--clrPrm));
    }
  }

  .details-grid {
    display: flex;
    flex-direction: column;
    gap: 16px;
  }

  .detail-item {
    display: flex;
    flex-direction: column;
    gap: 4px;

    .detail-label {
      font-size: 11px;
      color: rgb(var(--clr) / 60%);
      text-transform: uppercase;
      letter-spacing: 0.5px;
      font-weight: 600;
    }

    .detail-value {
      font-size: 14px;
      color: rgb(var(--clr));

      &:is(code) {
        font-family: "Cascadia Code", monospace;
        font-size: 12px;
        background: rgb(var(--bg1));
        padding: 8px;
        border-radius: 4px;
        word-break: break-all;
      }
    }

    .editable-value {
      display: flex;
      align-items: center;
      gap: 8px;
    }

    .tags-list {
      display: flex;
      flex-wrap: wrap;
      gap: 6px;
    }

    .tag-pill {
      background: rgb(var(--clrPrm) / 10%);
      color: rgb(var(--clrPrm));
      padding: 2px 8px;
      border-radius: 4px;
      font-size: 11px;
      font-weight: 600;
      border: 1px solid rgb(var(--clrPrm) / 20%);
    }

    .edit-btn {
      display: flex;
      align-items: center;
      justify-content: center;
      width: 28px;
      height: 28px;
      background: rgb(var(--clr) / 5%);
      border: 1px solid rgb(var(--clr) / 10%);
      border-radius: 6px;
      color: rgb(var(--clr) / 60%);
      cursor: pointer;
      transition: all 0.2s;

      &:hover {
        background: rgb(var(--clr) / 10%);
        color: rgb(var(--clr));
        border-color: rgb(var(--clr) / 20%);
      }
    }

    .edit-name-container {
      display: flex;
      align-items: center;
      gap: 8px;
    }

    .edit-name-input {
      flex: 1;
      max-width: 300px;
      padding: 8px 12px;
      font-size: 14px;
      background: rgb(var(--bg1));
      border: 1px solid rgb(var(--clr) / 20%);
      border-radius: 6px;
      color: rgb(var(--clr));
      outline: none;
      transition: border-color 0.2s;

      &:focus {
        border-color: rgb(var(--clrPrm));
      }

      &:disabled {
        opacity: 0.6;
      }
    }

    .edit-action-btn {
      display: flex;
      align-items: center;
      justify-content: center;
      width: 32px;
      height: 32px;
      border: none;
      border-radius: 6px;
      cursor: pointer;
      transition: all 0.2s;

      &.save {
        background: rgb(34, 197, 94);
        color: white;

        &:hover:not(:disabled) {
          background: rgb(22, 163, 74);
        }
      }

      &.cancel {
        background: rgb(var(--clr) / 10%);
        color: rgb(var(--clr) / 70%);

        &:hover:not(:disabled) {
          background: rgb(var(--clr) / 15%);
          color: rgb(var(--clr));
        }
      }

      &:disabled {
        opacity: 0.6;
        cursor: not-allowed;
      }
    }

    .mini-spinner {
      width: 14px;
      height: 14px;
      border: 2px solid rgba(255, 255, 255, 0.3);
      border-top-color: white;
      border-radius: 50%;
      animation: spin 0.8s linear infinite;
    }
  }

  .text-muted {
    color: rgb(var(--clr) / 50%);
    font-style: italic;
    font-size: 13px;
  }

  @keyframes spin {
    to {
      transform: rotate(360deg);
    }
  }
</style>
