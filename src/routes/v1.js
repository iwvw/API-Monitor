/**
 * 统一的 OpenAI 兼容接口 (/v1)
 * 根据全局配置动态分发请求到 Antigravity 或 Gemini CLI
 */

const express = require('express');
const router = express.Router();
const userSettingsService = require('../services/userSettings');
const path = require('path');
const fs = require('fs');
const { getSession, getSessionById } = require('../services/session');

// 动态加载模块路由和服务
const modulesDir = path.join(__dirname, '../../modules');
let agRouter = null;
let gcliRouter = null;
let gcliClient = null;
let gcliStorage = null;
let agService = null;
let agStorage = null;

try {
  const agPath = path.join(modulesDir, 'antigravity-api', 'router.js');
  if (fs.existsSync(agPath)) {
    agRouter = require(agPath);
  }
  const gcliPath = path.join(modulesDir, 'gemini-cli-api', 'router.js');
  if (fs.existsSync(gcliPath)) {
    gcliRouter = require(gcliPath);
  }
  // 加载 GCLI 客户端用于获取模型
  const gcliClientPath = path.join(modulesDir, 'gemini-cli-api', 'gemini-client.js');
  if (fs.existsSync(gcliClientPath)) {
    gcliClient = require(gcliClientPath);
  }
  const gcliStoragePath = path.join(modulesDir, 'gemini-cli-api', 'storage.js');
  if (fs.existsSync(gcliStoragePath)) {
    gcliStorage = require(gcliStoragePath);
  }
  // 加载 Antigravity 服务用于获取模型
  const agServicePath = path.join(modulesDir, 'antigravity-api', 'antigravity-service.js');
  if (fs.existsSync(agServicePath)) {
    agService = require(agServicePath);
  }
  // 加载 Antigravity storage 用于获取 API Key 设置
  const agStoragePath = path.join(modulesDir, 'antigravity-api', 'storage.js');
  if (fs.existsSync(agStoragePath)) {
    agStorage = require(agStoragePath);
  }
} catch (e) {
  console.error('Failed to load module routers for v1 aggregation:', e);
}

/**
 * API Key 认证中间件
 * 允许:
 * 1. 有效的 Admin Session
 * 2. Authorization Header "Bearer <API_KEY>"
 * 3. Query Param key=<API_KEY>
 */
function requireApiAuth(req, res, next) {
  // 1. 检查 Session
  const session = getSession(req);
  if (session) return next();

  // 2. 检查 Authorization Header (API Key)
  const authHeader = req.headers['authorization'];
  if (authHeader && authHeader.startsWith('Bearer ')) {
    const token = authHeader.substring(7);

    // 尝试作为 Session ID
    const sessionById = getSessionById(token);
    if (sessionById) return next();

    // 尝试作为 API Key (动态从存储中获取最新值)
    let agApiKey = null;
    try {
      const agStorage = require(path.join(modulesDir, 'antigravity-api', 'storage.js'));
      agApiKey = agStorage.getSetting('API_KEY');
    } catch (e) { }

    let gcliApiKey = null;
    try {
      const gcliStorage = require(path.join(modulesDir, 'gemini-cli-api', 'storage.js'));
      const gcliSettings = gcliStorage.getSettings();
      gcliApiKey = gcliSettings.API_KEY || '123456';
    } catch (e) { }

    if ((agApiKey && token === agApiKey) || (gcliApiKey && token === gcliApiKey)) {
      return next();
    }
  }

  // 3. 检查 Query Param (compat)
  const queryKey = req.query.key;
  if (queryKey) {
    let agApiKey = null;
    try {
      const agStorage = require(path.join(modulesDir, 'antigravity-api', 'storage.js'));
      agApiKey = agStorage.getSetting('API_KEY');
    } catch (e) { }

    let gcliApiKey = null;
    try {
      const gcliStorage = require(path.join(modulesDir, 'gemini-cli-api', 'storage.js'));
      const gcliSettings = gcliStorage.getSettings();
      gcliApiKey = gcliSettings.API_KEY || '123456';
    } catch (e) { }

    if ((agApiKey && queryKey === agApiKey) || (gcliApiKey && queryKey === gcliApiKey)) {
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

// 合并模型列表的智能处理
router.get('/models', requireApiAuth, async (req, res) => {
  try {
    const settings = userSettingsService.loadUserSettings();
    const channelEnabled = settings.channelEnabled || {};
    const channelModelPrefix = settings.channelModelPrefix || {};

    const allModelsMap = new Map(); // 使用 Map 进行全局去重 (ID 为 Key)

    // --- 1. 处理 Antigravity 渠道 ---
    if (channelEnabled['antigravity']) {
      try {
        const agService = require(
          path.join(modulesDir, 'antigravity-api', 'antigravity-service.js')
        );
        const prefix = channelModelPrefix['antigravity'] || '';
        const agModels = agService.getAvailableModels(prefix);
        agModels.forEach(m => allModelsMap.set(m.id, m));
      } catch (e) {
        console.warn('[v1/models] Antigravity process failed:', e.message);
      }
    }

    // --- 2. 处理 Gemini CLI 渠道 ---
    if (channelEnabled['gemini-cli']) {
      try {
        // 同样引用 GCLI 的逻辑 (如果 GCLI 也有 service 更好，否则复刻精简版)
        const gcliPrefix = channelModelPrefix['gemini-cli'] || '';
        const matrixPath = path.join(modulesDir, 'gemini-cli-api', 'gemini-matrix.json');
        const gcliStorage = require(path.join(modulesDir, 'gemini-cli-api', 'storage.js'));

        if (fs.existsSync(matrixPath)) {
          const matrix = JSON.parse(fs.readFileSync(matrixPath, 'utf8'));
          const disabledModels = gcliStorage.getDisabledModels();
          const redirects = gcliStorage.getModelRedirects();
          const redirectTargets = new Set(redirects.map(r => r.target_model));
          const now = Math.floor(Date.now() / 1000);

          Object.keys(matrix).forEach(baseId => {
            const config = matrix[baseId];
            const variants = [];
            if (config.base) {
              variants.push(baseId);
              if (config.search) variants.push(baseId + '-search');
            }
            if (config.maxThinking) {
              variants.push(baseId + '-maxthinking');
              if (config.search) variants.push(baseId + '-maxthinking-search');
            }
            if (config.noThinking) {
              variants.push(baseId + '-nothinking');
              if (config.search) variants.push(baseId + '-nothinking-search');
            }

            if (variants.length > 0) {
              variants.forEach(v => {
                const possibleIds = [v];
                if (config.fakeStream) possibleIds.push('假流/' + v);
                if (config.antiTrunc) possibleIds.push('流抗/' + v);

                possibleIds.forEach(id => {
                  const fullId = gcliPrefix + id;
                  if (!disabledModels.includes(fullId) && !redirectTargets.has(id)) {
                    if (!allModelsMap.has(fullId)) {
                      allModelsMap.set(fullId, {
                        id: fullId,
                        object: 'model',
                        created: now,
                        owned_by: 'google',
                      });
                    }
                  }
                });
              });
            } else {
              // base/maxThinking/noThinking 都为 false 时，直接生成功能性变体
              if (config.fakeStream) {
                const fullId = gcliPrefix + '假流/' + baseId;
                if (!disabledModels.includes(fullId) && !allModelsMap.has(fullId)) {
                  allModelsMap.set(fullId, {
                    id: fullId,
                    object: 'model',
                    created: now,
                    owned_by: 'google',
                  });
                }
              }
              if (config.antiTrunc) {
                const fullId = gcliPrefix + '流抗/' + baseId;
                if (!disabledModels.includes(fullId) && !allModelsMap.has(fullId)) {
                  allModelsMap.set(fullId, {
                    id: fullId,
                    object: 'model',
                    created: now,
                    owned_by: 'google',
                  });
                }
              }
            }
          });

          // GCLI Redirects
          redirects.forEach(r => {
            const fullId = gcliPrefix + r.source_model;
            if (!allModelsMap.has(fullId)) {
              allModelsMap.set(fullId, {
                id: fullId,
                object: 'model',
                created: now,
                owned_by: 'system-redirect',
              });
            }
          });
        }
      } catch (e) {
        console.warn('[v1/models] Gemini CLI process failed:', e.message);
      }
    }

    const data = Array.from(allModelsMap.values());
    if (data.length === 0) {
      return res
        .status(404)
        .json({ error: { message: 'No enabled AI models found', type: 'invalid_request_error' } });
    }

    res.json({ object: 'list', data });
  } catch (e) {
    res.status(500).json({ error: e.message });
  }
});

// 辅助函数：根据配置转发请求
const dispatch = async (req, res, next) => {
  // 标记为经过 V1 分发器 (负载均衡)
  req.lb = true;

  // 1. 还原路径
  // 如果 req.url 不以 /v1 开头，添加它
  if (!req.url.startsWith('/v1')) {
    req.url = '/v1' + req.url;
  }

  // 2. 获取配置
  const settings = userSettingsService.loadUserSettings();
  const channelEnabled = settings.channelEnabled || {};
  const channelModelPrefix = settings.channelModelPrefix || {};

  const agEnabled = channelEnabled['antigravity'] && agRouter;
  const gcliEnabled = channelEnabled['gemini-cli'] && gcliRouter;

  // 3. 模型路由逻辑 (仅针对包含 model 的 POST 请求)
  if (req.method === 'POST' && req.body && req.body.model) {
    const fullModelId = req.body.model;
    const agPrefix = channelModelPrefix['antigravity'] || '';
    const gcliPrefix = channelModelPrefix['gemini-cli'] || '';

    // --- A. 精确匹配前缀优先 ---

    // 尝试匹配 GCLI 前缀 (如果前缀非空且匹配)
    if (gcliPrefix && fullModelId.startsWith(gcliPrefix)) {
      const innerModel = fullModelId.substring(gcliPrefix.length);
      if (gcliEnabled) {
        req.body.model = innerModel;
        return gcliRouter(req, res, next);
      }
    }

    // 尝试匹配 Antigravity 前缀 (如果前缀非空且匹配)
    if (agPrefix && fullModelId.startsWith(agPrefix)) {
      const innerModel = fullModelId.substring(agPrefix.length);
      if (agEnabled) {
        req.body.model = innerModel;
        return agRouter(req, res, next);
      }
    }

    // --- B. 无前缀匹配或前缀为空时的探测逻辑 ---

    // 如果两个都开启，需要判断模型归属
    if (agEnabled && gcliEnabled) {
      try {
        // 1. 加载 GCLI 矩阵和获取实时可用列表
        const matrixPath = path.join(modulesDir, 'gemini-cli-api', 'gemini-matrix.json');
        const gcliRouterPath = path.join(modulesDir, 'gemini-cli-api', 'router.js');

        let isGcliModel = false;

        // 优先检查全名匹配 (剥离空前缀后)
        const checkModelId = gcliPrefix
          ? fullModelId.startsWith(gcliPrefix)
            ? fullModelId.substring(gcliPrefix.length)
            : null
          : fullModelId;

        if (checkModelId) {
          // a. 检查矩阵中的基础模型定义
          if (fs.existsSync(matrixPath)) {
            const matrix = JSON.parse(fs.readFileSync(matrixPath, 'utf8'));
            // 尝试直接匹配键名 (例如 gemini-2.0-pro)
            if (matrix[checkModelId]) {
              isGcliModel = true;
            } else {
              // 尝试模糊匹配：模型 ID 是否以矩阵中的某个键开头
              // 这处理了 gemini-2.0-pro-search 等变体
              const baseId = Object.keys(matrix).find(key => checkModelId.includes(key));
              if (baseId) isGcliModel = true;
            }
          }

          // b. 辅助判断：如果是 google/gemini 相关的路径且没匹配上前置条件
          if (
            !isGcliModel &&
            (checkModelId.toLowerCase().includes('gemini') ||
              checkModelId.toLowerCase().includes('google'))
          ) {
            // 启发式：含有 gemini 关键字且不是显式的 Antigravity 模型时，倾向于给 GCLI (如果是它特有的格式)
            // 但这里我们保持严谨，如果不确定，后面还有 fallback
          }
        }

        if (isGcliModel) {
          if (gcliPrefix && fullModelId.startsWith(gcliPrefix)) {
            req.body.model = fullModelId.substring(gcliPrefix.length);
          }
          return gcliRouter(req, res, next);
        }
      } catch (e) {
        console.error('[Dispatch] Precise GCLI model check failed:', e.message);
      }

      // 默认走 Antigravity (因为它支持的模型更多/更灵活)
      if (agPrefix && fullModelId.startsWith(agPrefix)) {
        req.body.model = fullModelId.substring(agPrefix.length);
      }
      return agRouter(req, res, next);
    }
  }

  // 4. 非模型请求或降级路由
  if (agEnabled) return agRouter(req, res, next);
  if (gcliEnabled) return gcliRouter(req, res, next);

  next();
};

// 挂载所有请求到分发器（需要 API Key 认证）
router.use(requireApiAuth, dispatch);

module.exports = router;
