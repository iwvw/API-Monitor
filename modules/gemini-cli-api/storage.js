const dbService = require('../../src/db/database');

// 初始化数据库
dbService.initialize();

/**
 * 获取账号列表
 */
function getAccounts() {
    try {
        const db = dbService.getDatabase();
        return db.prepare('SELECT * FROM gemini_cli_accounts ORDER BY created_at DESC').all();
    } catch (e) {
        console.error('❌ 读取 Gemini CLI 账号失败:', e.message);
        return [];
    }
}

/**
 * 添加账号
 */
function addAccount(account) {
    try {
        const db = dbService.getDatabase();
        const id = account.id || `gcli_${Math.random().toString(36).slice(2, 10)}`;

        const stmt = db.prepare(`
            INSERT INTO gemini_cli_accounts (id, name, email, client_id, client_secret, refresh_token, project_id)
            VALUES (?, ?, ?, ?, ?, ?, ?)
        `);

        stmt.run(
            id,
            account.name || 'Unnamed Account',
            account.email || null,
            account.client_id,
            account.client_secret,
            account.refresh_token,
            account.project_id || null
        );

        return { id, ...account };
    } catch (e) {
        console.error('❌ 添加 Gemini CLI 账号失败:', e.message);
        throw e;
    }
}

/**
 * 删除账号
 */
function deleteAccount(id) {
    try {
        const db = dbService.getDatabase();
        db.prepare('DELETE FROM gemini_cli_accounts WHERE id = ?').run(id);
        // 也删除相关的 Token 和日志
        db.prepare('DELETE FROM gemini_cli_tokens WHERE account_id = ?').run(id);
        return true;
    } catch (e) {
        console.error('❌ 删除 Gemini CLI 账号失败:', e.message);
        throw e;
    }
}

/**
 * 更新账号 (支持部分更新)
 */
function updateAccount(id, updates) {
    try {
        const db = dbService.getDatabase();

        // 检查账号是否存在
        const current = db.prepare('SELECT id FROM gemini_cli_accounts WHERE id = ?').get(id);
        if (!current) return false;

        const fields = [];
        const values = [];

        // 允许更新的字段
        const allowedFields = ['name', 'email', 'client_id', 'client_secret', 'refresh_token', 'project_id', 'cloudaicompanion_project_id', 'enable', 'status', 'last_used'];

        Object.keys(updates).forEach(key => {
            if (allowedFields.includes(key) && updates[key] !== undefined) {
                fields.push(`${key} = ?`);
                values.push(updates[key]);
            }
        });

        if (fields.length === 0) return true;

        values.push(id);
        const stmt = db.prepare(`UPDATE gemini_cli_accounts SET ${fields.join(', ')} WHERE id = ?`);
        stmt.run(...values);
        return true;
    } catch (e) {
        console.error('❌ 更新 Gemini CLI 账号失败:', e.message);
        throw e;
    }
}

/**
 * 切换账号启用状态
 */
function toggleAccount(id) {
    try {
        const db = dbService.getDatabase();
        const account = db.prepare('SELECT enable FROM gemini_cli_accounts WHERE id = ?').get(id);
        if (!account) return false;

        const newStatus = account.enable ? 0 : 1;
        db.prepare('UPDATE gemini_cli_accounts SET enable = ? WHERE id = ?').run(newStatus, id);
        return newStatus === 1;
    } catch (e) {
        console.error('❌ 切换 Gemini CLI 账号状态失败:', e.message);
        throw e;
    }
}

// Token 缓存
const tokenCache = new Map();

/**
 * 获取账号的 Token
 */
function getTokenByAccountId(accountId) {
    return tokenCache.get(accountId) || null;
}

/**
 * 保存 Token
 */
function saveToken(tokenData) {
    tokenCache.set(tokenData.account_id, {
        access_token: tokenData.access_token,
        expires_at: tokenData.expires_at,
        project_id: tokenData.project_id,
        email: tokenData.email
    });
}

/**
 * 添加日志
 */
function addLog(logData) {
    try {
        const db = dbService.getDatabase();
        const stmt = db.prepare(`
            INSERT INTO gemini_cli_logs (account_id, request_path, request_method, status_code, duration_ms, client_ip, user_agent, detail)
            VALUES (?, ?, ?, ?, ?, ?, ?, ?)
        `);
        stmt.run(
            logData.account_id || null,
            logData.request_path,
            logData.request_method,
            logData.status_code,
            logData.duration_ms,
            logData.client_ip || null,
            logData.user_agent || null,
            logData.detail || null
        );
    } catch (e) {
        console.error('❌ 添加 Gemini CLI 日志失败:', e.message);
    }
}

/**
 * 获取日志列表
 */
function getLogs(limit = 100, offset = 0) {
    try {
        const db = dbService.getDatabase();
        return db.prepare('SELECT * FROM gemini_cli_logs ORDER BY created_at DESC LIMIT ? OFFSET ?').all(limit, offset);
    } catch (e) {
        console.error('❌ 获取 Gemini CLI 日志失败:', e.message);
        return [];
    }
}

/**
 * 获取日志详情
 */
function getLogDetail(id) {
    try {
        const db = dbService.getDatabase();
        return db.prepare('SELECT * FROM gemini_cli_logs WHERE id = ?').get(id);
    } catch (e) {
        console.error('❌ 获取 Gemini CLI 日志详情失败:', e.message);
        return null;
    }
}

/**
 * 清空日志
 */
function clearLogs() {
    try {
        const db = dbService.getDatabase();
        db.prepare('DELETE FROM gemini_cli_logs').run();
        return true;
    } catch (e) {
        console.error('❌ 清空 Gemini CLI 日志失败:', e.message);
        return false;
    }
}

/**
 * 获取设置
 */
function getSettings() {
    try {
        const db = dbService.getDatabase();
        const rows = db.prepare('SELECT * FROM gemini_cli_settings').all();
        const settings = {};
        rows.forEach(row => {
            settings[row.key] = row.value;
        });
        return settings;
    } catch (e) {
        console.error('❌ 获取 Gemini CLI 设置失败:', e.message);
        return {};
    }
}

/**
 * 更新设置
 */
function updateSetting(key, value) {
    try {
        const db = dbService.getDatabase();
        const stmt = db.prepare(`
            INSERT INTO gemini_cli_settings (key, value, updated_at)
            VALUES (?, ?, CURRENT_TIMESTAMP)
            ON CONFLICT(key) DO UPDATE SET value = excluded.value, updated_at = CURRENT_TIMESTAMP
        `);
        stmt.run(key, value);
        return true;
    } catch (e) {
        console.error('❌ 更新 Gemini CLI 设置失败:', e.message);
        return false;
    }
}

// 确保存储模型重定向的表存在
try {
    const db = dbService.getDatabase();
    db.prepare(`
        CREATE TABLE IF NOT EXISTS gemini_cli_model_redirects (
            source_model TEXT PRIMARY KEY,
            target_model TEXT NOT NULL,
            created_at DATETIME DEFAULT CURRENT_TIMESTAMP
        )
    `).run();
} catch (e) {
    console.error('❌ 初始化 Gemini CLI 模型重定向表失败:', e.message);
}

/**
 * 获取所有模型重定向配置
 */
function getModelRedirects() {
    try {
        const db = dbService.getDatabase();
        return db.prepare('SELECT * FROM gemini_cli_model_redirects ORDER BY created_at DESC').all();
    } catch (e) {
        console.error('❌ 获取模型重定向失败:', e.message);
        return [];
    }
}

/**
 * 添加模型重定向
 */
function addModelRedirect(sourceModel, targetModel) {
    try {
        const db = dbService.getDatabase();
        db.prepare(`
            INSERT INTO gemini_cli_model_redirects (source_model, target_model)
            VALUES (?, ?)
            ON CONFLICT(source_model) DO UPDATE SET target_model = excluded.target_model
        `).run(sourceModel, targetModel);
        return true;
    } catch (e) {
        console.error('❌ 添加模型重定向失败:', e.message);
        return false;
    }
}

/**
 * 删除模型重定向
 */
function removeModelRedirect(sourceModel) {
    try {
        const db = dbService.getDatabase();
        db.prepare('DELETE FROM gemini_cli_model_redirects WHERE source_model = ?').run(sourceModel);
        return true;
    } catch (e) {
        console.error('❌ 删除模型重定向失败:', e.message);
        return false;
    }
}

module.exports = {
    getAccounts,
    addAccount,
    updateAccount,
    deleteAccount,
    toggleAccount,
    getTokenByAccountId,
    saveToken,
    addLog,
    getLogs,
    getLogDetail,
    clearLogs,
    getSettings,
    updateSetting,
    getDisabledModels,
    setModelStatus,
    getModelRedirects,
    addModelRedirect,
    removeModelRedirect
};

// 内存中的禁用模型缓存
let disabledModelsCache = null;

/**
 * 获取禁用的模型列表
 */
function getDisabledModels() {
    if (disabledModelsCache !== null) return disabledModelsCache;

    try {
        const db = dbService.getDatabase();
        const result = db.prepare("SELECT value FROM gemini_cli_settings WHERE key = 'disabled_models'").get();
        disabledModelsCache = result ? JSON.parse(result.value) : [];
        return disabledModelsCache;
    } catch (e) {
        console.error('获取禁用模型列表失败:', e.message);
        return [];
    }
}

/**
 * 设置模型状态
 */
function setModelStatus(modelId, enabled) {
    try {
        const db = dbService.getDatabase();
        let disabledModels = getDisabledModels();

        if (enabled) {
            disabledModels = disabledModels.filter(m => m !== modelId);
        } else {
            if (!disabledModels.includes(modelId)) {
                disabledModels.push(modelId);
            }
        }

        db.prepare(`
            INSERT OR REPLACE INTO gemini_cli_settings (key, value)
            VALUES ('disabled_models', ?)
        `).run(JSON.stringify(disabledModels));

        disabledModelsCache = disabledModels;
        return true;
    } catch (e) {
        console.error('设置模型状态失败:', e.message);
        throw e;
    }
}
