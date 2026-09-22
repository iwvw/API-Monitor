import React from 'react';
import { Empty, Loader } from '@cloudflare/kumo';
import { Button } from '@cloudflare/kumo/components/button';
import { Table } from '@cloudflare/kumo/components/table';
import { AppTable, DataTableFrame, SectionCard, StatusBadge, cx } from '../../components/ui/AppPrimitives.jsx';
import { Activity, Box, HardDrive, Layers, ShieldCheck } from '../../components/Icons.jsx';
import { BUCKET_LABEL, BUCKET_TONE, TABLE_SUMMARY_COLUMNS } from './constants.js';
import { statusMeta, formatExpireAt, formatDaysLeft, daysTone, formatMoney, formatCost } from './utils.js';

const bucketOrder = ['expired', 'within_7', 'within_30', 'normal', 'no_renew'];

const bucketToneClass = {
  danger: 'bg-kumo-danger/10 text-kumo-danger border-kumo-danger/20',
  warning: 'bg-kumo-warning/10 text-kumo-warning border-kumo-warning/20',
  info: 'bg-kumo-info/10 text-kumo-info border-kumo-info/20',
  neutral: 'bg-kumo-recessed text-kumo-subtle border-kumo-line',
};

function StatCard({ icon, label, value, tone = 'info' }) {
  return (
    <div className="flex min-w-0 items-center gap-3 rounded-lg border border-kumo-line bg-kumo-base p-3.5">
      <span className={cx('flex h-9 w-9 shrink-0 items-center justify-center rounded-md border', bucketToneClass[tone])}>
        {icon}
      </span>
      <div className="min-w-0">
        <div className="text-xs text-kumo-subtle">{label}</div>
        <div className="mt-0.5 font-mono text-lg font-semibold text-kumo-strong">{value}</div>
      </div>
    </div>
  );
}

export default function OverviewPanel({ overview, loading, onSelectBucket, onOpenAsset }) {
  if (loading && !overview) {
    return (
      <SectionCard title="资产总览" bodyPadding="none">
        <Empty
          size="base"
          className="rounded-none border-0 bg-transparent"
          icon={<Loader size={32} className="text-kumo-info" />}
          title="正在加载资产总览"
          description="统计资产计数、到期与成本"
        />
      </SectionCard>
    );
  }

  const data = overview || {
    physical_count: 0,
    virtual_count: 0,
    expiring_count: 0,
    expired_count: 0,
    orphan_count: 0,
    buckets: {},
    costs: [],
    total_monthly: {},
    recent_expiring: [],
  };

  const hasAny = data.physical_count + data.virtual_count > 0;

  return (
    <div className="flex min-w-0 flex-col gap-3 cq-sm:gap-4">
      <div className="grid grid-cols-2 gap-3 cq-md:grid-cols-4">
        <StatCard icon={<HardDrive className="h-4 w-4" />} label="实体资产" value={data.physical_count} tone="info" />
        <StatCard icon={<Box className="h-4 w-4" />} label="虚拟资产" value={data.virtual_count} tone="info" />
        <StatCard icon={<Activity className="h-4 w-4" />} label="即将到期" value={data.expiring_count} tone="warning" />
        <StatCard icon={<ShieldCheck className="h-4 w-4" />} label="已过期" value={data.expired_count} tone="danger" />
      </div>

      <SectionCard
        icon={<Layers className="h-4 w-4 text-kumo-info" />}
        title="到期分桶"
        description="点击分桶可跳转到对应筛选的资产列表"
        bodyPadding="md"
      >
        <div className="flex min-w-0 flex-wrap gap-2">
          {bucketOrder.map(bucket => {
            const count = data.buckets?.[bucket] || 0;
            const tone = BUCKET_TONE[bucket] || 'neutral';
            return (
              <Button
                key={bucket}
                size="sm"
                variant="ghost"
                disabled={count === 0}
                onClick={() => onSelectBucket?.(bucket)}
                className={cx(
                  'flex h-auto min-w-[7rem] flex-1 flex-col items-start gap-1 rounded-lg border px-3 py-2.5 text-left font-normal',
                  bucketToneClass[tone],
                  count === 0 ? 'cursor-default opacity-60' : 'hover:brightness-110'
                )}
              >
                <span className="text-xs">{BUCKET_LABEL[bucket]}</span>
                <span className="font-mono text-lg font-semibold">{count}</span>
              </Button>
            );
          })}
        </div>
      </SectionCard>

      <SectionCard
        icon={<Box className="h-4 w-4 text-kumo-info" />}
        title="成本汇总"
        description="按币种分组展示月均；一次性成本单列，不做跨币种自动换算"
        bodyPadding="md"
      >
        {data.costs && data.costs.length > 0 ? (
          <div className="flex min-w-0 flex-col gap-3">
            <div className="flex min-w-0 flex-wrap gap-2">
              {data.costs.map(cost => (
                <div key={cost.currency} className="min-w-[10rem] flex-1 rounded-lg border border-kumo-line bg-kumo-recessed/30 px-3 py-2.5">
                  <div className="text-xs text-kumo-subtle">{cost.currency} 月均</div>
                  <div className="mt-0.5 font-mono text-base font-semibold text-kumo-strong">
                    {formatMoney(cost.monthly, cost.currency)}
                  </div>
                  {cost.one_time > 0 && (
                    <div className="mt-1 text-[11px] text-kumo-subtle">
                      一次性 {formatMoney(cost.one_time, cost.currency)}
                    </div>
                  )}
                </div>
              ))}
            </div>
            {Object.keys(data.total_monthly || {}).length > 0 && (
              <div className="text-xs text-kumo-subtle">
                按配置汇率折算合计：
                {Object.entries(data.total_monthly).map(([currency, total]) => (
                  <span key={currency} className="ml-2 font-mono text-kumo-strong">
                    {formatMoney(total, currency)}
                  </span>
                ))}
              </div>
            )}
          </div>
        ) : (
          <div className="text-xs text-kumo-subtle">暂无成本信息。为资产填写成本金额与计费周期后在此汇总。</div>
        )}
      </SectionCard>

      <SectionCard
        icon={<Activity className="h-4 w-4 text-kumo-info" />}
        title="最近到期"
        description="按到期时间升序，取前 10 条"
        bodyPadding="none"
      >
        {hasAny && data.recent_expiring?.length > 0 ? (
          <DataTableFrame variant="embedded">
            <AppTable tableId="asset-recent-expiring" columns={TABLE_SUMMARY_COLUMNS}>
              <Table.Header variant="compact">
                <Table.Row>
                  <Table.Head>名称</Table.Head>
                  <Table.Head>状态</Table.Head>
                  <Table.Head>到期日</Table.Head>
                  <Table.Head className="text-right">剩余</Table.Head>
                  <Table.Head>成本</Table.Head>
                </Table.Row>
              </Table.Header>
              <Table.Body>
                {data.recent_expiring.map(asset => {
                  const meta = statusMeta(asset.derived_status);
                  return (
                    <Table.Row
                      key={asset.id}
                      className="cursor-pointer hover:bg-kumo-recessed/25"
                      onClick={() => onOpenAsset?.(asset)}
                    >
                      <Table.Cell>
                        <span className="truncate text-sm font-medium text-kumo-strong" title={asset.name}>{asset.name}</span>
                      </Table.Cell>
                      <Table.Cell className="text-center">
                        <StatusBadge tone={meta.tone}>{meta.label}</StatusBadge>
                      </Table.Cell>
                      <Table.Cell className="whitespace-nowrap text-kumo-subtle">{formatExpireAt(asset.expire_at)}</Table.Cell>
                      <Table.Cell className="whitespace-nowrap text-right">
                        <StatusBadge tone={daysTone(asset)}>{formatDaysLeft(asset)}</StatusBadge>
                      </Table.Cell>
                      <Table.Cell className="truncate text-kumo-subtle">{formatCost(asset)}</Table.Cell>
                    </Table.Row>
                  );
                })}
              </Table.Body>
            </AppTable>
          </DataTableFrame>
        ) : (
          <Empty
            size="base"
            className="rounded-none border-0 bg-transparent"
            icon={<Activity className="h-8 w-8 text-kumo-secondary" />}
            title="暂无即将到期资产"
            description="登记资产并填写到期时间后，这里会显示最近的到期项"
          />
        )}
      </SectionCard>
    </div>
  );
}
