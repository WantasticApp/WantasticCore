<script lang="ts">
    import { draggable } from "@neodrag/svelte";
    import { Button } from "fluent-svelte";
    import { _ } from "$store/i18n";
    import Titlebar from "$components/shared/Titlebar.svelte";
    import {
        openedApps,
        activeThing,
        bringToFront,
        appZIndexes,
    } from "$store/store";
    import {
        onboardingPeer,
        peerStore,
        type PeerConfigTab,
    } from "$store/peer";
    import { fade, fly, scale } from "svelte/transition";

    // Window state
    let step = 1;
    let selectedDevice = "";
    let preparingConfig = false;
    let configError = "";

    const devices = [
        {
            id: "wireguard",
            name: "WireGuard",
            desc: "Native Client",
            icon: `<svg xmlns="http://www.w3.org/2000/svg" height="32" width="32" viewBox="0 0 300 300"><g fill="currentColor"><path d="M299.745 145.56S306.685 0 146.705 0C5.225 0 .805 139.63.805 139.63S-20.005 300 149.965 300c163.02 0 149.78-154.44 149.78-154.44zm-197.8-50.863c30.017-18.364 68.366-7.14 82.735 20.476 2.724 5.234 3.07 13.291 1.345 18.782-5.955 18.956-20.014 29.587-39.312 34.103 5.69-4.87 10.218-10.394 11.659-18.025a26.402 26.402 0 00-4.543-20.956 26.76 26.76 0 00-30.81-9.39c-11.882 4.512-18.39 15.355-17.217 28.684 1.09 12.38 10.484 20.405 28.061 23.453-2.627 1.39-4.65 2.414-6.63 3.517a63.918 63.918 0 00-20.543 17.868c-1.784 2.408-3.01 2.602-5.727.941-35.338-21.61-37.61-75.844.982-99.453zm-26.449 133.53c-5.677 1.441-11.178 3.574-16.98 5.478 2.838-19.152 25.264-36.789 44.23-34.777a48.881 48.881 0 00-9.243 25.893c-6.302 1.161-12.24 1.942-18.007 3.405zm120.79-186.98c5.61.206 11.23.12 16.844.254 1.402.092 2.794.286 4.168.58a40.607 40.607 0 01-4.236 5.434c-2.007 1.87-4.275 3.698-7.166.856-.696-.684-2.339-.527-3.549-.543-5.582-.073-11.172-.252-16.746-.041a104.04 104.04 0 00-14.425 1.473c-.894.16-2.23 3.131-1.819 4.227.97 2.585 2.383 5.436 4.478 7.09 7.74 6.11 15.972 11.595 23.748 17.663 7.556 5.897 14.589 12.358 18.875 21.253 5.584 11.59 5.747 23.743 3.339 35.95-4.02 20.378-14.333 37.261-31.032 49.524-6.729 4.941-15.06 7.746-22.767 11.295-6.778 3.123-13.755 5.812-20.55 8.901-12.248 5.57-19.132 18.865-17.107 32.688 1.858 12.685 12.987 23.271 25.735 25.456 15.292 2.622 31.07-7.316 34.812-22.86 4.206-17.478-5.29-33.083-23.065-37.813-.783-.208-1.569-.405-3.201-.827 4.754-2.124 8.861-3.638 12.653-5.724a347.934 347.934 0 0019.48-11.562c1.875-1.2 2.887-1.2 4.486.182 12.225 10.57 19.518 23.718 21.563 39.84 3.384 26.683-9.247 51.197-33.072 63.761-36.86 19.44-81.965-2.686-90.106-43.552-6.974-35.003 17.73-66.754 47.462-72.884 12.787-2.636 24.48-7.96 33.57-17.807 5.865-6.354 8.708-11.806 9.677-14.266a39.565 39.565 0 002.721-14.469 33.867 33.867 0 00-2.965-12.398c-3.104-7.075-14.995-18.33-17.94-20.704l-28-21.92c-.987-.813-2.099-.754-4.507-.591-2.861.194-10.175.599-13.331-.228 2.553-1.933 9.513-4.746 12.502-7.007-9.074-6.13-19.43-3.916-28.941-5.747 2.199-4.095 13.08-10.39 19.27-11.09a91.533 91.533 0 00-1.688-10.282c-.378-1.391-1.931-2.74-3.286-3.535-3.286-1.927-6.77-3.517-10.55-5.433a21.936 21.936 0 0111.333-3.505A42.316 42.316 0 01134.3 23.99c6.742 1.54 12.124.535 17.488-4.048-4.222-1.7-8.444-3.253-12.538-5.09a123.04 123.04 0 01-11.78-6.159c10.623 1.476 20.897 5.459 31.758 4.004l.277-1.481-25.229-5.873c15.04-1.376 29.042-1.604 42.301 4.855 3.731 1.817 7.635 3.321 11.211 5.397 1.744 1.012 2.919 3.008 4.35 4.56 1.136 1.232 2.05 2.883 3.446 3.626 5.3 2.818 11.134 2.929 17.078 2.787l.13-1.993c5.983 1.87 12.715 8.768 12.704 13.806-9.69 0-19.374-.037-29.056.054-1.034.01-2.062.766-3.093 1.175.98.571 1.943 1.6 2.942 1.637z"/><path d="M183.785 26.906a1.48 1.48 0 00-.189 2.369 2.233 2.233 0 003.072.821c.933-.47 1.848-.97 2.975-1.566-.908-.775-1.636-1.415-2.385-2.032-1.318-1.086-2.411-.404-3.473.408z"/></g></svg>`,
        },
        {
            id: "mikrotik",
            name: "MikroTik",
            desc: "RouterOS v7",
            icon: `<svg xmlns="http://www.w3.org/2000/svg" width="32" height="32" version="1.2" viewBox="0 0 610 610"><path fill-rule="evenodd" d="M586.8 193.4v222.5c0 13.8-1.7 25.6-5.5 34.3-.7 1.6-1.5 3.2-2.3 4.7-5.5 8.9-16.6 17.7-31.6 25.9L344.4 592c-12.6 6.9-24.2 11.4-34 12.7q-2.8.4-5.4.4-2.7 0-5.5-.4c-9.8-1.3-21.4-5.8-34-12.7L164 536.4 62.6 480.8c-15.1-8.2-26.2-17-31.6-25.9-5.5-9-7.9-22.5-7.9-39V193.4c0-13.8 1.7-25.5 5.5-34.2.7-1.7 1.5-3.3 2.4-4.7q1.3-2.2 3-4.3c6.1-7.5 16-14.7 28.6-21.7L164 72.9l101.5-55.6c15-8.2 28.6-13 39.5-13q2.6 0 5.4.4c9.8 1.2 21.4 5.7 34 12.6l101.5 55.6 101.5 55.6c12.6 7 22.4 14.2 28.5 21.7q1.8 2.1 3.1 4.3c.8 1.4 1.6 3 2.3 4.7 3.8 8.7 5.5 20.4 5.5 34.2m-102.5 33.2c0-9.8-5.3-18.8-13.8-23.4l-152.7-83.7c-8-4.4-17.7-4.4-25.7 0l-38.9 21.3c-4.6 2.6-4.6 9.2 0 11.7l116.4 63.8c4.6 2.6 4.6 9.2 0 11.7l-51.8 28.4c-8 4.4-17.7-4.4-25.7 0l-112-61.4c-8-4.4-17.7-4.4-25.7 0l-14.9 8.2c-8.6 4.7-13.9 13.6-13.9 23.4v7l135.5 74.3c8.6 4.6 13.9 13.6 13.9 23.3v141.4c0 4.8 2.6 9.3 6.9 11.7l10.2 5.6c8 4.4 17.7 4.4 25.7 0l10.3-5.6c4.2-2.4 6.9-6.9 6.9-11.7V331.2c0-9.7 5.3-18.7 13.9-23.3l65.5-36c4.5-2.4 9.9.8 9.9 5.9v142.4c0 5.1 5.4 8.3 9.9 5.9l36.3-19.9c8.5-4.7 13.8-13.7 13.8-23.4zm-298.7 78.2c0-4.8-2.6-9.3-6.9-11.7l-43.2-23.7c-4.5-2.4-9.9.8-9.9 5.9v107.5c0 9.7 5.3 18.7 13.9 23.4l36.3 19.9c4.4 2.4 9.8-.8 9.8-5.9z" style="fill:currentColor"/></svg>`,
        },
        {
            id: "openwrt",
            name: "OpenWrt",
            desc: "LuCI / UCI",
            icon: `<svg xmlns="http://www.w3.org/2000/svg" xml:space="preserve" id="Layer_1" x="0" y="0" height="32" width="32" version="1.1" viewBox="42.59 0 426.86 511.97"><style>.st0 {fill: currentColor;}</style><path d="M255.913 267.323c-19.226-.324-34.995 14.906-35.32 34.131-.323 19.226 14.906 34.996 34.132 35.32h1.296c19.226-.324 34.455-16.202 34.131-35.32-.432-18.793-15.553-33.807-34.24-34.13M42.594 88.673l36.184 36.184C125.76 77.766 189.487 51.411 256.02 51.52c66.534-.108 130.26 26.355 177.244 73.34l36.184-36.184C414.796 34.022 339.297-.001 256.02-.001c-83.384 0-158.883 34.023-213.428 88.676" class="st0"/><path d="m107.831 153.913 36.184 36.183c29.702-29.703 69.99-46.444 112.006-46.444s82.304 16.633 112.006 46.444l36.184-36.183C366.299 116 313.806 92.239 256.02 92.239s-110.278 23.762-148.19 61.674" class="st0"/><path d="m172.637 218.827 36.184 36.183c26.03-26.03 68.262-26.03 94.292 0l36.184-36.183c-22.034-22.142-52.061-34.455-83.276-34.347-32.295-.108-61.782 13.069-83.384 34.347" class="st0"/><path d="M567.9 1.5c18.3-25 28.2-55.3 28.2-86.3 0-81.1-66-147-147-147-81.1 0-147 66-147 147 0 32.2 10.5 62 28.2 86.3l-33.8 33.8c-27.1-34.2-41.8-76.5-41.7-120.1 0-107 87.3-194.3 194.3-194.3s194.3 87.3 194.3 194.3c0 45.3-15.8 87-41.7 120.1z" style="fill:currentColor" transform="matrix(1.0801 0 0 -1.0801 -229.16 210.51)"/></svg>`,
        },
    ];

    function nextStep() {
        if (step < 3) step++;
        else close();
    }

    function prevStep() {
        if (step > 1) step--;
    }

    function close() {
        $openedApps = $openedApps.filter((app) => app !== "OnboardingGuide");
        $activeThing = "";
        onboardingPeer.set(null);
    }

    function selectDevice(id: string) {
        configError = "";
        selectedDevice = id;
        nextStep();
    }

    // Z-index for window stacking
    $: zIndex = $appZIndexes["OnboardingGuide"] || 100;

    function handleFocus() {
        $activeThing = "OnboardingGuide";
        bringToFront("OnboardingGuide");
    }

    async function openConfigApp(preferredTab: PeerConfigTab) {
        const peerId = $onboardingPeer?.id;
        if (!peerId) {
            configError =
                "Unable to find the new device context. Please close this guide and try again.";
            return;
        }

        preparingConfig = true;
        configError = "";
        try {
            const config = await peerStore.getPeerConfig(peerId);
            peerStore.setSelectedPeerConfig(peerId, config, preferredTab);
            if (!$openedApps.includes("PeerConfig")) {
                $openedApps = [...$openedApps, "PeerConfig"];
            }
            $activeThing = "PeerConfig";
            bringToFront("PeerConfig");
            close();
        } catch (err: any) {
            configError =
                err?.message || "Failed to load the device configuration.";
        } finally {
            preparingConfig = false;
        }
    }

    // Get first name for friendly greeting
    $: peerName = $onboardingPeer?.name || "Device";
    $: selectedPlatformTab =
        selectedDevice === "wireguard"
            ? "wireguard"
            : selectedDevice === "openwrt"
              ? "unix"
              : "mikrotik";
</script>

<div
    class="onboarding-guide activeShadow"
    style:z-index={zIndex}
    on:mousedown={handleFocus}
    on:touchstart={handleFocus}
    use:draggable={{
        handle: ".title-bar",
        bounds: "body",
    }}
    transition:scale={{ duration: 200 }}
>
    <Titlebar
        title="New Connection Setup"
        appName="OnboardingGuide"
        canReduce={false}
        canMaximize={false}
        canClose={true}
        on:close={close}
    />

    <div class="mainApp">
        <!-- Progress Bar -->
        <div class="progress-bar-container">
            <div class="progress-steps text-[rgb(var(--clr))]">
                {#each [1, 2, 3] as s}
                    <div class="step-indicator" class:active={step >= s}>
                        {#if step > s}
                            <svg
                                xmlns="http://www.w3.org/2000/svg"
                                width="14"
                                height="14"
                                viewBox="0 0 24 24"
                            >
                                <path
                                    fill="currentColor"
                                    d="M9 16.17L4.83 12l-1.42 1.41L9 19 21 7l-1.41-1.41z"
                                />
                            </svg>
                        {:else}
                            {s}
                        {/if}
                    </div>
                    {#if s < 3}
                        <div class="step-line" class:active={step > s}></div>
                    {/if}
                {/each}
            </div>
        </div>

        <div
            class="slides-wrapper text-[rgb(var(--clr))] h-auto overflow-y-auto"
        >
            {#if step === 1}
                <div class="slide" in:fly={{ x: 50, duration: 300 }} out:fade>
                    <div
                        class="hero-icon rounded-2xl bg-[rgba(var(--clrPrm)/15%)] text-[rgb(var(--clrPrm))] border border-[rgb(var(--clrPrm)/30%)] shadow-[0_0_20px_rgb(var(--clrPrm)/20%)] mb-6 mt-4 p-5 flex items-center justify-center"
                    >
                        <svg
                            xmlns="http://www.w3.org/2000/svg"
                            width="64"
                            height="64"
                            viewBox="0 0 24 24"
                        >
                            <path
                                fill="currentColor"
                                d="M12 2C6.48 2 2 6.48 2 12s4.48 10 10 10 10-4.48 10-10S17.52 2 12 2zm-1 17.93c-3.95-.49-7-3.85-7-7.93 0-.62.08-1.21.21-1.79L9 15v1c0 1.1.9 2 2 2v1.93zm6.9-2.54c-.26-.81-1-1.39-1.9-1.39h-1v-3c0-.55-.45-1-1-1H8v-2h2c.55 0 1-.45 1-1V7h2c1.1 0 2-.9 2-2v-.41c2.93 1.19 5 4.06 5 7.41 0 2.08-.8 3.97-2.1 5.39z"
                            />
                        </svg>
                    </div>
                    <!-- Non-gradient simple semantic color text so it doesn't break in Blink/WebKit variants unexpectedly -->
                    <h1
                        class="text-[28px] font-semibold mb-3 text-[rgb(var(--clr))]"
                    >
                        {peerName} Created!
                    </h1>
                    <p
                        class="text-[rgb(var(--clr)/60%)] mb-8 px-8 text-center leading-relaxed text-[14px]"
                    >
                        Your secure tunnel endpoint is ready. Let's get it
                        connected to your network.
                    </p>
                    <Button
                        variant="accent"
                        class="px-8 py-2"
                        on:click={nextStep}
                    >
                        Start Setup
                    </Button>
                </div>
            {:else if step === 2}
                <div class="slide" in:fly={{ x: 50, duration: 300 }} out:fade>
                    <h2 class="text-2xl font-semibold mb-2 mt-2">
                        Choose Platform
                    </h2>
                    <p class="text-[rgb(var(--clr)/60%)] mb-6 text-[14px]">
                        Select the device type you are connecting.
                    </p>
                    <div class="device-grid w-full px-8">
                        {#each devices as device, i}
                            <!-- svelte-ignore a11y-click-events-have-key-events -->
                            <div
                                class="device-card {selectedDevice === device.id
                                    ? 'selected'
                                    : ''}"
                                on:click={() => selectDevice(device.id)}
                                in:fly={{ y: 20, duration: 300, delay: 50 * i }}
                                role="button"
                                tabindex="0"
                            >
                                <div
                                    class="icon"
                                    class:text-[rgb(var(--clrPrm))]={selectedDevice ===
                                    device.id
                                        ? "selected"
                                        : ""}
                                >
                                    {@html device.icon}
                                </div>
                                <div class="info text-left">
                                    <div
                                        class="name font-medium text-[rgb(var(--clr))] text-[16px]"
                                    >
                                        {device.name}
                                    </div>
                                    <div
                                        class="desc text-xs text-[rgb(var(--clr)/50%)]"
                                    >
                                        {device.desc}
                                    </div>
                                </div>
                                <svg
                                    class="chevron justify-self-end text-[rgb(var(--clr)/30%)]"
                                    xmlns="http://www.w3.org/2000/svg"
                                    width="20"
                                    height="20"
                                    viewBox="0 0 24 24"
                                >
                                    <path
                                        fill="currentColor"
                                        d="M8.59 16.59L13.17 12 8.59 7.41 10 6l6 6-6 6-1.41-1.41z"
                                    />
                                </svg>
                            </div>
                        {/each}
                    </div>
                </div>
            {:else if step === 3}
                <div class="slide" in:fly={{ x: 50, duration: 300 }} out:fade>
                    <h2 class="text-2xl font-semibold mb-2 mt-2">
                        Configuration Method
                    </h2>
                    <p class="text-[rgb(var(--clr)/60%)] mb-6 text-[14px]">
                        How would you like to configure <span
                            class="text-[rgb(var(--clrPrm))] font-medium capitalize"
                            >{selectedDevice}</span
                        >?
                    </p>
                    {#if configError}
                        <div class="config-error">{configError}</div>
                    {/if}
                    <div
                        class="options-grid px-8 w-full gap-4 {selectedDevice !==
                        'wireguard'
                            ? 'grid-cols-1'
                            : 'grid-cols-2'}"
                    >
                        {#if selectedDevice === "wireguard"}
                            <!-- svelte-ignore a11y-click-events-have-key-events -->
                            <div
                                class="option-card"
                                class:busy={preparingConfig}
                                on:click={() => !preparingConfig && openConfigApp("qrcode")}
                                role="button"
                                tabindex="0"
                            >
                                <div
                                    class="opt-icon bg-[rgb(var(--clr)/5%)] p-4 rounded-full mb-3 text-[rgb(var(--clr))]"
                                >
                                    <svg
                                        xmlns="http://www.w3.org/2000/svg"
                                        width="32"
                                        height="32"
                                        viewBox="0 0 24 24"
                                    >
                                        <path
                                            fill="currentColor"
                                            d="M3 5v14a2 2 0 0 0 2 2h14c1.1 0 2-.9 2-2V5c0-1.1-.9-2-2-2H5a2 2 0 0 0-2 2zm12 4c0 3.32-2.69 6-6 6s-6-2.68-6-6h12z"
                                        />
                                    </svg>
                                </div>
                                <div
                                    class="font-medium text-[rgb(var(--clr))] text-[15px]"
                                >
                                    QR Code
                                </div>
                                <div
                                    class="text-xs text-[rgb(var(--clr)/50%)] mt-1"
                                >
                                    Scan with mobile app
                                </div>
                            </div>
                            <!-- svelte-ignore a11y-click-events-have-key-events -->
                            <div
                                class="option-card"
                                class:busy={preparingConfig}
                                on:click={() => !preparingConfig && openConfigApp("wireguard")}
                                role="button"
                                tabindex="0"
                            >
                                <div
                                    class="opt-icon bg-[rgb(var(--clr)/5%)] p-4 rounded-full mb-3 text-[rgb(var(--clr))]"
                                >
                                    <svg
                                        xmlns="http://www.w3.org/2000/svg"
                                        width="32"
                                        height="32"
                                        viewBox="0 0 24 24"
                                    >
                                        <path
                                            fill="currentColor"
                                            d="M19 9h-4V3H9v6H5l7 7 7-7zM5 18v2h14v-2H5z"
                                        />
                                    </svg>
                                </div>
                                <div
                                    class="font-medium text-[rgb(var(--clr))] text-[15px]"
                                >
                                    Download Config
                                </div>
                                <div
                                    class="text-xs text-[rgb(var(--clr)/50%)] mt-1"
                                >
                                    For desktop clients
                                </div>
                            </div>
                        {:else}
                            <!-- svelte-ignore a11y-click-events-have-key-events -->
                            <div
                                class="option-card w-full"
                                class:busy={preparingConfig}
                                on:click={() => !preparingConfig && openConfigApp(selectedPlatformTab)}
                                role="button"
                                tabindex="0"
                            >
                                <div
                                    class="opt-icon bg-[rgb(var(--clr)/5%)] p-4 rounded-full mb-3 text-[rgb(var(--clr))]"
                                >
                                    <svg
                                        xmlns="http://www.w3.org/2000/svg"
                                        width="32"
                                        height="32"
                                        viewBox="0 0 24 24"
                                    >
                                        <path
                                            fill="currentColor"
                                            d="M20 4H4c-1.1 0-1.99.9-1.99 2L2 18c0 1.1.9 2 2 2h16c1.1 0 2-.9 2-2V6c0-1.1-.9-2-2-2zm-5 14H4v-4h11v4zm0-5H4V9h11v4zm5 5h-4V9h4v9z"
                                        />
                                    </svg>
                                </div>
                                <div
                                    class="font-medium text-[rgb(var(--clr))] text-[15px]"
                                >
                                    View Setup Script
                                </div>
                                <div
                                    class="text-xs text-[rgb(var(--clr)/50%)] mt-1"
                                >
                                    Generate terminal commands for {selectedDevice}
                                </div>
                            </div>
                        {/if}
                    </div>
                </div>
            {/if}
        </div>

        <div class="footer-nav">
            {#if step > 1}
                <Button variant="standard" on:click={prevStep} disabled={preparingConfig}>Back</Button>
            {:else}
                <div></div>
            {/if}
            <Button variant="standard" on:click={close} disabled={preparingConfig}>Cancel</Button>
        </div>
    </div>
</div>

<style lang="scss">
    .onboarding-guide {
        background: var(--mica);
        position: absolute;
        top: 15%;
        left: 30%;
        border-radius: 8px;
        overflow: hidden;
        width: 550px;
        height: auto;
        min-height: 580px;
        display: flex;
        flex-direction: column;
        box-shadow: 0 8px 32px rgba(0, 0, 0, 0.4);
    }

    .mainApp {
        padding: 0;
        position: relative;
        display: flex;
        flex-direction: column;
        overflow-y: auto;
        overflow-x: hidden;
        flex: 1;
        background: rgb(var(--bg2));
    }

    .progress-bar-container {
        padding: 30px 0 20px 0;
        display: flex;
        justify-content: center;
    }

    .progress-steps {
        display: flex;
        align-items: center;
    }

    .step-indicator {
        width: 28px;
        height: 28px;
        border-radius: 50%;
        background: rgb(var(--clr) / 10%);
        display: flex;
        align-items: center;
        justify-content: center;
        font-size: 13px;
        font-weight: 600;
        color: rgb(var(--clr) / 60%);
        transition: all 0.3s ease;

        &.active {
            background: rgb(var(--clrPrm));
            color: white;
            box-shadow: 0 0 10px rgb(var(--clrPrm) / 40%);
        }
    }

    .step-line {
        width: 60px;
        height: 2px;
        background: rgb(var(--clr) / 10%);
        transition: all 0.3s ease;
        margin: 0 8px;

        &.active {
            background: rgb(var(--clrPrm));
        }
    }

    .slides-wrapper {
        position: relative;
        flex: 1;
        display: flex;
        overflow: hidden;
        min-height: 320px;
    }

    .slide {
        position: absolute;
        width: 100%;
        height: 100%;
        display: flex;
        flex-direction: column;
        align-items: center;
        padding: 0 30px 20px;
        text-align: center;
    }

    .device-grid {
        display: flex;
        flex-direction: column;
        gap: 12px;
    }

    .device-card {
        display: flex;
        align-items: center;
        padding: 16px 20px;
        background: rgb(var(--bg3));
        border: 1px solid rgb(var(--clr) / 10%);
        border-radius: 8px;
        cursor: pointer;
        transition: all 0.2s;
        width: 100%;

        &:hover {
            background: rgb(var(--bg3) / 80%);
            border-color: rgb(var(--clrPrm) / 40%);
            transform: translateY(-2px);
        }

        &.selected {
            border-color: rgb(var(--clrPrm));
            background: rgb(var(--clrPrm) / 10%);
        }

        .icon {
            width: 40px;
            height: 40px;
            display: flex;
            align-items: center;
            justify-content: center;
            margin-right: 16px;
            color: rgb(var(--clr));
        }

        .info {
            flex: 1;
        }
    }

    .options-grid {
        display: grid;
    }

    .config-error {
        width: min(100%, 640px);
        margin: 0 auto 16px;
        padding: 12px 14px;
        border-radius: 10px;
        border: 1px solid rgba(255, 92, 92, 0.35);
        background: rgba(120, 21, 21, 0.2);
        color: rgba(255, 214, 214, 0.95);
        font-size: 0.92rem;
        text-align: left;
    }

    .option-card {
        background: rgb(var(--bg3));
        border: 1px solid rgb(var(--clr) / 10%);
        border-radius: 8px;
        padding: 24px;
        display: flex;
        flex-direction: column;
        align-items: center;
        transition: all 0.2s;
        cursor: pointer;

        &:hover {
            background: rgb(var(--bg3) / 80%);
            border-color: rgb(var(--clrPrm));
            transform: translateY(-2px);

            .opt-icon {
                color: rgb(var(--clrPrm));
                background: rgb(var(--clrPrm) / 10%);
            }
        }

        &.busy {
            cursor: progress;
            opacity: 0.72;
            pointer-events: none;
            transform: none;
        }
    }

    .footer-nav {
        display: flex;
        justify-content: space-between;
        padding: 16px 24px;
        border-top: 1px solid rgb(var(--clr) / 10%);
    }

    @media (prefers-color-scheme: dark) {
        .onboarding-guide {
            background: hsla(0, 0%, 10%, 0.95);
            backdrop-filter: blur(20px);
        }
    }
</style>
