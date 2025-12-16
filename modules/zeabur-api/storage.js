/**
 * Zeabur 账号存储管理（使用 SQLite 数据库）
 */

const { ZeaburAccount } = require('../../src/db/models');
const dbService = require('../../src/db/database');

// 初始化数据库
dbService.initialize();

/**
 * 读取服务器存储的账号
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
      projects: [], // 项目数据需要单独查询
      createdAt: acc.created_at,
      updatedAt: acc.updated_at,
      lastSyncedAt: acc.last_synced_at
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
          created_at: account.createdAt || new Date().toISOString(),
          last_synced_at: account.lastSyncedAt || null
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
  loadServerAccounts,
  saveServerAccounts,
  getEnvAccounts
};
