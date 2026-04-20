/**
 * 统一的 OpenAI 兼容接口 (/v1)
 * 根据全局配置动态分发请求到 Antigravity, Gemini CLI, DeepSeek 或 Qwen
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
const agService = null;
let agStorage = null;
let dsRouter = null;
let dsStorage = null;
let qwenRouter = null;
let qwenStorage = null;
let gcliStorage = null;

try {
  const agPath = path.join(modulesDir, 'antigravity-api', 'router.js');
  if (fs.existsSync(agPath)) agRouter = require(agPath);
  
  const gcliPath = path.join(modulesDir, 'gemini-cli-api', 'router.js');
  if (fs.existsSync(gcliPath)) gcliRouter = require(gcliPath);
  
  const dsPath = path.join(modulesDir, 'deepseek-api', 'router.js');
  if (fs.existsSync(dsPath)) dsRouter = require(dsPath);
  
  const qwenPath = path.join(modulesDir, 'qwen-api', 'router.js');
  if (fs.existsSync(qwenPath)) qwenRouter = require(qwenPath);

  // 加载存储层用于鉴权
  const agStoragePath = path.join(modulesDir, 'antigravity-api', 'storage.js');
  if (fs.existsSync(agStoragePath)) agStorage = require(agStoragePath);

  const dsStoragePath = path.join(modulesDir, 'deepseek-api', 'storage.js');
  if (fs.existsSync(dsStoragePath)) dsStorage = require(dsStoragePath);

  const qwenStoragePath = path.join(modulesDir, 'qwen-api', 'storage.js');
  if (fs.existsSync(qwenStoragePath)) qwenStorage = require(qwenStoragePath);

  const gcliStoragePath = path.join(modulesDir, 'gemini-cli-api', 'storage.js');
  if (fs.existsSync(gcliStoragePath)) gcliStorage = require(gcliStoragePath);

} catch (e) {
  console.error('Failed to load module routers for v1 aggregation:', e);
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

// 辅助函数：获取深度搜索可用别名
const DS_ALIASES = ['gpt-', 'o1', 'o3', 'claude-', 'llama-', 'deepseek-'];

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
    if (agStorage && token === agStorage.getSetting('API_KEY')) return next();
    if (dsStorage && token === dsStorage.getSetting('API_KEY')) return next();
    if (qwenStorage && token === qwenStorage.getSetting('API_KEY')) return next();
    try { if (gcliStorage && token === (gcliStorage.getSettings().API_KEY || '123456')) return next(); } catch(e) {}
  }

  const queryKey = req.query.key;
  if (queryKey) {
    if (agStorage && queryKey === agStorage.getSetting('API_KEY')) return next();
    if (dsStorage && queryKey === dsStorage.getSetting('API_KEY')) return next();
    if (qwenStorage && queryKey === qwenStorage.getSetting('API_KEY')) return next();
    try { if (gcliStorage && queryKey === (gcliStorage.getSettings().API_KEY || '123456')) return next(); } catch(e) {}
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

    // 1. Antigravity
    if (channelEnabled['antigravity'] && agStorage) {
        try {
            const agService = require(path.join(modulesDir, 'antigravity-api', 'antigravity-service.js'));
            addModels(agService.getAvailableModels(''), channelModelPrefix['antigravity'] || '', 'google');
        } catch(e){}
    }

    // 2. DeepSeek
    if (channelEnabled['deepseek'] && dsStorage) {
        const dsMatrixPath = path.join(modulesDir, 'deepseek-api', 'deepseek-models.json');
        if (fs.existsSync(dsMatrixPath)) {
            const dsMatrix = JSON.parse(fs.readFileSync(dsMatrixPath, 'utf8'));
            const prefix = channelModelPrefix['deepseek'] || '';
            Object.keys(dsMatrix).forEach(id => {
                if(dsMatrix[id].base) addModels([id], prefix, 'deepseek');
                if(dsMatrix[id].search) addModels([id + '-search'], prefix, 'deepseek');
            });
        }
    }

    // 3. Gemini CLI — reuse router.js getAvailableModels() for name consistency
    if (channelEnabled['gemini-cli'] && gcliStorage) {
        try {
            const gcliRouterModule = require(path.join(modulesDir, 'gemini-cli-api', 'router.js'));
            const prefix = channelModelPrefix['gemini-cli'] || '';
            if (typeof gcliRouterModule.getAvailableModels === 'function') {
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
    if (channelEnabled['qwen'] && qwenStorage) {
        const prefix = channelModelPrefix['qwen'] || '';
        const matrix = qwenStorage.getMatrix();
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

  if (req.method === 'POST' && req.body && req.body.model) {
    const fullId = req.body.model;
    
    // 优先级 1: 显式前缀匹配
    // 检查 Qwen 前缀
    const qwenPrefix = channelModelPrefix['qwen'] || '';
    if (qwenPrefix && fullId.startsWith(qwenPrefix)) {
        req.body.model = fullId.substring(qwenPrefix.length);
        if (channelEnabled['qwen'] && qwenRouter) return qwenRouter(req, res, next);
    }
    // 检查 DeepSeek 前缀
    const dsPrefix = channelModelPrefix['deepseek'] || '';
    if (dsPrefix && fullId.startsWith(dsPrefix)) {
        req.body.model = fullId.substring(dsPrefix.length);
        if (channelEnabled['deepseek'] && dsRouter) return dsRouter(req, res, next);
    }
    // 检查 GCLI 前缀
    const gcliPrefix = channelModelPrefix['gemini-cli'] || '';
    if (gcliPrefix && fullId.startsWith(gcliPrefix)) {
        req.body.model = fullId.substring(gcliPrefix.length);
        if (channelEnabled['gemini-cli'] && gcliRouter) return gcliRouter(req, res, next);
    }
    // 检查 Antigravity 前缀
    const agPrefix = channelModelPrefix['antigravity'] || '';
    if (agPrefix && fullId.startsWith(agPrefix)) {
        req.body.model = fullId.substring(agPrefix.length);
        if (channelEnabled['antigravity'] && agRouter) return agRouter(req, res, next);
    }

    // 优先级 2: 智能探测 (无前缀或前缀不匹配)
    // A. 探测 Qwen 归属
    if (channelEnabled['qwen'] && qwenStorage && qwenRouter) {
        const matrix = qwenStorage.getMatrix();
        const baseId = fullId.toLowerCase();
        if (matrix[fullId] || baseId.startsWith('qwen')) {
            return qwenRouter(req, res, next);
        }
    }

    // B. 探测 DeepSeek 归属
    if (channelEnabled['deepseek'] && dsRouter) {
        if (fullId.startsWith('deepseek-') || DS_ALIASES.some(p => fullId.startsWith(p))) {
            return dsRouter(req, res, next);
        }
    }

    // C. 探测 Gemini CLI 归属
    if (channelEnabled['gemini-cli'] && gcliRouter) {
        const gcliModels = getGcliModelIds();
        // 仅当命中 GCLI 矩阵模型（支持前缀匹配以兼容变体）时分发
        if (gcliModels.some(m => fullId === m || fullId.startsWith(m + '-') || fullId.startsWith(m + '(') || fullId.includes('/' + m))) {
            return gcliRouter(req, res, next);
        }
    }

    // D. 探测 Antigravity 归属
    if (channelEnabled['antigravity'] && agRouter) {
        // 如果是 google 系模型但没被 GCLI 命中，交给 Antigravity
        if (fullId.includes('gemini-') || fullId.includes('google')) {
            return agRouter(req, res, next);
        }
    }
  }

  // 默认 Fallback
  if (channelEnabled['qwen'] && qwenRouter) return qwenRouter(req, res, next);
  if (channelEnabled['deepseek'] && dsRouter) return dsRouter(req, res, next);
  if (channelEnabled['gemini-cli'] && gcliRouter) return gcliRouter(req, res, next);
  if (channelEnabled['antigravity'] && agRouter) return agRouter(req, res, next);

  next();
};

router.use(requireApiAuth, dispatch);

module.exports = router;
