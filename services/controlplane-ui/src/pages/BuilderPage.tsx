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

function isSettingsPath(pathname: string): boolean {
  return pathname === "/manage/builder/settings" || pathname === "/manage/builder/git-access";
}

function isWorkspacesPath(pathname: string): boolean {
  return pathname === "/manage/builder/workspaces";
}

function builderTabPathname(pathname: string): string {
  if (pathname === "/manage/builder/builds") {
    return "/manage/builder";
  }
  if (pathname === "/manage/builder/git-access") {
    return "/manage/builder/settings";
  }
  return pathname;
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
  const [messageIsError, setMessageIsError] = useState(false);
  const [workspaceName, setWorkspaceName] = useState("");
  const [workspaceProfile, setWorkspaceProfile] = useState("");
  const [gitName, setGitName] = useState("");
  const [gitHost, setGitHost] = useState("");
  const [gitUsername, setGitUsername] = useState("");
  const [gitToken, setGitToken] = useState("");
  const [buildTarget, setBuildTarget] = useState("");
  const [imageTag, setImageTag] = useState("");
  const [showCreateWorkspace, setShowCreateWorkspace] = useState(false);
  const catalogFileRef = useRef<HTMLInputElement>(null);

  async function refreshSettings() {
    const [nextCatalog, nextGitAccess] = await Promise.all([
      client.getBuilderCatalog(),
      client.getBuilderGitAccess()
    ]);
    setCatalog(nextCatalog);
    setGitAccess(nextGitAccess);
  }

  async function refreshWorkspaces() {
    const [nextProfiles, nextWorkspaces, nextCurrent] = await Promise.all([
      client.listWorkProfiles(),
      client.listWorkspaces(),
      client.getCurrentWorkspace()
    ]);
    setProfiles(nextProfiles);
    setWorkspaces(nextWorkspaces);
    setCurrentWorkspace(nextCurrent);
    setWorkspaceProfile(nextProfiles[0]?.name || "");
  }

  async function refreshBuild() {
    const [nextCurrent, nextTargets, nextJob] = await Promise.all([
      client.getCurrentWorkspace(),
      client.listBuildTargets(),
      client.getCurrentBuildStatus()
    ]);
    setCurrentWorkspace(nextCurrent);
    setTargets(nextTargets);
    setLatestJob(nextJob);
  }

  async function refreshForPath(pathname: string) {
    if (isSettingsPath(pathname)) {
      await refreshSettings();
      return;
    }
    if (isWorkspacesPath(pathname)) {
      await refreshWorkspaces();
      return;
    }
    await refreshBuild();
  }

  useEffect(() => {
    void refreshForPath(props.pathname);
  }, [props.pathname]);

  useEffect(() => {
    if (latestJob?.status !== "running") {
      return;
    }
    const timer = window.setTimeout(() => {
      void client.getCurrentBuildStatus().then(setLatestJob);
    }, 2500);
    return () => window.clearTimeout(timer);
  }, [latestJob]);

  function showMessage(text: string, isError = false) {
    setMessageIsError(isError);
    setMessage(text);
  }

  async function createWorkspace(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    showMessage("");
    try {
      await client.createWorkspace({ name: workspaceName, workProfile: workspaceProfile });
      setWorkspaceName("");
      setShowCreateWorkspace(false);
      showMessage("Workspace created and selected.");
      await refreshWorkspaces();
    } catch (error) {
      showMessage(error instanceof Error ? error.message : "Could not create workspace.", true);
    }
  }

  async function selectWorkspace(workspaceId: string) {
    if (!workspaceId) {
      return;
    }
    await client.setCurrentWorkspace(workspaceId);
    showMessage("Current workspace updated.");
    await refreshWorkspaces();
  }

  async function deleteWorkspace(workspaceId: string) {
    if (!workspaceId) {
      return;
    }
    await client.deleteWorkspace(workspaceId);
    showMessage("Workspace deleted.");
    await refreshWorkspaces();
  }

  async function saveGitAccess(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    showMessage("");
    if (!gitName.trim()) {
      showMessage("Credential name is required.", true);
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
      showMessage("Builder Git access credential saved.");
      await refreshSettings();
    } catch (error) {
      showMessage(error instanceof Error ? error.message : "Could not save Git access.", true);
    }
  }

  async function editGitCredential(name: string, host: string, username: string) {
    setGitName(name);
    setGitHost(host);
    setGitUsername(username);
    setGitToken("");
  }

  async function deleteGitCredential(name: string) {
    showMessage("");
    try {
      await client.deleteBuilderGitAccess(name);
      showMessage(`Deleted Git access credential ${name}.`);
      await refreshSettings();
    } catch (error) {
      showMessage(error instanceof Error ? error.message : "Could not delete Git access.", true);
    }
  }

  function downloadCatalog() {
    if (!catalog?.configured) {
      return;
    }
    const text = catalog.document || "{}\n";
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
    showMessage("");
    try {
      const text = await file.text();
      const contentType = file.name.toLowerCase().endsWith(".json")
        ? "application/json"
        : "application/yaml";
      await client.putBuilderCatalog(text, contentType);
      showMessage("Build catalog uploaded.");
      await refreshSettings();
    } catch (error) {
      showMessage(error instanceof Error ? error.message : "Could not upload build catalog.", true);
    }
  }

  async function submitBuild(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    showMessage("");
    try {
      const job = await client.submitBuild({ targetName: buildTarget, imageTag });
      setLatestJob(job);
      showMessage("Build submitted.");
      await refreshBuild();
    } catch (error) {
      showMessage(error instanceof Error ? error.message : "Could not submit build.", true);
    }
  }

  return (
    <PageFrame
      title="Builder"
      eyebrow=""
      description="Build submission, workspaces, and settings (Git access and build catalog)."
      pathname={builderTabPathname(props.pathname)}
      onNavigate={navigate}
      tabs={[
        { label: "Build", path: "/manage/builder" },
        { label: "Workspaces", path: "/manage/builder/workspaces" },
        { label: "Settings", path: "/manage/builder/settings" }
      ]}
    >
      {message ? (
        <div className={messageIsError ? "message message--error" : "message"}>{message}</div>
      ) : null}
      {isSettingsPath(props.pathname) ? (
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
                <button
                  className={catalog?.configured ? "button button--primary" : "button"}
                  type="button"
                  disabled={!catalog?.configured}
                  onClick={downloadCatalog}
                >
                  Download YAML
                </button>
                <button
                  className={
                    catalog?.canConfigure && !catalog?.configured
                      ? "button button--primary"
                      : "button"
                  }
                  type="button"
                  disabled={!catalog?.canConfigure}
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
            <Card title="Git credentials" subtitle="Name + server + username + token">
              <div className="detail-list">
                <div>
                  <span>Coverage</span>
                  <strong>{gitAccess?.configured ? "Complete" : "Incomplete"}</strong>
                </div>
                <div>
                  <span>Required servers</span>
                  <strong>{gitAccess?.requiredHosts?.join(", ") || "None advertised"}</strong>
                </div>
                <div>
                  <span>Covered servers</span>
                  <strong>{gitAccess?.coveredHosts?.join(", ") || "None"}</strong>
                </div>
                <div>
                  <span>Missing servers</span>
                  <strong>{gitAccess?.missingHosts?.join(", ") || "None"}</strong>
                </div>
              </div>
              <form className="stack-form" onSubmit={saveGitAccess}>
                <label className="field">
                  <span>Credential name</span>
                  <input
                    value={gitName}
                    placeholder="MyGitCredential"
                    onChange={(event) => setGitName(event.target.value)}
                  />
                </label>
                <label className="field">
                  <span>Git Server</span>
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
              {(gitAccess?.credentials || []).length === 0 ? (
                <EmptyState message="No credentials yet. Save a named credential for each catalog Git server." />
              ) : (
                <div className="stack-form">
                  {(gitAccess?.credentials || []).map((credential) => (
                    <div className="detail-list" key={credential.name}>
                      <div>
                        <span>Name</span>
                        <strong>{credential.name}</strong>
                      </div>
                      <div>
                        <span>Server</span>
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
        </div>
      ) : isWorkspacesPath(props.pathname) ? (
        <Card title="Workspaces" subtitle="Current workspace, create, and managed workspace list">
          <div className="stack-form">
            <div className="status-box">
              <strong>Current workspace</strong>
              <span>
                {currentWorkspace
                  ? `${currentWorkspace.name} · ${currentWorkspace.workProfile} · ${currentWorkspace.status}`
                  : "None selected"}
              </span>
            </div>
            <div className="button-row">
              <button
                className={showCreateWorkspace ? "button" : "button button--primary"}
                type="button"
                onClick={() => setShowCreateWorkspace((open) => !open)}
              >
                {showCreateWorkspace ? "Cancel" : "+ Create workspace"}
              </button>
            </div>
            {showCreateWorkspace ? (
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
            ) : null}
            {workspaces.length === 0 ? (
              <EmptyState message="No workspaces yet. Create one to start builds." />
            ) : (
              <div className="stack-form">
                {workspaces.map((workspace) => {
                  const isCurrent = currentWorkspace?.id === workspace.id;
                  return (
                    <div className="detail-list" key={workspace.id}>
                      <div>
                        <span>Name</span>
                        <strong>
                          {workspace.name}
                          {isCurrent ? " (current)" : ""}
                        </strong>
                      </div>
                      <div>
                        <span>Profile</span>
                        <strong>{workspace.workProfile}</strong>
                      </div>
                      <div>
                        <span>Status</span>
                        <strong>{workspace.status}</strong>
                      </div>
                      <div className="button-row">
                        <button
                          className="button"
                          type="button"
                          disabled={isCurrent}
                          onClick={() => void selectWorkspace(workspace.id)}
                        >
                          Set current
                        </button>
                        <button
                          className="button button--ghost"
                          type="button"
                          onClick={() => void deleteWorkspace(workspace.id)}
                        >
                          Delete
                        </button>
                      </div>
                    </div>
                  );
                })}
              </div>
            )}
          </div>
        </Card>
      ) : (
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
      )}
    </PageFrame>
  );
}
