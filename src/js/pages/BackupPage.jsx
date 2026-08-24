import React, { useEffect, useMemo, useRef, useState } from 'react';
import { Button } from '@cloudflare/kumo/components/button';
import { Input } from '@cloudflare/kumo/components/input';
import { Select } from '@cloudflare/kumo/components/select';
import { Collapsible, Toolbar } from '@cloudflare/kumo';
import { Table } from '@cloudflare/kumo/components/table';
import { Badge } from '@cloudflare/kumo/components/badge';
import { toast } from '../modules/toast.js';
import { dialog } from '../modules/dialog.js';
import { useConfirmPress } from '../hooks/useConfirmPress.js';
import { AppTable, SectionCard } from '../components/ui/AppPrimitives.jsx';
import { Clock, Database, Download, Play, RefreshCw, Save, Trash, Upload } from '../components/Icons.jsx';

const PROVIDERS = [
  { value: 'local', label: '本地目录' },
  { value: 'oss', label: '阿里云 OSS' },
  { value: 'cos', label: '腾讯云 COS' },
  { value: 's3', label: 'S3 / R2' },
];

const DEFAULT_CONFIG = { provider: 'local', local_dir: '', cron: '', endpoint: '', bucket: '', access_key_id: '', access_key_secret: '', max_records: 0 };

const BACKUP_RECORD_COLUMNS = [
  { id: 'file', role: 'primary', grow: 1, minWidth: 200 },
  { id: 'size', role: 'number', grow: 1, minWidth: 200, align: 'center' },
  { id: 'createdAt', role: 'datetime', grow: 1, minWidth: 200, align: 'center' },
  { id: 'actions', role: 'actions-lg', width: 200, maxWidth: 220 },
];

const SCHEDULE_TYPES = [
  { value: 'off', label: '关闭自动备份' },
  { value: 'daily', label: '每天' },
  { value: 'weekly', label: '每周' },
  { value: 'monthly', label: '每月' },
  { value: 'custom', label: '高级 Cron' },
];

const WEEKDAY_OPTIONS = [
  { value: '1', label: '周一' },
  { value: '2', label: '周二' },
  { value: '3', label: '周三' },
  { value: '4', label: '周四' },
  { value: '5', label: '周五' },
  { value: '6', label: '周六' },
  { value: '0', label: '周日' },
];

const MONTH_DAY_OPTIONS = Array.from({ length: 28 }, (_, index) => ({ value: String(index + 1), label: `${index + 1} 日` }));

function authHeaders() {
  return { 'Content-Type': 'application/json' };
}

function formatTime(value) {
  return value ? new Date(Number(value) * 1000).toLocaleString('zh-CN', { hour12: false }) : '-';
}

function formatSize(value) {
  if (!value) return '0 B';
  if (value < 1024 * 1024) return `${(value / 1024).toFixed(1)} KB`;
  return `${(value / 1024 / 1024).toFixed(1)} MB`;
}

function parseSchedule(cron = '') {
  const value = String(cron || '').trim();
  if (!value) return { type: 'off', hour: '03', minute: '00', weekday: '1', day: '1', custom: '' };
  const parts = value.split(/\s+/);
  if (parts.length !== 5) return { type: 'custom', hour: '03', minute: '00', weekday: '1', day: '1', custom: value };
  const [minute, hour, day, month, weekday] = parts;
  if (day === '*' && month === '*' && weekday === '*') return { type: 'daily', hour: padTime(hour, 23), minute: padTime(minute, 59), weekday: '1', day: '1', custom: value };
  if (day === '*' && month === '*' && weekday !== '*') return { type: 'weekly', hour: padTime(hour, 23), minute: padTime(minute, 59), weekday, day: '1', custom: value };
  if (month === '*' && weekday === '*') return { type: 'monthly', hour: padTime(hour, 23), minute: padTime(minute, 59), weekday: '1', day, custom: value };
  return { type: 'custom', hour: '03', minute: '00', weekday: '1', day: '1', custom: value };
}

function buildSchedule(schedule) {
  if (schedule.type === 'off') return '';
  if (schedule.type === 'custom') return schedule.custom.trim();
  const hour = clampNumber(schedule.hour, 0, 23);
  const minute = clampNumber(schedule.minute, 0, 59);
  if (schedule.type === 'weekly') return `${minute} ${hour} * * ${clampNumber(schedule.weekday, 0, 6)}`;
  if (schedule.type === 'monthly') return `${minute} ${hour} ${clampNumber(schedule.day, 1, 28)} * *`;
  return `${minute} ${hour} * * *`;
}

function padTime(value, max) { return String(clampNumber(value, 0, max)).padStart(2, '0'); }

function clampNumber(value, min, max) {
  const parsed = Number.parseInt(value, 10);
  if (!Number.isFinite(parsed)) return String(min);
  return String(Math.max(min, Math.min(max, parsed)));
}

function scheduleSummary(schedule) {
  const cron = buildSchedule(schedule);
  if (!cron) return '自动备份未启用';
  const time = `${String(clampNumber(schedule.hour, 0, 23)).padStart(2, '0')}:${String(clampNumber(schedule.minute, 0, 59)).padStart(2, '0')}`;
  if (schedule.type === 'daily') return `每天 ${time} 自动备份`;
  if (schedule.type === 'weekly') return `每${WEEKDAY_OPTIONS.find((item) => item.value === String(schedule.weekday))?.label || '周一'} ${time} 自动备份`;
  if (schedule.type === 'monthly') return `每月 ${clampNumber(schedule.day, 1, 28)} 日 ${time} 自动备份`;
  return `Cron: ${cron}`;
}

export function BackupPanel({ embedded = false } = {}) {
  const { isArmed, confirmPress } = useConfirmPress();
  const [config, setConfig] = useState(DEFAULT_CONFIG);
  const [schedule, setSchedule] = useState(parseSchedule(''));
  const [records, setRecords] = useState([]);
  const [loading, setLoading] = useState(false);
  const [running, setRunning] = useState(false);

  const cloudMode = useMemo(() => config.provider !== 'local', [config.provider]);

  const load = async () => {
    setLoading(true);
    try {
      const [cfgRes, recRes] = await Promise.all([
        fetch('/api/backup/configs', { headers: authHeaders() }),
        fetch('/api/backup/records', { headers: authHeaders() }),
      ]);
      const [cfg, rec] = await Promise.all([cfgRes.json(), recRes.json()]);
      if (cfg.success) {
        const nextConfig = { ...DEFAULT_CONFIG, ...(cfg.data || {}) };
        setConfig(nextConfig);
        setSchedule(parseSchedule(nextConfig.cron));
      }
      if (rec.success) setRecords(Array.isArray(rec.data) ? rec.data : []);
    } catch {
      toast.error('载入备份中心失败');
    } finally {
      setLoading(false);
    }
  };

  useEffect(() => { load(); }, []);

  const fileInputRef = useRef(null);

  const save = async (override) => {
    const payload = override || { ...config, cron: buildSchedule(schedule) };
    try {
      const res = await fetch('/api/backup/configs', { method: 'POST', headers: authHeaders(), body: JSON.stringify(payload) });
      const data = await res.json();
      if (!data.success) { toast.error(data.error || '保存失败'); return false; }
      const nextConfig = { ...DEFAULT_CONFIG, ...(data.data || {}) };
      setConfig(nextConfig);
      setSchedule(parseSchedule(nextConfig.cron));
      toast.success('备份配置已保存');
      return true;
    } catch (error) {
      toast.error(error?.message || '保存失败，请检查网络后重试');
      return false;
    }
  };

  const exportConfig = () => {
    const payload = { ...config };
    const blob = new Blob([JSON.stringify(payload, null, 2)], { type: 'application/json' });
    const url = URL.createObjectURL(blob);
    const anchor = document.createElement('a');
    anchor.href = url;
    anchor.download = `api-monitor-backup-config-${new Date().toISOString().slice(0, 10)}.json`;
    document.body.appendChild(anchor);
    anchor.click();
    anchor.remove();
    URL.revokeObjectURL(url);
    toast.success('备份配置已导出（包含云存储密钥，请注意保管）');
  };

  const importConfig = async (event) => {
    const file = event.target.files?.[0];
    event.target.value = '';
    if (!file) return;
    try {
      const raw = JSON.parse(await file.text());
      if (!raw || typeof raw !== 'object' || Array.isArray(raw) || typeof raw.provider !== 'string' || !['local', 'oss', 'cos', 's3'].includes(raw.provider)) {
        throw new Error('unsupported backup config file');
      }
      const next = { ...DEFAULT_CONFIG, ...raw };
      setConfig(next);
      setSchedule(parseSchedule(next.cron || ''));
      const ok = await save(next);
      if (ok) toast.success('备份配置已导入并保存');
    } catch {
      toast.error('导入失败：文件不是有效的备份配置');
    }
  };

  const run = async () => {
    setRunning(true);
    try {
      const res = await fetch('/api/backup/run', { method: 'POST', headers: authHeaders() });
      const data = await res.json();
      if (!data.success) throw new Error(data.error || '备份失败');
      toast.success('备份已完成');
      await load();
    } catch (error) {
      toast.error(error.message || '备份失败');
    } finally {
      setRunning(false);
    }
  };

  const remove = async (record) => {
    if (!confirmPress(`backup-record:${record.id}`, `删除备份「${record.file_name}」`)) return;
    const res = await fetch(`/api/backup/records/${encodeURIComponent(record.id)}`, { method: 'DELETE', headers: authHeaders() });
    const data = await res.json();
    if (!data.success) return toast.error(data.error || '删除失败');
    await load();
  };

  const restore = async (record) => {
    if (!(await dialog.confirm(`确认从 ${record.file_name} 恢复？当前数据库和文件柜会被覆盖。`))) return;
    const res = await fetch('/api/backup/restore', {
      method: 'POST',
      headers: authHeaders(),
      body: JSON.stringify({ id: record.id, confirm: 'RESTORE' }),
    });
    const data = await res.json();
    if (!data.success) return toast.error(data.error || '恢复失败');
    toast.success('备份已恢复');
  };

  return (
    <div className="flex w-full min-w-0 flex-col gap-3 cq-sm:gap-4">
      {!embedded && (
        <section className="grid gap-3">
          <h1 className="text-lg font-semibold text-kumo-strong">备份中心</h1>
        </section>
      )}

      <section className="grid items-start gap-3">
        <SectionCard
          title="自动备份配置"
          description={scheduleSummary(schedule)}
          icon={<Database className="h-4 w-4 text-brand" />}
          actions={(
            <>
              <Input ref={fileInputRef} type="file" accept=".json,application/json" aria-label="导入备份配置" className="hidden" onChange={importConfig} />
              <Toolbar size="sm" aria-label="导出导入配置" className="shrink-0">
                <Toolbar.Button onClick={exportConfig} aria-label="导出配置" title="导出备份配置" icon={<Upload className="h-3.5 w-3.5" />}>
                  <span className="hidden cq-sm:inline">导出</span>
                </Toolbar.Button>
                <Toolbar.Button onClick={() => fileInputRef.current?.click()} aria-label="导入配置" title="导入备份配置" icon={<Download className="h-3.5 w-3.5" />}>
                  <span className="hidden cq-sm:inline">导入</span>
                </Toolbar.Button>
              </Toolbar>
              <Button size="sm" variant="secondary" onClick={load} disabled={loading}><RefreshCw className="h-3.5 w-3.5" />刷新备份</Button>
              <Button size="sm" variant="primary" onClick={run} disabled={running}><Play className="h-3.5 w-3.5" />立即备份</Button>
            </>
          )}
          bodyClassName="space-y-3"
        >
          <div className="space-y-3">
            <Select size="sm" label="存储渠道" className="w-full" value={config.provider} onValueChange={(value) => setConfig((prev) => ({ ...prev, provider: value }))} items={PROVIDERS} />
            {cloudMode && (
              <Collapsible.Root>
                <Collapsible.DefaultTrigger>渠道配置</Collapsible.DefaultTrigger>
                <Collapsible.DefaultPanel className="flex flex-col gap-3 pt-2">
                  <Input size="sm" label="服务端点" value={config.endpoint || ''} onChange={(event) => setConfig((prev) => ({ ...prev, endpoint: event.target.value }))} />
                  <Input size="sm" label="存储桶" value={config.bucket || ''} onChange={(event) => setConfig((prev) => ({ ...prev, bucket: event.target.value }))} />
                  <Input size="sm" label="访问密钥 ID" value={config.access_key_id || ''} onChange={(event) => setConfig((prev) => ({ ...prev, access_key_id: event.target.value }))} />
                  <Input size="sm" type="text" label="访问密钥 Secret" value={config.access_key_secret || ''} onChange={(event) => setConfig((prev) => ({ ...prev, access_key_secret: event.target.value }))} autoComplete="off" data-1p-ignore data-lpignore="true" data-bwignore="true" data-form-type="other" spellCheck={false} />
                </Collapsible.DefaultPanel>
              </Collapsible.Root>
            )}
            <Input size="sm" label="本地目录" value={config.local_dir || ''} onChange={(event) => setConfig((prev) => ({ ...prev, local_dir: event.target.value }))} />
            <Input size="sm" type="number" min="0" label="最大保留数量（0=不限制）" value={config.max_records ?? 0} onChange={(event) => { const parsed = Number.parseInt(event.target.value, 10); setConfig((prev) => ({ ...prev, max_records: Number.isFinite(parsed) && parsed > 0 ? parsed : 0 })); }} />
            <div className="grid gap-2 rounded-md border border-kumo-line p-3">
              <div className="flex items-center gap-2 text-xs font-semibold text-kumo-strong"><Clock className="h-3.5 w-3.5" />自动备份计划</div>
              <Select size="sm" className="w-full" aria-label="自动备份频率" value={schedule.type} onValueChange={(value) => setSchedule((prev) => ({ ...prev, type: value }))} items={SCHEDULE_TYPES} />
              {['daily', 'weekly', 'monthly'].includes(schedule.type) && (
                <div className="grid gap-2 cq-sm:grid-cols-2">
                  <Input size="sm" type="number" label="小时" min="0" max="23" value={schedule.hour} onChange={(event) => setSchedule((prev) => ({ ...prev, hour: event.target.value }))} />
                  <Input size="sm" type="number" label="分钟" min="0" max="59" value={schedule.minute} onChange={(event) => setSchedule((prev) => ({ ...prev, minute: event.target.value }))} />
                  {schedule.type === 'weekly' && <Select size="sm" label="星期" value={String(schedule.weekday)} onValueChange={(value) => setSchedule((prev) => ({ ...prev, weekday: value }))} items={WEEKDAY_OPTIONS} />}
                  {schedule.type === 'monthly' && <Select size="sm" label="每月日期" value={String(schedule.day)} onValueChange={(value) => setSchedule((prev) => ({ ...prev, day: value }))} items={MONTH_DAY_OPTIONS} />}
                </div>
              )}
{schedule.type === 'custom' && <Input size="sm" label="Cron 表达式" value={schedule.custom} onChange={(event) => setSchedule((prev) => ({ ...prev, custom: event.target.value }))} />}
            </div>
            <Button size="sm" variant="primary" onClick={() => save()}><Save className="h-3.5 w-3.5" />保存配置</Button>
          </div>
        </SectionCard>

        <SectionCard
          title="备份历史"
          icon={<Clock className="h-4 w-4 text-brand" />}
          bodyPadding="none"
        >
          {records.length === 0 ? (
            <div className="flex min-h-40 flex-col items-center justify-center gap-2 bg-kumo-control px-6 py-8 text-center text-kumo-default">
              <Database className="h-8 w-8 text-kumo-inactive" />
              <div className="text-base font-semibold text-kumo-strong">暂无备份</div>
              <div className="max-w-96 text-xs text-kumo-subtle">先执行一次备份</div>
            </div>
          ) : (
            <div className="max-h-80 overflow-auto">
            <AppTable tableId="backup-records" columns={BACKUP_RECORD_COLUMNS}>
              <Table.Header><Table.Row><Table.Head>文件</Table.Head><Table.Head style={{ textAlign: 'center' }}>大小</Table.Head><Table.Head style={{ textAlign: 'center' }}>时间</Table.Head><Table.Head className="app-table-action">操作</Table.Head></Table.Row></Table.Header>
              <Table.Body>
                {records.map((record) => (
                  <Table.Row key={record.id}>
                    <Table.Cell><div className="truncate font-mono text-xs text-kumo-strong">{record.file_name}</div><div className="mt-1 flex gap-1"><Badge variant="orange">本地</Badge>{record.remote_url && <Badge variant="teal" title={record.remote_url}>云端</Badge>}</div></Table.Cell>
                    <Table.Cell style={{ textAlign: 'center' }} className="text-xs">{formatSize(record.size)}</Table.Cell>
                    <Table.Cell style={{ textAlign: 'center' }} className="text-xs text-kumo-subtle">{formatTime(record.created_at)}</Table.Cell>
                    <Table.Cell><div className="flex justify-center gap-1"><Button size="sm" variant="secondary" onClick={() => { window.location.href = `/api/backup/records/${encodeURIComponent(record.id)}/download`; }}><Download className="h-3.5 w-3.5" />下载</Button><Button size="sm" variant="secondary" onClick={() => restore(record)}><RefreshCw className="h-3.5 w-3.5" />恢复</Button><Button size="sm" variant={isArmed(`backup-record:${record.id}`) ? 'destructive' : 'secondary-destructive'} onClick={() => remove(record)}><Trash className="h-3.5 w-3.5" />删除</Button></div></Table.Cell>
                  </Table.Row>
                ))}
              </Table.Body>
            </AppTable>
            </div>
          )}
        </SectionCard>
      </section>
    </div>
  );
}

export default function BackupPage() {
  return <BackupPanel />;
}
