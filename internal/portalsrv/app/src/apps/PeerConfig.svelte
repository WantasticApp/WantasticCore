<script lang="ts">
  import { scale, fly, fade } from "svelte/transition";
  import { createEventDispatcher, onMount } from "svelte";
  import {
    activeThing,
    appZIndexes,
    bringToFront,
    openedApps,
  } from "$store/store";
  import {
    peerStore,
    type EnrollmentToken,
    type PeerConfigTab,
  } from "$store/peer";
  import { authStore } from "$store/auth";
  import { wsStore } from "$store/websocket";
  import { _ } from "$store/i18n";
  import Titlebar from "$components/shared/Titlebar.svelte";
  import QRCode from "qrcode";

  // Backup upload token — one-time use, refreshed each time config opens
  let backupToken = "";
  let backupUrl = "";

  const dispatch = createEventDispatcher();

  // Get data from store instead of props
  $: selectedPeerConfig = $peerStore.selectedPeerConfig;
  $: preferredTab =
    (selectedPeerConfig?.preferredTab as PeerConfigTab | undefined) ||
    undefined;
  $: peers = $peerStore.peers;
  $: peerId = selectedPeerConfig?.peerId || "";
  $: peer = peers.find((p) => p.id === peerId) || null;
  $: configData = selectedPeerConfig?.config;

  // Window state
  let isMaximized = false;
  let isMinimized = false;
  let windowEl: HTMLDivElement;
  let isDragging = false;
  let dragOffset = { x: 0, y: 0 };
  let position = { x: 0, y: 0 };
  let initialized = false;

  // Tab management
  type TabId = "wireguard" | "mikrotik" | "unix" | "qrcode";
  const tabOrder: TabId[] = ["wireguard", "mikrotik", "unix", "qrcode"];
  let activeTab: TabId = "mikrotik";
  let appliedPreferredTab: PeerConfigTab | null = null;
  let transitionDirection = 1; // 1 = right, -1 = left

  // Copy state
  let copied = false;
  let copyTimeout: ReturnType<typeof setTimeout>;

  // QR Code
  let qrCodeDataUrl = "";
  let lastQrKey = "";

  // Security warnings
  let securityWarnings: string[] = [];

  const DEFAULT_PORTAL_URL = "https://console.wantastic.app";

  // Enrollment tokens for embedded agent selection
  let selectedTokenId = "";
  let tokenBootstrapPending = false;
  let tokenBootstrapError = "";
  $: availableTokens = $peerStore.tokens;
  $: if (!selectedTokenId) {
    const usableToken = pickUsableToken(availableTokens);
    if (usableToken) {
      selectedTokenId = usableToken.id;
    }
  }
  $: selectedToken =
    availableTokens.find((t) => t.id === selectedTokenId) ||
    pickUsableToken(availableTokens) ||
    (availableTokens.length > 0 ? availableTokens[0] : null);

  // Config markers for cleanup (used in RouterOS comments)
  const WG_COMMENT = "WANTASTIC-WG";

  // Z-index for window stacking
  $: zIndex = $appZIndexes["PeerConfig"] || 100;
  $: if (preferredTab && preferredTab !== appliedPreferredTab) {
    const currentIndex = tabOrder.indexOf(activeTab);
    const nextIndex = tabOrder.indexOf(preferredTab as TabId);
    transitionDirection = nextIndex >= currentIndex ? 1 : -1;
    activeTab = preferredTab as TabId;
    appliedPreferredTab = preferredTab;
  }
  $: if (!preferredTab) {
    appliedPreferredTab = null;
  }

  // Watch activeThing to restore when clicked from taskbar
  $: if ($activeThing === "PeerConfig" && isMinimized) {
    isMinimized = false;
  }

  // Bring to front when activated
  $: if ($activeThing === "PeerConfig") {
    bringToFront("PeerConfig");
  }

  function handleFocus() {
    $activeThing = "PeerConfig";
    bringToFront("PeerConfig");
  }

  onMount(async () => {
    // Center the window initially
    if (windowEl && !initialized) {
      const rect = windowEl.getBoundingClientRect();
      position = {
        x: (window.innerWidth - rect.width) / 2,
        y: (window.innerHeight - rect.height) / 2,
      };
      initialized = true;
    }
    await generateQRCode();
    checkSecurityWarnings();

    await ensureSetupToken();
  });

  function handleClose() {
    peerStore.clearSelectedPeerConfig();
    $activeThing = "";
    $openedApps = $openedApps.filter((oa) => oa !== "PeerConfig");
    dispatch("close");
  }

  function handleMaximize() {
    isMaximized = !isMaximized;
  }

  function handleReduce() {
    isMinimized = true;
    $activeThing = "";
  }

  function handleMouseDown(e: MouseEvent) {
    if (isMaximized) return;
    const target = e.target as HTMLElement;
    // Only start drag from title bar area
    if (!target.closest(".title-bar")) return;
    if (target.closest("button")) return;

    isDragging = true;
    handleFocus();
    const rect = windowEl.getBoundingClientRect();
    dragOffset = {
      x: e.clientX - rect.left,
      y: e.clientY - rect.top,
    };
  }

  function handleMouseMove(e: MouseEvent) {
    if (!isDragging) return;
    position = {
      x: e.clientX - dragOffset.x,
      y: e.clientY - dragOffset.y,
    };
  }

  function handleMouseUp() {
    isDragging = false;
  }

  function shellQuote(value: string) {
    return `'${String(value || "").replace(/'/g, `'\\''`)}'`;
  }

  function setupTokenName() {
    return `UNIX Setup · ${peer?.name || "wantasticd"}`;
  }

  function tokenExpiryTime(value: EnrollmentToken["expires_at"]) {
    if (!value) {
      return 0;
    }
    if (typeof value === "string") {
      const parsed = Date.parse(value);
      return Number.isNaN(parsed) ? 0 : parsed;
    }
    if (typeof value === "object" && typeof value.seconds === "number") {
      const nanos = typeof value.nanos === "number" ? value.nanos : 0;
      return value.seconds * 1000 + Math.floor(nanos / 1_000_000);
    }
    return 0;
  }

  function isUsableToken(token: EnrollmentToken | null | undefined) {
    if (!token?.token) {
      return false;
    }
    const expiry = tokenExpiryTime(token.expires_at);
    if (expiry > 0 && expiry <= Date.now()) {
      return false;
    }
    if (
      typeof token.max_uses === "number" &&
      token.max_uses > 0 &&
      typeof token.usage_count === "number" &&
      token.usage_count >= token.max_uses
    ) {
      return false;
    }
    return true;
  }

  function pickUsableToken(tokens: EnrollmentToken[]) {
    return tokens.find((token) => isUsableToken(token)) || null;
  }

  async function ensureSetupToken() {
    const tenantId = $authStore.tenant_id;
    if (!tenantId) {
      return;
    }
    tokenBootstrapError = "";
    tokenBootstrapPending = true;
    try {
      await peerStore.listTokens(tenantId);
      const existingToken = pickUsableToken($peerStore.tokens);
      if (existingToken) {
        selectedTokenId = existingToken.id;
        return;
      }
      const created = await peerStore.createToken(
        tenantId,
        setupTokenName(),
        7,
        3,
      );
      if (created?.id) {
        selectedTokenId = created.id;
      }
    } catch (err: any) {
      tokenBootstrapError = err?.message || "Failed to prepare token";
      console.error("Failed to prepare enrollment token:", err);
    } finally {
      tokenBootstrapPending = false;
    }
  }

  async function generateQRCode() {
    try {
      // Use server-provided QR code if available
      if (configData?.qr_code) {
        qrCodeDataUrl = `data:image/png;base64,${configData.qr_code}`;
        return;
      }
      // Otherwise generate from config
      if (wireguardConfig) {
        qrCodeDataUrl = await QRCode.toDataURL(wireguardConfig, {
          width: 256,
          margin: 2,
          color: { dark: "#000000", light: "#ffffff" },
        });
      }
    } catch (err) {
      console.error("Failed to generate QR code:", err);
    }
  }

  function checkSecurityWarnings() {
    securityWarnings = [];
    const checkAllowedIPs = allowedIPs || "10.100.0.0/24";

    // Warn about 0.0.0.0/0 route (full tunnel)
    if (checkAllowedIPs.includes("0.0.0.0/0")) {
      securityWarnings.push($_("peers.config.fullTunnelWarning"));
    }
  }

  $: {
    const nextQrKey = `${configData?.qr_code || ""}::${wireguardConfig}`;
    if (nextQrKey && nextQrKey !== lastQrKey) {
      lastQrKey = nextQrKey;
      void generateQRCode();
    }
  }

  $: checkSecurityWarnings();

  async function copyToClipboard(text: string) {
    try {
      await navigator.clipboard.writeText(text);
      copied = true;
      if (copyTimeout) clearTimeout(copyTimeout);
      copyTimeout = setTimeout(() => (copied = false), 2000);
    } catch (err) {
      console.error("Failed to copy:", err);
    }
  }

  // Parse WireGuard config from server response
  function parseWireGuardConfig(config: string) {
    const lines = config.split("\n");
    const parsed: Record<string, string> = {};

    for (const line of lines) {
      const trimmed = line.trim();
      if (trimmed.includes("=")) {
        const [key, ...valueParts] = trimmed.split("=");
        parsed[key.trim()] = valueParts.join("=").trim();
      }
    }
    return parsed;
  }

  // Raw WireGuard config from server
  $: wireguardConfig = configData?.config || "";

  // Parse values from config for RouterOS/OpenWrt generation
  $: parsedConfig = parseWireGuardConfig(wireguardConfig);
  $: serverEndpoint = parsedConfig["Endpoint"] || "overlay.wantastic.app:51820";
  $: serverPublicKey = parsedConfig["PublicKey"] || "SERVER_PUBLIC_KEY";
  $: peerPrivateKey = parsedConfig["PrivateKey"] || "YOUR_PRIVATE_KEY";
  $: peerAddress =
    parsedConfig["Address"] || peer?.assigned_ip || "10.100.0.2/32";
  $: allowedIPs = parsedConfig["AllowedIPs"] || "10.100.0.0/24";
  $: dns = parsedConfig["DNS"] || "1.1.1.1";
  $: persistentKeepalive = parseInt(
    parsedConfig["PersistentKeepalive"] || "25",
  );
  $: mtu = parseInt(parsedConfig["MTU"] || "1420");
  $: peerName = peer?.name || "overlay-peer";
  $: endpointHost = serverEndpoint.split(":")[0] || "overlay.wantastic.app";
  $: endpointPort = serverEndpoint.split(":")[1] || "51820";
  function getRouterOsRoutes(allowedIPs: string): string {
    const routes = allowedIPs.split(",").map((ip) => ip.trim());
    return routes
      .map(
        (ip) =>
          `/ip route add dst-address=${ip} gateway=wg-wantastic comment="${WG_COMMENT}"`,
      )
      .join("\n");
  }
  $: allowedIPsClean = allowedIPs.replaceAll(" ", "");

  $: pKeyClean = peerPrivateKey.replace(/"/g, '\\"').replaceAll(" ", "").trim();
  $: pubKeyClean = serverPublicKey
    .replace(/"/g, '\\"')
    .replaceAll(" ", "")
    .trim();

  // init.rsc has a single source: the file deployed at get.wantastic.app.
  // Hook URL stays origin-based so dev portals POST backups to themselves.
  $: portalOrigin = typeof window !== 'undefined' ? window.location.origin : 'https://console.wantastic.app';
  $: initURL = 'https://get.wantastic.app/init.rsc';
  $: hookBaseURL = `${portalOrigin}/hooks/backup`;

  // MikroTik script — wrapped in a single {} block so :local declarations are
  // visible across all subsequent lines (terminal treats each top-level line as
  // its own scope, so :local pk on line 1 is invisible on line 2 without {}).
  // We deliberately do NOT use :do {} on-error={} — RouterOS v7 does not
  // populate $error in that legacy form, so wrapping it would silently hide
  // the real RouterOS error from the operator.
  $: mikrotikConfig = `{
  /tool fetch url="${initURL}" mode=https
  /import init.rsc
  :local pk "${pKeyClean}"
  :local pub "${pubKeyClean}"
  :local addr "${peerAddress}"
  :local nets "${allowedIPsClean}"
  :global wantasticDeploy
  \$wantasticDeploy privateKey=(\$pk) publicKey=(\$pub) endpoint="${endpointHost}:${endpointPort}" address=(\$addr) allowedIPs=(\$nets) mtu="${mtu}" keepalive="${persistentKeepalive}"${backupToken ? ` backupToken="${backupToken}" peerName="${peerName}" hookURL="${hookBaseURL}"` : ''}${backupToken ? `
  :delay 5s
  :global wantasticBackup
  \$wantasticBackup` : ''}
}
`;

  $: setupToken = selectedToken?.token || configData?.setup_token || "";
  $: quotedSetupToken = shellQuote(setupToken);
  $: quotedPortalOrigin = shellQuote(portalOrigin);

  // UNIX / wantasticd installer script
  $: unixConfig = `#!/bin/sh
# Wantastic UNIX Setup Script
# Generated: ${new Date().toISOString()}
# Peer: ${peerName}
#
# Installs wantasticd and logs in with an enrollment token.
# Manual login command:
#   ${portalOrigin === DEFAULT_PORTAL_URL ? "wantasticd login --token <TOKEN>" : `wantasticd login --portal-url ${portalOrigin} --token <TOKEN>`}

set -eu

PORTAL_URL=${quotedPortalOrigin}
DEFAULT_PORTAL_URL='${DEFAULT_PORTAL_URL}'
TOKEN=${quotedSetupToken}
INSTALL_URL='https://get.wantastic.app/install.sh'

fetch_install_script() {
  if command -v curl >/dev/null 2>&1; then
    curl -fsSL "$INSTALL_URL"
    return 0
  fi
  if command -v wget >/dev/null 2>&1; then
    wget -qO- "$INSTALL_URL"
    return 0
  fi
  echo "Error: curl or wget is required." >&2
  return 1
}

if [ -z "$TOKEN" ]; then
  echo "Error: no enrollment token is available for this setup." >&2
  echo "Create a token in Wantastic and re-open this configuration window." >&2
  exit 1
fi

print_login_command() {
  if [ "$PORTAL_URL" = "$DEFAULT_PORTAL_URL" ]; then
    echo "wantasticd login --token $TOKEN"
  else
    echo "wantasticd login --portal-url $PORTAL_URL --token $TOKEN"
  fi
}

echo "==========================================="
echo "  Wantastic UNIX Setup"
echo "==========================================="
echo "Portal: $PORTAL_URL"
echo "Peer: ${peerName}"
echo ""

run_install() {
  if [ "$PORTAL_URL" = "$DEFAULT_PORTAL_URL" ]; then
    sh -s -- --token "$TOKEN"
  else
    sh -s -- --portal-url "$PORTAL_URL" --token "$TOKEN"
  fi
}

run_install_sudo() {
  if [ "$PORTAL_URL" = "$DEFAULT_PORTAL_URL" ]; then
    sudo sh -s -- --token "$TOKEN"
  else
    sudo sh -s -- --portal-url "$PORTAL_URL" --token "$TOKEN"
  fi
}

if [ "$(id -u)" -eq 0 ]; then
  fetch_install_script | run_install
elif command -v sudo >/dev/null 2>&1; then
  fetch_install_script | run_install_sudo
else
  echo "Error: root privileges are required. Re-run as root or install sudo." >&2
  echo "Manual login command after install:" >&2
  echo "  $(print_login_command)" >&2
  exit 1
fi

echo ""
echo "Equivalent manual login command:"
echo "  $(print_login_command)"
`;

  const tabs: Array<{ id: TabId; label: string }> = [
    { id: "mikrotik", label: "peers.config.tabs.mikrotik" },
    { id: "wireguard", label: "peers.config.tabs.wireguard" },
    { id: "unix", label: "peers.config.tabs.openwrt" },
    { id: "qrcode", label: "Mobile" },
  ];

  function switchTab(tabId: TabId) {
    const currentIndex = tabOrder.indexOf(activeTab);
    const newIndex = tabOrder.indexOf(tabId);
    transitionDirection = newIndex > currentIndex ? 1 : -1;
    activeTab = tabId;
  }

  // Generate a fresh one-time backup token when the config dialog opens
  $: if (peerId) {
    generateBackupToken(peerId);
  }

  async function generateBackupToken(pid: string) {
    try {
      const resp = await wsStore.callGRPC<{
        success: boolean;
        error?: string;
        upload_token: string;
        upload_url: string;
      }>("WUSPService", "GenerateBackupToken", { peer_id: pid });
      if (resp.success && resp.upload_token) {
        backupToken = resp.upload_token;
        backupUrl = resp.upload_url;
        console.log("[PeerConfig] Backup token generated:", backupToken.substring(0, 8) + "...");
      } else {
        console.warn("[PeerConfig] GenerateBackupToken returned:", resp.error || "no token");
        backupToken = "";
      }
    } catch (e: any) {
      console.warn("[PeerConfig] GenerateBackupToken failed:", e?.message || e);
      backupToken = "";
      backupUrl = "";
    }
  }

  $: getActiveConfig = (function getActiveConfig(): string {
    switch (activeTab) {
      case "mikrotik":
        return mikrotikConfig;
      case "wireguard":
        return wireguardConfig;
      case "unix":
        return unixConfig;
      default:
        return wireguardConfig;
    }
  })();

  function downloadConfig() {
    const config = getActiveConfig;
    const filename =
      activeTab === "unix"
        ? "wantasticd-unix-setup.sh"
        : activeTab === "mikrotik"
          ? "wantastic-overlay.rsc"
          : "wantastic-overlay.conf";

    const blob = new Blob([config], { type: "text/plain" });
    const url = URL.createObjectURL(blob);
    const a = document.createElement("a");
    a.href = url;
    a.download = filename;
    a.click();
    URL.revokeObjectURL(url);
  }
</script>

<svelte:window on:mousemove={handleMouseMove} on:mouseup={handleMouseUp} />

<div
  class="peer-config activeShadow"
  class:maximized={isMaximized}
  class:minimized={isMinimized}
  style:z-index={zIndex}
  style:left="{position.x}px"
  style:top="{position.y}px"
  bind:this={windowEl}
  on:mousedown={handleFocus}
  on:touchstart={handleFocus}
  transition:scale={{ duration: 200 }}
>
  <div on:mousedown={handleMouseDown}>
    <Titlebar
      title={$_("peers.peerConfig")}
      appName={"PeerConfig"}
      on:close={handleClose}
      on:maximize={handleMaximize}
      on:reduce={handleReduce}
    >
      <svg
        width="16"
        height="16"
        viewBox="0 0 24 24"
        fill="none"
        stroke="currentColor"
        stroke-width="2"
      >
        <rect x="3" y="11" width="18" height="11" rx="2" />
        <path d="M7 11V7a5 5 0 0110 0v4" />
      </svg>
      <span class="appName pl-2"
        >{$_("peers.peerConfig")}{peer?.name ? ` — ${peer.name}` : ""}</span
      >
    </Titlebar>
  </div>

  <div class="mainApp">
    <!-- Security Warnings -->
    {#if securityWarnings.length > 0}
      <div class="warning-banner">
        <svg
          width="16"
          height="16"
          viewBox="0 0 24 24"
          fill="none"
          stroke="currentColor"
          stroke-width="2"
        >
          <path
            d="M10.29 3.86L1.82 18a2 2 0 001.71 3h16.94a2 2 0 001.71-3L13.71 3.86a2 2 0 00-3.42 0z"
          />
          <line x1="12" y1="9" x2="12" y2="13" /><line
            x1="12"
            y1="17"
            x2="12.01"
            y2="17"
          />
        </svg>
        <div class="warning-content">
          <strong>{$_("peers.config.securityConsiderations")}</strong>
          <ul>
            {#each securityWarnings as warning}
              <li>{warning}</li>
            {/each}
          </ul>
        </div>
      </div>
    {/if}

    <!-- Tab Navigation -->
    <div class="tabs">
      <button
        class="tab-btn"
        class:active={activeTab === "mikrotik"}
        on:click={() => switchTab("mikrotik")}
      >
        <svg
          xmlns="http://www.w3.org/2000/svg"
          width="26"
          height="26"
          version="1.2"
          viewBox="0 0 610 610"
        >
          <path
            fill-rule="evenodd"
            d="M586.8 193.4v222.5c0 13.8-1.7 25.6-5.5 34.3-.7 1.6-1.5 3.2-2.3 4.7-5.5 8.9-16.6 17.7-31.6 25.9L344.4 592c-12.6 6.9-24.2 11.4-34 12.7q-2.8.4-5.4.4-2.7 0-5.5-.4c-9.8-1.3-21.4-5.8-34-12.7L164 536.4 62.6 480.8c-15.1-8.2-26.2-17-31.6-25.9-5.5-9-7.9-22.5-7.9-39V193.4c0-13.8 1.7-25.5 5.5-34.2.7-1.7 1.5-3.3 2.4-4.7q1.3-2.2 3-4.3c6.1-7.5 16-14.7 28.6-21.7L164 72.9l101.5-55.6c15-8.2 28.6-13 39.5-13q2.6 0 5.4.4c9.8 1.2 21.4 5.7 34 12.6l101.5 55.6 101.5 55.6c12.6 7 22.4 14.2 28.5 21.7q1.8 2.1 3.1 4.3c.8 1.4 1.6 3 2.3 4.7 3.8 8.7 5.5 20.4 5.5 34.2m-102.5 33.2c0-9.8-5.3-18.8-13.8-23.4l-152.7-83.7c-8-4.4-17.7-4.4-25.7 0l-38.9 21.3c-4.6 2.6-4.6 9.2 0 11.7l116.4 63.8c4.6 2.6 4.6 9.2 0 11.7l-51.8 28.4c-8 4.4-17.7 4.4-25.7 0l-112-61.4c-8-4.4-17.7-4.4-25.7 0l-14.9 8.2c-8.6 4.7-13.9 13.6-13.9 23.4v7l135.5 74.3c8.6 4.6 13.9 13.6 13.9 23.3v141.4c0 4.8 2.6 9.3 6.9 11.7l10.2 5.6c8 4.4 17.7 4.4 25.7 0l10.3-5.6c4.2-2.4 6.9-6.9 6.9-11.7V331.2c0-9.7 5.3-18.7 13.9-23.3l65.5-36c4.5-2.4 9.9.8 9.9 5.9v142.4c0 5.1 5.4 8.3 9.9 5.9l36.3-19.9c8.5-4.7 13.8-13.7 13.8-23.4zm-298.7 78.2c0-4.8-2.6-9.3-6.9-11.7l-43.2-23.7c-4.5-2.4-9.9.8-9.9 5.9v107.5c0 9.7 5.3 18.7 13.9 23.4l36.3 19.9c4.4 2.4 9.8-.8 9.8-5.9z"
            style="fill:currentColor"
          />
        </svg>
        <span>{$_("peers.config.tabs.mikrotik")}</span>
      </button>
      <button
        class="tab-btn"
        class:active={activeTab === "unix"}
        on:click={() => switchTab("unix")}
      >
        <svg
          xmlns="http://www.w3.org/2000/svg"
          width="24"
          height="24"
          viewBox="0 0 24 24"
          fill="none"
          stroke="currentColor"
          stroke-width="2"
          stroke-linecap="round"
          stroke-linejoin="round"
          ><rect x="3" y="4" width="18" height="14" rx="2" /><path
            d="m7 8 3 3-3 3"
          /><path d="M13 14h4" /></svg
        >
        <span>{$_("peers.config.tabs.openwrt")}</span>
      </button>
      <button
        class="tab-btn"
        class:active={activeTab === "wireguard"}
        on:click={() => switchTab("wireguard")}
      >
        <svg
          xmlns="http://www.w3.org/2000/svg"
          height="26"
          width="26"
          viewBox="0 0 300 300"
          ><g fill="currentColor"
            ><path
              d="M299.745 145.56S306.685 0 146.705 0C5.225 0 .805 139.63.805 139.63S-20.005 300 149.965 300c163.02 0 149.78-154.44 149.78-154.44zm-197.8-50.863c30.017-18.364 68.366-7.14 82.735 20.476 2.724 5.234 3.07 13.291 1.345 18.782-5.955 18.956-20.014 29.587-39.312 34.103 5.69-4.87 10.218-10.394 11.659-18.025a26.402 26.402 0 00-4.543-20.956 26.76 26.76 0 00-30.81-9.39c-11.882 4.512-18.39 15.355-17.217 28.684 1.09 12.38 10.484 20.405 28.061 23.453-2.627 1.39-4.65 2.414-6.63 3.517a63.918 63.918 0 00-20.543 17.868c-1.784 2.408-3.01 2.602-5.727.941-35.338-21.61-37.61-75.844.982-99.453zm-26.449 133.53c-5.677 1.441-11.178 3.574-16.98 5.478 2.838-19.152 25.264-36.789 44.23-34.777a48.881 48.881 0 00-9.243 25.893c-6.302 1.161-12.24 1.942-18.007 3.405zm120.79-186.98c5.61.206 11.23.12 16.844.254 1.402.092 2.794.286 4.168.58a40.607 40.607 0 01-4.236 5.434c-2.007 1.87-4.275 3.698-7.166.856-.696-.684-2.339-.527-3.549-.543-5.582-.073-11.172-.252-16.746-.041a104.04 104.04 0 00-14.425 1.473c-.894.16-2.23 3.131-1.819 4.227.97 2.585 2.383 5.436 4.478 7.09 7.74 6.11 15.972 11.595 23.748 17.663 7.556 5.897 14.589 12.358 18.875 21.253 5.584 11.59 5.747 23.743 3.339 35.95-4.02 20.378-14.333 37.261-31.032 49.524-6.729 4.941-15.06 7.746-22.767 11.295-6.778 3.123-13.755 5.812-20.55 8.901-12.248 5.57-19.132 18.865-17.107 32.688 1.858 12.685 12.987 23.271 25.735 25.456 15.292 2.622 31.07-7.316 34.812-22.86 4.206-17.478-5.29-33.083-23.065-37.813-.783-.208-1.569-.405-3.201-.827 4.754-2.124 8.861-3.638 12.653-5.724a347.934 347.934 0 0019.48-11.562c1.875-1.2 2.887-1.2 4.486.182 12.225 10.57 19.518 23.718 21.563 39.84 3.384 26.683-9.247 51.197-33.072 63.761-36.86 19.44-81.965-2.686-90.106-43.552-6.974-35.003 17.73-66.754 47.462-72.884 12.787-2.636 24.48-7.96 33.57-17.807 5.865-6.354 8.708-11.806 9.677-14.266a39.565 39.565 0 002.721-14.469 33.867 33.867 0 00-2.965-12.398c-3.104-7.075-14.995-18.33-17.94-20.704l-28-21.92c-.987-.813-2.099-.754-4.507-.591-2.861.194-10.175.599-13.331-.228 2.553-1.933 9.513-4.746 12.502-7.007-9.074-6.13-19.43-3.916-28.941-5.747 2.199-4.095 13.08-10.39 19.27-11.09a91.533 91.533 0 00-1.688-10.282c-.378-1.391-1.931-2.74-3.286-3.535-3.286-1.927-6.77-3.517-10.55-5.433a21.936 21.936 0 0111.333-3.505A42.316 42.316 0 01134.3 23.99c6.742 1.54 12.124.535 17.488-4.048-4.222-1.7-8.444-3.253-12.538-5.09a123.04 123.04 0 01-11.78-6.159c10.623 1.476 20.897 5.459 31.758 4.004l.277-1.481-25.229-5.873c15.04-1.376 29.042-1.604 42.301 4.855 3.731 1.817 7.635 3.321 11.211 5.397 1.744 1.012 2.919 3.008 4.35 4.56 1.136 1.232 2.05 2.883 3.446 3.626 5.3 2.818 11.134 2.929 17.078 2.787l.13-1.993c5.983 1.87 12.715 8.768 12.704 13.806-9.69 0-19.374-.037-29.056.054-1.034.01-2.062.766-3.093 1.175.98.571 1.943 1.6 2.942 1.637z"
            /><path
              d="M183.785 26.906a1.48 1.48 0 00-.189 2.369 2.233 2.233 0 003.072.821c.933-.47 1.848-.97 2.975-1.566-.908-.775-1.636-1.415-2.385-2.032-1.318-1.086-2.411-.404-3.473.408z"
            /></g
          ></svg
        >
        <span>{$_("peers.config.tabs.wireguard")}</span>
      </button>
      <button
        class="tab-btn"
        class:active={activeTab === "qrcode"}
        on:click={() => switchTab("qrcode")}
      >
        <svg
          xmlns="http://www.w3.org/2000/svg"
          width="24"
          height="24"
          viewBox="0 0 24 24"
          ><!-- Icon from Material Design Icons by Pictogrammers - https://github.com/Templarian/MaterialDesign/blob/master/LICENSE --><path
            fill="currentColor"
            d="M4 4h6v6H4zm16 0v6h-6V4zm-6 11h2v-2h-2v-2h2v2h2v-2h2v2h-2v2h2v3h-2v2h-2v-2h-3v2h-2v-4h3zm2 0v3h2v-3zM4 20v-6h6v6zM6 6v2h2V6zm10 0v2h2V6zM6 16v2h2v-2zm-2-5h2v2H4zm5 0h4v4h-2v-2H9zm2-5h2v4h-2zM2 2v4H0V2a2 2 0 0 1 2-2h4v2zm20-2a2 2 0 0 1 2 2v4h-2V2h-4V0zM2 18v4h4v2H2a2 2 0 0 1-2-2v-4zm20 4v-4h2v4a2 2 0 0 1-2 2h-4v-2z"
          /></svg
        >
        <span>{$_("peers.config.tabs.qrcode")}</span>
      </button>
    </div>

    <!-- Tab Content -->
    <div class="content">
      {#key activeTab}
        <div
          class="tab-panel"
          in:fly={{ x: transitionDirection * 30, duration: 200, delay: 50 }}
          out:fly={{ x: transitionDirection * -30, duration: 150 }}
        >
          {#if activeTab === "qrcode"}
            <div class="qr-container">
              {#if qrCodeDataUrl}
                <img
                  src={qrCodeDataUrl}
                  alt="WireGuard QR Code"
                  class="qr-code"
                />
                <p class="qr-hint">{$_("peers.config.scanHint")}</p>
              {:else}
                <div class="qr-loading">
                  <div class="spinner" />
                  <p>{$_("peers.config.generatingQR")}</p>
                </div>
              {/if}
            </div>
          {:else}
            <div class="config-panel flex flex-col justify-between">
              <div class="config-header">
                <div class="config-type">
                  {#if activeTab === "wireguard"}
                    <span class="badge"
                      >{$_("peers.config.types.standard")}</span
                    >
                    <span class="desc"
                      >{$_("peers.config.types.wireguardConfig")}</span
                    >
                  {:else if activeTab === "mikrotik"}
                    <span class="badge mikrotik"
                      >{$_("peers.config.types.routeros")}</span
                    >
                    <span class="desc"
                      >{$_("peers.config.types.mikrotikScript")}</span
                    >
                  {:else if activeTab === "unix"}
                    <span class="badge unix"
                      >{$_("peers.config.types.shell")}</span
                    >
                    <span class="desc"
                      >{$_("peers.config.types.openwrtScript")}</span
                    >
                  {/if}
                </div>

                {#if activeTab === "unix"}
                  <div class="token-meta">
                    {#if tokenBootstrapPending}
                      <span class="desc">Preparing enrollment token…</span>
                    {:else if selectedToken}
                      <span class="desc"
                        >Token: {selectedToken.name}</span
                      >
                    {:else if tokenBootstrapError}
                      <span class="desc token-error"
                        >{tokenBootstrapError}</span
                      >
                    {/if}
                  </div>
                {/if}

                <div class="config-actions">
                  <button
                    class="btn-action"
                    on:click={() => copyToClipboard(getActiveConfig)}
                  >
                    {#if copied}
                      <svg
                        width="16"
                        height="16"
                        viewBox="0 0 24 24"
                        fill="none"
                        stroke="currentColor"
                        stroke-width="2"
                      >
                        <polyline points="20 6 9 17 4 12" />
                      </svg>
                      <span>{$_("common.copied")}</span>
                    {:else}
                      <svg
                        width="16"
                        height="16"
                        viewBox="0 0 24 24"
                        fill="none"
                        stroke="currentColor"
                        stroke-width="2"
                      >
                        <rect x="9" y="9" width="13" height="13" rx="2" /><path
                          d="M5 15H4a2 2 0 01-2-2V4a2 2 0 012-2h9a2 2 0 012 2v1"
                        />
                      </svg>
                      <span>{$_("common.copy")}</span>
                    {/if}
                  </button>
                  <button class="btn-action" on:click={downloadConfig}>
                    <svg
                      width="16"
                      height="16"
                      viewBox="0 0 24 24"
                      fill="none"
                      stroke="currentColor"
                      stroke-width="2"
                    >
                      <path d="M21 15v4a2 2 0 01-2 2H5a2 2 0 01-2-2v-4" />
                      <polyline points="7 10 12 15 17 10" />
                      <line x1="12" y1="15" x2="12" y2="3" />
                    </svg>
                    <span>{$_("common.download")}</span>
                  </button>
                </div>
              </div>

              <div class="relative group">
                <pre
                  class="config-code bg-gray-950/80 border border-gray-800 p-4 rounded-xl font-mono text-xs text-blue-300 leading-relaxed overflow-x-auto"><code
                    >{getActiveConfig}</code
                  ></pre>
              </div>
            </div>
          {/if}

          <!-- Info Section -->
        </div>
      {/key}
    </div>
  </div>
</div>

<style lang="scss">
  .peer-config {
    background: var(--mica);
    position: absolute;
    border-radius: 8px;
    overflow: hidden;
    resize: both;
    width: min(680px, 95vw);
    min-height: min(480px, 80vh);
    max-height: 90vh;
    display: flex;
    flex-direction: column;
    box-shadow: 0 24px 48px rgba(0, 0, 0, 0.3);
  }

  .peer-config.maximized {
    position: fixed !important;
    top: 0 !important;
    left: 0 !important;
    width: 100vw !important;
    height: calc(100vh - 48px) !important;
    max-height: none;
    border-radius: 0;
    resize: none;
  }

  .peer-config.minimized {
    display: none;
  }

  .title-bar {
    cursor: grab;
    &:active {
      cursor: grabbing;
    }
  }

  .mainApp {
    display: flex;
    flex-direction: column;
    flex: 1;
    overflow: hidden;
  }

  // Warning Banner
  .warning-banner {
    display: flex;
    gap: 12px;
    padding: 12px 16px;
    margin: 12px 16px 0;
    background: rgba(245, 158, 11, 0.1);
    border: 1px solid rgba(245, 158, 11, 0.25);
    border-radius: 6px;
    color: #f59e0b;

    svg {
      flex-shrink: 0;
      margin-top: 2px;
    }

    .warning-content {
      flex: 1;

      strong {
        display: block;
        font-size: 12px;
        font-weight: 600;
        margin-bottom: 4px;
      }

      ul {
        margin: 0;
        padding-left: 16px;
        font-size: 11px;
        line-height: 1.5;
        opacity: 0.9;
      }
    }
  }

  // Tabs
  .tabs {
    display: flex;
    gap: 4px;
    padding: 12px 16px;
    background: rgb(var(--bg2) / 40%);
    border-bottom: 1px solid rgb(var(--clr) / 8%);
    flex-shrink: 0;
  }

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
    flex: 1;
    min-width: 0;

    svg {
      opacity: 0.6;
      flex-shrink: 0;
      transition: all 0.2s ease;
    }

    &:hover {
      color: rgb(var(--clr) / 85%);
      background: rgb(var(--clr) / 6%);
      border-color: rgb(var(--clr) / 10%);

      svg {
        opacity: 0.8;
      }
    }

    &.active {
      color: rgb(var(--clrPrm));
      background: rgb(var(--clrPrm) / 12%);
      border-color: rgb(var(--clrPrm) / 25%);
      font-weight: 600;

      svg {
        opacity: 1;
        color: rgb(var(--clrPrm));
      }
    }
  }

  // Content Area
  .content {
    flex: 1;
    overflow: hidden;
    position: relative;
  }

  .tab-panel {
    position: absolute;
    inset: 0;
    padding: 16px;
    overflow-y: auto;
    display: flex;
    flex-direction: column;
    gap: 16px;
  }

  // Config Panel
  .config-panel {
    display: flex;
    flex-direction: column;
    flex: 1;
    min-height: 0;
    justify-content: flex-start;
  }

  .config-header {
    display: flex;
    align-items: center;
    justify-content: space-between;
    margin-bottom: 12px;
    flex-wrap: wrap;
    gap: 12px;
  }

  .config-type {
    display: flex;
    align-items: center;
    gap: 10px;
  }

  .token-meta {
    display: flex;
    align-items: center;
    gap: 8px;
    min-height: 18px;
  }

  .badge {
    padding: 4px 10px;
    background: rgb(var(--clrPrm));
    color: #fff;
    border-radius: 4px;
    font-size: 11px;
    font-weight: 600;
    text-transform: uppercase;
    letter-spacing: 0.5px;

    &.mikrotik {
      background: #293239;
    }

    &.unix {
      background: #00a4dc;
    }

    &.wantastic {
      background: #7c4dff;
    }
  }

  .desc {
    font-size: 12px;
    color: rgb(var(--clr) / 60%);
  }

  .token-error {
    color: #f59e0b;
  }

  .config-actions {
    display: flex;
    gap: 8px;
  }

  .btn-action {
    display: flex;
    align-items: center;
    gap: 6px;
    padding: 8px 14px;
    background: rgb(var(--bg3) / 80%);
    border: 1px solid rgb(var(--clr) / 10%);
    border-radius: 6px;
    color: rgb(var(--clr) / 90%);
    font-size: 12px;
    font-weight: 500;
    cursor: pointer;
    transition: all 0.15s ease;

    svg {
      flex-shrink: 0;
    }

    &:hover {
      background: rgb(var(--clrPrm));
      border-color: rgb(var(--clrPrm));
      color: #fff;
    }
  }

  .config-code {
    flex: 1;
    margin: 0;
    padding: 14px;
    background: rgb(var(--bg1));
    border: 1px solid rgb(var(--clr) / 8%);
    border-radius: 6px;
    overflow: auto;
    max-height: auto;
    height: auto;
    min-height: 100px;
    code {
      font-family: "SF Mono", "Cascadia Code", "Fira Code", monospace;
      font-size: 11px;
      line-height: 1.6;
      color: rgb(var(--clr) / 85%);
      white-space: pre;
    }
  }

  // QR Code
  .qr-container {
    display: flex;
    flex-direction: column;
    align-items: center;
    justify-content: center;
    padding: 32px;
    background: rgb(var(--bg2) / 50%);
    border: 1px solid rgb(var(--clr) / 8%);
    border-radius: 8px;
  }

  .qr-code {
    width: 200px;
    height: 200px;
    border-radius: 8px;
    background: #fff;
    padding: 12px;
    transition: transform 0.3s cubic-bezier(0.175, 0.885, 0.32, 1.275);
    cursor: zoom-in;

    &:hover {
      transform: scale(1.5);
      z-index: 10;
      box-shadow:
        0 20px 25px -5px rgba(0, 0, 0, 0.1),
        0 8px 10px -6px rgba(0, 0, 0, 0.1);
    }
  }

  .qr-hint {
    margin: 16px 0 0;
    font-size: 12px;
    color: rgb(var(--clr) / 50%);
  }

  .qr-loading {
    text-align: center;
    padding: 20px;

    p {
      margin: 10px 0 0;
      font-size: 12px;
      color: rgb(var(--clr) / 50%);
    }
  }

  .spinner {
    width: 24px;
    height: 24px;
    border: 2px solid rgb(var(--clr) / 20%);
    border-top-color: rgb(var(--clrPrm));
    border-radius: 50%;
    animation: spin 0.8s linear infinite;
    margin: 0 auto;
  }

  @keyframes spin {
    to {
      transform: rotate(360deg);
    }
  }

  // Scrollbar
  .content::-webkit-scrollbar,
  .config-code::-webkit-scrollbar {
    width: 8px;
    height: 8px;
  }

  .content::-webkit-scrollbar-track,
  .config-code::-webkit-scrollbar-track {
    background: transparent;
  }

  .content::-webkit-scrollbar-thumb,
  .config-code::-webkit-scrollbar-thumb {
    background: rgb(var(--clr) / 15%);
    border-radius: 4px;
  }

  .content::-webkit-scrollbar-thumb:hover,
  .config-code::-webkit-scrollbar-thumb:hover {
    background: rgb(var(--clr) / 25%);
  }

  // Responsive
  @media (max-width: 600px) {
    .peer-config {
      width: 100%;
      height: 100%;
      max-width: 100%;
      max-height: 100%;
      border-radius: 0;
      top: 0 !important;
      left: 0 !important;
    }

    .tab-btn span {
      display: none;
    }

    .tab-btn svg {
      width: 18px;
      height: 18px;
    }

    .info-grid {
      grid-template-columns: 1fr;
    }

    .config-header {
      flex-direction: column;
      align-items: stretch;
    }

    .config-actions {
      width: 100%;
    }

    .btn-action {
      flex: 1;
      justify-content: center;
    }
  }

  .embedded-card {
    display: flex;
    gap: 16px;
    padding: 20px;
    background: rgb(var(--bg2) / 50%);
    border: 1px solid rgb(var(--clr) / 8%);
    border-radius: 8px;
    margin-bottom: 16px;
  }

  .embedded-icon {
    display: flex;
    align-items: center;
    justify-content: center;
    width: 48px;
    height: 48px;
    background: rgb(var(--clrPrm) / 10%);
    color: rgb(var(--clrPrm));
    border-radius: 8px;
    flex-shrink: 0;
  }

  .embedded-content {
    flex: 1;
    display: flex;
    flex-direction: column;
    gap: 8px;

    h4 {
      margin: 0;
      font-size: 14px;
      font-weight: 600;
      color: rgb(var(--clr));
    }

    p {
      margin: 0;
      font-size: 13px;
      line-height: 1.5;
      color: rgb(var(--clr) / 60%);
    }
  }

  .token-selection {
    display: flex;
    flex-direction: column;
    gap: 4px;
    margin-top: 12px;

    label {
      font-size: 11px;
      font-weight: 600;
      color: rgb(var(--clr) / 50%);
      text-transform: uppercase;
      letter-spacing: 0.5px;
    }
  }

  .select-wrapper {
    position: relative;
    max-width: 320px;
  }

  .select-wrapper select {
    width: 100%;
    appearance: none;
    background: rgb(var(--bg1));
    border: 1px solid rgb(var(--clr) / 10%);
    border-radius: 6px;
    padding: 8px 32px 8px 12px;
    font-size: 13px;
    color: rgb(var(--clr));
    outline: none;
    transition: all 0.2s;

    &:focus {
      border-color: rgb(var(--clrPrm));
      box-shadow: 0 0 0 2px rgb(var(--clrPrm) / 10%);
    }
  }

  .select-arrow {
    position: absolute;
    right: 10px;
    top: 50%;
    transform: translateY(-50%);
    pointer-events: none;
    color: rgb(var(--clr) / 50%);
  }

  @media (max-width: 600px) {
    .embedded-card {
      flex-direction: column;
    }

    .embedded-icon {
      width: 40px;
      height: 40px;
    }

    .select-wrapper {
      max-width: 100%;
    }
  }
</style>
