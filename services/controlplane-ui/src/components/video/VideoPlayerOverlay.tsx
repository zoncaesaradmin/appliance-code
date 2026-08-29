import React, { useCallback, useEffect, useRef, useState } from "react";
import { client } from "../../lib/api";
import { displayTitle, formatClock } from "../../lib/videoLibrary";
import type { ApplianceFileEntry } from "../../types";

const PLAYBACK_RATES = [0.75, 1, 1.25, 1.5, 2];
const COOKIE_REFRESH_MS = 10 * 60 * 1000;

type VideoElement = HTMLVideoElement & {
  webkitEnterFullscreen?: () => void;
  webkitExitFullscreen?: () => void;
  webkitDisplayingFullscreen?: boolean;
};

export function exitPlayerFullscreen(root: HTMLElement | null): boolean {
  const video = root?.querySelector("video") as VideoElement | null;
  if (document.fullscreenElement) {
    void document.exitFullscreen();
    return true;
  }
  if (video?.webkitDisplayingFullscreen) {
    video.webkitExitFullscreen?.();
    return true;
  }
  return false;
}

export function VideoPlayerOverlay(props: {
  entry: ApplianceFileEntry;
  src: string;
  error: string;
  canDelete: boolean;
  rootRef: React.MutableRefObject<HTMLDivElement | null>;
  onClose: () => void;
  onDelete: () => void;
  onError: (message: string) => void;
}): React.JSX.Element {
  const videoRef = useRef<VideoElement | null>(null);
  const [paused, setPaused] = useState(false);
  const [muted, setMuted] = useState(false);
  const [currentTime, setCurrentTime] = useState(0);
  const [duration, setDuration] = useState(0);
  const [rate, setRate] = useState(1);
  const [fullscreen, setFullscreen] = useState(false);

  const syncFullscreen = useCallback(() => {
    const video = videoRef.current;
    setFullscreen(Boolean(document.fullscreenElement || video?.webkitDisplayingFullscreen));
  }, []);

  useEffect(() => {
    const media = videoRef.current;
    if (!media) {
      return;
    }
    const onTime = () => {
      setCurrentTime(media.currentTime);
      setDuration(Number.isFinite(media.duration) ? media.duration : 0);
      setPaused(media.paused);
      setMuted(media.muted);
    };
    media.addEventListener("timeupdate", onTime);
    media.addEventListener("durationchange", onTime);
    media.addEventListener("play", onTime);
    media.addEventListener("pause", onTime);
    media.addEventListener("volumechange", onTime);
    document.addEventListener("fullscreenchange", syncFullscreen);
    return () => {
      media.removeEventListener("timeupdate", onTime);
      media.removeEventListener("durationchange", onTime);
      media.removeEventListener("play", onTime);
      media.removeEventListener("pause", onTime);
      media.removeEventListener("volumechange", onTime);
      document.removeEventListener("fullscreenchange", syncFullscreen);
    };
  }, [props.src, syncFullscreen]);

  useEffect(() => {
    const id = window.setInterval(() => {
      void client.prepareVideoPlayback().catch(() => undefined);
    }, COOKIE_REFRESH_MS);
    return () => window.clearInterval(id);
  }, [props.src]);

  useEffect(() => {
    const playButton = props.rootRef.current?.querySelector<HTMLElement>('[data-nav-id="player-play"]');
    playButton?.focus();
  }, [props.src, props.rootRef]);

  function togglePlay() {
    const video = videoRef.current;
    if (!video) {
      return;
    }
    if (video.paused) {
      void video.play();
    } else {
      video.pause();
    }
  }

  function skip(seconds: number) {
    const video = videoRef.current;
    if (!video) {
      return;
    }
    const limit = Number.isFinite(video.duration) ? video.duration : Number.POSITIVE_INFINITY;
    video.currentTime = Math.min(limit, Math.max(0, video.currentTime + seconds));
  }

  function seekTo(value: number) {
    const video = videoRef.current;
    if (!video) {
      return;
    }
    video.currentTime = value;
  }

  function cycleRate() {
    const video = videoRef.current;
    const index = PLAYBACK_RATES.indexOf(rate);
    const next = PLAYBACK_RATES[(index + 1) % PLAYBACK_RATES.length] ?? 1;
    setRate(next);
    if (video) {
      video.playbackRate = next;
    }
  }

  function toggleMute() {
    const video = videoRef.current;
    if (!video) {
      return;
    }
    video.muted = !video.muted;
  }

  async function toggleFullscreen() {
    const video = videoRef.current;
    const root = props.rootRef.current;
    if (document.fullscreenElement) {
      await document.exitFullscreen();
      return;
    }
    if (video?.webkitDisplayingFullscreen) {
      video.webkitExitFullscreen?.();
      return;
    }
    try {
      if (root?.requestFullscreen) {
        await root.requestFullscreen();
        return;
      }
    } catch {
      // Some TV and iOS browsers only fullscreen the media element.
    }
    video?.webkitEnterFullscreen?.();
  }

  return (
    <div
      className="video-player"
      ref={(node) => {
        props.rootRef.current = node;
      }}
      role="dialog"
      aria-modal="true"
      aria-labelledby="video-player-title"
    >
      <header className="video-player__top">
        <button
          type="button"
          className="video-player__icon-btn"
          data-nav
          data-nav-id="player-close"
          aria-label="Close player"
          onClick={props.onClose}
        >
          Close
        </button>
        <div className="video-player__heading">
          <h2 id="video-player-title">{displayTitle(props.entry)}</h2>
          <p>{props.entry.path}</p>
        </div>
        <button
          type="button"
          className="video-player__icon-btn"
          data-nav
          data-nav-id="player-fullscreen"
          aria-label={fullscreen ? "Exit full screen" : "Full screen"}
          onClick={() => void toggleFullscreen()}
        >
          {fullscreen ? "Exit full screen" : "Full screen"}
        </button>
      </header>

      <div className="video-player__stage">
        <video
          ref={videoRef}
          key={props.src}
          className="video-player__video"
          controlsList="nodownload"
          disablePictureInPicture
          playsInline
          preload="metadata"
          autoPlay
          src={props.src}
          onClick={togglePlay}
          onError={() =>
            props.onError("This video could not be played. Upload an MP4 with H.264 video and AAC audio.")
          }
        >
          Your browser does not support HTML5 video.
        </video>
        {paused ? (
          <button type="button" className="video-player__center-play" aria-hidden="true" tabIndex={-1} onClick={togglePlay}>
            ▶
          </button>
        ) : null}
      </div>

      {props.error ? <div className="video-player__error">{props.error}</div> : null}

      <div className="video-player__controls">
        <label className="video-player__seek">
          <span className="video-player__sr">Seek</span>
          <input
            type="range"
            min={0}
            max={duration || 0}
            step={0.1}
            value={Math.min(currentTime, duration || 0)}
            data-nav
            data-nav-id="player-seek"
            aria-label="Seek"
            onChange={(event) => seekTo(Number(event.target.value))}
          />
        </label>
        <div className="video-player__times">
          <span>{formatClock(currentTime)}</span>
          <span>{formatClock(duration)}</span>
        </div>
        <div className="video-player__buttons">
          <button type="button" className="video-player__icon-btn" data-nav data-nav-id="player-back" aria-label="Skip back 10 seconds" onClick={() => skip(-10)}>
            −10s
          </button>
          <button
            type="button"
            className="video-player__icon-btn video-player__icon-btn--play"
            data-nav
            data-nav-id="player-play"
            aria-label={paused ? "Play" : "Pause"}
            onClick={togglePlay}
          >
            {paused ? "Play" : "Pause"}
          </button>
          <button type="button" className="video-player__icon-btn" data-nav data-nav-id="player-forward" aria-label="Skip forward 10 seconds" onClick={() => skip(10)}>
            +10s
          </button>
          <button type="button" className="video-player__icon-btn" data-nav data-nav-id="player-mute" aria-label={muted ? "Unmute" : "Mute"} onClick={toggleMute}>
            {muted ? "Unmute" : "Mute"}
          </button>
          <button type="button" className="video-player__icon-btn" data-nav data-nav-id="player-rate" aria-label="Playback speed" onClick={cycleRate}>
            {rate}×
          </button>
          {props.canDelete ? (
            <button type="button" className="video-player__icon-btn video-player__icon-btn--danger" data-nav data-nav-id="player-delete" onClick={props.onDelete}>
              Delete
            </button>
          ) : null}
        </div>
      </div>
    </div>
  );
}
