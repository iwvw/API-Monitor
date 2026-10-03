// 远程桌面的本地偏好持久化。
//
// 画质档与帧率属于「用户为这台设备挑好的观看参数」，刷新页面或重连后不应
// 被重置回默认值（否则每次刷新都要重选一次 60 FPS）。这里只存与服务器无关的
// 纯前端偏好，且对读写全部做容错：localStorage 在隐私模式/被禁用时会抛错，
// 此时静默回落到默认值，绝不影响远程桌面主流程。
import { DEFAULT_DESKTOP_FPS, DEFAULT_DESKTOP_PRESET, desktopPresetById, normalizedDesktopFps } from '../../modules/remoteDesktopTouch.js';

const STORAGE_KEY = 'remote_desktop_preferences';

function safeStorage() {
  try {
    return window.localStorage;
  } catch {
    return null;
  }
}

export function readDesktopPreferences() {
  const fallback = { preset: DEFAULT_DESKTOP_PRESET, fps: DEFAULT_DESKTOP_FPS };
  const storage = safeStorage();
  if (!storage) return fallback;
  try {
    const raw = storage.getItem(STORAGE_KEY);
    if (!raw) return fallback;
    const parsed = JSON.parse(raw);
    return {
      // 用 desktopPresetById / normalizedDesktopFps 校验：存量数据里的未知档位
      // （例如旧版本已删除的预设）必须回落到有效值，而不是把 undefined 透传下去。
      preset: desktopPresetById(parsed?.preset).id,
      fps: normalizedDesktopFps(parsed?.fps),
    };
  } catch {
    return fallback;
  }
}

export function writeDesktopPreferences({ preset, fps }) {
  const storage = safeStorage();
  if (!storage) return;
  try {
    storage.setItem(STORAGE_KEY, JSON.stringify({
      preset: desktopPresetById(preset).id,
      fps: normalizedDesktopFps(fps),
    }));
  } catch {
    // 配额满或被禁用：偏好丢失可接受，不影响会话。
  }
}
