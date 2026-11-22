import { useEffect, useState } from 'react';
import { Badge } from '@cloudflare/kumo/components/badge';
import { Button } from '@cloudflare/kumo/components/button';
import { Input } from '@cloudflare/kumo/components/input';
import { Switch } from '@cloudflare/kumo/components/switch';
import { Table } from '@cloudflare/kumo';
import { AppTable, SectionCard } from '../../components/ui/AppPrimitives.jsx';
import { Play, RefreshCw, Save } from '../../components/Icons.jsx';
import { formatDateTime } from '../../modules/utils.js';

const AUTO_UPDATE_RESULT_COLUMNS = [
  { id: 'host', role: 'meta' },
  { id: 'container', role: 'primary', grow: 2 },
  { id: 'image', role: 'meta', grow: 1 },
  { id: 'status', role: 'status' },
  { id: 'message', role: 'meta', grow: 2 },
  { id: 'time', role: 'meta' },
];

const EMPTY_POLICY = {
  enabled: false,
  schedule: '0 4 * * *',
  hostSelector: '',
  containerLabel: 'auto-update=true',
  containerNames: '',
  includeCompose: false,
  maxConcurrency: 2,
  perContainerTimeoutSeconds: 600,
  notifyOnFailure: true,
};

function resultBadge(status) {
  if (status === 'updated') return { variant: 'success', label: '已更新' };
  if (status === 'failed') return { variant: 'error', label: '失败' };
  if (status === 'up-to-date') return { variant: 'neutral', label: '已最新' };
  if (status === 'skipped') return { variant: 'neutral', label: '跳过' };
  return { variant: 'neutral', label: status || '未知' };
}

function runStatusBadge(status) {
  if (status === 'success') return { variant: 'success', label: '成功' };
  if (status === 'partial') return { variant: 'warning', label: '部分成功' };
  if (status === 'failed') return { variant: 'error', label: '失败' };
  if (status === 'disabled') return { variant: 'neutral', label: '未开启' };
  if (status === 'skipped') return { variant: 'neutral', label: '无匹配主机' };
  return { variant: 'neutral', label: '未运行' };
}

export default function DockerAutoUpdatePanel({ state, busy, onSave, onRun, onRefresh }) {
  const [form, setForm] = useState(() => ({ ...EMPTY_POLICY, ...(state?.policy || {}) }));
  const [dirty, setDirty] = useState(false);

  useEffect(() => {
    if (!dirty) {
      setForm({ ...EMPTY_POLICY, ...(state?.policy || {}) });
    }
  }, [state?.policy, dirty]);

  const update = patch => {
    setForm(prev => ({ ...prev, ...patch }));
    setDirty(true);
  };

  const handleSave = async () => {
    const ok = await onSave(form);
    if (ok) setDirty(false);
  };

  const results = Array.isArray(state?.results) ? state.results : [];
  const running = Boolean(state?.running);
  const summary = state?.lastRunSummary || null;
  const lastStatusBadge = runStatusBadge(state?.lastRunStatus);

  return (
    <div className="flex w-full min-w-0 flex-col gap-3 cq-sm:gap-4">
      <SectionCard
        title="容器自动更新"
        description="按站点时区计划自动检测并更新命中策略的容器(默认关闭)"
        icon={<RefreshCw className="h-4 w-4 text-brand" />}
        actions={
          <Switch
            label={`总开关:${form.enabled ? '已开启' : '已关闭'}`}
            checked={form.enabled}
            onCheckedChange={checked => update({ enabled: checked })}
          />
        }
      >
        <div className="space-y-4">
          <div className="grid grid-cols-1 gap-x-4 gap-y-4 cq-sm:grid-cols-2">
            <Input
              size="sm"
              label="执行计划(Cron,站点时区)"
              value={form.schedule}
              placeholder="0 4 * * *"
              description="每日 4:00 执行示例;留空使用默认"
              onChange={event => update({ schedule: event.target.value })}
            />
            <Input
              size="sm"
              label="主机标签(逗号分隔)"
              value={form.hostSelector}
              placeholder="prod, edge"
              description="留空表示全部在线主机;与定时任务的标签选择一致"
              onChange={event => update({ hostSelector: event.target.value })}
            />
            <Input
              size="sm"
              label="容器标签白名单"
              value={form.containerLabel}
              placeholder="auto-update=true"
              description="仅更新带该标签的容器;留空表示不按标签过滤"
              onChange={event => update({ containerLabel: event.target.value })}
            />
            <Input
              size="sm"
              label="容器名单(逗号分隔,包含匹配)"
              value={form.containerNames}
              placeholder="web, api"
              description="填写后仅更新名称命中的容器"
              onChange={event => update({ containerNames: event.target.value })}
            />
            <Input
              size="sm"
              type="number"
              min="1"
              max="10"
              label="并发数"
              value={form.maxConcurrency}
              onChange={event => update({ maxConcurrency: Number(event.target.value) || 1 })}
            />
            <Input
              size="sm"
              type="number"
              min="60"
              max="3600"
              label="单容器超时(秒)"
              value={form.perContainerTimeoutSeconds}
              onChange={event =>
                update({ perContainerTimeoutSeconds: Number(event.target.value) || 600 })
              }
            />
          </div>
          <div className="grid grid-cols-1 gap-x-4 gap-y-3 cq-sm:grid-cols-2">
            <Switch
              label="包含 Compose 托管容器"
              checked={form.includeCompose}
              onCheckedChange={checked => update({ includeCompose: checked })}
            />
            <Switch
              label="失败时通知"
              checked={form.notifyOnFailure}
              onCheckedChange={checked => update({ notifyOnFailure: checked })}
            />
          </div>
        </div>
        <div className="mt-4 flex flex-wrap items-center justify-end gap-2 border-t border-kumo-line pt-3">
          {state?.lastRunAt && (
            <span className="mr-auto text-xs text-kumo-subtle">
              上次运行:{formatDateTime(state.lastRunAt)}
              <Badge variant={lastStatusBadge.variant} className="ml-1.5 text-xs">
                {lastStatusBadge.label}
              </Badge>
            </span>
          )}
          <Button
            size="sm"
            variant="outline"
            icon={<RefreshCw className="h-4 w-4" />}
            onClick={onRefresh}
            loading={busy}
          >
            刷新
          </Button>
          <Button
            size="sm"
            variant="secondary"
            icon={<Play className="h-4 w-4" />}
            onClick={onRun}
            disabled={running}
          >
            {running ? '运行中…' : '立即运行'}
          </Button>
          <Button
            size="sm"
            icon={<Save className="h-4 w-4" />}
            onClick={handleSave}
            loading={busy && !running}
            disabled={!dirty}
          >
            保存策略
          </Button>
        </div>
      </SectionCard>

      {summary && (
        <SectionCard title="上次运行结果" icon={<RefreshCw className="h-4 w-4 text-brand" />}>
          <div className="flex flex-wrap items-center gap-x-4 gap-y-1 text-xs text-kumo-subtle">
            <span>主机 {summary.hosts ?? 0}</span>
            <span>扫描 {summary.scanned ?? 0}</span>
            <span>已更新 {summary.updated ?? 0}</span>
            <span>已最新 {summary.upToDate ?? 0}</span>
            <span>跳过 {summary.skipped ?? 0}</span>
            <span>失败 {summary.failed ?? 0}</span>
            {summary.message ? <span className="text-kumo-strong">{summary.message}</span> : null}
          </div>
        </SectionCard>
      )}

      <SectionCard
        title="容器更新状态"
        icon={<RefreshCw className="h-4 w-4 text-brand" />}
        bodyPadding="none"
      >
        {results.length === 0 ? (
          <div className="px-4 py-6 text-center text-xs text-kumo-subtle">暂无自动更新记录</div>
        ) : (
          <AppTable
            tableId="docker-auto-update-results"
            columns={AUTO_UPDATE_RESULT_COLUMNS}
            className="w-full min-w-[720px] text-xs"
          >
            <Table.Header sticky variant="compact">
              <Table.Row className="h-8">
                <Table.Head className="!px-2.5 !py-1.5">主机</Table.Head>
                <Table.Head className="!px-2.5 !py-1.5">容器</Table.Head>
                <Table.Head className="!px-2.5 !py-1.5">镜像</Table.Head>
                <Table.Head className="!px-2 !py-1.5 text-center">状态</Table.Head>
                <Table.Head className="!px-2.5 !py-1.5">说明</Table.Head>
                <Table.Head className="!px-2.5 !py-1.5">时间</Table.Head>
              </Table.Row>
            </Table.Header>
            <Table.Body>
              {results.map((result, index) => {
                const badge = resultBadge(result.status);
                return (
                  <Table.Row
                    key={result.containerId || `${result.serverId}-${result.containerName}-${index}`}
                    className="[&>td]:align-middle"
                  >
                    <Table.Cell className="!px-2.5 !py-1.5 text-kumo-subtle">
                      {result.serverName || result.serverId}
                    </Table.Cell>
                    <Table.Cell className="!px-2.5 !py-1.5 font-medium text-kumo-strong">
                      {result.containerName}
                    </Table.Cell>
                    <Table.Cell className="!px-2.5 !py-1.5 font-mono text-kumo-subtle">
                      <span className="block max-w-[18rem] truncate" title={result.image}>
                        {result.image}
                      </span>
                    </Table.Cell>
                    <Table.Cell className="!px-2 !py-1.5 text-center">
                      <Badge variant={badge.variant} className="text-xs">
                        {badge.label}
                      </Badge>
                    </Table.Cell>
                    <Table.Cell className="!px-2.5 !py-1.5 text-kumo-subtle">
                      <span className="block max-w-[20rem] truncate" title={result.message}>
                        {result.message || '-'}
                      </span>
                    </Table.Cell>
                    <Table.Cell className="!px-2.5 !py-1.5 whitespace-nowrap text-kumo-subtle">
                      {result.updatedAt ? formatDateTime(result.updatedAt) : '-'}
                    </Table.Cell>
                  </Table.Row>
                );
              })}
            </Table.Body>
          </AppTable>
        )}
      </SectionCard>
    </div>
  );
}
