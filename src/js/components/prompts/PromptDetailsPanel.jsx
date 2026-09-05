import React, { useMemo } from 'react';
import { Badge, ClipboardText, Empty, Field } from '@cloudflare/kumo';
import { Button } from '@cloudflare/kumo/components/button';
import { Input } from '@cloudflare/kumo/components/input';
import { Select } from '@cloudflare/kumo/components/select';

const VISIBILITY_LABELS = {
  private: '私有',
  unlisted: '不公开索引',
  public: '公开',
};

function parseTags(value) {
  try {
    return JSON.parse(value || '[]');
  } catch {
    return [];
  }
}

export default function PromptDetailsPanel({ entry, versions, onUpdate, onRestoreVersion }) {
  const tags = useMemo(() => parseTags(entry?.tags_json), [entry?.tags_json]);
  if (!entry) return null;

  const publicPageUrl = `${window.location.origin}/p/${entry.public_id}`;
  const directLinkUrl = `${window.location.origin}/api/prompts/d/${entry.public_id}`;
  const isPublished = entry.latest_published_version_no > 0;

  return (
    <div className="divide-y divide-kumo-line text-xs">
      <section className="space-y-3 p-3">
        <div className="flex items-center justify-between gap-2">
          <span className="font-semibold text-kumo-strong">条目属性</span>
          <Badge variant={isPublished ? 'success' : 'secondary'}>
            {isPublished ? `已发布 v${entry.latest_published_version_no}` : '草稿'}
          </Badge>
        </div>
        <Field label="可见性">
          <Select alignItemWithTrigger
            size="sm"
            value={entry.visibility}
            onValueChange={value => onUpdate({ visibility: value })}
            renderValue={value => VISIBILITY_LABELS[value] || value}
          >
            <Select.Option value="private">私有</Select.Option>
            <Select.Option value="unlisted">不公开索引</Select.Option>
            <Select.Option value="public">公开</Select.Option>
          </Select>
        </Field>
        <Input
          size="sm"
          label="标签"
          key={`${entry.id}-${entry.tags_json}`}
          defaultValue={tags.join(', ')}
          placeholder="运维, 分析, 写作"
          onBlur={event =>
            onUpdate({
              tags_json: JSON.stringify(
                event.target.value
                  .split(',')
                  .map(item => item.trim())
                  .filter(Boolean)
              ),
            })
          }
        />
      </section>

      <section className="space-y-2 p-3">
        <div>
          <div className="font-semibold text-kumo-strong">发布地址</div>
          <div className="mt-0.5 text-[11px] text-kumo-subtle">公开地址始终指向最近发布版本。</div>
        </div>
        {isPublished ? (
          <>
            <ClipboardText
              size="sm"
              text={publicPageUrl}
              className="w-full min-w-0"
              tooltip={{ text: '复制公开页', copiedText: '已复制' }}
            />
            <ClipboardText
              size="sm"
              text={directLinkUrl}
              className="w-full min-w-0"
              tooltip={{ text: '复制 AI 直链', copiedText: '已复制' }}
            />
          </>
        ) : (
          <div className="rounded-md border border-dashed border-kumo-line px-3 py-4 text-center text-kumo-subtle">
            首次发布后生成地址
          </div>
        )}
      </section>

      <section className="p-3">
        <div className="mb-2 flex items-center justify-between">
          <span className="font-semibold text-kumo-strong">版本记录</span>
          <span className="text-[11px] text-kumo-subtle">{versions.length} 个版本</span>
        </div>
        {versions.length === 0 ? (
          <Empty size="sm" title="暂无版本" />
        ) : (
          <div className="max-h-64 space-y-1 overflow-y-auto pr-1 scrollbar-thin">
            {versions.map(version => (
              <div key={version.id} className="rounded-md border border-kumo-line p-2">
                <div className="flex items-center justify-between gap-2">
                  <div className="min-w-0">
                    <div className="font-semibold text-kumo-strong">v{version.version_no}</div>
                    <div className="truncate text-[10px] text-kumo-subtle">
                      {version.created_at?.slice(0, 16)}
                    </div>
                  </div>
                  <Button size="sm" variant="ghost" onClick={() => onRestoreVersion(version)}>
                    恢复
                  </Button>
                </div>
                <ClipboardText
                  size="sm"
                  text={`${directLinkUrl}/versions/${version.version_no}`}
                  className="mt-2 w-full min-w-0"
                  tooltip={{ text: '复制固定版本直链', copiedText: '已复制' }}
                />
              </div>
            ))}
          </div>
        )}
      </section>
    </div>
  );
}
