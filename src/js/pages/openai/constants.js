export const ENDPOINT_PROTOCOL_OPTIONS = [
  { value: 'auto', label: '自动（HTTP/2 优先）' },
  { value: 'http1', label: 'HTTP/1.1' },
  { value: 'h2', label: 'HTTP/2' },
];

// 大代理池（文件批量导入可达数千条）在表单/管理弹窗中只预览前 N 条，避免渲染卡顿。
export const PROXY_PREVIEW_LIMIT = 120;

// 报错详情弹窗的超长折叠阈值：超过该字符数的报错 JSON 默认只显示前缀，可一键展开。
export const LOG_DETAIL_COLLAPSE_LIMIT = 8000;

export const GATEWAY_EXPIRY_HOURS = Array.from({ length: 24 }, (_, hour) => {
  const value = String(hour).padStart(2, '0');
  return { value, label: value };
});

export const GATEWAY_EXPIRY_MINUTES = Array.from({ length: 60 }, (_, minute) => {
  const value = String(minute).padStart(2, '0');
  return { value, label: value };
});