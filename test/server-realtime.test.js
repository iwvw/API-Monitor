import { describe, expect, it } from 'vitest';
import {
  areRealtimeValuesEqual,
  mergePolledServerAccount,
  mergeRealtimeDiskInfo,
  resolveRealtimeMetricsCache,
  reuseRealtimeValueIfEqual,
} from '../src/js/modules/serverRealtime.js';
import { normalizeChartMetricRecords } from '../src/js/modules/serverChartMetrics.js';

describe('server realtime helpers', () => {
  it('reuses the previous disk array when disk metrics did not change', () => {
    const previous = [{ device: '/', used: '40 GB', total: '100 GB', usage: '40%' }];
    const next = mergeRealtimeDiskInfo(previous, {
      disk_usage: '40 GB/100 GB (40%)',
    });

    expect(next).toBe(previous);
  });

  it('updates the disk snapshot when disk metrics change', () => {
    const previous = [{ device: '/', used: '40 GB', total: '100 GB', usage: '40%' }];
    const next = mergeRealtimeDiskInfo(previous, {
      disk_usage: '41 GB/100 GB (41%)',
    });

    expect(next).not.toBe(previous);
    expect(next[0]).toEqual({ device: '/', used: '41 GB', total: '100 GB', usage: '41%' });
  });

  it('builds disk usage from disk_used and disk_total when realtime payload has no parsed disk snapshot', () => {
    const previous = [{ device: '/', used: '-', total: '-', usage: '0%' }];
    const next = mergeRealtimeDiskInfo(previous, {
      disk_used: 10 * 1024 * 1024 * 1024,
      disk_total: 20 * 1024 * 1024 * 1024,
      disk_usage: '50%',
    });

    expect(next[0]).toMatchObject({
      device: '/',
      usage: '50%',
    });
  });

  it('keeps collapsed metric cache stable to avoid unnecessary rerenders', () => {
    const currentCache = [{ _ts: 1, cpu_usage: 10 }];
    const nextCache = [{ _ts: 1, cpu_usage: 10 }, { _ts: 2, cpu_usage: 12 }];

    expect(resolveRealtimeMetricsCache(currentCache, nextCache, { isExpanded: false })).toBe(currentCache);
    expect(resolveRealtimeMetricsCache(currentCache, nextCache, { isExpanded: true })).toBe(nextCache);
  });

  it('can reuse nested values when they are deeply equal', () => {
    const previous = { usage: '40%', detail: { used: '40 GB' } };
    const next = { usage: '40%', detail: { used: '40 GB' } };

    expect(areRealtimeValuesEqual(previous, next)).toBe(true);
    expect(reuseRealtimeValueIfEqual(previous, next)).toBe(previous);
  });

  it('keeps volatile detail fields stable during silent server list polling', () => {
    const existing = {
      id: 'server-1',
      status: 'online',
      info: { disk: [{ usage: '40%' }] },
      last_check_time: '2026-06-17 23:00:00',
      last_check_status: 'success',
      updated_at: '2026-06-17T23:00:00Z',
      metricsCache: [{ _ts: 1, cpu_usage: 10 }],
    };
    const incoming = {
      id: 'server-1',
      status: 'online',
      info: { disk: [{ usage: '41%' }] },
      last_check_time: '2026-06-17 23:00:15',
      last_check_status: 'success',
      updated_at: '2026-06-17T23:00:15Z',
    };

    const merged = mergePolledServerAccount(existing, incoming, {
      silent: true,
      cachedMetrics: existing.metricsCache,
    });

    expect(merged.info).toBe(existing.info);
    expect(merged.last_check_time).toBe(existing.last_check_time);
    expect(merged.updated_at).toBe(existing.updated_at);
    expect(merged.metricsCache).toBe(existing.metricsCache);
  });

  it('preserves cpu and gpu model fields during realtime merges', () => {
    const existing = {
      info: {
        cpu: { Model: 'Intel Core i9', Usage: '12%' },
        gpu: { Model: 'NVIDIA RTX 4060', Usage: '8%' },
      },
    };

    const metrics = {
      cpu_usage: '18%',
      gpu: 22,
      gpu_usage: '22%',
    };

    const previousInfo = existing.info;
    const logicalCores = 0;
    const physicalCores = 0;
    const metricCpu = metrics.cpu && typeof metrics.cpu === 'object' ? metrics.cpu : {};
    const existingCpu = previousInfo.cpu && typeof previousInfo.cpu === 'object' ? previousInfo.cpu : {};
    const nextCpu = {
      Model: metrics.cpu_model || metricCpu.Model || existingCpu.Model || '',
      Load: metrics.load || '-',
      Usage: metrics.cpu_usage || '0%',
      Cores: logicalCores || previousInfo.cpu?.Cores || '-',
      LogicalCores: logicalCores || previousInfo.cpu?.LogicalCores || previousInfo.cpu?.Cores || '-',
      PhysicalCores: physicalCores || previousInfo.cpu?.PhysicalCores || previousInfo.cpu?.Cores || '-',
      Temp: previousInfo.cpu?.Temp || 0,
      Power: metrics.cpu_power || metrics.cpu_power_w || previousInfo.cpu?.Power || '',
    };

    const pushedGpu = typeof metrics.gpu === 'object' && metrics.gpu !== null ? metrics.gpu : {};
    const existingGpu = previousInfo.gpu || {};
    const nextGpu = {
      Model: pushedGpu.Model || metrics.gpu_model || existingGpu.Model || '',
      Usage: pushedGpu.Usage || metrics.gpu_usage || existingGpu.Usage || '0%',
      Memory: pushedGpu.Memory || metrics.gpu_mem || existingGpu.Memory || '',
      Power: pushedGpu.Power || metrics.gpu_power || existingGpu.Power || '',
      Temp: pushedGpu.Temp !== undefined ? pushedGpu.Temp : (metrics.gpu_temp !== undefined ? metrics.gpu_temp : (existingGpu.Temp || 0)),
      Percent: existingGpu.Percent || 0,
    };

    expect(nextCpu.Model).toBe('Intel Core i9');
    expect(nextGpu.Model).toBe('NVIDIA RTX 4060');
  });

  it('uses incoming detail fields for non-silent server list loads', () => {
    const existing = {
      id: 'server-1',
      info: { disk: [{ usage: '40%' }] },
    };
    const incoming = {
      id: 'server-1',
      info: { disk: [{ usage: '41%' }] },
    };

    const merged = mergePolledServerAccount(existing, incoming, {
      silent: false,
      cachedMetrics: null,
    });

    expect(merged.info).toBe(incoming.info);
  });

  it('keeps refreshed realtime samples connected to retained 5 minute history', () => {
    const base = Date.parse('2026-06-18T10:00:00Z');
    const records = [
      { _ts: base - 5 * 60 * 1000, cpu_usage: 12 },
      { _ts: base - 4 * 60 * 1000, cpu_usage: 14 },
      { _ts: base - 3 * 60 * 1000, cpu_usage: 15 },
      { _ts: base - 20 * 1000, cpu_usage: 18 },
      { _ts: base, cpu_usage: 20 },
    ];

    const normalized = normalizeChartMetricRecords(records);

    expect(normalized.some(record => record._gap)).toBe(false);
  });
});
