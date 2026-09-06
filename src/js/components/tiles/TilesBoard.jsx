// TilesBoard —— 卡片式图块看板（可嵌入正式仪表盘或独立 demo 页）。
// 消费 src/js/components/tiles/ 组件库：TileGrid 拖拽/档位缩放/响应式列数，TileFrame/TileChart/StatValue/MiniMeter/StatTileCard 卡片体系。
// 布局按移动端/桌面端分桶保存到后端用户设置（data.db，云端）；顶栏提供时间范围/增删指标/刷新/重置。
import React, { useCallback, useContext, useEffect, useMemo, useRef, useState } from 'react';
import { createPortal } from 'react-dom';
import { Button, ChartPalette, DropdownMenu, SkeletonLine, Switch } from '@cloudflare/kumo';
import { Select } from '@cloudflare/kumo/components/select';
import { SquaresFour } from '@phosphor-icons/react';
import { TimeRangePicker } from '../ui/TimeRangePicker.jsx';
import {
  TileGrid,
  TileFrame,
  TileChart,
  StatValue,
} from './index.js';
import useStore from '../../store.js';
import { HeaderToolsContext } from '../../modules/headerToolsContext.js';
import { PublicPageBrandIcon } from '../public/PublicPageIconPicker.jsx';
import { formatTokensAxis, formatTokensZh } from '../../pages/openai/utils.js';
import {
  Cloud,
  Globe,
  FolderOpen,
  Shield,
  Clock,
  Activity,
  ArrowRight,
  KoyebBrand,
  FlyIoBrand,
} from '../Icons.jsx';

const FETCH_TIMEOUT_MS = 8000;
const HOST_POLL_MS = 5000;
// 仪表盘时间粒度持久化：切换后刷新/重访仍保持上次选择的时段
const RANGE_STORAGE_KEY = 'tileboard_time_range';

const loadRangeFromStorage = () => {
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

function cacheGet(key) {
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

function cacheSet(key, value, ttlMs) {
  const expiresAt = Date.now() + ttlMs;
  memoryCache.set(key, { value, expiresAt });
  try {
    localStorage.setItem(`tileboard_cache_${key}`, JSON.stringify({ value, expiresAt }));
  } catch {
    /* ignore */
  }
}

const CACHE_TTL = {
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
  const MIN_COLS = 2;
  const MAX_COLS = 8;

const TILE_DEFS = [
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
const TILE_DEFS_BY_ID = Object.fromEntries(TILE_DEFS.map((d) => [d.id, d]));

// 模块间跳转：与侧边栏行为一致（切换激活 tab + 同步地址栏），保证刷新后仍停留在目标模块。
const navigateModule = (module, query) => {
  useStore.getState().setMainActiveTab(module);
  const nextPath = query ? `/${module}?${new URLSearchParams(query).toString()}` : `/${module}`;
  if (window.location.pathname + window.location.search !== nextPath) {
    window.history.pushState({ module }, '', nextPath);
  }
};

// 状态页公开路由：uptime → /status/slug、server → /s/slug、github → /gh/slug
const statusPageHref = (p) => {
  if (!p?.slug) return null;
  const prefix = p.kind === 'server' ? '/s' : p.kind === 'github' ? '/gh' : '/status';
  return `${prefix}/${encodeURIComponent(p.slug)}`;
};

// TileEntry —— 所有卡片内条目行的统一样式：等宽圆角边框 + 悬停高亮。
// leading=行首图标/状态点，name=主文本（截断），desc=第二行说明（模块入口），badge=名称旁小标记，
// trailing=行尾徽标/箭头；onClick 渲染为按钮，href 渲染为外链（新标签页），两者都无则为静态行。
function TileEntry({ leading, name, desc, badge, trailing, onClick, href, title, pad = 'py-1', className = '' }) {
  const base = `animate-tile-fade-up flex min-w-0 items-center gap-1.5 rounded-md border border-kumo-line/60 px-1.5 ${pad} text-left transition-colors hover:border-brand/60 hover:bg-kumo-tint ${className}`;
  const body = (
    <>
      {leading}
      {desc ? (
        <span className="flex min-w-0 flex-1 flex-col">
          <span className="flex min-w-0 items-center gap-1">
            <span className="truncate text-[10px] text-kumo-default">{name}</span>
            {badge}
          </span>
          <span className="truncate text-[9px] text-kumo-subtle">{desc}</span>
        </span>
      ) : (
        <>
          <span className="min-w-0 flex-1 truncate text-[10px] text-kumo-default">{name}</span>
          {badge}
        </>
      )}
      {trailing}
    </>
  );
  if (href) {
    return (
      <a href={href} target="_blank" rel="noreferrer" title={title || name} className={base}>
        {body}
      </a>
    );
  }
  if (onClick) {
    return (
      <button
        type="button"
        onClick={onClick}
        title={title || name}
        className={`animate-tile-fade-up flex min-w-0 items-center gap-1.5 rounded-md border border-kumo-line/60 px-1.5 ${pad} text-left transition-colors hover:border-brand/60 hover:bg-kumo-tint ${className}`}
      >
        {body}
      </button>
    );
  }
  return (
    <div title={title || name} className={base}>
      {body}
    </div>
  );
}

// TileSkeleton —— 卡片加载骨架：按卡片最终形态组合 SkeletonLine（数值行 + 图表块 / 进度条 / 条目行），
// 加载中替代「加载中…」文字占位，保持各卡内容区域的轮廓稳定。
function TileSkeleton({ variant = 'chart', rows = 0, className = '' }) {
  return (
    <div className={`flex h-full min-h-0 flex-col gap-2 overflow-hidden px-4 pb-2 pt-1 ${className}`} aria-hidden="true">
      <div className="flex shrink-0 items-baseline gap-2">
        <SkeletonLine className="h-5 w-14" />
        <SkeletonLine className="h-3.5 w-24" />
      </div>
      {variant === 'bars' && (
        <div className="flex shrink-0 flex-col gap-1.5">
          <SkeletonLine className="h-3.5 w-full" />
          <SkeletonLine className="h-3.5 w-full" />
          <SkeletonLine className="h-3.5 w-full" />
        </div>
      )}
      {variant === 'list' ? (
        <div className="flex min-h-0 flex-1 flex-col gap-1.5">
          {Array.from({ length: rows }).map((_, i) => (
            <SkeletonLine key={i} className="h-7 w-full shrink-0" />
          ))}
        </div>
      ) : (
        <div className="flex min-h-0 flex-1 items-stretch pb-1">
          <SkeletonLine className="h-full w-full" />
        </div>
      )}
    </div>
  );
}

function fetchWithTimeout(url, options = {}, timeoutMs = FETCH_TIMEOUT_MS) {
  const controller = new AbortController();
  const timer = window.setTimeout(() => controller.abort(), timeoutMs);
  return fetch(url, {
    ...options,
    headers: { 'Content-Type': 'application/json', ...(options.headers || {}) },
    signal: controller.signal,
  }).finally(() => window.clearTimeout(timer));
}

function toArray(value) {
  if (Array.isArray(value)) return value;
  if (value && Array.isArray(value.data)) return value.data;
  return [];
}

function fmtCompact(value) {
  const n = Number(value);
  if (!Number.isFinite(n)) return '-';
  if (Math.abs(n) >= 1e9) return `${(n / 1e9).toFixed(2)}B`;
  if (Math.abs(n) >= 1e6) return `${(n / 1e6).toFixed(2)}M`;
  if (Math.abs(n) >= 1e3) return `${(n / 1e3).toFixed(2)}k`;
  return String(Math.round(n));
}

function fmtPercent(value) {
  const n = Number(value);
  if (!Number.isFinite(n)) return '-';
  return `${n.toFixed(n < 1 ? 2 : 1)}%`;
}

function fmtMs(value) {
  const n = Number(value);
  if (!Number.isFinite(n)) return '-';
  return `${Math.round(n)} ms`;
}

function fmtBytes(bytes) {
  const n = Number(bytes);
  if (!Number.isFinite(n)) return '-';
  if (n >= 1e9) return `${(n / 1e9).toFixed(1)} GB`;
  if (n >= 1e6) return `${(n / 1e6).toFixed(1)} MB`;
  if (n >= 1e3) return `${(n / 1e3).toFixed(1)} KB`;
  return `${n} B`;
}

function shortDay(value) {
  const s = String(value || '');
  return /^\d{4}-/.test(s) ? s.slice(5) : s;
}

function pctDelta(values) {
  const list = values.filter((v) => Number.isFinite(Number(v)));
  if (list.length < 2 || Number(list[0]) === 0) return null;
  return ((Number(list[list.length - 1]) - Number(list[0])) / Math.abs(Number(list[0]))) * 100;
}

function seriesSummary(values) {
  const nums = (values || []).filter((v) => Number.isFinite(Number(v)) && Number(v) > 0);
  if (!nums.length) return null;
  const max = Math.max(...nums);
  const avg = nums.reduce((a, b) => a + Number(b), 0) / nums.length;
  return { max, avg };
}

function normalizeServerStatus(status) {
  if (status === 'online') return 'online';
  if (status === 'error' || status === 'interrupted' || status === 'suspect') return 'error';
  return 'offline';
}

function tileDensity(w, h) {
  if (h <= 1) return 'half';
  if (h >= 4) return 'rich';
  if (w >= 2) return 'full';
  return 'compact';
}

// 生成铺满当前列数的默认布局（全部 2 行高，行打包 + 末行加宽填满，任意列数协调无空白）。
// 半高（1 行）不作为默认，而是保留在缩放档位中：拖拽柄可随时把任意卡缩到半高。
function packDefaultLayout(cols) {
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

// —— 图块内容（纯展示组件）——

// 宽度分档：拉宽时信息密度递增，而不是单纯拉伸留白。
// narrow（1 列）：主值 + 环比；medium（2~3 列）：追加 日均/峰值；wide（≥4 列）：再加 今日 等维度。
function widthTier(w) {
  return w >= 4 ? 'wide' : w >= 2 ? 'medium' : 'narrow';
}

// 数值统计条：大数值 + 环比 + 按宽度档递增的附加统计项。
// half 档 1 列时垂直居中排列（避免窄卡横向溢出），其余横向 baseline 对齐。
function ValueStatBar({ value, delta, items = [], half = false, tier = 'narrow' }) {
  const vertical = half && tier === 'narrow';
  return (
    <div className={vertical ? 'flex flex-col items-center gap-0.5' : 'flex min-w-0 flex-wrap items-baseline gap-x-4 gap-y-0.5'}>
      <StatValue value={value} delta={delta} />
      {items.map((it, i) => (
        <span key={i} className="animate-tile-fade-up shrink-0 whitespace-nowrap text-[10px] text-kumo-subtle tabular-nums">
          {it.label} {it.value}
        </span>
      ))}
    </div>
  );
}

// half 档内容布局（对齐 Cloudflare 官方半高卡形态）：
// narrow（1 列）：作为 1×2 的高度压缩版 = 数值行 + 压缩高度缩略图（无特殊三行样式）；
// medium（2 列）：左侧数据 + 右侧 mini 缩略趋势图；
// wide（≥4 列）：数值行 + 分数据同行，底部贴边矮图。
function HalfTile({ tier, stat, footnote, spark, isDarkMode }) {
  if (tier === 'narrow') {
    return (
      <div className="flex h-full min-h-0 flex-col overflow-hidden px-4 pb-1.5 pt-1">
        <div className="animate-tile-fade-up shrink-0">{stat}</div>
        {spark && (
          <div className="animate-tile-fade-up -mx-4 -mb-1.5 mt-0.5 min-h-0 flex-1 overflow-hidden">{spark}</div>
        )}
      </div>
    );
  }
  if (tier === 'medium') {
    return (
      <div className="flex h-full min-h-0 items-stretch gap-3 px-4 pb-1.5 pt-1">
        <div className="flex min-w-0 flex-1 flex-col justify-center">
          <div className="animate-tile-fade-up shrink-0">{stat}</div>
          {footnote && (
            <div className="animate-tile-fade-up mt-0.5 shrink-0 truncate text-[10px] text-kumo-subtle tabular-nums">{footnote}</div>
          )}
        </div>
        {spark && <div className="animate-tile-fade-up w-1/3 shrink-0 overflow-hidden">{spark}</div>}
      </div>
    );
  }
  return (
    <div className="flex h-full min-h-0 items-stretch gap-3 px-4 pb-1 pt-1">
      <div className="flex min-w-0 flex-1 flex-col justify-center">
        <div className="animate-tile-fade-up shrink-0">{stat}</div>
        {footnote && (
          <div className="animate-tile-fade-up mt-0.5 shrink-0 truncate text-[10px] text-kumo-subtle tabular-nums">{footnote}</div>
        )}
      </div>
      {spark && <div className="animate-tile-fade-up -mr-4 -mb-1 w-1/2 shrink-0 overflow-hidden">{spark}</div>}
    </div>
  );
}

// API 调用趋势（多系列）：与 OpenAI 延迟卡同款结构——half 走 HalfTile 三档（左数据右缩略图），
// 非 half 走「数值行（总请求 + 词元 + 三系列色点图例）+ 整宽出血图」。
// 已合并原「API 请求趋势」卡（请求次数 = 读取 + 变更）。
function ApiTrendMultiTile({ data, loading, isDarkMode, density = 'full', w = 1 }) {
  const trend = useMemo(() => (Array.isArray(data?.trend) ? data.trend : []), [data]);
  const categories = useMemo(() => trend.map((p) => shortDay(p.bucket)), [trend]);
  const [isolated, setIsolated] = useState(null);
  const configs = useMemo(() => [
    {
      key: 'requests',
      name: '请求次数',
      color: ChartPalette.categorical(0, isDarkMode),
      pick: (p) => (Number(p.audit) || 0) + (Number(p.ops) || 0),
    },
    {
      key: 'tokens',
      name: '词元用量',
      color: ChartPalette.categorical(1, isDarkMode),
      pick: (p) => Number(p.tokens) || 0,
    },
    {
      key: 'traffic',
      name: '订阅流量',
      color: ChartPalette.categorical(2, isDarkMode),
      pick: (p) => Number(p.traffic) || 0,
    },
  ], [isDarkMode]);
  const series = useMemo(() => configs.map((c) => {
    const raw = trend.map(c.pick);
    const max = Math.max(...raw, 1);
    return {
      ...c,
      max,
      raw,
      data: raw.map((v) => ({ value: (v / max) * 100, raw: v })),
      total: raw.reduce((a, b) => a + b, 0),
    };
  }), [configs, trend]);
  const totalRequests = series[0]?.total ?? 0;
  const tokensTotal = series[1]?.total ?? 0;
  const trafficTotal = series[2]?.total ?? 0;
  const isHalf = density === 'half';
  const tier = widthTier(w);
  const visible = isolated ? series.filter((s) => s.key === isolated) : series;
  const showLegend = tier !== 'narrow' && !isHalf;
  const summaryItems = useMemo(() => {
    if (tier !== 'wide' && density !== 'rich') return [];
    const req = series[0];
    if (!req || !trend.length) return [];
    return [
      { label: '请求日均', value: fmtCompact(req.total / trend.length) },
      { label: '请求峰值', value: fmtCompact(Math.max(...req.raw, 0)) },
    ];
  }, [tier, density, series, trend.length]);

  if (loading) return <TileSkeleton variant="chart" />;
  if (!categories.length) return <div className="flex h-full items-center justify-center text-xs text-kumo-inactive">无数据</div>;

  const chart = (chartDensity) => (
    <TileChart
      series={visible.map((s) => ({ name: s.name, color: s.color, data: s.data }))}
      categories={categories}
      isDarkMode={isDarkMode}
      density={chartDensity}
      yMin={0}
      yMax={100}
      yInterval={25}
      tooltipValueFormat={(v) => {
        const raw = v && typeof v === 'object' ? v.raw : v;
        return fmtCompact(Number(raw) || 0);
      }}
    />
  );

  const halfStat = (
    <div className="flex min-w-0 items-baseline gap-2.5">
      <span className="text-xl font-semibold leading-tight text-kumo-default tabular-nums">{fmtCompact(totalRequests)}</span>
      {tier !== 'narrow' && (
        <span className="text-sm font-medium text-kumo-default/80 tabular-nums">{formatTokensZh(tokensTotal)}</span>
      )}
    </div>
  );
  const halfSpark = isHalf ? chart('compact') : null;

  return (
    <div className={`flex h-full min-h-0 flex-col overflow-hidden ${isHalf ? '' : 'px-4 pt-1'}`}>
      {isHalf ? (
        <HalfTile
          tier={tier}
          stat={halfStat}
          footnote={tier === 'narrow' ? `词元 ${formatTokensZh(tokensTotal)} · 流量 ${fmtBytes(trafficTotal)}` : `流量 ${fmtBytes(trafficTotal)}`}
          spark={halfSpark}
          isDarkMode={isDarkMode}
        />
      ) : (
        <>
          <div className="shrink-0">
            <div className="flex min-w-0 flex-wrap items-baseline gap-x-3 gap-y-1">
              <span className="text-2xl font-semibold leading-tight text-kumo-default tabular-nums">{fmtCompact(totalRequests)}</span>
              {tier !== 'narrow' && (
                <span className="text-base font-medium text-kumo-default/80 tabular-nums">{formatTokensZh(tokensTotal)}</span>
              )}
              {showLegend && series.map((s) => (
                <Button
                  key={s.key}
                  type="button"
                  size="xs"
                  variant="ghost"
                  onClick={() => setIsolated((prev) => (prev === s.key ? null : s.key))}
                  title={isolated === s.key ? '取消隔离' : `只看 ${s.name}`}
                  className={`inline-flex min-w-0 items-center gap-1 text-[10px] text-kumo-subtle transition-opacity ${
                    isolated !== null && isolated !== s.key ? 'opacity-40 hover:opacity-70' : ''
                  }`}
                >
                  <span className="h-2 w-2 shrink-0 rounded-full" style={{ background: s.color }} />
                  <span className="shrink-0">{s.name}</span>
                  <span className="shrink-0 tabular-nums text-kumo-default/80">
                    {s.key === 'traffic' ? fmtBytes(s.total) : s.key === 'tokens' ? formatTokensZh(s.total) : fmtCompact(s.total)}
                  </span>
                </Button>
              ))}
              {summaryItems.map((it, i) => (
                <span key={i} className="shrink-0 whitespace-nowrap text-[10px] text-kumo-subtle tabular-nums">
                  {it.label} {it.value}
                </span>
              ))}
            </div>
          </div>
          <div className="animate-tile-fade-up -mx-4 mt-1.5 min-h-0 flex-1 overflow-hidden">
            {chart(tier === 'narrow' ? 'compact' : 'full')}
          </div>
        </>
      )}
    </div>
  );
}

function ApiTokensTile({ data, loading, isDarkMode, density = 'full', w = 1 }) {
  const trend = useMemo(() => (Array.isArray(data?.trend) ? data.trend : []), [data]);
  const categories = useMemo(() => trend.map((p) => shortDay(p.bucket)), [trend]);
  const values = useMemo(() => trend.map((p) => p.tokens ?? 0), [trend]);
  const delta = useMemo(() => pctDelta(values), [values]);
  const summary = useMemo(() => seriesSummary(values), [values]);
  const color = useMemo(() => ChartPalette.categorical(1, isDarkMode), [isDarkMode]);

  const isHalf = density === 'half';
  const tier = widthTier(w);
  const items = useMemo(() => {
    if (tier === 'narrow') return [];
    const list = [];
    if (summary) list.push({ label: '日均', value: formatTokensZh(summary.avg) });
    if (summary) list.push({ label: '峰值', value: formatTokensZh(summary.max) });
    if (tier === 'wide') {
      const lastVal = values[values.length - 1];
      if (lastVal != null) list.push({ label: '今日', value: formatTokensZh(lastVal) });
    }
    return list;
  }, [tier, summary, values]);
  const footnote = useMemo(() => (summary ? `日均 ${formatTokensZh(summary.avg)} · 峰值 ${formatTokensZh(summary.max)}` : null), [summary]);
  const spark = isHalf ? (
    <TileChart
      series={[{ name: '词元用量', color, data: values }]}
      categories={categories}
      isDarkMode={isDarkMode}
      density="compact"
      tooltipValueFormat={(v) => formatTokensZh(v)}
    />
  ) : null;

  if (loading) return <TileSkeleton variant="chart" />;
  if (!categories.length) return <div className="flex h-full items-center justify-center text-xs text-kumo-inactive">无数据</div>;

  return (
    <div className={`flex h-full min-h-0 flex-col overflow-hidden ${isHalf ? '' : 'px-4 pt-1'}`}>
      {isHalf ? (
        <HalfTile
          tier={tier}
          stat={<StatValue value={formatTokensZh(data?.tokens ?? 0)} delta={delta} />}
          footnote={footnote}
          spark={spark}
          isDarkMode={isDarkMode}
        />
      ) : (
        <>
          <div className="shrink-0">
            <ValueStatBar value={formatTokensZh(data?.tokens ?? 0)} delta={delta} items={items} tier={tier} />
          </div>
          <div className="animate-tile-fade-up -mx-4 mt-1.5 min-h-0 flex-1 overflow-hidden">
            <TileChart
              series={[{ name: '词元用量', color, data: values }]}
              categories={categories}
              isDarkMode={isDarkMode}
              density={density}
              yAxisTickFormat={formatTokensAxis}
              tooltipValueFormat={(v) => formatTokensZh(v)}
            />
          </div>
        </>
      )}
    </div>
  );
}

function OpenaiRequestsTile({ data, loading, isDarkMode, density = 'full', w = 1 }) {
  const daily = useMemo(() => (Array.isArray(data?.daily) ? data.daily : []), [data]);
  const categories = useMemo(() => daily.map((p) => shortDay(p.day)), [daily]);
  const counts = useMemo(() => daily.map((p) => p.count ?? 0), [daily]);
  const total = useMemo(() => counts.reduce((a, b) => a + b, 0), [counts]);
  const delta = useMemo(() => pctDelta(counts), [counts]);
  const summary = useMemo(() => seriesSummary(counts), [counts]);
  const color = useMemo(() => ChartPalette.categorical(2, isDarkMode), [isDarkMode]);

  const isHalf = density === 'half';
  const tier = widthTier(w);
  const items = useMemo(() => {
    if (tier === 'narrow') return [];
    const list = [];
    if (summary) list.push({ label: '日均', value: fmtCompact(summary.avg) });
    if (summary) list.push({ label: '峰值', value: fmtCompact(summary.max) });
    if (tier === 'wide') {
      const lastVal = counts[counts.length - 1];
      if (lastVal != null) list.push({ label: '今日', value: fmtCompact(lastVal) });
    }
    return list;
  }, [tier, summary, counts]);
  const footnote = useMemo(() => (summary ? `日均 ${fmtCompact(summary.avg)} · 峰值 ${fmtCompact(summary.max)}` : null), [summary]);
  const spark = isHalf ? (
    <TileChart
      series={[{ name: '请求量', color, data: counts }]}
      categories={categories}
      isDarkMode={isDarkMode}
      density="compact"
      tooltipValueFormat={(v) => `${fmtCompact(v)} 次`}
    />
  ) : null;

  if (loading) return <TileSkeleton variant="chart" />;
  if (!categories.length) return <div className="flex h-full items-center justify-center text-xs text-kumo-inactive">无数据</div>;

  return (
    <div className={`flex h-full min-h-0 flex-col overflow-hidden ${isHalf ? '' : 'px-4 pt-1'}`}>
      {isHalf ? (
        <HalfTile
          tier={tier}
          stat={<StatValue value={fmtCompact(total)} delta={delta} />}
          footnote={footnote}
          spark={spark}
          isDarkMode={isDarkMode}
        />
      ) : (
        <>
          <div className="shrink-0">
            <ValueStatBar value={fmtCompact(total)} delta={delta} items={items} tier={tier} />
          </div>
          <div className="animate-tile-fade-up -mx-4 mt-1.5 min-h-0 flex-1 overflow-hidden">
            <TileChart
              series={[{ name: '请求量', color, data: counts }]}
              categories={categories}
              isDarkMode={isDarkMode}
              density={density}
              tooltipValueFormat={(v) => `${fmtCompact(v)} 次`}
            />
          </div>
        </>
      )}
    </div>
  );
}

function OpenaiLatencyTile({ data, loading, isDarkMode, density = 'full', w = 1 }) {
  const daily = useMemo(() => (Array.isArray(data?.daily) ? data.daily : []), [data]);
  const categories = useMemo(() => daily.map((p) => shortDay(p.day)), [daily]);
  const latValues = useMemo(() => daily.map((p) => p.avgLatency ?? 0), [daily]);
  const ttfbValues = useMemo(() => daily.map((p) => p.avgTtfbMs ?? 0), [daily]);
  const latColor = useMemo(() => ChartPalette.categorical(3, isDarkMode), [isDarkMode]);
  const ttfbColor = useMemo(() => ChartPalette.categorical(4, isDarkMode), [isDarkMode]);
  const latest = daily[daily.length - 1];
  const summary = useMemo(() => seriesSummary([...latValues, ...ttfbValues]), [latValues, ttfbValues]);

  const isHalf = density === 'half';
  const tier = widthTier(w);
  const showLegend = tier !== 'narrow' && !isHalf;
  const summaryItems = useMemo(() => {
    if (!summary || (tier !== 'wide' && density !== 'rich')) return [];
    return [
      { label: '延迟日均', value: fmtMs(summary.avg) },
      { label: '延迟峰值', value: fmtMs(summary.max) },
    ];
  }, [summary, tier, density]);

  if (loading) return <TileSkeleton variant="chart" />;
  if (!categories.length) return <div className="flex h-full items-center justify-center text-xs text-kumo-inactive">无数据</div>;

  const halfStat = (
    <div className="flex min-w-0 items-baseline gap-2.5">
      <span className="text-xl font-semibold leading-tight text-kumo-default tabular-nums">{fmtMs(latest?.avgLatency)}</span>
      {tier !== 'narrow' && (
        <span className="text-sm font-medium text-kumo-default/80 tabular-nums">{fmtMs(latest?.avgTtfbMs)}</span>
      )}
    </div>
  );
  const halfSpark = isHalf ? (
    <TileChart
      series={[
        { name: '延迟', color: latColor, data: latValues },
        { name: 'TTFB', color: ttfbColor, data: ttfbValues },
      ]}
      categories={categories}
      isDarkMode={isDarkMode}
      density="compact"
      yAxisTickFormat={(v) => `${(Number(v) / 1000).toFixed(1)}s`}
      tooltipValueFormat={(v) => fmtMs(v)}
    />
  ) : null;

  return (
    <div className={`flex h-full min-h-0 flex-col overflow-hidden ${isHalf ? '' : 'px-4 pt-1'}`}>
      {isHalf ? (
        <HalfTile
          tier={tier}
          stat={halfStat}
          footnote={tier === 'narrow' ? `TTFB ${fmtMs(latest?.avgTtfbMs)}` : null}
          spark={halfSpark}
          isDarkMode={isDarkMode}
        />
      ) : (
        <>
          <div className="shrink-0">
            <div className="flex min-w-0 flex-wrap items-baseline gap-x-3 gap-y-1">
              <span className="text-2xl font-semibold leading-tight text-kumo-default tabular-nums">{fmtMs(latest?.avgLatency)}</span>
              {tier !== 'narrow' && <span className="text-base font-medium text-kumo-default/80 tabular-nums">{fmtMs(latest?.avgTtfbMs)}</span>}
              {showLegend && (
                <>
                  <span className="animate-tile-fade-up inline-flex items-center gap-1 text-[10px] text-kumo-subtle">
                    <span className="h-2 w-2 rounded-full" style={{ background: latColor }} />延迟
                  </span>
                  <span className="animate-tile-fade-up inline-flex items-center gap-1 text-[10px] text-kumo-subtle">
                    <span className="h-2 w-2 rounded-full" style={{ background: ttfbColor }} />TTFB
                  </span>
                </>
              )}
              {summaryItems.map((it, i) => (
                <span key={i} className="animate-tile-fade-up shrink-0 whitespace-nowrap text-[10px] text-kumo-subtle tabular-nums">
                  {it.label} {it.value}
                </span>
              ))}
            </div>
          </div>
          <div className="animate-tile-fade-up -mx-4 mt-1.5 min-h-0 flex-1 overflow-hidden">
            <TileChart
              series={[
                { name: '延迟', color: latColor, data: latValues },
                { name: 'TTFB', color: ttfbColor, data: ttfbValues },
              ]}
              categories={categories}
              isDarkMode={isDarkMode}
              density={density}
              yAxisTickFormat={(v) => `${(Number(v) / 1000).toFixed(1)}s`}
              tooltipValueFormat={(v) => fmtMs(v)}
            />
          </div>
        </>
      )}
    </div>
  );
}

function OpenaiErrorsTile({ data, loading, isDarkMode, density = 'full', w = 1 }) {
  const daily = useMemo(() => (Array.isArray(data?.daily) ? data.daily : []), [data]);
  const categories = useMemo(() => daily.map((p) => shortDay(p.day)), [daily]);
  const errors = useMemo(() => daily.map((p) => p.errors ?? 0), [daily]);
  const total = useMemo(() => errors.reduce((a, b) => a + b, 0), [errors]);
  const delta = useMemo(() => pctDelta(errors), [errors]);
  const summary = useMemo(() => seriesSummary(errors), [errors]);
  const color = useMemo(() => ChartPalette.semantic('Attention', isDarkMode), [isDarkMode]);

  const isHalf = density === 'half';
  const tier = widthTier(w);
  const items = useMemo(() => {
    if (tier === 'narrow') return [];
    const list = [];
    if (summary) list.push({ label: '日均', value: fmtCompact(summary.avg) });
    if (summary) list.push({ label: '峰值', value: fmtCompact(summary.max) });
    if (tier === 'wide') {
      const lastVal = errors[errors.length - 1];
      if (lastVal != null) list.push({ label: '今日', value: fmtCompact(lastVal) });
    }
    return list;
  }, [tier, summary, errors]);
  const footnote = useMemo(() => (summary ? `日均 ${fmtCompact(summary.avg)} · 峰值 ${fmtCompact(summary.max)}` : null), [summary]);
  const spark = isHalf ? (
    <TileChart
      series={[{ name: '错误', color, data: errors }]}
      categories={categories}
      type="bar"
      isDarkMode={isDarkMode}
      density="compact"
      tooltipValueFormat={(v) => `${fmtCompact(v)} 次`}
    />
  ) : null;

  if (loading) return <TileSkeleton variant="chart" />;
  if (!categories.length) return <div className="flex h-full items-center justify-center text-xs text-kumo-inactive">无数据</div>;

  return (
    <div className={`flex h-full min-h-0 flex-col overflow-hidden ${isHalf ? '' : 'px-4 pt-1'}`}>
      {isHalf ? (
        <HalfTile
          tier={tier}
          stat={<StatValue value={fmtCompact(total)} delta={delta} />}
          footnote={footnote}
          spark={spark}
          isDarkMode={isDarkMode}
        />
      ) : (
        <>
          <div className="shrink-0">
            <ValueStatBar value={fmtCompact(total)} delta={delta} items={items} tier={tier} />
          </div>
          <div className="animate-tile-fade-up -mx-4 mt-1.5 min-h-0 flex-1 overflow-hidden">
            <TileChart
              series={[{ name: '错误', color, data: errors }]}
              categories={categories}
              type="bar"
              isDarkMode={isDarkMode}
              density={density}
              tooltipValueFormat={(v) => `${fmtCompact(v)} 次`}
            />
          </div>
        </>
      )}
    </div>
  );
}

// 主机性能：CPU/内存/磁盘 三进度条（hover 显示具体值）。半高 = 三行进度条（无右图）；
// 非半高 = CPU 实时数值 + 三进度条 + CPU 采样图（底部出血）。
function HostCpuTile({ data, isDarkMode, density = 'full', w = 1 }) {
  const samples = useMemo(() => (Array.isArray(data?.samples) ? data.samples : []), [data]);
  const memSamples = useMemo(() => (Array.isArray(data?.memSamples) ? data.memSamples : []), [data]);
  const diskSamples = useMemo(() => (Array.isArray(data?.diskSamples) ? data.diskSamples : []), [data]);
  const gpuSamples = useMemo(() => (Array.isArray(data?.gpuSamples) ? data.gpuSamples : []), [data]);
  const current = data?.cpu?.usage;
  const isHalf = density === 'half';
  const tier = widthTier(w);
  const mem = data?.memory;
  const disk = data?.disk;
  const load1 = data?.cpu?.loadAverage?.[0];
  const cores = data?.cpu?.cores;
  // 1 分钟滚动窗口多折线：CPU / 内存 / 磁盘 / GPU（缺数据序列自动剔除）
  const perfSeries = useMemo(() => [
    { name: 'CPU', color: ChartPalette.categorical(5, isDarkMode), data: samples },
    { name: '内存', color: ChartPalette.categorical(1, isDarkMode), data: memSamples },
    { name: '磁盘', color: ChartPalette.categorical(2, isDarkMode), data: diskSamples },
    ...(gpuSamples.length ? [{ name: 'GPU', color: ChartPalette.categorical(3, isDarkMode), data: gpuSamples }] : []),
  ].filter((s) => s.data.length > 0), [samples, memSamples, diskSamples, gpuSamples, isDarkMode]);

  if (current == null) return <TileSkeleton variant={isHalf || tier === 'narrow' ? 'chart' : 'bars'} />;

  const bars = [
    {
      label: 'CPU',
      usage: Number(current) || 0,
      detail: [cores ? `${cores} 核` : '', load1 != null ? `负载 ${Number(load1).toFixed(2)}` : ''].filter(Boolean).join(' · ') || '—',
      tone: 'success',
    },
    {
      label: '内存',
      usage: Number(mem?.usage) || 0,
      detail: mem ? `${fmtBytes(mem.used)} / ${fmtBytes(mem.total)}` : '—',
      tone: 'info',
    },
    {
      label: '磁盘',
      usage: Number(disk?.usage) || 0,
      detail: disk ? `${fmtBytes(disk.used)} / ${fmtBytes(disk.total)}` : '—',
      tone: 'brand',
    },
  ];
  const barList = (
    <div className="flex min-w-0 flex-col gap-1">
      {bars.map((b) => (
        <CompactMeter
          key={b.label}
          label={b.label}
          usage={b.usage}
          tone={b.tone}
          detail={b.detail}
          title={b.detail}
          hideValue={isHalf && tier === 'narrow'}
        />
      ))}
    </div>
  );

  // 半高：全部用 1 分钟滚动多折线图（1×1 数值 + 压缩图；2×1/4×1 左数据右图）
  if (isHalf) {
    if (tier === 'narrow') {
      return (
        <div className="flex h-full min-h-0 flex-col overflow-hidden px-4 pb-1.5 pt-1">
          <div className="animate-tile-fade-up flex shrink-0 items-baseline gap-1.5">
            <span className="text-xl font-semibold leading-tight text-kumo-default tabular-nums">{current.toFixed(1)}%</span>
            <span className="min-w-0 truncate text-[10px] text-kumo-subtle">{data?.hostname || '未连接主机'}</span>
          </div>
          <div className="animate-tile-fade-up -mx-4 -mb-1.5 mt-0.5 min-h-0 flex-1 overflow-hidden">
            <TileChart
              series={perfSeries}
              showSymbol={false}
              categories={samples.map(() => '')}
              isDarkMode={isDarkMode}
              density="compact"
              yAxisTickFormat={(v) => `${v}%`}
              tooltipValueFormat={(v) => `${Number(v).toFixed(1)}%`}
            />
          </div>
        </div>
      );
    }
    return (
      <div className="flex h-full min-h-0 items-stretch gap-3 px-4 pb-1.5 pt-1">
        <div className="flex min-w-0 flex-1 flex-col justify-center">
          <div className="animate-tile-fade-up shrink-0">
            <div className="flex items-baseline gap-1.5">
              <span className="text-xl font-semibold leading-tight text-kumo-default tabular-nums">{current.toFixed(1)}%</span>
              <span className="text-[10px] text-kumo-subtle">CPU</span>
            </div>
            <div className="mt-0.5 truncate text-[10px] text-kumo-subtle">{data?.hostname || '未连接主机'}</div>
          </div>
        </div>
        <div className="animate-tile-fade-up -mr-4 -mb-1 w-1/2 shrink-0 overflow-hidden">
          <TileChart
            series={perfSeries}
              showSymbol={false}
            categories={samples.map(() => '')}
            isDarkMode={isDarkMode}
            density="compact"
            yAxisTickFormat={(v) => `${v}%`}
            tooltipValueFormat={(v) => `${Number(v).toFixed(1)}%`}
          />
        </div>
      </div>
    );
  }

  // 非半高：1×2（compact 窄卡）也用滚动多折线图（数值 + 缩略图，不含进度条）；
  // 2×2 及以上保留「顶部 CPU 数值 + 三进度条 + CPU 采样图」
  if (tier === 'narrow') {
    return (
      <div className="flex h-full min-h-0 flex-col overflow-hidden px-4 pt-1">
        <div className="animate-tile-fade-up flex shrink-0 items-baseline gap-1.5">
          <span className="text-2xl font-semibold leading-tight text-kumo-default tabular-nums">{current.toFixed(1)}%</span>
          <span className="min-w-0 truncate text-[10px] text-kumo-subtle">{data?.hostname || '未连接主机'}</span>
        </div>
        <div className="animate-tile-fade-up -mx-4 mt-1 min-h-0 flex-1 overflow-hidden">
          <TileChart
            series={perfSeries}
              showSymbol={false}
            categories={samples.map(() => '')}
            isDarkMode={isDarkMode}
            density="compact"
            yAxisTickFormat={(v) => `${v}%`}
            tooltipValueFormat={(v) => `${Number(v).toFixed(1)}%`}
          />
        </div>
      </div>
    );
  }
  return (
    <div className="flex h-full min-h-0 flex-col overflow-hidden px-4 pt-1">
      <div className="flex shrink-0 items-baseline gap-1.5">
        <span className="text-2xl font-semibold leading-tight text-kumo-default tabular-nums">{current.toFixed(1)}%</span>
        <span className="min-w-0 truncate text-[10px] text-kumo-subtle">{data?.hostname || '未连接主机'}</span>
      </div>
      <div className="mt-1 shrink-0">{barList}</div>
      <div className="animate-tile-fade-up -mx-4 mt-1 min-h-0 flex-1 overflow-hidden">
        <TileChart
          series={perfSeries}
              showSymbol={false}
          categories={samples.map(() => '')}
          isDarkMode={isDarkMode}
          density={density}
          yMin={0}
          yMax={100}
          yInterval={25}
          yAxisTickFormat={(v) => `${v}%`}
          tooltipValueFormat={(v) => `${Number(v).toFixed(1)}%`}
        />
      </div>
    </div>
  );
}

// 紧凑仪表行：label + 细进度条 + 值（完整显示不截断，hover 显示详情）。hideValue 时只留 label + 进度条。
function CompactMeter({ label, usage, used, total, tone = 'brand', detail, title, hideValue = false }) {
  const pct = Math.min(100, Math.max(0, Number(usage) || 0));
  const barColor = {
    brand: 'bg-brand',
    info: 'bg-kumo-info',
    success: 'bg-kumo-success',
    warning: 'bg-kumo-warning',
  }[tone] || 'bg-brand';
  return (
    <div className="animate-tile-fade-up flex min-w-0 items-center gap-2 text-[11px] text-kumo-subtle" title={title}>
      <span className="w-8 shrink-0 truncate">{label}</span>
      <div className="h-1.5 min-w-0 flex-1 overflow-hidden rounded-full border border-kumo-line/70 bg-kumo-recessed">
        <div className={`h-full rounded-full ${barColor}`} style={{ width: `${pct}%` }} />
      </div>
      {!hideValue && (
        <span className="w-36 shrink-0 truncate text-left tabular-nums text-kumo-default/80">{detail || `${pct.toFixed(0)}%`}</span>
      )}
    </div>
  );
}

// 服务器状态卡（与监控可用率同款样式）：数值 + 可用率条 + 服务器条目列表。
// 1×1 不显示条目；半高宽（2×1/4×1）左侧数据 + 右侧条目；全高条目在下方，溢出可滚动。
function ServerStatusTile({ servers, density = 'full', w = 1 }) {
  const tier = widthTier(w);
  const isHalf = density === 'half';
  const total = servers?.total ?? 0;
  const online = servers?.online ?? 0;
  const error = servers?.error ?? 0;
  const offline = Math.max(0, total - online - error);
  const list = Array.isArray(servers?.list) ? servers.list : [];
  const upPct = total ? (online / total) * 100 : 0;

  if (!servers) return <TileSkeleton variant="list" rows={4} />;
  if (!total) return <div className="flex h-full items-center justify-center text-xs text-kumo-inactive">暂无服务器</div>;

  const head = (
    <>
      <div className="flex items-baseline gap-2">
        <span className="text-2xl font-semibold leading-tight text-kumo-default tabular-nums">{online}/{total}</span>
        {density !== 'compact' && <span className="text-sm font-medium text-kumo-subtle">在线 · {offline} 离线</span>}
      </div>
      <div className="mt-1 flex h-2 w-full overflow-hidden rounded-full border border-kumo-line/70 bg-kumo-recessed">
        <div className="h-full rounded-full bg-kumo-success" style={{ width: `${upPct}%` }} />
      </div>
    </>
  );

  const entries = (cols) => (
    <div className="min-h-0 flex-1 overflow-y-auto tile-scroll">
      <div className="grid content-start gap-1.5" style={{ gridTemplateColumns: `repeat(${cols}, minmax(0, 1fr))` }}>
        {list.map((m, i) => (
          <TileEntry
            key={`${m.name || ''}-${i}`}
            name={m.name || '未命名'}
            title={m.name || m.host || ''}
            onClick={() => navigateModule('server')}
            leading={
              <span className={`h-1.5 w-1.5 shrink-0 rounded-full ${
                m.status === 'online' ? 'bg-kumo-success' : m.status === 'error' ? 'bg-kumo-danger' : 'bg-kumo-fill'
              }`} />
            }
          />
        ))}
      </div>
    </div>
  );

  if (isHalf && tier === 'narrow') {
    // 1×1：不显示条目，数值 + 可用率条居中
    return (
      <div className="flex h-full min-h-0 flex-col justify-center gap-1 overflow-hidden px-4 pb-1.5 pt-1">
        <div className="animate-tile-fade-up flex flex-col items-center gap-1">{head}</div>
      </div>
    );
  }
  if (isHalf) {
    // 2×1 / 4×1：左侧数据 + 右侧条目（wide 右侧两列）
    return (
      <div className="flex h-full min-h-0 items-stretch gap-3 px-4 pb-1.5 pt-1">
        <div className="flex min-w-0 flex-1 flex-col justify-center">
          <div className="animate-tile-fade-up shrink-0">{head}</div>
        </div>
        <div className={`${tier === 'wide' ? 'w-1/2' : 'w-1/3'} flex shrink-0 flex-col overflow-hidden`}>
          {entries(tier === 'wide' ? 2 : 1)}
        </div>
      </div>
    );
  }
  // 全高：数值 + 可用率条 + 下方条目列表（多列）
  const fullCols = tier === 'wide' ? (density === 'rich' ? 3 : 4) : tier === 'medium' ? 2 : 1;
  return (
    <div className="flex h-full min-h-0 flex-col gap-2 px-4 pb-1.5 pt-1">
      <div className="animate-tile-fade-up shrink-0">{head}</div>
      {density === 'rich' && (
        <div className="shrink-0 text-[10px] text-kumo-subtle">
          {error > 0 ? `${error} 台异常 · ${offline} 台离线` : '全部服务器在线'}
        </div>
      )}
      {entries(fullCols)}
    </div>
  );
}

// 小尺寸只显示名称+计数，full/wide/rich 追加一行描述与模块健康状态点。
function ModuleToolsTile({ dash, uptime, density = 'full', w = 1 }) {
  const items = [
    { key: 'paas', name: 'PaaS 实例', desc: 'Koyeb / Fly 应用', count: dash?.paas ? dash.paas.koyeb.total + dash.paas.fly.total : null, ok: dash?.paas ? (dash.paas.koyeb.running + dash.paas.fly.running) > 0 : null, icon: Cloud },
    { key: 'dns', name: '域名解析', desc: 'Cloudflare 区域', count: dash ? dash.dns.zones : null, ok: dash ? dash.dns.zones > 0 : null, icon: Globe },
    { key: 'uptime', name: '服务监控', desc: '监控与状态页', count: uptime ? uptime.total : null, ok: uptime ? uptime.up > 0 : null, icon: Activity },
    { key: 'scheduler', name: '定时任务', desc: '任务与工作流', count: dash ? dash.scheduler.total : null, ok: dash ? dash.scheduler.enabled > 0 : null, icon: Clock },
    { key: 'totp', name: '双因子认证', desc: 'OTP 动态码', count: dash ? dash.totp.total : null, ok: dash ? dash.totp.total > 0 : null, icon: Shield },
    { key: 'filebox', name: '文件分享柜', desc: '文件与片段', count: dash ? dash.filebox.total : null, ok: dash ? dash.filebox.total > 0 : null, icon: FolderOpen },
  ];
  const isHalf = density === 'half';
  const tier = widthTier(w);
  // 列数：w=1（移动端/单列宽）一律单列显示；2×1=3 列 / 4×1=6 列横幅 / 2×2=3 列 / 4×2=4 列 / 2×4=3 列
  const cols = tier === 'narrow'
    ? 1
    : (isHalf ? (tier === 'wide' ? 6 : 3) : (density === 'rich' ? 3 : tier === 'wide' ? 4 : 3));
  const showDesc = !isHalf && density !== 'compact'; // 1×2 窄卡只显示名称+计数
  const isTiny = isHalf && tier === 'narrow'; // 0.5×1：条目高度稍增、更易点击

  // 数据未就绪（dash/uptime 任一未加载）时显示骨架，避免计数占位「—」闪烁
  if (!dash?.paas || !uptime) return <TileSkeleton variant="list" rows={3} />;

  return (
    <div className="flex h-full min-h-0 flex-col overflow-hidden px-4 pb-1.5 pt-1">
      <div className="grid min-h-0 flex-1 auto-rows-min content-start gap-1.5 overflow-y-auto tile-scroll" style={{ gridTemplateColumns: `repeat(${cols}, minmax(0, 1fr))` }}>
        {items.map((it) => (
          <TileEntry
            key={it.key}
            name={it.name}
            desc={showDesc ? it.desc : undefined}
            title={`${it.name} · ${it.desc}`}
            onClick={() => navigateModule(it.key)}
            pad={isTiny ? 'py-1.5' : 'py-1'}
            leading={<it.icon className="h-3.5 w-3.5 shrink-0 text-kumo-subtle" />}
            badge={
              it.ok != null ? (
                <span className={`h-1 w-1 shrink-0 rounded-full ${it.ok ? 'bg-kumo-success' : 'bg-kumo-fill'}`} />
              ) : undefined
            }
            trailing={
              <span className="shrink-0 text-[10px] tabular-nums text-kumo-subtle">{it.count != null ? it.count : '—'}</span>
            }
          />
        ))}
      </div>
    </div>
  );
}

// 状态页：公开状态页列表（点击打开）。1×2 默认布局 = 单列稍大卡片（左图标 + 名称 + 打开箭头），
// 更宽尺寸切多列网格；最小高度 1 行。
function StatusPagesTile({ dash, density = 'full', w = 1 }) {
  const list = Array.isArray(dash?.statusPages?.list) ? dash.statusPages.list : [];
  const total = dash?.statusPages?.total ?? list.length;
  const isHalf = density === 'half';
  const tier = widthTier(w);

  if (!dash) return <TileSkeleton variant="list" rows={3} />;
  if (!list.length && !total) {
    return <div className="flex h-full items-center justify-center text-xs text-kumo-inactive">暂无状态页</div>;
  }
  if (!list.length) {
    return (
      <div className="flex h-full items-center justify-center text-xs text-kumo-inactive">共 {total} 个状态页</div>
    );
  }

  // 1 列（1×2 默认）= 单列大卡片；2/3 列 = 两/三列网格；宽半高/宽全高 = 更多列
  const cols = tier === 'narrow' ? 1 : isHalf ? (tier === 'wide' ? 4 : 3) : tier === 'wide' ? 4 : 3;
  const single = cols === 1;
  return (
    <div className="flex h-full min-h-0 flex-col overflow-hidden px-4 pb-1.5 pt-1">
      <div className={`grid content-start gap-1.5 ${single ? '' : 'auto-rows-fr'}`} style={{ gridTemplateColumns: `repeat(${cols}, minmax(0, 1fr))` }}>
        {list.map((p, i) => {
          const href = p.url || statusPageHref(p);
          return (
            <TileEntry
              key={`${p.kind}-${p.id || i}`}
              name={p.name || '未命名状态页'}
              href={href || undefined}
              title={p.name || href || ''}
              pad={single ? 'py-2' : 'py-1.5'}
              leading={
                <PublicPageBrandIcon
                  pageKind={p.kind}
                  config={p.config}
                  iconClassName={`shrink-0 ${single ? 'h-4 w-4' : 'h-3.5 w-3.5'}`}
                  customIconClassName={`shrink-0 ${single ? 'h-4 w-4' : 'h-3.5 w-3.5'}`}
                />
              }
              trailing={href ? <ArrowRight className={`shrink-0 text-kumo-inactive ${single ? 'h-3.5 w-3.5' : 'h-3 w-3'}`} /> : undefined}
            />
          );
        })}
      </div>
    </div>
  );
}

// 定时任务：任务条目列表（一列显示，右侧标注启用/停用状态），0.5×1 起显示具体条目。
function SchedulerTile({ dash, density = 'full', w = 1 }) {
  const list = Array.isArray(dash?.scheduler?.list) ? dash.scheduler.list : [];
  const total = dash?.scheduler?.total ?? 0;
  const enabled = dash?.scheduler?.enabled ?? 0;

  if (!dash) return <TileSkeleton variant="list" rows={4} />;
  if (!list.length) {
    return (
      <div className="flex h-full flex-col items-center justify-center gap-1 px-4 pb-1.5 pt-1">
        <span className="text-xl font-semibold leading-tight text-kumo-default tabular-nums">{enabled}/{total}</span>
        <span className="text-[10px] text-kumo-subtle">已启用的计划任务</span>
      </div>
    );
  }

  return (
    <div className="flex h-full min-h-0 flex-col overflow-hidden px-4 pb-1.5 pt-1">
      <div className="flex min-h-0 flex-col gap-1.5 overflow-y-auto tile-scroll">
        {list.map((t, i) => (
          <TileEntry
            key={`${t.name}-${i}`}
            name={t.name || '未命名任务'}
            title={t.name}
            onClick={() => navigateModule('scheduler')}
            trailing={
              <span className={`shrink-0 text-[10px] ${t.enabled ? 'text-kumo-success' : 'text-kumo-subtle'}`}>
                {t.enabled ? '启用' : '停用'}
              </span>
            }
          />
        ))}
      </div>
    </div>
  );
}

// PaaS 实例：条目列表（平台图标 Koyeb/Fly + 名称 + 运行状态点），条目溢出可滚动。
function PaasTile({ dash, density = 'full', w = 1 }) {
  const list = Array.isArray(dash?.paas?.list) ? dash.paas.list : [];
  const total = dash?.paas ? dash.paas.koyeb.total + dash.paas.fly.total : 0;
  const running = dash?.paas ? dash.paas.koyeb.running + dash.paas.fly.running : 0;
  const isHalf = density === 'half';
  const tier = widthTier(w);

  if (!dash || !dash.paas) return <TileSkeleton variant="list" rows={4} />;
  if (!list.length) {
    return (
      <div className="flex h-full flex-col items-center justify-center gap-1 px-4 pb-1.5 pt-1">
        <span className="text-xl font-semibold leading-tight text-kumo-default tabular-nums">{running}/{total}</span>
        <span className="text-[10px] text-kumo-subtle">运行中实例</span>
      </div>
    );
  }

  const cols = isHalf ? (tier === 'wide' ? 4 : 2) : tier === 'wide' ? 4 : 2;
  return (
    <div className="flex h-full min-h-0 flex-col overflow-hidden px-4 pb-1.5 pt-1">
      <div
        className="grid min-h-0 flex-1 content-start gap-1.5 overflow-y-auto tile-scroll"
        style={{ gridTemplateColumns: `repeat(${cols}, minmax(0, 1fr))` }}
      >
        {list.map((p, i) => {
          const PlatformIcon = p.kind === 'koyeb' ? KoyebBrand : FlyIoBrand;
          return (
            <TileEntry
              key={`${p.kind}-${p.name}-${i}`}
              name={p.name || '未命名实例'}
              title={p.name}
              onClick={() => navigateModule('paas')}
              leading={<PlatformIcon className="h-3.5 w-3.5 shrink-0 text-kumo-subtle" />}
              trailing={
                <span className={`h-1.5 w-1.5 shrink-0 rounded-full ${p.status === 'running' ? 'bg-kumo-success' : 'bg-kumo-fill'}`} />
              }
            />
          );
        })}
      </div>
    </div>
  );
}

function UptimeTile({ data, density = 'full', w = 1 }) {
  const total = data?.total ?? 0;
  const up = data?.up ?? 0;
  const down = total - up;
  const upPct = total ? (up / total) * 100 : 0;
  const isHalf = density === 'half';
  const tier = widthTier(w);
  const items = useMemo(() => (Array.isArray(data?.items) ? data.items : []), [data]);

  if (!data) return <TileSkeleton variant="list" rows={4} />;

  const head = (
    <>
      <div className="flex items-baseline gap-2">
        <span className="text-2xl font-semibold leading-tight text-kumo-default tabular-nums">{up}/{total}</span>
        {density !== 'compact' && <span className="text-sm font-medium text-kumo-subtle">在线 · {down} 离线</span>}
      </div>
      <div className="mt-1 flex h-2 w-full overflow-hidden rounded-full border border-kumo-line/70 bg-kumo-recessed">
        <div className="h-full rounded-full bg-kumo-success" style={{ width: `${upPct}%` }} />
      </div>
    </>
  );

  const entries = (
    <div className="min-h-0 flex-1 overflow-y-auto tile-scroll">
      <div className={`flex flex-col ${isHalf ? 'gap-1' : 'gap-1.5'}`}>
        {items.map((m, i) => {
          const ok = m.lastHeartbeat ? (m.lastHeartbeat.status === 1 || m.lastHeartbeat.status === 'up') : true;
          return (
            <TileEntry
              key={m.id ?? `${m.name ?? ''}-${i}`}
              name={m.name || m.id || ''}
              title={m.name || m.id || ''}
              onClick={() => navigateModule('uptime')}
              leading={
                <span className={`h-1.5 w-1.5 shrink-0 rounded-full ${ok ? 'bg-kumo-success' : 'bg-kumo-danger'}`} />
              }
            />
          );
        })}
      </div>
    </div>
  );

  if (isHalf && tier === 'narrow') {
    // 1×1：不显示条目，数值 + 进度条居中
    return (
      <div className="flex h-full min-h-0 flex-col justify-center gap-1 overflow-hidden px-4 pb-1.5 pt-1">
        <div className="animate-tile-fade-up flex flex-col items-center gap-1">{head}</div>
      </div>
    );
  }
  if (isHalf) {
    // 2×1 / 4×1：左侧数据 + 右侧条目
    return (
      <div className="flex h-full min-h-0 items-stretch gap-3 px-4 pb-1.5 pt-1">
        <div className="flex min-w-0 flex-1 flex-col justify-center">
          <div className="animate-tile-fade-up shrink-0">{head}</div>
        </div>
        <div className={`${tier === 'wide' ? 'w-1/2' : 'w-1/3'} flex shrink-0 flex-col overflow-hidden`}>{entries}</div>
      </div>
    );
  }
  // 全高：数值 + 进度条 + 下方条目列表
  return (
    <div className="flex h-full min-h-0 flex-col gap-2 px-4 pb-1.5 pt-1">
      <div className="animate-tile-fade-up shrink-0">{head}</div>
      {density === 'rich' && (
        <div className="shrink-0 text-[10px] text-kumo-subtle">
          {down > 0 ? `${down} 台离线 · 请检查监控状态` : '全部监控在线'}
        </div>
      )}
      {entries}
    </div>
  );
}

function parseCfTime(point) {
  const ts = new Date(point?.datetime || point?.since || point?.date || '').getTime();
  return Number.isFinite(ts) ? ts : null;
}

function fmtCfAxisTime(ts, range) {
  const date = new Date(ts);
  if (Number.isNaN(date.getTime())) return '';
  if (range === '24h') return `${String(date.getHours()).padStart(2, '0')}:00`;
  return `${String(date.getMonth() + 1).padStart(2, '0')}/${String(date.getDate()).padStart(2, '0')}`;
}

function CfZoneTile({ data, loading, isDarkMode, empty, range = '24h', density = 'full', w = 4 }) {
  const points = useMemo(
    () => (Array.isArray(data?.timeseries)
      ? data.timeseries
        .filter(Boolean)
        .map((p) => ({ ts: parseCfTime(p), requests: p.requests ?? 0 }))
        .filter((p) => p.ts != null)
      : []),
    [data],
  );
  const categories = useMemo(() => points.map((p) => fmtCfAxisTime(p.ts, range)), [points, range]);
  const values = useMemo(() => points.map((p) => p.requests), [points]);
  const summary = useMemo(() => seriesSummary(values), [values]);
  const color = useMemo(() => ChartPalette.categorical(6, isDarkMode), [isDarkMode]);

  const isHalf = density === 'half';
  const tier = widthTier(w);
  const showSummary = tier === 'wide' || density === 'rich';
  const halfFootnote = useMemo(() => {
    const parts = [];
    if (data?.cacheHitRate != null) parts.push(`缓存命中 ${fmtPercent(data?.cacheHitRate)}`);
    if (tier === 'wide' && summary) parts.push(`峰值 ${fmtCompact(summary.max)}`);
    if (tier === 'wide' && summary) parts.push(`均值 ${fmtCompact(summary.avg)}`);
    return parts.length ? parts.join(' · ') : null;
  }, [tier, data, summary]);
  const halfSpark = isHalf ? (
    <TileChart
      series={[{ name: '请求量', color, data: values }]}
      categories={categories}
      isDarkMode={isDarkMode}
      density="compact"
      tooltipValueFormat={(v) => `${fmtCompact(v)} 次`}
    />
  ) : null;

  if (empty) return <div className="flex h-full items-center justify-center px-4 text-center text-xs text-kumo-inactive">未配置 Cloudflare 账号</div>;
  if (loading) return <TileSkeleton variant="chart" />;
  if (!categories.length) return <div className="flex h-full items-center justify-center text-xs text-kumo-inactive">无数据</div>;

  return (
    <div className={`flex h-full min-h-0 flex-col overflow-hidden ${isHalf ? '' : 'px-4 pt-1'}`}>
      {isHalf ? (
        <HalfTile
          tier={tier}
          stat={<StatValue value={fmtCompact(data?.requests)} delta={null} />}
          footnote={halfFootnote}
          spark={halfSpark}
          isDarkMode={isDarkMode}
        />
      ) : (
        <>
          <div className="flex shrink-0 flex-wrap items-baseline gap-x-4 gap-y-0.5">
            <StatValue value={fmtCompact(data?.requests)} delta={null} />
            <span className="shrink-0 text-sm font-medium text-kumo-subtle">缓存命中 {fmtPercent(data?.cacheHitRate)}</span>
            {showSummary && summary && (
              <>
                <span className="shrink-0 whitespace-nowrap text-[10px] text-kumo-subtle tabular-nums">
                  峰值 {fmtCompact(summary.max)}
                </span>
                <span className="shrink-0 whitespace-nowrap text-[10px] text-kumo-subtle tabular-nums">
                  均值 {fmtCompact(summary.avg)}
                </span>
              </>
            )}
          </div>
          <div className="animate-tile-fade-up -mx-4 mt-1.5 min-h-0 flex-1 overflow-hidden">
            <TileChart
              series={[{ name: '请求量', color, data: values }]}
              categories={categories}
              isDarkMode={isDarkMode}
              density={density}
              tooltipValueFormat={(v) => `${fmtCompact(v)} 次`}
            />
          </div>
        </>
      )}
    </div>
  );
}

// —— 组件 ——

export default function TilesBoard() {
  const theme = useStore((s) => s.theme);
  const isDarkMode = theme === 'dark';
  const setAppProcessUptimeSeconds = useStore((s) => s.setAppProcessUptimeSeconds);
  const headerToolsEl = useContext(HeaderToolsContext); // 正式面板：控制栏 portal 到面包屑栏；demo 页无 Provider 则内联
  const [rangeDays, setRangeDays] = useState(() => loadRangeFromStorage()?.days ?? 14);
  const [rangeLabel, setRangeLabel] = useState(() => loadRangeFromStorage()?.label ?? '过去 14 天');
  // 按列数分桶：2~8 列各一套独立布局。当前列数取 TileGrid 容器实际宽度（useContainerWidth 上报），
  // 侧栏 AI 面板让位后主内容变窄 → 列数随之变化，而不是按视口固定。
  const [cols, setCols] = useState(5);
  const [layouts, setLayouts] = useState(null); // { '2'..'8' }，null = 云端布局尚未加载
  const layout = layouts ? layouts[cols] : null;
  const setLayout = useCallback((updater) => {
    setLayouts((prev) => {
      const base = prev || {};
      const current = base[cols] ?? null;
      const next = typeof updater === 'function' ? updater(current) : updater;
      return { ...base, [cols]: next };
    });
  }, [cols]);
  const [cloudReady, setCloudReady] = useState(false);

  const [apiStats, setApiStats] = useState(null);
  const [apiStatsLoading, setApiStatsLoading] = useState(false);
  const [openaiData, setOpenaiData] = useState(null);
  const [openaiLoading, setOpenaiLoading] = useState(false);
  const [host, setHost] = useState(null);
  const [uptime, setUptime] = useState(null);
  const [dash, setDash] = useState(null);

  const [cfAccounts, setCfAccounts] = useState([]);
  const [cfZones, setCfZones] = useState([]);
  const [cfAccountId, setCfAccountId] = useState('');
  const [cfZoneId, setCfZoneId] = useState('all'); // 'all' = 全部 Zone 聚合
  const [cfRange, setCfRange] = useState(() => loadRangeFromStorage()?.cfRange ?? '24h');
  const [cfData, setCfData] = useState(null);
  const [cfLoading, setCfLoading] = useState(false);

  const loadApiStats = useCallback(async () => {
    const cacheKey = `apiStats:${rangeDays}`;
    const cached = cacheGet(cacheKey);
    if (cached) setApiStats(cached);
    if (!cached) setApiStatsLoading(true);
    try {
      const res = await fetchWithTimeout(`/api/system/api-stats?days=${rangeDays}`);
      const json = await res.json().catch(() => null);
      if (json?.success && json.data) {
        setApiStats(json.data);
        cacheSet(cacheKey, json.data, CACHE_TTL.apiStats);
      }
    } catch (err) {
      console.error('[TilesDemo] api-stats', err);
    } finally {
      setApiStatsLoading(false);
    }
  }, [rangeDays]);

  const loadOpenai = useCallback(async () => {
    const cacheKey = `openai:${rangeDays}`;
    const cached = cacheGet(cacheKey);
    if (cached) setOpenaiData(cached);
    if (!cached) setOpenaiLoading(true);
    try {
      // 近 24h（分钟/小时档）用小时粒度，其余按天
      const gran = rangeDays <= 1 ? 'hour' : 'day';
      const res = await fetchWithTimeout(`/api/openai/analytics/charts?days=${Math.max(1, rangeDays)}&granularity=${gran}`);
      const json = await res.json().catch(() => null);
      if (json && Array.isArray(json.daily)) {
        setOpenaiData(json);
        cacheSet(cacheKey, json, CACHE_TTL.openai);
      }
    } catch (err) {
      console.error('[TilesDemo] openai charts', err);
    } finally {
      setOpenaiLoading(false);
    }
  }, [rangeDays]);

  const loadUptime = useCallback(async () => {
    const cached = cacheGet('uptime');
    if (cached) setUptime(cached);
    try {
      const res = await fetchWithTimeout('/api/uptime/monitors');
      const json = await res.json().catch(() => null);
      const monitors = toArray(json);
      let up = 0;
      monitors.forEach((m) => {
        if (!m.active) return;
        if (m.lastHeartbeat) {
          const status = m.lastHeartbeat.status;
          if (status === 1 || status === 'up') up += 1;
        } else {
          up += 1;
        }
      });
      const uptimeData = { total: monitors.length, up, items: monitors };
      setUptime(uptimeData);
      cacheSet('uptime', uptimeData, CACHE_TTL.uptime);
    } catch (err) {
      console.error('[TilesDemo] uptime', err);
    }
  }, []);

  // 仪表盘信息汇总（服务器 / PaaS / DNS / 文件柜 / TOTP / 调度 / 状态页）。
  // 分快慢两批结算：快批（本地 SQLite/CF 列表）先行渲染，服务器/定时任务/状态页等卡不等慢批；
  // 慢批（Koyeb/Fly 直连外部，可达 8s+，超时 16s）独立补齐 PaaS 卡，失败/超时只影响单卡，不拖垮整个统计组。
  const DASH_FAST_SOURCES = [
    { key: 'servers', url: '/api/server/accounts' },
    { key: 'dns', url: '/api/cloudflare/zones' },
    { key: 'filebox', url: '/api/filebox/history' },
    { key: 'totp', url: '/api/totp/accounts' },
    { key: 'scheduler', url: '/api/scheduler/tasks' },
    { key: 'spU', url: '/api/uptime/status-pages' },
    { key: 'spS', url: '/api/server/status-pages' },
    { key: 'spG', url: '/api/github/public-pages' },
  ];
  const DASH_SLOW_SOURCES = [
    { key: 'koyeb', url: '/api/koyeb/data', timeout: 16000 },
    { key: 'fly', url: '/api/flyio/proxy/apps', timeout: 16000 },
  ];
  // 逐接口独立容错：单个接口超时/失败只取其空结果，不影响同批其他接口
  const fetchDashBatch = async (sources) => {
    const settled = await Promise.allSettled(
      sources.map((src) =>
        fetchWithTimeout(src.url, {}, src.timeout || FETCH_TIMEOUT_MS).then((r) => r.json().catch(() => ({}))),
      ),
    );
    const results = {};
    sources.forEach((src, i) => {
      results[src.key] = settled[i].status === 'fulfilled' && settled[i].value ? settled[i].value : {};
    });
    return results;
  };
  const buildPaasStats = (koyebJson, flyJson) => {
    const koyeb = { total: 0, running: 0, list: [] };
    (koyebJson?.accounts || []).forEach((acc) => {
      acc?.projects?.forEach((project) => {
        project?.services?.forEach((service) => {
          koyeb.total += 1;
          if (service?.status === 'HEALTHY' || service?.status === 'RUNNING') koyeb.running += 1;
          koyeb.list.push({
            name: service?.name || service?.id || '',
            status: service?.status === 'HEALTHY' || service?.status === 'RUNNING' ? 'running' : 'stopped',
          });
        });
      });
    });
    const fly = { total: 0, running: 0, list: [] };
    toArray(flyJson).forEach((acc) => {
      acc?.apps?.forEach((app) => {
        fly.total += 1;
        if (app?.status === 'deployed' || app?.status === 'running') fly.running += 1;
        fly.list.push({
          name: app?.name || app?.id || '',
          status: app?.status === 'deployed' || app?.status === 'running' ? 'running' : 'stopped',
        });
      });
    });
    return {
      koyeb,
      fly,
      list: [
        ...koyeb.list.map((s) => ({ ...s, kind: 'koyeb' })),
        ...fly.list.map((a) => ({ ...a, kind: 'fly' })),
      ],
    };
  };
  const loadDashboardStats = useCallback(async () => {
    const cached = cacheGet('dash');
    if (cached) setDash(cached);

    // 快批：本地/短路径接口先行结算，服务器、定时任务、状态页、模块入口等卡立即渲染；
    // 有缓存时 paas 沿用旧值避免 PaaS 卡闪骨架，慢批返回后再覆盖。
    let fastDash = null;
    try {
      const { servers: serversJson, dns: dnsJson, filebox: fileboxJson, totp: totpJson, scheduler: schedJson, spU: spUJson, spS: spSJson, spG: spGJson } = await fetchDashBatch(DASH_FAST_SOURCES);

      const serverItems = toArray(serversJson).map((s) => ({
        name: s?.name || s?.host || s?.id || '',
        host: s?.host || '',
        country: s?.country || s?.resolved_country || '',
        responseTime: s?.response_time ?? s?.responseTime ?? null,
        info: s?.info || null,
        status: normalizeServerStatus(s?.status),
      }));
      const servers = {
        total: serverItems.length,
        online: serverItems.filter((s) => s.status === 'online').length,
        error: serverItems.filter((s) => s.status === 'error').length,
        list: serverItems,
      };
      servers.offline = servers.total - servers.online - servers.error;

      const schedTasks = toArray(schedJson);
      const scheduler = {
        total: schedTasks.length,
        enabled: schedTasks.filter((t) => !!(t?.enabled || t?.isEnabled)).length,
        list: schedTasks.map((t) => ({
          name: t?.name || t?.title || t?.id || '',
          enabled: !!(t?.enabled || t?.isEnabled),
        })),
      };

      const statusPages = [
        ...toArray(spUJson).map((p) => ({ kind: 'uptime', id: p.id, slug: p.slug || '', name: p.name || p.title || '', url: p.url || '' })),
        ...toArray(spSJson).map((p) => ({ kind: 'server', id: p.id, slug: p.slug || '', name: p.name || p.title || '', url: p.url || '' })),
        ...toArray(spGJson).map((p) => ({ kind: 'github', id: p.id, slug: p.slug || '', name: p.name || p.title || '', url: p.url || '' })),
      ];

      fastDash = {
        servers,
        paas: cached?.paas ?? null, // 慢批返回前：有缓存沿用旧值，无缓存保持骨架
        dns: { zones: toArray(dnsJson).length },
        filebox: { total: toArray(fileboxJson).length },
        totp: { total: toArray(totpJson).length },
        scheduler,
        statusPages: { total: statusPages.length, list: statusPages },
      };
      setDash(fastDash);
    } catch (err) {
      console.error('[TilesDemo] dashboard stats (fast)', err);
    }

    // 慢批：Koyeb/Fly 直连外部，独立结算；失败置空结构，PaaS 卡从骨架转空态而非永久等待
    const slowResults = await fetchDashBatch(DASH_SLOW_SOURCES);
    const paas = buildPaasStats(slowResults.koyeb || {}, slowResults.fly || {});
    const fullDash = { ...(fastDash || {}), paas };
    setDash(fullDash);
    cacheSet('dash', fullDash, CACHE_TTL.dash);
  }, []);

  // 云端布局：读 /api/settings 的 tileLayout 字段（存于 data.db，跨设备）。
  // 兼容三种格式：旧数组（= 桌面 5 列）、旧断点桶（{ mobile, tablet, desktop, wide }）、新列数桶（{ '2'..'8' }）。
  useEffect(() => {
    let stopped = false;
    (async () => {
      try {
        const res = await fetchWithTimeout('/api/settings');
        const json = await res.json().catch(() => null);
        const saved = json?.data?.tileLayout ?? json?.tileLayout ?? null;
        const valid = (arr) => (Array.isArray(arr)
          ? arr.filter((it) => it && TILE_DEFS_BY_ID[it.i] && Number.isFinite(it.x) && Number.isFinite(it.w) && Number.isFinite(it.h))
          : null);
        let loaded = null;
        if (Array.isArray(saved)) {
          loaded = { 5: valid(saved) }; // 旧格式：整段视为 5 列桌面布局
        } else if (saved && typeof saved === 'object') {
          const isBreakpointFormat = 'mobile' in saved || 'desktop' in saved;
          if (isBreakpointFormat) {
            // 旧断点桶 → 映射到列数桶：mobile→2 / tablet→3 / desktop→5 / wide→8
            loaded = {
              2: valid(saved.mobile),
              3: valid(saved.tablet),
              5: valid(saved.desktop),
              8: valid(saved.wide),
            };
          } else {
            // 新列数桶：'2'..'8' 各一套
            loaded = {};
            for (let c = MIN_COLS; c <= MAX_COLS; c += 1) loaded[c] = valid(saved[String(c)]);
          }
        }
        if (!stopped && loaded) {
          setLayouts((prev) => {
            const merged = { ...(prev || {}) };
            let changed = false;
            Object.entries(loaded).forEach(([key, val]) => {
              if (val && val.length) {
                merged[key] = val;
                changed = true;
              }
            });
            return changed ? merged : prev;
          });
        }
      } catch (err) {
        console.error('[TilesDemo] load tile layout', err);
      } finally {
        if (!stopped) setCloudReady(true);
      }
    })();
    return () => {
      stopped = true;
    };
  }, []);

  // TileGrid 上报真实列数（重置/默认布局按此铺满）
  const [gridCols, setGridCols] = useState(0);

  // 无云端布局（或切换列数后为空）时按当前列数生成铺满的默认布局，各列数桶独立
  useEffect(() => {
    if (!cloudReady || gridCols === 0) return;
    setLayouts((prev) => {
      if (prev && prev[cols] != null) return prev;
      return { ...(prev || {}), [cols]: packDefaultLayout(cols) };
    });
  }, [cloudReady, gridCols, cols, setLayouts]);

  // 主机指标：5s 轮询，累积 CPU 采样做实时 sparkline
  useEffect(() => {
    let stopped = false;
    let timer;
    const tick = async () => {
      try {
        const res = await fetchWithTimeout('/api/system/host-metrics');
        const json = await res.json().catch(() => null);
        if (!stopped && json?.success && json.data) {
          // 页脚「已运行」时长依赖此回写（旧 DashboardPage 由轮询填充 store）
          setAppProcessUptimeSeconds(json.data.process?.uptime);
          setHost((prev) => {
            const next = json.data;
            // 首次加载用后端 1 分钟滚动历史初始化；之后轮询追加当前值并保留最近 60s（5s × 12 点）
            const seedCpu = Array.isArray(next.history?.cpu) ? next.history.cpu : [];
            const seedMem = Array.isArray(next.history?.memory) ? next.history.memory : [];
            const seedDisk = Array.isArray(next.history?.disk) ? next.history.disk : [];
            const samples = [...(prev?.samples?.length ? prev.samples : seedCpu), next.cpu?.usage ?? 0].slice(-60);
            const memSamples = [...(prev?.memSamples?.length ? prev.memSamples : seedMem), next.memory?.usage ?? 0].slice(-60);
            const diskSamples = [...(prev?.diskSamples?.length ? prev.diskSamples : seedDisk), next.disk?.usage ?? 0].slice(-60);
            // GPU 利用率：字段形态可能是 gpu.usage 或 gpu[0].usage，缺省时保持上一轮序列
            const gpuUsage = next.gpu?.usage ?? next.gpu?.[0]?.usage;
            const gpuSamples = gpuUsage != null
              ? [...(prev?.gpuSamples || []), gpuUsage].slice(-60)
              : (prev?.gpuSamples || []);
            return { ...next, samples, memSamples, diskSamples, gpuSamples };
          });
        }
      } catch (err) {
        // 轮询失败静默，下一轮继续
      }
      if (!stopped) timer = window.setTimeout(tick, HOST_POLL_MS);
    };
    tick();
    return () => {
      stopped = true;
      window.clearTimeout(timer);
    };
  }, []);

  useEffect(() => {
    loadApiStats();
    loadOpenai();
  }, [loadApiStats, loadOpenai]);

  useEffect(() => {
    loadUptime();
    loadDashboardStats();
  }, [loadUptime, loadDashboardStats]);

  // Cloudflare：账号 → zone → 分析（24h / 7d 窗口，实时代理 CF API）
  useEffect(() => {
    let stopped = false;
    (async () => {
      try {
        const cached = cacheGet('cfAccounts');
        if (cached) {
          if (!stopped) {
            setCfAccounts(cached);
            setCfAccountId(cached[0].id);
          }
          return;
        }
        const res = await fetchWithTimeout('/api/cloudflare/accounts');
        const json = await res.json().catch(() => ({}));
        const accounts = toArray(json);
        if (stopped || !accounts.length) return;
        cacheSet('cfAccounts', accounts, CACHE_TTL.cfAccounts);
        setCfAccounts(accounts);
        setCfAccountId(accounts[0].id);
      } catch (err) {
        console.error('[TilesDemo] cf accounts', err);
      }
    })();
    return () => {
      stopped = true;
    };
  }, []);

  useEffect(() => {
    if (!cfAccountId) return undefined;
    let stopped = false;
    (async () => {
      try {
        const cacheKey = `cfZones:${cfAccountId}`;
        const cached = cacheGet(cacheKey);
        if (cached) {
          if (!stopped) setCfZones(cached);
          return;
        }
        const res = await fetchWithTimeout(`/api/cloudflare/accounts/${encodeURIComponent(cfAccountId)}/zones`);
        const json = await res.json().catch(() => ({}));
        if (stopped) return;
        // 该接口响应为 { zones: [...] }（无 data 键），toArray 只认数组/{data}，需直接取 zones
        const zones = Array.isArray(json?.zones) ? json.zones : toArray(json);
        cacheSet(cacheKey, zones, CACHE_TTL.cfZones);
        setCfZones(zones);
        // 默认保持 'all'（全部 Zone 聚合），不自动选中第一个 zone
      } catch (err) {
        console.error('[TilesDemo] cf zones', err);
      }
    })();
    return () => {
      stopped = true;
    };
  }, [cfAccountId]);

  // Cloudflare 分析：cfZoneId === 'all' 时并发拉取全部 Zone 并按时间对齐聚合（请求量/缓存求和）
  useEffect(() => {
    if (!cfAccountId) return undefined;
    const zoneIds = cfZoneId === 'all' ? cfZones.map((z) => z.id) : [cfZoneId];
    if (!zoneIds.length) return undefined;
    let stopped = false;
    const cacheKey = `cfAnalytics:${cfAccountId}:${cfZoneId}:${cfRange}`;
    const cached = cacheGet(cacheKey);
    if (cached) setCfData(cached);
    if (!cached) setCfLoading(true);
    (async () => {
      try {
        const results = await Promise.all(
          zoneIds.map((id) =>
            fetchWithTimeout(
              `/api/cloudflare/accounts/${encodeURIComponent(cfAccountId)}/zones/${encodeURIComponent(id)}/analytics?timeRange=${cfRange}`,
            ).then((r) => r.json().catch(() => ({}))),
          ),
        );
        const byTs = new Map();
        let totalRequests = 0;
        let totalCached = 0;
        results.forEach((json) => {
          const an = json?.analytics || {};
          totalRequests += Number(an.requests) || 0;
          totalCached += Number(an.cachedRequests) || 0;
          (Array.isArray(an.timeseries) ? an.timeseries : []).forEach((p) => {
            const ts = parseCfTime(p);
            if (ts == null) return;
            let e = byTs.get(ts);
            if (!e) {
              e = { ts, requests: 0, cached: 0 };
              byTs.set(ts, e);
            }
            e.requests += Number(p.requests) || 0;
            e.cached += Number(p.cachedRequests) || 0;
          });
        });
        const timeseries = [...byTs.values()]
          .sort((a, b) => a.ts - b.ts)
          .map((e) => ({ datetime: new Date(e.ts).toISOString(), requests: e.requests, cachedRequests: e.cached }));
        if (!stopped) {
          const cfDataOut = {
            requests: totalRequests,
            cachedRequests: totalCached,
            cacheHitRate: totalRequests ? totalCached / totalRequests : 0,
            timeseries,
          };
          setCfData(cfDataOut);
          cacheSet(cacheKey, cfDataOut, CACHE_TTL.cfAnalytics);
        }
      } catch (err) {
        console.error('[TilesDemo] cf analytics', err);
      } finally {
        if (!stopped) setCfLoading(false);
      }
    })();
    return () => {
      stopped = true;
    };
  }, [cfAccountId, cfZoneId, cfRange, cfZones]);

  // 布局每次修改立即自动保存到云端（/api/settings PATCH，四断点对象 merge 进用户设置）；相同内容去重避免冗余写入
  const lastSavedLayoutRef = useRef(null);
  useEffect(() => {
    if (!cloudReady) return;
    const json = JSON.stringify(layouts);
    if (lastSavedLayoutRef.current === json) return;
    lastSavedLayoutRef.current = json;
    fetchWithTimeout('/api/settings', {
      method: 'PATCH',
      body: JSON.stringify({ tileLayout: layouts }),
    }).catch((err) => console.error('[TilesDemo] save tile layout', err));
  }, [layouts, cloudReady]);

  const visibleIds = useMemo(() => new Set((layout || []).map((it) => it.i)), [layout]);

  const toggleTile = useCallback((id) => {
    const def = TILE_DEFS_BY_ID[id];
    if (!def) return;
    setLayout((prev) => {
      const base = prev || [];
      if (base.some((it) => it.i === id)) return base.filter((it) => it.i !== id);
      const bottom = base.reduce((m, it) => Math.max(m, it.y + it.h), 0);
      return [...base, { i: id, x: 0, y: bottom, w: def.w, h: def.h, minW: def.minW ?? 1, minH: 1, maxW: 4, maxH: 4 }];
    });
  }, [setLayout]);

  const resetLayout = useCallback(() => {
    setLayout(packDefaultLayout(gridCols || 4));
  }, [gridCols, setLayout]);

  const renderTile = (id, item) => {
    const density = tileDensity(item.w, item.h);
    switch (id) {
      case 'apiTrend':
        return (
          <TileFrame title="API 调用趋势">
            <ApiTrendMultiTile data={apiStats} loading={apiStatsLoading} isDarkMode={isDarkMode} density={density} w={item.w} />
          </TileFrame>
        );
      case 'apiTokens':
        return (
          <TileFrame title="API 令牌消耗">
            <ApiTokensTile data={apiStats} loading={apiStatsLoading} isDarkMode={isDarkMode} density={density} w={item.w} />
          </TileFrame>
        );
      case 'openaiRequests':
        return (
          <TileFrame title="OpenAI 网关请求">
            <OpenaiRequestsTile data={openaiData} loading={openaiLoading} isDarkMode={isDarkMode} density={density} w={item.w} />
          </TileFrame>
        );
      case 'openaiLatency':
        return (
          <TileFrame title="OpenAI 延迟">
            <OpenaiLatencyTile data={openaiData} loading={openaiLoading} isDarkMode={isDarkMode} density={density} w={item.w} />
          </TileFrame>
        );
      case 'openaiErrors':
        return (
          <TileFrame title="OpenAI 错误数">
            <OpenaiErrorsTile data={openaiData} loading={openaiLoading} isDarkMode={isDarkMode} density={density} w={item.w} />
          </TileFrame>
        );
      case 'hostCpu':
        return (
          <TileFrame title="主机性能">
            <HostCpuTile data={host} isDarkMode={isDarkMode} density={density} w={item.w} />
          </TileFrame>
        );
      case 'uptime':
        return (
          <TileFrame title="监控可用率">
            <UptimeTile data={uptime} density={density} w={item.w} />
          </TileFrame>
        );
      case 'cfZone':
        return (
          <TileFrame
            title="Cloudflare Zone 请求"
            action={widthTier(item.w) === 'narrow' ? undefined : (
              <Select
                size="xs"
                aria-label="选择 Zone"
                value={cfZoneId}
                onValueChange={(v) => setCfZoneId(String(v ?? 'all'))}
                items={[
                  { value: 'all', label: '全部 Zone' },
                  ...cfZones.map((z) => ({ value: z.id, label: z.name || z.id })),
                ]}
                renderValue={(v) => (
                  <span className="min-w-0 truncate">
                    {v === 'all' ? '全部 Zone' : (cfZones.find((z) => z.id === v)?.name || String(v))}
                  </span>
                )}
                alignItemWithTrigger
                className="min-w-0 max-w-[12rem]"
              />
            )}
          >
            <CfZoneTile
              data={cfData}
              loading={cfLoading}
              isDarkMode={isDarkMode}
              empty={!cfAccounts.length}
              range={cfRange}
              density={density}
              w={item.w}
            />
          </TileFrame>
        );
      case 'servers':
        return (
          <TileFrame title="服务器状态">
            <ServerStatusTile servers={dash?.servers} density={density} w={item.w} />
          </TileFrame>
        );
      case 'paas':
        return (
          <TileFrame title="PaaS 实例">
            <PaasTile dash={dash} density={density} w={item.w} />
          </TileFrame>
        );
      case 'scheduler':
        return (
          <TileFrame title="定时任务">
            <SchedulerTile dash={dash} density={density} w={item.w} />
          </TileFrame>
        );
      case 'moduleTools':
        return (
          <TileFrame title="模块入口">
            <ModuleToolsTile dash={dash} uptime={uptime} density={density} w={item.w} />
          </TileFrame>
        );
      case 'statusPages':
        return (
          <TileFrame title="状态页">
            <StatusPagesTile dash={dash} density={density} w={item.w} />
          </TileFrame>
        );
      default:
        return null;
    }
  };

  // 控制工具栏：正式面板经 portal 渲染到面包屑栏（无间距），独立 demo 页内联在网格上方（mb-4）
  const toolbar = (
    <div className={`flex flex-wrap items-center justify-end gap-2 ${headerToolsEl ? '' : 'mb-4'}`}>
      <TimeRangePicker
        value={rangeLabel}
        onApply={(days, cfRange, label) => {
          setRangeDays(days);
          setCfRange(cfRange);
          setRangeLabel(label);
          try {
            localStorage.setItem(RANGE_STORAGE_KEY, JSON.stringify({ days, cfRange, label }));
          } catch {
            /* ignore */
          }
        }}
      />
      <DropdownMenu>
        <DropdownMenu.Trigger>
          <Button size="sm" icon={<SquaresFour className="h-4 w-4" />}>
            卡片管理
          </Button>
        </DropdownMenu.Trigger>
        <DropdownMenu.Content>
          {TILE_DEFS.map((def) => (
            <div
              key={def.id}
              className="flex min-w-[10rem] items-center justify-between gap-3 px-2.5 py-1.5"
            >
              <span className="min-w-0 truncate text-xs text-kumo-default">{def.title}</span>
              <Switch size="sm" checked={visibleIds.has(def.id)} onCheckedChange={() => toggleTile(def.id)} />
            </div>
          ))}
        </DropdownMenu.Content>
      </DropdownMenu>
    </div>
  );

  return (
    <div className="w-full">
      {headerToolsEl ? createPortal(toolbar, headerToolsEl) : toolbar}

      <TileGrid
        layout={layout}
        onLayoutChange={setLayout}
        onColsChange={(c) => {
          setGridCols(c);
          setCols(c); // 列数取 TileGrid 容器实际宽度（侧栏让位后变窄会相应变化），作为当前布局桶
        }}
        renderTile={(item) => renderTile(item.i, item)}
      />
    </div>
  );
}
