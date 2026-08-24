import { describe, expect, it, vi, beforeEach, afterEach } from 'vitest';

import {
  listOnepanelConfigs,
  createOnepanelConfig,
  updateOnepanelConfig,
  deleteOnepanelConfig,
  getOnepanelOverview,
  getOnepanelHealth,
  getOnepanelDashboardCurrent,
  listOnepanelWebsites,
  operateOnepanelWebsite,
  listOnepanelContainers,
  operateOnepanelContainers,
  reloadOnepanelOpenresty,
  proxyOnepanel,
  getOnepanelSpec,
  getOnepanelCatalog,
} from './onepanel.js';

const okResponse = (payload, ok = true) => ({ ok, json: async () => payload });

describe('onepanel requests', () => {
  let fetchMock;

  beforeEach(() => {
    vi.unstubAllGlobals();
    fetchMock = vi.fn();
    vi.stubGlobal('fetch', fetchMock);
  });

  afterEach(() => {
    vi.unstubAllGlobals();
  });

  it('listOnepanelConfigs issues a bare GET without a body', async () => {
    fetchMock.mockResolvedValue(okResponse({ success: true, data: [] }));
    await listOnepanelConfigs();
    expect(fetchMock).toHaveBeenCalledWith('/api/onepanel/config', { method: 'GET', headers: {} });
  });

  it('createOnepanelConfig posts a JSON body', async () => {
    fetchMock.mockResolvedValue(okResponse({ success: true, data: { id: 1 } }));
    await createOnepanelConfig({ name: 'p1', baseUrl: 'http://x' });
    expect(fetchMock).toHaveBeenCalledWith('/api/onepanel/config', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: '{"name":"p1","baseUrl":"http://x"}',
    });
  });

  it('updateOnepanelConfig encodes the serverId', async () => {
    fetchMock.mockResolvedValue(okResponse({ success: true }));
    await updateOnepanelConfig('srv 1', { name: 'p2' });
    expect(fetchMock).toHaveBeenCalledWith('/api/onepanel/config/srv%201', {
      method: 'PUT',
      headers: { 'Content-Type': 'application/json' },
      body: '{"name":"p2"}',
    });
  });

  it('deleteOnepanelConfig issues a body-less DELETE', async () => {
    fetchMock.mockResolvedValue(okResponse({ success: true }));
    await deleteOnepanelConfig('srv/1');
    expect(fetchMock).toHaveBeenCalledWith('/api/onepanel/config/srv%2F1', {
      method: 'DELETE',
      headers: {},
    });
  });

  it('getOnepanelOverview hits the encoded overview url', async () => {
    fetchMock.mockResolvedValue(okResponse({ success: true, data: {} }));
    await getOnepanelOverview('srv 1');
    expect(fetchMock).toHaveBeenCalledWith('/api/onepanel/srv%201/overview');
  });

  it('getOnepanelHealth hits the encoded health url', async () => {
    fetchMock.mockResolvedValue(okResponse({ success: true, data: { ok: true } }));
    await getOnepanelHealth('srv 1');
    expect(fetchMock).toHaveBeenCalledWith('/api/onepanel/srv%201/health');
  });

  it('getOnepanelDashboardCurrent hits the encoded dashboard url', async () => {
    fetchMock.mockResolvedValue(okResponse({ success: true, data: {} }));
    await getOnepanelDashboardCurrent('srv 1');
    expect(fetchMock).toHaveBeenCalledWith('/api/onepanel/srv%201/dashboard/current');
  });

  it('listOnepanelWebsites hits the encoded websites url', async () => {
    fetchMock.mockResolvedValue(okResponse({ success: true, data: [] }));
    await listOnepanelWebsites('srv 1');
    expect(fetchMock).toHaveBeenCalledWith('/api/onepanel/srv%201/websites');
  });

  it('operateOnepanelWebsite posts id and operate', async () => {
    fetchMock.mockResolvedValue(okResponse({ success: true }));
    await operateOnepanelWebsite('srv 1', 'w1', 'restart');
    expect(fetchMock).toHaveBeenCalledWith('/api/onepanel/srv%201/websites/w1/operate', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: '{"id":"w1","operate":"restart"}',
    });
  });

  it('listOnepanelContainers hits the encoded containers url', async () => {
    fetchMock.mockResolvedValue(okResponse({ success: true, data: [] }));
    await listOnepanelContainers('srv 1');
    expect(fetchMock).toHaveBeenCalledWith('/api/onepanel/srv%201/containers');
  });

  it('operateOnepanelContainers posts names and operation', async () => {
    fetchMock.mockResolvedValue(okResponse({ success: true }));
    await operateOnepanelContainers('srv 1', ['a', 'b'], 'stop');
    expect(fetchMock).toHaveBeenCalledWith('/api/onepanel/srv%201/containers/operate', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: '{"names":["a","b"],"operation":"stop"}',
    });
  });

  it('reloadOnepanelOpenresty posts without a body', async () => {
    fetchMock.mockResolvedValue(okResponse({ success: true }));
    await reloadOnepanelOpenresty('srv 1');
    expect(fetchMock).toHaveBeenCalledWith('/api/onepanel/srv%201/openresty/reload', {
      method: 'POST',
      headers: {},
    });
  });

  it('proxyOnepanel defaults the missing body to an empty object', async () => {
    fetchMock.mockResolvedValue(okResponse({ success: true, data: {} }));
    const v1Base = '/api' + '/v1';
    const target = `${v1Base}/summary`;
    await proxyOnepanel('srv 1', 'GET', target);
    expect(fetchMock).toHaveBeenCalledWith('/api/onepanel/srv%201/proxy', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: `{"method":"GET","path":"${target}","body":{}}`,
    });
  });

  it('proxyOnepanel keeps an explicit body', async () => {
    fetchMock.mockResolvedValue(okResponse({ success: true, data: {} }));
    const v1Base = '/api' + '/v1';
    const target = `${v1Base}/op`;
    await proxyOnepanel('srv 1', 'POST', target, { a: 1 });
    expect(fetchMock.mock.calls[0][1].body).toBe(`{"method":"POST","path":"${target}","body":{"a":1}}`);
  });

  it('getOnepanelSpec hits the spec url', async () => {
    fetchMock.mockResolvedValue(okResponse({ success: true, data: {} }));
    await getOnepanelSpec();
    expect(fetchMock).toHaveBeenCalledWith('/api/onepanel/spec');
  });

  it('getOnepanelCatalog hits the encoded catalog url', async () => {
    fetchMock.mockResolvedValue(okResponse({ success: true, data: [] }));
    await getOnepanelCatalog('srv 1');
    expect(fetchMock).toHaveBeenCalledWith('/api/onepanel/srv%201/proxy/catalog');
  });
});

describe('onepanel parseResponse', () => {
  let fetchMock;

  beforeEach(() => {
    vi.unstubAllGlobals();
    fetchMock = vi.fn();
    vi.stubGlobal('fetch', fetchMock);
  });

  afterEach(() => {
    vi.unstubAllGlobals();
  });

  it('throws with code and details on non-ok responses', async () => {
    fetchMock.mockResolvedValue(okResponse({ error: '拒绝', code: 'E401', details: { x: 1 } }, false));
    const err = await listOnepanelConfigs().catch((e) => e);
    expect(err.message).toBe('拒绝');
    expect(err.code).toBe('E401');
    expect(err.details).toEqual({ x: 1 });
  });

  it('throws with code and details when success is false', async () => {
    fetchMock.mockResolvedValue(okResponse({ success: false, error: '业务错误', code: 'E100' }));
    const err = await listOnepanelConfigs().catch((e) => e);
    expect(err.message).toBe('业务错误');
    expect(err.code).toBe('E100');
  });

  it('falls back to a default message without code or details', async () => {
    fetchMock.mockResolvedValue(okResponse({ success: false }));
    const err = await listOnepanelConfigs().catch((e) => e);
    expect(err.message).toBe('1Panel 请求失败');
    expect(err.code).toBeUndefined();
    expect(err.details).toBeUndefined();
  });

  it('tolerates JSON parse failures', async () => {
    fetchMock.mockResolvedValue({ ok: false, json: async () => { throw new SyntaxError('bad json'); } });
    const err = await listOnepanelConfigs().catch((e) => e);
    expect(err.message).toBe('1Panel 请求失败');
  });
});