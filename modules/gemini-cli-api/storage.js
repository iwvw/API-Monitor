const dbService = require('../../src/db/database');

// 初始化数据库
dbService.initialize();

// 确保设置表存在
try {
    const db = dbService.getDatabase();
    db.prepare(`
        CREATE TABLE IF NOT EXISTS gemini_cli_settings (
            key TEXT PRIMARY KEY,
            value TEXT,
            updated_at DATETIME DEFAULT CURRENT_TIMESTAMP
        )
    `).run();
} catch (e) {
    console.error('❌ 初始化 Gemini CLI 设置表失败:', e.message);
}

/**
 * 获取账号列表
 */
function getAccounts() {
    try {
        const db = dbService.getDatabase();
        return db.prepare(`
            SELECT 
                a.*,
                (SELECT COUNT(*) FROM gemini_cli_logs l WHERE l.account_id = a.id AND l.status_code = 200) as success_count,
                (SELECT COUNT(*) FROM gemini_cli_logs l WHERE l.account_id = a.id AND l.status_code != 200) as error_count
            FROM gemini_cli_accounts a 
            ORDER BY a.created_at DESC
        `).all();
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
 * 记录调用日志（与 Antigravity 格式一致）
 */
function recordLog(logData) {
    try {
        const db = dbService.getDatabase();
        const stmt = db.prepare(`
            INSERT INTO gemini_cli_logs (account_id, model, is_balanced, request_path, request_method, status_code, duration_ms, client_ip, user_agent, detail)
            VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
        `);
        stmt.run(
            logData.accountId || null,
            logData.model || null,
            logData.is_balanced ? 1 : 0,
            logData.path,
            logData.method,
            logData.statusCode,
            logData.durationMs,
            logData.clientIp || null,
            logData.userAgent || null,
            logData.detail ? JSON.stringify(logData.detail) : null
        );
    } catch (e) {
        console.error('❌ 记录 Gemini CLI 日志失败:', e.message);
    }
}

/**
 * 获取最近日志（与 Antigravity 格式一致）
 */
function getRecentLogs(limit = 100) {
    try {
        const db = dbService.getDatabase();
        const logs = db.prepare(`
            SELECT 
                l.id,
                l.account_id as accountId,
                l.model,
                l.is_balanced as isBalanced,
                a.name as accountName,
                l.request_path as path,
                l.request_method as method,
                l.status_code as statusCode,
                l.duration_ms as durationMs,
                l.client_ip as clientIp,
                l.user_agent as userAgent,
                l.detail,
                l.created_at as timestamp
            FROM gemini_cli_logs l
            LEFT JOIN gemini_cli_accounts a ON l.account_id = a.id
            ORDER BY l.created_at DESC 
            LIMIT ?
        `).all(limit);

        // 如果 model 列为空，尝试从 detail 字段提取 (兼容旧数据)
        return logs.map(log => {
            let model = log.model;
            if (!model && log.detail) {
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

// 确保模型检测历史表存在
try {
    const db = dbService.getDatabase();
    db.prepare(`
        CREATE TABLE IF NOT EXISTS gemini_cli_model_checks (
            id INTEGER PRIMARY KEY AUTOINCREMENT,
            model_id TEXT NOT NULL,
            status TEXT NOT NULL,
            error_message TEXT,
            check_time INTEGER NOT NULL,
            passed_accounts TEXT,
            created_at DATETIME DEFAULT CURRENT_TIMESTAMP
        )
    `).run();
    db.prepare(`CREATE UNIQUE INDEX IF NOT EXISTS idx_gcli_model_checks_unique ON gemini_cli_model_checks(model_id, check_time)`).run();
    db.prepare(`CREATE INDEX IF NOT EXISTS idx_gcli_model_checks_time ON gemini_cli_model_checks(check_time)`).run();
} catch (e) {
    console.error('❌ 初始化 Gemini CLI 模型检测表失败:', e.message);
}

/**
 * 记录模型检测结果
 */
function recordModelCheck(modelId, status, errorMessage = null, checkTime = null, passedAccounts = null) {
    try {
        const db = dbService.getDatabase();
        const time = checkTime || Math.floor(Date.now() / 1000);
        db.prepare(`
            INSERT INTO gemini_cli_model_checks (model_id, status, error_message, check_time, passed_accounts)
            VALUES (?, ?, ?, ?, ?)
            ON CONFLICT(model_id, check_time) DO UPDATE SET
                status = excluded.status,
                error_message = excluded.error_message,
                passed_accounts = excluded.passed_accounts
        `).run(modelId, status, errorMessage, time, passedAccounts);
        return true;
    } catch (e) {
        console.error('❌ [GCLI] 记录模型检测失败:', e.message);
        return false;
    }
}

/**
 * 获取模型检测历史
 */
function getModelCheckHistory() {
    try {
        const db = dbService.getDatabase();

        const times = db.prepare(`
            SELECT DISTINCT check_time FROM gemini_cli_model_checks 
            ORDER BY check_time DESC LIMIT 10
        `).all().map(r => r.check_time);

        const models = db.prepare(`
            SELECT DISTINCT model_id FROM gemini_cli_model_checks 
            ORDER BY model_id
        `).all().map(r => r.model_id);

        if (times.length === 0) {
            return { models: [], times: [], matrix: {} };
        }

        const checks = db.prepare(`
            SELECT model_id, status, check_time, passed_accounts, error_message FROM gemini_cli_model_checks 
            WHERE check_time IN (${times.map(() => '?').join(',')})
        `).all(...times);

        const matrix = {};
        models.forEach(model => { matrix[model] = {}; });
        checks.forEach(check => {
            if (matrix[check.model_id]) {
                matrix[check.model_id][check.check_time] = {
                    status: check.status,
                    passedAccounts: check.passed_accounts || '',
                    error_log: check.error_message || ''
                };
            }
        });

        return { models, times, matrix };
    } catch (e) {
        console.error('❌ [GCLI] 获取检测历史失败:', e.message);
        return { models: [], times: [], matrix: {} };
    }
}

/**
 * 清空模型检测历史
 */
function clearModelCheckHistory() {
    try {
        const db = dbService.getDatabase();
        db.prepare('DELETE FROM gemini_cli_model_checks').run();
        return true;
    } catch (e) {
        console.error('❌ [GCLI] 清空检测历史失败:', e.message);
        return false;
    }
}

/**
 * 获取统计信息
 */
function getStats() {
    try {
        const db = dbService.getDatabase();
        const stats = db.prepare(`
            SELECT 
                COUNT(*) as total_calls,
                SUM(CASE WHEN status_code = 200 THEN 1 ELSE 0 END) as success_calls,
                SUM(CASE WHEN status_code != 200 THEN 1 ELSE 0 END) as fail_calls
            FROM gemini_cli_logs
        `).get();

        const accounts = getAccounts();
        return {
            total_calls: stats.total_calls || 0,
            success_calls: stats.success_calls || 0,
            fail_calls: stats.fail_calls || 0,
            accounts: {
                total: accounts.length,
                online: accounts.filter(a => a.status === 'online').length,
                enabled: accounts.filter(a => a.enable !== 0).length
            }
        };
    } catch (e) {
        console.error('❌ 获取 Gemini CLI 统计失败:', e.message);
        return { total_calls: 0, success_calls: 0, fail_calls: 0, accounts: { total: 0, online: 0, enabled: 0 } };
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
    recordLog,
    getRecentLogs,
    getLogDetail,
    clearLogs,
    getSettings,
    updateSetting,
    getDisabledModels,
    setModelStatus,
    getModelRedirects,
    addModelRedirect,
    removeModelRedirect,
    recordModelCheck,
    getModelCheckHistory,
    clearModelCheckHistory,
    getStats
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
