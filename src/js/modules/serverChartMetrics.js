export const SERVER_REALTIME_SAMPLE_INTERVAL_MS = 1500;
export const SERVER_CHART_HISTORY_WINDOW_MS = 5 * 60 * 1000;
export const SERVER_CHART_HISTORY_LIMIT = Math.ceil(SERVER_CHART_HISTORY_WINDOW_MS / SERVER_REALTIME_SAMPLE_INTERVAL_MS) + 1;
export const SERVER_CHART_JITTER_TOLERANCE_MS = 650;
export const SERVER_CHART_OFFLINE_GAP_MS = SERVER_CHART_HISTORY_WINDOW_MS;

export const toTimestamp = (value, fallback) => {
  if (typeof value === 'number') {
    return value > 100000000000 ? value : value * 1000;
  }
  if (typeof value === 'string') {
    const sqliteUTCMatch = value.match(/^(\d{4}-\d{2}-\d{2}) (\d{2}:\d{2}:\d{2})(?:\.\d+)?$/);
    if (sqliteUTCMatch) {
      const parsedUTC = Date.parse(`${sqliteUTCMatch[1]}T${sqliteUTCMatch[2]}Z`);
      return Number.isFinite(parsedUTC) ? parsedUTC : fallback;
    }
  }
  const parsed = value ? new Date(value).getTime() : NaN;
  return Number.isFinite(parsed) ? parsed : fallback;
};

export const normalizeMetricRecords = (records = []) => {
  const now = Date.now();
  const list = Array.isArray(records) ? records : [];
  return list
    .map((record, index) => {
      const fallbackTime = now - (list.length - index - 1) * SERVER_REALTIME_SAMPLE_INTERVAL_MS;
      const timestamp = toTimestamp(record._ts || record.recorded_at || record.timestamp || record.time, fallbackTime);
      if (!Number.isFinite(timestamp)) return null;
      return {
        ...record,
        _ts: timestamp,
      };
    })
    .filter(Boolean)
    .sort((a, b) => a._ts - b._ts);
};

export const normalizeChartMetricRecords = (records = []) => {
  const normalized = normalizeMetricRecords(records);
  if (normalized.length <= 2) return normalized;

  const output = [];
  let segment = [];

  const flushSegment = () => {
    if (segment.length === 0) return;

    if (segment.length < 3) {
      output.push(...segment);
      segment = [];
      return;
    }

    const first = segment[0];
    const last = segment[segment.length - 1];
    const averageInterval = (last._ts - first._ts) / (segment.length - 1);
    const shouldSnap = Math.abs(averageInterval - SERVER_REALTIME_SAMPLE_INTERVAL_MS) <= SERVER_CHART_JITTER_TOLERANCE_MS;

    if (!shouldSnap) {
      output.push(...segment);
      segment = [];
      return;
    }

    const anchor = last._ts;
    segment.forEach((record, index) => {
      output.push({
        ...record,
        _rawTs: record._ts,
        _ts: anchor - (segment.length - index - 1) * SERVER_REALTIME_SAMPLE_INTERVAL_MS,
      });
    });
    segment = [];
  };

  normalized.forEach(record => {
    const last = segment[segment.length - 1];
    if (last && record._ts - last._ts >= SERVER_CHART_OFFLINE_GAP_MS) {
      flushSegment();
      const previous = output[output.length - 1];
      if (previous && !previous._gap) {
        const gapTs = previous._ts + SERVER_REALTIME_SAMPLE_INTERVAL_MS;
        if (gapTs < record._ts) {
          output.push({ _ts: gapTs, _gap: true });
        }
      }
    }
    segment.push(record);
  });
  flushSegment();

  return output
    .filter((record, index, list) => index === 0 || record._ts > list[index - 1]._ts)
    .slice(-SERVER_CHART_HISTORY_LIMIT);
};

export const formatSqliteUTCDateTime = (date) => {
  const d = date instanceof Date ? date : new Date(date);
  if (Number.isNaN(d.getTime())) return '';
  return [
    d.getUTCFullYear(),
    String(d.getUTCMonth() + 1).padStart(2, '0'),
    String(d.getUTCDate()).padStart(2, '0'),
  ].join('-') + ' ' + [
    String(d.getUTCHours()).padStart(2, '0'),
    String(d.getUTCMinutes()).padStart(2, '0'),
    String(d.getUTCSeconds()).padStart(2, '0'),
  ].join(':');
};
