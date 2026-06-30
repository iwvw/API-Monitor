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
import { Badge } from '@cloudflare/kumo/components/badge';
import { Empty } from '@cloudflare/kumo/components/empty';
import { Tooltip, TooltipProvider } from '@cloudflare/kumo/components/tooltip';
import { Tabs } from '@cloudflare/kumo';
import { MODULE_TABS_PROPS } from '../modules/kumoTabs.js';
import {
  Activity,
  ArrowRight,
  Check,
  Clock,
  Copy,
  Download,
  Edit,
  Eye,
  GitBranch,
  Layers,
  Pause,
  Play,
  Plus,
  RefreshCw,
  Save,
  Server,
  Sliders,
  Trash,
  Upload,
  X,
} from '../components/Icons.jsx';

const DEFAULT_TASK_FORM = {
  id: null,
  name: '',
  description: '',
  useCustom: false,
  periodType: 'day',
  hour: 3,
  minute: 0,
  dayOfMonth: 1,
  weekday: '1',
  schedule: '0 3 * * *',
  type: 'shell',
  command: '',
  enabled: 1,
  timeout_seconds: 300,
  retry_count: 0,
  retry_interval_seconds: 30,
  max_concurrency: 1,
  node_id: 'local',
  node_selector: '',
};

const DEFAULT_WORKFLOW_FORM = {
  id: null,
  name: '',
  description: '',
  schedule: '',
  enabled: 1,
  concurrency_policy: 'skip',
  failure_policy: 'stop',
  nodes: [
    { id: 'start', name: '开始', type: 'start', enabled: 1, x: 40, y: 80 },
    { id: 'task-1', name: '任务 1', type: 'task', task_id: 0, enabled: 1, x: 260, y: 80 },
  ],
  edges: [{ id: 'edge-1', from: 'start', to: 'task-1', condition: 'success' }],
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
  { value: 'internal', label: '内部接口' },
  { value: 'agent', label: 'Agent 命令' },
];

const CONDITION_ITEMS = [
  { value: 'success', label: '成功后' },
  { value: 'failed', label: '失败后' },
  { value: 'complete', label: '完成后' },
];

const STATUS_LABELS = {
  success: '成功',
  failed: '失败',
  running: '运行中',
  queued: '排队中',
  skipped: '已跳过',
  timeout: '超时',
  cancelled: '已取消',
};

const TASK_TABS = [
  { value: 'tasks', label: <span className="inline-flex items-center gap-1.5"><Clock className="w-3.5 h-3.5" />任务</span> },
  { value: 'workflows', label: <span className="inline-flex items-center gap-1.5"><GitBranch className="w-3.5 h-3.5" />工作流</span> },
  { value: 'runs', label: <span className="inline-flex items-center gap-1.5"><Activity className="w-3.5 h-3.5" />运行记录</span> },
  { value: 'nodes', label: <span className="inline-flex items-center gap-1.5"><Server className="w-3.5 h-3.5" />执行节点</span> },
];

function getCronExpressionFromSimple(form) {
  if (form.useCustom) return form.schedule.trim();
  const minute = clampInteger(form.minute, 0, 59, 0);
  const hour = clampInteger(form.hour, 0, 23, 0);
  const day = clampInteger(form.dayOfMonth, 1, 31, 1);
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
      return '0 3 * * *';
  }
}

function parseSimpleSchedule(schedule = '') {
  const parts = String(schedule).trim().split(/\s+/);
  const simple = { useCustom: parts.length !== 5, periodType: 'day', hour: 3, minute: 0, dayOfMonth: 1, weekday: '1' };
  if (parts.length !== 5) return simple;
  const [minute, hour, day, month, weekday] = parts;
  if (minute === '*' && hour === '*' && day === '*' && month === '*' && weekday === '*') return { ...simple, periodType: 'minute' };
  if (hour === '*' && day === '*' && month === '*' && weekday === '*' && /^\d+$/.test(minute)) return { ...simple, periodType: 'hour', minute: Number(minute) };
  if (day === '*' && month === '*' && weekday === '*' && /^\d+$/.test(minute) && /^\d+$/.test(hour)) return { ...simple, periodType: 'day', minute: Number(minute), hour: Number(hour) };
  if (day === '*' && month === '*' && /^\d+$/.test(weekday) && /^\d+$/.test(minute) && /^\d+$/.test(hour)) return { ...simple, periodType: 'week', minute: Number(minute), hour: Number(hour), weekday };
  if (month === '*' && weekday === '*' && /^\d+$/.test(day) && /^\d+$/.test(minute) && /^\d+$/.test(hour)) return { ...simple, periodType: 'month', minute: Number(minute), hour: Number(hour), dayOfMonth: Number(day) };
  return { ...simple, useCustom: true };
}

function clampInteger(value, min, max, fallback) {
  const next = Number(value);
  if (!Number.isFinite(next)) return fallback;
  return Math.min(max, Math.max(min, Math.trunc(next)));
}

function formatTimestamp(value) {
  if (!value) return '-';
  const millis = Number(value) * 1000;
  if (!Number.isFinite(millis)) return '-';
  return new Date(millis).toLocaleString('zh-CN', { hour12: false });
}

function statusBadgeVariant(status) {
  if (status === 'success' || status === 'online') return 'success';
  if (status === 'failed' || status === 'timeout' || status === 'offline') return 'error';
  if (status === 'running' || status === 'queued') return 'info';
  if (status === 'skipped' || status === 'unknown') return 'neutral';
  return 'secondary';
}

function statusLabel(status) {
  return STATUS_LABELS[status] || (status === 'online' ? '在线' : status === 'offline' ? '离线' : status || '未知');
}

function taskTypeLabel(value) {
  return TYPE_ITEMS.find((item) => item.value === value)?.label || value || 'Shell 命令';
}

function summarizeOutput(output = '') {
  const text = String(output || '').trim();
  if (!text) return '无输出';
  const status = text.match(/Status:\s*(\d+)/i)?.[1];
  if (status) {
    const body = text.replace(/Status:\s*\d+\s*/i, '').replace(/^Data:\s*/i, '').trim();
    return `HTTP ${status}${body ? ` / ${body.slice(0, 90)}` : ''}`;
  }
  return text.slice(0, 120);
}

function cloneWorkflowForm(workflow = null) {
  if (!workflow) return JSON.parse(JSON.stringify(DEFAULT_WORKFLOW_FORM));
  return {
    id: workflow.id,
    name: workflow.name || '',
    description: workflow.description || '',
    schedule: workflow.schedule || '',
    enabled: workflow.enabled ?? 1,
    concurrency_policy: workflow.concurrency_policy || 'skip',
    failure_policy: workflow.failure_policy || 'stop',
    nodes: Array.isArray(workflow.nodes) ? workflow.nodes.map((node, index) => ({
      ...node,
      enabled: node.enabled ?? 1,
      x: node.x ?? 60 + index * 190,
      y: node.y ?? 90,
    })) : [],
    edges: Array.isArray(workflow.edges) ? workflow.edges : [],
  };
}

function IconButton({ label, icon, onClick, variant = 'secondary', disabled = false }) {
  return (
    <Tooltip
      content={label}
      render={(
        <Button size="sm" variant={variant} shape="square" aria-label={label} onClick={onClick} disabled={disabled}>
          {icon}
        </Button>
      )}
    />
  );
}

function CronEditor({ form, setForm, preview, previewError }) {
  const currentSchedule = getCronExpressionFromSimple(form);
  return (
    <div className="space-y-3">
      <div className="flex items-center justify-between gap-3 rounded-md border border-kumo-line p-3">
        <div>
          <div className="text-sm font-medium text-kumo-strong">可视化 Cron 编辑器</div>
          <div className="text-xs text-kumo-subtle">简易周期会自动生成表达式，高级模式可手写。</div>
        </div>
        <Switch
          checked={form.useCustom}
          onCheckedChange={(checked) => setForm((prev) => ({ ...prev, useCustom: Boolean(checked) }))}
        />
      </div>

      {form.useCustom ? (
        <Input
          size="sm"
          label="Cron 表达式"
          value={form.schedule}
          onChange={(event) => setForm((prev) => ({ ...prev, schedule: event.target.value }))}
        />
      ) : (
        <div className="grid gap-3 sm:grid-cols-2">
          <Select size="sm" label="周期" className="w-full" value={form.periodType} onValueChange={(value) => setForm((prev) => ({ ...prev, periodType: value }))} items={PERIOD_ITEMS} />
          {form.periodType === 'week' && (
            <Select size="sm" label="星期" className="w-full" value={form.weekday} onValueChange={(value) => setForm((prev) => ({ ...prev, weekday: value }))} items={WEEKDAY_ITEMS} />
          )}
          {form.periodType === 'month' && (
            <Input size="sm" type="number" label="日期" min="1" max="31" value={form.dayOfMonth} onChange={(event) => setForm((prev) => ({ ...prev, dayOfMonth: Number(event.target.value) }))} />
          )}
          {['day', 'week', 'month'].includes(form.periodType) && (
            <Input size="sm" type="number" label="小时" min="0" max="23" value={form.hour} onChange={(event) => setForm((prev) => ({ ...prev, hour: Number(event.target.value) }))} />
          )}
          {['hour', 'day', 'week', 'month'].includes(form.periodType) && (
            <Input size="sm" type="number" label="分钟" min="0" max="59" value={form.minute} onChange={(event) => setForm((prev) => ({ ...prev, minute: Number(event.target.value) }))} />
          )}
        </div>
      )}

      <div className="grid gap-3 sm:grid-cols-[minmax(0,1fr)_minmax(0,1.3fr)]">
        <div className="flex items-center px-3 py-2 rounded-md border border-kumo-line bg-kumo-recessed font-mono text-xs text-kumo-default">
          {currentSchedule || '手动触发'}
        </div>
        <div className="rounded-md border border-kumo-line px-3 py-2 text-xs">
          {previewError ? (
            <span className="text-kumo-danger">{previewError}</span>
          ) : preview?.summary ? (
            <div className="space-y-1">
              <div className="font-medium text-kumo-strong">{preview.summary}</div>
              <div className="text-kumo-subtle">未来执行：{(preview.next || []).map(formatTimestamp).join('、')}</div>
            </div>
          ) : (
            <span className="text-kumo-subtle">填写周期后会预览未来 5 次运行时间。</span>
          )}
        </div>
      </div>
    </div>
  );
}

function WorkflowCanvas({ workflow, runs = [] }) {
  const nodes = workflow.nodes || [];
  const edges = workflow.edges || [];
  const latestRun = runs.find((run) => run.workflow_id === workflow.id);
  const nodeStatus = Object.fromEntries((latestRun?.node_runs || []).map((run) => [run.node_id, run.status]));
  const width = Math.max(760, ...nodes.map((node) => (node.x || 0) + 170), 760);
  const height = Math.max(240, ...nodes.map((node) => (node.y || 0) + 90), 240);

  return (
    <div className="overflow-auto rounded-md border border-kumo-line bg-kumo-recessed/30">
      <svg width={width} height={height} className="block">
        <defs>
          <marker id="workflow-arrow" markerWidth="10" markerHeight="10" refX="8" refY="3" orient="auto">
            <path d="M0,0 L0,6 L9,3 z" className="fill-kumo-subtle" />
          </marker>
        </defs>
        {edges.map((edge) => {
          const from = nodes.find((node) => node.id === edge.from);
          const to = nodes.find((node) => node.id === edge.to);
          if (!from || !to) return null;
          const x1 = (from.x || 0) + 140;
          const y1 = (from.y || 0) + 30;
          const x2 = to.x || 0;
          const y2 = (to.y || 0) + 30;
          return (
            <g key={edge.id}>
              <line x1={x1} y1={y1} x2={x2} y2={y2} className="stroke-kumo-subtle" strokeWidth="1.4" markerEnd="url(#workflow-arrow)" />
              <text x={(x1 + x2) / 2} y={(y1 + y2) / 2 - 6} className="fill-kumo-subtle text-[10px]">{CONDITION_ITEMS.find((item) => item.value === edge.condition)?.label || '成功后'}</text>
            </g>
          );
        })}
        {nodes.map((node) => {
          const status = nodeStatus[node.id];
          return (
            <foreignObject key={node.id} x={node.x || 0} y={node.y || 0} width="150" height="72">
              <div className="h-full rounded-md border border-kumo-line bg-kumo-base p-2">
                <div className="truncate text-xs font-semibold text-kumo-strong">{node.name}</div>
                <div className="mt-1 truncate text-[11px] text-kumo-subtle">{node.type === 'start' ? '开始节点' : node.task_id ? `任务 #${node.task_id}` : taskTypeLabel(node.type)}</div>
                <div className="mt-2">
                  <Badge variant={statusBadgeVariant(status || (node.enabled === 0 ? 'skipped' : 'queued'))} appearance="dot">
                    {node.enabled === 0 ? '停用' : status ? statusLabel(status) : '待运行'}
                  </Badge>
                </div>
              </div>
            </foreignObject>
          );
        })}
      </svg>
    </div>
  );
}

function SchedulerPage() {
  const [activeTab, setActiveTab] = useState('tasks');
  const [tasks, setTasks] = useState([]);
  const [workflows, setWorkflows] = useState([]);
  const [runs, setRuns] = useState([]);
  const [nodes, setNodes] = useState([]);
  const [loading, setLoading] = useState(false);
  const [saving, setSaving] = useState(false);
  const [taskDialogOpen, setTaskDialogOpen] = useState(false);
  const [workflowDialogOpen, setWorkflowDialogOpen] = useState(false);
  const [taskForm, setTaskForm] = useState(DEFAULT_TASK_FORM);
  const [workflowForm, setWorkflowForm] = useState(cloneWorkflowForm());
  const [cronPreview, setCronPreview] = useState(null);
  const [cronPreviewError, setCronPreviewError] = useState('');
  const [selectedRun, setSelectedRun] = useState(null);

  const authHeaders = useCallback(() => ({
    'Content-Type': 'application/json',
    'x-admin-password': localStorage.getItem('admin_password') || '',
  }), []);

  const loadAll = useCallback(async () => {
    setLoading(true);
    try {
      const headers = authHeaders();
      const [taskRes, workflowRes, runRes, nodeRes] = await Promise.all([
        fetch('/api/scheduler/tasks', { headers }),
        fetch('/api/scheduler/workflows', { headers }),
        fetch('/api/scheduler/runs', { headers }),
        fetch('/api/scheduler/nodes', { headers }),
      ]);
      const [taskData, workflowData, runData, nodeData] = await Promise.all([
        taskRes.json(),
        workflowRes.json(),
        runRes.json(),
        nodeRes.json(),
      ]);
      if (taskData.success) setTasks(Array.isArray(taskData.data) ? taskData.data : []);
      if (workflowData.success) setWorkflows(Array.isArray(workflowData.data) ? workflowData.data : []);
      if (runData.success) setRuns(Array.isArray(runData.data) ? runData.data : []);
      if (nodeData.success) setNodes(Array.isArray(nodeData.data) ? nodeData.data : []);
      if (!taskData.success || !workflowData.success || !runData.success || !nodeData.success) {
        toast.error('载入定时任务数据失败');
      }
    } catch (error) {
      console.error(error);
      toast.error('载入定时任务数据失败');
    } finally {
      setLoading(false);
    }
  }, [authHeaders]);

  useEffect(() => {
    loadAll();
  }, [loadAll]);

  const currentSchedule = useMemo(() => getCronExpressionFromSimple(taskForm), [taskForm]);

  useEffect(() => {
    if (!taskDialogOpen || !currentSchedule) {
      setCronPreview(null);
      setCronPreviewError('');
      return undefined;
    }
    const handle = window.setTimeout(async () => {
      try {
        const res = await fetch('/api/scheduler/cron/preview', {
          method: 'POST',
          headers: authHeaders(),
          body: JSON.stringify({ schedule: currentSchedule, count: 5 }),
        });
        const data = await res.json();
        if (data.success) {
          setCronPreview(data.data);
          setCronPreviewError('');
        } else {
          setCronPreview(null);
          setCronPreviewError(data.error || 'Cron 表达式无效');
        }
      } catch (error) {
        setCronPreview(null);
        setCronPreviewError('无法预览 Cron 表达式');
      }
    }, 250);
    return () => window.clearTimeout(handle);
  }, [authHeaders, currentSchedule, taskDialogOpen]);

  const openCreateTask = () => {
    setTaskForm(DEFAULT_TASK_FORM);
    setTaskDialogOpen(true);
  };

  const openEditTask = (task) => {
    const simple = parseSimpleSchedule(task.schedule || '');
    setTaskForm({
      ...DEFAULT_TASK_FORM,
      ...simple,
      id: task.id,
      name: task.name || '',
      description: task.description || '',
      schedule: task.schedule || DEFAULT_TASK_FORM.schedule,
      type: task.type || 'shell',
      command: task.command || '',
      enabled: task.enabled ?? 1,
      timeout_seconds: task.timeout_seconds ?? 300,
      retry_count: task.retry_count ?? 0,
      retry_interval_seconds: task.retry_interval_seconds ?? 30,
      max_concurrency: task.max_concurrency ?? 1,
      node_id: task.node_id || 'local',
      node_selector: task.node_selector || '',
    });
    setTaskDialogOpen(true);
  };

  const saveTask = async () => {
    if (!taskForm.name.trim() || !taskForm.command.trim()) {
      toast.warning('请填写任务名称和执行内容');
      return;
    }
    setSaving(true);
    try {
      const isEdit = Boolean(taskForm.id);
      const payload = {
        name: taskForm.name.trim(),
        description: taskForm.description.trim(),
        schedule: currentSchedule,
        type: taskForm.type,
        command: taskForm.command.trim(),
        enabled: taskForm.enabled,
        timeout_seconds: Number(taskForm.timeout_seconds) || 300,
        retry_count: Number(taskForm.retry_count) || 0,
        retry_interval_seconds: Number(taskForm.retry_interval_seconds) || 30,
        max_concurrency: Number(taskForm.max_concurrency) || 1,
        node_id: taskForm.node_id || 'local',
        node_selector: taskForm.node_selector || '',
      };
      const res = await fetch(isEdit ? `/api/scheduler/tasks/${taskForm.id}` : '/api/scheduler/tasks', {
        method: isEdit ? 'PUT' : 'POST',
        headers: authHeaders(),
        body: JSON.stringify(payload),
      });
      const data = await res.json();
      if (!data.success) throw new Error(data.error || '保存任务失败');
      toast.success('任务已保存');
      setTaskDialogOpen(false);
      await loadAll();
    } catch (error) {
      toast.error(error.message || '保存任务失败');
    } finally {
      setSaving(false);
    }
  };

  const toggleTask = async (task) => {
    try {
      const res = await fetch(`/api/scheduler/tasks/${task.id}`, {
        method: 'PUT',
        headers: authHeaders(),
        body: JSON.stringify({ ...task, enabled: task.enabled ? 0 : 1 }),
      });
      const data = await res.json();
      if (!data.success) throw new Error(data.error || '更新任务失败');
      toast.success(task.enabled ? '任务已停用' : '任务已启用');
      await loadAll();
    } catch (error) {
      toast.error(error.message || '更新任务失败');
    }
  };

  const runTask = async (task) => {
    try {
      const res = await fetch(`/api/scheduler/tasks/${task.id}/run`, { method: 'POST', headers: authHeaders() });
      const data = await res.json();
      if (!data.success) throw new Error(data.error || '运行任务失败');
      toast.success('任务已开始运行');
      window.setTimeout(loadAll, 1000);
    } catch (error) {
      toast.error(error.message || '运行任务失败');
    }
  };

  const deleteTask = async (task) => {
    if (!(await dialog.confirm(`确定删除任务“${task.name}”吗？`))) return;
    try {
      const res = await fetch(`/api/scheduler/tasks/${task.id}`, { method: 'DELETE', headers: authHeaders() });
      const data = await res.json();
      if (!data.success) throw new Error(data.error || '删除任务失败');
      toast.success('任务已删除');
      await loadAll();
    } catch (error) {
      toast.error(error.message || '删除任务失败');
    }
  };

  const openCreateWorkflow = () => {
    setWorkflowForm(cloneWorkflowForm());
    setWorkflowDialogOpen(true);
  };

  const openEditWorkflow = (workflow) => {
    setWorkflowForm(cloneWorkflowForm(workflow));
    setWorkflowDialogOpen(true);
  };

  const saveWorkflow = async () => {
    if (!workflowForm.name.trim()) {
      toast.warning('请填写工作流名称');
      return;
    }
    setSaving(true);
    try {
      const isEdit = Boolean(workflowForm.id);
      const payload = {
        name: workflowForm.name.trim(),
        description: workflowForm.description.trim(),
        schedule: workflowForm.schedule.trim(),
        enabled: workflowForm.enabled,
        concurrency_policy: workflowForm.concurrency_policy,
        failure_policy: workflowForm.failure_policy,
        nodes: workflowForm.nodes.map((node) => ({ ...node, task_id: Number(node.task_id) || 0 })),
        edges: workflowForm.edges,
      };
      const res = await fetch(isEdit ? `/api/scheduler/workflows/${workflowForm.id}` : '/api/scheduler/workflows', {
        method: isEdit ? 'PUT' : 'POST',
        headers: authHeaders(),
        body: JSON.stringify(payload),
      });
      const data = await res.json();
      if (!data.success) throw new Error(data.error || '保存工作流失败');
      toast.success('工作流已保存');
      setWorkflowDialogOpen(false);
      await loadAll();
    } catch (error) {
      toast.error(error.message || '保存工作流失败');
    } finally {
      setSaving(false);
    }
  };

  const runWorkflow = async (workflow) => {
    try {
      const res = await fetch(`/api/scheduler/workflows/${workflow.id}/run`, { method: 'POST', headers: authHeaders() });
      const data = await res.json();
      if (!data.success) throw new Error(data.error || '运行工作流失败');
      toast.success('工作流运行完成');
      await loadAll();
      setActiveTab('runs');
    } catch (error) {
      toast.error(error.message || '运行工作流失败');
    }
  };

  const deleteWorkflow = async (workflow) => {
    if (!(await dialog.confirm(`确定删除工作流“${workflow.name}”吗？`))) return;
    try {
      const res = await fetch(`/api/scheduler/workflows/${workflow.id}`, { method: 'DELETE', headers: authHeaders() });
      const data = await res.json();
      if (!data.success) throw new Error(data.error || '删除工作流失败');
      toast.success('工作流已删除');
      await loadAll();
    } catch (error) {
      toast.error(error.message || '删除工作流失败');
    }
  };

  const exportWorkflows = async () => {
    try {
      const res = await fetch('/api/scheduler/workflows/export', { headers: authHeaders() });
      const data = await res.json();
      if (!data.success) throw new Error(data.error || '导出工作流失败');
      const blob = new Blob([JSON.stringify(data.data, null, 2)], { type: 'application/json' });
      const url = URL.createObjectURL(blob);
      const link = document.createElement('a');
      link.href = url;
      link.download = `scheduler-workflows-${new Date().toISOString().slice(0, 10)}.json`;
      link.click();
      URL.revokeObjectURL(url);
    } catch (error) {
      toast.error(error.message || '导出工作流失败');
    }
  };

  const importWorkflows = async () => {
    const input = document.createElement('input');
    input.type = 'file';
    input.accept = 'application/json,.json';
    input.onchange = async () => {
      const file = input.files?.[0];
      if (!file) return;
      try {
        const text = await file.text();
        const parsed = JSON.parse(text);
        const workflowsToImport = Array.isArray(parsed) ? parsed : parsed.workflows;
        if (!Array.isArray(workflowsToImport) || workflowsToImport.length === 0) throw new Error('未找到工作流定义');
        const res = await fetch('/api/scheduler/workflows/import', {
          method: 'POST',
          headers: authHeaders(),
          body: JSON.stringify({ workflows: workflowsToImport }),
        });
        const data = await res.json();
        if (!data.success) throw new Error(data.error || '导入工作流失败');
        toast.success(`已导入 ${data.data?.imported || 0} 个工作流`);
        await loadAll();
      } catch (error) {
        toast.error(error.message || '导入工作流失败');
      }
    };
    input.click();
  };

  const retryRun = async (run) => {
    try {
      const res = await fetch(`/api/scheduler/workflow-runs/${run.id}/retry`, { method: 'POST', headers: authHeaders() });
      const data = await res.json();
      if (!data.success) throw new Error(data.error || '重试失败');
      toast.success('已创建重试运行');
      await loadAll();
    } catch (error) {
      toast.error(error.message || '重试失败');
    }
  };

  const cancelRun = async (run) => {
    try {
      const res = await fetch(`/api/scheduler/workflow-runs/${run.id}/cancel`, { method: 'POST', headers: authHeaders() });
      const data = await res.json();
      if (!data.success) throw new Error(data.error || '取消失败');
      toast.success('运行已取消');
      await loadAll();
    } catch (error) {
      toast.error(error.message || '取消失败');
    }
  };

  const clearOldRuns = async () => {
    if (!(await dialog.confirm('确定清理 30 天前的工作流运行记录吗？'))) return;
    try {
      const res = await fetch('/api/scheduler/runs?days=30', { method: 'DELETE', headers: authHeaders() });
      const data = await res.json();
      if (!data.success) throw new Error(data.error || '清理失败');
      toast.success('旧运行记录已清理');
      await loadAll();
    } catch (error) {
      toast.error(error.message || '清理运行记录失败');
    }
  };

  const clearAllRuns = async () => {
    if (!(await dialog.confirm('确定清空全部工作流运行记录吗？此操作不可恢复。'))) return;
    try {
      const res = await fetch('/api/scheduler/runs?all=true', { method: 'DELETE', headers: authHeaders() });
      const data = await res.json();
      if (!data.success) throw new Error(data.error || '清空失败');
      toast.success('全部运行记录已清空');
      await loadAll();
    } catch (error) {
      toast.error(error.message || '清空运行记录失败');
    }
  };

  const stats = useMemo(() => ({
    totalTasks: tasks.length,
    enabledTasks: tasks.filter((task) => task.enabled).length,
    workflows: workflows.length,
    failedRuns: runs.filter((run) => run.status === 'failed').length,
  }), [runs, tasks, workflows]);

  const nodeItems = useMemo(() => nodes.map((node) => ({ value: node.id, label: `${node.name}（${node.kind === 'local' ? '本机' : 'Agent'}）` })), [nodes]);
  const taskItems = useMemo(() => [{ value: '0', label: '内联任务' }, ...tasks.map((task) => ({ value: String(task.id), label: `${task.name} #${task.id}` }))], [tasks]);

  return (
    <TooltipProvider>
      <div className="flex w-full flex-col gap-5 px-1">
        <div className="flex flex-wrap items-center justify-between border-b border-kumo-line pb-3 gap-4">
          <Tabs
            {...MODULE_TABS_PROPS}
            value={activeTab}
            onValueChange={setActiveTab}
            tabs={TASK_TABS}
          />
          <div className="flex flex-wrap items-center gap-2">
            <IconButton label="刷新" onClick={loadAll} icon={<RefreshCw className={`h-3.5 w-3.5 ${loading ? 'animate-spin' : ''}`} />} />
            {activeTab === 'workflows' && (
              <>
                <IconButton label="导入工作流" onClick={importWorkflows} icon={<Upload className="h-3.5 w-3.5" />} />
                <IconButton label="导出工作流" onClick={exportWorkflows} icon={<Download className="h-3.5 w-3.5" />} />
              </>
            )}
            <Button size="sm" variant="primary" onClick={activeTab === 'workflows' ? openCreateWorkflow : openCreateTask}>
              <Plus className="h-3.5 w-3.5" />
              {activeTab === 'workflows' ? '新建工作流' : '新建任务'}
            </Button>
          </div>
        </div>

        <div className="grid gap-3 sm:grid-cols-2 xl:grid-cols-4">
          {[
            ['任务总数', stats.totalTasks, <Clock className="h-4 w-4" />],
            ['启用任务', stats.enabledTasks, <Check className="h-4 w-4" />],
            ['工作流', stats.workflows, <GitBranch className="h-4 w-4" />],
            ['失败运行', stats.failedRuns, <Activity className="h-4 w-4" />],
          ].map(([label, value, icon]) => (
            <div key={label} className="rounded-md border border-kumo-line p-3">
              <div className="flex items-center justify-between text-xs text-kumo-subtle">
                <span>{label}</span>
                {icon}
              </div>
              <div className="mt-2 font-mono text-xl font-bold text-kumo-strong">{value}</div>
            </div>
          ))}
        </div>


        {activeTab === 'tasks' && (
          <section className="space-y-3">
            {loading ? <SkeletonLine className="h-28" /> : tasks.length === 0 ? (
              <Empty size="sm" icon={<Clock className="h-8 w-8 text-kumo-inactive" />} title="暂无任务" description="创建 Shell、HTTP、内部接口或 Agent 任务后，可作为定时任务或工作流节点运行。" contents={<Button size="sm" variant="primary" onClick={openCreateTask}><Plus className="h-3.5 w-3.5" />新建任务</Button>} />
            ) : (
              <div className="overflow-x-auto rounded-md border border-kumo-line">
                <Table layout="fixed" className="min-w-[1080px]">
                  <colgroup><col className="w-[220px]" /><col className="w-[104px]" /><col className="w-[128px]" /><col className="w-[190px]" /><col className="w-[180px]" /><col className="w-[180px]" /><col className="w-[160px]" /></colgroup>
                  <Table.Header><Table.Row><Table.Head>任务</Table.Head><Table.Head>状态</Table.Head><Table.Head>类型</Table.Head><Table.Head>周期</Table.Head><Table.Head>下次运行</Table.Head><Table.Head>最近结果</Table.Head><Table.Head>操作</Table.Head></Table.Row></Table.Header>
                  <Table.Body>
                    {tasks.map((task) => (
                      <Table.Row key={task.id}>
                        <Table.Cell><div className="font-semibold text-kumo-strong">{task.name}</div><div className="truncate text-xs text-kumo-subtle">{task.description || task.command}</div></Table.Cell>
                        <Table.Cell><Badge variant={task.enabled ? 'success' : 'neutral'} appearance="dot">{task.enabled ? '启用' : '停用'}</Badge></Table.Cell>
                        <Table.Cell className="text-xs text-kumo-default">{taskTypeLabel(task.type)}</Table.Cell>
                        <Table.Cell><div className="font-mono text-xs text-kumo-default">{task.schedule || '手动'}</div><div className="text-xs text-kumo-subtle">{task.schedule_summary}</div></Table.Cell>
                        <Table.Cell className="text-xs text-kumo-subtle">{formatTimestamp(task.next_run)}</Table.Cell>
                        <Table.Cell>{task.recent_status ? <Badge variant={statusBadgeVariant(task.recent_status)} appearance="dot">{statusLabel(task.recent_status)}</Badge> : <span className="text-xs text-kumo-subtle">暂无运行</span>}</Table.Cell>
                        <Table.Cell>
                          <div className="flex items-center gap-1">
                            <IconButton label="立即运行" onClick={() => runTask(task)} icon={<Play className="h-3.5 w-3.5" />} />
                            <IconButton label={task.enabled ? '停用' : '启用'} onClick={() => toggleTask(task)} icon={task.enabled ? <Pause className="h-3.5 w-3.5" /> : <Check className="h-3.5 w-3.5" />} />
                            <IconButton label="编辑" onClick={() => openEditTask(task)} icon={<Edit className="h-3.5 w-3.5" />} />
                            <IconButton label="删除" variant="secondary-destructive" onClick={() => deleteTask(task)} icon={<Trash className="h-3.5 w-3.5" />} />
                          </div>
                        </Table.Cell>
                      </Table.Row>
                    ))}
                  </Table.Body>
                </Table>
              </div>
            )}
          </section>
        )}

        {activeTab === 'workflows' && (
          <section className="space-y-3">
            {workflows.length === 0 ? (
              <Empty size="sm" icon={<GitBranch className="h-8 w-8 text-kumo-inactive" />} title="暂无工作流" description="将多个任务连接成 DAG，按成功、失败或完成条件自动编排。" contents={<Button size="sm" variant="primary" onClick={openCreateWorkflow}><Plus className="h-3.5 w-3.5" />新建工作流</Button>} />
            ) : (
              <div className="grid gap-3">
                {workflows.map((workflow) => (
                  <div key={workflow.id} className="rounded-md border border-kumo-line p-3">
                    <div className="mb-3 flex flex-wrap items-center justify-between gap-3">
                      <div>
                        <div className="flex items-center gap-2"><h3 className="text-sm font-semibold text-kumo-strong">{workflow.name}</h3><Badge variant={workflow.enabled ? 'success' : 'neutral'} appearance="dot">{workflow.enabled ? '启用' : '停用'}</Badge></div>
                        <div className="mt-1 text-xs text-kumo-subtle">{workflow.description || '无描述'} / {workflow.schedule ? `Cron ${workflow.schedule}` : '手动触发'} / {workflow.nodes?.length || 0} 个节点</div>
                      </div>
                      <div className="flex gap-1">
                        <IconButton label="运行工作流" onClick={() => runWorkflow(workflow)} icon={<Play className="h-3.5 w-3.5" />} />
                        <IconButton label="编辑工作流" onClick={() => openEditWorkflow(workflow)} icon={<Edit className="h-3.5 w-3.5" />} />
                        <IconButton label="删除工作流" variant="secondary-destructive" onClick={() => deleteWorkflow(workflow)} icon={<Trash className="h-3.5 w-3.5" />} />
                      </div>
                    </div>
                    <WorkflowCanvas workflow={workflow} runs={runs} />
                  </div>
                ))}
              </div>
            )}
          </section>
        )}

        {activeTab === 'runs' && (
          <section className="space-y-3">
            <div className="flex justify-end gap-2">
              <Button size="sm" variant="secondary" onClick={clearOldRuns}><Trash className="h-3.5 w-3.5" />清理 30 天前</Button>
              <Button size="sm" variant="secondary-destructive" onClick={clearAllRuns}><Trash className="h-3.5 w-3.5" />清空全部</Button>
            </div>
            {runs.length === 0 ? (
              <Empty size="sm" icon={<Activity className="h-8 w-8 text-kumo-inactive" />} title="暂无运行记录" description="手动运行任务或工作流后，会在这里看到状态、耗时和节点输出。" />
            ) : (
              <div className="overflow-x-auto rounded-md border border-kumo-line">
                <Table layout="fixed" className="min-w-[920px]">
                  <colgroup><col /><col className="w-[110px]" /><col className="w-[130px]" /><col className="w-[180px]" /><col className="w-[120px]" /><col className="w-[128px]" /></colgroup>
                  <Table.Header><Table.Row><Table.Head>运行对象</Table.Head><Table.Head>状态</Table.Head><Table.Head>触发方式</Table.Head><Table.Head>开始时间</Table.Head><Table.Head>耗时</Table.Head><Table.Head>操作</Table.Head></Table.Row></Table.Header>
                  <Table.Body>
                    {runs.map((run) => (
                      <Table.Row key={run.id}>
                        <Table.Cell><div className="font-semibold text-kumo-strong">{run.workflow_name}</div><div className="truncate text-xs text-kumo-subtle">{run.summary || '暂无摘要'}</div></Table.Cell>
                        <Table.Cell><Badge variant={statusBadgeVariant(run.status)} appearance="dot">{statusLabel(run.status)}</Badge></Table.Cell>
                        <Table.Cell className="text-xs text-kumo-subtle">{run.trigger_type === 'manual' ? '手动触发' : run.trigger_type}</Table.Cell>
                        <Table.Cell className="text-xs text-kumo-subtle">{formatTimestamp(run.start_time || run.created_at)}</Table.Cell>
                        <Table.Cell className="font-mono text-xs text-kumo-default">{run.duration ?? 0}s</Table.Cell>
                        <Table.Cell><div className="flex items-center gap-1"><IconButton label="查看详情" onClick={async () => {
                          const res = await fetch(`/api/scheduler/runs/${run.id}`, { headers: authHeaders() });
                          const data = await res.json();
                          if (data.success) setSelectedRun(data.data);
                        }} icon={<Eye className="h-3.5 w-3.5" />} />{run.status === 'failed' && <IconButton label="重试运行" onClick={() => retryRun(run)} icon={<RefreshCw className="h-3.5 w-3.5" />} />}{run.status === 'running' && <IconButton label="取消运行" variant="secondary-destructive" onClick={() => cancelRun(run)} icon={<X className="h-3.5 w-3.5" />} />}</div></Table.Cell>
                      </Table.Row>
                    ))}
                  </Table.Body>
                </Table>
              </div>
            )}
          </section>
        )}

        {activeTab === 'nodes' && (
          <section className="grid gap-3 sm:grid-cols-2 xl:grid-cols-3">
            {nodes.map((node) => (
              <div key={node.id} className="rounded-md border border-kumo-line p-3">
                <div className="flex items-center justify-between gap-3"><div className="flex items-center gap-2"><Server className="h-4 w-4 text-kumo-brand" /><div className="font-semibold text-kumo-strong">{node.name}</div></div><Badge variant={statusBadgeVariant(node.status)} appearance="dot">{statusLabel(node.status)}</Badge></div>
                <div className="mt-3 grid grid-cols-2 gap-2 text-xs">
                  <div><span className="text-kumo-subtle">类型</span><div className="mt-1 text-kumo-strong">{node.kind === 'local' ? '本机' : 'Agent'}</div></div>
                  <div><span className="text-kumo-subtle">并发</span><div className="mt-1 font-mono text-kumo-strong">{node.active_runs}/{node.max_concurrency}</div></div>
                  <div className="col-span-2"><span className="text-kumo-subtle">标签</span><div className="mt-1 flex flex-wrap gap-1">{(node.labels || []).length === 0 ? <span className="text-kumo-subtle">无</span> : node.labels.map((label) => <Badge key={label} variant="secondary">{label}</Badge>)}</div></div>
                </div>
                <div className="mt-3 text-xs text-kumo-subtle">{node.capability_note}</div>
              </div>
            ))}
          </section>
        )}

        <Dialog.Root open={taskDialogOpen} onOpenChange={setTaskDialogOpen}>
          <Dialog className="w-[calc(100vw-1rem)] max-w-[calc(100vw-1rem)] overflow-y-auto p-5 sm:w-full sm:max-w-3xl sm:p-6">
            <Dialog.Title className="mb-4 text-base font-bold text-kumo-strong">{taskForm.id ? '编辑任务' : '新建任务'}</Dialog.Title>
            <div className="space-y-4">
              <div className="grid gap-3 sm:grid-cols-2">
                <Input size="sm" label="名称" value={taskForm.name} onChange={(event) => setTaskForm((prev) => ({ ...prev, name: event.target.value }))} />
                <Select size="sm" label="执行节点" className="w-full" value={taskForm.node_id} onValueChange={(value) => setTaskForm((prev) => ({ ...prev, node_id: value }))} items={nodeItems} />
              </div>
              <Input size="sm" label="描述" value={taskForm.description} onChange={(event) => setTaskForm((prev) => ({ ...prev, description: event.target.value }))} />
              <CronEditor form={taskForm} setForm={setTaskForm} preview={cronPreview} previewError={cronPreviewError} />
              <div className="grid gap-3 sm:grid-cols-2">
                <Select size="sm" label="任务类型" className="w-full" value={taskForm.type} onValueChange={(value) => setTaskForm((prev) => ({ ...prev, type: value }))} items={TYPE_ITEMS} />
                <Input size="sm" label="节点标签选择器" value={taskForm.node_selector} onChange={(event) => setTaskForm((prev) => ({ ...prev, node_selector: event.target.value }))} />
              </div>
              <Textarea size="sm" label={taskForm.type === 'http' ? 'URL' : taskForm.type === 'internal' ? '内部路径 / METHOD 路径' : '命令'} value={taskForm.command} onChange={(event) => setTaskForm((prev) => ({ ...prev, command: event.target.value }))} rows={4} />
              <div className="grid gap-3 sm:grid-cols-4">
                <Input size="sm" type="number" label="超时秒数" min="1" value={taskForm.timeout_seconds} onChange={(event) => setTaskForm((prev) => ({ ...prev, timeout_seconds: Number(event.target.value) }))} />
                <Input size="sm" type="number" label="重试次数" min="0" value={taskForm.retry_count} onChange={(event) => setTaskForm((prev) => ({ ...prev, retry_count: Number(event.target.value) }))} />
                <Input size="sm" type="number" label="重试间隔" min="0" value={taskForm.retry_interval_seconds} onChange={(event) => setTaskForm((prev) => ({ ...prev, retry_interval_seconds: Number(event.target.value) }))} />
                <Input size="sm" type="number" label="最大并发" min="1" value={taskForm.max_concurrency} onChange={(event) => setTaskForm((prev) => ({ ...prev, max_concurrency: Number(event.target.value) }))} />
              </div>
              <div className="flex items-center justify-between rounded-md border border-kumo-line p-3"><span className="text-sm font-medium text-kumo-strong">启用任务</span><Switch checked={taskForm.enabled === 1} onCheckedChange={(checked) => setTaskForm((prev) => ({ ...prev, enabled: checked ? 1 : 0 }))} /></div>
              <div className="flex justify-end gap-2 pt-2"><Button size="sm" variant="secondary" onClick={() => setTaskDialogOpen(false)}><X className="h-3.5 w-3.5" />取消</Button><Button size="sm" variant="primary" onClick={saveTask} disabled={saving || Boolean(cronPreviewError)}><Save className="h-3.5 w-3.5" />保存</Button></div>
            </div>
          </Dialog>
        </Dialog.Root>

        <Dialog.Root open={workflowDialogOpen} onOpenChange={setWorkflowDialogOpen}>
          <Dialog className="w-[calc(100vw-1rem)] max-w-[calc(100vw-1rem)] overflow-y-auto p-5 sm:w-full sm:max-w-5xl sm:p-6">
            <Dialog.Title className="mb-4 text-base font-bold text-kumo-strong">{workflowForm.id ? '编辑工作流' : '新建工作流'}</Dialog.Title>
            <div className="grid gap-5 xl:grid-cols-[minmax(0,1fr)_22rem]">
              <div className="space-y-4">
                <div className="grid gap-3 sm:grid-cols-2"><Input size="sm" label="名称" value={workflowForm.name} onChange={(event) => setWorkflowForm((prev) => ({ ...prev, name: event.target.value }))} /><Input size="sm" label="Cron（留空为手动）" value={workflowForm.schedule} onChange={(event) => setWorkflowForm((prev) => ({ ...prev, schedule: event.target.value }))} /></div>
                <Input size="sm" label="描述" value={workflowForm.description} onChange={(event) => setWorkflowForm((prev) => ({ ...prev, description: event.target.value }))} />
                <WorkflowCanvas workflow={workflowForm} runs={[]} />
                <div className="flex gap-2">
                  <Button size="sm" variant="secondary" onClick={() => setWorkflowForm((prev) => {
                    const index = prev.nodes.length + 1;
                    return { ...prev, nodes: [...prev.nodes, { id: `task-${index}`, name: `任务 ${index}`, type: 'task', task_id: 0, enabled: 1, x: 80 + index * 150, y: 150 }] };
                  })}><Plus className="h-3.5 w-3.5" />新增节点</Button>
                  <Button size="sm" variant="secondary" onClick={() => setWorkflowForm((prev) => {
                    if (prev.nodes.length < 2) return prev;
                    const from = prev.nodes[prev.nodes.length - 2].id;
                    const to = prev.nodes[prev.nodes.length - 1].id;
                    return { ...prev, edges: [...prev.edges, { id: `edge-${prev.edges.length + 1}`, from, to, condition: 'success' }] };
                  })}><ArrowRight className="h-3.5 w-3.5" />连接最后两个节点</Button>
                </div>
              </div>
              <div className="space-y-4">
                <div className="rounded-md border border-kumo-line p-3">
                  <div className="mb-3 flex items-center gap-2 text-sm font-semibold text-kumo-strong"><Layers className="h-4 w-4" />节点</div>
                  <div className="space-y-3">
                    {workflowForm.nodes.map((node, index) => (
                      <div key={node.id} className="rounded-md border border-kumo-line p-2">
                        <Input size="sm" label="节点名称" value={node.name} onChange={(event) => setWorkflowForm((prev) => ({ ...prev, nodes: prev.nodes.map((item, i) => i === index ? { ...item, name: event.target.value } : item) }))} />
                        {node.type !== 'start' && (
                          <div className="mt-2 space-y-2">
                            <Select size="sm" label="引用任务" className="w-full" value={String(node.task_id || 0)} onValueChange={(value) => setWorkflowForm((prev) => ({ ...prev, nodes: prev.nodes.map((item, i) => i === index ? { ...item, task_id: Number(value), type: Number(value) ? 'task' : 'shell' } : item) }))} items={taskItems} />
                            {!node.task_id && <Textarea size="sm" label="内联命令" value={node.command || ''} onChange={(event) => setWorkflowForm((prev) => ({ ...prev, nodes: prev.nodes.map((item, i) => i === index ? { ...item, command: event.target.value } : item) }))} rows={2} />}
                            <div className="flex items-center justify-between"><span className="text-xs text-kumo-subtle">启用</span><Switch checked={node.enabled !== 0} onCheckedChange={(checked) => setWorkflowForm((prev) => ({ ...prev, nodes: prev.nodes.map((item, i) => i === index ? { ...item, enabled: checked ? 1 : 0 } : item) }))} /></div>
                          </div>
                        )}
                      </div>
                    ))}
                  </div>
                </div>
                <div className="rounded-md border border-kumo-line p-3">
                  <div className="mb-3 flex items-center gap-2 text-sm font-semibold text-kumo-strong"><Sliders className="h-4 w-4" />依赖边</div>
                  <div className="space-y-2">
                    {workflowForm.edges.map((edge, index) => (
                      <div key={edge.id} className="grid grid-cols-[1fr_1fr_1fr_auto] gap-2 items-center">
                        <Select size="sm" aria-label="来源" className="w-full" value={edge.from} onValueChange={(value) => setWorkflowForm((prev) => ({ ...prev, edges: prev.edges.map((item, i) => i === index ? { ...item, from: value } : item) }))} items={workflowForm.nodes.map((node) => ({ value: node.id, label: node.name }))} />
                        <Select size="sm" aria-label="目标" className="w-full" value={edge.to} onValueChange={(value) => setWorkflowForm((prev) => ({ ...prev, edges: prev.edges.map((item, i) => i === index ? { ...item, to: value } : item) }))} items={workflowForm.nodes.map((node) => ({ value: node.id, label: node.name }))} />
                        <Select size="sm" aria-label="条件" className="w-full" value={edge.condition} onValueChange={(value) => setWorkflowForm((prev) => ({ ...prev, edges: prev.edges.map((item, i) => i === index ? { ...item, condition: value } : item) }))} items={CONDITION_ITEMS} />
                        <IconButton label="删除边" variant="secondary-destructive" onClick={() => setWorkflowForm((prev) => ({ ...prev, edges: prev.edges.filter((_, i) => i !== index) }))} icon={<Trash className="h-3.5 w-3.5" />} />
                      </div>
                    ))}
                  </div>
                </div>
                <div className="flex items-center justify-between rounded-md border border-kumo-line p-3"><span className="text-sm font-medium text-kumo-strong">启用工作流</span><Switch checked={workflowForm.enabled === 1} onCheckedChange={(checked) => setWorkflowForm((prev) => ({ ...prev, enabled: checked ? 1 : 0 }))} /></div>
                <div className="flex justify-end gap-2"><Button size="sm" variant="secondary" onClick={() => setWorkflowDialogOpen(false)}>取消</Button><Button size="sm" variant="primary" onClick={saveWorkflow} disabled={saving}><Save className="h-3.5 w-3.5" />保存</Button></div>
              </div>
            </div>
          </Dialog>
        </Dialog.Root>

        <Dialog.Root open={Boolean(selectedRun)} onOpenChange={(open) => !open && setSelectedRun(null)}>
          <Dialog className="w-[calc(100vw-1rem)] max-w-[calc(100vw-1rem)] overflow-y-auto p-5 sm:w-full sm:max-w-4xl sm:p-6">
            <Dialog.Title className="mb-4 text-base font-bold text-kumo-strong">运行详情</Dialog.Title>
            {selectedRun && (
              <div className="space-y-4">
                {selectedRun.workflow && <WorkflowCanvas workflow={selectedRun.workflow} runs={[selectedRun]} />}
                <div className="grid gap-2">
                  {(selectedRun.node_runs || []).map((nodeRun) => (
                    <div key={nodeRun.id} className="rounded-md border border-kumo-line p-3">
                      <div className="flex items-center justify-between gap-3"><div className="font-semibold text-kumo-strong">{nodeRun.node_name}</div><Badge variant={statusBadgeVariant(nodeRun.status)} appearance="dot">{statusLabel(nodeRun.status)}</Badge></div>
                      <div className="mt-2 text-xs text-kumo-subtle">{formatTimestamp(nodeRun.start_time)} / {nodeRun.duration ?? 0}s</div>
                      <pre className="mt-2 max-h-60 overflow-auto whitespace-pre-wrap break-words rounded-md bg-kumo-recessed p-3 text-xs text-kumo-default">{nodeRun.output || '无输出'}</pre>
                      <Button size="sm" variant="secondary" className="mt-2" onClick={() => navigator.clipboard?.writeText(nodeRun.output || '')}><Copy className="h-3.5 w-3.5" />复制输出</Button>
                    </div>
                  ))}
                </div>
              </div>
            )}
          </Dialog>
        </Dialog.Root>
      </div>
    </TooltipProvider>
  );
}

export default SchedulerPage;
