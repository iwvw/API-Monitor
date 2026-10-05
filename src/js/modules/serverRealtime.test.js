import { describe, expect, it } from 'vitest';
import {
  resolveMetricsAgeMs,
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

  it('marks stale metrics from a still-online connection as interrupted', () => {
    const now = Date.parse('2026-06-29T01:30:00Z');
    const health = resolveServerMetricsHealth({
      status: 'online',
      agent_online: true,
      metrics_last_seen_at: now - SERVER_METRICS_STALE_AFTER_MS - 1,
    }, now);

    expect(health.state).toBe('interrupted');
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
    expect(health.label).toBe('采集异常');
  });

  it('stays fresh despite large browser/server clock skew', () => {
    const browserNow = 1_800_000_000_000;
    const serverClockSkewMs = 44_000;
    const health = resolveServerMetricsHealth({
      status: 'online',
      agent_online: true,
      // 服务端时钟比浏览器慢 44s：绝对时间戳相减会误判为 ~45.5s 陈旧
      metrics_last_seen_at: browserNow - serverClockSkewMs - 1_500,
      metrics_age_ms: 1_500,
      metrics_received_at: browserNow - 200,
      metrics_received_age_ms: 1_500,
    }, browserNow);

    expect(health.state).toBe('fresh');
    expect(health.variant).toBe('success');
  });

  it('still reports interrupted once the received sample genuinely goes stale', () => {
    const browserNow = 1_800_000_000_000;
    const health = resolveServerMetricsHealth({
      status: 'online',
      agent_online: true,
      metrics_last_seen_at: browserNow - 44_000 - 1_500,
      metrics_age_ms: 1_500,
      metrics_received_at: browserNow - SERVER_METRICS_STALE_AFTER_MS - 1_000,
      metrics_received_age_ms: 1_500,
    }, browserNow);

    expect(health.state).toBe('interrupted');
  });

  it('advances age from the receive anchor using only the browser clock', () => {
    const receivedAt = 1_800_000_000_000;
    expect(resolveMetricsAgeMs({
      metrics_received_at: receivedAt,
      metrics_received_age_ms: 1_500,
    }, receivedAt + 10_000)).toBe(11_500);
  });
});
