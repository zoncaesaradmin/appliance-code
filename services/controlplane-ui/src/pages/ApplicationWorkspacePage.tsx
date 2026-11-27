import React, { useEffect, useState } from "react";
import { Card, LocationMap, PageFrame, type MapLocation } from "../components";
import { navigate } from "../lib/navigate";
import { applicationAuth, presentedAuthError, requiresSignIn, type ActiveMembership, type NearbyZone } from "../lib/applicationAuth";
import type { Session } from "../types";

type GeoLocation = MapLocation & { accuracy?: number };

function formatLocation(location: GeoLocation): string {
  const coordinates = `${location.latitude.toFixed(4)}°, ${location.longitude.toFixed(4)}°`;
  return location.accuracy && Number.isFinite(location.accuracy) ? `${coordinates} · accuracy about ${Math.round(location.accuracy)} m` : coordinates;
}

/**
 * First application PWA shell. Business screens can be added here without
 * changing the operator console route tree or its API contracts.
 */
export function ApplicationWorkspacePage(props: { pathname: string; session: Session }): React.JSX.Element {
  if (props.pathname === "/app/discover") {
    return <NearbyDiscoveryPage />;
  }
  return <ApplicationLandingPage />;
}

function ApplicationLandingPage(): React.JSX.Element {
  const [activeTab, setActiveTab] = useState<"services" | "orders" | "payments">("services");
  const [memberships, setMemberships] = useState<ActiveMembership[]>([]);
  const [membershipLoading, setMembershipLoading] = useState(true);
  const [membershipError, setMembershipError] = useState("");
  const [joinDialogOpen, setJoinDialogOpen] = useState(false);

  async function refreshMemberships(): Promise<void> {
    setMembershipLoading(true);
    setMembershipError("");
    try {
      const result = await applicationAuth.memberships();
      setMemberships(result.memberships);
    } catch (reason: unknown) {
      setMembershipError(presentedAuthError(reason, "Could not load your Zonity memberships."));
    } finally {
      setMembershipLoading(false);
    }
  }

  useEffect(() => {
    void refreshMemberships();
  }, []);

  const hasMembership = memberships.length > 0;
  return <main className="application-landing">
    <section className="application-landing__intro" aria-labelledby="application-landing-title">
      <div>
        <p className="application-landing__eyebrow">YOUR ZONITED HOME</p>
        <h1 id="application-landing-title">Discover local services.</h1>
      </div>
    </section>
    {membershipLoading ? <p className="application-membership-loading">Loading your Zonity access…</p> : hasMembership ? <>
      <ActiveMembershipSummary memberships={memberships} />
      <nav className="application-landing__tabs" aria-label="Application sections" role="tablist">
        {([['services', 'Services'], ['orders', 'Orders'], ['payments', 'Payments']] as const).map(([id, label]) => <button key={id} type="button" role="tab" aria-selected={activeTab === id} className={activeTab === id ? "application-landing__tab application-landing__tab--active" : "application-landing__tab"} onClick={() => setActiveTab(id)}>{label}</button>)}
      </nav>
      {activeTab === "services" ? <section className="application-service-grid" aria-label="Services">
        <article className="application-service-card application-service-card--primary"><div className="application-service-card__number">01</div><h2>Nearby Zonities</h2><p>Share your location to see public Zonities within 10 km and request access when you are close enough.</p><button className="application-service-card__action" type="button" onClick={() => setJoinDialogOpen(true)}>Find nearby <span aria-hidden="true">↗</span></button></article>
        <article className="application-service-card"><div className="application-service-card__number">02</div><h2>Ordering</h2><p>Browse local offerings and send a request when ordering becomes available in your Zonity.</p><span className="application-service-card__status">Coming next</span></article>
        <article className="application-service-card"><div className="application-service-card__number">03</div><h2>Payments</h2><p>Keep payment methods and receipts in one private place as services come online.</p><span className="application-service-card__status">Coming next</span></article>
      </section> : null}
      {activeTab === "orders" ? <section className="application-tab-empty"><p className="application-landing__eyebrow">ORDERS</p><h2>Your requests will appear here.</h2><p>When ordering is available, this is where you will see the progress of every request.</p></section> : null}
      {activeTab === "payments" ? <section className="application-tab-empty"><p className="application-landing__eyebrow">PAYMENTS</p><h2>Payments will stay simple.</h2><p>Payment methods and receipts will be available here when services need them.</p></section> : null}
    </> : <section className="application-join-prompt" aria-labelledby="application-join-title">
      <p className="application-landing__eyebrow">GET STARTED</p>
      <h2 id="application-join-title">Explore and join a Zonity near you.</h2>
      {membershipError ? <p className="form-error" role="alert">{membershipError}</p> : null}
      <button className="application-join-prompt__action" type="button" onClick={() => setJoinDialogOpen(true)}>Find a Zonity <span aria-hidden="true">↗</span></button>
    </section>}
    {joinDialogOpen ? <NearbyJoinDialog onClose={() => setJoinDialogOpen(false)} onJoined={() => void refreshMemberships()} /> : null}
  </main>;
}

function ActiveMembershipSummary(props: { memberships: ActiveMembership[] }): React.JSX.Element {
  const [primary, ...more] = props.memberships;
  return <section className="application-membership-summary" aria-label="Active Zonity memberships">
    <div className="application-membership-summary__status"><span className="application-membership-summary__dot" aria-hidden="true" />Active</div>
    <div className="application-membership-summary__tile" title={primary.name}>
      <span className="application-membership-summary__initials" aria-hidden="true">{primary.name.trim().slice(0, 2).toUpperCase()}</span>
      <strong>{primary.name}</strong>
      {more.length > 0 ? <span className="application-membership-summary__more">+{more.length} more</span> : null}
    </div>
  </section>;
}

function NearbyJoinDialog(props: { onClose: () => void; onJoined: () => void }): React.JSX.Element {
  const [zones, setZones] = useState<NearbyZone[]>([]);
  const [status, setStatus] = useState<"locating" | "loading" | "ready" | "joined">("locating");
  const [selected, setSelected] = useState("");
  const [joinMode, setJoinMode] = useState("");
  const [error, setError] = useState("");
  const [location, setLocation] = useState<GeoLocation | null>(null);
  const [choosingAnother, setChoosingAnother] = useState(false);
  const [locationNeedsSearch, setLocationNeedsSearch] = useState(false);

  async function findNearby(nextLocation: GeoLocation): Promise<void> {
    setLocation(nextLocation);
    setSelected("");
    setError("");
    setChoosingAnother(false);
    setLocationNeedsSearch(false);
    setStatus("loading");
    try {
      const result = await applicationAuth.nearby(nextLocation.latitude, nextLocation.longitude);
      setZones(result.zones);
      setStatus("ready");
    } catch (reason: unknown) {
      setError(presentedAuthError(reason, "Could not find nearby Zonities."));
      setStatus("ready");
    }
  }

  async function locate(): Promise<void> {
    setError("");
    setChoosingAnother(false);
    if (!navigator.geolocation) {
      setError("Location is not available in this browser.");
      setStatus("ready");
      return;
    }
    setStatus("locating");
    navigator.geolocation.getCurrentPosition(
      (position) => {
        void findNearby({ latitude: position.coords.latitude, longitude: position.coords.longitude, accuracy: position.coords.accuracy });
      },
      () => { setError("We could not read your location. Allow location access and try again."); setStatus("ready"); },
      { enableHighAccuracy: true, maximumAge: 60000, timeout: 15000 }
    );
  }

  useEffect(() => {
    void locate();
  }, []);

  async function join(): Promise<void> {
    if (!selected || !location) return;
    setError("");
    try {
      const result = await applicationAuth.join(selected, location.latitude, location.longitude);
      setJoinMode(result.approval_mode);
      setStatus("joined");
      props.onJoined();
    } catch (reason: unknown) {
      setError(presentedAuthError(reason, "Could not submit the join request."));
    }
  }

  function startMapPick(): void {
    if (!location) {
      void locate();
      return;
    }
    setChoosingAnother(true);
    setLocationNeedsSearch(false);
    setError("");
    setStatus("ready");
  }

  function selectAnother(nextLocation: MapLocation): void {
    setChoosingAnother(true);
    setLocation({ ...nextLocation });
    setLocationNeedsSearch(true);
    setZones([]);
    setSelected("");
  }

  const mapInteractive = Boolean(location) && status !== "joined" && status !== "locating" && status !== "loading";
  const showLocationActions = status !== "joined" && status !== "locating" && status !== "loading";

  return <div className="application-modal-scrim" role="presentation" onMouseDown={(event) => { if (event.target === event.currentTarget) props.onClose(); }}>
    <section className="application-modal" role="dialog" aria-modal="true" aria-labelledby="application-nearby-title">
      <header className="application-modal__header">
        <div><p className="application-landing__eyebrow">ZONITY ACCESS</p><h2 id="application-nearby-title">Explore nearby</h2></div>
        <button className="application-modal__close" type="button" onClick={props.onClose} aria-label="Close">×</button>
      </header>
      <p className="application-modal__description">Search from your current location, or move the pin on the map and search again.</p>
      {status === "locating" || status === "loading" ? <p className="application-modal__status">{status === "locating" ? "Getting your location…" : "Finding Zonities near you…"}</p> : null}
      {location ? <div className="application-location-panel">
        <LocationMap location={location} zones={locationNeedsSearch ? [] : zones} interactive={mapInteractive} onSelect={selectAnother} label="Location checked for nearby Zonities" />
        <p className="application-location-panel__details">Location checked: {formatLocation(location)}.</p>
        {locationNeedsSearch ? <p className="application-location-panel__hint">Pin moved. Search from this pinned location when you are ready.</p> : choosingAnother ? <p className="application-location-panel__hint">Tap the map to place the pin, then search from that location.</p> : <p className="application-location-panel__hint">Zoom to confirm the place, or tap the map to choose another point.</p>}
      </div> : null}
      {locationNeedsSearch ? <button className="button button--primary application-modal__primary" type="button" onClick={() => { if (location) void findNearby(location); }}>Search from pinned location</button> : null}
      {status === "ready" && zones.length > 0 && !locationNeedsSearch ? <>
        <p className="application-modal__status">Choose a Zonity to request access.</p>
        <div className="application-zone-list">{zones.map((zone) => <label className="application-zone-option" key={zone.id}><input type="radio" name="landing-zone" value={zone.id} checked={selected === zone.id} onChange={() => setSelected(zone.id)} /><span><strong>{zone.name}</strong><small>{zone.distance_km.toFixed(1)} km away</small></span></label>)}</div>
        <button className="button button--primary application-modal__primary" type="button" disabled={!selected} onClick={() => void join()}>Request access</button>
      </> : null}
      {status === "ready" && zones.length === 0 && !locationNeedsSearch ? <p className="application-modal__empty">There are no Zonities within 10 km of this location yet. Move the pin or use your current location to try again.</p> : null}
      {status === "joined" ? <p className="application-modal__empty">{joinMode === "auto_no_admin" ? "You joined this Zonity." : "Your request was sent to the Zonity admin."}</p> : null}
      {error ? <p className="form-error" role="alert">{error}</p> : null}
      <footer className="application-modal__footer">
        {showLocationActions ? <button className="button button--ghost application-modal__action" type="button" onClick={() => void locate()}>Search near me</button> : null}
        {showLocationActions && location ? <button className="button button--ghost application-modal__action" type="button" onClick={startMapPick}>Move pin on map</button> : null}
        <button className="button button--ghost application-modal__action" type="button" onClick={props.onClose}>{status === "joined" ? "Done" : "Not now"}</button>
      </footer>
    </section>
  </div>;
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
  const [location, setLocation] = useState<GeoLocation | null>(() => {
    try {
      const value = JSON.parse(window.sessionStorage.getItem(locationStorageKey) ?? "null") as { latitude?: number; longitude?: number } | null;
      return value && typeof value.latitude === "number" && typeof value.longitude === "number" ? { latitude: value.latitude, longitude: value.longitude } : null;
    } catch {
      return null;
    }
  });
  const [choosingAnother, setChoosingAnother] = useState(false);
  const [locationNeedsSearch, setLocationNeedsSearch] = useState(false);

  async function findNearby(nextLocation: GeoLocation): Promise<void> {
    setLocation(nextLocation);
    setError("");
    setChoosingAnother(false);
    setLocationNeedsSearch(false);
    setStatus("loading");
    window.sessionStorage.setItem(locationStorageKey, JSON.stringify(nextLocation));
    try {
      const result = await applicationAuth.nearby(nextLocation.latitude, nextLocation.longitude, discoveryToken);
      setZones(result.zones);
      setStatus("ready");
    } catch (reason: unknown) {
      setError(presentedAuthError(reason, "Could not find nearby Zonities."));
      setStatus("idle");
    }
  }

  async function locate(): Promise<void> {
    setError("");
    setChoosingAnother(false);
    if (!navigator.geolocation) { setError("Location is not available in this browser. Use the app from a device with location enabled."); return; }
    setStatus("locating");
    navigator.geolocation.getCurrentPosition(
      (position) => {
        void findNearby({ latitude: position.coords.latitude, longitude: position.coords.longitude, accuracy: position.coords.accuracy });
      },
      () => { setError("We could not read your location. Allow location access and try again."); setStatus("idle"); },
      { enableHighAccuracy: true, maximumAge: 60000, timeout: 15000 }
    );
  }

  function startMapPick(): void {
    if (!location) {
      void locate();
      return;
    }
    setChoosingAnother(true);
    setLocationNeedsSearch(false);
    setError("");
    setStatus("ready");
  }

  function selectAnother(nextLocation: MapLocation): void {
    setChoosingAnother(true);
    setLocation({ ...nextLocation });
    setLocationNeedsSearch(true);
    setZones([]);
    setSelected("");
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
      if (requiresSignIn(reason)) {
        navigate(`/app/login?next=${encodeURIComponent(window.location.pathname + window.location.search)}`);
        return;
      }
      setError(presentedAuthError(reason, "Could not submit the join request."));
    }
  }

  async function requestZone(): Promise<void> {
    setError("");
    try {
      await applicationAuth.session();
      await applicationAuth.requestZone(zoneName.trim());
      setZoneRequestSent(true);
    } catch (reason: unknown) {
      if (requiresSignIn(reason)) {
        navigate(`/app/login?next=${encodeURIComponent(window.location.pathname + window.location.search)}`);
        return;
      }
      setError(presentedAuthError(reason, "Could not submit the Zonity request."));
    }
  }

  const mapInteractive = Boolean(location) && status !== "joined" && status !== "locating" && status !== "loading";

  return <PageFrame title="Find nearby Zonities" eyebrow="Discovery" description="Search near you, or move the pin on the map and search that place. You choose whether to request access." pathname="/app/discover" onNavigate={navigate} tabs={[]}>
    <div className="application-home">
      <Card title="Your location" subtitle="Coordinates are used only to calculate nearby results.">
        <div className="button-row">
          <button className="button button--primary" type="button" onClick={() => void locate()} disabled={status === "locating" || status === "loading"}>{status === "locating" ? "Getting location…" : status === "loading" ? "Finding nearby…" : "Search near me"}</button>
          {location && status !== "joined" && status !== "locating" && status !== "loading" ? <button className="button button--ghost" type="button" onClick={startMapPick}>Move pin on map</button> : null}
          <button className="button button--ghost" type="button" onClick={() => navigate("/app")}>Back</button>
        </div>
        {location ? <div className="application-location-panel">
          <LocationMap location={location} zones={locationNeedsSearch ? [] : zones} interactive={mapInteractive} onSelect={selectAnother} label="Location checked for nearby Zonities" />
          <p className="application-location-panel__details">Location checked: {formatLocation(location)}.</p>
          {locationNeedsSearch ? <p className="application-location-panel__hint">Pin moved. Search from this pinned location when you are ready.</p> : choosingAnother ? <p className="application-location-panel__hint">Tap the map to place the pin, then search from that location.</p> : <p className="application-location-panel__hint">Zoom to confirm the place, or tap the map to choose another point.</p>}
        </div> : null}
        {locationNeedsSearch ? <button className="button button--primary" type="button" onClick={() => { if (location) void findNearby(location); }}>Search from pinned location</button> : null}
        {error ? <p className="form-error" role="alert">{error}</p> : null}
      </Card>
      {status === "joined" ? <Card title={joinMode === "auto_no_admin" ? "You are in" : "Request sent"} subtitle={joinMode === "auto_no_admin" ? "This Zonity does not have a Zonity admin yet." : "The Zonity admin can review your request."}><p>{joinMode === "auto_no_admin" ? "Your request was approved automatically because there is no active Zonity admin." : "Your request is pending Zonity admin approval."}</p></Card> : null}
      {status !== "joined" && zones.length > 0 ? <Card title="Nearby zones" subtitle="Choose one to request access."><div className="application-zone-list">{zones.map((zone) => <label className="application-zone-option" key={zone.id}><input type="radio" name="zone" value={zone.id} checked={selected === zone.id} onChange={() => setSelected(zone.id)} /><span><strong>{zone.name}</strong><small>{zone.distance_km.toFixed(1)} km away</small></span></label>)}</div><button className="button button--primary" type="button" disabled={!selected} onClick={() => void join()}>Request access</button></Card> : null}
      {status === "ready" && zones.length === 0 && !locationNeedsSearch ? <Card title="No zones found" subtitle="There are no public zones within 10 km of this location."><p>You cannot create a Zonity from this app. You can request one and the platform team will review and create it if appropriate.</p>{zoneRequestSent ? <p className="message">Your Zonity request was submitted for review.</p> : <form className="application-zone-request" onSubmit={(event) => { event.preventDefault(); void requestZone(); }}><label className="field-label" htmlFor="zone-request-name">Requested Zonity name</label><input id="zone-request-name" className="text-input" value={zoneName} onChange={(event) => setZoneName(event.target.value)} minLength={2} maxLength={100} required placeholder="Name or locality" /><button className="button button--primary" type="submit" disabled={!zoneName.trim()}>Request a Zonity</button></form>}</Card> : null}
    </div>
  </PageFrame>;
}

