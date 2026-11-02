import React, { useCallback, useEffect, useMemo, useRef, useState } from 'react';
import { toast } from '../../modules/toast.js';
import { useConfirmPress } from '../../hooks/useConfirmPress.js';
import { Button } from '@cloudflare/kumo/components/button';
import { LayerCard, Tabs } from '@cloudflare/kumo';
import { TooltipProvider } from '@cloudflare/kumo/components/tooltip';
import { MODULE_TABS_PROPS } from '../../modules/kumoTabs.js';
import { TabBarOverflowActions, stickyTabsBaseClass } from '../../components/ui/AppPrimitives.jsx';
import { Activity, Check, Clock, Download, GitBranch, Plus, RefreshCw, Server, Upload } from '../../components/Icons.jsx';
import { DEFAULT_TASK_FORM } from './constants.js';
import {
  cloneWorkflowForm,
  getCronExpressionFromSimple,
  parseSimpleSchedule,
} from './utils.js';
import { TASK_TABS } from './tabs.jsx';
import { TaskListTab } from './TaskListTab.jsx';
import { WorkflowListTab } from './WorkflowListTab.jsx';
import { RunsTab } from './RunsTab.jsx';
import { NodesTab } from './NodesTab.jsx';
import { TaskDialog } from './TaskDialog.jsx';
import { WorkflowDialog } from './WorkflowDialog.jsx';
import { TaskLogsDialog } from './TaskLogsDialog.jsx';
import { RunDetailDialog } from './RunDetailDialog.jsx';

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
          <TaskListTab
            tasks={tasks}
            loading={loading}
            isArmed={isArmed}
            openCreateTask={openCreateTask}
            openEditTask={openEditTask}
            openTaskLogs={openTaskLogs}
            runTask={runTask}
            toggleTask={toggleTask}
            deleteTask={deleteTask}
          />
        )}

        {activeTab === 'workflows' && (
          <WorkflowListTab
            workflows={workflows}
            tasks={tasks}
            runs={runs}
            isArmed={isArmed}
            onNavigate={onNavigate}
            openCreateWorkflow={openCreateWorkflow}
            openEditWorkflow={openEditWorkflow}
            runWorkflow={runWorkflow}
            deleteWorkflow={deleteWorkflow}
          />
        )}

        {activeTab === 'runs' && (
          <RunsTab
            runs={runs}
            isArmed={isArmed}
            clearOldRuns={clearOldRuns}
            clearAllRuns={clearAllRuns}
            authHeaders={authHeaders}
            setSelectedRun={setSelectedRun}
            retryRun={retryRun}
            cancelRun={cancelRun}
          />
        )}

        {activeTab === 'nodes' && (
          <NodesTab nodes={nodes} />
        )}

        <TaskDialog
          taskDialogOpen={taskDialogOpen}
          setTaskDialogOpen={setTaskDialogOpen}
          taskForm={taskForm}
          setTaskForm={setTaskForm}
          nodeItems={nodeItems}
          taskCommandLabel={taskCommandLabel}
          taskCommandPlaceholder={taskCommandPlaceholder}
          aiModelOptions={aiModelOptions}
          aiChannelOptions={aiChannelOptions}
          cronPreview={cronPreview}
          cronPreviewError={cronPreviewError}
          saveTask={saveTask}
          saving={saving}
        />

        <WorkflowDialog
          workflowDialogOpen={workflowDialogOpen}
          setWorkflowDialogOpen={setWorkflowDialogOpen}
          workflowForm={workflowForm}
          setWorkflowForm={setWorkflowForm}
          tasks={tasks}
          taskItems={taskItems}
          workflowNodeItems={workflowNodeItems}
          selectedWorkflowNode={selectedWorkflowNode}
          setSelectedWorkflowNodeId={setSelectedWorkflowNodeId}
          workflowCanvasEpoch={workflowCanvasEpoch}
          getWorkflowNodeAiChannelId={getWorkflowNodeAiChannelId}
          updateWorkflowNodeAiConfig={updateWorkflowNodeAiConfig}
          updateWorkflowNode={updateWorkflowNode}
          updateWorkflowNodeTask={updateWorkflowNodeTask}
          deleteWorkflowNode={deleteWorkflowNode}
          addWorkflowNode={addWorkflowNode}
          addWorkflowEdge={addWorkflowEdge}
          saveWorkflow={saveWorkflow}
          saving={saving}
        />

        <TaskLogsDialog
          taskLogsTarget={taskLogsTarget}
          setTaskLogsTarget={setTaskLogsTarget}
          taskLogs={taskLogs}
          taskLogsLoading={taskLogsLoading}
          taskLogsSelectedId={taskLogsSelectedId}
          setTaskLogsSelectedId={setTaskLogsSelectedId}
        />

        <RunDetailDialog
          selectedRun={selectedRun}
          setSelectedRun={setSelectedRun}
          tasks={tasks}
        />
      </div>
    </TooltipProvider>
  );
}

export default SchedulerPage;
