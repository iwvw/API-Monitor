import React, { useEffect, useMemo, useRef, useState } from 'react';
import { Button } from '@cloudflare/kumo/components/button';
import { Checkbox } from '@cloudflare/kumo/components/checkbox';
import { Input } from '@cloudflare/kumo/components/input';
import { addCollection } from '@iconify/react';
import logosIcons from '@iconify-json/logos/icons.json';
import { cx } from '../../components/ui/AppPrimitives.jsx';
import { toast } from '../../modules/toast.js';
import useStore from '../../store.js';
import { Search, X } from '../../components/Icons.jsx';
import EngineIconPreview from './EngineIconPreview.jsx';
import {
  DEFAULT_SEARCH_ENGINES,
  buildSearchUrl,
  readStoredEngineId,
  readStoredNewTab,
  runSearch,
  writeStoredEngineId,
  writeStoredNewTab,
} from '../../modules/publicSearch.js';

// 把图标集注册进 Iconify。
// 必须做这一步：@iconify/react 默认按需向 api.iconify.design 请求图标，
// 内网/离线环境下会静默失败（图标不显示且不报错）。
// addCollection 让图标从构建产物里解析，零网络请求。重复调用是幂等的。
//
// 只带 logos 一个集合：内置引擎（必应/Google/DuckDuckGo）都用它，
// 自定义引擎也允许填 logos:xxx 或图片地址。
// 这里刻意不再打包 simple-icons：
//   - publicSearch.js 已明确记录选型结论「不用 simple-icons，它是单色集合，
//     多个引擎会全是同一颜色，失去品牌辨识度」；
//   - 图标字段是自由文本输入，没有选择器，用户无从得知有哪些 simple-icons 名字；
//   - 代价却是实打实的 4.6MB 原始 JSON（约 1.2MB gzip）全量进包。
addCollection(logosIcons);

const pad = (value) => String(value).padStart(2, '0');

// 时钟：只在秒变化时重渲染，避免每秒整页刷新。
function useClock() {
  const [now, setNow] = useState(() => new Date());
  useEffect(() => {
    const timer = window.setInterval(() => setNow(new Date()), 1000);
    return () => window.clearInterval(timer);
  }, []);
  return now;
}

/**
 * 公开页顶部 hero：左侧 logo/站点名 + 右侧实时时钟 + 居中搜索框。
 *
 * 搜索框是「二合一」：
 *   - 输入时实时过滤下方书签（通过 onFilterChange 交给父组件）
 *   - 回车时跳到外部搜索引擎
 * 这样页面上只有一个搜索框，不会出现「搜索网址」和「搜索内容」两个框。
 *
 * 对齐要点：顶部行两侧元素用 items-end 底部对齐；时钟用等宽数字固定宽度，
 * 避免秒数变化引起整行抖动；搜索框限制最大宽度而不是铺满整行。
 */
export default function PublicHero({
  title = '网址导航',
  /** 站点名（留空则退回页面标题） */
  siteName = '',
  /** 左侧 logo 地址（留空则显示站点名称） */
  logoUrl = '',
  /**
   * 页面上有自定义背景图。
   *
   * 注意它**只用于决定要不要给文字加投影**（压在背景图上需要阴影才立得住），
   * 不再用于决定文字颜色 —— 那是另一个维度，见下面的 isDark。
   * 早前把两者混在一起，导致「配了背景图就当深色处理」：
   * 浅色主题下也输出白字，而文字实际压在**浅色玻璃**上，几乎看不见。
   */
  onBackground = false,
  /** 本地过滤关键词（受控，由父组件持有） */
  filterValue = '',
  /** 关键词变化回调；父组件据此过滤书签 */
  onFilterChange,
  /** 过滤命中的条目数（用于输入框右侧提示） */
  matchCount = null,
  /**
   * 实际可用的搜索引擎列表（由父组件从公开页配置解析后传入）。
   * 不传时用内置默认列表，组件单独使用时也能工作。
   */
  engines = DEFAULT_SEARCH_ENGINES,
  className,
}) {
  const now = useClock();
  const list = Array.isArray(engines) && engines.length ? engines : DEFAULT_SEARCH_ENGINES;
  const [engineId, setEngineId] = useState(() => readStoredEngineId(list));
  const [newTab, setNewTab] = useState(() => readStoredNewTab());
  const [menuOpen, setMenuOpen] = useState(false);
  const menuRef = useRef(null);
  // 主题模式（'light' | 'dark'）：决定压在玻璃上的文字/图标用深色还是浅色。
  // 从 store 读而不是读 document.dataset，这样切换主题会触发重渲染。
  const isDark = useStore((state) => state.theme === 'dark');

  // 当前引擎：列表变更（管理页改了配置）后，原来选中的 id 可能已不存在，
  // 这时回落到列表首个，而不是显示空图标。
  const engine = useMemo(
    () => list.find(item => item.id === engineId) || list[0],
    [list, engineId]
  );

  // 点击外部关闭引擎菜单
  useEffect(() => {
    if (!menuOpen) return undefined;
    const onDown = (event) => {
      if (menuRef.current && !menuRef.current.contains(event.target)) setMenuOpen(false);
    };
    document.addEventListener('mousedown', onDown);
    return () => document.removeEventListener('mousedown', onDown);
  }, [menuOpen]);

  // 回车 = 用当前关键词做外部搜索（本地过滤已在输入时实时生效）
  const submit = () => {
    if (!runSearch(engineId, filterValue, newTab, list)) {
      toast.error('请输入搜索内容');
    }
  };

  const setKeyword = (value) => onFilterChange?.(value);

  const time = `${pad(now.getHours())}:${pad(now.getMinutes())}:${pad(now.getSeconds())}`;
  const date = now.toLocaleDateString('zh-CN', { month: 'numeric', day: 'numeric', weekday: 'long' });

  // 文字颜色由**主题模式**决定（深色主题 → 白字，浅色主题 → 常规深字）。
  // 不能按「有没有背景图」判断：文字实际压在玻璃上，
  // 玻璃的明暗取决于主题；浅色主题下配了背景图仍是白玻璃，白字会看不清。
  const textStrong = isDark ? 'text-white' : 'text-kumo-strong';
  const textSubtle = isDark ? 'text-white/80' : 'text-kumo-subtle';
  // 背景图模式下给压在图上的文字加阴影（见 app.css 的 .public-on-bg-shadow）。
  // 这一项仍由 onBackground 决定 —— 它解决的是「背景花色杂乱导致文字不立」，
  // 与主题明暗是两个独立维度。
  const onBgShadow = onBackground ? 'public-on-bg-shadow' : '';

  return (
    <div className={cx('flex flex-col items-center gap-4', className)}>
      {/* 顶部一行居中：左侧「logo 或名称」（二选一）｜竖线｜右侧时钟+日期。
          对齐要点：外层用 items-center，让三者的**垂直中心**对齐。
          此前用 items-end + 各自的固定高度（h-10/h-12）做底部对齐，
          但 logo 是图片、右侧是两行文字，两者实际内容高度不同，
          底部对齐后视觉中心仍是错位的；改为中心对齐最直观。
          竖线不再 self-end（否则会被拉到行底），跟随 items-center 居中。 */}
      <div className="flex w-full flex-wrap items-center justify-center gap-3">
        {/* 二选一：给了 logo 就显示 logo，否则显示站点名称 */}
        <div className="flex items-center">
          {logoUrl ? (
            <img
              src={logoUrl}
              alt={siteName || title}
              className="h-10 w-auto object-contain sm:h-12"
              style={onBackground ? { filter: 'drop-shadow(0 2px 6px rgb(0 0 0 / 0.45))' } : undefined}
              onError={(event) => {
                // logo 加载失败时退回显示名称，避免顶部只剩一个时钟
                const fallback = event.currentTarget.parentElement?.nextElementSibling;
                event.currentTarget.style.display = 'none';
                if (fallback) fallback.style.display = '';
              }}
            />
          ) : null}
          <span
            className={cx('text-3xl font-semibold leading-none tracking-tight sm:text-4xl', textStrong, onBgShadow)}
            style={logoUrl ? { display: 'none' } : undefined}
          >
            {siteName || title}
          </span>
        </div>

        {/* 分隔竖线：高度取 logo 与时钟之间的中间值，居中显示 */}
        <span
          className={cx('h-9 w-px shrink-0', onBackground ? 'bg-white/40' : 'bg-kumo-line')}
          aria-hidden="true"
        />

        {/* 时钟：固定宽度 + 等宽数字，避免秒数跳动导致整行抖动。
            时间与日期**固定用白色**（不跟随主题）：
            它们直接压在背景图上、始终带投影，投影保证了浅色背景上也能读；
            若跟随主题在浅色下变深色，遇到深色背景图反而更糊。
            尺寸比原先大一级（text-3xl/4xl），让它成为页面的视觉重心。 */}
        <div className="flex flex-col items-center">
          <span className="public-clock-shadow font-mono text-3xl font-semibold leading-none tabular-nums text-white sm:text-4xl">{time}</span>
          <span className="public-clock-shadow mt-1 text-sm leading-none text-white/85">{date}</span>
        </div>
      </div>

      {/* 搜索框：居中且限制最大宽度，不铺满整行。
          尺寸比书签卡片略大（py-2.5 + 图标 5、输入框 sm→base），
          让它在页面里是明确的视觉焦点，而不是和卡片一样高的小条。 */}
      <div className="relative w-full max-w-2xl" ref={menuRef}>
        <div
          className={cx(
            // 复用书签卡片那套 3D 玻璃（public-glass-item）：
            // 搜索框同样压在背景图上，用同一材质才不会显得是两种玻璃。
            'public-glass-item flex items-center gap-2.5 rounded-xl px-3 py-2.5'
          )}
        >
          {/* 引擎切换：透明底色，图标直接露在外面。
              原来用 variant=secondary，图标外套着一个矩形底色块；
              换成 ghost 后仍需显式 bg-transparent，避免玻璃上残留按钮底。 */}
          <Button
            type="button"
            size="sm"
            variant="ghost"
            shape="square"
            onClick={() => setMenuOpen(open => !open)}
            aria-label={`切换搜索引擎（当前 ${engine.label}）`}
            title={`当前：${engine.label}`}
            className={cx('shrink-0 bg-transparent', isDark ? 'hover:bg-white/20' : 'hover:bg-kumo-strong/10')}
          >
            <EngineIconPreview icon={engine.icon} label={engine.label} color={engine.color} size="h-6 w-6" />
          </Button>

          <Input
            size="base"
            value={filterValue}
            onChange={event => setKeyword(event.target.value)}
            onKeyDown={(event) => { if (event.key === 'Enter') submit(); }}
            placeholder="搜索书签，或回车用搜索引擎检索"
            aria-label="搜索书签或检索网络"
            // 底色由外层玻璃容器负责，输入框本体保持透明。
            // 聚焦时不画 Kumo 默认焦点环（那圈深色框），由 .public-hero-search-input
            // 在 CSS 里兜底压掉；外层玻璃容器本身就是清晰的焦点区域。
            //
            // 两个坑：
            //  1. 文字色**不能**写成 [&_input]:text-white —— Kumo 的 Input 直接
            //     渲染原生 input 自身（className 就落在它上面），没有内层 input 子元素，
            //     后代选择器 &_input 永远匹配不到，样式静默失效。
            //  2. 深浅由**主题**决定，不是由「有没有背景图」决定：
            //     placeholder 若是深色默认值，压在深色玻璃上就看不见；
            //     反之浅色主题下用白字，压在浅色玻璃上同样看不见。
            //  3. placeholder 色必须用 ! 提权：Kumo 的 Input 自带
            //     `.kumo-input-placeholder::placeholder { color: var(--text-color-kumo-placeholder) }`，
            //     与 Tailwind 的 placeholder:text-* 同为「单类 + 伪元素」，
            //     而 Kumo 样式表在 Tailwind 之后加载 —— 不加 ! 时永远是 Kumo 的深色赢，
            //     表现为「设了白色 placeholder 却仍是黑的」（实测确认过）。
            className={cx(
              'public-hero-search-input min-w-0 flex-1 border-0 bg-transparent shadow-none ring-0 focus:ring-0',
              'focus-visible:ring-0 focus-visible:outline-none',
              isDark
                ? 'text-white placeholder:!text-white/60'
                : 'text-kumo-strong placeholder:!text-kumo-subtle'
            )}
          />

          {/* 命中数 + 清空：只在有输入时出现，避免空态干扰 */}
          {filterValue ? (
            <>
              {matchCount !== null && (
                <span className={cx('shrink-0 text-[11px] tabular-nums', textSubtle)}>{matchCount} 项</span>
              )}
              <Button
                size="sm"
                variant="ghost"
                shape="square"
                aria-label="清空搜索"
                title="清空"
                onClick={() => setKeyword('')}
                className={cx('shrink-0 bg-transparent', isDark ? 'hover:bg-white/20' : 'hover:bg-kumo-strong/10')}
              >
                <X className="h-4 w-4" />
              </Button>
            </>
          ) : null}

          {/* 右侧提交按钮：放大图标、透明底色。
              两点关键：
               1. 不用 Kumo 的 icon= 属性 —— 它会给图标强制尺寸
                  （size="sm" → size-4 = 16px），className 传的 h-5 会被覆盖，
                  图标根本放不大；
               2. variant 保持 ghost，并显式写 bg-transparent ——
                  玻璃上任何一点按钮底色都会读成「图标外面套了个方块」。 */}
          <Button
            type="button"
            size="sm"
            variant="ghost"
            shape="square"
            aria-label="搜索"
            title="搜索"
            onClick={submit}
            className={cx('shrink-0 bg-transparent', isDark ? 'hover:bg-white/20' : 'hover:bg-kumo-strong/10')}
          >
            <Search className="h-5 w-5" />
          </Button>
        </div>

        {/* 引擎菜单 + 新窗口开关。
            面板宽度改为**自适应内容**（原来是 w-full，与搜索框同宽，
            在页面上是一大片深色块、把下方书签整片遮住，观感像一块遮罩）。
            同时用玻璃材质与卡片/搜索框统一。 */}
        {menuOpen && (
          <div className="public-glass-item absolute left-0 top-full z-30 mt-2 w-max max-w-full rounded-xl p-2.5">
            <div className="flex items-center gap-1">
              {list.map(item => (
                <Button
                  key={item.id}
                  type="button"
                  size="sm"
                  // ghost + 显式透明底色：不给图标套任何方块底。
                  // 选中态只用一圈 ring 标识，不填底色 ——
                  // 填底色会让「当前引擎」看起来像被一个色块罩住，
                  // 与「官方彩色 logo 直接露出」的目标冲突。
                  variant="ghost"
                  shape="square"
                  onClick={() => {
                    setEngineId(item.id);
                    writeStoredEngineId(item.id, list);
                    setMenuOpen(false);
                  }}
                  aria-label={`使用 ${item.label} 搜索`}
                  title={item.label}
                  className={cx(
                    'rounded-lg bg-transparent hover:bg-white/15',
                    item.id === engineId && 'ring-1 ring-kumo-brand'
                  )}
                >
                  <EngineIconPreview icon={item.icon} label={item.label} color={item.color} size="h-6 w-6" />
                </Button>
              ))}
            </div>
            <div className="mt-2 border-t border-kumo-line/50 pt-2">
              <Checkbox
                size="sm"
                label="搜索结果使用新窗口打开"
                checked={newTab}
                onCheckedChange={(checked) => {
                  setNewTab(Boolean(checked));
                  writeStoredNewTab(Boolean(checked));
                }}
              />
            </div>
            <p className="mt-1 text-[11px] text-kumo-subtle">
              当前：{engine.label}{buildSearchUrl(engineId, filterValue) ? '' : '（输入关键词后回车）'}
            </p>
          </div>
        )}
      </div>
    </div>
  );
}
