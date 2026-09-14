import React from 'react';
import { Button } from '@cloudflare/kumo/components/button';
import { Dialog } from '@cloudflare/kumo/components/dialog';
import { Input } from '@cloudflare/kumo/components/input';
import CodeEditor from '../../components/ui/CodeEditor.jsx';
import { Upload } from '../../components/Icons.jsx';
import { SaSummary } from './utils.jsx';

export default function AccountDialog({
  open,
  onOpenChange,
  editingAccount,
  accountForm,
  setAccountForm,
  accountFileInputRef,
  onImportAccountJsonFile,
  onSubmit,
  submittingAccount,
}) {
  return (
    <Dialog.Root open={open} onOpenChange={onOpenChange}>
      <Dialog className="@container !w-[min(42rem,calc(100vw-2rem))] !max-w-[min(42rem,calc(100vw-2rem))] p-6">
        <Dialog.Title className="mb-1 text-base font-semibold text-kumo-strong">{editingAccount ? '编辑 GCP 账号' : '新增 GCP 账号'}</Dialog.Title>
        <Dialog.Description className="mb-4 text-xs text-kumo-subtle">使用 Service Account JSON 接入 GCP，支持粘贴或导入文件。</Dialog.Description>
      <div className="flex flex-col gap-3">
        <label className="flex flex-col gap-1">
          <span className="text-sm text-kumo-subtle">账号名称 *</span>
          <Input size="sm" value={accountForm.name} onChange={(event) => setAccountForm({ ...accountForm, name: event.target.value })} placeholder="例如：生产环境" />
        </label>
        {!editingAccount && (
          <label className="flex flex-col gap-1">
            <span className="text-sm text-kumo-subtle">Service Account JSON *</span>
            <input
              ref={accountFileInputRef}
              type="file"
              accept=".json,application/json"
              className="hidden"
              onChange={onImportAccountJsonFile}
            />
            <div className="flex justify-end">
              <Button type="button" size="sm" variant="secondary" onClick={() => accountFileInputRef.current?.click()}>
                <Upload className="h-4 w-4" />导入 JSON 文件
              </Button>
            </div>
            <CodeEditor
              label="Service Account JSON"
              fileName="service-account.json"
              language="json"
              minHeight="6rem"
              maxHeight="16rem"
              value={accountForm.serviceAccountJson}
              onChange={(serviceAccountJson) => setAccountForm({ ...accountForm, serviceAccountJson })}
              placeholder="粘贴完整的 Service Account JSON 密钥文件内容，或点击上方按钮导入文件"
            />
            <SaSummary json={accountForm.serviceAccountJson} onImport={() => accountFileInputRef.current?.click()} />
          </label>
        )}
        {editingAccount && (
          <label className="flex flex-col gap-1">
            <span className="text-sm text-kumo-subtle">Service Account JSON（留空不修改）</span>
            <CodeEditor
              label="Service Account JSON"
              fileName="service-account.json"
              language="json"
              minHeight="6rem"
              maxHeight="16rem"
              value={accountForm.serviceAccountJson}
              onChange={(serviceAccountJson) => setAccountForm({ ...accountForm, serviceAccountJson })}
              placeholder="不修改则留空"
            />
            <SaSummary json={accountForm.serviceAccountJson} />
          </label>
        )}
        <label className="flex flex-col gap-1">
          <span className="text-sm text-kumo-subtle">默认项目</span>
          <Input size="sm" value={accountForm.defaultProjectId} onChange={(event) => setAccountForm({ ...accountForm, defaultProjectId: event.target.value })} placeholder="留空则取 JSON 内 project_id" />
        </label>
        <label className="flex flex-col gap-1">
          <span className="text-sm text-kumo-subtle">备注</span>
          <Input size="sm" value={accountForm.description} onChange={(event) => setAccountForm({ ...accountForm, description: event.target.value })} />
        </label>
        <div className="flex justify-end gap-2 pt-2">
          <Button type="button" size="sm" variant="secondary" onClick={() => onOpenChange(false)}>取消</Button>
          <Button type="button" size="sm" variant="primary" loading={submittingAccount} onClick={onSubmit}>
            {editingAccount ? '保存' : '新增'}
          </Button>
        </div>
      </div>
      </Dialog>
    </Dialog.Root>
  );
}
