import React from 'react';
import { Button } from '@cloudflare/kumo/components/button';
import { Dialog } from '@cloudflare/kumo/components/dialog';
import { Input } from '@cloudflare/kumo/components/input';

export default function AddKoyebAccountDialog({ showAddKoyebModal, setShowAddKoyebModal, newKoyebName, setNewKoyebName, newKoyebToken, setNewKoyebToken, koyebAddAccountError, addKoyebAccount, koyebAddingAccount }) {
  return (
      <Dialog.Root open={showAddKoyebModal} onOpenChange={setShowAddKoyebModal}>
        <Dialog className="!w-[min(32rem,calc(100vw-2rem))] !max-w-[min(32rem,calc(100vw-2rem))] p-6">
          <Dialog.Title className="text-sm font-semibold text-kumo-strong mb-1">
            添加 Koyeb 账号
          </Dialog.Title>
          <Dialog.Description className="text-xs text-kumo-subtle mb-4">
            请输入 Koyeb API 令牌。以备注名区分。
          </Dialog.Description>
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
            <div className="flex justify-end gap-2 pt-2">
              <Dialog.Close
                render={(props) => (
                  <Button size="sm"
                    {...props}
                    variant="secondary"
                    className="text-xs"
                  >
                    取消
                  </Button>
                )}
              />
              <Button size="sm" onClick={addKoyebAccount} disabled={koyebAddingAccount} className="text-xs">
                {koyebAddingAccount ? '正在验证并添加...' : '确定添加'}
              </Button>
            </div>
          </div>
        </Dialog>
      </Dialog.Root>
  );
}
