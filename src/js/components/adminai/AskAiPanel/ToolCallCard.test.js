import { describe, it, expect } from 'vitest';
import { callArgsKey } from './ToolCallCard.jsx';

describe('callArgsKey — 调用关键参数指纹', () => {
  it('从 body 提取 hostId/action 等标识字段', () => {
    expect(callArgsKey({ method: 'POST', path: 'agent/exec', body: { hostId: 'host-a', action: 'install' } })).toBe('host-a');
    expect(callArgsKey({ method: 'POST', path: 'server/action', body: { action: 'stop' } })).toBe('stop');
  });

  it('顶层字段优先于 body', () => {
    expect(callArgsKey({ path: 'x', id: 'top-1', body: { id: 'body-2' } })).toBe('top-1');
  });

  it('query 字段兜底', () => {
    expect(callArgsKey({ path: 'x', query: { taskId: 't9' } })).toBe('t9');
  });

  it('args 为字符串 JSON 时兼容解析', () => {
    expect(callArgsKey('{"method":"POST","path":"x","body":{"zoneId":"z1"}}')).toBe('z1');
  });

  it('无标识字段返回空串（不参与去重区分）', () => {
    expect(callArgsKey({ method: 'GET', path: 'hosts' })).toBe('');
    expect(callArgsKey(undefined)).toBe('');
    expect(callArgsKey('not-json{')).toBe('');
  });
});