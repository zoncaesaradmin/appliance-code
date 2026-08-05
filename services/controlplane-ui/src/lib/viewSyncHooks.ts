import { useSyncExternalStore } from "react";
import {
  getViewSyncRegionGeneration,
  getViewSyncTagGeneration,
  type ViewSyncRegion,
  viewSyncStore
} from "./viewSync";

/**
 * React subscription to a view-sync region generation.
 * When any mutation invalidates that region, the generation increments and
 * components that list it in a dependency array re-run their effects.
 */
export function useViewSyncGeneration(region: ViewSyncRegion): number {
  return useSyncExternalStore(
    viewSyncStore.subscribe,
    () => getViewSyncRegionGeneration(region),
    () => 0
  );
}

/** React subscription to a domain tag generation (e.g. "licensing"). */
export function useViewSyncTag(tag: string): number {
  return useSyncExternalStore(
    viewSyncStore.subscribe,
    () => getViewSyncTagGeneration(tag),
    () => 0
  );
}
