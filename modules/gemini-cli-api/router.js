const express = require('express');
const router = express.Router();
const axios = require('axios');
const storage = require('./storage');
const client = require('./gemini-client');
const StreamProcessor = require('./utils/stream-processor');
const { requireAuth } = require('../../src/middleware/auth');

const streamProcessor = new StreamProcessor(client);

// ==================== 服务器端定时检测服务 ====================
const autoCheckService = {
  timerId: null,

  /**
   * 启动定时检测
   */
  start() {
    this.stop(); // 先停止已有定时器

    const settings = storage.getSettings();
    const enabled = settings.autoCheckEnabled === '1' || settings.autoCheckEnabled === true;
    const intervalMs = parseInt(settings.autoCheckInterval) || 3600000; // 默认 1 小时

    if (!enabled) {
      console.log('[GCLI AutoCheck] 定时检测未启用');
      return;
    }

    console.log(`[GCLI AutoCheck] 定时检测已启动，间隔: ${Math.round(intervalMs / 60000)} 分钟`);

    this.timerId = setInterval(() => {
      this.runCheck();
    }, intervalMs);

    // 记录下次执行时间
    this.nextRunTime = Date.now() + intervalMs;
  },

  /**
   * 停止定时检测
   */
  stop() {
    if (this.timerId) {
      clearInterval(this.timerId);
      this.timerId = null;
      console.log('[GCLI AutoCheck] 定时检测已停止');
    }
  },

  /**
   * 重启定时检测（设置变更时调用）
   */
  restart() {
    console.log('[GCLI AutoCheck] 重新加载设置...');
    this.start();
  },

  /**
   * 执行一次模型检测
   */
  async runCheck() {
    console.log('[GCLI AutoCheck] 开始执行定时模型检测...');

    try {
      const accounts = storage.getAccounts();
      if (accounts.length === 0) {
        console.log('[GCLI AutoCheck] 没有账号，跳过检测');
        return;
      }

      // 获取要检测的模型列表（复用现有逻辑）
      const set = new Set();
      const redirects = storage.getModelRedirects();
      if (Array.isArray(redirects)) {
        redirects.forEach(r => set.add(r.source_model));
      }

      const matrixConfig = getMatrixConfig();
      Object.keys(matrixConfig || {}).forEach(m => set.add(m));

      const history = storage.getModelCheckHistory();
      (history.models || []).forEach(m => set.add(m));

      let modelsToCheck = Array.from(set);

      // 应用禁用模型过滤
      const settings = storage.getSettings();
      if (settings.disabledCheckModels) {
        try {
          const disabledModels = JSON.parse(settings.disabledCheckModels);
          if (disabledModels.length > 0) {
            modelsToCheck = modelsToCheck.filter(m => !disabledModels.includes(m));
          }
        } catch (e) {}
      }

      if (modelsToCheck.length === 0) {
        modelsToCheck = [
          'gemini-2.5-pro',
          'gemini-2.5-flash',
          'gemini-1.5-pro',
          'gemini-1.5-flash',
        ];
      }

      const batchTime = Math.floor(Date.now() / 1000);

      // 初始化历史记录
      for (const modelId of modelsToCheck) {
        storage.recordModelCheck(modelId, 'error', 'Waiting...', batchTime, '');
      }

      const globalModelStatus = {};
      modelsToCheck.forEach(
        m => (globalModelStatus[m] = { ok: false, errors: [], passedIndices: [] })
      );

      console.log(
        `[GCLI AutoCheck] 检测 ${modelsToCheck.length} 个模型，${accounts.length} 个账号`
      );

      for (let i = 0; i < accounts.length; i++) {
        const account = accounts[i];
        const accountIndex = i + 1;

        for (const modelId of modelsToCheck) {
          const testRequest = {
            model: modelId,
            messages: [{ role: 'user', content: 'Hi' }],
            max_tokens: 5,
            stream: false,
          };

          try {
            const timeoutPromise = new Promise((_, reject) =>
              setTimeout(() => reject(new Error('Timeout')), 15000)
            );

            const response = await Promise.race([
              client.generateContent(testRequest, account.id),
              timeoutPromise,
            ]);

            const responseData = response && response.data ? response.data : response;
            const candidates = responseData?.response?.candidates || responseData?.candidates;
            const hasContent = candidates && candidates.length > 0;

            if (hasContent) {
              globalModelStatus[modelId].ok = true;
              globalModelStatus[modelId].passedIndices.push(accountIndex);
            } else {
              const errorMsg = responseData?.error?.message || 'Unexpected response';
              globalModelStatus[modelId].errors.push(`${account.name}: ${errorMsg}`);
            }
          } catch (e) {
            const errorMsg = e.response?.data?.error?.message || e.message;
            globalModelStatus[modelId].errors.push(`${account.name}: ${errorMsg}`);
          }

          // 实时更新数据库
          const passedAccounts = globalModelStatus[modelId].passedIndices.join(',');
          const status = globalModelStatus[modelId].ok ? 'ok' : 'error';
          const errorLog = globalModelStatus[modelId].errors.join('\n');
          storage.recordModelCheck(modelId, status, errorLog, batchTime, passedAccounts);
        }
      }

      console.log('[GCLI AutoCheck] 定时检测完成');
    } catch (error) {
      console.error('[GCLI AutoCheck] 定时检测失败:', error.message);
    }
  },

  /**
   * 获取状态
   */
  getStatus() {
    const settings = storage.getSettings();
    return {
      running: this.timerId !== null,
      enabled: settings.autoCheckEnabled === '1',
      intervalMs: parseInt(settings.autoCheckInterval) || 3600000,
      nextRunTime: this.nextRunTime || null,
    };
  },
};

// 模块加载时自动启动定时检测
setTimeout(() => {
  autoCheckService.start();
}, 2000); // 延迟 2 秒，等待数据库初始化完成

/**
 * API Key 认证中间件 (供外部客户端使用)
 */
const requireApiKey = async (req, res, next) => {
  const authHeader = req.headers.authorization;
  if (req.lb) return next(); // 如果经过负载均衡器验证，直接放行
  if (!authHeader || !authHeader.startsWith('Bearer ')) {
    return res
      .status(401)
      .json({ error: { message: 'Unauthorized', type: 'invalid_request_error', code: '401' } });
  }

  const token = authHeader.substring(7);

  try {
    const settings = await storage.getSettings();
    const configuredKey = settings.API_KEY || '123456'; // 默认 123456 仅供兼容，建议设置

    if (token !== configuredKey) {
      return res
        .status(401)
        .json({
          error: { message: 'Invalid API Key', type: 'invalid_request_error', code: '401' },
        });
    }
    next();
  } catch (e) {
    console.error('API Key 验证出错:', e);
    res.status(500).json({ error: 'Auth Error' });
  }
};

/**
 * OpenAI 兼容的模型列表接口 - 返回基础模型和思考模型变体
 */
const path = require('path');
const fs = require('fs');

const MATRIX_FILE = path.join(__dirname, 'gemini-matrix.json');

// 默认矩阵配置（如果文件不存在）
const DEFAULT_MATRIX = {
  'gemini-2.5-pro': {
    base: true,
    maxThinking: true,
    noThinking: true,
    search: true,
    fakeStream: true,
    antiTrunc: true,
  },
  'gemini-2.5-flash': {
    base: true,
    maxThinking: true,
    noThinking: true,
    search: true,
    fakeStream: true,
    antiTrunc: true,
  },
  'gemini-3-pro-preview': {
    base: true,
    maxThinking: true,
    noThinking: true,
    search: true,
    fakeStream: true,
    antiTrunc: true,
  },
  'gemini-3-flash-preview': {
    base: true,
    maxThinking: true,
    noThinking: true,
    search: true,
    fakeStream: true,
    antiTrunc: true,
  },
};

// 辅助函数：读取矩阵配置
function getMatrixConfig() {
  try {
    if (fs.existsSync(MATRIX_FILE)) {
      return JSON.parse(fs.readFileSync(MATRIX_FILE, 'utf8'));
    }
  } catch (e) {
    console.error('Failed to read matrix file:', e);
  }
  return DEFAULT_MATRIX;
}

// 辅助函数：保存矩阵配置
function saveMatrixConfig(config) {
  try {
    fs.writeFileSync(MATRIX_FILE, JSON.stringify(config, null, 2), 'utf8');
    return true;
  } catch (e) {
    console.error('Failed to save matrix file:', e);
    return false;
  }
}

/**
 * 获取模型矩阵配置 (内部 API)
 */
router.get('/config/matrix', requireAuth, (req, res) => {
  res.json(getMatrixConfig());
});

/**
 * 更新模型矩阵配置 (内部 API)
 */
router.post('/config/matrix', requireAuth, (req, res) => {
  const newConfig = req.body;
  if (saveMatrixConfig(newConfig)) {
    res.json({ success: true });
  } else {
    res.status(500).json({ error: 'Failed to save configuration' });
  }
});

// 获取设置
router.get('/settings', requireAuth, (req, res) => {
  try {
    const settings = storage.getSettings();
    // 附加定时检测运行状态
    settings._autoCheckStatus = autoCheckService.getStatus();
    res.json(settings);
  } catch (error) {
    res.status(500).json({ error: error.message });
  }
});

// 保存设置
router.post('/settings', requireAuth, (req, res) => {
  try {
    const updates = req.body;
    for (const [key, value] of Object.entries(updates)) {
      storage.updateSetting(key, value);
    }

    // 如果定时检测相关设置变更，重启定时器
    if ('autoCheckEnabled' in updates || 'autoCheckInterval' in updates) {
      autoCheckService.restart();
    }

    res.json({ success: true });
  } catch (error) {
    res.status(500).json({ error: error.message });
  }
});

// 辅助函数：根据矩阵配置和禁用列表生成可用模型列表
function getAvailableModels(prefix) {
  const matrix = getMatrixConfig();
  const models = [];
  const disabledModels = storage.getDisabledModels();

  Object.keys(matrix).forEach(baseModelId => {
    const config = matrix[baseModelId];
    if (!config) return;

    // 检查是否至少有一个功能开启
    const hasAnyEnabled =
      config.base ||
      config.maxThinking ||
      config.noThinking ||
      config.fakeStream ||
      config.antiTrunc;
    if (!hasAnyEnabled) return;

    // 1. 收集当前模型的基础可用变体 ID (不含前缀)
    const standardVariants = [];

    // A. 基础模型
    if (config.base) {
      standardVariants.push(baseModelId);
      // A.1 搜索变体
      if (config.search) {
        standardVariants.push(baseModelId + '-search');
      }
    }

    // B. 深度思考 (MaxThinking)
    if (config.maxThinking) {
      standardVariants.push(baseModelId + '-maxthinking');
      // B.1 搜索变体
      if (config.search) {
        standardVariants.push(baseModelId + '-maxthinking-search');
      }
    }

    // C. 快速思考 (NoThinking)
    if (config.noThinking) {
      standardVariants.push(baseModelId + '-nothinking');
      // C.1 搜索变体
      if (config.search) {
        standardVariants.push(baseModelId + '-nothinking-search');
      }
    }

    // 2. 为每个变体生成最终的模型对象 (应用全局前缀 + 功能前缀)
    if (standardVariants.length > 0) {
      standardVariants.forEach(variantId => {
        // 2.1 添加标准模型 (带全局前缀)
        const id1 = prefix + variantId;
        if (!disabledModels.includes(id1)) models.push(createModelObject(id1));

        // 2.2 添加假流式变体
        if (config.fakeStream) {
          const id2 = prefix + '假流/' + variantId;
          if (!disabledModels.includes(id2)) models.push(createModelObject(id2));
        }

        // 2.3 添加抗截断变体
        if (config.antiTrunc) {
          const id3 = prefix + '流抗/' + variantId;
          if (!disabledModels.includes(id3)) models.push(createModelObject(id3));
        }
      });
    } else {
      // 如果没有任何基础变体（base/maxThinking/noThinking 都为 false），
      // 但有功能性开关开启，则直接生成功能性变体
      if (config.fakeStream) {
        const id = prefix + '假流/' + baseModelId;
        if (!disabledModels.includes(id)) models.push(createModelObject(id));
      }
      if (config.antiTrunc) {
        const id = prefix + '流抗/' + baseModelId;
        if (!disabledModels.includes(id)) models.push(createModelObject(id));
      }
    }
  });

  // 3. 注入重定向模型
  const redirects = storage.getModelRedirects();
  redirects.forEach(r => {
    // 仅当源模型不冲突时添加 (应用前缀后的源模型名)
    const sourceWithPrefix = prefix + r.source_model;
    if (
      !models.find(m => m.id === sourceWithPrefix) &&
      !disabledModels.includes(sourceWithPrefix)
    ) {
      models.push(createModelObject(sourceWithPrefix));
    }
  });

  return models;
}

/**
 * OpenAI 兼容的模型列表接口 - 基于矩阵配置动态生成
 */
router.get(['/v1/models', '/models'], requireApiKey, async (req, res) => {
  try {
    // 获取全局设置中的前缀
    const userSettingsService = require('../../src/services/userSettings');
    const globalSettings = userSettingsService.loadUserSettings();
    const prefix = (globalSettings.channelModelPrefix || {})['gemini-cli'] || '';

    const models = getAvailableModels(prefix);

    res.json({ object: 'list', data: models });
  } catch (e) {
    console.error('[GCLI] Failed to fetch matrix models:', e.message);
    res.status(500).json({ error: e.message });
  }
});

function createModelObject(id) {
  return {
    id: id,
    object: 'model',
    created: Math.floor(Date.now() / 1000),
    owned_by: 'google',
  };
}

// 全局渠道启用状态检查中间件
function requireChannelEnabled(req, res, next) {
  const userSettingsService = require('../../src/services/userSettings');
  const settings = userSettingsService.loadUserSettings();
  const channelEnabled = settings.channelEnabled || {};

  if (channelEnabled['gemini-cli'] === false) {
    return res.status(403).json({
      error: {
        message: 'Gemini CLI channel is disabled in global settings.',
        type: 'permission_error',
        code: 'channel_disabled',
      },
    });
  }
  next();
}

// 所有 /v1 (及为了兼容性直接暴露在根部的 chat/completions) 接口受全局启用状态控制
router.use(['/v1', '/chat/completions'], requireChannelEnabled);

// 账号冷却/避让逻辑缓存 (内存中)
// key: accountId:modelId -> resetTime
const accountCoolDowns = new Map();

/**
 * 检查账号是否处于冷却期
 */
function isAccountInCoolDown(accountId, model) {
  const key = `${accountId}:${model}`;
  const resetTime = accountCoolDowns.get(key);

  if (!resetTime) return false;

  if (resetTime > Date.now()) {
    return true;
  }

  // 已过期，移除
  accountCoolDowns.delete(key);
  return false;
}

/**
 * 获取账号所有受限的模型
 */
function getAccountCoolDowns(accountId) {
  const limitedModels = [];
  const now = Date.now();

  for (const [key, resetTime] of accountCoolDowns.entries()) {
    if (key.startsWith(`${accountId}:`)) {
      if (resetTime > now) {
        limitedModels.push({
          model: key.split(':')[1],
          resetTime,
          remainingMs: resetTime - now,
        });
      } else {
        accountCoolDowns.delete(key);
      }
    }
  }
  return limitedModels;
}

// ============== 管理接口 (需 Admin 权限) ==============
router.use(
  ['/accounts', '/oauth/exchange', '/logs', '/settings', '/stats', '/quotas', '/models'],
  requireAuth
);

/**
 * 获取额度信息
 */
router.get('/quotas', async (req, res) => {
  try {
    const { accountId } = req.query;
    if (!accountId) {
      return res.json({});
    }

    // 获取账号
    const accounts = storage.getAccounts();
    const account = accounts.find(a => a.id === accountId);
    if (!account) {
      return res.status(404).json({ error: 'Account not found' });
    }

    // 使用 client 获取模型列表和额度
    const quotas = await client.getQuotas(account);
    res.json(quotas);
  } catch (e) {
    console.error('获取额度失败:', e);
    res.status(500).json({ error: e.message });
  }
});

/**
 * 切换模型状态
 */
router.post('/models/status', async (req, res) => {
  try {
    const { modelId, enabled } = req.body;
    if (!modelId) {
      return res.status(400).json({ error: 'Model ID required' });
    }

    storage.setModelStatus(modelId, enabled);
    res.json({ success: true, modelId, enabled });
  } catch (e) {
    res.status(500).json({ error: e.message });
  }
});

/**
 * 获取模型重定向列表
 */
router.get('/models/redirects', async (req, res) => {
  try {
    const redirects = storage.getModelRedirects();
    res.json(redirects);
  } catch (e) {
    res.status(500).json({ error: e.message });
  }
});

/**
 * 添加模型重定向
 */
router.post('/models/redirects', async (req, res) => {
  const { sourceModel, targetModel } = req.body;
  if (!sourceModel || !targetModel) {
    return res.status(400).json({ error: 'Source and target models required' });
  }

  if (sourceModel === targetModel) {
    return res.status(400).json({ error: 'Cannot redirect to self' });
  }

  try {
    storage.addModelRedirect(sourceModel, targetModel);
    res.json({ success: true, sourceModel, targetModel });
  } catch (e) {
    res.status(500).json({ error: e.message });
  }
});

/**
 * 删除模型重定向
 */
router.delete('/models/redirects/:sourceModel', async (req, res) => {
  const { sourceModel } = req.params;
  try {
    storage.removeModelRedirect(sourceModel);
    res.json({ success: true, sourceModel });
  } catch (e) {
    res.status(500).json({ error: e.message });
  }
});

/**
 * 获取统计信息
 */
router.get('/stats', (req, res) => {
  try {
    const stats = storage.getStats();
    res.json(stats);
  } catch (error) {
    res.status(500).json({ error: error.message });
  }
});

/**
 * 模型健康检测 - 对所有账号执行测试
 */
router.post('/accounts/check', async (req, res) => {
  console.log('[GCLI] Starting model check...');
  try {
    const accounts = storage.getAccounts();

    if (accounts.length === 0) {
      return res.json({
        success: true,
        message: 'No accounts to check',
        totalAccounts: 0,
      });
    }

    // 获取要检测的模型列表
    const set = new Set();

    // 1. 从重定向中获取
    const redirects = storage.getModelRedirects();
    if (Array.isArray(redirects)) {
      redirects.forEach(r => set.add(r.source_model));
    } else {
      Object.keys(redirects || {}).forEach(m => set.add(m));
    }

    // 2. 从矩阵配置中获取
    const matrix = getMatrixConfig();
    Object.keys(matrix || {}).forEach(m => set.add(m));

    // 3. 从历史记录补充
    const history = storage.getModelCheckHistory();
    (history.models || []).forEach(m => set.add(m));

    let modelsToCheck = Array.from(set);

    // 4. 应用禁用模型过滤
    const settings = storage.getSettings();
    if (settings.disabledCheckModels) {
      try {
        const disabledModels = JSON.parse(settings.disabledCheckModels);
        if (disabledModels.length > 0) {
          modelsToCheck = modelsToCheck.filter(m => !disabledModels.includes(m));
        }
      } catch (e) {
        console.error('[GCLI] Failed to parse disabledCheckModels:', e.message);
      }
    }

    // 5. 兜底方案
    if (modelsToCheck.length === 0) {
      modelsToCheck = ['gemini-2.5-pro', 'gemini-2.5-flash', 'gemini-1.5-pro', 'gemini-1.5-flash'];
    }

    const batchTime = Math.floor(Date.now() / 1000);

    // 初始化历史记录 (占位)
    for (const modelId of modelsToCheck) {
      storage.recordModelCheck(modelId, 'error', 'Waiting...', batchTime, '');
    }

    const globalModelStatus = {};
    modelsToCheck.forEach(
      m => (globalModelStatus[m] = { ok: false, errors: [], passedIndices: [] })
    );

    console.log(
      `[GCLI] Checking ${modelsToCheck.length} models across ${accounts.length} accounts at ${batchTime}`
    );

    for (let i = 0; i < accounts.length; i++) {
      const account = accounts[i];
      const accountIndex = i + 1;
      console.log(`[GCLI] Checking account #${accountIndex}: ${account.name || account.id}`);

      for (const modelId of modelsToCheck) {
        const testRequest = {
          model: modelId,
          messages: [{ role: 'user', content: 'Hi' }],
          max_tokens: 5,
          stream: false,
        };

        try {
          console.log(`[GCLI]   -> Testing ${modelId}...`);

          // 添加超时 (15秒)
          const timeoutPromise = new Promise((_, reject) =>
            setTimeout(() => reject(new Error('Timeout')), 15000)
          );

          const response = await Promise.race([
            client.generateContent(testRequest, account.id),
            timeoutPromise,
          ]);

          // axios 返回的是包装后的对象，实际数据在 data 属性中
          const responseData = response && response.data ? response.data : response;

          // 只要有任何形式的内容返回就初步认为成功
          // Google API 结构: { response: { candidates: [...] } }
          const candidates = responseData?.response?.candidates || responseData?.candidates;
          const hasContent = candidates && candidates.length > 0;

          if (hasContent) {
            globalModelStatus[modelId].ok = true;
            globalModelStatus[modelId].passedIndices.push(accountIndex);
            console.log(`\x1b[32m[GCLI]      ✓ ${modelId} passed\x1b[0m`);
          } else {
            // 打印实际响应结构用于调试
            console.log(
              `\x1b[31m[GCLI]      ✗ ${modelId} responseData:\x1b[0m`,
              JSON.stringify(responseData).substring(0, 300)
            );
            const errorMsg =
              responseData && responseData.error
                ? responseData.error.message
                : 'Unexpected response structure';
            globalModelStatus[modelId].errors.push(`${account.name}: ${errorMsg}`);
            console.log(`\x1b[31m[GCLI]      ✗ ${modelId} failed: ${errorMsg}\x1b[0m`);
          }
        } catch (e) {
          const errorMsg = e.response?.data?.error?.message || e.message || 'Unknown error';
          globalModelStatus[modelId].errors.push(`${account.name}: ${errorMsg}`);
          console.log(`\x1b[31m[GCLI]      ✗ ${modelId} failed: ${errorMsg}\x1b[0m`);
        }

        // 实时更新数据库，让前端轮询能看到进度
        const passedAccounts = globalModelStatus[modelId].passedIndices.join(',');
        const status = globalModelStatus[modelId].ok ? 'ok' : 'error';
        const errorLog = globalModelStatus[modelId].errors.join('\n');
        storage.recordModelCheck(modelId, status, errorLog, batchTime, passedAccounts);
      }

      storage.updateAccount(account.id, {
        last_check: batchTime,
        check_result: JSON.stringify({ timestamp: Date.now() }),
      });
    }

    console.log('[GCLI] Check complete');
    res.json({
      success: true,
      message: `Checked ${accounts.length} accounts`,
      totalAccounts: accounts.length,
      batchTime,
    });
  } catch (e) {
    console.error('[GCLI] Model check error:', e);
    res.status(500).json({ error: e.message });
  }
});

/**
 * 获取模型检测历史
 */
router.get('/models/check-history', (req, res) => {
  try {
    const history = storage.getModelCheckHistory();
    res.json(history);
  } catch (e) {
    res.status(500).json({ error: e.message });
  }
});

/**
 * 获取模型检测历史
 */
router.get('/models/check-history', (req, res) => {
  try {
    const history = storage.getModelCheckHistory();
    res.json(history);
  } catch (e) {
    res.status(500).json({ error: e.message });
  }
});

/**
 * 清空模型检测历史
 */
router.post('/models/check-history/clear', (req, res) => {
  try {
    storage.clearModelCheckHistory();
    res.json({ success: true });
  } catch (e) {
    res.status(500).json({ error: e.message });
  }
});

router.get('/accounts', async (req, res) => {
  try {
    const accounts = storage.getAccounts();

    // 尝试验证每个账号的 Token 状态
    const accountsWithStatus = await Promise.all(
      accounts.map(async account => {
        let status = 'online';

        // 获取该账号下所有被限流的模型列表
        const coolDowns = getAccountCoolDowns(account.id);

        try {
          // 仅当没被全局账号级限制（如果将来有）且目前是在线状态时验证
          await client.getAccessToken(account.id);
        } catch (e) {
          console.log(`Account ${account.name} validation failed:`, e.message);
          status = 'error';
        }

        return { ...account, status, coolDowns };
      })
    );

    res.json(accountsWithStatus);
  } catch (e) {
    res.status(500).json({ error: e.message });
  }
});

/**
 * 强制刷新所有账号状态和信息 (Email)
 */
router.post('/accounts/refresh', async (req, res) => {
  try {
    const accounts = storage.getAccounts();
    let refreshed = 0;
    let failed = 0;

    for (const account of accounts) {
      try {
        // 1. 获取 Token (这一步如果过期会自动刷新)
        const token = await client.getAccessToken(account.id);

        // 2. 强制获取最新 UserInfo 和 Project ID (如果缺失)
        const axiosConfig = await client.getAxiosConfig();
        let newEmail = account.email;
        let newProjectId = account.project_id;

        try {
          const userRes = await axios.get('https://www.googleapis.com/oauth2/v2/userinfo', {
            ...axiosConfig,
            headers: { Authorization: `Bearer ${token}` },
          });
          if (userRes.data?.email) newEmail = userRes.data.email;
        } catch (infoErr) {
          console.warn(`Failed to get email for ${account.name}: ${infoErr.message}`);
        }

        if (!newProjectId) {
          try {
            const projRes = await axios.get(
              'https://cloudresourcemanager.googleapis.com/v1/projects',
              {
                ...axiosConfig,
                headers: { Authorization: `Bearer ${token}` },
              }
            );
            const projects = projRes.data.projects || [];
            if (projects.length > 0) newProjectId = projects[0].projectId;
          } catch (projErr) {
            // 忽略 Project ID 获取失败
          }
        }

        if (newEmail !== account.email || newProjectId !== account.project_id) {
          storage.updateAccount(account.id, {
            ...account,
            email: newEmail,
            project_id: newProjectId,
          });

          // 更新 token 记录
          const tokenRecord = storage.getTokenByAccountId(account.id);
          if (tokenRecord) {
            storage.saveToken({
              account_id: account.id,
              ...tokenRecord,
              email: newEmail,
              project_id: newProjectId,
            });
          }
        }

        refreshed++;
      } catch (e) {
        console.error(`Failed to refresh account ${account.name}:`, e.message);
        failed++;
      }
    }

    // 刷新完成后，重新获取最新账号列表返回
    const updatedAccounts = await Promise.all(
      storage.getAccounts().map(async account => {
        // 这里不再验证 Token 状态，因为刷新过程中已经验证/刷新过了，直接返回 online 即可（或者简单验证）
        // 为了响应速度，这里只返回静态数据，状态已经在刷新循环中处理了
        // 如果需要状态，可以简单标记
        return { ...account, status: 'online' };
      })
    );

    res.json({ success: true, refreshed, failed, accounts: updatedAccounts });
  } catch (e) {
    res.status(500).json({ error: e.message });
  }
});

/**
 * 账号管理 - 添加账号
 */
router.post('/accounts', async (req, res) => {
  try {
    const { name, email, client_id, client_secret, refresh_token, project_id } = req.body;
    if (!client_id || !client_secret || !refresh_token) {
      return res.status(400).json({ error: 'Missing OAuth credentials' });
    }
    const id = `acc_${Math.random().toString(36).slice(2, 7)}`;
    await storage.addAccount({
      id,
      name: name || 'Unnamed Account',
      email,
      client_id,
      client_secret,
      refresh_token,
      project_id,
    });
    res.json({ message: 'Account added', id });
  } catch (e) {
    res.status(500).json({ error: e.message });
  }
});

/**
 * 账号管理 - 更新账号
 */
router.put('/accounts/:id', async (req, res) => {
  try {
    const { name, email, client_id, client_secret, refresh_token, project_id } = req.body;
    storage.updateAccount(req.params.id, {
      name,
      email,
      client_id,
      client_secret,
      refresh_token,
      project_id,
    });
    res.json({ message: 'Account updated', id: req.params.id });
  } catch (e) {
    res.status(500).json({ error: e.message });
  }
});

/**
 * 手动获取邮箱
 */
router.post('/accounts/fetch-email', async (req, res) => {
  try {
    const { client_id, client_secret, refresh_token } = req.body;
    if (!client_id || !client_secret || !refresh_token) {
      return res.status(400).json({ error: 'Missing credentials' });
    }

    const axios = require('axios');

    // 刷新 Token 获取 Access Token
    const params = new URLSearchParams({
      client_id,
      client_secret,
      refresh_token,
      grant_type: 'refresh_token',
    });

    const tokenRes = await axios.post('https://oauth2.googleapis.com/token', params.toString(), {
      headers: { 'Content-Type': 'application/x-www-form-urlencoded' },
    });

    // 获取用户信息
    const userRes = await axios.get('https://www.googleapis.com/oauth2/v2/userinfo', {
      headers: { Authorization: `Bearer ${tokenRes.data.access_token}` },
    });

    res.json({ email: userRes.data.email });
  } catch (e) {
    console.error('Fetch email error:', e.response?.data || e.message);
    res.status(500).json({ error: e.response?.data?.error_description || e.message });
  }
});

/**
 * 账号管理 - 删除账号
 */
router.delete('/accounts/:id', async (req, res) => {
  try {
    storage.deleteAccount(req.params.id);
    res.json({ message: 'Account deleted' });
  } catch (e) {
    res.status(500).json({ error: e.message });
  }
});

/**
 * 账号管理 - 切换启用状态
 */
router.post('/accounts/:id/toggle', async (req, res) => {
  try {
    const result = storage.toggleAccount(req.params.id);
    res.json({ message: 'Account toggled', enable: result });
  } catch (e) {
    res.status(500).json({ error: e.message });
  }
});

/**
 * 账号管理 - 导出账号
 */
router.get('/accounts/export', async (req, res) => {
  try {
    const accounts = storage.getAccounts();
    const exportData = {
      version: '1.0',
      type: 'gemini-cli-accounts',
      exportTime: new Date().toISOString(),
      accounts: accounts.map(acc => ({
        name: acc.name,
        email: acc.email,
        client_id: acc.client_id,
        client_secret: acc.client_secret,
        refresh_token: acc.refresh_token,
        project_id: acc.project_id,
      })),
    };
    res.json(exportData);
  } catch (e) {
    res.status(500).json({ error: e.message });
  }
});

/**
 * 账号管理 - 导入账号
 */
router.post('/accounts/import', async (req, res) => {
  try {
    const { accounts } = req.body;
    if (!Array.isArray(accounts)) {
      return res.status(400).json({ error: 'Invalid format: accounts must be an array' });
    }

    let imported = 0;
    let skipped = 0;
    const errors = [];

    for (const acc of accounts) {
      if (!acc.refresh_token) {
        skipped++;
        continue;
      }
      try {
        const id = `acc_${Math.random().toString(36).slice(2, 7)}`;
        storage.addAccount({
          id,
          name: acc.name || `Imported ${imported + 1}`,
          email: acc.email || '',
          client_id: acc.client_id || '',
          client_secret: acc.client_secret || '',
          refresh_token: acc.refresh_token,
          project_id: acc.project_id || '',
        });
        imported++;
      } catch (e) {
        errors.push({ name: acc.name, error: e.message });
        skipped++;
      }
    }

    res.json({ success: true, imported, skipped, errors: errors.length > 0 ? errors : undefined });
  } catch (e) {
    res.status(500).json({ error: e.message });
  }
});

/**
 * 日志管理 - 获取列表（与 Antigravity 格式一致）
 */
router.get('/logs', async (req, res) => {
  try {
    const limit = parseInt(req.query.limit) || 100;
    const logs = storage.getRecentLogs(limit);
    res.json(logs);
  } catch (e) {
    res.status(500).json({ error: e.message });
  }
});

/**
 * 日志管理 - 获取详情
 */
router.get('/logs/:id', async (req, res) => {
  try {
    const log = await storage.getLogDetail(req.params.id);
    if (log && log.detail) {
      log.detail = JSON.parse(log.detail);
    }
    res.json(log);
  } catch (e) {
    res.status(500).json({ error: e.message });
  }
});

/**
 * 日志管理 - 清空日志
 */
router.delete('/logs', async (req, res) => {
  try {
    await storage.clearLogs();
    res.json({ message: 'Logs cleared' });
  } catch (e) {
    res.status(500).json({ error: e.message });
  }
});

/**
 * 设置管理 - 获取设置
 */
router.get('/settings', async (req, res) => {
  try {
    const settings = await storage.getSettings();
    res.json(settings);
  } catch (e) {
    res.status(500).json({ error: e.message });
  }
});

/**
 * 设置管理 - 更新设置
 */
router.post('/settings', async (req, res) => {
  try {
    for (const [key, value] of Object.entries(req.body)) {
      await storage.updateSetting(key, String(value));
    }
    res.json({ message: 'Settings updated' });
  } catch (e) {
    res.status(500).json({ error: e.message });
  }
});

/**
 * 概览统计
 */
router.get('/stats', async (req, res) => {
  try {
    const accounts = await storage.getAccounts();
    const logs = storage.getRecentLogs(10);

    // 简单统计过去 24 小时调用量 (这里为了演示简单处理)
    const stats = {
      total_accounts: accounts.length,
      active_accounts: accounts.filter(a => a.enable !== 0).length,
      total_logs_count: logs.length, // 实际上应该是全部
      recent_logs: logs,
    };
    res.json(stats);
  } catch (e) {
    res.status(500).json({ error: e.message });
  }
});

/**
 * 账号管理 - OAuth Token 交换
 */
router.post('/oauth/exchange', async (req, res) => {
  try {
    const { code, redirect_uri, client_id, client_secret, project_id: customProjectId } = req.body;
    if (!code || !redirect_uri || !client_id || !client_secret) {
      return res
        .status(400)
        .json({ error: 'Missing code, redirect_uri, client_id, or client_secret' });
    }

    const axios = require('axios');
    const params = new URLSearchParams({
      code,
      client_id,
      client_secret,
      redirect_uri,
      grant_type: 'authorization_code',
    });
    const response = await axios.post('https://oauth2.googleapis.com/token', params.toString(), {
      headers: { 'Content-Type': 'application/x-www-form-urlencoded' },
    });

    let project_id = customProjectId || '';
    let email = null;

    // 获取用户邮箱
    try {
      const userRes = await axios.get('https://www.googleapis.com/oauth2/v2/userinfo', {
        headers: { Authorization: `Bearer ${response.data.access_token}` },
      });
      email = userRes.data.email;
    } catch (ue) {
      console.warn('Auto-discover email failed:', ue.message);
    }

    // 尝试自动发现 Project ID (可选)
    if (!project_id) {
      try {
        const projectsRes = await axios.get(
          'https://cloudresourcemanager.googleapis.com/v1/projects',
          {
            headers: { Authorization: `Bearer ${response.data.access_token}` },
          }
        );
        if (projectsRes.data.projects && projectsRes.data.projects.length > 0) {
          // 默认取第一个
          project_id = projectsRes.data.projects[0].projectId;
        }
      } catch (pe) {
        console.warn('Auto-discover Project ID failed:', pe.message);
      }
    }

    res.json({
      ...response.data,
      project_id,
      email,
    });
  } catch (e) {
    console.error('OAuth Exchange Error:', e.response?.data || e.message);
    res.status(e.response?.status || 500).json({
      error: e.response?.data?.error_description || e.message,
    });
  }
});

/**
 * OpenAI 兼容的对话接口
 */
router.post(['/v1/chat/completions', '/chat/completions'], requireApiKey, async (req, res) => {
  const startTime = Date.now();
  try {
    let { model } = req.body;

    // 获取前缀
    const userSettingsService = require('../../src/services/userSettings');
    const globalSettings = userSettingsService.loadUserSettings();
    const prefix = (globalSettings.channelModelPrefix || {})['gemini-cli'] || '';

    // 注意：v1.js 的 dispatch 已经尝试剥离过一次前缀
    // 这里的 model 应当是剥离后的 inner model，或者是没匹配上前缀的完整 ID

    // 如果 model 依然带着前缀，剥离它（兼容直接调用该路由的情况）
    if (prefix && model.startsWith(prefix)) {
      model = model.substring(prefix.length);
    }

    // 处理模型重定向
    const redirects = storage.getModelRedirects();
    const redirect = redirects.find(r => r.source_model === model);
    if (redirect) {
      model = redirect.target_model;
    }

    // 最终的模型 ID (带前缀，用于验证和日志)
    const modelWithPrefix = prefix + model;

    // 验证模型是否有效（即在矩阵配置中存在且未被禁用）
    const availableModels = getAvailableModels(prefix);
    if (!availableModels.find(m => m.id === modelWithPrefix)) {
      const disabledModels = storage.getDisabledModels();
      if (disabledModels.includes(modelWithPrefix)) {
        return res
          .status(403)
          .json({
            error: {
              message: `Model '${modelWithPrefix}' is disabled`,
              type: 'permission_error',
              code: 'model_disabled',
            },
          });
      } else {
        // 如果在 GCLI 矩阵中完全找不到，可能不该由本渠道处理
        return res
          .status(404)
          .json({
            error: {
              message: `Model '${modelWithPrefix}' not found in Gemini CLI matrix`,
              type: 'invalid_request_error',
              code: 'model_not_found',
            },
          });
      }
    }

    // 更新请求中的模型名为剥离前缀后的名字，供 client 使用
    req.body.model = model;

    // 获取所有启用账号
    let allAccounts = (await storage.getAccounts()).filter(a => a.enable !== 0);
    if (allAccounts.length === 0) {
      return res
        .status(503)
        .json({ error: { message: 'No enabled accounts available', type: 'service_unavailable' } });
    }

    // 过滤掉处于冷却期的账号
    allAccounts = allAccounts.filter(a => !isAccountInCoolDown(a.id, model));
    if (allAccounts.length === 0) {
      return res.status(429).json({
        error: {
          message:
            'All available Gemini accounts are currently rate-limited (429). Please try again later.',
          type: 'rate_limit_error',
          code: '429',
        },
      });
    }

    const strategy = globalSettings.load_balancing_strategy || 'random';
    const loadBalancer = require('../../src/utils/loadBalancer');

    // 智能重试逻辑
    const attemptedAccounts = new Set();
    let lastError = null;

    while (attemptedAccounts.size < allAccounts.length) {
      const availableAccounts = allAccounts.filter(a => !attemptedAccounts.has(a.id));
      if (availableAccounts.length === 0) break;

      const account = loadBalancer.getNextAccount('gemini-cli', availableAccounts, strategy);
      attemptedAccounts.add(account.id);

      try {
        if (req.body.stream) {
          res.setHeader('Content-Type', 'text/event-stream');
          res.setHeader('Cache-Control', 'no-cache');
          res.setHeader('Connection', 'keep-alive');

          // 累积流式响应内容用于日志记录
          let fullContent = '';
          let fullReasoning = '';

          const stream = streamProcessor.processStream(req.body, account.id);
          for await (const chunk of stream) {
            res.write(chunk);
            // 尝试解析 chunk 以累积内容
            try {
              if (chunk.startsWith('data: ') && !chunk.includes('[DONE]')) {
                const data = JSON.parse(chunk.slice(6));
                const delta = data.choices?.[0]?.delta;
                if (delta?.content) fullContent += delta.content;
                if (delta?.reasoning_content) fullReasoning += delta.reasoning_content;
              }
            } catch (e) {
              /* 忽略解析错误 */
            }
          }
          res.end();

          // 记录成功日志（包含累积的回复内容）
          const originalMessages = JSON.parse(JSON.stringify(req.body.messages || []));
          const settings = await storage.getSettings();

          // 确保合并系统指令到日志中
          if (!originalMessages.some(m => m.role === 'system') && settings.SYSTEM_INSTRUCTION) {
            originalMessages.unshift({ role: 'system', content: settings.SYSTEM_INSTRUCTION });
          }

          storage.recordLog({
            accountId: account.id,
            model: modelWithPrefix,
            is_balanced: req.lb,
            path: req.path,
            method: req.method,
            statusCode: 200,
            durationMs: Date.now() - startTime,
            clientIp: req.ip,
            userAgent: req.get('user-agent'),
            detail: {
              model: req.body.model,
              messages: originalMessages,
              type: 'stream',
              response: {
                choices: [
                  {
                    message: {
                      role: 'assistant',
                      content: fullContent,
                      reasoning_content: fullReasoning,
                    },
                  },
                ],
              },
            },
          });
          return; // 成功后退出
        } else {
          const response = await client.generateContent(req.body, account.id);
          const geminiData = response.data;

          const candidate = geminiData.candidates?.[0];
          const text = candidate?.content?.parts?.[0]?.text || '';
          const reasoning = candidate?.content?.parts?.find(p => p.thought)?.text || '';

          const responseData = {
            id: `chatcmpl-${Math.random().toString(36).slice(2)}`,
            object: 'chat.completion',
            created: Math.floor(Date.now() / 1000),
            model: modelWithPrefix, // 返回给用户带前缀的 ID
            choices: [
              {
                index: 0,
                message: {
                  role: 'assistant',
                  content: text,
                  reasoning_content: reasoning,
                },
                finish_reason: 'stop',
              },
            ],
            usage: {
              prompt_tokens: geminiData.usageMetadata?.promptTokenCount || 0,
              completion_tokens: geminiData.usageMetadata?.candidatesTokenCount || 0,
              total_tokens: geminiData.usageMetadata?.totalTokenCount || 0,
            },
          };

          // 记录成功日志
          const originalMessages = JSON.parse(JSON.stringify(req.body.messages || []));
          const settings = await storage.getSettings();

          // 确保合并系统指令到日志中
          if (!originalMessages.some(m => m.role === 'system') && settings.SYSTEM_INSTRUCTION) {
            originalMessages.unshift({ role: 'system', content: settings.SYSTEM_INSTRUCTION });
          }

          storage.recordLog({
            accountId: account.id,
            model: modelWithPrefix,
            is_balanced: req.lb,
            path: req.path,
            method: req.method,
            statusCode: 200,
            durationMs: Date.now() - startTime,
            clientIp: req.ip,
            userAgent: req.get('user-agent'),
            detail: {
              model: req.body.model,
              messages: originalMessages,
              response: responseData,
            },
          });

          return res.json(responseData); // 成功后退出
        }
      } catch (error) {
        console.warn(
          `[GCLI] Account ${account.name} failed, trying next... Error: ${error.message}`
        );
        lastError = error;

        // 处理 429 错误并提取重置时间
        if (error.response?.status === 429) {
          const errorData = error.response.data;
          // 支持多种 Google 错误格式中的重置时间字段
          const resetTimeStr =
            errorData?.quotaInfo?.resetTime ||
            errorData?.error?.details?.[0]?.metadata?.quotaResetTimeStamp ||
            errorData?.error?.details?.[0]?.metadata?.resetTime;

          const key = `${account.id}:${model}`;
          if (resetTimeStr) {
            const resetTime = new Date(resetTimeStr).getTime();
            if (!isNaN(resetTime)) {
              console.log(
                `[GCLI] Account ${account.name} model ${model} rate limited until ${resetTimeStr}. Adding to cool-down.`
              );
              accountCoolDowns.set(key, resetTime);
            }
          } else {
            // 如果没找到明确重置时间，默认避让 1 分钟
            accountCoolDowns.set(key, Date.now() + 60000);
          }
        }

        // 记录错误日志
        const originalMessages = JSON.parse(JSON.stringify(req.body.messages || []));
        const settings = await storage.getSettings();

        // 确保合并系统指令到日志中
        if (!originalMessages.some(m => m.role === 'system') && settings.SYSTEM_INSTRUCTION) {
          originalMessages.unshift({ role: 'system', content: settings.SYSTEM_INSTRUCTION });
        }

        storage.recordLog({
          accountId: account.id,
          model: modelWithPrefix,
          is_balanced: req.lb,
          path: req.path,
          method: req.method,
          statusCode: error.response?.status || 500,
          durationMs: Date.now() - startTime,
          clientIp: req.ip,
          userAgent: req.get('user-agent'),
          detail: {
            error: error.message,
            response_data: error.response?.data,
            messages: originalMessages,
            model: req.body.model,
          },
        });

        if (res.headersSent) {
          if (req.body.stream)
            res.write(
              `data: ${JSON.stringify({ error: { message: 'Stream interrupted: ' + error.message } })}\n\n`
            );
          return res.end();
        }
      }
    }

    // 所有账号都尝试过了
    res.status(lastError?.response?.status || 503).json({
      error: {
        message: `All Gemini accounts failed. Last error: ${lastError?.message}`,
        type: 'api_error',
      },
    });
  } catch (e) {
    console.error('Chat Completion General Error:', e.message);
    res.status(500).json({ error: { message: e.message, type: 'api_error' } });
  }
});

module.exports = router;
