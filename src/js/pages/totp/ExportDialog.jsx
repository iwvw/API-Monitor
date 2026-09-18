import React from 'react';
import { LayerDialog } from '@cloudflare/kumo/components/layer-dialog';
import CodeEditor from '../../components/ui/CodeEditor.jsx';

const ExportDialog = ({
  showExportModal,
  setShowExportModal,
  exportUris,
  exportMeta,
  copyExportedUris,
}) => {
  return (
    <LayerDialog.Root open={showExportModal} onOpenChange={setShowExportModal}>
      <LayerDialog.Content size="base">
        <LayerDialog.Title>
          备份与导出
        </LayerDialog.Title>
        <LayerDialog.Description>
          已生成默认加密备份，可用于迁移或恢复 2FA 账号。
        </LayerDialog.Description>
        <LayerDialog.Body>
          <div className="space-y-1.5">
            <CodeEditor
              label="导出的加密 2FA 备份"
              language="text"
              readOnly
              value={exportUris}
              minHeight="12rem"
            />
            <span className="text-[10px] text-kumo-subtle block">
              {exportMeta?.accountCount !== undefined
                ? `已加密 ${exportMeta.accountCount} 个账号和 ${exportMeta.groupCount || 0} 个分组。`
                : '当前内容可能是显式请求的明文 URI，请谨慎保管。'}
            </span>
          </div>
        </LayerDialog.Body>
        <LayerDialog.Actions dismissLabel="关闭">
          <LayerDialog.Actions.Primary type="button" onClick={copyExportedUris}>
            复制到剪贴板
          </LayerDialog.Actions.Primary>
        </LayerDialog.Actions>
      </LayerDialog.Content>
    </LayerDialog.Root>
  );
};

export default ExportDialog;
