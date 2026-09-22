import React, { act } from "react";
import { createRoot, type Root } from "react-dom/client";
import { afterEach, beforeEach, expect, it, vi } from "vitest";
import { AIServicePage, buildOpenAIClientSettings } from "./AIServicePage";

const api = vi.hoisted(() => ({
  getInferenceStatus: vi.fn(),
  listInferenceModels: vi.fn(),
  getInferenceCatalog: vi.fn(),
  importInferenceModel: vi.fn(),
  getInferenceImportProgress: vi.fn(),
  loadInferenceModel: vi.fn(),
  getInferenceLoadProgress: vi.fn(),
  deleteInferenceModel: vi.fn(),
  getIdentity: vi.fn()
}));
vi.mock("../lib/api", () => ({ client: api }));
vi.mock("../lib/navigate", () => ({ navigate: vi.fn() }));
vi.mock("../components", () => ({
  PageFrame: ({ children }: { children: React.ReactNode }) => <main>{children}</main>,
  Card: ({
    title,
    children,
    className
  }: {
    title: string;
    children: React.ReactNode;
    className?: string;
  }) => (
    <section className={className}>
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
    acceleration: "standard",
    gpuAvailable: false,
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
      {
        id: "available:1b",
        source: "available:1b",
        downloadBytes: 100,
        memoryBytes: 200,
        eligible: true,
        capabilities: {
          experiences: ["chat", "coding-agent"],
          toolCalling: true,
          responsesCompatible: true,
          codexCompatible: true,
          verification: "template-reported"
        }
      },
      { id: "too-large:100b", source: "too-large:100b", downloadBytes: 1000, memoryBytes: 2000, eligible: false }
    ]
  });
  api.getInferenceImportProgress.mockResolvedValue({ state: "idle" });
  api.getInferenceLoadProgress.mockResolvedValue({ state: "idle" });
  api.getIdentity.mockResolvedValue({
    canonicalOrigin: "https://zon-appliance.example"
  });
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

it("puts the downloaded library beside the enabled-model view", async () => {
  await act(async () => root.render(<AIServicePage />));
  const headings = [...element.querySelectorAll("h2")].map((node) => node.textContent);
  expect(headings.indexOf("Model library")).toBeLessThan(headings.indexOf("Enabled model"));
  expect(element.querySelector(".ai-services-layout__models")).not.toBeNull();
  expect(element.querySelector(".ai-services-layout__status")).not.toBeNull();
});

it("separates downloaded inventory from enabled model instances", async () => {
  api.getInferenceStatus.mockResolvedValue({
    engine: "ollama",
    architecture: "amd64",
    acceleration: "standard",
    gpuAvailable: false,
    ready: true,
    servingState: "ready",
    loadedModelId: "retired:1b",
    instances: [{ id: "default", models: ["retired:1b"], replicas: 1 }]
  });
  api.listInferenceModels.mockResolvedValue([{ id: "retired:1b" }, { id: "stored:2b" }]);
  await act(async () => root.render(<AIServicePage />));
  expect(element.textContent).toContain("Downloaded models");
  expect(element.textContent).toContain("stored:2b");
  expect(element.textContent).toContain("Enabled model");
  expect(element.textContent).toContain("Default instance");
  expect(element.textContent).toContain("Downloaded · enabled");

  const select = element.querySelector("select");
  expect(select).not.toBeNull();
  expect(element.querySelector("input")).toBeNull();
  expect(element.querySelector("textarea")).toBeNull();
  const labels = [...select!.options].map((option) => option.textContent);
  expect(labels).toContain("retired:1b (Chat assistant · downloaded · enabled)");
  expect(labels).toContain("stored:2b (Chat assistant · downloaded)");
  expect(labels).toContain("available:1b (Coding agent)");
  expect(labels.some((label) => label?.includes("too-large:100b"))).toBe(false);
  await act(async () => {
    select!.value = "available:1b";
    select!.dispatchEvent(new Event("change", { bubbles: true }));
  });
  expect(element.textContent).toMatch(/~1B params/);
  expect(element.textContent).toMatch(/est\. model/);
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
  expect([...select!.options].map((option) => option.textContent)).toContain("retired:1b (Chat assistant · downloaded)");
  expect([...element.querySelectorAll("button")].some((button) => button.textContent === "Enable")).toBe(true);
});

it("does not mislabel a candidate when registry capability metadata is unavailable", async () => {
  api.listInferenceModels.mockResolvedValue([]);
  api.getInferenceCatalog.mockResolvedValue({
    lastSuccess: "2026-09-15T00:00:00Z",
    stale: true,
    refreshing: false,
    scope: "Popular models",
    items: [{
      id: "qwen2.5-coder:1.5b",
      source: "qwen2.5-coder:1.5b",
      downloadBytes: 100,
      memoryBytes: 200,
      eligible: true,
      capabilities: {
        experiences: ["chat"],
        toolCalling: false,
        responsesCompatible: false,
        codexCompatible: false,
        verification: "unverified"
      }
    }]
  });
  await act(async () => root.render(<AIServicePage />));
  const labels = [...element.querySelector("select")!.options].map((option) => option.textContent);
  expect(labels).toContain("qwen2.5-coder:1.5b (Capability unverified)");
  expect(element.textContent).toContain("Download the model to check its tool support");
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
    "available:1b (Coding agent · downloaded)"
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
  expect(labels).toEqual([
    "org/large-7B (Chat assistant)",
    "org/mid-3B (Chat assistant)",
    "org/tiny-0.5B (Chat assistant)"
  ]);
  expect(api.getInferenceCatalog).toHaveBeenCalledWith({ sort: "parameters", order: "desc" });
});

it("shows serving ready-for-use and disables Enable when the selected model is already enabled", async () => {
  api.getInferenceStatus.mockResolvedValue({
    engine: "vllm",
    architecture: "amd64",
    acceleration: "accelerated",
    gpuAvailable: true,
    ready: true,
    servingState: "ready",
    loadedModelId: "retired:1b",
    instances: [{ id: "default", models: ["retired:1b"], replicas: 1 }],
    maxModelLen: 1024
  });
  api.listInferenceModels.mockResolvedValue([
    {
      id: "retired:1b",
      launchArguments: ["--max-model-len", "32768"],
      capabilities: {
        experiences: ["chat", "coding-agent"],
        toolCalling: true,
        responsesCompatible: true,
        codexCompatible: true,
        verification: "verified"
      }
    }
  ]);
  await act(async () => root.render(<AIServicePage />));
  expect(element.textContent).toContain("Ready for use (retired:1b)");
  const select = element.querySelector("select")!;
  await act(async () => {
    select.value = "retired:1b";
    select.dispatchEvent(new Event("change", { bubbles: true }));
  });
  expect(element.textContent).toContain("Default instance");
  const enable = [...element.querySelectorAll("button")].find((button) => button.textContent === "Enabled");
  expect(enable).toBeTruthy();
  expect(enable).toHaveProperty("disabled", true);
  const copy = [...element.querySelectorAll("button")].find(
    (button) => button.textContent === "Copy OpenAI client settings"
  );
  expect(copy).toBeTruthy();
  await act(async () => copy!.click());
  expect(element.textContent).toContain("https://zon-appliance.example/inference/v1");
  expect(element.textContent).toContain("retired:1b");
  expect(element.textContent).toContain("model_context_window = 1024");
  expect(element.textContent).not.toContain("model_context_window = 32768");
  expect(element.textContent).toContain("zon_model_catalog.json");
});

it("keeps Codex settings unavailable for a chat-only enabled model", async () => {
  api.getInferenceStatus.mockResolvedValue({
    engine: "ollama",
    architecture: "amd64",
    acceleration: "standard",
    ready: true,
    servingState: "ready",
    loadedModelId: "deepseek-r1:14b",
    instances: [{ id: "default", models: ["deepseek-r1:14b"], replicas: 1 }]
  });
  api.listInferenceModels.mockResolvedValue([
    {
      id: "deepseek-r1:14b",
      capabilities: {
        experiences: ["chat"],
        toolCalling: false,
        responsesCompatible: false,
        codexCompatible: false,
        verification: "chat-only"
      }
    }
  ]);
  await act(async () => root.render(<AIServicePage />));
  expect(element.textContent).toContain("available for chat only");
  expect([...element.querySelectorAll("button")].some((button) => button.textContent === "Copy OpenAI client settings")).toBe(false);
});

it("builds OpenAI client settings from the ready model", () => {
  const settings = buildOpenAIClientSettings({
    origin: "https://appliance.example",
    modelId: "openai-community/gpt2",
    contextWindow: 1024
  });
  expect(settings.baseURL).toBe("https://appliance.example/inference/v1");
  expect(settings.modelId).toBe("openai-community/gpt2");
  expect(settings.providerToml).toContain('model = "openai-community/gpt2"');
  expect(settings.providerToml).toContain("model_context_window = 1024");
  expect(settings.providerToml).toContain('env_key = "INFERENCE_API_KEY"');
  expect(settings.catalogJson).toContain('"slug": "openai-community/gpt2"');
  expect(settings.catalogJson).toContain('"supported_reasoning_levels"');
  expect(settings.catalogJson).toContain('"effort": "medium"');
  expect(settings.catalogJson).toContain('"shell_type": "shell_command"');
  expect(settings.catalogJson).toContain('"apply_patch_tool_type": "freeform"');
  expect(settings.catalogJson).toContain('"context_window": 1024');
  expect(settings.catalogJson).toContain('"limit": 512');
  const parsed = JSON.parse(settings.catalogJson) as {
    models: Array<Record<string, unknown>>;
  };
  expect(parsed.models).toHaveLength(1);
  expect(parsed.models[0].base_instructions).toEqual(expect.any(String));
  expect(parsed.models[0].minimal_client_version).toBe("0.130.0");
  expect(parsed.models[0].supports_search_tool).toBe(false);
});

it("rewrites appliance.internal origins to the mDNS .local name for client copy", () => {
  const settings = buildOpenAIClientSettings({
    origin: "https://big-machine.appliance.internal",
    modelId: "Qwen/Qwen2.5-3B-Instruct",
    contextWindow: 32768
  });
  expect(settings.baseURL).toBe("https://big-machine.local/inference/v1");
  expect(settings.providerToml).toContain('base_url = "https://big-machine.local/inference/v1"');
  expect(settings.instructions).toContain("Base URL: https://big-machine.local/inference/v1");
});

it("omits a fake context window when the loaded model does not report one", () => {
  const settings = buildOpenAIClientSettings({
    origin: "https://appliance.example",
    modelId: "org/chat-model"
  });
  expect(settings.providerToml).toContain("# model_context_window = <reload the model, then copy again>");
  expect(settings.catalogJson).not.toContain('"context_window"');
  expect(settings.catalogJson).toContain('"supported_reasoning_levels"');
  expect(settings.contextWindow).toBeUndefined();
});

it("polls enable progress while an async load runs", async () => {
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
      acceleration: "accelerated",
      gpuAvailable: true,
      ready: true,
      servingState: "inactive"
    })
    .mockResolvedValue({
      engine: "vllm",
      architecture: "amd64",
      acceleration: "accelerated",
      gpuAvailable: true,
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
  const load = [...element.querySelectorAll("button")].find((button) => button.textContent === "Enable");
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
  ).toBe("~0.5B params · download 0.9 GiB · est. model 5.8 GiB");
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
