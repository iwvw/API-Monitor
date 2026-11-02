import { describe, expect, it, beforeEach, afterEach, vi } from 'vitest';
import { apiRequest } from './utils.js';

// 这些用例锁定「接管」与「普通失败」的区分契约：后端用 409 + reason=superseded
// 表达「会话已被同主机的新连接接管」，前端必须把 reason 透传到 error 上，否则
// 页面会把它当成终态错误、提示刷新并永久停止重连（用户遇到的死页就是这样来的）。
describe('remote desktop apiRequest', () => {
  beforeEach(() => {
    vi.stubGlobal('fetch', vi.fn());
  });

  afterEach(() => {
    vi.unstubAllGlobals();
  });

  function respond(status, payload) {
    fetch.mockResolvedValue({
      ok: status >= 200 && status < 300,
      status,
      json: async () => payload,
    });
  }

  it('returns payload.data on success', async () => {
    respond(200, { success: true, data: { sessionId: 'abc' } });
    await expect(apiRequest('/x')).resolves.toEqual({ sessionId: 'abc' });
  });

  it('carries status and reason for a superseded session', async () => {
    respond(409, {
      success: false,
      error: 'remote desktop session superseded by a newer connection',
      reason: 'superseded',
    });
    await expect(apiRequest('/x')).rejects.toMatchObject({
      status: 409,
      reason: 'superseded',
    });
  });

  it('leaves reason undefined for ordinary failures so auto-reconnect still applies', async () => {
    respond(503, { success: false, error: 'agent offline' });
    const error = await apiRequest('/x').catch(err => err);
    expect(error.status).toBe(503);
    expect(error.reason).toBeUndefined();
  });

  it('surfaces the status when the body is not JSON', async () => {
    fetch.mockResolvedValue({ ok: false, status: 502, json: async () => { throw new Error('bad json'); } });
    const error = await apiRequest('/x').catch(err => err);
    expect(error.status).toBe(502);
    expect(error.reason).toBeUndefined();
  });
});
