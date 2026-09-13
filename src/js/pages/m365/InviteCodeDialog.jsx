import React from 'react';
import { Button } from '@cloudflare/kumo/components/button';
import { Dialog } from '@cloudflare/kumo/components/dialog';
import { Input } from '@cloudflare/kumo/components/input';
import { Select } from '@cloudflare/kumo/components/select';

export default function InviteCodeDialog({
  open,
  onOpenChange,
  inviteCodeGeneratorForm,
  setInviteCodeGeneratorForm,
  publicPages,
  generateInviteCodes,
  generatingInviteCodes,
}) {
  return (
    <Dialog.Root open={open} onOpenChange={onOpenChange}>
      <Dialog size="sm" className="p-5">
        <div className="space-y-4">
          <Dialog.Title>生成邀请码</Dialog.Title>
          <div className="grid gap-3">
            <Select alignItemWithTrigger
              aria-label="公开页"
              size="sm"
              value={inviteCodeGeneratorForm.publicPageId}
              onValueChange={value =>
                setInviteCodeGeneratorForm(current => ({ ...current, publicPageId: value }))
              }
              items={publicPages.map(page => ({ value: String(page.id), label: page.name }))}
            />
            <Input
              size="sm"
              type="number"
              min="1"
              max="5"
              aria-label="生成数量"
              value={inviteCodeGeneratorForm.quantity}
              onChange={event =>
                setInviteCodeGeneratorForm(current => ({
                  ...current,
                  quantity: event.target.value,
                }))
              }
              placeholder="1-5"
            />
            <div className="rounded-lg border border-kumo-line/70 bg-kumo-base/60 px-3 py-2 text-xs text-kumo-subtle">
              使用带 `code` 的注册链接，默认限 1 次。
            </div>
          </div>
          <div className="flex justify-end gap-2">
            <Button size="sm" variant="secondary" onClick={() => onOpenChange(false)}>
              取消
            </Button>
            <Button
              size="sm"
              variant="primary"
              onClick={generateInviteCodes}
              disabled={generatingInviteCodes}
            >
              {generatingInviteCodes ? '生成中...' : '生成'}
            </Button>
          </div>
        </div>
      </Dialog>
    </Dialog.Root>
  );
}
