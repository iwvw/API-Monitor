import React from 'react';
import { Button } from '@cloudflare/kumo/components/button';
import { Checkbox } from '@cloudflare/kumo/components/checkbox';
import { Table } from '@cloudflare/kumo/components/table';
import { SkeletonLine } from '@cloudflare/kumo/components/loader';
import { AppTable, DataTableFrame, EmptyState, SectionCard } from '../../components/ui/AppPrimitives.jsx';
import { Cloud, Copy, Download, FolderOpen, Layers, Plus, Trash, Upload } from '../../components/Icons.jsx';
import { BUCKET_TABLE_COLUMNS, OBJECT_TABLE_COLUMNS } from './constants.js';
import { formatDate, formatSize } from './utils.jsx';

export default function StoragePanel({
  scopeReady,
  loadingBuckets,
  loadingObjects,
  buckets,
  objects,
  selectedBucket,
  selectedObjects,
  uploadingObject,
  objectFileInputRef,
  onOpenBucketDialog,
  onSelectBucket,
  onDeleteBucket,
  onUploadObject,
  onDeleteSelectedObjects,
  onToggleAllObjects,
  onToggleObjectSelection,
  onOpenDetail,
  onDownloadObject,
  onDeleteObject,
  onCopy,
}) {
  return (
    <div className="flex flex-col gap-3">
      <SectionCard
        title="存储桶"
        icon={<Layers className="h-4 w-4" />}
        actions={scopeReady && (
          <Button type="button" size="sm" variant="primary" onClick={onOpenBucketDialog}>
            <Plus className="h-4 w-4" />创建桶
          </Button>
        )}
        bodyPadding="none"
      >
        {!scopeReady ? (
          <EmptyState card={false} icon={Cloud} title="请选择账号与项目" className="min-h-48" />
        ) : loadingBuckets ? (
          <div className="p-4"><SkeletonLine className="h-5 w-full" /></div>
        ) : buckets.length === 0 ? (
          <EmptyState card={false} icon={Layers} title="暂无存储桶" className="min-h-48" />
        ) : (
          <DataTableFrame variant="embedded" density="dense" className="overflow-auto">
            <AppTable tableId="gcp-buckets" columns={BUCKET_TABLE_COLUMNS}>
              <Table.Header variant="compact">
                <Table.Row>
                  <Table.Head>名称</Table.Head>
                  <Table.Head>位置</Table.Head>
                  <Table.Head>存储类别</Table.Head>
                  <Table.Head>创建时间</Table.Head>
                </Table.Row>
              </Table.Header>
              <Table.Body>
                {buckets.map((bucket) => (
                  <Table.Row
                    key={bucket.name}
                    variant={selectedBucket === bucket.name ? 'selected' : 'default'}
                    className="cursor-pointer"
                    onClick={() => onSelectBucket(bucket.name)}
                  >
                    <Table.Cell>
                      <div className="flex items-center justify-between gap-2">
                        <div className="truncate text-sm font-semibold text-kumo-strong">{bucket.name}</div>
                        <Button type="button" size="sm" variant="danger" className="h-6 w-6 p-0" onClick={(event) => { event.stopPropagation(); onDeleteBucket(bucket); }} aria-label="删除桶">
                          <Trash className="h-3.5 w-3.5" />
                        </Button>
                      </div>
                    </Table.Cell>
                    <Table.Cell className="text-sm text-kumo-strong">{bucket.location || '-'}</Table.Cell>
                    <Table.Cell className="text-sm text-kumo-strong">{bucket.storageClass || '-'}</Table.Cell>
                    <Table.Cell className="text-xs text-kumo-subtle">{formatDate(bucket.timeCreated)}</Table.Cell>
                  </Table.Row>
                ))}
              </Table.Body>
            </AppTable>
          </DataTableFrame>
        )}
      </SectionCard>

      {selectedBucket && (
        <SectionCard
          title={`对象 · ${selectedBucket}`}
          icon={<FolderOpen className="h-4 w-4" />}
          bodyPadding="none"
          actions={(
            <>
              <input
                ref={objectFileInputRef}
                type="file"
                className="hidden"
                onChange={(event) => {
                  const file = event.target.files?.[0];
                  if (file) onUploadObject(file);
                }}
              />
              {selectedObjects.size > 0 && (
                <Button type="button" size="sm" variant="danger" onClick={onDeleteSelectedObjects}>
                  <Trash className="h-4 w-4" />删除选中（{selectedObjects.size}）
                </Button>
              )}
              <Button type="button" size="sm" variant="primary" loading={uploadingObject} onClick={() => objectFileInputRef.current?.click()}>
                <Upload className="h-4 w-4" />上传对象
              </Button>
            </>
          )}
        >
          {loadingObjects ? (
            <div className="p-4"><SkeletonLine className="h-5 w-full" /></div>
          ) : objects.length === 0 ? (
            <EmptyState card={false} icon={FolderOpen} title="桶内暂无对象" className="min-h-40" />
          ) : (
            <DataTableFrame variant="embedded" density="dense" className="overflow-auto">
              <AppTable tableId="gcp-objects" columns={OBJECT_TABLE_COLUMNS}>
                <Table.Header variant="compact">
                  <Table.Row>
                    <Table.Head>
                      <Checkbox checked={objects.length > 0 && selectedObjects.size === objects.length} onCheckedChange={onToggleAllObjects} aria-label="全选" />
                    </Table.Head>
                    <Table.Head>名称</Table.Head>
                    <Table.Head>大小</Table.Head>
                    <Table.Head>类型</Table.Head>
                    <Table.Head>更新时间</Table.Head>
                    <Table.Head>操作</Table.Head>
                  </Table.Row>
                </Table.Header>
                <Table.Body>
                  {objects.map((object) => (
                    <Table.Row key={object.name} className={selectedObjects.has(object.name) ? 'bg-kumo-interact/10' : undefined}>
                      <Table.Cell>
                        <Checkbox checked={selectedObjects.has(object.name)} onCheckedChange={() => onToggleObjectSelection(object.name)} aria-label={`选择 ${object.name}`} />
                      </Table.Cell>
                      <Table.Cell><Button type="button" variant="ghost" size="xs" className="block h-auto max-w-full truncate px-1.5 py-0.5 font-semibold text-kumo-strong" onClick={() => onOpenDetail('object', object)} title={`${object.name} · 点击查看详情`}>{object.name}</Button></Table.Cell>
                      <Table.Cell>{formatSize(object.size)}</Table.Cell>
                      <Table.Cell className="truncate text-sm text-kumo-strong">{object.contentType || '-'}</Table.Cell>
                      <Table.Cell className="text-xs text-kumo-subtle">{formatDate(object.updated || object.timeCreated)}</Table.Cell>
                      <Table.Cell>
                        <div className="flex items-center justify-end gap-1">
                          <Button type="button" size="sm" variant="ghost" className="h-6 w-6 p-0" title="下载" onClick={() => onDownloadObject(object)} aria-label="下载对象">
                            <Download className="h-3.5 w-3.5" />
                          </Button>
                          <Button type="button" size="sm" variant="ghost" className="h-6 w-6 p-0" title="复制名称" onClick={() => onCopy(object.name)} aria-label="复制名称">
                            <Copy className="h-3.5 w-3.5" />
                          </Button>
                          <Button type="button" size="sm" variant="danger" className="h-6 w-6 p-0" title="删除" onClick={() => onDeleteObject(object)} aria-label="删除对象">
                            <Trash className="h-3.5 w-3.5" />
                          </Button>
                        </div>
                      </Table.Cell>
                    </Table.Row>
                  ))}
                </Table.Body>
              </AppTable>
            </DataTableFrame>
          )}
        </SectionCard>
      )}
    </div>
  );
}
