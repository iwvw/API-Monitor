/**
 * TOTP/HOTP 模块数据持久化管理
 */

const { TotpAccount, TotpGroup } = require('./models');
const dbService = require('../../src/db/database');

// 确保数据库已初始化
dbService.initialize();

// ==================== 账号操作 ====================

function loadAccounts() {
    try {
        return TotpAccount.getAllSorted();
    } catch (e) {
        console.error('❌ 加载账号失败:', e.message);
        return [];
    }
}

function getAccount(id) {
    try {
        return TotpAccount.findById(id);
    } catch (e) {
        console.error('❌ 获取账号失败:', e.message);
        return null;
    }
}

function createAccount(data) {
    try {
        return TotpAccount.createAccount(data);
    } catch (e) {
        console.error('❌ 创建账号失败:', e.message);
        throw e;
    }
}

function updateAccount(id, updates) {
    try {
        return TotpAccount.updateAccount(id, updates);
    } catch (e) {
        console.error('❌ 更新账号失败:', e.message);
        throw e;
    }
}

function deleteAccount(id) {
    try {
        return TotpAccount.delete(id);
    } catch (e) {
        console.error('❌ 删除账号失败:', e.message);
        throw e;
    }
}

function updateOrder(orderedIds) {
    try {
        TotpAccount.updateOrder(orderedIds);
    } catch (e) {
        console.error('❌ 更新排序失败:', e.message);
        throw e;
    }
}

function importAccounts(accounts) {
    const results = { success: 0, failed: 0, errors: [] };

    for (const acc of accounts) {
        try {
            if (!acc.secret) {
                results.failed++;
                results.errors.push(`缺少密钥: ${acc.issuer || '未知'}`);
                continue;
            }
            createAccount(acc);
            results.success++;
        } catch (e) {
            results.failed++;
            results.errors.push(`${acc.issuer || '未知'}: ${e.message}`);
        }
    }

    return results;
}

// ==================== 分组操作 ====================

function loadGroups() {
    try {
        return TotpGroup.getAllSorted();
    } catch (e) {
        console.error('❌ 加载分组失败:', e.message);
        return [];
    }
}

function getGroup(id) {
    try {
        return TotpGroup.findById(id);
    } catch (e) {
        console.error('❌ 获取分组失败:', e.message);
        return null;
    }
}

function createGroup(data) {
    try {
        return TotpGroup.createGroup(data);
    } catch (e) {
        console.error('❌ 创建分组失败:', e.message);
        throw e;
    }
}

function updateGroup(id, updates) {
    try {
        return TotpGroup.updateGroup(id, updates);
    } catch (e) {
        console.error('❌ 更新分组失败:', e.message);
        throw e;
    }
}

function deleteGroup(id) {
    try {
        // 删除分组时，将关联账号的 group_id 设为 null
        const db = dbService.getDatabase();
        db.prepare('UPDATE totp_accounts SET group_id = NULL WHERE group_id = ?').run(id);
        return TotpGroup.delete(id);
    } catch (e) {
        console.error('❌ 删除分组失败:', e.message);
        throw e;
    }
}

module.exports = {
    // 账号
    loadAccounts,
    getAccount,
    createAccount,
    updateAccount,
    deleteAccount,
    updateOrder,
    importAccounts,
    // 分组
    loadGroups,
    getGroup,
    createGroup,
    updateGroup,
    deleteGroup
};
