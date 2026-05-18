<script lang="ts">
    import { peerStore, type Peer } from "../store/peer";
    import { draggable } from "@neodrag/svelte";
    import { scale } from "svelte/transition";
    import Titlebar from "$components/shared/Titlebar.svelte";
    import {
        openedApps,
        activeThing,
        appZIndexes,
        bringToFront,
    } from "$store/store";
    import { createEventDispatcher, onMount, onDestroy } from "svelte";

    const dispatch = createEventDispatcher();

    export let peerId: string = "";
    export let peer: Peer | null = null;

    let currentNotes = "";
    let saveStatus: "idle" | "saving" | "saved" | "error" = "idle";
    let debounceTimer: ReturnType<typeof setTimeout>;
    let initialized = false;

    // Window state
    let isMaximized = false;
    let isMinimized = false;
    let isMobile = false;
    let windowEl: HTMLDivElement;

    onMount(() => {
        if (peer) {
            currentNotes = peer.notes || "";
            initialized = true;
        }
        isMobile = window.innerWidth <= 768;
        if (isMobile) {
            isMaximized = true;
        }
    });

    // Subscribe to peer store to get the specific peer
    $: {
        if (!initialized && !peer && peerId && $peerStore.peers.length > 0) {
            peer = $peerStore.peers.find((p) => p.id === peerId) || null;
            if (peer) {
                currentNotes = peer.notes || "";
                initialized = true;
            }
        }
    }

    // Z-index for window stacking
    $: zIndex = $appZIndexes["PeerNotes"] || 100;

    $: if ($activeThing === "PeerNotes" && isMinimized) {
        isMinimized = false;
    }

    $: if ($activeThing === "PeerNotes") {
        bringToFront("PeerNotes");
    }

    // Auto-save on notes change (debounced)
    function handleInput() {
        if (!initialized) return;
        clearTimeout(debounceTimer);
        saveStatus = "idle";

        debounceTimer = setTimeout(async () => {
            if (!peerId) return;
            saveStatus = "saving";
            try {
                await peerStore.updatePeerNotes(peerId, currentNotes);
                saveStatus = "saved";
                setTimeout(() => {
                    if (saveStatus === "saved") saveStatus = "idle";
                }, 2000);
            } catch {
                saveStatus = "error";
                setTimeout(() => {
                    if (saveStatus === "error") saveStatus = "idle";
                }, 4000);
            }
        }, 800);
    }

    onDestroy(() => {
        clearTimeout(debounceTimer);
        if (peerId && initialized) {
            peerStore.updatePeerNotes(peerId, currentNotes).catch(() => {});
        }
    });

    function handleFocus() {
        $activeThing = "PeerNotes";
        bringToFront("PeerNotes");
    }

    function handleClose() {
        clearTimeout(debounceTimer);
        if (peerId && initialized) {
            peerStore.updatePeerNotes(peerId, currentNotes).catch(() => {});
        }
        $activeThing = "";
        $openedApps = $openedApps.filter((oa) => oa !== "PeerNotes");
        dispatch("close");
    }

    function handleMaximize() {
        isMaximized = !isMaximized;
    }

    function handleReduce() {
        isMinimized = true;
        $activeThing = "";
    }
</script>

<div
    class="peer-notes activeShadow"
    class:maximized={isMaximized || isMobile}
    class:minimized={isMinimized}
    style:z-index={zIndex}
    bind:this={windowEl}
    on:mousedown={handleFocus}
    on:touchstart={handleFocus}
    use:draggable={{
        handle: ".title-bar",
        disabled: isMaximized || isMobile,
        bounds: "body",
    }}
    transition:scale={{ duration: 200 }}
>
    <Titlebar
        title={`Notes — ${peer?.name || "Device"}`}
        color="#2563EB"
        appName={"PeerNotes"}
        canMaximize={!isMobile}
        canReduce={!isMobile}
        canClose={true}
        on:close={handleClose}
        on:maximize={handleMaximize}
        on:reduce={handleReduce}
    >
        <svg
            slot="icon"
            width="16"
            height="16"
            viewBox="0 0 24 24"
            fill="none"
            stroke="currentColor"
            stroke-width="2"
        >
            <path
                d="M14.5 2H6a2 2 0 0 0-2 2v16a2 2 0 0 0 2 2h12a2 2 0 0 0 2-2V7.5L14.5 2z"
            />
            <polyline points="14 2 14 8 20 8" />
            <line x1="16" y1="13" x2="8" y2="13" />
            <line x1="16" y1="17" x2="8" y2="17" />
            <line x1="10" y1="9" x2="8" y2="9" />
        </svg>
        <span class="appName pl-2">Notes — {peer?.name || "Device"}</span>
    </Titlebar>

    <div class="notepad-container">
        <div class="status-bar">
            <span class="status-text">{peer?.name || "Device"} — Notes</span>
            <span
                class="save-indicator"
                class:saving={saveStatus === "saving"}
                class:saved={saveStatus === "saved"}
                class:error={saveStatus === "error"}
            >
                {#if saveStatus === "saving"}
                    <span class="dot pulse"></span> Saving...
                {:else if saveStatus === "saved"}
                    <span class="dot green"></span> Saved
                {:else if saveStatus === "error"}
                    <span class="dot red"></span> Error saving
                {:else}
                    &nbsp;
                {/if}
            </span>
        </div>

        <textarea
            class="notepad-editor"
            placeholder="Type your notes here..."
            bind:value={currentNotes}
            on:input={handleInput}
            spellcheck="false"
        ></textarea>
    </div>
</div>

<style lang="scss">
    .peer-notes {
        background: var(--mica);
        position: absolute;
        border-radius: 8px;
        overflow: hidden;
        resize: both;
        width: min(640px, 90vw);
        min-height: 350px;
        height: min(500px, 75vh);
        max-height: calc(100vh - 48px);
        display: flex;
        flex-direction: column;
        box-shadow: 0 8px 32px rgba(0, 0, 0, 0.24), 0 0 0 1px rgb(var(--clr) / 8%);
        border: 1px solid rgb(var(--clr) / 10%);
        top: 12vh;
        left: max(5vw, calc(50vw - 320px));
    }

    .peer-notes.maximized {
        position: fixed !important;
        top: 0 !important;
        left: 0 !important;
        width: 100vw !important;
        height: calc(100vh - 48px) !important;
        max-height: none;
        min-height: unset;
        border-radius: 0;
        resize: none;
        border: none;
        box-shadow: none;
        transform: none !important;
    }

    .peer-notes.minimized {
        display: none;
    }

    .notepad-container {
        flex: 1;
        display: flex;
        flex-direction: column;
        overflow: hidden;
    }

    .status-bar {
        display: flex;
        align-items: center;
        justify-content: space-between;
        padding: 4px 12px;
        font-size: 11px;
        color: rgb(var(--clr) / 45%);
        border-bottom: 1px solid rgb(var(--clr) / 8%);
        background: rgb(var(--clr) / 4%);
        flex-shrink: 0;
        user-select: none;
    }

    .save-indicator {
        display: flex;
        align-items: center;
        gap: 4px;
        min-width: 80px;
        justify-content: flex-end;
        transition: color 0.2s;
    }
    .save-indicator.saving {
        color: rgba(255, 200, 50, 0.7);
    }
    .save-indicator.saved {
        color: rgba(80, 200, 120, 0.8);
    }
    .save-indicator.error {
        color: rgba(255, 80, 80, 0.8);
    }

    .dot {
        width: 6px;
        height: 6px;
        border-radius: 50%;
        display: inline-block;
    }
    .dot.pulse {
        background: rgba(255, 200, 50, 0.8);
        animation: pulse 1s ease-in-out infinite;
    }
    .dot.green {
        background: rgba(80, 200, 120, 0.9);
    }
    .dot.red {
        background: rgba(255, 80, 80, 0.9);
    }

    @keyframes pulse {
        0%,
        100% {
            opacity: 1;
        }
        50% {
            opacity: 0.3;
        }
    }

    .notepad-editor {
        flex: 1;
        width: 100%;
        resize: none;
        border: none;
        outline: none;
        padding: 16px;
        font-family: "Cascadia Code", "Fira Code", "SF Mono", "Consolas",
            monospace;
        font-size: 13px;
        line-height: 1.6;
        background: transparent;
        color: rgb(var(--clr) / 88%);
        caret-color: rgb(var(--clrPrm));

        &::placeholder {
            color: rgb(var(--clr) / 25%);
        }
        &::-webkit-scrollbar {
            width: 6px;
        }
        &::-webkit-scrollbar-track {
            background: transparent;
        }
        &::-webkit-scrollbar-thumb {
            background-color: rgb(var(--clr) / 15%);
            border-radius: 3px;
        }
    }

    @media (max-width: 768px) {
        .notepad-editor {
            font-size: 15px;
            padding: 12px;
        }
    }
</style>
