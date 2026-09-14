import React from 'react';
import { Button } from '@cloudflare/kumo/components/button';

export const getAuthHeaders = () => ({
  'Content-Type': 'application/json',
});

export const unwrap = (result) => result?.data ?? result ?? {};

export function getGcpStatusTone(status) {
  const normalized = String(status || '').trim().toUpperCase();
  if (['RUNNING', 'READY', 'SUCCESS', 'VALID', 'IN_USE', 'ACTIVE', 'DONE', 'RESERVED'].includes(normalized)) return 'success';
  if (['PROVISIONING', 'STAGING', 'STARTING', 'STOPPING', 'CREATING', 'PENDING', 'RESTORING'].includes(normalized)) return 'info';
  if (['TERMINATED', 'STOPPED', 'SUSPENDED', 'UNKNOWN', 'UNVERIFIED'].includes(normalized)) return 'neutral';
  if (['FAILED', 'FAILURE', 'ERROR', 'INVALID', 'DEGRADED'].includes(normalized)) return 'danger';
  return 'neutral';
}

export function getVerifyStatusLabel(status) {
  const normalized = String(status || '').trim().toUpperCase();
  if (normalized === 'SUCCESS') return '已验证';
  if (normalized === 'FAILED') return '失败';
  return '未验证';
}

export function formatGb(value) {
  const num = Number(value);
  if (!Number.isFinite(num)) return '-';
  return `${num} GB`;
}

export function formatMemoryGb(memoryMb) {
  const num = Number(memoryMb);
  if (!Number.isFinite(num) || num <= 0) return '-';
  return `${(num / 1024).toFixed(1)} GB`;
}

export function formatCount(value) {
  const num = Number(value) || 0;
  if (!Number.isFinite(num)) return '0';
  if (num >= 1000000) return `${(num / 1000000).toFixed(1)}M`;
  if (num >= 1000) return `${(num / 1000).toFixed(1)}k`;
  return String(num);
}

export function formatDate(value) {
  if (!value) return '-';
  const date = new Date(value);
  if (Number.isNaN(date.getTime())) return value;
  return date.toLocaleString();
}

export function formatModelUsageAxis(timestamp) {
  const date = new Date(timestamp);
  if (Number.isNaN(date.getTime())) return '';
  return `${date.getMonth() + 1}/${date.getDate()}`;
}

export function formatSize(value) {
  const num = Number(value);
  if (!Number.isFinite(num) || num <= 0) return '-';
  if (num >= 1024 * 1024 * 1024) return `${(num / 1024 / 1024 / 1024).toFixed(2)} GB`;
  if (num >= 1024 * 1024) return `${(num / 1024 / 1024).toFixed(1)} MB`;
  if (num >= 1024) return `${(num / 1024).toFixed(1)} KB`;
  return `${num} B`;
}

export function parseSaSummary(json) {
  if (!json || !json.trim()) return null;
  try {
    const data = JSON.parse(json);
    if (!data || typeof data !== 'object') return null;
    return {
      projectId: data.project_id || '',
      clientEmail: data.client_email || '',
      clientId: data.client_id || '',
      keyId: data.private_key_id || '',
    };
  } catch {
    return null;
  }
}

export function SaSummary({ json, onImport }) {
  const summary = parseSaSummary(json);
  if (!summary) return null;
  const rows = [
    summary.projectId && { label: '项目', value: summary.projectId },
    summary.clientEmail && { label: 'SA 邮箱', value: summary.clientEmail },
    summary.clientId && { label: 'Client ID', value: summary.clientId },
    summary.keyId && { label: '密钥 ID', value: summary.keyId },
  ].filter(Boolean);
  if (rows.length === 0) return null;
  return (
    <div className="mt-2 rounded-md border border-kumo-interact/85 bg-kumo-recessed/40 p-3">
      <div className="mb-2 flex items-center justify-between">
        <span className="text-xs font-medium text-kumo-subtle">解析的凭证信息</span>
        {onImport && <Button type="button" size="sm" variant="ghost" className="h-5 px-1 text-[11px]" onClick={onImport}>读取到文件</Button>}
      </div>
      <div className="grid grid-cols-1 gap-1.5 text-xs cq-sm:grid-cols-2">
        {rows.map((row) => (
          <div key={row.label} className="min-w-0">
            <div className="text-kumo-subtle">{row.label}</div>
            <div className="truncate font-mono text-kumo-strong" title={row.value}>{row.value}</div>
          </div>
        ))}
      </div>
    </div>
  );
}
