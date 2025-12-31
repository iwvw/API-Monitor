/**
 * Cloudflare DNS 管理 - 数据存储模块（使用 SQLite 数据库）
 *
 * 存储结构:
 * - accounts: Cloudflare 账号列表 (API Token)
 * - templates: 常用 DNS 记录模板
 */

const { CloudflareAccount, CloudflareDnsTemplate } = require('../../src/db/models');
const dbService = require('../../src/db/database');

// 初始化数据库
dbService.initialize();

// ==================== 账号管理 ====================

/**
 * 获取所有 Cloudflare 账号
 */
function getAccounts() {
  try {
    const accounts = CloudflareAccount.findAll();
    // 转换字段名以保持向后兼容
    return accounts.map(acc => ({
      id: acc.id,
      name: acc.name,
      apiToken: acc.api_token,
      email: acc.email || '',
      createdAt: acc.created_at,
      lastUsed: acc.last_used,
    }));
  } catch (e) {
    console.error('❌ 读取 CF 账号失败:', e.message);
    return [];
  }
}

/**
 * 保存账号列表
 */
function saveAccounts(accounts) {
  try {
    const db = dbService.getDatabase();

    const transaction = db.transaction(() => {
      // 清空现有账号
      CloudflareAccount.truncate();

      // 插入新账号
      accounts.forEach(account => {
        CloudflareAccount.createAccount({
          id: account.id,
          name: account.name,
          apiToken: account.apiToken,
          email: account.email || '',
          createdAt: account.createdAt,
          lastUsed: account.lastUsed,
        });
      });
    });

    transaction();
    return true;
  } catch (e) {
    console.error('❌ 保存 CF 账号失败:', e.message);
    return false;
  }
}

/**
 * 添加账号
 * @param {Object} account - { name, apiToken, email? }
 */
function addAccount(account) {
  const id = 'cf_' + Date.now() + '_' + Math.random().toString(36).substr(2, 9);
  const newAccount = {
    id,
    name: account.name,
    apiToken: account.apiToken,
    email: account.email || '',
    createdAt: new Date().toISOString(),
    lastUsed: null,
  };

  CloudflareAccount.createAccount(newAccount);
  return newAccount;
}

/**
 * 更新账号
 */
function updateAccount(id, updates) {
  try {
    const account = CloudflareAccount.findById(id);
    if (!account) return null;

    const updateData = {};
    if (updates.name) updateData.name = updates.name;
    if (updates.apiToken) updateData.api_token = updates.apiToken;
    if (updates.email !== undefined) updateData.email = updates.email;

    CloudflareAccount.updateAccount(id, updateData);

    // 返回更新后的账号
    const updated = CloudflareAccount.findById(id);
    return {
      id: updated.id,
      name: updated.name,
      apiToken: updated.api_token,
      email: updated.email,
      createdAt: updated.created_at,
      lastUsed: updated.last_used,
    };
  } catch (e) {
    console.error('❌ 更新 CF 账号失败:', e.message);
    return null;
  }
}

/**
 * 删除账号
 */
function deleteAccount(id) {
  try {
    return CloudflareAccount.delete(id);
  } catch (e) {
    console.error('❌ 删除 CF 账号失败:', e.message);
    return false;
  }
}

/**
 * 获取单个账号
 */
function getAccountById(id) {
  try {
    const account = CloudflareAccount.findById(id);
    if (!account) return null;

    return {
      id: account.id,
      name: account.name,
      apiToken: account.api_token,
      email: account.email,
      createdAt: account.created_at,
      lastUsed: account.last_used,
    };
  } catch (e) {
    console.error('❌ 获取 CF 账号失败:', e.message);
    return null;
  }
}

/**
 * 更新账号最后使用时间
 */
function touchAccount(id) {
  try {
    CloudflareAccount.updateLastUsed(id);
  } catch (e) {
    // 静默失败，不输出日志
  }
}

// ==================== DNS 记录模板 ====================

/**
 * 获取 DNS 记录模板
 */
function getTemplates() {
  try {
    const templates = CloudflareDnsTemplate.getAllTemplates();
    // 转换字段名以保持向后兼容
    return templates.map(tpl => ({
      id: tpl.id,
      name: tpl.name,
      description: tpl.description || '',
      records: tpl.records, // 已经被解析为数组
      createdAt: tpl.created_at,
      updatedAt: tpl.updated_at,
    }));
  } catch (e) {
    console.error('❌ 读取 DNS 模板失败:', e.message);
    return [];
  }
}

/**
 * 保存 DNS 记录模板
 */
function saveTemplates(templates) {
  try {
    const db = dbService.getDatabase();

    const transaction = db.transaction(() => {
      // 清空现有模板
      CloudflareDnsTemplate.truncate();

      // 插入新模板
      templates.forEach(template => {
        CloudflareDnsTemplate.createTemplate({
          id: template.id,
          name: template.name,
          description: template.description || '',
          records: template.records,
          created_at: template.createdAt,
        });
      });
    });

    transaction();
    return true;
  } catch (e) {
    console.error('❌ 保存 DNS 模板失败:', e.message);
    return false;
  }
}

/**
 * 添加 DNS 模板
 * @param {Object} template - { name, description?, records }
 */
function addTemplate(template) {
  const id = 'tpl_' + Date.now() + '_' + Math.random().toString(36).substr(2, 9);
  const newTemplate = {
    id,
    name: template.name,
    description: template.description || '',
    records: template.records || [],
    createdAt: new Date().toISOString(),
  };

  CloudflareDnsTemplate.createTemplate(newTemplate);
  return newTemplate;
}

/**
 * 更新 DNS 模板
 */
function updateTemplate(id, updates) {
  try {
    const template = CloudflareDnsTemplate.getTemplate(id);
    if (!template) return null;

    const updateData = {};
    if (updates.name !== undefined) updateData.name = updates.name;
    if (updates.description !== undefined) updateData.description = updates.description;
    if (updates.records !== undefined) updateData.records = updates.records;

    CloudflareDnsTemplate.updateTemplate(id, updateData);

    // 返回更新后的模板
    const updated = CloudflareDnsTemplate.getTemplate(id);
    return {
      id: updated.id,
      name: updated.name,
      description: updated.description,
      records: updated.records,
      createdAt: updated.created_at,
      updatedAt: updated.updated_at,
    };
  } catch (e) {
    console.error('❌ 更新 DNS 模板失败:', e.message);
    return null;
  }
}

/**
 * 删除 DNS 模板
 */
function deleteTemplate(id) {
  try {
    return CloudflareDnsTemplate.delete(id);
  } catch (e) {
    console.error('❌ 删除 DNS 模板失败:', e.message);
    return false;
  }
}

module.exports = {
  // 账号管理
  getAccounts,
  saveAccounts,
  addAccount,
  updateAccount,
  deleteAccount,
  getAccountById,
  touchAccount,

  // DNS 模板
  getTemplates,
  saveTemplates,
  addTemplate,
  updateTemplate,
  deleteTemplate,
};
