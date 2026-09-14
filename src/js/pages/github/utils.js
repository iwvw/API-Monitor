import { normalizeWorkflowJobName, workflowJobMatchesDefinition } from '../../modules/githubWorkflowJobs.js';
import {
  ACTION_FLOW_BRANCH_INSET,
  ACTION_FLOW_CARD_WIDTH,
  ACTION_FLOW_MIN_SCALE,
  ACTION_FLOW_PADDING_X,
  ACTION_FLOW_PADDING_Y,
  ACTION_FLOW_ROW_GAP,
  ACTION_FLOW_STAGE_GAP,
  ACTION_FLOW_VIEWPORT_HEIGHT,
} from './constants.js';

const statusTone = (status) => {
  const value = String(status || '').toLowerCase();
  if (['success', 'completed', 'active'].includes(value)) return 'success';
  if (['partial', 'partial_success', 'partial-success'].includes(value)) return 'warning';
  if (['failure', 'failed', 'error', 'timed_out', 'cancelled', 'action_required', 'startup_failure', 'critical'].includes(value)) return 'error';
  if (['in_progress', 'queued', 'pending', 'requested', 'waiting', 'running', 'warning', 'rate_limited'].includes(value)) return 'warning';
  return 'neutral';
};

const statusLabel = (status) => ({
  success: '成功',
  completed: '已完成',
  active: '已启用',
  failure: '失败',
  failed: '失败',
  error: '错误',
  timed_out: '超时',
  cancelled: '已取消',
  action_required: '需要操作',
  startup_failure: '启动失败',
  critical: '严重',
  partial: '部分成功',
  partial_success: '部分成功',
  'partial-success': '部分成功',
  in_progress: '运行中',
  running: '运行中',
  queued: '排队中',
  pending: '等待中',
  requested: '已请求',
  waiting: '等待中',
  warning: '警告',
  rate_limited: '已限流',
  skipped: '已跳过',
  stale: '已过期',
  disabled: '已停用',
  neutral: '未知',
  unknown: '未知',
  info: '信息',
}[String(status || '').toLowerCase()] || status || '未知');

const tokenTestStatusLabel = (status) => ({
  success: '权限通过',
  warning: '权限不完整',
  failed: 'Token 无效',
  unknown: '未检测',
}[String(status || '').toLowerCase()] || status || '未检测');

const formatNumber = (value) => Number(value || 0).toLocaleString('en-US', { useGrouping: false });
const formatDateTime = (value) => {
  if (!value) return '-';
  const date = new Date(value);
  if (Number.isNaN(date.getTime())) return String(value);
  return date.toLocaleString();
};

const formatResetCountdown = (value, now) => {
  const resetAt = new Date(value).getTime();
  if (!Number.isFinite(resetAt)) return '';
  const remainingMinutes = Math.max(0, Math.ceil((resetAt - now) / 60000));
  if (remainingMinutes === 0) return '即将重置';
  return `${Math.floor(remainingMinutes / 60)}时${remainingMinutes % 60}分后重置`;
};

const formatActionDuration = (startedAt, finishedAt, now) => {
  const started = new Date(startedAt).getTime();
  const finished = new Date(finishedAt).getTime();
  if (!Number.isFinite(started)) return '-';
  const end = Number.isFinite(finished) ? finished : now;
  const totalSeconds = Math.max(0, Math.floor((end - started) / 1000));
  const hours = Math.floor(totalSeconds / 3600);
  const minutes = Math.floor((totalSeconds % 3600) / 60);
  const seconds = totalSeconds % 60;
  if (hours > 0) return `${hours}时${minutes}分${seconds}秒`;
  if (minutes > 0) return `${minutes}分${seconds}秒`;
  return `${seconds}秒`;
};

const actionFlowStatusDotClass = (status) => {
  const value = String(status || '').toLowerCase();
  if (['success', 'completed', 'active'].includes(value)) {
    return 'bg-kumo-success ring-1 ring-kumo-success/30';
  }
  if (['partial', 'partial_success', 'partial-success', 'in_progress', 'queued', 'pending', 'requested', 'waiting', 'running', 'warning', 'rate_limited'].includes(value)) {
    return 'bg-kumo-warning ring-1 ring-kumo-warning/30';
  }
  if (['failure', 'failed', 'error', 'timed_out', 'action_required', 'startup_failure', 'critical'].includes(value)) {
    return 'bg-kumo-danger ring-1 ring-kumo-danger/30';
  }
  if (['cancelled', 'skipped', 'stale', 'disabled'].includes(value)) {
    return 'bg-kumo-line ring-1 ring-kumo-line/40';
  }
  return 'bg-kumo-info ring-1 ring-kumo-info/30';
};

const actionFlowStatusMetaClass = (status, muted = false) => {
  if (muted) return 'text-kumo-subtle/80';
  const tone = statusTone(status);
  if (tone === 'success') return 'text-kumo-success';
  if (tone === 'error') return 'text-kumo-danger';
  if (tone === 'warning') return 'text-kumo-warning';
  return 'text-kumo-subtle';
};

const workflowJobStep = (job) => {
  const steps = Array.isArray(job?.steps) ? job.steps : [];
  return steps.find((step) => step.status === 'in_progress') || null;
};

const workflowJobPriority = (value) => ({
  failure: 7,
  failed: 7,
  timed_out: 7,
  cancelled: 6,
  action_required: 6,
  in_progress: 5,
  requested: 4,
  queued: 4,
  waiting: 4,
  pending: 4,
  skipped: 2,
  neutral: 2,
  success: 1,
  completed: 1,
})[normalizeWorkflowJobName(value)] || 3;

const summarizeWorkflowJobs = (definition, jobs, now) => {
  const relatedJobs = jobs.length > 0 ? jobs : [];
  const ranked = [...relatedJobs].sort((a, b) => workflowJobPriority(b.conclusion || b.status) - workflowJobPriority(a.conclusion || a.status));
  const primary = ranked[0] || null;
  const status = primary ? (primary.conclusion || primary.status) : 'queued';
  const activeStep = relatedJobs.map(workflowJobStep).find((step) => step?.status === 'in_progress') || (primary ? workflowJobStep(primary) : null);
  const starts = relatedJobs.map((job) => new Date(job.started_at).getTime()).filter(Number.isFinite);
  const finishes = relatedJobs.map((job) => new Date(job.completed_at).getTime()).filter(Number.isFinite);
  const startedAt = starts.length > 0 ? new Date(Math.min(...starts)).toISOString() : primary?.started_at;
  const completedAt = relatedJobs.length > 0 && finishes.length === relatedJobs.length ? new Date(Math.max(...finishes)).toISOString() : primary?.completed_at;
  const variants = relatedJobs.map((job) => ({
    id: job.id,
    definitionId: definition.id,
    name: job.name || definition.name || definition.id,
    status: job.conclusion || job.status || 'queued',
    duration: formatActionDuration(job.started_at, job.completed_at, now),
  }));
  return {
    id: definition.id,
    name: definition.matrix ? `Matrix: ${definition.id}` : (definition.name || definition.id),
    status,
    step: activeStep,
    duration: formatActionDuration(startedAt, completedAt, now),
    completedAt,
    count: relatedJobs.length,
    matrix: definition.matrix || relatedJobs.length > 1,
    variants,
  };
};

const flattenWorkflowDefinitions = (workflow) => (Array.isArray(workflow?.layers)
  ? workflow.layers.flat().map((definition) => ({ ...definition, needs: Array.isArray(definition.needs) ? [...definition.needs] : [] }))
  : []);

const buildWorkflowGraph = (workflow, jobs, now) => {
  const definitions = flattenWorkflowDefinitions(workflow);
  if (definitions.length === 0) {
    return {
      definitions: jobs.map((job) => ({ id: String(job.id), name: job.name, needs: [] })),
      summaries: new Map(jobs.map((job) => [String(job.id), summarizeWorkflowJobs({ id: String(job.id), name: job.name, needs: [] }, [job], now)])),
      unmatchedJobs: [],
      fallback: true,
    };
  }
  const used = new Set();
  const summaries = new Map();
  definitions.forEach((definition) => {
    const related = jobs.filter((job) => workflowJobMatchesDefinition(job, definition));
    related.forEach((job) => used.add(job.id));
    summaries.set(definition.id, summarizeWorkflowJobs(definition, related, now));
  });
  return { definitions, summaries, unmatchedJobs: jobs.filter((job) => !used.has(job.id)), fallback: false };
};

const workflowGroupName = (definitions) => {
  const names = definitions.map((definition) => String(definition.name || definition.id || '').trim()).filter(Boolean);
  if (names.length === 0) return '';
  const prefixes = names.map((name) => name.split(/[-:_\s/]+/)[0]).filter(Boolean);
  if (prefixes.length === names.length && prefixes.every((prefix) => prefix.toLowerCase() === prefixes[0].toLowerCase())) {
    const prefix = prefixes[0];
    return prefix.length <= 3 ? prefix.toUpperCase() : prefix;
  }
  return '';
};

const workflowDependentsById = (definitions) => {
  const dependents = new Map(definitions.map((definition) => [definition.id, []]));
  definitions.forEach((definition) => {
    (definition.needs || []).forEach((need) => {
      if (dependents.has(need)) dependents.get(need).push(definition.id);
    });
  });
  dependents.forEach((items) => items.sort());
  return dependents;
};

const workflowDescendantCounts = (definitions, dependents) => {
  const counts = new Map();
  const visit = (id, seen = new Set()) => {
    (dependents.get(id) || []).forEach((childId) => {
      if (seen.has(childId)) return;
      seen.add(childId);
      visit(childId, seen);
    });
    return seen;
  };
  definitions.forEach((definition) => {
    counts.set(definition.id, visit(definition.id).size);
  });
  return counts;
};

const workflowOfficialOrderHint = (definition) => {
  const id = normalizeWorkflowJobName(definition?.id);
  const hints = {
    base: 10,
    prek: 20,
    zizmor: 30,
    'lint-hadolint': 40,
    hassfest: 10,
    'gen-requirements-all': 11,
    mypy: 12,
    'prepare-pytest-full': 20,
    'dependency-review': 30,
    pylint: 31,
    'pylint-tests': 32,
    'audit-licenses': 40,
    'pytest-mariadb': 10,
    'pytest-partial': 20,
    'pytest-postgres': 30,
    'pytest-full': 40,
    'coverage-full': 10,
    'coverage-partial': 20,
    'upload-test-results': 30,
  };
  return hints[id] ?? null;
};

const sortWorkflowGroups = (groups, orderById, dependents, descendantCounts) => [...groups].sort((a, b) => {
  const hintedOrder = (group) => {
    const hints = group.map(workflowOfficialOrderHint).filter((value) => value != null);
    return hints.length > 0 ? Math.min(...hints) : null;
  };
  const hintA = hintedOrder(a);
  const hintB = hintedOrder(b);
  if (hintA != null || hintB != null) {
    if (hintA == null) return 1;
    if (hintB == null) return -1;
    if (hintA !== hintB) return hintA - hintB;
  }
  const score = (group) => Math.max(...group.map((definition) => (
    (descendantCounts.get(definition.id) || 0) * 10 + (dependents.get(definition.id)?.length || 0)
  )));
  const scoreDelta = score(b) - score(a);
  if (scoreDelta !== 0) return scoreDelta;
  const order = (group) => Math.min(...group.map((definition) => orderById.get(definition.id) ?? Number.MAX_SAFE_INTEGER));
  return order(a) - order(b);
});

const groupWorkflowLayer = (layer, dependents) => {
  const groupKeyForDefinition = (definition) => {
    if (definition.matrix) return `matrix:${definition.id}`;
    const needs = (definition.needs || []).slice().sort().join(',');
    const downstream = (dependents.get(definition.id) || []).join(',');
    return `${needs}|${downstream}`;
  };
  const groups = new Map();
  layer.forEach((definition) => {
    const key = groupKeyForDefinition(definition);
    if (!groups.has(key)) groups.set(key, []);
    groups.get(key).push(definition);
  });
  const grouped = [];
  layer.forEach((definition) => {
    const key = groupKeyForDefinition(definition);
    const group = groups.get(key);
    if (!group) return;
    groups.delete(key);
    grouped.push(group.length > 1 ? group : [definition]);
  });
  return grouped;
};

const summarizeWorkflowGroup = (definitions, summaries) => {
  if (definitions.length === 1) return summaries.get(definitions[0].id);
  const memberSummaries = definitions.map((definition) => summaries.get(definition.id)).filter(Boolean);
  const ranked = [...memberSummaries].sort((a, b) => workflowJobPriority(b.status) - workflowJobPriority(a.status));
  const primary = ranked[0] || memberSummaries[0];
  const completed = memberSummaries.map((item) => new Date(item.completedAt).getTime()).filter(Number.isFinite);
  return {
    id: `group-${definitions.map((definition) => definition.id).join('-')}`,
    name: workflowGroupName(definitions),
    status: primary?.status || 'queued',
    duration: primary?.duration || '',
    completedAt: completed.length === memberSummaries.length ? new Date(Math.max(...completed)).toISOString() : primary?.completedAt,
    count: memberSummaries.length,
    group: true,
    variants: memberSummaries.map((summary) => ({
      id: summary.id,
      name: summary.name,
      status: summary.status,
      duration: summary.duration,
    })),
  };
};

const actionFlowExpandedVariantsHeight = (count) => {
  if (!Number.isFinite(count) || count <= 0) return 0;
  const rowHeight = 32;
  const rowGap = 8;
  const containerInset = 13;
  return containerInset + count * rowHeight + Math.max(0, count - 1) * rowGap;
};

const actionFlowNodeHeight = (node, expanded = false) => {
  if (!node) return 44;
  const variants = Array.isArray(node.variants) ? node.variants : [];
  if (node.group) {
    const rowCount = Math.max(1, variants.length);
    const headerHeight = node.name ? 24 : 0;
    const rowHeight = 30;
    const gaps = Math.max(0, rowCount - 1) * 8 + (node.name ? 10 : 0);
    return 24 + headerHeight + rowCount * rowHeight + gaps;
  }
  if (!node.matrix && !node.step && variants.length <= 1) return 52;

  const rows = [node.matrix ? 52 : 28];
  const showActiveStep = Boolean(node.step && ['in_progress', 'running', 'queued', 'pending', 'waiting'].includes(normalizeWorkflowJobName(node.status)));
  if (showActiveStep) rows.push(24);
  rows.push(24);
  if (expanded && variants.length > 1) rows.push(actionFlowExpandedVariantsHeight(variants.length));
  rows.push(24);
  return 24 + rows.reduce((sum, height) => sum + height, 0) + Math.max(0, rows.length - 1) * 8;
};

const actionFlowPortY = (rect) => rect.y + Math.max(22, Math.min(26, rect.height - 24));

const workflowReachable = (from, to, dependents, visited = new Set()) => {
  if (from === to) return true;
  if (visited.has(from)) return false;
  visited.add(from);
  return (dependents.get(from) || []).some((child) => workflowReachable(child, to, dependents, visited));
};

const reduceWorkflowEdges = (definitions, dependents) => {
  const edges = [];
  definitions.forEach((definition) => {
    const needs = (definition.needs || []).filter((need) => dependents.has(need));
    needs.forEach((need) => {
      const expressedByIntermediate = needs.some((otherNeed) => (
        otherNeed !== need && workflowReachable(need, otherNeed, dependents)
      ));
      if (!expressedByIntermediate) edges.push({ from: need, to: definition.id });
    });
  });
  return edges;
};

const workflowTrace = (focusIds, edges, mode = 'neighbors') => {
  if (!focusIds || focusIds.size === 0) return null;
  const parents = new Map();
  const children = new Map();
  edges.forEach((edge) => {
    if (!parents.has(edge.to)) parents.set(edge.to, []);
    if (!children.has(edge.from)) children.set(edge.from, []);
    parents.get(edge.to).push(edge);
    children.get(edge.from).push(edge);
  });
  const tracedNodes = new Set(focusIds);
  const tracedEdges = new Map();
  const includeEdge = (edge, kind) => {
    tracedNodes.add(edge.from);
    tracedNodes.add(edge.to);
    tracedEdges.set(`${edge.from}->${edge.to}`, { kind });
  };
  focusIds.forEach((id) => {
    (parents.get(id) || []).forEach((edge) => includeEdge(edge, 'neighbor'));
    (children.get(id) || []).forEach((edge) => includeEdge(edge, 'neighbor'));
  });
  if (mode === 'neighbors') return { nodes: tracedNodes, edges: tracedEdges };

  const visitedParents = new Set();
  const visitParents = (id) => {
    if (visitedParents.has(id)) return;
    visitedParents.add(id);
    (parents.get(id) || []).forEach((edge) => {
      includeEdge(edge, 'upstream');
      visitParents(edge.from);
    });
  };
  focusIds.forEach(visitParents);
  return { nodes: tracedNodes, edges: tracedEdges };
};

const makeRoundedOrthogonalPath = (points, radius = 20) => {
  const compact = points.filter((point, index) => (
    index === 0 || point.x !== points[index - 1].x || point.y !== points[index - 1].y
  ));
  if (compact.length < 2) return '';
  const commands = [`M ${compact[0].x} ${compact[0].y}`];
  const lineTo = (point, previous) => {
    if (point.y === previous.y) commands.push(`H ${point.x}`);
    else commands.push(`V ${point.y}`);
  };
  let cursor = compact[0];
  for (let index = 1; index < compact.length - 1; index += 1) {
    const previous = compact[index - 1];
    const corner = compact[index];
    const next = compact[index + 1];
    const incoming = Math.abs(corner.x - previous.x) + Math.abs(corner.y - previous.y);
    const outgoing = Math.abs(next.x - corner.x) + Math.abs(next.y - corner.y);
    const cornerRadius = Math.min(radius, incoming / 2, outgoing / 2);
    const before = {
      x: corner.x + Math.sign(previous.x - corner.x) * cornerRadius,
      y: corner.y + Math.sign(previous.y - corner.y) * cornerRadius,
    };
    const after = {
      x: corner.x + Math.sign(next.x - corner.x) * cornerRadius,
      y: corner.y + Math.sign(next.y - corner.y) * cornerRadius,
    };
    lineTo(before, cursor);
    commands.push(`Q ${corner.x} ${corner.y} ${after.x} ${after.y}`);
    cursor = after;
  }
  lineTo(compact[compact.length - 1], cursor);
  return commands.join(' ');
};

const makeActionConnectorPath = (source, target, busX, sourceY = actionFlowPortY(source), targetY = actionFlowPortY(target)) => {
  const x1 = source.x + source.width;
  const y1 = sourceY;
  const x2 = target.x;
  const y2 = targetY;
  if (Math.abs(y2 - y1) < 4) return `M ${x1} ${y1} H ${x2}`;
  const bus = Math.max(x1 + 10, Math.min(x2 - 10, Number.isFinite(busX) ? busX : x1 + (x2 - x1) / 2));
  return makeRoundedOrthogonalPath([
    { x: x1, y: y1 },
    { x: bus, y: y1 },
    { x: bus, y: y2 },
    { x: x2, y: y2 },
  ]);
};

const makeActionStageBranchPath = (sourceX, sourceY, targetX, targetY, busX) => {
  if (Math.abs(targetY - sourceY) < 4) return `M ${sourceX} ${sourceY} H ${targetX}`;
  const dir = Math.sign(targetY - sourceY) || 1;
  const availableLeft = Math.max(8, busX - sourceX);
  const availableRight = Math.max(8, targetX - busX);
  const curve = Math.max(8, Math.min(18, availableLeft, availableRight));
  const handle = curve * 0.5522847498;
  const startCurveX = busX - curve;
  const startCurveY = sourceY + dir * curve;
  const endCurveY = targetY - dir * curve;
  return [
    `M ${sourceX} ${sourceY}`,
    `H ${startCurveX}`,
    `C ${busX - curve + handle} ${sourceY} ${busX} ${sourceY + dir * (curve - handle)} ${busX} ${startCurveY}`,
    `V ${endCurveY}`,
    `C ${busX} ${targetY - dir * (curve - handle)} ${busX + curve - handle} ${targetY} ${busX + curve} ${targetY}`,
    `H ${targetX}`,
  ].join(' ');
};

const actionFlowLateSplitBusX = (sourceX, targetX) => Math.max(sourceX + 12, targetX - ACTION_FLOW_BRANCH_INSET);

const findClearActionLane = (rects, preferredY) => {
  const clearance = 4;
  const intervals = rects
    .map((rect) => ({ start: rect.y - clearance, end: rect.y + rect.height + clearance }))
    .sort((a, b) => a.start - b.start)
    .reduce((merged, interval) => {
      const previous = merged[merged.length - 1];
      if (!previous || interval.start > previous.end) merged.push({ ...interval });
      else previous.end = Math.max(previous.end, interval.end);
      return merged;
    }, []);
  if (intervals.length === 0) return preferredY;
  const candidates = [Math.max(8, intervals[0].start - 10), intervals[intervals.length - 1].end + 10];
  for (let index = 0; index < intervals.length - 1; index += 1) {
    const start = intervals[index].end;
    const end = intervals[index + 1].start;
    if (end - start >= 8) candidates.push(start + (end - start) / 2);
  }
  return candidates.sort((a, b) => Math.abs(a - preferredY) - Math.abs(b - preferredY))[0];
};

const compactActionStageUpwards = (stage, visualParents, nodeRects) => {
  const firstNode = stage?.nodes?.[0];
  if (!firstNode?.rect) return;
  const parentPorts = [...(visualParents.get(firstNode.id) || [])]
    .map((parentId) => nodeRects.get(parentId))
    .filter(Boolean)
    .map(actionFlowPortY)
    .sort((a, b) => a - b);
  if (parentPorts.length === 0) return;
  const currentTop = firstNode.rect.y;
  const currentPort = actionFlowPortY(firstNode.rect);
  const maxShift = currentTop - ACTION_FLOW_PADDING_Y;
  if (maxShift <= 0) return;
  const desiredPort = Math.min(...parentPorts);
  const shift = Math.min(maxShift, Math.max(0, currentPort - desiredPort));
  if (shift < 2) return;
  stage.nodes.forEach((item) => {
    item.rect.y -= shift;
  });
};

const compactActionStages = (stages, visualParents, nodeRects, startStageIndex = 1) => {
  stages.slice(startStageIndex).forEach((stage) => {
    compactActionStageUpwards(stage, visualParents, nodeRects);
  });
};

const shiftActionStage = (stage, delta) => {
  if (!stage?.nodes?.length || !Number.isFinite(delta) || Math.abs(delta) < 2) return 0;
  let applied = delta;
  if (applied < 0) {
    const maxShiftUp = Math.max(0, stage.nodes[0].rect.y - ACTION_FLOW_PADDING_Y);
    applied = -Math.min(-applied, maxShiftUp);
  }
  if (Math.abs(applied) < 2) return 0;
  stage.nodes.forEach((item) => {
    item.rect.y += applied;
  });
  const lastRect = stage.nodes[stage.nodes.length - 1]?.rect;
  if (lastRect) {
    stage.height = Math.max(stage.height, lastRect.y + lastRect.height - ACTION_FLOW_PADDING_Y);
  }
  return applied;
};

const resolveActionStageDesiredPorts = (nodes, visualParents, nodeRects, fallbackPort) => {
  const parentIdsByIndex = nodes.map((item) => [...(visualParents.get(item.id) || [])]
    .filter((parentId) => nodeRects.has(parentId))
    .sort());
  const parentPortsByIndex = parentIdsByIndex.map((parentIds) => parentIds
    .map((parentId) => nodeRects.get(parentId))
    .filter(Boolean)
    .map(actionFlowPortY)
    .sort((a, b) => a - b));

  const desiredPorts = nodes.map((item, index) => {
    const parentPorts = parentPortsByIndex[index];
    if (parentPorts.length === 0) return fallbackPort;
    if (parentPorts.length === 1) return parentPorts[0];
    return parentPorts.reduce((sum, value) => sum + value, 0) / parentPorts.length;
  });

  const alignmentGroups = new Map();
  nodes.forEach((item, index) => {
    const parentIds = parentIdsByIndex[index];
    if (parentIds.length === 0) return;
    const key = parentIds.join('|');
    if (!alignmentGroups.has(key)) alignmentGroups.set(key, { parentIds, entries: [] });
    alignmentGroups.get(key).entries.push({ item, index });
  });

  alignmentGroups.forEach(({ parentIds, entries }) => {
    if (entries.length <= 1) return;
    const contiguous = entries.every((entry, index) => index === 0 || entry.index === entries[index - 1].index + 1);
    if (!contiguous) return;
    const parentPorts = parentIds
      .map((parentId) => nodeRects.get(parentId))
      .filter(Boolean)
      .map(actionFlowPortY)
      .sort((a, b) => a - b);
    if (parentPorts.length === 0) return;

    if (parentIds.length === 1) {
      const parentPort = parentPorts[0];
      const pivotIndex = Math.floor((entries.length - 1) / 2);
      const pivotEntry = entries[pivotIndex];
      const pivotOffset = actionFlowPortY({ y: 0, height: pivotEntry.item.height });
      const pivotTop = parentPort - pivotOffset;
      desiredPorts[pivotEntry.index] = parentPort;

      let currentTop = pivotTop;
      for (let index = pivotIndex - 1; index >= 0; index -= 1) {
        const entry = entries[index];
        currentTop -= ACTION_FLOW_ROW_GAP + entry.item.height;
        const portOffset = actionFlowPortY({ y: 0, height: entry.item.height });
        desiredPorts[entry.index] = currentTop + portOffset;
      }

      let currentBottom = pivotTop + pivotEntry.item.height;
      for (let index = pivotIndex + 1; index < entries.length; index += 1) {
        const entry = entries[index];
        const top = currentBottom + ACTION_FLOW_ROW_GAP;
        const portOffset = actionFlowPortY({ y: 0, height: entry.item.height });
        desiredPorts[entry.index] = top + portOffset;
        currentBottom = top + entry.item.height;
      }
      return;
    }

    let currentBottom = Math.min(...parentPorts) - actionFlowPortY({ y: 0, height: entries[0].item.height }) + entries[0].item.height;
    entries.forEach((entry, index) => {
      const top = index === 0
        ? currentBottom - entry.item.height
        : currentBottom + ACTION_FLOW_ROW_GAP;
      const portOffset = actionFlowPortY({ y: 0, height: entry.item.height });
      desiredPorts[entry.index] = top + portOffset;
      currentBottom = top + entry.item.height;
    });
  });

  return desiredPorts;
};

const shiftActionStageUp = (stage, amount) => {
  if (!stage?.nodes?.length || !Number.isFinite(amount) || amount < 2) return 0;
  const maxShift = Math.max(0, stage.nodes[0].rect.y - ACTION_FLOW_PADDING_Y);
  const applied = Math.min(maxShift, amount);
  if (applied < 2) return 0;
  stage.nodes.forEach((item) => {
    item.rect.y -= applied;
  });
  const lastRect = stage.nodes[stage.nodes.length - 1]?.rect;
  if (lastRect) {
    stage.height = Math.max(stage.height, lastRect.y + lastRect.height - ACTION_FLOW_PADDING_Y);
  }
  return applied;
};

const shiftActionStageTail = (stage, startIndex, amount) => {
  if (!stage?.nodes?.length || !Number.isFinite(amount) || amount < 2) return 0;
  if (!Number.isInteger(startIndex) || startIndex < 0 || startIndex >= stage.nodes.length) return 0;
  stage.nodes.slice(startIndex).forEach((item) => {
    item.rect.y += amount;
  });
  const lastRect = stage.nodes[stage.nodes.length - 1]?.rect;
  if (lastRect) {
    stage.height = Math.max(stage.height, lastRect.y + lastRect.height - ACTION_FLOW_PADDING_Y);
  }
  return amount;
};

const clearActionLaneInStage = (stage, laneY, clearance = 6) => {
  if (!stage?.nodes?.length || !Number.isFinite(laneY)) return 0;
  const blockers = stage.nodes
    .map((item, index) => ({ rect: item.rect, index }))
    .filter(({ rect }) => laneY >= rect.y - clearance && laneY <= rect.y + rect.height + clearance);
  if (blockers.length === 0) return 0;
  const requiredShift = Math.max(...blockers.map(({ rect }) => rect.y + rect.height + clearance - laneY));
  const shiftedUp = shiftActionStageUp(stage, requiredShift);
  if (shiftedUp > 0) return shiftedUp;
  const startIndex = Math.min(...blockers.map(({ index }) => index));
  return shiftActionStageTail(stage, startIndex, requiredShift);
};

const straightenActionConnectorStages = (candidateEdges, stages) => {
  candidateEdges
    .filter((edge) => edge.toStage - edge.fromStage > 1)
    .sort((left, right) => {
      const leftPriority = Number(Boolean(left.fanOut || left.fanIn));
      const rightPriority = Number(Boolean(right.fanOut || right.fanIn));
      if (leftPriority !== rightPriority) return rightPriority - leftPriority;
      const leftDelta = Math.abs(actionFlowPortY(left.sourceRect) - actionFlowPortY(left.targetRect));
      const rightDelta = Math.abs(actionFlowPortY(right.sourceRect) - actionFlowPortY(right.targetRect));
      if (leftDelta !== rightDelta) return leftDelta - rightDelta;
      return (right.toStage - right.fromStage) - (left.toStage - left.fromStage);
    })
    .forEach((edge) => {
      const sourceY = actionFlowPortY(edge.sourceRect);
      const targetY = actionFlowPortY(edge.targetRect);
      if (!(edge.fanOut || edge.fanIn) && Math.abs(sourceY - targetY) > 24) return;
      const preferredLaneY = sourceY;
      for (let stageIndex = edge.fromStage + 1; stageIndex < edge.toStage; stageIndex += 1) {
        clearActionLaneInStage(stages[stageIndex], preferredLaneY);
      }
    });
};

const alignAdjacentFanoutStages = (stages, visualChildren, visualStageById, nodeRects) => {
  visualChildren.forEach((targetIds, sourceId) => {
    if (targetIds.size <= 1) return;
    const sourceStageIndex = visualStageById.get(sourceId);
    if (!Number.isFinite(sourceStageIndex)) return;
    const nextStageIndex = sourceStageIndex + 1;
    const adjacentTargetIds = [...targetIds].filter((targetId) => visualStageById.get(targetId) === nextStageIndex);
    if (adjacentTargetIds.length <= 1) return;
    const stage = stages[nextStageIndex];
    const sourceRect = nodeRects.get(sourceId);
    if (!stage || !sourceRect || stage.nodes.length !== adjacentTargetIds.length) return;
    const sourceY = actionFlowPortY(sourceRect);
    const primaryNode = [...stage.nodes]
      .filter((item) => adjacentTargetIds.includes(item.id))
      .sort((left, right) => (
        Math.abs(actionFlowPortY(left.rect) - sourceY) - Math.abs(actionFlowPortY(right.rect) - sourceY)
      ))[0];
    if (!primaryNode) return;
    shiftActionStage(stage, sourceY - actionFlowPortY(primaryNode.rect));
  });
};

const resolveActionConnectorBusX = (edge, stages) => {
  const sourceX = edge.sourceRect.x + edge.sourceRect.width;
  const targetX = edge.targetRect.x;
  const isAdjacent = edge.toStage - edge.fromStage <= 1;

  if (isAdjacent) {
    if (edge.fanOut && !edge.fanIn) {
      return sourceX + Math.min(ACTION_FLOW_BRANCH_INSET, (targetX - sourceX) * 0.45);
    }
    if (edge.fanIn && !edge.fanOut) {
      return targetX - Math.min(ACTION_FLOW_BRANCH_INSET, (targetX - sourceX) * 0.45);
    }
    return sourceX + (targetX - sourceX) / 2;
  }

  const intermediateRects = stages
    .slice(edge.fromStage + 1, edge.toStage)
    .flatMap((stage) => stage.nodes.map((item) => item.rect));
  const crossesCard = (y) => intermediateRects.some((rect) => y >= rect.y - 4 && y <= rect.y + rect.height + 4);
  const canLateSplit = !crossesCard(edge.sourceY);
  if ((edge.fanOut || edge.fanIn || Math.abs(edge.sourceY - edge.targetY) <= 24) && canLateSplit) {
    return actionFlowLateSplitBusX(sourceX, targetX);
  }
  return targetX - ACTION_FLOW_STAGE_GAP / 2;
};

const routeActionConnector = (edge, stages, busXOverride = null) => {
  const sourceX = edge.sourceRect.x + edge.sourceRect.width;
  const targetX = edge.targetRect.x;
  const sourceBusX = sourceX + ACTION_FLOW_STAGE_GAP / 2;
  const targetBusX = targetX - ACTION_FLOW_STAGE_GAP / 2;
  if (edge.toStage - edge.fromStage <= 1) {
    if (Math.abs(edge.sourceY - edge.targetY) < 4) {
      return { path: `M ${sourceX} ${edge.sourceY} H ${targetX}`, maxY: Math.max(edge.sourceY, edge.targetY) };
    }
    const busX = Number.isFinite(busXOverride) ? busXOverride : actionFlowLateSplitBusX(sourceX, targetX);
    return {
      path: makeActionStageBranchPath(sourceX, edge.sourceY, targetX, edge.targetY, busX),
      maxY: Math.max(edge.sourceY, edge.targetY),
    };
  }

  const intermediateRects = stages
    .slice(edge.fromStage + 1, edge.toStage)
    .flatMap((stage) => stage.nodes.map((item) => item.rect));
  const crossesCard = (y) => intermediateRects.some((rect) => y >= rect.y - 4 && y <= rect.y + rect.height + 4);
  const canLateSplit = !crossesCard(edge.sourceY);
  if ((edge.fanOut || edge.fanIn || Math.abs(edge.sourceY - edge.targetY) <= 24) && canLateSplit) {
    const busX = Number.isFinite(busXOverride) ? busXOverride : actionFlowLateSplitBusX(sourceX, targetX);
    return {
      path: makeActionStageBranchPath(sourceX, edge.sourceY, targetX, edge.targetY, busX),
      maxY: Math.max(edge.sourceY, edge.targetY),
    };
  }

  if (edge.fanOut) {
    if (!crossesCard(edge.sourceY)) {
      return { path: makeActionConnectorPath(edge.sourceRect, edge.targetRect, targetBusX, edge.sourceY, edge.targetY), maxY: Math.max(edge.sourceY, edge.targetY) };
    }
    const laneY = findClearActionLane(intermediateRects, edge.sourceY);
    return {
      path: makeRoundedOrthogonalPath([
        { x: sourceX, y: edge.sourceY },
        { x: sourceBusX, y: edge.sourceY },
        { x: sourceBusX, y: laneY },
        { x: targetBusX, y: laneY },
        { x: targetBusX, y: edge.targetY },
        { x: targetX, y: edge.targetY },
      ]),
      maxY: Math.max(edge.sourceY, edge.targetY, laneY),
    };
  }
  if (!crossesCard(edge.sourceY)) {
    return { path: makeActionConnectorPath(edge.sourceRect, edge.targetRect, targetBusX, edge.sourceY, edge.targetY), maxY: Math.max(edge.sourceY, edge.targetY) };
  }
  if (!crossesCard(edge.targetY)) {
    return { path: makeActionConnectorPath(edge.sourceRect, edge.targetRect, sourceBusX, edge.sourceY, edge.targetY), maxY: Math.max(edge.sourceY, edge.targetY) };
  }

  const laneY = findClearActionLane(intermediateRects, (edge.sourceY + edge.targetY) / 2);
  return {
    path: makeRoundedOrthogonalPath([
      { x: sourceX, y: edge.sourceY },
      { x: sourceBusX, y: edge.sourceY },
      { x: sourceBusX, y: laneY },
      { x: targetBusX, y: laneY },
      { x: targetBusX, y: edge.targetY },
      { x: targetX, y: edge.targetY },
    ]),
    maxY: Math.max(edge.sourceY, edge.targetY, laneY),
  };
};

const parseTimestamp = (value) => {
  const time = new Date(value).getTime();
  return Number.isFinite(time) ? time : Date.now();
};

const parseJSON = (value, fallback = {}) => {
  if (!value) return fallback;
  if (typeof value === 'object') return value;
  try {
    return JSON.parse(value);
  } catch {
    return fallback;
  }
};

export {
  statusTone,
  statusLabel,
  tokenTestStatusLabel,
  formatNumber,
  formatDateTime,
  formatResetCountdown,
  formatActionDuration,
  actionFlowStatusDotClass,
  actionFlowStatusMetaClass,
  workflowJobStep,
  workflowJobPriority,
  summarizeWorkflowJobs,
  flattenWorkflowDefinitions,
  buildWorkflowGraph,
  workflowGroupName,
  workflowDependentsById,
  workflowDescendantCounts,
  workflowOfficialOrderHint,
  sortWorkflowGroups,
  groupWorkflowLayer,
  summarizeWorkflowGroup,
  actionFlowExpandedVariantsHeight,
  actionFlowNodeHeight,
  actionFlowPortY,
  workflowReachable,
  reduceWorkflowEdges,
  workflowTrace,
  makeRoundedOrthogonalPath,
  makeActionConnectorPath,
  makeActionStageBranchPath,
  actionFlowLateSplitBusX,
  findClearActionLane,
  compactActionStageUpwards,
  compactActionStages,
  shiftActionStage,
  resolveActionStageDesiredPorts,
  shiftActionStageUp,
  shiftActionStageTail,
  clearActionLaneInStage,
  straightenActionConnectorStages,
  alignAdjacentFanoutStages,
  resolveActionConnectorBusX,
  routeActionConnector,
  parseTimestamp,
  parseJSON,
};