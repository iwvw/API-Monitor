// TilesBoard 常量与布局纯函数：图块定义、云端缓存键、列数分桶、默认布局打包。
import useStore from '../../store.js';

export const FETCH_TIMEOUT_MS = 8000;
export const HOST_POLL_MS = 5000;
// 仪表盘时间粒度持久化：切换后刷新/重访仍保持上次选择的时段
export const RANGE_STORAGE_KEY = 'tileboard_time_range';

export const loadRangeFromStorage = () => {
  try {
    const raw = localStorage.getItem(RANGE_STORAGE_KEY);
    if (!raw) return null;
    const p = JSON.parse(raw);
    if (p && Number.isFinite(p.days) && p.days > 0 && typeof p.label === 'string' && typeof p.cfRange === 'string') {
      return p;
    }
  } catch {
    /* ignore */
  }
  return null;
};

// —— 首页数据缓存 ——
// 两层：内存层（模块级，切页重挂载直接命中）+ localStorage 层（跨刷新/重开有效）。
// 仅缓存相对静态的聚合数据；实时轮询（主机指标）不缓存。命中缓存后仍后台刷新保持新鲜。
const memoryCache = new Map();

export function cacheGet(key) {
  const mem = memoryCache.get(key);
  if (mem && mem.expiresAt > Date.now()) return mem.value;
  if (mem) memoryCache.delete(key);
  try {
    const raw = localStorage.getItem(`tileboard_cache_${key}`);
    if (raw) {
      const p = JSON.parse(raw);
      if (p && p.expiresAt > Date.now()) {
        memoryCache.set(key, { value: p.value, expiresAt: p.expiresAt });
        return p.value;
      }
      if (p) localStorage.removeItem(`tileboard_cache_${key}`);
    }
  } catch {
    /* ignore */
  }
  return null;
}

export function cacheSet(key, value, ttlMs) {
  const expiresAt = Date.now() + ttlMs;
  memoryCache.set(key, { value, expiresAt });
  try {
    localStorage.setItem(`tileboard_cache_${key}`, JSON.stringify({ value, expiresAt }));
  } catch {
    /* ignore */
  }
}

export const CACHE_TTL = {
  dash: 10 * 60 * 1000, // 仪表盘聚合（10 个接口，含 Koyeb/Fly 慢上游）：跨刷新 10 分钟
  apiStats: 2 * 60 * 1000,
  openai: 2 * 60 * 1000,
  uptime: 60 * 1000,
  cfAccounts: 5 * 60 * 1000,
  cfZones: 5 * 60 * 1000,
  cfAnalytics: 60 * 1000,
};

// 布局按列数分桶：2~8 列各一套独立布局预设，各自自动保存到云端；列数可随意切换（自适应或窗口宽度），
// 切到哪列数就用哪套布局，布局保持稳定不重排。
export const MIN_COLS = 2;
export const MAX_COLS = 8;

export const TILE_DEFS = [
  { id: 'apiTrend', title: 'API 调用趋势', w: 4, h: 2 },
  { id: 'apiTokens', title: 'API 令牌消耗', w: 1, h: 2 },
  { id: 'openaiRequests', title: 'OpenAI 网关请求', w: 2, h: 2 },
  { id: 'openaiLatency', title: 'OpenAI 延迟', w: 2, h: 2 },
  { id: 'openaiErrors', title: 'OpenAI 错误数', w: 1, h: 2 },
  { id: 'hostCpu', title: '主机性能', w: 2, h: 2 },
  { id: 'uptime', title: '监控可用率', w: 1, h: 2 },
  { id: 'cfZone', title: 'Cloudflare Zone 请求', w: 2, h: 2 },
  { id: 'servers', title: '服务器状态', w: 2, h: 2 },
  { id: 'paas', title: 'PaaS 实例', w: 2, h: 2 },
  { id: 'scheduler', title: '定时任务', w: 1, h: 2 },
  { id: 'moduleTools', title: '模块入口', w: 2, h: 1, minW: 2, minH: 1 },
  { id: 'statusPages', title: '状态页', w: 1, h: 2, minH: 2 },
];
export const TILE_DEFS_BY_ID = Object.fromEntries(TILE_DEFS.map((d) => [d.id, d]));

// 模块间跳转：与侧边栏行为一致（切换激活 tab + 同步地址栏），保证刷新后仍停留在目标模块。
export const navigateModule = (module, query) => {
  useStore.getState().setMainActiveTab(module);
  const nextPath = query ? `/${module}?${new URLSearchParams(query).toString()}` : `/${module}`;
  if (window.location.pathname + window.location.search !== nextPath) {
    window.history.pushState({ module }, '', nextPath);
  }
};

// 状态页公开路由：uptime → /status/slug、server → /s/slug、github → /gh/slug
export const statusPageHref = (p) => {
  if (!p?.slug) return null;
  const prefix = p.kind === 'server' ? '/s' : p.kind === 'github' ? '/gh' : '/status';
  return `${prefix}/${encodeURIComponent(p.slug)}`;
};

export function tileDensity(w, h) {
  if (h <= 1) return 'half';
  if (h >= 4) return 'rich';
  if (w >= 2) return 'full';
  return 'compact';
}

// 宽度分档：拉宽时信息密度递增，而不是单纯拉伸留白。
// narrow（1 列）：主值 + 环比；medium（2~3 列）：追加 日均/峰值；wide（≥4 列）：再加 今日 等维度。
export function widthTier(w) {
  return w >= 4 ? 'wide' : w >= 2 ? 'medium' : 'narrow';
}

// 生成铺满当前列数的默认布局（全部 2 行高，行打包 + 末行加宽填满，任意列数协调无空白）。
// 半高（1 行）不作为默认，而是保留在缩放档位中：拖拽柄可随时把任意卡缩到半高。
export function packDefaultLayout(cols) {
  const tiles = [
    { i: 'apiTrend', w: 4 },
    { i: 'openaiRequests', w: 2 },
    { i: 'hostCpu', w: 2 },
    { i: 'openaiLatency', w: 2 },
    { i: 'cfZone', w: 2 },
    { i: 'apiTokens', w: 1 },
    { i: 'openaiErrors', w: 1 },
    { i: 'uptime', w: 1 },
    { i: 'servers', w: 2 },
    { i: 'paas', w: 2 },
    { i: 'scheduler', w: 1 },
    { i: 'moduleTools', w: 2 },
    { i: 'statusPages', w: 1 },
  ];
  // 移动端（2 列）：全部卡使用 0.5×1 / 1×1（w=1），两两一行自动填充、末行单卡吸满整行，除底部外不留空白
  if (cols === 2) {
    const rows2 = [];
    let row2 = [];
    for (const t of tiles) {
      row2.push({ ...t, w: 1 });
      if (row2.length === 2) {
        rows2.push(row2);
        row2 = [];
      }
    }
    if (row2.length) rows2.push(row2);
    const lastR = rows2[rows2.length - 1];
    if (lastR && lastR.length === 1) lastR[0].w = 2; // 末行单卡吸满整行
    const layout = [];
    let y = 0;
    rows2.forEach((r) => {
      let x = 0;
      let rowH = 1;
      for (const t of r) {
        // 模块入口移动端用全高单列（1×2），其余按各自默认高度
        const h = t.i === 'moduleTools' ? 2 : (TILE_DEFS_BY_ID[t.i]?.h ?? 1);
        rowH = Math.max(rowH, h);
        layout.push({ i: t.i, x, y, w: t.w, h, minW: 1, minH: 1, maxW: cols, maxH: 4 });
        x += t.w;
      }
      y += rowH;
    });
    return layout;
  }
  const rows = [];
  let row = [];
  let rowW = 0;
  for (const t of tiles) {
    const w = Math.min(t.w, cols);
    if (rowW + w > cols) {
      rows.push(row);
      row = [];
      rowW = 0;
    }
    row.push({ ...t, w });
    rowW += w;
  }
  if (row.length) rows.push(row);
  // 末行余列由最后一张可扩展的卡加宽吸满（上限为整行）；跳过默认窄高卡（如状态页 1×2），
  // 避免把它的默认尺寸拉成横幅。
  const last = rows[rows.length - 1];
  const lastW = last ? last.reduce((s, t) => s + t.w, 0) : 0;
  if (last && lastW < cols) {
    for (let idx = last.length - 1; idx >= 0; idx -= 1) {
      const t = last[idx];
      const def = TILE_DEFS_BY_ID[t.i];
      const isNarrowTall = def && def.w === 1 && def.h >= 2;
      if (isNarrowTall) continue;
      t.w = Math.min(t.w + (cols - lastW), cols);
      break;
    }
  }
  const layout = [];
  let y = 0;
  rows.forEach((r) => {
    let x = 0;
    let rowH = 1;
    for (const t of r) {
      const h = TILE_DEFS_BY_ID[t.i]?.h ?? 1;
      rowH = Math.max(rowH, h);
      layout.push({ i: t.i, x, y, w: t.w, h, minW: TILE_DEFS_BY_ID[t.i]?.minW ?? 1, minH: TILE_DEFS_BY_ID[t.i]?.minH ?? 1, maxW: cols, maxH: 4 });
      x += t.w;
    }
    y += rowH;
  });
  return layout;
}
