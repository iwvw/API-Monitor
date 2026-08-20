// 统一鉴权头：面板走会话 Cookie，仅需 JSON 内容类型。
export const getAuthHeaders = () => ({
  'Content-Type': 'application/json',
});

// formatErrorResponseForDisplay 把报错 JSON 转为可读文本：合法 JSON 格式化缩进；
// 截断/非 JSON 内容还原字符串内的 \r\n、\n、\t 转义序列，避免挤成一行。
export function formatErrorResponseForDisplay(raw) {
  try {
    return JSON.stringify(JSON.parse(raw), null, 2);
  } catch {
    return raw
      .replace(/\\r\\n/g, '\n')
      .replace(/\\n/g, '\n')
      .replace(/\\t/g, '  ');
  }
}

// errorKindLabel 把后端错误环节代号转成简短中文标签，供详情弹窗展示。
export function errorKindLabel(kind) {
  const labels = {
    no_endpoint: '无可用端点',
    bad_request: '请求无效',
    gateway: '网关故障',
    blocked: '网关限制',
    upstream: '上游报错',
    bad_gateway: '上游不可达',
    dial: '连接失败',
    config: '端点配置错误',
  };
  return labels[kind] || kind || '未知';
}

// 从 Kumo CSS 变量读取主题色，供 ECharts 等需要真实颜色值的场景使用。
export const kumoHex = name => {
  try {
    const value = getComputedStyle(document.documentElement).getPropertyValue(name).trim();
    return value || undefined;
  } catch {
    return undefined;
  }
};

// 大数字压缩为「万 / 亿」单位；小数位默认 2 位。
export const formatCompact = (value, decimals = 2) => {
  const num = Number(value);
  if (!Number.isFinite(num)) return String(value);
  const abs = Math.abs(num);
  if (abs >= 1e8) return `${(num / 1e8).toFixed(decimals)}亿`;
  if (abs >= 1e4) return `${(num / 1e4).toFixed(decimals)}万`;
  if (Number.isInteger(num)) return String(num);
  return num.toFixed(decimals);
};

// 词元统一以百万（M）为单位，保留 2 位小数。
export const formatTokensM = value => `${(Number(value) / 1e6).toFixed(2)}M`;

export function createHealthCheckProgress(total = 0, running = false) {
  return { running, total, completed: 0, healthy: 0, degraded: 0, failed: 0 };
}

// parseProxyEntry 解析代理 URL 为可读摘要与完整值：
// 返回 { label, full, host, ip }。label 为友好名称（优先 # 后的节点名），
// ip 为纯主机地址（不含端口与用户信息），host 为 host:port 或 user:pass@host:port。
export function parseProxyEntry(raw) {
  const value = String(raw || '').trim();
  if (!value) return { label: '', full: value, host: '', ip: '' };
  let label = value;
  let host = '';
  let ip = '';
  try {
    let rest = value;
    let hash = '';
    const hashIndex = rest.indexOf('#');
    if (hashIndex !== -1) {
      hash = rest.slice(hashIndex);
      rest = rest.slice(0, hashIndex);
    }
    // 去掉 scheme://，兼容 socks/http/https 等自定义协议（new URL 对 socks 不解析 host）。
    const schemeEnd = rest.indexOf('://');
    if (schemeEnd !== -1) rest = rest.slice(schemeEnd + 3);
    // 去掉 userinfo，得到 host:port。
    const atIndex = rest.lastIndexOf('@');
    const authority = atIndex !== -1 ? rest.slice(atIndex + 1) : rest;
    host = authority;
    // 去掉端口得到纯 IP/主机名。
    ip = authority.replace(/:\d+$/, '');
    // # 后的 fragment 通常是节点名。
    const fallback = hash ? decodeURIComponent(hash.slice(1)) : '';
    label = fallback || authority || value;
  } catch {
    // 解析失败时展示原文。
  }
  return { label, full: value, host, ip };
}

export function activeModelIdsForEndpoint(endpoint) {
  const disabled = Array.isArray(endpoint?.disabledModels) ? endpoint.disabledModels : [];
  return Array.from(
    new Set(
      (Array.isArray(endpoint?.models) ? endpoint.models : [])
        .map(model => (typeof model === 'string' ? model.trim() : (model?.id || '').trim()))
        .filter(id => id && !disabled.includes(id))
    )
  );
}

// 按请求结果给 pill 上色：先看状态码（失败语义优先），成功再看总耗时。
// 耗时档位相对首字放宽（总耗时含上传+推理+输出）：绿 < 15s，蓝 15-45s，
// 黄 45-120s，红 >= 120s；成功但无输出的低完成度请求保持黄，提醒关注。
export function resultTone(statusCode, completionTokens, latencyMs) {
  const status = Number(statusCode) || 0;
  const ms = Number(latencyMs) || 0;
  if (status === 429) return 'warning';
  if (status >= 500) return 'danger';
  if (status >= 400) return 'warning';
  if (!(Number(completionTokens) > 0)) return 'warning';
  if (ms >= 120000) return 'danger';
  if (ms >= 45000) return 'warning';
  if (ms >= 15000) return 'info';
  return 'success';
}

// ttfbTone 根据首字耗时（毫秒）返回色阶。相对当前网关实际分布（响应常见
// 5-130s，空闲时可低至 1-3s）取档：绿 < 5s 正常，蓝 5-15s 偏慢，黄 15-45s
// 很慢，红 >= 45s 异常/接近超时；无数据（'—'）用灰 neutral 与蓝色区分。
export function ttfbTone(ms) {
  if (ms <= 0) return 'neutral';
  if (ms < 5000) return 'success';
  if (ms < 15000) return 'info';
  if (ms < 45000) return 'warning';
  return 'danger';
}

export function statusCodeTone(code) {
  if (code === 429) return 'warning';
  if (code >= 500) return 'danger';
  if (code >= 400) return 'warning';
  return 'success';
}

// logOutputSpeedText 计算单次请求的输出速度（TPS，输出词元/秒）：
// 输出词元数 ÷ 实际输出耗时（总耗时 − 首字耗时）。非流式无首字（ttfb=0）即用总
// 耗时；输出为 0 或无法计时时返回 null。整数取整、小数保留 1 位。
export function logOutputSpeedText(log) {
  const tokens = Number(log?.completionTokens) || 0;
  const latency = Number(log?.latencyMs) || 0;
  const ttfb = Number(log?.ttfbMs) || 0;
  if (!(tokens > 0)) return null;
  const genMs = Math.max(0, latency - ttfb);
  if (genMs <= 0) return null;
  const tps = (tokens / genMs) * 1000;
  return `${tps >= 100 ? tps.toFixed(0) : tps.toFixed(1)}`;
}

export function maskIp(raw, v6EdgeOnly = false) {
  if (!raw) return raw || '';
  let value = String(raw).trim();
  // 剥掉方括号包裹的 IPv6 端口：[2001:db8::1]:443 → 2001:db8::1。
  const bracketed = value.match(/^\[(.+)\]:\d+$/);
  if (bracketed) value = bracketed[1];
  const colonIdx = value.lastIndexOf(':');
  // 形如 1.2.3.4:5678 时去掉端口；IPv6（含 :: 分隔）不剥端口。
  if (/^\d{1,3}(\.\d{1,3}){3}/.test(value) && colonIdx > -1) {
    value = value.slice(0, colonIdx);
  }
  value = value.replace('[', '').replace(']', '');
  if (value.includes(':')) {
    // IPv6
    const segments = value.split(':');
    if (segments.length <= 2) return value;
    if (v6EdgeOnly) {
      return `${segments[0]}:***::***:${segments[segments.length - 1]}`;
    }
    const head = segments.slice(0, 2).join(':');
    const tail = segments.slice(-2).join(':');
    return `${head}***${tail}`;
  }
  const parts = value.split('.');
  if (parts.length === 4) {
    return `${parts[0]}.***.***.${parts[3]}`;
  }
  return value;
}

export function toLocalDateTimeValue(date) {
  if (!(date instanceof Date) || Number.isNaN(date.getTime())) return '';
  const pad = value => String(value).padStart(2, '0');
  return `${date.getFullYear()}-${pad(date.getMonth() + 1)}-${pad(date.getDate())}T${pad(
    date.getHours()
  )}:${pad(date.getMinutes())}`;
}

export function parseLocalDateTime(value) {
  if (!value) return undefined;
  const date = new Date(value);
  return Number.isNaN(date.getTime()) ? undefined : date;
}