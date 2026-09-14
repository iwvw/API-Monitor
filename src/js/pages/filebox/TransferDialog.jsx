import { Dialog } from '@cloudflare/kumo/components/dialog';
import { Badge } from '@cloudflare/kumo/components/badge';
import { Button } from '@cloudflare/kumo/components/button';
import { Select } from '@cloudflare/kumo/components/select';
import { formatFileSize } from '../../modules/utils.js';

export function TransferDialog({ transferModal, setTransferModal, storageNodes, onSubmit }) {
  return (
    <Dialog.Root
      open={transferModal.open}
      onOpenChange={(open) => !transferModal.transferring && setTransferModal((prev) => ({ ...prev, open }))}
    >
      <Dialog size="sm" className="flex max-h-[calc(100dvh-1rem)] !w-[min(32rem,calc(100vw-2rem))] !max-w-[min(32rem,calc(100vw-2rem))] flex-col overflow-hidden p-0">
        <div className="flex items-center justify-between gap-3 border-b border-kumo-line px-4 py-3">
          <Dialog.Title className="text-sm font-semibold text-kumo-strong">
            转移文件存储位置
          </Dialog.Title>
          <Dialog.Close disabled={transferModal.transferring} />
        </div>

        <div className="space-y-4 p-4 text-xs">
          {transferModal.entry && (
            <div className="space-y-2 rounded-md border border-kumo-line bg-kumo-recessed/30 p-3">
              <div className="flex justify-between gap-2">
                <span className="text-kumo-subtle">文件名</span>
                <span className="font-semibold text-kumo-strong truncate max-w-[200px]">
                  {transferModal.entry.originalName || transferModal.entry.filename}
                </span>
              </div>
              <div className="flex justify-between gap-2">
                <span className="text-kumo-subtle">大小</span>
                <span className="font-mono text-kumo-strong">
                  {formatFileSize(transferModal.entry.size || 0)}
                </span>
              </div>
              <div className="flex justify-between gap-2">
                <span className="text-kumo-subtle">当前位置</span>
                <Badge variant={transferModal.entry.storageType === 'remote' ? 'brand' : 'secondary'} size="sm">
                  {transferModal.entry.storageType === 'remote'
                    ? (storageNodes.find((n) => n.id === transferModal.entry.serverId)?.name || transferModal.entry.serverId || '远程节点')
                    : '主站本地'}
                </Badge>
              </div>
            </div>
          )}

          <div className="space-y-1.5">
            <Select alignItemWithTrigger
              size="sm"
              label="目标存储位置"
              value={transferModal.targetNodeId}
              onValueChange={(val) => setTransferModal((prev) => ({ ...prev, targetNodeId: val }))}
              items={[
                { value: 'local', label: '主站本地存储' },
                ...storageNodes.map((n) => ({
                  value: n.id,
                  label: `${n.name || n.id} (${n.host}:${n.storagePort || 61208})`,
                })),
              ]}
            />
            <p className="text-[11px] text-kumo-subtle">
              主站将自动拉取数据并安全迁移到目标节点，完成完整性校验后清理旧存储。
            </p>
          </div>
        </div>

        <div className="flex justify-end gap-2 border-t border-kumo-line px-4 py-3">
          <Button
            size="sm"
            variant="secondary"
            disabled={transferModal.transferring}
            onClick={() => setTransferModal({ open: false, entry: null, targetNodeId: 'local', transferring: false })}
          >
            取消
          </Button>
          <Button
            size="sm"
            variant="primary"
            loading={transferModal.transferring}
            onClick={onSubmit}
          >
            开始转移
          </Button>
        </div>
      </Dialog>
    </Dialog.Root>
  );
}
