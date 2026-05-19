<script lang="ts">
  import LucideIcon from "../LucideIcon.svelte";
  import { FolderIcon } from "./data";
  export let size: number | string = 24;
  export let strokeWidth: number | string = 2;
</script>
<LucideIcon parts={FolderIcon} {size} {strokeWidth} />
