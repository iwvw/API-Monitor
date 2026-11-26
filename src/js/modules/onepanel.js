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

// 分页搜索网站（比 /websites 更适合大量站点场景）。
export function searchOnepanelWebsites(serverId, query = {}) {
  return post(`/api/onepanel/${enc(serverId)}/websites/search`, query, { fallbackMessage: FALLBACK });
}

// 更新网站基础信息；payload 必须含 id（后端会忽略路径里的 id，以 body 为准）。
export function updateOnepanelWebsite(serverId, websiteId, payload = {}) {
  return post(`/api/onepanel/${enc(serverId)}/websites/${websiteId}/update`, { ...payload, id: websiteId }, { fallbackMessage: FALLBACK });
}

export function listOnepanelWebsiteOptions(serverId) {
  return post(`/api/onepanel/${enc(serverId)}/websites/options`, {}, { fallbackMessage: FALLBACK });
}

// 批量启停网站（payload 形如 { ids: [...], operate: 'start' }）。
export function batchOperateOnepanelWebsites(serverId, payload = {}) {
  return post(`/api/onepanel/${enc(serverId)}/websites/batch/operate`, payload, { fallbackMessage: FALLBACK });
}

// 反代配置：读取 / 启停 / 删除。
export function getOnepanelWebsiteProxyConfig(serverId, websiteId) {
  return post(`/api/onepanel/${enc(serverId)}/websites/${websiteId}/proxies`, { id: websiteId }, { fallbackMessage: FALLBACK });
}

export function deleteOnepanelWebsiteProxy(serverId, websiteId, payload = {}) {
  return post(`/api/onepanel/${enc(serverId)}/websites/${websiteId}/proxies/delete`, { ...payload, id: websiteId }, { fallbackMessage: FALLBACK });
}

export function updateOnepanelWebsiteProxyStatus(serverId, websiteId, payload = {}) {
  return post(`/api/onepanel/${enc(serverId)}/websites/${websiteId}/proxies/status`, { ...payload, id: websiteId }, { fallbackMessage: FALLBACK });
}

// 伪静态 / 重定向 / 防盗链 / 密码访问：读取与更新，路径与 payload 形状一致。
export function getOnepanelWebsiteRewrite(serverId, websiteId) {
  return post(`/api/onepanel/${enc(serverId)}/websites/${websiteId}/rewrite`, { id: websiteId }, { fallbackMessage: FALLBACK });
}

export function updateOnepanelWebsiteRewrite(serverId, websiteId, payload = {}) {
  return post(`/api/onepanel/${enc(serverId)}/websites/${websiteId}/rewrite/update`, { ...payload, id: websiteId }, { fallbackMessage: FALLBACK });
}

export function getOnepanelWebsiteRedirect(serverId, websiteId) {
  return post(`/api/onepanel/${enc(serverId)}/websites/${websiteId}/redirect`, { id: websiteId }, { fallbackMessage: FALLBACK });
}

export function updateOnepanelWebsiteRedirect(serverId, websiteId, payload = {}) {
  return post(`/api/onepanel/${enc(serverId)}/websites/${websiteId}/redirect/update`, { ...payload, id: websiteId }, { fallbackMessage: FALLBACK });
}

export function getOnepanelWebsiteLeech(serverId, websiteId) {
  return post(`/api/onepanel/${enc(serverId)}/websites/${websiteId}/leech`, { id: websiteId }, { fallbackMessage: FALLBACK });
}

export function updateOnepanelWebsiteLeech(serverId, websiteId, payload = {}) {
  return post(`/api/onepanel/${enc(serverId)}/websites/${websiteId}/leech/update`, { ...payload, id: websiteId }, { fallbackMessage: FALLBACK });
}

export function getOnepanelWebsiteAuths(serverId, websiteId) {
  return post(`/api/onepanel/${enc(serverId)}/websites/${websiteId}/auths`, { id: websiteId }, { fallbackMessage: FALLBACK });
}

export function updateOnepanelWebsiteAuths(serverId, websiteId, payload = {}) {
  return post(`/api/onepanel/${enc(serverId)}/websites/${websiteId}/auths/update`, { ...payload, id: websiteId }, { fallbackMessage: FALLBACK });
}

// 目录 / 权限 / 资源。
export function getOnepanelWebsiteDir(serverId, websiteId) {
  return post(`/api/onepanel/${enc(serverId)}/websites/${websiteId}/dir`, { id: websiteId }, { fallbackMessage: FALLBACK });
}

export function updateOnepanelWebsiteDir(serverId, websiteId, payload = {}) {
  return post(`/api/onepanel/${enc(serverId)}/websites/${websiteId}/dir/update`, { ...payload, id: websiteId }, { fallbackMessage: FALLBACK });
}

export function getOnepanelWebsiteResource(serverId, websiteId) {
  return get(`/api/onepanel/${enc(serverId)}/websites/${websiteId}/resource`, { fallbackMessage: FALLBACK });
}

// 网站日志。
export function searchOnepanelWebsiteLogs(serverId, payload = {}) {
  return post(`/api/onepanel/${enc(serverId)}/websites/${payload.id}/log`, payload, { fallbackMessage: FALLBACK });
}

// 证书：按站点查询 / 更新 / 删除 / 上传。
export function getOnepanelWebsiteSsl(serverId, websiteId) {
  return get(`/api/onepanel/${enc(serverId)}/ssl/website/${websiteId}`, { fallbackMessage: FALLBACK });
}

export function updateOnepanelSsl(serverId, sslId, payload = {}) {
  return post(`/api/onepanel/${enc(serverId)}/ssl/${sslId}/update`, { ...payload, id: sslId }, { fallbackMessage: FALLBACK });
}

export function deleteOnepanelSsl(serverId, sslId, payload = {}) {
  return post(`/api/onepanel/${enc(serverId)}/ssl/${sslId}/delete`, { ...payload, id: sslId }, { fallbackMessage: FALLBACK });
}

export function uploadOnepanelSsl(serverId, payload = {}) {
  return post(`/api/onepanel/${enc(serverId)}/ssl/upload`, payload, { fallbackMessage: FALLBACK });
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

// === 反代域名 ===
// 返回 { serverId, targets: [{ upstreamPort, domains: [{ domain, url, ... }] }] }，
// 由容器列表按「上游端口 ↔ 容器宿主端口」自行匹配。
export function getOnepanelReverseProxyDomains(serverId) {
  return get(`/api/onepanel/${enc(serverId)}/reverse-proxy/domains`, { fallbackMessage: FALLBACK });
}

// === 安装探测 ===
// 由 Agent 只读检查 1pctl / /opt/1panel / 1panel-core 进程 / 8888 端口。
// 不传 serverIds 时探测全部主机。
export function probeOnepanelInstall(serverIds) {
  const body = Array.isArray(serverIds) && serverIds.length > 0 ? { serverIds } : {};
  return post('/api/onepanel/probe', body, { fallbackMessage: FALLBACK });
}
