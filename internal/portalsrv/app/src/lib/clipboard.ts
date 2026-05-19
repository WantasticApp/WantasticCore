/**
 * Clipboard utility functions for copy-on-click functionality
 * Provides a reusable copyToClipboard function with toast notifications
 */

import { toasts } from "$store/toast";

/**
 * Copies text to clipboard and shows a toast notification
 * @param text - The text to copy
 * @param label - Optional label for the toast message (e.g., "IP", "Key", "Value")
 * @returns Promise<boolean> - true if copy succeeded, false otherwise
 */
export async function copyToClipboard(
  text: string,
  label: string = "Value"
): Promise<boolean> {
  if (!text) {
    return false;
  }

  try {
    await navigator.clipboard.writeText(text);
    toasts.success(`${label} copied to clipboard`);
    return true;
  } catch (err) {
    console.error("Failed to copy to clipboard:", err);
    // Fallback for older browsers
    try {
      const textArea = document.createElement("textarea");
      textArea.value = text;
      textArea.style.position = "fixed";
      textArea.style.left = "-9999px";
      textArea.style.top = "-9999px";
      document.body.appendChild(textArea);
      textArea.focus();
      textArea.select();
      const successful = document.execCommand("copy");
      document.body.removeChild(textArea);

      if (successful) {
        toasts.success(`${label} copied to clipboard`);
        return true;
      } else {
        toasts.error("Failed to copy to clipboard");
        return false;
      }
    } catch (fallbackErr) {
      console.error("Fallback copy also failed:", fallbackErr);
      toasts.error("Failed to copy to clipboard");
      return false;
    }
  }
}

/**
 * Type-specific copy functions for common use cases
 */
export const copy = {
  ip: (text: string) => copyToClipboard(text, "IP address"),
  key: (text: string) => copyToClipboard(text, "Key"),
  config: (text: string) => copyToClipboard(text, "Configuration"),
  port: (text: string | number) => copyToClipboard(String(text), "Port"),
  service: (text: string) => copyToClipboard(text, "Service"),
  hostname: (text: string) => copyToClipboard(text, "Hostname"),
  banner: (text: string) => copyToClipboard(text, "Banner"),
  mac: (text: string) => copyToClipboard(text, "MAC address"),
  id: (text: string) => copyToClipboard(text, "ID"),
  url: (text: string) => copyToClipboard(text, "URL"),
  email: (text: string) => copyToClipboard(text, "Email"),
  value: (text: string) => copyToClipboard(text, "Value"),
  text: (text: string, label?: string) =>
    copyToClipboard(text, label || "Text"),
};
