const express = require('express');
const storage = require('./storage');
const client = require('./antigravity-client');
const service = require('./antigravity-service');
const { v4: uuidv4 } = require('uuid');
const axios = require('axios');
const crypto = require('crypto');
const fs = require('fs');
const path = require('path');
const { requireAuth } = require('../../src/middleware/auth');
const { getSession, getSessionById } = require('../../src/services/session');

const router = express.Router();

// 内存中的 OAuth State
const AG_OAUTH_STATE = crypto.randomUUID();
const OAUTH_REDIRECT_URI = 'http://localhost:8045/oauth-callback';

// ==================== 服务器端定时检测服务 ====================
const autoCheckService = {
  timerId: null,

  /**
   * 启动定时检测
   */
  start() {
    this.stop();

    const settings = storage.getSettings();
    const enabled = settings.autoCheckEnabled === '1' || settings.autoCheckEnabled === true;
    const intervalMs = parseInt(settings.autoCheckInterval) || 3600000;

    if (!enabled) {
      console.log('[AntiG AutoCheck] 定时检测未启用');
      return;
    }

    console.log(`[AntiG AutoCheck] 定时检测已启动，间隔: ${Math.round(intervalMs / 60000)} 分钟`);

    this.timerId = setInterval(() => {
      this.runCheck();
    }, intervalMs);

    this.nextRunTime = Date.now() + intervalMs;
  },

  /**
   * 停止定时检测
   */
  stop() {
    if (this.timerId) {
      clearInterval(this.timerId);
      this.timerId = null;
      console.log('[AntiG AutoCheck] 定时检测已停止');
    }
  },

  /**
   * 重启定时检测
   */
  restart() {
    console.log('[AntiG AutoCheck] 重新加载设置...');
    this.start();
  },

  /**
   * 执行一次模型检测
   */
  async runCheck() {
    console.log('[AntiG AutoCheck] 开始执行定时模型检测...');

    try {
      const accounts = storage.getAccounts();
      if (accounts.length === 0) {
        console.log('[AntiG AutoCheck] 没有账号，跳过检测');
        return;
      }

      // 获取要检测的模型列表
      const modelConfigs = storage.getModelConfigs();
      let modelsToCheck = Object.keys(modelConfigs).filter(
        modelId => modelConfigs[modelId] === true
      );

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
        const historyModels = storage.getModelCheckHistory().models || [];
        modelsToCheck =
          historyModels.length > 0
            ? historyModels
            : [
                'gemini-3-pro-preview',
                'gemini-3-flash-preview',
                'gemini-2.5-pro',
                'gemini-2.5-flash',
              ];
      }

      const batchTime = Math.floor(Date.now() / 1000);

      for (const modelId of modelsToCheck) {
        storage.recordModelCheck(modelId, 'error', 'Waiting...', batchTime, '');
      }

      const globalModelStatus = {};
      modelsToCheck.forEach(
        m => (globalModelStatus[m] = { ok: false, errors: [], passedIndices: [] })
      );

      console.log(
        `[AntiG AutoCheck] 检测 ${modelsToCheck.length} 个模型，${accounts.length} 个账号 (并行)`
      );

      const checkAccount = async (account, accountIndex) => {
        for (const modelId of modelsToCheck) {
          const isThinkingModel =
            modelId.includes('-thinking') || modelId.includes('opus') || modelId.includes('claude');
          const testMaxTokens = isThinkingModel ? 2048 : 5;

          const testRequest = {
            model: modelId,
            messages: [{ role: 'user', content: 'Hi' }],
            max_tokens: testMaxTokens,
            stream: false,
          };

          try {
            const timeoutPromise = new Promise((_, reject) =>
              setTimeout(() => reject(new Error('Timeout')), 15000)
            );

            const response = await Promise.race([
              client.chatCompletions(account.id, testRequest),
              timeoutPromise,
            ]);

            const hasContent = response?.choices?.[0]?.message?.content !== undefined;
            const hasToolCalls = Array.isArray(response?.choices?.[0]?.message?.tool_calls);

            if (response && (hasContent || hasToolCalls)) {
              globalModelStatus[modelId].ok = true;
              globalModelStatus[modelId].passedIndices.push(accountIndex);
            } else {
              const errorMsg = response?.error?.message || 'Unexpected response';
              globalModelStatus[modelId].errors.push(`${account.name}: ${errorMsg}`);
            }
          } catch (e) {
            const errorMsg = e.response?.data?.error?.message || e.message;
            globalModelStatus[modelId].errors.push(`${account.name}: ${errorMsg}`);
          }

          // 实时更新数据库
          const passedAccounts = globalModelStatus[modelId].passedIndices
            .sort((a, b) => a - b)
            .join(',');
          const status = globalModelStatus[modelId].ok ? 'ok' : 'error';
          const errorLog = globalModelStatus[modelId].errors.join('\n');
          storage.recordModelCheck(modelId, status, errorLog, batchTime, passedAccounts);
        }
      };

      // 并行执行所有账号检测
      await Promise.allSettled(accounts.map((account, i) => checkAccount(account, i + 1)));

      // 检测完成后统一更新数据库
      for (const modelId of modelsToCheck) {
        const passedAccounts = globalModelStatus[modelId].passedIndices
          .sort((a, b) => a - b)
          .join(',');
        const status = globalModelStatus[modelId].ok ? 'ok' : 'error';
        const errorLog = globalModelStatus[modelId].errors.join('\n');
        storage.recordModelCheck(modelId, status, errorLog, batchTime, passedAccounts);
      }

      console.log('[AntiG AutoCheck] 定时检测完成');
    } catch (error) {
      console.error('[AntiG AutoCheck] 定时检测失败:', error.message);
    }
  },

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
}, 2000);

/**
 * API Key 认证中间件
 * 允许:
 * 1. 有效的 Admin Session
 * 2. Visualization Header "Authorization: Bearer <API_KEY>"
 */
function requireApiAuth(req, res, next) {
  // 1. 检查 Session / 聚合器分发
  if (req.lb) return next(); // 如果经过负载均衡器验证，直接放行
  const session = getSession(req);
  if (session) return next();

  // 2. 检查 Authorization Header (API Key)
  const authHeader = req.headers['authorization'];
  if (authHeader && authHeader.startsWith('Bearer ')) {
    const token = authHeader.substring(7);

    // 尝试作为 Session ID
    const sessionById = getSessionById(token);
    if (sessionById) return next();

    // 尝试作为 API Key
    const configuredApiKey = storage.getSetting('API_KEY');
    if (configuredApiKey && token === configuredApiKey) {
      return next();
    }
  }

  // 3. 检查 Query Param (compat)
  const queryKey = req.query.key;
  if (queryKey) {
    const configuredApiKey = storage.getSetting('API_KEY');
    if (configuredApiKey && queryKey === configuredApiKey) {
      return next();
    }
  }

  res
    .status(401)
    .json({
      error: {
        message: 'Invalid API Key or Session',
        type: 'invalid_request_error',
        code: 'invalid_api_key',
      },
    });
}

// ============== 管理接口 (需 Admin 权限) ==============

// 所有管理接口使用 requireAuth
router.use(
  ['/accounts', '/settings', '/logs', '/oauth', '/stats', '/quotas', '/config/matrix'],
  requireAuth
);

// 全局渠道启用状态检查中间件
function requireChannelEnabled(req, res, next) {
  const userSettingsService = require('../../src/services/userSettings');
  const settings = userSettingsService.loadUserSettings();
  const channelEnabled = settings.channelEnabled || {};

  if (channelEnabled['antigravity'] === false) {
    return res.status(403).json({
      error: {
        message: 'Antigravity channel is disabled in global settings.',
        type: 'permission_error',
        code: 'channel_disabled',
      },
    });
  }
  next();
}

// 所有 /v1 接口受全局启用状态控制
router.use('/v1', requireChannelEnabled);

/**
 * 获取模型矩阵配置 (内部 API)
 */
router.get('/config/matrix', requireAuth, (req, res) => {
  res.json(service.getMatrixConfig());
});

/**
 * 更新模型矩阵配置 (内部 API)
 */
router.post('/config/matrix', requireAuth, (req, res) => {
  const newConfig = req.body;
  if (service.saveMatrixConfig(newConfig)) {
    res.json({ success: true });
  } else {
    res.status(500).json({ error: 'Failed to save configuration' });
  }
});

// OAuth 配置 (与 client.js 保持一致)
const GOOGLE_CLIENT_ID =
  '1071006060591-tmhssin2h21lcre235vtolojh4g403ep.apps.googleusercontent.com';
const GOOGLE_CLIENT_SECRET = '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';
const SCOPES = [
  'https://www.googleapis.com/auth/cloud-platform',
  'https://www.googleapis.com/auth/userinfo.email',
  'https://www.googleapis.com/auth/userinfo.profile',
  'https://www.googleapis.com/auth/cclog',
  'https://www.googleapis.com/auth/experimentsandconfigs',
];

// 获取所有账号
router.get('/accounts', (req, res) => {
  try {
    const accounts = storage.getAccounts();
    res.json(accounts);
  } catch (error) {
    res.status(500).json({ error: error.message });
  }
});

// 获取设置
router.get('/settings', (req, res) => {
  try {
    const settings = storage.getSettings();
    res.json(settings);
  } catch (error) {
    res.status(500).json({ error: error.message });
  }
});

// 保存设置
router.post('/settings', (req, res) => {
  try {
    const body = req.body;
    if (!body) return res.status(400).json({ error: 'Body required' });

    // 兼容格式 1: { key: "PROXY", value: "..." }
    if (body.key !== undefined && body.value !== undefined) {
      storage.updateSetting(body.key, body.value);
    } else {
      // 兼容格式 2: { "PROXY": "...", "API_KEY": "..." }
      for (const [key, value] of Object.entries(body)) {
        if (value !== undefined) {
          storage.updateSetting(key, value);
        }
      }
    }

    // 如果有特定的模块刷新需求可以在此添加，Antigravity 暂无 autoCheckService
    res.json({ success: true });
  } catch (error) {
    console.error('Save settings error:', error);
    res.status(500).json({ error: error.message });
  }
});

// 刷新所有凭证
router.post('/accounts/refresh-all', async (req, res) => {
  try {
    const results = await client.refreshAllAccounts();
    res.json({
      success: true,
      total: results.total || 0,
      success_count: results.success || 0,
      fail_count: results.fail || 0,
    });
  } catch (error) {
    res.status(500).json({ error: error.message });
  }
});

// 手动添加/更新账号属性 (非 OAuth 流程)
router.put('/accounts/:id', (req, res) => {
  try {
    const { enable, name, email } = req.body;
    const accountId = req.params.id;

    // 如果是切换启用状态
    if (enable !== undefined) {
      storage.updateAccount(accountId, { enable: enable ? 1 : 0 });
    } else {
      storage.updateAccount(accountId, { name, email });
    }

    res.json({ success: true });
  } catch (error) {
    res.status(500).json({ error: error.message });
  }
});

// 删除账号
router.delete('/accounts/:id', (req, res) => {
  try {
    storage.deleteAccount(req.params.id);
    res.json({ success: true });
  } catch (error) {
    res.status(500).json({ error: error.message });
  }
});

// 导出账号
router.get('/accounts/export', async (req, res) => {
  try {
    const accounts = storage.getAccounts();
    const tokens = storage.getTokens();

    const exportData = {
      version: '1.0',
      type: 'antigravity-accounts',
      exportTime: new Date().toISOString(),
      accounts: accounts
        .map(acc => {
          const token = tokens.find(t => t.account_id === acc.id);
          return {
            name: acc.name,
            email: acc.email,
            refresh_token: token?.refresh_token || '',
            project_id: token?.project_id || '',
          };
        })
        .filter(acc => acc.refresh_token), // 只导出有 refresh_token 的
    };
    res.json(exportData);
  } catch (e) {
    res.status(500).json({ error: e.message });
  }
});

// 导入账号
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
        // 需要先获取 access_token
        const params = new URLSearchParams({
          client_id: GOOGLE_CLIENT_ID,
          client_secret: GOOGLE_CLIENT_SECRET,
          refresh_token: acc.refresh_token,
          grant_type: 'refresh_token',
        });

        const tokenRes = await axios.post(
          'https://oauth2.googleapis.com/token',
          params.toString(),
          {
            headers: { 'Content-Type': 'application/x-www-form-urlencoded' },
          }
        );

        const accessToken = tokenRes.data.access_token;

        // 获取 email
        let email = acc.email || '';
        let projectId = acc.project_id || '';

        if (!email) {
          try {
            const userRes = await axios.get('https://www.googleapis.com/oauth2/v2/userinfo', {
              headers: { Authorization: `Bearer ${accessToken}` },
            });
            email = userRes.data.email || '';
          } catch (e) {}
        }

        if (!projectId) {
          try {
            const projRes = await axios.get(
              'https://cloudresourcemanager.googleapis.com/v1/projects',
              {
                headers: { Authorization: `Bearer ${accessToken}` },
              }
            );
            const projects = projRes.data.projects || [];
            if (projects.length > 0) projectId = projects[0].projectId;
          } catch (e) {}
        }

        if (!projectId) {
          projectId = `antigravity-import-${Math.random().toString(36).substring(2, 8)}`;
        }

        const newAcc = storage.addAccount({
          name: acc.name || email || `Imported ${imported + 1}`,
          email: email,
          enable: true,
        });

        storage.saveToken({
          accountId: newAcc.id,
          accessToken,
          refreshToken: acc.refresh_token,
          expiresIn: tokenRes.data.expires_in || 3599,
          timestamp: Date.now(),
          projectId,
          email,
          userEmail: email,
        });

        storage.updateAccount(newAcc.id, { status: 'online' });
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
 * 模型健康检测 - 对所有账号执行测试
 */
router.post('/accounts/check', async (req, res) => {
  console.log('[AntiG] Starting model check (dialog test)...');
  try {
    const accounts = storage.getAccounts();
    // 检测所有账号，不仅仅是启用的
    const accountsToCheck = accounts;

    if (accountsToCheck.length === 0) {
      return res.json({
        success: true,
        message: 'No accounts to check',
        totalAccounts: 0,
      });
    }

    // 获取要检测的模型列表 - 使用 getModelConfigs 获取已启用的模型，与额度状态页面一致
    const modelConfigs = storage.getModelConfigs();
    let modelsToCheck = Object.keys(modelConfigs).filter(modelId => modelConfigs[modelId] === true);

    // 获取禁用的模型列表
    const settings = storage.getSettings();
    let disabledModels = [];
    if (settings.disabledCheckModels) {
      try {
        disabledModels = JSON.parse(settings.disabledCheckModels);
      } catch (e) {
        disabledModels = [];
      }
    }

    // 过滤掉当前禁用的模型
    if (disabledModels.length > 0) {
      modelsToCheck = modelsToCheck.filter(m => !disabledModels.includes(m));
    }

    // 如果没有已启用的模型，从历史记录补充
    if (modelsToCheck.length === 0) {
      const historyModels = storage.getModelCheckHistory().models || [];
      modelsToCheck = historyModels;
    }

    // 如果仍然没有模型，使用兜底方案
    if (modelsToCheck.length === 0) {
      modelsToCheck = [
        'gemini-3-pro-preview',
        'gemini-3-flash-preview',
        'gemini-2.5-pro',
        'gemini-2.5-flash',
        'gemini-1.5-pro',
        'gemini-1.5-flash',
      ];
    }

    console.log(
      `[AntiG] 将检测 ${modelsToCheck.length} 个模型: ${modelsToCheck.slice(0, 5).join(', ')}...`
    );

    // 统一批次时间戳
    const batchTime = Math.floor(Date.now() / 1000);

    // 初始化历史记录 (占位)，让表格能立即显示新列
    for (const modelId of modelsToCheck) {
      storage.recordModelCheck(modelId, 'error', 'Waiting...', batchTime, '');
    }

    // 记录全局模型健康状态
    const globalModelStatus = {};
    modelsToCheck.forEach(
      m => (globalModelStatus[m] = { ok: false, errors: [], passedIndices: [] })
    );

    console.log(
      `[AntiG] Checking ${modelsToCheck.length} models across ${accountsToCheck.length} accounts at ${batchTime}`
    );

    // 并行检测所有账号
    console.log(`[AntiG] 并行检测 ${accountsToCheck.length} 个账号...`);

    const checkAccount = async (account, accountIndex) => {
      console.log(`[AntiG] 开始检测账号 #${accountIndex}: ${account.name || account.id}`);

      for (const modelId of modelsToCheck) {
        // Claude thinking 模型要求 max_tokens > thinking.budget_tokens (1024)
        const isThinkingModel =
          modelId.includes('-thinking') || modelId.includes('opus') || modelId.includes('claude');
        const testMaxTokens = isThinkingModel ? 2048 : 5;

        const testRequest = {
          model: modelId,
          messages: [{ role: 'user', content: 'Hi' }],
          max_tokens: testMaxTokens,
          stream: false,
        };

        try {
          // 添加超时 (15秒)
          const timeoutPromise = new Promise((_, reject) =>
            setTimeout(() => reject(new Error('Timeout')), 15000)
          );

          const response = await Promise.race([
            client.chatCompletions(account.id, testRequest),
            timeoutPromise,
          ]);

          const hasContent = response?.choices?.[0]?.message?.content !== undefined;
          const hasToolCalls = Array.isArray(response?.choices?.[0]?.message?.tool_calls);

          if (response && (hasContent || hasToolCalls)) {
            globalModelStatus[modelId].ok = true;
            globalModelStatus[modelId].passedIndices.push(accountIndex);
            console.log(
              `\x1b[32m[Antigravity]      ✓ ${modelId} passed for ${account.name}\x1b[0m`
            );
          } else {
            const errorMsg =
              response && response.error ? response.error.message : 'Unexpected response structure';
            globalModelStatus[modelId].errors.push(`${account.name}: ${errorMsg}`);
            console.log(
              `\x1b[31m[Antigravity]      ✗ ${modelId} failed for ${account.name}: ${errorMsg}\x1b[0m`
            );
          }
        } catch (e) {
          const errorMsg = e.response?.data?.error?.message || e.message || 'Unknown error';
          globalModelStatus[modelId].errors.push(`${account.name}: ${errorMsg}`);
          console.log(
            `\x1b[31m[Antigravity]      ✗ ${modelId} failed for ${account.name}: ${errorMsg}\x1b[0m`
          );
        }

        // 实时更新数据库，让前端能看到进度
        const passedAccounts = globalModelStatus[modelId].passedIndices
          .sort((a, b) => a - b)
          .join(',');
        const status = globalModelStatus[modelId].ok ? 'ok' : 'error';
        const errorLog = globalModelStatus[modelId].errors.join('\n');
        storage.recordModelCheck(modelId, status, errorLog, batchTime, passedAccounts);
      }

      // 更新检测完成后的单个账号状态
      storage.updateAccount(account.id, {
        last_check: batchTime,
        check_result: JSON.stringify({
          status: 'checked',
          timestamp: Date.now(),
        }),
      });

      return { accountId: account.id, accountIndex };
    };

    // 并行执行所有账号检测
    await Promise.allSettled(accountsToCheck.map((account, i) => checkAccount(account, i + 1)));

    // 检测完成后统一更新数据库
    for (const modelId of modelsToCheck) {
      const passedAccounts = globalModelStatus[modelId].passedIndices
        .sort((a, b) => a - b)
        .join(',');
      const status = globalModelStatus[modelId].ok ? 'ok' : 'error';
      const errorLog = globalModelStatus[modelId].errors.join('\n');
      storage.recordModelCheck(modelId, status, errorLog, batchTime, passedAccounts);
    }

    console.log(`[AntiG] Check complete for ${accountsToCheck.length} accounts`);
    res.json({
      success: true,
      message: `Checked ${accountsToCheck.length} accounts`,
      totalAccounts: accountsToCheck.length,
      batchTime,
    });
  } catch (e) {
    console.error('[AntiG] Model check error:', e);
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

// 注入模型启用状态
function injectModelStatus(quotas) {
  if (!quotas) return quotas;
  const configs = storage.getModelConfigs();

  // 遍历所有分组
  Object.values(quotas).forEach(group => {
    if (group.models && Array.isArray(group.models)) {
      group.models.forEach(model => {
        // 如果 configs 中没有记录，默认为 true
        const isEnabled = configs[model.id] !== undefined ? configs[model.id] : true;
        model.enabled = isEnabled;
      });
    }
  });
  return quotas;
}

// 获取所有账号的额度汇总
router.get('/quotas', async (req, res) => {
  try {
    const accounts = storage.getAccounts();
    const enabledAccounts = accounts.filter(a => a.enable);

    if (enabledAccounts.length === 0) {
      return res.json({});
    }

    // 目前简单取第一个有效账号的额度，或者汇总所有账号
    // 由于 Antigravity 通常是共享模型配额，取一个即可，
    // 这里为了准确，尝试取第一个有 Token 的账号
    let quotas = {};
    for (const account of enabledAccounts) {
      try {
        quotas = await client.listQuotas(account.id);
        if (Object.keys(quotas).length > 0) break;
      } catch (e) {
        console.error(`Fetch quotas failed for ${account.name}:`, e.message);
      }
    }

    res.json(injectModelStatus(quotas));
  } catch (e) {
    res.status(500).json({ error: e.message });
  }
});

// 获取设置
router.get('/settings', (req, res) => {
  try {
    // storage.getSettings() 返回的是 { key: value } 对象
    const settingsMap = storage.getSettings();

    const allSettings = [
      // 1. 安全配置
      {
        key: 'API_KEY',
        value: settingsMap['API_KEY'] || '',
        category: 'auth',
        description: '用于保护 /v1/* 端点的访问密钥',
      },

      // 2. 服务与网络
      {
        key: 'PORT',
        value: settingsMap['PORT'] || '8045',
        category: 'network',
        description: '服务监听端口',
      },
      {
        key: 'HOST',
        value: settingsMap['HOST'] || '0.0.0.0',
        category: 'network',
        description: '服务监听地址',
      },
      {
        key: 'PROXY',
        value: settingsMap['PROXY'] || '',
        category: 'network',
        description: 'HTTP 代理服务器地址',
      },
      {
        key: 'TIMEOUT',
        value: settingsMap['TIMEOUT'] || '180000',
        category: 'network',
        description: '请求超时时间 (ms)',
      },
      {
        key: 'USE_NATIVE_AXIOS',
        value: settingsMap['USE_NATIVE_AXIOS'] || 'false',
        category: 'network',
        description: '是否使用原生 Axios (不通过 Go 客户端)',
      },

      // 3. API 端点配置
      {
        key: 'API_URL',
        value:
          settingsMap['API_URL'] ||
          'https://daily-cloudcode-pa.sandbox.googleapis.com/v1internal:streamGenerateContent?alt=sse',
        category: 'params',
        description: '流式接口 URL',
      },
      {
        key: 'API_MODELS_URL',
        value:
          settingsMap['API_MODELS_URL'] ||
          'https://daily-cloudcode-pa.sandbox.googleapis.com/v1internal:fetchAvailableModels',
        category: 'params',
        description: '模型列表 URL',
      },
      {
        key: 'API_NO_STREAM_URL',
        value:
          settingsMap['API_NO_STREAM_URL'] ||
          'https://daily-cloudcode-pa.sandbox.googleapis.com/v1internal:generateContent',
        category: 'params',
        description: '非流式接口 URL',
      },
      {
        key: 'API_HOST',
        value: settingsMap['API_HOST'] || 'daily-cloudcode-pa.sandbox.googleapis.com',
        category: 'params',
        description: 'API Host 头',
      },
      {
        key: 'API_USER_AGENT',
        value: settingsMap['API_USER_AGENT'] || 'antigravity/1.11.3 windows/amd64',
        category: 'params',
        description: 'API 请求 User-Agent',
      },

      // 4. 其他原有配置
      {
        key: 'CREDENTIAL_MAX_USAGE_PER_HOUR',
        value: settingsMap['CREDENTIAL_MAX_USAGE_PER_HOUR'] || '20',
        category: 'quota',
        description: '每小时凭证最大使用次数',
      },
      {
        key: 'REQUEST_LOG_RETENTION_DAYS',
        value: settingsMap['REQUEST_LOG_RETENTION_DAYS'] || '7',
        category: 'logs',
        description: '日志保留天数',
      },
    ];

    res.json(allSettings);
  } catch (e) {
    res.status(500).json({ error: e.message });
  }
});

// 切换模型启用状态
router.post('/models/:id/status', (req, res) => {
  const modelId = req.params.id;
  const { enabled } = req.body;

  try {
    if (enabled === undefined) {
      return res.status(400).json({ error: 'Enabled status required' });
    }
    storage.updateModelStatus(modelId, enabled);
    res.json({ success: true, modelId, enabled });
  } catch (e) {
    res.status(500).json({ error: e.message });
  }
});

// 获取模型重定向列表
router.get('/models/redirects', requireApiAuth, (req, res) => {
  try {
    const redirects = storage.getModelRedirects();
    res.json(redirects);
  } catch (e) {
    res.status(500).json({ error: e.message });
  }
});

// 添加模型重定向
router.post('/models/redirects', requireApiAuth, (req, res) => {
  const { sourceModel, targetModel } = req.body;
  if (!sourceModel || !targetModel) {
    return res.status(400).json({ error: 'Source and target models required' });
  }

  // 防止循环重定向
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

// 删除模型重定向
router.delete('/models/redirects/:sourceModel', requireApiAuth, (req, res) => {
  const { sourceModel } = req.params;
  try {
    storage.removeModelRedirect(sourceModel);
    res.json({ success: true, sourceModel });
  } catch (e) {
    res.status(500).json({ error: e.message });
  }
});

// 获取 Google OAuth URL
router.get('/oauth/url', (req, res) => {
  // 动态获取当前请求的 Origin
  // const protocol = req.headers['x-forwarded-proto'] || req.protocol;
  // const host = req.headers['x-forwarded-host'] || req.get('host');
  // const redirectUri = `${protocol}://${host}/oauth-callback`;

  const params = new URLSearchParams({
    access_type: 'offline',
    client_id: GOOGLE_CLIENT_ID,
    prompt: 'consent',
    redirect_uri: OAUTH_REDIRECT_URI,
    response_type: 'code',
    scope: SCOPES.join(' '),
    state: AG_OAUTH_STATE,
  });
  const url = `https://accounts.google.com/o/oauth2/v2/auth?${params.toString()}`;
  res.json({ url });
});

// 处理解析回调 URL
router.post('/oauth/parse-url', async (req, res) => {
  const { url, replaceId, customProjectId, allowRandomProjectId } = req.body;
  if (!url) return res.status(400).json({ error: 'URL is required' });

  try {
    // 尝试作为完整 URL 解析，如果用户只粘贴了查询字符串，则添加一个虚拟主机使其可解析
    const parsedUrl = new URL(url.includes('://') ? url : `http://localhost?${url}`);
    const code = parsedUrl.searchParams.get('code') || url.trim(); // 如果没有 'code' 参数，则尝试将整个 URL 视为 code
    const state = parsedUrl.searchParams.get('state');

    if (!code || code.length < 10) {
      console.error('Invalid authorization code or URL:', url);
      return res.status(400).json({ error: '无效的授权 Code 或 URL' });
    }

    // 校验 state 避免 CSRF（可选，但原程序有校验）
    if (state && state !== AG_OAUTH_STATE) {
      console.warn('OAuth state mismatch:', {
        received: state,
        expected: AG_OAUTH_STATE,
        expectedGlobal: AG_OAUTH_STATE,
      });
      // 为了提高兼容性，暂时打印警告但不强制拦截（如果用户刷新了页面，AG_OAUTH_STATE 可能会变）
    } else if (!state) {
      console.warn('OAuth state missing from callback URL.');
    }

    // 已经回归固定回调策略
    // const protocol = req.headers['x-forwarded-proto'] || req.protocol;
    // const host = req.headers['x-forwarded-host'] || req.get('host');
    // const redirectUri = `${protocol}://${host}/oauth-callback`;

    // 构造交换参数
    const params = new URLSearchParams({
      code,
      client_id: GOOGLE_CLIENT_ID,
      redirect_uri: OAUTH_REDIRECT_URI,
      grant_type: 'authorization_code',
    });
    if (GOOGLE_CLIENT_SECRET) {
      params.append('client_secret', GOOGLE_CLIENT_SECRET);
    }

    // 交换 Token (增加原程序中使用的 User-Agent 等关键 Header)
    let tokenData;
    try {
      console.log('Exchanging code for token...', {
        code: code.substring(0, 5) + '...',
        redirectUri: OAUTH_REDIRECT_URI,
      });
      const tokenRes = await axios({
        method: 'POST',
        url: 'https://oauth2.googleapis.com/token',
        headers: {
          Host: 'oauth2.googleapis.com',
          'User-Agent': 'Go-http-client/1.1',
          'Content-Type': 'application/x-www-form-urlencoded',
          'Accept-Encoding': 'gzip',
        },
        data: params.toString(),
        timeout: 30000, // 默认 30s 超时
      });
      tokenData = tokenRes.data;
    } catch (tokenErr) {
      const errorBody = tokenErr.response?.data;
      console.error('Code exchange failed:', errorBody || tokenErr.message);
      const errMsg = errorBody?.error_description || errorBody?.error || tokenErr.message;
      return res.status(400).json({
        error: `Google 授权交换失败: ${errMsg}`,
        details: errorBody,
        hint: '请确保您在点击“获取授权链接”生成 URL 后，授权后转跳的 8045 端口 URL 被完整复制回来。Google 仅认可 8045 端口作为重定向地址。',
      });
    }
    let projectId = customProjectId || null;
    let email = null;

    // 获取用户信息和项目 ID
    try {
      const userRes = await axios.get('https://www.googleapis.com/oauth2/v2/userinfo', {
        headers: { Authorization: `Bearer ${tokenData.access_token}` },
      });
      email = userRes.data.email;

      if (!projectId) {
        const projRes = await axios.get('https://cloudresourcemanager.googleapis.com/v1/projects', {
          headers: { Authorization: `Bearer ${tokenData.access_token}` },
        });
        const projects = projRes.data.projects || [];
        if (projects.length > 0) {
          projectId = projects[0].projectId;
        }
      }
    } catch (e) {
      console.error('Failed to fetch additional info:', e.message);
    }

    if (!projectId && allowRandomProjectId) {
      projectId = `antigravity-${Math.random().toString(36).substring(2, 10)}`;
    }

    if (!projectId) {
      return res
        .status(400)
        .json({
          error: '无法自动获取项目 ID，请手动输入或勾选允许随机 ID',
          code: 'PROJECT_ID_MISSING',
        });
    }

    // 保存账号和 Token
    let accountId = replaceId;
    if (!accountId) {
      const acc = storage.addAccount({
        name: email || 'Google Account',
        email: email,
        enable: true,
      });
      accountId = acc.id;
    }

    storage.saveToken({
      accountId,
      accessToken: tokenData.access_token,
      refreshToken: tokenData.refresh_token,
      expiresIn: tokenData.expires_in,
      timestamp: Date.now(),
      projectId,
      email: email,
      userEmail: email,
    });

    // 成功添加后更新状态为 online
    storage.updateAccount(accountId, { status: 'online' });

    res.json({ success: true, accountId });
  } catch (error) {
    console.error('OAuth parse failed:', error.response?.data || error.message);
    res.status(500).json({ error: error.message });
  }
});

// 批量刷新账号
router.post('/accounts/refresh-all', async (req, res) => {
  const accounts = storage.getAccounts().filter(a => a.enable);
  let refreshed = 0;
  let failed = 0;

  for (const acc of accounts) {
    try {
      const accessToken = await client.getValidToken(acc.id);
      if (!accessToken) throw new Error('Failed to get token');

      // 自动刷新邮箱信息和项目 ID

      // 1. 刷新邮箱
      let newEmail = null;
      try {
        // 尝试 v2 接口
        let userRes;
        try {
          userRes = await axios.get('https://www.googleapis.com/oauth2/v2/userinfo', {
            headers: {
              Authorization: `Bearer ${accessToken}`,
              'User-Agent': 'antigravity/1.11.3',
            },
            timeout: 5000,
          });
        } catch (v2Err) {
          // 如果 v2 失败，尝试 v3
          console.warn(`UserInfo v2 failed for ${acc.id}, trying v3:`, v2Err.message);
          userRes = await axios.get('https://www.googleapis.com/oauth2/v3/userinfo', {
            headers: {
              Authorization: `Bearer ${accessToken}`,
              'User-Agent': 'antigravity/1.11.3',
            },
            timeout: 5000,
          });
        }

        if (userRes.data && userRes.data.email) {
          newEmail = userRes.data.email;
          if (newEmail !== acc.email) {
            storage.updateAccount(acc.id, { email: newEmail });

            // 记录成功的更新 (Info level)
            storage.recordLog({
              accountId: acc.id,
              path: 'refresh-email',
              method: 'INTERNAL',
              statusCode: 200,
              durationMs: 0,
              detail: { message: 'Email updated', oldEmail: acc.email, newEmail },
            });
          }
        } else {
          storage.recordLog({
            accountId: acc.id,
            path: 'refresh-email',
            method: 'INTERNAL',
            statusCode: 200,
            durationMs: 0,
            detail: { warning: 'No email field in response', data: userRes.data },
          });
        }
      } catch (userInfoError) {
        const status = userInfoError.response?.status || 0;
        const errorDetail = userInfoError.response?.data || userInfoError.message;
        console.warn(`Failed to refresh user info for ${acc.id}:`, userInfoError.message);

        // 记录错误到日志
        storage.recordLog({
          accountId: acc.id,
          path: 'refresh-email',
          method: 'INTERNAL',
          statusCode: status,
          durationMs: 0,
          detail: { error: 'Failed to fetch userinfo', detail: errorDetail },
        });
      }

      // 2. 刷新项目 ID (如果缺失) 并更新 Token
      try {
        const currentToken = storage.getTokenByAccountId(acc.id);
        if (currentToken) {
          let newProjectId = currentToken.project_id;

          if (!newProjectId) {
            try {
              const projRes = await axios.get(
                'https://cloudresourcemanager.googleapis.com/v1/projects',
                {
                  headers: { Authorization: `Bearer ${accessToken}` },
                  timeout: 10000,
                }
              );
              const projects = projRes.data.projects || [];
              if (projects.length > 0) {
                newProjectId = projects[0].projectId;
              }
            } catch (projErr) {
              // 忽略 Project ID 获取失败
            }
          }

          // 构造符合 saveToken 期望的驼峰命名对象
          const tokenData = {
            accountId: currentToken.account_id,
            accessToken: currentToken.access_token,
            refreshToken: currentToken.refresh_token,
            expiresIn: currentToken.expires_in,
            timestamp: currentToken.timestamp,
            projectId: newProjectId,
            email: newEmail || currentToken.email,
            userId: currentToken.user_id,
            userEmail: newEmail || currentToken.user_email,
          };
          storage.saveToken(tokenData);
        }
      } catch (tokenErr) {
        console.warn(`Failed to update token info for ${acc.id}:`, tokenErr.message);
      }

      storage.updateAccount(acc.id, { status: 'online' });
      refreshed++;
    } catch (e) {
      storage.updateAccount(acc.id, { status: 'error' });
      failed++;
    }
  }

  // 获取最新列表返回
  const updatedAccounts = storage.getAccounts();
  res.json({ success: true, refreshed, failed, total: accounts.length, accounts: updatedAccounts });
});

// 手动添加账号 (Access Token + Refresh Token)
router.post('/accounts/manual', async (req, res) => {
  try {
    const { name, accessToken, refreshToken, projectId, expiresIn } = req.body;

    if (!accessToken || !refreshToken) {
      return res.status(400).json({ error: 'Access Token and Refresh Token are required' });
    }

    let pId = projectId;
    let email = '';

    // 尝试获取用户信息
    try {
      const userRes = await axios.get('https://www.googleapis.com/oauth2/v2/userinfo', {
        headers: { Authorization: `Bearer ${accessToken}` },
      });
      email = userRes.data.email;

      if (!pId) {
        const projRes = await axios.get('https://cloudresourcemanager.googleapis.com/v1/projects', {
          headers: { Authorization: `Bearer ${accessToken}` },
        });
        const projects = projRes.data.projects || [];
        if (projects.length > 0) {
          pId = projects[0].projectId;
        }
      }
    } catch (e) {
      console.warn('Manual add: failed to fetch user info or project:', e.message);
    }

    // 如果获取失败，允许为空或者生成随机ID if not critical
    if (!pId) {
      // 尝试从 accessToken 解析？ 通常不行，只能随机或者报错
      // 这里允许用户手动填入，如果没填入且获取不到，则给个默认值
      pId = `antigravity-manual-${Math.random().toString(36).substring(2, 8)}`;
    }

    const acc = storage.addAccount({
      name: name || email || 'Manual Account',
      email: email,
      enable: true,
    });

    storage.saveToken({
      accountId: acc.id,
      accessToken,
      refreshToken,
      expiresIn: expiresIn || 3599,
      timestamp: Date.now(),
      projectId: pId,
      email,
      userEmail: email,
    });

    storage.updateAccount(acc.id, { status: 'online' });

    res.json({ success: true, accountId: acc.id });
  } catch (error) {
    res.status(500).json({ error: error.message });
  }
});

// 获取统计信息
router.get('/stats', (req, res) => {
  try {
    const stats = storage.getStats();
    res.json(stats);
  } catch (error) {
    res.status(500).json({ error: error.message });
  }
});

// 刷新单个账号项目 ID
router.post('/accounts/:id/refresh-project-id', async (req, res) => {
  try {
    const accessToken = await client.getValidToken(req.params.id);
    if (!accessToken) throw new Error('Failed to get access token');

    const projRes = await axios.get('https://cloudresourcemanager.googleapis.com/v1/projects', {
      headers: { Authorization: `Bearer ${accessToken}` },
    });
    const projects = projRes.data.projects || [];
    if (projects.length === 0) throw new Error('No projects found');

    const projectId = projects[0].projectId;
    const currentToken = storage.getTokenByAccountId(req.params.id);
    storage.saveToken({ ...currentToken, accountId: req.params.id, accessToken, projectId });

    storage.updateAccount(req.params.id, { status: 'online' });

    res.json({ success: true, projectId });
  } catch (error) {
    storage.updateAccount(req.params.id, { status: 'error' });
    res.status(500).json({ error: error.message });
  }
});

// 查询账号额度
router.get('/accounts/:id/quotas', async (req, res) => {
  try {
    const accountId = req.params.id;
    // 验证账号是否存在且启用
    const account = storage.getAccountById(accountId);
    if (!account) {
      return res.status(404).json({ error: 'Account not found' });
    }

    const quotas = await client.listQuotas(accountId);
    res.json(injectModelStatus(quotas));
  } catch (error) {
    console.error(`Fetch quotas failed for account ${req.params.id}:`, error.message);
    res.status(500).json({ error: error.message });
  }
});

/**
 * ==================== 日志与设置 ====================
 */

// 获取调用日志
router.get('/logs', (req, res) => {
  const limit = parseInt(req.query.limit) || 100;
  const logs = storage.getRecentLogs(limit);
  res.json({ logs });
});

// 获取日志详情
router.get('/logs/:id', (req, res) => {
  const log = storage.getLogDetail(req.params.id);
  if (log) res.json({ log });
  else res.status(404).json({ error: 'Log not found' });
});

// 清空日志
router.post('/logs/clear', (req, res) => {
  storage.clearLogs();
  res.json({ success: true });
});

/**
 * ==================== OpenAI 兼容接口 ====================
 */

// 列出模型
router.get('/v1/models', requireApiAuth, async (req, res) => {
  const startTime = Date.now();
  let statusCode = 200;

  try {
    // 获取前缀配置
    const userSettingsService = require('../../src/services/userSettings');
    const globalSettings = userSettingsService.loadUserSettings();
    const prefix = (globalSettings.channelModelPrefix || {})['antigravity'] || '';

    const models = service.getAvailableModels(prefix);

    storage.recordLog({
      accountId: null,
      path: '/v1/models',
      method: 'GET',
      statusCode: 200,
      durationMs: Date.now() - startTime,
      clientIp: req.ip || req.connection?.remoteAddress,
      userAgent: req.headers['user-agent'],
      detail: { modelCount: models.length },
    });

    res.json({ object: 'list', data: models });
  } catch (error) {
    statusCode = 500;
    storage.recordLog({
      accountId: null,
      path: '/v1/models',
      method: 'GET',
      statusCode,
      durationMs: Date.now() - startTime,
      clientIp: req.ip || req.connection?.remoteAddress,
      userAgent: req.headers['user-agent'],
      detail: { error: error.message },
    });
    res.status(500).json({ error: error.message });
  }
});

// 聊天补全
router.post('/v1/chat/completions', requireApiAuth, async (req, res) => {
  const startTime = Date.now();
  try {
    let { model, messages, stream } = req.body;

    // 防崩溃保护：校验 model
    if (!model || typeof model !== 'string') {
      return res
        .status(400)
        .json({
          error: { message: 'Invalid or missing model parameter', type: 'invalid_request_error' },
        });
    }

    // 获取前缀配置
    const userSettingsService = require('../../src/services/userSettings');
    const globalSettings = userSettingsService.loadUserSettings();
    const prefix = (globalSettings.channelModelPrefix || {})['antigravity'] || '';

    // 极致解析：安全剥离前缀
    let rawModel = model;
    if (prefix && rawModel.startsWith(prefix)) {
      rawModel = rawModel.substring(prefix.length);
    }
    if (rawModel.startsWith('[AG]')) {
      rawModel = rawModel.substring(4);
    }

    // 记录功能性前缀，用于验证时使用
    let funcPrefix = '';
    if (rawModel.startsWith('假流/')) {
      funcPrefix = '假流/';
      rawModel = rawModel.substring(3);
    } else if (rawModel.startsWith('流抗/')) {
      funcPrefix = '流抗/';
      rawModel = rawModel.substring(3);
    }
    model = rawModel;

    // 构建完整的模型 ID（带渠道前缀和功能性前缀）用于验证和日志
    // 注意：如果请求来自 v1.js，模型名可能已被剥离前缀，这里需要重新添加
    const requestedModel = prefix + funcPrefix + model;

    // 核心验证：在重定向之前验证模型是否在矩阵中被允许
    const availableModels = service.getAvailableModels(prefix);
    if (!availableModels.find(m => m.id === requestedModel)) {
      return res.status(404).json({
        error: {
          message: `Model '${requestedModel}' not found or disabled in matrix`,
          type: 'invalid_request_error',
          code: 'model_not_found',
        },
      });
    }

    // 预处理重定向
    const redirects = storage.getModelRedirects();
    const redirect = redirects.find(r => r.source_model === model);
    if (redirect) {
      model = redirect.target_model;
      req.body.model = model;
    }

    // 最终兜底验证：即使通过了列表检查，如果映射后的目标模型在矩阵里被彻底禁用（三个开关全关），直接拦截
    const finalMatrix = service.getMatrixConfig();
    const finalMConfig = finalMatrix[model];
    if (
      finalMConfig &&
      finalMConfig.base === false &&
      finalMConfig.fakeStream === false &&
      finalMConfig.antiTrunc === false
    ) {
      return res
        .status(403)
        .json({
          error: {
            message: `Target model '${model}' is disabled in function matrix.`,
            type: 'permission_error',
          },
        });
    }

    // 最终用于日志展示的模型 ID (带前缀)
    const modelWithPrefix = requestedModel;

    // 获取所有启用账号
    const allAccounts = storage.getAccounts().filter(a => a.enable);
    if (allAccounts.length === 0) {
      return res.status(503).json({ error: 'No enabled accounts available' });
    }

    const settings = globalSettings;
    const strategy = settings.load_balancing_strategy || 'random';
    const loadBalancer = require('../../src/utils/loadBalancer');

    // 智能重试逻辑：尝试所有可用账号
    const attemptedAccounts = new Set();
    let lastError = null;

    while (attemptedAccounts.size < allAccounts.length) {
      // 排除已尝试过的账号
      const availableAccounts = allAccounts.filter(a => !attemptedAccounts.has(a.id));
      if (availableAccounts.length === 0) break;

      const account = loadBalancer.getNextAccount('antigravity', availableAccounts, strategy);
      attemptedAccounts.add(account.id);

      try {
        if (stream) {
          // 仅在尚未发送 Header 时设置
          if (!res.headersSent) {
            res.setHeader('Content-Type', 'text/event-stream');
            res.setHeader('Cache-Control', 'no-cache');
            res.setHeader('Connection', 'keep-alive');
            res.setHeader('X-Accel-Buffering', 'no'); // 禁用代理缓存
          }

          const id = `chatcmpl-${uuidv4()}`;
          const created = Math.floor(Date.now() / 1000);
          let fullContent = '';
          let fullReasoning = '';

          await client.chatCompletionsStream(account.id, req.body, event => {
            let chunk = null;
            if (event.type === 'text') {
              fullContent += event.content;
              chunk = {
                id,
                object: 'chat.completion.chunk',
                created,
                model: modelWithPrefix,
                choices: [{ index: 0, delta: { content: event.content }, finish_reason: null }],
              };
            } else if (event.type === 'thinking') {
              fullReasoning += event.content;
              chunk = {
                id,
                object: 'chat.completion.chunk',
                created,
                model: modelWithPrefix,
                choices: [
                  { index: 0, delta: { reasoning_content: event.content }, finish_reason: null },
                ],
              };
            } else if (event.type === 'tool_calls') {
              chunk = {
                id,
                object: 'chat.completion.chunk',
                created,
                model: modelWithPrefix,
                choices: [
                  {
                    index: 0,
                    delta: {
                      tool_calls: event.tool_calls.map((tc, idx) => ({
                        index: idx,
                        id: tc.id,
                        type: 'function',
                        function: { name: tc.name, arguments: JSON.stringify(tc.args) },
                      })),
                    },
                    finish_reason: null,
                  },
                ],
              };
            }

            if (chunk) {
              res.write(`data: ${JSON.stringify(chunk)}\n\n`);
            }
          });

          res.write('data: [DONE]\n\n');
          res.end();

          // 记录成功日志 (包含累计的回复内容和思考过程)
          const originalMessages = JSON.parse(JSON.stringify(req.body.messages || []));

          // 确保合并系统指令到日志中
          if (!originalMessages.some(m => m.role === 'system')) {
            const systemInst = client.getConfig().SYSTEM_INSTRUCTION;
            if (systemInst) {
              originalMessages.unshift({ role: 'system', content: systemInst });
            }
          }

          storage.recordLog({
            accountId: account.id,
            model: modelWithPrefix,
            is_balanced: req.lb,
            path: req.path,
            method: 'POST',
            statusCode: 200,
            durationMs: Date.now() - startTime,
            clientIp: req.ip,
            userAgent: req.headers['user-agent'],
            detail: {
              model: req.body.model,
              type: 'stream',
              accountName: account.name,
              messages: originalMessages,
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
          // 非流式处理
          const result = await client.chatCompletions(account.id, req.body);
          // 确保返回结果中的 model 是带前缀的
          if (result && result.model) result.model = modelWithPrefix;

          const originalMessages = JSON.parse(JSON.stringify(req.body.messages || []));

          // 确保合并系统指令到日志中
          if (!originalMessages.some(m => m.role === 'system')) {
            const systemInst = client.getConfig().SYSTEM_INSTRUCTION;
            if (systemInst) {
              originalMessages.unshift({ role: 'system', content: systemInst });
            }
          }

          storage.recordLog({
            accountId: account.id,
            model: modelWithPrefix,
            is_balanced: req.lb,
            path: req.path,
            method: 'POST',
            statusCode: 200,
            durationMs: Date.now() - startTime,
            clientIp: req.ip,
            userAgent: req.headers['user-agent'],
            detail: {
              model: req.body.model,
              response: result,
              accountName: account.name,
              messages: originalMessages, // 添加对话消息
            },
          });

          return res.json(result); // 成功后退出
        }
      } catch (error) {
        console.warn(
          `[Antigravity] Account ${account.name} failed, trying next... Error: ${error.message}`
        );
        lastError = error;

        // 如果是 401 之外的错误（通常是 429 或 5xx），记录日志并继续循环
        const originalMessages = JSON.parse(JSON.stringify(req.body.messages || []));

        // 确保合并系统指令到日志中
        if (!originalMessages.some(m => m.role === 'system')) {
          const systemInst = client.getConfig().SYSTEM_INSTRUCTION;
          if (systemInst) {
            originalMessages.unshift({ role: 'system', content: systemInst });
          }
        }

        storage.recordLog({
          accountId: account.id,
          model: modelWithPrefix,
          is_balanced: req.lb,
          path: req.path,
          method: 'POST',
          statusCode: error.response?.status || 500,
          durationMs: Date.now() - startTime,
          clientIp: req.ip,
          userAgent: req.headers['user-agent'],
          detail: {
            error: error.message,
            model: req.body.model,
            messages: originalMessages,
          },
        });

        // 如果已经发送了 Header (流式过程中报错)，则无法切换账号，只能报错
        if (res.headersSent) {
          if (stream)
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
        message: `All accounts failed. Last error: ${lastError?.message}`,
        type: 'api_error',
      },
    });
  } catch (e) {
    console.error('Chat Completion General Error:', e.message);
    if (!res.headersSent) {
      res.status(500).json({ error: { message: e.message, type: 'api_error' } });
    }
  }
});

module.exports = router;
