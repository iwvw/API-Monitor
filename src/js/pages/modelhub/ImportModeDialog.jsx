import { Button } from '@cloudflare/kumo/components/button';
import { LayerDialog } from '@cloudflare/kumo/components/layer-dialog';

export function ImportModeDialog({ endpointsApi }) {
  const { importModeDialog, setImportModeDialog, runEndpointImport } = endpointsApi;
  return (
      <LayerDialog.Root open={!!importModeDialog} onOpenChange={open => !open && setImportModeDialog(null)}>
        <LayerDialog.Content size="base">
          <LayerDialog.Title>导入端点</LayerDialog.Title>
          <LayerDialog.Description>
            共 {importModeDialog?.count ?? 0} 个端点，选择导入方式（文件包含完整配置：密钥、模型、映射、请求头、代理池与订阅、优先级权重）
          </LayerDialog.Description>
          <LayerDialog.Body>
            <div className="flex flex-col gap-2">
              <Button size="sm" variant="secondary" className="w-full justify-start" onClick={() => importModeDialog && runEndpointImport(importModeDialog.list, false)}>
                跳过已有（仅新增）
              </Button>
              <Button size="sm" variant="primary" className="w-full justify-start" onClick={() => importModeDialog && runEndpointImport(importModeDialog.list, true)}>
                覆盖导入（替换全部）
              </Button>
            </div>
          </LayerDialog.Body>
        </LayerDialog.Content>
      </LayerDialog.Root>
  );
}
