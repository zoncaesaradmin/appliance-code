import React, { useEffect, useMemo, useRef, useState } from "react";
import { Button, Card, EmptyState, PageFrame } from "../components";
import { client } from "../lib/api";
import { navigate } from "../lib/navigate";
import type {
  InferenceCatalog,
  InferenceCatalogEntry,
  InferenceImportProgress,
  InferenceLoadProgress,
  InferenceModel,
  InferenceRuntimeStatus
} from "../types";

const PARAM_HINT = /(\d+(?:\.\d+)?)\s*([MmBb])(?:[-_]|\b)/;
const MODELS_REFRESH_ERROR = "Could not refresh downloaded models. Displayed download status may be outdated.";
const IMPORT_POLL_MS = 2000;

function formatGiB(bytes: number): string {
  if (bytes <= 0) {
    return "unknown";
  }
  const value = bytes / 1024 ** 3;
  return `${value >= 10 ? value.toFixed(0) : value.toFixed(1)} GiB`;
}

function formatParamCount(params: number): string {
  if (params >= 1e9) {
    const billions = params / 1e9;
    return `${billions >= 10 ? billions.toFixed(0) : billions.toFixed(1)}B`;
  }
  if (params >= 1e6) {
    const millions = params / 1e6;
    return `${millions >= 10 ? millions.toFixed(0) : millions.toFixed(1)}M`;
  }
  return `${Math.max(1, Math.round(params))}`;
}

/** Estimated parameter/scale rank used for catalog sorting (matches manager). */
export function catalogEntrySizeRank(entry: InferenceCatalogEntry): number {
  const named = entry.id.match(PARAM_HINT);
  if (named) {
    const amount = Number(named[1]);
    const unit = named[2].toUpperCase();
    if (Number.isFinite(amount) && amount > 0) {
      return amount * (unit === "B" ? 1e9 : 1e6);
    }
  }
  if (entry.downloadBytes > 0) {
    return entry.downloadBytes / 2;
  }
  return entry.memoryBytes;
}

export function sortCatalogEntries(
  entries: InferenceCatalogEntry[],
  sort: "parameters" | "memory" | "name" = "parameters",
  order: "asc" | "desc" = "desc"
): InferenceCatalogEntry[] {
  const descending = order === "desc";
  return [...entries].sort((left, right) => {
    let cmp = 0;
    if (sort === "memory") {
      cmp = left.memoryBytes - right.memoryBytes;
    } else if (sort === "name") {
      cmp = left.id.localeCompare(right.id);
    } else {
      cmp = catalogEntrySizeRank(left) - catalogEntrySizeRank(right);
    }
    if (cmp !== 0) {
      return descending ? -cmp : cmp;
    }
    return left.id.localeCompare(right.id);
  });
}

function importInFlight(state: InferenceImportProgress["state"] | undefined): boolean {
  return state === "downloading" || state === "verifying" || state === "installing";
}

function loadInFlight(state: InferenceLoadProgress["state"] | undefined): boolean {
  return state === "loading";
}

function progressLabel(progress: InferenceImportProgress): string {
  if (progress.message) {
    return progress.message;
  }
  switch (progress.state) {
    case "downloading":
      return "Downloading model files";
    case "verifying":
      return "Verifying downloaded model";
    case "installing":
      return "Installing model";
    case "complete":
      return "Model downloaded";
    case "failed":
      return progress.error || "Download failed";
    default:
      return "Preparing download";
  }
}

export function servingStatusLabel(status: InferenceRuntimeStatus): string {
  switch (status.servingState) {
    case "loading":
      return status.loadedModelId ? `Loading… (${status.loadedModelId})` : "Loading…";
    case "ready":
      return status.loadedModelId ? `Ready for use (${status.loadedModelId})` : "Ready for use";
    case "failed":
      return status.loadedModelId ? `Load failed (${status.loadedModelId})` : "Load failed";
    default:
      return "Inactive";
  }
}

/** Origin used for OpenAI-compatible client config (no trailing slash).
 * Prefer the LAN mDNS name (<appliance-name>.local) over the internal DNS
 * zone (*.appliance.internal) so copied settings work from operator laptops
 * that resolve mDNS but may not use the appliance.internal zone. */
export function inferenceClientOrigin(canonicalOrigin?: string, fallbackOrigin?: string): string {
  const raw = (canonicalOrigin || fallbackOrigin || "").trim().replace(/\/$/, "");
  return rewriteApplianceInternalOriginToLocal(raw);
}

/** Map https://name.appliance.internal[:port] → https://name.local[:port]. */
export function rewriteApplianceInternalOriginToLocal(origin: string): string {
  const raw = origin.trim().replace(/\/$/, "");
  if (!raw) {
    return raw;
  }
  try {
    const parsed = new URL(raw);
    const host = parsed.hostname.toLowerCase();
    const suffix = ".appliance.internal";
    if (host.endsWith(suffix) && host.length > suffix.length) {
      const applianceName = host.slice(0, -suffix.length);
      if (applianceName && !applianceName.includes(".")) {
        parsed.hostname = `${applianceName}.local`;
        // URL#toString keeps a trailing slash for bare origins; strip it.
        return parsed.toString().replace(/\/$/, "");
      }
    }
  } catch {
    // Fall through and return the original string when URL parsing fails.
  }
  return raw;
}

/** OpenAI-compatible base URL published by the appliance Traefik route. */
export function inferenceOpenAIBaseURL(origin: string): string {
  return `${inferenceClientOrigin(origin)}/inference/v1`;
}

export function contextWindowFromLaunchArguments(argumentsList?: string[]): number | undefined {
  if (!argumentsList) {
    return undefined;
  }
  for (let index = 0; index + 1 < argumentsList.length; index++) {
    if (argumentsList[index] !== "--max-model-len") {
      continue;
    }
    const parsed = Number(argumentsList[index + 1]);
    if (Number.isFinite(parsed) && parsed > 0) {
      return Math.trunc(parsed);
    }
  }
  return undefined;
}

export type OpenAIClientSettings = {
  baseURL: string;
  modelId: string;
  contextWindow?: number;
  providerToml: string;
  catalogJson: string;
  instructions: string;
  copyAll: string;
};

/** Build copyable OpenAI / Codex client settings from the currently ready model. */
export function buildOpenAIClientSettings(input: {
  origin: string;
  modelId: string;
  contextWindow?: number;
}): OpenAIClientSettings {
  const baseURL = inferenceOpenAIBaseURL(input.origin);
  const modelId = input.modelId.trim();
  const knownWindow = input.contextWindow && input.contextWindow > 0 ? input.contextWindow : undefined;
  // Prefer the appliance-served window. When unknown, omit a fake default so
  // clients are not told 8192 while vLLM is still running at something else.
  const contextWindow = knownWindow ?? 0;
  const truncationLimit =
    contextWindow > 0 ? Math.max(256, Math.min(10000, Math.floor(contextWindow / 2))) : 4096;
  const contextWindowLine =
    contextWindow > 0
      ? `model_context_window = ${contextWindow}`
      : `# model_context_window = <reload the model, then copy again>`;
  const providerToml = `model = "${modelId}"
model_provider = "appliance"
${contextWindowLine}
model_catalog_json = "/path/to/zon_model_catalog.json"

[model_providers.appliance]
name = "ZON appliance"
base_url = "${baseURL}"
env_key = "INFERENCE_API_KEY"
wire_api = "responses"`;

  // Codex 0.154+ requires a full catalog entry shape (not a minimal slug map).
  // Keep this aligned with a known-working local vLLM profile catalog.
  const catalogModel: Record<string, unknown> = {
    slug: modelId,
    display_name: modelId,
    description: `Model currently ready on the ZON appliance (${modelId}).`,
    base_instructions: `You are Codex, a coding agent using ${modelId} on the ZON appliance. You and the user share the same workspace. Help the user inspect, modify, test, and understand code. Use the available tools to inspect files, edit code, run commands, and verify changes. Be concise, accurate, and pragmatic.`,
    default_reasoning_level: "medium",
    supported_reasoning_levels: [
      {
        effort: "medium",
        description: "Default reasoning level"
      }
    ],
    shell_type: "shell_command",
    visibility: "list",
    supported_in_api: true,
    priority: 100,
    minimal_client_version: "0.130.0",
    availability_nux: null,
    upgrade: null,
    support_verbosity: false,
    default_verbosity: null,
    apply_patch_tool_type: "freeform",
    web_search_tool_type: "text",
    input_modalities: ["text"],
    supports_image_detail_original: false,
    truncation_policy: {
      mode: "tokens",
      limit: truncationLimit
    },
    supports_parallel_tool_calls: true,
    auto_compact_token_limit: null,
    reasoning_summary_format: "none",
    default_reasoning_summary: "none",
    experimental_supported_tools: [],
    available_in_plans: [],
    supports_search_tool: false,
    supports_reasoning_summaries: false
  };
  if (contextWindow > 0) {
    catalogModel.context_window = contextWindow;
  }
  const catalogJson = JSON.stringify({ models: [catalogModel] }, null, 2);

  const instructions = [
    "Copy this into your OpenAI-compatible client.",
    "",
    "1. Provider / profile config (for example ~/.codex/zon.config.toml):",
    "   - Set base_url to the value below.",
    "   - Set model to the served model id below.",
    "   - Point model_catalog_json at a separate catalog file.",
    "   - Put an appliance API token in the env var named by env_key (needs inference.use).",
    "",
    "2. Model catalog file (for example ~/.codex/zon_model_catalog.json):",
    "   - Paste the JSON catalog below (complete Codex catalog entry).",
    "   - Keep the slug equal to the served model id.",
    "",
    "3. After you enable a different model on the appliance, copy these settings again.",
    "   Context window matches the loaded engine --max-model-len (served window, not the raw model card).",
    "",
    `Base URL: ${baseURL}`,
    `Model: ${modelId}`,
    `Context window: ${
      contextWindow > 0
        ? String(contextWindow)
        : "unknown until Load refreshes launch settings — reload the model, then copy again"
    }`
  ].join("\n");

  const copyAll = [instructions, "", "--- provider config ---", providerToml, "", "--- model catalog ---", catalogJson].join(
    "\n"
  );

  return {
    baseURL,
    modelId,
    contextWindow: contextWindow > 0 ? contextWindow : undefined,
    providerToml,
    catalogJson,
    instructions,
    copyAll
  };
}

/** Best-effort size line for the selected catalog entry. */
export function modelCapacitySummary(entry: InferenceCatalogEntry, limit = 120): string {
  const parts: string[] = [];
  const named = entry.id.match(PARAM_HINT);
  if (named) {
    const amount = Number(named[1]);
    const unit = named[2].toUpperCase();
    if (Number.isFinite(amount) && amount > 0) {
      parts.push(`~${amount % 1 === 0 ? amount.toFixed(0) : amount}${unit} params`);
    }
  } else if (entry.downloadBytes > 0) {
    // FP16 weights are roughly 2 bytes/parameter; download size is a usable stand-in.
    parts.push(`~${formatParamCount(entry.downloadBytes / 2)} params (est.)`);
  }
  if (entry.downloadBytes > 0) {
    parts.push(`download ${formatGiB(entry.downloadBytes)}`);
  }
  if (entry.memoryBytes > 0) {
    parts.push(`est. model ${formatGiB(entry.memoryBytes)}`);
  }
  if (entry.requiredBytes && entry.requiredBytes > 0) {
    parts.push(`load needs ${formatGiB(entry.requiredBytes)}`);
  }
  if (parts.length === 0) {
    return "Capacity details unavailable for this model.";
  }
  const summary = parts.join(" · ");
  return summary.length <= limit ? summary : `${summary.slice(0, Math.max(0, limit - 1)).trimEnd()}…`;
}

export function AIServicePage(): React.JSX.Element {
  const [status, setStatus] = useState<InferenceRuntimeStatus | null>(null);
  const [models, setModels] = useState<InferenceModel[]>([]);
  const [catalog, setCatalog] = useState<InferenceCatalog | null>(null);
  const [catalogError, setCatalogError] = useState("");
  const [selectedId, setSelectedId] = useState("");
  const [busy, setBusy] = useState("");
  const [importProgress, setImportProgress] = useState<InferenceImportProgress | null>(null);
  const [loadProgress, setLoadProgress] = useState<InferenceLoadProgress | null>(null);
  const [message, setMessage] = useState("");
  const [error, setError] = useState("");
  const [clientOrigin, setClientOrigin] = useState("");
  const [showClientSettings, setShowClientSettings] = useState(false);
  const [copiedKey, setCopiedKey] = useState("");
  const busyRef = useRef("");
  busyRef.current = busy;

  async function refresh() {
    const [runtime, installed, available, identity] = await Promise.allSettled([
      client.getInferenceStatus(),
      client.listInferenceModels(),
      client.getInferenceCatalog({ sort: "parameters", order: "desc" }),
      client.getIdentity()
    ]);
    if (runtime.status === "fulfilled") {
      setStatus(runtime.value);
    }
    if (installed.status === "fulfilled") {
      setModels(installed.value);
      setError((current) => (current === MODELS_REFRESH_ERROR ? "" : current));
    } else if (!busyRef.current) {
      // Avoid noisy stale errors while download/load holds the runtime write lock.
      setError(MODELS_REFRESH_ERROR);
    }
    if (available.status === "fulfilled") {
      setCatalog(available.value);
      setCatalogError("");
    } else {
      setCatalogError("Model discovery is unavailable. Downloaded models remain accessible.");
    }
    if (identity.status === "fulfilled") {
      setClientOrigin(inferenceClientOrigin(identity.value.canonicalOrigin, window.location.origin));
    } else {
      setClientOrigin((current) => current || inferenceClientOrigin(undefined, window.location.origin));
    }
  }

  useEffect(() => {
    void refresh();
    const timer = window.setInterval(() => {
      if (busyRef.current) {
        return;
      }
      void refresh();
    }, 30000);
    return () => window.clearInterval(timer);
  }, []);

  useEffect(() => {
    let cancelled = false;
    void (async () => {
      try {
        const [download, load] = await Promise.all([
          client.getInferenceImportProgress(),
          client.getInferenceLoadProgress()
        ]);
        if (cancelled) {
          return;
        }
        if (importInFlight(download.state)) {
          setImportProgress(download);
          setBusy(`download:${download.modelId || "model"}`);
          setSelectedId((current) => current || download.modelId || "");
          return;
        }
        if (loadInFlight(load.state)) {
          setLoadProgress(load);
          setBusy(`load:${load.modelId || "model"}`);
          setSelectedId((current) => current || load.modelId || "");
        }
      } catch {
        // Ignore resume failures; the operator can start a fresh action.
      }
    })();
    return () => {
      cancelled = true;
    };
  }, []);

  useEffect(() => {
    if (!importInFlight(importProgress?.state)) {
      return;
    }
    const timer = window.setInterval(() => {
      void (async () => {
        try {
          const progress = await client.getInferenceImportProgress();
          setImportProgress(progress);
          if (progress.state === "complete") {
            setBusy("");
            setMessage(`${progress.modelId || "Model"} downloaded. Enable it to verify runtime compatibility.`);
            await refresh();
            return;
          }
          if (progress.state === "failed") {
            setBusy("");
            setError(progress.error || "Model download failed.");
            return;
          }
          if (progress.state === "idle") {
            setBusy("");
            setImportProgress(null);
          }
        } catch (err) {
          setBusy("");
          setError(err instanceof Error ? err.message : "Could not read download progress.");
        }
      })();
    }, IMPORT_POLL_MS);
    return () => window.clearInterval(timer);
  }, [importProgress?.state]);

  useEffect(() => {
    if (!loadInFlight(loadProgress?.state)) {
      return;
    }
    const timer = window.setInterval(() => {
      void (async () => {
        try {
          const progress = await client.getInferenceLoadProgress();
          setLoadProgress(progress);
          if (progress.state === "ready") {
            setBusy("");
            setMessage(`${progress.modelId || "Model"} is ready for use.`);
            await refresh();
            return;
          }
          if (progress.state === "failed") {
            setBusy("");
            setError(progress.error || "Model load failed.");
            await refresh();
            return;
          }
          if (progress.state === "idle") {
            setBusy("");
            setLoadProgress(null);
          }
        } catch (err) {
          setBusy("");
          setError(err instanceof Error ? err.message : "Could not read load progress.");
        }
      })();
    }, IMPORT_POLL_MS);
    return () => window.clearInterval(timer);
  }, [loadProgress?.state]);

  async function run(label: string, operation: () => Promise<void>, success: string) {
    setBusy(label);
    setError("");
    setMessage("");
    try {
      await operation();
      setMessage(success);
      await refresh();
    } catch (err) {
      setError(err instanceof Error ? err.message : "The inference operation failed.");
    } finally {
      setBusy("");
    }
  }

  async function startDownload(entry: InferenceCatalogEntry) {
    setBusy(`download:${entry.id}`);
    setError("");
    setMessage("");
    try {
      const accepted = await client.importInferenceModel({
        catalogId: entry.id,
        modelId: entry.id,
        source: entry.source
      });
      setImportProgress(accepted);
      if (accepted.state === "complete") {
        setBusy("");
        setMessage(`${entry.id} downloaded. Enable it to verify runtime compatibility.`);
        await refresh();
      } else if (accepted.state === "failed") {
        setBusy("");
        setError(accepted.error || "Model download failed.");
      }
    } catch (err) {
      setBusy("");
      setImportProgress(null);
      setError(err instanceof Error ? err.message : "The inference operation failed.");
    }
  }

  async function startLoad(modelId: string) {
    setBusy(`load:${modelId}`);
    setError("");
    setMessage("");
    try {
      const accepted = await client.loadInferenceModel(modelId);
      setLoadProgress(accepted);
      if (accepted.state === "ready") {
        setBusy("");
        setMessage(`${modelId} is ready for use.`);
        await refresh();
      } else if (accepted.state === "failed") {
        setBusy("");
        setError(accepted.error || "Model load failed.");
        await refresh();
      }
    } catch (err) {
      setBusy("");
      setLoadProgress(null);
      setError(err instanceof Error ? err.message : "The inference operation failed.");
    }
  }

  const downloaded = useMemo(() => new Set(models.map((model) => model.id)), [models]);
  const options = useMemo(() => {
    const entries = new Map<string, InferenceCatalogEntry>();
    for (const entry of catalog?.items ?? []) {
      if (entry.eligible || downloaded.has(entry.id)) {
        entries.set(entry.id, entry);
      }
    }
    for (const model of models) {
      if (!entries.has(model.id)) {
        entries.set(model.id, {
          id: model.id,
          source: model.id,
          downloadBytes: 0,
          memoryBytes: 0,
          eligible: false,
          reason: "Not in the current eligible catalog"
        });
      }
    }
    return sortCatalogEntries(
      [...entries.values()],
      catalog?.sort ?? "parameters",
      catalog?.order ?? "desc"
    );
  }, [catalog?.items, catalog?.order, catalog?.sort, downloaded, models]);

  useEffect(() => {
    if (!selectedId && options.length > 0) {
      setSelectedId(options[0].id);
      return;
    }
    if (selectedId && !options.some((entry) => entry.id === selectedId)) {
      setSelectedId(options[0]?.id ?? "");
    }
  }, [options, selectedId]);

  const selected = options.find((entry) => entry.id === selectedId) ?? null;
  const selectedDownloaded = selected ? downloaded.has(selected.id) : false;
  const selectedSummary = selected ? modelCapacitySummary(selected) : "";
  const showProgress = importInFlight(importProgress?.state);
  const readyModelId = status?.servingState === "ready" ? status.loadedModelId?.trim() || "" : "";
  // Status instances are the source of truth. The fallback preserves a clear
  // display while talking to an older manager that only reports loadedModelId.
  const enabledInstances = useMemo(() => {
    const instances = (status?.instances ?? []).filter((instance) => instance.models.length > 0);
    if (instances.length > 0) {
      return instances;
    }
    return readyModelId ? [{ id: "default", models: [readyModelId], replicas: 1 }] : [];
  }, [readyModelId, status?.instances]);
  const enabledModelIDs = useMemo(
    () => new Set(enabledInstances.flatMap((instance) => instance.models)),
    [enabledInstances]
  );
  const selectedEnabled = !!selected && enabledModelIDs.has(selected.id);
  const loadBusy = busy.startsWith("load:");
  const loadButtonLabel = selectedEnabled
    ? "Enabled"
    : loadBusy
      ? "Enabling…"
      : enabledModelIDs.size > 0
        ? "Enable and replace"
        : "Enable";
  const readyContextWindow = useMemo(() => {
    if (!readyModelId) {
      return undefined;
    }
    // Prefer the live engine window from status. Catalog/inventory launch args
    // can still carry the model-card limit (e.g. 32768) after Load planned 8192.
    const fromStatus =
      status?.servingState === "ready" &&
      status.loadedModelId === readyModelId &&
      typeof status.maxModelLen === "number" &&
      status.maxModelLen > 0
        ? Math.trunc(status.maxModelLen)
        : undefined;
    if (fromStatus) {
      return fromStatus;
    }
    const installed = models.find((model) => model.id === readyModelId);
    const fromInstalled = contextWindowFromLaunchArguments(installed?.launchArguments);
    if (fromInstalled) {
      return fromInstalled;
    }
    const fromCatalog = catalog?.items?.find((entry) => entry.id === readyModelId);
    return contextWindowFromLaunchArguments(fromCatalog?.launchArguments);
  }, [catalog?.items, models, readyModelId, status?.loadedModelId, status?.maxModelLen, status?.servingState]);
  const clientSettings =
    readyModelId && clientOrigin
      ? buildOpenAIClientSettings({
          origin: clientOrigin,
          modelId: readyModelId,
          contextWindow: readyContextWindow
        })
      : null;

  async function copyText(key: string, value: string) {
    try {
      await navigator.clipboard.writeText(value);
      setCopiedKey(key);
      window.setTimeout(() => setCopiedKey((current) => (current === key ? "" : current)), 2000);
    } catch {
      setError("Could not copy to the clipboard. Select the text and copy it manually.");
    }
  }

  return (
    <PageFrame
      title="AI Services"
      eyebrow="Admin"
      description="Download models to this appliance, then enable one model for inference."
      pathname="/admin/ai-services"
      onNavigate={navigate}
      tabs={[]}
    >
      <div className="stack">
        {error ? (
          <div className="message message--error" role="alert">
            {error}
          </div>
        ) : null}
        {message ? (
          <div className="message" role="status">
            {message}
          </div>
        ) : null}
        <div className="ai-services-layout">
          <Card
            className="ai-services-layout__models"
            title="Model library"
            subtitle="Downloaded models are stored locally. Enabling a model replaces the current enabled model."
          >
            {catalogError || catalog?.lastError ? (
              <p className="message message--error">{catalogError || catalog?.lastError}</p>
            ) : null}
            {options.length === 0 ? (
              <EmptyState
                message={
                  catalog?.refreshing
                    ? "Discovering models…"
                    : (catalog?.items?.length ?? 0) > 0
                      ? "No catalog models currently fit this appliance's estimated memory or storage. Downloaded models still appear here."
                      : "No models are available yet. Discovery needs internet; downloaded models still appear here."
                }
              />
            ) : (
              <div className="stack">
                <section className="model-inventory" aria-labelledby="downloaded-models-title">
                  <div className="model-inventory__heading">
                    <div>
                      <h3 id="downloaded-models-title">Downloaded models</h3>
                      <p>Stored locally. You can enable one model at a time.</p>
                    </div>
                    <span className="pill">{models.length}</span>
                  </div>
                  {models.length > 0 ? (
                    <div className="model-inventory__list">
                      {models.map((model) => (
                        <button
                          className="model-inventory__item"
                          type="button"
                          key={model.id}
                          onClick={() => setSelectedId(model.id)}
                          disabled={busy !== ""}
                        >
                          <strong>{model.id}</strong>
                          <span className={enabledModelIDs.has(model.id) ? "pill pill--navy" : "pill"}>
                            {enabledModelIDs.has(model.id) ? "Downloaded · enabled" : "Downloaded"}
                          </span>
                        </button>
                      ))}
                    </div>
                  ) : (
                    <p className="model-inventory__empty">No models have been downloaded yet.</p>
                  )}
                </section>
                <label className="flex flex-col gap-1">
                  Choose a model
                  <select
                    className="rounded-lg border border-slate-300 px-3 py-2"
                    value={selectedId}
                    onChange={(event) => setSelectedId(event.target.value)}
                    aria-label="Select model"
                    disabled={busy !== ""}
                  >
                    {options.map((entry) => (
                      <option key={entry.id} value={entry.id}>
                        {enabledModelIDs.has(entry.id)
                          ? `${entry.id} (downloaded · enabled)`
                          : downloaded.has(entry.id)
                            ? `${entry.id} (downloaded)`
                            : entry.id}
                      </option>
                    ))}
                  </select>
                </label>
                {selected ? (
                  <>
                    <p className="text-sm text-slate-600" role="status" aria-live="polite">
                      {selectedSummary}
                    </p>
                    {showProgress && importProgress ? (
                      <div className="import-progress" role="status" aria-live="polite">
                        <div className="import-progress__label">{progressLabel(importProgress)}</div>
                        {typeof importProgress.percent === "number" ? (
                          <progress className="import-progress__bar" max={100} value={importProgress.percent} />
                        ) : (
                          <progress className="import-progress__bar" max={100} />
                        )}
                        <div className="import-progress__detail">
                          {typeof importProgress.percent === "number"
                            ? `${importProgress.percent}%`
                            : "Progress updating…"}
                          {importProgress.bytesDownloaded || importProgress.bytesTotal
                            ? ` · ${formatGiB(importProgress.bytesDownloaded || 0)}${
                                importProgress.bytesTotal ? ` / ${formatGiB(importProgress.bytesTotal)}` : ""
                              }`
                            : ""}
                        </div>
                      </div>
                    ) : null}
                    {loadInFlight(loadProgress?.state) ? (
                      <div className="import-progress" role="status" aria-live="polite">
                        <div className="import-progress__label">
                          {loadProgress?.message || "Loading model into the inference engine"}
                        </div>
                        <progress className="import-progress__bar" max={100} />
                        <div className="import-progress__detail">
                          {loadProgress?.oomKilled
                            ? "Engine pod was OOMKilled"
                            : loadProgress?.enginePhase
                              ? `Engine pod phase: ${loadProgress.enginePhase}`
                              : "Waiting for the engine Deployment to become ready…"}
                        </div>
                      </div>
                    ) : null}
                    <div className="button-row">
                      {selectedDownloaded ? (
                        <>
                          <Button
                            type="button"
                            disabled={busy !== "" || selectedEnabled}
                            onClick={() => void startLoad(selected.id)}
                          >
                            {loadButtonLabel}
                          </Button>
                          <Button
                            type="button"
                            variant="ghost"
                            disabled={busy !== ""}
                            onClick={() =>
                              void run(`remove:${selected.id}`, () => client.deleteInferenceModel(selected.id), `${selected.id} was removed.`)
                            }
                          >
                            Remove
                          </Button>
                        </>
                      ) : (
                        <Button
                          type="button"
                          disabled={busy !== "" || !selected.eligible}
                          onClick={() => void startDownload(selected)}
                        >
                          {busy === `download:${selected.id}` ? "Downloading…" : "Download"}
                        </Button>
                      )}
                    </div>
                  </>
                ) : null}
              </div>
            )}
          </Card>
          <Card
            className="ai-services-layout__status"
            title="Enabled model"
            subtitle="Alpha supports one enabled model at a time. Enabling another replaces it."
          >
            {status ? (
              <div className="stack">
                <div className="detail-list">
                  <div>
                    <span>Runtime</span>
                    <strong>
                      {status.engine} · {status.architecture}
                    </strong>
                  </div>
                  <div>
                    <span>Acceleration</span>
                    <strong>
                      {status.acceleration || "unknown"}
                      {typeof status.gpuAvailable === "boolean"
                        ? status.gpuAvailable
                          ? " · GPU available"
                          : " · GPU unavailable"
                        : ""}
                    </strong>
                  </div>
                  <div>
                    <span>Runtime status</span>
                    <strong>{servingStatusLabel(status)}</strong>
                  </div>
                </div>
                {enabledInstances.length > 0 ? (
                  <div className="enabled-model-list" aria-label="Enabled model">
                    {enabledInstances.map((instance) => (
                      <section className="enabled-model-list__instance" key={instance.id}>
                        <div className="enabled-model-list__heading">
                          <strong>{instance.id === "default" ? "Default instance" : instance.id}</strong>
                          <span>{instance.replicas} replica{instance.replicas === 1 ? "" : "s"}</span>
                        </div>
                        {instance.models.map((modelID) => (
                          <div className="enabled-model-list__model" key={modelID}>
                            <strong>{modelID}</strong>
                            <span className="pill pill--navy">Enabled</span>
                          </div>
                        ))}
                      </section>
                    ))}
                  </div>
                ) : (
                  <EmptyState message="No enabled model. Enable a downloaded model to make it available to clients." />
                )}
                {clientSettings ? (
                  <div className="button-row">
                    <Button type="button" variant="ghost" onClick={() => setShowClientSettings(true)}>
                      Copy OpenAI client settings
                    </Button>
                  </div>
                ) : null}
              </div>
            ) : (
              <EmptyState message="Inference runtime status is unavailable." />
            )}
          </Card>
        </div>
      </div>
      {showClientSettings && clientSettings ? (
        <div
          className="video-modal-scrim"
          role="presentation"
          onClick={() => setShowClientSettings(false)}
        >
          <div
            className="video-modal"
            role="dialog"
            aria-modal="true"
            aria-labelledby="openai-client-settings-title"
            onClick={(event) => event.stopPropagation()}
          >
            <h2 id="openai-client-settings-title">Copy OpenAI client settings</h2>
            <p>
              Use these values in your OpenAI-compatible client. Put the provider settings in one
              config file, and the model catalog JSON in a separate catalog file. Create an appliance
              API token with inference permissions and export it as <code>INFERENCE_API_KEY</code>.
            </p>
            <div className="stack">
              <div>
                <strong>Base URL</strong>
                <pre className="mt-2 overflow-x-auto rounded-lg bg-slate-100 p-3 text-xs">{clientSettings.baseURL}</pre>
              </div>
              <div>
                <strong>Model id</strong>
                <pre className="mt-2 overflow-x-auto rounded-lg bg-slate-100 p-3 text-xs">{clientSettings.modelId}</pre>
              </div>
              <div>
                <strong>Provider config</strong>
                <p className="video-modal__hint">
                  Example: paste into <code>~/.codex/zon.config.toml</code> and point{" "}
                  <code>model_catalog_json</code> at your catalog file.
                </p>
                <pre className="mt-2 max-h-48 overflow-auto rounded-lg bg-slate-100 p-3 text-xs whitespace-pre-wrap">
                  {clientSettings.providerToml}
                </pre>
                <div className="button-row mt-2">
                  <Button type="button" variant="ghost" onClick={() => void copyText("provider", clientSettings.providerToml)}>
                    {copiedKey === "provider" ? "Copied" : "Copy provider config"}
                  </Button>
                </div>
              </div>
              <div>
                <strong>Model catalog</strong>
                <p className="video-modal__hint">
                  Example: save as <code>~/.codex/zon_model_catalog.json</code>. Keep the slug equal
                  to the served model id.
                </p>
                <pre className="mt-2 max-h-48 overflow-auto rounded-lg bg-slate-100 p-3 text-xs whitespace-pre-wrap">
                  {clientSettings.catalogJson}
                </pre>
                <div className="button-row mt-2">
                  <Button type="button" variant="ghost" onClick={() => void copyText("catalog", clientSettings.catalogJson)}>
                    {copiedKey === "catalog" ? "Copied" : "Copy catalog JSON"}
                  </Button>
                </div>
              </div>
            </div>
            <div className="button-row mt-4">
              <Button type="button" onClick={() => void copyText("all", clientSettings.copyAll)}>
                {copiedKey === "all" ? "Copied" : "Copy all"}
              </Button>
              <Button type="button" variant="ghost" onClick={() => setShowClientSettings(false)}>
                Close
              </Button>
            </div>
          </div>
        </div>
      ) : null}
    </PageFrame>
  );
}
