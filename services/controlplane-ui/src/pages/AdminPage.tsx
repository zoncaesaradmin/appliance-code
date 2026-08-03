import React, { useEffect, useState } from "react";
import { Card, EmptyState, PageFrame } from "../components";
import { client } from "../lib/api";
import { formatTimestamp } from "../lib/format";
import { navigate } from "../lib/navigate";
import type { ApplianceIdentity, Version } from "../types";

export function AdminPage(props: { pathname: string; capabilities: string[] }): React.JSX.Element {
  const [version, setVersion] = useState<Version | null>(null);
  const [health, setHealth] = useState("unknown");
  const [identity, setIdentity] = useState<ApplianceIdentity | null>(null);

  const isProfiles = props.pathname === "/admin/profiles" || props.pathname.startsWith("/admin/profiles/");
  const isLicensing =
    props.pathname === "/admin/licensing" || props.pathname.startsWith("/admin/licensing/");
  const isSystemStatus =
    props.pathname === "/admin/system-status" ||
    props.pathname.startsWith("/admin/system-status/") ||
    (!isProfiles && !isLicensing);

  useEffect(() => {
    if (!isSystemStatus) {
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
  }, [isSystemStatus, props.pathname]);

  if (isProfiles) {
    return (
      <PageFrame
        title="Profiles"
        eyebrow=""
        description="Profile management entry points for future appliance feature grouping."
        pathname={props.pathname}
        onNavigate={navigate}
        tabs={[{ label: "Overview", path: "/admin/profiles" }]}
      >
        <Card title="Overview" subtitle="Future appliance feature grouping">
          <EmptyState message="Profile management details will grow here as more feature-specific admin controls are introduced." />
        </Card>
      </PageFrame>
    );
  }

  if (isLicensing) {
    return (
      <PageFrame
        title="Licensing"
        eyebrow=""
        description="License and entitlement surfaces for this appliance."
        pathname={props.pathname}
        onNavigate={navigate}
        tabs={[{ label: "Overview", path: "/admin/licensing" }]}
      >
        <Card title="Overview" subtitle="Reserved appliance licensing surface">
          <EmptyState message="Licensing and entitlement workflows will be added here when the supporting control-plane APIs exist." />
        </Card>
      </PageFrame>
    );
  }

  return (
    <PageFrame
      title="System Status"
      eyebrow=""
      description="Version, readiness, and appliance identity."
      pathname={props.pathname}
      onNavigate={navigate}
      tabs={[
        { label: "Overview", path: "/admin/system-status" },
        { label: "Resources", path: "/admin/system-status/resources" }
      ]}
    >
      {props.pathname === "/admin/system-status/resources" ? (
        <Card title="Resources" subtitle="Host and cluster resource posture">
          <EmptyState message="Resource utilization and capacity details will appear here when the supporting control-plane APIs are available." />
        </Card>
      ) : (
        <div className="grid-two">
          <Card title="Overview" subtitle="Primary appliance runtime posture">
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
