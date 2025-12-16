/**
 * Zeabur 账号存储管理
 */

const fs = require('fs');
const path = require('path');

// 配置目录（可通过环境变量覆盖）
const CONFIG_DIR = process.env.CONFIG_DIR || path.join(__dirname, '../../config');
const ACCOUNTS_FILE = path.join(CONFIG_DIR, 'zb-accounts.json');

/**
 * 确保配置目录存在
 */
function ensureConfigDir() {
  if (!fs.existsSync(CONFIG_DIR)) {
    fs.mkdirSync(CONFIG_DIR, { recursive: true });
  }
}

/**
 * 读取服务器存储的账号
 */
function loadServerAccounts() {
  try {
    if (fs.existsSync(ACCOUNTS_FILE)) {
      const stats = fs.statSync(ACCOUNTS_FILE);
      if (!stats.isFile()) {
        console.error('❌ zb-accounts.json 是目录而非文件，正在删除...');
        fs.rmSync(ACCOUNTS_FILE, { recursive: true });
        return [];
      }
      const data = fs.readFileSync(ACCOUNTS_FILE, 'utf8');
      return JSON.parse(data);
    }
  } catch (e) {
    console.error('❌ 读取账号文件失败:', e.message);
  }
  return [];
}

/**
 * 保存账号到服务器
 */
function saveServerAccounts(accounts) {
  try {
    ensureConfigDir();

    if (fs.existsSync(ACCOUNTS_FILE)) {
      const stats = fs.statSync(ACCOUNTS_FILE);
      if (!stats.isFile()) {
        console.warn('⚠️ 发现 zb-accounts.json 是目录，正在删除...');
        fs.rmSync(ACCOUNTS_FILE, { recursive: true });
      }
    }

    fs.writeFileSync(ACCOUNTS_FILE, JSON.stringify(accounts, null, 2), 'utf8');
    return true;
  } catch (e) {
    console.error('❌ 保存账号文件失败:', e.message);
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
