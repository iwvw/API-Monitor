import React from 'react';
import { Badge, ClipboardText, Empty, LayerCard } from '@cloudflare/kumo';
import { Button } from '@cloudflare/kumo/components/button';
import { Input } from '@cloudflare/kumo/components/input';
import { Select } from '@cloudflare/kumo/components/select';
import { Switch } from '@cloudflare/kumo/components/switch';
import { ExternalLink, FileText, Folder, Plus, Star, Trash } from '../Icons.jsx';
import { iconButtonIconClass, SectionCard } from '../ui/AppPrimitives.jsx';

const VISIBILITY_LABELS = { private: '私有', unlisted: '不公开索引', public: '公开' };

export function PromptCollectionsView({
  collections,
  entries,
  onCreate,
  onRename,
  onDelete,
  onOpenCollection,
  deleteIsArmed = () => false,
}) {
  const countByCollection = entries.reduce((counts, entry) => {
    const key = entry.collection_id || 'unfiled';
    counts[key] = (counts[key] || 0) + 1;
    return counts;
  }, {});

  return (
    <div className="min-h-0 flex-1 overflow-y-auto pr-1 scrollbar-thin">
      <div className="mb-4 flex flex-wrap items-center justify-between gap-3">
        <div>
          <h2 className="text-base font-semibold text-kumo-strong">集合</h2>
          <p className="mt-1 text-xs text-kumo-subtle">
            按用途整理提示词，不影响发布地址。
          </p>
        </div>
        <Button
          size="sm"
          variant="primary"
          icon={<Plus className={iconButtonIconClass} />}
          onClick={onCreate}
        >
          新建集合
        </Button>
      </div>

      {collections.length === 0 ? (
        <Empty
          className="min-h-72"
          icon={<Folder className="h-10 w-10 text-kumo-inactive" />}
          title="还没有集合"
          description="将同一工作流的提示词放在一起。"
          contents={
            <Button
              size="sm"
              variant="primary"
              icon={<Plus className={iconButtonIconClass} />}
              onClick={onCreate}
            >
              新建集合
            </Button>
          }
        />
      ) : (
        <div className="grid items-start grid-cols-1 gap-3 cq-sm:grid-cols-2 cq-xl:grid-cols-3">
          {collections.map(collection => (
            <LayerCard key={collection.id} className="overflow-hidden p-0">
              <LayerCard.Primary className="p-4">
                <div className="flex items-start gap-3">
                  <div className="flex h-9 w-9 shrink-0 items-center justify-center rounded-md border border-kumo-line bg-kumo-recessed/40">
                    <Folder className="h-4 w-4 text-brand" />
                  </div>
                  <div className="min-w-0 flex-1">
                    <div className="truncate text-sm font-semibold text-kumo-strong">
                      {collection.name}
                    </div>
                    <div className="mt-1 line-clamp-2 min-h-8 text-xs text-kumo-subtle">
                      {collection.description || '暂无描述'}
                    </div>
                    <Badge variant="secondary" className="mt-3">
                      {countByCollection[collection.id] || 0} 条提示词
                    </Badge>
                  </div>
                </div>
              </LayerCard.Primary>
              <LayerCard.Secondary className="flex items-center justify-end gap-1 border-t border-kumo-line px-2 py-2">
                <Button
                  size="sm"
                  variant="secondary"
                  onClick={() => onOpenCollection(collection.id)}
                >
                  查看
                </Button>
                <Button size="sm" variant="ghost" onClick={() => onRename(collection)}>
                  重命名
                </Button>
                <Button
                  size="sm"
                  variant={
                    deleteIsArmed(collection.id) ? 'destructive' : 'secondary-destructive'
                  }
                  shape="square"
                  aria-label={`删除集合 ${collection.name}`}
                  icon={<Trash className={iconButtonIconClass} />}
                  onClick={() => onDelete(collection)}
                />
              </LayerCard.Secondary>
            </LayerCard>
          ))}
        </div>
      )}
    </div>
  );
}

export function PromptPublishedView({ entries, onOpen, onDelete, deleteIsArmed = () => false }) {
  return (
    <div className="min-h-0 flex-1 overflow-y-auto pr-1 scrollbar-thin">
      <div className="mb-4">
        <h2 className="text-base font-semibold text-kumo-strong">已发布</h2>
        <p className="mt-1 text-xs text-kumo-subtle">管理可供人或外部 AI 使用的稳定版本。</p>
      </div>
      {entries.length === 0 ? (
        <Empty
          className="min-h-72"
          title="暂无已发布提示词"
          description="在工作区发布草稿后会显示在这里。"
        />
      ) : (
        <LayerCard className="overflow-hidden p-0">
          <div className="divide-y divide-kumo-line">
            {entries.map(item => {
              const directUrl = `${window.location.origin}/api/prompts/d/${item.public_id}`;
              return (
                <div key={item.id} className="flex flex-col gap-3 p-4 cq-sm:flex-row cq-sm:items-center">
                  <div className="flex min-w-0 flex-1 items-start gap-3">
                    <div className="flex h-9 w-9 shrink-0 items-center justify-center rounded-md border border-kumo-line bg-kumo-recessed/40">
                      <FileText className="h-4 w-4 text-brand" />
                    </div>
                    <div className="min-w-0 flex-1">
                      <div className="flex flex-wrap items-center gap-2">
                        <span className="truncate text-sm font-semibold text-kumo-strong">
                          {item.title}
                        </span>
                        {item.starred && (
                          <Star className="h-3.5 w-3.5 fill-current text-brand" />
                        )}
                        <Badge variant="success">v{item.latest_published_version_no}</Badge>
                      </div>
                      <div className="mt-1 text-xs text-kumo-subtle">
                        发布于 {item.latest_published_at?.slice(0, 16) || '未知时间'}
                      </div>
                      <ClipboardText
                        size="sm"
                        text={directUrl}
                        className="mt-2 max-w-xl"
                        tooltip={{ text: '复制 AI 直链', copiedText: '已复制' }}
                      />
                    </div>
                  </div>
                  <div className="flex shrink-0 items-center gap-1 self-end cq-sm:self-auto">
                    <Button
                      size="sm"
                      variant="secondary"
                      icon={<ExternalLink className={iconButtonIconClass} />}
                      onClick={() => onOpen(item.id)}
                    >
                      打开
                    </Button>
                    <Button
                      size="sm"
                      variant={
                        deleteIsArmed(item.id) ? 'destructive' : 'secondary-destructive'
                      }
                      shape="square"
                      aria-label={`删除提示词 ${item.title}`}
                      icon={<Trash className={iconButtonIconClass} />}
                      onClick={() => onDelete(item)}
                    />
                  </div>
                </div>
              );
            })}
          </div>
        </LayerCard>
      )}
    </div>
  );
}

export function PromptSettingsPanel({ settings, onChange, onSave, saving = false }) {
  if (!settings) return <Empty size="sm" title="正在加载设置" />;

  return (
    <div className="min-h-0 flex-1 overflow-y-auto pr-1 scrollbar-thin">
      <SectionCard
        title="默认策略"
        description="设置新条目的发布方式和公开访问边界"
        className="max-w-3xl self-start"
        bodyClassName="grid gap-4"
      >
        <div className="grid gap-4 cq-sm:grid-cols-2">
          <Select alignItemWithTrigger
            size="sm"
            label="默认可见性"
            value={settings.default_visibility}
            onValueChange={value => onChange({ default_visibility: value })}
            renderValue={value => VISIBILITY_LABELS[value] || value}
          >
            <Select.Option value="private">私有</Select.Option>
            <Select.Option value="unlisted">不公开索引</Select.Option>
            <Select.Option value="public">公开</Select.Option>
          </Select>
          <Select alignItemWithTrigger
            size="sm"
            label="原始直链格式"
            value={settings.default_direct_format}
            onValueChange={value => onChange({ default_direct_format: value })}
            renderValue={value => (value === 'text' ? '纯文本' : 'Markdown')}
          >
            <Select.Option value="markdown">Markdown</Select.Option>
            <Select.Option value="text">纯文本</Select.Option>
          </Select>
          <Input
            size="sm"
            type="number"
            min="0"
            max="3650"
            label="访问日志保留天数"
            value={String(settings.access_log_retention_days ?? 0)}
            onChange={event =>
              onChange({ access_log_retention_days: Math.max(0, Number(event.target.value) || 0) })
            }
          />
        </div>

        <div className="grid gap-3 border-t border-kumo-line pt-4 cq-sm:grid-cols-2">
          <Switch
            size="sm"
            label="允许公开页面"
            controlFirst={false}
            checked={settings.allow_public_pages}
            onCheckedChange={checked => onChange({ allow_public_pages: checked })}
          />
          <Switch
            size="sm"
            label="允许 AI 原始直链"
            controlFirst={false}
            checked={settings.allow_direct_links}
            onCheckedChange={checked => onChange({ allow_direct_links: checked })}
          />
        </div>

        <div className="flex justify-end border-t border-kumo-line pt-4">
          <Button size="sm" variant="primary" loading={saving} onClick={onSave}>
            保存设置
          </Button>
        </div>
      </SectionCard>
    </div>
  );
}
