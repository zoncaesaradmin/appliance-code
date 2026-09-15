import React, { useEffect, useMemo, useState } from "react";
import { Button, Card, EmptyState, PageFrame } from "../components";
import { client } from "../lib/api";
import { navigate } from "../lib/navigate";
import type { InferenceCatalog, InferenceCatalogEntry, InferenceModel, InferenceRuntimeStatus } from "../types";

const PARAM_HINT = /(\d+(?:\.\d+)?)\s*([MmBb])(?:[-_]|\b)/;

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
    parts.push(`est. RAM ${formatGiB(entry.memoryBytes)}`);
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
  const [message, setMessage] = useState("");
  const [error, setError] = useState("");

  async function refresh() {
    const [runtime, installed, available] = await Promise.allSettled([
      client.getInferenceStatus(),
      client.listInferenceModels(),
      client.getInferenceCatalog()
    ]);
    if (runtime.status === "fulfilled") {
      setStatus(runtime.value);
    }
    if (installed.status === "fulfilled") {
      setModels(installed.value);
    } else {
      setError("Could not refresh downloaded models. Displayed download status may be outdated.");
    }
    if (available.status === "fulfilled") {
      setCatalog(available.value);
      setCatalogError("");
    } else {
      setCatalogError("Model discovery is unavailable. Downloaded models remain accessible.");
    }
  }

  useEffect(() => {
    void refresh();
    const timer = window.setInterval(() => void refresh(), 30000);
    return () => window.clearInterval(timer);
  }, []);

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
    return [...entries.values()].sort(
      (a, b) => Number(downloaded.has(b.id)) - Number(downloaded.has(a.id)) || a.id.localeCompare(b.id)
    );
  }, [catalog?.items, downloaded, models]);

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

  return (
    <PageFrame
      title="AI Services"
      eyebrow="Admin"
      description="Select a model for this appliance, then download or load it."
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
        <Card title="Inference runtime" subtitle="The active engine and automatically selected mode.">
          {status ? (
            <div className="detail-list">
              <div>
                <span>Runtime</span>
                <strong>
                  {status.engine} · {status.architecture}
                </strong>
              </div>
              <div>
                <span>Mode</span>
                <strong>{status.activeMode || "Detection pending"}</strong>
              </div>
              <div>
                <span>Service</span>
                <strong>{status.ready ? "Available" : "Not ready"}</strong>
              </div>
            </div>
          ) : (
            <EmptyState message="Inference runtime status is unavailable." />
          )}
        </Card>
        <Card title="Models" subtitle="Models are discovered for this runtime and current capacity. Load verifies actual compatibility.">
          {catalogError || catalog?.lastError ? (
            <p className="message message--error">{catalogError || catalog?.lastError}</p>
          ) : null}
          {options.length === 0 ? (
            <EmptyState
              message={
                catalog?.refreshing
                  ? "Discovering models…"
                  : "No models are available yet. Discovery needs internet; downloaded models still appear here."
              }
            />
          ) : (
            <div className="stack">
              <label className="flex flex-col gap-1">
                Model
                <select
                  className="rounded-lg border border-slate-300 px-3 py-2"
                  value={selectedId}
                  onChange={(event) => setSelectedId(event.target.value)}
                  aria-label="Select model"
                >
                  {options.map((entry) => (
                    <option key={entry.id} value={entry.id}>
                      {downloaded.has(entry.id) ? `${entry.id} (downloaded)` : entry.id}
                    </option>
                  ))}
                </select>
              </label>
              {selected ? (
                <>
                  <p className="text-sm text-slate-600" role="status" aria-live="polite">
                    {selectedSummary}
                  </p>
                  <div className="button-row">
                    {selectedDownloaded ? (
                      <>
                        <Button
                          type="button"
                          disabled={busy !== ""}
                          onClick={() =>
                            void run(`load:${selected.id}`, () => client.loadInferenceModel(selected.id), `${selected.id} loaded successfully.`)
                          }
                        >
                          {busy === `load:${selected.id}` ? "Loading…" : "Load"}
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
                        onClick={() =>
                          void run(
                            `download:${selected.id}`,
                            () =>
                              client.importInferenceModel({
                                catalogId: selected.id,
                                modelId: selected.id,
                                source: selected.source
                              }),
                            `${selected.id} downloaded. Load it to verify runtime compatibility.`
                          )
                        }
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
      </div>
    </PageFrame>
  );
}
