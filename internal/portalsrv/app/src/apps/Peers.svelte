<script lang="ts">
  import { draggable } from "@neodrag/svelte";
  import { scale } from "svelte/transition";
  import Titlebar from "$components/shared/Titlebar.svelte";
  import AppWindow from "$components/AppWindow.svelte";
  import PeerUptimeChart from "$components/PeerUptimeChart.svelte";
  import { peerStore } from "$store/peer";
  import { webProxyStore } from "$store/webproxy";
  import {
    openedApps,
    activeThing,
    minimizedApps,
    appZIndexes,
    bringToFront,
  } from "$store/store";
  import MaterialSymbolsLightPulseAlert from "~icons/material-symbols-light/pulse-alert";
  import MaterialSymbolsLightSettings from "~icons/material-symbols-light/settings";
  import PeerSettings from "$components/PeerSettings.svelte";
  import { onMount } from "svelte";
  import { formatRelativeTime } from "$lib/dateUtils";
  import { toasts } from "$store/toast";
  import { translateError$, _ } from "$store/i18n";
  import { Button, IconButton, TextBox } from "fluent-svelte";
  import PortScanCard from "$components/PortScanCard.svelte";

  // Window state

  // Tooltip state for chart
  let tooltipVisible = false;
  let tooltipX = 0;
  let tooltipY = 0;
  let tooltipData: { index: number; rtt: number; success: boolean } | null =
    null;
  let activeTooltipPeerId = "";

  // Multi-selection state
  let selectedPeers = new Set<string>();
  let showSequenceRenameModal = false;
  let showMassTaggingModal = false;
  let sequencePattern = "Device-###";
  let sequenceStart = 1;
  let massTagsInput = "";
  let massTagsMode: "add" | "remove" = "add";

  // Confirmation Modal state
  let showConfirmModal = false;
  let confirmConfig = {
    title: "",
    message: "",
    confirmLabel: "Confirm",
    cancelLabel: "Cancel",
    type: "info" as "info" | "warning" | "danger",
    onConfirm: () => {},
  };

  function openConfirmModal(config: Partial<typeof confirmConfig>) {
    confirmConfig = {
      title: config.title || $_("common.confirm"),
      message: config.message || "",
      confirmLabel: config.confirmLabel || $_("common.confirm"),
      cancelLabel: config.cancelLabel || $_("common.cancel"),
      type: config.type || "info",
      onConfirm: config.onConfirm || (() => {}),
    };
    showConfirmModal = true;
  }

  function closeConfirmModal() {
    showConfirmModal = false;
  }

  function handleConfirm() {
    confirmConfig.onConfirm();
    closeConfirmModal();
  }

  function handleToggleWusp(peer: any) {
    peerStore.setSelectedPeer(peer);
    if (!$openedApps.includes("WuspDashboard")) {
      $openedApps = [...$openedApps, "WuspDashboard"];
       $activeThing = "WuspDashboard";
        bringToFront("WuspDashboard");
    } else {
      // hide it if it's already active
      if ($activeThing === "WuspDashboard") {
        $activeThing = "";
      }
      $openedApps = $openedApps.filter((app) => app !== "WuspDashboard");
    }
  }

  function handleToggleRouterOS(peer: any) {
    peerStore.setSelectedPeer(peer);
    if (!$openedApps.includes("RouterOSDashboard")) {
      $openedApps = [...$openedApps, "RouterOSDashboard"];
      $activeThing = "RouterOSDashboard";
      bringToFront("RouterOSDashboard");
    } else {
      if ($activeThing === "RouterOSDashboard") {
        $activeThing = "";
      }
      $openedApps = $openedApps.filter((app) => app !== "RouterOSDashboard");
    }
  }

  $: isMultiSelectActive = selectedPeers.size > 0;
  // allSelected logic now considers filtered set
  $: allSelected =
    (searchQuery ? filteredPeers.length : peers.length) > 0 &&
    (searchQuery
      ? filteredPeers.every((p) => selectedPeers.has(p.id))
      : selectedPeers.size === peers.length);
  $: selectedPeerItems = peers.filter((peer) => selectedPeers.has(peer.id));
  $: selectedHasSharedPeers = selectedPeerItems.some((peer) => peer.is_shared);

  function toggleSelection(peerId: string) {
    if (selectedPeers.has(peerId)) {
      selectedPeers.delete(peerId);
    } else {
      selectedPeers.add(peerId);
    }
    selectedPeers = selectedPeers;
  }

  function toggleSelectAll() {
    if (allSelected) {
      selectedPeers = new Set();
    } else {
      // Select only filtered peers if search is active
      const peersToSelect = searchQuery ? filteredPeers : peers;
      selectedPeers = new Set(peersToSelect.map((p) => p.id));
    }
  }

  // Bulk Actions
  function handleBulkDelete() {
    if (selectedHasSharedPeers) {
      toasts.error("Shared peers cannot be deleted");
      return;
    }
    openConfirmModal({
      title: $_("peers.deleteSelectedTitle"),
      message: $_("peers.deleteSelectedConfirm").replace(
        "{count}",
        selectedPeers.size.toString(),
      ),
      confirmLabel: $_("common.delete"),
      type: "danger",
      onConfirm: async () => {
        try {
          await peerStore.batchUpdatePeers(Array.from(selectedPeers), 1); // 1 = DELETE
        } catch (err: any) {
          console.error("Bulk delete failed:", err);
        }
        selectedPeers = new Set();
      },
    });
  }

  function openSequenceRename() {
    showSequenceRenameModal = true;
  }

  function closeSequenceRename() {
    showSequenceRenameModal = false;
  }

  function applySequenceRename() {
    openConfirmModal({
      title: $_("peers.renameTitle"),
      message:
        $_("peers.confirmRename").replace(
          "{count}",
          selectedPeers.size.toString(),
        ) +
        `\n\n${$_("peers.renamePattern")}: ${sequencePattern}\n${$_(
          "peers.renameStart",
        )}: ${sequenceStart}`,
      confirmLabel: $_("common.rename"),
      type: "warning",
      onConfirm: async () => {
        try {
          await peerStore.batchUpdatePeers(Array.from(selectedPeers), 2, {
            sequencePattern: sequencePattern,
            sequenceStart: sequenceStart,
          });
        } catch (err: any) {
          console.error("Sequence rename failed:", err);
        }
        closeSequenceRename();
        selectedPeers = new Set();
      },
    });
  }

  function openMassTagging() {
    showMassTaggingModal = true;
    massTagsInput = "";
  }

  function closeMassTagging() {
    showMassTaggingModal = false;
  }

  function applyMassTagging() {
    const tags = massTagsInput
      .split(",")
      .map((t) => t.trim())
      .filter((t) => t.length > 0);
    if (tags.length === 0) return;

    const op = massTagsMode === "add" ? 3 : 4;
    const action =
      massTagsMode === "add"
        ? $_("peers.bulkTagAddTitle")
        : $_("peers.bulkTagRemoveTitle");

    openConfirmModal({
      title: action,
      message: $_("peers.bulkTagConfirm")
        .replace("{action}", action)
        .replace("{count}", selectedPeers.size.toString())
        .replace("{tags}", tags.join(", ")),
      confirmLabel: action,
      type: "info",
      onConfirm: async () => {
        try {
          await peerStore.batchUpdatePeers(Array.from(selectedPeers), op, {
            tags: tags,
          });
        } catch (err: any) {
          console.error("Mass tagging failed:", err);
        }
        closeMassTagging();
        selectedPeers = new Set();
      },
    });
  }

  // Search and filter
  let searchQuery = "";

  // Expanded rows state
  let expandedRows: Record<string, boolean> = {};
  let pingLoading: Record<string, boolean> = {};
  let pingData: Record<string, any> = {};
  let activePeersTabs: Record<
    string,
    {
      pingTab: boolean;
      uptimeTab: boolean;
      portsTab: boolean;
    }
  > = {};
  let statsLoading: Record<string, boolean> = {};
  let statsData: Record<string, any> = {};
  let activeStatsTabs: Record<string, string> = {};
  let showUptime: Record<string, boolean> = {};
  let showPorts: Record<string, boolean> = {};

  // Port scan progress tracking
  let scanProgress: Record<string, number> = {};
  let activeScanIds: Record<string, string> = {};
  let scanStates: Record<string, string> = {}; // 'configuring', 'running', 'paused', 'stopping', 'stopped', 'completed', 'failed'
  let finalizingScanResults: Record<string, boolean> = {};

  const PORT_SCAN_RESULT_POLL_INTERVAL_MS = 300;
  const PORT_SCAN_RESULT_MAX_WAIT_MS = 6000;
  $: peers = $peerStore.peers;
  $: isLoading = $peerStore.isLoading;
  $: error = $peerStore.error;
  // Initialize activePeersTabs for each peer (only add NEW peers, preserve existing state)
  $: {
    for (const peer of peers) {
      if (!activePeersTabs[peer.id]) {
        activePeersTabs[peer.id] = {
          pingTab: false,
          uptimeTab: false,
          portsTab: false,
        };
      }
    }
    activePeersTabs = activePeersTabs; // trigger reactivity
  }
  // Filtered peers based on search
  $: filteredPeers = peers.filter((peer) => {
    if (!searchQuery) return true;
    return (
      peer.name?.toLowerCase().includes(searchQuery.toLowerCase()) ||
      peer.assigned_ip?.includes(searchQuery) ||
      peer.public_key?.includes(searchQuery) ||
      peer.tags?.some((tag) =>
        tag.toLowerCase().includes(searchQuery.toLowerCase()),
      )
    );
  });

  // Grouping logic
  let groupByTags = localStorage.getItem("peers_groupByTags") === "true";
  $: {
    localStorage.setItem("peers_groupByTags", String(groupByTags));
  }

  // ── Pagination ─────────────────────────────────────────────────────
  // Cheap client-side pagination — we already have the full list in
  // memory, so this just slices the filtered set. Page size and page
  // index reset to sane defaults whenever the filter changes so the
  // user never lands on an empty page after typing into search.
  const PAGE_SIZE_OPTIONS = [10, 25, 50, 100] as const;
  let pageSize: number =
    parseInt(localStorage.getItem("peers_pageSize") || "25", 10) || 25;
  if (!PAGE_SIZE_OPTIONS.includes(pageSize as any)) pageSize = 25;
  let currentPage = 1;

  $: localStorage.setItem("peers_pageSize", String(pageSize));

  // Reset to page 1 whenever the visible set shrinks below the current
  // window — covers search, filter, and pageSize changes in one rule.
  $: if (filteredPeers.length <= (currentPage - 1) * pageSize) {
    currentPage = 1;
  }

  $: totalPages = Math.max(1, Math.ceil(filteredPeers.length / pageSize));
  $: if (currentPage > totalPages) currentPage = totalPages;

  $: pagedPeers = filteredPeers.slice(
    (currentPage - 1) * pageSize,
    currentPage * pageSize,
  );

  $: peerGroups = groupByTags
    ? groupPeers(pagedPeers)
    : { [$_("peers.allDevices")]: pagedPeers };
  $: sortedGroupNames = Object.keys(peerGroups).sort((a, b) => {
    if (a === "Untagged") return 1; // Untagged last
    if (b === "Untagged") return -1;
    return a.localeCompare(b);
  });

  function groupPeers(list: typeof peers) {
    const groups: Record<string, typeof peers> = {};
    list.forEach((p) => {
      if (!p.tags || p.tags.length === 0) {
        if (!groups[$_("peers.untagged")]) groups[$_("peers.untagged")] = [];
        groups[$_("peers.untagged")].push(p);
      } else {
        p.tags.forEach((tag) => {
          if (!groups[tag]) groups[tag] = [];
          groups[tag].push(p);
        });
      }
    });
    return groups;
  }

  onMount(async () => {
    try {
      await peerStore.listPeers();

      // WebSocket subscription removed - PortScanCard handles its own data stream
    } catch (err: any) {
      console.error("Failed to load peers:", err);
    }
  });

  function handleAddPeer() {
    if (!$openedApps.includes("AddPeer")) {
      $openedApps = [...$openedApps, "AddPeer"];
    }
    $activeThing = "AddPeer";
    bringToFront("AddPeer");
  }

  async function handleRemovePeer(peerId: string, peerName: string) {
    if (
      !confirm($_("peers.deleteConfirmFinal", { values: { name: peerName } }))
    )
      return;

    try {
      await peerStore.removePeer(peerId);
    } catch (err: any) {
      console.error("Failed to remove peer:", err);
    }
  }

  // Notification toggle state
  let notificationLoading: Record<string, boolean> = {};

  async function copyToClipboard(text: string, type: "ip" | "key" = "ip") {
    try {
      await navigator.clipboard.writeText(text);
      toasts.success(
        type === "ip" ? $_("peers.ipCopied") : $_("peers.keyCopied"),
      );
    } catch (err) {
      console.error("Failed to copy:", err);
      toasts.error($_("peers.failedToCopy"));
    }
  }

  async function handleToggleNotification(
    peerId: string,
    currentState: boolean,
  ) {
    notificationLoading[peerId] = true;
    try {
      await peerStore.toggleNotification(peerId, !currentState);
    } catch (err: any) {
      console.error("Failed to toggle notification:", err);
    } finally {
      notificationLoading[peerId] = false;
      notificationLoading = notificationLoading;
    }
  }

  async function handleShowConfig(peerId: string) {
    // 1. Fetch config first
    try {
      const config = await peerStore.getPeerConfig(peerId);

      // 2. Set selected peer config in store
      peerStore.setSelectedPeerConfig(peerId, config);

      // 3. Open PeerConfig app
      if (!$openedApps.includes("PeerConfig")) {
        $openedApps = [...$openedApps, "PeerConfig"];
      }
      $activeThing = "PeerConfig";
      bringToFront("PeerConfig");
    } catch (err) {
      console.error("Failed to load peer config:", err);
      toasts.error($_("peers.failedToLoadConfig"));
    }
  }

  function formatLastSeen(
    lastHandshake?: { seconds: number; nanos: number } | string,
  ): string {
    // Use UTC-based relative time formatting
    return formatRelativeTime(lastHandshake as any, {
      neverLabel: $_("peers.never"),
    });
  }

  let lastToggleTime = 0;
  function toggleRow(peerId: string) {
    const now = Date.now();
    if (now - lastToggleTime < 300) return;
    lastToggleTime = now;

    expandedRows[peerId] = !expandedRows[peerId];
    if (expandedRows[peerId] && !activeStatsTabs[peerId]) {
      activeStatsTabs[peerId] = "history";
    }
  }

  async function handlePing(peerId: string, forceRetry = false) {
    // If ping data exists (including errors), toggle it off UNLESS we are forcing a retry
    if (!forceRetry) {
      activePeersTabs[peerId].pingTab = !activePeersTabs[peerId].pingTab;
    } else {
      activePeersTabs[peerId].pingTab = true;
    }

    activePeersTabs[peerId].uptimeTab = false;
    activePeersTabs[peerId].portsTab = false;
    if (!expandedRows[peerId]) {
      toggleRow(peerId);
    }
    if (pingData[peerId] && !forceRetry) {
      delete pingData[peerId];
      pingData = pingData;
      return;
    }
    // Clear stats data when switching to ping view - Exforce Exclusivity
    if (statsData[peerId]) {
      delete statsData[peerId];
      statsData = statsData;
    }
    if (showUptime[peerId]) {
      delete showUptime[peerId];
      showUptime = showUptime;
    }
    if (showPorts[peerId]) {
      delete showPorts[peerId];
      showPorts = showPorts;
    }

    pingLoading[peerId] = true;

    // Initialize with empty ping data for animation
    pingData[peerId] = {
      peer_ip: "",
      packets_sent: 0,
      packets_received: 0,
      packet_loss_percent: 0,
      min_rtt_ms: 0,
      avg_rtt_ms: 0,
      max_rtt_ms: 0,
      pings: [],
    };

    try {
      // Stream pings in real-time — each result updates the chart as it arrives
      const summary = await peerStore.pingPeer(peerId, 10, 1000, (event: any) => {
        if (!pingData[peerId]) return;
        const pings = [...(pingData[peerId].pings || []), event];
        const received = pings.filter((p: any) => p.success).length;
        const successRtts = pings.filter((p: any) => p.success && p.rtt_ms).map((p: any) => p.rtt_ms);
        pingData[peerId] = {
          ...pingData[peerId],
          pings,
          packets_sent: pings.length,
          packets_received: received,
          packet_loss_percent: ((pings.length - received) / pings.length) * 100,
          min_rtt_ms: successRtts.length ? Math.min(...successRtts) : 0,
          max_rtt_ms: successRtts.length ? Math.max(...successRtts) : 0,
          avg_rtt_ms: successRtts.length ? successRtts.reduce((a: number, b: number) => a + b, 0) / successRtts.length : 0,
        };
        pingData = pingData; // trigger Svelte reactivity
      });

      // Apply final summary from server
      if (summary) {
        pingData[peerId] = {
          ...pingData[peerId],
          peer_ip: summary.peer_ip,
          packets_sent: summary.packets_sent,
          packets_received: summary.packets_received,
          packet_loss_percent: summary.packet_loss_percent,
          min_rtt_ms: summary.min_rtt_ms,
          avg_rtt_ms: summary.avg_rtt_ms,
          max_rtt_ms: summary.max_rtt_ms,
        };
        pingData = pingData;
      }
      expandedRows[peerId] = true;
    } catch (err: any) {
      pingData[peerId] = { error: $_("peers.deviceNotReachable") };
    } finally {
      pingLoading[peerId] = false;
    }
  }

  function closePingChart(peerId: string) {
    delete pingData[peerId];
    pingData = pingData;
    activePeersTabs[peerId].pingTab = false;
  }

  async function handleShowUptime(peerId: string) {
    // Exclusive: Close ping chart if open
    activePeersTabs[peerId].uptimeTab = !activePeersTabs[peerId].uptimeTab;
    activePeersTabs[peerId].portsTab = false;
    activePeersTabs[peerId].pingTab = false;
    if (!expandedRows[peerId]) {
      toggleRow(peerId);
    }
    // Just show stats without forcing a live refresh
    handleGetStats(peerId, false);
  }
  async function handleShowPorts(peerId: string) {
    // Exclusive: Close ping chart if open
    activePeersTabs[peerId].portsTab = !activePeersTabs[peerId].portsTab;
    activePeersTabs[peerId].uptimeTab = false;
    activePeersTabs[peerId].pingTab = false;
    if (!expandedRows[peerId]) {
      toggleRow(peerId);
    }
    // Just show stats without forcing a live refresh
    handleGetStats(peerId, false);
  }
  async function handleGetStats(peerId: string, forceRefresh = true) {
    // If stats are already showing and we don't want to force refresh (e.g. from Uptime view)
    expandedRows[peerId] = true;
    const peer = peers.find((p) => p.id === peerId);

    // Optimistically populate with cached data from Peer object if available
    // This makes Uptime Chart appear immediately
    let initialData = statsData[peerId] || {};
    if (peer) {
      initialData = {
        ...initialData,
        peer_id: peerId,
        is_online: peer.is_online,
        // Use cached data if available
        uptime_history: peer.uptime_history || initialData.uptime_history,
        open_ports: peer.discovered_ports || initialData.open_ports,
        last_port_scan: peer.last_port_scan || initialData.last_port_scan,
        cached: true,
      };
      statsData[peerId] = initialData;
      statsData = statsData; // Reactivity
    }

    // Trigger fetch from backend
    statsLoading[peerId] = true;

    try {
      const result = await peerStore.getPeerStats(peerId);
      // Check if we got meaningful data (has open ports or at least is_online status)
      if (result) {
        if (result.open_ports?.length > 0 || result.is_online !== undefined) {
          statsData[peerId] = {
            ...statsData[peerId],
            ...result,
            cached: false,
            // Explicitly set no_ports to false so we don't trigger the empty view
            no_ports: false,
          };
        } else {
          // Even if no ports are found, we still want to show the stats card
          // so users can see Uptime History and Connection Details.
          statsData[peerId] = {
            peer_id: peerId,
            is_online: peer?.is_online || false,
            open_ports: [],
            // Set no_ports to false to ensure we render the main stats card
            no_ports: false,
            scan_in_progress: result.scan_in_progress || false,
            // Preserve handshake history if available
            uptime_history: result.uptime_history,
          };
        }

        // If scan is in progress, keep loading state active
        // Real-time updates will come via WebSocket
        if (result.scan_in_progress) {
          statsLoading[peerId] = true;
          const activeScanId =
            result.active_scan_id || result.activeScanId || "";
          if (activeScanId) {
            activeScanIds[peerId] = activeScanId;
          }
          if (!scanStates[peerId]) {
            scanStates[peerId] = "running";
          }
          if (scanProgress[peerId] === undefined) {
            scanProgress[peerId] = 0;
          }
        } else {
          statsLoading[peerId] = false;
          delete activeScanIds[peerId];
          delete scanProgress[peerId];
          if (scanStates[peerId] !== "configuring") {
            delete scanStates[peerId];
          }
        }
      }
    } catch (err: any) {
      statsLoading[peerId] = false;
      statsData[peerId] = {
        error: err.message || $_("peers.failedToGetStats"),
      };
    }

    // Trigger reactivity
    statsLoading = statsLoading;
    statsData = statsData;
    scanProgress = scanProgress;
    scanStates = scanStates;
    activeScanIds = activeScanIds;
  }

  async function handleManualScan(peerId: string, fullScan: boolean = false) {
    statsLoading[peerId] = true;
    scanProgress[peerId] = 0;
    scanStates[peerId] = "running";
    if (statsData[peerId]) {
      delete statsData[peerId].error;
    }

    // Trigger reactivity
    statsLoading = statsLoading;
    scanProgress = scanProgress;
    scanStates = scanStates;
    statsData = statsData;

    try {
      const resp = await peerStore.startPortScan(peerId, fullScan);
      const scanId = resp.scan_id || resp.scanId || resp.id;
      if (scanId) {
        activeScanIds[peerId] = scanId;
      }
      activeScanIds = activeScanIds;
    } catch (err: any) {
      toasts.error("Failed to start scan: " + err.message);
      statsLoading[peerId] = false;
      delete scanProgress[peerId];
      delete scanStates[peerId];
      delete activeScanIds[peerId];
      statsLoading = statsLoading;
      scanProgress = scanProgress;
      scanStates = scanStates;
      activeScanIds = activeScanIds;
    }
  }

  function handleOpenScan(peerId: string) {
    statsLoading[peerId] = false;
    delete scanProgress[peerId];
    delete activeScanIds[peerId];
    scanStates[peerId] = "configuring";
    scanStates = scanStates;
    activeScanIds = activeScanIds;
    statsLoading = statsLoading;
    statsData = statsData;
  }

  function pause(ms: number) {
    return new Promise((resolve) => setTimeout(resolve, ms));
  }

  function applyPeerStatsResult(peerId: string, result: any) {
    statsData[peerId] = {
      ...statsData[peerId],
      ...result,
      cached: false,
      no_ports: false,
    };
    statsData = statsData;
  }

  function clearPortScanUiState(peerId: string) {
    statsLoading[peerId] = false;
    delete scanProgress[peerId];
    delete activeScanIds[peerId];
    delete scanStates[peerId];
    statsLoading = statsLoading;
    scanProgress = scanProgress;
    activeScanIds = activeScanIds;
    scanStates = scanStates;
  }

  async function finalizeCompletedScan(peerId: string, scanId?: string) {
    if (finalizingScanResults[peerId]) {
      return;
    }

    finalizingScanResults[peerId] = true;

    try {
      const deadline = Date.now() + PORT_SCAN_RESULT_MAX_WAIT_MS;
      let latestResult: any = null;

      while (Date.now() <= deadline) {
        latestResult = await peerStore.getPeerStats(peerId);
        const activeScanId =
          latestResult?.active_scan_id || latestResult?.activeScanId || "";

        // The backend publishes the terminal "completed" event before it
        // persists the final scan result, so wait until the scan is no longer
        // reported as active before swapping the progress card for the results.
        if (!latestResult?.scan_in_progress && (!scanId || activeScanId !== scanId)) {
          applyPeerStatsResult(peerId, latestResult);
          clearPortScanUiState(peerId);
          return;
        }

        await pause(PORT_SCAN_RESULT_POLL_INTERVAL_MS);
      }

      if (latestResult && !latestResult.scan_in_progress) {
        applyPeerStatsResult(peerId, latestResult);
        clearPortScanUiState(peerId);
      }
    } catch (error) {
      console.error("Failed to finalize completed port scan", error);
    } finally {
      delete finalizingScanResults[peerId];
    }
  }

  function handleScanCardStatus(peerId: string, detail: any) {
    const previousStatus = scanStates[peerId];

    if (detail?.scanId) {
      activeScanIds[peerId] = detail.scanId;
    }
    if (detail?.progress !== undefined) {
      scanProgress[peerId] = detail.progress;
    }
    if (detail?.status) {
      scanStates[peerId] = detail.status;
      statsLoading[peerId] = ["running", "paused", "stopping"].includes(
        detail.status,
      );
    }

    scanProgress = scanProgress;
    scanStates = scanStates;
    activeScanIds = activeScanIds;
    statsLoading = statsLoading;

    if (detail?.status === "completed" && previousStatus !== detail.status) {
      void finalizeCompletedScan(peerId, detail?.scanId);
      return;
    }

    if (detail?.status === "stopped" && previousStatus !== detail.status) {
      void peerStore.getPeerStats(peerId).then((result) => {
        if (!result) return;
        applyPeerStatsResult(peerId, result);
      });
    }
  }

  function closeStatsCard(peerId: string) {
    delete statsData[peerId];
    delete scanProgress[peerId];
    delete activeScanIds[peerId];
    delete scanStates[peerId];
    statsData = statsData;
    scanProgress = scanProgress;
    activeScanIds = activeScanIds;
    scanStates = scanStates;
    expandedRows[peerId] = false;
  }

  // HTTP/HTTPS port detection helper
  const HTTP_PORTS = [
    80, 8080, 8000, 8888, 3000, 5000, 8081, 8082, 8443, 9000, 9090,
  ];
  const HTTPS_PORTS = [443, 8443, 9443];
  const HTTP_SERVICES = [
    "http",
    "http-alt",
    "http-proxy",
    "webcache",
    "nginx",
    "apache",
    "lighttpd",
  ];
  const HTTPS_SERVICES = ["https", "ssl", "tls", "https-alt"];

  // Check if a port serves a web page that can be opened in browser
  // Prioritizes backend is_webpage detection, falls back to heuristics
  // Only TCP ports can serve web pages - UDP never serves HTTP
  function isHttpPort(port: {
    port: number;
    service?: string;
    protocol?: string;
    is_webpage?: boolean;
  }): boolean {
    // UDP ports can never serve web pages (HTTP is TCP only)
    if (port.protocol === "udp") return false;

    // If backend detected HTML content, use that
    if (port.is_webpage) return true;

    const portNum = port.port;
    const service = (port.service || "").toLowerCase();

    // Exclude known non-webpage services on HTTP ports
    if (
      service.includes("json") ||
      service.includes("api") ||
      service.includes("soap") ||
      service.includes("xml") ||
      service.includes("winbox") ||
      service.includes("ssh")
    ) {
      return false;
    }

    // Check by service name for HTML-related services
    if (service.includes("http-html") || service.includes("webfig"))
      return true;
    if (HTTP_SERVICES.some((s) => service.includes(s))) return true;
    if (HTTPS_SERVICES.some((s) => service.includes(s))) return true;

    // Check by port number for common web ports
    if (HTTP_PORTS.includes(portNum) || HTTPS_PORTS.includes(portNum))
      return true;

    return false;
  }

  function isHttpsPort(port: {
    port: number;
    service?: string;
    protocol?: string;
  }): boolean {
    // UDP ports can never serve HTTPS
    if ((port as any).protocol === "udp") return false;

    const portNum = port.port;
    const service = (port.service || "").toLowerCase();

    // Check by service name
    if (HTTPS_SERVICES.some((s) => service.includes(s))) return true;

    // Check by port number
    if (HTTPS_PORTS.includes(portNum)) return true;

    return false;
  }

  // Open web browser for a peer's HTTP/HTTPS port
  async function handleOpenBrowser(
    peerId: string,
    peerIp: string,
    port: number,
    useHttps: boolean,
  ) {
    try {
      // Create a web proxy session via gRPC (will reuse existing if available)
      const session = await webProxyStore.createSession(
        peerId,
        peerIp,
        port,
        useHttps,
        true, // skipTlsVerify for self-signed certs
      );

      // Open a browser tab for this session (will reuse existing if available)
      webProxyStore.openBrowser(session, "/");

      // Always bring the browser window to front
      $activeThing = "WebBrowser";
      bringToFront("WebBrowser");
    } catch (err: any) {
      console.error("Failed to open browser:", err);
      alert(`Failed to open browser: ${err.message || "Unknown error"}`);
    }
  }
</script>

<AppWindow
  appName="Peers"
  title={$_("apps.peers")}
  canReduce={true}
  canMaximize={true}
  canResize={true}
  dragDisabled={isMultiSelectActive}
  width="1024px"
  height="768px"
>
  <div slot="header_icon" style="display: flex; align-items: center;">
    <svg
      xmlns="http://www.w3.org/2000/svg"
      width="16"
      height="16"
      viewBox="0 0 100 100"
    >
      <defs>
        <linearGradient id="peersGrad" x1="0%" y1="0%" x2="100%" y2="100%">
          <stop offset="0%" style="stop-color:#3b82f6;stop-opacity:1" />
          <stop offset="100%" style="stop-color:#1d4ed8;stop-opacity:1" />
        </linearGradient>
      </defs>
      <circle cx="35" cy="30" r="12" fill="url(#peersGrad)" opacity="0.9" />
      <path
        d="M35 45 Q20 50 20 65 L50 65 Q50 50 35 45 Z"
        fill="url(#peersGrad)"
        opacity="0.9"
      />
      <circle cx="65" cy="30" r="12" fill="url(#peersGrad)" opacity="0.9" />
      <path
        d="M65 45 Q50 50 50 65 L80 65 Q80 50 65 45 Z"
        fill="url(#peersGrad)"
        opacity="0.9"
      />
      <circle cx="50" cy="55" r="10" fill="url(#peersGrad)" />
      <path
        d="M50 67 Q38 70 38 82 L62 82 Q62 70 50 67 Z"
        fill="url(#peersGrad)"
      />
    </svg>
    <span class="appName pl-2">{$_("apps.peers")}</span>
  </div>
  <div class="mainApp">
    <div class="content">
      <!-- Toolbar with Search and Actions -->
      <div class="toolbar mt-4">
        <div class="search-container" class:has-selection={isMultiSelectActive}>
          <div class="search-select-wrapper" title={$_("peers.selectAll")}>
            <input
              type="checkbox"
              checked={allSelected}
              on:change={toggleSelectAll}
              class="search-select-cb"
            />
          </div>
          <div class="search-divider" />
          <div class="search-input-wrapper">
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
              placeholder={$_("peers.searchPlaceholder")}
              class="search-input"
            />
          </div>
        </div>

        {#if isMultiSelectActive}
          <div class="bulk-actions-inline" transition:scale={{ duration: 150 }}>
            <div class="divider-vertical" />
            <span class="selection-count">
              {$_("peers.selected").replace(
                "{count}",
                selectedPeers.size.toString(),
              )}
            </span>

            <IconButton
              class="action-btn"
              on:click={openSequenceRename}
              title={$_("peers.sequenceRename")}
            >
              <svg
                xmlns="http://www.w3.org/2000/svg"
                width="1em"
                height="1em"
                viewBox="0 0 16 16"
                {...$$props}
                ><!-- Icon from Qlementine Icons by Olivier Cléro - https://github.com/oclero/qlementine-icons/blob/master/LICENSE --><path
                  fill="currentColor"
                  d="M8 .5a.5.5 0 0 1 .5-.5h4a.5.5 0 0 1 0 1H11v14h1.5a.5.5 0 0 1 0 1h-4a.5.5 0 0 1 0-1H10V1H8.5A.5.5 0 0 1 8 .5"
                /><path
                  fill="currentColor"
                  fill-rule="evenodd"
                  d="M5.46 4.31a.501.501 0 0 0-.924 0l-2.5 6a.5.5 0 0 0 .923.385l.705-1.69h2.67l.705 1.69a.5.5 0 0 0 .923-.385l-2.5-6zM4.998 5.8L5.915 8h-1.83l.917-2.2z"
                  clip-rule="evenodd"
                /><path
                  fill="currentColor"
                  d="M8.5 3a.5.5 0 0 0 0-1H4.8c-1.68 0-2.52 0-3.16.327a3.02 3.02 0 0 0-1.31 1.31c-.327.642-.327 1.48-.327 3.16v2.4c0 1.68 0 2.52.327 3.16a3.02 3.02 0 0 0 1.31 1.31c.642.327 1.48.327 3.16.327h3.7a.5.5 0 0 0 0-1H4.8c-.857 0-1.44 0-1.89-.038c-.438-.035-.663-.1-.819-.18a2 2 0 0 1-.874-.874c-.08-.156-.145-.38-.18-.819c-.037-.45-.038-1.03-.038-1.89v-2.4c0-.857.001-1.44.038-1.89c.036-.438.101-.663.18-.819c.192-.376.498-.682.874-.874c.156-.08.381-.145.819-.18c.45-.036 1.03-.037 1.89-.037h3.7zm4 10a.506.506 0 0 0-.496.504c0 .278.226.503.504.496c.863-.02 1.41-.09 1.86-.318a3 3 0 0 0 1.31-1.31c.327-.642.327-1.48.327-3.16v-2.4c0-1.68 0-2.52-.327-3.16a3 3 0 0 0-1.31-1.31c-.449-.229-.995-.298-1.86-.318a.494.494 0 0 0-.504.496c0 .275.222.497.496.504q.333.007.592.029c.438.035.663.1.819.18c.376.192.682.498.874.874c.08.156.145.38.18.819c.037.45.038 1.03.038 1.89v2.4c0 .857-.001 1.44-.038 1.89c-.036.438-.101.663-.18.819a2 2 0 0 1-.874.874c-.156.08-.381.145-.819.18q-.26.02-.592.028z"
                /></svg
              >
            </IconButton>

            <IconButton
              class="action-btn"
              on:click={openMassTagging}
              title={$_("peers.massTagging")}
            >
              <svg
                xmlns="http://www.w3.org/2000/svg"
                width="1em"
                height="1em"
                viewBox="0 0 24 24"
                {...$$props}
                ><!-- Icon from Material Symbols by Google - https://github.com/google/material-design-icons/blob/master/LICENSE --><path
                  fill="currentColor"
                  d="m9 16l-.825 3.275q-.075.325-.325.525t-.6.2q-.475 0-.775-.375T6.3 18.8L7 16H4.275q-.5 0-.8-.387T3.3 14.75q.075-.35.35-.55t.625-.2H7.5l1-4H5.775q-.5 0-.8-.387T4.8 8.75q.075-.35.35-.55t.625-.2H9l.825-3.275Q9.9 4.4 10.15 4.2t.6-.2q.475 0 .775.375t.175.825L11 8h4l.825-3.275q.075-.325.325-.525t.6-.2q.475 0 .775.375t.175.825L17 8h2.725q.5 0 .8.387t.175.863q-.075.35-.35.55t-.625.2H16.5l-1 4h2.725q.5 0 .8.388t.175.862q-.075.35-.35.55t-.625.2H15l-.825 3.275q-.075.325-.325.525t-.6.2q-.475 0-.775-.375T12.3 18.8L13 16zm.5-2h4l1-4h-4z"
                /></svg
              >
            </IconButton>

            {#if !selectedHasSharedPeers}
              <IconButton
                class="action-btn delete"
                on:click={handleBulkDelete}
                title={$_("peers.deleteSelected")}
              >
                <svg
                  xmlns="http://www.w3.org/2000/svg"
                  width="1em"
                  height="1em"
                  viewBox="0 0 24 24"
                  {...$$props}
                  ><!-- Icon from Material Symbols by Google - https://github.com/google/material-design-icons/blob/master/LICENSE --><path
                    fill="currentColor"
                    d="M15 18v-2h4v2zm0-8V8h7v2zm0 4v-2h6v2zM3 8H2V6h4V4.5h4V6h4v2h-1v9q0 .825-.587 1.413T11 19H5q-.825 0-1.412-.587T3 17zm2 0v9h6V8zm0 0v9z"
                  /></svg
                >
              </IconButton>
            {/if}
            <button
              class="action-btn cancel-selection"
              on:click={() => (selectedPeers = new Set())}
              title={$_("peers.deselectAll")}
            >
              <svg
                width="14"
                height="14"
                viewBox="0 0 24 24"
                fill="none"
                stroke="currentColor"
                stroke-width="2"
              >
                <path d="M18 6L6 18M6 6l12 12" />
              </svg>
            </button>
          </div>
        {:else}
          <div class="toolbar-actions">
            <button
              class="icon-btn"
              on:click={handleAddPeer}
              title={$_("peers.addPeer")}
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
              class:active={groupByTags}
              on:click={() => (groupByTags = !groupByTags)}
              title={$_("peers.groupByTags")}
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
              class="icon-btn refresh"
              on:click={() => {
                peerStore.refresh();
              }}
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
        {/if}
      </div>

      <!-- {#if error}
        <div class="error-message">
          <strong>Error:</strong>
          {error}
        </div>
      {/if} -->

      {#if isLoading}
        <div class="loading-state">
          <p>Loading devices...</p>
        </div>
      {:else if filteredPeers.length === 0 && searchQuery}
        <div class="empty-state">
          <div class="empty-icon">
            <svg
              width="64"
              height="64"
              viewBox="0 0 24 24"
              fill="none"
              stroke="rgb(var(--clrPrm))"
              stroke-width="1.5"
              stroke-linecap="round"
              stroke-linejoin="round"
            >
              <circle cx="11" cy="11" r="8" />
              <path d="m21 21-4.3-4.3" />
              <path d="M11 8v6" opacity="0.5" />
              <path d="M8 11h6" opacity="0.5" />
            </svg>
          </div>
          <p>
            {$_("peers.noMatchingPeers", { values: { query: searchQuery } })}
          </p>
        </div>
      {:else if peers.length === 0}
        <div class="empty-state">
          <div class="empty-icon">
            <svg
              width="64"
              height="64"
              viewBox="0 0 24 24"
              fill="none"
              stroke="rgb(var(--clrPrm))"
              stroke-width="1.5"
              stroke-linecap="round"
              stroke-linejoin="round"
            >
              <rect width="18" height="18" x="3" y="4" rx="2" ry="2" />
              <line x1="16" x2="16" y1="2" y2="4" />
              <line x1="8" x2="8" y1="2" y2="4" />
              <circle cx="12" cy="13" r="3" opacity="0.5" />
              <path d="M12 17v4" />
              <path d="M8 21h8" />
            </svg>
          </div>
          <p>{$_("peers.noPeers")}</p>
          <Button on:click={handleAddPeer}>
            {$_("peers.addPeer")}
          </Button>
        </div>
      {:else}
        <table class="peers-table">
          <thead>
            <tr>
              <th class="col-checkbox" />
              <th class="col-expand" />
              <th class="col-name">{$_("peers.peerName")}</th>
              <th class="col-ip">{$_("peers.assignedIP")}</th>
              <th class="col-status">{$_("common.status")}</th>
              <th class="col-lastseen">{$_("peers.lastSeen")}</th>
              <th class="col-actions">{$_("common.actions")}</th>
            </tr>
          </thead>
          <tbody>
            {#each sortedGroupNames as groupName}
              {#if groupByTags}
                <tr class="group-header">
                  <td colspan="7">
                    <div class="group-title-tag">
                      <span class="tag-icon">#</span>
                      <span class="tag-name">{groupName}</span>
                      <span class="tag-count"
                        >{peerGroups[groupName].length}</span
                      >
                    </div>
                  </td>
                </tr>
              {/if}
              {#each peerGroups[groupName] as peer (groupByTags ? groupName + peer.id : peer.id)}
                {@const hasHandshake =
                  (peer.last_handshake !== undefined &&
                    peer.last_handshake !== null &&
                    Number(peer.last_handshake) > 0) ||
                  (peer.last_seen_at !== undefined &&
                    peer.last_seen_at !== null &&
                    String(peer.last_seen_at) !== "")}
                <tr
                  class="peer-row hover:bg-gray-200 cursor-pointer"
                  class:expanded={expandedRows[peer.id]}
                  class:selected={selectedPeers.has(peer.id)}
                  on:click={(e) => {
                    e.stopPropagation();
                    toggleRow(peer.id);
                  }}
                >
                  <td class="col-checkbox">
                    <input
                      type="checkbox"
                      checked={selectedPeers.has(peer.id)}
                      on:change={(e) => {
                        e.stopPropagation();
                        toggleSelection(peer.id);
                      }}
                      class="peer-cb"
                    />
                  </td>
                  <td class="col-expand">
                    <button
                      class="expand-btn"
                      class:expanded={expandedRows[peer.id]}
                      on:click={(e) => {
                        e.stopPropagation();
                        toggleRow(peer.id);
                      }}
                      title={expandedRows[peer.id]
                        ? $_("common.collapse")
                        : $_("common.expand")}
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
                      <div
                        class="status-dot"
                        class:online={peer.is_online}
                        class:never-connected={!peer.is_online && !hasHandshake}
                      />
                      <div class="name-content">
                        <span class="peer-name">{peer.name}</span>
                        {#if peer.is_shared}
                          <span class="shared-badge" title="Shared by {peer.owner_name || 'another account'}">
                            shared{peer.owner_name ? ` · ${peer.owner_name}` : ""}
                          </span>
                        {/if}
                        {#if peer.fingerprint?.hostname}
                          <span class="peer-hostname"
                            >{peer.fingerprint.hostname}</span
                          >
                        {/if}
                      </div>
                    </div>
                  </td>
                  <td class="col-ip">
                    <!-- svelte-ignore a11y-click-events-have-key-events -->
                    <code
                      class="ip-code copyable"
                      on:click={(e) => {
                        e.stopPropagation();
                        peer.assigned_ip && copyToClipboard(peer.assigned_ip);
                      }}
                      title={$_("peers.clickToCopy")}
                    >
                      {peer.assigned_ip || "N/A"}
                    </code>
                  </td>
                  <td class="col-status">
                    <span
                      class="status-badge"
                      class:online={peer.is_online}
                      class:never-connected={!peer.is_online && !hasHandshake}
                    >
                      {#if peer.is_online}
                        {$_("common.online")}
                      {:else if !hasHandshake}
                        {$_("peers.never")}
                      {:else}
                        {$_("common.offline")}
                      {/if}
                    </span>
                  </td>
                  <td class="col-lastseen">
                    {peer.is_online
                      ? "Just now"
                      : formatLastSeen(peer.last_seen_at || peer.last_handshake)}
                  </td>
                  <td class="col-actions">
                    <div class="action-buttons">
                      <button
                        class="action-btn ping-btn"
                        class:active={activePeersTabs[peer.id]?.pingTab &&
                          !pingLoading[peer.id] &&
                          !pingData[peer.id]?.error}
                        class:ping-loading={pingLoading[peer.id]}
                        class:ping-success={pingData[peer.id] &&
                          !pingData[peer.id]?.error &&
                          !pingLoading[peer.id] &&
                          activePeersTabs[peer.id]?.pingTab}
                        class:ping-error={pingData[peer.id]?.error &&
                          !pingLoading[peer.id] &&
                          activePeersTabs[peer.id]?.pingTab}
                        disabled={pingLoading[peer.id]}
                        on:click={(e) => {
                          e.stopPropagation();
                          handlePing(peer.id);
                        }}
                        title={pingLoading[peer.id]
                          ? "Pinging..."
                          : pingData[peer.id]?.error
                            ? "Ping failed - Click to retry"
                            : $_("peers.ping")}
                      >
                        {#if pingLoading[peer.id]}
                          <div
                            class="btn-spinner max-w-[32px]! max-h-[32px]!"
                          />
                        {:else}
                          <MaterialSymbolsLightPulseAlert size={16} />
                        {/if}
                      </button>
                      {#if peer.is_wantasticd || peer.client_type === 'wantasticd' || peer.fingerprint?.vendor === 'Wantastic'}
                      <button
                        class="action-btn"
                        class:active={$openedApps.includes("WuspDashboard") &&
                          $peerStore.selectedPeer?.id === peer.id}
                        on:click={(e) => {
                          e.stopPropagation();
                          handleToggleWusp(peer);
                        }}
                        title="WUSP Device Management"
                      >
                        <svg width="16" height="16" viewBox="0 0 24 24" fill="none" xmlns="http://www.w3.org/2000/svg">
                          <path d="M12 2L2 7l10 5 10-5-10-5zM2 17l10 5 10-5M2 12l10 5 10-5" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"/>
                        </svg>
                      </button>
                      {/if}
                      {#if peer.routeros_candidate || peer.routeros_api_ready}
                      <button
                        class="action-btn"
                        class:active={$openedApps.includes("RouterOSDashboard") &&
                          $peerStore.selectedPeer?.id === peer.id}
                        on:click={(e) => {
                          e.stopPropagation();
                          handleToggleRouterOS(peer);
                        }}
                        title="RouterOS Dashboard"
                      >
                        <svg width="16" height="16" viewBox="0 0 24 24" fill="none" xmlns="http://www.w3.org/2000/svg">
                          <rect x="3" y="4" width="18" height="16" rx="2" stroke="currentColor" stroke-width="2"/>
                          <path d="M6 9H18M6 14H18" stroke="currentColor" stroke-width="2" stroke-linecap="round"/>
                        </svg>
                      </button>
                      {/if}
                      <button
                        class="action-btn"
                        on:click={(e) => {
                          e.stopPropagation();
                          handleShowConfig(peer.id);
                        }}
                        title={$_("peers.showConfig")}
                      >
                        <MaterialSymbolsLightSettings size={16} />
                      </button>
                      <button
                        class="action-btn"
                        class:active={activePeersTabs[peer.id].portsTab}
                        on:click={(e) => {
                          e.stopPropagation();
                          handleShowPorts(peer.id);
                        }}
                        title={$_("peers.portDiscovery")}
                      >
                        <svg
                          width="16"
                          height="16"
                          viewBox="0 0 24 24"
                          fill="none"
                          xmlns="http://www.w3.org/2000/svg"
                        >
                          <path
                            d="M9 17V7m0 10a2 2 0 11-4 0m4 0a2 2 0 10-4 0m4 0h8a2 2 0 002-2V9a2 2 0 00-2-2h-8m0 0V5a2 2 0 114 0v2m-4 0a2 2 0 104 0"
                            stroke="currentColor"
                            stroke-width="2"
                            stroke-linecap="round"
                            stroke-linejoin="round"
                          />
                        </svg>
                      </button>
                      <button
                        class="action-btn"
                        class:active={activePeersTabs[peer.id].uptimeTab}
                        on:click={(e) => {
                          e.stopPropagation();
                          handleShowUptime(peer.id);
                        }}
                        title={$_("peers.uptimeHistory")}
                      >
                        <svg
                          width="16"
                          height="16"
                          viewBox="0 0 24 24"
                          fill="none"
                          xmlns="http://www.w3.org/2000/svg"
                        >
                          <path
                            d="M18 20V10M12 20V4M6 20v-6"
                            stroke="currentColor"
                            stroke-width="2"
                            stroke-linecap="round"
                            stroke-linejoin="round"
                          />
                        </svg>
                      </button>
                      {#if !peer.is_shared}
                      <button
                        class="action-btn"
                        class:active={peer.notification_enabled}
                        class:loading={notificationLoading[peer.id]}
                        on:click={(e) => {
                          e.stopPropagation();
                          handleToggleNotification(
                            peer.id,
                            peer.notification_enabled ?? false,
                          );
                        }}
                        title={peer.notification_enabled
                          ? $_("peers.disableAlerts")
                          : $_("peers.enableAlerts")}
                        disabled={notificationLoading[peer.id]}
                      >
                        {#if notificationLoading[peer.id]}
                          <div class="btn-spinner" />
                        {:else}
                          <svg
                            width="16"
                            height="16"
                            viewBox="0 0 24 24"
                            fill={peer.notification_enabled
                              ? "currentColor"
                              : "none"}
                            xmlns="http://www.w3.org/2000/svg"
                          >
                            <path
                              d="M18 8A6 6 0 106 8c0 7-3 9-3 9h18s-3-2-3-9M13.73 21a2 2 0 01-3.46 0"
                              stroke="currentColor"
                              stroke-width="2"
                              stroke-linecap="round"
                              stroke-linejoin="round"
                            />
                          </svg>
                        {/if}
                      </button>
                      <button
                        class="action-btn delete-btn"
                        on:click={(e) => {
                          e.stopPropagation();
                          handleRemovePeer(peer.id, peer.name);
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
                            d="M3 6h18M19 6v14a2 2 0 01-2 2H7a2 2 0 01-2-2V6m3 0V4a2 2 0 012-2h4a2 2 0 012 2v2m-6 5v6m4-6v6"
                            stroke="currentColor"
                            stroke-width="2"
                            stroke-linecap="round"
                            stroke-linejoin="round"
                          />
                        </svg>
                      </button>
                      {/if}
                    </div>
                  </td>
                </tr>
                {#if expandedRows[peer.id]}
                  <tr class="expanded-content">
                    <td colspan="12">
                      <div class="expanded-inner">
                        {#if pingData[peer.id]?.error && activePeersTabs[peer.id].pingTab}
                          <div class="ping-error-card">
                            <svg
                              width="32"
                              height="32"
                              viewBox="0 0 24 24"
                              fill="none"
                              class="error-icon-svg"
                            >
                              <circle
                                cx="12"
                                cy="12"
                                r="10"
                                stroke="#ef4444"
                                stroke-width="2"
                              />
                              <path
                                d="M12 8v4M12 16h.01"
                                stroke="#ef4444"
                                stroke-width="2"
                                stroke-linecap="round"
                              />
                            </svg>
                            <p>
                              {$translateError$(pingData[peer.id].error)}
                            </p>
                            <div class="error-actions">
                              <button
                                class="retry-btn"
                                on:click={() => {
                                  // closePingChart(peer.id); // Do not close, just force retry
                                  handlePing(peer.id, true);
                                }}
                              >
                                <svg
                                  width="14"
                                  height="14"
                                  viewBox="0 0 24 24"
                                  fill="none"
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
                              <button
                                class="close-btn"
                                on:click={() => closePingChart(peer.id)}
                              >
                                {$_("peers.close")}
                              </button>
                            </div>
                          </div>
                        {/if}
                        {#if pingData[peer.id] && activePeersTabs[peer.id].pingTab}
                          {#if pingData[peer.id].pings?.length > 0}
                            {@const pings = pingData[peer.id].pings}
                            {@const maxRtt = pingData[peer.id].max_rtt_ms || 1}
                            {@const minRtt = pingData[peer.id].min_rtt_ms || 0}
                            {@const avgRtt = pingData[peer.id].avg_rtt_ms || 0}
                            {@const chartWidth = 100}
                            {@const chartHeight = 100}
                            {@const padding = 0}
                            {@const innerWidth = chartWidth}
                            {@const innerHeight = chartHeight}
                            <div class="ping-chart-card">
                              <!-- Beautiful SVG Chart -->
                              <svg
                                class="ping-svg-chart"
                                viewBox="0 0 {chartWidth} {chartHeight}"
                                preserveAspectRatio="none"
                                on:mouseleave={() => {
                                  tooltipVisible = false;
                                  activeTooltipPeerId = "";
                                }}
                              >
                                <defs>
                                  <!-- Gradient fill - beautiful area gradient -->
                                  <linearGradient
                                    id="pingGradient-{peer.id}"
                                    x1="0%"
                                    y1="0%"
                                    x2="0%"
                                    y2="100%"
                                  >
                                    <stop
                                      offset="0%"
                                      stop-color="#22c55e"
                                      stop-opacity="0.2"
                                    />
                                    <stop
                                      offset="100%"
                                      stop-color="#22c55e"
                                      stop-opacity="0"
                                    />
                                  </linearGradient>
                                  <!-- Line gradient -->
                                  <linearGradient
                                    id="lineGradient-{peer.id}"
                                    x1="0%"
                                    y1="0%"
                                    x2="100%"
                                    y2="0%"
                                  >
                                    <stop offset="0%" stop-color="#16a34a" />
                                    <stop offset="50%" stop-color="#22c55e" />
                                    <stop offset="100%" stop-color="#4ade80" />
                                  </linearGradient>
                                  <!-- Glow filter -->
                                  <filter
                                    id="glow-{peer.id}"
                                    x="-50%"
                                    y="-50%"
                                    width="200%"
                                    height="200%"
                                  >
                                    <feGaussianBlur
                                      stdDeviation="1.5"
                                      result="coloredBlur"
                                    />
                                    <feMerge>
                                      <feMergeNode in="coloredBlur" />
                                      <feMergeNode in="SourceGraphic" />
                                    </feMerge>
                                  </filter>
                                </defs>

                                <!-- Area fill -->
                                <path
                                  class="ping-area"
                                  d={(() => {
                                    const points = pings.map((p, i) => {
                                      const x =
                                        (i / (pings.length - 1)) * innerWidth;
                                      const y = p.success
                                        ? (1 - p.rtt_ms / (maxRtt * 1.2)) *
                                          innerHeight
                                        : innerHeight;
                                      return { x, y };
                                    });
                                    let d = `M ${points[0].x} ${innerHeight}`;
                                    d += ` L ${points[0].x} ${points[0].y}`;
                                    for (let i = 1; i < points.length; i++) {
                                      const prev = points[i - 1];
                                      const curr = points[i];
                                      const cpx1 =
                                        prev.x + (curr.x - prev.x) * 0.4;
                                      const cpx2 =
                                        prev.x + (curr.x - prev.x) * 0.6;
                                      d += ` C ${cpx1} ${prev.y}, ${cpx2} ${curr.y}, ${curr.x} ${curr.y}`;
                                    }
                                    d += ` L ${
                                      points[points.length - 1].x
                                    } ${innerHeight}`;
                                    d += " Z";
                                    return d;
                                  })()}
                                  fill="url(#pingGradient-{peer.id})"
                                />

                                <!-- Smooth line with animation -->
                                <path
                                  class="ping-line"
                                  d={(() => {
                                    const points = pings.map((p, i) => {
                                      const x =
                                        (i / (pings.length - 1)) * innerWidth;
                                      const y = p.success
                                        ? (1 - p.rtt_ms / (maxRtt * 1.2)) *
                                          innerHeight
                                        : innerHeight;
                                      return { x, y };
                                    });
                                    let d = `M ${points[0].x} ${points[0].y}`;
                                    for (let i = 1; i < points.length; i++) {
                                      const prev = points[i - 1];
                                      const curr = points[i];
                                      const cpx1 =
                                        prev.x + (curr.x - prev.x) * 0.4;
                                      const cpx2 =
                                        prev.x + (curr.x - prev.x) * 0.6;
                                      d += ` C ${cpx1} ${prev.y}, ${cpx2} ${curr.y}, ${curr.x} ${curr.y}`;
                                    }
                                    return d;
                                  })()}
                                  fill="none"
                                  stroke="url(#lineGradient-{peer.id})"
                                  stroke-width="1.5"
                                  stroke-linecap="round"
                                  filter="url(#glow-{peer.id})"
                                />

                                <!-- Invisible hit areas for tooltips -->
                                {#each pings as ping, i}
                                  {@const x =
                                    (i / (pings.length - 1)) * innerWidth}
                                  {@const y = ping.success
                                    ? (1 - ping.rtt_ms / (maxRtt * 1.2)) *
                                      innerHeight
                                    : innerHeight}
                                  <circle
                                    cx={x}
                                    cy={y}
                                    r="8"
                                    fill="transparent"
                                    class="ping-hit-area"
                                    on:mouseenter={(e) => {
                                      tooltipVisible = true;
                                      activeTooltipPeerId = peer.id;
                                      const rect =
                                        e.currentTarget.ownerSVGElement?.getBoundingClientRect();
                                      if (rect) {
                                        tooltipX = e.clientX - rect.left;
                                        tooltipY = e.clientY - rect.top;
                                      }
                                      tooltipData = {
                                        index: i + 1,
                                        rtt: ping.rtt_ms,
                                        success: ping.success,
                                      };
                                    }}
                                    on:mouseleave={() => {
                                      tooltipVisible = false;
                                      activeTooltipPeerId = "";
                                    }}
                                  />
                                {/each}
                              </svg>

                              <!-- Floating labels overlay -->
                              <div class="chart-labels">
                                <div class="label avg">
                                  <span class="value">{avgRtt.toFixed(1)}</span>
                                  <span class="unit">ms avg</span>
                                </div>
                                <div class="label stats">
                                  <span class="success-count"
                                    >{pingData[peer.id]
                                      .packets_received ?? 0}/{pingData[peer.id]
                                      .packets_sent ?? 0}</span
                                  >
                                  <span
                                    class="loss"
                                    class:has-loss={pingData[peer.id]
                                      .packet_loss_percent > 0}
                                  >
                                    {(pingData[peer.id].packet_loss_percent ?? 0) > 0
                                      ? `${(pingData[
                                          peer.id
                                        ].packet_loss_percent ?? 0).toFixed(0)}% loss`
                                      : "No loss"}
                                  </span>
                                </div>
                              </div>

                              <!-- Tooltip -->
                              {#if tooltipVisible && activeTooltipPeerId === peer.id && tooltipData}
                                <div
                                  class="chart-tooltip"
                                  style="left: {tooltipX}px; top: {tooltipY -
                                    40}px;"
                                >
                                  <span class="tooltip-ping"
                                    >Ping #{tooltipData.index}</span
                                  >
                                  <span
                                    class="tooltip-rtt"
                                    class:failed={!tooltipData.success}
                                  >
                                    {tooltipData.success
                                      ? `${tooltipData.rtt.toFixed(1)} ms`
                                      : "Timed out"}
                                  </span>
                                </div>
                              {/if}

                              <!-- Close button -->
                              <button
                                class="close-ping-btn"
                                on:click={() => closePingChart(peer.id)}
                                title="Close"
                              >
                                <svg
                                  width="14"
                                  height="14"
                                  viewBox="0 0 24 24"
                                  fill="none"
                                >
                                  <path
                                    d="M18 6L6 18M6 6l12 12"
                                    stroke="currentColor"
                                    stroke-width="2.5"
                                    stroke-linecap="round"
                                  />
                                </svg>
                              </button>
                            </div>
                          {:else if !pingData[peer.id]?.error && activePeersTabs[peer.id].pingTab}
                            <div class="ping-empty">
                              <p>Pinging...</p>
                            </div>
                          {/if}
                        {/if}

                        <!-- 2. Uptime History Card -->
                        {#if statsData[peer.id]?.uptime_history && activePeersTabs[peer.id].uptimeTab}
                          <div class="stats-card history-card">
                            <button
                              class="stats-action-btn close z-50 backdrop-blur-md bg-black/50"
                              on:click={() => {
                                activePeersTabs[peer.id].uptimeTab = false;
                              }}
                            >
                              <svg
                                width="16"
                                height="16"
                                viewBox="0 0 24 24"
                                fill="none"
                                stroke="currentColor"
                                stroke-width="2.5"
                                ><path d="M18 6L6 18M6 6l12 12" /></svg
                              >
                            </button>
                            {#if statsLoading[peer.id] && !statsData[peer.id]?.uptime_history}
                              <div class="stats-loading">
                                <div class="btn-spinner" />
                                <p>Fetching history...</p>
                              </div>
                            {:else}
                              <PeerUptimeChart
                                uptimeHistoryBytes={statsData[peer.id]?.uptime_history}
                              />
                            {/if}
                          </div>
                        {/if}
                        <!-- 4. Discovered Ports Card -->
                        {#if statsData[peer.id] && activePeersTabs[peer.id].portsTab}
                          <div class="stats-card ports-card">
                            <div class="stats-header">
                              <div class="stats-header-left">
                                <h3>
                                  <svg
                                    width="16"
                                    height="16"
                                    viewBox="0 0 24 24"
                                    fill="none"
                                    stroke="currentColor"
                                    stroke-width="2"
                                    ><path
                                      d="M9 12h6M9 8h6M9 16h6M5 20h14a2 2 0 002-2V6a2 2 0 00-2-2H5a2 2 0 00-2 2v12a2 2 0 002 2z"
                                    /></svg
                                  >
                                  Discovered Ports
                                </h3>
                                {#if statsLoading[peer.id]}
                                  <span class="scanning-badge"
                                    ><div class="mini-spinner spinning" />
                                    Scanning...</span
                                  >
                                {/if}
                              </div>
                              <div class="stats-actions">
                                {#if statsData[peer.id]?.cached && !statsLoading[peer.id]}
                                  <span class="cached-badge">Cached</span>
                                {/if}
                                <button
                                  class="stats-action-btn refresh"
                                  class:spinning={statsLoading[peer.id]}
                                  on:click={() => handleGetStats(peer.id)}
                                  disabled={statsLoading[peer.id]}
                                  title="Refresh Stats"
                                >
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
                                      d="M23 4v6h-6M1 20v-6h6M3.51 9a9 9 0 0 1 14.85-3.36L23 10M1 14l4.64 4.36A9 9 0 0 0 20.49 15"
                                    /></svg
                                  >
                                </button>
                                <!-- Single Scan Button to open Configuration -->
                                {#if !scanStates[peer.id]}
                                  <button
                                    class="stats-action-btn scan"
                                    on:click={() => handleOpenScan(peer.id)}
                                    title="New Port Scan"
                                  >
                                    <svg
                                      width="14"
                                      height="14"
                                      viewBox="0 0 24 24"
                                      fill="none"
                                      stroke="currentColor"
                                      stroke-width="2"
                                      stroke-linecap="round"
                                      stroke-linejoin="round"
                                      ><circle cx="11" cy="11" r="8" /><line
                                        x1="21"
                                        y1="21"
                                        x2="16.65"
                                        y2="16.65"
                                      /></svg
                                    >
                                  </button>
                                {/if}
                                <button
                                  class="stats-action-btn close"
                                  on:click={() => {
                                    activePeersTabs[peer.id].portsTab = false;
                                  }}
                                >
                                  <svg
                                    width="16"
                                    height="16"
                                    viewBox="0 0 24 24"
                                    fill="none"
                                    stroke="currentColor"
                                    stroke-width="2.5"
                                    ><path d="M18 6L6 18M6 6l12 12" /></svg
                                  >
                                </button>
                              </div>
                            </div>

                            {#if statsData[peer.id]?.error && !statsLoading[peer.id]}
                              <div class="stats-error">
                                <svg
                                  width="32"
                                  height="32"
                                  viewBox="0 0 24 24"
                                  fill="none"
                                  class="error-icon-svg"
                                >
                                  <circle
                                    cx="12"
                                    cy="12"
                                    r="10"
                                    stroke="#ef4444"
                                    stroke-width="2"
                                  />
                                  <path
                                    d="M12 8v4M12 16h.01"
                                    stroke="#ef4444"
                                    stroke-width="2"
                                    stroke-linecap="round"
                                  />
                                </svg>
                                <p>
                                  Scan failed: {$translateError$(
                                    statsData[peer.id].error,
                                  )}
                                </p>
                                <div class="error-actions">
                                  <button
                                    class="retry-btn"
                                    on:click={() => handleGetStats(peer.id)}
                                  >
                                    <svg
                                      width="14"
                                      height="14"
                                      viewBox="0 0 24 24"
                                      fill="none"
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
                            {:else if scanStates[peer.id] || activeScanIds[peer.id]}
                              <!-- New PortScanCard Component -->
                              <PortScanCard
                                peerId={peer.id}
                                scanId={activeScanIds[peer.id]}
                                initialProgress={scanProgress[peer.id] || 0}
                                on:status={(event) =>
                                  handleScanCardStatus(peer.id, event.detail)}
                                on:close={() => {
                                  statsLoading[peer.id] = false;
                                  delete scanStates[peer.id];
                                  delete scanProgress[peer.id];
                                  delete activeScanIds[peer.id];
                                  statsLoading = statsLoading;
                                  activeScanIds = activeScanIds;
                                  scanStates = scanStates;
                                  scanProgress = scanProgress;
                                }}
                              />
                            {:else if statsLoading[peer.id] && !statsData[peer.id]}
                              <div class="stats-loading">
                                <div class="btn-spinner" />
                                <p>Loading discovered ports...</p>
                              </div>
                            {:else}
                              {@const rawPorts =
                                statsData[peer.id]?.open_ports ||
                                statsData[peer.id]?.discovered_ports}
                              <!-- Filter out UDP ports as requested -->
                              {@const ports = rawPorts?.filter(
                                (p) =>
                                  (p.protocol || "").toLowerCase() !== "udp",
                              )}
                              {#if ports && ports.length > 0}
                                <div class="ports-grid">
                                  {#each ports as port}
                                    <div
                                      class="port-card"
                                      class:http-port={isHttpPort(port)}
                                    >
                                      <div class="port-header">
                                        <span class="port-number"
                                          >{port.port}</span
                                        >
                                        <span class="port-protocol"
                                          >{port.protocol?.toUpperCase() ||
                                            "TCP"}</span
                                        >
                                      </div>
                                      <div class="port-service">
                                        {port.service || "Unknown"}
                                      </div>
                                      {#if isHttpPort(port)}
                                        {@const isHttps = isHttpsPort(port)}
                                        <button
                                          class="open-browser-btn"
                                          on:click={() =>
                                            handleOpenBrowser(
                                              peer.id,
                                              peer.assigned_ip.split("/")[0],
                                              port.port,
                                              isHttps,
                                            )}
                                        >
                                          <svg
                                            xmlns="http://www.w3.org/2000/svg"
                                            width="12"
                                            height="12"
                                            viewBox="0 0 24 24"
                                            fill="none"
                                            stroke="currentColor"
                                            stroke-width="2"
                                            stroke-linecap="round"
                                            stroke-linejoin="round"
                                            ><path d="M15 3h6v6" /><path
                                              d="M10 14 21 3"
                                            /><path
                                              d="M18 13v6a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2V8a2 2 0 0 1 2-2h6"
                                            /></svg
                                          >
                                          Open {isHttps ? "(HTTPS)" : ""}
                                        </button>
                                      {/if}
                                    </div>
                                  {/each}
                                </div>
                              {:else}
                                <div class="stats-empty">
                                  <div class="empty-icon">
                                    <svg
                                      width="48"
                                      height="48"
                                      viewBox="0 0 24 24"
                                      fill="none"
                                      stroke="rgb(var(--clrPrm))"
                                      stroke-width="1.5"
                                      stroke-linecap="round"
                                      stroke-linejoin="round"
                                    >
                                      <rect
                                        x="2"
                                        y="2"
                                        width="20"
                                        height="8"
                                        rx="2"
                                      />
                                      <path d="M12 10v4" />
                                      <path d="M12 14 8 18h-3" />
                                      <path d="M12 14l4 4h3" />
                                      <circle cx="5" cy="18" r="2" />
                                      <circle cx="19" cy="18" r="2" />
                                    </svg>
                                  </div>
                                  <p class="empty-title">
                                    {$_("peers.noPortsFound") ||
                                      "No Open Ports"}
                                  </p>
                                  <p class="empty-subtitle">
                                    {$_("peers.noPortsFoundDetail") ||
                                      "No open TCP ports were found on this device during the scan."}
                                  </p>
                                </div>
                              {/if}
                            {/if}
                          </div>
                        {/if}

                        <PeerSettings
                          {peer}
                          on:close={() => (expandedRows[peer.id] = false)}
                        />
                      </div>
                    </td>
                  </tr>
                {/if}
              {/each}
            {/each}
          </tbody>
        </table>

        <!-- Mobile Peer List -->
        <div class="mobile-peer-list">
          {#each sortedGroupNames as groupName}
            {#if groupByTags}
              <div class="group-header-mobile">
                <span>{groupName}</span>
                <span>({peerGroups[groupName].length})</span>
              </div>
            {/if}
            {#each peerGroups[groupName] as peer (groupByTags ? groupName + peer.id : peer.id)}
              {@const hasHandshake =
                (peer.last_handshake !== undefined &&
                  peer.last_handshake !== null &&
                  Number(peer.last_handshake) > 0) ||
                (peer.last_seen_at !== undefined &&
                  peer.last_seen_at !== null &&
                  String(peer.last_seen_at) !== "")}
              <!-- svelte-ignore a11y-click-events-have-key-events -->
              <div
                class="peer-mobile-card"
                class:expanded={expandedRows[peer.id] && true}
              >
                <!-- svelte-ignore a11y-click-events-have-key-events -->
                <div
                  class="card-main"
                  on:click={(e) => {
                    e.stopPropagation();
                    toggleRow(peer.id);
                  }}
                >
                  <div class="card-info">
                    <div
                      class="status-dot-mobile"
                      class:online={peer.is_online}
                    />
                    <div class="name-box">
                      <span class="peer-name">{peer.name}</span>
                      {#if peer.is_shared}
                        <span class="shared-badge" title="Shared by {peer.owner_name || 'another account'}">
                          shared{peer.owner_name ? ` · ${peer.owner_name}` : ""}
                        </span>
                      {/if}
                      {#if peer.fingerprint?.hostname}
                        <span class="peer-hostname"
                          >{peer.fingerprint.hostname}</span
                        >
                      {/if}
                    </div>
                  </div>
                  <div class="card-meta">
                    <code class="ip-badge">{peer.assigned_ip || "N/A"}</code>
                    <div
                      class="expand-icon"
                      class:expanded={expandedRows[peer.id]}
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

                {#if expandedRows[peer.id]}
                  <div
                    class="card-actions"
                    transition:scale={{ duration: 150, start: 0.95 }}
                  >
                    <button
                      class="action-btn"
                      class:active={activePeersTabs[peer.id]?.pingTab}
                      on:click={(e) => {
                        e.stopPropagation();
                        handlePing(peer.id);
                      }}
                    >
                      {#if pingLoading[peer.id]}
                        <div class="mini-spinner spinning" />
                      {:else}
                        <MaterialSymbolsLightPulseAlert size={20} />
                      {/if}
                    </button>
                    {#if peer.is_wantasticd || peer.client_type === 'wantasticd' || peer.fingerprint?.vendor === 'Wantastic'}
                      <button
                        class="action-btn"
                        class:active={$openedApps.includes("WuspDashboard") &&
                          $peerStore.selectedPeer?.id === peer.id}
                        on:click={(e) => {
                          e.stopPropagation();
                          handleToggleWusp(peer);
                        }}
                        title="WUSP Device Management"
                      >
                        <svg
                          width="20"
                          height="20"
                          viewBox="0 0 24 24"
                          fill="none"
                          xmlns="http://www.w3.org/2000/svg"
                        >
                          <path
                            d="M12 2L2 7l10 5 10-5-10-5zM2 17l10 5 10-5M2 12l10 5 10-5"
                            stroke="currentColor"
                            stroke-width="2"
                            stroke-linecap="round"
                            stroke-linejoin="round"
                          />
                        </svg>
                      </button>
                    {/if}
                    {#if peer.routeros_candidate || peer.routeros_api_ready}
                      <button
                        class="action-btn"
                        class:active={$openedApps.includes("RouterOSDashboard") &&
                          $peerStore.selectedPeer?.id === peer.id}
                        on:click={(e) => {
                          e.stopPropagation();
                          handleToggleRouterOS(peer);
                        }}
                        title="RouterOS Dashboard"
                      >
                        <svg width="16" height="16" viewBox="0 0 24 24" fill="none" xmlns="http://www.w3.org/2000/svg">
                          <rect x="3" y="4" width="18" height="16" rx="2" stroke="currentColor" stroke-width="2"/>
                          <path d="M6 9H18M6 14H18" stroke="currentColor" stroke-width="2" stroke-linecap="round"/>
                        </svg>
                      </button>
                    {/if}
                    <button
                      class="action-btn"
                      on:click={(e) => {
                        e.stopPropagation();
                        handleShowConfig(peer.id);
                      }}
                    >
                      <MaterialSymbolsLightSettings size={20} />
                    </button>
                    <button
                      class="action-btn"
                      class:active={activePeersTabs[peer.id].portsTab}
                      on:click={(e) => {
                        e.stopPropagation();
                        handleShowPorts(peer.id);
                      }}
                    >
                      <svg
                        width="20"
                        height="20"
                        viewBox="0 0 24 24"
                        fill="none"
                        xmlns="http://www.w3.org/2000/svg"
                      >
                        <path
                          d="M9 17V7m0 10a2 2 0 11-4 0m4 0a2 2 0 10-4 0m4 0h8a2 2 0 002-2V9a2 2 0 00-2-2h-8m0 0V5a2 2 0 114 0v2m-4 0a2 2 0 104 0"
                          stroke="currentColor"
                          stroke-width="2"
                          stroke-linecap="round"
                          stroke-linejoin="round"
                        />
                      </svg>
                    </button>
                    <button
                      class="action-btn"
                      class:active={activePeersTabs[peer.id].uptimeTab}
                      on:click={(e) => {
                        e.stopPropagation();
                        handleShowUptime(peer.id);
                      }}
                    >
                      <svg
                        width="20"
                        height="20"
                        viewBox="0 0 24 24"
                        fill="none"
                        xmlns="http://www.w3.org/2000/svg"
                      >
                        <path
                          d="M18 20V10M12 20V4M6 20v-6"
                          stroke="currentColor"
                          stroke-width="2"
                          stroke-linecap="round"
                          stroke-linejoin="round"
                        />
                      </svg>
                    </button>
                    {#if !peer.is_shared}
                    <button
                      class="action-btn"
                      class:active={peer.notification_enabled}
                      on:click={(e) => {
                        e.stopPropagation();
                        handleToggleNotification(
                          peer.id,
                          peer.notification_enabled ?? false,
                        );
                      }}
                    >
                      <svg
                        width="20"
                        height="20"
                        viewBox="0 0 24 24"
                        fill={peer.notification_enabled
                          ? "currentColor"
                          : "none"}
                        stroke="currentColor"
                        stroke-width="2"
                        stroke-linecap="round"
                        stroke-linejoin="round"
                      >
                        <path
                          d="M18 8A6 6 0 106 8c0 7-3 9-3 9h18s-3-2-3-9M13.73 21a2 2 0 01-3.46 0"
                        />
                      </svg>
                    </button>
                    <button
                      class="action-btn"
                      style="color: #ef4444;"
                      on:click={(e) => {
                        e.stopPropagation();
                        handleRemovePeer(peer.id, peer.name);
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
                    <!-- Reusing the existing Expanded Inner content logic -->
                    <!-- We duplicate the inner content logic here for mobile -->
                    <div class="expanded-inner p-4">
                      <!-- 1. Ping Chart -->
                      {#if pingData[peer.id]?.error && activePeersTabs[peer.id].pingTab}
                        <!-- Error Card (Same as desktop) -->
                        <div class="ping-error-card">
                          <p>{$translateError$(pingData[peer.id].error)}</p>
                          <button
                            class="retry-btn"
                            on:click={() => handlePing(peer.id)}
                          >
                            Retry
                          </button>
                        </div>
                      {/if}

                      {#if pingData[peer.id] && activePeersTabs[peer.id].pingTab && !pingData[peer.id]?.error}
                        <!-- We can reuse the ping chart visualization logic here if needed, or simple status -->
                        <!-- For now, let's just show basic stats or reuse the SVG logic if we extract it to a component -->
                        <!-- Re-implementing the SVG chart logic for mobile would be huge code duplication. 
                              Ideally, we should have a <PingChart> component. 
                              For now, we'll try to keep it simple or copy the extensive block if necessary. 
                              Let's assume we copy the Ping Chart block from above but simplified. -->
                        <div class="ping-chart-card h-40">
                          <!-- Simplified Ping Chart for Mobile -->
                          <div
                            class="flex items-center justify-center w-full h-full text-sm text-gray-500"
                          >
                            <!-- Start Ping Logic (Simplified copy from desktop) -->
                            {#if pingData[peer.id].pings?.length > 0}
                              {@const pings = pingData[peer.id].pings}
                              {@const maxRtt = Math.max(
                                ...pings
                                  .filter((p) => p.success)
                                  .map((p) => p.rtt_ms),
                                10,
                              )}
                              <!-- SVG Chart Mobile -->
                              <svg
                                class="w-full h-full"
                                viewBox="0 0 100 100"
                                preserveAspectRatio="none"
                              >
                                <!-- Basic area path -->
                                <path
                                  d={"M 0 100 " +
                                    pings
                                      .map((p, i) => {
                                        const x =
                                          (i / (pings.length - 1)) * 100;
                                        const y = p.success
                                          ? 100 - (p.rtt_ms / maxRtt) * 80
                                          : 100;
                                        return `L ${x} ${y}`;
                                      })
                                      .join(" ") +
                                    " L 100 100 Z"}
                                  fill="rgba(34, 197, 94, 0.1)"
                                />
                                <!-- Line path -->
                                <path
                                  d={"M 0 " +
                                    (pings[0].success
                                      ? 100 - (pings[0].rtt_ms / maxRtt) * 80
                                      : 100) +
                                    " " +
                                    pings
                                      .map((p, i) => {
                                        const x =
                                          (i / (pings.length - 1)) * 100;
                                        const y = p.success
                                          ? 100 - (p.rtt_ms / maxRtt) * 80
                                          : 100;
                                        return `L ${x} ${y}`;
                                      })
                                      .join(" ")}
                                  fill="none"
                                  stroke="#22c55e"
                                  stroke-width="2"
                                />
                              </svg>
                              <div
                                class="absolute top-2 left-3 text-2xl font-bold text-green-500"
                              >
                                {Math.round(
                                  pingData[peer.id].avg_rtt_ms || 0,
                                )}ms
                              </div>
                            {:else}
                              <span>Pinging...</span>
                            {/if}
                          </div>
                        </div>
                      {/if}

                      <!-- 2. Uptime History -->
                      {#if statsData[peer.id]?.uptime_history && activePeersTabs[peer.id].uptimeTab}
                        <div class="stats-card history-card">
                          <PeerUptimeChart
                            uptimeHistoryBytes={statsData[peer.id]?.uptime_history}
                          />
                        </div>
                      {/if}

                      <!-- 3. Ports -->
                      {#if statsData[peer.id] && activePeersTabs[peer.id].portsTab}
                        <div class="stats-card ports-card">
                          <!-- Mobile Ports Header -->
                          <div class="flex items-center justify-between mb-3">
                            <span class="text-sm font-semibold opacity-70"
                              >Open Ports</span
                            >
                            <div class="flex gap-2">
                              <button
                                class="p-1 rounded bg-black/5 hover:bg-black/10"
                                on:click={() => handleGetStats(peer.id)}
                              >
                                <svg
                                  width="14"
                                  height="14"
                                  viewBox="0 0 24 24"
                                  fill="none"
                                  stroke="currentColor"
                                  stroke-width="2"
                                  ><path
                                    d="M23 4v6h-6M1 20v-6h6M3.51 9a9 9 0 0 1 14.85-3.36L23 10M1 14l4.64 4.36A9 9 0 0 0 20.49 15"
                                  /></svg
                                >
                              </button>
                            </div>
                          </div>

                          {#if statsData[peer.id]?.open_ports?.length > 0}
                            <div
                              class="grid grid-cols-2 gap-2"
                              style="grid-template-columns: repeat(auto-fill, minmax(100px, 1fr));"
                            >
                              {#each statsData[peer.id].open_ports as port}
                                <div
                                  class="p-2 rounded bg-black/5 border border-black/5 flex flex-col gap-1"
                                >
                                  <div
                                    class="flex items-center justify-between"
                                  >
                                    <span class="font-mono text-sm font-bold"
                                      >{port.port}</span
                                    >
                                    <span class="text-[10px] opacity-50"
                                      >{port.protocol}</span
                                    >
                                  </div>
                                  <span class="text-xs opacity-70 truncate"
                                    >{port.service}</span
                                  >
                                </div>
                              {/each}
                            </div>
                          {:else}
                            <div
                              class="p-4 text-center text-sm opacity-50 bg-black/5 rounded-lg"
                            >
                              No ports found
                            </div>
                          {/if}
                        </div>
                      {/if}

                      <PeerSettings {peer} />
                    </div>
                  </div>
                {/if}
              </div>
            {/each}
          {/each}
        </div>
      {/if}
    </div>

    <div class="status-bar">
      <span>{peers.length} device{peers.length !== 1 ? "s" : ""}</span>
      <span>{peers.filter((p) => p.is_online).length} online</span>
      {#if searchQuery}
        <span>{filteredPeers.length} filtered</span>
      {/if}

      {#if filteredPeers.length > PAGE_SIZE_OPTIONS[0]}
        <div class="pager">
          <label class="pager-size">
            <span>Show</span>
            <select bind:value={pageSize}>
              {#each PAGE_SIZE_OPTIONS as size}
                <option value={size}>{size}</option>
              {/each}
            </select>
          </label>
          {#if totalPages > 1}
            <button
              class="pager-btn"
              type="button"
              aria-label="Previous page"
              disabled={currentPage <= 1}
              on:click={() => (currentPage = Math.max(1, currentPage - 1))}
            >‹</button>
            <span class="pager-label">{currentPage} / {totalPages}</span>
            <button
              class="pager-btn"
              type="button"
              aria-label="Next page"
              disabled={currentPage >= totalPages}
              on:click={() =>
                (currentPage = Math.min(totalPages, currentPage + 1))}
            >›</button>
          {/if}
        </div>
      {/if}
    </div>

    <!-- Modals scoped to app -->
    {#if showSequenceRenameModal}
      <div class="modal-overlay" transition:scale={{ duration: 150 }}>
        <div class="modal">
          <div class="modal-header">
            <h3>{$_("peers.sequenceRename")}</h3>
            <button class="close-btn" on:click={closeSequenceRename}>
              <svg
                width="20"
                height="20"
                viewBox="0 0 24 24"
                fill="none"
                stroke="currentColor"
                stroke-width="2"
              >
                <path d="M18 6L6 18M6 6l12 12" />
              </svg>
            </button>
          </div>
          <div class="modal-body">
            <div class="form-group">
              <label for="pattern"
                >{$_("peers.pattern")}
                <span class="hint">Use ### for numbers</span></label
              >
              <input
                id="pattern"
                type="text"
                bind:value={sequencePattern}
                placeholder="Device-###"
              />
            </div>
            <div class="form-group">
              <label for="start">{$_("peers.startNumber")}</label>
              <input
                id="start"
                type="number"
                min="1"
                bind:value={sequenceStart}
              />
            </div>
            <div class="preview-box">
              <svg
                width="16"
                height="16"
                viewBox="0 0 24 24"
                fill="none"
                stroke="currentColor"
                stroke-width="2"
                ><circle cx="12" cy="12" r="10" /><path d="M12 16v-4" /><path
                  d="M12 8h.01"
                /></svg
              >
              <span
                >{$_("peers.preview")}:
                <strong
                  >{sequencePattern.replace(/#+/g, (m) =>
                    String(sequenceStart).padStart(m.length, "0"),
                  )}</strong
                ></span
              >
            </div>
          </div>
          <div class="modal-actions">
            <button class="btn-secondary px-4" on:click={closeSequenceRename}
              >{$_("common.cancel")}</button
            >
            <button class="btn-primary" on:click={applySequenceRename}
              >{$_("common.apply")}</button
            >
          </div>
        </div>
      </div>
    {/if}

    {#if showMassTaggingModal}
      <div class="modal-overlay" transition:scale={{ duration: 150 }}>
        <div class="modal">
          <div class="modal-header">
            <h3>{$_("peers.massTagging")}</h3>
            <button class="close-btn" on:click={closeMassTagging}>
              <svg
                width="20"
                height="20"
                viewBox="0 0 24 24"
                fill="none"
                stroke="currentColor"
                stroke-width="2"
              >
                <path d="M18 6L6 18M6 6l12 12" />
              </svg>
            </button>
          </div>
          <div class="modal-body">
            <div class="form-group">
              <label for="mode">{$_("peers.mode")}</label>
              <div class="radio-group">
                <label class="radio-label">
                  <input type="radio" bind:group={massTagsMode} value="add" />
                  <span>{$_("peers.addTags")}</span>
                </label>
                <label class="radio-label">
                  <input
                    type="radio"
                    bind:group={massTagsMode}
                    value="remove"
                  />
                  <span>{$_("peers.removeTags")}</span>
                </label>
              </div>
            </div>
            <div class="form-group">
              <label for="tags">{$_("peers.tags")}</label>
              <input
                id="tags"
                type="text"
                bind:value={massTagsInput}
                placeholder="server, prod, london"
              />
              <span class="hint">{$_("peers.commaSeparated")}</span>
            </div>
          </div>
          <div class="modal-actions">
            <button class="btn-secondary px-4" on:click={closeMassTagging}
              >{$_("common.cancel")}</button
            >
            <button class="btn-primary" on:click={applyMassTagging}>
              {massTagsMode === "add"
                ? $_("peers.addTags")
                : $_("peers.removeTags")}
            </button>
          </div>
        </div>
      </div>
    {/if}

    {#if showConfirmModal}
      <!-- svelte-ignore a11y-click-events-have-key-events -->
      <div
        class="modal-overlay"
        on:click={closeConfirmModal}
        transition:scale={{ duration: 150, start: 0.95, opacity: 0 }}
      >
        <!-- svelte-ignore a11y-click-events-have-key-events -->
        <div class="modal" on:click|stopPropagation>
          <div class="modal-header">
            <h3>{confirmConfig.title}</h3>
          </div>
          <div class="modal-body">
            <p>{confirmConfig.message}</p>
          </div>
          <div class="modal-actions">
            <button class="btn-secondary px-4" on:click={closeConfirmModal}>
              {confirmConfig.cancelLabel}
            </button>
            <button
              class="btn-primary"
              class:bg-danger={confirmConfig.type === "danger"}
              on:click={handleConfirm}
            >
              {confirmConfig.confirmLabel}
            </button>
          </div>
        </div>
      </div>
    {/if}
  </div>

  {#if showSequenceRenameModal}
    <div class="modal-overlay" transition:scale={{ duration: 150 }}>
      <div class="modal w-[400px]">
        <div class="modal-header mb-4">
          <h3 class="text-lg font-semibold">{$_("peers.sequenceRename")}</h3>
        </div>
        <div class="modal-body flex flex-col gap-4">
          <div class="form-group">
            <p class="mb-1 text-sm opacity-70">{$_("peers.renamePattern")}</p>
            <TextBox
              bind:value={sequencePattern}
              placeholder="Device-###"
              class="w-full"
            />
            <p class="help-text mt-1 text-xs opacity-50">
              Use # for numbers (e.g. Device-### → Device-001)
            </p>
          </div>
          <div class="form-group">
            <p class="mb-1 text-sm opacity-70">{$_("peers.renameStart")}</p>
            <TextBox
              type="number"
              bind:value={sequenceStart}
              min="1"
              class="w-full"
            />
          </div>
          <div
            class="preview-box mt-2 p-3 rounded bg-[rgb(var(--clrPrm)/0.1)] border border-[rgb(var(--clrPrm)/0.2)] flex items-center gap-2 text-sm"
          >
            <svg
              width="16"
              height="16"
              viewBox="0 0 24 24"
              fill="none"
              stroke="currentColor"
              stroke-width="2"
              ><circle cx="12" cy="12" r="10" /><path d="M12 16v-4" /><path
                d="M12 8h.01"
              /></svg
            >
            <span
              >{$_("peers.preview")}:
              <strong class="text-[rgb(var(--clrPrm))]"
                >{sequencePattern.replace(/#+/g, (m) =>
                  String(sequenceStart).padStart(m.length, "0"),
                )}</strong
              ></span
            >
          </div>
        </div>
        <div class="modal-actions mt-6 flex justify-end gap-2">
          <Button on:click={closeSequenceRename}>{$_("common.cancel")}</Button>
          <Button variant="accent" on:click={applySequenceRename}
            >{$_("common.apply")}</Button
          >
        </div>
      </div>
    </div>
  {/if}

  {#if showMassTaggingModal}
    <div class="modal-overlay" transition:scale={{ duration: 150 }}>
      <div class="modal w-[400px]">
        <div class="modal-header mb-4">
          <h3 class="text-lg font-semibold">Bulk Tag Operations</h3>
        </div>
        <div class="modal-body flex flex-col gap-4">
          <div class="form-group">
            <!-- svelte-ignore a11y-label-has-associated-control -->
            <p class="mb-1 text-sm opacity-70">Action</p>
            <div
              class="grid grid-cols-2 gap-2 bg-[rgb(var(--bg2))] p-1 rounded-lg"
            >
              <Button
                variant={massTagsMode === "add" ? "accent" : "standard"}
                class="w-full justify-center"
                on:click={() => (massTagsMode = "add")}
              >
                Add Tags
              </Button>
              <Button
                variant={massTagsMode === "remove" ? "accent" : "standard"}
                class="w-full justify-center"
                on:click={() => (massTagsMode = "remove")}
              >
                Remove Tags
              </Button>
            </div>
          </div>
          <div class="form-group">
            <p class="mb-1 text-sm opacity-70">Tags</p>
            <TextBox
              bind:value={massTagsInput}
              placeholder="Enter tags separated by commas"
              class="w-full"
            />
          </div>
        </div>
        <div class="modal-actions mt-6 flex justify-end gap-2">
          <Button on:click={closeMassTagging}>Cancel</Button>
          <Button variant="accent" on:click={applyMassTagging}>
            {massTagsMode === "add" ? "Add Tags" : "Remove Tags"}
          </Button>
        </div>
      </div>
    </div>
  {/if}

  {#if showConfirmModal}
    <!-- svelte-ignore a11y-click-events-have-key-events -->
    <div
      class="modal-overlay"
      on:click={closeConfirmModal}
      transition:scale={{ duration: 150, start: 0.95, opacity: 0 }}
    >
      <!-- svelte-ignore a11y-click-events-have-key-events -->
      <div class="modal" on:click|stopPropagation>
        <div class="modal-header">
          <h3>{confirmConfig.title}</h3>
        </div>
        <div class="modal-body">
          <p
            style="margin: 0; color: rgb(var(--clr)/0.8); white-space: pre-line; line-height: 1.5;"
          >
            {confirmConfig.message}
          </p>

          {#if confirmConfig.type === "danger"}
            <div
              class="preview-box"
              style="margin-top: 12px; background: rgba(239, 68, 68, 0.1); border-color: rgba(239, 68, 68, 0.2); color: #ef4444;"
            >
              <strong>Warning:</strong> This action cannot be undone.
            </div>
          {/if}
        </div>
        <div
          class="modal-actions"
          class:danger={confirmConfig.type === "danger"}
        >
          <Button on:click={closeConfirmModal}>
            {confirmConfig.cancelLabel}
          </Button>
          <Button variant="accent" on:click={handleConfirm}>
            {confirmConfig.confirmLabel}
          </Button>
        </div>
      </div>
    </div>
  {/if}
</AppWindow>

<style lang="scss">
  .mainApp {
    flex: 1;
    display: flex;
    flex-direction: column;
    overflow: hidden;
  }

  .content {
    @apply flex-1 overflow-auto;
    &::-moz-scrollbar {
      @apply hidden;
    }
    &::-webkit-scrollbar {
      @apply hidden;
    }
  }

  // Toolbar styles
  .toolbar {
    @apply flex items-center justify-between mt-0 gap-4 px-4 py-2 border-b bg-[rgb(var(--bg2))];
  }

  .search-container {
    position: relative;
    flex: 1;
    max-width: 400px;
    display: flex;
    align-items: center;
    background: rgb(var(--bg2));
    border: 1px solid rgb(var(--clr) / 10%);
    border-radius: 6px;
    transition: all 0.2s;

    &:focus-within {
      border-color: rgb(var(--clrPrm));
      background: rgb(var(--bg3));
      box-shadow: 0 0 0 2px rgb(var(--clrPrm) / 10%);
    }

    &.has-selection {
      border-color: rgb(var(--clrPrm));
      background: rgb(var(--clrPrm) / 5%);
    }
  }

  .search-select-wrapper {
    display: flex;
    align-items: center;
    justify-content: center;
    padding: 0 10px;
    height: 100%;
    cursor: pointer;

    &:hover .search-select-cb {
      border-color: rgb(var(--clrPrm));
    }
  }

  .search-select-cb {
    width: 16px;
    height: 16px;
    margin: 0;
    cursor: pointer;
    accent-color: rgb(var(--clrPrm));
  }

  .search-divider {
    width: 1px;
    height: 20px;
    background: rgb(var(--clr) / 10%);
    margin-right: 4px;
  }

  .search-input-wrapper {
    flex: 1;
    position: relative;
    display: flex;
    align-items: center;
    height: 100%;
  }

  .search-icon {
    position: absolute;
    left: 12px;
    top: 50%;
    transform: translateY(-50%);
    color: rgb(var(--clr) / 50%);
    pointer-events: none;
  }

  .search-input {
    width: 100%;
    padding: 9px 12px 9px 36px;
    background: transparent;
    border: none;
    color: rgb(var(--clr));
    font-size: 14px;
    outline: none;
    height: 100%;

    &::placeholder {
      color: rgb(var(--clr) / 50%);
    }
  }

  .toolbar-actions {
    display: flex;
    gap: 8px;
  }

  .icon-btn {
    width: 36px;
    height: 36px;
    display: flex;
    align-items: center;
    justify-content: center;
    background: rgb(var(--bg2));
    border: 1px solid rgb(var(--clr) / 10%);
    border-radius: 6px;
    color: rgb(var(--clr));
    cursor: pointer;
    transition: all 0.2s;

    &:hover {
      background: rgb(var(--clr) / 10%);
      border-color: rgb(var(--clr) / 20%);
    }

    &:active {
      transform: scale(0.95);
    }

    &.active {
      background: rgb(var(--clrPrm) / 20%);
      border-color: rgb(var(--clrPrm));
      color: rgb(var(--clrPrm));
    }

    svg {
      flex-shrink: 0;
    }
  }

  .spread-btn {
    background: linear-gradient(
      135deg,
      rgb(var(--clrPrm)) 0%,
      rgb(var(--clrPrm) / 80%) 100%
    );
    border-color: transparent;
    color: white;

    &:hover {
      background: linear-gradient(
        135deg,
        rgb(var(--clrPrm) / 90%) 0%,
        rgb(var(--clrPrm) / 70%) 100%
      );
    }
  }

  // Table styles
  .peers-table {
    width: 100%;
    border-collapse: collapse;
    background: rgb(var(--bg2));
    border-radius: 8px;
    overflow: hidden;
  }

  .peers-table thead {
    background: rgb(var(--bg3));
    border-bottom: 1px solid rgb(var(--clr) / 10%);
  }

  .peers-table th {
    text-align: left;
    padding: 14px 6px;
    font-size: 10px;
    font-weight: 600;
    color: rgb(var(--clr) / 70%);
    letter-spacing: 0.5px;
  }

  .col-checkbox {
    width: 40px;
    text-align: center;
  }

  .col-expand {
    width: 40px;
    text-align: center;
  }

  .col-name {
    width: auto;
    min-width: 180px;
  }

  .name-content {
    display: flex;
    flex-direction: column;
    gap: 2px;
  }

  .peer-name {
    font-weight: 500;
  }

  .peer-hostname {
    font-size: 11px;
    color: rgb(var(--clr) / 60%);
    font-family: monospace;
  }

  .col-ip {
    width: 140px;
  }

  .col-status {
    width: 100px;
  }

  .col-lastseen {
    width: 150px;
    font-size: 10px !important;
  }

  .col-actions {
    width: 140px;
    text-align: right;
  }

  .peer-row {
    border-bottom: 1px solid rgb(var(--clr) / 5%);
    transition: background 0.2s;

    &:hover {
      background: rgb(var(--clr) / 3%);
    }

    &.expanded {
      background: rgb(var(--clr) / 5%);
    }

    td {
      padding: 12px 16px;
      font-size: 14px;
      vertical-align: middle;
    }
  }

  .expand-btn {
    width: 24px;
    height: 24px;
    display: flex;
    align-items: center;
    justify-content: center;
    background: transparent;
    border: none;
    border-radius: 4px;
    color: rgb(var(--clr) / 60%);
    cursor: pointer;
    transition: all 0.2s;

    &:hover {
      background: rgb(var(--clr) / 10%);
      color: rgb(var(--clr));
    }

    svg {
      transition: transform 0.2s;
    }

    &.expanded {
      background: rgb(var(--clrPrm) / 20%);
      color: rgb(var(--clrPrm));

      svg {
        transform: rotate(180deg);
      }
    }
  }

  .name-cell {
    display: flex;
    align-items: center;
    gap: 8px;
  }

  .status-dot {
    width: 8px;
    height: 8px;
    border-radius: 50%;
    background: #ef4444; /* Red-500 for Offline */
    flex-shrink: 0;
    box-shadow: 0 0 8px rgba(239, 68, 68, 0.5);

    &.online {
      background: #22c55e; /* Green-500 */
      box-shadow: 0 0 8px rgba(34, 197, 94, 0.5);
    }

    &.never-connected {
      background: #9ca3af; /* Gray-400 */
      box-shadow: none;
      border: 1px solid rgba(255, 255, 255, 0.1);
    }
  }

  .ip-code {
    font-family: "Cascadia Code", "Fira Code", monospace;
    font-size: 13px;
    background: rgb(var(--clr) / 5%);
    padding: 4px 8px;
    border-radius: 4px;
    color: rgb(var(--clr) / 90%);
  }

  .ip-code.copyable,
  .detail-value.copyable {
    cursor: pointer;
    position: relative;
    transition: all 0.15s ease;
  }

  .ip-code.copyable:hover,
  .detail-value.copyable:hover {
    background: rgb(var(--clrPrm) / 12%);
    color: rgb(var(--clrPrm));
  }

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
    padding: 4px 8px;
    border-radius: 12px;
    font-size: 10px;
    font-weight: 600;
    background: rgba(239, 68, 68, 0.1); /* Red background for Offline */
    color: #ef4444; /* Red text for Offline */
    border: 1px solid rgba(239, 68, 68, 0.2);

    &.online {
      background: rgba(34, 197, 94, 0.1);
      color: #22c55e;
      border-color: rgba(34, 197, 94, 0.2);
    }

    &.never-connected {
      background: rgba(156, 163, 175, 0.1);
      color: #9ca3af;
      border-color: rgba(156, 163, 175, 0.2);
    }
  }

  .action-buttons {
    display: flex;
    justify-content: flex-end;
    gap: 6px;
  }

  .action-btn {
    width: 32px;
    height: 32px;
    display: flex;
    align-items: center;
    justify-content: center;
    background: transparent;
    border: 1px solid rgb(var(--clr) / 10%);
    border-radius: 4px;
    color: rgb(var(--clr) / 70%);
    cursor: pointer;
    transition: all 0.2s;

    &:hover {
      background: rgb(var(--clr) / 10%);
      border-color: rgb(var(--clr) / 20%);
      color: rgb(var(--clr));
    }

    &.danger {
      color: #ef4444;
      border-color: rgba(239, 68, 68, 0.2);

      &:hover {
        background: rgba(239, 68, 68, 0.1);
        border-color: rgba(239, 68, 68, 0.4);
      }
    }

    &.active {
      background: rgb(var(--clrPrm) / 20%);
      border-color: rgb(var(--clrPrm));
      color: rgb(var(--clrPrm));

      &:hover {
        background: rgb(var(--clrPrm) / 25%);
        border-color: rgb(var(--clrPrm));
      }
    }

    &.loading {
      opacity: 0.6;
      cursor: not-allowed;
    }

    &:disabled {
      opacity: 0.5;
      cursor: not-allowed;
    }
  }

  .btn-spinner {
    width: 14px;
    height: 14px;
    border: 2px solid transparent;
    border-top-color: currentColor;
    border-radius: 50%;
    animation: spin 0.6s linear infinite;
  }

  /* Smart Ping Button States */
  .ping-btn {
    flex-shrink: 0;
    width: 32px;
    height: 32px;
    max-width: 32px;
    max-height: 32px;
    padding: none !important;
    box-sizing: border-box;
  }
  .ping-btn * {
    padding: none !important;
  }
  .ping-btn.ping-loading {
    opacity: 1;
    border-color: rgb(var(--clrPrm) / 40%);
    color: rgb(var(--clrPrm));
    padding: none !important;
    animation: pulse-border 1.5s ease-in-out infinite;
    cursor: wait;
  }

  .ping-btn.ping-success {
    background: rgba(34, 197, 94, 0.15);
    border-color: rgba(34, 197, 94, 0.5);
    color: #22c55e;

    &:hover {
      background: rgba(34, 197, 94, 0.25);
      border-color: #22c55e;
    }
  }

  .ping-btn.ping-error {
    background: rgba(239, 68, 68, 0.12);
    border-color: rgba(239, 68, 68, 0.4);
    color: #ef4444;
    cursor: pointer;
    opacity: 1;
    padding: none !important;
    &:hover {
      background: rgba(239, 68, 68, 0.2);
      border-color: #ef4444;
    }
  }

  @keyframes pulse-border {
    0%,
    100% {
      border-color: rgb(var(--clrPrm) / 20%);
    }
    50% {
      border-color: rgb(var(--clrPrm) / 60%);
    }
  }
  @keyframes spin {
    to {
      transform: rotate(360deg);
    }
  }

  // Expanded row content
  .expanded-content {
    background: rgb(var(--bg3));

    td {
      padding: 0 !important;
    }
  }

  .expanded-inner {
    padding: 8px;
    animation: expandDown 0.2s ease-out;
    display: flex;
    flex-direction: column;
    gap: 8px;
  }

  @keyframes expandDown {
    from {
      opacity: 0;
      transform: translateY(-10px);
    }
    to {
      opacity: 1;
      transform: translateY(0);
    }
  }

  // Ping view styles
  .ping-view {
    display: flex;
    flex-direction: column;
    gap: 16px;
  }

  .ping-chart-card {
    position: relative;
    width: 100%;
    height: 180px;
    background: rgb(var(--bg2) / 40%);
    backdrop-filter: var(--glass-blur);
    border-radius: var(--radius-md);
    overflow: hidden;
    border: 1px solid var(--border-color);
    box-shadow: var(--shadow-sm);
  }

  /* Stats View Tabs */
  .stats-tabs {
    display: flex;
    gap: 8px;
    margin-bottom: 20px;
    background: rgb(var(--clr) / 5%);
    padding: 4px;
    border-radius: var(--radius-md);
    width: fit-content;
  }

  .stats-tab-btn {
    display: flex;
    align-items: center;
    gap: 8px;
    padding: 8px 16px;
    border: none;
    background: transparent;
    color: rgb(var(--clr) / 60%);
    font-size: 13px;
    font-weight: 600;
    border-radius: var(--radius-sm);
    cursor: pointer;
    transition: all 0.2s ease;
  }

  .stats-tab-btn:hover {
    background: rgb(var(--clr) / 5%);
    color: rgb(var(--clr) / 80%);
  }

  .stats-tab-btn.active {
    background: var(--bg8);
    color: var(--primary);
    box-shadow: var(--shadow-sm);
  }

  @media (prefers-color-scheme: dark) {
    .stats-tab-btn.active {
      background: rgb(255 255 255 / 10%);
    }
  }

  .stats-tab-btn svg {
    opacity: 0.7;
    transition: opacity 0.2s;
  }
  .stats-tab-btn.active svg {
    opacity: 1;
  }

  .tab-count {
    display: inline-flex;
    align-items: center;
    justify-content: center;
    background: var(--primary);
    color: white;
    font-size: 10px;
    font-weight: 700;
    min-width: 18px;
    height: 18px;
    padding: 0 5px;
    border-radius: 9px;
    margin-left: 4px;
  }

  /* Transition for content panes */
  .stats-tab-pane {
    animation: fadeIn 0.3s ease-out;
  }

  @keyframes fadeIn {
    from {
      opacity: 0;
      transform: translateY(5px);
    }
    to {
      opacity: 1;
      transform: translateY(0);
    }
  }

  /* Mobile adjustments for tabs */
  @media (max-width: 640px) {
    .stats-tabs {
      width: 100%;
      overflow-x: auto;
      padding: 6px;
    }
    .stats-tab-btn {
      padding: 6px 12px;
      font-size: 12px;
      white-space: nowrap;
    }
  }

  .ping-svg-chart {
    width: 100%;
    height: 100%;
    display: block;
  }

  .ping-area {
    opacity: 0;
    animation: fadeInArea 0.8s ease-out forwards;
  }

  .ping-line {
    stroke-dasharray: 500px;
    stroke-dashoffset: 500px;
    animation: drawLine 1.2s ease-out forwards;
  }

  @keyframes fadeInArea {
    0% {
      opacity: 0;
      transform: translateY(10px);
    }
    100% {
      opacity: 1;
      transform: translateY(0);
    }
  }

  @keyframes drawLine {
    0% {
      stroke-dashoffset: 500px;
    }
    100% {
      stroke-dashoffset: 0;
    }
  }

  .ping-hit-area {
    cursor: crosshair;
    pointer-events: all;
  }

  .chart-labels {
    position: absolute;
    top: 16px;
    left: 20px;
    right: 20px;
    display: flex;
    justify-content: space-between;
    align-items: flex-start;
    pointer-events: none;
    z-index: 5;
  }

  .chart-labels .label {
    display: flex;
    flex-direction: column;
    gap: 2px;
  }

  .chart-labels .label.avg .value {
    font-size: 32px;
    font-weight: 700;
    color: #22c55e;
    line-height: 1;
    text-shadow: 0 2px 8px rgba(34, 197, 94, 0.3);
  }

  .chart-labels .label.avg .unit {
    font-size: 12px;
    color: rgb(var(--clr) / 50%);
    font-weight: 500;
    text-transform: uppercase;
    letter-spacing: 0.5px;
  }

  .chart-labels .label.stats {
    text-align: right;
    align-items: flex-end;
  }

  .chart-labels .label.stats .success-count {
    font-size: 18px;
    font-weight: 600;
    color: rgb(var(--clr) / 80%);
    font-family: "Cascadia Code", monospace;
  }

  .chart-labels .label.stats .loss {
    font-size: 11px;
    color: #22c55e;
    font-weight: 600;
  }

  .chart-labels .label.stats .loss.has-loss {
    color: #fbbf24;
  }

  .chart-tooltip {
    position: absolute;
    padding: 8px 12px;
    background: rgba(var(--bg1), 0.98);
    backdrop-filter: blur(12px);
    border-radius: 8px;
    border: 1px solid rgb(var(--clr) / 15%);
    box-shadow: 0 8px 24px rgba(0, 0, 0, 0.2);
    display: flex;
    flex-direction: column;
    gap: 2px;
    z-index: 100;
    pointer-events: none;
    transform: translateX(-50%);
  }

  .chart-tooltip .tooltip-ping {
    font-size: 10px;
    color: rgb(var(--clr) / 50%);
    text-transform: uppercase;
    letter-spacing: 0.5px;
  }

  .chart-tooltip .tooltip-rtt {
    font-size: 14px;
    font-weight: 700;
    color: #22c55e;
    font-family: "Cascadia Code", monospace;
  }

  .chart-tooltip .tooltip-rtt.failed {
    color: #ef4444;
  }

  .close-ping-btn {
    position: absolute;
    bottom: 12px;
    right: 12px;
    width: 28px;
    height: 28px;
    display: flex;
    align-items: center;
    justify-content: center;
    background: rgba(var(--bg2), 0.8);
    backdrop-filter: blur(8px);
    border: 1px solid rgb(var(--clr) / 10%);
    border-radius: 8px;
    color: rgb(var(--clr) / 50%);
    cursor: pointer;
    transition: all 0.2s;
    z-index: 20;

    &:hover {
      background: rgba(var(--bg1), 0.95);
      color: rgb(var(--clr));
      border-color: rgb(var(--clr) / 20%);
      transform: scale(1.1);
    }
  }

  .ping-empty {
    display: flex;
    align-items: center;
    justify-content: center;
    height: 120px;
    color: rgb(var(--clr) / 50%);
    font-size: 13px;
  }

  .ping-loading {
    display: flex;
    flex-direction: column;
    align-items: center;
    justify-content: center;
    gap: 16px;

    .spinner {
      width: 40px;
      height: 40px;
      border: 3px solid rgb(var(--clr) / 10%);
      border-top-color: rgb(var(--clrPrm));
      border-radius: 50%;
      animation: spin 0.8s linear infinite;
    }

    p {
      margin: 0;
      color: rgb(var(--clr) / 70%);
      font-size: 14px;
    }
  }

  @keyframes spin {
    to {
      transform: rotate(360deg);
    }
  }

  .ping-error-card {
    display: flex;
    flex-direction: column;
    align-items: center;
    gap: 12px;
    padding: 32px 0px;
    .error-icon-svg {
      opacity: 0.9;
    }

    p {
      margin: 0;
      color: #ef4444;
      font-weight: 600;
      font-size: 14px;
    }

    .error-actions {
      display: flex;
      gap: 8px;
      margin-top: 8px;
    }

    .retry-btn,
    .close-btn {
      display: flex;
      align-items: center;
      gap: 6px;
      padding: 6px 16px;
      border-radius: 6px;
      font-size: 13px;
      font-weight: 600;
      cursor: pointer;
      transition: all 0.15s;
    }

    .retry-btn {
      background: rgb(var(--clrPrm));
      color: white;
      border: none;
    }

    .close-btn {
      background: transparent;
      color: rgb(var(--clr) / 70%);
      border: 1px solid rgb(var(--clr) / 15%);

      &:hover {
        background: rgb(var(--clr) / 8%);
        border-color: rgb(var(--clr) / 25%);
      }
    }
  }

  // Stats view styles
  .stats-view {
    display: flex;
    flex-direction: column;
  }

  .stats-card {
    border-radius: 8px;
    padding: 20px;
  }
  .stats-card.history-card {
    .stats-action-btn.close {
      display: flex;
      align-items: center;
      justify-content: center;
      width: 28px;
      height: 28px;
      border-radius: 6px;
      position: absolute;
      top: 12px;
      right: 12px;
      backdrop-filter: blur(8px);
      cursor: pointer;
      z-index: 50;
      transition: all 0.2s;
    }
    padding: 0px;
  }
  .stats-header-actions {
    display: flex;
    align-items: center;
    gap: 10px;
  }

  .refresh-stats-btn {
    background: rgba(var(--primary), 0.1);
    color: var(--primary);
    border: 1px solid rgba(var(--primary), 0.2);
    border-radius: 6px;
    width: 28px;
    height: 28px;
    display: flex;
    align-items: center;
    justify-content: center;
    cursor: pointer;
    transition: all 0.2s;

    &:hover:not(:disabled) {
      background: var(--primary);
      color: white;
      transform: scale(1.05);
    }

    &:disabled {
      opacity: 0.5;
      cursor: not-allowed;
    }

    svg {
      transition: transform 0.3s;
    }

    &:hover:not(:disabled) svg {
      transform: rotate(180deg);
    }
  }

  .stats-header {
    display: flex;
    justify-content: space-between;
    align-items: center;
    margin-bottom: 16px;

    h3 {
      font-size: 14px;
      font-weight: 600;
      color: rgb(var(--clr) / 85%);
      margin: 0;
    }
  }

  .close-stats-btn {
    background: transparent;
    border: none;
    border-radius: 4px;
    padding: 4px;
    cursor: pointer;
    color: rgb(var(--clr) / 50%);
    transition: all 0.15s;

    &:hover {
      background: rgb(var(--clr) / 8%);
      color: rgb(var(--clr) / 80%);
    }
  }

  .stats-section {
    margin-bottom: 14px;

    &:last-child {
      margin-bottom: 0;
    }
  }

  // Fingerprint section styles
  .fingerprint-section {
    background: linear-gradient(
      135deg,
      rgb(var(--clrPrm) / 5%) 0%,
      rgb(var(--clrPrm) / 2%) 100%
    );
    border: 1px solid rgb(var(--clrPrm) / 15%);
    border-radius: 8px;
    padding: 12px;
  }

  .fingerprint-grid {
    display: grid;
    grid-template-columns: repeat(auto-fill, minmax(140px, 1fr));
    gap: 10px;
  }

  .fingerprint-item {
    display: flex;
    flex-direction: column;
    gap: 2px;
  }

  .fp-label {
    font-size: 10px;
    color: rgb(var(--clr) / 50%);
    // text-transform: uppercase;
    letter-spacing: 0.3px;
    font-weight: 500;
  }

  .fp-value {
    font-size: 13px;
    font-weight: 600;
    color: rgb(var(--clr) / 90%);

    &.fp-type {
      text-transform: capitalize;

      &[data-type="router"] {
        color: #f59e0b;
      }
      &[data-type="server"] {
        color: #3b82f6;
      }
      &[data-type="workstation"] {
        color: #8b5cf6;
      }
      &[data-type="switch"] {
        color: #06b6d4;
      }
      &[data-type="access_point"],
      &[data-type="ap"] {
        color: #10b981;
      }
      &[data-type="firewall"] {
        color: #ef4444;
      }
      &[data-type="nas"] {
        color: #6366f1;
      }
      &[data-type="camera"] {
        color: #ec4899;
      }
      &[data-type="phone"] {
        color: #14b8a6;
      }
    }

    &.fp-mac {
      font-family: "Cascadia Code", monospace;
      font-size: 11px;
      background: rgb(var(--bg2));
      padding: 2px 6px;
      border-radius: 4px;
    }
  }

  .confidence-badge {
    font-size: 10px;
    font-weight: 600;
    padding: 2px 6px;
    border-radius: 4px;
    margin-left: 6px;

    &.high {
      background: rgb(34, 197, 94, 0.15);
      color: #22c55e;
    }
    &.medium {
      background: rgb(245, 158, 11, 0.15);
      color: #f59e0b;
    }
    &.low {
      background: rgb(239, 68, 68, 0.15);
      color: #ef4444;
    }
  }

  .bulk-actions-inline {
    display: flex;
    align-items: center;
    gap: 8px;
    animation: fadeIn 0.2s ease-out;
  }

  .divider-vertical {
    width: 1px;
    height: 24px;
    background: rgb(var(--clr) / 10%);
    margin: 0 8px;
  }

  .selection-count {
    font-size: 13px;
    font-weight: 600;
    color: rgb(var(--clrPrm));
    white-space: nowrap;
    margin-right: 8px;
  }

  .action-btn.delete {
    color: #ef4444;
    border-color: rgba(239, 68, 68, 0.2);

    &:hover {
      background: rgba(239, 68, 68, 0.1);
      border-color: #ef4444;
    }
  }

  .action-btn.cancel-selection {
    margin-left: 4px;
    border-color: transparent;

    &:hover {
      background: rgb(var(--clr) / 5%);
      color: rgb(var(--clr));
    }
  }

  .detection-info {
    display: flex;
    align-items: center;
    gap: 6px;
    margin-top: 10px;
    padding-top: 10px;
    border-top: 1px solid rgb(var(--clr) / 8%);
    font-size: 11px;
    color: rgb(var(--clr) / 50%);

    svg {
      opacity: 0.6;
    }
  }

  .section-title {
    font-size: 11px;
    font-weight: 600;
    color: rgb(var(--clr) / 60%);
    margin-bottom: 8px;
    display: flex;
    align-items: center;
    gap: 6px;
    text-transform: uppercase;
    letter-spacing: 0.3px;

    svg {
      opacity: 0.7;
    }
  }

  .stats-grid {
    display: grid;
    grid-template-columns: repeat(auto-fit, minmax(120px, 1fr));
    gap: 8px;
  }

  .stat-box {
    background: rgba(var(--bg2), 0.4);
    border: 1px solid rgb(var(--clr) / 4%);
    border-radius: 6px;
    padding: 10px;
    display: flex;
    flex-direction: column;
    gap: 4px;
  }

  .stat-label {
    font-size: 10px;
    color: rgb(var(--clr) / 50%);
    text-transform: uppercase;
    letter-spacing: 0.3px;
    font-weight: 500;
  }

  .stat-value {
    font-size: 13px;
    font-weight: 600;
    color: rgb(var(--clr) / 85%);
    display: flex;
    align-items: center;
    gap: 5px;

    .status-icon {
      flex-shrink: 0;
    }

    &.online {
      color: #22c55e;
    }
  }

  .ports-scroll-container {
    overflow-x: auto;
    margin: 0 -12px;
    padding: 0 12px;

    /* Scrollbar styling for Windows 11 look */
    &::-webkit-scrollbar {
      height: 6px;
    }

    &::-webkit-scrollbar-track {
      background: rgba(var(--clr), 0.03);
      border-radius: 3px;
    }

    &::-webkit-scrollbar-thumb {
      background: rgba(var(--clr), 0.15);
      border-radius: 3px;

      &:hover {
        background: rgba(var(--clr), 0.25);
      }
    }
  }

  .ports-grid {
    display: grid;
    grid-template-columns: repeat(auto-fill, minmax(130px, 1fr));
    gap: 10px;
    width: 100%;
  }

  .port-card {
    background: rgba(var(--bg2), 0.4);
    border: 1px solid rgb(var(--clr) / 8%);
    border-radius: 10px;
    padding: 12px;
    display: flex;
    flex-direction: column;
    gap: 6px;
    transition: all 0.2s cubic-bezier(0.4, 0, 0.2, 1);
    position: relative;
    overflow: hidden;

    &:hover {
      background: rgba(var(--bg2), 0.6);
      border-color: rgba(var(--primary), 0.3);
      transform: translateY(-2px);
      box-shadow: 0 4px 12px rgba(0, 0, 0, 0.1);
    }

    &.http-port {
      border-left: 3px solid var(--primary);
    }
  }

  .port-header {
    display: flex;
    justify-content: space-between;
    align-items: center;
  }

  .port-number {
    font-size: 16px;
    font-weight: 700;
    color: var(--primary);
  }

  .port-protocol {
    font-size: 10px;
    font-weight: 700;
    color: rgb(var(--clr) / 40%);
    text-transform: uppercase;
    background: rgb(var(--clr) / 5%);
    padding: 2px 6px;
    border-radius: 4px;
  }

  .port-banner {
    font-size: 9px;
    color: rgb(var(--clr) / 55%);
    font-family: monospace;
    background: rgba(var(--clr), 0.04);
    padding: 2px 4px;
    border-radius: 2px;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }

  .port-card.http-port {
    border-color: rgb(var(--primary-rgb) / 25%);
    background: rgb(var(--primary-rgb) / 5%);

    &:hover {
      border-color: rgb(var(--primary-rgb) / 40%);
      background: rgb(var(--primary-rgb) / 10%);
    }
  }

  .open-browser-btn {
    display: flex;
    align-items: center;
    justify-content: center;
    gap: 4px;
    margin-top: 4px;
    padding: 4px 8px;
    font-size: 10px;
    font-weight: 600;
    color: var(--primary);
    background: rgb(var(--primary-rgb) / 10%);
    border: 1px solid rgb(var(--primary-rgb) / 30%);
    border-radius: var(--radius-xs);
    cursor: pointer;
    transition: var(--trans-fast);

    svg {
      stroke: currentColor;
    }

    &:hover {
      background: rgb(var(--primary-rgb) / 20%);
      border-color: rgb(var(--primary-rgb) / 50%);
    }

    &:active {
      transform: scale(0.98);
    }
  }

  .services-grid {
    display: flex;
    gap: 8px;
    flex-wrap: wrap;
  }

  .service-badge {
    background: rgba(var(--bg2), 0.4);
    border: 1px solid rgb(var(--clr) / 6%);
    border-radius: 6px;
    padding: 8px 12px;
    display: flex;
    align-items: center;
    gap: 8px;
    transition: all 0.15s;
    flex: 1;
    min-width: 0;

    &.ssh {
      border-color: rgba(16, 185, 129, 0.3);
      background: rgba(16, 185, 129, 0.06);

      .service-icon {
        stroke: #10b981;
      }
    }

    &.winbox {
      border-color: rgb(var(--primary-rgb) / 30%);
      background: rgb(var(--primary-rgb) / 6%);

      .service-icon {
        stroke: var(--primary);
      }
    }

    &:hover {
      transform: translateY(-1px);
      box-shadow: 0 2px 8px rgba(0, 0, 0, 0.08);
    }
  }

  .service-icon {
    flex-shrink: 0;
  }

  .service-info {
    display: flex;
    align-items: baseline;
    gap: 4px;
    min-width: 0;
  }

  .service-name {
    font-size: 12px;
    font-weight: 600;
    color: rgb(var(--clr) / 80%);
  }

  .service-port {
    font-size: 11px;
    color: rgb(var(--clr) / 55%);
    font-family: monospace;
  }

  .neighbors-list {
    display: flex;
    flex-direction: column;
    gap: 8px;
  }

  .neighbor-card {
    background: rgba(var(--bg2), 0.4);
    border: 1px solid rgb(var(--clr) / 6%);
    border-radius: 6px;
    padding: 10px;
    display: flex;
    flex-direction: column;
    gap: 6px;
    transition: all 0.15s;

    &:hover {
      border-color: rgb(var(--clrPrm) / 25%);
      background: rgba(var(--bg2), 0.6);
    }
  }

  .neighbor-header {
    display: flex;
    justify-content: space-between;
    align-items: center;
    gap: 10px;
  }

  .neighbor-name {
    font-size: 12px;
    font-weight: 600;
    color: rgb(var(--clr) / 85%);
  }

  .neighbor-vendor {
    font-size: 9px;
    color: rgb(var(--clr) / 60%);
    background: rgba(var(--clrPrm), 0.12);
    padding: 2px 6px;
    border-radius: 3px;
    text-transform: uppercase;
    font-weight: 600;
    letter-spacing: 0.3px;
  }

  .neighbor-desc {
    font-size: 11px;
    color: rgb(var(--clr) / 65%);
    line-height: 1.4;
  }

  .neighbor-details {
    display: flex;
    gap: 12px;
    flex-wrap: wrap;
    font-size: 10px;
  }

  .neighbor-detail {
    color: rgb(var(--clr) / 55%);

    strong {
      color: rgb(var(--clr) / 75%);
      font-weight: 600;
    }
  }

  .scan-timestamp {
    margin-top: 12px;
    padding-top: 12px;
    border-top: 1px solid rgb(var(--clr) / 4%);
    font-size: 10px;
    color: rgb(var(--clr) / 50%);
    text-align: center;
    display: flex;
    align-items: center;
    justify-content: center;
    gap: 5px;

    svg {
      opacity: 0.6;
    }
  }

  .stats-container {
    display: flex;
    flex-direction: column;
    gap: 16px;
    padding: 16px;
    width: 100%;
    box-sizing: border-box;
  }

  .stats-card {
    border-radius: 8px;
    padding: 20px;
    position: relative;
  }

  .stats-header {
    display: flex;
    justify-content: space-between;
    align-items: center;
    margin-bottom: 20px;

    h3 {
      margin: 0;
      font-size: 14px;
      font-weight: 600;
      color: rgb(var(--clr) / 90%);
      display: flex;
      align-items: center;
      gap: 8px;
    }
  }

  .stats-actions {
    display: flex;
    gap: 8px;
    align-items: center;
  }

  .stats-action-btn {
    display: flex;
    align-items: center;
    justify-content: center;
    width: 28px;
    height: 28px;
    border-radius: 6px;
    background: transparent;
    border: 1px solid transparent;
    color: rgb(var(--clr) / 40%);
    cursor: pointer;
    transition: all 0.2s;

    &:hover {
      background: rgb(var(--clr) / 5%);
      color: rgb(var(--clr));
      border-color: rgb(var(--clr) / 10%);
    }

    &.refresh.spinning {
      animation: spin 1s linear infinite;
      color: rgb(var(--clrPrm));
    }
  }

  .ping-stats-grid {
    display: grid;
    grid-template-columns: repeat(4, 1fr);
    gap: 12px;
    margin-bottom: 20px;
  }

  .ping-grid-mini {
    display: flex;
    flex-wrap: wrap;
    gap: 3px;
    margin-top: 12px;
    padding-top: 12px;
    border-top: 1px solid rgb(var(--clr) / 5%);
  }

  .ping-dot {
    width: 6px;
    height: 6px;
    border-radius: 50%;
    background: rgb(var(--clr) / 10%);

    &.success {
      background: #22c55e;
    }
    &.warning {
      background: #f59e0b;
    }
    &.error {
      background: #ef4444;
    }
  }

  .stats-card {
    border-radius: 8px;
    padding: 24px;
    position: relative;
  }

  .stats-header {
    margin-bottom: 24px;

    h3 {
      font-size: 15px;
      letter-spacing: 0.2px;

      svg {
        opacity: 0.8;
        color: rgb(var(--clrPrm));
      }
    }
  }

  .stats-action-btn {
    width: 32px;
    height: 32px;
    border-radius: 8px;

    &.close:hover {
      color: #ef4444;
      background: rgba(239, 68, 68, 0.1);
      border-color: rgba(239, 68, 68, 0.2);
    }
    &.scan:hover {
      color: rgb(var(--clrPrm));
      background: rgba(var(--clrPrm), 0.1);
      border-color: rgba(var(--clrPrm), 0.2);
    }
    &.scan-full:hover {
      color: #fbbf24;
      background: rgba(251, 191, 36, 0.1);
      border-color: rgba(251, 191, 36, 0.2);
    }
  }

  .stat-box {
    background: rgb(var(--bg1) / 30%);
    border: 1px solid rgb(var(--clr) / 6%);
    padding: 12px 16px;
    border-radius: 10px;
    transition: all 0.2s;

    &:hover {
      background: rgb(var(--bg1) / 50%);
      border-color: rgb(var(--clr) / 12%);
    }
  }

  .stat-value {
    font-size: 14px;
    &.loss {
      color: #ef4444;
    }
  }

  .stats-empty {
    padding: 32px;
    text-align: center;
    color: rgb(var(--clr) / 40%);
    font-size: 13px;
    font-style: italic;
  }

  .stats-loading,
  .stats-error {
    display: flex;
    flex-direction: column;
    align-items: center;
    justify-content: center;
    padding: 32px 20px;
    gap: 16px;
    width: 100%;
    text-align: center;

    p {
      color: rgb(var(--clr) / 60%);
      font-size: 13px;
      margin: 0;
      max-width: 280px;
      line-height: 1.5;
    }
  }

  /* Stats Skeleton Styles */
  .stats-skeleton {
    width: 100%;
    padding: 16px;
  }

  .skeleton-header-row {
    display: flex;
    justify-content: space-between;
    align-items: center;
    margin-bottom: 20px;
  }

  .skeleton-title {
    height: 22px;
    width: 200px;
    border-radius: 4px;
    background: linear-gradient(
      90deg,
      rgb(var(--clr) / 8%) 25%,
      rgb(var(--clr) / 15%) 50%,
      rgb(var(--clr) / 8%) 75%
    );
    background-size: 200% 100%;
    animation: skeleton-shimmer 1.5s infinite;
  }

  .skeleton-close {
    height: 28px;
    width: 28px;
    border-radius: 6px;
    background: linear-gradient(
      90deg,
      rgb(var(--clr) / 8%) 25%,
      rgb(var(--clr) / 15%) 50%,
      rgb(var(--clr) / 8%) 75%
    );
    background-size: 200% 100%;
    animation: skeleton-shimmer 1.5s infinite;
  }

  .skeleton-grid {
    display: grid;
    grid-template-columns: repeat(4, 1fr);
    gap: 12px;
    margin-bottom: 20px;
  }

  .skeleton-stat-card {
    height: 70px;
    border-radius: 10px;
    background: linear-gradient(
      90deg,
      rgb(var(--clr) / 5%) 25%,
      rgb(var(--clr) / 10%) 50%,
      rgb(var(--clr) / 5%) 75%
    );
    background-size: 200% 100%;
    animation: skeleton-shimmer 1.5s infinite;
    border: 1px solid rgb(var(--clr) / 8%);
  }

  .skeleton-ports-section {
    margin-top: 16px;
  }

  .skeleton-port-label {
    height: 14px;
    width: 120px;
    border-radius: 4px;
    margin-bottom: 12px;
    background: linear-gradient(
      90deg,
      rgb(var(--clr) / 8%) 25%,
      rgb(var(--clr) / 15%) 50%,
      rgb(var(--clr) / 8%) 75%
    );
    background-size: 200% 100%;
    animation: skeleton-shimmer 1.5s infinite;
  }

  .skeleton-ports-grid {
    display: flex;
    flex-wrap: wrap;
    gap: 8px;
  }

  .skeleton-port {
    height: 56px;
    width: 80px;
    border-radius: 8px;
    background: linear-gradient(
      90deg,
      rgb(var(--clr) / 5%) 25%,
      rgb(var(--clr) / 10%) 50%,
      rgb(var(--clr) / 5%) 75%
    );
    background-size: 200% 100%;
    animation: skeleton-shimmer 1.5s infinite;
    border: 1px solid rgb(var(--clr) / 8%);
  }

  @keyframes skeleton-shimmer {
    0% {
      background-position: 200% 0;
    }
    100% {
      background-position: -200% 0;
    }
  }

  /* Scan Progress Card Styles */
  .scan-progress-card {
    width: 100%;
    padding: 24px;
    display: flex;
    flex-direction: column;
    gap: 20px;
  }

  .scan-progress-header {
    display: flex;
    align-items: flex-start;
    gap: 16px;
  }

  .scan-icon-container {
    flex-shrink: 0;
    width: 48px;
    height: 48px;
    display: flex;
    align-items: center;
    justify-content: center;
    background: rgb(var(--clrPrm) / 10%);
    border-radius: 12px;
    color: rgb(var(--clrPrm));
  }

  .scan-icon.spinning {
    animation: spin 1s linear infinite;
  }

  @keyframes spin {
    from {
      transform: rotate(0deg);
    }
    to {
      transform: rotate(360deg);
    }
  }

  .scan-header {
    display: flex;
    justify-content: space-between;
    align-items: center;
    margin-bottom: 8px;
  }

  .scan-status-info {
    display: flex;
    align-items: center;
    gap: 8px;
  }

  .scan-actions {
    display: flex;
    gap: 8px;
  }

  .scan-btn {
    width: 28px;
    height: 28px;
    display: flex;
    align-items: center;
    justify-content: center;
    border-radius: 6px;
    border: 1px solid rgb(var(--clr) / 10%);
    background: transparent;
    color: rgb(var(--clr) / 80%);
    cursor: pointer;
    transition: all 0.2s;
    padding: 0;

    &:hover {
      background: rgb(var(--bg3));
      color: rgb(var(--clr));
      border-color: rgb(var(--clr) / 20%);
    }

    &.pause:hover {
      color: #fbbf24;
      background: rgba(251, 191, 36, 0.1);
      border-color: rgba(251, 191, 36, 0.2);
    }

    &.resume:hover {
      color: #10b981;
      background: rgba(16, 185, 129, 0.1);
      border-color: rgba(16, 185, 129, 0.2);
    }

    &.kill:hover {
      color: #ef4444;
      background: rgba(239, 68, 68, 0.1);
      border-color: rgba(239, 68, 68, 0.2);
    }

    svg {
      pointer-events: none;
    }
  }

  .scan-status-badge {
    padding: 2px 6px;
    border-radius: 4px;
    font-size: 10px;
    font-weight: 700;
    letter-spacing: 0.5px;

    &.running {
      background: rgba(var(--clrPrm), 0.1);
      color: rgb(var(--clrPrm));
    }

    &.paused {
      background: rgba(251, 191, 36, 0.1);
      color: #fbbf24;
    }
  }

  .scan-label {
    font-size: 12px;
    font-weight: 500;
    color: rgb(var(--clr) / 60%);
  }

  .scan-progress-info {
    flex: 1;

    h3 {
      margin: 0 0 6px;
      font-size: 16px;
      font-weight: 600;
      color: rgb(var(--clr));
    }

    .scan-description {
      margin: 0;
      font-size: 13px;
      color: rgb(var(--clr) / 65%);
      line-height: 1.5;
    }
  }

  .progress-bar-container {
    width: 100%;
  }

  .progress-bar-fill {
    height: 100%;
    background: linear-gradient(
      90deg,
      rgb(var(--clrPrm)) 0%,
      rgb(var(--prm-300) / 40%) 50%,
      rgb(var(--clrPrm)) 100%
    );
    background-size: 200% 100%;
    animation: progress-shimmer 2s infinite linear;
    border-radius: 4px;
    transition: width 0.3s ease-in-out;
    position: relative;
    box-shadow: 0 0 10px rgb(var(--clrPrm) / 30%);

    &.paused {
      background: #fbbf24;
      animation: none;
      box-shadow: none;
    }
  }

  @keyframes progress-shimmer {
    0% {
      background-position: 200% 0;
    }
    100% {
      background-position: -200% 0;
    }
  }

  .progress-stats {
    display: flex;
    justify-content: space-between;
    margin-top: 8px;
    font-size: 12px;
  }

  .progress-percent {
    color: rgb(var(--clrPrm));
    font-weight: 600;
  }

  .progress-time {
    color: rgb(var(--clr) / 50%);
  }

  .scan-details {
    display: flex;
    flex-wrap: wrap;
    gap: 16px;
    padding-top: 12px;
    border-top: 1px solid rgb(var(--clr) / 10%);
  }

  .scan-detail-item {
    display: flex;
    align-items: center;
    gap: 6px;
    font-size: 12px;
    color: rgb(var(--clr) / 60%);

    svg {
      opacity: 0.7;
    }
  }

  .stats-error {
    .error-icon-svg {
      opacity: 0.9;
    }

    p {
      color: #ef4444;
      font-size: 14px;
      font-weight: 600;
      text-align: center;
      margin: 0;
    }

    .error-actions {
      display: flex;
      gap: 8px;
      margin-top: 8px;
    }

    .retry-btn,
    .close-btn {
      display: flex;
      align-items: center;
      gap: 6px;
      padding: 8px 16px;
      border-radius: 6px;
      font-size: 13px;
      font-weight: 600;
      cursor: pointer;
      transition: all 0.15s;
    }

    .retry-btn {
      background: rgb(var(--clrPrm));
      color: white;
      border: none;

      &:hover {
        background: rgb(var(--clrPrm) / 85%);
        transform: translateY(-1px);
      }
    }

    .close-btn {
      background: transparent;
      color: rgb(var(--clr) / 70%);
      border: 1px solid rgb(var(--clr) / 15%);

      &:hover {
        background: rgb(var(--clr) / 8%);
        border-color: rgb(var(--clr) / 25%);
      }
    }
  }

  /* Empty state for no ports */
  .stats-empty {
    display: flex;
    flex-direction: column;
    align-items: center;
    justify-content: center;
    padding: 40px 20px;
    gap: 8px;

    .empty-icon {
      color: rgb(var(--clr) / 35%);
      margin-bottom: 8px;
    }

    .empty-title {
      color: rgb(var(--clr) / 80%);
      font-size: 15px;
      font-weight: 600;
      margin: 0;
    }

    .empty-subtitle {
      color: rgb(var(--clr) / 50%);
      font-size: 13px;
      margin: 0;
      text-align: center;
      max-width: 280px;
    }

    .empty-actions {
      display: flex;
      gap: 8px;
      margin-top: 12px;
    }

    .close-btn {
      display: flex;
      align-items: center;
      gap: 6px;
      padding: 8px 16px;
      border-radius: 6px;
      font-size: 13px;
      font-weight: 600;
      cursor: pointer;
      transition: all 0.15s;
      background: transparent;
      color: rgb(var(--clr) / 70%);
      border: 1px solid rgb(var(--clr) / 15%);

      &:hover {
        background: rgb(var(--clr) / 8%);
        border-color: rgb(var(--clr) / 25%);
      }
    }
  }

  /* Cached badge styles */
  .stats-header-left {
    display: flex;
    align-items: center;
    gap: 10px;
  }

  .cached-badge {
    display: inline-flex;
    align-items: center;
    gap: 4px;
    padding: 3px 8px;
    border-radius: 4px;
    font-size: 11px;
    font-weight: 600;
    background: rgb(var(--clr) / 8%);
    color: rgb(var(--clr) / 60%);
    border: 1px solid rgb(var(--clr) / 12%);
  }

  .scanning-badge {
    display: inline-flex;
    align-items: center;
    gap: 4px;
    padding: 3px 8px;
    border-radius: 4px;
    font-size: 11px;
    font-weight: 600;
    background: rgba(34, 197, 94, 0.1);
    color: #22c55e;
    border: 1px solid rgba(34, 197, 94, 0.2);
  }

  .scanning-badge .spinning {
    animation: spin 1s linear infinite;
  }

  @media (max-width: 768px) {
    .stats-grid {
      grid-template-columns: repeat(2, 1fr);
    }

    .ports-scroll-container {
      margin: 0 -8px;
      padding: 0 8px;
    }

    .ports-grid {
      grid-template-columns: repeat(auto-fill, minmax(110px, 1fr));
      gap: 4px;
    }

    .port-card {
      padding: 6px;
      min-width: 110px;
    }

    .port-number {
      font-size: 14px;
    }

    .port-service {
      font-size: 10px;
    }

    .services-grid {
      flex-direction: column;
    }
  }

  .status-bar {
    display: flex;
    gap: 16px;
    padding: 8px 24px;
    background: rgb(var(--bg2));
    border-top: 1px solid rgb(var(--clr) / 10%);
    font-size: 12px;
    color: rgb(var(--clr) / 80%);
    align-items: center;
  }

  /* Pagination strip — pushed to the right edge of the status bar.
     Tiny, monochrome, matches the surrounding 12px text. */
  .pager {
    display: flex;
    align-items: center;
    gap: 6px;
    margin-left: auto;
  }
  .pager-size {
    display: inline-flex;
    align-items: center;
    gap: 4px;
    color: rgb(var(--clr) / 60%);
  }
  .pager-size select {
    background: rgb(var(--bg1));
    color: rgb(var(--clr) / 90%);
    border: 1px solid rgb(var(--clr) / 14%);
    border-radius: 4px;
    font-size: 12px;
    padding: 2px 6px;
    cursor: pointer;
    appearance: none;
  }
  .pager-size select:focus-visible {
    outline: 1px solid rgb(var(--clr) / 30%);
    outline-offset: 1px;
  }
  .pager-btn {
    display: inline-flex;
    align-items: center;
    justify-content: center;
    width: 22px;
    height: 22px;
    border: 1px solid rgb(var(--clr) / 14%);
    border-radius: 4px;
    background: rgb(var(--bg1));
    color: rgb(var(--clr) / 80%);
    font-size: 14px;
    line-height: 1;
    cursor: pointer;
    user-select: none;
  }
  .pager-btn:hover:not(:disabled) {
    background: rgb(var(--clr) / 6%);
  }
  .pager-btn:disabled {
    opacity: 0.4;
    cursor: not-allowed;
  }
  .pager-label {
    min-width: 44px;
    text-align: center;
    color: rgb(var(--clr) / 70%);
    font-variant-numeric: tabular-nums;
  }
  @media (max-width: 600px) {
    .pager-size span {
      display: none;
    }
  }

  .error-message {
    padding: 12px;
    background: rgba(239, 68, 68, 0.1);
    border: 1px solid rgba(239, 68, 68, 0.3);
    border-radius: 6px;
    color: #ef4444;
    margin-bottom: 16px;
  }

  .loading-state {
    display: flex;
    justify-content: center;
    align-items: center;
    padding: 64px 24px;
    color: rgb(var(--clr) / 66%);
  }

  .empty-state {
    display: flex;
    flex-direction: column;
    align-items: center;
    justify-content: center;
    padding: 64px 24px;
    text-align: center;

    .empty-icon {
      font-size: 64px;
      margin-bottom: 16px;
      opacity: 0.5;
    }

    p {
      margin: 0 0 16px;
      color: rgb(var(--clr) / 70%);
    }

    .hint-text {
      font-size: 13px;
      color: rgb(var(--clr) / 50%);
      font-style: italic;
    }
  }

  .add-peer-btn {
    padding: 10px 20px;
    background: rgb(var(--clrPrm));
    color: white;
    border: none;
    border-radius: 6px;
    cursor: pointer;
    font-size: 14px;
    font-weight: 600;
    transition: all 0.2s;

    &:hover {
      background: rgb(var(--clrPrm) / 90%);
      transform: translateY(-1px);
    }

    &:active {
      transform: translateY(0);
    }
  }

  @media (max-width: 1200px) {
    .ping-stats-grid {
      grid-template-columns: repeat(3, 1fr);
    }
  }
  .mobile-peer-list {
    display: none;
  }
  @media (max-width: 768px) {
    .mobile-peer-list {
      display: flex;
    }
    .peers {
      min-width: 100%;
      left: 0 !important;
      top: 0 !important;
      width: 100vw !important;
      height: calc(100vh - 48px) !important;
      border-radius: 0;
      overflow-x: hidden;
    }

    .content {
      padding: 0px 1px;
      overflow-x: hidden;
    }

    .search-container {
      max-width: none;
      flex: 1;
    }

    .toolbar {
      flex-direction: row;
      flex-wrap: wrap;
      align-items: center;
      gap: 8px;
      height: auto;
      min-height: 48px;
      padding: 8px 0;
      top: 0;
      position: relative;
      background: transparent;
    }

    .toolbar-actions {
      display: flex;
      gap: 4px;
    }

    .icon-btn {
      width: 32px;
      height: 32px;
      padding: 4px;
    }

    .peers-table {
      font-size: 11px;
      table-layout: fixed;
      width: 100%;
    }

    .peers-table th,
    .peers-table td {
      padding: 4px 4px;
    }

    .col-ip,
    .col-lastseen {
      display: none;
    }

    .col-actions {
      width: 100px;
    }

    .col-status {
      width: 50px;
    }

    .col-name {
      width: auto;
      max-width: 120px;
      overflow: hidden;
      text-overflow: ellipsis;
      white-space: nowrap;
    }

    .col-actions .action-buttons {
      flex-wrap: nowrap;
      justify-content: flex-end;
      gap: 2px;
    }

    .col-actions .action-btn {
      width: 24px;
      height: 24px;
      padding: 3px;
    }

    .col-actions .action-btn svg {
      width: 12px;
      height: 12px;
    }

    .ping-stats-grid {
      grid-template-columns: repeat(2, 1fr);
    }

    .fingerprint-section .device-info-grid {
      grid-template-columns: 1fr 1fr;
    }

    .expanded-content {
      padding: 4px 8px;
    }

    .expanded-inner {
      padding: 4px;
      gap: 6px;
    }

    .fingerprint-section {
      padding: 8px;
      border-radius: 6px;
    }

    .fingerprint-grid {
      gap: 6px;
    }

    .fp-label {
      font-size: 9px;
    }

    .fp-value {
      font-size: 11px;
    }

    .info-row {
      padding: 4px 0;
    }

    .info-label {
      font-size: 10px;
    }

    .info-value {
      font-size: 11px;
    }

    .group-header td {
      padding: 6px 8px;
      font-size: 11px;
      border-radius: 6px;
    }

    /* Media Queries for Responsive View */
    @media (max-width: 768px) {
      .peers-table {
        display: none;
      }
      .mobile-peer-list {
        display: flex;
        padding: 12px;
      }
    }

    @media (min-width: 769px) {
      .mobile-peer-list {
        display: none;
      }
    }

    .mobile-peer-list {
      display: flex;
      flex-direction: column;
      gap: 12px;
      padding-bottom: 24px;
    }

    .group-header-mobile {
      padding: 16px 4px 8px;
      font-size: 11px;
      font-weight: 700;
      color: rgb(var(--clr) / 40%);
      text-transform: uppercase;
      letter-spacing: 1px;
      display: flex;
      align-items: center;
      gap: 8px;

      &::after {
        content: "";
        flex: 1;
        height: 1px;
        background: rgb(var(--clr) / 5%);
      }
    }

    .peer-mobile-card {
      background: rgb(var(--bg3) / 50%);
      backdrop-filter: blur(20px);
      -webkit-backdrop-filter: blur(20px);
      border: 1px solid rgb(var(--clr) / 8%);
      border-radius: 16px;
      overflow: hidden;
      transition: all 0.2s cubic-bezier(0.4, 0, 0.2, 1);
      box-shadow: 0 4px 12px rgb(0 0 0 / 5%);

      &.expanded {
        background: rgb(var(--bg3) / 80%);
        border-color: rgb(var(--clr) / 15%);
        box-shadow: 0 8px 24px rgb(0 0 0 / 12%);
        transform: scale(1.02);
        z-index: 5;
      }
    }

    .card-main {
      padding: 16px;
      display: flex;
      justify-content: space-between;
      align-items: center;
      cursor: pointer;
      -webkit-tap-highlight-color: transparent;

      &:active {
        background: rgb(var(--clr) / 5%);
      }
    }

    .card-info {
      display: flex;
      align-items: center;
      gap: 12px;
    }

    .status-dot-mobile {
      width: 10px;
      height: 10px;
      border-radius: 50%;
      background: #4b5563;
      box-shadow: 0 0 0 3px rgb(0 0 0 / 5%);

      &.online {
        background: #22c55e;
        box-shadow: 0 0 8px rgb(34 197 94 / 40%);
      }
    }

    .name-box {
      display: flex;
      flex-direction: column;
      gap: 2px;
    }

    .peer-name {
      font-size: 11px;
      font-weight: 500;
      color: rgb(var(--clr));
      line-height: 1.2;
    }

    .peer-hostname {
      font-size: 11px;
      color: rgb(var(--clr) / 50%);
    }

    .card-meta {
      display: flex;
      align-items: center;
      gap: 8px;
    }

    .ip-badge {
      font-size: 11px;
      font-family: ui-monospace, SFMono-Regular, Menlo, Monaco, Consolas,
        monospace;
      padding: 4px 8px;
      background: rgb(var(--clr) / 5%);
      border-radius: 6px;
      color: rgb(var(--clr) / 60%);
    }

    .expand-icon {
      color: rgb(var(--clr) / 30%);
      transition: transform 0.3s cubic-bezier(0.4, 0, 0.2, 1);

      &.expanded {
        transform: rotate(180deg);
        color: rgb(var(--clrPrm));
      }
    }

    .card-actions {
      display: flex;
      padding: 4px 12px 16px;
      gap: 8px;
      border-top: 1px solid rgb(var(--clr) / 5%);

      .action-btn {
        flex: 1;
        height: 44px;
        display: flex;
        align-items: center;
        justify-content: center;
        background: rgb(var(--bg2));
        border: 1px solid rgb(var(--clr) / 8%);
        border-radius: 12px;
        color: rgb(var(--clr) / 70%);

        svg {
          width: 20px;
          height: 20px;
        }

        &.active {
          background: rgb(var(--clrPrm) / 10%);
          color: rgb(var(--clrPrm));
          border-color: rgb(var(--clrPrm) / 20%);
        }

        &:active {
          background: rgb(var(--clr) / 10%);
          transform: scale(0.95);
        }
      }
    }

    .card-expanded-content {
      border-top: 1px solid rgb(var(--clr) / 5%);
      background: rgb(0 0 0 / 5%);
    }
  }

  .group-header td {
    background: var(--bg2);
    padding: 10px 16px;
    font-weight: 600;
    color: var(--clr);
    border-bottom: 1px solid var(--clr);
    border-top: 1px solid var(--clr);
  }

  /* Rich Modal Styles */
  .modal-overlay {
    position: fixed;
    inset: 0;
    z-index: 200;
    background: rgba(0, 0, 0, 0.5);
    backdrop-filter: blur(4px);
    display: flex;
    align-items: center;
    justify-content: center;
  }

  .modal {
    background: rgb(var(--bg1));
    border: 1px solid rgb(var(--clr) / 10%);
    border-radius: 12px;
    box-shadow:
      0 20px 25px -5px rgb(0 0 0 / 10%),
      0 8px 10px -6px rgb(0 0 0 / 10%);
    width: 100%;
    max-width: 420px;
    overflow: hidden;
    animation: modalSlide 0.2s ease-out;
  }

  @keyframes modalSlide {
    from {
      opacity: 0;
      transform: translateY(10px) scale(0.95);
    }
    to {
      opacity: 1;
      transform: translateY(0) scale(1);
    }
  }

  .modal-header {
    padding: 20px 24px;
    border-bottom: 1px solid rgb(var(--clr) / 5%);
    background: rgb(var(--bg2));

    h3 {
      margin: 0;
      font-size: 18px;
      font-weight: 600;
      color: rgb(var(--clr));
    }
  }

  .modal-body {
    padding: 24px;
    display: flex;
    flex-direction: column;
    gap: 20px;
  }

  .form-group label {
    display: block;
    margin-bottom: 8px;
    font-size: 13px;
    font-weight: 500;
    color: rgb(var(--clr) / 70%);
  }

  .styled-input {
    width: 100%;
    background: rgb(var(--bg8));
    border: 1px solid var(--border-color);
    border-radius: var(--radius-sm);
    padding: var(--sp-3) var(--sp-4);
    color: rgb(var(--clr));
    font-size: var(--text-base);
    outline: none;
    transition: var(--trans-normal);
    box-shadow: 0 1px 1px rgb(0 0 0 / 5%);

    &:focus {
      border-color: var(--primary);
      box-shadow: 0 0 0 3px rgb(var(--primary-rgb) / 10%);
    }

    &::placeholder {
      color: rgb(var(--clr) / 40%);
    }
  }

  .help-text {
    margin: 6px 0 0;
    font-size: 12px;
    color: rgb(var(--clr) / 50%);
  }

  .preview-box {
    display: flex;
    align-items: center;
    gap: 12px;
    padding: 12px;
    background: rgb(var(--clrPrm) / 10%);
    border: 1px solid rgb(var(--clrPrm) / 20%);
    border-radius: 8px;
    color: rgb(var(--clrPrm));
    font-size: 13px;

    strong {
      color: rgb(var(--clr));
    }
  }

  .toggle-group {
    display: flex;
    background: rgb(var(--bg3));
    padding: 4px;
    border-radius: 8px;
    border: 1px solid rgb(var(--clr) / 10%);
  }

  .toggle-btn {
    flex: 1;
    padding: 8px;
    border: none;
    background: transparent;
    border-radius: 6px;
    font-size: 13px;
    font-weight: 500;
    color: rgb(var(--clr) / 60%);
    cursor: pointer;
    transition: all 0.2s;

    &.selected {
      background: rgb(var(--bg1));
      color: rgb(var(--clr));
      box-shadow: 0 1px 3px rgb(0 0 0 / 10%);
    }

    &:hover:not(.selected) {
      color: rgb(var(--clr));
    }
  }

  .modal-actions {
    padding: 16px 24px;
    background: rgb(var(--bg2));
    border-top: 1px solid rgb(var(--clr) / 5%);
    display: flex;
    justify-content: flex-end;
    gap: 12px;
  }

  .btn-primary {
    background: rgb(var(--clrPrm));
    color: white;
    box-shadow: 0 4px 6px -1px rgb(var(--clrPrm) / 20%);

    &:hover {
      background: rgb(var(--clrPrm) / 90%);
      transform: translateY(-1px);
    }

    &.bg-danger {
      background: #ef4444;
      box-shadow: 0 4px 6px -1px rgba(239, 68, 68, 0.3);

      &:hover {
        background: #dc2626;
      }
    }
  }

  .btn-secondary {
    background: transparent;
    color: rgb(var(--clr) / 70%);

    &:hover {
      background: rgb(var(--clr) / 5%);
      color: rgb(var(--clr));
    }
  }

  /* Danger-state Fluent accent button: override the --fds-accent-* tokens
     only inside .modal-actions.danger. Custom properties cascade into the
     <Button class="style-accent"> child, so its background/hover/pressed
     states automatically pick up the red palette without needing !important
     or :global selectors against fluent-svelte's internals. */
  .modal-actions.danger {
    --fds-accent-default: #dc2626;
    --fds-accent-secondary: #ef4444;
    --fds-accent-tertiary: #b91c1c;
    --fds-text-on-accent-primary: #ffffff;
    --fds-text-on-accent-secondary: rgba(255, 255, 255, 0.9);
  }
  .ping-chart-card {
    position: relative;
    width: 100%;
    height: 180px;
    background: linear-gradient(
      135deg,
      rgba(var(--bg3), 0.9) 0%,
      rgba(var(--bg2), 0.95) 100%
    );
    border-radius: 16px;
    overflow: hidden;
    border: 1px solid rgb(var(--clr) / 8%);
    box-shadow:
      0 4px 24px rgba(0, 0, 0, 0.12),
      0 1px 2px rgba(0, 0, 0, 0.08),
      inset 0 1px 0 rgba(255, 255, 255, 0.05);
  }

  .ping-svg-chart {
    width: 100%;
    height: 100%;
    display: block;
  }

  .ping-area {
    opacity: 0;
    animation: fadeInArea 0.8s ease-out forwards;
  }

  .ping-line {
    stroke-dasharray: 500px;
    stroke-dashoffset: 500px;
    animation: drawLine 1.2s ease-out forwards;
  }

  @keyframes fadeInArea {
    0% {
      opacity: 0;
      transform: translateY(10px);
    }
    100% {
      opacity: 1;
      transform: translateY(0);
    }
  }

  @keyframes drawLine {
    0% {
      stroke-dashoffset: 500px;
    }
    100% {
      stroke-dashoffset: 0;
    }
  }

  .ping-hit-area {
    cursor: crosshair;
    pointer-events: all;
  }

  .chart-labels {
    position: absolute;
    top: 16px;
    left: 20px;
    right: 20px;
    display: flex;
    justify-content: space-between;
    align-items: flex-start;
    pointer-events: none;
    z-index: 5;
  }

  .chart-labels .label {
    display: flex;
    flex-direction: column;
    gap: 2px;
  }

  .chart-labels .label.avg .value {
    font-size: 32px;
    font-weight: 700;
    color: #22c55e;
    line-height: 1;
    text-shadow: 0 2px 8px rgba(34, 197, 94, 0.3);
  }

  .chart-labels .label.avg .unit {
    font-size: 12px;
    color: rgb(var(--clr) / 50%);
    font-weight: 500;
    text-transform: uppercase;
    letter-spacing: 0.5px;
  }

  .chart-labels .label.stats {
    text-align: right;
    align-items: flex-end;
  }

  .chart-labels .label.stats .success-count {
    font-size: 18px;
    font-weight: 600;
    color: rgb(var(--clr) / 80%);
    font-family: "Cascadia Code", monospace;
  }

  .chart-labels .label.stats .loss {
    font-size: 11px;
    color: #22c55e;
    font-weight: 600;
  }

  .chart-labels .label.stats .loss.has-loss {
    color: #fbbf24;
  }

  .chart-tooltip {
    position: absolute;
    padding: 8px 12px;
    background: rgba(var(--bg1), 0.98);
    backdrop-filter: blur(12px);
    border-radius: 8px;
    border: 1px solid rgb(var(--clr) / 15%);
    box-shadow: 0 8px 24px rgba(0, 0, 0, 0.2);
    display: flex;
    flex-direction: column;
    gap: 2px;
    z-index: 100;
    pointer-events: none;
    transform: translateX(-50%);
  }

  .chart-tooltip .tooltip-ping {
    font-size: 10px;
    color: rgb(var(--clr) / 50%);
    text-transform: uppercase;
    letter-spacing: 0.5px;
  }

  .chart-tooltip .tooltip-rtt {
    font-size: 14px;
    font-weight: 700;
    color: var(--primary);
  }

  .chart-tooltip .tooltip-rtt.failed {
    color: #ef4444;
  }

  .close-ping-btn {
    position: absolute;
    top: 12px;
    right: 12px;
    background: rgba(var(--bg1), 0.5);
    border: 1px solid rgba(var(--clr), 0.1);
    color: rgb(var(--clr) / 60%);
    width: 28px;
    height: 28px;
    border-radius: 6px;
    display: flex;
    align-items: center;
    justify-content: center;
    cursor: pointer;
    backdrop-filter: blur(4px);
    transition: all 0.2s;
    z-index: 10;
  }

  .close-ping-btn:hover {
    background: rgba(var(--bg1), 0.8);
    color: rgb(var(--clr));
    transform: scale(1.05);
  }

  .ping-empty {
    width: 100%;
    height: 180px;
    display: flex;
    align-items: center;
    justify-content: center;
    background: rgb(var(--bg2));
    border-radius: 16px;
    border: 1px dashed rgb(var(--clr) / 10%);
    color: rgb(var(--clr) / 40%);
    font-size: 13px;
  }

  /* Group Tags Redesign */
  .group-title-tag {
    display: inline-flex;
    align-items: stretch;
    font-family: ui-monospace, SFMono-Regular, Menlo, Monaco, Consolas,
      "Liberation Mono", "Courier New", monospace;
    font-size: 13px;
    line-height: normal; /* Fix alignment */
    border-radius: 4px;
    overflow: hidden;
    box-shadow: 0 1px 2px rgba(0, 0, 0, 0.1);
  }

  .tag-icon {
    background-color: #10a37f; /* OpenAI Green-ish or standard green */
    background-color: #16a34a; /* Green 600 */
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
    background-color: #16a34a; /* Green 600 */
    color: white;
    padding: 4px 10px;
    font-weight: 700;
    display: flex;
    align-items: center;
    justify-content: center;
  }
</style>
