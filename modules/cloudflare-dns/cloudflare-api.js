/**
 * Cloudflare API 集成模块
 *
 * 使用 Cloudflare API v4
 * 文档: https://developers.cloudflare.com/api
 */

const https = require('https');

const CF_API_BASE = 'api.cloudflare.com';

/**
 * 发送 Cloudflare API 请求
 * @param {string|object} auth - API Token (string) 或 { email, key } 对象
 */
function cfRequest(auth, method, path, body = null) {
  return new Promise((resolve, reject) => {
    const headers = {
      Accept: 'application/json',
    };

    // 只有在有 body 时才设置 Content-Type
    if (body && ['POST', 'PUT', 'PATCH'].includes(method.toUpperCase())) {
      headers['Content-Type'] = 'application/json';
    }

    // 认证处理
    if (typeof auth === 'string') {
      headers['Authorization'] = `Bearer ${auth.toString().trim()}`;
    } else if (auth && auth.email && auth.key) {
      headers['X-Auth-Email'] = auth.email.toString().trim();
      headers['X-Auth-Key'] = auth.key.toString().trim();
    }

    // 严格清理 Headers: 移除不可见字符和非 ASCII 字符
    Object.keys(headers).forEach(key => {
      if (typeof headers[key] === 'string') {
        headers[key] = headers[key].replace(/[^\x20-\x7E]/g, '').trim();
      }
    });

    // 处理 GraphQL 路径
    const fullPath = path.startsWith('/graphql') ? path : `/client/v4${path}`;

    const options = {
      hostname: CF_API_BASE,
      path: fullPath,
      method: method,
      headers: headers,
      timeout: 15000,
    };

    // 调试日志
    // console.log(`[CF-API] REQUEST: ${method} ${fullPath}`);

    try {
      const req = https.request(options, res => {
        let data = '';
        res.on('data', chunk => (data += chunk));
        res.on('end', () => {
          try {
            const json = JSON.parse(data);
            if (json.success || (res.statusCode >= 200 && res.statusCode < 300 && json.result)) {
              resolve(json);
            } else {
              const errors = json.errors || [];
              let errorMsg =
                errors.map(e => e.message).join(', ') || json.message || 'Unknown error';

              // 如果是 400 错误且有特定的错误信息
              if (res.statusCode === 400 && data.includes('invalid_request_headers')) {
                console.error('[CF-API] 原始报错数据:', data);
                errorMsg = 'Invalid request headers (可能由不必要的 Content-Type 或 Accept 引起)';
              }

              const error = new Error(errorMsg);
              error.statusCode = res.statusCode;
              error.response = data;
              error.path = path;
              reject(error);
            }
          } catch (e) {
            if (res.statusCode >= 200 && res.statusCode < 300) {
              resolve({ success: true, result: data });
            } else {
              reject(new Error(`HTTP ${res.statusCode}: Invalid JSON response`));
            }
          }
        });
      });

      req.on('error', reject);
      req.on('timeout', () => {
        req.destroy();
        reject(new Error('Request timeout'));
      });

      if (body) {
        req.write(JSON.stringify(body));
      }
      req.end();
    } catch (syncError) {
      console.error('[CF-API] https.request 同步报错:', syncError.message);
      console.error('[CF-API] 当前 Headers:', JSON.stringify(headers));
      reject(syncError);
    }
  });
}

// ==================== 账号验证 ====================

/**
 * 验证 API Token 或 Global API Key 是否有效
 * @param {string|object} auth - API Token (string) 或 { email, key } 对象
 */
async function verifyToken(auth) {
  try {
    // 对于 Global API Key，使用 /user 端点验证
    // 对于 API Token，使用 /user/tokens/verify 端点
    const endpoint =
      typeof auth === 'object' && auth.email
        ? '/user' // Global API Key
        : '/user/tokens/verify'; // API Token

    const result = await cfRequest(auth, 'GET', endpoint);

    return {
      valid: true,
      status: result.result?.status || 'active',
      expiresOn: result.result?.expires_on,
    };
  } catch (e) {
    return {
      valid: false,
      error: e.message,
    };
  }
}

/**
 * 获取用户信息
 */
async function getUserInfo(apiToken) {
  const result = await cfRequest(apiToken, 'GET', '/user');
  return result.result;
}

// ==================== Zone 管理 ====================

/**
 * 获取所有 Zone（域名）
 * @param {string} apiToken
 * @param {Object} options - { page, per_page, name, status }
 */
async function listZones(apiToken, options = {}) {
  const params = new URLSearchParams();
  if (options.page) params.append('page', options.page);
  if (options.per_page) params.append('per_page', options.per_page || 50);
  if (options.name) params.append('name', options.name);
  if (options.status) params.append('status', options.status);

  const query = params.toString();
  const path = '/zones' + (query ? `?${query}` : '');

  const result = await cfRequest(apiToken, 'GET', path);
  return {
    zones: result.result,
    resultInfo: result.result_info,
  };
}

/**
 * 获取单个 Zone 信息
 */
async function getZone(apiToken, zoneId) {
  const result = await cfRequest(apiToken, 'GET', `/zones/${zoneId}`);
  return result.result;
}

/**
 * 创建新的 Zone (添加域名)
 * @param {string} apiToken
 * @param {string} name - 域名,如 "example.com"
 * @param {Object} options - { account: { id }, jump_start, type }
 */
async function createZone(apiToken, name, options = {}) {
  const body = {
    name,
    jump_start: options.jump_start !== undefined ? options.jump_start : false,
    type: options.type || 'full', // full, partial, secondary
  };

  // 如果提供了账号 ID
  if (options.account && options.account.id) {
    body.account = { id: options.account.id };
  }

  const result = await cfRequest(apiToken, 'POST', '/zones', body);
  return result.result;
}

/**
 * 删除 Zone (删除域名)
 * @param {string} apiToken
 * @param {string} zoneId
 */
async function deleteZone(apiToken, zoneId) {
  const result = await cfRequest(apiToken, 'DELETE', `/zones/${zoneId}`);
  return result.result;
}

// ==================== DNS 记录管理 ====================

/**
 * 获取 Zone 的所有 DNS 记录
 * @param {string} apiToken
 * @param {string} zoneId
 * @param {Object} options - { type, name, content, page, per_page }
 */
async function listDnsRecords(apiToken, zoneId, options = {}) {
  const params = new URLSearchParams();
  if (options.type) params.append('type', options.type);
  if (options.name) params.append('name', options.name);
  if (options.content) params.append('content', options.content);
  if (options.page) params.append('page', options.page);
  params.append('per_page', options.per_page || 100);

  const query = params.toString();
  const path = `/zones/${zoneId}/dns_records` + (query ? `?${query}` : '');

  const result = await cfRequest(apiToken, 'GET', path);
  return {
    records: result.result,
    resultInfo: result.result_info,
  };
}

/**
 * 获取单个 DNS 记录
 */
async function getDnsRecord(apiToken, zoneId, recordId) {
  const result = await cfRequest(apiToken, 'GET', `/zones/${zoneId}/dns_records/${recordId}`);
  return result.result;
}

/**
 * 创建 DNS 记录
 * @param {string} apiToken
 * @param {string} zoneId
 * @param {Object} record - { type, name, content, ttl?, priority?, proxied? }
 */
async function createDnsRecord(apiToken, zoneId, record) {
  const body = {
    type: record.type,
    name: record.name,
    content: record.content,
    ttl: record.ttl || 1,
    proxied: record.proxied !== undefined ? record.proxied : true,
  };

  // MX 和 SRV 记录需要 priority
  if (record.priority !== undefined) {
    body.priority = record.priority;
  }

  const result = await cfRequest(apiToken, 'POST', `/zones/${zoneId}/dns_records`, body);
  return result.result;
}

/**
 * 更新 DNS 记录
 */
async function updateDnsRecord(apiToken, zoneId, recordId, record) {
  const body = {};
  if (record.type) body.type = record.type;
  if (record.name) body.name = record.name;
  if (record.content) body.content = record.content;
  if (record.ttl !== undefined) body.ttl = record.ttl;
  if (record.proxied !== undefined) body.proxied = record.proxied;
  if (record.priority !== undefined) body.priority = record.priority;

  const result = await cfRequest(
    apiToken,
    'PATCH',
    `/zones/${zoneId}/dns_records/${recordId}`,
    body
  );
  return result.result;
}

/**
 * 删除 DNS 记录
 */
async function deleteDnsRecord(apiToken, zoneId, recordId) {
  await cfRequest(apiToken, 'DELETE', `/zones/${zoneId}/dns_records/${recordId}`);
  return true;
}

/**
 * 批量创建 DNS 记录
 * @param {string} apiToken
 * @param {string} zoneId
 * @param {Array} records - 记录数组
 */
async function batchCreateDnsRecords(apiToken, zoneId, records) {
  const results = [];
  const errors = [];

  for (const record of records) {
    try {
      const created = await createDnsRecord(apiToken, zoneId, record);
      results.push({ success: true, record: created });
    } catch (e) {
      errors.push({ success: false, record, error: e.message });
    }
  }

  return { results, errors };
}

/**
 * 快速切换 DNS 记录的 IP
 * 查找指定类型和名称的记录，更新其 content
 */
async function switchDnsContent(apiToken, zoneId, type, name, newContent) {
  // 查找匹配的记录
  const { records } = await listDnsRecords(apiToken, zoneId, { type, name });

  if (records.length === 0) {
    throw new Error(`No ${type} record found for ${name}`);
  }

  const updated = [];
  for (const record of records) {
    const result = await updateDnsRecord(apiToken, zoneId, record.id, {
      content: newContent,
    });
    updated.push(result);
  }

  return updated;
}

/**
 * 导出 Zone 的所有 DNS 记录为 BIND 格式
 */
async function exportDnsRecords(apiToken, zoneId) {
  const result = await cfRequest(apiToken, 'GET', `/zones/${zoneId}/dns_records/export`);
  return result.result;
}

/**
 * 清除 Zone 的缓存
 * @param {string} apiToken
 * @param {string} zoneId
 * @param {Object} options - { purge_everything: true } 或 { files: [...] } 或 { tags: [...] }
 */
async function purgeCache(apiToken, zoneId, options = {}) {
  const body = options.purge_everything
    ? { purge_everything: true }
    : options.files
      ? { files: options.files }
      : options.tags
        ? { tags: options.tags }
        : { purge_everything: true };

  console.log('[CF-API] 清除缓存请求:', { zoneId, body });

  try {
    const result = await cfRequest(apiToken, 'POST', `/zones/${zoneId}/purge_cache`, body);
    console.log('[CF-API] 清除缓存成功:', result);
    return result.result;
  } catch (error) {
    console.error('[CF-API] 清除缓存失败:', error.message);
    throw error;
  }
}

// ==================== SSL/TLS 管理 ====================

/**
 * 获取 Zone 的 SSL 设置
 * @param {string|object} auth - API Token 或 { email, key }
 * @param {string} zoneId
 */
async function getSslSettings(auth, zoneId) {
  try {
    const result = await cfRequest(auth, 'GET', `/zones/${zoneId}/settings/ssl`);
    return result.result;
  } catch (e) {
    console.error('[CF-API] 获取SSL设置失败:', e.message);
    throw e;
  }
}

/**
 * 修改 Zone 的 SSL 模式
 * @param {string|object} auth
 * @param {string} zoneId
 * @param {string} mode - off, flexible, full, strict
 */
async function updateSslMode(auth, zoneId, mode) {
  const body = { value: mode };
  const result = await cfRequest(auth, 'PATCH', `/zones/${zoneId}/settings/ssl`, body);
  return result.result;
}

/**
 * 获取 Zone 的 SSL 证书包
 * @param {string|object} auth
 * @param {string} zoneId
 */
async function getSslCertificates(auth, zoneId) {
  try {
    const result = await cfRequest(auth, 'GET', `/zones/${zoneId}/ssl/certificate_packs`);
    return result.result || [];
  } catch (e) {
    console.error('[CF-API] 获取SSL证书失败:', e.message);
    return [];
  }
}

/**
 * 获取 SSL 验证状态
 * @param {string|object} auth
 * @param {string} zoneId
 */
async function getSslVerification(auth, zoneId) {
  try {
    const result = await cfRequest(auth, 'GET', `/zones/${zoneId}/ssl/verification`);
    return result.result || [];
  } catch (e) {
    console.error('[CF-API] 获取SSL验证状态失败:', e.message);
    return [];
  }
}

// ==================== Analytics 分析 ====================

/**
 * 获取 Zone 的 Analytics 数据
 * @param {string|object} auth
 * @param {string} zoneId
 * @param {Object} options - { since, until }
 */
async function getZoneAnalytics(auth, zoneId, options = {}) {
  try {
    // 默认获取最近24小时的数据
    const since = options.since || new Date(Date.now() - 24 * 60 * 60 * 1000).toISOString();
    const until = options.until || new Date().toISOString();

    const params = new URLSearchParams({
      since,
      until,
    });

    const result = await cfRequest(
      auth,
      'GET',
      `/zones/${zoneId}/analytics/dashboard?${params.toString()}`
    );

    return result.result;
  } catch (e) {
    console.error('[CF-API] 获取Analytics失败:', e.message);
    throw e;
  }
}

/**
 * 获取简化的Analytics数据（用于仪表板）
 * @param {string|object} auth
 * @param {string} zoneId
 * @param {string} timeRange - '24h', '7d', '30d'
 */
async function getSimpleAnalytics(auth, zoneId, timeRange = '24h') {
  const limits = {
    '24h': 24,
    '7d': 168,
    '30d': 720,
  };

  const limit = limits[timeRange] || 24;

  try {
    // 使用GraphQL Analytics Engine API - httpRequests1hGroups
    const query = `{
      viewer {
        zones(filter: {zoneTag: "${zoneId}"}) {
          httpRequests1hGroups(
            limit: ${limit}
            orderBy: [datetime_DESC]
          ) {
            sum {
              requests
              bytes
              cachedRequests
              cachedBytes
              threats
              pageViews
            }
            uniq {
              uniques
            }
          }
        }
      }
    }`;

    const result = await cfRequest(auth, 'POST', '/graphql', { query });

    console.log('[CF-API] GraphQL Analytics原始数据:', result);

    // 聚合所有小时的数据
    const groups = result.data?.viewer?.zones?.[0]?.httpRequests1hGroups || [];

    let totalRequests = 0;
    let totalBytes = 0;
    let totalCached = 0;
    let totalThreats = 0;
    let totalPageViews = 0;
    const uniquesSet = new Set();

    groups.forEach(group => {
      const sum = group.sum || {};
      const uniq = group.uniq || {};

      totalRequests += sum.requests || 0;
      totalBytes += sum.bytes || 0;
      totalCached += sum.cachedRequests || 0;
      totalThreats += sum.threats || 0;
      totalPageViews += sum.pageViews || 0;

      if (uniq.uniques) uniquesSet.add(uniq.uniques);
    });

    const cacheHitRate = totalRequests > 0 ? Math.round((totalCached / totalRequests) * 100) : 0;

    return {
      requests: totalRequests,
      bandwidth: totalBytes,
      threats: totalThreats,
      pageViews: totalPageViews,
      uniques: uniquesSet.size > 0 ? Math.max(...uniquesSet) : 0,
      cacheHitRate: cacheHitRate,
      timeseries: [],
    };
  } catch (e) {
    console.error('[CF-API] 获取Analytics失败:', e.message);
    console.error('[CF-API] 错误详情:', e);
    return {
      requests: 0,
      bandwidth: 0,
      threats: 0,
      pageViews: 0,
      uniques: 0,
      cacheHitRate: 0,
      timeseries: [],
    };
  }
}

/**
 * 计算缓存命中率
 */
function calculateCacheHitRate(totals) {
  const cached = totals.requests?.cached || 0;
  const all = totals.requests?.all || 0;
  return all > 0 ? Math.round((cached / all) * 100) : 0;
}

// ==================== 实用函数 ====================

/**
 * 获取支持的 DNS 记录类型
 */
function getSupportedRecordTypes() {
  return ['A', 'AAAA', 'CNAME', 'TXT', 'MX', 'NS', 'SRV', 'CAA', 'PTR'];
}

/**
 * 验证 DNS 记录数据
 */
function validateDnsRecord(record) {
  const errors = [];

  if (!record.type) {
    errors.push('Type is required');
  } else if (!getSupportedRecordTypes().includes(record.type)) {
    errors.push(`Invalid type: ${record.type}`);
  }

  if (!record.name) {
    errors.push('Name is required');
  }

  if (!record.content) {
    errors.push('Content is required');
  }

  // A 记录需要有效的 IPv4
  if (record.type === 'A') {
    const ipv4Regex = /^(\d{1,3}\.){3}\d{1,3}$/;
    if (!ipv4Regex.test(record.content)) {
      errors.push('Invalid IPv4 address');
    }
  }

  // AAAA 记录需要有效的 IPv6
  if (record.type === 'AAAA') {
    const ipv6Regex =
      /^([0-9a-fA-F]{1,4}:){7}[0-9a-fA-F]{1,4}$|^::$|^([0-9a-fA-F]{1,4}:)*::([0-9a-fA-F]{1,4}:)*[0-9a-fA-F]{1,4}$/;
    if (!ipv6Regex.test(record.content)) {
      errors.push('Invalid IPv6 address');
    }
  }

  // MX 记录需要 priority
  if (record.type === 'MX' && record.priority === undefined) {
    errors.push('MX record requires priority');
  }

  return {
    valid: errors.length === 0,
    errors,
  };
}

// ==================== Workers 管理 ====================

/**
 * 获取账号 ID (Workers API 需要)
 */
async function getAccountId(auth) {
  const result = await cfRequest(auth, 'GET', '/accounts?page=1&per_page=1');
  if (result.result && result.result.length > 0) {
    return result.result[0].id;
  }
  throw new Error('No account found for this token');
}

/**
 * 列出所有 Workers
 * @param {string} apiToken
 * @param {string} accountId - Cloudflare 账号 ID
 */
async function listWorkers(apiToken, accountId) {
  const result = await cfRequest(apiToken, 'GET', `/accounts/${accountId}/workers/scripts`);
  return result.result || [];
}

/**
 * 获取 Worker 脚本内容
 * @param {string} apiToken
 * @param {string} accountId
 * @param {string} scriptName
 */
async function getWorkerScript(apiToken, accountId, scriptName) {
  // 获取脚本代码
  return new Promise((resolve, reject) => {
    const headers = {
      Accept: 'application/javascript', // 请求纯 JavaScript 格式
    };

    // 认证处理
    if (typeof apiToken === 'string') {
      headers['Authorization'] = `Bearer ${apiToken}`;
    } else if (apiToken && apiToken.email && apiToken.key) {
      headers['X-Auth-Email'] = apiToken.email;
      headers['X-Auth-Key'] = apiToken.key;
    }

    const options = {
      hostname: CF_API_BASE,
      path: `/client/v4/accounts/${accountId}/workers/scripts/${scriptName}`,
      method: 'GET',
      headers: headers,
    };

    const req = https.request(options, res => {
      let data = '';
      res.on('data', chunk => (data += chunk));
      res.on('end', () => {
        // 检查是否是错误响应 (JSON)
        if (res.statusCode !== 200) {
          try {
            const errorJson = JSON.parse(data);
            const errMsg = errorJson.errors?.map(e => e.message).join(', ') || 'Unknown error';
            reject(new Error(errMsg));
          } catch {
            reject(new Error(`HTTP ${res.statusCode}: ${data.substring(0, 200)}`));
          }
          return;
        }

        // 如果返回的是 multipart 格式，提取脚本内容
        let scriptContent = data;
        const contentType = res.headers['content-type'] || '';

        if (contentType.includes('multipart')) {
          // 解析 multipart 响应，提取 JavaScript 内容
          const boundaryMatch = contentType.match(/boundary=(.+)/);
          if (boundaryMatch) {
            const boundary = boundaryMatch[1].replace(/"/g, '');
            const parts = data.split('--' + boundary);

            for (const part of parts) {
              // 找到包含 JavaScript 的部分
              if (
                part.includes('application/javascript') ||
                (part.includes('filename=') && part.includes('.js'))
              ) {
                // 提取实际内容（跳过 headers）
                const contentStart = part.indexOf('\r\n\r\n');
                if (contentStart !== -1) {
                  scriptContent = part.substring(contentStart + 4).trim();
                  // 移除末尾的 boundary 标记
                  if (scriptContent.endsWith('--')) {
                    scriptContent = scriptContent.slice(0, -2).trim();
                  }
                  break;
                }
              }
            }
          }
        }

        resolve({
          name: scriptName,
          script: scriptContent,
          meta: null,
        });
      });
    });

    req.on('error', reject);
    req.end();
  });
}

/**
 * 创建或更新 Worker 脚本
 * @param {string} apiToken
 * @param {string} accountId
 * @param {string} scriptName
 * @param {string} scriptContent - JavaScript 代码
 * @param {Object} metadata - 可选的元数据 { bindings, compatibility_date, etc. }
 */
async function putWorkerScript(apiToken, accountId, scriptName, scriptContent, metadata = {}) {
  return new Promise((resolve, reject) => {
    const boundary = '----CloudflareWorkerBoundary' + Date.now();

    // 检测是否是 ES Module 格式
    const isEsModule =
      scriptContent.includes('export default') ||
      scriptContent.includes('export {') ||
      scriptContent.includes('export async');

    // 构建 multipart 数据
    let body = '';

    // 元数据部分
    const meta = {
      bindings: metadata.bindings || [],
      compatibility_date: metadata.compatibility_date || new Date().toISOString().split('T')[0],
    };

    // 根据模块类型设置不同的配置
    if (isEsModule) {
      meta.main_module = 'worker.js';
    } else {
      meta.body_part = 'script';
    }

    body += `--${boundary}\r\n`;
    body += 'Content-Disposition: form-data; name="metadata"\r\n';
    body += 'Content-Type: application/json\r\n\r\n';
    body += JSON.stringify(meta) + '\r\n';

    // 脚本部分
    body += `--${boundary}\r\n`;
    if (isEsModule) {
      body += 'Content-Disposition: form-data; name="worker.js"; filename="worker.js"\r\n';
      body += 'Content-Type: application/javascript+module\r\n\r\n';
    } else {
      body += 'Content-Disposition: form-data; name="script"; filename="script.js"\r\n';
      body += 'Content-Type: application/javascript\r\n\r\n';
    }
    body += scriptContent + '\r\n';
    body += `--${boundary}--\r\n`;

    const headers = {
      'Content-Type': `multipart/form-data; boundary=${boundary}`,
      'Content-Length': Buffer.byteLength(body),
    };

    // 认证处理
    if (typeof apiToken === 'string') {
      headers['Authorization'] = `Bearer ${apiToken}`;
    } else if (apiToken && apiToken.email && apiToken.key) {
      headers['X-Auth-Email'] = apiToken.email;
      headers['X-Auth-Key'] = apiToken.key;
    }

    const options = {
      hostname: CF_API_BASE,
      path: `/client/v4/accounts/${accountId}/workers/scripts/${scriptName}`,
      method: 'PUT',
      headers: headers,
    };

    const req = https.request(options, res => {
      let data = '';
      res.on('data', chunk => (data += chunk));
      res.on('end', () => {
        try {
          const json = JSON.parse(data);
          if (json.success) {
            resolve(json.result);
          } else {
            const errors = json.errors || [];
            reject(new Error(errors.map(e => e.message).join(', ') || 'Unknown error'));
          }
        } catch (e) {
          reject(new Error('Invalid response'));
        }
      });
    });

    req.on('error', reject);
    req.write(body);
    req.end();
  });
}

/**
 * 删除 Worker 脚本
 */
async function deleteWorkerScript(apiToken, accountId, scriptName) {
  await cfRequest(apiToken, 'DELETE', `/accounts/${accountId}/workers/scripts/${scriptName}`);
  return true;
}

/**
 * 获取 Worker 子域名
 */
async function getWorkersSubdomain(apiToken, accountId) {
  try {
    const result = await cfRequest(apiToken, 'GET', `/accounts/${accountId}/workers/subdomain`);
    return result.result;
  } catch (e) {
    return null; // 可能没有启用子域名
  }
}

/**
 * 启用/禁用 Worker
 */
async function setWorkerEnabled(apiToken, accountId, scriptName, enabled) {
  // Workers 没有直接的启用/禁用 API，通过设置子域访问来模拟
  const body = { enabled };
  const result = await cfRequest(
    apiToken,
    'POST',
    `/accounts/${accountId}/workers/scripts/${scriptName}/subdomain`,
    body
  );
  return result.result;
}

/**
 * 获取 Worker 路由列表
 * @param {string} apiToken
 * @param {string} zoneId
 */
async function listWorkerRoutes(apiToken, zoneId) {
  const result = await cfRequest(apiToken, 'GET', `/zones/${zoneId}/workers/routes`);
  return result.result || [];
}

/**
 * 创建 Worker 路由
 * @param {string} apiToken
 * @param {string} zoneId
 * @param {string} pattern - 路由模式，如 "example.com/*"
 * @param {string} scriptName - Worker 脚本名称
 */
async function createWorkerRoute(apiToken, zoneId, pattern, scriptName) {
  const body = { pattern, script: scriptName };
  const result = await cfRequest(apiToken, 'POST', `/zones/${zoneId}/workers/routes`, body);
  return result.result;
}

/**
 * 更新 Worker 路由
 */
async function updateWorkerRoute(apiToken, zoneId, routeId, pattern, scriptName) {
  const body = { pattern, script: scriptName };
  const result = await cfRequest(
    apiToken,
    'PUT',
    `/zones/${zoneId}/workers/routes/${routeId}`,
    body
  );
  return result.result;
}

/**
 * 删除 Worker 路由
 */
async function deleteWorkerRoute(apiToken, zoneId, routeId) {
  await cfRequest(apiToken, 'DELETE', `/zones/${zoneId}/workers/routes/${routeId}`);
  return true;
}

/**
 * 获取 Worker 使用统计
 */
async function getWorkerAnalytics(apiToken, accountId, scriptName, since = null) {
  const params = new URLSearchParams();
  if (since) params.append('since', since);

  const query = params.toString();
  const path =
    `/accounts/${accountId}/workers/scripts/${scriptName}/analytics` + (query ? `?${query}` : '');

  try {
    const result = await cfRequest(apiToken, 'GET', path);
    return result.result;
  } catch (e) {
    return null; // Analytics 可能不可用
  }
}

/**
 * 列出 Worker 的自定义域名
 * @param {string} apiToken
 * @param {string} accountId
 * @param {string} scriptName
 */
async function listWorkerDomains(apiToken, accountId, scriptName) {
  // Workers Custom Domains API
  // GET /accounts/:account_id/workers/domains
  // 需要筛选出属于特定 worker 的域名
  try {
    const result = await cfRequest(apiToken, 'GET', `/accounts/${accountId}/workers/domains`);
    const allDomains = result.result || [];
    // 过滤出属于当前 worker 的域名
    return allDomains.filter(d => d.service === scriptName);
  } catch (e) {
    // 如果 API 不可用，返回空数组
    return [];
  }
}

/**
 * 添加 Worker 自定义域名
 * @param {string} apiToken
 * @param {string} accountId
 * @param {string} scriptName
 * @param {string} hostname
 * @param {string} environment - 环境，默认 production
 */
async function addWorkerDomain(
  apiToken,
  accountId,
  scriptName,
  hostname,
  environment = 'production'
) {
  const body = {
    hostname,
    service: scriptName,
    environment,
    zone_id: null, // 将由 API 自动查找
  };

  // 首先需要找到域名对应的 zone_id
  // 从 hostname 提取根域名并查找对应的 zone
  const parts = hostname.split('.');
  const possibleZones = [];
  for (let i = 0; i < parts.length - 1; i++) {
    possibleZones.push(parts.slice(i).join('.'));
  }

  // 查找匹配的 zone
  for (const zoneName of possibleZones) {
    try {
      const { zones } = await listZones(apiToken, { name: zoneName });
      if (zones && zones.length > 0) {
        body.zone_id = zones[0].id;
        break;
      }
    } catch (e) {
      // 继续尝试下一个
    }
  }

  if (!body.zone_id) {
    throw new Error(`未找到域名 ${hostname} 对应的 Zone，请确保该域名已在 Cloudflare DNS 中托管`);
  }

  const result = await cfRequest(apiToken, 'PUT', `/accounts/${accountId}/workers/domains`, body);
  return result.result;
}

/**
 * 删除 Worker 自定义域名
 * @param {string} apiToken
 * @param {string} accountId
 * @param {string} domainId
 */
async function deleteWorkerDomain(apiToken, accountId, domainId) {
  await cfRequest(apiToken, 'DELETE', `/accounts/${accountId}/workers/domains/${domainId}`);
  return true;
}

// ==================== Pages 管理 ====================

/**
 * 列出所有 Pages 项目
 * @param {string} apiToken
 * @param {string} accountId
 */
async function listPagesProjects(apiToken, accountId) {
  const result = await cfRequest(apiToken, 'GET', `/accounts/${accountId}/pages/projects`);
  return result.result || [];
}

/**
 * 获取 Pages 项目详情
 * @param {string} apiToken
 * @param {string} accountId
 * @param {string} projectName
 */
async function getPagesProject(apiToken, accountId, projectName) {
  const result = await cfRequest(
    apiToken,
    'GET',
    `/accounts/${accountId}/pages/projects/${projectName}`
  );
  return result.result;
}

/**
 * 获取 Pages 项目的部署列表
 * @param {string} apiToken
 * @param {string} accountId
 * @param {string} projectName
 */
async function listPagesDeployments(apiToken, accountId, projectName) {
  const result = await cfRequest(
    apiToken,
    'GET',
    `/accounts/${accountId}/pages/projects/${projectName}/deployments?per_page=20`
  );
  return result.result || [];
}

/**
 * 删除 Pages 部署
 * @param {string} apiToken
 * @param {string} accountId
 * @param {string} projectName
 * @param {string} deploymentId
 */
async function deletePagesDeployment(apiToken, accountId, projectName, deploymentId) {
  return await cfRequest(
    apiToken,
    'DELETE',
    `/accounts/${accountId}/pages/projects/${projectName}/deployments/${deploymentId}`
  );
}

/**
 * 重新部署 Pages 项目（使用最新的部署配置）
 * @param {string} apiToken
 * @param {string} accountId
 * @param {string} projectName
 */
async function retryPagesDeployment(apiToken, accountId, projectName, deploymentId) {
  return await cfRequest(
    apiToken,
    'POST',
    `/accounts/${accountId}/pages/projects/${projectName}/deployments/${deploymentId}/retry`
  );
}

/**
 * 获取 Pages 项目的自定义域名列表
 * @param {string} apiToken
 * @param {string} accountId
 * @param {string} projectName
 */
async function listPagesDomains(apiToken, accountId, projectName) {
  const result = await cfRequest(
    apiToken,
    'GET',
    `/accounts/${accountId}/pages/projects/${projectName}/domains`
  );
  return result.result || [];
}

/**
 * 添加 Pages 自定义域名
 * @param {string} apiToken
 * @param {string} accountId
 * @param {string} projectName
 * @param {string} domain
 */
async function addPagesDomain(apiToken, accountId, projectName, domain) {
  return await cfRequest(
    apiToken,
    'POST',
    `/accounts/${accountId}/pages/projects/${projectName}/domains`,
    { name: domain }
  );
}

/**
 * 删除 Pages 自定义域名
 * @param {string} apiToken
 * @param {string} accountId
 * @param {string} projectName
 * @param {string} domain
 */
async function deletePagesDomain(apiToken, accountId, projectName, domain) {
  return await cfRequest(
    apiToken,
    'DELETE',
    `/accounts/${accountId}/pages/projects/${projectName}/domains/${domain}`
  );
}

/**
 * 删除 Pages 项目
 * @param {string} apiToken
 * @param {string} accountId
 * @param {string} projectName
 */
async function deletePagesProject(apiToken, accountId, projectName) {
  return await cfRequest(
    apiToken,
    'DELETE',
    `/accounts/${accountId}/pages/projects/${projectName}`
  );
}

// ==================== R2 存储管理 ====================

/**
 * 获取 R2 存储桶列表
 */
async function listR2Buckets(auth, accountId) {
  try {
    const result = await cfRequest(auth, 'GET', `/accounts/${accountId}/r2/buckets`);
    return result.result?.buckets || [];
  } catch (e) {
    console.error('[CF-API] 获取 R2 存储桶失败:', e.message);
    throw e;
  }
}

/**
 * 获取 R2 存储桶详情
 */
async function getR2Bucket(auth, accountId, bucketName) {
  try {
    const result = await cfRequest(auth, 'GET', `/accounts/${accountId}/r2/buckets/${bucketName}`);
    console.log('[CF-API] R2 Bucket 详情:', JSON.stringify(result, null, 2));
    return result.result;
  } catch (e) {
    console.error('[CF-API] 获取 R2 存储桶详情失败:', e.message);
    throw e;
  }
}

/**
 * 创建 R2 存储桶
 */
async function createR2Bucket(auth, accountId, name, location = 'auto') {
  try {
    const body = { name };
    if (location && location !== 'auto') {
      body.location = location;
    }
    const result = await cfRequest(auth, 'POST', `/accounts/${accountId}/r2/buckets`, body);
    return result.result;
  } catch (e) {
    console.error('[CF-API] 创建 R2 存储桶失败:', e.message);
    throw e;
  }
}

/**
 * 删除 R2 存储桶
 */
async function deleteR2Bucket(auth, accountId, bucketName) {
  try {
    await cfRequest(auth, 'DELETE', `/accounts/${accountId}/r2/buckets/${bucketName}`);
    return true;
  } catch (e) {
    console.error('[CF-API] 删除 R2 存储桶失败:', e.message);
    throw e;
  }
}

/**
 * 列出 R2 存储桶内的对象
 */
async function listR2Objects(auth, accountId, bucketName, options = {}) {
  try {
    const { prefix, cursor, limit, delimiter } = options;
    let path = `/accounts/${accountId}/r2/buckets/${bucketName}/objects?`;

    const params = [];
    if (prefix) params.push(`prefix=${encodeURIComponent(prefix)}`);
    if (cursor) params.push(`cursor=${encodeURIComponent(cursor)}`);
    if (limit) params.push(`limit=${limit}`);
    if (delimiter) params.push(`delimiter=${encodeURIComponent(delimiter)}`);

    if (params.length > 0) {
      path += params.join('&');
    }

    const result = await cfRequest(auth, 'GET', path);

    // Cloudflare R2 API 返回格式:
    // - result: 直接是对象数组
    // - result_info.delimited: 文件夹前缀数组
    return {
      objects: result.result || [],
      delimited_prefixes: result.result_info?.delimited || [],
      cursor: result.result_info?.cursor || null,
    };
  } catch (e) {
    console.error('[CF-API] 列出 R2 对象失败:', e.message);
    throw e;
  }
}

/**
 * 删除 R2 对象
 */
async function deleteR2Object(auth, accountId, bucketName, objectKey) {
  try {
    await cfRequest(
      auth,
      'DELETE',
      `/accounts/${accountId}/r2/buckets/${bucketName}/objects/${encodeURIComponent(objectKey)}`
    );
    return true;
  } catch (e) {
    console.error('[CF-API] 删除 R2 对象失败:', e.message);
    throw e;
  }
}

module.exports = {
  // 验证
  verifyToken,
  getUserInfo,

  // Zone 管理
  listZones,
  getZone,
  createZone,
  deleteZone,

  // DNS 记录
  listDnsRecords,
  getDnsRecord,
  createDnsRecord,
  updateDnsRecord,
  deleteDnsRecord,
  batchCreateDnsRecords,
  switchDnsContent,
  exportDnsRecords,
  purgeCache, // 缓存管理

  // SSL/TLS 管理
  getSslSettings,
  updateSslMode,
  getSslCertificates,
  getSslVerification,

  // Analytics 分析
  getZoneAnalytics,
  getSimpleAnalytics,

  // 实用函数
  getSupportedRecordTypes,
  validateDnsRecord,

  // Workers 管理
  getAccountId,
  listWorkers,
  getWorkerScript,
  putWorkerScript,
  deleteWorkerScript,
  getWorkersSubdomain,
  setWorkerEnabled,
  listWorkerRoutes,
  createWorkerRoute,
  updateWorkerRoute,
  deleteWorkerRoute,
  getWorkerAnalytics,
  listWorkerDomains,
  addWorkerDomain,
  deleteWorkerDomain,

  // Pages 管理
  listPagesProjects,
  getPagesProject,
  listPagesDeployments,
  deletePagesDeployment,
  retryPagesDeployment,
  listPagesDomains,
  addPagesDomain,
  deletePagesDomain,
  deletePagesProject,

  // R2 管理
  listR2Buckets,
  getR2Bucket,
  createR2Bucket,
  deleteR2Bucket,
  listR2Objects,
  deleteR2Object,
};
