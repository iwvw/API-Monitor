async function parseResponse(response) {
  const data = await response.json().catch(() => ({}));
  if (!response.ok || data.success === false) {
    const error = new Error(data.error || '1Panel 请求失败');
    error.code = data.code;
    error.details = data.details;
    throw error;
  }
  return data;
}

function request(method, path, body) {
  const options = { method, headers: {} };
  if (body !== undefined) {
    options.headers['Content-Type'] = 'application/json';
    options.body = JSON.stringify(body);
  }
  return fetch(path, options).then(parseResponse);
}

// === 配置管理 ===
export function listOnepanelConfigs() {
  return request('GET', '/api/onepanel/config');
}

export function createOnepanelConfig(payload) {
  return request('POST', '/api/onepanel/config', payload);
}

export function updateOnepanelConfig(serverId, payload) {
  return request('PUT', `/api/onepanel/config/${encodeURIComponent(serverId)}`, payload);
}

export function deleteOnepanelConfig(serverId) {
  return request('DELETE', `/api/onepanel/config/${encodeURIComponent(serverId)}`);
}

// === 面板与总览 ===
export async function getOnepanelOverview(serverId) {
  const response = await fetch(`/api/onepanel/${encodeURIComponent(serverId)}/overview`);
  return parseResponse(response);
}

export async function getOnepanelHealth(serverId) {
  const response = await fetch(`/api/onepanel/${encodeURIComponent(serverId)}/health`);
  return parseResponse(response);
}

export async function getOnepanelDashboardCurrent(serverId) {
  const response = await fetch(`/api/onepanel/${encodeURIComponent(serverId)}/dashboard/current`);
  return parseResponse(response);
}

// === 网站 ===
export async function listOnepanelWebsites(serverId) {
  const response = await fetch(`/api/onepanel/${encodeURIComponent(serverId)}/websites`);
  return parseResponse(response);
}

export function operateOnepanelWebsite(serverId, websiteId, operate) {
  return request('POST', `/api/onepanel/${encodeURIComponent(serverId)}/websites/${websiteId}/operate`, { id: websiteId, operate });
}

// === 容器 ===
export async function listOnepanelContainers(serverId) {
  const response = await fetch(`/api/onepanel/${encodeURIComponent(serverId)}/containers`);
  return parseResponse(response);
}

export function operateOnepanelContainers(serverId, names, operation) {
  return request('POST', `/api/onepanel/${encodeURIComponent(serverId)}/containers/operate`, { names, operation });
}

// === OpenResty ===
export function reloadOnepanelOpenresty(serverId) {
  return request('POST', `/api/onepanel/${encodeURIComponent(serverId)}/openresty/reload`);
}

// === 通用代理 ===
export function proxyOnepanel(serverId, method, path, body) {
  return request('POST', `/api/onepanel/${encodeURIComponent(serverId)}/proxy`, { method, path, body: body === undefined ? {} : body });
}

// === 内置 API 目录 ===
export async function getOnepanelSpec() {
  const response = await fetch('/api/onepanel/spec');
  return parseResponse(response);
}

export async function getOnepanelCatalog(serverId) {
  const response = await fetch(`/api/onepanel/${encodeURIComponent(serverId)}/proxy/catalog`);
  return parseResponse(response);
}