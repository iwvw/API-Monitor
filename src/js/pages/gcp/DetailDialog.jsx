import React from 'react';
import { Button } from '@cloudflare/kumo/components/button';
import { Dialog } from '@cloudflare/kumo/components/dialog';
import { KeyValueGrid, StatusBadge } from '../../components/ui/AppPrimitives.jsx';
import { Copy } from '../../components/Icons.jsx';
import { formatDate, formatGb, formatMemoryGb, formatSize, getGcpStatusTone, getVerifyStatusLabel } from './utils.jsx';

const DETAIL_TITLES = {
  instance: '实例详情',
  disk: '磁盘详情',
  firewall: '防火墙规则详情',
  address: '静态 IP 详情',
  budget: '预算详情',
  billingAccount: '计费账号详情',
  object: '对象详情',
  account: '账号详情',
};

function renderDetailRows(rows) {
  return (
    <KeyValueGrid
      columns={1}
      items={rows.filter((row) => row && (row.show ?? true)).map((row) => ({
        key: row.label,
        label: row.label,
        value: row.value ?? '-',
      }))}
    />
  );
}

function renderDetailContent(selectedDetail, onCopy) {
  if (!selectedDetail) return null;
  const { kind, data } = selectedDetail;
  if (kind === 'instance') {
    return renderDetailRows([
      { label: '名称', value: <span className="font-medium text-kumo-strong">{data.name}</span> },
      { label: '实例 ID', value: <span className="font-mono text-xs">{data.id}</span> },
      { label: '状态', value: <StatusBadge tone={getGcpStatusTone(data.state)}>{data.state || '-'}</StatusBadge> },
      { label: '机型', value: data.machineType },
      { label: '规格', value: data.guestCpus > 0 ? `${data.guestCpus} vCPU / ${formatMemoryGb(data.memoryMb)}` : '-' },
      { label: '可用区', value: data.zone },
      { label: '公网 IP', value: data.publicIp ? <span className="inline-flex items-center gap-1"><span className="font-mono text-xs">{data.publicIp}</span><Button type="button" size="sm" variant="ghost" className="h-5 w-5 p-0" onClick={() => onCopy(data.publicIp)}><Copy className="h-3 w-3" /></Button></span> : '-' },
      { label: '内网 IP', value: data.privateIp ? <span className="font-mono text-xs">{data.privateIp}</span> : '-' },
      { label: '镜像', value: data.image },
      { label: '创建时间', value: formatDate(data.creationTimestamp) },
      { label: '删除保护', value: data.deletionProtection ? '开启' : '关闭' },
      { label: '标签', value: data.labels && Object.keys(data.labels).length > 0 ? <pre className="whitespace-pre-wrap text-xs text-kumo-strong">{JSON.stringify(data.labels, null, 2)}</pre> : '无' },
      { label: '磁盘', value: (data.disks || []).length > 0 ? data.disks.map((disk) => `${disk.deviceName || disk.source || ''}（${disk.type || ''}${disk.boot ? ' · 引导盘' : ''}${disk.diskSizeGb ? ` · ${disk.diskSizeGb}GB` : ''}）`).filter(Boolean).join('、') : '无' },
    ]);
  }
  if (kind === 'disk') {
    return renderDetailRows([
      { label: '名称', value: <span className="font-medium text-kumo-strong">{data.name}</span> },
      { label: '磁盘 ID', value: <span className="font-mono text-xs">{data.id}</span> },
      { label: '可用区', value: data.zone },
      { label: '类型', value: data.type },
      { label: '大小', value: formatGb(data.sizeGb) },
      { label: '状态', value: <StatusBadge tone={getGcpStatusTone(data.status)}>{data.status || '-'}</StatusBadge> },
      { label: '创建时间', value: formatDate(data.creationTimestamp) },
      { label: '源快照', value: data.sourceSnapshot || '-' },
      { label: '挂载实例', value: (data.users || []).length > 0 ? data.users.map((user) => user.split('/').pop()).join('、') : '未挂载' },
      { label: '标签', value: data.labels && Object.keys(data.labels).length > 0 ? <pre className="whitespace-pre-wrap text-xs text-kumo-strong">{JSON.stringify(data.labels, null, 2)}</pre> : '无' },
    ]);
  }
  if (kind === 'firewall') {
    return renderDetailRows([
      { label: '名称', value: <span className="font-medium text-kumo-strong">{data.name}</span> },
      { label: '方向', value: data.direction },
      { label: '动作', value: <StatusBadge tone={data.action === 'ALLOW' ? 'success' : 'danger'}>{data.action || '-'}</StatusBadge> },
      { label: '优先级', value: data.priority },
      { label: '网络', value: data.network },
      { label: '来源网段', value: (data.sourceRanges || []).join('、') || '-' },
      { label: '目标网段', value: (data.destinationRanges || []).join('、') || '-' },
      { label: '允许规则', value: (data.allowed || []).map((rule) => `${rule.ipProtocol}${rule.ports ? `:${rule.ports.join(',')}` : ''}`).join('；') || '-' },
      { label: '拒绝规则', value: (data.denied || []).map((rule) => `${rule.ipProtocol}${rule.ports ? `:${rule.ports.join(',')}` : ''}`).join('；') || '-' },
    ]);
  }
  if (kind === 'address') {
    return renderDetailRows([
      { label: '名称', value: <span className="font-medium text-kumo-strong">{data.name}</span> },
      { label: '地址', value: <span className="font-mono text-xs">{data.address}</span> },
      { label: '区域', value: data.region },
      { label: '类型', value: data.type || '-' },
      { label: '状态', value: <StatusBadge tone={getGcpStatusTone(data.status)}>{data.status || '-'}</StatusBadge> },
      { label: '占用者', value: (data.users || []).length > 0 ? data.users.map((user) => user.split('/').pop()).join('、') : '未占用' },
    ]);
  }
  if (kind === 'budget') {
    return renderDetailRows([
      { label: '名称', value: <span className="font-medium text-kumo-strong">{data.displayName || data.name}</span> },
      { label: '预算 ID', value: <span className="font-mono text-xs">{data.name}</span> },
      { label: '金额', value: data.amount ? `${data.amount} ${data.currencyCode || ''}`.trim() : '-' },
      { label: '状态', value: <StatusBadge tone={getGcpStatusTone(data.state)}>{data.state || '-'}</StatusBadge> },
      { label: '阈值', value: (data.thresholdRules || []).map((rule) => `${rule.thresholdPercent}%${rule.spendBasis ? `（${rule.spendBasis}）` : ''}`).join('、') || '-' },
    ]);
  }
  if (kind === 'billingAccount') {
    return renderDetailRows([
      { label: 'ID', value: <span className="font-mono text-xs">{data.name}</span> },
      { label: '名称', value: <span className="font-medium text-kumo-strong">{data.displayName}</span> },
      { label: '状态', value: <StatusBadge tone={data.open ? 'success' : 'neutral'}>{data.open ? '启用' : '停用'}</StatusBadge> },
    ]);
  }
  if (kind === 'object') {
    return renderDetailRows([
      { label: '名称', value: <span className="break-all font-medium text-kumo-strong">{data.name}</span> },
      { label: '大小', value: formatSize(data.size) },
      { label: '类型', value: data.contentType || '-' },
      { label: '创建时间', value: formatDate(data.timeCreated) },
      { label: '更新时间', value: formatDate(data.updated) },
    ]);
  }
  if (kind === 'account') {
    return renderDetailRows([
      { label: '名称', value: <span className="font-medium text-kumo-strong">{data.name}</span> },
      { label: 'Service Account', value: <span className="break-all font-mono text-xs">{data.clientEmail}</span> },
      { label: '默认项目', value: data.defaultProjectId ? <span className="font-mono text-xs">{data.defaultProjectId}</span> : '-' },
      { label: '验证状态', value: <StatusBadge tone={getGcpStatusTone(data.lastVerifyStatus)}>{getVerifyStatusLabel(data.lastVerifyStatus)}</StatusBadge> },
      { label: '备注', value: data.description || '-' },
      { label: '创建时间', value: formatDate(data.createdAt) },
      { label: '更新时间', value: formatDate(data.updatedAt) },
      { label: 'SA 凭证', value: data.hasServiceAccountJson ? '已保存（加密存储）' : '未保存' },
    ]);
  }
  return null;
}

export default function DetailDialog({ selectedDetail, onClose, onCopy }) {
  const detailTitle = selectedDetail ? (DETAIL_TITLES[selectedDetail.kind] || '资源详情') : '';
  return (
    <Dialog.Root open={Boolean(selectedDetail)} onOpenChange={(open) => { if (!open) onClose(); }}>
      <Dialog className="@container !w-[min(38rem,calc(100vw-2rem))] !max-w-[min(38rem,calc(100vw-2rem))] p-6">
        <Dialog.Title className="mb-4 text-base font-semibold text-kumo-strong">{detailTitle}</Dialog.Title>
        <Dialog.Description className="sr-only">资源详情</Dialog.Description>
        <div className="max-h-[60vh] overflow-auto scrollbar-thin">
          {renderDetailContent(selectedDetail, onCopy)}
        </div>
        <div className="flex justify-end gap-2 pt-4">
          <Button type="button" size="sm" variant="secondary" onClick={onClose}>关闭</Button>
        </div>
      </Dialog>
    </Dialog.Root>
  );
}
