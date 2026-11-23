import React from "react";
import { BrandMark } from "../components";
import { navigate } from "../lib/navigate";
import { ApplicationWorkspacePage } from "../pages/ApplicationWorkspacePage";
import type { Session } from "../types";

export function ApplicationShell(props: {
  pathname: string;
  session: Session;
  onLogout: () => Promise<void>;
}): React.JSX.Element {
  return (
    <div className="application-shell">
      <header className="application-shell__header">
        <button className="application-shell__brand" type="button" onClick={() => navigate("/app")}>
          <BrandMark size="sm" />
          <span>
            <strong>Zonited</strong>
            <small>Your local space</small>
          </span>
        </button>
        <nav className="application-shell__nav" aria-label="Zonited navigation">
          <button type="button" className="application-shell__nav-link application-shell__nav-link--active" onClick={() => navigate("/app")}>
            My space
          </button>
          <button type="button" className="application-shell__nav-link" onClick={() => navigate("/account/profile")}>
            Account
          </button>
          <button type="button" className="application-shell__nav-link" onClick={() => navigate("/console", true)}>
            Operations console
          </button>
          <button type="button" className="application-shell__signout" onClick={() => void props.onLogout()}>
            Sign out
          </button>
        </nav>
      </header>
      <main className="application-shell__main">
        <ApplicationWorkspacePage pathname={props.pathname} session={props.session} />
      </main>
      <footer className="application-shell__footer">
        <span>Private by design</span>
        <span>{props.session.displayName || props.session.username}</span>
      </footer>
    </div>
  );
}
