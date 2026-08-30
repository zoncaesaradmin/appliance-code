import type { ApplianceFileEntry } from "../types";
import { client } from "./api";

const POSTER_STORAGE_PREFIX = "appliance.controlplane-ui.video-poster.";
const POSTER_WIDTH = 320;
const POSTER_QUALITY = 0.72;
const MAX_CONCURRENT_CAPTURES = 2;
const CAPTURE_TIMEOUT_MS = 12_000;

const memoryCache = new Map<string, string>();
const inflight = new Map<string, Promise<string | null>>();

let activeCaptures = 0;
const captureWaiters: Array<() => void> = [];
let playbackCookie: Promise<void> | null = null;

export function posterStorageKey(path: string, modifiedAt: string): string {
  return `${POSTER_STORAGE_PREFIX}${encodeURIComponent(path)}:${modifiedAt}`;
}

export function readCachedPoster(path: string, modifiedAt: string): string | null {
  const key = posterStorageKey(path, modifiedAt);
  const memory = memoryCache.get(key);
  if (memory) {
    return memory;
  }
  try {
    const stored = window.sessionStorage.getItem(key);
    if (stored?.startsWith("data:image/")) {
      memoryCache.set(key, stored);
      return stored;
    }
  } catch {
    // Private-mode or missing sessionStorage.
  }
  return null;
}

export function writeCachedPoster(path: string, modifiedAt: string, dataUrl: string): void {
  const key = posterStorageKey(path, modifiedAt);
  memoryCache.set(key, dataUrl);
  try {
    window.sessionStorage.setItem(key, dataUrl);
  } catch {
    // Quota or missing storage; memory cache still helps this visit.
  }
}

async function withCaptureSlot<T>(work: () => Promise<T>): Promise<T> {
  while (activeCaptures >= MAX_CONCURRENT_CAPTURES) {
    await new Promise<void>((resolve) => {
      captureWaiters.push(resolve);
    });
  }
  activeCaptures += 1;
  try {
    return await work();
  } finally {
    activeCaptures -= 1;
    captureWaiters.shift()?.();
  }
}

async function ensurePlaybackCookie(): Promise<void> {
  if (!playbackCookie) {
    playbackCookie = client.prepareVideoPlayback().catch((error: unknown) => {
      playbackCookie = null;
      throw error;
    });
  }
  await playbackCookie;
}

function waitForEvent(target: EventTarget, eventName: string): Promise<void> {
  return new Promise((resolve, reject) => {
    const onError = () => reject(new Error("video failed to load"));
    target.addEventListener(eventName, () => resolve(), { once: true });
    target.addEventListener("error", onError, { once: true });
  });
}

function withTimeout<T>(promise: Promise<T>, ms: number): Promise<T> {
  return new Promise<T>((resolve, reject) => {
    const timer = window.setTimeout(() => reject(new Error("poster capture timed out")), ms);
    promise.then(
      (value) => {
        window.clearTimeout(timer);
        resolve(value);
      },
      (error: unknown) => {
        window.clearTimeout(timer);
        reject(error);
      }
    );
  });
}

async function captureVideoFrame(src: string): Promise<string | null> {
  const video = document.createElement("video");
  video.muted = true;
  video.defaultMuted = true;
  video.playsInline = true;
  video.preload = "auto";
  video.setAttribute("playsinline", "true");
  video.setAttribute("muted", "true");
  video.style.cssText = "position:fixed;left:-9999px;top:0;width:16px;height:9px;opacity:0;pointer-events:none";
  document.body.appendChild(video);
  video.src = src;

  const cleanup = () => {
    video.removeAttribute("src");
    video.load();
    video.remove();
  };

  try {
    await withTimeout(waitForEvent(video, "loadeddata"), CAPTURE_TIMEOUT_MS);
    try {
      await video.play();
    } catch {
      // Some browsers decode a still without play(); seek still works after loadeddata.
    }
    video.pause();
    const duration = Number.isFinite(video.duration) ? video.duration : 0;
    const target = duration > 2 ? 1 : duration > 0 ? Math.max(duration * 0.2, 0.05) : 0;
    if (Math.abs(video.currentTime - target) > 0.05) {
      const seeked = waitForEvent(video, "seeked");
      video.currentTime = target;
      await withTimeout(seeked, 8_000);
    }
    const width = video.videoWidth;
    const height = video.videoHeight;
    if (width < 2 || height < 2) {
      cleanup();
      return null;
    }
    const canvas = document.createElement("canvas");
    canvas.width = POSTER_WIDTH;
    canvas.height = Math.max(1, Math.round((POSTER_WIDTH * height) / width));
    const context = canvas.getContext("2d");
    if (!context) {
      cleanup();
      return null;
    }
    context.drawImage(video, 0, 0, canvas.width, canvas.height);
    const dataUrl = canvas.toDataURL("image/jpeg", POSTER_QUALITY);
    cleanup();
    return dataUrl.startsWith("data:image/") ? dataUrl : null;
  } catch {
    cleanup();
    return null;
  }
}

export async function loadVideoPoster(entry: Pick<ApplianceFileEntry, "path" | "modifiedAt">): Promise<string | null> {
  const cached = readCachedPoster(entry.path, entry.modifiedAt);
  if (cached) {
    return cached;
  }
  const inflightKey = posterStorageKey(entry.path, entry.modifiedAt);
  const pending = inflight.get(inflightKey);
  if (pending) {
    return pending;
  }
  const work = withCaptureSlot(async () => {
    const again = readCachedPoster(entry.path, entry.modifiedAt);
    if (again) {
      return again;
    }
    try {
      await ensurePlaybackCookie();
      const dataUrl = await captureVideoFrame(client.videoStreamURL(entry.path));
      if (dataUrl) {
        writeCachedPoster(entry.path, entry.modifiedAt, dataUrl);
      }
      return dataUrl;
    } catch {
      playbackCookie = null;
      return null;
    }
  }).finally(() => {
    inflight.delete(inflightKey);
  });
  inflight.set(inflightKey, work);
  return work;
}
