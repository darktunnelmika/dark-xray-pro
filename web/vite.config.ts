import { defineConfig } from "vite";

export default defineConfig({
  server: {
    host: "127.0.0.1",
    proxy: { "/api": "http://127.0.0.1:2087" },
  },
  build: {
    target: "es2022",
    outDir: "dist",
  },
});
