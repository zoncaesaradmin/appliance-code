import React, { FormEvent, useEffect, useRef, useState } from "react";
import { Card, EmptyState, PageFrame, ResourceList, ResourceListRow } from "../components";
import { client } from "../lib/api";
import { formatTimestamp } from "../lib/format";
import { navigate } from "../lib/navigate";
import type {
  BuilderCatalogStatus,
  BuilderGitAccessStatus,
  BuildTarget,
  Job,
  JobStep,
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

function formatSubmissionId(jobId: string): string {
  const trimmed = jobId.trim();
  if (trimmed.length <= 12) {
    return trimmed;
  }
  return `${trimmed.slice(0, 8)}…${trimmed.slice(-4)}`;
}

export function BuilderPage(props: { pathname: string }): React.JSX.Element {
  const [profiles, setProfiles] = useState<WorkProfile[]>([]);
  const [workspaces, setWorkspaces] = useState<Workspace[]>([]);
  const [currentWorkspace, setCurrentWorkspace] = useState<Workspace | null>(null);
  const [catalog, setCatalog] = useState<BuilderCatalogStatus | null>(null);
  const [gitAccess, setGitAccess] = useState<BuilderGitAccessStatus | null>(null);
  const [targets, setTargets] = useState<BuildTarget[]>([]);
  const [buildJobs, setBuildJobs] = useState<Job[]>([]);
  const [selectedBuildDetail, setSelectedBuildDetail] = useState<Job | null>(null);
  const [selectedBuildSteps, setSelectedBuildSteps] = useState<JobStep[]>([]);
  const [buildDetailLoading, setBuildDetailLoading] = useState(false);
  const [message, setMessage] = useState("");
  const [messageIsError, setMessageIsError] = useState(false);
  const messageTimerRef = useRef<number | null>(null);
  const [workspaceName, setWorkspaceName] = useState("");
  const [workspaceProfile, setWorkspaceProfile] = useState("");
  const [gitName, setGitName] = useState("");
  const [gitHost, setGitHost] = useState("");
  const [gitUsername, setGitUsername] = useState("");
  const [gitToken, setGitToken] = useState("");
  const [buildTarget, setBuildTarget] = useState("");
  const [imageTag, setImageTag] = useState("");
  const [showCreateWorkspace, setShowCreateWorkspace] = useState(false);
  const [showSubmitBuildDialog, setShowSubmitBuildDialog] = useState(false);
  const [showGitCredentialDialog, setShowGitCredentialDialog] = useState(false);
  const [gitCredentialDialogMode, setGitCredentialDialogMode] = useState<"add" | "edit">("add");
  const [catalogDialogMode, setCatalogDialogMode] = useState<"manage" | "upload" | null>(null);
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
    const [nextCurrent, nextTargets, nextJobs] = await Promise.all([
      client.getCurrentWorkspace(),
      client.listBuildTargets().catch(() => [] as BuildTarget[]),
      client.listJobs()
    ]);
    setCurrentWorkspace(nextCurrent);
    setTargets(nextTargets);
    setBuildJobs(nextJobs.filter((job) => job.type === "build"));
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
    clearMessage();
    void refreshForPath(props.pathname);
  }, [props.pathname]);

  useEffect(() => {
    return () => {
      if (messageTimerRef.current !== null) {
        window.clearTimeout(messageTimerRef.current);
      }
    };
  }, []);

  useEffect(() => {
    if (
      !showGitCredentialDialog &&
      !showCreateWorkspace &&
      !showSubmitBuildDialog &&
      !selectedBuildDetail &&
      !buildDetailLoading &&
      catalogDialogMode === null
    ) {
      return;
    }
    const onKeyDown = (event: KeyboardEvent) => {
      if (event.key === "Escape") {
        closeGitCredentialDialog();
        closeWorkspaceDialog();
        closeSubmitBuildDialog();
        closeBuildDetailDialog();
        setCatalogDialogMode(null);
      }
    };
    window.addEventListener("keydown", onKeyDown);
    return () => window.removeEventListener("keydown", onKeyDown);
  }, [
    showGitCredentialDialog,
    showCreateWorkspace,
    showSubmitBuildDialog,
    selectedBuildDetail,
    buildDetailLoading,
    catalogDialogMode
  ]);

  useEffect(() => {
    if (!buildJobs.some((job) => job.status === "running")) {
      return;
    }
    const timer = window.setTimeout(() => {
      void client.listJobs().then((jobs) => setBuildJobs(jobs.filter((job) => job.type === "build")));
    }, 2500);
    return () => window.clearTimeout(timer);
  }, [buildJobs]);

  function clearMessage() {
    if (messageTimerRef.current !== null) {
      window.clearTimeout(messageTimerRef.current);
      messageTimerRef.current = null;
    }
    setMessage("");
    setMessageIsError(false);
  }

  function showMessage(text: string, isError = false) {
    if (messageTimerRef.current !== null) {
      window.clearTimeout(messageTimerRef.current);
      messageTimerRef.current = null;
    }
    setMessageIsError(isError);
    setMessage(text);
    if (!text) {
      return;
    }
    // Success banners dismiss sooner; errors stay a bit longer but still expire.
    const dismissMs = isError ? 8000 : 4000;
    messageTimerRef.current = window.setTimeout(() => {
      messageTimerRef.current = null;
      setMessage("");
      setMessageIsError(false);
    }, dismissMs);
  }

  async function createWorkspace(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    showMessage("");
    try {
      await client.createWorkspace({ name: workspaceName, workProfile: workspaceProfile });
      closeWorkspaceDialog();
      showMessage("Workspace created and selected.");
      await refreshWorkspaces();
    } catch (error) {
      showMessage(error instanceof Error ? error.message : "Could not create workspace.", true);
    }
  }

  function openCreateWorkspace() {
    setWorkspaceName("");
    setWorkspaceProfile(profiles[0]?.name || workspaceProfile);
    setShowCreateWorkspace(true);
  }

  function closeWorkspaceDialog() {
    setShowCreateWorkspace(false);
    setWorkspaceName("");
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

  function openAddGitCredential() {
    setGitCredentialDialogMode("add");
    setGitName("");
    setGitHost("");
    setGitUsername("");
    setGitToken("");
    setShowGitCredentialDialog(true);
  }

  function closeGitCredentialDialog() {
    setShowGitCredentialDialog(false);
    setGitCredentialDialogMode("add");
    setGitName("");
    setGitHost("");
    setGitUsername("");
    setGitToken("");
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
      closeGitCredentialDialog();
      showMessage("Builder Git access credential saved.");
      await refreshSettings();
    } catch (error) {
      showMessage(error instanceof Error ? error.message : "Could not save Git access.", true);
    }
  }

  function editGitCredential(name: string, host: string, username: string) {
    setGitCredentialDialogMode("edit");
    setGitName(name);
    setGitHost(host);
    setGitUsername(username);
    setGitToken("");
    setShowGitCredentialDialog(true);
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
      setCatalogDialogMode(null);
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
      await client.submitBuild({ targetName: buildTarget, imageTag });
      closeSubmitBuildDialog();
      showMessage("Build submitted.");
      await refreshBuild();
    } catch (error) {
      showMessage(error instanceof Error ? error.message : "Could not submit build.", true);
    }
  }

  function openSubmitBuildDialog() {
    setBuildTarget(targets[0]?.name || "");
    setImageTag("");
    setShowSubmitBuildDialog(true);
  }

  function closeSubmitBuildDialog() {
    setShowSubmitBuildDialog(false);
    setBuildTarget("");
    setImageTag("");
  }

  async function openBuildDetailDialog(jobId: string) {
    showMessage("");
    setBuildDetailLoading(true);
    setSelectedBuildDetail(buildJobs.find((job) => job.id === jobId) || null);
    setSelectedBuildSteps([]);
    try {
      const [job, steps] = await Promise.all([client.getJob(jobId), client.listJobSteps(jobId)]);
      setSelectedBuildDetail(job);
      setSelectedBuildSteps(steps);
    } catch (error) {
      showMessage(error instanceof Error ? error.message : "Could not load build details.", true);
      setSelectedBuildDetail(null);
      setSelectedBuildSteps([]);
    } finally {
      setBuildDetailLoading(false);
    }
  }

  function closeBuildDetailDialog() {
    setSelectedBuildDetail(null);
    setSelectedBuildSteps([]);
    setBuildDetailLoading(false);
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
              {catalog?.configured ? (
                <button
                  type="button"
                  className="catalog-status-tile catalog-status-tile--ready"
                  onClick={() => setCatalogDialogMode("manage")}
                >
                  <span className="catalog-status-tile__badge">Added</span>
                  <span className="catalog-status-tile__title">Catalog is configured</span>
                  <span className="catalog-status-tile__meta">
                    Updated {catalog.updatedAt ? formatTimestamp(catalog.updatedAt) : "unknown"}
                  </span>
                  <span className="catalog-status-tile__action">Manage</span>
                </button>
              ) : (
                <div className="catalog-status-tile catalog-status-tile--missing">
                  <span className="catalog-status-tile__badge">Needed</span>
                  <span className="catalog-status-tile__title">No catalog yet</span>
                  <span className="catalog-status-tile__meta">
                    Add a catalog before creating workspaces.
                  </span>
                </div>
              )}
              {!catalog?.configured ? (
                <EmptyState message="Upload an appliance-native build-catalog.yaml to get started." />
              ) : null}
              <div className="button-row">
                <button
                  className="button button--primary"
                  type="button"
                  disabled={catalog?.canConfigure === false}
                  onClick={() => setCatalogDialogMode("upload")}
                >
                  + Add catalog
                </button>
              </div>
              <input
                ref={catalogFileRef}
                type="file"
                accept=".yaml,.yml,.json,application/yaml,text/yaml,application/json"
                hidden
                onChange={(event) => void uploadCatalog(event)}
              />
            </Card>
            <Card title="Git credentials" subtitle="Named HTTPS credentials for catalog Git servers">
              <div className="status-box">
                <strong>Configured credentials</strong>
                <span>{(gitAccess?.credentials || []).length}</span>
              </div>
              {(gitAccess?.credentials || []).length === 0 ? (
                <EmptyState message="No credentials yet. Add one for each catalog Git server." />
              ) : (
                <ResourceList>
                  {(gitAccess?.credentials || []).map((credential) => (
                    <ResourceListRow
                      key={credential.name}
                      actionsLabel={`Actions for ${credential.username} on ${credential.host}`}
                      columns={[
                        { key: "username", label: "Username", value: credential.username },
                        { key: "server", label: "Server", value: credential.host }
                      ]}
                      actions={[
                        {
                          id: "edit",
                          label: "Edit",
                          onSelect: () =>
                            editGitCredential(credential.name, credential.host, credential.username)
                        },
                        {
                          id: "delete",
                          label: "Delete",
                          danger: true,
                          onSelect: () => void deleteGitCredential(credential.name)
                        }
                      ]}
                    />
                  ))}
                </ResourceList>
              )}
              <div className="button-row">
                <button
                  className="button button--primary"
                  type="button"
                  disabled={gitAccess?.canConfigure === false}
                  onClick={openAddGitCredential}
                >
                  + Add credential
                </button>
              </div>
            </Card>
          </div>
          {showGitCredentialDialog ? (
            <div
              className="fixed inset-0 z-[120] flex items-center justify-center bg-slate-950/45 p-4"
              role="presentation"
              onClick={closeGitCredentialDialog}
            >
              <div
                className="w-full max-w-lg rounded-[1.75rem] border border-slate-200 bg-white p-6 shadow-2xl shadow-slate-900/25"
                role="dialog"
                aria-modal="true"
                aria-labelledby="git-credential-dialog-title"
                onClick={(event) => event.stopPropagation()}
              >
                <h2
                  id="git-credential-dialog-title"
                  className="m-0 text-xl font-bold tracking-tight text-slate-950"
                >
                  {gitCredentialDialogMode === "edit" ? "Edit Git credential" : "Add Git credential"}
                </h2>
                <p className="mt-2 mb-4 text-sm text-slate-500">
                  Name + server + username + token
                </p>
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
                    <input
                      value={gitUsername}
                      onChange={(event) => setGitUsername(event.target.value)}
                    />
                  </label>
                  <label className="field">
                    <span>Git token</span>
                    <input
                      type="password"
                      value={gitToken}
                      onChange={(event) => setGitToken(event.target.value)}
                    />
                  </label>
                  <div className="button-row">
                    <button className="button button--ghost" type="button" onClick={closeGitCredentialDialog}>
                      Cancel
                    </button>
                    <button className="button button--primary" type="submit">
                      Save credential
                    </button>
                  </div>
                </form>
              </div>
            </div>
          ) : null}
          {catalogDialogMode === "manage" ? (
            <div
              className="fixed inset-0 z-[120] flex items-center justify-center bg-slate-950/45 p-4"
              role="presentation"
              onClick={() => setCatalogDialogMode(null)}
            >
              <div
                className="w-full max-w-lg rounded-[1.75rem] border border-slate-200 bg-white p-6 shadow-2xl shadow-slate-900/25"
                role="dialog"
                aria-modal="true"
                aria-labelledby="catalog-manage-dialog-title"
                onClick={(event) => event.stopPropagation()}
              >
                <h2
                  id="catalog-manage-dialog-title"
                  className="m-0 text-xl font-bold tracking-tight text-slate-950"
                >
                  Build catalog
                </h2>
                <p className="mt-2 mb-5 text-sm text-slate-500">
                  Download the current document, or add a new file to replace it.
                </p>
                <div className="catalog-manage-actions">
                  <button
                    className="button button--primary"
                    type="button"
                    onClick={() => {
                      downloadCatalog();
                      setCatalogDialogMode(null);
                    }}
                  >
                    Download YAML
                  </button>
                  <button
                    className="button"
                    type="button"
                    disabled={catalog?.canConfigure === false}
                    onClick={() => setCatalogDialogMode("upload")}
                  >
                    Add catalog
                  </button>
                  <button
                    className="button button--ghost"
                    type="button"
                    onClick={() => setCatalogDialogMode(null)}
                  >
                    Close
                  </button>
                </div>
              </div>
            </div>
          ) : null}
          {catalogDialogMode === "upload" ? (
            <div
              className="fixed inset-0 z-[120] flex items-center justify-center bg-slate-950/45 p-4"
              role="presentation"
              onClick={() => setCatalogDialogMode(null)}
            >
              <div
                className="w-full max-w-lg rounded-[1.75rem] border border-slate-200 bg-white p-6 shadow-2xl shadow-slate-900/25"
                role="dialog"
                aria-modal="true"
                aria-labelledby="catalog-upload-dialog-title"
                onClick={(event) => event.stopPropagation()}
              >
                <h2
                  id="catalog-upload-dialog-title"
                  className="m-0 text-xl font-bold tracking-tight text-slate-950"
                >
                  Add catalog
                </h2>
                <p className="mt-2 mb-4 text-sm text-slate-500">
                  {catalog?.configured
                    ? "Uploading a file replaces the current catalog."
                    : "Choose an appliance-native build-catalog.yaml or JSON file."}
                </p>
                <div className="button-row">
                  <button
                    className="button button--ghost"
                    type="button"
                    onClick={() => setCatalogDialogMode(null)}
                  >
                    Cancel
                  </button>
                  <button
                    className="button button--primary"
                    type="button"
                    onClick={() => catalogFileRef.current?.click()}
                  >
                    Choose file
                  </button>
                </div>
              </div>
            </div>
          ) : null}
        </div>
      ) : isWorkspacesPath(props.pathname) ? (
        <>
          <Card title="Workspaces" subtitle="Named workspaces for catalog builds">
            <div className="status-box">
              <strong>Configured workspaces</strong>
              <span>{workspaces.length}</span>
            </div>
            {workspaces.length === 0 ? (
              <EmptyState message="No workspaces yet. Create one to start builds." />
            ) : (
              <ResourceList>
                {workspaces.map((workspace) => {
                  const isCurrent = currentWorkspace?.id === workspace.id;
                  return (
                    <ResourceListRow
                      key={workspace.id}
                      actionsLabel={`Actions for workspace ${workspace.name}`}
                      columns={[
                        {
                          key: "name",
                          label: "Workspace",
                          value: isCurrent ? `${workspace.name} (current)` : workspace.name
                        },
                        { key: "profile", label: "Profile", value: workspace.workProfile }
                      ]}
                      actions={[
                        {
                          id: "set-current",
                          label: "Set current",
                          disabled: isCurrent,
                          onSelect: () => void selectWorkspace(workspace.id)
                        },
                        {
                          id: "delete",
                          label: "Delete",
                          danger: true,
                          onSelect: () => void deleteWorkspace(workspace.id)
                        }
                      ]}
                    />
                  );
                })}
              </ResourceList>
            )}
            <div className="button-row">
              <button className="button button--primary" type="button" onClick={openCreateWorkspace}>
                + Create workspace
              </button>
            </div>
          </Card>
          {showCreateWorkspace ? (
            <div
              className="fixed inset-0 z-[120] flex items-center justify-center bg-slate-950/45 p-4"
              role="presentation"
              onClick={closeWorkspaceDialog}
            >
              <div
                className="w-full max-w-lg rounded-[1.75rem] border border-slate-200 bg-white p-6 shadow-2xl shadow-slate-900/25"
                role="dialog"
                aria-modal="true"
                aria-labelledby="workspace-dialog-title"
                onClick={(event) => event.stopPropagation()}
              >
                <h2
                  id="workspace-dialog-title"
                  className="m-0 text-xl font-bold tracking-tight text-slate-950"
                >
                  Create workspace
                </h2>
                <p className="mt-2 mb-4 text-sm text-slate-500">
                  Name + work profile. The new workspace becomes current.
                </p>
                <form className="stack-form" onSubmit={createWorkspace}>
                  <label className="field">
                    <span>Workspace name</span>
                    <input
                      value={workspaceName}
                      onChange={(event) => setWorkspaceName(event.target.value)}
                      required
                    />
                  </label>
                  <label className="field">
                    <span>Workspace profile</span>
                    <select
                      value={workspaceProfile}
                      onChange={(event) => setWorkspaceProfile(event.target.value)}
                      required
                    >
                      {profiles.map((profile) => (
                        <option key={profile.name} value={profile.name}>
                          {profile.name}
                        </option>
                      ))}
                    </select>
                  </label>
                  <div className="button-row">
                    <button className="button button--ghost" type="button" onClick={closeWorkspaceDialog}>
                      Cancel
                    </button>
                    <button className="button button--primary" type="submit">
                      Create workspace
                    </button>
                  </div>
                </form>
              </div>
            </div>
          ) : null}
        </>
      ) : (
        <>
          <Card title="Builds" subtitle="Submitted builds for this appliance">
            <div className="status-box">
              <strong>Submitted builds</strong>
              <span>{buildJobs.length}</span>
            </div>
            {buildJobs.length === 0 ? (
              <EmptyState message="No builds yet. Submit one for the current workspace." />
            ) : (
              <ResourceList>
                {buildJobs.map((job) => (
                  <ResourceListRow
                    key={job.id}
                    ariaLabel={`Open details for submission ${job.id}`}
                    onClick={() => void openBuildDetailDialog(job.id)}
                    columns={[
                      {
                        key: "submission",
                        label: "Submission ID",
                        value: formatSubmissionId(job.id)
                      },
                      { key: "target", label: "Target", value: job.targetName || "Unknown" },
                      { key: "status", label: "Status", value: job.status },
                      {
                        key: "submitted",
                        label: "Submitted",
                        value: formatTimestamp(job.createdAt)
                      },
                      {
                        key: "completed",
                        label: "Completed",
                        value: job.completedAt ? formatTimestamp(job.completedAt) : "—"
                      }
                    ]}
                  />
                ))}
              </ResourceList>
            )}
            <div className="button-row">
              <button
                className="button button--primary"
                type="button"
                disabled={!currentWorkspace}
                onClick={openSubmitBuildDialog}
              >
                + Submit build
              </button>
            </div>
            {!currentWorkspace ? (
              <p className="message" style={{ marginTop: "0.75rem" }}>
                Select or create a current workspace before submitting a build.
              </p>
            ) : null}
          </Card>
          {showSubmitBuildDialog ? (
            <div
              className="fixed inset-0 z-[120] flex items-center justify-center bg-slate-950/45 p-4"
              role="presentation"
              onClick={closeSubmitBuildDialog}
            >
              <div
                className="w-full max-w-lg rounded-[1.75rem] border border-slate-200 bg-white p-6 shadow-2xl shadow-slate-900/25"
                role="dialog"
                aria-modal="true"
                aria-labelledby="submit-build-dialog-title"
                onClick={(event) => event.stopPropagation()}
              >
                <h2
                  id="submit-build-dialog-title"
                  className="m-0 text-xl font-bold tracking-tight text-slate-950"
                >
                  Submit build
                </h2>
                <p className="mt-2 mb-4 text-sm text-slate-500">
                  Target + optional image tag for the current workspace.
                </p>
                <form className="stack-form" onSubmit={submitBuild}>
                  <label className="field">
                    <span>Current workspace</span>
                    <input value={currentWorkspace?.name || "No current workspace"} disabled />
                  </label>
                  <label className="field">
                    <span>Target</span>
                    <select
                      value={buildTarget}
                      onChange={(event) => setBuildTarget(event.target.value)}
                      required
                    >
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
                  <div className="button-row">
                    <button className="button button--ghost" type="button" onClick={closeSubmitBuildDialog}>
                      Cancel
                    </button>
                    <button className="button button--primary" type="submit" disabled={!buildTarget}>
                      Submit build
                    </button>
                  </div>
                </form>
              </div>
            </div>
          ) : null}
          {selectedBuildDetail || buildDetailLoading ? (
            <div
              className="fixed inset-0 z-[120] flex items-center justify-center bg-slate-950/45 p-4"
              role="presentation"
              onClick={closeBuildDetailDialog}
            >
              <div
                className="w-full max-w-2xl rounded-[1.75rem] border border-slate-200 bg-white p-6 shadow-2xl shadow-slate-900/25"
                role="dialog"
                aria-modal="true"
                aria-labelledby="build-detail-dialog-title"
                onClick={(event) => event.stopPropagation()}
              >
                <h2
                  id="build-detail-dialog-title"
                  className="m-0 text-xl font-bold tracking-tight text-slate-950"
                >
                  Build submission details
                </h2>
                <p className="mt-2 mb-4 text-sm text-slate-500">
                  Full job record for this submission.
                </p>
                {selectedBuildDetail ? (
                  <div className="detail-list">
                    <div>
                      <span>Submission ID</span>
                      <strong className="break-all">{selectedBuildDetail.id}</strong>
                    </div>
                    <div>
                      <span>Status</span>
                      <strong>{selectedBuildDetail.status}</strong>
                    </div>
                    <div>
                      <span>Target</span>
                      <strong>{selectedBuildDetail.targetName || "Unknown"}</strong>
                    </div>
                    <div>
                      <span>Artifact</span>
                      <strong>{selectedBuildDetail.artifactRef || "Not available"}</strong>
                    </div>
                    <div>
                      <span>Workspace ID</span>
                      <strong className="break-all">{selectedBuildDetail.workspaceId || "—"}</strong>
                    </div>
                    <div>
                      <span>Build ID</span>
                      <strong className="break-all">{selectedBuildDetail.buildId || "—"}</strong>
                    </div>
                    <div>
                      <span>Submitted</span>
                      <strong>{formatTimestamp(selectedBuildDetail.createdAt)}</strong>
                    </div>
                    <div>
                      <span>Started</span>
                      <strong>{formatTimestamp(selectedBuildDetail.startedAt)}</strong>
                    </div>
                    <div>
                      <span>Completed</span>
                      <strong>{formatTimestamp(selectedBuildDetail.completedAt)}</strong>
                    </div>
                    <div>
                      <span>Updated</span>
                      <strong>{formatTimestamp(selectedBuildDetail.updatedAt)}</strong>
                    </div>
                    {selectedBuildDetail.reasonCode ? (
                      <div>
                        <span>Reason</span>
                        <strong>{selectedBuildDetail.reasonCode}</strong>
                      </div>
                    ) : null}
                    {selectedBuildDetail.errorMessage ? (
                      <div>
                        <span>Error</span>
                        <strong>{selectedBuildDetail.errorMessage}</strong>
                      </div>
                    ) : null}
                  </div>
                ) : (
                  <EmptyState message="Loading build details..." />
                )}
                <div className="mt-5">
                  <h3 className="m-0 mb-2 text-base font-bold text-slate-950">Steps</h3>
                  {buildDetailLoading && selectedBuildSteps.length === 0 ? (
                    <p className="m-0 text-sm text-slate-500">Loading steps...</p>
                  ) : selectedBuildSteps.length === 0 ? (
                    <EmptyState message="No steps recorded for this submission." />
                  ) : (
                    <ResourceList>
                      {selectedBuildSteps.map((step) => (
                        <ResourceListRow
                          key={step.id}
                          columns={[
                            { key: "name", label: "Step", value: step.name },
                            { key: "status", label: "Status", value: step.status },
                            {
                              key: "started",
                              label: "Started",
                              value: formatTimestamp(step.startedAt)
                            },
                            {
                              key: "completed",
                              label: "Completed",
                              value: formatTimestamp(step.completedAt)
                            }
                          ]}
                        />
                      ))}
                    </ResourceList>
                  )}
                </div>
                <div className="button-row" style={{ marginTop: "1.25rem" }}>
                  <button className="button button--primary" type="button" onClick={closeBuildDetailDialog}>
                    Close
                  </button>
                </div>
              </div>
            </div>
          ) : null}
        </>
      )}
    </PageFrame>
  );
}
