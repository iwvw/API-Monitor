import { useCallback, useEffect, useMemo, useRef, useState } from 'react';
import { toast } from '../../modules/toast.js';
import { dialog } from '../../modules/dialog.js';
import { formatDateTime } from '../../modules/utils.js';
import { useConfirmPress } from '../../hooks/useConfirmPress.js';
import { getAuthHeaders, activeModelIdsForEndpoint, parseProxyEntry } from './utils.js';

// useEndpoints：端点 CRUD/排序/模型开关与批量/代理池/导入导出/模型映射/模型列表。
export function useEndpoints() {
  const { confirmPress } = useConfirmPress();

  // 批量模型开关（含健康联动关停）的进行中标记。
  const [modelBatchActionLoading, setModelBatchActionLoading] = useState(false);
const [endpoints, setEndpoints] = useState([]);
const [endpointsLoading, setEndpointsLoading] = useState(false);
const [endpointsRefreshing, setEndpointsRefreshing] = useState(false);
const [endpointToggleLoading, setEndpointToggleLoading] = useState({});
const [selectedEndpointId, setSelectedEndpointId] = useState('');
const [endpointReorderSaving, setEndpointReorderSaving] = useState(false);
const [endpointFormOpen, setEndpointFormOpen] = useState(false);
const [editingEndpoint, setEditingEndpoint] = useState(null);
const [endpointForm, setEndpointForm] = useState({
  name: '',
  baseUrl: '',
  apiKey: '',
  apiKeys: [],
  headers: [],
  proxyPool: [],
  autoSwitch: false,
  proxyEnabled: false,
  allowDirectFallback: true,
  rateLimitRetryEnabled: true,
  rateLimitRetryWaitSeconds: 10,
  protocol: 'auto',
});
const [endpointFormError, setEndpointFormError] = useState('');
const [endpointSaving, setEndpointSaving] = useState(false);
// 端点编辑弹窗中的多 key 状态：数组下标与 key 行对齐（0=主 key/K1，n=备用 key/K(n+1)）。
const [endpointKeyChecks, setEndpointKeyChecks] = useState([]);
const [endpointKeyChecking, setEndpointKeyChecking] = useState(false);

// Batch adding endpoints
// Load Endpoints
const loadEndpoints = useCallback(
  async (silent = false) => {
    if (!silent) setEndpointsLoading(true);
    try {
      const response = await fetch('/api/openai/endpoints', {
        headers: getAuthHeaders(),
      });
      const data = await response.json();
      if (Array.isArray(data)) {
        setEndpoints(data.map(ep => ({ ...ep, showKey: false, refreshing: false })));
      }
    } catch (error) {
      console.error('Failed to load endpoints:', error);
      toast.error('加载端点失败');
    } finally {
      if (!silent) setEndpointsLoading(false);
    }
  },
  [getAuthHeaders]
);

useEffect(() => {
  localStorage.removeItem('openai_endpoints_cache');
  loadEndpoints();
}, [loadEndpoints]);
const endpointImportInputRef = useRef(null);
const [endpointImporting, setEndpointImporting] = useState(false);
const [importModeDialog, setImportModeDialog] = useState(null);
const [endpointExporting, setEndpointExporting] = useState(false);

const exportEndpoints = async () => {
  setEndpointExporting(true);
  try {
    const response = await fetch('/api/openai/export', { headers: getAuthHeaders() });
    const payload = await response.json().catch(() => ({}));
    if (!response.ok || payload.success !== true) throw new Error(payload.error || '导出端点失败');
    const list = Array.isArray(payload.endpoints) ? payload.endpoints : [];
    if (list.length === 0) { toast.warning('暂无端点可导出'); return; }
    const blob = new Blob([JSON.stringify(payload, null, 2)], { type: 'application/json' });
    const url = URL.createObjectURL(blob);
    const anchor = document.createElement('a');
    anchor.href = url;
    anchor.download = `openai-endpoints-${new Date().toISOString().slice(0, 10)}.json`;
    document.body.appendChild(anchor);
    anchor.click();
    anchor.remove();
    URL.revokeObjectURL(url);
    toast.success(`已导出 ${list.length} 个端点（包含 API Key，请注意保管）`);
  } catch (error) {
    toast.error(error.message || '导出端点失败');
  } finally {
    setEndpointExporting(false);
  }
};

const importEndpointsFromFile = async (event) => {
  const file = event.target.files?.[0];
  event.target.value = '';
  if (!file) return;
  setEndpointImporting(true);
  try {
    const text = await file.text();
    const data = JSON.parse(text);
    const list = Array.isArray(data) ? data : (data.endpoints || []);
    if (list.length === 0) throw new Error('文件中没有端点数据');
    // 弹出模式选择：覆盖导入（替换全部）或跳过已有（仅新增）
    setImportModeDialog({ list, count: list.length });
  } catch (error) {
    toast.error(error.message || '导入端点失败');
  } finally {
    setEndpointImporting(false);
  }
};

const runEndpointImport = async (list, overwrite) => {
  setImportModeDialog(null);
  setEndpointImporting(true);
  try {
    const response = await fetch('/api/openai/import', {
      method: 'POST',
      headers: { ...getAuthHeaders(), 'Content-Type': 'application/json' },
      body: JSON.stringify({ endpoints: list, overwrite }),
    });
    const payload = await response.json().catch(() => ({}));
    if (!response.ok || payload.success !== true) throw new Error(payload.error || '导入端点失败');
    await loadEndpoints(true);
    toast.success(overwrite
      ? `覆盖导入完成：${payload.imported ?? 0} 个端点已替换`
      : `导入完成：新增 ${payload.imported ?? 0} 个，跳过 ${payload.skipped ?? 0} 个`);
  } catch (error) {
    toast.error(error.message || '导入端点失败');
  } finally {
    setEndpointImporting(false);
  }
};


const selectedEndpoint = useMemo(
  () => endpoints.find(endpoint => endpoint.id === selectedEndpointId) || endpoints[0] || null,
  [endpoints, selectedEndpointId]
);

// 实际启用（未被禁用）的模型总数，跨启用端点去重。
const enabledModelCount = useMemo(() => {
  const ids = new Set();
  endpoints
    .filter(endpoint => endpoint.enabled)
    .forEach(endpoint => activeModelIdsForEndpoint(endpoint).forEach(id => ids.add(id)));
  return ids.size;
}, [endpoints]);
useEffect(() => {
  if (endpoints.length === 0) {
    setSelectedEndpointId('');
    return;
  }
  if (!endpoints.some(endpoint => endpoint.id === selectedEndpointId)) {
    setSelectedEndpointId(endpoints[0].id);
  }
}, [endpoints, selectedEndpointId]);
const verifyEndpoint = async endpoint => {
  try {
    toast.info(`正在验证 ${endpoint.name || '端点'}...`, { isManual: true });
    const response = await fetch(`/api/openai/endpoints/${endpoint.id}/verify`, {
      method: 'POST',
      headers: getAuthHeaders(),
    });
    const data = await response.json();
    if (data.valid) {
      toast.success(`验证成功！找到 ${data.modelsCount || 0} 个模型`);
      await loadEndpoints(true);
    } else {
      toast.error('验证失败: ' + (data.error || 'API Key 无效'));
    }
  } catch (error) {
    toast.error('验证失败: ' + error.message);
  }
};

const refreshEndpointModels = async endpoint => {
  if (endpoint.refreshing) return;
  // Set local refreshing
  setEndpoints(prev => prev.map(e => (e.id === endpoint.id ? { ...e, refreshing: true } : e)));
  try {
    const response = await fetch(`/api/openai/endpoints/${endpoint.id}/verify`, {
      method: 'POST',
      headers: getAuthHeaders(),
    });
    const data = await response.json();
    if (data.valid) {
      toast.success(`${endpoint.name || '端点'} 模型列表已更新`);
      await loadEndpoints(true);
    } else {
      toast.error('刷新失败: ' + (data.error || 'API Key 无效'));
    }
  } catch (error) {
    toast.error('刷新失败: ' + error.message);
  } finally {
    setEndpoints(prev => prev.map(e => (e.id === endpoint.id ? { ...e, refreshing: false } : e)));
  }
};

const refreshAllEndpoints = async () => {
  setEndpointsRefreshing(true);
  try {
    const response = await fetch('/api/openai/endpoints/refresh', {
      method: 'POST',
      headers: getAuthHeaders(),
    });
    const data = await response.json();
    if (data.success) {
      const successCount = data.results?.filter(r => r.success).length || 0;
      toast.success(`刷新完成！已更新 ${successCount} 个启用端点`);
      await loadEndpoints(true);
    } else {
      toast.error('刷新失败: ' + (data.error || '未知错误'));
    }
  } catch (error) {
    toast.error('刷新失败: ' + error.message);
  } finally {
    setEndpointsRefreshing(false);
  }
};

const toggleEndpointEnabled = async endpoint => {
  if (endpointToggleLoading[endpoint.id]) return;
  const updatedEnabled = !endpoint.enabled;
  setEndpointToggleLoading(prev => ({ ...prev, [endpoint.id]: true }));
  try {
    const response = await fetch(`/api/openai/endpoints/${endpoint.id}/toggle`, {
      method: 'POST',
      headers: getAuthHeaders(),
      body: JSON.stringify({ enabled: updatedEnabled }),
    });
    const data = await response.json().catch(() => ({}));
    if (!response.ok || !data.success) throw new Error(data.error || '未知错误');

    const confirmedEnabled = Boolean(data.enabled);
    setEndpoints(prev =>
      prev.map(e => (e.id === endpoint.id ? { ...e, enabled: confirmedEnabled } : e))
    );
    const endpointName = endpoint.name || '端点';
    toast.success(confirmedEnabled ? `${endpointName} 已启用` : `${endpointName} 已停用`);
    await loadAllModels(true);
  } catch (error) {
    toast.error('操作失败: ' + error.message);
  } finally {
    setEndpointToggleLoading(prev => ({ ...prev, [endpoint.id]: false }));
  }
};

// 保存端点路由优先级/权重：PUT /api/openai/endpoints/:id/routing（照搬模型映射模式）。
const saveEndpointRouting = async (endpointId, field, value) => {
  setRoutingEditKey(null);
  const payload = field === 'priority' ? { priority: value } : { weight: value };
  try {
    const res = await fetch(`/api/openai/endpoints/${endpointId}/routing`, {
      method: 'PUT',
      headers: getAuthHeaders(),
      body: JSON.stringify(payload),
    });
    const data = await res.json().catch(() => ({}));
    if (!res.ok || !data.success) throw new Error(data.error || '保存失败');
    setEndpoints(prev =>
      prev.map(e =>
        e.id === endpointId
          ? {
              ...e,
              priority: typeof data.priority === 'number' ? data.priority : e.priority,
              weight: typeof data.weight === 'number' ? data.weight : e.weight,
            }
          : e
      )
    );
  } catch (error) {
    toast.error('路由设置保存失败: ' + error.message);
  }
};

// 端点列表拖拽排序：本地先更新顺序，再持久化到后端；失败时回滚。
const saveEndpointOrder = async nextEndpoints => {
  const orderedIds = nextEndpoints.map(ep => ep.id);
  setEndpointReorderSaving(true);
  try {
    const response = await fetch('/api/openai/endpoints/reorder', {
      method: 'POST',
      headers: { ...getAuthHeaders(), 'Content-Type': 'application/json' },
      body: JSON.stringify({ endpointIds: orderedIds }),
    });
    const data = await response.json().catch(() => ({}));
    if (!response.ok || !data.success) throw new Error(data.error || '保存失败');
    toast.success('端点顺序已保存');
  } catch (error) {
    toast.error('排序保存失败: ' + error.message);
    await loadEndpoints(true);
  } finally {
    setEndpointReorderSaving(false);
  }
};

// 端点列表按钮式排序：上移/下移后本地更新顺序并持久化（排序键 sort_order，刷新保持）。
const moveEndpoint = async (index, direction) => {
  const target = index + direction;
  const max = endpoints.length - 1;
  if (target < 0 || target > max) return;
  const next = [...endpoints];
  const [moved] = next.splice(index, 1);
  next.splice(target, 0, moved);
  setEndpoints(next);
  await saveEndpointOrder(next);
};

// 模型开关的进行中标记：ref 用于同步去重，state 用于驱动按钮禁用态渲染。
const modelSwitchLoadingRef = useRef({});
const [modelSwitchLoading, setModelSwitchLoading] = useState({});

const toggleModelEnabled = async (endpoint, modelId, enabled, silent = false, skipReload = false) => {
  const key = `${endpoint.id}:${modelId}`;
  if (modelSwitchLoadingRef.current[key]) return;
  modelSwitchLoadingRef.current[key] = true;
  setModelSwitchLoading(prev => ({ ...prev, [key]: true }));
  const prevDisabled = Array.isArray(endpoint.disabledModels) ? endpoint.disabledModels : [];
  // 乐观更新：立即切换开关状态，无需等待后端往返。
  setEndpoints(prev =>
    prev.map(e =>
      e.id === endpoint.id
        ? {
            ...e,
            disabledModels: enabled
              ? (Array.isArray(e.disabledModels) ? e.disabledModels : []).filter(id => id !== modelId)
              : [
                  ...(Array.isArray(e.disabledModels) ? e.disabledModels : []).filter(
                    id => id !== modelId
                  ),
                  modelId,
                ],
          }
        : e
    )
  );
  try {
    const response = await fetch(`/api/openai/endpoints/${endpoint.id}/models/toggle`, {
      method: 'POST',
      headers: getAuthHeaders(),
      body: JSON.stringify({ model: modelId, enabled }),
    });
    const data = await response.json().catch(() => ({}));
    if (!response.ok || !data.success) throw new Error(data.error || '更新失败');
    setEndpoints(prev =>
      prev.map(e =>
        e.id === endpoint.id
          ? { ...e, disabledModels: Array.isArray(data.disabledModels) ? data.disabledModels : [] }
          : e
      )
    );
    if (!silent) toast.success(enabled ? `${modelId} 已启用` : `${modelId} 已停用`);
    if (!skipReload) await loadAllModels(true);
  } catch (error) {
    setEndpoints(prev =>
      prev.map(e => (e.id === endpoint.id ? { ...e, disabledModels: prevDisabled } : e))
    );
    toast.error(`更新模型状态失败: ${error.message}`);
  } finally {
    modelSwitchLoadingRef.current[key] = false;
    setModelSwitchLoading(prev => ({ ...prev, [key]: false }));
  }
};

const modelEnabledForEndpoint = (endpoint, modelId) => {
  const disabled = Array.isArray(endpoint?.disabledModels) ? endpoint.disabledModels : [];
  return !disabled.includes(modelId);
};

const openAddEndpointModal = () => {
  setEditingEndpoint(null);
  setEndpointForm({
    name: '',
    baseUrl: '',
    apiKey: '',
    apiKeys: [],
    headers: [],
    proxyPool: [],
    proxyBatches: [],
    autoSwitch: false,
    proxyEnabled: false,
    allowDirectFallback: true,
    rateLimitRetryEnabled: true,
    rateLimitRetryWaitSeconds: 10,
    protocol: 'auto',
  });
  setEndpointFormError('');
  setEndpointFormOpen(true);
  setEndpointKeyChecks([]);
};

const openEditEndpointModal = endpoint => {
  setEditingEndpoint(endpoint);
  setEndpointForm({
    name: endpoint.name || '',
    baseUrl: endpoint.baseUrl || '',
    apiKey: endpoint.apiKey || '',
    apiKeys: Array.isArray(endpoint.apiKeys) ? endpoint.apiKeys : [],
    headers: Array.isArray(endpoint.headers) ? endpoint.headers : [],
    proxyPool: Array.isArray(endpoint.proxyPool) ? endpoint.proxyPool : [],
    proxyBatches: Array.isArray(endpoint.proxyBatches) ? endpoint.proxyBatches : [],
    autoSwitch: Boolean(endpoint.autoSwitch),
    proxyEnabled: Boolean(endpoint.proxyEnabled),
    allowDirectFallback: !endpoint.forceProxy,
    rateLimitRetryEnabled: endpoint.rateLimitRetryEnabled !== false,
    rateLimitRetryWaitSeconds: endpoint.rateLimitRetryWaitSeconds || 10,
    protocol: endpoint.protocol || 'auto',
  });
  setEndpointFormError('');
  setEndpointFormOpen(true);
  setEndpointKeyChecks([]);
  checkEndpointKeys(
    [endpoint.apiKey || '', ...(Array.isArray(endpoint.apiKeys) ? endpoint.apiKeys : [])],
    endpoint.id
  );
};

const updateEndpointProxy = (index, value) => {
  setEndpointForm(current => {
    const proxyPool = (current.proxyPool || []).map((proxy, i) => (i === index ? value : proxy));
    return { ...current, proxyPool };
  });
};

const addEndpointProxy = () => {
  setEndpointForm(current => ({
    ...current,
    proxyPool: [...(current.proxyPool || []), ''],
  }));
};

const removeEndpointProxy = index => {
  setEndpointForm(current => ({
    ...current,
    proxyPool: (current.proxyPool || []).filter((_, i) => i !== index),
  }));
};

const [proxyBatchOpen, setProxyBatchOpen] = useState(false);
const [proxyBatchText, setProxyBatchText] = useState('');
const [proxyImportLoading, setProxyImportLoading] = useState(false);
const [subscriptionUrlOpen, setSubscriptionUrlOpen] = useState(false);
const [subscriptionUrl, setSubscriptionUrl] = useState('');
// editingProxyIndex 标记当前正在编辑完整 URL 的代理条目索引；-1 表示无。
const [editingProxyIndex, setEditingProxyIndex] = useState(-1);
// proxyManagerOpen 控制「出口代理池」独立管理弹窗。
const [proxyManagerOpen, setProxyManagerOpen] = useState(false);

// manualProxyEntries：池中不属于任何导入批次的代理（手动添加），
// 携带真实池下标，供管理弹窗内编辑与删除。
const manualProxyEntries = useMemo(() => {
  const batchUrls = new Set((endpointForm.proxyBatches || []).flatMap(batch => batch.proxies || []));
  return (endpointForm.proxyPool || [])
    .map((proxy, index) => ({ proxy, index }))
    .filter(({ proxy }) => !batchUrls.has(proxy));
}, [endpointForm.proxyPool, endpointForm.proxyBatches]);

const saveProxyBatch = () => {
  const lines = proxyBatchText
    .split(/\r?\n/)
    .map(line => line.trim())
    .filter(Boolean);
  if (lines.length === 0) {
    toast.warning('粘贴至少一个代理地址');
    return;
  }
  const added = addProxyBatch(`批量添加 ${new Date().toLocaleTimeString('zh-CN', { hour: '2-digit', minute: '2-digit' })}`, lines);
  if (added === 0) {
    toast.info('粘贴的代理已全部属于其他批次，无需重复添加', { isManual: true });
    return;
  }
  setProxyBatchText('');
  setProxyBatchOpen(false);
  toast.success(`已批量添加 ${added} 个代理`);
};

// addProxyBatch 把一批代理（文件/订阅/批量粘贴）登记为一个「批次」：
//   a) 池中尚不存在 → 新增并入池；
//   b) 已在池中但无任何批次归属（历史导入/手动加入的同 URL）→ 一并记入本批次；
//   c) 已属于其他批次 → 跳过，避免两个批次拥有同一 URL 导致删除互相牵连。
// 这样旧数据（批次功能上线前导入的池）重新导入同一来源即可获得批次管理能力。
// 返回本次归入批次的条数（0 表示无新增）。
const addProxyBatch = (batchName, urls) => {
  const list = (Array.isArray(urls) ? urls : []).filter(Boolean);
  if (list.length === 0) return 0;
  const pool = endpointForm.proxyPool || [];
  const poolSet = new Set(pool);
  const owned = new Set((endpointForm.proxyBatches || []).flatMap(batch => batch.proxies || []));
  const fresh = list.filter(proxy => !poolSet.has(proxy));
  const newlyOwned = list.filter(proxy => poolSet.has(proxy) && !owned.has(proxy));
  const batchProxies = [...fresh, ...newlyOwned];
  if (batchProxies.length === 0) return 0;
  const batchId = `pb_${Date.now().toString(36)}${Math.random().toString(36).slice(2, 8)}`;
  setEndpointForm(current => ({
    ...current,
    proxyPool: [...pool, ...fresh],
    proxyBatches: [
      ...(current.proxyBatches || []),
      {
        id: batchId,
        name: batchName,
        createdAt: new Date().toISOString(),
        proxies: batchProxies,
      },
    ],
  }));
  return batchProxies.length;
};

// 文件导入：读取本地代理列表文件（.txt，每行一个代理），交给后端解析清洗后，
// 以文件为单位建立一个「批次」追加到池，便于之后按文件批量删除/管理。
const proxyFileInputRef = useRef(null);
const importProxyFile = file => {
  if (!file) return;
  const reader = new FileReader();
  reader.onload = async e => {
    const text = String(e.target?.result || '');
    if (!text.trim()) {
      toast.warning('文件内容为空');
      return;
    }
    const rawLineCount = text.split(/\r?\n/).map(l => l.trim()).filter(Boolean).length;
    if (proxyImportLoading) return;
    setProxyImportLoading(true);
    try {
      const response = await fetch('/api/openai/proxies/import-list', {
        method: 'POST',
        headers: { ...getAuthHeaders(), 'Content-Type': 'application/json' },
        body: JSON.stringify({ text }),
      });
      const data = await response.json().catch(() => ({}));
      if (!response.ok) throw new Error(data.error || `HTTP ${response.status}`);
      const list = Array.isArray(data.proxies) ? data.proxies : [];
      if (list.length === 0) {
        toast.info('文件中没有找到可导入的代理（支持 http(s)://、socks5://、host:port）', { isManual: true });
        return;
      }
      const batchName = file.name || '代理列表';
      const added = addProxyBatch(batchName, list);
      if (added === 0) {
        toast.info(`文件中的 ${list.length} 个代理已全部属于其他批次，无需重复导入`, { isManual: true });
        return;
      }
      const skipped = rawLineCount - list.length;
      toast.success(
        `已导入批次「${batchName}」${added} 条${skipped > 0 ? `（跳过 ${skipped} 行无效/重复）` : ''}`,
      );
    } catch (err) {
      toast.error(err.message || '文件导入失败');
    } finally {
      setProxyImportLoading(false);
      if (proxyFileInputRef.current) proxyFileInputRef.current.value = '';
    }
  };
  reader.readAsText(file);
};

// 批次管理：展开预览 / 删除整批 / 移出单条。
const [expandedBatchId, setExpandedBatchId] = useState(null);
const [manualProxyExpanded, setManualProxyExpanded] = useState(false);
// proxyRuntimeStates：代理池各出口的运行时禁用状态（冷却 / 429 冻结），
// 用于在管理弹窗里把被禁用的代理标红。key 为代理 URL。
const [proxyRuntimeStates, setProxyRuntimeStates] = useState({});
useEffect(() => {
  if (!proxyManagerOpen || !editingEndpoint?.id) return;
  let cancelled = false;
  const load = async () => {
    try {
      const response = await fetch(`/api/openai/endpoints/${editingEndpoint.id}/proxy-state`, {
        headers: getAuthHeaders(),
      });
      const data = await response.json().catch(() => ({}));
      if (!response.ok || !Array.isArray(data.proxies)) return;
      if (!cancelled) {
        const map = {};
        data.proxies.forEach(item => {
          map[item.proxy] = item;
        });
        setProxyRuntimeStates(map);
      }
    } catch {
      // 状态加载失败不阻断弹窗使用。
    }
  };
  load();
  const timer = setInterval(load, 15000);
  return () => {
    cancelled = true;
    clearInterval(timer);
  };
}, [proxyManagerOpen, editingEndpoint?.id]);
// disabledProxyUntil 返回某代理的禁用信息：{label, disableUntil} 或 null（可用）。
const disabledProxyUntil = proxy => {
  const item = proxyRuntimeStates[proxy];
  if (!item) return null;
  if (item.rateLimitedUntil && new Date(item.rateLimitedUntil).getTime() > Date.now()) {
    return { label: `429 冻结至 ${formatDateTime(item.rateLimitedUntil)}`, until: item.rateLimitedUntil };
  }
  if (item.cooldownUntil && new Date(item.cooldownUntil).getTime() > Date.now()) {
    return { label: `连接失败冷却至 ${formatDateTime(item.cooldownUntil)}`, until: item.cooldownUntil };
  }
  if (item.sunkUntil && new Date(item.sunkUntil).getTime() > Date.now()) {
    return { label: `坏代理沉淀至 ${formatDateTime(item.sunkUntil)}`, until: item.sunkUntil };
  }
  return null;
};
// disabledProxyCount 返回当前被禁用（冷却/冻结/沉淀）的代理条数。
const disabledProxyCount = useMemo(() => {
  if (!editingEndpoint?.id) return 0;
  return (endpointForm.proxyPool || []).filter(proxy => disabledProxyUntil(proxy)).length;
}, [endpointForm.proxyPool, proxyRuntimeStates]);
// unbanAllProxies 一键解封端点代理池全部出口：清除冷却/429 冻结/坏代理沉淀，
// 解封后重新拉取运行时状态使 UI 同步。
const [unbanningProxies, setUnbanningProxies] = useState(false);
const unbanAllProxies = async () => {
  if (!editingEndpoint?.id || unbanningProxies) return;
  setUnbanningProxies(true);
  try {
    const response = await fetch(`/api/openai/endpoints/${editingEndpoint.id}/proxy-state/unban`, {
      method: 'POST',
      headers: getAuthHeaders(),
    });
    const data = await response.json().catch(() => ({}));
    if (!response.ok || !data.success) throw new Error(data.error || `HTTP ${response.status}`);
    toast.success(data.cleared ? `已解封 ${data.cleared} 条代理` : '代理池无被禁用的出口');
    const stateRes = await fetch(`/api/openai/endpoints/${editingEndpoint.id}/proxy-state`, {
      headers: getAuthHeaders(),
    });
    const stateData = await stateRes.json().catch(() => ({}));
    if (stateRes.ok && Array.isArray(stateData.proxies)) {
      const map = {};
      stateData.proxies.forEach(item => {
        map[item.proxy] = item;
      });
      setProxyRuntimeStates(map);
    }
  } catch (error) {
    toast.error('解封失败: ' + error.message);
  } finally {
    setUnbanningProxies(false);
  }
};
// probeAllProxies 对端点代理池全部出口发起一次手动探活，完成后刷新运行时状态。
const [probingProxies, setProbingProxies] = useState(false);
const probeAllProxies = async () => {
  if (!editingEndpoint?.id || probingProxies) return;
  setProbingProxies(true);
  try {
    const response = await fetch(`/api/openai/endpoints/${editingEndpoint.id}/proxy-state/probe`, {
      method: 'POST',
      headers: getAuthHeaders(),
    });
    const data = await response.json().catch(() => ({}));
    if (!response.ok || !data.success) throw new Error(data.error || `HTTP ${response.status}`);
    toast.success(data.probed ? `已探测 ${data.probed} 条代理，可达 ${data.reachable} 条` : '代理池为空');
    const stateRes = await fetch(`/api/openai/endpoints/${editingEndpoint.id}/proxy-state`, {
      headers: getAuthHeaders(),
    });
    const stateData = await stateRes.json().catch(() => ({}));
    if (stateRes.ok && Array.isArray(stateData.proxies)) {
      const map = {};
      stateData.proxies.forEach(item => {
        map[item.proxy] = item;
      });
      setProxyRuntimeStates(map);
    }
  } catch (error) {
    toast.error('批量测试失败: ' + error.message);
  } finally {
    setProbingProxies(false);
  }
};
const removeProxyBatch = batch => {
  if (!confirmPress(`proxy-batch:${batch.id}`, `移除文件批次「${batch.name}」及其全部 ${batch.proxies.length} 条代理？`)) return;
  const members = new Set(batch.proxies || []);
  setEndpointForm(current => ({
    ...current,
    proxyPool: (current.proxyPool || []).filter(proxy => !members.has(proxy)),
    proxyBatches: (current.proxyBatches || []).filter(item => item.id !== batch.id),
  }));
  toast.success(`已移除批次「${batch.name}」的 ${batch.proxies.length} 条代理`);
};
const removeProxyFromBatch = (batch, proxy) => {
  setEndpointForm(current => {
    const batches = (current.proxyBatches || [])
      .map(item =>
        item.id === batch.id
          ? { ...item, proxies: (item.proxies || []).filter(p => p !== proxy) }
          : item,
      )
      .filter(item => item.id !== batch.id || (item.proxies || []).length > 0);
    return {
      ...current,
      proxyPool: (current.proxyPool || []).filter(item => item !== proxy),
      proxyBatches: batches,
    };
  });
};

// resolveSubscriptionProxies 通过后端拉取并解析订阅链接中的 socks/http 节点。
const resolveSubscriptionProxies = async () => {
  const url = subscriptionUrl.trim();
  if (!url) {
    toast.warning('填写订阅链接');
    return;
  }
  if (proxyImportLoading) return;
  setProxyImportLoading(true);
  try {
    const response = await fetch('/api/openai/proxies/resolve-subscription', {
      method: 'POST',
      headers: { ...getAuthHeaders(), 'Content-Type': 'application/json' },
      body: JSON.stringify({ url }),
    });
    const data = await response.json().catch(() => ({}));
    if (!response.ok) throw new Error(data.error || `HTTP ${response.status}`);
    const list = Array.isArray(data.proxies) ? data.proxies : [];
    if (list.length === 0) {
      toast.info(data.message || '订阅内容中没有找到 socks/http 节点', { isManual: true });
      return;
    }
    let batchName = url;
    try {
      batchName = `订阅 ${new URL(url).hostname}`;
    } catch {
      // URL 解析失败时退化为完整链接。
    }
    const added = addProxyBatch(batchName, list.map(item => item.proxy).filter(Boolean));
    if (added === 0) {
      toast.info('订阅链接中的代理已全部属于其他批次，无需重复导入', { isManual: true });
    } else {
      toast.success(`已从订阅链接导入 ${added} 个代理`);
    }
    setSubscriptionUrl('');
    setSubscriptionUrlOpen(false);
  } catch (error) {
    toast.error('解析订阅链接失败: ' + error.message);
  } finally {
    setProxyImportLoading(false);
  }
};

const updateEndpointHeader = (index, field, value) => {
  setEndpointForm(current => {
    const headers = (current.headers || []).map((header, i) =>
      i === index ? { ...header, [field]: value } : header
    );
    return { ...current, headers };
  });
};

const addEndpointHeader = () => {
  setEndpointForm(current => ({
    ...current,
    headers: [...(current.headers || []), { name: '', value: '' }],
  }));
};

const removeEndpointHeader = index => {
  setEndpointForm(current => ({
    ...current,
    headers: (current.headers || []).filter((_, i) => i !== index),
  }));
};

const saveEndpoint = async () => {
  if (!endpointForm.baseUrl) {
    setEndpointFormError('填写 API 地址');
    return;
  }
  setEndpointSaving(true);
  setEndpointFormError('');
  try {
    const url = editingEndpoint
      ? `/api/openai/endpoints/${editingEndpoint.id}`
      : '/api/openai/endpoints';
    const apiKeys = (endpointForm.apiKeys || []).map(k => (k || '').trim()).filter(Boolean);
    const response = await fetch(url, {
      method: editingEndpoint ? 'PUT' : 'POST',
      headers: getAuthHeaders(),
      body: JSON.stringify({ ...endpointForm, apiKeys, forceProxy: !endpointForm.allowDirectFallback }),
    });
    const data = await response.json();
    if (response.ok && (data.success || data.endpoint || data.id)) {
      toast.success(editingEndpoint ? '端点已更新' : '端点已添加');
      setEndpointFormOpen(false);
      await loadEndpoints(true);
      loadAllModels(true);
    } else {
      setEndpointFormError(data.error || '保存失败');
    }
  } catch (error) {
    setEndpointFormError('保存失败: ' + error.message);
  } finally {
    setEndpointSaving(false);
  }
};

// 对端点多 key 逐个做有效性检测（调后端 GET /models 判定）。keysArray 与弹窗行对齐：
// 第 0 项=主 key（K1），后续项=备用 key（K2...），空值行跳过但仍占据对应下标。
const checkEndpointKeys = useCallback(async (keysArray, endpointId) => {
  const rows = keysArray.map(k => (k || '').trim());
  const entries = rows
    .map((key, rowIndex) => ({ rowIndex, key }))
    .filter(e => e.key !== '');
  if (!endpointId) {
    return;
  }
  if (entries.length === 0) {
    setEndpointKeyChecks(Array(rows.length).fill(null));
    return;
  }
  setEndpointKeyChecking(true);
  setEndpointKeyChecks(rows.map(() => ({ status: 'checking' })));
  try {
    const response = await fetch(`/api/openai/endpoints/${endpointId}/key-check`, {
      method: 'POST',
      headers: getAuthHeaders(),
      body: JSON.stringify({
        keys: entries.map(e => e.key),
        timeout: 10000,
      }),
    });
    const data = await response.json();
    if (!response.ok) {
      throw new Error(data.error || '检测失败');
    }
    const next = Array(rows.length).fill(null);
    (data.results || []).forEach((result, idx) => {
      const rowIndex = entries[idx]?.rowIndex;
      if (rowIndex != null) next[rowIndex] = result;
    });
    setEndpointKeyChecks(next);
  } catch (error) {
    setEndpointKeyChecks(rows.map(() => ({ status: 'error', message: error.message })));
    toast.error(`Key 检测失败：${error.message}`);
  } finally {
    setEndpointKeyChecking(false);
  }
}, [getAuthHeaders]);

const appendEndpointKey = () => {
  setEndpointForm(current => ({
    ...current,
    apiKeys: [...(current.apiKeys || []), ''],
  }));
  setEndpointKeyChecks(prev => [...prev, null]);
};

const removeEndpointKey = rowIndex => {
  setEndpointForm(current => {
    const keys = [current.apiKey || '', ...(current.apiKeys || [])];
    keys.splice(rowIndex, 1);
    return {
      ...current,
      apiKey: keys[0] || '',
      apiKeys: keys.slice(1),
    };
  });
  setEndpointKeyChecks(prev => {
    const next = [...prev];
    next.splice(rowIndex, 1);
    return next;
  });
};

const [pendingDeleteEndpointId, setPendingDeleteEndpointId] = useState(null);
const DELETE_ENDPOINT_CONFIRM_MS = 3000;
const deleteEndpointConfirmActive = id =>
  pendingDeleteEndpointId?.id === id && pendingDeleteEndpointId.expiresAt > Date.now();

const deleteEndpoint = async endpoint => {
  if (!deleteEndpointConfirmActive(endpoint.id)) {
    setPendingDeleteEndpointId({ id: endpoint.id, expiresAt: Date.now() + DELETE_ENDPOINT_CONFIRM_MS });
    toast.info(`删除端点 ${endpoint.name || endpoint.baseUrl}？再次点击确认`, { isManual: true });
    return;
  }
  setPendingDeleteEndpointId(null);
  try {
    const response = await fetch(`/api/openai/endpoints/${endpoint.id}`, {
      method: 'DELETE',
      headers: getAuthHeaders(),
    });
    const data = await response.json();
    if (response.ok && data.success) {
      toast.success('端点已删除');
      await loadEndpoints(true);
      loadAllModels(true);
    } else {
      toast.error('删除失败: ' + (data.error || '未知错误'));
    }
  } catch (error) {
    toast.error('删除失败: ' + error.message);
  }
};
// ==================== 3. Models List ====================
const [allModels, setAllModels] = useState([]);

const loadAllModels = useCallback(
  async (silent = false) => {
    try {
      const response = await fetch('/api/openai/v1/models', {
        headers: getAuthHeaders(),
      });
      const data = await response.json();
      if (data && Array.isArray(data.data)) {
        const sorted = data.data.sort((a, b) => {
          if (a.owned_by !== b.owned_by) return a.owned_by.localeCompare(b.owned_by);
          return a.id.localeCompare(b.id);
        });
        setAllModels(sorted);
      }
    } catch (error) {
      console.error('Failed to load models list:', error);
    }
  },
  [getAuthHeaders]
);

useEffect(() => {
  loadAllModels(true);
}, [loadAllModels]);
// 模型映射（对外名称）行内编辑状态。
const [mappingEditKey, setMappingEditKey] = useState(null);
const [mappingDraft, setMappingDraft] = useState('');

// 端点路由优先级/权重行内编辑状态（照搬模型映射的编辑模式）。
const [routingEditKey, setRoutingEditKey] = useState(null); // `${endpointId}:priority|weight`
const [routingDraft, setRoutingDraft] = useState('');

// 批量切换端点模型的启用状态（原子接口，避免并发逐个 toggle 丢失）。
const batchToggleEndpointModels = async (endpoint, modelIds, enabled, successMessage) => {
  if (modelBatchActionLoading) return;
  const ids = Array.from(new Set((modelIds || []).filter(Boolean)));
  if (ids.length === 0) return;
  setModelBatchActionLoading(true);
  try {
    const response = await fetch(
      `/api/openai/endpoints/${endpoint.id}/models/toggle-batch`,
      {
        method: 'POST',
        headers: { ...getAuthHeaders(), 'Content-Type': 'application/json' },
        body: JSON.stringify({ models: ids, enabled }),
      }
    );
    const data = await response.json().catch(() => ({}));
    if (!response.ok || !data.success) throw new Error(data.error || '更新失败');
    setEndpoints(prev =>
      prev.map(e =>
        e.id === endpoint.id
          ? { ...e, disabledModels: Array.isArray(data.disabledModels) ? data.disabledModels : [] }
          : e
      )
    );
    await loadAllModels(true);
    toast.success(successMessage || `已${enabled ? '启用' : '关闭'} ${ids.length} 个模型`);
    return true;
  } catch (error) {
    toast.error(`批量更新失败: ${error.message}`);
    return false;
  } finally {
    setModelBatchActionLoading(false);
  }
};

// 关闭端点上所有「非有效」模型（未检测/检测失败/较慢之外的），仅停用不隐藏。
// 保存模型映射：PUT /api/openai/endpoints/:id/model-mappings。
const saveEndpointMapping = async (endpoint, modelId, alias) => {
  setMappingEditKey(null);
  const clean = (alias || '').trim();
  try {
    const res = await fetch(`/api/openai/endpoints/${endpoint.id}/model-mappings`, {
      method: 'PUT',
      headers: getAuthHeaders(),
      body: JSON.stringify({
        mappings: { ...(endpoint.modelMappings || {}), [modelId]: clean },
      }),
    });
    const data = await res.json().catch(() => ({}));
    if (!res.ok || !data.success) throw new Error(data.error || '保存失败');
    toast.success(clean ? `已映射 ${modelId} → ${clean}` : `已清除 ${modelId} 的映射`);
    setEndpoints(prev =>
      prev.map(e => (e.id === endpoint.id ? { ...e, modelMappings: data.modelMappings } : e))
    );
    await loadAllModels(true);
  } catch (error) {
    toast.error('保存映射失败: ' + error.message);
  }
};

// 批量启用被停用的模型（与「关闭检测失败的模型」拆分为两个明确动作）。
const batchEnableDisabledModels = async endpoint => {
  if (modelBatchActionLoading) return;
  const disabled = Array.isArray(endpoint.disabledModels) ? endpoint.disabledModels : [];
  if (disabled.length === 0) return;
  await batchToggleEndpointModels(endpoint, disabled, true, `已启用 ${disabled.length} 个被停用的模型`);
};
  return {
    endpoints,
    setEndpoints,
    endpointsLoading,
    setEndpointsLoading,
    endpointsRefreshing,
    setEndpointsRefreshing,
    endpointToggleLoading,
    setEndpointToggleLoading,
    selectedEndpointId,
    setSelectedEndpointId,
    endpointReorderSaving,
    setEndpointReorderSaving,
    endpointFormOpen,
    setEndpointFormOpen,
    editingEndpoint,
    setEditingEndpoint,
    endpointForm,
    setEndpointForm,
    endpointFormError,
    setEndpointFormError,
    endpointSaving,
    setEndpointSaving,
    endpointKeyChecks,
    setEndpointKeyChecks,
    endpointKeyChecking,
    setEndpointKeyChecking,
    loadEndpoints,
    endpointImportInputRef,
    endpointImporting,
    setEndpointImporting,
    importModeDialog,
    setImportModeDialog,
    endpointExporting,
    setEndpointExporting,
    exportEndpoints,
    importEndpointsFromFile,
    runEndpointImport,
    selectedEndpoint,
    enabledModelCount,
    verifyEndpoint,
    refreshEndpointModels,
    refreshAllEndpoints,
    toggleEndpointEnabled,
    saveEndpointRouting,
    saveEndpointOrder,
    moveEndpoint,
    modelSwitchLoadingRef,
    modelSwitchLoading,
    setModelSwitchLoading,
    toggleModelEnabled,
    modelEnabledForEndpoint,
    openAddEndpointModal,
    openEditEndpointModal,
    updateEndpointProxy,
    addEndpointProxy,
    removeEndpointProxy,
    proxyBatchOpen,
    setProxyBatchOpen,
    proxyBatchText,
    setProxyBatchText,
    proxyImportLoading,
    setProxyImportLoading,
    subscriptionUrlOpen,
    setSubscriptionUrlOpen,
    subscriptionUrl,
    setSubscriptionUrl,
    editingProxyIndex,
    setEditingProxyIndex,
    proxyManagerOpen,
    setProxyManagerOpen,
    manualProxyEntries,
    saveProxyBatch,
    addProxyBatch,
    proxyFileInputRef,
    importProxyFile,
    expandedBatchId,
    setExpandedBatchId,
    manualProxyExpanded,
    setManualProxyExpanded,
    proxyRuntimeStates,
    setProxyRuntimeStates,
    disabledProxyUntil,
    disabledProxyCount,
    unbanningProxies,
    setUnbanningProxies,
    unbanAllProxies,
    probingProxies,
    setProbingProxies,
    probeAllProxies,
    removeProxyBatch,
    removeProxyFromBatch,
    resolveSubscriptionProxies,
    updateEndpointHeader,
    addEndpointHeader,
    removeEndpointHeader,
    saveEndpoint,
    checkEndpointKeys,
    appendEndpointKey,
    removeEndpointKey,
    pendingDeleteEndpointId,
    setPendingDeleteEndpointId,
    DELETE_ENDPOINT_CONFIRM_MS,
    deleteEndpointConfirmActive,
    deleteEndpoint,
    allModels,
    setAllModels,
    loadAllModels,
    mappingEditKey,
    setMappingEditKey,
    mappingDraft,
    setMappingDraft,
    routingEditKey,
    setRoutingEditKey,
    routingDraft,
    setRoutingDraft,
    batchToggleEndpointModels,
    saveEndpointMapping,
    batchEnableDisabledModels,
  };
}
