<script lang="ts">
  import { openedApps, activeThing } from "$store/store";
  import { showTaskSwitcher } from "$store/ui";
  import { scale, fly } from "svelte/transition";

  // Display names for apps (especially sub-apps)
  const appDisplayNames: Record<string, string> = {
    SSHActivityViewer: "SSH Activity",
    WinboxActivityViewer: "Winbox Activity",
    SessionActivityViewer: "Session Activity",
    NewSSHSession: "New SSH",
    NewWinboxSession: "New Winbox",
    CreateGroupLink: "Create Group",
    PeerConfig: "Peer Config",
    AddPeer: "Add Peer",
    WinboxAccounts: "Winbox",
    WebBrowser: "Browser",
    RouterOSDashboard: "RouterOS",
  };

  // Icons for sub-apps that may not have SVG icons
  const appFallbackIcons: Record<string, string> = {
    SSHActivityViewer: "WebSSH",
    WinboxActivityViewer: "Winbox",
    SessionActivityViewer: "WebSSH",
    NewSSHSession: "WebSSH",
    NewWinboxSession: "Winbox",
    CreateGroupLink: "Topology",
    PeerConfig: "Peers",
    AddPeer: "Peers",
    WebBrowser: "Peers",
    RouterOSDashboard: "RouterOSDashboard",
  };

  function getDisplayName(app: string): string {
    // Handle SSHTerminal-xxx format
    if (app.startsWith("SSHTerminal-")) {
      return "Terminal";
    }
    return appDisplayNames[app] || app;
  }

  function getIconPath(app: string): string {
    // Handle SSHTerminal-xxx format
    if (app.startsWith("SSHTerminal-")) {
      return "img/icon/WebSSH.svg";
    }
    const iconName = appFallbackIcons[app] || app;
    return `img/icon/${iconName}.svg`;
  }

  function switchToApp(app: string) {
    $activeThing = app;
    $showTaskSwitcher = false;
  }

  function closeApp(app: string, event: Event) {
    event.stopPropagation();
    $openedApps = $openedApps.filter((a) => a !== app);
    if ($activeThing === app) {
      $activeThing = $openedApps[0] || "";
    }
  }

  function closeTaskSwitcher() {
    $showTaskSwitcher = false;
  }
</script>

{#if $showTaskSwitcher}
  <!-- svelte-ignore a11y-click-events-have-key-events -->
  <!-- svelte-ignore a11y-no-static-element-interactions -->
  <div
    class="task-switcher-overlay"
    on:click={closeTaskSwitcher}
    transition:fly={{ y: 300, duration: 200 }}
  >
    <!-- svelte-ignore a11y-click-events-have-key-events -->
    <!-- svelte-ignore a11y-no-static-element-interactions -->
    <div class="task-switcher" on:click|stopPropagation>
      <div class="task-header">
        <h3>{$openedApps.length} Open Apps</h3>
        <button
          class="close-all"
          on:click={() => {
            $openedApps = [];
            $showTaskSwitcher = false;
          }}
        >
          Close All
        </button>
      </div>

      <div class="task-grid">
        {#each $openedApps as app (app)}
          <!-- svelte-ignore a11y-click-events-have-key-events -->
          <!-- svelte-ignore a11y-no-static-element-interactions -->
          <div
            class="task-card"
            class:active={$activeThing === app}
            on:click={() => switchToApp(app)}
            transition:scale={{ duration: 150 }}
          >
            <div class="task-preview">
              <img
                class="app-icon"
                src={getIconPath(app)}
                alt={getDisplayName(app)}
                height="32"
                width="32"
              />
              <div class="app-name">{getDisplayName(app)}</div>
            </div>
            <button class="close-btn" on:click={(e) => closeApp(app, e)}>
              ×
            </button>
          </div>
        {/each}
      </div>

      {#if $openedApps.length === 0}
        <div class="empty-state">
          <p>No open apps</p>
          <p class="subtitle">Open an app from the taskbar</p>
        </div>
      {/if}
    </div>
  </div>
{/if}

<style lang="scss">
  .task-switcher-overlay {
    position: fixed;
    inset: 0;
    background: rgba(0, 0, 0, 0.7);
    z-index: 99999;
    display: flex;
    align-items: flex-end;
    backdrop-filter: blur(4px);
  }

  .task-switcher {
    background: rgb(var(--bg1));
    width: 100%;
    max-height: 70vh;
    border-radius: 16px 16px 0 0;
    padding: 20px;
    overflow-y: auto;
  }

  .task-header {
    display: flex;
    justify-content: space-between;
    align-items: center;
    margin-bottom: 20px;

    h3 {
      margin: 0;
      font-size: 20px;
      font-weight: 600;
    }

    .close-all {
      padding: 8px 16px;
      background: rgba(239, 68, 68, 0.1);
      color: #ef4444;
      border: 1px solid rgba(239, 68, 68, 0.3);
      border-radius: 8px;
      font-size: 14px;
      font-weight: 500;
      cursor: pointer;

      &:hover {
        background: rgba(239, 68, 68, 0.2);
      }
    }
  }

  .task-grid {
    display: grid;
    grid-template-columns: repeat(auto-fill, minmax(150px, 1fr));
    gap: 16px;
  }

  .task-card {
    position: relative;
    background: rgb(var(--bg3));
    border: 2px solid transparent;
    border-radius: 12px;
    padding: 16px;
    cursor: pointer;
    transition: all 0.2s;

    &.active {
      border-color: rgb(var(--clrPrm));
      background: rgba(var(--clrPrm), 0.1);
    }

    &:hover {
      transform: translateY(-4px);
      box-shadow: 0 4px 12px rgba(0, 0, 0, 0.15);
    }

    .task-preview {
      display: flex;
      flex-direction: column;
      align-items: center;
      gap: 12px;

      .app-icon {
        width: 48px;
        height: 48px;
        object-fit: contain;
      }

      .app-name {
        font-size: 14px;
        font-weight: 500;
        text-align: center;
      }
    }

    .close-btn {
      position: absolute;
      top: 8px;
      right: 8px;
      width: 28px;
      height: 28px;
      border-radius: 50%;
      background: rgba(0, 0, 0, 0.5);
      color: white;
      border: none;
      font-size: 20px;
      line-height: 1;
      cursor: pointer;
      display: flex;
      align-items: center;
      justify-content: center;

      &:hover {
        background: rgba(239, 68, 68, 0.9);
      }
    }
  }

  .empty-state {
    text-align: center;
    padding: 48px 24px;
    color: rgb(var(--clr) / 66%);

    p {
      margin: 0 0 8px 0;

      &.subtitle {
        font-size: 14px;
      }
    }
  }
</style>
