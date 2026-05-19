<!--
  Generic Lucide icon renderer. Each icon below is a Svelte component that
  wraps this with its specific path data. We bake the SVG paths in directly
  (lucide is MIT-licensed) so the app has no runtime dependency on the
  lucide-svelte package — which transitively pulls in a svelte version that
  GitHub advisories semver-include in vulnerable SSR ranges. Our app is
  pure CSR; the advisories don't apply to compiled output, but dropping
  the transitive dep clears the audit signal too.
-->
<script lang="ts">
  export let size: number | string = 24;
  /** Array of SVG path or shape primitives. Each entry is [tag, attrs]. */
  export let parts: Array<[string, Record<string, string | number>]> = [];
  export let strokeWidth: number | string = 2;
</script>

<svg
  xmlns="http://www.w3.org/2000/svg"
  width={size}
  height={size}
  viewBox="0 0 24 24"
  fill="none"
  stroke="currentColor"
  stroke-width={strokeWidth}
  stroke-linecap="round"
  stroke-linejoin="round"
>
  {#each parts as [tag, attrs]}
    {#if tag === "path"}
      <path {...attrs} />
    {:else if tag === "circle"}
      <circle {...attrs} />
    {:else if tag === "rect"}
      <rect {...attrs} />
    {:else if tag === "line"}
      <line {...attrs} />
    {:else if tag === "polyline"}
      <polyline {...attrs} />
    {:else if tag === "polygon"}
      <polygon {...attrs} />
    {:else if tag === "ellipse"}
      <ellipse {...attrs} />
    {/if}
  {/each}
</svg>
