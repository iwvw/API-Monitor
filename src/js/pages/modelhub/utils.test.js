import { describe, expect, it } from 'vitest';
import { formatCompact, formatTokensAxis, resolveAxisTicks } from './utils.js';

describe('resolveAxisTicks', () => {
  it('把词元轴的刻度对齐到亿,避免相邻刻度渲染成同一个标签', () => {
    const ticks = resolveAxisTicks([1200000, 275000000, 0], formatTokensAxis);
    expect(ticks).toEqual({ min: 0, max: 300000000, interval: 100000000 });

    const labels = [];
    for (let value = ticks.min; value <= ticks.max; value += ticks.interval) {
      labels.push(formatTokensAxis(value));
    }
    expect(labels).toEqual(['0', '1亿', '2亿', '3亿']);
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
