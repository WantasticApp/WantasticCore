import { writable, derived } from "svelte/store";

// Track window width
export const windowWidth = writable(
  typeof window !== "undefined" ? window.innerWidth : 1024
);

// Derived store for mobile detection
export const isMobile = derived(windowWidth, ($width) => $width <= 768);

// Task switcher state
export const showTaskSwitcher = writable(false);

// PWA Install state
export const deferredPrompt = writable<any>(null);
export const isStandalone = writable(false);

// Update window width on resize
if (typeof window !== "undefined") {
  window.addEventListener("resize", () => {
    windowWidth.set(window.innerWidth);
  });
}
