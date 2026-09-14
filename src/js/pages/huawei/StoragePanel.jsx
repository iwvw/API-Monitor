import React from 'react';
import { Button } from '@cloudflare/kumo/components/button';
import { Table } from '@cloudflare/kumo/components/table';
import {
  AppTable,
  DataTableFrame,
  EmptyState,
  SectionCard,
} from '../../components/ui/AppPrimitives.jsx';
import {
  ArrowLeft,
  HardDrive,
  Plus,
  Trash,
  Upload,
} from '../../components/Icons.jsx';
import SkeletonLines from './SkeletonLines.jsx';
import { BUCKET_TABLE_COLUMNS, OBJECT_TABLE_COLUMNS } from './constants.js';
import { formatBytes } from './utils.js';

export default function StoragePanel({
  loadingScope,
  selectedBucket,
  objects,
  buckets,
  selectedAccountId,
  uploadingObject,
  objectFileRef,
  handleUploadFile,
  backToBuckets,
  openBucket,
  deleteObjectAction,
  deleteBucketAction,
  setBucketDialogOpen,
}) {
  if (selectedBucket) {
    return (
      <SectionCard
        title={`桶：${selectedBucket.name}`}
        bodyPadding="none"
        actions={(
          <>
            <Button type="button" size="sm" variant="secondary" onClick={backToBuckets}><ArrowLeft className="h-4 w-4" />返回桶列表</Button>
            <Button type="button" size="sm" onClick={() => objectFileRef.current?.click()} disabled={uploadingObject}><Upload className="h-4 w-4" />{uploadingObject ? '上传中…' : '上传'}</Button>
          </>
        )}
      >
        <input ref={objectFileRef} type="file" className="hidden" onChange={handleUploadFile} />
        {loadingScope ? (
          <SkeletonLines />
        ) : objects.length === 0 ? (
          <EmptyState card={false} icon={HardDrive} title="桶为空" description="点击「上传」添加对象，或返回桶列表" className="min-h-64" />
        ) : (
          <DataTableFrame variant="embedded" density="dense" className="overflow-auto">
            <AppTable tableId="huawei-objects" columns={OBJECT_TABLE_COLUMNS}>
              <Table.Header variant="compact">
                <Table.Row>
                  <Table.Head>对象</Table.Head>
                  <Table.Head>大小</Table.Head>
                  <Table.Head>最后修改</Table.Head>
                  <Table.Head className="app-table-action">操作</Table.Head>
                </Table.Row>
              </Table.Header>
              <Table.Body>
                {objects.map((object) => (
                  <Table.Row key={object.name}>
                    <Table.Cell><span className="block max-w-full truncate font-mono text-xs" title={object.name}>{object.name}</span></Table.Cell>
                    <Table.Cell>{formatBytes(object.size)}</Table.Cell>
                    <Table.Cell className="whitespace-nowrap text-xs text-kumo-subtle">{object.lastModified || '-'}</Table.Cell>
                    <Table.Cell>
                      <div className="flex items-center justify-end gap-1">
                        <Button type="button" size="sm" shape="square" variant="destructive" title="删除" aria-label="删除" onClick={() => deleteObjectAction(object)}><Trash className="h-4 w-4" /></Button>
                      </div>
                    </Table.Cell>
                  </Table.Row>
                ))}
              </Table.Body>
            </AppTable>
          </DataTableFrame>
        )}
      </SectionCard>
    );
  }

  return (
    <SectionCard
      title="OBS 桶"
      bodyPadding="none"
      actions={(
        <Button type="button" size="sm" variant="primary" onClick={() => setBucketDialogOpen(true)}><Plus className="h-4 w-4" />新建桶</Button>
      )}
    >
      {loadingScope ? (
        <SkeletonLines />
      ) : !selectedAccountId ? (
        <EmptyState card={false} icon={HardDrive} title="请选择账号" description="选择华为云账号后展示对象存储" className="min-h-64" />
      ) : buckets.length === 0 ? (
        <EmptyState card={false} icon={HardDrive} title="暂无桶" description="点击「新建桶」创建对象存储桶" className="min-h-64" />
      ) : (
        <DataTableFrame variant="embedded" density="dense" className="overflow-auto">
          <AppTable tableId="huawei-buckets" columns={BUCKET_TABLE_COLUMNS}>
            <Table.Header variant="compact">
              <Table.Row>
                <Table.Head>桶名称</Table.Head>
                <Table.Head>区域</Table.Head>
                <Table.Head>创建时间</Table.Head>
                <Table.Head className="app-table-action">操作</Table.Head>
              </Table.Row>
            </Table.Header>
            <Table.Body>
              {buckets.map((bucket) => (
                <Table.Row key={bucket.name}>
                  <Table.Cell>
                    <Button type="button" variant="ghost" size="xs" className="block h-auto max-w-full truncate px-1.5 py-0.5 font-semibold text-kumo-strong" title={`${bucket.name} · 点击进入`} onClick={() => openBucket(bucket)}>{bucket.name}</Button>
                  </Table.Cell>
                  <Table.Cell className="truncate text-sm text-kumo-strong">{bucket.region || '-'}</Table.Cell>
                  <Table.Cell className="whitespace-nowrap text-xs text-kumo-subtle">{bucket.createdAt || '-'}</Table.Cell>
                  <Table.Cell>
                    <div className="flex items-center justify-end gap-1">
                      <Button type="button" size="sm" shape="square" variant="destructive" title="删除" aria-label="删除" onClick={() => deleteBucketAction(bucket)}><Trash className="h-4 w-4" /></Button>
                    </div>
                  </Table.Cell>
                </Table.Row>
              ))}
            </Table.Body>
          </AppTable>
        </DataTableFrame>
      )}
    </SectionCard>
  );
}
