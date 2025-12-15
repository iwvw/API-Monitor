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
  validateDnsRecord
};