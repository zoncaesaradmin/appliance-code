import React, { useEffect, useState } from "react";

export interface MapLocation {
  latitude: number;
  longitude: number;
}

interface MapZone extends MapLocation {
  id: string;
  name: string;
}

interface LocationMapProps {
  location: MapLocation;
  zones?: MapZone[];
  interactive?: boolean;
  onSelect?: (location: MapLocation) => void;
  label?: string;
  /** Initial / reset zoom when the selected location changes. */
  defaultZoom?: number;
}

const TILE_SIZE = 256;
const MIN_ZOOM = 10;
const MAX_ZOOM = 16;
const DEFAULT_ZOOM = 13;

function project(location: MapLocation, zoom: number): { x: number; y: number } {
  const scale = TILE_SIZE * 2 ** zoom;
  const latitude = Math.max(-85.05112878, Math.min(85.05112878, location.latitude));
  const latitudeRadians = latitude * Math.PI / 180;
  return {
    x: (location.longitude + 180) / 360 * scale,
    y: (1 - Math.asinh(Math.tan(latitudeRadians)) / Math.PI) / 2 * scale
  };
}

function unproject(point: { x: number; y: number }, zoom: number): MapLocation {
  const scale = TILE_SIZE * 2 ** zoom;
  const longitude = point.x / scale * 360 - 180;
  const latitudeRadians = Math.atan(Math.sinh(Math.PI * (1 - 2 * point.y / scale)));
  return { latitude: latitudeRadians * 180 / Math.PI, longitude };
}

export function LocationMap(props: LocationMapProps): React.JSX.Element {
  const defaultZoom = props.defaultZoom ?? DEFAULT_ZOOM;
  const [zoom, setZoom] = useState(defaultZoom);
  const center = project(props.location, zoom);
  const gridSize = `${Math.max(1.5, 3.6 - (zoom - MIN_ZOOM) * 0.28)}rem`;

  useEffect(() => {
    setZoom(defaultZoom);
  }, [defaultZoom, props.location.latitude, props.location.longitude]);

  function selectLocation(event: React.MouseEvent<HTMLDivElement>): void {
    if (!props.interactive || !props.onSelect) return;
    const bounds = event.currentTarget.getBoundingClientRect();
    props.onSelect(unproject({
      x: center.x + event.clientX - bounds.left - bounds.width / 2,
      y: center.y + event.clientY - bounds.top - bounds.height / 2
    }, zoom));
  }

  function changeZoom(delta: number, event: React.MouseEvent<HTMLButtonElement>): void {
    event.stopPropagation();
    setZoom((current) => Math.min(MAX_ZOOM, Math.max(MIN_ZOOM, current + delta)));
  }

  return <div
    className={`application-location-map${props.interactive ? " application-location-map--interactive" : ""}`}
    role={props.interactive ? "application" : undefined}
    aria-label={props.label ?? "Location map"}
    onClick={selectLocation}
  >
    <div className="application-location-map__surface" aria-hidden="true" style={{ backgroundSize: `100% 100%, 100% 100%, ${gridSize} ${gridSize}, ${gridSize} ${gridSize}` }} />
    <span className="application-location-map__search-radius" aria-hidden="true" title="Nearby search area" />
    <div className="application-location-map__veil" aria-hidden="true" />
    {props.zones?.map((zone) => {
      const point = project(zone, zoom);
      return <span
        key={zone.id}
        className="application-location-map__zone"
        title={zone.name}
        aria-label={zone.name}
        style={{ left: `calc(50% + ${point.x - center.x}px)`, top: `calc(50% + ${point.y - center.y}px)` }}
      />;
    })}
    <span className="application-location-map__user" aria-label="Selected location" />
    <div className="application-location-map__zoom" role="group" aria-label="Map zoom">
      <button className="application-location-map__zoom-button" type="button" aria-label="Zoom in" disabled={zoom >= MAX_ZOOM} onClick={(event) => changeZoom(1, event)}>+</button>
      <button className="application-location-map__zoom-button" type="button" aria-label="Zoom out" disabled={zoom <= MIN_ZOOM} onClick={(event) => changeZoom(-1, event)}>−</button>
    </div>
    {props.interactive ? <span className="application-location-map__hint">Tap the map to move the pin</span> : null}
    <span className="application-location-map__label">Location preview · z{zoom}</span>
  </div>;
}
