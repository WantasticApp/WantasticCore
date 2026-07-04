<script lang="ts">
  import { onMount, onDestroy } from 'svelte';
  import { ToggleSwitch } from 'fluent-svelte';
  import {
    activeThing,
    appZIndexes,
    bringToFront,
    openedApps,
  } from '$store/store';
  import { scale } from "svelte/transition";
  import { peerStore, type Peer } from '$store/peer';
  import { wuspStore, snapshotSections, isWuspActive, searchSnapshot, type SnapshotField } from '$store/wusp';
  import { snapshotStore, type DeviceSnapshot } from '$store/snapshot';
  import { wsConnectionGeneration, wsStore } from '$store/websocket';
  import { _ } from '$store/i18n';
  import Titlebar from '$components/shared/Titlebar.svelte';
  import { isMobile } from '$store/ui';
  import { dispatch } from 'd3';
  import { draggable } from '@neodrag/svelte';
  import type { ComponentType } from 'svelte';
  import {
    Home as HomeIcon,
    Network as NetworkIcon,
    Database as DatabaseIcon,
    Wrench as WrenchIcon,
    Archive as ArchiveIcon,
  } from "$components/icons";

  // ── State ──────────────────────────────────────────────────────────────
  const APP_NAME = 'WuspDashboard';
  type TabId = 'overview' | 'network' | 'datamodel' | 'operations' | 'snapshots';
  let activeTab: TabId = 'overview';
  let isMaximized = false;
  let isMinimized = false;
  let windowEl: HTMLDivElement;
  let loadedPeerId = '';
  let subscribedPeerId = '';
  let lastRefreshGeneration = 0;

  $: selectedPeer = $peerStore.selectedPeer as Peer | null;
  // Server stores WUSP state keyed by WireGuard public_key, not DB id
  $: peerId = selectedPeer?.public_key || '';
  $: peerName = selectedPeer?.name || 'Device';
  $: accountId = selectedPeer?.account_id || '';
  $: state = $wuspStore;
  $: ds = state.deviceState;
  $: snapshot = state.snapshot;
  $: sections = $snapshotSections;
  $: wuspActive = $isWuspActive;
  $: snapState = $snapshotStore;

  // WUSP capability: known after getDeviceState returns.
  // If ds loaded successfully (even with wusp_enable=false), the device has WUSP data.
  // If fingerprint says Wantastic, also capable. Only show "not available"
  // when loading is done AND ds is null AND no fingerprint match.
  $: doneLoading = !state.isLoading;
  $: isWuspCapable = ds !== null || selectedPeer?.fingerprint?.vendor === 'Wantastic';

  $: zIndex = $appZIndexes[APP_NAME] || 100;

  // Always try to load WUSP state — we can't know capability until we check
  $: if (peerId && peerId !== loadedPeerId) {
    loadPeer(peerId);
  }

  $: if (
    peerId &&
    loadedPeerId === peerId &&
    $wsConnectionGeneration > 0 &&
    $wsConnectionGeneration !== lastRefreshGeneration
  ) {
    refreshAfterReconnect(peerId, $wsConnectionGeneration);
  }

  function handleSync() {
    if (peerId && accountId) {
      wuspStore.syncDevice(peerId, accountId);
    }
  }

  function loadPeer(nextPeerId: string) {
    if (subscribedPeerId && subscribedPeerId !== nextPeerId) {
      wsStore.unsubscribeFromWusp(subscribedPeerId);
      subscribedPeerId = '';
    }
    loadedPeerId = nextPeerId;
    wuspStore.reset();
    wuspStore.getDeviceState(nextPeerId);
    wsStore.subscribeToWusp(nextPeerId);
    subscribedPeerId = nextPeerId;
    lastRefreshGeneration = $wsConnectionGeneration;
  }

  async function refreshAfterReconnect(nextPeerId: string, generation: number) {
    lastRefreshGeneration = generation;
    wsStore.subscribeToWusp(nextPeerId);
    await wuspStore.getDeviceState(nextPeerId);
  }

  function handleFocus() {
    activeThing.set(APP_NAME);
    bringToFront(APP_NAME);
  }

  function switchTab(tab: TabId) {
    activeTab = tab;
    // Always reload with the 'wusp' protocol filter on entry — the snapshot
    // store may still hold cached results from elsewhere (e.g. Account →
    // Snapshots loaded an unfiltered list including MikroTik backups for
    // other peers). Hitting the server again is cheap and keeps the per-peer
    // view trustworthy.
    if (tab === 'snapshots') {
      snapshotStore.list('wusp');
    }
  }

  // Show all WUSP snapshots for the account (matching Account.svelte's
  // Snapshots tab behaviour). The `name` field is the user-entered snapshot
  // label, not the peer name, so the previous `name === peerName` filter
  // never matched anything created from this dashboard.
  // device_snapshots has no peer_id column, so per-peer scoping isn't
  // possible at the data layer; the user filters via search/date instead.
  let snapSearch = '';
  let snapFromDate = ''; // YYYY-MM-DD
  let snapToDate = '';   // YYYY-MM-DD

  $: filteredSnapshots = (() => {
    let rows = (snapState.snapshots || []).filter(
      (s) => (s.protocol || 'wusp') === 'wusp',
    );
    const q = snapSearch.trim().toLowerCase();
    if (q) {
      rows = rows.filter((s) =>
        (s.name || '').toLowerCase().includes(q) ||
        (s.serial_number || '').toLowerCase().includes(q) ||
        (s.product_class || '').toLowerCase().includes(q),
      );
    }
    if (snapFromDate) {
      const fromTs = Math.floor(new Date(snapFromDate).getTime() / 1000);
      rows = rows.filter((s) => (s.created_at || 0) >= fromTs);
    }
    if (snapToDate) {
      const toTs = Math.floor(new Date(snapToDate).getTime() / 1000) + 86399;
      rows = rows.filter((s) => (s.created_at || 0) <= toTs);
    }
    return rows;
  })();

  function clearSnapshotFilters() {
    snapSearch = '';
    snapFromDate = '';
    snapToDate = '';
  }

  // ── Live preview subscription lifecycle ───────────────────────────────
  // While the dashboard is open for a peer, ask the WS proxy to fan out the
  // peer's WUSP Notify events (ValueChange / OperationComplete / ...). The
  // proxy refcounts subscribers; the very first dashboard for a peer triggers
  // the canonical Subscribe RPC down to the agent. wuspStore.applyNotify
  // (in store/wusp.ts) consumes the pushed events and mutates the snapshot
  // in place so the user sees changes without clicking Sync.
  onDestroy(() => {
    if (subscribedPeerId) {
      wsStore.unsubscribeFromWusp(subscribedPeerId);
      subscribedPeerId = '';
    }
    wuspStore.reset();
  });

  // ── Helpers ────────────────────────────────────────────────────────────
  function formatBytes(bytes: number): string {
    if (!bytes || bytes <= 0) return '0 B';
    const units = ['B', 'KB', 'MB', 'GB'];
    let i = 0;
    let val = bytes;
    while (val >= 1024 && i < units.length - 1) {
      val /= 1024;
      i++;
    }
    return `${val.toFixed(i > 0 ? 1 : 0)} ${units[i]}`;
  }

  function getField(path: string): string {
    if (!snapshot || !Array.isArray(snapshot)) return '';
    const f = snapshot.find((s) => s.path === path);
    return f?.value || '';
  }

  function sf(path: string): string {
    if (!snapshot || !Array.isArray(snapshot)) return '';
    const f = snapshot.find((s: any) => s.path === path);
    return f?.value || '';
  }

  function handleClose() {
    $activeThing = '';
    $openedApps = $openedApps.filter((oa) => oa !== APP_NAME);
    dispatch('close');
  }

  function handleMaximize() {
    isMaximized = !isMaximized;
  }

  function handleReduce() {
    isMinimized = true;
    $activeThing = '';
  }

  function getSectionFields(
    prefix: string,
  ): { path: string; value: string; shortPath: string }[] {
    return snapshot
      .filter((f) => f.path.startsWith(prefix) && f.value)
      .map((f) => ({
        path: f.path,
        value: f.value,
        shortPath: f.path.replace(prefix, ''),
      }));
  }

  function formatUptime(seconds: number): string {
    const d = Math.floor(seconds / 86400);
    const h = Math.floor((seconds % 86400) / 3600);
    const m = Math.floor((seconds % 3600) / 60);
    if (d > 0) return `${d}d ${h}h ${m}m`;
    if (h > 0) return `${h}h ${m}m`;
    return `${m}m`;
  }

  function formatDate(ts: number): string {
    if (!ts) return 'N/A';
    const d = ts > 1e12 ? new Date(ts) : new Date(ts * 1000);
    return d.toLocaleString();
  }

  // ── Overview computed fields ────────────────────────────────────────
  $: identityFields = snapshot && [
    ['Manufacturer', ds?.manufacturer || sf('Device.DeviceInfo.Manufacturer')],
    ['Model', sf('Device.DeviceInfo.ModelName')],
    ['Product Class', ds?.product_class || sf('Device.DeviceInfo.ProductClass')],
    ['Software', ds?.software_version || sf('Device.DeviceInfo.SoftwareVersion')],
    ['Hardware', ds?.hardware_version || sf('Device.DeviceInfo.HardwareVersion')],
    ['Serial', ds?.serial_number || sf('Device.DeviceInfo.SerialNumber')],
    ['Hostname', sf('Device.DeviceInfo.HostName') || sf('Device.DeviceInfo.FriendlyName')],
    ['Description', sf('Device.DeviceInfo.Description')],
  ] as [string, string][] || [];

  $: memTotal = snapshot ? (Number(sf('Device.DeviceInfo.MemoryStatus.Total')) || 0) : 0;
  $: memFree = snapshot ? (Number(sf('Device.DeviceInfo.MemoryStatus.Free')) || 0) : 0;
  $: memUsed = memTotal > 0 ? memTotal - memFree : 0;
  $: memPct = memTotal > 0 ? Math.round((memUsed / memTotal) * 100) : 0;
  $: uptimeSec = snapshot ? (Number(sf('Device.DeviceInfo.UpTime')) || 0) : 0;
  $: cpuArch = snapshot ? sf('Device.DeviceInfo.Processor.1.Architecture') : '';
  $: cpuCores = snapshot ? sf('Device.DeviceInfo.Processor.1.MaxNumberOfEntries') : '';

  // ── Network interface extraction from snapshot ─────────────────────
  interface NetIface { name: string; status: string; type: string; mac: string; mtu: string; ips: string[]; }
  interface CellularInterface {
    name: string;
    status: string;
    accessTechnology: string;
    operator: string;
    imei: string;
    iccid: string;
    imsi: string;
    apn: string;
    rssi: string;
    rsrp: string;
    rsrq: string;
    sinr: string;
  }
  interface LocationFix {
    source: string;
    acquiredTime: string;
    latitude: string;
    longitude: string;
    altitude: string;
  }

  $: networkInterfaces = (() => {
    const ifaces: NetIface[] = [];
    if (!snapshot || !Array.isArray(snapshot)) return ifaces;
    const ifaceMap: Record<string, NetIface> = {};
    for (const f of snapshot) {
      const m = f.path.match(/^Device\.IP\.Interface\.(\d+)\.(.+)$/);
      if (!m) continue;
      const idx = m[1];
      const key = m[2];
      if (!ifaceMap[idx]) ifaceMap[idx] = { name: '', status: '', type: '', mac: '', mtu: '', ips: [] };
      const iface = ifaceMap[idx];
      if (key === 'Name') iface.name = f.value;
      else if (key === 'Status') iface.status = f.value;
      else if (key === 'Type') iface.type = f.value;
      else if (key === 'MACAddress') iface.mac = f.value;
      else if (key === 'MaxMTUSize') iface.mtu = f.value;
      else if (key.includes('IPAddress') && key.endsWith('.IPAddress')) iface.ips.push(f.value);
    }
    for (const [, iface] of Object.entries(ifaceMap).sort(([a], [b]) => Number(a) - Number(b))) {
      if (iface.name) ifaces.push(iface);
    }
    return ifaces;
  })();

  function signalQuality(rsrp: string, rssi: string): number {
    const raw = Number(rsrp || rssi);
    if (!Number.isFinite(raw) || raw === 0) return 0;
    if (raw >= -85) return 4;
    if (raw >= -95) return 3;
    if (raw >= -105) return 2;
    return 1;
  }

  function parseLocationDataObject(dataObject: string): { latitude: string; longitude: string; altitude: string } {
    const pos = dataObject.match(/<[^>]*pos[^>]*>\s*([^<]+)\s*<\/[^>]*pos>/i)?.[1] || '';
    const parts = pos.trim().split(/\s+/);
    return {
      latitude: parts[0] || '',
      longitude: parts[1] || '',
      altitude: parts[2] || '',
    };
  }

  $: locationFix = (() => {
    const parsed = parseLocationDataObject(sf('Device.DeviceInfo.Location.1.DataObject'));
    const source = sf('Device.DeviceInfo.Location.1.Source');
    const acquiredTime = sf('Device.DeviceInfo.Location.1.AcquiredTime');
    if (!source && !acquiredTime && !parsed.latitude && !parsed.longitude) return null;
    return {
      source,
      acquiredTime,
      ...parsed,
    } satisfies LocationFix;
  })();

  $: cellularInterfaces = (() => {
    const modems: CellularInterface[] = [];
    if (!snapshot || !Array.isArray(snapshot)) return modems;
    const map: Record<string, CellularInterface> = {};
    const ensure = (idx: string) => {
      map[idx] ||= {
        name: '',
        status: '',
        accessTechnology: '',
        operator: '',
        imei: '',
        iccid: '',
        imsi: '',
        apn: '',
        rssi: '',
        rsrp: '',
        rsrq: '',
        sinr: '',
      };
      return map[idx];
    };

    for (const f of snapshot) {
      const iface = f.path.match(/^Device\.Cellular\.Interface\.(\d+)\.(.+)$/);
      const usim = f.path.match(/^Device\.Cellular\.Interface\.(\d+)\.(?:SIM|USIM)\.(?:\d+\.)?(.+)$/);
      const apn = f.path.match(/^Device\.Cellular\.AccessPoint\.(\d+)\.(.+)$/);
      if (iface) {
        const modem = ensure(iface[1]);
        const key = iface[2];
        if (key === 'Name' || key === 'Alias') modem.name = f.value;
        else if (key === 'Status' || key === 'RegistrationStatus') modem.status = f.value;
        else if (key === 'CurrentAccessTechnology' || key === 'AccessTechnology') modem.accessTechnology = f.value;
        else if (key === 'NetworkInUse' || key === 'Operator' || key === 'PLMN') modem.operator = f.value;
        else if (key === 'IMEI') modem.imei = f.value;
        else if (key.endsWith('RSSI') || key === 'SignalStrength') modem.rssi = f.value;
        else if (key.endsWith('RSRP')) modem.rsrp = f.value;
        else if (key.endsWith('RSRQ')) modem.rsrq = f.value;
        else if (key.endsWith('SINR') || key.endsWith('SNR')) modem.sinr = f.value;
      }
      if (usim) {
        const modem = ensure(usim[1]);
        if (usim[2] === 'ICCID') modem.iccid = f.value;
        else if (usim[2] === 'IMSI') modem.imsi = f.value;
      }
      if (apn) {
        const modem = ensure(apn[1]);
        if (apn[2] === 'APN' || apn[2] === 'Name') modem.apn = f.value;
      }
    }

    for (const [, modem] of Object.entries(map).sort(([a], [b]) => Number(a) - Number(b))) {
      if (modem.name || modem.status || modem.accessTechnology || modem.imei || modem.iccid || modem.apn) {
        modems.push(modem);
      }
    }
    return modems;
  })();

  // ── Data Model search ──────────────────────────────────────────────
  let dmSearch = '';

  $: filteredSnapshot = !snapshot || !Array.isArray(snapshot) ? [] :
    dmSearch.length >= 2
      ? snapshot.filter(f =>
          f.path.toLowerCase().includes(dmSearch.toLowerCase()) ||
          f.value.toLowerCase().includes(dmSearch.toLowerCase()))
      : snapshot;

  $: filteredSections = (() => {
    const s: Record<string, typeof snapshot> = {};
    for (const field of filteredSnapshot) {
      const parts = field.path.split('.');
      const section = parts.length >= 3 ? parts.slice(0, 2).join('.') : parts[0];
      if (!s[section]) s[section] = [];
      s[section].push(field);
    }
    return s;
  })();

  // ── Inline editing ─────────────────────────────────────────────────
  let editingPath = '';
  let editValue = '';
  let editBooleanValue = false;

  function isBooleanValue(value: string): boolean {
    const normalized = value.trim().toLowerCase();
    return normalized === 'true' || normalized === 'false';
  }

  function normalizeBooleanValue(value: string): string {
    return value.trim().toLowerCase() === 'true' ? 'true' : 'false';
  }

  function startEdit(path: string, value: string) {
    editingPath = path;
    editValue = value;
    editBooleanValue = normalizeBooleanValue(value) === 'true';
  }

  function cancelEdit() {
    editingPath = '';
    editValue = '';
    editBooleanValue = false;
  }

  function updateEditBooleanValue(next: boolean) {
    editBooleanValue = next;
    editValue = next ? 'true' : 'false';
  }

  function handleBooleanEditChange(event: Event) {
    updateEditBooleanValue((event.currentTarget as HTMLInputElement).checked);
  }

  async function commitEdit() {
    if (!editingPath || !peerId) return;
    const valueToCommit = isBooleanValue(editValue)
      ? normalizeBooleanValue(editValue)
      : editValue;
    const success = await wuspStore.sendSet(peerId, [
      { path: editingPath, value: valueToCommit },
    ]);
    if (success) {
      const idx = snapshot.findIndex(f => f.path === editingPath);
      if (idx >= 0) {
        snapshot[idx] = { ...snapshot[idx], value: valueToCommit };
        snapshot = [...snapshot];
      }
    }
    cancelEdit();
  }

  // ── Operations state ──────────────────────────────────────────────
  let queryPath = '';
  let showSuggestions = false;
  let setPath = '';
  let setValue = '';
  let rebootConfirm = false;
  let factoryConfirm1 = false;
  let factoryConfirm2 = false;

  $: autocompleteSuggestions = queryPath.length >= 3 && Array.isArray(snapshot)
    ? snapshot
        .map(f => f.path)
        .filter(p => p.toLowerCase().includes(queryPath.toLowerCase()))
        .sort()
    : [];

  async function handleSendSet() {
    if (!setPath || !peerId) return;
    const success = await wuspStore.sendSet(peerId, [{ path: setPath, value: setValue }]);
    if (success) {
      setPath = '';
      setValue = '';
    }
  }

  async function handleReboot() {
    if (!rebootConfirm) { rebootConfirm = true; return; }
    await wuspStore.sendOperate(peerId, 'Device.Reboot()');
    rebootConfirm = false;
  }

  async function handleFactoryReset() {
    if (!factoryConfirm1) { factoryConfirm1 = true; return; }
    if (!factoryConfirm2) { factoryConfirm2 = true; return; }
    await wuspStore.sendOperate(peerId, 'Device.FactoryReset()');
    factoryConfirm1 = false;
    factoryConfirm2 = false;
  }

  // ── Snapshots state ───────────────────────────────────────────────
  let snapshotName = '';
  let isCreatingSnapshot = false;
  let provisioningId = '';
  let deletingId = '';
  let compareMode = false;
  let compareA: DeviceSnapshot | null = null;
  let compareB: DeviceSnapshot | null = null;

  $: compareDiff = (() => {
    if (!compareA || !compareB) return [];
    let fieldsA: SnapshotField[] = [];
    let fieldsB: SnapshotField[] = [];
    try {
      const rawA = compareA.device_snapshot;
      if (rawA) {
        let jsonA: string;
        if (typeof rawA === 'string') { try { jsonA = atob(rawA as string); } catch { jsonA = rawA as string; } }
        else { jsonA = new TextDecoder().decode(rawA); }
        fieldsA = JSON.parse(jsonA);
      }
    } catch { fieldsA = []; }
    try {
      const rawB = compareB.device_snapshot;
      if (rawB) {
        let jsonB: string;
        if (typeof rawB === 'string') { try { jsonB = atob(rawB as string); } catch { jsonB = rawB as string; } }
        else { jsonB = new TextDecoder().decode(rawB); }
        fieldsB = JSON.parse(jsonB);
      }
    } catch { fieldsB = []; }
    const mapA = new Map(fieldsA.map(f => [f.path, f.value]));
    const mapB = new Map(fieldsB.map(f => [f.path, f.value]));
    const allPaths = new Set([...mapA.keys(), ...mapB.keys()]);
    const diffs: { path: string; valueA: string; valueB: string; type: 'changed' | 'added' | 'removed' }[] = [];
    for (const path of [...allPaths].sort()) {
      const a = mapA.get(path);
      const b = mapB.get(path);
      if (a !== b) {
        diffs.push({
          path,
          valueA: a ?? '',
          valueB: b ?? '',
          type: a === undefined ? 'added' : b === undefined ? 'removed' : 'changed',
        });
      }
    }
    return diffs;
  })();

  async function handleCreateSnapshot() {
    if (!snapshotName.trim() || !peerId) return;
    isCreatingSnapshot = true;
    await snapshotStore.create(peerId, snapshotName.trim());
    snapshotName = '';
    isCreatingSnapshot = false;
    // Re-fetch so the canonical server-side row replaces the optimistic one
    // (matches Account → Snapshots which lists immediately on tab entry).
    await snapshotStore.list('wusp');
  }

  async function handleProvision(snapId: string) {
    provisioningId = snapId;
    await snapshotStore.provision(peerId, snapId);
    provisioningId = '';
  }

  async function handleDeleteSnapshot(snapId: string) {
    deletingId = snapId;
    await snapshotStore.delete(snapId);
    deletingId = '';
  }

  function selectForCompare(snap: DeviceSnapshot) {
    if (!compareA) { compareA = snap; }
    else if (!compareB) { compareB = snap; }
    else { compareA = snap; compareB = null; }
  }

  function parseSnapshotParamCount(snap: DeviceSnapshot): number {
    try {
      const raw = snap.device_snapshot;
      if (!raw) return 0;
      let jsonStr: string;
      if (typeof raw === 'string') { try { jsonStr = atob(raw as string); } catch { jsonStr = raw as string; } }
      else { jsonStr = new TextDecoder().decode(raw); }
      const arr = JSON.parse(jsonStr);
      return Array.isArray(arr) ? arr.length : 0;
    } catch { return 0; }
  }

  // Tab definitions — semantically-correct lucide icons:
  //   Overview → Home, Network → Network, Data Model → Database (the old
  //   calendar path was nonsense for a schema view), Operations → Wrench
  //   (better fit than the previous download-arrow), Snapshots → Archive.
  const tabs: { id: TabId; label: string; icon: ComponentType }[] = [
    { id: 'overview',   label: 'Overview',   icon: HomeIcon },
    { id: 'network',    label: 'Network',    icon: NetworkIcon },
    { id: 'datamodel',  label: 'Data Model', icon: DatabaseIcon },
    { id: 'operations', label: 'Operations', icon: WrenchIcon },
    { id: 'snapshots',  label: 'Snapshots',  icon: ArchiveIcon },
  ];
</script>

<!-- svelte-ignore a11y-click-events-have-key-events -->
<div
  class="wusp-dashboard activeShadow"
  class:maximized={isMaximized || $isMobile}
  class:minimized={isMinimized}
  style:z-index={zIndex}
  bind:this={windowEl}
  on:mousedown={handleFocus}
  on:touchstart={handleFocus}
  use:draggable={{
    handle: '.title-bar',
    disabled: isMaximized || $isMobile,
    bounds: 'body',
  }}
  transition:scale={{ duration: 200 }}
>
  <Titlebar
    title={`WUSP - ${peerName}`}
    color="#00ADB533"
    appName={'WuspDashboard'}
    canMaximize={!$isMobile}
    canReduce={!$isMobile}
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
      <path d="M3 3v18h18" />
      <path d="M18.7 8l-5.1 5.2-2.8-2.7L7 14.3" />
    </svg>
    <span class="appName pl-2">WUSP — {peerName}</span>
  </Titlebar>

  <div class="dashboard-content">
    {#if state.isLoading && !ds}
      <div class="loading-state">
        <div class="spinner" />
        <span>Loading device state...</span>
      </div>
    {:else if !ds}
      <div class="empty-state">
        <svg width="48" height="48" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.5" opacity="0.3">
          <path d="M12 2L2 7l10 5 10-5-10-5zM2 17l10 5 10-5M2 12l10 5 10-5"/>
        </svg>
        <p>No WUSP data available for this device.</p>
        <button class="btn-primary" on:click={handleSync}>Sync Now</button>
      </div>
    {:else}
      <!-- Tab bar -->
      <div class="tabs">
        {#each tabs as tab}
          <button
            class="tab-btn"
            class:active={activeTab === tab.id}
            on:click={() => switchTab(tab.id)}
            title={tab.label}
          >
            <svelte:component this={tab.icon} size={16} strokeWidth={2} />
            <span>{tab.label}</span>
          </button>
        {/each}
        <div class="tab-spacer"></div>
        <div class="tab-actions">
          {#if state.lastSyncTime}
            <span class="last-sync">Synced {formatDate(state.lastSyncTime)}</span>
          {/if}
          <button
            class="btn-sync"
            class:syncing={state.isSyncing}
            on:click={handleSync}
            disabled={state.isSyncing}
          >
            {#if state.isSyncing}
              <div class="spinner-sm" />
            {:else}
              <svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><path d="M1 4v6h6M23 20v-6h-6"/><path d="M20.49 9A9 9 0 005.64 5.64L1 10m22 4l-4.64 4.36A9 9 0 013.51 15"/></svg>
            {/if}
            Sync
          </button>
        </div>
      </div>

      <!-- Error banner -->
      {#if state.error}
        <div class="error-banner">
          <svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><circle cx="12" cy="12" r="10"/><path d="M12 8v4M12 16h.01"/></svg>
          <span>{state.error}</span>
          <button on:click={() => wuspStore.clearError()}>Dismiss</button>
        </div>
      {/if}
      {#if snapState.error}
        <div class="error-banner">
          <svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><circle cx="12" cy="12" r="10"/><path d="M12 8v4M12 16h.01"/></svg>
          <span>{snapState.error}</span>
          <button on:click={() => snapshotStore.clearError()}>Dismiss</button>
        </div>
      {/if}

      <!-- Tab content -->
      <div class="tab-content">

        <!-- ════════════════════════════════════════════════════════════ -->
        <!-- OVERVIEW TAB                                                -->
        <!-- ════════════════════════════════════════════════════════════ -->
        {#if activeTab === 'overview'}
          <div class="overview-grid">
            <!-- Device Identity -->
            <div class="card identity-card">
              <div class="card-header">
                <svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><rect x="2" y="3" width="20" height="14" rx="2"/><path d="M8 21h8M12 17v4"/></svg>
                <h3>Device Identity</h3>
              </div>
              <div class="field-grid three-col">
                {#each identityFields as [label, value]}
                  <div class="field">
                    <label>{label}</label>
                    <span class:mono={label === 'Serial'}>{value || 'N/A'}</span>
                  </div>
                {/each}
              </div>
            </div>

            <!-- System Resources -->
            <div class="card">
              <div class="card-header">
                <svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><path d="M22 12h-4l-3 9L9 3l-3 9H2"/></svg>
                <h3>System Resources</h3>
              </div>
              {#if memTotal > 0}
                <div class="resource-item">
                  <div class="resource-label">
                    <span>Memory</span>
                    <span class="resource-value">{formatBytes(memUsed * 1024)} / {formatBytes(memTotal * 1024)}</span>
                  </div>
                  <div class="progress-bar">
                    <div class="progress-fill" class:warn={memPct > 80} class:critical={memPct > 95} style="width: {memPct}%"></div>
                  </div>
                  <span class="resource-pct">{memPct}% used</span>
                </div>
              {:else}
                <div class="field-muted">Memory data not available</div>
              {/if}
              <div class="field-grid" style="margin-top: 12px;">
                {#if cpuArch}
                  <div class="field">
                    <label>CPU Architecture</label>
                    <span>{cpuArch}{cpuCores ? ` (${cpuCores} cores)` : ''}</span>
                  </div>
                {/if}
              </div>
            </div>

            <!-- WUSP Protocol Status -->
            <div class="card">
              <div class="card-header">
                <svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><path d="M12 2L2 7l10 5 10-5-10-5zM2 17l10 5 10-5M2 12l10 5 10-5"/></svg>
                <h3>WUSP Protocol</h3>
              </div>
              <div class="status-badges">
                <div class="badge" class:active={ds.wusp_enable}>{ds.wusp_enable ? 'Enabled' : 'Disabled'}</div>
                <div class="badge status-{(ds.wusp_status || 'unknown').toLowerCase()}">{ds.wusp_status || 'Unknown'}</div>
                <div class="badge version">v{ds.wusp_version || '?'}</div>
              </div>
              <div class="field-grid" style="margin-top: 12px;">
                <div class="field"><label>Snapshot Size</label><span>{(snapshot || []).length} parameters</span></div>
                <div class="field"><label>Max Payload</label><span>{getField('Device.WUSP.MaxControlPayload') || '1200'} bytes</span></div>
                <div class="field"><label>Tunnel Only</label><span>{getField('Device.WUSP.TunnelOnly') || 'true'}</span></div>
              </div>
            </div>

            <!-- Location -->
            <div class="card">
              <div class="card-header">
                <svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><path d="M20 10c0 5-8 12-8 12S4 15 4 10a8 8 0 1116 0z"/><circle cx="12" cy="10" r="3"/></svg>
                <h3>Location</h3>
              </div>
              {#if locationFix}
                <div class="field-grid">
                  <div class="field"><span class="field-label">Source</span><span>{locationFix.source || 'Unknown'}</span></div>
                  <div class="field"><span class="field-label">Acquired</span><span>{locationFix.acquiredTime ? new Date(locationFix.acquiredTime).toLocaleString() : 'N/A'}</span></div>
                  <div class="field"><span class="field-label">Latitude</span><span class="mono">{locationFix.latitude || 'N/A'}</span></div>
                  <div class="field"><span class="field-label">Longitude</span><span class="mono">{locationFix.longitude || 'N/A'}</span></div>
                  <div class="field"><span class="field-label">Altitude</span><span>{locationFix.altitude ? `${locationFix.altitude} m` : 'N/A'}</span></div>
                </div>
              {:else}
                <div class="field-muted">No GPS/location fields in the current snapshot.</div>
              {/if}
            </div>

            <!-- Cellular Telemetry -->
            <div class="card">
              <div class="card-header">
                <svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><path d="M6.5 20.5a12 12 0 0111 0"/><path d="M4 17a16 16 0 0116 0"/><path d="M2 13.5a20 20 0 0120 0"/><circle cx="12" cy="20" r="1"/></svg>
                <h3>Cellular Telemetry</h3>
              </div>
              {#if cellularInterfaces.length > 0}
                <div class="cellular-list">
                  {#each cellularInterfaces as modem}
                    <div class="cellular-row">
                      <div class="cellular-head">
                        <div>
                          <strong>{modem.name || 'Cellular modem'}</strong>
                          <span>{modem.accessTechnology || 'Radio'}{modem.operator ? ` · ${modem.operator}` : ''}</span>
                        </div>
                        <div class="signal-bars" title={`RSRP ${modem.rsrp || 'N/A'} RSSI ${modem.rssi || 'N/A'}`}>
                          {#each [1, 2, 3, 4] as bar}
                            <i class:lit={bar <= signalQuality(modem.rsrp, modem.rssi)} />
                          {/each}
                        </div>
                      </div>
                      <div class="field-grid cellular-grid">
                        <div class="field"><span class="field-label">Status</span><span class="badge small" class:active={modem.status === 'Up' || modem.status === 'Registered'}>{modem.status || 'Unknown'}</span></div>
                        <div class="field"><span class="field-label">RSRP</span><span>{modem.rsrp || 'N/A'}</span></div>
                        <div class="field"><span class="field-label">RSRQ</span><span>{modem.rsrq || 'N/A'}</span></div>
                        <div class="field"><span class="field-label">SINR</span><span>{modem.sinr || 'N/A'}</span></div>
                        <div class="field"><span class="field-label">APN</span><span>{modem.apn || 'N/A'}</span></div>
                        <div class="field"><span class="field-label">ICCID</span><span class="mono">{modem.iccid || 'N/A'}</span></div>
                      </div>
                    </div>
                  {/each}
                </div>
              {:else}
                <div class="field-muted">No cellular rows in the current snapshot. Sync includes Device.Cellular; older agents may need an update.</div>
              {/if}
            </div>

            <!-- Uptime -->
            <div class="card">
              <div class="card-header">
                <svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><circle cx="12" cy="12" r="10"/><path d="M12 6v6l4 2"/></svg>
                <h3>Uptime</h3>
              </div>
              <div class="uptime-display">
                <span class="uptime-value">{uptimeSec > 0 ? formatUptime(uptimeSec) : 'N/A'}</span>
                {#if uptimeSec > 0}
                  <span class="uptime-detail">{uptimeSec.toLocaleString()} seconds</span>
                {/if}
              </div>
              <div class="field-grid" style="margin-top: 12px;">
                <div class="field"><label>NTP</label>
                  <span class="badge small" class:active={getField('Device.Time.Enable') === 'true'}>{getField('Device.Time.Enable') === 'true' ? 'Synced' : 'Off'}</span>
                </div>
                <div class="field"><label>Timezone</label><span>{getField('Device.Time.LocalTimeZone') || 'N/A'}</span></div>
              </div>
            </div>
          </div>

        <!-- ════════════════════════════════════════════════════════════ -->
        <!-- NETWORK TAB                                                 -->
        <!-- ════════════════════════════════════════════════════════════ -->
        {:else if activeTab === 'network'}
          <div class="network-layout">
            <!-- IP Summary -->
            <div class="card">
              <div class="card-header">
                <svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><circle cx="12" cy="12" r="10"/><path d="M2 12h20M12 2a15.3 15.3 0 014 10 15.3 15.3 0 01-4 10 15.3 15.3 0 01-4-10 15.3 15.3 0 014-10z"/></svg>
                <h3>IP Configuration</h3>
              </div>
              <div class="status-badges">
                <div class="badge active">IPv4 {getField('Device.IP.IPv4Status') || 'Enabled'}</div>
                <div class="badge" class:active={getField('Device.IP.IPv6Enable') === 'true'}>IPv6 {getField('Device.IP.IPv6Status') || 'Unknown'}</div>
                <div class="badge version">{getField('Device.IP.InterfaceNumberOfEntries') || '0'} interfaces</div>
              </div>
            </div>

            <!-- Interface cards -->
            <div class="iface-grid">
              {#each networkInterfaces as iface}
                <div class="card iface-card">
                  <div class="iface-header">
                    <span class="iface-name">{iface.name}</span>
                    <span class="badge small" class:active={iface.status === 'Up'}>{iface.status}</span>
                  </div>
                  <div class="iface-details">
                    {#if iface.type}
                      <div class="field"><label>Type</label><span>{iface.type}</span></div>
                    {/if}
                    {#if iface.mac}
                      <div class="field"><label>MAC</label><span class="mono">{iface.mac}</span></div>
                    {/if}
                    {#if iface.mtu}
                      <div class="field"><label>MTU</label><span>{iface.mtu}</span></div>
                    {/if}
                  </div>
                  {#if iface.ips.length > 0}
                    <div class="iface-ips">
                      {#each iface.ips as ip}
                        <span class="ip-badge" class:v4={ip.includes('.')} class:v6={ip.includes(':')}>{ip}</span>
                      {/each}
                    </div>
                  {/if}
                </div>
              {/each}
            </div>

            {#if networkInterfaces.length === 0}
              <div class="dm-empty">No network interfaces found in snapshot data.</div>
            {/if}

            <!-- WiFi / Firewall sections from snapshot -->
            {#each Object.entries(sections) as [section, fields]}
              {#if section.startsWith('Device.WiFi') || section.startsWith('Device.Firewall')}
                <div class="card">
                  <div class="card-header">
                    <h3>{section.replace('Device.', '')}</h3>
                  </div>
                  <table class="param-table">
                    <thead><tr><th>Parameter</th><th>Value</th></tr></thead>
                    <tbody>
                      {#each fields as field}
                        <tr>
                          <td class="param-path" title={field.path}>{field.path.replace(section + '.', '')}</td>
                          <td class="param-value">{field.value || '-'}</td>
                        </tr>
                      {/each}
                    </tbody>
                  </table>
                </div>
              {/if}
            {/each}
          </div>

        <!-- ════════════════════════════════════════════════════════════ -->
        <!-- DATA MODEL TAB                                              -->
        <!-- ════════════════════════════════════════════════════════════ -->
        {:else if activeTab === 'datamodel'}
          <div class="datamodel-browser">
            <div class="dm-toolbar">
              <input
                type="text"
                class="dm-search"
                placeholder="Search parameters (path or value)..."
                bind:value={dmSearch}
              />
              <span class="dm-stats-inline">
                {(filteredSnapshot || []).length}/{(snapshot || []).length} params
              </span>
            </div>
            {#each Object.entries(filteredSections) as [section, fields]}
              <details class="dm-section" open={!!dmSearch}>
                <summary>
                  <span class="section-name">{section}</span>
                  <span class="section-count">{fields.length}</span>
                </summary>
                <table class="param-table">
                  <thead>
                    <tr><th>Path</th><th>Value</th><th class="col-access">Access</th><th class="col-action"></th></tr>
                  </thead>
                  <tbody>
                    {#each fields as field}
                      {@const writable = field.access === 'readWrite'}
                      <tr class:writable>
                        <td class="param-path" title={field.path}>
                          {field.path.replace(section + '.', '')}
                        </td>
                        <td class="param-value">
                          {#if editingPath === field.path}
                            {#if isBooleanValue(field.value)}
                              <div class="inline-bool-edit">
                                <ToggleSwitch
                                  bind:checked={editBooleanValue}
                                  disabled={state.isSetting}
                                  on:change={handleBooleanEditChange}
                                >
                                  {editBooleanValue ? 'True' : 'False'}
                                </ToggleSwitch>
                              </div>
                            {:else}
                              <input
                                class="inline-edit"
                                bind:value={editValue}
                                on:keydown={(e) => {
                                  if (e.key === 'Enter') commitEdit();
                                  if (e.key === 'Escape') cancelEdit();
                                }}
                              />
                            {/if}
                          {:else if isBooleanValue(field.value)}
                            <span
                              class="badge small"
                              class:active={normalizeBooleanValue(field.value) === 'true'}
                              class:clickable={writable}
                              on:click={() => writable && startEdit(field.path, field.value)}
                              title={writable ? 'Click to edit' : 'Read-only'}
                            >
                              {field.value}
                            </span>
                          {:else if writable}
                            <span
                              class="clickable-value"
                              on:click={() => startEdit(field.path, field.value)}
                              title="Click to edit"
                            >
                              {field.value || '-'}
                            </span>
                          {:else}
                            <span>{field.value || '-'}</span>
                          {/if}
                        </td>
                        <td class="col-access">
                          <span class="access-badge" class:rw={writable}>{writable ? 'RW' : 'RO'}</span>
                        </td>
                        <td class="col-action">
                          {#if editingPath === field.path}
                            <button class="btn-mini save" on:click={commitEdit} disabled={state.isSetting}>
                              {state.isSetting ? '...' : 'Save'}
                            </button>
                            <button class="btn-mini cancel" on:click={cancelEdit}>Cancel</button>
                          {/if}
                        </td>
                      </tr>
                    {/each}
                  </tbody>
                </table>
              </details>
            {/each}
            {#if filteredSnapshot.length === 0 && dmSearch}
              <div class="dm-empty">No parameters match "{dmSearch}"</div>
            {/if}
          </div>

        <!-- ════════════════════════════════════════════════════════════ -->
        <!-- OPERATIONS TAB                                              -->
        <!-- ════════════════════════════════════════════════════════════ -->
        {:else if activeTab === 'operations'}
          <div class="operations-layout">
            <!-- Live Query -->
            <div class="card">
              <div class="card-header">
                <svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><circle cx="11" cy="11" r="8"/><path d="M21 21l-4.35-4.35"/></svg>
                <h3>Live Query</h3>
              </div>
              <div class="op-form">
                <div class="autocomplete-wrapper">
                  <input
                    type="text"
                    placeholder="Type a path... (e.g. Device.DeviceInfo.)"
                    bind:value={queryPath}
                    on:input={() => { showSuggestions = true; }}
                    on:focus={() => { showSuggestions = true; }}
                    on:blur={() => { setTimeout(() => showSuggestions = false, 200); }}
                  />
                  {#if showSuggestions && queryPath.length >= 3 && autocompleteSuggestions.length > 0}
                    <ul class="suggestions">
                      {#each autocompleteSuggestions.slice(0, 10) as suggestion}
                        <li on:mousedown={() => { queryPath = suggestion; showSuggestions = false; }}>
                          {suggestion}
                        </li>
                      {/each}
                    </ul>
                  {/if}
                </div>
                <button
                  class="btn-primary"
                  on:click={() => queryPath && wuspStore.sendGet(peerId, [queryPath])}
                  disabled={state.isLoading || !queryPath}
                >
                  {state.isLoading ? 'Loading...' : 'Get'}
                </button>
              </div>
              {#if state.liveParams.length > 0}
                <table class="param-table" style="margin-top: 12px;">
                  <thead><tr><th>Path</th><th>Value</th><th>Set</th></tr></thead>
                  <tbody>
                    {#each state.liveParams as p}
                      <tr>
                        <td class="param-path">{p.path}</td>
                        <td class="param-value">
                          <input
                            class="inline-edit"
                            value={p.value}
                            on:change={(e) => { p.value = e.currentTarget.value; }}
                          />
                        </td>
                        <td>
                          <button
                            class="btn-mini save"
                            on:click={() => wuspStore.sendSet(peerId, [{ path: p.path, value: p.value }])}
                            disabled={state.isSetting}
                          >Set</button>
                        </td>
                      </tr>
                    {/each}
                  </tbody>
                </table>
              {/if}
            </div>

            <!-- Set Parameter -->
            <div class="card">
              <div class="card-header">
                <svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><path d="M11 4H4a2 2 0 00-2 2v14a2 2 0 002 2h14a2 2 0 002-2v-7"/><path d="M18.5 2.5a2.121 2.121 0 013 3L12 15l-4 1 1-4 9.5-9.5z"/></svg>
                <h3>Set Parameter</h3>
              </div>
              <div class="op-form two-inputs">
                <input
                  type="text"
                  class="op-input"
                  placeholder="Parameter path"
                  bind:value={setPath}
                />
                <input
                  type="text"
                  class="op-input"
                  placeholder="Value"
                  bind:value={setValue}
                />
                <button
                  class="btn-primary"
                  on:click={handleSendSet}
                  disabled={state.isSetting || !setPath}
                >
                  {state.isSetting ? 'Setting...' : 'Set'}
                </button>
              </div>
            </div>

            <!-- Device Commands -->
            <div class="card">
              <div class="card-header">
                <svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><path d="M4 4h16c1.1 0 2 .9 2 2v12c0 1.1-.9 2-2 2H4c-1.1 0-2-.9-2-2V6c0-1.1.9-2 2-2z"/><path d="M8 12l3 3 5-5"/></svg>
                <h3>Device Commands</h3>
              </div>
              <div class="op-buttons">
                <div class="op-btn-group">
                  <button
                    class="op-btn"
                    class:danger={rebootConfirm}
                    on:click={handleReboot}
                    disabled={state.isLoading}
                  >
                    <svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><path d="M1 4v6h6M23 20v-6h-6"/><path d="M20.49 9A9 9 0 005.64 5.64L1 10m22 4l-4.64 4.36A9 9 0 013.51 15"/></svg>
                    {rebootConfirm ? 'Confirm Reboot?' : 'Reboot Device'}
                  </button>
                  {#if rebootConfirm}
                    <button class="btn-mini cancel" on:click={() => rebootConfirm = false}>Cancel</button>
                  {/if}
                </div>
                <div class="op-btn-group">
                  <button
                    class="op-btn"
                    class:danger={factoryConfirm1}
                    class:critical-btn={factoryConfirm2}
                    on:click={handleFactoryReset}
                    disabled={state.isLoading}
                  >
                    <svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><path d="M12 22s8-4 8-10V5l-8-3-8 3v7c0 6 8 10 8 10z"/></svg>
                    {#if factoryConfirm2}
                      CONFIRM Factory Reset
                    {:else if factoryConfirm1}
                      Are you sure?
                    {:else}
                      Factory Reset
                    {/if}
                  </button>
                  {#if factoryConfirm1}
                    <button class="btn-mini cancel" on:click={() => { factoryConfirm1 = false; factoryConfirm2 = false; }}>Cancel</button>
                  {/if}
                </div>
                <button
                  class="op-btn"
                  on:click={handleSync}
                  disabled={state.isSyncing}
                >
                  <svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><path d="M1 4v6h6M23 20v-6h-6"/><path d="M20.49 9A9 9 0 005.64 5.64L1 10m22 4l-4.64 4.36A9 9 0 013.51 15"/></svg>
                  {state.isSyncing ? 'Syncing...' : 'Force Re-Sync'}
                </button>
              </div>
            </div>
          </div>

        <!-- ════════════════════════════════════════════════════════════ -->
        <!-- SNAPSHOTS TAB                                               -->
        <!-- ════════════════════════════════════════════════════════════ -->
        {:else if activeTab === 'snapshots'}
          <div class="snapshots-layout">
            <!-- Create Snapshot -->
            <div class="card">
              <div class="card-header">
                <svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><path d="M5 8h14M5 8a2 2 0 110-4h14a2 2 0 110 4M5 8v10a2 2 0 002 2h10a2 2 0 002-2V8"/><path d="M10 12h4"/></svg>
                <h3>Take Snapshot</h3>
              </div>
              <div class="op-form">
                <input
                  type="text"
                  class="op-input"
                  placeholder="Snapshot name (e.g. Pre-Update Backup)"
                  bind:value={snapshotName}
                />
                <button
                  class="btn-primary"
                  on:click={handleCreateSnapshot}
                  disabled={isCreatingSnapshot || snapState.isSaving || !snapshotName.trim()}
                >
                  {#if isCreatingSnapshot || snapState.isSaving}
                    <div class="spinner-sm" /> Saving...
                  {:else}
                    Take Snapshot
                  {/if}
                </button>
              </div>
              <p class="field-muted" style="margin-top: 8px;">Captures the current device state as a reusable configuration snapshot.</p>
            </div>

            <!-- Compare toggle -->
            <div class="snapshot-controls">
              <button class="btn-outline" class:active={compareMode} on:click={() => { compareMode = !compareMode; compareA = null; compareB = null; }}>
                <svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><path d="M16 3h5v5M4 20L21 3M21 16v5h-5M15 15l6 6M4 4l5 5"/></svg>
                {compareMode ? 'Exit Compare' : 'Compare Snapshots'}
              </button>
              {#if compareMode}
                <span class="compare-hint">
                  {#if !compareA}Select first snapshot{:else if !compareB}Select second snapshot{:else}Showing diff below{/if}
                </span>
              {/if}
            </div>

            <!-- Filter bar — same shape as the Account snapshots tab -->
            <div class="snap-filterbar">
              <div class="snap-search-wrap">
                <svg class="snap-search-icon" width="14" height="14" viewBox="0 0 24 24" fill="none">
                  <circle cx="11" cy="11" r="7" stroke="currentColor" stroke-width="2"/>
                  <path d="M16 16L21 21" stroke="currentColor" stroke-width="2" stroke-linecap="round"/>
                </svg>
                <input
                  class="snap-search-input"
                  type="text"
                  placeholder="Search by name, serial, or model"
                  bind:value={snapSearch}
                />
              </div>
              <input
                class="snap-date"
                type="date"
                bind:value={snapFromDate}
                title="From date"
              />
              <span class="snap-date-sep">→</span>
              <input
                class="snap-date"
                type="date"
                bind:value={snapToDate}
                title="To date"
              />
              {#if snapSearch || snapFromDate || snapToDate}
                <button class="snap-clear" on:click={clearSnapshotFilters} title="Clear filters">
                  <svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2">
                    <line x1="18" y1="6" x2="6" y2="18"/><line x1="6" y1="6" x2="18" y2="18"/>
                  </svg>
                </button>
              {/if}
              <span class="snap-count">{filteredSnapshots.length} / {(snapState.snapshots || []).filter(s => (s.protocol || 'wusp') === 'wusp').length}</span>
            </div>

            <!-- Snapshot list -->
            {#if snapState.isLoading}
              <div class="loading-state compact">
                <div class="spinner" />
                <span>Loading snapshots...</span>
              </div>
            {:else if filteredSnapshots.length === 0}
              <div class="dm-empty">
                {#if snapSearch || snapFromDate || snapToDate}
                  No snapshots match your filters.
                {:else}
                  No snapshots saved yet. Take one above to get started.
                {/if}
              </div>
            {:else}
              <div class="snapshot-list">
                {#each filteredSnapshots as snap (snap.id)}
                  {@const isCompareSelected = compareA?.id === snap.id || compareB?.id === snap.id}
                  <div class="card snapshot-card" class:compare-selected={isCompareSelected}>
                    <div class="snapshot-row">
                      <div class="snapshot-info">
                        <div class="snapshot-name">{snap.name}</div>
                        <div class="snapshot-meta">
                          <span>{formatDate(snap.created_at)}</span>
                          {#if snap.manufacturer}
                            <span class="sep">|</span>
                            <span>{snap.manufacturer}</span>
                          {/if}
                          {#if snap.software_version}
                            <span class="sep">|</span>
                            <span>v{snap.software_version}</span>
                          {/if}
                          <span class="sep">|</span>
                          <span>{parseSnapshotParamCount(snap)} params</span>
                        </div>
                      </div>
                      <div class="snapshot-actions">
                        {#if compareMode}
                          <button
                            class="btn-mini"
                            class:active={isCompareSelected}
                            on:click={() => selectForCompare(snap)}
                          >
                            {isCompareSelected ? 'Selected' : 'Select'}
                          </button>
                        {:else}
                          <button
                            class="btn-mini save"
                            on:click={() => handleProvision(snap.id)}
                            disabled={provisioningId === snap.id || snapState.isProvisioning}
                          >
                            {provisioningId === snap.id ? 'Applying...' : 'Apply to Device'}
                          </button>
                          <button
                            class="btn-mini cancel"
                            on:click={() => handleDeleteSnapshot(snap.id)}
                            disabled={deletingId === snap.id}
                          >
                            {deletingId === snap.id ? '...' : 'Delete'}
                          </button>
                        {/if}
                      </div>
                    </div>
                  </div>
                {/each}
              </div>
            {/if}

            <!-- Compare diff view -->
            {#if compareMode && compareA && compareB}
              <div class="card compare-card">
                <div class="card-header">
                  <h3>Diff: {compareA.name} vs {compareB.name}</h3>
                  <span class="dm-stats-inline">{compareDiff.length} differences</span>
                </div>
                {#if compareDiff.length === 0}
                  <div class="dm-empty">These snapshots are identical.</div>
                {:else}
                  <table class="param-table">
                    <thead>
                      <tr>
                        <th>Path</th>
                        <th>{compareA.name}</th>
                        <th>{compareB.name}</th>
                        <th>Change</th>
                      </tr>
                    </thead>
                    <tbody>
                      {#each compareDiff as d}
                        <tr class="diff-row diff-{d.type}">
                          <td class="param-path" title={d.path}>{d.path}</td>
                          <td class="param-value diff-old">{d.valueA || '-'}</td>
                          <td class="param-value diff-new">{d.valueB || '-'}</td>
                          <td>
                            <span class="diff-badge diff-badge-{d.type}">
                              {d.type === 'changed' ? 'Modified' : d.type === 'added' ? 'Added' : 'Removed'}
                            </span>
                          </td>
                        </tr>
                      {/each}
                    </tbody>
                  </table>
                {/if}
              </div>
            {/if}
          </div>
        {/if}
      </div>
    {/if}
  </div>
</div>

<style>
  /* ══════════════════════════════════════════════════════════════════════ */
  /* Window Container                                                      */
  /* ══════════════════════════════════════════════════════════════════════ */
  .wusp-dashboard {
    position: absolute;
    width: 900px;
    height: 680px;
    /* Resizable, but never below the size at which the desktop layout
       (two-col overview, two-col network grid, full tab labels) still works.
       Mobile breakpoint at 768px takes over below that with a stacked layout. */
    min-width: 720px;
    min-height: 600px;
    max-height: 90vh;
    resize: both;
    border-radius: 8px;
    display: flex;
    flex-direction: column;
    overflow: hidden;
    background: var(--mica);
    color: rgb(var(--clr));
    font-family: -apple-system, BlinkMacSystemFont, 'Segoe UI', sans-serif;
    box-shadow:
      0 8px 32px rgba(0, 0, 0, 0.24),
      0 0 0 1px rgb(var(--clr) / 8%);
    border: 1px solid rgb(var(--clr) / 10%);
  }

  .dashboard-content {
    flex: 1;
    min-height: 0;
    overflow: hidden;
    padding: 0;
    display: flex;
    flex-direction: column;
  }

  /* ══════════════════════════════════════════════════════════════════════ */
  /* Tab Bar (matching Account.svelte pattern)                             */
  /* ══════════════════════════════════════════════════════════════════════ */
  .tabs {
    display: flex;
    gap: 4px;
    overflow-x: scroll;
    scroll-behavior: smooth;
    scrollbar-width: none;
    max-width: 100%;
    position: relative;
    padding: 12px 16px;
    background: rgb(var(--bg2) / 40%);
    border-bottom: 1px solid rgb(var(--clr) / 8%);
    flex-shrink: 0;
    align-items: center;
  }
  .tabs::-webkit-scrollbar { display: none; }

  .tab-btn {
    display: flex;
    align-items: center;
    justify-content: center;
    gap: 8px;
    padding: 10px 16px;
    background: transparent;
    border: 1px solid transparent;
    border-radius: 8px;
    color: rgb(var(--clr) / 60%);
    font-size: 13px;
    font-weight: 500;
    cursor: pointer;
    transition: all 0.2s ease;
    flex-shrink: 0;
    white-space: nowrap;
  }
  /* Lucide icons render as <svg> inside their own component, which puts
     them outside this file's scoped CSS — :global(svg) lets us style them
     without leaking to other components. */
  .tab-btn :global(svg) {
    opacity: 0.6;
    flex-shrink: 0;
    transition: all 0.2s ease;
  }
  .tab-btn:hover {
    color: rgb(var(--clr) / 85%);
    background: rgb(var(--clr) / 6%);
    border-color: rgb(var(--clr) / 10%);
  }
  .tab-btn:hover :global(svg) { opacity: 0.8; }
  .tab-btn.active {
    color: rgb(var(--clrPrm));
    background: rgb(var(--clrPrm) / 12%);
    border-color: rgb(var(--clrPrm) / 25%);
    font-weight: 600;
  }
  .tab-btn.active :global(svg) {
    opacity: 1;
  }

  .tab-spacer { flex: 1; }

  /* Reserve a fixed slot for the right-side cluster so the variable-width
     "Synced 4/25/2026, 9:44:20 PM" string can't shift the Sync button each
     refresh. tabular-nums fixes width drift between digit-shapes. */
  .tab-actions {
    display: flex;
    align-items: center;
    justify-content: flex-end;
    gap: 5px;
    flex-shrink: 0;
    min-width: 180px;
  }
  .last-sync {
    font-size: 11px;
    color: rgb(var(--clr) / 40%);
    white-space: nowrap;
    text-align: right;
    font-variant-numeric: tabular-nums;
  }
  /* Tab button min-height keeps layout identical between idle/hover/active
     so switching tabs doesn't cause a 1-pixel reflow chain through siblings. */
  .tab-btn { min-height: 36px; }

  /* Narrow viewports: collapse text labels to icons, hide the timestamp.
     Aligned with the mobile breakpoint (768px) so the transition happens
     in one step rather than at 720→768 staggered. */
  @media (max-width: 768px) {
    .tab-btn span { display: none; }
    .tab-actions { min-width: 0; }
    .last-sync { display: none; }
    .tabs { padding: 12px 8px; }
  }

  /* ══════════════════════════════════════════════════════════════════════ */
  /* Mobile (≤768px): fullscreen window, stacked inner grids                */
  /* The class:maximized={... || $isMobile} on the root already triggers   */
  /* the global .maximized fullscreen rule (global.css:1304) — these rules */
  /* are about the *content* of the window, not its outer chrome.          */
  /* ══════════════════════════════════════════════════════════════════════ */
  @media (max-width: 768px) {
    .wusp-dashboard {
      /* Mobile fullscreen: clear desktop min-width/max-height so the global
         .maximized override can size the window to the viewport. */
      min-width: 0 !important;
      max-height: none !important;
      resize: none !important;
      border-radius: 0 !important;
    }
    .overview-grid { grid-template-columns: 1fr; }
    .iface-grid { grid-template-columns: 1fr; }
    .field-grid,
    .field-grid.three-col { grid-template-columns: 1fr 1fr; }
    .tab-content { padding: 12px; }
    .card { padding: 12px; }
    .card-header { margin-bottom: 8px; }
    .btn-sync { padding: 6px 10px; }
    /* Collapse "Sync" label to just the icon on the smallest screens
       where the tab bar is already crammed. */
    .btn-sync :global(span) { display: none; }

    /* Param tables are 4-column desktop layouts (path, value, access,
       action). Stack them as cards on mobile — the 300px path column
       crushes everything else on narrow viewports otherwise. */
    .param-table,
    .param-table tbody,
    .param-table tr,
    .param-table td {
      display: block;
      width: 100%;
    }
    .param-table thead { display: none; }
    .param-table tr {
      border: 1px solid rgb(var(--clr) / 10%);
      border-radius: 8px;
      padding: 8px 10px;
      margin-bottom: 6px;
      background: rgb(var(--bg2) / 30%);
    }
    .param-table td {
      padding: 2px 0;
      border: none;
    }
    .param-table .param-path {
      font-size: 10px;
      letter-spacing: 0.05em;
      text-transform: uppercase;
      color: rgb(var(--clr) / 50%);
      max-width: none;
      white-space: normal;
      word-break: break-all;
    }
    .param-table .param-value {
      font-size: 12px;
      word-break: break-word;
      white-space: normal;
    }
    .param-table .col-access {
      width: auto;
      display: inline-block;
      margin-top: 4px;
    }
    .param-table .col-action {
      display: flex;
      gap: 6px;
      margin-top: 6px;
    }
    .param-table .col-action:empty {
      display: none;
    }
  }
  .btn-sync {
    display: inline-flex;
    align-items: center;
    gap: 6px;
    padding: 7px 14px;
    border-radius: 999px;
    border: 1px solid rgb(var(--clrPrm) / 30%);
    background: linear-gradient(
      180deg,
      rgb(var(--clrPrm) / 16%) 0%,
      rgb(var(--clrPrm) / 8%) 100%
    );
    color: rgb(var(--clrPrm));
    cursor: pointer;
    font-size: 12px;
    font-weight: 600;
    letter-spacing: 0.2px;
    box-shadow:
      0 1px 0 rgb(255 255 255 / 6%) inset,
      0 1px 3px rgb(0 0 0 / 18%);
    transition:
      background 0.15s ease,
      border-color 0.15s ease,
      transform 0.1s ease,
      box-shadow 0.15s ease;
  }
  .btn-sync :global(svg) {
    transition: transform 0.4s ease;
  }
  .btn-sync:hover {
    background: linear-gradient(
      180deg,
      rgb(var(--clrPrm) / 24%) 0%,
      rgb(var(--clrPrm) / 14%) 100%
    );
    border-color: rgb(var(--clrPrm) / 50%);
    box-shadow:
      0 1px 0 rgb(255 255 255 / 8%) inset,
      0 2px 6px rgb(var(--clrPrm) / 18%);
  }
  .btn-sync:hover :global(svg) { transform: rotate(-45deg); }
  .btn-sync:active {
    transform: translateY(1px);
    box-shadow:
      0 1px 0 rgb(255 255 255 / 4%) inset,
      0 1px 2px rgb(0 0 0 / 18%);
  }
  .btn-sync:disabled,
  .btn-sync.syncing {
    opacity: 0.7;
    cursor: wait;
  }
  .btn-sync.syncing :global(svg) {
    animation: btn-sync-spin 0.8s linear infinite;
  }
  @keyframes btn-sync-spin {
    to { transform: rotate(360deg); }
  }

  /* ══════════════════════════════════════════════════════════════════════ */
  /* Tab Content                                                           */
  /* ══════════════════════════════════════════════════════════════════════ */
  .tab-content {
    flex: 1;
    min-height: 0;
    overflow-y: auto;
    padding: 16px;
  }

  /* ══════════════════════════════════════════════════════════════════════ */
  /* Cards                                                                 */
  /* ══════════════════════════════════════════════════════════════════════ */
  .card {
    background: rgb(var(--bg1));
    border: 1px solid rgb(var(--clr) / 8%);
    border-radius: 8px;
    padding: 16px;
    margin-bottom: 12px;
  }
  .card-header {
    display: flex;
    align-items: center;
    gap: 8px;
    margin-bottom: 12px;
  }
  .card-header svg {
    opacity: 0.6;
    color: rgb(var(--clrPrm));
  }
  .card-header h3 {
    margin: 0;
    font-size: 14px;
    color: rgb(var(--clrPrm));
    font-weight: 600;
  }
  .card h3 {
    margin: 0 0 12px;
    font-size: 14px;
    color: rgb(var(--clrPrm));
    font-weight: 600;
  }

  /* ══════════════════════════════════════════════════════════════════════ */
  /* Overview Grid                                                         */
  /* ══════════════════════════════════════════════════════════════════════ */
  .overview-grid {
    display: grid;
    grid-template-columns: 1fr 1fr;
    gap: 12px;
  }
  .identity-card { grid-column: 1 / -1; }

  .cellular-list {
    display: grid;
    gap: 10px;
  }

  .cellular-row {
    border-radius: 12px;
    padding: 10px;
    background: rgb(var(--clr) / 5%);
  }

  .cellular-head {
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: 12px;
    margin-bottom: 10px;
  }

  .cellular-head strong,
  .cellular-head span {
    display: block;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }

  .cellular-head span {
    margin-top: 3px;
    color: rgb(var(--clr) / 56%);
    font-size: 12px;
  }

  .cellular-grid {
    grid-template-columns: repeat(3, minmax(0, 1fr));
  }

  .signal-bars {
    display: inline-flex;
    align-items: end;
    gap: 3px;
    height: 22px;
    flex-shrink: 0;
  }

  .signal-bars i {
    display: block;
    width: 5px;
    border-radius: 3px;
    background: rgb(var(--clr) / 16%);
  }

  .signal-bars i:nth-child(1) { height: 7px; }
  .signal-bars i:nth-child(2) { height: 11px; }
  .signal-bars i:nth-child(3) { height: 15px; }
  .signal-bars i:nth-child(4) { height: 19px; }

  .signal-bars i.lit {
    background: rgb(12 214 142);
  }

  .field-grid {
    display: grid;
    grid-template-columns: 1fr 1fr;
    gap: 8px;
  }
  .field-grid.three-col {
    grid-template-columns: 1fr 1fr 1fr;
  }
  .field label,
  .field-label {
    display: block;
    font-size: 11px;
    color: rgb(var(--clr) / 40%);
    margin-bottom: 2px;
    text-transform: uppercase;
    letter-spacing: 0.5px;
  }
  .field span {
    font-size: 13px;
    color: rgb(var(--clr) / 90%);
  }
  .field .mono {
    font-family: 'JetBrains Mono', monospace;
    font-size: 11px;
    word-break: break-all;
  }
  .field-muted {
    font-size: 12px;
    color: rgb(var(--clr) / 35%);
    font-style: italic;
  }

  /* Uptime display */
  .uptime-display { text-align: center; padding: 8px 0; }
  .uptime-value {
    font-size: 28px;
    font-weight: 700;
    color: rgb(var(--clrPrm));
    display: block;
  }
  .uptime-detail {
    font-size: 11px;
    color: rgb(var(--clr) / 35%);
    display: block;
    margin-top: 2px;
  }

  /* ══════════════════════════════════════════════════════════════════════ */
  /* Status Badges                                                         */
  /* ══════════════════════════════════════════════════════════════════════ */
  .status-badges { display: flex; gap: 8px; flex-wrap: wrap; }

  .badge {
    padding: 3px 10px;
    border-radius: 12px;
    font-size: 11px;
    font-weight: 600;
    background: rgb(var(--clr) / 8%);
    color: rgb(var(--clr) / 50%);
  }
  .badge.small { padding: 2px 8px; font-size: 10px; }
  .badge.active {
    background: rgba(166, 227, 161, 0.15);
    color: #a6e3a1;
  }
  .badge.status-active {
    background: rgba(166, 227, 161, 0.15);
    color: #a6e3a1;
  }
  .badge.status-dormant {
    background: rgba(249, 226, 175, 0.15);
    color: #f9e2af;
  }
  .badge.status-error {
    background: rgba(243, 139, 168, 0.15);
    color: #f38ba8;
  }
  .badge.version {
    background: rgb(var(--clrPrm) / 12%);
    color: rgb(var(--clrPrm));
  }

  /* Access badge */
  .access-badge {
    font-size: 10px;
    font-weight: 600;
    padding: 1px 6px;
    border-radius: 3px;
    background: rgb(var(--clr) / 6%);
    color: rgb(var(--clr) / 35%);
    font-family: 'JetBrains Mono', monospace;
  }
  .access-badge.rw {
    background: rgb(var(--clrPrm) / 12%);
    color: rgb(var(--clrPrm) / 80%);
  }

  /* ══════════════════════════════════════════════════════════════════════ */
  /* Resource Bars                                                         */
  /* ══════════════════════════════════════════════════════════════════════ */
  .resource-item { margin-bottom: 8px; }
  .resource-label {
    display: flex;
    justify-content: space-between;
    font-size: 12px;
    margin-bottom: 4px;
    color: rgb(var(--clr) / 70%);
  }
  .resource-value {
    color: rgb(var(--clrPrm));
    font-family: "JetBrains Mono", monospace;
    font-size: 11px;
  }
  .resource-pct {
    font-size: 11px;
    color: rgb(var(--clr) / 40%);
    margin-top: 2px;
    display: block;
  }
  .progress-bar {
    height: 6px;
    background: rgb(var(--clr) / 8%);
    border-radius: 3px;
    overflow: hidden;
  }
  .progress-fill {
    height: 100%;
    background: #a6e3a1;
    border-radius: 3px;
    transition: width 0.3s;
  }
  .progress-fill.warn { background: #f9e2af; }
  .progress-fill.critical { background: #f38ba8; }

  /* ══════════════════════════════════════════════════════════════════════ */
  /* Network Layout                                                        */
  /* ══════════════════════════════════════════════════════════════════════ */
  .network-layout { display: flex; flex-direction: column; gap: 0; }

  .iface-grid {
    display: grid;
    grid-template-columns: 1fr 1fr;
    gap: 12px;
    margin-bottom: 12px;
  }
  .iface-card { padding: 14px; }
  .iface-header {
    display: flex;
    align-items: center;
    gap: 8px;
    margin-bottom: 10px;
  }
  .iface-name {
    font-weight: 600;
    font-size: 14px;
    color: rgb(var(--clr) / 90%);
    font-family: "JetBrains Mono", monospace;
  }
  .iface-details {
    display: flex;
    flex-direction: column;
    gap: 6px;
    margin-bottom: 8px;
  }
  .iface-ips {
    display: flex;
    gap: 6px;
    flex-wrap: wrap;
    margin-top: 4px;
  }
  .ip-badge {
    padding: 2px 8px;
    border-radius: 4px;
    font-size: 11px;
    font-family: "JetBrains Mono", monospace;
  }
  .ip-badge.v4 {
    background: rgba(166, 227, 161, 0.12);
    color: #a6e3a1;
  }
  .ip-badge.v6 {
    background: rgb(var(--clrPrm) / 12%);
    color: rgb(var(--clrPrm));
  }

  /* ══════════════════════════════════════════════════════════════════════ */
  /* Param Table                                                           */
  /* ══════════════════════════════════════════════════════════════════════ */
  .param-table {
    width: 100%;
    border-collapse: collapse;
    font-size: 12px;
  }
  .param-table th {
    text-align: left;
    padding: 6px 8px;
    border-bottom: 1px solid rgb(var(--clr) / 10%);
    color: rgb(var(--clr) / 40%);
    font-weight: 500;
    font-size: 11px;
    text-transform: uppercase;
  }
  .param-table td {
    padding: 5px 8px;
    border-bottom: 1px solid rgb(var(--clr) / 5%);
  }
  .param-path {
    color: rgb(var(--clrPrm) / 80%);
    font-family: 'JetBrains Mono', monospace;
    font-size: 11px;
    max-width: 300px;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }
  .param-value {
    color: rgb(var(--clr) / 85%);
    font-family: 'JetBrains Mono', monospace;
    font-size: 11px;
    word-break: break-all;
  }

  /* ══════════════════════════════════════════════════════════════════════ */
  /* Data Model Browser                                                    */
  /* ══════════════════════════════════════════════════════════════════════ */
  .dm-toolbar {
    display: flex;
    align-items: center;
    gap: 12px;
    margin-bottom: 12px;
  }
  .dm-search {
    flex: 1;
    padding: 8px 12px;
    border: 1px solid rgb(var(--clr) / 12%);
    border-radius: 6px;
    background: rgb(var(--bg1));
    color: rgb(var(--clr));
    font-size: 13px;
  }
  .dm-search:focus {
    border-color: rgb(var(--clrPrm));
    outline: none;
  }
  .dm-stats-inline {
    font-size: 12px;
    color: rgb(var(--clr) / 40%);
    white-space: nowrap;
  }
  .dm-empty {
    text-align: center;
    padding: 24px;
    color: rgb(var(--clr) / 35%);
    font-size: 13px;
  }

  .dm-section {
    margin-bottom: 4px;
    border: 1px solid rgb(var(--clr) / 8%);
    border-radius: 6px;
    overflow: hidden;
  }
  .dm-section summary {
    padding: 8px 12px;
    background: rgb(var(--bg1));
    cursor: pointer;
    display: flex;
    justify-content: space-between;
    align-items: center;
    font-size: 13px;
    color: rgb(var(--clr) / 85%);
    user-select: none;
  }
  .dm-section summary:hover {
    background: rgb(var(--clr) / 5%);
  }
  .section-name { font-weight: 500; }
  .section-count {
    font-size: 11px;
    color: rgb(var(--clr) / 40%);
    background: rgb(var(--clr) / 8%);
    padding: 2px 8px;
    border-radius: 10px;
  }

  .col-access { width: 40px; text-align: center; }
  .col-action { width: 130px; text-align: right; display: flex; gap: 4px; justify-content: flex-end; align-items: center; }

  /* Inline editing */
  .inline-edit {
    width: 100%;
    padding: 5px 8px;
    border: 1px solid rgb(var(--clrPrm) / 60%);
    border-radius: 4px;
    background: rgb(var(--bg1));
    color: rgb(var(--clr));
    font-family: "JetBrains Mono", "SF Mono", monospace;
    font-size: 12px;
    outline: none;
    box-shadow: 0 0 0 2px rgb(var(--clrPrm) / 15%);
  }
  .inline-edit:focus {
    border-color: rgb(var(--clrPrm));
    box-shadow: 0 0 0 3px rgb(var(--clrPrm) / 25%);
  }
  .inline-bool-edit {
    display: flex;
    align-items: center;
    min-height: 28px;
  }
  .inline-bool-edit :global(label) {
    font-size: 12px;
    color: rgb(var(--clr) / 88%);
    font-family: "JetBrains Mono", "SF Mono", monospace;
  }
  .clickable-value {
    cursor: pointer;
    padding: 2px 4px;
    border-radius: 3px;
    transition: all 0.15s;
  }
  .clickable-value:hover {
    color: rgb(var(--clrPrm));
    background: rgb(var(--clrPrm) / 8%);
  }
  .badge.clickable { cursor: pointer; }
  .badge.clickable:hover { opacity: 0.8; }
  tr.writable { background: rgb(var(--clrPrm) / 3%); }

  .btn-mini {
    padding: 4px 12px;
    border-radius: 4px;
    border: 1px solid rgb(var(--clr) / 15%);
    font-size: 11px;
    font-weight: 500;
    cursor: pointer;
    background: rgb(var(--clr) / 5%);
    color: rgb(var(--clr) / 70%);
    transition: all 0.15s;
    white-space: nowrap;
  }
  .btn-mini:disabled { opacity: 0.4; cursor: not-allowed; }
  .btn-mini.save {
    background: rgba(166, 227, 161, 0.12);
    border-color: rgba(166, 227, 161, 0.4);
    color: #a6e3a1;
  }
  .btn-mini.save:hover:not(:disabled) { background: rgba(166, 227, 161, 0.2); }
  .btn-mini.cancel { border-color: rgb(var(--clr) / 20%); color: rgb(var(--clr) / 60%); }
  .btn-mini.cancel:hover { background: rgb(var(--clr) / 10%); }

  /* ══════════════════════════════════════════════════════════════════════ */
  /* Operations                                                            */
  /* ══════════════════════════════════════════════════════════════════════ */
  .operations-layout { display: flex; flex-direction: column; gap: 0; }

  .op-form {
    display: flex;
    gap: 8px;
    align-items: flex-start;
  }
  .op-form.two-inputs {
    display: grid;
    grid-template-columns: 1fr 1fr auto;
    gap: 8px;
  }
  .op-input {
    padding: 8px 12px;
    border: 1px solid rgb(var(--clr) / 12%);
    border-radius: 6px;
    background: rgb(var(--bg1));
    color: rgb(var(--clr));
    font-family: 'JetBrains Mono', monospace;
    font-size: 12px;
    width: 100%;
  }
  .op-input:focus { border-color: rgb(var(--clrPrm)); outline: none; }

  .op-buttons {
    display: flex;
    gap: 8px;
    flex-wrap: wrap;
    align-items: center;
  }
  .op-btn-group {
    display: flex;
    gap: 4px;
    align-items: center;
  }
  .op-btn {
    display: flex;
    align-items: center;
    gap: 6px;
    padding: 8px 16px;
    border: 1px solid rgb(var(--clr) / 12%);
    border-radius: 6px;
    background: rgb(var(--clr) / 5%);
    color: rgb(var(--clr) / 80%);
    cursor: pointer;
    font-size: 13px;
    transition: all 0.15s;
  }
  .op-btn:hover {
    background: rgb(var(--clr) / 10%);
    border-color: rgb(var(--clr) / 20%);
  }
  .op-btn:disabled { opacity: 0.5; cursor: not-allowed; }
  .op-btn.danger {
    border-color: rgba(243, 139, 168, 0.4);
    color: #f38ba8;
    background: rgba(243, 139, 168, 0.08);
  }
  .op-btn.critical-btn {
    border-color: rgba(243, 139, 168, 0.6);
    color: #f38ba8;
    background: rgba(243, 139, 168, 0.15);
    font-weight: 600;
  }

  .btn-primary {
    display: flex;
    align-items: center;
    gap: 6px;
    padding: 8px 16px;
    border: none;
    border-radius: 6px;
    background: rgb(var(--clrPrm));
    color: rgb(var(--bg1));
    cursor: pointer;
    font-weight: 600;
    font-size: 12px;
    white-space: nowrap;
    transition: opacity 0.15s;
  }
  .btn-primary:hover { opacity: 0.9; }
  .btn-primary:disabled { opacity: 0.5; cursor: not-allowed; }

  .btn-outline {
    display: flex;
    align-items: center;
    gap: 6px;
    padding: 6px 14px;
    border: 1px solid rgb(var(--clr) / 12%);
    border-radius: 6px;
    background: transparent;
    color: rgb(var(--clr) / 70%);
    cursor: pointer;
    font-size: 12px;
    transition: all 0.15s;
  }
  .btn-outline:hover {
    background: rgb(var(--clr) / 5%);
    border-color: rgb(var(--clr) / 20%);
  }
  .btn-outline.active {
    background: rgb(var(--clrPrm) / 12%);
    border-color: rgb(var(--clrPrm) / 30%);
    color: rgb(var(--clrPrm));
  }

  /* Autocomplete */
  .autocomplete-wrapper { position: relative; flex: 1; }
  .autocomplete-wrapper input {
    width: 100%;
    padding: 8px 12px;
    border: 1px solid rgb(var(--clr) / 12%);
    border-radius: 6px;
    background: rgb(var(--bg1));
    color: rgb(var(--clr));
    font-family: "JetBrains Mono", monospace;
    font-size: 12px;
  }
  .autocomplete-wrapper input:focus { border-color: rgb(var(--clrPrm)); outline: none; }
  .suggestions {
    position: absolute;
    top: 100%;
    left: 0;
    right: 0;
    background: rgb(var(--bg1));
    border: 1px solid rgb(var(--clr) / 15%);
    border-radius: 0 0 6px 6px;
    max-height: 200px;
    overflow-y: auto;
    z-index: 10;
    list-style: none;
    margin: 0;
    padding: 0;
  }
  .suggestions li {
    padding: 6px 10px;
    font-family: "JetBrains Mono", monospace;
    font-size: 11px;
    color: rgb(var(--clrPrm));
    cursor: pointer;
  }
  .suggestions li:hover { background: rgb(var(--clr) / 6%); }

  /* ══════════════════════════════════════════════════════════════════════ */
  /* Snapshots                                                             */
  /* ══════════════════════════════════════════════════════════════════════ */
  .snapshots-layout { display: flex; flex-direction: column; gap: 0; }

  .snapshot-controls {
    display: flex;
    align-items: center;
    gap: 12px;
    margin-bottom: 12px;
  }
  .compare-hint {
    font-size: 12px;
    color: rgb(var(--clr) / 45%);
    font-style: italic;
  }

  /* ── Snapshot filter bar (mirrors Account.svelte snapshot tab) ─────── */
  .snap-filterbar {
    display: flex;
    flex-wrap: wrap;
    align-items: center;
    gap: 8px;
    padding: 10px 12px;
    margin-bottom: 8px;
    border-radius: 8px;
    border: 1px solid rgb(var(--clr) / 8%);
    background: rgb(var(--bg2) / 30%);
  }
  .snap-filterbar input {
    height: 32px;
    box-sizing: border-box;
  }
  .snap-search-wrap {
    position: relative;
    flex: 1 1 220px;
    min-width: 180px;
  }
  .snap-search-icon {
    position: absolute;
    left: 10px;
    top: 50%;
    transform: translateY(-50%);
    color: rgb(var(--clr) / 45%);
    pointer-events: none;
  }
  .snap-search-input {
    width: 100%;
    padding: 0 10px 0 30px;
    border-radius: 6px;
    border: 1px solid rgb(var(--clr) / 12%);
    background: rgb(var(--bg1));
    color: rgb(var(--clr) / 90%);
    font-size: 13px;
    outline: none;
    transition: border-color 0.15s;
  }
  .snap-search-input:focus { border-color: rgb(var(--clrPrm) / 60%); }
  .snap-search-input::placeholder { color: rgb(var(--clr) / 35%); }
  .snap-date {
    flex: 0 1 140px;
    min-width: 130px;
    padding: 0 8px;
    border-radius: 6px;
    border: 1px solid rgb(var(--clr) / 12%);
    background: rgb(var(--bg1));
    color: rgb(var(--clr) / 85%);
    font-size: 12px;
    font-family: inherit;
    outline: none;
    color-scheme: dark;
  }
  .snap-date:focus { border-color: rgb(var(--clrPrm) / 60%); }
  .snap-date-sep {
    color: rgb(var(--clr) / 45%);
    font-size: 14px;
    flex-shrink: 0;
  }
  .snap-clear {
    height: 32px;
    width: 32px;
    display: inline-flex;
    align-items: center;
    justify-content: center;
    border-radius: 6px;
    border: 1px solid rgb(var(--clr) / 12%);
    background: rgb(var(--clr) / 5%);
    color: rgb(var(--clr) / 70%);
    cursor: pointer;
    flex-shrink: 0;
    transition: all 0.15s;
  }
  .snap-clear:hover {
    background: rgb(var(--clr) / 10%);
    color: rgb(var(--clr) / 90%);
  }
  .snap-count {
    margin-left: auto;
    font-size: 11px;
    color: rgb(var(--clr) / 50%);
    font-variant-numeric: tabular-nums;
    padding: 4px 10px;
    border-radius: 4px;
    background: rgb(var(--clr) / 6%);
    flex-shrink: 0;
  }
  @media (max-width: 768px) {
    .snap-search-wrap { flex-basis: 100%; }
    .snap-date { flex: 1 1 calc(50% - 22px); }
    .snap-count { margin-left: 0; }
  }

  .snapshot-list { display: flex; flex-direction: column; gap: 0; }

  .snapshot-card { padding: 12px 16px; }
  .snapshot-card.compare-selected {
    border-color: rgb(var(--clrPrm) / 40%);
    background: rgb(var(--clrPrm) / 5%);
  }

  .snapshot-row {
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: 16px;
  }
  .snapshot-info { flex: 1; min-width: 0; }
  .snapshot-name {
    font-size: 14px;
    font-weight: 600;
    color: rgb(var(--clr) / 90%);
    margin-bottom: 4px;
  }
  .snapshot-meta {
    display: flex;
    align-items: center;
    gap: 6px;
    font-size: 11px;
    color: rgb(var(--clr) / 40%);
    flex-wrap: wrap;
  }
  .snapshot-meta .sep { color: rgb(var(--clr) / 15%); }
  .snapshot-actions {
    display: flex;
    gap: 6px;
    flex-shrink: 0;
  }

  /* Compare diff */
  .compare-card { margin-top: 4px; }
  .compare-card .card-header {
    justify-content: space-between;
  }
  .diff-row.diff-changed td { background: rgba(249, 226, 175, 0.04); }
  .diff-row.diff-added td { background: rgba(166, 227, 161, 0.04); }
  .diff-row.diff-removed td { background: rgba(243, 139, 168, 0.04); }
  .diff-old { opacity: 0.6; }
  .diff-badge {
    font-size: 10px;
    font-weight: 600;
    padding: 1px 6px;
    border-radius: 3px;
  }
  .diff-badge-changed { background: rgba(249, 226, 175, 0.15); color: #f9e2af; }
  .diff-badge-added { background: rgba(166, 227, 161, 0.15); color: #a6e3a1; }
  .diff-badge-removed { background: rgba(243, 139, 168, 0.15); color: #f38ba8; }

  /* ══════════════════════════════════════════════════════════════════════ */
  /* Loading / Empty / Error                                               */
  /* ══════════════════════════════════════════════════════════════════════ */
  .loading-state,
  .empty-state {
    display: flex;
    flex-direction: column;
    align-items: center;
    justify-content: center;
    padding: 48px;
    color: rgb(var(--clr) / 40%);
    gap: 12px;
    flex: 1;
  }
  .loading-state.compact { padding: 24px; }
  .empty-state h3 {
    font-size: 16px;
    color: rgb(var(--clr) / 60%);
    margin: 0;
  }
  .empty-state p {
    text-align: center;
    line-height: 1.5;
    margin: 0;
  }

  .spinner {
    width: 24px;
    height: 24px;
    border: 2px solid rgb(var(--clr) / 10%);
    border-top-color: rgb(var(--clrPrm));
    border-radius: 50%;
    animation: spin 0.8s linear infinite;
  }
  .spinner-sm {
    width: 14px;
    height: 14px;
    border: 2px solid rgb(var(--clr) / 10%);
    border-top-color: currentColor;
    border-radius: 50%;
    animation: spin 0.8s linear infinite;
    flex-shrink: 0;
  }
  @keyframes spin { to { transform: rotate(360deg); } }

  .error-banner {
    display: flex;
    align-items: center;
    gap: 8px;
    padding: 8px 16px;
    background: rgba(243, 139, 168, 0.08);
    color: #f38ba8;
    font-size: 12px;
    flex-shrink: 0;
    border-bottom: 1px solid rgba(243, 139, 168, 0.15);
  }
  .error-banner span { flex: 1; }
  .error-banner button {
    background: transparent;
    border: 1px solid rgba(243, 139, 168, 0.3);
    color: #f38ba8;
    padding: 2px 8px;
    border-radius: 3px;
    cursor: pointer;
    font-size: 11px;
    flex-shrink: 0;
  }
  .error-banner button:hover {
    background: rgba(243, 139, 168, 0.1);
  }
</style>
