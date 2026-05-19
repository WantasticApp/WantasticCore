import { readable, writable, derived } from "svelte/store";

export let activeThing = writable("");

// Z-index management for window stacking
// Each time an app is focused, it gets a higher z-index
let zIndexCounter = 100;
export const appZIndexes = writable<Record<string, number>>({});

export function bringToFront(appName: string) {
  activeThing.set(appName);
  zIndexCounter += 1;
  appZIndexes.update((indexes) => ({
    ...indexes,
    [appName]: zIndexCounter,
  }));
}

// Helper to get z-index for an app (returns base if not set)
export function getAppZIndex(appName: string): number {
  let currentIndexes: Record<string, number> = {};
  appZIndexes.subscribe((v) => (currentIndexes = v))();
  return currentIndexes[appName] || 100;
}

export const appList = readable([
  "Peers",
  "WebSSH",
  "Winbox",
  "Topology",
  "Account",
  "Billing",
]);
export const openedApps = writable([]);
export const minimizedApps = writable<string[]>([]);

export let date = readable(new Date(), (set) => {
  const interval = setInterval(() => set(new Date()), 1000);
  return () => clearInterval(interval);
});

export let battery = writable(100);
export let brightness = writable(100);
export let speaker = writable(67);

export interface WidgetMaterialTheme {
  shellBg: string;
  shellBorder: string;
  shellBorderStrong: string;
  shellShadow: string;
  inkStrong: string;
  ink: string;
  inkSoft: string;
  inkMuted: string;
  headingMuted: string;
  panelBg: string;
  panelBgStrong: string;
  panelBorder: string;
  controlBg: string;
  controlBgHover: string;
  controlBorder: string;
  toolbarBg: string;
  toolbarBorder: string;
  emptyBg: string;
  emptyBorder: string;
  ambientTop: string;
  ambientBottom: string;
}

export interface DesktopBackgroundPreset {
  id: string;
  name: string;
  color: string;
  preview?: string;
  widgetTheme: WidgetMaterialTheme;
}

const baseWidgetTheme: WidgetMaterialTheme = {
  shellBg: "rgba(11, 17, 29, 0.44)",
  shellBorder: "rgba(167, 184, 214, 0.18)",
  shellBorderStrong: "rgba(218, 229, 248, 0.3)",
  shellShadow: "rgba(4, 9, 18, 0.22)",
  inkStrong: "rgba(246, 250, 255, 0.98)",
  ink: "rgba(232, 239, 248, 0.94)",
  inkSoft: "rgba(189, 203, 223, 0.88)",
  inkMuted: "rgba(154, 170, 193, 0.82)",
  headingMuted: "rgba(198, 210, 229, 0.78)",
  panelBg: "rgba(9, 16, 29, 0.15)",
  panelBgStrong: "rgba(9, 16, 29, 0.22)",
  panelBorder: "rgba(183, 200, 226, 0.12)",
  controlBg: "rgba(255, 255, 255, 0.04)",
  controlBgHover: "rgba(255, 255, 255, 0.1)",
  controlBorder: "rgba(210, 223, 245, 0.16)",
  toolbarBg:
    "linear-gradient(180deg, rgba(15, 22, 37, 0.36) 0%, rgba(11, 17, 30, 0.28) 100%)",
  toolbarBorder: "rgba(186, 204, 228, 0.14)",
  emptyBg:
    "linear-gradient(180deg, rgba(15, 22, 37, 0.32) 0%, rgba(11, 17, 30, 0.24) 100%)",
  emptyBorder: "rgba(196, 213, 236, 0.16)",
  ambientTop: "rgba(116, 145, 255, 0.18)",
  ambientBottom: "rgba(57, 119, 203, 0.12)",
};

function createWidgetTheme(
  overrides: Partial<WidgetMaterialTheme>
): WidgetMaterialTheme {
  return { ...baseWidgetTheme, ...overrides };
}

const DESKTOP_BACKGROUND_PRESETS: DesktopBackgroundPreset[] = [
  {
    id: "midnight-bloom",
    name: "Midnight Bloom",
    color:
      "radial-gradient(circle at 18% 18%, rgba(88, 113, 255, 0.28) 0%, transparent 34%), radial-gradient(circle at 82% 20%, rgba(70, 154, 255, 0.22) 0%, transparent 30%), linear-gradient(135deg, #07111f 0%, #111b32 48%, #091522 100%)",
    preview:
      "radial-gradient(circle at 18% 18%, rgba(88, 113, 255, 0.55) 0%, transparent 34%), radial-gradient(circle at 82% 20%, rgba(70, 154, 255, 0.38) 0%, transparent 30%), linear-gradient(135deg, #07111f 0%, #111b32 48%, #091522 100%)",
    widgetTheme: createWidgetTheme({
      ambientTop: "rgba(113, 131, 255, 0.18)",
      ambientBottom: "rgba(68, 139, 230, 0.12)",
    }),
  },
  {
    id: "aurora-tide",
    name: "Aurora Tide",
    color:
      "radial-gradient(circle at 20% 12%, rgba(61, 239, 210, 0.22) 0%, transparent 28%), radial-gradient(circle at 78% 18%, rgba(63, 151, 255, 0.18) 0%, transparent 26%), linear-gradient(135deg, #05151b 0%, #0b2f3b 44%, #12253a 100%)",
    preview:
      "radial-gradient(circle at 20% 12%, rgba(61, 239, 210, 0.42) 0%, transparent 28%), radial-gradient(circle at 78% 18%, rgba(63, 151, 255, 0.34) 0%, transparent 26%), linear-gradient(135deg, #05151b 0%, #0b2f3b 44%, #12253a 100%)",
    widgetTheme: createWidgetTheme({
      shellBg: "rgba(8, 23, 31, 0.42)",
      shellBorder: "rgba(149, 219, 226, 0.16)",
      shellBorderStrong: "rgba(207, 248, 245, 0.28)",
      headingMuted: "rgba(191, 226, 227, 0.78)",
      panelBorder: "rgba(170, 228, 231, 0.12)",
      controlBorder: "rgba(196, 240, 242, 0.18)",
      toolbarBorder: "rgba(176, 228, 228, 0.14)",
      emptyBorder: "rgba(192, 238, 240, 0.16)",
      ambientTop: "rgba(77, 241, 207, 0.18)",
      ambientBottom: "rgba(85, 165, 255, 0.12)",
    }),
  },
  {
    id: "violet-orbit",
    name: "Violet Orbit",
    color:
      "radial-gradient(circle at 18% 18%, rgba(152, 100, 255, 0.24) 0%, transparent 30%), radial-gradient(circle at 82% 20%, rgba(66, 148, 255, 0.16) 0%, transparent 26%), linear-gradient(135deg, #0a1022 0%, #22164a 46%, #101936 100%)",
    preview:
      "radial-gradient(circle at 18% 18%, rgba(152, 100, 255, 0.42) 0%, transparent 30%), radial-gradient(circle at 82% 20%, rgba(66, 148, 255, 0.28) 0%, transparent 26%), linear-gradient(135deg, #0a1022 0%, #22164a 46%, #101936 100%)",
    widgetTheme: createWidgetTheme({
      shellBg: "rgba(13, 14, 34, 0.46)",
      shellBorder: "rgba(190, 172, 255, 0.16)",
      shellBorderStrong: "rgba(231, 221, 255, 0.28)",
      inkSoft: "rgba(203, 195, 232, 0.88)",
      inkMuted: "rgba(170, 160, 207, 0.82)",
      headingMuted: "rgba(211, 201, 242, 0.78)",
      panelBorder: "rgba(199, 186, 255, 0.12)",
      ambientTop: "rgba(158, 111, 255, 0.18)",
      ambientBottom: "rgba(88, 142, 255, 0.12)",
    }),
  },
  {
    id: "forest-signal",
    name: "Forest Signal",
    color:
      "radial-gradient(circle at 18% 15%, rgba(44, 210, 134, 0.2) 0%, transparent 30%), radial-gradient(circle at 82% 18%, rgba(150, 255, 192, 0.12) 0%, transparent 22%), linear-gradient(135deg, #07160f 0%, #123324 46%, #0c1d15 100%)",
    preview:
      "radial-gradient(circle at 18% 15%, rgba(44, 210, 134, 0.38) 0%, transparent 30%), radial-gradient(circle at 82% 18%, rgba(150, 255, 192, 0.24) 0%, transparent 22%), linear-gradient(135deg, #07160f 0%, #123324 46%, #0c1d15 100%)",
    widgetTheme: createWidgetTheme({
      shellBg: "rgba(9, 23, 17, 0.44)",
      shellBorder: "rgba(162, 214, 174, 0.16)",
      shellBorderStrong: "rgba(221, 249, 228, 0.24)",
      inkSoft: "rgba(196, 220, 201, 0.86)",
      inkMuted: "rgba(156, 187, 164, 0.82)",
      headingMuted: "rgba(199, 223, 203, 0.76)",
      ambientTop: "rgba(61, 214, 132, 0.16)",
      ambientBottom: "rgba(126, 199, 147, 0.1)",
    }),
  },
  {
    id: "ember-circuit",
    name: "Ember Circuit",
    color:
      "radial-gradient(circle at 18% 20%, rgba(255, 124, 82, 0.22) 0%, transparent 28%), radial-gradient(circle at 80% 16%, rgba(255, 196, 96, 0.14) 0%, transparent 24%), linear-gradient(135deg, #1a1012 0%, #3a1319 46%, #241416 100%)",
    preview:
      "radial-gradient(circle at 18% 20%, rgba(255, 124, 82, 0.4) 0%, transparent 28%), radial-gradient(circle at 80% 16%, rgba(255, 196, 96, 0.28) 0%, transparent 24%), linear-gradient(135deg, #1a1012 0%, #3a1319 46%, #241416 100%)",
    widgetTheme: createWidgetTheme({
      shellBg: "rgba(26, 15, 19, 0.48)",
      shellBorder: "rgba(232, 171, 158, 0.16)",
      shellBorderStrong: "rgba(255, 219, 211, 0.28)",
      inkSoft: "rgba(226, 203, 198, 0.88)",
      inkMuted: "rgba(196, 165, 157, 0.82)",
      headingMuted: "rgba(228, 197, 191, 0.78)",
      ambientTop: "rgba(255, 136, 87, 0.16)",
      ambientBottom: "rgba(255, 190, 98, 0.1)",
    }),
  },
  {
    id: "obsidian-pulse",
    name: "Obsidian Pulse",
    color:
      "radial-gradient(circle at 18% 16%, rgba(112, 91, 255, 0.12) 0%, transparent 26%), radial-gradient(circle at 82% 84%, rgba(255, 80, 133, 0.12) 0%, transparent 24%), linear-gradient(135deg, #05070b 0%, #11141b 44%, #030406 100%)",
    preview:
      "radial-gradient(circle at 18% 16%, rgba(112, 91, 255, 0.28) 0%, transparent 26%), radial-gradient(circle at 82% 84%, rgba(255, 80, 133, 0.26) 0%, transparent 24%), linear-gradient(135deg, #05070b 0%, #11141b 44%, #030406 100%)",
    widgetTheme: createWidgetTheme({
      shellBg: "rgba(9, 11, 16, 0.4)",
      shellBorder: "rgba(198, 202, 214, 0.12)",
      shellBorderStrong: "rgba(235, 238, 244, 0.2)",
      panelBg: "rgba(255, 255, 255, 0.02)",
      panelBgStrong: "rgba(255, 255, 255, 0.045)",
      controlBg: "rgba(255, 255, 255, 0.045)",
      controlBgHover: "rgba(255, 255, 255, 0.1)",
      ambientTop: "rgba(114, 96, 255, 0.12)",
      ambientBottom: "rgba(255, 89, 147, 0.08)",
    }),
  },
  {
    id: "slate-horizon",
    name: "Slate Horizon",
    color:
      "radial-gradient(circle at 20% 14%, rgba(138, 164, 213, 0.14) 0%, transparent 28%), radial-gradient(circle at 82% 18%, rgba(121, 153, 255, 0.1) 0%, transparent 24%), linear-gradient(135deg, #121724 0%, #283347 48%, #161d2b 100%)",
    preview:
      "radial-gradient(circle at 20% 14%, rgba(138, 164, 213, 0.3) 0%, transparent 28%), radial-gradient(circle at 82% 18%, rgba(121, 153, 255, 0.2) 0%, transparent 24%), linear-gradient(135deg, #121724 0%, #283347 48%, #161d2b 100%)",
    widgetTheme: createWidgetTheme({
      shellBg: "rgba(15, 21, 35, 0.46)",
      shellBorder: "rgba(179, 195, 224, 0.16)",
      shellBorderStrong: "rgba(226, 236, 250, 0.28)",
      ambientTop: "rgba(146, 173, 222, 0.14)",
      ambientBottom: "rgba(116, 141, 255, 0.1)",
    }),
  },
  {
    id: "polar-current",
    name: "Polar Current",
    color:
      "radial-gradient(circle at 20% 18%, rgba(180, 228, 255, 0.18) 0%, transparent 30%), radial-gradient(circle at 82% 18%, rgba(127, 180, 255, 0.14) 0%, transparent 26%), linear-gradient(135deg, #0e1b2c 0%, #24435a 46%, #142233 100%)",
    preview:
      "radial-gradient(circle at 20% 18%, rgba(180, 228, 255, 0.34) 0%, transparent 30%), radial-gradient(circle at 82% 18%, rgba(127, 180, 255, 0.28) 0%, transparent 26%), linear-gradient(135deg, #0e1b2c 0%, #24435a 46%, #142233 100%)",
    widgetTheme: createWidgetTheme({
      shellBg: "rgba(11, 21, 34, 0.42)",
      shellBorder: "rgba(183, 223, 246, 0.18)",
      shellBorderStrong: "rgba(228, 243, 255, 0.28)",
      inkSoft: "rgba(207, 225, 239, 0.88)",
      inkMuted: "rgba(169, 193, 212, 0.82)",
      headingMuted: "rgba(205, 223, 235, 0.78)",
      ambientTop: "rgba(165, 225, 255, 0.18)",
      ambientBottom: "rgba(110, 171, 255, 0.12)",
    }),
  },
];

export const DEFAULT_DESKTOP_BACKGROUND = DESKTOP_BACKGROUND_PRESETS[0].color;

export function getDesktopBackgroundPreset(
  background: string | null | undefined
): DesktopBackgroundPreset {
  return (
    DESKTOP_BACKGROUND_PRESETS.find((preset) => preset.color === background) ||
    DESKTOP_BACKGROUND_PRESETS[0]
  );
}

// Desktop background presets
export const desktopBackgroundColors = readable(DESKTOP_BACKGROUND_PRESETS);

// Load saved background from localStorage, default to dark gradient
const savedBg =
  typeof window !== "undefined"
    ? localStorage.getItem("desktopBackground")
    : null;
export const desktopBackground = writable(
  savedBg || DEFAULT_DESKTOP_BACKGROUND
);

// Persist background changes to localStorage
if (typeof window !== "undefined") {
  desktopBackground.subscribe((value) => {
    localStorage.setItem("desktopBackground", value);
  });
}

export const themes = readable([
  "Windows/img0",
  "Windows/img19",
  "Glow/img20",
  "Captured Motion/img24",
  "Sunrise/img28",
  "Flow/img32",
]);
