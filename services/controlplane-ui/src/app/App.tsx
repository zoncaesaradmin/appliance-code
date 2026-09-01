import React, { useEffect, useState } from "react";
import { clearAuth, loadAuth, saveAuth, setAuthPersist } from "../auth";
import { AuthLayout } from "./AuthLayout";
import { BootScreen } from "./BootScreen";
import { client } from "../lib/api";
import { navigate } from "../lib/navigate";
import { useViewSyncGeneration } from "../lib/viewSyncHooks";
import { Shell } from "../shell/Shell";
import type { CapabilitiesResponse, Session } from "../types";
import type { AppShellState } from "./AppShellState";

export function App(): React.JSX.Element {
  const [pathname, setPathname] = useState(window.location.pathname || "/");
  const [shellState, setShellState] = useState<AppShellState>({
    booting: true,
    initialized: true,
    capabilities: [],
    session: null
  });
  const [bootError, setBootError] = useState("");
  const [refreshKey, setRefreshKey] = useState(0);
  const bootstrapSync = useViewSyncGeneration("shell.bootstrap");
  const appSync = useViewSyncGeneration("app");

  useEffect(() => {
    const onPopState = () => setPathname(window.location.pathname || "/");
    window.addEventListener("popstate", onPopState);
    return () => window.removeEventListener("popstate", onPopState);
  }, []);

  useEffect(() => {
    let ignore = false;
    async function boot() {
      setBootError("");
      try {
        const [setup, capabilities] = await Promise.all([
          client.getSetupStatus(),
          client.getCapabilities().catch(() => ({ capabilities: [] } as CapabilitiesResponse))
        ]);
        let session: Session | null = null;
        if (loadAuth()) {
          try {
            session = await client.getSession();
          } catch {
            clearAuth();
          }
        }
        if (!ignore) {
          setShellState({
            booting: false,
            initialized: setup.initialized,
            capabilities: capabilities.capabilities,
            session
          });
        }
      } catch (error) {
        if (!ignore) {
          setShellState((current) => ({ ...current, booting: false }));
          setBootError(error instanceof Error ? error.message : "Could not load appliance state.");
        }
      }
    }
    void boot();
    return () => {
      ignore = true;
    };
  }, [refreshKey]);

  // After mutations that declare shell.bootstrap / app, refresh capabilities
  // and session without returning to the boot screen.
  useEffect(() => {
    if (bootstrapSync === 0 && appSync === 0) {
      return;
    }
    if (!shellState.session && !loadAuth()) {
      return;
    }
    let ignore = false;
    void (async () => {
      try {
        const [capabilities, session] = await Promise.all([
          client.getCapabilities().catch(() => ({ capabilities: [] } as CapabilitiesResponse)),
          client.getSession().catch(() => null)
        ]);
        if (ignore) {
          return;
        }
        if (!session) {
          clearAuth();
          setShellState((current) => ({ ...current, session: null, capabilities: [] }));
          return;
        }
        setShellState((current) => ({
          ...current,
          capabilities: capabilities.capabilities,
          session
        }));
      } catch {
        // Keep existing shell state; surface errors on the next full boot.
      }
    })();
    return () => {
      ignore = true;
    };
  }, [bootstrapSync, appSync, shellState.session]);

  useEffect(() => {
    if (shellState.booting) {
      return;
    }
    if (!shellState.initialized && pathname !== "/setup") {
      navigate("/setup", true);
      return;
    }
    if (shellState.initialized && !shellState.session && pathname !== "/login") {
      navigate("/login", true);
      return;
    }
    if (shellState.session && (pathname === "/" || pathname === "/login" || pathname === "/setup")) {
      navigate("/home", true);
    }
  }, [pathname, shellState.booting, shellState.initialized, shellState.session]);

  async function handleLogin(username: string, password: string, persist: boolean): Promise<void> {
    // Login omits domain in the UI; client/backend default to local.
    const tokens = await client.login(username, password);
    setAuthPersist(persist);
    saveAuth(tokens);
    setRefreshKey((value) => value + 1);
  }

  async function handleSetup(username: string, password: string, persist: boolean): Promise<void> {
    await client.createFirstAdmin(username, password, "");
    const tokens = await client.login(username, password);
    setAuthPersist(persist);
    saveAuth(tokens);
    setRefreshKey((value) => value + 1);
  }

  async function handleGuest(persist: boolean): Promise<void> {
    const tokens = await client.loginAsGuest();
    setAuthPersist(persist);
    saveAuth(tokens);
    setRefreshKey((value) => value + 1);
  }

  async function handleLogout(): Promise<void> {
    await client.logout();
    setShellState((current) => ({ ...current, session: null }));
    navigate("/login", true);
  }

  if (shellState.booting) {
    return <BootScreen message="Loading appliance control plane UI..." />;
  }

  if (bootError) {
    return <BootScreen message={bootError} error />;
  }

  if (!shellState.initialized || !shellState.session) {
    const isSetup = !shellState.initialized;
    return (
      <AuthLayout
        mode={isSetup ? "setup" : "login"}
        onLogin={handleLogin}
        onSetup={handleSetup}
        guestAccess={shellState.capabilities.includes("guest-access")}
        onGuest={handleGuest}
      />
    );
  }

  return (
    <Shell
      pathname={pathname}
      session={shellState.session}
      capabilities={shellState.capabilities}
      onLogout={handleLogout}
      onSignedOut={() => {
        clearAuth();
        setShellState((current) => ({ ...current, session: null }));
        navigate("/login", true);
      }}
    />
  );
}
