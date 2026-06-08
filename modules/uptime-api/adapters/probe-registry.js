const axios = require('axios');
const net = require('net');
const https = require('https');
const dns = require('dns');
const { promisify } = require('util');
const storage = require('../storage');

const resolveDns = promisify(dns.resolve);

function parseJson(value, fallback = {}) {
  if (!value) return fallback;
  if (typeof value === 'object') return value;
  try {
    return JSON.parse(value);
  } catch (error) {
    return fallback;
  }
}

function parseAcceptedStatusCodes(value) {
  if (!value) return status => status >= 200 && status < 300;
  const ranges = String(value)
    .split(',')
    .map(part => part.trim())
    .filter(Boolean)
    .map(part => {
      if (part.includes('-')) {
        const [min, max] = part.split('-').map(Number);
        return { min, max };
      }
      const exact = Number(part);
      return { min: exact, max: exact };
    })
    .filter(range => Number.isFinite(range.min) && Number.isFinite(range.max));

  if (ranges.length === 0) return status => status >= 200 && status < 300;
  return status => ranges.some(range => status >= range.min && status <= range.max);
}

function normalizeConfig(monitor = {}) {
  return parseJson(monitor.config || monitor.config_json, {});
}

function getJsonPathValue(source, pathExpression) {
  const expression = String(pathExpression || '').trim();
  if (!expression) return source;

  const path = expression
    .replace(/^\$\./, '')
    .replace(/^\$/, '')
    .replace(/\[(\d+)\]/g, '.$1')
    .split('.')
    .map(part => part.trim())
    .filter(Boolean);

  return path.reduce((value, segment) => {
    if (value === null || value === undefined) return undefined;
    return value[segment];
  }, source);
}

function coerceComparable(value) {
  if (value === null || value === undefined) return value;
  if (typeof value === 'boolean' || typeof value === 'number') return value;
  const text = String(value).trim();
  if (/^-?\d+(\.\d+)?$/.test(text)) return Number(text);
  if (text === 'true') return true;
  if (text === 'false') return false;
  if (text === 'null') return null;
  return text;
}

function compareJsonValue(actual, expected, operator = 'equals') {
  const op = operator || 'equals';
  if (op === 'exists') return actual !== undefined && actual !== null;
  if (op === 'not_exists' || op === 'notExists') return actual === undefined || actual === null;

  const normalizedActual = coerceComparable(actual);
  const normalizedExpected = coerceComparable(expected);
  const actualText = typeof actual === 'string' ? actual : JSON.stringify(actual);
  const expectedText = String(expected ?? '');

  switch (op) {
    case 'equals':
    case 'eq':
      return normalizedActual === normalizedExpected;
    case 'not_equals':
    case 'notEquals':
    case 'ne':
      return normalizedActual !== normalizedExpected;
    case 'contains':
      return String(actualText ?? '').includes(expectedText);
    case 'not_contains':
    case 'notContains':
      return !String(actualText ?? '').includes(expectedText);
    case 'gt':
    case 'greater_than':
    case 'greaterThan':
      return Number(normalizedActual) > Number(normalizedExpected);
    case 'gte':
    case 'greater_or_equal':
    case 'greaterOrEqual':
      return Number(normalizedActual) >= Number(normalizedExpected);
    case 'lt':
    case 'less_than':
    case 'lessThan':
      return Number(normalizedActual) < Number(normalizedExpected);
    case 'lte':
    case 'less_or_equal':
    case 'lessOrEqual':
      return Number(normalizedActual) <= Number(normalizedExpected);
    case 'regex':
      return new RegExp(expectedText).test(String(actualText ?? ''));
    default:
      return normalizedActual === normalizedExpected;
  }
}

function tcpConnect(hostname, port, timeoutSeconds = 10) {
  return new Promise((resolve, reject) => {
    const socket = new net.Socket();
    socket.setTimeout(timeoutSeconds * 1000);
    socket.on('connect', () => {
      socket.destroy();
      resolve();
    });
    socket.on('timeout', () => {
      socket.destroy();
      reject(new Error('Connection Timeout'));
    });
    socket.on('error', err => {
      socket.destroy();
      reject(err);
    });
    socket.connect(port, hostname);
  });
}

async function requestHttp(monitor) {
  const started = Date.now();
  const agent = new https.Agent({ rejectUnauthorized: !monitor.ignoreTls });
  const headers = parseJson(monitor.headers, {});
  const response = await axios({
    url: monitor.url,
    method: monitor.method || 'GET',
    timeout: (monitor.timeout || 30) * 1000,
    headers,
    data: monitor.body || undefined,
    httpsAgent: agent,
    validateStatus: () => true,
  });

  return {
    response,
    latencyMs: Date.now() - started,
  };
}

function validateHttpResponse(monitor, response) {
  const accepted = parseAcceptedStatusCodes(monitor.accepted_status_codes);
  if (!accepted(response.status)) {
    throw new Error(`HTTP ${response.status} not accepted`);
  }

  if (monitor.type === 'keyword' && monitor.keyword) {
    const body = typeof response.data === 'string' ? response.data : JSON.stringify(response.data);
    if (!body.includes(monitor.keyword)) {
      throw new Error(`Keyword not found: ${monitor.keyword}`);
    }
  }
}

function buildHttpSuccess(response, latencyMs, extraDetails = {}) {
  const baseDetails = { contentLength: String(response.data || '').length };
  return {
    ok: true,
    status: 'up',
    latencyMs,
    message: 'OK',
    statusCode: response.status,
    details: { ...baseDetails, ...extraDetails },
  };
}

async function httpProbe(monitor) {
  const { response, latencyMs } = await requestHttp(monitor);
  validateHttpResponse(monitor, response);

  return buildHttpSuccess(response, latencyMs);
}

async function jsonQueryProbe(monitor) {
  const { response, latencyMs } = await requestHttp(monitor);
  validateHttpResponse({ ...monitor, type: 'http' }, response);
  const config = normalizeConfig(monitor);
  const pathExpression = config.jsonQueryPath || config.jsonPath || config.query || monitor.keyword;
  const operator = config.jsonQueryOperator || config.operator || 'equals';
  const expected = config.jsonExpectedValue ?? config.expectedValue ?? monitor.expectedValue;

  const actual = getJsonPathValue(response.data, pathExpression);
  if (!compareJsonValue(actual, expected, operator)) {
    throw new Error(`JSON Query mismatch at ${pathExpression || '$'}: expected ${operator} ${expected ?? ''}`);
  }

  return buildHttpSuccess(response, latencyMs, {
    jsonQueryPath: pathExpression || '$',
    jsonQueryOperator: operator,
    jsonQueryActual: actual,
  });
}

async function tcpProbe(monitor) {
  const started = Date.now();
  await tcpConnect(monitor.hostname, monitor.port, monitor.timeout || 10);
  return {
    ok: true,
    status: 'up',
    latencyMs: Date.now() - started,
    message: 'OK',
  };
}

async function pingProbe(monitor) {
  const ports = [80, 443, 53];
  let lastError = null;
  for (const port of ports) {
    try {
      return await tcpProbe({ ...monitor, port, timeout: Math.min(monitor.timeout || 2, 2) });
    } catch (error) {
      lastError = error;
    }
  }
  throw new Error(`Ping TCP fallback failed: ${lastError?.message || 'unreachable'}`);
}

async function dnsProbe(monitor) {
  const started = Date.now();
  const resolver = new dns.Resolver();
  if (monitor.dns_resolve_server) {
    resolver.setServers([monitor.dns_resolve_server]);
  }
  const method = promisify(resolver.resolve.bind(resolver));
  const type = monitor.dns_resolve_type || 'A';
  const records = await method(monitor.hostname, type);
  const expected = monitor.keyword || monitor.expectedValue || monitor.config?.expectedValue;
  if (expected && !JSON.stringify(records).includes(expected)) {
    throw new Error(`DNS expected value not found: ${expected}`);
  }
  return {
    ok: true,
    status: 'up',
    latencyMs: Date.now() - started,
    message: 'OK',
    details: { records, type },
  };
}

async function pushProbe(monitor) {
  const started = Date.now();
  const config = normalizeConfig(monitor);
  const graceSeconds = Number(monitor.pushGraceSeconds || monitor.push_grace_seconds || config.graceSeconds || 120);
  const lastPush = storage.getLastHeartbeat(monitor.id);
  const lastTime = lastPush?.time || lastPush?.created_at;
  const ageMs = lastTime ? Date.now() - new Date(lastTime).getTime() : Infinity;

  if (!lastPush || Number(lastPush.status) !== 1 || ageMs > graceSeconds * 1000) {
    throw new Error(lastPush ? `Push heartbeat overdue (${Math.round(ageMs / 1000)}s > ${graceSeconds}s)` : 'Push heartbeat missing');
  }

  return {
    ok: true,
    status: 'up',
    latencyMs: Date.now() - started,
    message: 'Push heartbeat received',
    details: {
      lastPushAt: lastTime,
      ageSeconds: Math.round(ageMs / 1000),
      graceSeconds,
    },
  };
}

const adapters = new Map();

function register(type, adapter) {
  adapters.set(type, adapter);
}

function get(type) {
  const adapter = adapters.get(type);
  if (!adapter) {
    throw new Error(`Unsupported monitor type: ${type}`);
  }
  return adapter;
}

async function check(monitor, context = {}) {
  const adapter = get(monitor.type || 'http');
  return adapter(monitor, context);
}

register('http', httpProbe);
register('keyword', httpProbe);
register('json', jsonQueryProbe);
register('json-query', jsonQueryProbe);
register('tcp', tcpProbe);
register('ping', pingProbe);
register('dns', dnsProbe);
register('push', pushProbe);

module.exports = {
  register,
  get,
  check,
  parseAcceptedStatusCodes,
  parseJson,
  getJsonPathValue,
  compareJsonValue,
  resolveDns,
};
