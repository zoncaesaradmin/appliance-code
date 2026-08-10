import React, { FormEvent, useEffect, useState } from "react";
import { Card, EmptyState, PageFrame } from "../components";
import { client } from "../lib/api";
import { formatTimestamp } from "../lib/format";
import { navigate } from "../lib/navigate";
import { requestViewSync, withViewSync } from "../lib/viewSync";
import { useViewSyncGeneration, useViewSyncTag } from "../lib/viewSyncHooks";
import type {
  ApplianceIdentity,
  ApplianceMetadataBundleStatus,
  ApplianceProfile,
  HostHealth,
  HostInfo,
  HostMDNSStatus,
  HostWifiAPStatus,
  LicensingStatus,
  MetadataBundleValidationResult,
  ProfileValidationResult,
  Version
} from "../types";
import { DNSPage } from "./DNSPage";

export function AdminPage(props: { pathname: string; capabilities: string[] }): React.JSX.Element {
  const isProfiles = props.pathname === "/admin/profiles" || props.pathname.startsWith("/admin/profiles/");
  const isMetadata =
    props.pathname === "/admin/metadata-bundle" || props.pathname.startsWith("/admin/metadata-bundle/");
  const isLicensing =
    props.pathname === "/admin/licensing" || props.pathname.startsWith("/admin/licensing/");
  const isHostServices =
    props.pathname === "/admin/host-services" || props.pathname.startsWith("/admin/host-services/");
  const isLANServices =
    props.pathname === "/admin/lan-services" || props.pathname.startsWith("/admin/lan-services/");
  const isSystemStatus =
    props.pathname === "/admin/system-status" ||
    props.pathname.startsWith("/admin/system-status/") ||
    (!isProfiles && !isLicensing && !isMetadata && !isHostServices && !isLANServices);

  if (isProfiles) {
    return <AdminProfilesPage />;
  }
  if (isMetadata) {
    return <AdminMetadataBundlePage />;
  }
  if (isLicensing) {
    return <AdminLicensingPage />;
  }
  if (isHostServices) {
    return <AdminHostServicesPage pathname={props.pathname} />;
  }
  if (isLANServices) {
    return <DNSPage />;
  }
  return <AdminSystemStatusPage pathname={props.pathname} capabilities={props.capabilities} />;
}

function EyeIcon(): React.JSX.Element {
  return (
    <svg
      className="password-field__icon"
      width="20"
      height="20"
      viewBox="0 0 24 24"
      fill="none"
      stroke="currentColor"
      strokeWidth="2"
      strokeLinecap="round"
      strokeLinejoin="round"
      aria-hidden="true"
    >
      <path d="M1 12s4-8 11-8 11 8 11 8-4 8-11 8-11-8-11-8z" />
      <circle cx="12" cy="12" r="3" />
    </svg>
  );
}

function EyeOffIcon(): React.JSX.Element {
  return (
    <svg
      className="password-field__icon"
      width="20"
      height="20"
      viewBox="0 0 24 24"
      fill="none"
      stroke="currentColor"
      strokeWidth="2"
      strokeLinecap="round"
      strokeLinejoin="round"
      aria-hidden="true"
    >
      <path d="M17.94 17.94A10.07 10.07 0 0 1 12 20c-7 0-11-8-11-8a18.45 18.45 0 0 1 5.06-5.94" />
      <path d="M9.9 4.24A9.12 9.12 0 0 1 12 4c7 0 11 8 11 8a18.5 18.5 0 0 1-2.16 3.19" />
      <path d="M14.12 14.12a3 3 0 1 1-4.24-4.24" />
      <line x1="1" y1="1" x2="23" y2="23" />
    </svg>
  );
}

function formatMediaStatus(
  media: import("../types").HostMediaStatus | undefined,
  _label: string
): string {
  if (!media) {
    return "Unknown";
  }
  if (!media.present) {
    return "Not present";
  }
  const state = media.enabled ? "Enabled (up)" : "Disabled (down)";
  const addrs = (media.ipv4Addresses || []).filter(Boolean);
  if (addrs.length === 0) {
    const ifaces = (media.interfaces || []).filter(Boolean);
    return ifaces.length > 0 ? `${state} · ${ifaces.join(", ")}` : state;
  }
  return `${state} · ${addrs.join(", ")}`;
}

function formatPrimaryLAN(network: import("../types").HostNetworkStatus | undefined): string {
  if (!network?.primaryLANIPv4) {
    return "";
  }
  const source = network.primaryLANSource || "unknown";
  const sourceLabel =
    source === "ethernet" ? "Ethernet" : source === "wifi" ? "Wi-Fi" : source;
  return `${network.primaryLANIPv4} (${sourceLabel})`;
}

function formatConfiguredNodeIPv4(nodeIPv4: string | undefined): string {
  const ip = (nodeIPv4 || "").trim();
  if (!ip) {
    return "";
  }
  if (ip === "10.42.0.1" || ip.startsWith("10.42.0.")) {
    return `${ip} (configured; Wi-Fi AP range — live LAN preferred above)`;
  }
  return `${ip} (configured)`;
}

function formatLinkKind(link: import("../types").HostNetworkLink): string {
  if (link.role === "management-ap") {
    return "Wi-Fi AP management";
  }
  if (link.kind === "ethernet") {
    return "Ethernet";
  }
  if (link.kind === "wifi") {
    return "Wi-Fi";
  }
  return link.kind || "interface";
}

function formatLinkAddresses(link: import("../types").HostNetworkLink): string {
  const addrs = (link.ipv4Addresses || []).filter(Boolean);
  if (addrs.length === 0) {
    return "no IPv4";
  }
  return addrs.join(", ");
}

function AdminHostServicesPage(props: { pathname: string }): React.JSX.Element {
  const isMDNS = props.pathname === "/admin/host-services/mdns";
  const [identity, setIdentity] = useState<ApplianceIdentity | null>(null);
  const [hostInfo, setHostInfo] = useState<HostInfo | null>(null);
  const [hostHealth, setHostHealth] = useState<HostHealth | null>(null);
  const [wifi, setWifi] = useState<HostWifiAPStatus | null>(null);
  const [mdns, setMdns] = useState<HostMDNSStatus | null>(null);
  const [networkError, setNetworkError] = useState("");
  const [wifiError, setWifiError] = useState("");
  const [mdnsError, setMdnsError] = useState("");
  const [message, setMessage] = useState("");
  const [psk, setPsk] = useState("");
  const [showPsk, setShowPsk] = useState(false);
  const [wifiBusy, setWifiBusy] = useState(false);
  const [mdnsBusy, setMdnsBusy] = useState(false);
  const [networkLoaded, setNetworkLoaded] = useState(false);
  const [wifiLoaded, setWifiLoaded] = useState(false);
  const [mdnsLoaded, setMdnsLoaded] = useState(false);

  useEffect(() => {
    let cancelled = false;
    setMessage("");
    void (async () => {
      const [nextIdentity, nextInfo, nextHealth] = await Promise.all([
        client.getIdentity().catch(() => null),
        client.getHostInfo().catch((err: unknown) => {
          if (!cancelled) {
            setNetworkError(err instanceof Error ? err.message : "Could not load host info.");
          }
          return null;
        }),
        client.getHostHealth().catch(() => null)
      ]);
      if (cancelled) {
        return;
      }
      setIdentity(nextIdentity);
      setHostInfo(nextInfo);
      setHostHealth(nextHealth);
      setNetworkLoaded(true);
      if (nextInfo || nextIdentity) {
        setNetworkError("");
      }
    })();
    void (async () => {
      try {
        const nextWifi = await client.getHostWifiAP();
        if (!cancelled) {
          setWifi(nextWifi);
          setWifiError("");
        }
      } catch (err) {
        if (!cancelled) {
          setWifiError(err instanceof Error ? err.message : "Could not load Wi-Fi AP status.");
        }
      } finally {
        if (!cancelled) {
          setWifiLoaded(true);
        }
      }
    })();
    void (async () => {
      try {
        const nextMdns = await client.getHostMDNS();
        if (!cancelled) {
          setMdns(nextMdns);
          setMdnsError("");
        }
      } catch (err) {
        if (!cancelled) {
          setMdnsError(err instanceof Error ? err.message : "Could not load mDNS status.");
        }
      } finally {
        if (!cancelled) {
          setMdnsLoaded(true);
        }
      }
    })();
    return () => {
      cancelled = true;
    };
  }, [props.pathname]);

  async function applyMDNS(desired: boolean) {
    setMdnsBusy(true);
    setMdnsError("");
    setMessage("");
    try {
      const status = await client.applyHostMDNS({ desired });
      setMdns(status);
      if (status.reason === "packages_missing") {
        setMdnsError(status.message || "mDNS packages are not installed on this host.");
      } else if (status.actual === "failed") {
        setMdnsError(status.message || "mDNS apply failed.");
      } else {
        setMessage(desired ? "mDNS enabled." : "mDNS disabled.");
      }
    } catch (err) {
      setMdnsError(err instanceof Error ? err.message : "Could not update mDNS.");
    } finally {
      setMdnsBusy(false);
    }
  }

  async function applyWifi(desired: boolean) {
    setWifiBusy(true);
    setWifiError("");
    setMessage("");
    try {
      const status = await client.applyHostWifiAP({
        desired,
        psk: desired ? psk : undefined
      });
      setWifi(status);
      if (desired) {
        setPsk("");
        setShowPsk(false);
      }
      if (status.reason === "packages_missing" || status.reason === "psk_missing") {
        setWifiError(status.message || status.reason);
      } else if (status.actual === "failed") {
        setWifiError(status.message || "Wi-Fi access point apply failed.");
      } else if (status.reason === "no_capable_hardware" || status.reason === "radio_in_use") {
        setWifiError(status.message || status.reason);
      } else {
        setMessage(desired ? "Wi-Fi access point enabled." : "Wi-Fi access point disabled.");
      }
    } catch (err) {
      setWifiError(err instanceof Error ? err.message : "Could not update Wi-Fi access point.");
    } finally {
      setWifiBusy(false);
    }
  }

  // true → only Disable; false → only Enable; null → loading/unavailable.
  const wifiOn: boolean | null = wifi ? wifi.desired || wifi.actual === "active" : null;
  const mdnsOn: boolean | null = mdns ? mdns.desired || mdns.actual === "active" : null;
  const pageError = isMDNS ? mdnsError : [networkError, wifiError].filter(Boolean).join(" ");

  return (
    <PageFrame
      title="Host Services"
      eyebrow=""
      description="Configure services that belong to this appliance host (this one physical or virtual machine)."
      pathname={props.pathname}
      onNavigate={navigate}
      tabs={[
        { label: "Network", path: "/admin/host-services" },
        { label: "mDNS", path: "/admin/host-services/mdns" }
      ]}
    >
      {pageError ? <div className="message message--error">{pageError}</div> : null}
      {message ? <div className="message">{message}</div> : null}
      {isMDNS ? (
        <Card title="mDNS" subtitle="Local hostname advertisement (avahi-daemon)">
          <div className={`host-service-panel${mdnsBusy ? " is-busy" : ""}`} aria-busy={mdnsBusy}>
            {mdns ? (
              <div className="detail-list">
                <div>
                  <span>Desired</span>
                  <strong>{mdns.desired ? "On" : "Off"}</strong>
                </div>
                <div>
                  <span>Actual</span>
                  <strong>{mdns.actual}</strong>
                </div>
                <div>
                  <span>Service</span>
                  <strong>{mdns.service}</strong>
                </div>
                {mdns.reason ? (
                  <div>
                    <span>Reason</span>
                    <strong>{mdns.reason}</strong>
                  </div>
                ) : null}
                {mdns.message ? (
                  <div>
                    <span>Detail</span>
                    <strong>{mdns.message}</strong>
                  </div>
                ) : null}
              </div>
            ) : (
              <EmptyState
                message={mdnsLoaded ? "mDNS status is unavailable." : "Loading mDNS status..."}
              />
            )}
            <div className="host-service-panel__actions">
              {mdnsOn === true ? (
                <button
                  className="button button--ghost"
                  type="button"
                  disabled={mdnsBusy}
                  onClick={() => void applyMDNS(false)}
                >
                  {mdnsBusy ? "Disabling mDNS…" : "Disable mDNS"}
                </button>
              ) : null}
              {mdnsOn === false ? (
                <button
                  className="button button--primary"
                  type="button"
                  disabled={mdnsBusy}
                  onClick={() => void applyMDNS(true)}
                >
                  {mdnsBusy ? "Enabling mDNS…" : "Enable mDNS"}
                </button>
              ) : null}
              {mdnsOn === null ? (
                <button className="button button--ghost" type="button" disabled>
                  {mdnsLoaded ? "Status unavailable" : "Loading…"}
                </button>
              ) : null}
            </div>
          </div>
        </Card>
      ) : (
        <div className="grid-two">
          <Card title="Host network" subtitle="Live host interfaces from host-agent (not install-time chart values)">
            {hostInfo || identity ? (
              <div className="detail-list">
                <div>
                  <span>Hostname</span>
                  <strong>{hostInfo?.hostname || "—"}</strong>
                </div>
                <div>
                  <span>Appliance FQDN</span>
                  <strong>{identity?.fqdn || "—"}</strong>
                </div>
                <div>
                  <span>Primary LAN IPv4</span>
                  <strong>
                    {formatPrimaryLAN(hostInfo?.network) ||
                      formatConfiguredNodeIPv4(identity?.nodeIPv4) ||
                      "—"}
                  </strong>
                </div>
                <div>
                  <span>Ethernet</span>
                  <strong>{formatMediaStatus(hostInfo?.network?.ethernet, "Ethernet")}</strong>
                </div>
                <div>
                  <span>Wi-Fi (client / LAN)</span>
                  <strong>{formatMediaStatus(hostInfo?.network?.wifi, "Wi-Fi")}</strong>
                </div>
                <div>
                  <span>Wi-Fi AP (management)</span>
                  <strong>{formatMediaStatus(hostInfo?.network?.wifiAP, "Wi-Fi AP")}</strong>
                </div>
                {(hostInfo?.network?.links || []).map((link) => (
                  <div key={`${link.name}-${link.role}`}>
                    <span>
                      {link.name} ({formatLinkKind(link)})
                    </span>
                    <strong>
                      {formatLinkAddresses(link)} · {link.state === "up" ? "up" : "down"}
                    </strong>
                  </div>
                ))}
                <div>
                  <span>Operating system</span>
                  <strong>{hostInfo?.operatingSystem || "—"}</strong>
                </div>
                <div>
                  <span>Architecture</span>
                  <strong>{hostInfo?.architecture || "—"}</strong>
                </div>
                {hostInfo?.kernelVersion ? (
                  <div>
                    <span>Kernel</span>
                    <strong>{hostInfo.kernelVersion}</strong>
                  </div>
                ) : null}
                <div>
                  <span>Host agent health</span>
                  <strong>{hostHealth?.status || "—"}</strong>
                </div>
              </div>
            ) : (
              <EmptyState
                message={
                  networkLoaded
                    ? "Host network status is unavailable."
                    : "Loading host network status..."
                }
              />
            )}
          </Card>

          <Card title="Wi-Fi access point" subtitle="Management AP at https://manage.ap/ (also https://10.42.0.1/)">
            <div className={`host-service-panel${wifiBusy ? " is-busy" : ""}`} aria-busy={wifiBusy}>
              {wifi ? (
                <div className="detail-list">
                  <div>
                    <span>Desired</span>
                    <strong>{wifi.desired ? "On" : "Off"}</strong>
                  </div>
                  <div>
                    <span>Actual</span>
                    <strong>{wifi.actual}</strong>
                  </div>
                  <div>
                    <span>SSID</span>
                    <strong>{wifi.ssid || "—"}</strong>
                  </div>
                  <div>
                    <span>Interface</span>
                    <strong>{wifi.iface || "—"}</strong>
                  </div>
                  <div>
                    <span>Management URL</span>
                    <strong>
                      {wifi.managementURL ||
                        (wifi.managementHostname
                          ? `https://${wifi.managementHostname}/`
                          : `https://${wifi.managementAddress}/`)}
                    </strong>
                  </div>
                  <div>
                    <span>Management IPv4</span>
                    <strong>{wifi.managementAddress || "10.42.0.1"}</strong>
                  </div>
                  <div>
                    <span>Local DNS for manage.ap</span>
                    <strong>
                      {wifi.localDNSServing === true
                        ? "Yes (AP dnsmasq)"
                        : wifi.localDNSServing === false
                          ? "No (host DNS / CoreDNS)"
                          : "—"}
                    </strong>
                  </div>
                  <div>
                    <span>Security</span>
                    <strong>{wifi.security}</strong>
                  </div>
                  {wifi.reason ? (
                    <div>
                      <span>Reason</span>
                      <strong>{wifi.reason}</strong>
                    </div>
                  ) : null}
                  {wifi.message ? (
                    <div>
                      <span>Detail</span>
                      <strong>{wifi.message}</strong>
                    </div>
                  ) : null}
                </div>
              ) : (
                <EmptyState
                  message={
                    wifiLoaded ? "Wi-Fi AP status is unavailable." : "Loading Wi-Fi AP status..."
                  }
                />
              )}
              <div className="host-service-panel__actions">
                {wifiOn === true ? (
                  <button
                    className="button button--ghost"
                    type="button"
                    disabled={wifiBusy}
                    onClick={() => void applyWifi(false)}
                  >
                    {wifiBusy ? "Disabling Wi-Fi AP…" : "Disable Wi-Fi AP"}
                  </button>
                ) : null}
                {wifiOn === false ? (
                  <form
                    className="stack-form"
                    style={{ width: "100%" }}
                    onSubmit={(event) => {
                      event.preventDefault();
                      void applyWifi(true);
                    }}
                  >
                    <p className="muted">Each enable requires a new WPA2 passphrase for security.</p>
                    <div className="field">
                      <label htmlFor="wifi-ap-psk">WPA2 passphrase (required to enable)</label>
                      <span className="password-field">
                        <input
                          id="wifi-ap-psk"
                          type={showPsk ? "text" : "password"}
                          autoComplete="new-password"
                          value={psk}
                          onChange={(event) => setPsk(event.target.value)}
                          placeholder="8–63 characters"
                          disabled={wifiBusy}
                          spellCheck={false}
                        />
                        <button
                          className="password-field__toggle"
                          type="button"
                          disabled={wifiBusy}
                          aria-label={showPsk ? "Hide passphrase" : "Show passphrase"}
                          aria-pressed={showPsk}
                          title={showPsk ? "Hide passphrase" : "Show passphrase"}
                          onClick={() => setShowPsk((v) => !v)}
                        >
                          {showPsk ? <EyeOffIcon /> : <EyeIcon />}
                        </button>
                      </span>
                    </div>
                    <button
                      className="button button--primary"
                      type="submit"
                      disabled={wifiBusy || psk.trim().length < 8}
                    >
                      {wifiBusy ? "Enabling Wi-Fi AP…" : "Enable Wi-Fi AP"}
                    </button>
                  </form>
                ) : null}
                {wifiOn === null ? (
                  <button className="button button--ghost" type="button" disabled>
                    {wifiLoaded ? "Status unavailable" : "Loading…"}
                  </button>
                ) : null}
              </div>
            </div>
          </Card>
        </div>
      )}
    </PageFrame>
  );
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
  const pageSync = useViewSyncGeneration("page");
  const licensingTag = useViewSyncTag("licensing");

  async function refresh() {
    setStatus(await client.getLicensingStatus());
  }

  useEffect(() => {
    void refresh().catch((err) =>
      setError(err instanceof Error ? err.message : "Could not load licensing status.")
    );
  }, [pageSync, licensingTag]);

  // Shared post-success plan: other views that care about setup/alerts re-fetch
  // without this page knowing about the notification widget implementation.
  const licensingResolvedSync = {
    regions: ["shell.alerts", "shell.bootstrap", "page"] as const,
    tags: ["licensing", "setup"]
  };

  async function acceptBase() {
    setSubmitting(true);
    setError("");
    setMessage("");
    try {
      setStatus(
        await withViewSync(
          () => client.acceptBaseEntitlement(),
          {
            regions: [...licensingResolvedSync.regions],
            tags: [...licensingResolvedSync.tags],
            reason: "licensing.base-accepted"
          }
        )
      );
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
      setStatus(
        await withViewSync(
          () => client.importLicense(document),
          {
            regions: [...licensingResolvedSync.regions],
            tags: [...licensingResolvedSync.tags],
            reason: "licensing.import"
          }
        )
      );
      setDocument("");
      setMessage("Offline license imported successfully.");
    } catch (err) {
      setError(err instanceof Error ? err.message : "Could not import license.");
    } finally {
      setSubmitting(false);
    }
  }

  const licensingResolved = Boolean(status?.resolved);
  const acceptBaseDisabled = submitting || licensingResolved;
  const importDisabled = submitting || !document.trim();

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
          {licensingResolved ? (
            <p className="muted">Base entitlement is already accepted (or a fuller license is active).</p>
          ) : null}
          <button
            className="button button--primary"
            disabled={acceptBaseDisabled}
            onClick={() => void acceptBase()}
          >
            {submitting ? "Working..." : licensingResolved ? "Base entitlement accepted" : "Accept base entitlement"}
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
              disabled={submitting}
            />
          </label>
          <button className="button button--primary" type="submit" disabled={importDisabled}>
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
  const pageSync = useViewSyncGeneration("page");
  const licensingTag = useViewSyncTag("licensing");

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
      setError(err instanceof Error ? err.message : "Could not load appliance profiles.")
    );
  }, [pageSync, licensingTag]);

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
      requestViewSync({
        regions: ["shell.bootstrap", "page"],
        tags: ["profiles", "setup"],
        reason: "profiles.activated"
      });
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
      title="Metadata"
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
