import React, { FormEvent, useEffect, useRef, useState } from "react";
import { Card, EmptyState, PageFrame } from "../components";
import { client } from "../lib/api";
import { formatTimestamp } from "../lib/format";
import { navigate } from "../lib/navigate";
import type {
  BuilderCatalogStatus,
  BuilderGitAccessStatus,
  BuildTarget,
  Job,
  WorkProfile,
  Workspace
} from "../types";

function isBaseSettingsPath(pathname: string): boolean {
  return pathname === "/manage/builder/settings" || pathname === "/manage/builder/git-access";
}

export function BuilderPage(props: { pathname: string }): React.JSX.Element {
  const [profiles, setProfiles] = useState<WorkProfile[]>([]);
  const [workspaces, setWorkspaces] = useState<Workspace[]>([]);
  const [currentWorkspace, setCurrentWorkspace] = useState<Workspace | null>(null);
  const [catalog, setCatalog] = useState<BuilderCatalogStatus | null>(null);
  const [gitAccess, setGitAccess] = useState<BuilderGitAccessStatus | null>(null);
  const [targets, setTargets] = useState<BuildTarget[]>([]);
  const [latestJob, setLatestJob] = useState<Job | null>(null);
  const [message, setMessage] = useState("");
  const [workspaceName, setWorkspaceName] = useState("");
  const [workspaceProfile, setWorkspaceProfile] = useState("");
  const [selectedWorkspaceId, setSelectedWorkspaceId] = useState("");
  const [gitName, setGitName] = useState("");
  const [gitHost, setGitHost] = useState("");
  const [gitUsername, setGitUsername] = useState("");
  const [gitToken, setGitToken] = useState("");
  const [buildTarget, setBuildTarget] = useState("");
  const [imageTag, setImageTag] = useState("");
  const catalogFileRef = useRef<HTMLInputElement>(null);

  async function refreshBuilder() {
    const [nextProfiles, nextWorkspaces, nextCurrent, nextCatalog, nextGitAccess, nextTargets, nextJob] =
      await Promise.all([
        client.listWorkProfiles(),
        client.listWorkspaces(),
        client.getCurrentWorkspace(),
        client.getBuilderCatalog(),
        client.getBuilderGitAccess(),
        client.listBuildTargets().catch(() => []),
        client.getCurrentBuildStatus().catch(() => null)
      ]);
    setProfiles(nextProfiles);
    setWorkspaces(nextWorkspaces);
    setCurrentWorkspace(nextCurrent);
    setCatalog(nextCatalog);
    setGitAccess(nextGitAccess);
    setTargets(nextTargets);
    setLatestJob(nextJob);
    setWorkspaceProfile(nextProfiles[0]?.name || "");
    setSelectedWorkspaceId(nextCurrent?.id || nextWorkspaces[0]?.id || "");
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
    setMessage("");
    if (!gitName.trim()) {
      setMessage("Credential name is required.");
      return;
    }
    try {
      await client.updateBuilderGitAccess({
        name: gitName.trim(),
        host: gitHost,
        username: gitUsername,
        token: gitToken
      });
      setGitName("");
      setGitHost("");
      setGitUsername("");
      setGitToken("");
      setMessage("Builder Git access credential saved.");
      await refreshBuilder();
    } catch (error) {
      setMessage(error instanceof Error ? error.message : "Could not save Git access.");
    }
  }

  async function editGitCredential(name: string, host: string, username: string) {
    setGitName(name);
    setGitHost(host);
    setGitUsername(username);
    setGitToken("");
  }

  async function deleteGitCredential(name: string) {
    setMessage("");
    try {
      await client.deleteBuilderGitAccess(name);
      setMessage(`Deleted Git access credential ${name}.`);
      await refreshBuilder();
    } catch (error) {
      setMessage(error instanceof Error ? error.message : "Could not delete Git access.");
    }
  }

  function downloadCatalog() {
    const text = catalog?.document || "{}\n";
    const blob = new Blob([text], { type: "application/yaml" });
    const url = URL.createObjectURL(blob);
    const anchor = document.createElement("a");
    anchor.href = url;
    anchor.download = "build-catalog.yaml";
    anchor.click();
    URL.revokeObjectURL(url);
  }

  async function uploadCatalog(event: React.ChangeEvent<HTMLInputElement>) {
    const file = event.target.files?.[0];
    event.target.value = "";
    if (!file) {
      return;
    }
    setMessage("");
    try {
      const text = await file.text();
      const contentType = file.name.toLowerCase().endsWith(".json")
        ? "application/json"
        : "application/yaml";
      await client.putBuilderCatalog(text, contentType);
      setMessage("Build catalog uploaded.");
      await refreshBuilder();
    } catch (error) {
      setMessage(error instanceof Error ? error.message : "Could not upload build catalog.");
    }
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
      description="Workspace creation, base settings (Git access and build catalog), and build submission."
      pathname={props.pathname}
      onNavigate={navigate}
      tabs={[
        { label: "Workspaces", path: "/manage/builder" },
        { label: "Base Settings", path: "/manage/builder/settings" },
        { label: "Builds", path: "/manage/builder/builds" }
      ]}
    >
      {message ? <div className="message">{message}</div> : null}
      {isBaseSettingsPath(props.pathname) ? (
        <div className="stack-form">
          <div className="grid-two">
            <Card title="Build catalog" subtitle="Single appliance catalog document">
              <div className="detail-list">
                <div>
                  <span>Status</span>
                  <strong>{catalog?.configured ? "Configured" : "Not configured"}</strong>
                </div>
                <div>
                  <span>Updated</span>
                  <strong>
                    {catalog?.updatedAt ? formatTimestamp(catalog.updatedAt) : "Never"}
                  </strong>
                </div>
              </div>
              {!catalog?.configured ? (
                <EmptyState message="Upload an appliance-native build-catalog.yaml before creating a workspace. See build-catalog.example.yaml for the schema." />
              ) : null}
              <div className="button-row">
                <button className="button" type="button" onClick={downloadCatalog}>
                  Download YAML
                </button>
                <button
                  className="button button--primary"
                  type="button"
                  onClick={() => catalogFileRef.current?.click()}
                >
                  Upload YAML
                </button>
                <input
                  ref={catalogFileRef}
                  type="file"
                  accept=".yaml,.yml,.json,application/yaml,text/yaml,application/json"
                  hidden
                  onChange={(event) => void uploadCatalog(event)}
                />
              </div>
            </Card>
            <Card title="Save credential" subtitle="Name + host + username + token">
              <form className="stack-form" onSubmit={saveGitAccess}>
                <label className="field">
                  <span>Credential name</span>
                  <input
                    value={gitName}
                    placeholder="github-com"
                    onChange={(event) => setGitName(event.target.value)}
                  />
                </label>
                <label className="field">
                  <span>Git host</span>
                  <input
                    value={gitHost}
                    placeholder="github.com"
                    onChange={(event) => setGitHost(event.target.value)}
                  />
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
          </div>
          <Card title="Named Git HTTPS credentials" subtitle="One credential per Git host">
            <div className="detail-list">
              <div>
                <span>Coverage</span>
                <strong>{gitAccess?.configured ? "Complete" : "Incomplete"}</strong>
              </div>
              <div>
                <span>Required hosts</span>
                <strong>{gitAccess?.requiredHosts?.join(", ") || "None advertised"}</strong>
              </div>
              <div>
                <span>Covered hosts</span>
                <strong>{gitAccess?.coveredHosts?.join(", ") || "None"}</strong>
              </div>
              <div>
                <span>Missing hosts</span>
                <strong>{gitAccess?.missingHosts?.join(", ") || "None"}</strong>
              </div>
            </div>
            {(gitAccess?.credentials || []).length === 0 ? (
              <EmptyState message="No credentials yet. Save a named credential for each catalog Git host." />
            ) : (
              <div className="stack-form">
                {(gitAccess?.credentials || []).map((credential) => (
                  <div className="detail-list" key={credential.name}>
                    <div>
                      <span>Name</span>
                      <strong>{credential.name}</strong>
                    </div>
                    <div>
                      <span>Host</span>
                      <strong>{credential.host}</strong>
                    </div>
                    <div>
                      <span>Username</span>
                      <strong>{credential.username}</strong>
                    </div>
                    <div className="button-row">
                      <button
                        className="button"
                        type="button"
                        onClick={() => void editGitCredential(credential.name, credential.host, credential.username)}
                      >
                        Edit
                      </button>
                      <button
                        className="button"
                        type="button"
                        onClick={() => void deleteGitCredential(credential.name)}
                      >
                        Delete
                      </button>
                    </div>
                  </div>
                ))}
              </div>
            )}
          </Card>
        </div>
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
