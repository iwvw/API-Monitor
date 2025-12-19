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

        // --- 3. 处理 OpenAI 渠道 ---
        if (channelEnabled['openai']) {
            try {
                const oaiStorage = require(path.join(modulesDir, 'openai-api', 'storage.js'));
                const prefix = channelModelPrefix['openai'] || '';
                const endpoints = oaiStorage.getEndpoints().filter(ep => ep.status === 'valid');
                
                endpoints.forEach(ep => {
                    if (ep.models) {
                        ep.models.forEach(m => {
                            const id = prefix + m;
                            if (!allModelsMap.has(id)) {
                                allModelsMap.set(id, { id, object: 'model', created: now, owned_by: ep.name || 'openai' });
                            }
                        });
                    }
                });
            } catch (e) {
                console.warn('[v1/models] OpenAI process failed:', e.message);
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
const dispatch = (req, res, next) => {
    // 1. 还原路径 (因为挂载在 /v1 下，req.url 被截断了)
    // 如果 req.url 已经包含 /v1 (例如被手动修改过)，则不重复添加
    if (!req.url.startsWith('/v1')) {
        req.url = '/v1' + req.url;
    }

    // 2. 获取配置
    const settings = userSettingsService.loadUserSettings();
    const visibility = settings.moduleVisibility || {};
    const channelEnabled = settings.channelEnabled || {};
    const channelModelPrefix = settings.channelModelPrefix || {};

    // 3. 根据前缀判断目标渠道
    const agPrefix = channelModelPrefix['antigravity'] || '';
    const gcliPrefix = channelModelPrefix['gemini-cli'] || '';
    const oaiPrefix = channelModelPrefix['openai'] || '';

    // 如果是 POST 请求且有 body.model，尝试根据前缀路由
    if (req.method === 'POST' && req.body && req.body.model) {
        const model = req.body.model;

        // 检查是否匹配 Antigravity 前缀
        if (agPrefix && model.startsWith(agPrefix)) {
            req.body.model = model.substring(agPrefix.length);
            if (channelEnabled['antigravity'] && agRouter) {
                return agRouter(req, res, next);
            }
        }

        // 检查是否匹配 GCLI 前缀
        if (gcliPrefix && model.startsWith(gcliPrefix)) {
            req.body.model = model.substring(gcliPrefix.length);
            if (channelEnabled['gemini-cli'] && gcliRouter) {
                return gcliRouter(req, res, next);
            }
        }

        // 检查是否匹配 OpenAI 前缀
        if (oaiPrefix && model.startsWith(oaiPrefix)) {
            req.body.model = model.substring(oaiPrefix.length);
            const oaiRouterPath = path.join(modulesDir, 'openai-api', 'router.js');
            if (channelEnabled['openai'] && fs.existsSync(oaiRouterPath)) {
                const oaiRouter = require(oaiRouterPath);
                return oaiRouter(req, res, next);
            }
        }
    }

    // 4. 默认分发逻辑（无前缀时）
    // 优先 Antigravity
    if (channelEnabled['antigravity'] && agRouter) {
        // 尝试让 Antigravity 处理
        return agRouter(req, res, (err) => {
            if (err) return next(err);
            // 如果 Antigravity 没处理 (next)，尝试 GCLI
            if (channelEnabled['gemini-cli'] && gcliRouter) {
                return gcliRouter(req, res, (err2) => {
                    if (err2) return next(err2);
                    // 尝试 OpenAI
                    if (channelEnabled['openai']) {
                        const oaiRouterPath = path.join(modulesDir, 'openai-api', 'router.js');
                        if (fs.existsSync(oaiRouterPath)) {
                            const oaiRouter = require(oaiRouterPath);
                            return oaiRouter(req, res, next);
                        }
                    }
                    next();
                });
            }
            // 如果 GCLI 也关闭，直接尝试 OpenAI
            if (channelEnabled['openai']) {
                const oaiRouterPath = path.join(modulesDir, 'openai-api', 'router.js');
                if (fs.existsSync(oaiRouterPath)) {
                    const oaiRouter = require(oaiRouterPath);
                    return oaiRouter(req, res, next);
                }
            }
            next();
        });
    }

    // 如果 Antigravity 关闭，尝试 Gemini CLI
    if (channelEnabled['gemini-cli'] && gcliRouter) {
        return gcliRouter(req, res, (err) => {
            if (err) return next(err);
            if (channelEnabled['openai']) {
                const oaiRouterPath = path.join(modulesDir, 'openai-api', 'router.js');
                if (fs.existsSync(oaiRouterPath)) {
                    const oaiRouter = require(oaiRouterPath);
                    return oaiRouter(req, res, next);
                }
            }
            next();
        });
    }

    // 如果前面都关闭，尝试 OpenAI
    if (channelEnabled['openai']) {
        const oaiRouterPath = path.join(modulesDir, 'openai-api', 'router.js');
        if (fs.existsSync(oaiRouterPath)) {
            const oaiRouter = require(oaiRouterPath);
            return oaiRouter(req, res, next);
        }
    }
};

// 挂载所有请求到分发器
router.use(dispatch);

module.exports = router;

