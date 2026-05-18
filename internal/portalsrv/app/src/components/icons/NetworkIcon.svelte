<script lang="ts">
  import LucideIcon from "../LucideIcon.svelte";
  import { NetworkIcon } from "./data";
  export let size: number | string = 24;
  export let strokeWidth: number | string = 2;
</script>
<LucideIcon parts={NetworkIcon} {size} {strokeWidth} />
