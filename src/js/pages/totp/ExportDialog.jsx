import React from 'react';
import { Button } from '@cloudflare/kumo/components/button';
import { Dialog } from '@cloudflare/kumo/components/dialog';
import CodeEditor from '../../components/ui/CodeEditor.jsx';

const ExportDialog = ({
  showExportModal,
  setShowExportModal,
  exportUris,
  exportMeta,
  copyExportedUris,
}) => {
  return (
    <Dialog.Root open={showExportModal} onOpenChange={setShowExportModal}>
      <Dialog className="!w-[min(40rem,calc(100vw-2rem))] !max-w-[min(40rem,calc(100vw-2rem))] p-6">
        <Dialog.Title className="text-base font-semibold text-kumo-strong mb-1">
          备份与导出
        </Dialog.Title>
        <Dialog.Description className="text-xs text-kumo-subtle mb-4">
          已生成默认加密备份，可用于迁移或恢复 2FA 账号。
        </Dialog.Description>

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

        <div className="flex justify-end gap-3 mt-6">
          <Dialog.Close
            render={props => (
              <Button size="sm" {...props} variant="secondary">
                关闭
              </Button>
            )}
          />
          <Button size="sm" variant="primary" onClick={copyExportedUris}>
            复制到剪贴板
          </Button>
        </div>
      </Dialog>
    </Dialog.Root>
  );
};

export default ExportDialog;
