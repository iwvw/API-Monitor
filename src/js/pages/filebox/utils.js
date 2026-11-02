import { formatDateTime } from '../../modules/utils.js';
import { EXPIRY_OPTIONS } from './constants.js';

export function formatSpeed(bytesPerSecond) {
  if (!Number.isFinite(bytesPerSecond) || bytesPerSecond <= 0) return '-';
  const units = ['B/s', 'KB/s', 'MB/s', 'GB/s'];
  let value = bytesPerSecond;
  let index = 0;
  while (value >= 1024 && index < units.length - 1) {
    value /= 1024;
    index += 1;
  }
  return `${value.toFixed(value >= 10 ? 1 : 2)} ${units[index]}`;
}

export function authHeaders() {
  return {};
}

export function formatExpiry(value) {
  return Number(value) === 0 ? '永久有效' : formatDateTime(value);
}

export function expiryLabel(value) {
  return EXPIRY_OPTIONS.find((item) => item.value === String(value))?.label || `${value} 小时`;
}
