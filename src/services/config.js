/**
 * 配置管理服务（使用 SQLite 数据库）
 */

const path = require('path');
const { SystemConfig } = require('../db/models');
const dbService = require('../db/database');
const { apiCache } = require('../utils/cache');
const { hashPasswordSync, isHashed } = require('../utils/password');

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
 * 读取管理员密码（优先环境变量，其次数据库）
 */
function loadAdminPassword() {
  if (process.env.ADMIN_PASSWORD) {
    return process.env.ADMIN_PASSWORD;
  }

  // 尝试从缓存获取
  const cacheKey = 'config:admin_password';
  const cached = apiCache.get(cacheKey);
  if (cached !== undefined) {
    return cached;
  }

  try {
    const password = SystemConfig.getConfigValue('admin_password');
    // 缓存密码 5 分钟
    apiCache.set(cacheKey, password, { ttl: 1000 * 60 * 5 });
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
 * 保存管理员密码到数据库（自动哈希）
 */
function saveAdminPassword(password) {
  try {
    // 如果密码尚未哈希，则进行哈希处理
    const hashedPassword = isHashed(password) ? password : hashPasswordSync(password);
    SystemConfig.setConfig('admin_password', hashedPassword, '管理员密码(哈希)');
    // 清除缓存
    apiCache.delete('config:admin_password');
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
    return accountsEnv
      .split(',')
      .map(item => {
        const [name, token] = item.split(':');
        return { name: name.trim(), token: token.trim() };
      })
      .filter(acc => acc.name && acc.token);
  } catch (e) {
    console.error('❌ 解析环境变量 ACCOUNTS 失败:', e.message);
    return [];
  }
}

/**
 * 检查是否处于演示模式
 */
function isDemoMode() {
  return process.env.DEMO_MODE === 'true';
}

module.exports = {
  CONFIG_DIR,
  ACCOUNTS_FILE,
  PASSWORD_FILE,
  loadAdminPassword,
  isPasswordSavedToFile,
  saveAdminPassword,
  getEnvAccounts,
  ensureConfigDir,
  isDemoMode,
};
