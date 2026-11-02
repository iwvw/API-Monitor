import { describe, expect, it } from 'vitest';
import {
  STATUS_TONE,
  buildInstanceDetailGroups,
  formatBytes,
  formatPercent,
  formatResource,
  formatUptime,
  instanceStatus,
} from './aiagentInstanceStatus.js';

describe('instanceStatus — 托管实例', () => {
  it('运行中：优先展示托管状态与 PID', () => {
    const result = instanceStatus({
      lifecycle: { managed: true, running: true, pid: 1234, desiredRunning: true },
      // 即使探测结果矛盾，托管状态也应胜出
      status: { online: false, hostOnline: true },
    });
    expect(result.label).toBe('运行中');
    expect(result.tone).toBe(STATUS_TONE.online);
    expect(result.detail).toContain('1234');
  });

  it('期望运行但未运行：判为启动中并带 spinner', () => {
    const result = instanceStatus({
      lifecycle: { managed: true, running: false, desiredRunning: true, restarts: 0 },
    });
    expect(result.label).toBe('启动中');
    expect(result.tone).toBe(STATUS_TONE.starting);
    expect(result.spinner).toBe(true);
  });

  it('启动中且已重启过：详情体现重启次数', () => {
    const result = instanceStatus({
      lifecycle: { managed: true, running: false, desiredRunning: true, restarts: 3 },
    });
    expect(result.label).toBe('启动中');
    expect(result.detail).toContain('3');
  });

  it('期望停止且已停止：判为已停止，不带 spinner', () => {
    const result = instanceStatus({
      lifecycle: { managed: true, running: false, desiredRunning: false, restarts: 0 },
    });
    expect(result.label).toBe('已停止');
    expect(result.tone).toBe(STATUS_TONE.process_stopped);
    expect(result.spinner).toBeFalsy();
  });

  it('已停止不显示重启次数（那是崩溃相关的信息，此处会误导）', () => {
    const result = instanceStatus({
      lifecycle: { managed: true, running: false, desiredRunning: false, restarts: 2 },
    });
    expect(result.label).toBe('已停止');
    expect(result.detail).not.toContain('2');
  });
});

describe('instanceStatus — 崩溃终态', () => {
  it('crashed 优先于 managed，不被托管分支吞掉', () => {
    const result = instanceStatus({
      lifecycle: { crashed: true, managed: true, running: false, restarts: 5 },
    });
    expect(result.label).toBe('已崩溃');
    expect(result.tone).toBe(STATUS_TONE.crashed);
    expect(result.detail).toContain('5');
  });

  it('crashed 即使进程表面在运行也判为崩溃（终态优先）', () => {
    const result = instanceStatus({
      lifecycle: { crashed: true, managed: true, running: true, pid: 1 },
    });
    expect(result.label).toBe('已崩溃');
  });
});

describe('instanceStatus — 非托管实例', () => {
  it('探测在线：判为运行中', () => {
    const result = instanceStatus({ status: { online: true, hostOnline: true, pid: 99 } });
    expect(result.label).toBe('运行中');
    expect(result.detail).toContain('99');
  });

  it('主机离线优先于进程判定', () => {
    const result = instanceStatus({
      status: { online: false, hostOnline: false, error: 'host agent offline' },
    });
    expect(result.label).toBe('主机离线');
    expect(result.tone).toBe(STATUS_TONE.host_offline);
  });

  it('端口被其它进程占用：给出占用者 PID', () => {
    const result = instanceStatus({
      status: {
        online: false,
        hostOnline: true,
        portListening: true,
        listenerMatchesProcess: false,
        listenerPid: 4321,
      },
    });
    expect(result.label).toBe('端口被占用');
    expect(result.detail).toContain('4321');
  });

  it('进程在跑但未监听端口', () => {
    const result = instanceStatus({
      status: { online: false, hostOnline: true, processRunning: true, portListening: false },
    });
    expect(result.label).toBe('未监听端口');
  });

  it('进程未运行', () => {
    const result = instanceStatus({
      status: { online: false, hostOnline: true, processRunning: false, portListening: false },
    });
    expect(result.label).toBe('进程未运行');
  });

  it('信息不足时判为状态未知，不伪造结论', () => {
    const result = instanceStatus({ status: { hostOnline: true } });
    expect(result.label).toBe('状态未知');
  });

  it('缺少 status/lifecycle 时不抛错', () => {
    expect(() => instanceStatus({})).not.toThrow();
    expect(() => instanceStatus(undefined)).not.toThrow();
    expect(instanceStatus(undefined).label).toBe('状态未知');
  });
});

describe('formatBytes', () => {
  it('0 与缺失显示占位符，而不是 0 B', () => {
    expect(formatBytes(0)).toBe('—');
    expect(formatBytes(undefined)).toBe('—');
    expect(formatBytes(null)).toBe('—');
    expect(formatBytes(-1)).toBe('—');
  });

  it('MB 量级保留一位或整数', () => {
    expect(formatBytes(5 * 1024 * 1024)).toBe('5.0 MB');
    expect(formatBytes(200 * 1024 * 1024)).toBe('200 MB');
  });

  it('GB 量级保留两位', () => {
    expect(formatBytes(2.5 * 1024 * 1024 * 1024)).toBe('2.50 GB');
  });
});

describe('formatPercent', () => {
  it('0 是合法值（空闲），不应显示占位符', () => {
    expect(formatPercent(0)).toBe('0.0%');
  });

  it('缺失显示占位符', () => {
    expect(formatPercent(undefined)).toBe('—');
    expect(formatPercent(null)).toBe('—');
  });

  it('保留一位小数', () => {
    expect(formatPercent(12.345)).toBe('12.3%');
  });
});

describe('formatResource', () => {
  it('托管且运行时取 lifecycle 的值', () => {
    const instance = {
      lifecycle: { managed: true, running: true, memoryBytes: 100 * 1024 * 1024 },
      status: { memoryBytes: 999 * 1024 * 1024 },
    };
    expect(formatResource(instance, 'memory')).toBe('100 MB');
  });

  it('托管但未运行时回退到探测结果', () => {
    const instance = {
      lifecycle: { managed: true, running: false, memoryBytes: 100 * 1024 * 1024 },
      status: { memoryBytes: 50 * 1024 * 1024 },
    };
    expect(formatResource(instance, 'memory')).toBe('50 MB');
  });

  it('非托管实例用探测结果', () => {
    const instance = { status: { memoryBytes: 256 * 1024 * 1024, cpuPercent: 3.5 } };
    expect(formatResource(instance, 'memory')).toBe('256 MB');
    expect(formatResource(instance, 'cpu')).toBe('3.5%');
  });

  it('两处都缺失时显示占位符', () => {
    expect(formatResource({}, 'memory')).toBe('—');
    expect(formatResource({}, 'cpu')).toBe('—');
  });
});

describe('formatUptime', () => {
  it('0 与缺失显示占位符', () => {
    expect(formatUptime(0)).toBe('—');
    expect(formatUptime(undefined)).toBe('—');
    expect(formatUptime(-5)).toBe('—');
  });

  it('秒级', () => {
    expect(formatUptime(45)).toBe('45 秒');
  });

  it('分钟级含秒', () => {
    expect(formatUptime(125)).toBe('2 分 5 秒');
  });

  it('小时级含分', () => {
    expect(formatUptime(3 * 3600 + 20 * 60)).toBe('3 小时 20 分');
  });

  it('天级含小时', () => {
    expect(formatUptime(2 * 86400 + 5 * 3600)).toBe('2 天 5 小时');
  });
});

describe('buildInstanceDetailGroups', () => {
  it('实例为空时返回空数组，不抛错', () => {
    expect(buildInstanceDetailGroups(null)).toEqual([]);
    expect(buildInstanceDetailGroups(undefined)).toEqual([]);
  });

  it('始终包含基本信息、探测结果与接入信息', () => {
    const groups = buildInstanceDetailGroups({ id: 'i1', label: 'x' });
    const titles = groups.map(g => g.title);
    expect(titles).toContain('基本信息');
    expect(titles).toContain('探测结果');
    expect(titles).toContain('接入信息');
  });

  it('未托管实例不展示托管分组（避免一堆无意义的「未知」）', () => {
    const groups = buildInstanceDetailGroups({
      id: 'i1',
      lifecycle: { managed: false, supported: false },
    });
    expect(groups.map(g => g.title)).not.toContain('托管进程');
  });

  it('托管实例展示托管分组，含期望状态与重启次数', () => {
    const groups = buildInstanceDetailGroups({
      id: 'i1',
      desiredState: 'running',
      lifecycle: {
        managed: true,
        supported: true,
        running: true,
        pid: 1234,
        uptimeSeconds: 3600,
        restarts: 2,
      },
    });
    const group = groups.find(g => g.title === '托管进程');
    expect(group).toBeTruthy();
    const rows = Object.fromEntries(group.rows.map(r => [r.label, r.value]));
    expect(rows['期望状态']).toBe('运行中');
    expect(rows['进程 PID']).toBe('1234');
    expect(rows['自动重启']).toBe('2 次');
    expect(rows['已崩溃']).toBe('否');
  });

  it('期望状态为空时标注「不托管」，与存量实例语义一致', () => {
    const groups = buildInstanceDetailGroups({
      id: 'i1',
      lifecycle: { managed: true, supported: true },
    });
    const group = groups.find(g => g.title === '托管进程');
    const rows = Object.fromEntries(group.rows.map(r => [r.label, r.value]));
    expect(rows['期望状态']).toBe('不托管（仅探测）');
  });

  it('crashed 时明确提示需手动启动', () => {
    const groups = buildInstanceDetailGroups({
      id: 'i1',
      lifecycle: { managed: true, supported: true, crashed: true, restarts: 5 },
    });
    const group = groups.find(g => g.title === '托管进程');
    const rows = Object.fromEntries(group.rows.map(r => [r.label, r.value]));
    expect(rows['已崩溃']).toBe('是（需手动启动）');
  });

  it('未知字段标注「未知」而不是伪造成「否」', () => {
    const groups = buildInstanceDetailGroups({ id: 'i1', status: {} });
    const group = groups.find(g => g.title === '探测结果');
    const rows = Object.fromEntries(group.rows.map(r => [r.label, r.value]));
    expect(rows['主机在线']).toBe('未知');
    expect(rows['进程运行']).toBe('未知');
  });

  it('端口被占用的探测结果能在详情里看到监听 PID', () => {
    const groups = buildInstanceDetailGroups({
      id: 'i1',
      status: {
        hostOnline: true,
        portListening: true,
        listenerMatchesProcess: false,
        listenerPid: 4321,
      },
    });
    const group = groups.find(g => g.title === '探测结果');
    const rows = Object.fromEntries(group.rows.map(r => [r.label, r.value]));
    expect(rows['监听端口的 PID']).toBe('4321');
    expect(rows['监听者命中规则']).toBe('否');
  });

  it('托管运行时资源取托管值（与列表列保持一致）', () => {
    const groups = buildInstanceDetailGroups({
      id: 'i1',
      lifecycle: { managed: true, running: true, memoryBytes: 512 * 1024 * 1024 },
      status: { memoryBytes: 999 * 1024 * 1024 },
    });
    const group = groups.find(g => g.title === '托管进程');
    const rows = Object.fromEntries(group.rows.map(r => [r.label, r.value]));
    expect(rows['内存']).toBe('512 MB');
  });
});
