import React from 'react';
import { Button } from '@cloudflare/kumo/components/button';
import { Dialog } from '@cloudflare/kumo/components/dialog';
import { Input } from '@cloudflare/kumo/components/input';

export default function AddFlyAccountDialog({ showAddFlyModal, setShowAddFlyModal, newFlyName, setNewFlyName, newFlyToken, setNewFlyToken, flyAddAccountError, addFlyAccount, flyAddingAccount }) {
  return (
      <Dialog.Root open={showAddFlyModal} onOpenChange={setShowAddFlyModal}>
        <Dialog className="!w-[min(32rem,calc(100vw-2rem))] !max-w-[min(32rem,calc(100vw-2rem))] p-6">
          <Dialog.Title className="text-sm font-semibold text-kumo-strong mb-1">
            添加 Fly.io 账号
          </Dialog.Title>
          <Dialog.Description className="text-xs text-kumo-subtle mb-4">
            请输入 Fly.io API 令牌（以 flyv1_ 开头）。
          </Dialog.Description>
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
              <Button size="sm" onClick={addFlyAccount} disabled={flyAddingAccount} className="text-xs">
                {flyAddingAccount ? '正在验证并添加...' : '确定添加'}
              </Button>
            </div>
          </div>
        </Dialog>
      </Dialog.Root>
  );
}
