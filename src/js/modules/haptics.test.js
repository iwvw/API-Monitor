import { describe, expect, it, vi, beforeEach, afterEach } from 'vitest';

import { canUseHaptics, triggerHapticFeedback } from './haptics.js';

describe('canUseHaptics', () => {
  afterEach(() => {
    vi.unstubAllGlobals();
  });

  it('returns false without a window', () => {
    expect(canUseHaptics()).toBe(false);
  });

  it('returns false when window has no navigator', () => {
    vi.stubGlobal('window', {});
    expect(canUseHaptics()).toBe(false);
  });

  it('returns false when vibrate is missing', () => {
    vi.stubGlobal('window', { navigator: {} });
    expect(canUseHaptics()).toBe(false);
  });

  it('returns true when vibrate is a function', () => {
    vi.stubGlobal('window', { navigator: { vibrate: () => true } });
    expect(canUseHaptics()).toBe(true);
  });
});

describe('triggerHapticFeedback', () => {
  let vibrateMock;
  let anchor = 0;

  beforeEach(() => {
    anchor += 1000000;
    vi.useFakeTimers();
    vi.setSystemTime(100000000000 + anchor);
    vibrateMock = vi.fn(() => true);
    vi.stubGlobal('window', { navigator: { vibrate: vibrateMock } });
  });

  afterEach(() => {
    vi.useRealTimers();
    vi.unstubAllGlobals();
  });

  it('returns false when haptics are unavailable', () => {
    vi.unstubAllGlobals();
    expect(triggerHapticFeedback('selection')).toBe(false);
  });

  it('maps known types to their patterns', () => {
    const base = 100000000000 + anchor;
    expect(triggerHapticFeedback('success')).toBe(true);
    expect(vibrateMock).toHaveBeenLastCalledWith([22, 28, 18]);

    vi.setSystemTime(base + 1000);
    expect(triggerHapticFeedback('warning')).toBe(true);
    expect(vibrateMock).toHaveBeenLastCalledWith([28, 32, 28]);

    vi.setSystemTime(base + 2000);
    expect(triggerHapticFeedback('error')).toBe(true);
    expect(vibrateMock).toHaveBeenLastCalledWith([35, 36, 35]);

    vi.setSystemTime(base + 3000);
    expect(triggerHapticFeedback('selection')).toBe(true);
    expect(vibrateMock).toHaveBeenLastCalledWith(18);
  });

  it('falls back to selection for unknown types', () => {
    triggerHapticFeedback('buzz');
    expect(vibrateMock).toHaveBeenCalledWith(18);
  });

  it('uses the selection pattern by default', () => {
    triggerHapticFeedback();
    expect(vibrateMock).toHaveBeenCalledWith(18);
  });

  it('throttles calls within 60ms', () => {
    expect(triggerHapticFeedback('success')).toBe(true);
    expect(triggerHapticFeedback('success')).toBe(false);
    expect(vibrateMock).toHaveBeenCalledTimes(1);

    vi.advanceTimersByTime(59);
    expect(triggerHapticFeedback('success')).toBe(false);

    vi.advanceTimersByTime(1);
    expect(triggerHapticFeedback('success')).toBe(true);
    expect(vibrateMock).toHaveBeenCalledTimes(2);
  });
});