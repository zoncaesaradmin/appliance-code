import React, { act } from "react";
import { createRoot, type Root } from "react-dom/client";
import { afterEach, beforeEach, expect, it, vi } from "vitest";
import { LocationMap } from "./LocationMap";

let element: HTMLDivElement;
let root: Root;

beforeEach(() => {
  Object.assign(globalThis, { IS_REACT_ACT_ENVIRONMENT: true });
  element = document.createElement("div");
  document.body.appendChild(element);
  root = createRoot(element);
});

afterEach(() => {
  act(() => root.unmount());
  element.remove();
});

it("exposes zoom controls and changes the zoom label", () => {
  act(() => {
    root.render(<LocationMap location={{ latitude: 12.72, longitude: 77.71 }} defaultZoom={13} />);
  });
  expect(element.querySelector('[aria-label="Map zoom"]')).toBeTruthy();
  expect(element.textContent).toContain("Location preview · z13");
  const zoomIn = element.querySelector('[aria-label="Zoom in"]') as HTMLButtonElement;
  act(() => {
    zoomIn.click();
  });
  expect(element.textContent).toContain("Location preview · z14");
});

it("reports a new point when the interactive map is clicked", () => {
  const onSelect = vi.fn();
  act(() => {
    root.render(<LocationMap location={{ latitude: 12.72, longitude: 77.71 }} interactive onSelect={onSelect} />);
  });
  const map = element.querySelector(".application-location-map") as HTMLDivElement;
  expect(map).toBeTruthy();
  Object.defineProperty(map, "getBoundingClientRect", {
    value: () => ({ left: 0, top: 0, width: 320, height: 240, right: 320, bottom: 240, x: 0, y: 0, toJSON: () => ({}) })
  });
  act(() => {
    map.dispatchEvent(new MouseEvent("click", { bubbles: true, clientX: 200, clientY: 140 }));
  });
  expect(onSelect).toHaveBeenCalledTimes(1);
  const next = onSelect.mock.calls[0][0] as { latitude: number; longitude: number };
  expect(Number.isFinite(next.latitude)).toBe(true);
  expect(Number.isFinite(next.longitude)).toBe(true);
});
