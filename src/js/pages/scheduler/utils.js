import {
  CONDITION_ITEMS,
  DEFAULT_WORKFLOW_FORM,
  STATUS_LABELS,
  TYPE_ITEMS,
  WORKFLOW_CANVAS_SIZES,
} from './constants.js';

export function getCronExpressionFromSimple(form) {
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

export function parseSimpleSchedule(schedule = '') {
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

export function clampInteger(value, min, max, fallback) {
  const next = Number(value);
  if (!Number.isFinite(next)) return fallback;
  return Math.min(max, Math.max(min, Math.trunc(next)));
}

export function formatTimestamp(value) {
  if (!value) return '-';
  const millis = Number(value) * 1000;
  if (!Number.isFinite(millis)) return '-';
  return new Date(millis).toLocaleString('zh-CN', { hour12: false });
}

// tryFormatJson 尝试把文本解析并格式化 JSON；非 JSON 返回 null。
// 字符串字段字面量中的 \n 转义还原为真实换行（如 output 里的多行 Markdown/文本）。
export function tryFormatJson(text) {
  if (!text) return null;
  const trimmed = String(text).trim();
  if (!(trimmed.startsWith('{') || trimmed.startsWith('['))) return null;
  try {
    return JSON.stringify(JSON.parse(trimmed), null, 2).replace(/\\n/g, '\n');
  } catch {
    return null;
  }
}

// formatOutputText 兼容旧调用（返回字符串，非 JSON 原样返回）
export function formatOutputText(text) {
  if (!text) return '';
  const json = tryFormatJson(text);
  return json != null ? json : String(text);
}

export function statusBadgeVariant(status) {
  if (status === 'success' || status === 'online') return 'success';
  if (status === 'failed' || status === 'timeout' || status === 'offline') return 'error';
  if (status === 'running' || status === 'queued') return 'warning';
  if (status === 'skipped' || status === 'unknown') return 'neutral';
  return 'none';
}

export function statusLabel(status) {
  return STATUS_LABELS[status] || (status === 'online' ? '在线' : status === 'offline' ? '离线' : status || '未知');
}

export function taskTypeLabel(value) {
  return TYPE_ITEMS.find((item) => item.value === value)?.label || value || 'Shell 命令';
}

export function workflowNodeTypeLabel(node) {
  if (node.type === 'start') return '开始节点';
  if (node.type === 'end') return '结束节点';
  if (node.type === 'ai') return 'AI 智能任务';
  if (node.task_id) return `任务 #${node.task_id}`;
  if (node.type === 'task') return '未绑定任务';
  return node.command ? '内联命令' : taskTypeLabel(node.type);
}

export function workflowNodeKindLabel(node) {
  if (node.type === 'start') return '入口';
  if (node.type === 'end') return '出口';
  if (node.type === 'ai') return 'AI';
  if (node.task_id) return '引用任务';
  if (node.type === 'task') return '待绑定';
  return '内联';
}

export function workflowNodeKindVariant(node) {
  if (node.type === 'start' || node.type === 'end') return 'blue';
  if (node.type === 'ai') return 'purple';
  if (node.task_id) return 'purple';
  if (node.type === 'task') return 'orange';
  return 'teal';
}

export function conditionLabel(value) {
  return CONDITION_ITEMS.find((item) => item.value === value)?.label || '成功后';
}

export function summarizeOutput(output = '') {
  const text = String(output || '').trim();
  if (!text) return '无输出';
  const status = text.match(/Status:\s*(\d+)/i)?.[1];
  if (status) {
    const body = text.replace(/Status:\s*\d+\s*/i, '').replace(/^Data:\s*/i, '').trim();
    return `HTTP ${status}${body ? ` / ${body.slice(0, 90)}` : ''}`;
  }
  return text.slice(0, 120);
}

export function cloneWorkflowForm(workflow = null) {
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

export function compareWorkflowNodes(a, b) {
  if (a.type === 'start' && b.type !== 'start') return -1;
  if (a.type !== 'start' && b.type === 'start') return 1;
  const xDiff = (a.x ?? 0) - (b.x ?? 0);
  if (xDiff !== 0) return xDiff;
  const yDiff = (a.y ?? 0) - (b.y ?? 0);
  if (yDiff !== 0) return yDiff;
  return String(a.name || a.id).localeCompare(String(b.name || b.id), 'zh-CN');
}

export function sortWorkflowNodes(nodes = []) {
  return [...nodes].sort(compareWorkflowNodes);
}

export function sortWorkflowEdges(edges = [], nodeMap) {
  return [...edges].sort((a, b) => {
    const from = nodeMap.get(a.to);
    const to = nodeMap.get(b.to);
    if (!from || !to) return 0;
    return compareWorkflowNodes(from, to);
  });
}

export function getValidWorkflowEdges(nodes = [], edges = []) {
  const nodeMap = new Map(nodes.map((node) => [node.id, node]));
  return edges.filter((edge) => nodeMap.has(edge.from) && nodeMap.has(edge.to));
}

export function buildWorkflowFlowStages(nodes = [], edges = []) {
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

// S 形圆角连线（与 GitHub makeActionStageBranchPath 同款）：母线靠近目标端，尽量不穿越中间节点
export function routeWorkflowEdge(source, target, cfg) {
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
export function buildWorkflowCanvasLayout(nodes = [], edges = [], size = 'default') {
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
