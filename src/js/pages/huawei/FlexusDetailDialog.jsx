import React from 'react';
import { Button } from '@cloudflare/kumo/components/button';
import { Dialog } from '@cloudflare/kumo/components/dialog';
import {
  KeyValueGrid,
  StatusBadge,
} from '../../components/ui/AppPrimitives.jsx';
import { Terminal } from '../../components/Icons.jsx';
import { getStatusTone } from './utils.js';

export default function FlexusDetailDialog({ flexusDetail, onOpenChange, formatExpire, selectedAccountId, setSshTarget }) {
  return (
    <Dialog.Root open={Boolean(flexusDetail)} onOpenChange={onOpenChange}>
      <Dialog className="@container !w-[min(42rem,calc(100vw-2rem))] !max-w-[min(42rem,calc(100vw-2rem))] p-6">
        <Dialog.Title className="mb-1 text-base font-semibold text-kumo-strong">{flexusDetail?.name || 'Flexus L 详情'}</Dialog.Title>
        <Dialog.Description className="mb-4 text-xs text-kumo-subtle">套餐组合与运行信息，全部来自华为云 API，无需登录官网。</Dialog.Description>
        {flexusDetail && (
          <div className="flex flex-col gap-3">
            <KeyValueGrid
              items={[
                {
                  label: '运行状态',
                  value: <StatusBadge tone={getStatusTone(flexusDetail.serverStatus)}>{flexusDetail.serverStatus || '-'}</StatusBadge>,
                },
                { label: '区域 / 项目', value: `${flexusDetail.regionId || '-'} / ${flexusDetail.projectId || '-'}` },
                {
                  label: '规格',
                  value: (
                    <div className="min-w-0">
                      <div className="truncate" title={flexusDetail.specDescription}>{flexusDetail.specDescription || '-'}</div>
                      {flexusDetail.specCode && <div className="truncate font-mono text-[11px] text-kumo-subtle">{flexusDetail.specCode}</div>}
                      {flexusDetail.vcpus > 0 && <div className="text-xs text-kumo-subtle">{flexusDetail.vcpus} vCPU · {flexusDetail.memoryMb} MB</div>}
                    </div>
                  ),
                },
                { label: '云主机', value: flexusDetail.cloudServerName || flexusDetail.cloudServerId || '-' },
                { label: '公网 IP', value: flexusDetail.publicIp || '-' },
                { label: '私网 IP', value: flexusDetail.privateIp || '-' },
                { label: '镜像', value: flexusDetail.imageName || '-' },
                { label: '计费', value: flexusDetail.chargeMode === 'prePaid' ? '包年包月' : flexusDetail.chargeMode || '-' },
                { label: '订单号', value: <code className="text-xs">{flexusDetail.orderId || '-'}</code> },
                { label: '创建时间', value: flexusDetail.createdAt || '-' },
                { label: '到期时间', value: <span className="font-medium">{formatExpire(flexusDetail.expireAt)}</span> },
                {
                  label: '流量包',
                  value: flexusDetail.trafficOriginal
                    ? `${flexusDetail.trafficTypeName || '流量'}：${Math.round(flexusDetail.trafficAmount)} / ${Math.round(flexusDetail.trafficOriginal)} GB（当期至 ${formatExpire(flexusDetail.trafficExpireAt)}）`
                    : '-',
                },
              ]}
            />
            {flexusDetail.composedResources?.length > 0 && (
              <div>
                <div className="mb-1 text-xs font-medium text-kumo-subtle">套餐组成</div>
                <div className="flex flex-wrap gap-1.5">
                  {flexusDetail.composedResources.map((res) => (
                    <span key={res.id || res.name} className="inline-flex items-center gap-1 rounded-md border border-kumo-line bg-kumo-base px-2 py-0.5 text-xs">
                      <code className="max-w-[16rem] truncate" title={res.id}>{res.name || res.id || res.typeName}</code>
                    </span>
                  ))}
                </div>
              </div>
            )}
            {flexusDetail.publicIp && (
              <div className="mt-1 flex items-center gap-2">
                <Button type="button" size="sm" onClick={() => setSshTarget({ accountId: selectedAccountId, instance: { name: flexusDetail.name, publicIp: flexusDetail.publicIp } })}>
                  <Terminal className="mr-1 h-4 w-4" />SSH 终端
                </Button>
              </div>
            )}
          </div>
        )}
      </Dialog>
    </Dialog.Root>
  );
}
