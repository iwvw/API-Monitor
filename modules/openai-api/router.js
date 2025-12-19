/**
 * OpenAI API 管理 - API 路由
 */

const express = require('express');
const router = express.Router();
const storage = require('./storage');
const openaiApi = require('./openai-api');

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
          lastChecked: new Date().toISOString()
        });
        endpoint.status = 'valid';
        endpoint.models = modelsResult.models || [];
      } else {
        storage.updateEndpoint(endpoint.id, {
          status: 'invalid',
          lastChecked: new Date().toISOString()
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
      verification
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
          name, baseUrl, apiKey, notes,
          status: 'valid',
          models: modelsResult.models || [],
          lastChecked: new Date().toISOString()
        });
      } else {
        storage.updateEndpoint(id, {
          name, baseUrl, apiKey, notes,
          status: 'invalid',
          lastChecked: new Date().toISOString()
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
      lastChecked: status.checkedAt
    });

    // 添加 valid 属性方便前端判断
    res.json({
      ...status,
      valid: status.status === 'valid'
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
        lastChecked: new Date().toISOString()
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
          lastChecked: status.checkedAt
        });
        results.push({
          id: endpoint.id,
          name: endpoint.name,
          success: status.status === 'valid',
          modelsCount: status.models?.length || 0
        });
      } catch (e) {
        results.push({
          id: endpoint.id,
          name: endpoint.name,
          success: false,
          error: e.message
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
router.post('/endpoints/:id/health-check', async (req, res) => {
  try {
    const { id } = req.params;
    const { model, timeout } = req.body;

    if (!model) {
      return res.status(400).json({ error: '模型名称必填' });
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
    res.status(500).json({ error: e.message });
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
        message: '该端点没有模型可供检测'
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
      lastHealthCheck: summary.checkedAt
    });

    res.json({
      success: true,
      ...summary
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
      models: healthData
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
          skipped: true
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
          lastHealthCheck: summary.checkedAt
        });

        results.push({
          endpointId: endpoint.id,
          name: endpoint.name,
          ...summary
        });
      } catch (e) {
        results.push({
          endpointId: endpoint.id,
          name: endpoint.name,
          error: e.message
        });
      }
    }

    res.json({
      success: true,
      checkedAt: new Date().toISOString(),
      endpoints: results
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
      exportedAt: new Date().toISOString()
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
        total: result.total
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
router.post(['/', '/v1/chat/completions', '/chat/completions'], async (req, res) => {
  const startTime = Date.now();
  try {
    const { model, stream } = req.body;
    const endpoints = storage.getEndpoints().filter(ep => ep.status === 'valid' && (ep.enabled === true || ep.enabled === 1));

    if (endpoints.length === 0) {
      return res.status(503).json({ error: { message: 'No valid OpenAI endpoints available', type: 'service_unavailable' } });
    }

    // 找到拥有该模型的端点
    const eligibleEndpoints = endpoints.filter(ep => ep.models && ep.models.includes(model));
    const targetEndpoints = eligibleEndpoints.length > 0 ? eligibleEndpoints : endpoints;

    // 负载均衡：随机选择一个端点
    const endpoint = targetEndpoints[Math.floor(Math.random() * targetEndpoints.length)];

    // 构建请求
    const axios = require('axios');
    const config = {
      method: 'post',
      url: `${endpoint.baseUrl.replace(/\/+$/, '')}/chat/completions`,
      headers: {
        'Authorization': `Bearer ${endpoint.apiKey}`,
        'Content-Type': 'application/json',
        'Accept': stream ? 'text/event-stream' : 'application/json'
      },
      data: req.body,
      responseType: stream ? 'stream' : 'json',
      timeout: 60000
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
    console.error('OpenAI Proxy Error:', e.message);
    
    // 严格隔离 Axios 错误对象，仅提取必要数据
    const responseStatus = (e.response && e.response.status) ? e.response.status : 500;
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
    const endpoints = storage.getEndpoints().filter(ep => ep.status === 'valid' && (ep.enabled === true || ep.enabled === 1));
    const allModels = new Set();
    
    endpoints.forEach(ep => {
      if (ep.models) {
        ep.models.forEach(m => allModels.add(m));
      }
    });

    const modelList = Array.from(allModels).map(id => ({
      id,
      object: 'model',
      created: Math.floor(Date.now() / 1000),
      owned_by: 'openai'
    }));

    res.json({ object: 'list', data: modelList });
  } catch (e) {
    res.status(500).json({ error: e.message });
  }
});

module.exports = router;