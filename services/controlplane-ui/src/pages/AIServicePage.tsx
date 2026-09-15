import React, { useEffect, useState } from "react";
import { Button, Card, EmptyState, PageFrame } from "../components";
import { client } from "../lib/api";
import { navigate } from "../lib/navigate";
import type { InferenceModel, InferenceRuntimeStatus } from "../types";

export function AIServicePage(): React.JSX.Element {
  const [status, setStatus] = useState<InferenceRuntimeStatus | null>(null);
  const [models, setModels] = useState<InferenceModel[]>([]);
  const [modelId, setModelId] = useState("");
  const [source, setSource] = useState("");
  const [digest, setDigest] = useState("");
  const [launchArguments, setLaunchArguments] = useState("");
  const [busy, setBusy] = useState("");
  const [message, setMessage] = useState("");
  const [error, setError] = useState("");

  async function refresh() {
    try {
      const [nextStatus, nextModels] = await Promise.all([
        client.getInferenceStatus(),
        client.listInferenceModels()
      ]);
      setStatus(nextStatus);
      setModels(nextModels);
      setError("");
    } catch (err) {
      setError(err instanceof Error ? err.message : "Could not reach the inference runtime.");
    }
  }

  useEffect(() => { void refresh(); }, []);

  async function run(label: string, operation: () => Promise<void>, success: string) {
    setBusy(label); setError(""); setMessage("");
    try {
      await operation();
      await refresh();
      setMessage(success);
    } catch (err) {
      setError(err instanceof Error ? err.message : "The inference operation failed.");
    } finally {
      setBusy("");
    }
  }

  async function addModel(event: React.FormEvent) {
    event.preventDefault();
	const resolvedModelId = status?.engine === "ollama" ? source.trim() : modelId.trim();
    await run("import", () => client.importInferenceModel({
      modelId: resolvedModelId,
      source: source.trim(),
      digest: status?.engine === "ollama" ? undefined : digest.trim() || undefined,
      launchArguments: status?.engine === "vllm" ? launchArguments.split("\n").map((value) => value.trim()).filter(Boolean) : undefined
    }), `Model ${resolvedModelId} was added.`);
    setModelId(""); setSource(""); setDigest(""); setLaunchArguments("");
  }

  return (
    <PageFrame title="AI Services" eyebrow="Admin" description="Manage the installed inference runtime and its local models." pathname="/admin/ai-services" onNavigate={navigate} tabs={[]}>
      <div className="stack">
        {error ? <div className="message message--error">{error}</div> : null}
        {message ? <div className="message">{message}</div> : null}
        <Card title="Inference runtime" subtitle="Runtime mode is detected from capabilities; hardware vendors and individual model names are not hardcoded.">
          {status ? <div className="detail-list">
            <div><span>Runtime</span><strong>{status.engine} · {status.package}</strong></div>
            <div><span>Architecture</span><strong>{status.architecture} (host {status.hostArchitecture})</strong></div>
            <div><span>Mode</span><strong>{status.activeMode || `${status.requestedMode} — detection pending`}</strong></div>
            <div><span>OpenAI API</span><strong>{status.ready ? "Ready at /ai/v1" : "Not ready"}</strong></div>
          </div> : <EmptyState message="Inference runtime status is unavailable." />}
        </Card>
        <Card title="Add model" subtitle="Connect the appliance temporarily, enter an engine-supported repository reference, verify it when a digest is available, then disconnect again.">
          <form className="stack" onSubmit={(event) => void addModel(event)}>
            {status?.engine !== "ollama" ? <label>Model ID<input required value={modelId} onChange={(event) => setModelId(event.target.value)} placeholder="local-name" /></label> : null}
            <label>Repository or model reference<input required value={source} onChange={(event) => setSource(event.target.value)} placeholder="engine-supported source" /></label>
			{status?.engine !== "ollama" ? <label>Expected digest (recommended)<input value={digest} onChange={(event) => setDigest(event.target.value)} placeholder="sha256:…" /></label> : <p className="m-0 text-sm leading-6 text-slate-600">Ollama verifies its content-addressed download. Its source reference is also the installed model ID.</p>}
            {status?.engine === "vllm" ? <label>vLLM launch arguments<textarea rows={9} value={launchArguments} onChange={(event) => setLaunchArguments(event.target.value)} placeholder={"--quantization\nmodelopt_fp4\n--max-model-len\n262144\n--gpu-memory-utilization\n0.8\n--cudagraph-capture-sizes\n4\n--no-enable-flashinfer-autotune\n--enable-auto-tool-choice\n--served-model-name\nqwen3.6\n--tool-call-parser\nqwen3_coder"} /></label> : null}
            <div className="button-row"><Button type="submit" disabled={busy !== ""}>Download and add</Button></div>
          </form>
        </Card>
        <Card title="Installed models" subtitle="Models listed by the active engine through its OpenAI-compatible endpoint.">
          {models.length === 0 ? <EmptyState message="No models are installed." /> : <div className="stack">
            {models.map((model) => <div className="detail-list" key={model.id}>
              <div><span>Model</span><strong>{model.id}</strong></div>
              <div><span>Provider</span><strong>{model.ownedBy || status?.engine || "Local"}</strong></div>
              <div className="button-row">
                <Button type="button" disabled={busy !== ""} onClick={() => void run(model.id, () => client.loadInferenceModel(model.id), `${model.id} is loaded.`)}>Load</Button>
                <Button type="button" variant="ghost" disabled={busy !== ""} onClick={() => void run(model.id, () => client.deleteInferenceModel(model.id), `${model.id} was removed.`)}>Remove</Button>
              </div>
            </div>)}
          </div>}
        </Card>
      </div>
    </PageFrame>
  );
}
