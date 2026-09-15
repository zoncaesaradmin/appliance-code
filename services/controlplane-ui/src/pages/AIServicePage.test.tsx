import React, { act } from "react";
import { createRoot, type Root } from "react-dom/client";
import { afterEach, beforeEach, expect, it, vi } from "vitest";
import { AIServicePage } from "./AIServicePage";

const api = vi.hoisted(() => ({
  getInferenceStatus: vi.fn(),
  listInferenceModels: vi.fn(),
  getInferenceCatalog: vi.fn(),
  importInferenceModel: vi.fn(),
  loadInferenceModel: vi.fn(),
  deleteInferenceModel: vi.fn()
}));
vi.mock("../lib/api", () => ({ client: api }));
vi.mock("../lib/navigate", () => ({ navigate: vi.fn() }));
vi.mock("../components", () => ({
  PageFrame: ({ children }: { children: React.ReactNode }) => <main>{children}</main>,
  Card: ({ title, children }: { title: string; children: React.ReactNode }) => (
    <section>
      <h2>{title}</h2>
      {children}
    </section>
  ),
  EmptyState: ({ message }: { message: string }) => <p>{message}</p>,
  Button: ({
    children,
    onClick,
    disabled
  }: {
    children: React.ReactNode;
    onClick: () => void;
    disabled?: boolean;
  }) => (
    <button onClick={onClick} disabled={disabled}>
      {children}
    </button>
  )
}));

let element: HTMLDivElement;
let root: Root;
beforeEach(() => {
  vi.clearAllMocks();
  Object.assign(globalThis, { IS_REACT_ACT_ENVIRONMENT: true });
  element = document.createElement("div");
  document.body.append(element);
  root = createRoot(element);
  api.getInferenceStatus.mockResolvedValue({ engine: "ollama", architecture: "amd64", activeMode: "cpu", ready: true });
  api.listInferenceModels.mockResolvedValue([{ id: "retired:1b" }]);
  api.getInferenceCatalog.mockResolvedValue({
    lastSuccess: "2026-09-15T00:00:00Z",
    stale: true,
    refreshing: false,
    scope: "Popular models",
    items: [
      { id: "available:1b", source: "available:1b", downloadBytes: 100, memoryBytes: 200, eligible: true },
      { id: "too-large:100b", source: "too-large:100b", downloadBytes: 1000, memoryBytes: 2000, eligible: false }
    ]
  });
  api.importInferenceModel.mockResolvedValue(undefined);
});
afterEach(async () => {
  await act(async () => root.unmount());
  element.remove();
});

it("offers one model dropdown with downloaded marks and no search controls", async () => {
  await act(async () => root.render(<AIServicePage />));
  const select = element.querySelector("select");
  expect(select).not.toBeNull();
  expect(element.querySelector("input")).toBeNull();
  expect(element.querySelector("textarea")).toBeNull();
  const labels = [...select!.options].map((option) => option.textContent);
  expect(labels).toContain("retired:1b (downloaded)");
  expect(labels).toContain("available:1b");
  expect(labels.some((label) => label?.includes("too-large:100b"))).toBe(false);
  await act(async () => {
    select!.value = "available:1b";
    select!.dispatchEvent(new Event("change", { bubbles: true }));
  });
  const download = [...element.querySelectorAll("button")].find((button) => button.textContent === "Download")!;
  await act(async () => download.click());
  expect(api.importInferenceModel).toHaveBeenCalledWith({
    catalogId: "available:1b",
    modelId: "available:1b",
    source: "available:1b"
  });
});

it("keeps downloaded models in the dropdown when catalog discovery fails", async () => {
  api.getInferenceCatalog.mockRejectedValue(new Error("offline"));
  await act(async () => root.render(<AIServicePage />));
  expect(element.textContent).toContain("Model discovery is unavailable");
  const select = element.querySelector("select");
  expect([...select!.options].map((option) => option.textContent)).toContain("retired:1b (downloaded)");
  expect([...element.querySelectorAll("button")].some((button) => button.textContent === "Load")).toBe(true);
});
