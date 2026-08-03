import React, { FormEvent, useEffect, useState } from "react";
import ReactDOM from "react-dom/client";
import { clearAuth, loadAuth, saveAuth } from "./auth";
import { ApiError, createControlPlaneClient, type ControlPlaneClient } from "./client";
import {
  currentMode,
  isSystemAdministrator,
  modeUsesFeatureSelector,
  visibleModes,
  type Mode
} from "./navigation";
import "./styles.css";
import {
  BrandMark,
  Card,
  EmptyState,
  Icon,
  IconButton,
  PageFrame,
  StatCard,
  cn
} from "./ui";
import type {
  APIToken,
  ApplianceIdentity,
  BuilderGitAccessStatus,
  BuildTarget,
  CapabilitiesResponse,
  CreateTokenResponse,
  DNSRecord,
  Job,
  RegistryDescriptor,
  RegistryGrant,
  Session,
  Version,
  WorkProfile,
  Workspace
} from "./types";

const client = createControlPlaneClient();

type AppShellState = {
  booting: boolean;
  initialized: boolean;
  capabilities: string[];
  session: Session | null;
};

function navigate(path: string, replace = false): void {
  if (replace) {
    window.history.replaceState({}, "", path);
  } else {
    window.history.pushState({}, "", path);
  }
  window.dispatchEvent(new PopStateEvent("popstate"));
}

function formatTimestamp(value?: string): string {
  if (!value) {
    return "Not available";
  }
  return new Date(value).toLocaleString();
}

function capabilityBadge(capability: string): string {
  return capability.toUpperCase();
}

function App(): React.JSX.Element {
  const [pathname, setPathname] = useState(window.location.pathname || "/");
  const [shellState, setShellState] = useState<AppShellState>({
    booting: true,
    initialized: true,
    capabilities: [],
    session: null
  });
  const [bootError, setBootError] = useState("");
  const [refreshKey, setRefreshKey] = useState(0);

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

  async function handleLogin(username: string, password: string): Promise<void> {
    const tokens = await client.login(username, password);
    saveAuth(tokens);
    setRefreshKey((value) => value + 1);
  }

  async function handleSetup(username: string, password: string): Promise<void> {
    await client.createFirstAdmin(username, password, "");
    const tokens = await client.login(username, password);
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
      />
    );
  }

  return (
    <Shell
      pathname={pathname}
      session={shellState.session}
      capabilities={shellState.capabilities}
      onLogout={handleLogout}
    />
  );
}

function BootScreen(props: { message: string; error?: boolean }): React.JSX.Element {
  return (
    <div className="boot-screen">
      <div className="boot-card">
        <BrandMark />
        <h1>{props.error ? "UI unavailable" : "Preparing appliance UI"}</h1>
        <p>{props.message}</p>
      </div>
    </div>
  );
}

function AuthLayout(props: {
  mode: "login" | "setup";
  onLogin: (username: string, password: string) => Promise<void>;
  onSetup: (username: string, password: string) => Promise<void>;
}): React.JSX.Element {
  const [username, setUsername] = useState("admin");
  const [password, setPassword] = useState("");
  const [confirmPassword, setConfirmPassword] = useState("");
  const [submitting, setSubmitting] = useState(false);
  const [error, setError] = useState("");

  async function handleSubmit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    setError("");
    setSubmitting(true);
    try {
      if (props.mode === "setup") {
        if (password !== confirmPassword) {
          throw new Error("Passwords do not match.");
        }
        await props.onSetup(username, password);
      } else {
        await props.onLogin(username, password);
      }
    } catch (submitError) {
      setError(submitError instanceof Error ? submitError.message : "Authentication failed.");
    } finally {
      setSubmitting(false);
    }
  }

  return (
    <div className="auth-layout">
      <section className="auth-visual">
        <div className="auth-visual__panel">
          <span className="eyebrow">Zon Appliance</span>
          <h1>Sleek infrastructure operations, built for an appliance surface.</h1>
          <p>
            A focused control-plane workspace for management, analysis, and administration without
            losing the appliance-grade clarity operators expect.
          </p>
          <div className="hero-diagram" aria-hidden="true">
            <div className="hero-diagram__frame">
              <div className="hero-diagram__sidebar" />
              <div className="hero-diagram__content">
                <div className="hero-diagram__header" />
                <div className="hero-diagram__cards">
                  <span />
                  <span />
                  <span />
                </div>
              </div>
            </div>
          </div>
        </div>
      </section>
      <section className="auth-form">
        <div className="auth-form__card">
          <span className="eyebrow">{props.mode === "setup" ? "First-time setup" : "Secure sign-in"}</span>
          <h2>{props.mode === "setup" ? "Create the first administrator" : "Sign in to the appliance"}</h2>
          <p>
            {props.mode === "setup"
              ? "Initialize the control plane with the first admin account."
              : "Use your control-plane credentials to continue."}
          </p>
          <form className="stack-form" onSubmit={handleSubmit}>
            <label className="field">
              <span>Username</span>
              <input value={username} onChange={(event) => setUsername(event.target.value)} />
            </label>
            <label className="field">
              <span>Password</span>
              <input
                type="password"
                value={password}
                onChange={(event) => setPassword(event.target.value)}
              />
            </label>
            {props.mode === "setup" ? (
              <label className="field">
                <span>Confirm password</span>
                <input
                  type="password"
                  value={confirmPassword}
                  onChange={(event) => setConfirmPassword(event.target.value)}
                />
              </label>
            ) : null}
            {error ? <div className="message message--error">{error}</div> : null}
            <button className="button button--primary" disabled={submitting} type="submit">
              {submitting
                ? "Working..."
                : props.mode === "setup"
                  ? "Create administrator"
                  : "Sign in"}
            </button>
          </form>
        </div>
      </section>
    </div>
  );
}

function Shell(props: {
  pathname: string;
  session: Session;
  capabilities: string[];
  onLogout: () => Promise<void>;
}): React.JSX.Element {
  const navigationModes = visibleModes({ session: props.session });
  const mode = currentMode(props.pathname, navigationModes);
  const [userMenuOpen, setUserMenuOpen] = useState(false);
  const [helpMenuOpen, setHelpMenuOpen] = useState(false);
  const [featureMenuMode, setFeatureMenuMode] = useState<Mode | null>(null);

  function openMode(nextMode: Mode) {
    setUserMenuOpen(false);
    setHelpMenuOpen(false);
    if (modeUsesFeatureSelector(nextMode)) {
      setFeatureMenuMode((openMode) => (openMode?.id === nextMode.id ? null : nextMode));
      return;
    }
    setFeatureMenuMode(null);
    navigate(nextMode.defaultPath);
  }

  function openFeature(path: string) {
    setFeatureMenuMode(null);
    navigate(path);
  }

  function closeTransientMenus() {
    setUserMenuOpen(false);
    setHelpMenuOpen(false);
    setFeatureMenuMode(null);
  }

  function onUserAction(path: string) {
    closeTransientMenus();
    navigate(path);
  }

  useEffect(() => {
    if (!featureMenuMode) {
      return;
    }
    const closeOnPointer = (event: PointerEvent) => {
      const target = event.target;
      if (target instanceof Element && target.closest(".mode-navigation")) {
        return;
      }
      setFeatureMenuMode(null);
    };
    const closeOnEscape = (event: KeyboardEvent) => {
      if (event.key === "Escape") {
        setFeatureMenuMode(null);
      }
    };
    window.addEventListener("pointerdown", closeOnPointer);
    window.addEventListener("keydown", closeOnEscape);
    return () => {
      window.removeEventListener("pointerdown", closeOnPointer);
      window.removeEventListener("keydown", closeOnEscape);
    };
  }, [featureMenuMode]);

  useEffect(() => {
    if (!userMenuOpen && !helpMenuOpen) {
      return;
    }
    const closeOnPointer = (event: PointerEvent) => {
      const target = event.target;
      if (target instanceof Element && target.closest(".top-navigation")) {
        return;
      }
      setUserMenuOpen(false);
      setHelpMenuOpen(false);
    };
    const closeOnEscape = (event: KeyboardEvent) => {
      if (event.key === "Escape") {
        setUserMenuOpen(false);
        setHelpMenuOpen(false);
      }
    };
    window.addEventListener("pointerdown", closeOnPointer);
    window.addEventListener("keydown", closeOnEscape);
    return () => {
      window.removeEventListener("pointerdown", closeOnPointer);
      window.removeEventListener("keydown", closeOnEscape);
    };
  }, [helpMenuOpen, userMenuOpen]);

  useEffect(() => {
    if (props.pathname.startsWith("/admin") && !isSystemAdministrator(props.session)) {
      closeTransientMenus();
      navigate("/home", true);
    }
  }, [props.pathname, props.session]);

  useEffect(() => {
    if (props.pathname === "/home/session") {
      navigate("/account/session", true);
    }
  }, [props.pathname]);

  return (
    <div className="grid min-h-screen grid-rows-[auto_1fr]">
      <header className="top-navigation relative z-[70] flex items-center justify-between gap-6 overflow-visible border-b border-slate-200/80 bg-white px-6 py-4 shadow-sm shadow-slate-900/5 max-[680px]:flex-col max-[680px]:items-start">
        <div className="flex items-center gap-3">
          <BrandMark />
          <div>
            <strong className="block text-sm font-bold text-slate-950">Zon Appliance</strong>
            <span className="text-sm text-slate-500">Control Plane UI</span>
          </div>
        </div>
        <div className="flex items-center gap-3 max-[680px]:w-full max-[680px]:flex-wrap max-[680px]:justify-start">
          <IconButton
            icon="search"
            label="Search"
            muted
            onClick={closeTransientMenus}
            title="Search will be added later"
          />
          <IconButton
            icon="alerts"
            label="Alerts"
            badge={0}
            muted
            onClick={closeTransientMenus}
            title="Notifications API planned"
          />
          <div className="relative">
            <IconButton
              icon="help"
              label="Help"
              onClick={() => {
                setHelpMenuOpen((open) => !open);
                setUserMenuOpen(false);
                setFeatureMenuMode(null);
              }}
            />
            {helpMenuOpen ? (
              <div className="shell-menu absolute right-0 top-[calc(100%+0.5rem)] z-[80] grid min-w-56 gap-1 rounded-2xl border border-slate-200 bg-white p-2 shadow-2xl shadow-slate-900/20 max-[680px]:left-0 max-[680px]:right-auto">
                <button onClick={() => onUserAction("/admin/system-status")}>About appliance</button>
                <button onClick={closeTransientMenus}>What&apos;s new</button>
                <button onClick={closeTransientMenus}>Help center</button>
                <button onClick={closeTransientMenus}>Ask for support</button>
              </div>
            ) : null}
          </div>
          <div className="relative">
            <button
              className="flex h-12 items-center gap-3 rounded-[1.15rem] bg-white px-2 text-left transition hover:bg-slate-100"
              onClick={() => {
                setUserMenuOpen((open) => !open);
                setHelpMenuOpen(false);
                setFeatureMenuMode(null);
              }}
            >
              <span className="grid h-12 w-12 place-items-center rounded-[1.15rem] bg-blue-100 text-blue-950">
                <Icon name="user" className="h-6 w-6" />
              </span>
              <span>
                <strong className="block text-sm font-bold text-slate-950">{props.session.username}</strong>
                <span className="text-sm text-slate-500">{props.session.authMethod}</span>
              </span>
            </button>
            {userMenuOpen ? (
              <div className="shell-menu absolute right-0 top-[calc(100%+0.5rem)] z-[80] grid min-w-56 gap-1 rounded-2xl border border-slate-200 bg-white p-2 shadow-2xl shadow-slate-900/20 max-[680px]:left-0 max-[680px]:right-auto">
                <button onClick={closeTransientMenus}>Preferences</button>
                <button onClick={closeTransientMenus}>Change password</button>
                <button onClick={() => onUserAction("/home/access")}>Manage API keys</button>
                <button onClick={() => onUserAction("/account/session")}>Session info</button>
                <button onClick={closeTransientMenus}>Logo options</button>
                <button
                  onClick={() => {
                    void props.onLogout();
                    closeTransientMenus();
                  }}
                >
                  Sign out
                </button>
              </div>
            ) : null}
          </div>
        </div>
      </header>
      <div className="grid grid-cols-[110px_minmax(0,1fr)] items-start gap-5 p-5 max-[1180px]:grid-cols-[90px_minmax(0,1fr)] max-[940px]:grid-cols-1">
        <div className="mode-navigation relative min-w-0">
          <aside className="grid content-start gap-2 rounded-3xl border border-slate-200/90 bg-white/75 p-3 max-[940px]:grid-flow-col max-[940px]:auto-cols-fr max-[940px]:overflow-x-auto">
            {navigationModes.map((entry) => {
              const active = entry.id === mode.id;
              return (
                <button
                  key={entry.id}
                  className={cn(
                    "grid justify-items-center gap-2 rounded-2xl px-2 py-3 text-slate-500 transition hover:bg-slate-100 hover:text-slate-950",
                    active && "bg-blue-950/10 text-blue-950"
                  )}
                  aria-expanded={featureMenuMode?.id === entry.id ? "true" : undefined}
                  onClick={() => openMode(entry)}
                >
                  <span className="grid h-10 w-10 place-items-center rounded-2xl bg-white shadow-sm shadow-slate-900/5">
                    <Icon name={entry.icon} />
                  </span>
                  <span className="text-xs font-bold">{entry.label}</span>
                </button>
              );
            })}
          </aside>
          {featureMenuMode ? (
            <div
              className="absolute left-[calc(100%+0.875rem)] top-0 z-40 w-64 rounded-3xl border border-slate-200 bg-white p-3 shadow-2xl shadow-slate-900/15 max-[940px]:static max-[940px]:mt-3 max-[940px]:w-full"
              role="menu"
              aria-label={`${featureMenuMode.label} features`}
            >
              <div className="px-2 pb-2">
                <span className="mb-1 inline-block text-xs font-bold uppercase tracking-[0.14em] text-blue-950">
                  {featureMenuMode.label}
                </span>
                <strong className="block text-sm text-slate-950">Select feature</strong>
              </div>
              <nav className="grid gap-1">
                {featureMenuMode.features.map((feature) => (
                  <button
                    key={feature.path}
                    className={cn(
                      "flex min-h-11 items-center gap-3 rounded-2xl px-3 text-left text-sm font-semibold text-slate-700 transition hover:bg-slate-100 hover:text-slate-950",
                      props.pathname.startsWith(feature.path) && "bg-blue-100 text-blue-950"
                    )}
                    onClick={() => openFeature(feature.path)}
                    role="menuitem"
                  >
                    <Icon name={feature.icon} className="h-4 w-4" />
                    {feature.label}
                  </button>
                ))}
              </nav>
            </div>
          ) : null}
        </div>
        <main className="min-w-0 w-full">
          <RouteView pathname={props.pathname} session={props.session} capabilities={props.capabilities} />
        </main>
      </div>
    </div>
  );
}

function RouteView(props: {
  pathname: string;
  session: Session;
  capabilities: string[];
}): React.JSX.Element {
  if (props.pathname.startsWith("/account/session")) {
    return <SessionPage session={props.session} />;
  }
  if (props.pathname.startsWith("/manage/builder")) {
    return <BuilderPage pathname={props.pathname} />;
  }
  if (props.pathname.startsWith("/manage/dns")) {
    return <DNSPage />;
  }
  if (props.pathname.startsWith("/manage/artifacts")) {
    return <ArtifactsPage pathname={props.pathname} />;
  }
  if (props.pathname.startsWith("/analyze/workflows")) {
    return <AnalyzePage />;
  }
  if (props.pathname.startsWith("/admin")) {
    return <AdminPage pathname={props.pathname} capabilities={props.capabilities} />;
  }
  return <HomePage pathname={props.pathname} capabilities={props.capabilities} />;
}

function SessionPage(props: { session: Session }): React.JSX.Element {
  return (
    <PageFrame
      title="Session info"
      description="Details for the currently authenticated control-plane session."
      pathname="/account/session"
      onNavigate={navigate}
      tabs={[]}
    >
      <div className="grid-two">
        <Card title="User details" subtitle="Current authenticated session">
          <div className="detail-list">
            <div>
              <span>Username</span>
              <strong>{props.session.username}</strong>
            </div>
            <div>
              <span>User ID</span>
              <strong>{props.session.userId}</strong>
            </div>
            <div>
              <span>Auth method</span>
              <strong>{props.session.authMethod}</strong>
            </div>
          </div>
        </Card>
        <Card title="Permissions" subtitle="Resolved control-plane permissions">
          <div className="badge-row">
            {props.session.permissions.map((permission) => (
              <span className="pill" key={permission}>
                {permission}
              </span>
            ))}
          </div>
        </Card>
      </div>
    </PageFrame>
  );
}

function HomePage(props: {
  pathname: string;
  capabilities: string[];
}): React.JSX.Element {
  const [version, setVersion] = useState<Version | null>(null);
  const [health, setHealth] = useState("unknown");
  const [identity, setIdentity] = useState<ApplianceIdentity | null>(null);
  const [tokens, setTokens] = useState<APIToken[]>([]);
  const [tokenName, setTokenName] = useState("");
  const [createdToken, setCreatedToken] = useState<CreateTokenResponse | null>(null);
  const [message, setMessage] = useState("");

  useEffect(() => {
    void (async () => {
      const [nextVersion, nextHealth, nextIdentity] = await Promise.all([
        client.getVersion().catch(() => null),
        client.getReady().then((value) => value.status).catch(() => "degraded"),
        client.getIdentity().catch(() => null)
      ]);
      setVersion(nextVersion);
      setHealth(nextHealth);
      setIdentity(nextIdentity);
    })();
  }, []);

  useEffect(() => {
    if (props.pathname !== "/home/access") {
      return;
    }
    void client.listTokens().then(setTokens).catch(() => setTokens([]));
  }, [props.pathname]);

  async function createToken(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    setMessage("");
    try {
      const response = await client.createToken({
        name: tokenName,
        lifetimeSeconds: 90 * 24 * 60 * 60,
        scopes: ["artifacts.read", "artifacts.write"]
      });
      setCreatedToken(response);
      setTokenName("");
      setTokens(await client.listTokens());
      setMessage("API token created. Copy the secret now; it is only shown once.");
    } catch (error) {
      setMessage(error instanceof Error ? error.message : "Could not create the API token.");
    }
  }

  async function revokeToken(id: string) {
    await client.deleteToken(id);
    setTokens(await client.listTokens());
  }

  return (
    <PageFrame
      title="Home"
      description="Default landing page for operators, with dashboard status and user-facing access tools."
      pathname={props.pathname}
      onNavigate={navigate}
      tabs={[
        { label: "Overview", path: "/home", icon: "home" },
        { label: "API Keys", path: "/home/access", icon: "key" }
      ]}
    >
      {props.pathname === "/home/access" ? (
        <div className="grid-two">
          <Card title="Create API token" subtitle="User-scoped token creation">
            <form className="stack-form" onSubmit={createToken}>
              <label className="field">
                <span>Token name</span>
                <input value={tokenName} onChange={(event) => setTokenName(event.target.value)} />
              </label>
              <button className="button button--primary" type="submit">
                Create token
              </button>
            </form>
            {message ? <div className="message">{message}</div> : null}
            {createdToken ? (
              <div className="secret-panel">
                <strong>New secret</strong>
                <code>{createdToken.token}</code>
              </div>
            ) : null}
          </Card>
          <Card title="Current API tokens" subtitle="Existing token metadata">
            <div className="table-list">
              {tokens.map((token) => (
                <div className="table-list__row" key={token.id}>
                  <div>
                    <strong>{token.name}</strong>
                    <span>Expires {formatTimestamp(token.expiresAt)}</span>
                  </div>
                  <button className="button button--ghost" onClick={() => void revokeToken(token.id)}>
                    Revoke
                  </button>
                </div>
              ))}
            </div>
          </Card>
        </div>
      ) : (
        <div className="stack">
          <div className="stats-grid">
            <StatCard label="Readiness" value={health} tone={health === "ready" ? "success" : "neutral"} />
            <StatCard label="Capabilities" value={String(props.capabilities.length)} />
            <StatCard label="Appliance" value={identity?.applianceName || "Unknown"} />
            <StatCard label="Primary DNS zone" value={identity?.dnsZone || "Unavailable"} />
          </div>
          <div className="grid-two">
            <Card title="System overview" subtitle="Live control-plane identity and build details">
              <div className="detail-list">
                <div>
                  <span>Canonical origin</span>
                  <strong>{identity?.canonicalOrigin || "Not available"}</strong>
                </div>
                <div>
                  <span>FQDN</span>
                  <strong>{identity?.fqdn || "Not available"}</strong>
                </div>
                <div>
                  <span>Version</span>
                  <strong>{version?.version || "Unknown"}</strong>
                </div>
                <div>
                  <span>Build commit</span>
                  <strong>{version?.commit || "Unknown"}</strong>
                </div>
              </div>
            </Card>
            <Card title="Enabled capabilities" subtitle="Resolved from the appliance profile">
              <div className="badge-row">
                {props.capabilities.map((capability) => (
                  <span className="pill pill--navy" key={capability}>
                    {capabilityBadge(capability)}
                  </span>
                ))}
              </div>
            </Card>
          </div>
        </div>
      )}
    </PageFrame>
  );
}

function BuilderPage(props: { pathname: string }): React.JSX.Element {
  const [profiles, setProfiles] = useState<WorkProfile[]>([]);
  const [workspaces, setWorkspaces] = useState<Workspace[]>([]);
  const [currentWorkspace, setCurrentWorkspace] = useState<Workspace | null>(null);
  const [gitAccess, setGitAccess] = useState<BuilderGitAccessStatus | null>(null);
  const [targets, setTargets] = useState<BuildTarget[]>([]);
  const [latestJob, setLatestJob] = useState<Job | null>(null);
  const [message, setMessage] = useState("");
  const [workspaceName, setWorkspaceName] = useState("");
  const [workspaceProfile, setWorkspaceProfile] = useState("");
  const [selectedWorkspaceId, setSelectedWorkspaceId] = useState("");
  const [gitHost, setGitHost] = useState("");
  const [gitUsername, setGitUsername] = useState("");
  const [gitToken, setGitToken] = useState("");
  const [buildTarget, setBuildTarget] = useState("");
  const [imageTag, setImageTag] = useState("");

  async function refreshBuilder() {
    const [nextProfiles, nextWorkspaces, nextCurrent, nextGitAccess, nextTargets, nextJob] =
      await Promise.all([
        client.listWorkProfiles(),
        client.listWorkspaces(),
        client.getCurrentWorkspace(),
        client.getBuilderGitAccess(),
        client.listBuildTargets().catch(() => []),
        client.getCurrentBuildStatus().catch(() => null)
      ]);
    setProfiles(nextProfiles);
    setWorkspaces(nextWorkspaces);
    setCurrentWorkspace(nextCurrent);
    setGitAccess(nextGitAccess);
    setTargets(nextTargets);
    setLatestJob(nextJob);
    setWorkspaceProfile(nextProfiles[0]?.name || "");
    setSelectedWorkspaceId(nextCurrent?.id || nextWorkspaces[0]?.id || "");
    setGitHost(nextGitAccess.host || "");
    setGitUsername(nextGitAccess.username || "");
  }

  useEffect(() => {
    void refreshBuilder();
  }, []);

  useEffect(() => {
    if (latestJob?.status !== "running") {
      return;
    }
    const timer = window.setTimeout(() => {
      void client.getCurrentBuildStatus().then(setLatestJob);
    }, 2500);
    return () => window.clearTimeout(timer);
  }, [latestJob]);

  async function createWorkspace(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    setMessage("");
    try {
      await client.createWorkspace({ name: workspaceName, workProfile: workspaceProfile });
      setWorkspaceName("");
      setMessage("Workspace created and selected.");
      await refreshBuilder();
    } catch (error) {
      setMessage(error instanceof Error ? error.message : "Could not create workspace.");
    }
  }

  async function selectWorkspace() {
    await client.setCurrentWorkspace(selectedWorkspaceId);
    setMessage("Current workspace updated.");
    await refreshBuilder();
  }

  async function deleteWorkspace() {
    if (!selectedWorkspaceId) {
      return;
    }
    await client.deleteWorkspace(selectedWorkspaceId);
    setMessage("Workspace deleted.");
    await refreshBuilder();
  }

  async function saveGitAccess(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    await client.updateBuilderGitAccess({
      host: gitHost,
      username: gitUsername,
      token: gitToken
    });
    setGitToken("");
    setMessage("Builder Git access updated.");
    await refreshBuilder();
  }

  async function submitBuild(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    const job = await client.submitBuild({ targetName: buildTarget, imageTag });
    setLatestJob(job);
    setMessage("Build submitted.");
  }

  return (
    <PageFrame
      title="Builder"
      description="Workspace creation, appliance-wide Git access, and build submission."
      pathname={props.pathname}
      onNavigate={navigate}
      tabs={[
        { label: "Workspaces", path: "/manage/builder", icon: "builder" },
        { label: "Git Access", path: "/manage/builder/git-access", icon: "key" },
        { label: "Builds", path: "/manage/builder/builds", icon: "artifacts" }
      ]}
    >
      {message ? <div className="message">{message}</div> : null}
      {props.pathname === "/manage/builder/git-access" ? (
        <Card title="Builder Git HTTPS access" subtitle="Shared appliance credential set">
          <form className="stack-form" onSubmit={saveGitAccess}>
            <div className="detail-list">
              <div>
                <span>Configured</span>
                <strong>{gitAccess?.configured ? "Yes" : "No"}</strong>
              </div>
              <div>
                <span>Required hosts</span>
                <strong>{gitAccess?.requiredHosts?.join(", ") || "None advertised"}</strong>
              </div>
            </div>
            <label className="field">
              <span>Git host</span>
              <input value={gitHost} onChange={(event) => setGitHost(event.target.value)} />
            </label>
            <label className="field">
              <span>Git username</span>
              <input value={gitUsername} onChange={(event) => setGitUsername(event.target.value)} />
            </label>
            <label className="field">
              <span>Git token</span>
              <input
                type="password"
                value={gitToken}
                onChange={(event) => setGitToken(event.target.value)}
              />
            </label>
            <button className="button button--primary" type="submit">
              Save Git access
            </button>
          </form>
        </Card>
      ) : props.pathname === "/manage/builder/builds" ? (
        <div className="grid-two">
          <Card title="Submit build" subtitle="Current workspace target selection">
            <form className="stack-form" onSubmit={submitBuild}>
              <label className="field">
                <span>Current workspace</span>
                <input value={currentWorkspace?.name || "No current workspace"} disabled />
              </label>
              <label className="field">
                <span>Target</span>
                <select value={buildTarget} onChange={(event) => setBuildTarget(event.target.value)}>
                  <option value="">Select a build target</option>
                  {targets.map((target) => (
                    <option key={target.name} value={target.name}>
                      {target.name}
                    </option>
                  ))}
                </select>
              </label>
              <label className="field">
                <span>Image tag</span>
                <input value={imageTag} onChange={(event) => setImageTag(event.target.value)} />
              </label>
              <button className="button button--primary" type="submit">
                Submit build
              </button>
            </form>
          </Card>
          <Card title="Latest build" subtitle="Most recent current-workspace build status">
            {latestJob ? (
              <div className="detail-list">
                <div>
                  <span>Status</span>
                  <strong>{latestJob.status}</strong>
                </div>
                <div>
                  <span>Target</span>
                  <strong>{latestJob.targetName || "Unknown"}</strong>
                </div>
                <div>
                  <span>Artifact</span>
                  <strong>{latestJob.artifactRef || "Not available"}</strong>
                </div>
                <div>
                  <span>Updated</span>
                  <strong>{formatTimestamp(latestJob.updatedAt)}</strong>
                </div>
              </div>
            ) : (
              <EmptyState message="No build has been submitted for the current workspace yet." />
            )}
          </Card>
        </div>
      ) : (
        <div className="grid-two">
          <Card title="Workspace selection" subtitle="Switch or delete an existing workspace">
            <label className="field">
              <span>Existing workspaces</span>
              <select
                value={selectedWorkspaceId}
                onChange={(event) => setSelectedWorkspaceId(event.target.value)}
              >
                <option value="">Select a workspace</option>
                {workspaces.map((workspace) => (
                  <option key={workspace.id} value={workspace.id}>
                    {workspace.name} ({workspace.workProfile})
                  </option>
                ))}
              </select>
            </label>
            <div className="button-row">
              <button className="button button--primary" onClick={() => void selectWorkspace()}>
                Set current
              </button>
              <button className="button button--ghost" onClick={() => void deleteWorkspace()}>
                Delete
              </button>
            </div>
            {currentWorkspace ? (
              <div className="status-box">
                <strong>Current workspace</strong>
                <span>
                  {currentWorkspace.name} · {currentWorkspace.status}
                </span>
              </div>
            ) : null}
          </Card>
          <Card title="Create workspace" subtitle="Provision a workspace from a selected profile">
            <form className="stack-form" onSubmit={createWorkspace}>
              <label className="field">
                <span>Workspace name</span>
                <input
                  value={workspaceName}
                  onChange={(event) => setWorkspaceName(event.target.value)}
                />
              </label>
              <label className="field">
                <span>Workspace profile</span>
                <select
                  value={workspaceProfile}
                  onChange={(event) => setWorkspaceProfile(event.target.value)}
                >
                  {profiles.map((profile) => (
                    <option key={profile.name} value={profile.name}>
                      {profile.name}
                    </option>
                  ))}
                </select>
              </label>
              <button className="button button--primary" type="submit">
                Create workspace
              </button>
            </form>
          </Card>
        </div>
      )}
    </PageFrame>
  );
}

function DNSPage(): React.JSX.Element {
  const [zone, setZone] = useState("appliance.internal");
  const [records, setRecords] = useState<DNSRecord[]>([]);
  const [name, setName] = useState("");
  const [ipv4, setIPv4] = useState("");
  const [ttl, setTTL] = useState("300");
  const [message, setMessage] = useState("");

  async function refreshRecords() {
    const response = await client.listDNSRecords();
    setZone(response.zone);
    setRecords(response.items);
  }

  useEffect(() => {
    void refreshRecords();
  }, []);

  async function submitRecord(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    await client.upsertDNSRecord(name, { ipv4, ttl: Number(ttl) });
    setName("");
    setIPv4("");
    setTTL("300");
    setMessage("DNS record saved.");
    await refreshRecords();
  }

  async function removeRecord(recordName: string) {
    await client.deleteDNSRecord(recordName);
    setMessage("DNS record deleted.");
    await refreshRecords();
  }

  return (
    <PageFrame
      title="DNS"
      description="Managed LAN DNS records for the appliance zone."
      pathname="/manage/dns"
      onNavigate={navigate}
      tabs={[{ label: "Records", path: "/manage/dns", icon: "dns" }]}
    >
      {message ? <div className="message">{message}</div> : null}
      <div className="grid-two">
        <Card title="DNS zone records" subtitle={`Zone: ${zone}`}>
          <div className="table-list">
            {records.map((record) => (
              <div className="table-list__row" key={record.fqdn}>
                <div>
                  <strong>{record.name}</strong>
                  <span>
                    {record.ipv4} · TTL {record.ttl}
                  </span>
                </div>
                <button className="button button--ghost" onClick={() => void removeRecord(record.name)}>
                  Delete
                </button>
              </div>
            ))}
          </div>
        </Card>
        <Card title="Add or update DNS record" subtitle="Single-record management flow">
          <form className="stack-form" onSubmit={submitRecord}>
            <label className="field">
              <span>Hostname</span>
              <input value={name} onChange={(event) => setName(event.target.value)} />
            </label>
            <label className="field">
              <span>IPv4 address</span>
              <input value={ipv4} onChange={(event) => setIPv4(event.target.value)} />
            </label>
            <label className="field">
              <span>TTL (seconds)</span>
              <input value={ttl} onChange={(event) => setTTL(event.target.value)} />
            </label>
            <button className="button button--primary" type="submit">
              Save record
            </button>
          </form>
        </Card>
      </div>
    </PageFrame>
  );
}

function ArtifactsPage(props: { pathname: string }): React.JSX.Element {
  const [repositories, setRepositories] = useState<string[]>([]);
  const [selectedRepository, setSelectedRepository] = useState("");
  const [tags, setTags] = useState<string[]>([]);
  const [digest, setDigest] = useState("");
  const [referrers, setReferrers] = useState<RegistryDescriptor[]>([]);
  const [grants, setGrants] = useState<RegistryGrant[]>([]);
  const [subjectType, setSubjectType] = useState("user");
  const [subjectId, setSubjectID] = useState("admin");
  const [pathPrefix, setPathPrefix] = useState("appliance/");
  const [actions, setActions] = useState({ pull: true, push: false });
  const [message, setMessage] = useState("");

  useEffect(() => {
    void (async () => {
      const nextRepositories = await client.listRepositories();
      setRepositories(nextRepositories);
      setSelectedRepository(nextRepositories[0] || "");
      setGrants(await client.listRegistryGrants());
    })();
  }, []);

  useEffect(() => {
    if (!selectedRepository) {
      return;
    }
    void client.listRepositoryTags(selectedRepository).then(setTags);
  }, [selectedRepository]);

  async function loadReferrers() {
    setReferrers(await client.listRepositoryReferrers(selectedRepository, digest));
  }

  async function createGrant(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    await client.createRegistryGrant({
      subjectType,
      subjectId,
      pathPrefix,
      actions: Object.entries(actions)
        .filter((entry) => entry[1])
        .map((entry) => entry[0])
    });
    setMessage("Registry grant created.");
    setGrants(await client.listRegistryGrants());
  }

  async function deleteGrant(id: string) {
    await client.deleteRegistryGrant(id);
    setMessage("Registry grant deleted.");
    setGrants(await client.listRegistryGrants());
  }

  return (
    <PageFrame
      title="Artifacts"
      description="Registry catalog visibility and grant management."
      pathname={props.pathname}
      onNavigate={navigate}
      tabs={[
        { label: "Catalog", path: "/manage/artifacts", icon: "catalog" },
        { label: "Grants", path: "/manage/artifacts/grants", icon: "key" }
      ]}
    >
      {message ? <div className="message">{message}</div> : null}
      {props.pathname === "/manage/artifacts/grants" ? (
        <div className="grid-two">
          <Card title="Create registry grant" subtitle="User or role-scoped grant creation">
            <form className="stack-form" onSubmit={createGrant}>
              <label className="field">
                <span>Subject type</span>
                <select value={subjectType} onChange={(event) => setSubjectType(event.target.value)}>
                  <option value="user">User</option>
                  <option value="role">Role</option>
                </select>
              </label>
              <label className="field">
                <span>Subject ID</span>
                <input value={subjectId} onChange={(event) => setSubjectID(event.target.value)} />
              </label>
              <label className="field">
                <span>Path prefix</span>
                <input value={pathPrefix} onChange={(event) => setPathPrefix(event.target.value)} />
              </label>
              <label className="checkbox-row">
                <input
                  type="checkbox"
                  checked={actions.pull}
                  onChange={(event) => setActions((current) => ({ ...current, pull: event.target.checked }))}
                />
                <span>Pull</span>
              </label>
              <label className="checkbox-row">
                <input
                  type="checkbox"
                  checked={actions.push}
                  onChange={(event) => setActions((current) => ({ ...current, push: event.target.checked }))}
                />
                <span>Push</span>
              </label>
              <button className="button button--primary" type="submit">
                Create grant
              </button>
            </form>
          </Card>
          <Card title="Current grants" subtitle="Existing registry grant entries">
            <div className="table-list">
              {grants.map((grant) => (
                <div className="table-list__row" key={grant.id}>
                  <div>
                    <strong>
                      {grant.subjectType}:{grant.subjectId}
                    </strong>
                    <span>
                      {grant.pathPrefix} · {grant.actions.join(", ")}
                    </span>
                  </div>
                  <button className="button button--ghost" onClick={() => void deleteGrant(grant.id)}>
                    Delete
                  </button>
                </div>
              ))}
            </div>
          </Card>
        </div>
      ) : (
        <div className="grid-two">
          <Card title="Repository catalog" subtitle="Browse available repositories and tags">
            <label className="field">
              <span>Repository</span>
              <select
                value={selectedRepository}
                onChange={(event) => setSelectedRepository(event.target.value)}
              >
                {repositories.map((repository) => (
                  <option key={repository} value={repository}>
                    {repository}
                  </option>
                ))}
              </select>
            </label>
            <div className="badge-row">
              {tags.map((tag) => (
                <span className="pill" key={tag}>
                  {tag}
                </span>
              ))}
            </div>
          </Card>
          <Card title="Artifact referrers" subtitle="Digest-based referrer lookup">
            <label className="field">
              <span>Digest</span>
              <input value={digest} onChange={(event) => setDigest(event.target.value)} />
            </label>
            <button className="button button--primary" onClick={() => void loadReferrers()}>
              Load referrers
            </button>
            <div className="table-list">
              {referrers.map((referrer) => (
                <div className="table-list__row" key={referrer.digest}>
                  <div>
                    <strong>{referrer.artifactType || referrer.mediaType}</strong>
                    <span>{referrer.digest}</span>
                  </div>
                  <span>{referrer.size} bytes</span>
                </div>
              ))}
            </div>
          </Card>
        </div>
      )}
    </PageFrame>
  );
}

function AnalyzePage(): React.JSX.Element {
  const [workspaces, setWorkspaces] = useState<Workspace[]>([]);
  const [latestJob, setLatestJob] = useState<Job | null>(null);

  useEffect(() => {
    void (async () => {
      setWorkspaces(await client.listWorkspaces().catch(() => []));
      setLatestJob(await client.getCurrentBuildStatus().catch(() => null));
    })();
  }, []);

  const readyCount = workspaces.filter((workspace) => workspace.status === "ready").length;
  const pendingCount = workspaces.filter((workspace) => workspace.status === "pending").length;

  return (
    <PageFrame
      title="Workflow analysis"
      description="A lightweight operational analysis view for current builder workflow activity."
      pathname="/analyze/workflows"
      onNavigate={navigate}
      tabs={[{ label: "Workflow Health", path: "/analyze/workflows", icon: "workflows" }]}
    >
      <div className="stats-grid">
        <StatCard label="Workspaces" value={String(workspaces.length)} />
        <StatCard label="Ready" value={String(readyCount)} tone="success" />
        <StatCard label="Pending" value={String(pendingCount)} />
        <StatCard label="Latest build" value={latestJob?.status || "none"} />
      </div>
      <Card title="Latest workflow signal" subtitle="Current build-oriented analysis placeholder">
        {latestJob ? (
          <div className="detail-list">
            <div>
              <span>Status</span>
              <strong>{latestJob.status}</strong>
            </div>
            <div>
              <span>Target</span>
              <strong>{latestJob.targetName || "Unknown"}</strong>
            </div>
            <div>
              <span>Updated</span>
              <strong>{formatTimestamp(latestJob.updatedAt)}</strong>
            </div>
          </div>
        ) : (
          <EmptyState message="No workflow activity is available yet. This page is ready for richer metrics and alert-backed analysis." />
        )}
      </Card>
    </PageFrame>
  );
}

function AdminPage(props: { pathname: string; capabilities: string[] }): React.JSX.Element {
  const [version, setVersion] = useState<Version | null>(null);
  const [health, setHealth] = useState("unknown");
  const [identity, setIdentity] = useState<ApplianceIdentity | null>(null);

  useEffect(() => {
    if (props.pathname !== "/admin/system-status") {
      return;
    }
    void (async () => {
      const [nextVersion, nextHealth, nextIdentity] = await Promise.all([
        client.getVersion().catch(() => null),
        client.getReady().then((value) => value.status).catch(() => "degraded"),
        client.getIdentity().catch(() => null)
      ]);
      setVersion(nextVersion);
      setHealth(nextHealth);
      setIdentity(nextIdentity);
    })();
  }, [props.pathname]);

  return (
    <PageFrame
      title="Administration"
      description="Appliance-wide operating posture, profile expansion points, and future licensing surfaces."
      pathname={props.pathname}
      onNavigate={navigate}
      tabs={[
        { label: "System Status", path: "/admin/system-status", icon: "status" },
        { label: "Profiles", path: "/admin/profiles", icon: "profiles" },
        { label: "Licensing", path: "/admin/licensing", icon: "license" }
      ]}
    >
      {props.pathname === "/admin/profiles" ? (
        <Card title="Profiles" subtitle="Future appliance feature grouping">
          <EmptyState message="Profile management details will grow here as more feature-specific admin controls are introduced." />
        </Card>
      ) : props.pathname === "/admin/licensing" ? (
        <Card title="Licensing" subtitle="Reserved appliance licensing surface">
          <EmptyState message="Licensing and entitlement workflows will be added here when the supporting control-plane APIs exist." />
        </Card>
      ) : (
        <div className="grid-two">
          <Card title="System status" subtitle="Primary appliance runtime posture">
            <div className="detail-list">
              <div>
                <span>Readiness</span>
                <strong>{health}</strong>
              </div>
              <div>
                <span>Version</span>
                <strong>{version?.version || "Unknown"}</strong>
              </div>
              <div>
                <span>Build time</span>
                <strong>{formatTimestamp(version?.buildTime)}</strong>
              </div>
              <div>
                <span>Commit</span>
                <strong>{version?.commit || "Unknown"}</strong>
              </div>
            </div>
          </Card>
          <Card title="Appliance identity" subtitle="Operator-facing identity and capability context">
            <div className="detail-list">
              <div>
                <span>Appliance name</span>
                <strong>{identity?.applianceName || "Unknown"}</strong>
              </div>
              <div>
                <span>FQDN</span>
                <strong>{identity?.fqdn || "Unavailable"}</strong>
              </div>
              <div>
                <span>Node IPv4</span>
                <strong>{identity?.nodeIPv4 || "Unavailable"}</strong>
              </div>
            </div>
            <div className="badge-row">
              {props.capabilities.map((capability) => (
                <span className="pill pill--navy" key={capability}>
                  {capability}
                </span>
              ))}
            </div>
          </Card>
        </div>
      )}
    </PageFrame>
  );
}

ReactDOM.createRoot(document.getElementById("root")!).render(
  <React.StrictMode>
    <App />
  </React.StrictMode>
);
