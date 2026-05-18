<script lang="ts">
  import { toUTCMillis, DAY_MS, formatLocalDate } from "$lib/dateUtils";
  import { onMount } from "svelte";
  import { fly } from "svelte/transition";
  import type { Handshake } from "$store/peer";
  import { _ } from "$store/i18n";

  export let uptimeHistoryBytes: Uint8Array | string | null = null;

  // Timeframe options - Always 30 days duration
  const TIMEFRAMES = [
    { label: "Day", id: "day", duration: DAY_MS * 30, interval: DAY_MS },
    {
      label: "Hour",
      id: "hour",
      duration: DAY_MS * 30,
      interval: 60 * 60 * 1000,
    },
    {
      label: "30m",
      id: "30m",
      duration: DAY_MS * 30,
      interval: 30 * 60 * 1000,
    },
    {
      label: "15m",
      id: "15m",
      duration: DAY_MS * 30,
      interval: 15 * 60 * 1000,
    },
  ];

  let selectedTimeframeId = "day";
  $: selectedTimeframe =
    TIMEFRAMES.find((t) => t.id === selectedTimeframeId) || TIMEFRAMES[0];

  $: chartStats = processHistory(uptimeHistoryBytes, selectedTimeframe);

  function decodeHistoryBytes(bytes: Uint8Array | string | null): number[] {
    if (!bytes || bytes.length === 0) return [];
    
    let buffer: Uint8Array;
    if (typeof bytes === "string") {
        try {
            const binary_string = window.atob(bytes);
            const len = binary_string.length;
            buffer = new Uint8Array(len);
            for (let i = 0; i < len; i++) {
                buffer[i] = binary_string.charCodeAt(i);
            }
        } catch (e) {
            console.error("Failed to decode base64 history bytes", e);
            return [];
        }
    } else {
        buffer = bytes;
    }

    // Decode array of 32-bit big-endian unsigned integers representing UNIX seconds
    const count = Math.floor(buffer.length / 4);
    const view = new DataView(buffer.buffer, buffer.byteOffset, buffer.byteLength);
    const result = [];
    for (let i = 0; i < count; i++) {
        result.push(view.getUint32(i * 4, false) * 1000); // converting seconds to ms
    }
    return result;
  }

  function processHistory(
    historyBytes: Uint8Array | string | null,
    timeframe: (typeof TIMEFRAMES)[0],
  ) {
    const historyMs = decodeHistoryBytes(historyBytes);
    const now = Date.now();
    // Limit max slots to 3000 to avoid performance issues
    const rawNumSlots = Math.floor(timeframe.duration / timeframe.interval);
    const numSlots = Math.min(3000, rawNumSlots);

    // Find the earliest handshake to determine onboarding time
    let onboardedAt = now; // Default to now if no history
    if (historyMs.length > 0) {
      onboardedAt = Math.min(...historyMs);
    }

    const stats = Array(numSlots)
      .fill(0)
      .map((_, i) => {
        const slotEnd = now - (numSlots - 1 - i) * timeframe.interval;
        const slotStart = slotEnd - timeframe.interval;

        // Check if this slot is before device was onboarded
        const isBeforeOnboarding = slotEnd < onboardedAt;

        return {
          date: new Date(slotStart),
          endDate: new Date(slotEnd),
          count: 0,
          status: isBeforeOnboarding
            ? ("unknown" as "online" | "degraded" | "offline" | "unknown")
            : ("offline" as "online" | "degraded" | "offline" | "unknown"),
        };
      });

    historyMs.forEach((millis) => {
      if (millis < now - timeframe.duration) return;

      const diff = now - millis;
      const slotIndex = numSlots - 1 - Math.floor(diff / timeframe.interval);

      if (slotIndex >= 0 && slotIndex < numSlots) {
        stats[slotIndex].count++;
      }
    });

    const expectedPerMin = 1 / 5; // One check every 5 mins
    const minsInInterval = timeframe.interval / 1000 / 60;
    // Reduce strictness for minimum counting (0.5 means half expected pings are enough for fully online green bar)
    const expectedCount = Math.max(1, minsInInterval * expectedPerMin);

    stats.forEach((s) => {
      // Skip unknown slots - they stay as unknown
      if (s.status === "unknown") return;

      if (s.count === 0) {
        s.status = "offline";
      } else if (s.count >= expectedCount * 0.4) {
        s.status = "online";
      } else {
        s.status = "degraded";
      }
    });

    return stats;
  }

  let hoveredIndex: number | null = null;

  function formatTooltipTime(date: Date, timeframeId: string) {
    if (timeframeId === "day") return formatLocalDate(date);
    return date.toLocaleString([], {
      month: "short",
      day: "numeric",
      hour: "2-digit",
      minute: "2-digit",
    });
  }

  let container: HTMLElement;
  let isScrolling = false;
  let scrollDirection: "left" | "right" | null = null;
  let animationFrame: number;

  const SCROLL_SPEED = 10;
  const SCROLL_THRESHOLD = 50; // px

  function updateScroll() {
    if (!isScrolling || !container || !scrollDirection) {
      cancelAnimationFrame(animationFrame);
      return;
    }

    container.scrollLeft +=
      scrollDirection === "right" ? SCROLL_SPEED : -SCROLL_SPEED;
    animationFrame = requestAnimationFrame(updateScroll);
  }

  function handleMouseMove(e: MouseEvent) {
    if (!container) return;

    const rect = container.getBoundingClientRect();
    const x = e.clientX - rect.left;

    if (x < SCROLL_THRESHOLD) {
      if (!isScrolling || scrollDirection !== "left") {
        isScrolling = true;
        scrollDirection = "left";
        cancelAnimationFrame(animationFrame);
        updateScroll();
      }
    } else if (x > rect.width - SCROLL_THRESHOLD) {
      if (!isScrolling || scrollDirection !== "right") {
        isScrolling = true;
        scrollDirection = "right";
        cancelAnimationFrame(animationFrame);
        updateScroll();
      }
    } else {
      isScrolling = false;
      scrollDirection = null;
      cancelAnimationFrame(animationFrame);
    }
  }

  function handleMouseLeave() {
    isScrolling = false;
    scrollDirection = null;
    cancelAnimationFrame(animationFrame);
  }

  // Drag to scroll implementation
  let isDragging = false;
  let startX: number;
  let initialScrollLeft: number;

  function handleMouseDown(e: MouseEvent) {
    if (!container) return;
    isDragging = true;
    container.style.cursor = "grabbing";
    startX = e.clientX;
    initialScrollLeft = container.scrollLeft;
  }

  function handleWindowMouseUp() {
    isDragging = false;
    if (container) {
      container.style.cursor = "grab";
    }
  }

  function handleWindowMouseMove(e: MouseEvent) {
    if (!isDragging || !container) return;
    e.preventDefault();
    const walk = (e.clientX - startX) * 2;
    container.scrollLeft = initialScrollLeft - walk;
  }

  function handleContainerMouseLeave() {
    if (!isDragging) {
      hoveredIndex = null;
    }
  }

  function setTimeframe(id: string) {
    selectedTimeframeId = id;
    scrollToEnd();
  }

  // Scroll to end only when timeframe changes (handled by setTimeframe) or on mount
  // Reactive statement removed to prevent snapping
  /* $: if (selectedTimeframeId && container) {
      scrollToEnd();
  } */

  function scrollToEnd() {
    // Small timeout to ensure DOM update (if any) is complete
    setTimeout(() => {
      if (container) {
        container.scrollLeft = container.scrollWidth;
      }
    }, 0);
  }

  onMount(() => {
    scrollToEnd();
  });
</script>

<svelte:window
  on:mouseup={handleWindowMouseUp}
  on:mousemove={handleWindowMouseMove}
/>

<div
  class="uptime-container"
  on:mouseleave={handleContainerMouseLeave}
  style="user-select: none;"
>
  <div class="chart-header">
    <div class="header-left">
      {#if hoveredIndex !== null && chartStats[hoveredIndex]}
        <div class="tooltip-data" transition:fly={{ y: 5, duration: 150 }}>
          <span class="date">
            {formatTooltipTime(
              chartStats[hoveredIndex].date,
              selectedTimeframeId,
            )}
          </span>
          <span class="count">
            {chartStats[hoveredIndex].count}
          </span>
          <span class="status {chartStats[hoveredIndex].status}">
            {#if chartStats[hoveredIndex].status === "online"}
              Operational
            {:else if chartStats[hoveredIndex].status === "degraded"}
              Degraded
            {:else if chartStats[hoveredIndex].status === "unknown"}
              Not Onboarded
            {:else}
              Offline
            {/if}
          </span>
        </div>
      {:else}
        <div class="label-wrapper">
          <span class="label">Uptime History (30d)</span>
        </div>
      {/if}
    </div>
  </div>
  <div class="chart-footer">
    <div class="controls">
      {#each TIMEFRAMES as tf}
        <button
          class="timeframe-btn"
          class:active={selectedTimeframeId === tf.id}
          on:click={() => setTimeframe(tf.id)}
        >
          {tf.label}
        </button>
      {/each}
    </div>
  </div>
  <div
    class="bars-container"
    bind:this={container}
    class:high-density={chartStats.length > 500}
    class:med-density={chartStats.length > 100 && chartStats.length <= 500}
    class:low-density={chartStats.length <= 100}
    on:mousedown={handleMouseDown}
    role="group"
  >
    {#each chartStats as slot, i}
      <button
        class="bar-wrapper"
        on:mouseenter={() => (hoveredIndex = i)}
        on:click={() => (hoveredIndex = i)}
        on:focus={() => (hoveredIndex = i)}
        aria-label="Status: {slot.status}, Date: {slot.date}"
      >
        <div class="bar {slot.status}" />
      </button>
    {/each}
  </div>
</div>

<style>
  .uptime-container {
    display: flex;
    flex-direction: column;
    width: 100%;
    height: 180px; /* Matching ping card height */
    background: rgb(var(--bg2) / 40%);
    backdrop-filter: var(--glass-blur);
    border: 1px solid var(--border-color);
    border-radius: var(--radius-lg);
    position: relative;
    overflow: hidden;
    box-shadow: var(--shadow-sm);
    transition: all 0.3s cubic-bezier(0.4, 0, 0.2, 1);
  }

  .uptime-container:hover {
    background: rgb(var(--bg2) / 60%);
    box-shadow: var(--shadow-md);
    border-color: rgb(var(--clr) / 15%);
  }

  .chart-header {
    position: absolute;
    top: 12px;
    left: 0;
    right: 0;
    display: flex;
    justify-content: space-between;
    align-items: center;
    padding: 0 12px;
    z-index: 50;
    pointer-events: none;
  }

  .header-left,
  .controls {
    pointer-events: auto;
  }

  .header-left {
    flex: 1;
    display: flex;
    align-items: center;
    min-width: 0;
    height: 32px;
  }

  .label-wrapper,
  .tooltip-data {
    background: rgb(var(--bg2) / 80%);
    backdrop-filter: blur(12px) saturate(180%);
    border: 1px solid rgb(var(--clr) / 15%);
    padding: 6px 12px;
    border-radius: var(--radius-md);
    box-shadow: 0 4px 16px rgba(0, 0, 0, 0.15);
    display: flex;
    align-items: center;
    white-space: nowrap;
  }

  .label {
    font-size: 10px;
    font-weight: 800;
    color: rgb(var(--clr) / 80%);
    letter-spacing: 0.05em;
    text-transform: uppercase;
  }

  .tooltip-data {
    gap: 8px;
    animation: slideIn 0.2s cubic-bezier(0, 0, 0.2, 1);
  }

  @keyframes slideIn {
    from {
      opacity: 0;
      transform: translateY(4px);
    }
    to {
      opacity: 1;
      transform: translateY(0);
    }
  }

  .tooltip-data .date {
    color: rgb(var(--clr) / 80%);
    font-family: var(--font-mono);
    font-weight: 600;
    font-size: 11px;
  }

  .status {
    font-weight: 800;
    text-transform: uppercase;
    font-size: 9px;
    letter-spacing: 0.05em;
    padding: 2px 6px;
    border-radius: 4px;
  }

  .status.online,
  .status.Online {
    color: #10b981;
    background: rgb(16 185 129 / 25%);
  }
  .status.degraded,
  .status.Degraded {
    color: #f59e0b;
    background: rgb(245 158 11 / 25%);
  }
  .status.offline,
  .status.Offline {
    color: #ef4444;
    background: rgb(239 68 68 / 25%);
  }
  .status.unknown {
    color: #6b7280;
    background: rgb(107 114 128 / 25%);
  }
  .controls {
    display: flex;
    gap: 2px;
    background: rgb(var(--bg2) / 80%);
    backdrop-filter: blur(12px) saturate(180%);
    padding: 4px;
    border-radius: var(--radius-md);
    border: 1px solid rgb(var(--clr) / 15%);
    box-shadow: 0 4px 16px rgba(0, 0, 0, 0.15);
    margin-left: 12px;
  }

  .timeframe-btn {
    background: transparent;
    border: none;
    color: rgb(var(--clr) / 40%);
    font-size: 10px;
    font-weight: 700;
    padding: 4px 10px;
    border-radius: 4px;
    cursor: pointer;
    transition: all 0.2s ease;
    text-transform: uppercase;
    white-space: nowrap;
  }

  .timeframe-btn:hover {
    color: rgb(var(--clr) / 70%);
    background: rgb(var(--clr) / 5%);
  }

  .timeframe-btn.active {
    background: var(--primary);
    color: #fff;
  }

  .bars-container {
    display: flex;
    align-items: flex-end;
    width: 100%;
    height: 100%;
    background: rgb(var(--clr) / 3%);
    overflow: hidden;
    position: absolute;
    inset: 0;
    z-index: 10;
    padding: 0;
    overflow-x: auto;
    scrollbar-width: none; /* Firefox */
    -ms-overflow-style: none; /* IE/Edge */
    /* Ensure smooth touch scrolling on mobile */
    -webkit-overflow-scrolling: touch;
    touch-action: pan-x;
  }

  .bars-container::-webkit-scrollbar {
    display: none;
  }

  .bar-wrapper {
    /* Remove flex: 1 to prevent shrinking below legible width */
    /* flex: 1; */
    height: 100%;
    display: flex;
    align-items: flex-end;
    cursor: crosshair;
    min-width: 10px; /* Slightly wider for better visibility on mobile */
    position: relative;
    flex-grow: 1; /* Allow growing but respect min-width */
    flex-shrink: 0; /* Prevent shrinking below min-width */
    background: none;
    border: none;
    padding: 0;
    margin: 0;
  }

  .bar {
    width: 100%;
    height: 100%;
    transition: all 0.15s ease;
    opacity: 0.5;
    border-left: 1px solid rgb(var(--clr) / 10%); /* Increased visibility */
  }

  .bar-wrapper:hover .bar {
    opacity: 1;
    z-index: 20;
  }

  .bar.online {
    background: var(--success);
  }
  .bar.degraded {
    background: var(--warning);
  }
  .bar.offline {
    background: var(--error);
  }
  .bar.unknown {
    background: #4b5563;
    opacity: 0.3;
  }

  .bar-wrapper:hover .bar.online {
    box-shadow: 0 0 20px rgb(var(--success-rgb) / 50%);
  }
  .bar-wrapper:hover .bar.degraded {
    box-shadow: 0 0 20px rgb(var(--warning-rgb) / 50%);
  }
  .bar-wrapper:hover .bar.offline {
    box-shadow: 0 0 20px rgb(var(--error-rgb) / 50%);
  }
  .bar-wrapper:hover .bar.unknown {
    box-shadow: 0 0 10px rgb(75 85 99 / 30%);
    opacity: 0.5;
  }

  /* Density handling */
  .bars-container.high-density {
    gap: 0;
  }
  .bars-container.med-density {
    gap: 0.5px;
  }
  .bars-container.low-density {
    gap: 1.5px;
  }
  .chart-footer {
    position: absolute;
    left: 0;
    right: 0;
    z-index: 50;
    display: flex;
    justify-content: space-between;
    align-items: center;
    padding: 0 8px;
    bottom: 8px;
  }
  /* Mobile refinements */
  @media (max-width: 640px) {
    .uptime-container {
      height: 150px;
    }

    .chart-header {
      flex-direction: column;
      align-items: flex-start;
      gap: 8px;
      top: 8px;
      padding: 0 8px;
    }
    .chart-footer {
      position: absolute;
      left: 0;
      right: 0;
      display: flex;
      justify-content: space-between;
      align-items: center;
      padding: 0 8px;
      bottom: 8px;
    }
    .header-left {
      width: 100%;
    }

    .controls {
      margin-left: 0;
      width: 100%;
      justify-content: space-between;
    }

    .timeframe-btn {
      flex: 1;
      padding: 4px 2px;
      font-size: 9px;
      text-align: center;
    }

    .label-wrapper,
    .tooltip-data {
      padding: 4px 10px;
      width: fit-content;
      justify-content: space-between;
    }

    /* Ensure bars container allows scrolling */
    .bars-container {
      overflow-x: auto;
      -webkit-overflow-scrolling: touch;
    }
  }
</style>
