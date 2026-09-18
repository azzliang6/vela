import { defineConfig } from "vite";
import react from "@vitejs/plugin-react";

// Tauri 固定端口约定，避免桌面壳连不上开发服务器
export default defineConfig({
  plugins: [react()],
  clearScreen: false,
  server: {
    port: 1420,
    strictPort: true,
    watch: { ignored: ["**/src-tauri/**"] },
  },
  build: {
    target: "chrome105",
    sourcemap: false,
  },
});
