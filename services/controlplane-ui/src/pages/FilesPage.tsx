import React, { FormEvent, useEffect, useMemo, useState } from "react";
import { Card, EmptyState, PageFrame, ResourceList, ResourceListRow } from "../components";
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
      await refresh(parentPath(result.path));
    } catch (err) {
      setError(err instanceof ApiError ? err.detail : err instanceof Error ? err.message : "Upload failed.");
    } finally {
      setUploading(false);
    }
  }

  async function deleteEntry(entry: ApplianceFileEntry) {
    const kind = entry.type === "directory" ? "directory" : "file";
    if (!window.confirm(`Delete ${kind} “${entry.name}”? This cannot be undone.`)) {
      return;
    }
    setError("");
    try {
      await client.deleteApplianceFile(entry.path);
      setMessage(`Deleted ${entry.path}.`);
      await refresh(currentPath);
    } catch (err) {
      setError(err instanceof ApiError ? err.detail : err instanceof Error ? err.message : "Delete failed.");
    }
  }

  return (
    <PageFrame
      title="Files"
      eyebrow=""
      description="Named file spaces on this appliance."
      pathname="/manage/files"
      onNavigate={navigate}
      tabs={[{ label: "Browse", path: "/manage/files" }]}
    >
      {message ? <div className="message">{message}</div> : null}
      {error ? <div className="message message--error">{error}</div> : null}

      <Card title="File spaces" subtitle="Browse directories and files stored on the appliance">
        <div className="button-row" style={{ marginBottom: "1rem" }}>
          <button className="button button--primary" type="button" onClick={openUploadDialog}>
            + Add file
          </button>
        </div>

        {loading ? (
          <EmptyState message="Loading file spaces…" />
        ) : items.length === 0 ? (
          <EmptyState message="No files here yet. Use Add file to upload from this machine." />
        ) : (
          <ResourceList>
            {items.map((entry) => (
              <ResourceListRow
                key={entry.path}
                ariaLabel={
                  entry.type === "directory"
                    ? `Open directory ${entry.name}`
                    : `File ${entry.name}`
                }
                onClick={entry.type === "directory" ? () => void refresh(entry.path) : undefined}
                actionsLabel={`Actions for ${entry.name}`}
                columns={[
                  {
                    key: "name",
                    label: "Name",
                    value: entry.type === "directory" ? `${entry.name}/` : entry.name
                  },
                  {
                    key: "size",
                    label: "Size",
                    value: entry.type === "directory" ? "—" : formatBytes(entry.sizeBytes)
                  },
                  {
                    key: "modified",
                    label: "Modified",
                    value: formatModifiedAt(entry.modifiedAt)
                  }
                ]}
                actions={[
                  {
                    id: "delete",
                    label: "Delete",
                    danger: true,
                    onSelect: () => void deleteEntry(entry)
                  }
                ]}
              />
            ))}
          </ResourceList>
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
