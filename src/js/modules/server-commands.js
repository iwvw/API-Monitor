import { del, get, post, put } from './apiClient.js';

const FALLBACK = '快速命令请求失败';

function buildQuery(filters = {}) {
  const params = new URLSearchParams();
  Object.entries(filters).forEach(([key, value]) => {
    if (value !== undefined && value !== null && value !== '') params.set(key, value);
  });
  return params.toString();
}

export async function fetchCommandSnippets(filters = {}) {
  return get(`/api/server/snippets?${buildQuery(filters)}`, { fallbackMessage: FALLBACK });
}

export async function createCommandSnippet(payload) {
  return post('/api/server/snippets', payload, { fallbackMessage: FALLBACK });
}

export async function updateCommandSnippet(id, payload) {
  return put(`/api/server/snippets/${id}`, payload, { fallbackMessage: FALLBACK });
}

export async function deleteCommandSnippet(id) {
  return del(`/api/server/snippets/${id}`, { fallbackMessage: FALLBACK });
}

export async function previewCommand(payload) {
  return post('/api/server/snippets/preview', payload, { fallbackMessage: FALLBACK });
}

export async function recordCommandHistory(payload) {
  return post('/api/server/snippets/history', payload, { fallbackMessage: FALLBACK });
}

export async function fetchCommandHistory(filters = {}) {
  return get(`/api/server/snippets/history?${buildQuery(filters)}`, { fallbackMessage: FALLBACK });
}
