import { describe, expect, it } from 'vitest';
import { needsAttention, sortInstances } from './aiagentInstanceList.js';

// 构造辅助：只写关心的字段，避免每个用例重复样板。
function running(label) {
  return {
    id: label,
    label,
    enabled: true,
    lifecycle: { managed: true, supported: true, running: true, desiredRunning: true },
  };
}
function stopped(label) {
  return {
    id: label,
    label,
    enabled: true,
    lifecycle: { managed: true, supported: true, running: false, desiredRunning: false },
  };
}
function crashed(label) {
  return {
    id: label,
    label,
    enabled: true,
    lifecycle: { managed: true, supported: true, crashed: true, restarts: 5 },
  };
}
function unmanaged(label) {
  return { id: label, label, enabled: true, status: { online: true, hostOnline: true } };
}
function disabled(label) {
  return { id: label, label, enabled: false, status: {} };
}

describe('needsAttention', () => {
  it('崩溃需要关注', () => {
    expect(needsAttention(crashed('a'))).toBe(true);
  });

  it('已停用需要关注', () => {
    expect(needsAttention(disabled('a'))).toBe(true);
  });

  it('正常运行不需要关注', () => {
    expect(needsAttention(running('a'))).toBe(false);
  });

  it('用户主动停止不算异常（那是预期结果）', () => {
    expect(needsAttention(stopped('a'))).toBe(false);
  });

  it('未托管但在线不算异常', () => {
    expect(needsAttention(unmanaged('a'))).toBe(false);
  });

  it('启动中的过渡态不算异常（会自动收敛）', () => {
    const starting = {
      id: 'a',
      label: 'a',
      enabled: true,
      lifecycle: { managed: true, running: false, desiredRunning: true },
    };
    expect(needsAttention(starting)).toBe(false);
  });

  it('端口被其它进程占用算异常', () => {
    const instance = {
      id: 'a',
      label: 'a',
      enabled: true,
      status: { hostOnline: true, portListening: true, listenerMatchesProcess: false },
    };
    expect(needsAttention(instance)).toBe(true);
  });

  it('进程在跑但未监听端口算异常', () => {
    const instance = {
      id: 'a',
      label: 'a',
      enabled: true,
      status: { hostOnline: true, processRunning: true, portListening: false },
    };
    expect(needsAttention(instance)).toBe(true);
  });

  it('主机离线算异常', () => {
    const instance = { id: 'a', label: 'a', enabled: true, status: { hostOnline: false } };
    expect(needsAttention(instance)).toBe(true);
  });

  it('状态未知算异常（信息不足时不该当作正常）', () => {
    const instance = { id: 'a', label: 'a', enabled: true, status: {} };
    expect(needsAttention(instance)).toBe(true);
  });

  it('空值不抛错', () => {
    expect(needsAttention(null)).toBe(false);
    expect(needsAttention(undefined)).toBe(false);
  });
});

describe('sortInstances', () => {
  it('异常排在最前', () => {
    const list = [running('正常A'), crashed('崩溃B'), unmanaged('未托管C')];
    const result = sortInstances(list);
    expect(result[0].label).toBe('崩溃B');
  });

  it('异常内部按名称排序，顺序稳定', () => {
    const list = [crashed('乙'), crashed('甲'), running('丙')];
    const result = sortInstances(list);
    expect(result[0].label).toBe('甲');
    expect(result[1].label).toBe('乙');
  });

  it('全部正常时按名称排序（不依赖数组原始顺序）', () => {
    const list = [running('c'), running('a'), running('b')];
    const result = sortInstances(list);
    expect(result.map(i => i.label)).toEqual(['a', 'b', 'c']);
  });

  it('已停用排在正常之前（停用是需要关注的状态）', () => {
    const list = [running('a'), disabled('b')];
    const result = sortInstances(list);
    expect(result[0].label).toBe('b');
  });

  it('不修改入参数组', () => {
    const list = [running('b'), running('a')];
    const snapshot = list.map(i => i.label);
    sortInstances(list);
    expect(list.map(i => i.label)).toEqual(snapshot);
  });

  it('非数组输入返回空数组，不抛错', () => {
    expect(sortInstances(null)).toEqual([]);
    expect(sortInstances(undefined)).toEqual([]);
    expect(sortInstances('nope')).toEqual([]);
  });

  it('空列表返回空列表', () => {
    expect(sortInstances([])).toEqual([]);
  });

  it('标签缺失时不抛错，且因状态未知排在前（未知视为需关注）', () => {
    const list = [running('b'), { id: 'x', enabled: true, lifecycle: {} }];
    expect(() => sortInstances(list)).not.toThrow();
    // 空 lifecycle 使状态判为「状态未知」，未知不自作主张当作正常，
    // 因此它排在正常运行的实例之前。
    expect(sortInstances(list)[0].label).toBeUndefined();
    expect(sortInstances(list)[1].label).toBe('b');
  });
});
