import React from "react";
import { Card, PageFrame } from "../components";
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
  return <ApplicationLandingPage name={props.session.displayName || props.session.username} />;
}

function ApplicationLandingPage(props: { name: string }): React.JSX.Element {
  const [activeTab, setActiveTab] = useState<"services" | "orders" | "payments">("services");
  return <main className="application-landing">
    <section className="application-landing__intro" aria-labelledby="application-landing-title">
      <div>
        <p className="application-landing__eyebrow">YOUR ZONITED SPACE</p>
        <h1 id="application-landing-title">Services around you.</h1>
        <p className="application-landing__lede">Good to see you, {props.name}. Find local services, place requests, and keep track of what matters to you.</p>
      </div>
      <button className="application-landing__location" type="button" onClick={() => navigate("/app/discover")}>Explore nearby</button>
    </section>
    <nav className="application-landing__tabs" aria-label="Application sections" role="tablist">
      {([['services', 'Services'], ['orders', 'Orders'], ['payments', 'Payments']] as const).map(([id, label]) => <button key={id} type="button" role="tab" aria-selected={activeTab === id} className={activeTab === id ? "application-landing__tab application-landing__tab--active" : "application-landing__tab"} onClick={() => setActiveTab(id)}>{label}</button>)}
    </nav>
    {activeTab === "services" ? <section className="application-service-grid" aria-label="Services">
      <article className="application-service-card application-service-card--primary"><div className="application-service-card__number">01</div><h2>Nearby Zonities</h2><p>Share your location to see public Zonities within 10 km and request access when you are close enough.</p><button className="application-service-card__action" type="button" onClick={() => navigate("/app/discover")}>Find nearby <span aria-hidden="true">↗</span></button></article>
      <article className="application-service-card"><div className="application-service-card__number">02</div><h2>Ordering</h2><p>Browse local offerings and send a request when ordering becomes available in your Zonity.</p><span className="application-service-card__status">Coming next</span></article>
      <article className="application-service-card"><div className="application-service-card__number">03</div><h2>Payments</h2><p>Keep payment methods and receipts in one private place as services come online.</p><span className="application-service-card__status">Coming next</span></article>
    </section> : null}
    {activeTab === "orders" ? <section className="application-tab-empty"><p className="application-landing__eyebrow">ORDERS</p><h2>Your requests will appear here.</h2><p>When ordering is available, this is where you will see the progress of every request.</p></section> : null}
    {activeTab === "payments" ? <section className="application-tab-empty"><p className="application-landing__eyebrow">PAYMENTS</p><h2>Payments will stay simple.</h2><p>Payment methods and receipts will be available here when services need them.</p></section> : null}
  </main>;
}

function NearbyDiscoveryPage(): React.JSX.Element {
  const [zones, setZones] = useState<NearbyZone[]>([]);
  const [status, setStatus] = useState<"idle" | "locating" | "loading" | "ready" | "joined">("idle");
  const [error, setError] = useState("");
  const [selected, setSelected] = useState("");
  const [joinMode, setJoinMode] = useState("");
  const [zoneName, setZoneName] = useState("");
  const [zoneRequestSent, setZoneRequestSent] = useState(false);
  const discoveryToken = new URLSearchParams(window.location.search).get("token") ?? "";
  const locationStorageKey = `zonited-onboarding-location:${discoveryToken || "session"}`;
  const [location, setLocation] = useState<{ latitude: number; longitude: number } | null>(() => {
    try {
      const value = JSON.parse(window.sessionStorage.getItem(locationStorageKey) ?? "null") as { latitude?: number; longitude?: number } | null;
      return value && typeof value.latitude === "number" && typeof value.longitude === "number" ? { latitude: value.latitude, longitude: value.longitude } : null;
    } catch {
      return null;
    }
  });

  async function locate(): Promise<void> {
    setError("");
    if (!navigator.geolocation) { setError("Location is not available in this browser. Use the app from a device with location enabled."); return; }
    setStatus("locating");
    navigator.geolocation.getCurrentPosition(
      (position) => {
        const coordinates = { latitude: position.coords.latitude, longitude: position.coords.longitude };
        setLocation(coordinates);
        window.sessionStorage.setItem(locationStorageKey, JSON.stringify(coordinates));
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
      if (!location) {
        setError("Share your location before requesting access.");
        return;
      }
      const result = await applicationAuth.join(selected, location.latitude, location.longitude);
      setJoinMode(result.approval_mode);
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
      {status === "joined" ? <Card title={joinMode === "auto_no_admin" ? "You are in" : "Request sent"} subtitle={joinMode === "auto_no_admin" ? "This Zonity does not have a Zonity admin yet." : "The Zonity admin can review your request."}><p>{joinMode === "auto_no_admin" ? "Your request was approved automatically because there is no active Zonity admin." : "Your request is pending Zonity admin approval."}</p></Card> : null}
      {status !== "joined" && zones.length > 0 ? <Card title="Nearby zones" subtitle="Choose one to request access."><div className="application-zone-list">{zones.map((zone) => <label className="application-zone-option" key={zone.id}><input type="radio" name="zone" value={zone.id} checked={selected === zone.id} onChange={() => setSelected(zone.id)} /><span><strong>{zone.name}</strong><small>{zone.distance_km.toFixed(1)} km away</small></span></label>)}</div><button className="button button--primary" type="button" disabled={!selected} onClick={() => void join()}>Request access</button></Card> : null}
      {status === "ready" && zones.length === 0 ? <Card title="No zones found" subtitle="There are no public zones within 10 km of this location."><p>You cannot create a Zonity from this app. You can request one and the platform team will review and create it if appropriate.</p>{zoneRequestSent ? <p className="message">Your Zonity request was submitted for review.</p> : <form className="application-zone-request" onSubmit={(event) => { event.preventDefault(); void requestZone(); }}><label className="field-label" htmlFor="zone-request-name">Requested Zonity name</label><input id="zone-request-name" className="text-input" value={zoneName} onChange={(event) => setZoneName(event.target.value)} minLength={2} maxLength={100} required placeholder="Name or locality" /><button className="button button--primary" type="submit" disabled={!zoneName.trim()}>Request a Zonity</button></form>}</Card> : null}
    </div>
  </PageFrame>;
}

