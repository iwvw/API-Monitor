/**
 * OpenAI API 管理 - API 路由
 */

const express = require('express');
const router = express.Router();
const storage = require('./storage');
const openaiApi = require('./openai-api');
const { proxyLimiter } = require('../../src/middleware/rateLimit');
const { validate, chatCompletionSchema } = require('../../src/middleware/validation');
const { createLogger } = require('../../src/utils/logger');
const logger = createLogger('OpenAI');

// ==================== 端点管理 ====================

/**
 * 获取所有端点（不隐藏 API Key，方便复制）
 */
router.get('/endpoints', (req, res) => {
  try {
    const endpoints = storage.getEndpoints();
    res.json(endpoints);
  } catch (e) {
    res.status(500).json({ error: e.message });
  }
});

/**
 * 添加端点
 */
router.post('/endpoints', async (req, res) => {
  try {
    const { name, baseUrl, apiKey, notes, skipVerify } = req.body;

    if (!name || !baseUrl || !apiKey) {
      return res.status(400).json({ error: '名称、API 地址和 API Key 必填' });
    }

    const endpoint = storage.addEndpoint({ name, baseUrl, apiKey, notes });
    let verification = null;

    // 验证 API Key（除非明确跳过验证，用于数据导入）
    if (!skipVerify) {
      verification = await openaiApi.verifyApiKey(baseUrl, apiKey);

      // 如果验证成功，获取模型列表
      if (verification.valid) {
        const modelsResult = await openaiApi.listModels(baseUrl, apiKey);
        storage.updateEndpoint(endpoint.id, {
          status: 'valid',
          models: modelsResult.models || [],
          lastChecked: new Date().toISOString(),
        });
        endpoint.status = 'valid';
        endpoint.models = modelsResult.models || [];
      } else {
        storage.updateEndpoint(endpoint.id, {
          status: 'invalid',
          lastChecked: new Date().toISOString(),
        });
        endpoint.status = 'invalid';
      }
    } else {
      // 跳过验证时，保持原有状态
      endpoint.status = 'unknown';
    }

    res.json({
      success: true,
      endpoint,
      verification,
    });
  } catch (e) {
    res.status(500).json({ error: e.message });
  }
});

/**
 * 更新端点
 */
router.put('/endpoints/:id', async (req, res) => {
  try {
    const { id } = req.params;
    const { name, baseUrl, apiKey, notes } = req.body;

    const endpoint = storage.getEndpointById(id);
    if (!endpoint) {
      return res.status(404).json({ error: '端点不存在' });
    }

    // 如果更新了 apiKey 或 baseUrl，重新验证
    if (apiKey || baseUrl) {
      const testUrl = baseUrl || endpoint.baseUrl;
      const testKey = apiKey || endpoint.apiKey;
      const verification = await openaiApi.verifyApiKey(testUrl, testKey);

      if (verification.valid) {
        const modelsResult = await openaiApi.listModels(testUrl, testKey);
        storage.updateEndpoint(id, {
          name,
          baseUrl,
          apiKey,
          notes,
          status: 'valid',
          models: modelsResult.models || [],
          lastChecked: new Date().toISOString(),
        });
      } else {
        storage.updateEndpoint(id, {
          name,
          baseUrl,
          apiKey,
          notes,
          status: 'invalid',
          lastChecked: new Date().toISOString(),
        });
      }
    } else {
      storage.updateEndpoint(id, { name, notes });
    }

    res.json({ success: true });
  } catch (e) {
    res.status(500).json({ error: e.message });
  }
});

/**
 * 切换端点启用状态
 */
router.post('/endpoints/:id/toggle', (req, res) => {
  try {
    const { id } = req.params;
    const { enabled } = req.body;

    const endpoint = storage.getEndpointById(id);
    if (!endpoint) {
      return res.status(404).json({ error: '端点不存在' });
    }

    storage.updateEndpoint(id, { enabled });
    res.json({ success: true, enabled });
  } catch (e) {
    res.status(500).json({ error: e.message });
  }
});

/**
 * 删除端点
 */
router.delete('/endpoints/:id', (req, res) => {
  try {
    const { id } = req.params;
    const deleted = storage.deleteEndpoint(id);
    if (!deleted) {
      return res.status(404).json({ error: '端点不存在' });
    }
    res.json({ success: true });
  } catch (e) {
    res.status(500).json({ error: e.message });
  }
});

/**
 * 验证端点
 */
router.post('/endpoints/:id/verify', async (req, res) => {
  try {
    const { id } = req.params;
    const endpoint = storage.getEndpointById(id);
    if (!endpoint) {
      return res.status(404).json({ error: '端点不存在' });
    }

    storage.touchEndpoint(id);
    const status = await openaiApi.getEndpointStatus(endpoint.baseUrl, endpoint.apiKey);

    // 更新端点状态
    storage.updateEndpoint(id, {
      status: status.status,
      models: status.models || [],
      lastChecked: status.checkedAt,
    });

    // 添加 valid 属性方便前端判断
    res.json({
      ...status,
      valid: status.status === 'valid',
    });
  } catch (e) {
    res.status(500).json({ error: e.message });
  }
});

/**
 * 获取端点的模型列表
 */
router.get('/endpoints/:id/models', async (req, res) => {
  try {
    const { id } = req.params;
    const endpoint = storage.getEndpointById(id);
    if (!endpoint) {
      return res.status(404).json({ error: '端点不存在' });
    }

    storage.touchEndpoint(id);
    const result = await openaiApi.listModels(endpoint.baseUrl, endpoint.apiKey);

    // 更新缓存的模型列表
    if (result.success) {
      storage.updateEndpoint(id, {
        models: result.models,
        lastChecked: new Date().toISOString(),
      });
    }

    res.json(result);
  } catch (e) {
    res.status(500).json({ error: e.message });
  }
});

/**
 * 刷新所有端点的模型列表
 * 支持两个路由: /endpoints/refresh 和 /endpoints/refresh-all
 */
router.post(['/endpoints/refresh', '/endpoints/refresh-all'], async (req, res) => {
  try {
    const endpoints = storage.getEnabledEndpoints();
    const results = [];

    for (const endpoint of endpoints) {
      try {
        const status = await openaiApi.getEndpointStatus(endpoint.baseUrl, endpoint.apiKey);
        storage.updateEndpoint(endpoint.id, {
          status: status.status,
          models: status.models || [],
          lastChecked: status.checkedAt,
        });
        results.push({
          id: endpoint.id,
          name: endpoint.name,
          success: status.status === 'valid',
          modelsCount: status.models?.length || 0,
        });
      } catch (e) {
        results.push({
          id: endpoint.id,
          name: endpoint.name,
          success: false,
          error: e.message,
        });
      }
    }

    res.json({ success: true, results });
  } catch (e) {
    res.status(500).json({ error: e.message });
  }
});

/**
 * 测试端点的聊天完成 API
 */
router.post('/endpoints/:id/test', async (req, res) => {
  try {
    const { id } = req.params;
    const { model } = req.body;

    const endpoint = storage.getEndpointById(id);
    if (!endpoint) {
      return res.status(404).json({ error: '端点不存在' });
    }

    storage.touchEndpoint(id);
    const result = await openaiApi.testChatCompletion(
      endpoint.baseUrl,
      endpoint.apiKey,
      model || 'gpt-3.5-turbo'
    );

    res.json(result);
  } catch (e) {
    res.status(500).json({ error: e.message });
  }
});

// ==================== 健康检查 ====================

/**
 * 单个模型健康检查
 * 使用流式 API，接收首个 chunk 即判定成功
 */
router.post('/endpoints/:id/health-check', async (req, res, next) => {
  try {
    const { id } = req.params;
    const { model, timeout } = req.body;

    if (!model) {
      const { BadRequestError } = require('../../src/middleware/errorHandler');
      throw new BadRequestError('模型名称必填');
    }

    const endpoint = storage.getEndpointById(id);
    if (!endpoint) {
      return res.status(404).json({ error: '端点不存在' });
    }

    storage.touchEndpoint(id);
    const result = await openaiApi.healthCheckModel(
      endpoint.baseUrl,
      endpoint.apiKey,
      model,
      timeout || openaiApi.DEFAULT_HEALTH_CHECK_TIMEOUT
    );

    // 更新存储中的模型健康状态
    storage.updateModelHealth(id, model, result);

    res.json(result);
  } catch (e) {
    next(e);
  }
});

/**
 * 批量健康检查端点的所有模型
 */
router.post('/endpoints/:id/health-check-all', async (req, res) => {
  try {
    const { id } = req.params;
    const { timeout, concurrency } = req.body;

    const endpoint = storage.getEndpointById(id);
    if (!endpoint) {
      return res.status(404).json({ error: '端点不存在' });
    }

    const models = endpoint.models || [];
    if (models.length === 0) {
      return res.json({
        success: true,
        totalModels: 0,
        message: '该端点没有模型可供检测',
      });
    }

    storage.touchEndpoint(id);
    const summary = await openaiApi.getEndpointHealthSummary(
      endpoint.baseUrl,
      endpoint.apiKey,
      models,
      timeout || openaiApi.DEFAULT_HEALTH_CHECK_TIMEOUT
    );

    // 更新所有模型的健康状态
    for (const result of summary.results) {
      storage.updateModelHealth(id, result.model, result);
    }

    // 更新端点的整体健康状态
    storage.updateEndpoint(id, {
      healthStatus: summary.overallStatus,
      lastHealthCheck: summary.checkedAt,
    });

    res.json({
      success: true,
      ...summary,
    });
  } catch (e) {
    res.status(500).json({ error: e.message });
  }
});

/**
 * 获取端点的健康状态（包括所有模型）
 */
router.get('/endpoints/:id/health', (req, res) => {
  try {
    const { id } = req.params;
    const endpoint = storage.getEndpointById(id);
    if (!endpoint) {
      return res.status(404).json({ error: '端点不存在' });
    }

    const healthData = storage.getEndpointHealth(id);

    res.json({
      endpointId: id,
      healthStatus: endpoint.healthStatus || 'unknown',
      lastHealthCheck: endpoint.lastHealthCheck || null,
      models: healthData,
    });
  } catch (e) {
    res.status(500).json({ error: e.message });
  }
});

/**
 * 批量健康检查所有端点的所有模型
 */
router.post('/health-check-all', async (req, res) => {
  try {
    const { timeout, concurrency } = req.body;
    const endpoints = storage.getEndpoints();
    const results = [];

    for (const endpoint of endpoints) {
      const models = endpoint.models || [];
      if (models.length === 0) {
        results.push({
          endpointId: endpoint.id,
          name: endpoint.name,
          totalModels: 0,
          skipped: true,
        });
        continue;
      }

      try {
        const summary = await openaiApi.getEndpointHealthSummary(
          endpoint.baseUrl,
          endpoint.apiKey,
          models,
          timeout || openaiApi.DEFAULT_HEALTH_CHECK_TIMEOUT
        );

        // 更新存储
        for (const result of summary.results) {
          storage.updateModelHealth(endpoint.id, result.model, result);
        }
        storage.updateEndpoint(endpoint.id, {
          healthStatus: summary.overallStatus,
          lastHealthCheck: summary.checkedAt,
        });

        results.push({
          endpointId: endpoint.id,
          name: endpoint.name,
          ...summary,
        });
      } catch (e) {
        results.push({
          endpointId: endpoint.id,
          name: endpoint.name,
          error: e.message,
        });
      }
    }

    res.json({
      success: true,
      checkedAt: new Date().toISOString(),
      endpoints: results,
    });
  } catch (e) {
    res.status(500).json({ error: e.message });
  }
});

// ==================== 导入导出 ====================

/**
 * 导出所有端点
 */
router.get('/export', (req, res) => {
  try {
    const endpoints = storage.exportEndpoints();
    res.json({
      success: true,
      endpoints: endpoints,
      exportedAt: new Date().toISOString(),
    });
  } catch (e) {
    res.status(500).json({ error: e.message });
  }
});

/**
 * 导入端点（直接覆盖数据库）
 */
router.post('/import', (req, res) => {
  try {
    const { endpoints, overwrite } = req.body;

    if (!endpoints || !Array.isArray(endpoints)) {
      return res.status(400).json({ error: '需要提供 endpoints 数组' });
    }

    if (overwrite) {
      // 直接覆盖所有端点
      storage.saveEndpoints(endpoints);
      res.json({ success: true, imported: endpoints.length });
    } else {
      // 使用原有的导入逻辑（去重）
      const result = storage.importEndpoints(endpoints, false);
      res.json({
        success: true,
        imported: result.importedCount,
        skipped: result.skippedCount,
        total: result.total,
      });
    }
  } catch (e) {
    res.status(500).json({ error: e.message });
  }
});

/**
 * 批量添加端点（简化格式）
 * 支持格式：每行一个，格式为 "名称:baseUrl:apiKey" 或 JSON 数组
 */
/**
 * OpenAI 兼容的对话接口 (代理转发)
 */
router.post(
  ['/', '/v1/chat/completions', '/chat/completions'],
  proxyLimiter,
  validate({ body: chatCompletionSchema }),
  async (req, res, next) => {
    const startTime = Date.now();
    try {
      const { model, stream } = req.body;
      const targetEndpointId = req.headers['x-endpoint-id'];

      let endpoint;

      // 1. 如果指定了端点 ID，优先使用
      if (targetEndpointId) {
        endpoint = storage.getEndpointById(targetEndpointId);
        logger.info(`Targeting endpoint: ${targetEndpointId}, found: ${endpoint ? endpoint.name : 'null'}`);

        if (!endpoint) {
          return res.status(404).json({ error: { message: 'Specified endpoint not found', type: 'invalid_request_error' } });
        }

        // 调试：打印详细的 enabled 类型
        // if (endpoint.enabled !== true && endpoint.enabled !== 1) {
        //    console.log(`[OpenAI Proxy] Endpoint disabled check: enabled value is ${endpoint.enabled} (type: ${typeof endpoint.enabled})`);
        // }

        // 暂时移除 enabled 检查，因为 UI 上没有入口启用/禁用，且 status === 'valid' 已足够
        // if (!endpoint.enabled && endpoint.enabled !== undefined) {
        //    return res.status(403).json({ error: { message: 'Specified endpoint is disabled', type: 'invalid_request_error' } });
        // }
      } else {
        // 2. 否则走原有的自动路由逻辑
        // enabled 为 true/1/undefined 时都允许（因为 UI 没有禁用入口，默认都认为是启用的）
        const endpoints = storage
          .getEndpoints()
          .filter(ep => ep.status === 'valid' && (ep.enabled !== false && ep.enabled !== 0));

        if (endpoints.length === 0) {
          return res
            .status(503)
            .json({
              error: { message: 'No valid OpenAI endpoints available', type: 'service_unavailable' },
            });
        }

        // 找到拥有该模型的端点
        const eligibleEndpoints = endpoints.filter(ep => ep.models && ep.models.includes(model));
        const targetEndpoints = eligibleEndpoints.length > 0 ? eligibleEndpoints : endpoints;

        // 负载均衡：随机选择一个端点
        endpoint = targetEndpoints[Math.floor(Math.random() * targetEndpoints.length)];
      }

      // 智能处理 URL：如果 baseUrl 不以 /v1 结尾且不包含 v1/chat，则尝试补全 /v1
      let fullUrl = endpoint.baseUrl.replace(/\/+$/, '');
      if (!fullUrl.toLowerCase().endsWith('/v1') && !fullUrl.toLowerCase().includes('/v1/')) {
        fullUrl += '/v1';
      }
      fullUrl += '/chat/completions';

      // ==================== 图片处理逻辑开始 ====================
      // 创建请求体的深拷贝，避免污染 req.body (影响日志记录)
      let upstreamBody = req.body;
      try {
        upstreamBody = JSON.parse(JSON.stringify(req.body));
      } catch (e) {
        logger.error(`[OpenAI Proxy] Body clone failed: ${e.message}`);
      }

      // 检查并转换本地图片路径为 Base64，以兼容公网 API
      // 优化：如果是内部模块 (Gemini CLI / Antigravity) 或目标是本地服务器，它们支持直接读取本地文件路径，无需转换
      // 这样可以避免下游日志中出现巨大的 Base64 字符串，且能正确显示图片路径
      const isInternalModule = fullUrl.includes('gemini-cli') || fullUrl.includes('antigravity');

      // 检查是否为本地地址（localhost/127.0.0.1/内网地址），本地模块可以直接读取文件
      const isLocalEndpoint = /^https?:\/\/(localhost|127\.0\.0\.1|0\.0\.0\.0|192\.168\.|10\.|172\.(1[6-9]|2[0-9]|3[0-1])\.)/i.test(fullUrl);

      if (!isInternalModule && !isLocalEndpoint && upstreamBody.messages && Array.isArray(upstreamBody.messages)) {
        try {
          const fs = require('fs');
          const path = require('path');

          for (const msg of upstreamBody.messages) {
            if (Array.isArray(msg.content)) {
              for (const part of msg.content) {
                if (part.type === 'image_url' && part.image_url && part.image_url.url) {
                  const imageUrl = part.image_url.url;
                  if (typeof imageUrl === 'string' && imageUrl.startsWith('/uploads/')) {
                    try {
                      const relativePath = imageUrl.startsWith('/') ? imageUrl.slice(1) : imageUrl;
                      const filePath = path.join(process.cwd(), 'data', relativePath);

                      if (fs.existsSync(filePath)) {
                        const fileBuffer = fs.readFileSync(filePath);
                        const ext = path.extname(filePath).toLowerCase();
                        let mimeType = 'image/jpeg';
                        if (ext === '.png') mimeType = 'image/png';
                        else if (ext === '.webp') mimeType = 'image/webp';
                        else if (ext === '.gif') mimeType = 'image/gif';

                        const base64Data = fileBuffer.toString('base64');
                        // 保存原始路径供下游日志使用
                        const originalUrl = part.image_url.url;
                        part.image_url.url = `data:${mimeType};base64,${base64Data}`;
                        part.image_url._original_url = originalUrl;

                        logger.info(`[OpenAI Proxy] Converted local image to Base64: ${filePath} (${Math.round(fileBuffer.length / 1024)}KB)`);
                      } else {
                        logger.warn(`[OpenAI Proxy] Local image file not found: ${filePath}`);
                      }
                    } catch (err) {
                      logger.error(`[OpenAI Proxy] Failed to convert local image: ${err.message}`);
                    }
                  }
                }
              }
            }
          }
        } catch (e) {
          logger.error(`[OpenAI Proxy] Image processing loop error: ${e.message}`);
        }
      }
      // ==================== 图片处理逻辑结束 ====================

      // 构建请求
      const axios = require('axios');
      const config = {
        method: 'post',
        url: fullUrl,
        headers: {
          Authorization: `Bearer ${endpoint.apiKey}`,
          'Content-Type': 'application/json',
          Accept: stream ? 'text/event-stream' : 'application/json',
        },
        data: upstreamBody,
        responseType: stream ? 'stream' : 'json',
        timeout: 60000,
      };

      const response = await axios(config);

      if (stream) {
        res.setHeader('Content-Type', 'text/event-stream');
        res.setHeader('Cache-Control', 'no-cache');
        res.setHeader('Connection', 'keep-alive');
        response.data.pipe(res);
      } else {
        res.status(response.status).json(response.data);
      }

      // 记录使用情况
      storage.touchEndpoint(endpoint.id);
    } catch (e) {
      logger.error(`OpenAI Proxy Error: ${e.message}`);

      // 严格隔离 Axios 错误对象，仅提取必要数据
      const responseStatus = e.response && e.response.status ? e.response.status : 500;
      let responseData = { error: { message: e.message, type: 'proxy_error' } };

      if (e.response && e.response.data) {
        // 深度克隆数据以断开任何潜在的引用链
        try {
          responseData = JSON.parse(JSON.stringify(e.response.data));
        } catch (parseErr) {
          responseData = { error: { message: String(e.response.data), type: 'api_error' } };
        }
      }

      res.status(responseStatus).json(responseData);
    }
  });

/**
 * OpenAI 兼容的模型列表接口
 */
router.get(['/v1/models', '/models'], async (req, res) => {
  try {
    const endpoints = storage
      .getEndpoints()
      .filter(ep => ep.status === 'valid' && (ep.enabled === true || ep.enabled === 1));

    // 使用 Map 来存储模型，Key 为模型 ID
    const modelMap = new Map();

    endpoints.forEach(ep => {
      if (ep.models) {
        ep.models.forEach(modelId => {
          // 如果模型已存在，且 owned_by 不是当前端点，则标记为 'multiple'
          // 或者我们可以保留第一个端点最为 owned_by，或者用逗号分隔
          if (modelMap.has(modelId)) {
            const existing = modelMap.get(modelId);
            if (!existing.owned_by.includes(ep.name)) {
              // optional: existing.owned_by += `, ${ep.name}`;
            }
          } else {
            modelMap.set(modelId, {
              id: modelId,
              object: 'model',
              created: Math.floor(Date.now() / 1000),
              owned_by: ep.name || 'openai', // 使用端点名称作为 owned_by
            });
          }
        });
      }
    });

    const modelList = Array.from(modelMap.values()).sort((a, b) => a.id.localeCompare(b.id));

    res.json({ object: 'list', data: modelList });
  } catch (e) {
    res.status(500).json({ error: e.message });
  }
});

module.exports = router;
