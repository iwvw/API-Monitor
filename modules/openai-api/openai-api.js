/**
 * OpenAI API 集成模块
 *
 * 支持 OpenAI 兼容的 API 端点
 * 用于验证 API Key 和获取模型列表
 * 支持模型健康检查（流式 API 检测）
 */

const https = require('https');
const http = require('http');
const { URL } = require('url');
const { createLogger } = require('../../src/utils/logger');
const logger = createLogger('OpenAI');

// 健康检查状态常量
const HealthStatus = {
  OPERATIONAL: 'operational', // 延迟 ≤ 6s
  DEGRADED: 'degraded', // 延迟 > 6s
  FAILED: 'failed', // 请求失败或超时
  UNKNOWN: 'unknown', // 未检测
};

// 默认配置
const DEFAULT_HEALTH_CHECK_TIMEOUT = 60000; // 60 秒超时 (适应慢速思考模型)
const DEGRADED_THRESHOLD = 20000; // 20 秒阈值

/**
 * 发送 HTTP 请求
 * @param {string} baseUrl - API 基础 URL
 * @param {string} apiKey - API Key
 * @param {string} method - HTTP 方法
 * @param {string} path - API 路径
 * @param {Object} body - 请求体
 */
function apiRequest(baseUrl, apiKey, method, path, body = null) {
  return new Promise((resolve, reject) => {
    try {
      // 解析 baseUrl
      let fullUrl = baseUrl.replace(/\/+$/, ''); // 移除末尾斜杠

      // 自动修正：如果用户填入了完整的 API 路径，截断它
      const endpointsToStrip = ['/chat/completions', '/completions', '/models', '/embeddings'];
      for (const end of endpointsToStrip) {
        if (fullUrl.endsWith(end)) {
          fullUrl = fullUrl.slice(0, -end.length).replace(/\/+$/, '');
        }
      }

      if (!fullUrl.startsWith('http://') && !fullUrl.startsWith('https://')) {
        fullUrl = 'https://' + fullUrl;
      }

      // 智能处理 /v1 路径
      // 如果 baseUrl 已经包含 /v1 或其他版本路径，使用它
      // 否则添加 /v1
      const hasVersionPath = /\/v\d+\/?/.test(fullUrl);
      if (!hasVersionPath) {
        fullUrl += '/v1';
      }

      // 构建完整的 URL
      // 移除 path 开头的 / 以便正确拼接
      const cleanPath = path.replace(/^\/+/, '');

      // 确保 fullUrl 以 / 结尾
      if (!fullUrl.endsWith('/')) {
        fullUrl += '/';
      }

      const url = new URL(cleanPath, fullUrl);

      logger.debug(`Request URL: ${url.href}`);
      const isHttps = url.protocol === 'https:';
      const httpModule = isHttps ? https : http;

      const options = {
        hostname: url.hostname,
        port: url.port || (isHttps ? 443 : 80),
        path: url.pathname + url.search,
        method: method,
        headers: {
          Authorization: `Bearer ${apiKey}`,
          'Content-Type': 'application/json',
          'User-Agent': 'API-Monitor/1.0',
        },
        timeout: 60000,
      };

      const req = httpModule.request(options, res => {
        let data = '';
        res.on('data', chunk => (data += chunk));
        res.on('end', () => {
          try {
            const json = JSON.parse(data);
            if (res.statusCode >= 200 && res.statusCode < 300) {
              resolve({ success: true, data: json, statusCode: res.statusCode });
            } else {
              resolve({
                success: false,
                error: json.error?.message || JSON.stringify(json),
                statusCode: res.statusCode,
              });
            }
          } catch (e) {
            // 如果响应不是 JSON，返回原始文本
            if (res.statusCode >= 200 && res.statusCode < 300) {
              resolve({ success: true, data: data, statusCode: res.statusCode });
            } else {
              resolve({
                success: false,
                error: data || 'Unknown error',
                statusCode: res.statusCode,
              });
            }
          }
        });
      });

      req.on('error', e => {
        reject(new Error(`Request failed: ${e.message}`));
      });

      req.on('timeout', () => {
        req.destroy();
        reject(new Error('Request timeout'));
      });

      if (body) {
        req.write(JSON.stringify(body));
      }
      req.end();
    } catch (e) {
      reject(new Error(`Invalid URL or request: ${e.message}`));
    }
  });
}

// ==================== API 验证 ====================

/**
 * 验证 API Key 是否有效
 * 通过获取模型列表来验证
 */
async function verifyApiKey(baseUrl, apiKey) {
  try {
    const result = await apiRequest(baseUrl, apiKey, 'GET', '/models');

    if (result.success && result.data) {
      // OpenAI 格式的响应
      if (result.data.data && Array.isArray(result.data.data)) {
        return {
          valid: true,
          modelsCount: result.data.data.length,
        };
      }
      // 有些 API 可能直接返回数组
      if (Array.isArray(result.data)) {
        return {
          valid: true,
          modelsCount: result.data.length,
        };
      }
      // 响应成功但格式不同
      return {
        valid: true,
        modelsCount: 0,
      };
    }

    return {
      valid: false,
      error: result.error || 'API Key 验证失败',
    };
  } catch (e) {
    return {
      valid: false,
      error: e.message,
    };
  }
}

// ==================== 模型管理 ====================

/**
 * 获取模型列表
 */
async function listModels(baseUrl, apiKey) {
  try {
    const result = await apiRequest(baseUrl, apiKey, 'GET', '/models');

    if (result.success && result.data) {
      let models = [];

      // OpenAI 格式的响应
      if (result.data.data && Array.isArray(result.data.data)) {
        models = result.data.data;
      }
      // 有些 API 可能直接返回数组
      else if (Array.isArray(result.data)) {
        models = result.data;
      }
      // 其他格式
      else if (result.data.models && Array.isArray(result.data.models)) {
        models = result.data.models;
      }

      // 只返回模型 ID 字符串数组，简化存储和显示
      return {
        success: true,
        models: models.map(m => m.id || m.name || 'unknown').filter(id => id !== 'unknown'),
      };
    }

    return {
      success: false,
      error: result.error || '获取模型列表失败',
      models: [],
    };
  } catch (e) {
    return {
      success: false,
      error: e.message,
      models: [],
    };
  }
}

/**
 * 测试聊天完成 API
 */
async function testChatCompletion(baseUrl, apiKey, model = 'gpt-3.5-turbo') {
  try {
    const result = await apiRequest(baseUrl, apiKey, 'POST', '/chat/completions', {
      model: model,
      messages: [
        { role: 'user', content: 'Say "Hello, API test successful!" in exactly those words.' },
      ],
      max_tokens: 50,
    });

    if (result.success && result.data) {
      const message = result.data.choices?.[0]?.message?.content || '';
      return {
        success: true,
        response: message,
        usage: result.data.usage || null,
      };
    }

    return {
      success: false,
      error: result.error || '测试失败',
    };
  } catch (e) {
    return {
      success: false,
      error: e.message,
    };
  }
}

// ==================== 模型健康检查 ====================

/**
 * 基于流式 API 的快速健康检查
 * 接收到首个 chunk 即判定成功，无需等待完整响应
 *
 * @param {string} baseUrl - API 基础 URL
 * @param {string} apiKey - API Key
 * @param {string} model - 模型名称
 * @param {number} timeout - 超时时间（毫秒），默认 60000
 * @returns {Promise<Object>} 健康检查结果
 */
async function healthCheckModel(baseUrl, apiKey, model, timeout = DEFAULT_HEALTH_CHECK_TIMEOUT) {
  const startTime = Date.now();

  return new Promise(resolve => {
    try {
      // 构建 URL
      let fullUrl = baseUrl.replace(/\/+$/, '');
      if (!fullUrl.startsWith('http://') && !fullUrl.startsWith('https://')) {
        fullUrl = 'https://' + fullUrl;
      }

      const hasVersionPath = /\/v\d+\/?/.test(fullUrl);
      if (!hasVersionPath) {
        fullUrl += '/v1';
      }

      // 确保 fullUrl 以 / 结尾
      if (!fullUrl.endsWith('/')) {
        fullUrl += '/';
      }

      const url = new URL('chat/completions', fullUrl);
      logger.debug(`Testing model: ${model} at ${url.href}`);

      const isHttps = url.protocol === 'https:';
      const httpModule = isHttps ? https : http;

      const requestBody = JSON.stringify({
        model: model,
        messages: [{ role: 'user', content: 'hi' }],
        stream: true, // 使用流式 API
      });

      const options = {
        hostname: url.hostname,
        port: url.port || (isHttps ? 443 : 80),
        path: url.pathname + url.search,
        method: 'POST',
        headers: {
          Authorization: `Bearer ${apiKey}`,
          'Content-Type': 'application/json',
          Accept: 'text/event-stream',
          'User-Agent': 'API-Monitor/1.0',
          'Content-Length': Buffer.byteLength(requestBody),
        },
        timeout: timeout,
      };

      const req = httpModule.request(options, res => {
        let firstChunkReceived = false;

        res.on('data', chunk => {
          if (!firstChunkReceived) {
            firstChunkReceived = true;
            const latency = Date.now() - startTime;

            // 收到首个 chunk 即判定成功
            req.destroy(); // 立即关闭连接，不等待完整响应

            // 判断状态
            let status;
            if (res.statusCode >= 200 && res.statusCode < 300) {
              status =
                latency <= DEGRADED_THRESHOLD ? HealthStatus.OPERATIONAL : HealthStatus.DEGRADED;
            } else {
              status = HealthStatus.FAILED;
            }

            logger.info(`${model}: ${status} (${latency}ms)`);

            resolve({
              model,
              status,
              latency,
              statusCode: res.statusCode,
              checkedAt: new Date().toISOString(),
            });
          }
        });

        res.on('end', () => {
          if (!firstChunkReceived) {
            const latency = Date.now() - startTime;

            // 没有收到任何数据
            if (res.statusCode >= 200 && res.statusCode < 300) {
              // 状态码正常但无数据，可能是非流式响应
              const status =
                latency <= DEGRADED_THRESHOLD ? HealthStatus.OPERATIONAL : HealthStatus.DEGRADED;
              resolve({
                model,
                status,
                latency,
                statusCode: res.statusCode,
                checkedAt: new Date().toISOString(),
              });
            } else {
              resolve({
                model,
                status: HealthStatus.FAILED,
                latency,
                statusCode: res.statusCode,
                error: `HTTP ${res.statusCode}`,
                checkedAt: new Date().toISOString(),
              });
            }
          }
        });

        res.on('error', e => {
          if (!firstChunkReceived) {
            resolve({
              model,
              status: HealthStatus.FAILED,
              latency: Date.now() - startTime,
              error: e.message,
              checkedAt: new Date().toISOString(),
            });
          }
        });
      });

      req.on('error', e => {
        resolve({
          model,
          status: HealthStatus.FAILED,
          latency: Date.now() - startTime,
          error: e.message,
          checkedAt: new Date().toISOString(),
        });
      });

      req.on('timeout', () => {
        req.destroy();
        resolve({
          model,
          status: HealthStatus.FAILED,
          latency: timeout,
          error: 'Request timeout',
          checkedAt: new Date().toISOString(),
        });
      });

      req.write(requestBody);
      req.end();
    } catch (e) {
      resolve({
        model,
        status: HealthStatus.FAILED,
        latency: Date.now() - startTime,
        error: e.message,
        checkedAt: new Date().toISOString(),
      });
    }
  });
}

/**
 * 批量健康检查多个模型
 * 并发执行，提高效率
 *
 * @param {string} baseUrl - API 基础 URL
 * @param {string} apiKey - API Key
 * @param {string[]} models - 模型名称数组
 * @param {number} timeout - 超时时间（毫秒）
 * @param {number} concurrency - 最大并发数，默认 5
 * @returns {Promise<Object[]>} 健康检查结果数组
 */
async function batchHealthCheck(
  baseUrl,
  apiKey,
  models,
  timeout = DEFAULT_HEALTH_CHECK_TIMEOUT,
  concurrency = 5
) {
  const results = [];
  const queue = [...models];

  // 创建并发执行任务
  const workers = [];
  for (let i = 0; i < Math.min(concurrency, models.length); i++) {
    workers.push(
      (async () => {
        while (queue.length > 0) {
          const model = queue.shift();
          if (model) {
            const result = await healthCheckModel(baseUrl, apiKey, model, timeout);
            results.push(result);
          }
        }
      })()
    );
  }

  await Promise.all(workers);

  // 按原始顺序排序
  const orderedResults = models.map(m => results.find(r => r.model === m)).filter(Boolean);

  return orderedResults;
}

/**
 * 获取端点所有模型的健康状态汇总
 */
async function getEndpointHealthSummary(
  baseUrl,
  apiKey,
  models,
  timeout = DEFAULT_HEALTH_CHECK_TIMEOUT
) {
  if (!models || models.length === 0) {
    return {
      totalModels: 0,
      operational: 0,
      degraded: 0,
      failed: 0,
      results: [],
      checkedAt: new Date().toISOString(),
    };
  }

  const results = await batchHealthCheck(baseUrl, apiKey, models, timeout);

  const summary = {
    totalModels: models.length,
    operational: results.filter(r => r.status === HealthStatus.OPERATIONAL).length,
    degraded: results.filter(r => r.status === HealthStatus.DEGRADED).length,
    failed: results.filter(r => r.status === HealthStatus.FAILED).length,
    results,
    checkedAt: new Date().toISOString(),
  };

  // 计算整体状态
  if (summary.failed === summary.totalModels) {
    summary.overallStatus = HealthStatus.FAILED;
  } else if (summary.operational === summary.totalModels) {
    summary.overallStatus = HealthStatus.OPERATIONAL;
  } else if (summary.failed > 0 || summary.degraded > 0) {
    summary.overallStatus = HealthStatus.DEGRADED;
  } else {
    summary.overallStatus = HealthStatus.UNKNOWN;
  }

  return summary;
}

/**
 * 获取端点状态信息
 */
async function getEndpointStatus(baseUrl, apiKey) {
  const startTime = Date.now();

  try {
    const verification = await verifyApiKey(baseUrl, apiKey);
    const responseTime = Date.now() - startTime;

    if (verification.valid) {
      const modelsResult = await listModels(baseUrl, apiKey);

      return {
        status: 'valid',
        responseTime,
        modelsCount: modelsResult.models?.length || verification.modelsCount || 0,
        models: modelsResult.models || [],
        checkedAt: new Date().toISOString(),
      };
    }

    return {
      status: 'invalid',
      responseTime,
      error: verification.error,
      checkedAt: new Date().toISOString(),
    };
  } catch (e) {
    return {
      status: 'error',
      responseTime: Date.now() - startTime,
      error: e.message,
      checkedAt: new Date().toISOString(),
    };
  }
}

module.exports = {
  verifyApiKey,
  listModels,
  testChatCompletion,
  getEndpointStatus,
  healthCheckModel,
  batchHealthCheck,
  getEndpointHealthSummary,
  HealthStatus,
  DEFAULT_HEALTH_CHECK_TIMEOUT,
  DEGRADED_THRESHOLD,
};
