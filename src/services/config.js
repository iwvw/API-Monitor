/**
 * 配置管理服务（使用 SQLite 数据库）
 */

const path = require('path');
const { SystemConfig, ZeaburAccount } = require('../db/models');
const dbService = require('../db/database');

// 初始化数据库
dbService.initialize();

// 配置目录（保留用于兼容性）
const CONFIG_DIR = process.env.CONFIG_DIR || path.join(__dirname, '../../config');
const ACCOUNTS_FILE = path.join(CONFIG_DIR, 'accounts.json');
const PASSWORD_FILE = path.join(CONFIG_DIR, 'password.json');

/**
 * 确保配置目录存在（保留用于兼容性）
 */
function ensureConfigDir() {
  // 数据库模式下不再需要，但保留函数以保持兼容性
}

/**
 * 读取主机存储的账号（从数据库）
 */
function loadServerAccounts() {
  try {
    const accounts = ZeaburAccount.findAll();
    // 转换字段名以保持向后兼容
    return accounts.map(acc => ({
      id: acc.id,
      name: acc.name,
      token: acc.token,
      status: acc.status,
      email: acc.email,
      username: acc.username,
      balance: acc.balance,
      cost: acc.cost,
      createdAt: acc.created_at,
      updatedAt: acc.updated_at
    }));
  } catch (e) {
    console.error('❌ 读取账号失败:', e.message);
    return [];
  }
}

/**
 * 保存账号到数据库
 */
function saveServerAccounts(accounts) {
  try {
    // 注意：这个函数会完全替换所有账号
    // 在实际使用中，建议使用更细粒度的操作（添加、更新、删除单个账号）
    const db = dbService.getDatabase();

    const transaction = db.transaction(() => {
      // 清空现有账号
      ZeaburAccount.truncate();

      // 插入新账号
      accounts.forEach(account => {
        ZeaburAccount.createAccount({
          id: account.id,
          name: account.name,
          token: account.token,
          status: account.status || 'active',
          email: account.email,
          username: account.username,
          balance: account.balance || 0,
          cost: account.cost || 0,
          created_at: account.createdAt || new Date().toISOString()
        });
      });
    });

    transaction();
    return true;
  } catch (e) {
    console.error('❌ 保存账号失败:', e.message);
    return false;
  }
}

/**
 * 读取管理员密码（优先环境变量，其次数据库）
 */
function loadAdminPassword() {
  if (process.env.ADMIN_PASSWORD) {
    return process.env.ADMIN_PASSWORD;
  }

  try {
    const password = SystemConfig.getConfigValue('admin_password');
    return password;
  } catch (e) {
    console.error('❌ 读取密码失败:', e.message);
    return null;
  }
}

/**
 * 检查密码是否已在数据库中设置
 */
function isPasswordSavedToFile() {
  try {
    const password = SystemConfig.getConfigValue('admin_password');
    return !!password;
  } catch (e) {
    return false;
  }
}

/**
 * 保存管理员密码到数据库
 */
function saveAdminPassword(password) {
  try {
    SystemConfig.setConfig('admin_password', password, '管理员密码');
    return true;
  } catch (e) {
    console.error('❌ 保存密码失败:', e.message);
    return false;
  }
}

/**
 * 从环境变量读取预配置的账号
 */
function getEnvAccounts() {
  const accountsEnv = process.env.ACCOUNTS;
  if (!accountsEnv) return [];

  try {
    // 格式: "账号1名称:token1,账号2名称:token2"
    return accountsEnv.split(',').map(item => {
      const [name, token] = item.split(':');
      return { name: name.trim(), token: token.trim() };
    }).filter(acc => acc.name && acc.token);
  } catch (e) {
    console.error('❌ 解析环境变量 ACCOUNTS 失败:', e.message);
    return [];
  }
}

module.exports = {
  CONFIG_DIR,
  ACCOUNTS_FILE,
  PASSWORD_FILE,
  loadServerAccounts,
  saveServerAccounts,
  loadAdminPassword,
  isPasswordSavedToFile,
  saveAdminPassword,
  getEnvAccounts,
  ensureConfigDir
};
