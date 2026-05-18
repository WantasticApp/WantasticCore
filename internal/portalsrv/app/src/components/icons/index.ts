// Drop-in replacement for `lucide-svelte`'s named exports. Each module
// here is a small Svelte component wrapping LucideIcon with the inlined
// SVG path data, so the app has no transitive lucide-svelte dependency
// (which semver-includes vulnerable-by-advisory Svelte versions).

export { default as ArchiveIcon } from "./ArchiveIcon.svelte";
export { default as DatabaseIcon } from "./DatabaseIcon.svelte";
export { default as FileText } from "./FileText.svelte";
export { default as FolderIcon } from "./FolderIcon.svelte";
export { default as GitBranchIcon } from "./GitBranchIcon.svelte";
export { default as HomeIcon } from "./HomeIcon.svelte";
export { default as MenuIcon } from "./MenuIcon.svelte";
export { default as NetworkIcon } from "./NetworkIcon.svelte";
export { default as PencilIcon } from "./PencilIcon.svelte";
export { default as PlusIcon } from "./PlusIcon.svelte";
export { default as RadioIcon } from "./RadioIcon.svelte";
export { default as RefreshCcwIcon } from "./RefreshCcwIcon.svelte";
export { default as SearchIcon } from "./SearchIcon.svelte";
export { default as ServerIcon } from "./ServerIcon.svelte";
export { default as ShieldIcon } from "./ShieldIcon.svelte";
export { default as Trash2Icon } from "./Trash2Icon.svelte";
export { default as WrenchIcon } from "./WrenchIcon.svelte";
export { default as XIcon } from "./XIcon.svelte";

// `lucide-svelte` exports some icons with `Icon`-less names too — keep
// those aliases so the WuspDashboard's `import { Home as HomeIcon }` form
// keeps working without churn at the call sites.
export { default as Home } from "./HomeIcon.svelte";
export { default as Network } from "./NetworkIcon.svelte";
export { default as Database } from "./DatabaseIcon.svelte";
export { default as Wrench } from "./WrenchIcon.svelte";
export { default as Archive } from "./ArchiveIcon.svelte";
