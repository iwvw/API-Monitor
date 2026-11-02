export const getAuthHeaders = () => ({
  'Content-Type': 'application/json',
});

export const getUploadHeaders = () => ({
});

export const formatFileSize = (bytes) => {
  const size = Number(bytes) || 0;
  if (size >= 1024 * 1024 * 1024) return `${(size / 1024 / 1024 / 1024).toFixed(2)} GB`;
  if (size >= 1024 * 1024) return `${(size / 1024 / 1024).toFixed(2)} MB`;
  if (size >= 1024) return `${(size / 1024).toFixed(2)} KB`;
  return `${size} B`;
};

export const formatSessionTime = (value) => {
  if (!value) return '-';
  const date = new Date(value);
  if (Number.isNaN(date.getTime())) return '-';
  return date.toLocaleString('zh-CN', { hour12: false });
};

export const describeUserAgent = (value) => {
  const ua = String(value || '');
  const browser = ua.includes('Edg/') ? 'Edge' : ua.includes('Chrome/') ? 'Chrome' : ua.includes('Firefox/') ? 'Firefox' : ua.includes('Safari/') ? 'Safari' : '浏览器';
  const platform = ua.includes('Windows') ? 'Windows' : ua.includes('Mac OS') ? 'macOS' : ua.includes('Android') ? 'Android' : ua.includes('iPhone') || ua.includes('iPad') ? 'iOS' : ua.includes('Linux') ? 'Linux' : '未知系统';
  return `${browser} · ${platform}`;
};

export const toInt = (value, fallback = 0) => {
  const parsed = parseInt(value, 10);
  return Number.isFinite(parsed) ? parsed : fallback;
};
