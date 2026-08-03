import React, { FormEvent, useEffect, useState } from "react";
import { Card, EmptyState, PageFrame } from "../components";
import { client } from "../lib/api";
import { formatTimestamp } from "../lib/format";
import { navigate } from "../lib/navigate";
import type {
  BuilderGitAccessStatus,
  BuildTarget,
  Job,
  WorkProfile,
  Workspace
} from "../types";

export function BuilderPage(props: { pathname: string }): React.JSX.Element {
  const [profiles, setProfiles] = useState<WorkProfile[]>([]);
  const [workspaces, setWorkspaces] = useState<Workspace[]>([]);
  const [currentWorkspace, setCurrentWorkspace] = useState<Workspace | null>(null);
  const [gitAccess, setGitAccess] = useState<BuilderGitAccessStatus | null>(null);
  const [targets, setTargets] = useState<BuildTarget[]>([]);
  const [latestJob, setLatestJob] = useState<Job | null>(null);
  const [message, setMessage] = useState("");
  const [workspaceName, setWorkspaceName] = useState("");
  const [workspaceProfile, setWorkspaceProfile] = useState("");
  const [selectedWorkspaceId, setSelectedWorkspaceId] = useState("");
  const [gitHost, setGitHost] = useState("");
  const [gitUsername, setGitUsername] = useState("");
  const [gitToken, setGitToken] = useState("");
  const [buildTarget, setBuildTarget] = useState("");
  const [imageTag, setImageTag] = useState("");

  async function refreshBuilder() {
    const [nextProfiles, nextWorkspaces, nextCurrent, nextGitAccess, nextTargets, nextJob] =
      await Promise.all([
        client.listWorkProfiles(),
        client.listWorkspaces(),
        client.getCurrentWorkspace(),
        client.getBuilderGitAccess(),
        client.listBuildTargets().catch(() => []),
        client.getCurrentBuildStatus().catch(() => null)
      ]);
    setProfiles(nextProfiles);
    setWorkspaces(nextWorkspaces);
    setCurrentWorkspace(nextCurrent);
    setGitAccess(nextGitAccess);
    setTargets(nextTargets);
    setLatestJob(nextJob);
    setWorkspaceProfile(nextProfiles[0]?.name || "");
    setSelectedWorkspaceId(nextCurrent?.id || nextWorkspaces[0]?.id || "");
    setGitHost(nextGitAccess.host || "");
    setGitUsername(nextGitAccess.username || "");
  }

  useEffect(() => {
    void refreshBuilder();
  }, []);

  useEffect(() => {
    if (latestJob?.status !== "running") {
      return;
    }
    const timer = window.setTimeout(() => {
      void client.getCurrentBuildStatus().then(setLatestJob);
    }, 2500);
    return () => window.clearTimeout(timer);
  }, [latestJob]);

  async function createWorkspace(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    setMessage("");
    try {
      await client.createWorkspace({ name: workspaceName, workProfile: workspaceProfile });
      setWorkspaceName("");
      setMessage("Workspace created and selected.");
      await refreshBuilder();
    } catch (error) {
      setMessage(error instanceof Error ? error.message : "Could not create workspace.");
    }
  }

  async function selectWorkspace() {
    await client.setCurrentWorkspace(selectedWorkspaceId);
    setMessage("Current workspace updated.");
    await refreshBuilder();
  }

  async function deleteWorkspace() {
    if (!selectedWorkspaceId) {
      return;
    }
    await client.deleteWorkspace(selectedWorkspaceId);
    setMessage("Workspace deleted.");
    await refreshBuilder();
  }

  async function saveGitAccess(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    await client.updateBuilderGitAccess({
      host: gitHost,
      username: gitUsername,
      token: gitToken
    });
    setGitToken("");
    setMessage("Builder Git access updated.");
    await refreshBuilder();
  }

  async function submitBuild(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    const job = await client.submitBuild({ targetName: buildTarget, imageTag });
    setLatestJob(job);
    setMessage("Build submitted.");
  }

  return (
    <PageFrame
      title="Builder"
      eyebrow=""
      description="Workspace creation, appliance-wide Git access, and build submission."
      pathname={props.pathname}
      onNavigate={navigate}
      tabs={[
        { label: "Workspaces", path: "/manage/builder" },
        { label: "Git Access", path: "/manage/builder/git-access" },
        { label: "Builds", path: "/manage/builder/builds" }
      ]}
    >
      {message ? <div className="message">{message}</div> : null}
      {props.pathname === "/manage/builder/git-access" ? (
        <Card title="Builder Git HTTPS access" subtitle="Shared appliance credential set">
          <form className="stack-form" onSubmit={saveGitAccess}>
            <div className="detail-list">
              <div>
                <span>Configured</span>
                <strong>{gitAccess?.configured ? "Yes" : "No"}</strong>
              </div>
              <div>
                <span>Required hosts</span>
                <strong>{gitAccess?.requiredHosts?.join(", ") || "None advertised"}</strong>
              </div>
            </div>
            <label className="field">
              <span>Git host</span>
              <input value={gitHost} onChange={(event) => setGitHost(event.target.value)} />
            </label>
            <label className="field">
              <span>Git username</span>
              <input value={gitUsername} onChange={(event) => setGitUsername(event.target.value)} />
            </label>
            <label className="field">
              <span>Git token</span>
              <input
                type="password"
                value={gitToken}
                onChange={(event) => setGitToken(event.target.value)}
              />
            </label>
            <button className="button button--primary" type="submit">
              Save Git access
            </button>
          </form>
        </Card>
      ) : props.pathname === "/manage/builder/builds" ? (
        <div className="grid-two">
          <Card title="Submit build" subtitle="Current workspace target selection">
            <form className="stack-form" onSubmit={submitBuild}>
              <label className="field">
                <span>Current workspace</span>
                <input value={currentWorkspace?.name || "No current workspace"} disabled />
              </label>
              <label className="field">
                <span>Target</span>
                <select value={buildTarget} onChange={(event) => setBuildTarget(event.target.value)}>
                  <option value="">Select a build target</option>
                  {targets.map((target) => (
                    <option key={target.name} value={target.name}>
                      {target.name}
                    </option>
                  ))}
                </select>
              </label>
              <label className="field">
                <span>Image tag</span>
                <input value={imageTag} onChange={(event) => setImageTag(event.target.value)} />
              </label>
              <button className="button button--primary" type="submit">
                Submit build
              </button>
            </form>
          </Card>
          <Card title="Latest build" subtitle="Most recent current-workspace build status">
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
                  <span>Artifact</span>
                  <strong>{latestJob.artifactRef || "Not available"}</strong>
                </div>
                <div>
                  <span>Updated</span>
                  <strong>{formatTimestamp(latestJob.updatedAt)}</strong>
                </div>
              </div>
            ) : (
              <EmptyState message="No build has been submitted for the current workspace yet." />
            )}
          </Card>
        </div>
      ) : (
        <div className="grid-two">
          <Card title="Workspace selection" subtitle="Switch or delete an existing workspace">
            <label className="field">
              <span>Existing workspaces</span>
              <select
                value={selectedWorkspaceId}
                onChange={(event) => setSelectedWorkspaceId(event.target.value)}
              >
                <option value="">Select a workspace</option>
                {workspaces.map((workspace) => (
                  <option key={workspace.id} value={workspace.id}>
                    {workspace.name} ({workspace.workProfile})
                  </option>
                ))}
              </select>
            </label>
            <div className="button-row">
              <button className="button button--primary" onClick={() => void selectWorkspace()}>
                Set current
              </button>
              <button className="button button--ghost" onClick={() => void deleteWorkspace()}>
                Delete
              </button>
            </div>
            {currentWorkspace ? (
              <div className="status-box">
                <strong>Current workspace</strong>
                <span>
                  {currentWorkspace.name} · {currentWorkspace.status}
                </span>
              </div>
            ) : null}
          </Card>
          <Card title="Create workspace" subtitle="Provision a workspace from a selected profile">
            <form className="stack-form" onSubmit={createWorkspace}>
              <label className="field">
                <span>Workspace name</span>
                <input
                  value={workspaceName}
                  onChange={(event) => setWorkspaceName(event.target.value)}
                />
              </label>
              <label className="field">
                <span>Workspace profile</span>
                <select
                  value={workspaceProfile}
                  onChange={(event) => setWorkspaceProfile(event.target.value)}
                >
                  {profiles.map((profile) => (
                    <option key={profile.name} value={profile.name}>
                      {profile.name}
                    </option>
                  ))}
                </select>
              </label>
              <button className="button button--primary" type="submit">
                Create workspace
              </button>
            </form>
          </Card>
        </div>
      )}
    </PageFrame>
  );
}
