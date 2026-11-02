import { useCallback, useEffect, useRef, useState } from 'react';
import { toast } from '../../../modules/toast.js';
import { getAuthHeaders } from '../utils.js';

export function useEndpointModels({ setEndpoints }) {
  const [allModels, setAllModels] = useState([]);
  const [modelBatchActionLoading, setModelBatchActionLoading] = useState(false);
  const [modelSwitchLoading, setModelSwitchLoading] = useState({});
  // 模型开关的进行中标记：ref 用于同步去重，state 用于驱动按钮禁用态渲染。
  const modelSwitchLoadingRef = useRef({});
  // 模型映射（对外名称）行内编辑状态。
  const [mappingEditKey, setMappingEditKey] = useState(null);
  const [mappingDraft, setMappingDraft] = useState('');
  // 端点路由优先级/权重行内编辑状态（照搬模型映射的编辑模式）。
  const [routingEditKey, setRoutingEditKey] = useState(null); // `${endpointId}:priority|weight`
  const [routingDraft, setRoutingDraft] = useState('');

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

  // 手动维护端点模型列表（Vertex AI 等无法自动拉取模型列表的上游需要手动添加）。
  // 入参支持一次粘贴多个模型名（逗号/换行/分号分隔）。replace=false 追加合并；
  // replace=true 全量替换（文本框内容即最终列表，删行后提交即可移除）。
  // POST /endpoints/:id/models/add。
  const addEndpointModels = async (endpoint, rawInput, { replace = false } = {}) => {
    const models = String(rawInput || '')
      .split(/[\n,;，；]/)
      .map(m => m.trim())
      .filter(Boolean);
    if (models.length === 0 && !replace) return false;
    try {
      const response = await fetch(`/api/openai/endpoints/${endpoint.id}/models/add`, {
        method: 'POST',
        headers: { ...getAuthHeaders(), 'Content-Type': 'application/json' },
        body: JSON.stringify({ models, replace }),
      });
      const data = await response.json().catch(() => ({}));
      if (!response.ok || !data.success) throw new Error(data.error || '保存失败');
      setEndpoints(prev =>
        prev.map(e =>
          e.id === endpoint.id
            ? {
                ...e,
                models: Array.isArray(data.models) ? data.models : e.models,
                disabledModels: Array.isArray(data.disabledModels)
                  ? data.disabledModels
                  : e.disabledModels,
              }
            : e
        )
      );
      await loadAllModels(true);
      toast.success(
        replace
          ? `模型列表已保存（共 ${Array.isArray(data.models) ? data.models.length : 0} 个）`
          : `已添加 ${Array.isArray(data.models) ? data.models.length : 0} 个模型`
      );
      return true;
    } catch (error) {
      toast.error(replace ? '保存模型列表失败: ' + error.message : '添加模型失败: ' + error.message);
      return false;
    }
  };

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
    allModels,
    setAllModels,
    loadAllModels,
    modelBatchActionLoading,
    setModelBatchActionLoading,
    modelSwitchLoadingRef,
    modelSwitchLoading,
    setModelSwitchLoading,
    toggleModelEnabled,
    modelEnabledForEndpoint,
    batchToggleEndpointModels,
    addEndpointModels,
    saveEndpointMapping,
    batchEnableDisabledModels,
    mappingEditKey,
    setMappingEditKey,
    mappingDraft,
    setMappingDraft,
    routingEditKey,
    setRoutingEditKey,
    routingDraft,
    setRoutingDraft,
  };
}
