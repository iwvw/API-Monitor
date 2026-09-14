import { Badge } from '@cloudflare/kumo/components/badge';
import { Button } from '@cloudflare/kumo/components/button';
import { Table } from '@cloudflare/kumo/components/table';
import { SkeletonLine } from '@cloudflare/kumo/components/loader';
import { formatDateTime } from '../../modules/utils.js';
import { Clock, History, RefreshCw, Server, Trash } from '../../components/Icons.jsx';
import { SectionCard } from '../../components/ui/AppPrimitives.jsx';
import { EntryName } from './EntryName.jsx';
import { formatExpiry } from './utils.js';

export function HistoryPanel({
  isArmed,
  historyLoading,
  serverHistory,
  storageNodes,
  localHistory,
  accessLogs,
  loadServerHistory,
  runCleanup,
  copyLink,
  openTransferModal,
  deleteEntry,
}) {
  return (
    <div className="grid gap-4">
      <SectionCard
        title="分享记录"
        icon={<History className="h-4 w-4 text-brand" />}
        actions={
          <>
            <Button size="sm" variant="secondary" onClick={loadServerHistory} loading={historyLoading} icon={<RefreshCw className="h-4 w-4" />}>
              刷新
            </Button>
            <Button size="sm" variant={isArmed('clear-expired-shares') ? 'destructive' : 'secondary-destructive'} onClick={runCleanup} icon={<Clock className="h-4 w-4" />}>
              清理过期
            </Button>
          </>
        }
        bodyPadding="none"
        bodyClassName="overflow-x-auto"
      >
        <Table layout="fixed" className="min-w-[880px]">
          <colgroup>
            <col />
            <col className="w-24" />
            <col className="w-36" />
            <col className="w-28" />
            <col className="w-32" />
            <col className="w-36" />
          </colgroup>
          <Table.Header>
            <Table.Row>
              <Table.Head>内容</Table.Head>
              <Table.Head>分享码</Table.Head>
              <Table.Head>存储位置</Table.Head>
              <Table.Head>下载次数</Table.Head>
              <Table.Head>到期</Table.Head>
              <Table.Head className="app-table-action">操作</Table.Head>
            </Table.Row>
          </Table.Header>
          <Table.Body>
            {historyLoading ? (
              Array.from({ length: 3 }).map((_, index) => (
                <Table.Row key={index}>
                  <Table.Cell colSpan={6}>
                    <SkeletonLine className="h-8 w-full" />
                  </Table.Cell>
                </Table.Row>
              ))
            ) : serverHistory.length === 0 ? (
              <Table.Row>
                <Table.Cell colSpan={6} className="p-8 text-center text-kumo-subtle">
                  暂无有效分享
                </Table.Cell>
              </Table.Row>
            ) : (
              serverHistory.map((entry) => {
                const isRemote = entry.storageType === 'remote';
                const nodeInfo = isRemote ? storageNodes.find((n) => n.id === entry.serverId) : null;
                const nodeLabel = isRemote
                  ? (nodeInfo ? `${nodeInfo.name || nodeInfo.id} (${nodeInfo.host})` : (entry.serverId || '远程节点'))
                  : '主站本地';

                return (
                  <Table.Row key={entry.code}>
                    <Table.Cell>
                      <EntryName entry={entry} />
                    </Table.Cell>
                    <Table.Cell className="font-mono text-xs font-semibold text-brand">{entry.code}</Table.Cell>
                    <Table.Cell>
                      <Badge variant={isRemote ? 'brand' : 'secondary'} size="sm" className="inline-flex items-center gap-1 font-mono text-[11px]">
                        <Server className="h-3 w-3 shrink-0" />
                        <span className="truncate max-w-[120px]">{nodeLabel}</span>
                      </Badge>
                    </Table.Cell>
                    <Table.Cell className="text-xs text-kumo-subtle">
                      {entry.downloads || 0}
                      {entry.maxDownloads ? ` / ${entry.maxDownloads}` : ' / 不限'}
                    </Table.Cell>
                    <Table.Cell className="text-xs text-kumo-subtle">{formatExpiry(entry.expiry)}</Table.Cell>
                    <Table.Cell>
                      <div className="flex gap-1">
                        <Button size="sm" variant="secondary" onClick={() => copyLink(entry.code)}>
                          复制
                        </Button>
                        {entry.type === 'file' && (
                          <Button size="sm" variant="secondary" onClick={() => openTransferModal(entry)} title="转移存储位置">
                            转移
                          </Button>
                        )}
                        <Button size="sm" variant={isArmed(`share:${entry.code}`) ? 'destructive' : 'secondary-destructive'} onClick={() => deleteEntry(entry.code)}>
                          <Trash className="h-3.5 w-3.5" />
                        </Button>
                      </div>
                    </Table.Cell>
                  </Table.Row>
                );
              })
            )}
          </Table.Body>
        </Table>
      </SectionCard>

      <div className="grid items-start gap-4 cq-xl:grid-cols-2">
        <SectionCard title="本地最近创建" icon={<History className="h-4 w-4 text-brand" />} bodyPadding="none">
          <div className="divide-y divide-kumo-line">
            {localHistory.length === 0 ? (
              <div className="py-8 text-center text-xs text-kumo-subtle">暂无本地记录</div>
            ) : (
              localHistory.slice(0, 8).map((entry) => (
                <div key={entry.code} className="flex items-center justify-between gap-3 px-4 py-2.5">
                  <EntryName entry={entry} />
                  <div className="flex shrink-0 items-center gap-2">
                    <span className="font-mono text-xs text-brand">{entry.code}</span>
                    <Button size="sm" variant="secondary" onClick={() => copyLink(entry.code)}>
                      复制
                    </Button>
                  </div>
                </div>
              ))
            )}
          </div>
        </SectionCard>

        <SectionCard title="访问日志" icon={<Clock className="h-4 w-4 text-brand" />} bodyPadding="none">
          <div className="max-h-72 overflow-auto divide-y divide-kumo-line">
            {accessLogs.length === 0 ? (
              <div className="py-8 text-center text-xs text-kumo-subtle">暂无访问日志</div>
            ) : (
              accessLogs.slice(0, 20).map((log) => (
                <div key={log.id} className="grid grid-cols-[5rem_5rem_minmax(0,1fr)_9rem] gap-2 px-4 py-2 text-xs">
                  <span className="font-mono text-brand">{log.code}</span>
                  <span className="text-kumo-strong">{log.action}</span>
                  <span className="truncate text-kumo-subtle">{log.ipAddress || log.userAgent || '-'}</span>
                  <span className="text-right text-kumo-subtle">{formatDateTime(log.createdAt)}</span>
                </div>
              ))
            )}
          </div>
        </SectionCard>
      </div>
    </div>
  );
}
