import React, { useCallback, useEffect, useMemo, useState } from 'react';
import { Loader } from '@cloudflare/kumo';
import { Button } from '@cloudflare/kumo/components/button';
import { Badge } from '@cloudflare/kumo/components/badge';
import PublicPageIconPicker from '../components/public/PublicPageIconPicker.jsx';
import { useCloudflareSpotlight } from '../hooks/useCloudflareSpotlight.js';
import {
  getPublicPageFaviconHref,
  swapPublicPageFavicon,
  withPublicPageIconId,
} from '../modules/publicPageBranding.js';
import { toast } from '../modules/toast.js';
import useStore from '../store.js';
import { cx } from '../components/ui/AppPrimitives.jsx';
import {
  AGGREGATE_SLUG,
  DEFAULT_DENSITY,
  DEFAULT_SORT,
  DENSITY,
  DENSITY_KEYS,
  SORT_VALUES,
  cardClassForDensity,
  countItems,
  filterGroups,
  gridStyleForDensity,
  isAggregateSlug,
  parsePublicBookmarksPath,
} from '../modules/publicBookmarks.js';
import { AlertTriangle, Bookmark, DragHandle, Edit, ExternalLink, Folder, Home, LogIn, Plus, RefreshCw, SortArrows, Trash } from '../components/Icons.jsx';
import ItemFormDialog, { emptyItemForm } from './bookmarks/ItemFormDialog.jsx';
import ItemIcon from './bookmarks/ItemIcon.jsx';
import PublicHero from './bookmarks/PublicHero.jsx';
import BookmarkContextMenu from './bookmarks/BookmarkContextMenu.jsx';
import {
  backgroundLayerStyle,
  backgroundOverlayStyle,
  getBackgroundConfig,
  pageWidthClass,
} from '../modules/publicPageBackground.js';
import { resolveSearchEngines } from '../modules/publicSearch.js';
import { dialog } from '../modules/dialog.js';

export { AGGREGATE_SLUG };

const normalizePublicPath = () => parsePublicBookmarksPath(window.location.pathname);

// 公开页的写操作请求：必须带登录态请求头，否则后端（AuthSession）返回 401。
// 未登录时这些函数不会被调用 —— 入口按钮不渲染。
async function bookmarksRequest(path, options = {}) {
  const response = await fetch(`/api/bookmarks${path}`, {
    ...options,
    headers: { ...(useStore.getState().getAuthHeaders?.() || {}), ...(options.headers || {}) },
  });
  const payload = await response.json().catch(() => ({}));
  if (!response.ok || payload.success === false) {
    throw new Error(payload.message || payload.error || `HTTP ${response.status}`);
  }
  return payload;
}

const isLocalHost = (host) => /^(localhost|127\.0\.0\.1|\[::1\])(?::\d+)?$/i.test(host || '');

const formatDateTime = (value) => {
  if (!value) return '';
  const date = new Date(String(value).includes('T') ? value : `${String(value).replace(' ', 'T')}Z`);
  if (Number.isNaN(date.getTime())) return '';
  return date.toLocaleString('zh-CN', {
    year: 'numeric',
    month: '2-digit',
    day: '2-digit',
    hour: '2-digit',
    minute: '2-digit',
  });
};

// 书签卡片本身很小，因此不用「固定 3/4 列」限制宽度：
// 列数由 auto-fill + 卡片最小宽度决定，屏幕越宽摆越多列，而不是把卡片拉长。
// 布局切换（密度）现在只在「空白处右键」菜单里选，不再占用工具条下拉。
const DENSITY_OPTIONS = [
  { value: 'compact', label: '紧凑' },
  { value: 'cozy', label: '标准' },
  { value: 'wide', label: '宽松' },
];

const DENSITY_STORAGE_KEY = 'publicBookmarksDensity';
const SORT_STORAGE_KEY = 'publicBookmarksSort';

function readStored(key, allowed, fallback) {
  try {
    const value = window.localStorage?.getItem(key);
    return allowed.includes(value) ? value : fallback;
  } catch {
    return fallback;
  }
}

function PublicItemIcon({ item, density = 'cozy' }) {
  const shape = DENSITY[density] || DENSITY.cozy;
  // 渲染规则统一在 ItemIcon 里（管理页、公开页、编辑预览共用同一套），
  // 这里只负责按密度把尺寸传进去。
  return <ItemIcon item={item} size={shape.icon} innerSize={shape.iconInner} />;
}

function BookmarkCard({ item, density, editable, sortable, dragging, over, onEdit, onDelete, onDragStart, onDragOver, onDrop, onDragEnd, onContextMenu }) {
  const openItem = useCallback(() => {
    const raw = item?.url || '';
    if (/^https?:\/\//i.test(raw)) {
      window.open(raw, '_blank', 'noopener,noreferrer');
    }
  }, [item?.url]);

  // 排序模式下点击卡片不跳转，避免误触；拖拽由 draggable 处理。
  const handleClick = () => {
    if (sortable) return;
    openItem();
  };

  return (
    <div
      role="button"
      tabIndex={0}
      draggable={Boolean(sortable)}
      onDragStart={sortable ? onDragStart : undefined}
      onDragOver={sortable ? onDragOver : undefined}
      onDrop={sortable ? onDrop : undefined}
      onDragEnd={sortable ? onDragEnd : undefined}
      // 右键菜单：仅登录态拦截，未登录保留浏览器默认菜单
      onContextMenu={(event) => {
        if (!editable) return;
        event.preventDefault();
        event.stopPropagation();
        onContextMenu?.(event, item);
      }}
      className={cx(
        // mx-auto：列宽被 1fr 拉得超过卡片上限时，让卡片在列内居中，
        // 而不是靠左、把空隙全堆到右侧。
        'public-glass-item group relative mx-auto flex w-full min-w-0 items-center gap-2.5 overflow-hidden rounded-xl p-2.5 focus-visible:outline focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-kumo-focus',
        cardClassForDensity(density),
        sortable && 'public-bookmark-draggable',
        // dragging：被拖起的那张（降透明度 + 轻微缩放）
        // over：当前悬停的目标（高亮边框，提示「会插到这里」）
        dragging && 'public-bookmark-dragging',
        over && !dragging && 'public-bookmark-over'
      )}
      onClick={handleClick}
      onKeyDown={(event) => {
        if (event.key === 'Enter' || event.key === ' ') {
          event.preventDefault();
          handleClick();
        }
      }}
      title={item.description || item.url}
    >
      {sortable && (
        <DragHandle className="h-4 w-4 shrink-0 text-kumo-subtle" aria-hidden="true" />
      )}
      <PublicItemIcon item={item} density={density} />
      <div className="min-w-0 flex-1">
        <div className="flex items-center gap-1.5">
          <span className="truncate text-[15px] font-medium leading-snug text-kumo-strong">{item.title}</span>
        </div>
        {item.description && <div className="mt-0.5 truncate text-[13px] leading-snug text-kumo-subtle">{item.description}</div>}
      </div>
      {/* 登录态下的就地编辑/删除；与分组标题行一致使用 ghost 悬浮显示 */}
      {editable && !sortable && (
        <div className="public-group-actions absolute right-1 top-1 flex items-center gap-0.5">
          <Button
            size="sm"
            variant="ghost"
            shape="square"
            icon={<Edit className="h-3.5 w-3.5" />}
            aria-label={`编辑 ${item.title}`}
            title="编辑"
            onClick={(event) => { event.stopPropagation(); onEdit?.(item); }}
          />
          <Button
            size="sm"
            variant="ghost"
            shape="square"
            icon={<Trash className="h-3.5 w-3.5" />}
            aria-label={`删除 ${item.title}`}
            title="删除"
            onClick={(event) => { event.stopPropagation(); onDelete?.(item); }}
          />
        </div>
      )}
    </div>
  );
}

function GroupSection({
  group, density, editable, sortMode,
  onAddItem, onEditItem, onDeleteItem, onToggleSort, onReorder, onItemContextMenu,
}) {
  const items = Array.isArray(group.items) ? group.items : [];
  const [draggingId, setDraggingId] = useState(null);
  const [overId, setOverId] = useState(null);

  const handleDragStart = (item, event) => {
    setDraggingId(item.id);
    event.dataTransfer.effectAllowed = 'move';
    event.dataTransfer.setData('text/plain', String(item.id));
    // 不调用 setDragImage：默认幽灵图已经够用，而自定义截图会与
    // .public-bookmark-dragging 的 scale(0.97) 打架（截图时卡片正被缩小）。
  };
  const handleDragOver = (targetItem, event) => {
    if (!draggingId) return;
    event.preventDefault();
    event.dataTransfer.dropEffect = 'move';
    // 记录当前悬停的卡片：拖到哪张上，就高亮哪张作为落点提示
    const targetKey = targetItem?.id != null ? String(targetItem.id) : null;
    if (targetKey && targetKey !== String(overId)) setOverId(targetItem.id);
  };
  const handleDrop = (targetId, event) => {
    event.preventDefault();
    const sourceId = draggingId ?? Number(event.dataTransfer.getData('text/plain'));
    setDraggingId(null);
    setOverId(null);
    if (!sourceId || String(sourceId) === String(targetId)) return;
    const from = items.findIndex(entry => String(entry.id) === String(sourceId));
    const to = items.findIndex(entry => String(entry.id) === String(targetId));
    if (from < 0 || to < 0) return;
    const next = [...items];
    const [moved] = next.splice(from, 1);
    next.splice(to, 0, moved);
    onReorder?.(group, next);
  };

  return (
    <section className="public-group-frame">
      {/* 分组标题行：组名 + ghost 操作按钮（hover 显示）+ 数量 */}
      <div className="public-group-header flex items-center justify-between gap-3 px-1 py-2">
        {/* 组名胶囊：图标与组名同处一个 3D 玻璃块内部（整块是「📁 组名」）。
            与卡片/搜索框用同一材质（public-glass-item），保持一致。
            描述是次要信息，留在胶囊外面。 */}
        <div className="flex min-w-0 items-center gap-2.5">
          <div className="public-glass-item public-glass-static flex min-w-0 items-center gap-2 rounded-lg px-2.5 py-1">
            <Folder className="h-4 w-4 shrink-0 text-kumo-subtle" />
            <h2 className="truncate text-base font-semibold text-kumo-strong">{group.title}</h2>
          </div>
          {group.description && <span className="min-w-0 truncate text-xs text-kumo-subtle">{group.description}</span>}
        </div>
        <div className="flex shrink-0 items-center gap-1">
          {editable && (
            <div className="public-group-actions flex items-center gap-0.5">
              <Button
                size="sm"
                variant="ghost"
                shape="square"
                icon={<Plus className="h-3.5 w-3.5" />}
                aria-label={`在「${group.title}」中添加网址`}
                title="添加网址"
                onClick={() => onAddItem?.(group)}
              />
              <Button
                size="sm"
                variant="ghost"
                shape="square"
                icon={<SortArrows className="h-3.5 w-3.5" />}
                aria-label={sortMode ? '退出排序' : '排序'}
                title={sortMode ? '完成排序' : '拖拽排序'}
                onClick={() => onToggleSort?.()}
                className={sortMode ? 'bg-kumo-recessed text-brand' : undefined}
              />
            </div>
          )}
          {/* 统计：数量 + 图标。
              不用 Kumo Badge —— 它的 variant 自带不透明底色，会盖住玻璃材质；
              这里改成与卡片同一材质的玻璃胶囊，只保留图标 + 数字的排版。 */}
          <div className="public-glass-item public-glass-static flex h-6 shrink-0 items-center gap-1 rounded-lg px-2 text-[11px] font-semibold text-kumo-strong">
            <Bookmark className="h-3 w-3" />
            {items.length}
          </div>
        </div>
      </div>
      {items.length === 0 ? (
        <div className="px-1 py-6 text-center text-sm text-kumo-subtle">这个分组还没有公开的网址。</div>
      ) : (
        <div
          className="grid py-1"
          style={gridStyleForDensity(density)}
          // 容器上监听 dragover：拖到卡片之间的空隙（不属于任何卡片）时
          // 清除落点高亮，避免高亮停留在最后经过的卡片上造成误导。
          onDragOver={sortMode ? (event) => {
            event.preventDefault();
            if (event.target === event.currentTarget) setOverId(null);
          } : undefined}
        >
          {items.map(item => (
            <BookmarkCard
              key={item.id}
              item={item}
              density={density}
              editable={editable}
              sortable={sortMode}
              dragging={String(draggingId) === String(item.id)}
              over={String(overId) === String(item.id)}
              onEdit={onEditItem}
              onDelete={onDeleteItem}
              onContextMenu={onItemContextMenu}
              onDragStart={event => handleDragStart(item, event)}
              onDragOver={event => handleDragOver(item, event)}
              onDrop={event => handleDrop(item.id, event)}
              onDragEnd={() => { setDraggingId(null); setOverId(null); }}
            />
          ))}
        </div>
      )}
    </section>
  );
}

function PublicBookmarksPage({ domainOnly = false, onDomainNotFound }) {
  const routeSlug = useMemo(() => normalizePublicPath(), []);
  const isAggregate = !domainOnly && isAggregateSlug(routeSlug);
  const surfaceRef = useCloudflareSpotlight();
  const isAuthenticated = useStore((state) => state.isAuthenticated);
  const [groups, setGroups] = useState([]);
  const [page, setPage] = useState(null);
  const [totalItems, setTotalItems] = useState(0);
  // 未公开的分组数量：用于明确告知「还有 N 个分组未公开」，
  // 避免用户以为分组在公开页里丢了。
  const [hiddenGroups, setHiddenGroups] = useState(0);
  /** 公开页全局配置（背景作用于整个页面，不是单个分组） */
  const [publicConfig, setPublicConfig] = useState({});
  /** 右键菜单状态 { x, y, item } */
  const [contextMenu, setContextMenu] = useState(null);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState('');
  const [density, setDensity] = useState(() => readStored(DENSITY_STORAGE_KEY, DENSITY_KEYS, DEFAULT_DENSITY));
  const [sort, setSort] = useState(() => readStored(SORT_STORAGE_KEY, SORT_VALUES, DEFAULT_SORT));
  const [keyword, setKeyword] = useState('');
  // 登录态就地编辑
  const [sortMode, setSortMode] = useState(false);
  const [itemDialogOpen, setItemDialogOpen] = useState(false);
  const [itemForm, setItemForm] = useState(() => emptyItemForm(0));

  const canEdit = Boolean(isAuthenticated && !domainOnly);

  const endpointFor = useCallback((sortValue) => {
    if (isAggregate) {
      return `/api/bookmarks/public/all/${encodeURIComponent(sortValue)}`;
    }
    if (routeSlug && !domainOnly) {
      return `/api/bookmarks/public/groups/${encodeURIComponent(routeSlug)}`;
    }
    return `/api/bookmarks/public/page-by-domain?domain=${encodeURIComponent(window.location.host)}`;
  }, [isAggregate, routeSlug, domainOnly]);

  const load = useCallback(async (sortValue = sort) => {
    setLoading(true);
    setError('');
    try {
      const response = await fetch(endpointFor(sortValue), { cache: 'no-store' });
      const result = await response.json().catch(() => ({}));
      if (!response.ok || result.success === false) {
        const err = new Error(result.error || '网址分组不存在或未公开');
        err.status = response.status;
        throw err;
      }
      const data = result.data || {};
      if (isAggregate) {
        const list = Array.isArray(data.groups) ? data.groups : [];
        setGroups(list);
        setPage(null);
        setHiddenGroups(Number(data.hidden_groups) || 0);
        setTotalItems(Number(data.total_items) || list.reduce((n, g) => n + (g.items?.length || 0), 0));
      } else {
        const group = data.group || data;
        setGroups(group ? [group] : []);
        setPage(group);
        setHiddenGroups(0);
        setTotalItems(Array.isArray(group?.items) ? group.items.length : 0);
      }
      // 全局背景配置（整个公开页共用）
      if (data.config && typeof data.config === 'object') setPublicConfig(data.config);
    } catch (err) {
      if (!routeSlug && domainOnly && err.status === 404 && onDomainNotFound) {
        onDomainNotFound();
        return;
      }
      setError(err.message || '公开页加载失败');
      setGroups([]);
      setPage(null);
    } finally {
      setLoading(false);
    }
  }, [endpointFor, isAggregate, routeSlug, domainOnly, onDomainNotFound, sort]);

  useEffect(() => { load(sort); }, [sort, load]);

  const pageTitle = isAggregate ? '全部网址' : (page?.title || '网址导航');

  useEffect(() => {
    const previousTitle = document.title;
    document.title = pageTitle;
    return () => { document.title = previousTitle; };
  }, [pageTitle]);

  useEffect(
    () => swapPublicPageFavicon(getPublicPageFaviconHref('bookmarks', page?.config)),
    [page?.config]
  );

  const updateGroupIcon = async (iconId) => {
    if (!page?.id) return;
    const response = await fetch(`/api/bookmarks/groups/${page.id}`, {
      method: 'PUT',
      headers: { 'Content-Type': 'application/json', ...(useStore.getState().getAuthHeaders?.() || {}) },
      body: JSON.stringify({
        title: page.title,
        description: page.description || '',
        icon: page.icon || '',
        public: true,
        slug: page.slug || '',
        domain: page.domain || '',
        cache_seconds: page.cache_seconds || 300,
        config: withPublicPageIconId(page.config, iconId),
      }),
    });
    const result = await response.json().catch(() => ({}));
    if (!response.ok || result.success === false) {
      throw new Error(result.error || '保存图标失败');
    }
    setPage((current) => (current ? { ...current, config: withPublicPageIconId(current.config, iconId) } : current));
    toast.success(iconId ? '分组图标已更新' : '已恢复默认图标');
  };

  const changeDensity = (value) => {
    setDensity(value);
    try { window.localStorage?.setItem(DENSITY_STORAGE_KEY, value); } catch { /* 忽略隐私模式下的写入失败 */ }
  };

  // --- 登录态就地编辑 ---
  // 所有写操作都带登录态请求头；未登录时后端返回 401，前端也不会渲染入口。
  // 请求统一走 /api/bookmarks（AuthSession），因此公开访客无法调用。

  const openAddItem = (group) => {
    setItemForm({ ...emptyItemForm(group.id), group_id: group.id });
    setItemDialogOpen(true);
  };

  const openEditItem = (item) => {
    setItemForm({
      id: item.id,
      group_id: item.group_id ?? page?.id ?? 0,
      title: item.title || '',
      url: item.url || '',
      description: item.description || '',
      icon_type: item.icon_type ?? 2,
      icon_src: item.icon_src || '',
      icon_text: item.icon_text || '',
      icon_bg_color: item.icon_bg_color || '',
      open_method: item.open_method ?? 2,
    });
    setItemDialogOpen(true);
  };

  const saveItem = async () => {
    const payload = {
      title: itemForm.title.trim(),
      url: itemForm.url.trim(),
      description: itemForm.description,
      icon_type: itemForm.icon_type,
      icon_src: itemForm.icon_src,
      icon_text: itemForm.icon_text,
      icon_bg_color: itemForm.icon_bg_color,
      open_method: itemForm.open_method,
    };
    try {
      if (itemForm.id) {
        await bookmarksRequest(`/items/${itemForm.id}`, { method: 'PUT', body: JSON.stringify(payload) });
        toast.success('网址已更新');
      } else {
        await bookmarksRequest('/items', {
          method: 'POST',
          body: JSON.stringify({ ...payload, group_id: itemForm.group_id }),
        });
        toast.success('网址已添加');
      }
      setItemDialogOpen(false);
      await load(sort);
    } catch (err) {
      toast.error(`保存失败：${err.message}`);
    }
  };

  const deleteItem = async (item) => {
    const confirmed = await dialog.deleteResource({
      title: '删除网址',
      message: `删除「${item.title}」？此操作不可恢复。`,
      confirmText: '删除',
      // 公开页面向访客/管理者快速清理，不要求输入名称二次确认。
      requireConfirmationInput: false,
    });
    if (!confirmed) return;
    try {
      await bookmarksRequest(`/items/${item.id}`, { method: 'DELETE' });
      toast.success('网址已删除');
      await load(sort);
    } catch (err) {
      toast.error(`删除失败：${err.message}`);
    }
  };

  // 拖拽排序：本地先反映结果，再持久化；失败则回滚提示。
  const reorderItems = async (group, nextItems) => {
    if (!group?.id) return;
    setGroups(current => current.map(entry =>
      entry.id === group.id ? { ...entry, items: nextItems } : entry));
    try {
      await bookmarksRequest('/items/sort', {
        method: 'POST',
        body: JSON.stringify({
          group_id: group.id,
          items: nextItems.map((item, index) => ({ id: item.id, sort: index })),
        }),
      });
    } catch (err) {
      toast.error(`排序保存失败：${err.message}`);
      await load(sort);
    }
  };

  // 搜索只过滤展示，不改变后端返回。
  const visibleGroups = useMemo(() => filterGroups(groups, keyword), [groups, keyword]);
  const visibleItemCount = useMemo(() => countItems(visibleGroups), [visibleGroups]);

  // 背景是「整个公开页」的全局配置，由公开端点随数据一起下发
  // （不再从分组 config 读取 —— 那是早期按分组存放的错误语义）。
  const backgroundConfig = useMemo(
    () => getBackgroundConfig(publicConfig),
    [publicConfig]
  );

  const bgLayerStyle = useMemo(() => backgroundLayerStyle(backgroundConfig), [backgroundConfig]);
  const bgOverlayStyle = useMemo(() => backgroundOverlayStyle(backgroundConfig), [backgroundConfig]);
  // 有自定义背景时，顶部按钮改用 ghost 样式、hero 文字转白
  const hasBg = Boolean(bgLayerStyle);
  // 内容区宽度由管理页配置（full = 铺满，与历史行为一致）
  const contentWidthClass = useMemo(() => pageWidthClass(publicConfig), [publicConfig]);
  // 搜索引擎列表同样由管理页配置；未配置时回落到内置默认
  const searchEngines = useMemo(() => resolveSearchEngines(publicConfig), [publicConfig]);

  return (
    <div
      ref={surfaceRef}
      className={cx(
        'cf-ai-background-surface public-bookmarks-page public-status-page relative isolate min-h-screen text-kumo-default',
        bgLayerStyle && 'has-custom-bg',
        isAuthenticated && 'is-authenticated'
      )}
      // 空白处右键：打开「布局」菜单。
      // 书签卡片自己的 onContextMenu 会 stopPropagation，因此卡片上右键仍走
      // 书签操作菜单，不会冒泡到这里被覆盖掉。未登录时保留浏览器默认菜单。
      onContextMenu={(event) => {
        if (!isAuthenticated) return;
        event.preventDefault();
        setContextMenu({ x: event.clientX, y: event.clientY, item: null });
      }}
    >
      {/* 自定义背景：独立覆盖层，不参与布局 */}
      {bgLayerStyle && <div aria-hidden="true" className="public-bookmarks-bg" style={bgLayerStyle} />}
      {bgOverlayStyle && <div aria-hidden="true" className="public-bookmarks-bg-overlay" style={bgOverlayStyle} />}
      {/* 无自定义背景时保留原有的点阵/光斑背景 */}
      {!bgLayerStyle && <div aria-hidden="true" className="cf-ai-background pointer-events-none absolute inset-0" />}
      <main className={cx('relative z-10 mx-auto flex min-h-screen w-full flex-col px-4 py-6 sm:px-6 lg:px-8', contentWidthClass)}>
        {/* 右上角操作区：刷新 + 主页。
            布局切换（密度）已移到「空白处右键」菜单里，排序下拉一并去掉。
            有自定义背景时统一用 ghost 样式，避免在背景图上突兀。 */}
        <div className="mb-2 flex flex-wrap items-center justify-end gap-2">
          <Button
            size="sm"
            variant={hasBg ? 'ghost' : 'secondary'}
            onClick={() => load(sort)}
            loading={loading}
            icon={<RefreshCw className="h-3.5 w-3.5" />}
            className={hasBg ? 'text-white hover:bg-white/15' : undefined}
          >
            刷新
          </Button>
          <Button
            size="sm"
            variant={hasBg ? 'ghost' : 'secondary'}
            onClick={() => { window.location.href = '/'; }}
            icon={isAuthenticated ? <Home className="h-3.5 w-3.5" /> : <LogIn className="h-3.5 w-3.5" />}
            aria-label={isAuthenticated ? '主页' : '登录'}
            title={isAuthenticated ? '跳转到主页' : '跳转到登录页'}
            className={hasBg ? 'text-white hover:bg-white/15' : undefined}
          >
            {isAuthenticated ? '主页' : '登录'}
          </Button>
        </div>

        {/* Hero：左侧 logo/站点名 + 右侧时钟 + 搜索框（二合一） */}
        <PublicHero
          title={pageTitle}
          siteName={pageTitle}
          logoUrl="/logo.svg"
          onBackground={hasBg}
          filterValue={keyword}
          onFilterChange={setKeyword}
          matchCount={keyword ? visibleItemCount : null}
          engines={searchEngines}
          enginesReady={!loading && !error}
          className="mb-6"
        />

        {loading && (
          <div className="flex flex-1 items-center justify-center py-24">
            <Loader size={28} />
          </div>
        )}

        {!loading && error && (
          <div className="public-status-card flex flex-1 flex-col items-center justify-center rounded-lg border border-kumo-interact/80 bg-kumo-base p-10 text-center">
            <AlertTriangle className="mb-3 h-9 w-9 text-kumo-warning" />
            <h1 className="text-lg font-semibold text-kumo-strong">无法显示网址导航</h1>
            <p className="mt-2 max-w-md text-sm leading-relaxed text-kumo-subtle">{error}</p>
            {!routeSlug && isLocalHost(window.location.host) && (
              <p className="mt-2 text-xs text-kumo-subtle">本地访问请使用 /bookmarks/slug 或 /bookmarks/all。</p>
            )}
          </div>
        )}

        {!loading && !error && (
          <div className="flex flex-col gap-4">
            {/* 非聚合模式：单个分组的标题与说明 */}
            {!isAggregate && page && (
              <section className="public-status-card rounded-lg border border-kumo-interact/80 bg-kumo-base px-4 py-3.5">
                <div className="flex flex-col gap-2 sm:flex-row sm:items-center sm:justify-between">
                  <div className="min-w-0">
                    <div className="text-base font-semibold text-kumo-strong">{page.title}</div>
                    {page.description && (
                      <p className="mt-1.5 max-w-3xl text-sm leading-relaxed text-kumo-subtle">{page.description}</p>
                    )}
                  </div>
                  <div className="flex shrink-0 items-center gap-2">
                    <Badge variant="secondary" className="h-7 w-fit justify-center gap-1 !text-[11px] font-semibold">
                      <Bookmark className="h-3 w-3" />
                      {totalItems} 个网址
                    </Badge>
                    <Button
                      size="sm"
                      variant="secondary"
                      onClick={() => { window.location.href = `/bookmarks/${AGGREGATE_SLUG}`; }}
                      title="查看全部公开分组"
                    >
                      查看全部
                    </Button>
                  </div>
                </div>
              </section>
            )}

            {/* 布局/排序切换已移到右上角按钮组；搜索框在顶部 hero。
                这里不再需要工具条，保持页面干净。 */}

            {visibleGroups.length === 0 ? (
              <section className="public-glass flex flex-col items-center justify-center rounded-xl p-10 text-center">
                <Bookmark className="mb-3 h-8 w-8 text-kumo-inactive" />
                <h2 className="text-sm font-semibold text-kumo-strong">
                  {groups.length === 0
                    ? (isAggregate ? '还没有公开的网址分组' : '这个分组还没有公开的网址')
                    : '没有匹配的网址'}
                </h2>
                <p className="mt-1.5 max-w-md text-xs leading-relaxed text-kumo-subtle">
                  {groups.length === 0
                    ? (isAggregate
                      ? '在「网址导航 → 公开」中把分组设为公开后，就会出现在这里。'
                      : '在「网址导航 → 公开」中把该分组设为公开后即可访问。')
                    : '换个关键词试试。'}
                </p>
                {isAggregate && groups.length === 0 && hiddenGroups > 0 && (
                  <p className="mt-2 text-xs font-medium text-kumo-warning">
                    当前有 {hiddenGroups} 个分组处于私有状态。
                  </p>
                )}
              </section>
            ) : (
              visibleGroups.map(group => (
                <GroupSection
                  key={group.id ?? group.title}
                  group={group}
                  density={density}
                  editable={canEdit}
                  sortMode={sortMode}
                  onAddItem={openAddItem}
                  onEditItem={openEditItem}
                  onDeleteItem={deleteItem}
                  onToggleSort={() => setSortMode(current => !current)}
                  onReorder={reorderItems}
                  onItemContextMenu={(event, item) => setContextMenu({ x: event.clientX, y: event.clientY, item })}
                />
              ))
            )}

            <footer className={cx(
              // 只剩「最后更新」一项，用 justify-end 让它靠右，与上方内容右缘对齐
              'flex flex-col gap-2 py-4 text-xs text-kumo-subtle sm:flex-row sm:items-center sm:justify-end',
              // 背景图模式下加阴影，避免浅色页脚文字糊在底图上
              hasBg && 'public-on-bg-shadow text-white/80'
            )}>
              {/* 原先的「由 API Monitor 提供」已按需求移除，只保留更新时间 */}
              {page?.updated_at && <span>最后更新：{formatDateTime(page.updated_at || page.created_at)}</span>}
            </footer>
          </div>
        )}
      </main>

      {/* 登录态就地编辑：与管理页共用同一套表单 */}
      {canEdit && (
        <ItemFormDialog
          open={itemDialogOpen}
          form={itemForm}
          request={bookmarksRequest}
          groups={isAggregate ? groups.map(group => ({ id: group.id, title: group.title })) : null}
          onOpenChange={(open) => {
            setItemDialogOpen(open);
            if (!open) setItemForm(emptyItemForm(0));
          }}
          onFormChange={setItemForm}
          onSave={saveItem}
        />
      )}

      {/* 右键菜单：
          卡片上右键 → 打开 / 编辑 / 删除（仅登录态，canEdit）；
          空白处 → 布局切换（登录态即可用，不要求 canEdit，因为 domainOnly 下也要能调密度）。
          两种场景共用一个菜单组件，由 state.item 是否为空区分。 */}
      {isAuthenticated && (
        <BookmarkContextMenu
          state={contextMenu}
          onClose={() => setContextMenu(null)}
          onEdit={openEditItem}
          onDelete={deleteItem}
          onOpen={(item) => {
            const raw = item?.url || '';
            if (/^https?:\/\//i.test(raw)) window.open(raw, '_blank', 'noopener,noreferrer');
          }}
          density={density}
          densityOptions={DENSITY_OPTIONS}
          onDensityChange={changeDensity}
        />
      )}
    </div>
  );
}

export default PublicBookmarksPage;
