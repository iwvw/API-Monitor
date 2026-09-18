import React from 'react';
import { LayerDialog } from '@cloudflare/kumo/components/layer-dialog';
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
    <LayerDialog.Root open={open} onOpenChange={onOpenChange}>
      <LayerDialog.Content size="sm">
        <LayerDialog.Title>生成邀请码</LayerDialog.Title>
        <LayerDialog.Body>
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
        </LayerDialog.Body>
        <LayerDialog.Actions dismissLabel="取消">
          <LayerDialog.Actions.Primary
            type="button"
            onClick={generateInviteCodes}
            loading={generatingInviteCodes}
          >
            生成
          </LayerDialog.Actions.Primary>
        </LayerDialog.Actions>
      </LayerDialog.Content>
    </LayerDialog.Root>
  );
}
