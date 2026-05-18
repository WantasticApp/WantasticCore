// Lucide icon path data. Vendored from lucide@0.469.0 (MIT) so we don't
// depend on `lucide-svelte` (whose svelte peer dep semver-includes versions
// that GitHub advisories flag, even though the SSR-only vulns don't apply
// to our pure-CSR build).
//
// Each entry is an array of [tag, attrs] tuples matching the Lucide SVG.
// Add a new icon by copy-pasting from https://lucide.dev/icons/<slug>.

export type IconParts = Array<[string, Record<string, string | number>]>;

export const ArchiveIcon: IconParts = [
  ["rect", { width: 20, height: 5, x: 2, y: 3, rx: 1 }],
  ["path", { d: "M4 8v11a2 2 0 0 0 2 2h12a2 2 0 0 0 2-2V8" }],
  ["path", { d: "M10 12h4" }],
];

export const DatabaseIcon: IconParts = [
  ["ellipse", { cx: 12, cy: 5, rx: 9, ry: 3 }],
  ["path", { d: "M3 5V19A9 3 0 0 0 21 19V5" }],
  ["path", { d: "M3 12A9 3 0 0 0 21 12" }],
];

export const FileText: IconParts = [
  ["path", { d: "M15 2H6a2 2 0 0 0-2 2v16a2 2 0 0 0 2 2h12a2 2 0 0 0 2-2V7Z" }],
  ["path", { d: "M14 2v4a2 2 0 0 0 2 2h4" }],
  ["path", { d: "M10 9H8" }],
  ["path", { d: "M16 13H8" }],
  ["path", { d: "M16 17H8" }],
];

export const FolderIcon: IconParts = [
  ["path", { d: "M20 20a2 2 0 0 0 2-2V8a2 2 0 0 0-2-2h-7.9a2 2 0 0 1-1.69-.9L9.6 3.9A2 2 0 0 0 7.93 3H4a2 2 0 0 0-2 2v13a2 2 0 0 0 2 2Z" }],
];

export const GitBranchIcon: IconParts = [
  ["line", { x1: 6, x2: 6, y1: 3, y2: 15 }],
  ["circle", { cx: 18, cy: 6, r: 3 }],
  ["circle", { cx: 6, cy: 18, r: 3 }],
  ["path", { d: "M18 9a9 9 0 0 1-9 9" }],
];

export const HomeIcon: IconParts = [
  ["path", { d: "m3 9 9-7 9 7v11a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2z" }],
  ["polyline", { points: "9 22 9 12 15 12 15 22" }],
];

export const MenuIcon: IconParts = [
  ["line", { x1: 4, x2: 20, y1: 12, y2: 12 }],
  ["line", { x1: 4, x2: 20, y1: 6, y2: 6 }],
  ["line", { x1: 4, x2: 20, y1: 18, y2: 18 }],
];

export const NetworkIcon: IconParts = [
  ["rect", { x: 16, y: 16, width: 6, height: 6, rx: 1 }],
  ["rect", { x: 2, y: 16, width: 6, height: 6, rx: 1 }],
  ["rect", { x: 9, y: 2, width: 6, height: 6, rx: 1 }],
  ["path", { d: "M5 16v-3a1 1 0 0 1 1-1h12a1 1 0 0 1 1 1v3" }],
  ["path", { d: "M12 12V8" }],
];

export const PencilIcon: IconParts = [
  ["path", { d: "M21.174 6.812a1 1 0 0 0-3.986-3.987L3.842 16.174a2 2 0 0 0-.5.83l-1.321 4.352a.5.5 0 0 0 .623.622l4.353-1.32a2 2 0 0 0 .83-.497z" }],
  ["path", { d: "m15 5 4 4" }],
];

export const PlusIcon: IconParts = [
  ["path", { d: "M5 12h14" }],
  ["path", { d: "M12 5v14" }],
];

export const RadioIcon: IconParts = [
  ["path", { d: "M4.9 19.1C1 15.2 1 8.8 4.9 4.9" }],
  ["path", { d: "M7.8 16.2c-2.3-2.3-2.3-6.1 0-8.5" }],
  ["circle", { cx: 12, cy: 12, r: 2 }],
  ["path", { d: "M16.2 7.8c2.3 2.3 2.3 6.1 0 8.5" }],
  ["path", { d: "M19.1 4.9C23 8.8 23 15.1 19.1 19" }],
];

export const RefreshCcwIcon: IconParts = [
  ["path", { d: "M21 12a9 9 0 0 0-9-9 9.75 9.75 0 0 0-6.74 2.74L3 8" }],
  ["path", { d: "M3 3v5h5" }],
  ["path", { d: "M3 12a9 9 0 0 0 9 9 9.75 9.75 0 0 0 6.74-2.74L21 16" }],
  ["path", { d: "M16 16h5v5" }],
];

export const SearchIcon: IconParts = [
  ["circle", { cx: 11, cy: 11, r: 8 }],
  ["path", { d: "m21 21-4.3-4.3" }],
];

export const ServerIcon: IconParts = [
  ["rect", { width: 20, height: 8, x: 2, y: 2, rx: 2, ry: 2 }],
  ["rect", { width: 20, height: 8, x: 2, y: 14, rx: 2, ry: 2 }],
  ["line", { x1: 6, x2: "6.01", y1: 6, y2: 6 }],
  ["line", { x1: 6, x2: "6.01", y1: 18, y2: 18 }],
];

export const ShieldIcon: IconParts = [
  ["path", { d: "M20 13c0 5-3.5 7.5-7.66 8.95a1 1 0 0 1-.67-.01C7.5 20.5 4 18 4 13V6a1 1 0 0 1 1-1c2 0 4.5-1.2 6.24-2.72a1.17 1.17 0 0 1 1.52 0C14.51 3.81 17 5 19 5a1 1 0 0 1 1 1z" }],
];

export const Trash2Icon: IconParts = [
  ["path", { d: "M3 6h18" }],
  ["path", { d: "M19 6v14c0 1-1 2-2 2H7c-1 0-2-1-2-2V6" }],
  ["path", { d: "M8 6V4c0-1 1-2 2-2h4c1 0 2 1 2 2v2" }],
  ["line", { x1: 10, x2: 10, y1: 11, y2: 17 }],
  ["line", { x1: 14, x2: 14, y1: 11, y2: 17 }],
];

export const WrenchIcon: IconParts = [
  ["path", { d: "M14.7 6.3a1 1 0 0 0 0 1.4l1.6 1.6a1 1 0 0 0 1.4 0l3.77-3.77a6 6 0 0 1-7.94 7.94l-6.91 6.91a2.12 2.12 0 0 1-3-3l6.91-6.91a6 6 0 0 1 7.94-7.94l-3.76 3.76z" }],
];

export const XIcon: IconParts = [
  ["path", { d: "M18 6 6 18" }],
  ["path", { d: "m6 6 12 12" }],
];
