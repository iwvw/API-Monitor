import { del, get, post, put } from './apiClient.js';

const FALLBACK = '1Panel 请求失败';

const enc = (value) => encodeURIComponent(value);

// === 配置管理 ===
export function listOnepanelConfigs() {
  return get('/api/onepanel/config', { fallbackMessage: FALLBACK });
}

export function createOnepanelConfig(payload) {
  return post('/api/onepanel/config', payload, { fallbackMessage: FALLBACK });
}

export function updateOnepanelConfig(serverId, payload) {
  return put(`/api/onepanel/config/${enc(serverId)}`, payload, { fallbackMessage: FALLBACK });
}

export function deleteOnepanelConfig(serverId) {
  return del(`/api/onepanel/config/${enc(serverId)}`, { fallbackMessage: FALLBACK });
}

// === 面板与总览 ===
export function getOnepanelOverview(serverId) {
  return get(`/api/onepanel/${enc(serverId)}/overview`, { fallbackMessage: FALLBACK });
}

export function getOnepanelHealth(serverId) {
  return get(`/api/onepanel/${enc(serverId)}/health`, { fallbackMessage: FALLBACK });
}

export function getOnepanelDashboardCurrent(serverId) {
  return get(`/api/onepanel/${enc(serverId)}/dashboard/current`, { fallbackMessage: FALLBACK });
}

// === 网站 ===
export function listOnepanelWebsites(serverId) {
  return get(`/api/onepanel/${enc(serverId)}/websites`, { fallbackMessage: FALLBACK });
}

export function operateOnepanelWebsite(serverId, websiteId, operate) {
  return post(`/api/onepanel/${enc(serverId)}/websites/${websiteId}/operate`, { id: websiteId, operate }, { fallbackMessage: FALLBACK });
}

// === 容器 ===
export function listOnepanelContainers(serverId) {
  return get(`/api/onepanel/${enc(serverId)}/containers`, { fallbackMessage: FALLBACK });
}

export function operateOnepanelContainers(serverId, names, operation) {
  return post(`/api/onepanel/${enc(serverId)}/containers/operate`, { names, operation }, { fallbackMessage: FALLBACK });
}

// === OpenResty ===
export function reloadOnepanelOpenresty(serverId) {
  return post(`/api/onepanel/${enc(serverId)}/openresty/reload`, undefined, { fallbackMessage: FALLBACK });
}

// === 通用代理 ===
export function proxyOnepanel(serverId, method, path, body) {
  return post(`/api/onepanel/${enc(serverId)}/proxy`, { method, path, body: body === undefined ? {} : body }, { fallbackMessage: FALLBACK });
}

// === 内置 API 目录 ===
export function getOnepanelSpec() {
  return get('/api/onepanel/spec', { fallbackMessage: FALLBACK });
}

export function getOnepanelCatalog(serverId) {
  return get(`/api/onepanel/${enc(serverId)}/proxy/catalog`, { fallbackMessage: FALLBACK });
}
