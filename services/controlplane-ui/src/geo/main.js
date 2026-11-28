import * as maplibregl from 'maplibre-gl';
import { clearAuth, loadAuth } from "../auth.ts";
import { client } from "../lib/api.ts";
import workerUrl from 'maplibre-gl/dist/maplibre-gl-worker.mjs?worker&url';
import 'maplibre-gl/dist/maplibre-gl.css';
import { TerraDraw, TerraDrawPolygonMode, TerraDrawSelectMode } from 'terra-draw';
import { TerraDrawMapLibreGLAdapter } from 'terra-draw-maplibre-gl-adapter';
import { createOfflineBasemapStyle, updateOfflineGrid } from './basemap.js';
import { parseCoordinates } from './coordinates.js';
import './style.css';
import './vertices.css';
import './location.css';
maplibregl.setWorkerUrl(workerUrl);

document.querySelector('#geo-app').innerHTML = `
<header><div class="brand"><span class="brand-mark">◈</span><span>GEO-ZONE<small>GEOGRAPHIC WORKSPACE</small></span></div><span class="header-note">Microzone registry <span class="dot">●</span> <button id="sign-out" class="text-button" type="button">Sign out</button></span></header>
<main><aside><div class="section-label">WORKSPACE / 01</div><h1>Microzones<span>.</span></h1><p class="intro">Define the boundaries that connect a place to its community.</p>
<section><div class="section-heading"><h2>Latitude and Longitude</h2><span>WGS 84</span></div><form id="navigate"><label for="coordinates">Coordinates<input id="coordinates" type="text" placeholder="18.5204, 73.8567" autocomplete="off" spellcheck="false" aria-describedby="coordinate-help coordinate-status" required></label><p id="coordinate-help" class="coordinate-help">Paste latitude, longitude separated by a comma or space.</p><div class="coordinate-actions"><button class="secondary" type="submit">Show on map ↗</button><button id="current-location" class="secondary" type="button">◎ Current location</button></div><p id="coordinate-status" class="coordinate-status" role="status" aria-live="polite"></p></form></section>
<section class="registry"><div class="section-heading"><h2>Saved zones</h2><button id="refresh" class="text-button">Refresh ↻</button></div><label class="sr-only" for="search">Search saved zones</label><input id="search" placeholder="Search by name"><div id="zones" class="zone-list">Loading zones…</div></section>
<div class="aside-foot">GEO-ZONE <span> / </span> GEO DATA SERVICES</div></aside>
<div class="workspace"><div id="map" aria-label="Interactive map"></div><div class="map-label"><span>MAP EXPLORER</span><strong>Define a place.</strong><small id="hint">Navigate to a location, then draw its boundary.</small></div>
<div class="tools" role="group" aria-label="Map editing tools"><button id="new" title="Start a new zone">＋ New zone</button><button id="draw" title="Draw a polygon">⌁ Draw</button><button id="edit" title="Select and edit vertices">✥ Edit vertices</button><button id="clear" title="Clear the current boundary">Clear</button></div>
<div class="editor"><div class="section-heading"><div><div class="section-label">ZONE DETAILS</div><h2 id="editor-title">New microzone</h2></div><span class="badge" id="state">UNSAVED</span></div><form id="save"><label>Microzone name<input id="name" maxlength="120" placeholder="e.g. Riverside community" required></label><label>Description <span class="optional">optional</span><textarea id="description" rows="2" placeholder="Add a note about this area"></textarea></label><div id="metrics" class="metrics">Draw a polygon to begin.</div><details><summary>View vertices</summary><pre id="vertices">No vertices yet.</pre></details><div class="actions"><button type="submit" class="primary">Save microzone</button><button id="export" type="button" class="secondary">Export GeoJSON ↓</button></div></form><p id="status" role="status" aria-live="polite"></p></div></div></main>`;

const $ = (selector) => document.querySelector(selector);
async function ensureSession() { try { await client.getSession(); return true; } catch { clearAuth(); window.location.href = '/geo/login?next=' + encodeURIComponent(window.location.pathname); return false; } }
let draw, selected = null, zones = [], dirty = false, loadingZone = false;
function setDirty(value) { dirty = value; $('#state').textContent = value ? 'UNSAVED' : selected ? 'SAVED' : 'UNSAVED'; }
function canDiscard() { return !dirty || window.confirm('Discard your unsaved changes?'); }
window.addEventListener('beforeunload', (event) => { if (dirty) event.preventDefault(); });
const initialCenter = [73.8567, 18.5204];
// Keep the standalone Zone Studio behavior as the usable default. A release
// can override this with its local signed style through VITE_MAP_STYLE; the
// explicit "offline" value remains available for development without tiles.
const defaultMapStyle = 'https://tiles.openfreemap.org/styles/bright';
const configuredMapStyle = import.meta.env.VITE_MAP_STYLE?.trim();
const offlineMap = configuredMapStyle === 'offline';
const mapStyle = offlineMap ? createOfflineBasemapStyle(initialCenter) : (configuredMapStyle || defaultMapStyle);
const map = new maplibregl.Map({container:'map',style:mapStyle,center:initialCenter,zoom:13});
map.addControl(new maplibregl.NavigationControl(), 'bottom-right');
map.addControl(new maplibregl.AttributionControl({customAttribution: offlineMap ? 'Offline coordinate map' : undefined}), 'bottom-left');
if (offlineMap) $('#hint').textContent = 'Offline coordinate map · navigate to a location, then draw its boundary.';
map.on('moveend', () => { if (offlineMap) updateOfflineGrid(map); });
let locationMarker, locationRequest = 0;
function locationStatus(message, error = false) {
  $('#coordinate-status').textContent = message;
  $('#coordinate-status').classList.toggle('error', error);
}
function showLocation(lat, lon) {
  if (!locationMarker) {
    locationMarker = new maplibregl.Marker({color: '#c35222'});
    locationMarker.getElement().setAttribute('aria-label', 'Selected location');
    locationMarker.getElement().setAttribute('title', 'Selected location');
  }
  locationMarker.setLngLat([lon, lat]).addTo(map);
  map.flyTo({center:[lon, lat], zoom:14});
}
map.on('load', () => {
  draw = new TerraDraw({adapter:new TerraDrawMapLibreGLAdapter({map}),modes:[new TerraDrawPolygonMode(),new TerraDrawSelectMode({flags:{polygon:{feature:{draggable:true,coordinates:{draggable:true,midpoints:true,deletable:true}}}}})]});
  draw.start(); draw.setMode('select');
  draw.on('finish', () => { draw.setMode('select'); $('#hint').textContent = 'Select the boundary to move its vertices.'; });
  draw.on('change', () => { if (!loadingZone) setDirty(true); updateGeometryReadout(); });
});
map.on('error', (e) => { if (!map.loaded()) $('#status').textContent = 'The basemap could not load. Check the map provider and network.'; });
function polygon() { return draw?.getSnapshot().find(f => f.geometry.type === 'Polygon' && f.properties.mode === 'polygon'); }
function updateGeometryReadout() {
  const points = polygon()?.geometry.coordinates[0];
  if (points?.length) {
    $('#metrics').textContent = `${Math.max(points.length - 1, 0)} vertices · Save to calculate area`;
    $('#vertices').textContent = points.slice(0, -1).map((p, i) => `${i + 1}. ${p[1].toFixed(6)}, ${p[0].toFixed(6)}`).join('\n');
  } else {
    $('#metrics').textContent = 'Draw a polygon to begin.';
    $('#vertices').textContent = 'No vertices yet.';
  }
}
function status(text, error=false) { $('#status').textContent=text; $('#status').classList.toggle('error',error); }
async function request(path, options = {}, retryAuth = true) { const auth = loadAuth(); const headers = new Headers(options.headers || {}); if (auth?.accessToken) headers.set('Authorization', `Bearer ${auth.accessToken}`); const response = await fetch('/api/v1/geo/' + path, {...options, headers}); if (response.status === 401 && retryAuth && auth?.refreshToken) { try { await client.refresh(); return request(path, options, false); } catch { /* fall through to the login redirect below */ } } if (response.status === 401) { clearAuth(); window.location.href = '/geo/login?next=' + encodeURIComponent(window.location.pathname); throw new Error('Authentication required.'); } if(!response.ok){let data;try{data=await response.json();}catch{data={};}throw new Error(data.message || `Request failed (${response.status})`);} return response.json(); }
async function refresh(){try{zones=await request('zones?order=name.asc');render();}catch(e){status('Could not load zones: '+e.message,true);}}
function render(){const term=$('#search').value.toLowerCase();const list=$('#zones');list.replaceChildren();const matches=zones.filter(z=>z.name.toLowerCase().includes(term));if(!matches.length){list.textContent='No matching microzones yet.';return;}for(const z of matches){const b=document.createElement('button');b.className='zone-item';const title=document.createElement('strong');title.textContent=z.name;const sub=document.createElement('small');sub.textContent=`${Math.round(z.area_sq_m).toLocaleString()} m² · revision ${z.revision}`;b.append(title,sub);b.addEventListener('click',()=>{if(canDiscard())openZone(z);});list.append(b);}}
function openZone(z){if(!draw){status('Map is still loading.',true);return;}const geom=z.geometry.type==='MultiPolygon'&&z.geometry.coordinates.length===1?{type:'Polygon',coordinates:z.geometry.coordinates[0]}:z.geometry;if(geom.type!=='Polygon'){status('Editing multiple polygon parts is not yet supported.',true);return;}loadingZone=true;draw.clear();const result=draw.addFeatures([{type:'Feature',id:z.id,properties:{mode:'polygon'},geometry:geom}]);loadingZone=false;if(result.some(r=>r.valid===false)){status('This boundary cannot be opened in the editor.',true);return;}selected=z;$('#name').value=z.name;$('#description').value=z.description;$('#editor-title').textContent=z.name;setDirty(false);updateGeometryReadout();$('#metrics').textContent=`${Math.round(z.area_sq_m).toLocaleString()} m² · ${Math.round(z.perimeter_m).toLocaleString()} m perimeter`;const bounds=new maplibregl.LngLatBounds();for(const ring of geom.coordinates) for(const point of ring)bounds.extend(point);map.fitBounds(bounds,{padding:100,maxZoom:16});draw.setMode('select');status('Zone loaded. Select the boundary to edit.');}
$('#navigate').addEventListener('submit', e => {
  e.preventDefault();
  ++locationRequest;
  $('#current-location').disabled = false;
  try {
    const {lat, lon} = parseCoordinates($('#coordinates').value);
    $('#coordinates').removeAttribute('aria-invalid');
    showLocation(lat, lon);
    locationStatus('Location marked on the map.');
  } catch (error) {
    $('#coordinates').setAttribute('aria-invalid', 'true');
    locationStatus(error.message, true);
  }
});
$('#coordinates').addEventListener('input', () => {
  ++locationRequest;
  $('#current-location').disabled = false;
  $('#coordinates').removeAttribute('aria-invalid');
  locationStatus('');
});
$('#current-location').addEventListener('click', () => {
  if (!window.isSecureContext || !navigator.geolocation) {
    locationStatus('Current location requires HTTPS or localhost and browser location access.', true);
    return;
  }
  const requestId = ++locationRequest;
  const button = $('#current-location');
  button.disabled = true;
  locationStatus('Finding your location…');
  navigator.geolocation.getCurrentPosition(position => {
    if (requestId !== locationRequest) return;
    button.disabled = false;
    const {latitude, longitude} = position.coords;
    $('#coordinates').value = `${latitude.toFixed(6)}, ${longitude.toFixed(6)}`;
    $('#coordinates').removeAttribute('aria-invalid');
    showLocation(latitude, longitude);
    locationStatus('Current location marked on the map.');
  }, error => {
    if (requestId !== locationRequest) return;
    button.disabled = false;
    const message = error.code === 1 ? 'Location access was denied. Allow it in your browser settings.' : error.code === 3 ? 'Location request timed out. Please try again.' : 'Could not determine your location. Please try again.';
    locationStatus(message, true);
  }, {enableHighAccuracy: true, timeout: 15000, maximumAge: 60000});
});
$('#new').addEventListener('click',()=>{if(!draw||!canDiscard())return;loadingZone=true;draw.clear();loadingZone=false;selected=null;$('#save').reset();setDirty(false);$('#editor-title').textContent='New microzone';updateGeometryReadout();status('');draw.setMode('polygon');});
$('#draw').addEventListener('click',()=>{if(!draw)return;if(polygon()){status('Clear the current polygon before drawing a new one.',true);return;}draw.setMode('polygon');$('#hint').textContent='Click to add vertices. Close the polygon at its first point.';});
$('#edit').addEventListener('click',()=>{if(!draw)return;if(!polygon()){status('Draw a polygon before editing vertices.',true);return;}draw.setMode('select');$('#hint').textContent='Click the boundary to show vertices. Drag points to move them; drag midpoints to add points.';status('Click the boundary to edit its vertices.');});
$('#clear').addEventListener('click',()=>{if(!draw||!canDiscard())return;draw.clear();setDirty(true);updateGeometryReadout();$('#hint').textContent='Boundary cleared. Draw a replacement, or choose New zone for another microzone.';status(selected?'Boundary cleared. Save to replace this zone.':'Boundary cleared. Draw a polygon to continue.');});
$('#name').addEventListener('input',()=>setDirty(true));
$('#description').addEventListener('input',()=>setDirty(true));
$('#refresh').addEventListener('click',refresh);$('#search').addEventListener('input',render);
$('#sign-out').addEventListener('click',async()=>{await client.logout().catch(()=>undefined);window.location.replace('/geo/login');});
$('#save').addEventListener('submit',async e=>{e.preventDefault();const feature=polygon();if(!feature){status('Draw a polygon before saving.',true);return;}try{const id=await request('editor/rpc/save_zone',{method:'POST',headers:{'Content-Type':'application/json'},body:JSON.stringify({p_name:$('#name').value.trim(),p_description:$('#description').value,p_geometry:feature.geometry,p_id:selected?.id||null,p_revision:selected?.revision||null})});await refresh();const saved=zones.find(z=>z.id===id);if(saved)openZone(saved);status('Microzone saved.');}catch(err){status(err.message,true);}});
$('#export').addEventListener('click',()=>{if(!selected||dirty){status('Save your changes before exporting.',true);return;}const blob=new Blob([JSON.stringify({type:'FeatureCollection',features:[{type:'Feature',id:selected.id,properties:{name:selected.name,description:selected.description,revision:selected.revision},geometry:selected.geometry}]},null,2)],{type:'application/geo+json'});const url=URL.createObjectURL(blob);const a=document.createElement('a');a.href=url;a.download=`microzone-${selected.id}.geojson`;a.click();URL.revokeObjectURL(url);});
ensureSession().then((authenticated) => { if (authenticated) refresh(); });
