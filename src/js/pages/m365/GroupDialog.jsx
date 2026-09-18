import React from 'react';
import { Button } from '@cloudflare/kumo/components/button';
import { LayerDialog } from '@cloudflare/kumo/components/layer-dialog';
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
    <LayerDialog.Root open={open} onOpenChange={onOpenChange}>
      <LayerDialog.Content size="sm">
        <LayerDialog.Title>新建组</LayerDialog.Title>
        <LayerDialog.Body>
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
        </LayerDialog.Body>
        <LayerDialog.Actions>
          <LayerDialog.Actions.Primary
            type="button"
            onClick={submitGroup}
            loading={submittingGroup}
          >
            {submittingGroup ? '创建中...' : '创建'}
          </LayerDialog.Actions.Primary>
        </LayerDialog.Actions>
      </LayerDialog.Content>
    </LayerDialog.Root>
  );
}
