import type React from "react";
import { AccountPage } from "../pages/AccountPage";
import { AdminPage } from "../pages/AdminPage";
import { AnalyzePage } from "../pages/AnalyzePage";
import { ArtifactsPage } from "../pages/ArtifactsPage";
import { BuilderPage } from "../pages/BuilderPage";
import { DNSPage } from "../pages/DNSPage";
import { HomePage } from "../pages/HomePage";
import type { Session } from "../types";

export function RouteView(props: {
  pathname: string;
  session: Session;
  capabilities: string[];
  onSignedOut: () => void;
}): React.JSX.Element {
  if (props.pathname.startsWith("/account")) {
    return (
      <AccountPage
        pathname={props.pathname}
        session={props.session}
        onSignedOut={props.onSignedOut}
      />
    );
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
  return <HomePage pathname={props.pathname} session={props.session} capabilities={props.capabilities} />;
}
