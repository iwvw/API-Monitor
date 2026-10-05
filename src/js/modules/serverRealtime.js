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

// 指标新鲜度必须只用「单一时钟」推导。服务端下发的 metrics_last_seen_at 是
// 服务端绝对时间戳，浏览器 Date.now() 是本地时钟，二者存在未知偏移（实测可达
// 40s+）。若直接相减，会把 ~1.5s 前刚上报的指标算成超过 45s 的陈旧值，主机状态
// 在橙色「中断」与绿色「在线」之间抖动。因此这里以「接收时刻 + 服务端上报的
// 相对年龄」为锚点，仅使用浏览器本地时钟推进，完全不跨时钟比较绝对时间戳。
export function resolveMetricsAgeMs(server = {}, now = Date.now(), lastSeenAt = 0) {
  const receivedAt = Number(server.metrics_received_at || 0);
  const receivedAgeMs = Number(server.metrics_received_age_ms || 0);
  if (receivedAt > 0) {
    return Math.max(0, receivedAgeMs + Math.max(0, now - receivedAt));
  }

  const reportedAgeMs = Number(server.metrics_age_ms || server.info?.metrics_age_ms || 0);
  if (reportedAgeMs > 0) {
    return reportedAgeMs;
  }

  // 兜底：仅在缺少服务端相对年龄时，才退回绝对时间戳相减（要求两侧时钟一致）。
  return lastSeenAt > 0 ? Math.max(0, now - lastSeenAt) : 0;
}

export function resolveServerMetricsHealth(server = {}, now = Date.now()) {
  const connectionStatus = String(server.status || '').toLowerCase();
  const interruptedConnection = connectionStatus === 'interrupted' || connectionStatus === 'suspect';

  if (connectionStatus === 'offline' && server.agent_online !== true) {
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
  const ageMs = resolveMetricsAgeMs(server, now, lastSeenAt);

  if (interruptedConnection) {
    return {
      state: 'interrupted',
      stale: true,
      label: '中断',
      variant: 'warning',
      dotClassName: 'bg-kumo-warning',
      ageMs,
    };
  }

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

  if (connectionStatus !== 'online' && server.agent_online !== true && server.agent_connected !== true) {
    return {
      state: 'offline',
      stale: false,
      label: '离线',
      variant: 'error',
      dotClassName: 'bg-kumo-danger',
      ageMs: 0,
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
      state: 'interrupted',
      stale: true,
      label: '中断',
      variant: 'warning',
      dotClassName: 'bg-kumo-warning',
      ageMs,
    };
  }

  return {
    state: 'interrupted',
    stale: true,
    label: '中断',
    variant: 'warning',
    dotClassName: 'bg-kumo-warning',
    ageMs,
  };
}

export function resolveServerDisplayStatus(server = {}, now = Date.now()) {
  const health = resolveServerMetricsHealth(server, now);
  if (health.state === 'offline') return { ...health, state: 'offline', label: '离线' };
  if (health.state === 'degraded') return { ...health, state: 'degraded', label: '采集异常' };
  if (health.state === 'interrupted') return { ...health, state: 'interrupted', label: '中断' };
  return { ...health, state: 'online', label: '在线' };
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
  const incomingSampleAt = Number(incoming.metrics_last_seen_at || incoming.info?.metrics_last_seen_at || 0);
  const existingSampleAt = Number(existing?.metrics_last_seen_at || existing?.info?.metrics_last_seen_at || 0);
  const sampleUnchanged = incomingSampleAt > 0 && incomingSampleAt === existingSampleAt;
  // 同一份样本复用既有锚点，避免每次轮询都重置锚点导致年龄被压低、并无谓触发重渲染。
  const receivedAt = sampleUnchanged
    ? (existing?.metrics_received_at || Date.now())
    : Date.now();
  const receivedAgeMs = sampleUnchanged
    ? (existing?.metrics_received_age_ms || 0)
    : (Number.isFinite(Number(incoming.metrics_age_ms)) ? Number(incoming.metrics_age_ms) : 0);

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
    metrics_received_at: incomingSampleAt > 0 ? receivedAt : (existing?.metrics_received_at || 0),
    metrics_received_age_ms: incomingSampleAt > 0 ? receivedAgeMs : (existing?.metrics_received_age_ms || 0),
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
