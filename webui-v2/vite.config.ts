import { defineConfig } from "vite";
import react from "@vitejs/plugin-react";
import path from "node:path";

// Isolated dev/preview ports so this never collides with the live daemon (:47274)
// or the legacy dashboard dev server.
export default defineConfig({
  plugins: [react()],
  resolve: {
    alias: {
      "@": path.resolve(__dirname, "src"),
    },
  },
  server: { port: 47280, strictPort: true },
  preview: { port: 47281, strictPort: true },
});
