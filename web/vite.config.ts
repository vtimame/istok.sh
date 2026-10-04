import { resolve } from "node:path"
import tailwindcss from "@tailwindcss/vite"
import react from "@vitejs/plugin-react"
import { defineConfig } from "vite"

// https://vite.dev/config/
export default defineConfig({
  plugins: [react(), tailwindcss()],
  resolve: {
    alias: {
      "@": resolve(import.meta.dirname, "./src"),
    },
  },
  // The Go binary embeds this directory; see internal/webui/assets.go.
  build: {
    outDir: resolve(import.meta.dirname, "../internal/webui/dist"),
    emptyOutDir: true,
  },
  // `pnpm dev` proxies the API to a running `istok ui --port 7700 --no-open`.
  server: {
    proxy: {
      "/api": { target: "http://127.0.0.1:7700", changeOrigin: true },
    },
  },
})
