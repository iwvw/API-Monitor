/**
 * DeepSeek 模块 SQLite 存储层
 * 参照 gemini-cli-api/storage.js 的模式实现
 */

const path = require('path');
const fs = require('fs');
const { createLogger } = require('../../src/utils/logger');
const logger = createLogger('DS-Store');

let db = null;

function getDb() {
    if (db) return db;
    try {
        const dbModule = require('../../src/db/database');
        db = dbModule.getDatabase();
    } catch (e) {
        logger.error('获取数据库连接失败:', e.message);
    }
    return db;
}

function initSchema() {
    const database = getDb();
    if (!database) return;
    try {
        const schemaPath = path.join(__dirname, 'schema.sql');
        const schema = fs.readFileSync(schemaPath, 'utf8');
        database.exec(schema);
        logger.info('DeepSeek 数据库表已初始化');
    } catch (e) {
        logger.error('数据库初始化失败:', e.message);
    }
}

// 初始化
initSchema();

// ==================== 账号管理 ====================

function getAccounts() {
    const database = getDb();
    if (!database) return [];
    try {
        return database.prepare('SELECT * FROM ds_accounts ORDER BY created_at ASC').all();
    } catch (e) {
        logger.error('获取账号列表失败:', e.message);
        return [];
    }
}

function addAccount(account) {
    const database = getDb();
    if (!database) return;
    database.prepare(`
    INSERT INTO ds_accounts (id, name, email, mobile, password, token, enable)
    VALUES (?, ?, ?, ?, ?, ?, ?)
  `).run(
        account.id,
        account.name || '',
        account.email || '',
        account.mobile || '',
        account.password || '',
        account.token || '',
        account.enable !== undefined ? account.enable : 1
    );
}

function updateAccount(id, data) {
    const database = getDb();
    if (!database) return;
    const fields = [];
    const values = [];
    for (const [key, value] of Object.entries(data)) {
        if (['name', 'email', 'mobile', 'password', 'token', 'enable'].includes(key)) {
            fields.push(`${key} = ?`);
            values.push(value);
        }
    }
    if (fields.length === 0) return;
    fields.push('updated_at = CURRENT_TIMESTAMP');
    values.push(id);
    database.prepare(`UPDATE ds_accounts SET ${fields.join(', ')} WHERE id = ?`).run(...values);
}

function deleteAccount(id) {
    const database = getDb();
    if (!database) return;
    database.prepare('DELETE FROM ds_accounts WHERE id = ?').run(id);
}

function toggleAccount(id) {
    const database = getDb();
    if (!database) return 0;
    const account = database.prepare('SELECT enable FROM ds_accounts WHERE id = ?').get(id);
    if (!account) return 0;
    const newState = account.enable ? 0 : 1;
    database.prepare('UPDATE ds_accounts SET enable = ?, updated_at = CURRENT_TIMESTAMP WHERE id = ?').run(newState, id);
    return newState;
}

function updateToken(id, token) {
    const database = getDb();
    if (!database) return;
    database.prepare('UPDATE ds_accounts SET token = ?, updated_at = CURRENT_TIMESTAMP WHERE id = ?').run(token, id);
}

function getAccountById(id) {
    const database = getDb();
    if (!database) return null;
    return database.prepare('SELECT * FROM ds_accounts WHERE id = ?').get(id);
}

// ==================== 设置管理 ====================

function getSettings() {
    const database = getDb();
    if (!database) return {};
    try {
        const rows = database.prepare('SELECT key, value FROM ds_settings').all();
        const settings = {};
        rows.forEach(r => { settings[r.key] = r.value; });
        return settings;
    } catch (e) {
        return {};
    }
}

function getSetting(key) {
    const database = getDb();
    if (!database) return null;
    const row = database.prepare('SELECT value FROM ds_settings WHERE key = ?').get(key);
    return row ? row.value : null;
}

function updateSetting(key, value) {
    const database = getDb();
    if (!database) return;
    database.prepare('INSERT OR REPLACE INTO ds_settings (key, value) VALUES (?, ?)').run(key, String(value));
}

// ==================== 日志管理 ====================

function recordLog(data) {
    const database = getDb();
    if (!database) return;
    try {
        database.prepare(`
      INSERT INTO ds_logs (account_id, model, is_balanced, path, method, status_code, duration_ms, first_token_time_ms, client_ip, user_agent, detail)
      VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
    `).run(
            data.accountId || '',
            data.model || '',
            data.is_balanced ? 1 : 0,
            data.path || '',
            data.method || 'POST',
            data.statusCode || 0,
            data.durationMs || 0,
            data.firstTokenTimeMs || null,
            data.clientIp || '',
            data.userAgent || '',
            data.detail ? JSON.stringify(data.detail) : null
        );
    } catch (e) {
        logger.error('记录日志失败:', e.message);
    }
}

function getRecentLogs(limit = 100) {
    const database = getDb();
    if (!database) return [];
    try {
        const logs = database.prepare(`
      SELECT l.*, a.name as accountName FROM ds_logs l
      LEFT JOIN ds_accounts a ON l.account_id = a.id
      ORDER BY l.timestamp DESC LIMIT ?
    `).all(limit);
        return logs.map(log => ({
            ...log,
            isBalanced: log.is_balanced === 1,
            statusCode: log.status_code,
            durationMs: log.duration_ms,
            firstTokenTimeMs: log.first_token_time_ms,
            accountId: log.account_id,
            clientIp: log.client_ip,
            userAgent: log.user_agent,
        }));
    } catch (e) {
        return [];
    }
}

function getLogDetail(id) {
    const database = getDb();
    if (!database) return null;
    return database.prepare('SELECT * FROM ds_logs WHERE id = ?').get(id);
}

function clearLogs() {
    const database = getDb();
    if (!database) return;
    database.prepare('DELETE FROM ds_logs').run();
}

// ==================== 模型重定向 ====================

function getModelRedirects() {
    const database = getDb();
    if (!database) return [];
    try {
        return database.prepare('SELECT * FROM ds_model_redirects').all();
    } catch (e) {
        return [];
    }
}

function addModelRedirect(source, target) {
    const database = getDb();
    if (!database) return;
    database.prepare('INSERT OR REPLACE INTO ds_model_redirects (source_model, target_model) VALUES (?, ?)').run(source, target);
}

function removeModelRedirect(source) {
    const database = getDb();
    if (!database) return;
    database.prepare('DELETE FROM ds_model_redirects WHERE source_model = ?').run(source);
}

function getDisabledModels() {
    // 通过设置中存储禁用列表
    const val = getSetting('DISABLED_MODELS');
    if (!val) return [];
    try { return JSON.parse(val); } catch (e) { return []; }
}

// ==================== 会话缓存 (连续对话) ====================

function saveSessionCache(contentKey, sessionId, parentId) {
    const database = getDb();
    if (!database) return;
    try {
        database.prepare(
            'INSERT INTO ds_session_cache (content_key, session_id, parent_id) VALUES (?, ?, ?)'
        ).run(contentKey, sessionId, parentId || null);
        // 保留最近 500 条
        database.prepare(
            'DELETE FROM ds_session_cache WHERE id NOT IN (SELECT id FROM ds_session_cache ORDER BY id DESC LIMIT 500)'
        ).run();
    } catch (e) {
        logger.error('保存会话缓存失败:', e.message);
    }
}

function findSessionCache(content) {
    const database = getDb();
    if (!database) return null;
    try {
        const rows = database.prepare(
            'SELECT content_key, session_id, parent_id FROM ds_session_cache ORDER BY id DESC'
        ).all();
        for (const row of rows) {
            if (content.includes(row.content_key)) {
                return { sessionId: row.session_id, parentId: row.parent_id };
            }
        }
    } catch (e) {
        logger.error('查找会话缓存失败:', e.message);
    }
    return null;
}

module.exports = {
    getAccounts,
    addAccount,
    updateAccount,
    deleteAccount,
    toggleAccount,
    updateToken,
    getAccountById,
    getSettings,
    getSetting,
    updateSetting,
    recordLog,
    getRecentLogs,
    getLogDetail,
    clearLogs,
    getModelRedirects,
    addModelRedirect,
    removeModelRedirect,
    getDisabledModels,
    saveSessionCache,
    findSessionCache,
};
