<script lang="ts">
  import Router from "./Router.svelte";
  import { onMount } from "svelte";
  import { deferredPrompt, isStandalone } from "./store/ui";

  onMount(() => {
    // Check if app is already installed (standalone mode)
    const mediaQuery = window.matchMedia("(display-mode: standalone)");
    isStandalone.set(mediaQuery.matches);

    // Listen for changes in display mode
    mediaQuery.addEventListener("change", (evt) => {
      isStandalone.set(evt.matches);
    }); 

    // Capture the PWA install prompt event
    window.addEventListener("beforeinstallprompt", (e) => {
      // Prevent the mini-infobar from appearing on mobile
      e.preventDefault();
      // Stash the event so it can be triggered later.
      deferredPrompt.set(e);
      console.log(" [App] captured beforeinstallprompt event");
    });

    window.addEventListener("appinstalled", () => {
      // Hide the app-provided install promotion
      deferredPrompt.set(null);
      isStandalone.set(true);
      console.log(" [App] PWA was installed");
    });
  });
</script>

<Router />
