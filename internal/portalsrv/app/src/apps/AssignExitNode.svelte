<script lang="ts">
    import { draggable } from "@neodrag/svelte";
    import { scale } from "svelte/transition";
    import Titlebar from "$components/shared/Titlebar.svelte";
    import { topologyStore } from "$store/topology";
    import { wsStore } from "$store/websocket";
    import {
        openedApps,
        activeThing,
        appZIndexes,
        bringToFront,
    } from "$store/store";
    import { accountStore } from "$store/account";
    import { translateError$ } from "$store/i18n";
    import { RadioButton } from "fluent-svelte";

    // Z-index for window stacking
    $: zIndex = $appZIndexes["AssignExitNode"] || 100;

    function handleFocus() {
        $activeThing = "AssignExitNode";
        bringToFront("AssignExitNode");
    }

    // Get selected peers from store
    $: selectedPeers = $topologyStore.selectedPeersForExitNode;
    $: if (selectedPeers && selectedPeers.length > 0 && !exitNodeId) {
        exitNodeId = selectedPeers[0].id;
    }

    // Form state
    let exitNodeId = "";
    let isLoading = false;
    let error = "";

    $: exitNode = selectedPeers.find((p) => p.id === exitNodeId);
    $: entryNode = selectedPeers.find((p) => p.id !== exitNodeId);

    async function handleSubmit() {
        if (selectedPeers.length !== 2) {
            error = "Exactly 2 peers are required";
            return;
        }

        if (!exitNode || !entryNode) {
            error = "Please select an exit node";
            return;
        }

        isLoading = true;
        error = "";

        try {
            const resp = await wsStore.callGRPC(
                "TenantPortalService",
                "AssignExitNode",
                {
                    account_id: $accountStore.account?.id || "",
                    entry_node_id: entryNode.public_key || entryNode.id,
                    exit_node_id: exitNode.id,
                },
            );

            // Close and clean up on success
            handleClose();
        } catch (err: any) {
            error = err.message || "Failed to assign exit node";
            console.error("AssignExitNode error:", err);
        } finally {
            isLoading = false;
        }
    }

    function handleClose() {
        topologyStore.clearSelectedPeersForExitNode();
        $openedApps = $openedApps.filter((app) => app !== "AssignExitNode");
        if ($activeThing === "AssignExitNode") {
            $activeThing = "";
        }
    }
</script>

<div
    class="assign-exit-node activeShadow"
    style:z-index={zIndex}
    on:mousedown={handleFocus}
    on:touchstart={handleFocus}
    use:draggable={{
        handle: ".title-bar",
        bounds: "body",
    }}
    transition:scale={{ duration: 200 }}
>
    <Titlebar appName="AssignExitNode" canReduce={false} canMaximize={false}>
        <svg
            xmlns="http://www.w3.org/2000/svg"
            width="16"
            height="16"
            viewBox="0 0 24 24"
            fill="none"
            stroke="currentColor"
            stroke-width="2"
        >
            <path d="M15 3h6v6" />
            <path d="M9 21H3v-6" />
            <path d="M21 3l-7 7" />
            <path d="M3 21l7-7" />
        </svg>
        <span class="appName pl-2">Assign Exit Node</span>
    </Titlebar>
    <div class="mainApp">
        <form class="form-content" on:submit|preventDefault={handleSubmit}>
            {#if error}
                <div class="error-message">
                    <span class="error-icon"></span>
                    <span>{$translateError$(error)}</span>
                </div>
            {/if}

            <div class="card">
                <div class="card-header">
                    <h3 class="card-title">
                        <svg
                            width="16"
                            height="16"
                            viewBox="0 0 24 24"
                            fill="none"
                            stroke="currentColor"
                            stroke-width="2"
                        >
                            <path
                                d="M17 21v-2a4 4 0 0 0-4-4H5a4 4 0 0 0-4 4v2"
                            />
                            <circle cx="9" cy="7" r="4" />
                            <path d="M23 21v-2a4 4 0 0 0-3-3.87" />
                            <path d="M16 3.13a4 4 0 0 1 0 7.75" />
                        </svg>
                        Select Exit Node
                    </h3>
                </div>
                <div class="card-body">
                    <p
                        class="section-hint mb-4"
                        style="font-size: 13px; color: rgb(var(--clr) / 70%);"
                    >
                        Choose which device will act as the internet exit point.
                        The other device will route its traffic through it.
                    </p>

                    <div class="flex flex-col gap-3">
                        {#each selectedPeers as peer}
                            <!-- svelte-ignore a11y-click-events-have-key-events -->
                            <div
                                class="node-selection-card rounded-md border p-4 transition-all cursor-pointer flex gap-4 items-center"
                                class:selected={exitNodeId === peer.id}
                                on:click={() => (exitNodeId = peer.id)}
                            >
                                <RadioButton
                                    bind:group={exitNodeId}
                                    value={peer.id}
                                    class="pointer-events-none"
                                />
                                <div class="flex flex-col w-full">
                                    <div
                                        class="flex justify-between items-center w-full"
                                    >
                                        <span class="font-semibold"
                                            >{peer.label}</span
                                        >
                                        <span
                                            class="text-xs px-2 py-1 rounded"
                                            style="background: rgb(var(--bg3) / 60%);"
                                            >{peer.ip}</span
                                        >
                                    </div>
                                    <span class="text-xs mt-1">
                                        {#if exitNodeId === peer.id}
                                            <span
                                                style="color: #10b981; font-weight: 500;"
                                                >Exit Node (Internet Access
                                                Point)</span
                                            >
                                        {:else}
                                            <span style="color: #0ea5e9;"
                                                >Entry Node (Routes Through
                                                Exit)</span
                                            >
                                        {/if}
                                    </span>
                                </div>
                            </div>
                        {/each}
                    </div>
                </div>
            </div>
        </form>

        <div class="form-footer">
            <button type="button" class="btn-secondary" on:click={handleClose}>
                Cancel
            </button>
            <button
                type="submit"
                class="btn-primary"
                disabled={isLoading || selectedPeers.length !== 2}
                on:click={handleSubmit}
            >
                {#if isLoading}
                    Assigning...
                {:else}
                    Confirm Assignment
                {/if}
            </button>
        </div>
    </div>
</div>

<style lang="scss">
    .assign-exit-node {
        background: var(--mica);
        position: absolute;
        top: 15%;
        left: 25%;
        border-radius: 8px;
        overflow: hidden;
        width: 480px;
        max-width: 90vw;
        display: flex;
        flex-direction: column;
        box-shadow: 0 24px 48px rgba(0, 0, 0, 0.3);
    }

    .mainApp {
        display: flex;
        flex-direction: column;
        padding: 0;
        overflow-y: auto;
        flex: 1;
    }

    .form-content {
        padding: 16px;
        display: flex;
        flex-direction: column;
        gap: 16px;
    }

    .error-message {
        display: flex;
        align-items: center;
        gap: 10px;
        padding: 10px 14px;
        background: rgba(239, 68, 68, 0.12);
        border: 1px solid rgba(239, 68, 68, 0.25);
        border-radius: 6px;
        color: #ef4444;
        font-size: 12px;
    }

    .card {
        background: rgb(var(--bg2) / 60%);
        border: 1px solid rgb(var(--clr) / 8%);
        border-radius: 8px;
        overflow: hidden;
    }

    .card-header {
        padding: 12px 14px;
        border-bottom: 1px solid rgb(var(--clr) / 8%);
        background: rgb(var(--bg3) / 30%);
    }

    .card-title {
        margin: 0;
        font-size: 13px;
        font-weight: 600;
        color: rgb(var(--clr));
        display: flex;
        align-items: center;
        gap: 8px;
    }

    .card-title svg {
        color: rgb(var(--clrPrm));
        flex-shrink: 0;
    }

    .card-body {
        padding: 14px;
    }

    .node-selection-card {
        border-color: rgb(var(--clr) / 10%);
        background: rgb(var(--bg1) / 40%);
    }

    .node-selection-card:hover {
        background: rgb(var(--bg1) / 80%);
    }

    .node-selection-card.selected {
        border-color: rgb(var(--clrPrm));
        background: rgb(var(--clrPrm) / 15%);
    }

    .form-footer {
        display: flex;
        justify-content: flex-end;
        gap: 10px;
        padding: 12px 16px;
        background: rgb(var(--bg2));
        border-top: 1px solid rgb(var(--clr) / 8%);
    }

    .btn-secondary {
        padding: 8px 16px;
        background: rgb(var(--clr) / 10%);
        border: 1px solid rgb(var(--clr) / 20%);
        border-radius: 6px;
        color: rgb(var(--clr));
        font-size: 13px;
        font-weight: 500;
        cursor: pointer;
        transition: all 0.2s;

        &:hover {
            background: rgb(var(--clr) / 15%);
        }
    }

    .btn-primary {
        padding: 8px 16px;
        background: rgb(var(--clrPrm));
        border: none;
        border-radius: 6px;
        color: white;
        font-size: 13px;
        font-weight: 600;
        cursor: pointer;
        transition: all 0.2s;

        &:hover:not(:disabled) {
            background: rgb(var(--clrPrm) / 85%);
            transform: translateY(-1px);
        }

        &:disabled {
            opacity: 0.6;
            cursor: not-allowed;
        }
    }
</style>
