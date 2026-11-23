import React from "react";
import { Card, EmptyState, PageFrame } from "../components";
import { navigate } from "../lib/navigate";
import type { Session } from "../types";

/**
 * First application PWA shell. Business screens can be added here without
 * changing the operator console route tree or its API contracts.
 */
export function ApplicationWorkspacePage(props: { pathname: string; session: Session }): React.JSX.Element {
  const name = props.session.displayName || props.session.username;
  return (
    <PageFrame
      title="Zonity"
      eyebrow="Application"
      description="Your signed-in space for local services, orders, and microzone activity."
      pathname={props.pathname}
      onNavigate={navigate}
      tabs={[]}
    >
      <div className="application-home">
        <Card title={`Good to see you, ${name}.`} subtitle="Your Zonity is ready for the next connection.">
          <div className="application-home__welcome">
            <div>
              <strong>One place for the things around you.</strong>
              <p>Application services will appear here as they become available in this signed appliance release.</p>
            </div>
            <span className="application-home__mark" aria-hidden="true">Z</span>
          </div>
        </Card>
        <div className="application-home__grid">
          <Card title="Services" subtitle="Discover what is available in your microzone.">
            <EmptyState message="Service discovery is coming next." />
          </Card>
          <Card title="Orders" subtitle="Keep track of requests made through your channels.">
            <EmptyState message="Order history will appear here." />
          </Card>
        </div>
        <div className="button-row">
          <button className="button button--primary" type="button" onClick={() => navigate("/account/profile")}>
            Manage your account
          </button>
          <button className="button button--ghost" type="button" onClick={() => navigate("/console", true)}>
            Operations console
          </button>
        </div>
      </div>
    </PageFrame>
  );
}

