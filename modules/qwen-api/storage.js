/**
 * Qwen 模块 SQLite 存储层
 * 参照 deepseek-api/storage.js 的模式实现
 */

const path = require('path');
const fs = require('fs');
const { createLogger } = require('../../src/utils/logger');
const { v4: uuidv4 } = require('uuid');
const logger = createLogger('Qwen-Store');

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

/**
 * 初始化数据库表逻辑
 */
function initDb() {
    const database = getDb();
    if (!database) return;

    try {
        const schema = fs.readFileSync(path.join(__dirname, 'schema.sql'), 'utf8');
        const statements = schema.split(';').filter(s => s.trim());
        for (const sql of statements) {
            database.prepare(sql).run();
        }
        
        // 自动迁移：检查并添加缺失字段 (解决 V2 升级兼容性)
        const tableInfo = database.prepare('PRAGMA table_info(qwen_logs)').all();
        const columns = tableInfo.map(c => c.name);
        
        if (!columns.includes('messages')) {
            logger.info('正在为 qwen_logs 表添加 messages 字段...');
            database.prepare('ALTER TABLE qwen_logs ADD COLUMN messages TEXT').run();
        }
        if (!columns.includes('first_token_time_ms')) {
            logger.info('正在为 qwen_logs 表添加 first_token_time_ms 字段...');
            database.prepare('ALTER TABLE qwen_logs ADD COLUMN first_token_time_ms INTEGER').run();
        }

        logger.info('Qwen 数据库表已初始化');
    } catch (e) {
        logger.error('初始化数据库表失败:', e.message);
    }
}

// 立即尝试初始化
initDb();

// ==================== 存储接口 ====================

function getAccounts() {
    const database = getDb();
    if (!database) return [];
    try {
        return database.prepare('SELECT * FROM qwen_accounts').all();
    } catch (e) { return []; }
}

function addAccount(account) {
    const database = getDb();
    if (!database) return;
    try {
        database.prepare(`
            INSERT INTO qwen_accounts (id, name, email, mobile, password, token, uid, enable, status)
            VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
        `).run(
            account.id, account.name, account.email, account.mobile,
            account.password, account.token, account.uid,
            account.enable !== undefined ? account.enable : 1,
            account.status || 'unknown'
        );
    } catch (e) { logger.error('添加账号失败:', e.message); }
}

function updateAccount(id, data) {
    const database = getDb();
    if (!database) return;
    const fields = Object.keys(data).map(k => `${k} = ?`).join(', ');
    const values = Object.values(data);
    try {
        database.prepare(`UPDATE qwen_accounts SET ${fields} WHERE id = ?`).run(...values, id);
    } catch (e) { logger.error('更新账号失败:', e.message); }
}

function deleteAccount(id) {
    const database = getDb();
    if (!database) return;
    try {
        database.prepare('DELETE FROM qwen_accounts WHERE id = ?').run(id);
    } catch (e) { logger.error('删除账号失败:', e.message); }
}

function getSettings() {
    const database = getDb();
    if (!database) return {};
    try {
        const rows = database.prepare('SELECT * FROM qwen_settings').all();
        const settings = {};
        rows.forEach(r => settings[r.key] = r.value);
        return settings;
    } catch (e) { return {}; }
}

function getSetting(key) {
    const database = getDb();
    if (!database) return null;
    try {
        const row = database.prepare('SELECT value FROM qwen_settings WHERE key = ?').get(key);
        return row ? row.value : null;
    } catch (e) { return null; }
}

function updateSetting(key, value) {
    const database = getDb();
    if (!database) return;
    try {
        database.prepare('INSERT OR REPLACE INTO qwen_settings (key, value) VALUES (?, ?)').run(key, value);
    } catch (e) { logger.error('更新配置失败:', e.message); }
}

/**
 * 记录请求日志 (V2 增强版)
 */
function addLog(logData) {
    const database = getDb();
    if (!database) return;
    try {
        database.prepare(`
            INSERT INTO qwen_logs (trace_id, account_id, model, prompt, response, messages, tokens, status, error, duration, first_token_time_ms)
            VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
        `).run(
            logData.trace_id || uuidv4(),
            logData.account_id,
            logData.model,
            logData.prompt,
            logData.response,
            logData.messages ? (typeof logData.messages === 'string' ? logData.messages : JSON.stringify(logData.messages)) : null,
            logData.tokens || 0,
            logData.status,
            logData.error || null,
            logData.duration || 0,
            logData.first_token_time_ms || null
        );
    } catch (e) {
        logger.error('记录请求日志失败:', e.message);
    }
}

// ==================== 统计与矩阵 ====================

function getStats() {
    const database = getDb();
    if (!database) return { total_calls: 0, success_calls: 0, total_tokens: 0 };
    try {
        const stats = database.prepare(`
            SELECT 
                COUNT(*) as total_calls,
                SUM(CASE WHEN status = 'success' THEN 1 ELSE 0 END) as success_calls,
                SUM(tokens) as total_tokens,
                AVG(duration) as avg_duration
            FROM qwen_logs
        `).get();
        return {
            total_calls: stats.total_calls || 0,
            success_calls: stats.success_calls || 0,
            total_tokens: stats.total_tokens || 0,
            avg_duration: stats.avg_duration || 0
        };
    } catch (e) {
        return { total_calls: 0, success_calls: 0, total_tokens: 0, avg_duration: 0 };
    }
}

function getMatrix() {
    const database = getDb();
    if (!database) return {};
    const row = getSetting('QWEN_MATRIX');
    try {
        return row ? JSON.parse(row) : {};
    } catch (e) { return {}; }
}

function updateMatrixItem(modelId, data) {
    const matrix = getMatrix();
    matrix[modelId] = {
        ...(matrix[modelId] || {}),
        ...data
    };
    updateSetting('QWEN_MATRIX', JSON.stringify(matrix));
    return { success: true };
}

async function syncModelsFromOfficial() {
    logger.info('正在从通义千问官网同步模型列表...');
    try {
        const axios = require('axios');
        
        // 改进：尝试获取任意一个在线账号的 Token 以获取全量模型
        const accounts = getAccounts();
        const activeAccount = accounts.find(a => a.token && (a.status === 'online' || a.enable !== false));
        const headers = {
            'User-Agent': 'Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/130.0.0.0 Safari/537.36'
        };
        
        if (activeAccount && activeAccount.token) {
            const jwt = activeAccount.token.includes('token=') ? 
                activeAccount.token.match(/token=([^;]+)/)[1] : activeAccount.token;
            headers['Authorization'] = `Bearer ${jwt}`;
            logger.info(`使用账号 [${activeAccount.name}] 的 Token 进行全量模型同步...`);
        } else {
            logger.info('未发现在线账号，将以游客身份同步 (模型数量将受限)...');
        }

        const resp = await axios.get('https://chat.qwen.ai/api/models', {
            headers: headers,
            timeout: 15000 
        });

        if (!resp || !resp.data) throw new Error('同步失败：官网返回了空响应');
        
        const officialModels = resp.data.data || [];
        logger.info(`官网接口返回了 ${officialModels.length} 个模型`);
        
        if (officialModels.length === 0) return { success: true, count: 0, added: 0 };

        const matrix = getMatrix();
        let addedCount = 0;

        officialModels.forEach(m => {
            if (!m.id) return;
            if (!matrix[m.id]) {
                matrix[m.id] = {
                    enabled: true,
                    name: m.name || m.id,
                    capabilities: m.info?.meta?.capabilities || {}
                };
                addedCount++;
            }
        });

        if (addedCount > 0) {
            updateSetting('QWEN_MATRIX', JSON.stringify(matrix));
            logger.info(`同步完成，矩阵新增了 ${addedCount} 个模型`);
        } else {
            logger.info('同步完成，矩阵已是最新，无新增模型');
        }

        return { success: true, count: officialModels.length, added: addedCount };
    } catch (e) {
        let msg = e.message;
        if (e.code === 'ECONNABORTED') msg = '请求官网超时（15s），请检查服务器网络状态';
        else if (e.response) msg = `官网接口报错: ${e.response.status} ${e.response.statusText}`;
        
        logger.error('同步官网模型异常:', msg);
        throw new Error(msg);
    }
}

// ==================== 模型重定向 (别名) ====================

function getModelRedirects() {
    const database = getDb();
    if (!database) return [];
    try {
        return database.prepare('SELECT * FROM qwen_model_redirects ORDER BY created_at DESC').all();
    } catch (e) { return []; }
}

function saveModelRedirect(sourceModel, targetModel) {
    const database = getDb();
    if (!database) return;
    try {
        database.prepare('INSERT OR REPLACE INTO qwen_model_redirects (source_model, target_model) VALUES (?, ?)').run(sourceModel, targetModel);
        return { success: true };
    } catch (e) {
        logger.error('保存模型重定向失败:', e.message);
        throw e;
    }
}

function deleteModelRedirect(sourceModel) {
    const database = getDb();
    if (!database) return;
    try {
        database.prepare('DELETE FROM qwen_model_redirects WHERE source_model = ?').run(sourceModel);
        return { success: true };
    } catch (e) {
        logger.error('删除模型重定向失败:', e.message);
        throw e;
    }
}

module.exports = {
    getAccounts, addAccount, updateAccount, deleteAccount,
    getSettings, getSetting, updateSetting,
    addLog, getStats,
    getMatrix, updateMatrixItem, syncModelsFromOfficial,
    getModelRedirects, saveModelRedirect, deleteModelRedirect
};
