import { describe, expect, it } from 'vitest';
import { parseDashboardTrendTimestamp } from './dashboardMetrics.js';

describe('dashboard metrics helpers', () => {
  it('parses API trend date buckets as UTC days', () => {
    expect(parseDashboardTrendTimestamp({ bucket: '2026-06-21' })).toBe(Date.parse('2026-06-21T00:00:00Z'));
  });

  it('parses SQLite-like trend buckets as UTC timestamps', () => {
    expect(parseDashboardTrendTimestamp({ bucket: '2026-06-21 10:30:45' })).toBe(Date.parse('2026-06-21T10:30:45Z'));
    expect(parseDashboardTrendTimestamp({ bucket: '2026-06-21T10:30' })).toBe(Date.parse('2026-06-21T10:30Z'));
  });

  it('accepts both seconds and millisecond timestamps', () => {
    expect(parseDashboardTrendTimestamp({ timestamp: 1_782_028_800 })).toBe(1_782_028_800_000);
    expect(parseDashboardTrendTimestamp({ timestamp: 1_782_028_800_000 })).toBe(1_782_028_800_000);
  });
});
