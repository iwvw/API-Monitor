import { describe, expect, it } from 'vitest';
import { isPortOccupied, resolvePortOccupancy } from './aiagentPortConflict.js';

describe('isPortOccupied', () => {
  it('诊断缺失时不误判占用', () => {
    expect(isPortOccupied(null, '', 4096)).toBe(false);
    expect(isPortOccupied(undefined, '4096', 4096)).toBe(false);
    expect(isPortOccupied({}, '', 4096)).toBe(false);
  });

  it('未填端口时用默认端口判断', () => {
    const diagnose = { usedPorts: [4096] };
    expect(isPortOccupied(diagnose, '', 4096)).toBe(true);
    expect(isPortOccupied(diagnose, '   ', 4096)).toBe(true);
  });

  it('已填端口时按填写值判断', () => {
    const diagnose = { usedPorts: [4096, 4099] };
    expect(isPortOccupied(diagnose, '4099', 4096)).toBe(true);
    expect(isPortOccupied(diagnose, '4097', 4096)).toBe(false);
    expect(isPortOccupied(diagnose, '4100', 4096)).toBe(false);
  });

  it('usedPorts 为空或端口非法时返回 false', () => {
    expect(isPortOccupied({ usedPorts: [] }, '', 4096)).toBe(false);
    expect(isPortOccupied({ usedPorts: [4096] }, 'abc', 4096)).toBe(false);
    expect(isPortOccupied({ usedPorts: [4096] }, '0', 4096)).toBe(false);
  });
});

describe('resolvePortOccupancy', () => {
  it('无占用/无诊断返回 null', () => {
    expect(resolvePortOccupancy(null, '', 4096)).toBe(null);
    expect(resolvePortOccupancy({ usedPorts: [] }, '', 4096)).toBe(null);
    expect(resolvePortOccupancy({ sameProviderPorts: [], foreignOccupiedPorts: [] }, '', 4096)).toBe(null);
  });

  it('同类占用标记为 same-provider（start 会自动清理，不提示换端口）', () => {
    const diagnose = {
      usedPorts: [4096],
      sameProviderPorts: [4096],
      foreignOccupiedPorts: [],
    };
    expect(resolvePortOccupancy(diagnose, '', 4096)).toEqual({ port: 4096, kind: 'same-provider' });
  });

  it('无关进程占用标记为 foreign（需要用户处理）', () => {
    const diagnose = {
      usedPorts: [4100],
      sameProviderPorts: [],
      foreignOccupiedPorts: [4100],
    };
    expect(resolvePortOccupancy(diagnose, '4100', 4096)).toEqual({ port: 4100, kind: 'foreign' });
  });

  it('旧版 Agent 无分类字段时按无关占用兜底', () => {
    const diagnose = { usedPorts: [4096], suggestedPort: 4097 };
    expect(resolvePortOccupancy(diagnose, '', 4096)).toEqual({ port: 4096, kind: 'foreign' });
  });

  it('未占用的端口返回 null', () => {
    const diagnose = {
      usedPorts: [4096],
      sameProviderPorts: [4096],
      foreignOccupiedPorts: [],
    };
    expect(resolvePortOccupancy(diagnose, '4097', 4096)).toBe(null);
  });

  it('端口非法时返回 null', () => {
    const diagnose = { usedPorts: [4096], sameProviderPorts: [4096], foreignOccupiedPorts: [] };
    expect(resolvePortOccupancy(diagnose, 'abc', 4096)).toBe(null);
    expect(resolvePortOccupancy(diagnose, '0', 4096)).toBe(null);
  });
});
