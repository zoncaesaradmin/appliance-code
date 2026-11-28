const DEFAULT_SPAN_DEGREES = 0.25;
const GRID_STEP_DEGREES = 0.01;

function clampLatitude(latitude) {
  return Math.max(-85, Math.min(85, latitude));
}

function normalizeLongitude(longitude) {
  const wrapped = ((longitude + 180) % 360 + 360) % 360 - 180;
  return wrapped === -180 ? 180 : wrapped;
}

function range(start, end, step) {
  const values = [];
  for (let value = start; value <= end + step / 2; value += step) {
    values.push(Number(value.toFixed(6)));
  }
  return values;
}

/**
 * Build a small local coordinate basemap for installations without a
 * geographic tile archive. It keeps the editor useful and makes the selected
 * coordinates meaningful without contacting a public tile provider.
 */
export function createOfflineGrid(center, span = DEFAULT_SPAN_DEGREES) {
  const [longitude, latitude] = center;
  const west = longitude - span;
  const east = longitude + span;
  const south = clampLatitude(latitude - span);
  const north = clampLatitude(latitude + span);
  const features = [];

  for (const longitudeLine of range(west, east, GRID_STEP_DEGREES)) {
    const major = Math.abs(Math.round(longitudeLine * 100) % 5) === 0;
    features.push({
      type: 'Feature',
      properties: { major },
      geometry: {
        type: 'LineString',
        coordinates: [[normalizeLongitude(longitudeLine), south], [normalizeLongitude(longitudeLine), north]]
      }
    });
  }
  for (const latitudeLine of range(south, north, GRID_STEP_DEGREES)) {
    const major = Math.abs(Math.round(latitudeLine * 100) % 5) === 0;
    features.push({
      type: 'Feature',
      properties: { major },
      geometry: {
        type: 'LineString',
        coordinates: [[normalizeLongitude(west), latitudeLine], [normalizeLongitude(east), latitudeLine]]
      }
    });
  }

  return { type: 'FeatureCollection', features };
}

export function createOfflineBasemapStyle(center) {
  return {
    version: 8,
    sources: {
      'offline-coordinate-grid': {
        type: 'geojson',
        data: createOfflineGrid(center)
      }
    },
    layers: [
      {
        id: 'background',
        type: 'background',
        paint: { 'background-color': '#e8e4dc' }
      },
      {
        id: 'offline-grid-minor',
        type: 'line',
        source: 'offline-coordinate-grid',
        filter: ['==', ['get', 'major'], false],
        paint: {
          'line-color': '#c9c4bb',
          'line-width': 1,
          'line-opacity': 0.45
        }
      },
      {
        id: 'offline-grid-major',
        type: 'line',
        source: 'offline-coordinate-grid',
        filter: ['==', ['get', 'major'], true],
        paint: {
          'line-color': '#9d978d',
          'line-width': 1.5,
          'line-opacity': 0.7
        }
      }
    ]
  };
}

export function updateOfflineGrid(map) {
  const source = map.getSource('offline-coordinate-grid');
  if (!source) return;
  const center = map.getCenter();
  source.setData(createOfflineGrid([center.lng, center.lat]));
}
