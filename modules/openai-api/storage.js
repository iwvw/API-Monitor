/**
 * OpenAI API 管理 - 数据存储模块
 * 
 * 存储结构:
 * - endpoints: OpenAI 兼容端点列表 (自定义端点和 API Key)
 */

const fs = require('fs');
const path = require('path');

// 配置目录
const CONFIG_DIR = process.env.CONFIG_DIR || path.join(__dirname, '../../config');
const ENDPOINTS_FILE = path.join(CONFIG_DIR, 'openai-endpoints.json');
const HEALTH_FILE = path.join(CONFIG_DIR, 'openai-health.json');

// 确保配置目录存在
function ensureConfigDir() {
  if (!fs.existsSync(CONFIG_DIR)) {
    fs.mkdirSync(CONFIG_DIR, { recursive: true });
  }
}

// ==================== 端点管理 ====================

/**
 * 获取所有 OpenAI 端点
 */
function getEndpoints() {
  try {
    ensureConfigDir();
    if (fs.existsSync(ENDPOINTS_FILE)) {
      const data = fs.readFileSync(ENDPOINTS_FILE, 'utf8');
      return JSON.parse(data);
    }
  } catch (e) {
    console.error('❌ 读取 OpenAI 端点失败:', e.message);
  }
  return [];
}

/**
 * 保存端点列表
 */
function saveEndpoints(endpoints) {
  try {
    ensureConfigDir();
    fs.writeFileSync(ENDPOINTS_FILE, JSON.stringify(endpoints, null, 2), 'utf8');
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
  const endpoints = getEndpoints();
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
    status: 'unknown', // unknown, valid, invalid
    models: [] // 缓存的模型列表
  };
  endpoints.push(newEndpoint);
  saveEndpoints(endpoints);
  return newEndpoint;
}

/**
 * 更新端点
 */
function updateEndpoint(id, updates) {
  const endpoints = getEndpoints();
  const index = endpoints.findIndex(e => e.id === id);
  if (index === -1) return null;
  
  // 只更新允许的字段
  const allowed = ['name', 'baseUrl', 'apiKey', 'notes', 'status', 'models', 'lastUsed', 'lastChecked', 'healthStatus', 'lastHealthCheck'];
  allowed.forEach(key => {
    if (updates[key] !== undefined) {
      endpoints[index][key] = updates[key];
    }
  });
  
  saveEndpoints(endpoints);
  return endpoints[index];
}

/**
 * 删除端点
 */
function deleteEndpoint(id) {
  const endpoints = getEndpoints();
  const index = endpoints.findIndex(e => e.id === id);
  if (index === -1) return false;
  
  endpoints.splice(index, 1);
  saveEndpoints(endpoints);
  return true;
}

/**
 * 获取单个端点
 */
function getEndpointById(id) {
  const endpoints = getEndpoints();
  return endpoints.find(e => e.id === id) || null;
}

/**
 * 更新端点最后使用时间
 */
function touchEndpoint(id) {
  const endpoints = getEndpoints();
  const endpoint = endpoints.find(e => e.id === id);
  if (endpoint) {
    endpoint.lastUsed = new Date().toISOString();
    saveEndpoints(endpoints);
  }
}

/**
 * 批量导入端点
 * @param {Array} endpointsData - 端点数据数组
 * @param {boolean} overwrite - 是否覆盖现有数据
 */
function importEndpoints(endpointsData, overwrite = false) {
  let endpoints = overwrite ? [] : getEndpoints();
  let importedCount = 0;
  let skippedCount = 0;
  
  for (const data of endpointsData) {
    // 检查是否已存在相同的端点（通过 baseUrl 和 apiKey 判断）
    const exists = endpoints.some(e => 
      e.baseUrl === data.baseUrl && e.apiKey === data.apiKey
    );
    
    if (exists && !overwrite) {
      skippedCount++;
      continue;
    }
    
    const id = 'oai_' + Date.now() + '_' + Math.random().toString(36).substr(2, 9);
    endpoints.push({
      id,
      name: data.name || '未命名端点',
      baseUrl: data.baseUrl,
      apiKey: data.apiKey,
      notes: data.notes || '',
      createdAt: new Date().toISOString(),
      lastUsed: null,
      lastChecked: null,
      status: 'unknown',
      models: []
    });
    importedCount++;
  }
  
  saveEndpoints(endpoints);
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
 * 获取所有健康状态数据
 */
function getHealthData() {
  try {
    ensureConfigDir();
    if (fs.existsSync(HEALTH_FILE)) {
      const data = fs.readFileSync(HEALTH_FILE, 'utf8');
      return JSON.parse(data);
    }
  } catch (e) {
    console.error('❌ 读取健康状态失败:', e.message);
  }
  return {};
}

/**
 * 保存健康状态数据
 */
function saveHealthData(healthData) {
  try {
    ensureConfigDir();
    fs.writeFileSync(HEALTH_FILE, JSON.stringify(healthData, null, 2), 'utf8');
    return true;
  } catch (e) {
    console.error('❌ 保存健康状态失败:', e.message);
    return false;
  }
}

/**
 * 更新单个模型的健康状态
 * @param {string} endpointId - 端点 ID
 * @param {string} model - 模型名称
 * @param {Object} healthResult - 健康检查结果
 */
function updateModelHealth(endpointId, model, healthResult) {
  const healthData = getHealthData();
  
  if (!healthData[endpointId]) {
    healthData[endpointId] = {};
  }
  
  healthData[endpointId][model] = {
    status: healthResult.status,
    latency: healthResult.latency,
    error: healthResult.error || null,
    checkedAt: healthResult.checkedAt
  };
  
  saveHealthData(healthData);
  return healthData[endpointId][model];
}

/**
 * 获取端点的所有模型健康状态
 */
function getEndpointHealth(endpointId) {
  const healthData = getHealthData();
  return healthData[endpointId] || {};
}

/**
 * 获取单个模型的健康状态
 */
function getModelHealth(endpointId, model) {
  const healthData = getHealthData();
  return healthData[endpointId]?.[model] || null;
}

/**
 * 清除端点的健康状态数据
 */
function clearEndpointHealth(endpointId) {
  const healthData = getHealthData();
  if (healthData[endpointId]) {
    delete healthData[endpointId];
    saveHealthData(healthData);
  }
}

/**
 * 清除所有健康状态数据
 */
function clearAllHealthData() {
  saveHealthData({});
}

module.exports = {
  getEndpoints,
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
  clearAllHealthData
};