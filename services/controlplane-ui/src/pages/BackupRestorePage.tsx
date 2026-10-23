import React, { useRef, useState } from "react";
import { Card, PageFrame } from "../components";
import { client } from "../lib/api";
import { navigate } from "../lib/navigate";

const format = "configuration-archive.zon/v1";
type Manifest = { format: string; archiveId: string; createdAt: string; revisionId: string; schemaVersion: string; payloadDigest: string; keyId: string };
type Envelope = { manifest: Manifest; nonce: string; ciphertext: string };
const text = new TextEncoder();
const b64 = (bytes: Uint8Array) => btoa(String.fromCharCode(...bytes));
const unb64 = (value: string) => Uint8Array.from(atob(value), c => c.charCodeAt(0));
const hex = (bytes: Uint8Array) => Array.from(bytes, b => b.toString(16).padStart(2, "0")).join("");
const buffer = (bytes: Uint8Array): ArrayBuffer => bytes.buffer.slice(bytes.byteOffset, bytes.byteOffset + bytes.byteLength) as ArrayBuffer;
const hash = async (bytes: Uint8Array) => new Uint8Array(await crypto.subtle.digest("SHA-256", buffer(bytes)));

async function key(file: File): Promise<Uint8Array> {
  const raw = new Uint8Array(await file.arrayBuffer());
  const parsed = raw.length === 32 ? raw : unb64(new TextDecoder().decode(raw).trim());
  if (parsed.length !== 32) throw new Error("Archive key must be 32 raw bytes or base64 for 32 bytes.");
  return parsed;
}
async function seal(payload: unknown, revisionId: string, schemaVersion: string, material: Uint8Array): Promise<Envelope> {
  const payloadBytes = text.encode(JSON.stringify(payload));
  const manifest: Manifest = { format, archiveId: `archive-${crypto.randomUUID().replaceAll("-", "")}`, createdAt: new Date().toISOString().replace(".000Z", "Z"), revisionId, schemaVersion, payloadDigest: `sha256:${hex(await hash(payloadBytes))}`, keyId: hex((await hash(material)).slice(0, 8)) };
  const nonce = crypto.getRandomValues(new Uint8Array(12));
  const cryptoKey = await crypto.subtle.importKey("raw", buffer(material), "AES-GCM", false, ["encrypt"]);
  const ciphertext = new Uint8Array(await crypto.subtle.encrypt({ name: "AES-GCM", iv: nonce, additionalData: text.encode(JSON.stringify(manifest)) }, cryptoKey, payloadBytes));
  return { manifest, nonce: b64(nonce), ciphertext: b64(ciphertext) };
}
async function open(file: File, material: Uint8Array): Promise<unknown> {
  const value = JSON.parse(await file.text()) as Envelope;
  if (!value?.manifest || value.manifest.format !== format) throw new Error("Unsupported configuration archive.");
  if (value.manifest.keyId !== hex((await hash(material)).slice(0, 8))) throw new Error("Selected key does not match the archive.");
  const cryptoKey = await crypto.subtle.importKey("raw", buffer(material), "AES-GCM", false, ["decrypt"]);
  const payload = new Uint8Array(await crypto.subtle.decrypt({ name: "AES-GCM", iv: unb64(value.nonce), additionalData: text.encode(JSON.stringify(value.manifest)) }, cryptoKey, unb64(value.ciphertext)));
  if (`sha256:${hex(await hash(payload))}` !== value.manifest.payloadDigest) throw new Error("Configuration archive digest does not match.");
  return JSON.parse(new TextDecoder().decode(payload));
}

export function BackupRestorePage(props: { pathname: string }): React.JSX.Element {
  const exportKey = useRef<HTMLInputElement>(null), archive = useRef<HTMLInputElement>(null), importKey = useRef<HTMLInputElement>(null);
  const [working, setWorking] = useState(false), [error, setError] = useState(""), [message, setMessage] = useState("");
  const exportConfig = async () => { const file = exportKey.current?.files?.[0]; if (!file) return setError("Choose an operator-held archive key file."); try { setWorking(true); setError(""); const revision = await client.captureConfiguration(); if (!revision.payload || !revision.id) throw new Error("No exportable configuration was returned."); const value = await seal(revision.payload, revision.id, revision.schemaVersion, await key(file)); const url = URL.createObjectURL(new Blob([JSON.stringify(value)], { type: "application/vnd.zon.configuration-archive+json" })); const a = document.createElement("a"); a.href = url; a.download = `${value.manifest.archiveId}.zonconfig`; a.click(); URL.revokeObjectURL(url); setMessage("Configuration export downloaded. Keep its key separately."); } catch (e) { setError(e instanceof Error ? e.message : "Export failed."); } finally { setWorking(false); } };
  const importConfig = async () => { const source = archive.current?.files?.[0], material = importKey.current?.files?.[0]; if (!source || !material) return setError("Choose both an archive and its key file."); if (!window.confirm("Replace the active desired configuration? Data is not restored.")) return; try { setWorking(true); setError(""); const revision = await client.importConfigurationPayload(await open(source, await key(material)), true); setMessage(`Configuration replacement completed as ${revision.id}. ${revision.applyMessage || "Configuration reconciled."}`); } catch (e) { setError(e instanceof Error ? e.message : "Import failed."); } finally { setWorking(false); } };
  return <PageFrame title="Backup & Restore" eyebrow="Administration" description="Configuration exports are portable encrypted files. A system backup is a separate, coordinated PostgreSQL and blob-storage operation." pathname={props.pathname} onNavigate={navigate} tabs={[{ label: "Backup & Restore", path: "/admin/backup-restore" }]}><div className="grid gap-5 xl:grid-cols-2"><Card title="Configuration export" subtitle="The archive key stays in this browser and is never sent to the appliance."><input ref={exportKey} type="file" /><button className="button button--primary mt-4" disabled={working} onClick={() => void exportConfig()}>Export configuration</button></Card><Card title="Configuration import" subtitle="Replace active desired configuration; application and pipeline data are not changed."><input ref={archive} type="file" accept=".zonconfig,application/json" /><input ref={importKey} className="mt-3" type="file" /><button className="button mt-4" disabled={working} onClick={() => void importConfig()}>Replace configuration</button></Card><Card title="System backup" subtitle="PostgreSQL, configuration, blob-storage objects, and the installation receipt."><p className="m-0 text-sm leading-6 text-slate-600">Create and restore this appliance-lifecycle operation with zonctl on the control-plane host. Blob storage is briefly quiesced so the archive is consistent. A browser-triggered system-backup operation needs a dedicated, allowlisted host-agent API; it is not exposed from this page.</p></Card></div>{error ? <p className="mt-5 text-sm text-red-700">{error}</p> : null}{message ? <p className="mt-5 text-sm text-emerald-700">{message}</p> : null}</PageFrame>;
}
