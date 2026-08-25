import React, { FormEvent, useEffect, useState } from "react";
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

function isPlayableVideo(name: string): boolean {
  return /\.(mp4|webm|ogg|mov|m4v)$/i.test(name);
}

export function VideosPage(): React.JSX.Element {
  const [currentPath, setCurrentPath] = useState("");
  const [items, setItems] = useState<ApplianceFileEntry[]>([]);
  const [loading, setLoading] = useState(true);
  const [message, setMessage] = useState("");
  const [error, setError] = useState("");
  const [showUploadDialog, setShowUploadDialog] = useState(false);
  const [logicalName, setLogicalName] = useState("");
  const [selectedFile, setSelectedFile] = useState<File | null>(null);
  const [uploading, setUploading] = useState(false);
  const [playing, setPlaying] = useState<ApplianceFileEntry | null>(null);
  const [playURL, setPlayURL] = useState("");
  const [playError, setPlayError] = useState("");

  async function refresh(path = currentPath) {
    setLoading(true);
    setError("");
    try {
      const result = await client.listVideoLibrary(path);
      setCurrentPath(result.path || "");
      setItems(result.items);
    } catch (err) {
      setError(err instanceof ApiError ? err.detail : err instanceof Error ? err.message : "Could not list videos.");
      setItems([]);
    } finally {
      setLoading(false);
    }
  }

  useEffect(() => {
    void refresh("");
  }, []);

  useEffect(() => {
    return () => {
      if (playURL) {
        URL.revokeObjectURL(playURL);
      }
    };
  }, [playURL]);

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
      setError("Choose a video file to upload.");
      return;
    }
    const destination =
      logicalName.trim().replace(/^\/+|\/+$/g, "") || joinFilePath(currentPath, selectedFile.name);
    if (!destination) {
      setError("Enter a destination path for the video.");
      return;
    }
    setUploading(true);
    setError("");
    try {
      const result = await client.uploadVideoLibraryFile(destination, selectedFile);
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
    const kind = entry.type === "directory" ? "directory" : "video";
    if (!window.confirm(`Delete ${kind} “${entry.name}”? This cannot be undone.`)) {
      return;
    }
    setError("");
    try {
      await client.deleteVideoLibraryFile(entry.path);
      if (playing?.path === entry.path) {
        setPlaying(null);
        setPlayURL((current) => {
          if (current) {
            URL.revokeObjectURL(current);
          }
          return "";
        });
      }
      setMessage(`Deleted ${entry.path}.`);
      await refresh(currentPath);
    } catch (err) {
      setError(err instanceof ApiError ? err.detail : err instanceof Error ? err.message : "Delete failed.");
    }
  }

  async function playEntry(entry: ApplianceFileEntry) {
    if (entry.type === "directory" || !isPlayableVideo(entry.name)) {
      return;
    }
    setPlayError("");
    setPlaying(entry);
    try {
      const blob = await client.downloadVideoLibraryFile(entry.path);
      setPlayURL((current) => {
        if (current) {
          URL.revokeObjectURL(current);
        }
        return URL.createObjectURL(blob);
      });
    } catch (err) {
      setPlaying(null);
      setPlayURL((current) => {
        if (current) {
          URL.revokeObjectURL(current);
        }
        return "";
      });
      setPlayError(
        err instanceof ApiError ? err.detail : err instanceof Error ? err.message : "Could not load video."
      );
    }
  }

  return (
    <PageFrame
      title="Videos"
      eyebrow=""
      description="Upload, browse, and play videos on this appliance."
      pathname="/manage/videos"
      onNavigate={navigate}
      tabs={[{ label: "Library", path: "/manage/videos" }]}
    >
      {message ? <div className="message">{message}</div> : null}
      {error ? <div className="message message--error">{error}</div> : null}
      {playError ? <div className="message message--error">{playError}</div> : null}

      {playing && playURL ? (
        <Card title="Now playing" subtitle={playing.path}>
          <video
            key={playURL}
            className="w-full max-h-[28rem] rounded-2xl bg-slate-950"
            controls
            autoPlay
            src={playURL}
          >
            Your browser does not support HTML5 video.
          </video>
        </Card>
      ) : null}

      <Card title="Video library" subtitle="Upload clips and play them from this appliance">
        <div className="button-row" style={{ marginBottom: "1rem" }}>
          <button className="button button--primary" type="button" onClick={openUploadDialog}>
            + Upload video
          </button>
          {currentPath ? (
            <button className="button button--ghost" type="button" onClick={() => void refresh(parentPath(currentPath))}>
              Up
            </button>
          ) : null}
        </div>

        {loading ? (
          <EmptyState message="Loading video library…" />
        ) : items.length === 0 ? (
          <EmptyState message="No videos here yet. Use Upload video to add a file from this machine." />
        ) : (
          <ResourceList>
            {items.map((entry) => (
              <ResourceListRow
                key={entry.path}
                ariaLabel={
                  entry.type === "directory"
                    ? `Open directory ${entry.name}`
                    : `Video ${entry.name}`
                }
                onClick={
                  entry.type === "directory"
                    ? () => void refresh(entry.path)
                    : isPlayableVideo(entry.name)
                      ? () => void playEntry(entry)
                      : undefined
                }
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
                  ...(entry.type === "file" && isPlayableVideo(entry.name)
                    ? [
                        {
                          id: "play",
                          label: "Play",
                          onSelect: () => void playEntry(entry)
                        }
                      ]
                    : []),
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
            aria-labelledby="videos-upload-dialog-title"
            onClick={(event) => event.stopPropagation()}
          >
            <h2
              id="videos-upload-dialog-title"
              className="m-0 text-xl font-bold tracking-tight text-slate-950"
            >
              Upload video
            </h2>
            <p className="mt-2 mb-4 text-sm text-slate-500">
              Pick a destination path and a video file from this computer.
            </p>
            <form className="stack-form" onSubmit={submitUpload}>
              <label className="field">
                <span>Destination path</span>
                <input
                  value={logicalName}
                  placeholder="clips/intro.mp4"
                  onChange={(event) => setLogicalName(event.target.value)}
                  disabled={uploading}
                />
              </label>
              <label className="field">
                <span>Video from this computer</span>
                <input
                  type="file"
                  accept="video/*,.mp4,.webm,.ogg,.mov,.m4v"
                  onChange={(event) => setSelectedFile(event.target.files?.[0] ?? null)}
                  disabled={uploading}
                />
              </label>
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
