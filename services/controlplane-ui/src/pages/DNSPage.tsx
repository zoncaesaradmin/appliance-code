import React, { FormEvent, useEffect, useState } from "react";
import { Card, PageFrame } from "../components";
import { client } from "../lib/api";
import { navigate } from "../lib/navigate";
import type { DNSRecord } from "../types";

export function DNSPage(): React.JSX.Element {
  const [zone, setZone] = useState("appliance.internal");
  const [records, setRecords] = useState<DNSRecord[]>([]);
  const [name, setName] = useState("");
  const [ipv4, setIPv4] = useState("");
  const [ttl, setTTL] = useState("300");
  const [message, setMessage] = useState("");

  async function refreshRecords() {
    const response = await client.listDNSRecords();
    setZone(response.zone);
    setRecords(response.items);
  }

  useEffect(() => {
    void refreshRecords();
  }, []);

  async function submitRecord(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    await client.upsertDNSRecord(name, { ipv4, ttl: Number(ttl) });
    setName("");
    setIPv4("");
    setTTL("300");
    setMessage("DNS record saved.");
    await refreshRecords();
  }

  async function removeRecord(recordName: string) {
    await client.deleteDNSRecord(recordName);
    setMessage("DNS record deleted.");
    await refreshRecords();
  }

  return (
    <PageFrame
      title="DNS"
      eyebrow=""
      description="Managed LAN DNS records for the appliance zone."
      pathname="/manage/dns"
      onNavigate={navigate}
      tabs={[{ label: "Records", path: "/manage/dns" }]}
    >
      {message ? <div className="message">{message}</div> : null}
      <div className="grid-two">
        <Card title="DNS zone records" subtitle={`Zone: ${zone}`}>
          <div className="table-list">
            {records.map((record) => (
              <div className="table-list__row" key={record.fqdn}>
                <div>
                  <strong>{record.name}</strong>
                  <span>
                    {record.ipv4} · TTL {record.ttl}
                  </span>
                </div>
                <button className="button button--ghost" onClick={() => void removeRecord(record.name)}>
                  Delete
                </button>
              </div>
            ))}
          </div>
        </Card>
        <Card title="Add or update DNS record" subtitle="Single-record management flow">
          <form className="stack-form" onSubmit={submitRecord}>
            <label className="field">
              <span>Hostname</span>
              <input value={name} onChange={(event) => setName(event.target.value)} />
            </label>
            <label className="field">
              <span>IPv4 address</span>
              <input value={ipv4} onChange={(event) => setIPv4(event.target.value)} />
            </label>
            <label className="field">
              <span>TTL (seconds)</span>
              <input value={ttl} onChange={(event) => setTTL(event.target.value)} />
            </label>
            <button className="button button--primary" type="submit">
              Save record
            </button>
          </form>
        </Card>
      </div>
    </PageFrame>
  );
}
