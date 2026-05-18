<script lang="ts">
  import { fly, scale } from "svelte/transition";
  import {
    activeThing,
    appList,
    openedApps,
    minimizedApps,
    bringToFront,
  } from "$store/store";
  import { authStore } from "../store/auth";
  import { isMobile } from "$store/ui";
  import clickOutside from "$lib/clickOutside";
  import { _ } from "$store/i18n";

  /* App Launching: Use consistent behavior - Open if closed, Bring to Front if open */
  const toggleOpenApp = (app: string) => {
    if (!$openedApps.includes(app)) {
      $openedApps = [...$openedApps, app];
    }
    // Always restore if minimized
    $minimizedApps = $minimizedApps.filter((a) => a !== app);
    $activeThing = app;
    bringToFront(app);
  };

  async function handleLogout() {
    await authStore.logout();
    window.location.hash = "#login";
    $activeThing = "";
    window.location.reload();
  }

  function openAccountSettings() {
    toggleOpenApp("Account");
  }

  // Get greeting based on time
  function getGreeting(): string {
    const hour = new Date().getHours();
    if (hour < 12) return $_("start.goodMorning");
    if (hour < 17) return $_("start.goodAfternoon");
    return $_("start.goodEvening");
  }

  $: greeting = getGreeting();
  $: userName = $authStore.user?.fullName || $_("start.user");
  $: firstName = userName.split(" ")[0];
</script>

<div
  class="start activeShadow z-[9000]"
  class:mobile={$isMobile}
  transition:fly={{ y: $isMobile ? 100 : 50, duration: 250, opacity: 1 }}
  use:clickOutside={{
    callback: () => ($activeThing = ""),
    exclude: [document.querySelector(".bgLight")],
  }}
>
  <!-- Header with logo, greeting and user name -->
  <div class="start-header">
    <img
      src="img/icon/logo.svg"
      alt="Wantastic"
      class="start-logo"
      height="32"
      width="32"
    />
    <div class="greeting-section">
      <span class="greeting">{greeting},</span>
      <span class="user-name">{firstName}</span>
    </div>
  </div>

  <!-- Apps Grid - Scrollable -->
  <div class="apps-container">
    <div class="apps-grid">
      {#each $appList as app, i}
        <button
          class="app-item"
          on:click={() => toggleOpenApp(app)}
          transition:scale={{ duration: 150, delay: i * 30 }}
        >
          <div class="app-icon-wrapper">
            <img src="img/icon/{app}.svg" alt={app} />
          </div>
          <span class="app-name">{app}</span>
        </button>
      {/each}
    </div>
  </div>

  <!-- Bottom Bar - Bubble Style -->
  <div class="bottom-bar">
    <div class="bottom-bubble">
      <button
        class="bubble-btn user-btn"
        on:click={openAccountSettings}
        title={$_("start.accountSettings")}
      >
        <img src="img/apps/settings/defAccount.webp" alt="User" />
        <span class="bubble-label">{firstName}</span>
      </button>

      <div class="bubble-divider" />

      <button
        class="bubble-btn power-btn"
        on:click={handleLogout}
        title={$_("start.signOut")}
      >
        <svg
          xmlns="http://www.w3.org/2000/svg"
          width="20"
          height="20"
          viewBox="0 0 24 24"
          fill="none"
          stroke="currentColor"
          stroke-width="2"
          stroke-linecap="round"
          stroke-linejoin="round"
        >
          <path d="M18.36 6.64a9 9 0 1 1-12.73 0" />
          <line x1="12" y1="2" x2="12" y2="12" />
        </svg>
      </button>
    </div>
  </div>
</div>

<style>
  .start {
    position: absolute;
    bottom: var(--sp-3);
    left: 50%;
    transform: translateX(-50%);
    width: min(600px, calc(100vw - 24px));
    height: min(calc(100vh - 80px), 580px);
    border-radius: var(--radius-md);
    background: rgb(var(--bg2) / 92%);
    backdrop-filter: var(--glass-blur);
    display: flex;
    flex-direction: column;
    overflow: hidden;
  }

  .start.mobile {
    bottom: 0;
    left: 0;
    right: 0;
    transform: none;
    width: 100%;
    height: calc(100vh - 56px);
    border-radius: var(--radius-xl) var(--radius-xl) 0 0;
  }

  /* Header Section */
  .start-header {
    padding: var(--sp-8) var(--sp-8) var(--sp-6);
    text-align: center;
    display: flex;
    flex-direction: column;
    align-items: center;
    gap: var(--sp-3);
  }

  .start-logo {
    width: 40px;
    height: 40px;
    object-fit: contain;
  }

  .greeting-section {
    display: flex;
    flex-direction: column;
    align-items: center;
    gap: var(--sp-1);
  }

  .greeting {
    font-size: var(--text-base);
    font-weight: 400;
    opacity: 0.7;
    letter-spacing: 0.02em;
  }

  .user-name {
    font-size: var(--text-2xl);
    font-weight: 600;
    letter-spacing: -0.02em;
    background: linear-gradient(135deg, var(--primary), rgb(var(--clrPrmHov)));
    -webkit-background-clip: text;
    -webkit-text-fill-color: transparent;
    background-clip: text;
  }

  .start.mobile .start-header {
    padding: var(--sp-6) var(--sp-5) var(--sp-4);
  }

  .start.mobile .user-name {
    font-size: var(--text-xl);
  }

  /* Apps Container */
  .apps-container {
    flex: 1;
    overflow-y: auto;
    overflow-x: hidden;
    padding: 0 var(--sp-6) var(--sp-4);
  }

  .apps-grid {
    display: grid;
    grid-template-columns: repeat(auto-fill, minmax(90px, 1fr));
    gap: var(--sp-2);
    padding-bottom: var(--sp-4);
  }

  .start.mobile .apps-container {
    padding: 0 var(--sp-4) var(--sp-4);
  }

  .start.mobile .apps-grid {
    grid-template-columns: repeat(auto-fill, minmax(80px, 1fr));
    gap: var(--sp-3);
  }

  .app-item {
    display: flex;
    flex-direction: column;
    align-items: center;
    gap: var(--sp-2);
    padding: var(--sp-4) var(--sp-2);
    border-radius: var(--radius-sm);
    background: transparent;
    border: none;
    cursor: pointer;
    transition: var(--trans-normal);
    color: inherit;
  }

  .app-item:hover {
    background: rgb(var(--clr) / 6%);
  }

  .app-item:active {
    transform: scale(0.95);
    background: rgb(var(--clr) / 10%);
  }

  .app-icon-wrapper {
    width: 48px;
    height: 48px;
    display: flex;
    align-items: center;
    justify-content: center;
    border-radius: var(--radius-md);
    background: rgb(var(--bg8) / 80%);
    box-shadow: var(--shadow-sm);
    transition: var(--trans-normal);
  }

  .app-item:hover .app-icon-wrapper {
    transform: translateY(-2px);
    box-shadow: var(--shadow-md);
  }

  .app-icon-wrapper img {
    width: 32px;
    height: 32px;
  }

  .app-name {
    font-size: var(--text-xs);
    font-weight: 500;
    text-align: center;
    opacity: 0.9;
    max-width: 80px;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }

  .start.mobile .app-icon-wrapper {
    width: 52px;
    height: 52px;
  }

  /* Bottom Bar - Bubble Style */
  .bottom-bar {
    padding: var(--sp-4) var(--sp-6) var(--sp-5);
    display: flex;
    justify-content: center;
  }

  .start.mobile .bottom-bar {
    padding: var(--sp-3) var(--sp-4) var(--sp-6);
  }

  .bottom-bubble {
    display: flex;
    align-items: center;
    gap: var(--sp-2);
    padding: var(--sp-2) var(--sp-3);
    background: rgb(var(--bg8) / 95%);
    border-radius: var(--radius-full);
    box-shadow: var(--shadow-md), 0 0 0 1px rgb(var(--clr) / 6%);
    backdrop-filter: blur(20px);
  }

  .bubble-btn {
    display: flex;
    align-items: center;
    gap: 10px;
    padding: var(--sp-2) var(--sp-4);
    border-radius: var(--radius-full);
    border: none;
    background: transparent;
    cursor: pointer;
    color: inherit;
    transition: var(--trans-normal);
    font-size: var(--text-sm);
    font-weight: 500;
  }

  .bubble-btn:hover {
    background: rgb(var(--clr) / 8%);
  }

  .bubble-btn:active {
    transform: scale(0.97);
  }

  .user-btn img {
    width: 28px;
    height: 28px;
    border-radius: 50%;
    object-fit: cover;
  }

  .bubble-divider {
    width: 1px;
    height: 24px;
    background: rgb(var(--clr) / 12%);
  }

  .power-btn {
    padding: 10px;
  }

  /* Dark mode adjustments */
  @media (prefers-color-scheme: dark) {
    .start {
      background: rgb(var(--bg2) / 88%);
    }

    .app-icon-wrapper {
      background: rgb(var(--bg7) / 60%);
      box-shadow: 0 2px 8px rgb(0 0 0 / 20%);
    }

    .bottom-bubble {
      background: rgb(var(--bg7) / 90%);
      box-shadow: var(--shadow-lg), 0 0 0 1px rgb(255 255 255 / 6%);
    }
  }

  /* Scrollbar styling */
  .apps-container::-webkit-scrollbar {
    width: 6px;
  }

  .apps-container::-webkit-scrollbar-thumb {
    background: rgb(var(--clr) / 20%);
    border-radius: var(--radius-full);
  }

  .apps-container::-webkit-scrollbar-thumb:hover {
    background: rgb(var(--clr) / 30%);
  }
</style>
