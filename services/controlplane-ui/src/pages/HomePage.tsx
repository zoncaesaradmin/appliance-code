import React, { useEffect, useMemo, useState } from "react";
import { Card, EmptyState, PageFrame, ResourceList, ResourceListRow, StatCard } from "../components";
import { ApiError } from "../client";
import { client } from "../lib/api";
import { capabilityBadge } from "../lib/format";
import { navigate } from "../lib/navigate";
import { useViewSyncGeneration, useViewSyncTag } from "../lib/viewSyncHooks";
import type { ApplianceIdentity, ApplianceSetupState, AuditEvent, Session, Version } from "../types";

function canReadAudit(session: Session): boolean {
  return session.permissions.some((permission) => permission.trim().toLowerCase() === "audit.read");
}

function formatTarget(event: AuditEvent): string {
  if (!event.targetType && !event.targetId) {
    return "—";
  }
  if (event.targetType && event.targetId) {
    return `${event.targetType}:${event.targetId}`;
  }
  return event.targetType || event.targetId || "—";
}

function formatActor(event: AuditEvent): string {
  if (event.actorUserId) {
    return event.actorUserId;
  }
  return event.actorType || "—";
}

function AuditLogsPanel(): React.JSX.Element {
  const [items, setItems] = useState<AuditEvent[]>([]);
  const [nextCursor, setNextCursor] = useState<string | undefined>();
  const [cursorStack, setCursorStack] = useState<Array<string | undefined>>([undefined]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);
  const pageSync = useViewSyncGeneration("page");

  const pageIndex = cursorStack.length - 1;
  const currentCursor = cursorStack[pageIndex];

  useEffect(() => {
    let cancelled = false;
    void (async () => {
      setLoading(true);
      setError(null);
      try {
        const result = await client.listAuditEvents({ limit: 10, cursor: currentCursor });
        if (cancelled) {
          return;
        }
        setItems(result.items);
        setNextCursor(result.nextCursor);
      } catch (err) {
        if (cancelled) {
          return;
        }
        if (err instanceof ApiError && err.status === 403) {
          setError("You do not have permission to read audit logs.");
        } else {
          setError(err instanceof Error ? err.message : "Failed to load audit logs");
        }
        setItems([]);
        setNextCursor(undefined);
      } finally {
        if (!cancelled) {
          setLoading(false);
        }
      }
    })();
    return () => {
      cancelled = true;
    };
  }, [currentCursor, pageSync]);

  return (
    <Card title="Audit Logs" subtitle="Newest security and mutation events first">
      {error ? <EmptyState message={error} /> : null}
      {!error && loading ? <EmptyState message="Loading audit events…" /> : null}
      {!error && !loading && items.length === 0 ? <EmptyState message="No audit events yet." /> : null}
      {!error && !loading && items.length > 0 ? (
        <div className="stack">
          <ResourceList className="audit-log-list">
            {items.map((event) => (
              <ResourceListRow
                key={event.id}
                ariaLabel={`${event.action} ${event.outcome}`}
                columns={[
                  { key: "time", label: "Time", value: new Date(event.occurredAt).toLocaleString() },
                  { key: "action", label: "Action", value: event.action },
                  {
                    key: "outcome",
                    label: "Outcome",
                    value: (
                      <span className={event.severity === "high" ? "pill pill--warn" : "pill pill--navy"}>
                        {event.outcome}
                        {event.severity === "high" ? " · high" : ""}
                      </span>
                    )
                  },
                  { key: "actor", label: "Actor", value: formatActor(event) },
                  { key: "target", label: "Target", value: formatTarget(event) },
                  { key: "source", label: "Source", value: event.sourceAddr || "—" },
                  { key: "request", label: "Request ID", value: event.requestId || "—" }
                ]}
              />
            ))}
          </ResourceList>
          <div className="button-row">
            <button
              className="button"
              type="button"
              disabled={pageIndex === 0 || loading}
              onClick={() => setCursorStack((stack) => stack.slice(0, -1))}
            >
              Previous page
            </button>
            <button
              className="button button--primary"
              type="button"
              disabled={!nextCursor || loading}
              onClick={() => {
                if (!nextCursor) {
                  return;
                }
                setCursorStack((stack) => [...stack, nextCursor]);
              }}
            >
              Next page
            </button>
          </div>
        </div>
      ) : null}
    </Card>
  );
}

export function HomePage(props: {
  pathname: string;
  session: Session;
  capabilities: string[];
}): React.JSX.Element {
  const [version, setVersion] = useState<Version | null>(null);
  const [health, setHealth] = useState("unknown");
  const [identity, setIdentity] = useState<ApplianceIdentity | null>(null);
  const [setupState, setSetupState] = useState<ApplianceSetupState | null>(null);
  const pageSync = useViewSyncGeneration("page");
  const setupTag = useViewSyncTag("setup");
  const showAudit = canReadAudit(props.session);

  const tabs = useMemo(() => {
    const next = [
      { label: "Overview", path: "/home" },
      { label: "Topology", path: "/home/topology" }
    ];
    if (showAudit) {
      next.push({ label: "Audit Logs", path: "/home/audit-logs" });
    }
    return next;
  }, [showAudit]);

  useEffect(() => {
    void (async () => {
      const [nextVersion, nextHealth, nextIdentity, nextSetup] = await Promise.all([
        client.getVersion().catch(() => null),
        client.getReady().then((value) => value.status).catch(() => "degraded"),
        client.getIdentity().catch(() => null),
        client.getApplianceSetupState().catch(() => null)
      ]);
      setVersion(nextVersion);
      setHealth(nextHealth);
      setIdentity(nextIdentity);
      setSetupState(nextSetup);
    })();
  }, [pageSync, setupTag]);

  return (
    <PageFrame
      title={`Welcome, ${props.session.username}`}
      eyebrow=""
      pathname={props.pathname}
      onNavigate={navigate}
      tabs={tabs}
    >
      {props.pathname === "/home/audit-logs" ? (
        showAudit ? (
          <AuditLogsPanel />
        ) : (
          <Card title="Audit Logs" subtitle="Administrator access required">
            <EmptyState message="You do not have permission to read audit logs." />
          </Card>
        )
      ) : props.pathname === "/home/topology" ? (
        <Card title="Topology" subtitle="Connected appliance topology">
          <EmptyState message="Topology details are not available yet. This view will show how appliances are connected for the current deployment." />
        </Card>
      ) : (
        <div className="stack">
          {setupState?.licensingUnresolved ? (
            <Card title="Licensing setup required" subtitle="Complete licensing after first login">
              <EmptyState message="Licensing is not configured. Configure licensing to unlock entitled capabilities, or continue with the base entitlement." />
              <button className="button button--primary" onClick={() => navigate("/admin/licensing")}>
                Open Licensing
              </button>
            </Card>
          ) : null}
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
