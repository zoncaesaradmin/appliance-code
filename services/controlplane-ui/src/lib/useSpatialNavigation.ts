import { useEffect, type RefObject } from "react";
import { arrowDirection, isBackKey, isEditableTarget, isRangeInput } from "./device";
import { navRectFromElement, pickSpatialNeighbor, visibleNavElements } from "./spatialNav";

export function useSpatialNavigation(options: {
  enabled?: boolean;
  containerRef: RefObject<HTMLElement | null>;
  onBack?: () => void;
}): void {
  const enabled = options.enabled !== false;
  const { containerRef, onBack } = options;

  useEffect(() => {
    if (!enabled) {
      return;
    }

    function handleKeyDown(event: KeyboardEvent) {
      if (isBackKey(event)) {
        if (!onBack) {
          return;
        }
        event.preventDefault();
        onBack();
        return;
      }

      const direction = arrowDirection(event);
      if (!direction) {
        return;
      }
      if (isEditableTarget(event.target) && (direction === "left" || direction === "right")) {
        return;
      }
      if (isRangeInput(event.target) && (direction === "left" || direction === "right")) {
        return;
      }

      const root = containerRef.current;
      if (!root) {
        return;
      }
      const elements = visibleNavElements(root);
      if (elements.length === 0) {
        return;
      }
      const active = document.activeElement;
      const currentElement = active instanceof HTMLElement && elements.includes(active) ? active : null;
      if (!currentElement) {
        event.preventDefault();
        elements[0]?.focus();
        return;
      }
      const rects = elements.map((element, index) => navRectFromElement(element, index));
      const currentIndex = elements.indexOf(currentElement);
      const currentRect = rects[currentIndex];
      if (!currentRect) {
        return;
      }
      const nextRect = pickSpatialNeighbor(currentRect, rects, direction);
      if (!nextRect) {
        return;
      }
      const nextElement = elements[rects.findIndex((rect) => rect.id === nextRect.id)];
      if (!nextElement) {
        return;
      }
      event.preventDefault();
      nextElement.focus();
      nextElement.scrollIntoView({ block: "nearest", inline: "center", behavior: "smooth" });
    }

    document.addEventListener("keydown", handleKeyDown);
    return () => document.removeEventListener("keydown", handleKeyDown);
  }, [containerRef, enabled, onBack]);
}
