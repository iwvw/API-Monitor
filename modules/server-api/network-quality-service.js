const { serverStorage, networkQualityStorage } = require('./storage');
const { TaskTypes } = require('./protocol');
const { createLogger } = require('../../src/utils/logger');

const logger = createLogger('NetworkQuality');

const DEFAULT_INTERVAL_MS = 60 * 1000;
const DEFAULT_TIMEOUT_MS = 2500;
const DEFAULT_RETENTION_DAYS = 3;
const DEFAULT_MAX_POINTS_PER_TARGET = 240;

const toNumber = (value, fallback = 0) => {
  const parsed = Number(value);
  return Number.isFinite(parsed) ? parsed : fallback;
};

const percentile = (values, ratio) => {
  if (!values.length) return null;
  const sorted = [...values].sort((a, b) => a - b);
  const index = Math.min(sorted.length - 1, Math.max(0, Math.ceil(sorted.length * ratio) - 1));
  return sorted[index];
};

const average = (values) => {
  if (!values.length) return null;
  return values.reduce((sum, value) => sum + value, 0) / values.length;
};

const averageDelta = (values) => {
  if (values.length < 2) return 0;
  const deltas = [];
  for (let index = 1; index < values.length; index += 1) {
    deltas.push(Math.abs(values[index] - values[index - 1]));
  }
  return average(deltas) || 0;
};

const isUnsupportedTaskType = (message = '') => {
  const text = String(message || '');
  return text.includes('不支持的任务类型')
    || /unsupported\s+task\s+type/i.test(text)
    || /task\s+type:\s*40/i.test(text);
};

const normalizeProbeResult = (serverId, targets, payload) => {
  let parsed = payload;
  if (typeof payload === 'string') {
    parsed = JSON.parse(payload);
  }

  const results = Array.isArray(parsed?.results) ? parsed.results : [];
  const targetById = new Map(targets.map(target => [String(target.id), target]));
  const checkedAt = parsed?.checked_at || new Date().toISOString();

  return results.map(result => {
    const target = targetById.get(String(result.id)) || {};
    const latency = Number(result.latency_ms);
    return {
      server_id: serverId,
      target_id: target.id || result.id || null,
      target_name: target.name || result.name || '',
      target_host: target.host || result.host || '',
      target_port: target.port || result.port || 80,
      success: !!result.success,
      latency_ms: Number.isFinite(latency) ? latency : null,
      error_message: result.error || result.error_message || null,
      checked_at: result.checked_at || checkedAt,
    };
  });
};

const toPoint = (record) => ({
  checked_at: record.checked_at,
  latency_ms: record.success ? toNumber(record.latency_ms, null) : null,
  success: !!record.success,
  loss_rate: record.success ? 0 : 100,
});

const downsampleRecords = (records, maxPoints = DEFAULT_MAX_POINTS_PER_TARGET) => {
  if (records.length <= maxPoints) return records.map(toPoint);

  const bucketSize = Math.ceil(records.length / maxPoints);
  const points = [];

  for (let index = 0; index < records.length; index += bucketSize) {
    const bucket = records.slice(index, index + bucketSize);
    const successfulLatencies = bucket
      .filter(item => item.success && Number.isFinite(Number(item.latency_ms)))
      .map(item => Number(item.latency_ms));
    const failedCount = bucket.length - successfulLatencies.length;
    const last = bucket[bucket.length - 1];

    points.push({
      checked_at: last.checked_at,
      latency_ms: successfulLatencies.length ? Math.max(...successfulLatencies) : null,
      success: failedCount === 0,
      loss_rate: bucket.length ? (failedCount / bucket.length) * 100 : 0,
    });
  }

  return points;
};

const buildTargetSummary = (targetName, records) => {
  const total = records.length;
  const successful = records.filter(item => item.success && Number.isFinite(Number(item.latency_ms)));
  const latencies = successful.map(item => Number(item.latency_ms));
  const failedCount = total - successful.length;
  const latest = records[records.length - 1] || null;

  return {
    name: targetName,
    total,
    successCount: successful.length,
    failedCount,
    lossRate: total ? (failedCount / total) * 100 : 0,
    avgLatency: average(latencies),
    minLatency: latencies.length ? Math.min(...latencies) : null,
    maxLatency: latencies.length ? Math.max(...latencies) : null,
    p95Latency: percentile(latencies, 0.95),
    jitterMs: averageDelta(latencies),
    latest: latest ? {
      success: !!latest.success,
      latencyMs: latest.success ? toNumber(latest.latency_ms, null) : null,
      error: latest.error_message || null,
      checkedAt: latest.checked_at,
    } : null,
  };
};

class NetworkQualityService {
  constructor() {
    this.timer = null;
    this.running = false;
    this.inFlight = false;
    this.lastCleanupAt = 0;
  }

  start() {
    if (this.running) return;
    this.running = true;
    this.timer = setInterval(() => {
      this.collectAll().catch(error => logger.warn(`网络质量采集失败: ${error.message}`));
    }, DEFAULT_INTERVAL_MS);
    if (typeof this.timer.unref === 'function') this.timer.unref();

    setTimeout(() => {
      this.collectAll().catch(error => logger.warn(`网络质量首次采集失败: ${error.message}`));
    }, 8000);

    logger.info('网络质量监控已启动 (间隔: 60秒, 窗口: 24h)');
  }

  stop() {
    if (this.timer) clearInterval(this.timer);
    this.timer = null;
    this.running = false;
  }

  async collectAll() {
    if (this.inFlight) return { skipped: true };
    this.inFlight = true;

    try {
      const servers = serverStorage.getAll().filter(server => server.status === 'online');
      const results = await Promise.allSettled(servers.map(server => this.collectServer(server.id)));
      const successCount = results.filter(item => item.status === 'fulfilled' && item.value?.saved > 0).length;

      if (Date.now() - this.lastCleanupAt > 60 * 60 * 1000) {
        this.lastCleanupAt = Date.now();
        networkQualityStorage.deleteOldRecords(DEFAULT_RETENTION_DAYS);
      }

      return { total: servers.length, successCount };
    } finally {
      this.inFlight = false;
    }
  }

  async collectServer(serverId) {
    const agentService = require('./agent-service');
    const targets = networkQualityStorage.getTargets();
    if (!targets.length) return { saved: 0 };
    if (!agentService.isOnline(serverId)) return { saved: 0, skipped: true };

    const payload = {
      timeout_ms: DEFAULT_TIMEOUT_MS,
      targets: targets.map(target => ({
        id: target.id,
        name: target.name,
        host: target.host,
        port: target.port || 80,
        type: target.type || 'tcp',
      })),
    };

    const result = await agentService.sendInternalTaskAndWait(
      serverId,
      {
        type: TaskTypes.NETWORK_QUALITY_PROBE,
        data: JSON.stringify(payload),
        timeout: Math.ceil((DEFAULT_TIMEOUT_MS + 3000) / 1000),
      },
      DEFAULT_TIMEOUT_MS + 7000
    );

    if (!result?.successful) {
      const message = result?.data || '网络质量探测失败';
      if (isUnsupportedTaskType(message)) {
        return {
          saved: 0,
          unsupported: true,
          message,
        };
      }
      throw new Error(message);
    }

    const records = normalizeProbeResult(serverId, targets, result.data);
    const saved = networkQualityStorage.createSamples(records);
    return { saved, records };
  }

  getServerQuality(serverId, options = {}) {
    const hours = Math.min(Math.max(Number(options.hours) || 24, 1), 168);
    const maxPointsPerTarget = Math.min(
      Math.max(Number(options.maxPointsPerTarget) || DEFAULT_MAX_POINTS_PER_TARGET, 60),
      480
    );
    const history = networkQualityStorage
      .getHistory(serverId, { hours, limit: 30000 })
      .sort((a, b) => new Date(a.checked_at) - new Date(b.checked_at));

    const grouped = new Map();
    for (const record of history) {
      const key = record.target_name || `${record.target_host}:${record.target_port}`;
      if (!grouped.has(key)) grouped.set(key, []);
      grouped.get(key).push(record);
    }

    const summary = [];
    const series = [];
    for (const [targetName, records] of grouped.entries()) {
      summary.push(buildTargetSummary(targetName, records));
      series.push({
        name: targetName,
        points: downsampleRecords(records, maxPointsPerTarget),
        rawCount: records.length,
      });
    }

    return {
      serverId,
      hours,
      maxPointsPerTarget,
      sampleCount: history.length,
      updatedAt: history.length ? history[history.length - 1].checked_at : null,
      summary,
      series,
    };
  }
}

module.exports = new NetworkQualityService();
