import React, { FormEvent, useCallback, useEffect, useMemo, useRef, useState } from "react";
import { EmptyState, PageFrame } from "../components";
import { VideoCard } from "../components/video/VideoCard";
import { exitPlayerFullscreen, VideoPlayerOverlay } from "../components/video/VideoPlayerOverlay";
import { ApiError } from "../client";
import { client } from "../lib/api";
import { isTelevisionUserAgent } from "../lib/device";
import { navigate } from "../lib/navigate";
import { useSpatialNavigation } from "../lib/useSpatialNavigation";
import {
  buildVideoShelves,
  canManageVideoLibrary,
  collectVideoTree,
  displayTitle,
  formatBytes,
  formatModifiedAt,
  isPlayableVideo,
  joinFilePath,
  normalizeRelativePath,
  playableVideos
} from "../lib/videoLibrary";
import type { ApplianceFileEntry, Session } from "../types";

type UploadState = {
  tone: "info" | "success" | "error";
  title: string;
  detail: string;
};

type PendingDelete =
  | { kind: "file"; entry: ApplianceFileEntry }
  | { kind: "directory"; path: string; title: string };

function describeDestinationPath(directory: string, selectedFile: File | null): string {
  return joinFilePath(normalizeRelativePath(directory), selectedFile?.name || "video.mp4");
}

export function VideosPage(props: { session: Session }): React.JSX.Element {
  const canManage = canManageVideoLibrary(props.session);
  const television = isTelevisionUserAgent();
  const [shelves, setShelves] = useState(buildVideoShelves([]));
  const [loading, setLoading] = useState(true);
  const [message, setMessage] = useState("");
  const [error, setError] = useState("");
  const [showUploadDialog, setShowUploadDialog] = useState(false);
  const [destinationDirectory, setDestinationDirectory] = useState("");
  const [selectedFile, setSelectedFile] = useState<File | null>(null);
  const [uploading, setUploading] = useState(false);
  const [uploadState, setUploadState] = useState<UploadState | null>(null);
  const [playing, setPlaying] = useState<ApplianceFileEntry | null>(null);
  const [playURL, setPlayURL] = useState("");
  const [playError, setPlayError] = useState("");
  const [pendingDelete, setPendingDelete] = useState<PendingDelete | null>(null);
  const [inspecting, setInspecting] = useState<ApplianceFileEntry | null>(null);

  const pageRef = useRef<HTMLDivElement | null>(null);
  const playerRef = useRef<HTMLDivElement | null>(null);
  const uploadRef = useRef<HTMLDivElement | null>(null);
  const inspectRef = useRef<HTMLDivElement | null>(null);
  const confirmRef = useRef<HTMLDivElement | null>(null);

  const destinationPreview = useMemo(
    () => describeDestinationPath(destinationDirectory, selectedFile),
    [destinationDirectory, selectedFile]
  );
  const videoCount = shelves.reduce((count, shelf) => count + shelf.videos.length, 0);

  async function refresh() {
    setLoading(true);
    setError("");
    try {
      const rows = await collectVideoTree((path) => client.listVideoLibrary(path));
      setShelves(buildVideoShelves(playableVideos(rows)));
    } catch (err) {
      setError(err instanceof ApiError ? err.detail : err instanceof Error ? err.message : "Could not list videos.");
      setShelves([]);
    } finally {
      setLoading(false);
    }
  }

  useEffect(() => {
    void refresh();
  }, []);

  useEffect(() => {
    if (loading || playing || showUploadDialog || pendingDelete || inspecting) {
      return;
    }
    const first = pageRef.current?.querySelector<HTMLElement>("[data-nav]");
    first?.focus();
  }, [loading, playing, showUploadDialog, pendingDelete, inspecting, shelves]);

  function openUploadDialog() {
    setDestinationDirectory("");
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
    const destination = joinFilePath(normalizeRelativePath(destinationDirectory), selectedFile.name);
    if (!isPlayableVideo(selectedFile.name)) {
      setError("Videos must be uploaded as MP4 files.");
      return;
    }
    setUploading(true);
    setError("");
    setUploadState({
      tone: "info",
      title: "Uploading video",
      detail: `Uploading ${destination} (${formatBytes(selectedFile.size)}) into the library.`
    });
    try {
      const result = await client.uploadVideoLibraryFile(destination, selectedFile);
      setMessage(result.overwritten ? `Updated ${result.path}.` : `Uploaded ${result.path}.`);
      setUploadState({
        tone: "success",
        title: result.overwritten ? "Video updated" : "Video uploaded",
        detail: `${result.path} is ${result.status === "ready" ? "ready to play" : "available"} (${formatBytes(result.size)}).`
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

  async function confirmDelete() {
    if (!pendingDelete) {
      return;
    }
    const target = pendingDelete.kind === "file" ? pendingDelete.entry.path : pendingDelete.path;
    setError("");
    try {
      await client.deleteVideoLibraryFile(target);
      if (playing && (playing.path === target || playing.path.startsWith(`${target}/`))) {
        setPlaying(null);
        setPlayURL("");
      }
      setMessage(`Deleted ${target}.`);
      setPendingDelete(null);
      setInspecting(null);
      await refresh();
    } catch (err) {
      setError(err instanceof ApiError ? err.detail : err instanceof Error ? err.message : "Delete failed.");
      setPendingDelete(null);
    }
  }

  async function playEntry(entry: ApplianceFileEntry) {
    if (entry.type === "directory" || !isPlayableVideo(entry.name)) {
      return;
    }
    setPlayError("");
    setPlaying(entry);
    try {
      await client.prepareVideoPlayback();
      setPlayURL(client.videoStreamURL(entry.path));
    } catch (err) {
      setPlaying(null);
      setPlayURL("");
      setPlayError(
        err instanceof ApiError ? err.detail : err instanceof Error ? err.message : "Could not load video."
      );
    }
  }

  function closePlayer() {
    setPlaying(null);
    setPlayURL("");
    setPlayError("");
  }

  const handlePlayerBack = useCallback(() => {
    if (exitPlayerFullscreen(playerRef.current)) {
      return;
    }
    setPlaying(null);
    setPlayURL("");
    setPlayError("");
  }, []);

  const handleModalBack = useCallback(() => {
    if (uploading) {
      return;
    }
    if (pendingDelete) {
      setPendingDelete(null);
      return;
    }
    setShowUploadDialog(false);
    setInspecting(null);
  }, [uploading, pendingDelete]);

  const pageOpen = !playing && !showUploadDialog && !pendingDelete && !inspecting;
  useSpatialNavigation({ containerRef: pageRef, enabled: pageOpen });
  useSpatialNavigation({
    containerRef: playerRef,
    enabled: Boolean(playing) && !pendingDelete && !inspecting,
    onBack: handlePlayerBack
  });
  useSpatialNavigation({
    containerRef: showUploadDialog ? uploadRef : pendingDelete ? confirmRef : inspectRef,
    enabled: showUploadDialog || Boolean(pendingDelete) || Boolean(inspecting),
    onBack: handleModalBack
  });

  const emptyMessage = television
    ? "No videos here yet. Upload from a computer or phone signed in to this appliance."
    : "No videos here yet. Use Upload video to add a file from this machine.";

  return (
    <PageFrame
      title="Videos"
      eyebrow=""
      description="Watch videos stored on this appliance. Select a title to play it full screen."
      pathname="/manage/videos"
      onNavigate={navigate}
      tabs={[{ label: "Library", path: "/manage/videos" }]}
      className="page-frame--videos"
    >
      <div className="video-library" ref={pageRef}>
        {message ? <div className="message">{message}</div> : null}
        {error ? <div className="message message--error">{error}</div> : null}
        {playError && !playing ? <div className="message message--error">{playError}</div> : null}

        <section className="video-library__stage">
          <div className="video-library__toolbar">
            {canManage && !television ? (
              <button className="button button--primary" type="button" data-nav data-nav-id="upload" onClick={openUploadDialog}>
                Upload video
              </button>
            ) : null}
            <button
              className="button button--ghost"
              type="button"
              data-nav
              data-nav-id="refresh"
              onClick={() => void refresh()}
              disabled={loading}
            >
              Refresh
            </button>
            <p className="video-library__count">
              {loading ? "Loading library…" : `${videoCount} video${videoCount === 1 ? "" : "s"}`}
            </p>
          </div>

          {uploadState ? (
            <div
              className={
                uploadState.tone === "error"
                  ? "status-box status-box--danger video-library__status"
                  : uploadState.tone === "success"
                    ? "status-box status-box--success video-library__status"
                    : "status-box video-library__status"
              }
            >
              <strong>{uploadState.title}</strong>
              <span>{uploadState.detail}</span>
            </div>
          ) : null}

          {loading ? (
            <EmptyState message="Loading video library…" />
          ) : videoCount === 0 ? (
            <EmptyState message={emptyMessage} />
          ) : (
            shelves.map((shelf) => (
              <section className="video-shelf" key={shelf.id} aria-label={shelf.title}>
                <div className="video-shelf__header">
                  <h2>{shelf.title}</h2>
                  {canManage && shelf.directoryPath ? (
                    <button
                      type="button"
                      className="video-shelf__manage"
                      data-nav
                      data-nav-id={`delete-dir:${shelf.directoryPath}`}
                      onClick={() =>
                        setPendingDelete({ kind: "directory", path: shelf.directoryPath, title: shelf.title })
                      }
                    >
                      Delete folder
                    </button>
                  ) : null}
                </div>
                <div className="video-shelf__row">
                  {shelf.videos.map((entry) => (
                    <VideoCard
                      key={entry.path}
                      entry={entry}
                      onPlay={() => void playEntry(entry)}
                      onMore={() => setInspecting(entry)}
                    />
                  ))}
                </div>
              </section>
            ))
          )}
        </section>
      </div>

      {playing && playURL ? (
        <VideoPlayerOverlay
          entry={playing}
          src={playURL}
          error={playError}
          rootRef={playerRef}
          onClose={closePlayer}
          onError={setPlayError}
        />
      ) : null}

      {showUploadDialog ? (
        <div
          className="video-modal-scrim"
          role="presentation"
          onClick={closeUploadDialog}
        >
          <div
            className="video-modal"
            role="dialog"
            aria-modal="true"
            aria-labelledby="videos-upload-dialog-title"
            ref={uploadRef}
            onClick={(event) => event.stopPropagation()}
          >
            <h2 id="videos-upload-dialog-title">Upload video</h2>
            <p>
              Upload a browser-compatible MP4. It is validated synchronously for H.264 video and AAC audio before it is added to the library.
            </p>
            <form className="stack-form" onSubmit={submitUpload}>
              <label className="field">
                <span>Destination directory (optional)</span>
                <input
                  value={destinationDirectory}
                  placeholder="training/intro"
                  data-nav
                  data-nav-id="upload-directory"
                  onChange={(event) => setDestinationDirectory(event.target.value)}
                  disabled={uploading}
                />
              </label>
              <label className="field">
                <span>Video from this computer</span>
                <input
                  type="file"
                  accept="video/mp4,.mp4"
                  data-nav
                  data-nav-id="upload-file"
                  onChange={(event) => setSelectedFile(event.target.files?.[0] ?? null)}
                  disabled={uploading}
                />
              </label>
              <p className="video-modal__hint">
                The uploaded filename is kept. Leave the directory empty to store it at the library root; this is stored as <strong>{destinationPreview}</strong>.
              </p>
              <div className="button-row">
                <button className="button button--ghost" type="button" data-nav data-nav-id="upload-cancel" onClick={closeUploadDialog} disabled={uploading}>
                  Cancel
                </button>
                <button className="button button--primary" type="submit" data-nav data-nav-id="upload-submit" disabled={uploading || !selectedFile}>
                  {uploading ? "Uploading…" : "Upload"}
                </button>
              </div>
            </form>
          </div>
        </div>
      ) : null}

      {inspecting ? (
        <div className="video-modal-scrim" role="presentation" onClick={() => setInspecting(null)}>
          <div
            className="video-modal"
            role="dialog"
            aria-modal="true"
            aria-labelledby="videos-details-dialog-title"
            ref={inspectRef}
            onClick={(event) => event.stopPropagation()}
          >
            <h2 id="videos-details-dialog-title">{displayTitle(inspecting)}</h2>
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
                <dt>Size</dt>
                <dd>{formatBytes(inspecting.sizeBytes)}</dd>
              </div>
              <div className="video-details__row">
                <dt>Added</dt>
                <dd>{formatModifiedAt(inspecting.modifiedAt)}</dd>
              </div>
              {inspecting.status ? (
                <div className="video-details__row">
                  <dt>Status</dt>
                  <dd>{inspecting.status === "ready" ? "Ready to play" : inspecting.status}</dd>
                </div>
              ) : null}
            </dl>
            <div className="button-row">
              <button className="button button--ghost" type="button" data-nav data-nav-id="details-close" onClick={() => setInspecting(null)}>
                Close
              </button>
				{canManage ? (
					<button
						className="button button--primary"
						type="button"
						onClick={() => void client.putFocusContent({ resourceType: "video", resourcePath: inspecting.path, title: displayTitle(inspecting) }).then(() => {
							setMessage(`${displayTitle(inspecting)} is now the current focus.`);
							setInspecting(null);
						}).catch((err) => setError(err instanceof Error ? err.message : "Could not set current focus."))}
					>
						Set as current focus
					</button>
				) : null}
              {canManage ? (
                <button
                  className="button button--danger"
                  type="button"
                  data-nav
                  data-nav-id="details-delete"
                  onClick={() => {
                    setPendingDelete({ kind: "file", entry: inspecting });
                    setInspecting(null);
                  }}
                >
                  Delete
                </button>
              ) : null}
            </div>
          </div>
        </div>
      ) : null}

      {pendingDelete ? (
        <div className="video-modal-scrim" role="presentation" onClick={() => setPendingDelete(null)}>
          <div
            className="video-modal"
            role="dialog"
            aria-modal="true"
            aria-labelledby="videos-delete-dialog-title"
            ref={confirmRef}
            onClick={(event) => event.stopPropagation()}
          >
            <h2 id="videos-delete-dialog-title">Delete {pendingDelete.kind === "file" ? "video" : "folder"}</h2>
            <p>
              Delete {pendingDelete.kind === "file" ? `“${displayTitle(pendingDelete.entry)}”` : `folder “${pendingDelete.title}”`}? This cannot be undone.
            </p>
            <div className="button-row">
              <button className="button button--ghost" type="button" data-nav data-nav-id="delete-cancel" onClick={() => setPendingDelete(null)}>
                Cancel
              </button>
              <button className="button button--danger" type="button" data-nav data-nav-id="delete-confirm" onClick={() => void confirmDelete()}>
                Delete
              </button>
            </div>
          </div>
        </div>
      ) : null}
    </PageFrame>
  );
}
