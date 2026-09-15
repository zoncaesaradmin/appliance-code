import React, { useEffect, useMemo, useState } from "react";
import { Button, Card, EmptyState, PageFrame } from "../components";
import { client } from "../lib/api";
import { navigate } from "../lib/navigate";
import type { InferenceCatalog, InferenceCatalogEntry, InferenceModel, InferenceRuntimeStatus } from "../types";

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
              ) : null}
            </div>
          )}
        </Card>
      </div>
    </PageFrame>
  );
}
