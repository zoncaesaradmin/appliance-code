import React from "react";
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
        <button className="application-shell__brand" type="button" aria-label="Zonited home" onClick={() => navigate("/app")}>
          <img className="application-shell__logo" src="/zonited-logo-circle.png" alt="Zonited" />
          <span>
            <strong>Zonited</strong>
          </span>
        </button>
        <nav className="application-shell__nav" aria-label="Application actions">
          <button type="button" className="application-shell__signout" onClick={() => void props.onLogout()}>
            Sign out
          </button>
        </nav>
      </header>
      <main className="application-shell__main">
        <ApplicationWorkspacePage pathname={props.pathname} session={props.session} />
      </main>
      <footer className="application-shell__footer">
        <span>Zonited</span>
        <span>Private by design</span>
      </footer>
    </div>
  );
}
