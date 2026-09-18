import React, { useCallback, useEffect, useMemo, useRef, useState } from 'react';
import {
  Badge,
  ChartPalette,
  ClipboardText,
  Grid,
  LayerCard,
  Tabs,
  Text,
  Toolbar,
} from '@cloudflare/kumo';
import { Button } from '@cloudflare/kumo/components/button';
import { LayerDialog } from '@cloudflare/kumo/components/layer-dialog';
import { Input } from '@cloudflare/kumo/components/input';
import { Select } from '@cloudflare/kumo/components/select';
import { Switch } from '@cloudflare/kumo/components/switch';
import { Table } from '@cloudflare/kumo/components/table';
import * as echarts from 'echarts/core';
import { LineChart } from 'echarts/charts';
import { AriaComponent, GridComponent, TooltipComponent } from 'echarts/components';
import { CanvasRenderer } from 'echarts/renderers';
import { toast } from '../../modules/toast.js';
import { dialog } from '../../modules/dialog.js';
import { useConfirmPress } from '../../hooks/useConfirmPress.js';
import { formatGitHubRepositoryDescription } from '../../modules/githubEmoji.js';
import { useNowTick } from '../../modules/usePageVisibility.js';
import { MODULE_TABS_PROPS } from '../../modules/kumoTabs.js';
import useStore from '../../store.js';
import { AnimatedCollapse } from '../../components/AnimatedCollapse.jsx';
import SiteFontTimeseriesChart from '../../components/SiteFontTimeseriesChart.jsx';
import {
  AppTable,
  ChartBoundaryBox,
  ChartWarmupSkeleton,
  DataTableFrame,
  TabBarOverflowActions,
  sectionCardHeaderClass,
  stickyTabsBaseClass,
} from '../../components/ui/AppPrimitives.jsx';
import GitHubPublicPagesPanel from './GitHubPublicPagesPanel.jsx';
import {
  Activity,
  Bell,
  Check,
  Clock,
  ExternalLink,
  GitBranch,
  GitHubBrand,
  Key,
  Play,
  Plus,
  RefreshCw,
  Rocket,
  Save,
  Settings,
  Star,
  TrendingUp,
  Trash,
  Upload,
  Download,
  Users,
  X,
} from '../../components/Icons.jsx';
import {
  GITHUB_ACTIONS_TABLE_WIDTHS,
  GITHUB_EVENTS_TABLE_WIDTHS,
  fineGrainedTokenURL,
  rangeOptions,
  tokenTypeOptions,
} from './constants.js';
import {
  formatActionDuration,
  formatDateTime,
  formatNumber,
  formatResetCountdown,
  parseTimestamp,
  statusLabel,
  statusTone,
  tokenTestStatusLabel,
} from './utils.js';
import tabs from './tabs.jsx';
import ActionWorkflowCanvas from './ActionWorkflowCanvas.jsx';
import ActionFlowPlaceholder from './ActionFlowPlaceholder.jsx';
import FillEmpty from './FillEmpty.jsx';
import PermissionChecks from './PermissionChecks.jsx';
import RepositoryStat, { RepositoryMetric } from './RepositoryStat.jsx';

echarts.use([LineChart, GridComponent, TooltipComponent, AriaComponent, CanvasRenderer]);

function GitHubPage() {
  const theme = useStore(s => s.theme);
  const { isArmed, confirmPress } = useConfirmPress();
  const isDarkMode = theme === 'dark';
  const getAuthHeaders = useCallback(
    () => ({
      'Content-Type': 'application/json',
    }),
    []
  );

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
  const [repoForm, setRepoForm] = useState({
    url: '',
    token_id: '',
    collect_interval_seconds: 900,
    retention_days: 90,
    webhook_enabled: false,
  });
  const [tokenForm, setTokenForm] = useState({
    name: '',
    token: '',
    type: 'fine_grained',
    default_token: false,
  });
  const [dispatchForm, setDispatchForm] = useState({ workflowId: '', ref: '' });
  const [historyRetentionDays, setHistoryRetentionDays] = useState('90');
  const [historyScope, setHistoryScope] = useState('all');
  const [maintenanceAction, setMaintenanceAction] = useState('');
  const eventSourceRef = useRef(null);
  const dispatchDefaultedRepoRef = useRef(null);
  const actionJobsRef = useRef([]);
  const actionCollapseTimerRef = useRef(null);

  const selectedRepo = useMemo(
    () =>
      repositories.find(repo => String(repo.id) === String(selectedRepoId)) ||
      repositories[0] ||
      null,
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

  const api = useCallback(
    async (path, options = {}) => {
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
    },
    [getAuthHeaders]
  );

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
      setSelectedRepoId(current => current || repos[0]?.id || null);
    } catch (error) {
      toast.error(error.message || '加载 GitHub 模块失败');
    }
  }, [api]);

  const repoImportInputRef = useRef(null);
  const [repoImporting, setRepoImporting] = useState(false);

  const exportRepositories = () => {
    if (repositories.length === 0) {
      toast.warning('暂无仓库可导出');
      return;
    }
    const payload = {
      version: '1.0',
      repositories: repositories.map(repo => ({
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

  const importRepositoriesFromFile = async event => {
    const file = event.target.files?.[0];
    event.target.value = '';
    if (!file) return;
    setRepoImporting(true);
    try {
      const data = JSON.parse(await file.text());
      const list = Array.isArray(data) ? data : data.repositories || [];
      const normalized = list.map(item => {
        const rawFull = String(item.full_name || '').trim();
        let owner = String(item.owner || '').trim();
        let name = String(item.name || '').trim();
        if (!owner && rawFull.includes('/')) {
          [owner, name] = [
            rawFull.slice(0, rawFull.indexOf('/')),
            rawFull.slice(rawFull.indexOf('/') + 1),
          ];
        }
        if (owner && !name && rawFull.includes('/')) {
          name = rawFull.slice(rawFull.indexOf('/') + 1);
        }
        return { ...item, owner, name };
      });
      const valid = normalized.filter(
        item => String(item.owner || '').trim() && String(item.name || '').trim()
      );
      if (valid.length === 0) throw new Error('文件中没有可导入的仓库（需要 owner 和 name）');
      if (
        !(await dialog.confirm(`确认导入 ${valid.length} 个仓库？重复仓库由 GitHub 侧自动去重。`))
      )
        return;
      const results = [];
      // 分批提交（10 个/批），避免一次性并发几十个请求压垮后端与 GitHub 限流。
      const CHUNK = 10;
      for (let i = 0; i < valid.length; i += CHUNK) {
        const chunk = valid.slice(i, i + CHUNK);
        const chunkResults = await Promise.allSettled(
          chunk.map(item =>
            api('/api/github/repositories', {
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
            })
          )
        );
        results.push(...chunkResults);
      }
      const imported = results.filter(result => result.status === 'fulfilled').length;
      const failed = results.filter(result => result.status === 'rejected').length;
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

  const loadRepoDetails = useCallback(
    async (repoId = selectedRepo?.id) => {
      if (!repoId) return;
      setTrendsLoading(true);
      try {
        const [
          trendData,
          actionData,
          eventData,
          trafficData,
          contributorData,
          workflowData,
          branchData,
        ] = await Promise.all([
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
    },
    [api, rangeDays, selectedRepo?.id]
  );

  const loadActionJobs = useCallback(
    async (runId = selectedActionRunId, options = {}) => {
      if (!selectedRepo?.id || !runId) return;
      const showLoading = options.showLoading ?? actionJobsRef.current.length === 0;
      if (showLoading) setActionJobsLoading(true);
      try {
        const run = actions.find(item => String(item.run_id) === String(runId));
        const params = new URLSearchParams();
        if (run?.workflow_name) params.set('workflow_name', run.workflow_name);
        if (run?.branch) params.set('branch', run.branch);
        if (run?.commit_sha) params.set('commit_sha', run.commit_sha);
        const detail = await api(
          `/api/github/repositories/${selectedRepo.id}/actions/runs/${runId}/jobs${params.toString() ? `?${params}` : ''}`
        );
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
    },
    [actions, api, selectedActionRunId, selectedRepo?.id]
  );

  useEffect(() => {
    loadOverview();
  }, [loadOverview]);

  useEffect(
    () => () => {
      if (actionCollapseTimerRef.current) window.clearTimeout(actionCollapseTimerRef.current);
    },
    []
  );

  useEffect(() => {
    loadRepoDetails();
  }, [loadRepoDetails]);

  useEffect(() => {
    if (selectedActionRunId)
      void loadActionJobs(selectedActionRunId, { showLoading: actionJobsRef.current.length === 0 });
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
    source.addEventListener('github', event => {
      try {
        const payload = JSON.parse(event.data);
        if (
          payload.kind === 'repository_refresh' ||
          payload.kind === 'repository_actions_refresh'
        ) {
          if (String(payload.repository_id) === String(selectedRepo?.id)) {
            void Promise.all([loadOverview(), loadRepoDetails(payload.repository_id)]);
          }
          return;
        }
        setEvents(current => [payload, ...current].slice(0, 100));
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

  const testToken = async id => {
    setTestingTokenId(String(id));
    try {
      const suffix = selectedRepo?.id
        ? `?repositoryId=${encodeURIComponent(selectedRepo.id)}&bind=true`
        : '';
      await api(`/api/github/tokens/${id}/test${suffix}`, { method: 'POST', body: '{}' });
      if (selectedRepo?.id) {
        await api(`/api/github/repositories/${selectedRepo.id}/refresh`, {
          method: 'POST',
          body: '{}',
        });
      }
      toast.success(
        selectedRepo?.id
          ? `Token 已检测并绑定到 ${selectedRepo.full_name}`
          : 'Token 基础权限检测完成'
      );
      await loadOverview();
    } catch (error) {
      toast.error(error.message || 'Token 权限检测失败');
      await loadOverview();
    } finally {
      setTestingTokenId(null);
    }
  };

  const deleteToken = async token => {
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
      setRepoForm({
        url: '',
        token_id: '',
        collect_interval_seconds: 900,
        retention_days: 90,
        webhook_enabled: false,
      });
      setRepoDialogOpen(false);
      setSelectedRepoId(repo.id);
      await loadOverview();
    } catch (error) {
      toast.error(error.message || '添加仓库失败');
    } finally {
      setSaving(false);
    }
  };

  const refreshRepository = async id => {
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

  const saveRepositoryOrder = async nextRepositories => {
    try {
      const orderedIds = nextRepositories.map(repo => repo.id);
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

  const handleRepositoryDragOver = event => {
    if (!draggedRepositoryId) return;
    event.preventDefault();
    event.dataTransfer.dropEffect = 'move';
  };

  const handleRepositoryDrop = async (targetRepoId, event) => {
    event.preventDefault();
    const sourceId = draggedRepositoryId || event.dataTransfer.getData('text/plain');
    setDraggedRepositoryId(null);
    if (!sourceId || String(sourceId) === String(targetRepoId)) return;
    const fromIndex = repositories.findIndex(repo => String(repo.id) === String(sourceId));
    const toIndex = repositories.findIndex(repo => String(repo.id) === String(targetRepoId));
    if (fromIndex < 0 || toIndex < 0) return;
    const next = [...repositories];
    const [moved] = next.splice(fromIndex, 1);
    next.splice(toIndex, 0, moved);
    setRepositories(next);
    await saveRepositoryOrder(next);
  };

  const toggleActionRun = run => {
    const runId = String(run.run_id);
    if (actionCollapseTimerRef.current) {
      window.clearTimeout(actionCollapseTimerRef.current);
      actionCollapseTimerRef.current = null;
    }
    if (String(selectedActionRunId) === runId) {
      setCollapsingActionRunId(runId);
      setSelectedActionRunId(null);
      actionCollapseTimerRef.current = window.setTimeout(() => {
        setCollapsingActionRunId(current => (current === runId ? null : current));
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

  const updateRepositoryToken = async value => {
    if (!selectedRepo?.id) return;
    setSaving(true);
    try {
      await api(`/api/github/repositories/${selectedRepo.id}`, {
        method: 'PATCH',
        body: JSON.stringify({ token_id: value ? Number(value) : null }),
      });
      await api(`/api/github/repositories/${selectedRepo.id}/refresh`, {
        method: 'POST',
        body: '{}',
      });
      toast.success('仓库访问凭据已更新');
      await loadOverview();
      await loadRepoDetails(selectedRepo.id);
    } catch (error) {
      toast.error(error.message || '更新仓库访问凭据失败');
    } finally {
      setSaving(false);
    }
  };

  const deleteRepository = async id => {
    const repo = repositories.find(item => item.id === id);
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
      await api(
        `/api/github/repositories/${selectedRepo.id}/actions/workflows/${encodeURIComponent(dispatchForm.workflowId.trim())}/dispatch`,
        {
          method: 'POST',
          body: JSON.stringify({ ref: dispatchForm.ref || selectedRepo.default_branch }),
        }
      );
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
        body: JSON.stringify({
          payload_url: `${window.location.origin}/api/github/webhook/${selectedRepo.id}`,
        }),
      });
      toast.success(result.created ? 'GitHub Webhook 已自动创建' : 'GitHub Webhook 已自动更新');
      await loadOverview();
    } catch (error) {
      toast.error(
        error.message ||
          '自动配置 Webhook 失败：请确认 Token 的 Resource owner、仓库范围和 Webhooks: write 权限'
      );
    } finally {
      setSaving(false);
    }
  };

  const cleanupGitHubHistory = async () => {
    const days = Math.max(
      1,
      Number(historyRetentionDays) ||
        Number(
          historyScope === 'current'
            ? selectedRepo?.retention_days
            : settings?.default_retention_days
        ) ||
        90
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
      if (historyScope === 'current' && selectedRepo?.id)
        params.set('repositoryId', String(selectedRepo.id));
      const result = await api(`/api/github/history?${params.toString()}`, { method: 'DELETE' });
      const totalDeleted = Object.values(result || {}).reduce(
        (sum, value) => sum + (Number(value) || 0),
        0
      );
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
      if (historyScope === 'current' && selectedRepo?.id)
        params.set('repositoryId', String(selectedRepo.id));
      const result = await api(
        `/api/github/history/compact${params.toString() ? `?${params.toString()}` : ''}`,
        { method: 'POST', body: '{}' }
      );
      const updatedEvents = Number(result?.github_events) || 0;
      const updatedDeliveries = Number(result?.github_webhook_deliveries) || 0;
      const savedBytes = Number(result?.bytes_saved) || 0;
      const savedMB = savedBytes > 0 ? `${(savedBytes / 1024 / 1024).toFixed(2)} MB` : '0 MB';
      toast.success(
        `已压缩 ${updatedEvents + updatedDeliveries} 条 GitHub 记录，约节省 ${savedMB}`
      );
      await loadOverview();
      if (selectedRepo?.id) await loadRepoDetails(selectedRepo.id);
    } catch (error) {
      toast.error(error.message || '压缩 GitHub 历史失败');
    } finally {
      setMaintenanceAction('');
    }
  };

  const chartData = useMemo(() => {
    const points = trends.map(point => ({
      ts: parseTimestamp(point.collected_at),
      stars: Number(point.stars) || 0,
      issues: Number(point.open_issues) || 0,
      prs: Number(point.open_pull_requests) || 0,
      commits: Number(point.commit_count) || 0,
      successRate: point.actions_total
        ? Math.round((Number(point.actions_success || 0) / Number(point.actions_total || 1)) * 100)
        : 0,
    }));
    return [
      {
        name: 'Stars',
        color: ChartPalette.semantic('Attention', isDarkMode),
        data: points.map(p => [p.ts, p.stars]),
      },
      {
        name: 'Issues',
        color: ChartPalette.semantic('Warning', isDarkMode),
        data: points.map(p => [p.ts, p.issues]),
      },
      {
        name: 'PR',
        color: ChartPalette.semantic('Info', isDarkMode),
        data: points.map(p => [p.ts, p.prs]),
      },
      {
        name: '提交',
        color: ChartPalette.semantic('Success', isDarkMode),
        data: points.map(p => [p.ts, p.commits]),
      },
      {
        name: 'Actions 成功率',
        color: ChartPalette.categorical(3, isDarkMode),
        data: points.map(p => [p.ts, p.successRate]),
      },
    ];
  }, [isDarkMode, trends]);

  const repoOptions = repositories.map(repo => ({ value: String(repo.id), label: repo.full_name }));
  const tokenOptions = [
    { value: '', label: '默认/公开访问' },
    ...tokens.map(token => ({ value: String(token.id), label: token.name })),
  ];
  const historyScopeOptions = [
    { value: 'all', label: '全部仓库' },
    ...(selectedRepo?.id
      ? [{ value: 'current', label: `当前仓库：${selectedRepo.full_name}` }]
      : []),
  ];
  const historyScopeLabel =
    historyScope === 'current' && selectedRepo
      ? `当前仓库 ${selectedRepo.full_name}`
      : '全部 GitHub 仓库';
  const workflowOptions = [
    { value: '', label: workflows.length > 0 ? '选择 Workflow' : '未发现 Workflow' },
    ...workflows
      .filter(workflow => !workflow.state || workflow.state === 'active')
      .map(workflow => ({
        value: String(workflow.id || workflow.path),
        label: workflow.name ? `${workflow.name} (${workflow.path})` : workflow.path,
      })),
  ];
  const branchOptions = useMemo(() => {
    const branchNames = branches.map(branch => branch.name).filter(Boolean);
    const availableBranchNames =
      selectedRepo?.default_branch && !branchNames.includes(selectedRepo.default_branch)
        ? [selectedRepo.default_branch, ...branchNames]
        : branchNames;
    return availableBranchNames.map(name => ({ value: name, label: name }));
  }, [branches, selectedRepo?.default_branch]);

  useEffect(() => {
    if (branchOptions.length === 0) return;
    setDispatchForm(current => ({
      ...current,
      ref: branchOptions.some(branch => branch.value === current.ref)
        ? current.ref
        : branchOptions[0].value,
    }));
  }, [selectedRepo?.id, branchOptions]);

  useEffect(() => {
    const fallback = selectedRepo?.retention_days || settings?.default_retention_days || 90;
    setHistoryRetentionDays(String(fallback));
  }, [selectedRepo?.id, selectedRepo?.retention_days, settings?.default_retention_days]);

  useEffect(() => {
    if (String(detailsRepoId) !== String(selectedRepo?.id)) return;
    if (dispatchDefaultedRepoRef.current === String(selectedRepo?.id)) return;
    const lastSuccessfulRun = actions.find(
      run => String(run.conclusion || '').toLowerCase() === 'success'
    );
    if (!lastSuccessfulRun) return;
    const runWorkflowName = String(
      lastSuccessfulRun.workflow_name || lastSuccessfulRun.display_title || ''
    ).toLowerCase();
    const workflow = workflows.find(
      item =>
        String(item.name || '').toLowerCase() === runWorkflowName ||
        String(item.path || '').toLowerCase() === runWorkflowName
    );
    if (!workflow) return;
    const workflowId = String(workflow.id || workflow.path);
    const ref = branchOptions.some(branch => branch.value === lastSuccessfulRun.branch)
      ? lastSuccessfulRun.branch
      : branchOptions[0]?.value || selectedRepo?.default_branch || '';
    dispatchDefaultedRepoRef.current = String(selectedRepo?.id);
    setDispatchForm(current =>
      current.workflowId === workflowId && current.ref === ref ? current : { workflowId, ref }
    );
  }, [
    actions,
    branchOptions,
    detailsRepoId,
    selectedRepo?.default_branch,
    selectedRepo?.id,
    workflows,
  ]);

  return (
    <div className="flex min-h-full w-full min-w-0 flex-col gap-4">
      <div
        className={`${stickyTabsBaseClass} justify-between gap-2 border-b border-kumo-line [&>*]:min-w-0`}
      >
        <Tabs
          {...MODULE_TABS_PROPS}
          value={activeTab}
          onValueChange={value => setActiveTab(String(value))}
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
                <Text variant="body" size="sm" bold>
                  仓库列表
                </Text>
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
                <Button
                  size="sm"
                  variant="primary"
                  icon={<Plus className="h-3.5 w-3.5" />}
                  onClick={() => setRepoDialogOpen(true)}
                >
                  添加仓库
                </Button>
              </div>
            </LayerCard.Secondary>
            <LayerCard.Primary className="p-0">
              {repositories.length === 0 ? (
                <FillEmpty title="暂无 GitHub 仓库" description="先添加仓库" />
              ) : (
                <div className="grid items-start gap-3 p-4 cq-sm:grid-cols-2 cq-xl:grid-cols-3 cq-2xl:grid-cols-4">
                  {repositories.map(repo => {
                    const isSelected = String(repo.id) === String(selectedRepo?.id);
                    const actionStatus =
                      repo.latest_action_conclusion || repo.latest_action_status || '未知';
                    const collectStatus = repo.last_status || 'pending';
                    const actionStartedAt =
                      repo.latest_action_started_at || repo.latest_action_created_at;
                    const actionDuration = formatActionDuration(
                      actionStartedAt,
                      repo.latest_action_updated_at,
                      currentTime
                    );
                    return (
                      <div
                        key={repo.id}
                        draggable
                        onDragStart={event => handleRepositoryDragStart(repo, event)}
                        onDragOver={handleRepositoryDragOver}
                        onDrop={event => handleRepositoryDrop(repo.id, event)}
                        onDragEnd={() => setDraggedRepositoryId(null)}
                        className={`min-w-0 cursor-move rounded-lg border border-kumo-line bg-kumo-base p-0 shadow-none transition-[opacity,transform,border-color] duration-160 ${isSelected ? 'ring-1 ring-brand/50' : ''} ${draggedRepositoryId === String(repo.id) ? 'scale-[0.99] opacity-50' : ''}`}
                      >
                        <div className="grid gap-3 p-3">
                          <div className="flex min-w-0 items-start justify-between gap-2">
                            <Button
                              type="button"
                              variant="ghost"
                              className="h-auto min-w-0 flex-1 !items-start !justify-start !px-0 text-left"
                              onClick={() => setSelectedRepoId(repo.id)}
                            >
                              <span className="block min-w-0">
                                <span className="block truncate text-sm font-semibold text-kumo-strong">
                                  {repo.full_name}
                                </span>
                                <span className="block truncate text-[11px] text-kumo-subtle">
                                  {formatGitHubRepositoryDescription(
                                    repo.description,
                                    repo.html_url
                                  )}
                                </span>
                              </span>
                            </Button>
                            <div className="flex shrink-0 items-center gap-1">
                              <Badge variant={repo.private ? 'warning' : 'success'}>
                                {repo.private ? '私有' : '公开'}
                              </Badge>
                              <Badge
                                variant={
                                  repo.owned_by_token || repo.can_operate_actions
                                    ? 'success'
                                    : 'neutral'
                                }
                              >
                                {repo.owned_by_token
                                  ? '本人仓库'
                                  : repo.can_operate_actions
                                    ? '有写权限'
                                    : repo.authenticated
                                      ? '只读权限'
                                      : '未认证'}
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
                                {actionStartedAt && (
                                  <span
                                    className="min-w-0 truncate"
                                    title={formatDateTime(actionStartedAt)}
                                  >
                                    {formatDateTime(actionStartedAt)}
                                  </span>
                                )}
                                <Badge variant={statusTone(actionStatus)}>
                                  {actionStartedAt
                                    ? `${statusLabel(actionStatus)} · ${actionDuration}`
                                    : statusLabel(actionStatus)}
                                </Badge>
                              </div>
                            </div>
                            <div className="flex min-w-0 items-center justify-between gap-2">
                              <span>采集</span>
                              <div className="flex min-w-0 items-center justify-end gap-2">
                                <span className="min-w-0 truncate">
                                  {formatDateTime(repo.last_collected_at)}
                                </span>
                                <Badge variant={statusTone(collectStatus)}>
                                  {statusLabel(collectStatus)}
                                </Badge>
                              </div>
                            </div>
                          </div>

                          <div className="flex items-center justify-end gap-1 border-t border-kumo-line pt-2">
                            {repo.html_url && (
                              <Button
                                size="sm"
                                shape="square"
                                variant="secondary"
                                icon={<ExternalLink className="h-3.5 w-3.5" />}
                                onClick={event => {
                                  event.stopPropagation();
                                  window.open(repo.html_url, '_blank');
                                }}
                                aria-label="打开 GitHub"
                                title="打开 GitHub"
                              />
                            )}
                            <Button
                              size="sm"
                              shape="square"
                              variant="secondary"
                              icon={<RefreshCw className="h-3.5 w-3.5" />}
                              onClick={event => {
                                event.stopPropagation();
                                refreshRepository(repo.id);
                              }}
                              loading={refreshingRepositoryId === String(repo.id)}
                              aria-label="刷新仓库"
                              title="刷新仓库"
                            />
                            <Button
                              size="sm"
                              shape="square"
                              variant={
                                isArmed(`github-repo:${repo.id}`)
                                  ? 'destructive'
                                  : 'secondary-destructive'
                              }
                              icon={<Trash className="h-3.5 w-3.5" />}
                              onClick={event => {
                                event.stopPropagation();
                                deleteRepository(repo.id);
                              }}
                              aria-label="删除仓库"
                              title="删除仓库"
                            />
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
                <Text variant="body" size="sm" bold truncate>
                  {selectedRepo.full_name}
                </Text>
              </div>
              <div className="flex flex-wrap items-end gap-2">
                <Select
                  alignItemWithTrigger
                  size="sm"
                  aria-label="仓库访问凭据"
                  value={selectedRepo.token_id ? String(selectedRepo.token_id) : ''}
                  onValueChange={updateRepositoryToken}
                  items={tokenOptions}
                  disabled={saving}
                />
                <Select
                  alignItemWithTrigger
                  size="sm"
                  aria-label="选择 GitHub 仓库"
                  value={String(selectedRepo.id)}
                  onValueChange={setSelectedRepoId}
                  items={repoOptions}
                />
                <Button
                  size="sm"
                  variant="secondary"
                  icon={<RefreshCw className="h-3.5 w-3.5" />}
                  onClick={() => refreshRepository(selectedRepo.id)}
                  loading={refreshingRepositoryId === String(selectedRepo.id)}
                >
                  刷新仓库
                </Button>
              </div>
            </LayerCard.Secondary>
            <LayerCard.Primary className="p-4">
              <Grid variant="6up" gap="sm" className="items-start cq-xl:grid-cols-5">
                <RepositoryMetric
                  icon={<Star className="h-3.5 w-3.5" />}
                  label="Stars"
                  value={formatNumber(selectedRepo.stars)}
                />
                <RepositoryMetric
                  icon={<GitBranch className="h-3.5 w-3.5" />}
                  label="Forks"
                  value={formatNumber(selectedRepo.forks)}
                />
                <RepositoryMetric
                  icon={<Activity className="h-3.5 w-3.5" />}
                  label="Issues / PR"
                  value={`${formatNumber(selectedRepo.open_issues)} / ${formatNumber(selectedRepo.open_pull_requests)}`}
                />
                <RepositoryMetric
                  icon={<Rocket className="h-3.5 w-3.5" />}
                  label="Latest Release"
                  value={selectedRepo.latest_release || '-'}
                />
                <RepositoryMetric
                  icon={<Clock className="h-3.5 w-3.5" />}
                  label="Rate Limit"
                  value={selectedRepo.rate_limit_remaining ?? '-'}
                  detail={
                    selectedRepo.rate_limit_reset
                      ? formatResetCountdown(selectedRepo.rate_limit_reset, currentTime)
                      : ''
                  }
                />
              </Grid>
            </LayerCard.Primary>
          </LayerCard>

          {activeTab === 'actions' && (
            <LayerCard className="p-0 shadow-none">
              <LayerCard.Secondary className={sectionCardHeaderClass}>
                <div className="flex items-center gap-2">
                  <Activity className="h-4 w-4 text-brand" />
                  <Text variant="body" size="sm" bold>
                    Actions 活动
                  </Text>
                </div>
                {canAttemptActionOperations ? (
                  <div className="flex flex-wrap items-center gap-2">
                    <Select
                      alignItemWithTrigger
                      size="sm"
                      className="w-64"
                      aria-label="选择 Workflow"
                      value={dispatchForm.workflowId}
                      onValueChange={value => setDispatchForm(p => ({ ...p, workflowId: value }))}
                      items={workflowOptions}
                      disabled={workflowOptions.length <= 1}
                    />
                    <Select
                      alignItemWithTrigger
                      size="sm"
                      className="w-40"
                      aria-label="选择触发分支"
                      value={dispatchForm.ref}
                      onValueChange={value => setDispatchForm(p => ({ ...p, ref: value }))}
                      items={branchOptions}
                      disabled={branchOptions.length === 0}
                    />
                    <Button
                      size="sm"
                      variant="primary"
                      icon={<Play className="h-3.5 w-3.5" />}
                      onClick={dispatchWorkflow}
                    >
                      触发
                    </Button>
                  </div>
                ) : (
                  <Badge variant="neutral">
                    {selectedRepo.authenticated ? '当前 Token 无写权限' : '未配置 Token'}
                  </Badge>
                )}
              </LayerCard.Secondary>
              <LayerCard.Primary className="p-0">
                {actions.length === 0 ? (
                  <FillEmpty title="暂无 Actions 记录" description="刷新后显示" />
                ) : (
                  <DataTableFrame
                    variant="embedded"
                    density="compact"
                    className="min-w-0 overflow-x-auto overflow-y-visible scrollbar-thin"
                  >
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
                        <Table.Row>
                          <Table.Head className="align-middle text-center">状态</Table.Head>
                          <Table.Head>Workflow</Table.Head>
                          <Table.Head className="align-middle">提交说明</Table.Head>
                          <Table.Head className="align-middle">分支</Table.Head>
                          <Table.Head className="align-middle text-center">时间</Table.Head>
                          <Table.Head className="app-table-action align-middle">操作</Table.Head>
                        </Table.Row>
                      </Table.Header>
                      <Table.Body>
                        {actions.map(run => {
                          const isActionExpanded =
                            String(selectedActionRunId) === String(run.run_id);
                          const isActionVisible =
                            isActionExpanded ||
                            String(collapsingActionRunId) === String(run.run_id);
                          return (
                            <React.Fragment key={run.run_id}>
                              <Table.Row
                                className="cursor-pointer"
                                onClick={() => toggleActionRun(run)}
                              >
                                <Table.Cell className="align-middle text-center">
                                  <Badge
                                    variant={statusTone(run.conclusion || run.status)}
                                  >{`${statusLabel(run.conclusion || run.status)} · ${formatActionDuration(run.run_started_at || run.created_at, run.updated_at, currentTime)}`}</Badge>
                                </Table.Cell>
                                <Table.Cell>
                                  <div
                                    className="min-w-0 truncate font-semibold text-kumo-strong"
                                    title={run.workflow_name || String(run.run_id)}
                                  >
                                    {run.workflow_name || run.run_id}
                                  </div>
                                  <div
                                    className="min-w-0 truncate text-[11px] text-kumo-subtle"
                                    title={`${run.actor || '-'} · ${String(run.commit_sha || '').slice(0, 8)}`}
                                  >
                                    {run.actor} · {String(run.commit_sha || '').slice(0, 8)}
                                  </div>
                                </Table.Cell>
                                <Table.Cell className="align-middle">
                                  <div
                                    className="truncate text-sm leading-6 text-kumo-strong"
                                    title={run.commit_message || run.display_title || ''}
                                  >
                                    {run.commit_message || run.display_title || '暂无提交说明'}
                                  </div>
                                </Table.Cell>
                                <Table.Cell className="align-middle">
                                  <div className="min-w-0 truncate" title={run.branch || '-'}>
                                    {run.branch || '-'}
                                  </div>
                                </Table.Cell>
                                <Table.Cell className="align-middle text-center">
                                  <div
                                    className="min-w-0 truncate text-sm leading-6 text-kumo-strong"
                                    title={formatDateTime(run.run_started_at || run.created_at)}
                                  >
                                    {formatDateTime(run.run_started_at || run.created_at)}
                                  </div>
                                </Table.Cell>
                                <Table.Cell className="align-middle text-center">
                                  <div className="flex justify-center gap-1">
                                    {run.html_url && (
                                      <Button
                                        size="sm"
                                        shape="square"
                                        variant="secondary"
                                        icon={<ExternalLink className="h-3.5 w-3.5" />}
                                        onClick={event => {
                                          event.stopPropagation();
                                          window.open(run.html_url, '_blank');
                                        }}
                                        aria-label="打开 Actions"
                                        title="打开 Actions"
                                      />
                                    )}
                                    {canAttemptActionOperations && (
                                      <>
                                        <Button
                                          size="sm"
                                          shape="square"
                                          variant="secondary"
                                          icon={<RefreshCw className="h-3.5 w-3.5" />}
                                          onClick={event => {
                                            event.stopPropagation();
                                            actionOperation(run.run_id, 'rerun');
                                          }}
                                          aria-label="重新运行"
                                          title="重新运行"
                                        />
                                        <Button
                                          size="sm"
                                          shape="square"
                                          variant="secondary"
                                          icon={<Check className="h-3.5 w-3.5" />}
                                          onClick={event => {
                                            event.stopPropagation();
                                            actionOperation(run.run_id, 'rerun-failed-jobs');
                                          }}
                                          aria-label="重跑失败任务"
                                          title="重跑失败任务"
                                        />
                                        <Button
                                          size="sm"
                                          shape="square"
                                          variant="secondary-destructive"
                                          icon={<X className="h-3.5 w-3.5" />}
                                          onClick={event => {
                                            event.stopPropagation();
                                            actionOperation(run.run_id, 'cancel');
                                          }}
                                          aria-label="取消"
                                          title="取消"
                                        />
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
                                            <Text variant="secondary" size="sm">
                                              暂无 Job 进度数据
                                            </Text>
                                          </div>
                                        ) : (
                                          (() => {
                                            return (
                                              <ActionWorkflowCanvas
                                                workflow={actionWorkflow}
                                                jobs={actionJobs}
                                                now={currentTime}
                                              />
                                            );
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
                    <Text variant="body" size="sm" bold>
                      仓库趋势
                    </Text>
                  </div>
                  <Select
                    alignItemWithTrigger
                    size="sm"
                    aria-label="趋势时间范围"
                    value={rangeDays}
                    onValueChange={setRangeDays}
                    items={rangeOptions}
                  />
                </LayerCard.Secondary>
                <LayerCard.Primary className="p-4">
                  {trendsLoading && trends.length < 2 ? (
                    <ChartWarmupSkeleton height={320} bars={10} />
                  ) : trends.length >= 2 ? (
                    <ChartBoundaryBox>
                      {tooltipBoundary => (
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
                          xAxisTickFormat={value => new Date(value).toLocaleDateString()}
                          yAxisTickFormat={value => `${Math.round(value)}`}
                          tooltipValueFormat={value => `${Math.round(value)}`}
                          tooltipBoundary={tooltipBoundary ?? undefined}
                          tooltipFollowCursor="x"
                          ariaDescription="GitHub 仓库趋势"
                        />
                      )}
                    </ChartBoundaryBox>
                  ) : (
                    <FillEmpty title="趋势数据不足" description="至少两次采集后显示" />
                  )}
                </LayerCard.Primary>
              </LayerCard>
              <LayerCard className="self-start p-0 shadow-none">
                <LayerCard.Secondary className={sectionCardHeaderClass}>
                  <Users className="h-4 w-4 text-brand" />
                  <Text variant="body" size="sm" bold>
                    流量与贡献者
                  </Text>
                </LayerCard.Secondary>
                <LayerCard.Primary className="grid content-start gap-3 p-4">
                  <RepositoryMetric
                    label="访问量"
                    value={formatNumber(traffic[0]?.views)}
                    detail={`唯一访客 ${formatNumber(traffic[0]?.view_uniques)}`}
                  />
                  <RepositoryMetric
                    label="克隆量"
                    value={formatNumber(traffic[0]?.clones)}
                    detail={`唯一克隆 ${formatNumber(traffic[0]?.clone_uniques)}`}
                  />
                  <RepositoryMetric
                    label="贡献者"
                    value={formatNumber(contributors.length)}
                    detail={
                      contributors
                        .slice(0, 3)
                        .map(item => item.login)
                        .join(', ') || '暂无贡献者数据'
                    }
                  />
                </LayerCard.Primary>
              </LayerCard>
            </div>
          )}

          {activeTab === 'events' && (
            <div className="grid items-start gap-4 cq-xl:grid-cols-[minmax(0,1.2fr)_minmax(20rem,0.8fr)]">
              <LayerCard className="p-0 shadow-none">
                <LayerCard.Secondary className={sectionCardHeaderClass}>
                  <Bell className="h-4 w-4 text-brand" />
                  <Text variant="body" size="sm" bold>
                    事件与通知源
                  </Text>
                </LayerCard.Secondary>
                <LayerCard.Primary className="p-0">
                  {events.length === 0 ? (
                    <FillEmpty title="暂无 GitHub 事件" description="等待 Webhook 或采集" />
                  ) : (
                    <DataTableFrame
                      variant="embedded"
                      density="compact"
                      className="min-w-0 overflow-x-auto overflow-y-visible scrollbar-thin"
                    >
                      <AppTable layout="fixed" widths={GITHUB_EVENTS_TABLE_WIDTHS}>
                        <colgroup>
                          <col style={{ width: GITHUB_EVENTS_TABLE_WIDTHS[0] }} />
                          <col style={{ width: GITHUB_EVENTS_TABLE_WIDTHS[1] }} />
                          <col style={{ width: GITHUB_EVENTS_TABLE_WIDTHS[2] }} />
                          <col style={{ width: GITHUB_EVENTS_TABLE_WIDTHS[3] }} />
                        </colgroup>
                        <Table.Header sticky variant="compact">
                          <Table.Row>
                            <Table.Head>事件</Table.Head>
                            <Table.Head className="align-middle text-center">等级</Table.Head>
                            <Table.Head className="align-middle text-center">来源</Table.Head>
                            <Table.Head className="align-middle text-center">时间</Table.Head>
                          </Table.Row>
                        </Table.Header>
                        <Table.Body>
                          {events.map((event, index) => (
                            <Table.Row key={event.id || `${event.event_type}-${index}`}>
                              <Table.Cell>
                                <div className="font-semibold text-kumo-strong">
                                  {event.title || event.event_type}
                                </div>
                                <div className="max-w-2xl truncate text-[11px] text-kumo-subtle">
                                  {event.message}
                                </div>
                              </Table.Cell>
                              <Table.Cell className="align-middle text-center">
                                <Badge variant={statusTone(event.severity)}>
                                  {statusLabel(event.severity)}
                                </Badge>
                              </Table.Cell>
                              <Table.Cell className="align-middle text-center">
                                {event.source || 'stream'}
                              </Table.Cell>
                              <Table.Cell className="align-middle text-center text-[11px] text-kumo-subtle">
                                {formatDateTime(event.created_at)}
                              </Table.Cell>
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
                    <Text variant="body" size="sm" bold>
                      Webhook 配置
                    </Text>
                  </div>
                  <Button
                    size="sm"
                    variant="primary"
                    icon={<Save className="h-3.5 w-3.5" />}
                    onClick={configureWebhook}
                    loading={saving}
                  >
                    自动配置
                  </Button>
                </LayerCard.Secondary>
                <LayerCard.Primary className="grid content-start gap-4 p-4">
                  <div className="grid gap-1">
                    <Text variant="secondary" size="xs">
                      Payload URL
                    </Text>
                    <ClipboardText
                      size="sm"
                      text={`${window.location.origin}/api/github/webhook/${selectedRepo.id}`}
                    />
                  </div>
                  <div className="grid gap-1">
                    <Text variant="secondary" size="xs">
                      Secret
                    </Text>
                    <ClipboardText size="sm" text={selectedRepo.webhook_secret || '-'} />
                  </div>
                  <Text variant="secondary" size="xs">
                    选择 application/json，启用 workflow_run、release、issues、pull_request、star 和
                    ping 事件。
                  </Text>
                </LayerCard.Primary>
              </LayerCard>
            </div>
          )}
        </div>
      )}

      {activeTab === 'public-pages' && <GitHubPublicPagesPanel repositories={repositories} />}

      {activeTab === 'settings' && (
        <div className="grid items-start gap-4 cq-xl:grid-cols-2">
          <LayerCard className="self-start p-0 shadow-none">
            <LayerCard.Secondary className={sectionCardHeaderClass}>
              <div className="flex min-w-0 items-center gap-2">
                <Key className="h-4 w-4 text-brand" />
                <Text variant="body" size="sm" bold>
                  GitHub Token
                </Text>
              </div>
              <Button
                size="sm"
                variant="secondary"
                icon={<ExternalLink className="h-3.5 w-3.5" />}
                onClick={() =>
                  window.open(
                    fineGrainedTokenURL(selectedRepo?.owner || ''),
                    '_blank',
                    'noopener,noreferrer'
                  )
                }
              >
                打开 GitHub 创建页
              </Button>
            </LayerCard.Secondary>
            <LayerCard.Primary className="grid gap-3 p-4">
              <Text variant="secondary" size="xs">
                组织仓库请将 Resource owner 设为仓库所属组织，并等待组织审批；仓库 Webhook
                使用仓库级 Webhooks: read/write，无需组织级权限。
              </Text>
              <Input
                size="sm"
                label="Token 名称"
                value={tokenForm.name}
                onChange={e => setTokenForm(p => ({ ...p, name: e.target.value }))}
                placeholder="生产账号"
              />
              <Input
                size="sm"
                label="Token"
                value={tokenForm.token}
                onChange={e => setTokenForm(p => ({ ...p, token: e.target.value }))}
                placeholder="github_pat_..."
                autoComplete="off"
                spellCheck={false}
                className="font-mono"
              />
              <Grid variant="2up" gap="sm">
                <Select
                  alignItemWithTrigger
                  size="sm"
                  label="Token 类型"
                  value={tokenForm.type}
                  onValueChange={value => setTokenForm(p => ({ ...p, type: value }))}
                  items={tokenTypeOptions}
                />
                <div className="flex h-full items-end">
                  <Switch
                    size="sm"
                    label="设为默认"
                    controlFirst={false}
                    checked={tokenForm.default_token}
                    onCheckedChange={checked =>
                      setTokenForm(p => ({ ...p, default_token: Boolean(checked) }))
                    }
                  />
                </div>
              </Grid>
              <Button
                size="sm"
                variant="primary"
                icon={<Save className="h-3.5 w-3.5" />}
                onClick={createToken}
                loading={saving}
              >
                保存 Token
              </Button>
              {tokens.length > 0 && (
                <div className="grid items-start gap-3 cq-sm:grid-cols-2">
                  {tokens.map(token => (
                    <div
                      key={token.id}
                      className="min-w-0 rounded-lg border border-kumo-line bg-kumo-base p-0 shadow-none"
                    >
                      <div className="grid gap-3 p-3">
                        <div className="flex min-w-0 items-start justify-between gap-2">
                          <div className="min-w-0">
                            <div className="flex min-w-0 flex-wrap items-center gap-2">
                              <Text variant="body" size="sm" bold truncate>
                                {token.name}
                              </Text>
                              {token.default_token && <Badge variant="success">默认</Badge>}
                            </div>
                            <Text variant="secondary" size="xs">
                              {token.type}
                            </Text>
                          </div>
                          <Badge variant={statusTone(token.last_test_status)}>
                            {tokenTestStatusLabel(token.last_test_status)}
                          </Badge>
                        </div>
                        <PermissionChecks token={token} />
                        <div className="flex items-center justify-end gap-2 border-t border-kumo-line pt-2">
                          <Button
                            size="sm"
                            variant="secondary"
                            onClick={() => testToken(token.id)}
                            loading={testingTokenId === String(token.id)}
                          >
                            {selectedRepo ? '检测并用于当前仓库' : '检测权限'}
                          </Button>
                          <Button
                            size="sm"
                            shape="square"
                            variant={
                              isArmed(`github-token:${token.id}`)
                                ? 'destructive'
                                : 'secondary-destructive'
                            }
                            icon={<Trash className="h-3.5 w-3.5" />}
                            onClick={() => deleteToken(token)}
                            aria-label="删除 Token"
                            title="删除 Token"
                          />
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
                  <Text variant="body" size="sm" bold>
                    采集与保留
                  </Text>
                </div>
                {settings && (
                  <Switch
                    size="sm"
                    label="启用后台采集"
                    controlFirst={false}
                    checked={settings.enabled}
                    onCheckedChange={checked =>
                      setSettings(p => ({ ...p, enabled: Boolean(checked) }))
                    }
                  />
                )}
              </LayerCard.Secondary>
              <LayerCard.Primary className="p-4">
                {settings ? (
                  <div className="grid gap-3">
                    <Input
                      size="sm"
                      label="默认采集间隔（秒）"
                      type="number"
                      min="60"
                      value={settings.default_collect_interval_seconds}
                      onChange={e =>
                        setSettings(p => ({
                          ...p,
                          default_collect_interval_seconds: Number(e.target.value),
                        }))
                      }
                    />
                    <Input
                      size="sm"
                      label="默认保留天数"
                      type="number"
                      min="1"
                      value={settings.default_retention_days}
                      onChange={e =>
                        setSettings(p => ({ ...p, default_retention_days: Number(e.target.value) }))
                      }
                    />
                    <Input
                      size="sm"
                      label="Rate Limit 低额度阈值"
                      type="number"
                      min="0"
                      value={settings.rate_limit_low_threshold}
                      onChange={e =>
                        setSettings(p => ({
                          ...p,
                          rate_limit_low_threshold: Number(e.target.value),
                        }))
                      }
                    />
                    <Input
                      size="sm"
                      label="Star 激增阈值"
                      type="number"
                      min="1"
                      value={settings.star_spike_threshold}
                      onChange={e =>
                        setSettings(p => ({ ...p, star_spike_threshold: Number(e.target.value) }))
                      }
                    />
                    <Button
                      size="sm"
                      variant="primary"
                      icon={<Save className="h-3.5 w-3.5" />}
                      onClick={saveSettings}
                      loading={saving}
                    >
                      保存设置
                    </Button>
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
                  <Text variant="body" size="sm" bold>
                    历史维护
                  </Text>
                </div>
                <Badge variant={historyScope === 'current' ? 'info' : 'secondary'}>
                  {historyScope === 'current' ? '当前仓库' : '全部仓库'}
                </Badge>
              </LayerCard.Secondary>
              <LayerCard.Primary className="grid gap-3 p-4">
                <Select
                  alignItemWithTrigger
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
                  onChange={e => setHistoryRetentionDays(e.target.value)}
                />
                <Text variant="secondary" size="xs">
                  “清理历史”删除旧的趋势、Actions、事件、Webhook 和审计记录；“压缩 Payload”将旧的大
                  JSON 改写为摘要，并在结束后回收数据库空间。
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

      <LayerDialog.Root open={repoDialogOpen} onOpenChange={setRepoDialogOpen}>
        <LayerDialog.Content size="base">
          <LayerDialog.Title>添加 GitHub 仓库</LayerDialog.Title>
          <LayerDialog.Description>支持公开和私有仓库。</LayerDialog.Description>
          <LayerDialog.Body>
            <form
              className="min-h-0"
              onSubmit={event => {
                event.preventDefault();
                createRepository();
              }}
            >
              <div className="@container grid gap-4">
                <Input
                  size="sm"
                  label="GitHub 仓库"
                  value={repoForm.url}
                  onChange={e => setRepoForm(p => ({ ...p, url: e.target.value }))}
                  placeholder="owner/repo 或完整 GitHub URL"
                  autoFocus
                />
                <div className="grid gap-3 cq-sm:grid-cols-2">
                  <Select
                    alignItemWithTrigger
                    size="sm"
                    label="访问凭据"
                    value={repoForm.token_id}
                    onValueChange={value => setRepoForm(p => ({ ...p, token_id: value }))}
                    items={tokenOptions}
                  />
                  <Input
                    size="sm"
                    label="采集间隔（秒）"
                    type="number"
                    min="60"
                    value={repoForm.collect_interval_seconds}
                    onChange={e =>
                      setRepoForm(p => ({ ...p, collect_interval_seconds: e.target.value }))
                    }
                  />
                  <Input
                    size="sm"
                    label="数据保留（天）"
                    type="number"
                    min="1"
                    value={repoForm.retention_days}
                    onChange={e => setRepoForm(p => ({ ...p, retention_days: e.target.value }))}
                  />
                  <div className="flex h-full items-end">
                    <Switch
                      size="sm"
                      label="启用 Webhook"
                      controlFirst={false}
                      checked={repoForm.webhook_enabled}
                      onCheckedChange={checked =>
                        setRepoForm(p => ({ ...p, webhook_enabled: Boolean(checked) }))
                      }
                    />
                  </div>
                </div>
              </div>
            </form>
          </LayerDialog.Body>
          <LayerDialog.Actions dismissLabel="取消">
            <LayerDialog.Actions.Primary
              type="button"
              onClick={createRepository}
              icon={<Plus className="h-3.5 w-3.5" />}
              loading={saving}
            >
              添加仓库
            </LayerDialog.Actions.Primary>
          </LayerDialog.Actions>
        </LayerDialog.Content>
      </LayerDialog.Root>
    </div>
  );
}

export default GitHubPage;
