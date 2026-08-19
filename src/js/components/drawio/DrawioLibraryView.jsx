import React from 'react';
import { Badge, Empty } from '@cloudflare/kumo';
import { Button } from '@cloudflare/kumo/components/button';
import { SkeletonLine } from '@cloudflare/kumo/components/loader';
import { Clock, Copy, Image, Plus, Trash, Upload } from '../Icons.jsx';
import { AppCard, iconButtonIconClass } from '../ui/AppPrimitives.jsx';
import { useConfirmPress } from '../../hooks/useConfirmPress.js';

function thumbnailStatus(document) {
  if (document.thumbnail_status === 'ready') return { label: '预览就绪', variant: 'success' };
  if (document.thumbnail_status === 'pending') return { label: '生成中', variant: 'warning' };
  if (document.thumbnail_status === 'failed') return { label: '预览失败', variant: 'error' };
  return { label: '待生成', variant: 'secondary' };
}

export default function DrawioLibraryView({
  documents,
  loading,
  search,
  onCreate,
  onImport,
  onOpen,
  onCopyPNG,
  onDelete,
  onRebuildThumbnail,
  copyingDocumentId,
}) {
  const { isArmed, confirmPress } = useConfirmPress();

  return (
    <div className="min-h-0 w-full min-w-0 flex-1 overflow-y-auto pr-1 scrollbar-thin">
      {loading ? (
        <div className="grid grid-cols-1 gap-3 cq-sm:grid-cols-2 cq-lg:grid-cols-3 cq-xl:grid-cols-5">
          {[0, 1, 2, 3, 4].map(item => (
            <SkeletonLine key={item} className="h-64 w-full" />
          ))}
        </div>
      ) : documents.length === 0 ? (
        <Empty
          className="min-h-80"
          icon={<Image className="h-10 w-10 text-kumo-inactive" />}
          title={search ? '没有匹配的图表' : '图库还是空的'}
          description={search ? '调整搜索关键词。' : undefined}
          contents={
            !search ? (
              <div className="flex items-center gap-2">
                <Button
                  size="sm"
                  variant="secondary"
                  icon={<Upload className={iconButtonIconClass} />}
                  onClick={onImport}
                >
                  导入文件
                </Button>
                <Button
                  size="sm"
                  variant="primary"
                  icon={<Plus className={iconButtonIconClass} />}
                  onClick={onCreate}
                >
                  新建图表
                </Button>
              </div>
            ) : null
          }
        />
      ) : (
        <div className="grid grid-cols-1 items-start gap-3 cq-sm:grid-cols-2 cq-lg:grid-cols-3 cq-xl:grid-cols-5">
          {documents.map(document => {
            const status = thumbnailStatus(document);
            return (
              <AppCard
                key={document.id}
                padding="none"
                className="flex min-w-0 flex-col overflow-hidden"
              >
                <div className="group h-48 w-full border-b border-kumo-line bg-kumo-recessed/20 cq-sm:h-52">
                  <div className="flex h-full w-full items-center justify-center overflow-hidden px-6 py-4">
                    {document.thumbnail_path && document.thumbnail_status === 'ready' ? (
                      <img
                        src={document.thumbnail_path}
                        alt=""
                        className="h-full w-full object-contain object-center"
                      />
                    ) : (
                      <span className="flex flex-col items-center gap-2 text-kumo-subtle">
                        <Image className="h-8 w-8" />
                        <span className="text-[11px]">
                          {document.cover_page_name || '画布预览'}
                        </span>
                      </span>
                    )}
                  </div>
                </div>
                <div className="border-t border-kumo-line p-3">
                  <div className="flex items-start justify-between gap-2">
                    <div className="min-w-0">
                      <div className="truncate text-sm font-semibold text-kumo-strong">
                        {document.title}
                      </div>
                      <div className="mt-1 flex flex-wrap items-center gap-x-3 gap-y-1 text-[11px] text-kumo-subtle">
                        <span>{document.page_count || 0} 页</span>
                        <span>v{document.latest_version_no || 0}</span>
                        <span className="inline-flex items-center gap-1">
                          <Clock className="h-3 w-3" />
                          {document.updated_at?.slice(0, 10)}
                        </span>
                      </div>
                    </div>
                    <Badge variant={status.variant}>{status.label}</Badge>
                  </div>
                </div>
                <div className="flex items-center justify-between gap-2 border-t border-kumo-line px-2 py-2">
                  <Button size="sm" variant="secondary" onClick={() => onOpen(document.id)}>
                    打开
                  </Button>
                  <div className="flex items-center gap-1">
                    {document.thumbnail_status !== 'ready' && (
                      <Button
                        size="sm"
                        variant="secondary"
                        onClick={() => onRebuildThumbnail(document.id)}
                      >
                        重建预览
                      </Button>
                    )}
                    <Button
                      size="sm"
                      variant="secondary"
                      shape="square"
                      aria-label={`复制 PNG ${document.title}`}
                      title="复制高清 PNG"
                      icon={<Copy className={iconButtonIconClass} />}
                      disabled={copyingDocumentId === document.id}
                      onClick={() => onCopyPNG(document)}
                    />
                    <Button
                      size="sm"
                      variant={isArmed(`library-${document.id}`) ? 'destructive' : 'secondary-destructive'}
                      shape="square"
                      aria-label={`删除图表 ${document.title}`}
                      title="删除图表"
                      icon={<Trash className={iconButtonIconClass} />}
                      onClick={async () => {
                        if (confirmPress(`library-${document.id}`, `删除图表「${document.title}」`)) {
                          await onDelete(document);
                        }
                      }}
                    />
                  </div>
                </div>
              </AppCard>
            );
          })}
        </div>
      )}
    </div>
  );
}
