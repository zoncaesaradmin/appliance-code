import React, { useEffect, useState } from "react";
import { Button, Card, EmptyState, PageFrame } from "../components";
import { client } from "../lib/api";
import { navigate } from "../lib/navigate";
import type { InferenceCatalog, InferenceCatalogEntry, InferenceModel, InferenceRuntimeStatus } from "../types";

const size = (bytes: number): string => `${(bytes / 1024 ** 3).toFixed(1)} GiB`;

export function AIServicePage(): React.JSX.Element {
  const [status, setStatus] = useState<InferenceRuntimeStatus | null>(null);
  const [models, setModels] = useState<InferenceModel[]>([]);
  const [catalog, setCatalog] = useState<InferenceCatalog | null>(null);
  const [catalogError, setCatalogError] = useState("");
  const [search, setSearch] = useState("");
  const [filter, setFilter] = useState("all");
  const [busy, setBusy] = useState("");
  const [message, setMessage] = useState("");
  const [error, setError] = useState("");

  async function refresh() {
    const [runtime, installed, available] = await Promise.allSettled([
      client.getInferenceStatus(), client.listInferenceModels(), client.getInferenceCatalog()
    ]);
    if (runtime.status === "fulfilled") setStatus(runtime.value);
    if (installed.status === "fulfilled") setModels(installed.value);
    else setError("Could not refresh downloaded models. Displayed download status may be outdated.");
    if (available.status === "fulfilled") { setCatalog(available.value); setCatalogError(""); }
    else setCatalogError("Model discovery is unavailable. Downloaded models remain accessible.");
  }

  useEffect(() => {
    void refresh();
    // Reads the local cache; never triggers upstream discovery.
    const timer = window.setInterval(() => void refresh(), 30000);
    return () => window.clearInterval(timer);
  }, []);

  async function run(label: string, operation: () => Promise<void>, success: string) {
    setBusy(label); setError(""); setMessage("");
    try { await operation(); setMessage(success); await refresh(); }
    catch (err) { setError(err instanceof Error ? err.message : "The inference operation failed."); }
    finally { setBusy(""); }
  }

  const downloaded = new Set(models.map((model) => model.id));
  const entries = new Map<string, InferenceCatalogEntry>();
  for (const entry of catalog?.items ?? []) {
    if (entry.eligible || downloaded.has(entry.id)) entries.set(entry.id, entry);
  }
  for (const model of models) {
    if (!entries.has(model.id)) entries.set(model.id, { id: model.id, source: model.id, downloadBytes: 0, memoryBytes: 0, eligible: false, reason: "Not in the current eligible catalog" });
  }
  const visible = [...entries.values()].filter((entry) => entry.id.toLowerCase().includes(search.toLowerCase()) &&
    (filter !== "downloaded" || downloaded.has(entry.id)) && (filter !== "available" || !downloaded.has(entry.id)))
    .sort((a, b) => Number(downloaded.has(b.id)) - Number(downloaded.has(a.id)) || a.id.localeCompare(b.id));
  const lastSuccess = catalog?.lastSuccess && !catalog.lastSuccess.startsWith("0001-") ? new Date(catalog.lastSuccess).toLocaleString() : "Not yet refreshed";

  return (
    <PageFrame title="AI Services" eyebrow="Admin" description="Find models for this appliance and manage your downloads." pathname="/admin/ai-services" onNavigate={navigate} tabs={[]}>
      <div className="stack">
        {error ? <div className="message message--error" role="alert">{error}</div> : null}
        {message ? <div className="message" role="status">{message}</div> : null}
        <Card title="Inference runtime" subtitle="The active engine and automatically selected mode.">
          {status ? <div className="detail-list">
            <div><span>Runtime</span><strong>{status.engine} · {status.architecture}</strong></div>
            <div><span>Mode</span><strong>{status.activeMode || "Detection pending"}</strong></div>
            <div><span>Service</span><strong>{status.ready ? "Available" : "Not ready"}</strong></div>
          </div> : <EmptyState message="Inference runtime status is unavailable." />}
        </Card>
        <Card title="Models" subtitle="Candidates are filtered for current capacity. Actual compatibility is verified when loaded. Downloaded models stay available offline.">
          <p className="text-sm text-slate-600" role="status">Catalog: {catalog?.refreshing ? "Refreshing…" : catalog?.stale ? "Cached / refresh overdue" : "Up to date"} · Last successful refresh: {lastSuccess}. Automatically checks every 24 hours.</p>
          {catalogError || catalog?.lastError ? <p className="message message--error">{catalogError || catalog?.lastError}</p> : null}
          <div className="flex flex-wrap items-end gap-3">
            <label className="flex flex-col gap-1">Search models<input className="rounded-lg border border-slate-300 px-3 py-2" value={search} onChange={(event) => setSearch(event.target.value)} placeholder="Search by model name" /></label>
            <label className="flex flex-col gap-1">Show<select className="rounded-lg border border-slate-300 px-3 py-2" value={filter} onChange={(event) => setFilter(event.target.value)}><option value="all">Supported &amp; downloaded</option><option value="downloaded">Downloaded</option><option value="available">Available to download</option></select></label>
          </div>
          {visible.length === 0 ? <EmptyState message={catalog?.refreshing ? "Discovering models. Downloaded models appear here independently." : "No models match this view. Discovery needs internet; cached and downloaded models remain available offline."} /> : <div className="mt-4 overflow-x-auto">
            <table className="w-full text-left text-sm">
              <thead><tr className="border-b border-slate-200"><th className="p-3">Model</th><th className="p-3">Compatibility</th><th className="p-3">Download</th><th className="p-3">Actions</th></tr></thead>
              <tbody>{visible.map((entry) => <tr key={entry.id} className="border-b border-slate-100">
                <td className="p-3 font-medium">{entry.id}{entry.downloadBytes > 0 ? <div className="font-normal text-slate-500">{size(entry.downloadBytes)} download · {size(entry.memoryBytes)} estimated memory</div> : null}</td>
                <td className="p-3" title={entry.reason}>{entry.eligible ? "Eligible · estimated fit" : "Not currently eligible"}</td>
                <td className="p-3">{downloaded.has(entry.id) ? "✓ Downloaded" : "↓ Not downloaded"}</td>
                <td className="p-3"><div className="button-row">{downloaded.has(entry.id) ? <>
                  <Button type="button" disabled={busy !== ""} onClick={() => void run(`load:${entry.id}`, () => client.loadInferenceModel(entry.id), `${entry.id} loaded successfully.`)}>{busy === `load:${entry.id}` ? "Loading…" : "Load"}</Button>
                  <Button type="button" variant="ghost" disabled={busy !== ""} onClick={() => void run(`remove:${entry.id}`, () => client.deleteInferenceModel(entry.id), `${entry.id} was removed.`)}>Remove</Button>
                </> : <Button type="button" disabled={busy !== "" || !entry.eligible} onClick={() => void run(`download:${entry.id}`, () => client.importInferenceModel({ catalogId: entry.id, modelId: entry.id, source: entry.source }), `${entry.id} downloaded. Load it to verify runtime compatibility.`)}>{busy === `download:${entry.id}` ? "Downloading…" : "Download now"}</Button>}</div></td>
              </tr>)}</tbody>
            </table>
          </div>}
          <p className="text-sm text-slate-500">{catalog?.scope || "Popular upstream models; not an exhaustive library."} Internet is required to download new models.</p>
        </Card>
      </div>
    </PageFrame>
  );
}
