import React, { useEffect, useMemo, useState } from 'react';
import { Button } from '@cloudflare/kumo/components/button';
import { Input } from '@cloudflare/kumo/components/input';
import { Select } from '@cloudflare/kumo/components/select';
import { Table } from '@cloudflare/kumo/components/table';
import { Badge } from '@cloudflare/kumo/components/badge';
import { toast } from '../modules/toast.js';
import { Clock, Database, Download, Play, RefreshCw, Save, Trash } from '../components/Icons.jsx';

const PROVIDERS = [
  { value: 'local', label: '本地目录' },
  { value: 'oss', label: '阿里云 OSS' },
  { value: 'cos', label: '腾讯云 COS' },
  { value: 's3', label: 'S3 / R2' },
];

const DEFAULT_CONFIG = { provider: 'local', local_dir: '', cron: '', endpoint: '', bucket: '', access_key_id: '', access_key_secret: '' };

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
  return { 'Content-Type': 'application/json', 'x-admin-password': localStorage.getItem('admin_password') || '' };
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

  const save = async () => {
    const payload = { ...config, cron: buildSchedule(schedule) };
    const res = await fetch('/api/backup/configs', { method: 'POST', headers: authHeaders(), body: JSON.stringify(payload) });
    const data = await res.json();
    if (!data.success) return toast.error(data.error || '保存失败');
    const nextConfig = { ...DEFAULT_CONFIG, ...(data.data || {}) };
    setConfig(nextConfig);
    setSchedule(parseSchedule(nextConfig.cron));
    toast.success('备份配置已保存');
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
    if (!window.confirm(`删除备份 ${record.file_name}？`)) return;
    const res = await fetch(`/api/backup/records/${encodeURIComponent(record.id)}`, { method: 'DELETE', headers: authHeaders() });
    const data = await res.json();
    if (!data.success) return toast.error(data.error || '删除失败');
    await load();
  };

  const restore = async (record) => {
    if (!window.confirm(`确认从 ${record.file_name} 恢复？当前数据库和文件柜会被覆盖。`)) return;
    const res = await fetch('/api/backup/restore', {
      method: 'POST',
      headers: authHeaders(),
      body: JSON.stringify({ id: record.id, confirm: 'RESTORE' }),
    });
    const data = await res.json();
    if (!data.success) return toast.error(data.error || '恢复失败');
    toast.success('备份已恢复，建议重启服务后继续使用');
  };

  return (
    <div className="space-y-4">
      <section className="grid gap-3 lg:grid-cols-[minmax(0,1fr)_auto]">
        {!embedded && (
          <div>
            <h1 className="text-lg font-semibold text-kumo-strong">备份中心</h1>
            <p className="mt-1 text-xs text-kumo-subtle">打包 SQLite 数据库与文件柜目录，支持本地保留和云端同步。</p>
          </div>
        )}
        <div className={embedded ? 'flex flex-wrap gap-2' : 'flex flex-wrap gap-2 lg:justify-end'}>
          <Button size="sm" variant="secondary" onClick={load} disabled={loading}><RefreshCw className="h-3.5 w-3.5" />刷新备份</Button>
          <Button size="sm" variant="primary" onClick={run} disabled={running}><Play className="h-3.5 w-3.5" />立即完整备份</Button>
        </div>
      </section>

      <section className="grid gap-3">
        <div className="rounded-md border border-kumo-line p-3">
          <div className="mb-3 flex items-center gap-2 text-sm font-semibold text-kumo-strong"><Database className="h-4 w-4" />自动备份配置</div>
          <div className="space-y-3">
            <Select size="sm" label="存储渠道" className="w-full" value={config.provider} onValueChange={(value) => setConfig((prev) => ({ ...prev, provider: value }))} items={PROVIDERS} />
            <Input size="sm" label="本地目录" value={config.local_dir || ''} onChange={(event) => setConfig((prev) => ({ ...prev, local_dir: event.target.value }))} />
            <div className="grid gap-2 rounded-md border border-kumo-line p-3">
              <div className="flex items-center gap-2 text-xs font-semibold text-kumo-strong"><Clock className="h-3.5 w-3.5" />自动备份计划</div>
              <Select size="sm" label="频率" className="w-full" value={schedule.type} onValueChange={(value) => setSchedule((prev) => ({ ...prev, type: value }))} items={SCHEDULE_TYPES} />
              {['daily', 'weekly', 'monthly'].includes(schedule.type) && (
                <div className="grid gap-2 sm:grid-cols-2">
                  <Input size="sm" type="number" label="小时" min="0" max="23" value={schedule.hour} onChange={(event) => setSchedule((prev) => ({ ...prev, hour: event.target.value }))} />
                  <Input size="sm" type="number" label="分钟" min="0" max="59" value={schedule.minute} onChange={(event) => setSchedule((prev) => ({ ...prev, minute: event.target.value }))} />
                  {schedule.type === 'weekly' && <Select size="sm" label="星期" value={String(schedule.weekday)} onValueChange={(value) => setSchedule((prev) => ({ ...prev, weekday: value }))} items={WEEKDAY_OPTIONS} />}
                  {schedule.type === 'monthly' && <Select size="sm" label="每月日期" value={String(schedule.day)} onValueChange={(value) => setSchedule((prev) => ({ ...prev, day: value }))} items={MONTH_DAY_OPTIONS} />}
                </div>
              )}
              {schedule.type === 'custom' && <Input size="sm" label="Cron 表达式" value={schedule.custom} onChange={(event) => setSchedule((prev) => ({ ...prev, custom: event.target.value }))} />}
              <div className="text-[11px] text-kumo-subtle">{scheduleSummary(schedule)}</div>
            </div>
            {cloudMode && (
              <>
                <Input size="sm" label="Endpoint" value={config.endpoint || ''} onChange={(event) => setConfig((prev) => ({ ...prev, endpoint: event.target.value }))} />
                <Input size="sm" label="Bucket" value={config.bucket || ''} onChange={(event) => setConfig((prev) => ({ ...prev, bucket: event.target.value }))} />
                <Input size="sm" label="AccessKeyID" value={config.access_key_id || ''} onChange={(event) => setConfig((prev) => ({ ...prev, access_key_id: event.target.value }))} />
                <Input size="sm" type="password" label="AccessKeySecret" value={config.access_key_secret || ''} onChange={(event) => setConfig((prev) => ({ ...prev, access_key_secret: event.target.value }))} />
              </>
            )}
            <Button size="sm" variant="primary" onClick={save}><Save className="h-3.5 w-3.5" />保存配置</Button>
          </div>
        </div>

        <div className="overflow-hidden rounded-md border border-kumo-line">
          <div className="border-b border-kumo-line px-3 py-2 text-sm font-semibold text-kumo-strong">备份历史</div>
          {records.length === 0 ? (
            <div className="flex min-h-40 flex-col items-center justify-center gap-2 bg-kumo-control px-6 py-8 text-center text-kumo-default">
              <Database className="h-8 w-8 text-kumo-inactive" />
              <div className="text-base font-semibold text-kumo-strong">暂无备份</div>
              <div className="max-w-96 text-xs text-kumo-subtle">点击立即备份后会生成可下载的 zip 包。</div>
            </div>
          ) : (
            <div className="max-h-80 overflow-auto">
            <Table layout="fixed" className={embedded ? 'min-w-[560px]' : 'min-w-[720px]'}>
              <Table.Header><Table.Row><Table.Head>文件</Table.Head><Table.Head>大小</Table.Head><Table.Head>时间</Table.Head><Table.Head>操作</Table.Head></Table.Row></Table.Header>
              <Table.Body>
                {records.map((record) => (
                  <Table.Row key={record.id}>
                    <Table.Cell><div className="truncate font-mono text-xs text-kumo-strong">{record.file_name}</div><div className="mt-1 flex gap-1"><Badge variant="secondary">本地</Badge>{record.remote_url && <Badge variant="info">云端</Badge>}</div></Table.Cell>
                    <Table.Cell className="text-xs">{formatSize(record.size)}</Table.Cell>
                    <Table.Cell className="text-xs text-kumo-subtle">{formatTime(record.created_at)}</Table.Cell>
                    <Table.Cell><div className="flex gap-1"><Button size="sm" variant="secondary" onClick={() => { window.location.href = `/api/backup/records/${encodeURIComponent(record.id)}/download`; }}><Download className="h-3.5 w-3.5" />下载</Button><Button size="sm" variant="secondary" onClick={() => restore(record)}><RefreshCw className="h-3.5 w-3.5" />恢复</Button><Button size="sm" variant="secondary-destructive" onClick={() => remove(record)}><Trash className="h-3.5 w-3.5" />删除</Button></div></Table.Cell>
                  </Table.Row>
                ))}
              </Table.Body>
            </Table>
            </div>
          )}
        </div>
      </section>
    </div>
  );
}

export default function BackupPage() {
  return <BackupPanel />;
}
