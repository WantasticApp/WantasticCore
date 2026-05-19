import "unplugin-icons/types/svelte";
export {};

declare global {
  namespace App {
    // interface Error {}
    // interface Locals {}
    // interface PageData {}
    // interface Platform {}
  }
  interface Window {
    BatteryManager: any;
  }

  interface Navigator {
    getBattery: () => Promise<{
      charging: boolean;
      chargingTime: number;
      dischargingTime: number;
      level: number;
      onchargingchange: any;
      onchargingtimechange: any;
      ondischargingtimechange: any;
      onlevelchange: any;
    }>;
  }
}
