import { describe, expect, it, vi, beforeEach, afterEach } from 'vitest';

import {
  DASHBOARD_INVALIDATION_EVENT,
  readDashboardStatsInvalidatedAt,
  invalidateDashboardStats,
} from './dashboardInvalidation.js';

describe('readDashboardStatsInvalidatedAt', () => {
  afterEach(() => {
    vi.unstubAllGlobals();
  });

  it('returns 0 without a window', () => {
    expect(readDashboardStatsInvalidatedAt()).toBe(0);
  });

  it('returns the stored numeric timestamp', () => {
    vi.stubGlobal('window', { localStorage: { getItem: () => '1700000000123' } });
    expect(readDashboardStatsInvalidatedAt()).toBe(1700000000123);
  });

  it('returns 0 for invalid, empty or non-positive values', () => {
    vi.stubGlobal('window', { localStorage: { getItem: () => 'abc' } });
    expect(readDashboardStatsInvalidatedAt()).toBe(0);

    vi.stubGlobal('window', { localStorage: { getItem: () => '' } });
    expect(readDashboardStatsInvalidatedAt()).toBe(0);

    vi.stubGlobal('window', { localStorage: { getItem: () => '-5' } });
    expect(readDashboardStatsInvalidatedAt()).toBe(0);

    vi.stubGlobal('window', { localStorage: { getItem: () => '0' } });
    expect(readDashboardStatsInvalidatedAt()).toBe(0);

    vi.stubGlobal('window', { localStorage: { getItem: () => null } });
    expect(readDashboardStatsInvalidatedAt()).toBe(0);
  });

  it('returns 0 when localStorage throws', () => {
    vi.stubGlobal('window', {
      localStorage: {
        getItem: () => { throw new Error('storage blocked'); },
      },
    });
    expect(readDashboardStatsInvalidatedAt()).toBe(0);
  });
});

describe('invalidateDashboardStats', () => {
  let setItemMock;
  let dispatchEventMock;

  beforeEach(() => {
    vi.useFakeTimers();
    vi.setSystemTime(1700000000000);
    setItemMock = vi.fn();
    dispatchEventMock = vi.fn();
    vi.stubGlobal('window', { localStorage: { setItem: setItemMock }, dispatchEvent: dispatchEventMock });
  });

  afterEach(() => {
    vi.useRealTimers();
    vi.unstubAllGlobals();
  });

  it('stores the timestamp and dispatches a CustomEvent with manual reason', () => {
    const timestamp = invalidateDashboardStats();
    expect(timestamp).toBe(1700000000000);
    expect(setItemMock).toHaveBeenCalledWith('app_dashboard_stats_invalidated_at', '1700000000000');
    expect(dispatchEventMock).toHaveBeenCalledTimes(1);
    const event = dispatchEventMock.mock.calls[0][0];
    expect(event).toBeInstanceOf(CustomEvent);
    expect(event.type).toBe(DASHBOARD_INVALIDATION_EVENT);
    expect(event.detail).toEqual({ reason: 'manual', timestamp: 1700000000000 });
  });

  it('dispatches the given reason', () => {
    invalidateDashboardStats('cron');
    expect(dispatchEventMock.mock.calls[0][0].detail.reason).toBe('cron');
  });

  it('still dispatches when setItem throws', () => {
    setItemMock.mockImplementation(() => { throw new Error('quota'); });
    const timestamp = invalidateDashboardStats();
    expect(timestamp).toBe(1700000000000);
    expect(dispatchEventMock).toHaveBeenCalledTimes(1);
    expect(dispatchEventMock.mock.calls[0][0].detail.timestamp).toBe(1700000000000);
  });
});