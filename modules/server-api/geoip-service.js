const net = require('net');
const geoip = require('geoip-lite');

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

  const record = geoip.lookup(ip);
  const country = record?.country;
  return country ? country.toLowerCase() : null;
};

module.exports = {
  lookupCountryByIp,
  normalizeIp,
};
