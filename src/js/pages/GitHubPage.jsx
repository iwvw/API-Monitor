import React, { useCallback, useEffect, useMemo, useRef, useState } from 'react';
import {
  Badge,
  ChartPalette,
  ClipboardText,
  Empty,
  Grid,
  LayerCard,
  Tabs,
  Text,
  Toolbar,
} from '@cloudflare/kumo';
import { Button } from '@cloudflare/kumo/components/button';
import { Dialog } from '@cloudflare/kumo/components/dialog';
import { Input } from '@cloudflare/kumo/components/input';
import { SkeletonLine } from '@cloudflare/kumo/components/loader';
import { Select } from '@cloudflare/kumo/components/select';
import { Switch } from '@cloudflare/kumo/components/switch';
import { Table } from '@cloudflare/kumo/components/table';
import * as echarts from 'echarts/core';
import { LineChart } from 'echarts/charts';
import { AriaComponent, GridComponent, TooltipComponent } from 'echarts/components';
import { CanvasRenderer } from 'echarts/renderers';
import { toast } from '../modules/toast.js';
import { dialog } from '../modules/dialog.js';
import { useConfirmPress } from '../hooks/useConfirmPress.js';
import { formatGitHubRepositoryDescription } from '../modules/githubEmoji.js';
import { normalizeWorkflowJobName, workflowJobMatchesDefinition } from '../modules/githubWorkflowJobs.js';
import { useNowTick } from '../modules/usePageVisibility.js';
import { useDraggableScroll } from '../hooks/useDraggableScroll.js';
import { MODULE_TABS_PROPS } from '../modules/kumoTabs.js';
import useStore from '../store.js';
import { AnimatedCollapse } from '../components/AnimatedCollapse.jsx';
import SiteFontTimeseriesChart from '../components/SiteFontTimeseriesChart.jsx';
import { AppTable, ChartBoundaryBox, ChartWarmupSkeleton, DataTableFrame, TabBarOverflowActions, sectionCardHeaderClass, stickyTabsBaseClass } from '../components/ui/AppPrimitives.jsx';
import GitHubPublicPagesPanel from '../components/github/GitHubPublicPagesPanel.jsx';
import {
  Activity,
  AlertTriangle,
  Bell,
  Check,
  Clock,
  ExternalLink,
  GitBranch,
  GitHubBrand,
  Globe,
  Key,
  Play,
  PlayCircle,
  Plus,
  RefreshCw,
  Rocket,
  Save,
  Settings,
  Star,
  Trash,
  TrendingUp,
  Upload,
  Download,
  Users,
  X,
} from '../components/Icons.jsx';

echarts.use([LineChart, GridComponent, TooltipComponent, AriaComponent, CanvasRenderer]);

const tabs = [
  { value: 'repositories', label: <span className="inline-flex items-center gap-1.5"><GitBranch className="h-3.5 w-3.5" />仓库</span> },
  { value: 'actions', label: <span className="inline-flex items-center gap-1.5"><PlayCircle className="h-3.5 w-3.5" />Actions</span> },
  { value: 'trends', label: <span className="inline-flex items-center gap-1.5"><TrendingUp className="h-3.5 w-3.5" />趋势</span> },
  { value: 'events', label: <span className="inline-flex items-center gap-1.5"><Bell className="h-3.5 w-3.5" />事件</span> },
  { value: 'public-pages', label: <span className="inline-flex items-center gap-1.5"><Globe className="h-3.5 w-3.5" />公开页</span> },
  { value: 'settings', label: <span className="inline-flex items-center gap-1.5"><Settings className="h-3.5 w-3.5" />设置</span> },
];

const tokenTypeOptions = [
  { value: 'fine_grained', label: 'Fine-grained PAT' },
  { value: 'classic', label: 'Classic PAT' },
  { value: 'app', label: 'GitHub App' },
];

const fineGrainedTokenURL = (resourceOwner = '') => {
  const params = new URLSearchParams({
    name: 'API-Monitor',
    description: 'API-Monitor GitHub observability',
    expires_in: 'none',
    actions: 'write',
    administration: 'read',
    contents: 'read',
    issues: 'read',
    pull_requests: 'read',
    webhooks: 'write',
    workflows: 'write',
  });
  if (resourceOwner) params.set('target_name', resourceOwner);
  return `https://github.com/settings/personal-access-tokens/new?${params.toString()}`;
};

const rangeOptions = [
  { value: '7', label: '7 天' },
  { value: '30', label: '30 天' },
  { value: '90', label: '90 天' },
  { value: '365', label: '365 天' },
];

const GITHUB_ACTIONS_TABLE_WIDTHS = [132, 220, 480, 132, 168, 124];
const GITHUB_EVENTS_TABLE_WIDTHS = [420, 120, 140, 200];

const SCOPE_BADGE_VARIANTS = {
  'admin:org': 'red',
  'admin:org_hook': 'red',
  'admin:packages': 'red',
  'admin:repo_hook': 'red',
  'admin:gpg_key': 'purple',
  'admin:public_key': 'purple',
  'audit_log': 'red',
  'delete_repo': 'red',
  gist: 'teal',
  notifications: 'teal',
  project: 'orange',
  'read:gpg_key': 'purple',
  'read:org': 'orange',
  'read:packages': 'orange',
  'read:project': 'orange',
  'read:public_key': 'purple',
  'read:repo_hook': 'orange',
  'read:user': 'green',
  repo: 'blue',
  'repo:status': 'blue',
  repo_deployment: 'blue',
  security_events: 'purple',
  'user:email': 'green',
  'user:follow': 'green',
  workflow: 'purple',
  'write:org': 'red',
  'write:packages': 'orange',
  'write:repo_hook': 'orange',
};

const scopeBadgeVariant = (scope) => SCOPE_BADGE_VARIANTS[String(scope || '')] || 'neutral';

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

const ACTION_FLOW_CARD_WIDTH = 260;
const ACTION_FLOW_STAGE_GAP = 48;
const ACTION_FLOW_PADDING_X = 28;
const ACTION_FLOW_PADDING_Y = 28;
const ACTION_FLOW_ROW_GAP = 38;
const ACTION_FLOW_VIEWPORT_HEIGHT = 320;
const ACTION_FLOW_MIN_VIEWPORT_HEIGHT = 112;
const ACTION_FLOW_MIN_SCALE = 0.72;
const ACTION_FLOW_BRANCH_INSET = 28;

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
  const edgeBusXMap = new Map();
  candidateEdges.forEach((edge) => {
    edgeBusXMap.set(edge, resolveActionConnectorBusX(edge, stages));
  });

  outgoing.forEach((edgesFromSource) => {
    if (edgesFromSource.length <= 1) return;
    const adjacentEdges = edgesFromSource.filter((edge) => edge.toStage - edge.fromStage <= 1);
    if (adjacentEdges.length <= 1) return;
    const sampleEdge = adjacentEdges[0];
    const sourceX = sampleEdge.sourceRect.x + sampleEdge.sourceRect.width;
    const targetX = sampleEdge.targetRect.x;
    const unifiedBusX = sourceX + Math.min(ACTION_FLOW_BRANCH_INSET, (targetX - sourceX) * 0.45);
    adjacentEdges.forEach((edge) => edgeBusXMap.set(edge, unifiedBusX));
  });

  incoming.forEach((edgesToTarget) => {
    if (edgesToTarget.length <= 1) return;
    const adjacentEdges = edgesToTarget.filter((edge) => edge.toStage - edge.fromStage <= 1);
    if (adjacentEdges.length <= 1) return;
    const sampleEdge = adjacentEdges[0];
    const sourceX = sampleEdge.sourceRect.x + sampleEdge.sourceRect.width;
    const targetX = sampleEdge.targetRect.x;
    const unifiedBusX = targetX - Math.min(ACTION_FLOW_BRANCH_INSET, (targetX - sourceX) * 0.45);
    adjacentEdges.forEach((edge) => {
      if (!edge.fanOut) {
        edgeBusXMap.set(edge, unifiedBusX);
      }
    });
  });

  const edges = candidateEdges.map((edge) => {
    const route = routeActionConnector(edge, stages, edgeBusXMap.get(edge));
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

function RepositoryMetric({ icon, label, value, detail }) {
  return (
    <LayerCard className="min-w-0 self-start px-3 py-2 shadow-none">
      <div className="flex min-w-0 items-center gap-1.5 text-kumo-subtle">
        {icon}
        <Text variant="secondary" size="sm" truncate>{label}</Text>
      </div>
      <div className="mt-1 flex min-w-0 items-baseline justify-between gap-2">
        <Text variant="heading3" as="span" truncate>{value}</Text>
        {detail && <Text variant="secondary" size="xs" truncate>{detail}</Text>}
      </div>
    </LayerCard>
  );
}

function FillEmpty({ title, description }) {
  return (
    <div className="flex items-center justify-center p-8">
      <Empty title={title} description={description} />
    </div>
  );
}

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
      className={`absolute grid min-w-0 content-start gap-1.5 overflow-visible rounded-md border bg-kumo-base px-3 py-2.5 transition-[border-color,box-shadow,opacity,filter] ${spotlighted ? 'z-40 border-brand/60 ring-2 ring-brand/20' : 'z-20'} ${active && !spotlighted ? 'border-brand/45 ring-1 ring-brand/20' : ''} ${!active && !spotlighted ? 'border-kumo-interact/70' : ''} ${muted ? 'opacity-[0.42] saturate-75' : 'opacity-100'}`}
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
      className={`relative overflow-x-auto overflow-y-hidden rounded-md border border-kumo-line bg-kumo-base scrollbar-thin ${canPanCanvas ? (isDragging ? 'cursor-grabbing' : 'cursor-grab') : ''} ${isDragging ? 'select-none' : ''}`}
      style={{ height: canvasFit.height }}
    >
      {(layout.graph.fallback || workflow?.error) && (
        <div className="absolute left-3 top-2 z-30 max-w-[60%] truncate rounded-md border border-kumo-line bg-kumo-base/95 px-2 py-1 text-xs leading-5 text-kumo-subtle" title={workflow?.error || ''}>
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
                    className="stroke-brand transition-[stroke,stroke-opacity,stroke-width] duration-150"
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
              <g className="fill-kumo-base stroke-brand" strokeWidth="2">
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
            <div className="w-52 rounded-md border border-kumo-line bg-kumo-base p-3">
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

function RepositoryStat({ label, value }) {
  return (
    <LayerCard className="min-w-0 p-2 text-center shadow-none">
      <div className="truncate text-[10px] font-medium text-kumo-subtle">{label}</div>
      <div className="truncate text-xs font-semibold text-kumo-strong">{value}</div>
    </LayerCard>
  );
}

function PermissionChecks({ token }) {
  const permissions = parseJSON(token.permissions_json);
  const checks = Array.isArray(permissions.checks) ? permissions.checks : [];
  const scopes = Array.isArray(permissions.scopes) ? permissions.scopes : [];
  if (checks.length === 0 && scopes.length === 0 && !token.last_test_error) {
    if (token.last_test_status === 'success' && token.last_test_at) {
      return <Text variant="secondary" size="xs">基础认证通过。选择仓库后再次检测可验证 Actions 和 Traffic 权限。检测时间：{formatDateTime(token.last_test_at)}</Text>;
    }
    return <Text variant="secondary" size="xs">点击“检测权限”验证 Token；选择仓库后可同时验证仓库权限。</Text>;
  }
  return (
    <div className="grid gap-2">
      {scopes.length > 0 && (
        <div className="flex min-w-0 flex-wrap items-center gap-1 text-[11px] text-kumo-subtle">
          <span>Classic scopes</span>
          {scopes.map((scope) => <Badge key={scope} variant={scopeBadgeVariant(scope)}>{scope}</Badge>)}
        </div>
      )}
      {checks.length > 0 && (
        <div className="grid gap-1 cq-sm:grid-cols-2">
          {checks.map((check) => (
            <div key={check.key || check.label} className="flex min-w-0 items-center justify-between gap-2 rounded-md border border-kumo-line px-2 py-1.5 text-[11px]">
              <span className="min-w-0 truncate text-kumo-strong">{check.label}</span>
              <div className="flex min-w-0 items-center gap-1">
                <span className="hidden max-w-32 truncate text-kumo-subtle cq-md:inline">{check.level}</span>
                <Badge variant={check.status === 'success' ? 'success' : check.status === 'skipped' ? 'neutral' : 'danger'}>
                  {check.status === 'success' ? '通过' : check.status === 'skipped' ? '跳过' : '失败'}
                </Badge>
              </div>
            </div>
          ))}
        </div>
      )}
      {token.last_test_error && <div className="truncate text-xs text-kumo-danger">{token.last_test_error}</div>}
    </div>
  );
}

function GitHubPage() {
  const { theme } = useStore();
  const { isArmed, confirmPress } = useConfirmPress();
  const isDarkMode = theme === 'dark';
  const getAuthHeaders = useCallback(() => ({
    'Content-Type': 'application/json',
  }), []);

  const [activeTab, setActiveTab] = useState('repositories');
  const [repoDialogOpen, setRepoDialogOpen] = useState(false);
  const [repositories, setRepositories] = useState([]);
  const [tokens, setTokens] = useState([]);
  const [settings, setSettings] = useState(null);
  const [collector, setCollector] = useState(null);
  const [selectedRepoId, setSelectedRepoId] = useState(null);
  const [trends, setTrends] = useState([]);
  const [trendsLoading, setTrendsLoading] = useState(false);
  const [actions, setActions] = useState([]);
  const [selectedActionRunId, setSelectedActionRunId] = useState(null);
  const [collapsingActionRunId, setCollapsingActionRunId] = useState(null);
  const [actionJobs, setActionJobs] = useState([]);
  const [actionWorkflow, setActionWorkflow] = useState(null);
  const [actionJobsLoading, setActionJobsLoading] = useState(false);
  const [events, setEvents] = useState([]);
  const [traffic, setTraffic] = useState([]);
  const [contributors, setContributors] = useState([]);
  const [workflows, setWorkflows] = useState([]);
  const [branches, setBranches] = useState([]);
  const [detailsRepoId, setDetailsRepoId] = useState(null);
  const currentTime = useNowTick(1000);
  const [saving, setSaving] = useState(false);
  const [testingTokenId, setTestingTokenId] = useState(null);
  const [refreshingRepositoryId, setRefreshingRepositoryId] = useState(null);
  const [draggedRepositoryId, setDraggedRepositoryId] = useState(null);
  const [rangeDays, setRangeDays] = useState('30');
  const [repoForm, setRepoForm] = useState({ url: '', token_id: '', collect_interval_seconds: 900, retention_days: 90, webhook_enabled: false });
  const [tokenForm, setTokenForm] = useState({ name: '', token: '', type: 'fine_grained', default_token: false });
  const [dispatchForm, setDispatchForm] = useState({ workflowId: '', ref: '' });
  const [historyRetentionDays, setHistoryRetentionDays] = useState('90');
  const [historyScope, setHistoryScope] = useState('all');
  const [maintenanceAction, setMaintenanceAction] = useState('');
  const eventSourceRef = useRef(null);
  const dispatchDefaultedRepoRef = useRef(null);
  const actionJobsRef = useRef([]);
  const actionCollapseTimerRef = useRef(null);

  const selectedRepo = useMemo(
    () => repositories.find((repo) => String(repo.id) === String(selectedRepoId)) || repositories[0] || null,
    [repositories, selectedRepoId]
  );

  useEffect(() => {
    if (historyScope === 'current' && !selectedRepo?.id) {
      setHistoryScope('all');
    }
  }, [historyScope, selectedRepo]);
  const canAttemptActionOperations = Boolean(
    selectedRepo?.authenticated && selectedRepo?.can_operate_actions
  );

  const api = useCallback(async (path, options = {}) => {
    const response = await fetch(path, {
      ...options,
      headers: {
        ...getAuthHeaders(),
        ...(options.headers || {}),
      },
    });
    const result = await response.json().catch(() => ({}));
    if (!response.ok || result.success === false) {
      throw new Error(result.error || `请求失败: ${response.status}`);
    }
    return result.data !== undefined ? result.data : result;
  }, [getAuthHeaders]);

  const loadOverview = useCallback(async () => {
    try {
      const [overview, tokenList] = await Promise.all([
        api('/api/github'),
        api('/api/github/tokens'),
      ]);
      const repos = overview.repositories || [];
      setRepositories(repos);
      setTokens(tokenList || []);
      setSettings(overview.settings || null);
      setCollector(overview.collector || null);
      setSelectedRepoId((current) => current || repos[0]?.id || null);
    } catch (error) {
      toast.error(error.message || '加载 GitHub 模块失败');
    }
  }, [api]);

  const repoImportInputRef = useRef(null);
  const [repoImporting, setRepoImporting] = useState(false);

  const exportRepositories = () => {
    if (repositories.length === 0) { toast.warning('暂无仓库可导出'); return; }
    const payload = {
      version: '1.0',
      repositories: repositories.map((repo) => ({
        owner: repo.owner,
        name: repo.name,
        tags: Array.isArray(repo.tags) ? repo.tags : [],
        note: repo.note || '',
        enabled: repo.enabled !== false,
        notify_enabled: repo.notify_enabled !== false,
      })),
    };
    const blob = new Blob([JSON.stringify(payload, null, 2)], { type: 'application/json' });
    const url = URL.createObjectURL(blob);
    const anchor = document.createElement('a');
    anchor.href = url;
    anchor.download = `github-repositories-${new Date().toISOString().slice(0, 10)}.json`;
    document.body.appendChild(anchor);
    anchor.click();
    anchor.remove();
    URL.revokeObjectURL(url);
    toast.success(`已导出 ${payload.repositories.length} 个仓库`);
  };

  const importRepositoriesFromFile = async (event) => {
    const file = event.target.files?.[0];
    event.target.value = '';
    if (!file) return;
    setRepoImporting(true);
    try {
      const data = JSON.parse(await file.text());
      const list = Array.isArray(data) ? data : (data.repositories || []);
      const normalized = list.map((item) => {
        const rawFull = String(item.full_name || '').trim();
        let owner = String(item.owner || '').trim();
        let name = String(item.name || '').trim();
        if (!owner && rawFull.includes('/')) {
          [owner, name] = [rawFull.slice(0, rawFull.indexOf('/')), rawFull.slice(rawFull.indexOf('/') + 1)];
        }
        if (owner && !name && rawFull.includes('/')) {
          name = rawFull.slice(rawFull.indexOf('/') + 1);
        }
        return { ...item, owner, name };
      });
      const valid = normalized.filter((item) => String(item.owner || '').trim() && String(item.name || '').trim());
      if (valid.length === 0) throw new Error('文件中没有可导入的仓库（需要 owner 和 name）');
      if (!(await dialog.confirm(`确认导入 ${valid.length} 个仓库？重复仓库由 GitHub 侧自动去重。`))) return;
      const results = [];
      // 分批提交（10 个/批），避免一次性并发几十个请求压垮后端与 GitHub 限流。
      const CHUNK = 10;
      for (let i = 0; i < valid.length; i += CHUNK) {
        const chunk = valid.slice(i, i + CHUNK);
        const chunkResults = await Promise.allSettled(chunk.map((item) => api('/api/github/repositories', {
          method: 'POST',
          headers: { 'Content-Type': 'application/json' },
          body: JSON.stringify({
            owner: String(item.owner || '').trim(),
            name: String(item.name || '').trim(),
            tags: Array.isArray(item.tags) ? item.tags : [],
            note: item.note || '',
            enabled: item.enabled !== false,
            notify_enabled: item.notify_enabled !== false,
          }),
        })));
        results.push(...chunkResults);
      }
      const imported = results.filter((result) => result.status === 'fulfilled').length;
      const failed = results.filter((result) => result.status === 'rejected').length;
      if (failed > 0) {
        toast.warning(`导入完成：成功 ${imported} 个，失败 ${failed} 个（已存在的仓库会跳过）`);
      } else {
        toast.success(`导入完成：成功 ${imported} 个`);
      }
      loadOverview();
    } catch (error) {
      toast.error(error.message || '导入仓库失败');
    } finally {
      setRepoImporting(false);
    }
  };

  const loadRepoDetails = useCallback(async (repoId = selectedRepo?.id) => {
    if (!repoId) return;
    setTrendsLoading(true);
    try {
      const [trendData, actionData, eventData, trafficData, contributorData, workflowData, branchData] = await Promise.all([
        api(`/api/github/repositories/${repoId}/trends?days=${rangeDays}`),
        api(`/api/github/repositories/${repoId}/actions/runs?limit=50`),
        api(`/api/github/repositories/${repoId}/events?limit=100`),
        api(`/api/github/repositories/${repoId}/traffic?limit=100`),
        api(`/api/github/repositories/${repoId}/contributors?limit=100`),
        api(`/api/github/repositories/${repoId}/actions/workflows`).catch(() => []),
        api(`/api/github/repositories/${repoId}/branches`).catch(() => []),
      ]);
      setTrends(trendData.snapshots || []);
      setActions(actionData || []);
      setEvents(eventData || []);
      setTraffic(trafficData || []);
      setContributors(contributorData || []);
      setWorkflows(workflowData || []);
      setBranches(branchData || []);
      setDetailsRepoId(String(repoId));
    } catch (error) {
      toast.error(error.message || '加载仓库详情失败');
    } finally {
      setTrendsLoading(false);
    }
  }, [api, rangeDays, selectedRepo?.id]);

  const loadActionJobs = useCallback(async (runId = selectedActionRunId, options = {}) => {
    if (!selectedRepo?.id || !runId) return;
    const showLoading = options.showLoading ?? actionJobsRef.current.length === 0;
    if (showLoading) setActionJobsLoading(true);
    try {
      const run = actions.find((item) => String(item.run_id) === String(runId));
      const params = new URLSearchParams();
      if (run?.workflow_name) params.set('workflow_name', run.workflow_name);
      if (run?.branch) params.set('branch', run.branch);
      if (run?.commit_sha) params.set('commit_sha', run.commit_sha);
      const detail = await api(`/api/github/repositories/${selectedRepo.id}/actions/runs/${runId}/jobs${params.toString() ? `?${params}` : ''}`);
      const jobs = Array.isArray(detail) ? detail : detail?.jobs || [];
      actionJobsRef.current = jobs;
      setActionJobs(jobs);
      setActionWorkflow(Array.isArray(detail) ? null : detail?.workflow || null);
    } catch (error) {
      if (showLoading) toast.error(error.message || '加载 Actions 流程失败');
      if (actionJobsRef.current.length === 0) {
        setActionJobs([]);
        setActionWorkflow(null);
      }
    } finally {
      if (showLoading) setActionJobsLoading(false);
    }
  }, [actions, api, selectedActionRunId, selectedRepo?.id]);

  useEffect(() => {
    loadOverview();
  }, [loadOverview]);

  useEffect(() => () => {
    if (actionCollapseTimerRef.current) window.clearTimeout(actionCollapseTimerRef.current);
  }, []);

  useEffect(() => {
    loadRepoDetails();
  }, [loadRepoDetails]);

  useEffect(() => {
    if (selectedActionRunId) void loadActionJobs(selectedActionRunId, { showLoading: actionJobsRef.current.length === 0 });
  }, [actions, loadActionJobs, selectedActionRunId]);

  useEffect(() => {
    const timer = window.setInterval(() => {
      if (document.hidden) return;
      void loadOverview();
      void loadRepoDetails();
    }, 60_000);
    return () => window.clearInterval(timer);
  }, [loadOverview, loadRepoDetails]);

  useEffect(() => {
    setWorkflows([]);
    setBranches([]);
    setDetailsRepoId(null);
    setDispatchForm({ workflowId: '', ref: '' });
    dispatchDefaultedRepoRef.current = null;
    setSelectedActionRunId(null);
    setCollapsingActionRunId(null);
    actionJobsRef.current = [];
    setActionJobs([]);
    setActionWorkflow(null);
  }, [selectedRepo?.id]);

  useEffect(() => {
    const source = new EventSource('/api/github/events/stream', { withCredentials: true });
    eventSourceRef.current = source;
    source.addEventListener('github', (event) => {
      try {
        const payload = JSON.parse(event.data);
        if (payload.kind === 'repository_refresh' || payload.kind === 'repository_actions_refresh') {
          if (String(payload.repository_id) === String(selectedRepo?.id)) {
            void Promise.all([loadOverview(), loadRepoDetails(payload.repository_id)]);
          }
          return;
        }
        setEvents((current) => [payload, ...current].slice(0, 100));
      } catch (error) {
        console.warn('Failed to parse GitHub event stream payload:', error);
      }
    });
    return () => {
      source.close();
      eventSourceRef.current = null;
    };
  }, [loadOverview, loadRepoDetails, selectedRepo?.id]);

  const createToken = async () => {
    if (!tokenForm.name.trim() || !tokenForm.token.trim()) {
      toast.warning('请填写 Token 名称和值');
      return;
    }
    setSaving(true);
    try {
      await api('/api/github/tokens', {
        method: 'POST',
        body: JSON.stringify(tokenForm),
      });
      toast.success('GitHub Token 已保存');
      setTokenForm({ name: '', token: '', type: 'fine_grained', default_token: false });
      await loadOverview();
    } catch (error) {
      toast.error(error.message || '保存 Token 失败');
    } finally {
      setSaving(false);
    }
  };

  const testToken = async (id) => {
    setTestingTokenId(String(id));
    try {
      const suffix = selectedRepo?.id ? `?repositoryId=${encodeURIComponent(selectedRepo.id)}&bind=true` : '';
      await api(`/api/github/tokens/${id}/test${suffix}`, { method: 'POST', body: '{}' });
      if (selectedRepo?.id) {
        await api(`/api/github/repositories/${selectedRepo.id}/refresh`, { method: 'POST', body: '{}' });
      }
      toast.success(selectedRepo?.id ? `Token 已检测并绑定到 ${selectedRepo.full_name}` : 'Token 基础权限检测完成');
      await loadOverview();
    } catch (error) {
      toast.error(error.message || 'Token 权限检测失败');
      await loadOverview();
    } finally {
      setTestingTokenId(null);
    }
  };

  const deleteToken = async (token) => {
    if (!confirmPress(`github-token:${token.id}`, `删除 Token「${token.name}」`)) return;
    try {
      await api(`/api/github/tokens/${token.id}`, { method: 'DELETE' });
      toast.success('Token 已删除');
      await loadOverview();
    } catch (error) {
      toast.error(error.message || '删除 Token 失败');
    }
  };

  const createRepository = async () => {
    if (!repoForm.url.trim()) {
      toast.warning('请粘贴 GitHub 仓库 URL 或 owner/repo');
      return;
    }
    setSaving(true);
    try {
      const payload = {
        ...repoForm,
        token_id: repoForm.token_id ? Number(repoForm.token_id) : null,
        collect_interval_seconds: Number(repoForm.collect_interval_seconds) || 900,
        retention_days: Number(repoForm.retention_days) || 90,
      };
      const repo = await api('/api/github/repositories', {
        method: 'POST',
        body: JSON.stringify(payload),
      });
      toast.success('仓库已添加，后台开始采集');
      setRepoForm({ url: '', token_id: '', collect_interval_seconds: 900, retention_days: 90, webhook_enabled: false });
      setRepoDialogOpen(false);
      setSelectedRepoId(repo.id);
      await loadOverview();
    } catch (error) {
      toast.error(error.message || '添加仓库失败');
    } finally {
      setSaving(false);
    }
  };

  const refreshRepository = async (id) => {
    setRefreshingRepositoryId(String(id));
    try {
      await api(`/api/github/repositories/${id}/refresh`, { method: 'POST', body: '{}' });
      toast.success('仓库刷新完成');
      await loadOverview();
      await loadRepoDetails(id);
    } catch (error) {
      toast.error(error.message || '刷新仓库失败');
    } finally {
      setRefreshingRepositoryId(null);
    }
  };

  const saveRepositoryOrder = async (nextRepositories) => {
    try {
      const orderedIds = nextRepositories.map((repo) => repo.id);
      const saved = await api('/api/github/repositories/reorder', {
        method: 'POST',
        body: JSON.stringify({ repository_ids: orderedIds }),
      });
      if (Array.isArray(saved)) setRepositories(saved);
    } catch (error) {
      toast.error(error.message || '仓库排序保存失败');
      await loadOverview();
    }
  };

  const handleRepositoryDragStart = (repo, event) => {
    setDraggedRepositoryId(String(repo.id));
    event.dataTransfer.effectAllowed = 'move';
    event.dataTransfer.setData('text/plain', String(repo.id));
  };

  const handleRepositoryDragOver = (event) => {
    if (!draggedRepositoryId) return;
    event.preventDefault();
    event.dataTransfer.dropEffect = 'move';
  };

  const handleRepositoryDrop = async (targetRepoId, event) => {
    event.preventDefault();
    const sourceId = draggedRepositoryId || event.dataTransfer.getData('text/plain');
    setDraggedRepositoryId(null);
    if (!sourceId || String(sourceId) === String(targetRepoId)) return;
    const fromIndex = repositories.findIndex((repo) => String(repo.id) === String(sourceId));
    const toIndex = repositories.findIndex((repo) => String(repo.id) === String(targetRepoId));
    if (fromIndex < 0 || toIndex < 0) return;
    const next = [...repositories];
    const [moved] = next.splice(fromIndex, 1);
    next.splice(toIndex, 0, moved);
    setRepositories(next);
    await saveRepositoryOrder(next);
  };

  const toggleActionRun = (run) => {
    const runId = String(run.run_id);
    if (actionCollapseTimerRef.current) {
      window.clearTimeout(actionCollapseTimerRef.current);
      actionCollapseTimerRef.current = null;
    }
    if (String(selectedActionRunId) === runId) {
      setCollapsingActionRunId(runId);
      setSelectedActionRunId(null);
      actionCollapseTimerRef.current = window.setTimeout(() => {
        setCollapsingActionRunId((current) => (current === runId ? null : current));
        actionCollapseTimerRef.current = null;
      }, 220);
      return;
    }
    setCollapsingActionRunId(null);
    actionJobsRef.current = [];
    setActionJobs([]);
    setActionWorkflow(null);
    setSelectedActionRunId(run.run_id);
  };

  const updateRepositoryToken = async (value) => {
    if (!selectedRepo?.id) return;
    setSaving(true);
    try {
      await api(`/api/github/repositories/${selectedRepo.id}`, {
        method: 'PATCH',
        body: JSON.stringify({ token_id: value ? Number(value) : null }),
      });
      await api(`/api/github/repositories/${selectedRepo.id}/refresh`, { method: 'POST', body: '{}' });
      toast.success('仓库访问凭据已更新');
      await loadOverview();
      await loadRepoDetails(selectedRepo.id);
    } catch (error) {
      toast.error(error.message || '更新仓库访问凭据失败');
    } finally {
      setSaving(false);
    }
  };

  const deleteRepository = async (id) => {
    const repo = repositories.find((item) => item.id === id);
    if (!confirmPress(`github-repo:${id}`, `删除仓库「${repo?.full_name || String(id)}」`)) return;
    try {
      await api(`/api/github/repositories/${id}?clean=false`, { method: 'DELETE' });
      toast.success('仓库已删除');
      setSelectedRepoId(null);
      await loadOverview();
    } catch (error) {
      toast.error(error.message || '删除仓库失败');
    }
  };

  const runCollector = async () => {
    try {
      await api('/api/github/collector/run', { method: 'POST', body: '{}' });
      toast.success('后台采集已执行');
      await loadOverview();
      await loadRepoDetails();
    } catch (error) {
      toast.error(error.message || '执行采集失败');
    }
  };

  const saveSettings = async () => {
    setSaving(true);
    try {
      const next = await api('/api/github/settings', {
        method: 'PUT',
        body: JSON.stringify(settings),
      });
      setSettings(next);
      toast.success('GitHub 设置已保存');
    } catch (error) {
      toast.error(error.message || '保存设置失败');
    } finally {
      setSaving(false);
    }
  };

  const actionOperation = async (runId, operation) => {
    if (!selectedRepo) return;
    try {
      await api(`/api/github/repositories/${selectedRepo.id}/actions/runs/${runId}/${operation}`, {
        method: 'POST',
        body: '{}',
      });
      toast.success('Actions 操作已提交');
      await loadRepoDetails(selectedRepo.id);
    } catch (error) {
      toast.error(error.message || 'Actions 操作失败');
      await loadOverview();
    }
  };

  const dispatchWorkflow = async () => {
    if (!selectedRepo || !dispatchForm.workflowId.trim()) {
      toast.warning('请填写 workflow ID 或文件名');
      return;
    }
    try {
      await api(`/api/github/repositories/${selectedRepo.id}/actions/workflows/${encodeURIComponent(dispatchForm.workflowId.trim())}/dispatch`, {
        method: 'POST',
        body: JSON.stringify({ ref: dispatchForm.ref || selectedRepo.default_branch }),
      });
      toast.success('Workflow dispatch 已提交');
    } catch (error) {
      toast.error(error.message || '触发 Workflow 失败');
      await loadOverview();
    }
  };

  const configureWebhook = async () => {
    if (!selectedRepo) return;
    setSaving(true);
    try {
      const result = await api(`/api/github/repositories/${selectedRepo.id}/webhook/configure`, {
        method: 'POST',
        body: JSON.stringify({ payload_url: `${window.location.origin}/api/github/webhook/${selectedRepo.id}` }),
      });
      toast.success(result.created ? 'GitHub Webhook 已自动创建' : 'GitHub Webhook 已自动更新');
      await loadOverview();
    } catch (error) {
      toast.error(error.message || '自动配置 Webhook 失败：请确认 Token 的 Resource owner、仓库范围和 Webhooks: write 权限');
    } finally {
      setSaving(false);
    }
  };

  const cleanupGitHubHistory = async () => {
    const days = Math.max(
      1,
      Number(historyRetentionDays)
      || Number(historyScope === 'current' ? selectedRepo?.retention_days : settings?.default_retention_days)
      || 90
    );
    const confirmed = await dialog.confirm({
      title: '确认清理 GitHub 历史',
      message: `确定清理 ${historyScopeLabel} ${days} 天前的 GitHub 历史记录吗？操作会同时压缩数据库文件，且不可恢复。`,
      confirmText: '清理',
      confirmClass: '!bg-kumo-danger !text-white',
    });
    if (!confirmed) return;
    setMaintenanceAction('cleanup-history');
    try {
      const params = new URLSearchParams({ days: String(days) });
      if (historyScope === 'current' && selectedRepo?.id) params.set('repositoryId', String(selectedRepo.id));
      const result = await api(`/api/github/history?${params.toString()}`, { method: 'DELETE' });
      const totalDeleted = Object.values(result || {}).reduce((sum, value) => sum + (Number(value) || 0), 0);
      toast.success(`GitHub 历史已清理，删除 ${totalDeleted} 条记录`);
      await loadOverview();
      if (selectedRepo?.id) await loadRepoDetails(selectedRepo.id);
    } catch (error) {
      toast.error(error.message || '清理 GitHub 历史失败');
    } finally {
      setMaintenanceAction('');
    }
  };

  const compactGitHubHistory = async () => {
    const confirmed = await dialog.confirm({
      title: '确认压缩 GitHub 历史',
      message: `确定压缩 ${historyScopeLabel} 已有的 GitHub 事件和 Webhook Payload 吗？操作会把旧的大 JSON 改写为摘要并压缩数据库文件。`,
      confirmText: '压缩',
      confirmClass: '!bg-kumo-warning !text-kumo-strong',
    });
    if (!confirmed) return;
    setMaintenanceAction('compact-history');
    try {
      const params = new URLSearchParams();
      if (historyScope === 'current' && selectedRepo?.id) params.set('repositoryId', String(selectedRepo.id));
      const result = await api(`/api/github/history/compact${params.toString() ? `?${params.toString()}` : ''}`, { method: 'POST', body: '{}' });
      const updatedEvents = Number(result?.github_events) || 0;
      const updatedDeliveries = Number(result?.github_webhook_deliveries) || 0;
      const savedBytes = Number(result?.bytes_saved) || 0;
      const savedMB = savedBytes > 0 ? `${(savedBytes / 1024 / 1024).toFixed(2)} MB` : '0 MB';
      toast.success(`已压缩 ${updatedEvents + updatedDeliveries} 条 GitHub 记录，约节省 ${savedMB}`);
      await loadOverview();
      if (selectedRepo?.id) await loadRepoDetails(selectedRepo.id);
    } catch (error) {
      toast.error(error.message || '压缩 GitHub 历史失败');
    } finally {
      setMaintenanceAction('');
    }
  };

  const chartData = useMemo(() => {
    const points = trends.map((point) => ({
      ts: parseTimestamp(point.collected_at),
      stars: Number(point.stars) || 0,
      issues: Number(point.open_issues) || 0,
      prs: Number(point.open_pull_requests) || 0,
      commits: Number(point.commit_count) || 0,
      successRate: point.actions_total ? Math.round((Number(point.actions_success || 0) / Number(point.actions_total || 1)) * 100) : 0,
    }));
    return [
      { name: 'Stars', color: ChartPalette.semantic('Attention', isDarkMode), data: points.map((p) => [p.ts, p.stars]) },
      { name: 'Issues', color: ChartPalette.semantic('Warning', isDarkMode), data: points.map((p) => [p.ts, p.issues]) },
      { name: 'PR', color: ChartPalette.semantic('Info', isDarkMode), data: points.map((p) => [p.ts, p.prs]) },
      { name: '提交', color: ChartPalette.semantic('Success', isDarkMode), data: points.map((p) => [p.ts, p.commits]) },
      { name: 'Actions 成功率', color: ChartPalette.categorical(3, isDarkMode), data: points.map((p) => [p.ts, p.successRate]) },
    ];
  }, [isDarkMode, trends]);

  const repoOptions = repositories.map((repo) => ({ value: String(repo.id), label: repo.full_name }));
  const tokenOptions = [{ value: '', label: '默认/公开访问' }, ...tokens.map((token) => ({ value: String(token.id), label: token.name }))];
  const historyScopeOptions = [
    { value: 'all', label: '全部仓库' },
    ...(selectedRepo?.id ? [{ value: 'current', label: `当前仓库：${selectedRepo.full_name}` }] : []),
  ];
  const historyScopeLabel = historyScope === 'current' && selectedRepo ? `当前仓库 ${selectedRepo.full_name}` : '全部 GitHub 仓库';
  const workflowOptions = [
    { value: '', label: workflows.length > 0 ? '选择 Workflow' : '未发现 Workflow' },
    ...workflows
      .filter((workflow) => !workflow.state || workflow.state === 'active')
      .map((workflow) => ({
        value: String(workflow.id || workflow.path),
        label: workflow.name ? `${workflow.name} (${workflow.path})` : workflow.path,
      })),
  ];
  const branchOptions = useMemo(() => {
    const branchNames = branches.map((branch) => branch.name).filter(Boolean);
    const availableBranchNames = selectedRepo?.default_branch && !branchNames.includes(selectedRepo.default_branch)
      ? [selectedRepo.default_branch, ...branchNames]
      : branchNames;
    return availableBranchNames.map((name) => ({ value: name, label: name }));
  }, [branches, selectedRepo?.default_branch]);

  useEffect(() => {
    if (branchOptions.length === 0) return;
    setDispatchForm((current) => ({
      ...current,
      ref: branchOptions.some((branch) => branch.value === current.ref) ? current.ref : branchOptions[0].value,
    }));
  }, [selectedRepo?.id, branchOptions]);

  useEffect(() => {
    const fallback = selectedRepo?.retention_days || settings?.default_retention_days || 90;
    setHistoryRetentionDays(String(fallback));
  }, [selectedRepo?.id, selectedRepo?.retention_days, settings?.default_retention_days]);

  useEffect(() => {
    if (String(detailsRepoId) !== String(selectedRepo?.id)) return;
    if (dispatchDefaultedRepoRef.current === String(selectedRepo?.id)) return;
    const lastSuccessfulRun = actions.find((run) => String(run.conclusion || '').toLowerCase() === 'success');
    if (!lastSuccessfulRun) return;
    const runWorkflowName = String(lastSuccessfulRun.workflow_name || lastSuccessfulRun.display_title || '').toLowerCase();
    const workflow = workflows.find((item) => (
      String(item.name || '').toLowerCase() === runWorkflowName ||
      String(item.path || '').toLowerCase() === runWorkflowName
    ));
    if (!workflow) return;
    const workflowId = String(workflow.id || workflow.path);
    const ref = branchOptions.some((branch) => branch.value === lastSuccessfulRun.branch)
      ? lastSuccessfulRun.branch
      : branchOptions[0]?.value || selectedRepo?.default_branch || '';
    dispatchDefaultedRepoRef.current = String(selectedRepo?.id);
    setDispatchForm((current) => (
      current.workflowId === workflowId && current.ref === ref ? current : { workflowId, ref }
    ));
  }, [actions, branchOptions, detailsRepoId, selectedRepo?.default_branch, selectedRepo?.id, workflows]);

  return (
    <div className="flex min-h-full w-full min-w-0 flex-col gap-4">
      <div className={`${stickyTabsBaseClass} justify-between gap-2 border-b border-kumo-line [&>*]:min-w-0`}>
        <Tabs
          {...MODULE_TABS_PROPS}
          value={activeTab}
          onValueChange={(value) => setActiveTab(String(value))}
          tabs={tabs}
        />
        <TabBarOverflowActions
          items={[
            {
              key: 'collect',
              label: '立即采集',
              icon: <Play className="h-3.5 w-3.5" />,
              onClick: runCollector,
              variant: 'primary',
            },
          ]}
        />
      </div>

      {activeTab === 'repositories' && (
        <div className="min-w-0">
          <LayerCard className="p-0 shadow-none">
            <LayerCard.Secondary className={sectionCardHeaderClass}>
              <div className="flex min-w-0 items-center gap-2">
                <GitBranch className="h-4 w-4 text-brand" />
                <Text variant="body" size="sm" bold>仓库列表</Text>
                <Badge variant="neutral">{repositories.length} 个仓库</Badge>
                <Badge variant={collector?.running ? 'success' : 'neutral'}>
                  {collector?.running ? '后台采集中' : '采集器待命'}
                </Badge>
              </div>
              <div className="flex shrink-0 items-center gap-2">
                <Input
                  ref={repoImportInputRef}
                  type="file"
                  accept=".json,application/json"
                  aria-label="导入仓库 JSON"
                  className="hidden"
                  onChange={importRepositoriesFromFile}
                />
                <Toolbar size="sm" aria-label="导出导入仓库" className="shrink-0">
                  <Toolbar.Button
                    onClick={exportRepositories}
                    disabled={repositories.length === 0}
                    aria-label="导出仓库列表"
                    title="导出仓库列表"
                    icon={<Upload className="h-3.5 w-3.5" />}
                  >
                    <span className="hidden cq-sm:inline">导出</span>
                  </Toolbar.Button>
                  <Toolbar.Button
                    onClick={() => repoImportInputRef.current?.click()}
                    disabled={repoImporting}
                    aria-label="导入仓库列表"
                    title="导入仓库列表"
                    icon={<Download className="h-3.5 w-3.5" />}
                  >
                    <span className="hidden cq-sm:inline">导入</span>
                  </Toolbar.Button>
                </Toolbar>
                <Button size="sm" variant="primary" icon={<Plus className="h-3.5 w-3.5" />} onClick={() => setRepoDialogOpen(true)}>添加仓库</Button>
              </div>
            </LayerCard.Secondary>
            <LayerCard.Primary className="p-0">
            {repositories.length === 0 ? (
              <FillEmpty title="暂无 GitHub 仓库" description="先添加仓库" />
            ) : (
              <div className="grid items-start gap-3 p-4 cq-sm:grid-cols-2 cq-xl:grid-cols-3 cq-2xl:grid-cols-4">
                {repositories.map((repo) => {
                  const isSelected = String(repo.id) === String(selectedRepo?.id);
                  const actionStatus = repo.latest_action_conclusion || repo.latest_action_status || '未知';
                  const collectStatus = repo.last_status || 'pending';
                  const actionStartedAt = repo.latest_action_started_at || repo.latest_action_created_at;
                  const actionDuration = formatActionDuration(actionStartedAt, repo.latest_action_updated_at, currentTime);
                  return (
                    <div
                      key={repo.id}
                      draggable
                      onDragStart={(event) => handleRepositoryDragStart(repo, event)}
                      onDragOver={handleRepositoryDragOver}
                      onDrop={(event) => handleRepositoryDrop(repo.id, event)}
                      onDragEnd={() => setDraggedRepositoryId(null)}
                      className={`min-w-0 cursor-move rounded-lg border border-kumo-line bg-kumo-base p-0 shadow-none transition-[opacity,transform,border-color] duration-160 ${isSelected ? 'ring-1 ring-brand/50' : ''} ${draggedRepositoryId === String(repo.id) ? 'scale-[0.99] opacity-50' : ''}`}
                    >
                      <div className="grid gap-3 p-3">
                        <div className="flex min-w-0 items-start justify-between gap-2">
                          <Button type="button" variant="ghost" className="h-auto min-w-0 flex-1 !items-start !justify-start !px-0 text-left" onClick={() => setSelectedRepoId(repo.id)}>
                            <span className="block min-w-0">
                              <span className="block truncate text-sm font-semibold text-kumo-strong">{repo.full_name}</span>
                              <span className="block truncate text-[11px] text-kumo-subtle">{formatGitHubRepositoryDescription(repo.description, repo.html_url)}</span>
                            </span>
                          </Button>
                          <div className="flex shrink-0 items-center gap-1">
                            <Badge variant={repo.private ? 'warning' : 'success'}>{repo.private ? '私有' : '公开'}</Badge>
                            <Badge variant={repo.owned_by_token || repo.can_operate_actions ? 'success' : 'neutral'}>
                              {repo.owned_by_token ? '本人仓库' : repo.can_operate_actions ? '有写权限' : repo.authenticated ? '只读权限' : '未认证'}
                            </Badge>
                          </div>
                        </div>

                        <div className="grid grid-cols-3 gap-2">
                          <RepositoryStat label="Stars" value={formatNumber(repo.stars)} />
                          <RepositoryStat label="Forks" value={formatNumber(repo.forks)} />
                          <RepositoryStat label="Issues" value={formatNumber(repo.open_issues)} />
                        </div>

                        <div className="grid gap-2 text-[11px] text-kumo-subtle">
                          <div className="flex min-w-0 items-center justify-between gap-2">
                            <span>Actions</span>
                            <div className="flex min-w-0 items-center justify-end gap-2">
                              {actionStartedAt && <span className="min-w-0 truncate" title={formatDateTime(actionStartedAt)}>{formatDateTime(actionStartedAt)}</span>}
                              <Badge variant={statusTone(actionStatus)}>{actionStartedAt ? `${statusLabel(actionStatus)} · ${actionDuration}` : statusLabel(actionStatus)}</Badge>
                            </div>
                          </div>
                          <div className="flex min-w-0 items-center justify-between gap-2">
                            <span>采集</span>
                            <div className="flex min-w-0 items-center justify-end gap-2">
                              <span className="min-w-0 truncate">{formatDateTime(repo.last_collected_at)}</span>
                              <Badge variant={statusTone(collectStatus)}>{statusLabel(collectStatus)}</Badge>
                            </div>
                          </div>
                        </div>

                        <div className="flex items-center justify-end gap-1 border-t border-kumo-line pt-2">
                          {repo.html_url && <Button size="sm" shape="square" variant="secondary" icon={<ExternalLink className="h-3.5 w-3.5" />} onClick={(event) => { event.stopPropagation(); window.open(repo.html_url, '_blank'); }} aria-label="打开 GitHub" title="打开 GitHub" />}
                          <Button size="sm" shape="square" variant="secondary" icon={<RefreshCw className="h-3.5 w-3.5" />} onClick={(event) => { event.stopPropagation(); refreshRepository(repo.id); }} loading={refreshingRepositoryId === String(repo.id)} aria-label="刷新仓库" title="刷新仓库" />
                          <Button size="sm" shape="square" variant={isArmed(`github-repo:${repo.id}`) ? 'destructive' : 'secondary-destructive'} icon={<Trash className="h-3.5 w-3.5" />} onClick={(event) => { event.stopPropagation(); deleteRepository(repo.id); }} aria-label="删除仓库" title="删除仓库" />
                        </div>
                      </div>
                    </div>
                  );
                })}
              </div>
            )}
            </LayerCard.Primary>
          </LayerCard>
        </div>
      )}

      {selectedRepo && ['actions', 'trends', 'events'].includes(activeTab) && (
        <div className="flex min-w-0 flex-col gap-4">
          <LayerCard className="p-0 shadow-none">
            <LayerCard.Secondary className={sectionCardHeaderClass}>
              <div className="flex min-w-0 items-center gap-2">
                <GitHubBrand className="h-4 w-4 text-brand" />
                <Text variant="body" size="sm" bold truncate>{selectedRepo.full_name}</Text>
              </div>
              <div className="flex flex-wrap items-end gap-2">
                <Select
                  size="sm"
                  aria-label="仓库访问凭据"
                  value={selectedRepo.token_id ? String(selectedRepo.token_id) : ''}
                  onValueChange={updateRepositoryToken}
                  items={tokenOptions}
                  disabled={saving}
                />
                <Select
                  size="sm"
                  aria-label="选择 GitHub 仓库"
                  value={String(selectedRepo.id)}
                  onValueChange={setSelectedRepoId}
                  items={repoOptions}
                />
                <Button size="sm" variant="secondary" icon={<RefreshCw className="h-3.5 w-3.5" />} onClick={() => refreshRepository(selectedRepo.id)} loading={refreshingRepositoryId === String(selectedRepo.id)}>刷新仓库</Button>
              </div>
            </LayerCard.Secondary>
            <LayerCard.Primary className="p-4">
              <Grid variant="6up" gap="sm" className="items-start cq-xl:grid-cols-5">
                <RepositoryMetric icon={<Star className="h-3.5 w-3.5" />} label="Stars" value={formatNumber(selectedRepo.stars)} />
                <RepositoryMetric icon={<GitBranch className="h-3.5 w-3.5" />} label="Forks" value={formatNumber(selectedRepo.forks)} />
                <RepositoryMetric icon={<Activity className="h-3.5 w-3.5" />} label="Issues / PR" value={`${formatNumber(selectedRepo.open_issues)} / ${formatNumber(selectedRepo.open_pull_requests)}`} />
                <RepositoryMetric icon={<Rocket className="h-3.5 w-3.5" />} label="Latest Release" value={selectedRepo.latest_release || '-'} />
                <RepositoryMetric icon={<Clock className="h-3.5 w-3.5" />} label="Rate Limit" value={selectedRepo.rate_limit_remaining ?? '-'} detail={selectedRepo.rate_limit_reset ? formatResetCountdown(selectedRepo.rate_limit_reset, currentTime) : ''} />
              </Grid>
            </LayerCard.Primary>
          </LayerCard>

          {activeTab === 'actions' && (
            <LayerCard className="p-0 shadow-none">
              <LayerCard.Secondary className={sectionCardHeaderClass}>
                <div className="flex items-center gap-2">
                  <Activity className="h-4 w-4 text-brand" />
                  <Text variant="body" size="sm" bold>Actions 活动</Text>
                </div>
                {canAttemptActionOperations ? (
                  <div className="flex flex-wrap items-center gap-2">
                    <Select size="sm" className="w-64" aria-label="选择 Workflow" value={dispatchForm.workflowId} onValueChange={(value) => setDispatchForm((p) => ({ ...p, workflowId: value }))} items={workflowOptions} disabled={workflowOptions.length <= 1} />
                    <Select size="sm" className="w-40" aria-label="选择触发分支" value={dispatchForm.ref} onValueChange={(value) => setDispatchForm((p) => ({ ...p, ref: value }))} items={branchOptions} disabled={branchOptions.length === 0} />
                    <Button size="sm" variant="primary" icon={<Play className="h-3.5 w-3.5" />} onClick={dispatchWorkflow}>触发</Button>
                  </div>
                ) : (
                  <Badge variant="neutral">{selectedRepo.authenticated ? '当前 Token 无写权限' : '未配置 Token'}</Badge>
                )}
              </LayerCard.Secondary>
              <LayerCard.Primary className="p-0">
              {actions.length === 0 ? <FillEmpty title="暂无 Actions 记录" description="刷新后显示" /> : (
                <DataTableFrame variant="embedded" density="compact" className="min-w-0 overflow-x-auto overflow-y-visible scrollbar-thin">
                  <AppTable layout="fixed" widths={GITHUB_ACTIONS_TABLE_WIDTHS}>
                    <colgroup>
                      <col style={{ width: GITHUB_ACTIONS_TABLE_WIDTHS[0] }} />
                      <col style={{ width: GITHUB_ACTIONS_TABLE_WIDTHS[1] }} />
                      <col style={{ width: GITHUB_ACTIONS_TABLE_WIDTHS[2] }} />
                      <col style={{ width: GITHUB_ACTIONS_TABLE_WIDTHS[3] }} />
                      <col style={{ width: GITHUB_ACTIONS_TABLE_WIDTHS[4] }} />
                      <col style={{ width: GITHUB_ACTIONS_TABLE_WIDTHS[5] }} />
                    </colgroup>
                    <Table.Header sticky variant="compact">
                      <Table.Row><Table.Head className="align-middle text-center">状态</Table.Head><Table.Head>Workflow</Table.Head><Table.Head className="align-middle">提交说明</Table.Head><Table.Head className="align-middle">分支</Table.Head><Table.Head className="align-middle text-center">时间</Table.Head><Table.Head className="app-table-action align-middle">操作</Table.Head></Table.Row>
                    </Table.Header>
                    <Table.Body>
                      {actions.map((run) => {
                        const isActionExpanded = String(selectedActionRunId) === String(run.run_id);
                        const isActionVisible = isActionExpanded || String(collapsingActionRunId) === String(run.run_id);
                        return (
                        <React.Fragment key={run.run_id}>
                        <Table.Row className="cursor-pointer" onClick={() => toggleActionRun(run)}>
                          <Table.Cell className="align-middle text-center"><Badge variant={statusTone(run.conclusion || run.status)}>{`${statusLabel(run.conclusion || run.status)} · ${formatActionDuration(run.run_started_at || run.created_at, run.updated_at, currentTime)}`}</Badge></Table.Cell>
                          <Table.Cell><div className="min-w-0 truncate font-semibold text-kumo-strong" title={run.workflow_name || String(run.run_id)}>{run.workflow_name || run.run_id}</div><div className="min-w-0 truncate text-[11px] text-kumo-subtle" title={`${run.actor || '-'} · ${String(run.commit_sha || '').slice(0, 8)}`}>{run.actor} · {String(run.commit_sha || '').slice(0, 8)}</div></Table.Cell>
                          <Table.Cell className="align-middle"><div className="truncate text-sm leading-6 text-kumo-strong" title={run.commit_message || run.display_title || ''}>{run.commit_message || run.display_title || '暂无提交说明'}</div></Table.Cell>
                          <Table.Cell className="align-middle"><div className="min-w-0 truncate" title={run.branch || '-'}>{run.branch || '-'}</div></Table.Cell>
                          <Table.Cell className="align-middle text-center"><div className="min-w-0 truncate text-sm leading-6 text-kumo-strong" title={formatDateTime(run.run_started_at || run.created_at)}>{formatDateTime(run.run_started_at || run.created_at)}</div></Table.Cell>
                          <Table.Cell className="align-middle text-center">
                            <div className="flex justify-center gap-1">
                              {run.html_url && <Button size="sm" shape="square" variant="secondary" icon={<ExternalLink className="h-3.5 w-3.5" />} onClick={(event) => { event.stopPropagation(); window.open(run.html_url, '_blank'); }} aria-label="打开 Actions" title="打开 Actions" />}
                              {canAttemptActionOperations && (
                                <>
                                  <Button size="sm" shape="square" variant="secondary" icon={<RefreshCw className="h-3.5 w-3.5" />} onClick={(event) => { event.stopPropagation(); actionOperation(run.run_id, 'rerun'); }} aria-label="重新运行" title="重新运行" />
                                  <Button size="sm" shape="square" variant="secondary" icon={<Check className="h-3.5 w-3.5" />} onClick={(event) => { event.stopPropagation(); actionOperation(run.run_id, 'rerun-failed-jobs'); }} aria-label="重跑失败任务" title="重跑失败任务" />
                                  <Button size="sm" shape="square" variant="secondary-destructive" icon={<X className="h-3.5 w-3.5" />} onClick={(event) => { event.stopPropagation(); actionOperation(run.run_id, 'cancel'); }} aria-label="取消" title="取消" />
                                </>
                              )}
                            </div>
                          </Table.Cell>
                        </Table.Row>
                        {isActionVisible && (
                          <Table.Row>
                            <Table.Cell colSpan={6} className="p-0">
                              <AnimatedCollapse open={isActionExpanded}>
                              <div className="bg-kumo-recessed/10 px-3 pb-3 pt-2">
                                {actionJobsLoading ? (
                                  <ActionFlowPlaceholder />
                                ) : actionJobs.length === 0 ? (
                                  <div className="flex h-[300px] items-center justify-center rounded-md border border-kumo-line bg-kumo-base">
                                    <Text variant="secondary" size="sm">暂无 Job 进度数据</Text>
                                  </div>
                                ) : (
                                  (() => {
                                    return <ActionWorkflowCanvas workflow={actionWorkflow} jobs={actionJobs} now={currentTime} />;
                                  })()
                                )}
                              </div>
                              </AnimatedCollapse>
                            </Table.Cell>
                          </Table.Row>
                        )}
                        </React.Fragment>
                      );
                      })}
                    </Table.Body>
                  </AppTable>
                </DataTableFrame>
              )}
              </LayerCard.Primary>
            </LayerCard>
          )}

          {activeTab === 'trends' && (
            <div className="grid items-start gap-4 cq-xl:grid-cols-[minmax(0,1.5fr)_minmax(20rem,0.7fr)]">
              <LayerCard className="p-0 shadow-none">
                <LayerCard.Secondary className={sectionCardHeaderClass}>
                  <div className="flex items-center gap-2">
                    <TrendingUp className="h-4 w-4 text-brand" />
                    <Text variant="body" size="sm" bold>仓库趋势</Text>
                  </div>
                  <Select size="sm" aria-label="趋势时间范围" value={rangeDays} onValueChange={setRangeDays} items={rangeOptions} />
                </LayerCard.Secondary>
                <LayerCard.Primary className="p-4">
                {trendsLoading && trends.length < 2 ? (
                  <ChartWarmupSkeleton height={320} bars={10} />
                ) : trends.length >= 2 ? (
                  <ChartBoundaryBox>
                    {(tooltipBoundary) => (
                      <SiteFontTimeseriesChart
                        echarts={echarts}
                        isDarkMode={isDarkMode}
                        type="line"
                        data={chartData}
                        height={320}
                        loading={trendsLoading}
                        xAxisName="时间"
                        yAxisName="指标"
                        xAxisTickCount={4}
                        xAxisTickFormat={(value) => new Date(value).toLocaleDateString()}
                        yAxisTickFormat={(value) => `${Math.round(value)}`}
                        tooltipValueFormat={(value) => `${Math.round(value)}`}
                        tooltipBoundary={tooltipBoundary ?? undefined}
                        tooltipFollowCursor="x"
                        ariaDescription="GitHub 仓库趋势"
                      />
                    )}
                  </ChartBoundaryBox>
                ) : <FillEmpty title="趋势数据不足" description="至少两次采集后显示" />}
                </LayerCard.Primary>
              </LayerCard>
              <LayerCard className="self-start p-0 shadow-none">
                <LayerCard.Secondary className={sectionCardHeaderClass}>
                  <Users className="h-4 w-4 text-brand" />
                  <Text variant="body" size="sm" bold>流量与贡献者</Text>
                </LayerCard.Secondary>
                <LayerCard.Primary className="grid content-start gap-3 p-4">
                  <RepositoryMetric label="访问量" value={formatNumber(traffic[0]?.views)} detail={`唯一访客 ${formatNumber(traffic[0]?.view_uniques)}`} />
                  <RepositoryMetric label="克隆量" value={formatNumber(traffic[0]?.clones)} detail={`唯一克隆 ${formatNumber(traffic[0]?.clone_uniques)}`} />
                  <RepositoryMetric label="贡献者" value={formatNumber(contributors.length)} detail={contributors.slice(0, 3).map((item) => item.login).join(', ') || '暂无贡献者数据'} />
                </LayerCard.Primary>
              </LayerCard>
            </div>
          )}

          {activeTab === 'events' && (
            <div className="grid items-start gap-4 cq-xl:grid-cols-[minmax(0,1.2fr)_minmax(20rem,0.8fr)]">
              <LayerCard className="p-0 shadow-none">
                <LayerCard.Secondary className={sectionCardHeaderClass}>
                  <Bell className="h-4 w-4 text-brand" />
                  <Text variant="body" size="sm" bold>事件与通知源</Text>
                </LayerCard.Secondary>
                <LayerCard.Primary className="p-0">
                {events.length === 0 ? <FillEmpty title="暂无 GitHub 事件" description="等待 Webhook 或采集" /> : (
                  <DataTableFrame variant="embedded" density="compact" className="min-w-0 overflow-x-auto overflow-y-visible scrollbar-thin">
                    <AppTable layout="fixed" widths={GITHUB_EVENTS_TABLE_WIDTHS}>
                      <colgroup>
                        <col style={{ width: GITHUB_EVENTS_TABLE_WIDTHS[0] }} />
                        <col style={{ width: GITHUB_EVENTS_TABLE_WIDTHS[1] }} />
                        <col style={{ width: GITHUB_EVENTS_TABLE_WIDTHS[2] }} />
                        <col style={{ width: GITHUB_EVENTS_TABLE_WIDTHS[3] }} />
                      </colgroup>
                      <Table.Header sticky variant="compact">
                        <Table.Row><Table.Head>事件</Table.Head><Table.Head className="align-middle text-center">等级</Table.Head><Table.Head className="align-middle text-center">来源</Table.Head><Table.Head className="align-middle text-center">时间</Table.Head></Table.Row>
                      </Table.Header>
                      <Table.Body>
                        {events.map((event, index) => (
                          <Table.Row key={event.id || `${event.event_type}-${index}`}>
                            <Table.Cell><div className="font-semibold text-kumo-strong">{event.title || event.event_type}</div><div className="max-w-2xl truncate text-[11px] text-kumo-subtle">{event.message}</div></Table.Cell>
                            <Table.Cell className="align-middle text-center"><Badge variant={statusTone(event.severity)}>{statusLabel(event.severity)}</Badge></Table.Cell>
                            <Table.Cell className="align-middle text-center">{event.source || 'stream'}</Table.Cell>
                            <Table.Cell className="align-middle text-center text-[11px] text-kumo-subtle">{formatDateTime(event.created_at)}</Table.Cell>
                          </Table.Row>
                        ))}
                      </Table.Body>
                    </AppTable>
                  </DataTableFrame>
                )}
                </LayerCard.Primary>
              </LayerCard>
              <LayerCard className="self-start p-0 shadow-none">
                <LayerCard.Secondary className={sectionCardHeaderClass}>
                  <div className="flex items-center gap-2">
                    <Key className="h-4 w-4 text-brand" />
                    <Text variant="body" size="sm" bold>Webhook 配置</Text>
                  </div>
                  <Button size="sm" variant="primary" icon={<Save className="h-3.5 w-3.5" />} onClick={configureWebhook} loading={saving}>自动配置</Button>
                </LayerCard.Secondary>
                <LayerCard.Primary className="grid content-start gap-4 p-4">
                  <div className="grid gap-1">
                    <Text variant="secondary" size="xs">Payload URL</Text>
                    <ClipboardText size="sm" text={`${window.location.origin}/api/github/webhook/${selectedRepo.id}`} />
                  </div>
                  <div className="grid gap-1">
                    <Text variant="secondary" size="xs">Secret</Text>
                    <ClipboardText size="sm" text={selectedRepo.webhook_secret || '-'} />
                  </div>
                  <Text variant="secondary" size="xs">选择 application/json，启用 workflow_run、release、issues、pull_request、star 和 ping 事件。</Text>
                </LayerCard.Primary>
              </LayerCard>
            </div>
          )}
        </div>
      )}

      {activeTab === 'public-pages' && (
        <GitHubPublicPagesPanel repositories={repositories} />
      )}

      {activeTab === 'settings' && (
        <div className="grid items-start gap-4 cq-xl:grid-cols-2">
          <LayerCard className="self-start p-0 shadow-none">
            <LayerCard.Secondary className={sectionCardHeaderClass}>
              <div className="flex min-w-0 items-center gap-2">
                <Key className="h-4 w-4 text-brand" />
                <Text variant="body" size="sm" bold>GitHub Token</Text>
              </div>
              <Button
                size="sm"
                variant="secondary"
                icon={<ExternalLink className="h-3.5 w-3.5" />}
                onClick={() => window.open(fineGrainedTokenURL(selectedRepo?.owner || ''), '_blank', 'noopener,noreferrer')}
              >
                打开 GitHub 创建页
              </Button>
            </LayerCard.Secondary>
            <LayerCard.Primary className="grid gap-3 p-4">
              <Text variant="secondary" size="xs">
                组织仓库请将 Resource owner 设为仓库所属组织，并等待组织审批；仓库 Webhook 使用仓库级 Webhooks: read/write，无需组织级权限。
              </Text>
              <Input size="sm" label="Token 名称" value={tokenForm.name} onChange={(e) => setTokenForm((p) => ({ ...p, name: e.target.value }))} placeholder="生产账号" />
              <Input size="sm" label="Token" value={tokenForm.token} onChange={(e) => setTokenForm((p) => ({ ...p, token: e.target.value }))} placeholder="github_pat_..." autoComplete="off" spellCheck={false} className="font-mono" />
              <Grid variant="2up" gap="sm">
                <Select size="sm" label="Token 类型" value={tokenForm.type} onValueChange={(value) => setTokenForm((p) => ({ ...p, type: value }))} items={tokenTypeOptions} />
                <div className="flex h-full items-end">
                  <Switch size="sm" label="设为默认" controlFirst={false} checked={tokenForm.default_token} onCheckedChange={(checked) => setTokenForm((p) => ({ ...p, default_token: Boolean(checked) }))} />
                </div>
              </Grid>
              <Button size="sm" variant="primary" icon={<Save className="h-3.5 w-3.5" />} onClick={createToken} loading={saving}>保存 Token</Button>
              {tokens.length > 0 && (
                <div className="grid items-start gap-3 cq-sm:grid-cols-2">
                  {tokens.map((token) => (
                    <div key={token.id} className="min-w-0 rounded-lg border border-kumo-line bg-kumo-base p-0 shadow-none">
                      <div className="grid gap-3 p-3">
                        <div className="flex min-w-0 items-start justify-between gap-2">
                          <div className="min-w-0">
                            <div className="flex min-w-0 flex-wrap items-center gap-2">
                              <Text variant="body" size="sm" bold truncate>{token.name}</Text>
                              {token.default_token && <Badge variant="success">默认</Badge>}
                            </div>
                            <Text variant="secondary" size="xs">{token.type}</Text>
                          </div>
                          <Badge variant={statusTone(token.last_test_status)}>{tokenTestStatusLabel(token.last_test_status)}</Badge>
                        </div>
                        <PermissionChecks token={token} />
                        <div className="flex items-center justify-end gap-2 border-t border-kumo-line pt-2">
                          <Button size="sm" variant="secondary" onClick={() => testToken(token.id)} loading={testingTokenId === String(token.id)}>
                            {selectedRepo ? '检测并用于当前仓库' : '检测权限'}
                          </Button>
                          <Button size="sm" shape="square" variant={isArmed(`github-token:${token.id}`) ? 'destructive' : 'secondary-destructive'} icon={<Trash className="h-3.5 w-3.5" />} onClick={() => deleteToken(token)} aria-label="删除 Token" title="删除 Token" />
                        </div>
                      </div>
                    </div>
                  ))}
                </div>
              )}
            </LayerCard.Primary>
          </LayerCard>

          <div className="grid gap-4 self-start">
            <LayerCard className="self-start p-0 shadow-none">
              <LayerCard.Secondary className={sectionCardHeaderClass}>
                <div className="flex min-w-0 items-center gap-2">
                  <Settings className="h-4 w-4 text-brand" />
                  <Text variant="body" size="sm" bold>采集与保留</Text>
                </div>
                {settings && (
                  <Switch size="sm" label="启用后台采集" controlFirst={false} checked={settings.enabled} onCheckedChange={(checked) => setSettings((p) => ({ ...p, enabled: Boolean(checked) }))} />
                )}
              </LayerCard.Secondary>
              <LayerCard.Primary className="p-4">
              {settings ? (
                <div className="grid gap-3">
                  <Input size="sm" label="默认采集间隔（秒）" type="number" min="60" value={settings.default_collect_interval_seconds} onChange={(e) => setSettings((p) => ({ ...p, default_collect_interval_seconds: Number(e.target.value) }))} />
                  <Input size="sm" label="默认保留天数" type="number" min="1" value={settings.default_retention_days} onChange={(e) => setSettings((p) => ({ ...p, default_retention_days: Number(e.target.value) }))} />
                  <Input size="sm" label="Rate Limit 低额度阈值" type="number" min="0" value={settings.rate_limit_low_threshold} onChange={(e) => setSettings((p) => ({ ...p, rate_limit_low_threshold: Number(e.target.value) }))} />
                  <Input size="sm" label="Star 激增阈值" type="number" min="1" value={settings.star_spike_threshold} onChange={(e) => setSettings((p) => ({ ...p, star_spike_threshold: Number(e.target.value) }))} />
                  <Button size="sm" variant="primary" icon={<Save className="h-3.5 w-3.5" />} onClick={saveSettings} loading={saving}>保存设置</Button>
                </div>
              ) : (
                <FillEmpty title="设置加载中" />
              )}
              </LayerCard.Primary>
            </LayerCard>

            <LayerCard className="self-start p-0 shadow-none">
              <LayerCard.Secondary className={sectionCardHeaderClass}>
                <div className="flex min-w-0 items-center gap-2">
                  <Activity className="h-4 w-4 text-brand" />
                  <Text variant="body" size="sm" bold>历史维护</Text>
                </div>
                <Badge variant={historyScope === 'current' ? 'info' : 'secondary'}>{historyScope === 'current' ? '当前仓库' : '全部仓库'}</Badge>
              </LayerCard.Secondary>
              <LayerCard.Primary className="grid gap-3 p-4">
                <Select
                  size="sm"
                  label="清理范围"
                  value={historyScope}
                  onValueChange={setHistoryScope}
                  items={historyScopeOptions}
                />
                <Input
                  size="sm"
                  label="清理保留天数"
                  type="number"
                  min="1"
                  value={historyRetentionDays}
                  onChange={(e) => setHistoryRetentionDays(e.target.value)}
                />
                <Text variant="secondary" size="xs">
                  “清理历史”删除旧的趋势、Actions、事件、Webhook 和审计记录；“压缩 Payload”将旧的大 JSON 改写为摘要，并在结束后回收数据库空间。
                </Text>
                <div className="flex flex-wrap gap-2">
                  <Button
                    size="sm"
                    variant="secondary-destructive"
                    icon={<Trash className="h-3.5 w-3.5" />}
                    onClick={cleanupGitHubHistory}
                    loading={maintenanceAction === 'cleanup-history'}
                  >
                    清理历史并压缩
                  </Button>
                  <Button
                    size="sm"
                    variant="secondary"
                    icon={<Save className="h-3.5 w-3.5" />}
                    onClick={compactGitHubHistory}
                    loading={maintenanceAction === 'compact-history'}
                  >
                    压缩已有 Payload
                  </Button>
                </div>
              </LayerCard.Primary>
            </LayerCard>
          </div>
        </div>
      )}

      {!selectedRepo && ['actions', 'trends', 'events'].includes(activeTab) && (
        <LayerCard className="p-0 shadow-none">
          <FillEmpty title="暂无仓库详情" description="请先选择仓库" />
        </LayerCard>
      )}

      <Dialog.Root open={repoDialogOpen} onOpenChange={setRepoDialogOpen}>
        <Dialog className="@container flex max-h-[min(calc(100dvh-2rem),34rem)] w-[min(calc(100vw-2rem),38rem)] flex-col overflow-hidden p-0">
          <div className="border-b border-kumo-line bg-kumo-recessed/20 px-5 py-4">
            <Dialog.Title className="text-base font-semibold text-kumo-strong">添加 GitHub 仓库</Dialog.Title>
            <Dialog.Description className="mt-1 text-xs text-kumo-subtle">
              支持公开和私有仓库。
            </Dialog.Description>
          </div>
          <form
            className="min-h-0 flex-1 overflow-y-auto"
            onSubmit={(event) => {
              event.preventDefault();
              createRepository();
            }}
          >
            <div className="grid gap-4 px-5 py-4">
              <Input
                size="sm"
                label="GitHub 仓库"
                value={repoForm.url}
                onChange={(e) => setRepoForm((p) => ({ ...p, url: e.target.value }))}
                placeholder="owner/repo 或完整 GitHub URL"
                autoFocus
              />
              <div className="grid gap-3 cq-sm:grid-cols-2">
                <Select
                  size="sm"
                  label="访问凭据"
                  value={repoForm.token_id}
                  onValueChange={(value) => setRepoForm((p) => ({ ...p, token_id: value }))}
                  items={tokenOptions}
                />
                <Input
                  size="sm"
                  label="采集间隔（秒）"
                  type="number"
                  min="60"
                  value={repoForm.collect_interval_seconds}
                  onChange={(e) => setRepoForm((p) => ({ ...p, collect_interval_seconds: e.target.value }))}
                />
                <Input
                  size="sm"
                  label="数据保留（天）"
                  type="number"
                  min="1"
                  value={repoForm.retention_days}
                  onChange={(e) => setRepoForm((p) => ({ ...p, retention_days: e.target.value }))}
                />
                <div className="flex h-full items-end">
                  <Switch
                    size="sm"
                    label="启用 Webhook"
                    controlFirst={false}
                    checked={repoForm.webhook_enabled}
                    onCheckedChange={(checked) => setRepoForm((p) => ({ ...p, webhook_enabled: Boolean(checked) }))}
                  />
                </div>
              </div>
            </div>
            <div className="flex justify-end gap-2 border-t border-kumo-line bg-kumo-recessed/25 px-5 py-3">
              <Dialog.Close render={(props) => <Button type="button" size="sm" variant="secondary" {...props}>取消</Button>} />
              <Button type="submit" size="sm" variant="primary" icon={<Plus className="h-3.5 w-3.5" />} loading={saving}>添加仓库</Button>
            </div>
          </form>
        </Dialog>
      </Dialog.Root>
    </div>
  );
}

export default GitHubPage;
