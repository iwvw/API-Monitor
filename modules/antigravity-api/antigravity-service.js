const fs = require('fs');
const path = require('path');
const storage = require('./storage');

const MATRIX_FILE = path.join(__dirname, 'antigravity-matrix.json');

// 默认矩阵配置
const DEFAULT_MATRIX = {
  'gemini-2.5-pro': { base: true, fakeStream: true, antiTrunc: true },
  'gemini-2.5-flash': { base: true, fakeStream: true, antiTrunc: true },
  'gemini-2.5-flash-lite': { base: true, fakeStream: true, antiTrunc: true },
  'gemini-3-pro-high': { base: true, fakeStream: true, antiTrunc: true },
  'gemini-3-pro-low': { base: true, fakeStream: true, antiTrunc: true },
  'claude-sonnet-4-5': { base: true, fakeStream: true, antiTrunc: true },
  'gpt-oss-120b-medium': { base: true, fakeStream: true, antiTrunc: true },
};

/**
 * 读取矩阵配置
 */
function getMatrixConfig() {
  try {
    if (fs.existsSync(MATRIX_FILE)) {
      return JSON.parse(fs.readFileSync(MATRIX_FILE, 'utf8'));
    }
  } catch (e) {
    console.error('Failed to read antigravity matrix file:', e);
  }
  // 如果文件不存在，返回空对象而非 DEFAULT_MATRIX，确保必须显式配置才开启
  return {};
}

/**
 * 保存矩阵配置
 */
function saveMatrixConfig(config) {
  try {
    fs.writeFileSync(MATRIX_FILE, JSON.stringify(config, null, 2), 'utf8');
    return true;
  } catch (e) {
    console.error('Failed to save antigravity matrix file:', e);
    return false;
  }
}

/**
 * 根据矩阵配置和额度状态生成可用模型列表 (OpenAI 格式)
 */
function getAvailableModels(prefix = '') {
  const matrix = getMatrixConfig();
  const models = [];
  const modelConfigs = storage.getModelConfigs(); // 从数据库读取的启用状态
  const now = Math.floor(Date.now() / 1000);

  // 获取所有重定向配置，收集重定向目标模型（这些模型不直接暴露）
  const redirects = storage.getModelRedirects();
  const redirectTargets = new Set(redirects.map(r => r.target_model));

  // 获取所有在"额度状态"页开启的模型 ID
  // 注意：modelConfigs 存储的是 model_id -> enabled (0/1)
  const activeModelIds = Object.keys(modelConfigs).filter(id => modelConfigs[id] !== false);

  // 1. 遍历所有活跃模型（跳过重定向目标模型，它们只能通过别名访问）
  activeModelIds.forEach(modelId => {
    // 如果该模型是重定向的目标，不直接暴露
    if (redirectTargets.has(modelId)) {
      return;
    }

    const config = matrix[modelId];

    // 如果矩阵中有明确配置
    if (config) {
      // 如果三个开关全关，视为彻底禁用
      if (config.base === false && config.fakeStream === false && config.antiTrunc === false) {
        return;
      }

      if (config.base === true) {
        models.push({
          id: prefix + modelId,
          object: 'model',
          created: now,
          owned_by: 'antigravity',
        });
      }
      if (config.fakeStream === true) {
        models.push({
          id: prefix + '假流/' + modelId,
          object: 'model',
          created: now,
          owned_by: 'antigravity',
        });
      }
      if (config.antiTrunc === true) {
        models.push({
          id: prefix + '流抗/' + modelId,
          object: 'model',
          created: now,
          owned_by: 'antigravity',
        });
      }
    } else {
      // 如果矩阵中没有配置，默认开启基础功能
      models.push({ id: prefix + modelId, object: 'model', created: now, owned_by: 'antigravity' });
    }
  });

  // 2. 注入重定向模型 (Alias) - 根据目标模型的矩阵配置生成对应变体
  redirects.forEach(r => {
    // 别名对应的源模型或目标模型在额度中被显式关闭
    if (modelConfigs[r.source_model] === false || modelConfigs[r.target_model] === false) return;

    // 获取目标模型的矩阵配置
    const targetConfig = matrix[r.target_model];

    // 如果目标模型在矩阵中被彻底禁用（三个开关全关）
    if (
      targetConfig &&
      targetConfig.base === false &&
      targetConfig.fakeStream === false &&
      targetConfig.antiTrunc === false
    ) {
      return;
    }

    // 根据目标模型的配置生成别名的功能性变体
    if (targetConfig) {
      if (targetConfig.base === true) {
        const id = prefix + r.source_model;
        if (!models.find(m => m.id === id)) {
          models.push({ id, object: 'model', created: now, owned_by: 'system-redirect' });
        }
      }
      if (targetConfig.fakeStream === true) {
        const id = prefix + '假流/' + r.source_model;
        if (!models.find(m => m.id === id)) {
          models.push({ id, object: 'model', created: now, owned_by: 'system-redirect' });
        }
      }
      if (targetConfig.antiTrunc === true) {
        const id = prefix + '流抗/' + r.source_model;
        if (!models.find(m => m.id === id)) {
          models.push({ id, object: 'model', created: now, owned_by: 'system-redirect' });
        }
      }
    } else {
      // 如果目标模型没有矩阵配置，默认只生成基础别名
      const id = prefix + r.source_model;
      if (!models.find(m => m.id === id)) {
        models.push({ id, object: 'model', created: now, owned_by: 'system-redirect' });
      }
    }
  });

  return models;
}

module.exports = {
  getMatrixConfig,
  saveMatrixConfig,
  getAvailableModels,
};
