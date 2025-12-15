/**
 * Cloudflare DNS 管理 - 数据存储模块
 * 
 * 存储结构:
 * - accounts: Cloudflare 账号列表 (API Token)
 * - records: 常用 DNS 记录模板
 */

const fs = require('fs');
const path = require('path');

// 配置目录
const CONFIG_DIR = process.env.CONFIG_DIR || path.join(__dirname, '../../config');
const ACCOUNTS_FILE = path.join(CONFIG_DIR, 'cf-accounts.json');
const TEMPLATES_FILE = path.join(CONFIG_DIR, 'cf-dns-templates.json');

// 确保配置目录存在
function ensureConfigDir() {
  if (!fs.existsSync(CONFIG_DIR)) {
    fs.mkdirSync(CONFIG_DIR, { recursive: true });
  }
}

// ==================== 账号管理 ====================

/**
 * 获取所有 Cloudflare 账号
 */
function getAccounts() {
  try {
    ensureConfigDir();
    if (fs.existsSync(ACCOUNTS_FILE)) {
      const data = fs.readFileSync(ACCOUNTS_FILE, 'utf8');
      return JSON.parse(data);
    }
  } catch (e) {
    console.error('❌ 读取 CF 账号失败:', e.message);
  }
  return [];
}

/**
 * 保存账号列表
 */
function saveAccounts(accounts) {
  try {
    ensureConfigDir();
    fs.writeFileSync(ACCOUNTS_FILE, JSON.stringify(accounts, null, 2), 'utf8');
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
  const accounts = getAccounts();
  const id = 'cf_' + Date.now() + '_' + Math.random().toString(36).substr(2, 9);
  const newAccount = {
    id,
    name: account.name,
    apiToken: account.apiToken,
    email: account.email || '',
    createdAt: new Date().toISOString(),
    lastUsed: null
  };
  accounts.push(newAccount);
  saveAccounts(accounts);
  return newAccount;
}

/**
 * 更新账号
 */
function updateAccount(id, updates) {
  const accounts = getAccounts();
  const index = accounts.findIndex(a => a.id === id);
  if (index === -1) return null;
  
  // 只更新允许的字段
  if (updates.name) accounts[index].name = updates.name;
  if (updates.apiToken) accounts[index].apiToken = updates.apiToken;
  if (updates.email !== undefined) accounts[index].email = updates.email;
  
  saveAccounts(accounts);
  return accounts[index];
}

/**
 * 删除账号
 */
function deleteAccount(id) {
  const accounts = getAccounts();
  const index = accounts.findIndex(a => a.id === id);
  if (index === -1) return false;
  
  accounts.splice(index, 1);
  saveAccounts(accounts);
  return true;
}

/**
 * 获取单个账号
 */
function getAccountById(id) {
  const accounts = getAccounts();
  return accounts.find(a => a.id === id) || null;
}

/**
 * 更新账号最后使用时间
 */
function touchAccount(id) {
  const accounts = getAccounts();
  const account = accounts.find(a => a.id === id);
  if (account) {
    account.lastUsed = new Date().toISOString();
    saveAccounts(accounts);
  }
}

// ==================== DNS 记录模板 ====================

/**
 * 获取 DNS 记录模板
 */
function getTemplates() {
  try {
    ensureConfigDir();
    if (fs.existsSync(TEMPLATES_FILE)) {
      const data = fs.readFileSync(TEMPLATES_FILE, 'utf8');
      return JSON.parse(data);
    }
  } catch (e) {
    console.error('❌ 读取 DNS 模板失败:', e.message);
  }
  return [];
}

/**
 * 保存 DNS 记录模板
 */
function saveTemplates(templates) {
  try {
    ensureConfigDir();
    fs.writeFileSync(TEMPLATES_FILE, JSON.stringify(templates, null, 2), 'utf8');
    return true;
  } catch (e) {
    console.error('❌ 保存 DNS 模板失败:', e.message);
    return false;
  }
}

/**
 * 添加 DNS 模板
 * @param {Object} template - { name, type, content, proxied, ttl, priority?, description? }
 */
function addTemplate(template) {
  const templates = getTemplates();
  const id = 'tpl_' + Date.now() + '_' + Math.random().toString(36).substr(2, 9);
  const newTemplate = {
    id,
    name: template.name,
    type: template.type || 'A',
    content: template.content,
    proxied: template.proxied !== false,
    ttl: template.ttl || 1, // 1 = auto
    priority: template.priority || null, // for MX records
    description: template.description || '',
    createdAt: new Date().toISOString()
  };
  templates.push(newTemplate);
  saveTemplates(templates);
  return newTemplate;
}

/**
 * 更新 DNS 模板
 */
function updateTemplate(id, updates) {
  const templates = getTemplates();
  const index = templates.findIndex(t => t.id === id);
  if (index === -1) return null;
  
  const allowed = ['name', 'type', 'content', 'proxied', 'ttl', 'priority', 'description'];
  allowed.forEach(key => {
    if (updates[key] !== undefined) {
      templates[index][key] = updates[key];
    }
  });
  
  saveTemplates(templates);
  return templates[index];
}

/**
 * 删除 DNS 模板
 */
function deleteTemplate(id) {
  const templates = getTemplates();
  const index = templates.findIndex(t => t.id === id);
  if (index === -1) return false;
  
  templates.splice(index, 1);
  saveTemplates(templates);
  return true;
}

module.exports = {
  // 账号管理
  getAccounts,
  addAccount,
  updateAccount,
  deleteAccount,
  getAccountById,
  touchAccount,
  
  // DNS 模板
  getTemplates,
  addTemplate,
  updateTemplate,
  deleteTemplate
};