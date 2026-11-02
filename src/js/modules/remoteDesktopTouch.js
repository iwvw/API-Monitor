export const TOUCH_TAP_MAX_MS = 280;
export const TOUCH_TAP_SLOP = 10;
export const TOUCH_DOUBLE_TAP_MS = 320;
export const TOUCH_DOUBLE_TAP_DISTANCE = 32;
export const TOUCH_LONG_PRESS_MS = 420;
export const TOUCH_SCROLL_STEP = 20;
export const TOUCH_PINCH_SLOP = 12;
export const TOUCH_SCROLL_SLOP = 8;

const clamp = (value, minimum, maximum) => Math.max(minimum, Math.min(maximum, value));

export function pointDistance(a, b) {
  return Math.hypot(Number(a?.x || 0) - Number(b?.x || 0), Number(a?.y || 0) - Number(b?.y || 0));
}

export function accelerateTrackpadDelta(deltaX, deltaY, elapsedMs = 16) {
  const distance = Math.hypot(deltaX, deltaY);
  if (!distance) return { x: 0, y: 0, gain: 1 };
  const speed = distance / Math.max(4, elapsedMs);
  const gain = speed <= 0.25 ? 1 : 1 + clamp((speed - 0.25) / 1.25, 0, 1) * 1.6;
  return { x: deltaX * gain, y: deltaY * gain, gain };
}

export function normalizedTrackpadDelta(deltaX, deltaY, elapsedMs, viewport, remoteVideo) {
  const accelerated = accelerateTrackpadDelta(deltaX, deltaY, elapsedMs);
  const remoteWidth = Math.max(1, Number(remoteVideo?.width || viewport?.width || 1));
  const remoteHeight = Math.max(1, Number(remoteVideo?.height || viewport?.height || 1));
  return {
    x: accelerated.x / remoteWidth,
    y: accelerated.y / remoteHeight,
  };
}

export function trackpadPixelDelta(deltaX, deltaY, elapsedMs) {
  const accelerated = accelerateTrackpadDelta(deltaX, deltaY, elapsedMs);
  return { x: accelerated.x, y: accelerated.y };
}

export function trackpadButtonMessage(action, button = 0) {
  return {
    type: 'mouse',
    action,
    button,
  };
}

export function normalizedVideoPoint(
  point,
  surface,
  video,
  fillMode,
  transform = { scale: 1, x: 0, y: 0 },
  clampOutside = false
) {
  const surfaceWidth = Math.max(1, Number(surface?.width || 1));
  const surfaceHeight = Math.max(1, Number(surface?.height || 1));
  const videoWidth = Math.max(1, Number(video?.width || surfaceWidth));
  const videoHeight = Math.max(1, Number(video?.height || surfaceHeight));
  const fitScale =
    fillMode === 'cover'
      ? Math.max(surfaceWidth / videoWidth, surfaceHeight / videoHeight)
      : Math.min(surfaceWidth / videoWidth, surfaceHeight / videoHeight);
  const renderedWidth = videoWidth * fitScale;
  const renderedHeight = videoHeight * fitScale;
  const offsetX = (surfaceWidth - renderedWidth) / 2;
  const offsetY = (surfaceHeight - renderedHeight) / 2;
  const viewScale = Math.max(0.01, Number(transform?.scale || 1));
  const clientX = Number(point?.clientX ?? point?.x ?? 0);
  const clientY = Number(point?.clientY ?? point?.y ?? 0);
  const localX = (clientX - Number(surface?.left || 0) - Number(transform?.x || 0)) / viewScale;
  const localY = (clientY - Number(surface?.top || 0) - Number(transform?.y || 0)) / viewScale;
  const normalizedX = (localX - offsetX) / renderedWidth;
  const normalizedY = (localY - offsetY) / renderedHeight;
  if (!clampOutside && (normalizedX < 0 || normalizedX > 1 || normalizedY < 0 || normalizedY > 1)) {
    return null;
  }
  return {
    x: clamp(normalizedX, 0, 1),
    y: clamp(normalizedY, 0, 1),
  };
}

// 画质预设。桌面场景下提高帧率的边际收益低于单帧清晰度，因此画质档只描述
// 「分辨率 + 码率」，帧率由独立的 FPS 档位控制（见 DESKTOP_FPS_OPTIONS）。
// 编码侧 maxLongEdge 的合法区间为 [1280, 3840]（agent-rust remote_desktop.rs
// 的 MIN/MAX_MAX_LONG_EDGE），超出会被钳制；实际输出还会被远程显示器原生
// 分辨率封顶——缩放只降不升，所以这里的值是「上限」而非保证值。
export const DESKTOP_QUALITY_PRESETS = [
  { id: 'smooth', label: '流畅', bitrate: 6_000_000, maxLongEdge: 1280 },
  { id: 'balanced', label: '适应', bitrate: 12_000_000, maxLongEdge: 1920 },
  { id: 'sharp', label: '清晰', bitrate: 24_000_000, maxLongEdge: 2560 },
  { id: 'ultra', label: '超清', bitrate: 32_000_000, maxLongEdge: 3840 },
];

// 帧率档位。Agent 侧 TARGET_FPS = 60 是硬上限（video_config 里 fps 会被
// clamp(30, 60)），因此这里只提供 30/60 两档；30 是默认值，弱网/弱机更稳。
export const DESKTOP_FPS_OPTIONS = [
  { id: 30, label: '30 FPS' },
  { id: 60, label: '60 FPS' },
];

export const DEFAULT_DESKTOP_FPS = 30;

export const DEFAULT_DESKTOP_PRESET = 'balanced';

export function desktopPresetById(id) {
  return (
    DESKTOP_QUALITY_PRESETS.find(preset => preset.id === id)
    || DESKTOP_QUALITY_PRESETS.find(preset => preset.id === DEFAULT_DESKTOP_PRESET)
  );
}

export function normalizedDesktopFps(fps) {
  const value = Number(fps);
  return value === 60 ? 60 : DEFAULT_DESKTOP_FPS;
}

// 预设 + 帧率对应的视频档位。粗指针移动端压低帧率与码率以优先流畅度与流量。
export function remoteDesktopProfileForPreset(presetId, coarsePointer = false, fps = DEFAULT_DESKTOP_FPS) {
  const preset = desktopPresetById(presetId);
  const requestedFps = normalizedDesktopFps(fps);
  return {
    fps: coarsePointer ? Math.min(30, requestedFps) : requestedFps,
    bitrate: coarsePointer ? Math.min(6_000_000, preset.bitrate) : preset.bitrate,
    maxLongEdge: preset.maxLongEdge,
  };
}

export function nextRemoteDesktopProfile({
  loss = 0,
  rtt = 0,
  bufferMs = 0,
  nativeBitrate = 12_000_000,
  healthyIntervals = 0,
  current = { fps: 30, bitrate: 12_000_000, maxLongEdge: 1920 },
  droppedFps = 0,
  // 当前所选画质预设档位；严重劣化时会在其基础上同时下调码率与分辨率。
  base = current,
}) {
  // 归一化基准档位，容忍调用方省略 maxLongEdge。
  const baseProfile = {
    fps: base.fps,
    bitrate: base.bitrate,
    maxLongEdge: Number(base.maxLongEdge) > 0 ? base.maxLongEdge : 1920,
  };
  const floor = Math.min(6_000_000, nativeBitrate);
  const mid = Math.min(8_000_000, nativeBitrate);
  if (loss > 5 || rtt > 140 || bufferMs > 100 || droppedFps > 4) {
    return {
      profile: {
        ...baseProfile,
        bitrate: floor,
        maxLongEdge: Math.min(baseProfile.maxLongEdge, 1280),
      },
      healthyIntervals: 0,
    };
  }
  if (loss > 2 || rtt > 80 || bufferMs > 40 || droppedFps > 1) {
    return {
      profile: {
        ...baseProfile,
        bitrate: mid,
        maxLongEdge: Math.min(baseProfile.maxLongEdge, 1920),
      },
      healthyIntervals: 0,
    };
  }
  const nextHealthy = healthyIntervals + 1;
  return {
    // Two healthy 2s intervals (4s) before restoring the preset, so recovery is
    // quick without oscillating between profiles.
    profile: nextHealthy >= 2 ? baseProfile : current,
    healthyIntervals: nextHealthy,
  };
}

export function consumeScrollDelta(remainder, deltaX, deltaY, threshold = TOUCH_SCROLL_STEP) {
  const x = Number(remainder?.x || 0) + deltaX;
  const y = Number(remainder?.y || 0) + deltaY;
  const stepsX = x >= 0 ? Math.floor(x / threshold) : Math.ceil(x / threshold);
  const stepsY = y >= 0 ? Math.floor(y / threshold) : Math.ceil(y / threshold);
  return {
    stepsX,
    stepsY,
    remainder: {
      x: x - stepsX * threshold,
      y: y - stepsY * threshold,
    },
  };
}

export function isDoubleTap(previous, current) {
  if (!previous || !current) return false;
  const elapsed = current.at - previous.at;
  return (
    elapsed >= 0 &&
    elapsed <= TOUCH_DOUBLE_TAP_MS &&
    pointDistance(previous, current) <= TOUCH_DOUBLE_TAP_DISTANCE
  );
}

export function nextPinchTransform(
  startTransform,
  startCenter,
  currentCenter,
  startDistance,
  currentDistance,
  viewport
) {
  const initialScale = Math.max(1, Number(startTransform?.scale || 1));
  const scale = clamp(initialScale * (currentDistance / Math.max(1, startDistance)), 1, 3);
  const ratio = scale / initialScale;
  const width = Math.max(1, Number(viewport?.width || 1));
  const height = Math.max(1, Number(viewport?.height || 1));
  const rawX = currentCenter.x - (startCenter.x - Number(startTransform?.x || 0)) * ratio;
  const rawY = currentCenter.y - (startCenter.y - Number(startTransform?.y || 0)) * ratio;
  return {
    scale,
    x: scale === 1 ? 0 : clamp(rawX, width * (1 - scale), 0),
    y: scale === 1 ? 0 : clamp(rawY, height * (1 - scale), 0),
  };
}

export function remoteCursorPoint(
  position,
  viewport,
  video,
  fillMode,
  transform = { scale: 1, x: 0, y: 0 }
) {
  const viewportWidth = Math.max(1, Number(viewport?.width || 1));
  const viewportHeight = Math.max(1, Number(viewport?.height || 1));
  const videoWidth = Math.max(1, Number(video?.width || viewportWidth));
  const videoHeight = Math.max(1, Number(video?.height || viewportHeight));
  const fitScale =
    fillMode === 'cover'
      ? Math.max(viewportWidth / videoWidth, viewportHeight / videoHeight)
      : Math.min(viewportWidth / videoWidth, viewportHeight / videoHeight);
  const renderedWidth = videoWidth * fitScale;
  const renderedHeight = videoHeight * fitScale;
  const offsetX = (viewportWidth - renderedWidth) / 2;
  const offsetY = (viewportHeight - renderedHeight) / 2;
  return {
    x:
      Number(transform.x || 0) +
      (offsetX + clamp(Number(position?.x || 0), 0, 1) * renderedWidth) *
        Number(transform.scale || 1),
    y:
      Number(transform.y || 0) +
      (offsetY + clamp(Number(position?.y || 0), 0, 1) * renderedHeight) *
        Number(transform.scale || 1),
  };
}
