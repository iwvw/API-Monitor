import React, { useCallback, useEffect, useMemo, useState } from 'react';
import { toast } from '../modules/toast.js';
import { dialog } from '../modules/dialog.js';
import { Button } from '@cloudflare/kumo/components/button';
import { Dialog } from '@cloudflare/kumo/components/dialog';
import { Input, Textarea } from '@cloudflare/kumo/components/input';
import { Select } from '@cloudflare/kumo/components/select';
import { Switch } from '@cloudflare/kumo/components/switch';
import { SkeletonLine } from '@cloudflare/kumo/components/loader';
import { Table } from '@cloudflare/kumo/components/table';
import {
  Check,
  Clock,
  Edit,
  Pause,
  Play,
  Plus,
  RefreshCw,
  Save,
  Trash,
  X,
} from '../components/Icons.jsx';

const DEFAULT_CRON_FORM = {
  name: '',
  useCustom: false,
  periodType: 'day',
  hour: 0,
  minute: 0,
  dayOfMonth: 1,
  weekday: '1',
  schedule: '0 0 * * *',
  type: 'shell',
  command: '',
  enabled: 1,
};

const PERIOD_ITEMS = [
  { value: 'minute', label: '每分钟' },
  { value: 'hour', label: '每小时' },
  { value: 'day', label: '每天' },
  { value: 'week', label: '每周' },
  { value: 'month', label: '每月' },
];

const WEEKDAY_ITEMS = [
  { value: '0', label: '周日' },
  { value: '1', label: '周一' },
  { value: '2', label: '周二' },
  { value: '3', label: '周三' },
  { value: '4', label: '周四' },
  { value: '5', label: '周五' },
  { value: '6', label: '周六' },
];

const TYPE_ITEMS = [
  { value: 'shell', label: 'Shell 命令' },
  { value: 'http', label: 'HTTP 请求' },
  { value: 'internal', label: '内部任务' },
];

function getCronExpressionFromSimple(form) {
  if (form.useCustom) return form.schedule;
  const minute = Number.isFinite(Number(form.minute)) ? Number(form.minute) : 0;
  const hour = Number.isFinite(Number(form.hour)) ? Number(form.hour) : 0;
  const day = Number.isFinite(Number(form.dayOfMonth)) ? Number(form.dayOfMonth) : 1;
  const weekday = form.weekday ?? '1';

  switch (form.periodType) {
    case 'minute':
      return '* * * * *';
    case 'hour':
      return `${minute} * * * *`;
    case 'day':
      return `${minute} ${hour} * * *`;
    case 'week':
      return `${minute} ${hour} * * ${weekday}`;
    case 'month':
      return `${minute} ${hour} ${day} * *`;
    default:
      return '0 0 * * *';
  }
}

function parseSimpleSchedule(schedule = '') {
  const parts = String(schedule).trim().split(/\s+/);
  const simple = {
    useCustom: parts.length !== 5,
    periodType: 'day',
    hour: 0,
    minute: 0,
    dayOfMonth: 1,
    weekday: '1',
  };
  if (parts.length !== 5) return simple;

  const [minute, hour, day, month, weekday] = parts;
  if (minute === '*' && hour === '*' && day === '*' && month === '*' && weekday === '*') {
    return { ...simple, periodType: 'minute' };
  }
  if (hour === '*' && day === '*' && month === '*' && weekday === '*' && /^\d+$/.test(minute)) {
    return { ...simple, periodType: 'hour', minute: Number(minute) };
  }
  if (day === '*' && month === '*' && weekday === '*' && /^\d+$/.test(minute) && /^\d+$/.test(hour)) {
    return { ...simple, periodType: 'day', minute: Number(minute), hour: Number(hour) };
  }
  if (day === '*' && month === '*' && /^\d+$/.test(weekday) && /^\d+$/.test(minute) && /^\d+$/.test(hour)) {
    return { ...simple, periodType: 'week', minute: Number(minute), hour: Number(hour), weekday };
  }
  if (month === '*' && weekday === '*' && /^\d+$/.test(day) && /^\d+$/.test(minute) && /^\d+$/.test(hour)) {
    return { ...simple, periodType: 'month', minute: Number(minute), hour: Number(hour), dayOfMonth: Number(day) };
  }
  return { ...simple, useCustom: true };
}

function formatTimestamp(value) {
  if (!value) return '-';
  const millis = Number(value) * 1000;
  if (!Number.isFinite(millis)) return '-';
  return new Date(millis).toLocaleString();
}

function SelfHPage() {
  const [cronTasks, setCronTasks] = useState([]);
  const [cronLogs, setCronLogs] = useState([]);
  const [loadingTasks, setLoadingTasks] = useState(false);
  const [loadingLogs, setLoadingLogs] = useState(false);
  const [saving, setSaving] = useState(false);
  const [editingTask, setEditingTask] = useState(null);
  const [form, setForm] = useState(DEFAULT_CRON_FORM);

  const authHeaders = useCallback(() => ({
    'Content-Type': 'application/json',
    'x-admin-password': localStorage.getItem('admin_password') || '',
  }), []);

  const loadCronTasks = useCallback(async () => {
    setLoadingTasks(true);
    try {
      const res = await fetch('/api/cron/tasks', { headers: authHeaders() });
      const data = await res.json();
      if (data.success) setCronTasks(Array.isArray(data.data) ? data.data : []);
      else toast.error(data.error || '加载任务失败');
    } catch (error) {
      console.error(error);
      toast.error('加载任务失败');
    } finally {
      setLoadingTasks(false);
    }
  }, [authHeaders]);

  const loadCronLogs = useCallback(async () => {
    setLoadingLogs(true);
    try {
      const res = await fetch('/api/cron/logs', { headers: authHeaders() });
      const data = await res.json();
      if (data.success) setCronLogs(Array.isArray(data.data) ? data.data : []);
    } catch (error) {
      console.error(error);
    } finally {
      setLoadingLogs(false);
    }
  }, [authHeaders]);

  useEffect(() => {
    loadCronTasks();
    loadCronLogs();
  }, [loadCronLogs, loadCronTasks]);

  const currentSchedule = useMemo(() => getCronExpressionFromSimple(form), [form]);

  const openCreateDialog = () => {
    setEditingTask({ id: null });
    setForm(DEFAULT_CRON_FORM);
  };

  const openEditDialog = (task) => {
    const simple = parseSimpleSchedule(task.schedule);
    setEditingTask(task);
    setForm({
      ...DEFAULT_CRON_FORM,
      ...simple,
      name: task.name || '',
      schedule: task.schedule || DEFAULT_CRON_FORM.schedule,
      type: task.type || 'shell',
      command: task.command || '',
      enabled: task.enabled ?? 1,
    });
  };

  const closeDialog = () => {
    setEditingTask(null);
    setForm(DEFAULT_CRON_FORM);
  };

  const saveTask = async () => {
    if (!form.name.trim() || !form.command.trim()) {
      toast.warning('请填写任务名称和执行内容');
      return;
    }
    setSaving(true);
    try {
      const isEdit = Boolean(editingTask?.id);
      const res = await fetch(isEdit ? `/api/cron/tasks/${editingTask.id}` : '/api/cron/tasks', {
        method: isEdit ? 'PUT' : 'POST',
        headers: authHeaders(),
        body: JSON.stringify({
          name: form.name.trim(),
          schedule: currentSchedule,
          type: form.type,
          command: form.command.trim(),
          enabled: form.enabled,
        }),
      });
      const data = await res.json();
      if (!data.success) {
        toast.error(data.error || '保存任务失败');
        return;
      }
      toast.success('任务已保存');
      closeDialog();
      await loadCronTasks();
    } catch (error) {
      console.error(error);
      toast.error('保存任务失败');
    } finally {
      setSaving(false);
    }
  };

  const toggleTask = async (task) => {
    const enabled = task.enabled ? 0 : 1;
    try {
      const res = await fetch(`/api/cron/tasks/${task.id}`, {
        method: 'PUT',
        headers: authHeaders(),
        body: JSON.stringify({ enabled }),
      });
      const data = await res.json();
      if (data.success) {
        await loadCronTasks();
      } else {
        toast.error(data.error || '更新任务失败');
      }
    } catch (error) {
      console.error(error);
      toast.error('更新任务失败');
    }
  };

  const runTask = async (task) => {
    try {
      const res = await fetch(`/api/cron/tasks/${task.id}/run`, {
        method: 'POST',
        headers: authHeaders(),
      });
      const data = await res.json();
      if (data.success) {
        toast.success('Started');
        setTimeout(loadCronLogs, 1200);
      } else {
        toast.error(data.error || '运行任务失败');
      }
    } catch (error) {
      console.error(error);
      toast.error('运行任务失败');
    }
  };

  const deleteTask = async (task) => {
    if (!(await dialog.confirm(`Delete "${task.name}"?`))) return;
    try {
      const res = await fetch(`/api/cron/tasks/${task.id}`, {
        method: 'DELETE',
        headers: authHeaders(),
      });
      const data = await res.json();
      if (data.success) {
        toast.success('Deleted');
        await Promise.all([loadCronTasks(), loadCronLogs()]);
      } else {
        toast.error(data.error || '删除任务失败');
      }
    } catch (error) {
      console.error(error);
      toast.error('删除任务失败');
    }
  };

  const clearLogs = async () => {
    if (!(await dialog.confirm('确定清理 7 天前的运行日志吗？'))) return;
    try {
      const res = await fetch('/api/cron/logs?days=7', {
        method: 'DELETE',
        headers: authHeaders(),
      });
      const data = await res.json();
      if (data.success) {
        toast.success('运行日志已清理');
        await loadCronLogs();
      } else {
        toast.error(data.error || '清理日志失败');
      }
    } catch (error) {
      console.error(error);
      toast.error('清理日志失败');
    }
  };

  return (
    <div className="flex flex-col gap-5 w-full px-1">
      <div className="flex flex-wrap items-center justify-between gap-3 border-b border-kumo-line pb-3">
        <div className="flex items-center gap-2">
          <Clock className="h-4 w-4 text-kumo-brand" />
          <h2 className="text-base font-bold text-kumo-strong">定时任务</h2>
        </div>
        <div className="flex flex-wrap items-center gap-2">
          <Button size="sm" variant="secondary" onClick={() => Promise.all([loadCronTasks(), loadCronLogs()])}>
            <RefreshCw className={`h-3.5 w-3.5 ${loadingTasks || loadingLogs ? 'animate-spin' : ''}`} />
          </Button>
          <Button size="sm" variant="primary" onClick={openCreateDialog}>
            <Plus className="h-3.5 w-3.5" />
            新建任务
          </Button>
        </div>
      </div>

      <section className="space-y-3">
        <div className="flex items-center justify-between">
          <h3 className="text-sm font-semibold text-kumo-strong">任务列表</h3>
          <span className="text-xs text-kumo-subtle">{cronTasks.length}</span>
        </div>
        {loadingTasks ? (
          <div className="space-y-2">
            <SkeletonLine className="h-10" />
            <SkeletonLine className="h-10" />
          </div>
        ) : cronTasks.length === 0 ? (
          <div className="border border-dashed border-kumo-line p-6 text-center text-sm text-kumo-subtle">
            暂无定时任务
          </div>
        ) : (
          <div className="overflow-x-auto rounded-lg border border-kumo-line">
            <Table layout="fixed" className="min-w-[920px]">
              <colgroup>
                <col className="w-[180px]" />
                <col className="w-[144px]" />
                <col className="w-[104px]" />
                <col />
                <col className="w-[168px]" />
                <col className="w-[152px]" />
              </colgroup>
              <Table.Header>
                <Table.Row>
                  <Table.Head>名称</Table.Head>
                  <Table.Head>计划</Table.Head>
                  <Table.Head>类型</Table.Head>
                  <Table.Head>执行内容</Table.Head>
                  <Table.Head>上次运行</Table.Head>
                  <Table.Head>操作</Table.Head>
                </Table.Row>
              </Table.Header>
              <Table.Body>
                {cronTasks.map((task) => (
                  <Table.Row key={task.id}>
                    <Table.Cell className="font-medium text-kumo-strong">{task.name}</Table.Cell>
                    <Table.Cell className="font-mono text-xs text-kumo-default">{task.schedule}</Table.Cell>
                    <Table.Cell className="text-xs text-kumo-default">{task.type || 'shell'}</Table.Cell>
                    <Table.Cell className="truncate font-mono text-xs text-kumo-subtle">{task.command}</Table.Cell>
                    <Table.Cell className="text-xs text-kumo-subtle">{formatTimestamp(task.last_run)}</Table.Cell>
                    <Table.Cell>
                      <div className="flex items-center gap-1">
                        <Button size="sm" variant="secondary" onClick={() => runTask(task)} aria-label="运行任务">
                          <Play className="h-3.5 w-3.5" />
                        </Button>
                        <Button size="sm" variant="secondary" onClick={() => toggleTask(task)} aria-label="启停任务">
                          {task.enabled ? <Pause className="h-3.5 w-3.5" /> : <Check className="h-3.5 w-3.5" />}
                        </Button>
                        <Button size="sm" variant="secondary" onClick={() => openEditDialog(task)} aria-label="编辑任务">
                          <Edit className="h-3.5 w-3.5" />
                        </Button>
                        <Button size="sm" variant="secondary-destructive" onClick={() => deleteTask(task)} aria-label="删除任务">
                          <Trash className="h-3.5 w-3.5" />
                        </Button>
                      </div>
                    </Table.Cell>
                  </Table.Row>
                ))}
              </Table.Body>
            </Table>
          </div>
        )}
      </section>

      <section className="space-y-3">
        <div className="flex items-center justify-between">
          <h3 className="text-sm font-semibold text-kumo-strong">运行日志</h3>
          <Button size="sm" variant="secondary" onClick={clearLogs}>
            <Trash className="h-3.5 w-3.5" />
            清空
          </Button>
        </div>
        {loadingLogs ? (
          <SkeletonLine className="h-24" />
        ) : cronLogs.length === 0 ? (
          <div className="border border-dashed border-kumo-line p-6 text-center text-sm text-kumo-subtle">
            暂无运行日志
          </div>
        ) : (
          <div className="grid gap-2">
            {cronLogs.map((log) => (
              <div key={log.id} className="border border-kumo-line p-3">
                <div className="flex flex-wrap items-center justify-between gap-2">
                  <div className="text-sm font-semibold text-kumo-strong">{log.task_name || log.task_id}</div>
                  <span className={log.status === 'success' ? 'text-xs text-kumo-success' : 'text-xs text-kumo-danger'}>
                    {log.status}
                  </span>
                </div>
                <pre className="mt-2 max-h-28 overflow-auto whitespace-pre-wrap break-words bg-kumo-recessed p-2 text-xs text-kumo-default">
                  {log.output || '（无输出）'}
                </pre>
                <div className="mt-2 text-[11px] text-kumo-subtle">
                  {formatTimestamp(log.start_time)} / {log.duration ?? 0}s
                </div>
              </div>
            ))}
          </div>
        )}
      </section>

      <Dialog.Root open={editingTask !== null} onOpenChange={(open) => !open && closeDialog()}>
        <Dialog className="w-[calc(100vw-1rem)] max-w-[calc(100vw-1rem)] overflow-y-auto p-5 sm:w-full sm:max-w-2xl sm:p-6">
          <Dialog.Title className="text-base font-bold text-kumo-strong mb-4">
            {editingTask?.id ? '编辑任务' : '新建任务'}
          </Dialog.Title>
          <div className="space-y-4">
            <Input
              size="sm"
              label="名称"
              value={form.name}
              onChange={(event) => setForm((prev) => ({ ...prev, name: event.target.value }))}
            />
            <div className="flex items-center justify-between gap-3">
              <span className="text-sm font-medium text-kumo-strong">自定义 Cron 表达式</span>
              <Switch
                checked={form.useCustom}
                onCheckedChange={(checked) => setForm((prev) => ({ ...prev, useCustom: Boolean(checked) }))}
              />
            </div>
            {form.useCustom ? (
              <Input
                size="sm"
                label="Cron"
                value={form.schedule}
                onChange={(event) => setForm((prev) => ({ ...prev, schedule: event.target.value }))}
              />
            ) : (
              <div className="grid gap-3 sm:grid-cols-2">
                <Select
                  aria-label="周期"
                  value={form.periodType}
                  onValueChange={(value) => setForm((prev) => ({ ...prev, periodType: value }))}
                  items={PERIOD_ITEMS}
                />
                {form.periodType === 'week' && (
                  <Select
                    aria-label="星期"
                    value={form.weekday}
                    onValueChange={(value) => setForm((prev) => ({ ...prev, weekday: value }))}
                    items={WEEKDAY_ITEMS}
                  />
                )}
                {form.periodType === 'month' && (
                  <Input
                    size="sm"
                    type="number"
                    label="日期"
                    min="1"
                    max="31"
                    value={form.dayOfMonth}
                    onChange={(event) => setForm((prev) => ({ ...prev, dayOfMonth: Number(event.target.value) }))}
                  />
                )}
                {['day', 'week', 'month'].includes(form.periodType) && (
                  <Input
                    size="sm"
                    type="number"
                    label="小时"
                    min="0"
                    max="23"
                    value={form.hour}
                    onChange={(event) => setForm((prev) => ({ ...prev, hour: Number(event.target.value) }))}
                  />
                )}
                {['hour', 'day', 'week', 'month'].includes(form.periodType) && (
                  <Input
                    size="sm"
                    type="number"
                    label="分钟"
                    min="0"
                    max="59"
                    value={form.minute}
                    onChange={(event) => setForm((prev) => ({ ...prev, minute: Number(event.target.value) }))}
                  />
                )}
              </div>
            )}
            <div className="rounded border border-kumo-line bg-kumo-recessed px-3 py-2 font-mono text-xs text-kumo-default">
              {currentSchedule}
            </div>
            <Select
              aria-label="任务类型"
              value={form.type}
              onValueChange={(value) => setForm((prev) => ({ ...prev, type: value }))}
              items={TYPE_ITEMS}
            />
            <Textarea
              size="sm"
              label={form.type === 'http' ? 'URL' : '命令'}
              value={form.command}
              onChange={(event) => setForm((prev) => ({ ...prev, command: event.target.value }))}
              rows={4}
            />
            <div className="flex items-center justify-between gap-3">
              <span className="text-sm font-medium text-kumo-strong">启用任务</span>
              <Switch
                checked={form.enabled === 1}
                onCheckedChange={(checked) => setForm((prev) => ({ ...prev, enabled: checked ? 1 : 0 }))}
              />
            </div>
            <div className="flex justify-end gap-2 pt-2">
              <Dialog.Close
                render={(props) => (
                  <Button size="sm" variant="secondary" {...props}>
                    <X className="h-3.5 w-3.5" />
                    取消
                  </Button>
                )}
              />
              <Button size="sm" variant="primary" onClick={saveTask} disabled={saving}>
                <Save className="h-3.5 w-3.5" />
                保存
              </Button>
            </div>
          </div>
        </Dialog>
      </Dialog.Root>
    </div>
  );
}

export default SelfHPage;
