import React, { useState } from 'react';
import { Button } from '@cloudflare/kumo/components/button';
import { Input } from '@cloudflare/kumo/components/input';
import { cx } from '../../components/ui/AppPrimitives.jsx';
import { Globe, Plus, RotateCw, Trash } from '../../components/Icons.jsx';
import {
  DEFAULT_SEARCH_ENGINES,
  SEARCH_ENGINE_MAX_COUNT,
  idFromUrl,
  isImageIcon,
  normalizeEngine,
  resolveSearchEngines,
} from '../../modules/publicSearch.js';

// 搜索引擎图标预览：与公开页 EngineIcon 同一套解析规则
// （Iconify 名 或 图片地址），所见即所得。
import EngineIconPreview from './EngineIconPreview.jsx';

/**
 * 公开页搜索引擎的自定义面板。
 *
 * 配置存在 public-settings 的 searchEngines 字段（与背景、内容宽度同一份 config），
 * 因此这里只管读改 config，不涉及额外数据表。
 *
 * 语义约定：
 *   - 列表为空 → 公开页回落到内置默认引擎（bing / google / duckduckgo）；
 *   - 保存非空列表 → **整份替换**，不与内置合并。
 *     合并的话「删掉某个内置引擎」就删不掉了。
 */
export default function SearchEnginesSettings({ config = {}, onChange }) {
  const configured = Array.isArray(config?.searchEngines) ? config.searchEngines : [];
  // 表单里始终展示「实际生效」的列表：未配置时用内置默认填充，
  // 让用户从一份可编辑的初始值开始改，而不是面对空列表。
  const engines = resolveSearchEngines(config);
  const usingDefault = configured.length === 0;

  // 展开的编辑项索引；null 表示全部折叠
  const [editing, setEditing] = useState(null);

  const commit = (list) => {
    // 归一化后再写回：拦掉非法项（缺 %s、非 http），
    // 与后端 sanitizeSearchEngines 同一套规则，避免保存后才发现被丢弃。
    const normalized = list
      .slice(0, SEARCH_ENGINE_MAX_COUNT)
      .map(item => normalizeEngine(item))
      .filter(Boolean)
      .map((item, index) => ({ ...item, id: item.id || idFromUrl(item.url) || `engine-${index + 1}` }));
    onChange?.({ ...config, searchEngines: normalized });
  };

  const updateAt = (index, patch) => {
    commit(engines.map((item, i) => (i === index ? { ...item, ...patch } : item)));
  };

  const removeAt = (index) => {
    commit(engines.filter((_, i) => i !== index));
    setEditing(null);
  };

  const addEngine = () => {
    if (engines.length >= SEARCH_ENGINE_MAX_COUNT) return;
    const next = [
      ...engines,
      { id: '', label: '', url: '', icon: '', color: '' },
    ];
    commit(next);
    setEditing(next.length - 1);
  };

  const resetToDefault = () => {
    // 清空配置 = 回落内置默认（不是复制一份默认列表进配置）
    onChange?.({ ...config, searchEngines: [] });
    setEditing(null);
  };

  return (
    <div className="space-y-3">
      <div className="flex flex-wrap items-center justify-between gap-2">
        <div className="min-w-0">
          <div className="text-xs font-medium text-kumo-strong">搜索引擎</div>
          <div className="mt-0.5 text-[11px] text-kumo-subtle">
            {usingDefault
              ? `当前使用内置默认（${DEFAULT_SEARCH_ENGINES.length} 个）。修改后会整份替换。`
              : `已自定义 ${configured.length} 个引擎。`}
          </div>
        </div>
        <div className="flex shrink-0 gap-2">
          {!usingDefault && (
            <Button size="sm" variant="ghost" onClick={resetToDefault} title="清空自定义，恢复内置默认列表">
              恢复默认
            </Button>
          )}
          <Button
            size="sm"
            variant="secondary"
            icon={<Plus className="h-3.5 w-3.5" />}
            onClick={addEngine}
            disabled={engines.length >= SEARCH_ENGINE_MAX_COUNT}
          >
            添加引擎
          </Button>
        </div>
      </div>

      {engines.length === 0 ? (
        <div className="rounded-lg border border-dashed border-kumo-line px-3 py-5 text-center text-xs text-kumo-subtle">
          还没有引擎，点「添加引擎」新建一个。
        </div>
      ) : (
        <div className="space-y-2">
          {engines.map((engine, index) => {
            const invalid = !normalizeEngine(engine);
            const open = editing === index;
            return (
              <div
                key={`${engine.id || 'new'}-${index}`}
                className={cx(
                  'rounded-lg border transition',
                  invalid ? 'border-kumo-warning/50 bg-kumo-warning/5' : 'border-kumo-line bg-kumo-recessed/20'
                )}
              >
                {/* 折叠态：图标 + 名称 + 地址摘要，点一下展开编辑 */}
                <div className="flex items-center gap-2 p-2">
                  <span className="flex h-7 w-7 shrink-0 items-center justify-center rounded bg-kumo-base">
                    <EngineIconPreview icon={engine.icon} label={engine.label} color={engine.color} size="h-4 w-4" />
                  </span>
                  <button
                    type="button"
                    className="min-w-0 flex-1 text-left"
                    onClick={() => setEditing(open ? null : index)}
                    aria-expanded={open}
                  >
                    <div className="truncate text-xs font-semibold text-kumo-strong">
                      {engine.label || <span className="text-kumo-warning">未命名</span>}
                    </div>
                    <div className="truncate font-mono text-[11px] text-kumo-subtle">
                      {engine.url || <span className="text-kumo-warning">未填写搜索地址</span>}
                    </div>
                  </button>
                  <Button
                    size="sm"
                    variant="ghost"
                    shape="square"
                    icon={<Trash className="h-3.5 w-3.5" />}
                    aria-label={`删除引擎 ${engine.label || index + 1}`}
                    title="删除"
                    onClick={() => removeAt(index)}
                  />
                </div>

                {open && (
                  <div className="space-y-2.5 border-t border-kumo-line/60 p-2.5">
                    <div className="grid gap-2.5 cq-sm:grid-cols-2">
                      <Input
                        size="sm"
                        label="名称"
                        placeholder="例如：必应"
                        value={engine.label}
                        onChange={event => updateAt(index, { label: event.target.value })}
                      />
                      <Input
                        size="sm"
                        label="图标（可选）"
                        placeholder="logos:bing 或 https://…/icon.png"
                        value={engine.icon}
                        onChange={event => updateAt(index, { icon: event.target.value })}
                      />
                    </div>
                    <div>
                      <Input
                        size="sm"
                        label="搜索地址（用 %s 代表关键词）"
                        placeholder="https://www.bing.com/search?q=%s"
                        value={engine.url}
                        onChange={event => updateAt(index, { url: event.target.value })}
                      />
                      <div className="mt-1 text-[11px] text-kumo-subtle">
                        必须包含 <code className="font-mono">%s</code>，提交时会被替换成关键词。
                        {engine.url && !engine.url.includes('%s') && (
                          <span className="text-kumo-warning"> 当前地址缺少 %s，保存后该项会被忽略。</span>
                        )}
                      </div>
                    </div>
                    <div className="flex flex-wrap items-end gap-3">
                      <div>
                        <div className="mb-1 text-[11px] font-medium text-kumo-strong">回退底色</div>
                        <label
                          className="relative flex h-8 w-12 cursor-pointer items-center justify-center rounded border border-kumo-line"
                          title="图标加载失败或未填图标时显示的底色"
                        >
                          <span
                            className={cx('h-4 w-4 rounded-full', engine.color ? '' : 'bg-kumo-recessed')}
                            style={engine.color ? { backgroundColor: engine.color } : undefined}
                          />
                          <input
                            type="color"
                            className="sr-only"
                            value={engine.color || '#808080'}
                            onChange={event => updateAt(index, { color: event.target.value })}
                            aria-label="选择回退底色"
                          />
                        </label>
                      </div>
                      <Button
                        size="sm"
                        variant="ghost"
                        icon={<RotateCw className="h-3.5 w-3.5" />}
                        onClick={() => setEditing(null)}
                      >
                        收起
                      </Button>
                    </div>
                  </div>
                )}
              </div>
            );
          })}
        </div>
      )}

      <p className="text-[11px] leading-relaxed text-kumo-subtle">
        图标可填 Iconify 名称（如 <code className="font-mono">logos:bing</code>、
        <code className="font-mono">logos:google-icon</code>）或图片地址。
        留空时用「回退底色 + 名称首字」显示。
      </p>
    </div>
  );
}
