import React, { FormEvent, useEffect, useMemo, useState } from "react";
import { Card, EmptyState, PageFrame, RowActionsMenu, type RowAction } from "../components";
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

type VideoTreeRow = {
  entry: ApplianceFileEntry;
  depth: number;
};

type UploadState = {
  tone: "info" | "success" | "error";
  title: string;
  detail: string;
};

function joinFilePath(prefix: string, name: string): string {
  const cleanPrefix = prefix.trim().replace(/^\/+|\/+$/g, "");
  const cleanName = name.trim().replace(/^\/+/, "");
  if (!cleanPrefix) {
    return cleanName;
  }
  return `${cleanPrefix}/${cleanName}`;
}

function normalizeRelativePath(path: string): string {
  return path.trim().replace(/^\/+/, "");
}

function resolveDestinationPath(destination: string, selectedFile: File): string {
  const cleaned = normalizeRelativePath(destination);
  if (!cleaned) {
    return selectedFile.name;
  }
  if (cleaned.endsWith("/")) {
    return joinFilePath(cleaned, selectedFile.name);
  }
  return cleaned.replace(/\/+$/g, "");
}

function describeDestinationPath(destination: string, selectedFile: File | null): string {
  const cleaned = normalizeRelativePath(destination);
  if (!cleaned) {
    return selectedFile ? selectedFile.name : "folder/video.mp4";
  }
  if (cleaned.endsWith("/")) {
    return `${cleaned}${selectedFile?.name || "…"}`;
  }
  return cleaned;
}

function isPlayableVideo(name: string): boolean {
  return /\.(mp4|webm|ogg|mov|m4v)$/i.test(name);
}

async function collectVideoTree(path = "", depth = 0): Promise<VideoTreeRow[]> {
  const result = await client.listVideoLibrary(path);
  const rows: VideoTreeRow[] = [];
  for (const entry of result.items) {
    rows.push({ entry, depth });
    if (entry.type === "directory") {
      rows.push(...(await collectVideoTree(entry.path, depth + 1)));
    }
  }
  return rows;
}

export function VideosPage(): React.JSX.Element {
  const [items, setItems] = useState<VideoTreeRow[]>([]);
  const [loading, setLoading] = useState(true);
  const [message, setMessage] = useState("");
  const [error, setError] = useState("");
  const [showUploadDialog, setShowUploadDialog] = useState(false);
  const [logicalName, setLogicalName] = useState("");
  const [selectedFile, setSelectedFile] = useState<File | null>(null);
  const [uploading, setUploading] = useState(false);
  const [uploadState, setUploadState] = useState<UploadState | null>(null);
  const [playing, setPlaying] = useState<ApplianceFileEntry | null>(null);
  const [playURL, setPlayURL] = useState("");
  const [playError, setPlayError] = useState("");

  const destinationPreview = useMemo(
    () => describeDestinationPath(logicalName, selectedFile),
    [logicalName, selectedFile]
  );

  const videoCount = items.filter((row) => row.entry.type === "file").length;
  const directoryCount = items.filter((row) => row.entry.type === "directory").length;

  async function refresh() {
    setLoading(true);
    setError("");
    try {
      setItems(await collectVideoTree(""));
    } catch (err) {
      setError(err instanceof ApiError ? err.detail : err instanceof Error ? err.message : "Could not list videos.");
      setItems([]);
    } finally {
      setLoading(false);
    }
  }

  useEffect(() => {
    void refresh();
  }, []);

  useEffect(() => {
    return () => {
      if (playURL) {
        URL.revokeObjectURL(playURL);
      }
    };
  }, [playURL]);

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
      setError("Choose a video file to upload.");
      return;
    }
    const destination = resolveDestinationPath(logicalName, selectedFile);
    if (!destination) {
      setError("Enter a destination path for the video.");
      return;
    }
    setUploading(true);
    setError("");
    setUploadState({
      tone: "info",
      title: "Uploading video",
      detail: `Uploading ${destination} (${formatBytes(selectedFile.size)}) into the library root hierarchy.`
    });
    try {
      const result = await client.uploadVideoLibraryFile(destination, selectedFile);
      setMessage(result.overwritten ? `Updated ${result.path}.` : `Uploaded ${result.path}.`);
      setUploadState({
        tone: "success",
        title: result.overwritten ? "Video updated" : "Video uploaded",
        detail: `${result.path} is now available in the library tree (${formatBytes(result.size)}).`
      });
      setShowUploadDialog(false);
      await refresh();
    } catch (err) {
      const detail = err instanceof ApiError ? err.detail : err instanceof Error ? err.message : "Upload failed.";
      setError(detail);
      setUploadState({
        tone: "error",
        title: "Upload failed",
        detail: `${destination}: ${detail}`
      });
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
      await refresh();
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

      <Card
        title="Video library"
        subtitle={`Current videos and directories shown from the library root (${videoCount} videos, ${directoryCount} directories)`}
      >
        <div className="button-row" style={{ marginBottom: "1rem" }}>
          <button className="button button--primary" type="button" onClick={openUploadDialog}>
            + Upload video
          </button>
          <button className="button button--ghost" type="button" onClick={() => void refresh()} disabled={loading}>
            Refresh
          </button>
        </div>
        <p className="text-sm text-slate-500" style={{ marginTop: 0, marginBottom: "1rem" }}>
          Paths below are relative to the video library root. The appliance base path stays hidden.
        </p>
        {uploadState ? (
          <div
            className={
              uploadState.tone === "error"
                ? "status-box status-box--danger video-tree__status"
                : uploadState.tone === "success"
                  ? "status-box status-box--success video-tree__status"
                  : "status-box video-tree__status"
            }
          >
            <strong>{uploadState.title}</strong>
            <span>{uploadState.detail}</span>
          </div>
        ) : null}

        {loading ? (
          <EmptyState message="Loading video library…" />
        ) : items.length === 0 ? (
          <EmptyState message="No videos here yet. Use Upload video to add a file from this machine." />
        ) : (
          <div className="video-tree">
            {items.map(({ entry, depth }) => {
              const rowActions: RowAction[] = [
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
              ];
              const clickable = entry.type === "file" && isPlayableVideo(entry.name);
              const pathLabel = entry.type === "directory" ? `${entry.path}/` : entry.path;
              return (
                <div
                  key={entry.path}
                  className={clickable ? "video-tree__row video-tree__row--clickable" : "video-tree__row"}
                  role={clickable ? "button" : undefined}
                  tabIndex={clickable ? 0 : undefined}
                  aria-label={entry.type === "directory" ? `Directory ${pathLabel}` : `Video ${pathLabel}`}
                  onClick={clickable ? () => void playEntry(entry) : undefined}
                  onKeyDown={
                    clickable
                      ? (event) => {
                          if (event.key === "Enter" || event.key === " ") {
                            event.preventDefault();
                            void playEntry(entry);
                          }
                        }
                      : undefined
                  }
                >
                  <div className="video-tree__primary" style={{ paddingLeft: `${depth * 1.25}rem` }}>
                    <span className="video-tree__glyph" aria-hidden="true">
                      {entry.type === "directory" ? "▸" : "•"}
                    </span>
                    <div className="video-tree__text">
                      <strong className="video-tree__name">
                        {entry.type === "directory" ? `${entry.name}/` : entry.name}
                      </strong>
                      <code className="video-tree__path">{pathLabel}</code>
                    </div>
                  </div>
                  <div className="video-tree__meta">
                    <span>{entry.type === "directory" ? "Directory" : formatBytes(entry.sizeBytes)}</span>
                    <span>{formatModifiedAt(entry.modifiedAt)}</span>
                  </div>
                  <div
                    className="video-tree__actions"
                    onClick={(event) => event.stopPropagation()}
                    onKeyDown={(event) => event.stopPropagation()}
                  >
                    <RowActionsMenu label={`Actions for ${entry.name}`} actions={rowActions} />
                  </div>
                </div>
              );
            })}
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
              Pick a relative destination path and a video file from this computer. Add a trailing slash to upload into a directory.
            </p>
            <form className="stack-form" onSubmit={submitUpload}>
              <label className="field">
                <span>Destination path</span>
                <input
                  value={logicalName}
                  placeholder="clips/intro.mp4 or clips/"
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
              <p className="text-sm text-slate-500" style={{ margin: 0 }}>
                Stored as <strong>{destinationPreview}</strong> relative to the video library root.
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
