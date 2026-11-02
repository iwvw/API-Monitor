import { useCallback, useEffect, useMemo, useRef, useState } from 'react';
import { toast } from '../../../modules/toast.js';
import { getAuthHeaders, activeModelIdsForEndpoint } from '../utils.js';

// sortEndpoints：端点列表统一排序规则——插件端点（pluginId 存在）始终置顶，
// 内部保持原始相对顺序；非插件端点按路由优先级 priority 降序、同级按 weight 降序。
// loadEndpoints 与 saveEndpointRouting 共用，避免优先级调整打乱插件置顶。
export function sortEndpoints(list) {
  return [...(Array.isArray(list) ? list : [])].sort((a, b) => {
    const pa = a?.pluginId ? 1 : 0;
    const pb = b?.pluginId ? 1 : 0;
    if (pa !== pb) return pb - pa;
    return (b.priority ?? 0) - (a.priority ?? 0) || (b.weight ?? 0) - (a.weight ?? 0);
  });
}

export function useEndpointCrud({ loadAllModels, routingEditKeyState, confirmState }) {
  const { isArmed, confirmPress } = confirmState;
  const [endpoints, setEndpoints] = useState([]);
  const [endpointsLoading, setEndpointsLoading] = useState(false);
  const [endpointsRefreshing, setEndpointsRefreshing] = useState(false);
  const [endpointToggleLoading, setEndpointToggleLoading] = useState({});
  const [selectedEndpointId, setSelectedEndpointId] = useState('');

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
          // 插件端点置顶，非插件按后端顺序（priority/weight/sort_order）排列。
          setEndpoints(sortEndpoints(data).map(ep => ({ ...ep, showKey: false, refreshing: false })));
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

  const refreshEndpointModels = async (endpoint, silent = false) => {
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
        if (!silent) toast.success(`${endpoint.name || '端点'} 模型列表已更新`);
        await loadEndpoints(true);
      } else {
        if (!silent) toast.error('刷新失败: ' + (data.error || 'API Key 无效'));
      }
    } catch (error) {
      if (!silent) toast.error('刷新失败: ' + error.message);
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
    const { setRoutingEditKey } = routingEditKeyState;
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
        sortEndpoints(
          prev.map(e =>
            e.id === endpointId
              ? {
                  ...e,
                  priority: typeof data.priority === 'number' ? data.priority : e.priority,
                  weight: typeof data.weight === 'number' ? data.weight : e.weight,
                }
              : e
          )
        )
      );
    } catch (error) {
      toast.error('路由设置保存失败: ' + error.message);
    }
  };

  const deleteEndpointConfirmActive = id =>
    isArmed(`openai-endpoint-delete:${id}`);

  const deleteEndpoint = async endpoint => {
    if (!confirmPress(`openai-endpoint-delete:${endpoint.id}`, `删除端点 ${endpoint.name || endpoint.baseUrl}`)) return;
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
    deleteEndpointConfirmActive,
    deleteEndpoint,
  };
}
