// 共享请求客户端：统一请求生命周期（JSON 序列化、Content-Type、success:false 归一、
// 错误对象携带 code/details/status）。鉴权走同源 cookie，无需显式头部。
//
// 返回值语义与既有模块保持一致：成功时返回解析后的完整 payload（不拆 data），
// 由调用方按需取 .data。失败时抛 Error，附带 code / details / status 字段。
//
// options:
//   - headers          额外请求头，与默认头合并
//   - raw: true        返回原始 Response（blob/text 等非 JSON 响应）
//   - fallbackMessage  响应体无错误信息时的兜底文案
//   - 其余字段透传给 fetch（如 signal、cache）

export function getAuthHeaders() {
  return { 'Content-Type': 'application/json' };
}

const DEFAULT_ERROR_MESSAGE = '请求失败';

function buildError(payload, response, fallbackMessage) {
  const message = payload?.error || payload?.message || fallbackMessage || `${DEFAULT_ERROR_MESSAGE} (${response.status})`;
  const error = new Error(message);
  error.status = response.status;
  if (payload?.code !== undefined) error.code = payload.code;
  if (payload?.details !== undefined) error.details = payload.details;
  return error;
}

export async function request(method, path, body, options = {}) {
  const { headers, raw, fallbackMessage, ...rest } = options;
  const isForm = typeof FormData !== 'undefined' && body instanceof FormData;

  const init = { method, ...rest };
  const mergedHeaders = { ...(headers || {}) };
  if (body !== undefined) {
    if (isForm) {
      init.body = body;
    } else {
      if (mergedHeaders['Content-Type'] === undefined) mergedHeaders['Content-Type'] = 'application/json';
      init.body = JSON.stringify(body);
    }
  }
  if (Object.keys(mergedHeaders).length > 0) init.headers = mergedHeaders;

  const response = await fetch(path, init);

  if (raw) {
    if (!response.ok) throw buildError({}, response, fallbackMessage);
    return response;
  }

  const payload = await response.json().catch(() => ({}));
  if (!response.ok || payload.success === false) {
    throw buildError(payload, response, fallbackMessage);
  }
  return payload;
}

export const get = (path, options) => request('GET', path, undefined, options);
export const post = (path, body, options) => request('POST', path, body, options);
export const put = (path, body, options) => request('PUT', path, body, options);
export const patch = (path, body, options) => request('PATCH', path, body, options);
export const del = (path, options) => request('DELETE', path, undefined, options);

export default { request, get, post, put, patch, del, getAuthHeaders };
