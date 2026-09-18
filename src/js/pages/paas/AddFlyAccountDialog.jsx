import React from 'react';
import { LayerDialog } from '@cloudflare/kumo/components/layer-dialog';
import { Input } from '@cloudflare/kumo/components/input';

export default function AddFlyAccountDialog({ showAddFlyModal, setShowAddFlyModal, newFlyName, setNewFlyName, newFlyToken, setNewFlyToken, flyAddAccountError, addFlyAccount, flyAddingAccount }) {
  return (
      <LayerDialog.Root open={showAddFlyModal} onOpenChange={setShowAddFlyModal}>
        <LayerDialog.Content size="sm">
          <LayerDialog.Title>添加 Fly.io 账号</LayerDialog.Title>
          <LayerDialog.Description>
            请输入 Fly.io API 令牌（以 flyv1_ 开头）。
          </LayerDialog.Description>
          <LayerDialog.Body>
            <div className="space-y-4">
              <div className="space-y-1">
                <label className="text-[11px] font-semibold text-kumo-subtle">备注名称</label>
                <Input size="sm"
                  aria-label="Fly.io 备注名称"
                  type="text"
                  value={newFlyName}
                  onChange={(e) => setNewFlyName(e.target.value)}
                  placeholder="我的 Fly.io 账号 1"
                  className="w-full text-kumo-strong p-2 text-xs"
                />
              </div>
              <div className="space-y-1">
                <label className="text-[11px] font-semibold text-kumo-subtle">API 令牌</label>
                <Input size="sm"
                  aria-label="Fly.io API 令牌"
                  type="text"
                  value={newFlyToken}
                  onChange={(e) => setNewFlyToken(e.target.value)}
                  placeholder="flyv1_xxxx"
                  autoComplete="off"
                  data-1p-ignore
                  data-lpignore="true"
                  data-bwignore="true"
                  data-form-type="other"
                  spellCheck={false}
                  className="w-full text-kumo-strong p-2 text-xs"
                />
              </div>
              {flyAddAccountError && (
                <div className="text-xs text-kumo-danger p-2 bg-kumo-danger/10 border border-kumo-danger/20 rounded">
                  {flyAddAccountError}
                </div>
              )}
            </div>
          </LayerDialog.Body>
          <LayerDialog.Actions dismissLabel="取消">
            <LayerDialog.Actions.Primary
              type="button"
              onClick={addFlyAccount}
              loading={flyAddingAccount}
            >
              确定添加
            </LayerDialog.Actions.Primary>
          </LayerDialog.Actions>
        </LayerDialog.Content>
      </LayerDialog.Root>
  );
}
