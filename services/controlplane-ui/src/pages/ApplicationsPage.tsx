import React, { useEffect, useState } from "react";
import { Button, Card, EmptyState, PageFrame } from "../components";
import { client } from "../lib/api";
import { formatTimestamp } from "../lib/format";
import { navigate } from "../lib/navigate";
import type { ApplicationDefinition, ApplicationInstance } from "../types";

// Applications are release-reviewed contracts. This page deliberately offers
// enable/disable only; it never accepts a user-supplied image or manifest.
export function ApplicationsPage(): React.JSX.Element {
  const [definitions, setDefinitions] = useState<ApplicationDefinition[]>([]);
  const [instances, setInstances] = useState<ApplicationInstance[]>([]);
  const [error, setError] = useState("");
  const [message, setMessage] = useState("");
  const [busy, setBusy] = useState("");

  async function load() {
    try {
      const [nextDefinitions, nextInstances] = await Promise.all([
        client.listApplications(),
        client.listApplicationInstances()
      ]);
      setDefinitions(nextDefinitions);
      setInstances(nextInstances);
      setError("");
    } catch (err) {
      setError(err instanceof Error ? err.message : "Could not load approved applications.");
    }
  }

  useEffect(() => {
    void load();
  }, []);

  async function enable(definition: ApplicationDefinition) {
    setBusy(definition.name);
    setError("");
    setMessage("");
    try {
      await client.installApplication(definition.name, definition.version);
      await load();
      setMessage(`${definition.name} is being enabled.`);
    } catch (err) {
      setError(err instanceof Error ? err.message : "Could not enable application.");
    } finally {
      setBusy("");
    }
  }

  async function disable(instance: ApplicationInstance) {
    setBusy(instance.name);
    setError("");
    setMessage("");
    try {
      await client.disableApplication(instance.name);
      await load();
      setMessage(`${instance.name} is being disabled and its external access is being withdrawn.`);
    } catch (err) {
      setError(err instanceof Error ? err.message : "Could not disable application.");
    } finally {
      setBusy("");
    }
  }

  const instancesByName = new Map(instances.map((instance) => [instance.name, instance]));
  return (
    <PageFrame
      title="Applications"
      eyebrow="Admin"
      description="Enable only applications that are reviewed and included in this signed appliance release."
      pathname="/admin/applications"
      onNavigate={navigate}
	  tabs={[]}
    >
      <div className="stack">
        {error ? <div className="message message--error">{error}</div> : null}
        {message ? <div className="message">{message}</div> : null}
        <Card title="Approved Applications" subtitle="Images, ports, mDNS records, and host firewall grants are fixed by the release catalog.">
          {definitions.length === 0 ? (
			<EmptyState message="No applications are included in this signed release." />
          ) : (
            <div className="stack">
              {definitions.map((definition) => {
                const instance = instancesByName.get(definition.name);
                const enabled = instance?.desiredState === "running";
                return (
                  <div className="detail-list" key={`${definition.name}@${definition.version}`}>
                    <div>
                      <span>Application</span>
                      <strong>{definition.name}</strong>
                    </div>
                    <div>
                      <span>Release version</span>
                      <strong>{definition.version}</strong>
                    </div>
                    <div>
                      <span>Status</span>
                      <strong>{instance ? `${instance.desiredState} / ${instance.observedState}` : "Disabled"}</strong>
                    </div>
                    <div>
                      <span>Updated</span>
                      <strong>{instance?.updatedAt ? formatTimestamp(instance.updatedAt) : "Not enabled"}</strong>
                    </div>
                    <div className="button-row">
                      {enabled ? (
                        <Button type="button" variant="ghost" disabled={busy === definition.name} onClick={() => void disable(instance)}>
                          Disable
                        </Button>
                      ) : (
                        <Button type="button" disabled={busy === definition.name} onClick={() => void enable(definition)}>
                          Enable
                        </Button>
                      )}
                    </div>
                  </div>
                );
              })}
            </div>
          )}
        </Card>
        <Card title="Jellyfin Access" subtitle="After Jellyfin reports running, open it directly from the LAN.">
          <p className="m-0 text-sm leading-6 text-slate-600">
            Use <strong>http://jellyfin.local:8096</strong>. Compatible Jellyfin clients can also discover the server over the approved local-network discovery port.
          </p>
        </Card>
      </div>
    </PageFrame>
  );
}
