import { defineConfig } from "vite";
import { tanstackStart } from "@tanstack/react-start/plugin/vite";
import react from "@vitejs/plugin-react";
import tailwindcss from "@tailwindcss/vite";
import { fileURLToPath } from "node:url";
export default defineConfig({
  resolve: { alias: { "@": fileURLToPath(new URL("./src", import.meta.url)) } },
  plugins: [
    tailwindcss(),
    tanstackStart({
      spa: { enabled: true, prerender: { outputPath: "/index.html" } },
    }),
    react(),
  ],
  // Prerender uses Vite preview; bind IPv4 explicitly so Linux localhost
  // DNS ordering cannot make its fetch target a different loopback address.
  preview: { host: "127.0.0.1" },
  server: { proxy: { "/api": "http://127.0.0.1:8080" } },
});
