/* global Response, Request */
import { describe, expect, it, vi, afterEach } from 'vitest';

const ORIGIN = 'https://panel.example.com';

const loadGuard = async () => {
  vi.resetModules();
  return import('./authGuard.js');
};

const stubWindow = (fetchImpl) => {
  vi.stubGlobal('window', {
    location: { origin: ORIGIN },
    fetch: fetchImpl,
  });
};

const respondWith = (status) => vi.fn(async () => new Response('{}', { status }));

afterEach(() => {
  vi.unstubAllGlobals();
});

describe('installAuthGuard', () => {
  it('同源 /api/ 请求收到 401 时触发回调并返回原响应', async () => {
    stubWindow(respondWith(401));
    const { installAuthGuard } = await loadGuard();
    const onUnauthorized = vi.fn();

    installAuthGuard(onUnauthorized);

    const response = await window.fetch('/api/settings');
    expect(response.status).toBe(401);
    expect(onUnauthorized).toHaveBeenCalledTimes(1);
  });

  it('成功响应不触发回调', async () => {
    stubWindow(respondWith(200));
    const { installAuthGuard } = await loadGuard();
    const onUnauthorized = vi.fn();

    installAuthGuard(onUnauthorized);

    await window.fetch('/api/settings');
    expect(onUnauthorized).not.toHaveBeenCalled();
  });

  it('非 /api/ 前缀（如网关 /v1）的 401 不触发回调', async () => {
    stubWindow(respondWith(401));
    const { installAuthGuard } = await loadGuard();
    const onUnauthorized = vi.fn();

    installAuthGuard(onUnauthorized);

    await window.fetch('/v1/chat/completions');
    expect(onUnauthorized).not.toHaveBeenCalled();
  });

  it('跨域 /api/ 的 401 不触发回调', async () => {
    stubWindow(respondWith(401));
    const { installAuthGuard } = await loadGuard();
    const onUnauthorized = vi.fn();

    installAuthGuard(onUnauthorized);

    await window.fetch('https://evil.example.com/api/settings');
    expect(onUnauthorized).not.toHaveBeenCalled();
  });

  it('支持 Request 对象输入', async () => {
    stubWindow(respondWith(401));
    const { installAuthGuard } = await loadGuard();
    const onUnauthorized = vi.fn();

    installAuthGuard(onUnauthorized);

    await window.fetch(new Request(`${ORIGIN}/api/uptime/monitors`));
    expect(onUnauthorized).toHaveBeenCalledTimes(1);
  });

  it('重复安装只包装一次 fetch', async () => {
    stubWindow(respondWith(401));
    const { installAuthGuard } = await loadGuard();
    const first = vi.fn();
    const second = vi.fn();

    installAuthGuard(first);
    installAuthGuard(second);

    await window.fetch('/api/settings');
    expect(first).toHaveBeenCalledTimes(1);
    expect(second).not.toHaveBeenCalled();
  });

  it('缺少 window 或回调时静默跳过', async () => {
    const { installAuthGuard } = await loadGuard();
    expect(() => installAuthGuard(() => {})).not.toThrow();

    stubWindow(respondWith(401));
    const mod = await loadGuard();
    expect(() => mod.installAuthGuard(null)).not.toThrow();
  });
});
