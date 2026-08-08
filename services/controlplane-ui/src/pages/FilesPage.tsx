import React, { FormEvent, useEffect, useMemo, useState } from "react";
import { Card, EmptyState, PageFrame } from "../components";
import { ApiError } from "../client";
import { client } from "../lib/api";
import { navigate } from "../lib/navigate";
import type { ApplianceFileEntry } from "../types";

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

export function FilesPage(): React.JSX.Element {
  const [currentPath, setCurrentPath] = useState("");
  const [items, setItems] = useState<ApplianceFileEntry[]>([]);
  const [loading, setLoading] = useState(true);
  const [message, setMessage] = useState("");
  const [error, setError] = useState("");
  const [showUploadDialog, setShowUploadDialog] = useState(false);
  const [logicalName, setLogicalName] = useState("");
  const [selectedFile, setSelectedFile] = useState<File | null>(null);
  const [uploading, setUploading] = useState(false);

  const breadcrumbs = useMemo(() => {
    const parts = currentPath.split("/").filter(Boolean);
    const crumbs = [{ label: "root", path: "" }];
    let walking = "";
    for (const part of parts) {
      walking = walking ? `${walking}/${part}` : part;
      crumbs.push({ label: part, path: walking });
    }
    return crumbs;
  }, [currentPath]);

  const destinationPreview = useMemo(() => {
    const name = logicalName.trim().replace(/^\/+/, "");
    if (name) {
      return name;
    }
    if (selectedFile) {
      return joinFilePath(currentPath, selectedFile.name);
    }
    return currentPath ? `${currentPath}/…` : "…";
  }, [logicalName, selectedFile, currentPath]);

  async function refresh(path = currentPath) {
    setLoading(true);
    setError("");
    try {
      const result = await client.listApplianceFiles(path);
      setCurrentPath(result.path || "");
      setItems(result.items);
    } catch (err) {
      setError(err instanceof ApiError ? err.detail : err instanceof Error ? err.message : "Could not list files.");
      setItems([]);
    } finally {
      setLoading(false);
    }
  }

  useEffect(() => {
    void refresh("");
  }, []);

  function openUploadDialog() {
    setLogicalName(currentPath ? `${currentPath}/` : "");
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
    const destination = logicalName.trim().replace(/^\/+|\/+$/g, "") || joinFilePath(currentPath, selectedFile.name);
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
      const nextPath = parentPath(result.path);
      await refresh(nextPath);
    } catch (err) {
      setError(err instanceof ApiError ? err.detail : err instanceof Error ? err.message : "Upload failed.");
    } finally {
      setUploading(false);
    }
  }

  async function downloadFile(entry: ApplianceFileEntry) {
    setError("");
    try {
      const blob = await client.downloadApplianceFile(entry.path);
      const url = URL.createObjectURL(blob);
      const anchor = document.createElement("a");
      anchor.href = url;
      anchor.download = entry.name;
      anchor.click();
      URL.revokeObjectURL(url);
    } catch (err) {
      setError(err instanceof ApiError ? err.detail : err instanceof Error ? err.message : "Download failed.");
    }
  }

  return (
    <PageFrame
      title="Files"
      eyebrow=""
      description="Named file spaces on this appliance. Paths are relative to the appliance files store — no host login required."
      pathname="/manage/files"
      onNavigate={navigate}
      tabs={[{ label: "Browse", path: "/manage/files" }]}
    >
      {message ? <div className="message">{message}</div> : null}
      {error ? <div className="message message--error">{error}</div> : null}

      <Card
        title="File spaces"
        subtitle="Browse directories and files already stored on the appliance"
      >
        <div className="button-row" style={{ marginBottom: "1rem" }}>
          <button className="button button--primary" type="button" onClick={openUploadDialog}>
            + Add file
          </button>
          <button className="button button--ghost" type="button" onClick={() => void refresh(currentPath)}>
            Refresh
          </button>
        </div>

        <div className="badge-row" style={{ marginBottom: "1rem" }}>
          {breadcrumbs.map((crumb, index) => (
            <button
              key={`${crumb.path}-${crumb.label}`}
              className="pill"
              type="button"
              onClick={() => void refresh(crumb.path)}
              style={{ cursor: "pointer" }}
            >
              {index === 0 ? "files:/" : crumb.label}
            </button>
          ))}
        </div>

        {loading ? (
          <EmptyState message="Loading file spaces…" />
        ) : items.length === 0 ? (
          <EmptyState message="No files here yet. Use Add file to upload from this machine." />
        ) : (
          <div className="table-list">
            {items.map((entry) => (
              <div className="table-list__row" key={entry.path}>
                <div>
                  <strong>{entry.name}</strong>
                  <span>
                    {entry.path} · {entry.type === "directory" ? "directory" : formatBytes(entry.sizeBytes)}
                    {entry.modifiedAt ? ` · ${entry.modifiedAt}` : ""}
                  </span>
                </div>
                <div className="button-row">
                  {entry.type === "directory" ? (
                    <button className="button button--ghost" type="button" onClick={() => void refresh(entry.path)}>
                      Open
                    </button>
                  ) : (
                    <button className="button button--ghost" type="button" onClick={() => void downloadFile(entry)}>
                      Download
                    </button>
                  )}
                </div>
              </div>
            ))}
          </div>
        )}
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
    </PageFrame>
  );
}
