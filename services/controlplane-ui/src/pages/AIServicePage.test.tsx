import React, { act } from "react";
import { createRoot, type Root } from "react-dom/client";
import { afterEach, beforeEach, expect, it, vi } from "vitest";
import { AIServicePage } from "./AIServicePage";

const api = vi.hoisted(() => ({
  getInferenceStatus: vi.fn(),
  listInferenceModels: vi.fn(),
  getInferenceCatalog: vi.fn(),
  importInferenceModel: vi.fn(),
  getInferenceImportProgress: vi.fn(),
  loadInferenceModel: vi.fn(),
  getInferenceLoadProgress: vi.fn(),
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
  api.getInferenceStatus.mockResolvedValue({
    engine: "ollama",
    architecture: "amd64",
    activeMode: "cpu",
    ready: true,
    servingState: "inactive"
  });
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
  api.getInferenceImportProgress.mockResolvedValue({ state: "idle" });
  api.getInferenceLoadProgress.mockResolvedValue({ state: "idle" });
  api.importInferenceModel.mockResolvedValue({
    modelId: "available:1b",
    source: "available:1b",
    state: "complete",
    percent: 100,
    message: "Model downloaded"
  });
  api.loadInferenceModel.mockResolvedValue({
    modelId: "retired:1b",
    state: "ready",
    message: "Model is ready for use"
  });
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
  expect(element.textContent).toMatch(/~1B params/);
  expect(element.textContent).toMatch(/est\. RAM/);
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

it("explains when catalog items exist but none are eligible", async () => {
  api.listInferenceModels.mockResolvedValue([]);
  api.getInferenceCatalog.mockResolvedValue({
    lastSuccess: "2026-09-15T00:00:00Z",
    stale: false,
    refreshing: false,
    scope: "Popular models",
    items: [{ id: "too-large:100b", source: "too-large:100b", downloadBytes: 1000, memoryBytes: 2000, eligible: false }]
  });
  await act(async () => root.render(<AIServicePage />));
  expect(element.querySelector("select")).toBeNull();
  expect(element.textContent).toContain("No catalog models currently fit this appliance");
});

it("clears a stale downloaded-models refresh error after a successful post-download refresh", async () => {
  api.listInferenceModels
    .mockRejectedValueOnce(new Error("temporary timeout"))
    .mockResolvedValue([{ id: "retired:1b" }, { id: "available:1b" }]);
  await act(async () => root.render(<AIServicePage />));
  expect(element.textContent).toContain("Could not refresh downloaded models");

  const select = element.querySelector("select")!;
  await act(async () => {
    select.value = "available:1b";
    select.dispatchEvent(new Event("change", { bubbles: true }));
  });
  const download = [...element.querySelectorAll("button")].find((button) => button.textContent === "Download")!;
  await act(async () => {
    download.click();
  });
  expect(element.textContent).not.toContain("Could not refresh downloaded models");
  expect(element.textContent).toContain("available:1b downloaded");
  expect([...element.querySelectorAll("option")].map((option) => option.textContent)).toContain(
    "available:1b (downloaded)"
  );
});

it("polls import progress while an async download runs", async () => {
  api.importInferenceModel.mockResolvedValue({
    modelId: "available:1b",
    source: "available:1b",
    state: "downloading",
    percent: 10,
    bytesDownloaded: 10,
    bytesTotal: 100,
    message: "Downloading model files"
  });
  api.getInferenceImportProgress
    .mockResolvedValueOnce({ state: "idle" })
    .mockResolvedValueOnce({
      modelId: "available:1b",
      state: "downloading",
      percent: 55,
      bytesDownloaded: 55,
      bytesTotal: 100,
      message: "Downloading model files"
    })
    .mockResolvedValue({
      modelId: "available:1b",
      state: "complete",
      percent: 100,
      bytesDownloaded: 100,
      bytesTotal: 100,
      message: "Model downloaded"
    });
  api.listInferenceModels
    .mockResolvedValueOnce([{ id: "retired:1b" }])
    .mockResolvedValue([{ id: "retired:1b" }, { id: "available:1b" }]);

  await act(async () => root.render(<AIServicePage />));
  const select = element.querySelector("select")!;
  await act(async () => {
    select.value = "available:1b";
    select.dispatchEvent(new Event("change", { bubbles: true }));
  });
  const download = [...element.querySelectorAll("button")].find((button) => button.textContent === "Download");
  expect(download).toBeTruthy();

  vi.useFakeTimers();
  await act(async () => download!.click());
  expect(element.textContent).toContain("Downloading model files");
  expect(element.querySelector("progress")).not.toBeNull();

  await act(async () => {
    await vi.advanceTimersByTimeAsync(2000);
  });
  expect(element.textContent).toContain("55%");

  await act(async () => {
    await vi.advanceTimersByTimeAsync(2000);
  });
  expect(element.textContent).toContain("available:1b downloaded");
  vi.useRealTimers();
});

it("sorts the model dropdown by estimated parameters descending", async () => {
  api.getInferenceCatalog.mockResolvedValue({
    lastSuccess: "2026-09-15T00:00:00Z",
    stale: false,
    refreshing: false,
    scope: "Popular models",
    sort: "parameters",
    order: "desc",
    items: [
      { id: "org/tiny-0.5B", source: "org/tiny-0.5B", downloadBytes: 100, memoryBytes: 200, eligible: true },
      { id: "org/large-7B", source: "org/large-7B", downloadBytes: 100, memoryBytes: 200, eligible: true },
      { id: "org/mid-3B", source: "org/mid-3B", downloadBytes: 100, memoryBytes: 200, eligible: true }
    ]
  });
  api.listInferenceModels.mockResolvedValue([]);
  await act(async () => root.render(<AIServicePage />));
  const labels = [...element.querySelector("select")!.options].map((option) => option.textContent);
  expect(labels).toEqual(["org/large-7B", "org/mid-3B", "org/tiny-0.5B"]);
  expect(api.getInferenceCatalog).toHaveBeenCalledWith({ sort: "parameters", order: "desc" });
});

it("shows serving ready-for-use and disables Load when the selected model is already loaded", async () => {
  api.getInferenceStatus.mockResolvedValue({
    engine: "vllm",
    architecture: "amd64",
    activeMode: "cuda",
    ready: true,
    servingState: "ready",
    loadedModelId: "retired:1b"
  });
  await act(async () => root.render(<AIServicePage />));
  expect(element.textContent).toContain("Ready for use (retired:1b)");
  const select = element.querySelector("select")!;
  await act(async () => {
    select.value = "retired:1b";
    select.dispatchEvent(new Event("change", { bubbles: true }));
  });
  const load = [...element.querySelectorAll("button")].find((button) => button.textContent === "Ready");
  expect(load).toBeTruthy();
  expect(load).toHaveProperty("disabled", true);
});

it("polls load progress while an async load runs", async () => {
  api.loadInferenceModel.mockResolvedValue({
    modelId: "retired:1b",
    state: "loading",
    message: "Loading model into the inference engine"
  });
  api.getInferenceLoadProgress
    .mockResolvedValueOnce({ state: "idle" })
    .mockResolvedValueOnce({
      modelId: "retired:1b",
      state: "loading",
      message: "Loading model into the inference engine"
    })
    .mockResolvedValue({
      modelId: "retired:1b",
      state: "ready",
      message: "Model is ready for use"
    });
  api.getInferenceStatus
    .mockResolvedValueOnce({
      engine: "vllm",
      architecture: "amd64",
      activeMode: "cuda",
      ready: true,
      servingState: "inactive"
    })
    .mockResolvedValue({
      engine: "vllm",
      architecture: "amd64",
      activeMode: "cuda",
      ready: true,
      servingState: "ready",
      loadedModelId: "retired:1b"
    });

  await act(async () => root.render(<AIServicePage />));
  const select = element.querySelector("select")!;
  await act(async () => {
    select.value = "retired:1b";
    select.dispatchEvent(new Event("change", { bubbles: true }));
  });
  const load = [...element.querySelectorAll("button")].find((button) => button.textContent === "Load");
  expect(load).toBeTruthy();

  vi.useFakeTimers();
  await act(async () => load!.click());
  expect(element.textContent).toContain("Loading model into the inference engine");

  await act(async () => {
    await vi.advanceTimersByTimeAsync(2000);
  });
  await act(async () => {
    await vi.advanceTimersByTimeAsync(2000);
  });
  expect(element.textContent).toContain("retired:1b is ready for use");
  vi.useRealTimers();
});

it("summarizes parameter and memory capacity for the selected model", async () => {
  const { modelCapacitySummary } = await import("./AIServicePage");
  expect(
    modelCapacitySummary({
      id: "Qwen/Qwen2.5-0.5B-Instruct",
      source: "Qwen/Qwen2.5-0.5B-Instruct@abc",
      downloadBytes: 999_604_126,
      memoryBytes: 6_271_162_944,
      eligible: true
    })
  ).toBe("~0.5B params · download 0.9 GiB · est. RAM 5.8 GiB");
  expect(
    modelCapacitySummary({
      id: "openai-community/gpt2",
      source: "openai-community/gpt2@abc",
      downloadBytes: 1_000_000_000,
      memoryBytes: 4_000_000_000,
      eligible: true
    })
  ).toContain("params (est.)");
});
