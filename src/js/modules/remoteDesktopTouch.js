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

// 初始/恢复档位。nativeBitrate 为前端按实际接收分辨率推算的码率上限（未知时传 0）：
// 桌面端在高分屏上按原生档恢复，粗指针移动端仍以低码率优先，避免移动网络抖动。
export function initialRemoteDesktopProfile(coarsePointer, nativeBitrate = 0) {
  if (coarsePointer) {
    return { fps: 30, bitrate: 6_000_000 };
  }
  return { fps: 60, bitrate: nativeBitrate > 12_000_000 ? nativeBitrate : 12_000_000 };
}

export function nextRemoteDesktopProfile({
  loss = 0,
  rtt = 0,
  bufferMs = 0,
  nativeBitrate = 12_000_000,
  healthyIntervals = 0,
  current = { fps: 60, bitrate: 12_000_000 },
  droppedFps = 0,
  coarsePointer = false,
}) {
  if (loss > 5 || rtt > 140 || bufferMs > 100 || droppedFps > 4) {
    return {
      profile: { fps: 30, bitrate: Math.min(6_000_000, nativeBitrate) },
      healthyIntervals: 0,
    };
  }
  if (loss > 2 || rtt > 80 || bufferMs > 40 || droppedFps > 1) {
    return {
      profile: { fps: 30, bitrate: Math.min(8_000_000, nativeBitrate) },
      healthyIntervals: 0,
    };
  }
  const nextHealthy = healthyIntervals + 1;
  return {
    // Three healthy 2s intervals (6s) before restoring the full profile, so
    // recovery from a degraded link is quick without oscillating.
    profile: nextHealthy >= 3 ? initialRemoteDesktopProfile(coarsePointer, nativeBitrate) : current,
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
