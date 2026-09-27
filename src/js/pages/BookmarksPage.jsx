import React, { useCallback, useEffect, useMemo, useRef, useState } from 'react';
import { ClipboardText, Empty, LayerCard, Loader, Tabs, Toolbar } from '@cloudflare/kumo';
import { Button } from '@cloudflare/kumo/components/button';
import { LayerDialog } from '@cloudflare/kumo/components/layer-dialog';
import { Input, Textarea } from '@cloudflare/kumo/components/input';
import { Select } from '@cloudflare/kumo/components/select';
import { Switch } from '@cloudflare/kumo/components/switch';
import useStore from '../store.js';
import { MODULE_TABS_PROPS } from '../modules/kumoTabs.js';
import { dialog } from '../modules/dialog.js';
import { toast } from '../modules/toast.js';
import { ResponsiveSearchInput, SectionCard, cx, iconButtonIconClass, sectionCardHeaderClass, stickyTabsBaseClass } from '../components/ui/AppPrimitives.jsx';
import ItemFormDialog, { emptyItemForm } from './bookmarks/ItemFormDialog.jsx';
import ItemIcon from './bookmarks/ItemIcon.jsx';
import BackgroundSettings from './bookmarks/BackgroundSettings.jsx';
import SearchEnginesSettings from './bookmarks/SearchEnginesSettings.jsx';
import { buildPublicSettingsPayload } from '../modules/publicPageBackground.js';
import { Bookmark, ChevronDown, ChevronUp, Copy, Download, Edit, ExternalLink, Folder, Globe, Image, Menu, Plus, Save, Search, Trash, Upload } from '../components/Icons.jsx';
import SunPanelImportDialog from './bookmarks/SunPanelImportDialog.jsx';

const API = '/api/bookmarks';


const TABS = [
  { value: 'navigate', label: <span className="inline-flex items-center gap-1.5"><Bookmark className="h-3.5 w-3.5" />导航</span> },
  { value: 'all', label: <span className="inline-flex items-center gap-1.5"><Menu className="h-3.5 w-3.5" />全部网址</span> },
  { value: 'public', label: <span className="inline-flex items-center gap-1.5"><Globe className="h-3.5 w-3.5" />公开</span> },
];

async function apiFetch(path, options = {}) {
  const response = await fetch(`${API}${path}`, {
    ...options,
    headers: { ...(useStore.getState().getAuthHeaders?.() || {}), ...options.headers },
  });
  if (!response.ok && response.status !== 204) {
    const payload = await response.json().catch(() => ({}));
    const error = new Error(payload.message || payload.error || `HTTP ${response.status}`);
    error.status = response.status;
    error.payload = payload;
    throw error;
  }
  return response.json().catch(() => ({}));
}

// 原生备份文件的格式标识（与后端 exportFormatName 对应）。
// 导入前用它做一次本地粗判，把「选错文件」这类失误在弹窗里直接说清楚，
// 而不是等后端返回 400。
const NATIVE_EXPORT_FORMAT = 'api-monitor.bookmarks';

const emptyGroupForm = () => ({
  id: 0,
  title: '',
  description: '',
  public: false,
  slug: '',
  domain: '',
  cache_seconds: 300,
  config: {},
});

const normalizeSlug = (value) => String(value || '')
  .toLowerCase()
  .replace(/[^a-z0-9]+/g, '-')
  .replace(/^-+|-+$/g, '');

const normalizeDomain = (value) => String(value || '')
  .trim()
  .toLowerCase()
  .replace(/^https?:\/\//, '')
  .split('/')[0];

// 条目图标：渲染规则统一在 ItemIcon（公开页、管理页、编辑预览共用）。
// 管理页的列表里用固定 40px 盒子；图片加载失败会回退到占位图标
// （此前这里只是把 <img> 隐藏，留下一个空盒子）。
function renderItemIcon(item, fallback) {
  return <ItemIcon item={item} size="h-10 w-10" className="text-kumo-subtle" fallback={fallback} />;
}


function GroupSettingsDialog({ open, form, onOpenChange, onFormChange, onSave }) {
  // 与 ItemFormDialog 保持一致：函数式更新，避免同一次事件里连调两次丢字段
  const setField = (key, value) => onFormChange(prev => ({ ...(prev ?? form), [key]: value }));
  const canSave = form.title.trim();
  const publicUrl = form.public && form.slug ? `${window.location.origin}/bookmarks/${encodeURIComponent(form.slug)}` : '';

  const copyLink = async () => {
    try {
      await navigator.clipboard.writeText(publicUrl);
      toast.success('公开链接已复制');
    } catch {
      toast.error('复制失败');
    }
  };

  return (
    <LayerDialog.Root open={open} onOpenChange={onOpenChange}>
      <LayerDialog.Content size="sm">
        <LayerDialog.Title>分组公开设置</LayerDialog.Title>
        <LayerDialog.Body>
        <div className="space-y-3">
          <Input
            size="sm"
            label="分组名称"
            value={form.title}
            onChange={event => setField('title', event.target.value)}
          />
          <Textarea
            size="sm"
            label="描述"
            rows={2}
            value={form.description}
            onChange={event => setField('description', event.target.value)}
          />
          <div className="flex items-center justify-between gap-3 rounded-lg border border-kumo-line bg-kumo-recessed/30 p-3">
            <div className="min-w-0">
              <div className="text-sm font-semibold text-kumo-strong">公开访问</div>
              <div className="mt-1 text-xs text-kumo-subtle">开启后可通过公开链接免登录访问此分组的网址。</div>
            </div>
            <Switch checked={!!form.public} onCheckedChange={checked => setField('public', checked)} />
          </div>
          {form.public && (
            <>
              <div>
                <div className="mb-1 text-xs font-medium text-kumo-strong">公开链接标识（slug）</div>
                <div className="flex items-start gap-2">
                  <Input
                    size="sm"
                    className="flex-1"
                    placeholder="例如：nav"
                    value={form.slug}
                    onChange={event => setField('slug', event.target.value)}
                  />
                  <Button size="sm" variant="secondary" disabled={!form.title} onClick={() => setField('slug', normalizeSlug(form.title))}>
                    自动生成
                  </Button>
                </div>
              </div>
              <Input
                size="sm"
                label="自定义域名（可选）"
                placeholder="nav.example.com"
                value={form.domain}
                onChange={event => setField('domain', event.target.value)}
              />
              <div className="flex items-center justify-between gap-3 rounded-lg border border-kumo-line bg-kumo-recessed/30 p-3">
                <div className="min-w-0">
                  <div className="text-xs font-medium text-kumo-strong">公开链接</div>
                  <div className="mt-1 truncate text-xs text-kumo-subtle">{publicUrl || '保存后生成公开链接'}</div>
                </div>
                {publicUrl && (
                  <Button size="sm" variant="secondary" icon={<Copy className="h-3.5 w-3.5" />} onClick={copyLink}>
                    复制
                  </Button>
                )}
              </div>
            </>
          )}
        </div>
        </LayerDialog.Body>
        <LayerDialog.Actions dismissLabel="取消">
          <LayerDialog.Actions.Primary type="button" disabled={!canSave} onClick={() => onSave()}>保存</LayerDialog.Actions.Primary>
        </LayerDialog.Actions>
      </LayerDialog.Content>
    </LayerDialog.Root>
  );
}

export default function BookmarksPage() {
  const [activeTab, setActiveTab] = useState('navigate');
  const [groups, setGroups] = useState([]);
  const [search, setSearch] = useState('');
  const [itemDialogOpen, setItemDialogOpen] = useState(false);
  const [itemForm, setItemForm] = useState(emptyItemForm(0));
  const [groupDialogOpen, setGroupDialogOpen] = useState(false);
  const [groupForm, setGroupForm] = useState(emptyGroupForm());
  // 加载态：此前没有，首次挂载与导入后的刷新都会先闪一下「还没有网址分组」空状态。
  const [loading, setLoading] = useState(true);
  // 导入（SunPanel）
  const [importOpen, setImportOpen] = useState(false);
  const [importText, setImportText] = useState('');
  const [importFileName, setImportFileName] = useState('');
  const [importGroupMode, setImportGroupMode] = useState('new');
  const [importTargetGroupId, setImportTargetGroupId] = useState(0);
  const [importOptions, setImportOptions] = useState({ skipLocalIcons: true, includeSystemCards: false });
  const [importing, setImporting] = useState(false);
  const importFileInputRef = useRef(null);
  // 原生格式的导出/导入（与 SunPanel 导入区分开）
  const [exporting, setExporting] = useState(false);
  // 导入弹窗：与「主机实例」页同一套交互 ——
  // 弹窗里选文件、识别格式、选模式、预览后再确认，而不是直接选文件就执行。
  const [nativeImportOpen, setNativeImportOpen] = useState(false);
  const [nativeImportFileName, setNativeImportFileName] = useState('');
  const [nativeImportPayload, setNativeImportPayload] = useState(null);
  const [nativeImportMode, setNativeImportMode] = useState('merge');
  const [nativeImportPreview, setNativeImportPreview] = useState(null);
  const [nativeImportError, setNativeImportError] = useState('');
  const [nativeImportSaving, setNativeImportSaving] = useState(false);
  const [fetchingFavicons, setFetchingFavicons] = useState(false);
  /** 公开页全局配置（背景作用于整个公开页，与分组无关） */
  const [publicBgConfig, setPublicBgConfig] = useState({});
  // 公开页设置保存的防抖计时器（见 saveGlobalBackground）
  const publicSettingsTimer = useRef(null);
  // 防抖窗口内尚未落盘的配置，卸载时补写用
  const publicSettingsPending = useRef(null);
  // 正在抓取图标的条目 id（卡片上显示骨架/转圈），以及整体进度。
  const [fetchingIds, setFetchingIds] = useState(() => new Set());
  const [fetchProgress, setFetchProgress] = useState(null);
  const load = useCallback(async () => {
    try {
      const data = await apiFetch('/groups');
      setGroups(data.groups || []);
    } catch (error) {
      toast.error(`加载失败：${error.message}`);
    } finally {
      setLoading(false);
    }
  }, []);

  // 公开页全局背景单独读取（不属于任何分组）
  const loadPublicSettings = useCallback(async () => {
    try {
      const payload = await apiFetch('/public-settings');
      const config = payload.data?.config;
      if (config && typeof config === 'object') setPublicBgConfig(config);
    } catch {
      // 读取失败不阻断页面：背景留空即可
    }
  }, []);

  useEffect(() => {
    load();
    loadPublicSettings();
  }, [load, loadPublicSettings]);

  const keyword = search.trim().toLowerCase();

  const filteredGroups = useMemo(() => {
    if (!keyword) return groups;
    return groups.map(group => {
      const matchedItems = (group.items || []).filter(item =>
        item.title?.toLowerCase().includes(keyword)
        || item.url?.toLowerCase().includes(keyword)
        || item.description?.toLowerCase().includes(keyword)
      );
      const groupMatched = group.title?.toLowerCase().includes(keyword) || group.description?.toLowerCase().includes(keyword);
      if (groupMatched) return { ...group, items: group.items || [] };
      if (matchedItems.length > 0) return { ...group, items: matchedItems };
      return null;
    }).filter(Boolean);
  }, [groups, keyword]);

  // 公开/私有分组计数，供「公开」页签显示状态汇总。
  const publicGroupCount = useMemo(
    () => groups.filter(group => group.public).length,
    [groups]
  );

  const allItems = useMemo(() => {
    const flattened = groups.flatMap(group => (group.items || []).map(item => ({ ...item, group_title: group.title })));
    if (!keyword) return flattened;
    return flattened.filter(item =>
      item.title?.toLowerCase().includes(keyword)
      || item.url?.toLowerCase().includes(keyword)
      || item.description?.toLowerCase().includes(keyword)
      || item.group_title?.toLowerCase().includes(keyword)
    );
  }, [groups, keyword]);

  const createGroup = async () => {
    const title = await dialog.prompt({
      title: '新建分组',
      message: '输入分组名称',
      placeholder: '例如：常用工具',
    });
    if (!title?.trim()) return;
    try {
      await apiFetch('/groups', { method: 'POST', body: JSON.stringify({ title: title.trim() }) });
      await load();
      toast.success('分组已创建');
    } catch (error) {
      toast.error(`创建失败：${error.message}`);
    }
  };

  const renameGroup = async group => {
    const title = await dialog.prompt({
      title: '重命名分组',
      message: '输入新的分组名称',
      defaultValue: group.title,
    });
    if (!title?.trim() || title.trim() === group.title) return;
    try {
      await apiFetch(`/groups/${group.id}`, {
        method: 'PUT',
        body: JSON.stringify({
          title: title.trim(),
          description: group.description || '',
          public: !!group.public,
          slug: group.slug || '',
          domain: group.domain || '',
          cache_seconds: group.cache_seconds || 300,
          config: group.config || {},
        }),
      });
      await load();
      toast.success('分组已更新');
    } catch (error) {
      toast.error(`更新失败：${error.message}`);
    }
  };

  const deleteGroup = async group => {
    const confirmed = await dialog.deleteResource({
      title: '删除分组',
      message: `删除分组「${group.title}」会同时删除组内 ${(group.items || []).length} 个网址，此操作不可恢复。`,
      confirmText: '删除分组',
    });
    if (!confirmed) return;
    try {
      await apiFetch(`/groups/${group.id}`, { method: 'DELETE' });
      await load();
      toast.success('分组已删除');
    } catch (error) {
      toast.error(`删除失败：${error.message}`);
    }
  };

  const openGroupSettings = group => {
    setGroupForm({
      id: group.id,
      title: group.title,
      description: group.description || '',
      public: !!group.public,
      slug: group.slug || '',
      domain: group.domain || '',
      cache_seconds: group.cache_seconds || 300,
      config: group.config || {},
    });
    setGroupDialogOpen(true);
  };

  const saveGroupSettings = async () => {
    try {
      await apiFetch(`/groups/${groupForm.id}`, { method: 'PUT', body: JSON.stringify(groupForm) });
      setGroupDialogOpen(false);
      await load();
      toast.success('分组已更新');
    } catch (error) {
      toast.error(`保存失败：${error.message}`);
    }
  };

  const openPublicGroup = group => {
    if (group.slug) {
      window.open(`/bookmarks/${encodeURIComponent(group.slug)}`, '_blank', 'noopener,noreferrer');
    }
  };

  // 聚合页固定地址：/bookmarks/all（后端把 "all" 作为保留 slug）。
  const openPublicAll = () => {
    window.open('/bookmarks/all', '_blank', 'noopener,noreferrer');
  };

  // 公开页全局背景/搜索引擎：加载与保存都走 public-settings（不再存进分组 config）。
  // 发请求前必须用 buildPublicSettingsPayload 展开成完整字段：后端是局部更新语义，
  // 缺键 = 保留旧值，按精简 config 直接 PUT 会导致「选回默认值」无法生效。
  //
  // 背景面板与搜索引擎面板都是「边输入边回调 onChange」，直接每次都 PUT 会变成
  // 一个字符一个请求、一次成功提示（还会互相抢顺序）。这里统一做防抖：
  // 输入停止 600ms 后才发一次，期间的新改动覆盖上一次待发请求。
  const saveGlobalBackground = nextConfig => {
    setPublicBgConfig(nextConfig);
    publicSettingsPending.current = nextConfig;
    if (publicSettingsTimer.current) clearTimeout(publicSettingsTimer.current);
    publicSettingsTimer.current = setTimeout(async () => {
      publicSettingsTimer.current = null;
      publicSettingsPending.current = null;
      try {
        const payload = await apiFetch('/public-settings', {
          method: 'PUT',
          body: JSON.stringify({ config: buildPublicSettingsPayload(nextConfig) }),
        });
        if (payload.success === false) throw new Error(payload.error || '保存失败');
        toast.success('公开页设置已保存');
      } catch (error) {
        toast.error(`保存失败：${error.message}`);
        // 保存失败时回读服务端真实状态，避免界面停留在未保存的乐观值上
        loadPublicSettings();
      }
    }, 600);
  };

  // 卸载时若还有防抖窗口内的改动，立刻补一次写入，避免「改完立刻切页」丢设置。
  useEffect(() => () => {
    if (publicSettingsTimer.current) clearTimeout(publicSettingsTimer.current);
    if (publicSettingsPending.current) {
      apiFetch('/public-settings', {
        method: 'PUT',
        body: JSON.stringify({ config: buildPublicSettingsPayload(publicSettingsPending.current) }),
      }).catch(() => {});
    }
  }, []);
  const getPublicAllUrl = () => `${window.location.origin}/bookmarks/all`;

  // 单个分组快速切换公开状态（无需进设置）。
  const toggleGroupPublic = async group => {
    const next = !group.public;
    try {
      await apiFetch('/groups/public', {
        method: 'POST',
        body: JSON.stringify({ public: next, ids: [group.id] }),
      });
      await load();
      toast.success(next ? `「${group.title}」已公开` : `「${group.title}」已取消公开`);
    } catch (error) {
      toast.error(`${next ? '公开' : '取消公开'}失败：${error.message}`);
    }
  };

  // 一键公开/取消公开全部分组。
  const setAllGroupsPublic = async next => {
    const count = groups.length;
    if (count === 0) {
      toast.error('还没有分组');
      return;
    }
    const confirmed = await dialog.confirm({
      title: next ? '全部公开' : '全部取消公开',
      message: next
        ? `将把全部 ${count} 个分组设为公开，任何拿到链接的人都能访问这些网址。`
        : `将把全部 ${count} 个分组设为私有，公开页将不再显示任何内容。`,
      confirmText: next ? '全部公开' : '全部取消公开',
    });
    if (!confirmed) return;
    try {
      const result = await apiFetch('/groups/public', {
        method: 'POST',
        body: JSON.stringify({ public: next }),
      });
      if (result.success === false) throw new Error(result.error || '操作失败');
      const updated = result.data?.updated ?? 0;
      const fixed = result.data?.slugs_fixed ?? 0;
      await load();
      toast.success(
        next
          ? `已公开 ${updated} 个分组${fixed ? `，补全 ${fixed} 个访问地址` : ''}`
          : `已取消公开 ${updated} 个分组`
      );
    } catch (error) {
      toast.error(`操作失败：${error.message}`);
    }
  };

  const getPublicGroupUrl = group => (
    group?.public && group?.slug ? `${window.location.origin}/bookmarks/${encodeURIComponent(group.slug)}` : ''
  );

  const getPublicGroupDomainUrl = group => (group?.domain ? `https://${group.domain}` : '');

  const selectGroupForPublic = group => {
    if (!group) return;
    setGroupForm({
      id: group.id,
      title: group.title,
      description: group.description || '',
      public: !!group.public,
      slug: group.slug || '',
      domain: group.domain || '',
      cache_seconds: group.cache_seconds || 300,
      config: group.config || {},
    });
  };

  // 移动分组：索引来自 filteredGroups（搜索时是子集），但交换必须作用在
  // 完整列表上，否则会把错误的两个分组对调并把错误顺序持久化到服务端。
  const moveGroup = async (index, direction) => {
    const visible = filteredGroups[index];
    if (!visible) return;
    const fullIndex = groups.findIndex(group => group.id === visible.id);
    if (fullIndex < 0) return;
    const target = fullIndex + direction;
    if (target < 0 || target >= groups.length) return;
    const next = [...groups];
    [next[fullIndex], next[target]] = [next[target], next[fullIndex]];
    try {
      await apiFetch('/groups/sort', {
        method: 'POST',
        body: JSON.stringify({ items: next.map((group, idx) => ({ id: group.id, sort: idx })) }),
      });
      await load();
    } catch (error) {
      toast.error(`排序保存失败：${error.message}`);
    }
  };

  const openItemForm = (group, item = null) => {
    const base = item ? { ...item } : emptyItemForm(group.id);
    setItemForm(base);
    setItemDialogOpen(true);
  };

  // 批量抓取 favicon：给还没有图标的网址自动拉取网站图标。
  // 复用后端已有的抓取逻辑（解析 <link rel=icon> -> 下载 -> 本地缓存）。
  const fetchAllFavicons = async () => {
    const all = groups.flatMap(group => (group.items || []).map(item => ({ ...item })));
    if (all.length === 0) {
      toast.error('还没有网址');
      return;
    }
    const missing = all.filter(item => !String(item.icon_src || '').trim());
    const targets = missing.length > 0 ? missing : all;

    const confirmed = await dialog.confirm({
      title: '抓取网站图标',
      message: missing.length > 0
        ? `将为 ${targets.length} 个还没有图标的网址抓取网站 favicon，已有图标的 ${all.length - missing.length} 个会跳过。`
        : `所有 ${all.length} 个网址都已有图标。是否重新抓取（覆盖现有图标）？`,
      confirmText: missing.length > 0 ? '开始抓取' : '全部重新抓取',
    });
    if (!confirmed) return;

    setFetchingFavicons(true);
    try {
      // 分批抓取：服务端每批限并发 + 总超时，这里循环推进。
      // 失败的条目（站点 502 / 超时）通过 skip_ids 排除，否则它们仍然
      // 「没有图标」，会永远排在队首，导致后面的条目永远轮不到。
      // 抓成功的条目也必须移出待办，否则下一轮又从队首开始取，永远走不完。
      // 「全部重新抓取」时必须显式 overwrite：服务端在 overwrite=false 时
      // 无论如何都会按「只处理没有图标的条目」过滤，等于什么都不做。
      const overwriteIcons = missing.length === 0;
      const pendingIds = targets.map(item => item.id);
      const skipIds = new Set();
      const doneIds = new Set();
      let succeeded = 0;
      const failures = [];
      const MAX_ROUNDS = 20;
      // 正在抓取的条目 id：卡片上显示 loading，让用户看到进度而不是只转一个按钮。
      const inFlight = new Set();

      setFetchProgress({ total: pendingIds.length, done: 0, active: 0 });
      const refreshProgress = () => setFetchProgress({
        total: pendingIds.length,
        done: skipIds.size + succeeded,
        active: inFlight.size,
      });

      for (let round = 0; round < MAX_ROUNDS; round += 1) {
        const remaining = pendingIds.filter(id => !skipIds.has(id) && !doneIds.has(id));
        if (remaining.length === 0) break;

        const batch = remaining.slice(0, 25);
        batch.forEach(id => inFlight.add(id));
        setFetchingIds(new Set(inFlight));
        refreshProgress();

        let payload;
        try {
          payload = await apiFetch('/favicons/fetch-batch', {
            method: 'POST',
            body: JSON.stringify({
              ids: batch,
              only_missing: !overwriteIcons,
              overwrite: overwriteIcons,
              limit: 25,
              skip_ids: [...skipIds],
            }),
          });
        } finally {
          batch.forEach(id => inFlight.delete(id));
          setFetchingIds(new Set(inFlight));
        }
        if (payload.success === false) throw new Error(payload.error || '抓取失败');
        const data = payload.data || {};
        const results = data.results || [];
        if (results.length === 0) break;

        for (const res of results) {
          if (res.error) {
            failures.push(res);
            skipIds.add(res.id);   // 本轮失败不重试，让后面的条目轮得到
          } else {
            succeeded += 1;
            doneIds.add(res.id);
          }
        }
        await load();   // 每轮刷新，显示已抓到的图标
        refreshProgress();
      }

      // 单次点击最多处理 MAX_ROUNDS * 25 条；超出的部分必须说清楚，
      // 否则「已抓取 N 个」会被当成全部完成。
      const untouched = pendingIds.filter(id => !skipIds.has(id) && !doneIds.has(id)).length;

      await load();
      if (untouched > 0) {
        toast.warning(
          `已抓取 ${succeeded} 个，失败 ${failures.length} 个，另有 ${untouched} 个本轮未处理（单次上限 ${MAX_ROUNDS * 25} 个），可再次点击继续`
        );
      } else if (failures.length > 0) {
        const sample = failures[0];
        toast.warning(
          `已抓取 ${succeeded} 个，失败 ${failures.length} 个（例：${sample.title}——${sample.error}）`
        );
      } else {
        toast.success(`已抓取 ${succeeded} 个网站图标`);
      }
    } catch (error) {
      toast.error(`抓取失败：${error.message}`);
    } finally {
      setFetchingFavicons(false);
      setFetchingIds(new Set());
      setFetchProgress(null);
    }
  };

  const openImportDialog = () => {
    setImportText('');
    setImportFileName('');
    setImportGroupMode('new');
    setImportTargetGroupId(0);
    setImportOptions({ skipLocalIcons: true, includeSystemCards: false });
    setImportOpen(true);
  };

  const submitImport = async () => {
    if (!importText.trim()) return;
    setImporting(true);
    try {
      const payload = {
        payload: importText,
        group_mode: importGroupMode,
        skip_local_icons: importOptions.skipLocalIcons,
        include_system_cards: importOptions.includeSystemCards,
      };
      if (importGroupMode === 'single') {
        if (!importTargetGroupId) throw new Error('请选择目标分组');
        payload.target_group_id = importTargetGroupId;
      }

      // 先 dry-run 取得将要发生的事，再让用户确认（批量写入不可误触）。
      const preview = await apiFetch('/import', { method: 'POST', body: JSON.stringify({ ...payload, dry_run: true }) });
      if (preview.success === false) throw new Error(preview.error || '解析失败');
      const previewReport = preview.data?.report || {};

      const confirmed = await dialog.confirm({
        title: '确认导入',
        message: `将创建 ${previewReport.groups_created || 0} 个分组、导入 ${previewReport.items_imported || 0} 个网址`
          + (previewReport.items_skipped ? `，跳过 ${previewReport.items_skipped} 个条目。` : '。')
          + '导入在单事务内完成，失败会整体回滚。',
        confirmText: '开始导入',
      });
      if (!confirmed) return;

      const result = await apiFetch('/import', { method: 'POST', body: JSON.stringify(payload) });
      if (result.success === false) throw new Error(result.error || '导入失败');
      const report = result.data?.report || {};

      await load();
      setImportOpen(false);
      const skipped = report.items_skipped || 0;
      if (skipped > 0) {
        toast.warning(`导入完成：新增 ${report.groups_created || 0} 个分组、${report.items_imported || 0} 个网址，跳过 ${skipped} 个条目`);
      } else {
        toast.success(`导入完成：新增 ${report.groups_created || 0} 个分组、${report.items_imported || 0} 个网址`);
      }
    } catch (error) {
      toast.error(`导入失败：${error.message}`);
    } finally {
      setImporting(false);
    }
  };

  // —— 原生格式的导出 / 导入 ——
  // 与上面的 SunPanel 导入并存：那个只认 SunPanel 的 .sun-panel.json，
  // 这里用本项目自己的完整格式，能无损往返（含分组公开状态与公开页外观）。

  /** 导出为 JSON 文件并触发下载。 */
  const exportBookmarks = async () => {
    setExporting(true);
    try {
      const payload = await apiFetch('/export');
      if (payload.success === false) throw new Error(payload.error || '导出失败');
      const data = payload.data || {};

      const json = JSON.stringify(data, null, 2);
      const blob = new Blob([json], { type: 'application/json;charset=utf-8' });
      const url = URL.createObjectURL(blob);
      const stamp = new Date().toISOString().slice(0, 10);
      const a = document.createElement('a');
      a.href = url;
      a.download = `bookmarks-${stamp}.json`;
      document.body.appendChild(a);
      a.click();
      a.remove();
      // 立刻 revoke 会让部分浏览器来不及下载，延后释放
      window.setTimeout(() => URL.revokeObjectURL(url), 1000);

      const stats = data.stats || {};
      toast.success(`已导出 ${stats.groups ?? 0} 个分组、${stats.items ?? 0} 个网址`);
    } catch (error) {
      toast.error(`导出失败：${error.message}`);
    } finally {
      setExporting(false);
    }
  };

  /** 打开导入弹窗并重置全部临时状态（避免上次的文件/预览残留）。 */
  const openNativeImportDialog = () => {
    setNativeImportFileName('');
    setNativeImportPayload(null);
    setNativeImportMode('merge');
    setNativeImportPreview(null);
    setNativeImportError('');
    setNativeImportSaving(false);
    setNativeImportOpen(true);
  };

  /**
   * 读取并解析选中的备份文件。
   *
   * 这里只做本地解析与格式粗判；解析通过后立刻用当前模式跑一次 dry-run，
   * 否则预览区一直是空的、「确认导入」按钮永远禁用（用户无法在默认的
   * 合并模式之外再触发一次预览）。用户之后切换模式会重新预览。
   */
  const processNativeImportFile = async (file) => {
    if (!file) return;
    setNativeImportFileName(file.name);
    setNativeImportPayload(null);
    setNativeImportPreview(null);
    setNativeImportError('');
    try {
      const text = await file.text();
      let parsed;
      try {
        parsed = JSON.parse(text);
      } catch {
        throw new Error('文件不是合法的 JSON');
      }
      // 提前拦住「选错文件」这个最常见的失误：本项目的导出格式有固定标识，
      // 直接给出提示比等后端返回 400 更清楚。
      const format = parsed && typeof parsed === 'object' ? parsed.format : '';
      if (format !== NATIVE_EXPORT_FORMAT) {
        throw new Error(
          `不是网址导航的备份文件（format=${format || '缺失'}，期望 ${NATIVE_EXPORT_FORMAT}）。`
          + '若这是 SunPanel 的导出文件，请用「导入」旁的 SunPanel 入口。'
        );
      }
      if (!Array.isArray(parsed.groups)) {
        throw new Error('备份文件缺少 groups 字段');
      }
      setNativeImportPayload(parsed);
      // parsed 直接传进去：此刻 state 里的 nativeImportPayload 还是旧值。
      await runNativeImportPreview(nativeImportMode, parsed);
    } catch (error) {
      setNativeImportError(error.message);
    }
  };

  /** 按当前模式跑 dry-run，拿到「将要发生什么」用于确认。 */
  const runNativeImportPreview = async (mode = nativeImportMode, payload = nativeImportPayload) => {
    if (!payload) return;
    setNativeImportSaving(true);
    setNativeImportError('');
    try {
      const result = await apiFetch('/import-native', {
        method: 'POST',
        body: JSON.stringify({ payload, mode, dry_run: true }),
      });
      if (result.success === false) throw new Error(result.error || '解析失败');
      setNativeImportPreview(result.data || {});
    } catch (error) {
      setNativeImportError(error.message);
      setNativeImportPreview(null);
    } finally {
      setNativeImportSaving(false);
    }
  };

  const confirmNativeImport = async () => {
    if (!nativeImportPayload) return;

    // 替换是破坏性操作，先确认再执行
    if (nativeImportMode === 'replace') {
      const confirmed = await dialog.confirm({
        title: '替换导入',
        // dialog 的 message 是纯文本渲染（whitespace-pre-wrap），不支持 markdown
        message: '将先清空当前所有分组与网址，再导入文件内容。\n此操作不可恢复，建议先导出备份。',
        confirmText: '继续',
      });
      if (!confirmed) return;
    }

    setNativeImportSaving(true);
    setNativeImportError('');
    try {
      const result = await apiFetch('/import-native', {
        method: 'POST',
        body: JSON.stringify({ payload: nativeImportPayload, mode: nativeImportMode }),
      });
      if (result.success === false) throw new Error(result.error || '导入失败');
      const done = result.data || {};

      await load();
      await loadPublicSettings();
      setNativeImportOpen(false);

      const skipped = done.items_skipped || 0;
      const summary = `导入完成：新增 ${done.groups_created || 0} 个分组、${done.items_imported || 0} 个网址`;
      if (skipped > 0) toast.warning(`${summary}，跳过 ${skipped} 个条目`);
      else toast.success(summary);
      // 后端可能对 slug 冲突等做了降级处理，这些信息不该被吞掉
      for (const warning of (done.warnings || []).slice(0, 3)) toast.warning(warning);
    } catch (error) {
      setNativeImportError(error.message);
    } finally {
      setNativeImportSaving(false);
    }
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
        await apiFetch(`/items/${itemForm.id}`, { method: 'PUT', body: JSON.stringify(payload) });
        toast.success('网址已更新');
      } else {
        await apiFetch('/items', { method: 'POST', body: JSON.stringify({ ...payload, group_id: itemForm.group_id }) });
        toast.success('网址已创建');
      }
      setItemDialogOpen(false);
      await load();
    } catch (error) {
      toast.error(`保存失败：${error.message}`);
    }
  };

  const deleteItem = async item => {
    const confirmed = await dialog.deleteResource({
      title: '删除网址',
      message: `删除「${item.title}」？`,
      confirmText: '删除',
      // 管理页面向管理员快速清理，不要求输入名称二次确认
      // （与其他模块的删除弹窗保持一致，见 GlobalDialogHost 的 requireConfirmationInput）
      requireConfirmationInput: false,
    });
    if (!confirmed) return;
    try {
      await apiFetch(`/items/${item.id}`, { method: 'DELETE' });
      await load();
      toast.success('网址已删除');
    } catch (error) {
      toast.error(`删除失败：${error.message}`);
    }
  };

  const moveItem = async (group, index, direction) => {
    const items = group.items || [];
    const target = index + direction;
    if (target < 0 || target >= items.length) return;
    const next = [...items];
    [next[index], next[target]] = [next[target], next[index]];
    try {
      await apiFetch('/items/sort', {
        method: 'POST',
        body: JSON.stringify({ group_id: group.id, items: next.map((item, idx) => ({ id: item.id, sort: idx })) }),
      });
      await load();
    } catch (error) {
      toast.error(`排序保存失败：${error.message}`);
    }
  };

  const openItem = item => {
    const raw = item.url || '';
    if (!/^https?:\/\//i.test(raw)) {
      toast.error('仅支持 http/https 链接');
      return;
    }
    if (item.open_method === 1) {
      window.location.href = raw;
    } else {
      window.open(raw, '_blank', 'noopener,noreferrer');
    }
  };

  const renderCard = (item, group, options = {}) => {
    const { showGroup = false, itemIndex = 0, count = 1 } = options;
    const isFetchingIcon = fetchingIds.has(item.id);
    return (
      <div
        key={item.id}
        role="button"
        tabIndex={0}
        className="group relative flex cursor-pointer items-center gap-3 rounded-xl border border-kumo-line bg-kumo-control p-3 transition hover:border-kumo-line/70 hover:bg-kumo-recessed"
        onClick={() => openItem(item)}
        onKeyDown={event => {
          if (event.key === 'Enter' || event.key === ' ') {
            event.preventDefault();
            openItem(item);
          }
        }}
        title={item.description || item.url}
      >
        {/* 正在抓取该条目图标：用转圈占位替换图标，让进度可见 */}
        {isFetchingIcon ? (
          <div className="flex h-10 w-10 shrink-0 items-center justify-center rounded-xl bg-kumo-recessed" aria-label="正在抓取图标">
            <Loader size={15} />
          </div>
        ) : (
          renderItemIcon(item, <Globe className="h-5 w-5" />)
        )}
        <div className="min-w-0 flex-1">
          <div className="flex items-center gap-1.5">
            <span className="truncate text-sm font-medium text-kumo-strong">{item.title}</span>
            {showGroup && item.group_title && (
              <span className="shrink-0 rounded bg-kumo-recessed px-1.5 py-0.5 text-[10px] text-kumo-subtle">{item.group_title}</span>
            )}
            <ExternalLink className="h-3 w-3 shrink-0 text-kumo-subtle opacity-0 transition group-hover:opacity-100" />
          </div>
          {item.description && (
            <div className="mt-0.5 truncate text-xs text-kumo-subtle">{item.description}</div>
          )}
        </div>
        <div className="absolute right-1.5 top-1.5 flex items-center gap-0.5 rounded-md bg-kumo-recessed/90 opacity-0 shadow-sm transition group-hover:opacity-100">
          <Button size="sm" variant="ghost" shape="square" icon={<Edit className="h-3.5 w-3.5" />} aria-label={`编辑 ${item.title}`} onClick={event => { event.stopPropagation(); openItemForm(group, item); }} />
          {!showGroup && (
            <>
              <Button size="sm" variant="ghost" shape="square" icon={<ChevronUp className="h-3.5 w-3.5" />} aria-label="上移" disabled={itemIndex === 0} onClick={event => { event.stopPropagation(); moveItem(group, itemIndex, -1); }} />
              <Button size="sm" variant="ghost" shape="square" icon={<ChevronDown className="h-3.5 w-3.5" />} aria-label="下移" disabled={itemIndex === count - 1} onClick={event => { event.stopPropagation(); moveItem(group, itemIndex, 1); }} />
            </>
          )}
          <Button size="sm" variant="ghost" shape="square" icon={<Trash className="h-3.5 w-3.5" />} aria-label={`删除 ${item.title}`} onClick={event => { event.stopPropagation(); deleteItem(item); }} />
        </div>
      </div>
    );
  };

  return (
    <div className="flex w-full min-w-0 flex-col gap-3 cq-sm:gap-4">
      <div className={`${stickyTabsBaseClass} justify-between gap-2 border-b border-kumo-line [&>*]:min-w-0`}>
        <Tabs
          {...MODULE_TABS_PROPS}
          value={activeTab}
          onValueChange={setActiveTab}
          tabs={TABS}
        />
        <div className="flex min-w-0 items-center gap-2">
          <ResponsiveSearchInput
            value={search}
            onChange={event => setSearch(event.target.value)}
            placeholder="搜索网址/标题/描述"
            ariaLabel="搜索网址"
            className="cq-md:w-56"
          />
          {/* 导出 / 导入：连体按钮组，与「主机实例」页的样式保持一致
              （Toolbar 组合 + 图标 + 窄屏隐藏文字）。
              导出=本项目原生格式（完整，含公开页外观）；
              导入走独立弹窗，格式识别、模式选择与预览都在弹窗里完成。 */}
          <Toolbar size="sm" aria-label="导出导入网址导航" className="shrink-0">
            <Toolbar.Button
              onClick={exportBookmarks}
              aria-label="导出网址导航"
              title="导出为 JSON（完整备份，含分组、网址与公开页外观）"
              icon={<Download className="h-3.5 w-3.5" />}
            >
              <span className="hidden cq-sm:inline">导出</span>
            </Toolbar.Button>
            <Toolbar.Button
              onClick={openNativeImportDialog}
              aria-label="导入网址导航"
              title="从 JSON 备份导入"
              icon={<Upload className="h-3.5 w-3.5" />}
            >
              <span className="hidden cq-sm:inline">导入</span>
            </Toolbar.Button>
          </Toolbar>
          <Button
            size="sm"
            variant="secondary"
            loading={fetchingFavicons}
            icon={<Globe className={iconButtonIconClass} />}
            onClick={fetchAllFavicons}
            title="为还没有图标的网址抓取网站 favicon（解析站点 <link rel=icon> 并缓存到本地）"
          >
            {fetchingFavicons ? '抓取中…' : '抓取图标'}
          </Button>
          <Button
            size="sm"
            variant="secondary"
            icon={<ExternalLink className={iconButtonIconClass} />}
            onClick={openPublicAll}
            title={`打开全部公开分组页面（${getPublicAllUrl()}）`}
          >
            全部公开页
          </Button>
          <Button size="sm" variant="primary" icon={<Plus className={iconButtonIconClass} />} onClick={createGroup}>
            新建分组
          </Button>
        </div>
      </div>

      {/* 抓取进度：抓取可能持续几十秒，给出明确的进度与当前并发数，
          否则用户只看到一个转圈的按钮，不知道还要等多久。 */}
      {fetchProgress && (
        <div className="flex items-center gap-3 rounded-lg border border-kumo-line bg-kumo-recessed/40 px-3 py-2">
          <Loader size={16} />
          <div className="min-w-0 flex-1">
            <div className="flex items-center justify-between gap-2 text-xs">
              <span className="font-medium text-kumo-strong">
                正在抓取网站图标… {fetchProgress.done} / {fetchProgress.total}
              </span>
              {fetchProgress.active > 0 && (
                <span className="shrink-0 text-kumo-subtle">并发 {fetchProgress.active}</span>
              )}
            </div>
            <div className="mt-1.5 h-1.5 w-full overflow-hidden rounded-full bg-kumo-line/40">
              <div
                className="h-full rounded-full bg-brand transition-[width]"
                style={{ width: `${fetchProgress.total ? Math.round((fetchProgress.done / fetchProgress.total) * 100) : 0}%` }}
                role="progressbar"
                aria-valuenow={fetchProgress.done}
                aria-valuemin={0}
                aria-valuemax={fetchProgress.total}
              />
            </div>
          </div>
        </div>
      )}

      {activeTab === 'navigate' && (
        loading ? (
          <div className="flex min-h-80 items-center justify-center">
            <Loader size={28} />
          </div>
        ) : groups.length === 0 ? (
          <Empty
            className="min-h-80"
            icon={<Bookmark className="h-10 w-10 text-kumo-inactive" />}
            title="还没有网址分组"
            description="创建分组后即可添加常用网址，一键快速访问。"
            contents={
              <Button size="sm" variant="primary" icon={<Plus className={iconButtonIconClass} />} onClick={createGroup}>
                新建分组
              </Button>
            }
          />
        ) : (
          <>
            {filteredGroups.length === 0 && (
              <Empty
                className="min-h-60"
                icon={<Search className="h-8 w-8 text-kumo-inactive" />}
                title="没有匹配的网址"
                description="换个关键词试试。"
              />
            )}
            <div className="flex flex-col gap-4">
              {filteredGroups.map((group, groupIndex) => (
                <SectionCard
                  key={group.id}
                  title={
                    <span className="inline-flex min-w-0 items-center gap-2">
                      <Folder className="h-4 w-4 shrink-0 text-brand" />
                      <span className="truncate">{group.title}</span>
                      <span className="text-xs font-normal text-kumo-subtle">{(group.items || []).length}</span>
                      {group.public && group.slug && (
                        <span className="shrink-0 rounded bg-kumo-success/10 px-1.5 py-0.5 text-[10px] font-normal text-kumo-success">
                          公开
                        </span>
                      )}
                    </span>
                  }
                  action={
                    <div className="flex items-center gap-1">
                      <Button size="sm" variant="secondary" shape="square" icon={<Plus className={iconButtonIconClass} />} aria-label={`在「${group.title}」中新建网址`} title="新建网址" onClick={() => openItemForm(group)} />
                      <Button size="sm" variant="ghost" shape="square" icon={<ChevronUp className={iconButtonIconClass} />} aria-label="上移分组" disabled={groupIndex === 0} onClick={() => moveGroup(groupIndex, -1)} />
                      <Button size="sm" variant="ghost" shape="square" icon={<ChevronDown className={iconButtonIconClass} />} aria-label="下移分组" disabled={groupIndex === filteredGroups.length - 1} onClick={() => moveGroup(groupIndex, 1)} />
                      <Button size="sm" variant="ghost" shape="square" icon={<Globe className={iconButtonIconClass} />} aria-label="公开设置" title="公开设置" onClick={() => openGroupSettings(group)} />
                      {group.public && group.slug && (
                        <Button size="sm" variant="ghost" shape="square" icon={<ExternalLink className={iconButtonIconClass} />} aria-label="打开公开页" title="打开公开页" onClick={() => openPublicGroup(group)} />
                      )}
                      <Button size="sm" variant="ghost" shape="square" icon={<Edit className={iconButtonIconClass} />} aria-label="重命名分组" onClick={() => renameGroup(group)} />
                      <Button size="sm" variant="ghost" shape="square" icon={<Trash className={iconButtonIconClass} />} aria-label="删除分组" onClick={() => deleteGroup(group)} />
                    </div>
                  }
                >
                  {(group.items || []).length === 0 ? (
                    <div className="grid gap-2.5 p-3 cq-sm:grid-cols-2 cq-lg:grid-cols-3 cq-xl:grid-cols-4">
                      <div
                        role="button"
                        tabIndex={0}
                        className="flex cursor-pointer flex-col items-center justify-center gap-1.5 rounded-xl border border-dashed border-kumo-line bg-kumo-recessed/30 p-6 text-xs text-kumo-subtle transition hover:border-brand/40 hover:bg-kumo-recessed hover:text-kumo-strong"
                        onClick={() => openItemForm(group)}
                        onKeyDown={event => {
                          if (event.key === 'Enter' || event.key === ' ') {
                            event.preventDefault();
                            openItemForm(group);
                          }
                        }}
                      >
                        <Plus className="h-5 w-5" />
                        <span>添加网址</span>
                      </div>
                    </div>
                  ) : (
                    <div className="grid gap-2.5 p-3 cq-sm:grid-cols-2 cq-lg:grid-cols-3 cq-xl:grid-cols-4">
                      {(group.items || []).map((item, itemIndex) => renderCard(item, group, { itemIndex, count: (group.items || []).length }))}
                    </div>
                  )}
                </SectionCard>
              ))}
            </div>
          </>
        )
      )}

      {activeTab === 'all' && (
        allItems.length === 0 ? (
          <Empty
            className="min-h-60"
            icon={<Menu className="h-8 w-8 text-kumo-inactive" />}
            title={keyword ? '没有匹配的网址' : '还没有网址'}
            description={keyword ? '换个关键词试试。' : '先在「导航」页签创建分组并添加网址。'}
          />
        ) : (
          <div className="grid gap-2.5 cq-sm:grid-cols-2 cq-lg:grid-cols-3 cq-xl:grid-cols-4">
            {allItems.map(item => {
              const group = groups.find(g => g.id === item.group_id) || { id: item.group_id, items: [] };
              return renderCard(item, group, { showGroup: true });
            })}
          </div>
        )
      )}

      {activeTab === 'public' && (
        <div className="grid items-start gap-4 cq-xl:grid-cols-[minmax(24rem,0.9fr)_minmax(0,1.1fr)]">
          {/* 左列：公开状态与配置整合为一张卡（原「公开配置」表单已并入这里） */}
          <LayerCard className="overflow-hidden p-0">
            <LayerCard.Secondary className={sectionCardHeaderClass}>
              <div>
                <h3 className="flex items-center gap-2 text-sm font-semibold text-kumo-strong"><Globe className="h-4 w-4" />分组与公开状态</h3>
              </div>
            </LayerCard.Secondary>
            <LayerCard.Primary className="p-4">
              {/* 选中某个分组后，下方展开它的公开配置（原来独立的「公开配置」卡片） */}
              <div className="mb-3">
                <div className="mb-1 text-xs font-medium text-kumo-strong">选择分组</div>
                <Select
                  size="sm"
                  className="w-full"
                  value={groupForm.id ? String(groupForm.id) : ''}
                  onValueChange={value => selectGroupForPublic(groups.find(g => g.id === Number(value)))}
                  aria-label="选择分组"
                  placeholder="选择要公开的分组"
                  items={groups.map(group => ({ value: String(group.id), label: group.title }))}
                />
              </div>
              {groupForm.id && (
                <div className="mb-3 space-y-3 rounded-lg border border-kumo-line bg-kumo-recessed/25 p-3">
                  <div className="grid gap-3 cq-sm:grid-cols-2">
                    <Input size="sm" label="分组名称" value={groupForm.title} onChange={event => setGroupForm(prev => ({ ...prev, title: event.target.value }))} />
                    <div>
                      <div className="mb-1 text-xs font-medium text-kumo-strong">公开链接标识（slug）</div>
                      <div className="flex items-start gap-2">
                        <Input size="sm" className="flex-1" value={groupForm.slug} onChange={event => setGroupForm(prev => ({ ...prev, slug: normalizeSlug(event.target.value) }))} placeholder="例如：nav" />
                        <Button size="sm" variant="secondary" disabled={!groupForm.title} onClick={() => setGroupForm(prev => ({ ...prev, slug: normalizeSlug(prev.title) }))}>自动生成</Button>
                      </div>
                    </div>
                    <Input size="sm" label="自定义域名（可选）" value={groupForm.domain} onChange={event => setGroupForm(prev => ({ ...prev, domain: normalizeDomain(event.target.value) }))} placeholder="nav.example.com" />
                    <Input size="sm" label="缓存秒数" type="number" min="30" value={groupForm.cache_seconds} onChange={event => setGroupForm(prev => ({ ...prev, cache_seconds: Number(event.target.value) || 300 }))} />
                    <div className="cq-sm:col-span-2">
                      <Textarea size="sm" label="描述" rows={3} value={groupForm.description} onChange={event => setGroupForm(prev => ({ ...prev, description: event.target.value }))} placeholder="公开页展示的说明文字" />
                    </div>
                  </div>
                  <div className="flex items-center justify-between gap-3 rounded-lg border border-kumo-line bg-kumo-recessed/30 p-3">
                    <div className="min-w-0">
                      <div className="text-sm font-semibold text-kumo-strong">公开访问</div>
                      <div className="mt-1 text-xs text-kumo-subtle">开启后可通过公开链接免登录访问此分组的网址。</div>
                    </div>
                    <Switch checked={!!groupForm.public} onCheckedChange={checked => setGroupForm(prev => ({ ...prev, public: checked }))} />
                  </div>
                  {(groupForm.public && groupForm.slug) || groupForm.domain ? (
                    <div className="rounded-lg border border-kumo-line bg-kumo-recessed/35 p-3 text-xs text-kumo-subtle">
                      <div className="font-semibold text-kumo-strong">预览地址</div>
                      <div className="mt-2 space-y-1.5">
                        {groupForm.public && groupForm.slug && (
                          <ClipboardText size="sm" text={getPublicGroupUrl({ public: true, slug: groupForm.slug })} className="w-full" tooltip={{ text: '复制地址', copiedText: '地址已复制', side: 'top' }} labels={{ copyAction: '复制公开地址' }} />
                        )}
                        {groupForm.domain && (
                          <ClipboardText size="sm" text={`https://${groupForm.domain}`} className="w-full" tooltip={{ text: '复制地址', copiedText: '地址已复制', side: 'top' }} labels={{ copyAction: '复制域名地址' }} />
                        )}
                      </div>
                    </div>
                  ) : null}
                  <div className="flex flex-wrap justify-end gap-2">
                    <Button size="sm" variant="secondary" onClick={() => setGroupForm(emptyGroupForm())}>重置</Button>
                    <Button size="sm" variant="primary" disabled={!groupForm.title.trim()} onClick={saveGroupSettings} icon={<Save className="h-3.5 w-3.5" />}>保存配置</Button>
                  </div>
                </div>
              )}

              {/* 聚合页入口：一个地址展示全部已公开分组 */}
              <div className="mb-3 rounded-lg border border-kumo-line bg-kumo-recessed/40 p-3">
                <div className="flex flex-col gap-2 cq-sm:flex-row cq-sm:items-center cq-sm:justify-between">
                  <div className="min-w-0">
                    <div className="text-xs font-semibold text-kumo-strong">全部公开分组页面</div>
                    <div className="mt-0.5 truncate font-mono text-xs text-kumo-subtle">/bookmarks/all</div>
                  </div>
                  <div className="flex shrink-0 gap-2">
                    <Button size="sm" variant="secondary" icon={<ExternalLink className="h-3.5 w-3.5" />} onClick={openPublicAll}>打开</Button>
                    <Button size="sm" variant="secondary" icon={<Copy className="h-3.5 w-3.5" />} onClick={async () => {
                      try {
                        await navigator.clipboard.writeText(getPublicAllUrl());
                        toast.success('聚合页地址已复制');
                      } catch {
                        toast.error('复制失败');
                      }
                    }}>复制</Button>
                  </div>
                </div>
              </div>
              {groups.length === 0 ? (
                <div className="flex min-h-56 flex-col items-center justify-center rounded-lg border border-dashed border-kumo-line text-center text-sm text-kumo-subtle">
                  <Folder className="mb-3 h-8 w-8 opacity-40" />
                  暂无分组，先在「导航」页签创建。
                </div>
              ) : (
                <>
                  {/* 批量操作 + 状态汇总：让「哪些会出现在公开页」一眼可见 */}
                  <div className="mb-3 flex flex-wrap items-center justify-between gap-2 rounded-lg border border-kumo-line bg-kumo-recessed/30 p-3">
                    <div className="min-w-0 text-xs text-kumo-subtle">
                      共 <span className="font-semibold text-kumo-strong">{groups.length}</span> 个分组，
                      其中 <span className="font-semibold text-kumo-success">{publicGroupCount}</span> 个公开、
                      <span className="font-semibold text-kumo-strong">{groups.length - publicGroupCount}</span> 个私有。
                      公开页只显示公开的分组。
                    </div>
                    <div className="flex shrink-0 gap-2">
                      <Button size="sm" variant="secondary" disabled={publicGroupCount === groups.length} onClick={() => setAllGroupsPublic(true)}>
                        全部公开
                      </Button>
                      <Button size="sm" variant="secondary" disabled={publicGroupCount === 0} onClick={() => setAllGroupsPublic(false)}>
                        全部取消公开
                      </Button>
                    </div>
                  </div>
                  {/* 瀑布流：分组卡片内容高度不一（描述/域名行数不同），
                      按 docs/standards/云厂商模块开发指南.md 的范式用 CSS columns 排布。
                      break-inside-avoid 防止卡片被拆到两列；间距用 [&>*]:mb-3（columns 不支持 gap）。 */}
                  <div className="columns-1 gap-3 cq-md:columns-2 [&>*]:mb-3 [&>*]:break-inside-avoid">
                    {groups.map(group => {
                      const publicUrl = getPublicGroupUrl(group);
                      return (
                        <div
                          key={group.id}
                          className={`rounded-lg border p-3 transition ${group.public ? 'border-kumo-success/40 bg-kumo-success/5' : 'border-kumo-line bg-kumo-base'}`}
                        >
                          <div className="flex flex-col gap-3 cq-sm:flex-row cq-sm:items-start cq-sm:justify-between">
                            <div className="min-w-0">
                              <div className="flex flex-wrap items-center gap-2">
                                <span className={`flex h-7 w-7 shrink-0 items-center justify-center rounded-md ${group.public ? 'bg-kumo-success/15 text-kumo-success' : 'bg-brand/10 text-brand'}`}>
                                  <Folder className="h-4 w-4" />
                                </span>
                                <span className="truncate text-sm font-semibold text-kumo-strong">{group.title}</span>
                                <span className={`rounded px-2 py-0.5 text-[10px] font-semibold ${group.public ? 'bg-kumo-success/15 text-kumo-success' : 'bg-kumo-line/30 text-kumo-subtle'}`}>
                                  {group.public ? '公开' : '私有'}
                                </span>
                                <span className="text-xs text-kumo-subtle">{(group.items || []).length} 个网址</span>
                              </div>
                              {group.public ? (
                                <div className="mt-1 truncate font-mono text-xs text-kumo-subtle">/bookmarks/{group.slug}</div>
                              ) : (
                                <div className="mt-1 text-xs text-kumo-subtle">未公开，不会出现在公开页</div>
                              )}
                              {group.domain && <div className="mt-0.5 truncate font-mono text-xs text-kumo-subtle">{group.domain}</div>}
                              {group.description && <div className="mt-2 line-clamp-2 text-xs leading-relaxed text-kumo-subtle">{group.description}</div>}
                            </div>
                            <div className="flex shrink-0 flex-wrap items-center gap-2">
                              {/* 状态开关直接放在列表里，不必进设置 */}
                              <Switch
                                size="sm"
                                checked={!!group.public}
                                onCheckedChange={() => toggleGroupPublic(group)}
                                aria-label={group.public ? `取消公开 ${group.title}` : `公开 ${group.title}`}
                                title={group.public ? '点击取消公开' : '点击设为公开'}
                              />
                              <Button size="sm" variant="secondary" shape="square" icon={<Edit className="h-3.5 w-3.5" />} onClick={() => selectGroupForPublic(group)} aria-label="配置公开设置" title="配置公开设置" />
                              {publicUrl && (
                                <>
                                  <Button size="sm" variant="secondary" shape="square" icon={<ExternalLink className="h-3.5 w-3.5" />} onClick={() => openPublicGroup(group)} aria-label="打开公开页" title="打开公开页" />
                                  <Button size="sm" variant="secondary" shape="square" icon={<Copy className="h-3.5 w-3.5" />} onClick={async () => {
                                    try {
                                      await navigator.clipboard.writeText(publicUrl);
                                      toast.success('公开地址已复制');
                                    } catch {
                                      toast.error('复制失败');
                                    }
                                  }} aria-label="复制公开地址" title="复制公开地址" />
                                </>
                              )}
                              {/* 此前公开页签里没有删除入口 */}
                              <Button
                                size="sm"
                                variant="secondary"
                                shape="square"
                                icon={<Trash className="h-3.5 w-3.5" />}
                                onClick={() => deleteGroup(group)}
                                aria-label={`删除分组 ${group.title}`}
                                title="删除分组"
                              />
                            </div>
                          </div>
                        </div>
                      );
                    })}
                  </div>
                </>
              )}
            </LayerCard.Primary>
          </LayerCard>

          {/* 公开页外观：背景 + 内容区宽度等全局设置，作用于整个公开页（含聚合页），
              不是一个分组一份。说明文案与内层嵌套已去掉——面板自身标题已足够表意。 */}
          <LayerCard className="overflow-hidden p-0">
            <LayerCard.Secondary className={sectionCardHeaderClass}>
              <div>
                <h3 className="flex items-center gap-2 text-sm font-semibold text-kumo-strong">
                  <Image className="h-4 w-4" />公开页外观
                </h3>
              </div>
            </LayerCard.Secondary>
            <LayerCard.Primary className="space-y-5 p-4">
              <BackgroundSettings config={publicBgConfig} onChange={saveGlobalBackground} />
              {/* 搜索引擎：与背景同属公开页全局设置，因此放在同一张卡里，
                  保存也走同一个 public-settings 接口 */}
              <div className="border-t border-kumo-line/60 pt-4">
                <SearchEnginesSettings config={publicBgConfig} onChange={saveGlobalBackground} />
              </div>
            </LayerCard.Primary>
          </LayerCard>
        </div>
      )}

      <ItemFormDialog
        open={itemDialogOpen}
        form={itemForm}
        request={apiFetch}
        onOpenChange={open => {
          setItemDialogOpen(open);
          if (!open) setItemForm(emptyItemForm(0));
        }}
        onFormChange={setItemForm}
        onSave={saveItem}
      />

      <GroupSettingsDialog
        open={groupDialogOpen}
        form={groupForm}
        onOpenChange={open => {
          setGroupDialogOpen(open);
          if (!open) setGroupForm(emptyGroupForm());
        }}
        onFormChange={setGroupForm}
        onSave={saveGroupSettings}
      />

      <SunPanelImportDialog
        open={importOpen}
        onOpenChange={setImportOpen}
        fileInputRef={importFileInputRef}
        fileName={importFileName}
        text={importText}
        setText={setImportText}
        setFileName={setImportFileName}
        groupMode={importGroupMode}
        setGroupMode={setImportGroupMode}
        targetGroupId={importTargetGroupId}
        setTargetGroupId={setImportTargetGroupId}
        groups={groups}
        options={importOptions}
        setOptions={setImportOptions}
        importing={importing}
        onSubmit={submitImport}
      />

      {/* 导入网址导航备份（原生格式）。
          交互与「主机实例」页的导入弹窗一致：选文件 → 识别 → 选模式 → 预览 → 确认。 */}
      <LayerDialog.Root open={nativeImportOpen} onOpenChange={setNativeImportOpen}>
        <LayerDialog.Content size="sm">
          <LayerDialog.Title>导入网址导航备份</LayerDialog.Title>
          <LayerDialog.Body>
            <div className="flex flex-col gap-4 text-xs">
              <div className="flex flex-col gap-1.5">
                <label className="font-medium text-kumo-subtle">选择备份 JSON 文件</label>
                <Input
                  size="sm"
                  aria-label="选择网址导航备份 JSON 文件"
                  type="file"
                  accept="application/json,.json"
                  className="px-3 py-2"
                  onChange={(event) => {
                    const file = event.target.files?.[0];
                    event.target.value = '';
                    if (file) processNativeImportFile(file);
                  }}
                />
                {nativeImportFileName && (
                  <div className="truncate text-[11px] text-kumo-subtle">已选择：{nativeImportFileName}</div>
                )}
              </div>

              {/* 模式选择：只有解析成功后才需要选 */}
              {nativeImportPayload && (
                <div className="flex flex-col gap-1.5">
                  <div className="font-medium text-kumo-subtle">导入方式</div>
                  <Select
                    size="sm"
                    className="w-full"
                    value={nativeImportMode}
                    onValueChange={(value) => {
                      setNativeImportMode(value);
                      setNativeImportPreview(null);
                      runNativeImportPreview(value);
                    }}
                    items={[
                      { value: 'merge', label: '合并（按分组名并入，重复网址跳过）' },
                      { value: 'replace', label: '替换（先清空现有数据，不可恢复）' },
                    ]}
                    aria-label="导入方式"
                  />
                  <div className="text-[11px] leading-relaxed text-kumo-subtle">
                    {nativeImportMode === 'replace'
                      ? '替换会先清空当前所有分组与网址，再导入文件内容。'
                      : '合并会按分组名称匹配：同名分组只追加其中尚不存在的网址。'}
                  </div>
                </div>
              )}

              {/* 预览：dry-run 的结果 */}
              {nativeImportPreview && (
                <div className="rounded border border-kumo-success/20 bg-kumo-success/10 p-2.5 text-xs font-semibold text-kumo-success">
                  ✓ 将创建 {nativeImportPreview.groups_created || 0} 个分组、
                  并入 {nativeImportPreview.groups_merged || 0} 个同名分组，
                  导入 {nativeImportPreview.items_imported || 0} 个网址
                  {nativeImportPreview.items_skipped ? `，跳过 ${nativeImportPreview.items_skipped} 个条目` : ''}。
                  <div className="mt-1 font-normal">导入在单事务内完成，失败会整体回滚。</div>
                </div>
              )}

              {nativeImportPreview?.warnings?.length > 0 && (
                <div className="rounded border border-kumo-warning/20 bg-kumo-warning/10 p-2.5 text-xs text-kumo-warning">
                  {nativeImportPreview.warnings.slice(0, 3).map((w, i) => (
                    <div key={i}>{w}</div>
                  ))}
                </div>
              )}

              {nativeImportError && (
                <div className="rounded border border-kumo-danger/20 bg-kumo-danger/10 p-2.5 text-xs font-semibold text-kumo-danger">
                  {nativeImportError}
                </div>
              )}
            </div>
          </LayerDialog.Body>
          <LayerDialog.Actions dismissLabel="取消">
            <LayerDialog.Actions.Primary
              type="button"
              onClick={confirmNativeImport}
              disabled={nativeImportSaving || !nativeImportPayload || !nativeImportPreview}
            >
              {nativeImportSaving ? '处理中…' : '确认导入'}
            </LayerDialog.Actions.Primary>
          </LayerDialog.Actions>
        </LayerDialog.Content>
      </LayerDialog.Root>
    </div>
  );
}
