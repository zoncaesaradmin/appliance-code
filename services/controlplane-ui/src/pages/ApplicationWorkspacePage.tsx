import React from "react";
import { Card, EmptyState, PageFrame } from "../components";
import { navigate } from "../lib/navigate";
import { applicationAuth, type NearbyZone } from "../lib/applicationAuth";
import type { Session } from "../types";
import { useState } from "react";

/**
 * First application PWA shell. Business screens can be added here without
 * changing the operator console route tree or its API contracts.
 */
export function ApplicationWorkspacePage(props: { pathname: string; session: Session }): React.JSX.Element {
  if (props.pathname === "/app/discover") {
    return <NearbyDiscoveryPage />;
  }
  const name = props.session.displayName || props.session.username;
  return (
    <PageFrame
      title="Zonited App"
      eyebrow="Application"
      description="Your signed-in space for local services, orders, and microzone activity."
      pathname={props.pathname}
      onNavigate={navigate}
      tabs={[]}
    >
      <div className="application-home">
        <Card title={`Good to see you, ${name}.`} subtitle="Your microzone is ready for the next connection.">
          <div className="application-home__welcome">
            <div>
              <strong>One place for the things around you.</strong>
              <p>Application services will appear here as they become available in this signed appliance release.</p>
            </div>
            <span className="application-home__mark" aria-hidden="true">Z</span>
          </div>
        </Card>
        <div className="application-home__grid">
          <Card title="Services" subtitle="Discover what is available in your microzone.">
            <div className="application-home__welcome">
              <div><strong>Find nearby Zonities.</strong><p>Share your location to see public zones within 10 km and request access.</p></div>
              <button className="button button--primary" type="button" onClick={() => navigate("/app/discover")}>Find nearby</button>
            </div>
          </Card>
          <Card title="Orders" subtitle="Keep track of requests made through your channels.">
            <EmptyState message="Order history will appear here." />
          </Card>
        </div>
        <div className="button-row">
          <button className="button button--primary" type="button" onClick={() => navigate("/account/profile")}>
            Manage your account
          </button>
          <button className="button button--ghost" type="button" onClick={() => navigate("/console", true)}>
            Operations console
          </button>
        </div>
      </div>
    </PageFrame>
  );
}

function NearbyDiscoveryPage(): React.JSX.Element {
  const [zones, setZones] = useState<NearbyZone[]>([]);
  const [status, setStatus] = useState<"idle" | "locating" | "loading" | "ready" | "joined">("idle");
  const [error, setError] = useState("");
  const [selected, setSelected] = useState("");
  const [zoneName, setZoneName] = useState("");
  const [zoneRequestSent, setZoneRequestSent] = useState(false);
  const discoveryToken = new URLSearchParams(window.location.search).get("token") ?? "";

  async function locate(): Promise<void> {
    setError("");
    if (!navigator.geolocation) { setError("Location is not available in this browser. Use the app from a device with location enabled."); return; }
    setStatus("locating");
    navigator.geolocation.getCurrentPosition(
      (position) => {
        setStatus("loading");
        void applicationAuth.nearby(position.coords.latitude, position.coords.longitude, discoveryToken)
          .then((result) => { setZones(result.zones); setStatus("ready"); })
          .catch((reason: unknown) => { setError(reason instanceof Error ? reason.message : "Could not find nearby zones."); setStatus("idle"); });
      },
      () => { setError("We could not read your location. Allow location access and try again."); setStatus("idle"); },
      { enableHighAccuracy: false, maximumAge: 300000, timeout: 10000 }
    );
  }

  async function join(): Promise<void> {
    if (!selected) return;
    setError("");
    try {
      await applicationAuth.session();
      await applicationAuth.join(selected);
      setStatus("joined");
    } catch (reason: unknown) {
      if (reason instanceof Error && reason.message.toLowerCase().includes("sign in")) {
        navigate(`/app/login?next=${encodeURIComponent(window.location.pathname + window.location.search)}`);
        return;
      }
      setError(reason instanceof Error ? reason.message : "Could not submit the request.");
    }
  }

  async function requestZone(): Promise<void> {
    setError("");
    try {
      await applicationAuth.session();
      await applicationAuth.requestZone(zoneName.trim());
      setZoneRequestSent(true);
    } catch (reason: unknown) {
      if (reason instanceof Error && reason.message.toLowerCase().includes("sign in")) {
        navigate(`/app/login?next=${encodeURIComponent(window.location.pathname + window.location.search)}`);
        return;
      }
      setError(reason instanceof Error ? reason.message : "Could not submit the Zonity request.");
    }
  }

  return <PageFrame title="Find nearby Zonities" eyebrow="Discovery" description="Share a location to see public zones within 10 km. You choose whether to request access." pathname="/app/discover" onNavigate={navigate} tabs={[]}>
    <div className="application-home">
      <Card title="Your location" subtitle="Your exact coordinates are used only to calculate nearby results.">
        <div className="button-row"><button className="button button--primary" type="button" onClick={() => void locate()} disabled={status === "locating" || status === "loading"}>{status === "locating" ? "Getting location…" : status === "loading" ? "Finding nearby…" : "Use my location"}</button><button className="button button--ghost" type="button" onClick={() => navigate("/app")}>Back</button></div>
        {error ? <p className="form-error" role="alert">{error}</p> : null}
      </Card>
      {status === "joined" ? <Card title="Request received" subtitle="The zone team can review your request."><p>Your request is pending. We will keep this space updated.</p></Card> : null}
      {status !== "joined" && zones.length > 0 ? <Card title="Nearby zones" subtitle="Choose one to request access."><div className="application-zone-list">{zones.map((zone) => <label className="application-zone-option" key={zone.id}><input type="radio" name="zone" value={zone.id} checked={selected === zone.id} onChange={() => setSelected(zone.id)} /><span><strong>{zone.name}</strong><small>{zone.distance_km.toFixed(1)} km away</small></span></label>)}</div><button className="button button--primary" type="button" disabled={!selected} onClick={() => void join()}>Request access</button></Card> : null}
      {status === "ready" && zones.length === 0 ? <Card title="No zones found" subtitle="There are no public zones within 10 km of this location."><p>You cannot create a Zonity from this app. You can request one and the platform team will review and create it if appropriate.</p>{zoneRequestSent ? <p className="message">Your Zonity request was submitted for review.</p> : <form className="application-zone-request" onSubmit={(event) => { event.preventDefault(); void requestZone(); }}><label className="field-label" htmlFor="zone-request-name">Requested Zonity name</label><input id="zone-request-name" className="text-input" value={zoneName} onChange={(event) => setZoneName(event.target.value)} minLength={2} maxLength={100} required placeholder="Name or locality" /><button className="button button--primary" type="submit" disabled={!zoneName.trim()}>Request a Zonity</button></form>}</Card> : null}
    </div>
  </PageFrame>;
}

