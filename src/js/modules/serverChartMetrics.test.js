import { describe, expect, it } from 'vitest';
import { normalizeChartMetricRecords } from './serverChartMetrics.js';

describe('server chart metrics helpers', () => {
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
