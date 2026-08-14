import React, { useCallback, useEffect, useMemo, useState } from 'react';
import { toast } from '../modules/toast.js';
import { useConfirmPress } from '../hooks/useConfirmPress.js';
import { Button } from '@cloudflare/kumo/components/button';
import { Dialog } from '@cloudflare/kumo/components/dialog';
import { Input } from '@cloudflare/kumo/components/input';
import { Select } from '@cloudflare/kumo/components/select';
import { Switch } from '@cloudflare/kumo/components/switch';
import { SkeletonLine } from '@cloudflare/kumo/components/loader';
import { Table } from '@cloudflare/kumo/components/table';
import { Badge } from '@cloudflare/kumo/components/badge';
import { Empty } from '@cloudflare/kumo/components/empty';
import { Flow } from '@cloudflare/kumo/components/flow';
import { Tooltip, TooltipProvider } from '@cloudflare/kumo/components/tooltip';
import { LayerCard, Tabs } from '@cloudflare/kumo';
import { MODULE_TABS_PROPS, TOOL_TABS_PROPS } from '../modules/kumoTabs.js';
import { SectionCard, TabBarOverflowActions, stickyTabsBaseClass } from '../components/ui/AppPrimitives.jsx';
import CodeEditor from '../components/ui/CodeEditor.jsx';
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
  if (status === 'running' || status === 'queued') return 'warning';
  if (status === 'skipped' || status === 'unknown') return 'neutral';
  return 'none';
}

function statusLabel(status) {
  return STATUS_LABELS[status] || (status === 'online' ? '在线' : status === 'offline' ? '离线' : status || '未知');
}

function taskTypeLabel(value) {
  return TYPE_ITEMS.find((item) => item.value === value)?.label || value || 'Shell 命令';
}

function workflowNodeTypeLabel(node) {
  if (node.type === 'start') return '开始节点';
  if (node.task_id) return `任务 #${node.task_id}`;
  if (node.type === 'task') return '未绑定任务';
  return node.command ? '内联命令' : taskTypeLabel(node.type);
}

function workflowNodeKindLabel(node) {
  if (node.type === 'start') return '入口';
  if (node.task_id) return '引用任务';
  if (node.type === 'task') return '待绑定';
  return '内联';
}

function workflowNodeKindVariant(node) {
  if (node.type === 'start') return 'blue';
  if (node.task_id) return 'purple';
  if (node.type === 'task') return 'orange';
  return 'teal';
}

function conditionLabel(value) {
  return CONDITION_ITEMS.find((item) => item.value === value)?.label || '成功后';
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

function compareWorkflowNodes(a, b) {
  if (a.type === 'start' && b.type !== 'start') return -1;
  if (a.type !== 'start' && b.type === 'start') return 1;
  const xDiff = (a.x ?? 0) - (b.x ?? 0);
  if (xDiff !== 0) return xDiff;
  const yDiff = (a.y ?? 0) - (b.y ?? 0);
  if (yDiff !== 0) return yDiff;
  return String(a.name || a.id).localeCompare(String(b.name || b.id), 'zh-CN');
}

function sortWorkflowNodes(nodes = []) {
  return [...nodes].sort(compareWorkflowNodes);
}

function sortWorkflowEdges(edges = [], nodeMap) {
  return [...edges].sort((a, b) => {
    const from = nodeMap.get(a.to);
    const to = nodeMap.get(b.to);
    if (!from || !to) return 0;
    return compareWorkflowNodes(from, to);
  });
}

function getValidWorkflowEdges(nodes = [], edges = []) {
  const nodeMap = new Map(nodes.map((node) => [node.id, node]));
  return edges.filter((edge) => nodeMap.has(edge.from) && nodeMap.has(edge.to));
}

function buildWorkflowFlowStages(nodes = [], edges = []) {
  const sortedNodes = sortWorkflowNodes(nodes);
  const nodeMap = new Map(nodes.map((node) => [node.id, node]));
  const validEdges = getValidWorkflowEdges(nodes, edges);
  const incomingCount = new Map(nodes.map((node) => [node.id, 0]));
  const remainingIncoming = new Map(nodes.map((node) => [node.id, 0]));
  const outgoing = new Map(nodes.map((node) => [node.id, []]));

  validEdges.forEach((edge) => {
    outgoing.get(edge.from).push(edge);
    incomingCount.set(edge.to, (incomingCount.get(edge.to) || 0) + 1);
    remainingIncoming.set(edge.to, (remainingIncoming.get(edge.to) || 0) + 1);
  });

  outgoing.forEach((items, nodeId) => {
    outgoing.set(nodeId, sortWorkflowEdges(items, nodeMap));
  });

  const levels = new Map();
  const visited = new Set();
  let queue = sortedNodes.filter((node) => (incomingCount.get(node.id) || 0) === 0);
  queue.forEach((node) => levels.set(node.id, 0));
  if (queue.length === 0 && sortedNodes[0]) {
    queue = [sortedNodes[0]];
    levels.set(sortedNodes[0].id, 0);
  }

  while (queue.length > 0) {
    const node = queue.shift();
    if (!node || visited.has(node.id)) continue;
    visited.add(node.id);
    const currentLevel = levels.get(node.id) || 0;

    (outgoing.get(node.id) || []).forEach((edge) => {
      const nextNode = nodeMap.get(edge.to);
      if (!nextNode) return;
      levels.set(nextNode.id, Math.max(levels.get(nextNode.id) || 0, currentLevel + 1));
      remainingIncoming.set(nextNode.id, (remainingIncoming.get(nextNode.id) || 0) - 1);
      if ((remainingIncoming.get(nextNode.id) || 0) <= 0) {
        queue.push(nextNode);
        queue = sortWorkflowNodes(queue);
      }
    });
  }

  let fallbackLevel = Math.max(0, ...Array.from(levels.values())) + 1;
  sortedNodes.forEach((node) => {
    if (visited.has(node.id)) return;
    levels.set(node.id, fallbackLevel);
    fallbackLevel += 1;
  });

  const stages = new Map();
  sortedNodes.forEach((node) => {
    const level = levels.get(node.id) || 0;
    if (!stages.has(level)) stages.set(level, []);
    stages.get(level).push(node);
  });

  return Array.from(stages.entries())
    .sort(([a], [b]) => a - b)
    .map(([, stageNodes]) => stageNodes);
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
          <div className="text-xs text-kumo-subtle">简易周期自动生成表达式。</div>
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
        <div className="grid gap-3 cq-sm:grid-cols-2">
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

      <div className="grid gap-3 cq-sm:grid-cols-[minmax(0,1fr)_minmax(0,1.3fr)]">
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

function WorkflowCanvas({ workflow, runs = [], selectedNodeId = '', onSelectNode = null, size = 'default' }) {
  const nodes = workflow.nodes || [];
  const edges = workflow.edges || [];
  const compact = size === 'compact';
  const editor = size === 'editor';
  const latestRun = runs.find((run) => run.workflow_id === workflow.id);
  const nodeStatus = Object.fromEntries((latestRun?.node_runs || []).map((run) => [run.node_id, run.status]));
  const validEdges = useMemo(() => getValidWorkflowEdges(nodes, edges), [nodes, edges]);
  const stages = useMemo(() => buildWorkflowFlowStages(nodes, edges), [nodes, edges]);
  const incomingEdges = useMemo(() => {
    const grouped = new Map();
    validEdges.forEach((edge) => {
      if (!grouped.has(edge.to)) grouped.set(edge.to, []);
      grouped.get(edge.to).push(edge);
    });
    return grouped;
  }, [validEdges]);

  const renderNode = (node) => {
    const status = nodeStatus[node.id];
    const selected = selectedNodeId === node.id;
    const dependencies = incomingEdges.get(node.id) || [];
    const dependencyText = dependencies.length > 1
      ? `${dependencies.length} 条依赖`
      : dependencies[0]
        ? conditionLabel(dependencies[0].condition)
        : '';

    return (
      <Flow.Node
        key={node.id}
        id={node.id}
        disabled={node.enabled === 0}
        render={(
          <li
            role={onSelectNode ? 'button' : undefined}
            tabIndex={onSelectNode ? 0 : undefined}
            aria-label={onSelectNode ? `选择节点 ${node.name || node.id}` : undefined}
            aria-pressed={onSelectNode ? selected : undefined}
            onClick={onSelectNode ? () => onSelectNode(node.id) : undefined}
            onKeyDown={onSelectNode ? (event) => {
              if (event.key !== 'Enter' && event.key !== ' ') return;
              event.preventDefault();
              onSelectNode(node.id);
            } : undefined}
            style={{ cursor: onSelectNode ? 'pointer' : 'default' }}
            className={`flex flex-col rounded-md border bg-kumo-base text-left shadow-sm transition-colors focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-kumo-brand/45 ${compact ? 'min-h-[76px] w-52 px-3 py-2.5' : 'min-h-[108px] w-60 px-4 py-3'} ${selected ? 'border-kumo-brand ring-2 ring-kumo-brand/25' : 'border-kumo-line hover:border-kumo-brand/50'} ${node.enabled === 0 ? 'opacity-60' : ''}`}
          >
            <span className={`flex min-w-0 items-start justify-between ${compact ? 'gap-2' : 'gap-3'}`}>
              <span className="min-w-0">
                <span className="block truncate text-sm font-semibold text-kumo-strong">{node.name || node.id}</span>
                <span className="mt-1 block truncate text-xs leading-5 text-kumo-subtle">{workflowNodeTypeLabel(node)}</span>
              </span>
              <Badge variant={workflowNodeKindVariant(node)}>{workflowNodeKindLabel(node)}</Badge>
            </span>
            <span className={`mt-auto flex min-w-0 items-center justify-between gap-2 ${compact ? 'pt-3' : 'pt-5'}`}>
              <Badge variant={statusBadgeVariant(status || (node.enabled === 0 ? 'skipped' : 'queued'))} appearance="dot">
                {node.enabled === 0 ? '停用' : status ? statusLabel(status) : '待运行'}
              </Badge>
              {dependencyText && (
                <span className="min-w-0 truncate text-xs text-kumo-subtle">
                  {dependencyText}
                </span>
              )}
            </span>
          </li>
        )}
      />
    );
  };

  if (nodes.length === 0) {
    return (
      <div className="rounded-md border border-kumo-line bg-kumo-recessed/30 px-3 py-8 text-center text-xs text-kumo-subtle">
        暂无节点
      </div>
    );
  }

  return (
    <Flow
      orientation="horizontal"
      align="center"
      className={`scheduler-workflow-canvas ${compact ? 'min-h-40 w-full' : 'min-h-[360px]'} ${editor ? 'scheduler-workflow-canvas-editor' : ''} rounded-md border border-kumo-line bg-kumo-base`}
      padding={compact ? { x: 24, y: 24 } : { x: 56, y: 64 }}
    >
      {stages.map((stage, index) => {
        if (stage.length === 1) return renderNode(stage[0]);
        return (
          <Flow.Parallel key={`stage-${index}`}>
            {stage.map((node) => renderNode(node))}
          </Flow.Parallel>
        );
      })}
    </Flow>
  );
}

function SchedulerPage() {
  const { isArmed, confirmPress } = useConfirmPress();
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
  const [selectedWorkflowNodeId, setSelectedWorkflowNodeId] = useState('task-1');
  const [cronPreview, setCronPreview] = useState(null);
  const [cronPreviewError, setCronPreviewError] = useState('');
  const [selectedRun, setSelectedRun] = useState(null);
  const [workflowCanvasEpoch, setWorkflowCanvasEpoch] = useState(0);

  const workflowNodeTabs = useMemo(() => workflowForm.nodes.map((node) => ({
    value: node.id,
    label: node.name || node.id,
  })), [workflowForm.nodes]);

  const authHeaders = useCallback(() => ({
    'Content-Type': 'application/json',
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

  useEffect(() => {
    if (!workflowDialogOpen) return undefined;
    let firstFrame = 0;
    let secondFrame = 0;
    const timer = window.setTimeout(() => setWorkflowCanvasEpoch((value) => value + 1), 180);
    firstFrame = window.requestAnimationFrame(() => {
      secondFrame = window.requestAnimationFrame(() => setWorkflowCanvasEpoch((value) => value + 1));
    });
    return () => {
      window.clearTimeout(timer);
      window.cancelAnimationFrame(firstFrame);
      window.cancelAnimationFrame(secondFrame);
    };
  }, [workflowDialogOpen]);

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
    if (!confirmPress(`task:${task.id}`, `删除任务「${task.name}」`)) return;
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
    const nextForm = cloneWorkflowForm();
    const firstTask = tasks[0];
    if (firstTask) {
      nextForm.nodes = nextForm.nodes.map((node) => (node.id === 'task-1'
        ? { ...node, name: firstTask.name || node.name, task_id: Number(firstTask.id) || 0, type: 'task' }
        : node));
    }
    setWorkflowForm(nextForm);
    setSelectedWorkflowNodeId(nextForm.nodes.find((node) => node.type !== 'start')?.id || nextForm.nodes[0]?.id || '');
    setWorkflowDialogOpen(true);
  };

  const openEditWorkflow = (workflow) => {
    const nextForm = cloneWorkflowForm(workflow);
    setWorkflowForm(nextForm);
    setSelectedWorkflowNodeId(nextForm.nodes.find((node) => node.type !== 'start')?.id || nextForm.nodes[0]?.id || '');
    setWorkflowDialogOpen(true);
  };

  const addWorkflowNode = () => {
    setWorkflowForm((prev) => {
      const index = prev.nodes.length + 1;
      const firstTask = tasks[0];
      const node = {
        id: `task-${Date.now()}`,
        name: firstTask?.name || `任务 ${index}`,
        type: 'task',
        task_id: Number(firstTask?.id) || 0,
        enabled: 1,
        x: 80 + index * 150,
        y: 150,
      };
      setSelectedWorkflowNodeId(node.id);
      return { ...prev, nodes: [...prev.nodes, node] };
    });
  };

  const updateWorkflowNode = (nodeId, patch) => {
    setWorkflowForm((prev) => ({
      ...prev,
      nodes: prev.nodes.map((node) => (node.id === nodeId ? { ...node, ...patch } : node)),
    }));
  };

  const updateWorkflowNodeTask = (node, value) => {
    const taskId = Number(value) || 0;
    const linkedTask = tasks.find((task) => Number(task.id) === taskId);
    const shouldAdoptTaskName = taskId && linkedTask && (!node.name || /^任务\s+\d+$/.test(node.name));
    updateWorkflowNode(node.id, {
      task_id: taskId,
      type: taskId ? 'task' : 'shell',
      ...(shouldAdoptTaskName ? { name: linkedTask.name || node.name } : {}),
    });
  };

  const deleteWorkflowNode = (nodeId) => {
    setWorkflowForm((prev) => {
      const nextNodes = prev.nodes.filter((node) => node.id !== nodeId);
      setSelectedWorkflowNodeId(nextNodes.find((node) => node.type !== 'start')?.id || nextNodes[0]?.id || '');
      return {
        ...prev,
        nodes: nextNodes,
        edges: prev.edges.filter((edge) => edge.from !== nodeId && edge.to !== nodeId),
      };
    });
  };

  const addWorkflowEdge = () => {
    setWorkflowForm((prev) => {
      if (prev.nodes.length < 2) return prev;
      const from = prev.nodes[Math.max(0, prev.nodes.length - 2)].id;
      const to = prev.nodes[prev.nodes.length - 1].id;
      return {
        ...prev,
        edges: [...prev.edges, { id: `edge-${Date.now()}`, from, to, condition: 'success' }],
      };
    });
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
    if (!confirmPress(`workflow:${workflow.id}`, `删除工作流「${workflow.name}」`)) return;
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
    if (!confirmPress('runs:clear-old', '清理 30 天前运行记录')) return;
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
    if (!confirmPress('runs:clear-all', '清空全部运行记录')) return;
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
    totalNodes: nodes.length,
    onlineNodes: nodes.filter((node) => node.status === 'online').length,
    agentNodes: nodes.filter((node) => node.kind === 'agent').length,
    totalNodeConcurrency: nodes.reduce((sum, node) => sum + (Number(node.max_concurrency) || 0), 0),
  }), [nodes, runs, tasks, workflows]);

  const summaryItems = activeTab === 'nodes'
    ? [
      {
        label: '节点总数',
        value: stats.totalNodes,
        icon: <Server className="h-5 w-5 text-kumo-info" />,
        cardClassName: 'bg-kumo-info/6',
      },
      {
        label: '在线节点',
        value: stats.onlineNodes,
        icon: <Check className="h-5 w-5 text-kumo-success" />,
        cardClassName: 'bg-kumo-success/6',
      },
      {
        label: 'Agent 节点',
        value: stats.agentNodes,
        icon: <GitBranch className="h-5 w-5 text-kumo-warning" />,
        cardClassName: 'bg-kumo-warning/8',
      },
      {
        label: '总并发',
        value: stats.totalNodeConcurrency,
        icon: <Activity className="h-5 w-5 text-kumo-brand" />,
        cardClassName: 'bg-kumo-brand/7',
      },
    ]
    : [
      {
        label: '任务总数',
        value: stats.totalTasks,
        icon: <Clock className="h-5 w-5 text-kumo-info" />,
        cardClassName: 'bg-kumo-info/6',
      },
      {
        label: '启用任务',
        value: stats.enabledTasks,
        icon: <Check className="h-5 w-5 text-kumo-success" />,
        cardClassName: 'bg-kumo-success/6',
      },
      {
        label: '工作流',
        value: stats.workflows,
        icon: <GitBranch className="h-5 w-5 text-kumo-warning" />,
        cardClassName: 'bg-kumo-warning/8',
      },
      {
        label: '失败运行',
        value: stats.failedRuns,
        icon: <Activity className="h-5 w-5 text-kumo-danger" />,
        cardClassName: 'bg-kumo-danger/6',
      },
    ];

  const nodeItems = useMemo(() => nodes.map((node) => ({ value: node.id, label: `${node.name}（${node.kind === 'local' ? '本机' : 'Agent'}）` })), [nodes]);
  const taskItems = useMemo(() => [{ value: '0', label: '内联命令' }, ...tasks.map((task) => ({ value: String(task.id), label: `${task.name} #${task.id}` }))], [tasks]);
  const workflowNodeItems = useMemo(() => workflowForm.nodes.map((node) => ({ value: node.id, label: node.name || node.id })), [workflowForm.nodes]);
  const selectedWorkflowNode = workflowForm.nodes.find((node) => node.id === selectedWorkflowNodeId) || workflowForm.nodes[0] || null;
  const taskCommandLabel = taskForm.type === 'http' ? 'URL' : taskForm.type === 'internal' ? '内部接口路径' : '命令';
  const taskCommandPlaceholder = taskForm.type === 'http'
    ? 'https://example.com/health'
    : taskForm.type === 'internal'
      ? 'GET /health'
      : 'echo hello';

  return (
    <TooltipProvider>
      <div className="flex w-full min-w-0 flex-col gap-3 cq-sm:gap-4">
        <div className={`${stickyTabsBaseClass} justify-between gap-2 border-b border-kumo-line [&>*]:min-w-0`}>
          <Tabs
            {...MODULE_TABS_PROPS}
            value={activeTab}
            onValueChange={setActiveTab}
            tabs={TASK_TABS}
          />
          <TabBarOverflowActions
            items={[
              {
                key: 'refresh',
                label: '刷新',
                icon: <RefreshCw className={`h-3.5 w-3.5 ${loading ? 'animate-spin' : ''}`} />,
                onClick: loadAll,
                loading,
              },
              ...(activeTab === 'workflows'
                ? [
                    {
                      key: 'import',
                      label: '导入工作流',
                      icon: <Download className="h-3.5 w-3.5" />,
                      onClick: importWorkflows,
                    },
                    {
                      key: 'export',
                      label: '导出工作流',
                      icon: <Upload className="h-3.5 w-3.5" />,
                      onClick: exportWorkflows,
                    },
                  ]
                : []),
              {
                key: 'create',
                label: activeTab === 'workflows' ? '新建工作流' : '新建任务',
                icon: <Plus className="h-3.5 w-3.5" />,
                onClick: activeTab === 'workflows' ? openCreateWorkflow : openCreateTask,
                variant: 'primary',
              },
            ]}
          />
        </div>

        <div className="grid grid-cols-4 gap-2 cq-sm:gap-3">
          {summaryItems.map(({ label, value, icon, cardClassName }) => (
            <LayerCard key={label} className={`min-w-0 p-2 cq-sm:p-3 ${cardClassName || ''}`}>
              <div className="flex items-center justify-between gap-2 text-[11px] text-kumo-subtle cq-sm:gap-3 cq-sm:text-xs">
                <span className="truncate">{label}</span>
                <span className="shrink-0">{icon}</span>
              </div>
              <div className="mt-1 font-mono text-base font-bold text-kumo-strong cq-sm:text-lg">{value}</div>
            </LayerCard>
          ))}
        </div>


        {activeTab === 'tasks' && (
          <SectionCard
            title="任务列表"
            icon={<Clock className="h-4 w-4 text-kumo-brand" />}
            bodyPadding="none"
          >
            {loading ? (
              <div className="p-4"><SkeletonLine className="h-28" /></div>
            ) : tasks.length === 0 ? (
              <Empty size="sm" className="rounded-none border-0 bg-transparent" icon={<Clock className="h-8 w-8 text-kumo-inactive" />} title="暂无任务" description="创建后可作为定时任务或工作流节点。" contents={<Button size="sm" variant="primary" onClick={openCreateTask}><Plus className="h-3.5 w-3.5" />新建任务</Button>} />
            ) : (
              <div className="overflow-x-auto">
                <Table layout="fixed" className="min-w-[1080px]">
                  <colgroup><col className="w-[220px]" /><col className="w-[104px]" /><col className="w-[128px]" /><col className="w-[190px]" /><col className="w-[180px]" /><col className="w-[180px]" /><col className="w-[160px]" /></colgroup>
                  <Table.Header><Table.Row><Table.Head>任务</Table.Head><Table.Head>状态</Table.Head><Table.Head>类型</Table.Head><Table.Head>周期</Table.Head><Table.Head>下次运行</Table.Head><Table.Head>最近结果</Table.Head><Table.Head className="app-table-action">操作</Table.Head></Table.Row></Table.Header>
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
                            <IconButton label="删除" variant={isArmed(`task:${task.id}`) ? 'destructive' : 'secondary-destructive'} onClick={() => deleteTask(task)} icon={<Trash className="h-3.5 w-3.5" />} />
                          </div>
                        </Table.Cell>
                      </Table.Row>
                    ))}
                  </Table.Body>
                </Table>
              </div>
            )}
          </SectionCard>
        )}

        {activeTab === 'workflows' && (
          <SectionCard
            title="工作流编排"
            icon={<GitBranch className="h-4 w-4 text-kumo-brand" />}
            bodyClassName={workflows.length === 0 ? '' : 'space-y-3'}
            bodyPadding={workflows.length === 0 ? 'none' : 'md'}
          >
            {workflows.length === 0 ? (
              <Empty size="sm" className="rounded-none border-0 bg-transparent" icon={<GitBranch className="h-8 w-8 text-kumo-inactive" />} title="暂无工作流" description="创建后可按 DAG 编排任务。" contents={<Button size="sm" variant="primary" onClick={openCreateWorkflow}><Plus className="h-3.5 w-3.5" />新建工作流</Button>} />
            ) : (
              <div className="grid gap-3 cq-lg:grid-cols-2">
                {workflows.map((workflow) => (
                  <div key={workflow.id} className="scheduler-workflow-card flex h-full flex-col rounded-md border border-kumo-line p-3">
                    <div className="grid min-h-0 flex-1 gap-3 cq-lg:grid-cols-[minmax(0,220px)_minmax(0,1fr)]">
                      <div className="flex min-h-0 min-w-0 flex-col gap-2.5">
                        <div className="flex flex-wrap items-start justify-between gap-2">
                          <div className="min-w-0 flex-1">
                            <div className="flex min-w-0 flex-wrap items-center gap-2">
                              <h3 className="truncate text-sm font-semibold text-kumo-strong">{workflow.name}</h3>
                              <Badge variant={workflow.enabled ? 'success' : 'neutral'} appearance="dot">{workflow.enabled ? '启用' : '停用'}</Badge>
                            </div>
                            <div className="mt-1 line-clamp-2 text-xs text-kumo-subtle">{workflow.description || '无描述'}</div>
                          </div>
                          <div className="flex shrink-0 gap-1">
                            <IconButton label="运行工作流" onClick={() => runWorkflow(workflow)} icon={<Play className="h-3.5 w-3.5" />} />
                            <IconButton label="编辑工作流" onClick={() => openEditWorkflow(workflow)} icon={<Edit className="h-3.5 w-3.5" />} />
                            <IconButton label="删除工作流" variant={isArmed(`workflow:${workflow.id}`) ? 'destructive' : 'secondary-destructive'} onClick={() => deleteWorkflow(workflow)} icon={<Trash className="h-3.5 w-3.5" />} />
                          </div>
                        </div>
                        <div className="grid gap-2 cq-sm:grid-cols-2 cq-lg:grid-cols-1">
                          <div className="rounded-md border border-kumo-line bg-kumo-recessed/25 px-2.5 py-2">
                            <div className="text-[11px] text-kumo-subtle">触发方式</div>
                            <div className="mt-1 truncate font-mono text-xs text-kumo-strong">{workflow.schedule ? `Cron ${workflow.schedule}` : '手动触发'}</div>
                          </div>
                          <div className="rounded-md border border-kumo-line bg-kumo-recessed/25 px-2.5 py-2">
                            <div className="text-[11px] text-kumo-subtle">节点规模</div>
                            <div className="mt-1 text-xs font-semibold text-kumo-strong">{workflow.nodes?.length || 0} 个节点</div>
                          </div>
                        </div>
                      </div>

                      <div className="flex min-h-0 min-w-0 overflow-visible">
                        <WorkflowCanvas workflow={workflow} runs={runs} size="compact" />
                      </div>
                    </div>
                  </div>
                ))}
              </div>
            )}
          </SectionCard>
        )}

        {activeTab === 'runs' && (
          <SectionCard
            title="运行记录"
            icon={<Activity className="h-4 w-4 text-kumo-brand" />}
            actions={(
              <>
                <Button size="sm" variant={isArmed('runs:clear-old') ? 'destructive' : 'secondary-destructive'} onClick={clearOldRuns}><Trash className="h-3.5 w-3.5" />清理 30 天前</Button>
                <Button size="sm" variant={isArmed('runs:clear-all') ? 'destructive' : 'secondary-destructive'} onClick={clearAllRuns}><Trash className="h-3.5 w-3.5" />清空全部</Button>
              </>
            )}
            bodyPadding="none"
          >
            {runs.length === 0 ? (
              <Empty size="sm" className="rounded-none border-0 bg-transparent" icon={<Activity className="h-8 w-8 text-kumo-inactive" />} title="暂无运行记录" description="运行后显示结果" />
            ) : (
              <div className="overflow-x-auto">
                <Table layout="fixed" className="min-w-[920px]">
                  <colgroup><col /><col className="w-[110px]" /><col className="w-[130px]" /><col className="w-[180px]" /><col className="w-[120px]" /><col className="w-[128px]" /></colgroup>
                  <Table.Header><Table.Row><Table.Head>运行对象</Table.Head><Table.Head>状态</Table.Head><Table.Head>触发方式</Table.Head><Table.Head>开始时间</Table.Head><Table.Head>耗时</Table.Head><Table.Head className="app-table-action">操作</Table.Head></Table.Row></Table.Header>
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
          </SectionCard>
        )}

        {activeTab === 'nodes' && (
          <SectionCard
            title="执行节点"
            icon={<Server className="h-4 w-4 text-kumo-brand" />}
            bodyPadding="none"
          >
            {nodes.length === 0 ? (
              <Empty size="sm" className="rounded-none border-0 bg-transparent" icon={<Server className="h-8 w-8 text-kumo-inactive" />} title="暂无执行节点" description="本机默认作为执行节点。" />
            ) : (
              <div className="overflow-x-auto">
                <Table layout="fixed" className="w-full min-w-[760px]">
                  <colgroup>
                    <col style={{ width: '18%' }} />
                    <col style={{ width: '10%' }} />
                    <col style={{ width: '8%' }} />
                    <col style={{ width: '8%' }} />
                    <col style={{ width: '16%' }} />
                    <col style={{ width: '40%' }} />
                  </colgroup>
                  <Table.Header><Table.Row><Table.Head>节点</Table.Head><Table.Head>状态</Table.Head><Table.Head>类型</Table.Head><Table.Head>并发</Table.Head><Table.Head>标签</Table.Head><Table.Head>说明</Table.Head></Table.Row></Table.Header>
                  <Table.Body>
                    {nodes.map((node) => (
                      <Table.Row key={node.id}>
                        <Table.Cell>
                          <div className="flex min-w-0 items-center gap-2">
                            <Server className="h-4 w-4 shrink-0 text-kumo-brand" />
                            <div className="min-w-0">
                              <div className="truncate font-semibold text-kumo-strong">{node.name}</div>
                              <div className="truncate text-xs text-kumo-subtle">{node.id}</div>
                            </div>
                          </div>
                        </Table.Cell>
                        <Table.Cell><Badge variant={statusBadgeVariant(node.status)} appearance="dot">{statusLabel(node.status)}</Badge></Table.Cell>
                        <Table.Cell className="text-xs text-kumo-default">{node.kind === 'local' ? '本机' : 'Agent'}</Table.Cell>
                        <Table.Cell className="font-mono text-xs text-kumo-strong">{node.active_runs ?? 0}/{node.max_concurrency ?? 0}</Table.Cell>
                        <Table.Cell>
                          <div className="flex flex-wrap gap-1">
                            {(node.labels || []).length === 0 ? <span className="text-xs text-kumo-subtle">无</span> : node.labels.map((label) => <Badge key={label} variant="secondary">{label}</Badge>)}
                          </div>
                        </Table.Cell>
                        <Table.Cell className="truncate text-xs text-kumo-subtle">{node.capability_note || '-'}</Table.Cell>
                      </Table.Row>
                    ))}
                  </Table.Body>
                </Table>
              </div>
            )}
          </SectionCard>
        )}

        <Dialog.Root open={taskDialogOpen} onOpenChange={setTaskDialogOpen}>
          <Dialog className="@container w-[calc(100vw-1rem)] max-w-[calc(100vw-1rem)] overflow-y-auto p-5 cq-sm:w-full cq-sm:max-w-3xl cq-sm:p-6">
            <Dialog.Title className="mb-4 text-base font-bold text-kumo-strong">{taskForm.id ? '编辑任务' : '新建任务'}</Dialog.Title>
            <div className="space-y-4">
              <div className="grid gap-3 cq-sm:grid-cols-2">
                <Input size="sm" label="名称" value={taskForm.name} onChange={(event) => setTaskForm((prev) => ({ ...prev, name: event.target.value }))} />
                <Select size="sm" label="执行节点" className="w-full" value={taskForm.node_id} onValueChange={(value) => setTaskForm((prev) => ({ ...prev, node_id: value }))} items={nodeItems} />
              </div>
              <Input size="sm" label="描述" value={taskForm.description} onChange={(event) => setTaskForm((prev) => ({ ...prev, description: event.target.value }))} />
              <CronEditor form={taskForm} setForm={setTaskForm} preview={cronPreview} previewError={cronPreviewError} />
              <div className="grid gap-3 cq-sm:grid-cols-2">
                <Select size="sm" label="任务类型" className="w-full" value={taskForm.type} onValueChange={(value) => setTaskForm((prev) => ({ ...prev, type: value }))} items={TYPE_ITEMS} />
                <Input size="sm" label="节点标签选择器" value={taskForm.node_selector} onChange={(event) => setTaskForm((prev) => ({ ...prev, node_selector: event.target.value }))} />
              </div>
              {taskForm.type === 'shell' || taskForm.type === 'agent' ? (
                <CodeEditor label={taskCommandLabel} language="shell" placeholder={taskCommandPlaceholder} value={taskForm.command} onChange={(command) => setTaskForm((prev) => ({ ...prev, command }))} minHeight="8rem" />
              ) : (
                <Input size="sm" label={taskCommandLabel} placeholder={taskCommandPlaceholder} value={taskForm.command} onChange={(event) => setTaskForm((prev) => ({ ...prev, command: event.target.value }))} />
              )}
              <div className="grid gap-3 cq-sm:grid-cols-4">
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
          <Dialog className="@container flex h-[calc(100dvh-1rem)] w-[calc(100vw-1rem)] max-w-[calc(100vw-1rem)] flex-col overflow-hidden p-5 cq-sm:w-[calc(100vw-2rem)] cq-sm:max-w-[92rem] cq-sm:p-6">
            <Dialog.Title className="mb-4 shrink-0 text-base font-bold text-kumo-strong">{workflowForm.id ? '编辑工作流' : '新建工作流'}</Dialog.Title>
            <div className="flex min-h-0 flex-1 flex-col gap-4">
              <div className="shrink-0 rounded-md border border-kumo-line p-3">
                <div className="grid gap-3 cq-lg:grid-cols-[minmax(0,1fr)_8rem]">
                  <div className="grid min-w-0 gap-3 cq-sm:grid-cols-2">
                    <Input size="sm" label="名称" value={workflowForm.name} onChange={(event) => setWorkflowForm((prev) => ({ ...prev, name: event.target.value }))} />
                    <Input size="sm" label="Cron（留空为手动）" value={workflowForm.schedule} onChange={(event) => setWorkflowForm((prev) => ({ ...prev, schedule: event.target.value }))} />
                  </div>
                  <div className="flex h-9 items-center justify-between gap-3 self-end rounded-md border border-kumo-line px-3">
                    <span className="text-sm font-medium text-kumo-strong">启用</span>
                    <Switch checked={workflowForm.enabled === 1} onCheckedChange={(checked) => setWorkflowForm((prev) => ({ ...prev, enabled: checked ? 1 : 0 }))} />
                  </div>
                </div>
                <div className="mt-3">
                  <Input size="sm" label="描述" value={workflowForm.description} onChange={(event) => setWorkflowForm((prev) => ({ ...prev, description: event.target.value }))} />
                </div>
              </div>

              <div className="grid min-h-0 flex-1 gap-4 overflow-y-auto pr-1 cq-xl:grid-cols-[minmax(0,1fr)_26rem] cq-xl:overflow-hidden cq-xl:pr-0">
                <div className="flex min-h-0 flex-col gap-3">
                  <WorkflowCanvas key={`workflow-editor-${workflowCanvasEpoch}`} workflow={workflowForm} runs={[]} selectedNodeId={selectedWorkflowNode?.id} onSelectNode={setSelectedWorkflowNodeId} size="editor" />
                  <div className="flex shrink-0 flex-wrap items-center justify-between gap-2 rounded-md border border-kumo-line p-3">
                    <Tabs
                      {...TOOL_TABS_PROPS}
                      value={selectedWorkflowNode?.id || selectedWorkflowNodeId}
                      onValueChange={setSelectedWorkflowNodeId}
                      tabs={workflowNodeTabs}
                      className="min-w-0 max-w-full"
                      listClassName="max-w-full overflow-x-auto"
                    />
                    <div className="flex shrink-0 gap-2">
                      <Button size="sm" variant="secondary" onClick={addWorkflowNode}><Plus className="h-3.5 w-3.5" />新增节点</Button>
                      <Button size="sm" variant="secondary" onClick={addWorkflowEdge}><ArrowRight className="h-3.5 w-3.5" />添加依赖</Button>
                    </div>
                  </div>
                </div>

                <div className="flex min-h-0 flex-col gap-3">
                  <div className="min-h-0 space-y-3 overflow-y-auto pr-1">
                    <div className="rounded-md border border-kumo-line p-3">
                      <div className="mb-3 flex items-center justify-between gap-3">
                        <div className="flex min-w-0 items-center gap-2 text-sm font-semibold text-kumo-strong">
                          <Layers className="h-4 w-4 shrink-0" />
                          <span className="truncate">当前节点</span>
                        </div>
                        {selectedWorkflowNode && selectedWorkflowNode.type !== 'start' && (
                          <IconButton label="删除节点" variant="secondary-destructive" onClick={() => deleteWorkflowNode(selectedWorkflowNode.id)} icon={<Trash className="h-3.5 w-3.5" />} />
                        )}
                      </div>
                      {selectedWorkflowNode ? (
                        <div className="space-y-3">
                          <div className="grid gap-3 cq-sm:grid-cols-2">
                            <Input size="sm" label="节点名称" value={selectedWorkflowNode.name || ''} onChange={(event) => updateWorkflowNode(selectedWorkflowNode.id, { name: event.target.value })} />
                            {selectedWorkflowNode.type !== 'start' && (
                              <div className="flex items-center justify-between rounded-md border border-kumo-line px-3 py-2">
                                <span className="text-sm font-medium text-kumo-strong">启用节点</span>
                                <Switch checked={selectedWorkflowNode.enabled !== 0} onCheckedChange={(checked) => updateWorkflowNode(selectedWorkflowNode.id, { enabled: checked ? 1 : 0 })} />
                              </div>
                            )}
                          </div>
                          {selectedWorkflowNode.type === 'start' ? (
                            <div className="rounded-md border border-kumo-line bg-kumo-recessed/30 px-3 py-2 text-xs text-kumo-subtle">开始节点只负责触发流程，不需要绑定任务。</div>
                          ) : (
                            <div className="space-y-3">
                              <Select
                                size="sm"
                                label="引用任务"
                                className="w-full"
                                value={String(selectedWorkflowNode.task_id || 0)}
                                onValueChange={(value) => updateWorkflowNodeTask(selectedWorkflowNode, value)}
                                items={taskItems}
                              />
                              {!selectedWorkflowNode.task_id && (
                                <CodeEditor
                                  label="内联命令"
                                  language="shell"
                                  value={selectedWorkflowNode.command || ''}
                                  onChange={(command) => updateWorkflowNode(selectedWorkflowNode.id, { command })}
                                  placeholder="echo workflow-inline-step"
                                  minHeight="8rem"
                                />
                              )}
                            </div>
                          )}
                        </div>
                      ) : (
                        <div className="rounded-md border border-kumo-line bg-kumo-recessed/30 px-3 py-4 text-center text-xs text-kumo-subtle">请选择一个节点。</div>
                      )}
                    </div>

                    <div className="rounded-md border border-kumo-line p-3">
                      <div className="mb-3 flex items-center justify-between gap-3">
                        <div className="flex items-center gap-2 text-sm font-semibold text-kumo-strong"><Sliders className="h-4 w-4" />依赖规则</div>
                        <Button size="sm" variant="secondary" onClick={addWorkflowEdge}><Plus className="h-3.5 w-3.5" />新增</Button>
                      </div>
                      <div className="space-y-2">
                        {workflowForm.edges.length === 0 && (
                          <div className="rounded-md border border-kumo-line bg-kumo-recessed/30 px-3 py-4 text-center text-xs text-kumo-subtle">暂无依赖规则，节点会独立存在。</div>
                        )}
                        {workflowForm.edges.map((edge, index) => (
                          <div key={edge.id} className="grid gap-2 rounded-md border border-kumo-line p-2.5 cq-lg:grid-cols-[minmax(0,1fr)_minmax(0,1fr)]">
                            <div className="grid grid-cols-[1fr_auto_1fr] items-end gap-2">
                              <Select size="sm" label="来源" className="w-full" value={edge.from} onValueChange={(value) => setWorkflowForm((prev) => ({ ...prev, edges: prev.edges.map((item, i) => i === index ? { ...item, from: value } : item) }))} items={workflowNodeItems} />
                              <ArrowRight className="mb-2 h-3.5 w-3.5 text-kumo-subtle" />
                              <Select size="sm" label="目标" className="w-full" value={edge.to} onValueChange={(value) => setWorkflowForm((prev) => ({ ...prev, edges: prev.edges.map((item, i) => i === index ? { ...item, to: value } : item) }))} items={workflowNodeItems} />
                            </div>
                            <div className="grid grid-cols-[1fr_auto] items-end gap-2">
                              <Select size="sm" label="触发条件" className="w-full" value={edge.condition} onValueChange={(value) => setWorkflowForm((prev) => ({ ...prev, edges: prev.edges.map((item, i) => i === index ? { ...item, condition: value } : item) }))} items={CONDITION_ITEMS} />
                              <IconButton label="删除依赖" variant="secondary-destructive" onClick={() => setWorkflowForm((prev) => ({ ...prev, edges: prev.edges.filter((_, i) => i !== index) }))} icon={<Trash className="h-3.5 w-3.5" />} />
                            </div>
                          </div>
                        ))}
                      </div>
                    </div>
                  </div>

                  <div className="flex shrink-0 justify-end gap-2"><Button size="sm" variant="secondary" onClick={() => setWorkflowDialogOpen(false)}>取消</Button><Button size="sm" variant="primary" onClick={saveWorkflow} disabled={saving}><Save className="h-3.5 w-3.5" />保存</Button></div>
                </div>
              </div>
            </div>
          </Dialog>
        </Dialog.Root>

        <Dialog.Root open={Boolean(selectedRun)} onOpenChange={(open) => !open && setSelectedRun(null)}>
          <Dialog className="@container w-[calc(100vw-1rem)] max-w-[calc(100vw-1rem)] overflow-y-auto p-5 cq-sm:w-full cq-sm:max-w-4xl cq-sm:p-6">
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
