<script lang="ts">
  import { onDestroy } from "svelte";
  import AppWindow from "$components/AppWindow.svelte";
  import { peerStore, protoToDate, type Peer } from "$store/peer";
  import {
    routerOSStore,
    type RouterOSRecord,
    type RouterOSResourceKey,
  } from "$store/routeros";
  import { isMobile } from "$store/ui";
  import {
    ArchiveIcon,
    DatabaseIcon,
    FolderIcon,
    GitBranchIcon,
    HomeIcon,
    MenuIcon,
    NetworkIcon,
    PencilIcon,
    PlusIcon,
    RadioIcon,
    RefreshCcwIcon,
    SearchIcon,
    ServerIcon,
    ShieldIcon,
    Trash2Icon,
    WrenchIcon,
    XIcon,
  } from "$components/icons";

  const APP_NAME = "RouterOSDashboard";

  type DashboardSection = "overview" | RouterOSResourceKey;
  type EditorMode = "add" | "edit" | null;

  interface SectionMeta {
    key: DashboardSection;
    label: string;
    short: string;
    description: string;
    icon: any;
  }

  interface EditorRow {
    uid: string;
    key: string;
    value: string;
  }

  interface ScopeNode {
    key: string;
    label: string;
    hint?: string;
    count: number;
    depth: number;
  }

  type EditorInputType = "text" | "number" | "password" | "textarea";
  type EditorOptionSource = "interfaces" | "bridges" | "wireless_profiles";

  interface EditorFieldDefinition {
    key: string;
    input?: EditorInputType;
    options?: string[];
    optionSource?: EditorOptionSource;
    placeholder?: string;
    hint?: string;
  }

  const sectionMeta: SectionMeta[] = [
    {
      key: "overview",
      label: "Overview",
      short: "OV",
      description: "Identity, access state, and live RouterOS readiness.",
      icon: HomeIcon,
    },
    {
      key: "addresses",
      label: "IP Addresses",
      short: "IP",
      description: "Assigned interface addresses and loopback entries.",
      icon: NetworkIcon,
    },
    {
      key: "routes",
      label: "Routes",
      short: "RT",
      description: "Static and learned routing records.",
      icon: GitBranchIcon,
    },
    {
      key: "firewall",
      label: "Firewall",
      short: "FW",
      description: "Filter, NAT, and mangle style rules.",
      icon: ShieldIcon,
    },
    {
      key: "packages",
      label: "Packages",
      short: "PK",
      description: "Installed RouterOS packages and versions.",
      icon: ArchiveIcon,
    },
    {
      key: "files",
      label: "Files",
      short: "FI",
      description: "File store contents available on the device.",
      icon: FolderIcon,
    },
    {
      key: "wireless",
      label: "Wireless",
      short: "WL",
      description: "Wireless radios, interfaces, and profiles.",
      icon: RadioIcon,
    },
    {
      key: "tr069",
      label: "TR-069",
      short: "TR",
      description: "ACS client settings and operator handoff state.",
      icon: DatabaseIcon,
    },
    {
      key: "bridge",
      label: "Bridge",
      short: "BR",
      description: "Bridge domains, ports, and L2 service settings.",
      icon: WrenchIcon,
    },
  ];

  const resourceFieldTemplates: Record<RouterOSResourceKey, string[]> = {
    addresses: ["address", "interface", "network", "comment"],
    routes: ["dst-address", "gateway", "distance", "comment"],
    firewall: ["chain", "action", "src-address", "dst-address", "comment"],
    packages: ["name", "version", "build-time", "disabled"],
    files: ["__name_only", "type", "size", "creation-time"],
    wireless: ["name", "interface", "ssid", "band"],
    tr069: ["enabled", "acs-url", "username", "password"],
    bridge: ["name", "interface", "bridge", "vlan-ids", "comment"],
  };

  const preferredFieldOrder = [
    "__name_only",
    "name",
    "interface",
    "address",
    "network",
    "dst-address",
    "gateway",
    "action",
    "chain",
    "ssid",
    "band",
    "version",
    "type",
    "size",
    "comment",
    "disabled",
  ];

  const resourceEditorSchemas: Partial<
    Record<RouterOSResourceKey, EditorFieldDefinition[]>
  > = {
    addresses: [
      { key: "address", placeholder: "10.0.0.1/24" },
      { key: "interface", optionSource: "interfaces" },
      { key: "network", placeholder: "10.0.0.0" },
      { key: "disabled", options: ["true", "false"] },
      {
        key: "comment",
        input: "textarea",
        placeholder: "Optional note for operators",
      },
    ],
    routes: [
      { key: "dst-address", placeholder: "0.0.0.0/0" },
      {
        key: "gateway",
        optionSource: "interfaces",
        placeholder: "192.168.88.1 or interface name",
      },
      { key: "distance", input: "number", placeholder: "1" },
      { key: "routing-table", placeholder: "main" },
      { key: "pref-src", placeholder: "10.0.0.1" },
      { key: "check-gateway", options: ["none", "arp", "ping", "bfd"] },
      { key: "disabled", options: ["true", "false"] },
      { key: "comment", input: "textarea" },
    ],
    firewall: [
      {
        key: "chain",
        options: [
          "input",
          "forward",
          "output",
          "prerouting",
          "postrouting",
          "srcnat",
          "dstnat",
        ],
      },
      {
        key: "action",
        options: [
          "accept",
          "drop",
          "reject",
          "return",
          "passthrough",
          "log",
          "src-nat",
          "dst-nat",
          "masquerade",
          "redirect",
          "fasttrack-connection",
        ],
      },
      {
        key: "protocol",
        options: ["tcp", "udp", "icmp", "gre", "sctp", "ipsec-esp", "ipsec-ah"],
      },
      { key: "src-address", placeholder: "10.0.0.0/24" },
      { key: "dst-address", placeholder: "0.0.0.0/0" },
      { key: "in-interface", optionSource: "interfaces" },
      { key: "out-interface", optionSource: "interfaces" },
      { key: "src-port", placeholder: "80,443" },
      { key: "dst-port", placeholder: "80,443" },
      { key: "disabled", options: ["true", "false"] },
      { key: "comment", input: "textarea" },
    ],
    wireless: [
      { key: "name", placeholder: "wlan1" },
      { key: "ssid", placeholder: "Wantastic" },
      {
        key: "band",
        options: [
          "2ghz-b/g/n",
          "2ghz-onlyn",
          "5ghz-a/n/ac",
          "5ghz-onlyac",
          "5ghz-ax",
          "2ghz-ax",
        ],
      },
      {
        key: "mode",
        options: ["ap-bridge", "bridge", "station", "station-bridge"],
      },
      { key: "security-profile", optionSource: "wireless_profiles" },
      { key: "disabled", options: ["true", "false"] },
      { key: "comment", input: "textarea" },
    ],
    tr069: [
      { key: "enabled", options: ["true", "false"] },
      { key: "acs-url", placeholder: "https://acs.example.com/cwmp" },
      { key: "username", placeholder: "tr069-user" },
      { key: "password", input: "password", placeholder: "ACS password" },
      { key: "periodic-inform-enable", options: ["true", "false"] },
      { key: "periodic-inform-interval", input: "number", placeholder: "3600" },
      { key: "comment", input: "textarea" },
    ],
    bridge: [
      { key: "name", placeholder: "bridge1" },
      { key: "comment", input: "textarea" },
    ],
  };

  const scopedEditorSchemas: Record<string, EditorFieldDefinition[]> = {
    "bridge:bridge": [
      { key: "name", placeholder: "bridge1" },
      {
        key: "protocol-mode",
        options: ["rstp", "mstp", "stp", "none"],
      },
      {
        key: "arp",
        options: [
          "enabled",
          "disabled",
          "proxy-arp",
          "reply-only",
          "local-proxy-arp",
        ],
      },
      { key: "vlan-filtering", options: ["true", "false"] },
      { key: "fast-forward", options: ["true", "false"] },
      { key: "igmp-snooping", options: ["true", "false"] },
      { key: "comment", input: "textarea" },
    ],
    "bridge:port": [
      { key: "bridge", optionSource: "bridges" },
      { key: "interface", optionSource: "interfaces" },
      { key: "pvid", input: "number", placeholder: "1" },
      { key: "edge", options: ["auto", "yes", "no"] },
      { key: "trusted", options: ["true", "false"] },
      { key: "comment", input: "textarea" },
    ],
    "bridge:vlan": [
      { key: "bridge", optionSource: "bridges" },
      { key: "vlan-ids", placeholder: "10,20,30" },
      { key: "tagged", placeholder: "bridge1,ether2" },
      { key: "untagged", placeholder: "ether3,ether4" },
      { key: "comment", input: "textarea" },
    ],
    "wireless:interface": [
      { key: "name", placeholder: "wlan1" },
      { key: "ssid", placeholder: "Wantastic" },
      {
        key: "band",
        options: [
          "2ghz-b/g/n",
          "2ghz-onlyn",
          "5ghz-a/n/ac",
          "5ghz-onlyac",
          "5ghz-ax",
          "2ghz-ax",
        ],
      },
      {
        key: "mode",
        options: ["ap-bridge", "bridge", "station", "station-bridge"],
      },
      { key: "security-profile", optionSource: "wireless_profiles" },
      { key: "disabled", options: ["true", "false"] },
      { key: "comment", input: "textarea" },
    ],
    "wireless:security_profile": [
      { key: "name", placeholder: "default" },
      { key: "mode", options: ["dynamic-keys", "static-keys", "none"] },
      {
        key: "authentication-types",
        placeholder: "wpa2-psk,wpa3-psk",
      },
      {
        key: "wpa2-pre-shared-key",
        input: "password",
        placeholder: "WiFi password",
      },
      {
        key: "wpa-pre-shared-key",
        input: "password",
        placeholder: "Legacy WiFi password",
      },
      { key: "supplicant-identity", placeholder: "MikroTik" },
      { key: "comment", input: "textarea" },
    ],
    "wireless:configuration": [
      { key: "name", placeholder: "corp-5g" },
      { key: "ssid", placeholder: "Wantastic" },
      {
        key: "country",
        placeholder: "morocco",
      },
      {
        key: "mode",
        options: ["ap", "station", "bridge", "station-bridge"],
      },
      { key: "security", optionSource: "wireless_profiles" },
      { key: "disabled", options: ["true", "false"] },
      { key: "comment", input: "textarea" },
    ],
  };

  const tableSkeletonRows = Array.from({ length: 8 }, (_, index) => index);

  let activeSection: DashboardSection = "overview";
  let isNavOpen = false;
  let sidebarCollapsed = false;
  let searchQuery = "";
  let selectedRecordId = "";
  let editorMode: EditorMode = null;
  let editorRows: EditorRow[] = [];
  let editorMeta: Record<string, string> = {};
  let editorError = "";
  let showDeleteConfirm = false;
  let deleteTargetId = "";
  let deleteTargetLabel = "";
  let pendingMutation: "configure_access" | "add" | "update" | "delete" | null =
    null;
  let lastHandledNotice = "";
  let openedPeerId = "";
  let rowCounter = 0;

  let setupUsername = "admin";
  let setupPassword = "";
  let setupPort = 8728;
  let setupUseTls = false;
  let usernameTouched = false;
  let portTouched = false;
  let tlsTouched = false;
  let isMobileView = false;
  let activeScopeKey = "";
  let autoUsedSavedWinboxForPeer = "";

  $: selectedPeer = $peerStore.selectedPeer as Peer | null;
  $: isMobileView = $isMobile;
  $: peerId = selectedPeer?.id || selectedPeer?.public_key || "";
  $: peerName = selectedPeer?.name || "RouterOS";
  $: shellTitle = selectedPeer?.fingerprint?.vendor
    ? `${selectedPeer.fingerprint.vendor} / RouterOS`
    : "MikroTik / RouterOS";
  $: state = $routerOSStore;
  $: overview = state.overview;
  $: capability = overview?.capability || {};
  $: identity = overview?.identity || {};
  // Reactive count/pending maps. Pre-computing these as $: blocks makes
  // Svelte's compiler track `state.counts` / `state.records` /
  // `state.sectionLoading` as direct dependencies. Without this, the
  // sidebar (and any other place that calls sectionCount() as a function
  // expression) does not re-render when the store updates, because
  // Svelte cannot see the closure read of `state` from the function body.
  $: sectionCountMap = (() => {
    const map: Partial<Record<DashboardSection, number>> = { overview: 0 };
    for (const meta of sectionMeta) {
      if (meta.key === "overview") continue;
      const cached = state.counts[meta.key as RouterOSResourceKey];
      map[meta.key] =
        typeof cached === "number"
          ? cached
          : state.records[meta.key as RouterOSResourceKey]?.length || 0;
    }
    return map;
  })();
  $: sectionPendingMap = (() => {
    const map: Partial<Record<DashboardSection, boolean>> = { overview: false };
    for (const meta of sectionMeta) {
      if (meta.key === "overview") continue;
      const key = meta.key as RouterOSResourceKey;
      map[meta.key] =
        state.counts[key] === null && !!state.sectionLoading[key];
    }
    return map;
  })();
  $: currentSectionMeta =
    sectionMeta.find((section) => section.key === activeSection) || sectionMeta[0];
  $: currentResource =
    activeSection === "overview" ? null : (activeSection as RouterOSResourceKey);
  $: resourceRecords = currentResource ? state.records[currentResource] || [] : [];
  $: scopeNodes = currentResource
    ? buildSectionScopes(currentResource, resourceRecords)
    : [];
  $: if (
    currentResource &&
    scopeNodes.length > 0 &&
    !scopeNodes.some((node) => node.key === activeScopeKey)
  ) {
    activeScopeKey = scopeNodes[0].key;
  }
  $: scopedRecords =
    currentResource && activeScopeKey
      ? applySectionScope(currentResource, resourceRecords, activeScopeKey)
      : resourceRecords;
  $: filteredRecords = filterRecords(scopedRecords, searchQuery);
  $: visibleColumns = currentResource
    ? pickColumns(
        currentResource,
        filteredRecords.length ? filteredRecords : scopedRecords,
      )
    : [];
  $: selectedRecord =
    filteredRecords.find((record) => record.id === selectedRecordId) ||
    scopedRecords.find((record) => record.id === selectedRecordId) ||
    null;
  $: recordEntries = selectedRecord ? sortFieldEntries(selectedRecord.fields) : [];
  $: resourceError = currentResource ? state.resourceErrors[currentResource] : null;
  $: resourceLoading =
    !!currentResource &&
    (!!state.sectionLoading[currentResource] || state.isLoading);
  $: accessReady = !!capability.api_ready;
  $: accessStatusLabel = accessReady
    ? "API Ready"
    : capability.candidate
      ? "Setup Required"
      : "Not detected";
  $: accessSource = capability.credential_source || "none";
  $: apiEndpoint = capability.api_port
    ? `${capability.api_port}/${capability.api_tls ? "TLS" : "TCP"}`
    : "Auto";
  $: lastChecked = formatTimestamp(capability.last_validated);
  $: systemPairs = topPairs(overview?.system_resource || {}, 8);
  $: routerboardPairs = topPairs(overview?.routerboard || {}, 8);
  $: identityPairs = compactIdentityPairs(identity);
  $: peerRxBytes = Number(
    selectedPeer?.transfer_rx ?? selectedPeer?.rx_bytes ?? 0,
  );
  $: peerTxBytes = Number(
    selectedPeer?.transfer_tx ?? selectedPeer?.tx_bytes ?? 0,
  );
  $: overlayTrafficTotal = peerRxBytes + peerTxBytes;
  $: peerOnline = !!selectedPeer?.is_online;
  $: peerLastSeen =
    selectedPeer?.last_seen_at || selectedPeer?.last_handshake || "";
  $: cpuLoad = parseIntValue(overview?.system_resource?.["cpu-load"]);
  $: totalMemory = parseIntValue(overview?.system_resource?.["total-memory"]);
  $: freeMemory = parseIntValue(overview?.system_resource?.["free-memory"]);
  $: memoryUsagePercent =
    totalMemory > 0 && freeMemory >= 0
      ? Math.round(((totalMemory - freeMemory) / totalMemory) * 100)
      : null;
  $: totalDisk = parseIntValue(
    overview?.system_resource?.["total-hdd-space"] ||
      overview?.system_resource?.["total-hdd-space-b"],
  );
  $: freeDisk = parseIntValue(
    overview?.system_resource?.["free-hdd-space"] ||
      overview?.system_resource?.["free-hdd-space-b"],
  );
  $: diskUsagePercent =
    totalDisk > 0 && freeDisk >= 0
      ? Math.round(((totalDisk - freeDisk) / totalDisk) * 100)
      : null;
  $: uptimeValue = overview?.system_resource?.["uptime"] || "Pending";
  $: routerSummary = [
    identity.identity,
    identity.model || identity.board_name,
    identity.version ? `RouterOS ${identity.version}` : "",
  ]
    .filter(Boolean)
    .join(" • ");
  $: totalInventoryRecords = Object.values(state.counts).reduce((sum, count) => {
    if (typeof count !== "number") return sum;
    return sum + count;
  }, 0);
  $: credentialSummary = capability.has_saved_access
    ? capability.preferred_username || "Saved API account"
    : capability.has_saved_winbox
      ? "Saved Winbox account available"
      : "No stored credentials";
  $: canAutoReuseWinbox =
    !!capability.has_saved_winbox && !capability.has_saved_access;
  $: showingSavedWinboxValidation =
    !!canAutoReuseWinbox &&
    !accessReady &&
    pendingMutation === "configure_access" &&
    !state.error;
  $: showManualAccessForm =
    !accessReady &&
    (!canAutoReuseWinbox ||
      (autoUsedSavedWinboxForPeer === peerId && !showingSavedWinboxValidation));
  $: editorKind = resolveEditorKind();
  $: editorFieldDefinitions =
    currentResource && editorMode
      ? resolveEditorFieldDefinitions(currentResource, editorKind)
      : [];
  $: editorSuggestedKeys =
    currentResource && editorMode
      ? buildEditorKeySuggestions(currentResource, editorKind)
      : [];
  $: editorMissingKeys = editorSuggestedKeys
    .filter((key) => !editorRows.some((row) => row.key === key))
    .slice(0, 18);
  $: if (peerId && peerId !== openedPeerId) {
    openedPeerId = peerId;
    selectedRecordId = "";
    searchQuery = "";
    editorMode = null;
    editorRows = [];
    showDeleteConfirm = false;
    autoUsedSavedWinboxForPeer = "";
    routerOSStore.open(peerId, currentResource);
  }
  $: if (!peerId && openedPeerId) {
    openedPeerId = "";
    routerOSStore.close();
  }
  $: if (!usernameTouched && capability.preferred_username) {
    setupUsername = capability.preferred_username;
  }
  $: if (!portTouched && capability.api_port) {
    setupPort = capability.api_port;
  }
  $: if (!tlsTouched && capability.api_tls !== undefined) {
    setupUseTls = !!capability.api_tls;
  }
  $: if (
    currentResource &&
    filteredRecords.length > 0 &&
    !filteredRecords.some((record) => record.id === selectedRecordId)
  ) {
    selectedRecordId = filteredRecords[0].id;
  }
  $: if (currentResource && filteredRecords.length === 0 && selectedRecordId) {
    selectedRecordId = "";
  }
  $: if (state.notice && state.notice !== lastHandledNotice) {
    lastHandledNotice = state.notice;
    if (
      pendingMutation === "configure_access" &&
      state.notice.startsWith("configure access")
    ) {
      pendingMutation = null;
      setupPassword = "";
      editorError = "";
    }
    if (pendingMutation === "add" && state.notice.startsWith("add")) {
      pendingMutation = null;
      editorMode = null;
      editorRows = [];
      editorError = "";
    }
    if (pendingMutation === "update" && state.notice.startsWith("update")) {
      pendingMutation = null;
      editorMode = null;
      editorRows = [];
      editorError = "";
    }
    if (pendingMutation === "delete" && state.notice.startsWith("delete")) {
      pendingMutation = null;
      showDeleteConfirm = false;
      deleteTargetId = "";
      selectedRecordId = "";
    }
  }
  $: if (state.error && pendingMutation) {
    pendingMutation = null;
  }
  $: if (
    !!peerId &&
    !accessReady &&
    !state.isSavingAccess &&
    !pendingMutation &&
    !capability.has_saved_access &&
    !!capability.has_saved_winbox &&
    autoUsedSavedWinboxForPeer !== peerId
  ) {
    autoUsedSavedWinboxForPeer = peerId;
    verifyAndSaveAccess(true);
  }

  onDestroy(() => {
    routerOSStore.close();
  });

  function filterRecords(records: RouterOSRecord[], query: string) {
    const term = query.trim().toLowerCase();
    if (!term) return records;
    return records.filter((record) => {
      if (record.id.toLowerCase().includes(term)) return true;
      return Object.entries(record.fields).some(([key, value]) =>
        `${key} ${value}`.toLowerCase().includes(term),
      );
    });
  }

  function pickColumns(resource: RouterOSResourceKey, records: RouterOSRecord[]) {
    const preferred = resourceFieldTemplates[resource] || [];
    const score = new Map<string, number>();

    for (const key of preferred) {
      score.set(key, (score.get(key) || 0) + 100);
    }

    for (const record of records) {
      for (const [key, value] of Object.entries(record.fields)) {
        if (!value || key === ".id" || key === "id") continue;
        score.set(key, (score.get(key) || 0) + 1);
      }
    }

    return Array.from(score.entries())
      .sort((a, b) => b[1] - a[1] || a[0].localeCompare(b[0]))
      .slice(0, 4)
      .map(([key]) => key);
  }

  function topPairs(source: Record<string, string>, limit: number) {
    return sortFieldEntries(source).slice(0, limit);
  }

  function sortFieldEntries(fields: Record<string, string>) {
    return Object.entries(fields)
      .filter(([key]) => !isMetaField(key))
      .sort(([left], [right]) => compareFieldKeys(left, right))
      .map(([key, value]) => ({ key, value }));
  }

  function compactIdentityPairs(source: Record<string, unknown>) {
    const raw: Record<string, string> = {};
    for (const [key, value] of Object.entries(source || {})) {
      if (typeof value === "string" && value.trim()) {
        raw[key] = value;
      }
    }
    return sortFieldEntries(raw).slice(0, 6);
  }

  function compareFieldKeys(left: string, right: string) {
    const leftIndex = preferredFieldOrder.indexOf(left);
    const rightIndex = preferredFieldOrder.indexOf(right);
    if (leftIndex !== -1 || rightIndex !== -1) {
      if (leftIndex === -1) return 1;
      if (rightIndex === -1) return -1;
      return leftIndex - rightIndex;
    }
    return left.localeCompare(right);
  }

  function formatFieldLabel(key: string) {
    if (key === "__name_only") return "Name";
    return key
      .replace(/^\./, "")
      .replace(/^__/, "")
      .replace(/[-_.]/g, " ")
      .replace(/\b\w/g, (match) => match.toUpperCase());
  }

  function formatTimestamp(value?: { seconds?: number; nanos?: number } | string) {
    const date = protoToDate(value || null);
    return date ? date.toLocaleString() : "Never";
  }

  function formatRelativeTimestamp(
    value?: { seconds?: number; nanos?: number } | string,
  ) {
    const date = protoToDate(value || null);
    if (!date) return "Never";
    const deltaMs = Date.now() - date.getTime();
    if (deltaMs < 60_000) return "Just now";
    if (deltaMs < 3_600_000) return `${Math.round(deltaMs / 60_000)}m ago`;
    if (deltaMs < 86_400_000) return `${Math.round(deltaMs / 3_600_000)}h ago`;
    return `${Math.round(deltaMs / 86_400_000)}d ago`;
  }

  function valueSummary(record: RouterOSRecord, key: string) {
    return record.fields[key] || "—";
  }

  function renderValue(value: string) {
    return value?.trim() ? value : "—";
  }

  function parseIntValue(value?: string) {
    if (!value) return -1;
    const normalized = String(value).replace(/[^\d.-]/g, "");
    const parsed = Number(normalized);
    return Number.isFinite(parsed) ? parsed : -1;
  }

  function formatBytes(value: number) {
    if (!Number.isFinite(value) || value <= 0) return "0 B";
    const units = ["B", "KB", "MB", "GB", "TB"];
    let size = value;
    let unitIndex = 0;
    while (size >= 1024 && unitIndex < units.length - 1) {
      size /= 1024;
      unitIndex += 1;
    }
    return `${size >= 10 || unitIndex === 0 ? size.toFixed(0) : size.toFixed(
      1,
    )} ${units[unitIndex]}`;
  }

  function percentBar(value: number | null) {
    if (value === null || !Number.isFinite(value)) return "0%";
    return `${Math.max(0, Math.min(100, value))}%`;
  }

  function isMetaField(key: string) {
    return key === ".id" || key.startsWith("__");
  }

  function displayRecordId(record: RouterOSRecord | null) {
    if (!record) return "—";
    const rawId = String(record.fields["__routeros_id"] || record.id || "");
    const normalizedId = rawId.replace(/^#+/, "").replace(/^\*+/, "").trim();
    return normalizedId ? `#${normalizedId}` : rawId || "—";
  }

  function sectionCount(section: RouterOSResourceKey) {
    const cached = state.counts[section];
    if (typeof cached === "number") {
      return cached;
    }
    return state.records[section]?.length || 0;
  }

  function sectionCountPending(section: RouterOSResourceKey) {
    return state.counts[section] === null && !!state.sectionLoading[section];
  }

  function sectionRecordCount(section: DashboardSection) {
    return section === "overview" ? 0 : sectionCount(section);
  }

  function switchSection(section: DashboardSection) {
    activeSection = section;
    searchQuery = "";
    selectedRecordId = "";
    editorMode = null;
    editorRows = [];
    editorMeta = {};
    showDeleteConfirm = false;
    activeScopeKey = "";
    isNavOpen = false;
    routerOSStore.clearError();
    if (section !== "overview") {
      routerOSStore.loadResource(section, false);
    }
  }

  function refreshCurrent(forceResourceReload = true) {
    routerOSStore.clearError();
    if (currentResource) {
      if (forceResourceReload) {
        routerOSStore.loadResource(currentResource, true);
      } else {
        routerOSStore.refresh([currentResource]);
      }
      return;
    }
    routerOSStore.refresh([]);
  }

  function createEditorRow(key = "", value = ""): EditorRow {
    rowCounter += 1;
    return {
      uid: `row-${rowCounter}`,
      key,
      value,
    };
  }

  function resolveEditorKind() {
    if (editorMeta["__kind"]) return editorMeta["__kind"];
    if (selectedRecord?.fields["__kind"]) return selectedRecord.fields["__kind"];
    if (currentResource === "bridge") {
      if (activeScopeKey.endsWith(":ports") || activeScopeKey === "ports") {
        return "port";
      }
      if (activeScopeKey.endsWith(":vlans") || activeScopeKey === "vlans") {
        return "vlan";
      }
      return "bridge";
    }
    if (currentResource === "wireless") {
      if (activeScopeKey === "profiles") return "security_profile";
      if (activeScopeKey === "configurations") return "configuration";
      return "interface";
    }
    return "default";
  }

  function resolveEditorFieldDefinitions(
    resource: RouterOSResourceKey,
    kind: string,
  ) {
    const merged = new Map<string, EditorFieldDefinition>();
    for (const definition of resourceEditorSchemas[resource] || []) {
      merged.set(definition.key, definition);
    }
    for (const definition of scopedEditorSchemas[`${resource}:${kind}`] || []) {
      merged.set(definition.key, {
        ...(merged.get(definition.key) || {}),
        ...definition,
      });
    }
    return Array.from(merged.values()).sort(
      (left, right) =>
        compareFieldKeys(left.key, right.key) || left.key.localeCompare(right.key),
    );
  }

  function relevantEditorRecords(
    resource: RouterOSResourceKey,
    kind: string,
  ): RouterOSRecord[] {
    const records = state.records[resource] || [];
    if (!kind || kind === "default") return records;
    return records.filter((record) => (record.fields["__kind"] || "default") === kind);
  }

  function buildEditorKeySuggestions(
    resource: RouterOSResourceKey,
    kind: string,
  ) {
    const score = new Map<string, number>();

    for (const definition of resolveEditorFieldDefinitions(resource, kind)) {
      score.set(definition.key, (score.get(definition.key) || 0) + 120);
    }

    for (const record of relevantEditorRecords(resource, kind)) {
      for (const key of Object.keys(record.fields)) {
        if (isMetaField(key)) continue;
        score.set(key, (score.get(key) || 0) + 1);
      }
    }

    if (selectedRecord) {
      for (const key of Object.keys(selectedRecord.fields)) {
        if (isMetaField(key)) continue;
        score.set(key, (score.get(key) || 0) + 24);
      }
    }

    return Array.from(score.entries())
      .sort((a, b) => b[1] - a[1] || compareFieldKeys(a[0], b[0]))
      .map(([key]) => key);
  }

  function inferFieldDefinition(key: string): EditorFieldDefinition {
    const normalized = key.trim();
    if (!normalized) {
      return {
        key: "",
        placeholder: "field-name",
        hint: "RouterOS property name",
      };
    }
    if (
      normalized === "disabled" ||
      normalized === "enabled" ||
      normalized.endsWith("-enable") ||
      normalized.endsWith("-enabled") ||
      normalized.endsWith("-snooping") ||
      normalized.startsWith("fast-") ||
      normalized.startsWith("dhcp-")
    ) {
      return { key: normalized, options: ["true", "false"] };
    }
    if (
      normalized.includes("password") ||
      normalized.includes("secret") ||
      normalized.includes("private-key")
    ) {
      return { key: normalized, input: "password" };
    }
    if (
      normalized === "comment" ||
      normalized.includes("script") ||
      normalized.includes("certificate")
    ) {
      return { key: normalized, input: "textarea" };
    }
    if (["distance", "pvid", "mtu", "actual-mtu", "l2mtu", "cost"].includes(normalized)) {
      return { key: normalized, input: "number" };
    }
    return { key: normalized, input: "text" };
  }

  function fieldDefinitionForKey(key: string) {
    return (
      editorFieldDefinitions.find((definition) => definition.key === key.trim()) ||
      inferFieldDefinition(key)
    );
  }

  function uniqueValues(values: Array<string | undefined>) {
    const seen = new Set<string>();
    const next: string[] = [];
    for (const value of values) {
      const normalized = (value || "").trim();
      if (!normalized || seen.has(normalized)) continue;
      seen.add(normalized);
      next.push(normalized);
    }
    return next;
  }

  function collectInterfaceOptions() {
    const values: string[] = [];
    for (const key of ["addresses", "wireless", "bridge"] as RouterOSResourceKey[]) {
      for (const record of state.records[key] || []) {
        const name =
          record.fields["interface"] ||
          record.fields["name"] ||
          record.fields["__bridge"];
        if (name) values.push(name);
      }
    }
    return uniqueValues(values);
  }

  function collectBridgeOptions() {
    return uniqueValues(
      (state.records.bridge || [])
        .filter((record) => record.fields["__kind"] === "bridge")
        .map((record) => record.fields["name"] || record.fields["__bridge"]),
    );
  }

  function collectWirelessProfileOptions() {
    return uniqueValues(
      (state.records.wireless || [])
        .filter((record) => record.fields["__kind"] === "security_profile")
        .map((record) => record.fields["name"]),
    );
  }

  function optionSourceValues(source?: EditorOptionSource) {
    switch (source) {
      case "interfaces":
        return collectInterfaceOptions();
      case "bridges":
        return collectBridgeOptions();
      case "wireless_profiles":
        return collectWirelessProfileOptions();
      default:
        return [];
    }
  }

  function observedFieldValues(key: string) {
    if (!currentResource || !key.trim()) return [];
    return uniqueValues(
      relevantEditorRecords(currentResource, editorKind).map(
        (record) => record.fields[key],
      ),
    ).slice(0, 16);
  }

  function fieldEnumOptions(row: EditorRow) {
    const definition = fieldDefinitionForKey(row.key);
    if (!definition.options?.length) return [];
    return uniqueValues([row.value, ...definition.options, ...observedFieldValues(row.key)]);
  }

  function fieldAutocompleteValues(row: EditorRow) {
    const definition = fieldDefinitionForKey(row.key);
    return uniqueValues([
      row.value,
      ...optionSourceValues(definition.optionSource),
      ...observedFieldValues(row.key),
    ]).slice(0, 20);
  }

  function usesEnumInput(row: EditorRow) {
    return fieldEnumOptions(row).length > 0;
  }

  function usesTextarea(row: EditorRow) {
    const definition = fieldDefinitionForKey(row.key);
    return (
      definition.input === "textarea" ||
      row.value.includes("\n") ||
      row.value.length > 72
    );
  }

  function inputTypeForRow(row: EditorRow) {
    const definition = fieldDefinitionForKey(row.key);
    if (definition.input === "password") return "password";
    if (definition.input === "number") return "number";
    return "text";
  }

  function placeholderForRow(row: EditorRow) {
    return fieldDefinitionForKey(row.key).placeholder || "value";
  }

  function hintForRow(row: EditorRow) {
    return fieldDefinitionForKey(row.key).hint || "";
  }

  function addSuggestedField(key: string) {
    if (editorRows.some((row) => row.key === key)) return;
    editorRows = [
      ...editorRows,
      createEditorRow(key, suggestedValueForField(key, selectedRecord)),
    ];
  }

  function suggestedValueForField(key: string, record: RouterOSRecord | null) {
    if (record?.fields[key]) return record.fields[key];
    if (
      currentResource === "bridge" &&
      key === "bridge" &&
      activeScopeKey.startsWith("bridge:")
    ) {
      return activeScopeKey
        .replace(/^bridge:/, "")
        .replace(/:(ports|vlans)$/, "");
    }
    switch (key) {
      case "disabled":
      case "enabled":
      case "trusted":
      case "fast-forward":
      case "igmp-snooping":
      case "vlan-filtering":
      case "periodic-inform-enable":
        return "false";
      case "protocol-mode":
        return "rstp";
      case "arp":
        return "enabled";
      case "mode":
        return currentResource === "wireless" && editorKind === "security_profile"
          ? "dynamic-keys"
          : "";
      default:
        return "";
    }
  }

  function openAddEditor() {
    if (!currentResource) return;
    editorMode = "add";
    editorError = "";
    editorMeta = defaultEditorMeta(currentResource, activeScopeKey);
    editorRows = initialEditorFields(currentResource, activeScopeKey).map((key) =>
      createEditorRow(key, suggestedValueForField(key, null)),
    );
    if (editorRows.length === 0) {
      editorRows = [createEditorRow()];
    }
  }

  function openEditEditor() {
    if (!selectedRecord || !currentResource) return;
    editorMode = "edit";
    editorError = "";
    editorMeta = Object.fromEntries(
      Object.entries(selectedRecord.fields).filter(([key]) =>
        key.startsWith("__"),
      ),
    );
    const rankedKeys = buildEditorKeySuggestions(
      currentResource,
      selectedRecord.fields["__kind"] || editorKind,
    );
    const visibleKeys = rankedKeys.filter((key) => key in selectedRecord.fields);
    const initialKeys = (visibleKeys.length ? visibleKeys : Object.keys(selectedRecord.fields))
      .filter((key) => !isMetaField(key))
      .slice(0, 10);
    editorRows = initialKeys.map((key) =>
      createEditorRow(key, selectedRecord.fields[key] || ""),
    );
    if (editorRows.length === 0) {
      editorRows = [createEditorRow()];
    }
  }

  function closeEditor() {
    editorMode = null;
    editorRows = [];
    editorMeta = {};
    editorError = "";
    pendingMutation = null;
  }

  function addEditorField() {
    const nextSuggested = editorSuggestedKeys.find(
      (key) => !editorRows.some((row) => row.key === key),
    );
    editorRows = [
      ...editorRows,
      createEditorRow(
        nextSuggested || "",
        nextSuggested ? suggestedValueForField(nextSuggested, selectedRecord) : "",
      ),
    ];
  }

  function removeEditorField(uid: string) {
    if (editorRows.length <= 1) return;
    editorRows = editorRows.filter((row) => row.uid !== uid);
  }

  function updateEditorField(
    uid: string,
    field: "key" | "value",
    value: string,
  ) {
    editorRows = editorRows.map((row) =>
      row.uid === uid ? { ...row, [field]: value } : row,
    );
  }

  function buildEditorPayload() {
    const payload: Record<string, string> = { ...editorMeta };
    for (const row of editorRows) {
      const key = row.key.trim();
      if (!key) continue;
      payload[key] = row.value;
    }
    return payload;
  }

  function submitEditor() {
    if (!currentResource) return;
    const payload = buildEditorPayload();
    if (Object.keys(payload).length === 0) {
      editorError = "Add at least one field before saving.";
      return;
    }

    routerOSStore.clearError();
    routerOSStore.clearNotice();
    editorError = "";

    if (editorMode === "add") {
      pendingMutation = "add";
      routerOSStore.addResource(currentResource, payload);
      return;
    }

    if (editorMode === "edit" && selectedRecord) {
      pendingMutation = "update";
      routerOSStore.updateResource(currentResource, selectedRecord.id, payload);
    }
  }

  function askDeleteRecord() {
    if (!selectedRecord) return;
    deleteTargetId = selectedRecord.id;
    deleteTargetLabel = displayRecordId(selectedRecord);
    showDeleteConfirm = true;
  }

  function confirmDeleteRecord() {
    if (!currentResource || !deleteTargetId) return;
    routerOSStore.clearError();
    routerOSStore.clearNotice();
    pendingMutation = "delete";
    routerOSStore.deleteResource(currentResource, deleteTargetId);
  }

  function verifyAndSaveAccess(useSavedWinbox = false) {
    routerOSStore.clearError();
    routerOSStore.clearNotice();
    pendingMutation = "configure_access";
    routerOSStore.configureAccess({
      username: useSavedWinbox ? "" : setupUsername,
      password: useSavedWinbox ? "" : setupPassword,
      port: setupPort,
      use_tls: setupUseTls,
      use_saved_winbox: useSavedWinbox,
    });
  }

  function transportLabel() {
    return setupUseTls ? `TLS / ${setupPort}` : `TCP / ${setupPort}`;
  }

  function inputValue(event: Event) {
    return (
      event.currentTarget as
        | HTMLInputElement
        | HTMLTextAreaElement
        | HTMLSelectElement
    ).value;
  }

  function handleSetupUsernameInput(event: Event) {
    usernameTouched = true;
    setupUsername = inputValue(event);
  }

  function handleSetupPasswordInput(event: Event) {
    setupPassword = inputValue(event);
  }

  function handleSetupPortInput(event: Event) {
    portTouched = true;
    setupPort = Number(inputValue(event)) || 8728;
  }

  function closeNavigator() {
    isNavOpen = false;
  }

  function handleNavToggle() {
    if (isMobileView) {
      isNavOpen = !isNavOpen;
      return;
    }
    sidebarCollapsed = !sidebarCollapsed;
  }

  function supportsAdd(resource: RouterOSResourceKey | null) {
    if (!resource) return false;
    return !["packages", "files"].includes(resource);
  }

  function supportsEdit(resource: RouterOSResourceKey | null) {
    if (!resource) return false;
    return !["packages", "files"].includes(resource);
  }

  function supportsDelete(resource: RouterOSResourceKey | null) {
    if (!resource) return false;
    return resource !== "packages";
  }

  function initialEditorFields(resource: RouterOSResourceKey, scopeKey: string) {
    if (resource === "bridge") {
      if (scopeKey.endsWith(":ports") || scopeKey === "ports") {
        return ["bridge", "interface", "pvid", "comment"];
      }
      if (scopeKey.endsWith(":vlans") || scopeKey === "vlans") {
        return ["bridge", "vlan-ids", "tagged", "untagged", "comment"];
      }
      return ["name", "protocol-mode", "arp", "comment"];
    }
    if (resource === "wireless") {
      if (scopeKey === "profiles") {
        return ["name", "authentication-types", "mode", "comment"];
      }
      return ["name", "ssid", "band", "disabled", "comment"];
    }
    return resourceFieldTemplates[resource] || ["name", "comment"];
  }

  function defaultEditorMeta(resource: RouterOSResourceKey, scopeKey: string) {
    const resolvePath = (kind: string, fallback: string) =>
      resourceRecords.find((record) => record.fields["__kind"] === kind)?.fields[
        "__path"
      ] || fallback;

    if (resource === "bridge") {
      if (scopeKey.endsWith(":ports") || scopeKey === "ports") {
        return {
          __path: resolvePath("port", "/interface/bridge/port"),
          __kind: "port",
        };
      }
      if (scopeKey.endsWith(":vlans") || scopeKey === "vlans") {
        return {
          __path: resolvePath("vlan", "/interface/bridge/vlan"),
          __kind: "vlan",
        };
      }
      return {
        __path: resolvePath("bridge", "/interface/bridge"),
        __kind: "bridge",
      };
    }
    if (resource === "wireless") {
      if (scopeKey === "profiles") {
        return {
          __path: resolvePath(
            "security_profile",
            "/interface/wireless/security-profiles",
          ),
          __kind: "security_profile",
        };
      }
      if (scopeKey === "configurations") {
        return {
          __path: resolvePath(
            "configuration",
            "/interface/wifi/configuration",
          ),
          __kind: "configuration",
        };
      }
      return {
        __path: resolvePath("interface", "/interface/wireless"),
        __kind: "interface",
      };
    }
    return {};
  }

  function buildSectionScopes(
    resource: RouterOSResourceKey,
    records: RouterOSRecord[],
  ): ScopeNode[] {
    if (resource === "bridge") {
      return buildBridgeScopes(records);
    }
    if (resource === "wireless") {
      return buildWirelessScopes(records);
    }
    if (resource === "files") {
      return buildFileScopes(records);
    }
    return [];
  }

  function applySectionScope(
    resource: RouterOSResourceKey,
    records: RouterOSRecord[],
    scopeKey: string,
  ) {
    if (resource === "bridge") {
      return filterBridgeRecords(records, scopeKey);
    }
    if (resource === "wireless") {
      return filterWirelessRecords(records, scopeKey);
    }
    if (resource === "files") {
      return filterFileRecords(records, scopeKey);
    }
    return records;
  }

  function buildBridgeScopes(records: RouterOSRecord[]): ScopeNode[] {
    const bridges = records.filter(
      (record) => record.fields["__kind"] === "bridge",
    );
    const ports = records.filter((record) => record.fields["__kind"] === "port");
    const vlans = records.filter((record) => record.fields["__kind"] === "vlan");
    const nodes: ScopeNode[] = [
      {
        key: "bridge-all",
        label: "All bridge items",
        hint: "Every bridge object",
        count: records.length,
        depth: 0,
      },
      {
        key: "bridges",
        label: "Bridges",
        hint: "Bridge interfaces",
        count: bridges.length,
        depth: 0,
      },
      {
        key: "ports",
        label: "Ports",
        hint: "Attached bridge ports",
        count: ports.length,
        depth: 0,
      },
      {
        key: "vlans",
        label: "VLANs",
        hint: "Bridge VLAN entries",
        count: vlans.length,
        depth: 0,
      },
    ];

    for (const bridge of bridges) {
      const bridgeName =
        bridge.fields["__bridge"] || bridge.fields["name"] || bridge.id;
      const bridgePorts = ports.filter(
        (record) => record.fields["__bridge"] === bridgeName,
      ).length;
      const bridgeVlans = vlans.filter(
        (record) => record.fields["__bridge"] === bridgeName,
      ).length;
      nodes.push({
        key: `bridge:${bridgeName}`,
        label: bridgeName,
        hint: `${bridgePorts} ports · ${bridgeVlans} vlans`,
        count: 1 + bridgePorts + bridgeVlans,
        depth: 1,
      });
      nodes.push({
        key: `bridge:${bridgeName}:ports`,
        label: "Ports",
        hint: "Bridge ports",
        count: bridgePorts,
        depth: 2,
      });
      nodes.push({
        key: `bridge:${bridgeName}:vlans`,
        label: "VLANs",
        hint: "Bridge VLANs",
        count: bridgeVlans,
        depth: 2,
      });
    }

    return nodes;
  }

  function filterBridgeRecords(records: RouterOSRecord[], scopeKey: string) {
    if (!scopeKey || scopeKey === "bridge-all") return records;
    if (scopeKey === "bridges") {
      return records.filter((record) => record.fields["__kind"] === "bridge");
    }
    if (scopeKey === "ports") {
      return records.filter((record) => record.fields["__kind"] === "port");
    }
    if (scopeKey === "vlans") {
      return records.filter((record) => record.fields["__kind"] === "vlan");
    }
    if (scopeKey.startsWith("bridge:") && scopeKey.endsWith(":ports")) {
      const bridgeName = scopeKey.slice("bridge:".length, -":ports".length);
      return records.filter(
        (record) =>
          record.fields["__kind"] === "port" &&
          record.fields["__bridge"] === bridgeName,
      );
    }
    if (scopeKey.startsWith("bridge:") && scopeKey.endsWith(":vlans")) {
      const bridgeName = scopeKey.slice("bridge:".length, -":vlans".length);
      return records.filter(
        (record) =>
          record.fields["__kind"] === "vlan" &&
          record.fields["__bridge"] === bridgeName,
      );
    }
    if (scopeKey.startsWith("bridge:")) {
      const bridgeName = scopeKey.slice("bridge:".length);
      return records.filter((record) => {
        if (record.fields["__kind"] === "bridge") {
          return (record.fields["__bridge"] || record.fields["name"]) === bridgeName;
        }
        return record.fields["__bridge"] === bridgeName;
      });
    }
    return records;
  }

  function buildWirelessScopes(records: RouterOSRecord[]): ScopeNode[] {
    const groups = [
      {
        key: "wireless-all",
        label: "All wireless",
        hint: "Interfaces and profiles",
        match: () => true,
      },
      {
        key: "interfaces",
        label: "Interfaces",
        hint: "Radios and SSIDs",
        match: (record: RouterOSRecord) =>
          record.fields["__kind"] === "interface",
      },
      {
        key: "profiles",
        label: "Profiles",
        hint: "Security profiles",
        match: (record: RouterOSRecord) =>
          record.fields["__kind"] === "security_profile",
      },
      {
        key: "configurations",
        label: "Configs",
        hint: "WiFi configuration sets",
        match: (record: RouterOSRecord) =>
          record.fields["__kind"] === "configuration",
      },
    ];
    return groups.map((group) => ({
      key: group.key,
      label: group.label,
      hint: group.hint,
      count: records.filter(group.match).length,
      depth: 0,
    }));
  }

  function filterWirelessRecords(records: RouterOSRecord[], scopeKey: string) {
    if (!scopeKey || scopeKey === "wireless-all") return records;
    if (scopeKey === "interfaces") {
      return records.filter((record) => record.fields["__kind"] === "interface");
    }
    if (scopeKey === "profiles") {
      return records.filter(
        (record) => record.fields["__kind"] === "security_profile",
      );
    }
    if (scopeKey === "configurations") {
      return records.filter(
        (record) => record.fields["__kind"] === "configuration",
      );
    }
    return records;
  }

  function buildFileScopes(records: RouterOSRecord[]): ScopeNode[] {
    const directories = new Map<string, number>();
    directories.set(
      "",
      records.filter((record) => (record.fields["__directory"] || "") === "")
        .length,
    );

    for (const record of records) {
      const directory = record.fields["__directory"] || "";
      const parts = directory ? directory.split("/") : [];
      let current = "";
      for (const part of parts) {
        current = current ? `${current}/${part}` : part;
        directories.set(current, (directories.get(current) || 0) + 1);
      }
    }

    return [
      {
        key: "root",
        label: "Root",
        hint: "Top level files",
        count: directories.get("") || 0,
        depth: 0,
      },
      ...Array.from(directories.entries())
        .filter(([dir]) => dir)
        .sort(([left], [right]) => left.localeCompare(right))
        .map(([dir, count]) => ({
          key: `dir:${dir}`,
          label: dir.split("/").slice(-1)[0],
          hint: dir,
          count,
          depth: dir.split("/").length,
        })),
    ];
  }

  function filterFileRecords(records: RouterOSRecord[], scopeKey: string) {
    if (!scopeKey || scopeKey === "root") {
      return records.filter(
        (record) => (record.fields["__directory"] || "") === "",
      );
    }
    if (scopeKey.startsWith("dir:")) {
      const directory = scopeKey.slice(4);
      return records.filter(
        (record) => (record.fields["__directory"] || "") === directory,
      );
    }
    return records;
  }
</script>

<AppWindow
  appName={APP_NAME}
  title={`RouterOS — ${peerName}`}
  width="min(1360px, 94vw)"
  height="min(900px, 90vh)"
  minWidth="980px"
  minHeight="680px"
  top="4.5%"
  left="3.5%"
  canResize={true}
>
  <div class="routeros-shell">
    {#if $isMobile && isNavOpen}
      <button
        class="mobile-scrim"
        type="button"
        on:click={closeNavigator}
        aria-label="Close RouterOS sidebar"
      />
    {/if}

    <div class="shell-layout">
      <aside
        class="device-rail"
        class:mobile-open={isNavOpen}
        class:collapsed={sidebarCollapsed}
      >
        <div class="rail-header">
          <div class="rail-brand-mark" aria-hidden="true">
            <img
              class="rail-brand-logo"
              src="/img/icon/brands/mikrotik.png"
              alt=""
            />
          </div>
          {#if !sidebarCollapsed || $isMobile}
            <div class="rail-brand-copy">
              <p class="eyebrow">{shellTitle}</p>
              <strong>{peerName}</strong>
            </div>
          {/if}
        </div>

        {#if !sidebarCollapsed || $isMobile}
          <section class="rail-device-card">
            <div class="device-card-head">
              <div>
                <p class="eyebrow">Device</p>
                <h2>{identity.identity || peerName}</h2>
              </div>
              <span class:online={peerOnline} class="live-pill">
                {peerOnline ? "Online" : "Offline"}
              </span>
            </div>
            <p class="device-summary">
              {routerSummary ||
                "RouterOS controller workspace over the overlay tunnel."}
            </p>
            <div class="device-meta-grid">
              <div>
                <span>Access</span>
                <strong>{accessStatusLabel}</strong>
              </div>
              <div>
                <span>Validated</span>
                <strong>{lastChecked === "Never" ? "Pending" : formatRelativeTimestamp(capability.last_validated)}</strong>
              </div>
              <div>
                <span>Endpoint</span>
                <strong>{apiEndpoint}</strong>
              </div>
              <div>
                <span>Source</span>
                <strong>{accessSource}</strong>
              </div>
            </div>
          </section>
        {/if}

        <nav class="rail-nav">
          {#each sectionMeta as section}
            <button
              type="button"
              class="nav-item"
              class:active={section.key === activeSection}
              title={section.label}
              on:click={() => switchSection(section.key)}
            >
              <span class="nav-item-icon">
                <svelte:component this={section.icon} size={14} />
              </span>
              {#if !sidebarCollapsed || $isMobile}
                <span class="nav-item-copy">
                  <strong>{section.label}</strong>
                  <small>{section.description}</small>
                </span>
                {#if section.key !== "overview"}
                  <span
                    class="nav-item-count"
                    class:pending={sectionPendingMap[section.key]}
                  >
                    {sectionCountMap[section.key] ?? 0}
                  </span>
                {/if}
              {/if}
            </button>
          {/each}
        </nav>
      </aside>

      <main class="workspace-shell">
        <header class="command-header">
          <button
            class="shell-toggle"
            type="button"
            on:click={handleNavToggle}
            aria-label="Toggle RouterOS sidebar"
          >
            <MenuIcon size={16} />
          </button>

          <div class="command-copy">
            <p class="eyebrow">Cloud Management</p>
            <div class="command-title">
              <h1>{currentSectionMeta.label}</h1>
              <span>{peerName}</span>
            </div>
            <p>{currentSectionMeta.description}</p>
          </div>

          <div class="command-actions">
            <span
              class="status-chip"
              class:success={accessReady}
              class:warn={!accessReady}
            >
              {accessStatusLabel}
            </span>
            <button
              class="icon-action"
              type="button"
              on:click={() => refreshCurrent(true)}
              title="Refresh current section"
            >
              <RefreshCcwIcon size={14} />
            </button>
          </div>
        </header>

        <div class="workspace-body">
          {#if state.error}
            <div class="banner error">
              <div>
                <strong>RouterOS dashboard</strong>
                <p>{state.error}</p>
              </div>
              <div class="banner-actions">
                <button
                  class="text-button"
                  type="button"
                  on:click={() => routerOSStore.clearError()}
                >
                  Dismiss
                </button>
                <button
                  class="text-button primary"
                  type="button"
                  on:click={() => routerOSStore.reconnect()}
                >
                  Reconnect
                </button>
              </div>
            </div>
          {/if}

          {#if state.notice}
            <div class="banner notice">
              <div>
                <strong>Action completed</strong>
                <p>{state.notice}</p>
              </div>
              <button
                class="text-button"
                type="button"
                on:click={() => routerOSStore.clearNotice()}
              >
                Dismiss
              </button>
            </div>
          {/if}

          {#if !selectedPeer}
            <section class="empty-state">
              <ServerIcon size={20} />
              <div>
                <h2>No device selected</h2>
                <p>
                  Open RouterOS from a MikroTik peer row so the dashboard knows
                  which device to attach to.
                </p>
              </div>
            </section>
          {:else if activeSection === "overview"}
            <div class="overview-grid">
              <section class="panel hero-panel">
                <div class="panel-head compact">
                  <div>
                    <p class="card-kicker">Router</p>
                    <h2>{identity.identity || peerName}</h2>
                  </div>
                </div>
                <div class="hero-grid">
                  <div class="hero-stat">
                    <span>Platform</span>
                    <strong class:pending-text={!identity.platform && !identity.model}>
                      {identity.platform || identity.model || "MikroTik"}
                    </strong>
                    <small class:pending-text={!identity.architecture}>
                      {identity.architecture || "Architecture pending"}
                    </small>
                  </div>
                  <div class="hero-stat">
                    <span>Version</span>
                    <strong class:pending-text={!identity.version}>
                      {identity.version ? `ROS ${identity.version}` : "Pending"}
                    </strong>
                    <small class:pending-text={!identity.board_name}>
                      {identity.board_name || "Board pending"}
                    </small>
                  </div>
                  <div class="hero-stat">
                    <span>API endpoint</span>
                    <strong>{apiEndpoint}</strong>
                    <small class:pending-text={!accessSource || accessSource === "none"}>
                      {accessSource}
                    </small>
                  </div>
                  <div class="hero-stat">
                    <span>Last validation</span>
                    <strong class:pending-text={lastChecked === "Never"}>{lastChecked}</strong>
                    <small class:pending-text={!capability.last_error && lastChecked === "Never"}>
                      {capability.last_error ? "Needs attention" : (lastChecked === "Never" ? "Awaiting probe" : "Synced session")}
                    </small>
                  </div>
                </div>
              </section>

              <section class="panel metric-panel">
                <div class="panel-head compact">
                  <div>
                    <p class="card-kicker">Overlay link</p>
                    <h2>Link performance</h2>
                  </div>
                </div>
                <div class="metric-stack">
                  <div class="metric-row">
                    <span>Connection</span>
                    <strong>{peerOnline ? "Online" : "Offline"}</strong>
                  </div>
                  <div class="metric-row">
                    <span>Last seen</span>
                    <strong>{formatRelativeTimestamp(peerLastSeen)}</strong>
                  </div>
                  <div class="metric-row">
                    <span>Download</span>
                    <strong>{formatBytes(peerRxBytes)}</strong>
                  </div>
                  <div class="metric-row">
                    <span>Upload</span>
                    <strong>{formatBytes(peerTxBytes)}</strong>
                  </div>
                  <div class="traffic-bars">
                    <div>
                      <span>RX</span>
                      <div class="progress-track">
                        <span
                          style={`width:${percentBar(
                            overlayTrafficTotal > 0
                              ? (peerRxBytes / overlayTrafficTotal) * 100
                              : 0,
                          )}`}
                        ></span>
                      </div>
                    </div>
                    <div>
                      <span>TX</span>
                      <div class="progress-track">
                        <span
                          class="tx"
                          style={`width:${percentBar(
                            overlayTrafficTotal > 0
                              ? (peerTxBytes / overlayTrafficTotal) * 100
                              : 0,
                          )}`}
                        ></span>
                      </div>
                    </div>
                  </div>
                </div>
              </section>

              <section class="panel metric-panel">
                <div class="panel-head compact">
                  <div>
                    <p class="card-kicker">System</p>
                    <h2>Runtime health</h2>
                  </div>
                </div>
                <div class="metric-stack">
                  <div class="metric-row">
                    <span>Uptime</span>
                    <strong class:pending-text={uptimeValue === "Pending"}>{uptimeValue}</strong>
                  </div>
                  <div>
                    <div class="progress-head">
                      <span>CPU load</span>
                      <strong class:pending-text={cpuLoad < 0}>
                        {cpuLoad >= 0 ? `${cpuLoad}%` : "Pending"}
                      </strong>
                    </div>
                    <div class="progress-track">
                      <span style={`width:${percentBar(cpuLoad >= 0 ? cpuLoad : 0)}`}></span>
                    </div>
                  </div>
                  <div>
                    <div class="progress-head">
                      <span>Memory used</span>
                      <strong class:pending-text={memoryUsagePercent === null}>
                        {memoryUsagePercent !== null
                          ? `${memoryUsagePercent}%`
                          : "Pending"}
                      </strong>
                    </div>
                    <div class="progress-track">
                      <span class="warn" style={`width:${percentBar(memoryUsagePercent)}`}></span>
                    </div>
                  </div>
                  <div>
                    <div class="progress-head">
                      <span>Storage used</span>
                      <strong class:pending-text={diskUsagePercent === null}>
                        {diskUsagePercent !== null ? `${diskUsagePercent}%` : "Pending"}
                      </strong>
                    </div>
                    <div class="progress-track">
                      <span class="danger" style={`width:${percentBar(diskUsagePercent)}`}></span>
                    </div>
                  </div>
                </div>
              </section>

              <section class="panel inventory-panel">
                <div class="panel-head compact">
                  <div>
                    <p class="card-kicker">Inventory</p>
                    <h2>Live sections</h2>
                  </div>
                  <span class="panel-caption">{totalInventoryRecords} total</span>
                </div>
                <div class="inventory-grid">
                  {#each sectionMeta.filter((section) => section.key !== "overview") as section}
                    <button
                      type="button"
                      class="inventory-item"
                      on:click={() => switchSection(section.key)}
                    >
                      <span>{section.label}</span>
                      <strong>{sectionCountMap[section.key] ?? 0}</strong>
                    </button>
                  {/each}
                </div>
              </section>

              <section class="panel identity-panel">
                <div class="panel-head compact">
                  <div>
                    <p class="card-kicker">Identity</p>
                    <h2>Device profile</h2>
                  </div>
                </div>
                {#if identityPairs.length === 0 && routerboardPairs.length === 0}
                  <div class="profile-empty">
                    <p class="profile-empty-title">Awaiting first probe</p>
                    <p class="profile-empty-hint">
                      {accessReady
                        ? "Identity and routerboard fields populate after the first /system/identity and /system/routerboard read."
                        : "Configure RouterOS access to start populating identity and routerboard fields."}
                    </p>
                  </div>
                {:else}
                  <div class="info-grid">
                    {#each identityPairs as item}
                      <div class="info-item">
                        <span>{formatFieldLabel(item.key)}</span>
                        <strong>{renderValue(item.value)}</strong>
                      </div>
                    {/each}
                    {#each routerboardPairs.slice(0, 3) as item}
                      <div class="info-item">
                        <span>{formatFieldLabel(item.key)}</span>
                        <strong>{renderValue(item.value)}</strong>
                      </div>
                    {/each}
                  </div>
                {/if}
              </section>

              <section class="panel access-panel">
                <div class="panel-head compact">
                  <div>
                    <p class="card-kicker">Access</p>
                    <h2>Management session</h2>
                  </div>
                </div>
                <div class="access-summary">
                  <div class="metric-row">
                    <span>Saved access</span>
                    <strong class:pending-text={!accessReady}>{credentialSummary}</strong>
                  </div>
                  <div class="metric-row">
                    <span>Preferred user</span>
                    <strong class:pending-text={!capability.preferred_username}>
                      {capability.preferred_username || "Unknown"}
                    </strong>
                  </div>
                  <div class="metric-row">
                    <span>Winbox reuse</span>
                    <strong>
                      {accessReady
                        ? "Already attached"
                        : canAutoReuseWinbox
                          ? "Automatic"
                          : "Unavailable"}
                    </strong>
                  </div>
                </div>
                <div class="panel-actions">
                  <button
                    class="small-button"
                    class:primary-cta={!accessReady}
                    type="button"
                    on:click={() => switchSection("addresses")}
                  >
                    {accessReady ? "Open IP Addresses" : "Configure access"}
                  </button>
                </div>
              </section>
            </div>
          {:else}
            <section class="section-shell">
              <div class="section-toolbar">
                <div class="search-box">
                  <SearchIcon size={13} />
                  <input
                    bind:value={searchQuery}
                    placeholder={`Search ${currentSectionMeta.label.toLowerCase()}...`}
                  />
                </div>

                <div class="toolbar-meta">
                  <span>{filteredRecords.length} / {scopedRecords.length}</span>
                  {#if resourceLoading}
                    <span class="loading-inline">Live loading</span>
                  {/if}
                </div>

                <div class="toolbar-icons">
                  <button
                    class="icon-action"
                    type="button"
                    title="Add record"
                    disabled={!accessReady || !supportsAdd(currentResource)}
                    on:click={openAddEditor}
                  >
                    <PlusIcon size={13} />
                  </button>
                  <button
                    class="icon-action"
                    type="button"
                    title="Edit selected record"
                    disabled={!accessReady || !selectedRecord || !supportsEdit(currentResource)}
                    on:click={openEditEditor}
                  >
                    <PencilIcon size={13} />
                  </button>
                  <button
                    class="icon-action danger"
                    type="button"
                    title="Delete selected record"
                    disabled={!accessReady || !selectedRecord || !supportsDelete(currentResource)}
                    on:click={askDeleteRecord}
                  >
                    <Trash2Icon size={13} />
                  </button>
                </div>
              </div>

              {#if resourceError}
                <div class="inline-warning">{resourceError}</div>
              {/if}

              {#if !accessReady}
                <div class="locked-layout">
                  <section class="panel locked-panel">
                    <div class="panel-head compact">
                      <div>
                        <p class="card-kicker">Access required</p>
                        <h2>
                          {currentSectionMeta.label} needs verified RouterOS access
                        </h2>
                      </div>
                    </div>
                    <p class="locked-copy">
                      Save or reuse a working RouterOS login first. After
                      verification, this section opens as a live editable table
                      instead of a static inspector.
                    </p>
                  </section>

                  <section class="panel setup-panel">
                    {#if showingSavedWinboxValidation}
                      <div class="setup-grid single">
                        <div class="setup-box">
                          <div class="panel-head compact">
                            <div>
                              <p class="card-kicker">Saved login</p>
                              <h2>Attaching saved Winbox account</h2>
                            </div>
                          </div>
                          <p class="locked-copy">
                            A working Winbox account is already stored for this
                            MikroTik. The dashboard is reusing it automatically.
                          </p>
                          <div class="metric-stack">
                            <div class="metric-row">
                              <span>Preferred user</span>
                              <strong>{capability.preferred_username || "Unknown"}</strong>
                            </div>
                            <div class="metric-row">
                              <span>Transport</span>
                              <strong>{apiEndpoint}</strong>
                            </div>
                            <div class="metric-row">
                              <span>Status</span>
                              <strong>Validating saved access…</strong>
                            </div>
                          </div>
                        </div>
                      </div>
                    {:else if showManualAccessForm}
                      <div class="setup-grid single">
                        <form
                          class="setup-box"
                          on:submit|preventDefault={() => verifyAndSaveAccess(false)}
                        >
                        <div class="panel-head compact">
                          <div>
                            <p class="card-kicker">Manual login</p>
                            <h2>Dedicated API user</h2>
                          </div>
                        </div>
                        <div class="setup-form-grid">
                          <label>
                            <span>Username</span>
                            <input
                              value={setupUsername}
                              on:input={handleSetupUsernameInput}
                              placeholder="admin"
                            />
                          </label>
                          <label>
                            <span>Password</span>
                            <input
                              type="password"
                              value={setupPassword}
                              on:input={handleSetupPasswordInput}
                              placeholder="RouterOS password"
                            />
                          </label>
                          <label>
                            <span>Port</span>
                            <input
                              type="number"
                              min="1"
                              max="65535"
                              value={setupPort}
                              on:input={handleSetupPortInput}
                            />
                          </label>
                          <label>
                            <span>Transport</span>
                            <button
                              type="button"
                              class="toggle-button"
                              class:active={setupUseTls}
                              on:click={() => {
                                tlsTouched = true;
                                setupUseTls = !setupUseTls;
                              }}
                            >
                              {transportLabel()}
                            </button>
                          </label>
                        </div>
                        <div class="setup-actions">
                          <small>
                            Saved on this MikroTik peer and reused on future opens.
                          </small>
                          <button
                            class="small-button primary"
                            type="submit"
                            disabled={pendingMutation === "configure_access" || state.isSavingAccess}
                          >
                            {state.isSavingAccess ? "Verifying..." : "Verify & Save"}
                          </button>
                        </div>
                        </form>
                      </div>
                    {/if}
                  </section>
                </div>
              {:else}
                <div class="resource-layout" class:has-scope={scopeNodes.length > 0}>
                  {#if scopeNodes.length > 0}
                    <aside class="panel navigator-panel">
                      <div class="panel-head compact">
                        <div>
                          <p class="card-kicker">Navigator</p>
                          <h2>{currentSectionMeta.label} tree</h2>
                        </div>
                      </div>

                      <div class="scope-list">
                        {#each scopeNodes as node}
                          <button
                            type="button"
                            class="scope-node"
                            class:active={activeScopeKey === node.key}
                            style={`--depth:${node.depth};`}
                            on:click={() => {
                              activeScopeKey = node.key;
                              searchQuery = "";
                              selectedRecordId = "";
                            }}
                          >
                            <span class="scope-node-copy">
                              <strong>{node.label}</strong>
                              {#if node.hint}
                                <small>{node.hint}</small>
                              {/if}
                            </span>
                            <span class="scope-node-count">{node.count}</span>
                          </button>
                        {/each}
                      </div>
                    </aside>
                  {/if}

                  <section class="panel list-panel">
                    <div class="panel-head compact">
                      <div>
                        <p class="card-kicker">Records</p>
                        <h2>{currentSectionMeta.label}</h2>
                      </div>
                      <span class="panel-caption">
                        {#if currentResource}
                          {sectionCountMap[currentResource] ?? 0} total
                        {/if}
                      </span>
                    </div>

                    {#if resourceLoading && filteredRecords.length === 0}
                      <div class="table-skeleton">
                        {#each tableSkeletonRows as row}
                          <div class="skeleton-row" aria-hidden="true">
                            <span class="skeleton-cell id"></span>
                            {#each visibleColumns.length ? visibleColumns : ["a", "b", "c", "d"] as _}
                              <span class="skeleton-cell"></span>
                            {/each}
                          </div>
                        {/each}
                      </div>
                    {:else if filteredRecords.length === 0}
                      <div class="pane-empty">
                        <p>No {currentSectionMeta.label.toLowerCase()} records found.</p>
                      </div>
                    {:else}
                      <div class="table-wrap">
                        <table class="record-table">
                          <thead>
                            <tr>
                              <th>ID</th>
                              {#each visibleColumns as column}
                                <th>{formatFieldLabel(column)}</th>
                              {/each}
                            </tr>
                          </thead>
                          <tbody>
                            {#each filteredRecords as record}
                              <tr
                                class:selected={record.id === selectedRecordId}
                                on:click={() => (selectedRecordId = record.id)}
                              >
                                <td class="record-id">{displayRecordId(record)}</td>
                                {#each visibleColumns as column}
                                  <td>{valueSummary(record, column)}</td>
                                {/each}
                              </tr>
                            {/each}
                          </tbody>
                        </table>
                      </div>
                    {/if}
                  </section>

                  <section class="panel inspector-panel">
                    <div class="panel-head compact">
                      <div>
                        <p class="card-kicker">Inspector</p>
                        <h2>
                          {selectedRecord
                            ? `Record ${displayRecordId(selectedRecord)}`
                            : "No record selected"}
                        </h2>
                      </div>
                      <div class="toolbar-icons">
                        <button
                          class="icon-action"
                          type="button"
                          title="Edit selected record"
                          disabled={!selectedRecord || !supportsEdit(currentResource)}
                          on:click={openEditEditor}
                        >
                          <PencilIcon size={13} />
                        </button>
                        <button
                          class="icon-action danger"
                          type="button"
                          title="Delete selected record"
                          disabled={!selectedRecord || !supportsDelete(currentResource)}
                          on:click={askDeleteRecord}
                        >
                          <Trash2Icon size={13} />
                        </button>
                      </div>
                    </div>

                    {#if selectedRecord}
                      <div class="detail-grid">
                        {#each recordEntries as item}
                          <div class="detail-item">
                            <span>{formatFieldLabel(item.key)}</span>
                            <strong
                              class:code={item.value.length > 32 || item.value.includes("\n")}
                            >
                              {renderValue(item.value)}
                            </strong>
                          </div>
                        {/each}
                      </div>
                    {:else}
                      <div class="pane-empty">
                        <p>
                          Select a record from the live table to inspect or edit
                          it.
                        </p>
                      </div>
                    {/if}
                  </section>
                </div>
              {/if}
            </section>
          {/if}
        </div>
      </main>
    </div>

    {#if editorMode}
      <div class="modal-scrim">
        <div class="editor-modal">
          <div class="modal-head">
            <div>
              <p class="card-kicker">
                {editorMode === "add" ? "Create record" : "Edit record"}
              </p>
              <h3>
                {editorMode === "add"
                  ? `New ${currentSectionMeta.label}`
                  : `${currentSectionMeta.label} #${
                      selectedRecord ? displayRecordId(selectedRecord) : ""
                    }`}
              </h3>
              <p class="modal-subtitle">
                {editorRows.length} guided field{editorRows.length === 1 ? "" : "s"}
                ready for this live RouterOS transaction.
              </p>
            </div>
            <button class="icon-action" type="button" on:click={closeEditor}>
              <XIcon size={14} />
            </button>
          </div>

          {#if editorError}
            <div class="inline-warning">{editorError}</div>
          {/if}

          <div class="editor-table-wrap">
            {#if editorMissingKeys.length > 0}
              <div class="editor-catalog">
                <div class="editor-catalog-copy">
                  <p class="card-kicker">Suggested fields</p>
                  <strong>Add the common RouterOS fields for this section</strong>
                </div>
                <div class="editor-chip-list">
                  {#each editorMissingKeys as key}
                    <button
                      class="field-chip"
                      type="button"
                      on:click={() => addSuggestedField(key)}
                    >
                      + {formatFieldLabel(key)}
                    </button>
                  {/each}
                </div>
              </div>
            {/if}

            <datalist id="routeros-editor-field-keys">
              {#each editorSuggestedKeys as key}
                <option value={key}></option>
              {/each}
            </datalist>

            <div class="editor-field-list">
              {#each editorRows as row}
                <div class="editor-field-row">
                  <label class="editor-input-group">
                    <span class="editor-input-label">Field</span>
                    <input
                      value={row.key}
                      list="routeros-editor-field-keys"
                      placeholder="field-name"
                      on:input={(event) =>
                        updateEditorField(row.uid, "key", inputValue(event))}
                    />
                  </label>

                  <label class="editor-input-group value">
                    <span class="editor-input-label">Value</span>
                    {#if usesEnumInput(row)}
                      <select
                        value={row.value}
                        on:change={(event) =>
                          updateEditorField(row.uid, "value", inputValue(event))}
                      >
                        <option value="">Select value</option>
                        {#each fieldEnumOptions(row) as option}
                          <option value={option}>{option}</option>
                        {/each}
                      </select>
                    {:else if usesTextarea(row)}
                      <textarea
                        rows="3"
                        value={row.value}
                        placeholder={placeholderForRow(row)}
                        on:input={(event) =>
                          updateEditorField(row.uid, "value", inputValue(event))}
                      ></textarea>
                    {:else}
                      <input
                        type={inputTypeForRow(row)}
                        value={row.value}
                        list={`routeros-editor-values-${row.uid}`}
                        placeholder={placeholderForRow(row)}
                        on:input={(event) =>
                          updateEditorField(row.uid, "value", inputValue(event))}
                      />
                      {#if fieldAutocompleteValues(row).length > 0}
                        <datalist id={`routeros-editor-values-${row.uid}`}>
                          {#each fieldAutocompleteValues(row) as option}
                            <option value={option}></option>
                          {/each}
                        </datalist>
                      {/if}
                    {/if}

                    {#if hintForRow(row)}
                      <small class="editor-input-hint">{hintForRow(row)}</small>
                    {/if}
                  </label>

                  <button
                    class="icon-action"
                    type="button"
                    on:click={() => removeEditorField(row.uid)}
                    title="Remove field"
                  >
                    <XIcon size={12} />
                  </button>
                </div>
              {/each}
            </div>
          </div>

          <div class="modal-footer">
            <button class="small-button" type="button" on:click={addEditorField}>
              Add field
            </button>
            <div class="modal-footer-actions">
              <button class="small-button" type="button" on:click={closeEditor}>
                Cancel
              </button>
              <button class="small-button primary" type="button" on:click={submitEditor}>
                {pendingMutation === "add" || pendingMutation === "update"
                  ? "Saving..."
                  : "Save"}
              </button>
            </div>
          </div>
        </div>
      </div>
    {/if}

    {#if showDeleteConfirm}
      <div class="modal-scrim">
        <div class="confirm-modal">
          <p class="card-kicker">Delete record</p>
          <h3>Remove RouterOS record #{deleteTargetLabel || deleteTargetId}?</h3>
          <p>This change is sent directly to the live RouterOS session.</p>
          <div class="confirm-actions">
            <button
              class="small-button"
              type="button"
              on:click={() => (showDeleteConfirm = false)}
            >
              Cancel
            </button>
            <button class="small-button danger" type="button" on:click={confirmDeleteRecord}>
              {pendingMutation === "delete" ? "Deleting..." : "Delete"}
            </button>
          </div>
        </div>
      </div>
    {/if}
  </div>
</AppWindow>

<style lang="scss">
  :global(.app-window) {
    .window-content {
      background: #11161d;
    }
  }

  .routeros-shell {
    --surface-0: #0c131a;
    --surface-1: #111922;
    --surface-2: #13202b;
    --surface-3: #182836;
    --border-1: rgba(112, 142, 170, 0.22);
    --border-2: rgba(112, 142, 170, 0.34);
    --text-1: #e4eef8;
    --text-2: #a6bacd;
    --accent: #2b7cff;
    --accent-soft: rgba(43, 124, 255, 0.18);
    --good: #2fc275;
    --warn: #d38a37;
    --danger: #cf5660;
    position: relative;
    height: 100%;
    min-height: 0;
    overflow: hidden;
    color: var(--text-1);
    font-size: 12px;
    line-height: 1.35;
  }

  .shell-layout {
    display: flex;
    height: 100%;
    min-height: 0;
  }

  .eyebrow,
  .card-kicker {
    margin: 0;
    font-size: 10px;
    font-weight: 700;
    line-height: 1.2;
    letter-spacing: 0.16em;
    text-transform: uppercase;
    color: #87a0b8;
  }

  .device-rail,
  .command-header,
  .panel,
  .banner,
  .empty-state,
  .editor-modal,
  .confirm-modal {
    border: 1px solid var(--border-1);
    background: linear-gradient(
      180deg,
      rgba(9, 18, 26, 0.96),
      rgba(13, 24, 34, 0.96)
    );
  }

  .device-rail {
    width: 274px;
    min-width: 274px;
    padding: 12px;
    border-right-width: 1px;
    display: flex;
    flex-direction: column;
    gap: 12px;
    overflow: auto;
    transition:
      width 0.16s ease,
      min-width 0.16s ease,
      transform 0.16s ease;
  }

  .device-rail.collapsed {
    width: 72px;
    min-width: 72px;
    padding-inline: 10px;
  }

  .rail-header {
    display: flex;
    align-items: center;
    gap: 10px;
    min-height: 42px;
  }

  .device-rail.collapsed .rail-header {
    justify-content: center;
  }

  .rail-brand-mark,
  .nav-item-icon {
    width: 34px;
    height: 34px;
    display: inline-flex;
    align-items: center;
    justify-content: center;
    border: 1px solid var(--border-1);
    background: #101925;
    color: #9cc7ff;
    flex: 0 0 auto;
  }

  .rail-brand-mark {
    padding: 5px;
  }

  .rail-brand-logo {
    width: 18px;
    height: 18px;
    display: block;
    object-fit: contain;
  }

  .rail-brand-copy strong,
  .device-card-head h2,
  .command-title h1,
  .panel-head h2 {
    margin: 0;
    color: var(--text-1);
  }

  .rail-brand-copy strong {
    display: block;
    font-size: 14px;
  }

  .rail-device-card {
    border: 1px solid var(--border-1);
    background: #0d1822;
    padding: 12px;
    display: grid;
    gap: 10px;
  }

  .device-card-head {
    display: flex;
    align-items: flex-start;
    justify-content: space-between;
    gap: 10px;
  }

  .device-card-head h2 {
    font-size: clamp(15px, 1.8vw, 22px);
    line-height: 1.05;
    word-break: break-word;
  }

  .device-summary {
    margin: 0;
    color: var(--text-2);
    font-size: 12px;
  }

  .device-meta-grid {
    display: grid;
    grid-template-columns: repeat(2, minmax(0, 1fr));
    gap: 8px;
  }

  .device-meta-grid div,
  .hero-stat,
  .info-item,
  .inventory-item {
    display: grid;
    gap: 4px;
    min-width: 0;
  }

  .device-meta-grid span,
  .hero-stat span,
  .info-item span,
  .metric-row span,
  .progress-head span,
  .panel-caption,
  .inventory-item span {
    color: var(--text-2);
    font-size: 11px;
  }

  .device-meta-grid strong,
  .hero-stat strong,
  .info-item strong,
  .metric-row strong,
  .progress-head strong,
  .inventory-item strong {
    color: var(--text-1);
    font-size: 12px;
    font-weight: 700;
    word-break: break-word;
  }

  .live-pill,
  .status-chip {
    display: inline-flex;
    align-items: center;
    gap: 6px;
    min-height: 26px;
    padding: 0 10px;
    border: 1px solid var(--border-2);
    background: rgba(17, 31, 45, 0.62);
    font-size: 11px;
    font-weight: 700;
    letter-spacing: 0.04em;
    text-transform: uppercase;
    white-space: nowrap;
  }

  .live-pill.online,
  .status-chip.success {
    color: #b8f3ce;
    border-color: rgba(47, 194, 117, 0.34);
    background: rgba(17, 70, 44, 0.6);
  }

  .status-chip.warn {
    color: #ffd2a2;
    border-color: rgba(211, 138, 55, 0.34);
    background: rgba(74, 45, 17, 0.56);
  }

  .status-chip.neutral {
    color: #d2ddea;
  }

  .rail-nav {
    display: flex;
    flex-direction: column;
    gap: 6px;
    min-height: 0;
    overflow: auto;
  }

  .nav-item {
    width: 100%;
    display: grid;
    grid-template-columns: 34px minmax(0, 1fr) auto;
    align-items: center;
    gap: 10px;
    padding: 8px 10px;
    border: 1px solid transparent;
    background: transparent;
    color: inherit;
    text-align: left;
    cursor: pointer;
  }

  .device-rail.collapsed .nav-item {
    grid-template-columns: 1fr;
    justify-items: center;
    padding-inline: 0;
  }

  .nav-item.active {
    border-color: rgba(78, 136, 217, 0.4);
    background: rgba(32, 65, 101, 0.52);
  }

  .nav-item-copy {
    min-width: 0;
  }

  .nav-item-copy strong {
    display: block;
    font-size: 13px;
  }

  .nav-item-copy small {
    display: block;
    margin-top: 2px;
    color: var(--text-2);
    font-size: 11px;
    line-height: 1.3;
  }

  .nav-item-count,
  .scope-node-count {
    min-width: 22px;
    padding: 1px 5px;
    border: 1px solid rgba(112, 142, 170, 0.2);
    background: rgba(15, 24, 34, 0.74);
    color: #97b2ce;
    font-size: 10px;
    font-weight: 700;
    text-align: center;
    font-variant-numeric: tabular-nums;
  }

  .nav-item-count.pending {
    color: #7aa8dd;
  }

  .workspace-shell {
    flex: 1;
    min-width: 0;
    min-height: 0;
    display: flex;
    flex-direction: column;
  }

  .command-header {
    display: grid;
    grid-template-columns: auto minmax(0, 1fr) auto;
    align-items: center;
    gap: 12px;
    padding: 12px 16px;
    border-bottom: 1px solid var(--border-1);
  }

  .shell-toggle,
  .icon-action,
  .small-button,
  .text-button,
  .toggle-button {
    border: 1px solid var(--border-2);
    background: #122030;
    color: var(--text-1);
    cursor: pointer;
    transition:
      background 0.12s ease,
      border-color 0.12s ease;
  }

  .shell-toggle,
  .icon-action {
    width: 32px;
    height: 32px;
    display: inline-flex;
    align-items: center;
    justify-content: center;
    padding: 0;
  }

  .command-copy {
    min-width: 0;
  }

  .command-copy p:last-child,
  .command-title span {
    margin: 0;
    color: var(--text-2);
  }

  .command-title {
    display: flex;
    align-items: baseline;
    gap: 10px;
    flex-wrap: wrap;
  }

  .command-title h1 {
    font-size: clamp(19px, 2vw, 28px);
    line-height: 1;
  }

  .command-actions {
    display: flex;
    align-items: center;
    justify-content: flex-end;
    gap: 8px;
    flex-wrap: wrap;
  }

  .workspace-body {
    flex: 1;
    min-height: 0;
    overflow: auto;
    display: grid;
    gap: 12px;
    padding: 12px;
    background: linear-gradient(180deg, #0f1720, #101723);
  }

  .banner,
  .empty-state,
  .panel {
    padding: 12px;
  }

  .banner {
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: 12px;
  }

  .banner.error {
    border-color: rgba(207, 86, 96, 0.35);
    background: rgba(63, 24, 30, 0.76);
  }

  .banner.notice {
    border-color: rgba(70, 125, 197, 0.35);
    background: rgba(20, 42, 67, 0.76);
  }

  .banner p,
  .banner strong,
  .empty-state p,
  .empty-state h2 {
    margin: 0;
  }

  .banner-actions,
  .panel-actions,
  .confirm-actions,
  .modal-footer-actions,
  .toolbar-icons {
    display: flex;
    align-items: center;
    gap: 8px;
  }

  .empty-state {
    display: flex;
    align-items: center;
    gap: 12px;
  }

  .overview-grid {
    display: grid;
    grid-template-columns: repeat(12, minmax(0, 1fr));
    gap: 12px;
  }

  .hero-panel {
    grid-column: span 6;
  }

  .metric-panel,
  .inventory-panel,
  .identity-panel,
  .access-panel {
    grid-column: span 3;
  }

  .panel-head {
    display: flex;
    align-items: flex-start;
    justify-content: space-between;
    gap: 10px;
    margin-bottom: 12px;
  }

  .panel-head p,
  .panel-head h2,
  .panel-head small {
    margin: 0;
  }

  .panel-head.compact h2 {
    font-size: 16px;
  }

  .panel-caption {
    white-space: nowrap;
  }

  .hero-grid,
  .info-grid,
  .inventory-grid {
    display: grid;
    gap: 10px;
  }

  .hero-grid,
  .info-grid {
    grid-template-columns: repeat(2, minmax(0, 1fr));
  }

  /* Inventory cards are smaller (just label + count) so we can pack
     more per row. auto-fit + minmax lets the panel scale from 2 cols
     on narrow widths up to 4 cols on a wide overview, dropping the
     8-card block from 4 rows to 2. */
  .inventory-grid {
    grid-template-columns: repeat(auto-fit, minmax(120px, 1fr));
  }

  .inventory-item {
    padding: 10px;
    border: 1px solid var(--border-1);
    border-radius: 8px;
    background: rgba(17, 28, 40, 0.72);
    text-align: left;
    color: inherit;
    cursor: pointer;
    transition:
      border-color 0.12s ease,
      transform 0.12s ease;
  }

  .inventory-item:hover {
    border-color: rgba(101, 162, 255, 0.45);
    transform: translateY(-1px);
  }

  .inventory-item strong {
    font-size: 18px;
  }

  .metric-stack,
  .access-summary {
    display: grid;
    gap: 10px;
  }

  .metric-row,
  .progress-head {
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: 12px;
  }

  .traffic-bars {
    display: grid;
    gap: 10px;
    margin-top: 4px;
  }

  .progress-head + .progress-track,
  .traffic-bars > div {
    display: grid;
    gap: 5px;
  }

  .progress-track {
    height: 6px;
    border: 1px solid rgba(90, 117, 144, 0.14);
    background: #0b1119;
    overflow: hidden;
  }

  .progress-track span {
    display: block;
    height: 100%;
    background: var(--accent);
  }

  .progress-track span.tx {
    background: #58b8ff;
  }

  .progress-track span.warn {
    background: #7bc4ff;
  }

  .progress-track span.danger {
    background: #6f8cff;
  }

  .section-shell,
  .locked-layout,
  .setup-grid {
    display: grid;
    gap: 12px;
  }

  .section-toolbar {
    display: grid;
    grid-template-columns: minmax(0, 1fr) auto auto;
    align-items: center;
    gap: 10px;
  }

  .search-box {
    display: flex;
    align-items: center;
    gap: 8px;
    min-height: 34px;
    padding: 0 10px;
    border: 1px solid var(--border-1);
    background: #0c141d;
  }

  .search-box input,
  .setup-form-grid input,
  .setup-form-grid select,
  .editor-field-row input,
  .editor-field-row select,
  .editor-field-row textarea {
    width: 100%;
    min-height: 32px;
    padding: 0 10px;
    border: 1px solid var(--border-1);
    background: #09121b;
    color: var(--text-1);
    font: inherit;
  }

  .search-box input {
    min-height: 0;
    padding: 0;
    border: 0;
    background: transparent;
    outline: none;
  }

  .editor-field-row textarea {
    min-height: 72px;
    padding-block: 8px;
    resize: vertical;
  }

  .toolbar-meta {
    display: flex;
    align-items: center;
    gap: 10px;
    color: var(--text-2);
    white-space: nowrap;
  }

  .loading-inline {
    color: #7ab0ff;
  }

  .locked-copy {
    margin: 0;
    max-width: 76ch;
    color: var(--text-2);
  }

  .setup-grid {
    grid-template-columns: repeat(2, minmax(0, 1fr));
  }

  .setup-grid.single {
    grid-template-columns: minmax(0, 1fr);
  }

  .setup-box {
    display: grid;
    gap: 12px;
  }

  .setup-form-grid {
    display: grid;
    grid-template-columns: repeat(2, minmax(0, 1fr));
    gap: 10px;
  }

  .setup-form-grid label {
    display: grid;
    gap: 6px;
    color: var(--text-2);
    font-size: 11px;
  }

  .small-button,
  .text-button,
  .toggle-button {
    min-height: 32px;
    padding: 0 12px;
    font: inherit;
    font-weight: 700;
  }

  .small-button.primary,
  .text-button.primary,
  .toggle-button.active {
    background: var(--accent);
    border-color: rgba(43, 124, 255, 0.6);
    color: #f8fbff;
  }

  /* Promoted CTA — used in the Access panel when no credentials are
     configured. Same shape as .small-button.primary, but tuned to
     stand out against the Overview background without competing with
     destructive actions. */
  .small-button.primary-cta {
    background: linear-gradient(180deg, rgba(101, 162, 255, 0.96), rgba(54, 121, 230, 0.94));
    border-color: rgba(126, 184, 255, 0.55);
    color: #f8fbff;
    box-shadow: 0 4px 12px rgba(40, 96, 198, 0.3);
  }

  .small-button.danger,
  .icon-action.danger {
    color: #ffc7cc;
    border-color: rgba(207, 86, 96, 0.34);
    background: rgba(74, 24, 30, 0.36);
  }

  /* Visual demotion for "no data yet" / "pending" / "unknown" values
     so the eye is drawn to actual values. Used everywhere a string
     like "Pending", "Never", "Auto", "none", "Unknown" appears. */
  .pending-text {
    color: var(--widget-ink-muted, rgba(154, 170, 193, 0.62)) !important;
    font-weight: 500 !important;
    font-style: italic;
  }

  /* Empty state for the Device profile panel — replaces the previous
     blank rectangle with a structured fallback that explains why no
     identity fields are populated yet. */
  .profile-empty {
    display: flex;
    flex-direction: column;
    gap: 6px;
    padding: 18px 12px;
    border: 1px dashed rgba(167, 184, 214, 0.18);
    border-radius: 12px;
    background: rgba(11, 17, 29, 0.32);
  }

  .profile-empty-title {
    margin: 0;
    color: rgba(232, 239, 248, 0.92);
    font-size: 13px;
    font-weight: 700;
  }

  .profile-empty-hint {
    margin: 0;
    color: rgba(189, 203, 223, 0.7);
    font-size: 12px;
    line-height: 1.45;
  }

  .setup-actions {
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: 12px;
  }

  .setup-actions small {
    color: var(--text-2);
  }

  .resource-layout {
    display: grid;
    grid-template-columns: minmax(0, 1.2fr) minmax(340px, 0.8fr);
    gap: 12px;
    min-height: 0;
  }

  .resource-layout.has-scope {
    grid-template-columns: 220px minmax(0, 1.1fr) minmax(340px, 0.75fr);
  }

  .navigator-panel,
  .list-panel,
  .inspector-panel {
    min-height: 0;
    display: flex;
    flex-direction: column;
  }

  .scope-list {
    display: grid;
    gap: 4px;
    min-height: 0;
    overflow: auto;
  }

  .scope-node {
    width: 100%;
    display: grid;
    grid-template-columns: minmax(0, 1fr) auto;
    align-items: center;
    gap: 10px;
    padding: 8px 10px 8px calc(10px + (var(--depth) * 12px));
    border: 1px solid transparent;
    background: transparent;
    color: inherit;
    text-align: left;
    cursor: pointer;
  }

  .scope-node.active {
    border-color: rgba(78, 136, 217, 0.4);
    background: rgba(32, 65, 101, 0.42);
  }

  .scope-node-copy {
    min-width: 0;
  }

  .scope-node-copy strong {
    display: block;
    font-size: 12px;
  }

  .scope-node-copy small {
    display: block;
    margin-top: 2px;
    color: var(--text-2);
    font-size: 10px;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }

  .table-skeleton,
  .table-wrap {
    min-height: 0;
    overflow: auto;
    border: 1px solid var(--border-1);
    background: #0b131c;
  }

  .table-skeleton {
    display: grid;
    gap: 1px;
    padding: 8px;
  }

  .skeleton-row {
    display: grid;
    grid-template-columns: 120px repeat(4, minmax(0, 1fr));
    gap: 10px;
    padding: 6px 0;
  }

  .skeleton-cell {
    height: 12px;
    background: linear-gradient(
      90deg,
      rgba(38, 58, 80, 0.3),
      rgba(79, 109, 140, 0.42),
      rgba(38, 58, 80, 0.3)
    );
    background-size: 220% 100%;
    animation: shimmer 1.15s linear infinite;
  }

  .skeleton-cell.id {
    width: 96px;
  }

  .record-table {
    width: 100%;
    border-collapse: collapse;
    table-layout: fixed;
    font-size: 12px;
  }

  .record-table thead th {
    position: sticky;
    top: 0;
    z-index: 1;
    background: #121d29;
    color: #9ab1c6;
    font-size: 10px;
    letter-spacing: 0.08em;
    text-transform: uppercase;
  }

  .record-table th,
  .record-table td {
    padding: 8px 10px;
    border-bottom: 1px solid rgba(91, 119, 145, 0.16);
    text-align: left;
    white-space: nowrap;
    overflow: hidden;
    text-overflow: ellipsis;
  }

  .record-table tbody tr {
    cursor: pointer;
  }

  .record-table tbody tr.selected {
    background: rgba(35, 85, 139, 0.42);
  }

  .record-id {
    color: #b6d7ff;
    font-weight: 700;
    font-size: 11px;
    letter-spacing: 0;
    font-variant-numeric: tabular-nums;
  }

  .detail-grid {
    display: grid;
    grid-template-columns: repeat(2, minmax(0, 1fr));
    gap: 10px;
    min-height: 0;
    overflow: auto;
  }

  .detail-item {
    display: grid;
    gap: 5px;
    padding: 10px;
    border: 1px solid var(--border-1);
    background: #0d1721;
  }

  .detail-item span {
    color: var(--text-2);
    font-size: 11px;
    letter-spacing: 0.07em;
    text-transform: uppercase;
  }

  .detail-item strong {
    color: var(--text-1);
    font-size: 13px;
    line-height: 1.4;
    white-space: pre-wrap;
    word-break: break-word;
  }

  .detail-item strong.code {
    font-family: ui-monospace, SFMono-Regular, Menlo, monospace;
    font-size: 12px;
  }

  .pane-empty,
  .inline-warning {
    padding: 14px;
    border: 1px solid var(--border-1);
    background: rgba(12, 21, 31, 0.84);
    color: var(--text-2);
  }

  .inline-warning {
    color: #ffd0d6;
    border-color: rgba(207, 86, 96, 0.34);
    background: rgba(63, 24, 30, 0.62);
  }

  .modal-scrim {
    position: absolute;
    inset: 0;
    z-index: 20;
    display: flex;
    align-items: center;
    justify-content: center;
    padding: 18px;
    background: rgba(5, 10, 16, 0.72);
  }

  .editor-modal,
  .confirm-modal {
    width: min(960px, calc(100% - 18px));
    max-height: calc(100% - 18px);
    display: flex;
    flex-direction: column;
  }

  .confirm-modal {
    width: min(420px, calc(100% - 18px));
    padding: 16px;
    gap: 12px;
  }

  .modal-head,
  .modal-footer {
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: 12px;
    padding: 12px 14px;
    border-bottom: 1px solid var(--border-1);
  }

  .modal-footer {
    border-top: 1px solid var(--border-1);
    border-bottom: 0;
  }

  .modal-head h3,
  .modal-head p,
  .confirm-modal h3,
  .confirm-modal p {
    margin: 0;
  }

  .modal-subtitle {
    color: var(--text-2);
    font-size: 11px;
  }

  .editor-table-wrap {
    flex: 1;
    min-height: 0;
    overflow: auto;
    padding: 14px;
  }

  .editor-catalog {
    display: grid;
    gap: 10px;
    margin-bottom: 14px;
    padding: 12px;
    border: 1px solid var(--border-1);
    background: rgba(11, 18, 27, 0.84);
  }

  .editor-catalog-copy {
    display: grid;
    gap: 4px;
  }

  .editor-catalog-copy strong {
    font-size: 13px;
  }

  .editor-chip-list {
    display: flex;
    flex-wrap: wrap;
    gap: 8px;
  }

  .field-chip {
    min-height: 28px;
    padding: 0 10px;
    border: 1px solid var(--border-1);
    background: #0f1a25;
    color: #b6d7ff;
    font: inherit;
    font-weight: 700;
    cursor: pointer;
  }

  .editor-field-list {
    display: grid;
    gap: 8px;
  }

  .editor-field-row {
    display: grid;
    grid-template-columns: minmax(180px, 0.8fr) minmax(0, 1.2fr) 32px;
    gap: 8px;
    align-items: start;
  }

  .editor-input-group {
    display: grid;
    gap: 6px;
    min-width: 0;
  }

  .editor-input-group.value {
    align-content: start;
  }

  .editor-input-label {
    color: var(--text-2);
    font-size: 10px;
    font-weight: 700;
    letter-spacing: 0.08em;
    text-transform: uppercase;
  }

  .editor-input-hint {
    color: #8aa6c2;
    font-size: 10px;
    line-height: 1.35;
  }

  .mobile-scrim {
    position: absolute;
    inset: 0;
    z-index: 5;
    border: 0;
    background: rgba(0, 0, 0, 0.45);
  }

  @keyframes shimmer {
    0% {
      background-position: 200% 0;
    }
    100% {
      background-position: -20% 0;
    }
  }

  @media (max-width: 1240px) {
    .overview-grid {
      grid-template-columns: repeat(6, minmax(0, 1fr));
    }

    .hero-panel,
    .metric-panel,
    .inventory-panel,
    .identity-panel,
    .access-panel {
      grid-column: span 3;
    }

    .resource-layout,
    .resource-layout.has-scope {
      grid-template-columns: minmax(0, 1fr);
    }

    .navigator-panel {
      max-height: 220px;
    }
  }

  @media (max-width: 920px) {
    .routeros-shell {
      min-height: 0;
      height: 100%;
    }

    .shell-layout,
    .workspace-shell {
      min-height: 0;
      height: 100%;
    }

    .device-rail {
      position: absolute;
      inset: 0 auto 0 0;
      z-index: 10;
      transform: translateX(-104%);
      width: min(320px, calc(100% - 44px));
      min-width: 0;
    }

    .device-rail.mobile-open {
      transform: translateX(0);
    }

    .device-rail.collapsed {
      width: min(320px, calc(100% - 44px));
      min-width: 0;
      padding-inline: 12px;
    }

    .command-header {
      grid-template-columns: auto minmax(0, 1fr);
      align-items: start;
    }

    .command-actions {
      grid-column: 1 / -1;
      justify-content: flex-start;
    }

    .workspace-body {
      padding-bottom: calc(76px + env(safe-area-inset-bottom, 0px));
    }

    .overview-grid,
    .setup-grid,
    .hero-grid,
    .info-grid,
    .inventory-grid,
    .detail-grid,
    .setup-form-grid {
      grid-template-columns: 1fr;
    }

    .section-toolbar {
      grid-template-columns: 1fr;
      align-items: stretch;
    }

    .toolbar-meta,
    .toolbar-icons,
    .setup-actions,
    .banner,
    .modal-footer {
      flex-wrap: wrap;
      justify-content: flex-start;
    }

    .editor-field-row {
      grid-template-columns: 1fr;
    }

    .editor-chip-list {
      gap: 6px;
    }
  }
</style>
