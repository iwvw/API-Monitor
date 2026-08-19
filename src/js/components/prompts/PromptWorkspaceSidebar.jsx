import React from 'react';
import { Badge, Empty } from '@cloudflare/kumo';
import { Button } from '@cloudflare/kumo/components/button';
import { Input } from '@cloudflare/kumo/components/input';
import { FileText, Folder, Plus, Star } from '../Icons.jsx';
import { iconButtonIconClass } from '../ui/AppPrimitives.jsx';

export default function PromptWorkspaceSidebar({
  collections,
  entries,
  search,
  onSearchChange,
  selectedCollectionId,
  onSelectCollection,
  selectedEntryId,
  onSelectEntry,
  starredOnly,
  onToggleStarredOnly,
  onCreateEntry,
  onToggleEntryStar,
}) {
  return (
    <div className="flex h-full min-h-0 flex-col bg-kumo-base">
      <div className="space-y-2 border-b border-kumo-line p-3">
        <Input
          size="sm"
          value={search}
          onChange={event => onSearchChange(event.target.value)}
          placeholder="搜索标题或正文"
          aria-label="搜索提示词"
        />
        <div className="flex gap-2">
          <Button
            size="sm"
            variant="primary"
            className="flex-1"
            icon={<Plus className={iconButtonIconClass} />}
            onClick={onCreateEntry}
          >
            新建提示词
          </Button>
          <Button
            size="sm"
            variant={starredOnly ? 'primary' : 'secondary'}
            shape="square"
            aria-label={starredOnly ? '显示全部提示词' : '仅显示收藏'}
            icon={<Star className={iconButtonIconClass} />}
            onClick={onToggleStarredOnly}
          />
        </div>
      </div>

      <div className="border-b border-kumo-line px-2 py-3">
        <div className="mb-2 flex items-center justify-between px-1">
          <span className="text-[11px] font-semibold text-kumo-subtle">集合</span>
          <Badge variant="secondary">{collections.length}</Badge>
        </div>
        <div className="space-y-1">
          <Button
            size="sm"
            variant={selectedCollectionId === null ? 'secondary' : 'ghost'}
            className="w-full !justify-start"
            icon={<FileText className="h-3.5 w-3.5" />}
            onClick={() => onSelectCollection(null)}
          >
            全部提示词
          </Button>
          {collections.map(collection => (
            <Button
              key={collection.id}
              size="sm"
              variant={selectedCollectionId === collection.id ? 'secondary' : 'ghost'}
              className="w-full !justify-start"
              icon={<Folder className="h-3.5 w-3.5" />}
              onClick={() => onSelectCollection(collection.id)}
            >
              <span className="truncate">{collection.name}</span>
            </Button>
          ))}
        </div>
      </div>

      <div className="flex min-h-0 flex-1 flex-col">
        <div className="flex items-center justify-between px-3 py-2">
          <span className="text-[11px] font-semibold text-kumo-subtle">提示词</span>
          <span className="text-[11px] text-kumo-subtle">{entries.length} 条</span>
        </div>
        <div className="min-h-0 flex-1 overflow-y-auto px-2 pb-2 scrollbar-thin">
          {entries.map(item => (
            <div
              key={item.id}
              className={`mb-1 flex items-center gap-1 rounded-md border px-1 py-1 ${
                selectedEntryId === item.id
                  ? 'border-kumo-line bg-kumo-tint'
                  : 'border-transparent hover:border-kumo-line hover:bg-kumo-recessed/40'
              }`}
            >
              <Button
                size="sm"
                variant="ghost"
                className="min-w-0 flex-1 !justify-start"
                onClick={() => onSelectEntry(item.id)}
              >
                <span className="min-w-0 text-left">
                  <span className="block truncate text-xs font-medium text-kumo-strong">
                    {item.title}
                  </span>
                  <span className="mt-0.5 block truncate text-[10px] text-kumo-subtle">
                    {item.latest_published_version_no > 0
                      ? `已发布 v${item.latest_published_version_no}`
                      : '仅草稿'}
                  </span>
                </span>
              </Button>
              <Button
                size="sm"
                variant="ghost"
                shape="square"
                aria-label={item.starred ? `取消收藏 ${item.title}` : `收藏 ${item.title}`}
                icon={
                  <Star
                    className={`h-3.5 w-3.5 ${item.starred ? 'fill-current text-brand' : ''}`}
                  />
                }
                onClick={() => onToggleEntryStar(item)}
              />
            </div>
          ))}
          {entries.length === 0 && (
            <Empty
              size="sm"
              title={search ? '没有匹配的提示词' : '当前集合为空'}
              description={search ? '更换关键词。' : '新建提示词开始整理。'}
            />
          )}
        </div>
      </div>
    </div>
  );
}
