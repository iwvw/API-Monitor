import { describe, expect, it } from 'vitest';
import { typewriterFrame } from './typewriter.js';

describe('typewriterFrame', () => {
  it('整体替换时要求重置揭示游标', () => {
    expect(typewriterFrame('hello', 'new message')).toEqual({ reset: true });
    expect(typewriterFrame('hello world', 'a')).toEqual({ reset: true });
  });

  it('增量追加时继续揭示', () => {
    expect(typewriterFrame('hello', 'hello world')).toEqual({ extend: true });
    expect(typewriterFrame('ab', 'abc')).toEqual({ extend: true });
  });

  it('文本未变化时保持现状', () => {
    expect(typewriterFrame('same', 'same')).toEqual({});
  });

  it('空首 chunk 到首段内容仍是增量追加', () => {
    expect(typewriterFrame('', 'first chunk')).toEqual({ extend: true });
  });
});
