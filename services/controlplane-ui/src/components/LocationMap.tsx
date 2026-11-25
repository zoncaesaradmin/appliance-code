import React from "react";

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
}

const TILE_SIZE = 256;
const ZOOM = 12;

function project(location: MapLocation): { x: number; y: number } {
  const scale = TILE_SIZE * 2 ** ZOOM;
  const latitude = Math.max(-85.05112878, Math.min(85.05112878, location.latitude));
  const latitudeRadians = latitude * Math.PI / 180;
  return {
    x: (location.longitude + 180) / 360 * scale,
    y: (1 - Math.asinh(Math.tan(latitudeRadians)) / Math.PI) / 2 * scale
  };
}

function unproject(point: { x: number; y: number }): MapLocation {
  const scale = TILE_SIZE * 2 ** ZOOM;
  const longitude = point.x / scale * 360 - 180;
  const latitudeRadians = Math.atan(Math.sinh(Math.PI * (1 - 2 * point.y / scale)));
  return { latitude: latitudeRadians * 180 / Math.PI, longitude };
}

function tileURL(x: number, y: number): string {
  const tiles = 2 ** ZOOM;
  const wrappedX = ((x % tiles) + tiles) % tiles;
  return `https://tile.openstreetmap.org/${ZOOM}/${wrappedX}/${y}.png`;
}

export function LocationMap(props: LocationMapProps): React.JSX.Element {
  const center = project(props.location);
  const centerTileX = Math.floor(center.x / TILE_SIZE);
  const centerTileY = Math.floor(center.y / TILE_SIZE);
  const tiles = [];
  for (let offsetY = -2; offsetY <= 2; offsetY += 1) {
    for (let offsetX = -2; offsetX <= 2; offsetX += 1) {
      const tileX = centerTileX + offsetX;
      const tileY = centerTileY + offsetY;
      if (tileY < 0 || tileY >= 2 ** ZOOM) continue;
      tiles.push(<img key={`${tileX}:${tileY}`} className="application-location-map__tile" src={tileURL(tileX, tileY)} alt="" aria-hidden="true" style={{ left: `calc(50% + ${tileX * TILE_SIZE - center.x}px)`, top: `calc(50% + ${tileY * TILE_SIZE - center.y}px)` }} />);
    }
  }

  function selectLocation(event: React.MouseEvent<HTMLDivElement>): void {
    if (!props.interactive || !props.onSelect) return;
    const bounds = event.currentTarget.getBoundingClientRect();
    props.onSelect(unproject({ x: center.x + event.clientX - bounds.left - bounds.width / 2, y: center.y + event.clientY - bounds.top - bounds.height / 2 }));
  }

  return <div className={`application-location-map${props.interactive ? " application-location-map--interactive" : ""}`} role={props.interactive ? "application" : undefined} aria-label={props.label ?? "Location map"} onClick={selectLocation}>
    {tiles}
    <div className="application-location-map__veil" aria-hidden="true" />
    {props.zones?.map((zone) => {
      const point = project(zone);
      return <span key={zone.id} className="application-location-map__zone" title={zone.name} aria-label={zone.name} style={{ left: `calc(50% + ${point.x - center.x}px)`, top: `calc(50% + ${point.y - center.y}px)` }} />;
    })}
    <span className="application-location-map__user" aria-label="Selected location" />
    {props.interactive ? <span className="application-location-map__hint">Tap the map to choose another point</span> : null}
    <a className="application-location-map__attribution" href="https://www.openstreetmap.org/copyright" target="_blank" rel="noreferrer">© OpenStreetMap</a>
  </div>;
}
