import { describe, expect, it, beforeEach, vi } from 'vitest';
import { readDesktopPreferences, writeDesktopPreferences } from './preferences.js';
import { DEFAULT_DESKTOP_FPS, DEFAULT_DESKTOP_PRESET } from '../../modules/remoteDesktopTouch.js';

const STORAGE_KEY = 'remote_desktop_preferences';

function createMemoryStorage() {
  const map = new Map();
  return {
    getItem: key => (map.has(key) ? map.get(key) : null),
    setItem: (key, value) => map.set(key, String(value)),
    removeItem: key => map.delete(key),
  };
}

describe('remote desktop preferences', () => {
  beforeEach(() => {
    vi.stubGlobal('window', { localStorage: createMemoryStorage() });
  });

  it('falls back to defaults when nothing is stored', () => {
    expect(readDesktopPreferences()).toEqual({ preset: DEFAULT_DESKTOP_PRESET, fps: DEFAULT_DESKTOP_FPS });
  });

  it('round-trips the selected preset and fps', () => {
    writeDesktopPreferences({ preset: 'ultra', fps: 60 });
    expect(readDesktopPreferences()).toEqual({ preset: 'ultra', fps: 60 });
  });

  it('sanitises unknown or corrupt stored values instead of passing them through', () => {
    // 旧版本删除过的档位 / 非法帧率必须回落，不能把 undefined 透传给编码器。
    window.localStorage.setItem(STORAGE_KEY, JSON.stringify({ preset: 'removed-tier', fps: 144 }));
    expect(readDesktopPreferences()).toEqual({ preset: DEFAULT_DESKTOP_PRESET, fps: DEFAULT_DESKTOP_FPS });

    window.localStorage.setItem(STORAGE_KEY, 'not-json');
    expect(readDesktopPreferences()).toEqual({ preset: DEFAULT_DESKTOP_PRESET, fps: DEFAULT_DESKTOP_FPS });

    window.localStorage.setItem(STORAGE_KEY, JSON.stringify(null));
    expect(readDesktopPreferences()).toEqual({ preset: DEFAULT_DESKTOP_PRESET, fps: DEFAULT_DESKTOP_FPS });
  });

  it('survives a throwing or unavailable localStorage', () => {
    // 隐私模式下 getItem/setItem 会抛错，偏好读写不得影响远程桌面主流程。
    vi.stubGlobal('window', {
      get localStorage() {
        throw new Error('denied');
      },
    });
    expect(readDesktopPreferences()).toEqual({ preset: DEFAULT_DESKTOP_PRESET, fps: DEFAULT_DESKTOP_FPS });
    expect(() => writeDesktopPreferences({ preset: 'sharp', fps: 60 })).not.toThrow();

    vi.stubGlobal('window', { localStorage: { getItem: () => { throw new Error('boom'); }, setItem: () => {} } });
    expect(readDesktopPreferences()).toEqual({ preset: DEFAULT_DESKTOP_PRESET, fps: DEFAULT_DESKTOP_FPS });

    vi.stubGlobal('window', { localStorage: { getItem: () => null, setItem: () => { throw new Error('quota'); } } });
    expect(() => writeDesktopPreferences({ preset: 'sharp', fps: 60 })).not.toThrow();
  });
});
