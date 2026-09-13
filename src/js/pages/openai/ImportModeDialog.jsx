import { Button } from '@cloudflare/kumo/components/button';
import { Dialog } from '@cloudflare/kumo/components/dialog';

export function ImportModeDialog({ endpointsApi }) {
  const { importModeDialog, setImportModeDialog, runEndpointImport } = endpointsApi;
  return (
      <Dialog.Root open={!!importModeDialog} onOpenChange={open => !open && setImportModeDialog(null)}>
        <Dialog className="!w-[min(30rem,calc(100vw-2rem))]">
          <div className="grid gap-1 px-6 pt-5 pb-4">
            <Dialog.Title className="text-sm font-semibold text-kumo-strong">导入端点</Dialog.Title>
            <Dialog.Description className="text-xs text-kumo-subtle">
              共 {importModeDialog?.count ?? 0} 个端点，选择导入方式（文件包含完整配置：密钥、模型、映射、请求头、代理池与订阅、优先级权重）
            </Dialog.Description>
          </div>
          <div className="flex items-center justify-end gap-2 border-t border-kumo-line px-6 py-4">
            <Button size="sm" variant="secondary" onClick={() => setImportModeDialog(null)}>取消</Button>
            <Button size="sm" variant="secondary" onClick={() => importModeDialog && runEndpointImport(importModeDialog.list, false)}>
              跳过已有（仅新增）
            </Button>
            <Button size="sm" variant="primary" onClick={() => importModeDialog && runEndpointImport(importModeDialog.list, true)}>
              覆盖导入（替换全部）
            </Button>
          </div>
        </Dialog>
      </Dialog.Root>
  );
}
