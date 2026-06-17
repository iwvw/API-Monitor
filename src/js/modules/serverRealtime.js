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
  const next = {
    ...incoming,
    info: silent && existing?.info
      ? existing.info
      : (incoming.info || existing?.info || null),
    metricsCache: cachedMetrics || null,
    metricsLoading: existing?.metricsLoading || false,
    gpuChartVisible: existing?.gpuChartVisible || false,
    gpuLoading: existing?.gpuLoading || false,
    netChartVisible: existing?.netChartVisible || false,
    netLoading: existing?.netLoading || false,
    error: existing?.error || null,
    loading: existing?.loading || false,
  };

  if (silent && existing) {
    next.last_check_time = existing.last_check_time ?? incoming.last_check_time;
    next.last_check_status = existing.last_check_status ?? incoming.last_check_status;
    next.updated_at = existing.updated_at ?? incoming.updated_at;
  }

  return next;
}
