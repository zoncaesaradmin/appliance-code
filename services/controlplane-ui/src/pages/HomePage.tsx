import React, { useEffect, useMemo, useState } from "react";
import { Card, EmptyState, PageFrame, ResourceList, ResourceListRow, StatCard } from "../components";
import { ApiError } from "../client";
import { client } from "../lib/api";
import { capabilityBadge } from "../lib/format";
import { navigate } from "../lib/navigate";
import { useVideoPoster } from "../lib/useVideoPoster";
import { useViewSyncGeneration, useViewSyncTag } from "../lib/viewSyncHooks";
import type { ApplianceFileEntry, ApplianceIdentity, ApplianceSetupState, AuditEvent, FocusContent, Session, Version } from "../types";

function focusVideoEntry(content: FocusContent): ApplianceFileEntry {
  const segments = content.resourcePath.split("/");
  return { name: segments.at(-1) || content.title, path: content.resourcePath, type: "file", sizeBytes: 0, modifiedAt: content.publishedAt };
}

function FocusVideo(props: { content: FocusContent; src: string }): React.JSX.Element {
  const entry = focusVideoEntry(props.content);
  const { poster, posterRef } = useVideoPoster(entry);
  return (
    <div className="focus-video" ref={posterRef}>
      <video className="focus-video__player" controls preload="metadata" poster={poster || undefined} src={props.src}>
        Your browser cannot play this video.
      </video>
    </div>
  );
}

function FocusFile(props: { content: FocusContent }): React.JSX.Element | null {
  const [url, setURL] = useState("");
  const [text, setText] = useState("");
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState("");
  const resourcePath = props.content.resourcePath;
  useEffect(() => {
    let objectURL = "";
    let cancelled = false;
    setLoading(true);
    setError("");
    setText("");
    setURL("");
    void client.downloadApplianceFile(resourcePath).then(async (blob) => {
      if (cancelled) return;
      if (blob.type.startsWith("text/") || /\.(txt|md|json|yaml|yml|log|csv|xml)$/i.test(resourcePath)) {
        setText(await blob.text());
      } else {
        objectURL = URL.createObjectURL(blob);
        setURL(objectURL);
      }
    }).catch((err) => {
      if (!cancelled) setError(err instanceof Error ? err.message : "Could not load the focused file.");
    }).finally(() => {
      if (!cancelled) setLoading(false);
    });
    return () => { cancelled = true; if (objectURL) URL.revokeObjectURL(objectURL); };
  }, [resourcePath]);
  if (loading) return <EmptyState message="Loading current focus…" />;
  if (error) return <EmptyState message={error} />;
  if (text) return <pre className="focus-file focus-file--text">{text}</pre>;
  if (/\.(png|jpe?g|gif|webp|svg)$/i.test(resourcePath)) return <img className="focus-file focus-file--image" src={url} alt={props.content.title} />;
  if (/\.pdf$/i.test(resourcePath)) return url ? <iframe className="focus-file focus-file--pdf" src={url} title={props.content.title} /> : null;
  return url ? <a className="button button--primary" href={url} target="_blank" rel="noreferrer">{props.content.title}</a> : null;
}

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
  const [focusContent, setFocusContent] = useState<FocusContent | null>(null);
  const [focusPlaybackURL, setFocusPlaybackURL] = useState("");
  const [focusPlaybackError, setFocusPlaybackError] = useState("");
  const pageSync = useViewSyncGeneration("page");
  const setupTag = useViewSyncTag("setup");
  const showAudit = canReadAudit(props.session);

  const tabs = useMemo(() => {
    const next = [
      { label: "Overview", path: "/home" },
      { label: "Connectivity", path: "/home/connectivity" }
    ];
    if (showAudit) {
      next.push({ label: "Audit Logs", path: "/home/audit-logs" });
    }
    return next;
  }, [showAudit]);

  useEffect(() => {
    void (async () => {
      const [nextVersion, nextHealth, nextIdentity, nextSetup, nextFocusContent] = await Promise.all([
        client.getVersion().catch(() => null),
        client.getReady().then((value) => value.status).catch(() => "degraded"),
        client.getIdentity().catch(() => null),
        client.getApplianceSetupState().catch(() => null),
        props.capabilities.includes("focus-content") ? client.getFocusContent().catch(() => null) : Promise.resolve(null)
      ]);
      setVersion(nextVersion);
      setHealth(nextHealth);
      setIdentity(nextIdentity);
      setSetupState(nextSetup);
      setFocusContent(nextFocusContent);
    })();
  }, [pageSync, setupTag]);

  useEffect(() => {
    if (focusContent?.resourceType !== "video") {
      setFocusPlaybackURL("");
      return;
    }
    let cancelled = false;
    setFocusPlaybackError("");
    void client.prepareVideoPlayback().then(() => {
      if (!cancelled) setFocusPlaybackURL(client.videoStreamURL(focusContent.resourcePath));
    }).catch((err) => {
      if (!cancelled) setFocusPlaybackError(err instanceof ApiError ? err.detail : err instanceof Error ? err.message : "Could not load the video.");
    });
    return () => { cancelled = true; };
  }, [focusContent?.resourcePath, focusContent?.resourceType]);

  return (
    <PageFrame
      title={`Welcome, ${props.session.displayName || props.session.username}`}
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
      ) : props.pathname === "/home/connectivity" ? (
        <Card title="Connectivity" subtitle="Connected appliance network and links">
          <EmptyState message="Connectivity details are not available yet. This view will show how appliances are connected for the current deployment." />
        </Card>
      ) : (
        <div className="stack">
          {focusContent ? (
            <Card title="Current focus" subtitle="">
              <div className="stack">
                <p>
                  Type: <strong>{focusContent.resourceType === "video" ? "Video" : "File"}</strong>
                </p>
                {focusContent.message ? <p>{focusContent.message}</p> : null}
                {focusContent.resourceType === "video" && focusPlaybackURL ? <FocusVideo content={focusContent} src={focusPlaybackURL} /> : null}
				{focusContent.resourceType === "file" ? <FocusFile content={focusContent} /> : null}
                {focusPlaybackError ? <EmptyState message={focusPlaybackError} /> : null}
              </div>
            </Card>
          ) : null}
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
            <StatCard label="Profile" value={setupState?.activeProfile || "Unknown"} />
            <StatCard label="Capabilities" value={String(props.capabilities.length)} />
            <StatCard label="Appliance Name" value={identity?.applianceName || "Unknown"} />
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
