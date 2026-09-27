import React, { useEffect, useState } from 'react';
import { Button } from '@cloudflare/kumo/components/button';
import { LayerDialog } from '@cloudflare/kumo/components/layer-dialog';
import { Input, Textarea } from '@cloudflare/kumo/components/input';
import { Select } from '@cloudflare/kumo/components/select';
import { cx } from '../../components/ui/AppPrimitives.jsx';
import { toast } from '../../modules/toast.js';
import useStore from '../../store.js';
import ItemIcon from './ItemIcon.jsx';
import { applyFetchedIcon } from '../../modules/publicBookmarks.js';

// 网址条目的新建/编辑弹窗。
//
// 管理页与公开页共用同一套表单：公开页在登录态下也能直接改条目，
// 若各写一份，字段与校验迟早会漂移。
//
// request 由调用方注入（管理页有自己的 apiFetch 包装，公开页用通用请求），
// 默认实现走 /api/bookmarks 并带上登录态请求头。

export const ICON_TYPE_OPTIONS = [
  { value: '2', label: '网站图标' },
  { value: '1', label: '文字' },
  { value: '3', label: 'Emoji 字符' },
];

export const OPEN_METHOD_OPTIONS = [
  { value: '2', label: '新窗口打开' },
  { value: '1', label: '当前页打开' },
];

export function emptyItemForm(groupId) {
  return {
    id: 0,
    group_id: groupId || 0,
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

// 预览下方的提示文案：说明当前会显示成什么，以及缺什么才会变成占位图标。
// 这些「静默回退」的规则不写出来，用户会以为是自己填错了。
function iconPreviewHint(form) {
  const type = Number(form.icon_type);
  if (type === 2) {
    if (!String(form.icon_src || '').trim()) {
      return '未填写图片地址，将显示占位图标。可点「获取图标」自动抓取。';
    }
    return '图片加载失败时会自动回退为占位图标。';
  }
  if (type === 1) {
    return String(form.icon_text || '').trim()
      ? '只取第一个字符显示。'
      : '未填写文字，将显示占位图标。';
  }
  if (type === 3) {
    return String(form.icon_text || '').trim()
      ? '按原样显示 Emoji 或字符。'
      : '未填写字符，将显示占位图标。';
  }
  return '将显示占位图标。';
}

const defaultRequest = async (path, options = {}) => {
  const response = await fetch(`/api/bookmarks${path}`, {
    ...options,
    headers: { ...(useStore.getState().getAuthHeaders?.() || {}), ...(options.headers || {}) },
  });
  const payload = await response.json().catch(() => ({}));
  if (!response.ok) {
    const error = new Error(payload.message || payload.error || `HTTP ${response.status}`);
    error.status = response.status;
    throw error;
  }
  return payload;
};

export default function ItemFormDialog({
  open,
  form,
  onOpenChange,
  onFormChange,
  onSave,
  /** 可选：覆盖请求实现（管理页传入自己的 apiFetch）。 */
  request = defaultRequest,
  /** 可选：聚合页新建时展示分组选择。 */
  groups = null,
}) {
  const [fetching, setFetching] = useState(false);

  useEffect(() => {
    setFetching(false);
  }, [open]);

  // 单字段更新走函数式，避免同一次事件里连调两次时，
  // 因闭包里的 form 陈旧而互相覆盖（曾经导致 icon_type 改不过去）。
  // onFormChange 的调用方传入的都是 useState 的 setter，支持函数式更新。
  const setField = (key, value) => onFormChange(prev => ({ ...(prev ?? form), [key]: value }));

  const fetchIcon = async () => {
    if (!form.url.trim()) {
      toast.error('请先填写网址');
      return;
    }
    setFetching(true);
    try {
      const data = await request('/favicon/fetch', {
        method: 'POST',
        body: JSON.stringify({ url: form.url.trim() }),
      });
      if (data.success === false) {
        toast.error(`图标获取失败：${data.error || '未知错误'}`);
      } else {
        const iconSrc = data.data?.icon_src || '';
        if (!iconSrc) {
          toast.error('图标获取失败：未返回图标地址');
        } else {
          // 走 applyFetchedIcon：一次写入 type 与 src 两个字段。
          // 若拆成两次 setField，会因都基于陈旧 form 展开而互相覆盖，type 改不过去。
          onFormChange(prev => applyFetchedIcon(prev ?? form, iconSrc));
          toast.success('已获取网站图标');
        }
      }
    } catch (error) {
      toast.error(`图标获取失败：${error.message}`);
    } finally {
      setFetching(false);
    }
  };

  const canSave = Boolean(form.title.trim() && form.url.trim())
    // 聚合页新建时必须先选分组（编辑时分组已定，无需再选）
    && (!groups || form.id || form.group_id);

  return (
    <LayerDialog.Root open={open} onOpenChange={onOpenChange}>
      <LayerDialog.Content size="sm">
        <LayerDialog.Title>{form.id ? '编辑网址' : '新建网址'}</LayerDialog.Title>
        <LayerDialog.Body>
          <div className="space-y-3">
            {/* 聚合页新建：需要先确定放进哪个分组 */}
            {groups && !form.id && (
              <div>
                <div className="mb-1 text-xs font-medium text-kumo-strong">所属分组</div>
                <Select
                  size="sm"
                  className="w-full"
                  value={form.group_id ? String(form.group_id) : ''}
                  onValueChange={value => setField('group_id', Number(value))}
                  placeholder="请选择分组"
                  items={groups.map(group => ({ value: String(group.id), label: group.title }))}
                  aria-label="所属分组"
                />
              </div>
            )}
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
            {/* 图标预览：始终显示当前表单值对应的图标，与公开页/管理页用同一套渲染规则
                （ItemIcon），所见即所得。改类型、改地址、改颜色都会即时反映。 */}
            <div className="flex items-center gap-3 rounded-lg border border-kumo-line bg-kumo-recessed/30 p-3">
              <ItemIcon
                item={form}
                size="h-10 w-10"
                className="border border-kumo-line/60 bg-kumo-base"
              />
              <div className="min-w-0 flex-1">
                <div className="text-xs font-medium text-kumo-strong">图标预览</div>
                <div className="mt-0.5 text-[11px] leading-relaxed text-kumo-subtle">
                  {iconPreviewHint(form)}
                </div>
              </div>
            </div>
            <div className="grid gap-3 cq-sm:grid-cols-2">
              <div>
                <div className="mb-1 text-xs font-medium text-kumo-strong">图标类型</div>
                <Select
                  size="sm"
                  className="w-full"
                  value={String(form.icon_type)}
                  onValueChange={value => setField('icon_type', Number(value))}
                  items={ICON_TYPE_OPTIONS}
                  aria-label="图标类型"
                />
              </div>
              <div>
                <div className="mb-1 text-xs font-medium text-kumo-strong">打开方式</div>
                <Select
                  size="sm"
                  className="w-full"
                  value={String(form.open_method)}
                  onValueChange={value => setField('open_method', Number(value))}
                  items={OPEN_METHOD_OPTIONS}
                  aria-label="打开方式"
                />
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
        </LayerDialog.Body>
        <LayerDialog.Actions dismissLabel="取消">
          <LayerDialog.Actions.Primary type="button" disabled={!canSave} onClick={() => onSave()}>保存</LayerDialog.Actions.Primary>
        </LayerDialog.Actions>
      </LayerDialog.Content>
    </LayerDialog.Root>
  );
}
