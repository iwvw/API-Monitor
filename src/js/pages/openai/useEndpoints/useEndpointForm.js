import { useCallback, useEffect, useMemo, useRef, useState } from 'react';
import { toast } from '../../../modules/toast.js';
import { formatDateTime } from '../../../modules/utils.js';
import { getAuthHeaders } from '../utils.js';

export function useEndpointForm({ loadEndpoints, loadAllModels, confirmState }) {
  const { isArmed, confirmPress } = confirmState;
  const [editingEndpoint, setEditingEndpoint] = useState(null);
  const [endpointFormOpen, setEndpointFormOpen] = useState(false);
  const [endpointForm, setEndpointForm] = useState({
    name: '',
    baseUrl: '',
    modelsUrl: '',
    apiKey: '',
    apiKeys: [],
    headers: [],
    proxyPool: [],
    autoSwitch: false,
    proxyEnabled: false,
    allowDirectFallback: true,
    rateLimitRetryEnabled: true,
    rateLimitRetryWaitSeconds: 10,
    keyRetryRounds: 2,
    protocol: 'auto',
  });
  const [endpointFormError, setEndpointFormError] = useState('');
  const [endpointSaving, setEndpointSaving] = useState(false);
  // 端点编辑弹窗中的多 key 状态：数组下标与 key 行对齐（0=主 key/K1，n=备用 key/K(n+1)）。
  const [endpointKeyChecks, setEndpointKeyChecks] = useState([]);
  const [endpointKeyChecking, setEndpointKeyChecking] = useState(false);
  const [endpointKeyEpoch, setEndpointKeyEpoch] = useState(0);

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

  const openAddEndpointModal = () => {
    setEditingEndpoint(null);
    setEndpointForm({
      name: '',
      baseUrl: '',
      modelsUrl: '',
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
      keyRetryRounds: 2,
      protocol: 'auto',
      upstreamType: 'openai',
      proxyPoolId: '',
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
      modelsUrl: endpoint.modelsUrl || '',
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
      keyRetryRounds: endpoint.keyRetryRounds || 2,
      protocol: endpoint.protocol || 'auto',
      upstreamType: endpoint.upstreamType || 'openai',
      proxyPoolId: endpoint.proxyPoolId || '',
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

  const appendEndpointKey = () => {
    setEndpointForm(current => ({
      ...current,
      apiKeys: [...(current.apiKeys || []), ''],
    }));
    setEndpointKeyChecks(prev => [...prev, null]);
  };

  const keyDeleteConfirmActive = rowIndex =>
    isArmed(`openai-endpoint-key:${endpointKeyEpoch}:${rowIndex}`);

  const removeEndpointKey = rowIndex => {
    if (!confirmPress(`openai-endpoint-key:${endpointKeyEpoch}:${rowIndex}`, '删除此 Key')) return;
    setEndpointKeyEpoch(epoch => epoch + 1);
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

  const deleteEndpointConfirmActive = id =>
    isArmed(`openai-endpoint-delete:${id}`);

  return {
    editingEndpoint,
    setEditingEndpoint,
    endpointFormOpen,
    setEndpointFormOpen,
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
    keyDeleteConfirmActive,
    deleteEndpointConfirmActive,
  };
}
