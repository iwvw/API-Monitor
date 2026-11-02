export const DEFAULT_PAAS_REFRESH_INTERVAL_SEC = 30;
export const MIN_PAAS_REFRESH_INTERVAL_SEC = 5;
export const MAX_PAAS_REFRESH_INTERVAL_SEC = 3600;

export const clampRefreshIntervalSec = (value) => (
  Math.min(MAX_PAAS_REFRESH_INTERVAL_SEC, Math.max(MIN_PAAS_REFRESH_INTERVAL_SEC, Math.round(value)))
);

export const normalizeRefreshIntervalInputSec = (value) => {
  const numeric = Number(value);
  if (!Number.isFinite(numeric) || numeric <= 0) return DEFAULT_PAAS_REFRESH_INTERVAL_SEC;
  return clampRefreshIntervalSec(numeric);
};

export const normalizeStoredRefreshIntervalSec = (value) => {
  const numeric = Number(value);
  if (!Number.isFinite(numeric) || numeric <= 0) return DEFAULT_PAAS_REFRESH_INTERVAL_SEC;

  let seconds = numeric >= 1000 ? numeric / 1000 : numeric;
  while (
    seconds > MAX_PAAS_REFRESH_INTERVAL_SEC &&
    Number.isInteger(seconds / 1000) &&
    seconds / 1000 >= MIN_PAAS_REFRESH_INTERVAL_SEC
  ) {
    seconds /= 1000;
  }

  return clampRefreshIntervalSec(seconds);
};

export const normalizeFlyImageForInput = (image) => {
  const value = String(image || '').trim();
  const mirrorPrefix = 'docker-hub-mirror.fly.io/';
  if (value.startsWith(mirrorPrefix)) {
    return value.slice(mirrorPrefix.length);
  }
  return value;
};
