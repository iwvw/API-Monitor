import React, { useEffect, useRef, useState } from 'react';
import { Button } from '@cloudflare/kumo/components/button';
import { Input } from '@cloudflare/kumo/components/input';
import { Select } from '@cloudflare/kumo/components/select';
import { Switch } from '@cloudflare/kumo/components/switch';
import { cx } from '../../components/ui/AppPrimitives.jsx';
import { toast } from '../../modules/toast.js';
import useStore from '../../store.js';
import { Trash, Upload } from '../../components/Icons.jsx';
import {
  DEFAULT_BG_DIM,
  PAGE_WIDTH_OPTIONS,
  backgroundLayerStyle,
  backgroundOverlayStyle,
  getBackgroundConfig,
  getPublicPageSettings,
  withBackgroundConfig,
} from '../../modules/publicPageBackground.js';

const SIZE_OPTIONS = [
  { value: 'cover', label: '铺满（裁切）' },
  { value: 'contain', label: '完整显示' },
  { value: 'auto', label: '原始尺寸' },
];

async function request(path, options = {}) {
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

/**
 * 网址导航公开页的背景自定义面板。
 *
 * 配置存放在 public-settings 单行表里（公开页全局设置），
 * 因此这里只管读改 config；背景图文件本身走独立的背景库接口。
 */
export default function BackgroundSettings({ config = {}, onChange }) {
  const bg = getBackgroundConfig(config);
  const page = getPublicPageSettings(config);
  const [uploading, setUploading] = useState(false);
  const [library, setLibrary] = useState([]);
  const [showLibrary, setShowLibrary] = useState(false);
  const fileRef = useRef(null);

  const patch = (next) => onChange?.(withBackgroundConfig(config, next));

  useEffect(() => {
    if (!showLibrary) return undefined;
    let cancelled = false;
    request('/backgrounds')
      .then(payload => { if (!cancelled) setLibrary(Array.isArray(payload.data) ? payload.data : []); })
      .catch(error => toast.error(`读取背景库失败：${error.message}`));
    return () => { cancelled = true; };
  }, [showLibrary]);

  const upload = async (event) => {
    const file = event.target.files?.[0];
    if (!file) return;
    if (!/^image\//.test(file.type)) {
      toast.error('请选择图片文件');
      event.target.value = '';
      return;
    }
    setUploading(true);
    try {
      const form = new FormData();
      form.append('file', file);
      form.append('name', file.name);
      const payload = await request('/backgrounds/upload', { method: 'POST', body: form });
      const url = payload.data?.url;
      if (!url) throw new Error('上传成功但未返回地址');
      patch({ bgImage: url });
      toast.success('背景图已上传');
      setShowLibrary(true);
    } catch (error) {
      toast.error(`上传失败：${error.message}`);
    } finally {
      setUploading(false);
      event.target.value = '';
    }
  };

  const removeFromLibrary = async (id) => {
    try {
      await request(`/backgrounds/${id}`, { method: 'DELETE' });
      setLibrary(current => current.filter(entry => entry.id !== id));
      toast.success('已从背景库删除');
    } catch (error) {
      toast.error(`删除失败：${error.message}`);
    }
  };

  // 实时预览：与公开页用同一套样式函数，所见即所得
  const previewLayer = backgroundLayerStyle(config);
  const previewOverlay = backgroundOverlayStyle(config);

  return (
    // 不再自带外框与底色：外层 LayerCard 已经提供容器，
    // 内层再包一层会出现「卡片套卡片」的双层边框。
    // 同理不再重复「公开页背景」标题——卡片头部已有同名标题。
    <div className="space-y-3">
      {/* 预览 */}
      <div className="relative h-28 overflow-hidden rounded-lg border border-kumo-line">
        {previewLayer ? (
          <>
            <div className="absolute inset-0" style={previewLayer} />
            <div className="absolute inset-0" style={previewOverlay} />
          </>
        ) : (
          <div className="absolute inset-0 bg-kumo-base" />
        )}
        <div className="public-glass absolute bottom-2 left-2 right-2 rounded-lg px-2.5 py-1.5">
          <div className="truncate text-xs font-medium text-kumo-strong">预览：分组标题</div>
        </div>
        <div className="public-glass-item absolute bottom-2 right-2 flex items-center gap-1.5 rounded-lg px-2 py-1">
          <span className="flex h-5 w-5 items-center justify-center rounded bg-kumo-recessed text-[10px] font-semibold text-kumo-strong">G</span>
          <span className="text-[11px] text-kumo-strong">示例网址</span>
        </div>
      </div>

      {/* 图片来源 */}
      <div className="space-y-2">
        <Input
          size="sm"
          label="背景图地址"
          placeholder="https://example.com/bg.jpg"
          value={bg.bgImage}
          onChange={event => patch({ bgImage: event.target.value })}
        />
        <div className="flex flex-wrap items-center gap-2">
          <input
            ref={fileRef}
            type="file"
            accept="image/*"
            className="hidden"
            aria-label="选择背景图片文件"
            onChange={upload}
          />
          <Button
            size="sm"
            variant="secondary"
            loading={uploading}
            icon={<Upload className="h-3.5 w-3.5" />}
            onClick={() => fileRef.current?.click()}
          >
            上传图片
          </Button>
          <Button size="sm" variant="secondary" onClick={() => setShowLibrary(current => !current)}>
            {showLibrary ? '收起背景库' : `背景库${library.length ? `（${library.length}）` : ''}`}
          </Button>
          {(bg.bgImage || bg.bgColor) && (
            <Button size="sm" variant="ghost" onClick={() => patch({ bgImage: '', bgColor: '' })}>
              清除背景
            </Button>
          )}
        </div>
      </div>

      {/* 背景库 */}
      {showLibrary && (
        <div className="rounded-lg border border-kumo-line p-2">
          {library.length === 0 ? (
            <div className="py-3 text-center text-xs text-kumo-subtle">背景库为空，先上传一张图片。</div>
          ) : (
            <div className="grid grid-cols-3 gap-2 cq-sm:grid-cols-4">
              {library.map(entry => (
                <div key={entry.id} className="group relative overflow-hidden rounded border border-kumo-line">
                  <Button
                    type="button"
                    size="sm"
                    variant="ghost"
                    className="h-16 w-full rounded-none bg-cover bg-center p-0"
                    style={{ backgroundImage: `url("${entry.url}")` }}
                    title={entry.name}
                    aria-label={`使用背景 ${entry.name}`}
                    onClick={() => patch({ bgImage: entry.url })}
                  />
                  {bg.bgImage === entry.url && (
                    <span className="absolute left-1 top-1 rounded bg-brand px-1 text-[10px] font-semibold text-white">使用中</span>
                  )}
                  <Button
                    size="sm"
                    variant="ghost"
                    shape="square"
                    className="absolute right-0.5 top-0.5 opacity-0 transition group-hover:opacity-100"
                    icon={<Trash className="h-3 w-3" />}
                    aria-label={`从背景库删除 ${entry.name}`}
                    onClick={() => removeFromLibrary(entry.id)}
                  />
                </div>
              ))}
            </div>
          )}
        </div>
      )}

      {/* 参数 */}
      <div className="grid gap-3 cq-sm:grid-cols-2">
        <div>
          <div className="mb-1 text-xs font-medium text-kumo-strong">填充方式</div>
          <Select
            size="sm"
            className="w-full"
            value={bg.bgSize}
            onValueChange={value => patch({ bgSize: value })}
            items={SIZE_OPTIONS}
            aria-label="背景填充方式"
          />
        </div>
        <div>
          <div className="mb-1 text-xs font-medium text-kumo-strong">背景色（兜底）</div>
          <div className="flex items-center gap-2">
            <label className="relative flex h-8 w-12 cursor-pointer items-center justify-center rounded border border-kumo-line" title="选择背景色">
              <span
                className={cx('h-4 w-4 rounded-full', bg.bgColor ? '' : 'bg-kumo-recessed')}
                style={bg.bgColor ? { backgroundColor: bg.bgColor } : undefined}
              />
              <input
                type="color"
                className="sr-only"
                value={bg.bgColor || '#1f2937'}
                onChange={event => patch({ bgColor: event.target.value })}
                aria-label="选择背景色"
              />
            </label>
            {bg.bgColor && (
              <Button size="sm" variant="ghost" onClick={() => patch({ bgColor: '' })}>重置</Button>
            )}
          </div>
        </div>
      </div>

      <div className="grid gap-3 cq-sm:grid-cols-2">
        <Input
          size="sm"
          type="number"
          min="0"
          max="40"
          label="模糊强度（0-40 px）"
          value={String(bg.bgBlur)}
          onChange={event => patch({ bgBlur: event.target.value })}
        />
        <Input
          size="sm"
          type="number"
          min="0"
          max="90"
          label="压暗程度（0-90 %）"
          value={String(Math.round(bg.bgDim * 100))}
          onChange={event => patch({ bgDim: Number(event.target.value) / 100 })}
        />
      </div>

      <div className="flex items-center justify-between gap-3">
        <div className="min-w-0">
          <div className="text-xs font-medium text-kumo-strong">固定背景（视差）</div>
          <div className="mt-0.5 text-[11px] text-kumo-subtle">滚动时背景不跟随移动。</div>
        </div>
        <Switch checked={bg.bgFixed} onCheckedChange={checked => patch({ bgFixed: Boolean(checked) })} />
      </div>

      {/* 内容区宽度：与背景同属公开页全局设置，因此放在同一份 config 里 */}
      <div>
        <div className="mb-1 text-xs font-medium text-kumo-strong">内容区宽度</div>
        <Select
          size="sm"
          className="w-full"
          value={page.pageWidth}
          onValueChange={value => patch({ pageWidth: value })}
          items={PAGE_WIDTH_OPTIONS}
          aria-label="公开页内容区宽度"
        />
        <div className="mt-1 text-[11px] text-kumo-subtle">
          网页导航区（含顶部搜索）的最大宽度；选「铺满」则随视口无限加宽。
        </div>
      </div>

      <p className="text-[11px] leading-relaxed text-kumo-subtle">
        压暗默认 {Math.round(DEFAULT_BG_DIM * 100)}%，用于保证浅色背景上的文字仍然清晰；
        背景图为空时使用背景色。
      </p>
    </div>
  );
}
