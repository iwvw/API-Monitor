export function modelHealthKey(endpointId, modelId) {
  return JSON.stringify([String(endpointId || ''), String(modelId || '').trim()]);
}

export const DEFAULT_MODEL_HEALTH_CONCURRENCY = 30;
const MAX_MODEL_HEALTH_CONCURRENCY = 30;
export const DEFAULT_MODEL_HEALTH_TIMEOUT_SECONDS = 6;

export function endpointModelIds(endpoint) {
  const models = Array.isArray(endpoint?.models) ? endpoint.models : [];
  return Array.from(
    new Set(
      models
        .map(model => (typeof model === 'string' ? model : model?.id))
        .map(model => String(model || '').trim())
        .filter(Boolean)
    )
  );
}

export function modelHealthTargets(endpoints) {
  return (Array.isArray(endpoints) ? endpoints : []).flatMap(endpoint => {
    if (!endpoint?.enabled) return [];
    return endpointModelIds(endpoint).map(modelId => ({ endpointId: endpoint.id, modelId }));
  });
}

export function normalizeModelHealthRecord(record, fallbackError = '检测未返回结果') {
  const rawStatus = String(record?.status || '').toLowerCase();
  const status =
    rawStatus === 'operational' || rawStatus === 'healthy'
      ? 'healthy'
      : rawStatus === 'degraded'
        ? 'degraded'
        : 'error';
  const parsedLatency = Number(record?.latency);
  const latency = Number.isFinite(parsedLatency) ? parsedLatency : null;

  return {
    status,
    loading: false,
    latency,
    checkedAt: record?.checkedAt || Date.now(),
    ...(status === 'error' ? { error: record?.error || fallbackError } : {}),
  };
}

export function countModelHealthResults(results) {
  return (Array.isArray(results) ? results : []).reduce(
    (counts, result) => {
      if (result.status === 'healthy') counts.healthy += 1;
      else if (result.status === 'degraded') counts.degraded += 1;
      else counts.failed += 1;
      return counts;
    },
    { healthy: 0, degraded: 0, failed: 0 }
  );
}

export function resolveModelHealthConcurrency(
  requestedConcurrency,
  totalTargets,
  fallback = DEFAULT_MODEL_HEALTH_CONCURRENCY,
  maxConcurrency = MAX_MODEL_HEALTH_CONCURRENCY
) {
  const total = Math.max(0, Math.floor(Number(totalTargets) || 0));
  if (total === 0) return 0;

  const safeMax = Math.max(1, Math.floor(Number(maxConcurrency) || MAX_MODEL_HEALTH_CONCURRENCY));
  const safeFallback = Math.max(1, Math.min(Math.floor(Number(fallback) || 0) || 1, safeMax, total));
  const parsed = Math.floor(Number(requestedConcurrency) || 0);
  if (!Number.isFinite(parsed) || parsed <= 0) return safeFallback;
  return Math.max(1, Math.min(parsed, safeMax, total));
}
