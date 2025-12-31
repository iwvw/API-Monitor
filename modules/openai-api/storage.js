/**
 * OpenAI API 管理 - 数据存储模块（使用 SQLite 数据库）
 *
 * 存储结构:
 * - endpoints: OpenAI 兼容端点列表 (自定义端点和 API Key)
 * - health_history: 健康检查历史记录
 */

const { OpenAIEndpoint, OpenAIHealthHistory } = require('../../src/db/models');
const dbService = require('../../src/db/database');

// 初始化数据库
dbService.initialize();

// ==================== 端点管理 ====================

/**
 * 获取所有 OpenAI 端点
 */
function getEndpoints() {
  try {
    const endpoints = OpenAIEndpoint.getAllEndpoints();
    // 转换字段名以保持向后兼容
    return endpoints.map(ep => ({
      id: ep.id,
      name: ep.name,
      baseUrl: ep.base_url,
      apiKey: ep.api_key,
      notes: '', // 旧版本有 notes 字段，新版本没有，保持兼容
      status: ep.status,
      enabled: ep.enabled == 1 || ep.enabled === true || ep.enabled === '1',
      models: ep.models || [],
      createdAt: ep.created_at,
      lastUsed: ep.last_used,
      lastChecked: ep.last_checked,
    }));
  } catch (e) {
    console.error('❌ 读取 OpenAI 端点失败:', e.message);
    return [];
  }
}

/**
 * 获取所有已启用的端点
 */
function getEnabledEndpoints() {
  return getEndpoints().filter(ep => ep.enabled);
}

/**
 * 保存端点列表
 */
function saveEndpoints(endpoints) {
  try {
    const db = dbService.getDatabase();

    const transaction = db.transaction(() => {
      // 清空现有端点
      OpenAIEndpoint.truncate();

      // 插入新端点
      endpoints.forEach(endpoint => {
        OpenAIEndpoint.createEndpoint({
          id: endpoint.id,
          name: endpoint.name,
          baseUrl: endpoint.baseUrl,
          apiKey: endpoint.apiKey,
          status: endpoint.status || 'unknown',
          enabled: endpoint.enabled === undefined ? 1 : endpoint.enabled ? 1 : 0,
          models: endpoint.models || [],
          createdAt: endpoint.createdAt,
          lastUsed: endpoint.lastUsed,
          lastChecked: endpoint.lastChecked,
        });
      });
    });

    transaction();
    return true;
  } catch (e) {
    console.error('❌ 保存 OpenAI 端点失败:', e.message);
    return false;
  }
}

/**
 * 添加端点
 * @param {Object} endpoint - { name, baseUrl, apiKey, notes? }
 */
function addEndpoint(endpoint) {
  const id = 'oai_' + Date.now() + '_' + Math.random().toString(36).substr(2, 9);
  const newEndpoint = {
    id,
    name: endpoint.name,
    baseUrl: endpoint.baseUrl,
    apiKey: endpoint.apiKey,
    notes: endpoint.notes || '',
    createdAt: new Date().toISOString(),
    lastUsed: null,
    lastChecked: null,
    status: 'unknown',
    enabled: 1,
    models: [],
  };

  OpenAIEndpoint.createEndpoint(newEndpoint);
  return newEndpoint;
}

/**
 * 更新端点
 */
function updateEndpoint(id, updates) {
  try {
    const endpoint = OpenAIEndpoint.getEndpoint(id);
    if (!endpoint) return null;

    const updateData = {};
    if (updates.name !== undefined) updateData.name = updates.name;
    if (updates.baseUrl !== undefined) updateData.baseUrl = updates.baseUrl;
    if (updates.apiKey !== undefined) updateData.apiKey = updates.apiKey;
    if (updates.status !== undefined) updateData.status = updates.status;
    if (updates.enabled !== undefined) updateData.enabled = updates.enabled;
    if (updates.models !== undefined) updateData.models = updates.models;
    if (updates.lastUsed !== undefined) updateData.lastUsed = updates.lastUsed;
    if (updates.lastChecked !== undefined) updateData.lastChecked = updates.lastChecked;

    OpenAIEndpoint.updateEndpoint(id, updateData);

    // 返回更新后的端点
    const updated = OpenAIEndpoint.getEndpoint(id);
    return {
      id: updated.id,
      name: updated.name,
      baseUrl: updated.base_url,
      apiKey: updated.api_key,
      notes: '',
      status: updated.status,
      enabled: updated.enabled == 1 || updated.enabled === true || updated.enabled === '1',
      models: updated.models,
      createdAt: updated.created_at,
      lastUsed: updated.last_used,
      lastChecked: updated.last_checked,
    };
  } catch (e) {
    console.error('❌ 更新 OpenAI 端点失败:', e.message);
    return null;
  }
}

/**
 * 删除端点
 */
function deleteEndpoint(id) {
  try {
    return OpenAIEndpoint.delete(id);
  } catch (e) {
    console.error('❌ 删除 OpenAI 端点失败:', e.message);
    return false;
  }
}

/**
 * 获取单个端点
 */
function getEndpointById(id) {
  try {
    const endpoint = OpenAIEndpoint.getEndpoint(id);
    if (!endpoint) return null;

    return {
      id: endpoint.id,
      name: endpoint.name,
      baseUrl: endpoint.base_url,
      apiKey: endpoint.api_key,
      notes: '',
      status: endpoint.status,
      enabled: endpoint.enabled == 1 || endpoint.enabled === true || endpoint.enabled === '1',
      models: endpoint.models,
      createdAt: endpoint.created_at,
      lastUsed: endpoint.last_used,
      lastChecked: endpoint.last_checked,
    };
  } catch (e) {
    console.error('❌ 获取 OpenAI 端点失败:', e.message);
    return null;
  }
}

/**
 * 更新端点最后使用时间
 */
function touchEndpoint(id) {
  try {
    OpenAIEndpoint.updateLastUsed(id);
  } catch (e) {
    console.error('❌ 更新端点使用时间失败:', e.message);
  }
}

/**
 * 批量导入端点
 * @param {Array} endpointsData - 端点数据数组
 * @param {boolean} overwrite - 是否覆盖现有数据
 */
function importEndpoints(endpointsData, overwrite = false) {
  const endpoints = overwrite ? [] : getEndpoints();
  let importedCount = 0;
  let skippedCount = 0;

  for (const data of endpointsData) {
    // 检查是否已存在相同的端点（通过 baseUrl 和 apiKey 判断）
    const exists = endpoints.some(e => e.baseUrl === data.baseUrl && e.apiKey === data.apiKey);

    if (exists && !overwrite) {
      skippedCount++;
      continue;
    }

    const id = 'oai_' + Date.now() + '_' + Math.random().toString(36).substr(2, 9);
    const newEndpoint = {
      id,
      name: data.name || '未命名端点',
      baseUrl: data.baseUrl,
      apiKey: data.apiKey,
      notes: data.notes || '',
      createdAt: new Date().toISOString(),
      lastUsed: null,
      lastChecked: null,
      status: 'unknown',
      enabled: data.enabled !== undefined ? (data.enabled ? 1 : 0) : 1,
      models: [],
    };

    OpenAIEndpoint.createEndpoint(newEndpoint);
    endpoints.push(newEndpoint);
    importedCount++;
  }

  return { importedCount, skippedCount, total: endpoints.length };
}

/**
 * 导出所有端点
 */
function exportEndpoints() {
  return getEndpoints();
}

// ==================== 健康状态管理 ====================

/**
 * 获取所有健康状态数据（兼容旧版本格式）
 */
function getHealthData() {
  try {
    // 从数据库获取所有端点的最新健康检查
    const endpoints = OpenAIEndpoint.getAllEndpoints();
    const healthData = {};

    endpoints.forEach(ep => {
      const history = OpenAIHealthHistory.getLatestCheck(ep.id);
      if (history) {
        healthData[ep.id] = {
          status: history.status,
          latency: history.response_time,
          error: history.error_message,
          checkedAt: history.checked_at,
        };
      }
    });

    return healthData;
  } catch (e) {
    console.error('❌ 读取健康状态失败:', e.message);
    return {};
  }
}

/**
 * 保存健康状态数据（兼容旧版本，实际存储到历史表）
 */
function saveHealthData(healthData) {
  try {
    // 将健康数据保存到历史表
    Object.entries(healthData).forEach(([endpointId, data]) => {
      OpenAIHealthHistory.recordCheck({
        endpoint_id: endpointId,
        status: data.status,
        response_time: data.latency,
        error_message: data.error,
        checked_at: data.checkedAt || new Date().toISOString(),
      });
    });
    return true;
  } catch (e) {
    console.error('❌ 保存健康状态失败:', e.message);
    return false;
  }
}

/**
 * 更新单个模型的健康状态
 * @param {string} endpointId - 端点 ID
 * @param {string} model - 模型名称（暂不使用，保持兼容）
 * @param {Object} healthResult - 健康检查结果
 */
function updateModelHealth(endpointId, model, healthResult) {
  try {
    // 记录健康检查历史
    OpenAIHealthHistory.recordCheck({
      endpoint_id: endpointId,
      status: healthResult.status,
      response_time: healthResult.latency,
      error_message: healthResult.error || null,
      checked_at: healthResult.checkedAt || new Date().toISOString(),
    });

    // 更新端点状态
    OpenAIEndpoint.updateStatus(endpointId, healthResult.status);

    return {
      status: healthResult.status,
      latency: healthResult.latency,
      error: healthResult.error || null,
      checkedAt: healthResult.checkedAt,
    };
  } catch (e) {
    console.error('❌ 更新模型健康状态失败:', e.message);
    return null;
  }
}

/**
 * 获取端点的所有模型健康状态
 */
function getEndpointHealth(endpointId) {
  try {
    const latest = OpenAIHealthHistory.getLatestCheck(endpointId);
    if (!latest) return {};

    return {
      status: latest.status,
      latency: latest.response_time,
      error: latest.error_message,
      checkedAt: latest.checked_at,
    };
  } catch (e) {
    console.error('❌ 获取端点健康状态失败:', e.message);
    return {};
  }
}

/**
 * 获取单个模型的健康状态
 */
function getModelHealth(endpointId, model) {
  return getEndpointHealth(endpointId);
}

/**
 * 清除端点的健康状态数据
 */
function clearEndpointHealth(endpointId) {
  try {
    // 删除该端点的所有健康检查历史
    const db = dbService.getDatabase();
    const stmt = db.prepare('DELETE FROM openai_health_history WHERE endpoint_id = ?');
    stmt.run(endpointId);
  } catch (e) {
    console.error('❌ 清除端点健康状态失败:', e.message);
  }
}

/**
 * 清除所有健康状态数据
 */
function clearAllHealthData() {
  try {
    const db = dbService.getDatabase();
    const stmt = db.prepare('DELETE FROM openai_health_history');
    stmt.run();
  } catch (e) {
    console.error('❌ 清除所有健康状态失败:', e.message);
  }
}

module.exports = {
  getEndpoints,
  getEnabledEndpoints,
  saveEndpoints,
  addEndpoint,
  updateEndpoint,
  deleteEndpoint,
  getEndpointById,
  touchEndpoint,
  importEndpoints,
  exportEndpoints,
  // 健康状态相关
  updateModelHealth,
  getEndpointHealth,
  getModelHealth,
  clearEndpointHealth,
  clearAllHealthData,
};
