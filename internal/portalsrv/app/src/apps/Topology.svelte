<script lang="ts">
  import { draggable } from "@neodrag/svelte";
  import { scale, fade } from "svelte/transition";
  import { onMount, tick, onDestroy } from "svelte";
  import Titlebar from "$components/shared/Titlebar.svelte";
  import { topologyStore } from "$store/topology";
  import { accountStore } from "$store/account";
  import { wsStore } from "$store/websocket";
  import {
    activeThing,
    openedApps,
    appZIndexes,
    bringToFront,
  } from "$store/store";
  import { translateError$, _ } from "$store/i18n";
  import {
    Button,
    MenuFlyout,
    MenuFlyoutItem,
    MenuFlyoutDivider,
  } from "fluent-svelte";
  import * as d3 from "d3";
  import { peerStore } from "$store/peer";
  import { formatRelativeTime, formatLocalDateTime } from "$lib/dateUtils";

  // Portal action: moves the node to document.body so it escapes any
  // CSS transform / overflow:hidden parent (e.g. the draggable window).
  function portal(node: HTMLElement) {
    document.body.appendChild(node);
    return {
      destroy() {
        if (node.parentNode) node.parentNode.removeChild(node);
      },
    };
  }

  // Window state
  let isMaximized = false;
  let isMinimized = false;
  let windowElement: HTMLElement;
  let svgElement: SVGSVGElement;
  let gElement: SVGGElement;

  // Z-index for window stacking
  $: zIndex = $appZIndexes["Topology"] || 100;

  // Watch activeThing to restore when clicked from taskbar
  $: if ($activeThing === "Topology" && isMinimized) {
    isMinimized = false;
  }

  // Bring to front when activated
  $: if ($activeThing === "Topology") {
    bringToFront("Topology");
  }

  function handleFocus() {
    $activeThing = "Topology";
    bringToFront("Topology");
  }

  // State
  let containerWidth: number;
  let containerHeight: number;
  let groupLinks: any[] = [];
  let d3Nodes: any[] = [];
  let d3Links: any[] = [];
  let simulation: d3.Simulation<any, any>;
  let zoomHandler: any;

  const NODE_RADIUS = 35;
  const LINK_TARGET_PADDING = 12;

  function getIconUrl(type: string, fp: any) {
    let iconUrl = "/img/icon/generic/device.png";
    const genericIcons: Record<string, string> = {
      router: "/img/icon/generic/router.png",
      device: "/img/icon/generic/device.png",
      server: "/img/icon/generic/server.png",
      global_server: "/img/icon/generic/global_server.png",
      peer: "/img/icon/generic/peer.png",
      tenant: "/img/icon/generic/tenant.png",
      internet: "/img/icon/generic/internet.png",
    };
    if (genericIcons[type]) iconUrl = genericIcons[type];
    if (fp) {
      const vendor = (fp.vendor || "").toLowerCase();
      const os = (fp.os_family || "").toLowerCase();
      if (vendor.includes("mikrotik") || os.includes("routeros"))
        iconUrl = "/img/icon/brands/mikrotik.png";
      else if (vendor.includes("microsoft") || os.includes("windows"))
        iconUrl = "/img/icon/brands/windows.png";
      else if (
        vendor.includes("apple") ||
        os.includes("macos") ||
        os.includes("ios")
      )
        iconUrl = "/img/icon/brands/apple.png";
      else if (
        vendor.includes("ubiquiti") ||
        os.includes("edgeos") ||
        os.includes("unifi")
      )
        iconUrl = "/img/icon/brands/ubiquiti.png";
      else if (vendor.includes("cisco")) iconUrl = "/img/icon/brands/cisco.png";
      else if (
        vendor.includes("linux") ||
        os.includes("linux") ||
        os.includes("openwrt") ||
        os.includes("unix") ||
        os.includes("qsdk")
      )
        iconUrl = "/img/icon/brands/linux.png";
      else if (os.includes("android")) iconUrl = "/img/icon/brands/android.png";
    }
    return iconUrl;
  }
  $: selectedPeers = $topologyStore.selectedPeersForExitNode;

  // Selection state
  let selectedNode: any = null;
  let selectedEdge: any = null;
  let showNodeDetails = false;
  let showEdgeDetails = false;
  let showLegendDialog = false;

  // Context Menu State
  let nodeMenuVisible = false;
  let menuX = 0;
  let menuY = 0;
  let menuNode: any = null;

  // Ping state
  let pingLoading = false;
  let pingResults: any = null;

  $: data = $topologyStore.data;
  $: isLoading = $topologyStore.isLoading;
  $: error = $topologyStore.error;
  $: nodeCount = data.nodes.length;
  $: edgeCount = data.edges.length;

  // D3 Integration
  onMount(() => {
    loadTopologyData().then(() => { initSimulation(); applyLayout(); });
    // Close context menu only when clicking outside it
    function onDocClick(e: MouseEvent) {
      if (!nodeMenuVisible) return;
      const menu = document.querySelector('.topology-context-menu');
      if (!menu || !menu.contains(e.target as Node)) {
        nodeMenuVisible = false;
      }
    }
    document.addEventListener('click', onDocClick, true);
    return () => document.removeEventListener('click', onDocClick, true);
  });

  onDestroy(() => {
    if (simulation) simulation.stop();
  });

  // ---------------------------------------------------------------------------
  // Force helpers — scale repulsion and link distance with graph size so that
  // large hub-spoke topologies (300+ nodes all connected to one gateway) don't
  // collapse into concentric rings.
  // ---------------------------------------------------------------------------

  /** charge strength: scales with sqrt(n) so large graphs spread out */
  function scaledCharge(n: number): number {
    return -Math.max(250, 70 * Math.sqrt(n));
    // examples:  10n → -250  |  50n → -495  |  100n → -700  |  300n → -1212
  }

  /**
   * Per-link distance callback: hub nodes (high degree) get longer links so
   * their spokes fan out instead of stacking in a ring at equal radius.
   */
  function scaledLinkDistance(links: any[]): (l: any) => number {
    const deg = new Map<string, number>();
    links.forEach((l: any) => {
      const s = l.source?.id ?? l.source;
      const t = l.target?.id ?? l.target;
      deg.set(s, (deg.get(s) ?? 0) + 1);
      deg.set(t, (deg.get(t) ?? 0) + 1);
    });
    return (l: any) => {
      const s = l.source?.id ?? l.source;
      const t = l.target?.id ?? l.target;
      const maxDeg = Math.max(deg.get(s) ?? 1, deg.get(t) ?? 1);
      // Hub with 50 connections → ~120px; leaf → ~65px. Keep graph compact.
      return Math.min(150, 55 + maxDeg * 2);
    };
  }

  function initSimulation() {
    if (!svgElement) return;

    // Pre-seed the topology key so the reactive guard doesn't fire
    // updateSimulation() after applyLayout() sets the grid fx/fy positions.
    // Without this, Svelte's pending reactive batch flushes AFTER the .then()
    // callback and updateSimulation wipes all fixed positions.
    _lastTopologyKey =
      data.nodes.map((n: any) => n.id).sort().join(",") +
      "|" +
      data.edges.map((e: any) => e.id).sort().join(",");

    // Initialize D3 Nodes and Links
    d3Nodes = data.nodes.map((n) => ({
      ...n,
      x: Math.random() * containerWidth,
      y: Math.random() * containerHeight,
    }));
    d3Links = data.edges
      .map((e) => ({
        ...e,
        source: d3Nodes.find((n) => n.id === e.source),
        target: d3Nodes.find((n) => n.id === e.target),
      }))
      .filter((l) => l.source && l.target);

    const n = d3Nodes.length;

    simulation = d3
      .forceSimulation(d3Nodes)
      .alphaDecay(0.05)       // Slightly slower decay so large graphs have time to spread
      .velocityDecay(0.55)    // Moderate friction
      .force(
        "link",
        d3
          .forceLink(d3Links)
          .id((d: any) => d.id)
          .distance(scaledLinkDistance(d3Links)),
      )
      .force("charge", d3.forceManyBody().strength(scaledCharge(n)))
      .force("center", d3.forceCenter(containerWidth / 2, containerHeight / 2).strength(0.06))
      // Collision force — prevents nodes from sitting on top of each other.
      .force("collide", d3.forceCollide(50).strength(0.85))
      .on("tick", () => {
        d3Nodes = [...d3Nodes];
        d3Links = [...d3Links];
      });

    // Zoom and Pan
    zoomHandler = d3.zoom().on("zoom", (event) => {
      d3.select(gElement).attr("transform", event.transform);
    });

    d3.select(svgElement).call(zoomHandler);
  }

  // Update simulation when data changes
  // Guard: only restart the simulation when the set of nodes/edges actually
  // changes.  $topologyStore fires for ALL state updates (including selection
  // changes), so without this guard every node click would set alpha > 0 and
  // make every node wiggle.
  let _lastTopologyKey = "";
  $: if (data && simulation) {
    const key =
      data.nodes.map((n: any) => n.id).sort().join(",") +
      "|" +
      data.edges.map((e: any) => e.id).sort().join(",");
    if (key !== _lastTopologyKey) {
      _lastTopologyKey = key;
      updateSimulation();
    }
  }

  function updateSimulation() {
    const existingNodes = new Map(d3Nodes.map((n) => [n.id, n]));
    d3Nodes = data.nodes.map((n) => {
      const existing = existingNodes.get(n.id);
      return existing
        ? { ...n, ...existing }
        : { ...n, x: containerWidth / 2, y: containerHeight / 2 };
    });

    d3Links = data.edges
      .map((e) => ({
        ...e,
        source: d3Nodes.find((n) => n.id === e.source),
        target: d3Nodes.find((n) => n.id === e.target),
      }))
      .filter((l) => l.source && l.target);

    simulation.nodes(d3Nodes);
    const linkForce: any = simulation.force("link");
    if (linkForce) linkForce.links(d3Links);
    simulation.alpha(0.05).restart();
    // Re-apply the active layout so node positions are maintained after a
    // data refresh (e.g. WebSocket update adding/removing a peer).
    applyLayout();
  }

  function drag(simulation: d3.Simulation<any, any>) {
    function dragstarted(event: any) {
      // Fix the node position — do NOT restart simulation so other nodes stay still
      event.subject.fx = event.subject.x;
      event.subject.fy = event.subject.y;

      // Force edge re-render immediately when a drag starts.
      d3Links = [...d3Links];
    }

    function dragged(event: any) {
      event.subject.fx = event.x;
      event.subject.fy = event.y;
      // Also update x/y directly so Svelte transform updates without a simulation tick
      event.subject.x = event.x;
      event.subject.y = event.y;
      d3Nodes = [...d3Nodes]; // trigger Svelte reactivity
      d3Links = [...d3Links]; // links depend on source/target positions too
    }

    function dragended(event: any) {
      // Keep fx/fy fixed — node stays where it was dropped, others unaffected
      d3Links = [...d3Links];
    }

    return d3
      .drag()
      .on("start", dragstarted)
      .on("drag", dragged)
      .on("end", dragended);
  }

  function applyDrag(element: any, d3Node: any) {
    d3.select(element).datum(d3Node).call(drag(simulation) as any);
    return {
      update(newD3Node: any) {
        // Re-bind datum when Svelte re-renders (e.g. after selection change)
        // so drag keeps working after the first click/select
        d3.select(element).datum(newD3Node);
      },
    };
  }

  function linkCoords(link: any) {
    const sx = Number(link?.source?.x ?? 0);
    const sy = Number(link?.source?.y ?? 0);
    const tx = Number(link?.target?.x ?? 0);
    const ty = Number(link?.target?.y ?? 0);

    const dx = tx - sx;
    const dy = ty - sy;
    const length = Math.hypot(dx, dy);

    if (!length || !Number.isFinite(length)) {
      return { x1: sx, y1: sy, x2: tx, y2: ty };
    }

    const ux = dx / length;
    const uy = dy / length;

    const startTrim = Math.min(NODE_RADIUS, Math.max(0, length / 2 - 1));
    const endTrim = Math.min(
      NODE_RADIUS + LINK_TARGET_PADDING,
      Math.max(0, length / 2 - 1),
    );

    return {
      x1: sx + ux * startTrim,
      y1: sy + uy * startTrim,
      x2: tx - ux * endTrim,
      y2: ty - uy * endTrim,
    };
  }

  async function loadTopologyData() {
    try {
      await topologyStore.loadTopology();
      try {
        const response: any = await wsStore.callGRPC(
          "TenantPortalService",
          "ListTenantGroupLinks",
          {},
        );
        groupLinks = response.links || [];
      } catch (err) {
        console.error("Failed to load group links:", err);
      }
    } catch (err) {
      console.error("Failed to load topology:", err);
    }
  }

  function handleMaximize() {
    isMaximized = !isMaximized;
  }

  function handleReduce() {
    isMinimized = true;
    $activeThing = "";
  }

  function handleRefresh() {
    loadTopologyData();
  }

  function handleCreateGroupLink() {
    if (selectedPeers.length < 2) {
      alert($_("topology.selectTwoPeers"));
      return;
    }
    topologyStore.setSelectedPeersForGroupLink(selectedPeers);
    if (!$openedApps.includes("CreateGroupLink")) {
      $openedApps = [...$openedApps, "CreateGroupLink"];
    }
    $activeThing = "CreateGroupLink";
    bringToFront("CreateGroupLink");
  }

  function handleNodeClick(e: any, node: any) {
    if (e && (e.shiftKey || e.ctrlKey || e.metaKey)) {
      handleToggleSelection(node);
    } else {
      selectedNode = node;
      selectedEdge = null;
      showNodeDetails = true;
      showEdgeDetails = false;
      topologyStore.setSelectedPeersForExitNode([node]);
    }
  }

  function handleNodeContextMenu(event: MouseEvent, node: any) {
    event.preventDefault();
    menuNode = node;
    // Clamp position so the menu doesn't overflow viewport edges
    const menuW = 220;
    const menuH = 320;
    menuX = Math.min(event.clientX, window.innerWidth - menuW);
    menuY = Math.min(event.clientY, window.innerHeight - menuH);
    // If near bottom-right, open upward/leftward
    if (menuY < 0) menuY = 4;
    if (menuX < 0) menuX = 4;
    nodeMenuVisible = true;
  }

  async function handlePingNode(node: any) {
    selectedNode = node;
    showNodeDetails = true;
    pingLoading = true;
    pingResults = null;
    try {
      const resp = await peerStore.pingPeer(node.id, 4, 3000);
      if (resp && resp.packets_sent && resp.packets_received !== undefined) {
        if (resp.packet_loss_percent === undefined) {
          resp.packet_loss_percent =
            ((resp.packets_sent - resp.packets_received) / resp.packets_sent) * 100;
        }
      }
      pingResults = resp;
    } catch (err: any) {
      pingResults = { error: err.message || "Ping failed" };
    } finally {
      pingLoading = false;
    }
  }

  function handleWebSSHNode(node: any) {
    // Set the selected peer in peerStore so NewSSHSession can pick it up
    const peer = d3Nodes.find((n) => n.id === node.id);
    if (peer) {
      peerStore.setSelectedPeer(peer);
    }
    // Open NewSSHSession as a separate app window
    if (!$openedApps.includes("NewSSHSession")) {
      $openedApps = [...$openedApps, "NewSSHSession"];
    }
    $activeThing = "NewSSHSession";
    bringToFront("NewSSHSession");
  }

  // Returns true only for peers running the wantasticd client (P2P capable).
  // Gateways, servers, and traditional WireGuard relay-only devices are excluded.
  function isWantasticdPeer(node: any): boolean {
    if (!node) return false;
    const nodeType = String(node.type || '').toLowerCase();
    // Must be a plain peer, not a server/gateway/global_server/router/tenant
    if (nodeType !== 'peer') return false;
    // Must have a Wantastic fingerprint — traditional WG clients won't have this
    if (!node.fingerprint) return false;
    const vendor = String(node.fingerprint.vendor || '').toLowerCase();
    return !(vendor === 'mikrotik' || vendor === 'ubiquiti' || vendor === 'edgeos' || vendor === 'unifi');
  }

  async function handleToggleSelection(node: any) {
    if (selectedPeers.some((p) => p.id === node.id)) {
      topologyStore.setSelectedPeersForExitNode(selectedPeers.filter((p) => p.id !== node.id));
    } else {
      topologyStore.setSelectedPeersForExitNode([...selectedPeers, node]);
    }
     let peer = await peerStore.getPeer(node.id);
      if (selectedPeers.some((p) => p.id === node.id)) {
        peerStore.setSelectedPeer(null);
      } else {
        peerStore.setSelectedPeer(peer);
      }
  }

  function handleEdgeClick(edge: any) {
    selectedEdge = edge;
    selectedNode = null;
    showEdgeDetails = true;
    showNodeDetails = false;
  }

  function hideDetails() {
    showNodeDetails = false;
    showEdgeDetails = false;
    selectedNode = null;
    selectedEdge = null;
  }

  function formatBytes(bytes: number) {
    if (bytes === 0) return "0 B";
    const k = 1024;
    const sizes = ["B", "KB", "MB", "GB", "TB"];
    const i = Math.floor(Math.log(bytes) / Math.log(k));
    return parseFloat((bytes / Math.pow(k, i)).toFixed(2)) + " " + sizes[i];
  }

  function formatTimestamp(ts: any) {
    if (!ts) return "";
    // Handle proto timestamp objects {seconds, nanos}, unix seconds, ISO strings
    const relative = formatRelativeTime(ts, { neverLabel: "" });
    const full = formatLocalDateTime(ts, "");
    if (!relative) return "";
    return full ? `${relative} (${full})` : relative;
  }

  // Use in {#if} to guard display: returns empty string (falsy) for invalid timestamps
  function validTimestamp(ts: any): string {
    return formatTimestamp(ts);
  }

  function protocolsToServices(protocols: any[]) {
    return protocols.map((p) => {
      if (typeof p === "object") return p.name || p.service || p.port;
      return p;
    });
  }

  async function deleteGroupLink(linkId: string) {
    if (!confirm($_("topology.confirmDeleteLink"))) return;
    try {
      await wsStore.callGRPC("TenantPortalService", "DeleteTenantGroupLink", {
        id: linkId,
      });
      loadTopologyData();
    } catch (err) {
      console.error("Failed to delete group link:", err);
    }
  }

  $: selectionCount = selectedPeers.length;

  let layoutType = "grid";

  function applyLayout() {
    if (!simulation) return;

    // Release all fixed positions first
    d3Nodes.forEach((n) => {
      n.fx = null;
      n.fy = null;
    });

    if (layoutType === "cose") {
      // Use the same scaled forces as initSimulation so large graphs spread out
      const n = d3Nodes.length;
      simulation.force("charge", d3.forceManyBody().strength(scaledCharge(n)));
      simulation.force(
        "link",
        d3
          .forceLink(d3Links)
          .id((d: any) => d.id)
          .distance(scaledLinkDistance(d3Links)),
      );
      simulation.force("collide", d3.forceCollide(50).strength(0.85));
      simulation.alpha(0.3).restart();
    } else if (layoutType === "circle") {
      const radius = Math.min(containerWidth, containerHeight) * 0.4;
      d3Nodes.forEach((n, i) => {
        const angle = (i / d3Nodes.length) * 2 * Math.PI;
        n.fx = containerWidth / 2 + radius * Math.cos(angle);
        n.fy = containerHeight / 2 + radius * Math.sin(angle);
      });
      simulation.alpha(0.05).restart();
    } else if (layoutType === "grid") {
      const cols = Math.ceil(Math.sqrt(d3Nodes.length));
      // Spacing shrinks as node count grows so the grid stays compact:
      //   10 nodes → 110px | 50 → 100px | 100 → 90px | 300+ → 80px
      const spacing = Math.max(80, Math.min(110, 1200 / Math.sqrt(d3Nodes.length)));
      d3Nodes.forEach((n, i) => {
        n.fx = containerWidth / 2 - (cols * spacing) / 2 + (i % cols) * spacing;
        n.fy =
          containerHeight / 2 -
          (cols * spacing) / 2 +
          Math.floor(i / cols) * spacing;
      });
      simulation.alpha(0.05).restart();
    } else if (layoutType === "concentric") {
      const layers = 3;
      d3Nodes.forEach((n, i) => {
        const layer = i % layers;
        const radius = (layer + 1) * 120;
        const nodesInLayer = d3Nodes.filter(
          (_, idx) => idx % layers === layer,
        ).length;
        const nodeIdxInLayer = Math.floor(i / layers);
        const angle = (nodeIdxInLayer / nodesInLayer) * 2 * Math.PI;
        n.fx = containerWidth / 2 + radius * Math.cos(angle);
        n.fy = containerHeight / 2 + radius * Math.sin(angle);
      });
      simulation.alpha(0.05).restart();
    } else if (layoutType === "breadthfirst") {
      // Find root nodes (nodes with most outgoing edges or peer type)
      const roots = d3Nodes.filter(
        (n) => n.type === "router" || n.type === "global_server",
      );
      if (roots.length === 0) roots.push(d3Nodes[0]);

      const levels: Map<string, number> = new Map();
      const queue: any[] = roots.map((r) => ({ node: r, level: 0 }));
      roots.forEach((r) => levels.set(r.id, 0));

      while (queue.length > 0) {
        const { node, level } = queue.shift();
        d3Links.forEach((link) => {
          if (link.source.id === node.id && !levels.has(link.target.id)) {
            levels.set(link.target.id, level + 1);
            queue.push({ node: link.target, level: level + 1 });
          }
        });
      }

      const nodesByLevel: Record<number, any[]> = {};
      d3Nodes.forEach((n) => {
        const lvl = levels.get(n.id) || 0;
        if (!nodesByLevel[lvl]) nodesByLevel[lvl] = [];
        nodesByLevel[lvl].push(n);
      });

      const maxLevel = Math.max(...Object.keys(nodesByLevel).map(Number));
      Object.entries(nodesByLevel).forEach(([level, nodes]) => {
        const lvl = Number(level);
        const y = (containerHeight / (maxLevel + 1)) * (lvl + 0.5);
        nodes.forEach((n, i) => {
          n.fx = (containerWidth / (nodes.length + 1)) * (i + 1);
          n.fy = y;
        });
      });
      simulation.alpha(0.05).restart();
    }
  }

  async function assignExitNode() {
    if (selectedPeers.length === 0) return;
    const nodeId = selectedPeers[0].id;
    try {
      await wsStore.callGRPC("TenantPortalService", "AssignExitNode", {
        account_id: $accountStore.account?.id || "",
        exit_node_id: nodeId,
        entry_node_id: "",
      });
      topologyStore.refresh();
      alert(
        $_("topology.exitNodeAssigned", {
          default: "Exit node assigned successfully",
        }),
      );
    } catch (err) {
      console.error("Failed to assign exit node:", err);
      alert(
        $_("topology.assignExitNodeFailed", {
          default: "Failed to assign exit node",
        }),
      );
    }
  }

  function handleFit() {
    if (svgElement && zoomHandler && d3Nodes.length > 0) {
      const bounds = gElement.getBBox();
      const fullWidth = containerWidth;
      const fullHeight = containerHeight;
      const width = bounds.width;
      const height = bounds.height;
      const midX = bounds.x + width / 2;
      const midY = bounds.y + height / 2;

      if (width === 0 || height === 0) return;

      const scale = 0.8 / Math.max(width / fullWidth, height / fullHeight);
      const translate = [
        fullWidth / 2 - scale * midX,
        fullHeight / 2 - scale * midY,
      ];

      d3.select(svgElement)
        .transition()
        .duration(750)
        .call(
          zoomHandler.transform,
          d3.zoomIdentity.translate(translate[0], translate[1]).scale(scale),
        );
    }
  }

  let exitNodeStates: Record<string, boolean> = {};

  async function toggleExitNode(e: Event, id: string) {
    e.stopPropagation();
    const isCurrentlyExitNode = exitNodeStates[id] || false;
    try {
      await wsStore.callGRPC("TenantPortalService", "AssignExitNode", {
        account_id: $accountStore.account?.id || "",
        exit_node_id: !isCurrentlyExitNode ? id : "",
        entry_node_id: "",
      });
      exitNodeStates[id] = !isCurrentlyExitNode;
      topologyStore.refresh();
    } catch (err) {
      console.error("Failed to toggle exit node", err);
      exitNodeStates[id] = isCurrentlyExitNode;
      exitNodeStates = { ...exitNodeStates };
    }
  }

  function handleCenter() {
    if (svgElement && zoomHandler) {
      d3.select(svgElement)
        .transition()
        .duration(750)
        .call(zoomHandler.transform, d3.zoomIdentity);
    }
  }

  $: onlineNodes = data.nodes.filter(
    (n) => n.status === "online" || n.status === 1,
  ).length;
  $: showGroupLinkButton = selectedPeers.length >= 2;
</script>

<div
  class="topology activeShadow"
  class:maximized={isMaximized}
  class:minimized={isMinimized}
  style:z-index={zIndex}
  on:mousedown={handleFocus}
  on:touchstart={handleFocus}
  role="application"
  bind:this={windowElement}
  use:draggable={{
    handle: ".title-bar",
    disabled: isMaximized,
    bounds: "body",
  }}
  transition:scale={{ duration: 200 }}
>
  <Titlebar
    appName="Topology"
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
        <linearGradient id="topoGrad" x1="0%" y1="0%" x2="100%" y2="100%">
          <stop offset="0%" style="stop-color:#a78bfa;stop-opacity:1" />
          <stop offset="100%" style="stop-color:#7c3aed;stop-opacity:1" />
        </linearGradient>
      </defs>
      <circle cx="50" cy="20" r="10" fill="url(#topoGrad)" />
      <circle cx="25" cy="50" r="8" fill="url(#topoGrad)" opacity="0.8" />
      <circle cx="50" cy="50" r="8" fill="url(#topoGrad)" opacity="0.8" />
      <circle cx="75" cy="50" r="8" fill="url(#topoGrad)" opacity="0.8" />
      <line
        x1="50"
        y1="30"
        x2="25"
        y2="42"
        stroke="url(#topoGrad)"
        stroke-width="2"
      />
      <line
        x1="50"
        y1="30"
        x2="50"
        y2="42"
        stroke="url(#topoGrad)"
        stroke-width="2"
      />
      <line
        x1="50"
        y1="30"
        x2="75"
        y2="42"
        stroke="url(#topoGrad)"
        stroke-width="2"
      />
    </svg>
    <span class="appName pl-2">{$_("topology.networkManager")}</span>
  </Titlebar>

  <div class="mainApp">
    <!-- Toolbar -->
    <div class="toolbar">
      <div class="toolbar-section">
        <h2>{$_("topology.networkTopology")}</h2>
        <div class="stats">
          <span class="stat-item">
            <span class="stat-dot online"></span>
            {$_("topology.onlineCount", {
              values: { count: onlineNodes, total: nodeCount },
            })}
          </span>
          <span class="stat-divider">•</span>
          <span class="stat-item"
            >{$_("topology.connectionCount", {
              values: { count: edgeCount },
            })}</span
          >
          {#if selectionCount > 0}
            <span class="stat-divider">•</span>
            <span class="stat-item selection"
              >{$_("topology.selectedCount", {
                values: { count: selectionCount },
              })}</span
            >
          {/if}
        </div>
      </div>

      <div class="toolbar-controls">
        {#if showGroupLinkButton}
          <button
            class="control-btn accent"
            on:click={handleCreateGroupLink}
            title="Create Group Link ({selectionCount} peers selected)"
          >
            <svg
              width="16"
              height="16"
              viewBox="0 0 24 24"
              fill="none"
              stroke="currentColor"
              stroke-width="2"
            >
              <path
                d="M10 13a5 5 0 0 0 7.54.54l3-3a5 5 0 0 0-7.07-7.07l-1.72 1.71"
              />
              <path
                d="M14 11a5 5 0 0 0-7.54-.54l-3 3a5 5 0 0 0 7.07 7.07l1.71-1.71"
              />
            </svg>
          </button>
        {/if}

        <select
          bind:value={layoutType}
          on:change={applyLayout}
          class="layout-select"
        >
          <option value="concentric">{$_("topology.layoutConcentric")}</option>
          <option value="cose">Force-Directed</option>
          <option value="circle">{$_("topology.layoutCircle")}</option>
          <option value="grid">{$_("topology.layoutGrid")}</option>
          <option value="breadthfirst"
            >{$_("topology.layoutBreadthFirst")}</option
          >
        </select>

        <button
          class="control-btn"
          on:click={handleFit}
          title={$_("topology.fitView")}
        >
          <svg width="16" height="16" viewBox="0 0 16 16" fill="none">
            <rect
              x="2"
              y="2"
              width="12"
              height="12"
              stroke="currentColor"
              stroke-width="2"
              fill="none"
              rx="1"
            />
            <path
              d="M6 6L10 10M10 6L6 10"
              stroke="currentColor"
              stroke-width="2"
            />
          </svg>
        </button>
        <button
          class="control-btn"
          on:click={handleCenter}
          title={$_("topology.centerView")}
        >
          <svg width="16" height="16" viewBox="0 0 16 16" fill="none">
            <circle
              cx="8"
              cy="8"
              r="3"
              stroke="currentColor"
              stroke-width="2"
              fill="none"
            />
            <circle
              cx="8"
              cy="8"
              r="6"
              stroke="currentColor"
              stroke-width="1"
              fill="none"
              opacity="0.5"
            />
          </svg>
        </button>

        <button
          class="control-btn primary"
          on:click={handleRefresh}
          disabled={isLoading}
          title={$_("topology.refreshTopology")}
        >
          {#if isLoading}
            <svg
              width="16"
              height="16"
              viewBox="0 0 16 16"
              fill="none"
              class="spin"
            >
              <path
                d="M8 2 A6 6 0 0 1 14 8"
                stroke="currentColor"
                stroke-width="2"
                stroke-linecap="round"
              />
            </svg>
          {:else}
            <svg width="16" height="16" viewBox="0 0 16 16" fill="none">
              <path
                d="M13 3 L13 7 L9 7 M3 13 L3 9 L7 9"
                stroke="currentColor"
                stroke-width="2"
                stroke-linecap="round"
                stroke-linejoin="round"
              />
              <path
                d="M12.5 6.5 A5.5 5.5 0 1 1 8 2.5 M3.5 9.5 A5.5 5.5 0 1 0 8 13.5"
                stroke="currentColor"
                stroke-width="2"
                stroke-linecap="round"
              />
            </svg>
          {/if}
        </button>
      </div>
    </div>

    <div class="content">
      {#if error}
        <div class="error-message">
          <span class="error-icon"></span>
          <span>{$translateError$(error)}</span>
        </div>
      {/if}

      <div
        class="topology-container"
        bind:clientWidth={containerWidth}
        bind:clientHeight={containerHeight}
      >
        <svg
          bind:this={svgElement}
          width="100%"
          height="100%"
          style="background: #111;"
        >
          <defs>
            <pattern
              id="grid"
              width="40"
              height="40"
              patternUnits="userSpaceOnUse"
            >
              <path
                d="M 40 0 L 0 0 0 40"
                fill="none"
                stroke="#222"
                stroke-width="1"
              />
            </pattern>
            <marker
              id="arrow"
              viewBox="0 -5 10 10"
              refX="10"
              refY="0"
              markerWidth="6"
              markerHeight="6"
              orient="auto"
            >
              <path d="M0,-5L10,0L0,5" fill="#444" />
            </marker>
          </defs>
          <rect width="100%" height="100%" fill="url(#grid)" />

          <g bind:this={gElement}>
            <!-- Edges -->
            {#each d3Links as link}
              {@const coords = linkCoords(link)}
              <line
                x1={coords.x1}
                y1={coords.y1}
                x2={coords.x2}
                y2={coords.y2}
                stroke={link.status === "active" ? "#7c3aed" : "#444"}
                stroke-width="2"
                stroke-dasharray={link.status === "active" ? "0" : "5,5"}
                marker-end="url(#arrow)"
                on:click={() => handleEdgeClick(link)}
                class="edge"
              />
            {/each}

            <!-- Nodes -->
            {#each d3Nodes as node}
              <g
                transform="translate({node.x},{node.y})"
                on:click={(e) => handleNodeClick(e, node)}
                on:contextmenu={(e) => handleNodeContextMenu(e, node)}
                use:applyDrag={node}
                class="node-group"
                class:selected={selectedPeers.some((p) => p.id === node.id) || selectedNode?.id === node.id}
              >
                <circle
                  r="35"
                  fill="#222"
                  stroke={selectedPeers.some((p) => p.id === node.id) || selectedNode?.id === node.id
                    ? "#7c3aed"
                    : "transparent"}
                  stroke-width="2"
                />

                <image
                  href={getIconUrl(String(node.type), node.fingerprint)}
                  x="-20"
                  y="-25"
                  width="40"
                  height="40"
                />

                {#if node.status === "online" || node.status === 1}
                  <circle
                    cx="15"
                    cy="12"
                    r="6"
                    fill="#10b981"
                    stroke="#222"
                    stroke-width="2"
                  />
                {/if}

                <text
                  y="30"
                  text-anchor="middle"
                  fill="#eee"
                  font-size="12"
                  class="node-label"
                >
                  {node.label}
                </text>
                <text y="42" text-anchor="middle" fill="#888" font-size="10">
                  {node.ip || ""}
                </text>
              </g>
            {/each}
          </g>
        </svg>

        <!-- Legend Button -->
        <div class="legend-toggle">
          <Button on:click={() => (showLegendDialog = !showLegendDialog)}>
            {$_("topology.legend")}
          </Button>
        </div>
        {#if showLegendDialog}
          <div class="legend-panel">
            <div class="legend-header">
              <h3>{$_("topology.legend")}</h3>
              <button
                class="close-btn"
                on:click={() => (showLegendDialog = false)}>✕</button
              >
            </div>
            <div class="legend-content">
              <div class="legend-section">
                <h5>Node Types</h5>
                <div class="legend-grid">
                  <div class="legend-item">
                    <div class="legend-icon">
                      {@html '<svg viewBox="0 0 64 64" width="24" height="24"><polygon points="32,6 56,20 56,48 32,56 8,48 8,20" fill="#9b59b6"/><rect x="14" y="24" width="36" height="16" rx="2" fill="#fff"/><circle cx="22" cy="32" r="3" fill="#9b59b6"/></svg>'}
                    </div>
                    <div class="legend-text">
                      <span class="legend-label">{$_("topology.router")}</span>
                      <span class="legend-desc">Network router device</span>
                    </div>
                  </div>
                  <div class="legend-item">
                    <div class="legend-icon">
                      {@html '<svg viewBox="0 0 64 64" width="24" height="24"><polygon points="32,6 54,18 54,46 32,54 10,46 10,18" fill="#a569bd"/><circle cx="32" cy="24" r="8" fill="#fff"/><path d="M20,46 Q20,36 32,36 Q44,36 44,46" fill="#fff"/></svg>'}
                    </div>
                    <div class="legend-text">
                      <span class="legend-label">{$_("topology.device")}</span>
                      <span class="legend-desc">General network device</span>
                    </div>
                  </div>
                  <div class="legend-item">
                    <div class="legend-icon">
                      <img
                        src="/img/icon/generic/server.png"
                        width="24"
                        height="24"
                        alt="Server"
                      />
                    </div>
                    <div class="legend-text">
                      <span class="legend-label">Server</span>
                      <span class="legend-desc">Infrastructure server</span>
                    </div>
                  </div>
                  <div class="legend-item">
                    <div class="legend-icon">
                      <img
                        src="/img/icon/generic/global_server.png"
                        width="24"
                        height="24"
                        alt="Global Server"
                      />
                    </div>
                    <div class="legend-text">
                      <span class="legend-label">Global Server</span>
                      <span class="legend-desc">Central management server</span>
                    </div>
                  </div>
                  <div class="legend-item">
                    <div class="legend-icon">
                      <img
                        src="/img/icon/generic/peer.png"
                        width="24"
                        height="24"
                        alt="Peer"
                      />
                    </div>
                    <div class="legend-text">
                      <span class="legend-label">Peer</span>
                      <span class="legend-desc">Connected client/device</span>
                    </div>
                  </div>
                </div>
              </div>

              <div class="legend-divider" />

              <div class="legend-section">
                <h5>Connection Status</h5>
                <div class="legend-grid">
                  <div class="legend-item">
                    <div class="legend-line active" />
                    <div class="legend-text">
                      <span class="legend-label"
                        >{$_("topology.activeLink")}</span
                      >
                      <span class="legend-desc">Healthy connection</span>
                    </div>
                  </div>
                  <div class="legend-item">
                    <div class="legend-line inactive" />
                    <div class="legend-text">
                      <span class="legend-label"
                        >{$_("topology.inactiveLink")}</span
                      >
                      <span class="legend-desc">Offline or broken link</span>
                    </div>
                  </div>
                </div>
              </div>

              <div class="legend-divider" />

              <div class="legend-section">
                <h5>Keyboard Shortcuts</h5>
                <div class="shortcuts-grid">
                  <div class="shortcut-item">
                    <kbd>Shift</kbd> + <span class="action">Drag</span>
                    <span class="desc">Box selection</span>
                  </div>
                  <div class="shortcut-item">
                    <kbd>Scroll</kbd>
                    <span class="desc">Zoom in/out</span>
                  </div>
                  <div class="shortcut-item">
                    <kbd>Click</kbd> + <span class="action">Drag</span>
                    <span class="desc">Pan canvas</span>
                  </div>
                  <div class="shortcut-item">
                    <kbd>Tap</kbd> works also on touch devices
                  </div>
                </div>
              </div>
            </div>
          </div>
        {/if}

        <!-- Node Details Panel -->
        {#if showNodeDetails}
          {#if selectedPeers.length > 1}
            <div class="details-panel" transition:scale={{ duration: 200 }}>
              <div class="details-header">
                <h3>{$_("topology.selectedCount", { values: { count: selectedPeers.length } })}</h3>
                <button class="close-btn" on:click={hideDetails}>✕</button>
              </div>

              <div class="details-body">
                {#if selectedPeers.length > 1}
                  {@const allSelectedGroups = [...new Set(selectedPeers.flatMap(p => p['groups'] || []))]}
                  {@const relatedLinks = groupLinks.filter((link) => {
                    const sourceGroup = link.source_group_id || link.source_group_name || link.source_group;
                    const targetGroup = link.target_group_id || link.target_group_name || link.target_group;
                    return allSelectedGroups.includes(sourceGroup) || allSelectedGroups.includes(targetGroup);
                  })}

                  {#if allSelectedGroups.length > 0}
                    <div class="group-links-section">
                      <span class="detail-label">{$_("topology.groups")} Active in Selection</span>
                      <div class="groups-list">
                        {#each allSelectedGroups as group}
                          <span class="group-badge">{group}</span>
                        {/each}
                      </div>
                    </div>
                  {/if}

                  {#if relatedLinks.length > 0}
                    <div class="group-links-section">
                      <span class="detail-label">Related {$_("topology.groupLinks")}</span>
                      <div class="group-links-list">
                        {#each relatedLinks as link}
                          {@const sourceGroup = link.source_group_id || link.source_group_name || link.source_group}
                          {@const targetGroup = link.dest_group_id || link.target_group_id || link.target_group_name || link.target_group}
                          {@const linkId = link.id || link.link_id}
                          {@const services = protocolsToServices(link.protocols || [])}
                          <div class="group-link-item">
                            <div class="group-link-header">
                              {#if sourceGroup === targetGroup}
                                <span class="group-link-name">{$_("topology.meshLink", { values: { group: sourceGroup } })}</span>
                              {:else}
                              <span class="group-link-name">{$_("topology.directLink", { values: { source: sourceGroup, target: targetGroup } })}</span>
                            {/if}
                          </div>
                          <div class="group-link-services">
                            <strong>{$_("topology.services")}</strong> {services.join(", ")}
                          </div>
                          <div class="group-link-action allow">
                            ✓ {(link.action || "allow").toUpperCase()}
                          </div>
                          <button class="delete-link-btn" on:click={() => deleteGroupLink(linkId)}>
                            <svg width="12" height="12" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2">
                              <path d="M3 6h18M19 6v14a2 2 0 0 1-2 2H7a2 2 0 0 1-2-2V6m3 0V4a2 2 0 0 1 2-2h4a2 2 0 0 1 2 2v2" />
                            </svg>
                          </button>
                        </div>
                      {/each}
                    </div>
                  </div>
                {/if}
                {/if}
              </div>
            </div>
          {:else if selectedNode}
            <!-- PANEL-1: single node details (from multi-select context) -->
            <div class="details-panel" transition:scale={{ duration: 200 }}>
            <div class="details-header">
              <h3>{$_("topology.nodeDetailsTitle")}</h3>
              <button class="close-btn" on:click={hideDetails}>✕</button>
            </div>

            <div class="details-body">
              <div class="detail-item">
                <span class="detail-label">{$_("common.name")}</span>
                <span class="detail-value">{selectedNode.label}</span>
              </div>

              <div class="detail-item">
                <span class="detail-label">{$_("common.type")}</span>
                <span class="detail-value">
                  <span class="badge {selectedNode.type}"
                    >{selectedNode.type}</span
                  >
                </span>
              </div>

              <div class="detail-item">
                <span class="detail-label">{$_("common.status")}</span>
                <span class="detail-value">
                  <span class="badge {selectedNode.status}"
                    >{selectedNode.status}</span
                  >
                </span>
              </div>

              {#if selectedNode.ip}
                <div class="detail-item">
                  <span class="detail-label">{$_("topology.ipAddress")}</span>
                  <span class="detail-value mono">{selectedNode.ip}</span>
                </div>
              {/if}

              <!-- Protocol and Device capabilities -->
              {#if selectedNode.type === "peer"}
                <div class="detail-item">
                  <span class="detail-label">Protocol & Capabilities</span>
                  <span
                    class="detail-value"
                    style="display: flex; justify-content: space-between; align-items: center;"
                  >
                    {#if selectedNode.fingerprint && selectedNode.fingerprint.vendor === "Wantastic"}
                      <span>Wantasticd (P2P / Custom WireGuard)</span>
                      <button
                        class="control-btn accent"
                        title="Toggle Exit Node Mode"
                        style="padding: 4px 8px; background: linear-gradient(135deg, #10b981 0%, #059669 100%); border: none; color: white;"
                        on:click={(e) => {
                          e.stopPropagation();
                          alert(
                            "Toggle Exit Node functionality is ready for backend integration.",
                          );
                        }}
                      >
                        <svg
                          width="12"
                          height="12"
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
                      </button>
                    {:else}
                      <span>WireGuard</span>
                    {/if}
                  </span>
                </div>
              {/if}

              {#if selectedNode.rx_bytes !== undefined}
                <div class="detail-item">
                  <span class="detail-label">{$_("topology.downloaded")}</span>
                  <span class="detail-value"
                    >{formatBytes(selectedNode.rx_bytes)}</span
                  >
                </div>
              {/if}

              {#if selectedNode.tx_bytes !== undefined}
                <div class="detail-item">
                  <span class="detail-label">{$_("topology.uploaded")}</span>
                  <span class="detail-value"
                    >{formatBytes(selectedNode.tx_bytes)}</span
                  >
                </div>
              {/if}

              {#if selectedNode.last_handshake}
                <div class="detail-item">
                  <span class="detail-label"
                    >{$_("topology.lastHandshake")}</span
                  >
                  <span class="detail-value"
                    >{formatTimestamp(selectedNode.last_handshake)}</span
                  >
                </div>
              {/if}

              <!-- Groups Section for Peers -->
              {#if selectedNode.type === "peer" && selectedNode.groups && selectedNode.groups.length > 0}
                <div class="group-links-section">
                  <span class="detail-label">{$_("topology.groups")}</span>
                  <div class="groups-list">
                    {#each selectedNode.groups as group}
                      <span class="group-badge">{group}</span>
                    {/each}
                  </div>
                </div>
              {/if}

              <!-- Related Group Links for Peers -->
              {#if selectedNode.type === "peer"}
                {@const relatedLinks = groupLinks.filter((link) => {
                  const sourceGroup =
                    link.source_group_id ||
                    link.source_group_name ||
                    link.source_group;
                  const targetGroup =
                    link.target_group_id ||
                    link.target_group_id ||
                    link.target_group_name ||
                    link.target_group;
                  return (
                    selectedNode.groups?.includes(sourceGroup) ||
                    selectedNode.groups?.includes(targetGroup)
                  );
                })}
                {#if relatedLinks.length > 0}
                  <div class="group-links-section">
                    <span class="detail-label">{$_("topology.groupLinks")}</span
                    >
                    <div class="group-links-list">
                      {#each relatedLinks as link}
                        {@const sourceGroup =
                          link.source_group_id ||
                          link.source_group_name ||
                          link.source_group}
                        {@const targetGroup =
                          link.dest_group_id ||
                          link.target_group_id ||
                          link.target_group_name ||
                          link.target_group}
                        {@const linkId = link.id || link.link_id}
                        {@const services = protocolsToServices(
                          link.protocols || [],
                        )}
                        <div class="group-link-item">
                          <div class="group-link-header">
                            {#if sourceGroup === targetGroup}
                              <span class="group-link-name"
                                >{$_("topology.meshLink", {
                                  values: { group: sourceGroup },
                                })}</span
                              >
                            {:else}
                              <span class="group-link-name"
                                >{$_("topology.directLink", {
                                  values: {
                                    source: sourceGroup,
                                    target: targetGroup,
                                  },
                                })}</span
                              >
                            {/if}
                          </div>
                          <div class="group-link-services">
                            <strong>{$_("topology.services")}</strong>
                            {services.join(", ")}
                          </div>
                          <div class="group-link-action allow">
                            ✓ {(link.action || "allow").toUpperCase()}
                          </div>
                          <button
                            class="delete-link-btn"
                            on:click={() => deleteGroupLink(linkId)}
                          >
                            <svg
                              width="12"
                              height="12"
                              viewBox="0 0 24 24"
                              fill="none"
                              stroke="currentColor"
                              stroke-width="2"
                            >
                              <path
                                d="M3 6h18M19 6v14a2 2 0 0 1-2 2H7a2 2 0 0 1-2-2V6m3 0V4a2 2 0 0 1 2-2h4a2 2 0 0 1 2 2v2"
                              />
                            </svg>
                          </button>
                        </div>
                      {/each}
                    </div>
                  </div>
                {/if}
              {/if}

              <!-- Ping Results Section -->
              {#if pingLoading || pingResults}
                <div class="ping-results-section">
                  <span class="detail-label">Connection Test (Ping)</span>
                  <div class="ping-content">
                    {#if pingLoading}
                      <div class="ping-loading">
                        <svg
                          class="spin"
                          width="16"
                          height="16"
                          viewBox="0 0 16 16"
                        >
                          <path
                            d="M8 2 A6 6 0 0 1 14 8"
                            stroke="currentColor"
                            stroke-width="2"
                            fill="none"
                          />
                        </svg>
                        <span>Testing connection...</span>
                      </div>
                    {:else if pingResults?.error}
                      <div class="ping-error">{pingResults.error}</div>
                    {:else if pingResults}
                      <div class="ping-stats">
                        <div class="stat">
                          <span class="l">Sent</span>
                          <span class="v">{pingResults.packets_sent}</span>
                        </div>
                        <div class="stat">
                          <span class="l">Received</span>
                          <span class="v">{pingResults.packets_received}</span>
                        </div>
                        <div class="stat">
                          <span class="l">Loss</span>
                          <span class="v"
                            >{pingResults.packet_loss_percent != null ? pingResults.packet_loss_percent.toFixed(1) : '0'}%</span
                          >
                        </div>
                        <div class="stat">
                          <span class="l">Avg RTT</span>
                          <span class="v"
                            >{pingResults.avg_rtt_ms?.toFixed(1)}ms</span
                          >
                        </div>
                      </div>
                      {#if pingResults.pings && pingResults.pings.length > 0}
                        <div class="ping-list">
                          {#each pingResults.pings as p}
                            <div class="p-item" class:s={p.success}>
                              <span class="dot"></span>
                              <span class="t">{p.rtt_ms?.toFixed(1)}ms</span>
                            </div>
                          {/each}
                        </div>
                      {/if}
                    {/if}
                  </div>
                </div>
              {/if}
            </div>
          </div>
        {:else if selectedNode}
          <!-- PANEL-2: single node details (from single-select state) -->
          <div class="details-panel" transition:scale={{ duration: 200 }}>
            <div class="details-header">
              <h3>{$_("topology.nodeDetailsTitle")}</h3>
              <button class="close-btn" on:click={hideDetails}>✕</button>
            </div>

            <div class="details-body">
              <div class="detail-item">
                <span class="detail-label">{$_("common.name")}</span>
                <span class="detail-value">{selectedNode.label}</span>
              </div>

              <div class="detail-item">
                <span class="detail-label">{$_("common.type")}</span>
                <span class="detail-value">
                  <span class="badge {selectedNode.type}"
                    >{selectedNode.type}</span
                  >
                </span>
              </div>

              <div class="detail-item">
                <span class="detail-label">{$_("common.status")}</span>
                <span class="detail-value">
                  <span class="badge {selectedNode.status}"
                    >{selectedNode.status}</span
                  >
                </span>
              </div>

              {#if selectedNode.ip}
                <div class="detail-item">
                  <span class="detail-label">{$_("topology.ipAddress")}</span>
                  <span class="detail-value mono">{selectedNode.ip}</span>
                </div>
              {/if}

              <!-- Protocol and Device capabilities -->
              {#if selectedNode.type === "peer"}
                <div class="detail-item">
                  <span class="detail-label">Protocol & Capabilities</span>
                  <span
                    class="detail-value"
                    style="display: flex; justify-content: space-between; align-items: center;"
                  >
                    {#if selectedNode.fingerprint && selectedNode.fingerprint.vendor === "Wantastic"}
                      <span>Wantasticd (P2P / Custom WireGuard)</span>
                      <button
                        class="control-btn accent"
                        title="Toggle Exit Node Mode"
                        style="padding: 4px 8px; background: linear-gradient(135deg, #10b981 0%, #059669 100%); border: none; color: white;"
                        on:click={(e) => {
                          e.stopPropagation();
                          alert(
                            "Toggle Exit Node functionality is ready for backend integration.",
                          );
                        }}
                      >
                        <svg
                          width="12"
                          height="12"
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
                      </button>
                    {:else}
                      <span>WireGuard</span>
                    {/if}
                  </span>
                </div>
              {/if}

              {#if selectedNode.rx_bytes !== undefined}
                <div class="detail-item">
                  <span class="detail-label">{$_("topology.downloaded")}</span>
                  <span class="detail-value"
                    >{formatBytes(selectedNode.rx_bytes)}</span
                  >
                </div>
              {/if}

              {#if selectedNode.tx_bytes !== undefined}
                <div class="detail-item">
                  <span class="detail-label">{$_("topology.uploaded")}</span>
                  <span class="detail-value"
                    >{formatBytes(selectedNode.tx_bytes)}</span
                  >
                </div>
              {/if}

              {#if selectedNode.last_handshake}
                <div class="detail-item">
                  <span class="detail-label"
                    >{$_("topology.lastHandshake")}</span
                  >
                  <span class="detail-value"
                    >{formatTimestamp(selectedNode.last_handshake)}</span
                  >
                </div>
              {/if}

              <!-- Groups Section for Peers -->
              {#if selectedNode.type === "peer" && selectedNode.groups && selectedNode.groups.length > 0}
                <div class="group-links-section">
                  <span class="detail-label">{$_("topology.groups")}</span>
                  <div class="groups-list">
                    {#each selectedNode.groups as group}
                      <span class="group-badge">{group}</span>
                    {/each}
                  </div>
                </div>
              {/if}

              <!-- Related Group Links for Peers -->
              {#if selectedNode.type === "peer"}
                {@const relatedLinks = groupLinks.filter((link) => {
                  const sourceGroup =
                    link.source_group_id ||
                    link.source_group_name ||
                    link.source_group;
                  const targetGroup =
                    link.target_group_id ||
                    link.target_group_id ||
                    link.target_group_name ||
                    link.target_group;
                  return (
                    selectedNode.groups?.includes(sourceGroup) ||
                    selectedNode.groups?.includes(targetGroup)
                  );
                })}
                {#if relatedLinks.length > 0}
                  <div class="group-links-section">
                    <span class="detail-label">{$_("topology.groupLinks")}</span
                    >
                    <div class="group-links-list">
                      {#each relatedLinks as link}
                        {@const sourceGroup =
                          link.source_group_id ||
                          link.source_group_name ||
                          link.source_group}
                        {@const targetGroup =
                          link.dest_group_id ||
                          link.target_group_id ||
                          link.target_group_name ||
                          link.target_group}
                        {@const linkId = link.id || link.link_id}
                        {@const services = protocolsToServices(
                          link.protocols || [],
                        )}
                        <div class="group-link-item">
                          <div class="group-link-header">
                            {#if sourceGroup === targetGroup}
                              <span class="group-link-name"
                                >{$_("topology.meshLink", {
                                  values: { group: sourceGroup },
                                })}</span
                              >
                            {:else}
                              <span class="group-link-name"
                                >{$_("topology.directLink", {
                                  values: {
                                    source: sourceGroup,
                                    target: targetGroup,
                                  },
                                })}</span
                              >
                            {/if}
                          </div>
                          <div class="group-link-services">
                            <strong>{$_("topology.services")}</strong>
                            {services.join(", ")}
                          </div>
                          <div class="group-link-action allow">
                            ✓ {(link.action || "allow").toUpperCase()}
                          </div>
                          <button
                            class="delete-link-btn"
                            on:click={() => deleteGroupLink(linkId)}
                          >
                            <svg
                              width="12"
                              height="12"
                              viewBox="0 0 24 24"
                              fill="none"
                              stroke="currentColor"
                              stroke-width="2"
                            >
                              <path
                                d="M3 6h18M19 6v14a2 2 0 0 1-2 2H7a2 2 0 0 1-2-2V6m3 0V4a2 2 0 0 1 2-2h4a2 2 0 0 1 2 2v2"
                              />
                            </svg>
                          </button>
                        </div>
                      {/each}
                    </div>
                  </div>
                {/if}
              {/if}

              <!-- Ping Results Section -->
              {#if pingLoading || pingResults}
                <div class="ping-results-section">
                  <span class="detail-label">Connection Test (Ping)</span>
                  <div class="ping-content">
                    {#if pingLoading}
                      <div class="ping-loading">
                        <svg
                          class="spin"
                          width="16"
                          height="16"
                          viewBox="0 0 16 16"
                        >
                          <path
                            d="M8 2 A6 6 0 0 1 14 8"
                            stroke="currentColor"
                            stroke-width="2"
                            fill="none"
                          />
                        </svg>
                        <span>Testing connection...</span>
                      </div>
                    {:else if pingResults?.error}
                      <div class="ping-error">{pingResults.error}</div>
                    {:else if pingResults}
                      <div class="ping-stats">
                        <div class="stat">
                          <span class="l">Sent</span>
                          <span class="v">{pingResults.packets_sent}</span>
                        </div>
                        <div class="stat">
                          <span class="l">Received</span>
                          <span class="v">{pingResults.packets_received}</span>
                        </div>
                        <div class="stat">
                          <span class="l">Loss</span>
                          <span class="v"
                            >{pingResults.packet_loss_percent != null ? pingResults.packet_loss_percent.toFixed(1) : '0'}%</span
                          >
                        </div>
                        <div class="stat">
                          <span class="l">Avg RTT</span>
                          <span class="v"
                            >{pingResults.avg_rtt_ms?.toFixed(1)}ms</span
                          >
                        </div>
                      </div>
                      {#if pingResults.pings && pingResults.pings.length > 0}
                        <div class="ping-list">
                          {#each pingResults.pings as p}
                            <div class="p-item" class:s={p.success}>
                              <span class="dot"></span>
                              <span class="t">{p.rtt_ms?.toFixed(1)}ms</span>
                            </div>
                          {/each}
                        </div>
                      {/if}
                    {/if}
                  </div>
                </div>
              {/if}
            </div>
          </div>
          {/if}
        {/if}

        <!-- Edge Details Panel -->
        {#if showEdgeDetails && selectedEdge}
          <div class="details-panel" transition:scale={{ duration: 200 }}>
            <div class="details-header">
              <h3>{$_("topology.linkDetailsTitle")}</h3>
              <button class="close-btn" on:click={hideDetails}>✕</button>
            </div>

            <div class="details-body">
              <div class="detail-item">
                <span class="detail-label">{$_("topology.from")}</span>
                <span class="detail-value">{selectedEdge?.source?.label || selectedEdge?.source?.id || selectedEdge?.source_id || selectedEdge?.source}</span>
              </div>

              <div class="detail-item">
                <span class="detail-label">{$_("topology.to")}</span>
                <span class="detail-value">{selectedEdge?.target?.label || selectedEdge?.target?.id || selectedEdge?.target_id || selectedEdge?.target}</span>
              </div>

              <div class="detail-item">
                <span class="detail-label">{$_("common.type")}</span>
                <span class="detail-value">
                  <span class="badge">{selectedEdge.type || '-'}</span>
                </span>
              </div>

              <div class="detail-item">
                <span class="detail-label">{$_("common.status")}</span>
                <span class="detail-value">
                  <span class="badge {selectedEdge.status}"
                    >{selectedEdge.status}</span
                  >
                </span>
              </div>

              {#if selectedEdge.bandwidth}
                <div class="detail-item">
                  <span class="detail-label">{$_("topology.bandwidth")}</span>
                  <span class="detail-value">{selectedEdge.bandwidth} Mbps</span
                  >
                </div>
              {/if}

              {#if selectedEdge.latency_ms}
                <div class="detail-item">
                  <span class="detail-label">{$_("topology.latency")}</span>
                  <span class="detail-value">{selectedEdge.latency_ms} ms</span>
                </div>
              {/if}

              {#if selectedEdge.packet_loss}
                <div class="detail-item">
                  <span class="detail-label">{$_("topology.packetLoss")}</span>
                  <span class="detail-value">{selectedEdge.packet_loss}%</span>
                </div>
              {/if}

              <!-- Group Links Section -->
              {#if selectedEdge.type === "peer_to_peer" || selectedEdge.type === "rule"}
                <div class="group-links-section">
                  <span class="detail-label">{$_("topology.groupLinks")}</span>

                  {#if selectedEdge.group_links && selectedEdge.group_links.length > 0}
                    <div class="group-links-list">
                      {#each selectedEdge.group_links as link}
                        <div class="group-link-item">
                          <div class="group-link-header">
                            {#if link.source_group === link.target_group}
                              <span class="group-link-name"
                                >{$_("topology.meshLink", {
                                  values: { group: link.source_group },
                                })}</span
                              >
                            {:else}
                              <span class="group-link-name"
                                >{$_("topology.directLink", {
                                  values: {
                                    source: link.source_group,
                                    target: link.target_group,
                                  },
                                })}</span
                              >
                            {/if}
                          </div>

                          {#if link.services && link.services.length > 0}
                            <div class="group-link-services">
                              <strong>{$_("topology.services")}</strong>
                              {link.services.join(", ")}
                            </div>
                          {/if}

                          <div
                            class="group-link-action"
                            class:allow={link.action === "allow"}
                            class:deny={link.action === "deny"}
                          >
                            ✓ {(link.action || "allow").toUpperCase()}
                          </div>

                          <div class="group-link-id">
                            {link.link_id || link.id || "N/A"}
                          </div>

                          <button
                            class="delete-link-btn"
                            on:click={() =>
                              deleteGroupLink(link.link_id || link.id)}
                          >
                            <svg
                              width="14"
                              height="14"
                              viewBox="0 0 24 24"
                              fill="none"
                              xmlns="http://www.w3.org/2000/svg"
                            >
                              <path
                                d="M3 6H5H21"
                                stroke="currentColor"
                                stroke-width="2"
                                stroke-linecap="round"
                                stroke-linejoin="round"
                              />
                              <path
                                d="M8 6V4C8 3.46957 8.21071 3 8.58579 2.62513C8.96086 2.25026 9.46957 2.04061 10 2.04061H14C14.5304 2.04061 15.0391 2.25026 15.4142 2.62513C15.7893 3 16 3.46957 16 4V6M19 6V20C19 20.5304 18.7893 21 18.4142 21.3749C18.0391 21.7497 17.5304 21.9594 17 21.9594H7C6.46957 21.9594 5.96086 21.7497 5.58579 21.3749C5.21071 21 5 20.5304 5 20V6H19Z"
                                stroke="currentColor"
                                stroke-width="2"
                                stroke-linecap="round"
                                stroke-linejoin="round"
                              />
                            </svg>
                            {$_("common.delete")}
                          </button>
                        </div>
                      {/each}
                    </div>
                  {:else}
                    <div class="no-group-links">
                      {$_("topology.noGroupLinks")}
                    </div>
                  {/if}
                </div>
              {/if}
            </div>
          </div>
        {/if}
      </div>
    </div>

    <!-- Context Menu for Nodes — rendered via portal to escape parent transform/overflow -->
    {#if nodeMenuVisible}
      <div
        use:portal
        class="topology-context-menu"
        style="position:fixed; left:{menuX}px; top:{menuY}px; z-index:99999;"
        on:mousedown|stopPropagation
        on:click|stopPropagation
      >
      <MenuFlyout bind:open={nodeMenuVisible} placement="bottom" alignment="start">
        <svelte:fragment slot="flyout">
          <div class="menu-header">
            <img
              src={getIconUrl(String(menuNode?.type), menuNode?.fingerprint)}
              alt=""
              width="16"
              height="16"
            />
            <span>{menuNode?.label || menuNode?.id}</span>
          </div>
          <MenuFlyoutDivider />
          <MenuFlyoutItem
            on:click={() => {
              handleNodeClick(null, menuNode);
              nodeMenuVisible = false;
            }}
          >
            <svelte:fragment slot="icon">
              <svg
                width="14"
                height="14"
                viewBox="0 0 24 24"
                fill="none"
                stroke="currentColor"
                stroke-width="2"
              >
                <circle cx="11" cy="11" r="8" /><path d="m21 21-4.3-4.3" />
              </svg>
            </svelte:fragment>
            View Details
          </MenuFlyoutItem>
        {#if menuNode?.type === "peer"}
          <MenuFlyoutItem
            on:click={() => {
              handleWebSSHNode(menuNode);
              nodeMenuVisible = false;
            }}
          >
            <svelte:fragment slot="icon">
              <svg
                width="14"
                height="14"
                viewBox="0 0 24 24"
                fill="none"
                stroke="currentColor"
                stroke-width="2"
              >
                <rect x="2" y="3" width="20" height="14" rx="2" /><path
                  d="M2 7h20M6 21h12"
                />
              </svg>
            </svelte:fragment>
            WebSSH
          </MenuFlyoutItem>
        
          <MenuFlyoutItem
            on:click={() => {
              handlePingNode(menuNode);
              nodeMenuVisible = false;
            }}
          >
            <svelte:fragment slot="icon">
              <svg
                width="14"
                height="14"
                viewBox="0 0 24 24"
                fill="none"
                stroke="currentColor"
                stroke-width="2"
              >
                <polyline points="22 12 18 12 15 21 9 3 6 12 2 12" />
              </svg>
            </svelte:fragment>
            Ping
          </MenuFlyoutItem>
          {/if}
          <MenuFlyoutDivider />
          <MenuFlyoutItem
            on:click={() => {
              handleToggleSelection(menuNode);
              nodeMenuVisible = false;
            }}
          >
            <svelte:fragment slot="icon">
              {#if selectedPeers.some((p) => p.id === menuNode.id)}
                <svg
                  width="14"
                  height="14"
                  viewBox="0 0 24 24"
                  fill="none"
                  stroke="currentColor"
                  stroke-width="2"
                >
                  <polyline points="20 6 9 17 4 12" />
                </svg>
              {:else}
                <svg
                  width="14"
                  height="14"
                  viewBox="0 0 24 24"
                  fill="none"
                  stroke="currentColor"
                  stroke-width="2"
                >
                  <rect
                    x="3"
                    y="3"
                    width="18"
                    height="18"
                    rx="2"
                    stroke-dasharray="4"
                  />
                </svg>
              {/if}
            </svelte:fragment>
            {selectedPeers.some((p) => p.id === menuNode.id)
              ? "Deselect"
              : "Select for Group"}
          </MenuFlyoutItem>
          {#if isWantasticdPeer(menuNode)}
          <MenuFlyoutItem
            on:click={() => {
              // Add to selection for exit node assignment
              if (!selectedPeers.some((p) => p.id === menuNode.id)) {
                selectedPeers = [...selectedPeers, menuNode];
              }
              if (selectedPeers.length === 1) {
                // If only one, treat it as the exit node candidate
                topologyStore.setSelectedPeersForExitNode(selectedPeers);
                alert(
                  "Now select the entry node (right-click and select, or click Assign Exit Node)",
                );
              } else if (selectedPeers.length >= 2) {
                // If we have 2 or more, open the assignment dialog
                topologyStore.setSelectedPeersForExitNode(
                  selectedPeers.slice(0, 2),
                );
                if (!$openedApps.includes("AssignExitNode")) {
                  $openedApps = [...$openedApps, "AssignExitNode"];
                }
                $activeThing = "AssignExitNode";
                bringToFront("AssignExitNode");
              }
              nodeMenuVisible = false;
            }}
          >
            <svelte:fragment slot="icon">
              <svg
                width="14"
                height="14"
                viewBox="0 0 24 24"
                fill="none"
                stroke="currentColor"
                stroke-width="2"
              >
                <path d="M15 3h6v6M9 21H3v-6M21 3l-7 7M3 21l7-7" />
              </svg>
            </svelte:fragment>
            Assign Exit Node
          </MenuFlyoutItem>
          {/if}
        </svelte:fragment>
      </MenuFlyout>
      </div>
    {/if}
  </div>
</div>

<style lang="scss">
  /* Keyframes */
  @keyframes spin {
    from {
      transform: rotate(0deg);
    }
    to {
      transform: rotate(360deg);
    }
  }

  .spin {
    animation: spin 1s linear infinite;
  }

  .topology {
    background: var(--mica);
    position: absolute;
    top: 5%;
    left: 5%;
    border-radius: var(--radius-sm);
    overflow: hidden;
    resize: both;
    min-width: 1000px;
    min-height: 700px;
    width: 90%;
    height: 85%;
    box-shadow: var(--shadow-lg);
  }

  .topology.maximized {
    top: 0 !important;
    left: 0 !important;
    width: 100% !important;
    height: calc(100% - 48px) !important;
    border-radius: 0;
    resize: none;
  }

  .topology.minimized {
    display: none;
  }

  .mainApp {
    display: flex;
    flex-direction: column;
    padding: 0;
    position: absolute;
    inset: 36px 0 0;
  }

  /* Toolbar */
  .toolbar {
    display: flex;
    justify-content: space-between;
    align-items: center;
    padding: var(--sp-4) var(--sp-6);
    background: rgb(var(--bg2));
    border-bottom: 2px solid var(--border-color);
    flex-shrink: 0;
  }

  .toolbar-section h2 {
    margin: 0 0 6px 0;
    font-size: 20px;
    font-weight: 600;
  }

  .stats {
    display: flex;
    align-items: center;
    gap: 8px;
    font-size: 13px;
    color: rgb(var(--clr) / 66%);
  }

  .stat-item {
    display: flex;
    align-items: center;
    gap: 6px;
  }

  .stat-item.selection {
    color: rgb(var(--clrPrm));
    font-weight: 600;
  }

  .stat-dot {
    width: 6px;
    height: 6px;
    border-radius: 50%;
    background: #6b7280;
  }

  .stat-dot.online {
    background: #10b981;
    box-shadow: 0 0 6px #10b981;
  }

  .stat-dot.idle {
    background: #f59e0b;
    box-shadow: 0 0 6px #f59e0b;
  }

  .stat-dot.offline {
    background: #6b7280;
  }

  .stat-divider {
    opacity: 0.5;
  }

  .toolbar-controls {
    display: flex;
    align-items: center;
    gap: 8px;
  }

  .checkbox-label {
    display: flex;
    align-items: center;
    gap: var(--sp-2);
    padding: 6px var(--sp-3);
    background: rgb(var(--bg3));
    border: 1px solid var(--border-color);
    border-radius: var(--radius-xs);
    cursor: pointer;
    font-size: var(--text-sm);
    transition: var(--trans-normal);

    &:hover {
      background: rgb(var(--bg2));
      border-color: var(--border-color-hover);
    }

    input[type="checkbox"] {
      cursor: pointer;
    }
  }

  .layout-select {
    padding: 6px var(--sp-3);
    background: rgb(var(--bg3));
    border: 1px solid var(--border-color);
    border-radius: var(--radius-xs);
    color: rgb(var(--clr));
    font-size: var(--text-sm);
    cursor: pointer;
    transition: var(--trans-normal);

    &:focus {
      outline: none;
      border-color: var(--primary);
    }
  }

  .control-btn {
    padding: 6px 12px;
    background: rgb(var(--bg3));
    border: 1px solid rgb(var(--clr) / 20%);
    border-radius: 6px;
    color: rgb(var(--clr));
    cursor: pointer;
    font-size: 13px;
    font-weight: 500;
    transition: all 0.2s;
    display: flex;
    align-items: center;
    justify-content: center;
    gap: 6px;

    &:hover:not(:disabled) {
      background: rgb(var(--bg2));
      border-color: rgb(var(--clr) / 30%);
    }

    &:disabled {
      opacity: 0.5;
      cursor: not-allowed;
    }

    &.primary {
      background: rgb(var(--clrPrm));
      color: white;
      border-color: rgb(var(--clrPrm));

      &:hover:not(:disabled) {
        background: rgb(var(--clrPrm) / 90%);
      }
    }

    &.accent {
      background: #9d4edd;
      color: white;
      border-color: #9d4edd;

      &:hover:not(:disabled) {
        background: #8b3fd4;
      }
    }
  }

  /* Content */
  .content {
    flex: 1;
    overflow: hidden;
    position: relative;
  }

  .error-message {
    position: absolute;
    top: 16px;
    left: 16px;
    right: 16px;
    display: flex;
    align-items: center;
    gap: 12px;
    padding: 12px 16px;
    background: rgba(239, 68, 68, 0.1);
    border: 1px solid #ef4444;
    border-radius: 6px;
    color: #ef4444;
    z-index: 100;
    font-size: 14px;
  }

  .error-icon {
    font-size: 20px;
  }

  /* Topology Container */
  .topology-container {
    position: absolute;
    inset: 0;
  }

  .topology-container {
    width: 100%;
    height: 100%;
    background: rgb(var(--bg1));
    position: relative;
    border-radius: 0 0 var(--radius-sm) var(--radius-sm);
    overflow: hidden;
  }

  /* Legend Dialog */
  .legend-toggle {
    position: absolute;
    bottom: 20px;
    left: 20px;
    z-index: 10;
  }

  .context-menu {
    position: fixed;
    background: rgb(var(--bg1));
    border: 1px solid var(--border-color);
    border-radius: var(--radius-sm);
    padding: var(--sp-2);
    display: flex;
    flex-direction: column;
    gap: 4px;
    z-index: 1050;
    min-width: 200px;
  }

  .context-btn {
    padding: 8px 12px;
    text-align: left;
    background: transparent;
    border: none;
    border-radius: 4px;
    color: rgb(var(--clr));
    font-size: 13px;
    font-family: var(--font-main);
    cursor: pointer;
    transition: background 0.15s;

    &:hover {
      background: rgb(var(--bg3));
    }

    &.primary {
      color: #10b981;
      font-weight: 500;
    }
  }

  .legend-panel {
    position: absolute;
    bottom: 70px;
    left: 20px;
    background: var(--mica);
    border: 1px solid var(--border-color);
    border-radius: var(--radius-md);
    padding: 16px;
    z-index: 50;
    width: 350px;
    max-height: 60vh;
    display: flex;
    flex-direction: column;
    box-shadow: 0 10px 15px -3px rgb(0 0 0 / 0.2);
    backdrop-filter: blur(20px);
    border-top: 1px solid rgba(255, 255, 255, 0.1);
  }
  .legend-header {
    display: flex;
    justify-content: space-between;
    align-items: center;
    margin-bottom: 16px;
  }
  .legend-header h3 {
    margin: 0;
    font-size: 16px;
    font-weight: 600;
    color: rgb(var(--clr));
  }

  .legend-content {
    overflow-y: auto;
    display: flex;
    flex-direction: column;
    gap: 20px;
    padding-right: 8px; /* avoid scrollbar overlap */
    font-family: var(--font-main);
  }

  .legend-section h5 {
    font-size: 14px;
    font-weight: 600;
    margin: 0 0 12px 0;
    color: rgb(var(--clr));
    font-family: "Segoe UI Variable", sans-serif;
  }

  .legend-grid {
    display: grid;
    grid-template-columns: repeat(auto-fill, minmax(200px, 1fr));
    gap: 12px;
  }

  .legend-item {
    display: flex;
    align-items: center;
    gap: 12px;
    padding: 8px;
    background: rgb(var(--bg3));
    border-radius: 6px;
    border: 1px solid rgb(var(--clr) / 5%);
  }

  .legend-icon {
    width: 32px;
    height: 32px;
    display: flex;
    align-items: center;
    justify-content: center;
    background: rgb(var(--bg1));
    border-radius: 4px;
    padding: 4px;
  }

  .legend-text {
    display: flex;
    flex-direction: column;
    gap: 2px;
  }

  .legend-label {
    font-size: 13px;
    font-weight: 600;
    color: rgb(var(--clr));
  }

  .legend-desc {
    font-size: 11px;
    color: rgb(var(--clr) / 60%);
  }

  .legend-line {
    width: 32px;
    height: 4px;
    border-radius: 2px;
    margin: 14px 0; /* center visually in 32px height container */
  }

  .legend-line.active {
    background: #06b6d4;
  }

  .legend-line.inactive {
    background: #6b7280;
    opacity: 0.5;
    background-image: linear-gradient(
      90deg,
      transparent 0%,
      transparent 40%,
      #6b7280 40%,
      #6b7280 60%,
      transparent 60%,
      transparent 100%
    );
    background-size: 8px 3px;
  }

  .legend-divider {
    height: 1px;
    background: rgb(var(--clr) / 10%);
    width: 100%;
  }

  .shortcuts-grid {
    display: grid;
    grid-template-columns: 1fr 1fr;
    gap: 12px;
  }

  .shortcut-item {
    display: flex;
    align-items: center;
    gap: 6px;
    font-size: 12px;
    color: rgb(var(--clr) / 80%);
  }

  kbd {
    background: rgb(var(--bg3));
    border: 1px solid rgb(var(--clr) / 20%);
    border-radius: 4px;
    padding: 2px 6px;
    font-family: var(--font-mono);
    font-size: 11px;
    font-weight: 600;
    color: rgb(var(--clr));
    box-shadow: 0 1px 0 rgb(var(--clr) / 20%);
  }

  .action {
    font-weight: 600;
    color: rgb(var(--clr));
  }

  .desc {
    color: rgb(var(--clr) / 60%);
    margin-left: auto;
  }

  /* Details Panel */
  .details-panel {
    position: absolute;
    top: 20px;
    right: 20px;
    width: 320px;
    background: rgb(var(--bg2) / 98%);
    backdrop-filter: blur(20px);
    border: 1px solid rgb(var(--clr) / 10%);
    border-radius: 12px;
    box-shadow: 0 10px 25px rgba(0, 0, 0, 0.15);
    overflow: hidden;
    z-index: 10;
  }

  .details-header {
    display: flex;
    justify-content: space-between;
    align-items: center;
    padding: 16px 20px;
    border-bottom: 1px solid rgb(var(--clr) / 10%);
    background: rgb(var(--bg3) / 50%);
  }

  .details-header h3 {
    margin: 0;
    font-size: 16px;
    font-weight: 600;
  }

  .close-btn {
    padding: 4px 8px;
    background: transparent;
    border: none;
    color: rgb(var(--clr) / 66%);
    cursor: pointer;
    font-size: 18px;
    border-radius: 4px;
    transition: all 0.2s;

    &:hover {
      background: rgb(var(--clr) / 10%);
      color: rgb(var(--clr));
    }
  }

  .details-body {
    padding: 20px;
    display: flex;
    flex-direction: column;
    gap: 16px;
    max-height: 500px;
    overflow-y: auto;
  }

  .detail-item {
    display: flex;
    flex-direction: column;
    gap: 4px;
  }

  .detail-label {
    font-size: 11px;
    color: rgb(var(--clr) / 66%);
    font-weight: 500;
    text-transform: uppercase;
    letter-spacing: 0.5px;
  }

  .detail-value {
    font-size: 14px;
    font-weight: 600;

    &.mono {
      font-family: "Cascadia Code", "Fira Code", monospace;
      font-size: 13px;
    }
  }

  /* Group Links Section */
  .group-links-section {
    margin-top: 12px;
    padding-top: 12px;
    border-top: 1px solid rgb(var(--clr) / 10%);
  }

  .group-links-list {
    max-height: 200px;
    overflow-y: auto;
    margin-top: 8px;
  }

  .group-link-item {
    margin: 8px 0;
    padding: 10px;
    background: rgb(var(--bg3));
    border-left: 3px solid rgb(var(--clrPrm));
    border-radius: 4px;
  }

  .group-link-header {
    font-weight: 600;
    font-size: 12px;
    margin-bottom: 6px;
  }

  .group-link-name {
    color: rgb(var(--clr));
  }

  .group-link-services {
    color: rgb(var(--clr) / 66%);
    font-size: 10px;
    margin-bottom: 4px;
  }

  .group-link-action {
    font-size: 9px;
    font-weight: 600;
    margin-bottom: 4px;

    &.allow {
      color: #28a745;
    }

    &.deny {
      color: #dc3545;
    }
  }

  .group-link-id {
    color: rgb(var(--clr) / 40%);
    font-size: 9px;
    font-family: "Cascadia Code", monospace;
    word-break: break-all;
    margin-bottom: 6px;
  }

  .delete-link-btn {
    font-size: 10px;
    padding: 4px 8px;
    background: #dc3545;
    color: white;
    border: none;
    border-radius: 3px;
    cursor: pointer;
    transition: background 0.2s;

    &:hover {
      background: #c82333;
    }
  }

  .no-group-links {
    font-size: 11px;
    color: rgb(var(--clr) / 60%);
    font-style: italic;
    padding: 10px;
    margin-top: 8px;
    background: rgb(var(--bg3) / 50%);
    border-radius: 4px;
  }

  .groups-list {
    display: flex;
    flex-wrap: wrap;
    gap: 6px;
    margin-top: 8px;
  }

  .group-badge {
    padding: 4px var(--sp-3);
    background: rgb(var(--primary-rgb) / 15%);
    color: var(--primary);
    border-radius: var(--radius-full);
    font-size: var(--text-xs);
    font-weight: 500;
  }

  /* Node type specific badges */
  .badge.server,
  .badge.global_server {
    background: rgba(139, 92, 246, 0.15);
    color: #8b5cf6;
  }

  .badge.peer,
  .badge.router {
    background: rgb(var(--success-rgb) / 15%);
    color: var(--success);
  }

  .badge.tenant {
    background: rgb(var(--primary-rgb) / 15%);
    color: var(--primary);
  }

  .badge.internet {
    background: rgb(var(--warning-rgb) / 15%);
    color: var(--warning);
  }

  .badge.online {
    background: rgb(var(--success-rgb) / 15%);
    color: var(--success);
  }

  .badge.offline {
    background: rgba(107, 114, 128, 0.15);
    color: #6b7280;
  }

  .badge.idle {
    background: rgb(var(--warning-rgb) / 15%);
    color: var(--warning);
  }

  .badge.inactive,
  .badge.active {
    background: rgb(var(--info-rgb) / 15%);
    color: var(--info);
  }

  .info-box {
    background: rgb(var(--primary-rgb) / 10%);
    border: 1px solid rgb(var(--primary-rgb) / 30%);
    border-radius: var(--radius-sm);
    padding: var(--sp-4);
    margin-bottom: var(--sp-6);

    strong {
      display: block;
      margin-bottom: var(--sp-3);
      font-size: var(--text-base);
    }
  }

  .peer-list {
    max-height: 120px;
    overflow-y: auto;
    margin-bottom: var(--sp-3);
    padding: var(--sp-2);
    background: rgb(0 0 0 / 10%);
    border-radius: var(--radius-xs);
  }

  .peer-item {
    font-size: var(--text-xs);
    padding: var(--sp-1) 0;
    font-family: var(--font-mono);
  }

  .info-text {
    font-size: var(--text-xs);
    color: rgb(var(--clr) / 70%);
    margin: 0;
    font-style: italic;
  }

  .help-text {
    font-size: var(--text-xs);
    color: rgb(var(--clr) / 66%);
    margin: var(--sp-2) 0 var(--sp-4) 0;
  }

  .services-grid {
    display: grid;
    grid-template-columns: repeat(auto-fill, minmax(150px, 1fr));
    gap: var(--sp-3);
  }

  .service-card {
    background: rgb(var(--bg3));
    border: 2px solid var(--border-color);
    border-radius: var(--radius-sm);
    padding: var(--sp-3);
    cursor: pointer;
    transition: var(--trans-normal);
    display: flex;
    align-items: center;
    gap: var(--sp-3);

    &:hover {
      border-color: rgb(var(--primary-rgb) / 50%);
      background: rgb(var(--bg2));
    }

    input[type="checkbox"] {
      cursor: pointer;
    }

    &:has(input:checked) {
      border-color: var(--primary);
      background: rgb(var(--primary-rgb) / 10%);
    }
  }

  .service-content {
    display: flex;
    align-items: center;
    gap: var(--sp-2);
    flex: 1;
  }

  .service-icon {
    font-size: var(--text-xl);
  }

  .service-name {
    font-size: var(--text-sm);
    font-weight: 500;
  }

  /* Responsive Design - Mobile & Tablet */
  @media (max-width: 1024px) {
    .topology {
      min-width: 100%;
      min-height: 100%;
      width: 100% !important;
      height: calc(100% - 48px) !important;
      top: 0 !important;
      left: 0 !important;
      border-radius: 0;
    }

    .toolbar {
      flex-direction: column;
      gap: 12px;
      padding: 12px;
    }

    .toolbar-controls {
      flex-wrap: wrap;
      width: 100%;
    }

    .details-panel {
      width: 280px;
      max-height: 50vh;
    }

    .legend {
      bottom: 10px;
      left: 10px;
      font-size: 11px;
    }

    .modal {
      width: 95%;
      max-width: none;
    }

    .services-grid {
      grid-template-columns: repeat(auto-fill, minmax(120px, 1fr));
    }
  }

  @media (max-width: 768px) {
    .toolbar {
      padding: 8px;
    }

    .toolbar-section h2 {
      font-size: 16px;
    }

    .stats {
      flex-wrap: wrap;
      font-size: 11px;
      pointer-events: none;
    }

    .control-btn,
    .layout-select,
    .checkbox-label {
      font-size: 12px;
      padding: 5px 10px;
    }

    .details-panel {
      width: 100%;
      max-width: 280px;
      right: 10px;
      top: 10px;
    }

    .details-body {
      max-height: 300px;
    }

    .legend {
      font-size: 10px;
      padding: 12px;
    }

    .legend-icon {
      width: 18px;
      height: 18px;
    }

    .legend-icon svg {
      width: 18px;
      height: 18px;
    }

    .modal-body {
      padding: 16px;
    }

    .services-grid {
      grid-template-columns: repeat(2, 1fr);
      gap: 8px;
    }

    .service-card {
      padding: 8px;
    }

    .service-icon {
      font-size: 16px;
    }

    .service-name {
      font-size: 11px;
    }

    .modal-footer {
      padding: 12px 16px;
    }
  }

  @media (max-width: 480px) {
    .topology {
      min-width: 320px;
    }

    .toolbar {
      padding: 6px;
    }

    .toolbar-section h2 {
      font-size: 14px;
      margin-bottom: 4px;
    }

    .stats {
      font-size: 10px;
      gap: 6px;
    }

    .toolbar-controls {
      gap: 6px;
    }

    .control-btn,
    .layout-select {
      font-size: 11px;
      padding: 4px 8px;
    }

    .details-panel {
      width: calc(100% - 20px);
      max-width: 100%;
      right: 10px;
      left: 10px;
      top: auto;
      bottom: 10px;
    }

    .details-body {
      max-height: 200px;
    }

    .legend {
      display: none; /* Hide legend on very small screens */
    }

    .modal {
      width: 100%;
      height: 100%;
      max-height: 100vh;
      border-radius: 0;
    }

    .services-grid {
      grid-template-columns: 1fr;
    }
  }

  /* Touch-friendly improvements */
  @media (hover: none) and (pointer: coarse) {
    .control-btn,
    .close-btn,
    .service-card,
    .btn {
      min-height: 44px; /* Apple's recommended touch target size */
      min-width: 44px;
    }

    .checkbox-label {
      padding: 10px 14px;
    }

    .layout-select {
      min-height: 44px;
      padding: 10px 14px;
    }
  }

  /* Topology Node Styling */
  .top-anchor {
    position: absolute;
    top: 0;
    left: 50%;
    transform: translateX(-50%);
  }
  .bottom-anchor {
    position: absolute;
    bottom: 0;
    left: 50%;
    transform: translateX(-50%);
  }
  .left-anchor {
    position: absolute;
    left: 0;
    top: 50%;
    transform: translateY(-50%);
  }
  .right-anchor {
    position: absolute;
    right: 0;
    top: 50%;
    transform: translateY(-50%);
  }

  .node-container {
    display: flex;
    flex-direction: column;
    align-items: center;
    justify-content: center;
    padding: 8px;
    background: var(--mica);
    border-radius: 12px;
    border: 1px solid var(--border-color);
    transition: all 0.2s;
    box-shadow: 0 4px 6px -1px rgb(0 0 0 / 0.1);
    width: 80px;
    height: 100px;
    position: relative;
    z-index: 10;
  }
  .node-container.selected {
    border-color: #3b82f6;
    box-shadow: 0 0 0 4px rgba(59, 130, 246, 0.2);
  }
  .icon-wrapper {
    position: relative;
    width: 48px;
    height: 48px;
    display: flex;
    align-items: center;
    justify-content: center;
    background: #f8fafc;
    border-radius: 50%;
    margin-bottom: 4px;
    border: 2px solid transparent;
  }
  .icon-wrapper.online {
    background: rgb(var(--bg1) / 50%);
    border-color: var(--accent-fill-rest);
  }
  .node-icon {
    width: 32px;
    height: 32px;
    object-fit: contain;
  }
  .status-indicator {
    position: absolute;
    bottom: 0;
    right: 0;
    width: 12px;
    height: 12px;
    border-radius: 50%;
    border: 2px solid white;
  }
  .status-indicator.online {
    background: #22c55e;
  }
  .label-container {
    display: flex;
    flex-direction: column;
    align-items: center;
    width: 100%;
  }
  .node-label {
    font-size: 10px;
    font-weight: 600;
    color: rgb(var(--clr));
    text-align: center;
    width: 100%;
  }
  .node-ip {
    font-size: 8px;
    color: #64748b;
  }
  .node-controls {
    margin-top: 4px;
  }
  .switch {
    position: relative;
    display: inline-block;
    width: 24px;
    height: 14px;
  }
  .switch input {
    opacity: 0;
    width: 0;
    height: 0;
  }
  .slider {
    position: absolute;
    cursor: pointer;
    top: 0;
    left: 0;
    right: 0;
    bottom: 0;
    background-color: #ccc;
    transition: 0.4s;
    border-radius: 14px;
  }
  .slider:before {
    position: absolute;
    content: "";
    height: 10px;
    width: 10px;
    left: 2px;
    bottom: 2px;
    background-color: white;
    transition: 0.4s;
    border-radius: 50%;
  }
  input:checked + .slider {
    background-color: #2196f3;
  }
  input:checked + .slider:before {
    transform: translateX(10px);
  }
  /* Context Menu Enhancements */
  .menu-header {
    padding: 8px 12px;
    display: flex;
    align-items: center;
    gap: 8px;
    font-size: 11px;
    font-weight: 600;
    color: rgb(var(--clr) / 50%);
    text-transform: uppercase;
    letter-spacing: 0.5px;
  }

  /* Ping Results Styling */
  .ping-results-section {
    margin-top: 16px;
    padding-top: 16px;
    border-top: 1px dashed rgb(var(--clr) / 20%);
  }

  .ping-content {
    margin-top: 10px;
    background: rgb(var(--bg3) / 50%);
    border-radius: 8px;
    padding: 12px;
  }

  .ping-loading {
    display: flex;
    align-items: center;
    gap: 10px;
    font-size: 13px;
    color: rgb(var(--clr) / 70%);
  }

  .ping-stats {
    display: grid;
    grid-template-columns: repeat(2, 1fr);
    gap: 12px;
    margin-bottom: 12px;
  }

  .stat {
    display: flex;
    flex-direction: column;
    gap: 2px;

    .l {
      font-size: 10px;
      color: rgb(var(--clr) / 50%);
      text-transform: uppercase;
    }

    .v {
      font-size: 14px;
      font-weight: 700;
      color: rgb(var(--clr));
    }
  }

  .ping-list {
    display: flex;
    flex-wrap: wrap;
    gap: 6px;
  }

  .p-item {
    display: flex;
    align-items: center;
    gap: 4px;
    padding: 2px 6px;
    background: rgb(var(--bg1));
    border-radius: 4px;
    font-size: 10px;
    font-weight: 600;
    border: 1px solid transparent;

    &.s {
      border-color: rgba(16, 185, 129, 0.2);
      .dot {
        background: #10b981;
      }
    }

    &:not(.s) {
      border-color: rgba(239, 68, 68, 0.2);
      .dot {
        background: #ef4444;
      }
    }
  }

  .dot {
    width: 6px;
    height: 6px;
    border-radius: 50%;
  }

  .ping-error {
    color: #ef4444;
    font-size: 12px;
    padding: 4px 0;
  }
</style>
