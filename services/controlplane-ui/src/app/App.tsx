import React, { useEffect, useState } from "react";
import { clearAuth, loadAuth, saveAuth, setAuthPersist } from "../auth";
import { AuthLayout } from "./AuthLayout";
import { ApplicationLoginPage } from "./ApplicationLoginPage";
import { ApplicationShell } from "./ApplicationShell";
import { BootScreen } from "./BootScreen";
import { client } from "../lib/api";
import { navigate } from "../lib/navigate";
import { consolePath, isApplicationPath, loginDestination, workspaceForPath } from "../lib/workspace";
import { useViewSyncGeneration } from "../lib/viewSyncHooks";
import { applicationAuth } from "../lib/applicationAuth";
import { Shell } from "../shell/Shell";
import type { CapabilitiesResponse, Session } from "../types";
import type { AppShellState } from "./AppShellState";

export function App(): React.JSX.Element {
  const [location, setLocation] = useState({
    pathname: window.location.pathname || "/",
    search: window.location.search || ""
  });
  const [shellState, setShellState] = useState<AppShellState>({
    booting: true,
    initialized: true,
    capabilities: [],
    session: null
  });
  const [bootError, setBootError] = useState("");
  const [applicationAuthenticated, setApplicationAuthenticated] = useState(false);
  const [refreshKey, setRefreshKey] = useState(0);
  const bootstrapSync = useViewSyncGeneration("shell.bootstrap");
  const appSync = useViewSyncGeneration("app");

  useEffect(() => {
    const onPopState = () =>
      setLocation({ pathname: window.location.pathname || "/", search: window.location.search || "" });
    window.addEventListener("popstate", onPopState);
    return () => window.removeEventListener("popstate", onPopState);
  }, []);

  useEffect(() => {
    document.title = location.pathname === "/" || isApplicationPath(location.pathname)
      ? "Zonited App"
      : "Zon Appliance";
  }, [location.pathname]);

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
        const applicationRoute = isApplicationPath(window.location.pathname) || window.location.pathname === "/";
        if (applicationRoute) {
          try {
            const status = await applicationAuth.session();
            setApplicationAuthenticated(status.authenticated);
          } catch {
            setApplicationAuthenticated(false);
          }
        } else if (loadAuth()) {
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
    const pathname = location.pathname;
    const applicationPath = pathname === "/" || isApplicationPath(pathname);
    if (shellState.booting) {
      return;
    }
    if (!applicationPath && applicationAuthenticated) {
      // Keep the operator and application identities separate when a user
      // moves between the two URL spaces in the same browser session.
      setApplicationAuthenticated(false);
      return;
    }
    if (!shellState.initialized && pathname !== "/setup") {
      navigate("/setup", true);
      return;
    }
    if (shellState.initialized && !shellState.session && !applicationAuthenticated && pathname === "/") {
      // The public origin is the Zonited application entry. Keep the operator
      // surface explicit at /login or /console instead of making the default
      // landing page choose between the two identities.
      navigate("/app/login", true);
      return;
    }
    if (shellState.initialized && !shellState.session && !applicationAuthenticated && pathname !== "/login" && pathname !== "/app/login") {
      const next = loginDestination(pathname, location.search);
      if (isApplicationPath(pathname)) {
        navigate(`/app/login?next=${encodeURIComponent(next)}`, true);
        return;
      }
      navigate(next === "/home" ? "/login" : `/login?next=${encodeURIComponent(pathname)}`, true);
      return;
    }
    if (
      shellState.session &&
      (pathname === "/" || pathname === "/login" || pathname === "/app/login" || pathname === "/setup")
    ) {
      navigate(loginDestination(pathname, location.search), true);
      return;
    }
    if (shellState.session && (pathname === "/console" || pathname.startsWith("/console/"))) {
      const legacyPath = consolePath(pathname);
      if (legacyPath !== pathname) {
        navigate(legacyPath, true);
      }
    }
    if (applicationAuthenticated && pathname === "/") {
      navigate("/app", true);
    }
  }, [location, shellState.booting, shellState.initialized, shellState.session, applicationAuthenticated]);

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

  async function handleGuest(name: string, persist: boolean): Promise<void> {
    const tokens = await client.loginAsGuest(name);
    setAuthPersist(persist);
    saveAuth(tokens);
    setRefreshKey((value) => value + 1);
  }

  async function handleLogout(): Promise<void> {
    await client.logout();
    setShellState((current) => ({ ...current, session: null }));
    navigate("/login", true);
  }

  async function handleApplicationLogout(): Promise<void> {
    await applicationAuth.logout().catch(() => undefined);
    setApplicationAuthenticated(false);
    navigate("/app/login", true);
  }

  if (shellState.booting) {
    return <BootScreen message="Loading appliance control plane UI..." />;
  }

  if (bootError) {
    return <BootScreen message={bootError} error />;
  }

  const applicationPath = location.pathname === "/" || isApplicationPath(location.pathname);

  if (shellState.initialized && applicationPath && !applicationAuthenticated) {
    return <ApplicationLoginPage onAuthenticated={() => { setApplicationAuthenticated(true); navigate("/app", true); }} />;
  }

  if (!shellState.initialized || (!shellState.session && !applicationAuthenticated)) {
    const isSetup = !shellState.initialized;
    return (
      <AuthLayout
        mode={isSetup ? "setup" : "login"}
        onLogin={handleLogin}
        onSetup={handleSetup}
        guestAccess={shellState.capabilities.includes("guest-access")}
        onGuest={handleGuest}
        workspace={workspaceForPath(loginDestination(location.pathname, location.search))}
      />
    );
  }

  if (applicationPath && applicationAuthenticated) {
    const applicationSession: Session = { userId: "", username: "Member", displayName: "Member", domain: "application", authMethod: "mobile_otp", permissions: [] };
    return (
      <ApplicationShell
        pathname={location.pathname}
        session={applicationSession}
        onLogout={handleApplicationLogout}
      />
    );
  }

  return (
    <Shell
      pathname={location.pathname}
      session={shellState.session!}
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
