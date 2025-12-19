/**
 * Cloudflare DNS 管理 - API 路由
 */

const express = require('express');
const router = express.Router();
const storage = require('./storage');
const cfApi = require('./cloudflare-api');
const { createLogger } = require('../../src/utils/logger');

const logger = createLogger('CF-DNS');

// ==================== 账号管理 ====================

/**
 * 获取所有账号（隐藏 API Token）
 */
router.get('/accounts', (req, res) => {
  try {
    const accounts = storage.getAccounts();
    // 隐藏敏感信息
    const safeAccounts = accounts.map(a => ({
      id: a.id,
      name: a.name,
      email: a.email,
      createdAt: a.createdAt,
      lastUsed: a.lastUsed,
      hasToken: !!a.apiToken
    }));
    res.json(safeAccounts);
  } catch (e) {
    res.status(500).json({ error: e.message });
  }
});

/**
 * 导出所有账号（包含 API Token，用于备份）
 */
router.get('/accounts/export', (req, res) => {
  try {
    const accounts = storage.getAccounts();
    // 返回完整信息用于导出
    const exportAccounts = accounts.map(a => ({
      name: a.name,
      email: a.email,
      apiToken: a.apiToken
    }));
    res.json({
      success: true,
      accounts: exportAccounts
    });
  } catch (e) {
    res.status(500).json({ error: e.message });
  }
});

/**
 * 添加账号
 */
router.post('/accounts', async (req, res) => {
  try {
    const { name, apiToken, email, skipVerify } = req.body;

    if (!name || !apiToken) {
      return res.status(400).json({ error: '名称和 API Token 必填' });
    }

    // 验证 Token（除非明确跳过验证，用于数据导入）
    if (!skipVerify) {
      const verification = await cfApi.verifyToken(apiToken);
      if (!verification.valid) {
        return res.status(400).json({ error: `Token 无效: ${verification.error}` });
      }
    }

    const account = storage.addAccount({ name, apiToken, email });
    res.json({
      success: true,
      account: {
        id: account.id,
        name: account.name,
        email: account.email,
        createdAt: account.createdAt
      }
    });
  } catch (e) {
    res.status(500).json({ error: e.message });
  }
});

/**
 * 更新账号
 */
router.put('/accounts/:id', async (req, res) => {
  try {
    const { id } = req.params;
    const { name, apiToken, email } = req.body;

    // 如果更新 Token，先验证
    if (apiToken) {
      const verification = await cfApi.verifyToken(apiToken);
      if (!verification.valid) {
        return res.status(400).json({ error: `Token 无效: ${verification.error}` });
      }
    }

    const updated = storage.updateAccount(id, { name, apiToken, email });
    if (!updated) {
      return res.status(404).json({ error: '账号不存在' });
    }

    res.json({ success: true });
  } catch (e) {
    res.status(500).json({ error: e.message });
  }
});

/**
 * 删除账号
 */
router.delete('/accounts/:id', (req, res) => {
  try {
    const { id } = req.params;
    const deleted = storage.deleteAccount(id);
    if (!deleted) {
      return res.status(404).json({ error: '账号不存在' });
    }
    res.json({ success: true });
  } catch (e) {
    res.status(500).json({ error: e.message });
  }
});

/**
 * 验证账号 Token
 */
router.post('/accounts/:id/verify', async (req, res) => {
  try {
    const { id } = req.params;
    const account = storage.getAccountById(id);
    if (!account) {
      return res.status(404).json({ error: '账号不存在' });
    }

    const verification = await cfApi.verifyToken(account.apiToken);
    res.json(verification);
  } catch (e) {
    res.status(500).json({ error: e.message });
  }
});

/**
 * 获取账号的 API Token（用于显示）
 */
router.get('/accounts/:id/token', (req, res) => {
  try {
    const { id } = req.params;
    const account = storage.getAccountById(id);
    if (!account) {
      return res.status(404).json({ error: '账号不存在' });
    }

    res.json({
      success: true,
      apiToken: account.apiToken
    });
  } catch (e) {
    res.status(500).json({ error: e.message });
  }
});

// ==================== Zone 管理 ====================

/**
 * 获取账号下的所有域名
 */
router.get('/accounts/:id/zones', async (req, res) => {
  try {
    const { id } = req.params;
    const account = storage.getAccountById(id);
    if (!account) {
      return res.status(404).json({ error: '账号不存在' });
    }

    storage.touchAccount(id);
    const { zones, resultInfo } = await cfApi.listZones(account.apiToken);
    
    res.json({
      zones: zones.map(z => ({
        id: z.id,
        name: z.name,
        status: z.status,
        paused: z.paused,
        type: z.type,
        nameServers: z.name_servers,
        createdOn: z.created_on,
        modifiedOn: z.modified_on
      })),
      pagination: resultInfo
    });
  } catch (e) {
    logger.error(`获取域名列表失败 (ID: ${req.params.id}):`, e.message);
    res.status(500).json({ error: e.message });
  }
});

// ==================== DNS 记录管理 ====================

/**
 * 获取域名的 DNS 记录
 */
router.get('/accounts/:accountId/zones/:zoneId/records', async (req, res) => {
  try {
    const { accountId, zoneId } = req.params;
    const { type, name, page } = req.query;

    const account = storage.getAccountById(accountId);
    if (!account) {
      return res.status(404).json({ error: '账号不存在' });
    }

    storage.touchAccount(accountId);
    const { records, resultInfo } = await cfApi.listDnsRecords(
      account.apiToken,
      zoneId,
      { type, name, page }
    );

    res.json({
      records: records.map(r => ({
        id: r.id,
        type: r.type,
        name: r.name,
        content: r.content,
        proxied: r.proxied,
        ttl: r.ttl,
        priority: r.priority,
        createdOn: r.created_on,
        modifiedOn: r.modified_on
      })),
      pagination: resultInfo
    });
  } catch (e) {
    res.status(500).json({ error: e.message });
  }
});

/**
 * 创建 DNS 记录
 */
router.post('/accounts/:accountId/zones/:zoneId/records', async (req, res) => {
  try {
    const { accountId, zoneId } = req.params;
    const { type, name, content, ttl, proxied, priority } = req.body;

    const account = storage.getAccountById(accountId);
    if (!account) {
      return res.status(404).json({ error: '账号不存在' });
    }

    // 验证记录
    const validation = cfApi.validateDnsRecord({ type, name, content, priority });
    if (!validation.valid) {
      return res.status(400).json({ error: validation.errors.join(', ') });
    }

    storage.touchAccount(accountId);
    const record = await cfApi.createDnsRecord(
      account.apiToken,
      zoneId,
      { type, name, content, ttl, proxied, priority }
    );

    res.json({
      success: true,
      record: {
        id: record.id,
        type: record.type,
        name: record.name,
        content: record.content,
        proxied: record.proxied,
        ttl: record.ttl
      }
    });
  } catch (e) {
    res.status(500).json({ error: e.message });
  }
});

/**
 * 更新 DNS 记录
 */
router.put('/accounts/:accountId/zones/:zoneId/records/:recordId', async (req, res) => {
  try {
    const { accountId, zoneId, recordId } = req.params;
    const { type, name, content, ttl, proxied, priority } = req.body;

    const account = storage.getAccountById(accountId);
    if (!account) {
      return res.status(404).json({ error: '账号不存在' });
    }

    storage.touchAccount(accountId);
    const record = await cfApi.updateDnsRecord(
      account.apiToken,
      zoneId,
      recordId,
      { type, name, content, ttl, proxied, priority }
    );

    res.json({
      success: true,
      record: {
        id: record.id,
        type: record.type,
        name: record.name,
        content: record.content,
        proxied: record.proxied,
        ttl: record.ttl
      }
    });
  } catch (e) {
    res.status(500).json({ error: e.message });
  }
});

/**
 * 删除 DNS 记录
 */
router.delete('/accounts/:accountId/zones/:zoneId/records/:recordId', async (req, res) => {
  try {
    const { accountId, zoneId, recordId } = req.params;

    const account = storage.getAccountById(accountId);
    if (!account) {
      return res.status(404).json({ error: '账号不存在' });
    }

    storage.touchAccount(accountId);
    await cfApi.deleteDnsRecord(account.apiToken, zoneId, recordId);

    res.json({ success: true });
  } catch (e) {
    res.status(500).json({ error: e.message });
  }
});

/**
 * 快速切换 DNS 记录内容
 */
router.post('/accounts/:accountId/zones/:zoneId/switch', async (req, res) => {
  try {
    const { accountId, zoneId } = req.params;
    const { type, name, newContent } = req.body;

    if (!type || !name || !newContent) {
      return res.status(400).json({ error: 'type, name, newContent 必填' });
    }

    const account = storage.getAccountById(accountId);
    if (!account) {
      return res.status(404).json({ error: '账号不存在' });
    }

    storage.touchAccount(accountId);
    const updated = await cfApi.switchDnsContent(
      account.apiToken,
      zoneId,
      type,
      name,
      newContent
    );

    res.json({
      success: true,
      updated: updated.length,
      records: updated.map(r => ({
        id: r.id,
        name: r.name,
        content: r.content
      }))
    });
  } catch (e) {
    res.status(500).json({ error: e.message });
  }
});

/**
 * 批量创建 DNS 记录
 */
router.post('/accounts/:accountId/zones/:zoneId/batch', async (req, res) => {
  try {
    const { accountId, zoneId } = req.params;
    const { records } = req.body;

    if (!records || !Array.isArray(records)) {
      return res.status(400).json({ error: '需要提供 records 数组' });
    }

    const account = storage.getAccountById(accountId);
    if (!account) {
      return res.status(404).json({ error: '账号不存在' });
    }

    storage.touchAccount(accountId);
    const { results, errors } = await cfApi.batchCreateDnsRecords(
      account.apiToken,
      zoneId,
      records
    );

    res.json({
      success: errors.length === 0,
      created: results.length,
      failed: errors.length,
      results,
      errors
    });
  } catch (e) {
    res.status(500).json({ error: e.message });
  }
});

// ==================== DNS 模板管理 ====================

/**
 * 获取所有模板
 */
router.get('/templates', (req, res) => {
  try {
    const templates = storage.getTemplates();
    res.json(templates);
  } catch (e) {
    res.status(500).json({ error: e.message });
  }
});

/**
 * 添加模板
 */
router.post('/templates', (req, res) => {
  try {
    const { name, type, content, proxied, ttl, priority, description } = req.body;

    if (!name || !type || !content) {
      return res.status(400).json({ error: '名称、类型、内容必填' });
    }

    const template = storage.addTemplate({
      name, type, content, proxied, ttl, priority, description
    });

    res.json({ success: true, template });
  } catch (e) {
    res.status(500).json({ error: e.message });
  }
});

/**
 * 更新模板
 */
router.put('/templates/:id', (req, res) => {
  try {
    const { id } = req.params;
    const updated = storage.updateTemplate(id, req.body);

    if (!updated) {
      return res.status(404).json({ error: '模板不存在' });
    }

    res.json({ success: true, template: updated });
  } catch (e) {
    res.status(500).json({ error: e.message });
  }
});

/**
 * 删除模板
 */
router.delete('/templates/:id', (req, res) => {
  try {
    const { id } = req.params;
    const deleted = storage.deleteTemplate(id);

    if (!deleted) {
      return res.status(404).json({ error: '模板不存在' });
    }

    res.json({ success: true });
  } catch (e) {
    res.status(500).json({ error: e.message });
  }
});

/**
 * 应用模板到域名
 */
router.post('/templates/:templateId/apply', async (req, res) => {
  try {
    const { templateId } = req.params;
    const { accountId, zoneId, recordName } = req.body;

    if (!accountId || !zoneId || !recordName) {
      return res.status(400).json({ error: 'accountId, zoneId, recordName 必填' });
    }

    const account = storage.getAccountById(accountId);
    if (!account) {
      return res.status(404).json({ error: '账号不存在' });
    }

    const templates = storage.getTemplates();
    const template = templates.find(t => t.id === templateId);
    if (!template) {
      return res.status(404).json({ error: '模板不存在' });
    }

    storage.touchAccount(accountId);
    const record = await cfApi.createDnsRecord(
      account.apiToken,
      zoneId,
      {
        type: template.type,
        name: recordName,
        content: template.content,
        ttl: template.ttl,
        proxied: template.proxied,
        priority: template.priority
      }
    );

    res.json({
      success: true,
      record: {
        id: record.id,
        type: record.type,
        name: record.name,
        content: record.content
      }
    });
  } catch (e) {
    res.status(500).json({ error: e.message });
  }
});

// ==================== 实用功能 ====================

/**
 * 获取支持的记录类型
 */
router.get('/record-types', (req, res) => {
  res.json(cfApi.getSupportedRecordTypes());
});

/**
 * 导出账号（包含完整数据，用于备份）
 */
router.get('/export/accounts', (req, res) => {
  try {
    const accounts = storage.getAccounts();
    res.json(accounts);
  } catch (e) {
    res.status(500).json({ error: e.message });
  }
});

/**
 * 批量导入账号（直接覆盖数据库）
 */
router.post('/import/accounts', (req, res) => {
  try {
    const { accounts, overwrite } = req.body;

    if (!accounts || !Array.isArray(accounts)) {
      return res.status(400).json({ error: '需要提供 accounts 数组' });
    }

    if (overwrite) {
      // 直接覆盖所有账号
      storage.saveAccounts(accounts);
    } else {
      // 追加账号
      accounts.forEach(account => {
        storage.addAccount(account);
      });
    }

    res.json({ success: true, count: accounts.length });
  } catch (e) {
    res.status(500).json({ error: e.message });
  }
});

/**
 * 批量导入模板（直接覆盖数据库）
 */
router.post('/import/templates', (req, res) => {
  try {
    const { templates, overwrite } = req.body;

    if (!templates || !Array.isArray(templates)) {
      return res.status(400).json({ error: '需要提供 templates 数组' });
    }

    if (overwrite) {
      // 直接覆盖所有模板
      storage.saveTemplates(templates);
    } else {
      // 追加模板
      templates.forEach(template => {
        storage.addTemplate(template);
      });
    }

    res.json({ success: true, count: templates.length });
  } catch (e) {
    res.status(500).json({ error: e.message });
  }
});

module.exports = router;