import { defineConfig } from "vitest/config";
import react from "@vitejs/plugin-react";

const api = process.env.API_PROXY_TARGET ?? "http://localhost:8080";

// Tailwind's native engine needs Node 20+; tests don't need CSS, so they skip it.
export default defineConfig(async () => ({
  plugins: [react(), ...(process.env.VITEST ? [] : [(await import("@tailwindcss/vite")).default()])],
  server: {
    port: 5173,
    watch: { usePolling: process.env.VITE_POLLING === "true" },
    proxy: { "/v1": api, "/readyz": api, "/openapi.yaml": api },
  },
  test: { environment: "jsdom", globals: true },
}));
