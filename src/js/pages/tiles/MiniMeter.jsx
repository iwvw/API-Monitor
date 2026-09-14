// MiniMeter —— 迷你仪表（kumo Meter 封装的紧凑形态），用于内存/磁盘等占比展示。
import React from 'react';
import { Meter } from '@cloudflare/kumo';

function clampPercent(value) {
  return Math.min(100, Math.max(0, Number(value) || 0));
}

function fmtPercent(value) {
  const n = Number(value);
  if (!Number.isFinite(n)) return '-';
  return `${n.toFixed(n < 1 ? 2 : 1)}%`;
}

export default function MiniMeter({ label, value, detail, tone = 'brand' }) {
  const indicatorClassName = {
    brand: 'bg-brand',
    info: 'bg-kumo-info',
    success: 'bg-kumo-success',
    warning: 'bg-kumo-warning',
  }[tone] || 'bg-brand';
  return (
    <Meter
      label={label}
      value={clampPercent(value)}
      min={0}
      max={100}
      customValue={detail || fmtPercent(value)}
      className="min-w-0 text-[10px]"
      trackClassName="!h-2 overflow-hidden rounded-full border border-kumo-line/70 bg-kumo-recessed"
      indicatorClassName={`!h-full rounded-full ${indicatorClassName}`}
    />
  );
}