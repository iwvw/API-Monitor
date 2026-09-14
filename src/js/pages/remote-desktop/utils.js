import { CANDIDATE_TYPE_LABELS, STATE_LABELS } from './constants.js';

export function serverIdFromPath() {
  const match = window.location.pathname.match(/^\/remote-desktop\/([^/]+)$/);
  return match ? decodeURIComponent(match[1]) : '';
}

export function authHeaders(json = false) {
  return {
    ...(json ? { 'Content-Type': 'application/json' } : {}),
  };
}

export async function apiRequest(url, options = {}) {
  const response = await fetch(url, options);
  const payload = await response.json().catch(() => ({}));
  if (!response.ok || payload.success === false) {
    const error = new Error(payload.error || `请求失败 (${response.status})`);
    error.status = response.status;
    throw error;
  }
  return payload.data ?? payload;
}

export function stateLabel(state) {
  return STATE_LABELS[state] || state;
}

// Map a WebRTC candidate type to a user-meaningful label. `host` means a direct
// LAN path (bypasses proxies/TUN); `srflx` is a public-IP hole-punch; `relay`
// goes through a TURN server. Knowing which one is active helps diagnose why a
// proxied/TUN machine cannot connect.
export function candidateTypeLabel(candidate) {
  const parts = String(candidate || '').split(' · ');
  const type = (parts[0] || '').trim();
  const protocol = (parts[1] || '').trim();
  const typeLabel = CANDIDATE_TYPE_LABELS[type] || type || '';
  const protoLabel = protocol === 'tcp' ? 'TCP' : 'UDP';
  return `${typeLabel}${protoLabel ? `(${protoLabel})` : ''}`;
}
