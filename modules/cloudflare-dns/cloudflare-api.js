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
 */
function cfRequest(apiToken, method, path, body = null) {
  return new Promise((resolve, reject) => {
    const options = {
      hostname: CF_API_BASE,
      path: `/client/v4${path}`,
      method: method,
      headers: {
        'Authorization': `Bearer ${apiToken}`,
        'Content-Type': 'application/json'
      },
      timeout: 15000
    };

    const req = https.request(options, (res) => {
      let data = '';
      res.on('data', chunk => data += chunk);
      res.on('end', () => {
        try {
          const json = JSON.parse(data);
          if (json.success) {
            resolve(json);
          } else {
            const errors = json.errors || [];
            const errorMsg = errors.map(e => e.message).join(', ') || 'Unknown error';
            reject(new Error(errorMsg));
          }
        } catch (e) {
          reject(new Error('Invalid JSON response'));
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
  });
}

// ==================== 账号验证 ====================

/**
 * 验证 API Token 是否有效
 */
async function verifyToken(apiToken) {
  try {
    const result = await cfRequest(apiToken, 'GET', '/user/tokens/verify');
    return {
      valid: true,
      status: result.result?.status,
      expiresOn: result.result?.expires_on
    };
  } catch (e) {
    return {
      valid: false,
      error: e.message
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
    resultInfo: result.result_info
  };
}

/**
 * 获取单个 Zone 信息
 */
async function getZone(apiToken, zoneId) {
  const result = await cfRequest(apiToken, 'GET', `/zones/${zoneId}`);
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
    resultInfo: result.result_info
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
    proxied: record.proxied !== undefined ? record.proxied : true
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

  const result = await cfRequest(apiToken, 'PATCH', `/zones/${zoneId}/dns_records/${recordId}`, body);
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
      content: newContent
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
    const ipv6Regex = /^([0-9a-fA-F]{1,4}:){7}[0-9a-fA-F]{1,4}$|^::$|^([0-9a-fA-F]{1,4}:)*::([0-9a-fA-F]{1,4}:)*[0-9a-fA-F]{1,4}$/;
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
    errors
  };
}

// ==================== Workers 管理 ====================

/**
 * 获取账号 ID (Workers API 需要)
 */
async function getAccountId(apiToken) {
  const result = await cfRequest(apiToken, 'GET', '/accounts?page=1&per_page=1');
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
    const options = {
      hostname: CF_API_BASE,
      path: `/client/v4/accounts/${accountId}/workers/scripts/${scriptName}`,
      method: 'GET',
      headers: {
        'Authorization': `Bearer ${apiToken}`,
        'Accept': 'application/javascript'  // 请求纯 JavaScript 格式
      }
    };

    const req = https.request(options, (res) => {
      let data = '';
      res.on('data', chunk => data += chunk);
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
              if (part.includes('application/javascript') ||
                (part.includes('filename=') && part.includes('.js'))) {
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
          meta: null
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
    const isEsModule = scriptContent.includes('export default') ||
      scriptContent.includes('export {') ||
      scriptContent.includes('export async');

    // 构建 multipart 数据
    let body = '';

    // 元数据部分
    const meta = {
      bindings: metadata.bindings || [],
      compatibility_date: metadata.compatibility_date || new Date().toISOString().split('T')[0]
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

    const options = {
      hostname: CF_API_BASE,
      path: `/client/v4/accounts/${accountId}/workers/scripts/${scriptName}`,
      method: 'PUT',
      headers: {
        'Authorization': `Bearer ${apiToken}`,
        'Content-Type': `multipart/form-data; boundary=${boundary}`,
        'Content-Length': Buffer.byteLength(body)
      }
    };

    const req = https.request(options, (res) => {
      let data = '';
      res.on('data', chunk => data += chunk);
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
  const result = await cfRequest(apiToken, 'PUT', `/zones/${zoneId}/workers/routes/${routeId}`, body);
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
  const path = `/accounts/${accountId}/workers/scripts/${scriptName}/analytics` + (query ? `?${query}` : '');

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
async function addWorkerDomain(apiToken, accountId, scriptName, hostname, environment = 'production') {
  const body = {
    hostname,
    service: scriptName,
    environment,
    zone_id: null // 将由 API 自动查找
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
  const result = await cfRequest(apiToken, 'GET', `/accounts/${accountId}/pages/projects/${projectName}`);
  return result.result;
}

/**
 * 获取 Pages 项目的部署列表
 * @param {string} apiToken
 * @param {string} accountId
 * @param {string} projectName
 */
async function listPagesDeployments(apiToken, accountId, projectName) {
  const result = await cfRequest(apiToken, 'GET', `/accounts/${accountId}/pages/projects/${projectName}/deployments?per_page=20`);
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
  return await cfRequest(apiToken, 'DELETE', `/accounts/${accountId}/pages/projects/${projectName}/deployments/${deploymentId}`);
}

/**
 * 重新部署 Pages 项目（使用最新的部署配置）
 * @param {string} apiToken
 * @param {string} accountId
 * @param {string} projectName
 */
async function retryPagesDeployment(apiToken, accountId, projectName, deploymentId) {
  return await cfRequest(apiToken, 'POST', `/accounts/${accountId}/pages/projects/${projectName}/deployments/${deploymentId}/retry`);
}

/**
 * 获取 Pages 项目的自定义域名列表
 * @param {string} apiToken
 * @param {string} accountId
 * @param {string} projectName
 */
async function listPagesDomains(apiToken, accountId, projectName) {
  const result = await cfRequest(apiToken, 'GET', `/accounts/${accountId}/pages/projects/${projectName}/domains`);
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
  return await cfRequest(apiToken, 'POST', `/accounts/${accountId}/pages/projects/${projectName}/domains`, { name: domain });
}

/**
 * 删除 Pages 自定义域名
 * @param {string} apiToken
 * @param {string} accountId
 * @param {string} projectName
 * @param {string} domain
 */
async function deletePagesDomain(apiToken, accountId, projectName, domain) {
  return await cfRequest(apiToken, 'DELETE', `/accounts/${accountId}/pages/projects/${projectName}/domains/${domain}`);
}

/**
 * 删除 Pages 项目
 * @param {string} apiToken
 * @param {string} accountId
 * @param {string} projectName
 */
async function deletePagesProject(apiToken, accountId, projectName) {
  return await cfRequest(apiToken, 'DELETE', `/accounts/${accountId}/pages/projects/${projectName}`);
}

module.exports = {
  // 验证
  verifyToken,
  getUserInfo,

  // Zone 管理
  listZones,
  getZone,

  // DNS 记录
  listDnsRecords,
  getDnsRecord,
  createDnsRecord,
  updateDnsRecord,
  deleteDnsRecord,
  batchCreateDnsRecords,
  switchDnsContent,
  exportDnsRecords,

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
  deletePagesProject
};
