/**
 * 统一的 OpenAI 兼容接口 (/v1)
 * 根据全局配置动态分发请求到 Gemini CLI 或 Qwen
 */

const express = require('express');
const router = express.Router();
const userSettingsService = require('../services/userSettings');
const path = require('path');
const fs = require('fs');
const { getSession, getSessionById } = require('../services/session');

// 动态加载模块路由和服务
const modulesDir = path.join(__dirname, '../../modules');
let gcliRouter = null;
let qwenRouter = null;
let qwenStorage = null;
let gcliStorage = null;
let gcliRouterLoaded = false;
let qwenRouterLoaded = false;
let qwenStorageLoaded = false;
let gcliStorageLoaded = false;

function loadOptionalModule(modulePath, label) {
  try {
    if (fs.existsSync(modulePath)) {
      return require(modulePath);
    }
  } catch (error) {
    console.error(`Failed to load ${label} for v1 aggregation:`, error.message);
  }
  return null;
}

function getGcliRouter() {
  if (!gcliRouterLoaded) {
    gcliRouterLoaded = true;
    gcliRouter = loadOptionalModule(path.join(modulesDir, 'gemini-cli-api', 'router.js'), 'Gemini CLI router');
  }
  return gcliRouter;
}

function getQwenRouter() {
  if (!qwenRouterLoaded) {
    qwenRouterLoaded = true;
    qwenRouter = loadOptionalModule(path.join(modulesDir, 'qwen-api', 'router.js'), 'Qwen router');
  }
  return qwenRouter;
}

function getQwenStorage() {
  if (!qwenStorageLoaded) {
    qwenStorageLoaded = true;
    qwenStorage = loadOptionalModule(path.join(modulesDir, 'qwen-api', 'storage.js'), 'Qwen storage');
  }
  return qwenStorage;
}

function getGcliStorage() {
  if (!gcliStorageLoaded) {
    gcliStorageLoaded = true;
    gcliStorage = loadOptionalModule(path.join(modulesDir, 'gemini-cli-api', 'storage.js'), 'Gemini CLI storage');
  }
  return gcliStorage;
}

// 辅助函数：获取 GCLI 矩阵模型列表
function getGcliModelIds() {
    try {
        const gcliMatrixPath = path.join(modulesDir, 'gemini-cli-api', 'gemini-matrix.json');
        if (fs.existsSync(gcliMatrixPath)) {
            const matrix = JSON.parse(fs.readFileSync(gcliMatrixPath, 'utf8'));
            return Object.keys(matrix);
        }
    } catch(e){}
    return [];
}

/**
 * API Key 认证中间件
 */
function requireApiAuth(req, res, next) {
  const session = getSession(req);
  if (session) return next();

  const authHeader = req.headers['authorization'];
  if (authHeader && authHeader.startsWith('Bearer ')) {
    const token = authHeader.substring(7);
    const sessionById = getSessionById(token);
    if (sessionById) return next();

    // 检查各渠道 API Key
    const currentQwenStorage = getQwenStorage();
    const currentGcliStorage = getGcliStorage();
    if (currentQwenStorage && token === currentQwenStorage.getSetting('API_KEY')) return next();
    try { if (currentGcliStorage && token === (currentGcliStorage.getSettings().API_KEY || '123456')) return next(); } catch(e) {}
  }

  const queryKey = req.query.key;
  if (queryKey) {
    const currentQwenStorage = getQwenStorage();
    const currentGcliStorage = getGcliStorage();
    if (currentQwenStorage && queryKey === currentQwenStorage.getSetting('API_KEY')) return next();
    try { if (currentGcliStorage && queryKey === (currentGcliStorage.getSettings().API_KEY || '123456')) return next(); } catch(e) {}
  }

  res.status(401).json({ error: { message: 'Invalid API Key or Session', type: 'invalid_request_error', code: 'invalid_api_key' } });
}

// 模型列表合并
router.get(['/models', '/model'], requireApiAuth, async (req, res) => {
  try {
    const settings = userSettingsService.loadUserSettings();
    const channelEnabled = settings.channelEnabled || {};
    const channelModelPrefix = settings.channelModelPrefix || {};
    const allModelsMap = new Map();
    const now = Math.floor(Date.now() / 1000);

    // 辅助函数：添加模型
    const addModels = (models, prefix, owner) => {
        models.forEach(m => {
            const id = prefix + (typeof m === 'string' ? m : m.id);
            allModelsMap.set(id, { id, object: 'model', created: now, owned_by: owner });
        });
    };

    // 3. Gemini CLI — reuse router.js getAvailableModels() for name consistency
    const currentGcliStorage = channelEnabled['gemini-cli'] ? getGcliStorage() : null;
    const currentQwenStorage = channelEnabled['qwen'] ? getQwenStorage() : null;

    if (channelEnabled['gemini-cli'] && currentGcliStorage) {
        try {
            const gcliRouterModule = getGcliRouter();
            const prefix = channelModelPrefix['gemini-cli'] || '';
            if (gcliRouterModule && typeof gcliRouterModule.getAvailableModels === 'function') {
                const gcliModels = gcliRouterModule.getAvailableModels(prefix);
                gcliModels.forEach(m => {
                    allModelsMap.set(m.id, { id: m.id, object: 'model', created: now, owned_by: 'google' });
                });
            } else {
                // Fallback: manual matrix parsing with correct naming
                const gcliMatrixPath = path.join(modulesDir, 'gemini-cli-api', 'gemini-matrix.json');
                if (fs.existsSync(gcliMatrixPath)) {
                    const gcliMatrix = JSON.parse(fs.readFileSync(gcliMatrixPath, 'utf8'));
                    Object.keys(gcliMatrix).forEach(id => {
                        if(gcliMatrix[id].base) addModels([id], prefix, 'google');
                        if(gcliMatrix[id].search) addModels([id + '-search'], prefix, 'google');
                        if(gcliMatrix[id].maxThinking) addModels([id + '-maxthinking'], prefix, 'google');
                        if(gcliMatrix[id].noThinking) addModels([id + '-nothinking'], prefix, 'google');
                        if(gcliMatrix[id].fakeStream) addModels(['假流/' + id], prefix, 'google');
                        if(gcliMatrix[id].antiTrunc) addModels(['流抗/' + id], prefix, 'google');
                    });
                }
            }
        } catch(e) {
            console.error('Failed to load GCLI models for v1 aggregation:', e.message);
        }
    }

    // 4. Qwen (向 qwen2API 靠拢的新能力)
    if (channelEnabled['qwen'] && currentQwenStorage) {
        const prefix = channelModelPrefix['qwen'] || '';
        const matrix = currentQwenStorage.getMatrix();
        Object.keys(matrix).forEach(id => {
            if(matrix[id].enabled) addModels([id], prefix, 'qwen');
        });
    }

    const data = Array.from(allModelsMap.values());
    res.json({ object: 'list', data });
  } catch (e) {
    res.status(500).json({ error: e.message });
  }
});

// 核心分发器 (Dispatch)
const dispatch = async (req, res, next) => {
  req.lb = true;
  if (!req.url.startsWith('/v1')) req.url = '/v1' + req.url;

  const settings = userSettingsService.loadUserSettings();
  const channelEnabled = settings.channelEnabled || {};
  const channelModelPrefix = settings.channelModelPrefix || {};
  const currentQwenRouter = channelEnabled['qwen'] ? getQwenRouter() : null;
  const currentGcliRouter = channelEnabled['gemini-cli'] ? getGcliRouter() : null;
  const currentQwenStorage = channelEnabled['qwen'] ? getQwenStorage() : null;

  if (req.method === 'POST' && req.body && req.body.model) {
    const fullId = req.body.model;
    
    // 优先级 1: 显式前缀匹配
    // 检查 Qwen 前缀
    const qwenPrefix = channelModelPrefix['qwen'] || '';
    if (qwenPrefix && fullId.startsWith(qwenPrefix)) {
        req.body.model = fullId.substring(qwenPrefix.length);
        if (currentQwenRouter) return currentQwenRouter(req, res, next);
    }

    // 检查 GCLI 前缀
    const gcliPrefix = channelModelPrefix['gemini-cli'] || '';
    if (gcliPrefix && fullId.startsWith(gcliPrefix)) {
        req.body.model = fullId.substring(gcliPrefix.length);
        if (currentGcliRouter) return currentGcliRouter(req, res, next);
    }
    // 优先级 2: 智能探测 (无前缀或前缀不匹配)
    // A. 探测 Qwen 归属
    if (currentQwenStorage && currentQwenRouter) {
        const matrix = currentQwenStorage.getMatrix();
        const baseId = fullId.toLowerCase();
        if (matrix[fullId] || baseId.startsWith('qwen')) {
            return currentQwenRouter(req, res, next);
        }
    }



    // C. 探测 Gemini CLI 归属
    if (currentGcliRouter) {
        const gcliModels = getGcliModelIds();
        // 仅当命中 GCLI 矩阵模型（支持前缀匹配以兼容变体）时分发
        if (gcliModels.some(m => fullId === m || fullId.startsWith(m + '-') || fullId.startsWith(m + '(') || fullId.includes('/' + m))) {
            return currentGcliRouter(req, res, next);
        }
    }

  }

  // 默认 Fallback
  if (currentQwenRouter) return currentQwenRouter(req, res, next);
  if (currentGcliRouter) return currentGcliRouter(req, res, next);

  next();
};

router.use(requireApiAuth, dispatch);

module.exports = router;
