import { svelte } from "@sveltejs/vite-plugin-svelte";
import sveltePreprocess from "svelte-preprocess";
import { defineConfig } from "vite";
import compression from "vite-plugin-compression";
import Icons from "unplugin-icons/vite";
import path from "path";

// https://vitejs.dev/config/
export default defineConfig({
  base: "./",
  plugins: [
    svelte({
      preprocess: sveltePreprocess({
        scss: {
          silenceDeprecations: ["legacy-js-api"],
        },
      }),
      // Silence unused CSS selector warnings
      onwarn: (warning, handler) => {
        if (warning.code === "css-unused-selector") return;
      },
      compilerOptions: {
        // Don't emit warnings for unused CSS
        css: "injected",
      },
    }),
    Icons({
      compiler: "svelte",
      autoInstall: true
    }),
    compression(),
  ],
  server: {
    // Allow requests from Go proxy on port 8001
    cors: true,
    origin: "https://wantastic.local",
    // Allow the container hostname when running in Docker
    allowedHosts: ["wantastic-frontend", "wantastic-portal-frontend"],
  },
  resolve: {
    alias: {
      $store: path.resolve(__dirname, "./src/store"),
      $apps: path.resolve(__dirname, "./src/apps"),
      $components: path.resolve(__dirname, "./src/components"),
      $lib: path.resolve(__dirname, "./src/lib"),
    },
  },
  build: {
    emptyOutDir: false,
    chunkSizeWarningLimit: 1000, // 1MB
    rollupOptions: {
      output: {
        manualChunks: (id) => {
          if (id.includes("node_modules")) {
            if (id.includes("svelte")) return "vendor-svelte";
            if (id.includes("lucide")) return "vendor-icons";
            if (id.includes("xterm")) return "vendor-xterm";
            if (id.includes("cytoscape")) return "vendor-cytoscape";
            if (id.includes("stripe")) return "vendor-stripe";
            if (id.includes("fluent-svelte")) return "vendor-ui";
            // return "vendor"; // removed to allow splitting other dependencies
          }
        },
      },
    },
  },
});
