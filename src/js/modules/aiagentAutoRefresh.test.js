import { afterEach, beforeEach, describe, expect, it } from 'vitest';
import {
  AUTO_REFRESH_INTERVAL_MS,
  readAutoRefreshPreference,
  shouldAutoRefresh,
  writeAutoRefreshPreference,
} from './aiagentAutoRefresh.js';

// vitest 配置的 environment 是 node，没有 window/document。
// 这里按用例注入最小桩件，而不是给整个项目切到 jsdom——
// 切换环境会影响所有既有测试，代价远大于收益。
function installBrowserStubs({ hidden = false, storageThrows = false } = {}) {
  const store = new Map();
  const localStorage = {
    getItem(key) {
      if (storageThrows) throw new Error('storage disabled');
      return store.has(key) ? store.get(key) : null;
    },
    setItem(key, value) {
      if (storageThrows) throw new Error('storage disabled');
      store.set(key, String(value));
    },
    clear() {
      store.clear();
    },
  };
  globalThis.window = { localStorage };
  globalThis.document = { hidden };
  return { store, localStorage };
}

function removeBrowserStubs() {
  delete globalThis.window;
  delete globalThis.document;
}

describe('readAutoRefreshPreference', () => {
  beforeEach(() => installBrowserStubs());
  afterEach(removeBrowserStubs);

  it('未设置时默认开启（用户盯着面板时希望看到状态变化）', () => {
    expect(readAutoRefreshPreference()).toBe(true);
  });

  it('显式关闭后读回 false', () => {
    window.localStorage.setItem('aiagent-auto-refresh', '0');
    expect(readAutoRefreshPreference()).toBe(false);
  });

  it('显式开启后读回 true', () => {
    window.localStorage.setItem('aiagent-auto-refresh', '1');
    expect(readAutoRefreshPreference()).toBe(true);
  });

  it('存储抛错时回退到默认开启，不阻断页面', () => {
    installBrowserStubs({ storageThrows: true });
    expect(readAutoRefreshPreference()).toBe(true);
  });

  it('无 window（非浏览器环境）时回退到默认开启', () => {
    removeBrowserStubs();
    expect(readAutoRefreshPreference()).toBe(true);
  });
});

describe('writeAutoRefreshPreference', () => {
  beforeEach(() => installBrowserStubs());
  afterEach(removeBrowserStubs);

  it('写入后能被读回', () => {
    writeAutoRefreshPreference(false);
    expect(readAutoRefreshPreference()).toBe(false);
    writeAutoRefreshPreference(true);
    expect(readAutoRefreshPreference()).toBe(true);
  });

  it('存储抛错时静默忽略，不抛给调用方', () => {
    installBrowserStubs({ storageThrows: true });
    expect(() => writeAutoRefreshPreference(true)).not.toThrow();
  });

  it('无 window 时不抛错', () => {
    removeBrowserStubs();
    expect(() => writeAutoRefreshPreference(true)).not.toThrow();
  });
});

describe('shouldAutoRefresh', () => {
  afterEach(removeBrowserStubs);

  it('开启且空闲且可见时才刷新', () => {
    installBrowserStubs({ hidden: false });
    expect(shouldAutoRefresh({ enabled: true, loading: false })).toBe(true);
  });

  it('关闭时不刷新', () => {
    installBrowserStubs({ hidden: false });
    expect(shouldAutoRefresh({ enabled: false, loading: false })).toBe(false);
  });

  it('有请求在途时不刷新（避免与手动刷新叠加成探测风暴）', () => {
    installBrowserStubs({ hidden: false });
    expect(shouldAutoRefresh({ enabled: true, loading: true })).toBe(false);
  });

  it('页面隐藏时不刷新（hook 暂停之外的第二道保险）', () => {
    installBrowserStubs({ hidden: true });
    expect(shouldAutoRefresh({ enabled: true, loading: false })).toBe(false);
  });

  it('参数缺失时不刷新，不抛错', () => {
    installBrowserStubs();
    expect(shouldAutoRefresh(undefined)).toBe(false);
    expect(shouldAutoRefresh({})).toBe(false);
  });

  it('无 document 时按「可见」处理，不抛错', () => {
    installBrowserStubs();
    delete globalThis.document;
    expect(shouldAutoRefresh({ enabled: true, loading: false })).toBe(true);
  });
});

describe('AUTO_REFRESH_INTERVAL_MS', () => {
  it('间隔不能过短：探测要往返主机 Agent，过于频繁会打满纳管主机', () => {
    expect(AUTO_REFRESH_INTERVAL_MS).toBeGreaterThanOrEqual(3000);
  });
});
