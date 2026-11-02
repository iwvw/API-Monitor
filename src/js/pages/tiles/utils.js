// TilesBoard 展示与数据纯函数：取值/格式化/时间解析，无副作用。
import { FETCH_TIMEOUT_MS } from './constants.js';

export function fetchWithTimeout(url, options = {}, timeoutMs = FETCH_TIMEOUT_MS) {
  const controller = new AbortController();
  const timer = window.setTimeout(() => controller.abort(), timeoutMs);
  return fetch(url, {
    ...options,
    headers: { 'Content-Type': 'application/json', ...(options.headers || {}) },
    signal: controller.signal,
  }).finally(() => window.clearTimeout(timer));
}

export function toArray(value) {
  if (Array.isArray(value)) return value;
  if (value && Array.isArray(value.data)) return value.data;
  return [];
}

export function fmtCompact(value) {
  const n = Number(value);
  if (!Number.isFinite(n)) return '-';
  if (Math.abs(n) >= 1e9) return `${(n / 1e9).toFixed(2)}B`;
  if (Math.abs(n) >= 1e6) return `${(n / 1e6).toFixed(2)}M`;
  if (Math.abs(n) >= 1e3) return `${(n / 1e3).toFixed(2)}k`;
  return String(Math.round(n));
}

export function fmtPercent(value) {
  const n = Number(value);
  if (!Number.isFinite(n)) return '-';
  return `${n.toFixed(n < 1 ? 2 : 1)}%`;
}

export function fmtMs(value) {
  const n = Number(value);
  if (!Number.isFinite(n)) return '-';
  return `${Math.round(n)} ms`;
}

export function fmtBytes(bytes) {
  const n = Number(bytes);
  if (!Number.isFinite(n)) return '-';
  if (n >= 1e9) return `${(n / 1e9).toFixed(1)} GB`;
  if (n >= 1e6) return `${(n / 1e6).toFixed(1)} MB`;
  if (n >= 1e3) return `${(n / 1e3).toFixed(1)} KB`;
  return `${n} B`;
}

export function shortDay(value) {
  const s = String(value || '');
  return /^\d{4}-/.test(s) ? s.slice(5) : s;
}

export function pctDelta(values) {
  const list = values.filter((v) => Number.isFinite(Number(v)));
  if (list.length < 2 || Number(list[0]) === 0) return null;
  return ((Number(list[list.length - 1]) - Number(list[0])) / Math.abs(Number(list[0]))) * 100;
}

export function seriesSummary(values) {
  const nums = (values || []).filter((v) => Number.isFinite(Number(v)) && Number(v) > 0);
  if (!nums.length) return null;
  const max = Math.max(...nums);
  const avg = nums.reduce((a, b) => a + Number(b), 0) / nums.length;
  return { max, avg };
}

export function normalizeServerStatus(status) {
  if (status === 'online') return 'online';
  if (status === 'error' || status === 'interrupted' || status === 'suspect') return 'error';
  return 'offline';
}

export function parseCfTime(point) {
  const ts = new Date(point?.datetime || point?.since || point?.date || '').getTime();
  return Number.isFinite(ts) ? ts : null;
}

export function fmtCfAxisTime(ts, range) {
  const date = new Date(ts);
  if (Number.isNaN(date.getTime())) return '';
  if (range === '24h') return `${String(date.getHours()).padStart(2, '0')}:00`;
  return `${String(date.getMonth() + 1).padStart(2, '0')}/${String(date.getDate()).padStart(2, '0')}`;
}
