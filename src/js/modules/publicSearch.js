// 公开页顶部搜索框：支持切换到外部搜索引擎。
//
// 引擎列表由**管理页统一配置**（存 public-settings 的 searchEngines 字段），
// 所有访客读取同一份；未配置时回落到这里的内置默认列表。
// 选择结果与新窗口开关存 localStorage，刷新后保持。
//
// icon 用 Iconify 图标名。选型过程（避免走弯路）：
//   - 不用各家 /favicon.ico：只有 16/32px，放大即糊；且依赖外网可达。
//   - 不用 simple-icons：它是**单色**集合，多个引擎会全是同一颜色，
//     失去品牌辨识度。
//   - 选定 logos 集合：自带官方**彩色**矢量 logo，透明背景，
//     Google 四色 / Bing 渐变 / DuckDuckGo 八色都是原始品牌配色。
// 自定义引擎的图标允许两种写法：Iconify 名（如 logos:bing）或图片 URL。
export const DEFAULT_SEARCH_ENGINES = [
  { id: 'bing', label: '必应', url: 'https://www.bing.com/search?q=%s', color: '#258FFB', icon: 'logos:bing' },
  { id: 'google', label: 'Google', url: 'https://www.google.com/search?q=%s', color: '#4285F4', icon: 'logos:google-icon' },
  { id: 'duckduckgo', label: 'DuckDuckGo', url: 'https://duckduckgo.com/?q=%s', color: '#DE5833', icon: 'logos:duckduckgo' },
];

/**
 * 兼容旧引用：仍导出 SEARCH_ENGINES（等于内置默认列表）。
 * 运行时请用 resolveSearchEngines(config) 读取「配置优先」的实际列表。
 */
export const SEARCH_ENGINES = DEFAULT_SEARCH_ENGINES;

const ENGINE_STORAGE_KEY = 'publicBookmarksSearchEngine';
const NEW_TAB_STORAGE_KEY = 'publicBookmarksSearchNewTab';

/** 引擎列表上限，与后端 searchEngineMaxCount 保持一致。 */
export const SEARCH_ENGINE_MAX_COUNT = 12;

/** 图标是否是可用的图片地址（http/https 或站内路径）。 */
export function isImageIcon(icon) {
  const raw = String(icon ?? '').trim();
  return /^https?:\/\//i.test(raw) || raw.startsWith('/');
}

/**
 * 归一化单个引擎项；非法项返回 null（调用方过滤掉）。
 *
 * 与后端 sanitizeSearchEngines 保持同一套规则：必须有名称、
 * 地址必须含 %s 且为 http(s)。前端也做一遍是为了给出即时反馈
 * （表单里能立刻标红），而不是保存后才被后端静默丢弃。
 */
export function normalizeEngine(raw) {
  if (!raw || typeof raw !== 'object') return null;
  const label = String(raw.label ?? '').trim();
  const url = String(raw.url ?? '').trim();
  if (!label || !url) return null;
  if (!url.includes('%s')) return null;
  if (!/^https?:\/\//i.test(url)) return null;

  const icon = String(raw.icon ?? '').trim();
  const colorRaw = String(raw.color ?? '').trim();
  const color = colorRaw && !colorRaw.startsWith('#') ? `#${colorRaw}` : colorRaw;
  const id = String(raw.id ?? '').trim();

  return {
    id,
    label: label.slice(0, 24),
    url: url.slice(0, 512),
    icon: icon.slice(0, 2048),
    color,
  };
}

/**
 * 给列表补齐 id：已有的保留，缺失时按主机名推断，再用 engine-N 兜底。
 * 供管理页编辑草稿使用，保证 React key 与选中态稳定。
 */
export function assignEngineIds(list = []) {
  return list
    .slice(0, SEARCH_ENGINE_MAX_COUNT)
    .map((item, index) => {
      const id = String(item?.id ?? '').trim();
      if (id) return { ...item, id };
      return { ...item, id: idFromUrl(item?.url) || `engine-${index + 1}` };
    });
}

/**
 * 从编辑草稿里取出**当前有效**的引擎项，用于写回设置。
 *
 * 关键点：还在填写中的空项（缺名称、缺 %s）只留在表单里，不写进配置。
 * 后端 sanitizeSearchEngines 同样会丢弃它们，写进去只会让配置与表单
 * 不一致；表单侧靠本地草稿保留这些行（见 SearchEnginesSettings）。
 */
export function persistableEngines(list = []) {
  return assignEngineIds(list)
    .map(item => normalizeEngine(item))
    .filter(Boolean);
}

/**
 * 从公开页配置解析出实际使用的引擎列表。
 *
 * 配置优先：管理页配了就整份替换（而不是与内置合并）——
 * 「删掉某个内置引擎」也是合法需求，合并会让它删不掉。
 * 配置为空/全部非法时回落到内置默认。
 */
export function resolveSearchEngines(config = {}) {
  const list = config && typeof config === 'object' ? config.searchEngines : null;
  if (!Array.isArray(list)) return DEFAULT_SEARCH_ENGINES;

  const normalized = [];
  const seen = new Set();
  for (const item of list) {
    if (normalized.length >= SEARCH_ENGINE_MAX_COUNT) break;
    const engine = normalizeEngine(item);
    if (!engine) continue;
    // 补齐 id：缺失或重复时按主机名生成并去重，保证 React key 与选中态稳定
    let id = engine.id || idFromUrl(engine.url);
    if (!id) id = 'engine';
    if (seen.has(id)) {
      let n = 2;
      while (seen.has(`${id}-${n}`)) n += 1;
      id = `${id}-${n}`;
    }
    seen.add(id);
    normalized.push({ ...engine, id });
  }
  return normalized.length ? normalized : DEFAULT_SEARCH_ENGINES;
}

/** 由搜索地址模板推断一个稳定 id（如 https://www.bing.com/search?q=%s -> bing）。 */
export function idFromUrl(url) {
  try {
    const host = new URL(url).hostname.toLowerCase().replace(/^www\./, '');
    const head = host.split('.')[0] || '';
    return head.replace(/[^a-z0-9-]/g, '');
  } catch {
    return '';
  }
}

export const getDefaultEngine = (engines = DEFAULT_SEARCH_ENGINES) => engines[0];

export function findEngine(id, engines = DEFAULT_SEARCH_ENGINES) {
  const list = Array.isArray(engines) && engines.length ? engines : DEFAULT_SEARCH_ENGINES;
  return list.find(engine => engine.id === id) || list[0];
}

export function readStoredEngineId(engines = DEFAULT_SEARCH_ENGINES) {
  try {
    const value = window.localStorage?.getItem(ENGINE_STORAGE_KEY);
    return findEngine(value, engines).id;
  } catch {
    return getDefaultEngine(engines).id;
  }
}

export function writeStoredEngineId(id, engines = DEFAULT_SEARCH_ENGINES) {
  try {
    window.localStorage?.setItem(ENGINE_STORAGE_KEY, findEngine(id, engines).id);
  } catch {
    // 隐私模式下写入失败可忽略，仅本次会话生效
  }
}

export function readStoredNewTab() {
  try {
    const value = window.localStorage?.getItem(NEW_TAB_STORAGE_KEY);
    // 默认新窗口打开：搜索结果通常不希望覆盖当前导航页
    return value === null ? true : value === 'true';
  } catch {
    return true;
  }
}

export function writeStoredNewTab(value) {
  try {
    window.localStorage?.setItem(NEW_TAB_STORAGE_KEY, value ? 'true' : 'false');
  } catch {
    // 同上
  }
}

/**
 * 构造搜索地址。空关键词返回空串（调用方据此忽略提交）。
 * 用 encodeURIComponent 编码，避免关键词里的 & # 破坏 URL。
 */
export function buildSearchUrl(engineId, keyword, engines = DEFAULT_SEARCH_ENGINES) {
  const query = String(keyword ?? '').trim();
  if (!query) return '';
  const engine = findEngine(engineId, engines);
  if (!engine?.url?.includes('%s')) return '';
  return engine.url.replace('%s', encodeURIComponent(query));
}

/**
 * 执行搜索。
 * @returns {boolean} 是否真的发起了跳转（空关键词返回 false）
 */
export function runSearch(engineId, keyword, newTab, engines = DEFAULT_SEARCH_ENGINES) {
  const url = buildSearchUrl(engineId, keyword, engines);
  if (!url) return false;
  if (newTab) window.open(url, '_blank', 'noopener,noreferrer');
  else window.location.href = url;
  return true;
}
