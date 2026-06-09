const net = require('net');

let geoip = null;
let geoipLoadFailed = false;

const truthy = new Set(['1', 'true', 'yes', 'on']);
const falsy = new Set(['0', 'false', 'no', 'off']);

function getGeoipMode() {
  const explicit = String(process.env.GEOIP_LOOKUP || process.env.ENABLE_GEOIP || '').trim().toLowerCase();
  if (truthy.has(explicit)) return true;
  if (falsy.has(explicit)) return false;

  // geoip-lite keeps a large binary database in external memory. In small Fly.io
  // style containers, default it off unless explicitly enabled.
  const lowMemoryMode = String(process.env.LOW_MEMORY_MODE || '').trim().toLowerCase();
  if (truthy.has(lowMemoryMode) || process.env.FLY_APP_NAME) return false;

  return true;
}

function getGeoip() {
  if (!getGeoipMode() || geoipLoadFailed) return null;
  if (geoip) return geoip;

  try {
    geoip = require('geoip-lite');
  } catch (error) {
    geoipLoadFailed = true;
    return null;
  }
  return geoip;
}

const normalizeIp = (value = '') => {
  const first = String(value || '').split(',')[0].trim();
  if (!first) return '';
  return first
    .replace(/^\[|\]$/g, '')
    .replace(/^::ffff:/i, '');
};

const isPrivateIp = (ip) => {
  if (!ip || !net.isIP(ip)) return true;
  if (ip === '127.0.0.1' || ip === '::1' || ip === '0.0.0.0') return true;

  if (net.isIPv4(ip)) {
    const parts = ip.split('.').map(Number);
    if (parts[0] === 10) return true;
    if (parts[0] === 127) return true;
    if (parts[0] === 169 && parts[1] === 254) return true;
    if (parts[0] === 172 && parts[1] >= 16 && parts[1] <= 31) return true;
    if (parts[0] === 192 && parts[1] === 168) return true;
    return false;
  }

  const lower = ip.toLowerCase();
  return (
    lower === '::' ||
    lower.startsWith('fc') ||
    lower.startsWith('fd') ||
    lower.startsWith('fe80:')
  );
};

const lookupCountryByIp = (value) => {
  const ip = normalizeIp(value);
  if (!ip || isPrivateIp(ip)) {
    return null;
  }

  const geoipLite = getGeoip();
  if (!geoipLite) return null;

  const record = geoipLite.lookup(ip);
  const country = record?.country;
  return country ? country.toLowerCase() : null;
};

module.exports = {
  lookupCountryByIp,
  normalizeIp,
};
