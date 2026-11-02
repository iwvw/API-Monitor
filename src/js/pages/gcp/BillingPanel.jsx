import React from 'react';
import { Button } from '@cloudflare/kumo/components/button';
import { Select } from '@cloudflare/kumo/components/select';
import { Table } from '@cloudflare/kumo/components/table';
import { Chart } from '@cloudflare/kumo';
import { SkeletonLine } from '@cloudflare/kumo/components/loader';
import { InlineCopyText } from '@cloudflare/kumo/components/inline-copy-text';
import { AppTable, DataTableFrame, EmptyState, KeyValueGrid, SectionCard, StatusBadge } from '../../components/ui/AppPrimitives.jsx';
import { Cloud, Cpu, PieChart } from '../../components/Icons.jsx';
import { BILLING_ACCOUNT_TABLE_COLUMNS, BUDGET_TABLE_COLUMNS, MODEL_USAGE_TABLE_COLUMNS } from './constants.js';
import { formatCount } from './utils.jsx';

export default function BillingPanel({
  scopeReady,
  selectedAccountId,
  selectedProjectId,
  loadingBilling,
  billingAccounts,
  billingInfo,
  budgets,
  modelUsage,
  loadingModelUsage,
  modelUsageDays,
  onModelUsageDaysChange,
  modelUsageChartData,
  modelUsageChartOptions,
  siteFontEcharts,
  isDarkMode,
  onOpenDetail,
  onCopy,
}) {
  return (
    <div className="columns-1 gap-3 lg:columns-2 [&>*]:mb-3 [&>*]:break-inside-avoid">
      <SectionCard title="计费账号" icon={<PieChart className="h-4 w-4" />} bodyPadding="none">
        {!selectedAccountId ? (
          <EmptyState card={false} icon={Cloud} title="请选择账号" className="min-h-48" />
        ) : loadingBilling ? (
          <div className="p-4"><SkeletonLine className="h-5 w-full" /></div>
        ) : billingAccounts.length === 0 ? (
          <EmptyState card={false} icon={PieChart} title="没有可访问的计费账号" description="SA 可能需要 billing 相关权限" className="min-h-48" />
        ) : (
          <DataTableFrame variant="embedded" density="dense" className="overflow-auto">
            <AppTable tableId="gcp-billing-accounts" columns={BILLING_ACCOUNT_TABLE_COLUMNS}>
              <Table.Header variant="compact">
                <Table.Row>
                  <Table.Head>计费账号 ID</Table.Head>
                  <Table.Head>名称</Table.Head>
                  <Table.Head>状态</Table.Head>
                </Table.Row>
              </Table.Header>
              <Table.Body>
                {billingAccounts.map((account) => (
                  <Table.Row key={account.name}>
                    <Table.Cell><Button type="button" variant="ghost" size="xs" className="block h-auto max-w-full truncate px-1.5 py-0.5 font-mono text-xs text-kumo-strong" onClick={() => onOpenDetail('billingAccount', account)} title={`${account.name} · 点击查看详情`}>{account.name}</Button></Table.Cell>
                    <Table.Cell className="truncate text-sm text-kumo-strong" title={account.displayName}>{account.displayName || '-'}</Table.Cell>
                    <Table.Cell><StatusBadge tone={account.open ? 'success' : 'neutral'}>{account.open ? '启用' : '停用'}</StatusBadge></Table.Cell>
                  </Table.Row>
                ))}
              </Table.Body>
            </AppTable>
          </DataTableFrame>
        )}
      </SectionCard>
      <SectionCard title="项目计费信息" icon={<PieChart className="h-4 w-4" />} bodyPadding="md">
        {!selectedAccountId || !selectedProjectId ? (
          <EmptyState card={false} icon={Cloud} title="请选择账号与项目" className="min-h-32" />
        ) : loadingBilling ? (
          <div className="p-4"><SkeletonLine className="h-5 w-full" /></div>
        ) : !billingInfo ? (
          <EmptyState card={false} icon={PieChart} title="暂无项目计费信息" description="SA 需要 billing 相关权限，或该项目未关联结算账号" className="min-h-32" />
        ) : (
          <KeyValueGrid
            columns={2}
            items={[
              {
                label: '结算账号',
                value: billingInfo.billingAccountName ? (
                  <InlineCopyText
                    value={billingInfo.billingAccountName}
                    variant="mono-secondary"
                    size="lg"
                    truncate
                    className="max-w-full"
                    labels={{ copyAction: '复制结算账号', copied: '结算账号已复制' }}
                  >
                    {billingInfo.billingAccountName}
                  </InlineCopyText>
                ) : '-',
              },
              {
                label: '计费状态',
                value: <StatusBadge tone={billingInfo.billingEnabled ? 'success' : 'neutral'}>{billingInfo.billingEnabled ? '已启用' : '未启用'}</StatusBadge>,
              },
            ]}
          />
        )}
      </SectionCard>
      <SectionCard title="预算" icon={<PieChart className="h-4 w-4" />} bodyPadding="none">
        {budgets.length === 0 ? (
          <EmptyState card={false} icon={PieChart} title="暂无预算" className="min-h-40" />
        ) : (
          <DataTableFrame variant="embedded" density="dense" className="overflow-auto">
            <AppTable tableId="gcp-budgets" columns={BUDGET_TABLE_COLUMNS}>
              <Table.Header variant="compact">
                <Table.Row>
                  <Table.Head>预算名称</Table.Head>
                  <Table.Head>金额</Table.Head>
                  <Table.Head>阈值</Table.Head>
                </Table.Row>
              </Table.Header>
              <Table.Body>
                {budgets.map((budget) => (
                  <Table.Row key={budget.name}>
                    <Table.Cell><Button type="button" variant="ghost" size="xs" className="block h-auto max-w-full truncate px-1.5 py-0.5 font-semibold text-kumo-strong" onClick={() => onOpenDetail('budget', budget)} title={`${budget.displayName || budget.name} · 点击查看详情`}>{budget.displayName || budget.name}</Button></Table.Cell>
                    <Table.Cell className="whitespace-nowrap text-sm text-kumo-strong">{budget.amount ? `${budget.amount} ${budget.currencyCode || ''}`.trim() : '-'}</Table.Cell>
                    <Table.Cell className="truncate text-xs text-kumo-subtle" title={(budget.thresholdRules || []).map((rule) => `${rule.thresholdPercent}%`).join(' / ')}>
                      {(budget.thresholdRules || []).map((rule) => `${rule.thresholdPercent}%`).join(' / ') || '-'}
                    </Table.Cell>
                  </Table.Row>
                ))}
              </Table.Body>
            </AppTable>
          </DataTableFrame>
        )}
      </SectionCard>
      <SectionCard
        title="模型用量"
        icon={<Cpu className="h-4 w-4" />}
        actions={scopeReady && (
          <Select alignItemWithTrigger size="sm" aria-label="用量时间范围" value={String(modelUsageDays)} onValueChange={onModelUsageDaysChange} className="w-32" items={[
            { value: '7', label: '近 7 天' },
            { value: '30', label: '近 30 天' },
            { value: '90', label: '近 90 天' },
          ]} />
        )}
        bodyPadding="none"
      >
        {!scopeReady ? (
          <EmptyState card={false} icon={Cloud} title="请选择账号与项目" className="min-h-48" />
        ) : loadingModelUsage ? (
          <div className="p-4"><SkeletonLine className="h-5 w-full" /></div>
        ) : !modelUsage || (modelUsage.total ?? 0) === 0 ? (
          <EmptyState card={false} icon={Cpu} title="暂无模型调用量" description="SA 需要 roles/monitoring.viewer 权限；若无调用则此处为空" className="min-h-48" />
        ) : (
          <div className="flex flex-col gap-3 p-3">
            <div className="grid grid-cols-2 gap-2 cq-sm:grid-cols-3">
              <div className="rounded-md border border-kumo-interact/85 bg-kumo-base p-2">
                <span className="text-[10px] text-kumo-subtle select-none">总调用量</span>
                <span className="mt-1 block text-base font-semibold tabular-nums text-kumo-strong">{formatCount(modelUsage.total)}</span>
              </div>
              <div className="rounded-md border border-kumo-interact/85 bg-kumo-base p-2">
                <span className="text-[10px] text-kumo-subtle select-none">日均</span>
                <span className="mt-1 block text-base font-semibold tabular-nums text-kumo-strong">{modelUsage.daily?.length ? formatCount(Math.round(modelUsage.total / Math.max(modelUsage.daily.length, 1))) : '-'}</span>
              </div>
              <div className="rounded-md border border-kumo-interact/85 bg-kumo-base p-2">
                <span className="text-[10px] text-kumo-subtle select-none">模型数</span>
                <span className="mt-1 block text-base font-semibold tabular-nums text-kumo-strong">{modelUsage.byModel?.length ?? 0}</span>
              </div>
            </div>
            <div className="min-h-0 w-full rounded-md border border-kumo-interact/90 bg-kumo-base" style={{ height: 168 }}>
              {modelUsageChartData.length === 0 ? (
                <div className="flex h-full items-center justify-center text-sm text-kumo-subtle">暂无数据</div>
              ) : (
                <Chart echarts={siteFontEcharts} isDarkMode={isDarkMode} options={modelUsageChartOptions ?? {}} height={168} />
              )}
            </div>
            {(modelUsage.byModel ?? []).length > 0 && (
              <DataTableFrame variant="card" density="dense" className="overflow-auto">
                <AppTable tableId="gcp-model-usage" columns={MODEL_USAGE_TABLE_COLUMNS}>
                  <Table.Header variant="compact">
                    <Table.Row>
                      <Table.Head>模型</Table.Head>
                      <Table.Head>调用量</Table.Head>
                    </Table.Row>
                  </Table.Header>
                  <Table.Body>
                    {(modelUsage.byModel ?? []).map((group) => (
                      <Table.Row key={group.model}>
                        <Table.Cell><div className="truncate font-mono text-xs text-kumo-strong">{group.model}</div></Table.Cell>
                        <Table.Cell className="tabular-nums">{formatCount(group.count)}</Table.Cell>
                      </Table.Row>
                    ))}
                  </Table.Body>
                </AppTable>
              </DataTableFrame>
            )}
          </div>
        )}
      </SectionCard>
    </div>
  );
}
