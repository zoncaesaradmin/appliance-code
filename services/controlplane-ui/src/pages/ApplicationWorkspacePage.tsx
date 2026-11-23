import React from "react";
import { Card, EmptyState, PageFrame } from "../components";
import { navigate } from "../lib/navigate";

/**
 * Transitional application entry point. The signed application PWA is a
 * separate delivery artifact; keeping this route explicit prevents an
 * application URL from silently displaying the operator console while that
 * artifact is not installed.
 */
export function ApplicationWorkspacePage(props: { pathname: string }): React.JSX.Element {
  return (
    <PageFrame
      title="Zonity"
      eyebrow="Application"
      description="The application workspace will be delivered separately from the appliance operations console."
      pathname={props.pathname}
      onNavigate={navigate}
      tabs={[]}
    >
      <Card title="Application workspace" subtitle="Zonity application services">
        <div className="stack">
          <EmptyState message="The signed Zonity application PWA is not included in this appliance release yet." />
          <div className="button-row">
            <button className="button button--primary" type="button" onClick={() => navigate("/console", true)}>
              Open Operations Console
            </button>
          </div>
        </div>
      </Card>
    </PageFrame>
  );
}

