import React from 'react';
import { Badge } from '@cloudflare/kumo/components/badge';
import { Button } from '@cloudflare/kumo/components/button';
import { Table } from '@cloudflare/kumo/components/table';
import { SkeletonLine } from '@cloudflare/kumo/components/loader';
import { AppCard, AppTable, ResponsiveSearchInput } from '../../components/ui/AppPrimitives.jsx';
import { AlertTriangle, ArrowLeft, Box, ChevronDown, ChevronRight, Database, Download, Eye, FileText, Folder, Plus, RefreshCw, Trash, Upload } from '../../components/Icons.jsx';
import { formatBytes, formatDate } from './utils.jsx';

const DNS_R2_COLUMNS = [
  { id: 'check', role: 'check' },
  { id: 'name', role: 'primary', grow: 1 },
  { id: 'size', role: 'count' },
  { id: 'modified', role: 'datetime' },
  { id: 'actions', role: 'actions-md' },
];

function R2Panel({
  loading,
  selectedAccountId,
  r2Buckets,
  r2BucketSearch,
  setR2BucketSearch,
  filteredR2Buckets,
  r2SelectedBucket,
  selectR2Bucket,
  r2Metrics,
  r2MetricsTotals,
  deleteR2Bucket,
  r2ColWidths,
  startR2Resize,
  r2Prefixes,
  r2Objects,
  r2ObjectTotalBytes,
  r2ObjectSearch,
  setR2ObjectSearch,
  r2Rows,
  filteredR2Rows,
  r2VisibleKeys,
  selectedR2Objects,
  setSelectedR2Objects,
  isArmed,
  r2CurrentPrefix,
  r2PathSegments,
  setR2BucketForm,
  setR2FolderForm,
  setModal,
  r2UploadInputRef,
  r2ExpandedPrefixes,
  handleR2UploadFiles,
  toggleR2FolderExpanded,
  previewR2Object,
  retryR2Dir,
  downloadR2Object,
  downloadR2Folder,
  toggleR2Selection,
  loadR2Objects,
  clearR2BucketCache,
  batchDeleteR2Objects,
  deleteR2Object,
}) {
  return (
    <div className="grid min-h-0 min-w-0 flex-1 grid-cols-1 gap-3 cq-lg:grid-cols-[18rem_minmax(0,1fr)]">
      <AppCard padding="sm" className="flex min-h-0 flex-col gap-3 cq-lg:h-full">
        <div className="flex items-center justify-between gap-2">
          <div className="min-w-0">
            <div className="text-sm font-semibold text-kumo-strong">存储桶</div>
            <div className="text-xs text-kumo-subtle">
              {r2Buckets.length} 个 Bucket
              {r2Metrics && (r2MetricsTotals.bytes > 0 || r2MetricsTotals.objects > 0) && (
                <span> · 已用 {formatBytes(r2MetricsTotals.bytes)} · {r2MetricsTotals.objects.toLocaleString('en-US', { useGrouping: false })} 个对象</span>
              )}
            </div>
          </div>
          <Button
            size="sm"
            shape="square"
            onClick={() => { setR2BucketForm({ name: '', location: 'auto' }); setModal({ type: 'r2Bucket', data: null }); }}
            disabled={!selectedAccountId}
            aria-label="创建存储桶"
            title="创建存储桶"
            icon={<Plus className="h-4 w-4" />}
          />
        </div>

        <ResponsiveSearchInput
          value={r2BucketSearch}
          onChange={(event) => setR2BucketSearch(event.target.value)}
          placeholder="搜索存储桶"
          ariaLabel="搜索 R2 存储桶"
        />

        <div className="flex min-h-0 flex-col gap-2 overflow-y-auto pr-1">
          {loading.r2 ? Array.from({ length: 5 }).map((_, index) => (
            <div key={index} className="rounded-md border border-kumo-line p-3">
              <SkeletonLine className="h-4 w-36" />
            </div>
          )) : !selectedAccountId ? (
            <div className="rounded-md border border-dashed border-kumo-line p-4 text-sm text-kumo-subtle">先选择 Cloudflare 账号。</div>
          ) : r2Buckets.length === 0 ? (
            <div className="rounded-md border border-dashed border-kumo-line p-4 text-sm text-kumo-subtle">还没有 R2 存储桶。</div>
          ) : filteredR2Buckets.length === 0 ? (
            <div className="rounded-md border border-dashed border-kumo-line p-4 text-sm text-kumo-subtle">没有匹配的存储桶。</div>
          ) : filteredR2Buckets.map((bucket) => {
            const isSelected = r2SelectedBucket?.name === bucket.name;
            return (
              <div
                key={bucket.name}
                className={`group rounded-md border p-2.5 ${isSelected ? 'border-brand/70 bg-brand/10 ring-1 ring-brand/20' : 'border-kumo-line bg-kumo-base hover:border-brand/50 hover:bg-kumo-recessed/40'}`}
              >
                <Button
                  type="button"
                  size="sm"
                  variant="ghost"
                  className="h-auto w-full min-w-0 items-start justify-start gap-2 px-0 py-0 text-left bg-transparent! hover:bg-transparent! active:bg-transparent! data-[active=true]:bg-transparent! data-[selected=true]:bg-transparent! focus-visible:bg-transparent!"
                  onClick={() => selectR2Bucket(bucket)}
                >
                  <Box className={`mt-0.5 h-4 w-4 shrink-0 ${isSelected ? 'text-brand' : 'text-kumo-subtle'}`} />
                  <span className="min-w-0 flex-1">
                    <span className={`block truncate text-sm font-medium ${isSelected ? 'text-brand' : 'text-kumo-strong'}`}>{bucket.name}</span>
                    <span className="mt-1 block truncate text-xs text-kumo-subtle" title={`创建于 ${formatDate(bucket.creation_date || bucket.created_at)}`}>
                      {formatDate(bucket.creation_date || bucket.created_at)}
                    </span>
                  </span>
                </Button>
                <div className="mt-2 flex items-center justify-between gap-2">
                  <Badge variant={bucket.public_url_base ? 'success' : 'outline'} className="text-[10px] leading-4">
                    {bucket.public_url_base ? '公开访问' : '私有'}
                  </Badge>
                  <Button size="sm" shape="square" variant={isArmed(`r2-bucket:${bucket.name}`) ? 'destructive' : 'secondary-destructive'} onClick={() => deleteR2Bucket(bucket)} aria-label={`删除 ${bucket.name}`} title="删除" icon={<Trash className="h-4 w-4" />} />
                </div>
              </div>
            );
          })}
        </div>
      </AppCard>

      <AppCard padding="none" className="flex min-h-0 min-w-0 flex-col overflow-hidden cq-lg:h-full">
        {!r2SelectedBucket ? (
          <div className="flex min-h-72 flex-1 flex-col items-center justify-center gap-3 p-8 text-center">
            <Database className="h-8 w-8 text-kumo-subtle" />
            <div>
              <div className="font-medium text-kumo-strong">选择一个存储桶</div>
              <div className="mt-1 text-sm text-kumo-subtle">左侧可搜索并进入 Bucket。</div>
            </div>
            <Button size="sm" disabled={!selectedAccountId} onClick={() => { setR2BucketForm({ name: '', location: 'auto' }); setModal({ type: 'r2Bucket', data: null }); }} icon={<Plus className="h-4 w-4" />}>
              创建存储桶
            </Button>
          </div>
        ) : (
          <div className="flex min-h-0 min-w-0 flex-1 flex-col">
            <input
              ref={r2UploadInputRef}
              type="file"
              multiple
              className="hidden"
              onChange={handleR2UploadFiles}
            />
            <div className="flex shrink-0 flex-col gap-3 border-b border-kumo-line p-3">
              <div className="flex flex-wrap items-center justify-between gap-2">
                <div className="min-w-0 flex-1">
                  <div className="flex min-w-0 flex-wrap items-center gap-1 rounded-md border border-kumo-line bg-kumo-recessed/25 px-2 py-1.5 text-sm">
                    <Database className="h-4 w-4 shrink-0 text-brand" />
                    <Button type="button" size="xs" variant="ghost" className="h-auto max-w-48 truncate px-1.5 py-0.5 font-semibold text-kumo-strong" onClick={() => loadR2Objects(r2SelectedBucket.name, '')}>
                      {r2SelectedBucket.name}
                    </Button>
                    <span className="text-kumo-subtle">/</span>
                    <Button type="button" size="xs" variant="ghost" className="h-auto px-1.5 py-0.5 text-kumo-subtle hover:text-kumo-strong" onClick={() => loadR2Objects(r2SelectedBucket.name, '')}>
                      根目录
                    </Button>
                    {r2PathSegments.map((segment, index) => {
                      const prefix = `${r2PathSegments.slice(0, index + 1).join('/')}/`;
                      return (
                        <React.Fragment key={prefix}>
                          <span className="text-kumo-subtle">/</span>
                          <Button type="button" size="xs" variant="ghost" className="h-auto max-w-40 truncate px-1.5 py-0.5 text-kumo-subtle hover:text-kumo-strong" onClick={() => loadR2Objects(r2SelectedBucket.name, prefix)}>
                            {segment}
                          </Button>
                        </React.Fragment>
                      );
                    })}
                  </div>
                </div>

                <div className="flex flex-wrap items-center gap-2">
                  <Button
                    size="sm"
                    variant="primary"
                    onClick={() => r2UploadInputRef.current?.click()}
                    disabled={loading.uploadR2}
                    icon={<Upload className="h-4 w-4" />}
                  >
                    上传
                  </Button>
                  <Button
                    size="sm"
                    variant="secondary"
                    onClick={() => {
                      setR2FolderForm({ name: '' });
                      setModal({ type: 'r2Folder', data: null });
                    }}
                    icon={<Folder className="h-4 w-4" />}
                  >
                    新建文件夹
                  </Button>
                  {r2CurrentPrefix && (
                    <Button
                      size="sm"
                      variant="secondary"
                      onClick={() => {
                        const parent = r2CurrentPrefix.split('/').filter(Boolean).slice(0, -1).join('/');
                        loadR2Objects(r2SelectedBucket.name, parent ? `${parent}/` : '');
                      }}
                      icon={<ArrowLeft className="h-4 w-4" />}
                    >
                      上一级
                    </Button>
                  )}
                  <Button size="sm" shape="square" variant="secondary" onClick={() => { clearR2BucketCache(r2SelectedBucket.name); loadR2Objects(r2SelectedBucket.name, r2CurrentPrefix, { force: true }); }} aria-label="刷新对象" title="刷新" icon={<RefreshCw className="h-4 w-4" />} />
                  {selectedR2Objects.length > 0 && (
                    <Button size="sm" variant={isArmed('batch-r2-objects') ? 'destructive' : 'secondary-destructive'} onClick={batchDeleteR2Objects} disabled={loading.batchDeleteR2} icon={<Trash className="h-4 w-4" />}>
                      删除 {selectedR2Objects.length}
                    </Button>
                  )}
                </div>
              </div>

              <div className="flex flex-col gap-2 cq-lg:flex-row cq-lg:items-center cq-lg:justify-between">
                <div className="flex flex-wrap items-center gap-2 text-xs text-kumo-subtle">
                  <Badge variant="outline">{r2Prefixes.length} 个文件夹</Badge>
                  <Badge variant="outline">{r2Objects.filter((object) => !String(object.key || object.name || '').endsWith('/.keep')).length} 个对象</Badge>
                  <Badge variant="outline">{formatBytes(r2ObjectTotalBytes)}</Badge>
                  {selectedR2Objects.length > 0 && <Badge variant="info">已选择 {selectedR2Objects.length}</Badge>}
                </div>
                <div className="w-full max-w-md">
                  <ResponsiveSearchInput
                    value={r2ObjectSearch}
                    onChange={(event) => setR2ObjectSearch(event.target.value)}
                    placeholder="搜索当前目录"
                    ariaLabel="搜索当前 R2 目录"
                  />
                </div>
              </div>
            </div>

            <div className="min-h-0 flex-1 overflow-auto">
                <AppTable tableId="dns-r2" columns={DNS_R2_COLUMNS} columnWidths={r2ColWidths}>
                  <Table.Header variant="compact">
                    <Table.Row>
                      <Table.CheckHead
                        checked={r2VisibleKeys.length > 0 && r2VisibleKeys.every((key) => selectedR2Objects.includes(key))}
                        indeterminate={selectedR2Objects.length > 0 && !r2VisibleKeys.every((key) => selectedR2Objects.includes(key))}
                        onCheckedChange={(checked) => setSelectedR2Objects(checked ? r2VisibleKeys : [])}
                        aria-label="全选当前可见 R2 对象"
                      />
                      <Table.Head className="relative pr-6">名称<Table.ResizeHandle onMouseDown={(e) => startR2Resize(1, e)} onTouchStart={(e) => startR2Resize(1, e)} /></Table.Head>
                      <Table.Head className="relative pr-6">大小<Table.ResizeHandle onMouseDown={(e) => startR2Resize(2, e)} onTouchStart={(e) => startR2Resize(2, e)} /></Table.Head>
                      <Table.Head className="relative pr-6">修改时间<Table.ResizeHandle onMouseDown={(e) => startR2Resize(3, e)} onTouchStart={(e) => startR2Resize(3, e)} /></Table.Head>
                      <Table.Head className="app-table-action">操作</Table.Head>
                    </Table.Row>
                  </Table.Header>
                  <Table.Body>
                    {loading.r2Objects ? (
                      Array.from({ length: 7 }).map((_, index) => <Table.Row key={index}><Table.Cell colSpan={5}><SkeletonLine className="h-4 w-full" /></Table.Cell></Table.Row>)
                    ) : r2Rows.length === 0 ? (
                      <Table.Row><Table.Cell colSpan={5} className="py-12 text-center text-kumo-subtle">当前目录为空。</Table.Cell></Table.Row>
                    ) : filteredR2Rows.length === 0 ? (
                      <Table.Row><Table.Cell colSpan={5} className="py-12 text-center text-kumo-subtle">没有匹配的对象。</Table.Cell></Table.Row>
                    ) : filteredR2Rows.map((row) => (
                      <Table.Row
                        key={row.key}
                        className={`cursor-pointer hover:bg-kumo-recessed/35 ${row.isLoading ? 'opacity-70' : ''}`}
                        onClick={() => { if (row.isLoading || row.isError) return; if (row.isFolder) toggleR2FolderExpanded(row.key); else previewR2Object(row.key); }}
                        title={row.isError ? '目录加载失败' : (row.isFolder ? '展开目录' : '预览文件')}
                      >
                        {(row.isLoading || row.isError) ? (
                          <Table.Cell />
                        ) : (
                          <Table.CheckCell
                            checked={selectedR2Objects.includes(row.key)}
                            onClick={(event) => event.stopPropagation()}
                            onCheckedChange={(checked) => toggleR2Selection(row.key, Boolean(checked))}
                            aria-label={`选择 ${row.key}`}
                          />
                        )}
                        <Table.Cell>
                          <div className="flex min-w-0 items-center gap-1" style={{ marginLeft: row.depth * 18 }}>
                            {row.isError ? (
                              <Button
                                type="button"
                                size="xs"
                                variant="ghost"
                                className="h-auto min-w-0 justify-start gap-2 px-0 py-0 text-left bg-transparent! hover:bg-transparent! active:bg-transparent! focus-visible:bg-transparent!"
                                onClick={(event) => { event.stopPropagation(); retryR2Dir(row.prefix); }}
                                aria-label={`重试加载目录 ${row.prefix}`}
                                title={`重试加载 ${row.prefix}`}
                              >
                                <AlertTriangle className="h-4 w-4 shrink-0 text-kumo-warning" />
                                <span className="truncate text-kumo-danger">加载失败，重试</span>
                              </Button>
                            ) : row.isLoading ? (
                              <span className="inline-flex items-center gap-2 px-1 text-xs text-kumo-subtle"><SkeletonLine className="h-3.5 w-3.5" />加载中…</span>
                            ) : (
                              <>
                            {row.isFolder && (
                              <Button
                                type="button"
                                size="xs"
                                shape="square"
                                variant="ghost"
                                className="h-6 w-6 shrink-0 bg-transparent! text-kumo-subtle hover:bg-transparent! active:bg-transparent! hover:text-brand! focus-visible:bg-transparent!"
                                onClick={(event) => { event.stopPropagation(); toggleR2FolderExpanded(row.key); }}
                                aria-label={r2ExpandedPrefixes.includes(row.key) ? `折叠目录 ${row.name}` : `展开目录 ${row.name}`}
                                title={r2ExpandedPrefixes.includes(row.key) ? '折叠目录' : '展开目录'}
                              >
                                {r2ExpandedPrefixes.includes(row.key) ? <ChevronDown className="h-3.5 w-3.5" /> : <ChevronRight className="h-3.5 w-3.5" />}
                              </Button>
                            )}
                            <Button
                              type="button"
                              size="xs"
                              variant="ghost"
                              className={`h-auto min-w-0 flex-1 justify-start gap-2 px-0 py-0 text-left bg-transparent! hover:bg-transparent! active:bg-transparent! focus-visible:bg-transparent! ${row.isFolder ? 'font-medium text-kumo-strong hover:text-brand!' : 'text-kumo-strong'}`}
                              onClick={(event) => {
                                event.stopPropagation();
                                if (row.isLoading) return;
                                if (row.isFolder) toggleR2FolderExpanded(row.key);
                                else previewR2Object(row.key);
                              }}
                              title={row.key}
                            >
                              {row.isLoading ? <SkeletonLine className="h-3.5 w-3.5" /> : row.isFolder ? <Folder className="h-4 w-4 shrink-0 text-brand" /> : <FileText className="h-4 w-4 shrink-0 text-kumo-subtle" />}
                              <span className="truncate">{row.isLoading ? '加载中…' : (row.name || row.key)}</span>
                            </Button>
                              </>
                            )}
                          </div>
                        </Table.Cell>
                        <Table.Cell>{row.isFolder ? '-' : formatBytes(row.size)}</Table.Cell>
                        <Table.Cell>{row.isFolder ? '-' : formatDate(row.uploaded || row.last_modified)}</Table.Cell>
                        <Table.Cell className="text-right">
                          {row.isFolder ? (
                            <div className="inline-flex gap-2" onClick={(event) => event.stopPropagation()}>
                              <Button size="sm" shape="square" variant="secondary" onClick={() => downloadR2Folder(row.key)} aria-label={`下载目录 ${row.key}`} title="下载目录" icon={<Download className="h-4 w-4" />} />
                              <Button size="sm" shape="square" variant={isArmed(`r2-object:${row.key}`) ? 'destructive' : 'secondary-destructive'} onClick={() => deleteR2Object(row.key)} aria-label={`删除目录 ${row.key}`} title="删除目录" icon={<Trash className="h-4 w-4" />} />
                            </div>
                          ) : (
                            <div className="inline-flex gap-2" onClick={(event) => event.stopPropagation()}>
                              <Button size="sm" shape="square" variant="secondary" onClick={() => previewR2Object(row.key)} aria-label={`预览 ${row.key}`} title="预览" icon={<Eye className="h-4 w-4" />} />
                              <Button size="sm" shape="square" variant="secondary" onClick={() => downloadR2Object(row.key)} aria-label={`下载 ${row.key}`} title="下载" icon={<Download className="h-4 w-4" />} />
                              <Button size="sm" shape="square" variant={isArmed(`r2-object:${row.key}`) ? 'destructive' : 'secondary-destructive'} onClick={() => deleteR2Object(row.key)} aria-label={`删除 ${row.key}`} title="删除" icon={<Trash className="h-4 w-4" />} />
                            </div>
                          )}
                        </Table.Cell>
                      </Table.Row>
                    ))}
                  </Table.Body>
                </AppTable>
            </div>
          </div>
        )}
      </AppCard>
    </div>
  );
}

export default R2Panel;
