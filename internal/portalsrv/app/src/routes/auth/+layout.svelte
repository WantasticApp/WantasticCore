<script lang="ts">
  import { isAuthenticated } from "../../store/auth";
  import { onMount } from "svelte";

  let shouldRedirect = false;

  onMount(() => {
    // Subscribe to auth state to redirect if already authenticated
    const unsubscribe = isAuthenticated.subscribe((auth) => {
      if (auth) {
        shouldRedirect = true;
        window.location.href = "/dashboard";
      }
    });

    return unsubscribe;
  });
</script>

{#if !shouldRedirect}
  <div class="auth-layout">
    <slot />
  </div>
{/if}

<style>
  :global(.auth-layout) {
    min-height: 100vh;
    display: flex;
    flex-direction: column;
    background: linear-gradient(135deg, #0a0e27 0%, #1a1f3a 100%);
  }
</style>
