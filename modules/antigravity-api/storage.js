const dbService = require('../../src/db/database');
const { v4: uuidv4 } = require('uuid');

// 初始化数据库
dbService.initialize();

// 确保存储模型配置的表存在
try {
    const db = dbService.getDatabase();
    db.prepare(`
        CREATE TABLE IF NOT EXISTS antigravity_model_config (
            model_id TEXT PRIMARY KEY,
            enabled INTEGER DEFAULT 1,
            updated_at DATETIME DEFAULT CURRENT_TIMESTAMP
        )
    `).run();
} catch (e) {
    console.error('❌ 初始化 Antigravity 模型配置表失败:', e.message);
}

/**
 * 获取所有 Antigravity 账号 (带统计)
 */
function getAccounts() {
    try {
        const db = dbService.getDatabase();
        return db.prepare(`
            SELECT 
                a.*,
                (SELECT project_id FROM antigravity_tokens t WHERE t.account_id = a.id LIMIT 1) as projectId,
                (SELECT COUNT(*) FROM antigravity_logs l WHERE l.account_id = a.id AND l.status_code = 200) as success_count,
                (SELECT COUNT(*) FROM antigravity_logs l WHERE l.account_id = a.id AND l.status_code != 200) as error_count
            FROM antigravity_accounts a
        `).all();
    } catch (e) {
        console.error('❌ 读取 Antigravity 账号失败:', e.message);
        return [];
    }
}

/**
 * 添加 Antigravity 账号
 */
function addAccount(account) {
    try {
        const db = dbService.getDatabase();
        const id = 'ag_' + uuidv4().replace(/-/g, '').substring(0, 12);

        const stmt = db.prepare(`
            INSERT INTO antigravity_accounts (id, name, email, password, api_key, panel_user, panel_password, enable, status)
            VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
        `);

        stmt.run(
            id,
            account.name,
            account.email || null,
            account.password || null,
            account.apiKey || null,
            account.panelUser || 'admin',
            account.panelPassword || null,
            account.enable !== undefined ? (account.enable ? 1 : 0) : 1,
            'unknown'
        );

        return getAccountById(id);
    } catch (e) {
        console.error('❌ 添加 Antigravity 账号失败:', e.message);
        return null;
    }
}

/**
 * 获取单个账号
 */
function getAccountById(id) {
    try {
        const db = dbService.getDatabase();
        return db.prepare('SELECT * FROM antigravity_accounts WHERE id = ?').get(id);
    } catch (e) {
        console.error('❌ 获取 Antigravity 账号失败:', e.message);
        return null;
    }
}

/**
 * 更新账号
 */
function updateAccount(id, updates) {
    try {
        const db = dbService.getDatabase();
        const account = getAccountById(id);
        if (!account) return null;

        const fields = [];
        const values = [];

        Object.entries(updates).forEach(([key, value]) => {
            // 映射前端字段到数据库字段
            const map = {
                name: 'name',
                email: 'email',
                password: 'password',
                apiKey: 'api_key',
                panelUser: 'panel_user',
                panelPassword: 'panel_password',
                enable: 'enable',
                status: 'status',
                lastUsed: 'last_used'
            };

            if (map[key]) {
                fields.push(`${map[key]} = ?`);
                values.push(value === true ? 1 : (value === false ? 0 : value));
            }
        });

        if (fields.length === 0) return account;

        values.push(id);
        const stmt = db.prepare(`UPDATE antigravity_accounts SET ${fields.join(', ')} WHERE id = ?`);
        stmt.run(...values);

        return getAccountById(id);
    } catch (e) {
        console.error('❌ 更新 Antigravity 账号失败:', e.message);
        return null;
    }
}

/**
 * 删除账号
 */
function deleteAccount(id) {
    try {
        const db = dbService.getDatabase();
        // 级联删除会处理 tokens
        const stmt = db.prepare('DELETE FROM antigravity_accounts WHERE id = ?');
        return stmt.run(id).changes > 0;
    } catch (e) {
        console.error('❌ 删除 Antigravity 账号失败:', e.message);
        return false;
    }
}

/**
 * 获取账号的 Token
 */
function getTokenByAccountId(accountId) {
    try {
        const db = dbService.getDatabase();
        return db.prepare('SELECT * FROM antigravity_tokens WHERE account_id = ? AND enable = 1').get(accountId);
    } catch (e) {
        console.error('❌ 获取 Token 失败:', e.message);
        return null;
    }
}

/**
 * 获取所有 Token
 */
function getTokens() {
    try {
        const db = dbService.getDatabase();
        return db.prepare('SELECT * FROM antigravity_tokens').all();
    } catch (e) {
        console.error('❌ 获取所有 Token 失败:', e.message);
        return [];
    }
}

/**
 * 保存或更新 Token
 */
function saveToken(tokenData) {
    try {
        const db = dbService.getDatabase();
        const existing = db.prepare('SELECT id FROM antigravity_tokens WHERE account_id = ?').get(tokenData.accountId);

        if (existing) {
            const stmt = db.prepare(`
                UPDATE antigravity_tokens 
                SET access_token = ?, refresh_token = ?, expires_in = ?, timestamp = ?, project_id = ?, 
                    email = ?, user_id = ?, user_email = ?, enable = 1
                WHERE account_id = ?
            `);
            stmt.run(
                tokenData.accessToken,
                tokenData.refreshToken,
                tokenData.expiresIn || 3600,
                tokenData.timestamp || Date.now(),
                tokenData.projectId || null,
                tokenData.email || null,
                tokenData.userId || null,
                tokenData.userEmail || null,
                tokenData.accountId
            );
        } else {
            const id = 'tok_' + uuidv4().replace(/-/g, '').substring(0, 12);
            const stmt = db.prepare(`
                INSERT INTO antigravity_tokens (id, account_id, access_token, refresh_token, expires_in, timestamp, project_id, email, user_id, user_email, enable)
                VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, 1)
            `);
            stmt.run(
                id,
                tokenData.accountId,
                tokenData.accessToken,
                tokenData.refreshToken,
                tokenData.expiresIn || 3600,
                tokenData.timestamp || Date.now(),
                tokenData.projectId || null,
                tokenData.email || null,
                tokenData.userId || null,
                tokenData.userEmail || null
            );
        }
        return true;
    } catch (e) {
        console.error('❌ 保存 Token 失败:', e.message);
        return false;
    }
}

/**
 * 禁用 Token
 */
function disableToken(accountId) {
    try {
        const db = dbService.getDatabase();
        const stmt = db.prepare('UPDATE antigravity_tokens SET enable = 0 WHERE account_id = ?');
        stmt.run(accountId);
        return true;
    } catch (e) {
        console.error('❌ 禁用 Token 失败:', e.message);
        return false;
    }
}

/**
 * 记录调用日志
 */
function recordLog(logData) {
    try {
        const db = dbService.getDatabase();
        const stmt = db.prepare(`
            INSERT INTO antigravity_logs (account_id, request_path, request_method, status_code, duration_ms, client_ip, user_agent, detail)
            VALUES (?, ?, ?, ?, ?, ?, ?, ?)
        `);
        stmt.run(
            logData.accountId || null,
            logData.path,
            logData.method,
            logData.statusCode,
            logData.durationMs,
            logData.clientIp || null,
            logData.userAgent || null,
            logData.detail ? JSON.stringify(logData.detail) : null
        );
    } catch (e) {
        console.error('❌ 记录调用日志失败:', e.message);
    }
}

/**
 * 获取最近日志（包含账号名称和模型）
 */
function getRecentLogs(limit = 100) {
    try {
        const db = dbService.getDatabase();
        const logs = db.prepare(`
            SELECT 
                l.id,
                l.account_id as accountId,
                a.name as accountName,
                l.request_path as path,
                l.request_method as method,
                l.status_code as statusCode,
                l.duration_ms as durationMs,
                l.client_ip as clientIp,
                l.user_agent as userAgent,
                l.detail,
                l.created_at as timestamp
            FROM antigravity_logs l
            LEFT JOIN antigravity_accounts a ON l.account_id = a.id
            ORDER BY l.created_at DESC 
            LIMIT ?
        `).all(limit);

        // 从 detail 字段提取 model
        return logs.map(log => {
            let model = null;
            if (log.detail) {
                try {
                    const detail = JSON.parse(log.detail);
                    model = detail.model || null;
                } catch (e) { }
            }
            return { ...log, model, detail: undefined };
        });
    } catch (e) {
        console.error('❌ 获取日志失败:', e.message);
        return [];
    }
}

/**
 * 获取日志详情
 */
function getLogDetail(id) {
    try {
        const db = dbService.getDatabase();
        const log = db.prepare('SELECT * FROM antigravity_logs WHERE id = ?').get(id);
        if (log && log.detail) {
            log.detail = JSON.parse(log.detail);
        }
        return log;
    } catch (e) {
        console.error('❌ 获取日志详情失败:', e.message);
        return null;
    }
}

/**
 * 清空日志
 */
function clearLogs() {
    try {
        const db = dbService.getDatabase();
        db.prepare('DELETE FROM antigravity_logs').run();
        return true;
    } catch (e) {
        console.error('❌ 清空日志失败:', e.message);
        return false;
    }
}

/**
 * 获取设置
 */
function getSettings() {
    try {
        const db = dbService.getDatabase();
        const rows = db.prepare('SELECT * FROM antigravity_settings').all();
        const settings = {};
        rows.forEach(row => {
            settings[row.key] = row.value;
        });
        return settings;
    } catch (e) {
        console.error('❌ 获取设置失败:', e.message);
        return {};
    }
}

/**
 * 获取单个设置
 */
function getSetting(key, defaultValue = null) {
    try {
        const db = dbService.getDatabase();
        const row = db.prepare('SELECT value FROM antigravity_settings WHERE key = ?').get(key);
        return row ? row.value : defaultValue;
    } catch (e) {
        console.error('❌ 获取设置失败:', e.message);
        return defaultValue;
    }
}

/**
 * 更新设置
 */
function updateSetting(key, value) {
    try {
        const db = dbService.getDatabase();
        const stmt = db.prepare(`
            INSERT INTO antigravity_settings (key, value, updated_at)
            VALUES (?, ?, CURRENT_TIMESTAMP)
            ON CONFLICT(key) DO UPDATE SET value = excluded.value, updated_at = CURRENT_TIMESTAMP
        `);
        stmt.run(key, value);
        return true;
    } catch (e) {
        console.error('❌ 更新设置失败:', e.message);
        return false;
    }
}

/**
 * 获取所有模型配置
 */
function getModelConfigs() {
    try {
        const db = dbService.getDatabase();
        const rows = db.prepare('SELECT * FROM antigravity_model_config').all();
        const configs = {};
        rows.forEach(row => {
            configs[row.model_id] = row.enabled === 1;
        });
        return configs;
    } catch (e) {
        console.error('❌ 获取模型配置失败:', e.message);
        return {};
    }
}

/**
 * 获取单个模型启用状态
 */
function isModelEnabled(modelId) {
    try {
        const db = dbService.getDatabase();
        const row = db.prepare('SELECT enabled FROM antigravity_model_config WHERE model_id = ?').get(modelId);
        // 默认为启用 (true)
        return row ? row.enabled === 1 : true;
    } catch (e) {
        return true;
    }
}

/**
 * 更新模型状态
 */
function updateModelStatus(modelId, enabled) {
    try {
        const db = dbService.getDatabase();
        const stmt = db.prepare(`
            INSERT INTO antigravity_model_config (model_id, enabled, updated_at)
            VALUES (?, ?, CURRENT_TIMESTAMP)
            ON CONFLICT(model_id) DO UPDATE SET enabled = excluded.enabled, updated_at = CURRENT_TIMESTAMP
        `);
        stmt.run(modelId, enabled ? 1 : 0);
        return true;
    } catch (e) {
        console.error('❌ 更新模型状态失败:', e.message);
        return false;
    }
}

// 确保存储模型重定向的表存在
try {
    const db = dbService.getDatabase();
    db.prepare(`
        CREATE TABLE IF NOT EXISTS antigravity_model_redirects (
            source_model TEXT PRIMARY KEY,
            target_model TEXT NOT NULL,
            created_at DATETIME DEFAULT CURRENT_TIMESTAMP
        )
    `).run();
} catch (e) {
    console.error('❌ 初始化 Antigravity 模型重定向表失败:', e.message);
}

/**
 * 获取所有模型重定向配置
 */
function getModelRedirects() {
    try {
        const db = dbService.getDatabase();
        return db.prepare('SELECT * FROM antigravity_model_redirects ORDER BY created_at DESC').all();
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
            INSERT INTO antigravity_model_redirects (source_model, target_model)
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
        db.prepare('DELETE FROM antigravity_model_redirects WHERE source_model = ?').run(sourceModel);
        return true;
    } catch (e) {
        console.error('❌ 删除模型重定向失败:', e.message);
        return false;
    }
}

module.exports = {
    getAccounts,
    addAccount,
    getTokens,
    updateAccount,
    deleteAccount,
    getAccountById,
    getTokenByAccountId,
    saveToken,
    disableToken,
    recordLog,
    getRecentLogs,
    getLogDetail,
    clearLogs,
    getSettings,
    getSetting,
    updateSetting,
    getModelConfigs,
    isModelEnabled,
    isModelEnabled,
    updateModelStatus,
    getModelRedirects,
    addModelRedirect,
    removeModelRedirect
};
