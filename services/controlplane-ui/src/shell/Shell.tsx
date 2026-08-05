import React, { useEffect, useState } from "react";
import {
  BrandMark,
  Icon,
  IconButton,
  cn
} from "../components";
import { client } from "../lib/api";
import { navigate } from "../lib/navigate";
import { useViewSyncGeneration } from "../lib/viewSyncHooks";
import {
  currentMode,
  isSystemAdministrator,
  modeUsesFeatureSelector,
  visibleModes,
  type Mode
} from "../navigation";
import type { Session } from "../types";
import { AboutApplianceDialog } from "./AboutApplianceDialog";
import { RouteView } from "./RouteView";

export function Shell(props: {
  pathname: string;
  session: Session;
  capabilities: string[];
  onLogout: () => Promise<void>;
  onSignedOut: () => void;
}): React.JSX.Element {
  const navigationModes = visibleModes({ session: props.session });
  const mode = currentMode(props.pathname, navigationModes);
  const [userMenuOpen, setUserMenuOpen] = useState(false);
  const [helpMenuOpen, setHelpMenuOpen] = useState(false);
  const [aboutOpen, setAboutOpen] = useState(false);
  const [featureMenuMode, setFeatureMenuMode] = useState<Mode | null>(null);
  const [notificationsOpen, setNotificationsOpen] = useState(false);
  const [notifications, setNotifications] = useState<
    Array<{ id: string; title: string; body: string; actionUrl?: string }>
  >([]);
  // Re-fetch header alerts after any successful mutation that invalidates
  // shell.alerts (pathname change remains a secondary refresh trigger).
  const alertsSync = useViewSyncGeneration("shell.alerts");

  useEffect(() => {
    let cancelled = false;
    void client
      .listNotifications()
      .then((items) => {
        if (!cancelled) {
          setNotifications(items);
        }
      })
      .catch(() => {
        if (!cancelled) {
          setNotifications([]);
        }
      });
    return () => {
      cancelled = true;
    };
  }, [props.pathname, alertsSync]);

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
    setNotificationsOpen(false);
    setFeatureMenuMode(null);
  }

  function openAboutAppliance() {
    closeTransientMenus();
    setAboutOpen(true);
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
    if (!userMenuOpen && !helpMenuOpen && !notificationsOpen) {
      return;
    }
    const closeOnPointer = (event: PointerEvent) => {
      const target = event.target;
      if (target instanceof Element && target.closest(".top-navigation")) {
        return;
      }
      setUserMenuOpen(false);
      setHelpMenuOpen(false);
      setNotificationsOpen(false);
    };
    const closeOnEscape = (event: KeyboardEvent) => {
      if (event.key === "Escape") {
        setUserMenuOpen(false);
        setHelpMenuOpen(false);
        setNotificationsOpen(false);
      }
    };
    window.addEventListener("pointerdown", closeOnPointer);
    window.addEventListener("keydown", closeOnEscape);
    return () => {
      window.removeEventListener("pointerdown", closeOnPointer);
      window.removeEventListener("keydown", closeOnEscape);
    };
  }, [helpMenuOpen, notificationsOpen, userMenuOpen]);

  useEffect(() => {
    if (props.pathname.startsWith("/admin") && !isSystemAdministrator(props.session)) {
      closeTransientMenus();
      navigate("/home", true);
    }
  }, [props.pathname, props.session]);

  useEffect(() => {
    if (props.pathname === "/home/session" || props.pathname === "/account") {
      navigate("/account/profile", true);
      return;
    }
    if (props.pathname === "/home/access") {
      navigate("/account/api-keys", true);
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
          <div className="relative">
            <IconButton
              icon="alerts"
              label="Alerts"
              badge={notifications.length}
              muted={notifications.length === 0}
              onClick={() => {
                setNotificationsOpen((open) => !open);
                setHelpMenuOpen(false);
                setUserMenuOpen(false);
                setFeatureMenuMode(null);
              }}
              title={
                notifications.length
                  ? `${notifications.length} notification${notifications.length === 1 ? "" : "s"}`
                  : "No notifications"
              }
            />
            {notificationsOpen ? (
              <div className="shell-menu absolute right-0 top-[calc(100%+0.5rem)] z-[80] grid min-w-80 gap-2 rounded-2xl border border-slate-200 bg-white p-3 shadow-2xl shadow-slate-900/20 max-[680px]:left-0 max-[680px]:right-auto">
                {notifications.length === 0 ? (
                  <p className="m-0 px-2 py-3 text-sm text-slate-500">No active notifications.</p>
                ) : (
                  notifications.map((note) => (
                    <div key={note.id} className="rounded-2xl bg-slate-50 p-3 text-left">
                      <strong className="block text-sm text-slate-950">{note.title}</strong>
                      <p className="mt-1 mb-3 text-sm leading-5 text-slate-500">{note.body}</p>
                      <div className="button-row">
                        {note.actionUrl ? (
                          <button
                            onClick={() => {
                              closeTransientMenus();
                              navigate(note.actionUrl!);
                            }}
                          >
                            Open
                          </button>
                        ) : null}
                        <button
                          onClick={() => {
                            void client.acknowledgeNotification(note.id).then(() =>
                              setNotifications((current) =>
                                current.filter((item) => item.id !== note.id)
                              )
                            );
                          }}
                        >
                          Dismiss
                        </button>
                      </div>
                    </div>
                  ))
                )}
              </div>
            ) : null}
          </div>
          <div className="relative">
            <IconButton
              icon="help"
              label="Help"
              onClick={() => {
                setHelpMenuOpen((open) => !open);
                setUserMenuOpen(false);
                setNotificationsOpen(false);
                setFeatureMenuMode(null);
              }}
            />
            {helpMenuOpen ? (
              <div className="shell-menu absolute right-0 top-[calc(100%+0.5rem)] z-[80] grid min-w-56 gap-1 rounded-2xl border border-slate-200 bg-white p-2 shadow-2xl shadow-slate-900/20 max-[680px]:left-0 max-[680px]:right-auto">
                <button onClick={openAboutAppliance}>About appliance</button>
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
                setNotificationsOpen(false);
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
                <button onClick={() => onUserAction("/account/profile")}>User profile</button>
                <button onClick={() => onUserAction("/account/password")}>Change password</button>
                <button onClick={() => onUserAction("/account/api-keys")}>Manage API keys</button>
                <button onClick={() => onUserAction("/account/session")}>Session info</button>
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
          <RouteView
            pathname={props.pathname}
            session={props.session}
            capabilities={props.capabilities}
            onSignedOut={props.onSignedOut}
          />
        </main>
      </div>
      <AboutApplianceDialog open={aboutOpen} onClose={() => setAboutOpen(false)} />
    </div>
  );
}
