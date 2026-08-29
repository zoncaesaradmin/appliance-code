import type { SpatialDirection } from "./device";

export type NavRect = {
  id: string;
  x: number;
  y: number;
  width: number;
  height: number;
};

function center(rect: NavRect): { x: number; y: number } {
  return { x: rect.x + rect.width / 2, y: rect.y + rect.height / 2 };
}

export function pickSpatialNeighbor(
  current: NavRect,
  candidates: NavRect[],
  direction: SpatialDirection
): NavRect | undefined {
  const origin = center(current);
  let best: NavRect | undefined;
  let bestScore = Number.POSITIVE_INFINITY;

  for (const candidate of candidates) {
    if (candidate.id === current.id) {
      continue;
    }
    const point = center(candidate);
    const dx = point.x - origin.x;
    const dy = point.y - origin.y;
    if (direction === "left" && dx >= -1) {
      continue;
    }
    if (direction === "right" && dx <= 1) {
      continue;
    }
    if (direction === "up" && dy >= -1) {
      continue;
    }
    if (direction === "down" && dy <= 1) {
      continue;
    }
    const absX = Math.abs(dx);
    const absY = Math.abs(dy);
    const primary = direction === "left" || direction === "right" ? absX : absY;
    const secondary = direction === "left" || direction === "right" ? absY : absX;
    const score = primary + (secondary * secondary) / 120;
    if (score < bestScore) {
      best = candidate;
      bestScore = score;
    }
  }
  return best;
}

export function navRectFromElement(element: HTMLElement, index: number): NavRect {
  const box = element.getBoundingClientRect();
  return {
    id: element.dataset.navId || element.id || `nav-${index}`,
    x: box.left,
    y: box.top,
    width: box.width,
    height: box.height
  };
}

export function visibleNavElements(root: ParentNode): HTMLElement[] {
  return [...root.querySelectorAll<HTMLElement>("[data-nav]")].filter((element) => {
    if (element.hasAttribute("disabled") || element.getAttribute("aria-disabled") === "true") {
      return false;
    }
    const style = window.getComputedStyle(element);
    if (style.display === "none" || style.visibility === "hidden") {
      return false;
    }
    const box = element.getBoundingClientRect();
    return box.width > 0 && box.height > 0;
  });
}
