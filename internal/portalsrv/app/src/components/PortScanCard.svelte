<script lang="ts">
    import { createEventDispatcher, onMount, onDestroy } from "svelte";
    import { peerStore } from "$store/peer";
    import { _ } from "$store/i18n";
    import { scale, fly, slide } from "svelte/transition";
    import { tweened } from "svelte/motion";
    import { cubicOut } from "svelte/easing";
    import {
        Button,
        TextBox,
        ToggleSwitch,
        ProgressRing,
        ProgressBar,
        RadioButton,
    } from "fluent-svelte";

    import { commonPorts } from "$lib/commonPorts";

    export let peerId: string;
    export let scanId: string | undefined = undefined;
    export let initialProgress: number = 0;

    const dispatch = createEventDispatcher();

    // Scan Configuration
    let scanMode: "common" | "full" | "custom" | string = "common";
    let customPortsInput = "";
    let scanTCP = true;
    let scanUDP = false;
    let isConfiguring = !scanId;

    // Status & Progress
    let progress = initialProgress;
    let status = "idle";
    let openPorts: any[] = [];
    let currentPort = 0;

    // Animated Port Counter
    const displayedPort = tweened(0, {
        duration: 800,
        easing: cubicOut,
    });

    $: displayedPort.set(currentPort);

    let totalPorts = 0;
    let startTime = Date.now();
    let elapsedTime = "00:00";
    let timerInterval: any;
    let message = "";
    let progressBarValue = Math.min(100, initialProgress);
    let progressLabel = `${Math.round(initialProgress)}%`;
    let activityLabel = "";

    // The backend rounds percent to int32 — for the first ~10% of any large
    // scan that floors to 0%, which used to make the bar look frozen. Show
    // a small but visible bar value while the scan is genuinely making
    // progress (currentPort > 0) so the user can tell it's alive.
    $: progressBarValue =
        status === "running" && progress <= 0 && currentPort > 0
            ? 0.5
            : Math.min(100, progress);
    $: progressLabel =
        status === "running" && progress <= 0 && currentPort > 0
            ? totalPorts > 0
                ? `${currentPort} / ${totalPorts}`
                : "<1%"
            : status === "running" && progress < 1 && totalPorts > 0
              ? `${Math.round(progress)}% (${currentPort}/${totalPorts})`
              : `${Math.round(progress)}%`;
    $: activityLabel =
        status === "running" && currentPort > 0
            ? totalPorts > 0
                ? `Checking port ${currentPort.toLocaleString()} of ${totalPorts.toLocaleString()}`
                : `Checking port ${currentPort.toLocaleString()}`
            : "";

    // Stream control
    let unsubscribe: () => void;
    let activeStreamKey = "";

    // Safety net for stuck "stopping" state. The backend publishes a final
    // "stopped" status update via Redis after the scanner honors Cancel(),
    // but there are real scenarios where that message can be missed:
    //   - Redis pub/sub dropped a message under load
    //   - the periodic worker finished and exited before the subscriber
    //     attached (Cancel() returns synchronously, but ScanPorts() may
    //     still be unwinding in-flight TCP probes)
    //   - the gRPC StreamPortScanStatus closed before forwarding the event
    // Without this, the UI shows "stopping..." forever. Cap the optimistic
    // state at 15s — long enough for real cancellations of slow scans,
    // short enough that a missed event surfaces as a definite "stopped".
    const STOPPING_TIMEOUT_MS = 15_000;
    let stoppingTimer: ReturnType<typeof setTimeout> | null = null;

    function clearStoppingTimer() {
        if (stoppingTimer) {
            clearTimeout(stoppingTimer);
            stoppingTimer = null;
        }
    }
    function armStoppingTimer() {
        clearStoppingTimer();
        stoppingTimer = setTimeout(() => {
            if (status === "stopping") {
                status = "stopped";
                message = "Scan stopped";
                stopTimer();
                emitStatus();
            }
            stoppingTimer = null;
        }, STOPPING_TIMEOUT_MS);
    }

    function emitStatus() {
        dispatch("status", {
            scanId,
            status,
            progress,
            currentPort,
            totalPorts,
            message,
        });
    }

    onMount(() => {
        if (scanId) {
            isConfiguring = false;
            startTimer();
        }
    });

    $: {
        const nextStreamKey = scanId
            ? `scan:${scanId}`
            : !isConfiguring
              ? `peer:${peerId}`
              : "";

        if (nextStreamKey !== activeStreamKey) {
            if (unsubscribe) {
                unsubscribe();
                unsubscribe = undefined;
            }

            activeStreamKey = nextStreamKey;

            if (nextStreamKey) {
                startStream(scanId);
            }
        }
    }

    onDestroy(() => {
        if (unsubscribe) unsubscribe();
        clearStoppingTimer();
        stopTimer();
    });

    function startStream(sid?: string) {
        status = "running";
        unsubscribe = peerStore.streamScanStatus(peerId, sid, {
            onData: (data: any) => {
                // Handle both snake_case (proto default) and camelCase (JS default)
                const sId = data.scan_id || data.scanId || data.id;
                const pPercent = data.progress_percent ?? data.progressPercent;
                const stat = data.status;
                const cPort = data.current_port || data.currentPort;
                const tPorts = data.total_ports || data.totalPorts;
                const lFound = data.last_found_port || data.lastFoundPort;
                const msg = data.message;

                if (sId && !scanId) {
                    scanId = sId;
                    isConfiguring = false;
                }
                if (pPercent !== undefined) progress = pPercent;
                if (stat) status = stat;
                if (cPort !== undefined) currentPort = cPort;
                if (tPorts !== undefined) totalPorts = tPorts;

                if (lFound) {
                    const exists = openPorts.find(
                        (p) => p.port === lFound.port,
                    );
                    if (!exists) {
                        openPorts = [...openPorts, lFound];
                    }
                }
                if (msg) message = msg;

                openPorts = openPorts; // trigger reactivity

                if (
                    status === "completed" ||
                    status === "failed" ||
                    status === "stopped"
                ) {
                    clearStoppingTimer();
                    stopTimer();
                }
                emitStatus();
            },
            onError: (err) => {
                console.error("Scan stream error:", err);
                status = "failed";
                message = err;
                clearStoppingTimer();
                stopTimer();
                emitStatus();
            },
            onEnd: () => {
                // The stream closed without a terminal status frame.
                // Resolve any in-flight state to its natural terminal so
                // the UI never shows a transient state forever.
                if (status === "running") status = "completed";
                else if (status === "stopping" || status === "paused") {
                    status = "stopped";
                }
                clearStoppingTimer();
                stopTimer();
                emitStatus();
            },
        });
    }

    async function handleStartScan() {
        isConfiguring = false;
        openPorts = [];
        progress = 0;
        message = "Starting scan...";
        let ports: number[] = [];
        let fullScan = false;

        if (scanMode === "custom") {
            ports = customPortsInput
                .split(/[\s,]+/)
                .map((p) => {
                    if (p.includes("-")) {
                        const [start, end] = p.split("-").map(Number);
                        if (!isNaN(start) && !isNaN(end)) {
                            const r = [];
                            for (let i = start; i <= end; i++) r.push(i);
                            return r;
                        }
                    }
                    return parseInt(p);
                })
                .flat()
                .filter((p) => !isNaN(p) && p > 0 && p <= 65535);
        } else if (scanMode === "full") {
            fullScan = true;
        } else if (scanMode === "common") {
            ports = commonPorts;
        }

        try {
            const res = await peerStore.startPortScan(
                peerId,
                fullScan,
                ports,
                scanTCP,
                scanUDP,
            );
            scanId = res.scan_id || res.scanId || res.id;
            startTime = Date.now();
            startTimer();
            emitStatus();
        } catch (err: any) {
            console.error("Failed to start scan:", err);
            status = "failed";
            message = err.message || "Failed to start scan";
            isConfiguring = true;
            emitStatus();
        }
    }

    function startTimer() {
        stopTimer();
        timerInterval = setInterval(() => {
            const diff = Math.floor((Date.now() - startTime) / 1000);
            const mins = Math.floor(diff / 60)
                .toString()
                .padStart(2, "0");
            const secs = (diff % 60).toString().padStart(2, "0");
            elapsedTime = `${mins}:${secs}`;
        }, 1000);
    }

    function stopTimer() {
        if (timerInterval) clearInterval(timerInterval);
    }

    // Helper to determine service name
    function getServiceName(port: any) {
        if (port.service && port.service !== "unknown") return port.service;
        const common: Record<number, string> = {
            22: "SSH",
            80: "HTTP",
            443: "HTTPS",
            53: "DNS",
            21: "FTP",
            23: "Telnet",
            3306: "MySQL",
            5432: "Postgres",
            8291: "Winbox",
        };
        return common[port.port] || (port.protocol === "udp" ? "UDP" : "TCP");
    }
</script>

<div
    class="flex flex-col h-full w-full bg-[var(--bg1)] text-[rgb(var(--clr)/90%)] rounded-lg overflow-hidden border border-[var(--border-color)] shadow-sm transition-colors duration-200"
>
    {#if isConfiguring}
        <div
            class="p-4 sm:p-6 flex flex-col gap-4 sm:gap-6"
            in:scale={{ start: 0.95, duration: 200 }}
        >
            <h3 class="text-lg font-semibold">Start Port Scan</h3>

            <div class="flex flex-col gap-2 mb-4">
                <h4 class="text-sm font-medium">Scan Mode</h4>
                <div class="flex gap-4">
                    <RadioButton bind:group={scanMode} value="common"
                        >Common</RadioButton
                    >
                    <RadioButton bind:group={scanMode} value="full"
                        >Full</RadioButton
                    >
                    <RadioButton bind:group={scanMode} value="custom"
                        >Custom</RadioButton
                    >
                </div>
                <div class="text-xs text-[rgb(var(--clr)/50%)] h-4 mt-1">
                    {#if scanMode === "common"}Fast scan of top 1000 ports
                    {:else if scanMode === "full"}Scan all 65535 ports (Slow)
                    {:else}Specify specific ports or ranges{/if}
                </div>
            </div>

            {#if scanMode === "custom"}
                <div class="flex flex-col gap-2" transition:slide>
                    <label
                        for="ports"
                        class="text-sm font-medium text-[rgb(var(--clr)/70%)]"
                        >Ports (e.g. 80, 443, 8000-8080)</label
                    >
                    <TextBox
                        placeholder="80, 443, 8000-8080"
                        bind:value={customPortsInput}
                    />
                </div>
            {/if}

            <div
                class="flex items-center justify-between py-2 border-t border-[rgb(var(--clr)/5%)] mt-2"
            >
                <span class="text-sm font-medium text-[rgb(var(--clr)/70%)]"
                    >Protocols</span
                >
                <div class="flex gap-4">
                    <ToggleSwitch bind:checked={scanTCP}>TCP</ToggleSwitch>
                    <ToggleSwitch bind:checked={scanUDP}>UDP</ToggleSwitch>
                </div>
            </div>

            {#if status === "failed" && message}
                <div
                    class="text-sm text-[var(--error)] bg-[rgba(var(--error-rgb),0.1)] p-2 rounded"
                >
                    {message}
                </div>
            {/if}

            <div
                class="mt-auto flex flex-col-reverse sm:flex-row justify-end gap-3 pt-4"
            >
                <Button
                    class="w-full sm:w-auto"
                    on:click={() => dispatch("close")}>Cancel</Button
                >
                <Button
                    class="w-full sm:w-auto"
                    variant="accent"
                    on:click={handleStartScan}
                    disabled={!scanTCP && !scanUDP}
                >
                    Start Scan
                </Button>
            </div>
        </div>
    {:else}
        <div class="flex flex-col h-full bg-[var(--bg1)]">
            <!-- Header Status Area -->
            <div
                class="p-4 border-b border-[var(--border-color)] flex flex-col gap-4"
            >
                <div
                    class="flex flex-col sm:flex-row justify-between items-start gap-2 sm:gap-0"
                >
                    <div>
                        <div
                            class="text-xs font-mono text-[rgb(var(--clr)/50%)] mb-1"
                        >
                            SCAN_ID: {scanId?.slice(0, 8)}
                        </div>
                        <h3
                            class="text-lg font-bold flex items-center gap-2 text-[rgb(var(--clr)/90%)]"
                        >
                            {#if status === "running"}
                                <ProgressRing
                                    size={16}
                                    class="text-[var(--primary)]"
                                />
                                <span class="text-[var(--primary)]"
                                    >Scanning...</span
                                >
                            {:else if status === "completed"}
                                <span
                                    class="h-3 w-3 rounded-full bg-[var(--success)]"
                                ></span>
                                <span class="text-[var(--success)]"
                                    >Complete</span
                                >
                            {:else if status === "stopped"}
                                <span
                                    class="h-3 w-3 rounded-full bg-[rgb(var(--clr)/45%)]"
                                ></span>
                                <span>Stopped</span>
                            {:else if status === "failed"}
                                <span
                                    class="h-3 w-3 rounded-full bg-[var(--error)]"
                                ></span>
                                <span class="text-[var(--error)]">Failed</span>
                            {:else}
                                <span
                                    class="h-3 w-3 rounded-full bg-[var(--warning)]"
                                ></span>
                                <span>{status}</span>
                            {/if}
                        </h3>
                        {#if status === "failed" && message}
                            <div class="text-xs text-[var(--error)] mt-1">
                                {message}
                            </div>
                        {/if}
                    </div>
                    <div
                        class="text-left sm:text-right w-full sm:w-auto flex justify-between sm:block items-end border-t sm:border-t-0 border-[var(--border-color)] pt-2 sm:pt-0 mt-2 sm:mt-0"
                    >
                        <div
                            class="sm:hidden text-xs text-[rgb(var(--clr)/50%)]"
                        >
                            Duration
                        </div>
                        <div>
                            <div
                                class="text-2xl font-mono font-light text-[rgb(var(--clr)/90%)]"
                            >
                                {elapsedTime}
                            </div>
                            <div
                                class="hidden sm:block text-xs text-[rgb(var(--clr)/50%)]"
                            >
                                Elapsed
                            </div>
                        </div>
                    </div>
                </div>

                <!-- Progress Bar -->
                <div class="flex flex-col gap-1 mt-2">
                    <ProgressBar
                        value={progressBarValue}
                        class="w-full"
                    />
                    <div class="flex items-center justify-between gap-3">
                        <div class="text-xs text-[rgb(var(--clr)/55%)]">
                            {#if activityLabel}
                                {activityLabel}
                            {:else if status === "completed"}
                                Scan finished
                            {:else if status === "stopped"}
                                Scan stopped
                            {:else if message}
                                {message}
                            {/if}
                        </div>
                        <div class="text-right text-xs text-[rgb(var(--clr)/50%)]">
                            {progressLabel}
                        </div>
                    </div>
                </div>
            </div>

            <!-- Results Grid -->
            <div class="flex-1 overflow-y-auto p-4 bg-[rgb(var(--bg1)/50%)]">
                {#if openPorts.length === 0}
                    <div
                        class="h-full flex flex-col items-center justify-center text-[rgb(var(--clr)/40%)]"
                    >
                        {#if status === "running"}
                            <div class="animate-pulse">
                                Searching for open ports...
                            </div>
                        {:else if status === "failed"}
                            <div class="text-[var(--error)]">Scan failed</div>
                        {:else}
                            <div>No open ports found.</div>
                        {/if}
                    </div>
                {:else}
                    <div class="grid grid-cols-2 sm:grid-cols-3 gap-3">
                        {#each openPorts as p (p.port)}
                            <div
                                class="bg-[rgb(var(--bg2)/80%)] border border-[var(--border-color)] p-3 rounded-lg shadow-sm flex items-center justify-between backdrop-blur-sm"
                                in:fly={{ y: 20, duration: 300 }}
                            >
                                <div>
                                    <div
                                        class="text-lg font-bold text-[var(--success)]"
                                    >
                                        {p.port}
                                    </div>
                                </div>
                                <div
                                    class="text-xs font-medium px-2 py-1 rounded bg-[rgb(var(--bg1))] text-[rgb(var(--clr)/80%)] border border-[var(--border-color)]"
                                >
                                    {getServiceName(p)}
                                </div>
                            </div>
                        {/each}
                    </div>
                {/if}
            </div>

            <!-- Actions Footer -->
            <div
                class="p-3 bg-gray-50 dark:bg-gray-800 border-t border-gray-200 dark:border-gray-700 flex flex-col sm:flex-row justify-end gap-2"
            >
                {#if status !== "completed" && status !== "failed" && status !== "stopped"}
                    <Button
                        class="text-red-600 hover:text-red-700 w-full sm:w-auto"
                        on:click={async () => {
                            if (!scanId) {
                                dispatch("close");
                                return;
                            }
                            // Optimistic: flip local state immediately so the
                            // user sees the click took effect even before the
                            // backend's status update arrives over the stream.
                            // armStoppingTimer caps the optimistic state so a
                            // missed final event from the backend never leaves
                            // the UI hanging forever.
                            const prevStatus = status;
                            status = "stopping";
                            message = "Stopping scan...";
                            armStoppingTimer();
                            emitStatus();
                            try {
                                await peerStore.stopPortScan(peerId, scanId);
                            } catch (err) {
                                clearStoppingTimer();
                                status = prevStatus;
                                message = err?.message || "Failed to stop scan";
                                emitStatus();
                            }
                        }}
                    >
                        Stop Scan
                    </Button>
                    {#if status === "running"}
                        <Button
                            class="w-full sm:w-auto"
                            on:click={async () => {
                                if (!scanId) return;
                                const prevStatus = status;
                                status = "paused";
                                message = "Pausing scan...";
                                emitStatus();
                                try {
                                    await peerStore.pausePortScan(
                                        peerId,
                                        scanId,
                                    );
                                } catch (err) {
                                    status = prevStatus;
                                    message =
                                        err?.message || "Failed to pause scan";
                                    emitStatus();
                                }
                            }}
                        >
                            Pause
                        </Button>
                    {:else if status === "paused"}
                        <Button
                            class="w-full sm:w-auto"
                            variant="accent"
                            on:click={async () => {
                                if (!scanId) return;
                                const prevStatus = status;
                                status = "running";
                                message = "Resuming scan...";
                                emitStatus();
                                try {
                                    await peerStore.resumePortScan(
                                        peerId,
                                        scanId,
                                    );
                                } catch (err) {
                                    status = prevStatus;
                                    message =
                                        err?.message || "Failed to resume scan";
                                    emitStatus();
                                }
                            }}
                        >
                            Resume
                        </Button>
                    {/if}
                {:else}
                    <Button
                        class="w-full sm:w-auto"
                        on:click={() => dispatch("close")}>Close</Button
                    >
                {/if}
            </div>
        </div>
    {/if}
</div>
