// 公开网址导航页的纯逻辑：路由判定、密度、排序与搜索过滤。
// 与 UI 解耦以便单测（沿用 sunpanelImport.js 的做法）。

/** 聚合页保留 slug：/bookmarks/all 展示全部已公开分组。 */
export const AGGREGATE_SLUG = 'all';

/**
 * 从 pathname 解析公开页 slug。
 * 返回 null 表示当前路径不是公开书签页。
 */
export function parsePublicBookmarksPath(pathname) {
  const path = String(pathname || '').replace(/\/+$/, '');
  const match = path.match(/^\/(?:b|bookmarks|bm)\/([^/]+)$/);
  if (!match) return null;
  let slug = match[1];
  try {
    slug = decodeURIComponent(slug);
  } catch {
    // 非法百分号编码：按原样处理
  }
  return slug || null;
}

/** 是否为聚合页（/bookmarks/all）。 */
export function isAggregateSlug(slug) {
  return String(slug || '').toLowerCase() === AGGREGATE_SLUG;
}

/**
 * 解析 /bookmarks/all/<sort> 里的排序描述。
 * 合法值形如 order-asc / name-desc / items-asc / updated-desc，非法时回退默认。
 */
export const SORT_VALUES = ['order-asc', 'name-asc', 'name-desc', 'items-desc', 'items-asc', 'updated-desc'];
export const DEFAULT_SORT = 'order-asc';

export function parseSortSpec(spec) {
  const value = String(spec || '').trim().toLowerCase();
  return SORT_VALUES.includes(value) ? value : DEFAULT_SORT;
}

// 展示密度：书签卡片本身很小，因此不固定列数，也不给页面设宽度上限——
// 列数由 CSS auto-fill + 卡片最小宽度决定，屏幕越宽摆越多列，
// 而不是把卡片拉宽或在两侧留出大片空白。
//
// gap 三档刻意拉开差距（0.375 / 0.75 / 1.25rem）：
// 早期是 0.5 / 0.625 / 0.75rem，相邻两档只差 2px，
// 切换布局时「间距没变」几乎看不出来，密度开关形同虚设。
export const DENSITY_KEYS = ['compact', 'cozy', 'wide'];
export const DEFAULT_DENSITY = 'cozy';

// 图标尺寸：图标盒子（icon）与图片尺寸（iconInner）。
// 两者保持一致 —— 图片若比盒子小（曾出现 44px 盒子配 28px 图，只占 64%），
// 视觉上就会显得又小又空。尺寸整体取「够清晰但不喧宾夺主」的档位。
//
// 列宽用 min/max 两个值定义一条**弹性区间**：
//   min = 卡片的最小可读宽度（决定窄屏时能塞几列）
//   max = 卡片的最大宽度（防止超宽屏把卡片拉成一条长条）
// 轨道写成 minmax(min, 1fr)，1fr 保证轨道**永远填满容器、两侧不留空隙**；
// min 保证不会被压得比 min 更窄；max 通过卡片自身的 max-width 兜底。
//
// 为什么不用固定列宽：固定轨道（repeat(auto-fill, 13rem)）虽然能让卡片宽度
// 绝对一致，但容器宽度除不尽时余数无处可去，全部堆在右侧形成空隙。
// 「无空隙」与「宽度绝对一致」在 CSS 网格里不可兼得，这里选择无空隙。
export const DENSITY = {
  compact: { min: '9rem', max: '12rem', gap: '0.375rem', shell: 'max-w-none', icon: 'h-8 w-8', iconInner: 'h-8 w-8' },
  cozy: { min: '11.5rem', max: '14.5rem', gap: '0.75rem', shell: 'max-w-none', icon: 'h-10 w-10', iconInner: 'h-10 w-10' },
  wide: { min: '14.5rem', max: '18rem', gap: '1.75rem', shell: 'max-w-none', icon: 'h-11 w-11', iconInner: 'h-11 w-11' },
};

export function resolveDensity(value) {
  return DENSITY_KEYS.includes(value) ? value : DEFAULT_DENSITY;
}

/**
 * auto-fill 网格样式：弹性轨道，永远填满容器（两侧不留空隙）。
 *
 * 轨道写成 minmax(min, 1fr)：
 *   - 1fr 让轨道拉伸填满整行 → 容器多宽就铺多宽，余数被各列分摊，**不会留白**；
 *   - min 是卡片的最小可读宽度，决定窄屏能塞几列。
 *
 * 与「固定列宽」的取舍：
 *   固定轨道（repeat(auto-fill, 13rem)）能让卡片宽度绝对一致，
 *   但容器宽度除不尽时余数无处可去、全堆在右侧形成空隙，观感更差。
 *   两者在 CSS 网格里不可兼得，这里选「无空隙」，代价是列宽随容器小幅伸缩。
 *   伸缩幅度被卡片自身的 max-w（见 cardClassForDensity）限制在上限内。
 */
export function gridStyleForDensity(density) {
  const shape = DENSITY[resolveDensity(density)];
  return {
    gap: shape.gap,
    gridTemplateColumns: `repeat(auto-fill, minmax(${shape.min}, 1fr))`,
  };
}

/**
 * 卡片最大宽度类（由 DENSITY.max 生成）。
 *
 * 轨道用 1fr 会拉伸填满容器，列宽可能超过这个上限；卡片加上 max-w 后
 * 就会**靠左、右侧留白**，看起来像「卡片没填满格子」。
 * 因此调用处要配合 mx-auto 让卡片在上限内居中，避免右侧堆出一条空隙。
 */
export function cardClassForDensity(density) {
  return `max-w-[${DENSITY[resolveDensity(density)].max}]`;
}

/** 该密度下页面容器的最大宽度类（配合 auto-fill，宽度随列数增长）。 */
export function shellClassForDensity(density) {
  return DENSITY[resolveDensity(density)].shell;
}

/**
 * 应用「获取图标」的结果到条目表单。
 *
 * 业务规则：抓到图标地址后必须**同时**把图标类型切到「网站图标」(2)。
 * 否则用户在类型为「文字/Emoji」时点获取，地址存进去但不显示 —— 这是曾经的 bug。
 *
 * 抽成纯函数的原因：这个「同时改两个字段」的意图如果写成两次独立的
 * setField 调用，会因为两次都基于同一个陈旧 form 展开而互相覆盖，
 * 结果只有后一次生效（icon_type 的修改被丢掉）。放在这里可以让测试直接锁住。
 */
export function applyFetchedIcon(form, iconSrc) {
  const src = String(iconSrc || '').trim();
  if (!src) return { ...(form || {}) };
  return { ...(form || {}), icon_type: 2, icon_src: src };
}

/** 按关键词过滤分组与其中的网址；分组名命中则整组保留。 */
export function filterGroups(groups, keyword) {
  const needle = String(keyword || '').trim().toLowerCase();
  if (!needle) return Array.isArray(groups) ? groups : [];
  return (Array.isArray(groups) ? groups : [])
    .map(group => {
      const items = (group.items || []).filter(item =>
        String(item?.title || '').toLowerCase().includes(needle)
        || String(item?.url || '').toLowerCase().includes(needle)
        || String(item?.description || '').toLowerCase().includes(needle));
      const groupMatched = String(group?.title || '').toLowerCase().includes(needle)
        || String(group?.description || '').toLowerCase().includes(needle);
      if (groupMatched) return group;
      if (items.length > 0) return { ...group, items };
      return null;
    })
    .filter(Boolean);
}

/** 统计过滤后可见的网址条数。 */
export function countItems(groups) {
  return (Array.isArray(groups) ? groups : [])
    .reduce((total, group) => total + ((group?.items || []).length), 0);
}
