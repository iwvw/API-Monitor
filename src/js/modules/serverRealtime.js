export function areRealtimeValuesEqual(a, b) {
  if (Object.is(a, b)) return true;
  if (a === null || b === null || a === undefined || b === undefined) return false;
  if (typeof a !== 'object' || typeof b !== 'object') return false;

  if (Array.isArray(a) || Array.isArray(b)) {
    if (!Array.isArray(a) || !Array.isArray(b) || a.length !== b.length) return false;
    return a.every((item, index) => areRealtimeValuesEqual(item, b[index]));
  }

  const aKeys = Object.keys(a);
  const bKeys = Object.keys(b);
  if (aKeys.length !== bKeys.length) return false;

  return aKeys.every(key => (
    Object.prototype.hasOwnProperty.call(b, key) && areRealtimeValuesEqual(a[key], b[key])
  ));
}

export function reuseRealtimeValueIfEqual(previousValue, nextValue) {
  return areRealtimeValuesEqual(previousValue, nextValue) ? previousValue : nextValue;
}

export const SERVER_METRICS_STALE_AFTER_MS = 45 * 1000;

export function resolveServerMetricsHealth(server = {}, now = Date.now()) {
  if (server.status !== 'online' && server.agent_online !== true) {
    return {
      state: 'offline',
      stale: false,
      label: '离线',
      variant: 'error',
      dotClassName: 'bg-kumo-danger',
      ageMs: 0,
    };
  }

  const explicitState = server.metrics_health || server.info?.metrics_health;
  const lastSeenAt = Number(
    server.metrics_last_seen_at ||
    server.info?.metrics_last_seen_at ||
    server.lastMetricUpdateTime ||
    0
  );
  const staleAfterMs = Number(server.info?.metrics_stale_after_ms || SERVER_METRICS_STALE_AFTER_MS);
  const ageMs = lastSeenAt > 0 ? Math.max(0, now - lastSeenAt) : Number(server.metrics_age_ms || 0);

  if (explicitState === 'degraded') {
    return {
      state: 'degraded',
      stale: true,
      label: '采集异常',
      variant: 'warning',
      dotClassName: 'bg-kumo-warning',
      ageMs,
    };
  }

  if ((lastSeenAt > 0 && ageMs <= staleAfterMs) || (lastSeenAt === 0 && explicitState === 'fresh')) {
    return {
      state: 'fresh',
      stale: false,
      label: '在线',
      variant: 'success',
      dotClassName: 'bg-kumo-success',
      ageMs,
    };
  }

  if (explicitState === 'stale' || server.metrics_stale === true || (lastSeenAt > 0 && ageMs > staleAfterMs)) {
    return {
      state: 'stale',
      stale: true,
      label: '中断',
      variant: 'warning',
      dotClassName: 'bg-kumo-warning',
      ageMs,
    };
  }

  return {
    state: 'missing',
    stale: true,
    label: '无指标',
    variant: 'warning',
    dotClassName: 'bg-kumo-warning',
    ageMs,
  };
}

export function mergeRealtimeDiskInfo(previousDisk, metrics = {}) {
  const previousList = Array.isArray(previousDisk) ? previousDisk : [];
  if (metrics.disk_usage === undefined || metrics.disk_usage === null) {
    return previousList;
  }

  const previousEntry = previousList[0] && typeof previousList[0] === 'object'
    ? previousList[0]
    : { device: '/', used: '-', total: '-', usage: '0%' };

  const diskUsageText = String(metrics.disk_usage);
  const diskMatch = diskUsageText.match(/(.+?)\/(.+?)\s*\((\d+%?)\)/);
  let nextEntry = previousEntry;

  if (diskMatch) {
    nextEntry = {
      device: previousEntry.device || '/',
      used: diskMatch[1].trim(),
      total: diskMatch[2].trim(),
      usage: diskMatch[3],
    };
  } else {
    const diskPercent = Number.parseFloat(diskUsageText);
    if (Number.isFinite(diskPercent)) {
      nextEntry = {
        ...previousEntry,
        device: previousEntry.device || '/',
        used: metrics.disk_used || previousEntry.used || '-',
        total: metrics.disk_total || previousEntry.total || '-',
        usage: `${Math.round(diskPercent)}%`,
      };
    }
  }

  if (areRealtimeValuesEqual(previousEntry, nextEntry)) {
    return previousList.length > 0 ? previousList : [nextEntry];
  }

  const remaining = previousList.slice(1);
  return remaining.length > 0 ? [nextEntry, ...remaining] : [nextEntry];
}

export function resolveRealtimeMetricsCache(currentCache, nextCache, { isExpanded = false } = {}) {
  if (!Array.isArray(nextCache) || nextCache.length === 0) {
    return Array.isArray(currentCache) ? currentCache : nextCache;
  }

  if (isExpanded || !Array.isArray(currentCache) || currentCache.length === 0) {
    return nextCache;
  }

  return currentCache;
}

export function mergePolledServerAccount(
  existing,
  incoming,
  {
    silent = false,
    cachedMetrics = null,
  } = {},
) {
  const websocketActive = existing?.lastMetricUpdateTime && (Date.now() - existing.lastMetricUpdateTime) < 30000;

  const next = {
    ...incoming,
    info: (silent && websocketActive && existing?.info)
      ? existing.info
      : (incoming.info || existing?.info || null),
    metricsCache: cachedMetrics || null,
    metricsLoading: existing?.metricsLoading || false,
    metrics_health: incoming.metrics_health || existing?.metrics_health || null,
    metrics_stale: incoming.metrics_stale ?? existing?.metrics_stale ?? false,
    metrics_last_seen: incoming.metrics_last_seen || existing?.metrics_last_seen || null,
    metrics_last_seen_at: incoming.metrics_last_seen_at || existing?.metrics_last_seen_at || 0,
    metrics_age_ms: incoming.metrics_age_ms ?? existing?.metrics_age_ms ?? 0,
    gpuChartVisible: existing?.gpuChartVisible || false,
    gpuLoading: existing?.gpuLoading || false,
    netChartVisible: existing?.netChartVisible || false,
    netLoading: existing?.netLoading || false,
    error: existing?.error || null,
    loading: existing?.loading || false,
    lastMetricUpdateTime: existing?.lastMetricUpdateTime || 0,
  };

  if (silent && existing) {
    next.last_check_time = existing.last_check_time ?? incoming.last_check_time;
    next.last_check_status = existing.last_check_status ?? incoming.last_check_status;
    next.updated_at = existing.updated_at ?? incoming.updated_at;
  }

  return next;
}
