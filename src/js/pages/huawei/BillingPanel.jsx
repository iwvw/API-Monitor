import React from 'react';
import { Table } from '@cloudflare/kumo/components/table';
import {
  AppTable,
  DataTableFrame,
  EmptyState,
  KeyValueGrid,
  SectionCard,
} from '../../components/ui/AppPrimitives.jsx';
import { PieChart } from '../../components/Icons.jsx';
import SkeletonLines from './SkeletonLines.jsx';
import { BILLING_SUM_COLUMNS, FREE_RESOURCE_COLUMNS } from './constants.js';
import { formatMoney } from './utils.js';

export default function BillingPanel({ loadingScope, billingData, freeResources, formatExpire }) {
  return (
    <div className="columns-1 gap-3 cq-md:columns-2 [&>*]:mb-3 [&>*]:break-inside-avoid">
      <SectionCard title="费用概览">
        {loadingScope ? (
          <SkeletonLines />
        ) : !billingData ? (
          <EmptyState card={false} icon={PieChart} title="暂无费用数据" description="切换到有数据的账单月份查看费用概览" className="min-h-48" />
        ) : (
          <KeyValueGrid
            items={[
              { label: '账期', value: billingData.cycle || '-' },
              { label: '账户余额', value: `${formatMoney((billingData.balances || []).reduce((sum, b) => sum + (b.amount || 0), 0))} 元` },
              { label: '当月总消费', value: `${formatMoney(billingData.totalConsume)} 元` },
              { label: '欠费', value: `${formatMoney(billingData.debtAmount)} 元` },
            ]}
          />
        )}
      </SectionCard>

      <SectionCard title="按服务消费" bodyPadding="none">
        {loadingScope ? (
          <SkeletonLines />
        ) : !billingData?.monthlySums?.length ? (
          <EmptyState card={false} icon={PieChart} title="该账期暂无消费明细" description="未产生费用或数据未出账" className="min-h-48" />
        ) : (
          <DataTableFrame variant="embedded" density="dense" className="overflow-auto">
            <AppTable tableId="huawei-billing-sum" columns={BILLING_SUM_COLUMNS}>
              <Table.Header variant="compact">
                <Table.Row>
                  <Table.Head>服务</Table.Head>
                  <Table.Head>资源类型</Table.Head>
                  <Table.Head>消费金额</Table.Head>
                </Table.Row>
              </Table.Header>
              <Table.Body>
                {billingData.monthlySums.map((sum, index) => (
                  <Table.Row key={`${sum.serviceTypeName}-${index}`}>
                    <Table.Cell><span className="block max-w-full truncate font-medium text-kumo-strong">{sum.serviceTypeName || '-'}</span></Table.Cell>
                    <Table.Cell className="truncate text-sm text-kumo-strong">{sum.resourceTypeName || '-'}</Table.Cell>
                    <Table.Cell className="whitespace-nowrap">{formatMoney(sum.consumeAmount)} 元</Table.Cell>
                  </Table.Row>
                ))}
              </Table.Body>
            </AppTable>
          </DataTableFrame>
        )}
      </SectionCard>

      <SectionCard title="Flexus 流量包" bodyPadding="none">
        {loadingScope ? (
          <SkeletonLines />
        ) : freeResources.length === 0 ? (
          <EmptyState card={false} icon={PieChart} title="暂无流量包" description="账号下没有可查询的流量包资源" className="min-h-48" />
        ) : (
          <DataTableFrame variant="embedded" density="dense" className="overflow-auto">
            <AppTable tableId="huawei-free-resources" columns={FREE_RESOURCE_COLUMNS}>
              <Table.Header variant="compact">
                <Table.Row>
                  <Table.Head>资源包</Table.Head>
                  <Table.Head>剩余 / 总量</Table.Head>
                  <Table.Head>当期周期</Table.Head>
                </Table.Row>
              </Table.Header>
              <Table.Body>
                {freeResources.map((fr) => (
                  <Table.Row key={fr.freeResourceId}>
                    <Table.Cell><span className="block max-w-full truncate font-medium text-kumo-strong">{fr.typeName || '-'}</span></Table.Cell>
                    <Table.Cell className="whitespace-nowrap">{Math.round(fr.amount)} / {Math.round(fr.originalAmount)} GB</Table.Cell>
                    <Table.Cell className="whitespace-nowrap text-xs text-kumo-subtle">{formatExpire(fr.endTime)}</Table.Cell>
                  </Table.Row>
                ))}
              </Table.Body>
            </AppTable>
          </DataTableFrame>
        )}
      </SectionCard>
    </div>
  );
}
