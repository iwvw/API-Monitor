import React from 'react';
import { Button } from '@cloudflare/kumo/components/button';
import { Dialog } from '@cloudflare/kumo/components/dialog';
import { Input } from '@cloudflare/kumo/components/input';

export default function GroupDialog({
  open,
  onOpenChange,
  groupForm,
  setGroupForm,
  submitGroup,
  submittingGroup,
}) {
  return (
    <Dialog.Root open={open} onOpenChange={onOpenChange}>
      <Dialog size="sm" className="p-5">
        <div className="space-y-4">
          <Dialog.Title>新建组</Dialog.Title>
          <div className="grid gap-3">
            <Input
              size="sm"
              aria-label="组名称"
              value={groupForm.displayName}
              onChange={event =>
                setGroupForm(current => ({ ...current, displayName: event.target.value }))
              }
              placeholder="组名称"
            />
            <Input
              size="sm"
              aria-label="邮件别名"
              value={groupForm.mailNickname}
              onChange={event =>
                setGroupForm(current => ({ ...current, mailNickname: event.target.value }))
              }
              placeholder="mailNickname"
            />
          </div>
          <div className="flex justify-end gap-2">
            <Button size="sm" variant="secondary" onClick={() => onOpenChange(false)}>
              取消
            </Button>
            <Button size="sm" variant="primary" onClick={submitGroup} disabled={submittingGroup}>
              {submittingGroup ? '创建中...' : '创建'}
            </Button>
          </div>
        </div>
      </Dialog>
    </Dialog.Root>
  );
}
