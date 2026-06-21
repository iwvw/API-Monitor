import { toTimestamp } from './serverChartMetrics.js';

export const parseDashboardTrendTimestamp = (point) => {
  const direct = point?.timestamp ?? point?.time ?? point?._ts;
  if (direct !== null && direct !== undefined) {
    const timestamp = toTimestamp(direct, NaN);
    if (Number.isFinite(timestamp)) return timestamp;
  }

  if (point?.bucket) {
    const bucket = String(point.bucket).trim();
    if (/^\d{4}-\d{2}-\d{2}$/.test(bucket)) {
      const parsed = Date.parse(`${bucket}T00:00:00Z`);
      return Number.isFinite(parsed) ? parsed : null;
    }
    const sqliteUTCMatch = bucket.match(/^(\d{4}-\d{2}-\d{2})[ T](\d{2}:\d{2}(?::\d{2})?(?:\.\d+)?)$/);
    if (sqliteUTCMatch) {
      const parsed = Date.parse(`${sqliteUTCMatch[1]}T${sqliteUTCMatch[2]}Z`);
      return Number.isFinite(parsed) ? parsed : null;
    }
    const parsed = Date.parse(bucket);
    return Number.isFinite(parsed) ? parsed : null;
  }

  return null;
};
