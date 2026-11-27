// Parse coordinates in latitude, longitude order. Longitude-first GeoJSON is
// deliberately not accepted here because this field is for map copy/paste.
const number = '[+-]?(?:\\d+(?:\\.\\d*)?|\\.\\d+)';
const decimalPair = new RegExp(`^\\s*(${number})(?:\\s*,\\s*|\\s+)(${number})\\s*$`);
const angle = `(${number})\\s*°(?:\\s*(\\d+(?:\\.\\d+)?)\\s*['′](?:\\s*(\\d+(?:\\.\\d+)?)\\s*["″])?)?\\s*([NSEW])?`;
const anglePair = new RegExp(`^\\s*${angle}(?:\\s*,\\s*|\\s+)${angle}\\s*$`, 'i');

export function parseCoordinates(value) {
  const text = value.trim().replace(/[−–]/g, '-');
  let lat, lon;
  const decimals = text.match(decimalPair);
  if (decimals) {
    lat = Number(decimals[1]);
    lon = Number(decimals[2]);
  } else {
    const parts = text.match(anglePair);
    if (!parts) throw new Error('Enter latitude and longitude, separated by a comma or space.');
    const convert = (degrees, minutes, seconds, direction, allowed) => {
      const deg = Number(degrees), min = Number(minutes || 0), sec = Number(seconds || 0);
      const hemisphere = direction?.toUpperCase();
      if (min >= 60 || sec >= 60 || (hemisphere && !allowed.includes(hemisphere)) || (degrees.startsWith('-') && hemisphere)) {
        throw new Error('Check the coordinate directions, minutes and seconds.');
      }
      const negative = degrees.startsWith('-') || hemisphere === 'S' || hemisphere === 'W';
      return (negative ? -1 : 1) * (Math.abs(deg) + min / 60 + sec / 3600);
    };
    lat = convert(parts[1], parts[2], parts[3], parts[4], 'NS');
    lon = convert(parts[5], parts[6], parts[7], parts[8], 'EW');
  }
  if (!Number.isFinite(lat) || !Number.isFinite(lon) || Math.abs(lat) > 90 || Math.abs(lon) > 180) {
    throw new Error('Latitude must be between -90 and 90, and longitude between -180 and 180.');
  }
  return { lat, lon };
}
