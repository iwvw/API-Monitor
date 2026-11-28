// 华为云页面纯函数：请求头、响应解包、格式化、状态色档。
import { getDisplayTimeZone } from '../../modules/utils.js';

export const getAuthHeaders = () => ({
  'Content-Type': 'application/json',
});

export const unwrap = (result) => result?.data ?? result ?? {};

const cycleFormatterCache = new Map();

const cycleFormatter = (timeZone) => {
  if (!timeZone || timeZone === 'system') return null;
  if (!cycleFormatterCache.has(timeZone)) {
    try {
      cycleFormatterCache.set(timeZone, new Intl.DateTimeFormat('en-CA', { timeZone, year: 'numeric', month: '2-digit' }));
    } catch {
      cycleFormatterCache.set(timeZone, null);
    }
  }
  return cycleFormatterCache.get(timeZone);
};

const currentCycleParts = () => {
  const formatter = cycleFormatter(getDisplayTimeZone());
  if (formatter) {
    const parts = formatter.formatToParts(new Date()).reduce((acc, part) => {
      acc[part.type] = part.value;
      return acc;
    }, {});
    const year = Number(parts.year);
    const month = Number(parts.month);
    if (Number.isFinite(year) && Number.isFinite(month)) return { year, month };
  }
  const now = new Date();
  return { year: now.getFullYear(), month: now.getMonth() + 1 };
};

export const billCycleMonths = (count = 6) => {
  const { year, month } = currentCycleParts();
  const options = [];
  for (let i = 0; i < count; i += 1) {
    const zeroBased = month - 1 - i;
    const y = year + Math.floor(zeroBased / 12);
    const m = ((zeroBased % 12) + 12) % 12;
    const value = `${y}-${String(m + 1).padStart(2, '0')}`;
    options.push({ value, label: value });
  }
  return options;
};

export const currentBillCycle = () => billCycleMonths(1)[0].value;

export const formatBytes = (bytes) => {
  if (!bytes && bytes !== 0) return '-';
  if (bytes < 1024) return `${bytes} B`;
  const units = ['KB', 'MB', 'GB', 'TB'];
  let value = bytes;
  let unit = -1;
  do { value /= 1024; unit += 1; } while (value >= 1024 && unit < units.length - 1);
  return `${value.toFixed(1)} ${units[unit]}`;
};

export const formatMoney = (value) => {
  const num = Number(value) || 0;
  return num.toFixed(2);
};

export function getStatusTone(status) {
  const normalized = String(status || '').trim().toUpperCase();
  if (['ACTIVE', 'SUCCESS', 'IN_USE', 'RUNNING', 'NORMAL', 'BIND', 'ENABLE'].includes(normalized)) return 'success';
  if (['SHUTOFF', 'STOPPED', 'FREE', 'DISABLE', 'ERROR', 'REBOOT'].includes(normalized)) return 'danger';
  if (['REBOOT', 'BUILD', 'STOPPING', 'PENDING'].includes(normalized)) return 'info';
  return 'neutral';
}
