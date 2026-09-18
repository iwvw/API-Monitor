import React from 'react';
import { Button } from '@cloudflare/kumo/components/button';
import { Dialog } from '@cloudflare/kumo/components/dialog';
import { Loader } from '@cloudflare/kumo';
import { CodeHighlighted } from '@cloudflare/kumo/code';
import { RefreshCw, X } from '../../components/Icons.jsx';

export default function KoyebUsageDialog({ koyebUsageTarget, setKoyebUsageTarget, openKoyebUsage, koyebUsageLoading, koyebUsageError, koyebUsageData }) {
  return (
      <Dialog.Root open={!!koyebUsageTarget} onOpenChange={(open) => { if (!open) setKoyebUsageTarget(null); }}>
        <Dialog className="flex h-[70vh] !w-[min(52rem,calc(100vw-2rem))] !max-w-[min(52rem,calc(100vw-2rem))] flex-col overflow-hidden p-0">
          <div className="flex items-center justify-between gap-2 border-b border-kumo-line p-4">
            <div>
              <Dialog.Title className="text-sm font-semibold text-kumo-strong">用量明细</Dialog.Title>
              {koyebUsageTarget && <p className="text-[10px] text-kumo-subtle mt-0.5">{koyebUsageTarget.name}</p>}
            </div>
            <div className="flex items-center gap-2">
              {koyebUsageTarget && (
                <Button size="sm" variant="secondary" onClick={() => openKoyebUsage(koyebUsageTarget)} icon={<RefreshCw className="h-3.5 w-3.5" />}>刷新</Button>
              )}
              <Button shape="square" size="sm" variant="ghost" aria-label="关闭" onClick={() => setKoyebUsageTarget(null)} icon={<X className="h-4 w-4" />} />
            </div>
          </div>
          <div className="flex-1 overflow-auto p-4">
            {koyebUsageLoading ? (
              <div className="flex h-full items-center justify-center gap-2 text-kumo-subtle"><Loader size={16} />正在加载用量...</div>
            ) : koyebUsageError ? (
              <div className="text-xs text-kumo-danger p-2 bg-kumo-danger/10 border border-kumo-danger/20 rounded">{koyebUsageError}</div>
            ) : !koyebUsageData ? (
              <div className="py-12 text-center text-kumo-subtle text-sm">暂无用量数据</div>
            ) : (
              <CodeHighlighted code={JSON.stringify(koyebUsageData, null, 2)} lang="json" showCopyButton />
            )}
          </div>
        </Dialog>
      </Dialog.Root>
  );
}
