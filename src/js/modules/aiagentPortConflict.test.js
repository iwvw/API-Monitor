import { describe, expect, it } from 'vitest';
import { isPortOccupied, resolvePortConflict } from './aiagentPortConflict.js';

describe('isPortOccupied', () => {
  it('诊断缺失时不误判占用', () => {
    expect(isPortOccupied(null, '', 4096)).toBe(false);
    expect(isPortOccupied(undefined, '4096', 4096)).toBe(false);
    expect(isPortOccupied({}, '', 4096)).toBe(false);
  });

  it('未填端口时用默认端口判断', () => {
    const diagnose = { usedPorts: [4096], suggestedPort: 4097 };
    expect(isPortOccupied(diagnose, '', 4096)).toBe(true);
    expect(isPortOccupied(diagnose, '   ', 4096)).toBe(true);
  });

  it('已填端口时按填写值判断', () => {
    const diagnose = { usedPorts: [4096, 4099], suggestedPort: 4097 };
    expect(isPortOccupied(diagnose, '4099', 4096)).toBe(true);
    expect(isPortOccupied(diagnose, '4097', 4096)).toBe(false);
    expect(isPortOccupied(diagnose, '4100', 4096)).toBe(false);
  });

  it('usedPorts 为空或端口非法时返回 false', () => {
    expect(isPortOccupied({ usedPorts: [], suggestedPort: 4096 }, '', 4096)).toBe(false);
    expect(isPortOccupied({ usedPorts: [4096], suggestedPort: 4097 }, 'abc', 4096)).toBe(false);
    expect(isPortOccupied({ usedPorts: [4096], suggestedPort: 4097 }, '0', 4096)).toBe(false);
  });
});

describe('resolvePortConflict', () => {
  it('无冲突/无诊断返回 null', () => {
    expect(resolvePortConflict(null, '', 4096)).toBe(null);
    expect(resolvePortConflict({ usedPorts: [], suggestedPort: 4096 }, '', 4096)).toBe(null);
    // 未填端口但默认端口空闲
    expect(
      resolvePortConflict({ usedPorts: [4097], suggestedPort: 4096 }, '', 4096)
    ).toBe(null);
  });

  it('默认端口被占时返回默认端口与建议端口', () => {
    const result = resolvePortConflict(
      { usedPorts: [4096], suggestedPort: 4097 },
      '',
      4096
    );
    expect(result).toEqual({ occupied: 4096, suggested: 4097 });
  });

  it('填写端口被占时返回填写值与建议端口', () => {
    const result = resolvePortConflict(
      { usedPorts: [4100, 4101], suggestedPort: 4097 },
      '4101',
      4096
    );
    expect(result).toEqual({ occupied: 4101, suggested: 4097 });
  });

  it('区间全占（无建议端口）时不提示切换', () => {
    expect(
      resolvePortConflict({ usedPorts: [4096, 4097, 4098], suggestedPort: 0 }, '', 4096)
    ).toBe(null);
  });
});