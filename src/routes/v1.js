/**
 * 统一的 OpenAI 兼容接口 (/v1)
 * 根据全局配置动态分发请求到 Antigravity 或 Gemini CLI
 */

const express = require('express');
const router = express.Router();
const userSettingsService = require('../services/userSettings');
const path = require('path');
const fs = require('fs');

// 动态加载模块路由和服务
const modulesDir = path.join(__dirname, '../../modules');
let agRouter = null;
let gcliRouter = null;
let gcliClient = null;
let gcliStorage = null;
let agService = null;

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
} catch (e) {
    console.error('Failed to load module routers for v1 aggregation:', e);
}

// 合并模型列表的智能处理
router.get('/models', async (req, res) => {
    try {
        const settings = userSettingsService.loadUserSettings();
        const channelEnabled = settings.channelEnabled || {};
        const channelModelPrefix = settings.channelModelPrefix || {};

        const allModelsMap = new Map(); // 使用 Map 进行全局去重 (ID 为 Key)
        const now = Math.floor(Date.now() / 1000);

        // --- 1. 处理 Antigravity 渠道 ---
        if (channelEnabled['antigravity']) {
            try {
                const agStorage = require(path.join(modulesDir, 'antigravity-api', 'storage.js'));
                const agClient = require(path.join(modulesDir, 'antigravity-api', 'antigravity-client.js'));
                const prefix = channelModelPrefix['antigravity'] || '';
                
                const accounts = agStorage.getAccounts().filter(a => a.enable);
                if (accounts.length > 0) {
                    const agData = await agClient.listModels(accounts[0].id);
                    if (agData && agData.data) {
                        const modelConfigs = agStorage.getModelConfigs();
                        const redirects = agStorage.getModelRedirects();
                        
                        // 记录哪些模型是被重定向的目标，后续需要隐藏
                        const redirectTargets = new Set(redirects.map(r => r.target_model));
                        
                        // 处理原始模型
                        agData.data.forEach(m => {
                            const isEnabled = modelConfigs[m.id] !== undefined ? modelConfigs[m.id] : true;
                            // 仅当模型启用，且不是重定向目标时才添加
                            if (isEnabled && !redirectTargets.has(m.id)) {
                                const id = prefix + m.id;
                                if (!allModelsMap.has(id)) {
                                    allModelsMap.set(id, { id, object: 'model', created: now, owned_by: 'antigravity' });
                                }
                            }
                        });

                        // 处理重定向模型 (Alias)
                        redirects.forEach(r => {
                            const id = prefix + r.source_model;
                            if (!allModelsMap.has(id)) {
                                allModelsMap.set(id, { id, object: 'model', created: now, owned_by: 'system-redirect' });
                            }
                        });
                    }
                }
            } catch (e) {
                console.warn('[v1/models] Antigravity process failed:', e.message);
            }
        }

        // --- 2. 处理 Gemini CLI 渠道 ---
        if (channelEnabled['gemini-cli']) {
            try {
                const gcliStorage = require(path.join(modulesDir, 'gemini-cli-api', 'storage.js'));
                const prefix = channelModelPrefix['gemini-cli'] || '';
                const matrixPath = path.join(modulesDir, 'gemini-cli-api', 'gemini-matrix.json');
                
                if (fs.existsSync(matrixPath)) {
                    const matrix = JSON.parse(fs.readFileSync(matrixPath, 'utf8'));
                    const disabledModels = gcliStorage.getDisabledModels();
                    const redirects = gcliStorage.getModelRedirects();
                    const redirectTargets = new Set(redirects.map(r => r.target_model));

                    Object.keys(matrix).forEach(baseModelId => {
                        const config = matrix[baseModelId];
                        if (!config) return;

                        // 生成变体
                        const variants = [];
                        if (config.base) {
                            variants.push(baseModelId);
                            if (config.search) variants.push(baseModelId + '-search');
                        }
                        if (config.maxThinking) {
                            variants.push(baseModelId + '-maxthinking');
                            if (config.search) variants.push(baseModelId + '-maxthinking-search');
                        }
                        if (config.noThinking) {
                            variants.push(baseModelId + '-nothinking');
                            if (config.search) variants.push(baseModelId + '-nothinking-search');
                        }

                        variants.forEach(v => {
                            const ids = [v];
                            if (config.fakeStream) ids.push('假流式/' + v);
                            if (config.antiTrunc) ids.push('流式抗截断/' + v);

                            ids.forEach(rawId => {
                                const fullId = prefix + rawId;
                                // 过滤禁用和重定向目标
                                if (!disabledModels.includes(fullId) && !redirectTargets.has(rawId)) {
                                    if (!allModelsMap.has(fullId)) {
                                        allModelsMap.set(fullId, { id: fullId, object: 'model', created: now, owned_by: 'google' });
                                    }
                                }
                            });
                        });
                    });

                    // 处理 GCLI 重定向
                    redirects.forEach(r => {
                        const id = prefix + r.source_model;
                        if (!allModelsMap.has(id)) {
                            allModelsMap.set(id, { id, object: 'model', created: now, owned_by: 'system-redirect' });
                        }
                    });
                }
            } catch (e) {
                console.warn('[v1/models] Gemini CLI process failed:', e.message);
            }
        }

        const data = Array.from(allModelsMap.values());
        if (data.length === 0) {
            return res.status(404).json({ error: { message: 'No enabled AI models found', type: 'invalid_request_error' } });
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
                const checkModelId = gcliPrefix ? (fullModelId.startsWith(gcliPrefix) ? fullModelId.substring(gcliPrefix.length) : null) : fullModelId;
                
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
                    if (!isGcliModel && (checkModelId.toLowerCase().includes('gemini') || checkModelId.toLowerCase().includes('google'))) {
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

// 挂载所有请求到分发器
router.use(dispatch);

module.exports = router;

