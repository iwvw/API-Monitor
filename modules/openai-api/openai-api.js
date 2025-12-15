/**
 * OpenAI API 集成模块
 * 
 * 支持 OpenAI 兼容的 API 端点
 * 用于验证 API Key 和获取模型列表
 */

const https = require('https');
const http = require('http');
const { URL } = require('url');

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
      
      console.log('[OpenAI API] Request URL:', url.href);
      const isHttps = url.protocol === 'https:';
      const httpModule = isHttps ? https : http;
      
      const options = {
        hostname: url.hostname,
        port: url.port || (isHttps ? 443 : 80),
        path: url.pathname + url.search,
        method: method,
        headers: {
          'Authorization': `Bearer ${apiKey}`,
          'Content-Type': 'application/json',
          'User-Agent': 'API-Monitor/1.0'
        },
        timeout: 30000
      };

      const req = httpModule.request(options, (res) => {
        let data = '';
        res.on('data', chunk => data += chunk);
        res.on('end', () => {
          try {
            const json = JSON.parse(data);
            if (res.statusCode >= 200 && res.statusCode < 300) {
              resolve({ success: true, data: json, statusCode: res.statusCode });
            } else {
              resolve({ 
                success: false, 
                error: json.error?.message || JSON.stringify(json), 
                statusCode: res.statusCode 
              });
            }
          } catch (e) {
            // 如果响应不是 JSON，返回原始文本
            if (res.statusCode >= 200 && res.statusCode < 300) {
              resolve({ success: true, data: data, statusCode: res.statusCode });
            } else {
              resolve({ success: false, error: data || 'Unknown error', statusCode: res.statusCode });
            }
          }
        });
      });

      req.on('error', (e) => {
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
          modelsCount: result.data.data.length
        };
      }
      // 有些 API 可能直接返回数组
      if (Array.isArray(result.data)) {
        return {
          valid: true,
          modelsCount: result.data.length
        };
      }
      // 响应成功但格式不同
      return {
        valid: true,
        modelsCount: 0
      };
    }
    
    return {
      valid: false,
      error: result.error || 'API Key 验证失败'
    };
  } catch (e) {
    return {
      valid: false,
      error: e.message
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
        models: models.map(m => m.id || m.name || 'unknown').filter(id => id !== 'unknown')
      };
    }
    
    return {
      success: false,
      error: result.error || '获取模型列表失败',
      models: []
    };
  } catch (e) {
    return {
      success: false,
      error: e.message,
      models: []
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
        { role: 'user', content: 'Say "Hello, API test successful!" in exactly those words.' }
      ],
      max_tokens: 50
    });
    
    if (result.success && result.data) {
      const message = result.data.choices?.[0]?.message?.content || '';
      return {
        success: true,
        response: message,
        usage: result.data.usage || null
      };
    }
    
    return {
      success: false,
      error: result.error || '测试失败'
    };
  } catch (e) {
    return {
      success: false,
      error: e.message
    };
  }
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
        checkedAt: new Date().toISOString()
      };
    }
    
    return {
      status: 'invalid',
      responseTime,
      error: verification.error,
      checkedAt: new Date().toISOString()
    };
  } catch (e) {
    return {
      status: 'error',
      responseTime: Date.now() - startTime,
      error: e.message,
      checkedAt: new Date().toISOString()
    };
  }
}

module.exports = {
  verifyApiKey,
  listModels,
  testChatCompletion,
  getEndpointStatus
};