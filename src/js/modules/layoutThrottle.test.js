import { afterEach, describe, expect, it, vi } from 'vitest';
import { createLayoutThrottle } from './layoutThrottle.js';

describe('createLayoutThrottle', () => {
  afterEach(() => {
    vi.useRealTimers();
  });

  it('前缘立即执行,窗口内只保留最后一次参数,窗口末尾补一次尾帧', () => {
    vi.useFakeTimers();
    let time = 0;
    const calls = [];
    const throttle = createLayoutThrottle(value => calls.push(value), 100, () => time);

    throttle.schedule(1);
    expect(calls).toEqual([1]);

    time = 10;
    throttle.schedule(2);
    time = 20;
    throttle.schedule(3);
    expect(calls).toEqual([1]);

    vi.advanceTimersByTime(100);
    expect(calls).toEqual([1, 3]);
  });

  it('空闲超过一个窗口后立即执行', () => {
    vi.useFakeTimers();
    let time = 0;
    const calls = [];
    const throttle = createLayoutThrottle(value => calls.push(value), 100, () => time);

    throttle.schedule('a');
    time = 500;
    throttle.schedule('b');
    expect(calls).toEqual(['a', 'b']);
  });

  it('cancel 丢弃尚未执行的尾帧', () => {
    vi.useFakeTimers();
    let time = 0;
    const calls = [];
    const throttle = createLayoutThrottle(value => calls.push(value), 100, () => time);

    throttle.schedule(1);
    time = 10;
    throttle.schedule(2);
    throttle.cancel();
    vi.advanceTimersByTime(200);
    expect(calls).toEqual([1]);
  });
});
