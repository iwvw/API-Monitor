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

// 合并模型列表的特殊处理
router.get('/models', async (req, res) => {
    try {
        const settings = userSettingsService.loadUserSettings();
        const channelEnabled = settings.channelEnabled || {};
        const channelModelPrefix = settings.channelModelPrefix || {};

        const allModels = [];
        const modelIds = new Set();
        const now = Math.floor(Date.now() / 1000);

        // 获取各渠道前缀
        const agPrefix = channelModelPrefix['antigravity'] || '';
        const gcliPrefix = channelModelPrefix['gemini-cli'] || '';

        // 从 Antigravity 获取模型
        if (channelEnabled['antigravity']) {
            try {
                const agStoragePath = path.join(modulesDir, 'antigravity-api', 'storage.js');
                const agClientPath = path.join(modulesDir, 'antigravity-api', 'antigravity-client.js');

                if (fs.existsSync(agStoragePath) && fs.existsSync(agClientPath)) {
                    const agStorage = require(agStoragePath);
                    const agClient = require(agClientPath);

                    const accounts = agStorage.getAccounts().filter(a => a.enable);
                    if (accounts.length > 0) {
                        const agData = await agClient.listModels(accounts[0].id);
                        if (agData && agData.data) {
                            agData.data.forEach(m => {
                                const prefixedId = agPrefix + m.id;
                                if (!modelIds.has(prefixedId)) {
                                    modelIds.add(prefixedId);
                                    allModels.push({ id: prefixedId, object: 'model', created: now, owned_by: 'antigravity' });
                                }
                            });
                        }
                    }
                }
            } catch (e) {
                console.warn('[v1/models] Failed to get Antigravity models:', e.message);
            }
        }

        // 从 GCLI 获取模型（动态列表 + 思考变体）
        // 从 GCLI 获取模型（基于配置矩阵 gemini-matrix.json）
        if (channelEnabled['gemini-cli']) {
            try {
                const matrixPath = path.join(modulesDir, 'gemini-cli-api', 'gemini-matrix.json');
                console.log('[v1/models] Debug: Checking matrix path:', matrixPath);
                if (fs.existsSync(matrixPath)) {
                    const matrixContent = fs.readFileSync(matrixPath, 'utf8');
                    const matrix = JSON.parse(matrixContent);
                    console.log('[v1/models] Debug: Matrix keys:', Object.keys(matrix));

                    Object.keys(matrix).forEach(baseModelId => {
                        const config = matrix[baseModelId];
                        if (!config) return;

                        // 1. 收集当前模型的基础可用变体 ID
                        const standardVariants = [];

                        // A. 基础模型
                        if (config.base) {
                            standardVariants.push(baseModelId);
                            if (config.search) standardVariants.push(baseModelId + '-search');
                        }

                        // B. 深度思考
                        if (config.maxThinking) {
                            standardVariants.push(baseModelId + '-maxthinking');
                            if (config.search) standardVariants.push(baseModelId + '-maxthinking-search');
                        }

                        // C. 快速思考
                        if (config.noThinking) {
                            standardVariants.push(baseModelId + '-nothinking');
                            if (config.search) standardVariants.push(baseModelId + '-nothinking-search');
                        }

                        // 2. 为每个变体生成最终的模型对象
                        standardVariants.forEach(variantId => {
                            const coreId = gcliPrefix + variantId;

                            // 2.1 标准模型
                            if (!modelIds.has(coreId)) {
                                modelIds.add(coreId);
                                allModels.push({ id: coreId, object: 'model', created: now, owned_by: 'google' });
                            }

                            // 2.2 假流式
                            if (config.fakeStream) {
                                const fakeId = gcliPrefix + '假流式/' + variantId;
                                if (!modelIds.has(fakeId)) {
                                    modelIds.add(fakeId);
                                    allModels.push({ id: fakeId, object: 'model', created: now, owned_by: 'google' });
                                }
                            }

                            // 2.3 抗截断
                            if (config.antiTrunc) {
                                const antiId = gcliPrefix + '流式抗截断/' + variantId;
                                if (!modelIds.has(antiId)) {
                                    modelIds.add(antiId);
                                    allModels.push({ id: antiId, object: 'model', created: now, owned_by: 'google' });
                                }
                            }
                        });
                    });
                    console.log('[v1/models] Debug: GCLI models generated count:', allModels.length);
                } else {
                    console.warn('[v1/models] gemini-matrix.json not found at:', matrixPath);
                }
            } catch (e) {
                console.warn('[v1/models] Failed to load GCLI matrix:', e.message);
            }
        }

        if (allModels.length === 0) {
            return res.status(404).json({ error: { message: 'No enabled AI module found', type: 'invalid_request_error' } });
        }

        res.json({ object: 'list', data: allModels });
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
    }

    // 4. 默认分发逻辑（无前缀时）
    // 优先 Antigravity
    if (channelEnabled['antigravity'] && agRouter) {
        // 尝试让 Antigravity 处理
        return agRouter(req, res, (err) => {
            if (err) return next(err);
            // 如果 Antigravity 没处理 (next)，尝试 GCLI
            if (channelEnabled['gemini-cli'] && gcliRouter) {
                return gcliRouter(req, res, next);
            }
            next();
        });
    }

    // 如果 Antigravity 关闭，尝试 Gemini CLI
    if (channelEnabled['gemini-cli'] && gcliRouter) {
        return gcliRouter(req, res, next);
    }

    // 都没处理
    if (!res.headersSent) {
        res.status(404).json({ error: { message: 'No enabled AI module found for this endpoint', type: 'invalid_request_error' } });
    }
};

// 挂载所有请求到分发器
router.use(dispatch);

module.exports = router;

