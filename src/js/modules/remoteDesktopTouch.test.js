import { describe, expect, it } from 'vitest';
import {
  DESKTOP_QUALITY_PRESETS,
  DEFAULT_DESKTOP_FPS,
  DEFAULT_DESKTOP_PRESET,
  accelerateTrackpadDelta,
  consumeScrollDelta,
  desktopPresetById,
  isDoubleTap,
  nextPinchTransform,
  nextRemoteDesktopProfile,
  normalizedDesktopFps,
  normalizedVideoPoint,
  normalizedTrackpadDelta,
  remoteCursorPoint,
  remoteDesktopProfileForPreset,
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

  it('exposes a quality preset ladder with resolution and bitrate', () => {
    expect(DESKTOP_QUALITY_PRESETS.map(preset => preset.id)).toEqual(['smooth', 'balanced', 'sharp', 'ultra']);
    expect(desktopPresetById('unknown').id).toBe(DEFAULT_DESKTOP_PRESET);
    // 画质档只描述分辨率 + 码率；帧率默认 30，由独立档位控制。
    const sharpDesktop = remoteDesktopProfileForPreset('sharp', false);
    expect(sharpDesktop).toEqual({ fps: 30, bitrate: 24_000_000, maxLongEdge: 2560 });
    // 超清档对应编码侧允许的最大长边（agent MAX_MAX_LONG_EDGE = 3840）。
    expect(remoteDesktopProfileForPreset('ultra', false)).toEqual({
      fps: 30,
      bitrate: 32_000_000,
      maxLongEdge: 3840,
    });
    // 粗指针移动端压低帧率与码率，但仍保留所选分辨率档。
    expect(remoteDesktopProfileForPreset('sharp', true)).toEqual({
      fps: 30,
      bitrate: 6_000_000,
      maxLongEdge: 2560,
    });
  });

  it('treats fps as an independent axis capped at the agent limit of 60', () => {
    // 60 是 Agent 侧 video_config 的硬上限（fps.clamp(30, TARGET_FPS)）。
    expect(remoteDesktopProfileForPreset('balanced', false, 60)).toEqual({
      fps: 60,
      bitrate: 12_000_000,
      maxLongEdge: 1920,
    });
    // 非法/未知帧率一律回落到 30，避免把 undefined 透传给编码器。
    expect(remoteDesktopProfileForPreset('balanced', false, 144).fps).toBe(30);
    expect(remoteDesktopProfileForPreset('balanced', false, undefined).fps).toBe(30);
    expect(normalizedDesktopFps(60)).toBe(60);
    // Kumo Select 以字符串回传 value，因此 '60' 必须被接受（Number 归一化）。
    expect(normalizedDesktopFps('60')).toBe(60);
    expect(normalizedDesktopFps('30')).toBe(30);
    // 非 60 的任意值（含 144 / 0 / NaN）一律回落到 30。
    expect(normalizedDesktopFps(144)).toBe(30);
    expect(normalizedDesktopFps(0)).toBe(30);
    expect(normalizedDesktopFps('abc')).toBe(30);
    // 粗指针（移动端）仍封顶 30，即使请求了 60。
    expect(remoteDesktopProfileForPreset('balanced', true, 60).fps).toBe(30);
    expect(DEFAULT_DESKTOP_FPS).toBe(30);
  });

  it('reduces bitrate and resolution when the link degrades', () => {
    const base = remoteDesktopProfileForPreset('sharp', false);
    const moderate = nextRemoteDesktopProfile({
      bufferMs: 90,
      nativeBitrate: 12_000_000,
      current: base,
      base,
    });
    expect(moderate.profile.bitrate).toBeLessThanOrEqual(8_000_000);
    expect(moderate.profile.bitrate).toBeGreaterThanOrEqual(6_000_000);
    expect(moderate.profile.maxLongEdge).toBe(1920);
    expect(moderate.healthyIntervals).toBe(0);

    const severe = nextRemoteDesktopProfile({
      loss: 8,
      nativeBitrate: 12_000_000,
      current: base,
      base,
    });
    expect(severe.profile.bitrate).toBe(6_000_000);
    expect(severe.profile.maxLongEdge).toBe(1280);
    expect(severe.healthyIntervals).toBe(0);
  });

  it('restores the selected preset after sustained health', () => {
    const base = remoteDesktopProfileForPreset('sharp', false);
    let profile = { ...base, bitrate: 6_000_000, maxLongEdge: 1280 };
    let healthyIntervals = 0;
    // Two healthy 2s intervals (4s) restore the preset.
    for (let i = 0; i < 2; i += 1) {
      const next = nextRemoteDesktopProfile({
        nativeBitrate: 28_000_000,
        current: profile,
        base,
        healthyIntervals,
      });
      profile = next.profile;
      healthyIntervals = next.healthyIntervals;
    }
    expect(profile).toEqual(base);
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
