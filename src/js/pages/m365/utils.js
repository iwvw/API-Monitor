import { SKU_DISPLAY_NAMES, SKU_ID_DISPLAY_NAMES } from './constants.js';

export function getDisplayText(value) {
  if (value === null || value === undefined || value === '') return '-';
  return String(value);
}

export function getSkuDisplayName(skuPartNumber, skuId) {
  const skuName = skuPartNumber || skuId || '';
  if (!skuName) return '-';
  return SKU_DISPLAY_NAMES[skuName] || SKU_ID_DISPLAY_NAMES[skuId] || skuName;
}

export function getSkuDisplayLabel(skuPartNumber, skuId) {
  return getSkuDisplayName(skuPartNumber, skuId);
}

export function getAssignedSkuLabels(assignedLicenses, skuLabelLookup = new Map()) {
  if (!Array.isArray(assignedLicenses) || assignedLicenses.length === 0) return [];
  const labels = [];
  const seen = new Set();
  assignedLicenses.forEach(item => {
    const skuId = String(item?.skuId || '').trim();
    const label = skuLabelLookup.get(skuId) || getSkuDisplayName(item?.skuPartNumber, skuId);
    const normalized = String(label || '').trim();
    if (!normalized || seen.has(normalized)) return;
    seen.add(normalized);
    labels.push(normalized);
  });
  return labels;
}

export function clampPercent(value) {
  if (!Number.isFinite(value)) return 0;
  return Math.max(0, Math.min(100, value));
}

export function formatMetricNumber(value) {
  const numericValue = Number(value);
  if (!Number.isFinite(numericValue)) return '0';
  return numericValue.toLocaleString('en-US', { useGrouping: false });
}

const BYTE_UNITS = ['B', 'KB', 'MB', 'GB', 'TB', 'PB'];

export function formatBytes(value) {
  const numericValue = Number(value);
  if (!Number.isFinite(numericValue) || numericValue <= 0) return '0 B';
  let index = 0;
  let scaled = numericValue;
  while (scaled >= 1024 && index < BYTE_UNITS.length - 1) {
    scaled /= 1024;
    index += 1;
  }
  const digits = index === 0 ? 0 : scaled >= 100 ? 0 : scaled >= 10 ? 1 : 2;
  return `${scaled.toFixed(digits)} ${BYTE_UNITS[index]}`;
}

export function getOneDriveUsageTone(usagePercent) {
  const pct = clampPercent(Number(usagePercent) || 0);
  if (pct >= 90) return 'danger';
  if (pct >= 70) return 'warning';
  return 'success';
}

export function getOneDriveUsagePercent(record) {
  if (!record) return 0;
  const explicit = Number(record.usagePercent);
  if (Number.isFinite(explicit) && explicit >= 0) return clampPercent(explicit);
  const used = Number(record.usedBytes) || 0;
  const total = Number(record.totalBytes) || 0;
  if (total <= 0) return 0;
  return clampPercent((used / total) * 100);
}

export function formatDateOnly(value) {
  if (!value) return '';
  const date = new Date(value);
  if (Number.isNaN(date.getTime())) return '';
  return date.toLocaleDateString('zh-CN', {
    year: 'numeric',
    month: '2-digit',
    day: '2-digit',
  });
}

export function getSkuLifecycleText(sku) {
  const nextLifecycleDateTime = sku?.nextLifecycleDateTime;
  if (!nextLifecycleDateTime) return '';
  const status = String(sku?.subscriptionStatus || '').toLowerCase();
  const label = ['enabled', 'warning'].includes(status) ? '到期' : '生命周期';
  const dateText = formatDateOnly(nextLifecycleDateTime);
  return dateText ? `${label} ${dateText}` : '';
}

export function getDomainFromPrincipalName(value) {
  const text = String(value || '').trim();
  const atIndex = text.indexOf('@');
  if (atIndex < 0 || atIndex === text.length - 1) return '';
  return text.slice(atIndex + 1).trim();
}

export function normalizeDomainValue(value) {
  return String(value || '')
    .trim()
    .toLowerCase();
}

export function getAccountDomainList(account) {
  const domains = new Set();
  if (Array.isArray(account?.verifiedDomains)) {
    account.verifiedDomains.forEach(domain => {
      const normalized = normalizeDomainValue(domain);
      if (normalized) domains.add(normalized);
    });
  }
  const defaultDomain = normalizeDomainValue(account?.defaultDomain);
  if (defaultDomain) {
    domains.add(defaultDomain);
  }
  return Array.from(domains).sort((a, b) => a.localeCompare(b));
}

export function extractOrganizationDomains(organization) {
  if (!organization || !Array.isArray(organization.verifiedDomains)) return [];
  const defaults = [];
  const others = [];
  organization.verifiedDomains.forEach(item => {
    const normalized = normalizeDomainValue(item?.name);
    if (!normalized) return;
    if (item?.isDefault) {
      defaults.push(normalized);
    } else {
      others.push(normalized);
    }
  });
  return Array.from(new Set([...defaults, ...others]));
}

export function getPrincipalLocalPart(value) {
  const text = String(value || '').trim();
  const atIndex = text.indexOf('@');
  if (atIndex <= 0) return text;
  return text.slice(0, atIndex).trim();
}

export function getFriendlyErrorMessage(message, fallback) {
  if (!message) return fallback;
  if (message.includes('Authorization_RequestDenied')) {
    return `${fallback}：当前租户权限不足`;
  }
  if (message.includes('ResourceNotFound')) {
    return `${fallback}：目标资源不存在`;
  }
  return message;
}

export const getAuthHeaders = () => ({
  'Content-Type': 'application/json',
});

export async function parseResponse(response) {
  const payload = await response.json().catch(() => ({}));
  if (!response.ok || payload.success === false) {
    throw new Error(payload.error || '请求失败');
  }
  return payload.data ?? payload;
}

export function downloadJson(filename, payload) {
  const blob = new Blob([JSON.stringify(payload, null, 2)], { type: 'application/json' });
  const url = URL.createObjectURL(blob);
  const link = document.createElement('a');
  link.href = url;
  link.download = filename;
  document.body.appendChild(link);
  link.click();
  document.body.removeChild(link);
  URL.revokeObjectURL(url);
}

export function parseJsonInput(input, fallbackKey) {
  const parsed = JSON.parse(input);
  if (Array.isArray(parsed)) return parsed;
  if (fallbackKey && Array.isArray(parsed[fallbackKey])) return parsed[fallbackKey];
  return parsed;
}

export function getRegistrationTone(status) {
  if (status === 'success') return 'success';
  if (status === 'partial') return 'warning';
  return 'danger';
}

export function getRegistrationStatusLabel(status) {
  if (status === 'success') return '成功';
  if (status === 'partial') return '部分成功';
  return '失败';
}

export function getRegistrationResultText(record) {
  if (record?.errorMessage) return record.errorMessage;
  if (record?.status === 'success') return '已完成创建与分配';
  if (record?.status === 'partial') return '账号已创建，后续步骤部分失败';
  return '创建流程失败';
}
