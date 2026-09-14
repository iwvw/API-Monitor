// 华为云页面纯函数：请求头、响应解包、格式化、状态色档。
export const getAuthHeaders = () => ({
  'Content-Type': 'application/json',
});

export const unwrap = (result) => result?.data ?? result ?? {};

export const currentBillCycle = () => {
  const d = new Date(Date.now() + 8 * 3600 * 1000);
  return `${d.getUTCFullYear()}-${String(d.getUTCMonth() + 1).padStart(2, '0')}`;
};

export const formatBytes = (bytes) => {
  if (!bytes && bytes !== 0) return '-';
  if (bytes < 1024) return `${bytes} B`;
  const units = ['KB', 'MB', 'GB', 'TB'];
  let value = bytes;
  let unit = -1;
  do { value /= 1024; unit += 1; } while (value >= 1024 && unit < units.length - 1);
  return `${value.toFixed(1)} ${units[unit]}`;
};

export const formatMoney = (value) => {
  const num = Number(value) || 0;
  return num.toFixed(2);
};

export function getStatusTone(status) {
  const normalized = String(status || '').trim().toUpperCase();
  if (['ACTIVE', 'SUCCESS', 'IN_USE', 'RUNNING', 'NORMAL', 'BIND', 'ENABLE'].includes(normalized)) return 'success';
  if (['SHUTOFF', 'STOPPED', 'FREE', 'DISABLE', 'ERROR', 'REBOOT'].includes(normalized)) return 'danger';
  if (['REBOOT', 'BUILD', 'STOPPING', 'PENDING'].includes(normalized)) return 'info';
  return 'neutral';
}
