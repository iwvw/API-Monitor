import React, { useEffect, useState } from 'react';
import { Button } from '@cloudflare/kumo/components/button';
import { Input } from '@cloudflare/kumo/components/input';
import { Select } from '@cloudflare/kumo/components/select';
import { Badge } from '@cloudflare/kumo/components/badge';
import { Switch } from '@cloudflare/kumo/components/switch';
import { LayerCard } from '@cloudflare/kumo';
import { toast } from '../modules/toast.js';
import { Download, FileText, RefreshCw, Search } from '../components/Icons.jsx';

const LEVELS = [
  { value: 'all', label: '全部' },
  { value: 'INFO', label: 'INFO' },
  { value: 'WARN', label: 'WARN' },
  { value: 'ERROR', label: 'ERROR' },
  { value: 'DEBUG', label: 'DEBUG' },
];

function authHeaders() {
  return { 'x-admin-password': localStorage.getItem('admin_password') || '' };
}

function variant(level) {
  if (level === 'ERROR') return 'error';
  if (level === 'WARN') return 'warning';
  if (level === 'INFO') return 'info';
  return 'secondary';
}

export default function SystemLogsPage() {
  const [level, setLevel] = useState('all');
  const [query, setQuery] = useState('');
  const [lines, setLines] = useState([]);
  const [logPath, setLogPath] = useState('');
  const [loading, setLoading] = useState(false);
  const [autoRefresh, setAutoRefresh] = useState(true);

  const load = async () => {
    setLoading(true);
    try {
      const params = new URLSearchParams({ limit: '500' });
      if (level !== 'all') params.set('level', level);
      if (query.trim()) params.set('q', query.trim());
      const res = await fetch(`/api/system/logs/stream?${params}`, { headers: authHeaders() });
      const data = await res.json();
      if (!data.success) throw new Error(data.error || '载入日志失败');
      setLines(data.data?.lines || []);
      setLogPath(data.data?.path || '');
    } catch (error) {
      toast.error(error.message || '载入日志失败');
    } finally {
      setLoading(false);
    }
  };

  const download = async () => {
    try {
      const res = await fetch('/api/system/logs/download', { headers: authHeaders() });
      if (!res.ok) throw new Error('下载日志失败');
      const blob = await res.blob();
      const url = URL.createObjectURL(blob);
      const link = document.createElement('a');
      link.href = url;
      link.download = 'app.log';
      document.body.appendChild(link);
      link.click();
      link.remove();
      URL.revokeObjectURL(url);
    } catch (error) {
      toast.error(error.message || '下载日志失败');
    }
  };

  useEffect(() => { load(); }, [level]);
  useEffect(() => {
    if (!autoRefresh) return undefined;
    const timer = window.setInterval(load, 5000);
    return () => window.clearInterval(timer);
  }, [autoRefresh, level, query]);

  return (
    <div className="space-y-4">
      <LayerCard className="p-4">
        <div className="flex flex-wrap items-start justify-between gap-3 border-b border-kumo-line pb-3">
          <div className="min-w-0">
            <h1 className="text-lg font-semibold text-kumo-strong">系统日志</h1>
            <p className="mt-1 truncate text-xs text-kumo-subtle">{logPath || '查看、筛选并下载 Go 后端应用日志。'}</p>
          </div>
          <div className="flex flex-wrap items-center gap-2">
            <Badge variant="secondary">{lines.length} 条</Badge>
            <label className="flex h-8 items-center gap-2 rounded-md border border-kumo-line bg-kumo-recessed px-2 text-xs text-kumo-subtle">
              <Switch checked={autoRefresh} onCheckedChange={setAutoRefresh} />
              自动刷新
            </label>
            <Button size="sm" variant="secondary" onClick={download} icon={<Download className="h-3.5 w-3.5" />}>下载</Button>
            <Button size="sm" variant="primary" onClick={load} loading={loading} icon={<RefreshCw className="h-3.5 w-3.5" />}>刷新</Button>
          </div>
        </div>

        <div className="mt-3 grid gap-3 md:grid-cols-[11rem_minmax(0,1fr)_auto] md:items-end">
          <Select size="sm" label="级别" className="w-full" value={level} onValueChange={setLevel} items={LEVELS} />
          <Input size="sm" label="关键字 / 正则" value={query} onChange={(event) => setQuery(event.target.value)} onKeyDown={(event) => event.key === 'Enter' && load()} placeholder="输入关键字或正则后回车" />
          <Button size="sm" variant="secondary" onClick={load} icon={<Search className="h-3.5 w-3.5" />}>检索</Button>
        </div>
      </LayerCard>

      <section className="overflow-hidden rounded-md border border-kumo-line bg-zinc-950 text-zinc-100 shadow-none">
        <div className="grid border-b border-white/10 bg-white/5 px-3 py-2 font-mono text-[11px] uppercase tracking-normal text-zinc-500 md:grid-cols-[4.5rem_10rem_7rem_minmax(0,1fr)]">
          <span>级别</span>
          <span>时间</span>
          <span>模块</span>
          <span>消息</span>
        </div>
        {lines.length === 0 ? (
          <div className="flex min-h-80 flex-col items-center justify-center gap-2 px-6 py-12 text-center text-zinc-500">
            <FileText className="h-8 w-8" />
            <div className="text-sm font-semibold text-zinc-300">暂无日志</div>
            <div className="text-xs">调整筛选条件或刷新后再查看。</div>
          </div>
        ) : (
          <div className="max-h-[calc(100vh-19rem)] min-h-[26rem] overflow-auto px-3 font-mono text-xs">
            {lines.map((line, index) => (
              <div key={`${line.time}-${index}`} className="grid gap-2 border-b border-white/5 py-1.5 md:grid-cols-[4.5rem_10rem_7rem_minmax(0,1fr)]">
                <Badge variant={variant(line.level)}>{line.level || 'RAW'}</Badge>
                <span className="truncate text-zinc-400">{line.time || '-'}</span>
                <span className="truncate text-zinc-400">{line.module || '-'}</span>
                <span className={`min-w-0 whitespace-pre-wrap break-words ${line.matched ? 'bg-yellow-500/20 text-yellow-100' : 'text-zinc-100'}`} title={line.raw}>{line.message || line.raw}</span>
              </div>
            ))}
          </div>
        )}
      </section>
    </div>
  );
}
