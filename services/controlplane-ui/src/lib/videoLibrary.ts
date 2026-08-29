import type { ApplianceFileEntry, ApplianceFileListResult, Session } from "../types";

export type VideoTreeRow = {
  entry: ApplianceFileEntry;
  depth: number;
};

export type VideoShelf = {
  id: string;
  title: string;
  directoryPath: string;
  videos: ApplianceFileEntry[];
};

export function isPlayableVideo(name: string): boolean {
  return /\.mp4$/i.test(name);
}

export function hasPermission(session: Pick<Session, "permissions">, permission: string): boolean {
  const wanted = permission.trim().toLowerCase();
  return session.permissions.some((entry) => entry.trim().toLowerCase() === wanted);
}

export function canManageVideoLibrary(session: Pick<Session, "permissions">): boolean {
  return hasPermission(session, "video.library.write");
}

export function formatBytes(size: number): string {
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

export function formatModifiedAt(value: string): string {
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

export function formatClock(seconds: number): string {
  if (!Number.isFinite(seconds) || seconds < 0) {
    return "0:00";
  }
  const total = Math.floor(seconds);
  const hours = Math.floor(total / 3600);
  const minutes = Math.floor((total % 3600) / 60);
  const rest = total % 60;
  if (hours > 0) {
    return `${hours}:${String(minutes).padStart(2, "0")}:${String(rest).padStart(2, "0")}`;
  }
  return `${minutes}:${String(rest).padStart(2, "0")}`;
}

export function joinFilePath(prefix: string, name: string): string {
  const cleanPrefix = prefix.trim().replace(/^\/+|\/+$/g, "");
  const cleanName = name.trim().replace(/^\/+/, "");
  if (!cleanPrefix) {
    return cleanName;
  }
  return `${cleanPrefix}/${cleanName}`;
}

export function normalizeRelativePath(path: string): string {
  return path.trim().replace(/^\/+/, "");
}

export function parentDirectory(path: string): string {
  const parts = normalizeRelativePath(path).split("/").filter(Boolean);
  parts.pop();
  return parts.join("/");
}

export function displayTitle(entry: ApplianceFileEntry): string {
  return entry.name.replace(/\.mp4$/i, "") || entry.name;
}

export function posterHue(value: string): number {
  let hash = 0;
  for (const char of value) {
    hash = (hash * 31 + char.charCodeAt(0)) >>> 0;
  }
  return hash % 360;
}

export function buildVideoShelves(videos: ApplianceFileEntry[]): VideoShelf[] {
  const buckets = new Map<string, ApplianceFileEntry[]>();
  for (const video of videos) {
    const directoryPath = parentDirectory(video.path);
    const list = buckets.get(directoryPath) ?? [];
    list.push(video);
    buckets.set(directoryPath, list);
  }
  const keys = [...buckets.keys()].sort((left, right) => {
    if (left === "") {
      return -1;
    }
    if (right === "") {
      return 1;
    }
    return left.localeCompare(right);
  });
  return keys.map((directoryPath) => {
    const items = [...(buckets.get(directoryPath) ?? [])].sort((left, right) =>
      left.name.localeCompare(right.name)
    );
    return {
      id: directoryPath || "library",
      title: directoryPath || "Library",
      directoryPath,
      videos: items
    };
  });
}

export async function collectVideoTree(
  list: (path: string) => Promise<ApplianceFileListResult>,
  path = "",
  depth = 0
): Promise<VideoTreeRow[]> {
  const result = await list(path);
  const rows: VideoTreeRow[] = [];
  for (const entry of result.items) {
    rows.push({ entry, depth });
    if (entry.type === "directory") {
      rows.push(...(await collectVideoTree(list, entry.path, depth + 1)));
    }
  }
  return rows;
}

export function playableVideos(rows: VideoTreeRow[]): ApplianceFileEntry[] {
  return rows
    .filter((row) => row.entry.type === "file" && isPlayableVideo(row.entry.name))
    .map((row) => row.entry);
}
