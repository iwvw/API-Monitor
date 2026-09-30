import { describe, expect, it } from 'vitest';
import {
  accelerateTrackpadDelta,
  consumeScrollDelta,
  initialRemoteDesktopProfile,
  isDoubleTap,
  nextPinchTransform,
  nextRemoteDesktopProfile,
  normalizedVideoPoint,
  normalizedTrackpadDelta,
  remoteCursorPoint,
  trackpadButtonMessage,
  trackpadPixelDelta,
} from './remoteDesktopTouch.js';

describe('remote desktop touch controls', () => {
  it('keeps slow movement precise and accelerates fast swipes', () => {
    const precise = accelerateTrackpadDelta(2, 0, 16);
    const fast = accelerateTrackpadDelta(24, 0, 16);
    expect(precise.x).toBeCloseTo(2, 3);
    expect(fast.x).toBeGreaterThan(40);
  });

  it('keeps trackpad sensitivity independent of the phone viewport width', () => {
    const phone = normalizedTrackpadDelta(
      12,
      6,
      16,
      { width: 360, height: 720 },
      { width: 1920, height: 1080 }
    );
    const tablet = normalizedTrackpadDelta(
      12,
      6,
      16,
      { width: 1024, height: 768 },
      { width: 1920, height: 1080 }
    );
    expect(phone).toEqual(tablet);
    expect(phone.x * 1920).toBeGreaterThan(12);
  });

  it('sends trackpad buttons without absolute coordinates', () => {
    expect(trackpadButtonMessage('click')).toEqual({
      type: 'mouse',
      action: 'click',
      button: 0,
    });
    expect(trackpadButtonMessage('click', 2)).toEqual({
      type: 'mouse',
      action: 'click',
      button: 2,
    });
  });

  it('sends integer trackpad deltas without remote geometry', () => {
    expect(trackpadPixelDelta(2, -3, 16)).toEqual({ x: 2, y: -3 });
    expect(trackpadPixelDelta(24, 0, 16).x).toBeGreaterThan(24);
  });

  it('starts coarse-pointer clients in a reaction-first mobile profile', () => {
    expect(initialRemoteDesktopProfile(true)).toEqual({ fps: 30, bitrate: 6_000_000 });
    expect(initialRemoteDesktopProfile(false)).toEqual({ fps: 60, bitrate: 12_000_000 });
    // 高分屏桌面端按原生码率启动，不再被固定压到 12 Mbps。
    expect(initialRemoteDesktopProfile(false, 18_000_000)).toEqual({ fps: 60, bitrate: 18_000_000 });
    // 未知分辨率（nativeBitrate 为 0）时回落到默认 12 Mbps。
    expect(initialRemoteDesktopProfile(false, 0)).toEqual({ fps: 60, bitrate: 12_000_000 });
    // 粗指针移动端始终以低码率优先，不受高分屏 nativeBitrate 影响。
    expect(initialRemoteDesktopProfile(true, 28_000_000)).toEqual({ fps: 30, bitrate: 6_000_000 });
  });

  it('reduces frame cadence when the decoder jitter buffer grows', () => {
    const next = nextRemoteDesktopProfile({
      bufferMs: 90,
      nativeBitrate: 12_000_000,
      current: { fps: 60, bitrate: 12_000_000 },
    });
    expect(next.profile.fps).toBe(30);
    expect(next.profile.bitrate).toBeLessThanOrEqual(8_000_000);
    expect(next.profile.bitrate).toBeGreaterThanOrEqual(6_000_000);
    expect(next.healthyIntervals).toBe(0);
  });

  it('deeply degraded links fall back to the 6 Mbps reaction floor', () => {
    const next = nextRemoteDesktopProfile({
      loss: 8,
      nativeBitrate: 12_000_000,
      current: { fps: 60, bitrate: 12_000_000 },
    });
    expect(next.profile.fps).toBe(30);
    expect(next.profile.bitrate).toBe(6_000_000);
    expect(next.healthyIntervals).toBe(0);
  });

  it('restores the native bitrate ceiling after sustained health', () => {
    let healthyIntervals = 0;
    let profile = { fps: 30, bitrate: 6_000_000 };
    for (let i = 0; i < 3; i += 1) {
      const next = nextRemoteDesktopProfile({
        nativeBitrate: 28_000_000,
        current: profile,
        healthyIntervals,
      });
      profile = next.profile;
      healthyIntervals = next.healthyIntervals;
    }
    // 恢复后应回到原生上限，而不是被固定在 12 Mbps。
    expect(profile).toEqual({ fps: 60, bitrate: 28_000_000 });
  });

  it('retains sub-threshold two-finger scroll movement', () => {
    const first = consumeScrollDelta({ x: 0, y: 0 }, 0, 11, 20);
    expect(first.stepsY).toBe(0);
    expect(first.remainder.y).toBe(11);
    const second = consumeScrollDelta(first.remainder, 0, 11, 20);
    expect(second.stepsY).toBe(1);
    expect(second.remainder.y).toBe(2);
  });

  it('requires double taps to be close in time and space', () => {
    const previous = { at: 1_000, x: 20, y: 30 };
    expect(isDoubleTap(previous, { at: 1_250, x: 30, y: 36 })).toBe(true);
    expect(isDoubleTap(previous, { at: 1_500, x: 30, y: 36 })).toBe(false);
    expect(isDoubleTap(previous, { at: 1_200, x: 90, y: 90 })).toBe(false);
  });

  it('keeps the pinch focal point stable while zooming', () => {
    const next = nextPinchTransform(
      { scale: 1, x: 0, y: 0 },
      { x: 100, y: 200 },
      { x: 100, y: 200 },
      100,
      200,
      { width: 400, height: 800 }
    );
    expect(next).toEqual({ scale: 2, x: -100, y: -200 });
  });

  it('maps the remote cursor inside contain letterboxing and local zoom', () => {
    const point = remoteCursorPoint(
      { x: 0.5, y: 0.5 },
      { width: 400, height: 800 },
      { width: 1920, height: 1080 },
      'contain',
      { scale: 2, x: -200, y: -400 }
    );
    expect(point.x).toBeCloseTo(200);
    expect(point.y).toBeCloseTo(400);
  });

  it('maps direct touch through contain letterboxing and ignores black bars', () => {
    const surface = { left: 10, top: 20, width: 400, height: 800 };
    const video = { width: 1920, height: 1080 };
    expect(normalizedVideoPoint({ x: 210, y: 420 }, surface, video, 'contain')).toEqual({
      x: 0.5,
      y: 0.5,
    });
    expect(normalizedVideoPoint({ x: 210, y: 100 }, surface, video, 'contain')).toBeNull();
    expect(
      normalizedVideoPoint({ x: 210, y: 100 }, surface, video, 'contain', undefined, true)
    ).toEqual({ x: 0.5, y: 0 });
  });

  it('maps direct touch to the visible crop in cover mode', () => {
    const point = normalizedVideoPoint(
      { x: 0, y: 400 },
      { left: 0, top: 0, width: 400, height: 800 },
      { width: 1920, height: 1080 },
      'cover'
    );
    expect(point.x).toBeCloseTo(0.359375, 5);
    expect(point.y).toBeCloseTo(0.5, 5);
  });

  it('inverts local pan and zoom before mapping direct touch', () => {
    const point = normalizedVideoPoint(
      { x: 200, y: 400 },
      { left: 0, top: 0, width: 400, height: 800 },
      { width: 1920, height: 1080 },
      'contain',
      { scale: 2, x: -200, y: -400 }
    );
    expect(point).toEqual({ x: 0.5, y: 0.5 });
  });
});
