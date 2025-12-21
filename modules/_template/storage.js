/**
 * 模块数据持久化管理
 */

const { TemplateAccount, TemplateItem } = require('./models');
const dbService = require('../../src/db/database');

// 初始化数据库
dbService.initialize();

/**
 * 加载所有账号
 */
function loadAccounts() {
    try {
        return TemplateAccount.findAll();
    } catch (e) {
        console.error('❌ 加载账号失败:', e.message);
        return [];
    }
}

/**
 * 保存/更新账号列表
 */
function saveAccounts(accounts) {
    try {
        const db = dbService.getDatabase();
        const transaction = db.transaction(() => {
            // 注意：这里取决于你是覆盖式保存还是增量保存
            // 覆盖式示例:
            TemplateAccount.truncate();
            accounts.forEach(acc => TemplateAccount.createAccount(acc));
        });
        transaction();
        return true;
    } catch (e) {
        console.error('❌ 保存账号失败:', e.message);
        return false;
    }
}

/**
 * 获取环境变量配置的账号
 */
function getEnvAccounts() {
    const envVar = process.env.{{MODULE_ENV_VAR}}; // 例如 ACCOUNTS_TEMPLATE
    if (!envVar) return [];

    try {
        return envVar.split(',').map(item => {
            const [name, token] = item.split(':');
            return { name: name.trim(), token: token.trim(), isEnv: true };
        }).filter(acc => acc.name && acc.token);
    } catch (e) {
        console.error('❌ 解析环境变量失败:', e.message);
        return [];
    }
}

module.exports = {
    loadAccounts,
    saveAccounts,
    getEnvAccounts
};
