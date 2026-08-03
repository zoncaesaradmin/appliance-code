import react from "@vitejs/plugin-react";
import tailwindcss from "@tailwindcss/vite";
import { loadEnv } from "vite";

export function createViteConfig(mode) {
  const env = loadEnv(mode, process.cwd(), "");
  const proxyTarget = env.VITE_CONTROL_PLANE_PROXY_TARGET?.trim();
  const outDir = process.env.APPLIANCE_UI_OUT_DIR?.trim() || "bin/dist";
  const host = process.env.VITE_HOST || "127.0.0.1";
  const devPort = Number(process.env.VITE_PORT || "5173");
  const previewPort = Number(process.env.VITE_PREVIEW_PORT || process.env.VITE_PORT || "4173");

  return {
    plugins: [tailwindcss(), react()],
    build: {
      outDir,
      sourcemap: true,
      emptyOutDir: false
    },
    server: {
      host,
      port: devPort,
      proxy: proxyTarget
        ? {
            "/api": proxyTarget,
            "/health": proxyTarget,
            "/version": proxyTarget
          }
        : undefined
    },
    preview: {
      host,
      port: previewPort
    }
  };
}
