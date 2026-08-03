import React, { useEffect, useState } from "react";
import { Card, EmptyState, PageFrame, StatCard } from "../components";
import { client } from "../lib/api";
import { capabilityBadge } from "../lib/format";
import { navigate } from "../lib/navigate";
import type { ApplianceIdentity, Session, Version } from "../types";

export function HomePage(props: {
  pathname: string;
  session: Session;
  capabilities: string[];
}): React.JSX.Element {
  const [version, setVersion] = useState<Version | null>(null);
  const [health, setHealth] = useState("unknown");
  const [identity, setIdentity] = useState<ApplianceIdentity | null>(null);

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

  return (
    <PageFrame
      title={`Welcome, ${props.session.username}`}
      eyebrow=""
      pathname={props.pathname}
      onNavigate={navigate}
      tabs={[
        { label: "Overview", path: "/home" },
        { label: "Topology", path: "/home/topology" }
      ]}
    >
      {props.pathname === "/home/topology" ? (
        <Card title="Topology" subtitle="Connected appliance topology">
          <EmptyState message="Topology details are not available yet. This view will show how appliances are connected for the current deployment." />
        </Card>
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
