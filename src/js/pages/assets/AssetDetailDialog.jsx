import React, { useEffect, useState } from 'react';
import { Empty, Loader } from '@cloudflare/kumo';
import { LayerDialog } from '@cloudflare/kumo/components/layer-dialog';
import { KeyValueGrid, StatusBadge } from '../../components/ui/AppPrimitives.jsx';
import { CATEGORY_LABEL, COST_CYCLE_LABEL, TYPE_LABEL } from './constants.js';
import { fetchEvents } from './api.js';
import { statusMeta, formatExpireAt, formatDaysLeft, daysTone, formatCost, formatSyncTime } from './utils.js';

const EVENT_LABEL = {
  created: '创建',
  updated: '更新',
  renewed: '续费',
  retired: '退役',
  source_lost: '来源失效',
  source_restored: '来源恢复',
};

export default function AssetDetailDialog({ open, asset, onClose }) {
  const [events, setEvents] = useState([]);
  const [loading, setLoading] = useState(false);

  useEffect(() => {
    if (!open || !asset?.id) return;
    let cancelled = false;
    setLoading(true);
    fetchEvents(asset.id)
      .then(list => { if (!cancelled) setEvents(list); })
      .catch(() => { if (!cancelled) setEvents([]); })
      .finally(() => { if (!cancelled) setLoading(false); });
    return () => { cancelled = true; };
  }, [open, asset?.id]);

  if (!asset) return null;
  const meta = statusMeta(asset.derived_status);

  const items = [
    { key: 'category', label: '分类', value: CATEGORY_LABEL[asset.category] || '--' },
    { key: 'type', label: '类型', value: TYPE_LABEL[asset.asset_type] || asset.asset_type || '--' },
    { key: 'provider', label: '提供方', value: asset.provider || '--' },
    { key: 'owner', label: '负责人', value: asset.owner || '--' },
    { key: 'status', label: '状态', value: <StatusBadge tone={meta.tone}>{meta.label}</StatusBadge> },
    { key: 'expire', label: '到期日', value: formatExpireAt(asset.expire_at) },
    { key: 'days', label: '剩余', value: <StatusBadge tone={daysTone(asset)}>{formatDaysLeft(asset)}</StatusBadge> },
    { key: 'auto_renew', label: '自动续费', value: asset.auto_renew ? '是' : '否' },
    { key: 'cost', label: '成本', value: formatCost(asset) },
    { key: 'cycle', label: '计费周期', value: COST_CYCLE_LABEL[asset.cost_cycle] || '--' },
    { key: 'acquire', label: '购置日期', value: asset.acquire_date || '--' },
    { key: 'remark', label: '备注', value: asset.remark || '--' },
  ];

  if (asset.category === 'physical') {
    items.push(
      { key: 'location', label: '位置', value: asset.location || '--' },
      { key: 'serial', label: '序列号', value: asset.serial_no || '--' },
      { key: 'model', label: '型号', value: asset.model || '--' },
    );
  }
  if (asset.origin === 'linked') {
    items.push(
      { key: 'source', label: '来源模块', value: asset.source_module || '--' },
      { key: 'synced', label: '上次同步', value: formatSyncTime(asset.source_synced_at) },
    );
  }

  return (
    <LayerDialog.Root open={open} onOpenChange={next => { if (!next) onClose(); }}>
      <LayerDialog.Content size="xl">
        <LayerDialog.Title>{asset.name}</LayerDialog.Title>
        <LayerDialog.Description>资产详情与生命周期事件</LayerDialog.Description>
        <LayerDialog.Body>
          <div className="space-y-4">
            <KeyValueGrid items={items} columns={2} />

            {asset.tags?.length > 0 && (
              <div className="flex min-w-0 flex-wrap gap-1">
                {asset.tags.map(tag => (
                  <span key={tag} className="rounded border border-kumo-line bg-kumo-recessed px-1.5 py-0.5 text-[11px] text-kumo-subtle">{tag}</span>
                ))}
              </div>
            )}

            <div>
              <div className="mb-2 text-xs font-semibold text-kumo-strong">生命周期事件</div>
              {loading ? (
                <div className="flex items-center justify-center py-4"><Loader size={20} className="text-kumo-info" /></div>
              ) : events.length === 0 ? (
                <Empty size="sm" title="暂无事件" description="资产变更后会记录在这里" />
              ) : (
                <div className="space-y-1">
                  {events.map(event => (
                    <div key={event.id} className="flex items-center justify-between gap-3 rounded-md px-2 py-1.5 hover:bg-kumo-recessed/25">
                      <span className="text-xs text-kumo-strong">{EVENT_LABEL[event.event_type] || event.event_type}</span>
                      <span className="truncate text-[11px] text-kumo-subtle">{event.detail || ''}</span>
                      <span className="whitespace-nowrap text-[11px] text-kumo-subtle">{event.created_at || ''}</span>
                    </div>
                  ))}
                </div>
              )}
            </div>
          </div>
        </LayerDialog.Body>
      </LayerDialog.Content>
    </LayerDialog.Root>
  );
}
