import React from 'react';
import { LayerDialog } from '@cloudflare/kumo/components/layer-dialog';
import { Input } from '@cloudflare/kumo/components/input';

export default function AddKoyebAccountDialog({ showAddKoyebModal, setShowAddKoyebModal, newKoyebName, setNewKoyebName, newKoyebToken, setNewKoyebToken, koyebAddAccountError, addKoyebAccount, koyebAddingAccount }) {
  return (
      <LayerDialog.Root open={showAddKoyebModal} onOpenChange={setShowAddKoyebModal}>
        <LayerDialog.Content size="sm">
          <LayerDialog.Title>添加 Koyeb 账号</LayerDialog.Title>
          <LayerDialog.Description>
            请输入 Koyeb API 令牌。以备注名区分。
          </LayerDialog.Description>
          <LayerDialog.Body>
            <div className="space-y-4">
              <div className="space-y-1">
                <label className="text-[11px] font-semibold text-kumo-subtle">备注名称</label>
                <Input size="sm"
                  aria-label="Koyeb 备注名称"
                  type="text"
                  value={newKoyebName}
                  onChange={(e) => setNewKoyebName(e.target.value)}
                  placeholder="我的 Koyeb 账号 1"
                  className="w-full text-kumo-strong p-2 text-xs"
                />
              </div>
              <div className="space-y-1">
                <label className="text-[11px] font-semibold text-kumo-subtle">API 令牌</label>
                <Input size="sm"
                  aria-label="Koyeb API 令牌"
                  type="text"
                  value={newKoyebToken}
                  onChange={(e) => setNewKoyebToken(e.target.value)}
                  placeholder="koyeb_api_token"
                  autoComplete="off"
                  data-1p-ignore
                  data-lpignore="true"
                  data-bwignore="true"
                  data-form-type="other"
                  spellCheck={false}
                  className="w-full text-kumo-strong p-2 text-xs"
                />
              </div>
              {koyebAddAccountError && (
                <div className="text-xs text-kumo-danger p-2 bg-kumo-danger/10 border border-kumo-danger/20 rounded">
                  {koyebAddAccountError}
                </div>
              )}
            </div>
          </LayerDialog.Body>
          <LayerDialog.Actions dismissLabel="取消">
            <LayerDialog.Actions.Primary
              type="button"
              onClick={addKoyebAccount}
              loading={koyebAddingAccount}
            >
              确定添加
            </LayerDialog.Actions.Primary>
          </LayerDialog.Actions>
        </LayerDialog.Content>
      </LayerDialog.Root>
  );
}
