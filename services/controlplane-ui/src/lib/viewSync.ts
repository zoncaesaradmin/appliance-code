/**
 * Generic client-side view synchronization after successful mutations.
 *
 * Pattern (TanStack Query / RTK Query style invalidation): a write action
 * declares which presentation regions must resync. Shell chrome, page data,
 * and optional app bootstrap subscribe independently by region — no page
 * hard-codes knowledge of alert widgets or sibling routes.
 *
 * Usage for a mutation that changes cross-cutting UI:
 *
 *   const status = await client.acceptBaseEntitlement();
 *   requestViewSync({
 *     regions: ["shell.alerts", "page", "shell.bootstrap"],
 *     tags: ["licensing", "setup"],
 *     reason: "licensing.base-accepted",
 *   });
 *
 * Pages/shell then re-fetch via useViewSyncGeneration / useViewSyncTag.
 */

export type ViewSyncRegion =
  /** Header alerts / notification bell. */
  | "shell.alerts"
  /** App-level session and capability bootstrap. */
  | "shell.bootstrap"
  /** Currently mounted page (opt-in listeners). */
  | "page"
  /** Last-resort remount of the signed-in SPA shell. */
  | "app";

export type ViewSyncRequest = {
  regions: ViewSyncRegion[];
  /**
   * Optional fine-grained topic tags. Subscribers that care about a domain
   * (licensing, profiles, host-services, …) bump only when listed.
   */
  tags?: string[];
  /** Free-text for logs and tests; never shown to operators by default. */
  reason?: string;
};

type Snapshot = {
  regions: Record<ViewSyncRegion, number>;
  tags: Record<string, number>;
  last?: ViewSyncRequest;
};

const emptyRegions = (): Record<ViewSyncRegion, number> => ({
  "shell.alerts": 0,
  "shell.bootstrap": 0,
  page: 0,
  app: 0
});

let snapshot: Snapshot = {
  regions: emptyRegions(),
  tags: {}
};

const listeners = new Set<() => void>();

function emit(): void {
  for (const listener of listeners) {
    listener();
  }
}

function getSnapshot(): Snapshot {
  return snapshot;
}

function subscribe(listener: () => void): () => void {
  listeners.add(listener);
  return () => {
    listeners.delete(listener);
  };
}

/**
 * Invalidate the declared presentation regions (and tags) so subscribers re-fetch.
 * Idempotent callers may call after every successful mutation that may affect shared UI.
 */
export function requestViewSync(request: ViewSyncRequest): void {
  if (!request.regions.length && !(request.tags && request.tags.length)) {
    return;
  }
  const nextRegions = { ...snapshot.regions };
  for (const region of request.regions) {
    nextRegions[region] = (nextRegions[region] ?? 0) + 1;
  }
  const nextTags = { ...snapshot.tags };
  for (const tag of request.tags ?? []) {
    const key = tag.trim();
    if (!key) {
      continue;
    }
    nextTags[key] = (nextTags[key] ?? 0) + 1;
  }
  snapshot = {
    regions: nextRegions,
    tags: nextTags,
    last: request
  };
  emit();
}

/** Run an async mutation, then request view sync only after it succeeds. */
export async function withViewSync<T>(
  action: () => Promise<T>,
  request: ViewSyncRequest
): Promise<T> {
  const result = await action();
  requestViewSync(request);
  return result;
}

/** Generation for a shell/page region. Use as a useEffect dependency. */
export function getViewSyncRegionGeneration(region: ViewSyncRegion): number {
  return snapshot.regions[region] ?? 0;
}

/** Generation for a topic tag. Use as a useEffect dependency. */
export function getViewSyncTagGeneration(tag: string): number {
  return snapshot.tags[tag] ?? 0;
}

/** Test helper: last request (if any). */
export function getLastViewSyncRequest(): ViewSyncRequest | undefined {
  return snapshot.last;
}

/** Test helper: reset generations. */
export function resetViewSyncForTests(): void {
  snapshot = { regions: emptyRegions(), tags: {} };
  emit();
}

export const viewSyncStore = {
  subscribe,
  getSnapshot
};
