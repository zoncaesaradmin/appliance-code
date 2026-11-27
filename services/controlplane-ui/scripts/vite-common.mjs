import react from "@vitejs/plugin-react";
import tailwindcss from "@tailwindcss/vite";
import { loadEnv } from "vite";

function json(res, status, body) {
  res.statusCode = status;
  res.setHeader("Content-Type", "application/json");
  res.end(JSON.stringify(body));
}

function mockApiPlugin(enabled) {
  return {
    name: "appliance-ui-mock-api",
    configureServer(server) {
      if (!enabled) {
        return;
      }
      server.middlewares.use((req, res, next) => {
        const path = (req.url || "").split("?")[0];
        if (req.method === "GET" && path === "/api/v1/setup/status") {
          return json(res, 200, { initialized: true });
        }
        if (req.method === "GET" && path === "/api/v1/capabilities") {
          return json(res, 200, { profile: "application-core", capabilities: [] });
        }
        if (req.method === "GET" && (path === "/version" || path === "/api/v1/version")) {
          return json(res, 200, {
            version: "0.1.0-dev",
            commit: "mock",
            buildTime: "2026-11-27T00:00:00Z",
            goVersion: "go1.25"
          });
        }
        if (req.method === "GET" && path === "/api/v1/health") {
          return json(res, 200, { status: "ok" });
        }
        if (req.method === "GET" && path === "/api/shared/v1/auth/session") {
          return json(res, 200, { authenticated: false });
        }
        if (req.method === "POST" && path === "/api/shared/v1/auth/otp/challenges") {
          return json(res, 200, { challenge_id: "mock-challenge" });
        }
        if (req.method === "POST" && path === "/api/shared/v1/auth/otp/verify") {
          return json(res, 200, {});
        }
        return next();
      });
    }
  };
}

export function createViteConfig(mode) {
  const env = loadEnv(mode, process.cwd(), "");
  const proxyTarget = env.VITE_CONTROL_PLANE_PROXY_TARGET?.trim();
  const outDir = process.env.APPLIANCE_UI_OUT_DIR?.trim() || "bin/dist";
  const host = process.env.VITE_HOST || "127.0.0.1";
  const devPort = Number(process.env.VITE_PORT || "5173");
  const previewPort = Number(process.env.VITE_PREVIEW_PORT || process.env.VITE_PORT || "4173");
  const mockEnabled = mode === "mock" || env.VITE_CONTROL_PLANE_MODE === "mock";

  return {
    plugins: [tailwindcss(), react(), mockApiPlugin(mockEnabled)],
    build: {
      outDir,
      sourcemap: true,
      emptyOutDir: false
    },
    server: {
      host,
      port: devPort,
      proxy: !mockEnabled && proxyTarget
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
