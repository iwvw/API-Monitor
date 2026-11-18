import { describe, expect, it } from 'vitest';
import { formatCompact, formatTokensAxis, resolveAxisTicks } from './utils.js';

describe('resolveAxisTicks', () => {
  it('词元轴刻度落在 1/2/5×10^k 上,标签互不相同', () => {
    const ticks = resolveAxisTicks([1200000, 275000000, 0], formatTokensAxis);
    expect(ticks).toEqual({ min: 0, max: 300000000, interval: 50000000 });

    const labels = [];
    for (let value = ticks.min; value <= ticks.max; value += ticks.interval) {
      labels.push(formatTokensAxis(value));
    }
    expect(labels).toEqual(['0', '5千万', '1亿', '1.5亿', '2亿', '2.5亿', '3亿']);
    expect(new Set(labels).size).toBe(labels.length);
  });

  it('万/亿量级的计数轴也不会出现重复标签', () => {
    const ticks = resolveAxisTicks([18000], value => formatCompact(value, 0));
    expect(ticks).toEqual({ min: 0, max: 20000, interval: 10000 });

    const labels = [];
    for (let value = ticks.min; value <= ticks.max; value += ticks.interval) {
      labels.push(formatCompact(value, 0));
    }
    expect(labels).toEqual(['0', '1万', '2万']);
  });

  it('每个候选间隔都会撞标签时返回 null,交回图表库默认分割', () => {
    expect(resolveAxisTicks([10], () => '同')).toBeNull();
  });

  it('空数据、全零、含负值或缺少格式化函数时不接管刻度', () => {
    expect(resolveAxisTicks([], formatTokensAxis)).toBeNull();
    expect(resolveAxisTicks([0, 0], formatTokensAxis)).toBeNull();
    expect(resolveAxisTicks([-5, 3], formatTokensAxis)).toBeNull();
    expect(resolveAxisTicks([1], null)).toBeNull();
    expect(resolveAxisTicks(null, formatTokensAxis)).toBeNull();
  });
});

describe('formatTokensAxis', () => {
  it('整单位不带小数,非整单位保留 1 位', () => {
    expect(formatTokensAxis(0)).toBe('0');
    expect(formatTokensAxis(999)).toBe('999');
    expect(formatTokensAxis(12000)).toBe('1.2万');
    expect(formatTokensAxis(50000000)).toBe('5千万');
    expect(formatTokensAxis(100000000)).toBe('1亿');
    expect(formatTokensAxis(150000000)).toBe('1.5亿');
    expect(formatTokensAxis(12000000)).toBe('1.2千万');
  });
});
