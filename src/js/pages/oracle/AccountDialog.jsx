import React from 'react';
import { Button } from '@cloudflare/kumo/components/button';
import { Dialog } from '@cloudflare/kumo/components/dialog';
import { Input } from '@cloudflare/kumo/components/input';
import CodeEditor from '../../components/ui/CodeEditor.jsx';
import { Info, Upload } from '../../components/Icons.jsx';

export default function AccountDialog({ open, onOpenChange, editingAccount, accountForm, setAccountForm, accountConfigText, updateAccountConfigText, privateKeyFileRef, uploadPrivateKey, saveAccount, submittingAccount }) {
  return (
    <Dialog.Root open={open} onOpenChange={onOpenChange}>
      <Dialog className="@container !w-[min(48rem,calc(100vw-2rem))] !max-w-[min(48rem,calc(100vw-2rem))] p-6">
        <Dialog.Title className="mb-1 text-base font-semibold text-kumo-strong">
          {editingAccount ? '编辑 Oracle 账号' : '添加 Oracle 账号'}
        </Dialog.Title>
        <Dialog.Description className="mb-4 text-xs text-kumo-subtle">
          从 OCI 控制台复制 API Key 配置，并粘贴 PEM 私钥全文。
        </Dialog.Description>
        <div className="mb-4 space-y-2">
          <CodeEditor
            label="OCI 配置文件"
            language="ini"
            value={accountConfigText}
            onChange={updateAccountConfigText}
            minHeight="10rem"
            placeholder={`[DEFAULT]
user=ocid1.user...
fingerprint=fa:d1:...
tenancy=ocid1.tenancy...
region=us-sanjose-1
key_file=<path to your private keyfile>`}
          />
          <div className="flex items-start gap-1.5 text-xs leading-5 text-kumo-subtle">
            <Info className="mt-0.5 h-3.5 w-3.5 shrink-0" />
            <span>粘贴后自动填充 User OCID、Fingerprint、Tenancy OCID 和 Region；key_file 不使用，私钥请在下方粘贴或上传。</span>
          </div>
        </div>
        <div className="grid gap-3 cq-md:grid-cols-2">
          <Input
            label="账号名称 *"
            value={accountForm.name}
            onChange={(event) => setAccountForm({ ...accountForm, name: event.target.value })}
            placeholder="如：东京主账号"
          />
          <Input
            label="Region *"
            value={accountForm.region}
            onChange={(event) => setAccountForm({ ...accountForm, region: event.target.value })}
            placeholder="如：ap-tokyo-1"
          />
          <Input
            label={editingAccount ? 'Tenancy OCID' : 'Tenancy OCID *'}
            value={accountForm.tenancyOcid}
            onChange={(event) => setAccountForm({ ...accountForm, tenancyOcid: event.target.value })}
            placeholder={editingAccount ? '留空不修改' : 'ocid1.tenancy...'}
          />
          <Input
            label={editingAccount ? 'User OCID' : 'User OCID *'}
            value={accountForm.userOcid}
            onChange={(event) => setAccountForm({ ...accountForm, userOcid: event.target.value })}
            placeholder={editingAccount ? '留空不修改' : 'ocid1.user...'}
          />
          <Input
            label="Fingerprint *"
            value={accountForm.fingerprint}
            onChange={(event) => setAccountForm({ ...accountForm, fingerprint: event.target.value })}
            placeholder="fingerprint"
          />
          <Input
            label="默认 Compartment"
            value={accountForm.defaultCompartmentId}
            onChange={(event) => setAccountForm({ ...accountForm, defaultCompartmentId: event.target.value })}
            placeholder="可选；留空用根租户"
          />
          <Input
            label="私钥 Passphrase"
            value={accountForm.passphrase}
            onChange={(event) => setAccountForm({ ...accountForm, passphrase: event.target.value })}
            placeholder="可选"
            type="text"
            autoComplete="off"
            data-1p-ignore
            data-lpignore="true"
            data-bwignore="true"
            data-form-type="other"
            spellCheck={false}
          />
          <Input
            label="备注"
            value={accountForm.description}
            onChange={(event) => setAccountForm({ ...accountForm, description: event.target.value })}
            placeholder="可选"
          />
          <div className="cq-md:col-span-2">
            <input
              ref={privateKeyFileRef}
              type="file"
              accept=".pem,.key,.txt"
              className="hidden"
              onChange={uploadPrivateKey}
            />
            <div className="mb-1.5 flex items-center justify-between gap-3">
              <div className="text-sm font-medium text-kumo-strong">
                {editingAccount ? 'Oracle API 私钥 PEM' : 'Oracle API 私钥 PEM *'}
              </div>
              <Button
                type="button"
                size="sm"
                variant="secondary"
                onClick={() => privateKeyFileRef.current?.click()}
                icon={<Upload className="h-3.5 w-3.5" />}
              >
                上传 PEM
              </Button>
            </div>
            <CodeEditor
              label="Oracle API 私钥 PEM"
              language="text"
              value={accountForm.privateKeyPem}
              onChange={(privateKeyPem) => setAccountForm({ ...accountForm, privateKeyPem })}
              minHeight="10rem"
              placeholder={editingAccount ? '留空不修改私钥' : '-----BEGIN PRIVATE KEY-----'}
            />
          </div>
        </div>
        <div className="mt-5 flex justify-end gap-2">
          <Button type="button" variant="secondary" onClick={() => onOpenChange(false)}>取消</Button>
          <Button type="button" onClick={saveAccount} disabled={submittingAccount}>{submittingAccount ? '保存中...' : '保存'}</Button>
        </div>
      </Dialog>
    </Dialog.Root>
  );
}
