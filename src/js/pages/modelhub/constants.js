export const ENDPOINT_PROTOCOL_OPTIONS = [
  { value: 'auto', label: '自动（HTTP/2 优先）' },
  { value: 'http1', label: 'HTTP/1.1' },
  { value: 'h2', label: 'HTTP/2' },
];

// 端点上游协议类型：OpenAI 兼容（默认）或 Google Gemini（AI Studio / Interactions API）。
export const ENDPOINT_UPSTREAM_OPTIONS = [
  { value: 'openai', label: 'OpenAI 兼容' },
  { value: 'gemini', label: 'Google Gemini' },
  { value: 'vertex', label: 'Google Vertex AI' },
];

// Gemini 上游官方地址：一般不变，选 Gemini 且 Base URL 为空时自动预填。
export const GEMINI_DEFAULT_BASE_URL = 'https://generativelanguage.googleapis.com';

// Vertex AI 上游官方地址（global 区域，请求会路由到可用区域；模型 ID 带
// -preview 或刚发布的新模型建议用 global）。固定区域需改 Base URL 为
// https://{区域}-aiplatform.googleapis.com/v1/publishers/google。
export const VERTEX_DEFAULT_BASE_URL = 'https://aiplatform.googleapis.com/v1/publishers/google';

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