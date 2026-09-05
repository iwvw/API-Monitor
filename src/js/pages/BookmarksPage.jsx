import React, { useCallback, useEffect, useMemo, useState } from 'react';
import { ClipboardText, Empty, LayerCard, Tabs } from '@cloudflare/kumo';
import { Button } from '@cloudflare/kumo/components/button';
import { Dialog } from '@cloudflare/kumo/components/dialog';
import { Input, Textarea } from '@cloudflare/kumo/components/input';
import { Select } from '@cloudflare/kumo/components/select';
import { Switch } from '@cloudflare/kumo/components/switch';
import useStore from '../store.js';
import { MODULE_TABS_PROPS } from '../modules/kumoTabs.js';
import { dialog } from '../modules/dialog.js';
import { toast } from '../modules/toast.js';
import { ResponsiveSearchInput, SectionCard, cx, iconButtonIconClass, sectionCardHeaderClass, stickyTabsBaseClass } from '../components/ui/AppPrimitives.jsx';
import { Bookmark, ChevronDown, ChevronUp, Copy, Edit, ExternalLink, Folder, Globe, Menu, Plus, Save, Search, Trash, X } from '../components/Icons.jsx';

const API = '/api/bookmarks';

const ICON_TYPE_OPTIONS = [
  { value: '2', label: '网站图标' },
  { value: '1', label: '文字' },
  { value: '3', label: 'Emoji 字符' },
];

const OPEN_METHOD_OPTIONS = [
  { value: '2', label: '新窗口打开' },
  { value: '1', label: '当前页打开' },
];

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

function emptyItemForm(groupId) {
  return {
    id: 0,
    group_id: groupId,
    title: '',
    url: '',
    description: '',
    icon_type: 2,
    icon_src: '',
    icon_text: '',
    icon_bg_color: '',
    open_method: 2,
  };
}

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

function renderItemIcon(item, fallback) {
  const { icon_type: type, icon_src: src, icon_text: text, icon_bg_color: bg } = item || {};
  const wrapperClass = 'flex h-11 w-11 shrink-0 items-center justify-center overflow-hidden rounded-xl';
  const bgClass = bg ? '' : 'bg-kumo-recessed';
  const containerStyle = bg ? { backgroundColor: bg } : undefined;
  if (type === 2 && src) {
    return (
      <div style={containerStyle} className={cx(wrapperClass, bgClass)}>
        <img src={src} alt="" loading="lazy" className="h-7 w-7 object-contain" onError={event => { event.currentTarget.style.display = 'none'; }} />
      </div>
    );
  }
  if (type === 1 && text) {
    return (
      <div style={containerStyle} className={cx(wrapperClass, bgClass, 'text-sm font-semibold text-white')}>
        {text.slice(0, 1)}
      </div>
    );
  }
  if (type === 3 && text) {
    return (
      <div style={containerStyle} className={cx(wrapperClass, bgClass, 'text-lg')}>
        {text}
      </div>
    );
  }
  return (
    <div style={containerStyle} className={cx(wrapperClass, bgClass, 'text-kumo-subtle')}>
      {fallback}
    </div>
  );
}

function ItemFormDialog({ open, form, onOpenChange, onFormChange, onSave }) {
  const [fetching, setFetching] = useState(false);

  useEffect(() => {
    setFetching(false);
  }, [open]);

  const setField = (key, value) => onFormChange({ ...form, [key]: value });

  const fetchIcon = async () => {
    if (!form.url.trim()) {
      toast.error('请先填写网址');
      return;
    }
    setFetching(true);
    try {
      const data = await apiFetch('/favicon/fetch', {
        method: 'POST',
        body: JSON.stringify({ url: form.url.trim() }),
      });
      if (data.success === false) {
        toast.error(`图标获取失败：${data.error || '未知错误'}`);
      } else {
        setField('icon_type', 2);
        setField('icon_src', data.data?.icon_src || '');
        toast.success('已获取网站图标');
      }
    } catch (error) {
      toast.error(`图标获取失败：${error.message}`);
    } finally {
      setFetching(false);
    }
  };

  const canSave = form.title.trim() && form.url.trim();

  return (
    <Dialog.Root open={open} onOpenChange={onOpenChange}>
      <Dialog className="w-[min(32rem,calc(100vw-2rem))] p-5">
        <Dialog.Title className="text-base font-semibold text-kumo-strong">
          {form.id ? '编辑网址' : '新建网址'}
        </Dialog.Title>
        <div className="mt-4 space-y-3">
          <Input
            size="sm"
            label="标题"
            placeholder="例如：GitHub"
            value={form.title}
            onChange={event => setField('title', event.target.value)}
          />
          <div>
            <div className="mb-1 text-xs font-medium text-kumo-strong">网址</div>
            <div className="flex items-start gap-2">
              <Input
                size="sm"
                className="flex-1"
                placeholder="https://example.com"
                value={form.url}
                onChange={event => setField('url', event.target.value)}
              />
              <Button size="sm" variant="secondary" onClick={fetchIcon} disabled={fetching || !form.url.trim()}>
                {fetching ? '获取中...' : '获取图标'}
              </Button>
            </div>
          </div>
          <Textarea
            size="sm"
            label="描述"
            rows={2}
            value={form.description}
            onChange={event => setField('description', event.target.value)}
          />
          <div className="grid gap-3 cq-sm:grid-cols-2">
            <div>
              <div className="mb-1 text-xs font-medium text-kumo-strong">图标类型</div>
              <Select
                size="sm"
                className="w-full"
                value={String(form.icon_type)}
                onValueChange={value => setField('icon_type', Number(value))}
                aria-label="图标类型"
              >
                {ICON_TYPE_OPTIONS.map(option => (
                  <Select.Option key={option.value} value={option.value}>{option.label}</Select.Option>
                ))}
              </Select>
            </div>
            <div>
              <div className="mb-1 text-xs font-medium text-kumo-strong">打开方式</div>
              <Select
                size="sm"
                className="w-full"
                value={String(form.open_method)}
                onValueChange={value => setField('open_method', Number(value))}
                aria-label="打开方式"
              >
                {OPEN_METHOD_OPTIONS.map(option => (
                  <Select.Option key={option.value} value={option.value}>{option.label}</Select.Option>
                ))}
              </Select>
            </div>
          </div>
          {form.icon_type === 2 && (
            <Input
              size="sm"
              label="图标图片地址"
              placeholder="留空则显示占位图标"
              value={form.icon_src}
              onChange={event => setField('icon_src', event.target.value)}
            />
          )}
          {(form.icon_type === 1 || form.icon_type === 3) && (
            <Input
              size="sm"
              label={form.icon_type === 1 ? '文字内容' : 'Emoji 或字符'}
              placeholder={form.icon_type === 1 ? '例如：工' : '例如：🔧'}
              value={form.icon_text}
              onChange={event => setField('icon_text', event.target.value)}
            />
          )}
          <div className="flex items-center gap-3">
            <div className="text-xs font-medium text-kumo-strong">图标背景色</div>
            <label
              className="relative flex h-7 w-12 cursor-pointer items-center justify-center rounded border border-kumo-line"
              title="选择颜色"
            >
              <span
                className={cx('h-4 w-4 rounded-full', form.icon_bg_color ? '' : 'bg-kumo-recessed')}
                style={form.icon_bg_color ? { backgroundColor: form.icon_bg_color } : undefined}
              />
              <input
                type="color"
                className="sr-only"
                value={form.icon_bg_color || '#808080'}
                onChange={event => setField('icon_bg_color', event.target.value)}
                aria-label="选择图标背景色"
              />
            </label>
            {form.icon_bg_color && (
              <Button size="sm" variant="ghost" onClick={() => setField('icon_bg_color', '')}>重置</Button>
            )}
          </div>
        </div>
        <div className="mt-6 flex justify-end gap-2">
          <Button size="sm" variant="secondary" onClick={() => onOpenChange(false)}>取消</Button>
          <Button size="sm" variant="primary" disabled={!canSave} onClick={() => onSave()}>保存</Button>
        </div>
      </Dialog>
    </Dialog.Root>
  );
}

function GroupSettingsDialog({ open, form, onOpenChange, onFormChange, onSave }) {
  const setField = (key, value) => onFormChange({ ...form, [key]: value });
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
    <Dialog.Root open={open} onOpenChange={onOpenChange}>
      <Dialog className="w-[min(32rem,calc(100vw-2rem))] p-5">
        <Dialog.Title className="text-base font-semibold text-kumo-strong">分组公开设置</Dialog.Title>
        <div className="mt-4 space-y-3">
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
        <div className="mt-6 flex justify-end gap-2">
          <Button size="sm" variant="secondary" onClick={() => onOpenChange(false)}>取消</Button>
          <Button size="sm" variant="primary" disabled={!canSave} onClick={() => onSave()}>保存</Button>
        </div>
      </Dialog>
    </Dialog.Root>
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

  const load = useCallback(async () => {
    try {
      const data = await apiFetch('/groups');
      setGroups(data.groups || []);
    } catch (error) {
      toast.error(`加载失败：${error.message}`);
    }
  }, []);

  useEffect(() => {
    load();
  }, [load]);

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

  const moveGroup = async (index, direction) => {
    const target = index + direction;
    if (target < 0 || target >= groups.length) return;
    const next = [...groups];
    [next[index], next[target]] = [next[target], next[index]];
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
        {renderItemIcon(item, <Globe className="h-5 w-5" />)}
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
          <Button size="sm" variant="primary" icon={<Plus className={iconButtonIconClass} />} onClick={createGroup}>
            新建分组
          </Button>
        </div>
      </div>

      {activeTab === 'navigate' && (
        groups.length === 0 ? (
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
          <LayerCard className="overflow-hidden p-0">
            <LayerCard.Secondary className={sectionCardHeaderClass}>
              <h3 className="flex items-center gap-2 text-sm font-semibold text-kumo-strong">
                <Globe className="h-4 w-4" />
                公开配置
              </h3>
            </LayerCard.Secondary>
            <LayerCard.Primary className="space-y-4 p-4">
              <div>
                <div className="mb-1 text-xs font-medium text-kumo-strong">选择分组</div>
                <Select
                  size="sm"
                  className="w-full"
                  value={groupForm.id ? String(groupForm.id) : ''}
                  onValueChange={value => selectGroupForPublic(groups.find(g => g.id === Number(value)))}
                  aria-label="选择分组"
                  placeholder="选择要公开的分组"
                >
                  {groups.map(group => (
                    <Select.Option key={group.id} value={String(group.id)}>{group.title}</Select.Option>
                  ))}
                </Select>
              </div>
              {!groupForm.id ? (
                <div className="rounded-lg border border-dashed border-kumo-line p-6 text-center text-xs text-kumo-subtle">
                  选择分组后配置其公开显示页面。
                </div>
              ) : (
                <>
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
                  <div className="rounded-lg border border-kumo-line bg-kumo-recessed/35 p-3 text-xs text-kumo-subtle">
                    <div className="font-semibold text-kumo-strong">预览地址</div>
                    <div className="mt-2 space-y-1.5">
                      {groupForm.public && groupForm.slug && (
                        <ClipboardText size="sm" text={getPublicGroupUrl({ public: true, slug: groupForm.slug })} className="w-full" tooltip={{ text: '复制地址', copiedText: '地址已复制', side: 'top' }} labels={{ copyAction: '复制公开地址' }} />
                      )}
                      {groupForm.domain && (
                        <ClipboardText size="sm" text={`https://${groupForm.domain}`} className="w-full" tooltip={{ text: '复制地址', copiedText: '地址已复制', side: 'top' }} labels={{ copyAction: '复制域名地址' }} />
                      )}
                      {!groupForm.public && <div className="text-xs text-kumo-subtle">开启公开访问后生成链接。</div>}
                    </div>
                  </div>
                  <div className="flex flex-wrap justify-end gap-2">
                    <Button size="sm" variant="secondary" onClick={() => setGroupForm(emptyGroupForm())}>重置</Button>
                    <Button size="sm" variant="primary" disabled={!groupForm.title.trim()} onClick={saveGroupSettings} icon={<Save className="h-3.5 w-3.5" />}>保存配置</Button>
                  </div>
                </>
              )}
            </LayerCard.Primary>
          </LayerCard>

          <LayerCard className="overflow-hidden p-0">
            <LayerCard.Secondary className={sectionCardHeaderClass}>
              <div>
                <h3 className="flex items-center gap-2 text-sm font-semibold text-kumo-strong"><Globe className="h-4 w-4" />分组与公开状态</h3>
              </div>
            </LayerCard.Secondary>
            <LayerCard.Primary className="p-4">
              {groups.length === 0 ? (
                <div className="flex min-h-56 flex-col items-center justify-center rounded-lg border border-dashed border-kumo-line text-center text-sm text-kumo-subtle">
                  <Folder className="mb-3 h-8 w-8 opacity-40" />
                  暂无分组，先在「导航」页签创建。
                </div>
              ) : (
                <div className="grid gap-3">
                  {groups.map(group => {
                    const publicUrl = getPublicGroupUrl(group);
                    return (
                      <div key={group.id} className="rounded-lg border border-kumo-line bg-kumo-base p-3">
                        <div className="flex flex-col gap-3 cq-sm:flex-row cq-sm:items-start cq-sm:justify-between">
                          <div className="min-w-0">
                            <div className="flex flex-wrap items-center gap-2">
                              <span className="flex h-7 w-7 shrink-0 items-center justify-center rounded-md bg-brand/10 text-brand">
                                <Folder className="h-4 w-4" />
                              </span>
                              <span className="truncate text-sm font-semibold text-kumo-strong">{group.title}</span>
                              <span className={`rounded px-2 py-0.5 text-[10px] font-semibold ${group.public ? 'bg-kumo-success/10 text-kumo-success' : 'bg-kumo-line/30 text-kumo-subtle'}`}>{group.public ? '公开' : '私有'}</span>
                              <span className="text-xs text-kumo-subtle">{(group.items || []).length} 个网址</span>
                            </div>
                            {group.slug && <div className="mt-1 truncate font-mono text-xs text-kumo-subtle">/bookmarks/{group.slug}</div>}
                            {group.domain && <div className="mt-0.5 truncate font-mono text-xs text-kumo-subtle">{group.domain}</div>}
                            {group.description && <div className="mt-2 line-clamp-2 text-xs leading-relaxed text-kumo-subtle">{group.description}</div>}
                          </div>
                          <div className="flex shrink-0 flex-wrap gap-2">
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
                          </div>
                        </div>
                      </div>
                    );
                  })}
                </div>
              )}
            </LayerCard.Primary>
          </LayerCard>
        </div>
      )}

      <ItemFormDialog
        open={itemDialogOpen}
        form={itemForm}
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
    </div>
  );
}
