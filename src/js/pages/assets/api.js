import { request } from '../../modules/apiClient.js';
import { ASSETS_API } from './constants.js';

export const fetchAssets = async (params = {}) => {
  const query = new URLSearchParams();
  Object.entries(params).forEach(([key, value]) => {
    if (value === undefined || value === null || value === '') return;
    query.set(key, String(value));
  });
  const suffix = query.toString();
  const result = await request('GET', `${ASSETS_API}${suffix ? `?${suffix}` : ''}`);
  return Array.isArray(result.data) ? result.data : [];
};

export const fetchAsset = async id => {
  const result = await request('GET', `${ASSETS_API}/${id}`);
  return result.data ?? result;
};

export const createAsset = async body => {
  const result = await request('POST', ASSETS_API, body);
  return result.data ?? result;
};

export const updateAsset = async (id, body) => {
  const result = await request('PUT', `${ASSETS_API}/${id}`, body);
  return result.data ?? result;
};

export const deleteAsset = async id => {
  await request('DELETE', `${ASSETS_API}/${id}`);
};

export const fetchOverview = async () => {
  const result = await request('GET', `${ASSETS_API}/overview`);
  return result.data ?? result;
};

export const fetchExpiring = async (within = 30) => {
  const result = await request('GET', `${ASSETS_API}/expiring?within=${within}`);
  return Array.isArray(result.data) ? result.data : [];
};

export const fetchCategories = async () => {
  const result = await request('GET', `${ASSETS_API}/categories`);
  return result.data ?? result;
};

export const fetchSettings = async () => {
  const result = await request('GET', `${ASSETS_API}/settings`);
  return result.data ?? result;
};

export const updateSettings = async body => {
  const result = await request('PUT', `${ASSETS_API}/settings`, body);
  return result.data ?? result;
};

export const resetSettings = async () => {
  const result = await request('POST', `${ASSETS_API}/settings/reset`);
  return result.data ?? result;
};

export const fetchEvents = async id => {
  const result = await request('GET', `${ASSETS_API}/${id}/events`);
  return Array.isArray(result.data) ? result.data : [];
};

export const fetchAlerts = async id => {
  const result = await request('GET', `${ASSETS_API}/${id}/alerts`);
  return Array.isArray(result.data) ? result.data : [];
};

export const fetchCandidates = async () => {
  const result = await request('GET', `${ASSETS_API}/candidates`);
  return Array.isArray(result.data) ? result.data : [];
};

export const linkAssets = async links => {
  const result = await request('POST', `${ASSETS_API}/links`, { links });
  return Array.isArray(result.data) ? result.data : [];
};

export const refreshAsset = async id => {
  const result = await request('POST', `${ASSETS_API}/${id}/refresh`);
  return result.data ?? result;
};

export const refreshAllAssets = async () => {
  const result = await request('POST', `${ASSETS_API}/refresh-all`);
  return result.data ?? result;
};

export const scanExpiry = async () => {
  const result = await request('POST', `${ASSETS_API}/scan-expiry`);
  return result.data ?? result;
};
