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
  getOnepanelReverseProxyDomains,
  searchOnepanelWebsites,
  updateOnepanelWebsite,
  getOnepanelWebsiteRewrite,
  getOnepanelWebsiteRedirect,
  getOnepanelWebsiteLeech,
  getOnepanelWebsiteAuths,
  updateOnepanelSsl,
  deleteOnepanelSsl,
  getOnepanelWebsiteSsl,
} from './onepanel.js';
import * as mod from './onepanel.js';

const okResponse = (payload, ok = true) => ({ ok, json: async () => payload });

const lastCall = (fetchMock) => fetchMock.mock.calls.at(-1);
const calledUrl = (fetchMock) => lastCall(fetchMock)[0];
const calledInit = (fetchMock) => lastCall(fetchMock)[1] || {};

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
    expect(calledUrl(fetchMock)).toBe('/api/onepanel/config');
    expect(calledInit(fetchMock).method).toBe('GET');
    expect(calledInit(fetchMock).body).toBeUndefined();
  });

  it('createOnepanelConfig posts a JSON body', async () => {
    fetchMock.mockResolvedValue(okResponse({ success: true, data: { id: 1 } }));
    await createOnepanelConfig({ name: 'p1', baseUrl: 'http://x' });
    expect(calledUrl(fetchMock)).toBe('/api/onepanel/config');
    expect(calledInit(fetchMock).method).toBe('POST');
    expect(calledInit(fetchMock).body).toBe('{"name":"p1","baseUrl":"http://x"}');
  });

  it('updateOnepanelConfig encodes the serverId', async () => {
    fetchMock.mockResolvedValue(okResponse({ success: true }));
    await updateOnepanelConfig('srv 1', { name: 'p2' });
    expect(calledUrl(fetchMock)).toBe('/api/onepanel/config/srv%201');
    expect(calledInit(fetchMock).method).toBe('PUT');
    expect(calledInit(fetchMock).body).toBe('{"name":"p2"}');
  });

  it('deleteOnepanelConfig issues a body-less DELETE', async () => {
    fetchMock.mockResolvedValue(okResponse({ success: true }));
    await deleteOnepanelConfig('srv/1');
    expect(calledUrl(fetchMock)).toBe('/api/onepanel/config/srv%2F1');
    expect(calledInit(fetchMock).method).toBe('DELETE');
    expect(calledInit(fetchMock).body).toBeUndefined();
  });

  it('getOnepanelOverview hits the encoded overview url', async () => {
    fetchMock.mockResolvedValue(okResponse({ success: true, data: {} }));
    await getOnepanelOverview('srv 1');
    expect(calledUrl(fetchMock)).toBe('/api/onepanel/srv%201/overview');
  });

  it('getOnepanelHealth hits the encoded health url', async () => {
    fetchMock.mockResolvedValue(okResponse({ success: true, data: { ok: true } }));
    await getOnepanelHealth('srv 1');
    expect(calledUrl(fetchMock)).toBe('/api/onepanel/srv%201/health');
  });

  it('getOnepanelDashboardCurrent hits the encoded dashboard url', async () => {
    fetchMock.mockResolvedValue(okResponse({ success: true, data: {} }));
    await getOnepanelDashboardCurrent('srv 1');
    expect(calledUrl(fetchMock)).toBe('/api/onepanel/srv%201/dashboard/current');
  });

  it('listOnepanelWebsites hits the encoded websites url', async () => {
    fetchMock.mockResolvedValue(okResponse({ success: true, data: [] }));
    await listOnepanelWebsites('srv 1');
    expect(calledUrl(fetchMock)).toBe('/api/onepanel/srv%201/websites');
  });

  it('operateOnepanelWebsite posts id and operate', async () => {
    fetchMock.mockResolvedValue(okResponse({ success: true }));
    await operateOnepanelWebsite('srv 1', 'w1', 'restart');
    expect(calledUrl(fetchMock)).toBe('/api/onepanel/srv%201/websites/w1/operate');
    expect(calledInit(fetchMock).method).toBe('POST');
    expect(calledInit(fetchMock).body).toBe('{"id":"w1","operate":"restart"}');
  });

  it('searchOnepanelWebsites posts paging body', async () => {
    fetchMock.mockResolvedValue(okResponse({ success: true, data: {} }));
    await searchOnepanelWebsites('srv 1', { page: 2, pageSize: 10 });
    expect(calledUrl(fetchMock)).toBe('/api/onepanel/srv%201/websites/search');
    expect(calledInit(fetchMock).body).toBe('{"page":2,"pageSize":10}');
  });

  it('updateOnepanelWebsite injects the id into the body', async () => {
    fetchMock.mockResolvedValue(okResponse({ success: true }));
    await updateOnepanelWebsite('srv 1', 'w1', { remark: '主站' });
    expect(calledUrl(fetchMock)).toBe('/api/onepanel/srv%201/websites/w1/update');
    expect(calledInit(fetchMock).body).toBe('{"remark":"主站","id":"w1"}');
  });

  it('website config helpers hit the right sub-paths', async () => {
    fetchMock.mockResolvedValue(okResponse({ success: true }));
    const cases = [
      ['getOnepanelWebsiteRewrite', '/rewrite'],
      ['getOnepanelWebsiteRedirect', '/redirect'],
      ['getOnepanelWebsiteLeech', '/leech'],
      ['getOnepanelWebsiteAuths', '/auths'],
    ];
    for (const [fnName, sub] of cases) {
      await mod[fnName]('srv 1', 'w1');
      expect(calledUrl(fetchMock)).toBe(`/api/onepanel/srv%201/websites/w1${sub}`);
      expect(calledInit(fetchMock).body).toBe('{"id":"w1"}');
    }
  });

  it('ssl helpers encode the ssl id', async () => {
    fetchMock.mockResolvedValue(okResponse({ success: true }));
    await updateOnepanelSsl('srv 1', 9, { description: 'x' });
    expect(calledUrl(fetchMock)).toBe('/api/onepanel/srv%201/ssl/9/update');
    expect(calledInit(fetchMock).body).toBe('{"description":"x","id":9}');
    await deleteOnepanelSsl('srv 1', 9);
    expect(calledUrl(fetchMock)).toBe('/api/onepanel/srv%201/ssl/9/delete');
    expect(calledInit(fetchMock).body).toBe('{"id":9}');
  });

  it('getOnepanelWebsiteSsl hits the by-website url', async () => {
    fetchMock.mockResolvedValue(okResponse({ success: true }));
    await getOnepanelWebsiteSsl('srv 1', 'w1');
    expect(calledUrl(fetchMock)).toBe('/api/onepanel/srv%201/ssl/website/w1');
    expect(calledInit(fetchMock).method).toBe('GET');
  });

  it('getOnepanelReverseProxyDomains hits the encoded reverse-proxy url', async () => {    fetchMock.mockResolvedValue(okResponse({ success: true, data: { targets: [] } }));
    await getOnepanelReverseProxyDomains('srv 1');
    expect(calledUrl(fetchMock)).toBe('/api/onepanel/srv%201/reverse-proxy/domains');
    expect(calledInit(fetchMock).method).toBe('GET');
  });

  it('listOnepanelContainers hits the encoded containers url', async () => {    fetchMock.mockResolvedValue(okResponse({ success: true, data: [] }));
    await listOnepanelContainers('srv 1');
    expect(calledUrl(fetchMock)).toBe('/api/onepanel/srv%201/containers');
  });

  it('operateOnepanelContainers posts names and operation', async () => {
    fetchMock.mockResolvedValue(okResponse({ success: true }));
    await operateOnepanelContainers('srv 1', ['a', 'b'], 'stop');
    expect(calledUrl(fetchMock)).toBe('/api/onepanel/srv%201/containers/operate');
    expect(calledInit(fetchMock).method).toBe('POST');
    expect(calledInit(fetchMock).body).toBe('{"names":["a","b"],"operation":"stop"}');
  });

  it('reloadOnepanelOpenresty posts without a body', async () => {
    fetchMock.mockResolvedValue(okResponse({ success: true }));
    await reloadOnepanelOpenresty('srv 1');
    expect(calledUrl(fetchMock)).toBe('/api/onepanel/srv%201/openresty/reload');
    expect(calledInit(fetchMock).method).toBe('POST');
    expect(calledInit(fetchMock).body).toBeUndefined();
  });

  it('proxyOnepanel defaults the missing body to an empty object', async () => {
    fetchMock.mockResolvedValue(okResponse({ success: true, data: {} }));
    const v1Base = '/api' + '/v1';
    const target = `${v1Base}/summary`;
    await proxyOnepanel('srv 1', 'GET', target);
    expect(calledUrl(fetchMock)).toBe('/api/onepanel/srv%201/proxy');
    expect(calledInit(fetchMock).body).toBe(`{"method":"GET","path":"${target}","body":{}}`);
  });

  it('proxyOnepanel keeps an explicit body', async () => {
    fetchMock.mockResolvedValue(okResponse({ success: true, data: {} }));
    const v1Base = '/api' + '/v1';
    const target = `${v1Base}/op`;
    await proxyOnepanel('srv 1', 'POST', target, { a: 1 });
    expect(calledInit(fetchMock).body).toBe(`{"method":"POST","path":"${target}","body":{"a":1}}`);
  });

  it('getOnepanelSpec hits the spec url', async () => {
    fetchMock.mockResolvedValue(okResponse({ success: true, data: {} }));
    await getOnepanelSpec();
    expect(calledUrl(fetchMock)).toBe('/api/onepanel/spec');
  });

  it('getOnepanelCatalog hits the encoded catalog url', async () => {
    fetchMock.mockResolvedValue(okResponse({ success: true, data: [] }));
    await getOnepanelCatalog('srv 1');
    expect(calledUrl(fetchMock)).toBe('/api/onepanel/srv%201/proxy/catalog');
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
