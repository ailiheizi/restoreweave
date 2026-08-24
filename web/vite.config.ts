import { defineConfig } from "vite";
import react from "@vitejs/plugin-react";

export default defineConfig({
  plugins: [react()],
  server: {
    port: 5173,
    // The browser client speaks only the daemon's typed loopback API. Keep
    // the development server convenient without adding a second backend.
    proxy: {
      "/api": {
        target: process.env.RESTOREWEAVE_API_TARGET ?? "http://127.0.0.1:4534",
        changeOrigin: true,
      },
    },
  },
});
