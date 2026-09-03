import React, { FormEvent, useEffect, useMemo, useState } from "react";
import { Card, EmptyState, Icon, PageFrame, RowActionsMenu } from "../components";
import { ApiError } from "../client";
import { client } from "../lib/api";
import { navigate } from "../lib/navigate";
import type { ApplianceFileEntry, Session } from "../types";

function formatBytes(size: number): string {
  if (size < 1024) {
    return `${size} B`;
  }
  if (size < 1024 * 1024) {
    return `${(size / 1024).toFixed(1)} KiB`;
  }
  if (size < 1024 * 1024 * 1024) {
    return `${(size / (1024 * 1024)).toFixed(1)} MiB`;
  }
  return `${(size / (1024 * 1024 * 1024)).toFixed(2)} GiB`;
}

function formatModifiedAt(value: string): string {
  if (!value) {
    return "—";
  }
  const parsed = new Date(value);
  if (Number.isNaN(parsed.getTime())) {
    return value;
  }
  return parsed.toLocaleString(undefined, {
    year: "numeric",
    month: "short",
    day: "2-digit",
    hour: "2-digit",
    minute: "2-digit"
  });
}

function joinFilePath(prefix: string, name: string): string {
  const cleanPrefix = prefix.trim().replace(/^\/+|\/+$/g, "");
  const cleanName = name.trim().replace(/^\/+/, "");
  if (!cleanPrefix) {
    return cleanName;
  }
  return `${cleanPrefix}/${cleanName}`;
}

function parentPath(path: string): string {
  const parts = path.trim().replace(/^\/+|\/+$/g, "").split("/").filter(Boolean);
  parts.pop();
  return parts.join("/");
}

type FileShelf = {
  id: string;
  title: string;
  directoryPath: string;
  files: ApplianceFileEntry[];
};

function hasPermission(session: Pick<Session, "permissions">, permission: string): boolean {
  const wanted = permission.trim().toLowerCase();
  return session.permissions.some((entry) => entry.trim().toLowerCase() === wanted);
}

function canManageFiles(session: Pick<Session, "permissions">): boolean {
  return hasPermission(session, "files.write");
}

function canManageFocus(session: Pick<Session, "permissions">): boolean {
  return hasPermission(session, "focus.content.manage");
}

function fileTitle(entry: ApplianceFileEntry): string {
  return entry.name.replace(/\.[^.]+$/, "") || entry.name;
}

function previewKind(path: string): "pdf" | "text" | "image" | "other" {
  if (/\.pdf$/i.test(path)) return "pdf";
  if (/\.(txt|md|json|yaml|yml|log|csv|xml)$/i.test(path)) return "text";
  if (/\.(png|jpe?g|gif|webp|svg)$/i.test(path)) return "image";
  return "other";
}

function previewLabel(path: string): string {
  const kind = previewKind(path);
  if (kind === "pdf") return "PDF";
  if (kind === "text") return "TEXT";
  if (kind === "image") return "IMAGE";
  const ext = path.split(".").at(-1);
  return ext ? ext.toUpperCase() : "FILE";
}

function buildShelves(files: ApplianceFileEntry[]): FileShelf[] {
  const buckets = new Map<string, ApplianceFileEntry[]>();
  for (const file of files) {
    const directoryPath = parentPath(file.path);
    const list = buckets.get(directoryPath) ?? [];
    list.push(file);
    buckets.set(directoryPath, list);
  }
  const keys = [...buckets.keys()].sort((left, right) => {
    if (left === "") return -1;
    if (right === "") return 1;
    return left.localeCompare(right);
  });
  return keys.map((directoryPath) => ({
    id: directoryPath || "library",
    title: directoryPath || "Library",
    directoryPath,
    files: [...(buckets.get(directoryPath) ?? [])].sort((left, right) => left.name.localeCompare(right.name))
  }));
}

async function collectFileTree(path = ""): Promise<ApplianceFileEntry[]> {
  const result = await client.listApplianceFiles(path);
  const files: ApplianceFileEntry[] = [];
  for (const entry of result.items) {
    if (entry.type === "file") {
      files.push(entry);
      continue;
    }
    files.push(...(await collectFileTree(entry.path)));
  }
  return files;
}

export function FilesPage(props: { session: Session; capabilities: string[] }): React.JSX.Element {
  const canWrite = canManageFiles(props.session);
  const canSetFocus = props.capabilities.includes("focus-content") && canManageFocus(props.session);
  const [shelves, setShelves] = useState<FileShelf[]>([]);
  const [loading, setLoading] = useState(true);
  const [message, setMessage] = useState("");
  const [error, setError] = useState("");
  const [showUploadDialog, setShowUploadDialog] = useState(false);
  const [logicalName, setLogicalName] = useState("");
  const [selectedFile, setSelectedFile] = useState<File | null>(null);
  const [uploading, setUploading] = useState(false);
  const [viewing, setViewing] = useState<ApplianceFileEntry | null>(null);
  const [viewURL, setViewURL] = useState("");
  const [viewText, setViewText] = useState("");
  const [viewError, setViewError] = useState("");
  const [inspecting, setInspecting] = useState<ApplianceFileEntry | null>(null);

  const destinationPreview = useMemo(() => {
    const name = logicalName.trim().replace(/^\/+/, "");
    if (name) {
      return name;
    }
    if (selectedFile) {
      return selectedFile.name;
    }
    return "…";
  }, [logicalName, selectedFile]);

  const fileCount = shelves.reduce((count, shelf) => count + shelf.files.length, 0);

  async function refresh() {
    setLoading(true);
    setError("");
    try {
      const files = await collectFileTree("");
      setShelves(buildShelves(files));
    } catch (err) {
      setError(err instanceof ApiError ? err.detail : err instanceof Error ? err.message : "Could not list files.");
      setShelves([]);
    } finally {
      setLoading(false);
    }
  }

  useEffect(() => {
    void refresh();
  }, []);

  function openUploadDialog() {
    setLogicalName("");
    setSelectedFile(null);
    setError("");
    setShowUploadDialog(true);
  }

  function closeUploadDialog() {
    if (uploading) {
      return;
    }
    setShowUploadDialog(false);
  }

  async function submitUpload(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    if (!selectedFile) {
      setError("Choose a file to upload.");
      return;
    }
    const destination = logicalName.trim().replace(/^\/+|\/+$/g, "") || joinFilePath("", selectedFile.name);
    if (!destination) {
      setError("Enter a logical name / destination path.");
      return;
    }
    setUploading(true);
    setError("");
    try {
      const result = await client.uploadApplianceFile(destination, selectedFile);
      setMessage(
        result.overwritten
          ? `Updated ${result.path} (${formatBytes(result.size)}).`
          : `Uploaded ${result.path} (${formatBytes(result.size)}).`
      );
      setShowUploadDialog(false);
      await refresh();
    } catch (err) {
      setError(err instanceof ApiError ? err.detail : err instanceof Error ? err.message : "Upload failed.");
    } finally {
      setUploading(false);
    }
  }

  async function deleteEntry(entry: ApplianceFileEntry) {
    if (!window.confirm(`Delete file “${entry.name}”? This cannot be undone.`)) {
      return;
    }
    setError("");
    try {
      await client.deleteApplianceFile(entry.path);
      setMessage(`Deleted ${entry.path}.`);
      await refresh();
    } catch (err) {
      setError(err instanceof ApiError ? err.detail : err instanceof Error ? err.message : "Delete failed.");
    }
  }

  async function openEntry(entry: ApplianceFileEntry) {
    setViewError("");
    setViewText("");
    setViewURL("");
    setViewing(entry);
    try {
      const blob = await client.downloadApplianceFile(entry.path);
      if (previewKind(entry.path) === "text") {
        setViewText(await blob.text());
        return;
      }
      setViewURL(URL.createObjectURL(blob));
    } catch (err) {
      setViewError(err instanceof ApiError ? err.detail : err instanceof Error ? err.message : "Could not open file.");
    }
  }

  useEffect(() => {
    return () => {
      if (viewURL) {
        URL.revokeObjectURL(viewURL);
      }
    };
  }, [viewURL]);

  async function setCurrentFocus(entry: ApplianceFileEntry) {
    try {
      await client.putFocusContent({
        resourceType: "file",
        resourcePath: entry.path,
        title: fileTitle(entry)
      });
      setMessage(`${fileTitle(entry)} is now the current focus.`);
    } catch (err) {
      setError(err instanceof ApiError ? err.detail : err instanceof Error ? err.message : "Could not set current focus.");
    }
  }

  return (
    <PageFrame
      title="Files"
      eyebrow=""
      description="Manage uploaded documents and set focus content for viewers."
      pathname="/manage/files"
      onNavigate={navigate}
      tabs={[{ label: "Library", path: "/manage/files" }]}
    >
      {message ? <div className="message">{message}</div> : null}
      {error ? <div className="message message--error">{error}</div> : null}

      <Card title="Document library" subtitle="Preview uploaded files and publish one as current focus">
        <div className="video-library">
          <section className="video-library__stage">
            <div className="video-library__toolbar">
              {canWrite ? (
                <button className="button button--primary" type="button" onClick={openUploadDialog}>
                  Upload file
                </button>
              ) : null}
              <button className="button button--ghost" type="button" onClick={() => void refresh()} disabled={loading}>
                Refresh
              </button>
              <p className="video-library__count">{loading ? "Loading library…" : `${fileCount} file${fileCount === 1 ? "" : "s"}`}</p>
            </div>

            {loading ? (
              <EmptyState message="Loading file library…" />
            ) : fileCount === 0 ? (
              <EmptyState message="No files here yet. Upload PDF or text documents to begin." />
            ) : (
              shelves.map((shelf) => (
                <section className="video-shelf" key={shelf.id} aria-label={shelf.title}>
                  <div className="video-shelf__header">
                    <h2>{shelf.title}</h2>
                    {shelf.directoryPath ? <span className="pill pill--navy">{shelf.directoryPath}</span> : null}
                  </div>
                  <div className="file-shelf__row">
                    {shelf.files.map((entry) => (
                      <article key={entry.path} className="file-card">
                        <button
                          type="button"
                          className="file-card__hit"
                          onClick={() => void openEntry(entry)}
                          aria-label={`Open ${entry.name}`}
                        >
                          <div className="file-card__preview">
                            <Icon name="files" className="h-8 w-8" />
                            <span className="file-card__badge">{previewLabel(entry.path)}</span>
                          </div>
                          <div className="file-card__meta">
                            <strong className="file-card__title">{entry.name}</strong>
                            <span>{formatBytes(entry.sizeBytes)}</span>
                            <span>{formatModifiedAt(entry.modifiedAt)}</span>
                          </div>
                        </button>
                        <div className="file-card__menu">
                          <RowActionsMenu
                            label={`Actions for ${entry.name}`}
                            actions={[
                              { id: "open", label: "View", onSelect: () => void openEntry(entry) },
                              { id: "details", label: "View details", onSelect: () => setInspecting(entry) },
                              ...(canSetFocus
                                ? [{ id: "focus", label: "Set as current focus", onSelect: () => void setCurrentFocus(entry) }]
                                : []),
                              ...(canWrite
                                ? [{ id: "delete", label: "Delete", danger: true, onSelect: () => void deleteEntry(entry) }]
                                : [])
                            ]}
                          />
                        </div>
                      </article>
                    ))}
                  </div>
                </section>
              ))
            )}
          </section>
        </div>
      </Card>

      {showUploadDialog ? (
        <div
          className="fixed inset-0 z-[120] flex items-center justify-center bg-slate-950/45 p-4"
          role="presentation"
          onClick={closeUploadDialog}
        >
          <div
            className="w-full max-w-lg rounded-[1.75rem] border border-slate-200 bg-white p-6 shadow-2xl shadow-slate-900/25"
            role="dialog"
            aria-modal="true"
            aria-labelledby="files-upload-dialog-title"
            onClick={(event) => event.stopPropagation()}
          >
            <h2
              id="files-upload-dialog-title"
              className="m-0 text-xl font-bold tracking-tight text-slate-950"
            >
              Add file
            </h2>
            <p className="mt-2 mb-4 text-sm text-slate-500">
              Give the upload a logical name (relative path). Other features can refer to that name later —
              for example <code>models-staging/qwen-pack.tar.zst</code>.
            </p>
            <form className="stack-form" onSubmit={submitUpload}>
              <label className="field">
                <span>Logical name / path</span>
                <input
                  value={logicalName}
                  placeholder="models-staging/my-model.tar.zst"
                  onChange={(event) => setLogicalName(event.target.value)}
                  disabled={uploading}
                />
              </label>
              <label className="field">
                <span>File from this computer</span>
                <input
                  type="file"
                  onChange={(event) => setSelectedFile(event.target.files?.[0] ?? null)}
                  disabled={uploading}
                />
              </label>
              <p className="text-sm text-slate-500" style={{ margin: 0 }}>
                Stored as <strong>{destinationPreview}</strong> under the appliance files store.
              </p>
              <div className="button-row">
                <button className="button button--ghost" type="button" onClick={closeUploadDialog} disabled={uploading}>
                  Cancel
                </button>
                <button className="button button--primary" type="submit" disabled={uploading || !selectedFile}>
                  {uploading ? "Uploading…" : "Upload"}
                </button>
              </div>
            </form>
          </div>
        </div>
      ) : null}

      {viewing ? (
        <div className="video-modal-scrim" role="presentation" onClick={() => setViewing(null)}>
          <div className="video-modal" role="dialog" aria-modal="true" aria-labelledby="files-preview-dialog-title" onClick={(event) => event.stopPropagation()}>
            <h2 id="files-preview-dialog-title">{fileTitle(viewing)}</h2>
            <p>{viewing.path}</p>
            {viewError ? <EmptyState message={viewError} /> : null}
            {!viewError && viewText ? <pre className="focus-file focus-file--text">{viewText}</pre> : null}
            {!viewError && !viewText && previewKind(viewing.path) === "pdf" && viewURL ? (
              <iframe className="focus-file focus-file--pdf" src={viewURL} title={viewing.name} />
            ) : null}
            {!viewError && !viewText && previewKind(viewing.path) === "image" && viewURL ? (
              <img className="focus-file focus-file--image" src={viewURL} alt={viewing.name} />
            ) : null}
            {!viewError && !viewText && previewKind(viewing.path) === "other" && viewURL ? (
              <a className="button button--primary" href={viewURL} target="_blank" rel="noreferrer">
                Open file
              </a>
            ) : null}
            <div className="button-row">
              <button className="button button--ghost" type="button" onClick={() => setViewing(null)}>
                Close
              </button>
            </div>
          </div>
        </div>
      ) : null}

      {inspecting ? (
        <div className="video-modal-scrim" role="presentation" onClick={() => setInspecting(null)}>
          <div className="video-modal" role="dialog" aria-modal="true" aria-labelledby="files-details-dialog-title" onClick={(event) => event.stopPropagation()}>
            <h2 id="files-details-dialog-title">{fileTitle(inspecting)}</h2>
            <dl className="video-details">
              <div className="video-details__row">
                <dt>File name</dt>
                <dd>{inspecting.name}</dd>
              </div>
              <div className="video-details__row">
                <dt>Library path</dt>
                <dd>{inspecting.path}</dd>
              </div>
              <div className="video-details__row">
                <dt>Type</dt>
                <dd>{previewLabel(inspecting.path)}</dd>
              </div>
              <div className="video-details__row">
                <dt>Size</dt>
                <dd>{formatBytes(inspecting.sizeBytes)}</dd>
              </div>
              <div className="video-details__row">
                <dt>Modified</dt>
                <dd>{formatModifiedAt(inspecting.modifiedAt)}</dd>
              </div>
            </dl>
            <div className="button-row">
              <button className="button button--ghost" type="button" onClick={() => setInspecting(null)}>
                Close
              </button>
              {canSetFocus ? (
                <button className="button button--primary" type="button" onClick={() => void setCurrentFocus(inspecting)}>
                  Set as current focus
                </button>
              ) : null}
              {canWrite ? (
                <button className="button button--danger" type="button" onClick={() => void deleteEntry(inspecting)}>
                  Delete
                </button>
              ) : null}
            </div>
          </div>
        </div>
      ) : null}
    </PageFrame>
  );
}
