import React from 'react';
import { LayerCard, Popover, Table } from '@cloudflare/kumo';
import { Badge } from '@cloudflare/kumo/components/badge';
import { Button } from '@cloudflare/kumo/components/button';
import { SkeletonLine } from '@cloudflare/kumo/components/loader';
import { Check, Copy, Eye, Plus, Search, Shield, Trash } from '../../components/Icons.jsx';
import { handleEditableRowDoubleClick } from '../../modules/tableInteractions.js';
import { statusVariant, zoneNameServers, zoneStatusLabel, zoneTypeLabel } from './utils.jsx';

function ZoneListPanel({
  loading,
  zones,
  selectedZoneId,
  selectedZone,
  zoneColWidths,
  isArmed,
  onOpenZoneModal,
  onPurgeZoneCache,
  onDeleteZone,
  onSelectZone,
  onCopyText,
}) {
  return (
    <>
      <div className="flex min-h-8 shrink-0 flex-col gap-2 pl-px cq-sm:flex-row cq-sm:items-center cq-sm:justify-between">
        <div className="grid grid-cols-3 gap-2 cq-sm:flex cq-sm:flex-wrap cq-sm:items-center">
          <Button size="sm" variant="secondary" onClick={onOpenZoneModal} icon={<Plus className="h-4 w-4" />} className="w-full justify-center cq-sm:w-auto">
            添加域名
          </Button>
          {selectedZone && (
            <>
              <Button size="sm" variant={isArmed(`zone-cache:${selectedZoneId}`) ? 'destructive' : 'secondary-destructive'} onClick={onPurgeZoneCache} disabled={loading.purge} icon={<Shield className="h-4 w-4" />} className="w-full justify-center cq-sm:w-auto">
                清除缓存
              </Button>
              <Button size="sm" variant={isArmed(`zone:${selectedZone.id}`) ? 'destructive' : 'secondary-destructive'} onClick={() => onDeleteZone(selectedZone)} icon={<Trash className="h-4 w-4" />} className="w-full justify-center cq-sm:w-auto">
                删除域名
              </Button>
            </>
          )}
        </div>
      </div>
      <div className="grid grid-cols-2 gap-2 @[370px]:grid-cols-3 cq-md:hidden">
        {loading.zones ? (
          Array.from({ length: 4 }).map((_, index) => (
            <LayerCard key={index} className="min-w-0 p-2">
              <SkeletonLine className="h-4 w-24" />
              <SkeletonLine className="mt-2 h-5 w-20" />
            </LayerCard>
          ))
        ) : zones.length === 0 ? (
          <LayerCard className="min-w-full p-8 text-center text-xs text-kumo-subtle">暂无域名。</LayerCard>
        ) : zones.map((zone) => (
          <LayerCard
            key={zone.id}
            className={`min-w-0 cursor-pointer p-2 ${zone.id === selectedZoneId ? 'border-brand/60 bg-brand/5 ring-1 ring-brand/35' : ''}`}
            onClick={() => onSelectZone(zone)}
          >
            <div className="min-w-0">
              <div className="truncate text-sm font-semibold leading-[1.35] text-kumo-strong" title={zone.name}>{zone.name}</div>
              <div className="mt-2 flex items-center gap-1.5">
                <Badge variant={statusVariant(zone.status)} className="text-[10px] leading-4">{zoneStatusLabel(zone.status)}</Badge>
                <Badge variant="secondary" className="text-[10px] leading-4">{zoneTypeLabel(zone.type)}</Badge>
              </div>
            </div>
          </LayerCard>
        ))}
      </div>
      <div className="dns-table-frame hidden max-w-full cq-md:flex">
        <div className="dns-table-scroll scrollbar-thin">
        <Table layout="fixed" className="w-full text-xs">
          <colgroup>
            {zoneColWidths.map((width, index) => <col key={index} style={{ width }} />)}
          </colgroup>
          <Table.Header sticky variant="compact">
            <Table.Row className="h-8">
              <Table.Head className="!px-2.5 !py-1.5 text-left">域名</Table.Head>
              <Table.Head className="!px-2.5 !py-1.5 text-center">状态</Table.Head>
              <Table.Head className="!px-2.5 !py-1.5 text-center">类型</Table.Head>
              <Table.Head className="!px-2.5 !py-1.5 text-center">NS</Table.Head>
              <Table.Head className="app-table-action !px-2 !py-1.5">操作</Table.Head>
            </Table.Row>
          </Table.Header>
          <Table.Body>
            {loading.zones ? (
              Array.from({ length: 4 }).map((_, index) => (
                <Table.Row key={index} className="h-9">
                  <Table.Cell className="!px-2.5 !py-1.5 text-left"><SkeletonLine className="h-3.5 w-32" /></Table.Cell>
                  <Table.Cell className="!px-2.5 !py-1.5 text-center"><SkeletonLine className="mx-auto h-3.5 w-14" /></Table.Cell>
                  <Table.Cell className="!px-2.5 !py-1.5 text-center"><SkeletonLine className="mx-auto h-3.5 w-10" /></Table.Cell>
                  <Table.Cell className="!px-2.5 !py-1.5 text-center"><SkeletonLine className="mx-auto h-3.5 w-8" /></Table.Cell>
                  <Table.Cell className="!px-2 !py-1.5 text-center"><SkeletonLine className="mx-auto h-3.5 w-12" /></Table.Cell>
                </Table.Row>
              ))
            ) : zones.length === 0 ? (
              <Table.Row>
                <Table.Cell colSpan={5} className="py-10 text-center text-kumo-subtle">
                  暂无域名。
                </Table.Cell>
              </Table.Row>
            ) : zones.map((zone) => (
              <Table.Row
                key={zone.id}
                variant={zone.id === selectedZoneId ? 'selected' : 'default'}
                className="h-9 cursor-pointer"
                title="双击进入管理"
                onDoubleClick={(event) => handleEditableRowDoubleClick(event, () => onSelectZone(zone))}
              >
                <Table.Cell className="!px-2.5 !py-1.5 text-left">
                  <div className="flex min-w-0">
                    <span className="truncate font-semibold text-kumo-strong" title={zone.name}>{zone.name}</span>
                  </div>
                </Table.Cell>
                <Table.Cell className="!px-2.5 !py-1.5 text-center">
                  <Badge variant={statusVariant(zone.status)} className="text-[10px] leading-4">{zoneStatusLabel(zone.status)}</Badge>
                </Table.Cell>
                <Table.Cell className="!px-2.5 !py-1.5 text-center">{zoneTypeLabel(zone.type)}</Table.Cell>
                <Table.Cell className="!px-2.5 !py-1.5 text-center">
                  <div className="flex w-full justify-center">
                    <Popover>
                      <Popover.Trigger
                        render={(
                          <Button
                            size="sm"
                            shape="square"
                            variant="secondary"
                            aria-label={`查看 ${zone.name} 名称服务器`}
                            title="名称服务器"
                            onClick={(event) => event.stopPropagation()}
                            icon={<Eye className="h-3.5 w-3.5" />}
                          />
                        )}
                      />
                      <Popover.Content side="right" align="center" className="w-80 p-3" onClick={(event) => event.stopPropagation()}>
                        <div className="mb-2 flex items-center justify-between gap-2">
                          <Popover.Title className="truncate text-sm font-semibold text-kumo-strong">
                            名称服务器
                          </Popover.Title>
                          <Button
                            size="sm"
                            variant="secondary"
                            disabled={zoneNameServers(zone).length === 0}
                            onClick={() => onCopyText(zoneNameServers(zone).join('\n'), '名称服务器')}
                            icon={<Copy className="h-3.5 w-3.5" />}
                          >
                            全部
                          </Button>
                        </div>
                        <div className="mb-2 truncate text-xs text-kumo-subtle" title={zone.name}>{zone.name}</div>
                        <div className="grid gap-2">
                          {zoneNameServers(zone).length > 0 ? zoneNameServers(zone).map((nameServer, index) => (
                            <div key={`${nameServer}-${index}`} className="flex min-w-0 items-center gap-2 rounded-md border border-kumo-line bg-kumo-recessed/25 px-2.5 py-2">
                              <code className="min-w-0 flex-1 truncate font-mono text-xs text-kumo-strong" title={nameServer}>
                                {nameServer}
                              </code>
                              <Button
                                size="sm"
                                shape="square"
                                variant="secondary"
                                aria-label={`复制 ${nameServer}`}
                                title="复制"
                                onClick={() => onCopyText(nameServer, '名称服务器')}
                                icon={<Copy className="h-3.5 w-3.5" />}
                              />
                            </div>
                          )) : (
                            <div className="rounded-md border border-kumo-line bg-kumo-recessed/25 px-3 py-3 text-center text-xs text-kumo-subtle">
                              暂无名称服务器
                            </div>
                          )}
                        </div>
                      </Popover.Content>
                    </Popover>
                  </div>
                </Table.Cell>
                <Table.Cell className="!px-2 !py-1.5 text-center">
                  <div className="inline-flex gap-1">
                    <Button size="sm" shape="square" variant="secondary" onClick={(event) => {
                      event.stopPropagation();
                      onSelectZone(zone);
                    }} aria-label={zone.id === selectedZoneId ? `已选择 ${zone.name}` : `管理 ${zone.name}`} title={zone.id === selectedZoneId ? '已选择' : '管理'}>
                      {zone.id === selectedZoneId ? <Check className="h-3.5 w-3.5" /> : <Search className="h-3.5 w-3.5" />}
                    </Button>
                    <Button size="sm" shape="square" variant={isArmed(`zone:${zone.id}`) ? 'destructive' : 'secondary-destructive'} onClick={(event) => {
                      event.stopPropagation();
                      onDeleteZone(zone);
                    }} aria-label={`删除 ${zone.name}`} title="删除">
                      <Trash className="h-3.5 w-3.5" />
                    </Button>
                  </div>
                </Table.Cell>
              </Table.Row>
            ))}
          </Table.Body>
        </Table>
        </div>
      </div>
    </>
  );
}

export default ZoneListPanel;
