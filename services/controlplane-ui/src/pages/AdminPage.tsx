import React, { FormEvent, useEffect, useState } from "react";
import { Card, EmptyState, PageFrame } from "../components";
import { client } from "../lib/api";
import { formatTimestamp } from "../lib/format";
import { navigate } from "../lib/navigate";
import type {
  ApplianceIdentity,
  ApplianceMetadataBundleStatus,
  ApplianceProfile,
  LicensingStatus,
  MetadataBundleValidationResult,
  ProfileValidationResult,
  Version
} from "../types";

export function AdminPage(props: { pathname: string; capabilities: string[] }): React.JSX.Element {
  const isProfiles = props.pathname === "/admin/profiles" || props.pathname.startsWith("/admin/profiles/");
  const isMetadata =
    props.pathname === "/admin/metadata-bundle" || props.pathname.startsWith("/admin/metadata-bundle/");
  const isLicensing =
    props.pathname === "/admin/licensing" || props.pathname.startsWith("/admin/licensing/");
  const isSystemStatus =
    props.pathname === "/admin/system-status" ||
    props.pathname.startsWith("/admin/system-status/") ||
    (!isProfiles && !isLicensing && !isMetadata);

  if (isProfiles) {
    return <AdminProfilesPage />;
  }
  if (isMetadata) {
    return <AdminMetadataBundlePage />;
  }
  if (isLicensing) {
    return <AdminLicensingPage />;
  }
  return <AdminSystemStatusPage pathname={props.pathname} capabilities={props.capabilities} />;
}

function AdminSystemStatusPage(props: {
  pathname: string;
  capabilities: string[];
}): React.JSX.Element {
  const [version, setVersion] = useState<Version | null>(null);
  const [health, setHealth] = useState("unknown");
  const [identity, setIdentity] = useState<ApplianceIdentity | null>(null);

  useEffect(() => {
    void (async () => {
      const [nextVersion, nextHealth, nextIdentity] = await Promise.all([
        client.getVersion().catch(() => null),
        client.getReady().then((value) => value.status).catch(() => "degraded"),
        client.getIdentity().catch(() => null)
      ]);
      setVersion(nextVersion);
      setHealth(nextHealth);
      setIdentity(nextIdentity);
    })();
  }, [props.pathname]);

  return (
    <PageFrame
      title="System Status"
      eyebrow=""
      description="Version, readiness, and appliance identity."
      pathname={props.pathname}
      onNavigate={navigate}
      tabs={[
        { label: "Overview", path: "/admin/system-status" },
        { label: "Resources", path: "/admin/system-status/resources" }
      ]}
    >
      {props.pathname === "/admin/system-status/resources" ? (
        <Card title="Resources" subtitle="Host and cluster resource posture">
          <EmptyState message="Resource utilization and capacity details will appear here when the supporting control-plane APIs are available." />
        </Card>
      ) : (
        <div className="grid-two">
          <Card title="Overview" subtitle="Primary appliance runtime posture">
            <div className="detail-list">
              <div>
                <span>Readiness</span>
                <strong>{health}</strong>
              </div>
              <div>
                <span>Version</span>
                <strong>{version?.version || "Unknown"}</strong>
              </div>
              <div>
                <span>Build time</span>
                <strong>{formatTimestamp(version?.buildTime)}</strong>
              </div>
              <div>
                <span>Commit</span>
                <strong>{version?.commit || "Unknown"}</strong>
              </div>
            </div>
          </Card>
          <Card title="Appliance identity" subtitle="Operator-facing identity and capability context">
            <div className="detail-list">
              <div>
                <span>Appliance name</span>
                <strong>{identity?.applianceName || "Unknown"}</strong>
              </div>
              <div>
                <span>FQDN</span>
                <strong>{identity?.fqdn || "Unavailable"}</strong>
              </div>
              <div>
                <span>Node IPv4</span>
                <strong>{identity?.nodeIPv4 || "Unavailable"}</strong>
              </div>
            </div>
            <div className="badge-row">
              {props.capabilities.map((capability) => (
                <span className="pill pill--navy" key={capability}>
                  {capability}
                </span>
              ))}
            </div>
          </Card>
        </div>
      )}
    </PageFrame>
  );
}

function AdminLicensingPage(): React.JSX.Element {
  const [status, setStatus] = useState<LicensingStatus | null>(null);
  const [message, setMessage] = useState("");
  const [error, setError] = useState("");
  const [document, setDocument] = useState("");
  const [submitting, setSubmitting] = useState(false);

  async function refresh() {
    setStatus(await client.getLicensingStatus());
  }

  useEffect(() => {
    void refresh().catch((err) =>
      setError(err instanceof Error ? err.message : "Could not load licensing status.")
    );
  }, []);

  async function acceptBase() {
    setSubmitting(true);
    setError("");
    setMessage("");
    try {
      setStatus(await client.acceptBaseEntitlement());
      setMessage("Base/free entitlement accepted. Licensing is now resolved.");
    } catch (err) {
      setError(err instanceof Error ? err.message : "Could not accept base entitlement.");
    } finally {
      setSubmitting(false);
    }
  }

  async function importLicense(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    setSubmitting(true);
    setError("");
    setMessage("");
    try {
      setStatus(await client.importLicense(document));
      setDocument("");
      setMessage("Offline license imported successfully.");
    } catch (err) {
      setError(err instanceof Error ? err.message : "Could not import license.");
    } finally {
      setSubmitting(false);
    }
  }

  return (
    <PageFrame
      title="Licensing"
      eyebrow=""
      description="Resolve appliance licensing with an offline license or the base/free entitlement."
      pathname="/admin/licensing"
      onNavigate={navigate}
      tabs={[{ label: "Overview", path: "/admin/licensing" }]}
    >
      {error ? <div className="message message--error">{error}</div> : null}
      {message ? <div className="message">{message}</div> : null}
      <div className="grid-two">
        <Card title="Licensing status" subtitle="Current entitlement state">
          {status ? (
            <div className="detail-list">
              <div>
                <span>State</span>
                <strong>{status.state}</strong>
              </div>
              <div>
                <span>Resolved</span>
                <strong>{status.resolved ? "Yes" : "No"}</strong>
              </div>
              <div>
                <span>Profile activation</span>
                <strong>{status.profileActivationAvailable ? "Available" : "Locked"}</strong>
              </div>
              <div>
                <span>Accepted at</span>
                <strong>{formatTimestamp(status.acceptedAt)}</strong>
              </div>
            </div>
          ) : (
            <EmptyState message="Loading licensing status..." />
          )}
          <div className="badge-row" style={{ marginTop: "1rem" }}>
            {(status?.entitledCapabilities || []).map((capability) => (
              <span className="pill pill--navy" key={capability}>
                {capability}
              </span>
            ))}
          </div>
        </Card>
        <Card title="Accept base/free entitlement" subtitle="Continue without a fuller license">
          <p className="muted">
            Accepting base/free entitlement marks licensing as resolved and keeps advanced
            capabilities unavailable until a fuller offline license is imported.
          </p>
          <button className="button button--primary" disabled={submitting} onClick={() => void acceptBase()}>
            {submitting ? "Working..." : "Accept base entitlement"}
          </button>
        </Card>
      </div>
      <Card title="Import offline license" subtitle="Paste a signed offline license document">
        <form className="stack-form" onSubmit={(event) => void importLicense(event)}>
          <label className="field">
            <span>License document (JSON)</span>
            <textarea
              rows={8}
              value={document}
              onChange={(event) => setDocument(event.target.value)}
              placeholder='{"version":1,"issuer":"zon","capabilities":["base"],"signature":"offline-dev"}'
            />
          </label>
          <button className="button button--primary" type="submit" disabled={submitting || !document.trim()}>
            Import license
          </button>
        </form>
      </Card>
    </PageFrame>
  );
}

function AdminProfilesPage(): React.JSX.Element {
  const [licensing, setLicensing] = useState<LicensingStatus | null>(null);
  const [profiles, setProfiles] = useState<ApplianceProfile[]>([]);
  const [message, setMessage] = useState("");
  const [error, setError] = useState("");
  const [validation, setValidation] = useState<ProfileValidationResult | null>(null);
  const [pendingActivate, setPendingActivate] = useState<string | null>(null);

  async function refresh() {
    const [nextLicensing, nextProfiles] = await Promise.all([
      client.getLicensingStatus(),
      client.listApplianceProfiles().catch(() => [])
    ]);
    setLicensing(nextLicensing);
    setProfiles(nextProfiles);
  }

  useEffect(() => {
    void refresh().catch((err) =>
      setError(err instanceof Error ? err.message : "Could not load profiles.")
    );
  }, []);

  async function validateProfile(id: string) {
    setError("");
    setValidation(await client.validateApplianceProfile(id));
  }

  async function confirmActivate(id: string) {
    setError("");
    setMessage("");
    try {
      const result = await client.activateApplianceProfile(id);
      setValidation(result.validation);
      setPendingActivate(null);
      setMessage(result.activation.message);
      await refresh();
    } catch (err) {
      setError(err instanceof Error ? err.message : "Could not activate profile.");
    }
  }

  const locked = licensing && !licensing.profileActivationAvailable;

  return (
    <PageFrame
      title="Profiles"
      eyebrow=""
      description="Profiles come from the active metadata bundle. Activate after licensing is resolved."
      pathname="/admin/profiles"
      onNavigate={navigate}
      tabs={[{ label: "Overview", path: "/admin/profiles" }]}
    >
      {error ? <div className="message message--error">{error}</div> : null}
      {message ? <div className="message">{message}</div> : null}
      {locked ? (
        <Card title="Licensing required" subtitle="Profile activation is locked">
          <EmptyState message="Resolve licensing before activating appliance profiles." />
          <button className="button button--primary" onClick={() => navigate("/admin/licensing")}>
            Open Licensing
          </button>
        </Card>
      ) : (
        <>
          <Card title="Appliance profiles" subtitle="Catalog from the active metadata bundle">
            <div className="table-list">
              {profiles.map((profile) => (
                <div className="table-list__row" key={profile.id}>
                  <div>
                    <strong>
                      {profile.displayName} {profile.active ? "(active)" : ""}
                    </strong>
                    <span>
                      Built-in · {profile.capabilities.join(", ")}
                      {profile.metadataVersion ? ` · metadata ${profile.metadataVersion}` : ""}
                    </span>
                  </div>
                  <div className="button-row">
                    <button className="button button--ghost" onClick={() => void validateProfile(profile.id)}>
                      Validate
                    </button>
                    <button
                      className="button button--primary"
                      onClick={() => {
                        setPendingActivate(profile.id);
                        void validateProfile(profile.id);
                      }}
                    >
                      Activate
                    </button>
                  </div>
                </div>
              ))}
            </div>
          </Card>
          <Card title="Validation / activation" subtitle="Grouped fail-closed checks">
            {validation ? (
              <div className="stack">
                <div className="detail-list">
                  <div>
                    <span>Profile</span>
                    <strong>{validation.profileId}</strong>
                  </div>
                  <div>
                    <span>Overall</span>
                    <strong>{validation.ok ? "OK" : "Failed"}</strong>
                  </div>
                </div>
                {validation.groups.map((group) => (
                  <div key={group.name} className="status-box">
                    <strong>
                      {group.name}: {group.ok ? "ok" : "failed"}
                    </strong>
                    <span>{group.message}</span>
                    {(group.errors || []).map((item) => (
                      <span key={item}>{item}</span>
                    ))}
                  </div>
                ))}
                {pendingActivate ? (
                  <button
                    className="button button--primary"
                    disabled={!validation.ok}
                    onClick={() => void confirmActivate(pendingActivate)}
                  >
                    Confirm activate {pendingActivate}
                  </button>
                ) : null}
              </div>
            ) : (
              <EmptyState message="Validate a profile to review definition, bundle, and entitlement checks." />
            )}
          </Card>
        </>
      )}
    </PageFrame>
  );
}

function AdminMetadataBundlePage(): React.JSX.Element {
  const [status, setStatus] = useState<ApplianceMetadataBundleStatus | null>(null);
  const [validation, setValidation] = useState<MetadataBundleValidationResult | null>(null);
  const [file, setFile] = useState<File | null>(null);
  const [message, setMessage] = useState("");
  const [error, setError] = useState("");
  const [submitting, setSubmitting] = useState(false);

  async function refresh() {
    setStatus(await client.getMetadataBundleStatus());
  }

  useEffect(() => {
    void refresh().catch((err) =>
      setError(err instanceof Error ? err.message : "Could not load metadata bundle status.")
    );
  }, []);

  async function validateSelected() {
    if (!file) {
      return;
    }
    setError("");
    setMessage("");
    setSubmitting(true);
    try {
      setValidation(await client.validateMetadataBundle(file));
      setMessage("Metadata bundle validated.");
    } catch (err) {
      setError(err instanceof Error ? err.message : "Validation failed.");
    } finally {
      setSubmitting(false);
    }
  }

  async function installSelected() {
    if (!file) {
      return;
    }
    setError("");
    setMessage("");
    setSubmitting(true);
    try {
      const result = await client.installMetadataBundle(file);
      setValidation(result.validation);
      setStatus(result.status);
      setMessage(`Installed metadata ${result.status.activeMetadataVersion}.`);
    } catch (err) {
      setError(err instanceof Error ? err.message : "Install failed.");
    } finally {
      setSubmitting(false);
    }
  }

  async function rollback() {
    setError("");
    setMessage("");
    setSubmitting(true);
    try {
      const next = await client.rollbackMetadataBundle();
      setStatus(next);
      setMessage(`Rolled back to metadata ${next.activeMetadataVersion}.`);
    } catch (err) {
      setError(err instanceof Error ? err.message : "Rollback failed.");
    } finally {
      setSubmitting(false);
    }
  }

  return (
    <PageFrame
      title="Metadata Bundle"
      eyebrow=""
      description="Install or roll back the signed appliance metadata bundle that defines profiles and capabilities."
      pathname="/admin/metadata-bundle"
      onNavigate={navigate}
      tabs={[{ label: "Overview", path: "/admin/metadata-bundle" }]}
    >
      {error ? <div className="message message--error">{error}</div> : null}
      {message ? <div className="message">{message}</div> : null}
      <Card title="Active metadata" subtitle="Installed metadata-bundle revisions">
        {status ? (
          <div className="detail-list">
            <div>
              <span>Software</span>
              <strong>{status.softwareVersion}</strong>
            </div>
            <div>
              <span>Active metadata</span>
              <strong>{status.activeMetadataVersion}</strong>
            </div>
            <div>
              <span>Previous metadata</span>
              <strong>{status.previousMetadataVersion || "None"}</strong>
            </div>
            <div>
              <span>Digest</span>
              <strong>{status.activeDigest || "—"}</strong>
            </div>
          </div>
        ) : (
          <EmptyState message="Loading metadata bundle status..." />
        )}
        <button
          className="button button--ghost"
          style={{ marginTop: "1rem" }}
          disabled={submitting || !status?.canRollback}
          onClick={() => void rollback()}
        >
          Roll back to previous
        </button>
      </Card>
      <Card title="Install metadata bundle" subtitle="Upload a signed .tar.zst archive">
        <label className="field">
          <span>Metadata archive</span>
          <input
            type="file"
            accept=".tar.zst,.zst"
            onChange={(event) => setFile(event.target.files?.[0] || null)}
          />
        </label>
        <div className="button-row">
          <button className="button button--ghost" disabled={!file || submitting} onClick={() => void validateSelected()}>
            Validate
          </button>
          <button className="button button--primary" disabled={!file || submitting} onClick={() => void installSelected()}>
            Install
          </button>
        </div>
        {validation ? (
          <div className="stack" style={{ marginTop: "1rem" }}>
            <strong>{validation.ok ? "Validation OK" : "Validation failed"}</strong>
            {validation.groups.map((group) => (
              <div key={group.name} className="status-box">
                <strong>
                  {group.name}: {group.ok ? "ok" : "failed"}
                </strong>
                <span>{group.message}</span>
                {(group.errors || []).map((item) => (
                  <span key={item}>{item}</span>
                ))}
              </div>
            ))}
          </div>
        ) : null}
      </Card>
    </PageFrame>
  );
}
