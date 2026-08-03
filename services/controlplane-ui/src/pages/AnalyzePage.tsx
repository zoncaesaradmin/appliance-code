import React, { useEffect, useState } from "react";
import { Card, EmptyState, PageFrame, StatCard } from "../components";
import { client } from "../lib/api";
import { formatTimestamp } from "../lib/format";
import { navigate } from "../lib/navigate";
import type { Job, Workspace } from "../types";

export function AnalyzePage(): React.JSX.Element {
  const [workspaces, setWorkspaces] = useState<Workspace[]>([]);
  const [latestJob, setLatestJob] = useState<Job | null>(null);

  useEffect(() => {
    void (async () => {
      setWorkspaces(await client.listWorkspaces().catch(() => []));
      setLatestJob(await client.getCurrentBuildStatus().catch(() => null));
    })();
  }, []);

  const readyCount = workspaces.filter((workspace) => workspace.status === "ready").length;
  const pendingCount = workspaces.filter((workspace) => workspace.status === "pending").length;

  return (
    <PageFrame
      title="Workflow analysis"
      eyebrow=""
      description="A lightweight operational analysis view for current builder workflow activity."
      pathname="/analyze/workflows"
      onNavigate={navigate}
      tabs={[{ label: "Workflow Health", path: "/analyze/workflows" }]}
    >
      <div className="stats-grid">
        <StatCard label="Workspaces" value={String(workspaces.length)} />
        <StatCard label="Ready" value={String(readyCount)} tone="success" />
        <StatCard label="Pending" value={String(pendingCount)} />
        <StatCard label="Latest build" value={latestJob?.status || "none"} />
      </div>
      <Card title="Latest workflow signal" subtitle="Current build-oriented analysis placeholder">
        {latestJob ? (
          <div className="detail-list">
            <div>
              <span>Status</span>
              <strong>{latestJob.status}</strong>
            </div>
            <div>
              <span>Target</span>
              <strong>{latestJob.targetName || "Unknown"}</strong>
            </div>
            <div>
              <span>Updated</span>
              <strong>{formatTimestamp(latestJob.updatedAt)}</strong>
            </div>
          </div>
        ) : (
          <EmptyState message="No workflow activity is available yet. This page is ready for richer metrics and alert-backed analysis." />
        )}
      </Card>
    </PageFrame>
  );
}
