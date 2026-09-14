import React from 'react';
import { Button } from '@cloudflare/kumo/components/button';
import { Switch } from '@cloudflare/kumo/components/switch';
import { Table } from '@cloudflare/kumo/components/table';
import { Badge, DropdownMenu, Meter } from '@cloudflare/kumo';
import { AppTable, DataTableFrame, SectionCard } from '../../components/ui/AppPrimitives.jsx';
import { Copy, Edit, Plus, RefreshCw, Trash } from '../../components/Icons.jsx';
import { SUBSCRIPTION_COLUMNS } from './constants.js';
import { copyText, formatBytes, formatTime, meteringLabel, statusLabel, subscriptionURL } from './utils.js';

export default function SubscriptionsPanel({ subscriptions, visibleNodesCount, createDisabled, publicBase, plans, isArmed, onCreate, onEdit, onToggleEnabled, onDelete, onResetToken, onRotateAddress }) {
  const currentSubscriptions = subscriptions;
  return (
    <SectionCard
      title={`订阅管理 (${currentSubscriptions.length})`}
      bodyPadding="none"
      actions={(
        <div className="flex min-w-0 flex-wrap items-center justify-end gap-2">
          <span className="hidden rounded border border-kumo-info/20 bg-kumo-info/10 px-1.5 py-0.5 text-[11px] font-semibold text-kumo-info cq-sm:inline-flex">{visibleNodesCount} 个节点</span>
          <span className="hidden rounded border border-kumo-badge-purple/20 bg-kumo-badge-purple/10 px-1.5 py-0.5 text-[11px] font-semibold text-kumo-badge-purple cq-sm:inline-flex">{currentSubscriptions.length} 个订阅</span>
          <Button size="sm" variant="primary" onClick={() => onCreate()} disabled={createDisabled}><Plus className="h-3.5 w-3.5" />生成订阅</Button>
        </div>
      )}
    >
      <DataTableFrame variant="embedded">
        <AppTable tableId="subscriptions" columns={SUBSCRIPTION_COLUMNS}>
          <Table.Header sticky variant="compact">
            <Table.Row>
              <Table.Head className="text-center">启用</Table.Head>
              <Table.Head>订阅链接</Table.Head>
              <Table.Head className="text-center">状态</Table.Head>
              <Table.Head>流量</Table.Head>
              <Table.Head className="text-center">访问</Table.Head>
              <Table.Head className="app-table-action">操作</Table.Head>
            </Table.Row>
          </Table.Header>
          <Table.Body>
            {currentSubscriptions.map((sub) => {
              const [label, variant] = statusLabel(sub);
              const used = (sub.traffic?.upload || 0) + (sub.traffic?.download || 0);
              const meteringAvailable = sub.traffic?.metering_status === 'available';
				const meteringStatus = sub.traffic?.metering_status || 'pending';
              const link = subscriptionURL(publicBase, sub);
              return (
                <Table.Row key={sub.id} onDoubleClick={() => onEdit(sub)} className="cursor-pointer">
                  <Table.Cell className="text-center">
                    <Switch size="sm" aria-label={sub.enabled ? `停用订阅 ${sub.name}` : `启用订阅 ${sub.name}`} checked={!!sub.enabled} onCheckedChange={(checked) => onToggleEnabled(sub, checked)} />
                  </Table.Cell>
                  <Table.Cell>
                    <div className="truncate text-sm font-semibold text-kumo-strong">{sub.name}</div>
                    <div className="mt-1 flex min-w-0 flex-wrap items-center gap-1">
                      <span className="rounded border border-kumo-info/20 bg-kumo-info/10 px-1.5 py-0.5 text-[10px] font-semibold text-kumo-info">{sub.node_count || 0} 个节点</span>
						<Badge variant="neutral">{plans.find((plan) => plan.id === sub.plan_id)?.name || '未绑定套餐'}</Badge>
                    </div>
                  </Table.Cell>
                  <Table.Cell className="text-center">
                    <Badge variant={variant} appearance="dot">{label}</Badge>
                  </Table.Cell>
                  <Table.Cell>
                    <Meter
                      label="已用流量"
                      value={meteringAvailable ? Math.min(100, sub.traffic?.percent || 0) : 0}
						customValue={meteringAvailable ? `${formatBytes(used)} / ${sub.traffic?.total ? formatBytes(sub.traffic.total) : '无限制'}` : meteringLabel(meteringStatus)}
                    />
                    <div className="mt-1 text-[10px] text-kumo-subtle">
                      {sub.traffic?.cycle_end ? `下次重置 ${formatTime(sub.traffic.cycle_end)}` : '不自动重置'}
                    </div>
                  </Table.Cell>
                  <Table.Cell>
                    <div className="text-xs font-semibold text-kumo-strong">{sub.access_count_today || 0} 次</div>
                    <div className="mt-1 text-[11px] text-kumo-subtle">{formatTime(sub.last_access_at)}</div>
                  </Table.Cell>
                  <Table.Cell className="text-center">
                    <div className="inline-flex items-center justify-center gap-2">
                      <DropdownMenu>
                        <DropdownMenu.Trigger
                          render={<Button size="sm" shape="square" variant="secondary" aria-label="复制订阅链接" title="复制订阅链接" icon={<Copy className="h-3.5 w-3.5" />} />}
                        />
                        <DropdownMenu.Content side="bottom" align="end" sideOffset={6} className="min-w-52">
                          <DropdownMenu.Item onClick={() => copyText(link, '自适应订阅链接已复制')}>
                            自适应订阅（按客户端自动识别）
                          </DropdownMenu.Item>
                          <DropdownMenu.Item onClick={() => copyText(subscriptionURL(publicBase, sub, 'clash'), 'Mihomo / Clash 订阅链接已复制')}>
                            Mihomo / Clash（YAML）
                          </DropdownMenu.Item>
                          <DropdownMenu.Item onClick={() => copyText(subscriptionURL(publicBase, sub, 'base64'), 'Base64 订阅链接已复制')}>
                            sing-box 官方 / v2rayN（Base64）
                          </DropdownMenu.Item>
                          <DropdownMenu.Item onClick={() => copyText(subscriptionURL(publicBase, sub, 'raw'), 'Raw 订阅链接已复制')}>
                            通用节点链接（Raw）
                          </DropdownMenu.Item>
                          <DropdownMenu.Item onClick={() => copyText(subscriptionURL(publicBase, sub, 'info'), '订阅信息页链接已复制')}>
                            订阅信息页（浏览器打开）
                          </DropdownMenu.Item>
                        </DropdownMenu.Content>
                      </DropdownMenu>
                      <Button size="sm" shape="square" variant="secondary" aria-label="编辑订阅链接" title="编辑订阅链接" onClick={() => onEdit(sub)} icon={<Edit className="h-3.5 w-3.5" />} />
                      <DropdownMenu>
                        <DropdownMenu.Trigger
                          render={<Button size="sm" shape="square" variant="secondary" aria-label="订阅安全操作" title="订阅安全操作" icon={<RefreshCw className="h-3.5 w-3.5" />} />}
                        />
                        <DropdownMenu.Content side="bottom" align="end" sideOffset={6} className="min-w-56">
                          <DropdownMenu.Item onClick={() => onRotateAddress(sub)}>
                            更换订阅地址（凭据不变）
                          </DropdownMenu.Item>
                          <DropdownMenu.Item onClick={() => onResetToken(sub)} variant="danger">
                            重置连接凭据（UUID / 密码）
                          </DropdownMenu.Item>
                        </DropdownMenu.Content>
                      </DropdownMenu>
                      <Button size="sm" shape="square" variant={isArmed(`subscription-delete:${sub.id}`) ? 'destructive' : 'secondary-destructive'} aria-label="删除订阅链接" title="删除订阅链接" onClick={() => onDelete(sub)} icon={<Trash className="h-3.5 w-3.5" />} />
                    </div>
                  </Table.Cell>
                </Table.Row>
              );
            })}
            {currentSubscriptions.length === 0 && (
              <Table.Row><Table.Cell colSpan={6} className="p-8 text-center text-kumo-subtle">暂无订阅</Table.Cell></Table.Row>
            )}
          </Table.Body>
        </AppTable>
      </DataTableFrame>
    </SectionCard>
  );
}
