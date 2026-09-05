import React, { useCallback, useEffect, useMemo, useRef, useState } from 'react';
import { toast } from '../modules/toast.js';
import { useConfirmPress } from '../hooks/useConfirmPress.js';
import { Button } from '@cloudflare/kumo/components/button';
import { Dialog } from '@cloudflare/kumo/components/dialog';
import { Input } from '@cloudflare/kumo/components/input';
import { Select } from '@cloudflare/kumo/components/select';
import { Switch } from '@cloudflare/kumo/components/switch';
import { Textarea } from '@cloudflare/kumo';
import { SkeletonLine } from '@cloudflare/kumo/components/loader';
import { Table } from '@cloudflare/kumo/components/table';
import { Badge } from '@cloudflare/kumo/components/badge';
import { Empty } from '@cloudflare/kumo/components/empty';
import { Tooltip, TooltipProvider } from '@cloudflare/kumo/components/tooltip';
import { LayerCard, Tabs } from '@cloudflare/kumo';
import useDraggableScroll from '../hooks/useDraggableScroll.js';
import { MODULE_TABS_PROPS, TOOL_TABS_PROPS } from '../modules/kumoTabs.js';
import { SectionCard, TabBarOverflowActions, sectionCardHeaderClass, stickyTabsBaseClass } from '../components/ui/AppPrimitives.jsx';
import FormCard from '../components/ui/FormCard.jsx';
import CodeEditor from '../components/ui/CodeEditor.jsx';
import JsonHighlight from '../components/ui/JsonHighlight.jsx';
import { renderMarkdown } from '../modules/markdown.js';
import {
  Activity,
  ArrowRight,
  Bell,
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
  Sparkle,
  Terminal,
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
  // AI 智能任务扩展配置（保存时序列化为 config JSON）
  aiModel: '',
  aiPolicy: 'allow',
  aiChannelId: '',
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
  { value: 'ai', label: 'AI 智能任务' },
];

const AI_POLICY_ITEMS = [
  { value: 'allow', label: '完全允许（写操作免审批）' },
  { value: 'readonly', label: '只读（禁用写操作）' },
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

// tryFormatJson 尝试把文本解析并格式化 JSON；非 JSON 返回 null。
// 字符串字段字面量中的 \n 转义还原为真实换行（如 output 里的多行 Markdown/文本）。
function tryFormatJson(text) {
  if (!text) return null;
  const trimmed = String(text).trim();
  if (!(trimmed.startsWith('{') || trimmed.startsWith('['))) return null;
  try {
    return JSON.stringify(JSON.parse(trimmed), null, 2).replace(/\\n/g, '\n');
  } catch {
    return null;
  }
}

// renderLogOutput 渲染日志正文：JSON 用 Shiki 语法高亮（跟随亮暗主题），
// AI 任务且非 JSON 时渲染 Markdown。
function renderLogOutput(output, isAiTask) {
  if (!output) return <span className="text-xs text-kumo-subtle">（无输出）</span>;
  const json = tryFormatJson(output);
  if (json != null) {
    return <JsonHighlight code={json} className="rounded-md border border-kumo-line" minHeight="12rem" />;
  }
  if (isAiTask) {
    return <MarkdownOutput text={output} />;
  }
  return <pre className="whitespace-pre-wrap break-all font-mono text-xs leading-relaxed text-kumo-default">{output}</pre>;
}

// formatOutputText 兼容旧调用（返回字符串，非 JSON 原样返回）
function formatOutputText(text) {
  if (!text) return '';
  const json = tryFormatJson(text);
  return json != null ? json : String(text);
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
  if (node.type === 'end') return '结束节点';
  if (node.type === 'ai') return 'AI 智能任务';
  if (node.task_id) return `任务 #${node.task_id}`;
  if (node.type === 'task') return '未绑定任务';
  return node.command ? '内联命令' : taskTypeLabel(node.type);
}

function workflowNodeKindLabel(node) {
  if (node.type === 'start') return '入口';
  if (node.type === 'end') return '出口';
  if (node.type === 'ai') return 'AI';
  if (node.task_id) return '引用任务';
  if (node.type === 'task') return '待绑定';
  return '内联';
}

function workflowNodeKindVariant(node) {
  if (node.type === 'start' || node.type === 'end') return 'blue';
  if (node.type === 'ai') return 'purple';
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

/* AI 输出 markdown 渲染（与编辑器预览同管线：marked + DOMPurify + katex） */
function MarkdownOutput({ text }) {
  const html = useMemo(() => renderMarkdown(text), [text]);
  if (!text) return null;
  return (
    <div
      className="app-markdown-preview prose prose-sm max-w-none break-words text-xs"
      dangerouslySetInnerHTML={{ __html: html }}
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
          <div className="text-xs text-kumo-subtle">按周期自动生成表达式。</div>
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
        <div className="space-y-3">
          <Select alignItemWithTrigger size="sm" label="周期" className="w-full" value={form.periodType} onValueChange={(value) => setForm((prev) => ({ ...prev, periodType: value }))} items={PERIOD_ITEMS} />
          {form.periodType === 'week' && (
            <Select alignItemWithTrigger size="sm" label="星期" className="w-full" value={form.weekday} onValueChange={(value) => setForm((prev) => ({ ...prev, weekday: value }))} items={WEEKDAY_ITEMS} />
          )}
          {form.periodType === 'month' && (
            <Input size="sm" type="number" label="日期" min="1" max="31" value={form.dayOfMonth} onChange={(event) => setForm((prev) => ({ ...prev, dayOfMonth: Number(event.target.value) }))} />
          )}
          {['day', 'week', 'month'].includes(form.periodType) && (
            <div className="grid grid-cols-2 gap-3">
              <Input size="sm" type="number" label="小时" min="0" max="23" value={form.hour} onChange={(event) => setForm((prev) => ({ ...prev, hour: Number(event.target.value) }))} />
              <Input size="sm" type="number" label="分钟" min="0" max="59" value={form.minute} onChange={(event) => setForm((prev) => ({ ...prev, minute: Number(event.target.value) }))} />
            </div>
          )}
          {form.periodType === 'hour' && (
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

/* ---------- 工作流画布（复用 GitHub Actions flow 画布的「纯布局 + SVG 连线」设计） ----------
   布局与连线全部由纯函数按节点/连线计算（不做 DOM 测量），缩放只作用于展示层 transform，
   天然避免 kumo Flow 在祖先 transform 下测量导致连线/节点错位的同类问题；画布外壳、双层
   动画连线、拖拽平移与非测量式 fit 缩放均与 GitHubPage 的 ActionWorkflowCanvas 同款。 */

const WORKFLOW_CANVAS_SIZES = {
  // compact 与 editor 共用同一套布局常量：卡片画布的节点尺寸/连线风格与编辑画布完全一致；
  //     高度由左栏锚定（absolute inset-0 + wrapper min-h 常量兜底，无测量反馈环）；
  // default（运行详情）用固定视口高度（GitHub ActionWorkflowCanvas 同款 320）；
  // editor 在对话框定高 flex 链中，测量稳定。
  compact: { nodeW: 208, nodeH: 96, stageGap: 72, rowGap: 18, padX: 24, padY: 24, minScale: 0.45, fixedHeight: null },
  editor: { nodeW: 208, nodeH: 96, stageGap: 72, rowGap: 18, padX: 24, padY: 24, minScale: 0.45, fixedHeight: null },
  default: { nodeW: 240, nodeH: 108, stageGap: 72, rowGap: 26, padX: 24, padY: 24, minScale: 0.6, fixedHeight: 320 },
};

// editor 视口位于对话框定高 flex 链中，测量高度与内容无关（无反馈）；此上界仅为异常兜底。
const WORKFLOW_CANVAS_MAX_HEIGHT = 520;

// S 形圆角连线（与 GitHub makeActionStageBranchPath 同款）：母线靠近目标端，尽量不穿越中间节点
function routeWorkflowEdge(source, target, cfg) {
  const x1 = source.x + source.width;
  const y1 = source.y + source.height / 2;
  const x2 = target.x;
  const y2 = target.y + target.height / 2;
  if (Math.abs(y2 - y1) < 4) return { path: `M ${x1} ${y1} H ${x2}`, midX: (x1 + x2) / 2, midY: y1 };
  const busX = x2 - Math.min(28, Math.max(16, (x2 - x1) * 0.45));
  const dir = Math.sign(y2 - y1) || 1;
  const availableLeft = Math.max(8, busX - x1);
  const availableRight = Math.max(8, x2 - busX);
  const availableVertical = Math.max(8, Math.abs(y2 - y1) / 2);
  const curve = Math.max(8, Math.min(24, availableLeft, availableRight, availableVertical));
  const handle = Math.max(4, curve / 2);
  const startCurveX = busX - curve;
  const startCurveY = y1 + dir * curve;
  const endCurveY = y2 - dir * curve;
  return {
    path: [
      `M ${x1} ${y1}`,
      `H ${startCurveX}`,
      `C ${busX - handle} ${y1} ${busX} ${y1 + dir * handle} ${busX} ${startCurveY}`,
      `V ${endCurveY}`,
      `C ${busX} ${y2 - dir * handle} ${busX + handle} ${y2} ${busX + curve} ${y2}`,
      `H ${x2}`,
    ].join(' '),
    midX: busX,
    midY: (y1 + y2) / 2,
  };
}

// 纯函数布局：由节点/连线直接产出各节点 rect 与连线 path。各列按序排列，列内节点垂直堆叠，
// 整列绕画布垂直中线对齐（线性链全部端口同高 → 直线；并行分支绕中线对称 → S 形母线）。
function buildWorkflowCanvasLayout(nodes = [], edges = [], size = 'default') {
  const cfg = WORKFLOW_CANVAS_SIZES[size] || WORKFLOW_CANVAS_SIZES.default;
  const stageList = buildWorkflowFlowStages(nodes, edges);
  const validEdges = getValidWorkflowEdges(nodes, edges);
  const stackHeights = stageList.map((stage) => stage.length * cfg.nodeH + Math.max(0, stage.length - 1) * cfg.rowGap);
  const contentHeight = Math.max(0, ...stackHeights) + cfg.padY * 2;
  const contentWidth = cfg.padX * 2 + stageList.length * cfg.nodeW + Math.max(0, stageList.length - 1) * cfg.stageGap;
  const rectById = new Map();
  stageList.forEach((stageNodes, stageIndex) => {
    const x = cfg.padX + stageIndex * (cfg.nodeW + cfg.stageGap);
    const stackTop = (contentHeight - stackHeights[stageIndex]) / 2;
    stageNodes.forEach((node, index) => {
      rectById.set(node.id, { x, y: stackTop + index * (cfg.nodeH + cfg.rowGap), width: cfg.nodeW, height: cfg.nodeH });
    });
  });
  const edgeList = validEdges
    .map((edge) => {
      const source = rectById.get(edge.from);
      const target = rectById.get(edge.to);
      if (!source || !target) return null;
      const { path, midX, midY } = routeWorkflowEdge(source, target, cfg);
      return { from: edge.from, to: edge.to, condition: edge.condition, path, midX, midY };
    })
    .filter(Boolean);
  return { width: contentWidth, height: contentHeight, rectById, stages: stageList, edges: edgeList, cfg };
}

function WorkflowCanvas({ workflow, runs = [], tasks = [], selectedNodeId = '', onSelectNode = null, size = 'default' }) {
  const nodes = workflow.nodes || [];
  const edges = workflow.edges || [];
  const compact = size === 'compact';
  const editor = size === 'editor';
  const cfg = WORKFLOW_CANVAS_SIZES[size] || WORKFLOW_CANVAS_SIZES.default;
  const layout = useMemo(() => buildWorkflowCanvasLayout(nodes, edges, size), [nodes, edges, size]);
  const latestRun = runs.find((run) => run.workflow_id === workflow.id);
  const nodeStatus = Object.fromEntries((latestRun?.node_runs || []).map((run) => [run.node_id, run.status]));
  const incomingEdges = useMemo(() => {
    const grouped = new Map();
    layout.edges.forEach((edge) => {
      if (!grouped.has(edge.to)) grouped.set(edge.to, []);
      grouped.get(edge.to).push(edge);
    });
    return grouped;
  }, [layout.edges]);

  const viewportRef = useRef(null);
  const [viewportSize, setViewportSize] = useState({ width: 0, height: 0 });
  useEffect(() => {
    const element = viewportRef.current;
    if (!element) return undefined;
    const updateSize = () => {
      const rect = element.getBoundingClientRect();
      setViewportSize({ width: rect.width, height: rect.height });
    };
    updateSize();
    if (typeof ResizeObserver === 'undefined') {
      window.addEventListener('resize', updateSize);
      return () => window.removeEventListener('resize', updateSize);
    }
    const observer = new ResizeObserver(updateSize);
    observer.observe(element);
    return () => observer.disconnect();
  }, []);

  // 画布缩放默认值：70%（用户指定），滚轮 ±，复位回 70%；不再以「适配容器」作为默认缩放
  const WORKFLOW_CANVAS_DEFAULT_SCALE = 0.7;
  const [displayedScale, setDisplayedScale] = useState(WORKFLOW_CANVAS_DEFAULT_SCALE);
  const view = useMemo(() => {
    const width = viewportSize.width || layout.width;
    const height = cfg.fixedHeight != null ? cfg.fixedHeight : Math.min(viewportSize.height || layout.height, WORKFLOW_CANVAS_MAX_HEIGHT);
    const scaledWidth = layout.width * displayedScale;
    const scaledHeight = layout.height * displayedScale;
    const overflowX = scaledWidth > width;
    const overflowY = scaledHeight > height;
    return {
      displayedScale,
      left: overflowX ? Math.max(8, cfg.padX / 2) : Math.max(0, (width - scaledWidth) / 2),
      top: overflowY ? Math.max(8, cfg.padY / 2) : Math.max(0, (height - scaledHeight) / 2),
      overflowX,
      overflowY,
      contentWidth: overflowX ? Math.ceil(scaledWidth + Math.max(8, cfg.padX / 2) * 2 + 12) : width,
      contentHeight: overflowY ? Math.ceil(scaledHeight + Math.max(8, cfg.padY / 2) * 2 + 12) : height,
    };
  }, [viewportSize.width, viewportSize.height, layout, cfg, displayedScale]);

  // 滚轮缩放：原生监听（passive:false）才能阻止默认滚动；Ctrl/⌘ 滚轮保留浏览器页面缩放
  useEffect(() => {
    const element = viewportRef.current;
    if (!element) return undefined;
    const handleWheel = (event) => {
      if (event.ctrlKey || event.metaKey) return;
      const factor = Math.exp(-event.deltaY * 0.0015);
      const next = Math.min(3, Math.max(0.25, displayedScale * factor));
      if (next === displayedScale) return;
      setDisplayedScale(next);
      event.preventDefault();
    };
    element.addEventListener('wheel', handleWheel, { passive: false });
    return () => element.removeEventListener('wheel', handleWheel);
  }, [displayedScale]);

  // 鸟瞰图：监听滚动窗口，视口指示框随平移/缩放实时更新
  const [scrollWindow, setScrollWindow] = useState({ left: 0, top: 0, width: 0, height: 0 });
  const minimapRef = useRef(null);
  const minimapDragRef = useRef(false);
  const syncScrollWindow = useCallback(() => {
    const element = viewportRef.current;
    if (!element) return;
    setScrollWindow({ left: element.scrollLeft, top: element.scrollTop, width: element.clientWidth || 0, height: element.clientHeight || 0 });
  }, []);
  useEffect(() => {
    const element = viewportRef.current;
    if (!element) return undefined;
    const onScroll = () => syncScrollWindow();
    onScroll();
    element.addEventListener('scroll', onScroll, { passive: true });
    const observer = new ResizeObserver(onScroll);
    observer.observe(element);
    return () => {
      element.removeEventListener('scroll', onScroll);
      observer.disconnect();
    };
  }, [syncScrollWindow, view.contentWidth, view.contentHeight]);

  const showMinimap = !compact && viewportSize.width >= 340;
  const navigateCanvasFromEvent = useCallback((event) => {
    const element = viewportRef.current;
    const frame = minimapRef.current;
    if (!element || !frame) return;
    const rect = frame.getBoundingClientRect();
    const mmScale = Math.min((rect.width - 16) / layout.width, (rect.height - 16) / layout.height);
    const layoutX = (event.clientX - rect.left - 8) / mmScale;
    const layoutY = (event.clientY - rect.top - 8) / mmScale;
    element.scrollLeft = layoutX * view.displayedScale + view.left - element.clientWidth / 2;
    element.scrollTop = layoutY * view.displayedScale + view.top - element.clientHeight / 2;
    syncScrollWindow();
  }, [layout.width, layout.height, view.displayedScale, view.left, view.top, syncScrollWindow]);

  const { dragHandlers, isDragging } = useDraggableScroll(viewportRef, {
    disabled: !(view.overflowX || view.overflowY),
  });

  const renderNodeCard = (node, rect) => {
    const status = nodeStatus[node.id];
    const selected = selectedNodeId === node.id;
    const linkedTask = tasks.find((t) => String(t.id) === String(node.task_id));
    const isAi = node.type === 'ai' || (!!linkedTask && linkedTask.type === 'ai');
    const dependencies = incomingEdges.get(node.id) || [];
    const dependencyText = dependencies.length > 1
      ? `${dependencies.length} 条依赖`
      : dependencies[0]
        ? conditionLabel(dependencies[0].condition)
        : '';

    return (
      <div
        key={node.id}
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
        style={{
          left: rect.x,
          top: rect.y,
          width: rect.width,
          height: rect.height,
          cursor: onSelectNode ? 'pointer' : 'default',
        }}
        className={`absolute flex flex-col overflow-hidden rounded-md border bg-kumo-base text-left focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-brand/45 ${compact ? 'px-2.5 py-2' : editor ? 'px-3.5 py-2.5' : 'px-4 py-3'} ${selected ? 'border-brand ring-2 ring-brand/25' : 'border-kumo-line hover:border-brand/50'} ${node.enabled === 0 ? 'opacity-60' : ''} ${status === 'running' ? 'scheduler-node-running' : ''}`}
      >
        <span className={`flex min-w-0 items-start justify-between ${compact ? 'gap-1.5' : editor ? 'gap-2.5' : 'gap-3'}`}>
          <span className="min-w-0">
            <span className={`block truncate font-semibold text-kumo-strong ${compact ? 'text-xs' : 'text-sm'}`}>{node.name || node.id}</span>
            <span className={`mt-0.5 block truncate leading-4 text-kumo-subtle ${compact ? 'text-[10px]' : 'mt-1 text-xs leading-5'}`}>{isAi ? 'AI 智能任务' : workflowNodeTypeLabel(node)}</span>
          </span>
          <Badge variant={isAi ? 'purple' : workflowNodeKindVariant(node)} className={compact ? 'text-[9px] px-1 py-0' : undefined}>{isAi ? 'AI' : workflowNodeKindLabel(node)}</Badge>
        </span>
        <span className={`mt-auto flex min-w-0 items-center justify-between gap-2 ${compact ? 'pt-2' : editor ? 'pt-3' : 'pt-5'}`}>
          <Badge variant={statusBadgeVariant(status || (node.enabled === 0 ? 'skipped' : 'queued'))} appearance="dot" className={compact ? 'text-[9px] px-1' : undefined}>
            {node.enabled === 0 ? '停用' : status ? statusLabel(status) : '待运行'}
          </Badge>
          {dependencyText && (
            <span className={`min-w-0 truncate text-kumo-subtle ${compact ? 'text-[10px]' : 'text-xs'}`}>
              {dependencyText}
            </span>
          )}
        </span>
      </div>
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
    <div
      className={`${compact ? 'absolute inset-0' : 'relative h-full border border-kumo-line'} overflow-hidden rounded-md bg-kumo-base`}
      style={cfg.fixedHeight ? { height: cfg.fixedHeight } : undefined}
    >
      <div
        ref={viewportRef}
        {...dragHandlers}
        onDoubleClick={(e) => {
          if (e.target instanceof Element && !e.target.closest('[role="button"]')) setDisplayedScale(WORKFLOW_CANVAS_DEFAULT_SCALE);
        }}
        className={`absolute inset-0 select-none overflow-auto ${view.overflowX || view.overflowY ? (isDragging ? 'cursor-grabbing' : 'cursor-grab') : ''}`}
      >
      <div className="relative" style={{ width: view.contentWidth, height: view.contentHeight }}>
        <div
          className="relative origin-top-left"
          style={{
            width: layout.width,
            height: layout.height,
            transform: `translate(${view.left}px, ${view.top}px) scale(${view.displayedScale})`,
          }}
        >
          <svg className="pointer-events-none absolute inset-0 z-0" width={layout.width} height={layout.height} aria-hidden="true">
            <g fill="none" strokeLinecap="round" strokeLinejoin="round">
              {layout.edges.map((edge) => (
                <g key={`${edge.from}-${edge.to}`}>
                  <path d={edge.path} stroke="var(--color-kumo-line)" strokeOpacity="0.9" strokeWidth="2" />
                  <path
                    d={edge.path}
                    stroke="var(--color-brand)"
                    strokeOpacity="0.3"
                    strokeWidth="2.5"
                    pathLength="1"
                    strokeDasharray="0.14 0.86"
                    strokeDashoffset="0"
                  >
                    <animate attributeName="stroke-dashoffset" from="1" to="0" dur="2.2s" repeatCount="indefinite" />
                  </path>
                </g>
              ))}
            </g>
          </svg>
          {layout.stages.flatMap((stage) => stage.map((node) => renderNodeCard(node, layout.rectById.get(node.id))))}
        </div>
      </div>
      </div>
      <Button
        type="button"
        size="xs"
        variant="ghost"
        onClick={() => setDisplayedScale(WORKFLOW_CANVAS_DEFAULT_SCALE)}
        title="滚轮缩放画布；点击或双击画布恢复 70%"
        className="absolute right-2 top-2 z-10 h-6 gap-1 rounded-full bg-kumo-base/90 px-2 text-[11px] font-medium text-kumo-subtle shadow-sm ring-1 ring-kumo-line"
      >
        {Math.round(view.displayedScale * 100)}%
      </Button>
      {showMinimap && (() => {
        const frameW = Math.min(180, Math.max(120, viewportSize.width * 0.3));
        const frameH = Math.min(120, Math.max(72, layout.height / layout.width * frameW + 8));
        const mmScale = Math.min((frameW - 16) / layout.width, (frameH - 16) / layout.height);
        const rawLeft = (scrollWindow.left - view.left) / view.displayedScale;
        const rawTop = (scrollWindow.top - view.top) / view.displayedScale;
        const rawW = scrollWindow.width / view.displayedScale;
        const rawH = scrollWindow.height / view.displayedScale;
        const visX = Math.max(0, Math.min(rawLeft, layout.width)) * mmScale;
        const visY = Math.max(0, Math.min(rawTop, layout.height)) * mmScale;
        const visW = Math.min(rawW, layout.width - visX / mmScale) * mmScale;
        const visH = Math.min(rawH, layout.height - visY / mmScale) * mmScale;
        return (
          <div
            ref={minimapRef}
            title="鸟瞰图：点击或拖动定位画布"
            className="absolute bottom-2 left-2 z-10 overflow-hidden rounded-md border border-kumo-line bg-kumo-base/95"
            style={{ width: frameW, height: frameH }}
            onPointerDown={(event) => {
              if (event.button !== 0) return;
              minimapDragRef.current = true;
              event.preventDefault();
              navigateCanvasFromEvent(event);
            }}
            onPointerMove={(event) => {
              if (minimapDragRef.current) navigateCanvasFromEvent(event);
            }}
            onPointerUp={() => { minimapDragRef.current = false; }}
            onPointerLeave={() => { minimapDragRef.current = false; }}
          >
            <svg width={frameW - 16} height={frameH - 16} className="pointer-events-none absolute left-2 top-2">
              <g transform={`scale(${mmScale})`} fill="none">
                {layout.edges.map((edge) => (
                  <path key={`${edge.from}-${edge.to}`} d={edge.path} stroke="var(--color-kumo-line)" strokeOpacity="0.85" strokeWidth="2" />
                ))}
                {layout.stages.flatMap((stage) => stage.map((node) => {
                  const rect = layout.rectById.get(node.id);
                  if (!rect) return null;
                  return (
                    <rect
                      key={node.id}
                      x={rect.x}
                      y={rect.y}
                      width={rect.width}
                      height={rect.height}
                      rx={4}
                      fill="var(--color-kumo-base)"
                      stroke="var(--color-kumo-line)"
                      strokeWidth="1.5"
                    />
                  );
                }))}
              </g>
              <rect
                x={visX}
                y={visY}
                width={Math.max(12, visW)}
                height={Math.max(8, visH)}
                fill="var(--color-brand)"
                fillOpacity="0.12"
                stroke="var(--color-brand)"
                strokeWidth="1.5"
              />
            </svg>
          </div>
        );
      })()}
    </div>
  );
}

function SchedulerPage({ onNavigate = () => {} }) {
  const { isArmed, confirmPress } = useConfirmPress();
  const mountedRef = useRef(true);
  useEffect(() => () => {
    mountedRef.current = false;
  }, []);
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
  const [taskLogs, setTaskLogs] = useState([]);
  const [taskLogsLoading, setTaskLogsLoading] = useState(false);
  const [taskLogsTarget, setTaskLogsTarget] = useState(null);
  const [taskLogsSelectedId, setTaskLogsSelectedId] = useState(null);
  const [workflowCanvasEpoch, setWorkflowCanvasEpoch] = useState(0);
  const [aiModelOptions, setAiModelOptions] = useState([{ value: '', label: '默认模型' }]);
  const [aiChannelOptions, setAiChannelOptions] = useState([]);

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

  useEffect(() => {
    if (!taskDialogOpen) return undefined;
    // AI 任务选项懒加载：模型选项来自对外 /v1 暴露的模型（/api/openai/models 与 /v1/models 同源，
    // 已过滤禁用模型并应用映射去前缀），推送目标来自已启用 Telegram 频道
    let cancelled = false;
    (async () => {
      try {
        const [epRes, chRes, aiChRes] = await Promise.all([
          fetch('/api/openai/models'),
          fetch('/api/notification/channels'),
          fetch('/api/admin-ai/channels'),
        ]);
        const epData = await epRes.json();
        const list = Array.isArray(epData) ? epData : (epData.data || []);
        const options = (list || [])
          .filter((m) => m && m.id)
          .map((m) => ({ value: m.id, label: m.id }));
        options.sort((a, b) => a.label.localeCompare(b.label));
        // 结果推送目标：优先列通知中心已启用的 Telegram 渠道，并兼容旧版 AI 频道 id（aac_ 前缀）
        const chData = await chRes.json();
        const channels = (chData.data || chData) || [];
        const telegramChannels = channels.filter((c) => c.type === 'telegram' && c.enabled);
        let aiTelegramChannels = [];
        try {
          const aiChData = await aiChRes.json();
          const aiRaw = (aiChData && aiChData.data) || aiChData || {};
          const aiList = Array.isArray(aiRaw) ? aiRaw : (aiRaw.channels || []);
          aiTelegramChannels = aiList.filter((c) => c && c.type === 'telegram' && c.enabled);
        } catch {
          // AI 频道列表不可用时仅展示通知中心渠道
        }
        if (cancelled) return;
        setAiModelOptions(options.length ? options : [{ value: '', label: '默认模型' }]);
        // 用 bot token 判定同一 Telegram bot：同一 bot 在通知中心与 AI 频道各配
        // 一份时加「（AI 频道）」后缀区分；同名但不同 bot（token 不同）不误标。
        const notifTokens = new Set(telegramChannels.map((c) => c.config?.bot_token || c.config?.botToken || ''));
        setAiChannelOptions([
          ...telegramChannels.map((c) => ({ value: c.id, label: c.name || c.id })),
          ...aiTelegramChannels.map((c) => {
            const base = c.name || c.id;
            const token = c.config?.botToken || c.config?.bot_token || '';
            return { value: c.id, label: token && notifTokens.has(token) ? `${base}（AI 频道）` : base };
          }),
        ]);
      } catch {
        if (!cancelled) {
          setAiModelOptions([{ value: '', label: '默认模型' }]);
          setAiChannelOptions([]);
        }
      }
    })();
    return () => { cancelled = true; };
  }, [taskDialogOpen]);

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
    let aiModel = '';
    let aiPolicy = 'allow';
    let aiChannelId = '';
    if (task.config) {
      try {
        const cfg = typeof task.config === 'string' ? JSON.parse(task.config) : task.config;
        aiModel = cfg.model || '';
        aiPolicy = cfg.policy === 'readonly' ? 'readonly' : 'allow';
        aiChannelId = cfg.channelId || '';
      } catch {
        // config 解析失败时按默认值处理
      }
    }
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
      aiModel,
      aiPolicy,
      aiChannelId,
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
      if (taskForm.type === 'ai') {
        payload.config = JSON.stringify({
          model: taskForm.aiModel || '',
          policy: taskForm.aiPolicy === 'readonly' ? 'readonly' : 'allow',
          channelId: taskForm.aiChannelId || '',
        });
      }
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

  const openTaskLogs = async (task) => {
    setTaskLogsTarget(task);
    setTaskLogsLoading(true);
    setTaskLogs([]);
    setTaskLogsSelectedId(null);
    try {
      const res = await fetch(`/api/cron/logs?task_id=${task.id}`, { headers: authHeaders() });
      const data = await res.json();
      const logs = Array.isArray(data.data) ? data.data : [];
      setTaskLogs(logs);
      if (logs.length > 0) setTaskLogsSelectedId(logs[0].id);
    } catch (error) {
      console.error(error);
      toast.error('加载任务日志失败');
    } finally {
      setTaskLogsLoading(false);
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
      const node = {
        id: `task-${Date.now()}`,
        name: `任务 ${index}`,
        type: 'shell',
        task_id: 0,
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

  // AI 节点的推送频道配置：从 node.config JSON 解析/合并 channelId。
  // 定义为函数而非 IIFE：selectedWorkflowNode 在组件下层才初始化，IIFE 会触发 TDZ。
  const getWorkflowNodeAiChannelId = (node) => {
    if (!node || node.type !== 'ai') return '';
    try {
      const parsed = node.config ? JSON.parse(node.config) : {};
      return parsed.channelId || '';
    } catch { return ''; }
  };

  const updateWorkflowNodeAiConfig = ({ channelId }) => {
    const target = workflowForm.nodes.find((node) => node.id === selectedWorkflowNodeId);
    if (!target) return;
    let parsed = {};
    try { parsed = target.config ? JSON.parse(target.config) : {}; } catch { /* reset */ }
    updateWorkflowNode(target.id, { config: JSON.stringify({ ...parsed, channelId: channelId?.trim?.() || '' }) });
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
        nodes: workflowForm.nodes.map((node) => {
          const cleaned = { ...node, task_id: Number(node.task_id) || 0 };
          // config 为空对象时不发送，避免后端落空 JSON。
          if (cleaned.config) {
            try {
              if (Object.keys(JSON.parse(cleaned.config)).length === 0) delete cleaned.config;
            } catch { delete cleaned.config; }
          }
          return cleaned;
        }),
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
      // 执行接口为同步阻塞（串行跑完整个 DAG 才返回）。期间并行轮询进度：
      // 先从运行列表找到本次 run 的 id，再轮询详情接口拿 node_runs，更新画布。
      const pollProgress = (async () => {
        let runId = null;
        for (let i = 0; i < 60 && !runId && mountedRef.current; i++) {
          try {
            const listRes = await fetch('/api/scheduler/runs', { headers: authHeaders() });
            const listData = await listRes.json();
            const list = Array.isArray(listData.data) ? listData.data : [];
            runId = list.find((run) => run.workflow_id === workflow.id && run.status === 'running')?.id || null;
          } catch {
          }
          if (!runId && mountedRef.current) await new Promise((resolve) => window.setTimeout(resolve, 300));
        }
        if (!runId) return;
        for (let i = 0; i < 120 && mountedRef.current; i++) {
          let done = false;
          try {
            const detailRes = await fetch(`/api/scheduler/runs/${runId}`, { headers: authHeaders() });
            const detailData = await detailRes.json();
            const detail = detailData.success ? detailData.data : null;
            if (detail) {
              setRuns((prev) => {
                const next = prev.filter((run) => run.id !== detail.id);
                return [detail, ...next];
              });
              done = detail.status !== 'running';
            }
          } catch {
          }
          if (done) break;
          await new Promise((resolve) => window.setTimeout(resolve, 700));
        }
      })();

      const res = await fetch(`/api/scheduler/workflows/${workflow.id}/run`, { method: 'POST', headers: authHeaders() });
      const data = await res.json();
      await pollProgress;
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
        icon: <Activity className="h-5 w-5 text-brand" />,
        cardClassName: 'bg-brand/7',
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
  const taskCommandLabel = taskForm.type === 'http' ? 'URL' : taskForm.type === 'internal' ? '内部接口路径' : taskForm.type === 'ai' ? '提示词' : '命令';
  const taskCommandPlaceholder = taskForm.type === 'http'
    ? 'https://example.com/health'
    : taskForm.type === 'internal'
      ? 'GET /health'
      : taskForm.type === 'ai'
        ? '例如：检查所有主机和 Docker 容器状态，输出巡检报告；有异常时说明原因并给出处置建议。'
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

        <div className="grid grid-cols-2 gap-2 cq-sm:grid-cols-4 cq-sm:gap-3">
          {summaryItems.map(({ label, value, icon, cardClassName }) => (
            <LayerCard key={label} className={`min-w-0 p-2 cq-sm:p-3 ${cardClassName || ''}`}>
              <div className="flex items-center justify-between gap-2 text-[11px] text-kumo-subtle cq-sm:gap-3 cq-sm:text-xs">
                <span className="truncate">{label}</span>
                <span className="shrink-0">{icon}</span>
              </div>
              <div className="mt-1 font-mono text-sm font-semibold text-kumo-strong">{value}</div>
            </LayerCard>
          ))}
        </div>


        {activeTab === 'tasks' && (
          <SectionCard
            title="任务列表"
            icon={<Clock className="h-4 w-4 text-brand" />}
            bodyPadding="none"
          >
            {loading ? (
              <div className="space-y-2 p-4">
                {Array.from({ length: 6 }).map((_, index) => <SkeletonLine key={index} className="h-11 w-full" />)}
              </div>
            ) : tasks.length === 0 ? (
              <Empty size="sm" className="rounded-none border-0 bg-transparent" icon={<Clock className="h-8 w-8 text-kumo-inactive" />} title="暂无任务" description="创建后可被工作流引用。" contents={<Button size="sm" variant="primary" onClick={openCreateTask}><Plus className="h-3.5 w-3.5" />新建任务</Button>} />
            ) : (
              <div className="overflow-x-auto">
                <Table layout="fixed" className="min-w-[1080px]">
                  <colgroup><col className="w-[104px]" /><col className="w-[220px]" /><col className="w-[128px]" /><col className="w-[190px]" /><col className="w-[180px]" /><col className="w-[180px]" /><col className="w-[160px]" /></colgroup>
                  <Table.Header><Table.Row><Table.Head>状态</Table.Head><Table.Head>任务</Table.Head><Table.Head>类型</Table.Head><Table.Head>周期</Table.Head><Table.Head>下次运行</Table.Head><Table.Head>日志</Table.Head><Table.Head className="app-table-action">操作</Table.Head></Table.Row></Table.Header>
                  <Table.Body>
                    {tasks.map((task) => (
                      <Table.Row key={task.id}>
                        <Table.Cell><Badge variant={task.enabled ? 'success' : 'neutral'} appearance="dot">{task.enabled ? '启用' : '停用'}</Badge></Table.Cell>
                        <Table.Cell><div className="font-semibold text-kumo-strong">{task.name}</div><div className="truncate text-xs text-kumo-subtle">{task.description || task.command}</div></Table.Cell>
                        <Table.Cell className="text-xs text-kumo-default">{taskTypeLabel(task.type)}</Table.Cell>
                        <Table.Cell><div className="font-mono text-xs text-kumo-default">{task.schedule || '手动'}</div><div className="text-xs text-kumo-subtle">{task.schedule_summary}</div></Table.Cell>
                        <Table.Cell className="text-xs text-kumo-subtle">{formatTimestamp(task.next_run)}</Table.Cell>
                        <Table.Cell>
                          <div className="flex items-center gap-2">
                            <IconButton label="查看日志" onClick={() => openTaskLogs(task)} icon={<Terminal className="h-3.5 w-3.5" />} />
                            {task.recent_status ? (
                              <Badge variant={statusBadgeVariant(task.recent_status)} appearance="dot">{statusLabel(task.recent_status)}</Badge>
                            ) : (
                              <span className="text-xs text-kumo-subtle">暂无运行</span>
                            )}
                          </div>
                        </Table.Cell>
                        <Table.Cell>
                          <div className="flex items-center justify-center gap-1">
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
            icon={<GitBranch className="h-4 w-4 text-brand" />}
            bodyClassName={workflows.length === 0 ? '' : 'space-y-3'}
            bodyPadding={workflows.length === 0 ? 'none' : 'md'}
          >
            {workflows.length === 0 ? (
              <Empty size="sm" className="rounded-none border-0 bg-transparent" icon={<GitBranch className="h-8 w-8 text-kumo-inactive" />} title="暂无工作流" description="创建后可按 DAG 编排任务。" contents={<Button size="sm" variant="primary" onClick={openCreateWorkflow}><Plus className="h-3.5 w-3.5" />新建工作流</Button>} />
            ) : (
              <div className="grid gap-3 cq-lg:grid-cols-2">
                {workflows.map((workflow) => (
                  <div key={workflow.id} className="scheduler-workflow-card flex h-full flex-col overflow-hidden rounded-xl border border-kumo-line bg-kumo-elevated p-3 shadow-none">
                    <div className="grid min-h-0 flex-1 gap-3 cq-lg:grid-cols-[minmax(0,220px)_minmax(0,1fr)]">
                      <div className="flex min-h-0 min-w-0 flex-col gap-2.5">
                        {/* 标题栏：标题居左，启用状态 pill 靠右 */}
                        <div className="flex min-w-0 items-center justify-between gap-2">
                          <h3 className="min-w-0 truncate text-sm font-semibold text-kumo-strong" title={workflow.name}>{workflow.name}</h3>
                          <Badge variant={workflow.enabled ? 'success' : 'neutral'} appearance="dot" className="shrink-0">{workflow.enabled ? '启用' : '停用'}</Badge>
                        </div>
                        {/* 元信息 */}
                        <div className="grid gap-2 cq-sm:grid-cols-2 cq-lg:grid-cols-1">
                          <div className="rounded-md border border-kumo-line bg-kumo-recessed/25 px-2.5 py-2">
                            <div className="text-[11px] text-kumo-subtle">触发方式</div>
                            <div className="mt-1 truncate font-mono text-xs text-kumo-strong">{workflow.schedule ? `Cron ${workflow.schedule}` : '手动触发'}</div>
                          </div>
                          <div className="rounded-md border border-kumo-line bg-kumo-recessed/25 px-2.5 py-2">
                            <div className="text-[11px] text-kumo-subtle">下次运行</div>
                            <div className="mt-1 truncate text-xs font-semibold text-kumo-strong">
                              {workflow.enabled && workflow.schedule ? (
                                workflow.next_run
                                  ? new Date(workflow.next_run * 1000).toLocaleString()
                                  : '等待调度'
                              ) : (
                                '手动触发'
                              )}
                            </div>
                          </div>
                        </div>
                        {/* 操作按钮单独一行 */}
                        <div className="mt-auto flex items-center gap-1">
                          <IconButton label="运行工作流" onClick={() => runWorkflow(workflow)} icon={<Play className="h-3.5 w-3.5" />} />
                          <IconButton label="配置通知规则" onClick={() => onNavigate('notification', { newRule: 'workflow.completed', workflowId: String(workflow.id), workflowName: workflow.name })} icon={<Bell className="h-3.5 w-3.5" />} />
                          <IconButton label="编辑工作流" onClick={() => openEditWorkflow(workflow)} icon={<Edit className="h-3.5 w-3.5" />} />
                          <IconButton label="删除工作流" variant={isArmed(`workflow:${workflow.id}`) ? 'destructive' : 'secondary-destructive'} onClick={() => deleteWorkflow(workflow)} icon={<Trash className="h-3.5 w-3.5" />} />
                        </div>
                      </div>

                      <div className="flex min-h-0 min-w-0 overflow-hidden rounded-md border border-kumo-line">
                        <div className="relative flex min-w-0 flex-1 min-h-[10rem]">
                          <WorkflowCanvas workflow={workflow} runs={runs} tasks={tasks} size="compact" />
                        </div>
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
            icon={<Activity className="h-4 w-4 text-brand" />}
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
                        <Table.Cell><div className="flex items-center justify-center gap-1"><IconButton label="查看详情" onClick={async () => {
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
            icon={<Server className="h-4 w-4 text-brand" />}
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
                            <Server className="h-4 w-4 shrink-0 text-brand" />
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
          <Dialog className="@container scheduler-task-dialog flex max-h-[calc(100dvh-2rem)] flex-col overflow-hidden p-5 cq-sm:p-6">
            <Dialog.Title className="mb-4 shrink-0 text-base font-semibold text-kumo-strong">{taskForm.id ? '编辑任务' : '新建任务'}</Dialog.Title>
            <div className="min-h-0 flex-1 overflow-y-auto pr-1">
              <div className="grid gap-4 cq-xs:grid-cols-2 cq-xs:items-start">
                <div className="min-w-0 space-y-4">
              <FormCard icon={<Server className="h-4 w-4" />} title="基础信息" description="任务名称、描述与执行节点">
                <div className="space-y-3 py-4">
                  <div className="grid gap-3 cq-sm:grid-cols-2">
                    <Input size="sm" label="名称" value={taskForm.name} onChange={(event) => setTaskForm((prev) => ({ ...prev, name: event.target.value }))} />
                    <Select alignItemWithTrigger size="sm" label="执行节点" className="w-full" value={taskForm.node_id} onValueChange={(value) => setTaskForm((prev) => ({ ...prev, node_id: value }))} items={nodeItems} />
                  </div>
                  <Input size="sm" label="描述" value={taskForm.description} onChange={(event) => setTaskForm((prev) => ({ ...prev, description: event.target.value }))} />
                  <div className="grid gap-3 cq-sm:grid-cols-2">
                    <Select alignItemWithTrigger size="sm" label="任务类型" className="w-full" value={taskForm.type} onValueChange={(value) => setTaskForm((prev) => ({ ...prev, type: value }))} items={TYPE_ITEMS} />
                    <Input size="sm" label="节点标签选择器" value={taskForm.node_selector} onChange={(event) => setTaskForm((prev) => ({ ...prev, node_selector: event.target.value }))} />
                  </div>
                  {taskForm.type === 'shell' || taskForm.type === 'agent' ? (
                    <div>
                      <div className="mb-1 text-xs font-medium text-kumo-subtle">{taskCommandLabel}</div>
                      <CodeEditor language="shell" placeholder={taskCommandPlaceholder} value={taskForm.command} onChange={(command) => setTaskForm((prev) => ({ ...prev, command }))} minHeight="8rem" />
                    </div>
                  ) : taskForm.type === 'ai' ? (
                    <div>
                      <div className="mb-1 text-xs font-medium text-kumo-subtle">{taskCommandLabel}</div>
                      <Textarea
                        rows={6}
                        placeholder={taskCommandPlaceholder}
                        value={taskForm.command}
                        onChange={(event) => setTaskForm((prev) => ({ ...prev, command: event.target.value }))}
                        className="w-full"
                      />
                    </div>
                  ) : (
                    <Input size="sm" label={taskCommandLabel} placeholder={taskCommandPlaceholder} value={taskForm.command} onChange={(event) => setTaskForm((prev) => ({ ...prev, command: event.target.value }))} />
                  )}
                </div>
              </FormCard>

              <FormCard icon={<Sliders className="h-4 w-4" />} title="执行参数" description="超时、重试与并发控制">
                <div className="grid gap-3 py-4 cq-sm:grid-cols-2">
                  <Input size="sm" type="number" label="超时秒数" min="1" value={taskForm.timeout_seconds} onChange={(event) => setTaskForm((prev) => ({ ...prev, timeout_seconds: Number(event.target.value) }))} />
                  <Input size="sm" type="number" label="重试次数" min="0" value={taskForm.retry_count} onChange={(event) => setTaskForm((prev) => ({ ...prev, retry_count: Number(event.target.value) }))} />
                  <Input size="sm" type="number" label="重试间隔" min="0" value={taskForm.retry_interval_seconds} onChange={(event) => setTaskForm((prev) => ({ ...prev, retry_interval_seconds: Number(event.target.value) }))} />
                  <Input size="sm" type="number" label="最大并发" min="1" value={taskForm.max_concurrency} onChange={(event) => setTaskForm((prev) => ({ ...prev, max_concurrency: Number(event.target.value) }))} />
                </div>
                <div className="flex items-center justify-between border-t border-kumo-line py-3">
                  <span className="text-sm font-medium text-kumo-strong">启用任务</span>
                  <Switch checked={taskForm.enabled === 1} onCheckedChange={(checked) => setTaskForm((prev) => ({ ...prev, enabled: checked ? 1 : 0 }))} />
                </div>
              </FormCard>
            </div>
            <div className="min-w-0 space-y-4">

              {taskForm.type === 'ai' && (
                <FormCard icon={<Sparkle className="h-4 w-4" />} title="AI 执行配置" description="可调用全部内部接口">
                  <div className="grid gap-3 py-4 cq-sm:grid-cols-2">
                    <Select alignItemWithTrigger size="sm" label="推理模型" className="w-full" value={taskForm.aiModel} onValueChange={(value) => setTaskForm((prev) => ({ ...prev, aiModel: String(value) }))} items={aiModelOptions} />
                    <Select alignItemWithTrigger size="sm" label="写操作策略" className="w-full" value={taskForm.aiPolicy} onValueChange={(value) => setTaskForm((prev) => ({ ...prev, aiPolicy: String(value) }))} items={AI_POLICY_ITEMS} />
                    <Select alignItemWithTrigger size="sm" label="结果推送" className="w-full cq-sm:col-span-2" value={taskForm.aiChannelId} onValueChange={(value) => setTaskForm((prev) => ({ ...prev, aiChannelId: String(value) }))} items={[{ value: '', label: '不推送（仅记录到运行结果）' }, ...aiChannelOptions]} />
                  </div>
                  <div className="border-t border-kumo-line py-3 text-xs text-kumo-subtle">
                    策略「完全允许」时写操作免审批执行。
                  </div>
                </FormCard>
              )}

              <FormCard icon={<Clock className="h-4 w-4" />} title="调度规则" description="可视化 Cron 或自定义表达式">
                <div className="py-4">
                  <CronEditor form={taskForm} setForm={setTaskForm} preview={cronPreview} previewError={cronPreviewError} />
                </div>
              </FormCard>
              </div>
              </div>
            </div>
            <div className="mt-3 flex shrink-0 items-center justify-end gap-2 border-t border-kumo-line pt-3">
              <Button size="sm" variant="secondary" onClick={() => setTaskDialogOpen(false)}><X className="h-3.5 w-3.5" />取消</Button>
              <Button size="sm" variant="primary" onClick={saveTask} disabled={saving || Boolean(cronPreviewError)}><Save className="h-3.5 w-3.5" />保存</Button>
            </div>
          </Dialog>
        </Dialog.Root>

        <Dialog.Root open={workflowDialogOpen} onOpenChange={setWorkflowDialogOpen}>
          <Dialog className="@container scheduler-workflow-dialog flex h-[min(720px,calc(100dvh-2rem))] flex-col overflow-hidden p-5 cq-sm:p-6">
            <div className="flex min-h-0 flex-1 flex-col gap-3">
              <div className="grid min-h-0 flex-1 gap-3 overflow-y-auto pr-1 cq-md:grid-cols-[minmax(0,1fr)_26rem] cq-md:overflow-hidden cq-md:pr-0">
                <div className="flex min-h-0 flex-col gap-3 overflow-y-auto pr-1">
                  <FormCard icon={<GitBranch className="h-4 w-4" />} title="工作流信息" description="名称、触发规则与启用状态">
                    <div className="grid gap-3 py-3 cq-lg:grid-cols-[minmax(0,1fr)_8rem]">
                      <div className="grid min-w-0 gap-3 cq-sm:grid-cols-2">
                        <Input size="sm" label="名称" value={workflowForm.name} onChange={(event) => setWorkflowForm((prev) => ({ ...prev, name: event.target.value }))} />
                        <Input size="sm" label="Cron（留空为手动）" value={workflowForm.schedule} onChange={(event) => setWorkflowForm((prev) => ({ ...prev, schedule: event.target.value }))} />
                      </div>
                      <div className="flex h-8 items-center justify-end gap-2 self-end">
                        <span className="text-sm font-medium text-kumo-strong">启用</span>
                        <Switch checked={workflowForm.enabled === 1} onCheckedChange={(checked) => setWorkflowForm((prev) => ({ ...prev, enabled: checked ? 1 : 0 }))} />
                      </div>
                    </div>
                    <div className="border-t border-kumo-line pb-3 pt-3">
                      <Input size="sm" label="描述" value={workflowForm.description} onChange={(event) => setWorkflowForm((prev) => ({ ...prev, description: event.target.value }))} />
                    </div>
                  </FormCard>
                  <LayerCard className="scheduler-workflow-canvas-editor-card flex min-h-0 flex-1 flex-col overflow-hidden rounded-xl border border-kumo-line bg-kumo-elevated shadow-none ring-0">
                    <LayerCard.Secondary className={`${sectionCardHeaderClass} my-0 shrink-0`}>
                      <div className="flex min-w-0 items-center gap-2.5 text-sm font-semibold text-kumo-strong">
                        <span className="flex h-7 w-7 shrink-0 items-center justify-center rounded-lg bg-kumo-fill text-brand">
                          <GitBranch className="h-4 w-4" />
                        </span>
                        <span className="truncate">流程画布</span>
                        <span className="shrink-0 rounded bg-kumo-recessed px-1.5 py-0.5 text-[10px] font-normal text-kumo-subtle">{workflowForm.nodes.length} 节点 / {workflowForm.edges.length} 依赖</span>
                      </div>
                      <Button size="sm" variant="secondary" onClick={addWorkflowNode}><Plus className="h-3.5 w-3.5" />新增节点</Button>
                    </LayerCard.Secondary>
                    <LayerCard.Primary className="min-h-0 flex-1 gap-0 overflow-visible bg-kumo-elevated p-0 pl-0 pr-0 pt-0 pb-0 ring-0">
                      <div className="min-h-0 flex-1 p-3">
                        <WorkflowCanvas key={`workflow-editor-${workflowCanvasEpoch}`} workflow={workflowForm} runs={[]} tasks={tasks} selectedNodeId={selectedWorkflowNode?.id} onSelectNode={setSelectedWorkflowNodeId} size="editor" />
                      </div>
                    </LayerCard.Primary>
                  </LayerCard>
                </div>

                <div className="flex min-h-0 flex-col gap-3">
                  <div className="min-h-0 flex-1 space-y-3 overflow-y-auto pb-2 pr-1">
                    <LayerCard className="flex flex-col overflow-hidden rounded-xl border border-kumo-line bg-kumo-elevated shadow-none ring-0">
                      <LayerCard.Secondary className={`${sectionCardHeaderClass} my-0`}>
                        <div className="flex min-w-0 items-center gap-2.5 text-sm font-semibold text-kumo-strong">
                          <span className="flex h-7 w-7 shrink-0 items-center justify-center rounded-lg bg-kumo-fill text-brand">
                            <Layers className="h-4 w-4" />
                          </span>
                          <span className="truncate">节点设置</span>
                        </div>
                        {selectedWorkflowNode && selectedWorkflowNode.type !== 'start' && (
                          <IconButton label="删除节点" variant="secondary-destructive" onClick={() => deleteWorkflowNode(selectedWorkflowNode.id)} icon={<Trash className="h-3.5 w-3.5" />} />
                        )}
                      </LayerCard.Secondary>
                      <LayerCard.Primary className="gap-0 overflow-visible bg-kumo-elevated px-4 pb-4 pt-3 ring-0">
                      {selectedWorkflowNode ? (
                        <div className="space-y-3">
                          <div className="grid gap-3 cq-sm:grid-cols-2">
                            <Input size="sm" label="节点名称" value={selectedWorkflowNode.name || ''} onChange={(event) => updateWorkflowNode(selectedWorkflowNode.id, { name: event.target.value })} />
                            {selectedWorkflowNode.type !== 'start' && (
                              <div className="flex items-end justify-end gap-2 pb-0.5">
                                <span className="text-sm font-medium text-kumo-strong">启用节点</span>
                                <Switch checked={selectedWorkflowNode.enabled !== 0} onCheckedChange={(checked) => updateWorkflowNode(selectedWorkflowNode.id, { enabled: checked ? 1 : 0 })} />
                              </div>
                            )}
                          </div>
                          {selectedWorkflowNode.type === 'start' ? (
                            <div className="rounded-md border border-kumo-line bg-kumo-recessed/30 px-3 py-2 text-xs text-kumo-subtle">开始节点只负责触发流程，不需要绑定任务。</div>
                          ) : (
                            <div className="space-y-3">
                              <Select alignItemWithTrigger
                                size="sm"
                                label="引用任务"
                                className="w-full"
                                value={String(selectedWorkflowNode.task_id || 0)}
                                onValueChange={(value) => updateWorkflowNodeTask(selectedWorkflowNode, value)}
                                items={taskItems}
                              />
                              {!selectedWorkflowNode.task_id && (
                                <div className="space-y-3">
                                  <div className="grid gap-3 cq-sm:grid-cols-2">
                                    <Select alignItemWithTrigger
                                      size="sm"
                                      label="节点类型"
                                      className="w-full"
                                      value={selectedWorkflowNode.type || 'shell'}
                                      onValueChange={(value) => updateWorkflowNode(selectedWorkflowNode.id, { type: value })}
                                      items={TYPE_ITEMS}
                                    />
                                    <div className="flex items-end justify-end gap-2 pb-0.5">
                                      <span className="text-sm font-medium text-kumo-strong">启用节点</span>
                                      <Switch checked={selectedWorkflowNode.enabled !== 0} onCheckedChange={(checked) => updateWorkflowNode(selectedWorkflowNode.id, { enabled: checked ? 1 : 0 })} />
                                    </div>
                                  </div>
                                  <CodeEditor
                                    label={selectedWorkflowNode.type === 'ai' ? 'AI 提示词' : '内联命令'}
                                    language="shell"
                                    value={selectedWorkflowNode.command || ''}
                                    onChange={(command) => updateWorkflowNode(selectedWorkflowNode.id, { command })}
                                    placeholder={selectedWorkflowNode.type === 'ai' ? undefined : 'echo workflow-inline-step'}
                                    minHeight="8rem"
                                  />
                                  {selectedWorkflowNode.type === 'ai' && (
                                    <Input
                                      size="sm"
                                      label="推送通知渠道 ID"
                                      value={getWorkflowNodeAiChannelId(selectedWorkflowNode) || ''}
                                      onChange={(event) => updateWorkflowNodeAiConfig({ channelId: event.target.value })}
                                      placeholder="可选：notif_ 开头的通知中心渠道 ID，完成后推送 AI 输出"
                                    />
                                  )}
                                </div>
                              )}
                            </div>
                          )}
                        </div>
                      ) : (
                        <div className="rounded-md border border-kumo-line bg-kumo-recessed/30 px-3 py-4 text-center text-xs text-kumo-subtle">从画布中选择一个节点。</div>
                      )}
                      </LayerCard.Primary>
                    </LayerCard>

                    <LayerCard className="flex flex-col overflow-hidden rounded-xl border border-kumo-line bg-kumo-elevated shadow-none ring-0">
                      <LayerCard.Secondary className={`${sectionCardHeaderClass} my-0`}>
                        <div className="flex items-center gap-2.5 text-sm font-semibold text-kumo-strong">
                          <span className="flex h-7 w-7 shrink-0 items-center justify-center rounded-lg bg-kumo-fill text-brand">
                            <Sliders className="h-4 w-4" />
                          </span>
                          依赖规则
                        </div>
                        <Button size="sm" variant="secondary" onClick={addWorkflowEdge}><Plus className="h-3.5 w-3.5" />新增</Button>
                      </LayerCard.Secondary>
                      <LayerCard.Primary className="gap-0 overflow-visible bg-kumo-elevated px-4 pb-4 pt-3 ring-0">
                      <div className="space-y-2">
                        {workflowForm.edges.length === 0 && (
                          <div className="rounded-md border border-kumo-line bg-kumo-recessed/30 px-3 py-4 text-center text-xs text-kumo-subtle">暂无依赖规则，节点会独立存在。点击「新增」为节点连线。</div>
                        )}
                        {workflowForm.edges.map((edge, index) => (
                          <div key={edge.id} className="grid gap-2 rounded-md border border-kumo-line p-2.5">
                            <div className="grid min-w-0 grid-cols-[minmax(0,1fr)_auto_minmax(0,1fr)] items-end gap-2">
                              <Select alignItemWithTrigger size="sm" label="来源" className="w-full" value={edge.from} onValueChange={(value) => setWorkflowForm((prev) => ({ ...prev, edges: prev.edges.map((item, i) => i === index ? { ...item, from: value } : item) }))} items={workflowNodeItems} />
                              <ArrowRight className="mb-2 h-3.5 w-3.5 shrink-0 text-kumo-subtle" />
                              <Select alignItemWithTrigger size="sm" label="目标" className="w-full" value={edge.to} onValueChange={(value) => setWorkflowForm((prev) => ({ ...prev, edges: prev.edges.map((item, i) => i === index ? { ...item, to: value } : item) }))} items={workflowNodeItems} />
                            </div>
                            <div className="grid min-w-0 grid-cols-[minmax(0,1fr)_auto] items-end gap-2">
                              <Select alignItemWithTrigger size="sm" label="触发条件" className="w-full" value={edge.condition} onValueChange={(value) => setWorkflowForm((prev) => ({ ...prev, edges: prev.edges.map((item, i) => i === index ? { ...item, condition: value } : item) }))} items={CONDITION_ITEMS} />
                              <IconButton label="删除依赖" variant="secondary-destructive" onClick={() => setWorkflowForm((prev) => ({ ...prev, edges: prev.edges.filter((_, i) => i !== index) }))} icon={<Trash className="h-3.5 w-3.5" />} />
                            </div>
                          </div>
                        ))}
                      </div>
                      </LayerCard.Primary>
                    </LayerCard>
                  </div>
                </div>
              </div>

              <div className="flex shrink-0 items-center justify-end gap-2 border-t border-kumo-line pt-3"><Button size="sm" variant="secondary" onClick={() => setWorkflowDialogOpen(false)}>取消</Button><Button size="sm" variant="primary" onClick={saveWorkflow} disabled={saving}><Save className="h-3.5 w-3.5" />保存</Button></div>
            </div>
          </Dialog>
        </Dialog.Root>

        <Dialog.Root open={Boolean(taskLogsTarget)} onOpenChange={(open) => !open && setTaskLogsTarget(null)}>
          <Dialog className="@container scheduler-task-dialog flex h-[min(680px,calc(100dvh-2rem))] w-[min(960px,calc(100vw-2rem))] flex-col overflow-hidden p-5 cq-sm:p-6">
            <Dialog.Title className="mb-4 shrink-0 text-base font-semibold text-kumo-strong">运行日志{taskLogsTarget ? `：${taskLogsTarget.name}` : ''}</Dialog.Title>
            <div className="grid min-h-0 flex-1 gap-0 cq-md:grid-cols-[minmax(0,240px)_minmax(0,1fr)]">
              <div className="flex min-h-0 flex-col gap-1 overflow-y-auto cq-md:border-r cq-md:border-kumo-line cq-md:pr-3">
                {taskLogsLoading ? (
                  <div className="space-y-2">
                    <SkeletonLine className="h-11" />
                    <SkeletonLine className="h-11" />
                    <SkeletonLine className="h-11" />
                  </div>
                ) : taskLogs.length === 0 ? (
                  <div className="rounded-lg border border-kumo-line p-6 text-center text-sm text-kumo-subtle">暂无运行日志</div>
                ) : (
                  taskLogs.map((logItem) => {
                    const isActive = logItem.id === taskLogsSelectedId;
                    return (
                      <Button
                        key={logItem.id}
                        variant="ghost"
                        size="sm"
                        className={`h-10 w-full justify-start rounded-md px-3 ${isActive ? 'bg-brand/15' : ''}`}
                        onClick={() => setTaskLogsSelectedId(logItem.id)}
                      >
                        <span className="flex w-full min-w-0 items-center justify-between gap-2">
                          <span className="truncate font-mono text-xs text-kumo-strong">{formatTimestamp(logItem.start_time)}</span>
                          <Badge variant={statusBadgeVariant(logItem.status)} appearance="dot" className="shrink-0">{statusLabel(logItem.status)}</Badge>
                        </span>
                      </Button>
                    );
                  })
                )}
              </div>
              <div className="min-h-0 overflow-y-auto cq-md:pl-3">
                {(() => {
                  const active = taskLogs.find((item) => item.id === taskLogsSelectedId);
                  if (!active) {
                    return <div className="flex h-full min-h-32 items-center justify-center rounded-lg border border-dashed border-kumo-line p-6 text-center text-sm text-kumo-subtle">选择左侧记录查看详情</div>;
                  }
                  return (
                    <LayerCard className="flex h-full min-h-0 flex-col overflow-hidden rounded-xl border border-kumo-line bg-kumo-elevated shadow-none ring-0">
                      <LayerCard.Secondary className={`${sectionCardHeaderClass} my-0 shrink-0`}>
                        <div className="flex min-w-0 items-center gap-3 text-xs text-kumo-subtle">
                          <span className="whitespace-nowrap font-mono">{formatTimestamp(active.start_time)}</span>
                          {active.duration != null && <span className="whitespace-nowrap">｜耗时 {active.duration}s</span>}
                        </div>
                        <Badge variant={statusBadgeVariant(active.status)} appearance="dot" className="shrink-0">{statusLabel(active.status)}</Badge>
                      </LayerCard.Secondary>
                      <LayerCard.Primary className="flex-1 gap-0 overflow-auto bg-kumo-elevated px-4 py-3 ring-0">
                        {renderLogOutput(active.output, taskLogsTarget?.type === 'ai')}
                      </LayerCard.Primary>
                    </LayerCard>
                  );
                })()}
              </div>
            </div>
            <div className="mt-4 flex shrink-0 items-center justify-end gap-2 border-t border-kumo-line pt-3">
              <Button size="sm" variant="secondary" onClick={() => setTaskLogsTarget(null)}><X className="h-3.5 w-3.5" />关闭</Button>
            </div>
          </Dialog>
        </Dialog.Root>

        <Dialog.Root open={Boolean(selectedRun)} onOpenChange={(open) => !open && setSelectedRun(null)}>
          <Dialog className="@container scheduler-task-dialog flex max-h-[calc(100dvh-2rem)] flex-col overflow-hidden p-5 cq-sm:p-6">
            <Dialog.Title className="mb-4 shrink-0 text-base font-semibold text-kumo-strong">运行详情</Dialog.Title>
            {selectedRun && (
              <div className="min-h-0 flex-1 space-y-4 overflow-y-auto pr-1">
                {selectedRun.workflow && <WorkflowCanvas workflow={selectedRun.workflow} runs={[selectedRun]} tasks={tasks} />}
                <div className="grid gap-3">
                  {(selectedRun.node_runs || []).map((nodeRun) => {
                    const wfNode = (selectedRun.workflow?.nodes || []).find((n) => n.id === nodeRun.node_id || n.name === nodeRun.node_name);
                    const linkedTask = tasks.find((t) => String(t.id) === String(wfNode?.task_id || nodeRun.task_id));
                    const isAi = wfNode?.type === 'ai' || linkedTask?.type === 'ai';
                    return (
                      <LayerCard key={nodeRun.id} className="flex flex-col overflow-hidden rounded-xl border border-kumo-line bg-kumo-elevated shadow-none ring-0">
                        <LayerCard.Secondary className={`${sectionCardHeaderClass} my-0`}>
                          <div className="flex min-w-0 items-center gap-2.5">
                            <span className={`flex h-7 w-7 shrink-0 items-center justify-center rounded-lg ${isAi ? 'bg-brand/10 text-brand' : 'bg-kumo-fill text-brand'}`}>
                              {isAi ? <Sparkle className="h-4 w-4" /> : <Activity className="h-4 w-4" />}
                            </span>
                            <div className="min-w-0">
                              <div className="flex items-center gap-2">
                                <span className="truncate text-sm font-semibold text-kumo-strong">{nodeRun.node_name}</span>
                                {isAi && <Badge variant="purple">AI</Badge>}
                              </div>
                              <div className="text-xs text-kumo-subtle">{formatTimestamp(nodeRun.start_time)} / {nodeRun.duration ?? 0}s</div>
                            </div>
                          </div>
                          <Badge variant={statusBadgeVariant(nodeRun.status)} appearance="dot">{statusLabel(nodeRun.status)}</Badge>
                        </LayerCard.Secondary>
                        <LayerCard.Primary className="gap-0 overflow-visible bg-kumo-elevated px-4 py-3 ring-0">
                          {isAi ? (
                            <div className="max-h-80 overflow-auto">
                              {renderLogOutput(nodeRun.output, true)}
                            </div>
                          ) : (
                            <pre className="max-h-60 overflow-auto whitespace-pre-wrap break-words rounded-md bg-kumo-recessed p-3 text-xs text-kumo-default">{formatOutputText(nodeRun.output) || '无输出'}</pre>
                          )}
                          <Button size="sm" variant="secondary" className="mt-2" onClick={() => navigator.clipboard?.writeText(nodeRun.output || '')}><Copy className="h-3.5 w-3.5" />复制输出</Button>
                        </LayerCard.Primary>
                      </LayerCard>
                    );
                  })}
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
