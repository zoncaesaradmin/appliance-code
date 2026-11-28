import { describe, expect, it } from 'vitest';
import { createOfflineBasemapStyle, createOfflineGrid } from './basemap.js';

describe('offline basemap', () => {
  it('provides a visible MapLibre style without external sources', () => {
    const style = createOfflineBasemapStyle([77.714703, 12.71865]);

    expect(style.sources['offline-coordinate-grid'].type).toBe('geojson');
    expect(style.layers.map((layer) => layer.id)).toEqual([
      'background',
      'offline-grid-minor',
      'offline-grid-major'
    ]);
    expect(JSON.stringify(style)).not.toContain('http');
  });

  it('keeps the grid centered on the selected location', () => {
    const grid = createOfflineGrid([77.714703, 12.71865]);
    const coordinates = grid.features.flatMap((feature) => feature.geometry.coordinates);

    expect(coordinates.some(([longitude, latitude]) => longitude === 77.714703 && latitude === 12.71865)).toBe(false);
    expect(coordinates.some(([longitude]) => Math.abs(longitude - 77.714703) < 1e-6)).toBe(true);
    expect(coordinates.some(([, latitude]) => Math.abs(latitude - 12.71865) < 1e-6)).toBe(true);
  });
});
