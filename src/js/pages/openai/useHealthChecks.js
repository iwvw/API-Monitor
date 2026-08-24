import { useEffect, useRef, useState } from 'react';
import { toast } from '../../modules/toast.js';
import {
  DEFAULT_MODEL_HEALTH_CONCURRENCY,
  DEFAULT_MODEL_HEALTH_TIMEOUT_SECONDS,
  countModelHealthResults,
  endpointModelIds,
  modelHealthKey,
  modelHealthTargets,
  normalizeModelHealthRecord,
  resolveModelHealthConcurrency,
} from '../../modules/openaiModelHealth.js';
import { createHealthCheckProgress, getAuthHeaders } from './utils.js';

// useHealthChecks：端点模型健康检测（单测/批量/进度/abort 管理）。
export function useHealthChecks({ endpoints, selectedEndpointId }) {
const [openaiModelHealth, setOpenaiModelHealth] = useState(() => {
  try {
    const saved = localStorage.getItem('openai_model_health_cache');
    return saved ? JSON.parse(saved) : {};
  } catch {
    return {};
  }
});

useEffect(() => {
  localStorage.setItem('openai_model_health_cache', JSON.stringify(openaiModelHealth));
}, [openaiModelHealth]);

const [modelHealthBatchLoading, setModelHealthBatchLoading] = useState(false);
const [healthCheckProgress, setHealthCheckProgress] = useState(() => createHealthCheckProgress());
const [healthCheckModal, setHealthCheckModal] = useState(false);
const [healthCheckForm, setHealthCheckForm] = useState({
  timeout: DEFAULT_MODEL_HEALTH_TIMEOUT_SECONDS,
  concurrency: DEFAULT_MODEL_HEALTH_CONCURRENCY,
});
const modelHealthAbortControllersRef = useRef(new Map());
// 批量检测进行中请求：切换端点时 abort，避免旧端点的检测状态带偏新端点。
const batchHealthAbortRef = useRef(null);

// 切换选中端点时立即终止该端点所有检测（单个 + 批量），并清理「检测中」状态。
useEffect(() => {
  modelHealthAbortControllersRef.current.forEach(controller => controller.abort());
  modelHealthAbortControllersRef.current.clear();
  batchHealthAbortRef.current?.abort();
  batchHealthAbortRef.current = null;
  setModelHealthBatchLoading(false);
  setOpenaiModelHealth(prev => {
    const next = {};
    for (const [key, record] of Object.entries(prev)) {
      next[key] = record?.loading ? { ...record, loading: false } : record;
    }
    return next;
  });
}, [selectedEndpointId]);

const markModelsChecking = targets => {
  const checkedAt = Date.now();
  setOpenaiModelHealth(prev => {
    const next = { ...prev };
    targets.forEach(({ endpointId, modelId }) => {
      next[modelHealthKey(endpointId, modelId)] = {
        status: 'checking',
        loading: true,
        latency: null,
        checkedAt,
      };
    });
    return next;
  });
};


const applyEndpointHealthResults = (endpointId, modelIds, records, fallbackError) => {
  const recordsByModel = new Map(
    (Array.isArray(records) ? records : []).map(record => [
      String(record?.model || '').trim(),
      record,
    ])
  );
  const results = modelIds.map(modelId =>
    normalizeModelHealthRecord(recordsByModel.get(modelId), fallbackError)
  );

  setOpenaiModelHealth(prev => {
    const next = { ...prev };
    modelIds.forEach((modelId, index) => {
      next[modelHealthKey(endpointId, modelId)] = results[index];
    });
    return next;
  });

  return results;
};

const testModelHealth = async (model, targetEndpointId, silentToast = false) => {
  const modelId = String(model?.id || '').trim();
  if (!modelId || !targetEndpointId) return null;
  const healthKey = modelHealthKey(targetEndpointId, modelId);
  const activeController = modelHealthAbortControllersRef.current.get(healthKey);
  if (activeController) {
    activeController.abort();
    modelHealthAbortControllersRef.current.delete(healthKey);
    setOpenaiModelHealth(prev => ({
      ...prev,
      [healthKey]: {
        status: 'cancelled',
        loading: false,
        latency: null,
        checkedAt: Date.now(),
        error: '检测已停止',
      },
    }));
    if (!silentToast) toast.warning(`${modelId} 检测已停止`);
    return null;
  }

  const controller = new AbortController();
  modelHealthAbortControllersRef.current.set(healthKey, controller);

  markModelsChecking([{ endpointId: targetEndpointId, modelId }]);

  try {
    const response = await fetch(
      `/api/openai/endpoints/${encodeURIComponent(targetEndpointId)}/health-check`,
      {
        method: 'POST',
        headers: { ...getAuthHeaders(), 'Content-Type': 'application/json' },
        signal: controller.signal,
        body: JSON.stringify({
          model: modelId,
          timeout: Math.max(1, Number(healthCheckForm.timeout) || DEFAULT_MODEL_HEALTH_TIMEOUT_SECONDS) * 1000,
        }),
      }
    );
    const data = await response.json().catch(() => ({}));
    if (!response.ok) {
      throw new Error(data.error || `HTTP ${response.status}`);
    }

    const result = applyEndpointHealthResults(targetEndpointId, [modelId], [data])[0];
    if (!silentToast) {
      if (result.status === 'healthy') {
        toast.success(`${modelId} 可用，延迟 ${result.latency ?? '-'} ms`);
      } else if (result.status === 'degraded') {
        toast.warning(`${modelId} 响应较慢，延迟 ${result.latency ?? '-'} ms`);
      } else {
        toast.error(`${modelId} 检测失败: ${result.error || '未知错误'}`);
      }
    }
    return result;
  } catch (e) {
    if (controller.signal.aborted) return null;
    const result = applyEndpointHealthResults(targetEndpointId, [modelId], [], e.message)[0];
    if (!silentToast) toast.error(`${modelId} 检测失败: ${result.error || e.message}`);
    return result;
  } finally {
    if (modelHealthAbortControllersRef.current.get(healthKey) === controller) {
      modelHealthAbortControllersRef.current.delete(healthKey);
    }
  }
};

// 批量检测：前端并发逐模型发请求，每个完成立即回填状态（无需等全部完成）。
const runBatchHealthCheckRequest = async (targets, fallbackMessage) => {
  if (!Array.isArray(targets) || targets.length === 0) return [];
  const concurrency = resolveModelHealthConcurrency(healthCheckForm.concurrency, targets.length);
  markModelsChecking(targets);
  const results = new Array(targets.length);
  let cursor = 0;
  const workers = Array.from({ length: concurrency }, async () => {
    while (true) {
      const index = cursor;
      cursor += 1;
      if (index >= targets.length) return;
      const target = targets[index];
      // silentToast=true：每个模型的 toast 由批量结果统一汇总，避免刷屏。
      const result = await testModelHealth(
        { id: target.modelId },
        target.endpointId,
        true
      );
      results[index] =
        result ||
        normalizeModelHealthRecord(
          { status: 'failed', error: '检测未返回结果', checkedAt: Date.now() },
          '检测未返回结果'
        );
    }
  });
  await Promise.all(workers);
  return results;
};

const startBatchHealthCheck = async () => {
  const endpointTargets = endpoints.filter(
    endpoint => endpoint.enabled && endpointModelIds(endpoint).length > 0
  );
  const allTargets = modelHealthTargets(endpointTargets);
  if (allTargets.length === 0) {
    toast.warning('没有找到任何启用的端点或模型');
    return;
  }

  setHealthCheckModal(false);
  setModelHealthBatchLoading(true);
  setHealthCheckProgress(createHealthCheckProgress(allTargets.length, true));
  const concurrency = resolveModelHealthConcurrency(
    healthCheckForm.concurrency,
    allTargets.length
  );
  toast.info(`正在按 ${concurrency} 并发批量检测 ${allTargets.length} 个模型...`, { isManual: true });

  try {
    const results = await runBatchHealthCheckRequest(allTargets, '批量检测失败');
    const counts = countModelHealthResults(results);
    setHealthCheckProgress({
      running: false,
      total: allTargets.length,
      completed: results.length,
      ...counts,
    });

    const message = `检测完成：可用 ${counts.healthy}，较慢 ${counts.degraded}，失败 ${counts.failed}`;
    if (counts.failed > 0) toast.warning(message);
    else toast.success(message);
  } catch {
    // 错误已在单模型检测内提示，此处仅终止流程。
  } finally {
    setModelHealthBatchLoading(false);
  }
};

const openHealthCheckForEndpoint = async endpointId => {
  const ep = endpoints.find(e => e.id === endpointId);
  const modelIds = endpointModelIds(ep);
  if (!ep || modelIds.length === 0) {
    toast.warning('该端点无可用模型');
    return;
  }

  setModelHealthBatchLoading(true);
  setHealthCheckProgress(createHealthCheckProgress(modelIds.length, true));
  const concurrency = resolveModelHealthConcurrency(healthCheckForm.concurrency, modelIds.length);
  toast.info(
    `正在按 ${concurrency} 并发批量检测 ${ep.name || '端点'} 的 ${modelIds.length} 个模型...`,
    { isManual: true }
  );

  try {
    const targets = modelIds.map(modelId => ({ endpointId, modelId }));
    const results = await runBatchHealthCheckRequest(targets, '端点检测失败');
    const counts = countModelHealthResults(results);
    setHealthCheckProgress({
      running: false,
      total: modelIds.length,
      completed: results.length,
      ...counts,
    });
    const message = `${ep.name || '端点'}：可用 ${counts.healthy}，较慢 ${counts.degraded}，失败 ${counts.failed}`;
    if (counts.failed > 0) toast.warning(message);
    else toast.success(message);
  } catch {
    // 错误已在单模型检测内提示，此处仅终止流程。
  } finally {
    setModelHealthBatchLoading(false);
  }
};
  return {
    openaiModelHealth, setOpenaiModelHealth,
    modelHealthBatchLoading, setModelHealthBatchLoading,
    healthCheckProgress, setHealthCheckProgress,
    healthCheckModal, setHealthCheckModal,
    healthCheckForm, setHealthCheckForm,
    modelHealthAbortControllersRef,
    testModelHealth,
    startBatchHealthCheck,
    openHealthCheckForEndpoint,
  };
}
