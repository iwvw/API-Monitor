import { normalizeWorkflowJobName, workflowJobMatchesDefinition } from '../../modules/githubWorkflowJobs.js';
import { statusTone } from './constants.js';

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

export {
  normalizePublicPath,
  parsePublicGithubStreamPayload,
  formatDateTime,
  formatActionDuration,
  formatNumber,
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
};
