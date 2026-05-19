import { writable } from "svelte/store";

export type ToastType = "error" | "success" | "warning" | "info";

export interface Toast {
  id: number;
  message: string;
  type: ToastType;
  duration: number;
}

function createToastStore() {
  const { subscribe, update } = writable<Toast[]>([]);
  let nextId = 0;

  function addToast(
    message: string,
    type: ToastType = "info",
    duration: number = 5000
  ) {
    const id = nextId++;
    const toast: Toast = { id, message, type, duration };

    update((toasts) => [...toasts, toast]);

    if (duration > 0) {
      setTimeout(() => {
        removeToast(id);
      }, duration);
    }

    return id;
  }

  function removeToast(id: number) {
    update((toasts) => toasts.filter((t) => t.id !== id));
  }

  return {
    subscribe,
    error: (message: string, duration = 5000) =>
      addToast(message, "error", duration),
    success: (message: string, duration = 3000) =>
      addToast(message, "success", duration),
    warning: (message: string, duration = 4000) =>
      addToast(message, "warning", duration),
    info: (message: string, duration = 3000) =>
      addToast(message, "info", duration),
    remove: removeToast,
  };
}

export const toasts = createToastStore();
