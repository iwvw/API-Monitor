import React, { useCallback, useEffect, useMemo, useRef, useState } from 'react';
import { Badge } from '@cloudflare/kumo/components/badge';
import { Button } from '@cloudflare/kumo/components/button';
import { Loader } from '@cloudflare/kumo/components/loader';
import { SkeletonLine } from '@cloudflare/kumo/components/loader';
import PublicPageIconPicker from '../components/public/PublicPageIconPicker.jsx';
import { useCloudflareSpotlight } from '../hooks/useCloudflareSpotlight.js';
import { useDraggableScroll } from '../hooks/useDraggableScroll.js';
import PublicOverviewStats from '../components/public/PublicOverviewStats.jsx';
import { formatGitHubRepositoryDescription } from '../modules/githubEmoji.js';
import { normalizeWorkflowJobName, workflowJobMatchesDefinition } from '../modules/githubWorkflowJobs.js';
import {
  getPublicGithubDataUpdatedAt,
  getPublicGithubRefreshInterval,
  hasPublicGithubWorkflowDetail,
  mergePublicGithubRepositories,
  shouldLoadPublicGithubRepositoryDetail,
} from '../modules/githubPublicRealtime.js';
import {
  getPublicPageFaviconHref,
  swapPublicPageFavicon,
  withPublicPageIconId,
} from '../modules/publicPageBranding.js';
import { toast } from '../modules/toast.js';
import { useNowTick } from '../modules/usePageVisibility.js';
import useStore from '../store.js';
import {
  AlertTriangle,
  ExternalLink,
  GitBranch,
  GitHubBrand,
  Globe,
  Home,
  LogIn,
  RefreshCw,
  Shield,
} from '../components/Icons.jsx';

const normalizePublicPath = () => {
  const path = window.location.pathname.replace(/\/+$/, '');
  const match = path.match(/^\/(?:github|gh)\/([^/]+)$/);
  return match ? decodeURIComponent(match[1]) : '';
};

const parsePublicGithubStreamPayload = (event) => {
  if (!event?.data) return {};
  try {
    return JSON.parse(event.data);
  } catch {
    return {};
  }
};

const statusPanelClass = {
  success: 'border-kumo-success/45 bg-kumo-base text-kumo-success',
  danger: 'border-kumo-danger/45 bg-kumo-base text-kumo-danger',
  warning: 'border-kumo-warning/45 bg-kumo-base text-kumo-warning',
  neutral: 'border-kumo-interact/80 bg-kumo-base text-kumo-strong',
};

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

const formatDateTime = (value) => {
  if (!value) return '尚未更新';
  const date = new Date(value);
  if (Number.isNaN(date.getTime())) return String(value);
  return date.toLocaleString('zh-CN', {
    month: '2-digit',
    day: '2-digit',
    hour: '2-digit',
    minute: '2-digit',
  });
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

const formatNumber = (value) => Number(value || 0).toLocaleString('en-US', { useGrouping: false });

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

const ACTION_FLOW_CARD_WIDTH = 260;
const ACTION_FLOW_STAGE_GAP = 72;
const ACTION_FLOW_PADDING_X = 28;
const ACTION_FLOW_PADDING_Y = 28;
const ACTION_FLOW_ROW_GAP = 38;
const ACTION_FLOW_VIEWPORT_HEIGHT = 320;
const ACTION_FLOW_MIN_VIEWPORT_HEIGHT = 112;
const ACTION_FLOW_MIN_SCALE = 0.72;
const ACTION_FLOW_BRANCH_INSET = 28;
const PUBLIC_ACTION_PANEL_INSET_Y = 26;

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
  const availableVertical = Math.max(8, Math.abs(targetY - sourceY) / 2);
  const curve = Math.max(8, Math.min(24, availableLeft, availableRight, availableVertical));
  const handle = Math.max(4, curve / 2);
  const startCurveX = busX - curve;
  const startCurveY = sourceY + dir * curve;
  const endCurveY = targetY - dir * curve;
  return [
    `M ${sourceX} ${sourceY}`,
    `H ${startCurveX}`,
    `C ${busX - handle} ${sourceY} ${busX} ${sourceY + dir * handle} ${busX} ${startCurveY}`,
    `V ${endCurveY}`,
    `C ${busX} ${targetY - dir * handle} ${busX + handle} ${targetY} ${busX + curve} ${targetY}`,
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

const routeActionConnector = (edge, stages) => {
  const sourceX = edge.sourceRect.x + edge.sourceRect.width;
  const targetX = edge.targetRect.x;
  const sourceBusX = sourceX + ACTION_FLOW_STAGE_GAP / 2;
  const targetBusX = targetX - ACTION_FLOW_STAGE_GAP / 2;
  if (edge.toStage - edge.fromStage <= 1) {
    if (Math.abs(edge.sourceY - edge.targetY) < 4) {
      return { path: `M ${sourceX} ${edge.sourceY} H ${targetX}`, maxY: Math.max(edge.sourceY, edge.targetY) };
    }
    const busX = actionFlowLateSplitBusX(sourceX, targetX);
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
    const busX = actionFlowLateSplitBusX(sourceX, targetX);
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

const buildActionCanvasLayout = (workflow, jobs, now, focusedDefinitionIds = null, focusMode = 'neighbors') => {
  const graph = buildWorkflowGraph(workflow, jobs, now);
  const definitionById = new Map(graph.definitions.map((definition) => [definition.id, definition]));
  const used = new Set();
  let layers = [];
  if (graph.fallback) {
    layers = graph.definitions.map((definition) => [definition]);
    graph.definitions.forEach((definition) => used.add(definition.id));
  } else if (Array.isArray(workflow?.layers) && workflow.layers.length > 0) {
    layers = workflow.layers
      .map((layer) => layer.filter((definition) => definitionById.has(definition.id)))
      .filter((layer) => layer.length > 0);
    layers.flat().forEach((definition) => used.add(definition.id));
  }
  graph.definitions.filter((definition) => !used.has(definition.id)).forEach((definition) => {
    const needs = (definition.needs || []).filter((need) => definitionById.has(need));
    let index = 0;
    needs.forEach((need) => {
      const parentIndex = layers.findIndex((layer) => layer.some((item) => item.id === need));
      if (parentIndex >= 0) index = Math.max(index, parentIndex + 1);
    });
    while (layers.length <= index) layers.push([]);
    layers[index].push(definition);
  });
  if (layers.length === 0) {
    layers = jobs.map((job) => [{ id: String(job.id), name: job.name, needs: [] }]);
  }

  const dependents = workflowDependentsById(graph.definitions);
  const descendantCounts = workflowDescendantCounts(graph.definitions, dependents);
  const orderById = new Map(graph.definitions.map((definition, index) => [definition.id, index]));
  const groupedLayers = layers.map((layer) => sortWorkflowGroups(groupWorkflowLayer(layer, dependents), orderById, dependents, descendantCounts));
  const visualIdByDefinition = new Map();
  const visualStageById = new Map();
  groupedLayers.forEach((layer) => {
    layer.forEach((definitions) => {
      const id = definitions.length === 1 ? definitions[0].id : `group-${definitions.map((definition) => definition.id).join('-')}`;
      definitions.forEach((definition) => visualIdByDefinition.set(definition.id, id));
    });
  });
  groupedLayers.forEach((layer, stageIndex) => {
    layer.forEach((definitions) => {
      const id = definitions.length === 1 ? definitions[0].id : `group-${definitions.map((definition) => definition.id).join('-')}`;
      visualStageById.set(id, stageIndex);
    });
  });
  const reducedDefinitionEdges = reduceWorkflowEdges(graph.definitions, dependents);
  const trace = workflowTrace(focusedDefinitionIds, reducedDefinitionEdges, focusMode);
  const highlightedVisualIds = trace
    ? new Set([...trace.nodes].map((id) => visualIdByDefinition.get(id)).filter(Boolean))
    : null;

  const stageItems = groupedLayers.map((layer) => layer.map((definitions) => {
    const id = definitions.length === 1 ? definitions[0].id : `group-${definitions.map((definition) => definition.id).join('-')}`;
    const node = summarizeWorkflowGroup(definitions, graph.summaries);
    const highlighted = highlightedVisualIds ? highlightedVisualIds.has(id) : null;
    return { id, definitions, node, highlighted, height: actionFlowNodeHeight(node, false) };
  }));
  const visualParents = new Map();
  const visualChildren = new Map();
  reducedDefinitionEdges.forEach((edge) => {
    const from = visualIdByDefinition.get(edge.from);
    const to = visualIdByDefinition.get(edge.to);
    if (!from || !to || from === to) return;
    if (!visualParents.has(to)) visualParents.set(to, new Set());
    if (!visualChildren.has(from)) visualChildren.set(from, new Set());
    visualParents.get(to).add(from);
    visualChildren.get(from).add(to);
  });
  const primaryLaneY = ACTION_FLOW_PADDING_Y + Math.max(0, ...stageItems.map((nodes) => (
    nodes[0] ? actionFlowPortY({ y: 0, height: nodes[0].height }) : 0
  )));
  const nodeRects = new Map();
  const stages = stageItems.map((nodes, stageIndex) => {
    const x = ACTION_FLOW_PADDING_X + stageIndex * (ACTION_FLOW_CARD_WIDTH + ACTION_FLOW_STAGE_GAP);
    const availableHeight = ACTION_FLOW_VIEWPORT_HEIGHT - ACTION_FLOW_PADDING_Y * 2;
    const desiredPorts = resolveActionStageDesiredPorts(nodes, visualParents, nodeRects, primaryLaneY);
    let previousBottom = ACTION_FLOW_PADDING_Y - ACTION_FLOW_ROW_GAP;
    const resolvedNodes = nodes.map((item, index) => {
      const portOffset = actionFlowPortY({ y: 0, height: item.height });
      const desiredPort = desiredPorts[index] ?? primaryLaneY;
      const alignedY = desiredPort - portOffset;
      const minimumY = previousBottom + ACTION_FLOW_ROW_GAP;
      const y = Math.max(ACTION_FLOW_PADDING_Y, minimumY, alignedY);
      const rect = { x, y, width: ACTION_FLOW_CARD_WIDTH, height: item.height };
      nodeRects.set(item.id, rect);
      previousBottom = y + item.height;
      return { ...item, rect };
    });
    const contentHeight = Math.max(0, previousBottom - ACTION_FLOW_PADDING_Y);
    return {
      stageIndex,
      nodes: resolvedNodes,
      height: Math.max(contentHeight, availableHeight),
    };
  });
  compactActionStages(stages, visualParents, nodeRects);
  alignAdjacentFanoutStages(stages, visualChildren, visualStageById, nodeRects);
  visualChildren.forEach((targetIds, sourceId) => {
    if (targetIds.size <= 1) return;
    const sourceStageIndex = visualStageById.get(sourceId);
    const targetStageIndexes = [...targetIds]
      .map((targetId) => visualStageById.get(targetId))
      .filter((stageIndex) => Number.isFinite(stageIndex) && stageIndex > sourceStageIndex);
    if (!Number.isFinite(sourceStageIndex) || targetStageIndexes.length <= 1) return;
    const firstTargetStage = Math.min(...targetStageIndexes);
    if (firstTargetStage - sourceStageIndex <= 1) return;
    const intermediateRects = stages
      .slice(sourceStageIndex + 1, firstTargetStage)
      .flatMap((stage) => stage.nodes.map((item) => item.rect));
    const sourceRect = nodeRects.get(sourceId);
    const sourceStage = stages[sourceStageIndex];
    const sourceNodeIndex = sourceStage?.nodes.findIndex((item) => item.id === sourceId) ?? -1;
    if (!sourceRect || !sourceStage || sourceNodeIndex < 0 || intermediateRects.length === 0) return;
    const sourceY = actionFlowPortY(sourceRect);
    const laneY = findClearActionLane(intermediateRects, sourceY);
    const delta = laneY - sourceY;
    if (Math.abs(delta) < 2) return;
    if (delta > 0) {
      sourceStage.nodes.slice(sourceNodeIndex).forEach((item) => {
        item.rect.y += delta;
      });
    } else {
      const movable = Math.min(-delta, Math.max(0, sourceStage.nodes[0].rect.y - ACTION_FLOW_PADDING_Y));
      sourceStage.nodes.slice(0, sourceNodeIndex + 1).forEach((item) => {
        item.rect.y -= movable;
      });
    }
    const lastRect = sourceStage.nodes[sourceStage.nodes.length - 1]?.rect;
    if (lastRect) sourceStage.height = Math.max(sourceStage.height, lastRect.y + lastRect.height - ACTION_FLOW_PADDING_Y);
  });
  compactActionStages(stages, visualParents, nodeRects);

  const candidateEdges = [];
  const seenEdges = new Map();
  const visibleDefinitionEdges = reducedDefinitionEdges.map((edge) => ({ ...edge }));

  visibleDefinitionEdges.forEach((definitionEdge) => {
    const from = visualIdByDefinition.get(definitionEdge.from);
    const to = visualIdByDefinition.get(definitionEdge.to);
    if (!from || !to || from === to || !nodeRects.has(from) || !nodeRects.has(to)) return;
    const fromStage = visualStageById.get(from);
    const toStage = visualStageById.get(to);
    if (!Number.isFinite(fromStage) || !Number.isFinite(toStage) || toStage <= fromStage) return;
    const edgeTrace = trace?.edges.get(`${definitionEdge.from}->${definitionEdge.to}`);
    const focusedPath = Boolean(edgeTrace);
    const key = `${from}->${to}`;
    const existing = seenEdges.get(key);
    if (existing) {
      existing.highlighted = existing.highlighted || focusedPath;
      return;
    }
    const sourceRect = nodeRects.get(from);
    const targetRect = nodeRects.get(to);
    const sourceStage = stages[fromStage];
    const targetStage = stages[toStage];
    const sourceNode = sourceStage?.nodes.find((item) => item.id === from);
    const targetNode = targetStage?.nodes.find((item) => item.id === to);
    candidateEdges.push({
      from,
      to,
      fromStage,
      toStage,
      sourceRect,
      targetRect,
      sourceNode,
      targetNode,
      fromDefinition: definitionEdge.from,
      toDefinition: definitionEdge.to,
      highlighted: focusedPath,
    });
    seenEdges.set(key, candidateEdges[candidateEdges.length - 1]);
  });
  const outgoing = new Map();
  const incoming = new Map();
  candidateEdges.forEach((edge) => {
    if (!outgoing.has(edge.from)) outgoing.set(edge.from, []);
    if (!incoming.has(edge.to)) incoming.set(edge.to, []);
    outgoing.get(edge.from).push(edge);
    incoming.get(edge.to).push(edge);
  });
  candidateEdges.forEach((edge) => {
    edge.fanOut = (outgoing.get(edge.from)?.length || 0) > 1;
    edge.fanIn = (incoming.get(edge.to)?.length || 0) > 1;
  });
  straightenActionConnectorStages(candidateEdges, stages);
  const rightAnchors = new Map();
  const leftAnchors = new Map();
  nodeRects.forEach((rect, id) => {
    rightAnchors.set(id, actionFlowPortY(rect));
    leftAnchors.set(id, actionFlowPortY(rect));
  });
  const canPlaceAnchor = (rect, y) => y >= rect.y + 4 && y <= rect.y + rect.height - 4;
  incoming.forEach((items, id) => {
    const rect = nodeRects.get(id);
    const current = leftAnchors.get(id);
    const candidates = items
      .map((edge) => rightAnchors.get(edge.from))
      .filter((y) => Number.isFinite(y) && canPlaceAnchor(rect, y) && Math.abs(y - current) <= 28)
      .sort((a, b) => Math.abs(a - current) - Math.abs(b - current));
    if (candidates.length > 0) leftAnchors.set(id, candidates[0]);
  });
  outgoing.forEach((items, id) => {
    const rect = nodeRects.get(id);
    const current = rightAnchors.get(id);
    const candidates = items
      .map((edge) => leftAnchors.get(edge.to))
      .filter((y) => Number.isFinite(y) && canPlaceAnchor(rect, y) && Math.abs(y - current) <= 28)
      .sort((a, b) => Math.abs(a - current) - Math.abs(b - current));
    if (candidates.length > 0) rightAnchors.set(id, candidates[0]);
  });
  outgoing.forEach((items, id) => {
    if (items.length <= 1) return;
    const sourceY = rightAnchors.get(id);
    const primaryEdge = [...items].sort((left, right) => (
      (left.toStage - left.fromStage) - (right.toStage - right.fromStage)
      || Math.abs(actionFlowPortY(left.targetRect) - sourceY) - Math.abs(actionFlowPortY(right.targetRect) - sourceY)
    ))[0];
    if (primaryEdge && canPlaceAnchor(primaryEdge.targetRect, sourceY)) {
      leftAnchors.set(primaryEdge.to, sourceY);
    }
  });
  incoming.forEach((items, id) => {
    if (items.length <= 1) return;
    const targetY = leftAnchors.get(id);
    const primaryEdge = [...items].sort((left, right) => (
      (left.toStage - left.fromStage) - (right.toStage - right.fromStage)
      || Math.abs(actionFlowPortY(left.sourceRect) - targetY) - Math.abs(actionFlowPortY(right.sourceRect) - targetY)
    ))[0];
    if (primaryEdge && canPlaceAnchor(primaryEdge.sourceRect, targetY)) {
      rightAnchors.set(primaryEdge.from, targetY);
    }
  });
  candidateEdges.forEach((edge) => {
    const sourceY = rightAnchors.get(edge.from);
    const targetY = leftAnchors.get(edge.to);
    if (Math.abs(targetY - sourceY) > 28) return;
    const sourceSingle = outgoing.get(edge.from)?.length === 1;
    const targetSingle = incoming.get(edge.to)?.length === 1;
    if (sourceSingle && targetSingle) {
      const overlapTop = Math.max(edge.sourceRect.y + 4, edge.targetRect.y + 4);
      const overlapBottom = Math.min(edge.sourceRect.y + edge.sourceRect.height - 4, edge.targetRect.y + edge.targetRect.height - 4);
      if (overlapTop <= overlapBottom) {
        const lane = Math.max(overlapTop, Math.min(overlapBottom, (sourceY + targetY) / 2));
        rightAnchors.set(edge.from, lane);
        leftAnchors.set(edge.to, lane);
      }
    } else if (sourceSingle && canPlaceAnchor(edge.sourceRect, targetY)) {
      rightAnchors.set(edge.from, targetY);
    } else if (targetSingle && canPlaceAnchor(edge.targetRect, sourceY)) {
      leftAnchors.set(edge.to, sourceY);
    }
  });
  candidateEdges.forEach((edge) => {
    edge.sourceY = rightAnchors.get(edge.from);
    edge.targetY = leftAnchors.get(edge.to);
  });
  const edges = candidateEdges.map((edge) => {
    const route = routeActionConnector(edge, stages);
    return {
      from: edge.from,
      to: edge.to,
      sourceX: edge.sourceRect.x + edge.sourceRect.width,
      targetX: edge.targetRect.x,
      sourceY: edge.sourceY,
      targetY: edge.targetY,
      highlighted: edge.highlighted,
      path: route.path,
      maxY: route.maxY,
    };
  });
  const width = ACTION_FLOW_PADDING_X * 2 + Math.max(1, groupedLayers.length) * ACTION_FLOW_CARD_WIDTH + Math.max(0, groupedLayers.length - 1) * ACTION_FLOW_STAGE_GAP;
  const height = Math.max(
    ACTION_FLOW_MIN_VIEWPORT_HEIGHT,
    ACTION_FLOW_PADDING_Y + Math.max(
      0,
      ...stages.flatMap((stage) => stage.nodes.map((item) => item.rect.y + item.rect.height)),
      ...edges.map((edge) => edge.maxY),
    ),
  );
  const resolvedStages = highlightedVisualIds
    ? stages.map((stage) => ({
      ...stage,
      nodes: stage.nodes.map((item) => ({ ...item, highlighted: highlightedVisualIds.has(item.id) })),
    }))
    : stages;
  return { graph, stages: resolvedStages, edges, width, height, hasFocus: Boolean(trace) };
};

function ActionStatusDot({ status, muted = false }) {
  return (
    <span
      aria-hidden="true"
      className={`mt-[0.4rem] inline-block h-3 w-3 shrink-0 rounded-full ring-1 ring-offset-1 ring-offset-kumo-base ${actionFlowStatusDotClass(status)} ${muted ? 'opacity-72' : 'opacity-100'}`}
    />
  );
}

function ActionFlowNode({
  node,
  style,
  expanded = false,
  highlighted = null,
  spotlighted = false,
  nodeRef = null,
  onToggle,
  onFocus,
  onBlur,
  onDefinitionFocus,
  onDefinitionBlur,
}) {
  const showActiveStep = Boolean(node.step && ['in_progress', 'running', 'queued', 'pending', 'waiting'].includes(normalizeWorkflowJobName(node.status)));
  const variantPreview = node.variants || [];
  const compact = !node.group && !node.matrix && !node.step && variantPreview.length <= 1;
  const active = highlighted === true;
  const muted = highlighted === false;
  const strongTextClass = muted ? 'text-kumo-subtle' : 'text-kumo-strong';
  const subtleTextClass = muted ? 'text-kumo-subtle/80' : 'text-kumo-subtle';
  const metaTextClass = actionFlowStatusMetaClass(node.status, muted);
  const completedVariants = variantPreview.filter((variant) => ['success', 'completed'].includes(normalizeWorkflowJobName(variant.status))).length;
  const matrixSummary = node.count > 0
    ? `${completedVariants || node.count}/${node.count} 个任务${completedVariants === node.count ? '已完成' : '处理中'}`
    : '矩阵任务';
  return (
    <div
      ref={nodeRef}
      className={`absolute grid min-w-0 content-start gap-1.5 overflow-visible rounded-md border bg-kumo-base px-3 py-2.5 transition-[border-color,box-shadow,opacity,filter] ${spotlighted ? 'z-40 border-kumo-brand/60 shadow-lg shadow-kumo-brand/10 ring-2 ring-kumo-brand/20' : 'z-20 shadow-sm'} ${active && !spotlighted ? 'border-kumo-brand/45 ring-1 ring-kumo-brand/20' : ''} ${!active && !spotlighted ? 'border-kumo-interact/70' : ''} ${muted ? 'opacity-[0.42] saturate-75' : 'opacity-100'}`}
      onClick={(event) => event.stopPropagation()}
      onMouseDown={(event) => event.stopPropagation()}
      onMouseEnter={onFocus}
      onMouseLeave={onBlur}
      onFocus={(event) => {
        if (event.target === event.currentTarget) onFocus?.();
      }}
      onBlur={(event) => {
        if (!event.currentTarget.contains(event.relatedTarget)) onBlur?.();
      }}
      style={style}
    >
      {node.group ? (
        <>
          {node.name && (
            <div className="flex min-w-0 items-start gap-2">
              <ActionStatusDot status={node.status} muted={muted} />
              <div className={`min-w-0 truncate text-sm font-semibold leading-6 ${strongTextClass}`} title={node.name}>{node.name}</div>
            </div>
          )}
          <div className="grid gap-2">
            {variantPreview.map((variant) => (
              <div
                key={variant.id || variant.name}
                className="flex min-w-0 items-center justify-between gap-3 py-1 text-sm leading-6"
              >
                <div className="flex min-w-0 items-start gap-2">
                  <ActionStatusDot status={variant.status} muted={muted} />
                  <span className={`min-w-0 truncate font-semibold ${strongTextClass}`} title={variant.name}>{variant.name}</span>
                </div>
                <span className={`shrink-0 whitespace-nowrap text-xs leading-6 ${actionFlowStatusMetaClass(variant.status, muted)}`} title={`${statusLabel(variant.status)} · ${variant.duration}`}>
                  {variant.duration || statusLabel(variant.status)}
                </span>
              </div>
            ))}
          </div>
        </>
      ) : (
        <>
          <div className="flex min-w-0 items-start justify-between gap-2 leading-6">
            <div className="flex min-w-0 items-start gap-2">
              <ActionStatusDot status={node.status} muted={muted} />
              <div className="min-w-0">
                <div className={`truncate text-sm font-semibold leading-6 ${strongTextClass}`} title={node.name}>{node.name}</div>
                {node.matrix && <div className={`truncate text-xs leading-6 ${subtleTextClass}`}>{matrixSummary}</div>}
              </div>
            </div>
            <span className={`shrink-0 whitespace-nowrap text-xs font-medium leading-6 ${metaTextClass}`} title={`${statusLabel(node.status)} · ${node.duration}`}>
              {node.duration || statusLabel(node.status)}
            </span>
          </div>
          {!compact && (
            <>
              {showActiveStep && (
                <div className={`truncate text-xs leading-6 ${strongTextClass}`} title={node.step.name}>当前步骤：{node.step.name}</div>
              )}
              {variantPreview.length > 1 ? (
                <Button
                  type="button"
                  size="xs"
                  variant="ghost"
                  className={`h-auto min-w-0 justify-start px-0 py-0 text-xs leading-6 ${metaTextClass}`}
                  onClick={(event) => {
                    event.stopPropagation();
                    onToggle?.();
                  }}
                >
                  {expanded ? '收起 Jobs' : '查看全部 Jobs'}
                </Button>
              ) : (
                <div className={`truncate text-xs leading-6 ${subtleTextClass}`}>{node.completedAt ? '已完成' : '等待运行'}</div>
              )}
              {expanded && variantPreview.length > 1 && (
                <div className="grid gap-2 border-t border-kumo-line pt-3">
                  {variantPreview.map((variant) => (
                    <div
                      key={variant.id || variant.name}
                      className="flex min-w-0 items-center justify-between gap-2 py-1 text-xs leading-6"
                      onMouseEnter={() => onDefinitionFocus?.(variant.definitionId || variant.id)}
                      onMouseLeave={onDefinitionBlur}
                      onFocus={(event) => {
                        event.stopPropagation();
                        onDefinitionFocus?.(variant.definitionId || variant.id);
                      }}
                      onBlur={() => {
                        onDefinitionBlur?.();
                      }}
                      tabIndex={0}
                    >
                      <div className="flex min-w-0 items-start gap-2">
                        <ActionStatusDot status={variant.status} muted={muted} />
                        <span className={`min-w-0 truncate ${strongTextClass}`} title={variant.name}>{variant.name}</span>
                      </div>
                      <span className={`shrink-0 whitespace-nowrap text-[11px] leading-6 ${actionFlowStatusMetaClass(variant.status, muted)}`} title={`${statusLabel(variant.status)} · ${variant.duration}`}>
                        {variant.duration || statusLabel(variant.status)}
                      </span>
                    </div>
                  ))}
                </div>
              )}
              <div className={`truncate text-xs leading-6 ${metaTextClass}`}>{node.completedAt ? `完成于 ${formatDateTime(node.completedAt)}` : '实时更新中'}</div>
            </>
          )}
        </>
      )}
    </div>
  );
}

function ActionWorkflowCanvas({ workflow, jobs, now }) {
  const viewportRef = useRef(null);
  const expandedNodeRef = useRef(null);
  const [viewportSize, setViewportSize] = useState({ width: 0, height: ACTION_FLOW_VIEWPORT_HEIGHT });
  const [expandedMatrixId, setExpandedMatrixId] = useState(null);
  const [focusState, setFocusState] = useState(null);
  const focusIds = expandedMatrixId ? null : focusState?.ids;
  const focusMode = expandedMatrixId ? 'neighbors' : focusState?.mode;
  const layout = useMemo(
    () => buildActionCanvasLayout(workflow, jobs, now, focusIds, focusMode),
    [focusIds, focusMode, workflow, jobs, now],
  );
  const baseLayoutSize = useMemo(() => {
    const baseLayout = buildActionCanvasLayout(workflow, jobs, now);
    return { width: baseLayout.width, height: baseLayout.height };
  }, [workflow, jobs, now]);
  const expandedNode = useMemo(() => {
    if (!expandedMatrixId) return null;
    for (const stage of layout.stages) {
      const match = stage.nodes.find((item) => item.id === expandedMatrixId || item.definitions.some((definition) => definition.id === expandedMatrixId));
      if (match) return match;
    }
    return null;
  }, [expandedMatrixId, layout.stages]);
  const expandedCanvasHeight = useMemo(() => {
    if (!expandedNode) return layout.height;
    return Math.max(
      layout.height,
      expandedNode.rect.y + actionFlowNodeHeight(expandedNode.node, true) + ACTION_FLOW_PADDING_Y + 8,
    );
  }, [expandedNode, layout.height]);
  const hasExpandedNode = Boolean(expandedNode);

  useEffect(() => {
    if (expandedMatrixId && !expandedNode) setExpandedMatrixId(null);
  }, [expandedMatrixId, expandedNode]);

  useEffect(() => {
    if (!expandedMatrixId) return undefined;
    const handlePointerDown = (event) => {
      const target = event.target;
      if (target instanceof Node && expandedNodeRef.current?.contains(target)) return;
      setExpandedMatrixId(null);
      setFocusState(null);
    };
    const handleKeyDown = (event) => {
      if (event.key === 'Escape') {
        setExpandedMatrixId(null);
        setFocusState(null);
      }
    };
    document.addEventListener('pointerdown', handlePointerDown, true);
    document.addEventListener('keydown', handleKeyDown);
    return () => {
      document.removeEventListener('pointerdown', handlePointerDown, true);
      document.removeEventListener('keydown', handleKeyDown);
    };
  }, [expandedMatrixId]);

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

  const canvasFit = useMemo(() => {
    const width = viewportSize.width || layout.width;
    const availableWidth = Math.max(width - ACTION_FLOW_PADDING_X * 2, ACTION_FLOW_CARD_WIDTH);
    const naturalScale = availableWidth / baseLayoutSize.width;
    const scale = Math.min(1, Math.max(ACTION_FLOW_MIN_SCALE, naturalScale));
    const scaledWidth = layout.width * scale;
    const scaledBaseHeight = baseLayoutSize.height * scale;
    const baseCanvasHeight = Math.max(ACTION_FLOW_MIN_VIEWPORT_HEIGHT, Math.ceil(scaledBaseHeight));
    const canvasHeight = Math.max(baseCanvasHeight, Math.ceil(expandedCanvasHeight * scale));
    const overflowX = scaledWidth > width;
    const left = overflowX
      ? Math.max(12, ACTION_FLOW_PADDING_X / 2)
      : Math.max(0, (width - scaledWidth) / 2);
    return {
      scale,
      left,
      top: Math.max(0, (baseCanvasHeight - scaledBaseHeight) / 2),
      height: canvasHeight,
      contentWidth: overflowX ? Math.ceil(scaledWidth + left * 2 + 12) : width,
      overflowX,
    };
  }, [baseLayoutSize.height, baseLayoutSize.width, expandedCanvasHeight, layout.width, viewportSize.width]);
  const activeConnectorPoints = useMemo(() => {
    if (hasExpandedNode) return [];
    const points = new Map();
    layout.edges.filter((edge) => edge.highlighted).forEach((edge) => {
      [
        { x: edge.sourceX, y: edge.sourceY },
        { x: edge.targetX, y: edge.targetY },
      ].forEach((point) => {
        const key = `${Math.round(point.x * 10)}-${Math.round(point.y * 10)}`;
        if (!points.has(key)) points.set(key, { ...point, key });
      });
    });
    return [...points.values()];
  }, [hasExpandedNode, layout.edges]);
  const canPanCanvas = canvasFit.overflowX;
  const { dragHandlers, isDragging } = useDraggableScroll(viewportRef, {
    disabled: hasExpandedNode || !canPanCanvas,
  });

  return (
    <div
      ref={viewportRef}
      {...dragHandlers}
      className={`relative overflow-x-auto overflow-y-hidden bg-transparent scrollbar-thin ${canPanCanvas ? (isDragging ? 'cursor-grabbing' : 'cursor-grab') : ''} ${isDragging ? 'select-none' : ''}`}
      style={{ height: canvasFit.height }}
    >
      {(layout.graph.fallback || workflow?.error) && (
        <div className="absolute left-3 top-2 z-30 max-w-[60%] truncate rounded-md border border-kumo-line bg-kumo-base/95 px-2 py-1 text-xs leading-5 text-kumo-subtle shadow-sm" title={workflow?.error || ''}>
          已按 Job 顺序显示{workflow?.error ? `：${workflow.error}` : ''}
        </div>
      )}
      <div
        className="relative origin-top-left min-w-full"
        style={{
          width: canvasFit.contentWidth,
          height: canvasFit.height,
        }}
      >
        <div
          className="relative origin-top-left"
          style={{
            width: layout.width,
            height: expandedCanvasHeight,
            transform: `translate(${canvasFit.left}px, ${canvasFit.top}px) scale(${canvasFit.scale})`,
          }}
        >
            <svg className="pointer-events-none absolute inset-0 z-0" width={layout.width} height={layout.height} aria-hidden="true">
              <g fill="none" strokeLinecap="round" strokeLinejoin="round">
                {layout.edges.filter((edge) => !edge.highlighted).map((edge) => (
                  <g key={`${edge.from}-${edge.to}`}>
                    <path
                      d={edge.path}
                      stroke="#b8c2cf"
                      strokeOpacity={hasExpandedNode ? '0.22' : (layout.hasFocus ? '0.38' : '0.98')}
                      strokeWidth="2"
                    />
                    <path
                      d={edge.path}
                      stroke="#6ea8ff"
                      strokeOpacity={hasExpandedNode ? '0.04' : (layout.hasFocus ? '0.08' : '0.26')}
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
            {hasExpandedNode && (
              <div
                className="absolute inset-0 z-30 bg-kumo-base/55 backdrop-blur-[1px]"
                onMouseDown={(event) => {
                  event.preventDefault();
                  event.stopPropagation();
                  setExpandedMatrixId(null);
                  setFocusState(null);
                }}
                aria-hidden="true"
              />
            )}
            {layout.stages.flatMap((stage) => stage.nodes.map(({ id, definitions, node, rect, highlighted }) => (
              <ActionFlowNode
                key={`${stage.stageIndex}-${id}`}
                node={node}
                expanded={expandedMatrixId === id}
                highlighted={hasExpandedNode ? expandedMatrixId === id : highlighted}
                spotlighted={expandedMatrixId === id}
                nodeRef={expandedMatrixId === id ? expandedNodeRef : null}
                onFocus={hasExpandedNode ? undefined : () => setFocusState({ ids: new Set(definitions.map((definition) => definition.id)), mode: 'lineage' })}
                onBlur={hasExpandedNode ? undefined : () => setFocusState(null)}
                onDefinitionFocus={(definitionId) => {
                  if (hasExpandedNode) return;
                  if (!definitionId) return;
                  setFocusState({ ids: new Set([definitionId]), mode: 'lineage' });
                }}
                onDefinitionBlur={() => {
                  if (hasExpandedNode) return;
                  setFocusState({ ids: new Set(definitions.map((definition) => definition.id)), mode: 'lineage' });
                }}
                onToggle={() => {
                  const primaryId = definitions[0]?.id;
                  if (!primaryId) return;
                  setFocusState(null);
                  setExpandedMatrixId((current) => (current === primaryId ? null : primaryId));
                }}
                style={{ left: rect.x, top: rect.y, width: rect.width }}
              />
            )))}
            <svg className="pointer-events-none absolute inset-0 z-30" width={layout.width} height={layout.height} aria-hidden="true">
              <g fill="none" strokeLinecap="round" strokeLinejoin="round">
                {layout.edges.filter((edge) => !hasExpandedNode && edge.highlighted).map((edge) => (
                  <path
                    key={`${edge.from}-${edge.to}-active`}
                    className="stroke-kumo-brand transition-[stroke,stroke-opacity,stroke-width] duration-150"
                    d={edge.path}
                    pathLength="1"
                    strokeDasharray="1"
                    strokeDashoffset="0"
                    strokeOpacity="0.96"
                    strokeWidth="3"
                  >
                    <animate attributeName="stroke-dashoffset" from="1" to="0" dur="180ms" fill="freeze" />
                  </path>
                ))}
              </g>
              <g className="fill-kumo-base stroke-kumo-brand" strokeWidth="2">
                {activeConnectorPoints.map((point) => (
                  <circle key={point.key} cx={point.x} cy={point.y} r="4">
                    <animate attributeName="opacity" from="0" to="1" dur="140ms" fill="freeze" />
                  </circle>
                ))}
              </g>
            </svg>
        </div>
      </div>
    </div>
  );
}

function ActionFlowPlaceholder() {
  return (
    <div className="flex min-h-[260px] items-center justify-center rounded-md border border-kumo-line bg-kumo-base px-8">
      <div className="flex w-full max-w-4xl items-center justify-center gap-4">
        {[0, 1, 2, 3].map((item) => (
          <React.Fragment key={item}>
            <div className="w-52 rounded-md border border-kumo-line bg-kumo-base p-3 shadow-sm">
              <div className="flex items-center justify-between gap-3">
                <SkeletonLine className="h-4 w-24" />
                <SkeletonLine className="h-5 w-14 rounded-full" />
              </div>
              <SkeletonLine className="mt-3 h-3 w-28" />
              <SkeletonLine className="mt-5 h-3 w-36" />
            </div>
            {item < 3 && <SkeletonLine className="h-1 w-12 shrink-0 rounded-full" />}
          </React.Fragment>
        ))}
      </div>
    </div>
  );
}

function ActionWorkflowLoadingState() {
  return (
    <div className="flex min-h-[112px] items-center justify-center bg-transparent px-6">
      <div className="w-52 rounded-md border border-kumo-line bg-kumo-base p-3 opacity-80 shadow-sm">
        <div className="flex items-center justify-between gap-3">
          <SkeletonLine className="h-4 w-24" />
          <SkeletonLine className="h-4 w-12 rounded-full" />
        </div>
        <SkeletonLine className="mt-3 h-3 w-28" />
      </div>
    </div>
  );
}

function RepositoryStat({ label, value }) {
  return (
    <div className="min-w-0 rounded-md border border-kumo-interact/70 bg-kumo-recessed/35 px-2.5 py-1.5 text-center">
      <div className="truncate text-[10px] font-medium uppercase tracking-[0.08em] text-kumo-subtle">{label}</div>
      <div className="mt-0.5 truncate text-xs font-bold text-kumo-strong">{value}</div>
    </div>
  );
}

function RepositoryCard({ item, now, config, detailLoading = false, onSelectRun }) {
  const projectPanelRef = useRef(null);
  const actionContentRef = useRef(null);
  const [panelHeights, setPanelHeights] = useState({ project: 0, action: 0 });

  // 1. 获取同一次提交 (Latest Commit SHA) 的所有 Workflow Runs 列表
  const recentRuns = Array.isArray(item?.recent_runs) && item.recent_runs.length > 0
    ? item.recent_runs
    : (item?.latest_run ? [item.latest_run] : []);

  const latestCommitSha = item?.latest_run?.commit_sha || recentRuns[0]?.commit_sha;
  const sameCommitRuns = useMemo(() => {
    if (!latestCommitSha) return recentRuns;
    const matching = recentRuns.filter(r => r.commit_sha === latestCommitSha);
    return matching.length > 0 ? matching : recentRuns;
  }, [recentRuns, latestCommitSha]);

  const [activeRunId, setActiveRunId] = useState(() => item?.latest_run?.run_id || sameCommitRuns[0]?.run_id || null);
  const [pendingRunId, setPendingRunId] = useState(null);

  useEffect(() => {
    if (sameCommitRuns.length > 0 && !sameCommitRuns.some(r => String(r.run_id) === String(activeRunId))) {
      setActiveRunId(sameCommitRuns[0]?.run_id);
    }
  }, [sameCommitRuns, activeRunId]);

  useEffect(() => {
    if (!detailLoading) {
      setPendingRunId(null);
    }
  }, [detailLoading]);

  const activeRun = useMemo(() => {
    return sameCommitRuns.find(r => String(r.run_id) === String(activeRunId)) || sameCommitRuns[0] || item?.latest_run || null;
  }, [sameCommitRuns, activeRunId, item?.latest_run]);

  const workflowName = activeRun?.workflow_name || activeRun?.display_title || '最新 Workflow';
  const actionStatus = activeRun?.conclusion || activeRun?.status || item?.latest_action_conclusion || item?.latest_action_status || 'unknown';
  const actionTone = statusTone(actionStatus);
  const jobs = Array.isArray(item?.jobs) ? item.jobs : [];
  const canLinkRepo = config?.showRepoLinks !== false && item?.html_url;
  const canLinkRun = activeRun?.html_url || item?.latest_run?.html_url;
  const hasStats = config?.showRepositoryStats !== false;
  const showDescriptions = config?.showDescriptions !== false;
  const runStartedAt = activeRun?.run_started_at || activeRun?.created_at || item?.latest_action_started_at || item?.latest_action_created_at;
  const runUpdatedAt = activeRun?.updated_at || item?.latest_action_updated_at;
  const runDuration = runStartedAt ? formatActionDuration(runStartedAt, runUpdatedAt, now) : '-';
  const hasDetailPayload = Array.isArray(item?.jobs) || Boolean(item?.workflow) || Boolean(item?.workflow_error);
  const showDetailLoading = detailLoading && !hasDetailPayload;
  const actionOverflows = panelHeights.project > 0
    && panelHeights.action + PUBLIC_ACTION_PANEL_INSET_Y > panelHeights.project + 1;
  const actionExpandedHeight = actionOverflows
    ? panelHeights.action + PUBLIC_ACTION_PANEL_INSET_Y
    : panelHeights.project;
  const isSwitchingRun = detailLoading && Boolean(pendingRunId);
  const pendingRunName = sameCommitRuns.find((run) => String(run.run_id) === String(pendingRunId))?.workflow_name
    || sameCommitRuns.find((run) => String(run.run_id) === String(pendingRunId))?.display_title
    || '工作流';

  useEffect(() => {
    const projectPanel = projectPanelRef.current;
    const actionContent = actionContentRef.current;
    if (!projectPanel || !actionContent) return undefined;

    const updateHeights = () => {
      const next = {
        project: Math.ceil(projectPanel.getBoundingClientRect().height),
        action: Math.ceil(actionContent.getBoundingClientRect().height),
      };
      setPanelHeights((current) => (
        current.project === next.project && current.action === next.action ? current : next
      ));
    };

    updateHeights();
    if (typeof ResizeObserver === 'undefined') {
      window.addEventListener('resize', updateHeights);
      return () => window.removeEventListener('resize', updateHeights);
    }
    const observer = new ResizeObserver(updateHeights);
    observer.observe(projectPanel);
    observer.observe(actionContent);
    return () => observer.disconnect();
  }, [showDetailLoading, jobs.length, item.workflow_error]);

  return (
    <article className="grid gap-3 lg:grid-cols-[minmax(360px,0.82fr)_minmax(0,1.68fr)] lg:items-start">
      <div ref={projectPanelRef} className="min-w-0 overflow-hidden rounded-lg border border-kumo-interact/75 bg-kumo-base">
        <div className="border-b border-kumo-interact/65 px-4 py-2.5">
          <div className="grid gap-1.5">
            <div className="flex min-w-0 items-center justify-between gap-2">
              <div className="flex min-w-0 items-start gap-2">
              <GitHubBrand className="mt-0.5 h-4 w-4 shrink-0 text-kumo-brand" />
              {canLinkRepo ? (
                <a
                  href={item.html_url}
                  target="_blank"
                  rel="noreferrer"
                  className="block min-w-0 flex-1 truncate pb-px text-sm font-bold leading-5 text-kumo-strong decoration-current [text-underline-offset:3px] hover:text-kumo-brand hover:underline"
                >
                  {item.full_name}
                </a>
              ) : (
                <div className="min-w-0 flex-1 truncate pb-px text-sm font-bold leading-5 text-kumo-strong">{item.full_name}</div>
              )}
              </div>

              <div className="flex shrink-0 items-center gap-1">
                <Badge variant={item.private ? 'warning' : 'success'}>
                  {item.private ? '私有' : '公开'}
                </Badge>
                <Badge
                  variant={
                    actionTone === 'success'
                      ? 'success'
                      : actionTone === 'error'
                        ? 'error'
                        : actionTone === 'warning'
                          ? 'warning'
                          : 'secondary'
                  }
                >
                  {statusLabel(actionStatus)}
                </Badge>
                <Badge variant="secondary" className="font-medium">
                  {runDuration}
                </Badge>
                {canLinkRun && (
                  <Button
                    size="sm"
                    variant="secondary"
                    shape="square"
                    icon={<ExternalLink className="h-3.5 w-3.5" />}
                    onClick={() => window.open(activeRun?.html_url || item?.latest_run?.html_url, '_blank', 'noopener,noreferrer')}
                    aria-label="打开运行详情"
                  />
                )}
              </div>
            </div>

            {showDescriptions && item.description && (
              <div className="truncate text-[12px] leading-5 text-kumo-subtle" title={formatGitHubRepositoryDescription(item.description)}>
                {formatGitHubRepositoryDescription(item.description)}
              </div>
            )}
            <div className="flex min-w-0 flex-wrap items-center gap-x-2.5 gap-y-1 text-[11px] text-kumo-subtle">
              <span className="inline-flex items-center gap-1">
                <GitBranch className="h-3.5 w-3.5" />
                {activeRun?.branch || item.default_branch || '默认分支'}
              </span>
              {activeRun?.actor && <span>{activeRun.actor}</span>}
              {activeRun?.commit_sha && <span className="font-mono">{String(activeRun.commit_sha).slice(0, 8)}</span>}
            </div>
          </div>
        </div>

        <div className={`grid min-w-0 gap-2 px-4 py-2.5 ${hasStats ? 'sm:grid-cols-[minmax(0,0.9fr)_minmax(0,1.1fr)]' : ''}`}>
          {hasStats && (
            <div className="grid grid-cols-2 gap-1.5">
              <RepositoryStat label="星标" value={formatNumber(item.stars)} />
              <RepositoryStat label="复刻" value={formatNumber(item.forks)} />
              <RepositoryStat label="议题" value={formatNumber(item.open_issues)} />
              <RepositoryStat label="拉取请求" value={formatNumber(item.open_pull_requests)} />
            </div>
          )}

          <div className="grid content-start gap-1.5 rounded-lg border border-kumo-interact/70 bg-kumo-recessed/25 px-3 py-2 text-[11px]">
            <div className="flex items-center justify-between gap-2 border-b border-kumo-interact/40 pb-1">
              <span className="font-bold text-kumo-strong text-[11px] flex items-center gap-1">
                <span>⚡ CI/CD 流水线</span>
                {sameCommitRuns.length > 1 && (
                  <span className="rounded bg-kumo-brand/10 text-kumo-brand px-1 py-0.2 text-[9px] font-mono font-semibold">
                    {sameCommitRuns.length}项工作流
                  </span>
                )}
              </span>
              <span className="shrink-0 text-[10px] text-kumo-subtle font-mono">
                {runStartedAt ? formatDateTime(runStartedAt) : '暂无记录'}
              </span>
            </div>

            {sameCommitRuns.length > 1 ? (
              <div className="max-h-[64px] overflow-y-auto scrollbar-thin space-y-1 pr-0.5">
                {sameCommitRuns.map((run) => {
                  const isSelected = String(run.run_id) === String(activeRun?.run_id);
                  const isPending = String(run.run_id) === String(pendingRunId);
                  const tone = statusTone(run.conclusion || run.status);
                  return (
                    <button
                      key={run.run_id}
                      type="button"
                      onClick={() => {
                        if (String(run.run_id) === String(activeRun?.run_id) && !detailLoading) return;
                        setPendingRunId(run.run_id);
                        setActiveRunId(run.run_id);
                        onSelectRun?.(run.run_id);
                      }}
                      className={`w-full flex items-center justify-between gap-1.5 rounded border px-2 py-0.5 text-left text-[10.5px] transition-[background-color,border-color,transform,box-shadow,color] duration-200 ${
                        isPending
                          ? 'border-kumo-brand/45 bg-kumo-brand/12 text-kumo-brand shadow-[0_0_0_1px_rgba(59,130,246,0.08)]'
                          : ''
                      } ${
                        isSelected
                          ? 'bg-kumo-brand/15 text-kumo-brand font-semibold border-kumo-brand/30'
                          : 'bg-kumo-base/60 text-kumo-subtle hover:bg-kumo-base hover:text-kumo-strong border-transparent'
                      }`}
                      disabled={isPending}
                    >
                      <div className="flex items-center gap-1.5 min-w-0">
                        <span className={`h-1.5 w-1.5 rounded-full shrink-0 ${
                          tone === 'success' ? 'bg-kumo-success' : tone === 'error' ? 'bg-kumo-danger' : 'bg-kumo-warning'
                        }`} />
                        <span className="truncate">{run.workflow_name || run.display_title}</span>
                      </div>
                      <span className="shrink-0 text-[9px] font-mono opacity-80">
                        {isPending ? (
                          <span className="inline-flex items-center gap-1 text-kumo-brand">
                            <Loader size={12} />
                            切换中
                          </span>
                        ) : (
                          statusLabel(run.conclusion || run.status)
                        )}
                      </span>
                    </button>
                  );
                })}
              </div>
            ) : (
              <div className="min-w-0 space-y-1">
                <div className="min-w-0 truncate font-semibold text-kumo-strong" title={workflowName}>{workflowName}</div>
                <div className="min-w-0 truncate text-kumo-subtle" title={activeRun?.commit_message || activeRun?.display_title || ''}>
                  {activeRun?.commit_message || activeRun?.display_title || '这个仓库还没有可展示的 workflow 运行记录。'}
                </div>
              </div>
            )}
          </div>
        </div>
      </div>

      <div
        className={`group relative min-w-0 overflow-visible rounded-lg border border-kumo-interact/45 bg-kumo-base/35 p-3 transition-[height,box-shadow,background-color] duration-300 ease-[cubic-bezier(0.22,1,0.36,1)] will-change-[height] hover:bg-kumo-base/50 lg:h-[var(--public-action-panel-height)] ${actionOverflows ? 'lg:overflow-hidden lg:hover:h-[var(--public-action-expanded-height)] lg:hover:shadow-lg' : ''}`}
        style={panelHeights.project > 0 ? {
          '--public-action-panel-height': `${panelHeights.project}px`,
          '--public-action-expanded-height': `${actionExpandedHeight}px`,
        } : undefined}
      >
        {isSwitchingRun && (
          <div className="pointer-events-none absolute inset-x-3 top-3 z-20 flex items-center justify-between rounded-md border border-kumo-brand/30 bg-kumo-base/88 px-3 py-2 text-[11px] text-kumo-brand shadow-sm backdrop-blur-sm">
            <span className="inline-flex min-w-0 items-center gap-1.5">
              <Loader size={14} />
              <span className="truncate">正在切换到 {pendingRunName}</span>
            </span>
            <span className="font-mono text-[10px] text-kumo-subtle">加载最新 Job</span>
          </div>
        )}
        <div className={`w-full min-w-0 lg:flex lg:h-full ${actionOverflows ? 'lg:items-start' : 'lg:items-center'}`}>
          <div ref={actionContentRef} className={`w-full min-w-0 transition-opacity duration-200 ${isSwitchingRun ? 'opacity-45' : 'opacity-100'}`}>
          {showDetailLoading ? (
            <ActionWorkflowLoadingState />
          ) : item.workflow_error && jobs.length === 0 ? (
            <div className="rounded-md border border-kumo-warning/25 bg-kumo-warning/8 px-3 py-2.5 text-sm text-kumo-subtle">
              {item.workflow_error}
            </div>
          ) : jobs.length === 0 ? (
            <div className="rounded-md border border-kumo-interact/70 bg-kumo-recessed/20 px-3 py-2.5 text-sm text-kumo-subtle">
              暂无 Job 进度数据
            </div>
          ) : (
            <ActionWorkflowCanvas workflow={item.workflow} jobs={jobs} now={now} />
          )}
          </div>
        </div>
        {actionOverflows && (
          <div className="pointer-events-none absolute inset-x-px bottom-px hidden h-12 rounded-b-lg bg-gradient-to-t from-kumo-base/95 via-kumo-base/55 to-transparent opacity-100 transition-opacity duration-300 ease-out group-hover:opacity-0 lg:block" aria-hidden="true" />
        )}
      </div>
    </article>
  );
}

function PublicGitHubPage({ domainOnly = false, onDomainNotFound }) {
  const isAuthenticated = useStore((state) => state.isAuthenticated);
  const slug = useMemo(() => normalizePublicPath(), []);
  const surfaceRef = useCloudflareSpotlight();
  const [page, setPage] = useState(null);
  const [loading, setLoading] = useState(true);
  const [refreshing, setRefreshing] = useState(false);
  const [error, setError] = useState('');
  const currentTime = useNowTick(1000);
  const [detailStatusByRepo, setDetailStatusByRepo] = useState({});
  const [repositoryFilter, setRepositoryFilter] = useState('all');
  const pageRef = useRef(null);
  const detailRequestSeqRef = useRef({});
  const loadRequestSeqRef = useRef(0);

  useEffect(() => {
    pageRef.current = page;
  }, [page]);

  const refreshRepositoryDetail = useCallback(async (pageSlug, repositoryID, { markLoading = true, runId = '' } = {}) => {
    const repoId = String(repositoryID || '');
    if (!pageSlug || !repoId) return;

    const nextRequestSeq = (detailRequestSeqRef.current[repoId] || 0) + 1;
    detailRequestSeqRef.current = { ...detailRequestSeqRef.current, [repoId]: nextRequestSeq };

    if (markLoading) {
      setDetailStatusByRepo((current) => ({ ...current, [repoId]: 'loading' }));
    }

    try {
      const params = new URLSearchParams();
      if (runId) params.set('run_id', String(runId));
      const response = await fetch(`/api/github/public/pages/${encodeURIComponent(pageSlug)}/repositories/${encodeURIComponent(repoId)}${params.toString() ? `?${params}` : ''}`, {
        cache: 'no-store',
      });
      const result = await response.json().catch(() => ({}));
      if (detailRequestSeqRef.current[repoId] !== nextRequestSeq) return;
      if (!response.ok || result.success === false) {
        throw new Error(result.error || 'Workflow 详情加载失败');
      }

      const detail = result.data || result;
      setPage((current) => {
        if (!current || current.slug !== pageSlug) return current;
        const repositories = Array.isArray(current.repositories) ? current.repositories : [];
        let changed = false;
        const nextRepositories = repositories.map((item) => {
          if (String(item?.id || '') !== repoId) return item;
          changed = true;
          return { ...item, ...detail };
        });
        return changed ? { ...current, repositories: nextRepositories } : current;
      });
      setDetailStatusByRepo((current) => ({ ...current, [repoId]: 'loaded' }));
    } catch (detailError) {
      if (detailRequestSeqRef.current[repoId] !== nextRequestSeq) return;
      setPage((current) => {
        if (!current || current.slug !== pageSlug) return current;
        const repositories = Array.isArray(current.repositories) ? current.repositories : [];
        let changed = false;
        const nextRepositories = repositories.map((item) => {
          if (String(item?.id || '') !== repoId) return item;
          changed = true;
          return {
            ...item,
            workflow_error: detailError.message || 'Workflow 详情加载失败',
          };
        });
        return changed ? { ...current, repositories: nextRepositories } : current;
      });
      setDetailStatusByRepo((current) => ({ ...current, [repoId]: 'failed' }));
    }
  }, []);

  const loadRepositoryDetails = useCallback(async (nextPage) => {
    const repositories = Array.isArray(nextPage?.repositories) ? nextPage.repositories : [];
    if (!nextPage?.slug || repositories.length === 0) {
      detailRequestSeqRef.current = {};
      setDetailStatusByRepo({});
      return;
    }

    const nextStatuses = {};
    const targets = [];
    repositories.forEach((repo) => {
      const repoId = String(repo?.id || '');
      if (!String(repo?.latest_run?.run_id || '')) {
        nextStatuses[repoId] = 'idle';
        return;
      }
      if (hasPublicGithubWorkflowDetail(repo)) {
        nextStatuses[repoId] = 'loaded';
        return;
      }
      nextStatuses[repoId] = 'loading';
      targets.push(repoId);
    });
    setDetailStatusByRepo(nextStatuses);

    await Promise.allSettled(targets.map((repoId) => (
      refreshRepositoryDetail(nextPage.slug, repoId, { markLoading: false })
    )));
  }, [refreshRepositoryDetail]);

  const syncRepositories = useCallback(async ({
    pageSlug,
    repositories,
    markLoading = false,
    onlyMissingDetail = false,
  }) => {
    if (!pageSlug) return;
    const targets = (Array.isArray(repositories) ? repositories : [])
      .map((repo) => ({ repoId: String(repo?.id || ''), repo }))
      .filter(({ repoId, repo }) => repoId && (!onlyMissingDetail || shouldLoadPublicGithubRepositoryDetail(repo)));

    if (targets.length === 0) return;

    await Promise.allSettled(targets.map(({ repoId }) => (
      refreshRepositoryDetail(pageSlug, repoId, { markLoading })
    )));
  }, [refreshRepositoryDetail]);

  const loadSummary = useCallback(async ({ silent = false, showRefreshing = silent } = {}) => {
    const requestSeq = loadRequestSeqRef.current + 1;
    loadRequestSeqRef.current = requestSeq;
    if (!silent) setLoading(true);
    else if (showRefreshing) setRefreshing(true);
    setError('');
    try {
      const endpoint = slug && !domainOnly
        ? `/api/github/public/pages/${encodeURIComponent(slug)}?summary=1`
        : `/api/github/public/page-by-domain?domain=${encodeURIComponent(window.location.host)}&summary=1`;
      const response = await fetch(endpoint, { cache: 'no-store' });
      const result = await response.json().catch(() => ({}));
      if (loadRequestSeqRef.current !== requestSeq) return;
      if (!response.ok || result.success === false) {
        const nextError = new Error(result.error || 'GitHub 公开页不存在或未公开');
        nextError.status = response.status;
        throw nextError;
      }
      const previousRepositories = Array.isArray(pageRef.current?.repositories) ? pageRef.current.repositories : [];
      const rawPage = result.data || result;
      const mergedPage = {
        ...rawPage,
        repositories: mergePublicGithubRepositories(
          Array.isArray(rawPage?.repositories) ? rawPage.repositories : [],
          previousRepositories,
        ),
      };
      setPage(mergedPage);
      return mergedPage;
    } catch (err) {
      if (loadRequestSeqRef.current !== requestSeq) return;
      if (!slug && domainOnly && err.status === 404 && onDomainNotFound) {
        onDomainNotFound();
        return;
      }
      setError(err.message || 'GitHub 公开页加载失败');
      if (!silent) {
        setPage(null);
        detailRequestSeqRef.current = {};
        setDetailStatusByRepo({});
      }
    } finally {
      if (loadRequestSeqRef.current === requestSeq) {
        if (!silent) setLoading(false);
        if (showRefreshing) setRefreshing(false);
      }
    }
    return null;
  }, [domainOnly, onDomainNotFound, slug]);

  const load = useCallback(async ({ silent = false, showRefreshing = silent } = {}) => {
    const nextPage = await loadSummary({ silent, showRefreshing });
    if (!nextPage) return;
    if (silent) {
      void syncRepositories({
        pageSlug: nextPage.slug,
        repositories: nextPage.repositories,
      });
      return;
    }
    void loadRepositoryDetails(nextPage);
  }, [loadRepositoryDetails, loadSummary, syncRepositories]);

  useEffect(() => {
    load();
  }, [load]);

  useEffect(() => {
    if (!page?.title) return undefined;
    const previousTitle = document.title;
    document.title = page.title;
    return () => {
      document.title = previousTitle;
    };
  }, [page?.title]);

  useEffect(() => swapPublicPageFavicon(getPublicPageFaviconHref('github', page?.config)), [page?.config]);

  useEffect(() => {
    if (typeof window.EventSource === 'function') return undefined;
    const interval = window.setInterval(() => {
      if (document.hidden) return;
      void loadSummary({ silent: true, showRefreshing: false }).then((nextPage) => {
        const currentPage = nextPage || pageRef.current;
        if (!currentPage?.slug) return;
        void syncRepositories({
          pageSlug: currentPage.slug,
          repositories: currentPage.repositories,
        });
      });
    }, getPublicGithubRefreshInterval(page));
    return () => window.clearInterval(interval);
  }, [loadSummary, page, syncRepositories]);

  useEffect(() => {
    if (!page?.slug || typeof window.EventSource !== 'function') return undefined;
    const source = new window.EventSource(`/api/github/public/pages/${encodeURIComponent(page.slug)}/stream`);
    const refreshRepository = (event) => {
      const payload = parsePublicGithubStreamPayload(event);
      const kind = String(payload?.kind || '');
      const repoId = String(payload?.repository_id || '');
      if (!repoId || !['repository_refresh', 'repository_actions_refresh'].includes(kind)) return;
      void loadSummary({ silent: true, showRefreshing: false }).then((nextPage) => {
        if (!nextPage) return;
        return refreshRepositoryDetail(nextPage.slug, repoId, { markLoading: false });
      });
    };
    source.addEventListener('github', refreshRepository);
    return () => {
      source.removeEventListener('github', refreshRepository);
      source.close();
    };
  }, [loadSummary, page?.slug, refreshRepositoryDetail]);

  const repositories = Array.isArray(page?.repositories) ? page.repositories : [];
  const dataUpdatedAt = getPublicGithubDataUpdatedAt(page);
  const failureCount = repositories.filter((repo) => statusTone(repo?.latest_run?.conclusion || repo?.latest_run?.status || repo?.latest_action_conclusion || repo?.latest_action_status) === 'error').length;
  const warningCount = repositories.filter((repo) => statusTone(repo?.latest_run?.conclusion || repo?.latest_run?.status || repo?.latest_action_conclusion || repo?.latest_action_status) === 'warning').length;
  const successCount = repositories.filter((repo) => statusTone(repo?.latest_run?.conclusion || repo?.latest_run?.status || repo?.latest_action_conclusion || repo?.latest_action_status) === 'success').length;
  const neutralCount = Math.max(0, repositories.length - failureCount - warningCount - successCount);
  const visibleRepositories = repositoryFilter === 'success'
    ? repositories.filter((repo) => statusTone(repo?.latest_run?.conclusion || repo?.latest_run?.status || repo?.latest_action_conclusion || repo?.latest_action_status) === 'success')
    : repositoryFilter === 'failure'
      ? repositories.filter((repo) => statusTone(repo?.latest_run?.conclusion || repo?.latest_run?.status || repo?.latest_action_conclusion || repo?.latest_action_status) === 'error')
      : repositoryFilter === 'other'
        ? repositories.filter((repo) => !['success', 'error'].includes(statusTone(repo?.latest_run?.conclusion || repo?.latest_run?.status || repo?.latest_action_conclusion || repo?.latest_action_status)))
        : repositories;
  const pageTone = repositories.length === 0 ? 'neutral' : failureCount > 0 ? 'danger' : warningCount > 0 ? 'warning' : 'success';
  const summaryText = repositories.length === 0
    ? '暂无公开仓库'
    : failureCount > 0
    ? `${failureCount} 个仓库的最新工作流存在失败`
    : warningCount > 0
    ? `${warningCount} 个仓库的最新工作流仍在运行或等待`
    : '全部仓库的最新工作流状态正常';
  const config = page?.config || {};

  const updatePageIcon = async (iconId) => {
    if (!page?.id) return;
    const response = await fetch(`/api/github/public-pages/${page.id}`, {
      method: 'PUT',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({
        config: withPublicPageIconId(page.config, iconId),
      }),
    });
    const result = await response.json().catch(() => ({}));
    if (!response.ok || result.success === false) {
      throw new Error(result.error || '保存 GitHub 公开页图标失败');
    }
    setPage((current) => (current ? {
      ...current,
      config: withPublicPageIconId(current.config, iconId),
    } : current));
    toast.success(iconId ? 'GitHub 公开页图标已更新' : '已恢复 GitHub 公开页默认图标');
  };

  return (
    <div ref={surfaceRef} className="cf-ai-background-surface public-github-page relative isolate min-h-screen text-kumo-default">
      <div aria-hidden="true" className="cf-ai-background pointer-events-none absolute inset-0" />
      <main className="relative z-10 mx-auto flex min-h-screen w-full max-w-[96rem] flex-col px-4 py-5 sm:px-6 lg:px-8">
        <div className="mb-4 flex items-center justify-between gap-3">
          <div className="flex min-w-0 items-center gap-2.5">
            <PublicPageIconPicker
              pageKind="github"
              config={page?.config}
              isAuthenticated={isAuthenticated}
              onChange={updatePageIcon}
              triggerClassName="flex h-8 w-8 shrink-0 items-center justify-center rounded-lg border border-kumo-interact/80 bg-kumo-base text-kumo-brand"
              iconClassName="h-5 w-5"
            />
            <div className="min-w-0">
              <div className="truncate text-base font-bold text-kumo-strong">{page?.title || 'GitHub 动态'}</div>
            </div>
          </div>
          <div className="flex items-center gap-2">
            <Button size="sm" variant="secondary" onClick={() => load({ silent: true })} loading={refreshing} icon={<RefreshCw className="h-3.5 w-3.5" />}>
              刷新
            </Button>
            <Button
              size="sm"
              variant="secondary"
              onClick={() => { window.location.href = '/'; }}
              icon={isAuthenticated ? <Home className="h-3.5 w-3.5" /> : <LogIn className="h-3.5 w-3.5" />}
              aria-label={isAuthenticated ? '主页' : '登录'}
              title={isAuthenticated ? '跳转到主页' : '跳转到登录页'}
            >
              {isAuthenticated ? '主页' : '登录'}
            </Button>
          </div>
        </div>

        {loading && !page && (
          <div className="flex flex-col gap-3">
            <section className="public-github-card rounded-lg border border-kumo-interact/80 bg-kumo-base px-4 py-3.5">
              <div className="grid gap-3">
                <SkeletonLine className="h-5 w-48" />
                <SkeletonLine className="h-4 w-72" />
              </div>
            </section>
            <ActionFlowPlaceholder />
          </div>
        )}

        {!loading && error && !page && (
          <div className="public-github-card flex flex-1 flex-col items-center justify-center rounded-lg border border-kumo-interact/80 bg-kumo-base p-10 text-center">
            <AlertTriangle className="mb-3 h-9 w-9 text-kumo-warning" />
            <h1 className="text-lg font-bold text-kumo-strong">无法显示 GitHub 公开页</h1>
            <p className="mt-2 max-w-md text-sm leading-relaxed text-kumo-subtle">{error}</p>
          </div>
        )}

        {page && (
          <div className="flex flex-col gap-3">
            <section className={`public-github-card rounded-lg border px-4 py-3 ${statusPanelClass[pageTone]}`}>
              <div className="flex flex-col gap-2.5 sm:flex-row sm:items-center sm:justify-between">
                <div>
                  <div className="flex items-center gap-2 text-base font-bold">
                    <Globe className="h-4 w-4" />
                    {summaryText}
                  </div>
                  {page.description && (
                    <p className="mt-1.5 max-w-3xl text-[13px] leading-5 opacity-90">{page.description}</p>
                  )}
                </div>
                <PublicOverviewStats
                  activeKey={repositoryFilter}
                  onChange={setRepositoryFilter}
                  items={[
                    { key: 'all', label: '仓库', value: repositories.length },
                    { key: 'success', label: '正常', value: successCount },
                    { key: 'failure', label: '失败', value: failureCount },
                    { key: 'other', label: '其他', value: warningCount + neutralCount },
                  ]}
                />
              </div>
            </section>

            {repositories.length === 0 ? (
              <section className="public-github-card rounded-lg border border-kumo-interact/80 bg-kumo-base p-6 text-center text-sm text-kumo-subtle">
                这个 GitHub 公开页还没有绑定仓库。
              </section>
            ) : (
              <section className="grid gap-3">
                {visibleRepositories.map((item) => (
                  <RepositoryCard
                    key={item.id || item.full_name}
                    item={item}
                    now={currentTime}
                    config={config}
                    detailLoading={detailStatusByRepo[String(item.id || '')] === 'loading'}
                    onSelectRun={(runId) => refreshRepositoryDetail(slug, item.id, { markLoading: true, runId })}
                  />
                ))}
                {visibleRepositories.length === 0 && (
                  <div className="rounded-lg border border-kumo-interact/70 bg-kumo-base p-6 text-center text-sm text-kumo-subtle">暂无匹配仓库。</div>
                )}
              </section>
            )}

            <footer className="flex flex-col gap-2 py-3 text-xs text-kumo-subtle sm:flex-row sm:items-center sm:justify-between">
              <span className="inline-flex items-center gap-1">
                <Shield className="h-3.5 w-3.5" />
                由 API Monitor 提供
              </span>
              <span>最后更新：{formatDateTime(dataUpdatedAt)}</span>
            </footer>
          </div>
        )}
      </main>
    </div>
  );
}

export default PublicGitHubPage;
