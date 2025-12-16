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
    const { name, baseUrl, apiKey, notes } = req.body;
    
    if (!name || !baseUrl || !apiKey) {
      return res.status(400).json({ error: '名称、API 地址和 API Key 必填' });
    }

    // 验证 API Key
    const verification = await openaiApi.verifyApiKey(baseUrl, apiKey);
    
    const endpoint = storage.addEndpoint({ name, baseUrl, apiKey, notes });
    
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
router.post('/endpoints/refresh', async (req, res) => {
  try {
    const endpoints = storage.getEndpoints();
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
 * 导入端点
 */
router.post('/import', (req, res) => {
  try {
    const { endpoints, overwrite } = req.body;
    
    if (!endpoints || !Array.isArray(endpoints)) {
      return res.status(400).json({ error: '需要提供 endpoints 数组' });
    }

    // 验证数据格式
    for (const ep of endpoints) {
      if (!ep.baseUrl || !ep.apiKey) {
        return res.status(400).json({ error: '每个端点必须包含 baseUrl 和 apiKey' });
      }
    }

    const result = storage.importEndpoints(endpoints, overwrite === true);
    
    res.json({
      success: true,
      ...result
    });
  } catch (e) {
    res.status(500).json({ error: e.message });
  }
});

/**
 * 批量添加端点（简化格式）
 * 支持格式：每行一个，格式为 "名称:baseUrl:apiKey" 或 JSON 数组
 */
router.post('/batch-add', async (req, res) => {
  try {
    const { text, endpoints: jsonEndpoints } = req.body;
    let endpointsToAdd = [];

    if (text) {
      // 解析文本格式
      const lines = text.split('\n').filter(line => line.trim());
      for (const line of lines) {
        const parts = line.split(':');
        if (parts.length >= 3) {
          // 格式: name:baseUrl:apiKey 或 name:https://url:apiKey
          const name = parts[0].trim();
          // 处理 URL 中可能包含的冒号（如 https://）
          let baseUrl, apiKey;
          if (parts[1].includes('//')) {
            // URL 格式如 https://api.example.com
            baseUrl = parts[1] + ':' + parts[2];
            apiKey = parts.slice(3).join(':').trim();
          } else {
            baseUrl = parts[1].trim();
            apiKey = parts.slice(2).join(':').trim();
          }
          
          if (name && baseUrl && apiKey) {
            endpointsToAdd.push({ name, baseUrl, apiKey });
          }
        }
      }
    } else if (jsonEndpoints && Array.isArray(jsonEndpoints)) {
      endpointsToAdd = jsonEndpoints;
    }

    if (endpointsToAdd.length === 0) {
      return res.status(400).json({ error: '没有有效的端点数据' });
    }

    const results = [];
    for (const ep of endpointsToAdd) {
      try {
        const endpoint = storage.addEndpoint(ep);
        // 验证并获取模型
        const verification = await openaiApi.verifyApiKey(ep.baseUrl, ep.apiKey);
        if (verification.valid) {
          const modelsResult = await openaiApi.listModels(ep.baseUrl, ep.apiKey);
          storage.updateEndpoint(endpoint.id, {
            status: 'valid',
            models: modelsResult.models || [],
            lastChecked: new Date().toISOString()
          });
        } else {
          storage.updateEndpoint(endpoint.id, {
            status: 'invalid',
            lastChecked: new Date().toISOString()
          });
        }
        results.push({ name: ep.name, success: true, valid: verification.valid });
      } catch (e) {
        results.push({ name: ep.name, success: false, error: e.message });
      }
    }

    res.json({
      success: true,
      added: results.filter(r => r.success).length,
      failed: results.filter(r => !r.success).length,
      results
    });
  } catch (e) {
    res.status(500).json({ error: e.message });
  }
});

module.exports = router;