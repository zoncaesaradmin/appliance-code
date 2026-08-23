import type React from "react";
import { useEffect } from "react";
import { AccountPage } from "../pages/AccountPage";
import { AdminPage } from "../pages/AdminPage";
import { AnalyzePage } from "../pages/AnalyzePage";
import { ArtifactsPage } from "../pages/ArtifactsPage";
import { BuilderPage } from "../pages/BuilderPage";
import { FilesPage } from "../pages/FilesPage";
import { VideosPage } from "../pages/VideosPage";
import { HomePage } from "../pages/HomePage";
import { navigate } from "../lib/navigate";
import type { Session } from "../types";

function Redirect(props: { to: string }): React.JSX.Element | null {
  useEffect(() => {
    navigate(props.to, true);
  }, [props.to]);
  return null;
}

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
  // Legacy Manage → DNS; now Admin → LAN Services.
  if (props.pathname === "/manage/dns" || props.pathname.startsWith("/manage/dns/")) {
    return <Redirect to="/admin/lan-services" />;
  }
  if (props.pathname.startsWith("/manage/files")) {
    return <FilesPage />;
  }
  if (props.pathname.startsWith("/manage/videos")) {
    return <VideosPage />;
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
