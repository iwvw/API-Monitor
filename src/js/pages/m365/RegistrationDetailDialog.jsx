import React from 'react';
import { Button } from '@cloudflare/kumo/components/button';
import { Dialog } from '@cloudflare/kumo/components/dialog';
import { cx, StatusBadge } from '../../components/ui/AppPrimitives.jsx';
import { formatDateTime } from '../../modules/utils.js';
import {
  getRegistrationResultText,
  getRegistrationStatusLabel,
  getRegistrationTone,
} from './utils.js';

export default function RegistrationDetailDialog({ registrationDetail, setRegistrationDetail }) {
  return (
    <Dialog.Root
      open={!!registrationDetail}
      onOpenChange={open => {
        if (!open) setRegistrationDetail(null);
      }}
    >
      <Dialog className="@container w-[calc(100vw-1rem)] max-w-[calc(100vw-1rem)] p-5 cq-sm:w-full cq-sm:max-w-3xl">
        {registrationDetail ? (
          <div className="space-y-4">
            <Dialog.Title>注册记录详情</Dialog.Title>
            <div className="grid gap-3 cq-sm:grid-cols-2">
              <div className="rounded-lg border border-kumo-line/70 bg-kumo-recessed/10 px-3 py-2.5">
                <div className="text-[11px] text-kumo-subtle">账号</div>
                <div className="mt-1 text-sm font-semibold text-kumo-strong">
                  {registrationDetail.displayName || registrationDetail.userPrincipalName || '-'}
                </div>
                <div className="mt-1 break-all font-mono text-[11px] text-kumo-subtle">
                  {registrationDetail.userPrincipalName || '-'}
                </div>
              </div>
              <div className="rounded-lg border border-kumo-line/70 bg-kumo-recessed/10 px-3 py-2.5">
                <div className="text-[11px] text-kumo-subtle">状态</div>
                <div className="mt-1">
                  <StatusBadge tone={getRegistrationTone(registrationDetail.status)}>
                    {getRegistrationStatusLabel(registrationDetail.status)}
                  </StatusBadge>
                </div>
              </div>
              <div className="rounded-lg border border-kumo-line/70 bg-kumo-recessed/10 px-3 py-2.5">
                <div className="text-[11px] text-kumo-subtle">来源公开页 / 邀请码</div>
                <div className="mt-1 text-sm font-medium text-kumo-strong">
                  {registrationDetail.publicPageName || registrationDetail.inviteName || '-'}
                </div>
                <div className="mt-1 break-all font-mono text-[11px] text-kumo-subtle">
                  {registrationDetail.inviteCode || '-'}
                </div>
              </div>
              <div className="rounded-lg border border-kumo-line/70 bg-kumo-recessed/10 px-3 py-2.5">
                <div className="text-[11px] text-kumo-subtle">目标租户 / Graph 用户 ID</div>
                <div className="mt-1 text-sm font-medium text-kumo-strong">
                  {registrationDetail.accountName || '-'}
                </div>
                <div className="mt-1 break-all font-mono text-[11px] text-kumo-subtle">
                  {registrationDetail.graphUserId || '-'}
                </div>
              </div>
            </div>
            <div className="rounded-lg border border-kumo-line/70 bg-kumo-recessed/10 px-3 py-2.5">
              <div className="text-[11px] text-kumo-subtle">创建时间</div>
              <div className="mt-1 text-sm text-kumo-strong">
                {registrationDetail.createdAt
                  ? formatDateTime(registrationDetail.createdAt)
                  : '-'}
              </div>
            </div>
            <div className="rounded-lg border border-kumo-line/70 bg-kumo-base px-3 py-2.5">
              <div className="text-[11px] text-kumo-subtle">结果 / 错误全文</div>
              <div
                className={cx(
                  'mt-2 whitespace-pre-wrap break-words rounded-md px-3 py-2 text-sm',
                  registrationDetail.errorMessage
                    ? 'border border-kumo-danger/20 bg-kumo-danger/5 text-kumo-danger'
                    : 'border border-kumo-line/70 bg-kumo-recessed/10 text-kumo-strong'
                )}
              >
                {getRegistrationResultText(registrationDetail)}
              </div>
            </div>
            <div className="flex justify-end">
              <Button size="sm" variant="secondary" onClick={() => setRegistrationDetail(null)}>
                关闭
              </Button>
            </div>
          </div>
        ) : null}
      </Dialog>
    </Dialog.Root>
  );
}
