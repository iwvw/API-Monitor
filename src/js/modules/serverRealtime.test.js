import { describe, expect, it } from 'vitest';
import {
  resolveServerMetricsHealth,
  SERVER_METRICS_STALE_AFTER_MS,
} from './serverRealtime.js';

describe('server realtime health helpers', () => {
  it('marks an online server with fresh metrics as healthy', () => {
    const now = Date.parse('2026-06-29T01:30:00Z');
    const health = resolveServerMetricsHealth({
      status: 'online',
      agent_online: true,
      metrics_last_seen_at: now - 5_000,
    }, now);

    expect(health.state).toBe('fresh');
    expect(health.variant).toBe('success');
    expect(health.label).toBe('在线');
  });

  it('separates stale metrics from a still-online connection', () => {
    const now = Date.parse('2026-06-29T01:30:00Z');
    const health = resolveServerMetricsHealth({
      status: 'online',
      agent_online: true,
      metrics_last_seen_at: now - SERVER_METRICS_STALE_AFTER_MS - 1,
    }, now);

    expect(health.state).toBe('stale');
    expect(health.stale).toBe(true);
    expect(health.variant).toBe('warning');
    expect(health.label).toBe('中断');
  });

  it('keeps degraded collection visible even when the latest sample is recent', () => {
    const now = Date.parse('2026-06-29T01:30:00Z');
    const health = resolveServerMetricsHealth({
      status: 'online',
      agent_online: true,
      metrics_health: 'degraded',
      metrics_last_seen_at: now - 1_000,
    }, now);

    expect(health.state).toBe('degraded');
    expect(health.stale).toBe(true);
    expect(health.label).toBe('异常');
  });
});
