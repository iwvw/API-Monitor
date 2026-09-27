import { describe, expect, it, vi, beforeEach } from 'vitest';
import {
  DEFAULT_SEARCH_ENGINES,
  SEARCH_ENGINE_MAX_COUNT,
  SEARCH_ENGINES,
  buildSearchUrl,
  findEngine,
  getDefaultEngine,
  idFromUrl,
  isImageIcon,
  normalizeEngine,
  readStoredEngineId,
  readStoredNewTab,
  resolveSearchEngines,
  runSearch,
  writeStoredEngineId,
  writeStoredNewTab,
} from './publicSearch.js';

// 内存版 localStorage（测试环境为 node，没有 window）
function installWindow() {
  const store = new Map();
  globalThis.window = {
    localStorage: {
      getItem: key => (store.has(key) ? store.get(key) : null),
      setItem: (key, value) => store.set(key, String(value)),
      removeItem: key => store.delete(key),
    },
    open: vi.fn(),
    location: { href: '' },
  };
  return { store, window: globalThis.window };
}

describe('publicSearch', () => {
  beforeEach(() => {
    delete globalThis.window;
  });

  it('内置默认引擎都带 %s 占位符，且不含已移除的百度', () => {
    expect(DEFAULT_SEARCH_ENGINES.length).toBeGreaterThanOrEqual(3);
    for (const engine of DEFAULT_SEARCH_ENGINES) {
      expect(engine.url).toContain('%s');
      expect(engine.id).toBeTruthy();
      expect(engine.label).toBeTruthy();
    }
    // 百度已按需求移除；若后续又被加回来，这条会提醒确认图标方案
    expect(DEFAULT_SEARCH_ENGINES.some(e => e.id === 'baidu')).toBe(false);
  });

  it('findEngine 未知 id 回退默认引擎', () => {
    expect(findEngine('bing').id).toBe('bing');
    expect(findEngine('nope').id).toBe(getDefaultEngine().id);
    expect(findEngine(undefined).id).toBe(getDefaultEngine().id);
  });

  it('buildSearchUrl 正确编码关键词', () => {
    const url = buildSearchUrl('bing', 'hello world');
    expect(url).toBe('https://www.bing.com/search?q=hello%20world');
  });

  it('buildSearchUrl 编码特殊字符，避免破坏 URL', () => {
    const url = buildSearchUrl('google', 'a&b=c#d');
    expect(url).toContain('a%26b%3Dc%23d');
    expect(url.startsWith('https://www.google.com/search?q=')).toBe(true);
  });

  it('空关键词返回空串', () => {
    expect(buildSearchUrl('bing', '')).toBe('');
    expect(buildSearchUrl('bing', '   ')).toBe('');
    expect(buildSearchUrl('bing', null)).toBe('');
  });

  it('每个引擎都能生成合法地址', () => {
    for (const engine of SEARCH_ENGINES) {
      const url = buildSearchUrl(engine.id, 'test');
      expect(url.startsWith('https://')).toBe(true);
      expect(url).not.toContain('%s');
    }
  });

  it('runSearch 空关键词不跳转', () => {
    const { window } = installWindow();
    expect(runSearch('bing', '', true)).toBe(false);
    expect(window.open).not.toHaveBeenCalled();
    expect(window.location.href).toBe('');
  });

  it('runSearch 新窗口模式使用 window.open', () => {
    const { window } = installWindow();
    expect(runSearch('bing', 'test', true)).toBe(true);
    expect(window.open).toHaveBeenCalledTimes(1);
    expect(window.open.mock.calls[0][1]).toBe('_blank');
    // 必须带 noopener 防止 tabnabbing
    expect(window.open.mock.calls[0][2]).toContain('noopener');
  });

  it('runSearch 当前页模式修改 location', () => {
    const { window } = installWindow();
    expect(runSearch('bing', 'test', false)).toBe(true);
    expect(window.location.href).toContain('bing.com/search');
    expect(window.open).not.toHaveBeenCalled();
  });

  it('引擎选择持久化并能读回', () => {
    installWindow();
    expect(readStoredEngineId()).toBe(getDefaultEngine().id);
    writeStoredEngineId('google');
    expect(readStoredEngineId()).toBe('google');
    // 非法值回退默认
    writeStoredEngineId('bogus');
    expect(readStoredEngineId()).toBe(getDefaultEngine().id);
  });

  it('新窗口开关持久化，默认开启', () => {
    installWindow();
    expect(readStoredNewTab()).toBe(true);
    writeStoredNewTab(false);
    expect(readStoredNewTab()).toBe(false);
    writeStoredNewTab(true);
    expect(readStoredNewTab()).toBe(true);
  });

  it('localStorage 不可用时安全降级', () => {
    globalThis.window = {
      get localStorage() { throw new Error('blocked'); },
      open: vi.fn(),
      location: { href: '' },
    };
    expect(() => readStoredEngineId()).not.toThrow();
    expect(() => writeStoredEngineId('bing')).not.toThrow();
    expect(() => readStoredNewTab()).not.toThrow();
    expect(readStoredNewTab()).toBe(true);
  });
});

describe('自定义搜索引擎', () => {
  it('normalizeEngine 接受合法项并补全 color 的 #', () => {
    const engine = normalizeEngine({ label: '必应', url: 'https://b.com/s?q=%s', color: '258FFB' });
    expect(engine).not.toBeNull();
    expect(engine.label).toBe('必应');
    expect(engine.color).toBe('#258FFB');
  });

  it('normalizeEngine 拒绝缺 %s / 非 http / 空名称', () => {
    expect(normalizeEngine({ label: 'x', url: 'https://b.com/search' })).toBeNull();       // 无 %s
    expect(normalizeEngine({ label: 'x', url: 'javascript:alert(1)%s' })).toBeNull();     // 非 http
    expect(normalizeEngine({ label: '', url: 'https://b.com/s?q=%s' })).toBeNull();       // 无名称
    expect(normalizeEngine(null)).toBeNull();
    expect(normalizeEngine('bing')).toBeNull();
  });

  it('resolveSearchEngines 优先用配置，且整份替换（不与内置合并）', () => {
    const engines = resolveSearchEngines({
      searchEngines: [
        { id: 'custom', label: '自建', url: 'https://s.example.com/?q=%s', icon: 'logos:bing' },
      ],
    });
    expect(engines).toHaveLength(1);
    expect(engines[0].id).toBe('custom');
    // 内置的 google 不应出现 —— 「删掉内置引擎」必须真的删得掉
    expect(engines.some(e => e.id === 'google')).toBe(false);
  });

  it('resolveSearchEngines 在配置为空/非法时回落到内置默认', () => {
    expect(resolveSearchEngines({})).toEqual(DEFAULT_SEARCH_ENGINES);
    expect(resolveSearchEngines({ searchEngines: [] })).toEqual(DEFAULT_SEARCH_ENGINES);
    expect(resolveSearchEngines({ searchEngines: 'nope' })).toEqual(DEFAULT_SEARCH_ENGINES);
    // 全部非法 -> 回落默认，而不是给出空列表（否则搜索框没引擎可用）
    expect(resolveSearchEngines({ searchEngines: [{ label: 'x', url: 'ftp://a/%s' }] }))
      .toEqual(DEFAULT_SEARCH_ENGINES);
  });

  it('缺失 id 时按主机名补全，并处理重复', () => {
    const engines = resolveSearchEngines({
      searchEngines: [
        { label: 'A', url: 'https://www.example.com/s?q=%s' },
        { label: 'B', url: 'https://example.com/other?q=%s' },
      ],
    });
    expect(engines[0].id).toBe('example');
    expect(engines[1].id).toBe('example-2'); // 去重，保证 React key / 选中态唯一
  });

  it('数量超过上限时截断', () => {
    const many = Array.from({ length: SEARCH_ENGINE_MAX_COUNT + 5 }, (_, i) => ({
      label: `e${i}`,
      url: `https://e${i}.com/?q=%s`,
    }));
    expect(resolveSearchEngines({ searchEngines: many })).toHaveLength(SEARCH_ENGINE_MAX_COUNT);
  });

  it('buildSearchUrl 使用传入的引擎列表', () => {
    const engines = resolveSearchEngines({
      searchEngines: [{ id: 'kagi', label: 'Kagi', url: 'https://kagi.com/search?q=%s' }],
    });
    expect(buildSearchUrl('kagi', 'hello world', engines))
      .toBe('https://kagi.com/search?q=hello%20world');
  });

  it('idFromUrl 从主机名生成稳定 id', () => {
    expect(idFromUrl('https://www.bing.com/search?q=%s')).toBe('bing');
    expect(idFromUrl('https://s.example.co.uk/?q=%s')).toBe('s');
    expect(idFromUrl('not a url')).toBe('');
  });

  it('isImageIcon 区分图片地址与 Iconify 名', () => {
    expect(isImageIcon('https://a.com/i.png')).toBe(true);
    expect(isImageIcon('/logo.svg')).toBe(true);
    expect(isImageIcon('logos:bing')).toBe(false);
    expect(isImageIcon('')).toBe(false);
  });
});
