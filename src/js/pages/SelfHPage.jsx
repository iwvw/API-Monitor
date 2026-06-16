import React, { useCallback, useEffect, useMemo, useState } from 'react';
import { toast } from '../modules/toast.js';
import { dialog } from '../modules/dialog.js';
import { Button } from '@cloudflare/kumo/components/button';
import { Dialog } from '@cloudflare/kumo/components/dialog';
import { Input, Textarea } from '@cloudflare/kumo/components/input';
import { Select } from '@cloudflare/kumo/components/select';
import { Switch } from '@cloudflare/kumo/components/switch';
import { SkeletonLine } from '@cloudflare/kumo/components/loader';
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
  { value: 'minute', label: 'Every minute' },
  { value: 'hour', label: 'Hourly' },
  { value: 'day', label: 'Daily' },
  { value: 'week', label: 'Weekly' },
  { value: 'month', label: 'Monthly' },
];

const WEEKDAY_ITEMS = [
  { value: '0', label: 'Sunday' },
  { value: '1', label: 'Monday' },
  { value: '2', label: 'Tuesday' },
  { value: '3', label: 'Wednesday' },
  { value: '4', label: 'Thursday' },
  { value: '5', label: 'Friday' },
  { value: '6', label: 'Saturday' },
];

const TYPE_ITEMS = [
  { value: 'shell', label: 'Shell' },
  { value: 'http', label: 'HTTP' },
  { value: 'internal', label: 'Internal' },
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
      else toast.error(data.error || 'Task load failed');
    } catch (error) {
      console.error(error);
      toast.error('Task load failed');
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
      toast.warning('Name and command are required');
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
        toast.error(data.error || 'Save failed');
        return;
      }
      toast.success('Saved');
      closeDialog();
      await loadCronTasks();
    } catch (error) {
      console.error(error);
      toast.error('Save failed');
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
        toast.error(data.error || 'Update failed');
      }
    } catch (error) {
      console.error(error);
      toast.error('Update failed');
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
        toast.error(data.error || 'Run failed');
      }
    } catch (error) {
      console.error(error);
      toast.error('Run failed');
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
        toast.error(data.error || 'Delete failed');
      }
    } catch (error) {
      console.error(error);
      toast.error('Delete failed');
    }
  };

  const clearLogs = async () => {
    if (!(await dialog.confirm('Clear logs older than seven days?'))) return;
    try {
      const res = await fetch('/api/cron/logs?days=7', {
        method: 'DELETE',
        headers: authHeaders(),
      });
      const data = await res.json();
      if (data.success) {
        toast.success('Logs cleared');
        await loadCronLogs();
      } else {
        toast.error(data.error || 'Clear failed');
      }
    } catch (error) {
      console.error(error);
      toast.error('Clear failed');
    }
  };

  return (
    <div className="flex flex-col gap-5 w-full px-1">
      <div className="flex flex-wrap items-center justify-between gap-3 border-b border-kumo-line pb-3">
        <div className="flex items-center gap-2">
          <Clock className="h-4 w-4 text-kumo-brand" />
          <h2 className="text-base font-bold text-kumo-strong">Scheduled Tasks</h2>
        </div>
        <div className="flex flex-wrap items-center gap-2">
          <Button size="sm" variant="secondary" onClick={() => Promise.all([loadCronTasks(), loadCronLogs()])}>
            <RefreshCw className={`h-3.5 w-3.5 ${loadingTasks || loadingLogs ? 'animate-spin' : ''}`} />
          </Button>
          <Button size="sm" variant="primary" onClick={openCreateDialog}>
            <Plus className="h-3.5 w-3.5" />
            Add
          </Button>
        </div>
      </div>

      <section className="space-y-3">
        <div className="flex items-center justify-between">
          <h3 className="text-sm font-semibold text-kumo-strong">Tasks</h3>
          <span className="text-xs text-kumo-subtle">{cronTasks.length}</span>
        </div>
        {loadingTasks ? (
          <div className="space-y-2">
            <SkeletonLine className="h-10" />
            <SkeletonLine className="h-10" />
          </div>
        ) : cronTasks.length === 0 ? (
          <div className="border border-dashed border-kumo-line p-6 text-center text-sm text-kumo-subtle">
            No scheduled tasks
          </div>
        ) : (
          <div className="overflow-x-auto border border-kumo-line">
            <table className="min-w-full table-fixed text-left text-xs">
              <thead className="bg-kumo-recessed text-kumo-subtle">
                <tr>
                  <th className="w-48 px-3 py-2 font-semibold">Name</th>
                  <th className="w-36 px-3 py-2 font-semibold">Schedule</th>
                  <th className="w-24 px-3 py-2 font-semibold">Type</th>
                  <th className="min-w-64 px-3 py-2 font-semibold">Command</th>
                  <th className="w-40 px-3 py-2 font-semibold">Last Run</th>
                  <th className="w-36 px-3 py-2 font-semibold">Actions</th>
                </tr>
              </thead>
              <tbody>
                {cronTasks.map((task) => (
                  <tr key={task.id} className="border-t border-kumo-line">
                    <td className="px-3 py-2 font-medium text-kumo-strong">{task.name}</td>
                    <td className="px-3 py-2 font-mono text-kumo-default">{task.schedule}</td>
                    <td className="px-3 py-2 text-kumo-default">{task.type || 'shell'}</td>
                    <td className="px-3 py-2 font-mono text-kumo-subtle truncate">{task.command}</td>
                    <td className="px-3 py-2 text-kumo-subtle">{formatTimestamp(task.last_run)}</td>
                    <td className="px-3 py-2">
                      <div className="flex items-center gap-1">
                        <Button size="sm" variant="secondary" onClick={() => runTask(task)} aria-label="Run task">
                          <Play className="h-3.5 w-3.5" />
                        </Button>
                        <Button size="sm" variant="secondary" onClick={() => toggleTask(task)} aria-label="Toggle task">
                          {task.enabled ? <Pause className="h-3.5 w-3.5" /> : <Check className="h-3.5 w-3.5" />}
                        </Button>
                        <Button size="sm" variant="secondary" onClick={() => openEditDialog(task)} aria-label="Edit task">
                          <Edit className="h-3.5 w-3.5" />
                        </Button>
                        <Button size="sm" variant="secondary-destructive" onClick={() => deleteTask(task)} aria-label="Delete task">
                          <Trash className="h-3.5 w-3.5" />
                        </Button>
                      </div>
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        )}
      </section>

      <section className="space-y-3">
        <div className="flex items-center justify-between">
          <h3 className="text-sm font-semibold text-kumo-strong">Run Logs</h3>
          <Button size="sm" variant="secondary" onClick={clearLogs}>
            <Trash className="h-3.5 w-3.5" />
            Clear
          </Button>
        </div>
        {loadingLogs ? (
          <SkeletonLine className="h-24" />
        ) : cronLogs.length === 0 ? (
          <div className="border border-dashed border-kumo-line p-6 text-center text-sm text-kumo-subtle">
            No logs
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
                  {log.output || '(empty)'}
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
        <Dialog className="p-6 sm:max-w-lg">
          <Dialog.Title className="text-base font-bold text-kumo-strong mb-4">
            {editingTask?.id ? 'Edit Task' : 'Add Task'}
          </Dialog.Title>
          <div className="space-y-4">
            <Input
              size="sm"
              label="Name"
              value={form.name}
              onChange={(event) => setForm((prev) => ({ ...prev, name: event.target.value }))}
            />
            <div className="flex items-center justify-between gap-3">
              <span className="text-sm font-medium text-kumo-strong">Custom expression</span>
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
                  aria-label="Period"
                  value={form.periodType}
                  onValueChange={(value) => setForm((prev) => ({ ...prev, periodType: value }))}
                  items={PERIOD_ITEMS}
                />
                {form.periodType === 'week' && (
                  <Select
                    aria-label="Weekday"
                    value={form.weekday}
                    onValueChange={(value) => setForm((prev) => ({ ...prev, weekday: value }))}
                    items={WEEKDAY_ITEMS}
                  />
                )}
                {form.periodType === 'month' && (
                  <Input
                    size="sm"
                    type="number"
                    label="Day"
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
                    label="Hour"
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
                    label="Minute"
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
              aria-label="Task type"
              value={form.type}
              onValueChange={(value) => setForm((prev) => ({ ...prev, type: value }))}
              items={TYPE_ITEMS}
            />
            <Textarea
              size="sm"
              label={form.type === 'http' ? 'URL' : 'Command'}
              value={form.command}
              onChange={(event) => setForm((prev) => ({ ...prev, command: event.target.value }))}
              rows={4}
            />
            <div className="flex items-center justify-between gap-3">
              <span className="text-sm font-medium text-kumo-strong">Enabled</span>
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
                    Cancel
                  </Button>
                )}
              />
              <Button size="sm" variant="primary" onClick={saveTask} disabled={saving}>
                <Save className="h-3.5 w-3.5" />
                Save
              </Button>
            </div>
          </div>
        </Dialog>
      </Dialog.Root>
    </div>
  );
}

export default SelfHPage;
