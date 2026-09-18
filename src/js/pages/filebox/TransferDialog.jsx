import { LayerDialog } from '@cloudflare/kumo/components/layer-dialog';
import { Badge } from '@cloudflare/kumo/components/badge';
import { Select } from '@cloudflare/kumo/components/select';
import { formatFileSize } from '../../modules/utils.js';

export function TransferDialog({ transferModal, setTransferModal, storageNodes, onSubmit }) {
  return (
    <LayerDialog.Root
      open={transferModal.open}
      dismissDisabled={transferModal.transferring}
      onOpenChange={(open) => {
        if (transferModal.transferring) return;
        if (open) {
          setTransferModal((prev) => ({ ...prev, open }));
          return;
        }
        setTransferModal({ open: false, entry: null, targetNodeId: 'local', transferring: false });
      }}
    >
      <LayerDialog.Content size="sm">
        <LayerDialog.Title>转移文件存储位置</LayerDialog.Title>

        <LayerDialog.Body>
        <div className="space-y-4 text-xs">
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
        </LayerDialog.Body>

        <LayerDialog.Actions dismissLabel="取消">
          <LayerDialog.Actions.Primary
            type="button"
            loading={transferModal.transferring}
            onClick={onSubmit}
          >
            开始转移
          </LayerDialog.Actions.Primary>
        </LayerDialog.Actions>
      </LayerDialog.Content>
    </LayerDialog.Root>
  );
}
