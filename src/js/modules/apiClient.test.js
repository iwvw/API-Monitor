import { describe, expect, it, vi, beforeEach, afterEach } from 'vitest';

import { request, get, post, del, getAuthHeaders } from './apiClient.js';

const okResponse = (payload, ok = true, status = ok ? 200 : 500) => ({
  ok,
  status,
  json: async () => payload,
});

describe('apiClient.request', () => {
  let fetchMock;

  beforeEach(() => {
    vi.unstubAllGlobals();
    fetchMock = vi.fn();
    vi.stubGlobal('fetch', fetchMock);
  });

  afterEach(() => {
    vi.unstubAllGlobals();
  });

  it('serializes JSON bodies and sets Content-Type', async () => {
    fetchMock.mockResolvedValue(okResponse({ success: true }));
    await post('/api/dockerhub/accounts', { a: 1 });
    const [url, init] = fetchMock.mock.calls[0];
    expect(url).toBe('/api/dockerhub/accounts');
    expect(init.method).toBe('POST');
    expect(init.body).toBe('{"a":1}');
    expect(init.headers['Content-Type']).toBe('application/json');
  });

  it('sends GET without a body and without headers', async () => {
    fetchMock.mockResolvedValue(okResponse({ success: true }));
    await get('/api/dockerhub/accounts');
    const [url, init] = fetchMock.mock.calls[0];
    expect(url).toBe('/api/dockerhub/accounts');
    expect(init.method).toBe('GET');
    expect(init.body).toBeUndefined();
    expect(init.headers).toBeUndefined();
  });

  it('does not set Content-Type for FormData bodies', async () => {
    fetchMock.mockResolvedValue(okResponse({ success: true }));
    const form = new FormData();
    form.append('k', 'v');
    await post('/api/dockerhub/accounts/verify', form);
    const init = fetchMock.mock.calls[0][1];
    expect(init.body).toBeInstanceOf(FormData);
    expect(init.headers).toBeUndefined();
  });

  it('returns the full payload without unwrapping data', async () => {
    fetchMock.mockResolvedValue(okResponse({ success: true, data: { id: 7 } }));
    expect(await get('/api/dockerhub/accounts')).toEqual({ success: true, data: { id: 7 } });
  });

  it('throws with code, details and status on success:false', async () => {
    fetchMock.mockResolvedValue(okResponse({ success: false, error: '坏', code: 'E1', details: { x: 1 } }, true, 200));
    const err = await get('/api/dockerhub/accounts').catch((e) => e);
    expect(err.message).toBe('坏');
    expect(err.code).toBe('E1');
    expect(err.details).toEqual({ x: 1 });
    expect(err.status).toBe(200);
  });

  it('throws on non-ok responses using the fallback message', async () => {
    fetchMock.mockResolvedValue(okResponse({}, false, 502));
    const err = await get('/api/dockerhub/accounts', { fallbackMessage: '自定义兜底' }).catch((e) => e);
    expect(err.message).toBe('自定义兜底');
    expect(err.status).toBe(502);
  });

  it('tolerates JSON parse failures', async () => {
    fetchMock.mockResolvedValue({ ok: true, status: 200, json: async () => { throw new SyntaxError('bad'); } });
    expect(await get('/api/dockerhub/accounts')).toEqual({});
  });

  it('raw mode returns the Response and skips JSON parsing', async () => {
    const response = { ok: true, status: 200, json: async () => { throw new Error('should not parse'); } };
    fetchMock.mockResolvedValue(response);
    expect(await request('GET', '/api/dockerhub/accounts', undefined, { raw: true })).toBe(response);
  });

  it('raw mode throws on non-ok responses', async () => {
    fetchMock.mockResolvedValue({ ok: false, status: 404, json: async () => ({}) });
    await expect(del('/api/dockerhub/accounts', { raw: true })).rejects.toThrow();
  });

  it('exposes getAuthHeaders for the documented convention', () => {
    expect(getAuthHeaders()).toEqual({ 'Content-Type': 'application/json' });
  });
});
