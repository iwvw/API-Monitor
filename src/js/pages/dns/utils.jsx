import React from 'react';
import { SSL_MODE_LABELS, ZONE_TYPE_LABELS, RECORD_TYPE_BADGE_VARIANTS } from './constants.js';

export function formatDate(value) {
  if (!value) return '-';
  const date = new Date(value);
  if (Number.isNaN(date.getTime())) return '-';
  return date.toLocaleString('zh-CN', {
    year: 'numeric',
    month: '2-digit',
    day: '2-digit',
    hour: '2-digit',
    minute: '2-digit',
  });
}

export function formatBytes(bytes) {
  if (bytes === null || bytes === undefined || bytes === '') return '-';
  const value = Number(bytes || 0);
  if (value >= 1024 ** 3) return `${(value / 1024 ** 3).toFixed(2)} GB`;
  if (value >= 1024 ** 2) return `${(value / 1024 ** 2).toFixed(2)} MB`;
  if (value >= 1024) return `${(value / 1024).toFixed(2)} KB`;
  return `${value} B`;
}

export function r2PreviewKind(key = '') {
  const extension = String(key).split('?')[0].split('#')[0].split('.').pop()?.toLowerCase() || '';
  if (['png', 'jpg', 'jpeg', 'gif', 'webp', 'avif', 'bmp', 'svg'].includes(extension)) return 'image';
  if (['mp4', 'webm', 'mov', 'm4v', 'ogg'].includes(extension)) return 'video';
  if (['mp3', 'wav', 'm4a', 'aac', 'flac', 'oga'].includes(extension)) return 'audio';
  if (extension === 'pdf') return 'frame';
  if ([
    'txt', 'log', 'json', 'xml', 'csv', 'md', 'yaml', 'yml', 'html', 'css', 'js', 'jsx', 'ts', 'tsx',
    'go', 'py', 'sh', 'sql', 'ini', 'env', 'toml',
  ].includes(extension)) return 'frame';
  return 'unknown';
}

export function objectFileName(value = '') {
  const parts = String(value).split('/').filter(Boolean);
  return parts[parts.length - 1] || 'object';
}

export function formatNumber(value) {
  if (value === null || value === undefined || value === '') return '-';
  const number = Number(value || 0);
  if (number >= 1000000) return `${(number / 1000000).toFixed(1)}M`;
  if (number >= 1000) return `${(number / 1000).toFixed(1)}K`;
  return String(number);
}

export function formatNumberAxis(value) {
  const number = Number(value || 0);
  const abs = Math.abs(number);
  if (abs >= 1000000) return `${(number / 1000000).toFixed(abs >= 10000000 ? 0 : 1)}M`;
  if (abs >= 1000) return `${(number / 1000).toFixed(abs >= 10000 ? 0 : 1)}K`;
  return `${Math.round(number)}`;
}

export function toDisplayNumber(value, fallback = 0) {
  const number = Number(value);
  return Number.isFinite(number) ? number : fallback;
}

export function formatPercent(value) {
  const number = toDisplayNumber(value);
  return `${Number.isInteger(number) ? number : number.toFixed(1)}%`;
}

export function parseAnalyticsTimestamp(point) {
  const timestamp = new Date(point?.datetime || point?.since || point?.date || '').getTime();
  return Number.isFinite(timestamp) ? timestamp : null;
}

export function formatAnalyticsAxisTime(timestamp, range = '24h') {
  const date = new Date(timestamp);
  if (Number.isNaN(date.getTime())) return '';
  if (range === '24h') {
    return `${String(date.getHours()).padStart(2, '0')}:00`;
  }
  return `${String(date.getMonth() + 1).padStart(2, '0')}/${String(date.getDate()).padStart(2, '0')}`;
}

export function sslModeLabel(mode) {
  return SSL_MODE_LABELS[mode] || mode || '-';
}

export function zoneTypeLabel(type) {
  return ZONE_TYPE_LABELS[type] || type || '-';
}

export function zoneNameServers(zone = {}) {
  const list = zone.nameServers || zone.name_servers || [];
  return Array.isArray(list) ? list.filter(Boolean) : [];
}

export function recordTypeBadgeVariant(type) {
  return RECORD_TYPE_BADGE_VARIANTS[String(type || '').toUpperCase()] || 'outline';
}

export function DnsPanelCard({ className = '', children }) {
  return (
    <div className={`rounded-lg border border-kumo-line bg-kumo-base shadow-none ${className}`}>
      {children}
    </div>
  );
}

export function recordShortName(name, zoneName) {
  if (!name || !zoneName) return name || '@';
  if (name === zoneName) return '@';
  const suffix = `.${zoneName}`;
  return name.endsWith(suffix) ? name.slice(0, -suffix.length) : name;
}

export function zoneStatusLabel(status) {
  const map = {
    active: '已激活',
    pending: '待验证',
    initializing: '初始化中',
    moved: '已迁移',
    deleted: '已删除',
  };
  return map[status] || status || '未知';
}

export function tunnelStatusLabel(status, connections = []) {
  if (connections.length > 0) return '已连接';
  const map = {
    active: '已连接',
    healthy: '健康',
    inactive: '未连接',
    down: '离线',
    degraded: '降级',
  };
  return map[status] || status || '未知';
}

export function statusVariant(status) {
  if (['active', 'healthy', 'success', 'finished', 'ok'].includes(status)) return 'success';
  if (['error', 'failed', 'down'].includes(status)) return 'error';
  if (['pending', 'initializing', 'queued', 'building', 'degraded'].includes(status)) return 'warning';
  return 'outline';
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
