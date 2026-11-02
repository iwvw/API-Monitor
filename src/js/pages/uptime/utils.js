import { ChartPalette } from '@cloudflare/kumo';

export const formatUptimeChartTime = (timestamp) => {
  const date = new Date(timestamp);
  if (Number.isNaN(date.getTime())) return '';
  return `${date.getHours().toString().padStart(2, '0')}:${date.getMinutes().toString().padStart(2, '0')}`;
};

export const formatLatencyAxis = (value) => {
  const latency = Number(value) || 0;
  const abs = Math.abs(latency);
  if (abs >= 1000) return `${(latency / 1000).toFixed(abs >= 10000 ? 0 : 1)}s`;
  return `${Math.round(latency)}ms`;
};

export const normalizeStatusSlug = (value, fallback = 'status') => {
  const text = String(value || fallback).trim().toLowerCase();
  const slug = text.replace(/[^a-z0-9]+/g, '-').replace(/^-+|-+$/g, '');
  return slug || fallback;
};

export const normalizeStatusDomain = (value) => (
  String(value || '')
    .trim()
    .replace(/^https?:\/\//i, '')
    .split('/')[0]
    .replace(/\/+$/g, '')
    .toLowerCase()
);

export const parseUptimeBeatTime = (value) => {
  if (!value) return null;
  if (typeof value === 'number') return Number.isFinite(value) ? value : null;
  const text = String(value).trim();
  if (!text) return null;
  const normalized = /^\d{4}-\d{2}-\d{2} \d{2}:\d{2}:\d{2}/.test(text)
    ? `${text.replace(' ', 'T')}Z`
    : text;
  const timestamp = new Date(normalized).getTime();
  return Number.isFinite(timestamp) ? timestamp : null;
};

export const normalizeUptimeBeat = (beat = {}) => {
  const timestamp = parseUptimeBeatTime(beat.time || beat.created_at || beat.createdAt);
  let status = beat.status;
  if (typeof status === 'number') {
    status = status === 1 ? 'up' : 'down';
  }
  return {
    ...beat,
    status,
    time: timestamp ? new Date(timestamp).toISOString() : beat.time,
    timestamp,
  };
};

export const getUptimeChartColor = (isDarkMode) => ChartPalette.semantic('Success', isDarkMode);

export const getUptimeImportActionMeta = (action) => (
  action === 'update'
    ? { tone: 'warning', label: '更新' }
    : { tone: 'success', label: '创建' }
);

export const buildUptimeImportSections = (preview) => {
  if (!preview) return [];

  const sections = [
    {
      key: 'monitors',
      title: '监测目标',
      description: '按名称、类型和地址匹配。',
      emptyLabel: '本次配置不包含监测目标。',
      items: (preview.monitors || []).map((item, index) => ({
        id: `monitor-${index}-${item.name || 'unnamed'}`,
        label: item.name || '未命名监测',
        detail: item.type ? `类型: ${String(item.type).toUpperCase()}` : '监测配置',
        action: item.action,
      })),
    },
    {
      key: 'statusPages',
      title: '状态页',
      description: '按 slug 匹配。',
      emptyLabel: '本次配置不包含状态页。',
      items: (preview.statusPages || []).map((item, index) => ({
        id: `status-page-${index}-${item.slug || item.title || 'untitled'}`,
        label: item.title || item.slug || '未命名状态页',
        detail: item.slug ? `Slug: ${item.slug}` : '状态页配置',
        action: item.action,
      })),
    },
    {
      key: 'maintenanceWindows',
      title: '维护窗口',
      description: '按标题匹配。',
      emptyLabel: '本次配置不包含维护窗口。',
      items: (preview.maintenanceWindows || []).map((item, index) => ({
        id: `maintenance-${index}-${item.title || 'untitled'}`,
        label: item.title || '未命名维护窗口',
        detail: '维护通知与时间窗口配置',
        action: item.action,
      })),
    },
  ];

  return sections.map((section) => {
    const creates = section.items.filter((item) => item.action !== 'update').length;
    const updates = section.items.length - creates;
    return {
      ...section,
      total: section.items.length,
      creates,
      updates,
    };
  });
};
