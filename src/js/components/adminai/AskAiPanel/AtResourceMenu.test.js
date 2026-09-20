import { describe, it, expect } from 'vitest';
import { fuzzyMatch } from './AtResourceMenu.jsx';

describe('fuzzyMatch — 子序列模糊匹配', () => {
  it('空查询命中一切且无高亮', () => {
    const r = fuzzyMatch('', 'example.com');
    expect(r).not.toBeNull();
    expect(r.ranges).toEqual([]);
  });

  it('按顺序出现的字符即命中，并给出高亮区间', () => {
    const r = fuzzyMatch('exm', 'example.com');
    expect(r).not.toBeNull();
    expect(r.ranges.length).toBeGreaterThan(0);
    // 高亮区间拼出的字符等于查询串
    const hit = r.ranges.map(([a, b]) => 'example.com'.slice(a, b)).join('');
    expect(hit).toBe('exm');
  });

  it('字符顺序不符则不命中', () => {
    expect(fuzzyMatch('mxe', 'example.com')).toBeNull();
    expect(fuzzyMatch('zzz', 'example.com')).toBeNull();
  });

  it('前缀命中得分高于散落命中', () => {
    const prefix = fuzzyMatch('ex', 'example.com');
    const scattered = fuzzyMatch('ex', 'some-extra');
    expect(prefix.score).toBeGreaterThan(scattered.score);
  });

  it('连续命中得分高于间断命中', () => {
    const contiguous = fuzzyMatch('abc', 'abcxxxxx');
    const gapped = fuzzyMatch('abc', 'axbxcxxx');
    expect(contiguous.score).toBeGreaterThan(gapped.score);
  });

  it('大小写不敏感', () => {
    expect(fuzzyMatch('EX', 'example')).not.toBeNull();
  });
});
