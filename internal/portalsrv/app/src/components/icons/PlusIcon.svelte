<script lang="ts">
  import LucideIcon from "../LucideIcon.svelte";
  import { PlusIcon } from "./data";
  export let size: number | string = 24;
  export let strokeWidth: number | string = 2;
</script>
<LucideIcon parts={PlusIcon} {size} {strokeWidth} />
