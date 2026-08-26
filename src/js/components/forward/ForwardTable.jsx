import React, { useCallback, useEffect, useState } from 'react';
import { Badge, Button, ClipboardText, Select, Table } from '@cloudflare/kumo';
import { Input } from '@cloudflare/kumo/components/input';
import { AnimatedCollapse } from '../AnimatedCollapse.jsx';
import { STATUS_COLORS, STATUS_LABELS, TRANSPORT_LABELS } from './useMeshLayout.js';

const FORWARD_API = '/api/server/forward';

const STATUS_BADGE_VARIANT = {
  running: 'success',
  deploying: 'info',
  failed: 'error',
  disconnected: 'warning',
  stopped: 'neutral',
  pending: 'neutral',
};

const TRANSPORT_BADGE_VARIANT = {
  cloudflare_tunnel: 'blue',
  tcp_relay: 'purple',
  p2p: 'neutral',
};

const TRANSPORT_FILTER_ITEMS = [
  { value: '', label: '全部传输方式' },
  { value: 'cloudflare_tunnel', label: 'CF Tunnel' },
  { value: 'tcp_relay', label: 'TCP 中继' },
  { value: 'p2p', label: 'P2P' },
];

const STATUS_FILTER_ITEMS = [
  { value: '', label: '全部状态' },
  { value: 'running', label: '运行中' },
  { value: 'deploying', label: '部署中' },
  { value: 'stopped', label: '已停止' },
  { value: 'failed', label: '失败' },
  { value: 'disconnected', label: '已断开' },
];

const TARGET_HEALTH_META = {
  healthy: { variant: 'success', label: '健康' },
  unhealthy: { variant: 'error', label: '故障' },
  unknown: { variant: 'neutral', label: '未知' },
};

export default function ForwardTable({ forwards, deploying, acting, onEdit, onDeploy, onStop, onStart, onDelete, deleteConfirmActive, isDeleting, onCreate }) {
  const [filterTransport, setFilterTransport] = useState('');
  const [filterStatus, setFilterStatus] = useState('');
  const [search, setSearch] = useState('');
  const [expandedId, setExpandedId] = useState(null);
  const [targets, setTargets] = useState([]);
  const [targetsLoading, setTargetsLoading] = useState(false);

  const loadTargets = useCallback(async (forwardId) => {
    if (!forwardId) { setTargets([]); return; }
    setTargetsLoading(true);
    try {
      const res = await fetch(`${FORWARD_API}/${forwardId}/targets`);
      const json = await res.json();
      if (json.success) setTargets(json.data || []);
    } catch (e) {
      setTargets([]);
    } finally {
      setTargetsLoading(false);
    }
  }, []);

  useEffect(() => {
    loadTargets(expandedId);
  }, [expandedId, loadTargets]);

  const filtered = forwards.filter((row) => {
    if (filterTransport && row.transport !== filterTransport) return false;
    if (filterStatus && row.apply_status !== filterStatus) return false;
    if (search) {
      const q = search.trim().toLowerCase();
      if (!(row.name || '').toLowerCase().includes(q) && !(row.server_name || '').toLowerCase().includes(q)) return false;
    }
    return true;
  });

  const columns = [
    {
      key: 'name',
      label: '规则名称',
      primary: true,
      render: (val, row) => (
        <div className="flex flex-col gap-0.5">
          <span className="truncate font-medium text-kumo-strong">{val}</span>
          <span className="truncate text-xs text-kumo-text-secondary">
            {row.server_name || row.server_id} · {(row.protocol || 'tcp').toUpperCase()}
          </span>
          {row.failover_enabled && row.failover_current_server_id && (
            <span className="truncate text-xs text-kumo-text-warning">
              已切换到 {row.failover_current_server_id}
              {row.failover_switched_at ? ` ${row.failover_switched_at}` : ''}
            </span>
          )}
        </div>
      ),
    },
    {
      key: 'transport',
      label: '传输方式',
      width: 110,
      render: (val) => (
        <Badge variant={TRANSPORT_BADGE_VARIANT[val] || 'neutral'} size="sm">{TRANSPORT_LABELS[val] || val}</Badge>
      ),
    },
    {
      key: 'access_url',
      label: '访问地址',
      content: true,
      render: (val) => val ? (
        <ClipboardText size="sm" text={val} tooltip={{ text: '复制', copiedText: '已复制', side: 'top' }} />
      ) : <span className="text-xs text-kumo-text-secondary">部署后显示</span>,
    },
    {
      key: 'apply_status',
      label: '状态',
      width: 96,
      render: (_, row) => (
        <span title={row.last_error || undefined}>
          <Badge variant={STATUS_BADGE_VARIANT[row.apply_status] || 'neutral'} appearance="dot" size="sm">
            {STATUS_LABELS[row.apply_status] || row.apply_status}
          </Badge>
        </span>
      ),
    },
    {
      key: 'failover',
      label: '故障转移',
      width: 120,
      render: (_, row) => {
        if (!row.failover_enabled) return <span className="text-xs text-kumo-text-secondary">未启用</span>;
        if (row.failover_current_server_id) return (
          <Badge variant="warning" appearance="dot" size="sm">
            已切换 → {row.failover_current_server_id}
          </Badge>
        );
        return <Badge variant="success" appearance="dot" size="sm">正常</Badge>;
      },
    },
    {
      key: 'connector_count',
      label: '连接',
      width: 56,
      render: (val) => <span className="text-xs tabular-nums text-kumo-default">{val || 0}</span>,
    },
    {
      key: 'actions',
      label: '操作',
      width: 280,
      render: (_, row) => (
        <div className="actions-xl flex items-center gap-1">
          <Button size="sm" variant="outline" onClick={() => onEdit(row)}>编辑</Button>
          {row.apply_status === 'running' ? (
            <Button size="sm" variant="outline" onClick={() => onStop(row.id)} disabled={acting?.has(`stop:${row.id}`)}>
              {acting?.has(`stop:${row.id}`) ? '停止中' : '停止'}
            </Button>
          ) : row.apply_status !== 'deploying' ? (
            <Button size="sm" variant="outline" onClick={() => onStart(row.id)} disabled={acting?.has(`start:${row.id}`)}>
              {acting?.has(`start:${row.id}`) ? '启动中' : '启动'}
            </Button>
          ) : null}
          <Button size="sm" variant="outline" onClick={() => onDeploy(row.id)} disabled={deploying.has(row.id)}>
            {deploying.has(row.id) ? '部署中' : '部署'}
          </Button>
          <Button
            size="sm"
            variant={deleteConfirmActive?.(`fwd:${row.id}`) ? 'destructive' : 'secondary-destructive'}
            disabled={isDeleting}
            onClick={() => onDelete(row)}
          >
            {deleteConfirmActive?.(`fwd:${row.id}`) ? '确认删除' : '删除'}
          </Button>
        </div>
      ),
    },
  ];

  return (
    <>
      <div className="flex items-center gap-2">
        <Select
          size="sm"
          value={filterTransport}
          onValueChange={setFilterTransport}
          items={TRANSPORT_FILTER_ITEMS}
          aria-label="传输方式"
          className="w-32"
        />
        <Select
          size="sm"
          value={filterStatus}
          onValueChange={setFilterStatus}
          items={STATUS_FILTER_ITEMS}
          aria-label="状态"
          className="w-28"
        />
        <Input size="sm" placeholder="搜索规则名称..." value={search} onChange={(e) => setSearch(e.target.value)} className="max-w-xs flex-1" />
      </div>

      {filtered.length === 0 ? (
        <div className="flex flex-col items-center justify-center py-20 text-kumo-text-secondary">
          <div className="mb-4 text-5xl opacity-30">↔</div>
          {forwards.length === 0 ? (
            <>
              <p className="mb-2">还没有转发规则</p>
              <p className="mb-4 text-sm">创建一条转发规则，将内网服务暴露到公网</p>
              <Button size="sm" onClick={onCreate}>+ 创建第一条转发规则</Button>
            </>
          ) : (
            <p className="text-sm">没有匹配的转发规则，试试调整筛选条件</p>
          )}
        </div>
      ) : (
        <Table
          columns={columns}
          data={filtered}
          onRowClick={(row) => setExpandedId(expandedId === row.id ? null : row.id)}
        />
      )}

      {filtered.map((row) => (
        <AnimatedCollapse key={row.id} className={expandedId === row.id ? '' : 'hidden'}>
          <div className="mt-2 rounded-lg border border-kumo-line bg-kumo-elevated p-4">
            <div className="grid grid-cols-2 gap-x-4 gap-y-3 text-sm">
              <div className="min-w-0"><span className="text-kumo-text-secondary">规则 ID: </span><ClipboardText size="sm" text={row.id} /></div>
              <div className="min-w-0"><span className="text-kumo-text-secondary">访问地址: </span><ClipboardText size="sm" text={row.access_url || '-'} /></div>
              <div><span className="text-kumo-text-secondary">本地服务: </span>{row.local_host}:{row.local_port}</div>
              <div><span className="text-kumo-text-secondary">传输方式: </span>{TRANSPORT_LABELS[row.transport] || row.transport}</div>
              <div><span className="text-kumo-text-secondary">创建时间: </span>{row.created_at}</div>
              <div><span className="text-kumo-text-secondary">更新时间: </span>{row.updated_at}</div>
              {row.last_stage && <div><span className="text-kumo-text-secondary">最后阶段: </span>{row.last_stage}</div>}
              {row.access_mode && <div><span className="text-kumo-text-secondary">访问控制: </span>{row.access_mode}</div>}
            </div>
            {row.last_error && (
              <div className="mt-2 text-sm text-kumo-text-danger">{row.last_error}</div>
            )}
            {row.failover_enabled && (
              <div className="mt-3 rounded-lg bg-kumo-fill px-3 py-2">
                <div className="flex items-center gap-2 text-xs font-medium text-kumo-strong">
                  <span className={`h-2 w-2 rounded-full`} style={{ background: STATUS_COLORS[row.apply_status] }} />
                  故障转移
                </div>
                <div className="mt-1 grid grid-cols-2 gap-x-4 gap-y-1 text-xs text-kumo-text-secondary">
                  <div>当前服务: {row.failover_current_server_id || row.server_name || row.server_id}</div>
                  {row.failover_switched_at && <div>切换时间: {row.failover_switched_at}</div>}
                  {row.failover_reason && <div className="col-span-2">原因: {row.failover_reason}</div>}
                </div>
                <div className="mt-2 flex flex-col gap-1">
                  {targetsLoading ? (
                    <p className="text-xs text-kumo-text-secondary">加载备用主机…</p>
                  ) : targets.length === 0 ? (
                    <p className="text-xs text-kumo-text-secondary">暂无备用主机</p>
                  ) : (
                    targets.map((t) => {
                      const healthMeta = TARGET_HEALTH_META[t.health_status] || TARGET_HEALTH_META.unknown;
                      return (
                        <div key={t.id} className="flex items-center gap-2 text-xs text-kumo-default">
                          <span className="min-w-0 truncate">{t.server_name || t.server_id}</span>
                          <span className="shrink-0 text-kumo-text-secondary">优先级 {t.priority}</span>
                          <Badge variant={healthMeta.variant} size="sm">{healthMeta.label}</Badge>
                        </div>
                      );
                    })
                  )}
                </div>
              </div>
            )}
          </div>
        </AnimatedCollapse>
      ))}
    </>
  );
}
